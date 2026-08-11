package resil

import (
	"errors"
	"sync"
	"time"
)

// CircuitBreaker states.
type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	}
	return "unknown"
}

// ErrCircuitOpen is returned by Execute when the breaker is open and the
// reset timeout has not yet elapsed, so no call is attempted.
var ErrCircuitOpen = errors.New("circuit breaker open")

// CircuitBreakerConfig configures a CircuitBreaker.
//
//   - Threshold: consecutive failures (in Closed state) that trip the breaker
//     to Open. Must be > 0; defaults to 5.
//   - ResetTimeout: how long the breaker stays Open before transitioning to
//     HalfOpen (one trial call allowed). Defaults to 30s.
//   - IsFailure: classifies an error as a failure. Defaults to "any non-nil
//     error". Errors for which IsFailure returns false (e.g. context.Canceled)
//     do not count toward the trip threshold and do not reset the count.
//   - OnStateChange: optional hook invoked outside the breaker lock on every
//     state transition, with the previous and new states.
type CircuitBreakerConfig struct {
	Threshold     int
	ResetTimeout  time.Duration
	IsFailure     func(error) bool
	OnStateChange func(from, to CircuitState)
}

// NewCircuitBreakerConfig returns a config with sensible defaults.
func NewCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		Threshold:    5,
		ResetTimeout: 30 * time.Second,
		IsFailure:    func(err error) bool { return err != nil },
	}
}

// WithThreshold sets the consecutive-failure count that trips the breaker.
func (c *CircuitBreakerConfig) WithThreshold(n int) *CircuitBreakerConfig {
	c.Threshold = n
	return c
}

// WithResetTimeout sets the Open→HalfOpen cool-down duration.
func (c *CircuitBreakerConfig) WithResetTimeout(d time.Duration) *CircuitBreakerConfig {
	c.ResetTimeout = d
	return c
}

// WithIsFailure sets the failure classifier.
func (c *CircuitBreakerConfig) WithIsFailure(fn func(error) bool) *CircuitBreakerConfig {
	c.IsFailure = fn
	return c
}

// WithOnStateChange registers a state-transition observer.
func (c *CircuitBreakerConfig) WithOnStateChange(fn func(from, to CircuitState)) *CircuitBreakerConfig {
	c.OnStateChange = fn
	return c
}

// CircuitBreaker is a stateful circuit breaker that blocks calls to a failing
// downstream until it has had time to recover.
//
// State machine:
//
//	Closed   — requests flow through; consecutive failures are counted.
//	           On Threshold consecutive failures → Open.
//	Open     — requests are rejected immediately with ErrCircuitOpen.
//	           After ResetTimeout elapses → HalfOpen.
//	HalfOpen — one trial request is allowed. Success → Closed; failure → Open.
//
// CircuitBreaker is orthogonal to Retry and Fallback: Retry retries transient
// errors on the same call, Fallback switches to a backup resource, and
// CircuitBreaker stops calling an unhealthy resource entirely to let it
// recover. Compose by wrapping in order: Retry(inside) → Fallback(middle) →
// CircuitBreaker(outside).
type CircuitBreaker struct {
	config *CircuitBreakerConfig

	mu         sync.Mutex
	state      CircuitState
	failures   int
	openedAt   time.Time
	halfOpenIn bool // true while a HalfOpen trial call is in flight
}

// NewCircuitBreaker creates a breaker. config nil → defaults.
func NewCircuitBreaker(config *CircuitBreakerConfig) *CircuitBreaker {
	if config == nil {
		config = NewCircuitBreakerConfig()
	}
	if config.Threshold <= 0 {
		config.Threshold = 5
	}
	if config.ResetTimeout <= 0 {
		config.ResetTimeout = 30 * time.Second
	}
	if config.IsFailure == nil {
		config.IsFailure = func(err error) bool { return err != nil }
	}
	return &CircuitBreaker{config: config, state: CircuitClosed}
}

// State returns the current breaker state. The HalfOpen transition is
// evaluated lazily, so State may report Open until called after ResetTimeout
// has elapsed.
func (b *CircuitBreaker) State() CircuitState {
	b.mu.Lock()
	transitioned, from := b.maybeHalfOpenLocked()
	state := b.state
	b.mu.Unlock()
	b.notify(from, CircuitHalfOpen, transitioned)
	return state
}

