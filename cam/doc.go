// Package cam is the parent of cog's concurrency, async, and meta-structure
// library. It contains no code itself; each subpackage is an independent
// module of focused primitives.
//
// Subpackage layout:
//
//   - atom:    atomic wrappers around numeric and boolean primitives with
//              convenience helpers (Sub/Inc/Dec, Toggle, JSON, noCopy guard).
//   - bus:     a synchronous in-process event bus with typed topics.
//   - coll:    generic, mostly lock-free collection types — Deque[T],
//              RingBuffer[T] (blocking bounded), Stack[T] (Treiber CAS),
//              Set[T] / Hset[T] (hash-set).
//   - conc:    concurrency primitives — Queue (mutex FIFO), QueueLf (lock-free
//              ring with cond signaling), WorkerPool (bounded pool with stats
//              and Submit/SubmitWithTimeout), Batch (size/time-based flusher).
//   - event:   a typed publish/subscribe hub with timeout-aware handlers,
//              drop+warn semantics on backpressure, and deduplication.
//   - idgen:   distributed-friendly ID generators — Snowflake (41b ts / 10b
//              machine / 12b seq) and Mist (47b counter + 16b random).
//   - num:     numeric helpers — Bytes (IEC/SI size parsing), Decimal (fixed
//              point), Range (interval arithmetic).
//   - pipeline: a staged processing pipeline with context cancellation.
//   - resil:   resilience patterns — Retry / Do (config- or option-driven,
//              with Unrecoverable + RetryIf predicates), Backoff (exponential
//              with jitter), CircuitBreaker (Closed/Open/HalfOpen), RateLimiter
//              (token bucket), Fallback (failover to a backup resource).
//              Orthogonal and composable: CircuitBreaker(Fallback(Retry(fn))).
//   - safe:    safe execution helpers — Recover (deferred panic→error),
//              RecoverFunc, Try (panic-catching runner), OneRun (once-only
//              execution), Rerun (restartable goroutine), Runner (managed
//              goroutine lifecycle).
//   - sched:   interval-based in-process task scheduler with StartAfter,
//              RunOnce, RunSingleInstance (skip-overlap), ErrFunc and panic
//              recovery. Complements the top-level cron-based cog/sched.
//   - syncx:   sync extensions — ConcurrentMap (sharded generic map), SafeMap
//              (single-shard building block), Closer (stateful close signal
//              with error/hook), Goroutine (countered goroutine group),
//              Use / Once / Bool (small CAS state helpers), SpinLock
//              (CAS-based spinlock for very hot critical sections).
//
// Design principles:
//
//   - Prefer generics over interface{} for type safety and to eliminate
//     runtime type assertions.
//   - Prefer lock-free algorithms (CAS, atomic.Pointer) for hot paths; fall
//     back to sync.Mutex / sync.Cond only when blocking semantics are needed.
//   - Each subpackage depends only on the standard library and (optionally)
//     on sibling cam subpackages, never on the rest of cog.
//   - APIs favor composable options (Option pattern) and explicit context
//     cancellation.
//
// The cam package has no exported symbols; import a subpackage directly.
package cam
