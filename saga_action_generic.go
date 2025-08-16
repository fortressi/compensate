package compensate

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tidwall/btree"
)

// SagaType defines the type signature for a saga.
// type SagaType[T any] interface {
type SagaType[T any] interface {
	ExecContext() T
}

// ActionData represents data that can be serialized in the saga context.
type ActionData interface{}

// UndoResult represents the result of a saga undo action.
type UndoResult struct {
	Err error
}

// ActionName represents a unique name for a saga Action.
type ActionName string

// Action represents the building blocks of sagas.
type Action[T any, S SagaType[T]] interface {
	DoIt(ctx context.Context, sgctx ActionContext[T, S]) (ActionResult[ActionData], error)
	UndoIt(ctx context.Context, sgctx ActionContext[T, S]) error
	Name() ActionName
}

// ActionContext provides context to individual actions.
type ActionContext[T any, S SagaType[T]] struct {
	AncestorTree *btree.Map[NodeName, any]
	NodeID       int
	DAG          *SagaDag
	UserContext  T
	// SagaParams   T
}

// Lookup retrieves the output from a previous node by name.
// Returns the output and true if found, or zero value and false if not found.
func (ac *ActionContext[T, S]) Lookup(nodeName NodeName) (any, bool) {
	if ac.AncestorTree == nil {
		return nil, false
	}
	return ac.AncestorTree.Get(nodeName)
}

// LookupTypedResult retrieves and unmarshals the output from a previous node.
// This method provides the ergonomic interface suggested in the plan.
func (ac *ActionContext[T, S]) LookupTypedResult(name string, result any) error {
	val, ok := ac.AncestorTree.Get(NodeName(name))
	if !ok {
		return fmt.Errorf("no output found for action %q", name)
	}
	
	// If it's json.RawMessage (from persistence), unmarshal it
	if jsonData, ok := val.(json.RawMessage); ok {
		return json.Unmarshal(jsonData, result)
	}
	
	// Otherwise, it should already be the correct type
	// This is a bit tricky without reflection, but the generic function handles it
	return fmt.Errorf("value is not json.RawMessage; use the generic LookupTyped function instead")
}

// LookupTyped retrieves the output from a previous node with type assertion.
// Returns the typed output and true if found and type matches, or zero value and false otherwise.
// If the value is stored as json.RawMessage (from persistence), it will be unmarshaled.
func LookupTyped[R any, T any, S SagaType[T]](ac ActionContext[T, S], nodeName NodeName) (R, bool) {
	var zero R
	value, found := ac.Lookup(nodeName)
	if !found {
		return zero, false
	}
	
	// First try direct type assertion
	if typed, ok := value.(R); ok {
		return typed, true
	}
	
	// If that fails, check if it's json.RawMessage (from persistence)
	if jsonData, ok := value.(json.RawMessage); ok {
		var result R
		if err := json.Unmarshal(jsonData, &result); err == nil {
			return result, true
		}
	}
	
	return zero, false
}

type EmptyActionContext ActionContext[any, SagaType[any]]

// ActionConstant is an Action implementation that emits a predefined value.
type ActionConstant[T any] struct {
	value T
}

// NewActionConstant creates a new ActionConstant.
func NewActionConstant[T any](value T) *ActionConstant[T] {
	return &ActionConstant[T]{value: value}
}

// DoIt implements the Action interface for ActionConstant.
func (ac *ActionConstant[T]) DoIt(ctx context.Context, _ EmptyActionContext) (ActionResult[T], error) {
	return ActionResult[T]{Output: ac.value}, nil
}

// UndoIt implements the Action interface for ActionConstant.
func (ac *ActionConstant[T]) UndoIt(ctx context.Context, _ EmptyActionContext) error {
	return nil // No-op for constant actions
}

// Name implements the Action interface for ActionConstant.
func (ac *ActionConstant[T]) Name() ActionName {
	return "ActionConstant"
}

// ActionResult represents the result of a saga action.
type ActionResult[T any] struct {
	Output    T                      // The output data from the action
	StartTime time.Time              // Set by SEC when action execution starts
	EndTime   time.Time              // Set by SEC when action execution completes
	Warnings  []string               // Non-fatal issues encountered during execution
	Metrics   map[string]interface{} // Performance/debugging metrics from the action
}

// NewActionResult creates a new ActionResult with the given output.
// Timing information should be set by the SEC, not by actions.
func NewActionResult[T any](output T) ActionResult[T] {
	return ActionResult[T]{
		Output:   output,
		Warnings: []string{},
		Metrics:  make(map[string]interface{}),
	}
}

// AddWarning adds a non-fatal warning message to the result.
func (r *ActionResult[T]) AddWarning(msg string) {
	if r.Warnings == nil {
		r.Warnings = []string{}
	}
	r.Warnings = append(r.Warnings, msg)
}

// SetMetric adds a performance or debugging metric to the result.
func (r *ActionResult[T]) SetMetric(key string, value interface{}) {
	if r.Metrics == nil {
		r.Metrics = make(map[string]interface{})
	}
	r.Metrics[key] = value
}

// Duration returns the duration of the action execution.
// Returns zero duration if timing information is not yet set by SEC.
func (r *ActionResult[T]) Duration() time.Duration {
	if r.StartTime.IsZero() || r.EndTime.IsZero() {
		return 0
	}
	return r.EndTime.Sub(r.StartTime)
}

// EmptyOutput is an empty struct type used for ActionInjectError.
type EmptyOutput struct{}

// ActionInjectError is an Action implementation that simulates an error.
type ActionInjectError struct{}

// DoIt implements the Action interface for ActionInjectError.
func (aie *ActionInjectError) DoIt(ctx context.Context, _ EmptyActionContext) (ActionResult[EmptyOutput], error) {
	// TODO: Add logging
	return ActionResult[EmptyOutput]{}, fmt.Errorf("error injected")
}

// UndoIt implements the Action interface for ActionInjectError.
func (aie *ActionInjectError) UndoIt(ctx context.Context, _ EmptyActionContext) error {
	// We should never undo an action that failed, but this is used
	// for simulating undo errors as well.
	return fmt.Errorf("error injected")
}

// Name implements the Action interface for ActionInjectError.
func (aie *ActionInjectError) Name() ActionName {
	return "InjectError"
}
