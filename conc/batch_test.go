package conc

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestBatchRunner_Basic(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	var count int32

	handler := NewBatchHandler("test", func(ctx context.Context, v int) error {
		atomic.AddInt32(&count, 1)
		return nil
	})

	config := &BatchConfig{Limit: 2, Retry: 0, RetryDelay: 0}
	runner := NewBatchRunner(items, handler, config)

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if count != 5 {
		t.Errorf("processed %d items, want 5", count)
	}

	stats := runner.GetStats()
	if stats.Completed != 5 || stats.Failed != 0 {
		t.Errorf("stats = %+v, want completed=5 failed=0", stats)
	}
}

func TestBatchRunner_Retry(t *testing.T) {
	var attempts int32
	handler := NewBatchHandler("retry-test", func(ctx context.Context, v int) error {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			return errors.New("transient")
		}
		return nil
	})

	config := &BatchConfig{Limit: 1, Retry: 3, RetryDelay: 1 * time.Millisecond}
	runner := NewBatchRunner([]int{1}, handler, config)

	if err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	results := runner.GetResults()
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Error != nil {
		t.Errorf("expected success, got error: %v", results[0].Error)
	}
	if results[0].Retries != 2 {
		t.Errorf("retries = %d, want 2", results[0].Retries)
	}
}

func TestBatchRunner_RetryExhausted(t *testing.T) {
	handler := NewBatchHandler("always-fail", func(ctx context.Context, v int) error {
		return errors.New("permanent")
	})

	config := &BatchConfig{Limit: 1, Retry: 2, RetryDelay: 1 * time.Millisecond}
	runner := NewBatchRunner([]int{1}, handler, config)

	_ = runner.Run(context.Background())

	results := runner.GetFailedResults()
	if len(results) != 1 {
		t.Fatalf("expected 1 failed result, got %d", len(results))
	}
	if results[0].Retries != 2 {
		t.Errorf("retries = %d, want 2", results[0].Retries)
	}
}

func TestBatchRunner_PanicRecovery(t *testing.T) {
	handler := NewBatchHandler("panic-test", func(ctx context.Context, v int) error {
		panic("boom")
	})

	config := &BatchConfig{Limit: 1, Retry: 0}
	runner := NewBatchRunner([]int{1}, handler, config)

	_ = runner.Run(context.Background())

	results := runner.GetFailedResults()
	if len(results) != 1 {
		t.Fatalf("expected 1 failed result, got %d", len(results))
	}
	if results[0].Panicked == nil {
		t.Error("expected panicked to be set")
	}
	if results[0].Error == nil {
		t.Error("expected error to be set")
	}
}

func TestBatchRunner_Hooks(t *testing.T) {
	var successCount, errorCount int32

	handler := NewBatchHandler("hook-test", func(ctx context.Context, v int) error {
		if v%2 == 0 {
			return errors.New("even fails")
		}
		return nil
	})

	config := &BatchConfig{Limit: 1, Retry: 0}
	runner := NewBatchRunner([]int{1, 2, 3}, handler, config)
	runner.OnSuccess = func(item int) { atomic.AddInt32(&successCount, 1) }
	runner.OnError = func(item int, err error) { atomic.AddInt32(&errorCount, 1) }

	_ = runner.Run(context.Background())

	if successCount != 2 {
		t.Errorf("success hook called %d times, want 2", successCount)
	}
	if errorCount != 1 {
		t.Errorf("error hook called %d times, want 1", errorCount)
	}
}

func TestBatchRunner_ContextCancel(t *testing.T) {
	handler := NewBatchHandler("slow", func(ctx context.Context, v int) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			return nil
		}
	})

	config := &BatchConfig{Limit: 1, Retry: 0, RateLimit: 0}
	runner := NewBatchRunner([]int{1, 2, 3, 4, 5}, handler, config)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := runner.Run(ctx)
	if err == nil {
		t.Error("expected context deadline error")
	}
}

func TestBatchRunner_RateLimit(t *testing.T) {
	var count int32
	handler := NewBatchHandler("rate-test", func(ctx context.Context, v int) error {
		atomic.AddInt32(&count, 1)
		return nil
	})

	// 10 QPS, 5 items → should take at least ~400ms (4 intervals)
	config := &BatchConfig{Limit: 1, Retry: 0, RateLimit: 10}
	runner := NewBatchRunner([]int{1, 2, 3, 4, 5}, handler, config)

	start := time.Now()
	_ = runner.Run(context.Background())
	elapsed := time.Since(start)

	if elapsed < 300*time.Millisecond {
		t.Errorf("rate-limited execution too fast: %v", elapsed)
	}
}

func TestBatchRunner_Empty(t *testing.T) {
	handler := NewBatchHandler("empty", func(ctx context.Context, v int) error { return nil })
	runner := NewBatchRunner([]int{}, handler, nil)

	if err := runner.Run(context.Background()); err != nil {
		t.Errorf("empty run should not error: %v", err)
	}
	if stats := runner.GetStats(); stats.Total != 0 {
		t.Errorf("empty stats total = %d, want 0", stats.Total)
	}
}

func TestBatchRunner_AutoLimit(t *testing.T) {
	items := make([]int, 100)
	handler := NewBatchHandler("auto", func(ctx context.Context, v int) error { return nil })

	// Limit=0 → auto limit
	config := &BatchConfig{Limit: 0, Retry: 0}
	runner := NewBatchRunner(items, handler, config)

	if runner.config.Limit <= 0 {
		t.Error("auto limit should set Limit > 0")
	}
	if runner.config.Limit > 100 {
		t.Error("auto limit should not exceed item count")
	}
}

func TestBatchStats_String(t *testing.T) {
	s := BatchStats{Total: 10, Completed: 10, Success: 8, Failed: 2, Elapsed: 5 * time.Second}
	str := s.String()
	if str == "" {
		t.Error("Stats.String() should not be empty")
	}
}

// TestBatchRunner_ShowProgressCancel verifies that RunStream does not deadlock
// when ShowProgress is enabled and the context is cancelled mid-run.
// Previously, defer LIFO order caused wg.Wait() to execute before cancelProgress(),
// leaving the progress goroutine blocked forever.
func TestBatchRunner_ShowProgressCancel(t *testing.T) {
	items := make([]int, 100)
	handler := NewBatchHandler("slow-progress", func(ctx context.Context, v int) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
			return nil
		}
	})

	config := &BatchConfig{
		Limit:            1,
		Retry:            0,
		ShowProgress:     true,
		ProgressInterval: 10 * time.Millisecond,
	}
	runner := NewBatchRunner(items, handler, config)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx)
	}()

	select {
	case err := <-done:
		// Expected: context deadline exceeded, no deadlock
		if err == nil {
			t.Error("expected context deadline error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run deadlocked with ShowProgress + ctx cancel")
	}
}
