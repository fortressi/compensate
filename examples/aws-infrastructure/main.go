package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/fortressi/compensate"
	"github.com/fortressi/compensate/examples/aws-infrastructure/actions"
	"github.com/fortressi/compensate/examples/aws-infrastructure/config"
	"github.com/google/uuid"
)

func main() {
	// Define commands
	deployCmd := flag.NewFlagSet("deploy", flag.ExitOnError)
	destroyCmd := flag.NewFlagSet("destroy", flag.ExitOnError)

	// Deploy command flags
	deployRegion := deployCmd.String("region", "us-west-2", "AWS region")
	deployStateDir := deployCmd.String("state-dir", "./saga-state", "Directory to store saga state")
	deploySagaID := deployCmd.String("saga-id", "", "Unique saga ID (auto-generated if not provided)")

	// Destroy command flags
	destroyStateDir := destroyCmd.String("state-dir", "./saga-state", "Directory containing saga state")
	destroySagaID := destroyCmd.String("saga-id", "", "Saga ID to destroy (required)")

	// Parse command
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "deploy":
		deployCmd.Parse(os.Args[2:])
		if err := runDeploy(*deployRegion, *deployStateDir, *deploySagaID); err != nil {
			log.Fatalf("Deploy failed: %v", err)
		}
	case "destroy":
		destroyCmd.Parse(os.Args[2:])
		if *destroySagaID == "" {
			log.Fatal("--saga-id is required for destroy command")
		}
		if err := runDestroy(*destroyStateDir, *destroySagaID); err != nil {
			log.Fatalf("Destroy failed: %v", err)
		}
	case "list":
		// List all sagas
		stateDir := "./saga-state"
		if len(os.Args) > 2 {
			stateDir = os.Args[2]
		}
		if err := listSagas(stateDir); err != nil {
			log.Fatalf("List failed: %v", err)
		}
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("AWS VPC Infrastructure Saga CLI")
	fmt.Println("\nUsage:")
	fmt.Println("  aws-infrastructure deploy [flags]   - Deploy AWS VPC infrastructure")
	fmt.Println("  aws-infrastructure destroy [flags]  - Destroy previously deployed infrastructure")
	fmt.Println("  aws-infrastructure list [state-dir] - List all sagas")
	fmt.Println("\nDeploy flags:")
	fmt.Println("  --region         AWS region (default: us-west-2)")
	fmt.Println("  --state-dir      Directory to store saga state (default: ./saga-state)")
	fmt.Println("  --saga-id        Unique saga ID (auto-generated if not provided)")
	fmt.Println("\nDestroy flags:")
	fmt.Println("  --state-dir      Directory containing saga state (default: ./saga-state)")
	fmt.Println("  --saga-id        Saga ID to destroy (required)")
}

func runDeploy(region, stateDir, sagaID string) error {
	ctx := context.Background()

	// Generate saga ID if not provided
	if sagaID == "" {
		sagaID = fmt.Sprintf("vpc-%s", uuid.New().String()[:8])
	}

	log.Printf("🚀 Deploying AWS VPC infrastructure with saga ID: %s", sagaID)
	log.Printf("Region: %s", region)
	log.Printf("State will be persisted to: %s", stateDir)

	// Create file store
	store, err := compensate.NewFileStore[*InfrastructureState](stateDir)
	if err != nil {
		return fmt.Errorf("failed to create file store: %w", err)
	}

	// Check if saga already exists
	if _, err := store.Load(ctx, sagaID); err == nil {
		return fmt.Errorf("saga with ID %s already exists. Use a different ID or destroy the existing saga first", sagaID)
	}

	// Initialize AWS configuration
	awsConfig, err := config.NewAWSConfig(ctx, region)
	if err != nil {
		return fmt.Errorf("failed to initialize AWS config: %w", err)
	}

	// Create infrastructure state
	infraState := NewInfrastructureState(region, "")
	sagaContext := &InfrastructureSaga{State: infraState}

	// Create action registry
	registry := compensate.NewActionRegistry[*InfrastructureState, *InfrastructureSaga]()

	// Register all actions
	if err := registerActions(registry, awsConfig, region); err != nil {
		return fmt.Errorf("failed to register actions: %w", err)
	}

	// Build the DAG
	builder := compensate.NewDagBuilder[*InfrastructureState, *InfrastructureSaga]("AWSVPCInfra", registry)
	if err := buildDAG(builder); err != nil {
		return fmt.Errorf("failed to build DAG: %w", err)
	}

	dag, err := builder.Build()
	if err != nil {
		return fmt.Errorf("failed to build DAG: %w", err)
	}

	// Create runnable saga DAG
	sagaDag := compensate.NewSagaDag(dag, json.RawMessage(`{}`))

	// Create executor with persistence
	executor := compensate.NewSagaExecutor(sagaDag, registry, sagaContext, sagaID, store)

	// Execute the saga
	startTime := time.Now()
	err = executor.Execute(ctx)

	if err != nil {
		log.Printf("❌ Infrastructure provisioning failed: %v", err)
		log.Printf("Note: The saga has automatically rolled back any created resources")
		return err
	}

	duration := time.Since(startTime)
	log.Printf("✅ Infrastructure provisioning completed successfully in %v", duration)

	// Print infrastructure details
	log.Printf("\nInfrastructure created:")
	fmt.Println(infraState.String())
	log.Printf("\nSaga ID: %s", sagaID)
	log.Printf("State persisted to: %s", filepath.Join(stateDir, sagaID+".json"))
	log.Printf("\nTo destroy this infrastructure later, run:")
	log.Printf("  ./aws-infrastructure destroy --saga-id %s --state-dir %s", sagaID, stateDir)

	return nil
}

