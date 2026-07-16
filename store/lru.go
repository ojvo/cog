package store

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"
)

// LRUCache is a sharded, pre-allocated, concurrent LRU cache with optional
// LRU-2 promotion, lazy TTL expiration, and an inspectable action chain.
//
// Design highlights:
//
//   - Sharded: bucketCnt shards each with their own sync.Mutex, reducing lock
//     contention. bucketCnt is rounded up to a power of two so the shard index
//     is a cheap bitmask.
//   - Pre-allocated: each shard allocates its node array and doubly-linked-list
//     index array once at construction; no per-Put allocation, no GC pressure.
//   - LRU-2 (optional): a second-level cache per shard. Items accessed a second
//     time are promoted from level-0 to level-1, so frequently-accessed items
//     survive even when level-0 is churned by one-shot reads.
//   - Lazy expiration: items carry an expireAt nanosecond stamp. Expired items
//     are skipped on Get but not eagerly removed (avoids GC thrashing); they
//     are eventually evicted by LRU pressure.
//   - Inspector chain: call Inspect to append an observer that fires on every
//     Put / Get / Del (and on eviction). Multiple inspectors fire in
//     registration order.
//
// LRUCache is safe for concurrent use by multiple goroutines.
type LRUCache struct {
	locks      []sync.Mutex
	shards     [][2]*lruShard // [0] = level-0 LRU, [1] = level-1 LRU-2 (nil if not enabled)
	expiration time.Duration
	inspector  atomic.Value // Inspector
	inspectMu  sync.Mutex   // guards Inspect chain updates
	mask       int32
}

// Inspector observes cache actions. It is invoked on every Put, Get, and Del
// operation (and on LRU eviction during Put).
//
//   - action: one of ActPut, ActGet, ActDel.
//   - key: the cache key.
//   - iface: pointer to the stored interface value, or nil if the entry is
//     bytes-only. The pointer is for reading only; do not mutate.
//   - bytes: the stored byte slice, or nil if the entry is interface-only.
//   - status: operation-specific status. For ActPut: 1 = newly added,
//     0 = updated existing, -1 = evicted an old item to make room.
//     For ActGet: 1 = hit, 0 = miss. For ActDel: 1 = deleted, 0 = not found.
type Inspector func(action int, key string, iface *interface{}, bytes []byte, status int)

// Action codes passed to Inspector.
const (
	ActPut = iota + 1
	ActGet
	ActDel
)

// NewLRUCache creates a sharded LRU cache.
//
//   - bucketCnt: number of shards (rounded up to the next power of two).
//     More shards reduce lock contention but use more memory.
//   - capPerBkt: capacity of each shard. Total capacity is bucketCnt * capPerBkt.
//   - expiration: optional item lifetime. 0 (or omitted) means no expiration.
//     Expiration is lazy: expired items are skipped on read and evicted by
//     LRU pressure rather than by a background goroutine.
func NewLRUCache(bucketCnt, capPerBkt uint16, expiration ...time.Duration) *LRUCache {
	mask := maskOfNextPowOf2(bucketCnt)
	c := &LRUCache{
		locks:  make([]sync.Mutex, mask+1),
		shards: make([][2]*lruShard, mask+1),
		mask:   int32(mask),
	}
	c.inspector.Store(Inspector(func(int, string, *interface{}, []byte, int) {}))
	for i := range c.shards {
		c.shards[i][0] = newLRUShard(capPerBkt)
	}
	if len(expiration) > 0 {
		c.expiration = expiration[0]
	}
	return c
}

// LRU2 enables two-level LRU for the cache. Items accessed a second time are
// promoted from level-0 to level-1, protecting frequently-accessed items from
// being evicted by one-shot reads.
//
// capPerBkt is the capacity of each level-1 shard, adding capPerBkt * bucketCnt
// extra slots. Must be called before the cache is used.
func (c *LRUCache) LRU2(capPerBkt uint16) *LRUCache {
	for i := range c.shards {
		c.shards[i][1] = newLRUShard(capPerBkt)
	}
	return c
}

