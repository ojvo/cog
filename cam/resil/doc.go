// Package resil provides resilience patterns:
//   - Retry: generic retry with exponential backoff and context cancellation
//   - RetryHTTP: HTTP-specific retry with status code and method filtering
//   - Backoff: pluggable backoff strategy (exponential with optional jitter)
//   - RateLimiter: token bucket rate limiter with float tokens, blocking Wait
//     with timeout, and a global instance registry. Use when callers must
//     eventually proceed and tokens are single-unit.
//   - TokenBucket: integer token bucket that supports multi-token AllowN
//     (variable-cost requests) and dynamic SetLimit. Non-blocking. Use when
//     callers tolerate rejection (e.g. dropping debug logs, shedding load).
//   - Fallback: stateful failover to a backup operation after consecutive failures.
//     Orthogonal to Retry: Retry retries on the same resource, Fallback switches
//     to a backup resource. Combine by wrapping Retry outside Fallback.
//   - CircuitBreaker: stateful breaker that blocks calls to a failing downstream
//     (Closed→Open after N failures, Open→HalfOpen after a cool-down, HalfOpen
//     trial succeeds→Closed / fails→Open). Orthogonal to Retry and Fallback;
//     compose as CircuitBreaker(Fallback(Retry(fn))).
package resil
