package diag

import (
	"fmt"
	"io"
	"os"
	"time"
)

// Timer measures elapsed time from creation until Stop is called.
// The zero value is not usable; construct via NewTimer.
type Timer struct {
	start time.Time
	out   io.Writer
}

// NewTimer creates a Timer that begins measuring immediately. The elapsed
// duration is written to w when Stop is called; if w is nil, os.Stdout is used.
func NewTimer(w ...io.Writer) *Timer {
	out := io.Writer(os.Stdout)
	if len(w) > 0 && w[0] != nil {
		out = w[0]
	}
	return &Timer{start: time.Now(), out: out}
}

// Stop writes the elapsed time since the Timer was created.
// Stop is idempotent: subsequent calls are no-ops.
func (t *Timer) Stop() {
	if t == nil || t.start.IsZero() {
		return
	}
	tc := time.Since(t.start)
	t.start = time.Time{}
	fmt.Fprintf(t.out, "diag] %v\n", tc)
}

// Reset re-bases the Timer to the current time. Returns the elapsed duration
// since the previous start (or zero on a fresh Timer). Useful for segmenting
// multi-phase operations.
func (t *Timer) Reset() time.Duration {
	if t == nil {
		return 0
	}
	now := time.Now()
	prev := t.start
	t.start = now
	if prev.IsZero() {
		return 0
	}
	return now.Sub(prev)
}

// TimeFunc returns a stop function that, when invoked, prints the elapsed time
// since TimeFunc was called. Useful for defer-based timing:
//
//	defer diag.TimeFunc()()
func TimeFunc(w ...io.Writer) func() {
	t := NewTimer(w...)
	return t.Stop
}
