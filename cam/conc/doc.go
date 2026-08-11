// Package conc provides concurrency primitives:
//   - WorkerPool: bounded goroutine pool with task queue and stats
//   - Queue: simple thread-safe FIFO queue
//   - QueueLf: lock-free ring buffer queue (MPSC)
//   - Batch: batch collector with size/time-based flushing
//   - Future: a generic promise with timeout/context cancellation and callbacks
//   - Semaphore: counting resizable CAS-based semaphore with context cancellation
//   - Singleflight: deduplicates concurrent calls by key so fn runs once per key
//
// All types are safe for concurrent use.
package conc
