package testx

import (
	"fmt"
	"reflect"
)

// === Package-level type-safe generic assertions ===
//
// These helpers are preferable to the reflect-based package-level assertions
// below when the compared values are known to be comparable at compile time:
// they avoid boxing and reflection, produce clearer failure messages, and
// catch type mismatches at the call site instead of at runtime.

// EqualT asserts expect == actual for any comparable type T.
// Returns true on success so callers can chain or short-circuit.
func EqualT[T comparable](t TestingT, expect, actual T, msg ...any) bool {
	if expect != actual {
		t.Errorf("%v unexpectedly not equal %v: %v", expect, actual, fmt.Sprint(msg...))
		return false
	}
	return true
}

// NotEqualT asserts expect != actual for any comparable type T.
func NotEqualT[T comparable](t TestingT, expect, actual T, msg ...any) bool {
	if expect == actual {
		t.Errorf("%v unexpectedly equal %v: %v", expect, actual, fmt.Sprint(msg...))
		return false
	}
	return true
}

// TrueT asserts v is true.
func TrueT(t TestingT, v bool, msg ...any) bool {
	if !v {
		t.Errorf("expected true, got false: %v", fmt.Sprint(msg...))
		return false
	}
	return true
}

// FalseT asserts v is false.
func FalseT(t TestingT, v bool, msg ...any) bool {
	if v {
		t.Errorf("expected false, got true: %v", fmt.Sprint(msg...))
		return false
	}
	return true
}

// === Package-level reflect-based assertions ===
//
// These mirror the convenience of testify/assert's top-level functions. They
// delegate to the comparison helpers already used by *Assert so the failure
// messages stay consistent.

// Equal asserts a == b (with type conversion, like testify's EqualValues).
func Equal(t TestingT, a, b any, msg ...any) bool {
	if !ObjectsAreEqualValues(a, b) {
		t.Errorf("%v unexpectedly not equal %v: %v", a, b, fmt.Sprint(msg...))
		return false
	}
	return true
}

// NotEqual asserts a != b.
func NotEqual(t TestingT, a, b any, msg ...any) bool {
	if ObjectsAreEqualValues(a, b) {
		t.Errorf("%v unexpectedly equal %v: %v", a, b, fmt.Sprint(msg...))
		return false
	}
	return true
}

// DEqual asserts reflect.DeepEqual(expect, actual).
// Unlike Equal, it does not perform type conversion; the values must have
// identical type and structure.
func DEqual(t TestingT, expect, actual any, msg ...any) bool {
	if !reflect.DeepEqual(expect, actual) {
		t.Errorf("deep equal failed: expect %v (%T), actual %v (%T): %v",
			expect, expect, actual, actual, fmt.Sprint(msg...))
		return false
	}
	return true
}

// Expect asserts fmt.Sprint(actual) == expect.
// Useful when comparing a value against a pre-stringified expected form.
func Expect(t TestingT, expect string, actual any, msg ...any) bool {
	if got := fmt.Sprint(actual); got != expect {
		t.Errorf("expected %q, got %q: %v", expect, got, fmt.Sprint(msg...))
		return false
	}
	return true
}

// NotExpect asserts fmt.Sprint(actual) != expect.
func NotExpect(t TestingT, expect string, actual any, msg ...any) bool {
	if got := fmt.Sprint(actual); got == expect {
		t.Errorf("expected NOT %q, but got it: %v", expect, fmt.Sprint(msg...))
		return false
	}
	return true
}

// Nil asserts actual == nil.
func Nil(t TestingT, actual any, msg ...any) bool {
	if !isNil(actual) {
		t.Errorf("expected nil, got %v: %v", actual, fmt.Sprint(msg...))
		return false
	}
	return true
}

// NotNil asserts actual != nil.
func NotNil(t TestingT, actual any, msg ...any) bool {
	if isNil(actual) {
		t.Errorf("expected not nil, but got nil: %v", fmt.Sprint(msg...))
		return false
	}
	return true
}

// Empty asserts the value is the zero of its type. For strings this is "",
// for slices/maps it is length 0, for pointers it is nil.
func Empty(t TestingT, actual any, msg ...any) bool {
	if !isEmpty(actual) {
		t.Errorf("expected empty, got %v: %v", actual, fmt.Sprint(msg...))
		return false
	}
	return true
}

// NotEmpty asserts the value is not the zero of its type.
func NotEmpty(t TestingT, actual any, msg ...any) bool {
	if isEmpty(actual) {
		t.Errorf("expected not empty, but got %v: %v", actual, fmt.Sprint(msg...))
		return false
	}
	return true
}

