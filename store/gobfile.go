package store

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"os"
)

// Magic helpers for "magic header + gob payload" file formats.
//
// These functions provide the atomic write + magic-verified read half of the
// common "versioned binary blob" persistence pattern (e.g. agent checkpoint
// files, caches, snapshots). The magic header guards against decoding a file
// written by an unrelated format or an incompatible older version.

// WriteGobWithMagic gob-encodes v into buf with magic prefixed, then writes it
// atomically to path (tmp + fsync + rename, see AtomicWriteFile).
//
// magic is the leading header bytes; on read the exact same bytes must match.
// An empty magic is allowed (payload starts immediately with gob stream).
func WriteGobWithMagic(path string, magic []byte, v any, perm os.FileMode) error {
	var buf bytes.Buffer
	if _, err := buf.Write(magic); err != nil {
		return err
	}
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		return fmt.Errorf("store: gob encode: %w", err)
	}
	return AtomicWriteFile(path, buf.Bytes(), perm)
}

// ErrGobMagic is returned by ReadGobWithMagic when the file exists but its
// leading bytes do not match the expected magic header.
var ErrGobMagic = fmt.Errorf("store: gob magic mismatch")

// ReadGobWithMagic reads path, verifies the leading bytes equal magic, and
// gob-decodes the remainder into v.
//
// The file is opened read-only and never modified. A magic mismatch (e.g. a
// half-written file from a crash, or a file of a different format) returns
// ErrGobMagic so callers can distinguish "no file" (os.ErrNotExist) from
// "file exists but wrong format".
func ReadGobWithMagic(path string, magic []byte, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if len(magic) > 0 {
		head := make([]byte, len(magic))
		if _, err := io.ReadFull(f, head); err != nil {
			return fmt.Errorf("store: read gob header: %w", err)
		}
		if !bytes.Equal(head, magic) {
			return ErrGobMagic
		}
	}
	if err := gob.NewDecoder(f).Decode(v); err != nil {
		return fmt.Errorf("store: gob decode: %w", err)
	}
	return nil
}
