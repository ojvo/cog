package json

import (
	"encoding/json"
	"strings"
)

// ExtractJSON extracts the first valid JSON object from s. It tries, in order:
//  1. Direct parse — the entire trimmed string is valid JSON.
//  2. ```json ... ``` fenced block.
//  3. ``` ... ``` fenced block (any language tag, skipped).
//  4. Balanced-brace scan: starting at each '{', find the matching '}' by
//     depth counting; if the candidate parses as JSON return it, otherwise
//     advance to the next '{' and retry.
//  5. Empty string (caller should fall back).
//
// The balanced-scan advancement (pos = start+1 after a failed candidate) is
// what distinguishes this from a naive first-match implementation: a leading
// invalid brace block does not prevent a later valid object from being found.
//
// The depth counter is string-literal aware: braces/quotes inside "..." do not
// participate, so prose that mentions JSON-looking fragments (e.g. a log line
// containing `}` inside a quoted string) cannot truncate or mis-pair the match.
func ExtractJSON(s string) string {
	return extractValue(s, '{', '}')
}

// ExtractJSONArray extracts the first valid JSON array from s using the same
// multi-strategy approach as ExtractJSON, but looking for '[' instead of '{'.
func ExtractJSONArray(s string) string {
	return extractValue(s, '[', ']')
}

// ExtractAny extracts the first valid JSON value — either an object or an
// array — whichever appears first in s, using the same multi-strategy approach
// as ExtractJSON/ExtractJSONArray. Unlike calling ExtractJSON then
// ExtractJSONArray, it never misses an array that precedes the first '{', or
// an object that precedes the first '['.
func ExtractAny(s string) string {
	s = strings.TrimSpace(s)

	// 1. Direct parse (object or array).
	if (strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")) && json.Valid([]byte(s)) {
		return s
	}
	if v := fencedValue(s); v != "" {
		return v
	}

	// 2. Balanced scan for the first value (object or array), advancement on failure.
	return firstBalancedValue(s)
}

// extractValue implements the shared ExtractJSON/ExtractJSONArray pipeline for
// a single open/close delimiter pair.
func extractValue(s string, open, close byte) string {
	s = strings.TrimSpace(s)

	// 1. Direct parse.
	if len(s) > 0 && s[0] == open && json.Valid([]byte(s)) {
		return s
	}
	if v := fencedValue(s); v != "" {
		return v
	}

	// 2. Balanced scan with advancement.
	pos := 0
	for pos < len(s) {
		idx := strings.IndexByte(s[pos:], open)
		if idx < 0 {
			break
		}
		start := pos + idx
		if end, ok := balancedEnd(s, start, open, close); ok {
			candidate := s[start : end+1]
			if json.Valid([]byte(candidate)) {
				return candidate
			}
		}
		pos = start + 1
	}

	// 3. Nothing found.
	return ""
}

// fencedValue returns the content of a ```json / ``` fenced block if it parses
// as valid JSON (object or array); empty otherwise.
func fencedValue(s string) string {
	// ```json ... ```
	if _, after, ok := strings.Cut(s, "```json"); ok {
		if before, _, found := strings.Cut(after, "```"); found {
			candidate := strings.TrimSpace(before)
			if json.Valid([]byte(candidate)) {
				return candidate
			}
		}
	}
	// ``` ... ``` (any language tag).
	if _, after, ok := strings.Cut(s, "```"); ok {
		// Skip the optional language tag on the first line.
		if nl := strings.Index(after, "\n"); nl >= 0 {
			after = after[nl+1:]
		}
		if before, _, found := strings.Cut(after, "```"); found {
			candidate := strings.TrimSpace(before)
			if json.Valid([]byte(candidate)) {
				return candidate
			}
		}
	}
	return ""
}

// firstBalancedValue returns the first brace-balanced JSON value (object or
// array) in s, scanning from the earliest '{' or '[' and advancing past failed
// candidates. Empty if none is balanced.
func firstBalancedValue(s string) string {
	pos := 0
	for pos < len(s) {
		oi := strings.IndexByte(s[pos:], '{')
		ai := strings.IndexByte(s[pos:], '[')
		start, open, close := -1, byte(0), byte(0)
		switch {
		case oi >= 0 && (ai < 0 || oi <= ai):
			start, open, close = pos+oi, '{', '}'
		case ai >= 0:
			start, open, close = pos+ai, '[', ']'
		default:
			return ""
		}
		if end, ok := balancedEnd(s, start, open, close); ok {
			candidate := s[start : end+1]
			if json.Valid([]byte(candidate)) {
				return candidate
			}
		}
		pos = start + 1
	}
	return ""
}

// balancedEnd scans s from start (which must be open) to the matching close,
// depth-counting while skipping string literals and their escapes. It returns
// the index of the matching close and whether one was found.
func balancedEnd(s string, start int, open, close byte) (int, bool) {
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return -1, false
}

// Extract extracts a JSON object from s using ExtractJSON, then unmarshals it
// into dst. Returns false if no valid JSON was found or unmarshal failed.
func Extract(s string, dst any) bool {
	raw := ExtractJSON(s)
	if raw == "" {
		return false
	}
	return json.Unmarshal([]byte(raw), dst) == nil
}

// ExtractArray extracts a JSON array from s using ExtractJSONArray, then
// unmarshals it into dst. Returns false if no valid JSON was found or unmarshal
// failed.
func ExtractArray(s string, dst any) bool {
	raw := ExtractJSONArray(s)
	if raw == "" {
		return false
	}
	return json.Unmarshal([]byte(raw), dst) == nil
}
