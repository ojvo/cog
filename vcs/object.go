package vcs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"ojv/cog/store"
)

var (
	ErrInvalidHash      = errors.New("invalid hash format")
	ErrInvalidHashChars = errors.New("invalid hash characters")
	ErrCorruptObject    = errors.New("object content does not match its hash")
)

const hashLength = 64

// ObjectStore is a content-addressed object store. Objects are stored under
// baseDir as <first-2-hex>/<remaining-62-hex>, keyed by their SHA-256 digest,
// which gives natural deduplication and integrity verification for free. An
// object file's permission bits are meaningful: they record the executable bit
// of the work-tree file a "plain object" (see Put) refers to, so a read-only
// object file is never literally the source content of anything — the content
// is only ever read through GetData, which verifies the hash.
type ObjectStore struct {
	baseDir string
	mu      sync.RWMutex
}

// NewObjectStore returns an ObjectStore rooted at vcsDir (objects live under
// vcsDir/objects).
func NewObjectStore(vcsDir string) *ObjectStore {
	return &ObjectStore{baseDir: filepath.Join(vcsDir, "objects")}
}

// isValidHash reports whether hashStr is a valid lowercase-hex SHA-256 digest
// (exactly 64 hex chars). It rejects path traversal and other invalid input.
func isValidHash(hashStr string) error {
	if len(hashStr) != hashLength {
		return ErrInvalidHash
	}
	for _, c := range hashStr {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return ErrInvalidHashChars
		}
	}
	return nil
}

// Put reads srcPath and stores its content, returning the content hash and
// whether the source was executable. The executable bit is recorded on the
// object file, so restoring it reproduces the mode; it is returned to the
// caller as well, because the caller needs it to build the tree entry and
// re-reading it would cost another stat and lock. Content is still the sole
// deduplication key: identical bytes from two files with different modes hash
// to the same object, and the last Put wins for the mode.
func (s *ObjectStore) Put(srcPath string) (string, bool, error) {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return "", false, err
	}
	exec := false
	if info, err := os.Stat(srcPath); err == nil {
		exec = info.Mode().Perm()&0o111 != 0
	}
	hashStr, err := s.PutData(data)
	if err != nil {
		return "", false, err
	}
	// Mode is recorded under the store lock: two concurrent Puts of identical
	// content with different modes would otherwise race on the object file's
	// bits, and a reader (objectExec) could observe a half-applied mode.
	if err := s.setObjectExec(hashStr, exec); err != nil {
		return "", false, err
	}
	return hashStr, exec, nil
}

// setObjectExec records the executable bit on an object file, creating the file
// if a caller stored content by another path. It is a no-op when the bit
// already matches, so an unchanged mode never rewrites the file (which would
// needlessly bump its mtime and defeat PruneObjects' min-age check).
func (s *ObjectStore) setObjectExec(hashStr string, exec bool) error {
	if err := isValidHash(hashStr); err != nil {
		return fmt.Errorf("invalid hash: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.baseDir, hashStr[:2], hashStr[2:])
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if (info.Mode().Perm()&0o111 != 0) == exec {
		return nil
	}
	mode := fs.FileMode(0o644)
	if exec {
		mode = 0o755
	}
	return os.Chmod(path, mode)
}

// PutData stores data and returns its content hash. Duplicate content returns
// the same hash without rewriting the object file.
func (s *ObjectStore) PutData(data []byte) (string, error) {
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])

	dstDir := filepath.Join(s.baseDir, hashStr[:2])

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return "", err
	}

	dstPath := filepath.Join(dstDir, hashStr[2:])
	if _, err := os.Stat(dstPath); err == nil {
		return hashStr, nil
	}

	if err := store.AtomicWriteFile(dstPath, data, 0o644); err != nil {
		return "", err
	}
	return hashStr, nil
}

// Get restores the object identified by hashStr into dstPath, overwriting any
// existing file. The destination directory is created if missing. The file is
// written with the object's recorded executability (0o755 when the object
// records the executable bit, 0o644 otherwise), so a restore reproduces the
// mode the content was stored with — a rolled-back script keeps its +x just as
// a checked-out one does.
func (s *ObjectStore) Get(hashStr string, dstPath string) error {
	data, err := s.GetData(hashStr)
	if err != nil {
		return err
	}
	exec, err := s.objectExec(hashStr)
	if err != nil {
		return err
	}
	return store.AtomicWriteFile(dstPath, data, execMode(exec))
}

