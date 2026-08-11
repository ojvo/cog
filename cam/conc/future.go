package conc

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrFutureNotDone is returned when a Get call times out before the Future
// completes.
var ErrFutureNotDone = errors.New("future: not done")

// Future represents a value available asynchronously. A Future is created via
// NewFuture and completed exactly once via Complete. Get/GetWithContext block
// until completion, timeout, or cancellation.
//
// The zero value is not usable; construct via NewFuture.
//
// Design note: cup/ju's Future used sync.Cond with a complex timer/cond
// interaction that contained a race condition in Get(timeout). This
// implementation uses a closed-channel signal which is race-free and
// idiomatic.
type Future[T any] struct {
	done      chan struct{}
	result    T
	err       error
	callbacks []func(T, error)
	mu        sync.Mutex
	completed bool
}

// NewFuture creates an incomplete Future.
func NewFuture[T any]() *Future[T] {
	return &Future[T]{done: make(chan struct{})}
}

// Complete sets the result and marks the Future as done. The first call wins;
// subsequent calls are no-ops. Registered callbacks are invoked synchronously
// after the Future is marked done (in registration order). Returns true if
// this call completed the Future.
func (f *Future[T]) Complete(result T, err error) bool {
	f.mu.Lock()
	if f.completed {
		f.mu.Unlock()
		return false
	}
	f.completed = true
	f.result = result
	f.err = err
	callbacks := f.callbacks
	f.callbacks = nil
	close(f.done) // wake all waiters
	f.mu.Unlock()

	for _, cb := range callbacks {
		cb(result, err)
	}
	return true
}

// OnComplete registers a callback invoked when the Future completes.
// If the Future is already done, the callback is invoked asynchronously.
// Returns f for chaining.
func (f *Future[T]) OnComplete(cb func(T, error)) *Future[T] {
	if cb == nil {
		return f
	}
	f.mu.Lock()
	if f.completed {
		result, err := f.result, f.err
		f.mu.Unlock()
		go cb(result, err)
		return f
	}
	f.callbacks = append(f.callbacks, cb)
	f.mu.Unlock()
	return f
}

// OnSuccess registers a callback invoked when the Future completes without error.
func (f *Future[T]) OnSuccess(cb func(T)) *Future[T] {
	return f.OnComplete(func(r T, err error) {
		if err == nil {
			cb(r)
		}
	})
}

// OnError registers a callback invoked when the Future completes with an error.
func (f *Future[T]) OnError(cb func(error)) *Future[T] {
	return f.OnComplete(func(_ T, err error) {
		if err != nil {
			cb(err)
		}
	})
}

// Get blocks until the Future completes or timeout elapses.
// A non-positive timeout means wait indefinitely.
// Returns ErrFutureNotDone if the timeout elapses first.
func (f *Future[T]) Get(timeout time.Duration) (T, error) {
	if timeout <= 0 {
		<-f.done
		return f.result, f.err
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-f.done:
		return f.result, f.err
	case <-t.C:
		var zero T
		return zero, ErrFutureNotDone
	}
}

// GetWithContext blocks until the Future completes or ctx is cancelled.
// Returns the ctx error if ctx is cancelled first.
func (f *Future[T]) GetWithContext(ctx context.Context) (T, error) {
	select {
	case <-f.done:
		return f.result, f.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// IsDone reports whether the Future has completed.
func (f *Future[T]) IsDone() bool {
	select {
	case <-f.done:
		return true
	default:
		return false
	}
}

// Done returns a channel that is closed when the Future completes.
// Enables select-based composition.
func (f *Future[T]) Done() <-chan struct{} {
	return f.done
}

// GetResult returns the result and error if completed; ok is false otherwise.
// This is a non-blocking peek.
func (f *Future[T]) GetResult() (T, error, bool) {
	select {
	case <-f.done:
		return f.result, f.err, true
	default:
		var zero T
		return zero, nil, false
	}
}
