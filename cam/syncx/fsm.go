package syncx

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrFSMInvalidTransition is returned when a transition is not allowed.
var ErrFSMInvalidTransition = errors.New("fsm: invalid transition")

// ErrFSMAlreadyStarted is returned when Start is called on a running FSM.
var ErrFSMAlreadyStarted = errors.New("fsm: already started")

// ErrFSMStopped is returned by Trigger/SetState after Stop has been called.
var ErrFSMStopped = errors.New("fsm: stopped")

// FSMTransition defines a state transition rule.
type FSMTransition struct {
	From      string       // source state
	To        string       // target state
	Events    []string     // events that trigger this transition (empty = condition-only)
	Condition func() bool  // guard; if nil, always allowed
	Action    func() error // action executed before state changes
}

// FSMHandler handles state lifecycle events.
type FSMHandler interface {
	OnEnter(from string) error
	OnExit(to string) error
}

// FSMHandlerFuncs is a function-based adapter implementing FSMHandler.
// Nil fields are no-ops.
type FSMHandlerFuncs struct {
	OnEnterFunc func(from string) error
	OnExitFunc  func(to string) error
}

func (h FSMHandlerFuncs) OnEnter(from string) error {
	if h.OnEnterFunc != nil {
		return h.OnEnterFunc(from)
	}
	return nil
}

func (h FSMHandlerFuncs) OnExit(to string) error {
	if h.OnExitFunc != nil {
		return h.OnExitFunc(to)
	}
	return nil
}

// FSMOnChange is invoked after a successful state transition.
// name is the FSM name; from/to are the old and new states.
type FSMOnChange func(name, from, to string)

// FSM is a finite state machine with transitions, guards, actions, and
// per-state handlers. It is safe for concurrent use.
//
// Transitions are serialized via an internal mutex: a Trigger or SetState
// call holds the transition lock for its entire duration, including handler
// invocations. Handlers that need to call back into the FSM must do so from a
// separate goroutine to avoid deadlock.
//
// Stop is a terminal state: after Stop, Trigger/SetState return ErrFSMStopped.
type FSM struct {
	name         string
	current      string
	initial      string
	transitions  map[string][]FSMTransition
	handlers     map[string]FSMHandler
	onChange     FSMOnChange
	logger       func(format string, args ...interface{})
	stateChanged chan string

	transitionMu sync.Mutex   // serializes transitions (includes handler calls)
	mu           sync.RWMutex // protects fields below
	stopped      bool
	started      bool
	cancel       context.CancelFunc
	ctx          context.Context
}

// NewFSM creates a finite state machine with the given name and initial state.
// The FSM is not started; call Start to begin condition-checking.
func NewFSM(name, initialState string) *FSM {
	return &FSM{
		name:         name,
		current:      initialState,
		initial:      initialState,
		transitions:  make(map[string][]FSMTransition),
		handlers:     make(map[string]FSMHandler),
		stateChanged: make(chan string, 16),
		logger:       func(string, ...interface{}) {},
	}
}

// SetOnChange registers a callback invoked after each successful transition.
// Must be called before Start.
func (f *FSM) SetOnChange(cb FSMOnChange) *FSM {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onChange = cb
	return f
}

// SetLogger sets a logger function. Must be called before Start.
func (f *FSM) SetLogger(logger func(format string, args ...interface{})) *FSM {
	f.mu.Lock()
	defer f.mu.Unlock()
	if logger != nil {
		f.logger = logger
	}
	return f
}

// AddTransition registers a transition rule.
func (f *FSM) AddTransition(t FSMTransition) *FSM {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.transitions[t.From] = append(f.transitions[t.From], t)
	return f
}

// AddSimpleTransition is a shortcut for a no-event, no-guard, no-action
// transition from one state to another. Trigger is via SetState.
func (f *FSM) AddSimpleTransition(from, to string) *FSM {
	return f.AddTransition(FSMTransition{From: from, To: to})
}

