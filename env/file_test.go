package env

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFile_BasicOps(t *testing.T) {
	dir, err := os.MkdirTemp("", "file-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	filePath := filepath.Join(dir, "test.txt")

	// Write
	content := "hello world"
	if err := WriteString(filePath, content, WriteModeCreate); err != nil {
		t.Fatalf("WriteString failed: %v", err)
	}

	// Exists
	if !Exists(filePath) {
		t.Error("File should exist")
	}
	if !IsFile(filePath) {
		t.Error("Should be a file")
	}
	if IsDir(filePath) {
		t.Error("Should not be a dir")
	}

	// Read
	readContent, err := ReadString(filePath)
	if err != nil {
		t.Fatalf("ReadString failed: %v", err)
	}
	if readContent != content {
		t.Errorf("Expected %s, got %s", content, readContent)
	}

	// Size
	size, err := GetFileSize(filePath)
	if err != nil {
		t.Fatalf("GetFileSize failed: %v", err)
	}
	if size != int64(len(content)) {
		t.Errorf("Expected size %d, got %d", len(content), size)
	}

	// Copy
	copyPath := filepath.Join(dir, "copy.txt")
	if err := CopyFile(filePath, copyPath); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}
	if !Exists(copyPath) {
		t.Error("Copy file should exist")
	}
	copyContent, _ := ReadString(copyPath)
	if copyContent != content {
		t.Error("Copy content mismatch")
	}

	// Move
	movePath := filepath.Join(dir, "move.txt")
	if err := MoveFile(copyPath, movePath); err != nil {
		t.Fatalf("MoveFile failed: %v", err)
	}
	if Exists(copyPath) {
		t.Error("Original file should be gone after move")
	}
	if !Exists(movePath) {
		t.Error("Moved file should exist")
	}

	// Delete
	if err := RemoveAll(movePath); err != nil {
		t.Fatalf("RemoveAll failed: %v", err)
	}
	if Exists(movePath) {
		t.Error("File should be deleted")
	}
}

