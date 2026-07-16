//go:build windows

package procmgmt

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
)

// SetPgid is a no-op on Windows (no process group semantics).
func SetPgid(cmd *exec.Cmd) {}

// KillGroup uses taskkill /T to kill the process and its child tree.
// Idempotent when the process has already exited.
func KillGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	if err := exec.Command("taskkill", "/T", "/F", "/PID", pid).Run(); err != nil {
		if kerr := cmd.Process.Kill(); kerr != nil && !errors.Is(kerr, os.ErrProcessDone) {
			return kerr
		}
	}
	return nil
}
