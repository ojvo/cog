package netx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"c.n/ojv/cog/resil"
)

// === HTTPClient (object-oriented API) tests ===

func TestHTTPClient_Get(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "test" {
			t.Errorf("expected q=test, got %s", r.URL.Query().Get("q"))
		}
		w.WriteHeader(200)
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer srv.Close()

	client := NewHTTPClient(HTTPClientConfig{BaseURL: srv.URL, MaxRetries: 0})
	resp, err := client.Get(context.Background(), "/api", map[string][]string{"q": {"test"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if string(resp.Body) != `{"status":"ok"}` {
		t.Errorf("body = %s", string(resp.Body))
	}

	stats := client.GetStats()
	if stats.RequestTotal != 1 || stats.SuccessTotal != 1 {
		t.Errorf("stats = %+v, want request=1 success=1", stats)
	}
}

func TestHTTPClient_PostJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"echo": body["msg"]})
	}))
	defer srv.Close()

	client := NewHTTPClient(HTTPClientConfig{BaseURL: srv.URL})

	var result map[string]string
	err := client.PostJSON(context.Background(), "/api", map[string]string{"msg": "hello"}, &result)
	if err != nil {
		t.Fatal(err)
	}
	if result["echo"] != "hello" {
		t.Errorf("echo = %s, want hello", result["echo"])
	}
}

func TestHTTPClient_GetJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, `{"name":"test","value":42}`)
	}))
	defer srv.Close()

	client := NewHTTPClient(HTTPClientConfig{BaseURL: srv.URL})

	var result struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}
	if err := client.GetJSON(context.Background(), "/api", nil, &result); err != nil {
		t.Fatal(err)
	}
	if result.Name != "test" || result.Value != 42 {
		t.Errorf("result = %+v", result)
	}
}

func TestHTTPClient_RetryOn5xx(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	client := NewHTTPClient(HTTPClientConfig{
		BaseURL:    srv.URL,
		MaxRetries: 3,
	})
	resp, err := client.Get(context.Background(), "/api", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	stats := client.GetStats()
	if stats.RetryTotal != 2 {
		t.Errorf("retryTotal = %d, want 2", stats.RetryTotal)
	}
}

func TestHTTPClient_NoRetryOnNonIdempotent(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(500)
	}))
	defer srv.Close()

	client := NewHTTPClient(HTTPClientConfig{
		BaseURL:    srv.URL,
		MaxRetries: 3,
	})

	// POST is not idempotent → 500 returns without retry, no Go error
	resp, err := client.Post(context.Background(), "/api", map[string]string{"k": "v"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 500 {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}

	// POST is not idempotent → no retry
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (no retry for POST)", attempts)
	}
}

func TestHTTPClient_RetryOn429(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 2 {
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client := NewHTTPClient(HTTPClientConfig{
		BaseURL:    srv.URL,
		MaxRetries: 2,
	})
	resp, err := client.Get(context.Background(), "/api", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHTTPClient_CustomRetryFunc(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	client := NewHTTPClient(HTTPClientConfig{
		BaseURL:    srv.URL,
		MaxRetries: 2,
		RetryFunc: func(resp *ClientResponse, err error) bool {
			// Custom: retry on 404
			return resp != nil && resp.StatusCode == 404
		},
	})

	_, err := client.Get(context.Background(), "/api", nil)
	if err == nil {
		t.Error("expected error after retries")
	}

	stats := client.GetStats()
	if stats.RetryTotal != 2 {
		t.Errorf("retryTotal = %d, want 2", stats.RetryTotal)
	}
}

func TestHTTPClient_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow handler, will be cancelled
		select {
		case <-r.Context().Done():
			return
		default:
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	client := NewHTTPClient(HTTPClientConfig{BaseURL: srv.URL})
	_, err := client.Get(ctx, "/api", nil)
	if err == nil {
		t.Error("expected context canceled error")
	}

	stats := client.GetStats()
	if stats.ErrorTotal != 1 {
		t.Errorf("errorTotal = %d, want 1", stats.ErrorTotal)
	}
}

func TestHTTPClient_Stats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client := NewHTTPClient(HTTPClientConfig{BaseURL: srv.URL, MaxRetries: 0})

	for i := 0; i < 5; i++ {
		_, _ = client.Get(context.Background(), "/api", nil)
	}

	stats := client.GetStats()
	if stats.RequestTotal != 5 {
		t.Errorf("requestTotal = %d, want 5", stats.RequestTotal)
	}
	if stats.SuccessTotal != 5 {
		t.Errorf("successTotal = %d, want 5", stats.SuccessTotal)
	}
	if stats.LastStatus != 200 {
		t.Errorf("lastStatus = %d, want 200", stats.LastStatus)
	}
}

func TestHTTPClient_ConnectionReuse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client := NewHTTPClient(HTTPClientConfig{BaseURL: srv.URL})

	// Multiple requests should reuse the same client/connection pool
	for i := 0; i < 10; i++ {
		resp, err := client.Get(context.Background(), "/api", nil)
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("request %d status = %d", i, resp.StatusCode)
		}
	}

	stats := client.GetStats()
	if stats.RequestTotal != 10 {
		t.Errorf("requestTotal = %d, want 10", stats.RequestTotal)
	}
}

