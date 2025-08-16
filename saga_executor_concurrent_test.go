package compensate

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConcurrentExecutionTiming verifies that concurrent execution is faster than sequential
func TestConcurrentExecutionTiming(t *testing.T) {
	// Create a DAG with parallel slow actions
	// setup -> [slowA (500ms), slowB (500ms)] -> cleanup
	
	registry := NewActionRegistry[*TestState, *TestSaga]()
	
	// Create slow actions that sleep
	slowActionA := NewActionFunc[*TestState, *TestSaga, string](
		"slow_a",
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) (ActionResult[string], error) {
			time.Sleep(300 * time.Millisecond)
			return NewActionResult("slowA done"), nil
		},
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) error { return nil },
	)
	
	slowActionB := NewActionFunc[*TestState, *TestSaga, string](
		"slow_b",
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) (ActionResult[string], error) {
			time.Sleep(300 * time.Millisecond)
			return NewActionResult("slowB done"), nil
		},
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) error { return nil },
	)
	
	setupAction := NewActionFunc[*TestState, *TestSaga, string](
		"setup",
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) (ActionResult[string], error) {
			return NewActionResult("setup done"), nil
		},
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) error { return nil },
	)
	
	cleanupAction := NewActionFunc[*TestState, *TestSaga, string](
		"cleanup",
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) (ActionResult[string], error) {
			return NewActionResult("cleanup done"), nil
		},
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) error { return nil },
	)
	
	// Register actions
	require.NoError(t, registry.Register(slowActionA))
	require.NoError(t, registry.Register(slowActionB))
	require.NoError(t, registry.Register(setupAction))
	require.NoError(t, registry.Register(cleanupAction))
	
	// Build DAG
	builder := NewDagBuilder[*TestState, *TestSaga]("ConcurrentTimingTest", registry)
	
	err := builder.Append(&ActionNodeKind[*TestState, *TestSaga]{
		NodeName: "setup",
		Action:   setupAction,
	})
	require.NoError(t, err)
	
	err = builder.AppendParallel(
		&ActionNodeKind[*TestState, *TestSaga]{
			NodeName: "slowA",
			Action:   slowActionA,
		},
		&ActionNodeKind[*TestState, *TestSaga]{
			NodeName: "slowB",
			Action:   slowActionB,
		},
	)
	require.NoError(t, err)
	
	err = builder.Append(&ActionNodeKind[*TestState, *TestSaga]{
		NodeName: "cleanup",
		Action:   cleanupAction,
	})
	require.NoError(t, err)
	
	dag, err := builder.Build()
	require.NoError(t, err)
	
	sagaDag := NewSagaDag(dag, nil)
	
	// Test state and saga
	state := &TestState{Counter: 0}
	saga := &TestSaga{State: state}
	store := NewMemoryStore[*TestState]()
	
	// Time sequential execution
	executor := NewSagaExecutor(sagaDag, registry, saga, "test-sequential", store)
	
	startSeq := time.Now()
	err = executor.Execute(context.Background())
	sequentialDuration := time.Since(startSeq)
	require.NoError(t, err)
	
	// Reset for concurrent execution
	state.Counter = 0
	executor = NewSagaExecutor(sagaDag, registry, saga, "test-concurrent", store)
	
	// Time concurrent execution
	startConc := time.Now()
	err = executor.ExecuteConcurrent(context.Background())
	concurrentDuration := time.Since(startConc)
	require.NoError(t, err)
	
	// Concurrent should be significantly faster (close to 300ms vs 600ms)
	// We use 0.8 as a safety margin for CI environments
	assert.True(t, concurrentDuration < time.Duration(float64(sequentialDuration)*0.8),
		"concurrent execution (%.0fms) should be faster than sequential (%.0fms)",
		concurrentDuration.Milliseconds(), sequentialDuration.Milliseconds())
	
	t.Logf("Sequential: %dms, Concurrent: %dms, Speedup: %.2fx",
		sequentialDuration.Milliseconds(),
		concurrentDuration.Milliseconds(),
		float64(sequentialDuration)/float64(concurrentDuration))
}

