package coll

import "sync/atomic"

type stackNode[T any] struct {
	value T
	next  *stackNode[T]
}

// Stack is a lock-free Treiber stack.
// All operations use atomic CAS, making it safe for concurrent use
// without locks.
type Stack[T any] struct {
	top atomic.Pointer[stackNode[T]]
	len int64
}

// NewStack creates a new empty Stack.
func NewStack[T any]() *Stack[T] {
	return new(Stack[T])
}

// Push adds an element to the top of the stack.
func (s *Stack[T]) Push(x T) {
	newHead := &stackNode[T]{value: x}
	for {
		oldHead := s.top.Load()
		newHead.next = oldHead
		if s.top.CompareAndSwap(oldHead, newHead) {
			break
		}
	}
	atomic.AddInt64(&s.len, 1)
}

// Pop removes and returns the top element.
// Returns false if the stack is empty.
func (s *Stack[T]) Pop() (T, bool) {
	var zero T
	for {
		oldHead := s.top.Load()
		if oldHead == nil {
			return zero, false
		}
		newHead := oldHead.next
		if s.top.CompareAndSwap(oldHead, newHead) {
			atomic.AddInt64(&s.len, -1)
			return oldHead.value, true
		}
	}
}

// Len returns the number of elements in the stack.
func (s *Stack[T]) Len() int64 {
	return atomic.LoadInt64(&s.len)
}
