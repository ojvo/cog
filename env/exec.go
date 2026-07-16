package env

import (
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

// TempDir returns the default temporary directory.
func TempDir() string {
	return os.TempDir()
}

// LookupEnv returns the value of the environment variable and whether it was set.
func LookupEnv(key string) (string, bool) {
	return os.LookupEnv(key)
}
