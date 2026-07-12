// Package resil provides resilience patterns:
//   - Retry: generic retry with exponential backoff and context cancellation
//   - RetryHTTP: HTTP-specific retry with status code and method filtering
//   - Backoff: pluggable backoff strategy (exponential with optional jitter)
//   - RateLimiter: token bucket rate limiter with global instance registry
package resil
