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

// EC2CreationAction creates an EC2 instance in the specified subnet
type EC2CreationAction[T any, S compensate.SagaType[T]] struct {
	ec2Client    *ec2.Client
	instanceType string
	region       string
}

// EC2Result represents the result of EC2 instance creation
type EC2Result struct {
	InstanceID string `json:"instance_id"`
	PublicIP   string `json:"public_ip,omitempty"`
	PrivateIP  string `json:"private_ip,omitempty"`
	State      string `json:"state"`
	AmiID      string `json:"ami_id"`
}

// NewEC2CreationAction creates a new EC2 creation action
func NewEC2CreationAction[T any, S compensate.SagaType[T]](ec2Client *ec2.Client, region, instanceType string) *EC2CreationAction[T, S] {
	return &EC2CreationAction[T, S]{
		ec2Client:    ec2Client,
		instanceType: instanceType,
		region:       region,
	}
}

// Name returns the action name
func (a *EC2CreationAction[T, S]) Name() compensate.ActionName {
	return "create_ec2_instance"
}

// DoIt creates the EC2 instance
func (a *EC2CreationAction[T, S]) DoIt(ctx context.Context, sagaCtx compensate.ActionContext[T, S]) (compensate.ActionResult[compensate.ActionData], error) {
	log.Printf("Creating EC2 instance")

	// Get subnet information from previous step
	subnetResult, found := compensate.LookupTyped[*SubnetResult](sagaCtx, "create_subnet")
	if !found {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("Subnet not found - EC2 instance depends on subnet creation")
	}

	// Get the latest Amazon Linux AMI
	amiID, err := a.getLatestAmazonLinuxAMI(ctx)
	if err != nil {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("failed to get AMI ID: %w", err)
	}

	// Launch the instance
	instance, err := a.launchInstance(ctx, amiID, subnetResult.SubnetID)
	if err != nil {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("failed to launch instance: %w", err)
	}

	log.Printf("Created EC2 Instance: %s", *instance.InstanceId)

	// Wait for instance to be running
	runningInstance, err := a.waitForInstanceRunning(ctx, *instance.InstanceId)
	if err != nil {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("instance failed to reach running state: %w", err)
	}

	result := &EC2Result{
		InstanceID: *runningInstance.InstanceId,
		PublicIP:   a.getStringValue(runningInstance.PublicIpAddress),
		PrivateIP:  a.getStringValue(runningInstance.PrivateIpAddress),
		State:      string(runningInstance.State.Name),
		AmiID:      amiID,
	}

	log.Printf("EC2 instance creation completed successfully - Public IP: %s", result.PublicIP)
	return compensate.ActionResult[compensate.ActionData]{Output: result}, nil
}

// UndoIt terminates the EC2 instance
func (a *EC2CreationAction[T, S]) UndoIt(ctx context.Context, sagaCtx compensate.ActionContext[T, S]) error {
	log.Printf("Cleaning up EC2 instance")

	// Get EC2 info from previous execution result
	ec2Result, found := compensate.LookupTyped[*EC2Result](sagaCtx, "ec2_instance")
	if !found {
		log.Printf("No EC2 instance found to cleanup")
		return nil
	}

	if err := a.terminateInstance(ctx, ec2Result.InstanceID); err != nil {
		log.Printf("Warning: failed to terminate instance %s: %v", ec2Result.InstanceID, err)
		return err
	}

	log.Printf("EC2 instance cleanup completed")
	return nil
}

