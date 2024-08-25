package compensate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test types for the new DAG builder API
type NewTestState struct {
	ID string `json:"id"`
}

type NewTestSaga struct {
	State *NewTestState
}

func (s *NewTestSaga) ExecContext() *NewTestState {
	return s.State
}

type SimpleResult struct {
	Value string `json:"value"`
}

func TestNewDagBuilderAPI(t *testing.T) {
	// Create action registry
	registry := NewActionRegistry[*NewTestState, *NewTestSaga]()

	// Create some test actions
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

	// Create DAG builder with registry
	builder := NewDagBuilder[*NewTestState, *NewTestSaga]("NewAPITest", registry)

	// Level 1: Single action - using the new type-safe API!
	err := builder.Append(&ActionNodeKind[*NewTestState, *NewTestSaga]{
		NodeName: "validate",
		Label:    "Validate Order",
		Action:   validateAction, // Direct action reference - no string duplication!
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

	// Verify dependency structure by examining the graph directly
	sagaDag := NewSagaDag(dag, nil)

	// Get the actual node IDs
	validateID, err := sagaDag.GetNodeIndex("validate")
	require.NoError(t, err)
	paymentID, err := sagaDag.GetNodeIndex("payment") 
	require.NoError(t, err)
	inventoryID, err := sagaDag.GetNodeIndex("inventory")
	require.NoError(t, err)
	confirmID, err := sagaDag.GetNodeIndex("confirm")
	require.NoError(t, err)

	// Check dependencies using the graph
	graph := sagaDag.Graph

	// Payment should depend on validate
	paymentPredecessors := graph.To(paymentID)
	paymentDeps := collectNodeIDs(paymentPredecessors)
	assert.Contains(t, paymentDeps, validateID, "payment should depend on validate")

	// Inventory should depend on validate
	inventoryPredecessors := graph.To(inventoryID)
	inventoryDeps := collectNodeIDs(inventoryPredecessors)
	assert.Contains(t, inventoryDeps, validateID, "inventory should depend on validate")

	// Confirm should depend on both payment and inventory
	confirmPredecessors := graph.To(confirmID)
	confirmDeps := collectNodeIDs(confirmPredecessors)
	assert.Contains(t, confirmDeps, paymentID, "confirm should depend on payment")
	assert.Contains(t, confirmDeps, inventoryID, "confirm should depend on inventory")


	// Verify that actions were auto-registered
	_, err = registry.Get("validate_action")
	assert.NoError(t, err, "validate action should be auto-registered")

	_, err = registry.Get("payment_action")
	assert.NoError(t, err, "payment action should be auto-registered")

	_, err = registry.Get("inventory_action")
	assert.NoError(t, err, "inventory action should be auto-registered")

	_, err = registry.Get("confirm_action")
	assert.NoError(t, err, "confirm action should be auto-registered")

	t.Logf("New DAG builder API test passed! Actions auto-registered and DAG structure verified.")
}