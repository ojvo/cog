package store

import (
	"sync"
	"testing"
	"time"
)

func TestCache_Basic(t *testing.T) {
	c := NewCache(WithDefaultExpiration(5*time.Minute), WithCleanupInterval(10*time.Minute))

	c.Set("foo", "bar", DefaultExpiration)
	if x, found := c.Get("foo"); !found || x != "bar" {
		t.Error("Set/Get failed")
	}

	c.Set("foo", "baz", DefaultExpiration)
	if x, found := c.Get("foo"); !found || x != "baz" {
		t.Error("Set replace failed")
	}
}

func TestCache_Expiration(t *testing.T) {
	c := NewCache(WithDefaultExpiration(50*time.Millisecond), WithCleanupInterval(100*time.Millisecond))

	c.Set("exp", "val", 50*time.Millisecond)
	time.Sleep(100 * time.Millisecond)

	if _, found := c.Get("exp"); found {
		t.Error("Item should have expired")
	}
}

func TestCache_Add(t *testing.T) {
	c := NewCache(WithDefaultExpiration(DefaultExpiration))

	err := c.Add("foo", "bar", DefaultExpiration)
	if err != nil {
		t.Error("Add failed")
	}

	err = c.Add("foo", "baz", DefaultExpiration)
	if err == nil {
		t.Error("Add should fail for existing key")
	}
}

func TestCache_Replace(t *testing.T) {
	c := NewCache(WithDefaultExpiration(DefaultExpiration))

	err := c.Replace("foo", "bar", DefaultExpiration)
	if err == nil {
		t.Error("Replace should fail for non-existent key")
	}

	c.Set("foo", "bar", DefaultExpiration)
	err = c.Replace("foo", "baz", DefaultExpiration)
	if err != nil {
		t.Error("Replace failed")
	}

	if x, _ := c.Get("foo"); x != "baz" {
		t.Error("Replace didn't update value")
	}
}

func TestCache_Increment(t *testing.T) {
	c := NewCache(WithDefaultExpiration(DefaultExpiration))
	c.Set("int", 10, DefaultExpiration)
	c.Set("int64", int64(10), DefaultExpiration)

	if _, err := c.IncrementInt("int", 5); err != nil {
		t.Errorf("Increment int failed: %v", err)
	}
	if x, _ := c.Get("int"); x != 15 {
		t.Errorf("Expected 15, got %v", x)
	}

	if _, err := c.IncrementInt64("int64", 5); err != nil {
		t.Errorf("Increment int64 failed: %v", err)
	}
	if x, _ := c.Get("int64"); x != int64(15) {
		t.Errorf("Expected 15, got %v", x)
	}
}

func TestCache_Decrement(t *testing.T) {
	c := NewCache(WithDefaultExpiration(DefaultExpiration))
	c.Set("int", 10, DefaultExpiration)
	c.Set("int64", int64(10), DefaultExpiration)

	if _, err := c.DecrementInt("int", 5); err != nil {
		t.Errorf("Decrement int failed: %v", err)
	}
	if x, _ := c.Get("int"); x != 5 {
		t.Errorf("Expected 5, got %v", x)
	}

	if _, err := c.DecrementInt64("int64", 5); err != nil {
		t.Errorf("Decrement int64 failed: %v", err)
	}
	if x, _ := c.Get("int64"); x != int64(5) {
		t.Errorf("Expected 5, got %v", x)
	}
}

func TestCache_DeleteExpired(t *testing.T) {
	c := NewCache(WithDefaultExpiration(50*time.Millisecond), WithCleanupInterval(100*time.Millisecond))

	c.Set("exp", "val", 50*time.Millisecond)
	c.Set("noexp", "val", NoExpiration)

	time.Sleep(100 * time.Millisecond)

	// Manually trigger delete expired (usually done by janitor, but we test logic here)
	c.DeleteExpired()

	if _, found := c.Get("exp"); found {
		t.Error("Expired item should be deleted")
	}

	if _, found := c.Get("noexp"); !found {
		t.Error("Non-expired item should remain")
	}
}

func TestCache_OnEvicted(t *testing.T) {
	c := NewCache(WithDefaultExpiration(DefaultExpiration))

	evictedKey := ""
	c.OnEvicted(func(k string, v interface{}) {
		evictedKey = k
	})

	c.Set("foo", "bar", DefaultExpiration)
	c.Delete("foo")

	if evictedKey != "foo" {
		t.Error("OnEvicted not called")
	}
}

