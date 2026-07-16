package safe

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunner_Run_Executes(t *testing.T) {
	var count int32
	r := NewRunner(func(ctx context.Context) error {
		atomic.AddInt32(&count, 1)
		return nil
	})
	if err := r.Run(); err != nil {
		t.Errorf("Run returned error: %v", err)
	}
	if atomic.LoadInt32(&count) != 1 {
		t.Errorf("handler should be called once, got %d", count)
	}
}

func TestRunner_DuplicateRun_NoOp(t *testing.T) {
	var count int32
	r := NewRunner(func(ctx context.Context) error {
		atomic.AddInt32(&count, 1)
		<-time.After(50 * time.Millisecond)
		return nil
	})
	r.Start()
	// Give the first Run time to set running flag.
	time.Sleep(5 * time.Millisecond)
	if err := r.Run(); err != nil {
		t.Errorf("duplicate Run should return nil, got %v", err)
	}
	r.Stop(true)
	if atomic.LoadInt32(&count) != 1 {
		t.Errorf("handler should be called once, got %d", count)
	}
}

func TestRunner_Running(t *testing.T) {
	r := NewRunner(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if r.Running() {
		t.Error("should not be running initially")
	}
	r.Start()
	time.Sleep(5 * time.Millisecond)
	if !r.Running() {
		t.Error("should be running after Start")
	}
	r.Stop(true)
	if r.Running() {
		t.Error("should not be running after Stop(true)")
	}
}

func TestRunner_Stop_CancelsContext(t *testing.T) {
	exited := make(chan struct{})
	r := NewRunner(func(ctx context.Context) error {
		<-ctx.Done()
		close(exited)
		return ctx.Err()
	})
	r.Start()
	time.Sleep(5 * time.Millisecond)
	r.Stop(false)
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("handler did not exit after Stop")
	}
}

func TestRunner_StopWait(t *testing.T) {
	r := NewRunner(func(ctx context.Context) error {
		<-ctx.Done()
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	r.Start()
	time.Sleep(5 * time.Millisecond)
	// Stop(true) blocks until handler exits.
	r.Stop(true)
	if r.Running() {
		t.Error("should not be running after Stop(true)")
	}
}

func TestRunner_Restart(t *testing.T) {
	var count int32
	r := NewRunner(func(ctx context.Context) error {
		atomic.AddInt32(&count, 1)
		<-ctx.Done()
		return nil
	})
	r.Start()
	time.Sleep(5 * time.Millisecond)
	r.Restart()
	time.Sleep(5 * time.Millisecond)
	r.Stop(true)
	if atomic.LoadInt32(&count) < 2 {
		t.Errorf("Restart should start a second run, got %d", count)
	}
}

func TestRunner_SetFunc(t *testing.T) {
	r := NewRunner(func(ctx context.Context) error {
		return errors.New("original")
	})
	r.SetFunc(func(ctx context.Context) error {
		return nil
	})
	if err := r.Run(); err != nil {
		t.Errorf("Run with replaced func should return nil, got %v", err)
	}
}

func TestRunner_PanicRecovery(t *testing.T) {
	r := NewRunner(func(ctx context.Context) error {
		panic("boom")
	})
	err := r.Run()
	if err == nil {
		t.Fatal("expected error from panic")
	}
	if r.Running() {
		t.Error("should not be running after panic")
	}
}

func TestRunner_Done(t *testing.T) {
	r := NewRunner(func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})
	// Before any Run, Done is open (a fresh un-closed channel from the
	// constructor). It only closes once a Run finishes.
	r.Start()
	time.Sleep(5 * time.Millisecond)
	// During run, Done should be open.
	select {
	case <-r.Done():
		t.Error("Done should be open during Run")
	default:
	}
	r.Stop(true)
	// After Stop(true), Done should be closed (the run has finished).
	select {
	case <-r.Done():
	default:
		t.Error("Done should be closed after Stop(true)")
	}
}

func TestRunner_Race(t *testing.T) {
	r := NewRunner(func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(10 * time.Millisecond):
			return nil
		}
	})
	var wg sync.WaitGroup
	workers := 50
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			r.Start()
			time.Sleep(time.Millisecond)
			r.Stop(true)
			r.Restart()
		}()
	}
	wg.Wait()
	r.Stop(true)
}
