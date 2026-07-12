package store

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

func TestDbLite_Basic(t *testing.T) {
	dir, err := os.MkdirTemp("", "dblite-test-basic")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	db, err := NewDbLite(dir)
	if err != nil {
		t.Fatalf("Failed to create db: %v", err)
	}
	defer db.Close()

	// Test Set and Get
	key := "test-key"
	value := "test-value"
	if err := db.Set(key, value); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	var result string
	if err := db.Get(key, &result); err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if result != value {
		t.Errorf("Expected %s, got %s", value, result)
	}

	// Test GetKeys
	keys, err := db.GetKeys()
	if err != nil {
		t.Fatalf("GetKeys failed: %v", err)
	}
	if len(keys) != 1 || keys[0] != key {
		t.Errorf("Expected keys [%s], got %v", key, keys)
	}

	// Test Delete
	if err := db.Delete(key); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if err := db.Get(key, &result); err != ErrKeyNotFound {
		t.Errorf("Expected ErrKeyNotFound, got %v", err)
	}
}

func TestDbLite_Persistence(t *testing.T) {
	dir, err := os.MkdirTemp("", "dblite-test-persistence")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Open DB and write data
	db1, err := NewDbLite(dir)
	if err != nil {
		t.Fatal(err)
	}
	
	for i := 0; i < 10; i++ {
		db1.Set(fmt.Sprintf("key-%d", i), i)
	}
	db1.Close()

	// Reopen DB
	db2, err := NewDbLite(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	// Verify data
	for i := 0; i < 10; i++ {
		var val int
		if err := db2.Get(fmt.Sprintf("key-%d", i), &val); err != nil {
			t.Errorf("Get key-%d failed: %v", i, err)
		}
		if val != i {
			t.Errorf("Expected %d, got %d", i, val)
		}
	}
}

func TestDbLite_TTL(t *testing.T) {
	dir, err := os.MkdirTemp("", "dblite-test-ttl")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	db, err := NewDbLite(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	key := "ttl-key"
	if err := db.SetWithTTL(key, "value", 2*time.Second); err != nil {
		t.Fatal(err)
	}

	// Should exist
	var val string
	if err := db.Get(key, &val); err != nil {
		t.Error("Key should exist")
	}

	// Wait for expiration
	time.Sleep(3 * time.Second)

	// Should be expired
	if err := db.Get(key, &val); err != ErrKeyExpired {
		t.Errorf("Expected ErrKeyExpired, got %v", err)
	}
}

func TestDbLite_Encryption(t *testing.T) {
	dir, err := os.MkdirTemp("", "dblite-test-enc")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	key := []byte("1234567890123456") // 16 bytes
	db, err := NewDbLite(dir, WithEncryption(key))
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Set("secret", "message"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	// Verify file content is not plain text (simple check)
	files, _ := os.ReadDir(dir)
	// content, _ := os.ReadFile(filepath.Join(dir, files[0].Name()))
	_ = files
	// This is a loose check, but "message" shouldn't appear plainly if encrypted properly
	// However, json.Marshal might add quotes. 
	// Better: try to open without key or with wrong key
	
	// Open without key - should fail or return garbage
	db2, err := NewDbLite(dir)
	if err != nil {
		t.Fatal(err)
	}
	var val string
	// Depending on implementation, it might fail at JSON unmarshal or return garbage
	err = db2.Get("secret", &val)
	if err == nil && val == "message" {
		t.Error("Should not be able to read encrypted data without key")
	}
	db2.Close()

	// Open with wrong key
	db3, err := NewDbLite(dir, WithEncryption([]byte("wrong-key-123456")))
	if err != nil {
		t.Fatal(err)
	}
	err = db3.Get("secret", &val)
	if err == nil && val == "message" {
		t.Error("Should not be able to read encrypted data with wrong key")
	}
	db3.Close()

	// Open with correct key
	db4, err := NewDbLite(dir, WithEncryption(key))
	if err != nil {
		t.Fatal(err)
	}
	defer db4.Close()
	if err := db4.Get("secret", &val); err != nil || val != "message" {
		t.Error("Should be able to read with correct key")
	}
}

func TestDbLite_Compression(t *testing.T) {
	dir, err := os.MkdirTemp("", "dblite-test-comp")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	db, err := NewDbLite(dir, WithCompression())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	longString := ""
	for i := 0; i < 1000; i++ {
		longString += "test"
	}

	if err := db.Set("long", longString); err != nil {
		t.Fatal(err)
	}

	var val string
	if err := db.Get("long", &val); err != nil || val != longString {
		t.Error("Compression read/write failed")
	}
}

func TestDbLite_Concurrency(t *testing.T) {
	dir, err := os.MkdirTemp("", "dblite-test-conc")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	db, err := NewDbLite(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var wg sync.WaitGroup
	workers := 10
	ops := 100

	// Concurrent Writers
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				if err := db.Set(key, j); err != nil {
					t.Errorf("Set failed: %v", err)
				}
			}
		}(i)
	}

	// Concurrent Readers
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				var val int
				// It might not be there yet, which is fine, but shouldn't panic
				db.Get(key, &val)
			}
		}(i)
	}

	wg.Wait()

	// Verify count
	keys, _ := db.GetKeys()
	if len(keys) != workers*ops {
		t.Errorf("Expected %d keys, got %d", workers*ops, len(keys))
	}
}

