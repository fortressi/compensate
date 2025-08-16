# Concurrent Execution Plan for SagaExecutor

## Overview

The Compensate library currently executes saga actions sequentially, even when the DAG structure indicates that certain actions could run in parallel. This document outlines a plan to add concurrent execution capabilities to the `SagaExecutor` while maintaining backward compatibility and ensuring correct error handling and compensation.

## Current State Analysis

### Existing Infrastructure

1. **DAG Builder Support**: The `DagBuilder` already supports parallel nodes through `AppendParallel()`:
   ```go
   err = builder.AppendParallel(
       &ActionNodeKind[*State, *Saga]{NodeName: "taskA", Action: actionA},
       &ActionNodeKind[*State, *Saga]{NodeName: "taskB", Action: actionB},
   )
   ```

2. **Execution Levels**: The `getExecutionLevels()` method correctly groups nodes by dependency levels:
   ```go
   // Returns: [[start], [setup], [taskA, taskB], [cleanup], [end]]
   levels, err := executor.getExecutionLevels()
   ```

3. **Sequential Execution**: Current `Execute()` uses topological sort for sequential execution:
   ```go
   func (e *SagaExecutor[T, S]) Execute(ctx context.Context) error {
       executionOrder, err := e.getTopologicalOrder()
       for _, nodeIndex := range executionOrder {
           if err := e.executeNode(ctx, nodeIndex); err != nil {
               // Handle failure and compensation
           }
       }
   }
   ```

## Design Decisions

### 1. Backward Compatibility
- Keep existing `Execute()` method for sequential execution
- Add new `ExecuteConcurrent()` method for parallel execution
- Allow users to choose execution mode based on their needs

### 2. Concurrency Control
- Implement configurable concurrency limits (default: runtime.NumCPU())
- Use worker pool pattern with semaphore for resource management
- Ensure graceful degradation under resource constraints

### 3. Error Handling Strategy
- If any node in a level fails, cancel other running nodes in that level
- Collect all errors from concurrent executions
- Maintain deterministic compensation order (reverse sequential)

### 4. State Management
- Use mutex for thread-safe access to shared state
- Ensure atomic updates to execution tracking structures
- Maintain consistency of timing information

## Implementation Plan

### Phase 1: Core Infrastructure

#### 1.1 Update SagaExecutor Structure

```go
// SagaExecutor handles the execution of saga actions
type SagaExecutor[T any, S SagaType[T]] struct {
    // ... existing fields ...
    
    // Concurrency control
    maxConcurrency int
    mu             sync.Mutex  // Protects shared state
}

// NewSagaExecutor creates a new saga executor
func NewSagaExecutor[T any, S SagaType[T]](
    dag *SagaDag,
    actionRegistry *ActionRegistry[T, S],
    sagaContext S,
    sagaID string,
    store Store[T],
) *SagaExecutor[T, S] {
    executor := &SagaExecutor[T, S]{
        // ... existing initialization ...
        maxConcurrency: runtime.NumCPU(), // Default concurrency
    }
    // ... rest of initialization ...
    return executor
}

// SetMaxConcurrency allows configuration of concurrency limit
func (e *SagaExecutor[T, S]) SetMaxConcurrency(max int) {
    if max < 1 {
        max = 1
    }
    e.maxConcurrency = max
}
```

#### 1.2 Thread-Safe State Updates

```go
// addCompleted safely adds a node to the completed list
func (e *SagaExecutor[T, S]) addCompleted(nodeIndex int64) {
    e.mu.Lock()
    defer e.mu.Unlock()
    e.completed = append(e.completed, nodeIndex)
}

// addFailed safely adds a node to the failed list
func (e *SagaExecutor[T, S]) addFailed(nodeIndex int64) {
    e.mu.Lock()
    defer e.mu.Unlock()
    e.failed = append(e.failed, nodeIndex)
}

// updateAncestorTree safely updates the ancestor tree
func (e *SagaExecutor[T, S]) updateAncestorTree(nodeName NodeName, value any) {
    e.mu.Lock()
    defer e.mu.Unlock()
    e.ancestorTree.Set(nodeName, value)
}

// addExecutionRecord safely adds an execution record
func (e *SagaExecutor[T, S]) addExecutionRecord(record ExecutionRecord) {
    e.mu.Lock()
    defer e.mu.Unlock()
    e.executionTrace = append(e.executionTrace, record)
}
```

