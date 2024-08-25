package compensate

// Package sec provides an implementation of distributed sagas in Go.
//
// Sagas orchestrate the execution of a set of asynchronous tasks that can fail.
// The saga pattern provides useful semantics for unwinding the whole operation
// when any task fails.  For more on distributed sagas, see this 2017 JOTB talk
// by Caitie McCaffrey: https://www.youtube.com/watch?v=0UTOLRTwOX0
//
// Overview
//
// 1. Define your saga actions as functions:
//    - Create "do" and "undo" functions for each action in your saga.
//    - Use `NewAction` to package these functions into an `ActionFunc`.
// 2. Create an `ActionRegistry`:
//    - Use `NewActionRegistry` to create an `ActionRegistry`.
//    - Register your `ActionFunc`s in the `ActionRegistry` using `RegisterAction`.
// 3. Construct a Saga Directed Acyclic Graph (DAG):
//    - Define the structure of your saga using a `SagaDag`.
//    - Each node in the `SagaDag` represents an action.
// 4. Run your saga:
//    - Create a `SecStore` implementation. You can use `NewInMemorySecStore` for
//      testing or implement your own for persistent storage.
//    - Create a `Sec` instance using `NewSec`, passing in your logger and `SecStore`.
//    - Use the `SecClient` interface to create (`CreateSaga`), start (`StartSaga`), and
//      manage your sagas.
//
// Example:
//
// For a detailed, documented example, refer to the `example` package. // Replace with actual path if different.
