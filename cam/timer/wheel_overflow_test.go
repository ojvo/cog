package timer

import (
	"math"
	"testing"
	"time"
)

func TestTimeWheel_Overflow(t *testing.T) {
	interval := time.Millisecond
	tw := New[string, any](interval, 1, func(d any) {})
	
	tw.tick = math.MaxInt64 - 100
	
	fired := make(chan struct{})
	tw.job = func(d any) {
		close(fired)
	}
	
	tw.AddTimer(200*time.Millisecond, "overflow", nil)
	
	tw.tick++
	tw.scanAndRunTask(tw.slots[0])
	
	select {
	case <-fired:
		t.Fatal("Task fired prematurely due to overflow!")
	case <-time.After(10 * time.Millisecond):
		// Passed
	}
}
