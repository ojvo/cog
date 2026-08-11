package syncx

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ojv/cog/testx"
)

// Traffic light states for tests: green -> yellow -> red -> green
const (
	stGreen  = "green"
	stYellow = "yellow"
	stRed    = "red"

	evtToYellow = "go_yellow"
	evtToRed    = "go_red"
	evtToGreen  = "go_green"
)

func newTrafficLight() *FSM {
	f := NewFSM("traffic", stGreen)
	f.AddTransition(FSMTransition{From: stGreen, To: stYellow, Events: []string{evtToYellow}})
	f.AddTransition(FSMTransition{From: stYellow, To: stRed, Events: []string{evtToRed}})
	f.AddTransition(FSMTransition{From: stRed, To: stGreen, Events: []string{evtToGreen}})
	return f
}

func TestFSMTrigger(t *testing.T) {
	f := newTrafficLight()

	testx.Equal(t, stGreen, f.State())

	testx.Nil(t, f.Trigger(evtToYellow))
	testx.Equal(t, stYellow, f.State())

	testx.Nil(t, f.Trigger(evtToRed))
	testx.Equal(t, stRed, f.State())

	testx.Nil(t, f.Trigger(evtToGreen))
	testx.Equal(t, stGreen, f.State())
}

func TestFSMInvalidTransition(t *testing.T) {
	f := newTrafficLight()

	// green -> red is not a valid rule (only green -> yellow)
	err := f.Trigger(evtToRed)
	testx.NotNil(t, err)
	testx.True(t, errors.Is(err, ErrFSMInvalidTransition), fmt.Sprintf("want ErrFSMInvalidTransition, got %v", err))
	testx.Equal(t, stGreen, f.State())
}

func TestFSMInvalidEvent(t *testing.T) {
	f := newTrafficLight()
	err := f.Trigger("nonexistent_event")
	testx.NotNil(t, err)
	testx.True(t, errors.Is(err, ErrFSMInvalidTransition))
}

func TestFSMGuard(t *testing.T) {
	f := NewFSM("guarded", stGreen)
	guardCalled := int32(0)
	f.AddTransition(FSMTransition{
		From:      stGreen,
		To:        stRed,
		Events:     []string{"block"},
		Condition: func() bool {
			atomic.AddInt32(&guardCalled, 1)
			return false // always blocks
		},
	})

	err := f.Trigger("block")
	testx.NotNil(t, err)
	testx.True(t, errors.Is(err, ErrFSMInvalidTransition))
	testx.Equal(t, int32(1), atomic.LoadInt32(&guardCalled), "guard should be called once")
	testx.Equal(t, stGreen, f.State(), "state should not change when guard fails")

	// Replace guard to allow
	f2 := NewFSM("guarded2", stGreen)
	f2.AddTransition(FSMTransition{
		From:      stGreen,
		To:        stRed,
		Events:     []string{"allow"},
		Condition: func() bool { return true },
	})
	testx.Nil(t, f2.Trigger("allow"))
	testx.Equal(t, stRed, f2.State())
}

func TestFSMAction(t *testing.T) {
	f := NewFSM("action", stGreen)
	actionCalled := int32(0)
	f.AddTransition(FSMTransition{
		From:   stGreen,
		To:     stRed,
		Events: []string{"act"},
		Action: func() error {
			atomic.AddInt32(&actionCalled, 1)
			return nil
		},
	})

	testx.Nil(t, f.Trigger("act"))
	testx.Equal(t, int32(1), atomic.LoadInt32(&actionCalled))
	testx.Equal(t, stRed, f.State())
}

func TestFSMActionError(t *testing.T) {
	f := NewFSM("action-err", stGreen)
	wantErr := errors.New("action failed")
	f.AddTransition(FSMTransition{
		From:   stGreen,
		To:     stRed,
		Events: []string{"act"},
		Action: func() error { return wantErr },
	})

	err := f.Trigger("act")
	testx.NotNil(t, err)
	testx.True(t, errors.Is(err, wantErr), fmt.Sprintf("want %v, got %v", wantErr, err))
	testx.Equal(t, stGreen, f.State(), "state should not change when action fails")
}

