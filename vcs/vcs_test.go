package vcs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"ojv/cog/store"
)

// snapFileTable expands a snapshot's tree into the flat path -> hash view used
// by tests that predate content-addressed trees. It fails the test on error so
// a corrupt tree can never be mistaken for an empty one.
func snapFileTable(t *testing.T, mgr *Manager, snap *Snapshot) map[string]string {
	t.Helper()
	files, err := mgr.snapshotFiles(snap)
	if err != nil {
		t.Fatalf("failed to expand snapshot %s: %v", snap.ID, err)
	}
	out := make(map[string]string, len(files))
	for p, f := range files {
		out[p] = f.hash
	}
	return out
}

// buildRootTree stores the given path -> hash table as tree objects and returns
// the root tree hash, for tests that need to hand-craft a snapshot.
func buildRootTree(t *testing.T, mgr *Manager, files map[string]string) string {
	t.Helper()
	b := newTreeBuilder()
	for p, h := range files {
		if err := b.add(p, h, false); err != nil {
			t.Fatalf("failed to add %s to tree: %v", p, err)
		}
	}
	root, err := b.build(mgr.store)
	if err != nil {
		t.Fatalf("failed to build tree: %v", err)
	}
	return root
}

func TestIsValidPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-path-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	outsidePath := filepath.Join(os.TempDir(), "outside-test.txt")

	tests := []struct {
		name    string
		path    string
		wantErr error
	}{
		{
			name:    "valid relative path",
			path:    "test.txt",
			wantErr: nil,
		},
		{
			name:    "valid relative path with dir",
			path:    "subdir/test.txt",
			wantErr: nil,
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: ErrInvalidPath,
		},
		{
			name:    "path traversal",
			path:    "../test.txt",
			wantErr: ErrPathTraversal,
		},
		{
			name:    "path traversal in middle",
			path:    "subdir/../test.txt",
			wantErr: nil,
		},
		{
			name:    "absolute path outside workdir",
			path:    outsidePath,
			wantErr: ErrPathTraversal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mgr.isValidPath(tt.path)
			if err != tt.wantErr {
				t.Errorf("isValidPath(%q) = %v, want %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestNewManager(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}

	if mgr.store == nil {
		t.Error("manager object store is nil")
	}
}

func TestManager_RecordOldState(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := []byte("original content")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}

	if !mgr.HasPendingChanges() {
		t.Error("HasPendingChanges should return true after RecordOldState")
	}

	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("second RecordOldState should not fail: %v", err)
	}
}

func TestManager_RecordOldState_NewFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	newFile := filepath.Join(tmpDir, "new.txt")

	if err := mgr.RecordOldState(newFile); err != nil {
		t.Fatalf("RecordOldState for non-existent file failed: %v", err)
	}

	if !mgr.HasPendingChanges() {
		t.Error("HasPendingChanges should return true")
	}
}

func TestManager_RecordOldState_InvalidPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	if err := mgr.RecordOldState("../traversal.txt"); err == nil {
		t.Error("RecordOldState with path traversal should fail")
	}

	if err := mgr.RecordOldState(""); err == nil {
		t.Error("RecordOldState with empty path should fail")
	}
}

func TestManager_RollbackPending(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	originalContent := []byte("original content")
	if err := os.WriteFile(testFile, originalContent, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}

	modifiedContent := []byte("modified content")
	if err := os.WriteFile(testFile, modifiedContent, 0644); err != nil {
		t.Fatalf("failed to modify test file: %v", err)
	}

	if err := mgr.RollbackPending(); err != nil {
		t.Fatalf("RollbackPending failed: %v", err)
	}

	restoredContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read restored file: %v", err)
	}

	if string(restoredContent) != string(originalContent) {
		t.Errorf("restored content = %q, want %q", string(restoredContent), string(originalContent))
	}

	if mgr.HasPendingChanges() {
		t.Error("HasPendingChanges should return false after rollback")
	}
}

func TestManager_RollbackPending_NewFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	newFile := filepath.Join(tmpDir, "new.txt")

	if err := mgr.RecordOldState(newFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}

	if err := os.WriteFile(newFile, []byte("new content"), 0644); err != nil {
		t.Fatalf("failed to create new file: %v", err)
	}

	if err := mgr.RollbackPending(); err != nil {
		t.Fatalf("RollbackPending failed: %v", err)
	}

	if _, err := os.Stat(newFile); !os.IsNotExist(err) {
		t.Error("new file should be deleted after rollback")
	}
}

func TestManager_CommitPending(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := []byte("commit content")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}

	snap, err := mgr.CommitPending("test commit", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}

	if snap == nil {
		t.Fatal("CommitPending returned nil snapshot")
	}

	if snap.Message != "test commit" {
		t.Errorf("snapshot message = %q, want %q", snap.Message, "test commit")
	}

	if mgr.HasPendingChanges() {
		t.Error("HasPendingChanges should return false after commit")
	}

	headID, err := mgr.GetHEAD()
	if err != nil {
		t.Fatalf("GetHEAD failed: %v", err)
	}

	if headID != snap.ID {
		t.Errorf("HEAD = %s, want %s", headID, snap.ID)
	}
}

func TestManager_CommitPending_NoChanges(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	snap, err := mgr.CommitPending("empty commit", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}

	if snap != nil {
		t.Error("CommitPending with no changes should return nil snapshot")
	}
}

func TestManager_CommitPending_WithExtraFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	store := mgr.store
	extraContent := []byte("extra file content")
	extraHash, err := store.PutData(extraContent)
	if err != nil {
		t.Fatalf("PutData failed: %v", err)
	}

	extraFiles := map[string]string{"/extra/file.txt": extraHash}

	snap, err := mgr.CommitPending("commit with extra", nil, extraFiles)
	if err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}

	if snap == nil {
		t.Fatal("CommitPending returned nil snapshot")
	}

	if _, ok := snapFileTable(t, mgr, snap)["/extra/file.txt"]; !ok {
		t.Error("extra file not found in snapshot")
	}
}

// TestManager_CommitPending_NewFile 验证 RecordOldState 后新建的文件能正确提交到快照
// 修复前：oldHash=="" 触发 delete(newFiles, relPath)，新文件不会被加入快照
func TestManager_CommitPending_NewFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-commit-new-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	// 先建一个文件，commit 一次，建立 HEAD 基线
	baseFile := filepath.Join(tmpDir, "base.txt")
	if err := os.WriteFile(baseFile, []byte("base"), 0644); err != nil {
		t.Fatalf("failed to write base file: %v", err)
	}
	if err := mgr.RecordOldState(baseFile); err != nil {
		t.Fatalf("RecordOldState base failed: %v", err)
	}
	if _, err := mgr.CommitPending("base commit", nil, nil); err != nil {
		t.Fatalf("CommitPending base failed: %v", err)
	}

	// 场景：对一个尚不存在的文件 RecordOldState（oldHash=""），随后创建该文件
	newFile := filepath.Join(tmpDir, "new.txt")
	if err := mgr.RecordOldState(newFile); err != nil {
		t.Fatalf("RecordOldState for non-existent file failed: %v", err)
	}
	if err := os.WriteFile(newFile, []byte("new content"), 0644); err != nil {
		t.Fatalf("failed to create new file: %v", err)
	}

	snap, err := mgr.CommitPending("add new file", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}
	if snap == nil {
		t.Fatal("CommitPending returned nil snapshot")
	}

	// 修复前：new.txt 不在 snap.Files 中（被错误地 delete）
	files := snapFileTable(t, mgr, snap)
	hash, ok := files["new.txt"]
	if !ok {
		t.Fatal("new file should be included in snapshot after being created")
	}
	if hash == "" {
		t.Error("new file hash should not be empty")
	}

	// 验证 HEAD 基线文件仍在快照中
	if _, ok := files["base.txt"]; !ok {
		t.Error("base file should still be in snapshot")
	}
}

// TestManager_CommitPending_DeletedFile 验证 RecordOldState 后删除的文件能正确从快照移除
// 修复前：oldHash!="" 触发 store.Put，因文件已删除导致 os.ReadFile 失败，commit 报错
func TestManager_CommitPending_DeletedFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-commit-del-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	// 先建一个文件，commit 一次，建立 HEAD 基线
	baseFile := filepath.Join(tmpDir, "base.txt")
	if err := os.WriteFile(baseFile, []byte("base"), 0644); err != nil {
		t.Fatalf("failed to write base file: %v", err)
	}
	if err := mgr.RecordOldState(baseFile); err != nil {
		t.Fatalf("RecordOldState base failed: %v", err)
	}
	if _, err := mgr.CommitPending("base commit", nil, nil); err != nil {
		t.Fatalf("CommitPending base failed: %v", err)
	}

	// 场景：对一个已存在文件 RecordOldState（oldHash=hash），随后删除该文件
	targetFile := filepath.Join(tmpDir, "target.txt")
	if err := os.WriteFile(targetFile, []byte("target content"), 0644); err != nil {
		t.Fatalf("failed to write target file: %v", err)
	}
	// 注意：target.txt 还没进 HEAD，需要先 record 再 commit 进基线，才能验证“从快照移除”
	if err := mgr.RecordOldState(targetFile); err != nil {
		t.Fatalf("RecordOldState target failed: %v", err)
	}
	if _, err := mgr.CommitPending("add target", nil, nil); err != nil {
		t.Fatalf("CommitPending add target failed: %v", err)
	}

	// 现在 target.txt 在 HEAD 快照中。再次 RecordOldState，然后删除它
	if err := mgr.RecordOldState(targetFile); err != nil {
		t.Fatalf("RecordOldState target again failed: %v", err)
	}
	if err := os.Remove(targetFile); err != nil {
		t.Fatalf("failed to remove target file: %v", err)
	}

	snap, err := mgr.CommitPending("delete target", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}
	if snap == nil {
		t.Fatal("CommitPending returned nil snapshot")
	}

	// 修复前：store.Put 因文件不存在而失败，commit 报错
	// 修复后：target.txt 应从快照中移除
	files := snapFileTable(t, mgr, snap)
	if _, ok := files["target.txt"]; ok {
		t.Error("deleted file should be removed from snapshot")
	}

	// 基线文件应保留
	if _, ok := files["base.txt"]; !ok {
		t.Error("base file should still be in snapshot")
	}
}

// TestManager_CommitPending_HEADUnreadable 验证 HEAD 读取失败时 commit 返回错误
// 修复前：headID, _ := v.readHEAD() 忽略错误，基线丢失
func TestManager_CommitPending_HEADUnreadable(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-commit-head-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	// 建立基线
	baseFile := filepath.Join(tmpDir, "base.txt")
	if err := os.WriteFile(baseFile, []byte("base"), 0644); err != nil {
		t.Fatalf("failed to write base file: %v", err)
	}
	if err := mgr.RecordOldState(baseFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}
	if _, err := mgr.CommitPending("base commit", nil, nil); err != nil {
		t.Fatalf("CommitPending base failed: %v", err)
	}

	// 让 headFile 指向一个已存在的目录，os.ReadFile 会返回 "is a directory" 错误
	mgr.headFile = tmpDir

	// 准备 pending change
	newFile := filepath.Join(tmpDir, "new.txt")
	if err := os.WriteFile(newFile, []byte("new"), 0644); err != nil {
		t.Fatalf("failed to write new file: %v", err)
	}
	if err := mgr.RecordOldState(newFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}

	// 修复前：忽略错误，commit 成功但丢失 base.txt
	// 修复后：返回错误
	_, err = mgr.CommitPending("should fail", nil, nil)
	if err == nil {
		t.Fatal("CommitPending with unreadable HEAD should return error")
	}
}

// TestManager_CommitPending_CorruptedBaseSnapshot 验证基线快照损坏时 commit 返回错误
// 修复前：if err == nil 静默跳过基线加载，基线文件丢失
func TestManager_CommitPending_CorruptedBaseSnapshot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-commit-corrupt-base-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	// 建立基线
	baseFile := filepath.Join(tmpDir, "base.txt")
	if err := os.WriteFile(baseFile, []byte("base"), 0644); err != nil {
		t.Fatalf("failed to write base file: %v", err)
	}
	if err := mgr.RecordOldState(baseFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}
	baseSnap, err := mgr.CommitPending("base commit", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending base failed: %v", err)
	}

	// 损坏基线快照文件
	snapPath := filepath.Join(tmpDir, "vcs", "snapshots", baseSnap.ID+".json")
	if err := os.WriteFile(snapPath, []byte("corrupted json content"), 0644); err != nil {
		t.Fatalf("failed to corrupt snapshot: %v", err)
	}

	// 准备 pending change
	newFile := filepath.Join(tmpDir, "new.txt")
	if err := os.WriteFile(newFile, []byte("new"), 0644); err != nil {
		t.Fatalf("failed to write new file: %v", err)
	}
	if err := mgr.RecordOldState(newFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}

	// 修复前：静默跳过基线，commit 成功但 base.txt 丢失
	// 修复后：返回错误
	_, err = mgr.CommitPending("should fail", nil, nil)
	if err == nil {
		t.Fatal("CommitPending with corrupted base snapshot should return error")
	}
}

