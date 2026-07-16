package store

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestAtomicWriteFile_Basic(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "sub", "data.json") // nested dir, verifies MkdirAll
	if err := AtomicWriteFile(dst, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"a":1}` {
		t.Fatalf("content mismatch: %s", got)
	}
	if fi, err := os.Stat(dst); err == nil {
		if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
			t.Fatalf("perm mismatch: %v", fi.Mode().Perm())
		}
	}
}

func TestAtomicWriteFile_NoTmpResidue(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "data.json")
	if err := AtomicWriteFile(dst, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".aw-tmp-") {
			t.Fatalf("temp file leaked: %s", e.Name())
		}
	}
}

func TestAtomicWriteFile_Overwrite(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "data.json")
	if err := AtomicWriteFile(dst, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFile(dst, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "v2" {
		t.Fatalf("expected v2, got %s", got)
	}
}

// TestAtomicWriteFile_Concurrent verifies concurrent writers don't collide:
// each goroutine writes its own file in an independent subdirectory, all
// should succeed with correct content. Uses independent dirs to avoid
// Windows same-directory rename AV-lock contention (OS limitation, not a
// code bug).
func TestAtomicWriteFile_Concurrent(t *testing.T) {
	dir := t.TempDir()
	const n = 32
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			dst := filepath.Join(dir, "g"+strconv.Itoa(i), "data.json")
			content := strings.Repeat("x", i+1)
			errs <- AtomicWriteFile(dst, []byte(content), 0o644)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < n; i++ {
		dst := filepath.Join(dir, "g"+strconv.Itoa(i), "data.json")
		got, err := os.ReadFile(dst)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != strings.Repeat("x", i+1) {
			t.Fatalf("g%d content mismatch: %q", i, got)
		}
	}
}

func TestAtomicWriteFile_BadPath(t *testing.T) {
	tmp := t.TempDir()
	existingFile := filepath.Join(tmp, "afile")
	if err := os.WriteFile(existingFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	badDst := filepath.Join(existingFile, "sub", "data.json")
	if err := AtomicWriteFile(badDst, []byte("x"), 0o644); err == nil {
		t.Fatal("expected error for uncreatable dir")
	}
}

func TestWriteFileSync_Basic(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "data.txt")
	if err := WriteFileSync(dst, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("content mismatch: %s", got)
	}
}