// TestConcurrentExecutionWithFailure tests that when one action fails, others are cancelled
func TestConcurrentExecutionWithFailure(t *testing.T) {
	var actionACancelled, actionBFailed atomic.Bool
	
	registry := NewActionRegistry[*TestState, *TestSaga]()
	
	actionA := NewActionFunc[*TestState, *TestSaga, string](
		"action_a",
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) (ActionResult[string], error) {
			// Simulate work that can be cancelled
			select {
			case <-time.After(1 * time.Second):
				return NewActionResult("completed"), nil
			case <-ctx.Done():
				actionACancelled.Store(true)
				return ActionResult[string]{}, ctx.Err()
			}
		},
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) error { 
			// Verify undo is called
			return nil 
		},
	)
	
	actionB := NewActionFunc[*TestState, *TestSaga, string](
		"action_b",
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) (ActionResult[string], error) {
			// Fail quickly
			time.Sleep(100 * time.Millisecond)
			actionBFailed.Store(true)
			return ActionResult[string]{}, fmt.Errorf("actionB failed")
		},
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) error { return nil },
	)
	
	setupAction := NewActionFunc[*TestState, *TestSaga, string](
		"setup",
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) (ActionResult[string], error) {
			return NewActionResult("setup done"), nil
		},
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) error { 
			// This should be called during compensation
			sgctx.UserContext.Counter = -1 // Mark as undone
			return nil 
		},
	)
	
	// Register actions
	require.NoError(t, registry.Register(actionA))
	require.NoError(t, registry.Register(actionB))
	require.NoError(t, registry.Register(setupAction))
	
	// Build DAG with parallel actions
	builder := NewDagBuilder[*TestState, *TestSaga]("ConcurrentFailureTest", registry)
	
	err := builder.Append(&ActionNodeKind[*TestState, *TestSaga]{
		NodeName: "setup",
		Action:   setupAction,
	})
	require.NoError(t, err)
	
	err = builder.AppendParallel(
		&ActionNodeKind[*TestState, *TestSaga]{
			NodeName: "actionA",
			Action:   actionA,
		},
		&ActionNodeKind[*TestState, *TestSaga]{
			NodeName: "actionB",
			Action:   actionB,
		},
	)
	require.NoError(t, err)
	
	// Add a final node to satisfy DAG requirements
	finalAction := NewActionFunc[*TestState, *TestSaga, string](
		"final",
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) (ActionResult[string], error) {
			return NewActionResult("final done"), nil
		},
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) error { return nil },
	)
	require.NoError(t, registry.Register(finalAction))
	
	err = builder.Append(&ActionNodeKind[*TestState, *TestSaga]{
		NodeName: "final",
		Action:   finalAction,
	})
	require.NoError(t, err)
	
	dag, err := builder.Build()
	require.NoError(t, err)
	
	sagaDag := NewSagaDag(dag, nil)
	
	// Test state and saga
	state := &TestState{Counter: 0}
	saga := &TestSaga{State: state}
	store := NewMemoryStore[*TestState]()
	
	executor := NewSagaExecutor(sagaDag, registry, saga, "test-failure", store)
	
	err = executor.ExecuteConcurrent(context.Background())
	require.Error(t, err)
	
	// Verify actionA was cancelled and actionB failed
	assert.True(t, actionACancelled.Load(), "actionA should have been cancelled")
	assert.True(t, actionBFailed.Load(), "actionB should have failed")
	
	// Verify compensation occurred
	assert.Equal(t, -1, state.Counter, "setup should have been undone")
}