// AddHandler registers a state handler for lifecycle events.
func (f *FSM) AddHandler(state string, h FSMHandler) *FSM {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[state] = h
	return f
}

// AddHandlerFuncs is a shortcut for AddHandler with FSMHandlerFuncs.
func (f *FSM) AddHandlerFuncs(state string, onEnter func(string) error, onExit func(string) error) *FSM {
	return f.AddHandler(state, FSMHandlerFuncs{OnEnterFunc: onEnter, OnExitFunc: onExit})
}

// State returns the current state.
func (f *FSM) State() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.current
}

// IsStarted reports whether Start has been called.
func (f *FSM) IsStarted() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.started
}

// IsStopped reports whether Stop has been called.
func (f *FSM) IsStopped() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.stopped
}

// CanTransition reports whether any transition from the current state is
// allowed for the given event (i.e. the guard passes if defined).
func (f *FSM) CanTransition(event string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, t := range f.transitions[f.current] {
		for _, e := range t.Events {
			if e == event && (t.Condition == nil || t.Condition()) {
				return true
			}
		}
	}
	return false
}

// Trigger attempts to transition based on the given event.
// Returns ErrFSMStopped if Stop has been called.
// Returns ErrFSMInvalidTransition if no matching rule exists or the guard fails.
// Returns any action error before the state change is applied.
func (f *FSM) Trigger(event string) error {
	if err := f.checkStopped(); err != nil {
		return err
	}

	f.transitionMu.Lock()
	defer f.transitionMu.Unlock()

	f.mu.RLock()
	current := f.current
	transitions := f.transitions[current]
	f.mu.RUnlock()

	for _, t := range transitions {
		for _, e := range t.Events {
			if e != event {
				continue
			}
			if t.Condition != nil && !t.Condition() {
				return fmt.Errorf("%w: %s -> %s (guard failed)", ErrFSMInvalidTransition, current, t.To)
			}
			if t.Action != nil {
				if err := t.Action(); err != nil {
					return fmt.Errorf("fsm action failed: %w", err)
				}
			}
			return f.applyTransition(current, t.To)
		}
	}
	return fmt.Errorf("%w: no rule for event %q from %s", ErrFSMInvalidTransition, event, current)
}

// SetState forces a transition to target if a rule exists from the current
// state and its guard passes. Unlike Trigger, it does not require an event.
func (f *FSM) SetState(target string) error {
	if err := f.checkStopped(); err != nil {
		return err
	}

	f.transitionMu.Lock()
	defer f.transitionMu.Unlock()

	f.mu.RLock()
	current := f.current
	transitions := f.transitions[current]
	f.mu.RUnlock()

	for _, t := range transitions {
		if t.To != target {
			continue
		}
		if t.Condition != nil && !t.Condition() {
			return fmt.Errorf("%w: %s -> %s (guard failed)", ErrFSMInvalidTransition, current, target)
		}
		if t.Action != nil {
			if err := t.Action(); err != nil {
				return fmt.Errorf("fsm action failed: %w", err)
			}
		}
		return f.applyTransition(current, target)
	}
	return fmt.Errorf("%w: %s -> %s", ErrFSMInvalidTransition, current, target)
}

// checkStopped returns ErrFSMStopped if the FSM has been stopped.
func (f *FSM) checkStopped() error {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.stopped {
		return ErrFSMStopped
	}
	return nil
}

// applyTransition performs the actual state change. The caller must hold
// transitionMu and have already validated the transition (guard + action).
func (f *FSM) applyTransition(from, to string) error {
	if from == to {
		return nil
	}

	// OnExit for old state (runs while current == from)
	f.mu.RLock()
	oldHandler := f.handlers[from]
	f.mu.RUnlock()
	if oldHandler != nil {
		if err := oldHandler.OnExit(to); err != nil {
			return fmt.Errorf("fsm OnExit %s: %w", from, err)
		}
	}

	// Apply state change
	f.mu.Lock()
	f.current = to
	onChange := f.onChange
	name := f.name
	logger := f.logger
	newHandler := f.handlers[to]
	f.mu.Unlock()

	logger("fsm [%s]: %s -> %s", name, from, to)

	// Non-blocking notify for WaitForState
	select {
	case f.stateChanged <- to:
	default:
	}

	if onChange != nil {
		onChange(name, from, to)
	}

	// OnEnter for new state
	if newHandler != nil {
		if err := newHandler.OnEnter(from); err != nil {
			return fmt.Errorf("fsm OnEnter %s: %w", to, err)
		}
	}
	return nil
}

