package atom

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestBool(t *testing.T) {
	b := NewBool(true)
	if v := b.Load(); v != true {
		t.Fatalf("Load() = %v, want true", v)
	}

	b.Store(false)
	if v := b.Load(); v != false {
		t.Fatalf("Load() after Store(false) = %v, want false", v)
	}

	if old := b.Swap(true); old != false {
		t.Fatalf("Swap(true) = %v, want false", old)
	}
	if v := b.Load(); v != true {
		t.Fatalf("Load() after Swap = %v, want true", v)
	}

	if !b.CompareAndSwap(true, false) {
		t.Fatal("CompareAndSwap(true, false) = false, want true")
	}
	if v := b.Load(); v != false {
		t.Fatalf("Load() after CAS = %v, want false", v)
	}
	if b.CompareAndSwap(true, false) {
		t.Fatal("CompareAndSwap(true, false) = true, want false (current is false)")
	}

	if v := b.Toggle(); v != true {
		t.Fatalf("Toggle() = %v, want true", v)
	}
	if v := b.Load(); v != true {
		t.Fatalf("Load() after Toggle = %v, want true", v)
	}
	if v := b.Toggle(); v != false {
		t.Fatalf("Toggle() = %v, want false", v)
	}

	// JSON round-trip.
	out, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(out) != "false" {
		t.Fatalf("Marshal = %s, want \"false\"", out)
	}
	if err := json.Unmarshal([]byte("true"), b); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if v := b.Load(); v != true {
		t.Fatalf("Load() after Unmarshal = %v, want true", v)
	}
	if s := b.String(); s != "true" {
		t.Fatalf("String() = %q, want \"true\"", s)
	}
}

func TestInt32(t *testing.T) {
	testSignedInt(t, func(v int32) signedIface[int32] { return NewInt32(v) })
}

func TestInt64(t *testing.T) {
	testSignedInt(t, func(v int64) signedIface[int64] { return NewInt64(v) })
}

// testSignedInt exercises the common API of Int32 and Int64 using a tiny
// helper interface so both types share the same assertions.
type signedIface[T int32 | int64] interface {
	Load() T
	Store(T)
	Add(T) T
	Sub(T) T
	Inc() T
	Dec() T
	CompareAndSwap(old, new T) bool
	Swap(T) T
	MarshalJSON() ([]byte, error)
	UnmarshalJSON([]byte) error
	String() string
}

func testSignedInt[T int32 | int64](t *testing.T, make func(T) signedIface[T]) {
	var initial T = 10
	i := make(initial)

	if v := i.Load(); v != initial {
		t.Fatalf("Load() = %v, want %v", v, initial)
	}
	i.Store(20)
	if v := i.Load(); v != 20 {
		t.Fatalf("Load() after Store = %v, want 20", v)
	}
	if v := i.Add(5); v != 25 {
		t.Fatalf("Add(5) = %v, want 25", v)
	}
	if v := i.Sub(5); v != 20 {
		t.Fatalf("Sub(5) = %v, want 20", v)
	}
	if v := i.Inc(); v != 21 {
		t.Fatalf("Inc() = %v, want 21", v)
	}
	if v := i.Dec(); v != 20 {
		t.Fatalf("Dec() = %v, want 20", v)
	}
	if !i.CompareAndSwap(20, 30) {
		t.Fatal("CompareAndSwap(20, 30) = false, want true")
	}
	if v := i.Load(); v != 30 {
		t.Fatalf("Load() after CAS = %v, want 30", v)
	}
	if i.CompareAndSwap(20, 40) {
		t.Fatal("CompareAndSwap(20, 40) = true, want false (current is 30)")
	}
	if old := i.Swap(50); old != 30 {
		t.Fatalf("Swap(50) = %v, want 30", old)
	}
	if v := i.Load(); v != 50 {
		t.Fatalf("Load() after Swap = %v, want 50", v)
	}

	// JSON round-trip.
	out, err := i.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(out) != "50" {
		t.Fatalf("MarshalJSON = %s, want \"50\"", out)
	}
	if err := i.UnmarshalJSON([]byte("60")); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if v := i.Load(); v != 60 {
		t.Fatalf("Load() after UnmarshalJSON = %v, want 60", v)
	}
	if s := i.String(); s != "60" {
		t.Fatalf("String() = %q, want \"60\"", s)
	}
}

func TestUint32(t *testing.T) {
	testUnsigned(t, func(v uint32) unsignedIface[uint32] { return NewUint32(v) })
}

