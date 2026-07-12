package conc

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueueLf_Basic(t *testing.T) {
	q := NewQueueLf(10)
	if q.Capacity() != 16 { // Power of 2
		t.Errorf("Expected capacity 16, got %d", q.Capacity())
	}

	ok, _ := q.Put(1)
	if !ok {
		t.Error("Put failed")
	}

	val, ok, _ := q.Get()
	if !ok {
		t.Error("Get failed")
	}
	if val.(int) != 1 {
		t.Errorf("Expected 1, got %v", val)
	}
}

func TestQueueLf_Concurrency(t *testing.T) {
	q := NewQueueLf(1024)
	count := 100000
	var wg sync.WaitGroup

	// Producers
	producers := 4
	wg.Add(producers)
	for i := 0; i < producers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < count/producers; j++ {
				q.Put(j)
			}
		}()
	}

	// Consumers
	consumers := 4
	var received int64
	wg.Add(consumers)
	for i := 0; i < consumers; i++ {
		go func() {
			defer wg.Done()
			for {
				val, ok, _ := q.Get()
				if !ok {
					return // Closed
				}
				if val == nil {
					// Should not happen if ok is true, unless we have nil values (we don't here)
					continue
				}
				atomic.AddInt64(&received, 1)
				if atomic.LoadInt64(&received) == int64(count) {
					q.Close()
					return
				}
			}
		}()
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Test timed out - potential deadlock")
	}

	if received != int64(count) {
		t.Errorf("Expected %d items, got %d", count, received)
	}
}

func TestQueueLf_Blocking(t *testing.T) {
	q := NewQueueLf(4) // Small capacity (usable 3)

	// Fill queue
	q.Put(1)
	q.Put(2)
	q.Put(3)

	// TryPut should fail (queue full)
	ok, _ := q.TryPut(4)
	if ok {
		t.Error("TryPut should fail when full")
	}

	// Blocking Put
	done := make(chan struct{})
	go func() {
		q.Put(4)
		close(done)
	}()

	select {
	case <-done:
		t.Error("Put should block when full")
	case <-time.After(100 * time.Millisecond):
		// Expected
	}

	// Release one item
	val, _, _ := q.Get()
	if val.(int) != 1 {
		t.Errorf("Expected 1, got %v", val)
	}

	select {
	case <-done:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Put should unblock after Get")
	}
}

func TestQueueLf_Batch(t *testing.T) {
	q := NewQueueLf(16)
	
	items := []interface{}{1, 2, 3, 4, 5}
	cnt, _ := q.Puts(items)
	if cnt != 5 {
		t.Errorf("Expected 5 puts, got %d", cnt)
	}

	out := make([]interface{}, 5)
	cnt, _ = q.Gets(out)
	if cnt != 5 {
		t.Errorf("Expected 5 gets, got %d", cnt)
	}
	
	for i, v := range out {
		if v.(int) != items[i].(int) {
			t.Errorf("Item %d mismatch: expected %v, got %v", i, items[i], v)
		}
	}
}
