package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseGitignore_Basics(t *testing.T) {
	g := ParseGitignore(`# comment
node_modules

*.log
!keep.log
/build
dist/
`)

	cases := []struct {
		rel   string
		isDir bool
		want  bool
	}{
		{"node_modules", true, true},
		{"node_modules", false, true}, // dir-only rule skipped for file, but unanchored name still matches
		{"src/node_modules", true, true},
		{"app.log", false, true},
		{"keep.log", false, false}, // negated
		{"build", true, true},      // anchored
		{"build", false, true},
		{"src/build", false, false}, // anchored doesn't match nested
		{"dist", true, true},        // dir-only
		{"dist", false, false},      // dir-only rule skipped for file
		{"src", false, false},
	}
	for _, c := range cases {
		got := g.Match(c.rel, c.isDir)
		if got != c.want {
			t.Errorf("Match(%q, isDir=%v) = %v; want %v", c.rel, c.isDir, got, c.want)
		}
	}
}

func TestParseGitignore_EmptyAndMissing(t *testing.T) {
	g := ParseGitignore("")
	if g.Match("anything", false) {
		t.Fatal("empty gitignore must not match anything")
	}

	dir := t.TempDir()
	g = LoadGitignore(dir) // no .gitignore present
	if g.Match("anything", false) {
		t.Fatal("missing .gitignore must yield a no-op matcher")
	}
}

func TestParseGitignore_NegationWins(t *testing.T) {
	g := ParseGitignore(`*.log
!important.log
`)
	if !g.Match("debug.log", false) {
		t.Error("debug.log should be ignored")
	}
	if g.Match("important.log", false) {
		t.Error("important.log should be re-included by negation")
	}
}

func TestParseGitignore_AnchoredDirectoryMatchesBeneath(t *testing.T) {
	g := ParseGitignore(`/vendor`)
	if !g.Match("vendor", true) {
		t.Error("anchored vendor dir should match")
	}
	if !g.Match("vendor/foo.go", false) {
		t.Error("anchored vendor pattern should match paths beneath it")
	}
	if g.Match("src/vendor", false) {
		t.Error("anchored vendor should NOT match nested src/vendor")
	}
}

func TestGitignoreStack_RootOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".gitignore"), "node_modules\n*.log\n")
	s := NewGitignoreStack(dir)
	if !s.Match("node_modules", true) {
		t.Error("root .gitignore should ignore node_modules")
	}
	if !s.Match("debug.log", false) {
		t.Error("root .gitignore should ignore *.log")
	}
	if s.Match("src/main.go", false) {
		t.Error("src/main.go should not be ignored")
	}
}

func TestGitignoreStack_NestedGitignore(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".gitignore"), "node_modules\n")
	sub := filepath.Join(dir, "vendor")
	mkdir(t, sub)
	writeFile(t, filepath.Join(sub, ".gitignore"), "*.tmp\n!keep.tmp\n")

	s := NewGitignoreStack(dir)
	s.Push(sub, "vendor")
	if !s.Match("vendor/cache.tmp", false) {
		t.Error("nested .gitignore should ignore *.tmp under vendor/")
	}
	if s.Match("vendor/keep.tmp", false) {
		t.Error("nested negation should re-include keep.tmp")
	}
	if s.Match("cache.tmp", false) {
		t.Error("nested .gitignore should NOT affect root-level files")
	}
	s.Pop()
	if s.Match("vendor/cache.tmp", false) {
		t.Error("after Pop, nested rules should no longer apply")
	}
}

func TestGitignoreStack_DeeperFrameWins(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".gitignore"), "secrets\n")
	sub := filepath.Join(dir, "app")
	mkdir(t, sub)
	writeFile(t, filepath.Join(sub, ".gitignore"), "!secrets\n")

	s := NewGitignoreStack(dir)
	if !s.Match("secrets", false) {
		t.Error("root should ignore secrets")
	}
	s.Push(sub, "app")
	if s.Match("app/secrets", false) {
		t.Error("nested negation should re-include app/secrets")
	}
}

func TestGitignoreStack_PopRootNeverRemoved(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".gitignore"), "node_modules\n")
	s := NewGitignoreStack(dir)
	s.Pop() // should be a no-op on root frame
	s.Pop()
	if !s.Match("node_modules", true) {
		t.Error("root frame must survive excessive Pops")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
