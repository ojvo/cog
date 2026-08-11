package util

import "unicode/utf8"

// Truncate 返回 s 截断到至多 maxBytes 字节的结果，不会在多字节 UTF-8
// 字符的中间截断。如果字符串被截断，suffix 会被追加（总长度可能
// 超过 maxBytes，超出量为 len(suffix)）。
//
// 用于替代 s[:n] 这种会破坏 emoji、CJK 等多字节字符的写法。
func Truncate(s string, maxBytes int, suffix string) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + suffix
}

// TruncateRunes 返回 s 截断到至多 maxRunes 个 rune 的结果，不会截断
// 字符中间。如果被截断则追加 suffix。
func TruncateRunes(s string, maxRunes int, suffix string) string {
	n := utf8.RuneCountInString(s)
	if n <= maxRunes {
		return s
	}
	i := 0
	for count := 0; count < maxRunes; count++ {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i] + suffix
}

// HeadTail 返回 s 的前 headBytes 字节和后 tailBytes 字节，中间用
// middle 连接，不会在 UTF-8 字符边界截断。如果 s 总长不超过
// headBytes+tailBytes，则原样返回。
func HeadTail(s string, headBytes, tailBytes int, middle string) string {
	if len(s) <= headBytes+tailBytes {
		return s
	}
	head := safePrefix(s, headBytes)
	tail := safeSuffix(s, tailBytes)
	return head + middle + tail
}

// safePrefix 返回 s 开头至多 maxBytes 字节，不截断多字节字符。
func safePrefix(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// safeSuffix 返回 s 末尾至多 maxBytes 字节，不截断多字节字符。
func safeSuffix(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	start := len(s) - maxBytes
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

// StringSliceContains reports whether s contains e.
func StringSliceContains(s []string, e string) bool {
	for _, v := range s {
		if v == e {
			return true
		}
	}
	return false
}

// StringSliceFind returns all indices in ss where the value equals s.
// Returns nil if not found.
func StringSliceFind(ss []string, s string) []int {
	var indices []int
	for i, v := range ss {
		if v == s {
			indices = append(indices, i)
		}
	}
	return indices
}

// StringSliceDiff returns elements in a that are not in b.
func StringSliceDiff(a, b []string) []string {
	var diff []string
	for _, e := range a {
		if !StringSliceContains(b, e) {
			diff = append(diff, e)
		}
	}
	return diff
}

// StringSliceUniq returns a deduplicated copy of ss, preserving the first
// occurrence order. Returns nil if ss is nil.
func StringSliceUniq(ss []string) []string {
	if ss == nil {
		return nil
	}
	out := make([]string, 0, len(ss))
	seen := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

// StringSliceReverse returns a new slice with elements in reverse order.
func StringSliceReverse(ss []string) []string {
	rev := make([]string, len(ss))
	for i, j := 0, len(ss)-1; i < len(ss); i, j = i+1, j-1 {
		rev[j] = ss[i]
	}
	return rev
}
