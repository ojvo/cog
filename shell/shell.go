package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"ojv/cog/procmgmt"
)

// Errors returned by Cmd operations.
var (
	ErrAlreadyFinished      = errors.New("already finished")
	ErrNotFoundCommand      = errors.New("command not found")
	ErrNotExecutePermission = errors.New("not execute permission")
	ErrInvalidArgs          = errors.New("invalid argument to exit")
	ErrProcessTimeout       = errors.New("process timeout")
	ErrProcessCancel        = errors.New("process cancelled")

	ErrLineBufferOverflow = errors.New("line buffer overflow")
)

// DefaultExitCode is used when the command cannot even start.
const DefaultExitCode = 2

// Cmd is an async command runner with option-chainable configuration.
// A Cmd is single-use: after Run/Wait completes, create a new one or use Clone.
type Cmd struct {
	ctx       context.Context
	cancel    context.CancelFunc
	parentCtx context.Context
	stdcmd    *exec.Cmd

	mu sync.Mutex

	Bash      string
	Args      []string // Arguments for ExecMode (ShellMode=false)
	Shell     string   // custom shell, default auto-detected from OS
	ShellMode bool
	Status    Status
	Env       []string
	Dir       string

	isFinalized bool

	timeout    int
	statusChan chan Status
	doneChan   chan error

	output ThreadSafeBuffer // stdout + stderr combined
	stdout bytes.Buffer
	stderr bytes.Buffer

	Stdout io.Writer // extra stdout writer (optional)
	Stderr io.Writer // extra stderr writer (optional)
}

// Status holds the final result of a command execution.
type Status struct {
	PID      int
	Finish   bool
	ExitCode int
	Error    error
	CostTime time.Duration

	Output string // stdout + stderr combined
	Stdout string
	Stderr string

	startTime time.Time
	endTime   time.Time
}

// Option configures a Cmd.
type Option func(*Cmd)

// WithTimeout sets the command timeout in seconds.
func WithTimeout(seconds int) Option {
	return func(o *Cmd) { o.timeout = seconds }
}

// WithShellMode enables shell mode (default: true).
func WithShellMode() Option {
	return func(o *Cmd) { o.ShellMode = true }
}

// WithExecMode enables exec mode (direct execution without shell wrapping).
func WithExecMode(_ bool) Option {
	return func(o *Cmd) { o.ShellMode = false }
}

// WithShell sets a custom shell (e.g. "sh", "zsh", "powershell", "cmd").
func WithShell(shell string) Option {
	return func(o *Cmd) { o.Shell = shell }
}

// WithArgs sets arguments for ExecMode.
func WithArgs(args ...string) Option {
	return func(o *Cmd) { o.Args = args }
}

// WithSetDir sets the working directory.
func WithSetDir(dir string) Option {
	return func(o *Cmd) { o.Dir = dir }
}

// WithSetEnv sets the environment variables.
func WithSetEnv(env []string) Option {
	return func(o *Cmd) { o.Env = env }
}

// WithStdout sets an extra stdout writer.
func WithStdout(w io.Writer) Option {
	return func(o *Cmd) { o.Stdout = w }
}

// WithStderr sets an extra stderr writer.
func WithStderr(w io.Writer) Option {
	return func(o *Cmd) { o.Stderr = w }
}

// WithContext sets the parent context for cancellation/timeout.
func WithContext(ctx context.Context) Option {
	return func(o *Cmd) { o.parentCtx = ctx }
}

// NewCommand creates a new Cmd. By default ShellMode is true and the shell
// is auto-detected ("cmd" on Windows, "bash" elsewhere).
func NewCommand(bash string, options ...Option) *Cmd {
	defaultShell := "bash"
	if runtime.GOOS == "windows" {
		defaultShell = "cmd"
	}
	c := &Cmd{
		Bash:       bash,
		Shell:      defaultShell,
		ShellMode:  true,
		statusChan: make(chan Status, 1),
		doneChan:   make(chan error, 1),
	}
	for _, opt := range options {
		opt(c)
	}
	return c
}

// Clone returns a new Cmd with the same configuration (Bash, Shell, ShellMode,
// Args, Env, Dir, timeout) but fresh state. Unlike the original, it preserves
// all user-set options instead of resetting to defaults.
func (c *Cmd) Clone() *Cmd {
	opts := []Option{
		WithShell(c.Shell),
	}
	if !c.ShellMode {
		opts = append(opts, WithExecMode(true))
	} else {
		opts = append(opts, WithShellMode())
	}
	if len(c.Args) > 0 {
		opts = append(opts, WithArgs(c.Args...))
	}
	if c.Dir != "" {
		opts = append(opts, WithSetDir(c.Dir))
	}
	if len(c.Env) > 0 {
		opts = append(opts, WithSetEnv(c.Env))
	}
	if c.timeout > 0 {
		opts = append(opts, WithTimeout(c.timeout))
	}
	if c.parentCtx != nil {
		opts = append(opts, WithContext(c.parentCtx))
	}
	if c.Stdout != nil {
		opts = append(opts, WithStdout(c.Stdout))
	}
	if c.Stderr != nil {
		opts = append(opts, WithStderr(c.Stderr))
	}
	return NewCommand(c.Bash, opts...)
}

