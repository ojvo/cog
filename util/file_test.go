package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestFile_Hash(t *testing.T) {
	dir, err := os.MkdirTemp("", "hash-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	filePath := filepath.Join(dir, "hash.txt")
	content := "test hash"
	WriteString(filePath, content, WriteModeCreate)

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
