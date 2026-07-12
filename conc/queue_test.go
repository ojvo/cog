package conc

import (
	"testing"
)

func TestQueue_Basic(t *testing.T) {
	q := New()
	if q.Len() != 0 {
		t.Errorf("Expected len 0, got %d", q.Len())
	}
	if !q.empty() {
		t.Error("Expected empty queue")
	}

	q.PushBack(1)
	if q.Len() != 1 {
		t.Errorf("Expected len 1, got %d", q.Len())
	}
	if q.Front() != 1 {
		t.Errorf("Expected front 1, got %v", q.Front())
	}
	if q.Back() != 1 {
		t.Errorf("Expected back 1, got %v", q.Back())
	}

	q.PushBack(2)
	if q.Len() != 2 {
		t.Errorf("Expected len 2, got %d", q.Len())
	}
	if q.Front() != 1 {
		t.Errorf("Expected front 1, got %v", q.Front())
	}
	if q.Back() != 2 {
		t.Errorf("Expected back 2, got %v", q.Back())
	}
	
	val := q.PopFront()
	if val != 1 {
		t.Errorf("Expected pop front 1, got %v", val)
	}
	if q.Len() != 1 {
		t.Errorf("Expected len 1, got %d", q.Len())
	}
	
	val = q.PopFront()
	if val != 2 {
		t.Errorf("Expected pop front 2, got %v", val)
	}
	if q.Len() != 0 {
		t.Errorf("Expected len 0, got %d", q.Len())
	}
	
	if q.PopFront() != nil {
		t.Error("Expected nil from empty pop")
	}
}

func TestQueue_FrontBack(t *testing.T) {
	q := New()
	q.PushFront(1)
	q.PushBack(2)
	// [1, 2]
	
	if q.Front() != 1 {
		t.Errorf("Expected front 1, got %v", q.Front())
	}
	if q.Back() != 2 {
		t.Errorf("Expected back 2, got %v", q.Back())
	}
	
	q.PushFront(0)
	// [0, 1, 2]
	if q.Front() != 0 {
		t.Errorf("Expected front 0, got %v", q.Front())
	}
	
	val := q.PopBack()
	if val != 2 {
		t.Errorf("Expected pop back 2, got %v", val)
	}
	// [0, 1]
	
	if q.Back() != 1 {
		t.Errorf("Expected back 1, got %v", q.Back())
	}
}

func TestQueue_Resize(t *testing.T) {
	q := New()
	// Initial capacity is 1
	
	// Add enough items to trigger grow
	for i := 0; i < 100; i++ {
		q.PushBack(i)
	}
	
	if q.Len() != 100 {
		t.Errorf("Expected len 100, got %d", q.Len())
	}
	
	// Verify order
	for i := 0; i < 100; i++ {
		val := q.PopFront()
		if val != i {
			t.Errorf("Expected %d, got %v", i, val)
		}
	}
	
	// Should shrink back down
	if q.Len() != 0 {
		t.Errorf("Expected len 0, got %d", q.Len())
	}
}

func TestQueue_String(t *testing.T) {
	q := New()
	q.PushBack(1)
	q.PushBack(2)
	q.PushBack(3)
	
	str := q.String()
	if str != "[1 2 3]" {
		t.Errorf("Expected [1 2 3], got %s", str)
	}
}

func TestQueue_ZeroValue(t *testing.T) {
	var q Queue
	q.PushBack(1)
	if q.Len() != 1 {
		t.Error("Zero value queue should work with PushBack")
	}
	
	var q2 Queue
	q2.PushFront(1)
	if q2.Len() != 1 {
		t.Error("Zero value queue should work with PushFront")
	}
}

func TestQueue_WrapAround(t *testing.T) {
	q := New()
	// Force capacity to be small initially? 
	// The implementation doubles capacity. 
	// We want to test the ring buffer wrapping.
	// Capacity starts at 1.
	
	q.PushBack(1) // cap 1 -> 2
	q.PushBack(2) // cap 2 -> 4
	q.PushBack(3) // cap 4
	
	// q: [1, 2, 3] (size 3, cap 4)
	// front=0, back=3
	
	q.PopFront() // [2, 3], front=1
	q.PopFront() // [3], front=2
	
	// Add more to wrap around
	q.PushBack(4) // [3, 4], back=0 (wrapped)
	q.PushBack(5) // [3, 4, 5], back=1
	
	if q.Front() != 3 {
		t.Errorf("Expected front 3, got %v", q.Front())
	}
	if q.Back() != 5 {
		t.Errorf("Expected back 5, got %v", q.Back())
	}
	
	// Verify order
	expected := []int{3, 4, 5}
	for _, exp := range expected {
		if val := q.PopFront(); val != exp {
			t.Errorf("Expected %d, got %v", exp, val)
		}
	}
}
