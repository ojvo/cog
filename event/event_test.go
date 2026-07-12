package event

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEventHub_Basic(t *testing.T) {
	config := DefaultEventHubConfig()
	config.UseWorkerPool = false // Use goroutines for simplicity in test
	bus := NewEventHub(config)
	defer bus.Close()

	var wg sync.WaitGroup
	wg.Add(1)

	var receivedData string
	bus.Subscribe("test_event", func(e Event) error {
		receivedData = e.Data.(string)
		wg.Done()
		return nil
	})

	bus.Publish(NewEvent("test_event", "hello"))

	wg.Wait()
	if receivedData != "hello" {
		t.Errorf("Expected 'hello', got '%s'", receivedData)
	}
}

func TestEventHub_Async(t *testing.T) {
	config := DefaultEventHubConfig()
	bus := NewEventHub(config)
	defer bus.Close()

	var wg sync.WaitGroup
	count := 10
	wg.Add(count)

	var received int64
	bus.Subscribe("async_event", func(e Event) error {
		defer wg.Done()
		atomic.AddInt64(&received, 1)
		time.Sleep(10 * time.Millisecond) // Simulate work
		return nil
	})

	for i := 0; i < count; i++ {
		bus.Publish(NewEvent("async_event", i))
	}

	wg.Wait()
	if received != int64(count) {
		t.Errorf("Expected %d, got %d", count, received)
	}
}

func TestEventHub_RequestResponse(t *testing.T) {
	config := DefaultEventHubConfig()
	bus := NewEventHub(config)
	defer bus.Close()

	// Responder
	bus.Subscribe("request_event", func(e Event) error {
		req := e.Data.(RequestEvent)
		bus.Respond(req, true, "response_data", nil)
		return nil
	})

	// Requester
	req := NewRequestEvent("request_event", "request_data")
	resp, err := bus.Request(req, 1*time.Second)

	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if !resp.Success {
		t.Error("Response should be success")
	}
	if resp.Data.(string) != "response_data" {
		t.Errorf("Expected 'response_data', got '%v'", resp.Data)
	}
}

func TestEventHub_Timeout(t *testing.T) {
	config := DefaultEventHubConfig()
	bus := NewEventHub(config)
	defer bus.Close()

	// Slow Responder
	bus.Subscribe("slow_event", func(e Event) error {
		time.Sleep(200 * time.Millisecond)
		req := e.Data.(RequestEvent)
		bus.Respond(req, true, "late", nil)
		return nil
	})

	req := NewRequestEvent("slow_event", "data")
	_, err := bus.Request(req, 50*time.Millisecond)

	if err == nil {
		t.Error("Request should timeout")
	}
}

func TestEventHub_Unsubscribe(t *testing.T) {
	config := DefaultEventHubConfig()
	bus := NewEventHub(config)
	defer bus.Close()

	var count int64
	handler := func(e Event) error {
		atomic.AddInt64(&count, 1)
		return nil
	}

	id := bus.Subscribe("unsub_event", handler)
	bus.PublishAndWait(NewEvent("unsub_event", 1))

	if atomic.LoadInt64(&count) != 1 {
		t.Error("Should receive first event")
	}

	bus.Unsubscribe(id)
	bus.PublishAndWait(NewEvent("unsub_event", 2))

	if atomic.LoadInt64(&count) != 1 {
		t.Error("Should not receive second event")
	}
}

func TestEventHub_Deduplication(t *testing.T) {
	config := DefaultEventHubConfig()
	config.EnableEventDeduplication = true
	bus := NewEventHub(config)
	defer bus.Close()

	var count int64
	bus.Subscribe("dedup_event", func(e Event) error {
		atomic.AddInt64(&count, 1)
		return nil
	})

	evt := NewEvent("dedup_event", "data")
	evt.EventID = "unique_id_1"

	bus.PublishAndWait(evt)
	bus.PublishAndWait(evt) // Duplicate

	if atomic.LoadInt64(&count) != 1 {
		t.Errorf("Expected 1 processing, got %d", count)
	}
}

