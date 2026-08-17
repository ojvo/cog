package util

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
)

// SecureEqual reports whether a and b match using a constant-time
// comparison. Use this instead of `==` for security-sensitive strings
// (auth tokens, API keys, signatures) to avoid timing side-channels.
// An empty candidate never matches.
func SecureEqual(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// SecureToken generates a 32-char hex cryptographic random token.
//
// Fail-closed: if the host RNG is unavailable (rand.Read fails),
// returns ("", error) rather than a predictable constant.
// A predictable token would defeat any auth layer that uses it.
//
// Callers that need a token at init-time should use MustSecureToken.
func SecureToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("securetoken: rand unavailable: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// MustSecureToken is like SecureToken but panics on error.
// Only use this at init-time or when a token is strictly required
// and the process cannot meaningfully continue without one.
func MustSecureToken() string {
	tok, err := SecureToken()
	if err != nil {
		panic("securetoken: " + err.Error())
	}
	return tok
}

// SecureTokenOrEmpty is like SecureToken but returns "" on error instead of
// the error. This is the fail-closed convenience wrapper for callers that
// treat an empty token as "auth disabled / regenerate on next try" and don't
// want to duplicate the fail-closed policy at every call site.
//
// Use this instead of copy-pasting `tok, err := SecureToken(); if err != nil
// { return "" }; return tok` — that exact snippet was duplicated across
// coki/session and coki/web, violating the "security-sensitive algorithms must
// not be duplicated" governance rule.
func SecureTokenOrEmpty() string {
	tok, err := SecureToken()
	if err != nil {
		return ""
	}
	return tok
}
