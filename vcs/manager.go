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
	ErrSymlinkEscape    = errors.New("path escapes work tree via symlink")
	ErrSnapshotNotFound = errors.New("snapshot not found")
	ErrNoChanges        = errors.New("no changes to commit")
)

// checkpoint 是一次可独立回滚/合并的写前留档作用域(子代理级)。
// changes 的键是绝对路径,值是写前内容的对象 hash(空 hash 表示"回滚时删除")。
// 同一 checkpoint 内对同一路径多次记录只保留首次(该作用域内"最旧"状态)。
type checkpoint struct {
	changes map[string]string
}

// virtualFilesMetaKey is the reserved Metadata key under which virtual files
// (extra files with absolute paths outside the work tree: task memory and other
// caller blobs) are recorded. They are not workspace content, so they never
// enter the snapshot tree; this key lets a snapshot still expose them through
// the flat file view. Callers must not use it themselves.
const virtualFilesMetaKey = "vcs_virtual_files"

// checkoutRestoreChunkBytes bounds how much restored content Checkout buffers
// before flushing a batch write. It trades a few extra directory fsyncs for a
// flat memory ceiling: without it, restoring a large tree would hold every
// file's content in memory at once.
const checkoutRestoreChunkBytes = 32 << 20 // 32 MiB

// Manager versions a work tree using a content-addressed object store and a
// parent-linked snapshot chain.
//
// Concurrency: within one Manager instance all operations are serialized by an
// internal RWMutex. State-mutating operations (CommitPending, Checkout, GC)
// additionally take a cross-process exclusive lock file in the state directory,
// so multiple Manager instances — in the same process or in different
// processes — may safely share one stateDir: the read-HEAD / write-HEAD
// sequence is atomic, so concurrent commits chain onto each other instead of
// orphaning snapshots. Reads (GetSnapshot, GetHEAD, DiffSnapshots) are
// lock-free with respect to that file and only coordinate through the
// in-process mutex; ListHistory takes the same lock so it never observes a
// half-advanced chain.
//
// Scope of the shared-stateDir guarantee: the lock protects on-disk state
// (HEAD, snapshots, objects). The in-memory pending-changes/checkpoint sets are
// per-instance and are NOT coordinated across instances — each Manager commits
// only the paths it recorded via RecordOldState. Concurrent writers of the same
// path must therefore either share one Manager or use separate work trees.
//
// The cross-process lock is advisory and only coordinates code that goes
// through a Manager; a lock whose owner process has died is reclaimed after a
// stale timeout so a crash cannot wedge the state directory.
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
// work tree. It rejects empty paths, lexical path traversal, and paths that
// escape the work tree through a symlinked ancestor.
//
// The lexical check is not sufficient on its own: a directory inside the work
// tree may be a symlink pointing outside it. A path like "link/out.txt" is
// lexically contained but resolves to a file beyond the boundary, so checkout
// and rollback would silently write outside the work tree. To close that, the
// deepest already-existing ancestor of the resolved path is evaluated with
// EvalSymlinks and re-checked for containment.
func (v *Manager) isValidPath(path string) error {
	if path == "" {
		return ErrInvalidPath
	}
	if v.workDir == "" {
		return nil
	}

	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(v.workDir, path)
	}
	cleanPath := filepath.Clean(absPath)
	cleanWorkDir := filepath.Clean(v.workDir)
	if !strings.HasPrefix(cleanPath, cleanWorkDir+string(filepath.Separator)) && cleanPath != cleanWorkDir {
		return ErrPathTraversal
	}

	return v.checkNoSymlinkEscape(cleanPath, cleanWorkDir)
}

