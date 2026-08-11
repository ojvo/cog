package timer

import (
	"testing"
	"time"
)

func TestChainTicker_Add(t *testing.T) {
	interval := time.Millisecond * 10
	ticker := NewChanTicker(interval)
	ch := make(chan time.Time, 1)
	ticker.Add(ch)
	t0 := time.Now()
	for i := 0; i < 10; i++ {
		<-ch
	}
	// 10 ticks * 10ms = 100ms
	t.Logf("time used: %s, expect %s", time.Now().Sub(t0), interval*10)
}

func TestChainTicker_Now(t *testing.T) {
	now := time.Now()
	ticker := NewChanTicker(time.Millisecond * 10)
	if ticker.LastTickTime().Before(now) {
		t.Fatalf("ticker LastTickTime is wrong")
	}

	for i := 0; i < 10; i++ {
		// Allow some slack (50ms) for scheduler
		if time.Since(ticker.LastTickTime()) > time.Millisecond*100 {
			t.Fatalf("ticker.LastTickTime() is too old: %v", time.Since(ticker.LastTickTime()))
		}
		time.Sleep(time.Millisecond * 10)
	}
}

func TestChainTicker_Remove(t *testing.T) {
	ticker := NewChanTicker(time.Millisecond)
	ch := make(chan time.Time, 1)
	ticker.Add(ch)
	for i := 0; i < 10; i++ {
		<-ch
	}

	ticker.Remove(ch)

	// Drain potential residual tick (since Remove no longer drains)
	select {
	case <-ch:
	default:
	}

	select {
	case <-ch:
		t.Fatalf("should not receive from channel")
	case <-time.After(time.Millisecond * 100):
		break
	}
}
