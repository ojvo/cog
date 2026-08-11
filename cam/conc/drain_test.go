package conc

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestDrain_ConsumesUntilClosed(t *testing.T) {
	ch := make(chan int, 2)
	var exited atomic.Bool
	Drain(ch, WithDrainOnExit(func() { exited.Store(true) }))

	// Fill beyond buffer: without a consumer this would block forever.
	for i := 0; i < 100; i++ {
		ch <- i
	}
	close(ch)

	deadline := time.After(5 * time.Second)
	for !exited.Load() {
		select {
		case <-deadline:
			t.Fatal("drain goroutine did not exit after channel closed")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestDrain_HookObservesItems(t *testing.T) {
	ch := make(chan string, 1)
	var got atomic.Int64
	Drain(ch, WithDrainHook(func(v any) { got.Add(1) }))

	ch <- "a"
	ch <- "b"
	close(ch)

	deadline := time.After(5 * time.Second)
	for got.Load() != 2 {
		select {
		case <-deadline:
			t.Fatal("hook did not observe all items")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestDrain_NeverClosed_NoHook(t *testing.T) {
	ch := make(chan struct{}, 1)
	Drain(ch)
	ch <- struct{}{}
	// No assertion: just verify the API doesn't panic and returns immediately.
	// A never-closed channel legitimately keeps the drain goroutine alive.
}
