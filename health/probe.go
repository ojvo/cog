package health

import (
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"ojv/cog/httputil"
	"ojv/cog/log"
)

// Checker is the contract for a readiness probe.
type Checker interface {
	Check() bool
	Name() string
}

// LivenessHandler answers GET /live with a fixed "alive" status.
type LivenessHandler struct{}

func (h *LivenessHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	httputil.OK(w, map[string]string{"status": "alive"})
}

// ReadinessHandler answers GET /ready. It is ready by default; callers
// register Checkers to gate readiness on subsystem health, and may
// flip the manual SetReady switch to drain traffic.
type ReadinessHandler struct {
	mu       sync.RWMutex
	checkers []Checker
	ready    bool
}

func NewReadinessHandler() *ReadinessHandler {
	return &ReadinessHandler{
		checkers: make([]Checker, 0),
		ready:    true,
	}
}

func (h *ReadinessHandler) AddChecker(checker Checker) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checkers = append(h.checkers, checker)
}

func (h *ReadinessHandler) SetReady(ready bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ready = ready
}

func (h *ReadinessHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()

	if !h.ready {
		httputil.Error(w, http.StatusServiceUnavailable, "service not ready")
		return
	}

	for _, checker := range h.checkers {
		if !checker.Check() {
			log.Warnf("Health: readiness check failed: %s", checker.Name())
			httputil.Error(w, http.StatusServiceUnavailable, checker.Name()+" not ready")
			return
		}
	}

	httputil.OK(w, map[string]string{"status": "ready"})
}

// StorageChecker verifies that the directory containing FilePath (or
// FilePath itself when it is a directory) is writable by creating and
// removing a .health probe file.
type StorageChecker struct {
	FilePath string
}

func NewStorageChecker(filePath string) *StorageChecker {
	return &StorageChecker{FilePath: filePath}
}

func (c *StorageChecker) Check() bool {
	info, statErr := os.Stat(c.FilePath)
	if statErr != nil && !os.IsNotExist(statErr) {
		return false
	}

	// Determine the directory to check writability.
	// If the path is a directory, use it directly; otherwise use its parent.
	dir := c.FilePath
	if statErr == nil && !info.IsDir() {
		dir = filepath.Dir(c.FilePath)
	}

	tmpFile := filepath.Join(dir, ".health")
	f, err := os.OpenFile(tmpFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(tmpFile)
	return true
}

func (c *StorageChecker) Name() string {
	return "storage"
}

// FuncChecker adapts a named func() bool into a Checker.
type FuncChecker struct {
	NameStr string
	CheckFn func() bool
}

func NewFuncChecker(name string, fn func() bool) *FuncChecker {
	return &FuncChecker{NameStr: name, CheckFn: fn}
}

func (c *FuncChecker) Check() bool {
	if c.CheckFn == nil {
		return true
	}
	return c.CheckFn()
}

func (c *FuncChecker) Name() string {
	return c.NameStr
}

// SchedulerChecker verifies the scheduler is accepting work.
type SchedulerChecker struct {
	IsRunning func() bool
}

func NewSchedulerChecker(isRunning func() bool) *SchedulerChecker {
	return &SchedulerChecker{IsRunning: isRunning}
}

func (c *SchedulerChecker) Check() bool {
	if c.IsRunning == nil {
		return true
	}
	return c.IsRunning()
}

func (c *SchedulerChecker) Name() string {
	return "scheduler"
}

// QueueDepthChecker fails readiness when queue depth exceeds a threshold.
type QueueDepthChecker struct {
	CurrentDepth func() int
	MaxDepth     int
}

func NewQueueDepthChecker(currentDepth func() int, maxDepth int) *QueueDepthChecker {
	return &QueueDepthChecker{CurrentDepth: currentDepth, MaxDepth: maxDepth}
}

func (c *QueueDepthChecker) Check() bool {
	if c.CurrentDepth == nil {
		return true
	}
	return c.CurrentDepth() <= c.MaxDepth
}

func (c *QueueDepthChecker) Name() string {
	return "queue_depth"
}
