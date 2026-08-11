package env

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// patchEnv sets the environment variable to value (unset if value == "")
// and registers a cleanup to restore the original value.
func patchEnv(t *testing.T, key, value string) {
	t.Helper()
	old, hadOld := os.LookupEnv(key)
	if value == "" {
		_ = os.Unsetenv(key)
	} else {
		_ = os.Setenv(key, value)
	}
	t.Cleanup(func() {
		if hadOld {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func TestExpandHome_EmptyPath(t *testing.T) {
	got, err := ExpandHome("")
	if err != nil {
		t.Fatalf("ExpandHome(\"\") err = %v, want nil", err)
	}
	if got != "" {
		t.Errorf("ExpandHome(\"\") = %q, want %q", got, "")
	}
}

func TestExpandHome_NoTildePrefix(t *testing.T) {
	cases := []string{
		"/abs/path",
		"relative/path",
		".",
		"..",
		"foo.txt",
		"C:\\windows\\path", // windows-style, no leading ~
	}
	for _, in := range cases {
		got, err := ExpandHome(in)
		if err != nil {
			t.Fatalf("ExpandHome(%q) err = %v, want nil", in, err)
		}
		if got != in {
			t.Errorf("ExpandHome(%q) = %q, want %q (unchanged)", in, got, in)
		}
	}
}

func TestExpandHome_TildeAlone(t *testing.T) {
	home, err := UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir err = %v", err)
	}
	got, err := ExpandHome("~")
	if err != nil {
		t.Fatalf("ExpandHome(\"~\") err = %v", err)
	}
	if got != home {
		t.Errorf("ExpandHome(\"~\") = %q, want %q", got, home)
	}
}

func TestExpandHome_TildeSlashPath(t *testing.T) {
	home, err := UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir err = %v", err)
	}
	got, err := ExpandHome("~/foo/bar")
	if err != nil {
		t.Fatalf("ExpandHome(\"~/foo/bar\") err = %v", err)
	}
	want := filepath.Join(home, "foo", "bar")
	if got != want {
		t.Errorf("ExpandHome(\"~/foo/bar\") = %q, want %q", got, want)
	}
}

func TestExpandHome_TildeBackslashPath(t *testing.T) {
	// "~\foo" should behave like "~/foo" — both are path separators.
	home, err := UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir err = %v", err)
	}
	got, err := ExpandHome("~\\foo")
	if err != nil {
		t.Fatalf("ExpandHome(\"~\\\\foo\") err = %v", err)
	}
	want := filepath.Join(home, "foo")
	if got != want {
		t.Errorf("ExpandHome(\"~\\\\foo\") = %q, want %q", got, want)
	}
}

func TestExpandHome_TildeUserRejected(t *testing.T) {
	cases := []string{
		"~user/foo",
		"~user",
		"~abc",
	}
	for _, in := range cases {
		_, err := ExpandHome(in)
		if !errors.Is(err, ErrExpandHomeInvalidTilde) {
			t.Errorf("ExpandHome(%q) err = %v, want ErrExpandHomeInvalidTilde", in, err)
		}
	}
}

// TestExpandHome_NoCache verifies that ExpandHome reflects the current
// environment at call time. This documents the design decision to skip the
// legacy go-homedir cache: a process that mutates HOME after startup should
// observe the new value.
func TestExpandHome_NoCache(t *testing.T) {
	// Skip on Windows: USERPROFILE / HOMEDRIVE+HOMEPATH semantics differ
	// and overwriting them mid-test can leave the process in a weird state.
	if runtime.GOOS == "windows" {
		t.Skip("skip env mutation test on windows")
	}

	tmp := t.TempDir()
	patchEnv(t, "HOME", tmp)

	got, err := ExpandHome("~/data")
	if err != nil {
		t.Fatalf("ExpandHome err = %v", err)
	}
	want := filepath.Join(tmp, "data")
	if got != want {
		t.Errorf("ExpandHome after HOME set = %q, want %q", got, want)
	}

	// Mutate HOME again and confirm a subsequent call observes the change.
	tmp2 := t.TempDir()
	patchEnv(t, "HOME", tmp2)

	got2, err := ExpandHome("~/data")
	if err != nil {
		t.Fatalf("ExpandHome second call err = %v", err)
	}
	want2 := filepath.Join(tmp2, "data")
	if got2 != want2 {
		t.Errorf("ExpandHome after HOME mutation = %q, want %q", got2, want2)
	}
}

// TestExpandHome_UserProfileOnWindows exercises the Windows-specific env
// var branch so the package is exercised on go test across platforms.
func TestExpandHome_UserProfileOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	tmp := t.TempDir()
	patchEnv(t, "USERPROFILE", tmp)

	got, err := ExpandHome("~/file")
	if err != nil {
		t.Fatalf("ExpandHome err = %v", err)
	}
	want := filepath.Join(tmp, "file")
	if got != want {
		t.Errorf("ExpandHome = %q, want %q", got, want)
	}
}
