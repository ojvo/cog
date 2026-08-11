package env

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
)

// GetExecutablePath returns the path to the current process binary.
func GetExecutablePath() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	name := os.Args[0]
	if filepath.Base(name) == name {
		if lp, err := exec.LookPath(name); err == nil {
			return lp
		}
	}
	if abs, err := filepath.Abs(name); err == nil {
		return abs
	}
	return name
}

// GetExecutableDir returns the directory path of the current executable.
func GetExecutableDir() (string, error) {
	return filepath.Dir(GetExecutablePath()), nil
}

// WorkingDir returns the current working directory.
func WorkingDir() (string, error) {
	return os.Getwd()
}

// UserHomeDir returns the current user's home directory.
func UserHomeDir() (string, error) {
	if dir, err := os.UserHomeDir(); err == nil && dir != "" {
		return dir, nil
	}
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.HomeDir, nil
}

// ErrExpandHomeInvalidTilde is returned by ExpandHome when the path begins
// with '~' but the next byte is not a path separator. Such forms (e.g.
// "~user/foo") would require user lookup and are not supported.
var ErrExpandHomeInvalidTilde = errors.New("'~' must be followed by a path separator")

// ExpandHome expands a leading '~' to the current user's home directory.
//
// Behavior:
//   - Empty path or path not starting with '~' is returned unchanged.
//   - "~" returns the home directory itself.
//   - "~/foo" or "~\foo" returns filepath.Join(home, "foo").
//   - "~user/foo" returns ErrExpandHomeInvalidTilde (user-qualified home
//     lookup is intentionally unsupported; use os/user directly if needed).
//
// Unlike the legacy go-homedir library, this uses os.UserHomeDir() which is
// pure-Go (no cgo) and reads HOME/HOMEDRIVE+HOMEPATH/USERPROFILE env vars
// directly. No caching is applied: env reads are O(1) and a cache would
// silently hide environment changes (e.g. HOME set after process start).
func ExpandHome(path string) (string, error) {
	if len(path) == 0 || path[0] != '~' {
		return path, nil
	}
	if len(path) > 1 && path[1] != '/' && path[1] != '\\' {
		return "", ErrExpandHomeInvalidTilde
	}
	home, err := UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, path[1:]), nil
}

// TempDir returns the default temporary directory.
func TempDir() string {
	return os.TempDir()
}

// LookupEnv returns the value of the environment variable and whether it was set.
func LookupEnv(key string) (string, bool) {
	return os.LookupEnv(key)
}
