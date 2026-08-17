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

// Snapshot is an immutable file-tree state: a map of relative path -> object
// hash, linked to a parent snapshot to form a chain.
type Snapshot struct {
	ID        string            `json:"id"`
	ParentID  string            `json:"parent_id"`
	Timestamp time.Time         `json:"timestamp"`
	Message   string            `json:"message"`
	Files     map[string]string `json:"files"`
	Metadata  map[string]string `json:"metadata"`
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

	paths := make([]string, 0, len(snap.Files))
	for p := range snap.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		writeStr(p)
		writeStr(snap.Files[p])
	}

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
