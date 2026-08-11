package json

import "testing"

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"direct", `{"a":1}`, `{"a":1}`},
		{"direct_with_noise", `  {"a":1}  `, `{"a":1}`},
		{"json_fence", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"plain_fence", "```\n{\"a\":1}\n```", `{"a":1}`},
		{"fence_with_lang", "```go\n{\"a\":1}\n```", `{"a":1}`},
		{"balanced_after_prose", `here is the answer: {"a":1} done`, `{"a":1}`},
		{"skip_invalid_first", `{"bad"} trailing {"good":2}`, `{"good":2}`},
		{"nested", `prefix {"a":{"b":2}} suffix`, `{"a":{"b":2}}`},
		{"empty", "", ""},
		{"no_json", `no json here`, ""},
		{"invalid_only", `{not valid}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ExtractJSON(c.in)
			if got != c.want {
				t.Errorf("ExtractJSON(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestExtractJSONArray(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"direct", `[1,2,3]`, `[1,2,3]`},
		{"direct_with_noise", `  [1,2,3]  `, `[1,2,3]`},
		{"json_fence", "```json\n[1,2]\n```", `[1,2]`},
		{"plain_fence", "```\n[1,2]\n```", `[1,2]`},
		{"balanced_after_prose", `result: [1,2,3] done`, `[1,2,3]`},
		{"skip_invalid_first", `[bad] trailing [1,2]`, `[1,2]`},
		{"nested", `prefix [[1,2],[3]] suffix`, `[[1,2],[3]]`},
		{"empty", "", ""},
		{"no_json", `no json here`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ExtractJSONArray(c.in)
			if got != c.want {
				t.Errorf("ExtractJSONArray(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestExtract(t *testing.T) {
	type obj struct {
		A int `json:"a"`
	}
	var v obj
	if !Extract(`here is {"a":42} ok`, &v) {
		t.Fatal("Extract returned false")
	}
	if v.A != 42 {
		t.Errorf("A = %d, want 42", v.A)
	}

	// No valid JSON → false.
	if Extract(`no json`, &v) {
		t.Error("Extract should return false for no JSON")
	}
}

func TestExtractArray(t *testing.T) {
	var v []int
	if !ExtractArray(`result: [1,2,3] end`, &v) {
		t.Fatal("ExtractArray returned false")
	}
	if len(v) != 3 || v[0] != 1 || v[2] != 3 {
		t.Errorf("v = %v, want [1 2 3]", v)
	}

	if ExtractArray(`no json`, &v) {
		t.Error("ExtractArray should return false for no JSON")
	}
}

// TestExtractJSON_StringLiteralAware 锁定字符串字面量感知契约:
// 括号出现在 "..." 内部(含转义)时不参与配平,不会截断或误配。
func TestExtractJSON_StringLiteralAware(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"brace_inside_string", `log: {"msg":"}","a":1} ok`, `{"msg":"}","a":1}`},
		{"escaped_quote_inside_string", `x {"msg":"a\"}b","v":2} y`, `{"msg":"a\"}b","v":2}`},
		{"array_inside_string", `p {"note":"[ignored]","k":3} q`, `{"note":"[ignored]","k":3}`},
		{"braces_in_prose_after", `说 {"a":1} 后面还有 } 这个字`, `{"a":1}`},
		{"string_with_unescaped_brace_then_valid", `{"bad":"}","x":1} {"good":2}`, `{"bad":"}","x":1}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ExtractJSON(c.in); got != c.want {
				t.Errorf("ExtractJSON(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestExtractJSONArray_StringLiteralAware 锁定数组版本同样跳过字符串字面量。
func TestExtractJSONArray_StringLiteralAware(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"brace_inside_string", `log: ["}",1] ok`, `["}",1]`},
		{"object_inside_string", `p ["note","{ignored}"] q`, `["note","{ignored}"]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ExtractJSONArray(c.in); got != c.want {
				t.Errorf("ExtractJSONArray(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestExtractAny 锁定联合提取契约:对象/数组任一,取最先出现者。
func TestExtractAny(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"object", `here {"a":1}`, `{"a":1}`},
		{"array", `here [1,2]`, `[1,2]`},
		{"array_before_object", `x [1,2] then {"a":1}`, `[1,2]`},
		{"object_before_array", `x {"a":1} then [1,2]`, `{"a":1}`},
		{"json_fence_object", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"json_fence_array", "```json\n[1,2]\n```", `[1,2]`},
		{"plain_fence", "```\n[1,2]\n```", `[1,2]`},
		{"skip_invalid_object", `{"bad"} then [1,2]`, `[1,2]`},
		{"skip_invalid_array", `[bad] then {"a":1}`, `{"a":1}`},
		{"no_json", `nothing here`, ""},
		{"empty", "", ""},
		{"nested_object", `{"a":{"b":[1,2]}}`, `{"a":{"b":[1,2]}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ExtractAny(c.in); got != c.want {
				t.Errorf("ExtractAny(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestExtractAny_StringLiteralAware 锁定 ExtractAny 的字符串字面量感知与联合顺序。
func TestExtractAny_StringLiteralAware(t *testing.T) {
	// 对象里的字符串含 "]" / "}" 不能打断扫描;数组先出现则取数组。
	if got := ExtractAny(`x {"note":"] still inside","v":1} [9]`); got != `{"note":"] still inside","v":1}` {
		t.Errorf("got %q", got)
	}
	if got := ExtractAny(`x ["}",1] {"a":1}`); got != `["}",1]` {
		t.Errorf("got %q", got)
	}
}