// TestCache_Flush_OnEvicted verifies Flush invokes OnEvicted for each item,
// consistent with Delete's behavior.
func TestCache_Flush_OnEvicted(t *testing.T) {
	c := NewCache(WithDefaultExpiration(DefaultExpiration))

	seen := make(map[string]interface{})
	var mu sync.Mutex
	c.OnEvicted(func(k string, v interface{}) {
		mu.Lock()
		seen[k] = v
		mu.Unlock()
	})

	c.Set("a", 1, DefaultExpiration)
	c.Set("b", 2, DefaultExpiration)
	c.Set("c", 3, DefaultExpiration)
	c.Flush()

	if got := len(seen); got != 3 {
		t.Fatalf("Flush should invoke OnEvicted for each item; got %d calls, want 3", got)
	}
	for _, k := range []string{"a", "b", "c"} {
		if _, ok := seen[k]; !ok {
			t.Errorf("Flush did not invoke OnEvicted for key %q", k)
		}
	}
	if c.ItemCount() != 0 {
		t.Error("Flush should leave cache empty")
	}
}

// TestCache_Delete_OnEvictedConcurrentNil verifies Delete does not panic when
// OnEvicted is concurrently set to nil after the delete decision but before the
// callback invocation.
func TestCache_Delete_OnEvictedConcurrentNil(t *testing.T) {
	c := NewCache(WithDefaultExpiration(DefaultExpiration))

	c.OnEvicted(func(k string, v interface{}) {})

	c.Set("k1", "v1", DefaultExpiration)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			c.OnEvicted(nil)
			c.OnEvicted(func(k string, v interface{}) {})
		}
	}()

	for i := 0; i < 100; i++ {
		c.Set("k1", "v1", DefaultExpiration)
		c.Delete("k1")
	}

	<-done
}

// TestCache_DeleteExpired_OnEvictedCallbackSnapshot verifies DeleteExpired
// captures the onEvicted snapshot under the lock so a concurrent OnEvicted(nil)
// does not cause a nil-call panic.
func TestCache_DeleteExpired_OnEvictedCallbackSnapshot(t *testing.T) {
	c := NewCache(WithDefaultExpiration(10 * time.Millisecond))

	c.OnEvicted(func(k string, v interface{}) {})

	c.Set("exp1", "v1", 10*time.Millisecond)
	c.Set("exp2", "v2", 10*time.Millisecond)

	time.Sleep(30 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			c.OnEvicted(nil)
			c.OnEvicted(func(k string, v interface{}) {})
		}
	}()

	for i := 0; i < 100; i++ {
		c.DeleteExpired()
	}

	<-done
}

func TestCache_Concurrency(t *testing.T) {
	c := NewCache(WithDefaultExpiration(DefaultExpiration))
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c.Set("key", i, DefaultExpiration)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Get("key")
		}()
	}

	wg.Wait()
}

func TestCache_GetWithExpiration(t *testing.T) {
	c := NewCache(WithDefaultExpiration(DefaultExpiration))

	exp := 100 * time.Millisecond
	c.Set("key", "val", exp)

	val, tExp, found := c.GetWithExpiration("key")
	if !found {
		t.Error("Key not found")
	}
	if val != "val" {
		t.Error("Value mismatch")
	}

	// Check if expiration time is approximately now + 100ms
	if time.Until(tExp) > exp || time.Until(tExp) < exp-50*time.Millisecond {
		t.Logf("Expiration time seems off: %v (expected around %v from now)", time.Until(tExp), exp)
	}
}

func TestCache_IncrementFloat(t *testing.T) {
	c := NewCache(WithDefaultExpiration(DefaultExpiration))
	c.Set("float", 1.5, DefaultExpiration)

	if _, err := c.IncrementFloat64("float", 0.5); err != nil {
		t.Errorf("IncrementFloat failed: %v", err)
	}

	if x, _ := c.Get("float"); x != 2.0 {
		t.Errorf("Expected 2.0, got %v", x)
	}
}

