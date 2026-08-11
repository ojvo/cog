package resil

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- Do basic tests ---

func TestDo_Success(t *testing.T) {
	err := Do(func() error { return nil })
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestDo_RetriesUntilSuccess(t *testing.T) {
	count := 0
	err := Do(func() error {
		count++
		if count < 3 {
			return fmt.Errorf("fail %d", count)
		}
		return nil
	}, Attempts(5), Delay(0))
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 calls, got %d", count)
	}
}

func TestDo_ExhaustsAttempts(t *testing.T) {
	err := Do(func() error { return errors.New("fail") }, Attempts(3), Delay(0))
	if err == nil {
		t.Fatal("expected error")
	}
	// Should be an aggregated doError with 3 entries.
	if _, ok := err.(doError); !ok {
		t.Errorf("expected doError, got %T", err)
	}
	if len(err.(doError)) != 3 {
		t.Errorf("expected 3 errors, got %d", len(err.(doError)))
	}
}

func TestDo_LastErrorOnly(t *testing.T) {
	err := Do(func() error { return errors.New("fail") },
		Attempts(3), Delay(0), LastErrorOnly(true))
	if err == nil {
		t.Fatal("expected error")
	}
	// Should NOT be doError; just the last error.
	if _, ok := err.(doError); ok {
		t.Error("lastErrorOnly should return the unwrapped last error")
	}
	if err.Error() != "fail" {
		t.Errorf("expected 'fail', got %q", err.Error())
	}
}

// --- DoWithData ---

func TestDoWithData_Success(t *testing.T) {
	val, err := DoWithData(func() (int, error) { return 42, nil })
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if val != 42 {
		t.Errorf("expected 42, got %d", val)
	}
}

func TestDoWithData_RetriesUntilSuccess(t *testing.T) {
	count := 0
	val, err := DoWithData(func() (string, error) {
		count++
		if count < 3 {
			return "", errors.New("fail")
		}
		return "ok", nil
	}, Attempts(5), Delay(0))
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if val != "ok" {
		t.Errorf("expected 'ok', got %q", val)
	}
}

func TestDoWithData_ExhaustsAttempts(t *testing.T) {
	val, err := DoWithData(func() (int, error) { return 0, errors.New("fail") },
		Attempts(2), Delay(0))
	if err == nil {
		t.Fatal("expected error")
	}
	if val != 0 {
		t.Errorf("expected zero value, got %d", val)
	}
}

// --- Unrecoverable ---

func TestUnrecoverable_StopsRetry(t *testing.T) {
	count := 0
	err := Do(func() error {
		count++
		if count == 2 {
			return Unrecoverable(errors.New("fatal"))
		}
		return errors.New("normal")
	}, Attempts(10), Delay(0))
	if err == nil {
		t.Fatal("expected error")
	}
	if count != 2 {
		t.Errorf("expected 2 calls (stopped by unrecoverable), got %d", count)
	}
	// unpackUnrecoverable strips the wrapper, so the inner error is "fatal".
	if !strings.Contains(err.Error(), "fatal") {
		t.Errorf("error should contain 'fatal', got %v", err)
	}
}

func TestIsRecoverable(t *testing.T) {
	if !IsRecoverable(errors.New("normal")) {
		t.Error("normal error should be recoverable")
	}
	if IsRecoverable(Unrecoverable(errors.New("fatal"))) {
		t.Error("unrecoverable error should not be recoverable")
	}
}

// --- RetryIf ---

func TestRetryIf_CustomPredicate(t *testing.T) {
	count := 0
	special := errors.New("special")
	_ = Do(func() error {
		count++
		if count < 3 {
			return special
		}
		return errors.New("other")
	}, Attempts(10), Delay(0), RetryIf(func(err error) bool {
		return !errors.Is(err, special) // don't retry "special"
	}))
	// First call returns "special", RetryIf returns false → stop.
	if count != 1 {
		t.Errorf("expected 1 call (stopped by RetryIf), got %d", count)
	}
}

