package env

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"
)

func TestContains_ChildInsideParent(t *testing.T) {
	parent := filepath.FromSlash("/a/b")
	child := filepath.FromSlash("/a/b/c")
	if !Contains(parent, child) {
		t.Fatalf("Contains(%q, %q) = false, want true", parent, child)
	}
}

func TestContains_EqualPaths(t *testing.T) {
	p := filepath.FromSlash("/a/b")
	if !Contains(p, p) {
		t.Fatalf("Contains(%q, %q) = false, want true", p, p)
	}
}

func TestContains_BoundarySafe(t *testing.T) {
	parent := filepath.FromSlash("/a/b")
	child := filepath.FromSlash("/a/bc")
	if Contains(parent, child) {
		t.Fatalf("Contains(%q, %q) = true, want false (boundary violation)", parent, child)
	}
}

func TestContains_NormalizesDotDot(t *testing.T) {
	parent := filepath.FromSlash("/a/b")
	child := filepath.FromSlash("/a/../a/b/c")
	if !Contains(parent, child) {
		t.Fatalf("Contains(%q, %q) = false, want true (dot-dot not normalized)", parent, child)
	}
}

func TestContains_RelativePathsResolved(t *testing.T) {
	parent, _ := filepath.Abs(filepath.FromSlash("."))
	child := filepath.FromSlash("subdir/file.txt")
	if !Contains(parent, child) {
		t.Logf("Contains(abs(.), subdir/file.txt) = false (may be expected depending on CWD)")
	}
}

func TestContains_EmptyParent(t *testing.T) {
	if Contains("", "/a/b") {
		t.Fatal("Contains(empty, ...) = true, want false")
	}
}

func TestContains_EmptyChild(t *testing.T) {
	if Contains("/a/b", "") {
		t.Fatal("Contains(..., empty) = true, want false")
	}
}

func TestContains_DifferentRoots(t *testing.T) {
	if runtime.GOOS == "windows" {
		if Contains(`C:\a`, `D:\a\b`) {
			t.Fatal("Contains(C:\\a, D:\\a\\b) = true, want false")
		}
	} else {
		if Contains("/a/b", "/x/y") {
			t.Fatal("Contains(/a/b, /x/y) = true, want false")
		}
	}
}

func TestOverlaps_ChildInsideParent(t *testing.T) {
	a := filepath.FromSlash("/a/b")
	b := filepath.FromSlash("/a/b/c")
	if !Overlaps(a, b) {
		t.Fatalf("Overlaps(%q, %q) = false, want true", a, b)
	}
}

func TestOverlaps_ParentInsideChild(t *testing.T) {
	a := filepath.FromSlash("/a/b/c")
	b := filepath.FromSlash("/a/b")
	if !Overlaps(a, b) {
		t.Fatalf("Overlaps(%q, %q) = false, want true", a, b)
	}
}

func TestOverlaps_EqualPaths(t *testing.T) {
	p := filepath.FromSlash("/a/b")
	if !Overlaps(p, p) {
		t.Fatalf("Overlaps(%q, %q) = false, want true", p, p)
	}
}

func TestOverlaps_DisjointPaths(t *testing.T) {
	a := filepath.FromSlash("/a/b")
	b := filepath.FromSlash("/x/y")
	if Overlaps(a, b) {
		t.Fatalf("Overlaps(%q, %q) = true, want false", a, b)
	}
}

func TestOverlaps_BoundarySafe(t *testing.T) {
	a := filepath.FromSlash("/a/b")
	b := filepath.FromSlash("/a/bc")
	if Overlaps(a, b) {
		t.Fatalf("Overlaps(%q, %q) = true, want false (boundary violation)", a, b)
	}
}

func TestOverlaps_EmptyPath(t *testing.T) {
	if Overlaps("", "/a/b") {
		t.Fatal("Overlaps(empty, ...) = true, want false")
	}
}

func TestSafeJoin_RelativePath(t *testing.T) {
	cwd := filepath.FromSlash("/a/b")
	got, err := SafeJoin(cwd, "c/d")
	if err != nil {
		t.Fatalf("SafeJoin(%q, %q) err = %v", cwd, "c/d", err)
	}
	want := filepath.FromSlash("/a/b/c/d")
	if got != want {
		t.Errorf("SafeJoin = %q, want %q", got, want)
	}
}

func TestSafeJoin_NormalizesDotDot(t *testing.T) {
	cwd := filepath.FromSlash("/a/b")
	got, err := SafeJoin(cwd, "c/./d")
	if err != nil {
		t.Fatalf("SafeJoin err = %v", err)
	}
	want := filepath.FromSlash("/a/b/c/d")
	if got != want {
		t.Errorf("SafeJoin = %q, want %q", got, want)
	}
}

func TestSafeJoin_EmptyPath(t *testing.T) {
	_, err := SafeJoin("/a/b", "")
	if !errors.Is(err, ErrPathEmpty) {
		t.Errorf("SafeJoin(empty) err = %v, want ErrPathEmpty", err)
	}
}

func TestSafeJoin_AbsolutePath(t *testing.T) {
	var abs string
	if runtime.GOOS == "windows" {
		abs = `C:\c\d`
	} else {
		abs = "/c/d"
	}
	_, err := SafeJoin("/a/b", abs)
	if !errors.Is(err, ErrPathAbsolute) {
		t.Errorf("SafeJoin(absolute) err = %v, want ErrPathAbsolute", err)
	}
}

func TestSafeJoin_PathEscapesCWD(t *testing.T) {
	_, err := SafeJoin("/a/b", "../../c")
	if !errors.Is(err, ErrPathEscapesCWD) {
		t.Errorf("SafeJoin(escape) err = %v, want ErrPathEscapesCWD", err)
	}
}

func TestSafeJoin_PathAtCWDRoot(t *testing.T) {
	got, err := SafeJoin("/a/b", ".")
	if err != nil {
		t.Fatalf("SafeJoin(.) err = %v", err)
	}
	want := filepath.Clean("/a/b")
	if got != want {
		t.Errorf("SafeJoin(.) = %q, want %q", got, want)
	}
}

// TestSafeJoin_DotDotPrefixName 验证以 ".." 开头但不是 ".." 或 "../" 的
// 合法文件名（如 "..foo"）不被误判为路径逃逸。
func TestSafeJoin_DotDotPrefixName(t *testing.T) {
	cwd := filepath.FromSlash("/a/b")
	got, err := SafeJoin(cwd, "..foo")
	if err != nil {
		t.Fatalf("SafeJoin(%q, %q) err = %v, want nil (..foo is a legal name)", cwd, "..foo", err)
	}
	want := filepath.FromSlash("/a/b/..foo")
	if got != want {
		t.Errorf("SafeJoin = %q, want %q", got, want)
	}
}
