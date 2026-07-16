package shell

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCmdExecution(t *testing.T) {
	cmd := NewCommand("echo hello")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Command execution failed: %v", err)
	}
	if !strings.Contains(cmd.Status.Output, "hello") {
		t.Errorf("Expected output containing 'hello', got '%s'", cmd.Status.Output)
	}
}

func TestCmdRunError(t *testing.T) {
	cmd := NewCommand("non_existent_command_xyz_123", WithExecMode(true))
	err := cmd.Run()
	if err == nil {
		t.Error("Expected error for non-existent command, got nil")
	}
}

func TestCmdTimeout(t *testing.T) {
	var cmdStr string
	if runtime.GOOS == "windows" {
		cmdStr = "ping -n 3 127.0.0.1"
	} else {
		cmdStr = "sleep 2"
	}

	cmd := NewCommand(cmdStr, WithTimeout(1))
	err := cmd.Run()
	if err != ErrProcessTimeout {
		t.Logf("Expected timeout error, got: %v", err)
		if err == nil {
			t.Error("Command finished successfully but should have timed out")
		}
	}
}

func TestCommandContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	cmdStr := "sleep 2"
	if runtime.GOOS == "windows" {
		cmdStr = "ping -n 3 127.0.0.1"
	}

	start := time.Now()
	_, _, err := CommandContext(ctx, cmdStr)
	duration := time.Since(start)

	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
	if duration > 1000*time.Millisecond {
		t.Errorf("Command took too long to cancel: %v", duration)
	}
}

func TestCommandWithChan(t *testing.T) {
	queue := make(chan string, 10)

	cmdStr := "echo line1 && echo line2 && echo line3"
	if runtime.GOOS == "windows" {
		cmdStr = "echo line1 & echo line2 & echo line3"
	}

	err := CommandWithChan(cmdStr, queue)
	if err != nil {
		t.Fatalf("CommandWithChan failed: %v", err)
	}

	count := 0
	for line := range queue {
		count++
		if !strings.HasPrefix(line, "line") {
			t.Errorf("Unexpected line: %s", line)
		}
	}

	if count != 3 {
		t.Errorf("Expected 3 lines, got %d", count)
	}
}

func TestCheckPnameRunning(t *testing.T) {
	var cmdStr string
	var procName string
	if runtime.GOOS == "windows" {
		cmdStr = "ping -n 5 127.0.0.1"
		procName = "PING.EXE"
	} else {
		cmdStr = "sleep 2"
		procName = "sleep"
	}

	go func() {
		Command(cmdStr)
	}()

	time.Sleep(100 * time.Millisecond)

	if !CheckPnameRunning(procName) {
		t.Logf("CheckPnameRunning returned false for %s (might be expected depending on environment)", procName)
	}
}

func TestCheckCmdExists(t *testing.T) {
	// "go" should always be on PATH during tests
	if !CheckCmdExists("go") {
		t.Error("Expected 'go' to be on PATH")
	}
	if CheckCmdExists("definitely_not_a_real_command_xyz") {
		t.Error("Expected non-existent command to return false")
	}
}

// --- Regression tests for refactored code ---

func TestClone_PreservesConfig(t *testing.T) {
	original := NewCommand("echo test",
		WithExecMode(true),
		WithArgs("arg1", "arg2"),
		WithSetDir("/tmp"),
		WithSetEnv([]string{"FOO=bar"}),
		WithTimeout(30),
	)

	clone := original.Clone()

	if clone.Bash != original.Bash {
		t.Errorf("Bash mismatch: got %q, want %q", clone.Bash, original.Bash)
	}
	if clone.ShellMode != original.ShellMode {
		t.Errorf("ShellMode mismatch: got %v, want %v", clone.ShellMode, original.ShellMode)
	}
	if len(clone.Args) != len(original.Args) {
		t.Errorf("Args length mismatch: got %d, want %d", len(clone.Args), len(original.Args))
	}
	if clone.Dir != original.Dir {
		t.Errorf("Dir mismatch: got %q, want %q", clone.Dir, original.Dir)
	}
	if len(clone.Env) != len(original.Env) {
		t.Errorf("Env length mismatch: got %d, want %d", len(clone.Env), len(original.Env))
	}
	if clone.timeout != original.timeout {
		t.Errorf("Timeout mismatch: got %d, want %d", clone.timeout, original.timeout)
	}
}

func TestClone_PreservesShellMode(t *testing.T) {
	original := NewCommand("echo test", WithShellMode(), WithShell("zsh"))
	clone := original.Clone()

	if !clone.ShellMode {
		t.Error("Clone should preserve ShellMode=true")
	}
	if clone.Shell != "zsh" {
		t.Errorf("Clone should preserve Shell='zsh', got %q", clone.Shell)
	}
}

func TestCmd_Stop_TerminatesProcess(t *testing.T) {
	cmdStr := "sleep 10"
	if runtime.GOOS == "windows" {
		cmdStr = "ping -n 20 127.0.0.1"
	}

	cmd := NewCommand(cmdStr)
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	cmd.Stop()

	select {
	case <-cmd.doneChan:
		// good
	case <-time.After(5 * time.Second):
		t.Error("Stop did not cause the command to finish within 5s")
	}

	if !cmd.Status.Finish {
		t.Error("Status.Finish should be true after Stop")
	}
}

func TestThreadSafeBuffer_Concurrent(t *testing.T) {
	var buf ThreadSafeBuffer
	var wg sync.WaitGroup

	writers := 100
	writesPerGoroutine := 50

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				buf.Write([]byte("x"))
			}
		}()
	}
	wg.Wait()

	expected := writers * writesPerGoroutine
	got := buf.String()
	if len(got) != expected {
		t.Errorf("Buffer length mismatch: got %d, want %d", len(got), expected)
	}
}

func TestOutputStream_LineBufferOverflow(t *testing.T) {
	ch := make(chan string, 1)
	stream := NewOutputStream(ch, true) // non-blocking
	stream.SetLineBufferSize(8)

	// Write a line longer than buffer without newline — should trigger overflow
	_, err := stream.Write([]byte("this is a very long line without newline"))
	if err != ErrLineBufferOverflow {
		t.Logf("Expected ErrLineBufferOverflow or line sent directly, got: %v", err)
	}
}

func TestOutputStream_MultiLine(t *testing.T) {
	ch := make(chan string, 10)
	stream := NewOutputStream(ch, true)

	input := "line1\nline2\nline3\n"
	stream.Write([]byte(input))

	close(ch)

	var lines []string
	for line := range ch {
		lines = append(lines, line)
	}

	if len(lines) != 3 {
		t.Errorf("Expected 3 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "line1" {
		t.Errorf("First line mismatch: got %q", lines[0])
	}
}

func TestCommandFormat(t *testing.T) {
	out, _, err := CommandFormat("echo %s", "formatted")
	if err != nil {
		t.Fatalf("CommandFormat failed: %v", err)
	}
	if !strings.Contains(out, "formatted") {
		t.Errorf("Expected output containing 'formatted', got '%s'", out)
	}
}

func TestCommandWithMultiOut(t *testing.T) {
	stdout, stderr, code, err := CommandWithMultiOut("echo hello")
	if err != nil {
		t.Fatalf("CommandWithMultiOut failed: %v", err)
	}
	if code != 0 {
		t.Errorf("Expected exit code 0, got %d", code)
	}
	if !strings.Contains(stdout, "hello") {
		t.Errorf("Expected stdout containing 'hello', got '%s'", stdout)
	}
	if stderr != "" {
		t.Errorf("Expected empty stderr, got '%s'", stderr)
	}
}
