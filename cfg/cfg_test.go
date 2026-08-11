package cfg

import (
	"os"
	"sync"
	"testing"
	"time"
)

// --- Format detection ---

func TestDetect_JSON(t *testing.T) {
	if f := detect([]byte(`{"a":1}`)); f != FormatJSON {
		t.Fatalf("expected JSON, got %v", f)
	}
}

func TestDetect_INI(t *testing.T) {
	if f := detect([]byte("[section]\nkey=val")); f != FormatINI {
		t.Fatalf("expected INI, got %v", f)
	}
	if f := detect([]byte("host = localhost")); f != FormatINI {
		t.Fatalf("expected INI for key=val, got %v", f)
	}
}

func TestDetect_Sfc(t *testing.T) {
	if f := detect([]byte("server:\n  port: 8080")); f != FormatSfc {
		t.Fatalf("expected Sfc, got %v", f)
	}
}

// --- INI parsing ---

func TestParseINI_Basic(t *testing.T) {
	input := `
# comment
host = localhost
port = 8080
debug = true

[database]
name = mydb
timeout = 30
`
	c, err := Parse([]byte(input), FormatINI)
	if err != nil {
		t.Fatal(err)
	}
	if v := c.StringOr("host", ""); v != "localhost" {
		t.Errorf("host = %q, want localhost", v)
	}
	if v := c.IntOr("port", 0); v != 8080 {
		t.Errorf("port = %d, want 8080", v)
	}
	if v := c.BoolOr("debug", false); !v {
		t.Error("debug should be true")
	}
	if v := c.StringOr("database.name", ""); v != "mydb" {
		t.Errorf("database.name = %q, want mydb", v)
	}
	if v := c.IntOr("database.timeout", 0); v != 30 {
		t.Errorf("database.timeout = %d, want 30", v)
	}
}

func TestParseINI_Quoted(t *testing.T) {
	c, err := Parse([]byte(`msg = "hello world"`), FormatINI)
	if err != nil {
		t.Fatal(err)
	}
	if v := c.StringOr("msg", ""); v != "hello world" {
		t.Errorf("msg = %q, want 'hello world'", v)
	}
}

func TestParseINI_CommaSeparated(t *testing.T) {
	c, err := Parse([]byte(`hosts = a,b,c`), FormatINI)
	if err != nil {
		t.Fatal(err)
	}
	ss := c.StringsOr("hosts", nil)
	if len(ss) != 3 || ss[0] != "a" || ss[2] != "c" {
		t.Errorf("hosts = %v, want [a b c]", ss)
	}
}

// --- YAML parsing ---

func TestParseYAML_Basic(t *testing.T) {
	input := `
server:
  host: localhost
  port: 8080
  debug: true

database:
  name: mydb
  timeout: 30
`
	c, err := Parse([]byte(input), FormatSfc)
	if err != nil {
		t.Fatal(err)
	}
	if v := c.StringOr("server.host", ""); v != "localhost" {
		t.Errorf("server.host = %q", v)
	}
	if v := c.IntOr("server.port", 0); v != 8080 {
		t.Errorf("server.port = %d", v)
	}
	if v := c.BoolOr("server.debug", false); !v {
		t.Error("server.debug should be true")
	}
	if v := c.StringOr("database.name", ""); v != "mydb" {
		t.Errorf("database.name = %q", v)
	}
}

func TestParseYAML_List(t *testing.T) {
	input := `
items:
  - alpha
  - beta
  - gamma
`
	c, err := Parse([]byte(input), FormatSfc)
	if err != nil {
		t.Fatal(err)
	}
	ss := c.StringsOr("items", nil)
	if len(ss) != 3 || ss[0] != "alpha" || ss[2] != "gamma" {
		t.Errorf("items = %v", ss)
	}
}

func TestParseYAML_FlowSequence(t *testing.T) {
	input := `tags: [go, yaml, config]`
	c, err := Parse([]byte(input), FormatSfc)
	if err != nil {
		t.Fatal(err)
	}
	ss := c.StringsOr("tags", nil)
	if len(ss) != 3 || ss[0] != "go" {
		t.Errorf("tags = %v", ss)
	}
}

func TestParseYAML_FlowMapping(t *testing.T) {
	input := `point: {x: 1, y: 2}`
	c, err := Parse([]byte(input), FormatSfc)
	if err != nil {
		t.Fatal(err)
	}
	sec, ok := c.Section("point")
	if !ok {
		t.Fatal("point section not found")
	}
	if v := sec.IntOr("x", 0); v != 1 {
		t.Errorf("x = %d", v)
	}
}

