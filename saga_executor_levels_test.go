package compensate

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test state for level detection testing
type LevelTestState struct {
	ID string `json:"id"`
}

type LevelTestSaga struct {
	State *LevelTestState
}

func (s *LevelTestSaga) ExecContext() *LevelTestState {
	return s.State
}

func TestGetExecutionLevelsLinear(t *testing.T) {
	// Test linear DAG: A -> B -> C
	sagaState := &LevelTestState{ID: "linear-test"}
	sagaContext := &LevelTestSaga{State: sagaState}
	registry := NewActionRegistry[*LevelTestState, *LevelTestSaga]()

	// Create actions
	actionA := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"action_a",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "action_a"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)
	
	actionB := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"action_b",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "action_b"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)
	
	actionC := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"action_c",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "action_c"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)

	// Register actions
	registry.Register(actionA)
	registry.Register(actionB)
	registry.Register(actionC)

	// Build linear DAG
	builder := NewDagBuilder[*LevelTestState, *LevelTestSaga]("LinearTest", registry)
	
	err := builder.Append(&ActionNodeKind[*LevelTestState, *LevelTestSaga]{
		NodeName: "actionA",
		Label:    "Action A",
		Action:   actionA,
	})
	require.NoError(t, err)
	
	err = builder.Append(&ActionNodeKind[*LevelTestState, *LevelTestSaga]{
		NodeName: "actionB", 
		Label:    "Action B",
		Action:   actionB,
	})
	require.NoError(t, err)
	
	err = builder.Append(&ActionNodeKind[*LevelTestState, *LevelTestSaga]{
		NodeName: "actionC",
		Label:    "Action C", 
		Action:   actionC,
	})
	require.NoError(t, err)

	// Build DAG and create executor
	dag, err := builder.Build()
	require.NoError(t, err)

	sagaDag := NewSagaDag(dag, json.RawMessage(`{}`))
	// Create executor with memory store for testing
	store := NewMemoryStore[*LevelTestState]()
	sagaID := "test-saga-1"
	executor := NewSagaExecutor(sagaDag, registry, sagaContext, sagaID, store)

	// Get execution levels
	levels, err := executor.getExecutionLevels()
	require.NoError(t, err)

	// Verify level structure for linear DAG
	// Should have levels: [start], [actionA], [actionB], [actionC], [end]
	t.Logf("Linear DAG levels: %v", levels)
	
	require.True(t, len(levels) >= 3, "should have at least 3 levels for A->B->C")
	
	// Find the levels containing our named actions
	actionALevel := findNodeInLevels(t, sagaDag, "actionA", levels)
	actionBLevel := findNodeInLevels(t, sagaDag, "actionB", levels)
	actionCLevel := findNodeInLevels(t, sagaDag, "actionC", levels)
	
	// Verify sequential ordering
	assert.True(t, actionALevel < actionBLevel, "actionA should be in earlier level than actionB")
	assert.True(t, actionBLevel < actionCLevel, "actionB should be in earlier level than actionC")
	
	// Each level should have exactly one action node (plus potentially start/end nodes)
	actionNodesInLevelA := countActionNodesInLevel(sagaDag, levels[actionALevel])
	actionNodesInLevelB := countActionNodesInLevel(sagaDag, levels[actionBLevel])  
	actionNodesInLevelC := countActionNodesInLevel(sagaDag, levels[actionCLevel])
	
	assert.Equal(t, 1, actionNodesInLevelA, "actionA level should have 1 action node")
	assert.Equal(t, 1, actionNodesInLevelB, "actionB level should have 1 action node")
	assert.Equal(t, 1, actionNodesInLevelC, "actionC level should have 1 action node")
}

