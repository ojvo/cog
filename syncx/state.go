package syncx

import "sync/atomic"

// Use is a single-bit occupancy flag with optional enter/exit callbacks.
// Useful for one-shot resource ownership (e.g. "first caller initializes").
// The zero value is an un-owned Use; callbacks must be set before the first
// Use call.
type Use struct {
	used     uint32
	OnUse    func() // invoked on the transition to used
	OnUnUse  func() // invoked on the transition to unused
}

// NewUse creates an un-owned Use.
func NewUse() *Use { return &Use{} }

// Use attempts to claim ownership. Returns true if this call claimed it (the
// state transitioned from unused to used). On success OnUse is invoked.
func (u *Use) Use() bool {
	if !atomic.CompareAndSwapUint32(&u.used, 0, 1) {
		return false
	}
	if u.OnUse != nil {
		u.OnUse()
	}
	return true
}

// UnUse releases ownership. Returns true if this call released it (the state
// transitioned from used to unused). On success OnUnUse is invoked.
func (u *Use) UnUse() bool {
	if !atomic.CompareAndSwapUint32(&u.used, 1, 0) {
		return false
	}
	if u.OnUnUse != nil {
		u.OnUnUse()
	}
	return true
}

// Used reports whether ownership is currently held.
func (u *Use) Used() bool {
	return atomic.LoadUint32(&u.used) == 1
}

// Once is a fire-once gate. Unlike sync.Once it does not block concurrent
// callers; only the first Do call executes fn and returns true.
type Once struct {
	n uint32
}

// Do executes fn exactly once across the lifetime of this Once. Returns true
// if this call executed fn.
func (o *Once) Do(fn func()) bool {
	if atomic.CompareAndSwapUint32(&o.n, 0, 1) {
		fn()
		return true
	}
	return false
}

// Done reports whether Do has been called.
func (o *Once) Done() bool {
	return atomic.LoadUint32(&o.n) == 1
}

// Bool is a 0/1 atomic boolean with transition-triggered callbacks. The zero
// value is false.
type Bool struct {
	n uint32
}

// NewBool creates a Bool with the given initial value.
func NewBool(v bool) *Bool {
	b := &Bool{}
	if v {
		b.n = 1
	}
	return b
}

// IsTrue reports the current value.
func (b *Bool) IsTrue() bool {
	return atomic.LoadUint32(&b.n) == 1
}

// Set sets the value to v. Returns the previous value.
func (b *Bool) Set(v bool) bool {
	var want uint32
	if v {
		want = 1
	}
	old := atomic.SwapUint32(&b.n, want)
	return old == 1
}

// ListenTrue atomically transitions to true if currently false and invokes fn
// on the transition. Returns true if this call performed the transition.
func (b *Bool) ListenTrue(fn func()) bool {
	if atomic.CompareAndSwapUint32(&b.n, 0, 1) {
		if fn != nil {
			fn()
		}
		return true
	}
	return false
}

// ListenFalse atomically transitions to false if currently true and invokes fn
// on the transition. Returns true if this call performed the transition.
func (b *Bool) ListenFalse(fn func()) bool {
	if atomic.CompareAndSwapUint32(&b.n, 1, 0) {
		if fn != nil {
			fn()
		}
		return true
	}
	return false
}
