package safe

import (
	"errors"
	"fmt"
	"runtime/debug"
)

// Recover recovers a panic and stores it as an error into *err. If stack is
// true (any first variadic argument true), the recovered error includes the
// goroutine stack trace. Recover is intended to be deferred:
//
//	defer safe.Recover(&err)
//	defer safe.Recover(&err, true) // include stack
func Recover(err *error, stack ...bool) {
	if er := recover(); er != nil {
		if err == nil {
			return
		}
		if len(stack) > 0 && stack[0] {
			*err = fmt.Errorf("%v\n%s", er, debug.Stack())
		} else {
			*err = fmt.Errorf("%v", er)
		}
	}
}

// RecoverFunc recovers a panic and invokes fn with the recovered error and
// stack trace. Intended to be deferred directly:
//
//	defer safe.RecoverFunc(func(err error, stack string) { log.Print(err, stack) })
func RecoverFunc(fn func(err error, stack string)) {
	if er := recover(); er != nil {
		if fn != nil {
			fn(fmt.Errorf("%v", er), string(debug.Stack()))
		}
	}
}

// Try invokes fn and converts any panic into an error. Each catch handler (if
// any) is invoked with the recovered error. Returns the recovered error or
// fn's own returned error.
func Try(fn func() error, catch ...func(err error)) (err error) {
	defer RecoverFunc(func(er error, stack string) {
		err = er
		for _, c := range catch {
			c(er)
		}
	})
	return fn()
}

// ErrNoHandler is returned by OneRun.Run / Runner.Run when no handler is set.
var ErrNoHandler = errors.New("safe: no handler set")