// Inspect appends an inspector to the observation chain. Inspectors fire in
// registration order (oldest first). Safe to call concurrently, but should be
// called during setup, not on every operation.
func (c *LRUCache) Inspect(fn Inspector) {
	c.inspectMu.Lock()
	defer c.inspectMu.Unlock()
	old := c.inspector.Load().(Inspector)
	c.inspector.Store(Inspector(func(action int, key string, iface *interface{}, bytes []byte, status int) {
		old(action, key, iface, bytes, status)
		fn(action, key, iface, bytes, status)
	}))
}

// Put stores val under key, replacing any existing entry.
func (c *LRUCache) Put(key string, val interface{}) { c.put(key, &val, nil) }

// PutInt64 stores a 64-bit integer under key as a compact 8-byte little-endian
// value, avoiding interface boxing.
func (c *LRUCache) PutInt64(key string, d int64) {
	var data [8]byte
	binary.LittleEndian.PutUint64(data[:], uint64(d))
	c.put(key, nil, data[:])
}

// PutBytes stores a byte slice under key without copying; the caller must not
// modify the slice after storing it.
func (c *LRUCache) PutBytes(key string, b []byte) { c.put(key, nil, b) }

// BytesToInt64 decodes a little-endian 8-byte slice (as stored by PutInt64 or
// returned by GetBytes) into an int64. Returns false if the slice is shorter
// than 8 bytes.
func BytesToInt64(b []byte) (int64, bool) {
	if len(b) >= 8 {
		return int64(binary.LittleEndian.Uint64(b)), true
	}
	return 0, false
}

// Get returns the interface value stored under key, or nil, false if absent
// (or expired).
func (c *LRUCache) Get(key string) (interface{}, bool) {
	if i, _, ok := c.get(key); ok && i != nil {
		return *i, true
	}
	return nil, false
}

// GetBytes returns the byte slice stored under key, or nil, false if absent.
func (c *LRUCache) GetBytes(key string) ([]byte, bool) {
	if _, b, ok := c.get(key); ok {
		return b, true
	}
	return nil, false
}

// GetInt64 returns the 64-bit integer stored under key (via PutInt64), or
// 0, false if absent.
func (c *LRUCache) GetInt64(key string) (int64, bool) {
	if _, b, ok := c.get(key); ok && len(b) >= 8 {
		return int64(binary.LittleEndian.Uint64(b)), true
	}
	return 0, false
}

// Del removes the entry under key. If LRU-2 is enabled, deletes from both
// levels.
func (c *LRUCache) Del(key string) {
	idx := hashBKRD(key) & c.mask
	on := c.inspector.Load().(Inspector)
	c.locks[idx].Lock()
	n, s, e := c.shards[idx][0].del(key)
	if c.shards[idx][1] != nil {
		if n2, s2, e2 := c.shards[idx][1].del(key); n2 != nil && (n == nil || e < e2) {
			n, s = n2, s2
		}
	}
	if s > 0 {
		on(ActDel, key, n.v.i, n.v.b, 1)
		n.v.i, n.v.b = nil, nil // release references
	} else {
		on(ActDel, key, nil, nil, 0)
	}
	c.locks[idx].Unlock()
}

// Walk calls walker sequentially for every valid (non-deleted, non-expired)
// item across all shards. If walker returns false, iteration stops immediately.
// Walk holds shard locks during iteration, so keep walker fast.
func (c *LRUCache) Walk(walker func(key string, iface *interface{}, bytes []byte, expireAt int64) bool) {
	for i := range c.shards {
		c.locks[i].Lock()
		if c.shards[i][0].walk(walker) {
			if c.shards[i][1] != nil {
				c.shards[i][1].walk(walker)
			}
		}
		c.locks[i].Unlock()
	}
}

// --- internal ---

