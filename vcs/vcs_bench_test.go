package vcs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// V5 benchmarks. They exist to decide — with data, not intuition — whether the
// hypothesized hotspots (whole-tree Checkout scan, whole-file-table
// CommitPending, ObjectStore lock granularity) are real bottlenecks under
// realistic shapes:
//
//   - many files          : a large flat tree (file-table size dominates)
//   - deep tree           : nested directories (walk cost dominates)
//   - large file          : a single multi-MB file (I/O + hashing dominates)
//   - frequent small commit: one-file edits on a big tree (per-commit overhead)
//
// Run with: go test -run '^$' -bench . -benchmem ./cog/vcs/

// benchTree writes n files of size bytes each into dir, strung across depth
// nested directories. Returns the file paths.
func benchTree(b *testing.B, dir string, n, size, depth int) []string {
	b.Helper()
	paths := make([]string, 0, n)
	content := make([]byte, size)
	for i := range content {
		content[i] = byte('a' + i%26)
	}
	for i := 0; i < n; i++ {
		sub := ""
		for d := 0; d < depth; d++ {
			sub = filepath.Join(sub, fmt.Sprintf("d%d", (i+d)%8))
		}
		dirPath := filepath.Join(dir, sub)
		if err := os.MkdirAll(dirPath, 0o755); err != nil {
			b.Fatal(err)
		}
		p := filepath.Join(dirPath, fmt.Sprintf("f%06d.txt", i))
		if err := os.WriteFile(p, content, 0o644); err != nil {
			b.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

// benchCommitAll records and commits every file once, then re-commits after
// touching a single file. This is the "commit cost grows with tree size" probe.
func BenchmarkCommit_ManyFiles(b *testing.B) {
	const files = 500
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		dir, err := os.MkdirTemp("", "vcs-bench-*")
		if err != nil {
			b.Fatal(err)
		}
		mgr := NewManager(filepath.Join(dir, ".jabo"), dir)
		if err := mgr.Init(); err != nil {
			b.Fatal(err)
		}
		paths := benchTree(b, dir, files, 256, 1)
		for _, p := range paths {
			if err := mgr.RecordOldState(p); err != nil {
				b.Fatal(err)
			}
		}
		b.StartTimer()

		if _, err := mgr.CommitPending("all", nil, nil); err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		// Touch one file: a single-file edit on an established big tree.
		if err := os.WriteFile(paths[0], []byte("changed"), 0o644); err != nil {
			b.Fatal(err)
		}
		if err := mgr.RecordOldState(paths[0]); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		if _, err := mgr.CommitPending("one", nil, nil); err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		os.RemoveAll(dir)
		b.StartTimer()
	}
}

// BenchmarkCommit_DeepTree probes the same commit path on a deep directory
// layout, where relative-path bookkeeping is heavier.
func BenchmarkCommit_DeepTree(b *testing.B) {
	const files = 300
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		dir, err := os.MkdirTemp("", "vcs-bench-*")
		if err != nil {
			b.Fatal(err)
		}
		mgr := NewManager(filepath.Join(dir, ".jabo"), dir)
		if err := mgr.Init(); err != nil {
			b.Fatal(err)
		}
		paths := benchTree(b, dir, files, 128, 6)
		for _, p := range paths {
			if err := mgr.RecordOldState(p); err != nil {
				b.Fatal(err)
			}
		}
		b.StartTimer()

		if _, err := mgr.CommitPending("deep", nil, nil); err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		os.RemoveAll(dir)
		b.StartTimer()
	}
}

// BenchmarkCheckout_ManyFiles measures restoring a large tree: the whole-tree
// walk plus full in-memory backup of every affected file.
func BenchmarkCheckout_ManyFiles(b *testing.B) {
	const files = 500
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		dir, err := os.MkdirTemp("", "vcs-bench-*")
		if err != nil {
			b.Fatal(err)
		}
		mgr := NewManager(filepath.Join(dir, ".jabo"), dir)
		if err := mgr.Init(); err != nil {
			b.Fatal(err)
		}
		paths := benchTree(b, dir, files, 256, 1)
		for _, p := range paths {
			if err := mgr.RecordOldState(p); err != nil {
				b.Fatal(err)
			}
		}
		snap, err := mgr.CommitPending("all", nil, nil)
		if err != nil {
			b.Fatal(err)
		}
		// Mutate the tree so checkout has real work to do.
		for _, p := range paths {
			if err := os.WriteFile(p, []byte("mutated"), 0o644); err != nil {
				b.Fatal(err)
			}
		}
		b.StartTimer()

		if err := mgr.Checkout(snap.ID); err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		os.RemoveAll(dir)
		b.StartTimer()
	}
}

