package compensate

import (
	"fmt"
)

// ActionError represents an error produced by a saga action.
type ActionError struct {
	error
}

// ActionFailed wraps a user-provided error in an ActionError.
func ActionFailed(err error) error {
	return &ActionError{fmt.Errorf("action failed: %w", err)}
}

// DeserializeFailed indicates a failure to deserialize saga data.
func DeserializeFailed(message string) error {
	return &ActionError{fmt.Errorf("deserialize failed: %s", message)}
}

// InjectedError indicates an error was injected (for testing).
func InjectedError() error {
	return &ActionError{fmt.Errorf("error injected")}
}

// SerializeFailed indicates a failure to serialize saga data.
func SerializeFailed(message string) error {
	return &ActionError{fmt.Errorf("serialize failed: %s", message)}
}

// SubsagaCreateFailed indicates a failure to create a subsaga.
func SubsagaCreateFailed(message string) error {
	return &ActionError{fmt.Errorf("failed to create subsaga: %s", message)}
}

// Convert tries to convert the error to a specific consumer error type.
func Convert[T ActionData](err error) (T, error) {
	var result T
	actionErr, ok := err.(*ActionError)
	if !ok {
		return result, err // Not an ActionError
	}

	if actionErr.error == nil {
		return result, fmt.Errorf("ActionError has no inner error")
	}

	// Attempt to unmarshal the inner error into the specific type T
	// data, err := json.Marshal(actionErr.error)
	// if err != nil {
	// 	return result, SerializeFailed(err.Error())
	// }

	// if err := json.Unmarshal(data, &result); err != nil {
	// 	return result, DeserializeFailed(err.Error())
	// }

	if innerErr, ok := actionErr.error.(T); ok {
		return innerErr, nil
	}

	return result, fmt.Errorf("failed to convert error to type %T", result)
}

// NewSerializeError creates a new SerializeFailed error.
func NewSerializeError(err error) error {
	return SerializeFailed(err.Error())
}

// NewDeserializeError creates a new DeserializeFailed error.
func NewDeserializeError(err error) error {
	return DeserializeFailed(err.Error())
}

// NewSubsagaError creates a new SubsagaCreateFailed error.
func NewSubsagaError(err error) error {
	return SubsagaCreateFailed(err.Error())
}

// UndoActionError represents an error produced by a failed undo action.
type UndoActionError struct {
	error
}

// PermanentFailure indicates a permanent failure of an undo action.
func PermanentFailure(err error) error {
	return &UndoActionError{fmt.Errorf("undo action failed permanently: %w", err)}
}
