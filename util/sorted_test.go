package util

import (
	"reflect"
	"testing"
)

func TestSortedKeys(t *testing.T) {
	got := SortedKeys(map[string]struct{}{"b": {}, "a": {}, "c": {}})
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SortedKeys = %v; want %v", got, want)
	}
	if got := SortedKeys(nil); len(got) != 0 {
		t.Fatalf("SortedKeys(nil) = %v; want empty", got)
	}
}
