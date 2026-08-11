package store

import (
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"time"
)

type cacheItem struct {
	Object     interface{}
	Expiration int64
}

// Returns true if the item has expired.
func (item cacheItem) Expired() bool {
	if item.Expiration == 0 {
		return false
	}
	return time.Now().UnixNano() > item.Expiration
}

const (
	// For use with functions that take an expiration time.
	NoExpiration time.Duration = -1
	// For use with functions that take an expiration time. Equivalent to
	// passing in the same expiration duration as was given to New() or
	// NewFrom() when the cache was created (e.g. 5 minutes.)
	DefaultExpiration time.Duration = 0
)

type Cache struct {
	*cache
	// If this is confusing, see the comment at the bottom of New()
}

type cache struct {
	defaultExpiration time.Duration
	items             map[string]cacheItem
	mu                sync.RWMutex
	onEvicted         func(string, interface{})
	janitor           *janitor
}

// Add an item to the cache, replacing any existing item. If the duration is 0
// (DefaultExpiration), the cache's default expiration time is used. If it is -1
// (NoExpiration), the item never expires.
func (c *cache) Set(k string, x interface{}, d time.Duration) {
	// "Inlining" of set
	var e int64
	if d == DefaultExpiration {
		d = c.defaultExpiration
	}
	if d > 0 {
		e = time.Now().Add(d).UnixNano()
	}
	c.mu.Lock()
	c.items[k] = cacheItem{
		Object:     x,
		Expiration: e,
	}
	// TODO: Calls to mu.Unlock are currently not deferred because defer
	// adds ~200 ns (as of go1.)
	c.mu.Unlock()
}

func (c *cache) set(k string, x interface{}, d time.Duration) {
	var e int64
	if d == DefaultExpiration {
		d = c.defaultExpiration
	}
	if d > 0 {
		e = time.Now().Add(d).UnixNano()
	}
	c.items[k] = cacheItem{
		Object:     x,
		Expiration: e,
	}
}

// Add an item to the cache, replacing any existing item, using the default
// expiration.
func (c *cache) SetDefault(k string, x interface{}) {
	c.Set(k, x, DefaultExpiration)
}

// Add an item to the cache only if an item doesn't already exist for the given
// key, or if the existing item has expired. Returns an error otherwise.
func (c *cache) Add(k string, x interface{}, d time.Duration) error {
	c.mu.Lock()
	_, found := c.get(k)
	if found {
		c.mu.Unlock()
		return fmt.Errorf("cacheItem %s already exists", k)
	}
	c.set(k, x, d)
	c.mu.Unlock()
	return nil
}

// Set a new value for the cache key only if it already exists, and the existing
// item hasn't expired. Returns an error otherwise.
func (c *cache) Replace(k string, x interface{}, d time.Duration) error {
	c.mu.Lock()
	_, found := c.get(k)
	if !found {
		c.mu.Unlock()
		return fmt.Errorf("cacheItem %s doesn't exist", k)
	}
	c.set(k, x, d)
	c.mu.Unlock()
	return nil
}

// Get an item from the cache. Returns the item or nil, and a bool indicating
// whether the key was found.
func (c *cache) Get(k string) (interface{}, bool) {
	c.mu.RLock()
	// "Inlining" of get and Expired
	item, found := c.items[k]
	if !found {
		c.mu.RUnlock()
		return nil, false
	}
	if item.Expiration > 0 {
		if time.Now().UnixNano() > item.Expiration {
			c.mu.RUnlock()
			return nil, false
		}
	}
	c.mu.RUnlock()
	return item.Object, true
}

// GetWithExpiration returns an item and its expiration time from the cache.
// It returns the item or nil, the expiration time if one is set (if the item
// never expires a zero value for time.Time is returned), and a bool indicating
// whether the key was found.
func (c *cache) GetWithExpiration(k string) (interface{}, time.Time, bool) {
	c.mu.RLock()
	// "Inlining" of get and Expired
	item, found := c.items[k]
	if !found {
		c.mu.RUnlock()
		return nil, time.Time{}, false
	}

	if item.Expiration > 0 {
		if time.Now().UnixNano() > item.Expiration {
			c.mu.RUnlock()
			return nil, time.Time{}, false
		}

		// Return the item and the expiration time
		c.mu.RUnlock()
		return item.Object, time.Unix(0, item.Expiration), true
	}

	// If expiration <= 0 (i.e. no expiration time set) then return the item
	// and a zeroed time.Time
	c.mu.RUnlock()
	return item.Object, time.Time{}, true
}

