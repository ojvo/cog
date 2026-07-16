package diag_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"c.n/ojv/cog/diag"
)

func TestGoroutineLeaks_None(t *testing.T) {
	leaks := diag.GoroutineLeaks(func() {
		// no goroutines spawned
	})
	if leaks != 0 {
		t.Errorf("expected 0 leaks, got %d", leaks)
	}
}

func TestGoroutineLeaks_WithLeak(t *testing.T) {
	leaks := diag.GoroutineLeaks(func() {
		go func() {
			timer := time.NewTimer(300 * time.Millisecond)
			defer timer.Stop()
			<-timer.C
		}()
	})
	if leaks <= 0 {
		t.Errorf("expected positive leak count, got %d", leaks)
	}
	// let leaked goroutine exit before next test
	time.Sleep(400 * time.Millisecond)
}

func TestGoroutineMark(t *testing.T) {
	m := diag.MarkGoroutines()
	done := make(chan struct{})
	go func() {
		time.Sleep(30 * time.Millisecond)
		close(done)
	}()
	<-done
	if got := m.Release(); got != 0 {
		t.Errorf("expected 0 delta after goroutine exit, got %d", got)
	}
}

func TestMemoryLeaks_NoAllocation(t *testing.T) {
	// f does nothing; result may be slightly negative due to deferred frees
	// from prior tests being collected. We only verify the function runs and
	// log the value rather than asserting a strict zero.
	n := diag.MemoryLeaks(func() {})
	t.Logf("memory leak count (no alloc): %d", n)
}

func TestMemoryLeaks_WithLeak(t *testing.T) {
	var leaked [][]byte
	n := diag.MemoryLeaks(func() {
		for i := 0; i < 50; i++ {
			leaked = append(leaked, make([]byte, 256))
		}
	})
	runtime.KeepAlive(leaked)
	if n <= 0 {
		t.Errorf("expected positive leak count, got %d", n)
	}
	t.Logf("memory leak count (with leak): %d", n)
}

func TestMemStat(t *testing.T) {
	s := diag.MemStat()
	if s == nil {
		t.Fatal("MemStat returned nil")
	}
	if s.Byte == 0 {
		t.Error("expected non-zero heap allocation")
	}
	if s.Frag < 0 || s.Frag > 1 {
		t.Errorf("frag out of [0,1]: %v", s.Frag)
	}
	if s.Idle < 0 || s.Idle > 1 {
		t.Errorf("idle out of [0,1]: %v", s.Idle)
	}
	t.Logf("MemStat: %+v", s)
}

func TestForceGC(t *testing.T) {
	diag.ForceGC(0)
	diag.ForceGC(1)
}

func TestMemoryMark_ForceGC(t *testing.T) {
	m := diag.MarkMemory()
	info := m.ForceGC(1)
	if info.Frag < 0 || info.Frag > 1 {
		t.Errorf("frag out of [0,1]: %v", info.Frag)
	}
	if info.Idle < 0 || info.Idle > 1 {
		t.Errorf("idle out of [0,1]: %v", info.Idle)
	}
	t.Logf("MemoryMark.ForceGC: %+v", info)
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{0, "0.00 B"},
		{512, "512.00 B"},
		{1024, "1.00 Kb"},
		{1024 * 1024, "1.00 Mb"},
		{1024 * 1024 * 1024, "1.00 Gb"},
		{3 * 1024 * 1024, "3.00 Mb"},
	}
	for _, c := range cases {
		got := diag.FormatBytes(c.in)
		if got != c.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPrintAlloc(t *testing.T) {
	// just ensure no panic
	diag.PrintAlloc("test")
}

func TestPprof_HeapCapture(t *testing.T) {
	path, err := diag.HeapProfile{}.Capture()
	if err != nil {
		t.Fatalf("HeapProfile.Capture: %v", err)
	}
	defer os.Remove(path)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("profile file not created: %v", err)
	}
}

func TestPprof_GoroutineCapture(t *testing.T) {
	path, err := diag.GoroutineProfile{}.Capture()
	if err != nil {
		t.Fatalf("GoroutineProfile.Capture: %v", err)
	}
	defer os.Remove(path)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("profile file not created: %v", err)
	}
}

func TestPprof_ThreadcreateCapture(t *testing.T) {
	path, err := diag.ThreadcreateProfile{}.Capture()
	if err != nil {
		t.Fatalf("ThreadcreateProfile.Capture: %v", err)
	}
	defer os.Remove(path)
}

