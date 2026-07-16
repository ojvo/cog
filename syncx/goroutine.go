package syncx

import (
	"context"
	"sync"
	"sync/atomic"
)

// Goroutine is a goroutine group: it launches goroutines sharing a context
// and tracks active/total counts. Done returns a channel closed when all
// launched goroutines have exited. Stop cancels the context.
//
// The zero value is not usable; construct via NewGoroutine or
// NewGoroutineWithContext.
type Goroutine struct {
	total  uint64
	active int32
	done   chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
}

// NewGoroutine creates a Goroutine group with context.Background.
func NewGoroutine() *Goroutine {
	return NewGoroutineWithContext(context.Background())
}

// NewGoroutineWithContext creates a Goroutine group derived from ctx. The
// group's context is canceled by Stop.
func NewGoroutineWithContext(ctx context.Context) *Goroutine {
	derived, cancel := context.WithCancel(ctx)
	return &Goroutine{
		ctx:    derived,
		cancel: cancel,
	}
}

// Go launches fs as goroutines sharing the group's context. If the group was
// idle (no active goroutines) and the previous done channel was closed, a new
// done channel is created so subsequent Done/Wait calls observe the new wave.
// Panics inside fs are recovered and discarded.
func (g *Goroutine) Go(fs ...func(ctx context.Context)) {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, f := range fs {
		atomic.AddUint64(&g.total, 1)

		// If the previous wave has finished (done closed) or never started,
		// (re)create the done channel so Wait blocks until this wave exits.
		if g.active == 0 {
			select {
			case <-g.done:
				g.done = make(chan struct{})
			default:
				if g.done == nil {
					g.done = make(chan struct{})
				}
			}
		}

		g.active++
		go g.run(f)
	}
}

func (g *Goroutine) run(f func(ctx context.Context)) {
	defer func() {
		recover()
		g.mu.Lock()
		g.active--
		if g.active == 0 && g.done != nil {
			close(g.done)
		}
		g.mu.Unlock()
	}()
	f(g.ctx)
}

// Done returns a channel that is closed when no goroutines are active. The
// returned channel is valid until the next Go call after a quiescent period;
// long-lived observers should re-read Done after each close.
func (g *Goroutine) Done() <-chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.done == nil {
		g.done = make(chan struct{})
		close(g.done)
	}
	return g.done
}

// Wait blocks until all active goroutines have exited.
func (g *Goroutine) Wait() {
	<-g.Done()
}

// Active returns the number of currently running goroutines.
func (g *Goroutine) Active() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return int(g.active)
}

// Total returns the cumulative number of goroutines ever launched.
func (g *Goroutine) Total() uint64 {
	return atomic.LoadUint64(&g.total)
}

// Stop cancels the group's context. If wait is true, blocks until all
// goroutines have exited. The ctx passed to running goroutines will be
// canceled; goroutines must observe ctx.Done() to terminate promptly.
func (g *Goroutine) Stop(wait ...bool) {
	g.cancel()
	if len(wait) > 0 && wait[0] {
		g.Wait()
	}
}
