package jsonrpc

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Standard JSON-RPC 2.0 error codes (per spec §5.1).
const (
	ParseError      = -32700
	InvalidRequest  = -32600
	MethodNotFound  = -32601
	InvalidParams   = -32602
	InternalError   = -32603
)

// Request represents a JSON-RPC 2.0 request.
// ID can be a number (int64) or a string.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Notification represents a JSON-RPC 2.0 notification (no ID, no response).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Error represents a JSON-RPC 2.0 error object.
// It implements the error interface so callers can use errors.As to
// extract the server-returned Code from a Call result.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	return fmt.Sprintf("jsonrpc: error code=%d: %s", e.Code, e.Message)
}

// Is supports errors.Is comparison by Code.
func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.Code == other.Code
}

// IsServerError returns the *Error if err is a server-returned JSON-RPC error,
// distinguishing it from local sentinel errors (e.g., connection closed).
func IsServerError(err error) (*Error, bool) {
	var rpcErr *Error
	if errors.As(err, &rpcErr) {
		return rpcErr, true
	}
	return nil, false
}
