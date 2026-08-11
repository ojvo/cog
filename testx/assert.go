package testx

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// ObjectsAreEqualValues returns true if a and b have equal values,
// performing type conversion when types differ (like testify's EqualValues).
func ObjectsAreEqualValues(a, b interface{}) bool {
	if a == nil || b == nil {
		return a == b
	}
	if reflect.TypeOf(a) == reflect.TypeOf(b) {
		return reflect.DeepEqual(a, b)
	}
	// Try converting b to a's type
	if vb := reflect.ValueOf(b); vb.Type().ConvertibleTo(reflect.TypeOf(a)) {
		return reflect.DeepEqual(a, vb.Convert(reflect.TypeOf(a)).Interface())
	}
	// Try converting a to b's type
	if va := reflect.ValueOf(a); va.Type().ConvertibleTo(reflect.TypeOf(b)) {
		return reflect.DeepEqual(va.Convert(reflect.TypeOf(b)).Interface(), b)
	}
	return false
}

// Assert provides fluent test assertions. Methods on *Assert return the
// receiver so that multiple checks can be chained on the same instance.
type Assert struct {
	t *testing.T
}

// New creates a new Assert helper for the given test or benchmark.
func New(t *testing.T) *Assert {
	return &Assert{t: t}
}

// NewAssert preserves the legacy constructor name. New is preferred.
func NewAssert(t *testing.T) *Assert {
	return New(t)
}

// T returns the underlying testing.T.
func (o *Assert) T() *testing.T {
	return o.t
}

// Errorf is a local variant to get the right line numbers.
func (o *Assert) Errorf(format string, rest ...interface{}) {
	_, file, line, _ := runtime.Caller(2)
	file = file[strings.LastIndex(file, "/")+1:]
	fmt.Printf("%s:%d %s\n", file, line, fmt.Sprintf(format, rest...))
	o.t.Fail()
}

// NotEqual checks for a not equal b.
func (o *Assert) NotEqual(a, b interface{}, msg ...string) {
	if ObjectsAreEqualValues(a, b) {
		o.Errorf("%v unexpectedly equal: %v", a, msg)
	}
}

// EqualValues checks for a equal b (with type conversion).
func (o *Assert) EqualValues(a, b interface{}, msg ...string) {
	if !ObjectsAreEqualValues(a, b) {
		o.Errorf("%v unexpectedly not equal %v: %v", a, b, msg)
	}
}

// Equal checks for a equal b.
func (o *Assert) Equal(a, b interface{}, msg ...string) {
	if !ObjectsAreEqualValues(a, b) {
		o.Errorf("%v unexpectedly not equal %v: %v", a, b, msg)
	}
}

// DEqual asserts reflect.DeepEqual(a, b).
func (o *Assert) DEqual(a, b interface{}, msg ...string) {
	if !reflect.DeepEqual(a, b) {
		o.Errorf("deep equal failed: %v vs %v: %v", a, b, msg)
	}
}

// Nil asserts v is nil (handles typed-nil).
func (o *Assert) Nil(v interface{}, msg ...string) {
	if !isNil(v) {
		o.Errorf("expected nil, got %v: %v", v, msg)
	}
}

// NotNil asserts v is not nil (handles typed-nil).
func (o *Assert) NotNil(v interface{}, msg ...string) {
	if isNil(v) {
		o.Errorf("expected not nil: %v", msg)
	}
}

// Empty asserts v is the zero of its type (empty string/slice/map, nil ptr).
func (o *Assert) Empty(v interface{}, msg ...string) {
	if !isEmpty(v) {
		o.Errorf("expected empty, got %v: %v", v, msg)
	}
}

// NotEmpty asserts v is not the zero of its type.
func (o *Assert) NotEmpty(v interface{}, msg ...string) {
	if isEmpty(v) {
		o.Errorf("expected not empty, got %v: %v", v, msg)
	}
}

// Zero asserts v equals reflect.Zero(typeof(v)).
func (o *Assert) Zero(v interface{}, msg ...string) {
	if !isZero(v) {
		o.Errorf("expected zero, got %v: %v", v, msg)
	}
}

// NotZero asserts v does not equal reflect.Zero(typeof(v)).
func (o *Assert) NotZero(v interface{}, msg ...string) {
	if isZero(v) {
		o.Errorf("expected not zero, got %v: %v", v, msg)
	}
}

// IsType asserts that actual's type matches expect. expect may be either
// the canonical type name (reflect.Type.String()) or a short alias
// accepted by the package-level IsType function.
func (o *Assert) IsType(expect string, actual interface{}, msg ...string) {
	args := make([]any, 0, len(msg))
	for _, m := range msg {
		args = append(args, m)
	}
	if !IsType(o.t, expect, actual, args...) {
		o.t.Fail()
	}
}

