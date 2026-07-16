package httputil

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"ojv/cog/log"
)

// ApiResponse is the canonical JSON envelope returned by server-side
// HTTP handlers.
type ApiResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// OK writes a 200 response with the standard envelope.
func OK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(ApiResponse{Code: http.StatusOK, Message: "success", Data: data}); err != nil {
		log.Errorf("HTTP: OK Encode failed: %v", err)
	}
}

// Accepted writes a 202 response with the standard envelope.
func Accepted(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(ApiResponse{Code: http.StatusAccepted, Message: "accepted", Data: data}); err != nil {
		log.Errorf("HTTP: Accepted Encode failed: %v", err)
	}
}

// Error writes a non-2xx response with the standard envelope.
func Error(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(ApiResponse{Code: statusCode, Message: message}); err != nil {
		log.Errorf("HTTP: Error Encode failed: %v", err)
	}
}

// NoContent writes a 204 response with no body.
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// ClearWriteDeadline removes the per-request write deadline set by
// http.Server.WriteTimeout. SSE/streaming handlers MUST call this
// before entering their streaming loop, otherwise WriteTimeout (an
// absolute deadline measured from when request headers were read)
// will kill the long-lived connection after the timeout expires.
//
// Uses http.ResponseController (Go 1.20+) to set the deadline to the
// zero value (no deadline). If the ResponseWriter doesn't support
// SetWriteDeadline (e.g., httptest.ResponseRecorder in tests), the
// error is logged as a warning but the handler continues — the stream
// still works, though in production it may be cut off by WriteTimeout.
func ClearWriteDeadline(w http.ResponseWriter) {
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		log.Warnf("HTTP: cannot clear write deadline (SSE stream may be cut off by WriteTimeout): %v", err)
	}
}

// SSEDefaultHeartbeat is the default interval between SSE heartbeat
// (keep-alive) comments. Proxies and load balancers typically close
// idle connections after 30-60s, so 15s gives a safe margin.
const SSEDefaultHeartbeat = 15 * time.Second

// SSEWrite writes an SSE frame to w and flushes. Returns the write
// error so the caller can detect a broken/client-disconnected
// connection and exit the streaming loop.
//
// Without this, a fmt.Fprintf + Flush sequence silently ignores write
// errors: the handler keeps looping, consuming events and attempting
// writes that go nowhere, until r.Context().Done() fires.
func SSEWrite(w io.Writer, flusher http.Flusher, format string, args ...any) error {
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// SSEHeartbeat writes a comment frame (": ping\n\n") that keeps the
// connection alive through proxies/load balancers that close idle
// connections after a timeout. The comment is ignored by SSE clients
// (EventSource spec: lines starting with ":" are comments).
// Returns the write error for client-disconnection detection.
func SSEHeartbeat(w io.Writer, flusher http.Flusher) error {
	return SSEWrite(w, flusher, ": ping\n\n")
}
