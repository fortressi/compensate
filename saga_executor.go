package compensate

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tidwall/btree"
	"golang.org/x/sync/errgroup"
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
	
	// Logging
	logger Logger
	
	// Concurrency control
	maxConcurrency int
	mu             sync.Mutex  // Protects shared state
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
		logger:         NopLogger(), // Default to no logging for backward compatibility
		maxConcurrency: runtime.NumCPU(),
	}
	
	// Initialize execution nodes
	executor.initializeNodes()
	
	return executor
}

// Thread-safe state update methods

// addCompleted safely adds a node to the completed list
func (e *SagaExecutor[T, S]) addCompleted(nodeIndex int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.completed = append(e.completed, nodeIndex)
}

// addFailed safely adds a node to the failed list
func (e *SagaExecutor[T, S]) addFailed(nodeIndex int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.failed = append(e.failed, nodeIndex)
}

// updateAncestorTree safely updates the ancestor tree
func (e *SagaExecutor[T, S]) updateAncestorTree(nodeName NodeName, value any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ancestorTree.Set(nodeName, value)
}

// addExecutionRecord safely adds an execution record
func (e *SagaExecutor[T, S]) addExecutionRecord(record ExecutionRecord) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.executionTrace = append(e.executionTrace, record)
}

// createAncestorSnapshot creates a read-only snapshot of the ancestor tree
func (e *SagaExecutor[T, S]) createAncestorSnapshot() *btree.Map[NodeName, any] {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	snapshot := btree.NewMap[NodeName, any](10)
	e.ancestorTree.Scan(func(key NodeName, value any) bool {
		snapshot.Set(key, value)
		return true
	})
	return snapshot
}

// calculateParallelismPotential calculates the potential parallelism in the execution plan
func (e *SagaExecutor[T, S]) calculateParallelismPotential(levels [][]int64) int {
	maxParallel := 0
	for _, level := range levels {
		if len(level) > maxParallel {
			maxParallel = len(level)
		}
	}
	return maxParallel
}

// WithLogger sets the logger for the executor
func (e *SagaExecutor[T, S]) WithLogger(logger Logger) *SagaExecutor[T, S] {
	e.logger = logger.With(
		"saga_id", e.sagaID,
		"saga_name", e.dag.SagaName,
	)
	return e
}

// EnableDefaultLogging enables logging with slog.Default()
func (e *SagaExecutor[T, S]) EnableDefaultLogging() *SagaExecutor[T, S] {
	return e.WithLogger(DefaultLogger())
}

// SetMaxConcurrency allows configuration of concurrency limit
func (e *SagaExecutor[T, S]) SetMaxConcurrency(max int) {
	if max < 1 {
		max = 1
	}
	e.maxConcurrency = max
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
			// Note: executeNode already adds to failed list
			
			// Persist failure state
			if persistErr := e.persistState(ctx, SagaStatusFailed); persistErr != nil {
				// Log persistence error but don't fail the compensation
				e.logger.Warn("failed to persist failure state", "error", persistErr)
			}
			
			if compensationErr := e.compensate(ctx); compensationErr != nil {
				return fmt.Errorf("action failed and compensation failed: action_error=%w, compensation_error=%v", err, compensationErr)
			}
			return fmt.Errorf("saga failed at node %d: %w", nodeIndex, err)
		}
		// Note: executeNode already adds to completed list
		
		// Persist execution state after each node
		if persistErr := e.persistState(ctx, SagaStatusRunning); persistErr != nil {
			// Log persistence error but continue execution
			e.logger.Warn("failed to persist execution state", "error", persistErr)
		}
	}
	
	// Mark saga as completed
	if err := e.persistState(ctx, SagaStatusCompleted); err != nil {
		e.logger.Warn("failed to persist completion state", "error", err)
	}
	
	return nil
}

