package log

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultTSFormat = "06-01-02 15:04:05.000"
)

const (
	DebugLevel Level = iota + 1
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
)

type Level int32

// OverflowPolicy defines how to handle queue overflow in async mode
type OverflowPolicy int

const (
	BlockUntilDrained OverflowPolicy = iota // Block until buffer has space (default, safe but slow)
	DropNewest                              // Discard incoming message (prevents blocking)
	DropOldest                              // Remove oldest message to make space (FIFO)
)

type Fields map[string]any

type LogOpts struct {
	Level          Level
	EnableColor    bool
	EnableCaller   bool
	LogFile        string
	EnableConsole  bool
	Async          bool           // 是否开启异步写入
	MaxSize        int64          // 单个日志文件最大大小（单位：MB）
	MaxBackups     int            // 最大保留旧日志文件数
	MaxQueueSize   int            // 异步模式下活动缓冲区最大字节数 (OOM 保护)
	OverflowPolicy OverflowPolicy // 缓冲区满时的处理策略
	Streams        []StreamOpts   // 多输出流配置
}

type StreamOpts struct {
	Type       string // "file", "console"
	Level      Level
	File       string
	MaxSize    int64
	MaxBackups int
	Color      bool
}

// Buffer pool instead of strings.Builder pool for direct writing
type buffer []byte

func (b *buffer) Write(p []byte) (int, error) {
	*b = append(*b, p...)
	return len(p), nil
}

func (b *buffer) WriteString(s string) (int, error) {
	*b = append(*b, s...)
	return len(s), nil
}

func (b *buffer) WriteByte(c byte) error {
	*b = append(*b, c)
	return nil
}

var bufferPool = sync.Pool{
	New: func() interface{} {
		b := make(buffer, 0, 512) // 增加初始容量
		return &b
	},
}

// rotator implements io.WriteCloser with size-based rotation
type rotator struct {
	filename   string
	maxSize    int64
	maxBackups int
	size       int64
	file       *os.File
	mu         sync.Mutex
}

func (r *rotator) Write(p []byte) (n int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	n = len(p)
	if r.size+int64(n) > r.maxSize {
		if err := r.rotate(); err != nil {
			// 如果轮转失败，尝试继续写入当前文件，不中断日志
			fmt.Fprintf(os.Stderr, "log rotate error: %v\n", err)
		}
	}

	_, err = r.file.Write(p)
	if err == nil {
		r.size += int64(n)
	}
	return n, err
}