// Zero asserts actual == reflect.Zero(typeof(actual)).
func Zero(t TestingT, actual any, msg ...any) bool {
	if !isZero(actual) {
		t.Errorf("expected zero, got %v: %v", actual, fmt.Sprint(msg...))
		return false
	}
	return true
}

// NotZero asserts actual != reflect.Zero(typeof(actual)).
func NotZero(t TestingT, actual any, msg ...any) bool {
	if isZero(actual) {
		t.Errorf("expected not zero, but got %v: %v", actual, fmt.Sprint(msg...))
		return false
	}
	return true
}

// Error asserts err != nil.
func Error(t TestingT, err error, msg ...any) bool {
	if err == nil {
		t.Errorf("expected an error, got nil: %v", fmt.Sprint(msg...))
		return false
	}
	return true
}

// NoErrorPkg is the package-level form of (*Assert).NoError, provided so
// callers using the package-level style can also assert error-free results.
// Named with the Pkg suffix to avoid colliding with the method name on Assert.
func NoErrorPkg(t TestingT, err error, msg ...any) bool {
	if err != nil {
		t.Errorf("expected no error, got %v: %v", err, fmt.Sprint(msg...))
		return false
	}
	return true
}

// True asserts b is true.
func True(t TestingT, b bool, msg ...any) bool {
	if !b {
		t.Errorf("expected true, got false: %v", fmt.Sprint(msg...))
		return false
	}
	return true
}

// False asserts b is false.
func False(t TestingT, b bool, msg ...any) bool {
	if b {
		t.Errorf("expected false, got true: %v", fmt.Sprint(msg...))
		return false
	}
	return true
}

// IsType asserts that actual's type name matches expect. expect may be a
// short alias ("int", "f64", "u8s" for []uint8, etc.) or the full type name
// returned by reflect.Type.String(). The special string "nil" matches a nil
// actual value.
//
// Note on []byte vs []uint8: reflect reports []byte as "[]uint8", so both
// spellings are accepted for byte slices.
func IsType(t TestingT, expect string, actual any, msg ...any) bool {
	if actual == nil {
		if expect == "nil" {
			return true
		}
		t.Errorf("expected type %s, got nil: %v", expect, fmt.Sprint(msg...))
		return false
	}
	got := reflect.TypeOf(actual).String()
	if got == expect || got == typeAlias(expect) {
		return true
	}
	// Accept []byte as an alias for []uint8 (reflect reports []uint8).
	if (expect == "[]byte" || expect == "bytes") && got == "[]uint8" {
		return true
	}
	t.Errorf("expected type %s, got %s: %v", expect, got, fmt.Sprint(msg...))
	return false
}

// === Internal helpers ===

// isNil handles the typed-nil case: an interface value whose dynamic value
// is nil (e.g. (*int)(nil)) still compares non-nil to the untyped nil, but
// should be treated as nil by assertions.
func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Slice,
		reflect.Map, reflect.Chan, reflect.Func:
		return rv.IsNil()
	}
	return false
}

// isEmpty reports whether v is the zero value of its type. It is stricter
// than isNil: only the empty string, empty slice/map, or nil pointer count.
func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return rv.Len() == 0
	case reflect.Slice, reflect.Map, reflect.Chan, reflect.Array:
		return rv.Len() == 0
	case reflect.Ptr, reflect.Interface:
		return rv.IsNil()
	case reflect.Bool:
		return !rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() == 0
	default:
		return false
	}
}

// isZero is a thin wrapper around reflect.Zero for the Zero/NotZero pair.
func isZero(v any) bool {
	if v == nil {
		return true
	}
	t := reflect.TypeOf(v)
	return reflect.DeepEqual(v, reflect.Zero(t).Interface())
}

// typeAlias translates a short alias to the reflect type string so IsType
// accepts both the alias and the canonical form. The alias set is taken
// from testify/tt for compatibility; unknown aliases pass through unchanged.
func typeAlias(expect string) string {
	m := map[string]string{
		"str":   "string",
		"int":   "int",
		"i8":    "int8",
		"i16":   "int16",
		"i32":   "int32",
		"i64":   "int64",
		"u":     "uint",
		"u8":    "uint8",
		"u16":   "uint16",
		"ui32":  "uint32",
		"ui64":  "uint64",
		"f32":   "float32",
		"f64":   "float64",
		"b":     "bool",
		"m":     "map",
		"ch":    "chan",
		"stu":   "struct",
		"bytes": "[]byte",
		"u8s":   "[]uint8",
		"c64":   "complex64",
		"c128":  "complex128",
	}
	if s, ok := m[expect]; ok {
		return s
	}
	return expect
}