func runDestroy(stateDir, sagaID string) error {
	ctx := context.Background()

	log.Printf("🔄 Destroying infrastructure for saga ID: %s", sagaID)

	// Create file store
	store, err := compensate.NewFileStore[*InfrastructureState](stateDir)
	if err != nil {
		return fmt.Errorf("failed to create file store: %w", err)
	}

	// Load saga state
	state, err := store.Load(ctx, sagaID)
	if err != nil {
		return fmt.Errorf("failed to load saga state: %w", err)
	}

	log.Printf("Original deployment: Status=%s, DeployedAt=%s",
		state.Status, state.CreatedAt.Format(time.RFC822))

	// Check if already rolled back
	if state.Status == compensate.SagaStatusRolledBack {
		log.Printf("Saga already rolled back")
		return nil
	}

	// Initialize AWS configuration
	awsConfig, err := config.NewAWSConfig(ctx, state.Context.Region)
	if err != nil {
		return fmt.Errorf("failed to initialize AWS config: %w", err)
	}

	// Create saga context with loaded state
	sagaContext := &InfrastructureSaga{State: state.Context}

	// Create action registry
	registry := compensate.NewActionRegistry[*InfrastructureState, *InfrastructureSaga]()

	// Register all actions
	if err := registerActions(registry, awsConfig, state.Context.Region); err != nil {
		return fmt.Errorf("failed to register actions: %w", err)
	}

	// Rebuild the DAG (same structure as deploy)
	builder := compensate.NewDagBuilder[*InfrastructureState, *InfrastructureSaga]("AWSVPCInfra", registry)
	if err := buildDAG(builder); err != nil {
		return fmt.Errorf("failed to build DAG: %w", err)
	}

	dag, err := builder.Build()
	if err != nil {
		return fmt.Errorf("failed to build DAG: %w", err)
	}

	// Create runnable saga DAG
	sagaDag := compensate.NewSagaDag(dag, json.RawMessage(`{}`))

	// Create executor from saved state
	executor := compensate.NewExecutorFromState(sagaDag, registry, sagaContext, state, store)

	// Perform rollback
	log.Printf("Found %d completed actions to rollback", len(state.CompletedActions))

	err = executor.Rollback(ctx)
	if err != nil {
		log.Printf("❌ Rollback failed: %v", err)
		return err
	}

	// Delete the saga state after successful rollback
	if err := store.Delete(ctx, sagaID); err != nil {
		log.Printf("Warning: failed to delete saga state: %v", err)
	}

	log.Printf("✅ Infrastructure destroyed successfully")
	log.Printf("All AWS resources have been deprovisioned")

	return nil
}

