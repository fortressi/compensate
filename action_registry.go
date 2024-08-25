package compensate

import (
	"fmt"

	"github.com/puzpuzpuz/xsync/v3"
)

// ActionRegistry is a registry of saga actions that can be used across multiple sagas.
//
// Actions are identified by their ActionName.
// Actions can exist at multiple nodes in each saga DAG. Since saga construction
// is dynamic and based upon user input, we need to allow a way to insert
// actions at runtime into the DAG. While this could be achieved by referencing
// the action during saga construction, this is not possible when reloading a
// saga from persistent storage. In this case, the concrete type of the Action
// is erased and the only mechanism we have to recover it is an `ActionName`. We
// therefore have all users register their actions for use across sagas so we
// can dynamically construct and restore sagas.
type ActionRegistry[T any, S SagaType[T]] struct {
	actions *xsync.MapOf[ActionName, Action[T, S]]
}

// NewActionRegistry creates a new ActionRegistry.
func NewActionRegistry[T any, S SagaType[T]]() *ActionRegistry[T, S] {

	return &ActionRegistry[T, S]{
		actions: xsync.NewMapOf[ActionName, Action[T, S]](),
	}
}

// Register adds an action to the registry.
func (r *ActionRegistry[T, S]) Register(action Action[T, S]) error {
	if _, ok := r.actions.Load(action.Name()); ok {
		return fmt.Errorf("action with name '%s' already registered", action.Name())
	}
	r.actions.Store(action.Name(), action)
	return nil
}

// Get retrieves an action from the registry by its name.
func (r *ActionRegistry[T, S]) Get(name ActionName) (Action[T, S], error) {
	action, ok := r.actions.Load(name)
	if !ok {
		return nil, NotFoundError()
	}
	return action, nil
}
