package timer

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestCircle_Basic(t *testing.T) {
	var ctr atomic.Int32

	c := NewCircle(10*time.Millisecond, func() {
		ctr.Add(1)
	})
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	c.Stop()

	// After Stop returns, the goroutine is done; no more callbacks.
	n := ctr.Load()
	time.Sleep(50 * time.Millisecond)
	if ctr.Load() != n {
		t.Errorf("callback ran after Stop returned: %d -> %d", n, ctr.Load())
	}
}

func TestCircle_Reset(t *testing.T) {
	var ctr atomic.Int32

	c := NewCircle(50*time.Millisecond, func() {
		ctr.Add(1)
	})
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	time.Sleep(120 * time.Millisecond) // ~2 ticks
	if err := c.Reset(10 * time.Millisecond); err != nil {
		t.Fatal(err)
	}

	time.Sleep(60 * time.Millisecond) // ~6 more ticks
	c.Stop()

	if n := ctr.Load(); n < 4 {
		t.Errorf("expected at least 4 ticks, got %d", n)
	}
}

func TestCircle_StopBeforeStart(t *testing.T) {
	c := NewCircle(time.Second, func() {})
	c.Stop() // no-op since Start was never called
	if err := c.Start(); err != nil {
		t.Fatalf("Start after no-op Stop should succeed: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	c.Stop()
}

func TestCircle_ResetAfterStop(t *testing.T) {
	c := NewCircle(5*time.Millisecond, func() {})
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	c.Stop()

	err := c.Reset(1 * time.Millisecond)
	if err == nil {
		t.Error("expected error for Reset after Stop")
	}
}

// TestCircle_StartAfterStop verifies that Start returns an error (not panic)
// after Stop, per the "Stop is terminal" contract.
func TestCircle_StartAfterStop(t *testing.T) {
	c := NewCircle(5*time.Millisecond, func() {})
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	c.Stop()

	if err := c.Start(); err == nil {
		t.Error("expected error for Start after Stop")
	}
}

func TestCircle_StartOnlyOnce(t *testing.T) {
	c := NewCircle(time.Hour, func() {})
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err == nil {
		t.Fatal("second Start should return an error instead of creating another loop")
	}
	c.Stop()
}

func TestCircle_MultipleStop(t *testing.T) {
	c := NewCircle(10*time.Millisecond, func() {})
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	c.Stop()
	c.Stop() // idempotent
}

func TestCircle_SubMillisecondInterval(t *testing.T) {
	c := NewCircle(0, func() {})
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	c.Stop()
}

func TestCircle_PanicRecovery(t *testing.T) {
	var ctr atomic.Int32

	c := NewCircle(10*time.Millisecond, func() {
		ctr.Add(1)
		panic("boom")
	})
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)
	c.Stop()

	if ctr.Load() == 0 {
		t.Error("callback should have run despite panics")
	}
}
