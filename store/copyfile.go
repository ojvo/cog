package store

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// CopyDir copies a plain file tree rooted at src into dst, preserving
// directory structure and file permissions. dst's parent must already exist
// (dst itself is created as needed).
//
// Semantics are intentionally limited to "ordinary file tree replication":
//   - regular files are copied with the source permission bits, atomically
//     (tmp + rename, see AtomicWriteFile), so a crash never leaves a
//     half-written file;
//   - directories are recreated with their source mode;
//   - symlinks, device files, hard links, ACLs, xattrs, mtimes are NOT
//     replicated. Callers needing rsync-grade fidelity should implement their
//     own walk.
//
// It is a plain file-tree copy with no semantic overlap with snapshot/versioned
// stores. Use it for migrations and workspace bootstrap, not for write-ahead
// logs or concurrent-access data.
func CopyDir(dst, src string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return AtomicWriteFile(target, data, info.Mode())
	})
}

// SeedFSOpts configures SeedFSDir.
type SeedFSOpts struct {
	// SkipExisting, when true, leaves an existing destination file untouched
	// (user may have customized it). When false, destination files are always
	// overwritten with the embedded content.
	SkipExisting bool
	// Perm is the permission for newly written destination files.
	// Zero uses 0o644.
	Perm fs.FileMode
}

// SeedFSDir seeds the files under srcRoot of an fs.FS (e.g. embed.FS) into the
// destination directory. Directories under srcRoot are walked recursively.
// Every file is written atomically (see AtomicWriteFile).
//
// SeedFSDir knows nothing about models, providers, or any product directory
// layout; callers pick the destination path and the overwrite policy.
func SeedFSDir(fsys fs.FS, srcRoot, dst string, opts SeedFSOpts) error {
	perm := opts.Perm
	if perm == 0 {
		perm = 0o644
	}
	return fs.WalkDir(fsys, srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if opts.SkipExisting {
			if _, err := os.Stat(target); err == nil {
				return nil
			}
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return fmt.Errorf("store: seed read %s: %w", path, err)
		}
		if err := AtomicWriteFile(target, data, perm); err != nil {
			return fmt.Errorf("store: seed write %s: %w", target, err)
		}
		return nil
	})
}
