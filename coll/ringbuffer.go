package coll

import "sync"

// RingBuffer is a thread-safe, blocking ring buffer.
// EnQueue blocks when full; DeQueue blocks when empty.
// Uses sync.Cond for producer-consumer semantics.
type RingBuffer[T any] struct {
	buf      []T
	size     int
	r        int
	w        int
	count    int
	mu       sync.Mutex
	notFull  *sync.Cond
	notEmpty *sync.Cond
}

// NewRingBuffer creates a new RingBuffer with the given fixed size.
func NewRingBuffer[T any](size int) *RingBuffer[T] {
	rb := &RingBuffer[T]{
		buf:  make([]T, size),
		size: size,
	}
	rb.notFull = sync.NewCond(&rb.mu)
	rb.notEmpty = sync.NewCond(&rb.mu)
	return rb
}

// EnQueue adds an item. Blocks if the buffer is full.
func (b *RingBuffer[T]) EnQueue(x T) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for b.count == b.size {
		b.notFull.Wait()
	}

	b.buf[b.w] = x
	b.w++
	if b.w >= b.size {
		b.w = 0
	}
	b.count++
	b.notEmpty.Signal()
}

// DeQueue removes and returns an item. Blocks if the buffer is empty.
func (b *RingBuffer[T]) DeQueue() T {
	b.mu.Lock()
	defer b.mu.Unlock()

	for b.count == 0 {
		b.notEmpty.Wait()
	}

	val := b.buf[b.r]
	var zero T
	b.buf[b.r] = zero // Help GC
	b.r++
	if b.r >= b.size {
		b.r = 0
	}
	b.count--
	b.notFull.Signal()
	return val
}

// EnQueueMany adds multiple items. Blocks until enough space is available.
func (b *RingBuffer[T]) EnQueueMany(x []T) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for b.count+len(x) > b.size {
		b.notFull.Wait()
	}

	for _, item := range x {
		b.buf[b.w] = item
		b.w++
		if b.w >= b.size {
			b.w = 0
		}
	}
	b.count += len(x)
	b.notEmpty.Broadcast()
}

// DeQueueMany removes multiple items into dst. Blocks until enough items are available.
func (b *RingBuffer[T]) DeQueueMany(dst []T) {
	b.mu.Lock()
	defer b.mu.Unlock()

	length := len(dst)
	for b.count < length {
		b.notEmpty.Wait()
	}

	for i := 0; i < length; i++ {
		dst[i] = b.buf[b.r]
		var zero T
		b.buf[b.r] = zero
		b.r++
		if b.r >= b.size {
			b.r = 0
		}
	}
	b.count -= length
	b.notFull.Broadcast()
}
