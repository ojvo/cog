package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// AtomicWriteFile writes data to path atomically: it creates a random temp
// file in the same directory, writes + fsyncs + closes + chmods, then renames
// it to the target path.
//
// This is the single source of truth for "tmp + rename" atomic writes:
//
//   - Temp file name is random (CreateTemp), so concurrent writers don't collide.
//   - fsync after write, so acknowledged content survives a crash.
//   - rename is atomic, so readers never see a half-written file.
//   - Any failure path cleans up the temp file, leaving no residue.
//
// Not suitable for "multi-file two-phase commit" or "version-exclusive" scenarios
// (see JSONDB batch writers, which have stronger semantics and keep their own
// implementation).
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	if err := atomicWriteNoDirSync(path, data, perm); err != nil {
		return err
	}
	// fsync the parent directory so the rename itself is durable.
	// On Linux/ext4, a crash after rename could lose the directory entry
	// change without this fsync. On Windows, Sync on a directory handle
	// is effectively a no-op (FlushFileBuffers on directories succeeds
	// but does nothing useful), so this is harmless cross-platform.
	syncDir(filepath.Dir(path))
	return nil
}

// BatchFile is one entry of an AtomicWriteFileBatch: the target path, its
// content, and the permission bits to apply.
type BatchFile struct {
	Path string
	Data []byte
	Perm os.FileMode
}

// AtomicWriteFileBatch writes many files with the same durability guarantees as
// AtomicWriteFile, but fsyncs each affected directory only once instead of once
// per file. Restoring a large work tree otherwise pays a directory fsync per
// file, which dominates the cost (measured ~2.6ms/file vs ~0.2ms for a plain
// write).
//
// Semantics: every file is written to a temp file in its target directory,
// fsynced, closed, chmodded and renamed; then each distinct target directory is
// fsynced. On the first error the remaining files are skipped and the error is
// returned; files already renamed stay in place (this is a batch of individual
// atomic writes, not a cross-file transaction — callers who need all-or-nothing
// must provide their own rollback, as Manager.Checkout does).
func AtomicWriteFileBatch(files []BatchFile) error {
	dirs := make(map[string]struct{}, len(files))
	for _, f := range files {
		if err := atomicWriteNoDirSync(f.Path, f.Data, f.Perm); err != nil {
			return err
		}
		dirs[filepath.Dir(f.Path)] = struct{}{}
	}
	for dir := range dirs {
		syncDir(dir)
	}
	return nil
}

// atomicWriteNoDirSync is AtomicWriteFile without the trailing directory fsync,
// factored out so the batch path can deduplicate directory syncs.
func atomicWriteNoDirSync(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("store: mkdir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".aw-tmp-*")
	if err != nil {
		return fmt.Errorf("store: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpPath)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("store: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("store: sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("store: close temp: %w", err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("store: chmod temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("store: rename: %w", err)
	}
	return nil
}

// syncDir fsyncs a directory so renames within it are durable. Best-effort: on
// Windows syncing a directory handle is a no-op, and a directory that cannot be
// opened is ignored (the per-file content fsync already happened).
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
}

// WriteFileSync writes data to path with an fsync before returning, but
// does NOT perform an atomic rename. This is for callers that manage their
// own tmp-file + rename two-phase commit (e.g., JSONDB.BatchUpdate, which
// writes all tmp files first then renames them together). For most use cases,
// prefer AtomicWriteFile which handles rename as well.
//
// On any failure the file is closed and removed.
func WriteFileSync(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("store: create %s: %w", path, err)
	}
	pathForCleanup := path
	cleanup := func() {
		f.Close()
		os.Remove(pathForCleanup)
	}
	if _, err := f.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("store: write %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("store: sync %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(pathForCleanup)
		return fmt.Errorf("store: close %s: %w", path, err)
	}
	return nil
}
