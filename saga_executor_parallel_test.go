package compensate

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test state for parallel execution testing
type ParallelTestState struct {
	ID          string   `json:"id"`
	StepResults []string `json:"step_results"`
	Completed   bool     `json:"completed"`
}

type ParallelTestSaga struct {
	State *ParallelTestState
}

func (s *ParallelTestSaga) ExecContext() *ParallelTestState {
	return s.State
}

// Simple result type for parallel tests
type ParallelResult struct {
	StepName string `json:"step_name"`
	Value    string `json:"value"`
}

func TestSagaExecutorWithParallelDAG(t *testing.T) {
	// Create test state
	sagaState := &ParallelTestState{
		ID:          "parallel-test-123",
		StepResults: []string{},
		Completed:   false,
	}
	sagaContext := &ParallelTestSaga{State: sagaState}

	// Create action registry
	registry := NewActionRegistry[*ParallelTestState, *ParallelTestSaga]()

	// Register actions for parallel testing
	registerParallelTestActions(t, registry)

	// Get action references from registry
	setupAction, err := registry.Get("setup_action")
	require.NoError(t, err)
	taskAAction, err := registry.Get("task_a_action")
	require.NoError(t, err)
	taskBAction, err := registry.Get("task_b_action")
	require.NoError(t, err)
	cleanupAction, err := registry.Get("cleanup_action")
	require.NoError(t, err)

	// Build DAG with parallel structure:
	// setup -> [taskA, taskB] -> cleanup
	builder := NewDagBuilder[*ParallelTestState, *ParallelTestSaga]("ParallelTestSaga", registry)

	// Level 1: Setup
	err = builder.Append(&ActionNodeKind[*ParallelTestState, *ParallelTestSaga]{
		NodeName: "setup",
		Label:    "Setup Resources",
		Action:   setupAction.(*ActionFunc[*ParallelTestState, *ParallelTestSaga, *ParallelResult]),
	})
	require.NoError(t, err)

	// Level 2: Parallel tasks (using our new variadic API)
	err = builder.AppendParallel(
		&ActionNodeKind[*ParallelTestState, *ParallelTestSaga]{
			NodeName: "taskA",
			Label:    "Execute Task A",
			Action:   taskAAction.(*ActionFunc[*ParallelTestState, *ParallelTestSaga, *ParallelResult]),
		},
		&ActionNodeKind[*ParallelTestState, *ParallelTestSaga]{
			NodeName: "taskB",
			Label:    "Execute Task B", 
			Action:   taskBAction.(*ActionFunc[*ParallelTestState, *ParallelTestSaga, *ParallelResult]),
		},
	)
	require.NoError(t, err)

	// Level 3: Cleanup
	err = builder.Append(&ActionNodeKind[*ParallelTestState, *ParallelTestSaga]{
		NodeName: "cleanup",
		Label:    "Cleanup Resources",
		Action:   cleanupAction.(*ActionFunc[*ParallelTestState, *ParallelTestSaga, *ParallelResult]),
	})
	require.NoError(t, err)

	// Build DAG and create executor
	dag, err := builder.Build()
	require.NoError(t, err)

	sagaDag := NewSagaDag(dag, json.RawMessage(`{}`))
	// Create executor with memory store for testing
	store := NewMemoryStore[*ParallelTestState]()
	sagaID := "test-saga-1"
	executor := NewSagaExecutor(sagaDag, registry, sagaContext, sagaID, store)

	// Execute the saga (sequentially for now, but with parallel dependencies)
	ctx := context.Background()
	err = executor.Execute(ctx)
	require.NoError(t, err, "parallel DAG execution should succeed")

	// Verify execution order respects dependencies
	executionOrder := executor.GetExecutionOrder()
	t.Logf("Execution order: %v", executionOrder)

	// Setup should be first
	assert.Equal(t, "setup_action", executionOrder[0], "setup should execute first")

	// TaskA and TaskB should be next (order doesn't matter since they're parallel)
	parallelTasks := []string{executionOrder[1], executionOrder[2]}
	assert.Contains(t, parallelTasks, "task_a_action", "taskA should execute after setup")
	assert.Contains(t, parallelTasks, "task_b_action", "taskB should execute after setup")

	// Cleanup should be last
	assert.Equal(t, "cleanup_action", executionOrder[3], "cleanup should execute last")

	// Verify final state
	assert.Equal(t, "parallel-test-123", sagaState.ID)
	assert.True(t, sagaState.Completed, "saga should be marked as completed")
	
	// Verify all steps were recorded
	expectedSteps := []string{"setup", "taskA", "taskB", "cleanup"}
	assert.Equal(t, expectedSteps, sagaState.StepResults, "all steps should be recorded in order")

	// Verify execution state
	execState := executor.GetExecutionState()
	for nodeID, execNode := range execState {
		if execNode.NodeName != "" && execNode.NodeName != "start" && execNode.NodeName != "end" {
			assert.Equal(t, ActionStateCompleted, execNode.State,
				"node %d (%s) should be completed", nodeID, execNode.NodeName)
			assert.Nil(t, execNode.Error,
				"node %d (%s) should have no error", nodeID, execNode.NodeName)
		}
	}
}

