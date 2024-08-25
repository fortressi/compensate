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
	"github.com/google/uuid"
)

// ResourceState represents mock resources being managed
type ResourceState struct {
	ID        string            `json:"id"`
	Resources map[string]string `json:"resources"`
}

// ResourceSaga implements the SagaType interface
type ResourceSaga struct {
	State *ResourceState
}

func (s *ResourceSaga) ExecContext() *ResourceState {
	return s.State
}

func main() {
	// Define commands
	deployCmd := flag.NewFlagSet("deploy", flag.ExitOnError)
	destroyCmd := flag.NewFlagSet("destroy", flag.ExitOnError)

	// Deploy command flags
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
		if err := runDeploy(*deployStateDir, *deploySagaID); err != nil {
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
	fmt.Println("Persistent Saga CLI Example")
	fmt.Println("\nUsage:")
	fmt.Println("  persistent_cli deploy [flags]   - Deploy resources")
	fmt.Println("  persistent_cli destroy [flags]  - Destroy previously deployed resources")
	fmt.Println("  persistent_cli list [state-dir] - List all sagas")
	fmt.Println("\nDeploy flags:")
	fmt.Println("  --state-dir      Directory to store saga state (default: ./saga-state)")
	fmt.Println("  --saga-id        Unique saga ID (auto-generated if not provided)")
	fmt.Println("\nDestroy flags:")
	fmt.Println("  --state-dir      Directory containing saga state (default: ./saga-state)")
	fmt.Println("  --saga-id        Saga ID to destroy (required)")
}

func runDeploy(stateDir, sagaID string) error {
	ctx := context.Background()

	// Generate saga ID if not provided
	if sagaID == "" {
		sagaID = fmt.Sprintf("resource-%s", uuid.New().String()[:8])
	}

	log.Printf("🚀 Deploying resources with saga ID: %s", sagaID)
	log.Printf("State will be persisted to: %s", stateDir)

	// Create file store
	store, err := compensate.NewFileStore[*ResourceState](stateDir)
	if err != nil {
		return fmt.Errorf("failed to create file store: %w", err)
	}

	// Check if saga already exists
	if _, err := store.Load(ctx, sagaID); err == nil {
		return fmt.Errorf("saga with ID %s already exists. Use a different ID or destroy the existing saga first", sagaID)
	}

	// Create resource state
	resourceState := &ResourceState{
		ID:        sagaID,
		Resources: make(map[string]string),
	}
	sagaContext := &ResourceSaga{State: resourceState}

	// Create action registry
	registry := compensate.NewActionRegistry[*ResourceState, *ResourceSaga]()

	// Create mock actions
	createDatabaseAction := compensate.NewActionFunc[*ResourceState, *ResourceSaga, string](
		"create_database",
		func(ctx context.Context, sgctx compensate.ActionContext[*ResourceState, *ResourceSaga]) (compensate.ActionFuncResult[string], error) {
			dbID := fmt.Sprintf("db-%s", uuid.New().String()[:8])
			sgctx.UserContext.Resources["database"] = dbID
			log.Printf("📀 Created database: %s", dbID)
			time.Sleep(500 * time.Millisecond) // Simulate work
			return compensate.ActionFuncResult[string]{Output: dbID}, nil
		},
		func(ctx context.Context, sgctx compensate.ActionContext[*ResourceState, *ResourceSaga]) error {
			if dbID, ok := sgctx.UserContext.Resources["database"]; ok {
				log.Printf("🗑️  Deleting database: %s", dbID)
				delete(sgctx.UserContext.Resources, "database")
				time.Sleep(300 * time.Millisecond) // Simulate work
			}
			return nil
		},
	)

	createServerAction := compensate.NewActionFunc[*ResourceState, *ResourceSaga, string](
		"create_server",
		func(ctx context.Context, sgctx compensate.ActionContext[*ResourceState, *ResourceSaga]) (compensate.ActionFuncResult[string], error) {
			serverID := fmt.Sprintf("server-%s", uuid.New().String()[:8])
			sgctx.UserContext.Resources["server"] = serverID
			log.Printf("🖥️  Created server: %s", serverID)
			time.Sleep(1 * time.Second) // Simulate work
			return compensate.ActionFuncResult[string]{Output: serverID}, nil
		},
		func(ctx context.Context, sgctx compensate.ActionContext[*ResourceState, *ResourceSaga]) error {
			if serverID, ok := sgctx.UserContext.Resources["server"]; ok {
				log.Printf("🗑️  Deleting server: %s", serverID)
				delete(sgctx.UserContext.Resources, "server")
				time.Sleep(500 * time.Millisecond) // Simulate work
			}
			return nil
		},
	)

	createLoadBalancerAction := compensate.NewActionFunc[*ResourceState, *ResourceSaga, string](
		"create_loadbalancer",
		func(ctx context.Context, sgctx compensate.ActionContext[*ResourceState, *ResourceSaga]) (compensate.ActionFuncResult[string], error) {
			lbID := fmt.Sprintf("lb-%s", uuid.New().String()[:8])
			sgctx.UserContext.Resources["loadbalancer"] = lbID
			log.Printf("⚖️  Created load balancer: %s", lbID)
			time.Sleep(700 * time.Millisecond) // Simulate work
			return compensate.ActionFuncResult[string]{Output: lbID}, nil
		},
		func(ctx context.Context, sgctx compensate.ActionContext[*ResourceState, *ResourceSaga]) error {
			if lbID, ok := sgctx.UserContext.Resources["loadbalancer"]; ok {
				log.Printf("🗑️  Deleting load balancer: %s", lbID)
				delete(sgctx.UserContext.Resources, "loadbalancer")
				time.Sleep(400 * time.Millisecond) // Simulate work
			}
			return nil
		},
	)

	// Register actions
	registry.Register(createDatabaseAction)
	registry.Register(createServerAction)
	registry.Register(createLoadBalancerAction)

	// Build the DAG
	builder := compensate.NewDagBuilder[*ResourceState, *ResourceSaga]("ResourceProvisioning", registry)

	// Database first
	err = builder.Append(&compensate.ActionNodeKind[*ResourceState, *ResourceSaga]{
		NodeName: "database",
		Label:    "Create Database",
		Action:   createDatabaseAction,
	})
	if err != nil {
		return err
	}

	// Server depends on database
	err = builder.Append(&compensate.ActionNodeKind[*ResourceState, *ResourceSaga]{
		NodeName: "server",
		Label:    "Create Server",
		Action:   createServerAction,
	})
	if err != nil {
		return err
	}

	// Load balancer depends on server
	err = builder.Append(&compensate.ActionNodeKind[*ResourceState, *ResourceSaga]{
		NodeName: "loadbalancer",
		Label:    "Create Load Balancer",
		Action:   createLoadBalancerAction,
	})
	if err != nil {
		return err
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
		log.Printf("❌ Resource provisioning failed: %v", err)
		log.Printf("Note: The saga has automatically rolled back any created resources")
		return err
	}

	duration := time.Since(startTime)
	log.Printf("✅ Resource provisioning completed successfully in %v", duration)

	// Print resource details
	log.Printf("\nResources created:")
	for resourceType, resourceID := range resourceState.Resources {
		log.Printf("  %s: %s", resourceType, resourceID)
	}
	log.Printf("\nSaga ID: %s", sagaID)
	log.Printf("State persisted to: %s", filepath.Join(stateDir, sagaID+".json"))
	log.Printf("\nTo destroy these resources later, run:")
	log.Printf("  ./persistent_cli destroy --saga-id %s --state-dir %s", sagaID, stateDir)

	return nil
}

func runDestroy(stateDir, sagaID string) error {
	ctx := context.Background()

	log.Printf("🔄 Destroying resources for saga ID: %s", sagaID)

	// Create file store
	store, err := compensate.NewFileStore[*ResourceState](stateDir)
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

	// Show resources to be destroyed
	log.Printf("\nResources to destroy:")
	for resourceType, resourceID := range state.Context.Resources {
		log.Printf("  %s: %s", resourceType, resourceID)
	}

	// Create saga context with loaded state
	sagaContext := &ResourceSaga{State: state.Context}

	// Create action registry (same as deploy)
	registry := compensate.NewActionRegistry[*ResourceState, *ResourceSaga]()

	// Re-create the same actions (they need to match for undo)
	createDatabaseAction := compensate.NewActionFunc[*ResourceState, *ResourceSaga, string](
		"create_database",
		func(ctx context.Context, sgctx compensate.ActionContext[*ResourceState, *ResourceSaga]) (compensate.ActionFuncResult[string], error) {
			// Not used during destroy
			return compensate.ActionFuncResult[string]{}, nil
		},
		func(ctx context.Context, sgctx compensate.ActionContext[*ResourceState, *ResourceSaga]) error {
			if dbID, ok := sgctx.UserContext.Resources["database"]; ok {
				log.Printf("🗑️  Deleting database: %s", dbID)
				delete(sgctx.UserContext.Resources, "database")
				time.Sleep(300 * time.Millisecond) // Simulate work
			}
			return nil
		},
	)

	createServerAction := compensate.NewActionFunc[*ResourceState, *ResourceSaga, string](
		"create_server",
		func(ctx context.Context, sgctx compensate.ActionContext[*ResourceState, *ResourceSaga]) (compensate.ActionFuncResult[string], error) {
			// Not used during destroy
			return compensate.ActionFuncResult[string]{}, nil
		},
		func(ctx context.Context, sgctx compensate.ActionContext[*ResourceState, *ResourceSaga]) error {
			if serverID, ok := sgctx.UserContext.Resources["server"]; ok {
				log.Printf("🗑️  Deleting server: %s", serverID)
				delete(sgctx.UserContext.Resources, "server")
				time.Sleep(500 * time.Millisecond) // Simulate work
			}
			return nil
		},
	)

	createLoadBalancerAction := compensate.NewActionFunc[*ResourceState, *ResourceSaga, string](
		"create_loadbalancer",
		func(ctx context.Context, sgctx compensate.ActionContext[*ResourceState, *ResourceSaga]) (compensate.ActionFuncResult[string], error) {
			// Not used during destroy
			return compensate.ActionFuncResult[string]{}, nil
		},
		func(ctx context.Context, sgctx compensate.ActionContext[*ResourceState, *ResourceSaga]) error {
			if lbID, ok := sgctx.UserContext.Resources["loadbalancer"]; ok {
				log.Printf("🗑️  Deleting load balancer: %s", lbID)
				delete(sgctx.UserContext.Resources, "loadbalancer")
				time.Sleep(400 * time.Millisecond) // Simulate work
			}
			return nil
		},
	)

	// Register actions
	registry.Register(createDatabaseAction)
	registry.Register(createServerAction)
	registry.Register(createLoadBalancerAction)

	// Rebuild the DAG (same structure as deploy)
	builder := compensate.NewDagBuilder[*ResourceState, *ResourceSaga]("ResourceProvisioning", registry)

	// Same DAG structure
	builder.Append(&compensate.ActionNodeKind[*ResourceState, *ResourceSaga]{
		NodeName: "database",
		Label:    "Create Database",
		Action:   createDatabaseAction,
	})
	builder.Append(&compensate.ActionNodeKind[*ResourceState, *ResourceSaga]{
		NodeName: "server",
		Label:    "Create Server",
		Action:   createServerAction,
	})
	builder.Append(&compensate.ActionNodeKind[*ResourceState, *ResourceSaga]{
		NodeName: "loadbalancer",
		Label:    "Create Load Balancer",
		Action:   createLoadBalancerAction,
	})

	dag, err := builder.Build()
	if err != nil {
		return fmt.Errorf("failed to build DAG: %w", err)
	}

	// Create runnable saga DAG
	sagaDag := compensate.NewSagaDag(dag, json.RawMessage(`{}`))

	// Create executor from saved state
	executor := compensate.NewExecutorFromState(sagaDag, registry, sagaContext, state, store)

	// Perform rollback
	log.Printf("\nStarting rollback of %d completed actions...", len(state.CompletedActions))

	err = executor.Rollback(ctx)
	if err != nil {
		log.Printf("❌ Rollback failed: %v", err)
		return err
	}

	// Delete the saga state after successful rollback
	if err := store.Delete(ctx, sagaID); err != nil {
		log.Printf("Warning: failed to delete saga state: %v", err)
	}

	log.Printf("\n✅ Resources destroyed successfully")
	log.Printf("All resources have been cleaned up")

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
	store, err := compensate.NewFileStore[*ResourceState](stateDir)
	if err != nil {
		return fmt.Errorf("failed to create file store: %w", err)
	}

	fmt.Printf("Found %d saga(s):\n\n", len(files))
	fmt.Printf("%-30s %-20s %-25s %-10s\n", "SAGA ID", "STATUS", "CREATED", "RESOURCES")
	fmt.Printf("%-30s %-20s %-25s %-10s\n", "-------", "------", "-------", "---------")

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

		fmt.Printf("%-30s %-20s %-25s %-10d\n",
			sagaID,
			state.Status,
			state.CreatedAt.Format(time.RFC822),
			len(state.Context.Resources),
		)

		// Show resource details
		if len(state.Context.Resources) > 0 {
			for resourceType, resourceID := range state.Context.Resources {
				fmt.Printf("  %s: %s\n", resourceType, resourceID)
			}
		}
	}

	return nil
}