func (c *cache) get(k string) (interface{}, bool) {
	item, found := c.items[k]
	if !found {
		return nil, false
	}
	// "Inlining" of Expired
	if item.Expiration > 0 {
		if time.Now().UnixNano() > item.Expiration {
			return nil, false
		}
	}
	return item.Object, true
}

// number constrains the generic typed increment/decrement helpers to the
// numeric kinds supported by the cache.
type number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64
}

// addTyped adds n to the numeric value stored under k and returns the new
// value. Returns an error if the key is absent/expired or the stored value is
// not of type T. The 13 typed Increment methods below delegate here, removing
// ~250 lines of per-type boilerplate.
func addTyped[T number](c *cache, k string, n T) (T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, found := c.items[k]
	if !found || v.Expired() {
		var zero T
		return zero, fmt.Errorf("cacheItem %s not found", k)
	}
	rv, ok := v.Object.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("The value for %s is not %T", k, rv)
	}
	nv := rv + n
	v.Object = nv
	c.items[k] = v
	return nv, nil
}

// subTyped subtracts n from the numeric value stored under k. See addTyped.
// The 13 typed Decrement methods delegate here.
func subTyped[T number](c *cache, k string, n T) (T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, found := c.items[k]
	if !found || v.Expired() {
		var zero T
		return zero, fmt.Errorf("cacheItem %s not found", k)
	}
	rv, ok := v.Object.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("The value for %s is not %T", k, rv)
	}
	nv := rv - n
	v.Object = nv
	c.items[k] = v
	return nv, nil
}

func (c *cache) IncrementInt(k string, n int) (int, error)             { return addTyped(c, k, n) }
func (c *cache) IncrementInt8(k string, n int8) (int8, error)          { return addTyped(c, k, n) }
func (c *cache) IncrementInt16(k string, n int16) (int16, error)       { return addTyped(c, k, n) }
func (c *cache) IncrementInt32(k string, n int32) (int32, error)       { return addTyped(c, k, n) }
func (c *cache) IncrementInt64(k string, n int64) (int64, error)       { return addTyped(c, k, n) }
func (c *cache) IncrementUint(k string, n uint) (uint, error)          { return addTyped(c, k, n) }
func (c *cache) IncrementUintptr(k string, n uintptr) (uintptr, error) { return addTyped(c, k, n) }
func (c *cache) IncrementUint8(k string, n uint8) (uint8, error)       { return addTyped(c, k, n) }
func (c *cache) IncrementUint16(k string, n uint16) (uint16, error)    { return addTyped(c, k, n) }
func (c *cache) IncrementUint32(k string, n uint32) (uint32, error)    { return addTyped(c, k, n) }
func (c *cache) IncrementUint64(k string, n uint64) (uint64, error)    { return addTyped(c, k, n) }
func (c *cache) IncrementFloat32(k string, n float32) (float32, error) { return addTyped(c, k, n) }
func (c *cache) IncrementFloat64(k string, n float64) (float64, error) { return addTyped(c, k, n) }

func (c *cache) DecrementInt(k string, n int) (int, error)             { return subTyped(c, k, n) }
func (c *cache) DecrementInt8(k string, n int8) (int8, error)          { return subTyped(c, k, n) }
func (c *cache) DecrementInt16(k string, n int16) (int16, error)       { return subTyped(c, k, n) }
func (c *cache) DecrementInt32(k string, n int32) (int32, error)       { return subTyped(c, k, n) }
func (c *cache) DecrementInt64(k string, n int64) (int64, error)       { return subTyped(c, k, n) }
func (c *cache) DecrementUint(k string, n uint) (uint, error)          { return subTyped(c, k, n) }
func (c *cache) DecrementUintptr(k string, n uintptr) (uintptr, error) { return subTyped(c, k, n) }
func (c *cache) DecrementUint8(k string, n uint8) (uint8, error)       { return subTyped(c, k, n) }
func (c *cache) DecrementUint16(k string, n uint16) (uint16, error)    { return subTyped(c, k, n) }
func (c *cache) DecrementUint32(k string, n uint32) (uint32, error)    { return subTyped(c, k, n) }
func (c *cache) DecrementUint64(k string, n uint64) (uint64, error)    { return subTyped(c, k, n) }
func (c *cache) DecrementFloat32(k string, n float32) (float32, error) { return subTyped(c, k, n) }
func (c *cache) DecrementFloat64(k string, n float64) (float64, error) { return subTyped(c, k, n) }