func TestManager_GetSnapshot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}

	snap, err := mgr.CommitPending("test", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}

	retrievedSnap, err := mgr.GetSnapshot(snap.ID)
	if err != nil {
		t.Fatalf("GetSnapshot failed: %v", err)
	}

	if retrievedSnap.ID != snap.ID {
		t.Errorf("retrieved snapshot ID = %s, want %s", retrievedSnap.ID, snap.ID)
	}
}

func TestManager_GetSnapshot_InvalidID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	_, err = mgr.GetSnapshot("invalid-id")
	if err == nil {
		t.Error("GetSnapshot with invalid ID should fail")
	}
}

func TestManager_GetSnapshot_NonExistent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	validID := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	_, err = mgr.GetSnapshot(validID)
	if err == nil {
		t.Error("GetSnapshot with non-existent ID should fail")
	}
}

func TestManager_GetSnapshot_PayloadIDMismatch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	fileID := strings.Repeat("a", 64)
	payloadID := strings.Repeat("b", 64)
	snap := Snapshot{ID: payloadID, Message: "bad"}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	snapDir := filepath.Join(tmpDir, "vcs", "snapshots")
	if err := os.MkdirAll(snapDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(snapDir, fileID+".json"), data, 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	_, err = mgr.GetSnapshot(fileID)
	if err == nil {
		t.Fatal("GetSnapshot should reject payload ID mismatch")
	}
}

func TestManager_GetSnapshot_InvalidParentID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	fileID := strings.Repeat("a", 64)
	snap := Snapshot{ID: fileID, ParentID: "bad-parent", Message: "bad"}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	snapDir := filepath.Join(tmpDir, "vcs", "snapshots")
	if err := os.MkdirAll(snapDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(snapDir, fileID+".json"), data, 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	_, err = mgr.GetSnapshot(fileID)
	if err == nil {
		t.Fatal("GetSnapshot should reject invalid parent ID")
	}
}

func TestManager_GetSnapshot_InvalidTreeHash(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	fileID := strings.Repeat("a", 64)
	snap := Snapshot{ID: fileID, Message: "bad", Tree: "not-a-hash"}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	snapDir := filepath.Join(tmpDir, "vcs", "snapshots")
	if err := os.MkdirAll(snapDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(snapDir, fileID+".json"), data, 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	_, err = mgr.GetSnapshot(fileID)
	if err == nil {
		t.Fatal("GetSnapshot should reject invalid tree hash")
	}
}

func TestManager_GetSnapshot_ContentHashMismatch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	validHash := strings.Repeat("a", 64)
	snap := Snapshot{
		ID:        validHash,
		Timestamp: time.Unix(1700000000, 0),
		Message:   "original",
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	snapDir := filepath.Join(tmpDir, "vcs", "snapshots")
	if err := os.MkdirAll(snapDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	filePath := filepath.Join(snapDir, validHash+".json")
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 篡改内容但保持文件名不变：修复前只要字段格式合法就会被接受。
	snap.Message = "tampered"
	data, err = json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal tampered failed: %v", err)
	}
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		t.Fatalf("rewrite failed: %v", err)
	}

	_, err = mgr.GetSnapshot(validHash)
	if err == nil {
		t.Fatal("GetSnapshot should reject tampered snapshot payload whose content hash no longer matches file ID")
	}
}

func TestManager_ListHistory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")

	for i := 0; i < 3; i++ {
		if err := os.WriteFile(testFile, []byte(fmt.Sprintf("content-%d", i)), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}
		if err := mgr.RecordOldState(testFile); err != nil {
			t.Fatalf("RecordOldState failed: %v", err)
		}
		if _, err := mgr.CommitPending(fmt.Sprintf("commit %d", i), nil, nil); err != nil {
			t.Fatalf("CommitPending failed: %v", err)
		}
	}

	history, err := mgr.ListHistory(10)
	if err != nil {
		t.Fatalf("ListHistory failed: %v", err)
	}

	if len(history) != 3 {
		t.Errorf("history length = %d, want 3", len(history))
	}
}

func TestManager_ListHistory_Limit(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")

	for i := 0; i < 5; i++ {
		if err := os.WriteFile(testFile, []byte(fmt.Sprintf("content-%d", i)), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}
		if err := mgr.RecordOldState(testFile); err != nil {
			t.Fatalf("RecordOldState failed: %v", err)
		}
		if _, err := mgr.CommitPending(fmt.Sprintf("commit %d", i), nil, nil); err != nil {
			t.Fatalf("CommitPending failed: %v", err)
		}
	}

	history, err := mgr.ListHistory(2)
	if err != nil {
		t.Fatalf("ListHistory failed: %v", err)
	}

	if len(history) != 2 {
		t.Errorf("history length = %d, want 2", len(history))
	}
}

func TestManager_Checkout(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	originalContent := []byte("original")
	if err := os.WriteFile(testFile, originalContent, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}

	snap, err := mgr.CommitPending("first commit", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}

	if err := os.WriteFile(testFile, []byte("modified"), 0644); err != nil {
		t.Fatalf("failed to modify test file: %v", err)
	}

	if err := mgr.Checkout(snap.ID); err != nil {
		t.Fatalf("Checkout failed: %v", err)
	}

	restoredContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read restored file: %v", err)
	}

	if string(restoredContent) != string(originalContent) {
		t.Errorf("restored content = %q, want %q", string(restoredContent), string(originalContent))
	}
}

func TestManager_DiffSnapshots(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	testFile1 := filepath.Join(tmpDir, "file1.txt")
	if err := os.WriteFile(testFile1, []byte("content1"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	if err := mgr.RecordOldState(testFile1); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}
	snap1, err := mgr.CommitPending("first commit", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}

	testFile2 := filepath.Join(tmpDir, "file2.txt")
	if err := os.WriteFile(testFile2, []byte("content2"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	if err := mgr.RecordOldState(testFile2); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}
	snap2, err := mgr.CommitPending("second commit", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}

	diffs, err := mgr.DiffSnapshots(snap1.ID, snap2.ID)
	if err != nil {
		t.Fatalf("DiffSnapshots failed: %v", err)
	}

	if len(diffs) == 0 {
		t.Error("DiffSnapshots should return differences")
	}

	for path, diff := range diffs {
		if diff.Status != DiffAdded && diff.Status != DiffModified && diff.Status != DiffDeleted {
			t.Errorf("unexpected diff status for %s: %s", path, diff.Status)
		}
	}
}

func TestManager_GC(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")

	for i := 0; i < 5; i++ {
		if err := os.WriteFile(testFile, []byte(fmt.Sprintf("content-%d", i)), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}
		if err := mgr.RecordOldState(testFile); err != nil {
			t.Fatalf("RecordOldState failed: %v", err)
		}
		if _, err := mgr.CommitPending(fmt.Sprintf("commit %d", i), nil, nil); err != nil {
			t.Fatalf("CommitPending failed: %v", err)
		}
	}

	deleted, err := mgr.GC(2)
	if err != nil {
		t.Fatalf("GC failed: %v", err)
	}

	if deleted < 3 {
		t.Errorf("GC deleted %d items, want at least 3", deleted)
	}

	history, err := mgr.ListHistory(10)
	if err != nil {
		t.Fatalf("ListHistory failed: %v", err)
	}

	if len(history) > 2 {
		t.Errorf("history length after GC = %d, want at most 2", len(history))
	}
}

func TestManager_GC_KeepAtLeastOne(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}
	if _, err := mgr.CommitPending("commit", nil, nil); err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}

	_, err = mgr.GC(0)
	if err != nil {
		t.Fatalf("GC failed: %v", err)
	}

	history, err := mgr.ListHistory(10)
	if err != nil {
		t.Fatalf("ListHistory failed: %v", err)
	}

	if len(history) != 1 {
		t.Errorf("history length after GC with keepLast=0 = %d, want 1", len(history))
	}
}

// TestManager_GC_CorruptedHEAD 验证 HEAD 损坏时 GC 返回错误而不是删除全部快照
// 修复前：headID 不匹配 allSnaps，keepSet 为空，所有快照被删除
func TestManager_GC_CorruptedHEAD(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-gc-corrupt-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	// 建立两个快照
	testFile := filepath.Join(tmpDir, "test.txt")
	for i := 0; i < 2; i++ {
		if err := os.WriteFile(testFile, []byte(fmt.Sprintf("content-%d", i)), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}
		if err := mgr.RecordOldState(testFile); err != nil {
			t.Fatalf("RecordOldState failed: %v", err)
		}
		if _, err := mgr.CommitPending(fmt.Sprintf("commit %d", i), nil, nil); err != nil {
			t.Fatalf("CommitPending failed: %v", err)
		}
	}

	// 损坏 HEAD：写入无效内容
	headPath := filepath.Join(tmpDir, "vcs", "HEAD")
	if err := os.WriteFile(headPath, []byte("corrupted-head-content"), 0644); err != nil {
		t.Fatalf("failed to corrupt HEAD: %v", err)
	}

	// GC 应返回错误，而不是静默删除所有快照
	_, err = mgr.GC(5)
	if err == nil {
		t.Fatal("GC with corrupted HEAD should return error")
	}

	// 验证快照文件仍然存在
	snapDir := filepath.Join(tmpDir, "vcs", "snapshots")
	entries, err := os.ReadDir(snapDir)
	if err != nil {
		t.Fatalf("failed to read snapshots dir: %v", err)
	}
	jsonCount := 0
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			jsonCount++
		}
	}
	if jsonCount != 2 {
		t.Errorf("after GC with corrupted HEAD, %d snapshots remain, want 2 (all should be preserved)", jsonCount)
	}
}

// TestManager_GC_HEADPointsToMissingSnapshot 验证 HEAD 合法但指向的快照文件不存在时 GC 拒绝执行
// 修复前：keepSet 为空，所有快照被删除
// 场景：HEAD 通过 isValidHash 校验，但 HEAD 指向的快照文件被手动删除/磁盘损坏
func TestManager_GC_HEADPointsToMissingSnapshot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-gc-missing-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	// 建立两个快照，第二个成为 HEAD
	testFile := filepath.Join(tmpDir, "test.txt")
	for i := 0; i < 2; i++ {
		if err := os.WriteFile(testFile, []byte(fmt.Sprintf("content-%d", i)), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}
		if err := mgr.RecordOldState(testFile); err != nil {
			t.Fatalf("RecordOldState failed: %v", err)
		}
		if _, err := mgr.CommitPending(fmt.Sprintf("commit %d", i), nil, nil); err != nil {
			t.Fatalf("CommitPending failed: %v", err)
		}
	}

	// 读取 HEAD，然后删除 HEAD 指向的快照文件
	headID, err := mgr.GetHEAD()
	if err != nil {
		t.Fatalf("GetHEAD failed: %v", err)
	}
	headSnapPath := filepath.Join(tmpDir, "vcs", "snapshots", headID+".json")
	if err := os.Remove(headSnapPath); err != nil {
		t.Fatalf("failed to remove HEAD snapshot: %v", err)
	}

	// GC 应拒绝执行，返回错误
	_, err = mgr.GC(5)
	if err == nil {
		t.Fatal("GC with HEAD pointing to missing snapshot should return error")
	}

	// 验证剩余的快照（第一个快照）仍然存在
	snapDir := filepath.Join(tmpDir, "vcs", "snapshots")
	entries, err := os.ReadDir(snapDir)
	if err != nil {
		t.Fatalf("failed to read snapshots dir: %v", err)
	}
	jsonCount := 0
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			jsonCount++
		}
	}
	if jsonCount != 1 {
		t.Errorf("after GC with missing HEAD snapshot, %d snapshots remain, want 1 (the other one should be preserved)", jsonCount)
	}
}

