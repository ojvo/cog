package log

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLogger_Basic(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "log_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	logFile := filepath.Join(tmpDir, "test.log")
	logger, err := NewLogger(LogOpts{
		Level:        DebugLevel,
		LogFile:      logFile,
		EnableCaller: true,
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	logger.Debug("debug msg")
	logger.Info("info msg")

	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	strContent := string(content)

	if !strings.Contains(strContent, "[D] debug msg") {
		t.Errorf("Expected debug msg, got %s", strContent)
	}
	if !strings.Contains(strContent, "[I] info msg") {
		t.Errorf("Expected info msg, got %s", strContent)
	}
	if !strings.Contains(strContent, "log_test.go") {
		t.Error("Expected caller info")
	}
}

func TestLogger_Async(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "log_async_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	logFile := filepath.Join(tmpDir, "async.log")
	logger, err := NewLogger(LogOpts{
		Level:   InfoLevel,
		LogFile: logFile,
		Async:   true,
	})
	if err != nil {
		t.Fatalf("Failed to create async logger: %v", err)
	}

	logger.Info("async msg 1")
	logger.Info("async msg 2")
	
	// Close should wait a bit and flush
	logger.Close()

	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	strContent := string(content)

	if !strings.Contains(strContent, "async msg 1") {
		t.Error("Missing async msg 1")
	}
	if !strings.Contains(strContent, "async msg 2") {
		t.Error("Missing async msg 2")
	}
}

func TestLogger_Levels(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "log_level_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	logFile := filepath.Join(tmpDir, "level.log")
	logger, err := NewLogger(LogOpts{
		Level:   WarnLevel,
		LogFile: logFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	logger.Debug("debug")
	logger.Info("info")
	logger.Warn("warn")

	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	strContent := string(content)

	if strings.Contains(strContent, "debug") {
		t.Error("Should not contain debug")
	}
	if strings.Contains(strContent, "info") {
		t.Error("Should not contain info")
	}
	if !strings.Contains(strContent, "warn") {
		t.Error("Should contain warn")
	}
}

func TestLogger_WithFields(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "log_fields_test")
	defer os.RemoveAll(tmpDir)

	logFile := filepath.Join(tmpDir, "fields.log")
	logger, _ := NewLogger(LogOpts{
		Level:   InfoLevel,
		LogFile: logFile,
	})
	defer logger.Close()

	l2 := logger.WithFields(Fields{"user_id": 123, "trace_id": "abc"})
	l2.Info("test message")

	content, _ := os.ReadFile(logFile)
	strContent := string(content)

	if !strings.Contains(strContent, "user_id=123") {
		t.Error("Missing user_id field")
	}
	if !strings.Contains(strContent, "trace_id=abc") {
		t.Error("Missing trace_id field")
	}
}

func TestLogger_Rotation(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "log_rotation_test")
	defer os.RemoveAll(tmpDir)

	logFile := filepath.Join(tmpDir, "rotation.log")
	// 设置极小的 MaxSize 以触发滚动
	logger, _ := NewLogger(LogOpts{
		Level:      InfoLevel,
		LogFile:    logFile,
		MaxSize:    1, // 1MB, 但我们会写入超过这个大小
		MaxBackups: 2,
	})

	// 模拟写入大量数据
	largeMsg := strings.Repeat("A", 1024*100) // 100KB
	for i := 0; i < 15; i++ {
		logger.Info(largeMsg)
	}
	logger.Close()

	// 检查是否有备份文件
	if _, err := os.Stat(logFile + ".1"); err != nil {
		t.Error("Rotation file .1 should exist")
	}
}

func TestLogger_RotationLimit(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "log_rotation_limit_test")
	defer os.RemoveAll(tmpDir)

	logFile := filepath.Join(tmpDir, "limit.log")
	maxBackups := 3
	logger, _ := NewLogger(LogOpts{
		Level:      InfoLevel,
		LogFile:    logFile,
		MaxSize:    1, // 1MB
		MaxBackups: maxBackups,
	})

	// 写入足够多的数据以产生超过 maxBackups 的文件
	largeMsg := strings.Repeat("B", 1024*1024) // 1MB
	for i := 0; i < 10; i++ {
		logger.Info(largeMsg)
	}
	logger.Close()

	// 检查备份文件数量是否符合预期
	for i := 1; i <= maxBackups; i++ {
		if _, err := os.Stat(fmt.Sprintf("%s.%d", logFile, i)); err != nil {
			t.Errorf("Backup file .%d should exist", i)
		}
	}
	// 检查多出的备份文件是否被清理（当前实现的 rotator 逻辑中尚未包含自动清理超额备份的代码，需要后续改进）
	if _, err := os.Stat(fmt.Sprintf("%s.%d", logFile, maxBackups+1)); err == nil {
		t.Errorf("Backup file .%d should NOT exist", maxBackups+1)
	}
}

func TestLogger_Concurrency(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "log_concurrency_test")
	defer os.RemoveAll(tmpDir)

	logFile := filepath.Join(tmpDir, "concurrency.log")
	logger, _ := NewLogger(LogOpts{
		Level:   InfoLevel,
		LogFile: logFile,
		Async:   true,
	})
	defer logger.Close()

	const goroutines = 20
	const logsPerGoroutine = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < logsPerGoroutine; j++ {
				logger.WithFields(Fields{"gid": id, "idx": j}).Info("concurrent message")
			}
		}(i)
	}
	wg.Wait()
}

