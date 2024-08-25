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

// VPCCreationAction creates a complete VPC infrastructure including:
// - VPC with CIDR block
// - Public subnet
// - Internet Gateway
// - Route table with internet route
type VPCCreationAction[T any, S compensate.SagaType[T]] struct {
	ec2Client  *ec2.Client
	vpcCIDR    string
	subnetCIDR string
	region     string
}

// VPCResult represents the result of VPC creation
type VPCResult struct {
	VpcID        string `json:"vpc_id"`
	SubnetID     string `json:"subnet_id"`
	IGWId        string `json:"igw_id"`
	RouteTableID string `json:"route_table_id"`
}

// NewVPCCreationAction creates a new VPC creation action
func NewVPCCreationAction[T any, S compensate.SagaType[T]](ec2Client *ec2.Client, region, vpcCIDR, subnetCIDR string) *VPCCreationAction[T, S] {
	return &VPCCreationAction[T, S]{
		ec2Client:  ec2Client,
		vpcCIDR:    vpcCIDR,
		subnetCIDR: subnetCIDR,
		region:     region,
	}
}

// Name returns the action name
func (a *VPCCreationAction[T, S]) Name() compensate.ActionName {
	return "create_vpc_infrastructure"
}

// DoIt creates the VPC infrastructure
func (a *VPCCreationAction[T, S]) DoIt(ctx context.Context, sagaCtx compensate.ActionContext[T, S]) (compensate.ActionResult[compensate.ActionData], error) {
	log.Printf("Creating VPC infrastructure in region %s", a.region)

	// Step 1: Create VPC
	vpcResult, err := a.createVPC(ctx)
	if err != nil {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("failed to create VPC: %w", err)
	}
	log.Printf("Created VPC: %s", *vpcResult.VpcId)

	// Step 2: Create Internet Gateway
	igwResult, err := a.createInternetGateway(ctx, *vpcResult.VpcId)
	if err != nil {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("failed to create internet gateway: %w", err)
	}
	log.Printf("Created Internet Gateway: %s", *igwResult.InternetGatewayId)

	// Step 3: Create Subnet
	subnetResult, err := a.createSubnet(ctx, *vpcResult.VpcId)
	if err != nil {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("failed to create subnet: %w", err)
	}
	log.Printf("Created Subnet: %s", *subnetResult.SubnetId)

	// Step 4: Create Route Table and Routes
	routeTableResult, err := a.createRouteTable(ctx, *vpcResult.VpcId, *igwResult.InternetGatewayId, *subnetResult.SubnetId)
	if err != nil {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("failed to create route table: %w", err)
	}
	log.Printf("Created Route Table: %s", *routeTableResult.RouteTableId)

	result := &VPCResult{
		VpcID:        *vpcResult.VpcId,
		SubnetID:     *subnetResult.SubnetId,
		IGWId:        *igwResult.InternetGatewayId,
		RouteTableID: *routeTableResult.RouteTableId,
	}

	log.Printf("VPC infrastructure creation completed successfully")
	return compensate.ActionResult[compensate.ActionData]{Output: result}, nil
}

// UndoIt removes the VPC infrastructure
func (a *VPCCreationAction[T, S]) UndoIt(ctx context.Context, sagaCtx compensate.ActionContext[T, S]) error {
	log.Printf("Cleaning up VPC infrastructure")

	// Get VPC info from previous execution result
	vpcResult, found := compensate.LookupTyped[*VPCResult](sagaCtx, "vpc_infrastructure")
	if !found {
		log.Printf("No VPC infrastructure found to cleanup")
		return nil
	}

	// Cleanup in reverse order of creation
	if vpcResult.RouteTableID != "" {
		if err := a.deleteRouteTable(ctx, vpcResult.RouteTableID); err != nil {
			log.Printf("Warning: failed to delete route table %s: %v", vpcResult.RouteTableID, err)
		}
	}

	if vpcResult.SubnetID != "" {
		if err := a.deleteSubnet(ctx, vpcResult.SubnetID); err != nil {
			log.Printf("Warning: failed to delete subnet %s: %v", vpcResult.SubnetID, err)
		}
	}

	if vpcResult.IGWId != "" && vpcResult.VpcID != "" {
		if err := a.deleteInternetGateway(ctx, vpcResult.IGWId, vpcResult.VpcID); err != nil {
			log.Printf("Warning: failed to delete internet gateway %s: %v", vpcResult.IGWId, err)
		}
	}

	if vpcResult.VpcID != "" {
		if err := a.deleteVPC(ctx, vpcResult.VpcID); err != nil {
			log.Printf("Warning: failed to delete VPC %s: %v", vpcResult.VpcID, err)
		}
	}

	log.Printf("VPC infrastructure cleanup completed")
	return nil
}

