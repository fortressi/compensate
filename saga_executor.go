package compensate

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/tidwall/btree"
	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/topo"
)

// ActionState represents the execution state of an action
type ActionState int

const (
	ActionStatePending ActionState = iota
	ActionStateRunning
	ActionStateCompleted
	ActionStateFailed
	ActionStateUndoing
	ActionStateUndone
)

func (s ActionState) String() string {
	switch s {
	case ActionStatePending:
		return "pending"
	case ActionStateRunning:
		return "running"
	case ActionStateCompleted:
		return "completed"
	case ActionStateFailed:
		return "failed"
	case ActionStateUndoing:
		return "undoing"
	case ActionStateUndone:
		return "undone"
	default:
		return "unknown"
	}
}

// ExecutionNode represents a node in the execution context
type ExecutionNode struct {
	NodeIndex int64
	NodeName  NodeName
	State     ActionState
	Result    *ActionResult[ActionData] // Full result including timing, warnings, metrics
	Error     error
}

// ExecutionRecord tracks the execution of a single action
type ExecutionRecord struct {
	ActionName string
	NodeID     int64
	StartTime  time.Time
	EndTime    time.Time
	Status     ActionState
	Error      error
}

// SagaExecutor handles the sequential execution of saga actions
type SagaExecutor[T any, S SagaType[T]] struct {
	dag           *SagaDag
	actionRegistry *ActionRegistry[T, S]
	sagaContext   S
	
	// Execution state
	nodes        map[int64]*ExecutionNode
	ancestorTree *btree.Map[NodeName, any]
	completed    []int64
	failed       []int64
	
	// Execution tracking
	executionTrace []ExecutionRecord
	
	// Persistence (required)
	store    Store[T]
	sagaID   string
	startedAt time.Time
}


// NewSagaExecutor creates a new saga executor with required persistence
func NewSagaExecutor[T any, S SagaType[T]](
	dag *SagaDag,
	actionRegistry *ActionRegistry[T, S],
	sagaContext S,
	sagaID string,
	store Store[T],
) *SagaExecutor[T, S] {
	executor := &SagaExecutor[T, S]{
		dag:            dag,
		actionRegistry: actionRegistry,
		sagaContext:    sagaContext,
		sagaID:         sagaID,
		store:          store,
		nodes:          make(map[int64]*ExecutionNode),
		ancestorTree:   btree.NewMap[NodeName, any](10),
		completed:      make([]int64, 0),
		failed:         make([]int64, 0),
		executionTrace: make([]ExecutionRecord, 0),
		startedAt:      time.Now(),
	}
	
	// Initialize execution nodes
	executor.initializeNodes()
	
	return executor
}


// initializeNodes sets up the execution state for all nodes
func (e *SagaExecutor[T, S]) initializeNodes() {
	for nodeIndex, internalNode := range e.dag.Nodes {
		nodeName := ""
		if name := internalNode.NodeName(); name != nil {
			nodeName = string(*name)
		}
		
		e.nodes[nodeIndex] = &ExecutionNode{
			NodeIndex: nodeIndex,
			NodeName:  NodeName(nodeName),
			State:     ActionStatePending,
			Result:    nil,
			Error:     nil,
		}
	}
}

// Execute runs the saga sequentially
func (e *SagaExecutor[T, S]) Execute(ctx context.Context) error {
	// Save initial state
	if err := e.persistState(ctx, SagaStatusRunning); err != nil {
		return fmt.Errorf("failed to save initial state: %w", err)
	}
	
	// Get topological order of nodes
	executionOrder, err := e.getTopologicalOrder()
	if err != nil {
		return fmt.Errorf("failed to get execution order: %w", err)
	}
	
	// Execute nodes in order
	for _, nodeIndex := range executionOrder {
		if err := e.executeNode(ctx, nodeIndex); err != nil {
			// If execution fails, trigger compensation
			e.failed = append(e.failed, nodeIndex)
			
			// Persist failure state
			if persistErr := e.persistState(ctx, SagaStatusFailed); persistErr != nil {
				// Log persistence error but don't fail the compensation
				fmt.Printf("Warning: failed to persist failure state: %v\n", persistErr)
			}
			
			if compensationErr := e.compensate(ctx); compensationErr != nil {
				return fmt.Errorf("action failed and compensation failed: action_error=%w, compensation_error=%v", err, compensationErr)
			}
			return fmt.Errorf("saga failed at node %d: %w", nodeIndex, err)
		}
		e.completed = append(e.completed, nodeIndex)
		
		// Persist execution state after each node
		if persistErr := e.persistState(ctx, SagaStatusRunning); persistErr != nil {
			// Log persistence error but continue execution
			fmt.Printf("Warning: failed to persist execution state: %v\n", persistErr)
		}
	}
	
	// Mark saga as completed
	if err := e.persistState(ctx, SagaStatusCompleted); err != nil {
		fmt.Printf("Warning: failed to persist completion state: %v\n", err)
	}
	
	return nil
}

