package httputil

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"c.n/ojv/cog/log"
)

const traceIDHeader = "X-Request-Id"

// RecoverMiddleware recovers from panics in HTTP handlers, logs the
// panic with a stack trace, and returns a 500 Internal Server Error.
// Without this, net/http's default behavior logs the panic and closes
// the connection without sending an HTTP response — the client sees
// a connection reset and cannot distinguish it from a network error.
//
// Must be the OUTERMOST middleware in the chain so it can recover
// panics from all inner middlewares and the handler itself. If the
// handler already wrote headers before panicking, the 500 response
// is a no-op (net/http logs a "superfluous WriteHeader" warning),
// which is acceptable for that rare case.
func RecoverMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rv := recover(); rv != nil {
				log.Errorf("HTTP: panic recovered: %v\n%s", rv, debug.Stack())
				Error(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next(w, r)
	}
}

// RequestLogger logs each request with a trace ID (reusing the
// inbound X-Request-Id header when present, otherwise generating one).
func RequestLogger(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		traceID := r.Header.Get(traceIDHeader)
		if traceID == "" {
			randBytes := make([]byte, 4)
			if _, err := rand.Read(randBytes); err != nil {
				randBytes = []byte{byte(time.Now().UnixNano())}
			}
			traceID = fmt.Sprintf("%d-%08x", time.Now().UnixNano(), randBytes)
		}
		w.Header().Set(traceIDHeader, traceID)

		start := time.Now()
		log.Infof("HTTP: [%s] %s %s", traceID, r.Method, r.URL.Path)

		next(w, r)

		duration := time.Since(start)
		log.Infof("HTTP: [%s] completed in %v", traceID, duration)
	}
}

// CORSMiddleware returns a middleware that applies CORS policy.
// The origin argument may be "*" or a comma-separated allowlist.
// For an allowlist, the matched request Origin is echoed back.
func CORSMiddleware(origin string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if origin == "" {
				origin = "*"
			}
			// Determine the ACAO header value. For a comma-separated allowlist,
			// echo back the single matched request Origin (browsers reject
			// comma-separated ACAO values). For "*", pass through as-is.
			var acao string
			if origin == "*" {
				acao = "*"
			} else {
				requestOrigin := r.Header.Get("Origin")
				if requestOrigin == "" {
					// No Origin header — same-origin or non-browser client.
					acao = origin
				} else if requestOrigin == origin {
					acao = origin
				} else {
					// Check comma-separated allowlist.
					allowed := false
					for _, allowedOrigin := range strings.Split(origin, ",") {
						allowedOrigin = strings.TrimSpace(allowedOrigin)
						if requestOrigin == allowedOrigin {
							allowed = true
							break
						}
					}
					if !allowed {
						Error(w, http.StatusForbidden, "origin not allowed")
						return
					}
					acao = requestOrigin // echo the single matched origin
				}
			}
			w.Header().Set("Access-Control-Allow-Origin", acao)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-Id, Authorization")
			w.Header().Set("Access-Control-Max-Age", "86400")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			next(w, r)
		}
	}
}

// AuthMiddleware returns a middleware that enforces a Bearer token
// API key. When apiKey is empty, the middleware is a no-op pass-through.
func AuthMiddleware(apiKey string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if apiKey == "" {
				next(w, r)
				return
			}

			authHeader := r.Header.Get("Authorization")
			expectedKey := "Bearer " + apiKey
			if subtle.ConstantTimeCompare([]byte(authHeader), []byte(expectedKey)) != 1 {
				Error(w, http.StatusUnauthorized, "unauthorized: invalid or missing api key")
				return
			}

			next(w, r)
		}
	}
}

// LimitBodySize returns a middleware that caps request body size.
// When maxBytes <= 0, a 1 MiB default is applied.
func LimitBodySize(maxBytes int64) func(http.HandlerFunc) http.HandlerFunc {
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			r.Body = io.NopCloser(http.MaxBytesReader(w, r.Body, maxBytes))
			next(w, r)
		}
	}
}

// TimeoutMiddleware sets a server-side per-request deadline by wrapping
// r.Context() with context.WithTimeout. Handlers that respect
// r.Context().Done() (directly or via context-aware I/O like
// json.Decoder, DB queries, channel sends with select) will abort
// promptly when the deadline fires, freeing the goroutine.
//
// Unlike http.Server.WriteTimeout (an absolute write deadline that
// fires on the first write attempt), this cancels the request context
// even if the handler never writes — closing the gap where a handler
// blocked on a lock or channel would otherwise consume a goroutine
// forever.
//
// The middleware does NOT write a 503 on timeout: the handler may have
// already started writing (concurrent WriteHeader would panic), and
// context-aware handlers produce their own error response on
// ctx.Err(). The sole mechanism is context cancellation.
//
// SSE endpoints MUST be exempt from this middleware: they run for
// hours and rely on r.Context().Done() for client-disconnect
// detection.
func TimeoutMiddleware(timeout time.Duration) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if timeout <= 0 {
				next(w, r)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next(w, r.WithContext(ctx))
		}
	}
}
