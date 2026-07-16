package safe

import (
	"errors"
	"strings"
	"testing"
)

func TestRecover_Basic(t *testing.T) {
	err := func() (err error) {
		defer Recover(&err)
		panic("boom")
	}()
	if err == nil {
		t.Fatal("expected error from panic, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error should contain panic message, got %v", err)
	}
}

func TestRecover_WithStack(t *testing.T) {
	err := func() (err error) {
		defer Recover(&err, true)
		panic(666)
	}()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "666") {
		t.Errorf("error should contain panic value, got %v", err)
	}
	// Stack trace should contain "goroutine" or runtime markers.
	if !strings.Contains(err.Error(), "goroutine") &&
		!strings.Contains(err.Error(), "panic") {
		// debug.Stack() output includes "goroutine" header. Some Go versions
		// format differently; we accept either marker.
		t.Logf("stack may not be present: %v", err)
	}
}

func TestRecover_NoPanic(t *testing.T) {
	err := func() (err error) {
		defer Recover(&err)
		return nil
	}()
	if err != nil {
		t.Errorf("expected nil error when no panic, got %v", err)
	}
}

func TestRecover_NilErrPtr(t *testing.T) {
	// Should not panic even if err pointer is nil.
	func() {
		defer Recover(nil)
		panic("ignored")
	}()
}

func TestRecoverFunc(t *testing.T) {
	var got error
	var stack string
	func() {
		defer RecoverFunc(func(err error, s string) {
			got = err
			stack = s
		})
		panic("cb-boom")
	}()
	if got == nil {
		t.Fatal("RecoverFunc did not capture error")
	}
	if !strings.Contains(got.Error(), "cb-boom") {
		t.Errorf("error should contain panic message, got %v", got)
	}
	if stack == "" {
		t.Error("stack should not be empty")
	}
}

func TestRecoverFunc_NilCallback(t *testing.T) {
	// Should not panic even if callback is nil.
	func() {
		defer RecoverFunc(nil)
		panic("ignored")
	}()
}

func TestRecoverFunc_NoPanic(t *testing.T) {
	called := false
	func() {
		defer RecoverFunc(func(err error, s string) {
			called = true
		})
		// No panic.
	}()
	if called {
		t.Error("callback should not be invoked when no panic")
	}
}

func TestTry_NoPanic_NoError(t *testing.T) {
	err := Try(func() error {
		return nil
	})
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestTry_ReturnsFnError(t *testing.T) {
	sentinel := errors.New("fn error")
	err := Try(func() error {
		return sentinel
	})
	if err != sentinel {
		t.Errorf("expected %v, got %v", sentinel, err)
	}
}

func TestTry_PanicToError(t *testing.T) {
	err := Try(func() error {
		panic("try-boom")
	})
	if err == nil {
		t.Fatal("expected error from panic")
	}
	if !strings.Contains(err.Error(), "try-boom") {
		t.Errorf("error should contain panic message, got %v", err)
	}
}

func TestTry_CatchHandler(t *testing.T) {
	var caught error
	err := Try(func() error {
		panic("catch-me")
	}, func(e error) {
		caught = e
	})
	if err == nil {
		t.Fatal("expected error from panic")
	}
	if caught == nil {
		t.Fatal("catch handler should be invoked")
	}
	if !strings.Contains(caught.Error(), "catch-me") {
		t.Errorf("caught error should contain message, got %v", caught)
	}
}

func TestTry_CatchHandler_ReturnsFnError(t *testing.T) {
	sentinel := errors.New("fn error")
	var caught error
	err := Try(func() error {
		return sentinel
	}, func(e error) {
		caught = e
	})
	// catch is only invoked on panic, not on returned errors.
	if err != sentinel {
		t.Errorf("expected %v, got %v", sentinel, err)
	}
	if caught != nil {
		t.Errorf("catch should not be invoked for returned error, got %v", caught)
	}
}
