package compensate

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test types for execution order testing
type SimpleState struct {
	ID     string   `json:"id"`
	Values []string `json:"values"`
}

type SimpleSaga struct {
	State *SimpleState
}

func (s *SimpleSaga) ExecContext() *SimpleState {
	return s.State
}

type TestState struct {
	Counter int `json:"counter"`
}

type TestSaga struct {
	State *TestState
}

func (s *TestSaga) ExecContext() *TestState {
	return s.State
}

// Test to specifically validate that actions run in dependency order
func TestExecutionOrderRespectsDependencies(t *testing.T) {
	state := &SimpleState{ID: "test", Values: []string{}}
	saga := &SimpleSaga{State: state}
	
	// Create registry
	registry := NewActionRegistry[*SimpleState, *SimpleSaga]()
	
	// Create actions that depend on each other
	// A -> B -> C (C depends on B, B depends on A)
	
	actionA := NewActionFunc[*SimpleState, *SimpleSaga, string](
		"action_a",
		func(ctx context.Context, sgctx ActionContext[*SimpleState, *SimpleSaga]) (ActionResult[string], error) {
			sgctx.UserContext.Values = append(sgctx.UserContext.Values, "A")
			return ActionResult[string]{Output: "result_a"}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*SimpleState, *SimpleSaga]) error {
			return nil
		},
	)
	
	actionB := NewActionFunc[*SimpleState, *SimpleSaga, string](
		"action_b", 
		func(ctx context.Context, sgctx ActionContext[*SimpleState, *SimpleSaga]) (ActionResult[string], error) {
			// B depends on A - verify A executed first
			resultA, found := LookupTyped[string](sgctx, "nodeA")
			if !found {
				return ActionResult[string]{}, fmt.Errorf("action B requires action A to run first")
			}
			if resultA != "result_a" {
				return ActionResult[string]{}, fmt.Errorf("action B got unexpected result from A: %s", resultA)
			}
			
			sgctx.UserContext.Values = append(sgctx.UserContext.Values, "B")
			return ActionResult[string]{Output: "result_b"}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*SimpleState, *SimpleSaga]) error {
			return nil
		},
	)
	
	actionC := NewActionFunc[*SimpleState, *SimpleSaga, string](
		"action_c",
		func(ctx context.Context, sgctx ActionContext[*SimpleState, *SimpleSaga]) (ActionResult[string], error) {
			// C depends on B - verify B executed first  
			resultB, found := LookupTyped[string](sgctx, "nodeB")
			if !found {
				return ActionResult[string]{}, fmt.Errorf("action C requires action B to run first")
			}
			if resultB != "result_b" {
				return ActionResult[string]{}, fmt.Errorf("action C got unexpected result from B: %s", resultB)
			}
			
			sgctx.UserContext.Values = append(sgctx.UserContext.Values, "C")
			return ActionResult[string]{Output: "result_c"}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*SimpleState, *SimpleSaga]) error {
			return nil
		},
	)
	
	// Register actions
	require.NoError(t, registry.Register(actionA))
	require.NoError(t, registry.Register(actionB))
	require.NoError(t, registry.Register(actionC))
	
	// Build DAG with explicit dependencies: A -> B -> C
	builder := NewDagBuilder[*SimpleState, *SimpleSaga]("DependencyTest", registry)
	
	err := builder.Append(&ActionNodeKind[*SimpleState, *SimpleSaga]{
		NodeName: "nodeA",
		Label:    "Action A",
		Action:   actionA,
	})
	require.NoError(t, err)
	
	err = builder.Append(&ActionNodeKind[*SimpleState, *SimpleSaga]{
		NodeName: "nodeB", 
		Label:    "Action B (depends on A)",
		Action:   actionB,
	})
	require.NoError(t, err)
	
	err = builder.Append(&ActionNodeKind[*SimpleState, *SimpleSaga]{
		NodeName: "nodeC",
		Label:    "Action C (depends on B)", 
		Action:   actionC,
	})
	require.NoError(t, err)
	
	// Build and execute
	dag, err := builder.Build()
	require.NoError(t, err)
	
	sagaDag := NewSagaDag(dag, json.RawMessage(`{}`))
	// Create executor with memory store for testing
	store := NewMemoryStore[*SimpleState]()
	sagaID := "test-saga-1"
	executor := NewSagaExecutor(sagaDag, registry, saga, sagaID, store)
	
	ctx := context.Background()
	err = executor.Execute(ctx)
	require.NoError(t, err, "execution should succeed when dependencies are respected")
	
	// Verify execution order - map action names to letters for easier comparison
	expectedOrder := []string{"action_a", "action_b", "action_c"}
	actualOrder := executor.GetExecutionOrder()
	assert.Equal(t, expectedOrder, actualOrder, "actions must execute in dependency order A -> B -> C")
	
	// Verify state was updated in order
	expectedValues := []string{"A", "B", "C"}
	assert.Equal(t, expectedValues, state.Values, "saga state should reflect sequential execution")
	
	// Verify all actions completed
	execState := executor.GetExecutionState()
	actionNodes := 0
	for _, execNode := range execState {
		if execNode.NodeName != "" && execNode.NodeName != "start" && execNode.NodeName != "end" {
			assert.Equal(t, ActionStateCompleted, execNode.State, 
				"action %s should be completed", execNode.NodeName)
			actionNodes++
		}
	}
	assert.Equal(t, 3, actionNodes, "should have exactly 3 action nodes")
}

