package num

import (
	"time"
)

// Integer is a type constraint for all integer types.
type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

// Iterator is a generic iterator interface.
type Iterator[T any] interface {
	Next() bool
	Value() T
}

// Seq is a function type similar to Go 1.23 iter.Seq.
type Seq[T any] func(yield func(T) bool)

// RangeIterator iterates from start to end with a step.
type RangeIterator[T Integer] struct {
	current T
	end     T
	step    T
	started bool
}

// Next advances the iterator. Returns false when iteration is complete.
func (r *RangeIterator[T]) Next() bool {
	if !r.started {
		r.started = true
	} else {
		r.current += r.step
	}
	if r.step > 0 {
		return r.current < r.end
	}
	return r.current > r.end
}

// Value returns the current element (idempotent between Next calls).
func (r *RangeIterator[T]) Value() T {
	return r.current
}

// All returns a Seq for use with Go 1.23+ for-range.
func (r *RangeIterator[T]) All() Seq[T] {
	return func(yield func(T) bool) {
		iter := *r
		iter.started = false
		for iter.Next() {
			if !yield(iter.Value()) {
				return
			}
		}
	}
}

// Range creates a range iterator (Python-style).
//   Range(5)        → 0, 1, 2, 3, 4
//   Range(1, 5)     → 1, 2, 3, 4
//   Range(0, 5, 2)  → 0, 2, 4
//   Range(5, 0, -1) → 5, 4, 3, 2, 1
func Range[T Integer](n T, m ...T) *RangeIterator[T] {
	start, end, step := T(0), n, T(1)
	switch len(m) {
	case 0:
		// Range(5): start=0, end=5, step=1
	case 1:
		// Range(1, 5): start=1, end=5, step=1
		start, end = n, m[0]
	default:
		// Range(0, 5, 2): start=0, end=5, step=2
		start, end, step = n, m[0], m[1]
	}

	if step == 0 {
		return &RangeIterator[T]{current: start, end: start, step: 1}
	}

	return &RangeIterator[T]{
		current: start,
		end:     end,
		step:    step,
		started: false,
	}
}

// TraverseIterator iterates a count of items with optional interval delay.
type TraverseIterator[T Integer] struct {
	current  T
	count    T
	interval time.Duration
	infinite bool
	started  bool
}

// Next advances the iterator. Returns false when count is reached.
func (t *TraverseIterator[T]) Next() bool {
	if !t.started {
		t.started = true
	} else {
		t.current++
	}

	if !t.infinite && t.current >= t.count {
		return false
	}

	if t.interval > 0 {
		time.Sleep(t.interval)
	}

	return true
}

// Value returns the current index.
func (t *TraverseIterator[T]) Value() T {
	return t.current
}

// All returns a Seq for use with Go 1.23+ for-range.
func (t *TraverseIterator[T]) All() Seq[T] {
	return func(yield func(T) bool) {
		iter := *t
		iter.started = false
		for iter.Next() {
			if !yield(iter.Value()) {
				return
			}
		}
	}
}

// Traverse creates a traverse iterator.
//   Traverse(-1, interval) → infinite loop with interval
//   Traverse(5)            → 0, 1, 2, 3, 4
//   Traverse(3, 1*time.Second) → 0, 1, 2 with 1s delay between each
func Traverse[T Integer](num T, interval ...time.Duration) *TraverseIterator[T] {
	var dur time.Duration
	if len(interval) > 0 {
		dur = interval[0]
	}
	return &TraverseIterator[T]{
		current:  0,
		count:    num,
		interval: dur,
		infinite: num < 0,
		started:  false,
	}
}

// TraverseInterval creates an infinite traverse iterator with the given interval.
func TraverseInterval(interval time.Duration) *TraverseIterator[int] {
	return Traverse(-1, interval)
}

// TraverseCount creates a traverse iterator for the given count.
func TraverseCount[T Integer](num T) *TraverseIterator[T] {
	return Traverse(num)
}

// Count is an alias for Traverse.
func Count[T Integer](num T, interval ...time.Duration) *TraverseIterator[T] {
	return Traverse(num, interval...)
}

// ForEach iterates over iter and calls fn for each element.
func ForEach[T any](iter Iterator[T], fn func(T)) {
	for iter.Next() {
		fn(iter.Value())
	}
}

// ToSlice collects all elements from iter into a slice.
func ToSlice[T any](iter Iterator[T]) []T {
	var result []T
	for iter.Next() {
		result = append(result, iter.Value())
	}
	return result
}

// MapIterator transforms elements from a source iterator.
type MapIterator[T any, R any] struct {
	iter    Iterator[T]
	mapper  func(T) R
	current R
}

// Next advances the iterator.
func (m *MapIterator[T, R]) Next() bool {
	if m.iter.Next() {
		m.current = m.mapper(m.iter.Value())
		return true
	}
	return false
}

// Value returns the mapped value.
func (m *MapIterator[T, R]) Value() R {
	return m.current
}

// Map creates a mapping iterator that transforms each element.
func Map[T any, R any](iter Iterator[T], mapper func(T) R) *MapIterator[T, R] {
	return &MapIterator[T, R]{iter: iter, mapper: mapper}
}

// FilterIterator filters elements from a source iterator.
type FilterIterator[T any] struct {
	iter      Iterator[T]
	predicate func(T) bool
	current   T
}

// Next advances to the next element matching the predicate.
func (f *FilterIterator[T]) Next() bool {
	for f.iter.Next() {
		val := f.iter.Value()
		if f.predicate(val) {
			f.current = val
			return true
		}
	}
	return false
}

// Value returns the current filtered value.
func (f *FilterIterator[T]) Value() T {
	return f.current
}

// Filter creates a filtering iterator that only yields elements matching the predicate.
func Filter[T any](iter Iterator[T], predicate func(T) bool) *FilterIterator[T] {
	return &FilterIterator[T]{iter: iter, predicate: predicate}
}
