// Package syncx provides concurrency-safe synchronization primitives:
//
//   - ConcurrentMap: a sharded thread-safe map (FNV-1 hashing, generic K/V)
//   - SafeMap: a single-shard RWMutex-protected map (building block for ConcurrentMap)
//   - Closer: a stateful close signal carrying an error and optional close hook
//   - Goroutine: a goroutine group with active/total counters and Wait/Stop
//   - Use / Once / Bool: small CAS-based state helpers
//   - SpinLock: a CAS-based spinlock for very hot critical sections
//     (prefer sync.Mutex unless profiling proves otherwise)
//   - FSM: a finite state machine with guards, actions, per-state handlers,
//     optional background condition polling, and change callbacks
//
// The package depends only on the Go standard library.
package syncx
