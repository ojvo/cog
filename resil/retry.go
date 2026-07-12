package resil

import (
	"context"
	"math"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

// RetryConfig 保持基础配置
type RetryConfig struct {
	MaxRetries    int             // 最大重试次数
	RetryDelay    time.Duration   // 初始重试间隔时间
	RetryMaxDelay time.Duration   // 最大重试间隔时间
	RetryJitter   float64         // 重试抖动因子(0-1)
	Context       context.Context // 上下文控制
}

// HTTPRetryConfig HTTP特定重试配置
type HTTPRetryConfig struct {
	*RetryConfig            // 嵌入基础配置
	RetryOn        []int    // 哪些状态码需要重试
	RetryOnMethods []string // 哪些HTTP方法可以重试
}

// NewRetryConfig 创建默认的基础重试配置
func NewRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:    3,                // 默认最多重试3次
		RetryDelay:    1 * time.Second,  // 默认初始重试间隔1秒
		RetryMaxDelay: 30 * time.Second, // 默认最大重试间隔30秒
		RetryJitter:   0.2,              // 默认20%的抖动
		Context:       context.Background(),
	}
}

// NewHTTPRetryConfig 创建默认的HTTP重试配置
func NewHTTPRetryConfig() *HTTPRetryConfig {
	return &HTTPRetryConfig{
		RetryConfig:    NewRetryConfig(),
		RetryOn:        []int{408, 429, 500, 502, 503, 504},                                              // 默认重试的状态码
		RetryOnMethods: []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodDelete}, // 默认可重试的方法
	}
}

// 基础配置方法
func (rc *RetryConfig) WithMaxRetries(maxRetries int) *RetryConfig {
	rc.MaxRetries = maxRetries
	return rc
}

func (rc *RetryConfig) WithRetryDelay(delay time.Duration) *RetryConfig {
	rc.RetryDelay = delay
	return rc
}

func (rc *RetryConfig) WithRetryMaxDelay(maxDelay time.Duration) *RetryConfig {
	rc.RetryMaxDelay = maxDelay
	return rc
}

func (rc *RetryConfig) WithRetryJitter(jitter float64) *RetryConfig {
	rc.RetryJitter = jitter
	return rc
}

func (rc *RetryConfig) WithContext(ctx context.Context) *RetryConfig {
	rc.Context = ctx
	return rc
}

// HTTP配置方法
func (hrc *HTTPRetryConfig) WithRetryOn(statusCodes ...int) *HTTPRetryConfig {
	hrc.RetryOn = statusCodes
	return hrc
}

func (hrc *HTTPRetryConfig) WithRetryOnMethods(methods ...string) *HTTPRetryConfig {
	hrc.RetryOnMethods = methods
	return hrc
}

// WithMaxRetries HTTP配置的最大重试次数设置
func (hrc *HTTPRetryConfig) WithMaxRetries(maxRetries int) *HTTPRetryConfig {
	hrc.RetryConfig.MaxRetries = maxRetries
	return hrc
}

// WithRetryDelay HTTP配置的重试延迟设置
func (hrc *HTTPRetryConfig) WithRetryDelay(delay time.Duration) *HTTPRetryConfig {
	hrc.RetryConfig.RetryDelay = delay
	return hrc
}

// WithRetryMaxDelay HTTP配置的最大重试延迟设置
func (hrc *HTTPRetryConfig) WithRetryMaxDelay(maxDelay time.Duration) *HTTPRetryConfig {
	hrc.RetryConfig.RetryMaxDelay = maxDelay
	return hrc
}

// WithRetryJitter HTTP配置的重试抖动设置
func (hrc *HTTPRetryConfig) WithRetryJitter(jitter float64) *HTTPRetryConfig {
	hrc.RetryConfig.RetryJitter = jitter
	return hrc
}

// WithContext HTTP配置的上下文设置
func (hrc *HTTPRetryConfig) WithContext(ctx context.Context) *HTTPRetryConfig {
	hrc.RetryConfig.Context = ctx
	return hrc
}

// CalculateRetryDelay 计算重试延迟时间
func CalculateRetryDelay(attempt int, config *RetryConfig) time.Duration {
	// 指数退避算法: baseDelay * 2^attempt
	delay := config.RetryDelay * time.Duration(math.Pow(2, float64(attempt)))

	// 添加随机抖动
	if config.RetryJitter > 0 {
		jitterAmount := float64(delay) * config.RetryJitter
		delay = delay + time.Duration(rand.Float64()*jitterAmount)
	}

	// 确保不超过最大延迟
	if delay > config.RetryMaxDelay {
		delay = config.RetryMaxDelay
	}

	return delay
}

// 执行通用重试
func Retry(operation func() error, config *RetryConfig) error {
	var err error

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		// 检查上下文是否已取消
		if config.Context != nil && config.Context.Err() != nil {
			return config.Context.Err()
		}

		err = operation()

		// 如果成功，直接返回
		if err == nil {
			return nil
		}

		// 如果已达到最大重试次数，返回最后一次错误
		if attempt >= config.MaxRetries {
			return err
		}

		// 计算延迟时间
		delay := CalculateRetryDelay(attempt, config)

		// 等待下一次重试
		if config.Context != nil {
			select {
			case <-config.Context.Done():
				return config.Context.Err()
			case <-time.After(delay):
				// 继续下一次重试
			}
		} else {
			time.Sleep(delay)
		}
	}

	return err
}

// ShouldRetryHTTP 判断HTTP请求是否应该重试
func ShouldRetryHTTP(method string, statusCode int, attempt int, config *HTTPRetryConfig) bool {
	// 检查是否达到最大重试次数
	if attempt >= config.MaxRetries {
		return false
	}

	// 检查HTTP方法是否可以重试
	methodAllowed := false
	if len(config.RetryOnMethods) == 0 {
		// 默认只允许GET、HEAD、OPTIONS方法重试
		methodAllowed = method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
	} else {
		for _, m := range config.RetryOnMethods {
			if strings.EqualFold(method, m) {
				methodAllowed = true
				break
			}
		}
	}

	if !methodAllowed {
		return false
	}

	// 检查状态码是否需要重试
	if len(config.RetryOn) == 0 {
		// 默认重试5xx错误
		return statusCode >= 500 && statusCode < 600
	}

	for _, code := range config.RetryOn {
		if statusCode == code {
			return true
		}
	}

	return false
}

// 执行HTTP请求重试
func RetryHTTP(method string, operation func() (int, error), config *HTTPRetryConfig) error {
	var statusCode int
	var err error

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		// 检查上下文是否已取消
		if config.Context != nil && config.Context.Err() != nil {
			return config.Context.Err()
		}

		statusCode, err = operation()

		// 如果成功或不需要重试，直接返回
		if err == nil && (statusCode < 400 || !ShouldRetryHTTP(method, statusCode, attempt, config)) {
			return nil
		}

		// 如果已达到最大重试次数，返回最后一次错误
		if attempt >= config.MaxRetries {
			if err != nil {
				return err
			}
			return nil
		}

		// 计算延迟时间
		delay := CalculateRetryDelay(attempt, config.RetryConfig)

		// 等待下一次重试
		if config.Context != nil {
			select {
			case <-config.Context.Done():
				return config.Context.Err()
			case <-time.After(delay):
				// 继续下一次重试
			}
		} else {
			time.Sleep(delay)
		}
	}

	return err
}