// getLatestAmazonLinuxAMI returns the latest Amazon Linux 2 AMI ID
func (a *EC2CreationAction[T, S]) getLatestAmazonLinuxAMI(ctx context.Context) (string, error) {
	input := &ec2.DescribeImagesInput{
		Owners: []string{"amazon"},
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("name"),
				Values: []string{"amzn2-ami-hvm-*-x86_64-gp2"},
			},
			{
				Name:   aws.String("state"),
				Values: []string{"available"},
			},
		},
	}

	result, err := a.ec2Client.DescribeImages(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to describe AMIs: %w", err)
	}

	if len(result.Images) == 0 {
		return "", fmt.Errorf("no Amazon Linux 2 AMIs found")
	}

	// Find the most recent AMI
	var latestAMI ec2types.Image
	for _, ami := range result.Images {
		if latestAMI.ImageId == nil || *ami.CreationDate > *latestAMI.CreationDate {
			latestAMI = ami
		}
	}

	if latestAMI.ImageId == nil {
		return "", fmt.Errorf("no suitable AMI found")
	}

	log.Printf("Using Amazon Linux 2 AMI: %s (%s)", *latestAMI.ImageId, *latestAMI.Name)
	return *latestAMI.ImageId, nil
}

// launchInstance launches an EC2 instance
func (a *EC2CreationAction[T, S]) launchInstance(ctx context.Context, amiID, subnetID string) (*ec2types.Instance, error) {
	// Prepare launch parameters
	input := &ec2.RunInstancesInput{
		ImageId:      aws.String(amiID),
		InstanceType: ec2types.InstanceType(a.instanceType),
		MinCount:     aws.Int32(1),
		MaxCount:     aws.Int32(1),
		SubnetId:     aws.String(subnetID),
		NetworkInterfaces: []ec2types.InstanceNetworkInterfaceSpecification{
			{
				AssociateCarrierIpAddress:       new(bool),
				AssociatePublicIpAddress:        new(bool),
				ConnectionTrackingSpecification: &ec2types.ConnectionTrackingSpecificationRequest{},
				DeleteOnTermination:             new(bool),
				Description:                     new(string),
				DeviceIndex:                     new(int32),
				EnaSrdSpecification:             &ec2types.EnaSrdSpecificationRequest{},
				Groups:                          []string{},
				InterfaceType:                   new(string),
				Ipv4PrefixCount:                 new(int32),
				Ipv4Prefixes:                    []ec2types.Ipv4PrefixSpecificationRequest{},
				Ipv6AddressCount:                new(int32),
				Ipv6Addresses:                   []ec2types.InstanceIpv6Address{},
				Ipv6PrefixCount:                 new(int32),
				Ipv6Prefixes:                    []ec2types.Ipv6PrefixSpecificationRequest{},
				NetworkCardIndex:                new(int32),
				NetworkInterfaceId:              new(string),
				PrimaryIpv6:                     new(bool),
				PrivateIpAddress:                new(string),
				PrivateIpAddresses:              []ec2types.PrivateIpAddressSpecification{},
				SecondaryPrivateIpAddressCount:  new(int32),
				SubnetId:                        new(string),
			},
			{
				AssociateCarrierIpAddress:       new(bool),
				AssociatePublicIpAddress:        new(bool),
				ConnectionTrackingSpecification: &ec2types.ConnectionTrackingSpecificationRequest{},
				DeleteOnTermination:             new(bool),
				Description:                     new(string),
				DeviceIndex:                     new(int32),
				EnaSrdSpecification:             &ec2types.EnaSrdSpecificationRequest{},
				Groups:                          []string{},
				InterfaceType:                   new(string),
				Ipv4PrefixCount:                 new(int32),
				Ipv4Prefixes:                    []ec2types.Ipv4PrefixSpecificationRequest{},
				Ipv6AddressCount:                new(int32),
				Ipv6Addresses:                   []ec2types.InstanceIpv6Address{},
				Ipv6PrefixCount:                 new(int32),
				Ipv6Prefixes:                    []ec2types.Ipv6PrefixSpecificationRequest{},
				NetworkCardIndex:                new(int32),
				NetworkInterfaceId:              new(string),
				PrimaryIpv6:                     new(bool),
				PrivateIpAddress:                new(string),
				PrivateIpAddresses:              []ec2types.PrivateIpAddressSpecification{},
				SecondaryPrivateIpAddressCount:  new(int32),
				SubnetId:                        new(string),
			},
		},
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeInstance,
				Tags: []ec2types.Tag{
					{Key: aws.String("Name"), Value: aws.String("compensate-saga-instance")},
					{Key: aws.String("CreatedBy"), Value: aws.String("compensate-saga")},
				},
			},
		},
		AdditionalInfo:                    new(string),
		BlockDeviceMappings:               []ec2types.BlockDeviceMapping{},
		CapacityReservationSpecification:  &ec2types.CapacityReservationSpecification{},
		ClientToken:                       new(string),
		CpuOptions:                        &ec2types.CpuOptionsRequest{},
		CreditSpecification:               &ec2types.CreditSpecificationRequest{},
		DisableApiStop:                    new(bool),
		DisableApiTermination:             new(bool),
		DryRun:                            new(bool),
		EbsOptimized:                      new(bool),
		ElasticGpuSpecification:           []ec2types.ElasticGpuSpecification{},
		ElasticInferenceAccelerators:      []ec2types.ElasticInferenceAccelerator{},
		EnablePrimaryIpv6:                 new(bool),
		EnclaveOptions:                    &ec2types.EnclaveOptionsRequest{},
		HibernationOptions:                &ec2types.HibernationOptionsRequest{},
		IamInstanceProfile:                &ec2types.IamInstanceProfileSpecification{},
		InstanceInitiatedShutdownBehavior: "",
		InstanceMarketOptions:             &ec2types.InstanceMarketOptionsRequest{},
		Ipv6AddressCount:                  new(int32),
		Ipv6Addresses:                     []ec2types.InstanceIpv6Address{},
		KernelId:                          new(string),
		KeyName:                           new(string),
		LaunchTemplate:                    &ec2types.LaunchTemplateSpecification{},
		LicenseSpecifications:             []ec2types.LicenseConfigurationRequest{},
		MaintenanceOptions:                &ec2types.InstanceMaintenanceOptionsRequest{},
		MetadataOptions:                   &ec2types.InstanceMetadataOptionsRequest{},
		Monitoring:                        &ec2types.RunInstancesMonitoringEnabled{},
		Placement:                         &ec2types.Placement{},
		PrivateDnsNameOptions:             &ec2types.PrivateDnsNameOptionsRequest{},
		PrivateIpAddress:                  new(string),
		RamdiskId:                         new(string),
		SecurityGroupIds:                  []string{},
		SecurityGroups:                    []string{},
		UserData:                          new(string),
	}

	result, err := a.ec2Client.RunInstances(ctx, input)
	if err != nil {
		return nil, err
	}

	if len(result.Instances) == 0 {
		return nil, fmt.Errorf("no instances were created")
	}

	return &result.Instances[0], nil
}