### Phase 2: Concurrent Execution Implementation

#### 2.1 Main Concurrent Execution Method

```go
// ExecuteConcurrent runs the saga with concurrent execution of parallel nodes
func (e *SagaExecutor[T, S]) ExecuteConcurrent(ctx context.Context) error {
    // Save initial state
    if err := e.persistState(ctx, SagaStatusRunning); err != nil {
        return fmt.Errorf("failed to save initial state: %w", err)
    }
    
    // Get execution levels
    levels, err := e.getExecutionLevels()
    if err != nil {
        return fmt.Errorf("failed to get execution levels: %w", err)
    }
    
    // Execute each level
    for levelIndex, level := range levels {
        if err := e.executeLevel(ctx, level); err != nil {
            // Persist failure state
            if persistErr := e.persistState(ctx, SagaStatusFailed); persistErr != nil {
                fmt.Printf("Warning: failed to persist failure state: %v\n", persistErr)
            }
            
            // Trigger compensation
            if compensationErr := e.compensate(ctx); compensationErr != nil {
                return fmt.Errorf("level %d failed and compensation failed: execution_error=%w, compensation_error=%v", 
                    levelIndex, err, compensationErr)
            }
            return fmt.Errorf("saga failed at level %d: %w", levelIndex, err)
        }
        
        // Persist state after each level
        if persistErr := e.persistState(ctx, SagaStatusRunning); persistErr != nil {
            fmt.Printf("Warning: failed to persist execution state: %v\n", persistErr)
        }
    }
    
    // Mark saga as completed
    if err := e.persistState(ctx, SagaStatusCompleted); err != nil {
        fmt.Printf("Warning: failed to persist completion state: %v\n", err)
    }
    
    return nil
}
```

#### 2.2 Level Execution with Concurrency Control

```go
// executeLevel executes all nodes in a level concurrently
func (e *SagaExecutor[T, S]) executeLevel(ctx context.Context, nodeIndices []int64) error {
    if len(nodeIndices) == 0 {
        return nil
    }
    
    // Create channels for coordination
    type nodeResult struct {
        nodeIndex int64
        err       error
    }
    
    resultCh := make(chan nodeResult, len(nodeIndices))
    
    // Create context with cancellation for this level
    levelCtx, cancel := context.WithCancel(ctx)
    defer cancel()
    
    // Semaphore for concurrency control
    sem := make(chan struct{}, e.maxConcurrency)
    
    // Launch goroutines for each node
    var wg sync.WaitGroup
    for _, nodeIndex := range nodeIndices {
        wg.Add(1)
        go func(idx int64) {
            defer wg.Done()
            
            // Acquire semaphore
            select {
            case sem <- struct{}{}:
                defer func() { <-sem }()
            case <-levelCtx.Done():
                resultCh <- nodeResult{idx, levelCtx.Err()}
                return
            }
            
            // Execute the node
            err := e.executeNode(levelCtx, idx)
            resultCh <- nodeResult{idx, err}
            
            // If node failed, cancel other nodes in this level
            if err != nil {
                cancel()
            }
        }(nodeIndex)
    }
    
    // Wait for all goroutines to complete
    go func() {
        wg.Wait()
        close(resultCh)
    }()
    
    // Collect results
    var errors []error
    failedNodes := make([]int64, 0)
    
    for result := range resultCh {
        if result.err != nil {
            errors = append(errors, fmt.Errorf("node %d failed: %w", result.nodeIndex, result.err))
            failedNodes = append(failedNodes, result.nodeIndex)
            e.addFailed(result.nodeIndex)
        } else {
            e.addCompleted(result.nodeIndex)
        }
    }
    
    // Return aggregated error if any nodes failed
    if len(errors) > 0 {
        return fmt.Errorf("level execution failed with %d errors: %v", len(errors), errors)
    }
    
    return nil
}
```

#### 2.3 Updated executeNode for Thread Safety

