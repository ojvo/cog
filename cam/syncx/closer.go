package syncx

import (
	"errors"
	"sync"
	"sync/atomic"
)

// ErrClosed is the default error carried by a Closer after Close is called.
var ErrClosed = errors.New("syncx: closed")

// Closer is a stateful close signal. It carries an optional error, an optional
// close hook, and a done channel that is closed exactly once. The zero value
// is not usable; construct via NewCloser or NewClosedCloser.
//
// Concurrent Close / CloseWithErr / Reset calls are safe. Done returns a
// channel that is closed when the Closer transitions to closed; Reset
// re-opens it.
type Closer struct {
	closed    uint32                // 0=open, 1=closed
	err       error                 // error carried on close
	closeFunc func(err error) error // optional hook invoked under mu on close
	done      chan struct{}         // closed when closed==1
	mu        sync.Mutex
}

// NewCloser creates an open Closer.
func NewCloser() *Closer {
	return &Closer{done: make(chan struct{})}
}

// NewClosedCloser creates a Closer that is already closed carrying err. Useful
// for representing an already-failed resource.
func NewClosedCloser(err error) *Closer {
	c := &Closer{
		closed: 1,
		err:    err,
		done:   make(chan struct{}),
	}
	close(c.done)
	return c
}

// Done returns a channel that is closed when the Closer is closed. Blocking
// on the returned channel is the canonical way to wait for shutdown.
func (c *Closer) Done() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.done
}

// Err returns the error carried on close, or nil if still open. Safe for
// concurrent use.
func (c *Closer) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Closed reports whether the Closer is closed. A nil *Closer is considered
// closed, enabling the idiom `if x == nil || x.Closed() { ... }`.
func (c *Closer) Closed() bool {
	if c == nil {
		return true
	}
	return atomic.LoadUint32(&c.closed) == 1
}

// Close transitions the Closer to closed carrying ErrClosed. Idempotent.
// Returns the result of the close hook (if set) on the transition that
// actually closed; returns nil on subsequent calls.
func (c *Closer) Close() error {
	return c.CloseWithErr(ErrClosed)
}

// CloseWithErr transitions the Closer to closed carrying err. If err is nil,
// the call is a no-op (a nil error cannot close the Closer). Idempotent for
// the same non-nil err. Returns the close hook result on the transitioning
// call.
func (c *Closer) CloseWithErr(err error) error {
	if err == nil {
		return nil
	}
	if !atomic.CompareAndSwapUint32(&c.closed, 0, 1) {
		return nil
	}
	c.mu.Lock()
	c.err = err
	close(c.done)
	hook := c.closeFunc
	c.mu.Unlock()
	if hook != nil {
		return hook(err)
	}
	return nil
}

// SetCloseFunc registers a hook invoked on the next successful Close. The hook
// receives the closing error and may return its own error to the caller of
// Close. Must be called before Close; concurrent calls with Close are safe
// but the hook may not run if Close wins the race.
func (c *Closer) SetCloseFunc(fn func(err error) error) *Closer {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeFunc = fn
	return c
}

// Reset reopens the Closer, clearing the carried error and recreating the
// done channel. If the Closer is already open, Reset is a no-op. Reset is
// safe for concurrent use with Done and Err; callers observing Done() must
// re-read Done() after Reset to get the new channel.
func (c *Closer) Reset() {
	if !atomic.CompareAndSwapUint32(&c.closed, 1, 0) {
		return
	}
	c.mu.Lock()
	c.err = nil
	c.done = make(chan struct{})
	c.mu.Unlock()
}
