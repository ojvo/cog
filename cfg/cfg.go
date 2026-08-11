// Package cfg provides a minimal, high-performance configuration library.
// It supports INI and YAML formats with typed getters, struct binding,
// env-var expansion, and schema validation.
//
// Design principles:
//   - Lock-free reads via atomic copy-on-write
//   - No external dependencies (pure stdlib)
//   - Explicit env expansion (never implicit)
//   - Single canonical API surface
package cfg

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config holds parsed configuration with copy-on-write semantics.
// Reads are lock-free; writes serialize via mutex and swap atomically.
type Config struct {
	mu   sync.Mutex
	data atomic.Value // stores map[string]any
	file string       // source path (empty for in-memory)
}

// New creates an empty Config.
func New() *Config {
	c := &Config{}
	c.data.Store(make(map[string]any))
	return c
}

// NewConfig preserves the legacy constructor name.
func NewConfig() *Config { return New() }

// Load parses a file (format auto-detected) and returns a Config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := New()
	c.file = path
	if err := c.parse(data, FormatAuto); err != nil {
		return nil, err
	}
	return c, nil
}

// Parse loads config from raw bytes with explicit format.
func Parse(data []byte, format Format) (*Config, error) {
	c := New()
	if err := c.parse(data, format); err != nil {
		return nil, err
	}
	return c, nil
}

// ParseBytes preserves the legacy auto-detect parse API.
func (c *Config) ParseBytes(data []byte) error {
	return c.parse(data, FormatAuto)
}

// ParseBytesAs preserves the legacy explicit-format parse API.
func (c *Config) ParseBytesAs(data []byte, format Format) error {
	return c.parse(data, format)
}

// ParseFile preserves the legacy file parse API.
func (c *Config) ParseFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	c.file = path
	return c.parse(data, FormatAuto)
}

// Merge overlays another config's data onto this one (shallow merge at top level).
func (c *Config) Merge(other *Config) {
	c.mu.Lock()
	defer c.mu.Unlock()
	base := deepCopy(c.root())
	for k, v := range other.root() {
		base[k] = v
	}
	c.data.Store(base)
}

// File returns the source file path, or empty if in-memory.
func (c *Config) File() string { return c.file }

// --- Read API ---

func (c *Config) root() map[string]any {
	return c.data.Load().(map[string]any)
}

// get traverses dotted key path and returns the value.
func (c *Config) get(key string) (any, bool) {
	parts := strings.Split(key, ".")
	cur := c.root()
	for i, p := range parts {
		v, ok := cur[p]
		if !ok {
			return nil, false
		}
		if i == len(parts)-1 {
			return v, true
		}
		next, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		cur = next
	}
	return nil, false
}

// Has returns true if key exists.
func (c *Config) Has(key string) bool {
	_, ok := c.get(key)
	return ok
}

// Raw returns the untyped value at key. Map and slice values are deep-copied
// so callers cannot mutate the internal copy-on-write tree.
func (c *Config) Raw(key string) (any, bool) {
	v, ok := c.get(key)
	if !ok {
		return nil, false
	}
	return deepCopyVal(v), true
}

// String returns string value or error.
func (c *Config) String(key string) (string, error) {
	v, ok := c.get(key)
	if !ok {
		return "", fmt.Errorf("cfg: key %q not found", key)
	}
	return asString(v), nil
}

// Int returns int value or error.
func (c *Config) Int(key string) (int, error) {
	v, ok := c.get(key)
	if !ok {
		return 0, fmt.Errorf("cfg: key %q not found", key)
	}
	return asInt(v)
}

// Bool returns bool value or error.
func (c *Config) Bool(key string) (bool, error) {
	v, ok := c.get(key)
	if !ok {
		return false, fmt.Errorf("cfg: key %q not found", key)
	}
	return asBool(v)
}

// Float64 returns float64 value or error.
func (c *Config) Float64(key string) (float64, error) {
	v, ok := c.get(key)
	if !ok {
		return 0, fmt.Errorf("cfg: key %q not found", key)
	}
	return asFloat64(v)
}

// Duration returns time.Duration or error.
func (c *Config) Duration(key string) (time.Duration, error) {
	v, ok := c.get(key)
	if !ok {
		return 0, fmt.Errorf("cfg: key %q not found", key)
	}
	return asDuration(v)
}

