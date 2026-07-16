package diag

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"time"
)

// MemInfo summarizes memory statistics.
type MemInfo struct {
	Obj  uint64  // current live heap objects (mallocs - frees)
	Byte uint64  // current heap allocated bytes (HeapAlloc)
	Frag float64 // fragmentation rate within in-use spans: (HeapInuse - HeapAlloc) / HeapInuse
	Idle float64 // fraction of idle spans not yet released to the OS: (HeapIdle - HeapReleased) / HeapIdle
}

// MemoryLeaks runs f and returns the change in live heap objects.
// A non-zero result indicates f may have leaked objects. The function forces
// GC before and after f to stabilize the measurement.
func MemoryLeaks(f func()) int {
	var then, now runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&then)
	f()
	runtime.GC()
	runtime.ReadMemStats(&now)
	return int((now.Mallocs - then.Mallocs) - (now.Frees - then.Frees))
}

// MemoryMark captures a memory baseline for later comparison via ForceGC.
// The zero value is not usable; construct via MarkMemory.
type MemoryMark struct {
	then *runtime.MemStats
	now  *runtime.MemStats
}

// MarkMemory captures a memory baseline after forcing GC and releasing OS memory.
func MarkMemory() *MemoryMark {
	m := &MemoryMark{
		then: &runtime.MemStats{},
		now:  &runtime.MemStats{},
	}
	runtime.GC()
	debug.FreeOSMemory()
	runtime.ReadMemStats(m.then)
	runtime.GC()
	return m
}

// ForceGC forces a garbage collection, optionally sleeps ms milliseconds to let
// the runtime reclaim memory, then captures the current memory stats and
// returns the delta MemInfo relative to the mark baseline.
func (m *MemoryMark) ForceGC(ms int64) MemInfo {
	debug.SetGCPercent(100)
	runtime.GC()
	debug.FreeOSMemory()
	if ms > 0 {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
	runtime.ReadMemStats(m.now)
	return MemInfo{
		Obj:  (m.now.Mallocs - m.then.Mallocs) - (m.now.Frees - m.then.Frees),
		Byte: m.now.Alloc - m.then.Alloc,
		Frag: fragRate(m.now.HeapInuse, m.now.HeapAlloc),
		Idle: idleRate(m.now.HeapIdle, m.now.HeapReleased),
	}
}

// MemStat returns the current memory statistics snapshot.
func MemStat() *MemInfo {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return &MemInfo{
		Obj:  m.Mallocs - m.Frees,
		Byte: m.HeapAlloc,
		Frag: fragRate(m.HeapInuse, m.HeapAlloc),
		Idle: idleRate(m.HeapIdle, m.HeapReleased),
	}
}

// ForceGC runs a garbage collection, optionally sleeping ms milliseconds
// afterwards to give the runtime time to release memory to the OS.
func ForceGC(ms int64) {
	runtime.GC()
	if ms > 0 {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}

// PrintAlloc writes a one-line summary of the current heap state to stdout,
// prefixed with str. Useful for ad-hoc instrumentation during development.
func PrintAlloc(str string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("heap %s/%s release %s, obj new %d - free %d - cur %d  <-- %s\n",
		FormatBytes(m.Alloc), FormatBytes(m.TotalAlloc),
		FormatBytes(m.HeapReleased), m.Mallocs, m.Frees, m.HeapObjects, str)
}

// FormatBytes renders b as a human-readable string with binary units (B, Kb, Mb, Gb).
// Values >= 1024 advance to the next unit, so 1024 yields "1.00 Kb".
func FormatBytes(b uint64) string {
	f := float64(b)
	u := []string{"B", "Kb", "Mb", "Gb"}
	idx := 0
	for f >= 1024 && idx < len(u)-1 {
		idx++
		f /= 1024
	}
	return fmt.Sprintf("%.2f %s", f, u[idx])
}

// fragRate returns (inuse - alloc) / inuse, or 0 if inuse is 0.
func fragRate(inuse, alloc uint64) float64 {
	if inuse == 0 {
		return 0
	}
	return float64(inuse-alloc) / float64(inuse)
}

// idleRate returns (idle - released) / idle, or 0 if idle is 0.
func idleRate(idle, released uint64) float64 {
	if idle == 0 {
		return 0
	}
	return float64(idle-released) / float64(idle)
}