func TestUint64(t *testing.T) {
	testUnsigned(t, func(v uint64) unsignedIface[uint64] { return NewUint64(v) })
}

type unsignedIface[T uint32 | uint64] interface {
	Load() T
	Store(T)
	Add(T) T
	Sub(T) T
	Inc() T
	Dec() T
	CompareAndSwap(old, new T) bool
	Swap(T) T
	MarshalJSON() ([]byte, error)
	UnmarshalJSON([]byte) error
	String() string
}

func testUnsigned[T uint32 | uint64](t *testing.T, make func(T) unsignedIface[T]) {
	var initial T = 10
	i := make(initial)

	if v := i.Load(); v != initial {
		t.Fatalf("Load() = %v, want %v", v, initial)
	}
	i.Store(20)
	if v := i.Load(); v != 20 {
		t.Fatalf("Load() after Store = %v, want 20", v)
	}
	if v := i.Add(5); v != 25 {
		t.Fatalf("Add(5) = %v, want 25", v)
	}
	if v := i.Sub(5); v != 20 {
		t.Fatalf("Sub(5) = %v, want 20", v)
	}
	if v := i.Inc(); v != 21 {
		t.Fatalf("Inc() = %v, want 21", v)
	}
	if v := i.Dec(); v != 20 {
		t.Fatalf("Dec() = %v, want 20", v)
	}
	if !i.CompareAndSwap(20, 30) {
		t.Fatal("CompareAndSwap(20, 30) = false, want true")
	}
	if v := i.Load(); v != 30 {
		t.Fatalf("Load() after CAS = %v, want 30", v)
	}
	if old := i.Swap(50); old != 30 {
		t.Fatalf("Swap(50) = %v, want 30", old)
	}
	if v := i.Load(); v != 50 {
		t.Fatalf("Load() after Swap = %v, want 50", v)
	}

	// JSON round-trip.
	out, err := i.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(out) != "50" {
		t.Fatalf("MarshalJSON = %s, want \"50\"", out)
	}
	if err := i.UnmarshalJSON([]byte("60")); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if v := i.Load(); v != 60 {
		t.Fatalf("Load() after UnmarshalJSON = %v, want 60", v)
	}
	if s := i.String(); s != "60" {
		t.Fatalf("String() = %q, want \"60\"", s)
	}
}

func TestUintSub_Wrapping(t *testing.T) {
	// Sub on unsigned types uses two's-complement wrapping.
	// Verify: 5 - 10 should wrap to a large value, not panic.
	u := NewUint32(5)
	v := u.Sub(10)
	if v != 4294967291 {
		t.Fatalf("Sub(10) on 5 = %v, want 4294967291 (uint32 wrap)", v)
	}
}

func TestConcurrency_Int32(t *testing.T) {
	val := NewInt32(0)
	const count = 1000
	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func() {
			defer wg.Done()
			val.Inc()
		}()
	}
	wg.Wait()
	if v := val.Load(); v != count {
		t.Fatalf("after %d concurrent Inc, Load() = %v, want %d", count, v, count)
	}
}

func TestConcurrency_Int64(t *testing.T) {
	val := NewInt64(0)
	const count = 2000
	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func() {
			defer wg.Done()
			val.Inc()
		}()
	}
	wg.Wait()
	if v := val.Load(); v != count {
		t.Fatalf("after %d concurrent Inc, Load() = %v, want %d", count, v, count)
	}
}

func TestConcurrency_Bool(t *testing.T) {
	b := NewBool(false)
	const count = 1000
	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func() {
			defer wg.Done()
			b.Toggle()
		}()
	}
	wg.Wait()
	// After an even number of toggles the value should return to false.
	if v := b.Load(); v != false {
		t.Fatalf("after %d Toggle calls, Load() = %v, want false", count, v)
	}
}

func TestJSONUnmarshal_Invalid(t *testing.T) {
	tests := []struct {
		name string
		fn   func([]byte) error
	}{
		{"Bool", func(b []byte) error { return NewBool(false).UnmarshalJSON(b) }},
		{"Int32", func(b []byte) error { return NewInt32(0).UnmarshalJSON(b) }},
		{"Int64", func(b []byte) error { return NewInt64(0).UnmarshalJSON(b) }},
		{"Uint32", func(b []byte) error { return NewUint32(0).UnmarshalJSON(b) }},
		{"Uint64", func(b []byte) error { return NewUint64(0).UnmarshalJSON(b) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fn([]byte("not-json")); err == nil {
				t.Fatal("UnmarshalJSON(invalid) = nil, want error")
			}
		})
	}
}