// NoError checks for no errors (nil).
func (o *Assert) NoError(err error, msg ...string) {
	if err != nil {
		o.Errorf("expecting no error, got %v: %v", err, msg)
	}
}

// TtNoError is an alias for NoError (preserves legacy name).
func (o *Assert) TtNoError(err error, msg ...string) {
	o.NoError(err, msg...)
}

// Error checks/expects an error.
func (o *Assert) Error(err error, msg ...string) {
	if err == nil {
		o.Errorf("expecting an error, didn't get it: %v", msg)
	}
}

// True checks bool is true.
func (o *Assert) True(b bool, msg ...string) {
	if !b {
		o.Errorf("expecting true, didn't: %v", msg)
	}
}

// False checks bool is false.
func (o *Assert) False(b bool, msg ...string) {
	if b {
		o.Errorf("expecting false, didn't: %v", msg)
	}
}

// Contains checks that needle is in haystack.
func (o *Assert) Contains(haystack, needle string, msg ...string) {
	if !strings.Contains(haystack, needle) {
		o.Errorf("%v doesn't contain %v: %v", haystack, needle, msg)
	}
}

// BM runs fn b.N times as a benchmark body. Convenience method that mirrors
// the package-level BM helper for callers already holding an *Assert built
// from a *testing.B.
func (o *Assert) BM(b *testing.B, fn func()) {
	BM(b, fn)
}

// Fail fails the test.
func (o *Assert) Fail(msg string) {
	_, file, line, _ := runtime.Caller(1)
	file = file[strings.LastIndex(file, "/")+1:]
	fmt.Printf("%s:%d %s\n", file, line, msg)
	o.t.FailNow()
}

// CheckEquals checks if actual == expect and fails the test and logs
// failure (including filename:linenum if they are not equal).
func (o *Assert) CheckEquals(actual interface{}, expected interface{}, msg interface{}) {
	if !ObjectsAreEqualValues(actual, expected) {
		_, file, line, _ := runtime.Caller(1)
		file = file[strings.LastIndex(file, "/")+1:]
		fmt.Printf("%s:%d mismatch!\nactual:\n%+v\nexpected:\n%+v\nfor %+v\n", file, line, actual, expected, msg)
		o.t.Fail()
	}
}

// Assert_ is similar to True() under a different name and earlier
// fortio/stats test implementation.
func (o *Assert) Assert_(cond bool, msg string, rest ...interface{}) {
	if !cond {
		_, file, line, _ := runtime.Caller(1)
		file = file[strings.LastIndex(file, "/")+1:]
		m := fmt.Sprintf(msg, rest...)
		fmt.Printf("%s:%d assert failure: %s\n", file, line, m)
		o.t.Fail()
	}
}

// ========== Test Suite ==========

type hasT interface {
	T() *testing.T
	SetT(t *testing.T)
}

// TestSuite to be used as base struct for test suites.
// replaces https://pkg.go.dev/github.com/stretchr/testify@v1.8.0/suite
type TestSuite struct {
	t *testing.T
}

// T returns the current testing.T.
func (s *TestSuite) T() *testing.T {
	return s.t
}

// SetT sets the testing.T in the suite object.
func (s *TestSuite) SetT(t *testing.T) {
	s.t = t
}

type hasSetupTest interface {
	SetupTest()
}

type hasTearDown interface {
	TearDownTest()
}

// RunSuite runs the test suite with SetupTest first and TearDownTest after.
// replaces https://pkg.go.dev/github.com/stretchr/testify/suite#Run
func RunSuite(t *testing.T, suite hasT) {
	suite.SetT(t)
	tests := []testing.InternalTest{}
	methodFinder := reflect.TypeOf(suite)
	var setup hasSetupTest
	if s, ok := suite.(hasSetupTest); ok {
		setup = s
	}
	var tearDown hasTearDown
	if td, ok := suite.(hasTearDown); ok {
		tearDown = td
	}
	for i := 0; i < methodFinder.NumMethod(); i++ {
		method := methodFinder.Method(i)
		if !isTest(method.Name, "Test") {
			continue
		}
		test := testing.InternalTest{
			Name: method.Name,
			F: func(_ *testing.T) {
				method.Func.Call([]reflect.Value{reflect.ValueOf(suite)})
			},
		}
		tests = append(tests, test)
	}
	for _, test := range tests {
		if setup != nil {
			setup.SetupTest()
		}
		t.Run(test.Name, test.F)
		if tearDown != nil {
			tearDown.TearDownTest()
		}
	}
}
