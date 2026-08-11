package syncx

import (
	"encoding/json"
	"fmt"
	"sync"
)

// defaultShards is the shard count used by NewMap / NewStringerMap.
// It is a power of two so the modulo can be computed via bitmask if desired.
const defaultShards = 32

// Stringer constrains keys that implement fmt.Stringer and are comparable.
// Used by NewStringerMap to hash via the string representation.
type Stringer interface {
	fmt.Stringer
	comparable
}

// ShardingFunc computes a 32-bit hash for a key. The hash is mod-reduced by
// the shard count to select a shard.
type ShardingFunc[K comparable] func(key K) uint32

// ConcurrentMap is a "thread" safe map divided into several shards to reduce
// lock contention. The zero value is not usable; construct via NewMap or
// NewMapWithShards.
type ConcurrentMap[K comparable, V any] struct {
	sharding ShardingFunc[K]
	shards   []SafeMap[K, V]
}

// NewMap creates a ConcurrentMap[string, V] using FNV-1 hashing with the
// default shard count.
func NewMap[V any]() ConcurrentMap[string, V] {
	return NewMapWithShards[string, V](defaultShards, fnv32)
}

// NewStringerMap creates a ConcurrentMap[K, V] for keys implementing Stringer.
// Hashing uses the key's String() representation.
func NewStringerMap[K Stringer, V any]() ConcurrentMap[K, V] {
	return NewMapWithShards[K, V](defaultShards, strfnv32[K])
}

// NewMapWithCustom creates a ConcurrentMap with a caller-provided sharding
// function and the default shard count.
func NewMapWithCustom[K comparable, V any](sharding ShardingFunc[K]) ConcurrentMap[K, V] {
	return NewMapWithShards[K, V](defaultShards, sharding)
}

// NewMapWithShards creates a ConcurrentMap with an explicit shard count and
// sharding function. shards must be positive; a non-positive value falls back
// to defaultShards.
func NewMapWithShards[K comparable, V any](shards int, sharding ShardingFunc[K]) ConcurrentMap[K, V] {
	if shards <= 0 {
		shards = defaultShards
	}
	m := ConcurrentMap[K, V]{
		sharding: sharding,
		shards:   make([]SafeMap[K, V], shards),
	}
	for i := range m.shards {
		m.shards[i] = NewSafeMap[K, V]()
	}
	return m
}

// shardCount returns the number of shards.
func (m ConcurrentMap[K, V]) shardCount() int { return len(m.shards) }

// GetShard returns the shard responsible for the given key.
func (m ConcurrentMap[K, V]) GetShard(key K) SafeMap[K, V] {
	return m.shards[uint(m.sharding(key))%uint(m.shardCount())]
}

// MSet sets all key/value pairs from data.
func (m ConcurrentMap[K, V]) MSet(data map[K]V) {
	for key, value := range data {
		m.GetShard(key).Set(key, value)
	}
}

// Set sets the given value under the specified key.
func (m ConcurrentMap[K, V]) Set(key K, value V) {
	m.GetShard(key).Set(key, value)
}

// UpsertCb computes the new value from the existing value (if any).
type UpsertCb[V any] func(oldValue V, exist bool) V

// Upsert inserts or updates a value using cb. Returns the value returned by cb.
func (m ConcurrentMap[K, V]) Upsert(key K, cb UpsertCb[V]) (result V) {
	shard := m.GetShard(key)
	shard.Update(func(inner map[K]V) {
		v, exist := inner[key]
		result = cb(v, exist)
		inner[key] = result
	})
	return
}

// SetIfAbsent sets value only if no value was associated with key. Returns true
// if the value was set.
func (m ConcurrentMap[K, V]) SetIfAbsent(key K, value V) (ok bool) {
	shard := m.GetShard(key)
	shard.Update(func(inner map[K]V) {
		_, ok = inner[key]
		if !ok {
			inner[key] = value
		}
	})
	return !ok
}

// SetIfExists sets value only if a value was already associated with key.
// Returns true if the value was updated.
func (m ConcurrentMap[K, V]) SetIfExists(key K, value V) (ok bool) {
	shard := m.GetShard(key)
	shard.Update(func(inner map[K]V) {
		_, ok = inner[key]
		if ok {
			inner[key] = value
		}
	})
	return ok
}

// Get retrieves the value under key.
func (m ConcurrentMap[K, V]) Get(key K) (V, bool) {
	return m.GetShard(key).Get(key)
}

// InsertCb produces a new value for insertion.
type InsertCb[V any] func() V

// GetOrInsert returns the existing value for key, or inserts the value
// produced by cb if key is absent. The cb is called at most once.
func (m ConcurrentMap[K, V]) GetOrInsert(key K, cb InsertCb[V]) V {
	shard := m.GetShard(key)
	if v, exist := shard.Get(key); exist {
		return v
	}
	var v V
	shard.Update(func(inner map[K]V) {
		// Assign to the outer v; do not shadow with := which would lose the
		// result and leave the outer v as the zero value.
		existing, exist := inner[key]
		if exist {
			v = existing
			return
		}
		v = cb()
		inner[key] = v
	})
	return v
}

// GetCb invokes cb with the value and existence flag under the shard's read
// lock.
type GetCb[V any] func(value V, exists bool)

