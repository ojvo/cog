// Package safe provides error-recovery helpers and single-task lifecycle
// runners:
//
//   - Recover / RecoverFunc / Try: convert panics into errors with optional
//     stack traces
//   - Runner: a single-shot runner with Start/Stop/Restart semantics
//   - OneRun: a serializer that runs at most one task at a time, queueing
//     subsequent Run callers behind the in-flight task
//   - Rerun: a reconnect loop driver implementing the Dialer contract
//
// The package depends only on the Go standard library.
package safe
