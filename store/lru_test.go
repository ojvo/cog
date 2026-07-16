package store

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestLRU_PutGet(t *testing.T) {
	c := NewLRUCache(4, 4)

	c.Put("a", "alpha")
	c.Put("b", 42)
	c.Put("c", true)

	if v, ok := c.Get("a"); !ok || v != "alpha" {
		t.Fatalf("Get(a) = %v, %v, want alpha, true", v, ok)
	}
	if v, ok := c.Get("b"); !ok || v != 42 {
		t.Fatalf("Get(b) = %v, %v, want 42, true", v, ok)
	}
	if v, ok := c.Get("c"); !ok || v != true {
		t.Fatalf("Get(c) = %v, %v, want true, true", v, ok)
	}
	if _, ok := c.Get("missing"); ok {
		t.Fatal("Get(missing) = true, want false")
	}
}

func TestLRU_PutUpdate(t *testing.T) {
	c := NewLRUCache(2, 4)

	c.Put("k", "v1")
	c.Put("k", "v2") // update, not add

	if v, ok := c.Get("k"); !ok || v != "v2" {
		t.Fatalf("Get(k) after update = %v, %v, want v2, true", v, ok)
	}
}

func TestLRU_Int64(t *testing.T) {
	c := NewLRUCache(2, 4)

	c.PutInt64("n", 1234567890)
	if v, ok := c.GetInt64("n"); !ok || v != 1234567890 {
		t.Fatalf("GetInt64(n) = %v, %v, want 1234567890, true", v, ok)
	}
	// Also accessible as bytes.
	if b, ok := c.GetBytes("n"); !ok || len(b) != 8 {
		t.Fatalf("GetBytes(n) len = %d, want 8", len(b))
	}
	// Not accessible as interface.
	if _, ok := c.Get("n"); ok {
		t.Fatal("Get(n) on int64 entry = true, want false")
	}
}

func TestLRU_Bytes(t *testing.T) {
	c := NewLRUCache(2, 4)

	data := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	c.PutBytes("raw", data)
	if b, ok := c.GetBytes("raw"); !ok || string(b) != string(data) {
		t.Fatalf("GetBytes(raw) = %x, %v, want deadbeef, true", b, ok)
	}
}

func TestBytesToInt64(t *testing.T) {
	// Round-trip with PutInt64 value.
	c := NewLRUCache(1, 4)
	c.PutInt64("n", 42)
	b, ok := c.GetBytes("n")
	if !ok {
		t.Fatal("GetBytes(n) = false")
	}
	v, ok := BytesToInt64(b)
	if !ok || v != 42 {
		t.Fatalf("BytesToInt64 = %d, %v, want 42, true", v, ok)
	}
	// Short slice returns false.
	if _, ok := BytesToInt64([]byte{1, 2, 3}); ok {
		t.Fatal("BytesToInt64(short) = true, want false")
	}
}

func TestLRU_Eviction(t *testing.T) {
	// 2 shards * 2 capacity = 4 total slots.
	// All keys hash to the same shard for predictable eviction.
	c := NewLRUCache(1, 3)

	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)
	// Shard is full. Access "a" to make it most-recently-used.
	c.Get("a")
	// Put "d" — should evict "b" (least recently used).
	c.Put("d", 4)

	if _, ok := c.Get("b"); ok {
		t.Fatal("Get(b) after eviction = true, want false")
	}
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("Get(a) = %v, %v, want 1, true (should survive)", v, ok)
	}
	if v, ok := c.Get("c"); !ok || v != 3 {
		t.Fatalf("Get(c) = %v, %v, want 3, true", v, ok)
	}
	if v, ok := c.Get("d"); !ok || v != 4 {
		t.Fatalf("Get(d) = %v, %v, want 4, true", v, ok)
	}
}

func TestLRU_Del(t *testing.T) {
	c := NewLRUCache(2, 4)

	c.Put("x", "val")
	c.Del("x")
	if _, ok := c.Get("x"); ok {
		t.Fatal("Get(x) after Del = true, want false")
	}
	// Del non-existent key should not panic.
	c.Del("nonexistent")
}

func TestLRU_Del_AfterGet(t *testing.T) {
	c := NewLRUCache(1, 4)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Get("a") // move a to head
	c.Del("a")
	c.Del("b")
	if _, ok := c.Get("a"); ok {
		t.Fatal("Get(a) after Del = true")
	}
	if _, ok := c.Get("b"); ok {
		t.Fatal("Get(b) after Del = true")
	}
}

