// Package pipeline provides generic meta-structures for composing
// processing pipelines: Chain, SafeCall, FanOut, HookChain, and
// ValidationChain. All primitives are generic (type-parameterized)
// to avoid unnecessary interface{} boxing.
package pipeline
