//go:build !windows

package shell

import "syscall"

// Kill sends a custom POSIX signal to the process.
func (c *Cmd) Kill(sig syscall.Signal) {
	if c.stdcmd == nil || c.stdcmd.Process == nil {
		return
	}
	_ = syscall.Kill(c.stdcmd.Process.Pid, sig)
}
