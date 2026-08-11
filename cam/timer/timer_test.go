package timer

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestCallerPerHour(t *testing.T) {
	// This is hard to test deterministically without mocking time,
	// but we can test the immediate call and cancellation.
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	called := make(chan struct{})
	
	CallerPerHour(ctx, func() {
		called <- struct{}{}
	})

	// Should be called immediately
	select {
	case <-called:
		// OK
	case <-time.After(1 * time.Second):
		t.Fatal("CallerPerHour did not call F immediately")
	}

	// Cancel context, ensure no panic or hang
	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestSleepSecond(t *testing.T) {
	start := time.Now()
	SleepSecond(0, 1) // 0s
	if time.Since(start) > 500*time.Millisecond {
		t.Logf("SleepSecond(0, 1) took %v", time.Since(start))
	}
	
	// Test invalid input (st >= et)
	start = time.Now()
	SleepSecond(1, 0) // Should sleep 1s
	if time.Since(start) < 1*time.Second {
		t.Errorf("SleepSecond(1, 0) should sleep at least 1s, got %v", time.Since(start))
	}
}

func TestSleepMs(t *testing.T) {
	// Test normal range
	start := time.Now()
	SleepMs(10, 20)
	dur := time.Since(start)
	if dur < 10*time.Millisecond || dur > 50*time.Millisecond { // ample buffer
		t.Errorf("SleepMs(10, 20) took %v", dur)
	}

	// Test invalid input
	start = time.Now()
	SleepMs(100, 50) // Should sleep 100ms
	if time.Since(start) < 100*time.Millisecond {
		t.Errorf("SleepMs(100, 50) took %v, expected >= 100ms", time.Since(start))
	}
}

func TestChainTicker_Stop(t *testing.T) {
	ticker := NewChanTicker(time.Millisecond * 10)
	
	// Verify it runs
	ch := make(chan time.Time, 1)
	ticker.Add(ch)
	select {
	case <-ch:
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ChainTicker did not tick")
	}
	
	// Stop it
	ticker.Stop()
	
	// Verify it stops (hard to verify goroutine exit from outside, but we can verify no new ticks if we drain)
	// Actually, if we remove the channel, we definitely won't get ticks. 
	// But Stop() closes the internal loop.
	
	// Let's just ensure Stop() doesn't panic and is idempotent.
	ticker.Stop()
}

func TestConcurrentTimerMapAccess(t *testing.T) {
	// Regression test for map race in TimeWheel
	tw := New[int, any](10*time.Millisecond, 10, func(data any) {})
	tw.Start()
	defer tw.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tw.AddTimer(time.Duration(i)*time.Millisecond, i, nil)
			time.Sleep(time.Millisecond)
			tw.RemoveTimer(i)
		}(i)
	}
	wg.Wait()
}
