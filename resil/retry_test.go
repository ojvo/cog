package resil

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"c.n/ojv/cog/util"
)

func TestRetry_Success(t *testing.T) {
	as := util.NewAssert(t)
	config := NewRetryConfig().
		WithMaxRetries(3).
		WithRetryDelay(1 * time.Millisecond)

	count := 0
	err := Retry(func() error {
		count++
		if count < 2 {
			return errors.New("fail")
		}
		return nil
	}, config)

	as.TtNoError(err)
	as.Equal(2, count)
}

func TestRetry_MaxRetries(t *testing.T) {
	as := util.NewAssert(t)
	config := NewRetryConfig().
		WithMaxRetries(2).
		WithRetryDelay(1 * time.Millisecond)

	count := 0
	err := Retry(func() error {
		count++
		return errors.New("fail")
	}, config)

	as.Error(err)
	as.Equal(3, count) // 1 initial + 2 retries
}

func TestRetry_ContextCancel(t *testing.T) {
	as := util.NewAssert(t)
	ctx, cancel := context.WithCancel(context.Background())
	config := NewRetryConfig().
		WithContext(ctx).
		WithRetryDelay(100 * time.Millisecond)

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := Retry(func() error {
		return errors.New("fail")
	}, config)

	as.Error(err)
	as.True(errors.Is(err, context.Canceled))
}

func TestRetryHTTP(t *testing.T) {
	as := util.NewAssert(t)
	config := NewHTTPRetryConfig().
		WithMaxRetries(3).
		WithRetryDelay(1 * time.Millisecond)

	// Case 1: Success 200
	err := RetryHTTP(http.MethodGet, func() (int, error) {
		return 200, nil
	}, config)
	as.TtNoError(err)

	// Case 2: 500 Retry -> Success
	count := 0
	err = RetryHTTP(http.MethodGet, func() (int, error) {
		count++
		if count < 2 {
			return 500, nil
		}
		return 200, nil
	}, config)
	as.TtNoError(err)
	as.Equal(2, count)

	// Case 3: 404 No Retry
	count = 0
	err = RetryHTTP(http.MethodGet, func() (int, error) {
		count++
		return 404, nil
	}, config)
	as.TtNoError(err)
	as.Equal(1, count)
}

func TestRetryHTTP_Methods(t *testing.T) {
	as := util.NewAssert(t)
	config := NewHTTPRetryConfig().
		WithRetryOnMethods("POST")

	count := 0
	err := RetryHTTP("GET", func() (int, error) {
		count++
		return 500, nil
	}, config)
	// Should not retry because GET is not in allowed methods
	as.TtNoError(err)
	as.Equal(1, count)
}
