package compensate

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActionResultHelpers(t *testing.T) {
	// Test NewActionResult
	output := "test output"
	result := NewActionResult(output)
	
	assert.Equal(t, output, result.Output)
	assert.NotNil(t, result.Warnings)
	assert.NotNil(t, result.Metrics)
	assert.Empty(t, result.Warnings)
	assert.Empty(t, result.Metrics)
	
	// Test AddWarning
	result.AddWarning("first warning")
	result.AddWarning("second warning")
	assert.Len(t, result.Warnings, 2)
	assert.Equal(t, "first warning", result.Warnings[0])
	assert.Equal(t, "second warning", result.Warnings[1])
	
	// Test SetMetric
	result.SetMetric("items_processed", 100)
	result.SetMetric("duration_ms", 250.5)
	assert.Len(t, result.Metrics, 2)
	assert.Equal(t, 100, result.Metrics["items_processed"])
	assert.Equal(t, 250.5, result.Metrics["duration_ms"])
	
	// Test Duration with no times set
	assert.Equal(t, time.Duration(0), result.Duration())
	
	// Test Duration with times set
	result.StartTime = time.Now()
	result.EndTime = result.StartTime.Add(100 * time.Millisecond)
	assert.Equal(t, 100*time.Millisecond, result.Duration())
}

func TestActionResultWithSEC(t *testing.T) {
	// Create test saga state and context
	sagaState := &SimpleState{Values: []string{}}
	sagaContext := &SimpleSaga{State: sagaState}
	
	// Create action registry
	registry := NewActionRegistry[*SimpleState, *SimpleSaga]()
	
	// Create an action that uses warnings and metrics
	testAction := NewActionFunc[*SimpleState, *SimpleSaga, string](
		"test_action",
		func(ctx context.Context, sgctx ActionContext[*SimpleState, *SimpleSaga]) (ActionResult[string], error) {
			result := NewActionResult("test output")
			
			// Add warnings
			result.AddWarning("This is a non-fatal warning")
			result.AddWarning("Another warning for testing")
			
			// Add metrics
			result.SetMetric("items_processed", 42)
			result.SetMetric("cache_hit_rate", 0.85)
			
			// Note: We don't set StartTime/EndTime - SEC should do that
			return result, nil
		},
		func(ctx context.Context, sgctx ActionContext[*SimpleState, *SimpleSaga]) error {
			return nil
		},
	)
	
	require.NoError(t, registry.Register(testAction))
	
	// Build DAG
	builder := NewDagBuilder[*SimpleState, *SimpleSaga]("TestTimingSaga", registry)
	err := builder.Append(&ActionNodeKind[*SimpleState, *SimpleSaga]{
		NodeName: "test_node",
		Label:    "Test Action Node",
		Action:   testAction,
	})
	require.NoError(t, err)
	
	dag, err := builder.Build()
	require.NoError(t, err)
	
	sagaDag := NewSagaDag(dag, json.RawMessage(`{}`))
	
	// Create executor with memory store
	store := NewMemoryStore[*SimpleState]()
	executor := NewSagaExecutor(sagaDag, registry, sagaContext, "test-timing-saga", store)
	
	// Execute the saga
	err = executor.Execute(context.Background())
	require.NoError(t, err)
	
	// Verify the action result has timing set by SEC
	execNode := executor.nodes[0] // First action node
	require.NotNil(t, execNode.Result)
	
	// Check that SEC set the timing
	assert.False(t, execNode.Result.StartTime.IsZero(), "StartTime should be set by SEC")
	assert.False(t, execNode.Result.EndTime.IsZero(), "EndTime should be set by SEC")
	assert.True(t, execNode.Result.EndTime.After(execNode.Result.StartTime), "EndTime should be after StartTime")
	
	// Check that warnings and metrics were preserved
	assert.Len(t, execNode.Result.Warnings, 2)
	assert.Contains(t, execNode.Result.Warnings, "This is a non-fatal warning")
	assert.Contains(t, execNode.Result.Warnings, "Another warning for testing")
	
	assert.Len(t, execNode.Result.Metrics, 2)
	assert.Equal(t, 42, execNode.Result.Metrics["items_processed"])
	assert.Equal(t, 0.85, execNode.Result.Metrics["cache_hit_rate"])
	
	// Check Duration calculation
	duration := execNode.Result.Duration()
	assert.Greater(t, duration, time.Duration(0), "Duration should be positive")
}

func TestActionResultPersistence(t *testing.T) {
	// Create test saga state and context
	sagaState := &SimpleState{Values: []string{}}
	sagaContext := &SimpleSaga{State: sagaState}
	
	// Create action registry
	registry := NewActionRegistry[*SimpleState, *SimpleSaga]()
	
	// Create an action with warnings and metrics
	testAction := NewActionFunc[*SimpleState, *SimpleSaga, string](
		"persist_test_action",
		func(ctx context.Context, sgctx ActionContext[*SimpleState, *SimpleSaga]) (ActionResult[string], error) {
			result := NewActionResult("persisted output")
			result.AddWarning("Warning to persist")
			result.SetMetric("metric_value", 123)
			return result, nil
		},
		func(ctx context.Context, sgctx ActionContext[*SimpleState, *SimpleSaga]) error {
			return nil
		},
	)
	
	require.NoError(t, registry.Register(testAction))
	
	// Build DAG
	builder := NewDagBuilder[*SimpleState, *SimpleSaga]("PersistTestSaga", registry)
	err := builder.Append(&ActionNodeKind[*SimpleState, *SimpleSaga]{
		NodeName: "persist_node",
		Label:    "Persist Test Action",
		Action:   testAction,
	})
	require.NoError(t, err)
	
	dag, err := builder.Build()
	require.NoError(t, err)
	
	sagaDag := NewSagaDag(dag, json.RawMessage(`{}`))
	
	// Create executor with memory store
	store := NewMemoryStore[*SimpleState]()
	executor := NewSagaExecutor(sagaDag, registry, sagaContext, "persist-test-saga", store)
	
	// Execute the saga
	err = executor.Execute(context.Background())
	require.NoError(t, err)
	
	// Load the persisted state
	loadedState, err := store.Load(context.Background(), "persist-test-saga")
	require.NoError(t, err)
	require.NotNil(t, loadedState)
	
	// Check that completed actions have the full metadata
	require.Len(t, loadedState.CompletedActions, 1)
	completedAction := loadedState.CompletedActions[0]
	
	assert.Equal(t, "persist_node", completedAction.Name)
	assert.False(t, completedAction.StartTime.IsZero())
	assert.False(t, completedAction.EndTime.IsZero())
	assert.Len(t, completedAction.Warnings, 1)
	assert.Equal(t, "Warning to persist", completedAction.Warnings[0])
	assert.Len(t, completedAction.Metrics, 1)
	// JSON numbers are stored directly now, no unmarshaling happens in memory store
	assert.Equal(t, 123, completedAction.Metrics["metric_value"])
}