func TestSagaExecutorParallelWithFailure(t *testing.T) {
	// Create test state that will cause taskB to fail
	sagaState := &ParallelTestState{
		ID:          "parallel-fail-test",
		StepResults: []string{},
		Completed:   false,
	}
	sagaContext := &ParallelTestSaga{State: sagaState}

	// Create action registry
	registry := NewActionRegistry[*ParallelTestState, *ParallelTestSaga]()
	registerParallelTestActions(t, registry)

	// Get action references from registry
	setupAction, err := registry.Get("setup_action")
	require.NoError(t, err)
	taskAAction, err := registry.Get("task_a_action")
	require.NoError(t, err)
	taskBFailAction, err := registry.Get("task_b_fail_action")
	require.NoError(t, err)
	finalAction, err := registry.Get("final_action")
	require.NoError(t, err)

	// Build simple parallel DAG: setup -> [taskA, taskB_fail]
	builder := NewDagBuilder[*ParallelTestState, *ParallelTestSaga]("ParallelFailTest", registry)

	err = builder.Append(&ActionNodeKind[*ParallelTestState, *ParallelTestSaga]{
		NodeName: "setup",
		Label:    "Setup Resources",
		Action:   setupAction.(*ActionFunc[*ParallelTestState, *ParallelTestSaga, *ParallelResult]),
	})
	require.NoError(t, err)

	err = builder.AppendParallel(
		&ActionNodeKind[*ParallelTestState, *ParallelTestSaga]{
			NodeName: "taskA",
			Label:    "Execute Task A",
			Action:   taskAAction.(*ActionFunc[*ParallelTestState, *ParallelTestSaga, *ParallelResult]),
		},
		&ActionNodeKind[*ParallelTestState, *ParallelTestSaga]{
			NodeName: "taskB_fail",
			Label:    "Execute Failing Task B",
			Action:   taskBFailAction.(*ActionFunc[*ParallelTestState, *ParallelTestSaga, *ParallelResult]),
		},
	)
	require.NoError(t, err)

	// Add a final action (even though it won't be reached due to failure)
	err = builder.Append(&ActionNodeKind[*ParallelTestState, *ParallelTestSaga]{
		NodeName: "final",
		Label:    "Final Action",
		Action:   finalAction.(*ActionFunc[*ParallelTestState, *ParallelTestSaga, *ParallelResult]),
	})
	require.NoError(t, err)

	// Build and execute
	dag, err := builder.Build()
	require.NoError(t, err)

	sagaDag := NewSagaDag(dag, json.RawMessage(`{}`))
	// Create executor with memory store for testing
	store := NewMemoryStore[*ParallelTestState]()
	sagaID := "test-saga-2"
	executor := NewSagaExecutor(sagaDag, registry, sagaContext, sagaID, store)

	// Execute - should fail at taskB_fail
	ctx := context.Background()
	err = executor.Execute(ctx)
	require.Error(t, err, "saga should fail when taskB_fail fails")
	assert.Contains(t, err.Error(), "saga failed at node", "error should indicate failure")

	// Verify compensation occurred
	failedNodes := executor.GetFailedNodes()
	assert.True(t, len(failedNodes) > 0, "should have failed nodes")

	// Find and verify setup was undone
	execState := executor.GetExecutionState()
	var setupNode *ExecutionNode
	for _, execNode := range execState {
		if execNode.NodeName == "setup" {
			setupNode = execNode
			break
		}
	}
	require.NotNil(t, setupNode, "should find setup node")
	assert.Equal(t, ActionStateUndone, setupNode.State, "setup should be undone after failure")

	// StepResults should be cleared by compensation
	assert.Empty(t, sagaState.StepResults, "step results should be cleared by undo")
}

