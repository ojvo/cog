package netx

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"ojv/cog/cam/resil"
)

// HTTPClientConfig configures an HTTPClient.
type HTTPClientConfig struct {
	BaseURL     string
	Timeout     time.Duration
	Headers     map[string]string
	Debug       bool
	MaxRetries  int
	RetryFunc   func(*ClientResponse, error) bool
	RetryConfig *resil.RetryConfig
	// Proxy URL string (e.g. "http://host:port").
	Proxy string
	// CookieJar for cookie persistence. If nil, cookies are not persisted.
	CookieJar http.CookieJar
	// AllowRedirects controls following redirects. Default true.
	AllowRedirects bool
	// VerifySSL enables TLS certificate verification. Default false.
	VerifySSL bool
	// RateLimitConfig enables rate limiting if set.
	RateLimitConfig *resil.RateLimitConfig
}

// HTTPClient is a reusable HTTP client with connection pooling,
// retry support, rate limiting, and runtime statistics.
type HTTPClient struct {
	client          *http.Client
	baseURL         string
	headers         map[string]string
	debug           bool
	maxRetries      int
	retryFunc       func(*ClientResponse, error) bool
	retryConfig     *resil.RetryConfig
	rateLimitConfig *resil.RateLimitConfig

	requestTotal int64
	attemptTotal int64
	retryTotal   int64
	successTotal int64
	errorTotal   int64
	lastStatus   int32
}

// ClientRequest is the request structure for HTTPClient.Do.
type ClientRequest struct {
	Method   string
	Path     string
	Params   url.Values
	Body     interface{} // JSON body (auto-marshaled)
	RawBody  []byte      // Raw body bytes (Content-Type set by caller via Headers)
	Headers  map[string]string
	FormData map[string]string   // Form data (application/x-www-form-urlencoded)
	Files    map[string][]string // Multipart file upload: field -> {filename, content}
	Cookies  []*http.Cookie
}

// ClientResponse is the response from HTTPClient.Do.
type ClientResponse struct {
	StatusCode int
	Body       []byte
	Headers    http.Header
}

// HTTPClientStats is a snapshot of HTTP client runtime statistics.
type HTTPClientStats struct {
	RequestTotal int64
	AttemptTotal int64
	RetryTotal   int64
	SuccessTotal int64
	ErrorTotal   int64
	LastStatus   int
}

// NewHTTPClient creates an HTTPClient with the given config.
func NewHTTPClient(config HTTPClientConfig) *HTTPClient {
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	if config.MaxRetries < 0 {
		config.MaxRetries = 0
	}

	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: !config.VerifySSL},
	}

	if config.Proxy != "" {
		if proxy, err := url.Parse(config.Proxy); err == nil {
			transport.Proxy = http.ProxyURL(proxy)
		}
	}

	client := &http.Client{
		Timeout:   config.Timeout,
		Transport: transport,
		Jar:       config.CookieJar,
	}

	if !config.AllowRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	retryConfig := config.RetryConfig
	if retryConfig == nil {
		retryConfig = &resil.RetryConfig{
			MaxRetries:    config.MaxRetries,
			RetryDelay:    100 * time.Millisecond,
			RetryMaxDelay: 2 * time.Second,
			RetryJitter:   0.1,
		}
	}

	// Deep copy headers to avoid aliasing the caller's map
	var headers map[string]string
	if config.Headers != nil {
		headers = make(map[string]string, len(config.Headers))
		for k, v := range config.Headers {
			headers[k] = v
		}
	}

	return &HTTPClient{
		client:          client,
		baseURL:         config.BaseURL,
		headers:         headers,
		debug:           config.Debug,
		maxRetries:      config.MaxRetries,
		retryFunc:       config.RetryFunc,
		retryConfig:     retryConfig,
		rateLimitConfig: config.RateLimitConfig,
	}
}

// GetStats returns a snapshot of runtime statistics.
func (c *HTTPClient) GetStats() HTTPClientStats {
	return HTTPClientStats{
		RequestTotal: atomic.LoadInt64(&c.requestTotal),
		AttemptTotal: atomic.LoadInt64(&c.attemptTotal),
		RetryTotal:   atomic.LoadInt64(&c.retryTotal),
		SuccessTotal: atomic.LoadInt64(&c.successTotal),
		ErrorTotal:   atomic.LoadInt64(&c.errorTotal),
		LastStatus:   int(atomic.LoadInt32(&c.lastStatus)),
	}
}

