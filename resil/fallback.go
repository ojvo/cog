package resil

import (
	"sync"
)

// FallbackConfig 配置连续失败后切换到备用操作的降级策略。
//
// Fallback 与 Retry 正交：
//   - Retry 在同一资源上重试瞬时错误
//   - Fallback 切换到备用资源，应对持续错误
//
// 二者可组合：将 Retry 包在 Fallback 外层，先切换资源再重试。
type FallbackConfig struct {
	// Threshold 连续失败达此数后切换到 fallback。默认 3。
	Threshold int
	// IsFailure 判断错误是否计入失败。默认：所有非 nil 错误都计入。
	// 返回 false 的错误（如 context.Canceled）不计数也不触发切换。
	IsFailure func(error) bool
}

// NewFallbackConfig 创建默认的 fallback 配置。
func NewFallbackConfig() *FallbackConfig {
	return &FallbackConfig{
		Threshold: 3,
		IsFailure: func(err error) bool { return err != nil },
	}
}

// WithThreshold 设置触发切换的连续失败次数。
func (c *FallbackConfig) WithThreshold(n int) *FallbackConfig {
	c.Threshold = n
	return c
}

// WithIsFailure 设置失败判定谓词。
func (c *FallbackConfig) WithIsFailure(fn func(error) bool) *FallbackConfig {
	c.IsFailure = fn
	return c
}

// FallbackExecutor 有状态的 fallback 执行器，跨调用追踪连续失败计数。
//
// 语义（与 wuu FallbackClient 一致）：
//   - 成功 → failures=0, using=false，恢复 primary
//   - 失败且 IsFailure(err)=true → failures++，达 Threshold 后 using=true
//   - 失败且 IsFailure(err)=false → 不计数（如取消、非故障错误）
//   - 已 using 时保持 using，直到某次调用成功后恢复 primary
//
// 当 fallback 为 nil 时退化为直接调用 primary（no-op 兼容）。
type FallbackExecutor struct {
	primary  func() error
	fallback func() error
	config   *FallbackConfig

	mu       sync.Mutex
	failures int
	using    bool
}

// NewFallbackExecutor 创建 fallback 执行器。
// config 为 nil 时使用默认配置；fallback 为 nil 时退化为直接调用 primary。
func NewFallbackExecutor(primary, fallback func() error, config *FallbackConfig) *FallbackExecutor {
	if config == nil {
		config = NewFallbackConfig()
	}
	if config.Threshold <= 0 {
		config.Threshold = 3
	}
	if config.IsFailure == nil {
		config.IsFailure = func(err error) bool { return err != nil }
	}
	return &FallbackExecutor{
		primary:  primary,
		fallback: fallback,
		config:   config,
	}
}

// Execute 执行当前激活的操作（primary 或 fallback），并更新内部状态。
func (e *FallbackExecutor) Execute() error {
	fn := e.current()
	err := fn()
	e.record(err)
	return err
}

// IsUsingFallback 返回当前是否处于 fallback 模式。
func (e *FallbackExecutor) IsUsingFallback() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.using
}

// Reset 清除失败计数和 fallback 状态，下次 Execute 重新使用 primary。
func (e *FallbackExecutor) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.failures = 0
	e.using = false
}

// current 返回当前应执行的操作。fallback 为 nil 或未切换时返回 primary。
func (e *FallbackExecutor) current() func() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.using && e.fallback != nil {
		return e.fallback
	}
	return e.primary
}

// record 根据调用结果更新内部状态。
func (e *FallbackExecutor) record(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err == nil {
		e.failures = 0
		e.using = false
		return
	}
	if !e.config.IsFailure(err) {
		return
	}
	if e.using {
		return
	}
	e.failures++
	// 无 fallback 可切换时不进入 using 状态，避免无意义的标记。
	if e.fallback != nil && e.failures >= e.config.Threshold {
		e.using = true
	}
}