func TestHTTPClient_FormData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %s, want application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm failed: %v", err)
		}
		if r.FormValue("key") != "val" {
			t.Errorf("FormValue key = %s, want val", r.FormValue("key"))
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client := NewHTTPClient(HTTPClientConfig{BaseURL: srv.URL})
	resp, err := client.Do(context.Background(), ClientRequest{
		Method:   "POST",
		Path:     "/api",
		FormData: map[string]string{"key": "val"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHTTPClient_FileUpload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseMultipartForm(10 << 20)
		if err != nil {
			t.Errorf("ParseMultipartForm failed: %v", err)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Errorf("FormFile failed: %v", err)
			return
		}
		defer file.Close()

		if header.Filename != "test.txt" {
			t.Errorf("filename = %s, want test.txt", header.Filename)
		}
		content, _ := io.ReadAll(file)
		if string(content) != "file content" {
			t.Errorf("content = %s, want 'file content'", string(content))
		}
		if r.FormValue("key") != "val" {
			t.Errorf("FormValue key = %s, want val", r.FormValue("key"))
		}
	}))
	defer srv.Close()

	client := NewHTTPClient(HTTPClientConfig{BaseURL: srv.URL})
	resp, err := client.Do(context.Background(), ClientRequest{
		Method:   "POST",
		Path:     "/api",
		FormData: map[string]string{"key": "val"},
		Files: map[string][]string{
			"file": {"test.txt", "file content"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHTTPClient_Cookies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("test_cookie")
		if err != nil || cookie.Value != "cookie_val" {
			t.Errorf("Expected cookie test_cookie=cookie_val, got %v", cookie)
		}
		http.SetCookie(w, &http.Cookie{Name: "resp_cookie", Value: "resp_val"})
	}))
	defer srv.Close()

	jar, _ := CreateCookieJar()
	client := NewHTTPClient(HTTPClientConfig{
		BaseURL:   srv.URL,
		CookieJar: jar,
	})
	resp, err := client.Do(context.Background(), ClientRequest{
		Method:  "GET",
		Path:    "/api",
		Cookies: []*http.Cookie{{Name: "test_cookie", Value: "cookie_val"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestHTTPClient_RateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	rlConfig := resil.NewRateLimitConfig().
		WithRateLimit(100).
		WithRateBurst(10)

	client := NewHTTPClient(HTTPClientConfig{
		BaseURL:         srv.URL,
		RateLimitConfig: rlConfig,
	})
	_, err := client.Get(context.Background(), "/api", nil)
	if err != nil {
		t.Fatalf("rate limited request failed: %v", err)
	}
}

// TestHTTPClient_HeadersDeepCopy verifies NewHTTPClient does not alias the
// caller's Headers map: mutating the original after client creation must not
// affect the client's default headers.
func TestHTTPClient_HeadersDeepCopy(t *testing.T) {
	var seenHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHeader = r.Header.Get("X-Test")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	headers := map[string]string{"X-Test": "original"}
	client := NewHTTPClient(HTTPClientConfig{BaseURL: srv.URL, Headers: headers})

	// Mutate the original map after client creation
	headers["X-Test"] = "mutated"

	_, err := client.Get(context.Background(), "/api", nil)
	if err != nil {
		t.Fatal(err)
	}
	if seenHeader != "original" {
		t.Fatalf("default header = %q, want %q (deep copy should isolate)", seenHeader, "original")
	}
}

// TestHTTPClient_RateLimiterPerClientIsolation verifies that two clients with
// different rate limit configs do not share a limiter: a low-rate client being
// throttled must not block a high-rate client.
func TestHTTPClient_RateLimiterPerClientIsolation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// Client A: very low rate (1 QPS, burst 1)
	lowConfig := resil.NewRateLimitConfig().
		WithRateLimit(1).
		WithRateBurst(1)
	clientLow := NewHTTPClient(HTTPClientConfig{
		BaseURL:         srv.URL,
		RateLimitConfig: lowConfig,
	})

	// Client B: high rate (1000 QPS, burst 100)
	highConfig := resil.NewRateLimitConfig().
		WithRateLimit(1000).
		WithRateBurst(100)
	clientHigh := NewHTTPClient(HTTPClientConfig{
		BaseURL:         srv.URL,
		RateLimitConfig: highConfig,
	})

	// Exhaust client A's burst
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := clientLow.Get(ctx, "/api", nil); err != nil {
		t.Fatalf("clientLow first request failed: %v", err)
	}

	// Client B should still be able to serve requests immediately
	start := time.Now()
	if _, err := clientHigh.Get(ctx, "/api", nil); err != nil {
		t.Fatalf("clientHigh request failed (should not be throttled by client A): %v", err)
	}
	elapsed := time.Since(start)
	// If global limiter were shared, client B would wait ~1s for client A's token.
	if elapsed > 500*time.Millisecond {
		t.Fatalf("clientHigh took %v, expected near-instant (limiter not isolated)", elapsed)
	}
}

func TestHTTPClient_HEAD(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("method = %s, want HEAD", r.Method)
		}
		w.Header().Set("X-Custom", "value")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client := NewHTTPClient(HTTPClientConfig{BaseURL: srv.URL})
	resp, err := client.Do(context.Background(), ClientRequest{
		Method: "HEAD",
		Path:   "/api",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if resp.Headers.Get("X-Custom") != "value" {
		t.Errorf("X-Custom = %s", resp.Headers.Get("X-Custom"))
	}
	if resp.Body != nil {
		t.Errorf("HEAD body should be nil, got %d bytes", len(resp.Body))
	}
}

// === Function-style API tests ===

func TestHttpApi_Get(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/test" {
			t.Errorf("Expected /test, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("q") != "hello" {
			t.Errorf("Expected q=hello, got %s", r.URL.Query().Get("q"))
		}

		w.Header().Set("X-Custom", "value")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	opts := NewRequestOptions()
	opts.Params["q"] = "hello"

	resp, err := HttpGet(ts.URL+"/test", *opts)
	if err != nil {
		t.Fatalf("HttpGet failed: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
	if resp.Body != "ok" {
		t.Errorf("Expected body ok, got %s", resp.Body)
	}
	if resp.GetHeader("X-Custom") != "value" {
		t.Errorf("Expected X-Custom header value, got %s", resp.GetHeader("X-Custom"))
	}
}

func TestHttpApi_PostJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}

		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", contentType)
		}

		var data map[string]string
		json.NewDecoder(r.Body).Decode(&data)
		if data["key"] != "val" {
			t.Errorf("Expected key=val, got %v", data)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success"}`))
	}))
	defer ts.Close()

	data := map[string]any{"key": "val"}
	resp, err := PostJSON(ts.URL, data)
	if err != nil {
		t.Fatalf("PostJSON failed: %v", err)
	}

	if resp["status"] != "success" {
		t.Errorf("Expected success status, got %v", resp)
	}
}

func TestHttpApi_Retry(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	retryConfig := resil.NewHTTPRetryConfig().
		WithMaxRetries(3).
		WithRetryDelay(10 * time.Millisecond)

	opts := NewRequestOptions()
	opts.RetryConfig = retryConfig

	resp, err := HttpGet(ts.URL, *opts)
	if err != nil {
		t.Fatalf("Retry request failed: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

func TestHttpApi_Download(t *testing.T) {
	content := "file content"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(content))
	}))
	defer ts.Close()

	tmpFile := "test_download.txt"
	defer os.Remove(tmpFile)

	err := DownloadFile(ts.URL, tmpFile)
	if err != nil {
		t.Fatalf("DownloadFile failed: %v", err)
	}

	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != content {
		t.Errorf("Expected %s, got %s", content, string(data))
	}
}

func TestHttpApi_Cookie(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("test_cookie")
		if err != nil || cookie.Value != "cookie_val" {
			t.Errorf("Expected cookie test_cookie=cookie_val, got %v", cookie)
		}

		http.SetCookie(w, &http.Cookie{Name: "resp_cookie", Value: "resp_val"})
	}))
	defer ts.Close()

	jar, _ := CreateCookieJar()
	opts := NewRequestOptions()
	opts.CookieJar = jar
	opts.Cookies = []*http.Cookie{
		{Name: "test_cookie", Value: "cookie_val"},
	}

	resp, err := HttpGet(ts.URL, *opts)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	// Check response cookies
	found := false
	for _, c := range resp.Cookies {
		if c.Name == "resp_cookie" && c.Value == "resp_val" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Response cookie not found")
	}
}

