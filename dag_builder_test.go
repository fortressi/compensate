package compensate

import (
	"context"
	"encoding/json"
	"testing"
)

func TestDagBuilder(t *testing.T) {
	// Create registries for both subsaga and main saga
	subsagaRegistry := NewActionRegistry[*NewTestState, *NewTestSaga]()
	subsagaBuilder := NewDagBuilder[*NewTestState, *NewTestSaga]("TestSubsaga", subsagaRegistry)

	// Create test actions
	action1 := NewActionFunc[*NewTestState, *NewTestSaga, *SimpleResult](
		"action1",
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) (ActionResult[*SimpleResult], error) {
			return ActionResult[*SimpleResult]{Output: &SimpleResult{Value: "action1"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) error {
			return nil
		},
	)

	action2 := NewActionFunc[*NewTestState, *NewTestSaga, *SimpleResult](
		"action2",
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) (ActionResult[*SimpleResult], error) {
			return ActionResult[*SimpleResult]{Output: &SimpleResult{Value: "action2"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) error {
			return nil
		},
	)

	action3 := NewActionFunc[*NewTestState, *NewTestSaga, *SimpleResult](
		"action3",
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) (ActionResult[*SimpleResult], error) {
			return ActionResult[*SimpleResult]{Output: &SimpleResult{Value: "action3"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) error {
			return nil
		},
	)

	action6 := NewActionFunc[*NewTestState, *NewTestSaga, *SimpleResult](
		"action6",
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) (ActionResult[*SimpleResult], error) {
			return ActionResult[*SimpleResult]{Output: &SimpleResult{Value: "action6"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) error {
			return nil
		},
	)

	err := subsagaBuilder.Append(&ActionNodeKind[*NewTestState, *NewTestSaga]{
		NodeName: "first stage",
		Label:    "First Saga Action",
		Action:   action1,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = subsagaBuilder.Append(&ActionNodeKind[*NewTestState, *NewTestSaga]{
		NodeName: "second stage",
		Label:    "Second Saga Action",
		Action:   action2,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = subsagaBuilder.Append(&ActionNodeKind[*NewTestState, *NewTestSaga]{
		NodeName: "third stage",
		Label:    "Third Saga Action",
		Action:   action2,
	})
	if err != nil {
		t.Fatal(err)
	}

	subsagaDag, err := subsagaBuilder.Build()
	if err != nil {
		t.Fatalf("failed to build DAG: %v", err)
	}

	sagaRegistry := NewActionRegistry[*NewTestState, *NewTestSaga]()
	sagaBuilder := NewDagBuilder[*NewTestState, *NewTestSaga]("TestSaga", sagaRegistry)

	err = sagaBuilder.Append(&ActionNodeKind[*NewTestState, *NewTestSaga]{
		NodeName: "first stage",
		Label:    "First action before subsaga",
		Action:   action3,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = sagaBuilder.Append(&SubsagaNodeKind{
		NodeName:       "subsaga stage",
		ParamsNodeName: "first stage",
		Dag:            subsagaDag,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = sagaBuilder.Append(&ActionNodeKind[*NewTestState, *NewTestSaga]{
		NodeName: "second stage",
		Label:    "First action after subsaga",
		Action:   action6,
	})
	if err != nil {
		t.Fatal(err)
	}

	sagaDag, err := sagaBuilder.Build()
	if err != nil {
		t.Fatalf("failed to build DAG: %v", err)
	}

	runnableSagaDag := NewSagaDag(sagaDag, json.RawMessage(`{"beans": "cheese"}`))

	dot, err := runnableSagaDag.Graph.ExportToDot()
	if err != nil {
		t.Fatalf("export to dot: %v", err)
	}

	t.Log(dot)

	if len(sagaDag.firstNodes) == 0 {
		t.Errorf("DAG has no root nodes")
	}

	if len(sagaDag.lastNodes) == 0 {
		t.Errorf("DAG has no leaf nodes")
	}
}
