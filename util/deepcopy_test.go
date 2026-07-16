package util

import (
	"testing"
)

func TestDeepCopyNil(t *testing.T) {
	var s *string
	if result := DeepCopy(s); result != nil {
		t.Fatal("expected nil for nil input")
	}
}

func TestDeepCopyBasic(t *testing.T) {
	type Person struct {
		Name string
		Age  int
	}
	original := &Person{Name: "Alice", Age: 30}
	cloned := DeepCopy(original)

	if cloned.Name != original.Name || cloned.Age != original.Age {
		t.Fatal("cloned should have same values")
	}

	cloned.Name = "Bob"
	if original.Name == "Bob" {
		t.Fatal("original should not be affected by cloned modification")
	}
}

func TestDeepCopyNested(t *testing.T) {
	type Inner struct {
		Value string
	}
	type Outer struct {
		Name  string
		Inner *Inner
	}

	original := &Outer{Name: "outer", Inner: &Inner{Value: "inner"}}
	cloned := DeepCopy(original)

	if cloned.Inner.Value != "inner" {
		t.Fatal("nested struct should be copied")
	}

	cloned.Inner.Value = "modified"
	if original.Inner.Value == "modified" {
		t.Fatal("original nested struct should not be affected")
	}
}

func TestDeepCopySlice(t *testing.T) {
	type WithSlice struct {
		Items []string
	}

	original := &WithSlice{Items: []string{"a", "b", "c"}}
	cloned := DeepCopy(original)

	if len(cloned.Items) != 3 {
		t.Fatal("slice should be copied")
	}

	cloned.Items[0] = "x"
	if original.Items[0] == "x" {
		t.Fatal("original slice should not be affected")
	}
}
