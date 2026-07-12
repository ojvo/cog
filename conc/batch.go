package conc

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// BatchHandler processes a single item of type T.
type BatchHandler[T any] interface {
	Name() string
	Handle(ctx context.Context, item T) error
}

// BatchHandlerFunc adapts a function to BatchHandler.
type BatchHandlerFunc[T any] struct {
	name string
	fn   func(ctx context.Context, item T) error
}

// NewBatchHandler creates a BatchHandler from a name and function.
func NewBatchHandler[T any](name string, fn func(ctx context.Context, item T) error) *BatchHandlerFunc[T] {
	return &BatchHandlerFunc[T]{name: name, fn: fn}
}

func (h *BatchHandlerFunc[T]) Name() string { return h.name }

func (h *BatchHandlerFunc[T]) Handle(ctx context.Context, item T) error {
	return h.fn(ctx, item)
}

// BatchResult holds the outcome of processing a single item.
type BatchResult[T any] struct {
	Item     T
	Error    error
	Retries  int
	Elapsed  time.Duration
	Panicked any
}

// BatchConfig controls batch execution behavior.
type BatchConfig struct {
	Limit            int           // max concurrency, <=0 means auto
	MaxAutoLimit     int           // cap for auto limit, <=0 uses 4*GOMAXPROCS
	RateLimit        float64       // QPS limit, <=0 means no limit
	Retry            int           // max retry attempts
	RetryDelay       time.Duration // base retry delay (exponential backoff)
	ShowProgress     bool          // print progress bar
	ProgressInterval time.Duration // progress refresh interval
	ProgressWriter   io.Writer     // progress output, default os.Stdout
}

// DefaultBatchConfig returns a sensible default config.
func DefaultBatchConfig() *BatchConfig {
	return &BatchConfig{
		Limit:            10,
		Retry:            3,
		RetryDelay:       time.Second,
		ShowProgress:     false,
		ProgressInterval: 500 * time.Millisecond,
		ProgressWriter:   os.Stdout,
	}
}

// BatchStats holds execution statistics.
type BatchStats struct {
	Total     int64
	Completed int64
	Success   int64
	Failed    int64
	Elapsed   time.Duration
}

func (s BatchStats) String() string {
	rate := 0.0
	if s.Total > 0 {
		rate = float64(s.Success) / float64(s.Total) * 100
	}
	return fmt.Sprintf("total:%d completed:%d success:%d failed:%d rate:%.2f%% elapsed:%s",
		s.Total, s.Completed, s.Success, s.Failed, rate, formatBatchDuration(s.Elapsed))
}

// BatchRunner processes a slice of items concurrently with retry,
// rate limiting, progress reporting, and result collection.
type BatchRunner[T any] struct {
	items   []T
	handler BatchHandler[T]
	config  *BatchConfig

	OnSuccess func(item T)
	OnError   func(item T, err error)

	total     int64
	completed int64
	failed    int64
	startTime time.Time

	results   []BatchResult[T]
	resultsMu sync.Mutex
}

// NewBatchRunner creates a batch runner.
func NewBatchRunner[T any](items []T, handler BatchHandler[T], config *BatchConfig) *BatchRunner[T] {
	if config == nil {
		config = DefaultBatchConfig()
	} else {
		// Copy to avoid mutating caller's config
		c := *config
		config = &c
	}
	if config.Limit <= 0 {
		autoLimit := len(items)
		autoCap := config.MaxAutoLimit
		if autoCap <= 0 {
			autoCap = runtime.GOMAXPROCS(0) * 4
		}
		if autoCap <= 0 {
			autoCap = 1
		}
		if autoLimit > autoCap {
			autoLimit = autoCap
		}
		if autoLimit <= 0 {
			autoLimit = 1
		}
		config.Limit = autoLimit
	}
	if config.Limit > len(items) && len(items) > 0 {
		config.Limit = len(items)
	}
	if config.Limit <= 0 {
		config.Limit = 1
	}
	if config.ProgressWriter == nil {
		config.ProgressWriter = os.Stdout
	}

	return &BatchRunner[T]{
		items:   items,
		handler: handler,
		config:  config,
		total:   int64(len(items)),
		results: make([]BatchResult[T], 0, len(items)),
	}
}

// Run executes all items and collects results.
func (r *BatchRunner[T]) Run(ctx context.Context) error {
	return r.RunStream(ctx, func(result BatchResult[T]) {
		r.resultsMu.Lock()
		defer r.resultsMu.Unlock()
		r.results = append(r.results, result)
	})
}