// executeNode executes a single node
func (e *SagaExecutor[T, S]) executeNode(ctx context.Context, nodeIndex int64) error {
	execNode := e.nodes[nodeIndex]
	internalNode := e.dag.Nodes[nodeIndex]
	
	// Update state to running
	execNode.State = ActionStateRunning
	
	// Only handle ActionNodeInternal for now
	actionNode, ok := internalNode.(*ActionNodeInternal)
	if !ok {
		// Skip non-action nodes (like StartNode, EndNode)
		execNode.State = ActionStateCompleted
		return nil
	}
	
	// Get action from registry
	action, err := e.actionRegistry.Get(actionNode.ActionName)
	if err != nil {
		execNode.State = ActionStateFailed
		execNode.Error = err
		return fmt.Errorf("action not found: %s", actionNode.ActionName)
	}
	
	// Record start of execution
	startTime := time.Now()
	
	// Create action context
	actionCtx := ActionContext[T, S]{
		AncestorTree: e.ancestorTree,
		NodeID:       int(nodeIndex),
		DAG:          e.dag,
		UserContext:  e.sagaContext.ExecContext(),
	}
	
	// Execute the action
	result, err := action.DoIt(ctx, actionCtx)
	endTime := time.Now()
	
	// SEC always sets the timing, overwriting any values from action
	result.StartTime = startTime
	result.EndTime = endTime
	
	// Determine final status and handle result
	var finalStatus ActionState
	if err != nil {
		execNode.State = ActionStateFailed
		execNode.Error = err
		finalStatus = ActionStateFailed
	} else {
		// Store the full result
		execNode.State = ActionStateCompleted
		execNode.Result = &result
		finalStatus = ActionStateCompleted
		
		// Add output to ancestor tree for dependent actions
		if execNode.NodeName != "" {
			e.ancestorTree.Set(execNode.NodeName, result.Output)
		}
	}
	
	// Record execution in trace
	record := ExecutionRecord{
		ActionName: string(actionNode.ActionName),
		NodeID:     nodeIndex,
		StartTime:  startTime,
		EndTime:    endTime,
		Status:     finalStatus,
		Error:      err,
	}
	e.executionTrace = append(e.executionTrace, record)
	
	if err != nil {
		return fmt.Errorf("action %s failed: %w", actionNode.ActionName, err)
	}
	
	return nil
}

// compensate undoes completed actions in reverse order
func (e *SagaExecutor[T, S]) compensate(ctx context.Context) error {
	// Undo completed actions in reverse order
	for i := len(e.completed) - 1; i >= 0; i-- {
		nodeIndex := e.completed[i]
		if err := e.undoNode(ctx, nodeIndex); err != nil {
			return fmt.Errorf("failed to undo node %d: %w", nodeIndex, err)
		}
	}
	return nil
}

// undoNode undoes a single node
func (e *SagaExecutor[T, S]) undoNode(ctx context.Context, nodeIndex int64) error {
	execNode := e.nodes[nodeIndex]
	internalNode := e.dag.Nodes[nodeIndex]
	
	// Update state to undoing
	execNode.State = ActionStateUndoing
	
	// Only handle ActionNodeInternal
	actionNode, ok := internalNode.(*ActionNodeInternal)
	if !ok {
		// Skip non-action nodes
		execNode.State = ActionStateUndone
		return nil
	}
	
	// Get action from registry
	action, err := e.actionRegistry.Get(actionNode.ActionName)
	if err != nil {
		return fmt.Errorf("action not found during undo: %s", actionNode.ActionName)
	}
	
	// Create action context
	actionCtx := ActionContext[T, S]{
		AncestorTree: e.ancestorTree,
		NodeID:       int(nodeIndex),
		DAG:          e.dag,
		UserContext:  e.sagaContext.ExecContext(),
	}
	
	// Execute the undo
	err = action.UndoIt(ctx, actionCtx)
	if err != nil {
		return fmt.Errorf("undo action %s failed: %w", actionNode.ActionName, err)
	}
	
	execNode.State = ActionStateUndone
	return nil
}