```go
// executeNode executes a single node (thread-safe version)
func (e *SagaExecutor[T, S]) executeNode(ctx context.Context, nodeIndex int64) error {
    execNode := e.nodes[nodeIndex]
    internalNode := e.dag.Nodes[nodeIndex]
    
    // Update state to running
    execNode.State = ActionStateRunning
    
    // Only handle ActionNodeInternal for now
    actionNode, ok := internalNode.(*ActionNodeInternal)
    if !ok {
        // Skip non-action nodes (like StartNode, EndNode)
        execNode.State = ActionStateCompleted
        return nil
    }
    
    // Get action from registry
    action, err := e.actionRegistry.Get(actionNode.ActionName)
    if err != nil {
        execNode.State = ActionStateFailed
        execNode.Error = err
        return fmt.Errorf("action not found: %s", actionNode.ActionName)
    }
    
    // Record start of execution
    startTime := time.Now()
    
    // Create action context with a snapshot of ancestor tree
    // This ensures consistent view during concurrent execution
    e.mu.Lock()
    ancestorSnapshot := e.createAncestorSnapshot()
    e.mu.Unlock()
    
    actionCtx := ActionContext[T, S]{
        AncestorTree: ancestorSnapshot,
        NodeID:       int(nodeIndex),
        DAG:          e.dag,
        UserContext:  e.sagaContext.ExecContext(),
    }
    
    // Execute the action
    result, err := action.DoIt(ctx, actionCtx)
    endTime := time.Now()
    
    // SEC always sets the timing
    result.StartTime = startTime
    result.EndTime = endTime
    
    // Determine final status and handle result
    var finalStatus ActionState
    if err != nil {
        execNode.State = ActionStateFailed
        execNode.Error = err
        finalStatus = ActionStateFailed
    } else {
        execNode.State = ActionStateCompleted
        execNode.Result = &result
        finalStatus = ActionStateCompleted
        
        // Add output to ancestor tree for dependent actions
        if execNode.NodeName != "" {
            e.updateAncestorTree(execNode.NodeName, result.Output)
        }
    }
    
    // Record execution in trace
    record := ExecutionRecord{
        ActionName: string(actionNode.ActionName),
        NodeID:     nodeIndex,
        StartTime:  startTime,
        EndTime:    endTime,
        Status:     finalStatus,
        Error:      err,
    }
    e.addExecutionRecord(record)
    
    if err != nil {
        return fmt.Errorf("action %s failed: %w", actionNode.ActionName, err)
    }
    
    return nil
}

// createAncestorSnapshot creates a read-only snapshot of the ancestor tree
func (e *SagaExecutor[T, S]) createAncestorSnapshot() *btree.Map[NodeName, any] {
    snapshot := btree.NewMap[NodeName, any](10)
    e.ancestorTree.Scan(func(key NodeName, value any) bool {
        snapshot.Set(key, value)
        return true
    })
    return snapshot
}
```

### Phase 3: Testing Strategy

#### 3.1 Concurrent Execution Timing Test

```go
func TestConcurrentExecutionTiming(t *testing.T) {
    // Create a DAG with parallel slow actions
    // setup -> [slowA (500ms), slowB (500ms)] -> cleanup
    
    registry := NewActionRegistry[*TestState, *TestSaga]()
    
    // Create slow actions that sleep
    slowActionA := NewActionFunc[*TestState, *TestSaga, string](
        "slow_a",
        func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) (ActionResult[string], error) {
            time.Sleep(500 * time.Millisecond)
            return NewActionResult("slowA done"), nil
        },
        func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) error { return nil },
    )
    
    slowActionB := NewActionFunc[*TestState, *TestSaga, string](
        "slow_b",
        func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) (ActionResult[string], error) {
            time.Sleep(500 * time.Millisecond)
            return NewActionResult("slowB done"), nil
        },
        func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) error { return nil },
    )
    
    // ... register actions and build DAG ...
    
    executor := NewSagaExecutor(sagaDag, registry, saga, "test-concurrent", store)
    
    // Time sequential execution
    startSeq := time.Now()
    err := executor.Execute(context.Background())
    sequentialDuration := time.Since(startSeq)
    require.NoError(t, err)
    
    // Reset executor
    executor = NewSagaExecutor(sagaDag, registry, saga, "test-concurrent-2", store)
    
    // Time concurrent execution
    startConc := time.Now()
    err = executor.ExecuteConcurrent(context.Background())
    concurrentDuration := time.Since(startConc)
    require.NoError(t, err)
    
    // Concurrent should be significantly faster (close to 500ms vs 1000ms)
    assert.True(t, concurrentDuration < sequentialDuration*0.7,
        "concurrent execution (%.0fms) should be faster than sequential (%.0fms)",
        concurrentDuration.Milliseconds(), sequentialDuration.Milliseconds())
}
```

