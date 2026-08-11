package conc

import (
	"context"
	"sync"
	"sync/atomic"
)

// Semaphore is a counting, resizable semaphore based on compare-and-swap.
//
// It packs limit (high 32 bits) and count (low 32 bits) into a single uint64
// so that limit and count can be updated atomically together. Blocked waiters
// are woken via a broadcast channel that is closed on Release/SetLimit.
//
// Acquire blocks until n entries can be reserved, the context is cancelled, or
// the semaphore is stopped. TryAcquire is non-blocking. Release returns the
// previous count. SetLimit grows or shrinks the capacity concurrently.
//
// A goroutine may enter the semaphore multiple times by calling Acquire or
// TryAcquire repeatedly; it must call Release the same number of times to
// fully exit. There is no guaranteed wake order (neither FIFO nor LIFO).
//
// All methods are safe for concurrent use. A Semaphore is not reusable after
// Stop: subsequent Acquire/TryAcquire return ErrSemaphoreStopped.
type Semaphore struct {
	// state packs limit (high 32 bits) and count (low 32 bits).
	state atomic.Uint64

	// broadcast fields. broadcastCh is closed (and replaced) by Release and
	// SetLimit to wake all blocked Acquire callers. Lock guards the channel
	// swap so a waiter reads a consistent, current channel.
	lock        sync.RWMutex
	broadcastCh chan struct{}

	// stopped is set by Stop. Once true, Acquire/TryAcquire refuse new
	// entries and return ErrSemaphoreStopped.
	stopped atomic.Bool
}

// ErrSemaphoreStopped is returned by Acquire/TryAcquire after Stop is called.
var ErrSemaphoreStopped = errSemaphore("semaphore: stopped")

type errSemaphore string

func (e errSemaphore) Error() string { return string(e) }

// NewSemaphore creates a Semaphore with the given limit. Panics if limit <= 0.
// The semaphore is usable immediately; Acquire blocks until capacity is
// available.
func NewSemaphore(limit int) *Semaphore {
	if limit <= 0 {
		panic("semaphore: limit must be greater than 0")
	}
	s := &Semaphore{broadcastCh: make(chan struct{})}
	s.state.Store(uint64(limit) << 32)
	return s
}

// Acquire reserves n entries, blocking until space is available or ctx is
// cancelled. A nil ctx is treated as context.Background().
//
// Returns nil on success, ctx.Err() if ctx is cancelled before the semaphore
// can be acquired, or ErrSemaphoreStopped if Stop has been called. Note that
// cancellation is best-effort: if Release runs concurrently and frees space,
// Acquire may still succeed instead of returning ctx.Err().
//
// Panics if n <= 0.
func (s *Semaphore) Acquire(ctx context.Context, n int) error {
	if n <= 0 {
		panic("semaphore: n must be positive number")
	}

	var ctxDoneCh <-chan struct{}
	if ctx != nil {
		ctxDoneCh = ctx.Done()
	}

	for {
		// Cheap check: stopped semaphores refuse new entries.
		if s.stopped.Load() {
			return ErrSemaphoreStopped
		}
		// Cheap check: context already cancelled.
		select {
		case <-ctxDoneCh:
			return ctx.Err()
		default:
		}

		state := s.state.Load()
		count := state & 0xFFFFFFFF
		limit := state >> 32
		newCount := count + uint64(n)

		if newCount <= limit {
			// Fast path: space available, CAS the new count in.
			if s.state.CompareAndSwap(state, limit<<32+newCount) {
				return nil
			}
			// Lost the race; retry.
			continue
		}

		// Slow path: no space. Wait on the broadcast channel.
		s.lock.RLock()
		broadcastCh := s.broadcastCh
		s.lock.RUnlock()

		// Re-check state to avoid missing a Release between Load and
		// reading the channel. If state changed, retry the fast path.
		if s.state.Load() != state {
			continue
		}
		// Check stop again so Stop wakes a blocked waiter.
		if s.stopped.Load() {
			return ErrSemaphoreStopped
		}

		select {
		case <-ctxDoneCh:
			return ctx.Err()
		case <-broadcastCh:
			// Capacity may have changed; loop to re-check.
		}
	}
}

// TryAcquire reserves n entries without blocking. Returns true on success,
// false if the semaphore is full, stopped, or n would overflow the limit.
//
// Panics if n <= 0.
func (s *Semaphore) TryAcquire(n int) bool {
	if n <= 0 {
		panic("semaphore: n must be positive number")
	}
	if s.stopped.Load() {
		return false
	}
	for {
		state := s.state.Load()
		count := state & 0xFFFFFFFF
		limit := state >> 32
		newCount := count + uint64(n)

		if newCount > limit {
			return false
		}
		if s.state.CompareAndSwap(state, limit<<32+newCount) {
			return true
		}
	}
}

// Release decrements the count by n and returns the previous count. Wakes all
// blocked Acquire callers via broadcast.
//
// Panics if n <= 0 or n exceeds the current count (release-without-acquire).
func (s *Semaphore) Release(n int) int {
	if n <= 0 {
		panic("semaphore: n must be positive number")
	}
	for {
		state := s.state.Load()
		count := state & 0xFFFFFFFF
		if count < uint64(n) {
			panic("semaphore: release without acquire")
		}
		newCount := count - uint64(n)
		if s.state.CompareAndSwap(state, state&0xFFFFFFFF00000000+newCount) {
			s.broadcast()
			return int(count)
		}
	}
}

// SetLimit changes the capacity to limit. Wakes all blocked Acquire callers so
// they can re-evaluate against the new limit. Panics if limit <= 0.
//
// If limit is lower than the current count, existing entries are not
// revoked: subsequent Acquire calls will block until count drops below the
// new limit.
func (s *Semaphore) SetLimit(limit int) {
	if limit <= 0 {
		panic("semaphore: limit must be greater than 0")
	}
	for {
		state := s.state.Load()
		newState := uint64(limit)<<32 + state&0xFFFFFFFF
		if s.state.CompareAndSwap(state, newState) {
			s.broadcast()
			return
		}
	}
}

// GetLimit returns the current limit.
func (s *Semaphore) GetLimit() int {
	state := s.state.Load()
	return int(state >> 32)
}

// GetCount returns the current number of occupied entries.
func (s *Semaphore) GetCount() int {
	state := s.state.Load()
	return int(state & 0xFFFFFFFF)
}

// Stop marks the semaphore as stopped and wakes all blocked Acquire callers.
// Subsequent Acquire/TryAcquire return ErrSemaphoreStopped. Stop is terminal.
//
// Stop does not release currently-held entries; existing owners must still
// call Release to keep the count consistent.
func (s *Semaphore) Stop() {
	if s.stopped.CompareAndSwap(false, true) {
		s.broadcast()
	}
}

// broadcast closes the current broadcastCh and replaces it with a fresh one.
// Closing the old channel wakes all waiters blocked on <-broadcastCh; new
// waiters will read the new (open) channel and retry via the for loop.
func (s *Semaphore) broadcast() {
	s.lock.Lock()
	old := s.broadcastCh
	s.broadcastCh = make(chan struct{})
	s.lock.Unlock()
	close(old)
}
