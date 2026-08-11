package resil

import (
	"errors"
	"sync"
	"testing"
)

func TestFallback_DefaultConfig(t *testing.T) {
	c := NewFallbackConfig()
	if c.Threshold != 3 {
		t.Fatalf("default Threshold = %d, want 3", c.Threshold)
	}
	if c.IsFailure == nil {
		t.Fatal("default IsFailure is nil")
	}
	if !c.IsFailure(errors.New("any")) {
		t.Fatal("default IsFailure should return true for non-nil error")
	}
	if c.IsFailure(nil) {
		t.Fatal("default IsFailure should return false for nil")
	}
}

func TestFallback_NoFallback_IsNoOp(t *testing.T) {
	called := 0
	primary := func() error {
		called++
		return errors.New("fail")
	}
	e := NewFallbackExecutor(primary, nil, nil)
	for i := 0; i < 5; i++ {
		if err := e.Execute(); err == nil {
			t.Fatal("expected error from primary")
		}
	}
	if called != 5 {
		t.Fatalf("primary called %d times, want 5 (no fallback should keep calling primary)", called)
	}
	if e.IsUsingFallback() {
		t.Fatal("should never enter fallback mode when fallback is nil")
	}
}

func TestFallback_FailuresTriggerFallback(t *testing.T) {
	primaryCalls := 0
	fallbackCalls := 0
	primary := func() error {
		primaryCalls++
		return errors.New("overloaded")
	}
	fallback := func() error {
		fallbackCalls++
		return nil
	}
	// Threshold=3: 前 3 次 primary 失败累计到阈值，第 4 次开始切到 fallback。
	cfg := NewFallbackConfig().WithThreshold(3)
	e := NewFallbackExecutor(primary, fallback, cfg)

	for i := 0; i < 3; i++ {
		if err := e.Execute(); err == nil {
			t.Fatalf("call %d: expected primary error", i+1)
		}
	}
	if primaryCalls != 3 {
		t.Fatalf("primaryCalls = %d, want 3", primaryCalls)
	}
	if !e.IsUsingFallback() {
		t.Fatal("after 3 failures threshold reached, should have switched to using=true")
	}

	// 第 4 次调用应使用 fallback 并成功，恢复 primary 状态。
	if err := e.Execute(); err != nil {
		t.Fatalf("call 4: expected fallback success, got %v", err)
	}
	if fallbackCalls != 1 {
		t.Fatalf("fallbackCalls = %d, want 1", fallbackCalls)
	}
	if e.IsUsingFallback() {
		t.Fatal("after fallback success, should recover to primary")
	}
}

func TestFallback_SuccessRecoversToPrimary(t *testing.T) {
	primaryCalls := 0
	fallbackCalls := 0
	primary := func() error {
		primaryCalls++
		return errors.New("fail")
	}
	fallback := func() error {
		fallbackCalls++
		return nil
	}
	cfg := NewFallbackConfig().WithThreshold(1)
	e := NewFallbackExecutor(primary, fallback, cfg)

	// 第 1 次 primary 失败，达阈值切换。
	if err := e.Execute(); err == nil {
		t.Fatal("call 1: expected primary error")
	}
	if primaryCalls != 1 || fallbackCalls != 0 {
		t.Fatalf("after call 1: primaryCalls=%d fallbackCalls=%d, want 1/0", primaryCalls, fallbackCalls)
	}

	// 第 2 次 fallback 成功，恢复 primary。
	if err := e.Execute(); err != nil {
		t.Fatalf("call 2: expected fallback success, got %v", err)
	}
	if primaryCalls != 1 || fallbackCalls != 1 {
		t.Fatalf("after call 2: primaryCalls=%d fallbackCalls=%d, want 1/1", primaryCalls, fallbackCalls)
	}

	// 第 3 次回到 primary 再次失败。
	if err := e.Execute(); err == nil {
		t.Fatal("call 3: expected primary error")
	}
	if primaryCalls != 2 || fallbackCalls != 1 {
		t.Fatalf("after call 3: primaryCalls=%d fallbackCalls=%d, want 2/1", primaryCalls, fallbackCalls)
	}
}

func TestFallback_NonFailureErrorDoesNotTrigger(t *testing.T) {
	primaryCalls := 0
	fallbackCalls := 0
	canceledErr := errors.New("canceled")
	overloadErr := errors.New("overloaded")
	primary := func() error {
		primaryCalls++
		return canceledErr
	}
	fallback := func() error {
		fallbackCalls++
		return nil
	}
	// IsFailure 只把 overloaded 当作故障，canceled 不计数。
	cfg := NewFallbackConfig().WithThreshold(3).WithIsFailure(func(err error) bool {
		return errors.Is(err, overloadErr)
	})
	e := NewFallbackExecutor(primary, fallback, cfg)

	// 5 次 canceled 错误都不应触发 fallback。
	for i := 0; i < 5; i++ {
		if err := e.Execute(); !errors.Is(err, canceledErr) {
			t.Fatalf("call %d: expected canceled, got %v", i+1, err)
		}
	}
	if primaryCalls != 5 || fallbackCalls != 0 {
		t.Fatalf("after 5 canceled: primaryCalls=%d fallbackCalls=%d, want 5/0", primaryCalls, fallbackCalls)
	}
	if e.IsUsingFallback() {
		t.Fatal("canceled errors should not trigger fallback")
	}

	// 改 primary 返回 overloadErr，3 次后应触发 fallback。
	primary = func() error {
		primaryCalls++
		return overloadErr
	}
	e2 := NewFallbackExecutor(primary, fallback, cfg)
	for i := 0; i < 3; i++ {
		if err := e2.Execute(); !errors.Is(err, overloadErr) {
			t.Fatalf("overload call %d: expected overloaded, got %v", i+1, err)
		}
	}
	if !e2.IsUsingFallback() {
		t.Fatal("3 overload errors should trigger fallback")
	}
}