// TestConcurrencyLimit tests that concurrency limit is respected
func TestConcurrencyLimit(t *testing.T) {
	const maxConcurrency = 2
	var currentlyRunning atomic.Int32
	var maxObserved int32
	
	registry := NewActionRegistry[*TestState, *TestSaga]()
	
	// Create many parallel actions
	actions := make([]Action[*TestState, *TestSaga], 10)
	for i := 0; i < 10; i++ {
		actionName := fmt.Sprintf("action_%d", i)
		actions[i] = NewActionFunc[*TestState, *TestSaga, string](
			ActionName(actionName),
			func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) (ActionResult[string], error) {
				// Track concurrent executions
				current := currentlyRunning.Add(1)
				defer currentlyRunning.Add(-1)
				
				// Update max observed
				for {
					max := atomic.LoadInt32(&maxObserved)
					if current <= max || atomic.CompareAndSwapInt32(&maxObserved, max, current) {
						break
					}
				}
				
				// Simulate work
				time.Sleep(100 * time.Millisecond)
				return NewActionResult("done"), nil
			},
			func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) error { return nil },
		)
		require.NoError(t, registry.Register(actions[i]))
	}
	
	// Build DAG with all actions in parallel
	builder := NewDagBuilder[*TestState, *TestSaga]("ConcurrencyLimitTest", registry)
	
	// Create parallel nodes one by one
	nodes := make([]*ActionNodeKind[*TestState, *TestSaga], 10)
	for i := 0; i < 10; i++ {
		nodes[i] = &ActionNodeKind[*TestState, *TestSaga]{
			NodeName: NodeName(fmt.Sprintf("action_%d", i)),
			Action:   actions[i].(*ActionFunc[*TestState, *TestSaga, string]),
		}
	}
	
	// AppendParallel takes variadic arguments, not a slice
	err := builder.AppendParallel(
		nodes[0], nodes[1], nodes[2], nodes[3], nodes[4],
		nodes[5], nodes[6], nodes[7], nodes[8], nodes[9],
	)
	require.NoError(t, err)
	
	// Add a final node to satisfy DAG requirements
	finalAction := NewActionFunc[*TestState, *TestSaga, string](
		"final",
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) (ActionResult[string], error) {
			return NewActionResult("final done"), nil
		},
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) error { return nil },
	)
	require.NoError(t, registry.Register(finalAction))
	
	err = builder.Append(&ActionNodeKind[*TestState, *TestSaga]{
		NodeName: "final",
		Action:   finalAction,
	})
	require.NoError(t, err)
	
	dag, err := builder.Build()
	require.NoError(t, err)
	
	sagaDag := NewSagaDag(dag, nil)
	
	// Test state and saga
	state := &TestState{Counter: 0}
	saga := &TestSaga{State: state}
	store := NewMemoryStore[*TestState]()
	
	executor := NewSagaExecutor(sagaDag, registry, saga, "test-limit", store)
	executor.SetMaxConcurrency(maxConcurrency)
	
	err = executor.ExecuteConcurrent(context.Background())
	require.NoError(t, err)
	
	// Verify concurrency was limited
	assert.LessOrEqual(t, maxObserved, int32(maxConcurrency),
		"should not exceed max concurrency of %d", maxConcurrency)
	
	t.Logf("Max concurrent executions observed: %d (limit: %d)", maxObserved, maxConcurrency)
}

// TestConcurrentExecutionWithLogging tests structured logging during concurrent execution
func TestConcurrentExecutionWithLogging(t *testing.T) {
	registry := NewActionRegistry[*TestState, *TestSaga]()
	
	// Create actions
	action1 := NewActionFunc[*TestState, *TestSaga, string](
		"action_1",
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) (ActionResult[string], error) {
			return NewActionResult("action1 done"), nil
		},
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) error { return nil },
	)
	
	action2 := NewActionFunc[*TestState, *TestSaga, string](
		"action_2",
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) (ActionResult[string], error) {
			return NewActionResult("action2 done"), nil
		},
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) error { return nil },
	)
	
	// Register actions
	require.NoError(t, registry.Register(action1))
	require.NoError(t, registry.Register(action2))
	
	// Build simple parallel DAG
	builder := NewDagBuilder[*TestState, *TestSaga]("LoggingTest", registry)
	
	err := builder.AppendParallel(
		&ActionNodeKind[*TestState, *TestSaga]{
			NodeName: "action1",
			Action:   action1,
		},
		&ActionNodeKind[*TestState, *TestSaga]{
			NodeName: "action2",
			Action:   action2,
		},
	)
	require.NoError(t, err)
	
	// Add a final node to satisfy DAG requirements
	finalAction := NewActionFunc[*TestState, *TestSaga, string](
		"final",
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) (ActionResult[string], error) {
			return NewActionResult("final done"), nil
		},
		func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) error { return nil },
	)
	require.NoError(t, registry.Register(finalAction))
	
	err = builder.Append(&ActionNodeKind[*TestState, *TestSaga]{
		NodeName: "final",
		Action:   finalAction,
	})
	require.NoError(t, err)
	
	dag, err := builder.Build()
	require.NoError(t, err)
	
	sagaDag := NewSagaDag(dag, nil)
	
	// Test state and saga
	state := &TestState{Counter: 0}
	saga := &TestSaga{State: state}
	store := NewMemoryStore[*TestState]()
	
	executor := NewSagaExecutor(sagaDag, registry, saga, "test-logging", store)
	
	// Enable default logging (will use slog.Default())
	// In a real test, you might configure a custom logger to capture output
	executor.EnableDefaultLogging()
	
	err = executor.ExecuteConcurrent(context.Background())
	require.NoError(t, err)
	
	// Verify execution completed successfully
	execState := executor.GetExecutionState()
	for _, node := range execState {
		if node.NodeName == "action1" || node.NodeName == "action2" {
			assert.Equal(t, ActionStateCompleted, node.State)
		}
	}
}