func TestEventHub_RateLimit(t *testing.T) {
	config := DefaultEventHubConfig()
	bus := NewEventHub(config)
	defer bus.Close()

	bus.EnableRateLimiter(5) // 5 req/sec

	// Responder
	bus.Subscribe("limit_event", func(e Event) error {
		req := e.Data.(RequestEvent)
		bus.Respond(req, true, "ok", nil)
		return nil
	})

	successCount := 0
	failCount := 0

	for i := 0; i < 10; i++ {
		req := NewRequestEvent("limit_event", i)
		_, err := bus.Request(req, 100*time.Millisecond)
		if err != nil {
			failCount++
		} else {
			successCount++
		}
	}

	// First 5 should succeed (roughly), others might fail depending on refill
	if successCount == 0 {
		t.Error("Should allow some requests")
	}
	// Note: Rate limiter implementation might be strict or token bucket.
	// Current implementation: tokens = rateLimit initially.
	// So first 5 succeed immediately. 6th fails if no time elapsed.
	if failCount == 0 {
		t.Error("Should rate limit some requests")
	}
}

func TestEventHub_ContextCancel(t *testing.T) {
	config := DefaultEventHubConfig()
	bus := NewEventHub(config)
	defer bus.Close()

	bus.Subscribe("cancel_event", func(e Event) error {
		select {
		case <-time.After(1 * time.Second):
			req, ok := e.Data.(RequestEvent)
			if ok {
				bus.Respond(req, true, "late", nil)
			}
		case <-e.Context.Done():
			// Cancelled
		}
		return nil
	})

	req := NewRequestEvent("cancel_event", "data")

	go func() {
		time.Sleep(50 * time.Millisecond)
		req.CancelFunc()
	}()

	_, err := bus.Request(req, 2*time.Second)
	if err == nil {
		t.Error("Should return error on cancel")
	}
}

func TestEventHub_SubscribeOptions(t *testing.T) {
	config := DefaultEventHubConfig()
	bus := NewEventHub(config)
	defer bus.Close()

	// Filter test
	var received int64
	bus.SubscribeWithOptions("filter_event", func(e Event) error {
		atomic.AddInt64(&received, 1)
		return nil
	}, HandlerOptions{
		Async: false,
		Filter: func(e Event) bool {
			return e.Data.(int) > 5
		},
	})

	bus.Publish(NewEvent("filter_event", 1))  // Should be filtered
	bus.Publish(NewEvent("filter_event", 10)) // Should pass

	if atomic.LoadInt64(&received) != 1 {
		t.Errorf("Expected 1 event, got %d", received)
	}
}

func TestEventHub_OnError(t *testing.T) {
	config := DefaultEventHubConfig()
	config.UseWorkerPool = false
	bus := NewEventHub(config)
	defer bus.Close()

	var errReceived error
	var wg sync.WaitGroup
	wg.Add(1)

	bus.SubscribeWithOptions("error_event", func(e Event) error {
		return errors.New("handler error")
	}, HandlerOptions{
		Async: true,
		OnError: func(e Event, err error) {
			errReceived = err
			wg.Done()
		},
	})

	bus.Publish(NewEvent("error_event", "data"))

	wg.Wait()
	if errReceived == nil || errReceived.Error() != "handler error" {
		t.Error("OnError should be called with correct error")
	}
}

// ============================================================================
// Middleware Tests
// ============================================================================

func TestEventHub_Middleware_Transform(t *testing.T) {
	config := DefaultEventHubConfig()
	config.UseWorkerPool = false
	bus := NewEventHub(config)
	defer bus.Close()

	// Middleware that transforms data
	bus.Use(func(e Event) (Event, error) {
		if s, ok := e.Data.(string); ok {
			e.Data = s + "_transformed"
		}
		return e, nil
	})

	var wg sync.WaitGroup
	wg.Add(1)
	var received string
	bus.Subscribe("mw_event", func(e Event) error {
		received = e.Data.(string)
		wg.Done()
		return nil
	})

	bus.Publish(NewEvent("mw_event", "hello"))
	wg.Wait()

	if received != "hello_transformed" {
		t.Errorf("Expected 'hello_transformed', got '%s'", received)
	}
}

