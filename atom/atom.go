// Package atom provides atomic wrappers around the primitive numeric and
// boolean types from sync/atomic.
//
// Each wrapper adds convenience helpers that the underlying atomic types lack:
//
//   - Sub / Inc / Dec for numeric types (mirroring Add).
//   - Toggle for Bool.
//   - MarshalJSON / UnmarshalJSON so wrappers can be embedded in JSON-serialised
//     structs without a separate accessor.
//   - String for quick debugging / logging.
//   - A noCopy guard so `go vet` catches accidental value copies.
//
// Wrappers are safe for concurrent use by multiple goroutines.
package atom

import (
	"encoding/json"
	"strconv"
	"sync/atomic"
)

// noCopy is a zero-size sentinel that makes `go vet` report any value copy of
// the embedding struct. Embed it as the first field (`_ noCopy`) so the vet
// copylocks check fires.
//
// See https://golang.org/issues/8005#issuecomment-190753527.
type noCopy struct{}

// Lock and Unlock are no-ops; they exist solely so `go vet` treats noCopy as
// a lock for the copylocks checker.
func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

// Bool is an atomic wrapper around bool with JSON and String support.
type Bool struct {
	_ noCopy
	v atomic.Bool
}

// NewBool creates a Bool initialised to val.
func NewBool(val bool) *Bool {
	b := &Bool{}
	b.v.Store(val)
	return b
}

// Load atomically returns the wrapped value.
func (b *Bool) Load() bool { return b.v.Load() }

// Store atomically sets the wrapped value.
func (b *Bool) Store(val bool) { b.v.Store(val) }

// Swap atomically sets the value and returns the previous value.
func (b *Bool) Swap(val bool) (old bool) { return b.v.Swap(val) }

// CompareAndSwap atomically sets the value to new only if the current value
// equals old. It reports whether the swap happened.
func (b *Bool) CompareAndSwap(old, new bool) (swapped bool) {
	return b.v.CompareAndSwap(old, new)
}

// Toggle atomically flips the boolean and returns the new value.
func (b *Bool) Toggle() bool {
	for {
		old := b.v.Load()
		if b.v.CompareAndSwap(old, !old) {
			return !old
		}
	}
}

// MarshalJSON encodes the wrapped value as a JSON boolean.
func (b *Bool) MarshalJSON() ([]byte, error) { return json.Marshal(b.Load()) }

// UnmarshalJSON decodes a JSON boolean into the wrapped value.
func (b *Bool) UnmarshalJSON(data []byte) error {
	var v bool
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	b.Store(v)
	return nil
}

// String returns the wrapped value as a string ("true" or "false").
func (b *Bool) String() string { return strconv.FormatBool(b.Load()) }

// Int32 is an atomic wrapper around int32 with arithmetic and JSON support.
type Int32 struct {
	_ noCopy
	v atomic.Int32
}

// NewInt32 creates an Int32 initialised to val.
func NewInt32(val int32) *Int32 {
	i := &Int32{}
	i.v.Store(val)
	return i
}

// Load atomically returns the wrapped value.
func (i *Int32) Load() int32 { return i.v.Load() }

// Store atomically sets the wrapped value.
func (i *Int32) Store(val int32) { i.v.Store(val) }

// Add atomically adds delta and returns the new value.
func (i *Int32) Add(delta int32) int32 { return i.v.Add(delta) }

// Sub atomically subtracts delta and returns the new value.
func (i *Int32) Sub(delta int32) int32 { return i.v.Add(-delta) }

// Inc atomically increments by 1 and returns the new value.
func (i *Int32) Inc() int32 { return i.Add(1) }

// Dec atomically decrements by 1 and returns the new value.
func (i *Int32) Dec() int32 { return i.Sub(1) }

// CompareAndSwap atomically sets the value to new only if the current value
// equals old. It reports whether the swap happened.
func (i *Int32) CompareAndSwap(old, new int32) (swapped bool) {
	return i.v.CompareAndSwap(old, new)
}

// Swap atomically sets the value and returns the previous value.
func (i *Int32) Swap(val int32) (old int32) { return i.v.Swap(val) }

// MarshalJSON encodes the wrapped value as a JSON number.
func (i *Int32) MarshalJSON() ([]byte, error) { return json.Marshal(i.Load()) }

// UnmarshalJSON decodes a JSON number into the wrapped value.
func (i *Int32) UnmarshalJSON(b []byte) error {
	var v int32
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	i.Store(v)
	return nil
}

// String returns the wrapped value as a decimal string.
func (i *Int32) String() string { return strconv.FormatInt(int64(i.Load()), 10) }

// Int64 is an atomic wrapper around int64 with arithmetic and JSON support.
type Int64 struct {
	_ noCopy
	v atomic.Int64
}

// NewInt64 creates an Int64 initialised to val.
func NewInt64(val int64) *Int64 {
	i := &Int64{}
	i.v.Store(val)
	return i
}

// Load atomically returns the wrapped value.
func (i *Int64) Load() int64 { return i.v.Load() }

// Store atomically sets the wrapped value.
func (i *Int64) Store(val int64) { i.v.Store(val) }

// Add atomically adds delta and returns the new value.
func (i *Int64) Add(delta int64) int64 { return i.v.Add(delta) }

