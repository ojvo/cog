package util

import (
	"fmt"
	"reflect"
	"regexp"
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

type AssertTest struct {
	t *testing.T
}

func NewAssert(t *testing.T) *AssertTest {
	return &AssertTest{t: t}
}

// Errorf is a local variant to get the right line numbers.
func (o *AssertTest) Errorf(format string, rest ...interface{}) {
	_, file, line, _ := runtime.Caller(2)
	file = file[strings.LastIndex(file, "/")+1:]
	fmt.Printf("%s:%d %s\n", file, line, fmt.Sprintf(format, rest...))
	o.t.Fail()
}

// NotEqual checks for a not equal b.
func (o *AssertTest) NotEqual(a, b interface{}, msg ...string) {
	if ObjectsAreEqualValues(a, b) {
		o.Errorf("%v unexpectedly equal: %v", a, msg)
	}
}

// EqualValues checks for a equal b.
func (o *AssertTest) EqualValues(a, b interface{}, msg ...string) {
	if !ObjectsAreEqualValues(a, b) {
		o.Errorf("%v unexpectedly not equal %v: %v", a, b, msg)
	}
}

// Equal also checks for a equal b.
func (o *AssertTest) Equal(a, b interface{}, msg ...string) {
	if !ObjectsAreEqualValues(a, b) {
		o.Errorf("%v unexpectedly not equal %v: %v", a, b, msg)
	}
}

// NoError checks for no errors (nil).
func (o *AssertTest) TtNoError(err error, msg ...string) {
	if err != nil {
		o.Errorf("expecting no error, got %v: %v", err, msg)
	}
}

// Error checks/expects an error.
func (o *AssertTest) Error(err error, msg ...string) {
	if err == nil {
		o.Errorf("expecting an error, didn't get it: %v", msg)
	}
}

// True checks bool is true.
func (o *AssertTest) True(b bool, msg ...string) {
	if !b {
		o.Errorf("expecting true, didn't: %v", msg)
	}
}

// False checks bool is false.
func (o *AssertTest) False(b bool, msg ...string) {
	if b {
		o.Errorf("expecting false, didn't: %v", msg)
	}
}

// Contains checks that needle is in haystack.
func (o *AssertTest) Contains(haystack, needle string, msg ...string) {
	if !strings.Contains(haystack, needle) {
		o.Errorf("%v doesn't contain %v: %v", haystack, needle, msg)
	}
}

// Fail fails the test.
func (o *AssertTest) Fail(msg string) {
	_, file, line, _ := runtime.Caller(1)
	file = file[strings.LastIndex(file, "/")+1:]
	fmt.Printf("%s:%d %s\n", file, line, msg)
	o.t.FailNow()
}

// CheckEquals checks if actual == expect and fails the test and logs
// failure (including filename:linenum if they are not equal).
func (o *AssertTest) CheckEquals(actual interface{}, expected interface{}, msg interface{}) {
	if expected != actual {
		_, file, line, _ := runtime.Caller(1)
		file = file[strings.LastIndex(file, "/")+1:]
		fmt.Printf("%s:%d mismatch!\nactual:\n%+v\nexpected:\n%+v\nfor %+v\n", file, line, actual, expected, msg)
		o.t.Fail()
	}
}

// Assert is similar to True() under a different name and earlier
// fortio/stats test implementation.
func (o *AssertTest) Assert(cond bool, msg string, rest ...interface{}) {
	if !cond {
		_, file, line, _ := runtime.Caller(1)
		file = file[strings.LastIndex(file, "/")+1:]
		m := fmt.Sprintf(msg, rest...)
		fmt.Printf("%s:%d assert failure: %s\n", file, line, m)
		o.t.Fail()
	}
}

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

// Run runs the test suite with SetupTest first and TearDownTest after.
// replaces https://pkg.go.dev/github.com/stretchr/testify/suite#Run
func (o *AssertTest) Run(suite hasT) {
	suite.SetT(o.t)
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
		//nolint:staticcheck // consider fixing later for perf but this is just to run a few tests.
		if ok, _ := regexp.MatchString("^Test", method.Name); !ok {
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
		o.t.Run(test.Name, test.F)
		if tearDown != nil {
			tearDown.TearDownTest()
		}
	}
}
