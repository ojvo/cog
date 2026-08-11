package timer

import "time"

// SafeTimer wraps time.Timer with a safe Reset method that correctly drains
// the timer channel before resetting. This prevents the race condition
// described in the time.Timer.Reset documentation:
//
//	Reset should be invoked only on stopped or expired timers with drained
//	channels. If a program has already received a value from t.C, the timer
//	is known to have expired and the channel drained, so t.Reset can be
//	used directly. If a program has not yet received a value from t.C,
//	however, the timer must be stopped and—if Stop reports that the timer
//	expired before being stopped—the channel explicitly drained.
//
// SafeTimer encapsulates this protocol so callers can reset without concern.
//
// Based on: https://play.golang.org/p/Ys9qqanqmU
type SafeTimer struct {
	*time.Timer
	scr bool // saw channel read
}

// NewSafeTimer creates a SafeTimer that fires after d.
func NewSafeTimer(d time.Duration) *SafeTimer {
	return &SafeTimer{Timer: time.NewTimer(d)}
}

// SCR marks the channel as read. Must be called after receiving a value from
// the timer's C channel and before calling Reset.
func (t *SafeTimer) SCR() {
	t.scr = true
}

// Reset stops the timer, drains the channel if needed, and resets it to fire
// after d. Returns the same value as time.Timer.Reset (true if the timer had
// been active).
func (t *SafeTimer) Reset(d time.Duration) bool {
	ret := t.Stop()
	if !ret && !t.scr {
		<-t.C
	}
	t.Timer.Reset(d)
	t.scr = false
	return ret
}
