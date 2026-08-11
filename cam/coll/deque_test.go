package coll

import "testing"

func TestDeque_Empty(t *testing.T) {
	q := NewDeque[int]()
	if q.Len() != 0 {
		t.Error("q.Len() =", q.Len(), "expect 0")
	}
}

func TestDeque_FrontBack(t *testing.T) {
	q := NewDeque[string]()
	q.PushBack("foo")
	q.PushBack("bar")
	q.PushBack("baz")
	if q.Front() != "foo" {
		t.Error("wrong value at front of queue")
	}
	if q.Back() != "baz" {
		t.Error("wrong value at back of queue")
	}

	if q.PopFront() != "foo" {
		t.Error("wrong value removed from front of queue")
	}
	if q.Front() != "bar" {
		t.Error("wrong value remaining at front of queue")
	}
	if q.Back() != "baz" {
		t.Error("wrong value remaining at back of queue")
	}

	if q.PopBack() != "baz" {
		t.Error("wrong value removed from back of queue")
	}
	if q.Front() != "bar" {
		t.Error("wrong value remaining at front of queue")
	}
	if q.Back() != "bar" {
		t.Error("wrong value remaining at back of queue")
	}
}

func TestDeque_GrowShrinkBack(t *testing.T) {
	q := NewDeque[int]()
	size := minCapacity * 2

	for i := 0; i < size; i++ {
		if q.Len() != i {
			t.Error("q.Len() =", q.Len(), "expected", i)
		}
		q.PushBack(i)
	}
	bufLen := q.Cap()

	for i := size; i > 0; i-- {
		if q.Len() != i {
			t.Error("q.Len() =", q.Len(), "expected", i)
		}
		x := q.PopBack()
		if x != i-1 {
			t.Error("q.PopBack() =", x, "expected", i-1)
		}
	}
	if q.Len() != 0 {
		t.Error("q.Len() =", q.Len(), "expected 0")
	}
	if q.Cap() == bufLen {
		t.Error("queue buffer did not shrink")
	}
}

func TestDeque_GrowShrinkFront(t *testing.T) {
	q := NewDeque[int]()
	size := minCapacity * 2

	for i := 0; i < size; i++ {
		if q.Len() != i {
			t.Error("q.Len() =", q.Len(), "expected", i)
		}
		q.PushBack(i)
	}
	bufLen := q.Cap()

	for i := 0; i < size; i++ {
		if q.Len() != size-i {
			t.Error("q.Len() =", q.Len(), "expected", size-i)
		}
		x := q.PopFront()
		if x != i {
			t.Error("q.PopFront() =", x, "expected", i)
		}
	}
	if q.Len() != 0 {
		t.Error("q.Len() =", q.Len(), "expected 0")
	}
	if q.Cap() == bufLen {
		t.Error("queue buffer did not shrink")
	}
}

func TestDeque_PushFront(t *testing.T) {
	q := NewDeque[int]()
	q.PushFront(1)
	q.PushFront(2)
	q.PushFront(3)

	if q.Front() != 3 {
		t.Error("Front should be 3")
	}
	if q.Back() != 1 {
		t.Error("Back should be 1")
	}
	if q.PopFront() != 3 {
		t.Error("PopFront should return 3")
	}
}

func TestDeque_At(t *testing.T) {
	q := NewDeque[int]()
	for i := 0; i < 10; i++ {
		q.PushBack(i)
	}
	for i := 0; i < 10; i++ {
		if q.At(i) != i {
			t.Errorf("At(%d) = %d, expected %d", i, q.At(i), i)
		}
	}
}

func TestDeque_Set(t *testing.T) {
	q := NewDeque[int]()
	for i := 0; i < 5; i++ {
		q.PushBack(i)
	}
	q.Set(2, 99)
	if q.At(2) != 99 {
		t.Errorf("Set failed: At(2) = %d, expected 99", q.At(2))
	}
}

func TestDeque_Rotate(t *testing.T) {
	q := NewDeque[int]()
	for i := 0; i < 5; i++ {
		q.PushBack(i)
	}
	q.Rotate(2)
	if q.At(0) != 2 {
		t.Errorf("After rotate(2), At(0) = %d, expected 2", q.At(0))
	}
	if q.At(4) != 1 {
		t.Errorf("After rotate(2), At(4) = %d, expected 1", q.At(4))
	}
}

func TestDeque_Clear(t *testing.T) {
	q := NewDeque[int]()
	for i := 0; i < 10; i++ {
		q.PushBack(i)
	}
	q.Clear()
	if q.Len() != 0 {
		t.Error("Len should be 0 after Clear")
	}
}
