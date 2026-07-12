package util

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"io"
)

// GUID generates a random 32-character hex string (128-bit).
// Example: 4725f5ae6a350b1c45687c9934456e6f
func GUID() string {
	var b [16]byte
	_, _ = io.ReadFull(rand.Reader, b[:])
	return hex.EncodeToString(b[:])
}

// UUID generates a standard UUID v4 string.
// Example: 550e8400-e29b-41d4-a716-446655440000
func UUID() string {
	var b [16]byte
	_, _ = io.ReadFull(rand.Reader, b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant 10xx

	buf := make([]byte, 36)
	hex.Encode(buf[0:8], b[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], b[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], b[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], b[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:], b[10:])

	return string(buf)
}

// Md5Signer returns the MD5 hex digest of the given message.
func Md5Signer(message string) string {
	hash := md5.Sum([]byte(message))
	return hex.EncodeToString(hash[:])
}
