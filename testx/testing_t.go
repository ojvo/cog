package testx

import "testing"

// TestingT is the minimum interface required from a test or benchmark driver
// to host assertions. Both *testing.T and *testing.B satisfy it, so the same
// helpers work for unit tests and benchmarks.
type TestingT interface {
	Errorf(format string, args ...any)
}

// TB extends TestingT with the methods needed for FailNow-style assertions.
// *testing.T and *testing.B both satisfy TB. Use this in helpers that need
// to terminate the test (Fatal/FailNow).
type TB interface {
	TestingT
	Helper()
	FailNow()
}

// BM runs fn b.N times as a benchmark body. It is a one-line replacement for
// the canonical `for i := 0; i < b.N; i++ { fn() }` pattern, keeping
// benchmark functions concise and consistent.
func BM(b *testing.B, fn func()) {
	for i := 0; i < b.N; i++ {
		fn()
	}
}
