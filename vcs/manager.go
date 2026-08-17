package vcs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ojv/cog/store"
)

var (
	ErrInvalidPath      = errors.New("invalid path")
	ErrPathTraversal    = errors.New("path traversal detected")
	ErrSnapshotNotFound = errors.New("snapshot not found")
	ErrNoChanges        = errors.New("no changes to commit")
)

// checkpoint 是一次可独立回滚/合并的写前留档作用域(子代理级)。
// changes 的键是绝对路径,值是写前内容的对象 hash(空 hash 表示"回滚时删除")。
// 同一 checkpoint 内对同一路径多次记录只保留首次(该作用域内"最旧"状态)。
type checkpoint struct {
	changes map[string]string
}

// Manager versions a work tree using a content-addressed object store and a
// parent-linked snapshot chain. It is safe for concurrent use.
type Manager struct {
	baseDir        string
	workDir        string
	vcsRelPath     string
	store          *ObjectStore
	headFile       string
	mu             sync.RWMutex
	pendingChanges map[string]string      // 回合级(id="")留档:path -> oldHash
	checkpoints    map[string]*checkpoint // 子代理级留档:id -> checkpoint
}

// NewManager creates a version-control manager. stateDir is the metadata
// directory (e.g. ".jabo"); VCS data lives under its vcs/ subdirectory
// (objects/, snapshots/ and HEAD). workDir is the tree whose files are
// versioned. Checkout ignores the whole stateDir, so future non-vcs state
// files placed there (session memory, logs, etc.) are never treated as
// workspace content.
func NewManager(stateDir, workDir string) *Manager {
	vcsDir := filepath.Join(stateDir, "vcs")
	rel, _ := filepath.Rel(workDir, stateDir)
	if rel == "" || rel == "." {
		// stateDir == workDir: only the vcs/ subdirectory is metadata.
		rel = "vcs"
	}
	return &Manager{
		baseDir:        vcsDir,
		workDir:        workDir,
		vcsRelPath:     rel,
		store:          NewObjectStore(vcsDir),
		headFile:       filepath.Join(vcsDir, "HEAD"),
		pendingChanges: make(map[string]string),
		checkpoints:    make(map[string]*checkpoint),
	}
}

// Init creates the metadata directory layout if it does not exist.
func (v *Manager) Init() error {
	dirs := []string{
		filepath.Join(v.baseDir, "snapshots"),
		filepath.Join(v.baseDir, "objects"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create vcs directory %s: %w", dir, err)
		}
	}
	return nil
}

// GetStore returns the underlying object store.
func (v *Manager) GetStore() *ObjectStore {
	return v.store
}

// PutBlob stores arbitrary bytes in the object store and returns their content
// hash. It is the mechanism for versioning non-workspace state alongside the
// work tree (e.g. task memory), referenced from snapshot metadata via a hash.
func (v *Manager) PutBlob(data []byte) (string, error) {
	return v.store.PutData(data)
}

// GetBlob retrieves bytes previously stored via PutBlob by their content hash.
func (v *Manager) GetBlob(hash string) ([]byte, error) {
	return v.store.GetData(hash)
}

// isValidPath reports whether path (relative or absolute) resolves inside the
// work tree. It rejects empty paths and path traversal.
func (v *Manager) isValidPath(path string) error {
	if path == "" {
		return ErrInvalidPath
	}
	if v.workDir != "" {
		absPath := path
		if !filepath.IsAbs(path) {
			absPath = filepath.Join(v.workDir, path)
		}
		cleanPath := filepath.Clean(absPath)
		cleanWorkDir := filepath.Clean(v.workDir)
		if !strings.HasPrefix(cleanPath, cleanWorkDir+string(filepath.Separator)) && cleanPath != cleanWorkDir {
			return ErrPathTraversal
		}
	}
	return nil
}

// toRelPath converts an absolute path into a path relative to the work tree.
func (v *Manager) toRelPath(path string) string {
	if filepath.IsAbs(path) && v.workDir != "" {
		if rel, err := filepath.Rel(v.workDir, path); err == nil {
			return rel
		}
	}
	return path
}

// RecordOldState captures the current state of absPath before it is modified,
// so it can later be rolled back. For a file that does not exist yet, an empty
// hash is recorded (meaning "delete on rollback"). Recording the same path
// twice is a no-op.
func (v *Manager) RecordOldState(absPath string) error {
	return v.RecordOldStateIn("", absPath)
}

