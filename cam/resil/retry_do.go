package resil

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"
)

// This file implements an advanced retry API with function-style options,
// complementing the simpler config-driven Retry / RetryHTTP functions in
// retry.go. The two APIs coexist:
//   - Retry / RetryHTTP: simple, config-object driven, returns the last error.
//   - Do / DoWithData: advanced, option driven, supports unrecoverable errors,
//     error aggregation, custom retry predicates, retry callbacks, injectable
//     timers, and composable delay strategies.

// RetryableFunc is a function that may be retried.
type RetryableFunc func() error

// RetryableFuncWithData is a function that returns a value and may be retried.
type RetryableFuncWithData[T any] func() (T, error)

// RetryIfFunc decides whether a retry should be attempted after err.
type RetryIfFunc func(error) bool

// OnRetryFunc is invoked after each failed attempt (before the delay).
type OnRetryFunc func(attempt uint, err error)

// DelayTypeFunc computes the delay before the next attempt given the attempt
// number n (1-based after the first failure), the error, and the config.
type DelayTypeFunc func(n uint, err error, config *DoConfig) time.Duration

// Timer abstracts the timer used to wait between attempts. The default
// implementation uses time.After; tests may inject a fake timer.
type Timer interface {
	After(time.Duration) <-chan time.Time
}

type timerImpl struct{}

func (timerImpl) After(d time.Duration) <-chan time.Time { return time.After(d) }

// DoConfig holds the resolved configuration for Do / DoWithData. It is
// constructed by applying Option values to a default config.
type DoConfig struct {
	attempts                      uint
	attemptsForError              map[error]uint
	delay                         time.Duration
	maxDelay                      time.Duration
	maxJitter                     time.Duration
	onRetry                       OnRetryFunc
	retryIf                       RetryIfFunc
	delayType                     DelayTypeFunc
	lastErrorOnly                 bool
	context                       context.Context
	timer                         Timer
	wrapContextErrorWithLastError bool

	// maxBackOffN caches the maximum shift used by BackOffDelay to avoid
	// overflowing time.Duration (int64). It is computed lazily on the first
	// BackOffDelay call. Access is not synchronized; DoConfig should not be
	// shared across concurrent Do calls.
	maxBackOffN uint
}

// Option configures a DoConfig.
type Option func(*DoConfig)

func emptyOption(*DoConfig) {}

// newDoConfig returns the default DoConfig.
func newDoConfig() *DoConfig {
	return &DoConfig{
		attempts:         uint(10),
		attemptsForError: make(map[error]uint),
		delay:            100 * time.Millisecond,
		maxJitter:        100 * time.Millisecond,
		onRetry:          func(uint, error) {},
		retryIf:          IsRecoverable,
		delayType:        CombineDelay(BackOffDelay, RandomDelay),
		lastErrorOnly:    false,
		context:          context.Background(),
		timer:            timerImpl{},
	}
}

// Do calls fn and retries until it succeeds, the attempt limit is reached, or
// the context is canceled. Returns nil on success or an error (possibly an
// Error aggregating all attempts) on failure.
func Do(fn RetryableFunc, opts ...Option) error {
	wrapped := func() (any, error) { return nil, fn() }
	_, err := DoWithData(wrapped, opts...)
	return err
}

