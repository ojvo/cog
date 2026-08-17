package vcs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"ojv/cog/store"
)

var (
	ErrInvalidHash      = errors.New("invalid hash format")
	ErrInvalidHashChars = errors.New("invalid hash characters")
)

const hashLength = 64

// ObjectStore is a content-addressed object store. Objects are stored under
// baseDir as <first-2-hex>/<remaining-62-hex>, keyed by their SHA-256 digest,
// which gives natural deduplication and integrity verification for free.
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

// Put reads srcPath and stores its content, returning the content hash.
func (s *ObjectStore) Put(srcPath string) (string, error) {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return "", err
	}
	return s.PutData(data)
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
// existing file. The destination directory is created if missing.
func (s *ObjectStore) Get(hashStr string, dstPath string) error {
	data, err := s.GetData(hashStr)
	if err != nil {
		return err
	}
	return store.AtomicWriteFile(dstPath, data, 0o644)
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

// GetData returns the content of the object identified by hashStr.
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
	return data, nil
}

// PruneObjects deletes objects whose hash is not in keepHashes and whose
// modification time is older than minAge. Files that do not match the object
// naming scheme are left untouched. It returns the number of objects deleted.
func (s *ObjectStore) PruneObjects(keepHashes map[string]bool, minAge time.Duration) (int, error) {
	deleted := 0
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

	type toDelete struct {
		path string
		hash string
	}
	var deleteList []toDelete

	for _, entry := range entries {
		if entry.IsDir() && len(entry.Name()) == 2 {
			subDir := filepath.Join(s.baseDir, entry.Name())
			files, err := os.ReadDir(subDir)
			if err != nil {
				continue
			}

			for _, f := range files {
				if !f.IsDir() {
					hashStr := entry.Name() + f.Name()
					// Only consider valid vcs object hashes, so non-vcs files
					// are never accidentally deleted.
					if isValidHash(hashStr) != nil {
						continue
					}
					if keepHashes[hashStr] {
						continue
					}

					info, err := f.Info()
					if err != nil {
						continue
					}

					if info.ModTime().Before(cutoff) {
						deleteList = append(deleteList, toDelete{
							path: filepath.Join(subDir, f.Name()),
							hash: hashStr,
						})
					}
				}
			}
		}
	}

	for _, item := range deleteList {
		s.mu.Lock()
		err := os.Remove(item.path)
		s.mu.Unlock()

		if err == nil {
			deleted++
		}
	}

	return deleted, nil
}
