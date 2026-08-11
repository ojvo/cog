package util

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAsString(t *testing.T) {
	if got := AsString(123); got != "123" {
		t.Fatalf("AsString(123) = %q", got)
	}
	if got := AsString(true); got != "true" {
		t.Fatalf("AsString(true) = %q", got)
	}
	if got := AsString([]byte("abc")); got != "abc" {
		t.Fatalf("AsString([]byte) = %q", got)
	}
}

func TestAsInt64(t *testing.T) {
	cases := []struct {
		in   any
		want int64
	}{
		{"123", 123},
		{123.9, 123},
		{"0x10", 16},
		{"0b1010", 10},
		{"-0x10", -16},
		{"2s", int64(2 * time.Second)},
	}
	for _, tc := range cases {
		if got := AsInt64(tc.in); got != tc.want {
			t.Fatalf("AsInt64(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestAsUint64(t *testing.T) {
	if got := AsUint64("0x10"); got != 16 {
		t.Fatalf("AsUint64 hex = %d", got)
	}
	if got := AsUint64([]byte{0x01, 0x02}); got != 0x0102 {
		t.Fatalf("AsUint64 bytes = %d", got)
	}
}

func TestAsFloat64(t *testing.T) {
	if got := AsFloat64("123.45"); got != 123.45 {
		t.Fatalf("AsFloat64 string = %v", got)
	}
	bits := uint64(0x3ff0000000000000)
	if got := AsFloat64(bits); got != 1 {
		t.Fatalf("AsFloat64 bits = %v", got)
	}
}

func TestAsFloat64_Float32Direct(t *testing.T) {
	v := float32(0.1)
	got := AsFloat64(v)
	if got != float64(v) {
		t.Fatalf("AsFloat64(float32) = %v, want %v (direct conversion)", got, float64(v))
	}
	if got == 0.1 {
		t.Fatalf("AsFloat64(float32(0.1)) = 0.1, but float32(0.1) != 0.1 exactly; string roundtrip would lose this")
	}
}

func TestAsBool(t *testing.T) {
	truthy := []any{"true", 1, []int{1}, struct{}{}, []byte("yes")}
	for _, in := range truthy {
		if !AsBool(in) {
			t.Fatalf("AsBool(%v) should be true", in)
		}
	}
	falsy := []any{"", "0", "off", "false", 0, []byte("no"), []int{}}
	for _, in := range falsy {
		if AsBool(in) {
			t.Fatalf("AsBool(%v) should be false", in)
		}
	}
}

func TestAsAnySliceAndStrings(t *testing.T) {
	if got := AsAnySlice(5); len(got) != 1 || got[0].(int) != 5 {
		t.Fatalf("AsAnySlice scalar = %#v", got)
	}
	ss := AsStrings([]int{1, 2, 3})
	if strings.Join(ss, ",") != "1,2,3" {
		t.Fatalf("AsStrings = %v", ss)
	}
}

func TestCopyValue(t *testing.T) {
	type Inner struct{ N int }
	type Outer struct {
		Name string
		Data []int
		M    map[string]Inner
		P    *Inner
	}
	orig := Outer{
		Name: "x",
		Data: []int{1, 2},
		M:    map[string]Inner{"a": {N: 7}},
		P:    &Inner{N: 9},
	}
	cp := CopyValue(orig).(Outer)
	orig.Data[0] = 99
	orig.M["a"] = Inner{N: 8}
	orig.P.N = 10
	if cp.Data[0] != 1 || cp.M["a"].N != 7 || cp.P.N != 9 {
		t.Fatalf("CopyValue not deep copied: %#v", cp)
	}
}

func TestAsBytes(t *testing.T) {
	if got := string(AsBytes("abc")); got != "abc" {
		t.Fatalf("AsBytes string = %q", got)
	}
	if got := AsBytes(true); string(got) != "true" {
		t.Fatalf("AsBytes bool = %q", got)
	}
}

func TestAsString_Error(t *testing.T) {
	err := errors.New("boom")
	if got := AsString(err); got != "boom" {
		t.Fatalf("AsString(error) = %q", got)
	}
}