// Sub atomically subtracts delta and returns the new value.
func (i *Int64) Sub(delta int64) int64 { return i.v.Add(-delta) }

// Inc atomically increments by 1 and returns the new value.
func (i *Int64) Inc() int64 { return i.Add(1) }

// Dec atomically decrements by 1 and returns the new value.
func (i *Int64) Dec() int64 { return i.Sub(1) }

// CompareAndSwap atomically sets the value to new only if the current value
// equals old. It reports whether the swap happened.
func (i *Int64) CompareAndSwap(old, new int64) (swapped bool) {
	return i.v.CompareAndSwap(old, new)
}

// Swap atomically sets the value and returns the previous value.
func (i *Int64) Swap(val int64) (old int64) { return i.v.Swap(val) }

// MarshalJSON encodes the wrapped value as a JSON number.
func (i *Int64) MarshalJSON() ([]byte, error) { return json.Marshal(i.Load()) }

// UnmarshalJSON decodes a JSON number into the wrapped value.
func (i *Int64) UnmarshalJSON(b []byte) error {
	var v int64
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	i.Store(v)
	return nil
}

// String returns the wrapped value as a decimal string.
func (i *Int64) String() string { return strconv.FormatInt(i.Load(), 10) }

// Uint32 is an atomic wrapper around uint32 with arithmetic and JSON support.
type Uint32 struct {
	_ noCopy
	v atomic.Uint32
}

// NewUint32 creates a Uint32 initialised to val.
func NewUint32(val uint32) *Uint32 {
	i := &Uint32{}
	i.v.Store(val)
	return i
}

// Load atomically returns the wrapped value.
func (i *Uint32) Load() uint32 { return i.v.Load() }

// Store atomically sets the wrapped value.
func (i *Uint32) Store(val uint32) { i.v.Store(val) }

// Add atomically adds delta and returns the new value.
func (i *Uint32) Add(delta uint32) uint32 { return i.v.Add(delta) }

// Sub atomically subtracts delta and returns the new value.
func (i *Uint32) Sub(delta uint32) uint32 { return i.v.Add(^(delta - 1)) }

// Inc atomically increments by 1 and returns the new value.
func (i *Uint32) Inc() uint32 { return i.Add(1) }

// Dec atomically decrements by 1 and returns the new value.
func (i *Uint32) Dec() uint32 { return i.Sub(1) }

// CompareAndSwap atomically sets the value to new only if the current value
// equals old. It reports whether the swap happened.
func (i *Uint32) CompareAndSwap(old, new uint32) (swapped bool) {
	return i.v.CompareAndSwap(old, new)
}

// Swap atomically sets the value and returns the previous value.
func (i *Uint32) Swap(val uint32) (old uint32) { return i.v.Swap(val) }

// MarshalJSON encodes the wrapped value as a JSON number.
func (i *Uint32) MarshalJSON() ([]byte, error) { return json.Marshal(i.Load()) }

// UnmarshalJSON decodes a JSON number into the wrapped value.
func (i *Uint32) UnmarshalJSON(b []byte) error {
	var v uint32
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	i.Store(v)
	return nil
}

// String returns the wrapped value as a decimal string.
func (i *Uint32) String() string { return strconv.FormatUint(uint64(i.Load()), 10) }

// Uint64 is an atomic wrapper around uint64 with arithmetic and JSON support.
type Uint64 struct {
	_ noCopy
	v atomic.Uint64
}

// NewUint64 creates a Uint64 initialised to val.
func NewUint64(val uint64) *Uint64 {
	i := &Uint64{}
	i.v.Store(val)
	return i
}

// Load atomically returns the wrapped value.
func (i *Uint64) Load() uint64 { return i.v.Load() }

// Store atomically sets the wrapped value.
func (i *Uint64) Store(val uint64) { i.v.Store(val) }

// Add atomically adds delta and returns the new value.
func (i *Uint64) Add(delta uint64) uint64 { return i.v.Add(delta) }

// Sub atomically subtracts delta and returns the new value.
func (i *Uint64) Sub(delta uint64) uint64 { return i.v.Add(^(delta - 1)) }

// Inc atomically increments by 1 and returns the new value.
func (i *Uint64) Inc() uint64 { return i.Add(1) }

// Dec atomically decrements by 1 and returns the new value.
func (i *Uint64) Dec() uint64 { return i.Sub(1) }

// CompareAndSwap atomically sets the value to new only if the current value
// equals old. It reports whether the swap happened.
func (i *Uint64) CompareAndSwap(old, new uint64) (swapped bool) {
	return i.v.CompareAndSwap(old, new)
}

// Swap atomically sets the value and returns the previous value.
func (i *Uint64) Swap(val uint64) (old uint64) { return i.v.Swap(val) }

// MarshalJSON encodes the wrapped value as a JSON number.
func (i *Uint64) MarshalJSON() ([]byte, error) { return json.Marshal(i.Load()) }

// UnmarshalJSON decodes a JSON number into the wrapped value.
func (i *Uint64) UnmarshalJSON(b []byte) error {
	var v uint64
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	i.Store(v)
	return nil
}

// String returns the wrapped value as a decimal string.
func (i *Uint64) String() string { return strconv.FormatUint(i.Load(), 10) }