func TestLRU_Walk(t *testing.T) {
	c := NewLRUCache(1, 8)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)

	var keys []string
	c.Walk(func(key string, _ *interface{}, _ []byte, _ int64) bool {
		keys = append(keys, key)
		return true
	})

	// Walk iterates in LRU order (most recent first): c, b, a.
	if len(keys) != 3 {
		t.Fatalf("Walk visited %d keys, want 3", len(keys))
	}
	if keys[0] != "c" || keys[1] != "b" || keys[2] != "a" {
		t.Fatalf("Walk order = %v, want [c b a]", keys)
	}
}

func TestLRU_Walk_StopEarly(t *testing.T) {
	c := NewLRUCache(1, 8)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)

	count := 0
	c.Walk(func(key string, _ *interface{}, _ []byte, _ int64) bool {
		count++
		return count < 2 // stop after 2
	})
	if count != 2 {
		t.Fatalf("Walk visited %d, want 2 (stopped early)", count)
	}
}

func TestLRU_Expiration(t *testing.T) {
	c := NewLRUCache(1, 4, 50*time.Millisecond)

	c.Put("temp", "value")
	if v, ok := c.Get("temp"); !ok || v != "value" {
		t.Fatalf("Get before expiry = %v, %v, want value, true", v, ok)
	}
	time.Sleep(80 * time.Millisecond)
	if _, ok := c.Get("temp"); ok {
		t.Fatal("Get after expiry = true, want false")
	}
}

func TestLRU_NoExpiration(t *testing.T) {
	c := NewLRUCache(1, 4) // no expiration
	c.Put("perm", "value")
	time.Sleep(30 * time.Millisecond)
	if v, ok := c.Get("perm"); !ok || v != "value" {
		t.Fatalf("Get without expiration = %v, %v, want value, true", v, ok)
	}
}

func TestLRU_Inspect(t *testing.T) {
	c := NewLRUCache(1, 4)

	var actions []string
	c.Inspect(func(action int, key string, _ *interface{}, _ []byte, status int) {
		actions = append(actions, fmt.Sprintf("act=%d key=%s status=%d", action, key, status))
	})

	c.Put("a", 1) // ActPut, status=1 (added)
	c.Put("a", 2) // ActPut, status=0 (updated)
	c.Get("a")    // ActGet, status=1 (hit)
	c.Get("miss") // ActGet, status=0 (miss)
	c.Del("a")    // ActDel, status=1 (deleted)
	c.Del("miss") // ActDel, status=0 (not found)

	expected := []string{
		"act=1 key=a status=1",
		"act=1 key=a status=0",
		"act=2 key=a status=1",
		"act=2 key=miss status=0",
		"act=3 key=a status=1",
		"act=3 key=miss status=0",
	}
	if len(actions) != len(expected) {
		t.Fatalf("got %d actions, want %d: %v", len(actions), len(expected), actions)
	}
	for i, want := range expected {
		if actions[i] != want {
			t.Fatalf("action[%d] = %s, want %s", i, actions[i], want)
		}
	}
}

func TestLRU_Inspect_Chain(t *testing.T) {
	c := NewLRUCache(1, 4)

	var order []int
	c.Inspect(func(int, string, *interface{}, []byte, int) { order = append(order, 1) })
	c.Inspect(func(int, string, *interface{}, []byte, int) { order = append(order, 2) })
	c.Inspect(func(int, string, *interface{}, []byte, int) { order = append(order, 3) })

	c.Put("k", "v")
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Fatalf("inspector chain order = %v, want [1 2 3]", order)
	}
}

func TestLRU_Inspect_Eviction(t *testing.T) {
	c := NewLRUCache(1, 2)

	var evicted []string
	c.Inspect(func(action int, key string, _ *interface{}, _ []byte, status int) {
		if status == -1 {
			evicted = append(evicted, key)
		}
	})

	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3) // should evict "a"

	if len(evicted) != 1 || evicted[0] != "a" {
		t.Fatalf("evicted = %v, want [a]", evicted)
	}
}

func TestLRU_LRU2(t *testing.T) {
	c := NewLRUCache(1, 2).LRU2(2)

	c.Put("a", 1)
	c.Put("b", 2)

	// Access "a" twice — first Get promotes to level-1.
	c.Get("a")
	// Access "a" again — now in level-1.
	c.Get("a")

	// Fill level-0 with new items, evicting "b".
	c.Put("c", 3)
	c.Put("d", 4)

	// "a" should still be in level-1.
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("Get(a) after LRU-2 promotion = %v, %v, want 1, true", v, ok)
	}
	// "b" should have been evicted.
	if _, ok := c.Get("b"); ok {
		t.Fatal("Get(b) = true, want false (should be evicted)")
	}
}

