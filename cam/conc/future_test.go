package conc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"ojv/cog/testx"
)

func TestFutureCompleteAndGet(t *testing.T) {
	f := NewFuture[int]()
	go func() {
		time.Sleep(10 * time.Millisecond)
		f.Complete(42, nil)
	}()

	v, err := f.Get(time.Second)
	testx.Nil(t, err)
	testx.Equal(t, 42, v)
	testx.True(t, f.IsDone())
}

func TestFutureCompleteWithError(t *testing.T) {
	f := NewFuture[string]()
	wantErr := errors.New("boom")
	go func() {
		time.Sleep(10 * time.Millisecond)
		f.Complete("", wantErr)
	}()

	v, err := f.Get(time.Second)
	testx.NotNil(t, err)
	testx.Equal(t, wantErr, err)
	testx.Equal(t, "", v)
}

func TestFutureGetTimeout(t *testing.T) {
	f := NewFuture[int]()

	start := time.Now()
	v, err := f.Get(50 * time.Millisecond)
	elapsed := time.Since(start)

	testx.NotNil(t, err)
	testx.True(t, errors.Is(err, ErrFutureNotDone), fmt.Sprintf("want ErrFutureNotDone, got %v", err))
	testx.Equal(t, 0, v)
	testx.True(t, elapsed >= 40*time.Millisecond, fmt.Sprintf("elapsed %v too short", elapsed))
	testx.False(t, f.IsDone())
}

func TestFutureGetWithContextCancellation(t *testing.T) {
	f := NewFuture[int]()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	v, err := f.GetWithContext(ctx)
	testx.NotNil(t, err)
	testx.True(t, errors.Is(err, context.DeadlineExceeded), fmt.Sprintf("want DeadlineExceeded, got %v", err))
	testx.Equal(t, 0, v)
}

func TestFutureGetWithContextSuccess(t *testing.T) {
	f := NewFuture[int]()
	go func() {
		time.Sleep(10 * time.Millisecond)
		f.Complete(7, nil)
	}()

	v, err := f.GetWithContext(context.Background())
	testx.Nil(t, err)
	testx.Equal(t, 7, v)
}

func TestFutureCompleteIsIdempotent(t *testing.T) {
	f := NewFuture[int]()
	ok1 := f.Complete(1, nil)
	ok2 := f.Complete(2, nil)
	ok3 := f.Complete(3, nil)

	testx.True(t, ok1)
	testx.False(t, ok2)
	testx.False(t, ok3)

	v, err := f.Get(time.Second)
	testx.Nil(t, err)
	testx.Equal(t, 1, v, "first Complete wins")
}

func TestFutureOnCompleteBeforeComplete(t *testing.T) {
	f := NewFuture[int]()
	var (
		mu     sync.Mutex
		gotVal int
		gotErr error
		called bool
	)
	f.OnComplete(func(v int, err error) {
		mu.Lock()
		defer mu.Unlock()
		gotVal, gotErr, called = v, err, true
	})

	f.Complete(99, nil)

	// OnComplete registered before Complete runs synchronously after Complete.
	time.Sleep(10 * time.Millisecond)
	mu.Lock()
	testx.True(t, called, "callback should fire")
	testx.Equal(t, 99, gotVal)
	testx.Nil(t, gotErr)
	mu.Unlock()
}

func TestFutureOnCompleteAfterComplete(t *testing.T) {
	f := NewFuture[int]()
	f.Complete(5, nil)

	var (
		mu     sync.Mutex
		gotVal int
		called bool
	)
	f.OnComplete(func(v int, err error) {
		mu.Lock()
		defer mu.Unlock()
		gotVal, called = v, true
	})

	// Async callback for already-completed Future.
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	testx.True(t, called, "async callback should fire")
	testx.Equal(t, 5, gotVal)
	mu.Unlock()
}

func TestFutureOnSuccessOnError(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		f := NewFuture[int]()
		var mu sync.Mutex
		var got int
		var errCalled bool
		f.OnSuccess(func(v int) { mu.Lock(); defer mu.Unlock(); got = v }).
			OnError(func(error) { mu.Lock(); defer mu.Unlock(); errCalled = true })
		f.Complete(7, nil)

		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		testx.Equal(t, 7, got)
		testx.False(t, errCalled)
		mu.Unlock()
	})

	t.Run("error", func(t *testing.T) {
		f := NewFuture[int]()
		var mu sync.Mutex
		var successCalled bool
		var gotErr error
		f.OnSuccess(func(int) { mu.Lock(); defer mu.Unlock(); successCalled = true }).
			OnError(func(err error) { mu.Lock(); defer mu.Unlock(); gotErr = err })
		wantErr := errors.New("fail")
		f.Complete(0, wantErr)

		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		testx.False(t, successCalled)
		testx.Equal(t, wantErr, gotErr)
		mu.Unlock()
	})
}

func TestFutureDoneChannel(t *testing.T) {
	f := NewFuture[int]()
	select {
	case <-f.Done():
		t.Fatal("Done should not be closed before Complete")
	default:
	}

	go func() {
		time.Sleep(10 * time.Millisecond)
		f.Complete(1, nil)
	}()

	select {
	case <-f.Done():
	case <-time.After(time.Second):
		t.Fatal("Done should be closed after Complete")
	}

	v, err, ok := f.GetResult()
	testx.True(t, ok)
	testx.Nil(t, err)
	testx.Equal(t, 1, v)
}

func TestFutureGetResultNotDone(t *testing.T) {
	f := NewFuture[int]()
	_, _, ok := f.GetResult()
	testx.False(t, ok)
}

func TestFutureConcurrentGet(t *testing.T) {
	f := NewFuture[int]()
	var wg sync.WaitGroup
	const N = 100
	results := make([]int, N)
	errs := make([]error, N)

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			v, err := f.Get(time.Second)
			results[idx] = v
			errs[idx] = err
		}(i)
	}

	time.Sleep(20 * time.Millisecond)
	f.Complete(777, nil)
	wg.Wait()

	for i := 0; i < N; i++ {
		testx.Nil(t, errs[i], fmt.Sprintf("goroutine %d", i))
		testx.Equal(t, 777, results[i], fmt.Sprintf("goroutine %d", i))
	}
}

func TestFutureGetZeroTimeout(t *testing.T) {
	f := NewFuture[int]()
	go func() {
		time.Sleep(20 * time.Millisecond)
		f.Complete(1, nil)
	}()

	// Zero timeout means wait indefinitely.
	start := time.Now()
	v, err := f.Get(0)
	elapsed := time.Since(start)
	testx.Nil(t, err)
	testx.Equal(t, 1, v)
	testx.True(t, elapsed >= 15*time.Millisecond, "should have waited for Complete")
}
