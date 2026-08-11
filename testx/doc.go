// Package testx provides testing extensions: fluent assertions,
// package-level assertions, type-safe generic assertions, benchmark
// helpers, and a test-suite runner.
//
// It is a lightweight, zero-dependency alternative to testify/assert,
// testify/suite, and testify/require, designed for projects that prefer
// not to pull in large third-party test frameworks.
//
// # Three API styles
//
// All three styles can be mixed in the same test file. Choose per call site
// based on what reads best:
//
//  1. Fluent: build an *Assert once and chain multiple checks.
//     a := testx.New(t)
//     a.Equal(1, 1).NotNil(x)
//  2. Package-level: stateless top-level functions, useful when the test
//     only needs a single assertion or prefers the testify-style signature.
//     testx.Equal(t, 1, 1)
//     testx.Nil(t, x)
//  3. Generic: compile-time type-safe equality for comparable values,
//     avoiding reflection and boxing overhead.
//     testx.EqualT(t, 2, Add(1, 1))
//
// # Benchmark helper
//
// BM runs a closure b.N times, replacing the canonical
// `for i := 0; i < b.N; i++ { fn() }` loop:
//
//	func BenchmarkAdd(b *testing.B) {
//	    testx.BM(b, func() { Add(1, 1) })
//	}
//
// # Test suites
//
// Embed TestSuite and define SetupTest/TearDownTest methods; RunSuite
// invokes every TestXxx method on the suite with setup/teardown around
// each, mirroring testify/suite.Suite:
//
//	type MySuite struct{ testx.TestSuite }
//	func (s *MySuite) SetupTest()    { /* ... */ }
//	func (s *MySuite) TestThing()    { /* ... */ }
//	func TestAll(t *testing.T)       { testx.RunSuite(t, &MySuite{}) }
//
// # TestingT interface
//
// Package-level assertions accept the minimal TestingT interface
// (Errorf(format, args...any)), so they can be used with any test driver
// that satisfies it - not just *testing.T.
package testx
