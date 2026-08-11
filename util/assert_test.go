package util

import (
	"errors"
	"testing"
)

func TestAssert_Basic(t *testing.T) {
	// Verify the deprecated compatibility shims still work.
	a := NewAssert(t)

	a.True(true, "True failed")
	a.False(false, "False failed")
	a.Equal(1, 1, "Equal failed")
	a.NotEqual(1, 2, "NotEqual failed")
	a.EqualValues(int(1), int64(1), "EqualValues failed")
	a.TtNoError(nil, "TtNoError failed")
	a.Error(errors.New("test error"), "Error failed")
	a.Contains("hello world", "world", "Contains failed")
}

func TestCheckEquals_SliceNoPanic(t *testing.T) {
	a := NewAssert(t)
	a.CheckEquals([]int{1, 2, 3}, []int{1, 2, 3}, "slice should be equal")
	a.CheckEquals(map[string]int{"a": 1}, map[string]int{"a": 1}, "map should be equal")
}