func TestExecutionOrderValidatesCurrentImplementation(t *testing.T) {
	// This test documents the current behavior of our simple topological sort
	// When we improve the DAG traversal algorithm, this test may need to be updated
	
	
	state := &TestState{Counter: 0}
	saga := &TestSaga{State: state}
	registry := NewActionRegistry[*TestState, *TestSaga]()
	
	// Create 3 actions that don't have strict dependencies
	for i, name := range []string{"first", "second", "third"} {
		actionName := name
		actionIndex := i
		action := NewActionFunc[*TestState, *TestSaga, int](
			ActionName(actionName),
			func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) (ActionResult[int], error) {
				sgctx.UserContext.Counter += actionIndex + 1
				return ActionResult[int]{Output: actionIndex}, nil
			},
			func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) error {
				return nil
			},
		)
		require.NoError(t, registry.Register(action))
	}
	
	// Build DAG
	builder := NewDagBuilder[*TestState, *TestSaga]("CurrentBehaviorTest", registry)
	actions := make(map[string]*ActionFunc[*TestState, *TestSaga, int])
	
	// Get action references
	for _, name := range []string{"first", "second", "third"} {
		action, err := registry.Get(ActionName(name))
		require.NoError(t, err)
		actions[name] = action.(*ActionFunc[*TestState, *TestSaga, int])
	}
	
	for _, name := range []string{"first", "second", "third"} {
		err := builder.Append(&ActionNodeKind[*TestState, *TestSaga]{
			NodeName: NodeName(name),
			Label:    fmt.Sprintf("Action %s", name),
			Action:   actions[name],
		})
		require.NoError(t, err)
	}
	
	dag, err := builder.Build()
	require.NoError(t, err)
	
	sagaDag := NewSagaDag(dag, json.RawMessage(`{}`))
	// Create executor with memory store for testing
	store := NewMemoryStore[*TestState]()
	sagaID := "test-saga-2"
	executor := NewSagaExecutor(sagaDag, registry, saga, sagaID, store)
	
	err = executor.Execute(context.Background())
	require.NoError(t, err)
	
	// Document current behavior: our simple algorithm executes in DAG node order
	// This will help us verify when we implement proper topological sorting
	actualOrder := executor.GetExecutionOrder()
	t.Logf("Current execution order: %v", actualOrder)
	
	// Verify that all actions executed
	assert.Len(t, actualOrder, 3, "all 3 actions should execute")
	assert.Contains(t, actualOrder, "first", "first action should execute")
	assert.Contains(t, actualOrder, "second", "second action should execute") 
	assert.Contains(t, actualOrder, "third", "third action should execute")
	
	// Verify final state
	assert.Equal(t, 6, state.Counter, "counter should be 1+2+3=6") // 1+2+3=6
}