// Do executes an HTTP request with retry and rate limiting support.
func (c *HTTPClient) Do(ctx context.Context, req ClientRequest) (*ClientResponse, error) {
	atomic.AddInt64(&c.requestTotal, 1)

	// Rate limiting - use per-client limiter instead of global singleton
	// to ensure different clients with different configs are isolated.
	if c.rateLimitConfig != nil && c.rateLimitConfig.RateLimit > 0 {
		limiter := resil.NewRateLimiter(c.rateLimitConfig)
		if err := limiter.Wait(); err != nil {
			atomic.AddInt64(&c.errorTotal, 1)
			return nil, fmt.Errorf("rate limit wait timeout: %w", err)
		}
	}

	// Build URL
	baseURL := strings.TrimRight(c.baseURL, "/")
	reqPath := strings.TrimLeft(req.Path, "/")

	var reqURL string
	if baseURL == "" {
		reqURL = reqPath
	} else {
		reqURL = baseURL + "/" + reqPath
	}
	if len(req.Params) > 0 {
		if strings.Contains(reqURL, "?") {
			reqURL += "&" + req.Params.Encode()
		} else {
			reqURL += "?" + req.Params.Encode()
		}
	}

	// Build request body
	bodyBytes, contentType, err := buildRequestBody(req)
	if err != nil {
		atomic.AddInt64(&c.errorTotal, 1)
		return nil, fmt.Errorf("build request body: %w", err)
	}

	var lastErr error
	var lastResp *ClientResponse

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		atomic.AddInt64(&c.attemptTotal, 1)
		if attempt > 0 {
			atomic.AddInt64(&c.retryTotal, 1)
		}

		select {
		case <-ctx.Done():
			atomic.AddInt64(&c.errorTotal, 1)
			return nil, ctx.Err()
		default:
		}

		if attempt > 0 {
			backoff := resil.CalculateRetryDelay(attempt-1, c.retryConfig)

			if c.debug {
				fmt.Printf("[HTTPClient] Retry attempt %d, waiting %v\n", attempt, backoff)
			}

			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}

		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		httpReq, err := http.NewRequestWithContext(ctx, req.Method, reqURL, bodyReader)
		if err != nil {
			atomic.AddInt64(&c.errorTotal, 1)
			return nil, fmt.Errorf("create request: %w", err)
		}

		// Apply default headers
		for k, v := range c.headers {
			httpReq.Header.Set(k, v)
		}
		// Apply per-request headers
		for k, v := range req.Headers {
			httpReq.Header.Set(k, v)
		}
		// Set Content-Type if not already set
		if contentType != "" && httpReq.Header.Get("Content-Type") == "" {
			httpReq.Header.Set("Content-Type", contentType)
		}
		// Add cookies
		for _, cookie := range req.Cookies {
			httpReq.AddCookie(cookie)
		}

		if c.debug {
			fmt.Printf("[HTTPClient] %s %s (attempt %d)\n", req.Method, reqURL, attempt)
		}

		resp, err := c.client.Do(httpReq)
		if err != nil {
			if resp != nil {
				_ = resp.Body.Close()
			}
			lastErr = err
			if c.shouldRetry(req.Method, nil, err) {
				continue
			}
			atomic.AddInt64(&c.errorTotal, 1)
			return nil, fmt.Errorf("request failed: %w", err)
		}

		// For HEAD requests, don't read body
		if req.Method == http.MethodHead {
			_ = resp.Body.Close()
			lastResp = &ClientResponse{
				StatusCode: resp.StatusCode,
				Headers:    resp.Header,
			}
			atomic.StoreInt32(&c.lastStatus, int32(resp.StatusCode))
			if c.shouldRetry(req.Method, lastResp, nil) {
				lastErr = fmt.Errorf("http status %d", resp.StatusCode)
				continue
			}
			atomic.AddInt64(&c.successTotal, 1)
			return lastResp, nil
		}

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read response body: %w", err)
			if c.shouldRetry(req.Method, nil, lastErr) {
				continue
			}
			atomic.AddInt64(&c.errorTotal, 1)
			return nil, lastErr
		}

		if c.debug {
			fmt.Printf("[HTTPClient] Response: %d (len: %d)\n", resp.StatusCode, len(body))
		}

		lastResp = &ClientResponse{
			StatusCode: resp.StatusCode,
			Body:       body,
			Headers:    resp.Header,
		}
		atomic.StoreInt32(&c.lastStatus, int32(resp.StatusCode))

		if c.shouldRetry(req.Method, lastResp, nil) {
			lastErr = fmt.Errorf("http status %d", resp.StatusCode)
			continue
		}

		atomic.AddInt64(&c.successTotal, 1)
		return lastResp, nil
	}

	atomic.AddInt64(&c.errorTotal, 1)
	return nil, fmt.Errorf("request failed after %d retries: %w", c.maxRetries, lastErr)
}

