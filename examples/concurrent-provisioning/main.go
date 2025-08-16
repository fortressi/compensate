// Example: Concurrent Infrastructure Provisioning
//
// This example demonstrates how to use the Compensate library's concurrent execution
// feature to provision cloud infrastructure resources in parallel, reducing total
// provisioning time.
//
// The example simulates provisioning:
// - VPC (Virtual Private Cloud)
// - Three parallel resources: Database, Cache, Queue
// - App Server (depends on all three resources)
//
// With sequential execution, this would take ~120s (30s per resource).
// With concurrent execution, it takes ~60s (VPC + parallel resources + app).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/fortressi/compensate"
)

// InfraState represents the state of our infrastructure provisioning
type InfraState struct {
	VPCID        string            `json:"vpc_id"`
	DatabaseHost string            `json:"database_host"`
	CacheHost    string            `json:"cache_host"`
	QueueURL     string            `json:"queue_url"`
	AppServerIP  string            `json:"app_server_ip"`
	Resources    map[string]string `json:"resources"`
}

// InfraSaga wraps the infrastructure state
type InfraSaga struct {
	State *InfraState
}

func (s *InfraSaga) ExecContext() *InfraState {
	return s.State
}

// ResourceInfo contains information about a provisioned resource
type ResourceInfo struct {
	ResourceID   string    `json:"resource_id"`
	ResourceType string    `json:"resource_type"`
	Endpoint     string    `json:"endpoint,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func main() {
	// Configure structured logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Create initial state
	state := &InfraState{
		Resources: make(map[string]string),
	}
	saga := &InfraSaga{State: state}

	// Create action registry
	registry := compensate.NewActionRegistry[*InfraState, *InfraSaga]()

	// Register all infrastructure actions
	registerInfraActions(registry)

	// Build the provisioning DAG
	builder := compensate.NewDagBuilder[*InfraState, *InfraSaga]("CloudProvisioning", registry)

	// Level 1: Create VPC
	err := builder.Append(&compensate.ActionNodeKind[*InfraState, *InfraSaga]{
		NodeName: "create_vpc",
		Label:    "Create VPC",
		Action:   createVPCAction(),
	})
	if err != nil {
		log.Fatalf("Failed to add VPC action: %v", err)
	}

	// Level 2: Create resources in parallel (Database, Cache, Queue)
	err = builder.AppendParallel(
		&compensate.ActionNodeKind[*InfraState, *InfraSaga]{
			NodeName: "create_database",
			Label:    "Create Database",
			Action:   createDatabaseAction(),
		},
		&compensate.ActionNodeKind[*InfraState, *InfraSaga]{
			NodeName: "create_cache",
			Label:    "Create Cache",
			Action:   createCacheAction(),
		},
		&compensate.ActionNodeKind[*InfraState, *InfraSaga]{
			NodeName: "create_queue",
			Label:    "Create Queue",
			Action:   createQueueAction(),
		},
	)
	if err != nil {
		log.Fatalf("Failed to add parallel actions: %v", err)
	}

	// Level 3: Create app server (depends on all resources)
	err = builder.Append(&compensate.ActionNodeKind[*InfraState, *InfraSaga]{
		NodeName: "create_app_server",
		Label:    "Create App Server",
		Action:   createAppServerAction(),
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
	sagaDag := compensate.NewSagaDag(dag, json.RawMessage(`{"region": "us-west-2"}`))

	// Create executor with in-memory store
	store := compensate.NewMemoryStore[*InfraState]()
	sagaID := fmt.Sprintf("infra-%d", time.Now().Unix())
	executor := compensate.NewSagaExecutor(sagaDag, registry, saga, sagaID, store)

	// Enable structured logging with slog
	executor.EnableDefaultLogging()

	// Set concurrency limit (optional - defaults to runtime.NumCPU())
	executor.SetMaxConcurrency(3)

	fmt.Println("Starting infrastructure provisioning...")
	fmt.Println("This will create resources in parallel to save time.")
	fmt.Println()

	// Time the execution
	start := time.Now()

	// Execute concurrently
	ctx := context.Background()
	err = executor.ExecuteConcurrent(ctx)

	duration := time.Since(start)

	if err != nil {
		fmt.Printf("\nProvisioning failed after %v: %v\n", duration, err)
		fmt.Println("Resources have been automatically cleaned up.")
		return
	}

	fmt.Printf("\nInfrastructure provisioned successfully in %v!\n", duration)
	fmt.Println("\nProvisioned resources:")
	fmt.Printf("- VPC ID: %s\n", state.VPCID)
	fmt.Printf("- Database: %s\n", state.DatabaseHost)
	fmt.Printf("- Cache: %s\n", state.CacheHost)
	fmt.Printf("- Queue: %s\n", state.QueueURL)
	fmt.Printf("- App Server: %s\n", state.AppServerIP)

	// Demonstrate manual rollback
	fmt.Println("\nPress Enter to tear down the infrastructure...")
	fmt.Scanln()

	fmt.Println("Starting infrastructure teardown...")
	rollbackStart := time.Now()
	
	err = executor.Rollback(ctx)
	rollbackDuration := time.Since(rollbackStart)
	
	if err != nil {
		fmt.Printf("\nRollback failed after %v: %v\n", rollbackDuration, err)
		return
	}

	fmt.Printf("\nInfrastructure torn down successfully in %v!\n", rollbackDuration)
}

// Infrastructure provisioning actions

func createVPCAction() *compensate.ActionFunc[*InfraState, *InfraSaga, *ResourceInfo] {
	return compensate.NewActionFunc[*InfraState, *InfraSaga, *ResourceInfo](
		"create_vpc_action",
		func(ctx context.Context, sgctx compensate.ActionContext[*InfraState, *InfraSaga]) (compensate.ActionResult[*ResourceInfo], error) {
			logger := slog.Default().With("action", "create_vpc")
			logger.Info("creating VPC")
			
			// Simulate API call
			time.Sleep(2 * time.Second)
			
			vpcID := fmt.Sprintf("vpc-%d", time.Now().UnixNano())
			sgctx.UserContext.VPCID = vpcID
			sgctx.UserContext.Resources["vpc"] = vpcID
			
			result := &ResourceInfo{
				ResourceID:   vpcID,
				ResourceType: "vpc",
				CreatedAt:    time.Now(),
			}
			
			logger.Info("VPC created", "vpc_id", vpcID)
			return compensate.ActionResult[*ResourceInfo]{Output: result}, nil
		},
		func(ctx context.Context, sgctx compensate.ActionContext[*InfraState, *InfraSaga]) error {
			logger := slog.Default().With("action", "delete_vpc")
			logger.Info("deleting VPC", "vpc_id", sgctx.UserContext.VPCID)
			
			// Simulate API call
			time.Sleep(1 * time.Second)
			
			delete(sgctx.UserContext.Resources, "vpc")
			sgctx.UserContext.VPCID = ""
			
			logger.Info("VPC deleted")
			return nil
		},
	)
}

func createDatabaseAction() *compensate.ActionFunc[*InfraState, *InfraSaga, *ResourceInfo] {
	return compensate.NewActionFunc[*InfraState, *InfraSaga, *ResourceInfo](
		"create_database_action",
		func(ctx context.Context, sgctx compensate.ActionContext[*InfraState, *InfraSaga]) (compensate.ActionResult[*ResourceInfo], error) {
			logger := slog.Default().With("action", "create_database")
			logger.Info("creating database instance")
			
			// Verify VPC exists
			vpcInfo, found := compensate.LookupTyped[*ResourceInfo](sgctx, "create_vpc")
			if !found || vpcInfo.ResourceID == "" {
				return compensate.ActionResult[*ResourceInfo]{}, fmt.Errorf("VPC must be created first")
			}
			
			// Simulate slow provisioning
			time.Sleep(5 * time.Second)
			
			dbHost := fmt.Sprintf("db-%d.region.rds.amazonaws.com", time.Now().UnixNano())
			sgctx.UserContext.DatabaseHost = dbHost
			sgctx.UserContext.Resources["database"] = dbHost
			
			result := &ResourceInfo{
				ResourceID:   dbHost,
				ResourceType: "database",
				Endpoint:     dbHost + ":5432",
				CreatedAt:    time.Now(),
			}
			
			logger.Info("database created", "endpoint", result.Endpoint)
			return compensate.ActionResult[*ResourceInfo]{Output: result}, nil
		},
		func(ctx context.Context, sgctx compensate.ActionContext[*InfraState, *InfraSaga]) error {
			logger := slog.Default().With("action", "delete_database")
			logger.Info("deleting database", "host", sgctx.UserContext.DatabaseHost)
			
			time.Sleep(2 * time.Second)
			
			delete(sgctx.UserContext.Resources, "database")
			sgctx.UserContext.DatabaseHost = ""
			
			logger.Info("database deleted")
			return nil
		},
	)
}

func createCacheAction() *compensate.ActionFunc[*InfraState, *InfraSaga, *ResourceInfo] {
	return compensate.NewActionFunc[*InfraState, *InfraSaga, *ResourceInfo](
		"create_cache_action",
		func(ctx context.Context, sgctx compensate.ActionContext[*InfraState, *InfraSaga]) (compensate.ActionResult[*ResourceInfo], error) {
			logger := slog.Default().With("action", "create_cache")
			logger.Info("creating cache cluster")
			
			// Verify VPC exists
			vpcInfo, found := compensate.LookupTyped[*ResourceInfo](sgctx, "create_vpc")
			if !found || vpcInfo.ResourceID == "" {
				return compensate.ActionResult[*ResourceInfo]{}, fmt.Errorf("VPC must be created first")
			}
			
			// Simulate slow provisioning
			time.Sleep(5 * time.Second)
			
			cacheHost := fmt.Sprintf("cache-%d.region.cache.amazonaws.com", time.Now().UnixNano())
			sgctx.UserContext.CacheHost = cacheHost
			sgctx.UserContext.Resources["cache"] = cacheHost
			
			result := &ResourceInfo{
				ResourceID:   cacheHost,
				ResourceType: "cache",
				Endpoint:     cacheHost + ":6379",
				CreatedAt:    time.Now(),
			}
			
			logger.Info("cache created", "endpoint", result.Endpoint)
			return compensate.ActionResult[*ResourceInfo]{Output: result}, nil
		},
		func(ctx context.Context, sgctx compensate.ActionContext[*InfraState, *InfraSaga]) error {
			logger := slog.Default().With("action", "delete_cache")
			logger.Info("deleting cache", "host", sgctx.UserContext.CacheHost)
			
			time.Sleep(2 * time.Second)
			
			delete(sgctx.UserContext.Resources, "cache")
			sgctx.UserContext.CacheHost = ""
			
			logger.Info("cache deleted")
			return nil
		},
	)
}

func createQueueAction() *compensate.ActionFunc[*InfraState, *InfraSaga, *ResourceInfo] {
	return compensate.NewActionFunc[*InfraState, *InfraSaga, *ResourceInfo](
		"create_queue_action",
		func(ctx context.Context, sgctx compensate.ActionContext[*InfraState, *InfraSaga]) (compensate.ActionResult[*ResourceInfo], error) {
			logger := slog.Default().With("action", "create_queue")
			logger.Info("creating message queue")
			
			// Verify VPC exists
			vpcInfo, found := compensate.LookupTyped[*ResourceInfo](sgctx, "create_vpc")
			if !found || vpcInfo.ResourceID == "" {
				return compensate.ActionResult[*ResourceInfo]{}, fmt.Errorf("VPC must be created first")
			}
			
			// Simulate slow provisioning
			time.Sleep(5 * time.Second)
			
			queueURL := fmt.Sprintf("https://sqs.region.amazonaws.com/123456789/queue-%d", time.Now().UnixNano())
			sgctx.UserContext.QueueURL = queueURL
			sgctx.UserContext.Resources["queue"] = queueURL
			
			result := &ResourceInfo{
				ResourceID:   queueURL,
				ResourceType: "queue",
				Endpoint:     queueURL,
				CreatedAt:    time.Now(),
			}
			
			logger.Info("queue created", "url", queueURL)
			return compensate.ActionResult[*ResourceInfo]{Output: result}, nil
		},
		func(ctx context.Context, sgctx compensate.ActionContext[*InfraState, *InfraSaga]) error {
			logger := slog.Default().With("action", "delete_queue")
			logger.Info("deleting queue", "url", sgctx.UserContext.QueueURL)
			
			time.Sleep(1 * time.Second)
			
			delete(sgctx.UserContext.Resources, "queue")
			sgctx.UserContext.QueueURL = ""
			
			logger.Info("queue deleted")
			return nil
		},
	)
}

func createAppServerAction() *compensate.ActionFunc[*InfraState, *InfraSaga, *ResourceInfo] {
	return compensate.NewActionFunc[*InfraState, *InfraSaga, *ResourceInfo](
		"create_app_server_action",
		func(ctx context.Context, sgctx compensate.ActionContext[*InfraState, *InfraSaga]) (compensate.ActionResult[*ResourceInfo], error) {
			logger := slog.Default().With("action", "create_app_server")
			logger.Info("creating app server")
			
			// Verify all dependencies exist
			dbInfo, foundDB := compensate.LookupTyped[*ResourceInfo](sgctx, "create_database")
			cacheInfo, foundCache := compensate.LookupTyped[*ResourceInfo](sgctx, "create_cache")
			queueInfo, foundQueue := compensate.LookupTyped[*ResourceInfo](sgctx, "create_queue")
			
			if !foundDB || !foundCache || !foundQueue {
				return compensate.ActionResult[*ResourceInfo]{}, fmt.Errorf("all resources (DB, Cache, Queue) must be created first")
			}
			
			logger.Info("configuring app server with resources",
				"database", dbInfo.Endpoint,
				"cache", cacheInfo.Endpoint,
				"queue", queueInfo.Endpoint)
			
			// Simulate server provisioning
			time.Sleep(3 * time.Second)
			
			serverIP := fmt.Sprintf("10.0.1.%d", time.Now().UnixNano()%255)
			sgctx.UserContext.AppServerIP = serverIP
			sgctx.UserContext.Resources["app_server"] = serverIP
			
			result := &ResourceInfo{
				ResourceID:   serverIP,
				ResourceType: "app_server",
				Endpoint:     serverIP + ":8080",
				CreatedAt:    time.Now(),
			}
			
			logger.Info("app server created", "ip", serverIP)
			return compensate.ActionResult[*ResourceInfo]{Output: result}, nil
		},
		func(ctx context.Context, sgctx compensate.ActionContext[*InfraState, *InfraSaga]) error {
			logger := slog.Default().With("action", "terminate_app_server")
			logger.Info("terminating app server", "ip", sgctx.UserContext.AppServerIP)
			
			time.Sleep(1 * time.Second)
			
			delete(sgctx.UserContext.Resources, "app_server")
			sgctx.UserContext.AppServerIP = ""
			
			logger.Info("app server terminated")
			return nil
		},
	)
}

func registerInfraActions(registry *compensate.ActionRegistry[*InfraState, *InfraSaga]) {
	actions := []compensate.Action[*InfraState, *InfraSaga]{
		createVPCAction(),
		createDatabaseAction(),
		createCacheAction(),
		createQueueAction(),
		createAppServerAction(),
	}
	
	for _, action := range actions {
		if err := registry.Register(action); err != nil {
			log.Fatalf("Failed to register action: %v", err)
		}
	}
}