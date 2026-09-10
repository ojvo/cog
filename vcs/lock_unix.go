//go:build !windows

package vcs

import (
	"errors"
	"os"
	"syscall"
)

// processAlive reports whether pid is still running. Signal 0 performs
// existence and permission checking without actually delivering a signal.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	// EPERM means the process exists but is owned by another user: it is
	// alive, so we must not reclaim its lock.
	return errors.Is(err, syscall.EPERM)
}
