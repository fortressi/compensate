package compensate

import (
	"errors"
	"fmt"

	"github.com/fortressi/compensate/set"
	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/traverse"
)

// DagBuilder builds a DAG that can be executed as a saga or subsaga.
type DagBuilder[T any, S SagaType[T]] struct {
	sagaName SagaName
	dag      *Dag

	// the initial set of nodes (root nodes), if any have been added
	firstAdded []NodeIndex

	/// the most-recently-added set of nodes (current leaf nodes)
	///
	/// Callers use the builder by appending a sequence of nodes (or subsagas).
	/// Some of these may run concurrently.
	///
	/// The `append()`/`append_parallel()` functions are public.  As the names
	/// imply, they append new nodes to the graph.  They also update
	/// "last_added" so that the next set of nodes will depend on the ones that
	/// were just added.
	///
	/// The `add_*()` functions are private, for use only by
	/// `append()`/`append_parallel()`.  These functions have a consistent
	/// pattern: they add nodes to the graph, they create dependencies from
	/// each node in "last_added" to each of the new nodes, and they return the
	/// index of the last node they added.  They do _not_ update "last_added"
	/// themselves.
	lastAdded []NodeIndex

	nodeNames *set.Set[NodeName]
	registry  *ActionRegistry[T, S] // For auto-registering actions
}

// NewDagBuilder creates a new DagBuilder.
func NewDagBuilder[T any, S SagaType[T]](sagaName SagaName, registry *ActionRegistry[T, S]) *DagBuilder[T, S] {
	return &DagBuilder[T, S]{
		sagaName:   sagaName,
		dag:        NewDag(sagaName),
		firstAdded: nil,
		lastAdded:  []NodeIndex{},
		nodeNames:  &set.Set[NodeName]{},
		registry:   registry,
	}
}

// Append adds a single node sequentially to the DAG.
func (b *DagBuilder[T, S]) Append(node Node) error {
	return b.appendParallelSlice([]Node{node})
}

// AppendParallel adds multiple nodes that can run concurrently (variadic API).
func (b *DagBuilder[T, S]) AppendParallel(nodes ...Node) error {
	return b.appendParallelSlice(nodes)
}

// appendParallelSlice adds nodes that can run concurrently (internal implementation).
func (b *DagBuilder[T, S]) appendParallelSlice(userNodes []Node) error {
	// It's not allowed to have an empty stage.  It's not clear what would
	// be intended by this.  With the current implementation, you'd wind up
	// creating two separate connected components of the DAG, which violates
	// all kinds of assumptions.
	if len(userNodes) == 0 {
		return fmt.Errorf("empty stage")
	}

	newNodes := make([]NodeIndex, 0, len(userNodes))
	for _, userNode := range userNodes {
		// Now validate that the names of these new nodes are unique.  It's
		// important that we do this after the above check because otherwise if
		// the user added a subsaga node whose parameters came from a parallel
		// node (added in the same call), we wouldn't catch the problem.
		if b.nodeNames.Contains(userNode.nodeName()) {
			return fmt.Errorf("node with name '%s' already exists", userNode.nodeName())
		} else {
			// We add the nodeNames to the BTreeSet here, not in addInternalNode(), because
			// here we know we're only dealing with user provided nodes. If we did this inside
			// addInternalNodes() then we'd need a type switch to exclude adding the start/end
			// nodes to the set - since not all nodes have a NodeName.
			b.nodeNames.Insert(userNode.nodeName())
		}

		switch n := userNode.(type) {
		case *ActionNodeKind[T, S]:
			// Auto-register the action if not already registered
			actionName := n.Action.Name()
			if _, err := b.registry.Get(actionName); err != nil {
				// Action not registered, register it now
				if regErr := b.registry.Register(n.Action); regErr != nil {
					return fmt.Errorf("failed to register action %s: %w", actionName, regErr)
				}
			}

			internalNode := &ActionNodeInternal{
				Name:       n.NodeName,
				LabelValue: n.Label,
				ActionName: actionName,
			}

			id, err := b.addInternalNode(internalNode)
			if err != nil {
				return err
			}

			newNodes = append(newNodes, id)
		case *ConstantNodeKind:
			internalNode := &ConstantNodeInternal{
				Name: n.NodeName,
				// TODO(morganj): Need to make this work... json stuff...
				// Value: node.Constant,
			}

			id, err := b.addInternalNode(internalNode)
			if err != nil {
				return err
			}

			newNodes = append(newNodes, id)
		case *SubsagaNodeKind:
			idx, err := b.addSubsagaNode(n)
			if err != nil {
				return fmt.Errorf("add subsaga failed: %w", err)
			}

			newNodes = append(newNodes, idx)
		default:
			// this really shouldn't happen...
			panic(fmt.Sprintf("node with unrecognised type: %T", n))
		}
	}

	if len(b.firstAdded) == 0 {
		b.firstAdded = newNodes
	}

	b.setLastAdded(newNodes)

	return nil
}