// getTopologicalOrder returns nodes in execution order using proper topological sorting
func (e *SagaExecutor[T, S]) getTopologicalOrder() ([]int64, error) {
	// Use gonum's topological sort with stabilized ordering for deterministic results
	sorted, err := topo.SortStabilized(e.dag.Graph, func(nodes []graph.Node) {
		// Sort by node ID for deterministic tie-breaking
		sort.Slice(nodes, func(i, j int) bool {
			return nodes[i].ID() < nodes[j].ID()
		})
	})
	
	if err != nil {
		return nil, fmt.Errorf("topological sort failed (cycle detected?): %w", err)
	}
	
	// Convert from gonum graph.Node to our int64 node IDs
	order := make([]int64, len(sorted))
	for i, node := range sorted {
		order[i] = node.ID()
	}
	
	return order, nil
}

// getExecutionLevels groups nodes into dependency levels for concurrent execution
func (e *SagaExecutor[T, S]) getExecutionLevels() ([][]int64, error) {
	// Build dependency map: nodeID -> set of nodes it depends on
	dependencies := make(map[int64]map[int64]bool)
	
	// Initialize dependency sets for all nodes
	for nodeID := range e.dag.Nodes {
		dependencies[nodeID] = make(map[int64]bool)
	}
	
	// Analyze edges to build dependency relationships
	nodes := e.dag.Graph.Nodes()
	for nodes.Next() {
		node := nodes.Node()
		nodeID := node.ID()
		
		// Find all nodes this node depends on (incoming edges)
		predecessors := e.dag.Graph.To(nodeID)
		for predecessors.Next() {
			predecessor := predecessors.Node()
			dependencies[nodeID][predecessor.ID()] = true
		}
	}
	
	// Group nodes into levels based on their dependencies
	var levels [][]int64
	completed := make(map[int64]bool)
	allNodes := make([]int64, 0, len(dependencies))
	
	// Get all node IDs for iteration
	for nodeID := range dependencies {
		allNodes = append(allNodes, nodeID)
	}
	
	// Keep building levels until all nodes are assigned
	for len(completed) < len(allNodes) {
		var currentLevel []int64
		
		// Find nodes whose dependencies are all completed
		for _, nodeID := range allNodes {
			if completed[nodeID] {
				continue // Already assigned to a level
			}
			
			// Check if all dependencies are satisfied
			canExecute := true
			for depID := range dependencies[nodeID] {
				if !completed[depID] {
					canExecute = false
					break
				}
			}
			
			if canExecute {
				currentLevel = append(currentLevel, nodeID)
			}
		}
		
		// Ensure we're making progress
		if len(currentLevel) == 0 {
			return nil, fmt.Errorf("circular dependency detected or unable to make progress")
		}
		
		// Mark current level nodes as completed for dependency resolution
		for _, nodeID := range currentLevel {
			completed[nodeID] = true
		}
		
		// Sort level for deterministic output
		sort.Slice(currentLevel, func(i, j int) bool {
			return currentLevel[i] < currentLevel[j]
		})
		
		levels = append(levels, currentLevel)
	}
	
	return levels, nil
}

// GetExecutionState returns the current state of all nodes
func (e *SagaExecutor[T, S]) GetExecutionState() map[int64]*ExecutionNode {
	result := make(map[int64]*ExecutionNode)
	for k, v := range e.nodes {
		result[k] = v
	}
	return result
}

// GetCompletedNodes returns the list of completed node indices
func (e *SagaExecutor[T, S]) GetCompletedNodes() []int64 {
	return append([]int64(nil), e.completed...)
}

// GetFailedNodes returns the list of failed node indices  
func (e *SagaExecutor[T, S]) GetFailedNodes() []int64 {
	return append([]int64(nil), e.failed...)
}

// GetExecutionTrace returns the execution trace (copy to avoid external modification)
func (e *SagaExecutor[T, S]) GetExecutionTrace() []ExecutionRecord {
	trace := make([]ExecutionRecord, len(e.executionTrace))
	copy(trace, e.executionTrace)
	return trace
}

