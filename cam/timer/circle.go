package timer

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"
)

// Circle is a lightweight periodic callback runner with runtime-adjustable
// interval. Call Start to begin the loop, Reset to change the interval, and
// Stop to shut down. After Stop, the Circle cannot be restarted; callbacks
// panic safely via stderr.
type Circle struct {
	interval time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	stopped  bool
	started  bool
	done     chan struct{}
	fn       func()
}

// NewCircle creates a Circle that calls fn every interval after Start.
// Minimum interval is 1ms.
func NewCircle(interval time.Duration, fn func()) *Circle {
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Circle{interval: interval, ctx: ctx, cancel: cancel, done: make(chan struct{}), fn: fn}
}

// Start begins the periodic callback loop. Returns an error if called after
// Stop (Stop is a terminal state; the Circle cannot be restarted).
func (c *Circle) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return fmt.Errorf("circle: Start after Stop")
	}
	if c.started {
		return fmt.Errorf("circle: Start called more than once")
	}
	c.started = true
	go c.loop()
	return nil
}

// Reset changes the interval to d. The new interval takes effect after the
// current callback cycle completes.
func (c *Circle) Reset(d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return fmt.Errorf("circle: Reset after Stop")
	}
	if d < time.Millisecond {
		d = time.Millisecond
	}
	c.interval = d
	return nil
}

// Stop terminates the periodic loop. Blocks until the goroutine exits.
// Safe for concurrent use. Calling Stop before Start is a no-op.
func (c *Circle) Stop() {
	c.mu.Lock()
	if c.stopped || !c.started {
		c.mu.Unlock()
		return
	}
	c.stopped = true
	c.mu.Unlock()
	c.cancel()
	<-c.done
}

func (c *Circle) loop() {
	defer close(c.done)
	for {
		c.mu.Lock()
		if c.stopped {
			c.mu.Unlock()
			return
		}
		interval := c.interval
		c.mu.Unlock()

		timer := time.NewTimer(interval)
		select {
		case <-c.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		c.doTick()
	}
}

func (c *Circle) doTick() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "circle: panic recovered: %v\n", r)
		}
	}()
	c.fn()
}