func (c *LRUCache) put(key string, i *interface{}, b []byte) {
	idx := hashBKRD(key) & c.mask
	on := c.inspector.Load().(Inspector)
	// expireAt is always positive for live entries: time.Now().UnixNano() is
	// the creation stamp when expiration is 0, or the expiry stamp otherwise.
	// A value of 0 is reserved as a "deleted" tombstone (see lruShard.del).
	expireAt := time.Now().UnixNano()
	if c.expiration > 0 {
		expireAt += int64(c.expiration)
	}
	c.locks[idx].Lock()
	status := c.shards[idx][0].put(key, i, b, expireAt, on)
	c.locks[idx].Unlock()
	on(ActPut, key, i, b, status)
}

func (c *LRUCache) get(key string) (i *interface{}, b []byte, _ bool) {
	idx := hashBKRD(key) & c.mask
	on := c.inspector.Load().(Inspector)
	now := time.Now().UnixNano()
	c.locks[idx].Lock()
	n, s := (*lruNode)(nil), 0
	if c.shards[idx][1] == nil {
		// normal LRU mode
		n, s = c.tryGet(key, idx, 0, now)
	} else {
		// LRU-2 mode: try to remove from level-0; if found, promote to level-1.
		// If not in level-0, look in level-1.
		e := int64(0)
		if n, s, e = c.shards[idx][0].del(key); s > 0 {
			c.shards[idx][1].put(key, n.v.i, n.v.b, e, on)
		} else {
			n, s = c.tryGet(key, idx, 1, now)
		}
	}
	if s <= 0 {
		c.locks[idx].Unlock()
		on(ActGet, key, nil, nil, 0)
		return
	}
	i, b = n.v.i, n.v.b
	c.locks[idx].Unlock()
	on(ActGet, key, i, b, 1)
	return i, b, true
}

// tryGet returns the node at the given level if it exists and is not expired.
func (c *LRUCache) tryGet(key string, idx, level int32, now int64) (*lruNode, int) {
	if n, s := c.shards[idx][level].get(key); s > 0 && n.expireAt > 0 && (c.expiration <= 0 || now < n.expireAt) {
		return n, s
	}
	return nil, 0
}

// --- shard ---

const (
	lruPrev = uint16(0)
	lruNext = uint16(1)
)

type lruValue struct {
	i *interface{} // interface pointer; nil if bytes-only
	b []byte       // bytes value; nil if interface-only
}

type lruNode struct {
	k        string
	v        lruValue
	expireAt int64 // nano timestamp; 0 if marked as deleted. createdAt = expireAt - expiration
}

// lruShard is a single-shard LRU with pre-allocated memory.
//
// Indexing: links[0] is the sentinel head/tail. links[i] for i >= 1 are real
// nodes. items is 0-indexed, so items[x-1] corresponds to links[x]. last
// grows from 0 to cap(items); once full, evictions recycle the tail slot.
type lruShard struct {
	links   [][2]uint16       // doubly-linked list: [prev, next]
	items   []lruNode         // pre-allocated node pool
	indices map[string]uint16 // key -> 1-based index
	last    uint16            // highest allocated index (0 = empty)
}

func newLRUShard(cap uint16) *lruShard {
	return &lruShard{
		links:   make([][2]uint16, uint32(cap)+1),
		items:   make([]lruNode, cap),
		indices: make(map[string]uint16, cap),
	}
}

