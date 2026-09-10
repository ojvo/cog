package vcs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- 多层 tree 对象 ---

// TestTree_SubtreeSharing 验证：只改一个文件时，未被触及的目录子树哈希不变，
// 即子树在多次快照之间共享，而不是每次重建。
func TestTree_SubtreeSharing(t *testing.T) {
	dir := t.TempDir()
	s := NewObjectStore(dir)

	// 两个目录 a/ 和 b/，各含一个文件。
	blobA, _ := s.PutData([]byte("a"))
	blobB, _ := s.PutData([]byte("b"))
	blobB2, _ := s.PutData([]byte("b2"))

	build := func(aHash, bHash string) string {
		b := newTreeBuilder()
		if err := b.add("a/f.txt", aHash, false); err != nil {
			t.Fatal(err)
		}
		if err := b.add("b/f.txt", bHash, false); err != nil {
			t.Fatal(err)
		}
		root, err := b.build(s)
		if err != nil {
			t.Fatal(err)
		}
		return root
	}

	root1 := build(blobA, blobB)
	root2 := build(blobA, blobB2) // 只改 b/

	if root1 == root2 {
		t.Fatal("tree hash must change when content changes")
	}

	// 解出两个版本的 a/ 子树哈希，必须相同（共享）。
	a1 := subtreeHash(t, s, root1, "a")
	a2 := subtreeHash(t, s, root2, "a")
	if a1 != a2 {
		t.Fatalf("unchanged subtree a/ should be shared: %s vs %s", a1, a2)
	}
	b1 := subtreeHash(t, s, root1, "b")
	b2 := subtreeHash(t, s, root2, "b")
	if b1 == b2 {
		t.Fatal("changed subtree b/ should differ")
	}
}

// subtreeHash returns the tree hash of a top-level directory in a root tree.
func subtreeHash(t *testing.T, s *ObjectStore, root, name string) string {
	t.Helper()
	data, err := s.GetData(root)
	if err != nil {
		t.Fatal(err)
	}
	entries, _, err := decodeTree(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name == name {
			if e.Kind != kindTree {
				t.Fatalf("%s is not a tree", name)
			}
			return e.Hash
		}
	}
	t.Fatalf("subtree %s not found", name)
	return ""
}

