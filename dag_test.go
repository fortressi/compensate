package compensate

import (
	"context"
	"encoding/json"
	"testing"
)

// TODO(morganj): These tests seriously need improvement

// func newGraphIterator(graph simple.DirectedGraph) iter.Seq[graph.Node] {
// 	return func(yield func(n graph.Node) bool) {
// 		for i := len(s) - 1; i >= 0; i-- {
// 			if !yield(i, s[i]) {
// 				return
// 			}
// 		}
// 	}
// }

func TestSagaDagConstruction(t *testing.T) {
	// Create action registry
	registry := NewActionRegistry[*NewTestState, *NewTestSaga]()

	// Create action
	action1 := NewActionFunc[*NewTestState, *NewTestSaga, *SimpleResult](
		"action1",
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) (ActionFuncResult[*SimpleResult], error) {
			return ActionFuncResult[*SimpleResult]{Output: &SimpleResult{Value: "action1"}}, nil
		},
		func(ctx context.Context, sgctx ActionContext[*NewTestState, *NewTestSaga]) error {
			return nil
		},
	)

	// Register action
	registry.Register(action1)

	// Create builder with type parameters and registry
	builder := NewDagBuilder[*NewTestState, *NewTestSaga]("TestSaga", registry)

	// Use new ActionNodeKind API
	actionNode := &ActionNodeKind[*NewTestState, *NewTestSaga]{
		NodeName: "action1",
		Label:    "First Action",
		Action:   action1,
	}

	builder.Append(actionNode)

	dag, err := builder.Build()
	if err != nil {
		t.Fatalf("failed to build DAG: %v", err)
	}

	params := json.RawMessage(`{"param": "start"}`)

	sagaDag := NewSagaDag(dag, params)
	t.Logf("%+v", sagaDag)
}

func TestDagExportToDot(t *testing.T) {
	dag := NewDag("TestSaga")
	startNode := &StartNode{Params: json.RawMessage(`{"param": "start"}`)}
	actionNode := &ActionNodeInternal{
		Name:       "action1",
		LabelValue: "First Action",
		ActionName: "action1",
	}

	dag.AddNode(startNode)
	dag.AddNode(actionNode)

	dotFormat, err := dag.ExportToDot()
	if err != nil {
		t.Fatalf("failed to export DAG to DOT format: %v", err)
	}

	if len(dotFormat) == 0 {
		t.Errorf("Dot format output is empty")
	}
}
