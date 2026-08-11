package util

import (
	"testing"

	"ojv/cog/testx"
)

// ObjectsAreEqualValues delegates to testx.ObjectsAreEqualValues.
//
// Deprecated: use testx.ObjectsAreEqualValues instead.
func ObjectsAreEqualValues(a, b interface{}) bool {
	return testx.ObjectsAreEqualValues(a, b)
}

// AssertTest is a compatibility alias for testx.Assert.
//
// Deprecated: use testx.Assert instead.
type AssertTest = testx.Assert

// NewAssert creates a new testx.Assert helper.
//
// Deprecated: use testx.New instead.
func NewAssert(t *testing.T) *testx.Assert {
	return testx.New(t)
}

// TestSuite is a compatibility alias for testx.TestSuite.
//
// Deprecated: use testx.TestSuite instead.
type TestSuite = testx.TestSuite
