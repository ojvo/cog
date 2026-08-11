package timer

import (
	"testing"
	"time"
)

type TestData struct {
	Val string
}

func TestTimeWheel(t *testing.T) {
	// Job callback
	ch := make(chan string, 10)
	job := func(data TestData) {
		ch <- data.Val
	}

	// 100ms interval, 10 slots -> 1s total cycle
	tw := New[string, TestData](100*time.Millisecond, 10, job)
	tw.Start()
	defer tw.Stop()

	// Add task: delay 200ms
	tw.AddTimer(200*time.Millisecond, "task1", TestData{Val: "A"})
	
	// Add task: delay 1.2s (1 cycle + 2 slots)
	tw.AddTimer(1200*time.Millisecond, "task2", TestData{Val: "B"})

	// Verify task1
	select {
	case res := <-ch:
		if res != "A" {
			t.Errorf("Expected A, got %s", res)
		}
	case <-time.After(500 * time.Millisecond):
		t.Errorf("Timeout waiting for task1")
	}

	// Verify task2
	select {
	case res := <-ch:
		if res != "B" {
			t.Errorf("Expected B, got %s", res)
		}
	case <-time.After(1500 * time.Millisecond):
		t.Errorf("Timeout waiting for task2")
	}
}

func TestTimeWheel_Remove(t *testing.T) {
	ch := make(chan string, 10)
	job := func(data string) {
		ch <- "run"
	}

	tw := New[string, string](100*time.Millisecond, 10, job)
	tw.Start()
	defer tw.Stop()

	// Add task: delay 500ms
	tw.AddTimer(500*time.Millisecond, "remove_me", "dummy")
	
	// Remove it immediately
	tw.RemoveTimer("remove_me")

	select {
	case <-ch:
		t.Errorf("Task should have been removed")
	case <-time.After(1 * time.Second):
		// OK
	}
}

func TestTimeWheel_Stop(t *testing.T) {
	tw := New[string, int](100*time.Millisecond, 10, func(data int) {})
	tw.Start()
	tw.Stop()

	// Should not block
	done := make(chan struct{})
	go func() {
		tw.AddTimer(100*time.Millisecond, "test", 0)
		tw.RemoveTimer("test")
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Fatal("TimeWheel operations blocked after Stop")
	}
	
	// Double stop should be safe
	tw.Stop()
}

func TestTimeWheel_Upsert(t *testing.T) {
	ch := make(chan string, 10)
	job := func(data TestData) {
		ch <- data.Val
	}

	tw := New[string, TestData](50*time.Millisecond, 20, job)
	tw.Start()
	defer tw.Stop()

	// Add task A, delay 200ms
	tw.AddTimer(200*time.Millisecond, "key1", TestData{Val: "A"})
	
	// Immediately overwrite with task B, delay 300ms
	tw.AddTimer(300*time.Millisecond, "key1", TestData{Val: "B"})

	// We expect ONLY B to run, after ~300ms. A should be cancelled.
	// If A runs, we get "A" at ~200ms.
	
	select {
	case res := <-ch:
		if res == "A" {
			t.Errorf("Task A should have been cancelled/overwritten")
		} else if res == "B" {
			// OK, B ran first (as expected if A was cancelled)
		} else {
			t.Errorf("Unexpected result: %s", res)
		}
	case <-time.After(500 * time.Millisecond):
		t.Errorf("Timeout waiting for task B")
	}
	
	// Ensure no other task runs
	select {
	case res := <-ch:
		t.Errorf("Unexpected second task ran: %s", res)
	case <-time.After(200 * time.Millisecond):
		// OK
	}
}

func TestTimeWheel_ZeroDelay(t *testing.T) {
	ch := make(chan string, 10)
	job := func(data string) {
		ch <- data
	}

	// 10ms interval
	tw := New[string, string](10*time.Millisecond, 10, job)
	tw.Start()
	defer tw.Stop()

	// Add task with 0 delay (should be scheduled for next tick, i.e. ~10ms)
	start := time.Now()
	tw.AddTimer(0, "zero", "done")

	select {
	case res := <-ch:
		if res != "done" {
			t.Errorf("Expected done, got %s", res)
		}
		elapsed := time.Since(start)
		// Should be roughly 10ms (1 tick)
		// If it was dropped, we timeout.
		if elapsed > 100*time.Millisecond {
			t.Logf("Zero delay task took %v", elapsed)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Zero delay task did not run")
	}
}

func TestTimeWheel_CatchUp(t *testing.T) {
	// Test the bulk catch-up optimization
	ch := make(chan int, 100)
	job := func(val int) {
		ch <- val
	}

	interval := 1 * time.Millisecond
	slotNum := 10
	tw := New[int, int](interval, slotNum, job)
	
	// Manually set start time to simulate a long sleep
	// We want to simulate that we slept for 1000 ticks (1s)
	// Normal loop would iterate 1000 times.
	// Bulk optimization should iterate 10 (slotNum) times.
	
	tw.Start()
	defer tw.Stop()
	
	// Add tasks distributed in slots
	// Slot 0, 1, 2...
	// We add tasks that expire "soon" relative to "now".
	// But we want to simulate the catch-up.
	
	// To test catch-up, we need to block the ticker or manipulate time?
	// We can't easily block the internal ticker.
	// But we can pause the test execution, then Resume?
	// No, the internal ticker keeps firing.
	
	// Alternative: Manually manipulate `tw.tick` and `tw.startTime`?
	// But `tw` fields are unexported (except in this package).
	// Since we are in `timer` package (package timer), we can access unexported fields!
	
	tw.Stop() // Stop the real ticker
	
	// Re-initialize for manual driving
	tw = New[int, int](interval, slotNum, job)
	// Don't call Start(), we will drive it manually.
	tw.startTime = time.Now().Add(-10 * time.Second) // Started 10s ago
	tw.tick = 0 // But tick is 0. So we are WAY behind.
	
	// Add tasks
	// Task 1: expire at tick 100 (in past relative to 10s ago, but future relative to tick 0)
	// Task 2: expire at tick 500
	// Task 3: expire at tick 9000 (still behind 10s=10000 ticks)
	
	// Note: AddTimer uses getPositionAndExpire which uses tw.tick.
	// If tw.tick = 0.
	// AddTimer(100ms) -> expire = 100.
	
	tw.AddTimer(100*time.Millisecond, 1, 1)
	tw.AddTimer(500*time.Millisecond, 2, 2)
	tw.AddTimer(9000*time.Millisecond, 3, 3)
	
	// Now we call tickHandler with "Now".
	// elapsed = 10s. expectedTick = 10000.
	// tick = 0.
	// Diff = 10000 > slotNum(10).
	// Should trigger bulk scan.
	
	// We expect ALL 3 tasks to fire because 100, 500, 9000 <= 10000.
	
	tw.tickHandler(time.Now())
	
	// Verify results
	count := 0
	for i := 0; i < 3; i++ {
		select {
		case <-ch:
			count++
		case <-time.After(100 * time.Millisecond):
			// wait
		}
	}
	
	if count != 3 {
		t.Errorf("Expected 3 catch-up tasks, got %d", count)
	}
	
	// Verify tick was updated
	if tw.tick < 9900 {
		t.Errorf("Tick should have caught up to ~10000, got %d", tw.tick)
	}
}
