package resil

import (
	"context"
	"testing"
	"time"

	"ojv/cog/testx"
)

func TestBackoff_Defaults(t *testing.T) {
	as := testx.New(t)
	bo := NewBackOff()

	as.Equal(defaultMinDelay, bo.MinDelay)
	as.Equal(defaultMaxDelay, bo.MaxDelay)
	as.Equal(defaultFactor, bo.Factor)
	as.Equal(defaultJitter, bo.Jitter)
}

func TestBackoff_Options(t *testing.T) {
	as := testx.New(t)
	bo := NewBackOff(
		WithMinDelay(50*time.Millisecond),
		WithMaxDelay(1*time.Second),
		WithFactor(1.5),
		WithJitterFlag(true),
	)

	as.Equal(50*time.Millisecond, bo.MinDelay)
	as.Equal(1*time.Second, bo.MaxDelay)
	as.Equal(1.5, bo.Factor)
	as.True(bo.Jitter)
}

func TestBackoff_Duration(t *testing.T) {
	as := testx.New(t)
	bo := NewBackOff(
		WithMinDelay(10*time.Millisecond),
		WithFactor(2.0),
		WithMaxDelay(100*time.Millisecond),
	)

	// Attempt 0: 10 * 2^0 = 10
	d1 := bo.Duration()
	as.Equal(10*time.Millisecond, d1)

	// Attempt 1: 10 * 2^1 = 20
	d2 := bo.Duration()
	as.Equal(20*time.Millisecond, d2)

	// Attempt 2: 10 * 2^2 = 40
	d3 := bo.Duration()
	as.Equal(40*time.Millisecond, d3)

	// Attempt 3: 10 * 2^3 = 80
	d4 := bo.Duration()
	as.Equal(80*time.Millisecond, d4)

	// Attempt 4: 10 * 2^4 = 160 -> capped at 100
	d5 := bo.Duration()
	as.Equal(100*time.Millisecond, d5)
}

func TestBackoff_Reset(t *testing.T) {
	as := testx.New(t)
	bo := NewBackOff()
	bo.Duration()
	bo.Duration()
	as.True(bo.Attempts() > 0)

	bo.Reset()
	as.Equal(uint64(0), bo.Attempts())
}

func TestBackoff_SleepCtx(t *testing.T) {
	as := testx.New(t)
	bo := NewBackOff(WithMinDelay(100 * time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())

	start := time.Now()
	// Cancel immediately
	cancel()
	bo.SleepCtx(ctx)
	elapsed := time.Since(start)

	// Should return almost immediately, definitely less than 100ms
	as.True(elapsed < 50*time.Millisecond, "Should cancel immediately")
}
