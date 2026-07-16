package util

import "testing"

func TestValidateID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"task-123", true},
		{"abc_def", true},
		{"ABC123", true},
		{"", false},
		{"a b", false},
		{"a/b", false},
		{"a.b", false},
	}
	for _, tt := range tests {
		got := ValidateID(tt.id)
		if got != tt.want {
			t.Errorf("ValidateID(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}

func TestValidateIDWithLimit(t *testing.T) {
	short := "abc"
	long := "a"
	for i := 0; i < 200; i++ {
		long += "a"
	}

	if !ValidateIDWithLimit(short, 10) {
		t.Error("short ID should be valid")
	}
	if ValidateIDWithLimit(long, 10) {
		t.Error("long ID should be invalid")
	}
}

func TestValidateIDWithLimitZero(t *testing.T) {
	if !ValidateIDWithLimit("abc", 0) {
		t.Error("zero limit should use default, valid ID should pass")
	}
}
