// Package conc provides concurrency primitives:
//   - WorkerPool: bounded goroutine pool with task queue and stats
//   - Queue: simple thread-safe FIFO queue
//   - QueueLf: lock-free ring buffer queue (MPSC)
//   - Batch: batch collector with size/time-based flushing
//
// All types are safe for concurrent use.
package conc
