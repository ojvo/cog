package vcs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

// treeEntryKind distinguishes a subtree (directory) from a file blob.
type treeEntryKind string

const (
	kindBlob treeEntryKind = "blob"
	kindTree treeEntryKind = "tree"
)

// treeEntry is one child of a tree object: a file blob or a subdirectory.
type treeEntry struct {
	Name string        `json:"name"`
	Kind treeEntryKind `json:"kind"`
	Hash string        `json:"hash"`
	// Exec records the executable bit for blobs. File permission bits beyond
	// this are not versioned (content addressing makes an object's own mode
	// irrelevant); only executability survives checkout.
	Exec bool `json:"exec,omitempty"`
}

// shardRef is one slice of a sharded directory: Key is the smallest entry name
// in the slice. Carrying the key in the parent lets a mutation route to the one
// shard that can contain a name without reading any shard, which is what makes
// a change to a huge directory O(1) instead of O(shard count).
type shardRef struct {
	Key  string `json:"key"`
	Hash string `json:"hash"`
}

// tree is a directory. Its content address (SHA-256 of the canonical
// serialization) is the tree hash used by snapshots and by parent trees to
// reference subtrees, so an unchanged directory keeps the same hash and is
// shared across snapshots instead of being re-stored.
//
// A directory is stored in one of two shapes:
//
//   - Flat: Entries holds its children directly. This is the shape used by
//     every directory with at most treeFanout children, and it is byte-identical
//     to the original single-object format, so ordinary directories keep their
//     existing hashes.
//   - Sharded: Shards holds references to consecutive slices of the children
//     (in sorted order), each slice a separate flat tree. A directory only takes
//     this shape once it exceeds treeFanout children, which is what keeps a
//     one-file change in a huge directory O(1): the shard's key range identifies
//     the single shard to read and rewrite, leaving the others untouched.
//
// Entries and Shards are mutually exclusive: at most one is non-empty (an empty
// directory has neither, and is the flat shape with zero entries).
type tree struct {
	Entries []treeEntry `json:"entries,omitempty"`
	Shards  []shardRef  `json:"shards,omitempty"`
}

// treeFanout is the maximum number of children a flat tree holds. A directory
// with more children is split into shards of at most this size. The cap bounds
// the worst-case re-serialization of a single change: a flat directory of N
// files costs O(N) per change, a sharded one costs O(treeFanout) plus O(1) for
// the parent.
const treeFanout = 256

