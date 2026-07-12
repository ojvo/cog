package util

import (
	"testing"
)

func TestRandString(t *testing.T) {
	// Test length
	for _, n := range []int{1, 10, 50, 100} {
		s := randString(n)
		if len(s) != n {
			t.Errorf("Expected length %d, got %d", n, len(s))
		}
	}

	// Test randomness (simple check for duplicates)
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		s := randString(10)
		if seen[s] {
			t.Errorf("Duplicate string generated: %s", s)
		}
		seen[s] = true
	}

	// Test concurrency
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 1000; j++ {
				randString(10)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
