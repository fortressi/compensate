package actions

import (
	"context"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/fortressi/compensate"
)

// CreateRouteTableAction creates a route table with internet route and associates it with the subnet
type CreateRouteTableAction[T any, S compensate.SagaType[T]] struct {
	ec2Client *ec2.Client
}

// RouteTableResult represents the result of route table creation
type RouteTableResult struct {
	RouteTableID     string `json:"route_table_id"`
	VpcID            string `json:"vpc_id"`
	SubnetID         string `json:"subnet_id"`
	IGWId            string `json:"igw_id"`
	AssociationID    string `json:"association_id"` // For cleanup of association
}

// NewCreateRouteTableAction creates a new route table creation action
func NewCreateRouteTableAction[T any, S compensate.SagaType[T]](ec2Client *ec2.Client) *CreateRouteTableAction[T, S] {
	return &CreateRouteTableAction[T, S]{
		ec2Client: ec2Client,
	}
}

// Name returns the action name
func (a *CreateRouteTableAction[T, S]) Name() compensate.ActionName {
	return "create_route_table"
}

// DoIt creates the route table, adds internet route, and associates with subnet
func (a *CreateRouteTableAction[T, S]) DoIt(ctx context.Context, sagaCtx compensate.ActionContext[T, S]) (compensate.ActionResult[compensate.ActionData], error) {
	log.Printf("Creating route table")

	// Get VPC ID from VPC action
	vpcResult, found := compensate.LookupTyped[*VPCOnlyResult](sagaCtx, "create_vpc")
	if !found {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("VPC not found in saga context")
	}

	// Get IGW ID from IGW action
	igwResult, found := compensate.LookupTyped[*IGWResult](sagaCtx, "create_internet_gateway")
	if !found {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("Internet Gateway not found in saga context")
	}

	// Get Subnet ID from subnet action
	subnetResult, found := compensate.LookupTyped[*SubnetResult](sagaCtx, "create_subnet")
	if !found {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("Subnet not found in saga context")
	}

	vpcID := vpcResult.VpcID
	igwID := igwResult.IGWId
	subnetID := subnetResult.SubnetID

	log.Printf("Creating route table for VPC %s with IGW %s and subnet %s", vpcID, igwID, subnetID)

	// Create Route Table
	input := &ec2.CreateRouteTableInput{
		VpcId: aws.String(vpcID),
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeRouteTable,
				Tags: []ec2types.Tag{
					{Key: aws.String("Name"), Value: aws.String("compensate-saga-route-table")},
					{Key: aws.String("CreatedBy"), Value: aws.String("compensate-saga")},
				},
			},
		},
	}

	result, err := a.ec2Client.CreateRouteTable(ctx, input)
	if err != nil {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("failed to create route table: %w", err)
	}

	routeTableID := *result.RouteTable.RouteTableId
	log.Printf("Created route table: %s", routeTableID)

	// Create route to internet gateway
	routeInput := &ec2.CreateRouteInput{
		RouteTableId:         aws.String(routeTableID),
		DestinationCidrBlock: aws.String("0.0.0.0/0"),
		GatewayId:            aws.String(igwID),
	}

	_, err = a.ec2Client.CreateRoute(ctx, routeInput)
	if err != nil {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("failed to create internet route in route table %s: %w", routeTableID, err)
	}

	log.Printf("Created internet route 0.0.0.0/0 -> %s in route table %s", igwID, routeTableID)

	// Associate with subnet
	associateInput := &ec2.AssociateRouteTableInput{
		RouteTableId: aws.String(routeTableID),
		SubnetId:     aws.String(subnetID),
	}

	associateResult, err := a.ec2Client.AssociateRouteTable(ctx, associateInput)
	if err != nil {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("failed to associate route table %s with subnet %s: %w", routeTableID, subnetID, err)
	}

	associationID := *associateResult.AssociationId
	log.Printf("Associated route table %s with subnet %s (association ID: %s)", routeTableID, subnetID, associationID)

	routeTableResult := &RouteTableResult{
		RouteTableID:  routeTableID,
		VpcID:         vpcID,
		SubnetID:      subnetID,
		IGWId:         igwID,
		AssociationID: associationID,
	}

	return compensate.ActionResult[compensate.ActionData]{Output: routeTableResult}, nil
}

// UndoIt removes the route table and its associations
func (a *CreateRouteTableAction[T, S]) UndoIt(ctx context.Context, sagaCtx compensate.ActionContext[T, S]) error {
	log.Printf("Cleaning up route table")

	// Get route table info from previous execution result
	routeTableResult, found := compensate.LookupTyped[*RouteTableResult](sagaCtx, "create_route_table")
	if !found {
		log.Printf("No route table found to cleanup")
		return nil
	}

	if routeTableResult.RouteTableID == "" {
		log.Printf("Route table ID is empty, nothing to cleanup")
		return nil
	}

	routeTableID := routeTableResult.RouteTableID
	log.Printf("Deleting route table: %s", routeTableID)

	// Disassociate from subnet first (if association exists)
	if routeTableResult.AssociationID != "" {
		disassociateInput := &ec2.DisassociateRouteTableInput{
			AssociationId: aws.String(routeTableResult.AssociationID),
		}
		_, err := a.ec2Client.DisassociateRouteTable(ctx, disassociateInput)
		if err != nil {
			log.Printf("Warning: failed to disassociate route table %s (association %s): %v", 
				routeTableID, routeTableResult.AssociationID, err)
			// Continue with deletion anyway
		} else {
			log.Printf("Disassociated route table %s from subnet", routeTableID)
		}
	}

	// Delete the route table
	deleteInput := &ec2.DeleteRouteTableInput{
		RouteTableId: aws.String(routeTableID),
	}
	_, err := a.ec2Client.DeleteRouteTable(ctx, deleteInput)
	if err != nil {
		return fmt.Errorf("failed to delete route table %s: %w", routeTableID, err)
	}

	log.Printf("Route table %s deleted successfully", routeTableID)
	return nil
}