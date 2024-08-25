package compensate

import (
	"fmt"

	"github.com/fortressi/compensate/dag"
	"github.com/google/uuid"
	"gonum.org/v1/gonum/graph/encoding"
	"gonum.org/v1/gonum/graph/simple"
)

// SagaID represents a unique identifier for a Saga execution.
type SagaID struct {
	UUID uuid.UUID
}

// String returns the string representation of the SagaID.
func (s SagaID) String() string {
	return s.UUID.String()
}

// ActionRegistryError represents an error returned from ActionRegistry.Get().
type ActionRegistryError struct {
	error
}

// NotFoundError indicates that an action with the given name was not found.
func NotFoundError() error {
	return &ActionRegistryError{fmt.Errorf("action not found")}
}

type SagaParams any

// AI Generated below

// SagaName represents a human-readable name for a particular saga.
type SagaName string

// String returns the string representation of the SagaName.
func (s SagaName) String() string {
	return string(s)
}

// NodeName represents a unique name for a saga Node.
type NodeName string

// Dag represents a directed acyclic graph (DAG).
type Dag struct {
	*dag.Graph
	SagaName   SagaName
	nodes      map[int64]InternalNode // Store nodes by gonumNode ID for easy access
	firstNodes []int64                // Root nodes of the DAG
	lastNodes  []int64                // Leaf nodes of the DAG
}

// NewDag creates a new empty Dag.
func NewDag(sagaName SagaName) *Dag {
	return &Dag{
		Graph:      dag.New(),
		SagaName:   sagaName,
		nodes:      make(map[int64]InternalNode),
		firstNodes: []int64{},
		lastNodes:  []int64{},
	}
}

// AddNode adds an internal node to the DAG.
func (d *Dag) AddNode(node InternalNode) NodeIndex {
	gonumNode := d.NewNode()

	if nodeName := node.NodeName(); nodeName != nil {
		if err := gonumNode.SetAttribute(encoding.Attribute{Key: "nodeName", Value: string(*nodeName)}); err != nil {
			panic(err)
		}
	}
	if err := gonumNode.SetAttribute(encoding.Attribute{Key: "label", Value: node.Label()}); err != nil {
		panic(err)
	}

	d.Graph.AddNode(gonumNode)
	d.nodes[gonumNode.ID()] = node
	return NodeIndex(gonumNode.ID())
}

// AddEdge adds a directed edge between two nodes in the DAG.
func (d *Dag) AddEdge(fromID, toID NodeIndex) error {
	fromNode := d.Node(int64(fromID))
	if fromNode == nil {
		return fmt.Errorf("node does not exist")
	}
	toNode := d.Node(int64(toID))
	if toNode == nil {
		return fmt.Errorf("node does not exist")
	}

	d.SetEdge(simple.Edge{F: fromNode, T: toNode})
	return nil
}

// getNodeIndexByName finds a node by its name and returns its index.
// This is primarily for testing purposes.
func (d *Dag) getNodeIndexByName(name string) (int64, error) {
	targetName := NodeName(name)
	for nodeID, node := range d.nodes {
		if nodeName := node.NodeName(); nodeName != nil && *nodeName == targetName {
			return nodeID, nil
		}
	}
	return 0, fmt.Errorf("node with name '%s' not found", name)
}

// GetNode retrieves an internal node by its gonumNode ID.
func (d *Dag) GetNode(id int64) (InternalNode, error) {
	node, exists := d.nodes[id]
	if !exists {
		return nil, fmt.Errorf("node not found: %d", id)
	}
	return node, nil
}

// AddFirstNode adds a node as a root (first) node of the DAG.
// func (d *Dag) AddFirstNode(nodeID int64) {
// 	d.firstNodes = append(d.firstNodes, nodeID)
// }

// AddLastNode adds a node as a leaf (last) node of the DAG.
// func (d *Dag) AddLastNode(nodeID int64) {
// 	d.lastNodes = append(d.lastNodes, nodeID)
// }

type NodeIndex int64

func (n NodeIndex) ToInt64() int64 {
	return int64(n)
}

func NodeIndexSliceToInt64Slice(in []NodeIndex) []int64 {
	out := make([]int64, len(in))
	for i := range in {
		out[i] = in[i].ToInt64()
	}
	return out
}
