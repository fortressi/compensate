package compensate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileStore provides a file-based implementation of Store that persists
// saga state as JSON files on disk.
type FileStore[T any] struct {
	basePath string
	mu       sync.Mutex // Protects file operations
}

// NewFileStore creates a new file-based store that saves saga state
// to the specified directory.
func NewFileStore[T any](basePath string) (Store[T], error) {
	// Ensure the base directory exists
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base directory: %w", err)
	}

	return &FileStore[T]{
		basePath: basePath,
	}, nil
}

// Save persists the saga state to a JSON file.
func (f *FileStore[T]) Save(ctx context.Context, sagaID string, state State[T]) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Update timestamp
	state.UpdatedAt = time.Now()

	// Marshal state to JSON
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Write to file
	filename := f.filename(sagaID)
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}

// Load retrieves the saga state from a JSON file.
func (f *FileStore[T]) Load(ctx context.Context, sagaID string) (*State[T], error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Read file
	filename := f.filename(sagaID)
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("saga %s not found", sagaID)
		}
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	// Unmarshal state
	var state State[T]
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	return &state, nil
}

// Delete removes the saga state file.
func (f *FileStore[T]) Delete(ctx context.Context, sagaID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	filename := f.filename(sagaID)
	if err := os.Remove(filename); err != nil {
		if os.IsNotExist(err) {
			// Already deleted, not an error
			return nil
		}
		return fmt.Errorf("failed to delete state file: %w", err)
	}

	return nil
}

// filename returns the full path for a saga's state file.
func (f *FileStore[T]) filename(sagaID string) string {
	return filepath.Join(f.basePath, sagaID+".json")
}