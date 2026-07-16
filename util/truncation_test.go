package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateOutput_NoLimit(t *testing.T) {
	s := "hello world\nline2\nline3"
	got := TruncateOutput(s, TruncateOptions{})
	if got != s {
		t.Fatalf("expected original, got %q", got)
	}
}

func TestTruncateOutput_MaxBytesHead(t *testing.T) {
	s := strings.Repeat("A", 100)
	got := TruncateOutput(s, TruncateOptions{MaxBytes: 20, Mode: TruncateHead})
	if !strings.HasPrefix(got, strings.Repeat("A", 20)) {
		t.Fatalf("expected head of 20 A's, got %q", got)
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("expected truncation marker, got %q", got)
	}
}

func TestTruncateOutput_MaxBytesHeadTail(t *testing.T) {
	s := "HEAD" + strings.Repeat("x", 200) + "TAIL"
	got := TruncateOutput(s, TruncateOptions{MaxBytes: 20, Mode: TruncateHeadTail})
	if !strings.HasPrefix(got, "HEAD") {
		t.Fatalf("expected head to start with HEAD, got %q", got)
	}
	if !strings.HasSuffix(got, "TAIL") {
		t.Fatalf("expected tail to end with TAIL, got %q", got)
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("expected truncation marker, got %q", got)
	}
}

func TestTruncateOutput_MaxLines(t *testing.T) {
	s := "line1\nline2\nline3\nline4\nline5"
	got := TruncateOutput(s, TruncateOptions{MaxLines: 3, Mode: TruncateHead})
	if strings.Contains(got, "line4") || strings.Contains(got, "line5") {
		t.Fatalf("expected line4/line5 to be dropped, got %q", got)
	}
	if !strings.Contains(got, "line1") || !strings.Contains(got, "line2") || !strings.Contains(got, "line3") {
		t.Fatalf("expected line1-3 to be kept, got %q", got)
	}
}

func TestTruncateOutput_MaxLinesAndBytes(t *testing.T) {
	s := "aaaa\nbbbb\ncccc\ndddd\neeee"
	got := TruncateOutput(s, TruncateOptions{MaxLines: 3, MaxBytes: 15, Mode: TruncateHead})
	if strings.Contains(got, "dddd") || strings.Contains(got, "eeee") {
		t.Fatalf("expected dddd/eeee dropped by MaxLines, got %q", got)
	}
	if len(got) > 200 {
		t.Fatalf("expected short output, got len=%d", len(got))
	}
}

func TestTruncateOutput_UTF8Safe(t *testing.T) {
	// "你好世界" = 12 bytes (4 runes × 3 bytes each)
	s := "你好世界"
	// MaxBytes=7 with TruncateHead: 7 bytes would split the 3rd rune (世),
	// so we expect 6 bytes ("你好") to be kept.
	got := TruncateOutput(s, TruncateOptions{MaxBytes: 7, Mode: TruncateHead})
	idx := strings.Index(got, "...")
	if idx < 0 {
		t.Fatalf("expected marker in output, got %q", got)
	}
	head := got[:idx]
	if !utf8.ValidString(head) {
		t.Fatalf("head not valid UTF-8: %x", head)
	}
	if head != "你好" {
		t.Fatalf("expected 你好, got %q (len=%d)", head, len(head))
	}
}

func TestTruncateOutput_CustomMarker(t *testing.T) {
	s := strings.Repeat("Z", 100)
	got := TruncateOutput(s, TruncateOptions{MaxBytes: 10, Mode: TruncateHead, Marker: "<<<cut %d>>>"})
	if !strings.Contains(got, "<<<cut") || !strings.Contains(got, ">>>") {
		t.Fatalf("expected custom marker, got %q", got)
	}
}

func TestTruncateOutput_TempFileWritten(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "full.txt")
	s := strings.Repeat("A", 100)
	got := TruncateOutput(s, TruncateOptions{MaxBytes: 10, Mode: TruncateHead, TempFile: tmp})
	if len(got) >= len(s) {
		t.Fatalf("expected truncated output, got len=%d", len(got))
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("temp file not written: %v", err)
	}
	if string(data) != s {
		t.Fatalf("temp file content mismatch: got len=%d, want len=%d", len(data), len(s))
	}
}

func TestTruncateOutput_TempFileWriteErrorNotFatal(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "no_such_dir", "full.txt")
	s := strings.Repeat("A", 100)
	got := TruncateOutput(s, TruncateOptions{MaxBytes: 10, Mode: TruncateHead, TempFile: tmp})
	if len(got) >= len(s) {
		t.Fatalf("expected truncated output despite temp file error, got len=%d", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("expected truncation marker, got %q", got)
	}
}

func TestDefaultTruncateOptions(t *testing.T) {
	opts := DefaultTruncateOptions()
	if opts.MaxBytes != 64*1024 {
		t.Fatalf("expected MaxBytes=64KB, got %d", opts.MaxBytes)
	}
	if opts.Mode != TruncateHeadTail {
		t.Fatalf("expected TruncateHeadTail, got %d", opts.Mode)
	}
	if opts.MaxLines != 0 {
		t.Fatalf("expected MaxLines=0, got %d", opts.MaxLines)
	}
	if opts.TempFile != "" {
		t.Fatalf("expected empty TempFile, got %q", opts.TempFile)
	}
}