// Strings returns []string value or error.
func (c *Config) Strings(key string) ([]string, error) {
	v, ok := c.get(key)
	if !ok {
		return nil, fmt.Errorf("cfg: key %q not found", key)
	}
	return asStrings(v)
}

// --- Or variants (with default) ---

func (c *Config) StringOr(key, def string) string {
	if v, ok := c.get(key); ok {
		return asString(v)
	}
	return def
}

func (c *Config) IntOr(key string, def int) int {
	if v, ok := c.get(key); ok {
		if i, err := asInt(v); err == nil {
			return i
		}
	}
	return def
}

func (c *Config) BoolOr(key string, def bool) bool {
	if v, ok := c.get(key); ok {
		if b, err := asBool(v); err == nil {
			return b
		}
	}
	return def
}

func (c *Config) Float64Or(key string, def float64) float64 {
	if v, ok := c.get(key); ok {
		if f, err := asFloat64(v); err == nil {
			return f
		}
	}
	return def
}

func (c *Config) DurationOr(key string, def time.Duration) time.Duration {
	if v, ok := c.get(key); ok {
		if d, err := asDuration(v); err == nil {
			return d
		}
	}
	return def
}

func (c *Config) StringsOr(key string, def []string) []string {
	if v, ok := c.get(key); ok {
		if ss, err := asStrings(v); err == nil {
			return ss
		}
	}
	return def
}

// Legacy getter shims.
func (c *Config) GetString(key string) string              { return c.StringOr(key, "") }
func (c *Config) GetInt(key string) int                    { return c.IntOr(key, 0) }
func (c *Config) GetBool(key string) bool                  { return c.BoolOr(key, false) }
func (c *Config) GetFloat64(key string) float64            { return c.Float64Or(key, 0) }
func (c *Config) GetDuration(key string) time.Duration     { return c.DurationOr(key, 0) }
func (c *Config) GetStrings(key string) []string           { return c.StringsOr(key, nil) }
func (c *Config) GetStringDefault(key, def string) string  { return c.StringOr(key, def) }
func (c *Config) GetIntDefault(key string, def int) int    { return c.IntOr(key, def) }
func (c *Config) GetBoolDefault(key string, def bool) bool { return c.BoolOr(key, def) }
func (c *Config) GetFloat64Default(key string, def float64) float64 {
	return c.Float64Or(key, def)
}
func (c *Config) GetDurationDefault(key string, def time.Duration) time.Duration {
	return c.DurationOr(key, def)
}

// --- Structural getters ---

