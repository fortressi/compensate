package compensate

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/btree"
)

// Test saga: Order Processing
// Flow: ValidateOrder -> ProcessPayment -> UpdateInventory -> SendConfirmation

// OrderProcessingSaga state
type OrderProcessingState struct {
	OrderID       string  `json:"order_id"`
	CustomerID    string  `json:"customer_id"`
	Amount        float64 `json:"amount"`
	PaymentID     string  `json:"payment_id,omitempty"`
	InventoryTxn  string  `json:"inventory_txn,omitempty"`
	Confirmed     bool    `json:"confirmed"`
	ValidationMsg string  `json:"validation_msg,omitempty"`
}

// OrderProcessingSaga implements SagaType
type OrderProcessingSaga struct {
	State *OrderProcessingState
}

func (s *OrderProcessingSaga) ExecContext() *OrderProcessingState {
	return s.State
}

// Action output types for executor test  
type ExecutorValidationResult struct {
	Valid   bool   `json:"valid"`
	Message string `json:"message"`
}

type ExecutorPaymentResult struct {
	PaymentID     string `json:"payment_id"`
	TransactionID string `json:"transaction_id"`
}

type ExecutorInventoryResult struct {
	TransactionID string `json:"transaction_id"`
	ItemsReserved int    `json:"items_reserved"`
}

type ExecutorConfirmationResult struct {
	ConfirmationID string `json:"confirmation_id"`
	SentAt         string `json:"sent_at"`
}