func TestLogger_MultiWriter(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "log_multi_test")
	defer os.RemoveAll(tmpDir)

	logFile := filepath.Join(tmpDir, "multi.log")
	logger, _ := NewLogger(LogOpts{
		Level: InfoLevel,
		LogFile: logFile,
		EnableConsole: true,
	})
	defer logger.Close()

	logger.Info("multi writer message")

	// 验证文件写入
	content, _ := os.ReadFile(logFile)
	if !strings.Contains(string(content), "multi writer message") {
		t.Error("Message missing from log file")
	}
}

func BenchmarkLogger_Sync(b *testing.B) {
	tmpDir, _ := os.MkdirTemp("", "benchmark")
	defer os.RemoveAll(tmpDir)
	logFile := filepath.Join(tmpDir, "sync.log")
	logger, _ := NewLogger(LogOpts{
		Level:   InfoLevel,
		LogFile: logFile,
	})
	defer logger.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark sync msg")
	}
}

func BenchmarkLogger_Async(b *testing.B) {
	tmpDir, _ := os.MkdirTemp("", "benchmark")
	defer os.RemoveAll(tmpDir)
	logFile := filepath.Join(tmpDir, "async.log")
	logger, _ := NewLogger(LogOpts{
		Level:   InfoLevel,
		LogFile: logFile,
		Async:   true,
	})
	defer logger.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark async msg")
	}
}

func BenchmarkLogger_Format(b *testing.B) {
	tmpDir, _ := os.MkdirTemp("", "benchmark")
	defer os.RemoveAll(tmpDir)
	logFile := filepath.Join(tmpDir, "format.log")
	logger, _ := NewLogger(LogOpts{
		Level:   InfoLevel,
		LogFile: logFile,
	})
	defer logger.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark format msg: %s", "hello")
	}
}

func BenchmarkLogger_WithFields(b *testing.B) {
	tmpDir, _ := os.MkdirTemp("", "benchmark")
	defer os.RemoveAll(tmpDir)
	logFile := filepath.Join(tmpDir, "fields.log")
	logger, _ := NewLogger(LogOpts{
		Level:   InfoLevel,
		LogFile: logFile,
	})
	defer logger.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.WithFields(Fields{
			"user_id":  10086,
			"trace_id": "abc-def-ghi",
			"action":   "login",
		}).Info("user login success")
	}
}

func BenchmarkLogger_Caller(b *testing.B) {
	tmpDir, _ := os.MkdirTemp("", "benchmark")
	defer os.RemoveAll(tmpDir)
	logFile := filepath.Join(tmpDir, "caller.log")
	
	b.Run("Disabled", func(b *testing.B) {
		logger, _ := NewLogger(LogOpts{
			Level:   InfoLevel,
			LogFile: logFile,
			EnableCaller: false,
		})
		defer logger.Close()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Info("no caller info")
		}
	})

	b.Run("Enabled", func(b *testing.B) {
		logger, _ := NewLogger(LogOpts{
			Level:   InfoLevel,
			LogFile: logFile,
			EnableCaller: true,
		})
		defer logger.Close()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Info("with caller info")
		}
	})
}

func BenchmarkLogger_LevelCheck(b *testing.B) {
	// 测试在不同日志级别设置下，被过滤掉的日志性能（原子操作 vs 锁）
	logger, _ := NewLogger(LogOpts{
		Level: WarnLevel, // 设置为 Warn，Debug 应该被过滤
	})
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Debug("this should be filtered fast")
	}
}

func TestLogger_MultiStreamComplex(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "log_multi_complex")
	defer os.RemoveAll(tmpDir)

	file1 := filepath.Join(tmpDir, "stream1.log")
	file2 := filepath.Join(tmpDir, "stream2.log")

	logger, _ := NewLogger(LogOpts{
		Streams: []StreamOpts{
			{Type: "file", File: file1, Level: InfoLevel},
			{Type: "file", File: file2, Level: ErrorLevel},
		},
	})
	defer logger.Close()

	logger.Info("info to stream1 only")
	logger.Error("error to both streams")

	c1, _ := os.ReadFile(file1)
	c2, _ := os.ReadFile(file2)

	if !strings.Contains(string(c1), "info to stream1 only") || !strings.Contains(string(c1), "error to both streams") {
		t.Error("stream1 missing messages")
	}
	if strings.Contains(string(c2), "info to stream1 only") || !strings.Contains(string(c2), "error to both streams") {
		t.Error("stream2 messages incorrect")
	}
}

func BenchmarkLogger_WithFields_Complex(b *testing.B) {
	tmpDir, _ := os.MkdirTemp("", "bench_fields")
	defer os.RemoveAll(tmpDir)
	logFile := filepath.Join(tmpDir, "bench.log")
	logger, _ := NewLogger(LogOpts{
		Level:   InfoLevel,
		LogFile: logFile,
	})
	defer logger.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.WithFields(Fields{
			"str": "abc",
			"int": 123,
			"bool": true,
			"float": 1.23,
		}).Info("msg")
	}
}

func BenchmarkLogger_AsyncQueueFull(b *testing.B) {
	tmpDir, _ := os.MkdirTemp("", "bench_async_full")
	defer os.RemoveAll(tmpDir)
	logFile := filepath.Join(tmpDir, "async.log")
	// 使用较小的队列来模拟写满
	logger, _ := NewLogger(LogOpts{
		Level:   InfoLevel,
		LogFile: logFile,
		Async:   true,
	})
	defer logger.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("bench msg")
	}
}
