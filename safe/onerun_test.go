package safe

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOneRun_Basic(t *testing.T) {
	var count int32
	o := NewOneRun(func(ctx context.Context) error {
		atomic.AddInt32(&count, 1)
		return nil
	})
	if err := o.Run(); err != nil {
		t.Errorf("Run returned error: %v", err)
	}
	if atomic.LoadInt32(&count) != 1 {
		t.Errorf("handler should be called once, got %d", count)
	}
}

func TestOneRun_NoHandler(t *testing.T) {
	o := NewOneRun(nil)
	if err := o.Run(); err != ErrNoHandler {
		t.Errorf("expected ErrNoHandler, got %v", err)
	}
}

func TestOneRun_SetHandler(t *testing.T) {
	o := NewOneRun(nil)
	if err := o.Run(); err != ErrNoHandler {
		t.Fatalf("expected ErrNoHandler, got %v", err)
	}
	o.SetHandler(func(ctx context.Context) error {
		return nil
	})
	if err := o.Run(); err != nil {
		t.Errorf("Run with handler should return nil, got %v", err)
	}
}

func TestOneRun_Serializes(t *testing.T) {
	// Multiple concurrent Run calls should all execute eventually (each
	// cancels the prior, but the latest runs to completion).
	var count int32
	o := NewOneRun(func(ctx context.Context) error {
		atomic.AddInt32(&count, 1)
		time.Sleep(5 * time.Millisecond)
		return nil
	})
	var wg sync.WaitGroup
	workers := 10
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			o.Run()
		}()
	}
	wg.Wait()
	// count may be less than workers (some Runs are canceled before executing
	// fn), but it should be at least 1.
	if atomic.LoadInt32(&count) < 1 {
		t.Errorf("expected at least 1 run, got %d", count)
	}
}

func TestOneRun_AllComplete(t *testing.T) {
	// When each Run is fast (no cancellation), all callers should complete.
	var count int32
	o := NewOneRun(func(ctx context.Context) error {
		atomic.AddInt32(&count, 1)
		return nil
	})
	var wg sync.WaitGroup
	workers := 10
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			o.Run()
		}()
	}
	wg.Wait()
	if atomic.LoadInt32(&count) != int32(workers) {
		t.Errorf("expected %d runs, got %d", workers, count)
	}
}

func TestOneRun_Running(t *testing.T) {
	o := NewOneRun(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if o.Running() {
		t.Error("should not be running initially")
	}
	go o.Run()
	time.Sleep(20 * time.Millisecond)
	if !o.Running() {
		t.Error("should be running during Run")
	}
	o.Close()
	time.Sleep(20 * time.Millisecond)
	if o.Running() {
		t.Error("should not be running after Close")
	}
}

func TestOneRun_Close_CancelsInFlight(t *testing.T) {
	exited := make(chan struct{})
	o := NewOneRun(func(ctx context.Context) error {
		<-ctx.Done()
		close(exited)
		return ctx.Err()
	})
	go o.Run()
	time.Sleep(20 * time.Millisecond)
	o.Close()
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("handler did not exit after Close")
	}
}

func TestOneRun_Race(t *testing.T) {
	var count int32
	runner := NewOneRun(func(ctx context.Context) error {
		atomic.AddInt32(&count, 1)
		time.Sleep(2 * time.Millisecond)
		return nil
	})
	var wg sync.WaitGroup
	workers := 20
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			runner.Run()
		}()
	}
	wg.Wait()
	runner.Close()
	if atomic.LoadInt32(&count) < 1 {
		t.Errorf("expected at least 1 run, got %d", count)
	}
}
