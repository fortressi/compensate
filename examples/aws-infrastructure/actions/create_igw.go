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

// CreateInternetGatewayAction creates an Internet Gateway and attaches it to a VPC
type CreateInternetGatewayAction[T any, S compensate.SagaType[T]] struct {
	ec2Client *ec2.Client
}

// IGWResult represents the result of Internet Gateway creation
type IGWResult struct {
	IGWId string `json:"igw_id"`
	VpcID string `json:"vpc_id"` // Keep track of which VPC it's attached to
}

// NewCreateInternetGatewayAction creates a new Internet Gateway creation action
func NewCreateInternetGatewayAction[T any, S compensate.SagaType[T]](ec2Client *ec2.Client) *CreateInternetGatewayAction[T, S] {
	return &CreateInternetGatewayAction[T, S]{
		ec2Client: ec2Client,
	}
}

// Name returns the action name
func (a *CreateInternetGatewayAction[T, S]) Name() compensate.ActionName {
	return "create_internet_gateway"
}

// DoIt creates the Internet Gateway and attaches it to the VPC
func (a *CreateInternetGatewayAction[T, S]) DoIt(ctx context.Context, sagaCtx compensate.ActionContext[T, S]) (compensate.ActionResult[compensate.ActionData], error) {
	log.Printf("Creating Internet Gateway")

	// Get VPC ID from previous action
	vpcResult, found := compensate.LookupTyped[*VPCOnlyResult](sagaCtx, "create_vpc")
	if !found {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("VPC not found in saga context")
	}

	vpcID := vpcResult.VpcID
	log.Printf("Creating Internet Gateway for VPC: %s", vpcID)

	// Create Internet Gateway
	input := &ec2.CreateInternetGatewayInput{
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeInternetGateway,
				Tags: []ec2types.Tag{
					{Key: aws.String("Name"), Value: aws.String("compensate-saga-igw")},
					{Key: aws.String("CreatedBy"), Value: aws.String("compensate-saga")},
				},
			},
		},
	}

	result, err := a.ec2Client.CreateInternetGateway(ctx, input)
	if err != nil {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("failed to create internet gateway: %w", err)
	}

	igwID := *result.InternetGateway.InternetGatewayId
	log.Printf("Created Internet Gateway: %s", igwID)

	// Attach to VPC
	attachInput := &ec2.AttachInternetGatewayInput{
		InternetGatewayId: aws.String(igwID),
		VpcId:             aws.String(vpcID),
	}

	_, err = a.ec2Client.AttachInternetGateway(ctx, attachInput)
	if err != nil {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("failed to attach internet gateway %s to VPC %s: %w", igwID, vpcID, err)
	}

	log.Printf("Attached Internet Gateway %s to VPC %s", igwID, vpcID)

	igwResult := &IGWResult{
		IGWId: igwID,
		VpcID: vpcID,
	}

	return compensate.ActionResult[compensate.ActionData]{Output: igwResult}, nil
}

// UndoIt detaches and removes the Internet Gateway
func (a *CreateInternetGatewayAction[T, S]) UndoIt(ctx context.Context, sagaCtx compensate.ActionContext[T, S]) error {
	log.Printf("Cleaning up Internet Gateway")

	// Get IGW info from previous execution result
	igwResult, found := compensate.LookupTyped[*IGWResult](sagaCtx, "create_internet_gateway")
	if !found {
		log.Printf("No Internet Gateway found to cleanup")
		return nil
	}

	if igwResult.IGWId == "" {
		log.Printf("Internet Gateway ID is empty, nothing to cleanup")
		return nil
	}

	log.Printf("Detaching and deleting Internet Gateway: %s", igwResult.IGWId)

	// Detach from VPC first
	if igwResult.VpcID != "" {
		detachInput := &ec2.DetachInternetGatewayInput{
			InternetGatewayId: aws.String(igwResult.IGWId),
			VpcId:             aws.String(igwResult.VpcID),
		}
		_, err := a.ec2Client.DetachInternetGateway(ctx, detachInput)
		if err != nil {
			log.Printf("Warning: failed to detach internet gateway %s from VPC %s: %v", igwResult.IGWId, igwResult.VpcID, err)
			// Continue with deletion anyway
		} else {
			log.Printf("Detached Internet Gateway %s from VPC %s", igwResult.IGWId, igwResult.VpcID)
		}
	}

	// Delete the Internet Gateway
	deleteInput := &ec2.DeleteInternetGatewayInput{
		InternetGatewayId: aws.String(igwResult.IGWId),
	}
	_, err := a.ec2Client.DeleteInternetGateway(ctx, deleteInput)
	if err != nil {
		return fmt.Errorf("failed to delete internet gateway %s: %w", igwResult.IGWId, err)
	}

	log.Printf("Internet Gateway %s deleted successfully", igwResult.IGWId)
	return nil
}