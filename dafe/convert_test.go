package dafe

import (
	"strings"
	"testing"

	"ojv/cog/util"
)

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
	if m["name"] != "Carol" || util.AsInt64(m["age"]) != 20 {
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

func TestConvertInto_NilInputs(t *testing.T) {
	if err := ConvertInto(nil, nil); err != nil {
		t.Fatal(err)
	}
}