// TestCache_TypedIncrement_Variants verifies the generic addTyped helper works
// correctly across representative numeric types after the boilerplate collapse.
func TestCache_TypedIncrement_Variants(t *testing.T) {
	c := NewCache(WithDefaultExpiration(DefaultExpiration))

	c.Set("i", int(10), DefaultExpiration)
	if v, err := c.IncrementInt("i", 5); err != nil || v != 15 {
		t.Errorf("IncrementInt: v=%d err=%v", v, err)
	}

	c.Set("i8", int8(10), DefaultExpiration)
	if v, err := c.IncrementInt8("i8", 5); err != nil || v != 15 {
		t.Errorf("IncrementInt8: v=%d err=%v", v, err)
	}

	c.Set("i16", int16(10), DefaultExpiration)
	if v, err := c.IncrementInt16("i16", 5); err != nil || v != 15 {
		t.Errorf("IncrementInt16: v=%d err=%v", v, err)
	}

	c.Set("i32", int32(10), DefaultExpiration)
	if v, err := c.IncrementInt32("i32", 5); err != nil || v != 15 {
		t.Errorf("IncrementInt32: v=%d err=%v", v, err)
	}

	c.Set("i64", int64(10), DefaultExpiration)
	if v, err := c.IncrementInt64("i64", 5); err != nil || v != 15 {
		t.Errorf("IncrementInt64: v=%d err=%v", v, err)
	}

	c.Set("u", uint(10), DefaultExpiration)
	if v, err := c.IncrementUint("u", 5); err != nil || v != 15 {
		t.Errorf("IncrementUint: v=%d err=%v", v, err)
	}

	c.Set("up", uintptr(10), DefaultExpiration)
	if v, err := c.IncrementUintptr("up", 5); err != nil || v != 15 {
		t.Errorf("IncrementUintptr: v=%d err=%v", v, err)
	}

	c.Set("u8", uint8(10), DefaultExpiration)
	if v, err := c.IncrementUint8("u8", 5); err != nil || v != 15 {
		t.Errorf("IncrementUint8: v=%d err=%v", v, err)
	}

	c.Set("u16", uint16(10), DefaultExpiration)
	if v, err := c.IncrementUint16("u16", 5); err != nil || v != 15 {
		t.Errorf("IncrementUint16: v=%d err=%v", v, err)
	}

	c.Set("u32", uint32(10), DefaultExpiration)
	if v, err := c.IncrementUint32("u32", 5); err != nil || v != 15 {
		t.Errorf("IncrementUint32: v=%d err=%v", v, err)
	}

	c.Set("u64", uint64(10), DefaultExpiration)
	if v, err := c.IncrementUint64("u64", 5); err != nil || v != 15 {
		t.Errorf("IncrementUint64: v=%d err=%v", v, err)
	}

	c.Set("f32", float32(1.5), DefaultExpiration)
	if v, err := c.IncrementFloat32("f32", 0.5); err != nil || v != 2.0 {
		t.Errorf("IncrementFloat32: v=%v err=%v", v, err)
	}

	c.Set("f64", float64(1.5), DefaultExpiration)
	if v, err := c.IncrementFloat64("f64", 0.5); err != nil || v != 2.0 {
		t.Errorf("IncrementFloat64: v=%v err=%v", v, err)
	}
}