// BeginCheckpoint 开辟一个独立的子代理级留档作用域。id 必须是唯一、非空
// 的字符串(通常由调用方用节点/子代理标识生成)。重复 id 视为已存在,返回
// 错误以避免两个并发作用域互相污染。
func (v *Manager) BeginCheckpoint(id string) error {
	if id == "" {
		return fmt.Errorf("checkpoint id must not be empty")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, exists := v.checkpoints[id]; exists {
		return fmt.Errorf("checkpoint %q already exists", id)
	}
	v.checkpoints[id] = &checkpoint{changes: make(map[string]string)}
	return nil
}

// RecordOldStateIn 在指定 checkpoint(id 为空 = 回合级)里记录 absPath 的写前
// 状态。与 RecordOldState 相同:文件不存在则记空 hash(回滚时删除),同一
// 作用域内重复记录同一路径是 no-op(保留最旧)。
func (v *Manager) RecordOldStateIn(id, absPath string) error {
	if err := v.isValidPath(absPath); err != nil {
		return fmt.Errorf("invalid path %s: %w", absPath, err)
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	if id != "" {
		cp, ok := v.checkpoints[id]
		if !ok {
			return fmt.Errorf("checkpoint %q not found", id)
		}
		if _, exists := cp.changes[absPath]; exists {
			return nil
		}
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			cp.changes[absPath] = ""
			return nil
		}
		hash, err := v.store.Put(absPath)
		if err != nil {
			return fmt.Errorf("vcs store put failed: %w", err)
		}
		cp.changes[absPath] = hash
		return nil
	}

	if _, exists := v.pendingChanges[absPath]; exists {
		return nil
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		v.pendingChanges[absPath] = ""
		return nil
	}

	hash, err := v.store.Put(absPath)
	if err != nil {
		return fmt.Errorf("vcs store put failed: %w", err)
	}

	v.pendingChanges[absPath] = hash
	return nil
}

// RollbackPending restores every recorded path to its recorded old state and
// clears the pending set. Newly-created files (empty old hash) are removed.
// Any leftover sub-agent checkpoints are also rolled back defensively, so a
// turn-level rollback never strands a live checkpoint.
func (v *Manager) RollbackPending() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	var rollbackErrors []error

	// 先回滚残留的子代理 checkpoint(正常流程下应已 commit/rollback 干净)。
	for id := range v.checkpoints {
		if err := v.rollbackChangesLocked(v.checkpoints[id].changes); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("checkpoint %q: %w", id, err))
		}
		delete(v.checkpoints, id)
	}

	if err := v.rollbackChangesLocked(v.pendingChanges); err != nil {
		rollbackErrors = append(rollbackErrors, err)
	}

	if len(rollbackErrors) > 0 {
		return fmt.Errorf("rollback completed with %d errors, %d items remain pending: %v", len(rollbackErrors), len(v.pendingChanges), rollbackErrors)
	}
	return nil
}

// rollbackChangesLocked 把一组 {path: oldHash} 留档恢复到写前状态。调用方
// 必须已持有写锁。成功回滚的路径会被就地删除(清空),失败的路径保留在原
// map 里以便重试或上报。
func (v *Manager) rollbackChangesLocked(changes map[string]string) error {
	var rollbackErrors []error
	successPaths := make([]string, 0, len(changes))

	for absPath, oldHash := range changes {
		if oldHash == "" {
			if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("failed to remove %s: %w", absPath, err))
				continue
			}
			successPaths = append(successPaths, absPath)
			continue
		}

		if err := v.store.Get(oldHash, absPath); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("failed to restore %s from vcs: %w", absPath, err))
			continue
		}
		successPaths = append(successPaths, absPath)
	}

	for _, path := range successPaths {
		delete(changes, path)
	}

	if len(rollbackErrors) > 0 {
		return fmt.Errorf("rollback failed for %d items: %v", len(rollbackErrors), rollbackErrors)
	}
	return nil
}

// RollbackCheckpoint 回滚指定子代理 checkpoint 的全部改动(恢复到该子代理
// 写前的状态),并从作用域表里移除它。id 不存在返回错误。
func (v *Manager) RollbackCheckpoint(id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	cp, ok := v.checkpoints[id]
	if !ok {
		return fmt.Errorf("checkpoint %q not found", id)
	}
	delete(v.checkpoints, id)
	return v.rollbackChangesLocked(cp.changes)
}

