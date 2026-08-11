package conc

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSingleflight_Deduplicates verifies that N concurrent Do calls for the
// same key invoke fn exactly once and that all callers receive the same result.
func TestSingleflight_Deduplicates(t *testing.T) {
	var sf Singleflight[string, int]
	var calls int32
	const n = 32

	results := make([]int, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start
			results[idx], errs[idx] = sf.Do("k", func() (int, error) {
				atomic.AddInt32(&calls, 1)
				time.Sleep(10 * time.Millisecond)
				return 42, nil
			})
		}(i)
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("fn invoked %d times, want 1", got)
	}
	for i, r := range results {
		if r != 42 {
			t.Errorf("results[%d] = %d, want 42", i, r)
		}
		if errs[i] != nil {
			t.Errorf("errs[%d] = %v, want nil", i, errs[i])
		}
	}
}

// TestSingleflight_ErrorPropagated verifies that an error returned by fn is
// propagated to all waiters.
func TestSingleflight_ErrorPropagated(t *testing.T) {
	var sf Singleflight[string, string]
	sentinel := errors.New("boom")
	const n = 16

	errs := make([]error, n)
	vals := make([]string, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start
			vals[idx], errs[idx] = sf.Do("err-key", func() (string, error) {
				return "", sentinel
			})
		}(i)
	}
	close(start)
	wg.Wait()

	for i, e := range errs {
		if !errors.Is(e, sentinel) {
			t.Errorf("errs[%d] = %v, want sentinel", i, e)
		}
		if vals[i] != "" {
			t.Errorf("vals[%d] = %q, want empty", i, vals[i])
		}
	}
}

// TestSingleflight_DifferentKeysAreIndependent verifies that different keys
// each invoke fn separately.
func TestSingleflight_DifferentKeysAreIndependent(t *testing.T) {
	var sf Singleflight[int, int]
	var calls int32
	const n = 10

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start
			sf.Do(idx, func() (int, error) {
				atomic.AddInt32(&calls, 1)
				return idx, nil
			})
		}(i)
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != n {
		t.Errorf("fn invoked %d times, want %d", got, n)
	}
}

// TestSingleflight_SequentialReuse verifies that after a Do call completes and
// cleans up, a subsequent Do for the same key invokes fn again.
func TestSingleflight_SequentialReuse(t *testing.T) {
	var sf Singleflight[int, int]

	v1, _ := sf.Do(7, func() (int, error) { return 100, nil })
	v2, _ := sf.Do(7, func() (int, error) { return 200, nil })

	if v1 != 100 || v2 != 200 {
		t.Errorf("sequential Do should not share results: got %d, %d; want 100, 200", v1, v2)
	}
}

// TestSingleflight_IntKey verifies the generic constraint accepts non-string
// comparable keys (int in this case).
func TestSingleflight_IntKey(t *testing.T) {
	var sf Singleflight[int64, string]
	v, err := sf.Do(1<<40, func() (string, error) { return "ok", nil })
	if err != nil || v != "ok" {
		t.Errorf("got (%q, %v), want (ok, nil)", v, err)
	}
}

// TestSingleflight_StructKey verifies the generic constraint accepts struct
// keys.
func TestSingleflight_StructKey(t *testing.T) {
	type k struct{ a, b int }
	var sf Singleflight[k, int]
	v, err := sf.Do(k{1, 2}, func() (int, error) { return 3, nil })
	if err != nil || v != 3 {
		t.Errorf("got (%d, %v), want (3, nil)", v, err)
	}
}

// TestSingleflight_Forget verifies that Forget removes the in-flight entry so
// a subsequent Do starts a new invocation.
func TestSingleflight_Forget(t *testing.T) {
	var sf Singleflight[string, int]
	var calls int32

	// Start a long-running fn under key "k" but don't wait for it.
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		sf.Do("k", func() (int, error) {
			close(started)
			atomic.AddInt32(&calls, 1)
			<-done
			return 1, nil
		})
	}()

	<-started

	// Now Forget "k" and issue a second Do. It should invoke fn a second
	// time rather than joining the in-flight call.
	sf.Forget("k")
	v, _ := sf.Do("k", func() (int, error) {
		atomic.AddInt32(&calls, 1)
		return 2, nil
	})

	if v != 2 {
		t.Errorf("after Forget, Do returned %d, want 2", v)
	}

	// Let the original in-flight call finish.
	close(done)

	// Allow the first goroutine to actually return so the test goroutine
	// observes the final state. (The first call's result is discarded since
	// Forget detached it.)
	time.Sleep(5 * time.Millisecond)

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("fn invoked %d times, want 2", got)
	}
}

