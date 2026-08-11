package util

import "math/rand/v2"

// Character sets for RandomString and related helpers.
const (
	CharsetUpper = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	CharsetLower = "abcdefghijklmnopqrstuvwxyz"
	CharsetAlpha = CharsetUpper + CharsetLower
	CharsetDigit = "0123456789"
	CharsetAlnum = CharsetAlpha + CharsetDigit
	CharsetSym   = "`" + `~!@#$%^&*()-_+={}[]|\;:"<>,./?`
	CharsetHexLo = CharsetDigit + "abcdef"
	CharsetHexUp = CharsetDigit + "ABCDEF"
)

// RandomString returns a random string of the given length using characters
// drawn from the concatenation of charsets. If no charsets are given,
// CharsetAlnum is used. Returns "" if length <= 0.
//
// Uses math/rand/v2 top-level functions, which are safe for concurrent use by
// multiple goroutines and auto-seeded from the OS entropy source. Output is
// NOT cryptographically secure; for security-sensitive tokens use SecureToken.
func RandomString(length int, charsets ...string) string {
	if length <= 0 {
		return ""
	}
	charset := CharsetAlnum
	if len(charsets) > 0 {
		var combined string
		for _, c := range charsets {
			combined += c
		}
		if combined != "" {
			charset = combined
		}
	}
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.IntN(len(charset))]
	}
	return string(b)
}

// RandomBytes returns n non-cryptographic random bytes. Returns nil if n <= 0.
// Output is NOT cryptographically secure.
func RandomBytes(n int) []byte {
	if n <= 0 {
		return nil
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rand.IntN(256))
	}
	return b
}

// RandomIntN returns a non-negative pseudo-random int in [0, n). Panics if
// n <= 0.
func RandomIntN(n int) int { return rand.IntN(n) }

// RandomInt64 returns a non-negative pseudo-random int64.
func RandomInt64() int64 { return rand.Int64() }

// RandomHexLower returns a random lowercase-hex string of the given length.
func RandomHexLower(length int) string { return RandomString(length, CharsetHexLo) }

// RandomHexUpper returns a random uppercase-hex string of the given length.
func RandomHexUpper(length int) string { return RandomString(length, CharsetHexUp) }

// RandomLower returns a random lowercase-letter string of the given length.
func RandomLower(length int) string { return RandomString(length, CharsetLower) }

// RandomUpper returns a random uppercase-letter string of the given length.
func RandomUpper(length int) string { return RandomString(length, CharsetUpper) }

// RandomAlpha returns a random alphabetic (mixed-case) string of the given
// length.
func RandomAlpha(length int) string { return RandomString(length, CharsetAlpha) }
