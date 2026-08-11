package idgen

import (
	"sync"
	"testing"
)

func TestAutoInc_Basic(t *testing.T) {
	a := NewAutoInc(1, 1)

	for i := int64(1); i <= 10; i++ {
		if got := a.Next(); got != i {
			t.Errorf("expected %d, got %d", i, got)
		}
	}
}

func TestAutoInc_StartOffset(t *testing.T) {
	a := NewAutoInc(100, 1)
	if got := a.Peek(); got != 100 {
		t.Errorf("Peek expected 100, got %d", got)
	}
	if got := a.Next(); got != 100 {
		t.Errorf("expected 100, got %d", got)
	}
	if got := a.Next(); got != 101 {
		t.Errorf("expected 101, got %d", got)
	}
}

func TestAutoInc_NegativeStep(t *testing.T) {
	a := NewAutoInc(10, -2)

	if got := a.Next(); got != 10 {
		t.Errorf("expected 10, got %d", got)
	}
	if got := a.Next(); got != 8 {
		t.Errorf("expected 8, got %d", got)
	}
	if got := a.Next(); got != 6 {
		t.Errorf("expected 6, got %d", got)
	}
}

func TestAutoInc_BigStep(t *testing.T) {
	a := NewAutoInc(0, 1000)

	if got := a.Next(); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
	if got := a.Next(); got != 1000 {
		t.Errorf("expected 1000, got %d", got)
	}
}

func TestAutoInc_Concurrent(t *testing.T) {
	a := NewAutoInc(0, 1)
	const goroutines = 10
	const perGoroutine = 1000
	const total = goroutines * perGoroutine

	var wg sync.WaitGroup
	seen := make(map[int64]bool)
	var mu sync.Mutex

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				id := a.Next()
				mu.Lock()
				if seen[id] {
					t.Errorf("duplicate ID: %d", id)
				}
				seen[id] = true
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if len(seen) != total {
		t.Errorf("expected %d unique IDs, got %d", total, len(seen))
	}
}

func TestAutoInc_Peek(t *testing.T) {
	a := NewAutoInc(5, 3)

	if got := a.Peek(); got != 5 {
		t.Errorf("Peek expected 5, got %d", got)
	}
	if got := a.Next(); got != 5 {
		t.Errorf("Next expected 5, got %d", got)
	}
	if got := a.Peek(); got != 8 {
		t.Errorf("Peek expected 8, got %d", got)
	}
	if got := a.Next(); got != 8 {
		t.Errorf("Next expected 8, got %d", got)
	}
}

func TestAutoInc_PanicZeroStep(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for step == 0")
		}
	}()
	NewAutoInc(0, 0)
}