// CommitCheckpoint 把指定子代理 checkpoint 的改动合并进回合级 pending,并
// 移除该作用域。合并遵循"保留最旧"不变量:回合级已存在的路径优先(它先于
// 子代理开始),仅当回合级尚未记录该路径时才采纳 checkpoint 里的旧状态。
// id 不存在返回错误。
func (v *Manager) CommitCheckpoint(id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	cp, ok := v.checkpoints[id]
	if !ok {
		return fmt.Errorf("checkpoint %q not found", id)
	}
	delete(v.checkpoints, id)

	for path, hash := range cp.changes {
		if _, exists := v.pendingChanges[path]; exists {
			continue
		}
		v.pendingChanges[path] = hash
	}
	return nil
}

// HasPendingChanges reports whether any paths are awaiting commit/rollback.
func (v *Manager) HasPendingChanges() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if len(v.pendingChanges) > 0 {
		return true
	}
	for _, cp := range v.checkpoints {
		if len(cp.changes) > 0 {
			return true
		}
	}
	return false
}

// ForgetPending drops the recorded old state for absPath without restoring it.
func (v *Manager) ForgetPending(absPath string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.pendingChanges, absPath)
}

// CommitPending creates a snapshot from the pending changes plus extraFiles,
// advances HEAD to it, and clears the pending set. It returns nil, nil when
// there is nothing to commit. extraFiles maps relative paths to object hashes
// for files that are not part of the work tree (e.g. virtual blobs).
func (v *Manager) CommitPending(message string, metadata map[string]string, extraFiles map[string]string) (*Snapshot, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	// 防御性合并残留 checkpoint:正常流程下子代理已在 runSubAgent 收口时
	// commit/rollback 干净,但若因 panic/异常路径残留,必须合并进回合级 pending,
	// 否则快照仍指向这些文件的旧 hash 而与磁盘不一致。
	for id, cp := range v.checkpoints {
		for path, hash := range cp.changes {
			if _, exists := v.pendingChanges[path]; !exists {
				v.pendingChanges[path] = hash
			}
		}
		delete(v.checkpoints, id)
	}

	if len(v.pendingChanges) == 0 && len(extraFiles) == 0 {
		return nil, nil
	}

	newFiles := make(map[string]string)

	// A HEAD read failure or corrupted base snapshot must be fatal: otherwise
	// the commit would silently drop all prior baseline files.
	headID, err := v.readHEAD()
	if err != nil {
		return nil, fmt.Errorf("failed to read HEAD: %w", err)
	}
	if headID != "" {
		headSnap, err := v.getSnapshot(headID)
		if err != nil {
			return nil, fmt.Errorf("failed to load parent snapshot %s: %w", headID, err)
		}
		for path, hash := range headSnap.Files {
			newFiles[v.toRelPath(path)] = hash
		}
	}

	for path, hash := range extraFiles {
		newFiles[v.toRelPath(path)] = hash
	}

	// Decide from the current on-disk state, not the recorded old hash: a file
	// may have been created or deleted since RecordOldState ran.
	for absPath := range v.pendingChanges {
		relPath := v.toRelPath(absPath)

		if _, err := os.Stat(absPath); err != nil {
			if os.IsNotExist(err) {
				delete(newFiles, relPath)
				continue
			}
			return nil, fmt.Errorf("failed to stat %s: %w", absPath, err)
		}

		hash, err := v.store.Put(absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to store %s: %w", absPath, err)
		}
		newFiles[relPath] = hash
	}

	snap, err := v.createSnapshot(headID, message, newFiles, metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to create commit snapshot: %w", err)
	}

	v.pendingChanges = make(map[string]string)

	return snap, nil
}

