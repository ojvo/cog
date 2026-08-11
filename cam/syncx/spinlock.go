package syncx

import (
	"runtime"
	"sync/atomic"
)

// SpinLock is a CAS-based spinlock that yields to the scheduler when
// contended. It implements sync.Locker.
//
// SpinLock is intended for very hot critical sections where the overhead of
// sync.Mutex (which includes a semaphore and a wait queue) is measurable. In
// almost all cases sync.Mutex is preferable: it is fairer (FIFO under
// contention), uses less CPU when blocked, and integrates with the Go
// scheduler's preemption points.
//
// Use SpinLock only when all of the following hold:
//   - The critical section is extremely short (a few ns, no I/O).
//   - Contention is low to moderate.
//   - Profiling shows sync.Mutex is a measurable bottleneck.
//
// SpinLock is NOT fair: under heavy contention some waiters may starve. It
// must not be held across blocking calls (I/O, channel ops, mutex.Lock) — that
// would waste CPU and risk priority inversion.
//
// The zero value is an unlocked SpinLock.
type SpinLock struct {
	lock uint32
}

// NewSpinLock returns an unlocked SpinLock.
func NewSpinLock() *SpinLock { return &SpinLock{} }

// Lock acquires the lock, spinning and yielding to the scheduler until it
// succeeds.
func (l *SpinLock) Lock() {
	for !atomic.CompareAndSwapUint32(&l.lock, 0, 1) {
		runtime.Gosched()
	}
}

// TryLock attempts to acquire the lock without waiting. Returns true if the
// lock was acquired.
func (l *SpinLock) TryLock() bool {
	return atomic.CompareAndSwapUint32(&l.lock, 0, 1)
}

// Unlock releases the lock. It is the caller's responsibility to ensure the
// lock is held; unlocking an unlocked SpinLock is a no-op here (no panic) but
// is a programming error.
func (l *SpinLock) Unlock() {
	atomic.StoreUint32(&l.lock, 0)
}
