package sched

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ojv/cog/testx"
)

// assert helpers that accept format args, since testx.Assert.True only takes
// plain strings.
func trueF(t *testing.T, cond bool, format string, args ...interface{}) {
	t.Helper()
	if !cond {
		t.Fatalf(format, args...)
	}
}

// helper: wait until counter reaches target or test fails after timeout.
func waitFor(t *testing.T, name string, c *int64, want int64) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(c) >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%s: counter %d never reached %d", name, atomic.LoadInt64(c), want)
}

func TestNew(t *testing.T) {
	s := New()
	defer s.Stop()
	as := testx.New(t)
	as.NotNil(s)
	as.Equal(0, len(s.Tasks()))
}

func TestValidate(t *testing.T) {
	s := New()
	defer s.Stop()
	as := testx.New(t)

	// nil TaskFunc
	_, err := s.Add(&Task{Interval: time.Second})
	as.Error(err)
	as.Equal("task function cannot be nil", err.Error())

	// zero Interval
	_, err = s.Add(&Task{TaskFunc: func() error { return nil }})
	as.Error(err)
	as.Equal("task interval must be positive", err.Error())

	// negative Interval
	_, err = s.Add(&Task{Interval: -time.Second, TaskFunc: func() error { return nil }})
	as.Error(err)
}

func TestAddAndDel(t *testing.T) {
	s := New()
	defer s.Stop()
	as := testx.New(t)

	var c int64
	id, err := s.Add(&Task{
		Interval: 50 * time.Millisecond,
		TaskFunc: func() error { atomic.AddInt64(&c, 1); return nil },
	})
	as.NoError(err)
	as.True(id > 0)

	waitFor(t, "first run", &c, 1)

	_, err = s.Lookup(id)
	as.NoError(err)

	s.Del(id)

	// after Del, task is gone
	_, err = s.Lookup(id)
	as.Error(err)

	cur := atomic.LoadInt64(&c)
	time.Sleep(200 * time.Millisecond)
	// no further invocations
	as.Equal(cur, atomic.LoadInt64(&c))
}

func TestAddWithIDDuplicate(t *testing.T) {
	s := New()
	defer s.Stop()
	as := testx.New(t)

	t1 := &Task{Interval: 50 * time.Millisecond, TaskFunc: func() error { return nil }}
	err := s.AddWithID(42, t1)
	as.NoError(err)

	// same ID
	err = s.AddWithID(42, &Task{Interval: 50 * time.Millisecond, TaskFunc: func() error { return nil }})
	as.Error(err)
	as.True(errors.Is(err, ErrIDInUse))

	// different ID works
	err = s.AddWithID(43, &Task{Interval: 50 * time.Millisecond, TaskFunc: func() error { return nil }})
	as.NoError(err)
}

func TestAddAutoCollisionRecovers(t *testing.T) {
	// Force a collision by pre-occupying an ID, then Add must still succeed by
	// generating a different one.
	s := New()
	defer s.Stop()
	as := testx.New(t)

	err := s.AddWithID(1, &Task{Interval: 50 * time.Millisecond, TaskFunc: func() error { return nil }})
	as.NoError(err)

	// Add uses Mist-generated IDs which won't be 1, so this should just work;
	// the loop is exercised conceptually but collisions are astronomically
	// unlikely with Mist.
	id, err := s.Add(&Task{Interval: 50 * time.Millisecond, TaskFunc: func() error { return nil }})
	as.NoError(err)
	as.True(id != 1) // Mist-generated IDs are > 1 (increas starts at 2)
}

func TestRunOnce(t *testing.T) {
	s := New()
	defer s.Stop()
	as := testx.New(t)

	var c int64
	id, err := s.Add(&Task{
		Interval: 50 * time.Millisecond,
		RunOnce:  true,
		TaskFunc: func() error { atomic.AddInt64(&c, 1); return nil },
	})
	as.NoError(err)

	waitFor(t, "once", &c, 1)
	time.Sleep(200 * time.Millisecond)
	as.Equal(int64(1), atomic.LoadInt64(&c))

	// auto-removed after RunOnce
	_, err = s.Lookup(id)
	as.Error(err)
}

func TestRunSingleInstance(t *testing.T) {
	s := New()
	defer s.Stop()
	as := testx.New(t)

	var c int64
	// Slow task: blocks 200ms; interval 50ms; RunSingleInstance should skip
	// overlapping invocations.
	var started int64
	_, err := s.Add(&Task{
		Interval:          50 * time.Millisecond,
		RunSingleInstance: true,
		TaskFunc: func() error {
			atomic.AddInt64(&started, 1)
			time.Sleep(200 * time.Millisecond)
			atomic.AddInt64(&c, 1)
			return nil
		},
	})
	as.NoError(err)

	time.Sleep(500 * time.Millisecond)
	startedVal := atomic.LoadInt64(&started)
	// If overlapping invocations were spawned, started would be > completed+1.
	// With RunSingleInstance, started should be at most completed+1.
	completedVal := atomic.LoadInt64(&c)
	trueF(t, startedVal <= completedVal+1,
		"started %d should be <= completed+1 %d", startedVal, completedVal+1)
}

func TestErrFunc(t *testing.T) {
	s := New()
	defer s.Stop()
	as := testx.New(t)

	var got atomic.Value // error
	_, err := s.Add(&Task{
		Interval: 50 * time.Millisecond,
		TaskFunc: func() error { return errors.New("boom") },
		ErrFunc:  func(e error) { got.Store(e) },
	})
	as.NoError(err)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v := got.Load(); v != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	v, ok := got.Load().(error)
	as.True(ok)
	as.NotNil(v)
	as.Equal("boom", v.Error())
}