// createSnapshot marshals a snapshot to disk and advances HEAD, removing the
// snapshot file if the HEAD update fails.
func (v *Manager) createSnapshot(parentID string, message string, files map[string]string, metadata map[string]string) (*Snapshot, error) {
	snapshot := &Snapshot{
		ParentID:  parentID,
		Timestamp: time.Now(),
		Message:   message,
		Files:     files,
		Metadata:  metadata,
	}

	snapshot.ID = computeSnapshotID(snapshot)

	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	snapDir := filepath.Join(v.baseDir, "snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create snapshots directory: %w", err)
	}

	snapPath := filepath.Join(snapDir, snapshot.ID+".json")
	if err := store.AtomicWriteFile(snapPath, data, 0o644); err != nil {
		return nil, fmt.Errorf("failed to write snapshot file: %w", err)
	}

	if err := store.AtomicWriteFile(v.headFile, []byte(snapshot.ID), 0o644); err != nil {
		os.Remove(snapPath)
		return nil, fmt.Errorf("failed to update HEAD: %w", err)
	}

	return snapshot, nil
}

// GetSnapshot returns the snapshot with the given ID, verifying its content
// hash matches the ID and that its parent ID and file hashes are valid.
func (v *Manager) GetSnapshot(id string) (*Snapshot, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.getSnapshot(id)
}

func (v *Manager) getSnapshot(id string) (*Snapshot, error) {
	if err := isValidHash(id); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSnapshotNotFound, id)
	}

	snapPath := filepath.Join(v.baseDir, "snapshots", id+".json")
	data, err := os.ReadFile(snapPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrSnapshotNotFound, id)
		}
		return nil, fmt.Errorf("failed to read snapshot %s: %w", id, err)
	}

	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("failed to parse snapshot %s: %w", id, err)
	}
	if snap.ID != "" && snap.ID != id {
		return nil, fmt.Errorf("snapshot payload ID mismatch: file requested %s but payload contains %s", id, snap.ID)
	}
	computedID := computeSnapshotID(&snap)
	if computedID != id {
		return nil, fmt.Errorf("snapshot payload content mismatch: file requested %s but payload computes to %s", id, computedID)
	}
	snap.ID = id
	if snap.ParentID != "" {
		if err := isValidHash(snap.ParentID); err != nil {
			return nil, fmt.Errorf("snapshot %s has invalid parent ID %q: %w", id, snap.ParentID, err)
		}
	}
	for path, hash := range snap.Files {
		if err := isValidHash(hash); err != nil {
			return nil, fmt.Errorf("snapshot %s contains invalid hash for file %s: %w", id, path, err)
		}
	}
	return &snap, nil
}

// GetHEAD returns the ID of the current HEAD snapshot ("" when none exists).
func (v *Manager) GetHEAD() (string, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.readHEAD()
}

func (v *Manager) readHEAD() (string, error) {
	data, err := os.ReadFile(v.headFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read HEAD: %w", err)
	}
	// Trim whitespace: a user/editor may append a trailing newline to HEAD.
	return strings.TrimSpace(string(data)), nil
}

// isIgnoredPath reports whether relPath falls inside the vcs metadata directory
// itself, so that checkout never treats metadata files as workspace content.
func (v *Manager) isIgnoredPath(relPath string) bool {
	cleaned := filepath.ToSlash(filepath.Clean(relPath))
	vcsRel := filepath.ToSlash(filepath.Clean(v.vcsRelPath))
	if cleaned == vcsRel {
		return true
	}
	return strings.HasPrefix(cleaned, vcsRel+"/")
}

