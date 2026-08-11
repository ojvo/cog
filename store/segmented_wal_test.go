package store

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newTestWALDir(t *testing.T) string {
	dir, err := os.MkdirTemp("", "og-wal-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestSegmentedWAL_BasicCRUD(t *testing.T) {
	dir := newTestWALDir(t)
	w, err := NewSegmentedWAL(dir)
	if err != nil {
		t.Fatalf("NewSegmentedWAL failed: %v", err)
	}
	defer w.Close()

	// Set
	if err := w.Set("key1", []byte("value1")); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if err := w.Set("key2", []byte("value2")); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Get
	v, ok := w.Get("key1")
	if !ok || string(v) != "value1" {
		t.Errorf("Get(key1) = %q, %v; want value1, true", string(v), ok)
	}
	v, ok = w.Get("key2")
	if !ok || string(v) != "value2" {
		t.Errorf("Get(key2) = %q, %v; want value2, true", string(v), ok)
	}
	_, ok = w.Get("nonexistent")
	if ok {
		t.Error("Get(nonexistent) should return false")
	}

	// Len
	if w.Len() != 2 {
		t.Errorf("Len() = %d, want 2", w.Len())
	}

	// Keys
	keys := w.Keys()
	if len(keys) != 2 {
		t.Errorf("Keys() len = %d, want 2", len(keys))
	}

	// Overwrite
	if err := w.Set("key1", []byte("updated")); err != nil {
		t.Fatalf("Set overwrite failed: %v", err)
	}
	v, ok = w.Get("key1")
	if !ok || string(v) != "updated" {
		t.Errorf("Get(key1) after overwrite = %q, %v; want updated, true", string(v), ok)
	}

	// Delete
	if err := w.Delete("key1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, ok = w.Get("key1")
	if ok {
		t.Error("Get(key1) after delete should return false")
	}
	if w.Len() != 1 {
		t.Errorf("Len() after delete = %d, want 1", w.Len())
	}

	// Delete nonexistent (no error)
	if err := w.Delete("nokey"); err != nil {
		t.Errorf("Delete(nonexistent) should be nil, got %v", err)
	}
}

func TestSegmentedWAL_GetReturnsCopy(t *testing.T) {
	dir := newTestWALDir(t)
	w, err := NewSegmentedWAL(dir)
	if err != nil {
		t.Fatalf("NewSegmentedWAL failed: %v", err)
	}
	defer w.Close()

	w.Set("k", []byte("original"))
	v, _ := w.Get("k")
	v[0] = 'X' // modify the returned copy

	v2, _ := w.Get("k")
	if string(v2) != "original" {
		t.Errorf("internal value was modified: got %q, want original", string(v2))
	}
}

func TestSegmentedWAL_SetCopiesInput(t *testing.T) {
	dir := newTestWALDir(t)
	w, err := NewSegmentedWAL(dir)
	if err != nil {
		t.Fatalf("NewSegmentedWAL failed: %v", err)
	}
	defer w.Close()

	input := []byte("original")
	if err := w.Set("k", input); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	input[0] = 'X'

	got, ok := w.Get("k")
	if !ok || string(got) != "original" {
		t.Errorf("Set retained caller-owned slice: got %q, %v; want original, true", got, ok)
	}
}

func TestSegmentedWAL_Recover(t *testing.T) {
	dir := newTestWALDir(t)

	// Write data and close
	w1, err := NewSegmentedWAL(dir)
	if err != nil {
		t.Fatalf("NewSegmentedWAL failed: %v", err)
	}
	w1.Set("a", []byte("1"))
	w1.Set("b", []byte("2"))
	w1.Set("c", []byte("3"))
	w1.Delete("b")
	if err := w1.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Reopen and verify recovery
	w2, err := NewSegmentedWAL(dir)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	defer w2.Close()

	if w2.Len() != 2 {
		t.Errorf("after recovery Len() = %d, want 2", w2.Len())
	}
	v, ok := w2.Get("a")
	if !ok || string(v) != "1" {
		t.Errorf("after recovery Get(a) = %q, %v; want 1, true", string(v), ok)
	}
	v, ok = w2.Get("c")
	if !ok || string(v) != "3" {
		t.Errorf("after recovery Get(c) = %q, %v; want 3, true", string(v), ok)
	}
	_, ok = w2.Get("b")
	if ok {
		t.Error("after recovery Get(b) should be deleted")
	}

	// Should be able to continue writing
	w2.Set("d", []byte("4"))
	v, ok = w2.Get("d")
	if !ok || string(v) != "4" {
		t.Errorf("Get(d) after recovery = %q, %v; want 4, true", string(v), ok)
	}
}

func TestSegmentedWAL_SegmentCount(t *testing.T) {
	dir := newTestWALDir(t)
	w, err := NewSegmentedWAL(dir)
	if err != nil {
		t.Fatalf("NewSegmentedWAL failed: %v", err)
	}
	defer w.Close()

	if w.SegmentCount() != 1 {
		t.Errorf("initial SegmentCount = %d, want 1", w.SegmentCount())
	}
}

func TestSegmentedWAL_GetStats(t *testing.T) {
	dir := newTestWALDir(t)
	w, err := NewSegmentedWAL(dir)
	if err != nil {
		t.Fatalf("NewSegmentedWAL failed: %v", err)
	}
	defer w.Close()

	w.Set("k1", []byte("v1"))
	w.Set("k2", []byte("v2"))

	stats := w.GetStats()
	if stats.Dir != dir {
		t.Errorf("stats.Dir = %s, want %s", stats.Dir, dir)
	}
	if stats.Entries != 2 {
		t.Errorf("stats.Entries = %d, want 2", stats.Entries)
	}
	if stats.Segments != 1 {
		t.Errorf("stats.Segments = %d, want 1", stats.Segments)
	}
	if stats.Closed {
		t.Error("stats.Closed should be false")
	}
	if stats.Sequence < 2 {
		t.Errorf("stats.Sequence = %d, want >= 2", stats.Sequence)
	}
	if stats.TotalRecords < 2 {
		t.Errorf("stats.TotalRecords = %d, want >= 2", stats.TotalRecords)
	}
}

func TestSegmentedWAL_SetSyncInterval(t *testing.T) {
	dir := newTestWALDir(t)
	w, err := NewSegmentedWALWithConfig(dir, WALConfig{
		SyncInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewSegmentedWALWithConfig failed: %v", err)
	}
	defer w.Close()

	w.SetSyncInterval(200 * time.Millisecond)

	// Write and verify it works
	w.Set("k", []byte("v"))
	v, ok := w.Get("k")
	if !ok || string(v) != "v" {
		t.Errorf("Get(k) = %q, %v; want v, true", string(v), ok)
	}
}

func TestSegmentedWAL_CloseTwice(t *testing.T) {
	dir := newTestWALDir(t)
	w, err := NewSegmentedWAL(dir)
	if err != nil {
		t.Fatalf("NewSegmentedWAL failed: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	// Second close should be a no-op (no panic, no error)
	if err := w.Close(); err != nil {
		t.Errorf("second Close should return nil, got %v", err)
	}
}

func TestSegmentedWAL_SetAfterClose(t *testing.T) {
	dir := newTestWALDir(t)
	w, err := NewSegmentedWAL(dir)
	if err != nil {
		t.Fatalf("NewSegmentedWAL failed: %v", err)
	}
	w.Close()

	if err := w.Set("k", []byte("v")); err == nil {
		t.Error("Set after Close should return error")
	}
	if err := w.Delete("k"); err == nil {
		t.Error("Delete after Close should return error")
	}
}

func TestSegmentedWAL_EmptyDir(t *testing.T) {
	dir := newTestWALDir(t)
	w, err := NewSegmentedWAL(dir)
	if err != nil {
		t.Fatalf("NewSegmentedWAL on empty dir failed: %v", err)
	}
	defer w.Close()

	if w.Len() != 0 {
		t.Errorf("Len() on empty WAL = %d, want 0", w.Len())
	}
	if len(w.Keys()) != 0 {
		t.Errorf("Keys() on empty WAL = %v, want empty", w.Keys())
	}
}

func TestSegmentedWAL_LargeValue(t *testing.T) {
	dir := newTestWALDir(t)
	w, err := NewSegmentedWAL(dir)
	if err != nil {
		t.Fatalf("NewSegmentedWAL failed: %v", err)
	}
	defer w.Close()

	large := make([]byte, 4096)
	for i := range large {
		large[i] = byte(i % 256)
	}
	w.Set("big", large)
	v, ok := w.Get("big")
	if !ok {
		t.Fatal("Get(big) not found")
	}
	if len(v) != len(large) {
		t.Fatalf("len(v) = %d, want %d", len(v), len(large))
	}
	for i := range large {
		if v[i] != large[i] {
			t.Fatalf("byte %d mismatch: got %d, want %d", i, v[i], large[i])
		}
	}
}

func TestSegmentedWAL_ConcurrentAccess(t *testing.T) {
	dir := newTestWALDir(t)
	w, err := NewSegmentedWAL(dir)
	if err != nil {
		t.Fatalf("NewSegmentedWAL failed: %v", err)
	}
	defer w.Close()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "key-" + string(rune('A'+n))
			w.Set(key, []byte("val"))
			w.Get(key)
		}(i)
	}
	wg.Wait()

	if w.Len() != 20 {
		t.Errorf("Len() after concurrent writes = %d, want 20", w.Len())
	}
}

func TestSegmentedWAL_RecoverMultipleSegments(t *testing.T) {
	dir := newTestWALDir(t)

	// Write enough data to create multiple segments by manually
	// creating segment files that simulate rotation.
	w, err := NewSegmentedWAL(dir)
	if err != nil {
		t.Fatalf("NewSegmentedWAL failed: %v", err)
	}

	// Write some data
	for i := 0; i < 10; i++ {
		key := "key-" + string(rune('A'+i))
		w.Set(key, []byte("value"))
	}
	w.Close()

	// Simulate an additional segment by creating a second wal file
	// with a higher ID and some records
	segPath := filepath.Join(dir, "wal-00000000000000000100.log")
	file, err := os.OpenFile(segPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		t.Fatalf("create second segment: %v", err)
	}
	// Write a record with seq=100
	rec := logRecord{
		Seq:   100,
		Type:  recordTypeSet,
		Key:   "from-second-seg",
		Value: []byte("yes"),
	}
	data, _ := rec.encode()
	file.Write(data)
	file.Close()

	// Reopen - should recover from both segments
	w2, err := NewSegmentedWAL(dir)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	defer w2.Close()

	v, ok := w2.Get("from-second-seg")
	if !ok || string(v) != "yes" {
		t.Errorf("Get(from-second-seg) = %q, %v; want yes, true", string(v), ok)
	}
	// Original keys should also be present
	v, ok = w2.Get("key-A")
	if !ok || string(v) != "value" {
		t.Errorf("Get(key-A) = %q, %v; want value, true", string(v), ok)
	}
}

func TestSegmentedWAL_WALLogger(t *testing.T) {
	dir := newTestWALDir(t)

	logged := false
	SetWALLogger(func(format string, args ...interface{}) {
		logged = true
	})

	w, err := NewSegmentedWAL(dir)
	if err != nil {
		t.Fatalf("NewSegmentedWAL failed: %v", err)
	}
	defer w.Close()

	// Recovery logging should have triggered
	if !logged {
		// Recovery may not log if no segments exist; write, close, reopen
		w.Set("k", []byte("v"))
		w.Close()

		logged = false
		w2, err := NewSegmentedWAL(dir)
		if err != nil {
			t.Fatalf("reopen failed: %v", err)
		}
		if !logged {
			t.Error("WAL logger should have been called during recovery")
		}
		w2.Close()
	}

	// Reset to silent
	SetWALLogger(func(format string, args ...interface{}) {})
}
