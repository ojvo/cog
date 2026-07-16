package util

import (
	"fmt"
	"strconv"
	"strings"
)

// CharCodeAt returns the rune at rune index n in s. It returns 0 when n is out of range.
func CharCodeAt(s string, n int) rune {
	i := 0
	for _, r := range s {
		if i == n {
			return r
		}
		i++
	}
	return 0
}

// ToUnicode returns the per-rune ASCII-escaped representation of s.
// Example: "你好\"" -> []string{"\\u4f60", "\\u597d", "\\\""}.
func ToUnicode(s string) []string {
	out := make([]string, 0, len(s))
	for _, r := range s {
		q := strconv.QuoteToASCII(string(r))
		out = append(out, q[1:len(q)-1])
	}
	return out
}

// Unicode returns the concatenated ASCII-escaped representation of s.
func Unicode(s string) string {
	return strings.Join(ToUnicode(s), "")
}

// ToUC returns a compact per-rune Unicode view, replacing "\\u" with "U"
// while preserving literal backslashes and quotes.
func ToUC(s string) []string {
	out := make([]string, 0, len(s))
	for _, v := range ToUnicode(s) {
		st := strings.ReplaceAll(v, "\\u", "U")
		if st == `\\` {
			st = `\`
		}
		if st == `\"` {
			st = `"`
		}
		out = append(out, st)
	}
	return out
}

// UnicodeToUTF8 decodes JavaScript/JSON-style \uXXXX escape sequences in s.
// Non-escape text is preserved as-is.
func UnicodeToUTF8(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if i+6 <= len(s) && s[i] == '\\' && s[i+1] == 'u' {
			hexPart := s[i+2 : i+6]
			if v, err := strconv.ParseInt(hexPart, 16, 32); err == nil {
				b.WriteString(fmt.Sprintf("%c", rune(v)))
				i += 6
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
