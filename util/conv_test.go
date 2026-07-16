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
	bits := uint64(0x3ff0000000000000) // 1.0
	if got := AsFloat64(bits); got != 1 {
		t.Fatalf("AsFloat64 bits = %v", got)
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

func TestAsMaps(t *testing.T) {
	type User struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	u := User{Name: "Alice", Age: 30}
	m := AsAnyMap(u)
	if m["name"] != "Alice" {
		t.Fatalf("AsAnyMap name = %#v", m)
	}
	sm := AsStringMap(u)
	if sm["age"] != "30" {
		t.Fatalf("AsStringMap age = %#v", sm)
	}
}

func TestConvertInto_MapToStruct(t *testing.T) {
	type User struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	var u User
	err := ConvertInto(map[string]any{"name": "Bob", "age": "40"}, &u)
	if err != nil {
		t.Fatal(err)
	}
	if u.Name != "Bob" || u.Age != 40 {
		t.Fatalf("ConvertInto map->struct = %#v", u)
	}
}

func TestConvertInto_StructToMap(t *testing.T) {
	type User struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	var m map[string]any
	err := ConvertInto(User{Name: "Carol", Age: 20}, &m)
	if err != nil {
		t.Fatal(err)
	}
	if m["name"] != "Carol" || AsInt64(m["age"]) != 20 {
		t.Fatalf("ConvertInto struct->map = %#v", m)
	}
}

func TestConvertInto_JSONString(t *testing.T) {
	type User struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	var u User
	err := ConvertInto(`{"name":"Dave","age":18}`, &u)
	if err != nil {
		t.Fatal(err)
	}
	if u.Name != "Dave" || u.Age != 18 {
		t.Fatalf("ConvertInto json = %#v", u)
	}
}

func TestConvertInto_Slice(t *testing.T) {
	var out []int
	if err := ConvertInto([]string{"1", "2", "3"}, &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 || out[0] != 1 || out[2] != 3 {
		t.Fatalf("ConvertInto slice = %#v", out)
	}
}

func TestConvertInto_NonPtr(t *testing.T) {
	var x int
	err := ConvertInto(1, x)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "指针") {
		t.Fatalf("unexpected error: %v", err)
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

func TestConvertOptionCustomTag(t *testing.T) {
	type Src struct {
		Value string `db:"v"`
	}
	type Dst struct {
		Value string `db:"v"`
	}
	var dst Dst
	if err := ConvertInto(Src{Value: "ok"}, &dst, ConvertOption{Tags: []string{"db"}}); err != nil {
		t.Fatal(err)
	}
	if dst.Value != "ok" {
		t.Fatalf("custom tag convert = %#v", dst)
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

func TestConvertInto_NilInputs(t *testing.T) {
	if err := ConvertInto(nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestAsString_Error(t *testing.T) {
	err := errors.New("boom")
	if got := AsString(err); got != "boom" {
		t.Fatalf("AsString(error) = %q", got)
	}
}
