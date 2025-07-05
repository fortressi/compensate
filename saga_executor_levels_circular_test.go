package compensate

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetExecutionLevelsDetectsCircularDependency(t *testing.T) {
	// This test verifies that our level detection algorithm can detect circular dependencies
	// We'll create a circular dependency by manually manipulating the graph
	
	sagaState := &LevelTestState{ID: "circular-test"}
	sagaContext := &LevelTestSaga{State: sagaState}
	registry := NewActionRegistry[*LevelTestState, *LevelTestSaga]()

	// Create simple actions
	actionA := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"action_a",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionResult[*SimpleResult], error) {
			return ActionResult[*SimpleResult]{Output: &SimpleResult{Value: "action_a"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)
	
	actionB := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"action_b",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionResult[*SimpleResult], error) {
			return ActionResult[*SimpleResult]{Output: &SimpleResult{Value: "action_b"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)
	
	actionC := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"action_c",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionResult[*SimpleResult], error) {
			return ActionResult[*SimpleResult]{Output: &SimpleResult{Value: "action_c"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)

	// Register actions
	registry.Register(actionA)
	registry.Register(actionB)
	registry.Register(actionC)

	// Build a normal DAG first: A -> B -> C
	builder := NewDagBuilder[*LevelTestState, *LevelTestSaga]("CircularTest", registry)
	
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

	// Build DAG and create SagaDag
	dag, err := builder.Build()
	require.NoError(t, err)

	sagaDag := NewSagaDag(dag, json.RawMessage(`{}`))
	
	// Now manually create a circular dependency by adding an edge from C back to A
	// This simulates what could happen if someone built a circular DAG
	actionAID, err := sagaDag.GetNodeIndex("actionA")
	require.NoError(t, err)
	actionCID, err := sagaDag.GetNodeIndex("actionC")
	require.NoError(t, err)
	
	// Add edge C -> A to create cycle: A -> B -> C -> A
	// Use simple.Edge to create the edge
	nodeA := sagaDag.Graph.Node(actionAID)
	nodeC := sagaDag.Graph.Node(actionCID)
	sagaDag.Graph.SetEdge(sagaDag.Graph.NewEdge(nodeC, nodeA))
	
	// Create executor
	// Create executor with memory store for testing
	store := NewMemoryStore[*LevelTestState]()
	sagaID := "test-saga-1"
	executor := NewSagaExecutor(sagaDag, registry, sagaContext, sagaID, store)

	// Get execution levels - should detect circular dependency
	levels, err := executor.getExecutionLevels()
	
	// Should return an error about circular dependency
	assert.Error(t, err, "should detect circular dependency")
	assert.Contains(t, err.Error(), "circular dependency", "error should mention circular dependency")
	assert.Nil(t, levels, "should not return levels when circular dependency detected")
	
	t.Logf("Correctly detected circular dependency: %v", err)
}

func TestGetExecutionLevelsEmptyDAG(t *testing.T) {
	// Test edge case of empty DAG (only start/end nodes)
	sagaState := &LevelTestState{ID: "empty-test"}
	sagaContext := &LevelTestSaga{State: sagaState}
	registry := NewActionRegistry[*LevelTestState, *LevelTestSaga]()

	// Create dummy action
	dummyAction := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"dummy_action",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionResult[*SimpleResult], error) {
			return ActionResult[*SimpleResult]{Output: &SimpleResult{Value: "dummy_action"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)
	registry.Register(dummyAction)

	// Build empty DAG - just create a DAG without any actions
	builder := NewDagBuilder[*LevelTestState, *LevelTestSaga]("EmptyTest", registry)
	
	// Add a single dummy action to satisfy DAG builder requirements
	err := builder.Append(&ActionNodeKind[*LevelTestState, *LevelTestSaga]{
		NodeName: "dummy",
		Label:    "Dummy Action",
		Action:   dummyAction,
	})
	require.NoError(t, err)

	dag, err := builder.Build()
	require.NoError(t, err)

	sagaDag := NewSagaDag(dag, json.RawMessage(`{}`))
	// Create executor with memory store for testing
	store := NewMemoryStore[*LevelTestState]()
	sagaID := "test-saga-2"
	executor := NewSagaExecutor(sagaDag, registry, sagaContext, sagaID, store)

	// Get execution levels - should work fine
	levels, err := executor.getExecutionLevels()
	require.NoError(t, err, "empty DAG should not cause errors")
	
	// Should have levels for start, dummy action, and end
	assert.True(t, len(levels) >= 3, "should have at least 3 levels (start, action, end)")
	
	t.Logf("Empty DAG levels: %v", levels)
}

func TestGetExecutionLevelsSingleAction(t *testing.T) {
	// Test simple case of single action
	sagaState := &LevelTestState{ID: "single-test"}
	sagaContext := &LevelTestSaga{State: sagaState}
	registry := NewActionRegistry[*LevelTestState, *LevelTestSaga]()

	// Create only action
	onlyAction := NewActionFunc[*LevelTestState, *LevelTestSaga, *SimpleResult](
		"only_action",
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) (ActionResult[*SimpleResult], error) {
			return ActionResult[*SimpleResult]{Output: &SimpleResult{Value: "only_action"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*LevelTestState, *LevelTestSaga]) error {
			return nil
		},
	)
	registry.Register(onlyAction)

	// Build single action DAG
	builder := NewDagBuilder[*LevelTestState, *LevelTestSaga]("SingleTest", registry)
	
	err := builder.Append(&ActionNodeKind[*LevelTestState, *LevelTestSaga]{
		NodeName: "onlyAction",
		Label:    "Only Action",
		Action:   onlyAction,
	})
	require.NoError(t, err)

	dag, err := builder.Build()
	require.NoError(t, err)

	sagaDag := NewSagaDag(dag, json.RawMessage(`{}`))
	// Create executor with memory store for testing
	store := NewMemoryStore[*LevelTestState]()
	sagaID := "test-saga-3"
	executor := NewSagaExecutor(sagaDag, registry, sagaContext, sagaID, store)

	// Get execution levels
	levels, err := executor.getExecutionLevels()
	require.NoError(t, err, "single action DAG should work")
	
	// Find the action level
	actionLevel := findNodeInLevels(t, sagaDag, "onlyAction", levels)
	
	// Verify there's exactly 1 action in that level
	actionCount := countActionNodesInLevel(sagaDag, levels[actionLevel])
	assert.Equal(t, 1, actionCount, "should have exactly 1 action in the action level")
	
	t.Logf("Single action DAG levels: %v", levels)
}