// TestCache_TypedDecrement_Variants verifies the generic subTyped helper works
// correctly across representative numeric types after the boilerplate collapse.
func TestCache_TypedDecrement_Variants(t *testing.T) {
	c := NewCache(WithDefaultExpiration(DefaultExpiration))

	c.Set("i", int(10), DefaultExpiration)
	if v, err := c.DecrementInt("i", 5); err != nil || v != 5 {
		t.Errorf("DecrementInt: v=%d err=%v", v, err)
	}

	c.Set("i8", int8(10), DefaultExpiration)
	if v, err := c.DecrementInt8("i8", 5); err != nil || v != 5 {
		t.Errorf("DecrementInt8: v=%d err=%v", v, err)
	}

	c.Set("i16", int16(10), DefaultExpiration)
	if v, err := c.DecrementInt16("i16", 5); err != nil || v != 5 {
		t.Errorf("DecrementInt16: v=%d err=%v", v, err)
	}

	c.Set("i32", int32(10), DefaultExpiration)
	if v, err := c.DecrementInt32("i32", 5); err != nil || v != 5 {
		t.Errorf("DecrementInt32: v=%d err=%v", v, err)
	}

	c.Set("i64", int64(10), DefaultExpiration)
	if v, err := c.DecrementInt64("i64", 5); err != nil || v != 5 {
		t.Errorf("DecrementInt64: v=%d err=%v", v, err)
	}

	c.Set("u", uint(10), DefaultExpiration)
	if v, err := c.DecrementUint("u", 5); err != nil || v != 5 {
		t.Errorf("DecrementUint: v=%d err=%v", v, err)
	}

	c.Set("up", uintptr(10), DefaultExpiration)
	if v, err := c.DecrementUintptr("up", 5); err != nil || v != 5 {
		t.Errorf("DecrementUintptr: v=%d err=%v", v, err)
	}

	c.Set("u8", uint8(10), DefaultExpiration)
	if v, err := c.DecrementUint8("u8", 5); err != nil || v != 5 {
		t.Errorf("DecrementUint8: v=%d err=%v", v, err)
	}

	c.Set("u16", uint16(10), DefaultExpiration)
	if v, err := c.DecrementUint16("u16", 5); err != nil || v != 5 {
		t.Errorf("DecrementUint16: v=%d err=%v", v, err)
	}

	c.Set("u32", uint32(10), DefaultExpiration)
	if v, err := c.DecrementUint32("u32", 5); err != nil || v != 5 {
		t.Errorf("DecrementUint32: v=%d err=%v", v, err)
	}

	c.Set("u64", uint64(10), DefaultExpiration)
	if v, err := c.DecrementUint64("u64", 5); err != nil || v != 5 {
		t.Errorf("DecrementUint64: v=%d err=%v", v, err)
	}

	c.Set("f32", float32(2.0), DefaultExpiration)
	if v, err := c.DecrementFloat32("f32", 0.5); err != nil || v != 1.5 {
		t.Errorf("DecrementFloat32: v=%v err=%v", v, err)
	}

	c.Set("f64", float64(2.0), DefaultExpiration)
	if v, err := c.DecrementFloat64("f64", 0.5); err != nil || v != 1.5 {
		t.Errorf("DecrementFloat64: v=%v err=%v", v, err)
	}
}

// TestCache_TypedIncrement_Errors verifies error paths shared by all typed
// methods via the generic helper: missing key, expired key, and type mismatch.
func TestCache_TypedIncrement_Errors(t *testing.T) {
	c := NewCache(WithDefaultExpiration(DefaultExpiration))

	// Missing key
	if _, err := c.IncrementInt("missing", 1); err == nil {
		t.Error("IncrementInt on missing key should error")
	}

	// Expired key
	c.Set("exp", int(10), 10*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	if _, err := c.IncrementInt("exp", 1); err == nil {
		t.Error("IncrementInt on expired key should error")
	}

	// Type mismatch: stored value is string, not int
	c.Set("str", "not-a-number", DefaultExpiration)
	if _, err := c.IncrementInt("str", 1); err == nil {
		t.Error("IncrementInt on non-int value should error")
	}

	// Type mismatch: stored value is int, not uint64
	c.Set("i", int(10), DefaultExpiration)
	if _, err := c.IncrementUint64("i", 1); err == nil {
		t.Error("IncrementUint64 on int value should error")
	}

	// Decrement error paths mirror increment
	if _, err := c.DecrementInt("missing", 1); err == nil {
		t.Error("DecrementInt on missing key should error")
	}
	c.Set("str2", "not-a-number", DefaultExpiration)
	if _, err := c.DecrementFloat64("str2", 1.0); err == nil {
		t.Error("DecrementFloat64 on non-float value should error")
	}
}

// TestCache_TypedIncrement_Persists verifies the new value is actually written
// back to the cache (not just returned), confirming the helper's write-back path.
func TestCache_TypedIncrement_Persists(t *testing.T) {
	c := NewCache(WithDefaultExpiration(DefaultExpiration))

	c.Set("k", int(100), DefaultExpiration)
	if _, err := c.IncrementInt("k", 50); err != nil {
		t.Fatalf("IncrementInt failed: %v", err)
	}
	if v, ok := c.Get("k"); !ok || v != 150 {
		t.Errorf("expected persisted 150, got %v (ok=%v)", v, ok)
	}

	// Chain multiple increments
	c.IncrementInt("k", 1)
	c.IncrementInt("k", 1)
	if v, _ := c.Get("k"); v != 152 {
		t.Errorf("expected 152 after chained increments, got %v", v)
	}
}