func TestRetryIf_StopsOnPredicate(t *testing.T) {
	count := 0
	_ = Do(func() error {
		count++
		return errors.New("fail")
	}, Attempts(10), Delay(0), RetryIf(func(err error) bool {
		return count < 3
	}))
	if count != 3 {
		t.Errorf("expected 3 calls, got %d", count)
	}
}

// --- OnRetry ---

func TestOnRetry_Callback(t *testing.T) {
	var attempts []uint
	var errs []error
	count := 0
	err := Do(func() error {
		count++
		if count < 4 {
			return fmt.Errorf("fail %d", count)
		}
		return nil
	}, Attempts(10), Delay(0), OnRetry(func(n uint, err error) {
		attempts = append(attempts, n)
		errs = append(errs, err)
	}))
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if len(attempts) != 3 {
		t.Fatalf("expected 3 callbacks, got %d", len(attempts))
	}
	if attempts[0] != 0 || attempts[1] != 1 || attempts[2] != 2 {
		t.Errorf("callback attempts = %v, want [0 1 2]", attempts)
	}
}

// --- Context cancellation ---

func TestDo_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	count := 0
	errCh := make(chan error, 1)
	go func() {
		errCh <- Do(func() error {
			count++
			return errors.New("fail")
		}, Context(ctx), Attempts(0), Delay(50*time.Millisecond))
	}()
	time.Sleep(80 * time.Millisecond)
	cancel()
	err := <-errCh
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestDo_ContextAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Do(func() error { return nil }, Context(ctx))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestDo_WrapContextErrorWithLastError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- Do(func() error { return errors.New("last") },
			Context(ctx), Attempts(0), Delay(50*time.Millisecond),
			WrapContextErrorWithLastError(true))
	}()
	time.Sleep(80 * time.Millisecond)
	cancel()
	err := <-errCh
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("should wrap context error, got %v", err)
	}
	if !strings.Contains(err.Error(), "last") {
		t.Errorf("should contain last error, got %v", err)
	}
}

// --- AttemptsForError ---

func TestDo_AttemptsForError(t *testing.T) {
	sentinel := errors.New("sentinel")
	count := 0
	_ = Do(func() error {
		count++
		return sentinel
	}, Attempts(100), Delay(0), AttemptsForError(3, sentinel))
	if count != 3 {
		t.Errorf("expected 3 calls (limited by AttemptsForError), got %d", count)
	}
}

// --- Delay strategies ---

func TestBackOffDelay(t *testing.T) {
	config := &DoConfig{delay: 100 * time.Millisecond}
	d1 := BackOffDelay(0, nil, config)
	d2 := BackOffDelay(1, nil, config)
	d3 := BackOffDelay(2, nil, config)
	if d1 != 100*time.Millisecond {
		t.Errorf("BackOffDelay(0) = %v, want 100ms", d1)
	}
	if d2 != 200*time.Millisecond {
		t.Errorf("BackOffDelay(1) = %v, want 200ms", d2)
	}
	if d3 != 400*time.Millisecond {
		t.Errorf("BackOffDelay(2) = %v, want 400ms", d3)
	}
}

func TestFixedDelay(t *testing.T) {
	config := &DoConfig{delay: 50 * time.Millisecond}
	d := FixedDelay(0, nil, config)
	if d != 50*time.Millisecond {
		t.Errorf("FixedDelay = %v, want 50ms", d)
	}
	d2 := FixedDelay(100, nil, config)
	if d2 != 50*time.Millisecond {
		t.Errorf("FixedDelay(100) = %v, want 50ms", d2)
	}
}

func TestRandomDelay_ZeroJitter(t *testing.T) {
	config := &DoConfig{maxJitter: 0}
	d := RandomDelay(0, nil, config)
	if d != 0 {
		t.Errorf("RandomDelay with zero jitter should return 0, got %v", d)
	}
}

