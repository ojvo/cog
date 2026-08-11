package util

import (
	"strings"
	"testing"

	"ojv/cog/testx"
)

func TestRandomStringLength(t *testing.T) {
	for _, n := range []int{0, 1, 5, 32, 100} {
		s := RandomString(n)
		testx.Equal(t, n, len(s))
	}
	testx.Equal(t, "", RandomString(-1))
}

func TestRandomStringCharset(t *testing.T) {
	s := RandomString(100, CharsetLower)
	testx.Equal(t, 100, len(s))
	for _, c := range s {
		testx.True(t, c >= 'a' && c <= 'z')
	}
}

func TestRandomStringMultipleCharsets(t *testing.T) {
	s := RandomString(100, CharsetUpper, CharsetDigit)
	for _, c := range s {
		isUpper := c >= 'A' && c <= 'Z'
		isDigit := c >= '0' && c <= '9'
		testx.True(t, isUpper || isDigit)
	}
}

func TestRandomStringDefaultCharset(t *testing.T) {
	s := RandomString(200)
	for _, c := range s {
		isUpper := c >= 'A' && c <= 'Z'
		isLower := c >= 'a' && c <= 'z'
		isDigit := c >= '0' && c <= '9'
		testx.True(t, isUpper || isLower || isDigit)
	}
}

func TestRandomStringUniqueness(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		s := RandomString(16)
		testx.False(t, seen[s], "duplicate random string")
		seen[s] = true
	}
}

func TestRandomBytes(t *testing.T) {
	testx.Nil(t, RandomBytes(0))
	testx.Nil(t, RandomBytes(-1))
	b := RandomBytes(32)
	testx.Equal(t, 32, len(b))
	// Statistically extremely unlikely for all bytes to be 0.
	nonZero := false
	for _, x := range b {
		if x != 0 {
			nonZero = true
			break
		}
	}
	testx.True(t, nonZero, "random bytes should not be all zeros")
}

func TestRandomIntN(t *testing.T) {
	for i := 0; i < 100; i++ {
		v := RandomIntN(10)
		testx.True(t, v >= 0 && v < 10)
	}
	// Range check with a larger bound.
	for i := 0; i < 100; i++ {
		v := RandomIntN(1000)
		testx.True(t, v >= 0 && v < 1000)
	}
}

func TestRandomInt64(t *testing.T) {
	v := RandomInt64()
	// Should be non-negative (Int64 returns [0, 1<<63)).
	testx.True(t, v >= 0)
}

func TestRandomHexLower(t *testing.T) {
	s := RandomHexLower(20)
	testx.Equal(t, 20, len(s))
	for _, c := range s {
		isDigit := c >= '0' && c <= '9'
		isLowerHex := c >= 'a' && c <= 'f'
		testx.True(t, isDigit || isLowerHex)
	}
}

func TestRandomHexUpper(t *testing.T) {
	s := RandomHexUpper(20)
	testx.Equal(t, 20, len(s))
	for _, c := range s {
		isDigit := c >= '0' && c <= '9'
		isUpperHex := c >= 'A' && c <= 'F'
		testx.True(t, isDigit || isUpperHex)
	}
}

func TestRandomLower(t *testing.T) {
	s := RandomLower(50)
	testx.Equal(t, 50, len(s))
	testx.True(t, s == strings.ToLower(s))
}

func TestRandomUpper(t *testing.T) {
	s := RandomUpper(50)
	testx.Equal(t, 50, len(s))
	testx.True(t, s == strings.ToUpper(s))
}

func TestRandomAlpha(t *testing.T) {
	s := RandomAlpha(50)
	testx.Equal(t, 50, len(s))
	for _, c := range s {
		isUpper := c >= 'A' && c <= 'Z'
		isLower := c >= 'a' && c <= 'z'
		testx.True(t, isUpper || isLower)
	}
}

func TestCharsetsAreNotEmpty(t *testing.T) {
	for _, c := range []string{CharsetUpper, CharsetLower, CharsetAlpha, CharsetDigit, CharsetAlnum, CharsetSym, CharsetHexLo, CharsetHexUp} {
		testx.True(t, len(c) > 0)
	}
}