func TestSagaExecutorSequential(t *testing.T) {
	// Create initial saga state
	sagaState := &OrderProcessingState{
		OrderID:    "order-123",
		CustomerID: "customer-456",
		Amount:     99.99,
		Confirmed:  false,
	}
	
	sagaContext := &OrderProcessingSaga{State: sagaState}
	
	// Create action registry
	registry := NewActionRegistry[*OrderProcessingState, *OrderProcessingSaga]()
	
	// Create and register actions
	validateAction := NewActionFunc[*OrderProcessingState, *OrderProcessingSaga, *ExecutorValidationResult](
		"validate_order",
		func(ctx context.Context, sgctx ActionContext[*OrderProcessingState, *OrderProcessingSaga]) (ActionFuncResult[*ExecutorValidationResult], error) {
			state := sgctx.UserContext
			result := &ExecutorValidationResult{Valid: true, Message: "Order is valid"}
			state.ValidationMsg = result.Message
			return ActionFuncResult[*ExecutorValidationResult]{Output: result}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*OrderProcessingState, *OrderProcessingSaga]) error {
			state := sgctx.UserContext
			state.ValidationMsg = ""
			return nil
		},
	)
	
	paymentAction := NewActionFunc[*OrderProcessingState, *OrderProcessingSaga, *ExecutorPaymentResult](
		"process_payment",
		func(ctx context.Context, sgctx ActionContext[*OrderProcessingState, *OrderProcessingSaga]) (ActionFuncResult[*ExecutorPaymentResult], error) {
			state := sgctx.UserContext
			result := &ExecutorPaymentResult{PaymentID: "payment-" + state.OrderID, TransactionID: "txn-" + state.OrderID}
			state.PaymentID = result.PaymentID
			return ActionFuncResult[*ExecutorPaymentResult]{Output: result}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*OrderProcessingState, *OrderProcessingSaga]) error {
			state := sgctx.UserContext
			state.PaymentID = ""
			return nil
		},
	)
	
	inventoryAction := NewActionFunc[*OrderProcessingState, *OrderProcessingSaga, *ExecutorInventoryResult](
		"update_inventory",
		func(ctx context.Context, sgctx ActionContext[*OrderProcessingState, *OrderProcessingSaga]) (ActionFuncResult[*ExecutorInventoryResult], error) {
			state := sgctx.UserContext
			result := &ExecutorInventoryResult{TransactionID: "inv-txn-" + state.OrderID, ItemsReserved: 1}
			state.InventoryTxn = result.TransactionID
			return ActionFuncResult[*ExecutorInventoryResult]{Output: result}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*OrderProcessingState, *OrderProcessingSaga]) error {
			state := sgctx.UserContext
			state.InventoryTxn = ""
			return nil
		},
	)
	
	confirmationAction := NewActionFunc[*OrderProcessingState, *OrderProcessingSaga, *ExecutorConfirmationResult](
		"send_confirmation",
		func(ctx context.Context, sgctx ActionContext[*OrderProcessingState, *OrderProcessingSaga]) (ActionFuncResult[*ExecutorConfirmationResult], error) {
			state := sgctx.UserContext
			result := &ExecutorConfirmationResult{ConfirmationID: "conf-" + state.OrderID, SentAt: "2023-01-01T00:00:00Z"}
			state.Confirmed = true
			return ActionFuncResult[*ExecutorConfirmationResult]{Output: result}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*OrderProcessingState, *OrderProcessingSaga]) error {
			state := sgctx.UserContext
			state.Confirmed = false
			return nil
		},
	)
	
	// Register actions
	registry.Register(validateAction)
	registry.Register(paymentAction)
	registry.Register(inventoryAction)
	registry.Register(confirmationAction)
	
	// Build DAG
	builder := NewDagBuilder[*OrderProcessingState, *OrderProcessingSaga]("OrderProcessing", registry)
	
	// Add nodes in dependency order
	err := builder.Append(&ActionNodeKind[*OrderProcessingState, *OrderProcessingSaga]{
		NodeName: "validate",
		Label:    "Validate Order",
		Action:   validateAction,
	})
	require.NoError(t, err)
	
	err = builder.Append(&ActionNodeKind[*OrderProcessingState, *OrderProcessingSaga]{
		NodeName: "payment",
		Label:    "Process Payment",
		Action:   paymentAction,
	})
	require.NoError(t, err)
	
	err = builder.Append(&ActionNodeKind[*OrderProcessingState, *OrderProcessingSaga]{
		NodeName: "inventory",
		Label:    "Update Inventory",
		Action:   inventoryAction,
	})
	require.NoError(t, err)
	
	err = builder.Append(&ActionNodeKind[*OrderProcessingState, *OrderProcessingSaga]{
		NodeName: "confirmation",
		Label:    "Send Confirmation",
		Action:   confirmationAction,
	})
	require.NoError(t, err)
	
	// Build the DAG
	dag, err := builder.Build()
	require.NoError(t, err)
	
	// Create runnable saga DAG
	sagaDag := NewSagaDag(dag, json.RawMessage(`{"initial": "params"}`))
	
	// Create executor with memory store for testing
	store := NewMemoryStore[*OrderProcessingState]()
	sagaID := "test-saga-1"
	executor := NewSagaExecutor(sagaDag, registry, sagaContext, sagaID, store)
	
	// Execute the saga
	ctx := context.Background()
	err = executor.Execute(ctx)
	require.NoError(t, err, "saga execution should succeed")
	
	// Verify execution order
	expectedOrder := []string{"validate_order", "process_payment", "update_inventory", "send_confirmation"}
	actualOrder := executor.GetExecutionOrder()
	t.Logf("Expected order: %v", expectedOrder)
	t.Logf("Actual order: %v", actualOrder)
	assert.Equal(t, expectedOrder, actualOrder, "actions should execute in correct dependency order")
	
	// Verify final state
	assert.Equal(t, "order-123", sagaState.OrderID)
	assert.Equal(t, "payment-order-123", sagaState.PaymentID)
	assert.Equal(t, "inv-txn-order-123", sagaState.InventoryTxn)
	assert.True(t, sagaState.Confirmed)
	assert.Equal(t, "Order is valid", sagaState.ValidationMsg)
	
	// Verify execution state
	execState := executor.GetExecutionState()
	completedNodes := executor.GetCompletedNodes()
	
	// Should have 4 completed action nodes (plus start/end nodes)
	assert.True(t, len(completedNodes) >= 4, "should have completed at least 4 action nodes")
	
	// All action nodes should be completed
	for nodeIndex, execNode := range execState {
		if execNode.NodeName != "" { // Only check named action nodes
			assert.Equal(t, ActionStateCompleted, execNode.State, 
				"node %d (%s) should be completed", nodeIndex, execNode.NodeName)
			assert.Nil(t, execNode.Error, 
				"node %d (%s) should have no error", nodeIndex, execNode.NodeName)
		}
	}
}