func TestHttpApi_RateLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	rlConfig := resil.NewRateLimitConfig().
		WithRateLimit(100).
		WithRateBurst(10)

	opts := NewRequestOptions()
	opts.RateLimitConfig = rlConfig

	_, err := HttpGet(ts.URL, *opts)
	if err != nil {
		t.Fatalf("Rate limited request failed: %v", err)
	}
}

func TestHttpApi_Upload(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseMultipartForm(10 << 20)
		if err != nil {
			t.Errorf("ParseMultipartForm failed: %v", err)
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			t.Errorf("FormFile failed: %v", err)
			return
		}
		defer file.Close()

		if header.Filename != "test.txt" {
			t.Errorf("Expected filename test.txt, got %s", header.Filename)
		}

		content, _ := io.ReadAll(file)
		if string(content) != "file content" {
			t.Errorf("Expected file content, got %s", string(content))
		}

		if r.FormValue("key") != "val" {
			t.Errorf("Expected form key=val, got %s", r.FormValue("key"))
		}
	}))
	defer ts.Close()

	opts := NewRequestOptions()
	opts.File = map[string][]string{
		"file": {"test.txt", "file content"},
	}
	opts.DataJson = map[string]string{
		"key": "val",
	}

	resp, err := HttpPost(ts.URL, *opts)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
}

func TestHeaderFromStruct(t *testing.T) {
	type AuthHeaders struct {
		Authorization string `json:"Authorization"`
		Accept        string `json:"Accept"`
		XRequestID    string `json:"X-Request-ID"`
	}

	headers, err := HeaderFromStruct(AuthHeaders{
		Authorization: "Bearer token123",
		Accept:        "application/json",
		XRequestID:    "abc-123",
	})
	if err != nil {
		t.Fatalf("HeaderFromStruct failed: %v", err)
	}

	if headers["Authorization"] != "Bearer token123" {
		t.Errorf("Expected 'Bearer token123', got '%s'", headers["Authorization"])
	}
	if headers["Accept"] != "application/json" {
		t.Errorf("Expected 'application/json', got '%s'", headers["Accept"])
	}
	if headers["X-Request-ID"] != "abc-123" {
		t.Errorf("Expected 'abc-123', got '%s'", headers["X-Request-ID"])
	}
}

func TestHeaderFromStruct_NonStruct(t *testing.T) {
	_, err := HeaderFromStruct("not a struct")
	if err == nil {
		t.Error("Expected error for non-struct string input")
	}
}