// TestManager_GC_BrokenAncestorChain 验证 HEAD 祖先链中途断裂时 GC 拒绝执行。
// 修复前：遍历静默 break，keepSet 只保留前半段，后半段祖先会被误删。
func TestManager_GC_BrokenAncestorChain(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-gc-broken-chain-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	testFile := filepath.Join(tmpDir, "test.txt")
	var snaps []*Snapshot
	for i := 0; i < 3; i++ {
		if err := os.WriteFile(testFile, []byte(fmt.Sprintf("content-%d", i)), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}
		if err := mgr.RecordOldState(testFile); err != nil {
			t.Fatalf("RecordOldState failed: %v", err)
		}
		snap, err := mgr.CommitPending(fmt.Sprintf("commit %d", i), nil, nil)
		if err != nil {
			t.Fatalf("CommitPending failed: %v", err)
		}
		snaps = append(snaps, snap)
	}

	// 删除中间祖先快照：HEAD(第3个) 仍存在，但它的 ParentID 指向缺失的第2个快照。
	middleSnapPath := filepath.Join(tmpDir, "vcs", "snapshots", snaps[1].ID+".json")
	if err := os.Remove(middleSnapPath); err != nil {
		t.Fatalf("failed to remove middle snapshot: %v", err)
	}

	_, err = mgr.GC(10)
	if err == nil {
		t.Fatal("GC should fail when HEAD ancestor chain is broken")
	}

	// 应保留现有两个快照文件（HEAD 和最老祖先），而不是误删最老祖先。
	snapDir := filepath.Join(tmpDir, "vcs", "snapshots")
	entries, err := os.ReadDir(snapDir)
	if err != nil {
		t.Fatalf("failed to read snapshots dir: %v", err)
	}
	jsonCount := 0
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			jsonCount++
		}
	}
	if jsonCount != 2 {
		t.Fatalf("after GC with broken ancestor chain, %d snapshots remain, want 2", jsonCount)
	}
}

// TestManager_GC_SkipsNonVCSFiles 验证 GC 不删除 snapshots 目录下的非 vcs 文件
// 修复前：所有 .json 文件都被加入 allSnaps，非 hash 命名的文件被误删
func TestManager_GC_SkipsNonVCSFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-gc-nonvcs-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	// 建立一个真实快照
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}
	realSnap, err := mgr.CommitPending("real", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}

	// 在 snapshots 目录下放入非 vcs 文件
	snapDir := filepath.Join(tmpDir, "vcs", "snapshots")
	nonVCSFiles := []string{
		"README.json",
		"config.json",
		"notes.txt", // 非 .json 扩展名
	}
	for _, name := range nonVCSFiles {
		path := filepath.Join(snapDir, name)
		if err := os.WriteFile(path, []byte("not a vcs snapshot"), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	// 调用 GC，保留 1 个快照
	removed, err := mgr.GC(1)
	if err != nil {
		t.Fatalf("GC failed: %v", err)
	}

	// removed 应为 0（真实快照被保留，非 vcs 文件被跳过）
	if removed != 0 {
		t.Errorf("GC removed %d, want 0 (real snapshot kept, non-vcs files skipped)", removed)
	}

	// 真实快照应保留
	_, err = mgr.GetSnapshot(realSnap.ID)
	if err != nil {
		t.Error("real snapshot should still exist")
	}

	// 非 vcs 文件应保留
	for _, name := range nonVCSFiles {
		path := filepath.Join(snapDir, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("non-vcs file %s should NOT be deleted: %v", name, err)
		}
	}
}

// TestManager_ReadHEAD_TrimsWhitespace 验证 readHEAD 对 HEAD 内容做 trim
// 修复前：HEAD 含换行符时 isValidHash 拒绝，操作失败
func TestManager_ReadHEAD_TrimsWhitespace(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-head-trim-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	// 建立快照获取有效 HEAD
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}
	snap, err := mgr.CommitPending("base", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}

	// 在 HEAD 文件内容末尾添加换行符（模拟编辑器行为）
	headPath := filepath.Join(tmpDir, "vcs", "HEAD")
	if err := os.WriteFile(headPath, []byte(snap.ID+"\n"), 0644); err != nil {
		t.Fatalf("failed to write HEAD with newline: %v", err)
	}

	// 修复前：GetHEAD 返回 "snap.ID\n"，ListHistory/CommitPending 会失败
	// 修复后：GetHEAD 返回 "snap.ID"（已 trim）
	headID, err := mgr.GetHEAD()
	if err != nil {
		t.Fatalf("GetHEAD failed: %v", err)
	}
	if headID != snap.ID {
		t.Errorf("GetHEAD = %q, want %q (should be trimmed)", headID, snap.ID)
	}

	// ListHistory 应能正常工作
	history, err := mgr.ListHistory(10)
	if err != nil {
		t.Fatalf("ListHistory failed: %v", err)
	}
	if len(history) != 1 {
		t.Errorf("history length = %d, want 1", len(history))
	}

	// CommitPending 应能正常加载基线
	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}
	snap2, err := mgr.CommitPending("second", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}
	if snap2 == nil {
		t.Fatal("CommitPending returned nil snapshot")
	}
	// 基线文件应保留
	if _, ok := snapFileTable(t, mgr, snap2)["test.txt"]; !ok {
		t.Error("base file should still be in snapshot (HEAD trim failed to load base)")
	}
}

// ================

func TestIsValidHash(t *testing.T) {
	tests := []struct {
		name    string
		hash    string
		wantErr error
	}{
		{
			name:    "valid hash",
			hash:    "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
			wantErr: nil,
		},
		{
			name:    "empty hash",
			hash:    "",
			wantErr: ErrInvalidHash,
		},
		{
			name:    "short hash",
			hash:    "a1b2c3d4",
			wantErr: ErrInvalidHash,
		},
		{
			name:    "long hash",
			hash:    "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2extra",
			wantErr: ErrInvalidHash,
		},
		{
			name:    "invalid chars uppercase",
			hash:    "A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4E5F6A1B2",
			wantErr: ErrInvalidHashChars,
		},
		{
			name:    "invalid chars special",
			hash:    "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6g1h2",
			wantErr: ErrInvalidHashChars,
		},
		{
			name:    "path traversal dots",
			hash:    "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6..",
			wantErr: ErrInvalidHash,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := isValidHash(tt.hash)
			if err != tt.wantErr {
				t.Errorf("isValidHash(%q) = %v, want %v", tt.hash, err, tt.wantErr)
			}
		})
	}
}

func TestObjectStore_PutAndGet(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewObjectStore(tmpDir)

	testContent := []byte("hello world")
	hash, err := store.PutData(testContent)
	if err != nil {
		t.Fatalf("PutData failed: %v", err)
	}

	if len(hash) != hashLength {
		t.Errorf("hash length = %d, want %d", len(hash), hashLength)
	}

	content, err := store.GetData(hash)
	if err != nil {
		t.Fatalf("GetData failed: %v", err)
	}

	if string(content) != string(testContent) {
		t.Errorf("GetData = %q, want %q", string(content), string(testContent))
	}
}

func TestObjectStore_PutDuplicate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewObjectStore(tmpDir)

	testContent := []byte("duplicate content")
	hash1, err := store.PutData(testContent)
	if err != nil {
		t.Fatalf("first PutData failed: %v", err)
	}

	hash2, err := store.PutData(testContent)
	if err != nil {
		t.Fatalf("second PutData failed: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("hashes differ: %s vs %s", hash1, hash2)
	}
}

func TestObjectStore_GetInvalidHash(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewObjectStore(tmpDir)

	_, err = store.GetData("invalid")
	if err == nil {
		t.Error("GetData with invalid hash should fail")
	}

	err = store.Get("invalid", filepath.Join(tmpDir, "output"))
	if err == nil {
		t.Error("Get with invalid hash should fail")
	}
}

func TestObjectStore_GetPathTraversal(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewObjectStore(tmpDir)

	traversalHash := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4../ab"

	_, err = store.GetData(traversalHash)
	if err == nil {
		t.Error("GetData with path traversal should fail")
	}

	err = store.Get(traversalHash, filepath.Join(tmpDir, "output"))
	if err == nil {
		t.Error("Get with path traversal should fail")
	}
}

func TestObjectStore_GetNonExistent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewObjectStore(tmpDir)

	validHash := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

	_, err = store.GetData(validHash)
	if err == nil {
		t.Error("GetData with non-existent hash should fail")
	}
}

func TestObjectStore_PutFromFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewObjectStore(tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := []byte("file content")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	hash, _, err := store.Put(testFile)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	content, err := store.GetData(hash)
	if err != nil {
		t.Fatalf("GetData failed: %v", err)
	}

	if string(content) != string(testContent) {
		t.Errorf("GetData = %q, want %q", string(content), string(testContent))
	}
}

func TestObjectStore_GetToFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewObjectStore(tmpDir)

	testContent := []byte("output content")
	hash, err := store.PutData(testContent)
	if err != nil {
		t.Fatalf("PutData failed: %v", err)
	}

	outputFile := filepath.Join(tmpDir, "output.txt")
	if err := store.Get(hash, outputFile); err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	readContent, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	if string(readContent) != string(testContent) {
		t.Errorf("output file = %q, want %q", string(readContent), string(testContent))
	}
}

func TestObjectStore_GetData_RejectsTamperedContent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewObjectStore(tmpDir)
	hash, err := store.PutData([]byte("original content"))
	if err != nil {
		t.Fatalf("PutData failed: %v", err)
	}

	// Sanity: the untouched object reads back fine.
	if got, err := store.GetData(hash); err != nil || string(got) != "original content" {
		t.Fatalf("GetData before tamper = %q, %v", got, err)
	}

	// Overwrite the object file in place, leaving its name (the expected hash)
	// unchanged. A content-addressed store must not hand these bytes back.
	objPath := filepath.Join(tmpDir, "objects", hash[:2], hash[2:])
	if err := os.WriteFile(objPath, []byte("tampered content"), 0644); err != nil {
		t.Fatalf("failed to tamper object: %v", err)
	}

	if _, err := store.GetData(hash); !errors.Is(err, ErrCorruptObject) {
		t.Fatalf("GetData on tampered object err = %v, want ErrCorruptObject", err)
	}
}

