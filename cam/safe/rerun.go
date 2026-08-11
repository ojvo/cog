package safe

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

// Dialer is the contract Rerun drives. Dial establishes a connection, Run
// serves on it until error, and Close releases the connection.
type Dialer interface {
	Dial(ctx context.Context) error // establish a connection
	Run(ctx context.Context) error  // serve until error or ctx cancellation
	io.Closer                       // release this connection
}

// ErrNotDialed is the initial dialErr before the first successful Dial.
var ErrNotDialed = errors.New("safe: not dialed")

// Rerun drives a Dialer in a reconnect loop. Each iteration dials (with
// backoff via OnInterval), runs until error, then re-dials. The first dial
// result is surfaced via DialRun; subsequent failures are reported via Status.
//
// The zero value is not usable; construct via NewRerun.
type Rerun struct {
	dialed     bool
	dialErr    error
	onInterval func(retry int) time.Duration
	onDial     func(index, retry int, err error)
	firstErr   chan error
	mu         sync.Mutex
	*OneRun
}

// NewRerun creates a Rerun with a 10-second default reconnect interval.
func NewRerun() *Rerun {
	return &Rerun{
		dialed:     false,
		dialErr:    ErrNotDialed,
		onInterval: func(retry int) time.Duration { return 10 * time.Second },
		firstErr:   make(chan error),
		OneRun:     NewOneRun(nil),
	}
}

// OnDial registers a callback invoked after each Dial attempt with (index,
// retry, err). index is the connection attempt cycle; retry is the retry
// within a cycle. Must be called before Run.
func (r *Rerun) OnDial(fn func(index, retry int, err error)) {
	r.onDial = fn
}

// OnInterval registers a function returning the backoff before the next dial
// attempt given the retry count. Must be called before Run.
func (r *Rerun) OnInterval(fn func(retry int) time.Duration) {
	r.onInterval = fn
}

// DialRun starts the reconnect loop in the background and returns the first
// dial's error (nil on success). The loop continues until the OneRun is
// canceled via Close.
func (r *Rerun) DialRun(d Dialer) error {
	go r.Run(d)
	return <-r.firstErr
}

// Run drives the Dialer in a reconnect loop. It blocks until the underlying
// OneRun is canceled.
func (r *Rerun) Run(d Dialer) error {
	r.OneRun.SetHandler(func(ctx context.Context) error {
		for index := 0; ; index++ {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			// Dial loop: retry until success or cancellation.
			for retry := 0; ; retry++ {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
				err := d.Dial(ctx)
				if index == 0 && retry == 0 {
					select {
					case r.firstErr <- err:
					default:
					}
				}
				if r.onDial != nil {
					r.onDial(index, retry, err)
				}
				if err == nil {
					break
				}
				r.mu.Lock()
				r.dialed, r.dialErr = false, err
				r.mu.Unlock()
				// onInterval receives the 1-based retry count to match the
				// original semantics (first retry = 1, second = 2, ...).
				<-time.After(r.onInterval(retry + 1))
			}

			r.mu.Lock()
			r.dialed, r.dialErr = true, nil
			r.mu.Unlock()

			err := d.Run(ctx)

			r.mu.Lock()
			r.dialed, r.dialErr = false, err
			r.mu.Unlock()
		}
	})
	return r.OneRun.Run()
}

// Status reports the current dial state. dialed is true while the Dialer's Run
// is active; reason is the last dial/run error (empty when dialed).
func (r *Rerun) Status() (dialed bool, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	dialed = r.dialed
	if r.dialErr != nil {
		reason = r.dialErr.Error()
	}
	return
}