func TestSagaExecutorWithFailure(t *testing.T) {
	// Create saga state that will cause payment to fail
	sagaState := &OrderProcessingState{
		OrderID:    "order-fail",
		CustomerID: "customer-bad",
		Amount:     1.0, // Valid amount for validation, but will cause payment failure due to negative check
		Confirmed:  false,
	}
	
	sagaContext := &OrderProcessingSaga{State: sagaState}
	
	// Create action registry  
	registry := NewActionRegistry[*OrderProcessingState, *OrderProcessingSaga]()
	
	// Build simple DAG: validate -> payment
	builder := NewDagBuilder[*OrderProcessingState, *OrderProcessingSaga]("FailingOrderProcessing", registry)

	// Create actions inline for this test
	validateAction := NewActionFunc[*OrderProcessingState, *OrderProcessingSaga, *ExecutorValidationResult](
		"validate_order",
		func(ctx context.Context, sgctx ActionContext[*OrderProcessingState, *OrderProcessingSaga]) (ActionFuncResult[*ExecutorValidationResult], error) {
			state := sgctx.UserContext
			
			result := &ExecutorValidationResult{
				Valid:   state.Amount > 0 && state.CustomerID != "",
				Message: "Order is valid",
			}
			
			if !result.Valid {
				return ActionFuncResult[*ExecutorValidationResult]{}, fmt.Errorf("order validation failed")
			}
			
			// Update saga state
			state.ValidationMsg = result.Message
			
			return ActionFuncResult[*ExecutorValidationResult]{Output: result}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*OrderProcessingState, *OrderProcessingSaga]) error {
			// Undo validation
			state := sgctx.UserContext
			state.ValidationMsg = ""
			return nil
		},
	)

	paymentAction := NewActionFunc[*OrderProcessingState, *OrderProcessingSaga, *ExecutorPaymentResult](
		"process_payment",
		func(ctx context.Context, sgctx ActionContext[*OrderProcessingState, *OrderProcessingSaga]) (ActionFuncResult[*ExecutorPaymentResult], error) {
			state := sgctx.UserContext
			
			// Check validation result from previous step
			validationOutput, found := LookupTyped[*ExecutorValidationResult](sgctx, "validate")
			if !found || !validationOutput.Valid {
				return ActionFuncResult[*ExecutorPaymentResult]{}, fmt.Errorf("cannot process payment: validation failed")
			}
			
			// Simulate payment failure for negative amounts or bad customers
			if state.Amount <= 0 {
				return ActionFuncResult[*ExecutorPaymentResult]{}, fmt.Errorf("invalid payment amount: %f", state.Amount)
			}
			
			// Simulate payment failure for specific customer
			if state.CustomerID == "customer-bad" {
				return ActionFuncResult[*ExecutorPaymentResult]{}, fmt.Errorf("payment declined for customer: %s", state.CustomerID)
			}
			
			result := &ExecutorPaymentResult{
				PaymentID:     "payment-" + state.OrderID,
				TransactionID: "txn-" + state.OrderID,
			}
			
			// Update saga state
			state.PaymentID = result.PaymentID
			
			return ActionFuncResult[*ExecutorPaymentResult]{Output: result}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*OrderProcessingState, *OrderProcessingSaga]) error {
			// Undo payment (refund)
			state := sgctx.UserContext
			state.PaymentID = ""
			return nil
		},
	)
	
	err := builder.Append(&ActionNodeKind[*OrderProcessingState, *OrderProcessingSaga]{
		NodeName: "validate",
		Label:    "Validate Order",
		Action:   validateAction,
	})
	require.NoError(t, err)
	
	err = builder.Append(&ActionNodeKind[*OrderProcessingState, *OrderProcessingSaga]{
		NodeName: "payment",
		Label:    "Process Payment",
		Action:   paymentAction,
	})
	require.NoError(t, err)
	
	dag, err := builder.Build()
	require.NoError(t, err)
	
	sagaDag := NewSagaDag(dag, json.RawMessage(`{}`))
	// Create executor with memory store for testing
	store := NewMemoryStore[*OrderProcessingState]()
	sagaID := "test-rollback-saga"
	executor := NewSagaExecutor(sagaDag, registry, sagaContext, sagaID, store)
	
	// Execute - should fail at payment
	ctx := context.Background()
	err = executor.Execute(ctx)
	require.Error(t, err, "saga should fail")
	assert.Contains(t, err.Error(), "saga failed at node")
	
	// Verify compensation occurred
	execState := executor.GetExecutionState()
	failedNodes := executor.GetFailedNodes()
	assert.True(t, len(failedNodes) > 0, "should have failed nodes")
	
	// Find the validation node - it should be undone
	var validationNode *ExecutionNode
	for _, execNode := range execState {
		if execNode.NodeName == "validate" {
			validationNode = execNode
			break
		}
	}
	require.NotNil(t, validationNode, "should find validation node")
	t.Logf("Validation node state: %d (%s)", validationNode.State, validationNode.State.String())
	assert.Equal(t, ActionStateUndone, validationNode.State, "validation should be undone")
	
	// Saga state should be reset by compensation
	assert.Empty(t, sagaState.ValidationMsg, "validation message should be cleared by undo")
}


func TestActionLookup(t *testing.T) {
	// Test the LookupTyped functionality specifically
	ancestorTree := btree.NewMap[NodeName, any](10)
	ancestorTree.Set("test_node", &ExecutorValidationResult{Valid: true, Message: "test"})
	
	actionCtx := ActionContext[*OrderProcessingState, *OrderProcessingSaga]{
		AncestorTree: ancestorTree,
		NodeID:       1,
		DAG:          nil,
		UserContext:  &OrderProcessingState{},
	}
	
	// Test successful lookup
	result, found := LookupTyped[*ExecutorValidationResult](actionCtx, "test_node")
	assert.True(t, found, "should find the node")
	assert.True(t, result.Valid, "should get correct value")
	assert.Equal(t, "test", result.Message)
	
	// Test missing node
	_, found = LookupTyped[*ExecutorValidationResult](actionCtx, "missing_node")
	assert.False(t, found, "should not find missing node")
	
	// Test wrong type
	ancestorTree.Set("wrong_type", "string_value")
	_, found = LookupTyped[*ExecutorValidationResult](actionCtx, "wrong_type")
	assert.False(t, found, "should not find with wrong type")
}