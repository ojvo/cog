package safe

import (
	"context"
	"sync"
	"sync/atomic"
)

// OneRun serializes task execution: at most one Run executes at a time, and
// each Run call cancels the in-flight run (if any) and waits for it to exit
// before starting the next. This makes Run safe to call from many goroutines
// when only the latest invocation should run to completion.
//
// The zero value is not usable; construct via NewOneRun.
type OneRun struct {
	cancel  context.CancelFunc
	running uint32
	mu      sync.Mutex
	fn      func(ctx context.Context) error
	done    chan struct{}
}

// NewOneRun creates a OneRun with the given handler. fn may be nil; Run will
// return ErrNoHandler until SetHandler installs one.
func NewOneRun(fn func(ctx context.Context) error) *OneRun {
	return &OneRun{fn: fn}
}

// SetHandler replaces the handler. Safe to call concurrently with Run.
func (o *OneRun) SetHandler(fn func(ctx context.Context) error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.fn = fn
}

// Run cancels the in-flight run (if any), waits for it to exit, then invokes
// the handler. Concurrent Run callers serialize behind the in-flight run.
// Returns ErrNoHandler if no handler is set.
func (o *OneRun) Run() (err error) {
	o.mu.Lock()
	if o.fn == nil {
		o.mu.Unlock()
		return ErrNoHandler
	}
	// Cancel any in-flight run.
	if o.cancel != nil {
		o.cancel()
	}
	// Wait for the previous run's done channel (returns immediately if nil
	// or already closed).
	if o.done != nil {
		prev := o.done
		o.mu.Unlock()
		<-prev
		o.mu.Lock()
	}
	o.done = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	o.cancel = cancel
	fn := o.fn
	o.mu.Unlock()

	atomic.StoreUint32(&o.running, 1)
	defer func() {
		atomic.StoreUint32(&o.running, 0)
		o.mu.Lock()
		select {
		case <-o.done:
			// already closed (defensive: concurrent Close path)
		default:
			close(o.done)
		}
		o.mu.Unlock()
	}()
	return fn(ctx)
}

// Running reports whether a handler is currently executing.
func (o *OneRun) Running() bool {
	return atomic.LoadUint32(&o.running) == 1
}

// Close cancels the in-flight run (if any). Does not wait. The OneRun can be
// reused via subsequent Run calls.
func (o *OneRun) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.cancel != nil {
		o.cancel()
		o.cancel = nil
	}
	return nil
}