// TestConcurrentExecutionMultipleLevels tests a more complex DAG with multiple concurrent levels
func TestConcurrentExecutionMultipleLevels(t *testing.T) {
	// Create a DAG: setup -> [A, B] -> [C, D, E] -> final
	registry := NewActionRegistry[*TestState, *TestSaga]()
	
	// Track execution order
	var executionOrder []string
	var orderMutex sync.Mutex
	
	createAction := func(name string) Action[*TestState, *TestSaga] {
		return NewActionFunc[*TestState, *TestSaga, string](
			ActionName(name),
			func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) (ActionResult[string], error) {
				orderMutex.Lock()
				executionOrder = append(executionOrder, name)
				orderMutex.Unlock()
				
				time.Sleep(50 * time.Millisecond) // Small delay to test concurrency
				return NewActionResult(name + " done"), nil
			},
			func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) error { return nil },
		)
	}
	
	// Create and register all actions
	setupAction := createAction("setup")
	actionA := createAction("A")
	actionB := createAction("B")
	actionC := createAction("C")
	actionD := createAction("D")
	actionE := createAction("E")
	finalAction := createAction("final")
	
	for _, action := range []Action[*TestState, *TestSaga]{
		setupAction, actionA, actionB, actionC, actionD, actionE, finalAction,
	} {
		require.NoError(t, registry.Register(action))
	}
	
	// Build complex DAG
	builder := NewDagBuilder[*TestState, *TestSaga]("ComplexConcurrentTest", registry)
	
	// Level 0: setup
	err := builder.Append(&ActionNodeKind[*TestState, *TestSaga]{
		NodeName: "setup",
		Action:   setupAction.(*ActionFunc[*TestState, *TestSaga, string]),
	})
	require.NoError(t, err)
	
	// Level 1: [A, B]
	err = builder.AppendParallel(
		&ActionNodeKind[*TestState, *TestSaga]{
			NodeName: "A",
			Action:   actionA.(*ActionFunc[*TestState, *TestSaga, string]),
		},
		&ActionNodeKind[*TestState, *TestSaga]{
			NodeName: "B",
			Action:   actionB.(*ActionFunc[*TestState, *TestSaga, string]),
		},
	)
	require.NoError(t, err)
	
	// Level 2: [C, D, E]
	err = builder.AppendParallel(
		&ActionNodeKind[*TestState, *TestSaga]{
			NodeName: "C",
			Action:   actionC.(*ActionFunc[*TestState, *TestSaga, string]),
		},
		&ActionNodeKind[*TestState, *TestSaga]{
			NodeName: "D",
			Action:   actionD.(*ActionFunc[*TestState, *TestSaga, string]),
		},
		&ActionNodeKind[*TestState, *TestSaga]{
			NodeName: "E",
			Action:   actionE.(*ActionFunc[*TestState, *TestSaga, string]),
		},
	)
	require.NoError(t, err)
	
	// Level 3: final
	err = builder.Append(&ActionNodeKind[*TestState, *TestSaga]{
		NodeName: "final",
		Action:   finalAction.(*ActionFunc[*TestState, *TestSaga, string]),
	})
	require.NoError(t, err)
	
	dag, err := builder.Build()
	require.NoError(t, err)
	
	sagaDag := NewSagaDag(dag, nil)
	
	// Test state and saga
	state := &TestState{Counter: 0}
	saga := &TestSaga{State: state}
	store := NewMemoryStore[*TestState]()
	
	executor := NewSagaExecutor(sagaDag, registry, saga, "test-multilevel", store)
	
	err = executor.ExecuteConcurrent(context.Background())
	require.NoError(t, err)
	
	// Verify execution order respects levels
	t.Logf("Execution order: %v", executionOrder)
	
	// Find positions of each action
	positions := make(map[string]int)
	for i, action := range executionOrder {
		positions[action] = i
	}
	
	// Verify level ordering
	assert.Less(t, positions["setup"], positions["A"], "setup should execute before A")
	assert.Less(t, positions["setup"], positions["B"], "setup should execute before B")
	
	assert.Less(t, positions["A"], positions["C"], "A should execute before C")
	assert.Less(t, positions["A"], positions["D"], "A should execute before D")
	assert.Less(t, positions["A"], positions["E"], "A should execute before E")
	assert.Less(t, positions["B"], positions["C"], "B should execute before C")
	assert.Less(t, positions["B"], positions["D"], "B should execute before D")
	assert.Less(t, positions["B"], positions["E"], "B should execute before E")
	
	assert.Less(t, positions["C"], positions["final"], "C should execute before final")
	assert.Less(t, positions["D"], positions["final"], "D should execute before final")
	assert.Less(t, positions["E"], positions["final"], "E should execute before final")
}