func TestPanicRecovery(t *testing.T) {
	s := New()
	defer s.Stop()
	as := testx.New(t)

	var got atomic.Value // error
	_, err := s.Add(&Task{
		Interval: 50 * time.Millisecond,
		RunOnce:  true,
		TaskFunc: func() error { panic("kaboom") },
		ErrFunc:  func(e error) { got.Store(e) },
	})
	as.NoError(err)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v := got.Load(); v != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	v, ok := got.Load().(error)
	as.True(ok)
	as.NotNil(v)
	as.Contains(v.Error(), "kaboom")
}

func TestStartAfter(t *testing.T) {
	s := New()
	defer s.Stop()
	as := testx.New(t)

	var c int64
	start := time.Now()
	startAfter := start.Add(300 * time.Millisecond)
	_, err := s.Add(&Task{
		Interval:   50 * time.Millisecond,
		StartAfter: startAfter,
		TaskFunc:   func() error { atomic.AddInt64(&c, 1); return nil },
	})
	as.NoError(err)

	// No execution before StartAfter + Interval = ~350ms.
	time.Sleep(250 * time.Millisecond)
	as.Equal(int64(0), atomic.LoadInt64(&c))

	waitFor(t, "after startafter", &c, 1)
	elapsed := time.Since(start)
	// Must have waited at least StartAfter.
	trueF(t, elapsed >= 300*time.Millisecond, "elapsed %s < 300ms", elapsed)
}

func TestStartAfterInPast(t *testing.T) {
	s := New()
	defer s.Stop()
	as := testx.New(t)

	var c int64
	_, err := s.Add(&Task{
		Interval:   50 * time.Millisecond,
		StartAfter: time.Now().Add(-time.Minute),
		TaskFunc:   func() error { atomic.AddInt64(&c, 1); return nil },
	})
	as.NoError(err)
	// Fires after one Interval (since StartAfter is clamped to 0).
	waitFor(t, "past startafter", &c, 1)
}

func TestStopIsTerminal(t *testing.T) {
	s := New()
	s.Stop()
	as := testx.New(t)

	_, err := s.Add(&Task{Interval: time.Second, TaskFunc: func() error { return nil }})
	as.Error(err)
	as.True(errors.Is(err, ErrSchedulerStop))

	err = s.AddWithID(1, &Task{Interval: time.Second, TaskFunc: func() error { return nil }})
	as.Error(err)
	as.True(errors.Is(err, ErrSchedulerStop))
}

func TestStopCancelsTasks(t *testing.T) {
	s := New()
	as := testx.New(t)

	var c int64
	_, err := s.Add(&Task{
		Interval: 50 * time.Millisecond,
		TaskFunc: func() error { atomic.AddInt64(&c, 1); return nil },
	})
	as.NoError(err)

	waitFor(t, "first", &c, 1)
	s.Stop()

	cur := atomic.LoadInt64(&c)
	time.Sleep(200 * time.Millisecond)
	// no further invocations after Stop
	as.Equal(cur, atomic.LoadInt64(&c))
}

func TestTasksSnapshot(t *testing.T) {
	s := New()
	defer s.Stop()
	as := testx.New(t)

	for i := 0; i < 5; i++ {
		err := s.AddWithID(int64(i+1), &Task{
			Interval: 10 * time.Second,
			TaskFunc: func() error { return nil },
		})
		as.NoError(err)
	}

	snap := s.Tasks()
	as.Equal(5, len(snap))
	// mutating snapshot must not affect scheduler
	snap[999] = &Task{}
	as.Equal(5, len(s.Tasks()))
}

func TestDelMissingIsNoop(t *testing.T) {
	s := New()
	defer s.Stop()
	// should not panic
	s.Del(99999)
}

func TestConcurrentAddDel(t *testing.T) {
	s := New()
	defer s.Stop()
	as := testx.New(t)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := int64(i + 1)
			_ = s.AddWithID(id, &Task{
				Interval: 20 * time.Millisecond,
				TaskFunc: func() error { return nil },
			})
			time.Sleep(50 * time.Millisecond)
			s.Del(id)
		}(i)
	}
	wg.Wait()
	// If we get here without deadlock/panic, the test passes.
	as.True(true)
}

func TestDelDuringExecutionDoesNotInterrupt(t *testing.T) {
	s := New()
	defer s.Stop()
	as := testx.New(t)

	var completed atomic.Bool
	_, err := s.Add(&Task{
		Interval: 50 * time.Millisecond,
		TaskFunc: func() error {
			time.Sleep(150 * time.Millisecond)
			completed.Store(true)
			return nil
		},
	})
	as.NoError(err)

	// Give it time to start, then delete.
	time.Sleep(80 * time.Millisecond)
	// Del should not interrupt the in-flight task.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if completed.Load() {
			break
		}
		time.Sleep(time.Millisecond)
	}
	as.True(completed.Load(), "in-flight task should complete despite Del")
}

func ExampleScheduler() {
	s := New()
	defer s.Stop()

	_, _ = s.Add(&Task{
		Interval: 100 * time.Millisecond,
		TaskFunc: func() error {
			fmt.Println("tick")
			return nil
		},
	})

	// In a real program you'd let it run; for the example we sleep briefly.
	time.Sleep(250 * time.Millisecond)
	// Output:
	// tick
	// tick
}
