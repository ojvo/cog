//go:build windows

package shell

import "syscall"

// Kill is a no-op on Windows (Windows lacks POSIX signals).
func (c *Cmd) Kill(_ syscall.Signal) {}