func TestManager_Checkout_RejectsTamperedObject(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	file := filepath.Join(tmpDir, "a.txt")
	if err := os.WriteFile(file, []byte("version one"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RecordOldState(file); err != nil {
		t.Fatal(err)
	}
	snap, err := mgr.CommitPending("v1", nil, nil)
	if err != nil || snap == nil {
		t.Fatalf("CommitPending = %v, %v", snap, err)
	}
	targetHash := snapFileTable(t, mgr, snap)["a.txt"]

	// Move the work tree forward and tamper the object that v1 depends on.
	if err := os.WriteFile(file, []byte("version two"), 0644); err != nil {
		t.Fatal(err)
	}
	objPath := filepath.Join(mgr.baseDir, "objects", targetHash[:2], targetHash[2:])
	if err := os.WriteFile(objPath, []byte("corrupted"), 0644); err != nil {
		t.Fatalf("failed to tamper object: %v", err)
	}

	if err := mgr.Checkout(snap.ID); err == nil {
		t.Fatal("Checkout with tampered object must fail")
	}
	// The corrupted object must not have been written into the work tree.
	if got, _ := os.ReadFile(file); string(got) != "version two" {
		t.Fatalf("workspace was modified despite corruption: %q", got)
	}
}

func TestManager_GC_RefusesToPruneObjectsWhenSnapshotDeleteFails(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	file := filepath.Join(tmpDir, "f.txt")

	var snapIDs []string
	for i := 0; i < 3; i++ {
		if err := os.WriteFile(file, []byte(fmt.Sprintf("c-%d", i)), 0644); err != nil {
			t.Fatal(err)
		}
		if err := mgr.RecordOldState(file); err != nil {
			t.Fatal(err)
		}
		snap, err := mgr.CommitPending(fmt.Sprintf("c %d", i), nil, nil)
		if err != nil || snap == nil {
			t.Fatalf("CommitPending: %v, %v", snap, err)
		}
		snapIDs = append(snapIDs, snap.ID)
	}

	// Make the oldest snapshot (the one GC would delete with keepLast=1)
	// unremovable. On Windows, holding the file open with a share mode that
	// denies deletion makes os.Remove fail; on POSIX unlink of an open file
	// succeeds, so the scenario cannot be reproduced and the test skips.
	stale := filepath.Join(mgr.baseDir, "snapshots", snapIDs[0]+".json")
	lock, err := os.OpenFile(stale, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := os.Remove(stale); err == nil {
		t.Skip("filesystem allows unlinking an open file; cannot simulate delete failure")
	}

	// All objects referenced by the surviving chain must still be present
	// after a failed GC: it must not partially prune.
	deleted, err := mgr.GC(1)
	if err == nil {
		t.Fatalf("GC must fail when a stale snapshot cannot be deleted, deleted=%d", deleted)
	}
	if !strings.Contains(err.Error(), "refusing to prune objects") {
		t.Fatalf("GC error = %v, want refusal to prune", err)
	}

	// The surviving HEAD snapshot must still be fully restorable.
	head, err := mgr.GetHEAD()
	if err != nil || head == "" {
		t.Fatalf("GetHEAD = %q, %v", head, err)
	}
	if _, err := mgr.GetSnapshot(head); err != nil {
		t.Fatalf("HEAD snapshot unreadable after failed GC: %v", err)
	}
	if err := mgr.Checkout(head); err != nil {
		t.Fatalf("HEAD snapshot not restorable after failed GC: %v", err)
	}
	if got, _ := os.ReadFile(file); string(got) != "c-2" {
		t.Fatalf("work tree not restored to HEAD after failed GC: %q", got)
	}
}

func TestObjectStore_PruneObjects(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewObjectStore(tmpDir)

	keepContent := []byte("keep this")
	keepHash, err := store.PutData(keepContent)
	if err != nil {
		t.Fatalf("PutData failed: %v", err)
	}

	pruneContent := []byte("prune this")
	pruneHash, err := store.PutData(pruneContent)
	if err != nil {
		t.Fatalf("PutData failed: %v", err)
	}

	keepHashes := map[string]bool{keepHash: true}

	deleted, err := store.PruneObjects(keepHashes, 0)
	if err != nil {
		t.Fatalf("PruneObjects failed: %v", err)
	}

	if deleted < 1 {
		t.Errorf("PruneObjects deleted %d objects, want at least 1", deleted)
	}

	_, err = store.GetData(keepHash)
	if err != nil {
		t.Error("kept object should still exist")
	}

	_, err = store.GetData(pruneHash)
	if err == nil {
		t.Error("pruned object should not exist")
	}
}

// TestObjectStore_PruneObjects_SkipsInvalidHash 验证 PruneObjects 跳过非 vcs 文件
// 修复前：objects 目录下的非 hash 命名文件会被误删
func TestObjectStore_PruneObjects_SkipsInvalidHash(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-prune-invalid-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewObjectStore(tmpDir)

	// 创建一个有效的 vcs 对象
	keepContent := []byte("keep this")
	keepHash, err := store.PutData(keepContent)
	if err != nil {
		t.Fatalf("PutData failed: %v", err)
	}

	// 在 objects 目录下放入非 vcs 文件（不符合 hash 格式的文件名）
	// 场景 1: 文件名长度不是 62（hashLength - 2 = 62）
	objDir := filepath.Join(tmpDir, "objects", keepHash[:2])
	shortFile := filepath.Join(objDir, "short.txt")
	if err := os.WriteFile(shortFile, []byte("not a vcs object"), 0644); err != nil {
		t.Fatalf("failed to write short file: %v", err)
	}

	// 场景 2: 目录名不是有效 hex
	invalidDir := filepath.Join(tmpDir, "objects", "zz")
	os.MkdirAll(invalidDir, 0755)
	invalidFile := filepath.Join(invalidDir, strings.Repeat("a", 62))
	if err := os.WriteFile(invalidFile, []byte("invalid dir prefix"), 0644); err != nil {
		t.Fatalf("failed to write invalid file: %v", err)
	}

	// 设置文件 ModTime 为过去，确保满足 minAge 条件
	pastTime := time.Now().Add(-2 * time.Hour)
	os.Chtimes(shortFile, pastTime, pastTime)
	os.Chtimes(invalidFile, pastTime, pastTime)

	// 调用 PruneObjects，只保留 keepHash
	keepHashes := map[string]bool{keepHash: true}
	deleted, err := store.PruneObjects(keepHashes, 0)
	if err != nil {
		t.Fatalf("PruneObjects failed: %v", err)
	}

	// 有效对象应保留
	_, err = store.GetData(keepHash)
	if err != nil {
		t.Error("kept object should still exist")
	}

	// 非 vcs 文件应保留（不被识别，不被删除）
	if _, err := os.Stat(shortFile); err != nil {
		t.Errorf("non-vcs file with short name should NOT be deleted: %v", err)
	}
	if _, err := os.Stat(invalidFile); err != nil {
		t.Errorf("non-vcs file with invalid dir prefix should NOT be deleted: %v", err)
	}

	// deleted 应为 0（keepHash 被保留，无效文件被跳过）
	if deleted != 0 {
		t.Errorf("PruneObjects deleted %d objects, want 0 (only invalid files exist besides kept object)", deleted)
	}
}

// TestObjectStore_PruneObjects_ConcurrentPut 验证 GC 与并发 Put/Get 共用同一把锁时
// 不会产生"撕裂"的对象：任何一次成功的 GetData 都必须返回与哈希一致的内容。
//
// 注意这里断言的不是"刚写完的对象一定不被删"——GC 的目的正是删除不可达对象，
// 一个未被 keepHashes 列出的对象被删是正确行为；要断言的是**不会读到半写文件**
// （删除阶段与读写互斥），且删除与重建并发时不会把对象文件写坏。
func TestObjectStore_PruneObjects_ConcurrentPut(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-prune-race-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewObjectStore(tmpDir)

	const writers = 4
	const perWriter = 60

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 40; i++ {
			if _, err := store.PruneObjects(map[string]bool{}, 0); err != nil {
				t.Errorf("PruneObjects failed: %v", err)
				return
			}
		}
	}()

	var wg sync.WaitGroup
	errs := make(chan error, writers*perWriter)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				data := []byte(fmt.Sprintf("w%d-i%d", w, i))
				h, err := store.PutData(data)
				if err != nil {
					errs <- fmt.Errorf("PutData: %w", err)
					return
				}
				// 对象可能已被并发的 GC 删除（它未在 keepHashes 中），这属于
				// 正确行为；但若仍存在，GetData 必须返回与其哈希一致的内容，
				// 绝不能是半写/被截断的文件（GetData 内部会校验 SHA-256，
				// 不一致会返回 ErrCorruptObject 而非静默通过）。
				got, err := store.GetData(h)
				if err != nil {
					if errors.Is(err, ErrCorruptObject) {
						errs <- fmt.Errorf("torn object read: %w", err)
						return
					}
					continue // 被 GC 删除，符合预期
				}
				if string(got) != string(data) {
					errs <- fmt.Errorf("content mismatch: got %q want %q", got, data)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	<-done
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestObjectStore_ConcurrentAccess(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewObjectStore(tmpDir)

	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func(n int) {
			content := []byte("concurrent content " + string(rune('0'+n)))
			hash, err := store.PutData(content)
			if err != nil {
				t.Errorf("concurrent PutData failed: %v", err)
				done <- false
				return
			}
			_, err = store.GetData(hash)
			if err != nil {
				t.Errorf("concurrent GetData failed: %v", err)
				done <- false
				return
			}
			done <- true
		}(i)
	}

	successCount := 0
	for i := 0; i < 10; i++ {
		if <-done {
			successCount++
		}
	}

	if successCount != 10 {
		t.Errorf("only %d/10 concurrent operations succeeded", successCount)
	}
}

// TestComputeSnapshotID_NoCollision 验证长度前缀编码防止字段拼接碰撞
// 旧实现：ParentID=""+Message="<64hex>" 与 ParentID="<64hex>"+Message="" 产生相同 ID
func TestComputeSnapshotID_NoCollision(t *testing.T) {
	validHash := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

	// 场景 A: 首次提交（ParentID=""），message 恰好是 64 位 hex
	snapA := &Snapshot{
		ParentID: "",
		Message:  validHash,
		Tree:     validHash,
		Metadata: nil,
	}

	// 场景 B: 子提交（ParentID=hash），message 为空
	snapB := &Snapshot{
		ParentID: validHash,
		Message:  "",
		Tree:     validHash,
		Metadata: nil,
	}

	idA := computeSnapshotID(snapA)
	idB := computeSnapshotID(snapB)

	if idA == idB {
		t.Errorf("snapshot ID collision: ParentID=\"\"+Message=hash vs ParentID=hash+Message=\"\" both produce %s", idA)
	}
}

// TestComputeSnapshotID_Deterministic 验证相同输入产生相同 ID
func TestComputeSnapshotID_Deterministic(t *testing.T) {
	ts := time.Unix(1700000000, 123456789)
	treeHash := "c0ffee00000000000000000000000000000000000000000000000000000000ff"
	makeSnap := func() *Snapshot {
		return &Snapshot{
			ParentID:  "parent123",
			Timestamp: ts,
			Message:   "test message",
			Tree:      treeHash,
			Metadata:  map[string]string{"key": "value"},
		}
	}

	if computeSnapshotID(makeSnap()) != computeSnapshotID(makeSnap()) {
		t.Error("same snapshot content should produce same ID")
	}
}

// TestComputeSnapshotID_TreeAffectsID 验证 tree 哈希影响 snapshot ID：不同文件树
// 必须产生不同 ID，否则内容不同的提交会互相覆盖。
func TestComputeSnapshotID_TreeAffectsID(t *testing.T) {
	ts := time.Unix(1700000000, 0)
	base := Snapshot{
		ParentID:  "parent123",
		Timestamp: ts,
		Message:   "same",
		Tree:      "aaaa000000000000000000000000000000000000000000000000000000000000",
		Metadata:  map[string]string{"key": "value"},
	}
	other := base
	other.Tree = "bbbb000000000000000000000000000000000000000000000000000000000000"

	if computeSnapshotID(&base) == computeSnapshotID(&other) {
		t.Fatal("snapshot ID should differ when the tree differs")
	}
}

// TestComputeSnapshotID_TimestampAffectsID 验证时间戳会影响 snapshot ID。
// 否则同一父快照 + 相同 message/tree/metadata 的重复提交会覆盖历史。
func TestComputeSnapshotID_TimestampAffectsID(t *testing.T) {
	treeHash := "c0ffee00000000000000000000000000000000000000000000000000000000ff"
	snap1 := &Snapshot{
		ParentID:  "parent123",
		Timestamp: time.Unix(1700000000, 0),
		Message:   "same",
		Tree:      treeHash,
		Metadata:  map[string]string{"key": "value"},
	}
	snap2 := &Snapshot{
		ParentID:  "parent123",
		Timestamp: time.Unix(1700000001, 0),
		Message:   "same",
		Tree:      treeHash,
		Metadata:  map[string]string{"key": "value"},
	}

	if computeSnapshotID(snap1) == computeSnapshotID(snap2) {
		t.Fatal("snapshot ID should differ when timestamps differ")
	}
}

// TestManager_CommitPending_DuplicateCommitDoesNotOverwriteHistory 验证两次相同内容的提交不会生成相同 ID 覆盖历史。
func TestManager_CommitPending_DuplicateCommitDoesNotOverwriteHistory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-duplicate-commit-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("same content"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState first failed: %v", err)
	}
	first, err := mgr.CommitPending("same message", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending first failed: %v", err)
	}

	// 再次记录并提交完全相同的内容与 message
	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState second failed: %v", err)
	}
	second, err := mgr.CommitPending("same message", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending second failed: %v", err)
	}
	if first.ID == second.ID {
		t.Fatal("duplicate commits should not reuse the same snapshot ID")
	}

	history, err := mgr.ListHistory(10)
	if err != nil {
		t.Fatalf("ListHistory failed: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2", len(history))
	}
	if history[0].ID != second.ID || history[1].ID != first.ID {
		t.Fatalf("history order mismatch: got [%s, %s], want [%s, %s]", history[0].ID, history[1].ID, second.ID, first.ID)
	}
}

// TestManager_IsIgnoredPath_PrefixMatching 验证多组件 vcsRelPath 的前缀匹配
func TestManager_IsIgnoredPath_PrefixMatching(t *testing.T) {
	// 场景 1: jaboDir 是 workDir 的子目录（典型生产配置）
	tmpDir, err := os.MkdirTemp("", "vcs-ignore-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	jaboDir := filepath.Join(tmpDir, ".jabo")
	mgr := NewManager(jaboDir, tmpDir)

	tests := []struct {
		name    string
		relPath string
		want    bool
	}{
		{"vcs snapshot file", ".jabo/vcs/snapshots/abc.json", true},
		{"vcs object file", ".jabo/vcs/objects/ab/cdef.json", true},
		{"jabo dir itself", ".jabo", true},
		{"normal file", "src/main.go", false},
		{"file with similar prefix", ".jabo_backup/config.json", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mgr.isIgnoredPath(tt.relPath); got != tt.want {
				t.Errorf("isIgnoredPath(%q) = %v, want %v", tt.relPath, got, tt.want)
			}
		})
	}
}

// TestManager_IsIgnoredPath_JaboDirEqWorkDir 验证 jaboDir==workDir 时忽略 vcs 子目录
// 这是测试环境常见配置，修复前 vcsRelPath="." 导致 isIgnoredPath 永远不匹配
func TestManager_IsIgnoredPath_JaboDirEqWorkDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-ignore-eq-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// jaboDir == workDir（测试常用配置）
	mgr := NewManager(tmpDir, tmpDir)

	tests := []struct {
		name    string
		relPath string
		want    bool
	}{
		{"vcs snapshot file", "vcs/snapshots/abc.json", true},
		{"vcs object file", "vcs/objects/ab/cdef.json", true},
		{"vcs dir itself", "vcs", true},
		{"normal file", "main.go", false},
		{"normal nested file", "src/pkg/util.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mgr.isIgnoredPath(tt.relPath); got != tt.want {
				t.Errorf("isIgnoredPath(%q) = %v, want %v", tt.relPath, got, tt.want)
			}
		})
	}
}

