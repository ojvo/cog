package util

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrPathEmpty is returned by SafeJoin when the given path is empty.
var ErrPathEmpty = errors.New("path is empty")

// ErrPathAbsolute is returned by SafeJoin when the given path is absolute.
var ErrPathAbsolute = errors.New("absolute path not allowed")

// ErrPathEscapesCWD is returned by SafeJoin when the joined path escapes cwd.
var ErrPathEscapesCWD = errors.New("path escapes cwd")

// SafeJoin joins cwd and path, ensuring the result does not escape cwd.
// It rejects empty paths, absolute paths, and traversal (../../) attempts.
// cwd is used as the base; path must be a relative path that stays within cwd.
//
// Examples:
//
//	SafeJoin("/a/b", "c/d")     // "/a/b/c/d", nil
//	SafeJoin("/a/b", "")        // "", ErrPathEmpty
//	SafeJoin("/a/b", "/c/d")    // "", ErrPathAbsolute
//	SafeJoin("/a/b", "../../c") // "", ErrPathEscapesCWD
func SafeJoin(cwd, path string) (string, error) {
	if path == "" {
		return "", ErrPathEmpty
	}
	cleaned := filepath.Clean(path)
	if filepath.IsAbs(cleaned) {
		return "", ErrPathAbsolute
	}
	joined := filepath.Join(cwd, cleaned)
	rel, err := filepath.Rel(cwd, joined)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", ErrPathEscapesCWD
	}
	return joined, nil
}

// Contains reports whether child is inside parent (or equal to parent).
// Both paths are Cleaned and made absolute before comparison.
// Returns false if either path is empty or cannot be resolved.
//
// Examples:
//
//	Contains("/a/b", "/a/b/c")   // true
//	Contains("/a/b", "/a/b")     // true
//	Contains("/a/b", "/a/bc")    // false (boundary-safe, no prefix false positive)
//	Contains("/a/b", "/a/../a/b/c") // true (Cleaned)
func Contains(parent, child string) bool {
	if parent == "" || child == "" {
		return false
	}
	pAbs, err := filepath.Abs(filepath.Clean(parent))
	if err != nil {
		return false
	}
	cAbs, err := filepath.Abs(filepath.Clean(child))
	if err != nil {
		return false
	}
	sep := string(filepath.Separator)
	return cAbs == pAbs || strings.HasPrefix(cAbs+sep, pAbs+sep)
}

// Overlaps reports whether a and b share any common directory prefix.
// Returns true if a Contains b or b Contains a.
// Returns false if either path is empty or cannot be resolved.
//
// Examples:
//
//	Overlaps("/a/b", "/a/b/c")    // true
//	Overlaps("/a/b/c", "/a/b")    // true
//	Overlaps("/a/b", "/a/b")      // true
//	Overlaps("/a/b", "/x/y")      // false
func Overlaps(a, b string) bool {
	return Contains(a, b) || Contains(b, a)
}