func TestParseYAML_Multiline(t *testing.T) {
	input := "desc: |\n  line1\n  line2\n  line3\n"
	c, err := Parse([]byte(input), FormatSfc)
	if err != nil {
		t.Fatal(err)
	}
	v := c.StringOr("desc", "")
	if v != "line1\nline2\nline3" {
		t.Errorf("desc = %q", v)
	}
}

func TestParseYAML_FoldedMultiline(t *testing.T) {
	input := "desc: >\n  hello\n  world\n"
	c, err := Parse([]byte(input), FormatSfc)
	if err != nil {
		t.Fatal(err)
	}
	v := c.StringOr("desc", "")
	if v != "hello world" {
		t.Errorf("desc = %q, want 'hello world'", v)
	}
}

// --- JSON parsing ---

func TestParseJSON_Basic(t *testing.T) {
	input := `{"host":"localhost","port":8080,"debug":true,"ratio":3.14}`
	c, err := Parse([]byte(input), FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	if v := c.StringOr("host", ""); v != "localhost" {
		t.Errorf("host = %q", v)
	}
	if v := c.IntOr("port", 0); v != 8080 {
		t.Errorf("port = %d", v)
	}
	if v := c.BoolOr("debug", false); !v {
		t.Error("debug should be true")
	}
	if v := c.Float64Or("ratio", 0); v != 3.14 {
		t.Errorf("ratio = %f", v)
	}
}

func TestParseJSON_Nested(t *testing.T) {
	input := `{"db":{"host":"pg","port":5432}}`
	c, err := Parse([]byte(input), FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	if v := c.StringOr("db.host", ""); v != "pg" {
		t.Errorf("db.host = %q", v)
	}
	if v := c.IntOr("db.port", 0); v != 5432 {
		t.Errorf("db.port = %d", v)
	}
}

// --- Typed getters ---

func TestGetters_Missing(t *testing.T) {
	c := New()
	if _, err := c.String("nope"); err == nil {
		t.Error("expected error for missing key")
	}
	if _, err := c.Int("nope"); err == nil {
		t.Error("expected error for missing key")
	}
}

func TestDuration(t *testing.T) {
	c := New()
	c.Set("t", "5s")
	d, err := c.Duration("t")
	if err != nil {
		t.Fatal(err)
	}
	if d != 5*time.Second {
		t.Errorf("d = %v, want 5s", d)
	}
}

// --- Set / CoW ---

func TestSet_Dotted(t *testing.T) {
	c := New()
	c.Set("a.b.c", 42)
	if v := c.IntOr("a.b.c", 0); v != 42 {
		t.Errorf("a.b.c = %d", v)
	}
}

// --- Merge ---

func TestMerge(t *testing.T) {
	c1, _ := Parse([]byte(`{"a":1,"b":2}`), FormatJSON)
	c2, _ := Parse([]byte(`{"b":99,"c":3}`), FormatJSON)
	c1.Merge(c2)
	if v := c1.IntOr("a", 0); v != 1 {
		t.Errorf("a = %d", v)
	}
	if v := c1.IntOr("b", 0); v != 99 {
		t.Errorf("b = %d, want 99", v)
	}
	if v := c1.IntOr("c", 0); v != 3 {
		t.Errorf("c = %d", v)
	}
}

// --- Env expansion ---

func TestExpandEnv(t *testing.T) {
	os.Setenv("CFG_TEST_HOST", "prod-server")
	defer os.Unsetenv("CFG_TEST_HOST")

	c, _ := Parse([]byte(`{"host":"${CFG_TEST_HOST}","port":"${NOEXIST:9090}"}`), FormatJSON)
	expanded := c.ExpandEnv()
	if v := expanded.StringOr("host", ""); v != "prod-server" {
		t.Errorf("host = %q, want prod-server", v)
	}
	if v := expanded.StringOr("port", ""); v != "9090" {
		t.Errorf("port = %q, want 9090", v)
	}
}

// --- Bind scalar ---

func TestBind_Scalar(t *testing.T) {
	c, _ := Parse([]byte(`{"name":"test","count":42,"ok":true}`), FormatJSON)
	var s string
	var n int
	var b bool
	c.Bind("name", &s)
	c.Bind("count", &n)
	c.Bind("ok", &b)
	if s != "test" {
		t.Errorf("s = %q", s)
	}
	if n != 42 {
		t.Errorf("n = %d", n)
	}
	if !b {
		t.Error("b should be true")
	}
}

// --- Bind struct ---

func TestBind_Struct(t *testing.T) {
	input := `
server:
  host: 0.0.0.0
  port: 3000
  timeout: 5s
  tags: [web, api]
`
	type Server struct {
		Host    string        `cfg:"host"`
		Port    int           `cfg:"port"`
		Timeout time.Duration `cfg:"timeout"`
		Tags    []string      `cfg:"tags"`
	}

	c, err := Parse([]byte(input), FormatSfc)
	if err != nil {
		t.Fatal(err)
	}
	var srv Server
	c.Bind("server", &srv)
	if srv.Host != "0.0.0.0" {
		t.Errorf("Host = %q", srv.Host)
	}
	if srv.Port != 3000 {
		t.Errorf("Port = %d", srv.Port)
	}
	if srv.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v", srv.Timeout)
	}
	if len(srv.Tags) != 2 || srv.Tags[0] != "web" {
		t.Errorf("Tags = %v", srv.Tags)
	}
}

func TestBind_NestedStruct(t *testing.T) {
	input := `{"app":{"db":{"host":"pg","port":5432},"name":"myapp"}}`
	type DB struct {
		Host string `cfg:"host"`
		Port int    `cfg:"port"`
	}
	type App struct {
		Name string `cfg:"name"`
		DB   DB     `cfg:"db"`
	}

	c, _ := Parse([]byte(input), FormatJSON)
	var app App
	c.Bind("app", &app)
	if app.Name != "myapp" {
		t.Errorf("Name = %q", app.Name)
	}
	if app.DB.Host != "pg" {
		t.Errorf("DB.Host = %q", app.DB.Host)
	}
	if app.DB.Port != 5432 {
		t.Errorf("DB.Port = %d", app.DB.Port)
	}
}

// --- Validate ---

func TestValidate_Required(t *testing.T) {
	c := New()
	err := c.Validate(Schema{
		"host": {Required: true},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	ve := err.(*ValidationError)
	if len(ve.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(ve.Errors))
	}
}

func TestValidate_TypeMismatch(t *testing.T) {
	c, _ := Parse([]byte(`{"port":"not_a_number"}`), FormatJSON)
	err := c.Validate(Schema{
		"port": {Type: "int"},
	})
	if err == nil {
		t.Fatal("expected validation error for type mismatch")
	}
}

func TestValidate_Range(t *testing.T) {
	c, _ := Parse([]byte(`{"port":100}`), FormatJSON)
	err := c.Validate(Schema{
		"port": {Type: "int", Min: MinInt64(1024), Max: MaxInt64(65535)},
	})
	if err == nil {
		t.Fatal("expected validation error for range")
	}
}

func TestValidate_Pattern(t *testing.T) {
	c, _ := Parse([]byte(`{"email":"bad"}`), FormatJSON)
	err := c.Validate(Schema{
		"email": {Pattern: `^.+@.+\..+$`},
	})
	if err == nil {
		t.Fatal("expected validation error for pattern")
	}
	// Valid case
	c2, _ := Parse([]byte(`{"email":"a@b.c"}`), FormatJSON)
	if err2 := c2.Validate(Schema{"email": {Pattern: `^.+@.+\..+$`}}); err2 != nil {
		t.Fatalf("unexpected error: %v", err2)
	}
}

func TestValidate_OneOf(t *testing.T) {
	c, _ := Parse([]byte(`{"env":"staging"}`), FormatJSON)
	err := c.Validate(Schema{
		"env": {OneOf: []string{"dev", "prod"}},
	})
	if err == nil {
		t.Fatal("expected validation error for OneOf")
	}
}

func TestValidate_Pass(t *testing.T) {
	c, _ := Parse([]byte(`{"host":"localhost","port":8080}`), FormatJSON)
	err := c.Validate(Schema{
		"host": {Type: "string", Required: true},
		"port": {Type: "int", Min: MinInt64(1), Max: MaxInt64(65535)},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Section / Map / Each ---

func TestSection(t *testing.T) {
	c, _ := Parse([]byte(`{"db":{"host":"pg","port":5432}}`), FormatJSON)
	sec, ok := c.Section("db")
	if !ok {
		t.Fatal("section not found")
	}
	if v := sec.StringOr("host", ""); v != "pg" {
		t.Errorf("host = %q", v)
	}
}

func TestMap(t *testing.T) {
	c, _ := Parse([]byte(`{"labels":{"env":"prod","team":"core"}}`), FormatJSON)
	m, ok := c.Map("labels")
	if !ok {
		t.Fatal("map not found")
	}
	if m["env"] != "prod" || m["team"] != "core" {
		t.Errorf("m = %v", m)
	}
}

func TestEach(t *testing.T) {
	input := `
items:
  - name: a
    value: 1
  - name: b
    value: 2
`
	c, _ := Parse([]byte(input), FormatSfc)
	var names []string
	c.Each("items", func(i int, sub *Config) bool {
		names = append(names, sub.StringOr("name", ""))
		return true
	})
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Errorf("names = %v", names)
	}
}

// --- Concurrent safety ---

func TestConcurrency(t *testing.T) {
	c, _ := Parse([]byte(`{"counter":0}`), FormatJSON)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			c.Set("counter", n)
		}(i)
		go func() {
			defer wg.Done()
			c.IntOr("counter", 0)
		}()
	}
	wg.Wait()
	// Just ensure no panic/race
}

// --- Load from file ---

func TestLoad_File(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/test.ini"
	os.WriteFile(path, []byte("[app]\nname = hello\nport = 9090\n"), 0644)

	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if v := c.StringOr("app.name", ""); v != "hello" {
		t.Errorf("app.name = %q", v)
	}
	if v := c.IntOr("app.port", 0); v != 9090 {
		t.Errorf("app.port = %d", v)
	}
	if c.File() != path {
		t.Errorf("File() = %q", c.File())
	}
}

// --- Edge cases ---

func TestEmpty(t *testing.T) {
	c, err := Parse([]byte(""), FormatINI)
	if err != nil {
		t.Fatal(err)
	}
	if c.Has("anything") {
		t.Error("empty config should have no keys")
	}
}

func TestYAML_Null(t *testing.T) {
	c, _ := Parse([]byte("val: null\nval2: ~"), FormatSfc)
	v, ok := c.Raw("val")
	if !ok {
		t.Fatal("val should exist")
	}
	if v != nil {
		t.Errorf("val = %v, want nil", v)
	}
}

func TestINI_BoolVariants(t *testing.T) {
	input := "a = yes\nb = no\nc = on\nd = off\n"
	c, _ := Parse([]byte(input), FormatINI)
	if v := c.BoolOr("a", false); !v {
		t.Error("a should be true")
	}
	if v := c.BoolOr("b", true); v {
		t.Error("b should be false")
	}
	if v := c.BoolOr("c", false); !v {
		t.Error("c should be true")
	}
	if v := c.BoolOr("d", true); v {
		t.Error("d should be false")
	}
}

// --- Deep copy / no leak tests ---

// TestAsMap_DeepCopy verifies that AsMap returns a deep copy: mutating the
// returned map must not affect the internal copy-on-write tree.
func TestAsMap_DeepCopy(t *testing.T) {
	c, _ := Parse([]byte(`{"db":{"host":"pg","port":5432}}`), FormatJSON)
	m := c.AsMap()
	// Mutate the returned map.
	m["db"].(map[string]any)["host"] = "HACKED"
	delete(m, "db")
	// Original config must be unaffected.
	if v := c.StringOr("db.host", ""); v != "pg" {
		t.Errorf("db.host = %q, want pg (AsMap leaked internal map)", v)
	}
}

// TestRaw_DeepCopy verifies that Raw returns deep-copied map/slice values.
func TestRaw_DeepCopy(t *testing.T) {
	c, _ := Parse([]byte(`{"db":{"host":"pg"},"tags":["a","b"]}`), FormatJSON)

	// Map value.
	v, ok := c.Raw("db")
	if !ok {
		t.Fatal("Raw(db) not found")
	}
	v.(map[string]any)["host"] = "HACKED"
	if v := c.StringOr("db.host", ""); v != "pg" {
		t.Errorf("db.host = %q, want pg (Raw leaked internal map)", v)
	}

	// Slice value.
	v, ok = c.Raw("tags")
	if !ok {
		t.Fatal("Raw(tags) not found")
	}
	v.([]any)[0] = "HACKED"
	ss := c.StringsOr("tags", nil)
	if ss[0] != "a" {
		t.Errorf("tags[0] = %q, want a (Raw leaked internal slice)", ss[0])
	}
}

// TestSection_DeepCopy verifies that Section returns an independent copy:
// mutating the sub-Config must not affect the parent.
func TestSection_DeepCopy(t *testing.T) {
	c, _ := Parse([]byte(`{"db":{"host":"pg","port":5432}}`), FormatJSON)
	sec, ok := c.Section("db")
	if !ok {
		t.Fatal("section not found")
	}
	sec.Set("host", "HACKED")
	// Parent must be unaffected.
	if v := c.StringOr("db.host", ""); v != "pg" {
		t.Errorf("db.host = %q, want pg (Section leaked internal map)", v)
	}
}

// TestSlice_DeepCopy verifies that Slice returns independent sub-Configs.
func TestSlice_DeepCopy(t *testing.T) {
	input := `
items:
  - name: alpha
    v: 1
  - name: beta
    v: 2
`
	c, _ := Parse([]byte(input), FormatSfc)
	subs, ok := c.Slice("items")
	if !ok {
		t.Fatal("slice not found")
	}
	subs[0].Set("name", "HACKED")
	// Parent must be unaffected.
	c.Each("items", func(i int, sub *Config) bool {
		if i == 0 {
			if v := sub.StringOr("name", ""); v != "alpha" {
				t.Errorf("items[0].name = %q, want alpha (Slice leaked)", v)
			}
		}
		return true
	})
}
