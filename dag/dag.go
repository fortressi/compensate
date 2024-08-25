package dag

import (
	"fmt"

	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/encoding"
	"gonum.org/v1/gonum/graph/encoding/dot"
	"gonum.org/v1/gonum/graph/simple"
)

type Graph struct {
	*simple.DirectedGraph
	attrs encoding.Attributes
}

func New() *Graph {
	return &Graph{DirectedGraph: simple.NewDirectedGraph()}
}

func (g *Graph) NewNode() *Node {
	return &Node{Node: g.DirectedGraph.NewNode()}
	// return &node{Node: g.DirectedGraph.NewNode(), attrs: make(map[string]string)}
}

func (g *Graph) Attributers() (encoding.Attributer, encoding.Attributer, encoding.Attributer) {
	return &Graph{}, &Node{}, &edge{}
}

func (g *Graph) Attributes() []encoding.Attribute {
	return g.attrs.Attributes()
}

func (g *Graph) SetAttribute(attr encoding.Attribute) error {
	return g.attrs.SetAttribute(attr)
}

type Node struct {
	graph.Node
	attrs encoding.Attributes
	// dotID string
	// attrs map[string]string
}

func (n *Node) Attributes() []encoding.Attribute {
	return n.attrs.Attributes()
}

func (n *Node) SetAttribute(attr encoding.Attribute) error {
	return n.attrs.SetAttribute(attr)
}

// ExportToDot exports the DAG to Graphviz .dot format.
func (g *Graph) ExportToDot() (string, error) {
	data, err := dot.Marshal(g, "", "", "")
	if err != nil {
		return "", fmt.Errorf("failed to export DAG to DOT format: %v", err)
	}
	return string(data), nil
}

// SetDOTID sets a DOT ID.
// func (n *node) SetDOTID(id string) {
// 	n.dotID = id
// }

// // SetAttribute sets a DOT attribute.
// func (n *node) SetAttribute(attr encoding.Attribute) error {
// 	n.attrs[attr.Key] = attr.Value
// 	return nil
// }

// // SetAttribute sets a DOT attribute.
// func (n *node) SetAttribute(attr encoding.Attribute) error {
// 	n.attrs[attr.Key] = attr.Value
// 	return nil
// }

func (g *Graph) NewEdge(from, to graph.Node) graph.Edge {
	return &edge{Edge: g.DirectedGraph.NewEdge(from, to)}
}

type edge struct {
	graph.Edge
	attrs encoding.Attributes
}

func (e *edge) Attributes() []encoding.Attribute {
	return e.attrs.Attributes()
}

func (e *edge) SetAttribute(attr encoding.Attribute) error {
	return e.attrs.SetAttribute(attr)
}
