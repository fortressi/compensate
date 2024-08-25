package compensate

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Example: Order Processing Saga State
// This demonstrates how to define serializable saga state

// OrderSagaState represents the shared state for an order processing saga
// All fields must be exported for JSON serialization
type OrderSagaState struct {
	OrderID     string      `json:"order_id"`
	CustomerID  string      `json:"customer_id"`
	Items       []OrderItem `json:"items"`
	TotalAmount float64     `json:"total_amount"`
	PaymentID   string      `json:"payment_id,omitempty"`
	ShipmentID  string      `json:"shipment_id,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
}

type OrderItem struct {
	ProductID string  `json:"product_id"`
	Quantity  int     `json:"quantity"`
	Price     float64 `json:"price"`
}

// OrderSagaContext implements SagaType[*OrderSagaState]
type OrderSagaContext struct {
	State *OrderSagaState
}

func (c *OrderSagaContext) ExecContext() *OrderSagaState {
	return c.State
}

// Action outputs - also need to be serializable
type PaymentResult struct {
	PaymentID     string    `json:"payment_id"`
	TransactionID string    `json:"transaction_id"`
	ProcessedAt   time.Time `json:"processed_at"`
}

type InventoryResult struct {
	ReservationID string   `json:"reservation_id"`
	ReservedItems []string `json:"reserved_items"`
}

// Test helpers for verifying serializability
func AssertSerializable[T any](t *testing.T, value T, description string) {
	// Serialize
	data, err := json.Marshal(value)
	require.NoError(t, err, "%s: type %T should be JSON serializable", description, value)

	// Deserialize into new instance
	var restored T
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err, "%s: type %T should be JSON deserializable", description, value)

	// For structs with time fields, we need custom comparison due to time precision
	originalJSON, _ := json.Marshal(value)
	restoredJSON, _ := json.Marshal(restored)
	assert.JSONEq(t, string(originalJSON), string(restoredJSON),
		"%s: type %T should round-trip through JSON", description, value)
}

func TestSagaStateSerializable(t *testing.T) {
	// Test saga state
	sagaState := &OrderSagaState{
		OrderID:    "order-123",
		CustomerID: "customer-456",
		Items: []OrderItem{
			{ProductID: "prod-1", Quantity: 2, Price: 29.99},
			{ProductID: "prod-2", Quantity: 1, Price: 49.99},
		},
		TotalAmount: 109.97,
		CreatedAt:   time.Now().UTC().Truncate(time.Millisecond),
	}

	AssertSerializable(t, sagaState, "OrderSagaState")

	// Test with optional fields populated
	sagaState.PaymentID = "payment-789"
	sagaState.ShipmentID = "shipment-101"
	AssertSerializable(t, sagaState, "OrderSagaState with optional fields")

	// Test action outputs
	paymentResult := &PaymentResult{
		PaymentID:     "payment-789",
		TransactionID: "txn-abc123",
		ProcessedAt:   time.Now().UTC().Truncate(time.Millisecond),
	}
	AssertSerializable(t, paymentResult, "PaymentResult")

	inventoryResult := &InventoryResult{
		ReservationID: "reserve-123",
		ReservedItems: []string{"item-1", "item-2"},
	}
	AssertSerializable(t, inventoryResult, "InventoryResult")
}

// Example action using the saga state
func TestActionWithSagaState(t *testing.T) {
	// This shows how an action would access saga state
	processPaymentAction := NewActionFunc[*OrderSagaState, *OrderSagaContext, *PaymentResult](
		"process_payment",
		func(ctx context.Context, sgctx ActionContext[*OrderSagaState, *OrderSagaContext]) (ActionFuncResult[*PaymentResult], error) {
			// Access saga state through the context
			orderState := sgctx.UserContext

			// Simulate payment processing
			result := &PaymentResult{
				PaymentID:     "payment-" + orderState.OrderID,
				TransactionID: "txn-" + orderState.OrderID,
				ProcessedAt:   time.Now().UTC(),
			}

			// Update saga state
			orderState.PaymentID = result.PaymentID

			return ActionFuncResult[*PaymentResult]{Output: result}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*OrderSagaState, *OrderSagaContext]) error {
			// Compensate by refunding
			orderState := sgctx.UserContext
			orderState.PaymentID = "" // Clear payment ID
			return nil
		},
	)

	// Verify the action is properly typed
	assert.Equal(t, ActionName("process_payment"), processPaymentAction.Name())
}

// Example of bad state that won't serialize properly
type BadSagaState struct {
	orderID string // private field - won't serialize!
	total   float64
}

func TestBadSagaStateFails(t *testing.T) {
	badState := &BadSagaState{
		orderID: "order-123",
		total:   99.99,
	}

	// Serialize
	data, err := json.Marshal(badState)
	require.NoError(t, err) // This will pass, but...

	// Check that private fields weren't serialized
	var check map[string]interface{}
	err = json.Unmarshal(data, &check)
	require.NoError(t, err)

	// The map should be empty because no fields were exported
	assert.Empty(t, check, "private fields should not be serialized")
}
