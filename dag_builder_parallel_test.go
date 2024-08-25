package compensate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gonum.org/v1/gonum/graph"
)

// Test types are shared from dag_builder_new_test.go

func TestAppendParallelVariadic(t *testing.T) {
	// Create action registry
	registry := NewActionRegistry[*NewTestState, *NewTestSaga]()
	builder := NewDagBuilder[*NewTestState, *NewTestSaga]("ParallelTest", registry)

	// Create test actions
	validateAction := NewActionFunc[*NewTestState, *NewTestSaga, *SimpleResult](
		"validate_action",
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "validated"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) error {
			return nil
		},
	)

	paymentAction := NewActionFunc[*NewTestState, *NewTestSaga, *SimpleResult](
		"payment_action",
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "paid"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) error {
			return nil
		},
	)

	inventoryAction := NewActionFunc[*NewTestState, *NewTestSaga, *SimpleResult](
		"inventory_action",
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "reserved"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) error {
			return nil
		},
	)

	confirmAction := NewActionFunc[*NewTestState, *NewTestSaga, *SimpleResult](
		"confirm_action",
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "confirmed"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) error {
			return nil
		},
	)

	// Level 1: Single action
	err := builder.Append(&ActionNodeKind[*NewTestState, *NewTestSaga]{
		NodeName: "validate",
		Label:    "Validate Order",
		Action:   validateAction,
	})
	require.NoError(t, err)

	// Level 2: Two parallel actions
	err = builder.AppendParallel(
		&ActionNodeKind[*NewTestState, *NewTestSaga]{
			NodeName: "payment",
			Label:    "Process Payment",
			Action:   paymentAction,
		},
		&ActionNodeKind[*NewTestState, *NewTestSaga]{
			NodeName: "inventory",
			Label:    "Check Inventory",
			Action:   inventoryAction,
		},
	)
	require.NoError(t, err)

	// Level 3: Single action depending on both
	err = builder.Append(&ActionNodeKind[*NewTestState, *NewTestSaga]{
		NodeName: "confirm",
		Label:    "Send Confirmation",
		Action:   confirmAction,
	})
	require.NoError(t, err)

	// Build DAG
	dag, err := builder.Build()
	require.NoError(t, err)

	// Verify dependency structure
	validateNodeIndex, err := dag.getNodeIndexByName("validate")
	require.NoError(t, err)

	paymentNodeIndex, err := dag.getNodeIndexByName("payment")
	require.NoError(t, err)

	inventoryNodeIndex, err := dag.getNodeIndexByName("inventory")
	require.NoError(t, err)

	confirmNodeIndex, err := dag.getNodeIndexByName("confirm")
	require.NoError(t, err)

	// Check dependencies using the underlying graph
	graph := dag.Graph

	// Validate has no incoming edges (except potentially from start node)
	validatePredecessors := graph.To(validateNodeIndex)
	predecessorCount := 0
	for validatePredecessors.Next() {
		predecessorCount++
	}
	// Should have at most 1 predecessor (start node, if it exists)
	assert.LessOrEqual(t, predecessorCount, 1, "validate should have at most 1 predecessor")

	// Payment should depend on validate
	paymentPredecessors := graph.To(paymentNodeIndex)
	paymentDeps := collectNodeIDs(paymentPredecessors)
	assert.Contains(t, paymentDeps, validateNodeIndex, "payment should depend on validate")

	// Inventory should depend on validate
	inventoryPredecessors := graph.To(inventoryNodeIndex)
	inventoryDeps := collectNodeIDs(inventoryPredecessors)
	assert.Contains(t, inventoryDeps, validateNodeIndex, "inventory should depend on validate")

	// Confirm should depend on both payment and inventory
	confirmPredecessors := graph.To(confirmNodeIndex)
	confirmDeps := collectNodeIDs(confirmPredecessors)
	assert.Contains(t, confirmDeps, paymentNodeIndex, "confirm should depend on payment")
	assert.Contains(t, confirmDeps, inventoryNodeIndex, "confirm should depend on inventory")
	assert.Equal(t, 2, len(confirmDeps), "confirm should have exactly 2 dependencies")

	t.Logf("Dependency structure verified:")
	t.Logf("  validate (%d) -> [payment (%d), inventory (%d)] -> confirm (%d)",
		validateNodeIndex, paymentNodeIndex, inventoryNodeIndex, confirmNodeIndex)
}

