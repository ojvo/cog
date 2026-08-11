package util

import (
	"strings"
	"testing"
)

func TestCaller_GetCallerInfo(t *testing.T) {
	info := GetCallerInfo(0)

	if info.Function == "unknown" {
		t.Error("GetCallerInfo returned unknown function")
	}

	if !strings.HasSuffix(info.File, "caller_test.go") {
		t.Errorf("File = %s, expected caller_test.go suffix", info.File)
	}

	// The function name should be TestCaller_GetCallerInfo or similar
	if !strings.Contains(info.Function, "TestCaller_GetCallerInfo") {
		t.Errorf("Function = %s, expected to contain TestCaller_GetCallerInfo", info.Function)
	}
}

func TestCaller_GetCallerInfoString(t *testing.T) {
	str := GetCallerInfoString(0)
	if str == "" {
		t.Error("GetCallerInfoString returned empty")
	}
	t.Logf("Caller info: %s", str)
}

func TestCaller_GetCallerStack_Basic(t *testing.T) {
	frames := GetCallerStack(0, 3)
	if len(frames) == 0 {
		t.Fatal("GetCallerStack returned no frames")
	}
	if len(frames) > 3 {
		t.Errorf("len(frames) = %d, want <= 3", len(frames))
	}

	// Innermost frame should be this test function.
	first := frames[0]
	if !strings.HasSuffix(first.File, "caller_test.go") {
		t.Errorf("first File = %s, want caller_test.go suffix", first.File)
	}
	if !strings.Contains(first.Function, "TestCaller_GetCallerStack_Basic") {
		t.Errorf("first Function = %s, want to contain TestCaller_GetCallerStack_Basic", first.Function)
	}

	// A deeper frame should differ from the innermost.
	if len(frames) >= 2 {
		if frames[1].Function == first.Function {
			t.Error("second frame Function equals first; expected a different caller")
		}
	}
}

func TestCaller_GetCallerStack_NonPositiveDepth(t *testing.T) {
	if got := GetCallerStack(0, 0); got != nil {
		t.Errorf("GetCallerStack(0, 0) = %v, want nil", got)
	}
	if got := GetCallerStack(0, -1); got != nil {
		t.Errorf("GetCallerStack(0, -1) = %v, want nil", got)
	}
}

func TestCaller_GetCallerStack_Skip(t *testing.T) {
	// skip=0 from this frame: innermost is this test.
	frames0 := GetCallerStack(0, 1)
	if len(frames0) != 1 {
		t.Fatalf("len(frames0) = %d, want 1", len(frames0))
	}
	if !strings.Contains(frames0[0].Function, "TestCaller_GetCallerStack_Skip") {
		t.Errorf("frames0[0].Function = %s, want TestCaller_GetCallerStack_Skip", frames0[0].Function)
	}

	// skip=1 from this frame: innermost should be testing.tRunner or similar.
	frames1 := GetCallerStack(1, 1)
	if len(frames1) != 1 {
		t.Fatalf("len(frames1) = %d, want 1", len(frames1))
	}
	if frames1[0].Function == frames0[0].Function {
		t.Errorf("skip=1 should skip this test func, got same Function %q", frames1[0].Function)
	}
}

func TestCaller_GetCallerStack_DeepExhaustion(t *testing.T) {
	// A very large maxDepth must terminate gracefully without panicking and
	// return only the frames that actually exist.
	frames := GetCallerStack(0, 1000)
	if len(frames) == 0 {
		t.Fatal("expected at least one frame")
	}
	if len(frames) > 1000 {
		t.Errorf("len(frames) = %d, want <= 1000", len(frames))
	}
}
