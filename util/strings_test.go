package util

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncate_ASCII(t *testing.T) {
	got := Truncate("hello world", 5, "...")
	want := "hello..."
	if got != want {
		t.Errorf("Truncate(%q, 5, ...) = %q, want %q", "hello world", got, want)
	}
}

func TestTruncate_NoTruncationNeeded(t *testing.T) {
	got := Truncate("short", 100, "...")
	if got != "short" {
		t.Errorf("Truncate(short, 100, ...) = %q, want %q", got, "short")
	}
}

func TestTruncate_EmptyString(t *testing.T) {
	got := Truncate("", 10, "...")
	if got != "" {
		t.Errorf(`Truncate("", 10, ...) = %q, want %q`, got, "")
	}
}

func TestTruncate_NoSuffix(t *testing.T) {
	got := Truncate("hello world", 5, "")
	want := "hello"
	if got != want {
		t.Errorf(`Truncate(%q, 5, "") = %q, want %q`, "hello world", got, want)
	}
}

func TestTruncate_UTF8_MultiByte(t *testing.T) {
	// "你好世界" is 4 runes, 12 bytes (3 bytes per CJK rune)
	s := "你好世界"
	// Truncate to 4 bytes: should cut at byte 3 (first rune), not byte 4
	// (which is the middle of the second rune).
	got := Truncate(s, 4, "...")
	want := "你..."
	if got != want {
		t.Errorf("Truncate(%q, 4, ...) = %q, want %q", s, got, want)
	}
	// Verify result is valid UTF-8
	if !utf8.ValidString(got) {
		t.Errorf("Truncate produced invalid UTF-8: %q", got)
	}
}

func TestTruncate_UTF8_Emoji(t *testing.T) {
	// "a😀b" = 1 + 4 + 1 = 6 bytes (emoji is 4 bytes)
	s := "a😀b"
	// Truncate to 3 bytes: should cut after 'a' (byte 1), not in middle of emoji
	got := Truncate(s, 3, "...")
	want := "a..."
	if got != want {
		t.Errorf("Truncate(%q, 3, ...) = %q, want %q", s, got, want)
	}
	if !utf8.ValidString(got) {
		t.Errorf("Truncate produced invalid UTF-8: %q", got)
	}
}

func TestTruncateRunes_ASCII(t *testing.T) {
	got := TruncateRunes("hello world", 5, "...")
	want := "hello..."
	if got != want {
		t.Errorf("TruncateRunes(%q, 5, ...) = %q, want %q", "hello world", got, want)
	}
}

func TestTruncateRunes_UTF8(t *testing.T) {
	s := "你好世界你好世界" // 8 runes
	got := TruncateRunes(s, 4, "...")
	want := "你好世界..."
	if got != want {
		t.Errorf("TruncateRunes(%q, 4, ...) = %q, want %q", s, got, want)
	}
}

func TestTruncateRunes_NoTruncationNeeded(t *testing.T) {
	got := TruncateRunes("短", 10, "...")
	if got != "短" {
		t.Errorf("TruncateRunes(%q, 10, ...) = %q, want %q", "短", got, "短")
	}
}

func TestHeadTail_FitsEntirely(t *testing.T) {
	s := "short"
	got := HeadTail(s, 10, 10, "...")
	if got != s {
		t.Errorf("HeadTail(%q, 10, 10, ...) = %q, want %q", s, got, s)
	}
}

func TestHeadTail_Split(t *testing.T) {
	s := "hello world foo bar"
	got := HeadTail(s, 5, 3, "...")
	want := "hello...bar"
	if got != want {
		t.Errorf("HeadTail(%q, 5, 3, ...) = %q, want %q", s, got, want)
	}
}

func TestHeadTail_UTF8(t *testing.T) {
	s := "你好世界你好世界"
	// head 6 bytes (2 CJK runes), tail 6 bytes (2 CJK runes)
	got := HeadTail(s, 6, 6, "...")
	want := "你好...世界"
	if got != want {
		t.Errorf("HeadTail(%q, 6, 6, ...) = %q, want %q", s, got, want)
	}
	if !utf8.ValidString(got) {
		t.Errorf("HeadTail produced invalid UTF-8: %q", got)
	}
}

func TestSafePrefix_UTF8(t *testing.T) {
	s := "你好世界"
	// 4 bytes = first rune (3 bytes) + should not cut into second rune
	got := safePrefix(s, 4)
	want := "你"
	if got != want {
		t.Errorf("safePrefix(%q, 4) = %q, want %q", s, got, want)
	}
}

func TestSafeSuffix_UTF8(t *testing.T) {
	s := "你好世界"
	// Last 4 bytes from end: should start at last rune boundary
	got := safeSuffix(s, 4)
	want := "界"
	if got != want {
		t.Errorf("safeSuffix(%q, 4) = %q, want %q", s, got, want)
	}
}

func TestTruncate_LongString(t *testing.T) {
	s := strings.Repeat("a", 10000)
	got := Truncate(s, 100, "...")
	if len(got) != 103 {
		t.Errorf("Truncate(10000 a's, 100, ...) length = %d, want 103", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("Truncate should end with suffix")
	}
}

func TestStringSliceContains(t *testing.T) {
	s := []string{"a", "b", "c"}
	if !StringSliceContains(s, "b") {
		t.Error("expected to contain b")
	}
	if StringSliceContains(s, "d") {
		t.Error("expected not to contain d")
	}
}

func TestStringSliceFind(t *testing.T) {
	s := []string{"a", "b", "a", "c"}
	idx := StringSliceFind(s, "a")
	if len(idx) != 2 || idx[0] != 0 || idx[1] != 2 {
		t.Errorf("Find(a) = %v, want [0 2]", idx)
	}
	if idx := StringSliceFind(s, "z"); idx != nil {
		t.Errorf("Find(z) = %v, want nil", idx)
	}
}

func TestStringSliceDiff(t *testing.T) {
	a := []string{"a", "b", "c", "d", "e", "f"}
	b := []string{"b", "d", "f", "g"}
	diff := StringSliceDiff(a, b)
	if StringSliceContains(diff, "b") || StringSliceContains(diff, "d") || StringSliceContains(diff, "f") {
		t.Error("diff should not contain elements from b")
	}
	if !StringSliceContains(diff, "a") || !StringSliceContains(diff, "c") || !StringSliceContains(diff, "e") {
		t.Error("diff should contain a, c, e")
	}
	if len(diff) != 3 {
		t.Errorf("diff length = %d, want 3", len(diff))
	}
}

func TestStringSliceUniq(t *testing.T) {
	in := []string{"a", "b", "a", "c", "b", "d"}
	out := StringSliceUniq(in)
	if len(out) != 4 {
		t.Errorf("len = %d, want 4", len(out))
	}
	expected := []string{"a", "b", "c", "d"}
	for i, v := range expected {
		if out[i] != v {
			t.Errorf("out[%d] = %q, want %q", i, out[i], v)
		}
	}
}

func TestStringSliceUniq_Empty(t *testing.T) {
	if out := StringSliceUniq(nil); out != nil {
		t.Errorf("nil in = %v, want nil", out)
	}
}

func TestStringSliceReverse(t *testing.T) {
	in := []string{"a", "b", "c"}
	out := StringSliceReverse(in)
	want := []string{"c", "b", "a"}
	if len(out) != len(want) {
		t.Fatalf("len = %d, want %d", len(out), len(want))
	}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("out[%d] = %q, want %q", i, out[i], want[i])
		}
	}
	// Original should not be mutated
	if in[0] != "a" {
		t.Error("original slice was mutated")
	}
}
