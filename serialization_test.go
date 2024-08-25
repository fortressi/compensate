package compensate

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SerializationTestHelper provides utilities for testing serialization conformance
type SerializationTestHelper struct {
	t *testing.T
}

// NewSerializationTestHelper creates a new test helper
func NewSerializationTestHelper(t *testing.T) *SerializationTestHelper {
	return &SerializationTestHelper{t: t}
}

// AssertSerializable verifies that a value can round-trip through JSON serialization
func (h *SerializationTestHelper) AssertSerializable(value interface{}, description string) {
	t := h.t

	// Serialize
	data, err := json.Marshal(value)
	require.NoError(t, err, "%s: should be JSON serializable", description)

	// Log the serialized form for debugging
	t.Logf("%s serialized: %s", description, string(data))

	// Verify non-empty serialization (catches private field structs)
	if data != nil && string(data) == "{}" {
		// Check if this is a pointer to an empty struct
		var m map[string]interface{}
		json.Unmarshal(data, &m)
		if len(m) == 0 {
			t.Errorf("%s: serialized to empty object - likely has only private fields", description)
		}
	}
}

// AssertRoundTrip verifies that a value can be serialized and deserialized without loss
func (h *SerializationTestHelper) AssertRoundTrip(original interface{}, target interface{}, description string) {
	t := h.t

	// Serialize
	data, err := json.Marshal(original)
	require.NoError(t, err, "%s: serialization failed", description)

	// Deserialize
	err = json.Unmarshal(data, target)
	require.NoError(t, err, "%s: deserialization failed", description)

	// Compare JSON representations (handles time precision issues)
	originalJSON, _ := json.Marshal(original)
	targetJSON, _ := json.Marshal(target)
	assert.JSONEq(t, string(originalJSON), string(targetJSON),
		"%s: round-trip serialization should preserve all data", description)
}

// TestSerializationConformance demonstrates the conformance testing approach
func TestSerializationConformance(t *testing.T) {
	helper := NewSerializationTestHelper(t)

	t.Run("Good saga state", func(t *testing.T) {
		type GoodState struct {
			ID      string            `json:"id"`
			Counter int               `json:"counter"`
			Data    map[string]string `json:"data"`
		}

		state := &GoodState{
			ID:      "test-123",
			Counter: 42,
			Data:    map[string]string{"key": "value"},
		}

		helper.AssertSerializable(state, "GoodState")

		var restored GoodState
		helper.AssertRoundTrip(state, &restored, "GoodState round-trip")
	})

	t.Run("Bad saga state with private fields", func(t *testing.T) {
		type BadState struct {
			id      string // private - won't serialize!
			counter int    // private - won't serialize!
		}

		state := &BadState{
			id:      "test-123",
			counter: 42,
		}

		// This will fail the conformance test
		data, _ := json.Marshal(state)
		assert.Equal(t, "{}", string(data), "private fields should result in empty JSON")
	})

	t.Run("Nested state structures", func(t *testing.T) {
		type NestedItem struct {
			Name  string `json:"name"`
			Value int    `json:"value"`
		}

		type ComplexState struct {
			Items  []NestedItem          `json:"items"`
			Lookup map[string]NestedItem `json:"lookup"`
		}

		state := &ComplexState{
			Items: []NestedItem{
				{Name: "item1", Value: 10},
				{Name: "item2", Value: 20},
			},
			Lookup: map[string]NestedItem{
				"key1": {Name: "lookup1", Value: 30},
			},
		}

		helper.AssertSerializable(state, "ComplexState")

		var restored ComplexState
		helper.AssertRoundTrip(state, &restored, "ComplexState round-trip")
	})
}

// SagaStateConformance defines the contract for serializable saga states
type SagaStateConformance interface {
	// ValidateSerializable checks if the state can be properly serialized
	// This is optional but recommended for runtime validation
	ValidateSerializable() error
}

// Example implementation with validation
type ValidatedOrderState struct {
	OrderID  string  `json:"order_id"`
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

func (s *ValidatedOrderState) ValidateSerializable() error {
	// Test serialization
	_, err := json.Marshal(s)
	return err
}

func TestValidatedState(t *testing.T) {
	state := &ValidatedOrderState{
		OrderID:  "order-456",
		Amount:   99.99,
		Currency: "USD",
	}

	// Test the validation method
	require.NoError(t, state.ValidateSerializable())

	// Test with helper
	helper := NewSerializationTestHelper(t)
	helper.AssertSerializable(state, "ValidatedOrderState")
}
