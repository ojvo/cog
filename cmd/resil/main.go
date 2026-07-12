package main

import (
	"errors"
	"fmt"
	"time"

	"ojv/cog/resil"
)

// This demo shows Retry + Backoff + HTTPRetry combination:
//   - Retry: generic retry with exponential backoff + jitter
//   - Backoff: stateful backoff iterator for fine-grained control
//   - RetryHTTP: HTTP-specific retry with method/status filtering
//   - Builder-style config via With* options
func main() {
	fmt.Println("=== Cog/Resil Integration Demo ===")
	fmt.Println("Retry + Backoff + HTTPRetry")
	fmt.Println()

	// 1. Generic Retry with builder-style config
	cfg := resil.NewRetryConfig().
		WithMaxRetries(3).
		WithRetryDelay(10*time.Millisecond).
		WithRetryMaxDelay(100*time.Millisecond).
		WithRetryJitter(0.2)

	attempts := 0
	err := resil.Retry(func() error {
		attempts++
		if attempts < 3 {
			return fmt.Errorf("transient error (attempt %d)", attempts)
		}
		return nil
	}, cfg)
	fmt.Printf("[Retry]      attempts=%d err=%v\n", attempts, err)

	// 2. Stateful Backoff iterator
	bo := resil.NewBackOff(
		resil.WithMinDelay(1*time.Millisecond),
		resil.WithMaxDelay(50*time.Millisecond),
		resil.WithFactor(2),
		resil.WithJitterFlag(true),
	)
	fmt.Print("[Backoff]    delays:")
	for i := 0; i < 5; i++ {
		fmt.Printf(" %v", bo.Duration())
	}
	fmt.Printf(" (attempts=%d)\n", bo.Attempts())
	bo.Reset()

	// 3. HTTPRetry with method + status code filtering
	httpCfg := resil.NewHTTPRetryConfig().
		WithMaxRetries(2).
		WithRetryDelay(5*time.Millisecond).
		WithRetryOn(500, 502, 503).
		WithRetryOnMethods("GET", "POST")

	// Simulate an HTTP call that returns 503 twice then 200
	calls := 0
	statusCodes := []int{503, 503, 200}
	err = resil.RetryHTTP("GET", func() (int, error) {
		code := statusCodes[calls]
		calls++
		return code, nil
	}, httpCfg)
	fmt.Printf("[RetryHTTP]  calls=%d finalStatus=%d err=%v\n", calls, statusCodes[calls-1], err)

	// 4. Non-retryable method (PUT not in allowed list)
	calls = 0
	err = resil.RetryHTTP("PUT", func() (int, error) {
		calls++
		return 500, nil
	}, httpCfg)
	fmt.Printf("[RetryHTTP]  PUT non-retryable: calls=%d err=%v\n", calls, err)

	// 5. Context cancellation short-circuits retry
	fmt.Println("[Retry]      context cancellation demo skipped (see tests)")
	_ = errors.New("placeholder")
	fmt.Println()
	fmt.Println("Demo completed successfully.")
}
