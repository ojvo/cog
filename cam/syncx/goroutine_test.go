package syncx

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGoroutine_Basic(t *testing.T) {
	g := NewGoroutine()
	var counter int32
	g.Go(
		func(ctx context.Context) { atomic.AddInt32(&counter, 1) },
		func(ctx context.Context) { atomic.AddInt32(&counter, 1) },
		func(ctx context.Context) { atomic.AddInt32(&counter, 1) },
	)
	g.Wait()
	if atomic.LoadInt32(&counter) != 3 {
		t.Errorf("counter = %d, want 3", counter)
	}
	if g.Total() != 3 {
		t.Errorf("Total = %d, want 3", g.Total())
	}
	if g.Active() != 0 {
		t.Errorf("Active = %d, want 0 after Wait", g.Active())
	}
}

func TestGoroutine_ActiveDuringRun(t *testing.T) {
	g := NewGoroutine()
	started := make(chan struct{})
	release := make(chan struct{})
	g.Go(func(ctx context.Context) {
		close(started)
		<-release
	})
	<-started
	if g.Active() != 1 {
		t.Errorf("Active = %d, want 1 during run", g.Active())
	}
	close(release)
	g.Wait()
	if g.Active() != 0 {
		t.Errorf("Active = %d, want 0 after Wait", g.Active())
	}
}

func TestGoroutine_StopCancelsContext(t *testing.T) {
	g := NewGoroutine()
	exited := make(chan struct{})
	g.Go(func(ctx context.Context) {
		<-ctx.Done()
		close(exited)
	})
	g.Stop(false)
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("goroutine did not exit after Stop")
	}
}

func TestGoroutine_StopWait(t *testing.T) {
	g := NewGoroutine()
	g.Go(func(ctx context.Context) {
		<-ctx.Done()
		time.Sleep(5 * time.Millisecond)
	})
	// Stop(true) blocks until all goroutines exit.
	g.Stop(true)
	if g.Active() != 0 {
		t.Error("Active should be 0 after Stop(true)")
	}
}

func TestGoroutine_PanicRecovery(t *testing.T) {
	g := NewGoroutine()
	g.Go(func(ctx context.Context) {
		panic("boom")
	})
	// Wait should not hang despite the panic.
	g.Wait()
	if g.Active() != 0 {
		t.Error("Active should be 0 after panicked goroutine exits")
	}
}

func TestGoroutine_DoneInitiallyClosed(t *testing.T) {
	g := NewGoroutine()
	// A fresh Goroutine with no active goroutines has a closed Done channel.
	select {
	case <-g.Done():
	default:
		t.Error("Done should be closed for an idle Goroutine")
	}
}

func TestGoroutine_Race(t *testing.T) {
	g := NewGoroutine()
	var wg sync.WaitGroup
	workers := 100
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			g.Go(func(ctx context.Context) {
				time.Sleep(time.Millisecond)
			})
		}()
	}
	wg.Wait()
	g.Wait()
	if g.Active() != 0 {
		t.Errorf("Active = %d, want 0", g.Active())
	}
	if g.Total() != uint64(workers) {
		t.Errorf("Total = %d, want %d", g.Total(), workers)
	}
}

func TestGoroutine_ReusesAfterWait(t *testing.T) {
	g := NewGoroutine()
	// First wave.
	g.Go(func(ctx context.Context) {})
	g.Wait()
	// Second wave after quiescence should re-arm Done.
	g.Go(func(ctx context.Context) {
		time.Sleep(5 * time.Millisecond)
	})
	// Done should now block until the second wave exits.
	select {
	case <-g.Done():
		t.Error("Done should not be closed while second wave is running")
	case <-time.After(time.Millisecond):
	}
	g.Wait()
}

func TestGoroutine_OnPanic(t *testing.T) {
	g := NewGoroutine()

	var (
		gotRecover interface{}
		gotStack   []byte
		wg         sync.WaitGroup
	)
	wg.Add(1)
	g.OnPanic(func(r interface{}, stack []byte) {
		gotRecover = r
		gotStack = stack
		wg.Done()
	})

	g.Go(func(ctx context.Context) {
		panic("boom")
	})
	wg.Wait()

	if gotRecover != "boom" {
		t.Errorf("OnPanic recover = %v, want %q", gotRecover, "boom")
	}
	if len(gotStack) == 0 {
		t.Error("OnPanic stack should be non-empty")
	}
	// Group must still reach quiescence despite the panic.
	g.Wait()
	if g.Active() != 0 {
		t.Errorf("Active = %d, want 0 after panicked goroutine exits", g.Active())
	}
}

func TestGoroutine_OnPanic_Nil(t *testing.T) {
	// Passing nil reverts to silent recovery (no handler call).
	g := NewGoroutine()
	called := atomic.Bool{}
	g.OnPanic(func(r interface{}, stack []byte) {
		called.Store(true)
	})
	g.OnPanic(nil)
	g.Go(func(ctx context.Context) {
		panic("hush")
	})
	g.Wait()
	if called.Load() {
		t.Error("handler should not be called after OnPanic(nil)")
	}
}

func TestGoroutine_SubGo_Basic(t *testing.T) {
	g := NewGoroutine()
	var counter int32
	cancel := g.SubGo(func(ctx context.Context) {
		atomic.AddInt32(&counter, 1)
	})
	_ = cancel
	g.Wait()
	if atomic.LoadInt32(&counter) != 1 {
		t.Errorf("counter = %d, want 1", counter)
	}
	if g.Total() != 1 {
		t.Errorf("Total = %d, want 1", g.Total())
	}
}

func TestGoroutine_SubGo_CancelChildOnly(t *testing.T) {
	g := NewGoroutine()

	childExited := make(chan struct{})
	siblingExited := make(chan struct{})

	// Child: cancelled via returned CancelFunc.
	childCancel := g.SubGo(func(ctx context.Context) {
		<-ctx.Done()
		close(childExited)
	})

	// Sibling: waits on parent context; must NOT exit when childCancel is called.
	g.Go(func(ctx context.Context) {
		<-ctx.Done()
		close(siblingExited)
	})

	childCancel()

	select {
	case <-childExited:
	case <-time.After(time.Second):
		t.Fatal("child goroutine did not exit after CancelFunc")
	}

	// Sibling should still be running.
	select {
	case <-siblingExited:
		t.Fatal("sibling should not exit when only child is cancelled")
	case <-time.After(20 * time.Millisecond):
	}

	// Now cancel parent: sibling should exit.
	g.Stop(false)
	select {
	case <-siblingExited:
	case <-time.After(time.Second):
		t.Fatal("sibling did not exit after parent Stop")
	}
}

func TestGoroutine_SubGo_PanicRecovered(t *testing.T) {
	g := NewGoroutine()

	var (
		gotRecover interface{}
		wg         sync.WaitGroup
	)
	wg.Add(1)
	g.OnPanic(func(r interface{}, stack []byte) {
		gotRecover = r
		wg.Done()
	})

	cancel := g.SubGo(func(ctx context.Context) {
		panic("sub boom")
	})
	_ = cancel
	wg.Wait()

	if gotRecover != "sub boom" {
		t.Errorf("OnPanic recover = %v, want %q", gotRecover, "sub boom")
	}
	g.Wait()
	if g.Active() != 0 {
		t.Errorf("Active = %d, want 0 after panicked SubGo exits", g.Active())
	}
}

func TestGoroutine_SubGo_NilPanics(t *testing.T) {
	g := NewGoroutine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("SubGo(nil) should panic")
		}
	}()
	g.SubGo(nil)
}