#### 3.2 Concurrent Failure Handling Test

```go
func TestConcurrentExecutionWithFailure(t *testing.T) {
    // Test that when one action in a level fails, others are cancelled
    // and compensation runs correctly
    
    var actionACancelled, actionBFailed atomic.Bool
    
    actionA := NewActionFunc[*TestState, *TestSaga, string](
        "action_a",
        func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) (ActionResult[string], error) {
            // Simulate work that can be cancelled
            select {
            case <-time.After(1 * time.Second):
                return NewActionResult("completed"), nil
            case <-ctx.Done():
                actionACancelled.Store(true)
                return ActionResult[string]{}, ctx.Err()
            }
        },
        func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) error { 
            // Verify undo is called
            return nil 
        },
    )
    
    actionB := NewActionFunc[*TestState, *TestSaga, string](
        "action_b",
        func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) (ActionResult[string], error) {
            // Fail quickly
            time.Sleep(100 * time.Millisecond)
            actionBFailed.Store(true)
            return ActionResult[string]{}, fmt.Errorf("actionB failed")
        },
        func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) error { return nil },
    )
    
    // ... build DAG with parallel actions ...
    
    err := executor.ExecuteConcurrent(context.Background())
    require.Error(t, err)
    
    // Verify actionA was cancelled and actionB failed
    assert.True(t, actionACancelled.Load(), "actionA should have been cancelled")
    assert.True(t, actionBFailed.Load(), "actionB should have failed")
    
    // Verify compensation occurred
    // ... check that setup action was undone ...
}
```

#### 3.3 Concurrency Limit Test

```go
func TestConcurrencyLimit(t *testing.T) {
    // Test that concurrency limit is respected
    const maxConcurrency = 2
    var currentlyRunning atomic.Int32
    var maxObserved int32
    
    // Create many parallel actions
    actions := make([]Action[*TestState, *TestSaga], 10)
    for i := 0; i < 10; i++ {
        actionName := fmt.Sprintf("action_%d", i)
        actions[i] = NewActionFunc[*TestState, *TestSaga, string](
            ActionName(actionName),
            func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) (ActionResult[string], error) {
                // Track concurrent executions
                current := currentlyRunning.Add(1)
                defer currentlyRunning.Add(-1)
                
                // Update max observed
                for {
                    max := atomic.LoadInt32(&maxObserved)
                    if current <= max || atomic.CompareAndSwapInt32(&maxObserved, max, current) {
                        break
                    }
                }
                
                // Simulate work
                time.Sleep(100 * time.Millisecond)
                return NewActionResult("done"), nil
            },
            func(ctx context.Context, sgctx ActionContext[*TestState, *TestSaga]) error { return nil },
        )
    }
    
    // ... build DAG with all actions in parallel ...
    
    executor := NewSagaExecutor(sagaDag, registry, saga, "test-limit", store)
    executor.SetMaxConcurrency(maxConcurrency)
    
    err := executor.ExecuteConcurrent(context.Background())
    require.NoError(t, err)
    
    // Verify concurrency was limited
    assert.LessOrEqual(t, maxObserved, int32(maxConcurrency),
        "should not exceed max concurrency of %d", maxConcurrency)
}
```

### Phase 4: Integration and Documentation

#### 4.1 Update Examples

Create a new example demonstrating concurrent execution benefits:

