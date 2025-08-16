package compensate

import (
	"context"
	"encoding/json"
	"fmt"
)

// ActionFn is a generic function type for saga actions.
// NOTE(morganj): Do we need a context.Context here...
// type ActionFn[T any, S SagaType[T], R any] func(ctx context.Context, sgctx ActionContext[T, S]) (R, error)

type DoItFunc[T any, S SagaType[T], R ActionData] func(ctx context.Context, sgctx ActionContext[T, S]) (ActionResult[R], error)
type UndoItFunc[T any, S SagaType[T]] func(ctx context.Context, sgctx ActionContext[T, S]) error

// ActionFunc is an implementation of Action that uses ordinary functions.
type ActionFunc[T any, S SagaType[T], R ActionData] struct {
	name       ActionName
	actionFunc DoItFunc[T, S, R]
	undoFunc   UndoItFunc[T, S]
}

// NewActionFunc constructs a new ActionFunc from a pair of functions.
func NewActionFunc[T any, S SagaType[T], R ActionData](name ActionName, actionFunc DoItFunc[T, S, R], undoFunc UndoItFunc[T, S]) *ActionFunc[T, S, R] {
	return &ActionFunc[T, S, R]{
		name:       name,
		actionFunc: actionFunc,
		undoFunc:   undoFunc,
	}
}

func NoOpUndo[T any, S SagaType[T]](_ context.Context, _ ActionContext[T, S]) error {
	return nil
}

// NewActionFuncWithNoOpUndo constructs a new ActionFunc with a no-op undo function.
func NewActionFuncWithNoOpUndo[T any, S SagaType[T], R ActionData](name ActionName, actionFunc DoItFunc[T, S, R]) *ActionFunc[T, S, R] {
	return NewActionFunc(name, actionFunc, NoOpUndo[T, S])
}

// DoIt implements the Action interface for ActionFunc.
func (af *ActionFunc[T, S, R]) DoIt(ctx context.Context, sgctx ActionContext[T, S]) (ActionResult[ActionData], error) {
	result, err := af.actionFunc(ctx, sgctx)
	if err != nil {
		return ActionResult[ActionData]{}, err
	}

	// Validate that the output can be serialized
	_, err = json.Marshal(result.Output)
	if err != nil {
		return ActionResult[ActionData]{}, ActionFailed(NewSerializeError(err))
	}

	// Convert to ActionResult[ActionData] preserving all fields except timing
	// (SEC will set timing after this returns)
	return ActionResult[ActionData]{
		Output:   result.Output,
		Warnings: result.Warnings,
		Metrics:  result.Metrics,
	}, nil
}

// UndoIt implements the Action interface for ActionFunc.
func (af *ActionFunc[T, S, R]) UndoIt(ctx context.Context, sgctx ActionContext[T, S]) error {
	return af.undoFunc(ctx, sgctx)
}

// Name implements the Action interface for ActionFunc.
func (af *ActionFunc[T, S, R]) Name() ActionName {
	return af.name
}

// String implements the fmt.Stringer interface for ActionFunc.
func (af *ActionFunc[T, S, R]) String() string {
	return fmt.Sprintf("ActionFunc[%T]", af)
}