func TestFile_DirectoryOps(t *testing.T) {
	dir, err := os.MkdirTemp("", "dir-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Create subdirs
	subDir := filepath.Join(dir, "sub", "deep")
	if err := CreateDir(subDir); err != nil {
		t.Fatalf("CreateDir failed: %v", err)
	}
	if !IsDir(subDir) {
		t.Error("Should be a directory")
	}

	// Create files
	WriteString(filepath.Join(dir, "root.txt"), "root", WriteModeCreate)
	WriteString(filepath.Join(subDir, "deep.txt"), "deep", WriteModeCreate)

	// ListFiles
	files, err := ListFiles(dir, &ListOptions{Recursive: true, IncludeFiles: true})
	if err != nil {
		t.Fatalf("ListFiles failed: %v", err)
	}

	foundRoot := false
	foundDeep := false
	for _, f := range files {
		if strings.HasSuffix(f, "root.txt") {
			foundRoot = true
		}
		if strings.HasSuffix(f, "deep.txt") {
			foundDeep = true
		}
	}

	if !foundRoot || !foundDeep {
		t.Errorf("ListFiles missing files. Found: %v", files)
	}

	// TraverseDirSlice
	allFiles, err := TraverseDirSlice(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(allFiles) != 2 {
		t.Errorf("Expected 2 files, got %d", len(allFiles))
	}
}

func TestFile_CountLines(t *testing.T) {
	dir, err := os.MkdirTemp("", "lines-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	filePath := filepath.Join(dir, "lines.txt")
	content := "line1\nline2\nline3"
	WriteString(filePath, content, WriteModeCreate)

	lines, err := CountLines(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if lines != 3 {
		t.Errorf("Expected 3 lines, got %d", lines)
	}
}

func TestFile_ExecutablePath(t *testing.T) {
	// Just ensure it doesn't panic and returns something reasonable
	path := GetExecutablePath()
	if path == "" {
		t.Error("GetExecutablePath returned empty string")
	}
}

// TestCountLines_LongLine 验证 CountLines 能处理超过 bufio.Scanner 默认 64KB
// 限制的超长行（原实现用 Scanner 会报 bufio.ErrTooLong）。
func TestCountLines_LongLine(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "longline.txt")

	// 构造一条 >64KB 的超长行 + 一条短行
	longLine := strings.Repeat("X", 100_000)
	content := longLine + "\nshort\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	lines, err := CountLines(filePath)
	if err != nil {
		t.Fatalf("CountLines failed on long line: %v", err)
	}
	if lines != 2 {
		t.Errorf("Expected 2 lines, got %d", lines)
	}
}

// TestListDirsOnly_ConsistentFormat 验证 ListDirsOnly 返回的全是全路径
// （原实现 depth=0 返回相对名，深层返回全路径，不一致）。
func TestListDirsOnly_ConsistentFormat(t *testing.T) {
	dir := t.TempDir()
	// 创建子目录和孙子目录
	subDir := filepath.Join(dir, "subdir")
	os.MkdirAll(subDir, 0755)
	grandDir := filepath.Join(subDir, "granddir")
	os.MkdirAll(grandDir, 0755)

	result, err := ListDirsOnly(dir, 3)
	if err != nil {
		t.Fatalf("ListDirsOnly failed: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("Expected at least 1 directory")
	}
	// 所有结果都应该是绝对路径（以 dir 开头）
	for _, d := range result {
		if !filepath.IsAbs(d) && !strings.HasPrefix(d, dir) {
			t.Errorf("Directory %q is not a full path under %q", d, dir)
		}
	}
}

func TestFile_Hash(t *testing.T) {
	dir, err := os.MkdirTemp("", "hash-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	filePath := filepath.Join(dir, "hash.txt")
	content := "test hash"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	sha1Hash, err := CalcSHA1(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(sha1Hash) != 40 { // SHA1 is 40 hex chars
		t.Errorf("Invalid SHA1 length: %d", len(sha1Hash))
	}

	sha256Hash, err := CalcSHA256(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(sha256Hash) != 64 { // SHA256 is 64 hex chars
		t.Errorf("Invalid SHA256 length: %d", len(sha256Hash))
	}
}

// ========== ReadLines ==========

// TestReadLines_Basic verifies line preservation including the last line
// without a trailing newline (cor/disk.ReadLine lost the last line on EOF).
func TestReadLines_Basic(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "lines.txt")

	// No trailing newline on the last line — original ReadLine would lose it
	content := "alpha\nbeta\ngamma"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	lines, err := ReadLines(filePath)
	if err != nil {
		t.Fatalf("ReadLines failed: %v", err)
	}
	want := []string{"alpha", "beta", "gamma"}
	if len(lines) != len(want) {
		t.Fatalf("Expected %d lines, got %d: %v", len(want), len(lines), lines)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("Line %d: expected %q, got %q", i, w, lines[i])
		}
	}
}

// TestReadLines_BlankLines verifies blank lines are preserved
// (a bare "\n" should yield an empty string entry, not be skipped).
func TestReadLines_BlankLines(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "blanks.txt")

	content := "a\n\nb\n\n\nc\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	lines, err := ReadLines(filePath)
	if err != nil {
		t.Fatalf("ReadLines failed: %v", err)
	}
	want := []string{"a", "", "b", "", "", "c"}
	if len(lines) != len(want) {
		t.Fatalf("Expected %d lines, got %d: %v", len(want), len(lines), lines)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("Line %d: expected %q, got %q", i, w, lines[i])
		}
	}
}

// TestReadLines_CRLF verifies \r\n line endings are stripped of both \r and \n.
func TestReadLines_CRLF(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "crlf.txt")

	content := "line1\r\nline2\r\nline3\r\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	lines, err := ReadLines(filePath)
	if err != nil {
		t.Fatalf("ReadLines failed: %v", err)
	}
	want := []string{"line1", "line2", "line3"}
	if len(lines) != len(want) {
		t.Fatalf("Expected %d lines, got %d: %v", len(want), len(lines), lines)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("Line %d: expected %q, got %q", i, w, lines[i])
		}
	}
}

// TestReadLines_LongLine verifies ReadLines handles >64KB lines
// (bufio.Scanner's default limit would fail with bufio.ErrTooLong).
func TestReadLines_LongLine(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "longline.txt")

	longLine := strings.Repeat("X", 100_000)
	content := longLine + "\nshort\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	lines, err := ReadLines(filePath)
	if err != nil {
		t.Fatalf("ReadLines failed: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("Expected 2 lines, got %d", len(lines))
	}
	if len(lines[0]) != 100_000 {
		t.Errorf("Expected line 0 of length %d, got %d", 100_000, len(lines[0]))
	}
	if lines[1] != "short" {
		t.Errorf("Expected line 1 = %q, got %q", "short", lines[1])
	}
}

// TestReadLines_EmptyFile verifies an empty file returns an empty slice, not [""]
func TestReadLines_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "empty.txt")

	if err := os.WriteFile(filePath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	lines, err := ReadLines(filePath)
	if err != nil {
		t.Fatalf("ReadLines failed: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("Expected 0 lines for empty file, got %d: %v", len(lines), lines)
	}
}

// TestReadLines_MissingFile verifies error is surfaced (cor/disk swallowed it).
func TestReadLines_MissingFile(t *testing.T) {
	_, err := ReadLines(filepath.Join(t.TempDir(), "nope.txt"))
	if err == nil {
		t.Error("Expected error for missing file, got nil")
	}
}

// ========== WriteFromReader ==========

// TestWriteFromReader_Basic verifies content is written and bytes returned.
func TestWriteFromReader_Basic(t *testing.T) {
	dir := t.TempDir()
	// Nested path to verify parent directory creation
	filePath := filepath.Join(dir, "sub", "deep", "out.bin")
	content := []byte("hello stream")

	n, err := WriteFromReader(filePath, strings.NewReader(string(content)))
	if err != nil {
		t.Fatalf("WriteFromReader failed: %v", err)
	}
	if n != int64(len(content)) {
		t.Errorf("Expected %d bytes written, got %d", len(content), n)
	}

	got, err := ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Errorf("Expected %q, got %q", content, got)
	}
}