// registerParallelTestActions registers actions for parallel testing
func registerParallelTestActions(t *testing.T, registry *ActionRegistry[*ParallelTestState, *ParallelTestSaga]) {
	// Setup action
	setupAction := NewActionFunc[*ParallelTestState, *ParallelTestSaga, *ParallelResult](
		"setup_action",
		func(ctx context.Context, sgctx ActionContext[*ParallelTestState, *ParallelTestSaga]) (ActionFuncResult[*ParallelResult], error) {
			state := sgctx.UserContext
			state.StepResults = append(state.StepResults, "setup")
			
			result := &ParallelResult{
				StepName: "setup",
				Value:    "resources_ready",
			}
			return ActionFuncResult[*ParallelResult]{Output: result}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*ParallelTestState, *ParallelTestSaga]) error {
			state := sgctx.UserContext
			state.StepResults = []string{} // Clear results on undo
			return nil
		},
	)

	// TaskA action  
	taskAAction := NewActionFunc[*ParallelTestState, *ParallelTestSaga, *ParallelResult](
		"task_a_action",
		func(ctx context.Context, sgctx ActionContext[*ParallelTestState, *ParallelTestSaga]) (ActionFuncResult[*ParallelResult], error) {
			state := sgctx.UserContext
			
			// Verify setup completed
			setupResult, found := LookupTyped[*ParallelResult](sgctx, "setup")
			if !found || setupResult.Value != "resources_ready" {
				return ActionFuncResult[*ParallelResult]{}, fmt.Errorf("taskA requires setup to complete first")
			}
			
			state.StepResults = append(state.StepResults, "taskA")
			
			result := &ParallelResult{
				StepName: "taskA",
				Value:    "task_a_completed",
			}
			return ActionFuncResult[*ParallelResult]{Output: result}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*ParallelTestState, *ParallelTestSaga]) error {
			state := sgctx.UserContext
			// Remove taskA from results
			for i, step := range state.StepResults {
				if step == "taskA" {
					state.StepResults = append(state.StepResults[:i], state.StepResults[i+1:]...)
					break
				}
			}
			return nil
		},
	)

	// TaskB action
	taskBAction := NewActionFunc[*ParallelTestState, *ParallelTestSaga, *ParallelResult](
		"task_b_action",
		func(ctx context.Context, sgctx ActionContext[*ParallelTestState, *ParallelTestSaga]) (ActionFuncResult[*ParallelResult], error) {
			state := sgctx.UserContext
			
			// Verify setup completed
			setupResult, found := LookupTyped[*ParallelResult](sgctx, "setup")
			if !found || setupResult.Value != "resources_ready" {
				return ActionFuncResult[*ParallelResult]{}, fmt.Errorf("taskB requires setup to complete first")
			}
			
			state.StepResults = append(state.StepResults, "taskB")
			
			result := &ParallelResult{
				StepName: "taskB",
				Value:    "task_b_completed",
			}
			return ActionFuncResult[*ParallelResult]{Output: result}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*ParallelTestState, *ParallelTestSaga]) error {
			state := sgctx.UserContext
			// Remove taskB from results
			for i, step := range state.StepResults {
				if step == "taskB" {
					state.StepResults = append(state.StepResults[:i], state.StepResults[i+1:]...)
					break
				}
			}
			return nil
		},
	)

	// TaskB fail action (for failure testing)
	taskBFailAction := NewActionFunc[*ParallelTestState, *ParallelTestSaga, *ParallelResult](
		"task_b_fail_action",
		func(ctx context.Context, sgctx ActionContext[*ParallelTestState, *ParallelTestSaga]) (ActionFuncResult[*ParallelResult], error) {
			// Always fail
			return ActionFuncResult[*ParallelResult]{}, fmt.Errorf("taskB_fail intentionally fails")
		},
		func(ctx context.Context, sgctx ActionContext[*ParallelTestState, *ParallelTestSaga]) error {
			// Nothing to undo since it failed
			return nil
		},
	)

	// Cleanup action
	cleanupAction := NewActionFunc[*ParallelTestState, *ParallelTestSaga, *ParallelResult](
		"cleanup_action",
		func(ctx context.Context, sgctx ActionContext[*ParallelTestState, *ParallelTestSaga]) (ActionFuncResult[*ParallelResult], error) {
			state := sgctx.UserContext
			
			// Verify both tasks completed
			taskAResult, foundA := LookupTyped[*ParallelResult](sgctx, "taskA")
			taskBResult, foundB := LookupTyped[*ParallelResult](sgctx, "taskB")
			
			if !foundA || !foundB {
				return ActionFuncResult[*ParallelResult]{}, fmt.Errorf("cleanup requires both taskA and taskB to complete")
			}
			
			if taskAResult.Value != "task_a_completed" || taskBResult.Value != "task_b_completed" {
				return ActionFuncResult[*ParallelResult]{}, fmt.Errorf("cleanup requires both tasks to have succeeded")
			}
			
			state.StepResults = append(state.StepResults, "cleanup")
			state.Completed = true
			
			result := &ParallelResult{
				StepName: "cleanup",
				Value:    "cleanup_completed",
			}
			return ActionFuncResult[*ParallelResult]{Output: result}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*ParallelTestState, *ParallelTestSaga]) error {
			state := sgctx.UserContext
			state.Completed = false
			// Remove cleanup from results
			for i, step := range state.StepResults {
				if step == "cleanup" {
					state.StepResults = append(state.StepResults[:i], state.StepResults[i+1:]...)
					break
				}
			}
			return nil
		},
	)

	// Final action (for DAG structure requirements)
	finalAction := NewActionFunc[*ParallelTestState, *ParallelTestSaga, *ParallelResult](
		"final_action",
		func(ctx context.Context, sgctx ActionContext[*ParallelTestState, *ParallelTestSaga]) (ActionFuncResult[*ParallelResult], error) {
			state := sgctx.UserContext
			state.StepResults = append(state.StepResults, "final")
			
			result := &ParallelResult{
				StepName: "final",
				Value:    "final_completed",
			}
			return ActionFuncResult[*ParallelResult]{Output: result}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*ParallelTestState, *ParallelTestSaga]) error {
			state := sgctx.UserContext
			// Remove final from results
			for i, step := range state.StepResults {
				if step == "final" {
					state.StepResults = append(state.StepResults[:i], state.StepResults[i+1:]...)
					break
				}
			}
			return nil
		},
	)

	// Register all actions
	require.NoError(t, registry.Register(setupAction))
	require.NoError(t, registry.Register(taskAAction))
	require.NoError(t, registry.Register(taskBAction))
	require.NoError(t, registry.Register(taskBFailAction))
	require.NoError(t, registry.Register(cleanupAction))
	require.NoError(t, registry.Register(finalAction))
}