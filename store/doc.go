// Package store provides persistence and caching:
//   - DbLite: embedded key-value store with JSON persistence
//   - SegmentedWAL: segment-based write-ahead log with rotation, background
//     sync/merge, crash recovery, and in-memory cache
//   - Cache: in-memory cache with TTL, eviction callback, and janitor cleanup
package store