// TestManager_Checkout_PreservesVCSState 验证 Checkout 不会删除 vcs 元数据文件
// 修复前：当 jaboDir==workDir 时，vcs/snapshots/*.json 会被当作"额外文件"删除
func TestManager_Checkout_PreservesVCSState(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-preserve-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("original"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}
	snap, err := mgr.CommitPending("first commit", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}

	// 修改文件后 checkout 回去
	if err := os.WriteFile(testFile, []byte("modified"), 0644); err != nil {
		t.Fatalf("failed to modify file: %v", err)
	}
	if err := mgr.Checkout(snap.ID); err != nil {
		t.Fatalf("Checkout failed: %v", err)
	}

	// 验证 vcs 元数据文件仍然存在
	snapPath := filepath.Join(tmpDir, "vcs", "snapshots", snap.ID+".json")
	if _, err := os.Stat(snapPath); err != nil {
		t.Errorf("snapshot file should still exist after checkout: %v", err)
	}

	// 验证 ListHistory 仍能工作
	history, err := mgr.ListHistory(10)
	if err != nil {
		t.Errorf("ListHistory failed after checkout: %v", err)
	}
	if len(history) != 1 {
		t.Errorf("history length = %d, want 1 (vcs state should be preserved)", len(history))
	}
}

// TestManager_Checkout_MissingObject 验证当对象缺失时 pre-check 失败、工作区不动
func TestManager_Checkout_MissingObject(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-missing-obj-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("original"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}
	snap, err := mgr.CommitPending("first commit", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}

	// 删除底层 object 文件，模拟对象丢失
	objHash := snapFileTable(t, mgr, snap)["test.txt"]
	objPath := filepath.Join(tmpDir, "vcs", "objects", objHash[:2], objHash[2:])
	if err := os.Remove(objPath); err != nil {
		t.Fatalf("failed to remove object file: %v", err)
	}

	// 修改文件后尝试 checkout
	if err := os.WriteFile(testFile, []byte("modified"), 0644); err != nil {
		t.Fatalf("failed to modify file: %v", err)
	}

	err = mgr.Checkout(snap.ID)
	if err == nil {
		t.Error("Checkout with missing object should fail")
	}

	// 验证工作区未被修改（pre-check 失败应保持工作区不动）
	content, _ := os.ReadFile(testFile)
	if string(content) != "modified" {
		t.Errorf("workspace should be untouched when pre-check fails, got %q", string(content))
	}
}

// TestManager_Checkout_CorruptedSnapshot 验证 hash 校验防止 panic
// 修复前：op.hash[:12] 在 hash 不足 12 字符时会 panic
func TestManager_Checkout_CorruptedSnapshot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-corrupt-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	// 手动构造一个损坏的快照文件（tree hash 过短）
	corruptSnap := &Snapshot{
		ID:        "",
		ParentID:  "",
		Timestamp: time.Now(),
		Message:   "corrupt",
		Tree:      "short", // 无效 hash
		Metadata:  nil,
	}
	corruptSnap.ID = computeSnapshotID(corruptSnap)

	data, err := json.Marshal(corruptSnap)
	if err != nil {
		t.Fatalf("failed to marshal corrupt snapshot: %v", err)
	}

	snapDir := filepath.Join(tmpDir, "vcs", "snapshots")
	os.MkdirAll(snapDir, 0755)
	snapPath := filepath.Join(snapDir, corruptSnap.ID+".json")
	if err := os.WriteFile(snapPath, data, 0644); err != nil {
		t.Fatalf("failed to write corrupt snapshot: %v", err)
	}

	// Checkout 不应 panic，应返回 pre-check 错误
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Checkout panicked on corrupt snapshot: %v", r)
		}
	}()

	err = mgr.Checkout(corruptSnap.ID)
	if err == nil {
		t.Error("Checkout with corrupt snapshot should fail")
	}
}

// TestManager_Checkout_PathTraversal 验证快照含路径遍历（../）时 Checkout 不会写入 workDir 之外
// 修复前：filepath.Join 解析 ../，absPath 指向 workDir 上层，store.Get 写入任意位置
func TestManager_Checkout_PathTraversal(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-traversal-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	// 先正常 commit 一个文件建立 HEAD
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("original"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}
	if _, err := mgr.CommitPending("base", nil, nil); err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}

	// 构造一个含路径遍历的篡改快照
	store := mgr.store
	maliciousContent := []byte("malicious")
	maliciousHash, err := store.PutData(maliciousContent)
	if err != nil {
		t.Fatalf("PutData failed: %v", err)
	}

	// 目标路径：workDir 外的文件
	outsideTarget := filepath.Join(filepath.Dir(tmpDir), "pwned.txt")

	// 直接构造一个含 ".." 条目的恶意 tree 对象（绕过 treeBuilder 的校验），
	// 模拟被篡改的快照/对象。decodeTree 必须拒绝它，Checkout 必须 fail-closed，
	// 绝不能把内容写到 workDir 之外。
	rawTree, err := json.Marshal(&tree{Entries: []treeEntry{
		{Name: "..", Kind: kindTree, Hash: maliciousHash},
	}})
	if err != nil {
		t.Fatalf("failed to marshal malicious tree: %v", err)
	}
	treeHash, err := store.PutData(rawTree)
	if err != nil {
		t.Fatalf("failed to store malicious tree: %v", err)
	}

	maliciousSnap := &Snapshot{
		ParentID:  "",
		Timestamp: time.Now(),
		Message:   "malicious",
		Tree:      treeHash,
		Metadata:  nil,
	}
	maliciousSnap.ID = computeSnapshotID(maliciousSnap)

	data, err := json.Marshal(maliciousSnap)
	if err != nil {
		t.Fatalf("failed to marshal malicious snapshot: %v", err)
	}
	snapDir := filepath.Join(tmpDir, "vcs", "snapshots")
	os.MkdirAll(snapDir, 0755)
	snapPath := filepath.Join(snapDir, maliciousSnap.ID+".json")
	if err := os.WriteFile(snapPath, data, 0644); err != nil {
		t.Fatalf("failed to write malicious snapshot: %v", err)
	}

	// Checkout 必须拒绝损坏/恶意 tree（fail-closed）。
	if err := mgr.Checkout(maliciousSnap.ID); err == nil {
		t.Fatal("Checkout must reject a tree containing a path-traversal entry")
	}

	// 验证 workDir 外的文件没有被创建
	if _, err := os.Stat(outsideTarget); err == nil {
		os.Remove(outsideTarget) // 清理
		t.Errorf("path traversal: file %s should NOT be created outside workDir", outsideTarget)
	}
}

// TestManager_Checkout_VirtualFileSkipped 验证 extraFiles 中的虚拟文件（绝对路径）不参与 Checkout 恢复
func TestManager_Checkout_VirtualFileSkipped(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-virtual-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	// 正常文件
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("original"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}

	// 虚拟文件（绝对路径，不在 workDir 内）
	store := mgr.store
	virtualContent := []byte("virtual data")
	virtualHash, err := store.PutData(virtualContent)
	if err != nil {
		t.Fatalf("PutData failed: %v", err)
	}
	// 使用一个不存在的绝对路径作为虚拟文件
	virtualPath := filepath.Join(filepath.Dir(tmpDir), "virtual_memory_dump.json")
	extraFiles := map[string]string{virtualPath: virtualHash}

	snap, err := mgr.CommitPending("with virtual", nil, extraFiles)
	if err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}

	// 修改正常文件后 Checkout 回去
	if err := os.WriteFile(testFile, []byte("modified"), 0644); err != nil {
		t.Fatalf("failed to modify test file: %v", err)
	}
	if err := mgr.Checkout(snap.ID); err != nil {
		t.Fatalf("Checkout failed: %v", err)
	}

	// 正常文件应被恢复
	restored, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read restored file: %v", err)
	}
	if string(restored) != "original" {
		t.Errorf("test.txt = %q, want %q", string(restored), "original")
	}

	// 虚拟文件不应被写入磁盘
	if _, err := os.Stat(virtualPath); err == nil {
		os.Remove(virtualPath) // 清理
		t.Errorf("virtual file %s should NOT be created on disk", virtualPath)
	}
}

// TestManager_Checkout_RollbackRemovesNewlyCreatedFiles 验证 checkout 部分恢复失败时
// 已经新建出来但原本不存在的目标文件会被回滚删除，而不是残留在工作区。
func TestManager_Checkout_RollbackRemovesNewlyCreatedFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-checkout-rollback-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	store := mgr.store

	hash1, err := store.PutData([]byte("created during checkout"))
	if err != nil {
		t.Fatalf("PutData hash1 failed: %v", err)
	}
	hash2, err := store.PutData([]byte("will hit existing dir"))
	if err != nil {
		t.Fatalf("PutData hash2 failed: %v", err)
	}

	// 直接写入篡改快照：第一个文件可恢复成功，第二个目标路径预先是目录，
	// 这样 restore 阶段会在部分成功后失败，覆盖回滚路径。
	snap := &Snapshot{
		Timestamp: time.Now(),
		Message:   "partial checkout failure",
		Tree: buildRootTree(t, mgr, map[string]string{
			"created.txt": hash1,
			"blocked":     hash2,
		}),
	}
	snap.ID = computeSnapshotID(snap)
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("failed to marshal snapshot: %v", err)
	}
	snapDir := filepath.Join(tmpDir, "vcs", "snapshots")
	if err := os.MkdirAll(snapDir, 0755); err != nil {
		t.Fatalf("failed to create snapshots dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(snapDir, snap.ID+".json"), data, 0644); err != nil {
		t.Fatalf("failed to write snapshot: %v", err)
	}

	// 让 blocked 目标在 restore 时失败：目标路径是一个已存在的非空目录。
	if err := os.Mkdir(filepath.Join(tmpDir, "blocked"), 0755); err != nil {
		t.Fatalf("failed to create blocked dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "blocked", "keep.txt"), []byte("keep"), 0644); err != nil {
		t.Fatalf("failed to seed blocked dir: %v", err)
	}

	err = mgr.Checkout(snap.ID)
	if err == nil {
		t.Fatal("Checkout should fail when one restore target path is an existing directory")
	}

	// 修复前：created.txt 已被成功写出，但失败回滚不会删除它，导致工作区残留脏文件。
	// 修复后：created.txt 应被删除。
	if _, err := os.Stat(filepath.Join(tmpDir, "created.txt")); !os.IsNotExist(err) {
		t.Fatalf("created.txt should be removed during rollback, got err=%v", err)
	}
	// blocked 应仍然是目录，而不是被文件覆盖。
	info, err := os.Stat(filepath.Join(tmpDir, "blocked"))
	if err != nil {
		t.Fatalf("blocked should still exist as directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("blocked should remain a directory after rollback")
	}
}

// TestManager_Checkout_BackupDirIsCleanedUp 验证 #4 的临时目录备份不会在工作区
// 或状态目录里留下残留：成功与失败两条路径都必须清理 vcs/tmp 下的备份目录。
func TestManager_Checkout_BackupDirIsCleanedUp(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-backup-cleanup-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	file := filepath.Join(tmpDir, "a.txt")
	if err := os.WriteFile(file, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RecordOldState(file); err != nil {
		t.Fatal(err)
	}
	snap, err := mgr.CommitPending("base", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 成功路径：checkout 成功后备份目录必须被删除。
	if err := mgr.Checkout(snap.ID); err != nil {
		t.Fatalf("Checkout failed: %v", err)
	}
	assertNoBackupDirs(t, mgr)

	// 失败路径：HEAD 指向目录使 checkout 失败，备份目录同样必须被删除。
	if err := os.WriteFile(file, []byte("v3"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr.headFile = tmpDir
	if err := mgr.Checkout(snap.ID); err == nil {
		t.Fatal("Checkout should fail when HEAD update fails")
	}
	assertNoBackupDirs(t, mgr)

	// 备份目录不在工作区内，不得被工作区扫描当作内容文件。
	if _, err := os.Stat(filepath.Join(tmpDir, "tmp")); !os.IsNotExist(err) {
		t.Fatalf("backup must live under vcs/, not the work tree (got err=%v)", err)
	}
}

// assertNoBackupDirs fails if any checkout backup directory remains under the
// VCS tmp root.
func assertNoBackupDirs(t *testing.T, mgr *Manager) {
	t.Helper()
	tmpRoot := filepath.Join(mgr.baseDir, "tmp")
	entries, err := os.ReadDir(tmpRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("failed to read backup root %s: %v", tmpRoot, err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("checkout left backup dirs behind: %v", names)
	}
}

// TestManager_Checkout_HeadWriteFailureRollsBackWorkspace 验证 HEAD 更新失败时，工作区也会回滚。
func TestManager_Checkout_HeadWriteFailureRollsBackWorkspace(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-checkout-headfail-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("v1"), 0644); err != nil {
		t.Fatalf("failed to write v1: %v", err)
	}
	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}
	first, err := mgr.CommitPending("first", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending first failed: %v", err)
	}
	if err := os.WriteFile(testFile, []byte("v2"), 0644); err != nil {
		t.Fatalf("failed to write v2: %v", err)
	}
	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}
	second, err := mgr.CommitPending("second", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending second failed: %v", err)
	}

	// 让 HEAD 路径指向目录，触发 AtomicWriteFile 失败。
	mgr.headFile = tmpDir
	err = mgr.Checkout(first.ID)
	if err == nil {
		t.Fatal("Checkout should fail when HEAD update fails")
	}

	data2, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read file after rollback: %v", err)
	}
	if string(data2) != "v2" {
		t.Fatalf("workspace should be rolled back to pre-checkout state v2, got %q", string(data2))
	}

	// 恢复原 HEAD 路径后验证 HEAD 仍是旧值 second.ID
	mgr.headFile = filepath.Join(tmpDir, "vcs", "HEAD")
	headID, err := mgr.readHEAD()
	if err != nil {
		t.Fatalf("failed to read real HEAD: %v", err)
	}
	if headID != second.ID {
		t.Fatalf("HEAD should remain at old snapshot %s, got %s", second.ID, headID)
	}
}

// TestManager_Checkout_ClearsPendingChanges 验证成功 checkout 后清空 pendingChanges，避免后续 RollbackPending 撤销成功 checkout。
func TestManager_Checkout_ClearsPendingChanges(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-checkout-pending-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("v1"), 0644); err != nil {
		t.Fatalf("failed to write v1: %v", err)
	}
	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}
	first, err := mgr.CommitPending("first", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending first failed: %v", err)
	}
	if err := os.WriteFile(testFile, []byte("dirty"), 0644); err != nil {
		t.Fatalf("failed to write dirty: %v", err)
	}
	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState dirty failed: %v", err)
	}

	if !mgr.HasPendingChanges() {
		t.Fatal("pending changes should exist before checkout")
	}
	if err := mgr.Checkout(first.ID); err != nil {
		t.Fatalf("Checkout failed: %v", err)
	}
	if mgr.HasPendingChanges() {
		t.Fatal("pending changes should be cleared after successful checkout")
	}
	if err := mgr.RollbackPending(); err != nil {
		t.Fatalf("RollbackPending after checkout should be a no-op, got: %v", err)
	}
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(data) != "v1" {
		t.Fatalf("RollbackPending after checkout should not undo checkout, got %q", string(data))
	}
}