// put inserts or updates an entry. Returns 1 if newly added, 0 if updated.
// If the shard is full, evicts the tail (LRU) item and calls on with ActPut,
// status -1 before recycling the slot.
func (s *lruShard) put(k string, i *interface{}, b []byte, expireAt int64, on Inspector) int {
	if x, ok := s.indices[k]; ok {
		s.items[x-1].v.i, s.items[x-1].v.b, s.items[x-1].expireAt = i, b, expireAt
		s.adjust(x, lruPrev, lruNext) // refresh to head
		return 0
	}

	if s.last == uint16(cap(s.items)) {
		tail := &s.items[s.links[0][lruPrev]-1]
		if (*tail).expireAt > 0 { // don't notify for already-deleted items
			on(ActPut, (*tail).k, (*tail).v.i, (*tail).v.b, -1)
		}
		delete(s.indices, (*tail).k)
		s.indices[k], (*tail).k, (*tail).v.i, (*tail).v.b, (*tail).expireAt = s.links[0][lruPrev], k, i, b, expireAt
		s.adjust(s.links[0][lruPrev], lruPrev, lruNext) // refresh to head
		return 1
	}

	s.last++
	if len(s.indices) <= 0 {
		s.links[0][lruPrev] = s.last
	} else {
		s.links[s.links[0][lruNext]][lruPrev] = s.last
	}
	s.items[s.last-1].k, s.items[s.last-1].v.i, s.items[s.last-1].v.b, s.items[s.last-1].expireAt, s.links[s.last], s.indices[k], s.links[0][lruNext] = k, i, b, expireAt, [2]uint16{0, s.links[0][lruNext]}, s.last, s.last
	return 1
}

// get returns the node for key and moves it to head. Returns nil, 0 if absent.
func (s *lruShard) get(k string) (*lruNode, int) {
	if x, ok := s.indices[k]; ok {
		s.adjust(x, lruPrev, lruNext) // refresh to head
		return &s.items[x-1], 1
	}
	return nil, 0
}

// del marks the entry as deleted (expireAt = 0) and sinks it to tail.
// Returns the node, 1 if found, and the previous expireAt.
func (s *lruShard) del(k string) (_ *lruNode, _ int, e int64) {
	if x, ok := s.indices[k]; ok && s.items[x-1].expireAt > 0 {
		s.items[x-1].expireAt, e = 0, s.items[x-1].expireAt
		s.adjust(x, lruNext, lruPrev) // sink to tail
		return &s.items[x-1], 1, e
	}
	return nil, 0, 0
}

// walk calls f for each valid (non-deleted) item in LRU order (head to tail).
// Stops if f returns false.
func (s *lruShard) walk(f func(key string, iface *interface{}, bytes []byte, expireAt int64) bool) bool {
	for idx := s.links[0][lruNext]; idx != 0; idx = s.links[idx][lruNext] {
		if s.items[idx-1].expireAt > 0 && !f(s.items[idx-1].k, s.items[idx-1].v.i, s.items[idx-1].v.b, s.items[idx-1].expireAt) {
			return false
		}
	}
	return true
}

// adjust moves node idx to one end of the list.
//   - f=lruPrev, t=lruNext: move to head (most-recently-used end).
//   - f=lruNext, t=lruPrev: move to tail (least-recently-used end).
//
// If idx is already at the target end, this is a no-op.
func (s *lruShard) adjust(idx, f, t uint16) {
	if s.links[idx][f] != 0 { // not already at the target end
		s.links[s.links[idx][t]][f], s.links[s.links[idx][f]][t], s.links[idx][f], s.links[idx][t], s.links[s.links[0][t]][f], s.links[0][t] =
			s.links[idx][f], s.links[idx][t], 0, s.links[0][t], idx, idx
	}
}

// --- helpers ---

// hashBKRD is the Bernstein-style hash used for shard selection. It trades
// distribution quality for speed on short keys.
func hashBKRD(s string) (hash int32) {
	for i := 0; i < len(s); i++ {
		hash = hash*131 + int32(s[i])
	}
	return hash
}

// maskOfNextPowOf2 returns the mask (n-1) for the smallest power of two >= n.
// If n is already a power of two, returns n-1.
func maskOfNextPowOf2(cap uint16) uint16 {
	if cap > 0 && cap&(cap-1) == 0 {
		return cap - 1
	}
	cap |= cap >> 1
	cap |= cap >> 2
	cap |= cap >> 4
	return cap | cap>>8
}
