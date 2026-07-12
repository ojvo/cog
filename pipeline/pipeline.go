// Package pipeline provides generic meta-structures for composing
// cross-cutting concerns across the Colm runtime.
//
// Five orthogonal primitives:
//
//   - Chain[F]:           decorator-based middleware composition
//   - SafeCall/Recover:   panic-safe function invocation
//   - FanOut[T]:          multi-consumer event fan-out (fire-and-forget)
//   - HookChain[T]:       interruptible hook chain (stop on first error)
//   - ValidationChain[T]: collect-all validation chain (gather every violation)
//
// These primitives follow the governance principle of "few concepts,
// strong composition": each package (slm, toolkit, flow, task, evolution)
// retains its own domain-specific type aliases, but delegates the
// structural composition logic to these primitives instead of
// reimplementing it locally.
//
// # Primitive selection guide
//
//   - Need to wrap a handler with middleware?      → Chain[F] / ChainInner[F]
//   - Need to safely call a user-provided func?    → SafeCall / SafeCallRecover
//   - Need to fan out an event to many consumers?  → FanOut[T]
//   - Need a veto chain (first error stops)?       → HookChain[T]
//   - Need to collect all violations (no abort)?   → ValidationChain[T]
//
// # Hook pattern (convention, not code)
//
// The "before/after callback pair" pattern (toolkit.ToolHook,
// evolution.StepInterceptor, flow.Hooks) is documented as a convention
// rather than extracted into code, because Go generics cannot unify
// the domain-specific callback signatures. Domain packages should:
//
//  1. Define their own Hook struct with typed Before/After fields
//  2. Use Chain[F] to build a middleware from the hook
//  3. Document that their hook follows the pipeline.Hook convention
package pipeline

import "context"

// ---------------------------------------------------------------------------
// SafeCall / SafeCallRecover — Panic-safe function invocation
// ---------------------------------------------------------------------------

// SafeCall invokes fn with panic recovery. Returns true if fn ran
// without panicking. This is the foundational building block for all
// panic-safe callback dispatch in the runtime.
//
// For callers that need the recovered panic value (e.g. for logging or
// error construction), use SafeCallRecover.
func SafeCall(fn func()) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
		}
	}()
	fn()
	return true
}

// SafeCallRecover is like SafeCall but also returns the recovered panic
// value. When ok is false, recovered holds the panic value; when ok is
// true, recovered is nil.
//
// Use this variant when the panic value is needed (e.g. logging the
// panic detail, constructing a structured error from the panic). For
// simple "did it panic?" checks, SafeCall is sufficient.
func SafeCallRecover(fn func()) (ok bool, recovered any) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
			recovered = r
		}
	}()
	fn()
	return true, nil
}

// ---------------------------------------------------------------------------
// Chain[F] — Decorator middleware composition
// ---------------------------------------------------------------------------

