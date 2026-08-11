package syncx

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCloser_BasicClose(t *testing.T) {
	c := NewCloser()
	if c.Closed() {
		t.Error("new Closer should not be closed")
	}
	go func() {
		<-time.After(10 * time.Millisecond)
		c.Close()
	}()
	<-c.Done()
	if !c.Closed() {
		t.Error("Closer should be closed after Close")
	}
	if err := c.Err(); err != ErrClosed {
		t.Errorf("Err = %v, want ErrClosed", err)
	}
}

func TestCloser_Close_Idempotent(t *testing.T) {
	c := NewCloser()
	if err := c.Close(); err != nil {
		t.Errorf("first Close should return nil, got %v", err)
	}
	// Second Close returns nil (no-op for the same transition).
	if err := c.Close(); err != nil {
		t.Errorf("second Close should return nil, got %v", err)
	}
	if !c.Closed() {
		t.Error("Closer should still be closed")
	}
}

func TestCloser_CloseWithErr_Nil_NoOp(t *testing.T) {
	c := NewCloser()
	if err := c.CloseWithErr(nil); err != nil {
		t.Errorf("CloseWithErr(nil) should return nil, got %v", err)
	}
	if c.Closed() {
		t.Error("CloseWithErr(nil) should not close the Closer")
	}
}

func TestCloser_CloseWithErr_Custom(t *testing.T) {
	c := NewCloser()
	sentinel := errors.New("boom")
	if err := c.CloseWithErr(sentinel); err != nil {
		t.Errorf("CloseWithErr should return nil (no hook), got %v", err)
	}
	if !c.Closed() {
		t.Error("Closer should be closed")
	}
	if err := c.Err(); err != sentinel {
		t.Errorf("Err = %v, want %v", err, sentinel)
	}
}

func TestCloser_NewClosedCloser(t *testing.T) {
	sentinel := errors.New("already failed")
	c := NewClosedCloser(sentinel)
	if !c.Closed() {
		t.Error("NewClosedCloser should be closed")
	}
	select {
	case <-c.Done():
	default:
		t.Error("Done channel should be already closed")
	}
	if err := c.Err(); err != sentinel {
		t.Errorf("Err = %v, want %v", err, sentinel)
	}
}

func TestCloser_SetCloseFunc(t *testing.T) {
	c := NewCloser()
	hookErr := errors.New("hook fired")
	called := false
	c.SetCloseFunc(func(err error) error {
		called = true
		if err != ErrClosed {
			t.Errorf("hook received %v, want ErrClosed", err)
		}
		return hookErr
	})
	if err := c.Close(); err != hookErr {
		t.Errorf("Close should return hook error %v, got %v", hookErr, err)
	}
	if !called {
		t.Error("close hook was not invoked")
	}
}

func TestCloser_Reset(t *testing.T) {
	c := NewCloser()
	c.Close()
	if !c.Closed() {
		t.Error("should be closed before Reset")
	}
	c.Reset()
	if c.Closed() {
		t.Error("should be open after Reset")
	}
	if err := c.Err(); err != nil {
		t.Errorf("Err should be nil after Reset, got %v", err)
	}
	// The Done channel after Reset should be a fresh open channel.
	select {
	case <-c.Done():
		t.Error("Done should not be closed after Reset")
	default:
	}
	// Reset on open Closer is a no-op.
	c.Reset()
	if c.Closed() {
		t.Error("Reset on open Closer should not close it")
	}
}

func TestCloser_NilPointer_Closed(t *testing.T) {
	var c *Closer
	if !c.Closed() {
		t.Error("nil *Closer should be considered closed")
	}
}

func TestCloser_ConcurrentClose(t *testing.T) {
	c := NewCloser()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Close()
		}()
	}
	wg.Wait()
	if !c.Closed() {
		t.Error("should be closed after concurrent Close")
	}
	// Done channel should be closed exactly once (not panic).
	<-c.Done()
}

// TestCloser_ResetReturnsNewDoneChannel verifies that Done() after Reset
// returns a fresh open channel, and the old channel remains closed.
func TestCloser_ResetReturnsNewDoneChannel(t *testing.T) {
	c := NewCloser()
	ch1 := c.Done()
	c.Close()
	<-ch1 // ch1 is closed
	c.Reset()
	ch2 := c.Done()
	// ch1 should still be closed.
	select {
	case <-ch1:
	default:
		t.Error("old Done channel should remain closed after Reset")
	}
	// ch2 should be open.
	select {
	case <-ch2:
		t.Error("new Done channel should be open after Reset")
	default:
	}
}

// TestCloser_ConcurrentDoneAndReset verifies that concurrent Done() and
// Reset() calls do not race on the done channel field.
func TestCloser_ConcurrentDoneAndReset(t *testing.T) {
	c := NewCloser()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = c.Done()
		}()
		go func() {
			defer wg.Done()
			c.Reset()
		}()
	}
	wg.Wait()
}
