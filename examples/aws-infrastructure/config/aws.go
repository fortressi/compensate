package config

import (
	"context"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// AWSConfig holds the AWS configuration and clients
type AWSConfig struct {
	Config aws.Config
	EC2    *ec2.Client
	Region string
}

// NewAWSConfig creates a new AWS configuration with the specified region
func NewAWSConfig(ctx context.Context, region string) (*AWSConfig, error) {
	// Load the default AWS configuration
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create EC2 client
	ec2Client := ec2.NewFromConfig(cfg)

	awsConfig := &AWSConfig{
		Config: cfg,
		EC2:    ec2Client,
		Region: region,
	}

	// Validate AWS credentials by making a simple API call
	if err := awsConfig.validateCredentials(ctx); err != nil {
		return nil, fmt.Errorf("AWS credentials validation failed: %w", err)
	}

	log.Printf("AWS configuration initialized for region: %s", region)
	return awsConfig, nil
}

// validateCredentials validates that we can make AWS API calls
func (c *AWSConfig) validateCredentials(ctx context.Context) error {
	// Try to describe regions to validate credentials
	_, err := c.EC2.DescribeRegions(ctx, &ec2.DescribeRegionsInput{})
	if err != nil {
		return fmt.Errorf("failed to validate AWS credentials: %w", err)
	}
	return nil
}

// GetLatestAmazonLinuxAMI returns the latest Amazon Linux 2 AMI ID for the configured region
func (c *AWSConfig) GetLatestAmazonLinuxAMI(ctx context.Context) (string, error) {
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

	result, err := c.EC2.DescribeImages(ctx, input)
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

// DefaultVPCCIDR returns the default CIDR block for VPC creation
func DefaultVPCCIDR() string {
	return "10.0.0.0/16"
}

// DefaultSubnetCIDR returns the default CIDR block for subnet creation
func DefaultSubnetCIDR() string {
	return "10.0.1.0/24"
}

// GetAvailabilityZone returns the first availability zone for the configured region
func (c *AWSConfig) GetAvailabilityZone(ctx context.Context) (string, error) {
	input := &ec2.DescribeAvailabilityZonesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("state"),
				Values: []string{"available"},
			},
		},
	}

	result, err := c.EC2.DescribeAvailabilityZones(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to describe availability zones: %w", err)
	}

	if len(result.AvailabilityZones) == 0 {
		return "", fmt.Errorf("no availability zones found")
	}

	return *result.AvailabilityZones[0].ZoneName, nil
}