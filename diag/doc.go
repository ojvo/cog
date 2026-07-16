// Package diag provides diagnostics utilities for Go programs:
//
//   - Goroutine leak detection (GoroutineLeaks, GoroutineMark)
//   - Memory statistics and leak detection (MemStat, MemoryLeaks, MemoryMark)
//   - pprof profile capture (Profile interface, Pprof helper, AutoCPUProfile)
//   - Execution timing (Timer, TimeFunc)
//
// The package only depends on the Go standard library.
package diag
