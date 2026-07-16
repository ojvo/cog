package httputil

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOK(t *testing.T) {
	w := httptest.NewRecorder()
	OK(w, map[string]string{"key": "value"})

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp ApiResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Code != http.StatusOK {
		t.Errorf("code = %d, want %d", resp.Code, http.StatusOK)
	}
	if resp.Message != "success" {
		t.Errorf("message = %q, want success", resp.Message)
	}
}

func TestAccepted(t *testing.T) {
	w := httptest.NewRecorder()
	Accepted(w, nil)

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
}

func TestError(t *testing.T) {
	w := httptest.NewRecorder()
	Error(w, http.StatusBadRequest, "bad request")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp ApiResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Message != "bad request" {
		t.Errorf("message = %q, want bad request", resp.Message)
	}
}

func TestNoContent(t *testing.T) {
	w := httptest.NewRecorder()
	NoContent(w)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestRequestLogger(t *testing.T) {
	called := false
	handler := RequestLogger(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	handler(w, r)

	if !called {
		t.Error("handler was not called")
	}
	traceID := w.Header().Get("X-Request-Id")
	if traceID == "" {
		t.Error("trace ID should be set")
	}
}

func TestRequestLoggerWithExistingTraceID(t *testing.T) {
	handler := RequestLogger(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	r.Header.Set("X-Request-Id", "existing-id")
	handler(w, r)

	if w.Header().Get("X-Request-Id") != "existing-id" {
		t.Errorf("trace ID = %q, want existing-id", w.Header().Get("X-Request-Id"))
	}
}

func TestCORSMiddlewareWildcard(t *testing.T) {
	called := false
	handler := CORSMiddleware("*")(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	handler(w, r)

	if !called {
		t.Error("handler was not called")
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("CORS origin = %q, want *", w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSMiddlewareOptions(t *testing.T) {
	handler := CORSMiddleware("*")(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for OPTIONS")
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("OPTIONS", "/test", nil)
	handler(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("OPTIONS status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestCORSMiddlewareDisallowedOrigin(t *testing.T) {
	handler := CORSMiddleware("https://allowed.com")(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for disallowed origin")
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	r.Header.Set("Origin", "https://evil.com")
	handler(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestCORSMiddlewareMultipleOrigins(t *testing.T) {
	called := false
	handler := CORSMiddleware("https://a.com, https://b.com")(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	r.Header.Set("Origin", "https://b.com")
	handler(w, r)

	if !called {
		t.Error("handler should be called for allowed origin")
	}
}

func TestAuthMiddlewareNoKey(t *testing.T) {
	called := false
	handler := AuthMiddleware("")(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	handler(w, r)

	if !called {
		t.Error("handler should be called when no API key configured")
	}
}

func TestAuthMiddlewareValidKey(t *testing.T) {
	called := false
	handler := AuthMiddleware("secret")(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	r.Header.Set("Authorization", "Bearer secret")
	handler(w, r)

	if !called {
		t.Error("handler should be called with valid key")
	}
}

func TestAuthMiddlewareInvalidKey(t *testing.T) {
	handler := AuthMiddleware("secret")(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called with invalid key")
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	r.Header.Set("Authorization", "Bearer wrong")
	handler(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestLimitBodySize(t *testing.T) {
	called := false
	handler := LimitBodySize(100)(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/test", strings.NewReader("small body"))
	handler(w, r)

	if !called {
		t.Error("handler should be called for small body")
	}
}

func TestChain(t *testing.T) {
	order := []string{}

	mw1 := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw1")
			next(w, r)
		}
	}
	mw2 := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw2")
			next(w, r)
		}
	}

	handler := Chain([]func(http.HandlerFunc) http.HandlerFunc{mw1, mw2}, func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	handler(w, r)

	// mw1 wraps mw2 wraps handler, so execution order is mw1 -> mw2 -> handler
	if len(order) != 3 || order[0] != "mw1" || order[1] != "mw2" || order[2] != "handler" {
		t.Errorf("execution order = %v, want [mw1 mw2 handler]", order)
	}
}

func TestRecoverMiddlewarePanic(t *testing.T) {
	handler := RecoverMiddleware(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic from handler")
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	handler(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(w.Body.String(), "internal server error") {
		t.Errorf("body = %q, want to contain 'internal server error'", w.Body.String())
	}
}

func TestRecoverMiddlewareNoPanic(t *testing.T) {
	called := false
	handler := RecoverMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	handler(w, r)

	if !called {
		t.Error("handler should be called when no panic occurs")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRecoverMiddlewareChained(t *testing.T) {
	// Verify RecoverMiddleware as outermost can recover panics from
	// inner middlewares and the handler.
	panicHandler := func(w http.ResponseWriter, r *http.Request) {
		panic("chained panic")
	}
	handler := Chain([]func(http.HandlerFunc) http.HandlerFunc{RecoverMiddleware, RequestLogger}, panicHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	handler(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// TestClearWriteDeadline_SSESurvivesTimeout verifies that an SSE handler
// calling ClearWriteDeadline can stream data past the server's
// WriteTimeout. Without ClearWriteDeadline, the connection would be
// killed after WriteTimeout (an absolute deadline from header read).
func TestClearWriteDeadline_SSESurvivesTimeout(t *testing.T) {
	// Create a server with a very short WriteTimeout (50ms).
	// The SSE handler will wait 200ms before writing — if the deadline
	// is not cleared, the write will fail with a timeout error.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Errorf("ResponseWriter does not support flushing")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		// Clear the write deadline so the 200ms sleep doesn't get killed.
		ClearWriteDeadline(w)

		// Sleep longer than WriteTimeout. Without ClearWriteDeadline,
		// the subsequent write would fail.
		time.Sleep(200 * time.Millisecond)

		_, err := w.Write([]byte("data: hello\n\n"))
		if err != nil {
			t.Errorf("write after timeout failed: %v (WriteTimeout killed the connection)", err)
		}
		flusher.Flush()
	})

	server := &http.Server{
		Addr:         "127.0.0.1:0",
		Handler:      handler,
		WriteTimeout: 50 * time.Millisecond,
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	go server.Serve(ln)
	defer server.Close()

	// Give the server a moment to start.
	time.Sleep(50 * time.Millisecond)

	// Connect and read the response.
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read the body. If WriteTimeout killed the connection, this would
	// return an error or an incomplete body.
	buf := make([]byte, 1024)
	n, err := resp.Body.Read(buf)
	if err != nil && err.Error() != "EOF" {
		t.Fatalf("read body failed: %v (WriteTimeout may have killed the connection)", err)
	}
	body := string(buf[:n])
	if !strings.Contains(body, "hello") {
		t.Errorf("body = %q, want to contain 'hello'", body)
	}
}

// TestClearWriteDeadline_NoDeadline verifies that ClearWriteDeadline
// is a no-op (with a warning) when the ResponseWriter doesn't support
// SetWriteDeadline (e.g., httptest.ResponseRecorder).
func TestClearWriteDeadline_NoDeadline(t *testing.T) {
	w := httptest.NewRecorder()
	// Should not panic even though ResponseRecorder doesn't support
	// SetWriteDeadline.
	ClearWriteDeadline(w)
}
