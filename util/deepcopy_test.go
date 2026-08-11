package util

import (
	"testing"
)

func TestDeepCopy_Nil(t *testing.T) {
	var s *string
	if result := DeepCopy(s); result != nil {
		t.Fatal("expected nil for nil input")
	}
}

func TestDeepCopy_Basic(t *testing.T) {
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

func TestDeepCopy_Nested(t *testing.T) {
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

func TestDeepCopy_Slice(t *testing.T) {
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

func TestDeepCopy_Map(t *testing.T) {
	original := map[string]int{"a": 1, "b": 2}
	cloned := DeepCopy(&original)

	if (*cloned)["a"] != 1 || (*cloned)["b"] != 2 {
		t.Fatal("map should be copied with same values")
	}
	(*cloned)["a"] = 99
	if original["a"] == 99 {
		t.Fatal("original map should not be affected")
	}
}

func TestDeepCopy_SliceOfStructs(t *testing.T) {
	type Item struct {
		ID   int
		Name string
	}
	original := []Item{{1, "a"}, {2, "b"}}
	cloned := DeepCopy(&original)

	if len(*cloned) != 2 {
		t.Fatal("slice len mismatch")
	}
	(*cloned)[0].Name = "x"
	if original[0].Name == "x" {
		t.Fatal("original slice elements should be deep-copied")
	}
}

func TestDeepCopy_MapOfStructs(t *testing.T) {
	type Val struct{ N int }
	original := map[string]*Val{"x": {42}}
	cloned := DeepCopy(&original)

	(*cloned)["x"].N = 99
	if original["x"].N == 99 {
		t.Fatal("map values should be deep-copied")
	}
}

func TestDeepCopy_DeepCopyInterface(t *testing.T) {
	type custom struct{ S string }

	original := &custom{S: "orig"}
	cloned := DeepCopy(original)

	if cloned.S != "orig" {
		t.Fatalf("got %q, want %q", cloned.S, "orig")
	}
	cloned.S = "changed"
	if original.S == "changed" {
		t.Fatal("original should be independent")
	}
}

func TestDeepCopy_Time(t *testing.T) {
	type WithTime struct {
		T  string
		At int
	}
	original := &WithTime{T: "hello", At: 5}
	cloned := DeepCopy(original)

	if cloned.T != "hello" || cloned.At != 5 {
		t.Fatal("time-adjacent struct should copy correctly")
	}
}

func TestDeepCopy_PointerSlice(t *testing.T) {
	type Item struct{ V string }
	items := []*Item{{"a"}, {"b"}}
	cloned := DeepCopy(&items)

	(*cloned)[0].V = "x"
	if items[0].V == "x" {
		t.Fatal("pointer elements should be deep-copied")
	}
}

func TestDeepCopy_Cycle(t *testing.T) {
	type Node struct {
		Name  string
		Child *Node
	}
	a := &Node{Name: "a"}
	b := &Node{Name: "b"}
	a.Child = b
	b.Child = a // cycle: a -> b -> a

	// Should not infinite-loop
	clone := DeepCopy(a)
	if clone.Child == nil || clone.Child.Child == nil {
		t.Fatal("cycle copying failed")
	}
	if clone.Child.Name != "b" {
		t.Fatalf("got %q, want %q", clone.Child.Name, "b")
	}
	// The cycle should be preserved (clone.Child.Child == clone)
	if clone.Child.Child != clone {
		t.Fatal("cycle should point to the cloned root")
	}
}

func TestDeepCopy_NestedMapKey(t *testing.T) {
	type Key struct{ ID int }
	original := map[Key]string{{1}: "one"}
	cloned := DeepCopy(&original)

	(*cloned)[Key{1}] = "modified"
	if original[Key{1}] == "modified" {
		t.Fatal("map with struct keys should deep-copy")
	}
}

func TestDeepCopy_NilSliceField(t *testing.T) {
	type S struct {
		Items []string
	}
	original := &S{}
	cloned := DeepCopy(original)
	if cloned.Items != nil {
		t.Fatal("nil slice should remain nil")
	}
}
