package syncx

import (
	"encoding/json"
	"maps"
	"sync"
)

// SafeMap is a single-shard RWMutex-protected map. It is the building block
// for ConcurrentMap but can also be used directly when a single shard is
// acceptable. The zero value is not usable; construct via NewSafeMap.
type SafeMap[K comparable, V any] struct {
	m   map[K]V
	mux *sync.RWMutex
}

// NewSafeMap creates an empty SafeMap.
func NewSafeMap[K comparable, V any]() SafeMap[K, V] {
	return SafeMap[K, V]{
		m:   make(map[K]V),
		mux: &sync.RWMutex{},
	}
}

// View invokes fn for every key/value pair under the read lock.
func (s SafeMap[K, V]) View(fn func(K, V)) {
	s.mux.RLock()
	defer s.mux.RUnlock()
	for k, v := range s.m {
		fn(k, v)
	}
}

// Clone returns a shallow copy of the underlying map taken under the read
// lock.
func (s SafeMap[K, V]) Clone() map[K]V {
	s.mux.RLock()
	defer s.mux.RUnlock()
	return maps.Clone(s.m)
}

// Find invokes fn for each key in keys (when present) under the read lock.
// fn receives the key, the value (zero value if absent), and an existence
// flag.
func (s SafeMap[K, V]) Find(fn func(key K, value V, exist bool), keys ...K) {
	s.mux.RLock()
	defer s.mux.RUnlock()
	for _, k := range keys {
		v, ok := s.m[k]
		fn(k, v, ok)
	}
}

// Count returns the number of elements.
func (s SafeMap[K, V]) Count() int {
	s.mux.RLock()
	defer s.mux.RUnlock()
	return len(s.m)
}

// Get returns the value and existence flag for key.
func (s SafeMap[K, V]) Get(key K) (V, bool) {
	s.mux.RLock()
	defer s.mux.RUnlock()
	v, ok := s.m[key]
	return v, ok
}

// GetCb invokes cb with the value and existence flag for key under the read
// lock. Useful for atomic read-then-act.
func (s SafeMap[K, V]) GetCb(key K, cb func(value V, exists bool)) {
	s.mux.RLock()
	defer s.mux.RUnlock()
	v, ok := s.m[key]
	cb(v, ok)
}

// Set sets key=value under the write lock.
func (s SafeMap[K, V]) Set(key K, value V) {
	s.mux.Lock()
	defer s.mux.Unlock()
	s.m[key] = value
}

// Del removes key under the write lock.
func (s SafeMap[K, V]) Del(key K) {
	s.mux.Lock()
	defer s.mux.Unlock()
	delete(s.m, key)
}

// Update invokes fn with the underlying map under the write lock, allowing
// arbitrary atomic mutations.
func (s SafeMap[K, V]) Update(fn func(map[K]V)) {
	s.mux.Lock()
	defer s.mux.Unlock()
	fn(s.m)
}

// MarshalJSON renders the map as JSON under the write lock.
func (s SafeMap[K, V]) MarshalJSON() ([]byte, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	return json.Marshal(s.m)
}

// UnmarshalJSON replaces the underlying map from JSON under the write lock.
func (s *SafeMap[K, V]) UnmarshalJSON(b []byte) error {
	s.mux.Lock()
	defer s.mux.Unlock()
	return json.Unmarshal(b, &s.m)
}

// Clear removes all items.
func (s SafeMap[K, V]) Clear() {
	s.mux.Lock()
	defer s.mux.Unlock()
	clear(s.m)
}
