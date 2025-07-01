# Compensate

A Go library implementing the Distributed Saga pattern for managing complex, multi-step operations with automatic rollback capabilities. Inspired by oxidecomputer/steno, which was in turn inspired by [Caitie McCaffrey's saga pattern presentation](https://www.youtube.com/watch?v=xDuwrtwYHu8).

## Features

- **Type-safe generics**: Full Go 1.18+ generics support for compile-time safety
- **DAG-based workflow**: Actions organized in a Directed Acyclic Graph for optimal execution
- **Automatic rollback**: Failed actions trigger automatic compensation of completed steps
- **Parallel execution**: Independent actions can run concurrently
- **State persistence**: Save and restore saga state with pluggable storage backends
- **Action results**: Actions can produce typed outputs accessible by dependent actions
- **Visualization**: Export saga DAGs as Graphviz DOT files

## Installation

```bash
go get github.com/fortressi/compensate
```

## Quick Start

Here's a simple "Hello World" saga that demonstrates the basic concepts:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/fortressi/compensate"
)

// Define your state type
type HelloState struct {
    Message string
}

// Define your saga type that implements SagaType interface
type HelloSaga struct {
    State *HelloState
}

func (s *HelloSaga) ExecContext() *HelloState {
    return s.State
}

// Define a simple result type
type MessageResult struct {
    Output string
}

func main() {
    // Create an action with do and undo functions
    helloAction := compensate.NewActionFunc[*HelloState, *HelloSaga, *MessageResult](
        "hello_action",
        // Do function
        func(ctx context.Context, sagaCtx *HelloSaga) (compensate.ActionFuncResult[*MessageResult], error) {
            fmt.Println("Executing: Hello, World!")
            sagaCtx.State.Message = "Hello executed"
            
            return compensate.ActionFuncResult[*MessageResult]{
                Result: &MessageResult{Output: "Hello, World!"},
            }, nil
        },
        // Undo function
        func(ctx context.Context, sagaCtx *HelloSaga) error {
            fmt.Println("Rolling back: Goodbye, World!")
            sagaCtx.State.Message = "Hello rolled back"
            return nil
        },
    )

    // Create registry and builder
    registry := compensate.NewActionRegistry[*HelloState, *HelloSaga]()
    registry.RegisterAction(helloAction)
    
    builder := compensate.NewDagBuilder[*HelloState, *HelloSaga]("HelloSaga", registry)
    
    // Add the action to the saga
    builder.Append(&compensate.ActionNodeKind[*HelloState, *HelloSaga]{
        NodeName: "say_hello",
        Label:    "Say Hello",
        Action:   helloAction,
    })
    
    // Build the DAG
    dag, err := builder.Build()
    if err != nil {
        log.Fatal(err)
    }
    
    // Create saga context and executor
    sagaContext := &HelloSaga{State: &HelloState{}}
    sagaID := "hello-saga-001"
    store := compensate.NewMemoryStore[*HelloState]()
    
    sagaDag := compensate.NewSagaDag(dag, []byte(`{}`))
    executor := compensate.NewSagaExecutor(sagaDag, registry, sagaContext, sagaID, store)
    
    // Execute the saga
    ctx := context.Background()
    if err := executor.Execute(ctx); err != nil {
        log.Printf("Saga failed: %v", err)
        // Rollback will happen automatically
    }
    
    fmt.Printf("Final state: %s\n", sagaContext.State.Message)
}
```

## Core Concepts

### Actions

Actions are the building blocks of sagas. Each action has:
- A **Do** function that performs the operation
- An **Undo** function that compensates/reverses the operation
- Optional typed result that can be passed to dependent actions

### Saga DAG

Actions are organized in a Directed Acyclic Graph (DAG) where:
- Nodes represent actions
- Edges represent dependencies
- Actions with no dependencies can execute in parallel

### State Management

Sagas maintain two types of state:
- **Saga State**: Shared context accessible to all actions
- **Action State**: Individual results from each action execution

## Examples

The repository includes several comprehensive examples:

### 1. Manual Rollback Example
Located in `examples/manual_rollback/`, demonstrates:
- Basic saga construction
- Manual rollback triggering
- Simple resource creation/deletion pattern

### 2. AWS Infrastructure Example
Located in `examples/aws-infrastructure/`, demonstrates:
- Complex multi-step AWS VPC provisioning
- CLI interface with deploy/destroy commands
- State persistence between runs
- Parallel action execution

```bash
# Deploy infrastructure
cd examples/aws-infrastructure
go run . deploy --saga-id my-vpc-123

# Destroy using persisted state
go run . destroy --saga-id my-vpc-123
```

### 3. Persistent CLI Example
Located in `examples/persistent_cli/`, demonstrates:
- File-based saga state persistence
- Recovery from interruptions
- Resource provisioning with cleanup

## Advanced Usage

### Building Complex Sagas

```go
// Sequential actions
builder.Append(action1).Append(action2).Append(action3)

// Parallel actions
builder.AppendParallel(
    &compensate.ActionNodeKind[*State, *Saga]{
        NodeName: "parallel1",
        Action:   parallelAction1,
    },
    &compensate.ActionNodeKind[*State, *Saga]{
        NodeName: "parallel2", 
        Action:   parallelAction2,
    },
)

// Add dependencies manually
builder.AddNode("node1", "Node 1", action1)
builder.AddNode("node2", "Node 2", action2)
builder.AddDependency("node1", "node2") // node2 depends on node1
```

### Accessing Action Results

```go
// In a dependent action's Do function
func doDependent(ctx context.Context, sagaCtx *MySaga) (ActionFuncResult[*Result], error) {
    // Get result from a previous action
    prevResult, ok := compensate.GetActionResult[*PreviousResult](sagaCtx, "previous_action_name")
    if !ok {
        return ActionFuncResult[*Result]{}, fmt.Errorf("previous result not found")
    }
    
    // Use the previous result
    fmt.Printf("Previous output: %s\n", prevResult.Output)
    
    return ActionFuncResult[*Result]{
        Result: &Result{Data: processData(prevResult)},
    }, nil
}
```

### Persistence

```go
// File-based persistence
store := compensate.NewFileStore[*MyState]("/path/to/saga/states")

// Memory persistence (for testing)
store := compensate.NewMemoryStore[*MyState]()

// Create executor with persistence
executor := compensate.NewSagaExecutor(sagaDag, registry, sagaContext, sagaID, store)
```

### Visualization

Export your saga DAG as a Graphviz DOT file:

```go
dag, _ := builder.Build()
dot := dag.ToDot()
// Write to file and visualize with graphviz
```

## Architecture

The library follows a modular architecture:

- **Core Types**: Generic interfaces for actions, sagas, and state management
- **DAG Builder**: Fluent API for constructing saga workflows
- **Executor**: Manages saga execution with automatic rollback on failure
- **Registry**: Type-safe action registration and retrieval
- **Storage**: Pluggable persistence layer with file and memory implementations
- **Event Log**: Comprehensive execution history tracking

## Current Status

✅ **Implemented**:
- Sequential saga execution
- Automatic rollback on failure  
- State persistence and recovery
- Type-safe action composition
- Comprehensive examples

🚧 **In Progress**:
- Concurrent action execution
- Additional storage backends
- Production monitoring tools

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Acknowledgments

- Inspired by [Caitie McCaffrey's](https://caitiem.com/) work on distributed sagas
- Built with Go's powerful generics system for type safety
