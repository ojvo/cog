package coll

import (
	"sync"
	"testing"
	"time"
)

func TestRingBuffer_Simple(t *testing.T) {
	rb := NewRingBuffer[int](5)

	for i := 0; i < 5; i++ {
		rb.EnQueue(i)
	}

	for i := 0; i < 5; i++ {
		val := rb.DeQueue()
		if val != i {
			t.Errorf("expected %d, got %d", i, val)
		}
	}
}

func TestRingBuffer_Concurrency(t *testing.T) {
	rb := NewRingBuffer[int](100)
	count := 1000
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < count; i++ {
			rb.EnQueue(i)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < count; i++ {
			val := rb.DeQueue()
			if val != i {
				t.Errorf("expected %d, got %d", i, val)
			}
		}
	}()

	wg.Wait()
}

func TestRingBuffer_Many(t *testing.T) {
	rb := NewRingBuffer[int](10)

	inputs := []int{1, 2, 3, 4, 5}
	rb.EnQueueMany(inputs)

	outputs := make([]int, 5)
	rb.DeQueueMany(outputs)

	for i, v := range outputs {
		if v != inputs[i] {
			t.Errorf("expected %d, got %d", inputs[i], v)
		}
	}
}

func TestRingBuffer_BlocksWhenFull(t *testing.T) {
	rb := NewRingBuffer[int](2)
	rb.EnQueue(1)
	rb.EnQueue(2)

	// Should block — spawn a consumer to drain
	done := make(chan struct{})
	go func() {
		rb.EnQueue(3) // blocks until space available
		close(done)
	}()

	// Drain one item to unblock
	val := rb.DeQueue()
	if val != 1 {
		t.Errorf("expected 1, got %d", val)
	}

	// Wait for producer to complete
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: producer did not complete within 5s")
	}
}

func TestRingBuffer_BlocksWhenEmpty(t *testing.T) {
	rb := NewRingBuffer[int](2)

	// Should block — spawn a producer
	done := make(chan struct{})
	go func() {
		val := rb.DeQueue() // blocks until item available
		if val != 42 {
			t.Errorf("expected 42, got %d", val)
		}
		close(done)
	}()

	rb.EnQueue(42)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: consumer did not complete within 5s")
	}
}