// ExecuteConcurrent runs the saga with concurrent execution of parallel nodes
func (e *SagaExecutor[T, S]) ExecuteConcurrent(ctx context.Context) error {
	e.logger.Info("starting concurrent saga execution",
		"total_nodes", len(e.dag.Nodes),
		"max_concurrency", e.maxConcurrency,
	)
	startTime := time.Now()
	
	// Save initial state
	if err := e.persistState(ctx, SagaStatusRunning); err != nil {
		e.logger.Error("failed to save initial state", "error", err)
		return fmt.Errorf("failed to save initial state: %w", err)
	}
	
	// Get execution levels
	levels, err := e.getExecutionLevels()
	if err != nil {
		e.logger.Error("failed to get execution levels", "error", err)
		return fmt.Errorf("failed to get execution levels: %w", err)
	}
	
	e.logger.Debug("execution plan determined",
		"levels", len(levels),
		"parallelism_potential", e.calculateParallelismPotential(levels),
	)
	
	// Execute each level
	for levelIndex, level := range levels {
		levelLogger := e.logger.With("level", levelIndex, "nodes", len(level))
		levelLogger.Info("executing level")
		
		if err := e.executeLevel(ctx, level, levelLogger); err != nil {
			levelLogger.Error("level execution failed", "error", err)
			
			// Persist failure state
			if persistErr := e.persistState(ctx, SagaStatusFailed); persistErr != nil {
				e.logger.Warn("failed to persist failure state", "error", persistErr)
			}
			
			// Trigger compensation
			compensationLogger := e.logger.WithGroup("compensation")
			compensationLogger.Info("starting compensation")
			
			if compensationErr := e.compensate(ctx); compensationErr != nil {
				compensationLogger.Error("compensation failed", "error", compensationErr)
				return fmt.Errorf("level %d failed and compensation failed: execution_error=%w, compensation_error=%v", 
					levelIndex, err, compensationErr)
			}
			
			compensationLogger.Info("compensation completed successfully")
			return fmt.Errorf("saga failed at level %d: %w", levelIndex, err)
		}
		
		levelLogger.Info("level completed successfully")
		
		// Persist state after each level
		if persistErr := e.persistState(ctx, SagaStatusRunning); persistErr != nil {
			e.logger.Warn("failed to persist execution state", "error", persistErr)
		}
	}
	
	// Mark saga as completed
	if err := e.persistState(ctx, SagaStatusCompleted); err != nil {
		e.logger.Warn("failed to persist completion state", "error", err)
	}
	
	duration := time.Since(startTime)
	e.logger.Info("saga execution completed",
		"duration_ms", duration.Milliseconds(),
		"status", "success",
	)
	
	return nil
}

