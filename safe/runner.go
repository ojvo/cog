package safe

import (
	"context"
	"sync"
	"sync/atomic"
)

// Runner is a single-shot task runner. At most one Run is active at a time;
// concurrent Run calls are no-ops. Start runs in a goroutine; Stop cancels
// the context passed to the handler; Restart stops (optionally waiting) then
// starts again.
//
// The zero value is not usable; construct via NewRunner or
// NewRunnerWithContext.
type Runner struct {
	running uint32
	fn      func(ctx context.Context) error
	parent  context.Context
	cancel  context.CancelFunc
	stop    chan struct{}
	mu      sync.Mutex
}

// NewRunner creates a Runner with context.Background.
func NewRunner(fn func(ctx context.Context) error) *Runner {
	return NewRunnerWithContext(context.Background(), fn)
}

// NewRunnerWithContext creates a Runner whose handler context is derived from
// ctx.
func NewRunnerWithContext(ctx context.Context, fn func(ctx context.Context) error) *Runner {
	return &Runner{
		fn:     fn,
		parent: ctx,
		stop:   make(chan struct{}),
	}
}

// SetFunc replaces the handler. Returns the Runner for chaining.
func (r *Runner) SetFunc(fn func(ctx context.Context) error) *Runner {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fn = fn
	return r
}

// Done returns a channel that is closed when the current run finishes. The
// channel is recreated on each Run.
func (r *Runner) Done() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stop == nil {
		r.stop = make(chan struct{})
		close(r.stop)
	}
	return r.stop
}

// Running reports whether a handler is currently executing.
func (r *Runner) Running() bool {
	return atomic.LoadUint32(&r.running) == 1
}

// Run executes the handler synchronously. Returns nil without invoking fn if
// a run is already in progress. Panics inside fn are recovered into the
// returned error.
func (r *Runner) Run() (err error) {
	if !atomic.CompareAndSwapUint32(&r.running, 0, 1) {
		return nil
	}

	r.mu.Lock()
	// Recreate stop if the previous run already closed it.
	select {
	case <-r.stop:
		r.stop = make(chan struct{})
	default:
		if r.stop == nil {
			r.stop = make(chan struct{})
		}
	}
	ctx, cancel := context.WithCancel(r.parent)
	r.cancel = cancel
	fn := r.fn
	r.mu.Unlock()

	return func() (runErr error) {
		defer atomic.StoreUint32(&r.running, 0)
		defer Recover(&runErr)
		defer func() {
			r.mu.Lock()
			if r.stop != nil {
				select {
				case <-r.stop:
				default:
					close(r.stop)
				}
			}
			r.cancel = nil
			r.mu.Unlock()
		}()
		if fn != nil {
			return fn(ctx)
		}
		return nil
	}()
}

// Start runs the handler in a goroutine. Equivalent to `go r.Run()`.
func (r *Runner) Start() {
	go r.Run()
}

// Stop cancels the handler's context. If wait is true and a handler is
// running, blocks until it finishes.
func (r *Runner) Stop(wait ...bool) {
	r.mu.Lock()
	cancel := r.cancel
	r.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if len(wait) > 0 && wait[0] && r.Running() {
		<-r.Done()
	}
}

// Restart stops the current run (waiting for it to finish) and starts a new
// one in a goroutine.
func (r *Runner) Restart() {
	r.Stop(true)
	r.Start()
}
