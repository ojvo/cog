package pipeline

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

// ---------------------------------------------------------------------------
// SafeCall tests
// ---------------------------------------------------------------------------

func TestSafeCall_Normal(t *testing.T) {
	var called atomic.Bool
	ok := SafeCall(func() { called.Store(true) })
	if !ok {
		t.Fatal("expected ok=true for normal call")
	}
	if !called.Load() {
		t.Fatal("expected function to be called")
	}
}

func TestSafeCall_Panic(t *testing.T) {
	var called atomic.Bool
	ok := SafeCall(func() {
		called.Store(true)
		panic("boom")
	})
	if ok {
		t.Fatal("expected ok=false for panicking call")
	}
	if !called.Load() {
		t.Fatal("expected function to be called before panic")
	}
}

func TestSafeCall_Nil(t *testing.T) {
	// SafeCall with nil fn should not panic (deferred recover catches nil deref)
	ok := SafeCall(nil)
	if ok {
		t.Fatal("expected ok=false for nil fn")
	}
}

// ---------------------------------------------------------------------------
// SafeCallRecover tests
// ---------------------------------------------------------------------------

func TestSafeCallRecover_Normal(t *testing.T) {
	var called atomic.Bool
	ok, r := SafeCallRecover(func() { called.Store(true) })
	if !ok {
		t.Fatal("expected ok=true for normal call")
	}
	if r != nil {
		t.Fatalf("expected nil recovered, got %v", r)
	}
	if !called.Load() {
		t.Fatal("expected function to be called")
	}
}

func TestSafeCallRecover_Panic(t *testing.T) {
	ok, r := SafeCallRecover(func() { panic("boom") })
	if ok {
		t.Fatal("expected ok=false for panicking call")
	}
	if r != "boom" {
		t.Fatalf("expected recovered='boom', got %v", r)
	}
}

func TestSafeCallRecover_Nil(t *testing.T) {
	ok, _ := SafeCallRecover(nil)
	if ok {
		t.Fatal("expected ok=false for nil fn")
	}
	// recovered may be nil or non-nil depending on runtime; just check ok is false
}

// ---------------------------------------------------------------------------
// Chain[F] tests
// ---------------------------------------------------------------------------

func TestChain_Order(t *testing.T) {
	var order []string
	mw1 := func(next func() string) func() string {
		return func() string {
			order = append(order, "mw1-before")
			result := next()
			order = append(order, "mw1-after")
			return result
		}
	}
	mw2 := func(next func() string) func() string {
		return func() string {
			order = append(order, "mw2-before")
			result := next()
			order = append(order, "mw2-after")
			return result
		}
	}
	core := func() string {
		order = append(order, "core")
		return "ok"
	}

	handler := Chain(core, mw1, mw2)
	result := handler()

	if result != "ok" {
		t.Fatalf("expected 'ok', got %s", result)
	}
	expected := []string{"mw1-before", "mw2-before", "core", "mw2-after", "mw1-after"}
	if len(order) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Fatalf("at index %d: expected %s, got %s", i, v, order[i])
		}
	}
}

func TestChain_Empty(t *testing.T) {
	core := func() int { return 42 }
	handler := Chain(core)
	if handler() != 42 {
		t.Fatal("expected 42")
	}
}

func TestChain_Single(t *testing.T) {
	var called atomic.Bool
	mw := func(next func() string) func() string {
		return func() string {
			called.Store(true)
			return next()
		}
	}
	core := func() string { return "result" }
	handler := Chain(core, mw)
	if handler() != "result" {
		t.Fatal("expected 'result'")
	}
	if !called.Load() {
		t.Fatal("expected middleware to be called")
	}
}

func TestChain_CanBlock(t *testing.T) {
	mw := func(next func() string) func() string {
		return func() string {
			return "blocked"
		}
	}
	core := func() string { return "core" }
	handler := Chain(core, mw)
	if handler() != "blocked" {
		t.Fatal("expected 'blocked'")
	}
}

func TestChainInner_Order(t *testing.T) {
	var order []string
	mw1 := func(next func() string) func() string {
		return func() string {
			order = append(order, "mw1-before")
			result := next()
			order = append(order, "mw1-after")
			return result
		}
	}
	mw2 := func(next func() string) func() string {
		return func() string {
			order = append(order, "mw2-before")
			result := next()
			order = append(order, "mw2-after")
			return result
		}
	}
	core := func() string {
		order = append(order, "core")
		return "ok"
	}

	// ChainInner: mw1 is innermost, mw2 is outermost
	handler := ChainInner(core, mw1, mw2)
	handler()

	expected := []string{"mw2-before", "mw1-before", "core", "mw1-after", "mw2-after"}
	if len(order) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Fatalf("at index %d: expected %s, got %s", i, v, order[i])
		}
	}
}

func TestChainInner_Empty(t *testing.T) {
	core := func() int { return 42 }
	handler := ChainInner(core)
	if handler() != 42 {
		t.Fatal("expected 42")
	}
}

