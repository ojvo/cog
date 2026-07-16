//go:build !windows

package procmgmt

import (
	"os/exec"
	"syscall"
)

// SetPgid puts the child process in its own process group so KillGroup
// can terminate the entire process tree (daemons, watch processes, etc.).
//
// Merge rather than overwrite: if the caller has already set SysProcAttr
// (e.g. Foreground, Setsid, Setctty, credentials), preserve those fields
// and only set Setpgid. Direct assignment would silently drop the caller's
// settings — a fragile implicit contract that breaks contrib backends
// (JobObject, AppContainer, sandbox-exec, etc.).
func SetPgid(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// KillGroup kills the child process's entire process group.
// Idempotent when the group is already empty (ESRCH).
func KillGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		return err
	}
	return nil
}