// createVPC creates a new VPC
func (a *VPCCreationAction[T, S]) createVPC(ctx context.Context) (*ec2types.Vpc, error) {
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
		return nil, err
	}

	// Wait for VPC to be available
	waiter := ec2.NewVpcAvailableWaiter(a.ec2Client)
	err = waiter.Wait(ctx, &ec2.DescribeVpcsInput{
		VpcIds: []string{*result.Vpc.VpcId},
	}, 5*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("VPC did not become available: %w", err)
	}

	return result.Vpc, nil
}

// createInternetGateway creates and attaches an internet gateway
func (a *VPCCreationAction[T, S]) createInternetGateway(ctx context.Context, vpcId string) (*ec2types.InternetGateway, error) {
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
		return nil, err
	}

	// Attach to VPC
	attachInput := &ec2.AttachInternetGatewayInput{
		InternetGatewayId: result.InternetGateway.InternetGatewayId,
		VpcId:             aws.String(vpcId),
	}

	_, err = a.ec2Client.AttachInternetGateway(ctx, attachInput)
	if err != nil {
		return nil, fmt.Errorf("failed to attach internet gateway: %w", err)
	}

	return result.InternetGateway, nil
}

// createSubnet creates a public subnet
func (a *VPCCreationAction[T, S]) createSubnet(ctx context.Context, vpcId string) (*ec2types.Subnet, error) {
	// Get the first availability zone
	az, err := a.getAvailabilityZone(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get availability zone: %w", err)
	}

	input := &ec2.CreateSubnetInput{
		VpcId:            aws.String(vpcId),
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
		return nil, err
	}

	// Enable auto-assign public IP
	modifyInput := &ec2.ModifySubnetAttributeInput{
		SubnetId:            result.Subnet.SubnetId,
		MapPublicIpOnLaunch: &ec2types.AttributeBooleanValue{Value: aws.Bool(true)},
	}

	_, err = a.ec2Client.ModifySubnetAttribute(ctx, modifyInput)
	if err != nil {
		return nil, fmt.Errorf("failed to enable auto-assign public IP: %w", err)
	}

	return result.Subnet, nil
}

// createRouteTable creates a route table with internet route
func (a *VPCCreationAction[T, S]) createRouteTable(ctx context.Context, vpcId, igwId, subnetId string) (*ec2types.RouteTable, error) {
	// Create Route Table
	input := &ec2.CreateRouteTableInput{
		VpcId: aws.String(vpcId),
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
		return nil, err
	}

	// Create route to internet gateway
	routeInput := &ec2.CreateRouteInput{
		RouteTableId:         result.RouteTable.RouteTableId,
		DestinationCidrBlock: aws.String("0.0.0.0/0"),
		GatewayId:            aws.String(igwId),
	}

	_, err = a.ec2Client.CreateRoute(ctx, routeInput)
	if err != nil {
		return nil, fmt.Errorf("failed to create internet route: %w", err)
	}

	// Associate with subnet
	associateInput := &ec2.AssociateRouteTableInput{
		RouteTableId: result.RouteTable.RouteTableId,
		SubnetId:     aws.String(subnetId),
	}

	_, err = a.ec2Client.AssociateRouteTable(ctx, associateInput)
	if err != nil {
		return nil, fmt.Errorf("failed to associate route table with subnet: %w", err)
	}

	return result.RouteTable, nil
}

// getAvailabilityZone returns the first availability zone for the configured region
func (a *VPCCreationAction[T, S]) getAvailabilityZone(ctx context.Context) (string, error) {
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

// Cleanup methods
func (a *VPCCreationAction[T, S]) deleteRouteTable(ctx context.Context, routeTableId string) error {
	input := &ec2.DeleteRouteTableInput{
		RouteTableId: aws.String(routeTableId),
	}
	_, err := a.ec2Client.DeleteRouteTable(ctx, input)
	return err
}

func (a *VPCCreationAction[T, S]) deleteSubnet(ctx context.Context, subnetId string) error {
	input := &ec2.DeleteSubnetInput{
		SubnetId: aws.String(subnetId),
	}
	_, err := a.ec2Client.DeleteSubnet(ctx, input)
	return err
}

func (a *VPCCreationAction[T, S]) deleteInternetGateway(ctx context.Context, igwId, vpcId string) error {
	// Detach first
	detachInput := &ec2.DetachInternetGatewayInput{
		InternetGatewayId: aws.String(igwId),
		VpcId:             aws.String(vpcId),
	}
	_, err := a.ec2Client.DetachInternetGateway(ctx, detachInput)
	if err != nil {
		return err
	}

	// Then delete
	deleteInput := &ec2.DeleteInternetGatewayInput{
		InternetGatewayId: aws.String(igwId),
	}
	_, err = a.ec2Client.DeleteInternetGateway(ctx, deleteInput)
	return err
}

func (a *VPCCreationAction[T, S]) deleteVPC(ctx context.Context, vpcId string) error {
	input := &ec2.DeleteVpcInput{
		VpcId: aws.String(vpcId),
	}
	_, err := a.ec2Client.DeleteVpc(ctx, input)
	return err
}