// Start begins async command execution.
func (c *Cmd) Start() error {
	if c.Status.Finish {
		return ErrAlreadyFinished
	}
	if err := c.run(); err != nil {
		c.mu.Lock()
		c.Status.Finish = true
		c.Status.Error = err
		c.mu.Unlock()
		close(c.doneChan)
		close(c.statusChan)
		return err
	}
	return nil
}

// Wait blocks until the command finishes and returns the final error.
func (c *Cmd) Wait() error {
	<-c.doneChan
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Status.Error
}

// Run is shorthand for Start + Wait.
func (c *Cmd) Run() error {
	if err := c.Start(); err != nil {
		return err
	}
	return c.Wait()
}

func (c *Cmd) buildCtx() {
	parent := c.parentCtx
	if parent == nil {
		parent = context.Background()
	}
	if c.timeout > 0 {
		c.ctx, c.cancel = context.WithTimeout(parent, time.Duration(c.timeout)*time.Second)
	} else {
		c.ctx, c.cancel = context.WithCancel(parent)
	}
}

func (c *Cmd) run() error {
	c.buildCtx()
	c.mu.Lock()
	c.Status.startTime = time.Now()
	c.mu.Unlock()

	var cmd *exec.Cmd
	if c.ShellMode {
		switch c.Shell {
		case "cmd":
			cmd = exec.Command("cmd", "/C", c.Bash)
		case "powershell", "pwsh":
			cmd = exec.Command(c.Shell, "-Command", c.Bash)
		default:
			cmd = exec.Command(c.Shell, "-c", c.Bash)
		}
	} else {
		if len(c.Args) > 0 {
			cmd = exec.Command(c.Bash, c.Args...)
		} else {
			parts := strings.Split(c.Bash, " ")
			if len(parts) > 1 {
				cmd = exec.Command(parts[0], parts[1:]...)
			} else {
				cmd = exec.Command(parts[0])
			}
		}
	}

	cmd.Dir = c.Dir
	cmd.Env = c.Env
	procmgmt.SetPgid(cmd)

	// Merge writers: internal buffers + optional user writers.
	var writersOut []io.Writer
	writersOut = append(writersOut, &c.output, &c.stdout)
	if c.Stdout != nil {
		writersOut = append(writersOut, c.Stdout)
	}
	cmd.Stdout = io.MultiWriter(writersOut...)

	var writersErr []io.Writer
	writersErr = append(writersErr, &c.output, &c.stderr)
	if c.Stderr != nil {
		writersErr = append(writersErr, c.Stderr)
	}
	cmd.Stderr = io.MultiWriter(writersErr...)

	c.stdcmd = cmd
	if err := c.stdcmd.Start(); err != nil {
		c.mu.Lock()
		c.Status.Error = err
		c.mu.Unlock()
		return err
	}

	c.monitorContext()
	go c.handleWait()
	return nil
}

// handleWait waits for the process to exit, then finalizes status.
// This is the sole owner of finalize() — monitorContext only calls Stop()
// to kill the process, which causes stdcmd.Wait() to return here.
func (c *Cmd) handleWait() {
	err := c.stdcmd.Wait()

	c.mu.Lock()
	if c.ctx.Err() == context.Canceled {
		c.Status.Error = ErrProcessCancel
	} else if c.ctx.Err() == context.DeadlineExceeded {
		c.Status.Error = ErrProcessTimeout
	} else if err != nil {
		c.Status.Error = formatExitCode(err)
	}
	c.Status.Stdout = c.stdout.String()
	c.Status.Stderr = c.stderr.String()
	c.Status.Output = c.output.String()
	c.mu.Unlock()

	c.finalize()
}

// monitorContext watches for context cancellation/timeout and kills
// the process if the context fires before the command completes.
func (c *Cmd) monitorContext() {
	if c.ctx == nil {
		return
	}
	go func() {
		select {
		case <-c.doneChan:
			return
		case <-c.ctx.Done():
			select {
			case <-c.doneChan:
				return
			default:
			}
			c.Stop()
		}
	}()
}

