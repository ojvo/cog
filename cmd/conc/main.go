package main

import (
	"fmt"
	"sync/atomic"
	"time"

	"ojv/cog/conc"
)

// This demo shows WorkerPool + LockFreeQueue combination:
//   - Submit tasks to a bounded worker pool
//   - Observe typed WorkerPoolStats (workers/queue/tasks)
//   - Use SubmitWithTimeout for backpressure
//   - Demonstrate panic isolation
func main() {
	fmt.Println("=== Cog/Conc Integration Demo ===")
	fmt.Println("WorkerPool + LockFreeQueue + Typed Stats")
	fmt.Println()

	// 1. Create a pool with 4 workers and queue size 64
	pool := conc.NewWorkerPool(4, 64)
	pool.Start()
	defer pool.Stop()

	// 2. Submit a batch of tasks
	var processed int64
	const total = 100
	for i := 0; i < total; i++ {
		pool.Submit(func() {
			time.Sleep(2 * time.Millisecond)
			atomic.AddInt64(&processed, 1)
		})
	}

	// 3. Snapshot stats while running
	stats := pool.Stats()
	fmt.Printf("[Stats] Workers=%d Running=%v QueueSize=%d QueueUsed=%d TasksTotal=%d TasksCompleted=%d\n",
		stats.Workers, stats.Running, stats.QueueSize, stats.QueueUsed, stats.TasksTotal, stats.TasksCompleted)

	// 4. Backpressure: SubmitWithTimeout returns false when queue is full
	fullPool := conc.NewWorkerPool(1, 1)
	fullPool.Start()
	defer fullPool.Stop()
	fullPool.Submit(func() { time.Sleep(100 * time.Millisecond) })
	fullPool.Submit(func() { time.Sleep(100 * time.Millisecond) })
	if ok := fullPool.SubmitWithTimeout(func() {}, 10*time.Millisecond); !ok {
		fmt.Println("[Backpressure] SubmitWithTimeout correctly rejected on full queue")
	}

	// 5. Panic isolation: panicking task does not crash the pool
	pool.Submit(func() { panic("demo panic") })
	pool.Submit(func() { atomic.AddInt64(&processed, 1) })

	// Wait for all tasks to drain
	for {
		s := pool.Stats()
		if s.TasksCompleted >= s.TasksTotal {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	fmt.Printf("[Result] Processed=%d/%d\n", atomic.LoadInt64(&processed), total+1)
	fmt.Println()
	fmt.Println("Demo completed successfully.")
}
