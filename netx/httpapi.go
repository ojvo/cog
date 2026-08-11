package netx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"ojv/cog/cam/resil"
)

// RequestOptions configures a function-style HTTP request.
type RequestOptions struct {
	Timeout         int
	AllowRedirects  bool
	Verify          bool
	Headers         map[string]string
	Params          map[string]string
	Data            string
	DataJson        map[string]string
	Json            map[string]any
	File            map[string][]string
	Proxy           string
	Cookies         []*http.Cookie
	CookieJar       http.CookieJar
	RetryConfig     *resil.HTTPRetryConfig
	RateLimitConfig *resil.RateLimitConfig
}

// Response is the function-style HTTP response.
type Response struct {
	Url        string
	StatusCode int
	Status     string
	Timer      float64
	Headers    http.Header
	Body       string
	Content    []byte
	Json       map[string]interface{}
	Length     int
	Cookies    []*http.Cookie
	Request    struct {
		URL     string
		Method  string
		Headers http.Header
		Body    []byte
	}
}

// IsSuccess returns true if the status code is in the 2xx range.
func (r *Response) IsSuccess() bool { return r.StatusCode >= 200 && r.StatusCode < 300 }

// IsRedirect returns true if the status code is in the 3xx range.
func (r *Response) IsRedirect() bool { return r.StatusCode >= 300 && r.StatusCode < 400 }

// IsError returns true if the status code is >= 400.
func (r *Response) IsError() bool { return r.StatusCode >= 400 }

// GetHeader returns the value of the named response header.
func (r *Response) GetHeader(name string) string { return r.Headers.Get(name) }

// NewRequestOptions returns a RequestOptions with sensible defaults.
func NewRequestOptions() *RequestOptions {
	return &RequestOptions{
		Timeout:        10,
		AllowRedirects: false,
		Verify:         false,
		Headers:        make(map[string]string),
		Params:         make(map[string]string),
	}
}

// WithRetry sets the retry config on RequestOptions.
func (ro RequestOptions) WithRetry(config *resil.HTTPRetryConfig) RequestOptions {
	ro.RetryConfig = config
	return ro
}

// WithDefaultRetry sets default retry config on RequestOptions.
func (ro RequestOptions) WithDefaultRetry() RequestOptions {
	ro.RetryConfig = resil.NewHTTPRetryConfig()
	return ro
}

// WithRateLimit sets the rate limit config on RequestOptions.
func (ro RequestOptions) WithRateLimit(config *resil.RateLimitConfig) RequestOptions {
	ro.RateLimitConfig = config
	return ro
}