// TestWriteFromReader_TruncatesExisting verifies existing content is replaced,
// not appended (cor/disk.WriteIo used WriteFile which also truncates — verifying parity).
func TestWriteFromReader_TruncatesExisting(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "trunc.txt")

	// Pre-existing longer content
	if err := os.WriteFile(filePath, []byte("OLD CONTENT THAT IS LONGER"), 0644); err != nil {
		t.Fatal(err)
	}

	// Write shorter content — must replace, not append
	if _, err := WriteFromReader(filePath, strings.NewReader("new")); err != nil {
		t.Fatal(err)
	}

	got, err := ReadString(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if got != "new" {
		t.Errorf("Expected truncated to %q, got %q", "new", got)
	}
}

// ========== CompareFiles ==========

func TestCompareFiles_Identical(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")

	content := []byte("identical content")
	if err := os.WriteFile(a, content, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, content, 0644); err != nil {
		t.Fatal(err)
	}

	eq, err := CompareFiles(a, b, false)
	if err != nil {
		t.Fatalf("CompareFiles failed: %v", err)
	}
	if !eq {
		t.Error("Expected identical files to compare equal")
	}
}

func TestCompareFiles_DifferentContent(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")

	if err := os.WriteFile(a, []byte("content A"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("content B"), 0644); err != nil {
		t.Fatal(err)
	}

	eq, err := CompareFiles(a, b, false)
	if err != nil {
		t.Fatalf("CompareFiles failed: %v", err)
	}
	if eq {
		t.Error("Expected different content to compare unequal")
	}
}

func TestCompareFiles_DifferentSize(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")

	if err := os.WriteFile(a, []byte("short"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("longer content"), 0644); err != nil {
		t.Fatal(err)
	}

	eq, err := CompareFiles(a, b, false)
	if err != nil {
		t.Fatalf("CompareFiles failed: %v", err)
	}
	if eq {
		t.Error("Expected different-size files to compare unequal")
	}
}

// TestCompareFiles_ModTimeMismatch verifies the checkModTime flag.
// Same content, same size, but different mtime → unequal when flag set,
// equal when flag clear.
func TestCompareFiles_ModTimeMismatch(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")

	content := []byte("same content")
	if err := os.WriteFile(a, content, 0644); err != nil {
		t.Fatal(err)
	}
	// Stagger writes so mtimes differ
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(b, content, 0644); err != nil {
		t.Fatal(err)
	}

	eq, err := CompareFiles(a, b, true)
	if err != nil {
		t.Fatalf("CompareFiles(checkModTime=true) failed: %v", err)
	}
	if eq {
		t.Error("Expected unequal modtimes to compare unequal with checkModTime=true")
	}

	eq, err = CompareFiles(a, b, false)
	if err != nil {
		t.Fatalf("CompareFiles(checkModTime=false) failed: %v", err)
	}
	if !eq {
		t.Error("Expected equal content to compare equal with checkModTime=false")
	}
}

// TestCompareFiles_LargeFile verifies streaming comparison works on multi-chunk
// files (>4KB so multiple iterations of the read loop).
func TestCompareFiles_LargeFile(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.bin")
	b := filepath.Join(dir, "b.bin")

	// 100KB of identical content — exercises multiple 4KB iterations
	content := bytes.Repeat([]byte{0xAB}, 100*1024)
	if err := os.WriteFile(a, content, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, content, 0644); err != nil {
		t.Fatal(err)
	}

	eq, err := CompareFiles(a, b, false)
	if err != nil {
		t.Fatalf("CompareFiles failed: %v", err)
	}
	if !eq {
		t.Error("Expected large identical files to compare equal")
	}

	// Flip one byte at the end to verify last-chunk comparison works
	content[len(content)-1] = 0xCD
	if err := os.WriteFile(b, content, 0644); err != nil {
		t.Fatal(err)
	}
	eq, err = CompareFiles(a, b, false)
	if err != nil {
		t.Fatalf("CompareFiles (mutated) failed: %v", err)
	}
	if eq {
		t.Error("Expected mutated last byte to compare unequal")
	}
}

func TestCompareFiles_MissingSource(t *testing.T) {
	dir := t.TempDir()
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(b, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	eq, err := CompareFiles(filepath.Join(dir, "missing.txt"), b, false)
	if err == nil {
		t.Error("Expected error for missing source, got nil")
	}
	if eq {
		t.Error("Expected false for missing source, got true")
	}
}

func TestCompareFiles_MissingDestination(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(a, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	eq, err := CompareFiles(a, filepath.Join(dir, "missing.txt"), false)
	if err == nil {
		t.Error("Expected error for missing destination, got nil")
	}
	if eq {
		t.Error("Expected false for missing destination, got true")
	}
}

// ========== CompareText ==========

func TestCompareText_Identical(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")

	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(a, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	eq, err := CompareText(a, b, false)
	if err != nil {
		t.Fatalf("CompareText failed: %v", err)
	}
	if !eq {
		t.Error("Expected identical text to compare equal")
	}
}

func TestCompareText_Different(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")

	if err := os.WriteFile(a, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("world\n"), 0644); err != nil {
		t.Fatal(err)
	}

	eq, err := CompareText(a, b, false)
	if err != nil {
		t.Fatalf("CompareText failed: %v", err)
	}
	if eq {
		t.Error("Expected different text to compare unequal")
	}
}

// TestCompareText_LineEndingNormalization verifies ignoreLineEndings=true.
// File A: CRLF endings + trailing newline.
// File B: LF endings, no trailing newline.
// With ignoreLineEndings → equal; without → unequal.
func TestCompareText_LineEndingNormalization(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")

	if err := os.WriteFile(a, []byte("line1\r\nline2\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("line1\nline2"), 0644); err != nil {
		t.Fatal(err)
	}

	// Without normalization: lengths differ → unequal
	eq, err := CompareText(a, b, false)
	if err != nil {
		t.Fatalf("CompareText(ignoreLineEndings=false) failed: %v", err)
	}
	if eq {
		t.Error("Expected CRLF vs LF+no-trailing to compare unequal without normalization")
	}

	// With normalization: equal
	eq, err = CompareText(a, b, true)
	if err != nil {
		t.Fatalf("CompareText(ignoreLineEndings=true) failed: %v", err)
	}
	if !eq {
		t.Error("Expected CRLF vs LF+no-trailing to compare equal with normalization")
	}
}

func TestCompareText_MissingFile(t *testing.T) {
	dir := t.TempDir()
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(b, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	eq, err := CompareText(filepath.Join(dir, "missing.txt"), b, false)
	if err == nil {
		t.Error("Expected error for missing file, got nil")
	}
	if eq {
		t.Error("Expected false for missing file, got true")
	}
}

func TestWriteFileExec_BasicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")

	data := []byte("#!/bin/sh\necho hello\n")
	if err := WriteFileExec(path, data); err != nil {
		t.Fatalf("WriteFileExec failed: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("content mismatch: got %q, want %q", got, data)
	}
}

func TestWriteFileExec_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "script.sh")

	if err := WriteFileExec(path, []byte("x")); err != nil {
		t.Fatalf("WriteFileExec failed: %v", err)
	}
	if !Exists(path) {
		t.Error("expected file to exist after WriteFileExec")
	}
}

func TestWriteFileExec_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.sh")

	if err := WriteFileExec(path, []byte("old")); err != nil {
		t.Fatalf("first WriteFileExec failed: %v", err)
	}
	if err := WriteFileExec(path, []byte("new content")); err != nil {
		t.Fatalf("second WriteFileExec failed: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != "new content" {
		t.Errorf("expected overwrite, got %q", got)
	}
}

// TestWriteFileExec_ExecutablePermission verifies the file mode is 0755
// on platforms where the executable bit is meaningful.
func TestWriteFileExec_ExecutablePermission(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable bit is not meaningful on windows")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	if err := WriteFileExec(path, []byte("#!/bin/sh\n")); err != nil {
		t.Fatalf("WriteFileExec failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	mode := info.Mode().Perm()
	if mode != 0755 {
		t.Errorf("file mode = %o, want 0755", mode)
	}
	// Sanity: owner/group/other all have the executable bit set.
	if mode&0111 != 0111 {
		t.Errorf("expected executable bits set, got mode %o", mode)
	}
}