func TestGetExecutionLevelsParallel(t *testing.T) {
	// Test parallel DAG: A -> [B, C] -> D
	sagaState := &LevelTestState{ID: "parallel-test"}
	sagaContext := &LevelTestSaga{State: sagaState}
	registry := NewActionRegistry[*LevelTestState, *LevelTestSaga]()

	// Create actions
	actionA := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"action_a",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "action_a"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)
	
	actionB := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"action_b",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "action_b"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)
	
	actionC := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"action_c",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "action_c"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)
	
	actionD := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"action_d",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "action_d"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)

	// Register actions
	registry.Register(actionA)
	registry.Register(actionB)
	registry.Register(actionC)
	registry.Register(actionD)

	// Build parallel DAG
	builder := NewDagBuilder[*LevelTestState, *LevelTestSaga]("ParallelTest", registry)
	
	err := builder.Append(&ActionNodeKind[*LevelTestState, *LevelTestSaga]{
		NodeName: "actionA",
		Label:    "Action A",
		Action:   actionA,
	})
	require.NoError(t, err)
	
	err = builder.AppendParallel(
		&ActionNodeKind[*LevelTestState, *LevelTestSaga]{
			NodeName: "actionB",
			Label:    "Action B",
			Action:   actionB,
		},
		&ActionNodeKind[*LevelTestState, *LevelTestSaga]{
			NodeName: "actionC",
			Label:    "Action C",
			Action:   actionC,
		},
	)
	require.NoError(t, err)
	
	err = builder.Append(&ActionNodeKind[*LevelTestState, *LevelTestSaga]{
		NodeName: "actionD",
		Label:    "Action D",
		Action:   actionD,
	})
	require.NoError(t, err)

	// Build DAG and create executor
	dag, err := builder.Build()
	require.NoError(t, err)

	sagaDag := NewSagaDag(dag, json.RawMessage(`{}`))
	// Create executor with memory store for testing
	store := NewMemoryStore[*LevelTestState]()
	sagaID := "test-saga-2"
	executor := NewSagaExecutor(sagaDag, registry, sagaContext, sagaID, store)

	// Get execution levels
	levels, err := executor.getExecutionLevels()
	require.NoError(t, err)

	t.Logf("Parallel DAG levels: %v", levels)
	
	// Find levels for each action
	actionALevel := findNodeInLevels(t, sagaDag, "actionA", levels)
	actionBLevel := findNodeInLevels(t, sagaDag, "actionB", levels)
	actionCLevel := findNodeInLevels(t, sagaDag, "actionC", levels)
	actionDLevel := findNodeInLevels(t, sagaDag, "actionD", levels)
	
	// Verify parallel structure: A < [B,C] < D
	assert.True(t, actionALevel < actionBLevel, "actionA should be before actionB")
	assert.True(t, actionALevel < actionCLevel, "actionA should be before actionC")
	assert.Equal(t, actionBLevel, actionCLevel, "actionB and actionC should be in same level (parallel)")
	assert.True(t, actionBLevel < actionDLevel, "actionB should be before actionD")
	assert.True(t, actionCLevel < actionDLevel, "actionC should be before actionD")
	
	// Verify the parallel level has exactly 2 action nodes
	parallelLevelActions := countActionNodesInLevel(sagaDag, levels[actionBLevel])
	assert.Equal(t, 2, parallelLevelActions, "parallel level should have exactly 2 action nodes")
	
	// Verify other levels have 1 action node each
	assert.Equal(t, 1, countActionNodesInLevel(sagaDag, levels[actionALevel]), "actionA level should have 1 action node")
	assert.Equal(t, 1, countActionNodesInLevel(sagaDag, levels[actionDLevel]), "actionD level should have 1 action node")
}

