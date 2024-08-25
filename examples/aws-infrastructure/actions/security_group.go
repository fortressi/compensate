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

// SecurityGroupCreationAction creates a security group with SSH and HTTP access
type SecurityGroupCreationAction[T any, S compensate.SagaType[T]] struct {
	ec2Client *ec2.Client
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

// NewSecurityGroupCreationAction creates a new security group creation action
func NewSecurityGroupCreationAction[T any, S compensate.SagaType[T]](ec2Client *ec2.Client) *SecurityGroupCreationAction[T, S] {
	return &SecurityGroupCreationAction[T, S]{
		ec2Client: ec2Client,
	}
}

// Name returns the action name
func (a *SecurityGroupCreationAction[T, S]) Name() compensate.ActionName {
	return "create_security_group"
}

// DoIt creates the security group
func (a *SecurityGroupCreationAction[T, S]) DoIt(ctx context.Context, sagaCtx compensate.ActionContext[T, S]) (compensate.ActionResult[compensate.ActionData], error) {
	log.Printf("Creating security group")

	// Get VPC information from previous step
	vpcResult, found := compensate.LookupTyped[*VPCOnlyResult](sagaCtx, "create_vpc")
	if !found {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("VPC not found - security group depends on VPC creation")
	}

	// Create security group
	sgResult, err := a.createSecurityGroup(ctx, vpcResult.VpcID)
	if err != nil {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("failed to create security group: %w", err)
	}

	log.Printf("Created Security Group: %s", *sgResult.GroupId)

	// Add ingress rules
	rules, err := a.addIngressRules(ctx, *sgResult.GroupId)
	if err != nil {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("failed to add ingress rules: %w", err)
	}

	result := &SecurityGroupResult{
		SecurityGroupID: *sgResult.GroupId,
		Rules:           rules,
	}

	log.Printf("Security group creation completed successfully")
	return compensate.ActionResult[compensate.ActionData]{Output: result}, nil
}

// UndoIt removes the security group
func (a *SecurityGroupCreationAction[T, S]) UndoIt(ctx context.Context, sagaCtx compensate.ActionContext[T, S]) error {
	log.Printf("Cleaning up security group")

	// Get security group info from previous execution result
	sgResult, found := compensate.LookupTyped[*SecurityGroupResult](sagaCtx, "security_group")
	if !found {
		log.Printf("No security group found to cleanup")
		return nil
	}

	if err := a.deleteSecurityGroup(ctx, sgResult.SecurityGroupID); err != nil {
		log.Printf("Warning: failed to delete security group %s: %v", sgResult.SecurityGroupID, err)
		return err
	}

	log.Printf("Security group cleanup completed")
	return nil
}

// createSecurityGroup creates a new security group in the VPC
func (a *SecurityGroupCreationAction[T, S]) createSecurityGroup(ctx context.Context, vpcId string) (*ec2types.SecurityGroup, error) {
	input := &ec2.CreateSecurityGroupInput{
		GroupName:   aws.String("compensate-saga-security-group"),
		Description: aws.String("Security group created by compensate saga for demo infrastructure"),
		VpcId:       aws.String(vpcId),
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeSecurityGroup,
				Tags: []ec2types.Tag{
					{Key: aws.String("Name"), Value: aws.String("compensate-saga-sg")},
					{Key: aws.String("CreatedBy"), Value: aws.String("compensate-saga")},
				},
			},
		},
	}

	result, err := a.ec2Client.CreateSecurityGroup(ctx, input)
	if err != nil {
		return nil, err
	}

	// Return the security group with the ID
	return &ec2types.SecurityGroup{
		GroupId:     result.GroupId,
		GroupName:   aws.String("compensate-saga-security-group"),
		Description: aws.String("Security group created by compensate saga for demo infrastructure"),
		VpcId:       aws.String(vpcId),
	}, nil
}

// addIngressRules adds SSH and HTTP ingress rules to the security group
func (a *SecurityGroupCreationAction[T, S]) addIngressRules(ctx context.Context, groupId string) ([]SecurityGroupRule, error) {
	// Define the rules we want to add
	rules := []SecurityGroupRule{
		{Protocol: "tcp", Port: 22, Source: "0.0.0.0/0"},  // SSH
		{Protocol: "tcp", Port: 80, Source: "0.0.0.0/0"},  // HTTP
		{Protocol: "tcp", Port: 443, Source: "0.0.0.0/0"}, // HTTPS
		{Protocol: "icmp", Port: 8, Source: "0.0.0.0/0"},  // ICMP (ping - echo request)
	}

	// Convert to AWS EC2 ingress rules
	var ipPermissions []ec2types.IpPermission
	for _, rule := range rules {
		var fromPort, toPort *int32
		if rule.Protocol == "icmp" {
			// For ICMP: fromPort = ICMP type, toPort = ICMP code (-1 for all codes)
			icmpType := int32(rule.Port)
			icmpCode := int32(-1)
			fromPort = &icmpType
			toPort = &icmpCode
		} else if rule.Port != -1 {
			port := int32(rule.Port)
			fromPort = &port
			toPort = &port
		}

		permission := ec2types.IpPermission{
			IpProtocol: aws.String(rule.Protocol),
			FromPort:   fromPort,
			ToPort:     toPort,
			IpRanges: []ec2types.IpRange{
				{
					CidrIp:      aws.String(rule.Source),
					Description: aws.String(fmt.Sprintf("Allow %s traffic", rule.Protocol)),
				},
			},
		}
		ipPermissions = append(ipPermissions, permission)
	}

	// Add the ingress rules
	input := &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId:       aws.String(groupId),
		IpPermissions: ipPermissions,
	}

	_, err := a.ec2Client.AuthorizeSecurityGroupIngress(ctx, input)
	if err != nil {
		return nil, err
	}

	log.Printf("Added %d ingress rules to security group %s", len(rules), groupId)
	return rules, nil
}

// deleteSecurityGroup deletes the security group
func (a *SecurityGroupCreationAction[T, S]) deleteSecurityGroup(ctx context.Context, groupId string) error {
	input := &ec2.DeleteSecurityGroupInput{
		GroupId: aws.String(groupId),
	}

	_, err := a.ec2Client.DeleteSecurityGroup(ctx, input)
	return err
}

