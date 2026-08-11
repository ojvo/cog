package csv

import "strings"

// ToSnake converts a CamelCase or mixed identifier to snake_case.
// When screaming is true the result is upper-cased (SCREAMING_SNAKE_CASE),
// otherwise it is lower-cased. Adjacent upper-case letters are treated as
// an acronym and kept together (e.g. "HTTPServer" → "http_server",
// "HTTP2Server" → "http2_server"), so acronyms with trailing digits do not
// split into per-letter fragments.
//
// Whitespace, hyphens and underscores in the input are all normalized to a
// single underscore separator.
//
// Example:
//
//	ToSnake("HTTP2Server", false) → "http2_server"
//	ToSnake("UserID",      true)  → "USER_ID"
//	ToSnake("user-id",     false) → "user_id"
func ToSnake(s string, screaming bool) string {
	const del = '_'
	s = strings.TrimSpace(s)
	n := ""
	for i, v := range s {
		nextCaseIsChanged := false
		if i+1 < len(s) {
			next := s[i+1]
			if (v >= 'A' && v <= 'Z' && next >= 'a' && next <= 'z') ||
				(v >= 'a' && v <= 'z' && next >= 'A' && next <= 'Z') {
				nextCaseIsChanged = true
			}
		}

		if i > 0 && n[len(n)-1] != del && nextCaseIsChanged {
			if v >= 'A' && v <= 'Z' {
				n += string(del) + string(v)
			} else if v >= 'a' && v <= 'z' {
				n += string(v) + string(del)
			}
		} else if v == ' ' || v == '_' || v == '-' {
			n += string(del)
		} else {
			n += string(v)
		}
	}

	if screaming {
		return strings.ToUpper(n)
	}
	return strings.ToLower(n)
}
