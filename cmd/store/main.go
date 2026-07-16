package main

import (
	"fmt"
	"time"

	"c.n/ojv/cog/store"
)

// This demo shows Cache + SegmentedWAL tiered storage:
//   - Hot layer: in-memory Cache with TTL
//   - Cold layer: SegmentedWAL with persistent disk storage
//   - Write-through: Set writes to both layers
//   - Read fallback: Cache miss falls back to WAL
//   - Runtime stats from both layers
func main() {
	fmt.Println("=== Cog/Store Integration Demo ===")
	fmt.Println("Cache (hot) + SegmentedWAL (cold) tiered storage")
	fmt.Println()

	dir := "./tiered-data"
	wal, err := store.NewSegmentedWAL(dir)
	if err != nil {
		fmt.Printf("Failed to create WAL: %v\n", err)
		return
	}
	defer wal.Close()

	cache := store.NewCache(
		store.WithDefaultExpiration(5*time.Minute),
		store.WithCleanupInterval(time.Minute),
	)

	// 1. Write-through: write to both cache and WAL
	writeThrough := func(key string, val []byte) {
		cache.Set(key, val, store.DefaultExpiration)
		if err := wal.Set(key, val); err != nil {
			fmt.Printf("WAL write error for %s: %v\n", key, err)
		}
	}

	// 2. Read with fallback: cache first, then WAL
	readWithFallback := func(key string) ([]byte, bool) {
		if v, ok := cache.Get(key); ok {
			return v.([]byte), true
		}
		return wal.Get(key)
	}

	// 3. Populate data
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("user:%d", i)
		writeThrough(key, []byte(fmt.Sprintf("payload-%d", i)))
	}
	fmt.Println("[Write] 5 key-value pairs written through Cache → WAL")

	// 4. Read back
	hits := 0
	for i := 0; i < 5; i++ {
		if _, ok := readWithFallback(fmt.Sprintf("user:%d", i)); ok {
			hits++
		}
	}
	fmt.Printf("[Read]  %d/%d hits (cache + wal fallback)\n", hits, 5)

	// 5. Simulate cache eviction and verify WAL fallback
	cache.Delete("user:0")
	if v, ok := readWithFallback("user:0"); ok {
		fmt.Printf("[Fallback] cache miss → WAL hit: %s\n", string(v))
	}

	// 6. Runtime stats from both layers
	cacheItems := cache.ItemCount()
	walStats := wal.GetStats()
	fmt.Printf("[Stats] Cache items=%d | WAL entries=%d segments=%d records=%d\n",
		cacheItems, walStats.Entries, walStats.Segments, walStats.TotalRecords)
	fmt.Println()
	fmt.Println("Demo completed successfully.")
}