// Delete an item from the cache. Does nothing if the key is not in the cache.
func (c *cache) Delete(k string) {
	c.mu.Lock()
	v, evicted := c.delete(k)
	onEvicted := c.onEvicted
	c.mu.Unlock()
	if evicted && onEvicted != nil {
		onEvicted(k, v)
	}
}

func (c *cache) delete(k string) (interface{}, bool) {
	if c.onEvicted != nil {
		if v, found := c.items[k]; found {
			delete(c.items, k)
			return v.Object, true
		}
	}
	delete(c.items, k)
	return nil, false
}

type keyAndValue struct {
	key   string
	value interface{}
}

// Delete all expired items from the cache.
func (c *cache) DeleteExpired() {
	var evictedItems []keyAndValue
	now := time.Now().UnixNano()
	c.mu.Lock()
	onEvicted := c.onEvicted
	for k, v := range c.items {
		// "Inlining" of expired
		if v.Expiration > 0 && now > v.Expiration {
			ov, evicted := c.delete(k)
			if evicted {
				evictedItems = append(evictedItems, keyAndValue{k, ov})
			}
		}
	}
	c.mu.Unlock()
	if onEvicted == nil {
		return
	}
	for _, v := range evictedItems {
		onEvicted(v.key, v.value)
	}
}

// Sets an (optional) function that is called with the key and value when an
// item is evicted from the cache. (Including when it is deleted manually, but
// not when it is overwritten.) Set to nil to disable.
func (c *cache) OnEvicted(f func(string, interface{})) {
	c.mu.Lock()
	c.onEvicted = f
	c.mu.Unlock()
}

// Write the cache's items (using Gob) to an io.Writer.
//
// NOTE: This method is deprecated in favor of c.Items() and NewFrom() (see the
// documentation for NewFrom().)
func (c *cache) Save(w io.Writer) (err error) {
	enc := gob.NewEncoder(w)
	defer func() {
		if x := recover(); x != nil {
			err = fmt.Errorf("Error registering item types with Gob library")
		}
	}()
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, v := range c.items {
		gob.Register(v.Object)
	}
	err = enc.Encode(&c.items)
	return
}

// Save the cache's items to the given filename, creating the file if it
// doesn't exist, and overwriting it if it does.
//
// NOTE: This method is deprecated in favor of c.Items() and NewFrom() (see the
// documentation for NewFrom().)
func (c *cache) SaveFile(fname string) error {
	fp, err := os.Create(fname)
	if err != nil {
		return err
	}
	err = c.Save(fp)
	if err != nil {
		fp.Close()
		return err
	}
	return fp.Close()
}

// Add (Gob-serialized) cache items from an io.Reader, excluding any items with
// keys that already exist (and haven't expired) in the current cache.
//
// NOTE: This method is deprecated in favor of c.Items() and NewFrom() (see the
// documentation for NewFrom().)
func (c *cache) Load(r io.Reader) error {
	dec := gob.NewDecoder(r)
	items := map[string]cacheItem{}
	err := dec.Decode(&items)
	if err == nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		for k, v := range items {
			ov, found := c.items[k]
			if !found || ov.Expired() {
				c.items[k] = v
			}
		}
	}
	return err
}

// Load and add cache items from the given filename, excluding any items with
// keys that already exist in the current cache.
//
// NOTE: This method is deprecated in favor of c.Items() and NewFrom() (see the
// documentation for NewFrom().)
func (c *cache) LoadFile(fname string) error {
	fp, err := os.Open(fname)
	if err != nil {
		return err
	}
	err = c.Load(fp)
	if err != nil {
		fp.Close()
		return err
	}
	return fp.Close()
}