// canonicalEntries sorts entries by name so the serialization — and therefore
// the hash — depends only on directory content, never on insertion order.
func canonicalEntries(entries []treeEntry) []treeEntry {
	sorted := make([]treeEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	return sorted
}

// encodeTree serializes a flat tree to its canonical bytes. It is the single
// source of truth for a flat tree's content address, so hash and storage can
// never drift.
func encodeTree(entries []treeEntry) ([]byte, string, error) {
	return encodeTreeObj(&tree{Entries: canonicalEntries(entries)})
}

// encodeShardedTree serializes a directory that references child slices. Shard
// order is significant (it is the sorted key order), so unlike entries it is not
// re-sorted.
func encodeShardedTree(shards []shardRef) ([]byte, string, error) {
	return encodeTreeObj(&tree{Shards: shards})
}

func encodeTreeObj(t *tree) ([]byte, string, error) {
	data, err := json.Marshal(t)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal tree: %w", err)
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}

// decodeTree parses and validates a tree object's bytes, returning either its
// entries (flat shape) or its shard hashes (sharded shape).
//
// A malformed entry or an entry name that is not a single path segment (empty,
// ".", ".." or containing a separator) is rejected: tree contents are
// attacker-influenced (they come from snapshot files on disk) and a name like
// "../x" must never be spliced into a filesystem path.
func decodeTree(data []byte) (entries []treeEntry, shards []shardRef, err error) {
	var t tree
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal tree: %w", err)
	}
	if len(t.Entries) > 0 && len(t.Shards) > 0 {
		return nil, nil, fmt.Errorf("tree must not contain both entries and shards")
	}
	for _, e := range t.Entries {
		if e.Name == "" || e.Name == "." || e.Name == ".." ||
			strings.ContainsAny(e.Name, `/\`) {
			return nil, nil, fmt.Errorf("tree contains invalid entry name %q", e.Name)
		}
		if e.Kind != kindBlob && e.Kind != kindTree {
			return nil, nil, fmt.Errorf("tree entry %q has invalid kind %q", e.Name, e.Kind)
		}
		if err := isValidHash(e.Hash); err != nil {
			return nil, nil, fmt.Errorf("tree entry %q has invalid hash: %w", e.Name, err)
		}
	}
	var prevKey string
	for i, sh := range t.Shards {
		if err := isValidHash(sh.Hash); err != nil {
			return nil, nil, fmt.Errorf("tree has invalid shard hash %q: %w", sh.Hash, err)
		}
		if sh.Key == "" || sh.Key == "." || sh.Key == ".." ||
			strings.ContainsAny(sh.Key, `/\`) {
			return nil, nil, fmt.Errorf("tree has invalid shard key %q", sh.Key)
		}
		// Shards partition the sorted entry space, so keys must strictly
		// increase; a non-increasing key would make routing ambiguous.
		if i > 0 && sh.Key <= prevKey {
			return nil, nil, fmt.Errorf("tree shard keys must strictly increase: %q after %q", sh.Key, prevKey)
		}
		prevKey = sh.Key
	}
	return t.Entries, t.Shards, nil
}

// splitTreePath splits a slash-separated relative path into directory segments
// and a leaf name. A path with no leaf (empty or trailing slash) is invalid
// because a tree entry must name a file or directory.
func splitTreePath(relPath string) (dirs []string, leaf string, err error) {
	clean := strings.Trim(path.Clean("/"+strings.ReplaceAll(relPath, `\`, "/")), "/")
	if clean == "" {
		return nil, "", fmt.Errorf("empty path")
	}
	parts := strings.Split(clean, "/")
	for _, p := range parts {
		if p == "." || p == ".." {
			return nil, "", fmt.Errorf("path %q escapes the work tree", relPath)
		}
	}
	return parts[:len(parts)-1], parts[len(parts)-1], nil
}

// flatFile is one (path, hash, executable) row of an expanded snapshot tree.
type flatFile struct {
	hash string
	exec bool
}

// treeBuilder accumulates paths into a directory tree and writes each distinct
// tree object exactly once, returning the root tree hash.
//
// It is lazy and tree-backed: it can be seeded with a parent root tree and only
// expands (reads) the directories a mutation actually touches. An untouched
// subtree keeps its hash verbatim and is never re-read or re-written, so a
// one-file commit costs O(path depth) object reads and writes — not O(tree).
type treeBuilder struct {
	root *treeNode
	// store resolves and persists tree objects; nil means "build only, do not
	// read or store" (used by tests that construct trees from scratch).
	store *ObjectStore
	// written caches tree hash -> struct{} so a tree object is never stored
	// twice within one build.
	written map[string]struct{}
}

// treeNode is either a file leaf, a loaded directory (children), or an
// unloaded directory referenced by treeHash. A node is never more than one of
// these.
type treeNode struct {
	children map[string]*treeNode
	file     *flatFile
	// treeHash is set for an unloaded subtree: its content lives in the object
	// store and is expanded on demand.
	treeHash string
	// shardRefs records, for a directory loaded from the sharded shape, the
	// shard key ranges and hashes (in ascending key order).
	shardRefs []shardRef
	// shardChildren[i] holds the expanded children of shard i. Only shards that
	// a mutation routed to are ever expanded; the rest stay unread and are
	// reused by hash on rebuild. shardChildren is nil for a flat directory.
	shardChildren map[int]map[string]*treeNode
	// sharded reports whether this directory is (or should be) stored sharded.
	// It is set for a directory loaded from the sharded shape, and for a flat
	// directory whose child count has grown past treeFanout.
	sharded bool
	// dirty marks a loaded directory whose children changed, so it must be
	// re-serialized and re-stored.
	dirty bool
}

// shardIndexFor maps an entry name to the shard it belongs to: the last shard
// whose first-key is <= name. Shards partition the sorted entry space, so this
// is a binary search over the shard keys. It returns -1 when there are no
// shards.
func shardIndexFor(refs []shardRef, name string) int {
	lo, hi := 0, len(refs)
	for lo < hi {
		mid := (lo + hi) / 2
		if refs[mid].Key <= name {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo - 1
}

func newTreeBuilder() *treeBuilder {
	return &treeBuilder{root: &treeNode{children: map[string]*treeNode{}}, written: map[string]struct{}{}}
}

// newBuilderFromTree returns a builder seeded with a parent root tree. Nothing
// is read until a path under it is mutated.
func newBuilderFromTree(s *ObjectStore, rootTree string) *treeBuilder {
	tb := newTreeBuilder()
	tb.store = s
	if rootTree != "" {
		tb.root = &treeNode{treeHash: rootTree}
	}
	return tb
}

// load expands a node's subtree if it is still lazy. Directories are loaded
// one level at a time as paths descend, so the whole tree is never read.
//
// A sharded directory is loaded without reading any shard: only the shard key
// ranges are recorded. Individual shards are expanded lazily by loadShard when
// a path routes into them, which keeps a single-file change in a huge directory
// proportional to one shard rather than the whole directory.
func (tb *treeBuilder) load(node *treeNode) error {
	if node.treeHash == "" {
		return nil
	}
	if node.file != nil || node.children != nil || node.shardChildren != nil {
		return nil
	}
	if tb.store == nil {
		return fmt.Errorf("cannot expand tree %s: builder has no object store", node.treeHash)
	}
	data, err := tb.store.GetData(node.treeHash)
	if err != nil {
		return fmt.Errorf("failed to read tree %s: %w", node.treeHash, err)
	}
	entries, shards, err := decodeTree(data)
	if err != nil {
		return err
	}

	node.treeHash = ""

	if len(shards) > 0 {
		node.sharded = true
		node.shardRefs = shards
		node.shardChildren = make(map[int]map[string]*treeNode, len(shards))
		return nil
	}

	node.children = make(map[string]*treeNode, len(entries))
	for _, e := range entries {
		node.children[e.Name] = newChildNode(e)
	}
	return nil
}

// newChildNode builds a tree node for a decoded entry.
func newChildNode(e treeEntry) *treeNode {
	child := &treeNode{}
	if e.Kind == kindTree {
		child.treeHash = e.Hash
	} else {
		child.file = &flatFile{hash: e.Hash, exec: e.Exec}
	}
	return child
}

// loadShard expands shard idx on demand and caches it. It is a no-op if the
// shard was already expanded.
func (tb *treeBuilder) loadShard(node *treeNode, idx int) (map[string]*treeNode, error) {
	if kids, ok := node.shardChildren[idx]; ok {
		return kids, nil
	}
	data, err := tb.store.GetData(node.shardRefs[idx].Hash)
	if err != nil {
		return nil, fmt.Errorf("failed to read shard %d (%s): %w", idx, node.shardRefs[idx].Hash, err)
	}
	entries, shards, err := decodeTree(data)
	if err != nil {
		return nil, err
	}
	if len(shards) > 0 {
		return nil, fmt.Errorf("%w: shard %s is itself sharded", ErrCorruptObject, node.shardRefs[idx].Hash)
	}
	kids := make(map[string]*treeNode, len(entries))
	for _, e := range entries {
		kids[e.Name] = newChildNode(e)
	}
	node.shardChildren[idx] = kids
	return kids, nil
}

// add inserts or replaces the file at relPath.
//
// Adding a file that is already present with the same hash and executable bit
// is a no-op: the path is not marked dirty, so the directories above it keep
// their existing hashes and are neither re-serialized nor re-stored. This is
// what makes re-committing unchanged content idempotent — the resulting root
// tree hash equals the parent's, and commit can skip creating a snapshot.
func (tb *treeBuilder) add(relPath, hash string, exec bool) error {
	dirs, leaf, err := splitTreePath(relPath)
	if err != nil {
		return err
	}
	node := tb.root
	if err := tb.load(node); err != nil {
		return err
	}
	for _, d := range dirs {
		child, err := tb.childByName(node, d, true)
		if err != nil {
			return fmt.Errorf("path %q: %w", relPath, err)
		}
		node = child
	}

	existing, found, err := tb.lookup(node, leaf)
	if err != nil {
		return err
	}
	if found {
		if existing.file == nil {
			return fmt.Errorf("path %q conflicts with an existing directory", relPath)
		}
		if existing.file.hash == hash && existing.file.exec == exec {
			return nil // unchanged: leave the tree (and its hashes) untouched
		}
	}
	if err := tb.setChild(node, leaf, &treeNode{file: &flatFile{hash: hash, exec: exec}}); err != nil {
		return err
	}
	return nil
}

// lookup returns the child named name, expanding the shard that can contain it
// (and only that shard). found reports whether it exists.
func (tb *treeBuilder) lookup(node *treeNode, name string) (*treeNode, bool, error) {
	if node.sharded {
		idx := shardIndexFor(node.shardRefs, name)
		if idx < 0 {
			return nil, false, nil
		}
		kids, err := tb.loadShard(node, idx)
		if err != nil {
			return nil, false, err
		}
		child, ok := kids[name]
		return child, ok, nil
	}
	child, ok := node.children[name]
	return child, ok, nil
}

// childByName returns the child directory named name, creating an empty
// directory when create is true and it does not exist. An existing non-directory
// is an error.
func (tb *treeBuilder) childByName(node *treeNode, name string, create bool) (*treeNode, error) {
	child, ok, err := tb.lookup(node, name)
	if err != nil {
		return nil, err
	}
	if ok {
		if child.file != nil {
			return nil, fmt.Errorf("conflicts with an existing file %q", name)
		}
		if err := tb.load(child); err != nil {
			return nil, err
		}
		return child, nil
	}
	if !create {
		return nil, nil
	}
	child = &treeNode{children: map[string]*treeNode{}}
	if err := tb.setChild(node, name, child); err != nil {
		return nil, err
	}
	return child, nil
}

// setChild stores name -> child in node, routing to the correct shard for a
// sharded directory and marking the affected directory (and shard) dirty.
func (tb *treeBuilder) setChild(node *treeNode, name string, child *treeNode) error {
	node.dirty = true
	if !node.sharded {
		if node.children == nil {
			node.children = map[string]*treeNode{}
		}
		node.children[name] = child
		return nil
	}
	idx := shardIndexFor(node.shardRefs, name)
	if idx < 0 {
		idx = 0
	}
	kids, err := tb.loadShard(node, idx)
	if err != nil {
		return err
	}
	kids[name] = child
	return nil
}

// deleteChild removes name from node, returning whether it existed.
func (tb *treeBuilder) deleteChild(node *treeNode, name string) (bool, error) {
	node.dirty = true
	if !node.sharded {
		if _, ok := node.children[name]; !ok {
			return false, nil
		}
		delete(node.children, name)
		return true, nil
	}
	idx := shardIndexFor(node.shardRefs, name)
	if idx < 0 {
		return false, nil
	}
	kids, err := tb.loadShard(node, idx)
	if err != nil {
		return false, err
	}
	if _, ok := kids[name]; !ok {
		return false, nil
	}
	delete(kids, name)
	return true, nil
}

// isEmpty reports whether a directory node currently has no children, loading
// nothing it does not already hold. A sharded directory with any shard present
// is not empty.
func (node *treeNode) isEmpty() bool {
	if node.sharded {
		for _, kids := range node.shardChildren {
			if len(kids) > 0 {
				return false
			}
		}
		return len(node.shardRefs) == 0
	}
	return len(node.children) == 0
}

// remove deletes the file at relPath, pruning any directories left empty.
func (tb *treeBuilder) remove(relPath string) error {
	dirs, leaf, err := splitTreePath(relPath)
	if err != nil {
		return err
	}
	if err := tb.load(tb.root); err != nil {
		return err
	}
	chain := []*treeNode{tb.root}
	node := tb.root
	for _, d := range dirs {
		child, err := tb.childByName(node, d, false)
		if err != nil {
			return err
		}
		if child == nil {
			return nil // nothing to remove
		}
		node = child
		chain = append(chain, node)
	}
	existed, err := tb.deleteChild(node, leaf)
	if err != nil || !existed {
		return err
	}
	// Prune now-empty directories bottom-up so a deleted file does not leave
	// phantom empty dirs in the tree.
	for i := len(chain) - 1; i >= 1; i-- {
		if !chain[i].isEmpty() {
			break
		}
		if _, err := tb.deleteChild(chain[i-1], dirs[i-1]); err != nil {
			return err
		}
	}
	return nil
}

// build serializes the accumulated tree into the object store, returning the
// root tree hash. Only dirty directories are re-serialized and re-stored;
// untouched subtrees keep their existing hash and are never read or written.
func (tb *treeBuilder) build(s *ObjectStore) (string, error) {
	if tb.store == nil {
		tb.store = s
	}
	if tb.written == nil {
		tb.written = map[string]struct{}{}
	}
	return tb.writeNode(tb.root)
}

func (tb *treeBuilder) writeNode(node *treeNode) (string, error) {
	if node.file != nil {
		return node.file.hash, nil
	}
	// An unloaded, unmodified subtree is reused verbatim.
	if node.treeHash != "" && !node.dirty {
		return node.treeHash, nil
	}
	if err := tb.load(node); err != nil {
		return "", err
	}

	if node.sharded {
		hash, err := tb.writeShardedNode(node)
		if err != nil {
			return "", err
		}
		node.treeHash = hash
		node.dirty = false
		return hash, nil
	}

	// Flat directory: build all child entries, recursing into subtrees first.
	entries, err := tb.childEntries(node.children)
	if err != nil {
		return "", err
	}

	var hash string
	if len(entries) <= treeFanout {
		hash, err = tb.storeFlatTree(entries)
	} else {
		// The directory has outgrown the flat shape; split it now.
		hash, err = tb.storeShardedEntries(entries)
	}
	if err != nil {
		return "", err
	}
	node.treeHash = hash
	node.dirty = false
	return hash, nil
}

// childEntries converts a directory's children into tree entries, recursing
// into subdirectories (which writes them first).
func (tb *treeBuilder) childEntries(children map[string]*treeNode) ([]treeEntry, error) {
	entries := make([]treeEntry, 0, len(children))
	for name, child := range children {
		if child.file != nil {
			entries = append(entries, treeEntry{Name: name, Kind: kindBlob, Hash: child.file.hash, Exec: child.file.exec})
			continue
		}
		subHash, err := tb.writeNode(child)
		if err != nil {
			return nil, err
		}
		entries = append(entries, treeEntry{Name: name, Kind: kindTree, Hash: subHash})
	}
	return entries, nil
}

// writeShardedNode rebuilds a directory that is stored in the sharded shape.
//
// Untouched shards are reused by hash and never read: only shards a mutation
// actually expanded are rebuilt. This is what keeps a one-file change in a huge
// directory proportional to one shard rather than the whole directory.
//
// A change can shift later boundaries only if it changed a shard's entry
// count (an add or a delete). Reuse therefore works in two parts:
//
//   - Shards before the first expanded shard are byte-identical and are reused
//     verbatim.
//   - If the expanded region's total entry count is unchanged (pure replaces)
//     and still chunks to the same shard sizes, shards after it are reused
//     verbatim too.
//
// Otherwise the tail from the first expanded shard is re-chunked, which is the
// only correct option when boundaries moved.
func (tb *treeBuilder) writeShardedNode(node *treeNode) (string, error) {
	// Determine the contiguous range of expanded shards [lo, hi].
	lo := len(node.shardRefs)
	hi := -1
	for idx := range node.shardChildren {
		if idx < lo {
			lo = idx
		}
		if idx > hi {
			hi = idx
		}
	}
	if hi < 0 {
		// Nothing was expanded: the directory is unchanged.
		return tb.emitShards(node.shardRefs)
	}

	// Collect the expanded region's entries, remembering how many entries the
	// original shards in that range held.
	regionEntries := 0
	for i := lo; i <= hi; i++ {
		kids, err := tb.loadShard(node, i)
		if err != nil {
			return "", err
		}
		regionEntries += len(kids)
	}
	// Re-read to build entries after knowing the original count (loadShard is
	// cached, so this is not extra I/O).
	var region []treeEntry
	for i := lo; i <= hi; i++ {
		entries, err := tb.childEntries(node.shardChildren[i])
		if err != nil {
			return "", err
		}
		region = append(region, entries...)
	}

	// A pure replace keeps the entry count and, because the name ordering is
	// unchanged, the region still chunks to the same number of shards. In that
	// case everything after the region is reused verbatim.
	regionShards := (regionEntries + treeFanout - 1) / treeFanout
	if len(region) == regionEntries && hi-lo+1 == regionShards {
		refs := make([]shardRef, 0, len(node.shardRefs))
		refs = append(refs, node.shardRefs[:lo]...)
		chunkRefs, err := tb.chunkShardRefs(region)
		if err != nil {
			return "", err
		}
		refs = append(refs, chunkRefs...)
		refs = append(refs, node.shardRefs[hi+1:]...)
		return tb.emitShards(refs)
	}

	// Boundaries may have moved: re-chunk the whole tail from lo to the end.
	var tail []treeEntry
	tail = append(tail, region...)
	for i := hi + 1; i < len(node.shardRefs); i++ {
		kids, err := tb.loadShard(node, i)
		if err != nil {
			return "", err
		}
		entries, err := tb.childEntries(kids)
		if err != nil {
			return "", err
		}
		tail = append(tail, entries...)
	}

	// If the whole directory has shrunk back to the flat threshold, store it
	// flat so small directories never carry shard indirection.
	total := lo*treeFanout + len(tail)
	if total <= treeFanout {
		var all []treeEntry
		for i := 0; i < lo; i++ {
			kids, err := tb.loadShard(node, i)
			if err != nil {
				return "", err
			}
			entries, err := tb.childEntries(kids)
			if err != nil {
				return "", err
			}
			all = append(all, entries...)
		}
		all = append(all, tail...)
		return tb.storeFlatTree(all)
	}

	refs := make([]shardRef, 0, len(node.shardRefs))
	refs = append(refs, node.shardRefs[:lo]...)
	chunkRefs, err := tb.chunkShardRefs(tail)
	if err != nil {
		return "", err
	}
	refs = append(refs, chunkRefs...)
	return tb.emitShards(refs)
}

// chunkShardRefs splits sorted entries into canonical shard refs of at most
// treeFanout entries each.
func (tb *treeBuilder) chunkShardRefs(entries []treeEntry) ([]shardRef, error) {
	sorted := canonicalEntries(entries)
	refs := make([]shardRef, 0, (len(sorted)+treeFanout-1)/treeFanout)
	for start := 0; start < len(sorted); start += treeFanout {
		end := start + treeFanout
		if end > len(sorted) {
			end = len(sorted)
		}
		chunk := sorted[start:end]
		hash, err := tb.storeFlatTree(chunk)
		if err != nil {
			return nil, err
		}
		refs = append(refs, shardRef{Key: chunk[0].Name, Hash: hash})
	}
	return refs, nil
}

// emitShards writes the sharded directory object for refs and returns its hash.
func (tb *treeBuilder) emitShards(refs []shardRef) (string, error) {
	data, hash, err := encodeShardedTree(refs)
	if err != nil {
		return "", err
	}
	if err := tb.storeTreeObject(data, hash); err != nil {
		return "", err
	}
	return hash, nil
}

// storeFlatTree writes a flat tree object (writing it only once per build) and
// returns its hash.
func (tb *treeBuilder) storeFlatTree(entries []treeEntry) (string, error) {
	data, hash, err := encodeTree(entries)
	if err != nil {
		return "", err
	}
	if err := tb.storeTreeObject(data, hash); err != nil {
		return "", err
	}
	return hash, nil
}

// storeShardedEntries splits sorted entries into consecutive shards of at most
// treeFanout, writes each shard as a flat tree, then writes this directory as a
// tree referencing the shards. The chunking is by sorted order, so the result is
// deterministic and independent of map iteration order.
func (tb *treeBuilder) storeShardedEntries(entries []treeEntry) (string, error) {
	sorted := canonicalEntries(entries)
	shards := make([]shardRef, 0, (len(sorted)+treeFanout-1)/treeFanout)
	for start := 0; start < len(sorted); start += treeFanout {
		end := start + treeFanout
		if end > len(sorted) {
			end = len(sorted)
		}
		chunk := sorted[start:end]
		shardHash, err := tb.storeFlatTree(chunk)
		if err != nil {
			return "", err
		}
		shards = append(shards, shardRef{Key: chunk[0].Name, Hash: shardHash})
	}

	data, hash, err := encodeShardedTree(shards)
	if err != nil {
		return "", err
	}
	if err := tb.storeTreeObject(data, hash); err != nil {
		return "", err
	}
	return hash, nil
}

// storeTreeObject writes a tree object unless it was already written in this
// build.
func (tb *treeBuilder) storeTreeObject(data []byte, hash string) error {
	if _, ok := tb.written[hash]; ok {
		return nil
	}
	if _, err := tb.store.PutData(data); err != nil {
		return err
	}
	tb.written[hash] = struct{}{}
	return nil
}

// expandTree flattens a tree into path -> flatFile, also returning every tree
// hash visited (including the root). hashOf resolves a tree hash to its bytes;
// a missing or corrupt tree is an error (fail-closed), never a silently partial
// listing. GC uses the tree-hash set to keep subtrees reachable from retained
// snapshots.
func expandTree(s *ObjectStore, rootHash string, hashOf func(string) ([]byte, error)) (map[string]flatFile, map[string]struct{}, error) {
	out := make(map[string]flatFile)
	trees := make(map[string]struct{})
	if err := walkTree(s, rootHash, "", out, trees, hashOf); err != nil {
		return nil, nil, err
	}
	return out, trees, nil
}

// walkTree expands one tree node depth-first. The resolver's error is wrapped
// with the tree hash but otherwise preserved, so a caller can still tell a
// missing object (fs.ErrNotExist) from a corrupt one (ErrCorruptObject) instead
// of receiving a flattened "not found" for both.
func walkTree(s *ObjectStore, treeHash, prefix string, out map[string]flatFile, trees map[string]struct{}, hashOf func(string) ([]byte, error)) error {
	data, err := hashOf(treeHash)
	if err != nil {
		return fmt.Errorf("failed to read tree %s: %w", treeHash, err)
	}
	trees[treeHash] = struct{}{}
	entries, shards, err := decodeTree(data)
	if err != nil {
		return fmt.Errorf("%w: tree %s: %v", ErrCorruptObject, treeHash, err)
	}

	// A sharded directory holds no entries of its own; each shard is a flat
	// tree of children at this same level, so expand them all at this prefix.
	for _, shard := range shards {
		if err := walkTree(s, shard.Hash, prefix, out, trees, hashOf); err != nil {
			return err
		}
	}

	for _, e := range entries {
		full := e.Name
		if prefix != "" {
			full = prefix + "/" + e.Name
		}
		if e.Kind == kindTree {
			if err := walkTree(s, e.Hash, full, out, trees, hashOf); err != nil {
				return err
			}
			continue
		}
		out[full] = flatFile{hash: e.Hash, exec: e.Exec}
	}
	return nil
}