// ---------------------------------------------------------------------------
// Integration: Chain + context.Context (like slm.Handler)
// ---------------------------------------------------------------------------

func TestChain_WithCtx(t *testing.T) {
	type Handler = func(ctx context.Context, name string) (string, error)

	mw := func(next Handler) Handler {
		return func(ctx context.Context, name string) (string, error) {
			return next(ctx, "wrapped:"+name)
		}
	}
	core := func(ctx context.Context, name string) (string, error) {
		return "executed:" + name, nil
	}

	handler := Chain[Handler](core, mw)
	result, err := handler(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if result != "executed:wrapped:test" {
		t.Fatalf("expected 'executed:wrapped:test', got %s", result)
	}
}

// ---------------------------------------------------------------------------
// FanOut[T] tests
// ---------------------------------------------------------------------------

func TestFanOut_Emit(t *testing.T) {
	var count atomic.Int32
	e1 := func(ctx context.Context, event string) { count.Add(1) }
	e2 := func(ctx context.Context, event string) { count.Add(1) }

	f := FanOut[string]{e1, e2}
	f.Emit(context.Background(), "test")

	if count.Load() != 2 {
		t.Fatalf("expected 2 calls, got %d", count.Load())
	}
}

func TestFanOut_PanicRecovery(t *testing.T) {
	var count atomic.Int32
	panicEmitter := func(ctx context.Context, event string) { panic("boom") }
	normalEmitter := func(ctx context.Context, event string) { count.Add(1) }

	f := FanOut[string]{panicEmitter, normalEmitter}
	f.Emit(context.Background(), "test")

	if count.Load() != 1 {
		t.Fatalf("expected 1 call (after panic recovery), got %d", count.Load())
	}
}

func TestFanOut_Empty(t *testing.T) {
	f := FanOut[string]{}
	f.Emit(context.Background(), "test") // should not panic
}

func TestFanOut_Append(t *testing.T) {
	var count atomic.Int32
	e1 := func(ctx context.Context, event string) { count.Add(1) }
	e2 := func(ctx context.Context, event string) { count.Add(10) }

	f := FanOut[string]{e1}
	f = f.Append(e2)
	f.Emit(context.Background(), "test")

	if count.Load() != 11 {
		t.Fatalf("expected 11, got %d", count.Load())
	}
}

func TestFanOut_EmitWith_PanicCallback(t *testing.T) {
	var count atomic.Int32
	var panicVal any
	panicEmitter := func(ctx context.Context, event string) { panic("boom") }
	normalEmitter := func(ctx context.Context, event string) { count.Add(1) }

	f := FanOut[string]{panicEmitter, normalEmitter}
	f.EmitWith(context.Background(), "test", func(r any) { panicVal = r })

	if count.Load() != 1 {
		t.Fatalf("expected 1 call (after panic recovery), got %d", count.Load())
	}
	if panicVal == nil {
		t.Fatal("expected onPanic to be called with panic value")
	}
	if panicVal != "boom" {
		t.Fatalf("expected panic value 'boom', got %v", panicVal)
	}
}

func TestFanOut_EmitWith_NilCallback(t *testing.T) {
	var count atomic.Int32
	panicEmitter := func(ctx context.Context, event string) { panic("boom") }
	normalEmitter := func(ctx context.Context, event string) { count.Add(1) }

	f := FanOut[string]{panicEmitter, normalEmitter}
	f.EmitWith(context.Background(), "test", nil) // nil onPanic → silently swallow

	if count.Load() != 1 {
		t.Fatalf("expected 1 call (after panic recovery), got %d", count.Load())
	}
}

func TestFanOut_EmitWith_NoPanic(t *testing.T) {
	var panicCalled atomic.Bool
	e1 := func(ctx context.Context, event string) {}

	f := FanOut[string]{e1}
	f.EmitWith(context.Background(), "test", func(r any) { panicCalled.Store(true) })

	if panicCalled.Load() {
		t.Fatal("expected onPanic NOT to be called when no panic occurs")
	}
}

// ---------------------------------------------------------------------------
// HookChain[T] tests
// ---------------------------------------------------------------------------

func TestHookChain_AllPass(t *testing.T) {
	var count atomic.Int32
	h1 := func(ctx context.Context, s string) error { count.Add(1); return nil }
	h2 := func(ctx context.Context, s string) error { count.Add(1); return nil }

	chain := HookChain[string]{h1, h2}
	err := chain.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if count.Load() != 2 {
		t.Fatalf("expected 2 calls, got %d", count.Load())
	}
}

func TestHookChain_StopsOnError(t *testing.T) {
	var count atomic.Int32
	h1 := func(ctx context.Context, s string) error { count.Add(1); return errors.New("veto") }
	h2 := func(ctx context.Context, s string) error { count.Add(1); return nil }

	chain := HookChain[string]{h1, h2}
	err := chain.Call(context.Background(), "test")
	if err == nil || err.Error() != "veto" {
		t.Fatalf("expected 'veto' error, got %v", err)
	}
	if count.Load() != 1 {
		t.Fatalf("expected 1 call (chain stopped), got %d", count.Load())
	}
}

func TestHookChain_PanicRecovery(t *testing.T) {
	var count atomic.Int32
	panicHook := func(ctx context.Context, s string) error { panic("boom") }
	normalHook := func(ctx context.Context, s string) error { count.Add(1); return nil }

	chain := HookChain[string]{panicHook, normalHook}
	err := chain.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("expected nil error (panic recovered), got %v", err)
	}
	if count.Load() != 1 {
		t.Fatalf("expected 1 call (after panic recovery), got %d", count.Load())
	}
}