func (c *Cmd) finalize() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isFinalized {
		return
	}

	c.Status.CostTime = time.Since(c.Status.startTime)
	c.Status.Finish = true
	c.Status.endTime = time.Now()
	if c.stdcmd.Process != nil {
		c.Status.PID = c.stdcmd.Process.Pid
	}
	if c.stdcmd.ProcessState != nil {
		c.Status.ExitCode = c.stdcmd.ProcessState.ExitCode()
	} else {
		c.Status.ExitCode = -1
	}

	close(c.doneChan)
	close(c.statusChan)
	c.isFinalized = true
}

// Stop kills the process and its entire process group.
// Safe to call multiple times; no-op if the process already exited.
func (c *Cmd) Stop() {
	if c.stdcmd == nil || c.stdcmd.Process == nil {
		return
	}
	c.cancel()
	_ = procmgmt.KillGroup(c.stdcmd)
}

// killSignal sends a custom signal to the process.
// On Windows this is a no-op (Windows lacks POSIX signals).
// The POSIX implementation is in shell_kill_posix.go.

// Cost returns the elapsed time of the last execution.
func (c *Cmd) Cost() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Status.CostTime
}

// StatusChan returns the channel that receives the final Status once.
// Useful for select-based waiting alongside other channels.
func (c *Cmd) StatusChan() <-chan Status {
	return c.statusChan
}

// formatExitCode maps common exit codes to semantic errors.
func formatExitCode(err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		switch exitErr.ExitCode() {
		case 127:
			return ErrNotFoundCommand
		case 126:
			return ErrNotExecutePermission
		case 128:
			return ErrInvalidArgs
		}
	}
	return err
}

// CheckCmdExists reports whether a command is available on PATH.
func CheckCmdExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

// CheckPnameRunning reports whether a process name is currently running.
func CheckPnameRunning(pname string) bool {
	if runtime.GOOS == "windows" {
		out, _, _ := CommandFormat("tasklist | findstr %s", pname)
		return strings.Contains(out, pname)
	}
	out, _, _ := CommandFormat("ps aux | grep %s | grep -v grep", pname)
	return strings.Contains(out, pname)
}

// -----------------------------------------------------------------------
// Convenience functions
// -----------------------------------------------------------------------

// Command runs a shell command and returns combined output, exit code, and error.
func Command(args string) (string, int, error) {
	cmd := NewCommand(args)
	err := cmd.Run()
	return cmd.Status.Output, cmd.Status.ExitCode, err
}

// CommandContext is like Command but with a parent context.
func CommandContext(ctx context.Context, args string) (string, int, error) {
	cmd := NewCommand(args, WithContext(ctx))
	err := cmd.Run()
	return cmd.Status.Output, cmd.Status.ExitCode, err
}

// CommandFormat runs a printf-formatted shell command.
func CommandFormat(format string, vals ...interface{}) (string, int, error) {
	return Command(fmt.Sprintf(format, vals...))
}

// CommandContains runs a command and reports whether the output contains all subs.
func CommandContains(args string, subs ...string) bool {
	out, _, err := Command(args)
	if err != nil {
		return false
	}
	for _, sub := range subs {
		if !strings.Contains(out, sub) {
			return false
		}
	}
	return true
}

// CommandScript writes a script to a temp file and executes it with bash.
func CommandScript(script []byte) (string, int, error) {
	fpath := filepath.Join(os.TempDir(), fmt.Sprintf("go-shell-%s.sh", randString(16)))
	defer os.RemoveAll(fpath)

	if err := os.WriteFile(fpath, script, 0666); err != nil {
		return "", DefaultExitCode, fmt.Errorf("dump script to file failed: %w", err)
	}
	return CommandFormat("bash %s", fpath)
}

// CommandWithMultiOut runs a command and returns stdout, stderr, exit code, and error.
func CommandWithMultiOut(cmdStr string) (string, string, int, error) {
	cmd := NewCommand(cmdStr)
	err := cmd.Run()
	return cmd.Status.Stdout, cmd.Status.Stderr, cmd.Status.ExitCode, err
}

// CommandWithMultiOutContext is like CommandWithMultiOut but with a parent context.
func CommandWithMultiOutContext(ctx context.Context, cmdStr string) (string, string, int, error) {
	cmd := NewCommand(cmdStr, WithContext(ctx))
	err := cmd.Run()
	return cmd.Status.Stdout, cmd.Status.Stderr, cmd.Status.ExitCode, err
}

// CommandWithChan runs a command and streams output lines to the given channel.
// The channel is closed when the command finishes. The caller must drain it.
func CommandWithChan(cmdStr string, queue chan string) error {
	streamer := NewOutputStream(queue, true)
	cmd := NewCommand(cmdStr)
	cmd.Stdout = streamer
	cmd.Stderr = streamer
	err := cmd.Run()
	close(queue)
	return err
}
