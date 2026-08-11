package json

import (
	"encoding/json"
	"reflect"
	"testing"
)

func b(s string) []byte { return []byte(s) }

func TestStripComments_BlockComment(t *testing.T) {
	jsonc := b(`{"foo": /** this is a block comment */ "bar",
		"num": /* inline */ 42}`)
	want := b(`{"foo":  "bar",
		"num":  42}`)
	got := StripComments(jsonc)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("StripComments() = %q, want %q", got, want)
	}
	if !json.Valid(got) {
		t.Error("result is not valid JSON")
	}
}

func TestStripComments_LineComment(t *testing.T) {
	// Line comment removed; newline preserved (valid JSON whitespace).
	jsonc := b("{\"foo\": // this is a line comment\n\"bar\"}")
	got := StripComments(jsonc)
	if !json.Valid(got) {
		t.Fatalf("result is not valid JSON: %q", got)
	}
	if !bytesOrStrContains(got, `"foo"`) || !bytesOrStrContains(got, `"bar"`) {
		t.Errorf("StripComments line comment: missing key/value in %q", got)
	}
	if bytesOrStrContains(got, "//") {
		t.Errorf("StripComments line comment: comment not removed: %q", got)
	}
}

func TestStripComments_BothCommentTypes(t *testing.T) {
	jsonc := b(`{
		"name": /* block */ "alice",
		// line comment
		"age": /** doc */ 30
	}`)
	got := StripComments(jsonc)
	if !json.Valid(got) {
		t.Errorf("StripComments produced invalid JSON: %q", got)
	}
}

func TestStripComments_CommentsInsideStrings(t *testing.T) {
	// Comment-like sequences inside strings must be preserved.
	jsonc := b(`{"url": "https://example.com/*path*/", "id": "//notacomment"}`)
	got := StripComments(jsonc)
	// Should be unchanged because // and /* are inside a string.
	if string(got) != string(jsonc) {
		t.Errorf("StripComments() = %q, want %q (strings should be untouched)", got, jsonc)
	}
	if !json.Valid(got) {
		t.Error("result is not valid JSON")
	}
}

func TestStripComments_EscapedQuote(t *testing.T) {
	jsonc := b(`{"escaped": "\"//not // a comment\"", "x": 1}`)
	got := StripComments(jsonc)
	if !json.Valid(got) {
		t.Errorf("StripComments on escaped quote: %q (invalid JSON)", got)
	}
	// "x": 1 must still be present.
	if !bytesOrStrContains(got, `"x"`) {
		t.Errorf("escaped quote test: missing \"x\" key, got %q", got)
	}
}

func TestStripComments_NoComments(t *testing.T) {
	input := b(`{"key":"value","num":42}`)
	got := StripComments(input)
	if string(got) != string(input) {
		t.Errorf("StripComments() = %q, want %q", got, input)
	}
}

func TestStripComments_EmptyInput(t *testing.T) {
	got := StripComments([]byte{})
	if len(got) != 0 {
		t.Errorf("StripComments empty = %q, want empty", got)
	}
}

func TestStripComments_SoloLineComment(t *testing.T) {
	// Entire input is a line comment — should be empty valid JSON.
	got := StripComments(b("// just a comment"))
	if string(got) != "" {
		t.Errorf("StripComments solo line = %q, want empty", got)
	}
}

func TestStripComments_SoloBlockComment(t *testing.T) {
	got := StripComments(b("/* just a comment */"))
	if string(got) != "" {
		t.Errorf("StripComments solo block = %q, want empty", got)
	}
}

func TestStripComments_TrailingSlash(t *testing.T) {
	// Lone '/' at end of input — should be preserved (not a comment start).
	input := b(`{"path":"/some/path"}/`)
	got := StripComments(input)
	if !bytesOrStrContains(got, `/`) {
		t.Errorf("trailing slash: result %q, expected slash preserved", got)
	}
}

func TestValidJSONC(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{
			name: "valid block comment",
			data: b(`{"foo":/*comment*/"bar"}`),
			want: true,
		},
		{
			name: "valid line comment",
			data: b("{\"foo\": // comment\n\"bar\"}"),
			want: true,
		},
		{
			name: "invalid - unterminated line comment",
			data: b(`{"foo"://comment without ending`),
			want: false,
		},
		{
			name: "valid plain JSON",
			data: b(`{"key":"value"}`),
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidJSONC(tt.data); got != tt.want {
				t.Errorf("ValidJSONC() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnmarshalJSONC(t *testing.T) {
	jsonc := b(`{
		// user configuration
		"host": /* production host */ "example.com",
		"port": 443
	}`)
	var cfg struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := UnmarshalJSONC(jsonc, &cfg); err != nil {
		t.Fatalf("UnmarshalJSONC: %v", err)
	}
	if cfg.Host != "example.com" {
		t.Errorf("Host = %q, want %q", cfg.Host, "example.com")
	}
	if cfg.Port != 443 {
		t.Errorf("Port = %d, want %d", cfg.Port, 443)
	}
}

func TestUnmarshalJSONC_NestedComment(t *testing.T) {
	// Comment containing a string-like /* that should not confuse the parser.
	jsonc := b(`{
		"a": /* "fake key": 99 */ 1,
		"b": 2
	}`)
	var m map[string]int
	if err := UnmarshalJSONC(jsonc, &m); err != nil {
		t.Fatalf("UnmarshalJSONC: %v", err)
	}
	if m["a"] != 1 || m["b"] != 2 || len(m) != 2 {
		t.Errorf("nested comment: got %v, want {a:1, b:2}", m)
	}
}

func TestUnmarshalJSONC_Invalid(t *testing.T) {
	// Block comment swallows closing brace.
	jsonc := b(`{"foo": /* unterminated block "bar"`)
	var m map[string]any
	err := UnmarshalJSONC(jsonc, &m)
	if err == nil {
		t.Error("UnmarshalJSONC should fail for unterminated block comment")
	}
}

func bytesOrStrContains(data []byte, s string) bool {
	for i := 0; i <= len(data)-len(s); i++ {
		if string(data[i:i+len(s)]) == s {
			return true
		}
	}
	return false
}

func BenchmarkStripComments(b *testing.B) {
	jsonc := []byte(`{"foo": /** this is a block comment */ "bar foo",
		"true": /* true */ false, "number": 42,
		"object": { "test": "done" },
		"array" : [1, 2, 3],
		"url" : "https://github.com",
		"escape":"\"wo//rking" }`)
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_ = StripComments(jsonc)
	}
}
