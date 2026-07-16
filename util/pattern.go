package util

import "unicode/utf8"

// Match reports whether s matches pattern using a simple wildcard grammar:
//   - '*' matches any sequence of characters (including empty)
//   - '?' matches any single character
// The matcher is Unicode-aware and works on UTF-8 rune boundaries.
func Match(s, pattern string) bool {
	if pattern == "*" {
		return true
	}
	return deepMatch(s, pattern)
}

func deepMatch(s, pattern string) bool {
	for len(pattern) > 0 {
		if pattern[0] > 0x7f {
			return deepMatchRune(s, pattern)
		}
		switch pattern[0] {
		default:
			if len(s) == 0 {
				return false
			}
			if s[0] > 0x7f {
				return deepMatchRune(s, pattern)
			}
			if s[0] != pattern[0] {
				return false
			}
		case '?':
			if len(s) == 0 {
				return false
			}
		case '*':
			return deepMatch(s, pattern[1:]) || (len(s) > 0 && deepMatch(s[1:], pattern))
		}
		s = s[1:]
		pattern = pattern[1:]
	}
	return len(s) == 0 && len(pattern) == 0
}

func deepMatchRune(s, pattern string) bool {
	var sr, pr rune
	var srsz, prsz int

	if len(s) > 0 {
		if s[0] > 0x7f {
			sr, srsz = utf8.DecodeRuneInString(s)
		} else {
			sr, srsz = rune(s[0]), 1
		}
	} else {
		sr, srsz = utf8.RuneError, 0
	}
	if len(pattern) > 0 {
		if pattern[0] > 0x7f {
			pr, prsz = utf8.DecodeRuneInString(pattern)
		} else {
			pr, prsz = rune(pattern[0]), 1
		}
	} else {
		pr, prsz = utf8.RuneError, 0
	}

	for pr != utf8.RuneError {
		switch pr {
		default:
			if srsz == utf8.RuneError {
				return false
			}
			if sr != pr {
				return false
			}
		case '?':
			if srsz == utf8.RuneError {
				return false
			}
		case '*':
			return deepMatchRune(s, pattern[prsz:]) || (srsz > 0 && deepMatchRune(s[srsz:], pattern))
		}
		s = s[srsz:]
		pattern = pattern[prsz:]
		if len(s) > 0 {
			if s[0] > 0x7f {
				sr, srsz = utf8.DecodeRuneInString(s)
			} else {
				sr, srsz = rune(s[0]), 1
			}
		} else {
			sr, srsz = utf8.RuneError, 0
		}
		if len(pattern) > 0 {
			if pattern[0] > 0x7f {
				pr, prsz = utf8.DecodeRuneInString(pattern)
			} else {
				pr, prsz = rune(pattern[0]), 1
			}
		} else {
			pr, prsz = utf8.RuneError, 0
		}
	}
	return srsz == 0 && prsz == 0
}

var maxRuneBytes = func() []byte {
	b := make([]byte, 4)
	if utf8.EncodeRune(b, '\U0010FFFF') != 4 {
		panic("invalid rune encoding")
	}
	return b
}()

// Allowable returns the minimum and maximum byte-string bounds represented by
// pattern. If pattern is empty or begins with '*', both bounds are empty.
func Allowable(pattern string) (min, max string) {
	if pattern == "" || pattern[0] == '*' {
		return "", ""
	}
	minb := make([]byte, 0, len(pattern))
	maxb := make([]byte, 0, len(pattern))
	wild := false
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '*' {
			wild = true
			break
		}
		if pattern[i] == '?' {
			minb = append(minb, 0)
			maxb = append(maxb, maxRuneBytes...)
		} else {
			minb = append(minb, pattern[i])
			maxb = append(maxb, pattern[i])
		}
	}
	if wild {
		r, n := utf8.DecodeLastRune(maxb)
		if r != utf8.RuneError && r < utf8.MaxRune {
			r++
			if r > 0x7f {
				b := make([]byte, 4)
				nn := utf8.EncodeRune(b, r)
				maxb = append(maxb[:len(maxb)-n], b[:nn]...)
			} else {
				maxb = append(maxb[:len(maxb)-n], byte(r))
			}
		}
	}
	return string(minb), string(maxb)
}
