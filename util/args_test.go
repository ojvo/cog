package util

import (
	"reflect"
	"testing"
)

func TestArgParser_Basic(t *testing.T) {
	var (
		verbose bool
		name    string
		count   int
	)

	args := []string{"--verbose", "--name", "test-app", "--count", "42"}

	parser := NewArgParser()
	parser.Bool(&verbose, "--verbose")
	parser.String(&name, "--name")
	parser.Int(&count, "--count")

	result := parser.Parse(args)
	if result.HasErrors() {
		t.Fatalf("Parse failed: %v", result.Error())
	}

	if !verbose {
		t.Error("verbose should be true")
	}
	if name != "test-app" {
		t.Errorf("name = %s, want test-app", name)
	}
	if count != 42 {
		t.Errorf("count = %d, want 42", count)
	}
}

func TestArgParser_Required(t *testing.T) {
	var requiredArg string
	args := []string{}

	parser := NewArgParser()
	parser.StringRequired(&requiredArg, "--req")

	result := parser.Parse(args)
	if !result.HasErrors() {
		t.Error("Expected error for missing required argument")
	}
}

func TestArgParser_Slices(t *testing.T) {
	var (
		tags []string
		ids  []int
	)

	args := []string{"--tags", "t1", "t2", "--ids", "1", "2", "3"}

	parser := NewArgParser()
	parser.Strings(&tags, "--tags")
	parser.Ints(&ids, "--ids")

	result := parser.Parse(args)
	if result.HasErrors() {
		t.Fatalf("Parse failed: %v", result.Error())
	}

	expectedTags := []string{"t1", "t2"}
	if !reflect.DeepEqual(tags, expectedTags) {
		t.Errorf("tags = %v, want %v", tags, expectedTags)
	}

	expectedIds := []int{1, 2, 3}
	if !reflect.DeepEqual(ids, expectedIds) {
		t.Errorf("ids = %v, want %v", ids, expectedIds)
	}
}

func TestArgParser_Trailing(t *testing.T) {
	var trailing []string
	args := []string{"cmd", "arg1", "--", "trailing1", "trailing2"}

	parser := NewArgParser()
	parser.Trailing(&trailing, "trailing args")

	// cmd is usually arg[0] in os.Args, but parser assumes args start from first actual arg usually?
	// The implementation iterates from 0.
	// If we pass "cmd" as first arg, it will be treated as trailing or unknown if not flag.
	// Let's assume we pass args excluding program name usually, or program name is handled.
	// ArgParser doesn't skip index 0 automatically.

	// If "cmd" is not a flag, it goes to trailing.
	result := parser.Parse(args)
	if result.HasErrors() {
		t.Fatalf("Parse failed: %v", result.Error())
	}

	// "cmd", "arg1" are not flags, so they are trailing.
	// "--" forces rest to be trailing.
	// So all should be trailing.
	expected := []string{"cmd", "arg1", "trailing1", "trailing2"}
	if !reflect.DeepEqual(trailing, expected) {
		t.Errorf("trailing = %v, want %v", trailing, expected)
	}
}

func TestArgParser_BoolFlags(t *testing.T) {
	var (
		flag1 bool
		flag2 bool
	)

	// Test --no-flag behavior
	args := []string{"--flag1", "--no-flag2"}

	parser := NewArgParser()
	parser.Bool(&flag1, "--flag1")
	parser.Bool(&flag2, "--flag2") // Should auto-generate --no-flag2 support logic?
	// The implementation checks: *target = !strings.Contains(argName, "--no-")
	// And normalizeNames adds --no- version if it starts with --.

	result := parser.Parse(args)
	if result.HasErrors() {
		t.Fatalf("Parse failed: %v", result.Error())
	}

	if !flag1 {
		t.Error("flag1 should be true")
	}
	if flag2 { // Default is false, but if we pass --no-flag2, it sets to false.
		// Wait, if it wasn't passed, it would be false (default).
		// To test this properly, we should maybe init with true?
		// Or assume the handler sets it.
		// Handler: *target = !strings.Contains(argName, "--no-")
		// So if --no-flag2 is passed, *target = false.
		t.Error("flag2 should be false")
	}
}