func TestEventHub_Middleware_Reject(t *testing.T) {
	config := DefaultEventHubConfig()
	config.UseWorkerPool = false
	bus := NewEventHub(config)
	defer bus.Close()

	// Middleware that rejects events with data == "blocked"
	bus.Use(func(e Event) (Event, error) {
		if e.Data == "blocked" {
			return e, fmt.Errorf("blocked by middleware")
		}
		return e, nil
	})

	var count int64
	bus.Subscribe("mw_reject", func(e Event) error {
		atomic.AddInt64(&count, 1)
		return nil
	})

	bus.Publish(NewEvent("mw_reject", "blocked"))
	bus.PublishAndWait(NewEvent("mw_reject", "allowed"))

	if atomic.LoadInt64(&count) != 1 {
		t.Errorf("Expected 1 event (blocked one rejected), got %d", count)
	}
}

func TestEventHub_Middleware_Chain(t *testing.T) {
	config := DefaultEventHubConfig()
	config.UseWorkerPool = false
	bus := NewEventHub(config)
	defer bus.Close()

	// Multiple middlewares in order
	bus.Use(func(e Event) (Event, error) {
		e.Data = e.Data.(string) + "_A"
		return e, nil
	})
	bus.Use(func(e Event) (Event, error) {
		e.Data = e.Data.(string) + "_B"
		return e, nil
	})

	var wg sync.WaitGroup
	wg.Add(1)
	var received string
	bus.Subscribe("mw_chain", func(e Event) error {
		received = e.Data.(string)
		wg.Done()
		return nil
	})

	bus.Publish(NewEvent("mw_chain", "start"))
	wg.Wait()

	if received != "start_A_B" {
		t.Errorf("Expected 'start_A_B', got '%s'", received)
	}
}

// ============================================================================
// SubscribeAsync Tests
// ============================================================================

func TestEventHub_SubscribeAsync_Basic(t *testing.T) {
	config := DefaultEventHubConfig()
	bus := NewEventHub(config)
	defer bus.Close()

	var wg sync.WaitGroup
	count := 20
	wg.Add(count)

	var received int64
	bus.SubscribeAsync("async_sub_event", func(e Event) error {
		defer wg.Done()
		atomic.AddInt64(&received, 1)
		return nil
	}, WithAsyncConcurrency(4), WithAsyncBufferSize(256))

	for i := 0; i < count; i++ {
		bus.Publish(NewEvent("async_sub_event", i))
	}

	wg.Wait()
	if atomic.LoadInt64(&received) != int64(count) {
		t.Errorf("Expected %d, got %d", count, received)
	}
}

func TestEventHub_SubscribeAsync_DropOnFull(t *testing.T) {
	config := DefaultEventHubConfig()
	bus := NewEventHub(config)
	defer bus.Close()

	var processed int64
	// Small buffer, slow handler - some events will be dropped
	bus.SubscribeAsync("async_drop", func(e Event) error {
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt64(&processed, 1)
		return nil
	}, WithAsyncConcurrency(1), WithAsyncBufferSize(2), WithQueuePolicy(DropOnFull))

	// Publish many events quickly
	for i := 0; i < 20; i++ {
		bus.Publish(NewEvent("async_drop", i))
	}

	// Give time for processing
	time.Sleep(200 * time.Millisecond)

	// Should have processed fewer than 20 (some dropped)
	processedCount := atomic.LoadInt64(&processed)
	if processedCount >= 20 {
		t.Errorf("Expected some events dropped, processed %d", processedCount)
	}
	if processedCount == 0 {
		t.Error("Should have processed at least some events")
	}
}

func TestEventHub_SubscribeAsync_BlockOnFull(t *testing.T) {
	config := DefaultEventHubConfig()
	bus := NewEventHub(config)
	defer bus.Close()

	var processed int64
	// Small buffer, BlockOnFull policy
	bus.SubscribeAsync("async_block", func(e Event) error {
		atomic.AddInt64(&processed, 1)
		return nil
	}, WithAsyncConcurrency(1), WithAsyncBufferSize(2), WithQueuePolicy(BlockOnFull))

	// Publish events - should block but not drop
	for i := 0; i < 10; i++ {
		bus.Publish(NewEvent("async_block", i))
	}

	// Give time for processing
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt64(&processed) != 10 {
		t.Errorf("Expected 10 processed (BlockOnFull), got %d", atomic.LoadInt64(&processed))
	}
}

