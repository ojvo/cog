package coll

import (
	"sync"
	"testing"
)

func TestStack_Pop(t *testing.T) {
	stack := NewStack[int]()
	for i := 0; i < 5; i++ {
		stack.Push(i)
	}

	if stack.Len() != 5 {
		t.Errorf("Len() = %v, expect 5", stack.Len())
	}

	count := 0
	for {
		val, ok := stack.Pop()
		if !ok {
			break
		}
		// Stack is LIFO: 4, 3, 2, 1, 0
		expected := 4 - count
		if val != expected {
			t.Errorf("Pop() = %v, expect %v", val, expected)
		}
		count++
	}
	if count != 5 {
		t.Errorf("Popped %v items, expect 5", count)
	}
	if stack.Len() != 0 {
		t.Errorf("Len() = %v, expect 0", stack.Len())
	}
}

func TestStack_Empty(t *testing.T) {
	stack := NewStack[string]()
	_, ok := stack.Pop()
	if ok {
		t.Error("Pop on empty stack should return false")
	}
	if stack.Len() != 0 {
		t.Error("Len should be 0")
	}
}

func TestStack_Concurrent(t *testing.T) {
	stack := NewStack[int]()
	var wg sync.WaitGroup

	// Concurrent producers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				stack.Push(base*100 + j)
			}
		}(i)
	}
	wg.Wait()

	// Concurrent consumers after all pushes complete
	results := make(chan int, 1000)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if val, ok := stack.Pop(); ok {
					results <- val
				}
			}
		}()
	}
	wg.Wait()
	close(results)

	count := 0
	for range results {
		count++
	}

	if count != 1000 {
		t.Errorf("expected 1000 pops, got %d", count)
	}
}

func BenchmarkStack_Push(b *testing.B) {
	q := NewStack[int]()
	b.RunParallel(func(p *testing.PB) {
		var ctr int
		for p.Next() {
			q.Push(ctr)
			ctr++
		}
	})
}