// HttpRequest sends an HTTP request using a temporary HTTPClient.
func HttpRequest(method, baseurl string, args ...RequestOptions) (*Response, error) {
	var args0 RequestOptions
	if len(args) > 0 {
		args0 = args[0]
	}

	config := HTTPClientConfig{
		Timeout:         time.Duration(args0.Timeout) * time.Second,
		Headers:         args0.Headers,
		Proxy:           args0.Proxy,
		CookieJar:       args0.CookieJar,
		AllowRedirects:  args0.AllowRedirects,
		VerifySSL:       args0.Verify,
		RateLimitConfig: args0.RateLimitConfig,
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	if args0.RetryConfig != nil {
		config.MaxRetries = args0.RetryConfig.MaxRetries
		config.RetryConfig = args0.RetryConfig.RetryConfig
	}

	client := NewHTTPClient(config)

	req := ClientRequest{
		Method:  getHTTPMethod(method),
		Path:    baseurl,
		Params:  mapToValues(args0.Params),
		Files:   args0.File,
		Cookies: args0.Cookies,
	}
	if args0.DataJson != nil {
		req.FormData = args0.DataJson
	}
	if args0.Json != nil {
		req.Body = args0.Json
	}
	if args0.Data != "" {
		req.RawBody = []byte(args0.Data)
	}

	startTime := time.Now()
	resp, err := client.Do(context.Background(), req)
	elapsed := time.Since(startTime).Seconds()
	if err != nil {
		return nil, err
	}

	result := &Response{
		Url:        baseurl,
		StatusCode: resp.StatusCode,
		Headers:    resp.Headers,
		Content:    resp.Body,
		Body:       string(resp.Body),
		Length:     len(resp.Body),
		Timer:      elapsed,
		Request: struct {
			URL     string
			Method  string
			Headers http.Header
			Body    []byte
		}{
			URL:    baseurl,
			Method: req.Method,
		},
	}

	// Extract cookies from Set-Cookie headers
	if resp.Headers != nil {
		httpResp := &http.Response{Header: resp.Headers}
		result.Cookies = httpResp.Cookies()
	}

	// Try parsing JSON
	var jsonData map[string]interface{}
	if err := json.Unmarshal(resp.Body, &jsonData); err == nil {
		result.Json = jsonData
	}

	return result, nil
}

// HttpGet sends a GET request.
func HttpGet(baseurl string, arg ...RequestOptions) (*Response, error) {
	return HttpRequest("get", baseurl, arg...)
}

// HttpPost sends a POST request.
func HttpPost(baseurl string, arg ...RequestOptions) (*Response, error) {
	return HttpRequest("post", baseurl, arg...)
}

// HttpHead sends a HEAD request.
func HttpHead(baseurl string, arg ...RequestOptions) (*Response, error) {
	return HttpRequest("head", baseurl, arg...)
}

// HttpPut sends a PUT request.
func HttpPut(baseurl string, arg ...RequestOptions) (*Response, error) {
	return HttpRequest("put", baseurl, arg...)
}

// HttpOptions sends an OPTIONS request.
func HttpOptions(baseurl string, arg ...RequestOptions) (*Response, error) {
	return HttpRequest("options", baseurl, arg...)
}

// HttpDelete sends a DELETE request.
func HttpDelete(baseurl string, arg ...RequestOptions) (*Response, error) {
	return HttpRequest("delete", baseurl, arg...)
}

// HttpPatch sends a PATCH request.
func HttpPatch(baseurl string, arg ...RequestOptions) (*Response, error) {
	return HttpRequest("patch", baseurl, arg...)
}

// DownloadFile downloads a URL to a local file.
func DownloadFile(url, filepath string, args ...RequestOptions) error {
	resp, err := HttpGet(url, args...)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed, status: %d", resp.StatusCode)
	}
	return os.WriteFile(filepath, resp.Content, 0644)
}

// GetJSON sends a GET request and returns the parsed JSON.
func GetJSON(url string, args ...RequestOptions) (map[string]interface{}, error) {
	resp, err := HttpGet(url, args...)
	if err != nil {
		return nil, err
	}
	if resp.Json == nil {
		return nil, fmt.Errorf("response is not valid JSON")
	}
	return resp.Json, nil
}

// PostJSON sends a POST request with JSON body and returns the parsed JSON response.
func PostJSON(url string, jsonData map[string]any, args ...RequestOptions) (map[string]interface{}, error) {
	var args0 RequestOptions
	if len(args) > 0 {
		args0 = args[0]
	}
	args0.Json = jsonData
	resp, err := HttpPost(url, args0)
	if err != nil {
		return nil, err
	}
	if resp.Json == nil {
		return nil, fmt.Errorf("response is not valid JSON")
	}
	return resp.Json, nil
}

// getHTTPMethod maps lowercase method names to standard HTTP methods.
func getHTTPMethod(method string) string {
	switch method {
	case "get":
		return http.MethodGet
	case "head":
		return http.MethodHead
	case "options":
		return http.MethodOptions
	case "post":
		return http.MethodPost
	case "put":
		return http.MethodPut
	case "delete":
		return http.MethodDelete
	case "patch":
		return http.MethodPatch
	default:
		return http.MethodGet
	}
}

// mapToValues converts a map[string]string to url.Values.
func mapToValues(m map[string]string) url.Values {
	if len(m) == 0 {
		return nil
	}
	values := url.Values{}
	for k, v := range m {
		values.Set(k, v)
	}
	return values
}

// HeaderFromStruct converts a struct to a map[string]string header map
// via JSON marshaling. Struct field names become header keys (as-is from JSON),
// and values are stringified with fmt.Sprint.
func HeaderFromStruct(v interface{}) (map[string]string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal struct: %w", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("unmarshal to header map: %w", err)
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		result[k] = fmt.Sprint(v)
	}
	return result, nil
}
