package util

import "strings"

// NormalizeNewlines converts Windows CRLF (\r\n) and legacy Mac CR (\r) line
// endings to LF (\n). Input already in LF is returned unchanged.
//
// This must run before text enters components that treat \r and \n
// independently (e.g. textarea sanitizers that map each to \n, which would
// otherwise double the line count for CRLF input).
func NormalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}
