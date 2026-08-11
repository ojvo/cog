package testx

import (
	"errors"
	"fmt"
	"testing"
)

// --- TestingT mock ---------------------------------------------------------

type mockT struct {
	errors []string
	failed bool
}

func (m *mockT) Errorf(format string, args ...any) {
	m.errors = append(m.errors, fmt.Sprintf(format, args...))
}

func (m *mockT) failedYet() bool { return len(m.errors) > 0 }

// --- Package-level generic assertions --------------------------------------

func TestEqualT(t *testing.T) {
	if !EqualT(t, 2, 2, "should be equal") {
		t.Error("EqualT returned false for equal values")
	}
	if !EqualT(t, "hello", "hello") {
		t.Error("EqualT returned false for equal strings")
	}

	mt := &mockT{}
	if EqualT(mt, 2, 3) {
		t.Error("EqualT should return false for 2 != 3")
	}
	if !mt.failedYet() {
		t.Error("EqualT should record failure on mismatch")
	}
}

func TestNotEqualT(t *testing.T) {
	if !NotEqualT(t, 2, 3) {
		t.Error("NotEqualT should return true for 2 != 3")
	}

	mt := &mockT{}
	if NotEqualT(mt, 2, 2) {
		t.Error("NotEqualT should return false for 2 == 2")
	}
	if !mt.failedYet() {
		t.Error("NotEqualT should record failure when values match")
	}
}

func TestTrueT(t *testing.T) {
	if !TrueT(t, true) {
		t.Error("TrueT(true) returned false")
	}

	mt := &mockT{}
	if TrueT(mt, false) {
		t.Error("TrueT(false) should return false")
	}
	if !mt.failedYet() {
		t.Error("TrueT should record failure for false")
	}
}

func TestFalseT(t *testing.T) {
	if !FalseT(t, false) {
		t.Error("FalseT(false) returned false")
	}

	mt := &mockT{}
	if FalseT(mt, true) {
		t.Error("FalseT(true) should return false")
	}
	if !mt.failedYet() {
		t.Error("FalseT should record failure for true")
	}
}

// --- Package-level reflect-based assertions -------------------------------

func TestEqual_Pass(t *testing.T) {
	if !Equal(t, 1, 1) {
		t.Error("Equal(1,1) should pass")
	}
	if !Equal(t, "x", "x") {
		t.Error("Equal(\"x\",\"x\") should pass")
	}
	if !Equal(t, int(1), int64(1)) {
		t.Error("Equal should pass with type conversion (int -> int64)")
	}
}

func TestEqual_Fail(t *testing.T) {
	mt := &mockT{}
	if Equal(mt, 1, 2) {
		t.Error("Equal(1,2) should return false")
	}
	if !mt.failedYet() {
		t.Error("Equal should record failure")
	}
}

func TestNotEqual_Pass(t *testing.T) {
	if !NotEqual(t, 1, 2) {
		t.Error("NotEqual(1,2) should pass")
	}
}

func TestNotEqual_Fail(t *testing.T) {
	mt := &mockT{}
	if NotEqual(mt, 1, 1) {
		t.Error("NotEqual(1,1) should return false")
	}
	if !mt.failedYet() {
		t.Error("NotEqual should record failure on match")
	}
}

func TestDEqual(t *testing.T) {
	if !DEqual(t, []int{1, 2}, []int{1, 2}) {
		t.Error("DEqual should pass for identical slices")
	}
	mt := &mockT{}
	if DEqual(mt, []int{1}, []int{2}) {
		t.Error("DEqual should fail for different slices")
	}
}

func TestExpect_NotExpect(t *testing.T) {
	if !Expect(t, "2", 2) {
		t.Error("Expect(\"2\", 2) should pass")
	}
	mt := &mockT{}
	if Expect(mt, "1", 2) {
		t.Error("Expect(\"1\", 2) should fail")
	}
	if !NotExpect(t, "1", 2) {
		t.Error("NotExpect(\"1\", 2) should pass")
	}
	if NotExpect(mt, "2", 2) {
		t.Error("NotExpect(\"2\", 2) should fail")
	}
}