// Execute runs fn if the breaker allows it, recording the outcome. Returns
// ErrCircuitOpen (without calling fn) when the breaker is open and the
// cool-down has not elapsed. In HalfOpen, only one concurrent trial call is
// allowed; additional callers receive ErrCircuitOpen until the trial resolves.
func (b *CircuitBreaker) Execute(fn func() error) error {
	allowed, openTransitioned, openFrom := b.allowCall()
	if !allowed {
		b.notify(openFrom, CircuitHalfOpen, openTransitioned)
		return ErrCircuitOpen
	}
	b.notify(openFrom, CircuitHalfOpen, openTransitioned)
	err := fn()
	b.record(err)
	return err
}

// Reset forces the breaker back to Closed and clears failure counters. Useful
// for tests or administrative intervention.
func (b *CircuitBreaker) Reset() {
	b.mu.Lock()
	prev := b.state
	b.state = CircuitClosed
	b.failures = 0
	b.openedAt = time.Time{}
	b.halfOpenIn = false
	b.mu.Unlock()
	b.notify(prev, CircuitClosed, prev != CircuitClosed)
}

// allowCall reports whether a call may proceed, transitioning Open→HalfOpen
// when the cool-down has elapsed and reserving the HalfOpen trial slot.
// Returns whether a (possibly) Open→HalfOpen transition occurred so the caller
// can fire the hook outside the lock.
func (b *CircuitBreaker) allowCall() (allowed bool, transitioned bool, from CircuitState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if transitioned, from = b.maybeHalfOpenLocked(); transitioned {
		// state is now HalfOpen (or unchanged); fall through to the switch.
	}
	switch b.state {
	case CircuitClosed:
		return true, transitioned, from
	case CircuitHalfOpen:
		if b.halfOpenIn {
			return false, transitioned, from
		}
		b.halfOpenIn = true
		return true, transitioned, from
	case CircuitOpen:
		return false, transitioned, from
	}
	return false, transitioned, from
}

// record updates the breaker state based on the call outcome.
func (b *CircuitBreaker) record(err error) {
	b.mu.Lock()
	failure := err != nil && b.config.IsFailure(err)
	prev := b.state
	next := prev
	switch b.state {
	case CircuitClosed:
		if err == nil {
			b.failures = 0
		} else if failure {
			b.failures++
			if b.failures >= b.config.Threshold {
				next = CircuitOpen
			}
		}
	case CircuitHalfOpen:
		b.halfOpenIn = false
		if err == nil {
			next = CircuitClosed
		} else if failure {
			next = CircuitOpen
		}
	}
	changed := next != prev
	if changed {
		b.applyTransitionLocked(next)
	}
	b.mu.Unlock()
	b.notify(prev, next, changed)
}

// applyTransitionLocked sets the state and resets associated counters for the
// target state. Caller must hold b.mu.
func (b *CircuitBreaker) applyTransitionLocked(to CircuitState) {
	b.state = to
	switch to {
	case CircuitClosed:
		b.failures = 0
		b.halfOpenIn = false
	case CircuitOpen:
		b.openedAt = time.Now()
		b.failures = 0
		b.halfOpenIn = false
	case CircuitHalfOpen:
		b.halfOpenIn = false
	}
}

// maybeHalfOpenLocked transitions Open→HalfOpen when ResetTimeout has elapsed.
// Returns (true, prevOpen) if the transition occurred. Caller must hold b.mu.
func (b *CircuitBreaker) maybeHalfOpenLocked() (bool, CircuitState) {
	if b.state == CircuitOpen && time.Since(b.openedAt) >= b.config.ResetTimeout {
		from := b.state
		b.applyTransitionLocked(CircuitHalfOpen)
		return true, from
	}
	return false, b.state
}

// notify fires the OnStateChange hook outside the lock to avoid deadlocks if
// the hook calls back into the breaker (e.g. logs State()).
func (b *CircuitBreaker) notify(from, to CircuitState, changed bool) {
	if changed && b.config.OnStateChange != nil {
		b.config.OnStateChange(from, to)
	}
}
