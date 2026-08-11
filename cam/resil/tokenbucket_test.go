package resil

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ojv/cog/testx"
)

func TestTokenBucket_Burst(t *testing.T) {
	as := testx.New(t)
	tb := NewTokenBucket(5, 1) // capacity 5, 1 token/sec
	for i := 0; i < 5; i++ {
		as.True(tb.Allow(), "burst call should succeed")
	}
	as.False(tb.Allow(), "6th call should fail (capacity exhausted)")
}

func TestTokenBucket_AllowN(t *testing.T) {
	as := testx.New(t)
	tb := NewTokenBucket(10, 1)
	as.True(tb.AllowN(7), "7 <= 10 capacity should succeed")
	as.True(tb.AllowN(3), "3 <= remaining 3 should succeed")
	as.False(tb.AllowN(1), "no tokens left, should fail")
}

func TestTokenBucket_AllowN_NonPositive(t *testing.T) {
	as := testx.New(t)
	tb := NewTokenBucket(0, 0)
	as.True(tb.AllowN(0), "AllowN(0) should always succeed")
	as.True(tb.AllowN(-5), "AllowN(negative) should always succeed")
}

func TestTokenBucket_AllowN_ExceedsCapacity(t *testing.T) {
	as := testx.New(t)
	tb := NewTokenBucket(5, 100)
	as.False(tb.AllowN(6), "n > capacity should always fail")
}

func TestTokenBucket_Refill(t *testing.T) {
	as := testx.New(t)
	tb := NewTokenBucket(1, 10) // capacity 1, 10 tokens/sec
	as.True(tb.Allow(), "first call consumes the only token")
	as.False(tb.Allow(), "no tokens left")

	// 10 tokens/sec -> 100ms per token
	time.Sleep(120 * time.Millisecond)
	as.True(tb.Allow(), "after 120ms, 1 token should be refilled")
}

func TestTokenBucket_RefillCapped(t *testing.T) {
	as := testx.New(t)
	tb := NewTokenBucket(3, 1000) // high refill rate, small capacity
	as.True(tb.AllowN(3), "consume all 3")
	as.False(tb.Allow(), "empty")

	time.Sleep(50 * time.Millisecond) // would refill ~50 tokens
	as.True(tb.Allow(), "should have at least 1 token")
	// Verify capped at capacity (3): consume at most 3 immediately
	as.True(tb.Allow())
	as.True(tb.Allow())
	as.False(tb.Allow(), "should be capped at capacity=3")
}

func TestTokenBucket_SetLimit_Shrink(t *testing.T) {
	as := testx.New(t)
	tb := NewTokenBucket(10, 1)
	// Drain most tokens
	as.True(tb.AllowN(8))
	// Shrink capacity below current tokens (2 < 10)
	tb.SetLimit(2, 1)
	// Now tokens should be capped at 2
	as.True(tb.Allow(), "1 of 2")
	as.True(tb.Allow(), "2 of 2")
	as.False(tb.Allow(), "exhausted")
}

func TestTokenBucket_SetLimit_Grow(t *testing.T) {
	as := testx.New(t)
	tb := NewTokenBucket(1, 1)
	as.True(tb.Allow())
	as.False(tb.Allow())
	// Grow capacity; existing tokens stay (0), but new capacity allows refill
	tb.SetLimit(10, 1000)
	time.Sleep(20 * time.Millisecond)
	// Should have refilled multiple tokens
	as.True(tb.Allow(), "should have refilled after growing capacity")
}

func TestTokenBucket_SetLimit_ZeroRefill(t *testing.T) {
	as := testx.New(t)
	tb := NewTokenBucket(5, 1000)
	as.True(tb.AllowN(5)) // drain all
	tb.SetLimit(5, 0)      // disable refill
	time.Sleep(20 * time.Millisecond)
	as.False(tb.Allow(), "with refillRate=0, no new tokens should appear")
}

func TestTokenBucket_Tokens(t *testing.T) {
	as := testx.New(t)
	tb := NewTokenBucket(10, 0) // no refill
	as.Equal(int64(10), tb.Tokens())
	tb.AllowN(3)
	as.Equal(int64(7), tb.Tokens())
}

func TestTokenBucket_Capacity(t *testing.T) {
	as := testx.New(t)
	tb := NewTokenBucket(10, 1)
	as.Equal(int64(10), tb.Capacity())
	tb.SetLimit(20, 1)
	as.Equal(int64(20), tb.Capacity())
}

func TestTokenBucket_Concurrent(t *testing.T) {
	as := testx.New(t)
	// Capacity 1000, refill 100/sec — under contention we should never
	// hand out more tokens than were ever available.
	tb := NewTokenBucket(1000, 100)
	const goroutines = 50
	const callsPerG = 100
	var (
		ok       int64
		wg       sync.WaitGroup
		started  = make(chan struct{})
	)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			<-started
			for j := 0; j < callsPerG; j++ {
				if tb.Allow() {
					atomic.AddInt64(&ok, 1)
				}
			}
		}()
	}
	close(started)
	wg.Wait()

	// Initial burst = 1000. Over the test duration (~ms), refill adds at most
	// a few extra tokens. Cap the upper bound generously to avoid flakiness
	// while still catching gross over-issuance.
	if got := atomic.LoadInt64(&ok); got > 1100 {
		t.Errorf("issued %d tokens, initial capacity 1000 — over-issuance indicates a race", got)
	}
	if got := atomic.LoadInt64(&ok); got < 1000 {
		t.Errorf("issued only %d tokens, expected at least 1000 (initial burst) — under-issuance is also a bug", got)
	}
	as.True(true) // satisfy testx.Assert
}

func TestTokenBucket_ZeroValue(t *testing.T) {
	as := testx.New(t)
	var tb TokenBucket // zero value
	as.False(tb.Allow(), "zero-value bucket has 0 capacity, should reject")
	as.True(tb.AllowN(0), "AllowN(0) should still succeed on zero-value bucket")
}

func BenchmarkTokenBucket_Allow(b *testing.B) {
	tb := NewTokenBucket(1<<30, 1<<30) // never exhaust
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tb.Allow()
	}
}

func BenchmarkTokenBucket_AllowN(b *testing.B) {
	tb := NewTokenBucket(1<<30, 1<<30)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tb.AllowN(1)
	}
}

func BenchmarkTokenBucket_Concurrent(b *testing.B) {
	tb := NewTokenBucket(1<<30, 1<<30)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			tb.Allow()
		}
	})
}