func (b *DagBuilder[T, S]) addInternalNode(n InternalNode) (NodeIndex, error) {
	// Add the node to the graph
	id := b.dag.AddNode(n)

	err := b.dependsOnLast(id)
	if err != nil {
		return 0, err
	}

	return id, nil
}

// Record that the nodes in `nodes` should be ancestors of whatever nodes
// get added next.
func (b *DagBuilder[T, S]) setLastAdded(nodes []NodeIndex) {
	b.lastAdded = nodes
}

// Record that node `newnode` depends on the last set of nodes that were
// appended
func (b *DagBuilder[T, S]) dependsOnLast(newNodeIndex NodeIndex) error {
	for _, node := range b.lastAdded {
		if err := b.dag.AddEdge(node, newNodeIndex); err != nil {
			return fmt.Errorf("dependsOnLast: %w", err)
		}
	}
	return nil
}

// Adds another DAG to this one as a subsaga
//
// This isn't quite the same as inserting the given DAG into the DAG that
// we're building.  Subsaga nodes live in a separate namespace of node
// names.  We do this by adding the SubsagaStart and SubsagaEnd nodes
// around the given DAG.
func (b *DagBuilder[T, S]) addSubsagaNode(n *SubsagaNodeKind) (NodeIndex, error) {
	// Validate that if we're appending a subsaga, then the node from which
	// it gets its parameters has already been appended.  If it hasn't been
	// appended, this is definitely invalid because we'd have no way to get
	// these parameters when we need them.  As long as the parameters node
	// has been appended already, then the new subsaga node will depend on
	// that node (either directly or indirectly).
	//
	// It might seem more natural to validate this in `add_subsaga()`.  But
	// if we're appending multiple nodes in parallel here, then by the time
	// we get to `add_subsaga()`, it's possible that we've added nodes to
	// the graph on which the subsaga does _not_ depend.  That would cause
	// us to erroneously think this is valid when it's not.
	if !b.nodeNames.Contains(n.ParamsNodeName) {
		return 0, fmt.Errorf("bad subsaga params, node with name '%s' does not exist in dag", n.ParamsNodeName)
	}

	err := validateSubgraph(n.Dag)
	if err != nil {
		return 0, fmt.Errorf("validation error: %w", err)
	}

	subsagaStartNode := &SubsagaStartNode{
		SagaName:       n.Dag.SagaName,
		ParamsNodeName: n.ParamsNodeName,
	}

	subsagaStartNodeID, err := b.addInternalNode(subsagaStartNode)
	if err != nil {
		return 0, fmt.Errorf("adding subsaga node: %w", err)
	}

	subsagaIDToSagaID := b.appendSubgraph(n.Dag)

	// The initial nodes of the subsaga DAG must depend on the SubsagaStart
	// node that we added.
	for childFirstNode := range n.Dag.firstNodes {
		parentFirstNode, found := subsagaIDToSagaID[NodeIndex(childFirstNode)]
		if !found {
			// TODO(morganj): is this the most sensible reaction?
			panic("this is a bug in the framework")
		}
		err := b.dag.AddEdge(subsagaStartNodeID, parentFirstNode)
		if err != nil {
			return 0, fmt.Errorf("unable to add subsagaEndNode edge: %w", err)
		}
	}

	// Add a SubsagaEnd node that depends on the last nodes of the subsaga
	// DAG.
	subsagaEndNodeID, err := b.addInternalNode(&SubsagaEndNode{Name: n.NodeName})
	if err != nil {
		return 0, fmt.Errorf("adding subsaga node: %w", err)
	}
	for _, childLastNode := range n.Dag.lastNodes {
		parentLastNode, found := subsagaIDToSagaID[NodeIndex(childLastNode)]
		if !found {
			// TODO(morganj): is this the most sensible reaction?
			panic("this is a bug in the framework")
		}
		err := b.dag.AddEdge(parentLastNode, subsagaEndNodeID)
		if err != nil {
			return 0, fmt.Errorf("unable to add subsagaEndNode edge: %w", err)
		}
	}

	return subsagaEndNodeID, nil
}

