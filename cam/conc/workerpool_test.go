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

func TestWorkerPool_SubmitWait(t *testing.T) {
	pool := NewWorkerPool(2, 10)
	pool.Start()
	defer pool.Stop()

	// Basic: SubmitWait blocks until task completes and the side effect is visible.
	var result int64
	ok := pool.SubmitWait(func() {
		atomic.StoreInt64(&result, 42)
	})
	if !ok {
		t.Fatal("SubmitWait returned false on running pool")
	}
	// Since SubmitWait blocks until completion, the result must be observable.
	if got := atomic.LoadInt64(&result); got != 42 {
		t.Errorf("expected result=42 after SubmitWait, got %d", got)
	}
}

func TestWorkerPool_SubmitWait_Ordering(t *testing.T) {
	pool := NewWorkerPool(1, 10) // single worker → strict serial execution
	pool.Start()
	defer pool.Stop()

	var seq []int
	var mu sync.Mutex
	for i := 0; i < 10; i++ {
		i := i
		if !pool.SubmitWait(func() {
			mu.Lock()
			seq = append(seq, i)
			mu.Unlock()
		}) {
			t.Fatalf("SubmitWait returned false at i=%d", i)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seq) != 10 {
		t.Fatalf("expected 10 results, got %d", len(seq))
	}
	for i, v := range seq {
		if v != i {
			t.Errorf("expected seq[%d]=%d, got %d", i, i, v)
		}
	}
}

func TestWorkerPool_SubmitWait_AfterStop(t *testing.T) {
	pool := NewWorkerPool(1, 10)
	pool.Start()
	pool.Stop()

	// SubmitWait on a stopped pool must return false immediately (no blocking).
	done := make(chan bool)
	go func() {
		done <- pool.SubmitWait(func() {})
	}()
	select {
	case ok := <-done:
		if ok {
			t.Error("SubmitWait should return false after Stop")
		}
	case <-time.After(time.Second):
		t.Fatal("SubmitWait blocked on stopped pool")
	}
}

func TestWorkerPool_SubmitWait_Panic(t *testing.T) {
	pool := NewWorkerPool(1, 10)
	pool.Start()
	defer pool.Stop()

	// A panicking task should be recovered by the worker, and SubmitWait
	// must still return (not block forever) because close(done) is in defer.
	ok := pool.SubmitWait(func() {
		panic("boom")
	})
	if !ok {
		t.Error("SubmitWait returned false despite panic; expected true since task did execute")
	}

	// Pool should still be usable after a panic.
	var ran int32
	ok = pool.SubmitWait(func() {
		atomic.StoreInt32(&ran, 1)
	})
	if !ok || atomic.LoadInt32(&ran) != 1 {
		t.Error("pool not usable after panic in SubmitWait task")
	}
}
