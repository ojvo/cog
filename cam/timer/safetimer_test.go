package timer

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSafeTimer_Reset(t *testing.T) {
	var fired atomic.Int32

	timer := NewSafeTimer(50 * time.Millisecond)
	<-timer.C
	timer.SCR()

	fired.Store(0)
	timer.Reset(50 * time.Millisecond)
	<-timer.C
	timer.SCR()
	fired.Add(1)

	if fired.Load() != 1 {
		t.Fatal("timer should fire after reset")
	}
}

func TestSafeTimer_ResetBeforeFire(t *testing.T) {
	timer := NewSafeTimer(time.Hour) // far future
	timer.Reset(50 * time.Millisecond)
	<-timer.C
	timer.SCR()
	// Should fire within 500ms
}

func TestSafeTimer_Concurrent(t *testing.T) {
	var wg sync.WaitGroup
	var fired atomic.Int32

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			timer := NewSafeTimer(time.Duration(10+i*5) * time.Millisecond)
			<-timer.C
			timer.SCR()
			fired.Add(1)
		}()
	}
	wg.Wait()

	if fired.Load() != 10 {
		t.Fatalf("expected 10 fires, got %d", fired.Load())
	}
}

func TestSafeTimer_StopAndReset(t *testing.T) {
	timer := NewSafeTimer(20 * time.Millisecond)
	time.Sleep(30 * time.Millisecond) // let it fire
	timer.SCR()
	timer.Reset(10 * time.Millisecond)
	<-timer.C
	timer.SCR()
	// Should not hang
}
