package conc

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPool_Basic(t *testing.T) {
	pool := NewWorkerPool(2, 10)
	pool.Start()
	defer pool.Stop()

	var wg sync.WaitGroup
	count := 10
	wg.Add(count)
	
	var processed int64
	
	for i := 0; i < count; i++ {
		pool.Submit(func() {
			defer wg.Done()
			atomic.AddInt64(&processed, 1)
			time.Sleep(10 * time.Millisecond)
		})
	}

	wg.Wait()

	if processed != int64(count) {
		t.Errorf("Expected %d tasks processed, got %d", count, processed)
	}
}

func TestWorkerPool_Timeout(t *testing.T) {
	// Small queue to force blocking
	pool := NewWorkerPool(1, 1)
	pool.Start()
	defer pool.Stop()

	// Fill queue and busy worker
	pool.Submit(func() {
		time.Sleep(200 * time.Millisecond)
	})
	pool.Submit(func() {
		time.Sleep(200 * time.Millisecond)
	})

	// This should fail due to timeout
	success := pool.SubmitWithTimeout(func() {}, 10*time.Millisecond)
	if success {
		t.Error("SubmitWithTimeout should fail when queue is full")
	}

	// This should succeed after waiting
	success = pool.SubmitWithTimeout(func() {}, 500*time.Millisecond)
	if !success {
		t.Error("SubmitWithTimeout should succeed when slot becomes available")
	}
}

func TestWorkerPool_Stats(t *testing.T) {
	pool := NewWorkerPool(2, 10)
	pool.Start()
	
	var wg sync.WaitGroup
	wg.Add(1)
	pool.Submit(func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
	})
	
	stats := pool.Stats()
	if stats.Workers != 2 {
		t.Errorf("Expected 2 workers, got %d", stats.Workers)
	}
	if stats.QueueSize != uint32(16) { // QueueLf rounds up to power of 2
		t.Errorf("Expected queue size 16, got %d", stats.QueueSize)
	}
	
	wg.Wait()
	pool.Stop()
}

func TestWorkerPool_Panic(t *testing.T) {
	pool := NewWorkerPool(1, 10)
	pool.Start()
	defer pool.Stop()

	var wg sync.WaitGroup
	wg.Add(1)
	
	// Should not crash the pool
	pool.Submit(func() {
		defer wg.Done()
		panic("test panic")
	})

	wg.Wait()
	
	// Should still work
	done := make(chan bool)
	pool.Submit(func() {
		done <- true
	})
	
	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("WorkerPool stopped working after panic")
	}
}

func TestWorkerPool_StopWait(t *testing.T) {
	pool := NewWorkerPool(2, 10)
	pool.Start()

	var processed int64
	// Submit tasks that take some time
	for i := 0; i < 5; i++ {
		pool.Submit(func() {
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt64(&processed, 1)
		})
	}

	// Stop should wait for tasks to finish
	pool.Stop()

	if atomic.LoadInt64(&processed) != 5 {
		t.Errorf("Stop did not wait for all tasks to complete. Processed: %d", processed)
	}
	
	// Should not accept new tasks
	if pool.Submit(func() {}) {
		t.Error("Should not accept tasks after Stop")
	}
}

func TestWorkerPool_DefaultPool(t *testing.T) {
	// Ensure default pool is initialized
	p1 := GetDefaultPool()
	p2 := GetDefaultPool()
	
	if p1 != p2 {
		t.Error("Default pool should be singleton")
	}
	
	// Test helper functions
	done := make(chan struct{})
	Submit(func() {
		close(done)
	})
	
	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("Default pool Submit failed")
	}
}

func TestWorkerPool_Defaults(t *testing.T) {
	// Test with 0 workers and 0 queue size
	pool := NewWorkerPool(0, 0)
	
	stats := pool.Stats()
	if stats.Workers <= 0 {
		t.Error("Expected default workers > 0")
	}
	if stats.QueueSize <= 0 {
		t.Error("Expected default queue size > 0")
	}
}
