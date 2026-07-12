package main

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"ojv/cog/event"
)

// This demo shows EventHub combination features:
//   - Middleware chain (filtering + enrichment)
//   - SubscribeAsync with bounded queue and backpressure policy
//   - SubscribeBatch with size/interval-based flushing
//   - GetSnapshot for observability
func main() {
	fmt.Println("=== Cog/Event Integration Demo ===")
	fmt.Println("EventHub: Middleware + Async + Batch + Snapshot")
	fmt.Println()

	cfg := event.DefaultEventHubConfig()
	cfg.UseWorkerPool = false // use goroutines for clarity
	bus := event.NewEventHub(cfg)
	defer bus.Close()

	// 1. Middleware: enrich event with prefix, reject "drop" events
	bus.Use(func(e event.Event) (event.Event, error) {
		if strings.Contains(e.Type, "drop") {
			return e, fmt.Errorf("dropped by middleware")
		}
		return e, nil
	})

	// 2. Regular synchronous subscriber
	var syncCount int64
	bus.Subscribe("user.login", func(e event.Event) error {
		atomic.AddInt64(&syncCount, 1)
		return nil
	})

	// 3. Dedicated async subscriber with bounded queue
	var asyncCount int64
	bus.SubscribeAsync("user.login", func(e event.Event) error {
		atomic.AddInt64(&asyncCount, 1)
		return nil
	},
		event.WithAsyncConcurrency(2),
		event.WithAsyncBufferSize(128),
	)

	// 4. Batch subscriber: flush every 10 events or 200ms
	var batchCount int64
	bus.SubscribeBatch("user.login", func(events []event.Event) error {
		atomic.AddInt64(&batchCount, int64(len(events)))
		return nil
	},
		event.WithBatchSize(10),
		event.WithFlushInterval(200*time.Millisecond),
	)

	// 5. Publish events
	const total = 25
	for i := 0; i < total; i++ {
		bus.Publish(event.NewEvent("user.login", fmt.Sprintf("user-%d", i)))
	}
	// One event rejected by middleware
	bus.Publish(event.NewEvent("user.drop", "rejected"))

	// 6. Wait for async + batch to drain
	time.Sleep(500 * time.Millisecond)

	// 7. Observability snapshot
	snap := bus.GetSnapshot()
	fmt.Printf("[Sync]      Processed: %d\n", atomic.LoadInt64(&syncCount))
	fmt.Printf("[Async]     Processed: %d\n", atomic.LoadInt64(&asyncCount))
	fmt.Printf("[Batch]     Processed: %d\n", atomic.LoadInt64(&batchCount))
	fmt.Printf("[Snapshot]  Subs=%d AsyncWorkers=%d BatchWorkers=%d Closed=%v\n",
		snap.Subscriptions, snap.AsyncWorkers, snap.BatchWorkers, snap.Closed)
	fmt.Println()
	fmt.Println("Demo completed successfully.")
}
