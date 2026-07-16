package util

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const defaultMaxLogLen = 500

func SanitizeLogInput(s string) string {
	return SanitizeLogInputWithLimit(s, defaultMaxLogLen)
}

func SanitizeLogInputWithLimit(s string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = defaultMaxLogLen
	}
	runes := []rune(s)
	if len(runes) > maxLen {
		runes = runes[:maxLen]
		s = string(runes) + "...(truncated)"
	}
	var result strings.Builder
	result.Grow(len(s))
	for _, r := range s {
		if r < 32 && r != '\t' {
			result.WriteString(fmt.Sprintf("\\x%02x", r))
		} else if r == 127 {
			result.WriteString("\\x7f")
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// SafeTruncate truncates s to at most maxLen bytes, respecting UTF-8 boundaries.
// If truncation occurs, the result is a valid UTF-8 string with length <= maxLen.
func SafeTruncate(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	for i := maxLen; i >= 0; i-- {
		if utf8.RuneStart(s[i]) {
			return s[:i]
		}
	}
	return s[:maxLen]
}
