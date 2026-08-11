package json

import "encoding/json"

// StripComments removes // line comments and /* */ block comments from a
// JSONC (JSON with Comments) byte slice. String literals and escape sequences
// are handled correctly — comments inside strings are preserved as-is.
//
// The returned slice shares no memory with the input.
func StripComments(b []byte) []byte {
	return stripComments(b)
}

// UnmarshalJSONC strips comments then calls json.Unmarshal.
func UnmarshalJSONC(data []byte, v any) error {
	return json.Unmarshal(stripComments(data), v)
}

// ValidJSONC reports whether data is valid JSON after stripping comments.
func ValidJSONC(data []byte) bool {
	return json.Valid(stripComments(data))
}

// stripComments implements the JSONC comment-removal state machine.
//
// States are implicit in the scanning loop:
//   - normal:     copy bytes to output, look for comments and string delimiters
//   - inString:   copy bytes, track escape sequences (\\, \")
//   - blockCmt:   skip everything until */
//   - lineCmt:    skip everything until \n
//
// The algorithm processes bytes in a single left-to-right pass with O(n)
// time and a single output allocation (worst-case capacity = len(input)).
func stripComments(b []byte) []byte {
	out := make([]byte, 0, len(b))
	var (
		inString  bool  // inside a JSON string literal
		escaped   bool  // previous character was a backslash inside a string
		blockCmt  bool  // inside a /* */ block comment
		lineCmt   bool  // inside a // line comment
		sawStar   bool  // saw '*' inside a block comment (looking for '/')
	)
	i := 0
	for i < len(b) {
		ch := b[i]
		i++

		// ── line comment: skip, resume on newline ──
		if lineCmt {
			if ch == '\n' {
				lineCmt = false
			}
			continue
		}

		// ── block comment: skip, resume on */ ──
		if blockCmt {
			if sawStar && ch == '/' {
				blockCmt = false
				sawStar = false
			} else {
				sawStar = ch == '*'
			}
			continue
		}

		// ── string literal ──
		if inString {
			out = append(out, ch)
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}

		// ── normal mode: look for strings and comment starts ──
		switch {
		case ch == '"':
			inString = true
			out = append(out, ch)
		case ch == '/':
			// Could be start of // or /*.
			if i < len(b) {
				next := b[i]
				if next == '/' {
					lineCmt = true
					i++
				} else if next == '*' {
					blockCmt = true
					sawStar = false
					i++
				} else {
					// Lone '/' — not a valid JSONC comment start.
					// Output as-is (could be in a future JSON extension
					// or simply handled gracefully).
					out = append(out, ch)
				}
			} else {
				// '/' is the very last byte — output it.
				out = append(out, ch)
			}
		default:
			out = append(out, ch)
		}
	}
	return out
}
