package util

import (
	"errors"
	"testing"
)

func TestAssert_Basic(t *testing.T) {
	// We can't easily test failure cases because they call t.Fail(), which fails the current test.
	// But we can test passing cases.
	
	a := NewAssert(t)
	
	a.True(true, "True failed")
	a.False(false, "False failed")
	a.Equal(1, 1, "Equal failed")
	a.NotEqual(1, 2, "NotEqual failed")
	a.EqualValues(int(1), int64(1), "EqualValues failed") // int vs int64
	a.TtNoError(nil, "TtNoError failed")
	a.Error(errors.New("test error"), "Error failed")
	a.Contains("hello world", "world", "Contains failed")
}
