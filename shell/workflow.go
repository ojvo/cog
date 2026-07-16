package shell

import (
	"context"
	"fmt"
	"os"
	"sync"
)

// ExecuteCommand runs a command in exec mode and returns stdout.
// On error, stderr is included in the error message.
func ExecuteCommand(name string, args ...string) (string, error) {
	cmd := NewCommand(name, WithExecMode(true), WithArgs(args...))
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %v: %w\n%s", name, args, err, cmd.Status.Stderr)
	}
	return cmd.Status.Stdout, nil
}

// RunCommand runs a command in exec mode with real-time output to os.Stdout/os.Stderr.
func RunCommand(name string, args ...string) error {
	cmd := NewCommand(name, WithExecMode(true), WithArgs(args...), WithStdout(os.Stdout), WithStderr(os.Stderr))
	return cmd.Run()
}

// RunCommandContext is like RunCommand but with a parent context.
func RunCommandContext(ctx context.Context, name string, args ...string) error {
	cmd := NewCommand(name, WithExecMode(true), WithArgs(args...), WithStdout(os.Stdout), WithStderr(os.Stderr), WithContext(ctx))
	return cmd.Run()
}

// -----------------------------------------------------------------------
// CommandQueue — ordered command pipeline with conditions and dry-run
// -----------------------------------------------------------------------

// CommandStep represents a single step in a CommandQueue.
type CommandStep struct {
	Name        string
	Description string
	Command     string
	Args        []string
	Condition   func() bool
	Skip        bool
}

// CommandQueue executes a sequence of commands in order, with optional
// conditional steps, dry-run mode, and success/error callbacks.
type CommandQueue struct {
	Steps     []CommandStep
	DryRun    bool
	Verbose   bool
	OnSuccess func()
	OnError   func(error)
}

// NewCommandQueue creates an empty CommandQueue.
func NewCommandQueue() *CommandQueue {
	return &CommandQueue{
		Steps: make([]CommandStep, 0),
	}
}

// AddStep appends an unconditional step.
func (q *CommandQueue) AddStep(name, description, command string, args ...string) *CommandQueue {
	q.Steps = append(q.Steps, CommandStep{
		Name:        name,
		Description: description,
		Command:     command,
		Args:        args,
	})
	return q
}

// AddConditionalStep appends a step that only runs if condition returns true.
func (q *CommandQueue) AddConditionalStep(name, description, command string, condition func() bool, args ...string) *CommandQueue {
	q.Steps = append(q.Steps, CommandStep{
		Name:        name,
		Description: description,
		Command:     command,
		Args:        args,
		Condition:   condition,
	})
	return q
}

// Execute runs all steps with context.Background().
func (q *CommandQueue) Execute() error {
	return q.ExecuteWithContext(context.Background())
}

// ExecuteWithContext runs all steps, respecting context cancellation.
func (q *CommandQueue) ExecuteWithContext(ctx context.Context) error {
	if q.Verbose {
		fmt.Printf("Executing command queue with %d steps\n", len(q.Steps))
	}

	for i, step := range q.Steps {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if step.Condition != nil && !step.Condition() {
			if q.Verbose {
				fmt.Printf("Skipping step [%d/%d]: %s - condition not met\n", i+1, len(q.Steps), step.Name)
			}
			continue
		}

		if step.Skip {
			if q.Verbose {
				fmt.Printf("Skipping step [%d/%d]: %s\n", i+1, len(q.Steps), step.Name)
			}
			continue
		}

		if q.Verbose {
			fmt.Printf("Executing step [%d/%d]: %s\n", i+1, len(q.Steps), step.Name)
			fmt.Printf("  Description: %s\n", step.Description)
		}

		if q.DryRun {
			fmt.Printf("  [DRY-RUN] %s %v\n", step.Command, step.Args)
			continue
		}

		if err := RunCommandContext(ctx, step.Command, step.Args...); err != nil {
			wrapped := fmt.Errorf("step %d (%s) failed: %w", i+1, step.Name, err)
			if q.OnError != nil {
				q.OnError(wrapped)
			}
			return wrapped
		}
	}

	if q.OnSuccess != nil {
		q.OnSuccess()
	}
	return nil
}

// -----------------------------------------------------------------------
// WorkflowStateMachine — generic step-based workflow with shared context
// -----------------------------------------------------------------------

// WorkflowContext carries shared data between workflow steps.
type WorkflowContext struct {
	Data map[string]interface{}
}

// WorkflowStep represents a single step in a WorkflowStateMachine.
type WorkflowStep struct {
	Name        string
	Description string
	Execute     func(ctx *WorkflowContext) error
	Condition   func(ctx *WorkflowContext) bool
}

// WorkflowStateMachine executes a sequence of typed steps with a shared
// WorkflowContext, supporting conditions, dry-run, and callbacks.
type WorkflowStateMachine struct {
	Steps    []WorkflowStep
	Current  int
	Context  *WorkflowContext
	DryRun   bool
	Verbose  bool
	OnState  func(step WorkflowStep)
	OnError  func(error)
	OnFinish func()
}

