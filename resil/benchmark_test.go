package resil

import (
	"testing"
	"time"
)

func BenchmarkCalculateRetryDelay(b *testing.B) {
	config := NewRetryConfig()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CalculateRetryDelay(i%5, config)
	}
}

func BenchmarkShouldRetryHTTP(b *testing.B) {
	config := NewHTTPRetryConfig()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ShouldRetryHTTP("GET", 500, i%3, config)
	}
}

func BenchmarkBackoff_Duration(b *testing.B) {
	bo := NewBackOff(
		WithMinDelay(1*time.Millisecond),
		WithMaxDelay(1*time.Second),
		WithFactor(2),
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bo.Duration()
		if bo.Attempts() > 20 {
			bo.Reset()
		}
	}
}

func BenchmarkRetry(b *testing.B) {
	config := &RetryConfig{
		MaxRetries:    3,
		RetryDelay:    1 * time.Microsecond,
		RetryMaxDelay: 10 * time.Microsecond,
		RetryJitter:   0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Retry(func() error { return nil }, config)
	}
}
