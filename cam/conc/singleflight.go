package conc

import (
	"fmt"
	"sync"
)

// Singleflight deduplicates concurrent calls by key. When multiple goroutines
// call Do with the same key simultaneously, only the first invokes fn; the
// others block until it completes and receive the same result.
//
// Singleflight is useful for cache miss protection: if N goroutines all miss
// the cache for the same key, only one fetches from the backing store while
// the others wait for the in-flight result.
//
// The zero value is ready to use. Singleflight is safe for concurrent use by
// multiple goroutines.
type Singleflight[K comparable, V any] struct {
	mu sync.Mutex
	m  map[K]*sfCall[V]
}

// sfCall represents an in-flight (or completed) Do invocation.
// Waiters block on wg; val/err are published before wg.Done so any goroutine
// that observes wg.Wait() returning is guaranteed to see the final values.
type sfCall[V any] struct {
	wg  sync.WaitGroup
	val V
	err error
}

// PanicError is returned to all callers when fn panics. Converting the panic
// to an error ensures joined callers are released instead of waiting forever.
type PanicError struct {
	Value any
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("singleflight: function panicked: %v", e.Value)
}

// Do executes fn(key) exactly once for concurrent callers sharing key.
// If another goroutine is already running fn(key), Do blocks until it
// completes and returns the same (val, err). fn is invoked at most once per
// outstanding key; once fn returns and the entry is cleaned up, a subsequent
// Do(key, ...) will invoke fn again.
//
// The returned values are shared without copying. Callers must not mutate V
// if it is a reference type (slice, map, pointer) unless the type is otherwise
// synchronized.
func (s *Singleflight[K, V]) Do(key K, fn func() (V, error)) (V, error) {
	s.mu.Lock()
	if s.m == nil {
		s.m = make(map[K]*sfCall[V])
	}
	if c, ok := s.m[key]; ok {
		// Another caller is running fn(key). Wait for it.
		s.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}
	c := &sfCall[V]{}
	c.wg.Add(1)
	s.m[key] = c
	s.mu.Unlock()

	// Publish the result before signaling waiters. sync.WaitGroup's
	// happens-before guarantee (Wait returning happens-after Done) ensures
	// blocked callers see the final val/err. The deferred completion path also
	// releases waiters and cleans up when fn panics.
	func() {
		defer func() {
			if r := recover(); r != nil {
				c.err = &PanicError{Value: r}
			}
			c.wg.Done()

			// Only delete our own entry. If Forget was called during fn and a new
			// Do created a fresh entry for the same key, deleting unconditionally
			// would discard that new entry (classic golang.org/x/sync/singleflight
			// Forget race). Compare the pointer to ensure we only remove ours.
			s.mu.Lock()
			if s.m[key] == c {
				delete(s.m, key)
			}
			s.mu.Unlock()
		}()
		c.val, c.err = fn()
	}()

	return c.val, c.err
}

// Forget removes the in-flight entry for key, if any. Subsequent Do(key, ...)
// calls will start a new fn invocation rather than joining the current one.
// An in-flight fn is NOT cancelled; if it is still running it will complete
// and its result is discarded (no waiters remain joined to it).
//
// This is useful when the in-flight call is suspected to be stuck or its
// result is no longer relevant: new callers get a fresh invocation while the
// old one finishes in the background.
func (s *Singleflight[K, V]) Forget(key K) {
	s.mu.Lock()
	delete(s.m, key)
	s.mu.Unlock()
}
