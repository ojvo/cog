package httputil

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestTimeoutMiddleware_NoOpWhenZero verifies that timeout<=0 passes
// the request through without altering the context deadline.
func TestTimeoutMiddleware_NoOpWhenZero(t *testing.T) {
	parentDeadline := time.Now().Add(30 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), parentDeadline)
	defer cancel()

	var seenDeadline time.Time
	var ok bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenDeadline, ok = r.Context().Deadline()
		w.WriteHeader(http.StatusOK)
	})

	TimeoutMiddleware(0)(handler).ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx),
	)

	if !ok {
		t.Fatal("expected a deadline to be present (inherited from parent)")
	}
	if !seenDeadline.Equal(parentDeadline) {
		t.Errorf("deadline altered: got %v, want %v (parent should be preserved)", seenDeadline, parentDeadline)
	}
}

// TestTimeoutMiddleware_SetsShorterDeadline verifies that the middleware
// tightens the deadline when its timeout is sooner than the parent's.
func TestTimeoutMiddleware_SetsShorterDeadline(t *testing.T) {
	// Parent deadline far in the future.
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	var got time.Time
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = r.Context().Deadline()
		w.WriteHeader(http.StatusOK)
	})

	start := time.Now()
	TimeoutMiddleware(50 * time.Millisecond)(handler).ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx),
	)

	if got.IsZero() {
		t.Fatal("expected a deadline to be set")
	}
	elapsed := got.Sub(start)
	if elapsed < 40*time.Millisecond || elapsed > 200*time.Millisecond {
		t.Errorf("deadline not ~50ms from start: got %v", elapsed)
	}
}

// TestTimeoutMiddleware_DoesNotExtendParentDeadline verifies that the
// middleware respects a shorter parent deadline instead of extending it.
func TestTimeoutMiddleware_DoesNotExtendParentDeadline(t *testing.T) {
	short := 100 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), short)
	defer cancel()

	parentDeadline, _ := ctx.Deadline()

	var got time.Time
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = r.Context().Deadline()
		w.WriteHeader(http.StatusOK)
	})

	// Middleware timeout (1h) is longer than parent (100ms) — parent wins.
	TimeoutMiddleware(time.Hour)(handler).ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx),
	)

	if !got.Equal(parentDeadline) {
		t.Errorf("deadline altered: got %v, want parent %v", got, parentDeadline)
	}
}

// TestTimeoutMiddleware_HandlerSeesCancellation verifies that a handler
// blocking past the deadline observes ctx.Done() and can abort.
func TestTimeoutMiddleware_HandlerSeesCancellation(t *testing.T) {
	done := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			w.WriteHeader(http.StatusServiceUnavailable)
			close(done)
		case <-time.After(time.Hour):
			t.Error("handler did not observe cancellation in time")
		}
	})

	rec := httptest.NewRecorder()
	TimeoutMiddleware(20*time.Millisecond)(handler).ServeHTTP(
		rec,
		httptest.NewRequest(http.MethodGet, "/", nil),
	)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler never observed cancellation")
	}

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
