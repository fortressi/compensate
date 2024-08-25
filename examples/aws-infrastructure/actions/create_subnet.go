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

// CreateSubnetAction creates a public subnet in the VPC
type CreateSubnetAction[T any, S compensate.SagaType[T]] struct {
	ec2Client  *ec2.Client
	subnetCIDR string
	region     string
}

// SubnetResult represents the result of subnet creation
type SubnetResult struct {
	SubnetID         string `json:"subnet_id"`
	AvailabilityZone string `json:"availability_zone"`
	VpcID            string `json:"vpc_id"` // Keep track of which VPC it belongs to
}

// NewCreateSubnetAction creates a new subnet creation action
func NewCreateSubnetAction[T any, S compensate.SagaType[T]](ec2Client *ec2.Client, region, subnetCIDR string) *CreateSubnetAction[T, S] {
	return &CreateSubnetAction[T, S]{
		ec2Client:  ec2Client,
		subnetCIDR: subnetCIDR,
		region:     region,
	}
}

// Name returns the action name
func (a *CreateSubnetAction[T, S]) Name() compensate.ActionName {
	return "create_subnet"
}

// DoIt creates the subnet and enables auto-assign public IP
func (a *CreateSubnetAction[T, S]) DoIt(ctx context.Context, sagaCtx compensate.ActionContext[T, S]) (compensate.ActionResult[compensate.ActionData], error) {
	log.Printf("Creating subnet with CIDR %s", a.subnetCIDR)

	// Get VPC ID from previous action
	vpcResult, found := compensate.LookupTyped[*VPCOnlyResult](sagaCtx, "create_vpc")
	if !found {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("VPC not found in saga context")
	}

	vpcID := vpcResult.VpcID
	log.Printf("Creating subnet in VPC: %s", vpcID)

	// Get the first availability zone
	az, err := a.getAvailabilityZone(ctx)
	if err != nil {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("failed to get availability zone: %w", err)
	}

	// Create subnet
	input := &ec2.CreateSubnetInput{
		VpcId:            aws.String(vpcID),
		CidrBlock:        aws.String(a.subnetCIDR),
		AvailabilityZone: aws.String(az),
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeSubnet,
				Tags: []ec2types.Tag{
					{Key: aws.String("Name"), Value: aws.String("compensate-saga-subnet")},
					{Key: aws.String("CreatedBy"), Value: aws.String("compensate-saga")},
				},
			},
		},
	}

	result, err := a.ec2Client.CreateSubnet(ctx, input)
	if err != nil {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("failed to create subnet: %w", err)
	}

	subnetID := *result.Subnet.SubnetId
	log.Printf("Created subnet: %s in AZ: %s", subnetID, az)

	// Enable auto-assign public IP
	modifyInput := &ec2.ModifySubnetAttributeInput{
		SubnetId:            aws.String(subnetID),
		MapPublicIpOnLaunch: &ec2types.AttributeBooleanValue{Value: aws.Bool(true)},
	}

	_, err = a.ec2Client.ModifySubnetAttribute(ctx, modifyInput)
	if err != nil {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("failed to enable auto-assign public IP for subnet %s: %w", subnetID, err)
	}

	log.Printf("Enabled auto-assign public IP for subnet %s", subnetID)

	subnetResult := &SubnetResult{
		SubnetID:         subnetID,
		AvailabilityZone: az,
		VpcID:            vpcID,
	}

	return compensate.ActionResult[compensate.ActionData]{Output: subnetResult}, nil
}

// UndoIt removes the subnet
func (a *CreateSubnetAction[T, S]) UndoIt(ctx context.Context, sagaCtx compensate.ActionContext[T, S]) error {
	log.Printf("Cleaning up subnet")

	// Get subnet info from previous execution result
	subnetResult, found := compensate.LookupTyped[*SubnetResult](sagaCtx, "create_subnet")
	if !found {
		log.Printf("No subnet found to cleanup")
		return nil
	}

	if subnetResult.SubnetID == "" {
		log.Printf("Subnet ID is empty, nothing to cleanup")
		return nil
	}

	log.Printf("Deleting subnet: %s", subnetResult.SubnetID)
	input := &ec2.DeleteSubnetInput{
		SubnetId: aws.String(subnetResult.SubnetID),
	}

	_, err := a.ec2Client.DeleteSubnet(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to delete subnet %s: %w", subnetResult.SubnetID, err)
	}

	log.Printf("Subnet %s deleted successfully", subnetResult.SubnetID)
	return nil
}

// getAvailabilityZone returns the first availability zone for the configured region
func (a *CreateSubnetAction[T, S]) getAvailabilityZone(ctx context.Context) (string, error) {
	input := &ec2.DescribeAvailabilityZonesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("state"),
				Values: []string{"available"},
			},
		},
	}

	result, err := a.ec2Client.DescribeAvailabilityZones(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to describe availability zones: %w", err)
	}

	if len(result.AvailabilityZones) == 0 {
		return "", fmt.Errorf("no availability zones found")
	}

	return *result.AvailabilityZones[0].ZoneName, nil
}