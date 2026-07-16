package util

import (
	"strings"
	"testing"
)

func TestJSONFile_RoundTrip(t *testing.T) {
	type cfg struct {
		Name string `json:"name"`
		Port int    `json:"port"`
	}
	dir := t.TempDir()
	path := JoinPath(dir, "cfg.json")
	in := cfg{Name: "demo", Port: 8080}
	if err := SaveJSONFile(path, in); err != nil {
		t.Fatal(err)
	}
	var out cfg
	if err := LoadJSONFile(path, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("round trip = %#v, want %#v", out, in)
	}
}

func TestJSONFile_Pretty(t *testing.T) {
	type cfg struct {
		Name string `json:"name"`
	}
	dir := t.TempDir()
	path := JoinPath(dir, "pretty.json")
	if err := SaveJSONPrettyFile(path, cfg{Name: "demo"}); err != nil {
		t.Fatal(err)
	}
	s, err := ReadString(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "\n") || !strings.Contains(s, "    ") {
		t.Fatalf("pretty json not formatted: %q", s)
	}
}

func TestJSONFile_LoadMissing(t *testing.T) {
	var x map[string]any
	if err := LoadJSONFile(JoinPath(t.TempDir(), "missing.json"), &x); err == nil {
		t.Fatal("expected error")
	}
}