func TestLRU_LRU2_PromotionOnSecondAccess(t *testing.T) {
	c := NewLRUCache(1, 3).LRU2(3)

	c.Put("x", "v1")
	c.Put("y", "v2")

	// First Get: moves x from level-0 to level-1.
	c.Get("x")
	// Second Get: finds x in level-1.
	if v, ok := c.Get("x"); !ok || v != "v1" {
		t.Fatalf("second Get(x) = %v, %v, want v1, true", v, ok)
	}
}

func TestLRU_Concurrency(t *testing.T) {
	c := NewLRUCache(8, 64)

	const goroutines = 50
	const ops = 200

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				key := fmt.Sprintf("g%d-k%d", id, i%10)
				c.Put(key, i)
				c.Get(key)
				if i%3 == 0 {
					c.Del(key)
				}
			}
		}(g)
	}
	wg.Wait()
	// No panic / race means pass. Verify cache still works.
	c.Put("final", "done")
	if v, ok := c.Get("final"); !ok || v != "done" {
		t.Fatalf("Get(final) after concurrency = %v, %v, want done, true", v, ok)
	}
}

func TestLRU_Concurrency_Int64(t *testing.T) {
	c := NewLRUCache(4, 64)

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int64) {
			defer wg.Done()
			c.PutInt64("counter", n)
			c.GetInt64("counter")
		}(int64(i))
	}
	wg.Wait()
}

func TestLRU_ZeroExpiration_GetAfterExpiryDoesNotPanic(t *testing.T) {
	c := NewLRUCache(1, 2, 10*time.Millisecond)
	c.Put("a", 1)
	c.Put("b", 2)
	time.Sleep(30 * time.Millisecond)
	// Both expired; Get should return false without panic.
	if _, ok := c.Get("a"); ok {
		t.Fatal("Get(a) after expiry = true, want false")
	}
	if _, ok := c.Get("b"); ok {
		t.Fatal("Get(b) after expiry = true, want false")
	}
}

func TestLRU_PowerOfTwoBucketCount(t *testing.T) {
	// Non-power-of-two bucket count should be rounded up.
	c := NewLRUCache(3, 4)
	// mask should be 3 (next power of 2 is 4, mask = 3).
	if c.mask != 3 {
		t.Fatalf("mask = %d, want 3", c.mask)
	}
	// Should have 4 shards.
	if len(c.shards) != 4 {
		t.Fatalf("len(shards) = %d, want 4", len(c.shards))
	}
}

func TestLRU_Walk_WithDeleted(t *testing.T) {
	c := NewLRUCache(1, 8)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)
	c.Del("b")

	var keys []string
	c.Walk(func(key string, _ *interface{}, _ []byte, _ int64) bool {
		keys = append(keys, key)
		return true
	})
	// "b" was deleted (sunk to tail, expireAt=0), should be skipped.
	for _, k := range keys {
		if k == "b" {
			t.Fatal("Walk visited deleted key 'b'")
		}
	}
	if len(keys) != 2 {
		t.Fatalf("Walk visited %d keys, want 2 (b deleted)", len(keys))
	}
}

func TestMaskOfNextPowOf2(t *testing.T) {
	tests := []struct {
		in   uint16
		want uint16
	}{
		{1, 0},
		{2, 1},
		{3, 3},
		{4, 3},
		{5, 7},
		{7, 7},
		{8, 7},
		{9, 15},
		{16, 15},
		{255, 255},
		{256, 255},
	}
	for _, tt := range tests {
		if got := maskOfNextPowOf2(tt.in); got != tt.want {
			t.Errorf("maskOfNextPowOf2(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestLRU_ReuseAfterEviction(t *testing.T) {
	// Verify that after eviction, the recycled slot works correctly.
	c := NewLRUCache(1, 2)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3) // evicts "a"
	c.Put("d", 4) // evicts "b"

	if _, ok := c.Get("a"); ok {
		t.Fatal("Get(a) = true, want false")
	}
	if _, ok := c.Get("b"); ok {
		t.Fatal("Get(b) = true, want false")
	}
	if v, ok := c.Get("c"); !ok || v != 3 {
		t.Fatalf("Get(c) = %v, %v, want 3, true", v, ok)
	}
	if v, ok := c.Get("d"); !ok || v != 4 {
		t.Fatalf("Get(d) = %v, %v, want 4, true", v, ok)
	}
}
