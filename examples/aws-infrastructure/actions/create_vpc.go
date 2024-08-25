package actions

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/fortressi/compensate"
)

// CreateVPCAction creates just the VPC with specified CIDR block
type CreateVPCAction[T any, S compensate.SagaType[T]] struct {
	ec2Client *ec2.Client
	vpcCIDR   string
	region    string
}

// VPCOnlyResult represents the result of VPC-only creation
type VPCOnlyResult struct {
	VpcID string `json:"vpc_id"`
}

// NewCreateVPCAction creates a new VPC-only creation action
func NewCreateVPCAction[T any, S compensate.SagaType[T]](ec2Client *ec2.Client, region, vpcCIDR string) *CreateVPCAction[T, S] {
	return &CreateVPCAction[T, S]{
		ec2Client: ec2Client,
		vpcCIDR:   vpcCIDR,
		region:    region,
	}
}

// Name returns the action name
func (a *CreateVPCAction[T, S]) Name() compensate.ActionName {
	return "create_vpc"
}

// DoIt creates the VPC and waits for it to become available
func (a *CreateVPCAction[T, S]) DoIt(ctx context.Context, sagaCtx compensate.ActionContext[T, S]) (compensate.ActionResult[compensate.ActionData], error) {
	log.Printf("Creating VPC with CIDR %s in region %s", a.vpcCIDR, a.region)

	// Create VPC
	input := &ec2.CreateVpcInput{
		CidrBlock: aws.String(a.vpcCIDR),
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeVpc,
				Tags: []ec2types.Tag{
					{Key: aws.String("Name"), Value: aws.String("compensate-saga-vpc")},
					{Key: aws.String("CreatedBy"), Value: aws.String("compensate-saga")},
				},
			},
		},
	}

	result, err := a.ec2Client.CreateVpc(ctx, input)
	if err != nil {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("failed to create VPC: %w", err)
	}

	vpcID := *result.Vpc.VpcId
	log.Printf("Created VPC: %s", vpcID)

	// Wait for VPC to be available
	waiter := ec2.NewVpcAvailableWaiter(a.ec2Client)
	err = waiter.Wait(ctx, &ec2.DescribeVpcsInput{
		VpcIds: []string{vpcID},
	}, 5*time.Minute)
	if err != nil {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("VPC %s did not become available: %w", vpcID, err)
	}

	log.Printf("VPC %s is now available", vpcID)

	vpcResult := &VPCOnlyResult{
		VpcID: vpcID,
	}

	return compensate.ActionResult[compensate.ActionData]{Output: vpcResult}, nil
}

// UndoIt removes the VPC
func (a *CreateVPCAction[T, S]) UndoIt(ctx context.Context, sagaCtx compensate.ActionContext[T, S]) error {
	log.Printf("Cleaning up VPC")

	// Get VPC info from previous execution result
	vpcResult, found := compensate.LookupTyped[*VPCOnlyResult](sagaCtx, "create_vpc")
	if !found {
		log.Printf("No VPC found to cleanup")
		return nil
	}

	if vpcResult.VpcID == "" {
		log.Printf("VPC ID is empty, nothing to cleanup")
		return nil
	}

	log.Printf("Deleting VPC: %s", vpcResult.VpcID)
	input := &ec2.DeleteVpcInput{
		VpcId: aws.String(vpcResult.VpcID),
	}

	_, err := a.ec2Client.DeleteVpc(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to delete VPC %s: %w", vpcResult.VpcID, err)
	}

	log.Printf("VPC %s deleted successfully", vpcResult.VpcID)
	return nil
}