// Chain assembles a decorator-chain: middlewares[0] is the outermost
// wrapper, the last middleware wraps closest to core.
//
// This is the standard composition order used by slm.Chain,
// toolkit.WithMiddleware, and http middleware chains.
//
// Example:
//
//	handler := pipeline.Chain(core, mw1, mw2)
//	// Calling handler executes: mw1 → mw2 → core → mw2 → mw1
func Chain[F any](core F, middlewares ...func(F) F) F {
	h := core
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// ChainInner assembles a decorator-chain: middlewares[0] is the
// innermost wrapper (closest to core), the last middleware is outermost.
//
// This is useful when middlewares are listed in dependency order
// (e.g., cache → hooks → timeout → audit).
//
// Example:
//
//	handler := pipeline.ChainInner(core, cache, hooks, timeout, audit)
//	// Calling handler executes: audit → timeout → hooks → cache → core
func ChainInner[F any](core F, middlewares ...func(F) F) F {
	h := core
	for _, mw := range middlewares {
		h = mw(h)
	}
	return h
}

// ChainSlice is the slice variant of Chain, accepting a slice of decorators
// instead of variadic arguments. Useful when decorators are built dynamically.
func ChainSlice[F any](core F, decorators []func(F) F) F {
	h := core
	for i := len(decorators) - 1; i >= 0; i-- {
		h = decorators[i](h)
	}
	return h
}

// ---------------------------------------------------------------------------
// FanOut[T] — Multi-consumer event fan-out (fire-and-forget)
// ---------------------------------------------------------------------------

// FanOut calls every emitter in order. If an emitter panics, the panic
// is recovered and the remaining emitters still execute.
//
// This is the fire-and-forget counterpart to HookChain: FanOut always
// calls all emitters; HookChain stops on the first error. Use FanOut
// for notification/observation dispatch where every consumer should
// receive the event regardless of others' outcomes.
//
// Example:
//
//	emitter := pipeline.FanOut[string]{emit1, emit2}
//	emitter.EmitWith(ctx, event, func(r any) { log.Printf("panic: %v", r) })
type FanOut[T any] []func(context.Context, T)

// Emit calls every emitter in order. Panics in individual emitters are
// recovered so that remaining emitters still execute. Panics are silently
// swallowed; use EmitWith for panic logging or other side effects.
func (f FanOut[T]) Emit(ctx context.Context, event T) {
	for _, e := range f {
		SafeCall(func() { e(ctx, event) })
	}
}

// EmitWith calls every emitter in order, like Emit, but invokes onPanic
// when an emitter panics. The onPanic callback receives the recovered
// panic value and can log it, increment metrics, etc.
// If onPanic is nil, panics are silently swallowed (same as Emit).
func (f FanOut[T]) EmitWith(ctx context.Context, event T, onPanic func(recovered any)) {
	for _, e := range f {
		if ok, r := SafeCallRecover(func() { e(ctx, event) }); !ok && onPanic != nil {
			onPanic(r)
		}
	}
}

// Append adds emitters to the fan-out and returns the new FanOut.
func (f FanOut[T]) Append(emitters ...func(context.Context, T)) FanOut[T] {
	return append(f, emitters...)
}

// ---------------------------------------------------------------------------
// HookChain[T] — Interruptible hook chain (stop on first error)
// ---------------------------------------------------------------------------

// HookChain calls each hook in order. If any hook returns a non-nil
// error, the chain stops and returns that error. Panics in individual
// hooks are recovered (treated as nil error, chain continues).
//
// This is the interruptible counterpart to FanOut: FanOut always calls
// all emitters; HookChain stops on the first error. Use HookChain for
// validation/policy hooks (e.g. BeforeTool validators, access checks)
// where a single veto should stop the chain.
//
// Example:
//
//	chain := HookChain[string]{validate1, validate2}
//	if err := chain.Call(ctx, input); err != nil {
//	    // validation failed
//	}
type HookChain[T any] []func(context.Context, T) error

// Call invokes each hook in order. Stops at the first non-nil error.
// Panics are recovered per-hook (treated as success, chain continues).
func (h HookChain[T]) Call(ctx context.Context, event T) error {
	for _, fn := range h {
		var err error
		if ok, _ := SafeCallRecover(func() { err = fn(ctx, event) }); !ok {
			continue // panic recovered, skip this hook
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// ValidationChain[T] — Collect-all validation chain (no short-circuit)
// ---------------------------------------------------------------------------

// Violation describes a single validation rule violation against one input.
// Rule identifies the rule that produced it (set by the validator, typically
// via closure); Detail is a human-readable description that should be
// self-contained — including any input identifier (finding title, file:line)
// the caller needs to locate the violation, since the generic chain does not
// retain the input value itself.
type Violation struct {
	Rule   string
	Detail string
}

// ValidationChain[T] applies every validator to every input and collects
// all violations without short-circuiting. Each validator is a function
// func(context.Context, T) *Violation that inspects one input and returns
// a Violation (with Rule set) if the input fails, or nil if it passes.
//
// This is the collect-all counterpart to HookChain:
//   - HookChain[T]:  veto chain, first error stops — for gates that should abort
//   - ValidationChain[T]: report chain, all violations collected — for reports
//     that should surface every defect in a single pass
//
// Validators receive the context.Context to support I/O-backed checks (e.g.
// executing evidence commands) with cancellation, mirroring the (ctx, T)
// signature of FanOut emitters and HookChain hooks. Panics in individual
// validators are recovered per-validator (treated as no violation, chain
// continues), matching the panic-safety of FanOut and HookChain.
//
// Example:
//
//	chain := pipeline.ValidationChain[finding]{evidenceRule, cmdRule}
//	vs := chain.Check(ctx, findings)
//	for _, v := range vs { report(v) }
type ValidationChain[T any] []func(context.Context, T) *Violation

// Check applies every validator to every input, collecting all violations.
// Iteration order: for each input in order, validators run in chain order.
// No short-circuit — every validator sees every input. Per-validator panics
// are recovered (treated as pass, chain continues).
func (c ValidationChain[T]) Check(ctx context.Context, items []T) []Violation {
	var vs []Violation
	for _, item := range items {
		for _, v := range c {
			var vio *Violation
			if ok, _ := SafeCallRecover(func() { vio = v(ctx, item) }); !ok {
				continue // panic recovered, skip this validator
			}
			if vio != nil {
				vs = append(vs, *vio)
			}
		}
	}
	return vs
}