func TestHookChain_Empty(t *testing.T) {
	chain := HookChain[string]{}
	err := chain.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("expected nil error for empty chain, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ValidationChain[T] tests
// ---------------------------------------------------------------------------

func TestValidationChain_CollectsAllNoShortCircuit(t *testing.T) {
	// 关键区分性测试: 与 HookChain stop-on-first 相反, collect-all 须收集
	// 每个输入上每条规则的全部违规, 不因首条违规短路。
	var calls atomic.Int32
	v1 := func(ctx context.Context, s string) *Violation {
		calls.Add(1)
		return &Violation{Rule: "R1", Detail: "v1:" + s}
	}
	v2 := func(ctx context.Context, s string) *Violation {
		calls.Add(1)
		return &Violation{Rule: "R2", Detail: "v2:" + s}
	}

	chain := ValidationChain[string]{v1, v2}
	vs := chain.Check(context.Background(), []string{"a", "b"})

	// 2 inputs × 2 validators = 4 calls, 4 violations (none short-circuited).
	if calls.Load() != 4 {
		t.Fatalf("expected 4 validator calls (no short-circuit), got %d", calls.Load())
	}
	if len(vs) != 4 {
		t.Fatalf("expected 4 violations collected, got %d: %v", len(vs), vs)
	}
	// 顺序: input a → R1, R2; input b → R1, R2.
	wantRules := []string{"R1", "R2", "R1", "R2"}
	for i, w := range wantRules {
		if vs[i].Rule != w {
			t.Fatalf("at %d: expected %s, got %s (vs=%v)", i, w, vs[i].Rule, vs)
		}
	}
}

func TestValidationChain_NilViolationPasses(t *testing.T) {
	passed := atomic.Int32{}
	v := func(ctx context.Context, n int) *Violation {
		if n < 0 {
			return &Violation{Rule: "NEG", Detail: "negative"}
		}
		passed.Add(1)
		return nil
	}
	chain := ValidationChain[int]{v}
	vs := chain.Check(context.Background(), []int{1, -1, 2})
	if len(vs) != 1 || vs[0].Rule != "NEG" {
		t.Fatalf("expected 1 NEG violation, got %v", vs)
	}
	if passed.Load() != 2 {
		t.Fatalf("expected 2 passes, got %d", passed.Load())
	}
}

func TestValidationChain_PanicRecovery(t *testing.T) {
	panicV := func(ctx context.Context, s string) *Violation { panic("boom") }
	normalV := func(ctx context.Context, s string) *Violation {
		return &Violation{Rule: "OK", Detail: s}
	}
	chain := ValidationChain[string]{panicV, normalV}
	vs := chain.Check(context.Background(), []string{"x"})
	if len(vs) != 1 || vs[0].Rule != "OK" {
		t.Fatalf("panic validator should be skipped, got %v", vs)
	}
}

func TestValidationChain_Empty(t *testing.T) {
	chain := ValidationChain[string]{}
	vs := chain.Check(context.Background(), []string{"a"})
	if vs != nil {
		t.Fatalf("expected nil for empty chain, got %v", vs)
	}
}

func TestValidationChain_EmptyItems(t *testing.T) {
	called := atomic.Bool{}
	v := func(ctx context.Context, s string) *Violation {
		called.Store(true)
		return nil
	}
	chain := ValidationChain[string]{v}
	vs := chain.Check(context.Background(), nil)
	if vs != nil {
		t.Fatalf("expected nil for empty items, got %v", vs)
	}
	if called.Load() {
		t.Fatal("validator must not run on empty items")
	}
}

func TestValidationChain_CtxPropagated(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	v := func(ctx context.Context, s string) *Violation {
		if ctx.Err() != nil {
			return &Violation{Rule: "CANCELLED", Detail: "ctx done"}
		}
		return nil
	}
	chain := ValidationChain[string]{v}
	vs := chain.Check(ctx, []string{"x"})
	if len(vs) != 1 || vs[0].Rule != "CANCELLED" {
		t.Fatalf("expected ctx to propagate to validator, got %v", vs)
	}
}
