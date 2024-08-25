package compensate

import (
	"encoding/json"
	"fmt"
)

// InternalNode represents an internal node in the saga DAG.
type InternalNode interface {
	NodeName() *NodeName
	Label() string
}

// StartNode represents the start of the DAG.
type StartNode struct {
	Params json.RawMessage
}

func (n *StartNode) NodeName() *NodeName {
	return nil
}

func (n *StartNode) Label() string {
	return "(start node)"
}

// EndNode represents the end of the DAG.
type EndNode struct{}

func (n *EndNode) NodeName() *NodeName {
	return nil
}

func (n *EndNode) Label() string {
	return "(end node)"
}

// ActionNodeInternal represents an action node in the DAG.
type ActionNodeInternal struct {
	Name       NodeName
	LabelValue string
	ActionName ActionName
}

func (n *ActionNodeInternal) NodeName() *NodeName {
	return &n.Name
}

func (n *ActionNodeInternal) Label() string {
	return n.LabelValue
}

// ConstantNodeInternal represents a constant node in the DAG.
type ConstantNodeInternal struct {
	Name  NodeName
	Value json.RawMessage
}

func (n *ConstantNodeInternal) NodeName() *NodeName {
	return &n.Name
}

func (n *ConstantNodeInternal) Label() string {
	valueAsJSON, err := json.Marshal(n.Value)
	if err != nil {
		return fmt.Sprintf("(failed to serialize constant value: %v)", err)
	}
	return fmt.Sprintf("(constant = %s)", string(valueAsJSON))
}

// SubsagaStartNode represents the start of a subsaga.
type SubsagaStartNode struct {
	SagaName       SagaName
	ParamsNodeName NodeName
}

func (n *SubsagaStartNode) NodeName() *NodeName {
	return nil
}

func (n *SubsagaStartNode) Label() string {
	return fmt.Sprintf("(subsaga start: %s)", n.SagaName)
}

// SubsagaEndNode represents the end of a subsaga.
type SubsagaEndNode struct {
	Name NodeName
}

func (n *SubsagaEndNode) NodeName() *NodeName {
	return &n.Name
}

func (n *SubsagaEndNode) Label() string {
	return "(subsaga end)"
}
