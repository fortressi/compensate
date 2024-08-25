package compensate

import (
	"encoding/json"
	"fmt"

	"github.com/fortressi/compensate/dag"
	"gonum.org/v1/gonum/graph/simple"
)

// SagaDag represents a saga DAG that is built on top of a regular Dag.
type SagaDag struct {
	Graph     *dag.Graph
	SagaName  SagaName
	StartNode int64
	EndNode   int64
	Nodes     map[int64]InternalNode // Access nodes by gonumNode ID
}

// NewSagaDag creates a new SagaDag by wrapping the DAG with Start and End nodes.
func NewSagaDag(dag *Dag, params json.RawMessage) *SagaDag {
	sagaDag := &SagaDag{
		SagaName: dag.SagaName,
		Graph:    dag.Graph,
		Nodes:    dag.nodes,
	}

	// Add Start and End nodes
	startNode := sagaDag.Graph.NewNode()
	sagaDag.Graph.AddNode(startNode)
	sagaDag.Nodes[startNode.ID()] = &StartNode{Params: params}
	sagaDag.StartNode = startNode.ID()

	endNode := sagaDag.Graph.NewNode()
	sagaDag.Graph.AddNode(endNode)
	sagaDag.Nodes[endNode.ID()] = &EndNode{}
	sagaDag.EndNode = endNode.ID()

	// Connect the Start node to the first nodes
	for _, firstNode := range dag.firstNodes {
		sagaDag.Graph.SetEdge(simple.Edge{F: dag.Node(startNode.ID()), T: dag.Node(firstNode)})
	}

	// Connect the last nodes to the End node
	for _, lastNode := range dag.lastNodes {
		sagaDag.Graph.SetEdge(simple.Edge{F: dag.Node(lastNode), T: dag.Node(endNode.ID())})
	}

	return sagaDag
}

// GetSagaName returns the saga name.
func (s *SagaDag) GetSagaName() SagaName {
	return s.SagaName
}

// GetNode returns a node given its index.
func (s *SagaDag) GetNode(nodeID int64) (InternalNode, error) {
	node, exists := s.Nodes[nodeID]
	if !exists {
		return nil, fmt.Errorf("node not found: %d", nodeID)
	}
	return node, nil
}

// GetNodeIndex returns the index for a given node name.
func (s *SagaDag) GetNodeIndex(name string) (int64, error) {
	for id, node := range s.Nodes {
		if nodeName := node.NodeName(); nodeName != nil && string(*nodeName) == name {
			return id, nil
		}
	}
	return 0, fmt.Errorf("saga has no node named %q", name)
}

// GetNodes returns an iterator over all named nodes in the saga DAG.
func (s *SagaDag) GetNodes() *SagaDagIterator {
	return NewSagaDagIterator(s)
}

// SagaDagIterator iterates over the named nodes in a SagaDag.
type SagaDagIterator struct {
	dag   *SagaDag
	index int64
}

// NewSagaDagIterator creates a new iterator for a SagaDag.
func NewSagaDagIterator(dag *SagaDag) *SagaDagIterator {
	return &SagaDagIterator{
		dag:   dag,
		index: dag.StartNode,
	}
}

// Next advances the iterator to the next named node in the SagaDag.
func (it *SagaDagIterator) Next() (*NodeEntry, bool) {
	for it.index < it.dag.EndNode {
		node, exists := it.dag.Nodes[it.index]
		it.index++

		if exists {
			switch node.(type) {
			case *ActionNodeInternal, *ConstantNodeInternal, *SubsagaEndNode:
				return &NodeEntry{Internal: node, Index: NodeIndex(it.index - 1)}, true
			default:
				// Skip non-named nodes
			}
		}
	}
	return nil, false
}

// NodeEntry represents a node in the SagaDag.
type NodeEntry struct {
	Internal InternalNode
	Index    NodeIndex
}