// Checkout restores the work tree to the snapshot identified by id. It is
// transactional: a pre-check verifies all required objects exist; a backup is
// taken of affected files; and any restore or HEAD-update failure rolls the
// work tree back. On success the pending-change set is cleared.
func (v *Manager) Checkout(id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	snap, err := v.getSnapshot(id)
	if err != nil {
		return err
	}

	currentFiles := make(map[string]bool)
	if v.workDir != "" {
		err := filepath.WalkDir(v.workDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			relPath, err := filepath.Rel(v.workDir, path)
			if err != nil {
				return nil
			}
			if v.isIgnoredPath(relPath) {
				return nil
			}
			currentFiles[relPath] = true
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to scan workdir: %w", err)
		}
	}

	type restoreOp struct {
		absPath string
		relPath string
		hash    string
	}
	var ops []restoreOp
	for path, hash := range snap.Files {
		relPath := v.toRelPath(path)
		absPath := path
		if !filepath.IsAbs(path) && v.workDir != "" {
			absPath = filepath.Join(v.workDir, path)
		}
		// The resolved path must stay inside the work tree: this blocks path
		// traversal from tampered snapshots, and skips virtual files (extra
		// files with absolute paths outside workDir).
		if v.workDir != "" {
			cleanAbs := filepath.Clean(absPath)
			cleanWork := filepath.Clean(v.workDir)
			if !strings.HasPrefix(cleanAbs, cleanWork+string(filepath.Separator)) && cleanAbs != cleanWork {
				continue
			}
		}
		ops = append(ops, restoreOp{absPath: absPath, relPath: relPath, hash: hash})
		delete(currentFiles, relPath)
	}

	var preCheckErrors []error
	for _, op := range ops {
		if err := isValidHash(op.hash); err != nil {
			preCheckErrors = append(preCheckErrors, fmt.Errorf("invalid hash for file %s: %w", op.relPath, err))
			continue
		}
		if !v.store.Exists(op.hash) {
			preCheckErrors = append(preCheckErrors, fmt.Errorf("missing object %s for file %s", op.hash[:12], op.relPath))
		}
	}
	for relPath := range currentFiles {
		absPath := filepath.Join(v.workDir, relPath)
		if _, err := os.Stat(absPath); err != nil && !os.IsNotExist(err) {
			preCheckErrors = append(preCheckErrors, fmt.Errorf("cannot access file %s: %w", relPath, err))
		}
	}
	if len(preCheckErrors) > 0 {
		return fmt.Errorf("checkout pre-check failed (%d errors), workspace untouched: %v", len(preCheckErrors), preCheckErrors)
	}

	backup := make(map[string][]byte)
	restoreTargetExisted := make(map[string]bool)

	for relPath := range currentFiles {
		absPath := filepath.Join(v.workDir, relPath)
		data, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		backup[relPath] = data
	}

	for _, op := range ops {
		if _, err := os.Stat(op.absPath); err == nil {
			restoreTargetExisted[op.relPath] = true
			data, err := os.ReadFile(op.absPath)
			if err == nil {
				backup[op.relPath] = data
			}
		}
	}

	var restoreErrors []error
	for _, op := range ops {
		if err := v.store.Get(op.hash, op.absPath); err != nil {
			restoreErrors = append(restoreErrors, fmt.Errorf("failed to restore %s: %w", op.relPath, err))
		}
	}

	for relPath := range currentFiles {
		absPath := filepath.Join(v.workDir, relPath)
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			restoreErrors = append(restoreErrors, fmt.Errorf("failed to remove extra file %s: %w", relPath, err))
		}
	}

	rollbackWorkspace := func() {
		for _, op := range ops {
			content, hasBackup := backup[op.relPath]
			if hasBackup {
				_ = os.WriteFile(op.absPath, content, 0o644)
				continue
			}
			if !restoreTargetExisted[op.relPath] {
				_ = os.Remove(op.absPath)
			}
		}
		for relPath, content := range backup {
			if restoreTargetExisted[relPath] {
				continue
			}
			absPath := filepath.Join(v.workDir, relPath)
			_ = os.WriteFile(absPath, content, 0o644)
		}
	}

	if len(restoreErrors) > 0 {
		rollbackWorkspace()
		return fmt.Errorf("checkout failed with %d errors, workspace rolled back: %v", len(restoreErrors), restoreErrors)
	}

	if err := store.AtomicWriteFile(v.headFile, []byte(id), 0o644); err != nil {
		rollbackWorkspace()
		return fmt.Errorf("checkout updated workspace but failed to update HEAD; workspace rolled back: %w", err)
	}

	v.pendingChanges = make(map[string]string)
	v.checkpoints = make(map[string]*checkpoint)
	return nil
}

// ListHistory returns up to limit snapshots walking backward from HEAD.
func (v *Manager) ListHistory(limit int) ([]*Snapshot, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	headID, err := v.readHEAD()
	if err != nil {
		return nil, err
	}
	if headID == "" {
		return nil, nil
	}

	var history []*Snapshot
	currentID := headID
	for currentID != "" && len(history) < limit {
		snap, err := v.getSnapshot(currentID)
		if err != nil {
			break
		}
		history = append(history, snap)
		currentID = snap.ParentID
	}
	return history, nil
}