func TestGetExecutionLevelsThreeWayParallel(t *testing.T) {
	// Test three-way parallel: setup -> [taskA, taskB, taskC] -> cleanup
	sagaState := &LevelTestState{ID: "threeway-test"}
	sagaContext := &LevelTestSaga{State: sagaState}
	registry := NewActionRegistry[*LevelTestState, *LevelTestSaga]()

	// Create actions
	setupAction := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"setup_action",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "setup_action"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)
	
	taskA := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"task_a",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "task_a"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)
	
	taskB := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"task_b",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "task_b"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)
	
	taskC := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"task_c",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "task_c"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)
	
	cleanupAction := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"cleanup_action",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "cleanup_action"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)

	// Register actions
	registry.Register(setupAction)
	registry.Register(taskA)
	registry.Register(taskB)
	registry.Register(taskC)
	registry.Register(cleanupAction)

	// Build three-way parallel DAG
	builder := NewDagBuilder[*LevelTestState, *LevelTestSaga]("ThreeWayTest", registry)
	
	err := builder.Append(&ActionNodeKind[*LevelTestState, *LevelTestSaga]{
		NodeName: "setup",
		Label:    "Setup",
		Action:   setupAction,
	})
	require.NoError(t, err)
	
	err = builder.AppendParallel(
		&ActionNodeKind[*LevelTestState, *LevelTestSaga]{
			NodeName: "taskA",
			Label:    "Task A",
			Action:   taskA,
		},
		&ActionNodeKind[*LevelTestState, *LevelTestSaga]{
			NodeName: "taskB", 
			Label:    "Task B",
			Action:   taskB,
		},
		&ActionNodeKind[*LevelTestState, *LevelTestSaga]{
			NodeName: "taskC",
			Label:    "Task C",
			Action:   taskC,
		},
	)
	require.NoError(t, err)
	
	err = builder.Append(&ActionNodeKind[*LevelTestState, *LevelTestSaga]{
		NodeName: "cleanup",
		Label:    "Cleanup",
		Action:   cleanupAction,
	})
	require.NoError(t, err)

	// Build DAG and create executor
	dag, err := builder.Build()
	require.NoError(t, err)

	sagaDag := NewSagaDag(dag, json.RawMessage(`{}`))
	// Create executor with memory store for testing
	store := NewMemoryStore[*LevelTestState]()
	sagaID := "test-saga-3"
	executor := NewSagaExecutor(sagaDag, registry, sagaContext, sagaID, store)

	// Get execution levels
	levels, err := executor.getExecutionLevels()
	require.NoError(t, err)

	t.Logf("Three-way parallel DAG levels: %v", levels)
	
	// Find levels
	setupLevel := findNodeInLevels(t, sagaDag, "setup", levels)
	taskALevel := findNodeInLevels(t, sagaDag, "taskA", levels)
	taskBLevel := findNodeInLevels(t, sagaDag, "taskB", levels)
	taskCLevel := findNodeInLevels(t, sagaDag, "taskC", levels)
	cleanupLevel := findNodeInLevels(t, sagaDag, "cleanup", levels)
	
	// Verify structure: setup < [taskA, taskB, taskC] < cleanup
	assert.True(t, setupLevel < taskALevel, "setup should be before taskA")
	assert.True(t, setupLevel < taskBLevel, "setup should be before taskB")
	assert.True(t, setupLevel < taskCLevel, "setup should be before taskC")
	
	assert.Equal(t, taskALevel, taskBLevel, "taskA and taskB should be in same level")
	assert.Equal(t, taskBLevel, taskCLevel, "taskB and taskC should be in same level")
	
	assert.True(t, taskALevel < cleanupLevel, "taskA should be before cleanup")
	assert.True(t, taskBLevel < cleanupLevel, "taskB should be before cleanup")
	assert.True(t, taskCLevel < cleanupLevel, "taskC should be before cleanup")
	
	// Verify parallel level has exactly 3 action nodes
	parallelLevelActions := countActionNodesInLevel(sagaDag, levels[taskALevel])
	assert.Equal(t, 3, parallelLevelActions, "parallel level should have exactly 3 action nodes")
}

