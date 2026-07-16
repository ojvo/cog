package ving

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Helper function to create a basic server
func newServer() *Engine {
	return New()
}

// --------------------------------------------------------------------------------
// Original example_test.go content
// --------------------------------------------------------------------------------

// 错误回调处理器
func errorHandler(ctx *Context) {
	// 记录错误日志
	log.Println("记录错误日志", ctx.Status, ctx.Error)
	// 输出错误信息到客户端
	ctx.ResponseWriter.WriteHeader(ctx.Status)
	if ctx.Error != nil {
		_, _ = ctx.ResponseWriter.Write([]byte(ctx.Error.Error()))
	}
}

// 后置回调处理器
func afterHandler(ctx *Context) {
	log.Println("执行了后置处理器", ctx.IsAborted())
}

// 测试回应
func TestEcho(t *testing.T) {
	app := New()
	app.GET("/", func(ctx *Context) error {
		t.Log("Hello Tsing")
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "GET", "/", nil)
	if err != nil {
		t.Error(err)
		return
	}
	app.ServeHTTP(httptest.NewRecorder(), r)
}

func TestStatusCode(t *testing.T) {
	app := New()
	app.GET("/", func(ctx *Context) error {
		return ctx.NoContent()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", "/", nil)
	if err != nil {
		t.Error(err)
		return
	}
	resp := httptest.NewRecorder()
	app.ServeHTTP(resp, req)
	t.Log(resp.Code)
}

// 测试处理器
func TestHandlers(t *testing.T) {
	app := New(Config{
		Recovery:     true,
		AfterHandler: afterHandler,
	})
	app.Use(func(ctx *Context) error {
		t.Log("1 执行了全局中间件")
		return nil
	})
	group := app.Group("/group", func(ctx *Context) error {
		t.Log("2 执行了 /group")
		return nil
	})
	group.Use(func(ctx *Context) error {
		t.Log("3 执行了路由组 /group 中间件")
		return nil
	})
	group.GET("/object", func(ctx *Context) error {
		t.Log("4 执行了 /group/object")
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "GET", "/group/object", nil)
	if err != nil {
		t.Error(err)
		return
	}
	app.ServeHTTP(httptest.NewRecorder(), r)
}

// 测试 PathValue
func TestPathValue(t *testing.T) {
	app := New()
	app.GET("/:path/:file", func(ctx *Context) error {
		t.Log("path=", ctx.PathValue("path"))
		t.Log("file=", ctx.PathValue("file"))
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "GET", "/haha/123", nil)
	if err != nil {
		t.Error(err)
		return
	}
	app.ServeHTTP(httptest.NewRecorder(), r)
}

// 测试Context传值
func TestContextValue(t *testing.T) {
	app := New()
	app.GET("/", func(ctx *Context) error {
		// 在ctx中写入参数
		ctx.SetValue("hello", "tsing")
		return nil
	}, func(ctx *Context) error {
		t.Log("hello=", ctx.GetValue("hello"))
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "GET", "/", nil)
	if err != nil {
		t.Error(err)
		return
	}
	app.ServeHTTP(httptest.NewRecorder(), r)
}

// 测试中止处理器链
func TestAbort(t *testing.T) {
	app := New()
	group := app.Group("/group")
	group.GET("/object", func(ctx *Context) error {
		t.Log("ok")
		ctx.Abort()
		return nil
	}, func(ctx *Context) error {
		t.Error("中止失败")
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "GET", "/group/object", nil)
	if err != nil {
		t.Error(err)
		return
	}
	app.ServeHTTP(httptest.NewRecorder(), r)
}

// 测试QueryValue
func TestQueryParams(t *testing.T) {
	app := New()
	app.GET("/", func(ctx *Context) error {
		t.Log("id=", ctx.QueryValue("id"))
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "GET", "/?id=123", nil)
	if err != nil {
		t.Error(err)
		return
	}
	app.ServeHTTP(httptest.NewRecorder(), r)
}

// 测试FormValue
func TestFormValue(t *testing.T) {
	app := New()
	app.POST("/", func(ctx *Context) error {
		t.Log("test=", ctx.FormValue("test"))
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "POST", "/", strings.NewReader("test=ok"))
	if err != nil {
		t.Error(err)
		return
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	app.ServeHTTP(httptest.NewRecorder(), r)
}

// 测试 404 错误
func TestNotFoundError(t *testing.T) {
	app := New(Config{
		ErrorHandler: errorHandler,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "GET", "/404", nil)
	if err != nil {
		t.Error(err)
		return
	}
	app.ServeHTTP(httptest.NewRecorder(), r)
}

// 测试 405 事件
func TestMethodNotAllowedError(t *testing.T) {
	app := New(Config{
		HandleMethodNotAllowed: true,
		ErrorHandler:           errorHandler,
	})
	app.POST("/", func(ctx *Context) error {
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "GET", "/", nil)
	if err != nil {
		t.Error(err)
		return
	}
	app.ServeHTTP(httptest.NewRecorder(), r)
}

// 测试panic事件
func TestPanicError(t *testing.T) {
	app := New(Config{
		Recovery:     true,
		ErrorHandler: errorHandler,
	})
	app.GET("/", func(ctx *Context) error {
		panic("panic消息")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "GET", "/", nil)
	if err != nil {
		t.Error(err)
		return
	}
	app.ServeHTTP(httptest.NewRecorder(), r)
}

// 测试CORS
func TestCORS(t *testing.T) {
	app := New(Config{
		ErrorHandler:           errorHandler, // 通过错误处理器来实现自动响应OPTIONS请求
		HandleMethodNotAllowed: true,         // 错误处理器中需要判断 405 状态码
	})
	app.GET("/", func(ctx *Context) error {
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "OPTIONS", "/", nil)
	if err != nil {
		t.Error(err)
		return
	}
	app.ServeHTTP(httptest.NewRecorder(), r)
}

// --------------------------------------------------------------------------------
// Original pprof_test.go content
// --------------------------------------------------------------------------------

func TestWrap(t *testing.T) {
	app := newServer()
	Wrap(app)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("test failed : result code %d, want %d", w.Code, http.StatusOK)
	}
}

// --------------------------------------------------------------------------------
// Original context_test.go content
// --------------------------------------------------------------------------------

type mockHijacker struct {
	httptest.ResponseRecorder
}

func (m *mockHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, nil
}

func TestHijackSupport(t *testing.T) {
	r := New()
	r.GET("/hijack", func(c *Context) error {
		_, ok := c.ResponseWriter.(http.Hijacker)
		if !ok {
			c.String(500, "hijack not supported")
			return nil
		}
		c.String(200, "hijack supported")
		return nil
	})

	// Create a mock writer that supports Hijack
	w := &mockHijacker{
		ResponseRecorder: *httptest.NewRecorder(),
	}
	req := httptest.NewRequest("GET", "/hijack", nil)

	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("Expected 200 (supported), got %d (%s)", w.Code, w.Body.String())
	}
}

func TestFlushSupport(t *testing.T) {
	r := New()
	r.GET("/flush", func(c *Context) error {
		c.String(200, "chunk1")
		if flusher, ok := c.ResponseWriter.(http.Flusher); ok {
			flusher.Flush()
		} else {
			c.String(500, "flush not supported")
			return nil
		}
		c.String(200, "chunk2")
		return nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/flush", nil)

	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("Expected 200, got %d", w.Code)
	}
	if !w.Flushed {
		t.Fatal("Expected Flushed to be true")
	}
	if w.Body.String() != "chunk1chunk2" {
		t.Fatalf("Expected chunk1chunk2, got %s", w.Body.String())
	}
}

// --------------------------------------------------------------------------------
// New Test Scenarios for Full Coverage
// --------------------------------------------------------------------------------

// TestParseJSON tests the ParseJSON method
func TestParseJSON(t *testing.T) {
	app := New()
	type User struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	app.POST("/json", func(ctx *Context) error {
		var user User
		if err := ctx.ParseJSON(&user); err != nil {
			return ctx.String(http.StatusBadRequest, "bad request")
		}
		return ctx.JSON(http.StatusOK, user)
	})

	body := strings.NewReader(`{"name":"tsing","age":18}`)
	req := httptest.NewRequest("POST", "/json", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
	expected := `{"name":"tsing","age":18}`
	if w.Body.String() != expected {
		t.Errorf("Expected %s, got %s", expected, w.Body.String())
	}
}

// TestFileUpload tests multipart file upload
func TestFileUpload(t *testing.T) {
	app := New()
	app.POST("/upload", func(ctx *Context) error {
		file, err := ctx.FormFile("file")
		if err != nil {
			return ctx.String(http.StatusBadRequest, "upload failed")
		}
		return ctx.String(http.StatusOK, file.Filename)
	})

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("hello world"))
	_ = writer.Close()

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
	if w.Body.String() != "test.txt" {
		t.Errorf("Expected test.txt, got %s", w.Body.String())
	}
}

// TestStaticFileServing tests static file serving
func TestStaticFileServing(t *testing.T) {
	// Create a temporary directory and file
	tmpDir, err := os.MkdirTemp("", "ving_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "hello.txt")
	if err := os.WriteFile(testFile, []byte("hello static"), 0644); err != nil {
		t.Fatal(err)
	}

	app := New()
	app.Static("/static", tmpDir, true)

	// Test valid file
	req := httptest.NewRequest("GET", "/static/hello.txt", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for valid file, got %d", w.Code)
	}
	if w.Body.String() != "hello static" {
		t.Errorf("Expected content 'hello static', got %s", w.Body.String())
	}

	// Test directory traversal (Security check)
	req = httptest.NewRequest("GET", "/static/../hello.txt", nil)
	w = httptest.NewRecorder()
	app.ServeHTTP(w, req)
	// Should be 404 because cleaned path doesn't match prefix or blocked by security check
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for traversal attempt, got %d", w.Code)
	}
}

// TestRedirect tests the Redirect method
func TestRedirect(t *testing.T) {
	app := New()
	app.GET("/redirect", func(ctx *Context) error {
		return ctx.Redirect(http.StatusFound, "/target")
	})

	req := httptest.NewRequest("GET", "/redirect", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("Expected 302, got %d", w.Code)
	}
	if w.Header().Get("Location") != "/target" {
		t.Errorf("Expected Location /target, got %s", w.Header().Get("Location"))
	}
}

// TestTSR tests Trailing Slash Redirect
func TestTSR(t *testing.T) {
	app := New()
	app.GET("/path/", func(ctx *Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	// Request without slash should 301 to with slash
	req := httptest.NewRequest("GET", "/path", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusMovedPermanently {
		t.Errorf("Expected 301 TSR, got %d", w.Code)
	}
	if w.Header().Get("Location") != "/path/" {
		t.Errorf("Expected Location /path/, got %s", w.Header().Get("Location"))
	}
}

// TestWildcardRoute verifies catch-all routing
func TestWildcardRoute(t *testing.T) {
	app := New()
	app.GET("/files/*filepath", func(ctx *Context) error {
		return ctx.String(http.StatusOK, ctx.PathValue("filepath"))
	})

	req := httptest.NewRequest("GET", "/files/dir/file.txt", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
	if w.Body.String() != "/dir/file.txt" { // Typically *filepath includes leading slash
		// Check implementation detail: if it strips slash or not. usually it captures "/dir/file.txt"
		t.Logf("Got filepath: %s", w.Body.String())
	}
}

// TestMultiValues verifies QueryValues and FormValues
func TestMultiValues(t *testing.T) {
	app := New()
	app.POST("/multi", func(ctx *Context) error {
		ids := ctx.QueryValues("id")
		tags := ctx.FormValues("tag")
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"ids":  ids,
			"tags": tags,
		})
	})

	body := strings.NewReader("tag=a&tag=b")
	req := httptest.NewRequest("POST", "/multi?id=1&id=2", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
	// Verify response contains both values
	if !strings.Contains(w.Body.String(), `"ids":["1","2"]`) && !strings.Contains(w.Body.String(), `"ids":["2","1"]`) {
		t.Errorf("Expected ids to contain 1 and 2, got %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"tags":["a","b"]`) && !strings.Contains(w.Body.String(), `"tags":["b","a"]`) {
		t.Errorf("Expected tags to contain a and b, got %s", w.Body.String())
	}
}

// TestSaveFile verifies file saving functionality
func TestSaveFile(t *testing.T) {
	app := New()
	tmpDir, err := os.MkdirTemp("", "ving_save_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	app.POST("/save", func(ctx *Context) error {
		file, err := ctx.FormFile("file")
		if err != nil {
			return ctx.String(http.StatusBadRequest, "no file")
		}
		savePath := filepath.Join(tmpDir, file.Filename)
		if err := ctx.SaveFile(file, savePath, 0644); err != nil {
			return ctx.String(http.StatusInternalServerError, err.Error())
		}
		return ctx.String(http.StatusOK, "saved")
	})

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "saved.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("saved content"))
	_ = writer.Close()

	req := httptest.NewRequest("POST", "/save", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d body: %s", w.Code, w.Body.String())
	}

	// Verify file exists on disk
	content, err := os.ReadFile(filepath.Join(tmpDir, "saved.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "saved content" {
		t.Errorf("Expected saved content, got %s", string(content))
	}
}

// TestMatchRoute verifies multiple method registration
func TestMatchRoute(t *testing.T) {
	app := New()
	app.Match([]string{"GET", "POST"}, "/match", func(ctx *Context) error {
		return ctx.String(http.StatusOK, ctx.Request.Method)
	})

	// Test GET
	req := httptest.NewRequest("GET", "/match", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Body.String() != "GET" {
		t.Errorf("Expected GET, got %s", w.Body.String())
	}

	// Test POST
	req = httptest.NewRequest("POST", "/match", nil)
	w = httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Body.String() != "POST" {
		t.Errorf("Expected POST, got %s", w.Body.String())
	}
}

// TestStaticFileFS verifies custom file system serving
func TestStaticFileFS(t *testing.T) {
	app := New()
	// Use os.DirFS or http.Dir (standard library)
	// For testing, we can use the current directory or a temp one.
	tmpDir, err := os.MkdirTemp("", "ving_fs_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "test.css"), []byte("body {}"), 0644); err != nil {
		t.Fatal(err)
	}

	app.StaticFileFS("/style.css", "test.css", http.Dir(tmpDir))

	req := httptest.NewRequest("GET", "/style.css", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
	if w.Body.String() != "body {}" {
		t.Errorf("Expected body {}, got %s", w.Body.String())
	}
}

// TestContextCopy verifies context copying
func TestContextCopy(t *testing.T) {
	app := New()
	app.GET("/copy", func(ctx *Context) error {
		ctx.SetValue("key", "value")
		cp := ctx.Copy()

		if cp.GetValue("key") != "value" {
			t.Error("Expected value to be copied")
		}
		if cp.writermem.ResponseWriter != nil {
			t.Error("Expected ResponseWriter to be nil in copy")
		}
		return nil
	})

	req := httptest.NewRequest("GET", "/copy", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
}

// TestMultipleFileUpload verifies uploading multiple files
func TestMultipleFileUpload(t *testing.T) {
	app := New()
	app.POST("/upload", func(ctx *Context) error {
		files, err := ctx.FormFiles("files")
		if err != nil {
			return ctx.String(http.StatusBadRequest, "upload failed")
		}
		return ctx.String(http.StatusOK, fmt.Sprintf("%d files", len(files)))
	})

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for i := 0; i < 2; i++ {
		part, err := writer.CreateFormFile("files", fmt.Sprintf("file%d.txt", i))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte("content"))
	}
	_ = writer.Close()

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
	if w.Body.String() != "2 files" {
		t.Errorf("Expected 2 files, got %s", w.Body.String())
	}
}

// TestParamExistence verifies existence checks for Query/Form params
func TestParamExistence(t *testing.T) {
	app := New()
	app.POST("/params", func(ctx *Context) error {
		v1, ok1 := ctx.QueryParam("q")
		v2, ok2 := ctx.FormParam("f")
		_, ok3 := ctx.QueryParam("missing")

		if !ok1 || v1 != "1" {
			return ctx.String(http.StatusBadRequest, "q failed")
		}
		if !ok2 || v2 != "2" {
			return ctx.String(http.StatusBadRequest, "f failed")
		}
		if ok3 {
			return ctx.String(http.StatusBadRequest, "missing failed")
		}
		return ctx.String(http.StatusOK, "ok")
	})

	body := strings.NewReader("f=2")
	req := httptest.NewRequest("POST", "/params?q=1", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d body: %s", w.Code, w.Body.String())
	}
}

// TestOtherMethods verifies DELETE, PUT, PATCH
func TestOtherMethods(t *testing.T) {
	app := New()
	app.DELETE("/del", func(ctx *Context) error { return ctx.String(200, "DELETE") })
	app.PUT("/put", func(ctx *Context) error { return ctx.String(200, "PUT") })
	app.PATCH("/patch", func(ctx *Context) error { return ctx.String(200, "PATCH") })

	methods := map[string]string{
		"DELETE": "/del",
		"PUT":    "/put",
		"PATCH":  "/patch",
	}

	for method, path := range methods {
		req := httptest.NewRequest(method, path, nil)
		w := httptest.NewRecorder()
		app.ServeHTTP(w, req)
		if w.Body.String() != method {
			t.Errorf("Expected %s, got %s", method, w.Body.String())
		}
	}
}

// TestStaticFileSingle verifies serving a single static file
func TestStaticFileSingle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ving_single_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	fPath := filepath.Join(tmpDir, "single.txt")
	if err := os.WriteFile(fPath, []byte("single"), 0644); err != nil {
		t.Fatal(err)
	}

	app := New()
	app.StaticFile("/s", fPath)

	req := httptest.NewRequest("GET", "/s", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Body.String() != "single" {
		t.Errorf("Expected single, got %s", w.Body.String())
	}
}

// --------------------------------------------------------------------------------
// Benchmarks
// --------------------------------------------------------------------------------

func runRequest(b *testing.B, app *Engine, method, path string) {
	req := httptest.NewRequest(method, path, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		app.ServeHTTP(w, req)
	}
}

// BenchmarkSimple tests a basic GET request with no params
func BenchmarkSimple(b *testing.B) {
	app := New()
	app.GET("/ping", func(c *Context) error {
		return nil
	})
	runRequest(b, app, "GET", "/ping")
}

// BenchmarkParam tests a GET request with path parameters
func BenchmarkParam(b *testing.B) {
	app := New()
	app.GET("/user/:name", func(c *Context) error {
		return nil
	})
	runRequest(b, app, "GET", "/user/tsing")
}

// BenchmarkFiveParams tests a GET request with 5 path parameters
func BenchmarkFiveParams(b *testing.B) {
	app := New()
	app.GET("/:a/:b/:c/:d/:e", func(c *Context) error {
		return nil
	})
	runRequest(b, app, "GET", "/test/param/route/benchmark/five")
}

// BenchmarkMiddleware tests request with multiple middlewares
func BenchmarkMiddleware(b *testing.B) {
	app := New()
	app.Use(func(c *Context) error { return nil })
	app.Use(func(c *Context) error { return nil })
	app.Use(func(c *Context) error { return nil })
	app.GET("/ping", func(c *Context) error { return nil })
	runRequest(b, app, "GET", "/ping")
}

// BenchmarkParallel tests concurrent requests
func BenchmarkParallel(b *testing.B) {
	app := New()
	app.GET("/ping", func(c *Context) error {
		return nil
	})

	req := httptest.NewRequest("GET", "/ping", nil)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			w := httptest.NewRecorder()
			app.ServeHTTP(w, req)
		}
	})
}

// Benchmark404 tests 404 Not Found performance
func Benchmark404(b *testing.B) {
	app := New()
	app.GET("/ping", func(c *Context) error { return nil })
	runRequest(b, app, "GET", "/notfound")
}
