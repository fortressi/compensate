package compensate

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Store defines the interface for persisting saga state.
// It is generic over T, the saga context type, to provide type safety
// without reflection for the main saga state.
type Store[T any] interface {
	// Save persists the current saga state
	Save(ctx context.Context, sagaID string, state State[T]) error

	// Load retrieves a saga state by ID
	Load(ctx context.Context, sagaID string) (*State[T], error)

	// Delete removes a saga state after successful rollback
	Delete(ctx context.Context, sagaID string) error
}

// State contains the minimal information needed to resume or rollback a saga.
// It is generic over T, the saga context type.
type State[T any] struct {
	SagaID           string             `json:"saga_id"`
	SagaName         string             `json:"saga_name"`
	Status           string             `json:"status"` // "running", "completed", "failed", "rolled_back"
	Context          T                  `json:"context"`
	CompletedActions []CompletedAction  `json:"completed_actions"`
	CreatedAt        time.Time          `json:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at"`
}

// CompletedAction records an action that has been successfully executed,
// along with its output for use by dependent actions during rollback.
type CompletedAction struct {
	Name   string          `json:"name"`
	Output json.RawMessage `json:"output,omitempty"`
}

// Saga status constants
const (
	SagaStatusRunning     = "running"
	SagaStatusCompleted   = "completed"
	SagaStatusFailed      = "failed"
	SagaStatusRollingBack = "rolling_back"
	SagaStatusRolledBack  = "rolled_back"
)

// MemoryStore provides an in-memory implementation of Store for testing
// or scenarios where persistence is not required.
type MemoryStore[T any] struct {
	states map[string]*State[T]
	mu     sync.RWMutex
}

// NewMemoryStore creates a new in-memory store.
func NewMemoryStore[T any]() Store[T] {
	return &MemoryStore[T]{
		states: make(map[string]*State[T]),
	}
}

// Save stores the saga state in memory.
func (m *MemoryStore[T]) Save(ctx context.Context, sagaID string, state State[T]) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Create a copy to avoid external modifications
	stateCopy := state
	stateCopy.UpdatedAt = time.Now()
	
	m.states[sagaID] = &stateCopy
	return nil
}

// Load retrieves the saga state from memory.
func (m *MemoryStore[T]) Load(ctx context.Context, sagaID string) (*State[T], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, exists := m.states[sagaID]
	if !exists {
		return nil, fmt.Errorf("saga %s not found", sagaID)
	}

	// Return a copy to avoid external modifications
	stateCopy := *state
	return &stateCopy, nil
}

// Delete removes the saga state from memory.
func (m *MemoryStore[T]) Delete(ctx context.Context, sagaID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.states, sagaID)
	return nil
}