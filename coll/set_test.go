package coll

import (
	"sync"
	"testing"
)

func TestHset_Basic(t *testing.T) {
	s := NewHset[int]()
	if !s.Empty() {
		t.Error("new set should be empty")
	}

	s.Add(1, 2, 3)
	if s.Len() != 3 {
		t.Errorf("expected len 3, got %d", s.Len())
	}

	if !s.Contains(1) || !s.Contains(2) || !s.Contains(3) {
		t.Error("set should contain added elements")
	}

	if s.Contains(4) {
		t.Error("set should not contain missing element")
	}

	s.Remove(2)
	if s.Len() != 2 {
		t.Errorf("expected len 2, got %d", s.Len())
	}
	if s.Contains(2) {
		t.Error("set should not contain removed element")
	}

	s.Clear()
	if !s.Empty() {
		t.Error("cleared set should be empty")
	}
}

func TestHset_Race(t *testing.T) {
	s := NewHset[int]()
	var wg sync.WaitGroup
	n := 100

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(val int) {
			defer wg.Done()
			s.Add(val)
		}(i)
	}

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(val int) {
			defer wg.Done()
			s.Contains(val)
		}(i)
	}

	wg.Wait()

	if s.Len() != n {
		t.Errorf("expected len %d, got %d", n, s.Len())
	}
}

func TestHset_Same(t *testing.T) {
	s1 := NewHset[int]()
	s1.Add(1, 2)
	s2 := NewHset[int]()
	s2.Add(2, 1)
	s3 := NewHset[int]()
	s3.Add(1)

	if !s1.Same(s2) {
		t.Error("sets should be same")
	}
	if s1.Same(s3) {
		t.Error("sets should not be same")
	}
	if !s1.Same(s1) {
		t.Error("set should be same as itself")
	}
}

func TestHset_Values(t *testing.T) {
	s := NewHset[string]()
	s.Add("a", "b", "c")
	vals := s.Values()
	if len(vals) != 3 {
		t.Errorf("expected 3 values, got %d", len(vals))
	}

	// Check all values are present
	s2 := NewHset[string]()
	for _, v := range vals {
		s2.Add(v)
	}
	if !s.Same(s2) {
		t.Error("Values() should return all elements")
	}
}

func TestHset_String(t *testing.T) {
	s := NewHset[int]()
	s.Add(1)
	str := s.String()
	if str == "" {
		t.Error("string should not be empty")
	}
}
