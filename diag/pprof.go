package diag

import (
	"os"
	"runtime"
	"runtime/pprof"
	"time"
)

// Profile represents a pprof profile that can be captured to a file.
type Profile interface {
	// Capture writes the profile to a temporary file and returns its path.
	Capture() (path string, err error)
}

// CPUProfile captures a CPU profile over a duration (default 30s).
type CPUProfile struct {
	Duration time.Duration
}

// Capture starts CPU profiling, waits for Duration, then stops and writes
// the result to a temporary file.
func (p CPUProfile) Capture() (string, error) {
	dur := p.Duration
	if dur == 0 {
		dur = 30 * time.Second
	}
	f, err := newTemp()
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := pprof.StartCPUProfile(f); err != nil {
		return "", err
	}
	time.Sleep(dur)
	pprof.StopCPUProfile()
	return f.Name(), nil
}

// HeapProfile captures the heap profile.
type HeapProfile struct{}

func (HeapProfile) Capture() (string, error) { return captureProfile("heap") }

// MutexProfile captures stack traces of holders of contended mutexes.
// Enable runtime.SetMutexProfileFraction at program start to populate it.
type MutexProfile struct{}

func (MutexProfile) Capture() (string, error) { return captureProfile("mutex") }

// BlockProfile captures stack traces that led to blocking on synchronization
// primitives. If Rate > 0, it is set as the blocking sample rate (in nanoseconds
// per event) before capture.
type BlockProfile struct {
	Rate int
}

func (p BlockProfile) Capture() (string, error) {
	if p.Rate > 0 {
		runtime.SetBlockProfileRate(p.Rate)
	}
	return captureProfile("block")
}

// GoroutineProfile captures stack traces of all current goroutines.
type GoroutineProfile struct{}

func (GoroutineProfile) Capture() (string, error) { return captureProfile("goroutine") }

// ThreadcreateProfile captures stack traces that led to the creation of new OS threads.
type ThreadcreateProfile struct{}

func (ThreadcreateProfile) Capture() (string, error) { return captureProfile("threadcreate") }

// captureProfile looks up a profile by name and writes it to a temporary file.
// Returns an error if the profile does not exist or writing fails.
func captureProfile(name string) (string, error) {
	p := pprof.Lookup(name)
	if p == nil {
		return "", os.ErrNotExist
	}
	f, err := newTemp()
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := p.WriteTo(f, 2); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// newTemp creates a temporary file for profile output.
func newTemp() (*os.File, error) {
	return os.CreateTemp("", "profile-")
}
