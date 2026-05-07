package cor

import (
	"ojv/cog/cfg"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreFacade(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cor_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	confFile := filepath.Join(tmpDir, "test.cfg")
	confContent := `
app.name = TestApp
[log]
    level = debug
    console = true
[server]
    port = 9000
`
	if err := os.WriteFile(confFile, []byte(confContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Test Init
	if err := Init(confFile); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer Close()

	// Test Config Accessors
	if GetString("app.name") != "TestApp" {
		t.Errorf("Expected TestApp, got %s", GetString("app.name"))
	}
	if GetInt("server.port") != 9000 {
		t.Errorf("Expected 9000, got %d", GetInt("server.port"))
	}

	// Test ConfigInstance
	conf := ConfigInstance()
	if conf == nil {
		t.Fatal("ConfigInstance returned nil")
	}

	// Test Schema Validation via Facade
	schema := cfg.Schema{
		"app.name":    {Type: "string", Required: true},
		"server.port": {Type: "int", Required: true},
	}
	if err := conf.Validate(schema); err != nil {
		t.Errorf("Validation failed: %v", err)
	}

	// Test environment variable expansion in log path
	os.Setenv("TEST_LOG_DIR", tmpDir)
	confContentEnv := `
[log]
    file = ${TEST_LOG_DIR}/env_test.log
`
	confFileEnv := filepath.Join(tmpDir, "test_env.cfg")
	os.WriteFile(confFileEnv, []byte(confContentEnv), 0644)
	if err := Init(confFileEnv); err != nil {
		t.Fatalf("Init with env failed: %v", err)
	}
	Info("test env log")
	Close()

	if _, err := os.Stat(filepath.Join(tmpDir, "env_test.log")); os.IsNotExist(err) {
		t.Error("Log file with env var not created")
	}

	// Test Fallback (invalid path)
	confContentFallback := `
[log]
    file = /invalid/path/that/cannot/be/created/app.log
`
	confFileFallback := filepath.Join(tmpDir, "test_fallback.cfg")
	os.WriteFile(confFileFallback, []byte(confContentFallback), 0644)
	if err := Init(confFileFallback); err != nil {
		t.Fatalf("Init with fallback failed: %v", err)
	}
	Info("this should go to stdout due to fallback")
	Close()

	// Test Logging via Facade (just ensure no panic)
	Debug("test debug")
	Infof("test info %s", "formatted")
	Warn("test warn")
	Errorf("test error %d", 500)

	// Test Multi-stream log config
	confContentMulti := `
[log]
    async = false
[[log.streams]]
    type = console
    level = info
[[log.streams]]
    type = file
    file = ${TEST_LOG_DIR}/multi_info.log
    level = info
[[log.streams]]
    type = file
    file = ${TEST_LOG_DIR}/multi_error.log
    level = error
`
	confFileMulti := filepath.Join(tmpDir, "test_multi.cfg")
	os.WriteFile(confFileMulti, []byte(confContentMulti), 0644)
	if err := Init(confFileMulti); err != nil {
		t.Fatalf("Init with multi-stream failed: %v", err)
	}
	Info("this is info message")
	Error("this is error message")
	Close()

	infoContent, _ := os.ReadFile(filepath.Join(tmpDir, "multi_info.log"))
	errorContent, _ := os.ReadFile(filepath.Join(tmpDir, "multi_error.log"))

	if !contains(string(infoContent), "this is info message") {
		t.Error("info log missing info message")
	}
	if !contains(string(infoContent), "this is error message") {
		t.Error("info log missing error message")
	}
	if contains(string(errorContent), "this is info message") {
		t.Error("error log should not contain info message")
	}
	if !contains(string(errorContent), "this is error message") {
		t.Error("error log missing error message")
	}
}

func TestLogInitialization(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cor_log_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// 1. 测试 log.path 兼容性
	confFile := filepath.Join(tmpDir, "test_path.cfg")
	logFile := filepath.Join(tmpDir, "path_test.log")
	confContent := `
[log]
    path = ` + strings.ReplaceAll(logFile, "\\", "/") + `
    console = false
`
	if err := os.WriteFile(confFile, []byte(confContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Init(confFile); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	Info("test log path message")
	Close()

	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Error("Log file using 'path' key not created")
	} else {
		content, _ := os.ReadFile(logFile)
		if !strings.Contains(string(content), "test log path message") {
			t.Error("Log file content incorrect")
		}
	}

	// 2. 测试 log.streams 中 path 的兼容性
	confFileMulti := filepath.Join(tmpDir, "test_multi_path.cfg")
	logFileMulti := filepath.Join(tmpDir, "multi_path_test.log")
	confContentMulti := `
[[log.streams]]
    type = file
    path = ` + strings.ReplaceAll(logFileMulti, "\\", "/") + `
`
	if err := os.WriteFile(confFileMulti, []byte(confContentMulti), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Init(confFileMulti); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	Info("test multi log path message")
	Close()

	if _, err := os.Stat(logFileMulti); os.IsNotExist(err) {
		t.Error("Log file using 'path' in streams not created")
	} else {
		content, _ := os.ReadFile(logFileMulti)
		if !strings.Contains(string(content), "test multi log path message") {
			t.Error("Multi log file content incorrect")
		}
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestUninitializedCore(t *testing.T) {
	// Reset state
	Close()

	// Should not panic and return defaults
	if GetString("any.key") != "" {
		t.Errorf("Expected empty string for uninitialized core")
	}
	if GetInt("any.port") != 0 {
		t.Errorf("Expected 0 for uninitialized core")
	}

	// ConfigInstance should return a valid empty config
	if ConfigInstance() == nil {
		t.Error("ConfigInstance should not return nil even if uninitialized")
	}
}

func TestCore_Concurrency(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "cor_concurrency")
	defer os.RemoveAll(tmpDir)
	confFile := filepath.Join(tmpDir, "bench.cfg")
	os.WriteFile(confFile, []byte("key = initial\n"), 0644)

	Init(confFile)
	defer Close()

	const goroutines = 50
	const iterations = 1000
	done := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < iterations; j++ {
				_ = GetString("key")
			}
			done <- true
		}()
	}

	for i := 0; i < goroutines; i++ {
		<-done
	}
}

func BenchmarkCore_LockFreeGet(b *testing.B) {
	tmpDir, _ := os.MkdirTemp("", "cor_bench")
	defer os.RemoveAll(tmpDir)
	confFile := filepath.Join(tmpDir, "bench.cfg")
	os.WriteFile(confFile, []byte("key = value\n"), 0644)

	Init(confFile)
	defer Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = GetString("key")
		}
	})
}