// Start enables the background condition-check loop. The loop polls
// transitions without events (Condition-only) at the given interval and
// applies the first matching one.
// If interval <= 0, a default of 500ms is used.
// Returns ErrFSMAlreadyStarted if already started.
func (f *FSM) Start(interval time.Duration) error {
	f.mu.Lock()
	if f.started {
		f.mu.Unlock()
		return ErrFSMAlreadyStarted
	}
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	f.ctx, f.cancel = context.WithCancel(context.Background())
	f.started = true
	f.mu.Unlock()

	go f.conditionLoop(interval)
	return nil
}

// Stop halts the background condition-check loop. Stop is terminal: subsequent
// Trigger/SetState calls return ErrFSMStopped.
func (f *FSM) Stop() {
	f.mu.Lock()
	if !f.started {
		f.stopped = true
		f.mu.Unlock()
		return
	}
	f.started = false
	f.stopped = true
	cancel := f.cancel
	f.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Reset returns the FSM to its initial state without invoking handlers.
// Reset does not un-stop a stopped FSM.
func (f *FSM) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current = f.initial
}

// WaitForState blocks until the FSM reaches target or timeout elapses.
// Returns nil on success, the timeout error otherwise.
// If the FSM is stopped while waiting, returns ErrFSMStopped.
func (f *FSM) WaitForState(target string, timeout time.Duration) error {
	if f.State() == target {
		return nil
	}
	if f.IsStopped() {
		return ErrFSMStopped
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	for {
		select {
		case s := <-f.stateChanged:
			if s == target {
				return nil
			}
		case <-t.C:
			return fmt.Errorf("fsm: wait for %s timeout", target)
		case <-f.doneCh():
			return ErrFSMStopped
		}
	}
}

// doneCh returns a channel closed when the FSM is stopped. If the FSM was
// never started, returns a closed channel (WaitForState will then rely on
// timeout and stateChanged only).
func (f *FSM) doneCh() <-chan struct{} {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.ctx == nil {
		// Never started: return a nil channel that blocks forever, so
		// WaitForState only exits on stateChanged or timeout.
		return nil
	}
	return f.ctx.Done()
}

// conditionLoop polls condition-only transitions at the given interval.
func (f *FSM) conditionLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			f.checkConditions()
		case <-f.ctx.Done():
			return
		}
	}
}

func (f *FSM) checkConditions() {
	if err := f.checkStopped(); err != nil {
		return
	}

	// Use transitionMu to serialize with any Trigger/SetState in flight.
	f.transitionMu.Lock()
	defer f.transitionMu.Unlock()

	f.mu.RLock()
	current := f.current
	transitions := f.transitions[current]
	f.mu.RUnlock()

	for _, t := range transitions {
		if len(t.Events) != 0 || t.Condition == nil {
			continue
		}
		if !t.Condition() {
			continue
		}
		if t.Action != nil {
			if err := t.Action(); err != nil {
				f.mu.RLock()
				logger := f.logger
				name := f.name
				f.mu.RUnlock()
				logger("fsm [%s] condition action failed: %v", name, err)
				continue
			}
		}
		if err := f.applyTransition(current, t.To); err != nil {
			f.mu.RLock()
			logger := f.logger
			name := f.name
			f.mu.RUnlock()
			logger("fsm [%s] condition transition failed: %v", name, err)
		}
		return // only first matching condition per tick
	}
}
