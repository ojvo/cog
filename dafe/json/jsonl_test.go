package json

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestForEachLine_BasicLines(t *testing.T) {
	input := "line1\nline2\nline3\n"
	var lines []string
	err := ForEachLine(strings.NewReader(input), func(line []byte) error {
		lines = append(lines, string(line))
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"line1\n", "line2\n", "line3\n"}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d", len(lines), len(want))
	}
	for i, l := range lines {
		if l != want[i] {
			t.Errorf("line %d: got %q, want %q", i, l, want[i])
		}
	}
}

func TestForEachLine_NoTrailingNewline(t *testing.T) {
	input := "alpha\nbeta\ngamma"
	var lines []string
	err := ForEachLine(strings.NewReader(input), func(line []byte) error {
		lines = append(lines, string(line))
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"alpha\n", "beta\n", "gamma"}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d", len(lines), len(want))
	}
	for i, l := range lines {
		if l != want[i] {
			t.Errorf("line %d: got %q, want %q", i, l, want[i])
		}
	}
}

func TestForEachLine_EmptyInput(t *testing.T) {
	err := ForEachLine(strings.NewReader(""), func(line []byte) error {
		t.Error("callback should not be called for empty input")
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestForEachLine_SingleLineNoNewline(t *testing.T) {
	err := ForEachLine(strings.NewReader("only"), func(line []byte) error {
		if string(line) != "only" {
			t.Errorf("got %q, want %q", line, "only")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestForEachLine_ErrStop(t *testing.T) {
	input := "a\nb\nc\nd\ne\n"
	count := 0
	err := ForEachLine(strings.NewReader(input), func(line []byte) error {
		count++
		if count == 3 {
			return ErrStop
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Errorf("got %d calls, want 3 (ErrStop after 3rd)", count)
	}
}

func TestForEachLine_CallbackError(t *testing.T) {
	expected := errors.New("custom error")
	input := "a\nb\nc\n"
	err := ForEachLine(strings.NewReader(input), func(line []byte) error {
		return expected
	})
	if !errors.Is(err, expected) {
		t.Errorf("got %v, want %v", err, expected)
	}
}

func TestForEachLine_LargeLineExceedsInitialChunk(t *testing.T) {
	big := bytes.Repeat([]byte("X"), 100_000)
	input := make([]byte, 0, len(big)+1)
	input = append(input, big...)
	input = append(input, '\n')
	err := ForEachLine(bytes.NewReader(input), func(line []byte) error {
		if len(line) != len(big)+1 {
			t.Errorf("got line length %d, want %d", len(line), len(big)+1)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestForEachLine_ManySmallLines(t *testing.T) {
	var sb strings.Builder
	const numLines = 10_000
	for i := 0; i < numLines; i++ {
		sb.WriteString("x\n")
	}
	count := 0
	err := ForEachLine(strings.NewReader(sb.String()), func(line []byte) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != numLines {
		t.Errorf("got %d lines, want %d", count, numLines)
	}
}

func TestForEachLine_NoProgress(t *testing.T) {
	r := &stuckReader{}
	err := ForEachLine(r, func(line []byte) error {
		return nil
	})
	if !errors.Is(err, io.ErrNoProgress) {
		t.Errorf("got %v, want io.ErrNoProgress", err)
	}
}

type stuckReader struct{}

func (s *stuckReader) Read(p []byte) (int, error) {
	return 0, nil
}

func TestForEachLine_PartialReads(t *testing.T) {
	data := "hello\nworld\nfoo\nbar\n"
	r := &slowReader{data: []byte(data), chunkSize: 3}
	var lines []string
	err := ForEachLine(r, func(line []byte) error {
		lines = append(lines, string(line))
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"hello\n", "world\n", "foo\n", "bar\n"}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d", len(lines), len(want))
	}
	for i, l := range lines {
		if l != want[i] {
			t.Errorf("line %d: got %q, want %q", i, l, want[i])
		}
	}
}

type slowReader struct {
	data      []byte
	pos       int
	chunkSize int
}

func (s *slowReader) Read(p []byte) (int, error) {
	if s.pos >= len(s.data) {
		return 0, io.EOF
	}
	n := s.chunkSize
	if s.pos+n > len(s.data) {
		n = len(s.data) - s.pos
	}
	copy(p, s.data[s.pos:s.pos+n])
	s.pos += n
	return n, nil
}

func TestForEachLine_EmptyLines(t *testing.T) {
	input := "a\n\n\nb\n"
	var lines []string
	err := ForEachLine(strings.NewReader(input), func(line []byte) error {
		lines = append(lines, string(line))
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"a\n", "\n", "\n", "b\n"}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d", len(lines), len(want))
	}
}

func TestForEachLine_MixedLargeAndSmall(t *testing.T) {
	big := bytes.Repeat([]byte("Y"), 80_000)
	var input []byte
	input = append(input, big...)
	input = append(input, '\n')
	input = append(input, []byte("small\n")...)
	input = append(input, big...)
	input = append(input, '\n')
	input = append(input, []byte("end\n")...)

	count := 0
	err := ForEachLine(bytes.NewReader(input), func(line []byte) error {
		count++
		switch count {
		case 1:
			if len(line) != len(big)+1 {
				t.Errorf("line 1: got length %d, want %d", len(line), len(big)+1)
			}
		case 2:
			if string(line) != "small\n" {
				t.Errorf("line 2: got %q, want %q", line, "small\n")
			}
		case 3:
			if len(line) != len(big)+1 {
				t.Errorf("line 3: got length %d, want %d", len(line), len(big)+1)
			}
		case 4:
			if string(line) != "end\n" {
				t.Errorf("line 4: got %q, want %q", line, "end\n")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 4 {
		t.Errorf("got %d lines, want 4", count)
	}
}

// ---------------------------------------------------------------------------
// JSONLWriter / JSONLReader tests
// ---------------------------------------------------------------------------

func TestJSONLWriter_Append(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	w, err := NewJSONLWriter(path)
	if err != nil {
		t.Fatalf("NewJSONLWriter: %v", err)
	}

	type entry struct {
		Msg string `json:"msg"`
		N   int    `json:"n"`
	}

	for i := 0; i < 5; i++ {
		if err := w.Append(entry{Msg: "hello", N: i}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	w.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 5 {
		t.Errorf("expected 5 lines, got %d", lines)
	}
}

func TestJSONLReader_ReadAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "read.jsonl")

	w, _ := NewJSONLWriter(path)
	type entry struct {
		Msg string `json:"msg"`
	}
	for i := 0; i < 3; i++ {
		w.Append(entry{Msg: "test"})
	}
	w.Close()

	r := NewJSONLReader()
	results, err := r.ReadAll(path, func() any { return &entry{} })
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 entries, got %d", len(results))
	}
}

func TestJSONLReader_ReadLast(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last.jsonl")

	w, _ := NewJSONLWriter(path)
	type entry struct {
		N int `json:"n"`
	}
	for i := 0; i < 10; i++ {
		w.Append(entry{N: i})
	}
	w.Close()

	r := NewJSONLReader()
	results, err := r.ReadLast(path, 3, func() any { return &entry{} })
	if err != nil {
		t.Fatalf("ReadLast: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 entries, got %d", len(results))
	}
	last := results[len(results)-1].(*entry)
	if last.N != 9 {
		t.Errorf("expected last entry N=9, got %d", last.N)
	}
}

func TestJSONLReader_ReadAll_SkipsCorruptLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.jsonl")

	content := `{"n":1}
{corrupt json}
{"n":3}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r := NewJSONLReader()
	type entry struct {
		N int `json:"n"`
	}
	results, err := r.ReadAll(path, func() any { return &entry{} })
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 valid entries, got %d", len(results))
	}
}

func TestJSONLReader_ReadAll_EmptyLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")

	content := `{"n":1}

{"n":2}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r := NewJSONLReader()
	type entry struct {
		N int `json:"n"`
	}
	results, err := r.ReadAll(path, func() any { return &entry{} })
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 entries (empty lines skipped), got %d", len(results))
	}
}

func TestJSONLReader_ReadAll_NoTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notrail.jsonl")

	content := `{"n":1}
{"n":2}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r := NewJSONLReader()
	type entry struct {
		N int `json:"n"`
	}
	results, err := r.ReadAll(path, func() any { return &entry{} })
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 entries, got %d", len(results))
	}
}

func TestReadLast_PreservesOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "order.jsonl")

	w, _ := NewJSONLWriter(path)
	type entry struct {
		N int `json:"n"`
	}
	for i := 0; i < 10; i++ {
		w.Append(entry{N: i})
	}
	w.Close()

	r := NewJSONLReader()
	results, err := r.ReadLast(path, 5, func() any { return &entry{} })
	if err != nil {
		t.Fatalf("ReadLast: %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(results))
	}
	for i, r := range results {
		e := r.(*entry)
		if e.N != i+5 {
			t.Errorf("entry %d: N=%d, want %d (ring buffer reorder bug)", i, e.N, i+5)
		}
	}
}

func TestReadLast_FewerThanN(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "few.jsonl")

	w, _ := NewJSONLWriter(path)
	type entry struct {
		N int `json:"n"`
	}
	for i := 0; i < 3; i++ {
		w.Append(entry{N: i})
	}
	w.Close()

	r := NewJSONLReader()
	results, err := r.ReadLast(path, 10, func() any { return &entry{} })
	if err != nil {
		t.Fatalf("ReadLast: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 entries (all), got %d", len(results))
	}
	for i, r := range results {
		e := r.(*entry)
		if e.N != i {
			t.Errorf("entry %d: N=%d, want %d", i, e.N, i)
		}
	}
}
