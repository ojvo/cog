package util

import (
	"encoding"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSimple(t *testing.T) {
	t.Parallel()
	type config struct {
		String string `clap:"--string"`
	}
	cfg := &config{}
	results, err := ClapParse([]string{"--string", "hello"}, cfg)
	if err != nil {
		t.Fatalf("parsing error: %s", err)
	}
	t.Logf("results: %+v\n", results)
	wanted := &config{String: "hello"}
	if !reflect.DeepEqual(cfg, wanted) {
		t.Errorf("wanted: '%v', got '%v'", wanted, cfg)
	}
}

func TestUnexpectedArgument(t *testing.T) {
	t.Parallel()
	type config struct {
		String string `clap:"--string"`
		Int    int    `clap:"--int"`
	}
	cfg := &config{}
	results, err := ClapParse([]string{"--string", "hello", "--unexpected", "world", "--int", "10"}, cfg)
	if err != nil {
		t.Fatalf("parsing error: %s", err)
	}
	t.Logf("results: %+v\n", results)
	if !results.HasWarnings() {
		t.Errorf("expected warnings for --unexpected / world")
	}
	wanted := &config{String: "hello", Int: 10}
	if !reflect.DeepEqual(cfg, wanted) {
		t.Errorf("wanted: '%v', got '%v'", wanted, cfg)
	}
}

func TestMandatoryArgument(t *testing.T) {
	t.Parallel()
	type config struct {
		String string `clap:"--string,mandatory"`
	}
	cfg := &config{}
	_, err := ClapParse([]string{}, cfg)
	if err == nil {
		t.Fatal("expected error for missing mandatory argument")
	}
	if !errors.Is(err, ErrMandatoryArgument) {
		t.Errorf("expected ErrMandatoryArgument, got: '%v'", err)
	}
}

func TestShort(t *testing.T) {
	t.Parallel()
	type config struct {
		String string `clap:",-s"`
	}
	cfg := &config{}
	results, err := ClapParse([]string{"-s", "hello"}, cfg)
	if err != nil {
		t.Fatalf("parsing error: %s", err)
	}
	t.Logf("results: %+v\n", results)
	wanted := &config{String: "hello"}
	if !reflect.DeepEqual(cfg, wanted) {
		t.Errorf("wanted: '%v', got '%v'", wanted, cfg)
	}
}

func TestMandatoryShort(t *testing.T) {
	t.Parallel()
	type config struct {
		String string `clap:",-s,mandatory"`
	}
	cfg := &config{}
	_, err := ClapParse([]string{}, cfg)
	if err == nil {
		t.Fatal("expected error for missing mandatory short argument")
	}
	if !errors.Is(err, ErrMandatoryArgument) {
		t.Errorf("expected ErrMandatoryArgument, got: '%v'", err)
	}
}

func TestInvalidTag(t *testing.T) {
	t.Parallel()
	type config struct {
		String string `clap:"--string,-s,-S"`
	}
	cfg := &config{}
	_, err := ClapParse([]string{"--string", "hello"}, cfg)
	if err == nil {
		t.Fatal("expected error for invalid tag")
	}
	t.Logf("error: %v", err)
}

func TestInvalidTagShort(t *testing.T) {
	t.Parallel()
	type config struct {
		String string `clap:",-s,-S"`
	}
	cfg := &config{}
	_, err := ClapParse([]string{"-s", "hello"}, cfg)
	if err == nil {
		t.Fatal("expected error for invalid tag")
	}
	t.Logf("error: %v", err)
}

func TestTrailing(t *testing.T) {
	t.Parallel()
	type config struct {
		Int      int      `clap:"--int"`
		Trailing []string `clap:"trailing"`
	}
	cfg := &config{}
	results, err := ClapParse([]string{"--int", "10", "trailing1", "trailing2"}, cfg)
	if err != nil {
		t.Fatalf("parsing error: %s", err)
	}
	t.Logf("results: %+v\n", results)
	wanted := &config{Int: 10, Trailing: []string{"trailing1", "trailing2"}}
	if !reflect.DeepEqual(cfg, wanted) {
		t.Errorf("wanted: '%v', got '%v'", wanted, cfg)
	}
}

func TestTrailingWithIgnored(t *testing.T) {
	t.Parallel()
	type config struct {
		Int      int      `clap:"--int"`
		Trailing []string `clap:"trailing"`
	}
	cfg := &config{}
	results, err := ClapParse([]string{"--int", "10", "--unknown", "trailing1"}, cfg)
	if err != nil {
		t.Fatalf("parsing error: %s", err)
	}
	if !results.HasWarnings() || len(results.Ignored) < 1 {
		t.Errorf("expected warnings for unknown flag, got: %+v", results)
	}
	t.Logf("results: %+v\n", results)
}

func TestSlice(t *testing.T) {
	t.Parallel()
	type config struct {
		Bool  bool  `clap:"-O"`
		Short bool  `clap:"--x"`
		Slice []int `clap:"--int-slice,-I"`
	}
	cfg := &config{}
	results, err := ClapParse([]string{
		"image", "-x", "shortparam", "-O", "-I", "42", "-S", "22", "32", "42",
	}, cfg)
	if err != nil {
		t.Fatalf("parsing error: %s", err)
	}
	t.Logf("results: %+v\n", results)
	wanted := &config{Bool: true, Slice: []int{42}}
	if !reflect.DeepEqual(cfg, wanted) {
		t.Errorf("wanted: '%v', got '%v'", wanted, cfg)
	}
	if !results.HasWarnings() || len(results.Ignored) != 4 {
		t.Errorf("expected 4 ignored, got %d: %+v", len(results.Ignored), results)
	}
}

func TestComplete(t *testing.T) {
	t.Parallel()
	type config struct {
		Extensions  []string `clap:"--extensions,-e,mandatory"`
		Recursive   bool     `clap:"--recursive,-r"`
		Verbose     bool     `clap:"--verbose,-v"`
		Size        int      `clap:"--size,-s"`
		Directories []string `clap:"trailing"`
	}
	cfg := &config{}
	results, err := ClapParse([]string{
		"--extensions", "jpg", "png", "bmp", "-v", "-s", "10", "$home/temp", "$home/tmp", "/tmp",
	}, cfg)
	if err != nil {
		t.Fatalf("parsing error: %s", err)
	}
	t.Logf("results: %+v\n", results)
	wanted := &config{
		Extensions: []string{"jpg", "png", "bmp"}, Verbose: true, Size: 10,
		Directories: []string{"$home/temp", "$home/tmp", "/tmp"},
	}
	if !reflect.DeepEqual(cfg, wanted) {
		t.Errorf("wanted: '%v', got '%v'", wanted, cfg)
	}
}

func TestBooleans(t *testing.T) {
	t.Parallel()
	type config struct {
		Recursive bool `clap:"--recursive,-R"`
	}
	cfg := &config{Recursive: false}
	results, err := ClapParse([]string{"--recursive"}, cfg)
	if err != nil {
		t.Fatalf("parsing error: %s", err)
	}
	t.Logf("results: %+v\n", results)
	if !cfg.Recursive {
		t.Errorf("expected Recursive=true")
	}
	cfg = &config{Recursive: true}
	_, err = ClapParse([]string{"--no-recursive"}, cfg)
	if err != nil {
		t.Fatalf("parsing error: %s", err)
	}
	if cfg.Recursive {
		t.Errorf("expected Recursive=false after --no-recursive")
	}
}

func TestTypes(t *testing.T) {
	t.Parallel()
	type config struct {
		String      string    `clap:"--string"`
		Int         int       `clap:"--int"`
		Int8        int8      `clap:"--int8"`
		Int16       int16     `clap:"--int16"`
		Int32       int32     `clap:"--int32"`
		Int64       int64     `clap:"--int64"`
		UInt        uint      `clap:"--uint"`
		UInt8       uint8     `clap:"--uint8"`
		UInt16      uint16    `clap:"--uint16"`
		UInt32      uint32    `clap:"--uint32"`
		UInt64      uint64    `clap:"--uint64"`
		Float32     float32   `clap:"--float32"`
		Float64     float64   `clap:"--float64"`
		Bool        bool      `clap:"--bool"`
		DefaultTrue bool      `clap:"--defaulttrue"`
		StringSlice []string  `clap:"--string-slice"`
		IntSlice    []int     `clap:"--int-slice"`
		StringArray [2]string `clap:"--string-array"`
		IntArray    [3]int    `clap:"--int-array"`
		Trailing    []string  `clap:"trailing"`
	}
	cfg := &config{DefaultTrue: true}
	results, err := ClapParse([]string{
		"--string", "str", "--int", "10", "--int8", "8", "--int16", "16", "--int32", "32", "--int64", "64",
		"--uint", "12", "--uint8", "255", "--uint16", "65535", "--uint32", "65535", "--uint64", "65535",
		"--float32", "12.32", "--float64", "12.64", "--bool", "--no-defaulttrue", "--string-slice", "a", "b", "c",
		"--int-slice", "10", "11", "12", "--string-array", "a", "b", "--int-array", "10", "11", "12",
		"w", "x", "y", "z",
	}, cfg)
	if err != nil {
		t.Fatalf("parsing error: %s", err)
	}
	t.Logf("results: %+v\n", results)
	wanted := &config{
		String: "str", Int: 10, Int8: 8, Int16: 16, Int32: 32, Int64: 64,
		UInt: 12, UInt8: 255, UInt16: 65535, UInt32: 65535, UInt64: 65535,
		Float32: 12.32, Float64: 12.64, Bool: true, DefaultTrue: false,
		StringSlice: []string{"a", "b", "c"}, IntSlice: []int{10, 11, 12},
		StringArray: [2]string{"a", "b"}, IntArray: [3]int{10, 11, 12},
		Trailing: []string{"w", "x", "y", "z"},
	}
	if !reflect.DeepEqual(cfg, wanted) {
		t.Errorf("wanted: '%v', got '%v'", wanted, cfg)
	}
}

func TestReadme(t *testing.T) {
	t.Parallel()
	type config struct {
		Cookie      string    `clap:"--cookie"`
		HTTPOnly    bool      `clap:"--httpOnly"`
		Secure      bool      `clap:"--secure"`
		Origins     [4]string `clap:"--origins,-O,mandatory"`
		Port        int       `clap:",-P,mandatory"`
		ConfigFiles []string  `clap:"trailing"`
	}
	cfg := &config{Secure: true}
	results, err := ClapParse([]string{
		"-P", "8080", "--cookie", "clapcookie", "--httpOnly", "--origins", "http://localhost:5137",
		"https://localhost:5173", "http://localhost:3000", "https://localhost:3000",
		"config-db.json", "config-log.json",
	}, cfg)
	if err != nil {
		t.Fatalf("parsing error: %s", err)
	}
	t.Logf("results: %+v\n", results)
	wanted := &config{
		Cookie: "clapcookie", HTTPOnly: true, Secure: true,
		Origins: [4]string{
			"http://localhost:5137", "https://localhost:5173",
			"http://localhost:3000", "https://localhost:3000",
		},
		Port: 8080, ConfigFiles: []string{"config-db.json", "config-log.json"},
	}
	if !reflect.DeepEqual(cfg, wanted) {
		t.Errorf("wanted: '%v', got '%v'", wanted, cfg)
	}
}

func TestInvalidIntSlice(t *testing.T) {
	t.Parallel()
	type config struct {
		IntSlice []int `clap:"--int-slice"`
	}
	cfg := &config{}
	_, err := ClapParse([]string{"--int-slice", "10", "foo", "12"}, cfg)
	if err == nil {
		t.Fatal("expected error for non-integer value in slice")
	}
	t.Logf("error: %v", err)
}

func TestInvalidIntArray(t *testing.T) {
	t.Parallel()
	type config struct {
		IntArray [3]int `clap:"--int-array"`
	}
	cfg := &config{}
	_, err := ClapParse([]string{"--int-array", "10", "foo", "12"}, cfg)
	if err == nil {
		t.Fatal("expected error for non-integer value in array")
	}
	t.Logf("error: %v", err)
}

func TestDuplicatedArgument(t *testing.T) {
	t.Parallel()
	type config struct {
		String string `clap:"--string"`
	}
	cfg := &config{}
	_, err := ClapParse([]string{"--string", "hello", "--string", "world"}, cfg)
	if err == nil {
		t.Fatal("expected error for duplicated argument")
	}
	if !errors.Is(err, ErrDuplicatedArgument) {
		t.Errorf("expected ErrDuplicatedArgument, got: '%v'", err)
	}
}

func TestMissingArgument(t *testing.T) {
	t.Parallel()
	type config struct {
		String string `clap:"--string"`
		Int    int    `clap:"--int"`
	}
	cfg := &config{}
	_, err := ClapParse([]string{"--string", "--string", "world"}, cfg)
	if err == nil {
		t.Fatal("expected error for missing argument value (flag consumed as value)")
	}
	t.Logf("error: %v", err)

	_, err = ClapParse([]string{"--string", "world", "--int"}, cfg)
	if err == nil {
		t.Fatal("expected error for --int without value")
	}
	t.Logf("error: %v", err)
}

func TestMissingSliceArgument(t *testing.T) {
	t.Parallel()
	type config struct {
		IntSlice []int `clap:"--int-slice"`
	}
	cfg := &config{}
	_, err := ClapParse([]string{"--int-slice"}, cfg)
	if err == nil {
		t.Fatal("expected error for missing slice argument values")
	}
	t.Logf("error: %v", err)
}

func TestUintParsingError(t *testing.T) {
	t.Parallel()
	type config struct {
		Uint uint `clap:"--uint"`
	}
	cfg := &config{}
	_, err := ClapParse([]string{"--uint", "foo"}, cfg)
	if err == nil {
		t.Fatal("expected error for invalid uint")
	}
	t.Logf("error: %v", err)
}

func TestFloatParsingError(t *testing.T) {
	t.Parallel()
	type config struct {
		Float64 float64 `clap:"--float64"`
	}
	cfg := &config{}
	_, err := ClapParse([]string{"--float64", "foo"}, cfg)
	if err == nil {
		t.Fatal("expected error for invalid float")
	}
	t.Logf("error: %v", err)
}

func TestUnsettableField(t *testing.T) {
	t.Parallel()
	type config struct {
		aString string `clap:"--string"`
	}
	cfg := &config{}
	_, err := ClapParse([]string{"--string", "foo"}, cfg)
	if err != nil {
		t.Fatalf("unexpected error for unsettable field: %s", err)
	}
	_ = cfg.aString // silence unused warning
}

func TestNonEmptyTrailing(t *testing.T) {
	t.Parallel()
	type config struct {
		Field    string   `clap:"--string"`
		Trailing []string `clap:"trailing,-t"`
	}
	cfg := &config{}
	_, err := ClapParse([]string{"--string", "hello"}, cfg)
	if err == nil {
		t.Fatal("expected error for invalid trailing tag")
	}
	t.Logf("error: %v", err)
}

func TestNonStringTrailing(t *testing.T) {
	t.Parallel()
	type config struct {
		Field    string `clap:"--string"`
		Trailing int    `clap:"trailing"`
	}
	cfg := &config{}
	_, err := ClapParse([]string{"--string", "hello"}, cfg)
	if err == nil {
		t.Fatal("expected error for non-[]string trailing field")
	}
	t.Logf("error: %v", err)
}

func TestInvalidShortName(t *testing.T) {
	t.Parallel()
	type config struct {
		Field string `clap:",-s,foo,bar"`
	}
	cfg := &config{}
	_, err := ClapParse([]string{"-s", "hello"}, cfg)
	if err == nil {
		t.Fatal("expected error for invalid short name tag")
	}
	t.Logf("error: %v", err)
}

func TestNoShortName(t *testing.T) {
	t.Parallel()
	type config struct {
		Field string `clap:",-"`
	}
	cfg := &config{}
	_, err := ClapParse([]string{"-s", "hello"}, cfg)
	t.Logf("error: %v", err)
}

func TestInvalidShortNameParameter(t *testing.T) {
	t.Parallel()
	type config struct {
		Field string `clap:",-s,unexpected"`
	}
	cfg := &config{}
	_, err := ClapParse([]string{"-s", "hello"}, cfg)
	if err == nil {
		t.Fatal("expected error for invalid short name tag parameter")
	}
	t.Logf("error: %v", err)
}

// ---------------------------------------------------------------------------
// New tests from tmp/cor/clap
// ---------------------------------------------------------------------------

func TestDuration(t *testing.T) {
	t.Parallel()
	type DurationConfig struct {
		Timeout time.Duration `clap:"timeout,t" description:"timeout duration"`
	}
	args := []string{"--timeout", "1h30m"}
	cfg := &DurationConfig{}
	_, err := ClapParse(args, cfg)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Timeout != 90*time.Minute {
		t.Errorf("Expected Timeout to be 1h30m, got %v", cfg.Timeout)
	}
}

func TestUsage(t *testing.T) {
	type DurationConfig struct {
		Timeout time.Duration `clap:"timeout,t" description:"timeout duration"`
	}
	cfg := &DurationConfig{}
	usage := Usage(cfg)
	if !strings.Contains(usage, "--timeout") {
		t.Errorf("Usage missing --timeout")
	}
	if !strings.Contains(usage, "timeout duration") {
		t.Errorf("Usage missing description")
	}
}

func TestSlices(t *testing.T) {
	t.Parallel()
	type SliceConfig struct {
		Ints   []int64   `clap:"ints"`
		Floats []float64 `clap:"floats"`
	}
	args := []string{"--ints", "100", "200", "--floats", "1.5", "2.5"}
	cfg := &SliceConfig{}
	_, err := ClapParse(args, cfg)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(cfg.Ints) != 2 || cfg.Ints[0] != 100 || cfg.Ints[1] != 200 {
		t.Errorf("Expected Ints to be [100, 200], got %v", cfg.Ints)
	}
	if len(cfg.Floats) != 2 || cfg.Floats[0] != 1.5 || cfg.Floats[1] != 2.5 {
		t.Errorf("Expected Floats to be [1.5, 2.5], got %v", cfg.Floats)
	}
}

// IP is a custom type implementing encoding.TextUnmarshaler.
type IP struct {
	addr string
}

func (i *IP) UnmarshalText(text []byte) error {
	i.addr = string(text)
	return nil
}

// Ensure IP implements encoding.TextUnmarshaler
var _ encoding.TextUnmarshaler = (*IP)(nil)

func TestCustomType(t *testing.T) {
	t.Parallel()
	type CustomConfig struct {
		Address IP `clap:"address,a"`
	}
	args := []string{"--address", "127.0.0.1"}
	cfg := &CustomConfig{}
	_, err := ClapParse(args, cfg)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Address.addr != "127.0.0.1" {
		t.Errorf("Expected Address to be 127.0.0.1, got %s", cfg.Address.addr)
	}
}

func TestPointers(t *testing.T) {
	t.Parallel()
	type PointerConfig struct {
		Count *int    `clap:"count"`
		Name  *string `clap:"name"`
		Flag  *bool   `clap:"flag"`
	}
	args := []string{"--count", "123", "--name", "pointer", "--flag"}
	cfg := &PointerConfig{}
	_, err := ClapParse(args, cfg)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Count == nil || *cfg.Count != 123 {
		t.Errorf("Expected Count to be 123")
	}
	if cfg.Name == nil || *cfg.Name != "pointer" {
		t.Errorf("Expected Name to be pointer")
	}
	if cfg.Flag == nil || !*cfg.Flag {
		t.Errorf("Expected Flag to be true")
	}
}

func TestUsageDefaults(t *testing.T) {
	defaultInt := 42
	type PointerConfig struct {
		Count *int    `clap:"count"`
		Name  *string `clap:"name"`
		Flag  *bool   `clap:"flag"`
	}
	cfg := &PointerConfig{
		Count: &defaultInt,
	}
	usage := Usage(cfg)
	if !strings.Contains(usage, "(default: 42)") {
		t.Errorf("Usage missing default value 42. Got: %s", usage)
	}

	type TestConfig struct {
		Verbose   bool     `clap:"verbose,v"`
		Name      string   `clap:"name,n,mandatory"`
		Count     int      `clap:"count,c"`
		Items     []string `clap:"items,i"`
		Recursive bool     `clap:"recursive"`
		Trailing  []string `clap:"trailing"`
	}
	cfg2 := &TestConfig{
		Count: 100,
	}
	usage2 := Usage(cfg2)
	if !strings.Contains(usage2, "(default: 100)") {
		t.Errorf("Usage missing default value 100. Got: %s", usage2)
	}
}

func TestLargeUint(t *testing.T) {
	t.Parallel()
	type LargeUintConfig struct {
		ID uint64 `clap:"id"`
	}
	args := []string{"--id", "18446744073709551615"}
	cfg := &LargeUintConfig{}
	_, err := ClapParse(args, cfg)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.ID != 18446744073709551615 {
		t.Errorf("Expected ID to be 18446744073709551615, got %v", cfg.ID)
	}
}

func TestArrays(t *testing.T) {
	t.Parallel()
	type ArrayConfig struct {
		Ints   [2]int64   `clap:"ints"`
		Floats [2]float64 `clap:"floats"`
	}
	args := []string{"--ints", "10", "20", "--floats", "1.1", "2.2"}
	cfg := &ArrayConfig{}
	_, err := ClapParse(args, cfg)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Ints[0] != 10 || cfg.Ints[1] != 20 {
		t.Errorf("Expected Ints to be [10, 20], got %v", cfg.Ints)
	}
	if cfg.Floats[0] != 1.1 || cfg.Floats[1] != 2.2 {
		t.Errorf("Expected Floats to be [1.1, 2.2], got %v", cfg.Floats)
	}
}

// ---------------------------------------------------------------------------
// New feature tests: greedy, once, env, description
// ---------------------------------------------------------------------------

func TestGreedy(t *testing.T) {
	t.Parallel()
	type config struct {
		Files []string `clap:"files,f,greedy"`
		Count int      `clap:"count,c"`
	}
	cfg := &config{}
	results, err := ClapParse([]string{"-f", "a.txt", "b.txt", "--count", "5", "-f", "c.txt"}, cfg)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	t.Logf("results: %+v", results)
	if len(cfg.Files) != 3 || cfg.Files[0] != "a.txt" || cfg.Files[1] != "b.txt" || cfg.Files[2] != "c.txt" {
		t.Errorf("Expected 3 files, got %v", cfg.Files)
	}
	if cfg.Count != 5 {
		t.Errorf("Expected Count=5, got %d", cfg.Count)
	}
}

func TestOnce(t *testing.T) {
	t.Parallel()
	type config struct {
		Name string `clap:"name,n,once"`
	}
	cfg := &config{}
	_, err := ClapParse([]string{"--name", "a", "--name", "b"}, cfg)
	if err == nil {
		t.Fatal("expected error for duplicated once argument")
	}
	if !errors.Is(err, ErrDuplicatedArgument) {
		t.Errorf("expected ErrDuplicatedArgument, got: '%v'", err)
	}
}

func TestDescription(t *testing.T) {
	type config struct {
		Name string `clap:"name,n,mandatory" description:"the user name"`
	}
	cfg := &config{}
	usage := Usage(cfg)
	if !strings.Contains(usage, "the user name") {
		t.Errorf("Usage missing description")
	}
	if !strings.Contains(usage, "(mandatory)") {
		t.Errorf("Usage missing mandatory marker")
	}
}

func TestEnvVariable(t *testing.T) {
	type config struct {
		Mode  string `clap:"mode,m" env:"CLAP_TEST_MODE"`
		Debug bool   `clap:"debug,d" env:"CLAP_TEST_DEBUG"`
	}
	os.Setenv("CLAP_TEST_MODE", "production")
	os.Setenv("CLAP_TEST_DEBUG", "true")
	defer os.Unsetenv("CLAP_TEST_MODE")
	defer os.Unsetenv("CLAP_TEST_DEBUG")

	cfg := &config{}
	_, err := ClapParse([]string{"-d"}, cfg) // -d overrides env
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Mode != "production" {
		t.Errorf("Expected Mode from env, got '%s'", cfg.Mode)
	}
	if !cfg.Debug {
		t.Errorf("Expected Debug=true from env + flag")
	}
}

func TestClapParseArg(t *testing.T) {
	// ClapParseArg uses os.Args, which depends on test environment
	// Just verify it's callable
	type config struct {
		Name string `clap:"name,n"`
	}
	cfg := &config{}
	results, err := ClapParseArg(cfg)
	if err != nil && !results.HasErrors() {
		t.Logf("ClapParseArg returned: %v", err)
	}
	// No assertion; behavior depends on os.Args
}

func TestUsageWithEnv(t *testing.T) {
	type config struct {
		Mode string `clap:"mode,m" env:"CLAP_TEST_MODE" description:"run mode"`
	}
	cfg := &config{Mode: "dev"}
	usage := Usage(cfg)
	if !strings.Contains(usage, "[env: CLAP_TEST_MODE]") {
		t.Errorf("Usage missing env annotation: %s", usage)
	}
	if !strings.Contains(usage, "(default: dev)") {
		t.Errorf("Usage missing default value annotation: %s", usage)
	}
}