func TestCombineDelay(t *testing.T) {
	config := &DoConfig{delay: 100 * time.Millisecond}
	combined := CombineDelay(FixedDelay, BackOffDelay)
	// FixedDelay returns 100ms, BackOffDelay(0) returns 100ms → total 200ms
	d := combined(0, nil, config)
	if d != 200*time.Millisecond {
		t.Errorf("CombineDelay(0) = %v, want 200ms", d)
	}
}

func TestDo_MaxDelay(t *testing.T) {
	count := 0
	start := time.Now()
	_ = Do(func() error {
		count++
		return errors.New("fail")
	}, Attempts(3), Delay(50*time.Millisecond), MaxDelay(10*time.Millisecond),
		DelayType(FixedDelay))
	elapsed := time.Since(start)
	// With MaxDelay=10ms, total wait should be < 100ms (2 waits × 10ms).
	if elapsed > 100*time.Millisecond {
		t.Errorf("MaxDelay not respected, elapsed %v", elapsed)
	}
}

// --- Timer injection ---

type fakeTimer struct {
	ch chan time.Time
}

func (f *fakeTimer) After(_ time.Duration) <-chan time.Time {
	return f.ch
}

func TestDo_WithTimer(t *testing.T) {
	ft := &fakeTimer{ch: make(chan time.Time)}
	count := 0
	errCh := make(chan error, 1)
	go func() {
		errCh <- Do(func() error {
			count++
			if count < 3 {
				return errors.New("fail")
			}
			return nil
		}, Attempts(5), Delay(0), WithTimer(ft))
	}()
	// Advance the fake timer twice.
	ft.ch <- time.Time{}
	ft.ch <- time.Time{}
	err := <-errCh
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 calls, got %d", count)
	}
}

// --- Error type ---

func TestDoError_Error(t *testing.T) {
	e := doError{errors.New("a"), errors.New("b")}
	s := e.Error()
	if !strings.Contains(s, "#1: a") || !strings.Contains(s, "#2: b") {
		t.Errorf("Error() = %q", s)
	}
}

func TestDoError_Is(t *testing.T) {
	sentinel := errors.New("sentinel")
	e := doError{errors.New("other"), sentinel}
	if !errors.Is(e, sentinel) {
		t.Error("doError.Is should find sentinel")
	}
	if errors.Is(e, errors.New("absent")) {
		t.Error("doError.Is should not find absent error")
	}
}

func TestDoError_As(t *testing.T) {
	// doError wraps multiple errors; errors.As should find matches among them.
	e := doError{errors.New("a"), Unrecoverable(errors.New("b"))}
	var target unrecoverableError
	if !errors.As(e, &target) {
		t.Error("doError.As should find unrecoverableError")
	}
}

func TestDoError_Unwrap(t *testing.T) {
	first := errors.New("first")
	last := errors.New("last")
	e := doError{first, last}
	if unwrapped := errors.Unwrap(e); unwrapped != last {
		t.Errorf("Unwrap = %v, want %v", unwrapped, last)
	}
}

func TestDoError_WrappedErrors(t *testing.T) {
	a := errors.New("a")
	b := errors.New("b")
	e := doError{a, b}
	if w := e.WrappedErrors(); len(w) != 2 || w[0] != a || w[1] != b {
		t.Errorf("WrappedErrors = %v", w)
	}
}

// --- UntilSucceeded ---

func TestDo_UntilSucceeded(t *testing.T) {
	count := 0
	err := Do(func() error {
		count++
		if count < 5 {
			return errors.New("fail")
		}
		return nil
	}, UntilSucceeded(), Delay(0))
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5 calls, got %d", count)
	}
}

// --- Delay nil option ---

func TestDelayType_NilOption(t *testing.T) {
	opt := DelayType(nil)
	config := newDoConfig()
	opt(config) // should not panic
}

func TestOnRetry_NilOption(t *testing.T) {
	opt := OnRetry(nil)
	config := newDoConfig()
	opt(config) // should not panic
}

func TestRetryIf_NilOption(t *testing.T) {
	opt := RetryIf(nil)
	config := newDoConfig()
	opt(config) // should not panic
}
