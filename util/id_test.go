package util

import (
	"regexp"
	"testing"
)

func TestID_GUID(t *testing.T) {
	g := GUID()
	if len(g) != 32 {
		t.Fatalf("GUID() length = %d, want 32", len(g))
	}
	// Must be valid hex
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(g) {
		t.Errorf("GUID() = %s, want 32 hex chars", g)
	}
	// Uniqueness (probabilistic)
	g2 := GUID()
	if g == g2 {
		t.Error("GUID() generated same value twice (extremely unlikely)")
	}
}

func TestID_UUID(t *testing.T) {
	u := UUID()
	if len(u) != 36 {
		t.Fatalf("UUID() length = %d, want 36", len(u))
	}
	// UUID v4 format: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(u) {
		t.Errorf("UUID() = %s, want valid v4 format", u)
	}
	// Version digit must be 4
	if u[14] != '4' {
		t.Errorf("UUID version = %c, want 4", u[14])
	}
	// Variant digit must be 8/9/a/b
	v := u[19]
	if v != '8' && v != '9' && v != 'a' && v != 'b' {
		t.Errorf("UUID variant = %c, want 8/9/a/b", v)
	}
}

func TestID_Md5Signer(t *testing.T) {
	// Known MD5: md5("hello") = 5d41402abc4b2a76b9719d911017c592
	got := Md5Signer("hello")
	if got != "5d41402abc4b2a76b9719d911017c592" {
		t.Errorf("Md5Signer(\"hello\") = %s, want known value", got)
	}
	// Empty string
	if Md5Signer("") != "d41d8cd98f00b204e9800998ecf8427e" {
		t.Errorf("Md5Signer(\"\") = %s", Md5Signer(""))
	}
}
