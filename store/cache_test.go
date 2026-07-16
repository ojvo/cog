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

	err := c.Increment("int", 5)
	if err != nil {
		t.Errorf("Increment int failed: %v", err)
	}
	if x, _ := c.Get("int"); x != 15 {
		t.Errorf("Expected 15, got %v", x)
	}

	err = c.Increment("int64", 5)
	if err != nil {
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

	err := c.Decrement("int", 5)
	if err != nil {
		t.Errorf("Decrement int failed: %v", err)
	}
	if x, _ := c.Get("int"); x != 5 {
		t.Errorf("Expected 5, got %v", x)
	}

	err = c.Decrement("int64", 5)
	if err != nil {
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
	if time.Until(tExp) > exp || time.Until(tExp) < exp - 50*time.Millisecond {
		t.Logf("Expiration time seems off: %v (expected around %v from now)", time.Until(tExp), exp)
	}
}

func TestCache_IncrementFloat(t *testing.T) {
	c := NewCache(WithDefaultExpiration(DefaultExpiration))
	c.Set("float", 1.5, DefaultExpiration)
	
	if err := c.IncrementFloat("float", 0.5); err != nil {
		t.Errorf("IncrementFloat failed: %v", err)
	}
	
	if x, _ := c.Get("float"); x != 2.0 {
		t.Errorf("Expected 2.0, got %v", x)
	}
}