// DoWithData calls fn and retries until it succeeds, the attempt limit is
// reached, or the context is canceled. Returns the successful value and nil
// on success, or the zero value and an error on failure.
func DoWithData[T any](fn RetryableFuncWithData[T], opts ...Option) (T, error) {
	var empty T

	config := newDoConfig()
	for _, opt := range opts {
		opt(config)
	}

	if err := context.Cause(config.context); err != nil {
		return empty, err
	}

	var n uint
	var lastErr error

	// attempts == 0 means retry until success.
	if config.attempts == 0 {
		for {
			val, err := fn()
			if err == nil {
				return val, nil
			}
			if !IsRecoverable(err) {
				return empty, err
			}
			if !config.retryIf(err) {
				return empty, err
			}
			lastErr = err
			config.onRetry(n, err)
			n++
			select {
			case <-config.timer.After(doDelay(config, n, err)):
			case <-config.context.Done():
				if config.wrapContextErrorWithLastError {
					return empty, doError{context.Cause(config.context), lastErr}
				}
				return empty, context.Cause(config.context)
			}
		}
	}

	errorLog := doError{}

	// Copy attemptsForError so we can decrement without mutating the original.
	attemptsForError := make(map[error]uint, len(config.attemptsForError))
	for err, attempts := range config.attemptsForError {
		attemptsForError[err] = attempts
	}

	shouldRetry := true
	for shouldRetry {
		val, err := fn()
		if err == nil {
			return val, nil
		}
		errorLog = append(errorLog, unpackUnrecoverable(err))
		if !config.retryIf(err) {
			break
		}
		config.onRetry(n, err)

		for errToCheck, attempts := range attemptsForError {
			if errors.Is(err, errToCheck) {
				attempts--
				attemptsForError[errToCheck] = attempts
				shouldRetry = shouldRetry && attempts > 0
			}
		}

		// If this is the last attempt, don't wait.
		if n == config.attempts-1 {
			break
		}
		n++
		select {
		case <-config.timer.After(doDelay(config, n, err)):
		case <-config.context.Done():
			if config.lastErrorOnly {
				return empty, context.Cause(config.context)
			}
			return empty, append(errorLog, context.Cause(config.context))
		}
		shouldRetry = shouldRetry && n < config.attempts
	}

	if config.lastErrorOnly {
		return empty, errorLog.Unwrap()
	}
	return empty, errorLog
}

// --- Options ---

// Attempts sets the total number of attempts (initial + retries). 0 means
// retry until success. Default is 10.
func Attempts(attempts uint) Option {
	return func(c *DoConfig) { c.attempts = attempts }
}

// UntilSucceeded retries until fn succeeds. Equivalent to Attempts(0).
func UntilSucceeded() Option {
	return func(c *DoConfig) { c.attempts = 0 }
}

// AttemptsForError limits retries for a specific error. The retry stops if
// the per-error budget is exhausted, even if the global attempt count has not
// been reached.
func AttemptsForError(attempts uint, err error) Option {
	return func(c *DoConfig) { c.attemptsForError[err] = attempts }
}

// Delay sets the base delay between retries. Default is 100ms.
func Delay(delay time.Duration) Option {
	return func(c *DoConfig) { c.delay = delay }
}

// MaxDelay caps the delay between retries. 0 means no cap.
func MaxDelay(maxDelay time.Duration) Option {
	return func(c *DoConfig) { c.maxDelay = maxDelay }
}

// MaxJitter sets the maximum random jitter used by RandomDelay. Default is
// 100ms.
func MaxJitter(maxJitter time.Duration) Option {
	return func(c *DoConfig) { c.maxJitter = maxJitter }
}

// DelayType sets the delay strategy. Default is CombineDelay(BackOffDelay,
// RandomDelay). Passing nil is a no-op.
func DelayType(delayType DelayTypeFunc) Option {
	if delayType == nil {
		return emptyOption
	}
	return func(c *DoConfig) { c.delayType = delayType }
}

// OnRetry registers a callback invoked after each failed attempt. Passing nil
// is a no-op.
func OnRetry(onRetry OnRetryFunc) Option {
	if onRetry == nil {
		return emptyOption
	}
	return func(c *DoConfig) { c.onRetry = onRetry }
}

// RetryIf sets a predicate that decides whether to retry after an error. The
// default is IsRecoverable. Passing nil is a no-op.
func RetryIf(retryIf RetryIfFunc) Option {
	if retryIf == nil {
		return emptyOption
	}
	return func(c *DoConfig) { c.retryIf = retryIf }
}

// Context sets the context used for cancellation. Default is
// context.Background.
func Context(ctx context.Context) Option {
	return func(c *DoConfig) { c.context = ctx }
}

// WithTimer injects a custom Timer, useful for testing.
func WithTimer(t Timer) Option {
	return func(c *DoConfig) { c.timer = t }
}

// LastErrorOnly, when true, makes Do return only the last error instead of an
// aggregated Error. Default is false.
func LastErrorOnly(lastErrorOnly bool) Option {
	return func(c *DoConfig) { c.lastErrorOnly = lastErrorOnly }
}

// WrapContextErrorWithLastError, when true and Attempts(0) is set, wraps the
// context cancellation error with the last error from fn. Default is false.
func WrapContextErrorWithLastError(wrap bool) Option {
	return func(c *DoConfig) { c.wrapContextErrorWithLastError = wrap }
}

