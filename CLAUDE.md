# CLAUDE.md

We are coworkers. When you think of me think of be as your colleague, rather than the user or the human. We're a  team
of people working together, your success is my success and my success is yours. Technically I'm your boss but we're not
really formal around here. I'm smart but not infallible, you're much better read than i am. I have more experience of
the physical world than you do. Our experiences are complimentary and we work together to solve problems.

We prefer commit messages to be more informal but accurate. We try to describe what we've implemented and what we're
thinking for the next steps, calling what test coverage we have and may be missing. For commit messages we prefer full
informal sentences (almost like an inner monologue) that gives the reader a sense of what we've been thinking. We don't
mind if they're longer, and we also don't mind bulleted lists (sparingly) or code snippets. In the case of bugs we aim
to convey what the bug was accurately, and why we've chosen to fix it in the way we have (and any other relevant options
that were discounted).

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Structure

Plans and design documents should be stored in `docs/plans/` with descriptive names (e.g., `saga-state-persistence.md`, `rollback-improvements.md`).

## Project Overview

Compensate is a Go library implementing the Distributed Saga pattern for managing complex, multi-step operations with automatic rollback capabilities. The project is inspired by Caitie McCaffrey's saga pattern presentation.

## Development Commands

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run specific test
go test -v ./... -run TestName

# Build the project
go build ./...

# Check for compilation errors
go vet ./...

# Update dependencies
go mod tidy

# Format code
go fmt ./...
```

## Architecture Overview

### Core Concepts

1. **Saga**: A distributed transaction composed of multiple actions arranged in a DAG
2. **Action**: An operation with both "do" and "undo" (compensate) functions
3. **SEC (Saga Execution Coordinator)**: Manages saga execution and state

### Key Components

- **Action System** (`saga_action_generic.go`): Generic interface `Action[T, S]` with `DoIt()` and `UndoIt()` methods
- **Action Registry** (`action_registry.go`): Global registry for action types using `RegisterAction()` and `GetAction()`
- **DAG Builder** (`dag_builder.go`): Fluent API for constructing saga DAGs with `AddNode()` and `AddDependency()`
- **Storage** (`store.go`): `SecStore` interface for persisting saga state and events
- **Event Logging** (`saga_log.go`): `SagaLog` tracks execution history with events like `NodeStarted`, `NodeCompleted`, `NodeFailed`

### Usage Pattern

1. Define action functions (do/undo pairs)
2. Create actions using `NewActionFunc[T, S](name, doFunc, undoFunc)`
3. Register actions: `ActionRegistry.RegisterAction(action)`
4. Build saga DAG using `DagBuilder`
5. Execute saga through SEC (Saga Execution Coordinator)

### Current Implementation Status

- ✅ Core types and interfaces (Action, SagaDag, SecStore)
- ✅ Action registry with type-safe generics
- ✅ DAG construction and visualization (DOT export)
- ✅ Event logging system
- ✅ In-memory storage implementation
- ✅ Manual rollback functionality via `SagaExecutor.Rollback()`
- 🚧 SEC (Saga Execution Coordinator) - partial implementation with TODOs
- ❌ Persistent storage backends
- ❌ Comprehensive test coverage

### Important Notes

- The SEC implementation in `sec.go` is incomplete with multiple TODO markers
- Test coverage is minimal - only basic DAG construction tests exist
- No example applications demonstrating full saga execution
- Generic type parameters: `T` for action state, `S` for saga state throughout the codebase

# important-instruction-reminders
Do what has been asked; nothing more, nothing less.
NEVER create files unless they're absolutely necessary for achieving your goal.
ALWAYS prefer editing an existing file to creating a new one.
NEVER proactively create documentation files (*.md) or README files. Only create documentation files if explicitly requested by the User.

      
      IMPORTANT: this context may or may not be relevant to your tasks. You should not respond to this context or otherwise consider it in your response unless it is highly relevant to your task. Most of the time, it is not relevant.
