package util

import "testing"

func TestNormalizeNewlines(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"a\r\nb", "a\nb"},
		{"a\rb", "a\nb"},
		{"a\nb", "a\nb"},
		{"", ""},
		{"\r\n\r\n", "\n\n"},
		{"a\r\nb\r\nc", "a\nb\nc"},
	}
	for _, c := range cases {
		if got := NormalizeNewlines(c.in); got != c.want {
			t.Errorf("NormalizeNewlines(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