// waitForInstanceRunning waits for the instance to reach running state
func (a *EC2CreationAction[T, S]) waitForInstanceRunning(ctx context.Context, instanceID string) (*ec2types.Instance, error) {
	log.Printf("Waiting for instance %s to reach running state...", instanceID)

	waiter := ec2.NewInstanceRunningWaiter(a.ec2Client)
	err := waiter.Wait(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	}, 10*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("instance did not reach running state: %w", err)
	}

	// Get the updated instance information
	describeResult, err := a.ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe instance: %w", err)
	}

	if len(describeResult.Reservations) == 0 || len(describeResult.Reservations[0].Instances) == 0 {
		return nil, fmt.Errorf("instance not found after creation")
	}

	return &describeResult.Reservations[0].Instances[0], nil
}

// terminateInstance terminates the EC2 instance
func (a *EC2CreationAction[T, S]) terminateInstance(ctx context.Context, instanceID string) error {
	input := &ec2.TerminateInstancesInput{
		InstanceIds: []string{instanceID},
	}

	_, err := a.ec2Client.TerminateInstances(ctx, input)
	if err != nil {
		return err
	}

	log.Printf("Initiated termination of instance %s", instanceID)

	// Wait for termination to complete
	waiter := ec2.NewInstanceTerminatedWaiter(a.ec2Client)
	err = waiter.Wait(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	}, 10*time.Minute)
	if err != nil {
		log.Printf("Warning: failed to wait for instance termination: %v", err)
		// Don't return error here as the termination was initiated
	}

	return nil
}

