package util

import (
	"math/rand"
	"testing"
	"time"
	"unicode/utf8"
)

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern string
		text    string
		want    bool
	}{
		{"hello world", "hello world", true},
		{"jello world", "hello world", false},
		{"hello*", "hello world", true},
		{"jello*", "hello world", false},
		{"hello?world", "hello world", true},
		{"he*o?world", "hello world", true},
		{"he*o?*r*", "hello world", true},
		{"*", "的情况下解析一个", true},
		{"*况下*", "的情况下解析一个", true},
		{"*况?*", "的情况下解析一个", true},
		{"的情况?解析一个", "的情况下解析一个", true},
		{"my-folder/oo*", "my-folder/oo", true},
		{"my-folder/In*", "my-folder/India/Karnataka/", true},
		{"my-folder/In*", "my-folder/Karnataka/India/", false},
		{"my-folder/abc?efg", "my-folder/abc/efg", true},
		{"my-folder/abc?", "my-folder/abcd", true},
		{"my-folder/abc?", "my-folder/abcde", false},
	}
	for i, tc := range cases {
		if got := Match(tc.text, tc.pattern); got != tc.want {
			t.Fatalf("case %d: Match(%q, %q) = %v, want %v", i, tc.text, tc.pattern, got, tc.want)
		}
	}
}

func TestAllowable(t *testing.T) {
	cases := []struct {
		pattern string
		min     string
		max     string
	}{
		{"hell*", "hell", "helm"},
		{"hell?", "hell" + string(rune(0)), "hell" + string(utf8.MaxRune)},
		{"h解析ell*", "h解析ell", "h解析elm"},
		{"h解*ell*", "h解", "h觤"},
	}
	for _, tc := range cases {
		min, max := Allowable(tc.pattern)
		if min != tc.min || max != tc.max {
			t.Fatalf("Allowable(%q) = (%q,%q), want (%q,%q)", tc.pattern, min, max, tc.min, tc.max)
		}
	}
}

func TestMatchRandomInput(t *testing.T) {
	rand.Seed(time.Now().UnixNano())
	patterns := []string{"a*", "?bc", "你*界", "he?lo", "*suffix", "prefix*"}
	texts := []string{"abc", "xbc", "你好世界", "hello", "withsuffix", "prefixandmore"}
	for i := 0; i < 100; i++ {
		_ = Match(texts[rand.Intn(len(texts))], patterns[rand.Intn(len(patterns))])
	}
}
