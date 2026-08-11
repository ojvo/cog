package cor

import (
	"context"
	"fmt"
	"ojv/cog/cfg"
	"ojv/cog/log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
)

var (
	defaultKernel = &Kernel{}
)

// Kernel represents the Cog application kernel.
// It manages component lifecycle and provides signal-based graceful shutdown.
type Kernel struct {
	mu           sync.RWMutex
	globalConfig atomic.Pointer[cfg.Config]
	components   []Component

	baseCtx       context.Context
	baseCtxCancel context.CancelFunc
	interrupt     chan os.Signal
	initOnce      sync.Once // guards one-time initialization of baseCtx + interrupt
	onceExit      sync.Once
	exited        atomic.Bool
}

// Component defines the interface for Cog components.
type Component interface {
	Name() string
	Init(c *cfg.Config) error
	Close() error
}

// Init initializes the default kernel.
func Init(configFile string) error {
	return defaultKernel.Init(configFile)
}

// Init initializes the kernel with a config file.
func (k *Kernel) Init(configFile string) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	// Set built-in env vars for use in config
	if exe, err := os.Executable(); err == nil {
		os.Setenv("EXE_DIR", filepath.Dir(exe))
	}
	if absConfig, err := filepath.Abs(configFile); err == nil {
		os.Setenv("CONF_DIR", filepath.Dir(absConfig))
	}

	c, err := cfg.Load(configFile)
	if err != nil {
		return err
	}
	c = c.ExpandEnv()
	k.globalConfig.Store(c)

	// Always ensure log component is first if not already present
	foundLog := false
	for _, comp := range k.components {
		if comp.Name() == "log" {
			foundLog = true
			break
		}
	}
	if !foundLog {
		k.components = append([]Component{&logComponent{}}, k.components...)
	}

	// Initialize components
	for _, comp := range k.components {
		if err := comp.Init(c); err != nil {
			return fmt.Errorf("failed to init component %s: %w", comp.Name(), err)
		}
	}

	return nil
}

// RegisterComponent registers a component to the default kernel.
func RegisterComponent(comp Component) {
	defaultKernel.RegisterComponent(comp)
}

// RegisterComponent registers a component to the kernel.
func (k *Kernel) RegisterComponent(comp Component) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.components = append(k.components, comp)
}

type logComponent struct{}

func (l *logComponent) Name() string { return "log" }

func (l *logComponent) Init(c *cfg.Config) error {
	logOpts := log.LogOpts{
		Level:         log.InfoLevel,
		EnableConsole: c.BoolOr("log.console", true),
		EnableColor:   c.BoolOr("log.color", true),
	}

	// Helper to parse level string
	parseLevel := func(s string, defaultLevel log.Level) log.Level {
		switch strings.ToLower(s) {
		case "debug":
			return log.DebugLevel
		case "info":
			return log.InfoLevel
		case "warn":
			return log.WarnLevel
		case "error":
			return log.ErrorLevel
		case "fatal":
			return log.FatalLevel
		}
		return defaultLevel
	}

	// 优先尝试绑定多输出流配置
	if streams, ok := c.Slice("log.streams"); ok {
		for _, s := range streams {
			var opts log.StreamOpts
			if s.Bind("", &opts) {
				// Manually handle level if it's a string in config
				if levelStr := s.StringOr("level", ""); levelStr != "" {
					opts.Level = parseLevel(levelStr, logOpts.Level)
				}
				// 兼容 file 和 path 字段
				if opts.File == "" {
					opts.File = s.StringOr("path", "")
				}
				logOpts.Streams = append(logOpts.Streams, opts)
			}
		}
	}

	// 如果没有多流配置，尝试回退到单文件配置
	if len(logOpts.Streams) == 0 {
		if levelStr := c.StringOr("log.level", ""); levelStr != "" {
			logOpts.Level = parseLevel(levelStr, log.InfoLevel)
		}

		// 兼容 log.file 和 log.path
		logFile := c.StringOr("log.path", "")
		if logFile == "" {
			logFile = c.StringOr("log.file", "")
		}
		logOpts.LogFile = logFile
		logOpts.EnableCaller = c.BoolOr("log.caller", false)
		logOpts.Async = c.BoolOr("log.async", false)
		logOpts.MaxSize = int64(c.IntOr("log.max_size", 0))
		logOpts.MaxBackups = c.IntOr("log.max_backups", 0)
		logOpts.MaxQueueSize = c.IntOr("log.max_queue_size", 0)
	} else {
		// 即使有多流配置，全局开关依然有效
		logOpts.EnableCaller = c.BoolOr("log.caller", false)
		logOpts.Async = c.BoolOr("log.async", false)
		logOpts.MaxQueueSize = c.IntOr("log.max_queue_size", 0)
		if levelStr := c.StringOr("log.level", ""); levelStr != "" {
			logOpts.Level = parseLevel(levelStr, logOpts.Level)
		}
	}

	return log.Init(logOpts)
}

func (l *logComponent) Close() error {
	log.Flush()
	return log.Close()
}

func (k *Kernel) config() *cfg.Config {
	c := k.globalConfig.Load()
	if c == nil {
		return cfg.NewConfig()
	}
	return c
}

