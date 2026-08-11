package coll

import "sync/atomic"

// linkedNode is the internal node of LinkedQueue.
type linkedNode[T any] struct {
	value T
	next  atomic.Pointer[linkedNode[T]]
}

// LinkedQueue is a lock-free FIFO queue based on the Michael-Scott algorithm.
// It is unbounded and safe for concurrent use by multiple producers and
// consumers. The queue uses an atomic compare-and-swap loop on a singly
// linked list with a sentinel (dummy) head node.
//
// The zero value is not ready to use; call NewLinkedQueue.
//
// ABA safety: nodes are never recycled by the queue itself; reclamation is
// delegated to the Go garbage collector, so the classic ABA problem of
// Michael-Scott queues does not arise here.
type LinkedQueue[T any] struct {
	head atomic.Pointer[linkedNode[T]]
	tail atomic.Pointer[linkedNode[T]]
	len  int64
}

// NewLinkedQueue creates an empty LinkedQueue.
func NewLinkedQueue[T any]() *LinkedQueue[T] {
	dummy := &linkedNode[T]{}
	q := &LinkedQueue[T]{}
	q.head.Store(dummy)
	q.tail.Store(dummy)
	return q
}

// Enqueue appends v to the back of the queue.
// This operation is lock-free; a failed CAS retries in a bounded spin loop.
func (q *LinkedQueue[T]) Enqueue(v T) {
	n := &linkedNode[T]{value: v}
	for {
		tail := q.tail.Load()
		next := tail.next.Load()
		if tail == q.tail.Load() { // re-check consistency
			if next == nil {
				// Try to link the new node after tail.
				if tail.next.CompareAndSwap(nil, n) {
					// Advance tail; another enqueue may already have done it,
					// so a failed CAS here is harmless.
					q.tail.CompareAndSwap(tail, n)
					atomic.AddInt64(&q.len, 1)
					return
				}
			} else {
				// Tail lagged behind; advance it.
				q.tail.CompareAndSwap(tail, next)
			}
		}
	}
}

// Dequeue removes and returns the front element.
// Returns the zero value and false if the queue is empty.
func (q *LinkedQueue[T]) Dequeue() (T, bool) {
	var zero T
	for {
		head := q.head.Load()
		tail := q.tail.Load()
		next := head.next.Load()
		if head == q.head.Load() { // re-check consistency
			if head == tail {
				if next == nil {
					// Queue is empty.
					return zero, false
				}
				// Tail lagged behind; advance it.
				q.tail.CompareAndSwap(tail, next)
			} else {
				// Read value before CAS, otherwise another dequeue may free
				// next before we read it.
				v := next.value
				if q.head.CompareAndSwap(head, next) {
					atomic.AddInt64(&q.len, -1)
					return v, true
				}
			}
		}
	}
}

// Len returns the number of elements currently in the queue.
// The value is a snapshot and may be stale by the time it is used.
func (q *LinkedQueue[T]) Len() int64 {
	return atomic.LoadInt64(&q.len)
}

// Empty reports whether the queue is empty.
// The result is a snapshot and may be stale by the time it is used.
func (q *LinkedQueue[T]) Empty() bool {
	return q.Len() == 0
}
