// Package jsonrpc implements JSON-RPC 2.0 protocol primitives.
//
// It provides the message model (Request, Response, Notification, Error)
// and a bidirectional connection (Conn) for JSON-RPC over io.Reader/io.Writer.
//
// The Conn type fixes several defects found in common LSP/MCP implementations:
//   - Call cleans up pending map on ctx cancel (prevents entry leak + write to closed channel)
//   - Call returns *Error preserving server Code (not flattened to a generic error)
//   - readLoop is interruptible via Close (closes done + stdin to unblock Decode)
//   - OnNotification handlers are actually invoked by readLoop (not dead code)
package jsonrpc
