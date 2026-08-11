package num

import (
	"testing"
	"time"
)

func TestRange_Single(t *testing.T) {
	// Range(5) → 0,1,2,3,4
	r := Range(5)
	got := ToSlice(r)
	want := []int{0, 1, 2, 3, 4}
	if len(got) != len(want) {
		t.Fatalf("Range(5) got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Range(5)[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestRange_StartEnd(t *testing.T) {
	// Range(1, 5) → 1,2,3,4
	r := Range(1, 5)
	got := ToSlice(r)
	want := []int{1, 2, 3, 4}
	if len(got) != len(want) {
		t.Fatalf("Range(1,5) got %v, want %v", got, want)
	}
}

func TestRange_Step(t *testing.T) {
	// Range(0, 10, 2) → 0,2,4,6,8
	r := Range(0, 10, 2)
	got := ToSlice(r)
	want := []int{0, 2, 4, 6, 8}
	if len(got) != len(want) {
		t.Fatalf("Range(0,10,2) got %v, want %v", got, want)
	}
}

func TestRange_NegativeStep(t *testing.T) {
	// Range(5, 0, -1) → 5,4,3,2,1
	r := Range(5, 0, -1)
	got := ToSlice(r)
	want := []int{5, 4, 3, 2, 1}
	if len(got) != len(want) {
		t.Fatalf("Range(5,0,-1) got %v, want %v", got, want)
	}
}

func TestRange_Empty(t *testing.T) {
	// Range(0) → empty
	r := Range(0)
	got := ToSlice(r)
	if len(got) != 0 {
		t.Errorf("Range(0) got %v, want empty", got)
	}
}

func TestRange_ZeroStep(t *testing.T) {
	// step=0 → empty iterator
	r := Range(0, 5, 0)
	got := ToSlice(r)
	if len(got) != 0 {
		t.Errorf("Range(0,5,0) got %v, want empty", got)
	}
}

func TestRange_DifferentIntTypes(t *testing.T) {
	// int64
	r := Range(int64(1), int64(4))
	got := ToSlice(r)
	if len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Errorf("Range[int64] got %v", got)
	}

	// uint
	r2 := Range(uint(3))
	got2 := ToSlice(r2)
	if len(got2) != 3 || got2[0] != 0 || got2[2] != 2 {
		t.Errorf("Range[uint] got %v", got2)
	}
}

func TestTraverse_Count(t *testing.T) {
	tr := Traverse(3)
	got := ToSlice(tr)
	if len(got) != 3 || got[0] != 0 || got[2] != 2 {
		t.Errorf("Traverse(3) got %v", got)
	}
}

func TestTraverse_WithInterval(t *testing.T) {
	start := time.Now()
	tr := Traverse(2, 10*time.Millisecond)
	ToSlice(tr)
	elapsed := time.Since(start)
	// Should sleep at least once (first call doesn't sleep, second does)
	if elapsed < 10*time.Millisecond {
		t.Errorf("Traverse with interval too fast: %v", elapsed)
	}
}

func TestForEach(t *testing.T) {
	r := Range(5)
	var sum int
	ForEach(r, func(v int) { sum += v })
	// 0+1+2+3+4 = 10
	if sum != 10 {
		t.Errorf("ForEach sum = %d, want 10", sum)
	}
}

func TestMap(t *testing.T) {
	r := Range(4) // 0,1,2,3
	mapped := Map[int, int](r, func(v int) int { return v * v })
	got := ToSlice(mapped)
	want := []int{0, 1, 4, 9}
	if len(got) != len(want) {
		t.Fatalf("Map got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Map[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestFilter(t *testing.T) {
	r := Range(10) // 0..9
	filtered := Filter[int](r, func(v int) bool { return v%2 == 0 })
	got := ToSlice(filtered)
	want := []int{0, 2, 4, 6, 8}
	if len(got) != len(want) {
		t.Fatalf("Filter got %v, want %v", got, want)
	}
}

func TestMapFilterChain(t *testing.T) {
	// Range(5) → filter even → map *10
	r := Range(5)
	even := Filter[int](r, func(v int) bool { return v%2 == 0 })
	mapped := Map[int, int](even, func(v int) int { return v * 10 })
	got := ToSlice(mapped)
	want := []int{0, 20, 40}
	if len(got) != len(want) {
		t.Fatalf("Chain got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Chain[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestTraverseCount(t *testing.T) {
	tc := TraverseCount(3)
	got := ToSlice(tc)
	if len(got) != 3 {
		t.Errorf("TraverseCount(3) got %v", got)
	}
}

func TestCount(t *testing.T) {
	c := Count(4)
	got := ToSlice(c)
	if len(got) != 4 {
		t.Errorf("Count(4) got %v", got)
	}
}
