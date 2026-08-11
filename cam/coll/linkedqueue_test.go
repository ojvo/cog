package coll

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestLinkedQueue_Empty(t *testing.T) {
	q := NewLinkedQueue[int]()
	if !q.Empty() {
		t.Fatal("new queue should be empty")
	}
	if got := q.Len(); got != 0 {
		t.Fatalf("Len = %d, want 0", got)
	}
	if _, ok := q.Dequeue(); ok {
		t.Fatal("Dequeue on empty queue should return ok=false")
	}
}

func TestLinkedQueue_BasicFIFO(t *testing.T) {
	q := NewLinkedQueue[int]()

	for i := 0; i < 5; i++ {
		q.Enqueue(i)
		if got := q.Len(); got != int64(i+1) {
			t.Fatalf("after %d enqueues, Len = %d, want %d", i+1, got, i+1)
		}
	}

	for i := 0; i < 5; i++ {
		v, ok := q.Dequeue()
		if !ok {
			t.Fatalf("Dequeue returned ok=false at i=%d", i)
		}
		if v != i {
			t.Fatalf("Dequeue = %d, want %d", v, i)
		}
	}

	if _, ok := q.Dequeue(); ok {
		t.Fatal("Dequeue after drain should return ok=false")
	}
	if !q.Empty() {
		t.Fatal("drained queue should be empty")
	}
}

func TestLinkedQueue_StringValues(t *testing.T) {
	q := NewLinkedQueue[string]()
	values := []string{"alpha", "beta", "gamma", "delta"}
	for _, v := range values {
		q.Enqueue(v)
	}
	for i, want := range values {
		got, ok := q.Dequeue()
		if !ok {
			t.Fatalf("Dequeue returned ok=false at i=%d", i)
		}
		if got != want {
			t.Fatalf("Dequeue = %q, want %q", got, want)
		}
	}
}

func TestLinkedQueue_ZeroValueIsUsable(t *testing.T) {
	// Ensure Enqueue/Dequeue on a zero int value behave correctly.
	q := NewLinkedQueue[int]()
	q.Enqueue(0)
	v, ok := q.Dequeue()
	if !ok {
		t.Fatal("Dequeue should return ok=true for enqueued zero value")
	}
	if v != 0 {
		t.Fatalf("Dequeue = %d, want 0", v)
	}
}

func TestLinkedQueue_Interleaved(t *testing.T) {
	q := NewLinkedQueue[int]()
	const N = 100

	// Enqueue then immediately dequeue N times.
	for i := 0; i < N; i++ {
		q.Enqueue(i)
		v, ok := q.Dequeue()
		if !ok {
			t.Fatalf("Dequeue returned ok=false at i=%d", i)
		}
		if v != i {
			t.Fatalf("Dequeue = %d, want %d", v, i)
		}
		if q.Len() != 0 {
			t.Fatalf("Len = %d after balanced op, want 0", q.Len())
		}
	}
}

func TestLinkedQueue_ConcurrentSingleProducerSingleConsumer(t *testing.T) {
	q := NewLinkedQueue[int]()
	const N = 10000

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < N; i++ {
			q.Enqueue(i)
		}
	}()

	go func() {
		defer wg.Done()
		var seq int
		for seq < N {
			v, ok := q.Dequeue()
			if !ok {
				continue
			}
			if v != seq {
				t.Errorf("Dequeue = %d, want %d", v, seq)
				return
			}
			seq++
		}
	}()

	wg.Wait()
	if !q.Empty() {
		t.Fatal("queue should be empty after balanced producer/consumer")
	}
}

// TestLinkedQueue_ConcurrentMPMC verifies that under many producers and many
// consumers, every enqueued item is dequeued exactly once. (FIFO order across
// multiple concurrent consumers is not checkable without a global dequeue
// timeline; the SPSC test above covers per-producer FIFO ordering.)
func TestLinkedQueue_ConcurrentMPMC(t *testing.T) {
	q := NewLinkedQueue[int]()
	const Producers = 8
	const Consumers = 8
	const PerProducer = 2000
	const Total = Producers * PerProducer

	var prodWg sync.WaitGroup
	prodWg.Add(Producers)
	for p := 0; p < Producers; p++ {
		p := p
		go func() {
			defer prodWg.Done()
			base := p * PerProducer
			for i := 0; i < PerProducer; i++ {
				q.Enqueue(base + i)
			}
		}()
	}

	// Each consumer writes to its own slice to avoid lock contention; we
	// merge after all consumers exit.
	perConsumer := make([][]int, Consumers)
	for i := range perConsumer {
		perConsumer[i] = make([]int, 0, Total/Consumers)
	}
	producersDone := int32(0)

	var consWg sync.WaitGroup
	consWg.Add(Consumers)
	for c := 0; c < Consumers; c++ {
		c := c
		go func() {
			defer consWg.Done()
			for {
				v, ok := q.Dequeue()
				if !ok {
					if atomic.LoadInt32(&producersDone) == Producers && q.Empty() {
						return
					}
					continue
				}
				perConsumer[c] = append(perConsumer[c], v)
			}
		}()
	}

	prodWg.Wait()
	atomic.StoreInt32(&producersDone, Producers)
	consWg.Wait()

	// Validate: every value in [0, Total) appears exactly once.
	seen := make([]int, Total)
	totalSeen := 0
	for _, slice := range perConsumer {
		for _, v := range slice {
			if v < 0 || v >= Total {
				t.Errorf("value out of range: %d", v)
				continue
			}
			seen[v]++
			totalSeen++
		}
	}
	if totalSeen != Total {
		t.Errorf("total seen = %d, want %d", totalSeen, Total)
	}
	dupOrMissing := 0
	for v, count := range seen {
		if count != 1 {
			if dupOrMissing < 5 {
				t.Errorf("value %d seen %d times, want 1", v, count)
			}
			dupOrMissing++
		}
	}
	if dupOrMissing > 5 {
		t.Errorf("(%d more duplicate/missing values suppressed)", dupOrMissing-5)
	}
}
