package resil

import (
	"sync"
	"time"
)

// TokenBucket is an integer-token bucket rate limiter that supports
// multi-token requests and dynamic limit changes. It is safe for concurrent
// use.
//
// TokenBucket is the non-blocking counterpart to RateLimiter:
//
//   - RateLimiter uses float64 tokens and supports blocking Wait with timeout.
//   - TokenBucket uses integer tokens and supports variable-cost AllowN(n).
//
// Use TokenBucket when callers can tolerate rejection (e.g. dropping debug
// logs, shedding load) and need to consume a variable number of tokens per
// call (e.g. bytes-per-second, weighted requests). Use RateLimiter when
// callers must eventually proceed (blocking with timeout) and tokens are
// always single-unit.
//
// The zero value is a valid, empty bucket with zero capacity and zero refill.
// Use NewTokenBucket to construct a usable instance.
type TokenBucket struct {
	mu         sync.Mutex
	capacity   int64
	tokens     int64
	refillRate int64 // tokens added per second
	lastRefill time.Time
}

// NewTokenBucket returns a bucket with the given capacity (max accumulated
// tokens) and refillRate (tokens added per second). The bucket starts full,
// so the initial burst equals capacity.
func NewTokenBucket(capacity, refillRate int64) *TokenBucket {
	return &TokenBucket{
		capacity:   capacity,
		tokens:     capacity,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

// AllowN attempts to consume n tokens without blocking. Returns true if
// successful. If n is non-positive, returns true without consuming anything.
// If n exceeds capacity, returns false (the request can never be satisfied).
//
// On failure, the bucket is still refilled to reflect elapsed time, so a
// later retry sees an up-to-date token count.
func (tb *TokenBucket) AllowN(n int64) bool {
	if n <= 0 {
		return true
	}
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refillLocked()
	if n > tb.capacity {
		return false
	}
	if tb.tokens >= n {
		tb.tokens -= n
		return true
	}
	return false
}

// Allow is shorthand for AllowN(1).
func (tb *TokenBucket) Allow() bool { return tb.AllowN(1) }

// SetLimit atomically updates the capacity and refillRate. Tokens accumulated
// since the last refill are first credited (capped to the new capacity), so
// shrinking the limit does not discard already-earned bursts beyond the new
// capacity.
func (tb *TokenBucket) SetLimit(capacity, refillRate int64) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refillLocked()
	tb.capacity = capacity
	tb.refillRate = refillRate
	if tb.tokens > capacity {
		tb.tokens = capacity
	}
}

// Tokens returns the current number of available tokens (after refilling).
// Mainly useful for inspection and tests.
func (tb *TokenBucket) Tokens() int64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refillLocked()
	return tb.tokens
}

// Capacity returns the configured maximum token count.
func (tb *TokenBucket) Capacity() int64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return tb.capacity
}

// refillLocked adds tokens accrued since the last refill, capped at capacity.
// Must be called with tb.mu held.
func (tb *TokenBucket) refillLocked() {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill)
	if elapsed <= 0 {
		return
	}
	added := elapsed.Nanoseconds() * tb.refillRate / int64(time.Second)
	if added <= 0 {
		return
	}
	tb.tokens += added
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}
	tb.lastRefill = now
}
