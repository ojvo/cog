package vcs

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"time"
)

// DiffStatus is the kind of change between two snapshots for a single path.
type DiffStatus string

const (
	DiffAdded    DiffStatus = "added"
	DiffModified DiffStatus = "modified"
	DiffDeleted  DiffStatus = "deleted"
)

// FileDiff describes how a single path changed between two snapshots.
type FileDiff struct {
	Status  DiffStatus
	OldHash string
	NewHash string
}

// Snapshot is an immutable file-tree state. The tree itself is stored as
// content-addressed tree objects (see tree.go); the snapshot only references
// the root tree hash, so a snapshot file stays O(1) regardless of work-tree
// size and unchanged directories are shared across snapshots.
type Snapshot struct {
	ID        string            `json:"id"`
	ParentID  string            `json:"parent_id"`
	Timestamp time.Time         `json:"timestamp"`
	Message   string            `json:"message"`
	Tree      string            `json:"tree"`
	Metadata  map[string]string `json:"metadata"`

	// files is the expanded path -> {hash, exec} table, populated on read (by
	// the Manager, which has the object store) and never serialized. It is a
	// convenience for callers that need the flat view; the authoritative form
	// is Tree. trees is the set of tree objects the expansion visited, used by
	// GC to keep subtrees reachable.
	files map[string]flatFile
	trees map[string]struct{}
}

// computeSnapshotID derives a content-addressed ID from a snapshot's payload.
// Fields are length-prefixed so that field concatenation cannot collide (e.g.
// ParentID=""+Message="<64hex>" vs ParentID="<64hex>"+Message=""). Map keys are
// sorted so the ID is independent of map iteration order.
func computeSnapshotID(snap *Snapshot) string {
	h := sha256.New()
	writeStr := func(s string) {
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], uint32(len(s)))
		h.Write(buf[:])
		h.Write([]byte(s))
	}
	writeStr(snap.ParentID)
	writeStr(snap.Timestamp.UTC().Format(time.RFC3339Nano))
	writeStr(snap.Message)
	writeStr(snap.Tree)

	if snap.Metadata != nil {
		keys := make([]string, 0, len(snap.Metadata))
		for k := range snap.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			writeStr(k)
			writeStr(snap.Metadata[k])
		}
	}

	return hex.EncodeToString(h.Sum(nil))
}
