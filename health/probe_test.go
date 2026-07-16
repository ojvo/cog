package health

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLivenessHandler(t *testing.T) {
	handler := &LivenessHandler{}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestLivenessHandlerMethodNotAllowed(t *testing.T) {
	handler := &LivenessHandler{}

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestReadinessHandlerReady(t *testing.T) {
	handler := NewReadinessHandler()

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 when ready, got %d", w.Code)
	}
}

func TestReadinessHandlerNotReady(t *testing.T) {
	handler := NewReadinessHandler()
	handler.SetReady(false)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 when not ready, got %d", w.Code)
	}
}

func TestReadinessHandlerWithChecker(t *testing.T) {
	handler := NewReadinessHandler()
	handler.AddChecker(NewFuncChecker("test_checker", func() bool { return false }))

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 when checker fails, got %d", w.Code)
	}
}

func TestReadinessHandlerMethodNotAllowed(t *testing.T) {
	handler := NewReadinessHandler()

	req := httptest.NewRequest(http.MethodPost, "/ready", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestFuncChecker(t *testing.T) {
	checker := NewFuncChecker("custom", func() bool { return true })

	if !checker.Check() {
		t.Error("FuncChecker should return true")
	}
	if checker.Name() != "custom" {
		t.Error("FuncChecker name should be custom")
	}
}

func TestFuncCheckerNil(t *testing.T) {
	checker := NewFuncChecker("nil_fn", nil)

	if !checker.Check() {
		t.Error("FuncChecker with nil function should default to true")
	}
}

// ---------------------------------------------------------------------------
// StorageChecker
// ---------------------------------------------------------------------------

func TestStorageChecker_WritableDir(t *testing.T) {
	dir := t.TempDir()
	checker := NewStorageChecker(dir)

	if !checker.Check() {
		t.Error("writable directory should pass check")
	}
	if checker.Name() != "storage" {
		t.Errorf("expected name 'storage', got %q", checker.Name())
	}
}

func TestStorageChecker_WritableFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "test.db")
	// Create the file.
	if err := os.WriteFile(file, []byte(""), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	checker := NewStorageChecker(file)
	if !checker.Check() {
		t.Error("writable file should pass check")
	}
}

func TestStorageChecker_NonExistentPath(t *testing.T) {
	checker := NewStorageChecker("/nonexistent/path/that/does/not/exist")

	if checker.Check() {
		t.Error("non-existent path should fail check")
	}
}

func TestStorageChecker_ReadOnlyDir(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("chmod does not affect directory write permissions on Windows")
	}
	dir := t.TempDir()
	// Make directory read-only.
	if err := os.Chmod(dir, 0555); err != nil {
		t.Skip("cannot change permissions")
	}
	defer os.Chmod(dir, 0755) // Restore for cleanup.

	checker := NewStorageChecker(dir)
	if checker.Check() {
		t.Error("read-only directory should fail check")
	}
}

func TestStorageChecker_ReflectsCurrentState(t *testing.T) {
	dir := t.TempDir()
	checker := NewStorageChecker(dir)

	// First check should pass.
	if !checker.Check() {
		t.Error("first check should pass")
	}

	// Remove the directory; check should now fail since it re-evaluates each time.
	os.RemoveAll(dir)
	if checker.Check() {
		t.Error("check should fail after directory removal (no caching)")
	}
}

// ---------------------------------------------------------------------------
// SchedulerChecker
// ---------------------------------------------------------------------------

func TestSchedulerChecker_Running(t *testing.T) {
	checker := NewSchedulerChecker(func() bool { return true })

	if !checker.Check() {
		t.Error("running scheduler should pass check")
	}
	if checker.Name() != "scheduler" {
		t.Errorf("expected name 'scheduler', got %q", checker.Name())
	}
}

func TestSchedulerChecker_NotRunning(t *testing.T) {
	checker := NewSchedulerChecker(func() bool { return false })

	if checker.Check() {
		t.Error("stopped scheduler should fail check")
	}
}

func TestSchedulerChecker_NilFunc(t *testing.T) {
	checker := NewSchedulerChecker(nil)

	if !checker.Check() {
		t.Error("nil IsRunning should default to true")
	}
}

// ---------------------------------------------------------------------------
// QueueDepthChecker
// ---------------------------------------------------------------------------

func TestQueueDepthChecker_BelowMax(t *testing.T) {
	checker := NewQueueDepthChecker(func() int { return 50 }, 100)

	if !checker.Check() {
		t.Error("depth below max should pass check")
	}
	if checker.Name() != "queue_depth" {
		t.Errorf("expected name 'queue_depth', got %q", checker.Name())
	}
}

func TestQueueDepthChecker_AtMax(t *testing.T) {
	checker := NewQueueDepthChecker(func() int { return 100 }, 100)

	if !checker.Check() {
		t.Error("depth at max should pass check")
	}
}

func TestQueueDepthChecker_AboveMax(t *testing.T) {
	checker := NewQueueDepthChecker(func() int { return 101 }, 100)

	if checker.Check() {
		t.Error("depth above max should fail check")
	}
}

func TestQueueDepthChecker_NilFunc(t *testing.T) {
	checker := NewQueueDepthChecker(nil, 100)

	if !checker.Check() {
		t.Error("nil CurrentDepth should default to true")
	}
}

// ---------------------------------------------------------------------------
// ReadinessHandler with multiple checkers
// ---------------------------------------------------------------------------

func TestReadinessHandler_MultipleCheckers(t *testing.T) {
	handler := NewReadinessHandler()
	handler.AddChecker(NewFuncChecker("ok1", func() bool { return true }))
	handler.AddChecker(NewFuncChecker("ok2", func() bool { return true }))

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when all checkers pass, got %d", w.Code)
	}
}

func TestReadinessHandler_MultipleCheckersOneFails(t *testing.T) {
	handler := NewReadinessHandler()
	handler.AddChecker(NewFuncChecker("ok", func() bool { return true }))
	handler.AddChecker(NewFuncChecker("bad", func() bool { return false }))

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when one checker fails, got %d", w.Code)
	}
}

func TestReadinessHandler_SetReadyBackToTrue(t *testing.T) {
	handler := NewReadinessHandler()
	handler.SetReady(false)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}

	handler.SetReady(true)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 after SetReady(true), got %d", w.Code)
	}
}