// TestTree_DeterministicHash 验证 tree 哈希与插入顺序无关。
func TestTree_DeterministicHash(t *testing.T) {
	dir := t.TempDir()
	s := NewObjectStore(dir)
	h1, _ := s.PutData([]byte("1"))
	h2, _ := s.PutData([]byte("2"))
	h3, _ := s.PutData([]byte("3"))

	encode1, hash1, err := encodeTree([]treeEntry{
		{Name: "c", Kind: kindBlob, Hash: h3},
		{Name: "a", Kind: kindBlob, Hash: h1},
		{Name: "b", Kind: kindBlob, Hash: h2},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, hash2, err := encodeTree([]treeEntry{
		{Name: "a", Kind: kindBlob, Hash: h1},
		{Name: "b", Kind: kindBlob, Hash: h2},
		{Name: "c", Kind: kindBlob, Hash: h3},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hash1 != hash2 {
		t.Fatal("tree hash must be independent of entry order")
	}

	// 内容可解析且顺序规范。
	entries, _, err := decodeTree(encode1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[0].Name != "a" || entries[2].Name != "c" {
		t.Fatalf("entries not canonical: %+v", entries)
	}
}

// TestTree_RejectsTraversalNames 验证 decodeTree 拒绝可拼接为逃逸路径的条目名。
func TestTree_RejectsTraversalNames(t *testing.T) {
	for _, name := range []string{"..", ".", "a/b", `a\b`, ""} {
		raw, _ := json.Marshal(&tree{Entries: []treeEntry{
			{Name: name, Kind: kindBlob, Hash: strings.Repeat("a", 64)},
		}})
		if _, _, err := decodeTree(raw); err == nil {
			t.Fatalf("decodeTree must reject entry name %q", name)
		}
	}
}

// TestTree_ExpandDistinguishesMissingFromCorrupt 验证树展开错误保留区分度：
// 缺失对象应可被 errors.Is(err, fs.ErrNotExist) 识别，损坏对象应可被
// errors.Is(err, ErrCorruptObject) 识别，而不是被压平成同一种 "not found"。
func TestTree_ExpandDistinguishesMissingFromCorrupt(t *testing.T) {
	dir := t.TempDir()
	s := NewObjectStore(dir)

	// 场景 1：根 tree 根本不存在 → 缺失。
	missingTree := strings.Repeat("ab", 32)
	_, _, err := expandTree(s, missingTree, s.getObjectBytes)
	if err == nil {
		t.Fatal("expanding a missing tree must fail")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing tree error should wrap fs.ErrNotExist, got: %v", err)
	}
	if errors.Is(err, ErrCorruptObject) {
		t.Fatalf("missing tree must not be reported as corrupt: %v", err)
	}

	// 场景 2：存在一个其字节与哈希不符的 tree 对象 → 损坏。
	// 手动写一个内容不匹配其文件名（哈希）的对象文件。
	badHash := strings.Repeat("cd", 32)
	badDir := filepath.Join(dir, "objects", badHash[:2])
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badDir, badHash[2:]), []byte("not matching its hash"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err = expandTree(s, badHash, s.getObjectBytes)
	if err == nil {
		t.Fatal("expanding a corrupt tree must fail")
	}
	if !errors.Is(err, ErrCorruptObject) {
		t.Fatalf("corrupt tree error should wrap ErrCorruptObject, got: %v", err)
	}

	// 场景 3：对象存在且哈希正确，但内容不是合法 tree → 也应视为损坏。
	garbageHash, err := s.PutData([]byte("{}this is not a valid tree json"))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = expandTree(s, garbageHash, s.getObjectBytes)
	if err == nil {
		t.Fatal("expanding a malformed tree must fail")
	}
	if !errors.Is(err, ErrCorruptObject) {
		t.Fatalf("malformed tree error should wrap ErrCorruptObject, got: %v", err)
	}
}

// TestTreeBuilder_RejectsFileDirConflict 验证同名路径不能同时是文件与目录。
func TestTreeBuilder_RejectsFileDirConflict(t *testing.T) {
	b := newTreeBuilder()
	if err := b.add("x", strings.Repeat("a", 64), false); err != nil {
		t.Fatal(err)
	}
	if err := b.add("x/y", strings.Repeat("b", 64), false); err == nil {
		t.Fatal("adding x/y under existing file x must fail")
	}

	b2 := newTreeBuilder()
	if err := b2.add("x/y", strings.Repeat("a", 64), false); err != nil {
		t.Fatal(err)
	}
	if err := b2.add("x", strings.Repeat("b", 64), false); err == nil {
		t.Fatal("adding file x over existing directory x must fail")
	}
}

// --- 扁平大目录分片（tree fanout） ---

// TestTree_FlatShapeUnchangedBelowFanout 验证不超过 fanout 的目录仍写入原有的
// 扁平行式：其序列化字节与直接用 encodeTree 产生的完全一致，即哈希不变。
func TestTree_FlatShapeUnchangedBelowFanout(t *testing.T) {
	dir := t.TempDir()
	s := NewObjectStore(dir)

	entries := make([]treeEntry, 0, treeFanout)
	for i := 0; i < treeFanout; i++ {
		entries = append(entries, treeEntry{
			Name: fmt.Sprintf("f%04d", i),
			Kind: kindBlob,
			Hash: strings.Repeat("a", 64),
		})
	}

	b := newTreeBuilder()
	for _, e := range entries {
		if err := b.add(e.Name, e.Hash, e.Exec); err != nil {
			t.Fatal(err)
		}
	}
	gotHash, err := b.build(s)
	if err != nil {
		t.Fatal(err)
	}

	wantData, wantHash, err := encodeTree(entries)
	if err != nil {
		t.Fatal(err)
	}
	if gotHash != wantHash {
		t.Fatalf("flat directory hash changed: got %s want %s", gotHash, wantHash)
	}
	gotData, err := s.GetData(gotHash)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotData) != string(wantData) {
		t.Fatal("flat directory bytes must be identical to encodeTree output")
	}
}

// TestTree_ShardsAboveFanout 验证超过 fanout 的目录被分片：目录对象只含 shard
// 哈希，且每个 shard 是 <= treeFanout 的扁平 tree。
func TestTree_ShardsAboveFanout(t *testing.T) {
	dir := t.TempDir()
	s := NewObjectStore(dir)

	total := treeFanout*3 + 7
	b := newTreeBuilder()
	for i := 0; i < total; i++ {
		if err := b.add(fmt.Sprintf("f%05d", i), strings.Repeat("a", 64), false); err != nil {
			t.Fatal(err)
		}
	}
	root, err := b.build(s)
	if err != nil {
		t.Fatal(err)
	}

	data, err := s.GetData(root)
	if err != nil {
		t.Fatal(err)
	}
	entries, shards, err := decodeTree(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("sharded directory must not hold entries, got %d", len(entries))
	}
	wantShards := (total + treeFanout - 1) / treeFanout
	if len(shards) != wantShards {
		t.Fatalf("shard count = %d, want %d", len(shards), wantShards)
	}
	// 每个 shard 必须是 <= fanout 的扁平 tree，且拼接后覆盖全部条目。
	seen := 0
	for i, sh := range shards {
		// 首键必须等于该分片内最小条目名。
		if i > 0 && !(shards[i-1].Key < sh.Key) {
			t.Fatalf("shard keys must strictly increase: %q then %q", shards[i-1].Key, sh.Key)
		}
		sd, err := s.GetData(sh.Hash)
		if err != nil {
			t.Fatal(err)
		}
		se, ss, err := decodeTree(sd)
		if err != nil {
			t.Fatal(err)
		}
		if len(ss) != 0 {
			t.Fatal("a shard must not itself be sharded")
		}
		if len(se) > treeFanout {
			t.Fatalf("shard has %d entries, want <= %d", len(se), treeFanout)
		}
		if len(se) > 0 && se[0].Name != sh.Key {
			t.Fatalf("shard key %q must equal its first entry %q", sh.Key, se[0].Name)
		}
		seen += len(se)
	}
	if seen != total {
		t.Fatalf("shards cover %d entries, want %d", seen, total)
	}
}

// TestTree_ShardedRoundTrip 验证分片目录可被完整展开回原始路径表。
func TestTree_ShardedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewObjectStore(dir)

	total := treeFanout*2 + 13
	b := newTreeBuilder()
	want := make(map[string]string, total)
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("f%05d", i)
		hash := fmt.Sprintf("%064x", i)
		want[name] = hash
		if err := b.add(name, hash, false); err != nil {
			t.Fatal(err)
		}
	}
	root, err := b.build(s)
	if err != nil {
		t.Fatal(err)
	}

	files, _, err := expandTree(s, root, s.getObjectBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != total {
		t.Fatalf("expanded %d files, want %d", len(files), total)
	}
	for name, hash := range want {
		if files[name].hash != hash {
			t.Fatalf("file %s hash = %s, want %s", name, files[name].hash, hash)
		}
	}
}

// TestTree_ShardedChangeReusesUntouchedShards 验证分片目录上改一个文件时，
// 只有包含该文件的分片被重写，其余分片哈希保持不变（这是 #3 的核心收益）。
func TestTree_ShardedChangeReusesUntouchedShards(t *testing.T) {
	dir := t.TempDir()
	s := NewObjectStore(dir)

	total := treeFanout * 3
	// 首轮：全部同名同哈希，形成一个 3 分片的目录。
	seed := newTreeBuilder()
	for i := 0; i < total; i++ {
		if err := seed.add(fmt.Sprintf("f%05d", i), strings.Repeat("a", 64), false); err != nil {
			t.Fatal(err)
		}
	}
	root1, err := seed.build(s)
	if err != nil {
		t.Fatal(err)
	}

	// 从已有分片树出发，改最后一个分片里的一个文件（名字排序在最后一档）。
	mut := newBuilderFromTree(s, root1)
	if err := mut.add(fmt.Sprintf("f%05d", total-1), strings.Repeat("b", 64), false); err != nil {
		t.Fatal(err)
	}
	root2, err := mut.build(s)
	if err != nil {
		t.Fatal(err)
	}

	if root1 == root2 {
		t.Fatal("changing an entry must change the directory hash")
	}
	s1 := loadShards(t, s, root1)
	s2 := loadShards(t, s, root2)
	if len(s1) != len(s2) {
		t.Fatalf("shard count changed: %d -> %d", len(s1), len(s2))
	}
	// 前 n-1 个分片必须逐字节复用（哈希相同），只有最后一个变化。
	for i := 0; i < len(s1)-1; i++ {
		if s1[i] != s2[i] {
			t.Fatalf("untouched shard %d must be reused: %s vs %s", i, s1[i].Hash, s2[i].Hash)
		}
	}
	if s1[len(s1)-1] == s2[len(s2)-1] {
		t.Fatal("the shard containing the changed entry must differ")
	}
}

// TestTree_ShardedDeleteKeepsConsistency 验证在分片目录中删除条目后，展开结果
// 与直接构建的内容一致（重分片后边界仍规范）。
func TestTree_ShardedDeleteKeepsConsistency(t *testing.T) {
	dir := t.TempDir()
	s := NewObjectStore(dir)

	total := treeFanout*2 + 5
	want := map[string]string{}
	b := newTreeBuilder()
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("f%05d", i)
		want[name] = strings.Repeat("a", 64)
		if err := b.add(name, strings.Repeat("a", 64), false); err != nil {
			t.Fatal(err)
		}
	}
	root1, err := b.build(s)
	if err != nil {
		t.Fatal(err)
	}

	// 从一个中间分片删除若干条目。
	b2 := newBuilderFromTree(s, root1)
	for _, i := range []int{treeFanout - 1, treeFanout, treeFanout + 1} {
		name := fmt.Sprintf("f%05d", i)
		delete(want, name)
		if err := b2.remove(name); err != nil {
			t.Fatal(err)
		}
	}
	root2, err := b2.build(s)
	if err != nil {
		t.Fatal(err)
	}

	files, _, err := expandTree(s, root2, s.getObjectBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(want) {
		t.Fatalf("after delete, expanded %d files, want %d", len(files), len(want))
	}
	for name := range want {
		if _, ok := files[name]; !ok {
			t.Fatalf("file %s should still be present", name)
		}
	}
}

// TestTree_ShardedMatchesFreshBuild 验证分片目录经过一次修改后，其哈希与
// "从零重建同样内容"的哈希一致——即重分片结果规范、与构建路径无关。
func TestTree_ShardedMatchesFreshBuild(t *testing.T) {
	dir := t.TempDir()
	s := NewObjectStore(dir)

	total := treeFanout*2 + 11
	seed := newTreeBuilder()
	for i := 0; i < total; i++ {
		if err := seed.add(fmt.Sprintf("f%05d", i), strings.Repeat("a", 64), false); err != nil {
			t.Fatal(err)
		}
	}
	root1, err := seed.build(s)
	if err != nil {
		t.Fatal(err)
	}

	// 在已有分片树上改一个条目。
	mut := newBuilderFromTree(s, root1)
	target := fmt.Sprintf("f%05d", treeFanout+3)
	if err := mut.add(target, strings.Repeat("c", 64), false); err != nil {
		t.Fatal(err)
	}
	got, err := mut.build(s)
	if err != nil {
		t.Fatal(err)
	}

	// 从零重建同样内容。
	fresh := newTreeBuilder()
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("f%05d", i)
		h := strings.Repeat("a", 64)
		if name == target {
			h = strings.Repeat("c", 64)
		}
		if err := fresh.add(name, h, false); err != nil {
			t.Fatal(err)
		}
	}
	want, err := fresh.build(s)
	if err != nil {
		t.Fatal(err)
	}

	if got != want {
		t.Fatalf("sharded rebuild hash = %s, want %s (must match a fresh build)", got, want)
	}
}

// loadShards returns the shard references of a directory tree object.
func loadShards(t *testing.T, s *ObjectStore, root string) []shardRef {
	t.Helper()
	data, err := s.GetData(root)
	if err != nil {
		t.Fatal(err)
	}
	_, shards, err := decodeTree(data)
	if err != nil {
		t.Fatal(err)
	}
	return shards
}

// --- 增量提交 ---

// TestManager_Commit_IncrementalReusesUnchangedSubtrees 验证一次提交只写变更路径
// 上的 tree，未变目录子树保持同一哈希。
func TestManager_Commit_IncrementalReusesUnchangedSubtrees(t *testing.T) {
	workDir := t.TempDir()
	mgr := NewManager(filepath.Join(workDir, ".jabo"), workDir)

	mk := func(rel, content string) {
		p := filepath.Join(workDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := mgr.RecordOldState(p); err != nil {
			t.Fatal(err)
		}
	}
	mk(filepath.Join("a", "f.txt"), "a1")
	mk(filepath.Join("b", "f.txt"), "b1")
	snap1, err := mgr.CommitPending("base", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	mk(filepath.Join("b", "f.txt"), "b2")
	snap2, err := mgr.CommitPending("change b", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if snap1.Tree == snap2.Tree {
		t.Fatal("root tree must change")
	}
	if subtreeHash(t, mgr.store, snap1.Tree, "a") != subtreeHash(t, mgr.store, snap2.Tree, "a") {
		t.Fatal("unchanged subtree a/ must be shared between snapshots")
	}
	if subtreeHash(t, mgr.store, snap1.Tree, "b") == subtreeHash(t, mgr.store, snap2.Tree, "b") {
		t.Fatal("changed subtree b/ must differ")
	}
}

// countObjects returns how many object files the store holds.
func countObjects(t *testing.T, mgr *Manager) int {
	t.Helper()
	total := 0
	entries, err := os.ReadDir(mgr.store.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(mgr.store.baseDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		total += len(files)
	}
	return total
}

// TestManager_Commit_RepeatedUnchangedIsNoOpOnTree 验证幂等提交：重复提交内容未变的
// 文件时，不写任何新的对象——根 tree 复用父快照的哈希，且对象总数不变。
// 注意快照本身仍会创建（时间戳不同），只是 tree 层完全复用。
func TestManager_Commit_RepeatedUnchangedIsNoOpOnTree(t *testing.T) {
	workDir := t.TempDir()
	mgr := NewManager(filepath.Join(workDir, ".jabo"), workDir)

	f := filepath.Join(workDir, "sub", "f.txt")
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("stable"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RecordOldState(f); err != nil {
		t.Fatal(err)
	}
	snap1, err := mgr.CommitPending("first", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	before := countObjects(t, mgr)

	// 记录同一文件但内容不变，再次提交。
	if err := mgr.RecordOldState(f); err != nil {
		t.Fatal(err)
	}
	snap2, err := mgr.CommitPending("second", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// 根 tree 必须完全复用（这是幂等性的核心断言）。
	if snap1.Tree != snap2.Tree {
		t.Fatalf("unchanged commit must reuse the parent tree: %s vs %s", snap1.Tree, snap2.Tree)
	}
	// 不得写入任何新对象（blob 与 tree 都已存在）。
	if after := countObjects(t, mgr); after != before {
		t.Fatalf("unchanged commit wrote new objects: %d -> %d", before, after)
	}
}

// TestManager_Commit_DeletePrunesEmptyDirs 验证删除目录内最后一个文件后，空目录
// 不再出现在树中。
func TestManager_Commit_DeletePrunesEmptyDirs(t *testing.T) {
	workDir := t.TempDir()
	mgr := NewManager(filepath.Join(workDir, ".jabo"), workDir)

	nested := filepath.Join(workDir, "x", "y", "f.txt")
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nested, []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RecordOldState(nested); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.CommitPending("base", nil, nil); err != nil {
		t.Fatal(err)
	}

	if err := mgr.RecordOldState(nested); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(nested); err != nil {
		t.Fatal(err)
	}
	snap, err := mgr.CommitPending("delete", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	files := snapFileTable(t, mgr, snap)
	if len(files) != 0 {
		t.Fatalf("expected empty tree after deleting the only file, got %v", files)
	}
}

// --- GC 与 tree ---

// TestManager_GC_KeepsTreeObjects 验证 GC 不会删除被保留快照引用的 tree 对象，
// 保留的历史仍可展开与 checkout。
func TestManager_GC_KeepsTreeObjects(t *testing.T) {
	workDir := t.TempDir()
	mgr := NewManager(filepath.Join(workDir, ".jabo"), workDir)

	f := filepath.Join(workDir, "sub", "f.txt")
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RecordOldState(f); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.CommitPending("c1", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RecordOldState(f); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap2, err := mgr.CommitPending("c2", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := mgr.GC(1); err != nil {
		t.Fatalf("GC failed: %v", err)
	}

	// 保留的 HEAD 必须仍能展开并 checkout（其 tree 对象未被 GC 删除）。
	files := snapFileTable(t, mgr, snap2)
	if _, ok := files["sub/f.txt"]; !ok {
		t.Fatalf("retained snapshot tree lost its entries after GC: %v", files)
	}
	if err := mgr.Checkout(snap2.ID); err != nil {
		t.Fatalf("checkout of retained snapshot failed after GC: %v", err)
	}
	got, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v2" {
		t.Fatalf("restored content = %q, want v2", got)
	}
}

// --- 快照只引用 tree ---

// TestManager_SnapshotFileIsSmall 验证快照文件体积与文件数无关（只含 tree 哈希），
// 这是增量快照的核心收益之一。
func TestManager_SnapshotFileIsSmall(t *testing.T) {
	workDir := t.TempDir()
	mgr := NewManager(filepath.Join(workDir, ".jabo"), workDir)

	for i := 0; i < 200; i++ {
		p := filepath.Join(workDir, "d", "f"+string(rune('a'+i%26))+string(rune('0'+i/26))+".txt")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := mgr.RecordOldState(p); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := mgr.CommitPending("many", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(mgr.baseDir, "snapshots", snap.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	// 快照 JSON 只含 id/parent/timestamp/message/tree/metadata，应远小于
	// 逐文件表（200 条 path->hash 至少数 KB）。留出充足余量以避免脆断言。
	if len(data) > 2048 {
		t.Fatalf("snapshot file should be O(1) in tree size, got %d bytes", len(data))
	}
}
