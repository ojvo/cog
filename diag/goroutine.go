package diag

import "runtime"

// GoroutineLeaks runs f and reports the change in goroutine count.
// A non-zero result indicates f may have leaked goroutines.
//
// Note: a transient background goroutine that is still alive when f returns
// will register as a leak; callers should ensure f runs to a quiescent state
// for an accurate reading.
func GoroutineLeaks(f func()) int {
	then := runtime.NumGoroutine()
	f()
	now := runtime.NumGoroutine()
	return now - then
}

// GoroutineMark captures a goroutine count baseline for later comparison.
// The zero value is not usable; construct via MarkGoroutines.
type GoroutineMark struct {
	then int
	now  int
}

// MarkGoroutines captures the current goroutine count as a baseline.
func MarkGoroutines() *GoroutineMark {
	return &GoroutineMark{then: runtime.NumGoroutine()}
}

// Release returns the change in goroutine count since the mark was taken.
func (m *GoroutineMark) Release() int {
	m.now = runtime.NumGoroutine()
	return m.now - m.then
}
