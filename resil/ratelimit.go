package resil

import (
	"errors"
	"math"
	"sync"
	"time"
)

// RateLimitConfig 限流配置
type RateLimitConfig struct {
	RateLimit      int           // 每秒最大请求数
	RateBurst      int           // 突发请求数量
	WaitTimeout    time.Duration // 等待超时时间
}

// NewRateLimitConfig 创建默认的限流配置
func NewRateLimitConfig() *RateLimitConfig {
	return &RateLimitConfig{
		RateLimit:   10,               // 默认每秒10个请求
		RateBurst:   20,               // 默认突发20个请求
		WaitTimeout: 30 * time.Second, // 默认等待超时30秒
	}
}

// WithRateLimit 设置每秒请求数限制
func (rlc *RateLimitConfig) WithRateLimit(limit int) *RateLimitConfig {
	rlc.RateLimit = limit
	return rlc
}

// WithRateBurst 设置突发请求数量
func (rlc *RateLimitConfig) WithRateBurst(burst int) *RateLimitConfig {
	rlc.RateBurst = burst
	return rlc
}

// WithWaitTimeout 设置等待超时时间
func (rlc *RateLimitConfig) WithWaitTimeout(timeout time.Duration) *RateLimitConfig {
	rlc.WaitTimeout = timeout
	return rlc
}

// RateLimiter 限流器结构
type RateLimiter struct {
	limit       int           // 每秒请求数限制
	burst       int           // 允许的突发请求数
	tokens      float64       // 当前可用令牌数
	lastRefill  time.Time     // 上次补充令牌的时间
	mu          sync.Mutex    // 互斥锁
	waitTimeout time.Duration // 等待超时时间
}

// NewRateLimiter 创建新的限流器
func NewRateLimiter(config *RateLimitConfig) *RateLimiter {
	if config == nil {
		config = NewRateLimitConfig()
	}
	
	return &RateLimiter{
		limit:       config.RateLimit,
		burst:       config.RateBurst,
		tokens:      float64(config.RateBurst),
		lastRefill:  time.Now(),
		waitTimeout: config.WaitTimeout,
	}
}

// Allow 尝试获取令牌
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill).Seconds()
	rl.lastRefill = now
	
	// 根据经过的时间补充令牌
	rl.tokens = math.Min(float64(rl.burst), rl.tokens+float64(rl.limit)*elapsed)
	
	if rl.tokens < 1 {
		return false
	}
	
	rl.tokens--
	return true
}

// Wait 等待直到获取到令牌或超时
func (rl *RateLimiter) Wait() error {
	if rl.limit <= 0 {
		return errors.New("限流器配置无效: rate limit <= 0")
	}

	deadline := time.Now().Add(rl.waitTimeout)

	for {
		rl.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(rl.lastRefill).Seconds()
		rl.lastRefill = now

		// 根据经过的时间补充令牌
		rl.tokens = math.Min(float64(rl.burst), rl.tokens+float64(rl.limit)*elapsed)

		if rl.tokens >= 1 {
			rl.tokens--
			rl.mu.Unlock()
			return nil
		}

		// 计算需要等待的时间
		missing := 1.0 - rl.tokens
		waitDuration := time.Duration(missing / float64(rl.limit) * float64(time.Second))
		rl.mu.Unlock()

		if time.Now().Add(waitDuration).After(deadline) {
			return errors.New("等待令牌超时")
		}

		if waitDuration < time.Millisecond {
			waitDuration = time.Millisecond
		}

		time.Sleep(waitDuration)
	}
}

// 全局限流器
var (
	globalLimiter     *RateLimiter
	globalLimiterOnce sync.Once
)

// GetGlobalRateLimiter 获取全局限流器
func GetGlobalRateLimiter(config *RateLimitConfig) *RateLimiter {
	globalLimiterOnce.Do(func() {
		globalLimiter = NewRateLimiter(config)
	})
	return globalLimiter
}