// GetExecutionOrder returns just the action names in execution order (for easy testing)
func (e *SagaExecutor[T, S]) GetExecutionOrder() []string {
	order := make([]string, len(e.executionTrace))
	for i, record := range e.executionTrace {
		order[i] = record.ActionName
	}
	return order
}

// Rollback manually triggers compensation to undo all completed actions
// This can be called after a successful execution to deprovision resources
func (e *SagaExecutor[T, S]) Rollback(ctx context.Context) error {
	if len(e.completed) == 0 {
		return fmt.Errorf("no completed actions to rollback")
	}
	
	// Update status to rolling back
	if err := e.persistState(ctx, SagaStatusRollingBack); err != nil {
		fmt.Printf("Warning: failed to persist rollback state: %v\n", err)
	}
	
	// Trigger compensation to undo all completed actions
	err := e.compensate(ctx)
	
	// Update final status
	finalStatus := SagaStatusRolledBack
	if err != nil {
		finalStatus = SagaStatusFailed
	}
	if persistErr := e.persistState(ctx, finalStatus); persistErr != nil {
		fmt.Printf("Warning: failed to persist final rollback state: %v\n", persistErr)
	}
	
	return err
}


// persistState saves the current execution state using our new Store interface
func (e *SagaExecutor[T, S]) persistState(ctx context.Context, status string) error {
	// Build completed actions with their outputs
	completedActions := make([]CompletedAction, 0, len(e.completed))
	
	for _, nodeID := range e.completed {
		node := e.nodes[nodeID]
		if node == nil || node.NodeName == "" {
			continue // Skip unnamed nodes
		}
		
		// Get the output from ancestor tree
		var output json.RawMessage
		if val, ok := e.ancestorTree.Get(node.NodeName); ok && val != nil {
			// Marshal the output to JSON
			data, err := json.Marshal(val)
			if err != nil {
				return fmt.Errorf("failed to marshal action output for %s: %w", node.NodeName, err)
			}
			output = data
		}
		
		// Build completed action with full result data
		ca := CompletedAction{
			Name:   string(node.NodeName),
			Output: output,
		}
		
		// Add timing and metadata if we have the full result
		if node.Result != nil {
			ca.StartTime = node.Result.StartTime
			ca.EndTime = node.Result.EndTime
			ca.Warnings = node.Result.Warnings
			ca.Metrics = node.Result.Metrics
		}
		
		completedActions = append(completedActions, ca)
	}
	
	// Create state
	state := State[T]{
		SagaID:           e.sagaID,
		SagaName:         string(e.dag.SagaName),
		Status:           status,
		Context:          e.sagaContext.ExecContext(),
		CompletedActions: completedActions,
		CreatedAt:        e.startedAt,
		UpdatedAt:        time.Now(),
	}
	
	return e.store.Save(ctx, e.sagaID, state)
}

// NewExecutorFromState creates an executor from a saved state for rollback
func NewExecutorFromState[T any, S SagaType[T]](
	dag *SagaDag,
	registry *ActionRegistry[T, S],
	sagaContext S,
	state *State[T],
	store Store[T],
) *SagaExecutor[T, S] {
	executor := &SagaExecutor[T, S]{
		dag:            dag,
		actionRegistry: registry,
		sagaContext:    sagaContext,
		sagaID:         state.SagaID,
		store:          store,
		nodes:          make(map[int64]*ExecutionNode),
		ancestorTree:   btree.NewMap[NodeName, any](10),
		completed:      make([]int64, 0),
		failed:         make([]int64, 0),
		executionTrace: make([]ExecutionRecord, 0),
		startedAt:      state.CreatedAt,
	}
	
	// Initialize nodes
	executor.initializeNodes()
	
	// Restore completed actions and their outputs
	for _, completedAction := range state.CompletedActions {
		// Find the node by name
		nodeID, err := dag.GetNodeIndex(completedAction.Name)
		if err != nil {
			// Log warning but continue
			fmt.Printf("Warning: completed action %s not found in DAG\n", completedAction.Name)
			continue
		}
		
		// Mark as completed
		executor.completed = append(executor.completed, nodeID)
		if node, ok := executor.nodes[nodeID]; ok {
			node.State = ActionStateCompleted
		}
		
		// Restore output to ancestor tree if present
		// Store as json.RawMessage so LookupTyped can handle unmarshaling to the correct type
		if completedAction.Output != nil {
			executor.ancestorTree.Set(NodeName(completedAction.Name), completedAction.Output)
		}
	}
	
	return executor
}