// TestManager_DiffSnapshots_EmptyOld 验证 oldID="" 时与空快照对比
func TestManager_DiffSnapshots_EmptyOld(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-diff-empty-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	testFile := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}
	snap, err := mgr.CommitPending("commit", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}

	// oldID="" 表示与空状态对比，所有文件都应该是 added
	diffs, err := mgr.DiffSnapshots("", snap.ID)
	if err != nil {
		t.Fatalf("DiffSnapshots with empty oldID failed: %v", err)
	}

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}

	diff, ok := diffs["file.txt"]
	if !ok {
		t.Fatal("file.txt not in diffs")
	}
	if diff.Status != DiffAdded {
		t.Errorf("diff status = %s, want %s", diff.Status, DiffAdded)
	}
}

// TestManager_DiffSnapshots_SameSnapshot 验证相同快照对比返回空 diff
func TestManager_DiffSnapshots_SameSnapshot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-diff-same-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	testFile := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	if err := mgr.RecordOldState(testFile); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}
	snap, err := mgr.CommitPending("commit", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}

	diffs, err := mgr.DiffSnapshots(snap.ID, snap.ID)
	if err != nil {
		t.Fatalf("DiffSnapshots failed: %v", err)
	}

	if len(diffs) != 0 {
		t.Errorf("same snapshot should have 0 diffs, got %d", len(diffs))
	}
}

// TestManager_ConcurrentSubAgentWritesKeepOldestState 模拟多个子代理并发写
// 同一文件：每个 goroutine 先 RecordOldState（对应 PreWriteProvider 回调），
// 再写入各自内容。因 RecordOldState 对同一 path 只保留首次旧状态，所以
// RollbackPending 后文件必须恢复到最初的 v0 内容，而不是某个中间版本。
func TestManager_ConcurrentSubAgentWritesKeepOldestState(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-concurrent-rollback-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	shared := filepath.Join(tmpDir, "shared.txt")
	original := []byte("v0-original")
	if err := os.WriteFile(shared, original, 0644); err != nil {
		t.Fatalf("failed to write original file: %v", err)
	}

	const agents = 20
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, agents)

	for i := 0; i < agents; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			if err := mgr.RecordOldState(shared); err != nil {
				errs <- fmt.Errorf("agent %d RecordOldState: %w", n, err)
				return
			}
			content := []byte(fmt.Sprintf("agent-%d", n))
			if err := os.WriteFile(shared, content, 0644); err != nil {
				errs <- fmt.Errorf("agent %d write: %w", n, err)
			}
		}(i)
	}

	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	// 所有子代理都结束后，回滚应只恢复到最旧的 v0 内容。
	if err := mgr.RollbackPending(); err != nil {
		t.Fatalf("RollbackPending failed: %v", err)
	}

	got, err := os.ReadFile(shared)
	if err != nil {
		t.Fatalf("failed to read restored file: %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("restored content = %q, want oldest state %q", string(got), string(original))
	}

	if mgr.HasPendingChanges() {
		t.Fatal("pending changes should be empty after rollback")
	}
}

// TestManager_ConcurrentSubAgentWritesNewFileRemovedOnRollback 验证对尚不存在的
// 文件并发写时，回滚后该文件被删除（旧状态为空 hash 表示"删除"）。
func TestManager_ConcurrentSubAgentWritesNewFileRemovedOnRollback(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-concurrent-newfile-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	newFile := filepath.Join(tmpDir, "new.txt")
	const agents = 10
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, agents)

	for i := 0; i < agents; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			if err := mgr.RecordOldState(newFile); err != nil {
				errs <- fmt.Errorf("agent %d RecordOldState: %w", n, err)
				return
			}
			if err := os.WriteFile(newFile, []byte(fmt.Sprintf("agent-%d", n)), 0644); err != nil {
				errs <- fmt.Errorf("agent %d write: %w", n, err)
			}
		}(i)
	}

	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	if err := mgr.RollbackPending(); err != nil {
		t.Fatalf("RollbackPending failed: %v", err)
	}

	if _, err := os.Stat(newFile); !os.IsNotExist(err) {
		t.Fatal("new file should be deleted after rollback")
	}

	if mgr.HasPendingChanges() {
		t.Fatal("pending changes should be empty after rollback")
	}
}

// ================ 子代理级 checkpoint 行为 ================

// TestManager_BeginCheckpoint 验证 checkpoint 作用域的基本创建规则。
func TestManager_BeginCheckpoint(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-checkpoint-begin-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	if err := mgr.BeginCheckpoint(""); err == nil {
		t.Error("BeginCheckpoint with empty id should fail")
	}

	if err := mgr.BeginCheckpoint("agent-1"); err != nil {
		t.Fatalf("BeginCheckpoint with valid id failed: %v", err)
	}

	if err := mgr.BeginCheckpoint("agent-1"); err == nil {
		t.Error("BeginCheckpoint with duplicate id should fail")
	}
}