func TestFSMHandlers(t *testing.T) {
	f := NewFSM("handlers", stGreen)
	var mu sync.Mutex
	var events []string

	f.AddHandlerFuncs(stGreen,
		func(from string) error {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, "enter:green<-"+from)
			return nil
		},
		func(to string) error {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, "exit:green->"+to)
			return nil
		},
	)
	f.AddHandlerFuncs(stRed,
		func(from string) error {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, "enter:red<-"+from)
			return nil
		},
		func(to string) error {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, "exit:red->"+to)
			return nil
		},
	)
	f.AddTransition(FSMTransition{From: stGreen, To: stRed, Events: []string{"go"}})

	// Initial state handler should NOT be called at construction (only on transitions).
	mu.Lock()
	testx.Equal(t, 0, len(events))
	mu.Unlock()

	testx.Nil(t, f.Trigger("go"))

	mu.Lock()
	defer mu.Unlock()
	testx.Equal(t, 2, len(events), fmt.Sprintf("want 2 events, got %v", events))
	testx.Equal(t, "exit:green->red", events[0])
	testx.Equal(t, "enter:red<-green", events[1])
}

func TestFSMOnChangeCallback(t *testing.T) {
	f := newTrafficLight()
	var mu sync.Mutex
	var changes []string
	f.SetOnChange(func(name, from, to string) {
		mu.Lock()
		defer mu.Unlock()
		changes = append(changes, from+"->"+to)
	})

	f.Trigger(evtToYellow)
	f.Trigger(evtToRed)

	mu.Lock()
	defer mu.Unlock()
	testx.Equal(t, 2, len(changes))
	testx.Equal(t, "green->yellow", changes[0])
	testx.Equal(t, "yellow->red", changes[1])
}

func TestFSMSetState(t *testing.T) {
	f := newTrafficLight()

	// green -> yellow is a valid rule
	testx.Nil(t, f.SetState(stYellow))
	testx.Equal(t, stYellow, f.State())

	// yellow -> green is NOT a valid rule
	err := f.SetState(stGreen)
	testx.NotNil(t, err)
	testx.True(t, errors.Is(err, ErrFSMInvalidTransition))
	testx.Equal(t, stYellow, f.State())
}

func TestFSMCanTransition(t *testing.T) {
	f := newTrafficLight()

	testx.True(t, f.CanTransition(evtToYellow))
	testx.False(t, f.CanTransition(evtToRed)) // green has no -> red rule
	testx.False(t, f.CanTransition("nonexistent"))
}

func TestFSMReset(t *testing.T) {
	f := newTrafficLight()
	f.Trigger(evtToYellow)
	testx.Equal(t, stYellow, f.State())

	f.Reset()
	testx.Equal(t, stGreen, f.State())
}

func TestFSMWaitForState(t *testing.T) {
	f := newTrafficLight()

	go func() {
		time.Sleep(20 * time.Millisecond)
		f.Trigger(evtToYellow)
	}()

	testx.Nil(t, f.WaitForState(stYellow, time.Second))
	testx.Equal(t, stYellow, f.State())
}

func TestFSMWaitForStateTimeout(t *testing.T) {
	f := newTrafficLight()

	err := f.WaitForState(stRed, 50*time.Millisecond)
	testx.NotNil(t, err)
}

func TestFSMConditionOnlyTransition(t *testing.T) {
	f := NewFSM("conditional", "idle")
	condCalled := int32(0)
	f.AddTransition(FSMTransition{
		From:      "idle",
		To:        "running",
		Condition: func() bool {
			return atomic.AddInt32(&condCalled, 1) >= 3 // true on 3rd check
		},
	})

	testx.Nil(t, f.Start(20*time.Millisecond))
	defer f.Stop()

	// Wait for the condition loop to fire the transition
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if f.State() == "running" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	testx.Equal(t, "running", f.State(), "condition-only transition should fire")
	testx.True(t, atomic.LoadInt32(&condCalled) >= 3)
}