// checkNoSymlinkEscape verifies that the deepest existing ancestor of
// cleanPath resolves to a location still inside cleanWorkDir. Paths that do
// not exist yet are validated against their nearest existing ancestor, so
// "new/dir/file.txt" is checked at "new" (or higher) rather than failing.
// cleanWorkDir is itself resolved so that a symlinked workDir root (e.g.
// /tmp on macOS) does not produce false positives.
func (v *Manager) checkNoSymlinkEscape(cleanPath, cleanWorkDir string) error {
	resolvedWork, err := filepath.EvalSymlinks(cleanWorkDir)
	if err != nil {
		// Work tree root itself is unresolvable; fall back to the lexical
		// containment already established by the caller.
		resolvedWork = cleanWorkDir
	}

	ancestor := cleanPath
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			break
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			break
		}
		ancestor = parent
	}

	resolvedAncestor, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		// The ancestor exists but cannot be resolved (permissions, races).
		// Fail closed rather than risk writing outside the work tree.
		return fmt.Errorf("%w: cannot resolve %s", ErrSymlinkEscape, ancestor)
	}

	if resolvedAncestor != resolvedWork &&
		!strings.HasPrefix(resolvedAncestor, resolvedWork+string(filepath.Separator)) {
		return fmt.Errorf("%w: %s resolves to %s", ErrSymlinkEscape, cleanPath, resolvedAncestor)
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

// isRootedPath reports whether path is rooted: absolute, or starting with a
// separator. A leading separator is rooted even on Windows (where such a path
// has no drive but still means "from the root of the current drive"), so this
// is stricter than filepath.IsAbs and treats "/x" as rooted on every platform.
func isRootedPath(path string) bool {
	return filepath.IsAbs(path) || strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`)
}

// isVirtualExtraPath reports whether an extra file is a virtual file rather
// than a work-tree path. Virtual files are the caller's own blobs (task
// memory, etc.): they are recorded for diffing but must never be treated as
// restorable workspace content. A path is virtual when it is rooted and does
// not resolve inside the work tree; relPath is the pre-computed toRelPath
// result.
func (v *Manager) isVirtualExtraPath(path, relPath string) bool {
	if v.workDir == "" || !isRootedPath(path) {
		return false
	}
	if relPath == path {
		// Different volume or otherwise unresolvable relative to workDir.
		return true
	}
	return relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator))
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
		hash, _, err := v.store.Put(absPath)
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

	hash, _, err := v.store.Put(absPath)
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
		// Re-validate at write time: a symlink may have appeared since the
		// path was recorded, and rollback writes directly to disk. Without
		// this, a late symlink swap could redirect the restore outside the
		// work tree. Fail closed and keep the entry pending.
		if err := v.isValidPath(absPath); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("refusing to restore %s: %w", absPath, err))
			continue
		}

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
	return v.commitPendingLocked(message, metadata, extraFiles)
}

// commitPendingLocked performs the commit while v.mu is held. The cross-process
// state-dir lock is taken inside so that the read-HEAD / write-HEAD sequence is
// atomic across Managers and processes sharing this state directory: without
// it, two concurrent commits read the same parent, both write HEAD, and the
// first becomes an orphaned snapshot no longer reachable from HEAD.
func (v *Manager) commitPendingLocked(message string, metadata map[string]string, extraFiles map[string]string) (*Snapshot, error) {
	var snap *Snapshot
	err := v.withStateLock(func() error {
		s, err := v.commitPendingUnderLock(message, metadata, extraFiles)
		if err != nil {
			return err
		}
		snap = s
		return nil
	})
	if err != nil {
		return nil, err
	}
	return snap, nil
}

func (v *Manager) commitPendingUnderLock(message string, metadata map[string]string, extraFiles map[string]string) (*Snapshot, error) {

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

	// Seed the builder from the parent's root tree, lazily: only directories
	// along a changed path are read. Unchanged subtrees keep their hash and are
	// neither re-read nor re-written, so commit cost is O(changes), not O(tree).
	headID, err := v.readHEAD()
	if err != nil {
		return nil, fmt.Errorf("failed to read HEAD: %w", err)
	}

	builder := newTreeBuilder()
	parentVirtual := map[string]string{}
	if headID != "" {
		headSnap, err := v.getSnapshot(headID)
		if err != nil {
			return nil, fmt.Errorf("failed to load parent snapshot %s: %w", headID, err)
		}
		builder = newBuilderFromTree(v.store, headSnap.Tree)
		if raw := headSnap.Metadata[virtualFilesMetaKey]; raw != "" {
			_ = json.Unmarshal([]byte(raw), &parentVirtual)
		}
	}

	// extraFiles: caller-supplied entries. Validate before the snapshot exists.
	// Virtual files (absolute paths outside the work tree) are metadata, not
	// workspace content: they live in Metadata under a reserved key so they
	// never enter the tree, where a leading-separator path has no segment form.
	virtualFiles := map[string]string{}
	for path, hash := range extraFiles {
		// Never trust caller-supplied hashes: a hash that is malformed, whose
		// object is absent, or that is claimed for a workspace path escaping the
		// work tree would produce a HEAD that diffs against content nothing can
		// ever restore. Validate before the snapshot is created.
		if err := isValidHash(hash); err != nil {
			return nil, fmt.Errorf("extra file %s: invalid content hash %q: %w", path, hash, err)
		}
		if !v.store.Exists(hash) {
			return nil, fmt.Errorf("extra file %s: object %s not found in store", path, hash[:12])
		}
		relPath := v.toRelPath(path)
		if v.isVirtualExtraPath(path, relPath) {
			// A path that stays outside the work tree is a virtual file: the
			// caller's own blob (e.g. task memory) recorded by its original
			// path. It is metadata, not workspace content, so Checkout never
			// restores it and no work-tree containment rule applies to it.
			virtualFiles[path] = hash
			continue
		}
		if err := v.isValidPath(relPath); err != nil {
			return nil, fmt.Errorf("extra file %s: %w", path, err)
		}
		if v.isIgnoredPath(relPath) {
			return nil, fmt.Errorf("extra file %s: paths inside the vcs metadata directory are not versionable", path)
		}
		exec, err := v.store.objectExec(hash)
		if err != nil {
			return nil, fmt.Errorf("extra file %s: %w", path, err)
		}
		if err := builder.add(relPath, hash, exec); err != nil {
			return nil, fmt.Errorf("extra file %s: %w", path, err)
		}
	}

	// Decide from the current on-disk state, not the recorded old hash: a file
	// may have been created or deleted since RecordOldState ran.
	for absPath := range v.pendingChanges {
		relPath := v.toRelPath(absPath)

		if _, err := os.Stat(absPath); err != nil {
			if os.IsNotExist(err) {
				if err := builder.remove(relPath); err != nil {
					return nil, fmt.Errorf("failed to record deletion of %s: %w", relPath, err)
				}
				continue
			}
			return nil, fmt.Errorf("failed to stat %s: %w", absPath, err)
		}

		hash, exec, err := v.store.Put(absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to store %s: %w", absPath, err)
		}
		if err := builder.add(relPath, hash, exec); err != nil {
			return nil, fmt.Errorf("failed to record %s: %w", relPath, err)
		}
	}

	rootTree, err := builder.build(v.store)
	if err != nil {
		return nil, fmt.Errorf("failed to build snapshot tree: %w", err)
	}

	// Carry virtual files forward from the parent so they behave like any other
	// versioned entry: present until explicitly removed.
	mergedMeta := metadata
	if len(virtualFiles) > 0 || len(parentVirtual) > 0 {
		combined := make(map[string]string, len(parentVirtual)+len(virtualFiles))
		for p, h := range parentVirtual {
			combined[p] = h
		}
		for p, h := range virtualFiles {
			combined[p] = h
		}
		mergedMeta = mergeVirtualFiles(metadata, combined)
	}

	snap, err := v.createSnapshot(headID, message, rootTree, mergedMeta)
	if err != nil {
		return nil, fmt.Errorf("failed to create commit snapshot: %w", err)
	}

	v.pendingChanges = make(map[string]string)

	return snap, nil
}

// mergeVirtualFiles returns metadata with the caller's virtual files merged
// into the reserved key, without mutating the caller's map.
func mergeVirtualFiles(metadata, incoming map[string]string) map[string]string {
	merged := make(map[string]string, len(metadata)+1)
	for k, v := range metadata {
		merged[k] = v
	}
	encoded, _ := json.Marshal(incoming)
	merged[virtualFilesMetaKey] = string(encoded)
	return merged
}

// createSnapshot marshals a snapshot to disk and advances HEAD, removing the
// snapshot file if the HEAD update fails.
func (v *Manager) createSnapshot(parentID string, message string, rootTree string, metadata map[string]string) (*Snapshot, error) {
	snapshot := &Snapshot{
		ParentID:  parentID,
		Timestamp: time.Now(),
		Message:   message,
		Tree:      rootTree,
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
	if snap.Tree != "" {
		if err := isValidHash(snap.Tree); err != nil {
			return nil, fmt.Errorf("snapshot %s has invalid tree hash %q: %w", id, snap.Tree, err)
		}
	}
	return &snap, nil
}

// snapshotFiles returns the expanded path -> {hash, exec} table for a snapshot,
// reading its tree objects. The result is cached on the snapshot so repeated
// walks (Diff, Checkout planning) do not re-read the tree. A missing or corrupt
// tree is an error, never a silent partial view.
func (v *Manager) snapshotFiles(snap *Snapshot) (map[string]flatFile, error) {
	if snap.files != nil {
		return snap.files, nil
	}
	files := map[string]flatFile{}
	if snap.Tree != "" {
		expanded, trees, err := expandTree(v.store, snap.Tree, v.store.getObjectBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to expand tree for snapshot %s: %w", snap.ID, err)
		}
		files = expanded
		snap.trees = trees
	}
	// Merge back virtual files recorded in metadata; they are not tree entries.
	if raw := snap.Metadata[virtualFilesMetaKey]; raw != "" {
		var virtualFiles map[string]string
		if err := json.Unmarshal([]byte(raw), &virtualFiles); err == nil {
			for p, h := range virtualFiles {
				files[p] = flatFile{hash: h}
			}
		}
	}
	snap.files = files
	return files, nil
}

// execMode maps a recorded executable flag to the file mode used when restoring
// a work-tree file. Only executability is versioned: an object's own permission
// bits reflect whatever the platform gave the object file (0o444 read-only on
// some file systems, 0o666 on Windows) and reproducing them literally would
// make restored files read-only.
func execMode(exec bool) fs.FileMode {
	if exec {
		return 0o755
	}
	return 0o644
}

// writeFileAtomic writes content to path via a temp file + rename, applying
// mode. It is used for restoring work-tree files so a partially-written file is
// never observed, and so the executable bit recorded at commit time survives
// checkout.
func (v *Manager) writeFileAtomic(path string, content []byte, mode fs.FileMode) error {
	return store.AtomicWriteFile(path, content, mode)
}

// newBackupDir creates a fresh, empty directory for a checkout's file backup
// under the VCS state directory (thereby invisible to the work-tree scan).
func (v *Manager) newBackupDir() (string, error) {
	tmpRoot := filepath.Join(v.baseDir, "tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		return "", fmt.Errorf("failed to create backup root %s: %w", tmpRoot, err)
	}
	dir, err := os.MkdirTemp(tmpRoot, "checkout-*")
	if err != nil {
		return "", fmt.Errorf("failed to create backup dir: %w", err)
	}
	return dir, nil
}

// copyToBackup copies the file at absPath into backupDir, keyed by relPath.
// The parent directories are created as needed.
func (v *Manager) copyToBackup(backupDir, relPath, absPath string) error {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", relPath, err)
	}
	dst := filepath.Join(backupDir, relPath)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("failed to create backup dir for %s: %w", relPath, err)
	}
	return store.WriteFileSync(dst, data, 0o644)
}

// restoreFromBackup copies a backup entry back to absPath with the recorded
// permission bits. It is the rollback counterpart of copyToBackup and uses the
// same atomic write path as a normal checkout write.
//
// A path recorded as an existing restore target may have no backup entry (for
// example the target was a directory, which is not backed up). That is not an
// error: the pre-checkout state is already in place, so there is nothing to
// restore.
func (v *Manager) restoreFromBackup(backupDir, relPath, absPath string, mode fs.FileMode) error {
	data, err := os.ReadFile(filepath.Join(backupDir, relPath))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read backup of %s: %w", relPath, err)
	}
	return v.writeFileAtomic(absPath, data, mode)
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
		exec    bool
	}
	snapFiles, err := v.snapshotFiles(snap)
	if err != nil {
		return err
	}
	var ops []restoreOp
	for path, f := range snapFiles {
		// Tree keys are always slash-separated (they are content-address
		// inputs). Convert to the platform's relative form so they compare
		// equal to the walk's relative paths — otherwise the same file appears
		// once as "sub/f.txt" and once as "sub\f.txt" on Windows, and checkout
		// would delete what it just restored.
		relPath := filepath.FromSlash(path)
		absPath := path
		if v.workDir != "" && !isRootedPath(path) {
			absPath = filepath.Join(v.workDir, relPath)
		}
		// The resolved path must stay inside the work tree: this blocks path
		// traversal from tampered snapshots and symlink escapes, and skips
		// virtual files (extra files with rooted paths outside workDir).
		// It uses the same authority as record-time validation so the two can
		// never drift.
		if v.workDir != "" {
			if err := v.isValidPath(absPath); err != nil {
				continue
			}
		}
		ops = append(ops, restoreOp{absPath: absPath, relPath: relPath, hash: f.hash, exec: f.exec})
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

	// Backup the affected files to a temporary directory instead of holding
	// them in memory: a checkout of a large work tree would otherwise buffer
	// the entire old content (every extra file plus every restore target) in
	// RAM, and a multi-GiB workspace could exhaust it before a single byte is
	// restored. The temp dir lives under the VCS state directory, so it is
	// ignored by the work-tree scan and never mistaken for workspace content,
	// and it is removed unconditionally on every exit path.
	backupDir, err := v.newBackupDir()
	if err != nil {
		return err
	}
	defer os.RemoveAll(backupDir)

	// backupModes records the permission to restore for every path that
	// existed before this checkout; restoreTargetExisted records which backup
	// entries the restore step will overwrite (as opposed to extra files,
	// which are deleted and only need restoring on rollback).
	backupModes := make(map[string]fs.FileMode)
	restoreTargetExisted := make(map[string]bool)

	// backupOne copies absPath under the backup dir, keyed by relPath. Missing
	// files are recorded as "did not exist" (skip), not as an error: the set
	// of files can change between the scan and here.
	backupOne := func(relPath, absPath string) error {
		info, err := os.Stat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("failed to stat %s: %w", relPath, err)
		}
		if info.IsDir() {
			return nil
		}
		backupModes[relPath] = info.Mode().Perm()
		return v.copyToBackup(backupDir, relPath, absPath)
	}

	// Fail closed: a backup that cannot be taken means a failure during
	// restore would be unrecoverable, so abort before mutating the workspace.
	for relPath := range currentFiles {
		if err := backupOne(relPath, filepath.Join(v.workDir, relPath)); err != nil {
			return fmt.Errorf("checkout backup failed, workspace untouched: %w", err)
		}
	}
	for _, op := range ops {
		if _, err := os.Stat(op.absPath); err == nil {
			restoreTargetExisted[op.relPath] = true
		}
		if err := backupOne(op.relPath, op.absPath); err != nil {
			return fmt.Errorf("checkout backup failed, workspace untouched: %w", err)
		}
	}

	// Restore content and the recorded executable bit: a snapshot that loses a
	// script's +x produces a workspace that no longer runs. Writes go through
	// store.AtomicWriteFileBatch in byte-budgeted chunks so each affected
	// directory is fsynced once per chunk instead of once per file, while peak
	// memory stays bounded by the budget rather than by the total restored
	// size: buffering every file's content at once would hold the whole work
	// tree in RAM. Content is still read (and hash-verified) per object.
	var restoreErrors []error
	var chunk []store.BatchFile
	chunkBytes := 0
	flush := func() {
		if len(chunk) == 0 {
			return
		}
		if err := store.AtomicWriteFileBatch(chunk); err != nil {
			restoreErrors = append(restoreErrors, fmt.Errorf("failed to write restored files: %w", err))
		}
		chunk = chunk[:0]
		chunkBytes = 0
	}
	for _, op := range ops {
		data, err := v.store.GetData(op.hash)
		if err != nil {
			restoreErrors = append(restoreErrors, fmt.Errorf("failed to restore %s: %w", op.relPath, err))
			continue
		}
		if chunkBytes > 0 && chunkBytes+len(data) > checkoutRestoreChunkBytes {
			flush()
		}
		chunk = append(chunk, store.BatchFile{
			Path: op.absPath,
			Data: data,
			Perm: execMode(op.exec),
		})
		chunkBytes += len(data)
	}
	flush()

	for relPath := range currentFiles {
		absPath := filepath.Join(v.workDir, relPath)
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			restoreErrors = append(restoreErrors, fmt.Errorf("failed to remove extra file %s: %w", relPath, err))
		}
	}

	// rollbackWorkspace undoes the partial checkout, returning the errors it
	// hit. Reporting these is not optional: a silent failure here leaves the
	// work tree in a half-restored state while the caller is told it was
	// rolled back.
	//
	// Restoring a path means copying it back from backupDir; a path that was
	// recorded as a restore target but is absent from the backup did not exist
	// before, so rollback removes it instead.
	rollbackWorkspace := func() []error {
		var rollbackErrors []error
		for _, op := range ops {
			if restoreTargetExisted[op.relPath] {
				if err := v.restoreFromBackup(backupDir, op.relPath, op.absPath, backupModes[op.relPath]); err != nil {
					rollbackErrors = append(rollbackErrors, fmt.Errorf("failed to restore %s: %w", op.relPath, err))
				}
				continue
			}
			if err := os.Remove(op.absPath); err != nil && !os.IsNotExist(err) {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("failed to remove %s: %w", op.relPath, err))
			}
		}
		for relPath := range backupModes {
			if restoreTargetExisted[relPath] {
				continue // already handled above as a restore target
			}
			absPath := filepath.Join(v.workDir, relPath)
			if err := v.restoreFromBackup(backupDir, relPath, absPath, backupModes[relPath]); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("failed to restore %s: %w", relPath, err))
			}
		}
		return rollbackErrors
	}

	if len(restoreErrors) > 0 {
		if rbErrors := rollbackWorkspace(); len(rbErrors) > 0 {
			return fmt.Errorf("checkout failed with %d errors and rollback was incomplete (%d errors); workspace may be partially restored: %v; rollback errors: %v",
				len(restoreErrors), len(rbErrors), restoreErrors, rbErrors)
		}
		return fmt.Errorf("checkout failed with %d errors, workspace rolled back: %v", len(restoreErrors), restoreErrors)
	}

	if err := store.AtomicWriteFile(v.headFile, []byte(id), 0o644); err != nil {
		if rbErrors := rollbackWorkspace(); len(rbErrors) > 0 {
			return fmt.Errorf("checkout updated workspace but failed to update HEAD and rollback was incomplete (%d errors); workspace may be partially restored: %w; rollback errors: %v",
				len(rbErrors), err, rbErrors)
		}
		return fmt.Errorf("checkout updated workspace but failed to update HEAD; workspace rolled back: %w", err)
	}

	v.pendingChanges = make(map[string]string)
	v.checkpoints = make(map[string]*checkpoint)
	return nil
}

// ListHistory returns up to limit snapshots walking backward from HEAD.
//
// HEAD itself is always expected to resolve: if it does not, that is genuine
// corruption and is reported as an error (with any prefix collected) rather
// than silently presenting an empty or partial history. An ancestor that
// cannot be read ends the walk cleanly: the tail beyond it was pruned by GC,
// which is the normal retention boundary and not an error.
func (v *Manager) ListHistory(limit int) ([]*Snapshot, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	var history []*Snapshot
	var chainErr error
	err := v.withStateLock(func() error {
		h, err := v.listHistoryLocked(limit)
		history = h
		chainErr = err
		return nil
	})
	if err != nil {
		return history, err
	}
	return history, chainErr
}

// listHistoryLocked walks the snapshot chain from HEAD. Callers must hold both
// the per-instance lock and the cross-process state-dir lock so that the walk
// never observes a HEAD advanced by a concurrent commit in another Manager.
// The first (HEAD) snapshot must resolve; a missing ancestor ends the walk.
func (v *Manager) listHistoryLocked(limit int) ([]*Snapshot, error) {
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
			if len(history) == 0 {
				return nil, fmt.Errorf("HEAD snapshot %s is unreadable: %w", currentID, err)
			}
			// Reached the end of retained history (pruned ancestor).
			return history, nil
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

	var oldFiles map[string]flatFile
	if oldID != "" {
		oldSnap, err := v.getSnapshot(oldID)
		if err != nil {
			return nil, err
		}
		oldFiles, err = v.snapshotFiles(oldSnap)
		if err != nil {
			return nil, err
		}
	} else {
		oldFiles = make(map[string]flatFile)
	}

	newSnap, err := v.getSnapshot(newID)
	if err != nil {
		return nil, err
	}
	newFiles, err := v.snapshotFiles(newSnap)
	if err != nil {
		return nil, err
	}

	diffs := make(map[string]FileDiff)

	// Tree keys are slash-separated; present them in the platform's relative
	// form so callers see the same paths as before tree storage (and the same
	// form Checkout uses).
	toPlatform := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.FromSlash(p)
	}

	for path, oldFile := range oldFiles {
		newFile, exists := newFiles[path]
		if !exists {
			diffs[toPlatform(path)] = FileDiff{Status: DiffDeleted, OldHash: oldFile.hash}
		} else if oldFile.hash != newFile.hash {
			diffs[toPlatform(path)] = FileDiff{Status: DiffModified, OldHash: oldFile.hash, NewHash: newFile.hash}
		}
	}

	for path, newFile := range newFiles {
		if _, exists := oldFiles[path]; !exists {
			diffs[toPlatform(path)] = FileDiff{Status: DiffAdded, NewHash: newFile.hash}
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

	var deleted int
	err := v.withStateLock(func() error {
		d, err := v.gcLocked(keepLast)
		deleted = d
		return err
	})
	if err != nil {
		return deleted, err
	}
	return deleted, nil
}

func (v *Manager) gcLocked(keepLast int) (int, error) {

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

	// keepSnaps caches the snapshots loaded while walking the chain, so the
	// second pass below reuses them instead of re-reading, re-parsing and
	// re-verifying each snapshot file (and its parent link) a second time.
	keepSet := make(map[string]bool)
	keepSnaps := make(map[string]*Snapshot)
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
		keepSnaps[currentID] = snap
		currentID = snap.ParentID
	}

	// Fail-safe: a non-empty HEAD that resolves to no keepable snapshot means
	// HEAD points at a missing snapshot; deleting everything would be unsafe.
	if headID != "" && len(keepSet) == 0 && len(allSnaps) > 0 {
		return 0, fmt.Errorf("HEAD %s points to a missing snapshot; refusing to GC %d snapshots (state inconsistent)", headID[:12], len(allSnaps))
	}

	// Delete stale snapshots first. This must be fail-closed: if a snapshot
	// file cannot be removed (permissions, file locks, read-only media), it
	// still exists on disk and still references its objects. Pruning objects
	// in that state would leave a readable snapshot whose content is gone,
	// silently corrupting recoverable history. So any deletion failure aborts
	// the whole GC before object pruning begins.
	deletedSnapshots := 0
	if err := v.deleteSnapshots(snapDir, allSnaps, keepSet); err != nil {
		return deletedSnapshots, err
	}
	deletedSnapshots = len(allSnaps) - len(keepSet)

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

	for id, snap := range keepSnaps {
		// Mark the snapshot's tree, every subtree, and every blob reachable
		// from it. Tree objects are stored objects too: missing them here would
		// delete a kept snapshot's ability to be expanded.
		files, err := v.snapshotFiles(snap)
		if err != nil {
			return deletedSnapshots, fmt.Errorf("failed to expand kept snapshot %s: %w", id[:12], err)
		}
		for _, f := range files {
			keepHashes[f.hash] = true
		}
		for treeHash := range snap.trees {
			keepHashes[treeHash] = true
		}
		// 保护快照 metadata 里引用的对象 hash(如 memory_hash)。这样把
		// 任务记忆等非工作树状态以 blob 形式版本化时,GC 不会误删它们。
		for _, hash := range snap.Metadata {
			if isValidHash(hash) == nil {
				keepHashes[hash] = true
			}
		}
	}

	deletedObjects, err := v.store.PruneObjects(keepHashes, 1*time.Hour)
	if err != nil {
		return deletedSnapshots, fmt.Errorf("prune objects failed: %w", err)
	}

	return deletedSnapshots + deletedObjects, nil
}

// deleteSnapshots removes every snapshot in allSnaps that is not in keepSet.
// It is all-or-nothing in spirit: the first removal failure is returned
// immediately so the caller can abort before pruning objects. Snapshots
// already removed before the failure stay removed (they are unambiguously
// garbage); the caller must not report a partial count as success.
func (v *Manager) deleteSnapshots(snapDir string, allSnaps, keepSet map[string]bool) error {
	for id := range allSnaps {
		if keepSet[id] {
			continue
		}
		if err := os.Remove(filepath.Join(snapDir, id+".json")); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to delete snapshot %s: %w; refusing to prune objects", id[:12], err)
		}
	}
	return nil
}
