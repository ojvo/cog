package util

import (
	"os"
	"path/filepath"
	"strings"
)

// Gitignore is a minimal .gitignore matcher. It supports the common
// patterns used in real repos: blank lines, comments (#), negation (!),
// directory-only patterns (trailing /), anchored patterns (leading /),
// and the * / ? / [..] wildcards via filepath.Match. It intentionally
// does not implement ** globstar or nested per-directory .gitignore
// files — use GitignoreStack for nested walks.
//
// Paths are slash-separated and relative to the .gitignore's directory.
type Gitignore struct {
	rules []gitignoreRule
}

type gitignoreRule struct {
	pattern  string
	negate   bool
	dirOnly  bool
	anchored bool
}

// LoadGitignore reads the .gitignore at root and returns a matcher.
// A missing or unreadable file yields an empty matcher that ignores
// nothing, so callers can use it unconditionally before a walk.
func LoadGitignore(root string) *Gitignore {
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return &Gitignore{}
	}
	return ParseGitignore(string(data))
}

// ParseGitignore builds a matcher from raw .gitignore file contents.
func ParseGitignore(data string) *Gitignore {
	g := &Gitignore{}
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		r := gitignoreRule{pattern: trimmed}
		if strings.HasPrefix(r.pattern, "!") {
			r.negate = true
			r.pattern = r.pattern[1:]
		}
		if strings.HasSuffix(r.pattern, "/") {
			r.dirOnly = true
			r.pattern = strings.TrimSuffix(r.pattern, "/")
		}
		if strings.HasPrefix(r.pattern, "/") {
			r.anchored = true
			r.pattern = strings.TrimPrefix(r.pattern, "/")
		}
		if r.pattern == "" {
			continue
		}
		g.rules = append(g.rules, r)
	}
	return g
}

// Match reports whether the slash-separated relative path should be
// ignored. Later rules win, so a trailing negation can re-include a
// previously ignored path. isDir indicates whether the path is a
// directory (matters for trailing-/ directory-only rules).
func (g *Gitignore) Match(rel string, isDir bool) bool {
	matched, negate := g.matchResult(rel, isDir)
	return matched && !negate
}

// matchResult reports whether any rule matched rel and, if so, whether
// the winning (last) matching rule was a negation. This lets a
// GitignoreStack combine matchers across nested .gitignore files while
// still honoring negations: a nested "!keep.me" must re-include a path
// a parent .gitignore excluded.
func (g *Gitignore) matchResult(rel string, isDir bool) (matched, negate bool) {
	for _, r := range g.rules {
		if r.dirOnly && !isDir {
			continue
		}
		if r.matchPath(rel) {
			matched = true
			negate = r.negate
		}
	}
	return matched, negate
}

func (r gitignoreRule) matchPath(rel string) bool {
	if r.anchored || strings.Contains(r.pattern, "/") {
		if ok, _ := filepath.Match(r.pattern, rel); ok {
			return true
		}
		// Anchored directory pattern also matches everything beneath it.
		return strings.HasPrefix(rel, r.pattern+"/")
	}
	// Unanchored: match the basename of any path component.
	base := rel
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		base = rel[i+1:]
	}
	if ok, _ := filepath.Match(r.pattern, base); ok {
		return true
	}
	// Match a directory component anywhere in the path.
	for _, part := range strings.Split(rel, "/") {
		if ok, _ := filepath.Match(r.pattern, part); ok {
			return true
		}
	}
	return false
}

// GitignoreStack tracks the chain of .gitignore files from a walk's
// root down to the directory currently being visited. Real repos
// routinely place a .gitignore inside a subdirectory (e.g. a vendored
// tool directory that ignores its own node_modules), and those nested
// rules are invisible to a single root-only matcher. During a
// recursive walk such an unhonored node_modules can swamp the entry
// budget before the walk reaches the files the user wants, so the
// nested files must be applied.
//
// Each frame holds the matcher parsed from a directory's .gitignore
// plus that directory's path relative to the walk root (slash
// separated, "" for the root). A nested matcher's patterns are
// evaluated against the path relative to the directory that owns
// them, matching git's own semantics. Deeper frames win (nearest-file
// wins), mirroring git.
type GitignoreStack struct {
	root   string
	frames []gitignoreStackFrame
}

type gitignoreStackFrame struct {
	relDir string // dir owning the .gitignore, relative to root, slash-sep
	ig     *Gitignore
}

// NewGitignoreStack returns a stack seeded with the root .gitignore
// (if any). The root frame is always present and never popped.
func NewGitignoreStack(root string) *GitignoreStack {
	s := &GitignoreStack{root: root}
	s.frames = append(s.frames, gitignoreStackFrame{relDir: "", ig: LoadGitignore(root)})
	return s
}

// Push loads the .gitignore in dir (an absolute path under root, with
// relDir its slash-separated path relative to root) and adds it to the
// stack. Call when the walk descends into a directory being kept;
// pair with Pop when leaving it. A directory with no .gitignore still
// gets a frame so push/pop bookkeeping stays balanced.
func (s *GitignoreStack) Push(absDir, relDir string) {
	data, err := os.ReadFile(filepath.Join(absDir, ".gitignore"))
	var ig *Gitignore
	if err != nil {
		ig = &Gitignore{}
	} else {
		ig = ParseGitignore(string(data))
	}
	s.frames = append(s.frames, gitignoreStackFrame{relDir: filepath.ToSlash(relDir), ig: ig})
}

// Pop removes the most recently pushed frame. The seeded root frame
// is never popped.
func (s *GitignoreStack) Pop() {
	if len(s.frames) > 1 {
		s.frames = s.frames[:len(s.frames)-1]
	}
}

// Match reports whether rel (slash-separated, relative to the walk
// root) should be ignored, consulting every .gitignore from the root
// down to the current directory. Each frame matches against rel made
// relative to the directory that owns it; a deeper frame's later rules
// win, mirroring git's nearest-file-wins ordering.
func (s *GitignoreStack) Match(rel string, isDir bool) bool {
	ignored := false
	for _, f := range s.frames {
		sub := rel
		if f.relDir != "" {
			prefix := f.relDir + "/"
			if !strings.HasPrefix(rel, prefix) {
				continue
			}
			sub = rel[len(prefix):]
		}
		if matched, neg := f.ig.matchResult(sub, isDir); matched {
			ignored = !neg
		}
	}
	return ignored
}