// BenchmarkCheckout_LargeFile isolates a single multi-MB file: the cost is
// dominated by reading, hashing and writing the object, not the map.
func BenchmarkCheckout_LargeFile(b *testing.B) {
	const size = 4 << 20 // 4 MiB
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		dir, err := os.MkdirTemp("", "vcs-bench-*")
		if err != nil {
			b.Fatal(err)
		}
		mgr := NewManager(filepath.Join(dir, ".jabo"), dir)
		if err := mgr.Init(); err != nil {
			b.Fatal(err)
		}
		p := filepath.Join(dir, "big.bin")
		content := make([]byte, size)
		if err := os.WriteFile(p, content, 0o644); err != nil {
			b.Fatal(err)
		}
		if err := mgr.RecordOldState(p); err != nil {
			b.Fatal(err)
		}
		snap, err := mgr.CommitPending("big", nil, nil)
		if err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("small"), 0o644); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		if err := mgr.Checkout(snap.ID); err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		os.RemoveAll(dir)
		b.StartTimer()
	}
}

// BenchmarkObjectStore_Put measures raw object writes, exposing the global lock
// plus fsync-per-object cost.
func BenchmarkObjectStore_Put(b *testing.B) {
	dir, err := os.MkdirTemp("", "vcs-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	s := NewObjectStore(dir)
	data := make([]byte, 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Vary content slightly so each Put is a new object (no dedup shortcut).
		data[0] = byte(i)
		if _, err := s.PutData(data); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkObjectStore_Get measures reads, including the V1 SHA-256 verify
// added on the read path.
func BenchmarkObjectStore_Get(b *testing.B) {
	dir, err := os.MkdirTemp("", "vcs-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	s := NewObjectStore(dir)
	data := make([]byte, 4096)
	h, err := s.PutData(data)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.GetData(h); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCommit_FrequentSmall measures the steady-state per-commit overhead:
// one-file edit + commit, repeated, on a moderately sized tree.
func BenchmarkCommit_FrequentSmall(b *testing.B) {
	const files = 200
	dir, err := os.MkdirTemp("", "vcs-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	mgr := NewManager(filepath.Join(dir, ".jabo"), dir)
	if err := mgr.Init(); err != nil {
		b.Fatal(err)
	}
	paths := benchTree(b, dir, files, 128, 1)
	for _, p := range paths {
		if err := mgr.RecordOldState(p); err != nil {
			b.Fatal(err)
		}
	}
	if _, err := mgr.CommitPending("base", nil, nil); err != nil {
		b.Fatal(err)
	}

	target := paths[0]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := os.WriteFile(target, []byte(fmt.Sprintf("v%d", i)), 0o644); err != nil {
			b.Fatal(err)
		}
		if err := mgr.RecordOldState(target); err != nil {
			b.Fatal(err)
		}
		if _, err := mgr.CommitPending("edit", nil, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCommit_FrequentSmall_BigTree isolates the per-commit cost over an
// existing large flat tree (20k files in one directory — the worst case for a
// tree structure, because the root tree object itself is large). Setup is done
// before the timed region; each iteration commits one edited file.
func BenchmarkCommit_FrequentSmall_BigTree(b *testing.B) {
	const files = 20000
	dir, err := os.MkdirTemp("", "vcs-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	mgr := NewManager(filepath.Join(dir, ".jabo"), dir)
	if err := mgr.Init(); err != nil {
		b.Fatal(err)
	}
	paths := benchTree(b, dir, files, 16, 1)
	for _, p := range paths {
		if err := mgr.RecordOldState(p); err != nil {
			b.Fatal(err)
		}
	}
	if _, err := mgr.CommitPending("base", nil, nil); err != nil {
		b.Fatal(err)
	}

	target := paths[0]
	payload := make([]byte, 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		payload[0] = byte(i)
		if err := os.WriteFile(target, payload, 0o644); err != nil {
			b.Fatal(err)
		}
		if err := mgr.RecordOldState(target); err != nil {
			b.Fatal(err)
		}
		if _, err := mgr.CommitPending("edit", nil, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCommit_FrequentSmall_NestedTree is the realistic counterpart of
// FrequentSmall_BigTree: the same 20k files, but spread across nested
// directories so each tree node is small. Subtree sharing means a one-file
// commit reads and rewrites only the trees along that file's path, so cost is
// O(path depth) rather than O(tree). This is where content-addressed trees pay
// off; a single flat directory of 20k files is the structure's worst case
// because the root tree itself is huge (Git has the same property).
func BenchmarkCommit_FrequentSmall_NestedTree(b *testing.B) {
	const files = 20000
	dir, err := os.MkdirTemp("", "vcs-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	mgr := NewManager(filepath.Join(dir, ".jabo"), dir)
	if err := mgr.Init(); err != nil {
		b.Fatal(err)
	}
	paths := benchTree(b, dir, files, 16, 4)
	for _, p := range paths {
		if err := mgr.RecordOldState(p); err != nil {
			b.Fatal(err)
		}
	}
	if _, err := mgr.CommitPending("base", nil, nil); err != nil {
		b.Fatal(err)
	}

	target := paths[0]
	payload := make([]byte, 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		payload[0] = byte(i)
		if err := os.WriteFile(target, payload, 0o644); err != nil {
			b.Fatal(err)
		}
		if err := mgr.RecordOldState(target); err != nil {
			b.Fatal(err)
		}
		if _, err := mgr.CommitPending("edit", nil, nil); err != nil {
			b.Fatal(err)
		}
	}
}