func TestDbLite_Merge(t *testing.T) {
	dir, err := os.MkdirTemp("", "dblite-test-merge")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	db, err := NewDbLite(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Write many keys
	for i := 0; i < 100; i++ {
		db.Set(fmt.Sprintf("key-%d", i), i)
	}

	// Update them (creates garbage)
	for i := 0; i < 100; i++ {
		db.Set(fmt.Sprintf("key-%d", i), i+1)
	}

	// Delete some (creates tombstones)
	for i := 0; i < 50; i++ {
		db.Delete(fmt.Sprintf("key-%d", i))
	}

	// Trigger Merge
	if err := db.Merge(); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	// Verify data correctness
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key-%d", i)
		var val int
		err := db.Get(key, &val)

		if i < 50 {
			if err != ErrKeyNotFound {
				t.Errorf("Key %s should be deleted", key)
			}
		} else {
			if err != nil {
				t.Errorf("Key %s should exist", key)
			}
			if val != i+1 {
				t.Errorf("Key %s: expected %d, got %d", key, i+1, val)
			}
		}
	}
}

func TestDbLite_CloseIdempotency(t *testing.T) {
	dir, err := os.MkdirTemp("", "dblite-test-close")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	db, err := NewDbLite(dir)
	if err != nil {
		t.Fatal(err)
	}

	// First close
	if err := db.Close(); err != nil {
		t.Errorf("First close failed: %v", err)
	}

	// Second close - should not panic
	if err := db.Close(); err != nil {
		t.Errorf("Second close failed: %v", err)
	}
}

func TestDbLite_Stats(t *testing.T) {
	dir, err := os.MkdirTemp("", "dblite-test-stats")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	db, err := NewDbLite(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Set("key1", "val1")
	db.Set("key2", "val2")
	db.Delete("key1")
	
	var val string
	db.Get("key2", &val)
	
	stats := db.stats
	// We can't access stats fields directly if they are not exported or if we are in different package
	// But here we are in same package 'ju' (implied by package declaration in test file)
	
	if stats.Writes != 2 { // 2 Sets
		t.Errorf("Expected 2 writes, got %d", stats.Writes)
	}
	if stats.Deletes != 1 {
		t.Errorf("Expected 1 delete, got %d", stats.Deletes)
	}
	if stats.Reads != 1 {
		t.Errorf("Expected 1 read, got %d", stats.Reads)
	}
	if stats.KeyCount != 1 { // key1 deleted, key2 remains
		t.Errorf("Expected 1 key count, got %d", stats.KeyCount)
	}
}
