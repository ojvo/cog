// Package vcs provides a content-addressed file-tree version control
// primitive. It is a self-contained, dependency-light implementation of:
//
//   - ObjectStore: SHA-256 content-addressed object storage with natural
//     deduplication and integrity verification.
//   - Snapshot: immutable file-tree state values addressed by a length-prefixed
//     content hash, forming a parent-linked chain.
//   - Manager: transactional commit/checkout/rollback over a work tree, with
//     pre-check, backup and rollback guarantees, plus garbage collection that
//     refuses to run when the snapshot chain is inconsistent.
//
// This package intentionally carries no business semantics: no task state, no
// session memory, no product directory layout. Callers pass a state directory
// (VCS data lives under its vcs/ subdirectory) explicitly and may attach
// arbitrary string metadata to snapshots.
//
// It depends only on the standard library and ojv/cog/store for atomic writes.
package vcs
