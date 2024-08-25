package compensate

// Node represents a node in the saga DAG.
//
// There are three kinds of nodes you can add to a graph:
//
//   - an _action_ (see NewActionNode), which executes a particular Action
//     with an associated undo action
//   - a _constant_ (see NewConstantNode), which is like an action that
//     outputs a value that's known when the DAG is constructed
//   - a _subsaga_ (see NewSubsagaNode), which executes another DAG in the
//     context of this saga
//
// Each of these node types has a `node_name` and produces an output.  Other
// nodes that depend on this node (directly or indirectly) can access the
// output by looking it up by the node name using
// `ActionContext.Lookup`:
//
//   - The output of an action node is emitted by the action itself.
//   - The output of a constant node is the value provided when the node was
//     created (see NewConstantNode).
//   - The output of a subsaga node is the output of the subsaga itself.  Note
//     that the output of individual nodes from the subsaga DAG is _not_
//     available to other nodes in this DAG.  Only the final output is available.
type Node interface {
	nodeName() NodeName
	isValidNodeKind()
}

// type Node[T ValidNodeKinds] struct {
// 	NodeName NodeName
// 	Kind     T
// }

// NodeKind represents the type of a Node.
type NodeKind struct {
	// Action holds the action details for an Action node.
	Action *struct {
		Label      string
		ActionName ActionName
	}
	// Constant holds the constant value for a Constant node.
	Constant *ActionData
	// Subsaga holds the subsaga details for a Subsaga node.
	Subsaga *struct {
		ParamsNodeName NodeName
		Dag            *Dag
	}
}

type ActionNodeKind[T any, S SagaType[T]] struct {
	NodeName NodeName
	Action   Action[T, S]
	Label    string
}

func (a *ActionNodeKind[T, S]) isValidNodeKind() {}
func (a *ActionNodeKind[T, S]) nodeName() NodeName {
	return a.NodeName
}

type ConstantNodeKind struct {
	NodeName NodeName
	Label    string
	Constant *ActionData
}

func (a *ConstantNodeKind) isValidNodeKind() {}
func (a *ConstantNodeKind) nodeName() NodeName {
	return a.NodeName
}

type SubsagaNodeKind struct {
	NodeName       NodeName
	ParamsNodeName NodeName
	Dag            *Dag
}

func (a *SubsagaNodeKind) isValidNodeKind() {}
func (a *SubsagaNodeKind) nodeName() NodeName {
	return a.NodeName
}
