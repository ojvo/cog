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