// RunStream executes items and calls onResult for each completed item.
func (r *BatchRunner[T]) RunStream(ctx context.Context, onResult func(BatchResult[T])) error {
	if len(r.items) == 0 {
		return nil
	}

	r.startTime = time.Now()
	semaphore := make(chan struct{}, r.config.Limit)

	var ticker *time.Ticker
	if r.config.RateLimit > 0 {
		interval := time.Duration(float64(time.Second) / r.config.RateLimit)
		ticker = time.NewTicker(interval)
		defer ticker.Stop()
	}

	var wg sync.WaitGroup
	progressCtx, cancelProgress := context.WithCancel(ctx)
	// Ensure cancelProgress runs BEFORE wg.Wait() so the progress goroutine
	// can observe cancellation and exit; otherwise defer LIFO order would
	// Wait() first (deadlock) and cancel second.
	defer func() {
		cancelProgress()
		wg.Wait()
	}()

	if r.config.ShowProgress {
		wg.Add(1)
		go r.showProgress(progressCtx, &wg)
	}

	var taskWg sync.WaitGroup
	for _, item := range r.items {
		select {
		case <-ctx.Done():
			taskWg.Wait()
			return ctx.Err()
		default:
		}

		if ticker != nil {
			select {
			case <-ctx.Done():
				taskWg.Wait()
				return ctx.Err()
			case <-ticker.C:
			}
		}

		select {
		case <-ctx.Done():
			taskWg.Wait()
			return ctx.Err()
		case semaphore <- struct{}{}:
		}

		taskWg.Add(1)
		go func(item T) {
			defer func() {
				<-semaphore
				taskWg.Done()
			}()

			result := r.executeWithRetry(ctx, item)
			r.updateStats(result)

			if result.Error == nil {
				if r.OnSuccess != nil {
					r.OnSuccess(item)
				}
			} else {
				if r.OnError != nil {
					r.OnError(item, result.Error)
				}
			}

			if onResult != nil {
				onResult(result)
			}
		}(item)
	}

	taskWg.Wait()
	cancelProgress()
	wg.Wait()

	if r.config.ShowProgress {
		r.printProgress(true)
	}

	return nil
}

func (r *BatchRunner[T]) executeWithRetry(ctx context.Context, item T) (result BatchResult[T]) {
	result.Item = item
	startTime := time.Now()

	defer func() {
		if p := recover(); p != nil {
			result.Error = fmt.Errorf("panic: %v", p)
			result.Panicked = p
			result.Elapsed = time.Since(startTime)
		}
	}()

	var lastErr error
	for attempt := 0; attempt <= r.config.Retry; attempt++ {
		select {
		case <-ctx.Done():
			result.Error = ctx.Err()
			result.Elapsed = time.Since(startTime)
			return result
		default:
		}

		err := r.handler.Handle(ctx, item)
		if err == nil {
			result.Retries = attempt
			result.Elapsed = time.Since(startTime)
			return result
		}

		lastErr = err

		if attempt < r.config.Retry {
			shift := attempt
			if shift > 30 {
				shift = 30 // cap to prevent overflow
			}
			delay := r.config.RetryDelay * time.Duration(1<<uint(shift))
			select {
			case <-ctx.Done():
				result.Error = ctx.Err()
				result.Elapsed = time.Since(startTime)
				return result
			case <-time.After(delay):
			}
		}
	}

	result.Error = lastErr
	result.Retries = r.config.Retry
	result.Elapsed = time.Since(startTime)
	return result
}

func (r *BatchRunner[T]) updateStats(result BatchResult[T]) {
	atomic.AddInt64(&r.completed, 1)
	if result.Error != nil {
		atomic.AddInt64(&r.failed, 1)
	}
}

func (r *BatchRunner[T]) showProgress(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(r.config.ProgressInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.printProgress(false)
		}
	}
}

func (r *BatchRunner[T]) printProgress(final bool) {
	completed := atomic.LoadInt64(&r.completed)
	failed := atomic.LoadInt64(&r.failed)

	percent := float64(completed) / float64(r.total) * 100
	elapsed := time.Since(r.startTime)

	speed := float64(0)
	if elapsed.Seconds() > 0 {
		speed = float64(completed) / elapsed.Seconds()
	}
	remaining := time.Duration(0)
	if speed > 0 {
		remaining = time.Duration(float64(r.total-completed)/speed) * time.Second
	}

	barWidth := 40
	filled := int(float64(barWidth) * percent / 100)
	bar := ""
	for i := 0; i < barWidth; i++ {
		if i < filled {
			bar += "="
		} else if i == filled {
			bar += ">"
		} else {
			bar += " "
		}
	}

	prefix := "\r"
	suffix := ""
	if final {
		prefix = ""
		suffix = "\n"
	}

	fmt.Fprintf(r.config.ProgressWriter, "%s%s [%s] [%s] %6.2f%% (%d/%d) fail:%d %.1f/s %s/%s%s",
		prefix,
		time.Now().Format("15:04:05"),
		r.handler.Name(),
		bar,
		percent,
		completed,
		r.total,
		failed,
		speed,
		formatBatchDuration(elapsed),
		formatBatchDuration(remaining),
		suffix,
	)
}

// GetResults returns all collected results.
func (r *BatchRunner[T]) GetResults() []BatchResult[T] {
	r.resultsMu.Lock()
	defer r.resultsMu.Unlock()

	results := make([]BatchResult[T], len(r.results))
	copy(results, r.results)
	return results
}

// GetFailedResults returns only failed results.
func (r *BatchRunner[T]) GetFailedResults() []BatchResult[T] {
	r.resultsMu.Lock()
	defer r.resultsMu.Unlock()

	failed := make([]BatchResult[T], 0)
	for _, result := range r.results {
		if result.Error != nil {
			failed = append(failed, result)
		}
	}
	return failed
}

// GetStats returns current execution statistics.
func (r *BatchRunner[T]) GetStats() BatchStats {
	completed := atomic.LoadInt64(&r.completed)
	failed := atomic.LoadInt64(&r.failed)

	return BatchStats{
		Total:     r.total,
		Completed: completed,
		Success:   completed - failed,
		Failed:    failed,
		Elapsed:   time.Since(r.startTime),
	}
}

func formatBatchDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}