func (m ConcurrentMap[K, V]) GetCb(key K, cb GetCb[V]) {
	m.GetShard(key).GetCb(key, cb)
}

// Count returns the total number of elements across all shards.
func (m ConcurrentMap[K, V]) Count() int {
	count := 0
	for i := range m.shards {
		count += m.shards[i].Count()
	}
	return count
}

// Has reports whether key is present.
func (m ConcurrentMap[K, V]) Has(key K) bool {
	_, ok := m.GetShard(key).Get(key)
	return ok
}

// Remove removes the element under key.
func (m ConcurrentMap[K, V]) Remove(key K) {
	m.GetShard(key).Del(key)
}

// RemoveCb invokes cb under the shard's write lock. If cb returns true and the
// key exists, the element is removed. Returns whether an element was removed.
type RemoveCb[V any] func(value V, exists bool) bool

func (m ConcurrentMap[K, V]) RemoveCb(key K, cb RemoveCb[V]) (ok bool) {
	shard := m.GetShard(key)
	shard.Update(func(inner map[K]V) {
		v, exist := inner[key]
		result := cb(v, exist)
		ok = exist && result
		if ok {
			delete(inner, key)
		}
	})
	return
}

// Pop removes and returns the element under key.
func (m ConcurrentMap[K, V]) Pop(key K) (value V, exists bool) {
	shard := m.GetShard(key)
	shard.Update(func(inner map[K]V) {
		value, exists = inner[key]
		delete(inner, key)
	})
	return
}

// IsEmpty reports whether the map contains no elements.
func (m ConcurrentMap[K, V]) IsEmpty() bool { return m.Count() == 0 }

// Tuple is a key/value pair yielded by iterators.
type Tuple[K comparable, V any] struct {
	Key K
	Val V
}

// IterBuffered returns a channel of all key/value pairs. Iteration runs in a
// background goroutine over a point-in-time snapshot of each shard; the
// channel is closed when iteration completes. Callers should drain the channel
// to avoid leaking the goroutine.
func (m ConcurrentMap[K, V]) IterBuffered() <-chan Tuple[K, V] {
	ch := make(chan Tuple[K, V], 1024)
	go fanIn(m.snapshot(), ch)
	return ch
}

// Clear removes all items from every shard.
func (m ConcurrentMap[K, V]) Clear() {
	for i := range m.shards {
		m.shards[i].Clear()
	}
}

func (m ConcurrentMap[K, V]) snapshot() []map[K]V {
	list := make([]map[K]V, 0, len(m.shards))
	for i := range m.shards {
		list = append(list, m.shards[i].Clone())
	}
	return list
}

func fanIn[K comparable, V any](shards []map[K]V, ch chan Tuple[K, V]) {
	var wg sync.WaitGroup
	for i := range shards {
		wg.Add(1)
		go func(s map[K]V) {
			defer wg.Done()
			for k, v := range s {
				ch <- Tuple[K, V]{k, v}
			}
		}(shards[i])
	}
	wg.Wait()
	close(ch)
}

// Items returns a snapshot of all key/value pairs as a plain map.
func (m ConcurrentMap[K, V]) Items() map[K]V {
	tmp := make(map[K]V)
	for i := range m.shards {
		m.shards[i].View(func(key K, val V) {
			tmp[key] = val
		})
	}
	return tmp
}

// IterCb invokes fn for every key/value pair. The shard RLock is held during
// each call, so fn sees a consistent view of a single shard but not across
// shards.
type IterCb[K comparable, V any] func(key K, v V)

func (m ConcurrentMap[K, V]) IterCb(fn IterCb[K, V]) {
	for i := range m.shards {
		m.shards[i].View(fn)
	}
}

// Keys returns all keys. Order is unspecified.
func (m ConcurrentMap[K, V]) Keys() []K {
	keys := make([]K, 0, m.Count())
	for item := range m.IterBuffered() {
		keys = append(keys, item.Key)
	}
	return keys
}

// Values returns all values. Order is unspecified.
func (m ConcurrentMap[K, V]) Values() []V {
	values := make([]V, 0, m.Count())
	for item := range m.IterBuffered() {
		values = append(values, item.Val)
	}
	return values
}

// MarshalJSON renders the map as a JSON object.
func (m ConcurrentMap[K, V]) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.Items())
}

// UnmarshalJSON populates the map from a JSON object. Existing entries are
// not cleared.
func (m *ConcurrentMap[K, V]) UnmarshalJSON(b []byte) error {
	tmp := make(map[K]V)
	if err := json.Unmarshal(b, &tmp); err != nil {
		return err
	}
	for key, val := range tmp {
		m.Set(key, val)
	}
	return nil
}

// fnv32 implements FNV-1 (multiply-then-xor) 32-bit hashing, matching the
// standard library's hash/fnv New32. Note: this is NOT FNV-1a (New32a), which
// XORs first.
func fnv32(key string) uint32 {
	const prime32 = uint32(16777619)
	hash := uint32(2166136261)
	for i := 0; i < len(key); i++ {
		hash *= prime32
		hash ^= uint32(key[i])
	}
	return hash
}

func strfnv32[K fmt.Stringer](key K) uint32 {
	return fnv32(key.String())
}