// Build finalizes the DAG construction and returns the DAG.
func (b *DagBuilder[T, S]) Build() (*Dag, error) {
	dag := &Dag{
		SagaName: b.sagaName,
		Graph:    b.dag.Graph,
		nodes:    b.dag.nodes,
	}

	if len(b.firstAdded) == 0 {
		return nil, fmt.Errorf("DAG has no root nodes")
	} else {
		dag.firstNodes = NodeIndexSliceToInt64Slice(b.firstAdded)
	}

	if len(b.lastAdded) != 1 {
		return dag, errors.New("DAG must end with exactly one leaf node")
	} else {
		dag.lastNodes = NodeIndexSliceToInt64Slice(b.lastAdded)
	}

	return dag, nil
}

// appendSubgraph appends some subgraph to the graph in DagBuilder. It maintains a
// map[subgraphID]rootGraphID to support later manipulations with the original graph
// node IDs as context.
func (b *DagBuilder[T, S]) appendSubgraph(subgraph *Dag) map[NodeIndex]NodeIndex {
	subsagaIDToSagaID := make(map[NodeIndex]NodeIndex)

	visitor := b.newNodeVisitor(subgraph, subsagaIDToSagaID)
	walker := traverse.BreadthFirst{
		// Visit is called on all nodes on their first visit.
		Visit: visitor,
	}

	id := walker.Walk(subgraph.DirectedGraph, simple.Node(0), nil)
	fmt.Println(id)

	return subsagaIDToSagaID
}

func validateSubgraph(subgraph *Dag) error {
	nodes := subgraph.DirectedGraph.Nodes()
	for nodes.Next() {
		node := subgraph.nodes[nodes.Node().ID()]

		// Dags are not allowed to have Start/End nodes.  These are only
		// added to `SagaDag`s. Given that, we can copy the rest of the
		// nodes directly into the parent graph.
		switch node.(type) {
		case *StartNode, *EndNode, *SubsagaStartNode, *SubsagaEndNode:
			return fmt.Errorf("subsaga Dag contained unexpected node: %+v", node)
		}

	}

	return nil
}

func (b *DagBuilder[T, S]) newNodeVisitor(subgraph *Dag, subsagaIDToSagaID map[NodeIndex]NodeIndex) func(node graph.Node) {
	return func(node graph.Node) {
		childNodeIndex := NodeIndex(node.ID())
		childNode := subgraph.nodes[childNodeIndex.ToInt64()]

		// append the node
		parentNodeIndex := b.dag.AddNode(childNode)

		// fmt.Printf("updating subsagaIDToSagaID[%d] = %d\n", childNodeIndex, parentNodeIndex)
		subsagaIDToSagaID[childNodeIndex] = parentNodeIndex

		// For any incoming edges for this node in the subgraph, create a
		// corresponding edge in the new graph.
		childNodeNeighbours := subgraph.DirectedGraph.To(int64(childNodeIndex))
		for childNodeNeighbours.Next() {
			subsagaNodeIndex := NodeIndex(childNodeNeighbours.Node().ID())
			sagaNodeIndex, found := subsagaIDToSagaID[subsagaNodeIndex]
			// fmt.Printf("retrieving subsagaIDToSagaID[%d] = %d\n", subsagaNodeIndex, sagaNodeIndex)
			if !found {
				panic("this is a bug in the framework")
			}

			err := b.dag.AddEdge(sagaNodeIndex, parentNodeIndex)
			if err != nil {
				panic("this is a bug in the framework")
			}
		}
	}
}