func TestEventHub_SubscribeAsync_Unsubscribe(t *testing.T) {
	config := DefaultEventHubConfig()
	bus := NewEventHub(config)
	defer bus.Close()

	var count int64
	id := bus.SubscribeAsync("async_unsub", func(e Event) error {
		atomic.AddInt64(&count, 1)
		return nil
	}, WithAsyncConcurrency(1), WithAsyncBufferSize(10))

	bus.Publish(NewEvent("async_unsub", 1))
	time.Sleep(50 * time.Millisecond)

	if atomic.LoadInt64(&count) != 1 {
		t.Error("Should receive first event")
	}

	// Unsubscribe via unified Unsubscribe
	if !bus.Unsubscribe(id) {
		t.Error("Unsubscribe should return true for async subscription")
	}

	bus.Publish(NewEvent("async_unsub", 2))
	time.Sleep(50 * time.Millisecond)

	if atomic.LoadInt64(&count) != 1 {
		t.Error("Should not receive second event after unsubscribe")
	}
}

// ============================================================================
// SubscribeBatch Tests
// ============================================================================

func TestEventHub_SubscribeBatch_BySize(t *testing.T) {
	config := DefaultEventHubConfig()
	bus := NewEventHub(config)
	defer bus.Close()

	var wg sync.WaitGroup
	wg.Add(1)

	var batches [][]Event
	bus.SubscribeBatch("batch_size", func(events []Event) error {
		batches = append(batches, events)
		if len(batches) == 2 { // 10 events / 5 per batch = 2 batches
			wg.Done()
		}
		return nil
	}, WithBatchSize(5), WithFlushInterval(10*time.Second))

	for i := 0; i < 10; i++ {
		bus.Publish(NewEvent("batch_size", i))
	}

	wg.Wait()

	if len(batches) != 2 {
		t.Fatalf("Expected 2 batches, got %d", len(batches))
	}
	if len(batches[0]) != 5 || len(batches[1]) != 5 {
		t.Errorf("Expected 5 events per batch, got %d and %d", len(batches[0]), len(batches[1]))
	}
}

func TestEventHub_SubscribeBatch_ByTime(t *testing.T) {
	config := DefaultEventHubConfig()
	bus := NewEventHub(config)
	defer bus.Close()

	var wg sync.WaitGroup
	wg.Add(1)

	var batch []Event
	bus.SubscribeBatch("batch_time", func(events []Event) error {
		batch = events
		wg.Done()
		return nil
	}, WithBatchSize(100), WithFlushInterval(50*time.Millisecond))

	// Publish 3 events (less than batchSize)
	for i := 0; i < 3; i++ {
		bus.Publish(NewEvent("batch_time", i))
	}

	// Should flush by time, not by size
	wg.Wait()

	if len(batch) != 3 {
		t.Errorf("Expected 3 events in batch, got %d", len(batch))
	}
}

func TestEventHub_SubscribeBatch_FlushOnClose(t *testing.T) {
	config := DefaultEventHubConfig()
	bus := NewEventHub(config)

	var batch []Event
	bus.SubscribeBatch("batch_close", func(events []Event) error {
		batch = events
		return nil
	}, WithBatchSize(100), WithFlushInterval(10*time.Second))

	// Publish events that won't trigger size or time flush
	for i := 0; i < 5; i++ {
		bus.Publish(NewEvent("batch_close", i))
	}

	// Close should flush remaining events
	bus.Close()

	if len(batch) != 5 {
		t.Errorf("Expected 5 events flushed on close, got %d", len(batch))
	}
}

func TestEventHub_SubscribeBatch_Unsubscribe(t *testing.T) {
	config := DefaultEventHubConfig()
	bus := NewEventHub(config)
	defer bus.Close()

	var count int64
	bus.SubscribeBatch("batch_unsub", func(events []Event) error {
		atomic.AddInt64(&count, int64(len(events)))
		return nil
	}, WithBatchSize(100), WithFlushInterval(50*time.Millisecond))

	bus.Publish(NewEvent("batch_unsub", 1))
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt64(&count) != 1 {
		t.Error("Should receive first batch")
	}

	if !bus.UnsubscribeBatch("batch_unsub") {
		t.Error("UnsubscribeBatch should return true")
	}

	bus.Publish(NewEvent("batch_unsub", 2))
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt64(&count) != 1 {
		t.Error("Should not receive events after unsubscribe")
	}
}

