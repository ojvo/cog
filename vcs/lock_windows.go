//go:build windows

package vcs

import (
	"syscall"
)

// processAlive reports whether pid is still running on Windows.
//
// OpenProcess with PROCESS_QUERY_LIMITED_INFORMATION is the cheapest reliable
// liveness probe available to the stdlib-only build. A handle that opens means
// the PID exists (or is a zombie handle we cannot see); a failure with
// ERROR_INVALID_PARAMETER means the PID is gone. We treat "cannot open" as
// "not alive" so that a lock left by a dead process is eventually reclaimed —
// this is a liveness heuristic for crash recovery, not an authorization check.
func processAlive(pid int) bool {
	const processQueryLimitedInformation = 0x1000
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = syscall.CloseHandle(h)
	return true
}