func listSagas(stateDir string) error {
	// Get all JSON files in the state directory
	files, err := filepath.Glob(filepath.Join(stateDir, "*.json"))
	if err != nil {
		return fmt.Errorf("failed to list saga files: %w", err)
	}

	if len(files) == 0 {
		fmt.Println("No sagas found")
		return nil
	}

	// Create a temporary store to load states
	store, err := compensate.NewFileStore[*InfrastructureState](stateDir)
	if err != nil {
		return fmt.Errorf("failed to create file store: %w", err)
	}

	fmt.Printf("Found %d saga(s):\n\n", len(files))
	fmt.Printf("%-30s %-20s %-25s %-15s\n", "SAGA ID", "STATUS", "CREATED", "REGION")
	fmt.Printf("%-30s %-20s %-25s %-15s\n", "-------", "------", "-------", "------")

	ctx := context.Background()
	for _, file := range files {
		// Extract saga ID from filename
		sagaID := filepath.Base(file)
		sagaID = sagaID[:len(sagaID)-5] // Remove .json extension

		state, err := store.Load(ctx, sagaID)
		if err != nil {
			fmt.Printf("%-30s ERROR: %v\n", sagaID, err)
			continue
		}

		fmt.Printf("%-30s %-20s %-25s %-15s\n",
			sagaID,
			state.Status,
			state.CreatedAt.Format(time.RFC822),
			state.Context.Region,
		)

		// Show created resources
		if len(state.Context.CreatedResources) > 0 {
			fmt.Printf("  Resources: %v\n", state.Context.CreatedResources)
		}
	}

	return nil
}

// registerActions registers all the AWS infrastructure actions
func registerActions(
	registry *compensate.ActionRegistry[*InfrastructureState, *InfrastructureSaga],
	awsConfig *config.AWSConfig,
	region string,
) error {
	// Create all actions
	createVPCAction := actions.NewCreateVPCAction[*InfrastructureState, *InfrastructureSaga](
		awsConfig.EC2, region, config.DefaultVPCCIDR())

	createIGWAction := actions.NewCreateInternetGatewayAction[*InfrastructureState, *InfrastructureSaga](
		awsConfig.EC2)

	createSubnetAction := actions.NewCreateSubnetAction[*InfrastructureState, *InfrastructureSaga](
		awsConfig.EC2, region, config.DefaultSubnetCIDR())

	createRouteTableAction := actions.NewCreateRouteTableAction[*InfrastructureState, *InfrastructureSaga](
		awsConfig.EC2)

	// Register all actions
	registry.Register(createVPCAction)
	registry.Register(createIGWAction)
	registry.Register(createSubnetAction)
	registry.Register(createRouteTableAction)

	return nil
}

// buildDAG builds the infrastructure provisioning DAG
func buildDAG(builder *compensate.DagBuilder[*InfrastructureState, *InfrastructureSaga]) error {
	// Create VPC first
	err := builder.Append(&compensate.ActionNodeKind[*InfrastructureState, *InfrastructureSaga]{
		NodeName: "create_vpc",
		Label:    "Create VPC",
		Action:   actions.NewCreateVPCAction[*InfrastructureState, *InfrastructureSaga](nil, "", ""),
	})
	if err != nil {
		return err
	}

	// Create Internet Gateway
	err = builder.Append(&compensate.ActionNodeKind[*InfrastructureState, *InfrastructureSaga]{
		NodeName: "create_internet_gateway",
		Label:    "Create Internet Gateway",
		Action:   actions.NewCreateInternetGatewayAction[*InfrastructureState, *InfrastructureSaga](nil),
	})
	if err != nil {
		return err
	}

	// Create Subnet
	err = builder.Append(&compensate.ActionNodeKind[*InfrastructureState, *InfrastructureSaga]{
		NodeName: "create_subnet",
		Label:    "Create Subnet",
		Action:   actions.NewCreateSubnetAction[*InfrastructureState, *InfrastructureSaga](nil, "", ""),
	})
	if err != nil {
		return err
	}

	// Create Route Table
	err = builder.Append(&compensate.ActionNodeKind[*InfrastructureState, *InfrastructureSaga]{
		NodeName: "create_route_table",
		Label:    "Create Route Table",
		Action:   actions.NewCreateRouteTableAction[*InfrastructureState, *InfrastructureSaga](nil),
	})
	if err != nil {
		return err
	}

	return nil
}