// Section returns a sub-Config rooted at key (must be a map). The sub-Config
// holds a deep copy so mutations do not propagate back to the parent.
func (c *Config) Section(key string) (*Config, bool) {
	v, ok := c.get(key)
	if !ok {
		return nil, false
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	sub := New()
	sub.data.Store(deepCopy(m))
	return sub, true
}

// GetSection preserves the legacy section API.
func (c *Config) GetSection(key string) (*Config, bool) { return c.Section(key) }

// Slice returns []*Config for a list-of-maps key. Each sub-Config holds a
// deep copy so mutations do not propagate back to the parent.
func (c *Config) Slice(key string) ([]*Config, bool) {
	v, ok := c.get(key)
	if !ok {
		return nil, false
	}
	items, ok := v.([]any)
	if !ok {
		return nil, false
	}
	result := make([]*Config, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		sub := New()
		sub.data.Store(deepCopy(m))
		result = append(result, sub)
	}
	return result, len(result) > 0
}

// GetSlice preserves the legacy slice API.
func (c *Config) GetSlice(key string) ([]*Config, bool) { return c.Slice(key) }

// Map returns map[string]string for a map-valued key.
func (c *Config) Map(key string) (map[string]string, bool) {
	v, ok := c.get(key)
	if !ok {
		return nil, false
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	result := make(map[string]string, len(m))
	for k, val := range m {
		result[k] = asString(val)
	}
	return result, true
}

// GetMap preserves the legacy map API.
func (c *Config) GetMap(key string) map[string]string {
	m, _ := c.Map(key)
	return m
}

// Each iterates over a list-of-maps key, calling fn for each sub-Config.
// Each sub-Config holds a deep copy so mutations do not propagate back.
func (c *Config) Each(key string, fn func(int, *Config) bool) {
	v, ok := c.get(key)
	if !ok {
		return
	}
	items, ok := v.([]any)
	if !ok {
		return
	}
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		sub := New()
		sub.data.Store(deepCopy(m))
		if !fn(i, sub) {
			break
		}
	}
}

// AsMap returns the full config tree as map[string]any. The returned map is
// a deep copy; callers may freely mutate it without affecting the Config.
func (c *Config) AsMap() map[string]any { return deepCopy(c.root()) }

// --- Write API ---

// Set stores a value at the given dotted key (copy-on-write).
func (c *Config) Set(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	root := deepCopy(c.root())
	parts := strings.Split(key, ".")
	cur := root
	for _, p := range parts[:len(parts)-1] {
		v, ok := cur[p]
		if !ok {
			next := make(map[string]any)
			cur[p] = next
			cur = next
		} else if next, ok := v.(map[string]any); ok {
			cur = next
		} else {
			next := make(map[string]any)
			cur[p] = next
			cur = next
		}
	}
	cur[parts[len(parts)-1]] = value
	c.data.Store(root)
}

// --- Env Expansion ---

// ExpandEnv returns a new Config with all string values expanded via ${VAR} / ${VAR:default}.
func (c *Config) ExpandEnv() *Config {
	out := New()
	out.data.Store(expandMap(c.root()))
	out.file = c.file
	return out
}

// Expand expands ${VAR} and ${VAR:default} in a single string.
func Expand(s string) string {
	return os.Expand(s, func(k string) string {
		if idx := strings.Index(k, ":"); idx != -1 {
			if val, ok := os.LookupEnv(k[:idx]); ok {
				return val
			}
			return k[idx+1:]
		}
		return os.Getenv(k)
	})
}

func expandMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = expandValue(v)
	}
	return out
}

func expandValue(v any) any {
	switch val := v.(type) {
	case string:
		if strings.Contains(val, "${") {
			return Expand(val)
		}
		return val
	case map[string]any:
		return expandMap(val)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = expandValue(item)
		}
		return out
	default:
		return v
	}
}

// --- Internal converters ---

func asString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case bool:
		return strconv.FormatBool(val)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", val)
	}
}

func asInt(v any) (int, error) {
	switch val := v.(type) {
	case int:
		return val, nil
	case int64:
		return int(val), nil
	case float64:
		return int(val), nil
	case string:
		return strconv.Atoi(strings.TrimSpace(val))
	case bool:
		if val {
			return 1, nil
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("cfg: cannot convert %T to int", v)
	}
}

func asBool(v any) (bool, error) {
	switch val := v.(type) {
	case bool:
		return val, nil
	case string:
		return strconv.ParseBool(strings.TrimSpace(val))
	case int:
		return val != 0, nil
	case float64:
		return val != 0, nil
	default:
		return false, fmt.Errorf("cfg: cannot convert %T to bool", v)
	}
}

func asFloat64(v any) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(val), 64)
	default:
		return 0, fmt.Errorf("cfg: cannot convert %T to float64", v)
	}
}

func asDuration(v any) (time.Duration, error) {
	switch val := v.(type) {
	case time.Duration:
		return val, nil
	case string:
		return time.ParseDuration(strings.TrimSpace(val))
	case int:
		return time.Duration(val) * time.Millisecond, nil
	case float64:
		return time.Duration(val * float64(time.Millisecond)), nil
	default:
		return 0, fmt.Errorf("cfg: cannot convert %T to Duration", v)
	}
}

func asStrings(v any) ([]string, error) {
	switch val := v.(type) {
	case []any:
		out := make([]string, len(val))
		for i, item := range val {
			out[i] = asString(item)
		}
		return out, nil
	case []string:
		return val, nil
	case string:
		return strings.Split(val, ","), nil
	default:
		return nil, fmt.Errorf("cfg: cannot convert %T to []string", v)
	}
}

// --- Internal helpers ---

func deepCopy(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = deepCopyVal(v)
	}
	return out
}

func deepCopyVal(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return deepCopy(val)
	case []any:
		c := make([]any, len(val))
		for i, item := range val {
			c[i] = deepCopyVal(item)
		}
		return c
	default:
		return v
	}
}