func TestFallback_OverloadWhileOnFallbackStaysOnFallback(t *testing.T) {
	primaryCalls := 0
	fallbackCalls := 0
	primary := func() error {
		primaryCalls++
		return errors.New("primary-down")
	}
	fallback := func() error {
		fallbackCalls++
		return errors.New("fallback-down")
	}
	cfg := NewFallbackConfig().WithThreshold(1)
	e := NewFallbackExecutor(primary, fallback, cfg)

	// 1 次 primary 失败 → 切换到 fallback。
	if err := e.Execute(); err == nil {
		t.Fatal("call 1: expected primary error")
	}
	if !e.IsUsingFallback() {
		t.Fatal("should be in fallback mode after threshold")
	}

	// 多次 fallback 失败，应保持 fallback 状态。
	for i := 0; i < 3; i++ {
		if err := e.Execute(); err == nil {
			t.Fatalf("fallback call %d: expected error", i+1)
		}
	}
	if !e.IsUsingFallback() {
		t.Fatal("should stay in fallback mode while fallback also fails")
	}
	if primaryCalls != 1 || fallbackCalls != 3 {
		t.Fatalf("primaryCalls=%d fallbackCalls=%d, want 1/3", primaryCalls, fallbackCalls)
	}

	// fallback 成功后应恢复 primary。
	fallback = func() error {
		fallbackCalls++
		return nil
	}
	e3 := NewFallbackExecutor(primary, fallback, cfg)
	_ = e3.Execute() // primary fail
	_ = e3.Execute() // fallback success
	if e3.IsUsingFallback() {
		t.Fatal("after fallback success on new executor, should recover to primary")
	}
}

func TestFallback_ConcurrentSafety(t *testing.T) {
	primaryCalls := 0
	fallbackCalls := 0
	var mu sync.Mutex
	primary := func() error {
		mu.Lock()
		primaryCalls++
		mu.Unlock()
		return errors.New("fail")
	}
	fallback := func() error {
		mu.Lock()
		fallbackCalls++
		mu.Unlock()
		return nil
	}
	cfg := NewFallbackConfig().WithThreshold(2)
	e := NewFallbackExecutor(primary, fallback, cfg)

	const goroutines = 50
	const iterations = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = e.Execute()
				_ = e.IsUsingFallback()
			}
		}()
	}
	wg.Wait()

	total := goroutines * iterations
	mu.Lock()
	pc, fc := primaryCalls, fallbackCalls
	mu.Unlock()
	if pc+fc != total {
		t.Fatalf("total calls = %d, want %d (primary=%d fallback=%d)", pc+fc, total, pc, fc)
	}
}

func TestFallback_ThresholdBoundary(t *testing.T) {
	primaryCalls := 0
	fallbackCalls := 0
	primary := func() error {
		primaryCalls++
		return errors.New("fail")
	}
	fallback := func() error {
		fallbackCalls++
		return nil
	}
	cfg := NewFallbackConfig().WithThreshold(3)
	e := NewFallbackExecutor(primary, fallback, cfg)

	// 第 1、2 次失败不应切换。
	for i := 0; i < 2; i++ {
		_ = e.Execute()
		if e.IsUsingFallback() {
			t.Fatalf("after %d failures, should not be in fallback mode", i+1)
		}
	}
	if primaryCalls != 2 || fallbackCalls != 0 {
		t.Fatalf("after 2 failures: primaryCalls=%d fallbackCalls=%d, want 2/0", primaryCalls, fallbackCalls)
	}

	// 第 3 次失败达阈值，但本次调用仍是 primary（状态在调用后才更新）。
	_ = e.Execute()
	if primaryCalls != 3 || fallbackCalls != 0 {
		t.Fatalf("after 3rd call: primaryCalls=%d fallbackCalls=%d, want 3/0", primaryCalls, fallbackCalls)
	}
	if !e.IsUsingFallback() {
		t.Fatal("after 3 failures, should have switched to using=true")
	}

	// 第 4 次调用使用 fallback。
	_ = e.Execute()
	if primaryCalls != 3 || fallbackCalls != 1 {
		t.Fatalf("after 4th call: primaryCalls=%d fallbackCalls=%d, want 3/1", primaryCalls, fallbackCalls)
	}
}

func TestFallback_Reset(t *testing.T) {
	primaryCalls := 0
	fallbackCalls := 0
	primary := func() error {
		primaryCalls++
		return errors.New("fail")
	}
	fallback := func() error {
		fallbackCalls++
		return nil
	}
	cfg := NewFallbackConfig().WithThreshold(1)
	e := NewFallbackExecutor(primary, fallback, cfg)

	// 触发 fallback。
	_ = e.Execute()
	if !e.IsUsingFallback() {
		t.Fatal("expected to be in fallback mode")
	}

	// Reset 后下次应使用 primary。
	e.Reset()
	if e.IsUsingFallback() {
		t.Fatal("after Reset, should not be in fallback mode")
	}

	// 下次 Execute 使用 primary（会再次失败并重新触发）。
	prevPrimary := primaryCalls
	_ = e.Execute()
	if primaryCalls != prevPrimary+1 {
		t.Fatalf("after Reset, primaryCalls should increment by 1, got %d (prev %d)", primaryCalls, prevPrimary)
	}
}
