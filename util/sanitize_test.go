package util

import (
	"strings"
	"testing"
)

func TestSanitizeLogInput(t *testing.T) {
	got := SanitizeLogInput("hello")
	if got != "hello" {
		t.Errorf("SanitizeLogInput = %q, want hello", got)
	}
}

func TestSanitizeLogInputControlChars(t *testing.T) {
	got := SanitizeLogInput("hello\x00world\x01")
	if !strings.Contains(got, "\\x00") || !strings.Contains(got, "\\x01") {
		t.Errorf("SanitizeLogInput = %q, control chars not escaped", got)
	}
}

func TestSanitizeLogInputTruncation(t *testing.T) {
	long := make([]byte, 600)
	for i := range long {
		long[i] = 'a'
	}
	got := SanitizeLogInput(string(long))
	if !strings.Contains(got, "...(truncated)") {
		t.Errorf("long input should be truncated, got len=%d", len(got))
	}
}

func TestSanitizeLogInputWithLimitZero(t *testing.T) {
	got := SanitizeLogInputWithLimit("abc", 0)
	if got != "abc" {
		t.Errorf("zero limit should use default, got %q", got)
	}
}

func TestSanitizeLogInputTab(t *testing.T) {
	got := SanitizeLogInput("hello\tworld")
	if got != "hello\tworld" {
		t.Errorf("tab should be preserved, got %q", got)
	}
}

func TestSanitizeLogInputDEL(t *testing.T) {
	got := SanitizeLogInput("hello\x7f")
	if !strings.Contains(got, "\\x7f") {
		t.Errorf("DEL should be escaped, got %q", got)
	}
}