// Copies all unexpired items in the cache into a new map and returns it.
func (c *cache) Items() map[string]cacheItem {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := make(map[string]cacheItem, len(c.items))
	now := time.Now().UnixNano()
	for k, v := range c.items {
		// "Inlining" of Expired
		if v.Expiration > 0 {
			if now > v.Expiration {
				continue
			}
		}
		m[k] = v
	}
	return m
}

// Returns the number of items in the cache. This may include items that have
// expired, but have not yet been cleaned up.
func (c *cache) ItemCount() int {
	c.mu.RLock()
	n := len(c.items)
	c.mu.RUnlock()
	return n
}

// Delete all items from the cache. If OnEvicted is set, it is invoked for
// each evicted item (consistent with Delete), so observers can release
// resources tied to cached values.
func (c *cache) Flush() {
	var evictedItems []keyAndValue
	c.mu.Lock()
	onEvicted := c.onEvicted
	for k, v := range c.items {
		if onEvicted != nil {
			evictedItems = append(evictedItems, keyAndValue{k, v.Object})
		}
		delete(c.items, k)
	}
	c.mu.Unlock()
	for _, kv := range evictedItems {
		onEvicted(kv.key, kv.value)
	}
}

type janitor struct {
	Interval time.Duration
	stop     chan bool
}

func (j *janitor) Run(c *cache) {
	ticker := time.NewTicker(j.Interval)
	for {
		select {
		case <-ticker.C:
			c.DeleteExpired()
		case <-j.stop:
			ticker.Stop()
			return
		}
	}
}

func stopJanitor(c *Cache) {
	close(c.janitor.stop)
}

func runJanitor(c *cache, ci time.Duration) {
	j := &janitor{
		Interval: ci,
		stop:     make(chan bool),
	}
	c.janitor = j
	go j.Run(c)
}

func newCache(de time.Duration, m map[string]cacheItem) *cache {
	if de == 0 {
		de = -1
	}
	c := &cache{
		defaultExpiration: de,
		items:             m,
	}
	return c
}

func newCacheWithJanitor(de time.Duration, ci time.Duration, m map[string]cacheItem) *Cache {
	c := newCache(de, m)
	// This trick ensures that the janitor goroutine (which--granted it
	// was enabled--is running DeleteExpired on c forever) does not keep
	// the returned C object from being garbage collected. When it is
	// garbage collected, the finalizer stops the janitor goroutine, after
	// which c can be collected.
	C := &Cache{c}
	if ci > 0 {
		runJanitor(c, ci)
		runtime.SetFinalizer(C, stopJanitor)
	}
	return C
}

// CacheOptions holds configuration options for the cache.
type CacheOptions struct {
	DefaultExpiration time.Duration
	CleanupInterval   time.Duration
	OnEvicted         func(string, interface{})
}

// CacheOption is a function that configures a CacheOptions.
type CacheOption func(*CacheOptions)

// WithDefaultExpiration sets the default expiration for items.
func WithDefaultExpiration(d time.Duration) CacheOption {
	return func(opts *CacheOptions) {
		opts.DefaultExpiration = d
	}
}

// A value of 0 or less disables automatic cleanup.
func WithCleanupInterval(d time.Duration) CacheOption {
	return func(opts *CacheOptions) {
		opts.CleanupInterval = d
	}
}

func WithOnEvicted(f func(string, interface{})) CacheOption {
	return func(opts *CacheOptions) {
		opts.OnEvicted = f
	}
}

func NewCache(options ...CacheOption) *Cache {
	// Initialize default options
	opts := CacheOptions{
		DefaultExpiration: DefaultExpiration, // Use the package constant
		CleanupInterval:   time.Duration(0),  // Default to no automatic cleanup
		OnEvicted:         nil,
	}

	// Apply provided options
	for _, opt := range options {
		opt(&opts)
	}

	// Use the final options to create the cache
	items := make(map[string]cacheItem)
	c := &cache{
		defaultExpiration: opts.DefaultExpiration,
		items:             items,
		onEvicted:         opts.OnEvicted,
	}

	C := &Cache{c}

	// Start janitor if cleanup interval is positive
	if opts.CleanupInterval > 0 {
		runJanitor(c, opts.CleanupInterval)
		runtime.SetFinalizer(C, stopJanitor)
	}

	return C
}
