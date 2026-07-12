package resil

import (
	"testing"
	"time"

	"ojv/cog/util"
)

func TestRateLimiter_Allow(t *testing.T) {
	as := util.NewAssert(t)
	config := NewRateLimitConfig().
		WithRateLimit(10).
		WithRateBurst(2)

	rl := NewRateLimiter(config)

	// Should allow burst amount
	as.True(rl.Allow())
	as.True(rl.Allow())

	// Should fail now (assuming execution is fast enough)
	as.False(rl.Allow())
}

func TestRateLimiter_Refill(t *testing.T) {
	as := util.NewAssert(t)
	// 10 requests per second -> 1 token every 100ms
	config := NewRateLimitConfig().
		WithRateLimit(10).
		WithRateBurst(1)

	rl := NewRateLimiter(config)

	as.True(rl.Allow())
	as.False(rl.Allow())

	time.Sleep(120 * time.Millisecond)
	as.True(rl.Allow())
}

func TestRateLimiter_Wait(t *testing.T) {
	as := util.NewAssert(t)
	config := NewRateLimitConfig().
		WithRateLimit(10).
		WithRateBurst(1).
		WithWaitTimeout(1 * time.Second)

	rl := NewRateLimiter(config)

	// Consume burst
	as.True(rl.Allow())

	start := time.Now()
	// Wait for next token (approx 100ms)
	err := rl.Wait()
	as.TtNoError(err)
	elapsed := time.Since(start)

	as.True(elapsed >= 90*time.Millisecond, "Should wait at least ~100ms")
}

func TestRateLimiter_WaitTimeout(t *testing.T) {
	as := util.NewAssert(t)
	config := NewRateLimitConfig().
		WithRateLimit(1). // 1 per second
		WithRateBurst(0).
		WithWaitTimeout(10 * time.Millisecond) // Timeout fast

	rl := NewRateLimiter(config)

	// Token generation takes 1s, but we only wait 10ms
	err := rl.Wait()
	as.Error(err)
}

func TestGlobalRateLimiter(t *testing.T) {
	as := util.NewAssert(t)
	rl1 := GetGlobalRateLimiter(nil)
	rl2 := GetGlobalRateLimiter(nil)

	as.Equal(rl1, rl2)
}

func TestRateLimiter_WaitZeroLimit(t *testing.T) {
	as := util.NewAssert(t)
	config := NewRateLimitConfig().
		WithRateLimit(0). // invalid: zero rate
		WithRateBurst(1).
		WithWaitTimeout(100 * time.Millisecond)

	rl := NewRateLimiter(config)

	// Consume burst token
	as.True(rl.Allow())

	// Wait should return error immediately (no division by zero)
	err := rl.Wait()
	as.Error(err)
}
