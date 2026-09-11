package env

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestWithin(t *testing.T) {
	ws := t.TempDir()
	if !Within(ws, ws) {
		t.Fatal("workspace root must be within itself")
	}
	if !Within(ws, filepath.Join(ws, "dir", "file.go")) {
		t.Fatal("nested path must be within workspace")
	}
	if Within(ws, filepath.Join(ws, "..", "outside.go")) {
		t.Fatal("sibling path must not be within workspace")
	}
	if Within(ws, filepath.Join(ws, "relative.go")) == false {
		t.Fatal("joined path must be within workspace")
	}
}

// TestWithin_RelativeResolvedAgainstRoot 钉死基准语义：Within 的相对路径相对 root
// 解析（与 Contains 相对 CWD 不同），因此纯相对子路径必须判为界内。
func TestWithin_RelativeResolvedAgainstRoot(t *testing.T) {
	ws := t.TempDir()
	if !Within(ws, filepath.Join("dir", "file.go")) {
		t.Fatal("relative path must be resolved against the workspace root")
	}
	if !Within(ws, filepath.Join("dir", "..", "a.go")) {
		t.Fatal("relative dot-dot below root must stay within")
	}
	if Within(ws, filepath.Join("..", "escape.go")) {
		t.Fatal("relative path escaping root must not be within")
	}
}

func TestOutside(t *testing.T) {
	ws := t.TempDir()
	if !Outside(ws, filepath.Join(ws, "..", "outside.go")) {
		t.Fatal("sibling path must be provably outside workspace")
	}
	if Outside(ws, filepath.Join(ws, "dir", "file.go")) {
		t.Fatal("nested path must not be outside workspace")
	}
	if Outside(ws, ws) {
		t.Fatal("workspace root must not be outside itself")
	}
}

// TestOutside_RelativeResolvedAgainstRoot 与 Within 基准一致，并保留 fail-closed：
// 空路径不可解析，必须返回 false（不得误判为“已证明在外”）。
func TestOutside_RelativeResolvedAgainstRoot(t *testing.T) {
	ws := t.TempDir()
	if !Outside(ws, filepath.Join("..", "escape.go")) {
		t.Fatal("relative escaping path must be outside root")
	}
	if Outside(ws, filepath.Join("dir", "file.go")) {
		t.Fatal("relative nested path must not be outside root")
	}
	if Outside(ws, "   ") {
		t.Fatal("empty path must be unresolvable, not proven outside")
	}
}

func TestCleanWithin(t *testing.T) {
	ws := t.TempDir()
	if got := CleanWithin(ws, filepath.Join("dir", "..", "a.go")); got != filepath.Join(ws, "a.go") {
		t.Fatalf("CleanWithin(relative) = %q", got)
	}
	if got := CleanWithin(ws, filepath.Join(ws, "b.go")); got != filepath.Join(ws, "b.go") {
		t.Fatalf("CleanWithin(absolute) = %q", got)
	}
	if got := CleanWithin(ws, filepath.Join("..", "escape.go")); got != "" {
		t.Fatalf("CleanWithin(escape) = %q; want empty", got)
	}
	if got := CleanWithin(ws, "   "); got != "" {
		t.Fatalf("CleanWithin(empty) = %q; want empty", got)
	}
}

func TestContainmentBoundarySafe(t *testing.T) {
	ws := t.TempDir()
	sibling := ws + "-sibling"
	if Within(ws, sibling) {
		t.Fatalf("prefix sibling %q must not be within %q", sibling, ws)
	}
	if !Outside(ws, sibling) {
		t.Fatalf("prefix sibling %q must be outside %q", sibling, ws)
	}
	if Contains(ws, sibling) {
		t.Fatalf("Contains(%q, %q) = true, want false (prefix false positive)", ws, sibling)
	}
}

// TestContains_Self 自身包含自身：Rel 返回 "."，必须判为包含。
func TestContains_Self(t *testing.T) {
	ws := t.TempDir()
	if !Contains(ws, ws) {
		t.Fatal("a directory must contain itself")
	}
}

// TestContains_RootContainsEverything 根路径包含其下任意路径。
func TestContains_RootContainsEverything(t *testing.T) {
	vol := filepath.VolumeName(t.TempDir()) + string(filepath.Separator)
	if !Contains(vol, t.TempDir()) {
		t.Fatalf("volume root %q must contain any path on it", vol)
	}
}

// TestContains_EmptyArgs 空参不可解析，必须返回 false（fail-closed）。
func TestContains_EmptyArgs(t *testing.T) {
	ws := t.TempDir()
	if Contains("", ws) || Contains(ws, "") || Contains("", "") {
		t.Fatal("empty arguments must never be contained")
	}
}

// TestWithin_And_Contains_AgreeOnCase 钉死族内一致性：Windows 大小写不敏感，
// Within（Rel 实现）与 Contains（同样 Rel 实现）对同一大小写变体必须给出一致结论。
func TestWithin_And_Contains_AgreeOnCase(t *testing.T) {
	ws := t.TempDir()
	child := filepath.Join(ws, "Sub", "File.go")
	if !Within(ws, child) {
		t.Fatal("nested absolute path must be within")
	}
	if !Contains(ws, child) {
		t.Fatal("Contains must agree with Within on a nested path")
	}
	if runtime.GOOS != "windows" {
		return
	}
	upper := filepath.Join(ws, "SUB", "FILE.GO")
	if Within(ws, upper) != Contains(ws, upper) {
		t.Fatalf("Within(%q) and Contains(%q) disagree on case variant", ws, upper)
	}
	if !Contains(ws, upper) {
		t.Fatal("Windows containment must be case-insensitive")
	}
}