// objectExec reports whether the object file records the executable bit. The
// object's own mode is the only place executability is stored; a missing object
// is an error because a commit must not record a file whose object is absent.
func (s *ObjectStore) objectExec(hashStr string) (bool, error) {
	if err := isValidHash(hashStr); err != nil {
		return false, fmt.Errorf("invalid hash: %w", err)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	info, err := os.Stat(filepath.Join(s.baseDir, hashStr[:2], hashStr[2:]))
	if err != nil {
		return false, fmt.Errorf("object %s not found: %w", hashStr, err)
	}
	return info.Mode().Perm()&0o111 != 0, nil
}

// Exists reports whether the object identified by hashStr is present.
func (s *ObjectStore) Exists(hashStr string) bool {
	if err := isValidHash(hashStr); err != nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, err := os.Stat(filepath.Join(s.baseDir, hashStr[:2], hashStr[2:]))
	return err == nil
}

// GetData returns the content of the object identified by hashStr, verifying
// that the bytes hash back to hashStr. Content addressing is only an integrity
// guarantee if readers actually re-check it: a truncated, silently-corrupted or
// externally-tampered object file would otherwise be restored into the work
// tree as if valid. The check is one SHA-256 over bytes already in memory.
func (s *ObjectStore) GetData(hashStr string) ([]byte, error) {
	if err := isValidHash(hashStr); err != nil {
		return nil, fmt.Errorf("invalid hash: %w", err)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	srcPath := filepath.Join(s.baseDir, hashStr[:2], hashStr[2:])
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return nil, fmt.Errorf("object %s not found: %w", hashStr, err)
	}

	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != hashStr {
		return nil, fmt.Errorf("%w: object %s", ErrCorruptObject, hashStr)
	}
	return data, nil
}

// getObjectBytes is the tree walker's resolver. It returns the object's bytes
// and preserves the underlying error, so callers can distinguish a missing
// object (which wraps fs.ErrNotExist) from a corrupt one (ErrCorruptObject).
func (s *ObjectStore) getObjectBytes(hashStr string) ([]byte, error) {
	return s.GetData(hashStr)
}

// PruneObjects deletes objects whose hash is not in keepHashes and whose
// modification time is older than minAge. Files that do not match the object
// naming scheme are left untouched. It returns the number of objects deleted.
//
// Deletion is not all-or-nothing: a file that disappears (a concurrent Put
// re-creating an object, or another process's GC) is treated as already gone,
// so a prune racing with itself never fails the whole sweep.
func (s *ObjectStore) PruneObjects(keepHashes map[string]bool, minAge time.Duration) (int, error) {
	cutoff := time.Now().Add(-minAge)

	s.mu.RLock()
	entries, err := os.ReadDir(s.baseDir)
	s.mu.RUnlock()

	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	// Collect candidates without holding the lock: the per-file Stat is I/O
	// and holds no state the lock protects.
	var deleteList []string
	for _, entry := range entries {
		if !entry.IsDir() || len(entry.Name()) != 2 {
			continue
		}
		subDir := filepath.Join(s.baseDir, entry.Name())
		files, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			hashStr := entry.Name() + f.Name()
			// Only consider valid vcs object hashes, so non-vcs files are never
			// accidentally deleted.
			if isValidHash(hashStr) != nil || keepHashes[hashStr] {
				continue
			}
			info, err := f.Info()
			if err != nil || !info.ModTime().Before(cutoff) {
				continue
			}
			deleteList = append(deleteList, filepath.Join(subDir, f.Name()))
		}
	}

	if len(deleteList) == 0 {
		return 0, nil
	}

	// One lock acquisition for the whole sweep instead of one per file. Readers
	// (GetData) and writers (PutData) still exclude the delete phase, so an
	// object can never be deleted while a caller is reading or re-writing it.
	s.mu.Lock()
	defer s.mu.Unlock()

	deleted := 0
	for _, path := range deleteList {
		if err := os.Remove(path); err == nil || os.IsNotExist(err) {
			deleted++
		}
	}
	return deleted, nil
}