// buildRequestBody builds the request body and returns the body bytes and Content-Type.
func buildRequestBody(req ClientRequest) ([]byte, string, error) {
	// Priority: Files > FormData > RawBody > Body(JSON)
	if req.Files != nil {
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)

		// Add form fields
		for k, v := range req.FormData {
			if err := writer.WriteField(k, v); err != nil {
				return nil, "", err
			}
		}

		// Add files
		for fieldName, file := range req.Files {
			if len(file) == 0 {
				return nil, "", errors.New("empty file data for field: " + fieldName)
			}
			fileWriter, err := writer.CreateFormFile(fieldName, file[0])
			if err != nil {
				return nil, "", err
			}
			fileContent := []byte("")
			if len(file) > 1 {
				fileContent = []byte(file[1])
			}
			if _, err = fileWriter.Write(fileContent); err != nil {
				return nil, "", err
			}
		}

		contentType := writer.FormDataContentType()
		if err := writer.Close(); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), contentType, nil
	}

	if req.FormData != nil {
		values := url.Values{}
		for k, v := range req.FormData {
			values.Set(k, v)
		}
		return []byte(values.Encode()), "application/x-www-form-urlencoded", nil
	}

	if req.RawBody != nil {
		return req.RawBody, "application/x-www-form-urlencoded", nil
	}

	if req.Body != nil {
		data, err := json.Marshal(req.Body)
		if err != nil {
			return nil, "", err
		}
		return data, "application/json", nil
	}

	return nil, "", nil
}

func (c *HTTPClient) shouldRetry(method string, resp *ClientResponse, err error) bool {
	if c.retryFunc != nil {
		return c.retryFunc(resp, err)
	}

	if !isHTTPIdempotent(method) {
		return false
	}

	if err != nil {
		return true
	}

	if resp != nil {
		if resp.StatusCode >= 500 && resp.StatusCode <= 599 {
			return true
		}
		if resp.StatusCode == 429 {
			return true
		}
	}

	return false
}

func isHTTPIdempotent(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

// Get is a shortcut for GET requests.
func (c *HTTPClient) Get(ctx context.Context, path string, params url.Values) (*ClientResponse, error) {
	return c.Do(ctx, ClientRequest{Method: http.MethodGet, Path: path, Params: params})
}

// Post is a shortcut for POST requests.
func (c *HTTPClient) Post(ctx context.Context, path string, body interface{}) (*ClientResponse, error) {
	return c.Do(ctx, ClientRequest{Method: http.MethodPost, Path: path, Body: body})
}

// GetJSON sends a GET request and unmarshals the JSON response.
func (c *HTTPClient) GetJSON(ctx context.Context, path string, params url.Values, result interface{}) error {
	resp, err := c.Get(ctx, path, params)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http status: %d, body: %s", resp.StatusCode, string(resp.Body))
	}
	if err := json.Unmarshal(resp.Body, result); err != nil {
		return fmt.Errorf("unmarshal json: %w", err)
	}
	return nil
}

// PostJSON sends a POST request and unmarshals the JSON response.
func (c *HTTPClient) PostJSON(ctx context.Context, path string, body interface{}, result interface{}) error {
	resp, err := c.Post(ctx, path, body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http status: %d, body: %s", resp.StatusCode, string(resp.Body))
	}
	if result != nil && len(resp.Body) > 0 {
		if err := json.Unmarshal(resp.Body, result); err != nil {
			return fmt.Errorf("unmarshal json: %w", err)
		}
	}
	return nil
}