// NewWorkflowStateMachine creates a new state machine with an empty context.
func NewWorkflowStateMachine() *WorkflowStateMachine {
	return &WorkflowStateMachine{
		Context: &WorkflowContext{
			Data: make(map[string]interface{}),
		},
	}
}

// AddStep appends an unconditional step.
func (sm *WorkflowStateMachine) AddStep(name, description string, execute func(ctx *WorkflowContext) error) *WorkflowStateMachine {
	sm.Steps = append(sm.Steps, WorkflowStep{
		Name:        name,
		Description: description,
		Execute:     execute,
	})
	return sm
}

// AddConditionalStep appends a step that only runs if condition returns true.
func (sm *WorkflowStateMachine) AddConditionalStep(name, description string, condition func(ctx *WorkflowContext) bool, execute func(ctx *WorkflowContext) error) *WorkflowStateMachine {
	sm.Steps = append(sm.Steps, WorkflowStep{
		Name:        name,
		Description: description,
		Execute:     execute,
		Condition:   condition,
	})
	return sm
}

// CurrentState returns the name of the current step, or "Completed" if done.
func (sm *WorkflowStateMachine) CurrentState() string {
	if sm.Current >= len(sm.Steps) {
		return "Completed"
	}
	return sm.Steps[sm.Current].Name
}

// Execute runs all steps with context.Background().
func (sm *WorkflowStateMachine) Execute() error {
	return sm.ExecuteWithContext(context.Background())
}

// ExecuteWithContext runs all steps, respecting context cancellation.
func (sm *WorkflowStateMachine) ExecuteWithContext(ctx context.Context) error {
	for sm.Current < len(sm.Steps) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		step := sm.Steps[sm.Current]

		if step.Condition != nil && !step.Condition(sm.Context) {
			if sm.Verbose {
				fmt.Printf("Skipping step [%d/%d]: %s - condition not met\n", sm.Current+1, len(sm.Steps), step.Name)
			}
			sm.Current++
			continue
		}

		if sm.Verbose {
			fmt.Printf("Executing step [%d/%d]: %s\n", sm.Current+1, len(sm.Steps), step.Name)
			fmt.Printf("  Description: %s\n", step.Description)
		}

		if sm.OnState != nil {
			sm.OnState(step)
		}

		if !sm.DryRun {
			if err := step.Execute(sm.Context); err != nil {
				wrapped := fmt.Errorf("step %d (%s) failed: %w", sm.Current+1, step.Name, err)
				if sm.OnError != nil {
					sm.OnError(wrapped)
				}
				return wrapped
			}
		}

		sm.Current++
	}

	if sm.OnFinish != nil {
		sm.OnFinish()
	}
	return nil
}

// -----------------------------------------------------------------------
// ExecuteConcurrent — bounded concurrent task execution
// -----------------------------------------------------------------------

// ExecuteConcurrent runs tasks with a concurrency limit, collecting all errors.
// Returns nil if all tasks succeed; otherwise returns an aggregated error.
func ExecuteConcurrent(tasks []func() error, maxConcurrency int) error {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, maxConcurrency)
	errChan := make(chan error, len(tasks))

	for _, task := range tasks {
		wg.Add(1)
		go func(t func() error) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			if err := t(); err != nil {
				errChan <- err
			}
		}(task)
	}

	wg.Wait()
	close(errChan)

	var errorList []error
	for err := range errChan {
		errorList = append(errorList, err)
	}

	if len(errorList) > 0 {
		return fmt.Errorf("%d errors occurred: %v", len(errorList), errorList)
	}
	return nil
}

// ExecuteConcurrentWithContext runs context-aware tasks with a concurrency
// limit. On the first error, remaining tasks are cancelled via context.
// Returns nil if all tasks succeed; otherwise returns an aggregated error.
func ExecuteConcurrentWithContext(ctx context.Context, tasks []func(context.Context) error, maxConcurrency int) error {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, maxConcurrency)
	errChan := make(chan error, len(tasks))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for _, task := range tasks {
		wg.Add(1)
		go func(t func(context.Context) error) {
			defer wg.Done()

			select {
			case <-ctx.Done():
				return
			case semaphore <- struct{}{}:
			}
			defer func() { <-semaphore }()

			select {
			case <-ctx.Done():
				return
			default:
			}

			if err := t(ctx); err != nil {
				select {
				case errChan <- err:
					cancel()
				default:
				}
			}
		}(task)
	}

	wg.Wait()
	close(errChan)

	var errorList []error
	for err := range errChan {
		errorList = append(errorList, err)
	}

	if len(errorList) > 0 {
		return fmt.Errorf("%d errors occurred: %v", len(errorList), errorList)
	}
	return nil
}