```go
// examples/concurrent-provisioning/main.go
package main

import (
    "context"
    "fmt"
    "time"
    "github.com/fortressi/compensate"
)

func main() {
    // Example: Provision cloud infrastructure concurrently
    // VPC -> [Database, Cache, Queue] -> App Server
    
    builder := compensate.NewDagBuilder[*InfraState, *InfraSaga]("CloudProvisioning", registry)
    
    // Level 1: Create VPC
    builder.Append(&compensate.ActionNodeKind[*InfraState, *InfraSaga]{
        NodeName: "create_vpc",
        Action:   createVPCAction,
    })
    
    // Level 2: Create resources in parallel (each takes 30s)
    builder.AppendParallel(
        &compensate.ActionNodeKind[*InfraState, *InfraSaga]{
            NodeName: "create_database",
            Action:   createDatabaseAction,
        },
        &compensate.ActionNodeKind[*InfraState, *InfraSaga]{
            NodeName: "create_cache", 
            Action:   createCacheAction,
        },
        &compensate.ActionNodeKind[*InfraState, *InfraSaga]{
            NodeName: "create_queue",
            Action:   createQueueAction,
        },
    )
    
    // Level 3: Create app server (depends on all resources)
    builder.Append(&compensate.ActionNodeKind[*InfraState, *InfraSaga]{
        NodeName: "create_app_server",
        Action:   createAppServerAction,
    })
    
    // Execute concurrently
    start := time.Now()
    err := executor.ExecuteConcurrent(context.Background())
    if err != nil {
        fmt.Printf("Provisioning failed: %v\n", err)
        return
    }
    
    fmt.Printf("Infrastructure provisioned in %v\n", time.Since(start))
    // Output: Infrastructure provisioned in ~60s (vs ~120s sequential)
}
```

#### 4.2 Performance Benchmarks

```go
func BenchmarkSequentialVsConcurrent(b *testing.B) {
    // Benchmark with different DAG patterns
    patterns := []struct {
        name        string
        buildDAG    func() *SagaDag
        parallelism int
    }{
        {"Linear", buildLinearDAG, 1},
        {"ForkJoin", buildForkJoinDAG, 2},
        {"WideFan", buildWideFanDAG, 10},
        {"Complex", buildComplexDAG, 5},
    }
    
    for _, pattern := range patterns {
        b.Run(pattern.name+"_Sequential", func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                executor := NewSagaExecutor(pattern.buildDAG(), ...)
                executor.Execute(context.Background())
            }
        })
        
        b.Run(pattern.name+"_Concurrent", func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                executor := NewSagaExecutor(pattern.buildDAG(), ...)
                executor.ExecuteConcurrent(context.Background())
            }
        })
    }
}
```

## Migration Guide

### For Users

1. **No changes required**: Existing code using `Execute()` continues to work
2. **Opt-in to concurrency**: Replace `Execute()` with `ExecuteConcurrent()`:
   ```go
   // Before
   err := executor.Execute(ctx)
   
   // After - with concurrent execution
   err := executor.ExecuteConcurrent(ctx)
   ```
3. **Configure concurrency**: Set max concurrency if needed:
   ```go
   executor.SetMaxConcurrency(10) // Limit to 10 concurrent actions
   ```

### Best Practices

1. **Action Design**: Ensure actions are thread-safe and idempotent
2. **Resource Management**: Set appropriate concurrency limits based on resources
3. **Error Handling**: Design actions to handle context cancellation gracefully
4. **Testing**: Test both sequential and concurrent execution paths

## Implementation Timeline

1. **Week 1**: Core infrastructure (executor updates, thread safety)
2. **Week 2**: Concurrent execution implementation
3. **Week 3**: Comprehensive testing and benchmarks
4. **Week 4**: Documentation, examples, and performance tuning

## Open Questions

1. **Default Behavior**: Should we make concurrent execution the default in a future major version?
2. **Configuration**: Should concurrency limit be configurable per-level or globally?
3. **Metrics**: Should we add execution metrics (parallelism achieved, time saved)?
4. **Debugging**: What additional logging/tracing would help debug concurrent execution?

## Conclusion

This plan adds powerful concurrent execution capabilities to the Compensate library while maintaining backward compatibility and safety. The implementation focuses on correctness, performance, and ease of use, making it simple for users to speed up their saga executions when the DAG structure allows for parallelism.