// DiffSnapshots compares oldID and newID and returns per-path changes. An empty
// oldID is treated as the empty state (all files "added").
func (v *Manager) DiffSnapshots(oldID, newID string) (map[string]FileDiff, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	var oldFiles map[string]string
	if oldID != "" {
		oldSnap, err := v.getSnapshot(oldID)
		if err != nil {
			return nil, err
		}
		oldFiles = oldSnap.Files
	} else {
		oldFiles = make(map[string]string)
	}

	newSnap, err := v.getSnapshot(newID)
	if err != nil {
		return nil, err
	}
	newFiles := newSnap.Files

	diffs := make(map[string]FileDiff)

	for path, oldHash := range oldFiles {
		newHash, exists := newFiles[path]
		if !exists {
			diffs[path] = FileDiff{Status: DiffDeleted, OldHash: oldHash}
		} else if oldHash != newHash {
			diffs[path] = FileDiff{Status: DiffModified, OldHash: oldHash, NewHash: newHash}
		}
	}

	for path, newHash := range newFiles {
		if _, exists := oldFiles[path]; !exists {
			diffs[path] = FileDiff{Status: DiffAdded, NewHash: newHash}
		}
	}

	return diffs, nil
}

// GC prunes history to the most recent keepLast snapshots and garbage-collects
// unreferenced objects. It refuses to run if the snapshot chain is broken or
// HEAD points to a missing snapshot, so it can never delete all history as a
// result of inconsistent state. It returns the number of items deleted.
func (v *Manager) GC(keepLast int) (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	snapDir := filepath.Join(v.baseDir, "snapshots")
	entries, err := os.ReadDir(snapDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read snapshots directory: %w", err)
	}

	allSnaps := make(map[string]bool)
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			id := strings.TrimSuffix(e.Name(), ".json")
			// Only valid snapshot IDs count, so non-vcs .json files are safe.
			if isValidHash(id) != nil {
				continue
			}
			allSnaps[id] = true
		}
	}

	keepCount := keepLast
	if keepCount < 1 {
		keepCount = 1
	}

	keepSet := make(map[string]bool)
	headID, err := v.readHEAD()
	if err != nil {
		return 0, fmt.Errorf("failed to read HEAD: %w", err)
	}
	if headID != "" {
		if err := isValidHash(headID); err != nil {
			return 0, fmt.Errorf("HEAD contains invalid snapshot ID %q: %w", headID, err)
		}
	}
	currentID := headID
	for currentID != "" && len(keepSet) < keepCount {
		if !allSnaps[currentID] {
			return 0, fmt.Errorf("snapshot chain broken at %s: snapshot file missing; refusing to GC", currentID[:12])
		}
		keepSet[currentID] = true
		snap, err := v.getSnapshot(currentID)
		if err != nil {
			return 0, fmt.Errorf("snapshot chain broken at %s: %w; refusing to GC", currentID[:12], err)
		}
		currentID = snap.ParentID
	}

	// Fail-safe: a non-empty HEAD that resolves to no keepable snapshot means
	// HEAD points at a missing snapshot; deleting everything would be unsafe.
	if headID != "" && len(keepSet) == 0 && len(allSnaps) > 0 {
		return 0, fmt.Errorf("HEAD %s points to a missing snapshot; refusing to GC %d snapshots (state inconsistent)", headID[:12], len(allSnaps))
	}

	deletedSnapshots := 0
	for id := range allSnaps {
		if !keepSet[id] {
			if err := os.Remove(filepath.Join(snapDir, id+".json")); err == nil {
				deletedSnapshots++
			}
		}
	}

	keepHashes := make(map[string]bool)

	// Preserve objects referenced by pending changes (write lock held).
	for _, hash := range v.pendingChanges {
		if hash != "" {
			keepHashes[hash] = true
		}
	}
	for _, cp := range v.checkpoints {
		for _, hash := range cp.changes {
			if hash != "" {
				keepHashes[hash] = true
			}
		}
	}

	for id := range keepSet {
		snap, err := v.getSnapshot(id)
		if err == nil {
			for _, hash := range snap.Files {
				keepHashes[hash] = true
			}
			// 保护快照 metadata 里引用的对象 hash(如 memory_hash)。这样把
			// 任务记忆等非工作树状态以 blob 形式版本化时,GC 不会误删它们。
			for _, hash := range snap.Metadata {
				if isValidHash(hash) == nil {
					keepHashes[hash] = true
				}
			}
		}
	}

	deletedObjects, err := v.store.PruneObjects(keepHashes, 1*time.Hour)
	if err != nil {
		return deletedSnapshots, fmt.Errorf("prune objects failed: %w", err)
	}

	return deletedSnapshots + deletedObjects, nil
}
