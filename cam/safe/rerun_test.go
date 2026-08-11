package safe

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeDialer is a test Dialer that fails Dial up to failCount times then
// succeeds; Run blocks until ctx is canceled.
type fakeDialer struct {
	dialCount  int32
	runCount   int32
	closeCount int32
	failCount  int32
}

func (d *fakeDialer) Dial(ctx context.Context) error {
	n := atomic.AddInt32(&d.dialCount, 1)
	if n <= d.failCount {
		return errors.New("dial failed")
	}
	return nil
}

func (d *fakeDialer) Run(ctx context.Context) error {
	atomic.AddInt32(&d.runCount, 1)
	<-ctx.Done()
	return ctx.Err()
}

func (d *fakeDialer) Close() error {
	atomic.AddInt32(&d.closeCount, 1)
	return nil
}

func TestRerun_DialSuccess_FirstTry(t *testing.T) {
	d := &fakeDialer{failCount: 0}
	r := NewRerun()
	r.OnInterval(func(retry int) time.Duration { return time.Millisecond })

	dialed := make(chan error, 1)
	go func() {
		dialed <- r.DialRun(d)
	}()

	select {
	case err := <-dialed:
		if err != nil {
			t.Errorf("first dial should succeed, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("DialRun did not return")
	}

	// Verify dial state.
	dialedFlag, _ := r.Status()
	if !dialedFlag {
		t.Error("should be dialed after successful Dial")
	}

	r.OneRun.Close()
}

func TestRerun_DialRetry_ThenSuccess(t *testing.T) {
	d := &fakeDialer{failCount: 2}
	r := NewRerun()
	r.OnInterval(func(retry int) time.Duration { return time.Millisecond })

	// DialRun returns the FIRST dial's error. Since failCount=2, the first
	// dial fails, so DialRun returns an error. The reconnect loop continues
	// in the background and eventually succeeds.
	done := make(chan error, 1)
	go func() { done <- r.DialRun(d) }()

	select {
	case err := <-done:
		if err == nil {
			t.Error("first dial should fail, expected non-nil error")
		}
	case <-time.After(time.Second):
		t.Fatal("DialRun did not return")
	}

	// Wait for the background loop to retry until success (3 total dials).
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&d.dialCount) >= 3 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if got := atomic.LoadInt32(&d.dialCount); got != 3 {
		t.Errorf("Dial should be called 3 times (2 failures + 1 success), got %d", got)
	}

	// After successful dial, Run blocks until canceled. Status should be dialed.
	dialed, _ := r.Status()
	if !dialed {
		t.Error("should be dialed after retries succeed")
	}

	r.OneRun.Close()
}

func TestRerun_OnDial_Callback(t *testing.T) {
	d := &fakeDialer{failCount: 1}
	r := NewRerun()
	r.OnInterval(func(retry int) time.Duration { return time.Millisecond })

	var dialEvents []struct {
		index int
		retry int
		err   error
	}
	var mu sync.Mutex
	r.OnDial(func(index, retry int, err error) {
		mu.Lock()
		dialEvents = append(dialEvents, struct {
			index int
			retry int
			err   error
		}{index, retry, err})
		mu.Unlock()
	})

	done := make(chan error, 1)
	go func() { done <- r.DialRun(d) }()
	<-done // first dial fails; DialRun returns

	// Wait for at least 2 OnDial events (first failure + eventual success).
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(dialEvents)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(dialEvents) < 2 {
		t.Errorf("OnDial should be called at least twice, got %d", len(dialEvents))
	}
	// First event: (index=0, retry=0, err=non-nil)
	if dialEvents[0].index != 0 || dialEvents[0].retry != 0 {
		t.Errorf("first OnDial event = (idx=%d, retry=%d), want (0, 0)",
			dialEvents[0].index, dialEvents[0].retry)
	}
	if dialEvents[0].err == nil {
		t.Error("first OnDial event should have non-nil err (dial failure)")
	}

	r.OneRun.Close()
}

func TestRerun_OnInterval_RetryCount(t *testing.T) {
	// Verify the retry count passed to OnInterval is 1-based and increments
	// by 1 each retry (not double-incremented, which was the original bug).
	d := &fakeDialer{failCount: 3}
	r := NewRerun()
	var retries []int
	var mu sync.Mutex
	r.OnInterval(func(retry int) time.Duration {
		mu.Lock()
		retries = append(retries, retry)
		mu.Unlock()
		return time.Millisecond
	})
	r.OnDial(func(_, _ int, _ error) {})

	done := make(chan error, 1)
	go func() { done <- r.DialRun(d) }()
	<-done // first dial fails; DialRun returns

	// Wait for 3 retry intervals to be recorded.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(retries)
		mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(retries) != 3 {
		t.Fatalf("expected 3 retry intervals, got %d", len(retries))
	}
	if retries[0] != 1 || retries[1] != 2 || retries[2] != 3 {
		t.Errorf("retry sequence = %v, want [1 2 3]", retries)
	}

	r.OneRun.Close()
}

func TestRerun_Status_Reason(t *testing.T) {
	d := &fakeDialer{failCount: 0}
	r := NewRerun()
	r.OnInterval(func(retry int) time.Duration { return time.Millisecond })

	// Initial state.
	dialed, reason := r.Status()
	if dialed {
		t.Error("should not be dialed initially")
	}
	if reason != ErrNotDialed.Error() {
		t.Errorf("initial reason = %q, want %q", reason, ErrNotDialed.Error())
	}

	done := make(chan error, 1)
	go func() { done <- r.DialRun(d) }()
	<-done

	dialed, reason = r.Status()
	if !dialed {
		t.Error("should be dialed after successful Dial")
	}
	if reason != "" {
		t.Errorf("reason should be empty when dialed, got %q", reason)
	}

	r.OneRun.Close()
}

func TestRerun_RunFailure_TriggersReconnect(t *testing.T) {
	// Dialer that succeeds dial but Run returns an error immediately,
	// forcing a reconnect cycle. We verify DialCount > 1 after multiple Run
	// failures.
	d := &failingRunDialer{failCount: 2}
	r := NewRerun()
	r.OnInterval(func(retry int) time.Duration { return time.Millisecond })

	done := make(chan error, 1)
	go func() { done <- r.DialRun(d) }()
	<-done

	// Give the loop time to reconnect a few times.
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&d.dialCount); got < 2 {
		t.Errorf("expected multiple Dial calls after Run failures, got %d", got)
	}

	r.OneRun.Close()
}

type failingRunDialer struct {
	dialCount int32
	failCount int32 // number of Run calls that should fail before blocking
	runCount  int32
}

func (d *failingRunDialer) Dial(ctx context.Context) error {
	atomic.AddInt32(&d.dialCount, 1)
	return nil
}

func (d *failingRunDialer) Run(ctx context.Context) error {
	n := atomic.AddInt32(&d.runCount, 1)
	if n <= d.failCount {
		return errors.New("run failed")
	}
	<-ctx.Done()
	return ctx.Err()
}

func (d *failingRunDialer) Close() error { return nil }

// Ensure *fakeDialer and *failingRunDialer implement Dialer at compile time.
var (
	_ Dialer    = (*fakeDialer)(nil)
	_ Dialer    = (*failingRunDialer)(nil)
	_ io.Closer = (*fakeDialer)(nil)
)