func TestGetExecutionLevelsComplex(t *testing.T) {
	// Test complex DAG: A -> [B, C] -> [D, E] -> F
	// Where D depends on B, E depends on C
	sagaState := &LevelTestState{ID: "complex-test"}
	sagaContext := &LevelTestSaga{State: sagaState}
	registry := NewActionRegistry[*LevelTestState, *LevelTestSaga]()

	// Create actions
	actionA := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"action_a",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "action_a"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)
	
	actionB := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"action_b",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "action_b"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)
	
	actionC := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"action_c",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "action_c"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)
	
	actionD := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"action_d",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "action_d"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)
	
	actionE := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"action_e",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "action_e"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)
	
	actionF := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"action_f",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "action_f"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)

	// Register actions
	registry.Register(actionA)
	registry.Register(actionB)
	registry.Register(actionC)
	registry.Register(actionD)
	registry.Register(actionE)
	registry.Register(actionF)

	// Build complex DAG
	builder := NewDagBuilder[*LevelTestState, *LevelTestSaga]("ComplexTest", registry)
	
	// Level 1: A
	err := builder.Append(&ActionNodeKind[*LevelTestState, *LevelTestSaga]{
		NodeName: "actionA",
		Label:    "Action A",
		Action:   actionA,
	})
	require.NoError(t, err)
	
	// Level 2: [B, C] 
	err = builder.AppendParallel(
		&ActionNodeKind[*LevelTestState, *LevelTestSaga]{
			NodeName: "actionB",
			Label:    "Action B",
			Action:   actionB,
		},
		&ActionNodeKind[*LevelTestState, *LevelTestSaga]{
			NodeName: "actionC",
			Label:    "Action C", 
			Action:   actionC,
		},
	)
	require.NoError(t, err)
	
	// Level 3: [D, E] - both depend on B and C
	err = builder.AppendParallel(
		&ActionNodeKind[*LevelTestState, *LevelTestSaga]{
			NodeName: "actionD",
			Label:    "Action D",
			Action:   actionD,
		},
		&ActionNodeKind[*LevelTestState, *LevelTestSaga]{
			NodeName: "actionE",
			Label:    "Action E",
			Action:   actionE,
		},
	)
	require.NoError(t, err)
	
	// Level 4: F
	err = builder.Append(&ActionNodeKind[*LevelTestState, *LevelTestSaga]{
		NodeName: "actionF",
		Label:    "Action F",
		Action:   actionF,
	})
	require.NoError(t, err)

	// Build DAG and create executor
	dag, err := builder.Build()
	require.NoError(t, err)

	sagaDag := NewSagaDag(dag, json.RawMessage(`{}`))
	// Create executor with memory store for testing
	store := NewMemoryStore[*LevelTestState]()
	sagaID := "test-saga-4"
	executor := NewSagaExecutor(sagaDag, registry, sagaContext, sagaID, store)

	// Get execution levels
	levels, err := executor.getExecutionLevels()
	require.NoError(t, err)

	t.Logf("Complex DAG levels: %v", levels)
	
	// Find levels
	levelA := findNodeInLevels(t, sagaDag, "actionA", levels)
	levelB := findNodeInLevels(t, sagaDag, "actionB", levels)  
	levelC := findNodeInLevels(t, sagaDag, "actionC", levels)
	levelD := findNodeInLevels(t, sagaDag, "actionD", levels)
	levelE := findNodeInLevels(t, sagaDag, "actionE", levels)
	levelF := findNodeInLevels(t, sagaDag, "actionF", levels)
	
	// Verify level progression: A < [B,C] < [D,E] < F
	assert.True(t, levelA < levelB, "A should be before B")
	assert.True(t, levelA < levelC, "A should be before C")
	assert.Equal(t, levelB, levelC, "B and C should be in same level")
	
	assert.True(t, levelB < levelD, "B should be before D")
	assert.True(t, levelC < levelE, "C should be before E") 
	assert.Equal(t, levelD, levelE, "D and E should be in same level")
	
	assert.True(t, levelD < levelF, "D should be before F")
	assert.True(t, levelE < levelF, "E should be before F")
	
	// Verify each parallel level has correct number of actions
	assert.Equal(t, 1, countActionNodesInLevel(sagaDag, levels[levelA]), "level A should have 1 action")
	assert.Equal(t, 2, countActionNodesInLevel(sagaDag, levels[levelB]), "level B/C should have 2 actions")
	assert.Equal(t, 2, countActionNodesInLevel(sagaDag, levels[levelD]), "level D/E should have 2 actions")
	assert.Equal(t, 1, countActionNodesInLevel(sagaDag, levels[levelF]), "level F should have 1 action")
}

// Helper function to find which level contains a named node
func findNodeInLevels(t *testing.T, sagaDag *SagaDag, nodeName string, levels [][]int64) int {
	nodeID, err := sagaDag.GetNodeIndex(nodeName)
	require.NoError(t, err, "should find node %s", nodeName)
	
	for levelIndex, level := range levels {
		for _, nodeInLevel := range level {
			if nodeInLevel == nodeID {
				return levelIndex
			}
		}
	}
	
	t.Fatalf("node %s (ID %d) not found in any level", nodeName, nodeID)
	return -1
}

// Helper function to count action nodes in a level (excluding start/end nodes)
func countActionNodesInLevel(sagaDag *SagaDag, level []int64) int {
	count := 0
	for _, nodeID := range level {
		if node, err := sagaDag.GetNode(nodeID); err == nil {
			// Only count actual action nodes, not start/end nodes
			if _, isAction := node.(*ActionNodeInternal); isAction {
				count++
			}
		}
	}
	return count
}