func TestPprof_BlockCapture(t *testing.T) {
	// BlockProfile requires SetBlockProfileRate; without events the profile
	// is empty but Capture should still succeed and produce a file.
	path, err := diag.BlockProfile{Rate: 1}.Capture()
	if err != nil {
		t.Fatalf("BlockProfile.Capture: %v", err)
	}
	defer os.Remove(path)
}

func TestPprof_Helper_Heap(t *testing.T) {
	dir := t.TempDir()
	orig := diag.PprofDir
	diag.PprofDir = dir
	defer func() { diag.PprofDir = orig }()

	if cmd := diag.Pprof("test", "heap"); cmd != "heap" {
		t.Errorf("expected 'heap', got %q", cmd)
	}
	want := filepath.Join(dir, "test.heap.prof")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected heap profile file %s: %v", want, err)
	}
}

func TestPprof_Helper_CPUStartStop(t *testing.T) {
	dir := t.TempDir()
	orig := diag.PprofDir
	diag.PprofDir = dir
	defer func() { diag.PprofDir = orig }()

	if cmd := diag.Pprof("cpu", "cpu start"); cmd != "cpu start" {
		t.Fatalf("cpu start returned %q", cmd)
	}
	time.Sleep(20 * time.Millisecond)
	diag.Pprof("cpu", "cpu stop")

	want := filepath.Join(dir, "cpu.cpu.prof")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected cpu profile file %s: %v", want, err)
	}
}

func TestPprof_Helper_CPUStopWithoutStart(t *testing.T) {
	dir := t.TempDir()
	orig := diag.PprofDir
	diag.PprofDir = dir
	defer func() { diag.PprofDir = orig }()

	// stop without start should be a safe no-op
	if cmd := diag.Pprof("cpu", "cpu stop"); cmd != "cpu stop" {
		t.Errorf("expected 'cpu stop', got %q", cmd)
	}
}

func TestPprof_Helper_UnknownCmd(t *testing.T) {
	dir := t.TempDir()
	orig := diag.PprofDir
	diag.PprofDir = dir
	defer func() { diag.PprofDir = orig }()

	if cmd := diag.Pprof("x", "unknown"); cmd != "unknown" {
		t.Errorf("expected 'unknown', got %q", cmd)
	}
}

func TestAutoCPUProfile_InvalidArgs(t *testing.T) {
	// invalid args should be no-op
	diag.AutoCPUProfile(100, 1000, 0)     // total too small
	diag.AutoCPUProfile(120000, 5, 0)     // duration too small
	diag.AutoCPUProfile(120000, 1000, -1) // interval negative
}

func TestTimer(t *testing.T) {
	var sb strings.Builder
	timer := diag.NewTimer(&sb)
	time.Sleep(15 * time.Millisecond)
	timer.Stop()
	out := sb.String()
	if !strings.Contains(out, "diag]") {
		t.Errorf("expected output to contain 'diag]', got %q", out)
	}

	// Stop should be idempotent
	timer.Stop()
	if sb.String() != out {
		t.Error("second Stop should be a no-op")
	}
}

func TestTimer_DefaultWriter(t *testing.T) {
	// no writer arg: should default to os.Stdout without panicking
	timer := diag.NewTimer()
	timer.Stop()
}

func TestTimer_Reset(t *testing.T) {
	var sb strings.Builder
	timer := diag.NewTimer(&sb)
	time.Sleep(10 * time.Millisecond)
	d1 := timer.Reset()
	if d1 <= 0 {
		t.Errorf("expected positive duration, got %v", d1)
	}
	time.Sleep(10 * time.Millisecond)
	timer.Stop()
	if !strings.Contains(sb.String(), "diag]") {
		t.Errorf("expected output after Stop, got %q", sb.String())
	}
}

func TestTimer_NilSafe(t *testing.T) {
	var t1 *diag.Timer
	t1.Stop() // must not panic
}

func TestTimeFunc(t *testing.T) {
	var sb strings.Builder
	stop := diag.TimeFunc(&sb)
	time.Sleep(5 * time.Millisecond)
	stop()
	if !strings.Contains(sb.String(), "diag]") {
		t.Errorf("expected output, got %q", sb.String())
	}
}
