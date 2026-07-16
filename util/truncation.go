package util

import (
	"log"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// TruncateMode selects how truncated content is preserved.
type TruncateMode int

const (
	// TruncateHead keeps only the head; the tail is discarded.
	TruncateHead TruncateMode = iota
	// TruncateHeadTail keeps both head and tail, discarding the middle.
	// Suitable for long outputs where the ending (e.g. error summary) is
	// still valuable.
	TruncateHeadTail
)

// defaultTruncateMarker is the marker inserted where content was dropped.
// %d is substituted with the number of dropped bytes.
const defaultTruncateMarker = "...\n[truncated %d bytes]\n..."

// TruncateOptions configures TruncateOutput.
type TruncateOptions struct {
	// MaxBytes caps the output to at most this many bytes (0 = no byte limit).
	MaxBytes int
	// MaxLines caps the output to at most this many lines (0 = no line limit).
	MaxLines int
	// Mode selects head-only or head+tail preservation.
	Mode TruncateMode
	// Marker is inserted where content was dropped. Empty uses the default.
	// If Marker contains "%d", it is substituted with the dropped byte count.
	Marker string
	// TempFile, when non-empty, receives the full untruncated input before
	// truncation. Write failures are non-fatal (logged only). The caller is
	// responsible for cleanup.
	TempFile string
}

// DefaultTruncateOptions returns options suitable for command output:
// 64KB head+tail, no line limit, default marker, no temp file.
func DefaultTruncateOptions() TruncateOptions {
	return TruncateOptions{
		MaxBytes: 64 * 1024,
		Mode:     TruncateHeadTail,
	}
}

// TruncateOutput truncates s according to opts. When both MaxBytes and MaxLines
// are 0, s is returned unchanged. MaxLines is applied first, then MaxBytes.
//
// UTF-8 boundaries are respected: truncation never splits a multi-byte rune.
// When TempFile is set, the full original s is written there first; a write
// failure is logged but does not block truncation.
func TruncateOutput(s string, opts TruncateOptions) string {
	if opts.MaxBytes == 0 && opts.MaxLines == 0 {
		return s
	}
	// Write the full original content to TempFile before truncating.
	// Failures are non-fatal.
	if opts.TempFile != "" {
		if err := os.WriteFile(opts.TempFile, []byte(s), 0o644); err != nil {
			log.Printf("truncation: temp file write failed (%s): %v", opts.TempFile, err)
		}
	}
	marker := opts.Marker
	if marker == "" {
		marker = defaultTruncateMarker
	}
	if opts.MaxLines > 0 {
		s = truncateLines(s, opts.MaxLines, opts.Mode, marker)
	}
	if opts.MaxBytes > 0 {
		s = truncateBytes(s, opts.MaxBytes, opts.Mode, marker)
	}
	return s
}

// truncateLines limits s to at most maxLines lines. Excess lines are dropped
// (head only, or head+tail depending on mode) and marker is inserted.
func truncateLines(s string, maxLines int, mode TruncateMode, marker string) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return s
	}
	dropped := len(s) // approximate; exact byte count computed below
	if mode == TruncateHead || maxLines < 2 {
		kept := strings.Join(lines[:maxLines], "\n")
		return kept + "\n" + formatMarker(marker, dropped-len(kept))
	}
	// HeadTail: keep first half + last half.
	headN := maxLines / 2
	tailN := maxLines - headN
	if tailN > len(lines)-headN {
		tailN = len(lines) - headN
	}
	head := strings.Join(lines[:headN], "\n")
	tail := strings.Join(lines[len(lines)-tailN:], "\n")
	kept := len(head) + len(tail)
	return head + "\n" + formatMarker(marker, dropped-kept) + "\n" + tail
}

// truncateBytes limits s to at most maxBytes bytes, respecting UTF-8 boundaries.
func truncateBytes(s string, maxBytes int, mode TruncateMode, marker string) string {
	if len(s) <= maxBytes {
		return s
	}
	if mode == TruncateHead {
		kept := safeHead(s, maxBytes)
		return kept + formatMarker(marker, len(s)-len(kept))
	}
	// HeadTail: keep first maxBytes/2 + last maxBytes/2.
	half := maxBytes / 2
	headEnd := safeHeadLen(s, half)
	tailStart := safeTailStart(s, half)
	if tailStart <= headEnd {
		// Overlap: fall back to head-only.
		kept := s[:headEnd]
		return kept + formatMarker(marker, len(s)-len(kept))
	}
	head := s[:headEnd]
	tail := s[tailStart:]
	kept := len(head) + len(tail)
	return head + formatMarker(marker, len(s)-kept) + tail
}

// safeHead returns the longest UTF-8-valid prefix of s with length <= maxLen.
func safeHead(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	return s[:safeHeadLen(s, maxLen)]
}

// safeHeadLen returns the largest index i <= maxLen where s[i] is a rune start.
func safeHeadLen(s string, maxLen int) int {
	if maxLen <= 0 {
		return 0
	}
	if len(s) <= maxLen {
		return len(s)
	}
	i := maxLen
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	if i == 0 {
		i = maxLen // fall back to hard cut
	}
	return i
}

// safeTailStart returns the smallest index i >= len(s)-minTail where s[i] is a
// rune start, ensuring the tail has at most minTail bytes.
func safeTailStart(s string, minTail int) int {
	if minTail <= 0 || len(s) <= minTail {
		return 0
	}
	start := len(s) - minTail
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return start
}

// formatMarker substitutes %d in marker with the dropped byte count.
// If marker has no %d, it is returned as-is.
func formatMarker(marker string, dropped int) string {
	if !strings.Contains(marker, "%d") {
		return marker
	}
	return strings.ReplaceAll(marker, "%d", strconv.Itoa(dropped))
}
