package conc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ojv/cog/testx"
)

func TestSemaphoreNewPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for limit <= 0")
		}
	}()
	NewSemaphore(0)
}

func TestSemaphoreAcquireRelease(t *testing.T) {
	s := NewSemaphore(2)
	testx.Equal(t, 2, s.GetLimit(), "initial limit")
	testx.Equal(t, 0, s.GetCount(), "initial count")

	testx.Nil(t, s.Acquire(context.Background(), 1))
	testx.Equal(t, 1, s.GetCount())

	testx.Nil(t, s.Acquire(context.Background(), 1))
	testx.Equal(t, 2, s.GetCount())

	// Third acquire should block; release from a goroutine.
	done := make(chan struct{})
	go func() {
		testx.Nil(t, s.Acquire(context.Background(), 1))
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("acquire should block when full")
	case <-time.After(50 * time.Millisecond):
	}

	prev := s.Release(1)
	testx.Equal(t, 2, prev, "release returns previous count")

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("acquire should succeed after release")
	}
	testx.Equal(t, 2, s.GetCount())
	s.Release(2)
}

func TestSemaphoreTryAcquire(t *testing.T) {
	s := NewSemaphore(1)
	testx.True(t, s.TryAcquire(1))
	testx.False(t, s.TryAcquire(1), "should fail when full")
	s.Release(1)
	testx.True(t, s.TryAcquire(1))
}

func TestSemaphoreContextCancel(t *testing.T) {
	s := NewSemaphore(1)
	testx.Nil(t, s.Acquire(context.Background(), 1))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := s.Acquire(ctx, 1)
	testx.NotNil(t, err)
	testx.True(t, errors.Is(err, context.DeadlineExceeded),
		fmt.Sprintf("want DeadlineExceeded, got %v", err))
	s.Release(1)
}

func TestSemaphoreSetLimitGrow(t *testing.T) {
	s := NewSemaphore(1)
	testx.Nil(t, s.Acquire(context.Background(), 1))

	// Blocked acquirer should succeed after SetLimit raises the limit.
	done := make(chan error, 1)
	go func() { done <- s.Acquire(context.Background(), 1) }()

	select {
	case <-done:
		t.Fatal("acquire should block before SetLimit")
	case <-time.After(30 * time.Millisecond):
	}

	s.SetLimit(2)
	select {
	case err := <-done:
		testx.Nil(t, err)
	case <-time.After(time.Second):
		t.Fatal("acquire should succeed after grow")
	}
}

func TestSemaphoreSetLimitShrink(t *testing.T) {
	s := NewSemaphore(3)
	testx.Nil(t, s.Acquire(context.Background(), 3))
	testx.Equal(t, 3, s.GetCount())

	// Shrinking below current count does NOT revoke existing entries.
	s.SetLimit(1)
	testx.Equal(t, 1, s.GetLimit())
	testx.Equal(t, 3, s.GetCount())

	// But further Acquire should block until count drops below the new limit.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := s.Acquire(ctx, 1)
	testx.NotNil(t, err)
	testx.True(t, errors.Is(err, context.DeadlineExceeded))

	// After releasing down to 0, Acquire should succeed.
	s.Release(3)
	testx.Nil(t, s.Acquire(context.Background(), 1))
}

func TestSemaphoreStopRejectsNew(t *testing.T) {
	s := NewSemaphore(1)
	s.Stop()

	err := s.Acquire(context.Background(), 1)
	testx.True(t, errors.Is(err, ErrSemaphoreStopped),
		fmt.Sprintf("want ErrSemaphoreStopped, got %v", err))

	testx.False(t, s.TryAcquire(1))
}

func TestSemaphoreStopWakesBlocked(t *testing.T) {
	s := NewSemaphore(1)
	testx.Nil(t, s.Acquire(context.Background(), 1))

	// Blocked acquirer should wake with ErrSemaphoreStopped after Stop.
	done := make(chan error, 1)
	go func() { done <- s.Acquire(context.Background(), 1) }()

	select {
	case <-done:
		t.Fatal("acquire should block before Stop")
	case <-time.After(30 * time.Millisecond):
	}

	s.Stop()
	select {
	case err := <-done:
		testx.True(t, errors.Is(err, ErrSemaphoreStopped),
			fmt.Sprintf("want ErrSemaphoreStopped, got %v", err))
	case <-time.After(time.Second):
		t.Fatal("blocked acquire should wake after Stop")
	}
}

func TestSemaphoreStopIsTerminal(t *testing.T) {
	s := NewSemaphore(1)
	s.Stop()
	s.Stop() // idempotent
	testx.True(t, errors.Is(s.Acquire(context.Background(), 1), ErrSemaphoreStopped))
}

func TestSemaphoreReleaseWithoutAcquire(t *testing.T) {
	s := NewSemaphore(2)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for release-without-acquire")
		}
	}()
	s.Release(1)
}

func TestSemaphoreNonPositivePanics(t *testing.T) {
	s := NewSemaphore(1)
	for _, fn := range []func(){
		func() { s.Acquire(context.Background(), 0) },
		func() { s.TryAcquire(0) },
		func() { s.Release(0) },
	} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatal("expected panic for n <= 0")
				}
			}()
			fn()
		}()
	}
}

func TestSemaphoreConcurrent(t *testing.T) {
	const N = 50
	s := NewSemaphore(4)
	var wg sync.WaitGroup
	var acquired int64
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Acquire(context.Background(), 1); err != nil {
				return
			}
			atomic.AddInt64(&acquired, 1)
			time.Sleep(time.Millisecond)
			s.Release(1)
		}()
	}
	wg.Wait()
	testx.Equal(t, int64(N), acquired)
	testx.Equal(t, 0, s.GetCount())
}

func TestSemaphoreMultipleEntries(t *testing.T) {
	s := NewSemaphore(10)
	testx.Nil(t, s.Acquire(context.Background(), 3))
	testx.Equal(t, 3, s.GetCount())
	testx.True(t, s.TryAcquire(5))
	testx.Equal(t, 8, s.GetCount())
	testx.False(t, s.TryAcquire(5), "should fail (8+5 > 10)")
	testx.Nil(t, s.Acquire(context.Background(), 2))
	testx.Equal(t, 10, s.GetCount())
	s.Release(7)
	testx.Equal(t, 3, s.GetCount())
	s.Release(3)
	testx.Equal(t, 0, s.GetCount())
}

func TestSemaphoreSetLimitWakesWaiters(t *testing.T) {
	s := NewSemaphore(1)
	testx.Nil(t, s.Acquire(context.Background(), 1))

	// Multiple waiters blocked.
	const waiters = 3
	done := make(chan error, waiters)
	for i := 0; i < waiters; i++ {
		go func() { done <- s.Acquire(context.Background(), 1) }()
	}
	// Allow them to block.
	time.Sleep(30 * time.Millisecond)

	// SetLimit to a larger value should wake them and let them all acquire.
	s.SetLimit(waiters + 1)
	for i := 0; i < waiters; i++ {
		select {
		case err := <-done:
			testx.Nil(t, err)
		case <-time.After(time.Second):
			t.Fatal("waiter timed out after SetLimit")
		}
	}
}