func TestAppendParallelThreeWay(t *testing.T) {
	// Create action registry
	registry := NewActionRegistry[*NewTestState, *NewTestSaga]()
	builder := NewDagBuilder[*NewTestState, *NewTestSaga]("ThreeWayParallel", registry)

	// Create test actions
	setupAction := NewActionFunc[*NewTestState, *NewTestSaga, *SimpleResult](
		"setup_action",
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "setup"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) error {
			return nil
		},
	)

	actionA := NewActionFunc[*NewTestState, *NewTestSaga, *SimpleResult](
		"action_a",
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "a"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) error {
			return nil
		},
	)

	actionB := NewActionFunc[*NewTestState, *NewTestSaga, *SimpleResult](
		"action_b",
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "b"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) error {
			return nil
		},
	)

	actionC := NewActionFunc[*NewTestState, *NewTestSaga, *SimpleResult](
		"action_c",
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "c"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) error {
			return nil
		},
	)

	cleanupAction := NewActionFunc[*NewTestState, *NewTestSaga, *SimpleResult](
		"cleanup_action",
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "cleanup"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) error {
			return nil
		},
	)

	// Level 1: Single setup action
	err := builder.Append(&ActionNodeKind[*NewTestState, *NewTestSaga]{
		NodeName: "setup",
		Label:    "Setup Resources",
		Action:   setupAction,
	})
	require.NoError(t, err)

	// Level 2: Three parallel actions
	err = builder.AppendParallel(
		&ActionNodeKind[*NewTestState, *NewTestSaga]{
			NodeName: "taskA",
			Label:    "Task A",
			Action:   actionA,
		},
		&ActionNodeKind[*NewTestState, *NewTestSaga]{
			NodeName: "taskB",
			Label:    "Task B",
			Action:   actionB,
		},
		&ActionNodeKind[*NewTestState, *NewTestSaga]{
			NodeName: "taskC",
			Label:    "Task C",
			Action:   actionC,
		},
	)
	require.NoError(t, err)

	// Level 3: Cleanup action depending on all three
	err = builder.Append(&ActionNodeKind[*NewTestState, *NewTestSaga]{
		NodeName: "cleanup",
		Label:    "Cleanup Resources",
		Action:   cleanupAction,
	})
	require.NoError(t, err)

	// Build and verify
	dag, err := builder.Build()
	require.NoError(t, err)

	setupIndex, err := dag.getNodeIndexByName("setup")
	require.NoError(t, err)

	taskAIndex, err := dag.getNodeIndexByName("taskA")
	require.NoError(t, err)

	taskBIndex, err := dag.getNodeIndexByName("taskB")
	require.NoError(t, err)

	taskCIndex, err := dag.getNodeIndexByName("taskC")
	require.NoError(t, err)

	cleanupIndex, err := dag.getNodeIndexByName("cleanup")
	require.NoError(t, err)

	graph := dag.Graph

	// All parallel tasks should depend on setup
	for taskName, taskIndex := range map[string]int64{
		"taskA": taskAIndex,
		"taskB": taskBIndex,
		"taskC": taskCIndex,
	} {
		predecessors := graph.To(taskIndex)
		deps := collectNodeIDs(predecessors)
		assert.Contains(t, deps, setupIndex, "%s should depend on setup", taskName)
	}

	// Cleanup should depend on all three tasks
	cleanupPredecessors := graph.To(cleanupIndex)
	cleanupDeps := collectNodeIDs(cleanupPredecessors)
	assert.Contains(t, cleanupDeps, taskAIndex, "cleanup should depend on taskA")
	assert.Contains(t, cleanupDeps, taskBIndex, "cleanup should depend on taskB")
	assert.Contains(t, cleanupDeps, taskCIndex, "cleanup should depend on taskC")
	assert.Equal(t, 3, len(cleanupDeps), "cleanup should have exactly 3 dependencies")

	t.Logf("Three-way parallel structure verified:")
	t.Logf("  setup -> [taskA, taskB, taskC] -> cleanup")
}

func TestAppendParallelEmpty(t *testing.T) {
	registry := NewActionRegistry[*NewTestState, *NewTestSaga]()
	builder := NewDagBuilder[*NewTestState, *NewTestSaga]("EmptyParallelTest", registry)

	// Should fail with empty parallel stage
	err := builder.AppendParallel()
	assert.Error(t, err, "empty parallel stage should be rejected")
	assert.Contains(t, err.Error(), "empty stage", "error should mention empty stage")
}

func TestAppendParallelDuplicateNames(t *testing.T) {
	registry := NewActionRegistry[*NewTestState, *NewTestSaga]()
	builder := NewDagBuilder[*NewTestState, *NewTestSaga]("DuplicateNameTest", registry)

	action1 := NewActionFunc[*NewTestState, *NewTestSaga, *SimpleResult](
		"action1",
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "action1"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) error {
			return nil
		},
	)

	action2 := NewActionFunc[*NewTestState, *NewTestSaga, *SimpleResult](
		"action2",
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "action2"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) error {
			return nil
		},
	)

	// Should fail with duplicate node names
	err := builder.AppendParallel(
		&ActionNodeKind[*NewTestState, *NewTestSaga]{
			NodeName: "duplicate",
			Label:    "First Action",
			Action:   action1,
		},
		&ActionNodeKind[*NewTestState, *NewTestSaga]{
			NodeName: "duplicate", // Same name
			Label:    "Second Action",
			Action:   action2,
		},
	)
	assert.Error(t, err, "duplicate node names should be rejected")
	assert.Contains(t, err.Error(), "already exists", "error should mention duplicate name")
}

// Helper function to collect node IDs from an iterator
func collectNodeIDs(iterator graph.Nodes) []int64 {
	var ids []int64
	for iterator.Next() {
		ids = append(ids, iterator.Node().ID())
	}
	return ids
}