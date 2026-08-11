package testx

import (
	"fmt"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"
)

// CallerInfo returns the file:line for each frame on the goroutine stack,
// starting from the caller of the function that called CallerInfo.
// Frames inside the testx package and the standard testing/runtime packages
// are filtered out, so the returned slice starts at the user's test code.
//
// This is used by assertion failure messages to point at the failing line.
func CallerInfo() []string {
	pc := make([]uintptr, 32)
	n := runtime.Callers(2, pc)
	frames := runtime.CallersFrames(pc[:n])

	var callers []string
	for {
		frame, more := frames.Next()
		file := frame.File
		name := frame.Function

		// Filter out standard library testing and runtime.
		if strings.HasPrefix(name, "testing.") ||
			strings.HasPrefix(name, "runtime.") {
			if !more {
				break
			}
			continue
		}

		// Filter out frames inside this package, but keep _test.go frames so
		// failures raised via internal helpers still surface the test line.
		parts := strings.Split(file, "/")
		if len(parts) > 1 {
			dir := parts[len(parts)-2]
			if dir == "testx" {
				if !strings.HasSuffix(file, "_test.go") {
					if !more {
						break
					}
					continue
				}
			}
		}

		simpleFile := parts[len(parts)-1]
		callers = append(callers, fmt.Sprintf("%s:%d", simpleFile, frame.Line))

		if !more {
			break
		}
	}
	return callers
}

// callerFrame returns a short "file:line" string for the frame `skip` calls
// above the caller of callerFrame. Returns "" when the stack is shorter than
// expected. This is the single-frame convenience wrapper around CallerInfo
// used by assertion helpers that only need their immediate caller.
func callerFrame(skip int) string {
	pc, _, _, ok := runtime.Caller(skip + 1)
	if !ok {
		return ""
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return ""
	}
	file, line := fn.FileLine(pc)
	parts := strings.Split(file, "/")
	return fmt.Sprintf("%s:%d", parts[len(parts)-1], line)
}

// isTest reports whether name is a test method name with the given prefix,
// matching testing's rules: either exactly the prefix, or prefix followed
// by an uppercase letter.
func isTest(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	if len(name) == len(prefix) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(name[len(prefix):])
	return !unicode.IsLower(r)
}
