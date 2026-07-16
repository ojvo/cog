// Package syncx provides concurrency-safe synchronization primitives:
//
//   - ConcurrentMap: a sharded thread-safe map (FNV-1 hashing, generic K/V)
//   - SafeMap: a single-shard RWMutex-protected map (building block for ConcurrentMap)
//   - Closer: a stateful close signal carrying an error and optional close hook
//   - Goroutine: a goroutine group with active/total counters and Wait/Stop
//   - Use / Once / Bool: small CAS-based state helpers
//
// The package depends only on the Go standard library.
package syncx
