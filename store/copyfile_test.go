package store

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

//go:embed testdata/seedroot
var seedFS embed.FS

func TestCopyDir_PlainTree(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	dst := filepath.Join(t.TempDir(), "dst")
	mustWrite(t, filepath.Join(src, "a.txt"), "A")
	mustWrite(t, filepath.Join(src, "sub", "b.json"), `{"b":1}`)

	if err := CopyDir(dst, src); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dst, "a.txt")); got != "A" {
		t.Fatalf("a.txt: %q", got)
	}
	if got := readFile(t, filepath.Join(dst, "sub", "b.json")); got != `{"b":1}` {
		t.Fatalf("b.json: %q", got)
	}
}

func TestCopyDir_EmptySource(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	dst := filepath.Join(t.TempDir(), "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := CopyDir(dst, src); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("dst not created: %v", err)
	}
}

func TestSeedFSDir_SkipExisting(t *testing.T) {
	dst := t.TempDir()
	mustWrite(t, filepath.Join(dst, "a.txt"), "CUSTOM")

	// First run with SkipExisting: existing a.txt must be preserved.
	if err := SeedFSDir(seedFS, "testdata/seedroot", dst, SeedFSOpts{SkipExisting: true}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dst, "a.txt")); got != "CUSTOM" {
		t.Fatalf("SkipExisting violated: %q", got)
	}
	if got := readFile(t, filepath.Join(dst, "sub", "c.txt")); got != "C\n" {
		t.Fatalf("seeded c.txt: %q", got)
	}

	// Overwrite run: existing a.txt is replaced.
	if err := SeedFSDir(seedFS, "testdata/seedroot", dst, SeedFSOpts{SkipExisting: false}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dst, "a.txt")); got != "A\n" {
		t.Fatalf("overwrite: %q", got)
	}
}

func TestSeedFSDir_NoTmpResidue(t *testing.T) {
	dst := t.TempDir()
	if err := SeedFSDir(seedFS, "testdata/seedroot", dst, SeedFSOpts{SkipExisting: true}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if len(e.Name()) >= 7 && e.Name()[:7] == ".aw-tmp" {
			t.Fatalf("tmp residue left: %s", e.Name())
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var _ fs.FS = seedFS
