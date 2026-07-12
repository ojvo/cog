// Package log provides a structured logging library with async writing,
// file rotation, multi-stream output, and log levels (Debug/Info/Warn/Error/Fatal).
//
// The default logger writes to stdout; call SetLogger to reconfigure.
// Package-level functions (Info, Warn, Error, etc.) are safe for concurrent use.
package log