// getStringValue safely gets string value from pointer
func (a *EC2CreationAction[T, S]) getStringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// AttachSecurityGroupAction attaches the security group to the EC2 instance
type AttachSecurityGroupAction[T any, S compensate.SagaType[T]] struct {
	ec2Client *ec2.Client
}

// ConfigurationResult represents the final configuration result
type ConfigurationResult struct {
	ConnectionInfo string `json:"connection_info"`
	AccessDetails  string `json:"access_details"`
}

// NewAttachSecurityGroupAction creates a new attach security group action
func NewAttachSecurityGroupAction[T any, S compensate.SagaType[T]](ec2Client *ec2.Client) *AttachSecurityGroupAction[T, S] {
	return &AttachSecurityGroupAction[T, S]{
		ec2Client: ec2Client,
	}
}

// Name returns the action name
func (a *AttachSecurityGroupAction[T, S]) Name() compensate.ActionName {
	return "attach_security_group"
}

// DoIt attaches the security group to the instance and provides connection info
func (a *AttachSecurityGroupAction[T, S]) DoIt(ctx context.Context, sagaCtx compensate.ActionContext[T, S]) (compensate.ActionResult[compensate.ActionData], error) {
	log.Printf("Attaching security group to EC2 instance")

	// Get EC2 and Security Group info from previous steps
	ec2Result, ec2Found := compensate.LookupTyped[*EC2Result](sagaCtx, "ec2_instance")
	sgResult, sgFound := compensate.LookupTyped[*SecurityGroupResult](sagaCtx, "security_group")

	if !ec2Found {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("EC2 instance not found")
	}
	if !sgFound {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("Security Group not found")
	}

	// Attach security group to instance
	err := a.attachSecurityGroup(ctx, ec2Result.InstanceID, sgResult.SecurityGroupID)
	if err != nil {
		return compensate.ActionResult[compensate.ActionData]{}, fmt.Errorf("failed to attach security group: %w", err)
	}

	// Generate connection information
	connectionInfo := a.generateConnectionInfo(ec2Result)

	result := &ConfigurationResult{
		ConnectionInfo: connectionInfo,
		AccessDetails:  fmt.Sprintf("Instance %s is ready with security group %s", ec2Result.InstanceID, sgResult.SecurityGroupID),
	}

	log.Printf("Security group attachment completed successfully")
	return compensate.ActionResult[compensate.ActionData]{Output: result}, nil
}

// UndoIt detaches the security group (no-op since instance termination handles this)
func (a *AttachSecurityGroupAction[T, S]) UndoIt(ctx context.Context, sagaCtx compensate.ActionContext[T, S]) error {
	log.Printf("Security group detachment (no-op - handled by instance termination)")
	return nil
}

// attachSecurityGroup attaches the security group to the instance
func (a *AttachSecurityGroupAction[T, S]) attachSecurityGroup(ctx context.Context, instanceID, securityGroupID string) error {
	input := &ec2.ModifyInstanceAttributeInput{
		InstanceId: aws.String(instanceID),
		Groups:     []string{securityGroupID},
	}

	_, err := a.ec2Client.ModifyInstanceAttribute(ctx, input)
	return err
}

// generateConnectionInfo creates helpful connection information
func (a *AttachSecurityGroupAction[T, S]) generateConnectionInfo(ec2Result *EC2Result) string {
	if ec2Result.PublicIP == "" {
		return fmt.Sprintf("Instance %s is running but has no public IP. Use private IP: %s", ec2Result.InstanceID, ec2Result.PrivateIP)
	}

	return fmt.Sprintf(`Instance is ready for access:
- Instance ID: %s
- Public IP: %s
- Private IP: %s
- SSH Command: ssh -i your-key.pem ec2-user@%s
- State: %s`,
		ec2Result.InstanceID,
		ec2Result.PublicIP,
		ec2Result.PrivateIP,
		ec2Result.PublicIP,
		ec2Result.State)
}
