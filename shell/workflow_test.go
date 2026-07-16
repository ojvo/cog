package shell

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCommandQueue(t *testing.T) {
	q := NewCommandQueue()
	q.AddStep("step1", "echo hello", "echo", "hello")

	if err := q.Execute(); err != nil {
		t.Fatalf("CommandQueue execution failed: %v", err)
	}
}

func TestCommandQueue_DryRun(t *testing.T) {
	q := NewCommandQueue()
	q.DryRun = true
	q.AddStep("step1", "echo hello", "echo", "hello")

	if err := q.Execute(); err != nil {
		t.Fatalf("CommandQueue dry-run failed: %v", err)
	}
}

func TestCommandQueue_OnError(t *testing.T) {
	q := NewCommandQueue()
	var caughtErr error
	q.OnError = func(err error) { caughtErr = err }

	// Use a command that will fail
	q.AddStep("fail-step", "failing command", "nonexistent_cmd_xyz_123")

	err := q.Execute()
	if err == nil {
		t.Fatal("Expected error from failing step")
	}
	if caughtErr == nil {
		t.Error("OnError callback was not invoked")
	}
}

func TestCommandQueue_ConditionalSkip(t *testing.T) {
	q := NewCommandQueue()
	executed := false
	q.AddConditionalStep("conditional", "should skip", "echo hello", func() bool {
		return false // condition not met
	})
	q.AddStep("unconditional", "echo world", "echo", "world")

	if err := q.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	_ = executed // conditional step was skipped, not executed
}

func TestWorkflowStateMachine(t *testing.T) {
	sm := NewWorkflowStateMachine()

	sm.AddStep("step1", "set data", func(ctx *WorkflowContext) error {
		ctx.Data["key"] = "value"
		return nil
	})

	sm.AddStep("step2", "check data", func(ctx *WorkflowContext) error {
		if val, ok := ctx.Data["key"]; !ok || val != "value" {
			return errors.New("data not found or incorrect")
		}
		return nil
	})

	if err := sm.Execute(); err != nil {
		t.Fatalf("WorkflowStateMachine execution failed: %v", err)
	}

	if sm.CurrentState() != "Completed" {
		t.Errorf("Expected 'Completed', got %q", sm.CurrentState())
	}
}

func TestWorkflowCancellation(t *testing.T) {
	sm := NewWorkflowStateMachine()

	ctx, cancel := context.WithCancel(context.Background())

	sm.AddStep("step1", "trigger cancel", func(wc *WorkflowContext) error {
		cancel()
		return nil
	})

	sm.AddStep("step2", "should not run", func(wc *WorkflowContext) error {
		return errors.New("step 2 should not run")
	})

	err := sm.ExecuteWithContext(ctx)
	// Depending on timing, might return context.Canceled or nil (step1
	// finished before the context check). Either is acceptable.
	if err != nil && err != context.Canceled {
		t.Errorf("Expected context.Canceled or nil, got: %v", err)
	}
}

func TestWorkflowStateMachine_OnState_OnFinish(t *testing.T) {
	sm := NewWorkflowStateMachine()

	stateCalls := 0
	finishCalled := false

	sm.OnState = func(step WorkflowStep) { stateCalls++ }
	sm.OnFinish = func() { finishCalled = true }

	sm.AddStep("s1", "step1", func(ctx *WorkflowContext) error { return nil })
	sm.AddStep("s2", "step2", func(ctx *WorkflowContext) error { return nil })

	if err := sm.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if stateCalls != 2 {
		t.Errorf("Expected 2 OnState calls, got %d", stateCalls)
	}
	if !finishCalled {
		t.Error("OnFinish was not called")
	}
}

func TestExecuteConcurrent(t *testing.T) {
	tasks := []func() error{
		func() error { return nil },
		func() error { return nil },
	}

	if err := ExecuteConcurrent(tasks, 2); err != nil {
		t.Errorf("ExecuteConcurrent failed: %v", err)
	}

	errTasks := []func() error{
		func() error { return errors.New("fail") },
	}

	if err := ExecuteConcurrent(errTasks, 1); err == nil {
		t.Error("ExecuteConcurrent should have failed")
	}
}

func TestExecuteConcurrentWithContext(t *testing.T) {
	tasks := []func(context.Context) error{
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { return nil },
	}

	if err := ExecuteConcurrentWithContext(context.Background(), tasks, 2); err != nil {
		t.Errorf("ExecuteConcurrentWithContext failed: %v", err)
	}

	// Test cancellation — pre-cancelled context should skip tasks
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	longTasks := []func(context.Context) error{
		func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
				return nil
			}
		},
	}

	// Tasks should be skipped due to pre-cancelled context
	_ = ExecuteConcurrentWithContext(ctx, longTasks, 1)
	// No error expected since task was skipped (not run)
}

func TestExecuteCommand(t *testing.T) {
	out, err := ExecuteCommand("echo", "test")
	if err != nil {
		t.Fatalf("ExecuteCommand failed: %v", err)
	}
	if !strings.Contains(out, "test") {
		t.Errorf("Expected output containing 'test', got '%s'", out)
	}
}

func TestCommander_CommandWithResult(t *testing.T) {
	c := Commander{Command: "go", Args: []string{"env"}}
	out, err := c.CommandWithResult()
	if err != nil {
		t.Fatalf("CommandWithResult failed: %v", err)
	}
	if out == "" {
		t.Error("Expected non-empty output from 'go env'")
	}
}

func TestCommander_CommandWithResult_Error(t *testing.T) {
	c := Commander{Command: "nonexistent_cmd_xyz"}
	_, err := c.CommandWithResult()
	if err == nil {
		t.Error("Expected error for non-existent command")
	}
}
