package idgen

import (
	"sync/atomic"
)

// AutoInc provides local sequential unique ID generation using atomic operations.
// Safe for concurrent use without locks.
//
// The generated IDs form a sequence: start, start+step, start+2*step, ...
// Positive step produces ascending IDs; negative step produces descending IDs.
// Step must not be zero.
type AutoInc struct {
	current atomic.Int64
	step    int64
	done    atomic.Bool
}

// NewAutoInc creates an AutoInc starting at 'start' and incrementing by 'step'
// on each call to Next. Panics if step == 0.
func NewAutoInc(start, step int64) *AutoInc {
	if step == 0 {
		panic("step must not be 0")
	}
	a := &AutoInc{step: step}
	a.current.Store(start - step) // first Next() will return start
	return a
}

// Next returns the next ID in the sequence.
func (a *AutoInc) Next() int64 {
	return a.current.Add(a.step)
}

// Peek returns the value that Next() would return without advancing the counter.
func (a *AutoInc) Peek() int64 {
	return a.current.Load() + a.step
}
