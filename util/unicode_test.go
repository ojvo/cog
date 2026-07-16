package util

import (
	"reflect"
	"testing"
)

var (
	unicodeEscaped = `\u4f60\u597d\"\\u4f60\\u597d\"`
	unicodeText    = `你好\"\你\好\"`
)

func TestUnicodeToUTF8(t *testing.T) {
	got := UnicodeToUTF8(unicodeEscaped)
	want := unicodeText
	if got != want {
		t.Fatalf("UnicodeToUTF8() = %q, want %q", got, want)
	}
}

func TestCharCodeAt(t *testing.T) {
	r := CharCodeAt(unicodeText, 1)
	if r != '好' {
		t.Fatalf("CharCodeAt(...,1) = %q", r)
	}
	if CharCodeAt(unicodeText, 999) != 0 {
		t.Fatal("out of range should return 0")
	}
}

func TestToUnicode(t *testing.T) {
	got := ToUnicode("你好")
	want := []string{`\u4f60`, `\u597d`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ToUnicode = %#v, want %#v", got, want)
	}
}

func TestUnicode(t *testing.T) {
	got := Unicode("你好")
	want := `\u4f60\u597d`
	if got != want {
		t.Fatalf("Unicode = %q, want %q", got, want)
	}
}

func TestToUC(t *testing.T) {
	got := ToUC(unicodeText)
	want := []string{"U4f60", "U597d", `\`, `"`, `\`, "U4f60", `\`, "U597d", `\`, `"`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ToUC = %#v, want %#v", got, want)
	}
}