func (r *rotator) rotate() error {
	if r.file != nil {
		r.file.Close()
	}

	// 简单的备份逻辑：file.log -> file.log.1 -> file.log.2 ...
	for i := r.maxBackups; i > 0; i-- {
		source := r.filename
		if i > 1 {
			source = fmt.Sprintf("%s.%d", r.filename, i-1)
		}
		dest := fmt.Sprintf("%s.%d", r.filename, i)
		if _, err := os.Stat(source); err == nil {
			os.Rename(source, dest)
		}
	}

	// 重新创建新文件
	f, err := os.OpenFile(r.filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	r.file = f
	r.size = 0
	return nil
}

func (r *rotator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

// 简化的线程安全写入器
type safeWriter struct {
	mu             sync.Mutex
	w              io.Writer
	async          bool
	active         *buffer    // 当前活动缓冲区
	free           *buffer    // 备用缓冲区
	cond           *sync.Cond // 用于通知 worker
	closed         atomic.Bool
	wg             sync.WaitGroup
	maxQueueSize   int            // 最大缓冲区大小 (字节)
	overflowPolicy OverflowPolicy // 溢出策略
	droppedCount   int64          // 统计丢弃的消息数
}

func (sw *safeWriter) Write(p []byte) (int, error) {
	if sw.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	if !sw.async {
		sw.mu.Lock()
		defer sw.mu.Unlock()
		return sw.w.Write(p)
	}

	sw.mu.Lock()
	defer sw.mu.Unlock()

	// Handle queue overflow based on policy
	for sw.maxQueueSize > 0 && len(*sw.active) > sw.maxQueueSize && !sw.closed.Load() {
		switch sw.overflowPolicy {
		case DropNewest:
			// Discard the incoming message
			sw.droppedCount++
			return len(p), nil
		case DropOldest:
			// Remove oldest data (simple truncation from start, not ideal but safe)
			if len(*sw.active) > len(p) {
				*sw.active = (*sw.active)[len(p):]
			} else {
				*sw.active = (*sw.active)[:0]
			}
			break
		case BlockUntilDrained:
			// Default: block until space is available
			fallthrough
		default:
			sw.cond.Wait()
		}
	}

	if sw.closed.Load() {
		return 0, io.ErrClosedPipe
	}

	*sw.active = append(*sw.active, p...)
	if len(*sw.active) > 32*1024 {
		sw.cond.Broadcast()
	}
	return len(p), nil
}

func (sw *safeWriter) startWorker() {
	sw.wg.Add(1)
	go func() {
		defer sw.wg.Done()
		for {
			sw.mu.Lock()
			// 等待数据或关闭信号
			for len(*sw.active) == 0 && !sw.closed.Load() {
				sw.cond.Wait()
			}

			// 如果已关闭且活动缓冲区为空，退出
			if len(*sw.active) == 0 && sw.closed.Load() {
				sw.mu.Unlock()
				return
			}

			// 交换缓冲区
			sw.active, sw.free = sw.free, sw.active
			sw.cond.Broadcast() // 通知生产者，现在有空间了
			sw.mu.Unlock()

			// 批量写入
			if len(*sw.free) > 0 {
				sw.w.Write(*sw.free)
				*sw.free = (*sw.free)[:0] // 清空备用缓冲区
			}
		}
	}()
}

type Logger struct {
	streams []*stream
	level   atomic.Int32
	color   bool
	caller  bool
	closer  io.Closer // 用于关闭所有流
	fields  Fields    // 结构化日志字段
	opts    LogOpts   // 记录当前的配置
}

type stream struct {
	sw    *safeWriter
	level Level
}

// 预定义的颜色和标签
var levelInfo = [...]struct {
	colorPrefix, label, plainLabel string
}{
	{"", " [D]", " [D]"},                // Debug (索引 0)
	{"", " [I]", " [I]"},                // Info (索引 1)
	{"\033[33m", " [W]\033[0m", " [W]"}, // Warn (索引 2)
	{"\033[31m", " [E]\033[0m", " [E]"}, // Error (索引 3)
	{"\033[31m", " [F]\033[0m", " [F]"}, // Fatal (索引 4)
}

var (
	defaultLogger atomic.Pointer[Logger]
)

func init() {
	l := mustNewLogger(LogOpts{
		Level:         InfoLevel,
		EnableConsole: true,
		EnableColor:   true,
	})
	defaultLogger.Store(l)
}

// Init re-initializes the default global logger.
func Init(opts LogOpts) error {
	l, err := NewLogger(opts)
	if err != nil {
		return err
	}

	old := defaultLogger.Swap(l)
	if old != nil {
		old.Close()
	}
	return nil
}

// 全局函数 - Lock-free access to defaultLogger
func Debug(msg string)                  { defaultLogger.Load().log(DebugLevel, msg, nil) }
func Debugf(format string, args ...any) { defaultLogger.Load().log(DebugLevel, format, args) }
func Info(msg string)                   { defaultLogger.Load().log(InfoLevel, msg, nil) }
func Infof(format string, args ...any)  { defaultLogger.Load().log(InfoLevel, format, args) }
func Warn(msg string)                   { defaultLogger.Load().log(WarnLevel, msg, nil) }
func Warnf(format string, args ...any)  { defaultLogger.Load().log(WarnLevel, format, args) }
func Error(msg string)                  { defaultLogger.Load().log(ErrorLevel, msg, nil) }
func Errorf(format string, args ...any) { defaultLogger.Load().log(ErrorLevel, format, args) }
func Fatal(msg string) {
	defaultLogger.Load().log(FatalLevel, msg, nil)
	os.Exit(1)
}
func Fatalf(format string, args ...any) {
	defaultLogger.Load().log(FatalLevel, format, args)
	os.Exit(1)
}

// 必须成功创建日志器（用于默认日志器）
func mustNewLogger(opts LogOpts) *Logger {
	logger, err := NewLogger(opts)
	if err != nil {
		// 回退到最简单的配置
		sw := &safeWriter{w: os.Stderr}
		l := &Logger{
			streams: []*stream{{sw: sw, level: opts.Level}},
			color:   false,
			caller:  false,
		}
		l.level.Store(int32(opts.Level))
		return l
	}
	return logger
}

type handler struct {
	w     io.Writer
	level Level
}

func NewLogger(opts LogOpts) (*Logger, error) {
	if opts.Level == 0 {
		opts.Level = InfoLevel
	}

	if opts.Async && opts.MaxQueueSize <= 0 {
		opts.MaxQueueSize = 10 * 1024 * 1024 // 默认 10MB 缓冲区
	}

	l := &Logger{
		color:  opts.EnableColor,
		caller: opts.EnableCaller,
		opts:   opts,
	}
	l.level.Store(int32(opts.Level))

	// Backward compatibility: handle LogFile/EnableConsole
	if opts.LogFile != "" || opts.EnableConsole {
		if opts.LogFile != "" {
			found := false
			for _, s := range opts.Streams {
				if s.Type == "file" && s.File == opts.LogFile {
					found = true
					break
				}
			}
			if !found {
				opts.Streams = append(opts.Streams, StreamOpts{
					Type:       "file",
					File:       opts.LogFile,
					Level:      opts.Level,
					MaxSize:    opts.MaxSize,
					MaxBackups: opts.MaxBackups,
				})
			}
		}
		if opts.EnableConsole {
			found := false
			for _, s := range opts.Streams {
				if s.Type == "console" {
					found = true
					break
				}
			}
			if !found {
				opts.Streams = append(opts.Streams, StreamOpts{
					Type:  "console",
					Level: opts.Level,
					Color: opts.EnableColor,
				})
			}
		}
	}

	var closers []io.Closer
	for _, s := range opts.Streams {
		var w io.Writer
		if s.Type == "file" && s.File != "" {
			cleanPath := filepath.Clean(s.File)
			absPath, _ := filepath.Abs(cleanPath)
			s.File = absPath
			_ = os.MkdirAll(filepath.Dir(s.File), 0755)

			r, err := newRotator(s.File, s.MaxSize, s.MaxBackups)
			if err == nil {
				w = r
				closers = append(closers, r)
			}
		} else if s.Type == "console" {
			w = os.Stdout
		}

		if w != nil {
			if s.Level == 0 {
				s.Level = opts.Level
			}
			sw := &safeWriter{
				w:              w,
				async:          opts.Async,
				maxQueueSize:   opts.MaxQueueSize,
				overflowPolicy: opts.OverflowPolicy,
			}
			if opts.Async {
				sw.cond = sync.NewCond(&sw.mu)
				b1 := make(buffer, 0, 64*1024)
				b2 := make(buffer, 0, 64*1024)
				sw.active = &b1
				sw.free = &b2
				sw.startWorker()

				// 定时刷新
				go func(s *safeWriter) {
					ticker := time.NewTicker(500 * time.Millisecond)
					defer ticker.Stop()
					for range ticker.C {
						if s.closed.Load() {
							return
						}
						s.mu.Lock()
						if len(*s.active) > 0 {
							s.cond.Broadcast()
						}
						s.mu.Unlock()
					}
				}(sw)
			}
			l.streams = append(l.streams, &stream{sw: sw, level: s.Level})
		}
	}

	if len(l.streams) == 0 {
		// 默认输出到控制台
		sw := &safeWriter{w: os.Stdout, async: opts.Async, maxQueueSize: opts.MaxQueueSize}
		if opts.Async {
			sw.cond = sync.NewCond(&sw.mu)
			b1 := make(buffer, 0, 64*1024)
			b2 := make(buffer, 0, 64*1024)
			sw.active = &b1
			sw.free = &b2
			sw.startWorker()
		}
		l.streams = append(l.streams, &stream{sw: sw, level: opts.Level})
	}

	l.closer = &multiCloser{closers}
	return l, nil
}

type multiCloser struct {
	closers []io.Closer
}

func (m *multiCloser) Close() error {
	var errs []string
	for _, c := range m.closers {
		if err := c.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("closer errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

func newRotator(filename string, maxSizeMB int64, maxBackups int) (*rotator, error) {
	if maxSizeMB <= 0 {
		maxSizeMB = 100 // 默认 100MB
	}
	if maxBackups <= 0 {
		maxBackups = 10 // 默认保留 10 个备份
	}
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	fi, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}

	return &rotator{
		filename:   filename,
		maxSize:    maxSizeMB * 1024 * 1024,
		maxBackups: maxBackups,
		file:       file,
		size:       fi.Size(),
	}, nil
}

func (l *Logger) Close() error {
	for _, s := range l.streams {
		if s.sw != nil {
			if s.sw.closed.Swap(true) {
				continue
			}
			if s.sw.async {
				s.sw.mu.Lock()
				s.sw.cond.Broadcast()
				s.sw.mu.Unlock()
				s.sw.wg.Wait()
			}
		}
	}
	if l.closer != nil {
		return l.closer.Close()
	}
	return nil
}

// 统一的日志方法
func (l *Logger) Debug(format string, args ...any) { l.log(DebugLevel, format, args) }
func (l *Logger) Info(format string, args ...any)  { l.log(InfoLevel, format, args) }
func (l *Logger) Warn(format string, args ...any)  { l.log(WarnLevel, format, args) }
func (l *Logger) Error(format string, args ...any) { l.log(ErrorLevel, format, args) }
func (l *Logger) Fatal(format string, args ...any) { l.log(FatalLevel, format, args); os.Exit(1) }

// 核心日志方法
func (l *Logger) log(level Level, format string, args []any) {
	if int32(level) < l.level.Load() {
		return
	}

	// 从池中获取缓冲区
	bufPtr := bufferPool.Get().(*buffer)

	// 构建日志消息
	l.buildMessage(bufPtr, level, format, args)

	// 分发到各个流
	for _, s := range l.streams {
		if level >= s.level {
			s.sw.Write(*bufPtr)
		}
	}

	// 释放临时缓冲区
	*bufPtr = (*bufPtr)[:0]
	bufferPool.Put(bufPtr)
}

// Flush flushes any buffered log messages.
func (l *Logger) Flush() {
	for _, s := range l.streams {
		if s.sw != nil && s.sw.async {
			s.sw.mu.Lock()
			if len(*s.sw.active) > 0 {
				s.sw.cond.Signal()
			}
			s.sw.mu.Unlock()
		}
	}
}

// Flush flushes the default logger.
func Flush() {
	if l := defaultLogger.Load(); l != nil {
		l.Flush()
	}
}

// 构建日志消息
func (l *Logger) buildMessage(buf *buffer, level Level, format string, args []any) {
	// 时间戳
	now := time.Now()
	// 使用自定义格式化减少内存分配
	*buf = now.AppendFormat(*buf, defaultTSFormat)

	// 级别标签
	idx := int(level) - 1
	if idx < 0 || idx >= len(levelInfo) {
		idx = int(InfoLevel) - 1 // 默认为 Info 级别
	}

	if l.color {
		buf.WriteString(levelInfo[idx].colorPrefix)
		buf.WriteString(levelInfo[idx].label)
	} else {
		buf.WriteString(levelInfo[idx].plainLabel)
	}

	// 消息内容
	buf.WriteByte(' ')
	if len(args) == 0 {
		buf.WriteString(format)
	} else {
		// 优化：对简单参数使用更快的格式化
		l.formatMessage(buf, format, args)
	}

	// 结构化字段
	if len(l.fields) > 0 {
		buf.WriteString(" |")
		for k, v := range l.fields {
			buf.WriteByte(' ')
			buf.WriteString(k)
			buf.WriteByte('=')
			switch val := v.(type) {
			case string:
				buf.WriteString(val)
			case int:
				*buf = strconv.AppendInt(*buf, int64(val), 10)
			case int64:
				*buf = strconv.AppendInt(*buf, val, 10)
			case bool:
				*buf = strconv.AppendBool(*buf, val)
			default:
				fmt.Fprint(buf, v)
			}
		}
	}

	// 调用者信息
	if l.caller {
		// 动态确定 caller skip
		// 我们尝试通过一次 Caller(4) 来跳过大部分框架层
		// skip=3 是 log.Info -> Logger.log -> Logger.buildMessage -> runtime.Caller
		// 如果是通过 log.Info 这种全局函数调用的，skip 应该是 4
		skip := 3
		_, file, line, ok := runtime.Caller(skip)
		if ok {
			// 如果发现还在 log.go 或 cor.go 中，再向上跳一层
			if strings.HasSuffix(file, "log.go") || strings.HasSuffix(file, "cor.go") {
				skip++
				if _, f, l, ok2 := runtime.Caller(skip); ok2 {
					file, line = f, l
					// 再检查一次 cor.go (如果是 cor.Info -> log.Info -> ...)
					if strings.HasSuffix(file, "cor.go") {
						skip++
						if _, f3, l3, ok3 := runtime.Caller(skip); ok3 {
							file, line = f3, l3
						}
					}
				}
			}
			buf.WriteString(" @")
			buf.WriteString(filepath.Base(file))
			buf.WriteByte(':')
			*buf = strconv.AppendInt(*buf, int64(line), 10)
		}
	}

	buf.WriteByte('\n')
}

func (l *Logger) formatMessage(buf *buffer, format string, args []any) {
	// 针对没有参数的情况已经在 buildMessage 处理了
	// 这里处理带参数的情况

	// 简单实现：只针对简单的 %s, %d, %v 进行基础替换优化
	// 实际生产环境中可以使用更成熟的模板引擎或手写解析器
	// 这里为了展示 0 拷贝和高性能，我们只对几个常用场景做优化，其余回退到 fmt.Fprintf

	if len(args) == 1 {
		switch v := args[0].(type) {
		case string:
			// 处理简单的 "msg: %s"
			l.formatSingle(buf, format, v)
			return
		case int:
			l.formatSingleInt(buf, format, int64(v))
			return
		case int64:
			l.formatSingleInt(buf, format, v)
			return
		}
	}

	fmt.Fprintf(buf, format, args...)
}

func (l *Logger) formatSingle(buf *buffer, format string, arg string) {
	idx := strings.Index(format, "%s")
	if idx == -1 {
		fmt.Fprintf(buf, format, arg)
		return
	}
	// Check if there are other placeholders
	if strings.Index(format[idx+2:], "%") != -1 {
		fmt.Fprintf(buf, format, arg)
		return
	}
	buf.WriteString(format[:idx])
	buf.WriteString(arg)
	buf.WriteString(format[idx+2:])
}

func (l *Logger) formatSingleInt(buf *buffer, format string, arg int64) {
	idx := strings.Index(format, "%d")
	if idx == -1 {
		fmt.Fprintf(buf, format, arg)
		return
	}
	// Check if there are other placeholders
	if strings.Index(format[idx+2:], "%") != -1 {
		fmt.Fprintf(buf, format, arg)
		return
	}
	buf.WriteString(format[:idx])
	*buf = strconv.AppendInt(*buf, arg, 10)
	buf.WriteString(format[idx+2:])
}

func (l *Logger) Set(level Level) {
	l.level.Store(int32(level))
}

func (l *Logger) WithFields(fields Fields) *Logger {
	newLogger := &Logger{
		streams: l.streams,
		color:   l.color,
		caller:  l.caller,
		closer:  nil, // 子 Logger 不负责关闭主文件
		fields:  make(Fields, len(l.fields)+len(fields)),
	}
	newLogger.level.Store(l.level.Load())
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}
	for k, v := range fields {
		newLogger.fields[k] = v
	}
	return newLogger
}

// 便民函数
func DefaultLogger() *Logger { return defaultLogger.Load() }

// Close closes the default logger.
func Close() error {
	if l := defaultLogger.Load(); l != nil {
		return l.Close()
	}
	return nil
}

// SetLevel sets the default logger's level.
func SetLevel(level Level) {
	if l := defaultLogger.Load(); l != nil {
		l.level.Store(int32(level))
	}
}
