// Package event provides EventHub, a publish/subscribe event bus with:
//   - Synchronous and asynchronous subscriptions
//   - Batch subscriptions with size/time-based flushing
//   - Middleware chain for cross-cutting concerns
//   - Per-event-type metrics (processed/dropped counts)
//   - Optional WorkerPool for async dispatch
//   - Request/response pattern with timeout and correlation IDs
//   - Runtime snapshot for observability
package event