// executeLevel executes all nodes in a level concurrently
func (e *SagaExecutor[T, S]) executeLevel(ctx context.Context, nodeIndices []int64, levelLogger Logger) error {
	if len(nodeIndices) == 0 {
		return nil
	}
	
	levelLogger.Debug("starting concurrent execution", 
		"parallelism", min(len(nodeIndices), e.maxConcurrency))
	
	// Create context with cancellation for this level
	levelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	
	// Use errgroup for cleaner goroutine management
	g, gctx := errgroup.WithContext(levelCtx)
	g.SetLimit(e.maxConcurrency)
	
	// Atomic counter for active goroutines (for debugging)
	var activeGoroutines int32
	
	for _, nodeIndex := range nodeIndices {
		nodeIndex := nodeIndex // capture loop variable
		
		g.Go(func() error {
			goroutineID := atomic.AddInt32(&activeGoroutines, 1)
			goroutineLogger := levelLogger.With("goroutine", goroutineID)
			defer atomic.AddInt32(&activeGoroutines, -1)
			
			goroutineLogger.Debug("goroutine started", "node_id", nodeIndex)
			
			err := e.executeNodeConcurrent(gctx, nodeIndex, goroutineLogger)
			
			if err != nil {
				goroutineLogger.Error("goroutine failed", "error", err)
				return err
			}
			
			goroutineLogger.Debug("goroutine completed")
			return nil
		})
	}
	
	// Wait for all goroutines
	err := g.Wait()
	
	if err != nil {
		levelLogger.Error("level execution failed", 
			"error", err,
			"active_goroutines", atomic.LoadInt32(&activeGoroutines))
		return err
	}
	
	levelLogger.Debug("all goroutines completed successfully")
	return nil
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
		e.addCompleted(nodeIndex)
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
		e.addFailed(nodeIndex)
	} else {
		// Store the full result
		execNode.State = ActionStateCompleted
		execNode.Result = &result
		finalStatus = ActionStateCompleted
		e.addCompleted(nodeIndex)
		
		// Add output to ancestor tree for dependent actions
		if execNode.NodeName != "" {
			e.updateAncestorTree(execNode.NodeName, result.Output)
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
	e.addExecutionRecord(record)
	
	if err != nil {
		return fmt.Errorf("action %s failed: %w", actionNode.ActionName, err)
	}
	
	return nil
}

// executeNodeConcurrent executes a single node (thread-safe version)
func (e *SagaExecutor[T, S]) executeNodeConcurrent(ctx context.Context, nodeIndex int64, logger Logger) error {
	execNode := e.nodes[nodeIndex]
	internalNode := e.dag.Nodes[nodeIndex]
	
	nodeLogger := logger.With(
		"node_id", nodeIndex,
		"node_name", execNode.NodeName,
	)
	
	// Update state to running
	execNode.State = ActionStateRunning
	nodeLogger.Debug("node execution started")
	
	// Only handle ActionNodeInternal for now
	actionNode, ok := internalNode.(*ActionNodeInternal)
	if !ok {
		nodeLogger.Debug("skipping non-action node")
		execNode.State = ActionStateCompleted
		e.addCompleted(nodeIndex)
		return nil
	}
	
	actionLogger := nodeLogger.With("action_name", actionNode.ActionName)
	
	// Get action from registry
	action, err := e.actionRegistry.Get(actionNode.ActionName)
	if err != nil {
		actionLogger.Error("action not found in registry", "error", err)
		execNode.State = ActionStateFailed
		execNode.Error = err
		e.addFailed(nodeIndex)
		return fmt.Errorf("action not found: %s", actionNode.ActionName)
	}
	
	// Record start of execution
	startTime := time.Now()
	actionLogger.Debug("executing action")
	
	// Create action context with a snapshot of ancestor tree
	// This ensures consistent view during concurrent execution
	ancestorSnapshot := e.createAncestorSnapshot()
	
	actionCtx := ActionContext[T, S]{
		AncestorTree: ancestorSnapshot,
		NodeID:       int(nodeIndex),
		DAG:          e.dag,
		UserContext:  e.sagaContext.ExecContext(),
	}
	
	// Execute the action
	result, err := action.DoIt(ctx, actionCtx)
	endTime := time.Now()
	
	// SEC always sets the timing
	result.StartTime = startTime
	result.EndTime = endTime
	
	duration := time.Since(startTime)
	
	// Determine final status and handle result
	var finalStatus ActionState
	if err != nil {
		actionLogger.Error("action execution failed",
			"error", err,
			"duration_ms", duration.Milliseconds(),
		)
		execNode.State = ActionStateFailed
		execNode.Error = err
		finalStatus = ActionStateFailed
		e.addFailed(nodeIndex)
	} else {
		actionLogger.Info("action executed successfully",
			"duration_ms", duration.Milliseconds(),
			"has_output", result.Output != nil,
		)
		
		// Store the full result
		execNode.State = ActionStateCompleted
		execNode.Result = &result
		finalStatus = ActionStateCompleted
		e.addCompleted(nodeIndex)
		
		// Add output to ancestor tree for dependent actions
		if execNode.NodeName != "" {
			e.updateAncestorTree(execNode.NodeName, result.Output)
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
	e.addExecutionRecord(record)
	
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
		e.logger.Warn("failed to persist rollback state", "error", err)
	}
	
	// Trigger compensation to undo all completed actions
	err := e.compensate(ctx)
	
	// Update final status
	finalStatus := SagaStatusRolledBack
	if err != nil {
		finalStatus = SagaStatusFailed
	}
	if persistErr := e.persistState(ctx, finalStatus); persistErr != nil {
		e.logger.Warn("failed to persist final rollback state", "error", persistErr)
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
			// Note: NewExecutorFromState doesn't have a logger yet, so we skip logging here
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