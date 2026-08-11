package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type gobRecord struct {
	ID   int
	Name string
	Tags []string
}

func TestWriteReadGobWithMagic_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.gob")
	magic := []byte("DXP1")

	rec := gobRecord{ID: 7, Name: "会话", Tags: []string{"a", "b"}}
	if err := WriteGobWithMagic(path, magic, &rec, 0o644); err != nil {
		t.Fatal(err)
	}

	var got gobRecord
	if err := ReadGobWithMagic(path, magic, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, rec) {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestReadGobWithMagic_MagicMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "other.gob")
	if err := WriteGobWithMagic(path, []byte("VER2"), gobRecord{ID: 1}, 0o644); err != nil {
		t.Fatal(err)
	}

	var got gobRecord
	if err := ReadGobWithMagic(path, []byte("VER1"), &got); !errors.Is(err, ErrGobMagic) {
		t.Fatalf("want ErrGobMagic, got %v", err)
	}
}

func TestReadGobWithMagic_NotExist(t *testing.T) {
	var got gobRecord
	err := ReadGobWithMagic(filepath.Join(t.TempDir(), "nope.gob"), []byte("DXP1"), &got)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want os.ErrNotExist, got %v", err)
	}
}

func TestReadGobWithMagic_EmptyMagic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plain.gob")
	rec := gobRecord{ID: 3}
	if err := WriteGobWithMagic(path, nil, &rec, 0o644); err != nil {
		t.Fatal(err)
	}
	var got gobRecord
	if err := ReadGobWithMagic(path, nil, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, rec) {
		t.Fatalf("mismatch: %+v", got)
	}
}

// TestReadGobWithMagic_Truncated simulates a half-written file (magic present,
// payload cut off): decode must fail, not hang or panic.
func TestReadGobWithMagic_Truncated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trunc.gob")
	if err := os.WriteFile(path, []byte("DXP1\x00\x01"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got gobRecord
	if err := ReadGobWithMagic(path, []byte("DXP1"), &got); err == nil {
		t.Fatal("want decode error, got nil")
	}
	// Sanity: file was not modified by the read.
	if b, err := os.ReadFile(path); err != nil || !bytes.Equal(b, []byte("DXP1\x00\x01")) {
		t.Fatalf("read must not mutate file: %v %v", b, err)
	}
}
