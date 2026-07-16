package diag

import (
	"fmt"
	"os"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"time"
)

// PprofDir is the default directory for profile output written by Pprof and
// AutoCPUProfile. It may be overridden before invoking those functions.
var PprofDir = "prof"

// BaseMillisecond is the millisecond unit used by AutoCPUProfile thresholds.
const BaseMillisecond = 1000

var (
	cpuProfileMu   sync.Mutex
	cpuProfileFile *os.File

	autoPprofRunning atomic.Bool
)

// Pprof writes a named profile to PprofDir.
//
// pname is a name prefix for the output file.
// cmd selects the profile type:
//   - "heap", "threadcreate", "block", "goroutine", "allocs", "mutex":
//     snapshot the matching runtime profile.
//   - "cpu start": begin CPU profiling (no-op if already running).
//   - "cpu stop": stop an in-progress CPU profiling session.
//
// Returns cmd unchanged. Errors are silently ignored to keep the helper
// safe for ad-hoc instrumentation; inspect PprofDir contents to verify output.
func Pprof(pname, cmd string) string {
	os.MkdirAll(PprofDir, 0o755)
	filename := fmt.Sprintf("%s/%s.%s.prof", PprofDir, pname, cmd)
	switch cmd {
	case "heap", "threadcreate", "block", "goroutine", "allocs", "mutex":
		p := pprof.Lookup(cmd)
		if p == nil {
			return cmd
		}
		f, err := os.Create(filename)
		if err != nil {
			return cmd
		}
		_ = p.WriteTo(f, 2)
		_ = f.Close()
	case "cpu start":
		cpuProfileMu.Lock()
		defer cpuProfileMu.Unlock()
		if cpuProfileFile != nil {
			return cmd
		}
		filename = fmt.Sprintf("%s/%s.cpu.prof", PprofDir, pname)
		f, err := os.Create(filename)
		if err != nil {
			return cmd
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			_ = f.Close()
			return cmd
		}
		cpuProfileFile = f
	case "cpu stop":
		cpuProfileMu.Lock()
		defer cpuProfileMu.Unlock()
		if cpuProfileFile == nil {
			return cmd
		}
		pprof.StopCPUProfile()
		_ = cpuProfileFile.Close()
		cpuProfileFile = nil
	}
	return cmd
}

// AutoCPUProfile samples CPU profiles repeatedly for the given durations and
// writes each sample as a numbered file under PprofDir.
//
//   - total:    total sampling window in milliseconds (>= 60s required).
//   - duration: per-sample duration in milliseconds (>= 10ms required).
//   - interval: pause between samples in milliseconds (>= 0).
//
// Only one AutoCPUProfile run is active at a time; subsequent calls while a
// run is in progress are no-ops. The function returns immediately and runs
// the sampling loop in a background goroutine.
func AutoCPUProfile(total, duration, interval int64) {
	if total < 60*BaseMillisecond || total < duration || duration < 10 || interval < 0 {
		return
	}
	if !autoPprofRunning.CompareAndSwap(false, true) {
		return
	}
	os.MkdirAll(PprofDir, 0o755)
	startTime := time.Now().UnixMilli()
	go func() {
		defer autoPprofRunning.Store(false)
		times := 0
		for {
			if time.Now().UnixMilli()-startTime > total {
				break
			}
			times++
			fn := fmt.Sprintf("%s/%.9d.cpu.prof", PprofDir, times)
			f, err := os.Create(fn)
			if err != nil {
				break
			}
			if err := pprof.StartCPUProfile(f); err != nil {
				_ = f.Close()
				break
			}
			time.Sleep(time.Duration(duration) * time.Millisecond)
			pprof.StopCPUProfile()
			if err := f.Close(); err != nil {
				break
			}
			if interval > 0 {
				time.Sleep(time.Duration(interval) * time.Millisecond)
			}
		}
	}()
}