func TestFSMStartTwice(t *testing.T) {
	f := NewFSM("double-start", "idle")
	testx.Nil(t, f.Start(time.Second))
	defer f.Stop()

	err := f.Start(time.Second)
	testx.NotNil(t, err)
	testx.True(t, errors.Is(err, ErrFSMAlreadyStarted), fmt.Sprintf("want ErrFSMAlreadyStarted, got %v", err))
}

func TestFSMStopRejectsTrigger(t *testing.T) {
	f := newTrafficLight()
	f.Start(time.Second)
	f.Stop()

	err := f.Trigger(evtToYellow)
	testx.NotNil(t, err)
	testx.True(t, errors.Is(err, ErrFSMStopped), fmt.Sprintf("want ErrFSMStopped, got %v", err))
	testx.Equal(t, stGreen, f.State(), "state should not change after Stop")
}

func TestFSMStopRejectsSetState(t *testing.T) {
	f := newTrafficLight()
	f.Start(time.Second)
	f.Stop()

	err := f.SetState(stYellow)
	testx.NotNil(t, err)
	testx.True(t, errors.Is(err, ErrFSMStopped))
}

func TestFSMStopWithoutStart(t *testing.T) {
	f := newTrafficLight()
	// Stop without Start should still mark as stopped and reject transitions.
	f.Stop()
	testx.True(t, f.IsStopped())

	err := f.Trigger(evtToYellow)
	testx.NotNil(t, err)
	testx.True(t, errors.Is(err, ErrFSMStopped))
}

func TestFSMConcurrentTrigger(t *testing.T) {
	f := NewFSM("concurrent", stGreen)
	// green -> red and green -> yellow both available
	f.AddTransition(FSMTransition{From: stGreen, To: stRed, Events: []string{"r"}})
	f.AddTransition(FSMTransition{From: stGreen, To: stYellow, Events: []string{"y"}})

	var wg sync.WaitGroup
	const N = 50
	wg.Add(N)
	var successCount int32
	for i := 0; i < N; i++ {
		go func(idx int) {
			defer wg.Done()
			var evt string
			if idx%2 == 0 {
				evt = "r"
			} else {
				evt = "y"
			}
			if err := f.Trigger(evt); err == nil {
				atomic.AddInt32(&successCount, 1)
			}
		}(i)
	}
	wg.Wait()

	// Exactly one Trigger should succeed (the first one to acquire transitionMu).
	testx.Equal(t, int32(1), atomic.LoadInt32(&successCount), "only one transition should win")
	testx.True(t, f.State() == stRed || f.State() == stYellow)
}

func TestFSMHandlerOnError(t *testing.T) {
	f := NewFSM("handler-err", stGreen)
	wantErr := errors.New("onexit failed")
	f.AddHandlerFuncs(stGreen, nil, func(string) error { return wantErr })
	f.AddTransition(FSMTransition{From: stGreen, To: stRed, Events: []string{"go"}})

	err := f.Trigger("go")
	testx.NotNil(t, err)
	testx.True(t, errors.Is(err, wantErr))
	// State was changed before OnExit ran in the current implementation.
	// OnExit is called while still in the old state in our impl, but if it
	// fails, the state has NOT been changed yet (we exit before the state
	// update). Verify this:
	testx.Equal(t, stGreen, f.State(), "state should not change if OnExit fails")
}

func TestFSMSameStateTransition(t *testing.T) {
	f := NewFSM("same", stGreen)
	f.AddTransition(FSMTransition{From: stGreen, To: stGreen, Events: []string{"self"}})

	// Transition to same state should be a no-op success.
	testx.Nil(t, f.Trigger("self"))
	testx.Equal(t, stGreen, f.State())
}