func TestEventHub_SubscribeBatch_Retry(t *testing.T) {
	config := DefaultEventHubConfig()
	bus := NewEventHub(config)
	defer bus.Close()

	var attempt int64
	var wg sync.WaitGroup
	wg.Add(1)

	bus.SubscribeBatch("batch_retry", func(events []Event) error {
		attempts := atomic.AddInt64(&attempt, 1)
		if attempts < 3 {
			return fmt.Errorf("simulated failure")
		}
		wg.Done()
		return nil
	}, WithBatchSize(1), WithFlushInterval(50*time.Millisecond), WithBatchMaxRetry(3))

	bus.Publish(NewEvent("batch_retry", 1))
	wg.Wait()

	if atomic.LoadInt64(&attempt) < 3 {
		t.Errorf("Expected at least 3 attempts, got %d", atomic.LoadInt64(&attempt))
	}
}

func TestEventHub_Metrics(t *testing.T) {
	config := DefaultEventHubConfig()
	config.UseWorkerPool = false
	bus := NewEventHub(config)
	defer bus.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	bus.Subscribe("metric_test", func(e Event) error {
		wg.Done()
		return nil
	})

	bus.Publish(NewEvent("metric_test", "a"))
	bus.Publish(NewEvent("metric_test", "b"))
	wg.Wait()

	metrics := bus.GetEventMetrics()
	m, ok := metrics["metric_test"]
	if !ok {
		t.Fatal("expected metrics for 'metric_test'")
	}
	if m["processed"] != 2 {
		t.Errorf("expected 2 processed, got %d", m["processed"])
	}
	if m["dropped"] != 0 {
		t.Errorf("expected 0 dropped, got %d", m["dropped"])
	}
}

func TestEventHub_Metrics_Dropped(t *testing.T) {
	config := DefaultEventHubConfig()
	bus := NewEventHub(config)
	defer bus.Close()

	var wg sync.WaitGroup
	wg.Add(1)

	// Subscribe async with tiny buffer and DropOnFull; slow handler ensures drops
	bus.SubscribeAsync("drop_test", func(e Event) error {
		if e.Data.(int) == 0 {
			wg.Done()
		}
		time.Sleep(100 * time.Millisecond)
		return nil
	}, WithAsyncBufferSize(1), WithAsyncConcurrency(1), WithQueuePolicy(DropOnFull))

	for i := 0; i < 10; i++ {
		bus.Publish(NewEvent("drop_test", i))
	}

	wg.Wait()
	time.Sleep(50 * time.Millisecond) // Allow remaining drops to register

	metrics := bus.GetEventMetrics()
	m, ok := metrics["drop_test"]
	if !ok {
		t.Fatal("expected metrics for 'drop_test'")
	}
	if m["processed"] != 10 {
		t.Errorf("expected 10 processed, got %d", m["processed"])
	}
	if m["dropped"] == 0 {
		t.Error("expected some dropped events")
	}
}

func TestEventHub_GetSnapshot(t *testing.T) {
	config := DefaultEventHubConfig()
	config.UseWorkerPool = false
	bus := NewEventHub(config)

	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe("snap_test", func(e Event) error {
		wg.Done()
		return nil
	})

	bus.SubscribeAsync("snap_async", func(e Event) error {
		return nil
	}, WithAsyncConcurrency(1))

	bus.SubscribeBatch("snap_batch", func(events []Event) error {
		return nil
	}, WithBatchSize(10), WithFlushInterval(100*time.Millisecond))

	bus.Publish(NewEvent("snap_test", "x"))
	wg.Wait()

	snap := bus.GetSnapshot()
	if snap.Closed {
		t.Error("expected Closed=false")
	}
	if snap.Subscriptions != 1 {
		t.Errorf("expected 1 subscription, got %d", snap.Subscriptions)
	}
	if snap.AsyncWorkers != 1 {
		t.Errorf("expected 1 async worker, got %d", snap.AsyncWorkers)
	}
	if snap.BatchWorkers != 1 {
		t.Errorf("expected 1 batch worker, got %d", snap.BatchWorkers)
	}
	if snap.Metrics == nil {
		t.Error("expected non-nil Metrics")
	}
	if _, ok := snap.Metrics["snap_test"]; !ok {
		t.Error("expected metrics for 'snap_test'")
	}
	if snap.RequestStats == nil {
		t.Error("expected non-nil RequestStats")
	}

	bus.Close()
	snap2 := bus.GetSnapshot()
	if !snap2.Closed {
		t.Error("expected Closed=true after Close")
	}
}
