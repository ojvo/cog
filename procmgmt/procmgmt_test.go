//go:build !windows

package procmgmt

import (
	"os/exec"
	"syscall"
	"testing"
)

// TestSetPgid_PreservesExistingSysProcAttr verifies that SetPgid merges
// into an existing SysProcAttr rather than overwriting it. Before the fix,
// direct assignment (cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true})
// silently dropped other fields the caller may have set (Foreground, Setsid,
// Setctty, credentials, etc.). After the fix, only Setpgid is set.
func TestSetPgid_PreservesExistingSysProcAttr(t *testing.T) {
	cmd := exec.Command("echo", "test")
	// Simulate a caller that sets Foreground (a POSIX-specific field).
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Foreground: true,
	}

	SetPgid(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr is nil after SetPgid")
	}
	if !cmd.SysProcAttr.Setpgid {
		t.Error("Setpgid not set after SetPgid")
	}
	if !cmd.SysProcAttr.Foreground {
		t.Error("caller's Foreground field was overwritten — merge broken")
	}
}

// TestSetPgid_NilSysProcAttrCreatesNew verifies the common path where
// SysProcAttr is nil — SetPgid creates one with Setpgid=true.
func TestSetPgid_NilSysProcAttrCreatesNew(t *testing.T) {
	cmd := exec.Command("echo", "test")
	cmd.SysProcAttr = nil

	SetPgid(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("expected SysProcAttr to be created, got nil")
	}
	if !cmd.SysProcAttr.Setpgid {
		t.Error("Setpgid not set on freshly created SysProcAttr")
	}
}