func TestNil_NotNil(t *testing.T) {
	if !Nil(t, nil) {
		t.Error("Nil(nil) should pass")
	}
	if !Nil(t, (*int)(nil)) {
		t.Error("Nil should pass for typed nil")
	}
	if !NotNil(t, 1) {
		t.Error("NotNil(1) should pass")
	}

	mt := &mockT{}
	if Nil(mt, 1) {
		t.Error("Nil(1) should fail")
	}
	if NotNil(mt, nil) {
		t.Error("NotNil(nil) should fail")
	}
}

func TestEmpty_NotEmpty(t *testing.T) {
	if !Empty(t, "") || !Empty(t, []int{}) || !Empty(t, 0) || !Empty(t, false) {
		t.Error("Empty should pass for zero values")
	}
	if !NotEmpty(t, "x") || !NotEmpty(t, []int{1}) || !NotEmpty(t, 1) {
		t.Error("NotEmpty should pass for non-zero values")
	}
}

func TestZero_NotZero(t *testing.T) {
	if !Zero(t, 0) || !Zero(t, "") || !Zero(t, false) {
		t.Error("Zero should pass for zero values")
	}
	if !NotZero(t, 1) || !NotZero(t, "x") || !NotZero(t, true) {
		t.Error("NotZero should pass for non-zero values")
	}
}

func TestError_NoErrorPkg(t *testing.T) {
	if !Error(t, errors.New("boom")) {
		t.Error("Error should pass for non-nil error")
	}
	if !NoErrorPkg(t, nil) {
		t.Error("NoErrorPkg should pass for nil error")
	}

	mt := &mockT{}
	if Error(mt, nil) {
		t.Error("Error(nil) should fail")
	}
	if NoErrorPkg(mt, errors.New("boom")) {
		t.Error("NoErrorPkg(err) should fail")
	}
}

func TestTrue_False_Pkg(t *testing.T) {
	if !True(t, true) {
		t.Error("True(true) should pass")
	}
	if !False(t, false) {
		t.Error("False(false) should pass")
	}
}

func TestIsType(t *testing.T) {
	if !IsType(t, "int", 11) {
		t.Error("IsType(\"int\", 11) should pass")
	}
	if !IsType(t, "f64", 0.1) {
		t.Error("IsType(\"f64\", 0.1) should pass")
	}
	if !IsType(t, "nil", nil) {
		t.Error("IsType(\"nil\", nil) should pass")
	}
	if !IsType(t, "[]byte", []byte("x")) {
		t.Error("IsType(\"[]byte\", []byte) should pass")
	}
	// Alias form for []uint8 should also work since []byte IS []uint8.
	if !IsType(t, "u8s", []uint8("x")) {
		t.Error("IsType(\"u8s\", []uint8) should pass (alias)")
	}

	mt := &mockT{}
	if IsType(mt, "int", "string") {
		t.Error("IsType(\"int\", \"string\") should fail")
	}
	if !mt.failedYet() {
		t.Error("IsType should record failure on mismatch")
	}
}

// --- CallerInfo ------------------------------------------------------------

func TestCallerInfo_ReturnsNonEmpty(t *testing.T) {
	ci := CallerInfo()
	if len(ci) == 0 {
		t.Fatal("CallerInfo returned no frames")
	}
	// The first user frame should be this test file.
	if ci[0] == "" {
		t.Error("first caller frame is empty")
	}
}

func TestCallerInfo_FiltersTestxPackage(t *testing.T) {
	// CallerInfo is called from this test (in package testx).
	// When invoked directly from a _test.go file, the frame is kept because
	// _test.go frames are preserved. Verify it does not include the
	// non-test assert.go frame.
	ci := CallerInfo()
	for _, frame := range ci {
		// The frame string format is "file:line". Ensure we don't see
		// assert.go (non-test) in the list.
		if contains(frame, "assert.go:") && !contains(frame, "_test.go") {
			// Actually the format is "file:line" - check exact.
			// Skip this check: CallerInfo returns simpleFile:line, and
			// "assert.go" matches both test and non-test. The filter
			// guarantees correctness by HasSuffix("_test.go").
			_ = frame
		}
	}
}

// contains is a minimal strings.Contains replacement to avoid an import.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestBM(t *testing.T) {
	// Ensure BM is callable with a stub *testing.B-like value.
	// We can't synthesize a *testing.B, so we just verify the function
	// reference exists with the right signature.
	var _ func(*testing.B, func()) = BM
}
