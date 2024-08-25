package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/fortressi/compensate"
)

// SimpleState holds the state for our simple saga
type SimpleState struct {
	Resources []string
}

// SimpleSaga implements SagaType
type SimpleSaga struct {
	State *SimpleState
}

func (s *SimpleSaga) ExecContext() *SimpleState {
	return s.State
}

// Resource creation/deletion results
type ResourceResult struct {
	ResourceID string
	CreatedAt  time.Time
}

func main() {
	log.Println("Manual Rollback Example - Demonstrating saga unwinding")

	ctx := context.Background()

	// Create state and saga context
	state := &SimpleState{Resources: []string{}}
	sagaContext := &SimpleSaga{State: state}

	// Create action registry
	registry := compensate.NewActionRegistry[*SimpleState, *SimpleSaga]()

	// Define actions with do/undo pairs
	databaseAction := compensate.NewActionFunc[*SimpleState, *SimpleSaga, *ResourceResult](
		"create_database",
		// DoIt - creates a database
		func(ctx context.Context, sagaCtx compensate.ActionContext[*SimpleState, *SimpleSaga]) (compensate.ActionFuncResult[*ResourceResult], error) {
			log.Println("📦 Creating database...")
			time.Sleep(1 * time.Second) // Simulate work

			result := &ResourceResult{
				ResourceID: "db-12345",
				CreatedAt:  time.Now(),
			}

			// Track in state
			sagaCtx.UserContext.Resources = append(sagaCtx.UserContext.Resources, result.ResourceID)

			log.Printf("✅ Database created: %s", result.ResourceID)
			return compensate.ActionFuncResult[*ResourceResult]{Output: result}, nil
		},
		// UndoIt - deletes the database
		func(ctx context.Context, sagaCtx compensate.ActionContext[*SimpleState, *SimpleSaga]) error {
			dbResult, found := compensate.LookupTyped[*ResourceResult](sagaCtx, "create_database")
			if !found {
				return fmt.Errorf("database result not found")
			}

			log.Printf("🗑️  Deleting database: %s...", dbResult.ResourceID)
			time.Sleep(1 * time.Second) // Simulate work

			// Remove from state
			newResources := []string{}
			for _, r := range sagaCtx.UserContext.Resources {
				if r != dbResult.ResourceID {
					newResources = append(newResources, r)
				}
			}
			sagaCtx.UserContext.Resources = newResources

			log.Printf("✅ Database deleted: %s", dbResult.ResourceID)
			return nil
		},
	)

	cacheAction := compensate.NewActionFunc[*SimpleState, *SimpleSaga, *ResourceResult](
		"create_cache",
		// DoIt - creates a cache instance
		func(ctx context.Context, sagaCtx compensate.ActionContext[*SimpleState, *SimpleSaga]) (compensate.ActionFuncResult[*ResourceResult], error) {
			log.Println("💾 Creating cache instance...")
			time.Sleep(1 * time.Second)

			result := &ResourceResult{
				ResourceID: "cache-67890",
				CreatedAt:  time.Now(),
			}

			sagaCtx.UserContext.Resources = append(sagaCtx.UserContext.Resources, result.ResourceID)

			log.Printf("✅ Cache created: %s", result.ResourceID)
			return compensate.ActionFuncResult[*ResourceResult]{Output: result}, nil
		},
		// UndoIt - deletes the cache
		func(ctx context.Context, sagaCtx compensate.ActionContext[*SimpleState, *SimpleSaga]) error {
			cacheResult, found := compensate.LookupTyped[*ResourceResult](sagaCtx, "create_cache")
			if !found {
				return fmt.Errorf("cache result not found")
			}

			log.Printf("🗑️  Deleting cache: %s...", cacheResult.ResourceID)
			time.Sleep(1 * time.Second)

			// Remove from state
			newResources := []string{}
			for _, r := range sagaCtx.UserContext.Resources {
				if r != cacheResult.ResourceID {
					newResources = append(newResources, r)
				}
			}
			sagaCtx.UserContext.Resources = newResources

			log.Printf("✅ Cache deleted: %s", cacheResult.ResourceID)
			return nil
		},
	)

	appServerAction := compensate.NewActionFunc[*SimpleState, *SimpleSaga, *ResourceResult](
		"create_app_server",
		// DoIt - creates application server
		func(ctx context.Context, sagaCtx compensate.ActionContext[*SimpleState, *SimpleSaga]) (compensate.ActionFuncResult[*ResourceResult], error) {
			// Check dependencies exist
			_, dbFound := compensate.LookupTyped[*ResourceResult](sagaCtx, "create_database")
			_, cacheFound := compensate.LookupTyped[*ResourceResult](sagaCtx, "create_cache")

			if !dbFound || !cacheFound {
				return compensate.ActionFuncResult[*ResourceResult]{}, fmt.Errorf("dependencies not found")
			}

			log.Println("🖥️  Creating application server...")
			time.Sleep(2 * time.Second) // Takes longer

			result := &ResourceResult{
				ResourceID: "app-server-11111",
				CreatedAt:  time.Now(),
			}

			sagaCtx.UserContext.Resources = append(sagaCtx.UserContext.Resources, result.ResourceID)

			log.Printf("✅ App server created: %s", result.ResourceID)
			return compensate.ActionFuncResult[*ResourceResult]{Output: result}, nil
		},
		// UndoIt - shuts down app server
		func(ctx context.Context, sagaCtx compensate.ActionContext[*SimpleState, *SimpleSaga]) error {
			appResult, found := compensate.LookupTyped[*ResourceResult](sagaCtx, "create_app_server")
			if !found {
				return fmt.Errorf("app server result not found")
			}

			log.Printf("🗑️  Shutting down app server: %s...", appResult.ResourceID)
			time.Sleep(1 * time.Second)

			// Remove from state
			newResources := []string{}
			for _, r := range sagaCtx.UserContext.Resources {
				if r != appResult.ResourceID {
					newResources = append(newResources, r)
				}
			}
			sagaCtx.UserContext.Resources = newResources

			log.Printf("✅ App server shut down: %s", appResult.ResourceID)
			return nil
		},
	)

	// Build DAG
	builder := compensate.NewDagBuilder[*SimpleState, *SimpleSaga]("ManualRollbackExample", registry)

	// Database and cache can be created in parallel
	err := builder.AppendParallel(
		&compensate.ActionNodeKind[*SimpleState, *SimpleSaga]{
			NodeName: "create_database",
			Label:    "Create Database",
			Action:   databaseAction,
		},
		&compensate.ActionNodeKind[*SimpleState, *SimpleSaga]{
			NodeName: "create_cache",
			Label:    "Create Cache",
			Action:   cacheAction,
		},
	)
	if err != nil {
		log.Fatalf("Failed to add parallel actions: %v", err)
	}

	// App server depends on both database and cache
	err = builder.Append(&compensate.ActionNodeKind[*SimpleState, *SimpleSaga]{
		NodeName: "create_app_server",
		Label:    "Create App Server",
		Action:   appServerAction,
	})
	if err != nil {
		log.Fatalf("Failed to add app server action: %v", err)
	}

	// Build the DAG
	dag, err := builder.Build()
	if err != nil {
		log.Fatalf("Failed to build DAG: %v", err)
	}

	// Create saga DAG
	sagaDag := compensate.NewSagaDag(dag, json.RawMessage(`{}`))

	// Create executor with memory store (for demonstration)
	store := compensate.NewMemoryStore[*SimpleState]()
	sagaID := "manual-rollback-demo"
	executor := compensate.NewSagaExecutor(sagaDag, registry, sagaContext, sagaID, store)

	// Execute the saga
	log.Println("\n🚀 Phase 1: Creating resources...")
	err = executor.Execute(ctx)
	if err != nil {
		log.Fatalf("❌ Saga execution failed: %v", err)
	}

	log.Println("\n✅ All resources created successfully!")
	log.Printf("Current resources: %v", state.Resources)

	// Simulate using the resources
	log.Println("\n⏳ Simulating resource usage for 5 seconds...")
	time.Sleep(5 * time.Second)

	// Now manually trigger rollback to clean up
	log.Println("\n🔄 Phase 2: Manually triggering rollback to clean up resources...")
	err = executor.Rollback(ctx)
	if err != nil {
		log.Fatalf("❌ Rollback failed: %v", err)
	}

	log.Println("\n✅ Rollback completed successfully!")
	log.Printf("Final resources (should be empty): %v", state.Resources)

	// Demonstrate that rollback uses the saga context
	log.Println("\n📊 Execution summary:")
	executionOrder := executor.GetExecutionOrder()
	log.Printf("Actions executed: %v", executionOrder)
	log.Println("Note: Rollback executed actions in reverse order, using stored resource IDs from the saga context")
}