// ConfigInstance returns the default kernel's config.
func ConfigInstance() *cfg.Config {
	return defaultKernel.config()
}

// Config Accessors
func GetString(key string) string               { return defaultKernel.config().StringOr(key, "") }
func GetInt(key string) int                     { return defaultKernel.config().IntOr(key, 0) }
func GetBool(key string) bool                   { return defaultKernel.config().BoolOr(key, false) }
func GetFloat64(key string) float64             { return defaultKernel.config().Float64Or(key, 0) }
func GetSection(key string) (*cfg.Config, bool) { return defaultKernel.config().Section(key) }
func GetSlice(key string) ([]*cfg.Config, bool) { return defaultKernel.config().Slice(key) }
func Bind(key string, target any) bool          { return defaultKernel.config().Bind(key, target) }

// Log Accessors
func Debug(msg string)                  { log.Debug(msg) }
func Debugf(format string, args ...any) { log.Debugf(format, args...) }
func Info(msg string)                   { log.Info(msg) }
func Infof(format string, args ...any)  { log.Infof(format, args...) }
func Warn(msg string)                   { log.Warn(msg) }
func Warnf(format string, args ...any)  { log.Warnf(format, args...) }
func Error(msg string)                  { log.Error(msg) }
func Errorf(format string, args ...any) { log.Errorf(format, args...) }
func Fatal(msg string)                  { log.Fatal(msg) }
func Fatalf(format string, args ...any) { log.Fatalf(format, args...) }

// SetLogLevel sets the default logger's level.
func SetLogLevel(level log.Level) {
	log.SetLevel(level)
}

// ---------------------------------------------------------------------------
// Lifecycle: signal-based graceful shutdown
// ---------------------------------------------------------------------------

// ensureBaseContext initializes the base context and interrupt channel exactly
// once. Safe for concurrent use; subsequent calls are no-ops. The initOnce
// guard prevents a race where parallel Run/Exit/BaseContext callers would
// otherwise each create their own baseCtx/interrupt, leaking the first set
// and replacing signal.Notify registrations.
func (k *Kernel) ensureBaseContext() {
	k.initOnce.Do(func() {
		k.baseCtx, k.baseCtxCancel = context.WithCancel(context.Background())
		k.interrupt = make(chan os.Signal, 1)
	})
}

// Run blocks until an OS signal (SIGINT, SIGTERM) or Exit() is called,
// then performs graceful shutdown of all components.
// Must be called after Init.
func Run() { defaultKernel.Run() }

// Run blocks until an OS signal (SIGINT, SIGTERM) or Exit() is called,
// then performs graceful shutdown of all components.
func (k *Kernel) Run() {
	if k.exited.Load() {
		return
	}
	k.ensureBaseContext()
	// Exit() may have raced between the exited.Load() check above and
	// ensureBaseContext(); re-check to avoid blocking on an already-cancelled
	// context or registering signal.Notify after Exit cleaned up.
	if k.exited.Load() {
		return
	}

	signal.Notify(k.interrupt, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-k.interrupt:
	case <-k.baseCtx.Done():
	}

	k.onceExit.Do(func() {
		k.exited.Store(true)
		// Cancel base context first so any goroutines watching BaseContext can clean up
		k.baseCtxCancel()
		signal.Stop(k.interrupt)
		_ = k.Close()
	})
}

// Exit triggers graceful shutdown programmatically.
// Safe to call multiple times; only the first call takes effect.
func Exit() { defaultKernel.Exit() }

// Exit triggers graceful shutdown. Safe to call from any goroutine.
func (k *Kernel) Exit() {
	if k.exited.Load() {
		return
	}
	// Ensure baseCtxCancel/interrupt are initialized even if Exit is called
	// before Run/BaseContext. This allows the onceExit.Do block below to
	// safely call baseCtxCancel() and signal.Stop() without nil checks.
	k.ensureBaseContext()
	k.onceExit.Do(func() {
		k.exited.Store(true)
		k.baseCtxCancel()
		signal.Stop(k.interrupt)
		_ = k.Close()
	})
}

// BaseContext returns a context that is cancelled before components are
// closed during shutdown. Long-running goroutines should watch this context
// to clean up before the full shutdown sequence.
func BaseContext() context.Context { return defaultKernel.BaseContext() }

// BaseContext returns the shutdown context.
func (k *Kernel) BaseContext() context.Context {
	k.ensureBaseContext()
	return k.baseCtx
}

// Close closes the default kernel.
func Close() error {
	return defaultKernel.Close()
}

// Close closes all components in reverse order.
func (k *Kernel) Close() error {
	k.mu.Lock()
	defer k.mu.Unlock()

	var errs []string
	for i := len(k.components) - 1; i >= 0; i-- {
		if err := k.components[i].Close(); err != nil {
			errs = append(errs, fmt.Errorf("component %s close error: %w", k.components[i].Name(), err).Error())
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// NewLogger creates a new logger instance via the facade.
func NewLogger(opts log.LogOpts) (*log.Logger, error) {
	return log.NewLogger(opts)
}