// TestSingleflight_ForgetAbsentKey verifies Forget on a missing key is a no-op.
func TestSingleflight_ForgetAbsentKey(t *testing.T) {
	var sf Singleflight[string, int]
	// Should not panic.
	sf.Forget("missing")
	v, err := sf.Do("missing", func() (int, error) { return 7, nil })
	if err != nil || v != 7 {
		t.Errorf("Do after Forget(missing) returned (%d, %v), want (7, nil)", v, err)
	}
}

func TestSingleflight_PanicReleasesWaiters(t *testing.T) {
	var sf Singleflight[string, int]
	entered := make(chan struct{})
	release := make(chan struct{})
	leaderDone := make(chan error, 1)
	waiterDone := make(chan error, 1)
	waiterEntered := make(chan struct{})
	waiterFnStarted := make(chan struct{}, 1)

	go func() {
		_, err := sf.Do("panic", func() (int, error) {
			close(entered)
			<-release
			panic("boom")
		})
		leaderDone <- err
	}()
	<-entered
	go func() {
		close(waiterEntered)
		_, err := sf.Do("panic", func() (int, error) {
			waiterFnStarted <- struct{}{}
			return 1, nil
		})
		waiterDone <- err
	}()
	<-waiterEntered
	// Give the waiter a chance to join before releasing the panicking leader.
	// If it had incorrectly created a second call, waiterFnStarted would fire.
	select {
	case <-waiterFnStarted:
		t.Fatal("waiter did not join the in-flight call")
	case <-time.After(10 * time.Millisecond):
	}

	close(release)
	for _, done := range []<-chan error{leaderDone, waiterDone} {
		select {
		case err := <-done:
			var panicErr *PanicError
			if !errors.As(err, &panicErr) || panicErr.Value != "boom" {
				t.Fatalf("got %v, want PanicError(boom)", err)
			}
		case <-time.After(time.Second):
			t.Fatal("panic left a singleflight caller blocked")
		}
	}

	if v, err := sf.Do("panic", func() (int, error) { return 2, nil }); err != nil || v != 2 {
		t.Fatalf("entry was not cleaned up after panic: got (%d, %v)", v, err)
	}
}

// TestSingleflight_ForgetRaceOnFnCompletion verifies the classic singleflight
// Forget race: when Forget is called while fn is in-flight and a new Do
// creates a fresh entry, the old fn's completion must NOT delete the new
// entry. Previously the unconditional delete(s.m, key) in Do would discard
// the new entry, causing subsequent Do callers to start a third invocation
// rather than joining the in-flight new one.
func TestSingleflight_ForgetRaceOnFnCompletion(t *testing.T) {
	var sf Singleflight[string, int]
	var calls int32

	// Step 1: start a slow fn under "k".
	oldRelease := make(chan struct{})
	oldStarted := make(chan struct{})
	go func() {
		sf.Do("k", func() (int, error) {
			close(oldStarted)
			atomic.AddInt32(&calls, 1)
			<-oldRelease
			return 1, nil
		})
	}()
	<-oldStarted

	// Step 2: Forget and immediately start a new fn under "k".
	sf.Forget("k")
	newRelease := make(chan struct{})
	newStarted := make(chan struct{})
	newDone := make(chan int, 1)
	go func() {
		v, _ := sf.Do("k", func() (int, error) {
			close(newStarted)
			atomic.AddInt32(&calls, 1)
			<-newRelease
			return 2, nil
		})
		newDone <- v
	}()
	<-newStarted

	// Step 3: release the OLD fn. Its completion path must NOT delete the
	// new in-flight entry.
	close(oldRelease)
	// Give the old fn's cleanup path a chance to run.
	time.Sleep(10 * time.Millisecond)

	// Step 4: a third Do under "k" should JOIN the still-in-flight new fn,
	// not start a third invocation. The new fn is still blocked on
	// newRelease, so the third Do must block on it. If the bug existed
	// (old fn's delete removed the new entry), the third Do would invoke
	// fn a third time and return 3 immediately.
	thirdDone := make(chan int, 1)
	go func() {
		v, _ := sf.Do("k", func() (int, error) {
			atomic.AddInt32(&calls, 1)
			return 3, nil
		})
		thirdDone <- v
	}()

	// Give the third Do a chance to run. With the fix, it blocks on the
	// new in-flight entry; without the fix, it returns 3 immediately.
	select {
	case v := <-thirdDone:
		t.Fatalf("third Do returned %d before newRelease; it did not join the in-flight new fn (bug present)", v)
	case <-time.After(20 * time.Millisecond):
		// Good — third Do is blocked on the new in-flight entry.
	}

	// Release the new fn so waiters can complete.
	close(newRelease)

	// Both the new fn caller and the third caller should observe value 2.
	if v := <-newDone; v != 2 {
		t.Errorf("new fn caller got %d, want 2", v)
	}
	if v := <-thirdDone; v != 2 {
		t.Errorf("third caller got %d, want 2 (should join new in-flight fn)", v)
	}

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("fn invoked %d times, want 2 (old + new; third should join)", got)
	}
}
