// Package coll provides generic, thread-safe collection data structures
// that are missing from the standard library:
//
//   - Deque[T]:       generic double-ended queue (ring buffer, auto-resize)
//   - Hset[T]/Set[T]: thread-safe generic set backed by a hash map
//   - RingBuffer[T]:  blocking bounded ring buffer (producer-consumer)
//   - Stack[T]:       lock-free Treiber stack
//   - LinkedQueue[T]: lock-free unbounded FIFO queue (Michael-Scott)
//
// These collections prioritize correctness, minimal allocations, and
// ergonomic generics. The Deque uses power-of-2 sizing for O(1) bitwise
// modulus; the Stack and LinkedQueue use CAS-based lock-free operations;
// the RingBuffer uses sync.Cond for blocking enqueue/dequeue semantics.
package coll
