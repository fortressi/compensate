package main

import (
	"encoding/json"
)

// InfrastructureState represents the complete state of our AWS infrastructure
// that will be provisioned by the saga. This state is serializable and will
// be passed between actions to track what resources have been created.
type InfrastructureState struct {
	// Request parameters
	Region       string `json:"region"`
	InstanceType string `json:"instance_type"`

	// VPC Resources (created sequentially first)
	VpcID        string `json:"vpc_id,omitempty"`
	SubnetID     string `json:"subnet_id,omitempty"`
	IGWId        string `json:"igw_id,omitempty"`
	RouteTableID string `json:"route_table_id,omitempty"`

	// Parallel Resources (created concurrently after VPC)
	SecurityGroupID string `json:"security_group_id,omitempty"`
	InstanceID      string `json:"instance_id,omitempty"`

	// Final Configuration (populated after resources are created)
	PublicIP       string `json:"public_ip,omitempty"`
	PrivateIP      string `json:"private_ip,omitempty"`
	ConnectionInfo string `json:"connection_info,omitempty"`

	// Cleanup tracking
	CreatedResources []string `json:"created_resources,omitempty"`
}

// InfrastructureSaga implements the SagaType interface for our infrastructure provisioning
type InfrastructureSaga struct {
	State *InfrastructureState
}

func (s *InfrastructureSaga) ExecContext() *InfrastructureState {
	return s.State
}

// NewInfrastructureState creates a new infrastructure state with the given configuration
func NewInfrastructureState(region, instanceType string) *InfrastructureState {
	return &InfrastructureState{
		Region:           region,
		InstanceType:     instanceType,
		CreatedResources: make([]string, 0),
	}
}

// AddCreatedResource tracks a resource for cleanup purposes
func (s *InfrastructureState) AddCreatedResource(resourceType, resourceID string) {
	resource := resourceType + ":" + resourceID
	s.CreatedResources = append(s.CreatedResources, resource)
}

// String returns a JSON representation of the state for logging
func (s *InfrastructureState) String() string {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "InfrastructureState{error marshaling}"
	}
	return string(data)
}

// VPCResult represents the result of VPC creation
type VPCResult struct {
	VpcID        string `json:"vpc_id"`
	SubnetID     string `json:"subnet_id"`
	IGWId        string `json:"igw_id"`
	RouteTableID string `json:"route_table_id"`
}

// SecurityGroupResult represents the result of security group creation
type SecurityGroupResult struct {
	SecurityGroupID string              `json:"security_group_id"`
	Rules           []SecurityGroupRule `json:"rules"`
}

type SecurityGroupRule struct {
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
	Source   string `json:"source"`
}

// EC2Result represents the result of EC2 instance creation
type EC2Result struct {
	InstanceID string `json:"instance_id"`
	PublicIP   string `json:"public_ip,omitempty"`
	PrivateIP  string `json:"private_ip,omitempty"`
	State      string `json:"state"`
}

// ConfigurationResult represents the final configuration result
type ConfigurationResult struct {
	ConnectionInfo string `json:"connection_info"`
	AccessDetails  string `json:"access_details"`
}