// --- Delay strategies ---

// BackOffDelay exponentially backs off: delay << n. It guards against
// overflowing time.Duration by capping n at a precomputed maximum.
func BackOffDelay(n uint, _ error, config *DoConfig) time.Duration {
	const maxShift uint = 62 // 1<<63 would overflow signed int64

	if config.maxBackOffN == 0 {
		d := config.delay
		if d <= 0 {
			d = 1
		}
		config.maxBackOffN = maxShift - uint(math.Floor(math.Log2(float64(d))))
	}
	if n > config.maxBackOffN {
		n = config.maxBackOffN
	}
	return config.delay << n
}

// FixedDelay returns the same delay for every attempt.
func FixedDelay(_ uint, _ error, config *DoConfig) time.Duration {
	return config.delay
}

// RandomDelay returns a random delay in [0, maxJitter). If maxJitter is 0,
// returns 0 (avoids rand.Int63n(0) panic).
func RandomDelay(_ uint, _ error, config *DoConfig) time.Duration {
	if config.maxJitter <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(config.maxJitter)))
}

// CombineDelay sums the output of multiple delay strategies, capping at
// math.MaxInt64.
func CombineDelay(delays ...DelayTypeFunc) DelayTypeFunc {
	const maxInt64 = uint64(math.MaxInt64)
	return func(n uint, err error, config *DoConfig) time.Duration {
		var total uint64
		for _, d := range delays {
			total += uint64(d(n, err, config))
			if total > maxInt64 {
				total = maxInt64
			}
		}
		return time.Duration(total)
	}
}

// --- Unrecoverable errors ---

// unrecoverableError wraps an error to signal that retrying is futile. Do and
// DoWithData stop immediately when they encounter it.
type unrecoverableError struct{ error }

func (e unrecoverableError) Error() string {
	if e.error != nil {
		return e.error.Error()
	}
	return "unrecoverable error"
}

func (e unrecoverableError) Unwrap() error { return e.error }

// Unrecoverable marks err as unrecoverable. Do / DoWithData will not retry
// after this error.
func Unrecoverable(err error) error {
	return unrecoverableError{err}
}

// IsRecoverable reports whether err is not wrapped with Unrecoverable.
func IsRecoverable(err error) bool {
	return !errors.Is(err, unrecoverableError{})
}

// unrecoverableError satisfies errors.Is by type, so errors.Is(err,
// unrecoverableError{}) works for any unrecoverable wrapping.
func (unrecoverableError) Is(target error) bool {
	_, ok := target.(unrecoverableError)
	return ok
}

func unpackUnrecoverable(err error) error {
	if u, ok := err.(unrecoverableError); ok {
		return u.error
	}
	return err
}

// --- Error aggregation ---

// doError is an error that aggregates all attempt errors. It implements
// Is, As, and Unwrap so errors.Is / errors.As can inspect any attempt's
// error. Unwrap returns the last error for compatibility with errors.Unwrap.
type doError []error

// Error formats all attempt errors.
func (e doError) Error() string {
	lines := make([]string, len(e))
	for i, l := range e {
		if l != nil {
			lines[i] = fmt.Sprintf("#%d: %s", i+1, l.Error())
		}
	}
	return fmt.Sprintf("all attempts fail:\n%s", strings.Join(lines, "\n"))
}

func (e doError) Is(target error) bool {
	for _, v := range e {
		if errors.Is(v, target) {
			return true
		}
	}
	return false
}

func (e doError) As(target interface{}) bool {
	for _, v := range e {
		if errors.As(v, target) {
			return true
		}
	}
	return false
}

// Unwrap returns the last error. Use WrappedErrors to access all errors.
func (e doError) Unwrap() error {
	if len(e) == 0 {
		return nil
	}
	return e[len(e)-1]
}

// WrappedErrors returns all attempt errors. Implements the errwrap.Wrapper
// interface for compatibility with hashicorp/errwrap.
func (e doError) WrappedErrors() []error { return e }

// doDelay computes the delay for attempt n, applying the maxDelay cap.
func doDelay(config *DoConfig, n uint, err error) time.Duration {
	d := config.delayType(n, err, config)
	if config.maxDelay > 0 && d > config.maxDelay {
		d = config.maxDelay
	}
	return d
}
