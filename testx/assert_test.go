package testx

import (
	"errors"
	"testing"
)

func TestAssert_Basic(t *testing.T) {
	a := New(t)

	a.True(true, "True failed")
	a.False(false, "False failed")
	a.Equal(1, 1, "Equal failed")
	a.NotEqual(1, 2, "NotEqual failed")
	a.EqualValues(int(1), int64(1), "EqualValues failed")
	a.NoError(nil, "NoError failed")
	a.TtNoError(nil, "TtNoError failed")
	a.Error(errors.New("test error"), "Error failed")
	a.Contains("hello world", "world", "Contains failed")
}

func TestAssert_NilFamily(t *testing.T) {
	a := New(t)

	a.Nil(nil, "nil should be nil")
	a.Nil((*int)(nil), "typed nil should be Nil")

	a.NotNil(1, "1 should be NotNil")
	a.NotNil("x", "string should be NotNil")
	a.NotNil([]int{}, "non-nil empty slice should be NotNil")
}

func TestAssert_EmptyFamily(t *testing.T) {
	a := New(t)

	a.Empty("", "empty string")
	a.Empty([]int{}, "empty slice")
	a.Empty([]int(nil), "nil slice")
	a.Empty(map[string]int{}, "empty map")
	a.Empty(0, "zero int is empty")
	a.Empty(false, "false is empty")
	a.Empty((*int)(nil), "nil pointer is empty")

	a.NotEmpty("x", "non-empty string")
	a.NotEmpty([]int{1}, "non-empty slice")
	a.NotEmpty(1, "non-zero int")
	a.NotEmpty(true, "true is not empty")
}

func TestAssert_ZeroFamily(t *testing.T) {
	a := New(t)

	a.Zero(0, "int zero")
	a.Zero("", "string zero")
	a.Zero(false, "bool zero")
	a.Zero(nil, "nil is zero")

	a.NotZero(1, "int non-zero")
	a.NotZero("x", "string non-zero")
	a.NotZero(true, "bool non-zero")
}

func TestAssert_DEqual(t *testing.T) {
	a := New(t)

	a.DEqual([]int{1, 2, 3}, []int{1, 2, 3}, "slices deep equal")
	a.DEqual(map[string]int{"a": 1}, map[string]int{"a": 1}, "maps deep equal")
	a.DEqual(1, 1, "ints deep equal")
}

func TestAssert_IsType(t *testing.T) {
	a := New(t)

	a.IsType("int", 11)
	a.IsType("f64", 0.1)
	a.IsType("string", "hello")
	a.IsType("nil", nil)
	a.IsType("[]byte", []byte("x"))
	a.IsType("[]uint8", []uint8("x")) // alias for []byte
}

// TestAssert_BM is a smoke test ensuring the BM helper signature compiles.
// Actual benchmark behavior is validated by BenchmarkBM_Smoke below.
func TestAssert_BM(t *testing.T) {
	var _ func(*testing.B, func()) = BM
}

func BenchmarkBM_Smoke(b *testing.B) {
	BM(b, func() { _ = 1 + 1 })
}

func TestCheckEquals_SliceNoPanic(t *testing.T) {
	a := New(t)
	a.CheckEquals([]int{1, 2, 3}, []int{1, 2, 3}, "slice should be equal")
	a.CheckEquals(map[string]int{"a": 1}, map[string]int{"a": 1}, "map should be equal")
}

func TestNewAssert_Legacy(t *testing.T) {
	a := NewAssert(t)
	a.True(true, "NewAssert should work like New")
}
