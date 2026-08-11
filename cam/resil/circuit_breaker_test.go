package resil

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCircuitBreaker_ClosedPassesThrough(t *testing.T) {
	cb := NewCircuitBreaker(NewCircuitBreakerConfig().WithThreshold(3))
	calls := 0
	err := cb.Execute(func() error {
		calls++
		return nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("expected 1 successful call, got calls=%d err=%v", calls, err)
	}
	if cb.State() != CircuitClosed {
		t.Fatalf("expected closed, got %s", cb.State())
	}
}

func TestCircuitBreaker_TripsAfterThreshold(t *testing.T) {
	var transitions []CircuitState
	cfg := NewCircuitBreakerConfig().
		WithThreshold(3).
		WithOnStateChange(func(from, to CircuitState) {
			transitions = append(transitions, from, to)
		})
	cb := NewCircuitBreaker(cfg)

	fail := errors.New("fail")
	for i := 0; i < 3; i++ {
		_ = cb.Execute(func() error { return fail })
	}
	if cb.State() != CircuitOpen {
		t.Fatalf("expected open after 3 failures, got %s", cb.State())
	}
	// Next call must be rejected without invoking fn.
	invoked := false
	err := cb.Execute(func() error { invoked = true; return nil })
	if err != ErrCircuitOpen {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
	if invoked {
		t.Fatal("fn must not run when open")
	}
	// Transition hook: Closed→Open fired exactly once.
	if len(transitions) != 2 || transitions[0] != CircuitClosed || transitions[1] != CircuitOpen {
		t.Fatalf("unexpected transitions: %v", transitions)
	}
}

func TestCircuitBreaker_HalfOpenRecovery(t *testing.T) {
	cfg := NewCircuitBreakerConfig().
		WithThreshold(2).
		WithResetTimeout(20 * time.Millisecond)
	cb := NewCircuitBreaker(cfg)

	fail := errors.New("fail")
	_ = cb.Execute(func() error { return fail })
	_ = cb.Execute(func() error { return fail }) // trips to Open
	if cb.State() != CircuitOpen {
		t.Fatalf("expected open, got %s", cb.State())
	}

	time.Sleep(30 * time.Millisecond) // cool-down elapses

	// State() lazily transitions to HalfOpen.
	if cb.State() != CircuitHalfOpen {
		t.Fatalf("expected half-open after cool-down, got %s", cb.State())
	}

	// Trial call succeeds → Closed.
	err := cb.Execute(func() error { return nil })
	if err != nil {
		t.Fatalf("trial call should succeed, got %v", err)
	}
	if cb.State() != CircuitClosed {
		t.Fatalf("expected closed after successful trial, got %s", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	cfg := NewCircuitBreakerConfig().
		WithThreshold(2).
		WithResetTimeout(10 * time.Millisecond)
	cb := NewCircuitBreaker(cfg)

	fail := errors.New("fail")
	_ = cb.Execute(func() error { return fail })
	_ = cb.Execute(func() error { return fail })
	if cb.State() != CircuitOpen {
		t.Fatalf("expected open, got %s", cb.State())
	}
	time.Sleep(15 * time.Millisecond)

	// Trial call fails → back to Open.
	err := cb.Execute(func() error { return fail })
	if err != fail {
		t.Fatalf("trial should return the underlying error, got %v", err)
	}
	if cb.State() != CircuitOpen {
		t.Fatalf("expected open after failed trial, got %s", cb.State())
	}
}

func TestCircuitBreaker_NonFailureErrorIgnored(t *testing.T) {
	cfg := NewCircuitBreakerConfig().
		WithThreshold(2).
		WithIsFailure(func(err error) bool { return false }) // no error counts as failure
	cb := NewCircuitBreaker(cfg)

	// These errors don't count as failures → breaker stays closed.
	for i := 0; i < 5; i++ {
		_ = cb.Execute(func() error { return errors.New("benign") })
	}
	if cb.State() != CircuitClosed {
		t.Fatalf("non-failure errors must not trip the breaker, got %s", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenSingleTrial(t *testing.T) {
	cfg := NewCircuitBreakerConfig().
		WithThreshold(1).
		WithResetTimeout(10 * time.Millisecond)
	cb := NewCircuitBreaker(cfg)

	fail := errors.New("fail")
	_ = cb.Execute(func() error { return fail }) // trips immediately (threshold=1)
	time.Sleep(15 * time.Millisecond)

	// First caller gets the trial slot; concurrent callers are rejected.
	var wg sync.WaitGroup
	var gotErr int64
	var gotOK int64
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Block the trial holder so the slot stays reserved.
			err := cb.Execute(func() error {
				time.Sleep(50 * time.Millisecond)
				return nil
			})
			if err == ErrCircuitOpen {
				gotErr++
			} else if err == nil {
				gotOK++
			}
		}()
	}
	wg.Wait()
	// Exactly one trial proceeds to completion; the rest are rejected. But
	// after the trial resolves (Closed), subsequent callers in the burst may
	// also succeed since the breaker is closed again. We only assert that at
	// least one was rejected (proving the single-trial gate works).
	if gotErr == 0 {
		t.Fatal("expected at least one rejected call during half-open trial")
	}
	if gotOK == 0 {
		t.Fatal("expected at least one successful call")
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker(NewCircuitBreakerConfig().WithThreshold(1))
	_ = cb.Execute(func() error { return errors.New("fail") })
	if cb.State() != CircuitOpen {
		t.Fatalf("expected open, got %s", cb.State())
	}
	cb.Reset()
	if cb.State() != CircuitClosed {
		t.Fatalf("expected closed after Reset, got %s", cb.State())
	}
}