// TestManager_RecordOldStateIn 验证 checkpoint 内留档的归属与去重。
func TestManager_RecordOldStateIn(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-checkpoint-record-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	file := filepath.Join(tmpDir, "f.txt")
	if err := os.WriteFile(file, []byte("v0"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// 未 begin 的 checkpoint id 应报错。
	if err := mgr.RecordOldStateIn("ghost", file); err == nil {
		t.Error("RecordOldStateIn with unknown checkpoint id should fail")
	}

	if err := mgr.BeginCheckpoint("agent-1"); err != nil {
		t.Fatalf("BeginCheckpoint failed: %v", err)
	}
	if err := mgr.RecordOldStateIn("agent-1", file); err != nil {
		t.Fatalf("RecordOldStateIn failed: %v", err)
	}
	// 同一 checkpoint 内同一 path 重复记录是 no-op。
	if err := mgr.RecordOldStateIn("agent-1", file); err != nil {
		t.Fatalf("second RecordOldStateIn should not fail: %v", err)
	}

	// 回合级 pending 不应被 checkpoint 记录污染。
	if mgr.HasPendingChanges() == false {
		t.Fatal("HasPendingChanges should be true (checkpoint has changes)")
	}
	if len(mgr.pendingChanges) != 0 {
		t.Errorf("turn-level pendingChanges should be empty, got %d entries", len(mgr.pendingChanges))
	}
}

// TestManager_RollbackCheckpoint 验证 checkpoint 回滚恢复到该作用域写前状态。
func TestManager_RollbackCheckpoint(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-checkpoint-rollback-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	file := filepath.Join(tmpDir, "f.txt")
	if err := os.WriteFile(file, []byte("v0"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	if err := mgr.BeginCheckpoint("agent-1"); err != nil {
		t.Fatalf("BeginCheckpoint failed: %v", err)
	}
	if err := mgr.RecordOldStateIn("agent-1", file); err != nil {
		t.Fatalf("RecordOldStateIn failed: %v", err)
	}
	if err := os.WriteFile(file, []byte("v1"), 0644); err != nil {
		t.Fatalf("failed to modify file: %v", err)
	}

	if err := mgr.RollbackCheckpoint("agent-1"); err != nil {
		t.Fatalf("RollbackCheckpoint failed: %v", err)
	}

	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("failed to read restored file: %v", err)
	}
	if string(got) != "v0" {
		t.Errorf("restored content = %q, want %q", string(got), "v0")
	}

	// 回滚后该 checkpoint 应从作用域表移除。
	if err := mgr.RollbackCheckpoint("agent-1"); err == nil {
		t.Error("RollbackCheckpoint on already-removed id should fail")
	}
}

// TestManager_RollbackCheckpoint_UnknownID 验证回滚未知 id 报错。
func TestManager_RollbackCheckpoint_UnknownID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-checkpoint-rollback-unknown-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	if err := mgr.RollbackCheckpoint("ghost"); err == nil {
		t.Error("RollbackCheckpoint with unknown id should fail")
	}
}

// TestManager_CommitCheckpoint_KeepsOldest 验证合并时"回合级已存在路径优先"，
// 即回合级先记录的最旧状态不会被 checkpoint 里的较新状态覆盖。
func TestManager_CommitCheckpoint_KeepsOldest(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-checkpoint-commit-oldest-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	file := filepath.Join(tmpDir, "f.txt")
	if err := os.WriteFile(file, []byte("v0"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// 回合级先记录 v0（最旧）。
	if err := mgr.RecordOldState(file); err != nil {
		t.Fatalf("RecordOldState (turn-level) failed: %v", err)
	}
	if err := os.WriteFile(file, []byte("v1"), 0644); err != nil {
		t.Fatalf("failed to write v1: %v", err)
	}

	// 子代理 checkpoint 记录 v1（较新）。
	if err := mgr.BeginCheckpoint("agent-1"); err != nil {
		t.Fatalf("BeginCheckpoint failed: %v", err)
	}
	if err := mgr.RecordOldStateIn("agent-1", file); err != nil {
		t.Fatalf("RecordOldStateIn failed: %v", err)
	}
	if err := os.WriteFile(file, []byte("v2"), 0644); err != nil {
		t.Fatalf("failed to write v2: %v", err)
	}

	if err := mgr.CommitCheckpoint("agent-1"); err != nil {
		t.Fatalf("CommitCheckpoint failed: %v", err)
	}

	// 回合级 pending 必须仍是 v0（最旧），回滚应恢复到 v0。
	if err := mgr.RollbackPending(); err != nil {
		t.Fatalf("RollbackPending failed: %v", err)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("failed to read restored file: %v", err)
	}
	if string(got) != "v0" {
		t.Errorf("restored content = %q, want oldest %q", string(got), "v0")
	}
}

// TestManager_CommitCheckpoint_MergesNewPath 验证 checkpoint 里回合级尚未记录的
// 新路径会被合并进回合级 pending。
func TestManager_CommitCheckpoint_MergesNewPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-checkpoint-commit-new-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	file := filepath.Join(tmpDir, "f.txt")
	if err := os.WriteFile(file, []byte("v0"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	if err := mgr.BeginCheckpoint("agent-1"); err != nil {
		t.Fatalf("BeginCheckpoint failed: %v", err)
	}
	if err := mgr.RecordOldStateIn("agent-1", file); err != nil {
		t.Fatalf("RecordOldStateIn failed: %v", err)
	}
	if err := os.WriteFile(file, []byte("v1"), 0644); err != nil {
		t.Fatalf("failed to modify file: %v", err)
	}

	if err := mgr.CommitCheckpoint("agent-1"); err != nil {
		t.Fatalf("CommitCheckpoint failed: %v", err)
	}

	// 合并后回合级 pending 应包含该路径，回滚恢复到 v0。
	if len(mgr.pendingChanges) != 1 {
		t.Errorf("turn-level pendingChanges should have 1 entry after merge, got %d", len(mgr.pendingChanges))
	}
	if err := mgr.RollbackPending(); err != nil {
		t.Fatalf("RollbackPending failed: %v", err)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("failed to read restored file: %v", err)
	}
	if string(got) != "v0" {
		t.Errorf("restored content = %q, want %q", string(got), "v0")
	}

	// 合并后 checkpoint 应已移除。
	if err := mgr.CommitCheckpoint("agent-1"); err == nil {
		t.Error("CommitCheckpoint on already-committed id should fail")
	}
}

// TestManager_CommitCheckpoint_UnknownID 验证提交未知 id 报错。
func TestManager_CommitCheckpoint_UnknownID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-checkpoint-commit-unknown-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	if err := mgr.CommitCheckpoint("ghost"); err == nil {
		t.Error("CommitCheckpoint with unknown id should fail")
	}
}

// TestManager_RollbackPending_RollsBackLeftoverCheckpoints 验证回合级回滚会防御性地
// 回滚残留 checkpoint（正常流程下子代理应已 commit/rollback，此处模拟异常残留）。
func TestManager_RollbackPending_RollsBackLeftoverCheckpoints(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-checkpoint-leftover-rollback-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	file := filepath.Join(tmpDir, "f.txt")
	if err := os.WriteFile(file, []byte("v0"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	if err := mgr.BeginCheckpoint("agent-1"); err != nil {
		t.Fatalf("BeginCheckpoint failed: %v", err)
	}
	if err := mgr.RecordOldStateIn("agent-1", file); err != nil {
		t.Fatalf("RecordOldStateIn failed: %v", err)
	}
	if err := os.WriteFile(file, []byte("dirty"), 0644); err != nil {
		t.Fatalf("failed to modify file: %v", err)
	}

	if err := mgr.RollbackPending(); err != nil {
		t.Fatalf("RollbackPending failed: %v", err)
	}

	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("failed to read restored file: %v", err)
	}
	if string(got) != "v0" {
		t.Errorf("restored content = %q, want %q", string(got), "v0")
	}

	if mgr.HasPendingChanges() {
		t.Error("pending changes (including checkpoints) should be empty after rollback")
	}
}

// TestManager_CommitPending_MergesLeftoverCheckpoints 验证提交时防御性合并残留
// checkpoint，使快照与磁盘状态一致（快照记录的是当前磁盘上的新内容）。
func TestManager_CommitPending_MergesLeftoverCheckpoints(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-checkpoint-leftover-commit-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	file := filepath.Join(tmpDir, "f.txt")
	if err := os.WriteFile(file, []byte("v0"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	if err := mgr.BeginCheckpoint("agent-1"); err != nil {
		t.Fatalf("BeginCheckpoint failed: %v", err)
	}
	if err := mgr.RecordOldStateIn("agent-1", file); err != nil {
		t.Fatalf("RecordOldStateIn failed: %v", err)
	}
	if err := os.WriteFile(file, []byte("v1"), 0644); err != nil {
		t.Fatalf("failed to modify file: %v", err)
	}

	snap, err := mgr.CommitPending("commit with leftover checkpoint", nil, nil)
	if err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}
	if snap == nil {
		t.Fatal("CommitPending returned nil snapshot")
	}

	hash, ok := snapFileTable(t, mgr, snap)["f.txt"]
	if !ok {
		t.Fatal("f.txt should be in snapshot")
	}
	if hash == "" {
		t.Error("f.txt hash should not be empty")
	}

	if mgr.HasPendingChanges() {
		t.Error("pending changes (including checkpoints) should be empty after commit")
	}
}

// TestManager_PutBlob_GetBlob 验证非工作树状态（如任务记忆）能以 blob 形式
// 存进对象库并原样取回，供快照 metadata 通过 hash 引用。
func TestManager_PutBlob_GetBlob(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-blob-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)

	payload := []byte(`{"goal":"fix foo","phase":"executing"}`)
	hash, err := mgr.PutBlob(payload)
	if err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}
	if err := isValidHash(hash); err != nil {
		t.Fatalf("PutBlob returned invalid hash %q: %v", hash, err)
	}

	got, err := mgr.GetBlob(hash)
	if err != nil {
		t.Fatalf("GetBlob failed: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("GetBlob = %q, want %q", string(got), string(payload))
	}
}

// TestManager_GetBlob_UnknownHash 验证取回未知 hash 报错。
func TestManager_GetBlob_UnknownHash(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-blob-unknown-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	validHash := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	if _, err := mgr.GetBlob(validHash); err == nil {
		t.Error("GetBlob with unknown hash should fail")
	}
}

// TestManager_GC_PreservesMetadataBlob 验证快照 metadata 里引用的对象 hash
// 会被 GC 保护，不会在修剪历史时被误删（支撑"任务记忆随快照版本化"）。
func TestManager_GC_PreservesMetadataBlob(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-gc-metadata-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	store := mgr.store

	memBlob := []byte(`{"goal":"restore me"}`)
	memHash, err := mgr.PutBlob(memBlob)
	if err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	file := filepath.Join(tmpDir, "f.txt")
	if err := os.WriteFile(file, []byte("v0"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	if err := mgr.RecordOldState(file); err != nil {
		t.Fatalf("RecordOldState failed: %v", err)
	}

	// 提交一个快照，metadata 里引用 memory blob 的 hash。
	if _, err := mgr.CommitPending("turn", map[string]string{"memory_hash": memHash}, nil); err != nil {
		t.Fatalf("CommitPending failed: %v", err)
	}

	// 对象必须仍存在（GC 会保护 metadata 引用的 hash）。
	if !store.Exists(memHash) {
		t.Fatal("memory blob should exist after commit")
	}

	// GC(1) 只保留最近 1 个快照，但该快照 metadata 引用了 memory blob，
	// 修剪后 blob 必须仍存在（旧实现会误删）。
	if _, err := mgr.GC(1); err != nil {
		t.Fatalf("GC failed: %v", err)
	}
	if !store.Exists(memHash) {
		t.Fatal("memory blob should survive GC because snapshot metadata references it")
	}
}

// makeSymlink 创建符号链接；Windows 上如缺少权限/开发者模式则跳过测试。
func makeSymlink(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Symlink(oldname, newname); err != nil {
		t.Skipf("symlinks unavailable on this system: %v", err)
	}
}

// TestManager_isValidPath_SymlinkLogic 不依赖真实 symlink 权限，直接验证
// 统一路径校验对"合法嵌套 / 已存在不越界 / 越界 symlink 解析"的判定。
// 该测试利用 EvalSymlinks 对真实存在目录的解析结果，覆盖 checkNoSymlinkEscape
// 的越界分支；symlink 本身不可用时仅覆盖前两个分支。
func TestManager_isValidPath_Containment(t *testing.T) {
	workDir, err := os.MkdirTemp("", "vcs-work-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workDir)

	mgr := NewManager(filepath.Join(workDir, ".jabo"), workDir)

	// 合法：工作区内尚不存在的新路径，按最近存在祖先校验通过。
	if err := mgr.isValidPath(filepath.Join(workDir, "new", "dir", "f.txt")); err != nil {
		t.Fatalf("in-tree new path rejected: %v", err)
	}

	// 合法：已存在的嵌套文件。
	sub := filepath.Join(workDir, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(sub, "a.txt")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.isValidPath(f); err != nil {
		t.Fatalf("existing in-tree file rejected: %v", err)
	}

	// 非法：词法越界。
	if err := mgr.isValidPath(filepath.Join(workDir, "..", "escape.txt")); !errors.Is(err, ErrPathTraversal) {
		t.Fatalf("lexical traversal err = %v, want ErrPathTraversal", err)
	}

	// 空路径非法。
	if err := mgr.isValidPath(""); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("empty path err = %v, want ErrInvalidPath", err)
	}
}

// TestManager_RecordOldState_RejectsSymlinkEscape 验证：工作区内的目录
// 符号链接指向工作区外时，不得接受其下的路径（否则回滚会写到工作区外）。
func TestManager_RecordOldState_RejectsSymlinkEscape(t *testing.T) {
	workDir, err := os.MkdirTemp("", "vcs-work-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workDir)

	outside, err := os.MkdirTemp("", "vcs-outside-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outside)

	makeSymlink(t, outside, filepath.Join(workDir, "link"))

	mgr := NewManager(filepath.Join(workDir, ".jabo"), workDir)
	escaped := filepath.Join(workDir, "link", "out.txt")
	if err := mgr.RecordOldState(escaped); !errors.Is(err, ErrSymlinkEscape) {
		t.Fatalf("RecordOldState via symlink err = %v, want ErrSymlinkEscape", err)
	}
}

// TestManager_Checkout_RejectsSymlinkEscape 验证：被篡改的快照若通过
// symlink 指向工作区外，checkout 必须跳过该条目，绝不写入工作区外。
func TestManager_Checkout_RejectsSymlinkEscape(t *testing.T) {
	workDir, err := os.MkdirTemp("", "vcs-work-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workDir)

	outside, err := os.MkdirTemp("", "vcs-outside-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outside)

	mgr := NewManager(filepath.Join(workDir, ".jabo"), workDir)

	// 建立一个正常快照，然后手工注入一个通过 symlink 逃逸的条目。
	file := filepath.Join(workDir, "a.txt")
	if err := os.WriteFile(file, []byte("in-tree"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RecordOldState(file); err != nil {
		t.Fatal(err)
	}
	snap, err := mgr.CommitPending("base", nil, nil)
	if err != nil || snap == nil {
		t.Fatalf("CommitPending: %v, %v", snap, err)
	}

	makeSymlink(t, outside, filepath.Join(workDir, "link"))

	evil := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(evil, []byte("untouched"), 0644); err != nil {
		t.Fatal(err)
	}

	// 构造一个 tree 含 link/victim.txt（link 是指向 workDir 之外的符号链接）的
	// 篡改快照，验证 Checkout 的 symlink 逃逸防护。
	hash, err := mgr.PutBlob([]byte("malicious"))
	if err != nil {
		t.Fatal(err)
	}
	snapPath := filepath.Join(mgr.baseDir, "snapshots", snap.ID+".json")
	raw, err := os.ReadFile(snapPath)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Snapshot
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatal(err)
	}
	// 读取父快照的现有文件表并加入逃逸条目，重建 tree。
	baseFiles := snapFileTable(t, mgr, snap)
	baseFiles["link/victim.txt"] = hash
	loaded.Tree = buildRootTree(t, mgr, baseFiles)
	loaded.ID = ""
	loaded.ID = computeSnapshotID(&loaded)
	tampered, err := json.Marshal(&loaded)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AtomicWriteFile(filepath.Join(mgr.baseDir, "snapshots", loaded.ID+".json"), tampered, 0644); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Checkout(loaded.ID); err != nil {
		t.Fatalf("Checkout failed: %v", err)
	}

	// 工作区外的文件必须保持原样。
	if got, _ := os.ReadFile(evil); string(got) != "untouched" {
		t.Fatalf("checkout escaped work tree and overwrote %s: %q", evil, got)
	}
}

// TestManager_Rollback_RejectsSymlinkSwap 验证：记录后若路径被 symlink
// 替换，回滚必须 fail-closed，不写到工作区外。
func TestManager_Rollback_RejectsSymlinkSwap(t *testing.T) {
	workDir, err := os.MkdirTemp("", "vcs-work-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workDir)

	outside, err := os.MkdirTemp("", "vcs-outside-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outside)

	mgr := NewManager(filepath.Join(workDir, ".jabo"), workDir)

	// 先在工作区写文件并记录其写前状态。
	dir := filepath.Join(workDir, "sub")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RecordOldState(target); err != nil {
		t.Fatal(err)
	}

	// 记录后把 sub 换成指向工作区外的 symlink，再模拟一次修改。
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	makeSymlink(t, outside, dir)
	evil := filepath.Join(outside, "f.txt")
	if err := os.WriteFile(evil, []byte("outside-state"), 0644); err != nil {
		t.Fatal(err)
	}

	// 回滚必须报告失败（拒绝写入），且工作区外文件不被覆盖。
	if err := mgr.RollbackPending(); err == nil {
		t.Fatal("RollbackPending must fail closed on symlink swap")
	}
	if got, _ := os.ReadFile(evil); string(got) != "outside-state" {
		t.Fatalf("rollback escaped work tree and overwrote %s: %q", evil, got)
	}
}

// TestManager_SharedStateDir_ConcurrentCommitsNoLoss 验证：两个 Manager 实例
// 共享同一 stateDir 并发提交时，跨进程锁保证读 HEAD / 写 HEAD 序列化，
// 所有提交都可在 HEAD 链上被追溯（旧实现会覆盖 HEAD、丢提交）。
func TestManager_SharedStateDir_ConcurrentCommitsNoLoss(t *testing.T) {
	workDir, err := os.MkdirTemp("", "vcs-shared-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workDir)

	stateDir := filepath.Join(workDir, ".jabo")
	mgrA := NewManager(stateDir, workDir)
	if err := mgrA.Init(); err != nil {
		t.Fatal(err)
	}

	const commits = 8
	var wg sync.WaitGroup
	errs := make(chan error, commits)

	for i := 0; i < commits; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Each goroutine owns its own Manager instance, mirroring separate
			// processes that share the state directory. Sharing one Manager
			// across goroutines would race on its in-memory pendingChanges set
			// (that state is deliberately not synchronized by the state-dir
			// lock), which is not the deployment scenario under test.
			mgr := NewManager(stateDir, workDir)
			name := fmt.Sprintf("f-%d.txt", i)
			path := filepath.Join(workDir, name)
			if err := os.WriteFile(path, []byte(fmt.Sprintf("content-%d", i)), 0644); err != nil {
				errs <- err
				return
			}
			if err := mgr.RecordOldState(path); err != nil {
				errs <- err
				return
			}
			if _, err := mgr.CommitPending(name, nil, nil); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent commit failed: %v", err)
	}

	// 沿 HEAD 链回溯，必须能找到全部 8 次提交（无提交因覆盖 HEAD 丢失）。
	history, err := mgrA.ListHistory(commits + 2)
	if err != nil {
		t.Fatalf("ListHistory failed: %v", err)
	}
	if len(history) != commits {
		t.Fatalf("history length = %d, want %d (commits were lost)", len(history), commits)
	}

	seen := make(map[string]bool)
	for _, snap := range history {
		for path := range snapFileTable(t, mgrA, snap) {
			seen[path] = true
		}
	}
	for i := 0; i < commits; i++ {
		name := fmt.Sprintf("f-%d.txt", i)
		if !seen[name] {
			t.Errorf("%s missing from committed history chain", name)
		}
	}
}

// TestManager_StateLock_SerializesHolders 验证：锁被持有时其他 acquirer 等待，
// 释放后可获取；且不会永久卡死。
func TestManager_StateLock_SerializesHolders(t *testing.T) {
	workDir, err := os.MkdirTemp("", "vcs-lock-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workDir)

	mgr := NewManager(filepath.Join(workDir, ".jabo"), workDir)
	if err := mgr.Init(); err != nil {
		t.Fatal(err)
	}

	lock, err := mgr.acquireStateLock(time.Second)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// 第二个 acquirer 在锁被持有时应超时而非立即成功。
	start := time.Now()
	if _, err := mgr.acquireStateLock(120 * time.Millisecond); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("second acquire err = %v, want ErrLockTimeout", err)
	}
	if time.Since(start) < 100*time.Millisecond {
		t.Fatal("contended acquire returned too early; it must wait")
	}

	lock.release()

	// 释放后应能获取。
	l2, err := mgr.acquireStateLock(time.Second)
	if err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
	l2.release()
}

// TestManager_StateLock_ReclaimsDeadOwner 验证：持有者崩溃（记录了过期时间戳
// 且 PID 不存活）后，锁可被回收，不会永久污染 stateDir。
func TestManager_StateLock_ReclaimsDeadOwner(t *testing.T) {
	workDir, err := os.MkdirTemp("", "vcs-stale-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workDir)

	mgr := NewManager(filepath.Join(workDir, ".jabo"), workDir)
	if err := mgr.Init(); err != nil {
		t.Fatal(err)
	}

	// 手工写一个过期且 owner 已死的锁。
	stale := lockRecord{PID: 1 << 30, Timestamp: time.Now().Add(-2 * lockStaleAfter)}
	data, _ := json.Marshal(stale)
	if err := os.WriteFile(mgr.lockPath(), data, 0644); err != nil {
		t.Fatal(err)
	}

	lock, err := mgr.acquireStateLock(2 * time.Second)
	if err != nil {
		t.Fatalf("acquire over stale lock failed: %v", err)
	}
	lock.release()
}

// === V4: 失败路径语义与输入边界 ===

// TestManager_Checkout_RollbackReportsIncompleteRollback 验证：checkout 恢复
// 失败、且回滚写回也失败时，错误必须如实报告"回滚不完整"，不能谎报
// "workspace rolled back"。
//
// 构造：快照恢复 ro/keep.txt（ro 是只读目录 → AtomicWriteFile 失败），同时
// 工作区里 ro/keep.txt 的旧内容需要备份；回滚时写回同一只读目录同样失败，
// 于是回滚错误被聚合上报。
func TestManager_Checkout_RollbackReportsIncompleteRollback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs POSIX directory write permissions")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root ignores directory permissions")
	}

	tmpDir, err := os.MkdirTemp("", "vcs-rollback-report-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		os.Chmod(filepath.Join(tmpDir, "ro"), 0o755)
		os.RemoveAll(tmpDir)
	}()

	mgr := NewManager(tmpDir, tmpDir)
	store := mgr.store

	hash, err := store.PutData([]byte("new content"))
	if err != nil {
		t.Fatal(err)
	}

	snap := &Snapshot{
		Timestamp: time.Now(),
		Message:   "body",
		Tree:      buildRootTree(t, mgr, map[string]string{filepath.Join("ro", "keep.txt"): hash}),
	}
	snap.ID = computeSnapshotID(snap)
	data, _ := json.Marshal(snap)
	snapDir := filepath.Join(tmpDir, "vcs", "snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapDir, snap.ID+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// ro/keep.txt 已存在（既有内容 → 备份成立），随后把 ro 设为只读：
	// restore 与 rollback 的写回都会失败。
	roDir := filepath.Join(tmpDir, "ro")
	if err := os.Mkdir(roDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roDir, "keep.txt"), []byte("old content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(roDir, 0o555); err != nil {
		t.Fatal(err)
	}

	err = mgr.Checkout(snap.ID)
	if err == nil {
		t.Fatal("Checkout should fail when the target directory is not writable")
	}
	// restore 失败（只读目录不可写），回滚写回同一只读目录同样失败，因此
	// 错误必须明确报告"回滚不完整"，而不是宣称工作区已干净回滚。
	if !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("error must report an incomplete rollback, got: %v", err)
	}
}

// TestManager_Checkout_PreservesExecutableBit 验证：提交时记录的可执行位在
// checkout 恢复后仍然保留。
func TestManager_Checkout_PreservesExecutableBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no executable bit")
	}

	tmpDir, err := os.MkdirTemp("", "vcs-mode-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	script := filepath.Join(tmpDir, "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RecordOldState(script); err != nil {
		t.Fatal(err)
	}
	snap, err := mgr.CommitPending("mode", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// 破坏：改成非可执行并改内容后 checkout 回去。
	if err := os.Chmod(script, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Checkout(snap.ID); err != nil {
		t.Fatalf("Checkout failed: %v", err)
	}

	info, err := os.Stat(script)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("executable bit lost after checkout: mode=%v", info.Mode().Perm())
	}
}

// TestManager_Rollback_PreservesExecutableBit 验证：回滚一个已可执行脚本的
// 改动后，可执行位必须保留。store.Get 现在按对象记录的 exec 位恢复，而不再
// 固定写 0o644。
func TestManager_Rollback_PreservesExecutableBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no executable bit")
	}

	tmpDir, err := os.MkdirTemp("", "vcs-rollback-mode-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	script := filepath.Join(tmpDir, "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}

	// 记录写前状态后修改内容（保持可执行），再回滚。
	if err := mgr.RecordOldState(script); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("changed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RollbackPending(); err != nil {
		t.Fatalf("RollbackPending failed: %v", err)
	}

	info, err := os.Stat(script)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("executable bit lost after rollback: mode=%v", info.Mode().Perm())
	}
	data, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "#!/bin/sh\necho hi\n" {
		t.Fatalf("rollback restored wrong content: %q", string(data))
	}
}

// TestManager_CommitPending_RejectsInvalidExtraFiles 验证：extraFiles 中非法
// hash / 缺失对象 / 逃逸路径 / vcs 元数据路径都会被拒绝，不会写出不可恢复的
// HEAD。
func TestManager_CommitPending_RejectsInvalidExtraFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-extra-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	validHash, err := mgr.PutBlob([]byte("blob"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		files map[string]string
	}{
		{"malformed hash", map[string]string{"a.txt": "not-a-hash"}},
		{"missing object", map[string]string{"a.txt": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		{"path traversal", map[string]string{filepath.Join("..", "escape.txt"): validHash}},
		{"vcs metadata path", map[string]string{filepath.Join("vcs", "snapshots", "x.json"): validHash}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := mgr.CommitPending("bad", nil, tc.files); err == nil {
				t.Fatalf("CommitPending should reject %s", tc.name)
			}
		})
	}

	// 合法的 extra file 仍应成功。
	snap, err := mgr.CommitPending("good", nil, map[string]string{"a.txt": validHash})
	if err != nil {
		t.Fatalf("CommitPending with valid extra file failed: %v", err)
	}
	if snap == nil {
		t.Fatal("expected a snapshot")
	}
	files := snapFileTable(t, mgr, snap)
	if files["a.txt"] != validHash {
		t.Fatalf("extra file hash not recorded: %v", files)
	}
}

// TestManager_ListHistory_ReportsUnreadableHEAD 验证：HEAD 指向的快照不可读时，
// ListHistory 必须报错，而不是把空/部分历史当作完整真相。
func TestManager_ListHistory_ReportsUnreadableHEAD(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-broken-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	f := filepath.Join(tmpDir, "f.txt")
	if err := os.WriteFile(f, []byte("v0"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RecordOldState(f); err != nil {
		t.Fatal(err)
	}
	snap, err := mgr.CommitPending("c0", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// 删除 HEAD 指向的快照文件，制造 HEAD 不可读。
	if err := os.Remove(filepath.Join(tmpDir, "vcs", "snapshots", snap.ID+".json")); err != nil {
		t.Fatal(err)
	}

	history, err := mgr.ListHistory(10)
	if err == nil {
		t.Fatal("ListHistory must report an unreadable HEAD, not silently return nothing")
	}
	if len(history) != 0 {
		t.Fatalf("history = %v, want empty when HEAD is unreadable", idsOf(history))
	}
}

// TestManager_ListHistory_StopsCleanlyAtPrunedTail 验证：GC 修剪掉的尾部不属于
// 错误——链走到被修剪的祖先处应干净结束，而不是报"链断裂"。
func TestManager_ListHistory_StopsCleanlyAtPrunedTail(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vcs-pruned-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir, tmpDir)
	f := filepath.Join(tmpDir, "f.txt")
	for i := 0; i < 4; i++ {
		if err := os.WriteFile(f, []byte(fmt.Sprintf("v%d", i)), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := mgr.RecordOldState(f); err != nil {
			t.Fatal(err)
		}
		if _, err := mgr.CommitPending(fmt.Sprintf("c%d", i), nil, nil); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := mgr.GC(2); err != nil {
		t.Fatalf("GC failed: %v", err)
	}

	history, err := mgr.ListHistory(10)
	if err != nil {
		t.Fatalf("ListHistory after GC should stop cleanly at the pruned tail, got: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2 (retained window)", len(history))
	}
}

func idsOf(snaps []*Snapshot) []string {
	out := make([]string, 0, len(snaps))
	for _, s := range snaps {
		out = append(out, s.ID)
	}
	return out
}
