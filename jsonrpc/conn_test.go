package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// newPipeConn uses io.Pipe to simulate bidirectional stdio.
func newPipeConn(t *testing.T) (*Conn, io.WriteCloser, io.ReadCloser) {
	t.Helper()
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()
	conn := NewConn(clientRead, clientWrite, clientWrite)
	return conn, serverWrite, serverRead
}

func TestConn_Call_Success(t *testing.T) {
	conn, serverWrite, serverRead := newPipeConn(t)
	defer conn.Close()
	defer serverWrite.Close()

	go func() {
		defer serverRead.Close()
		dec := json.NewDecoder(serverRead)
		var req map[string]json.RawMessage
		if err := dec.Decode(&req); err != nil {
			return
		}
		var id int64
		json.Unmarshal(req["id"], &id)
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result":  map[string]any{"value": "ok"},
		}
		data, _ := json.Marshal(resp)
		serverWrite.Write(data)
	}()

	var result struct {
		Value string `json:"value"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := conn.Call(ctx, "test/method", map[string]any{"q": "1"}, &result)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if result.Value != "ok" {
		t.Errorf("Value = %q, want ok", result.Value)
	}
}

func TestConn_Call_EchoedRequestDetected(t *testing.T) {
	// 模拟 cat/echo 类误配:对端把我们的请求原样回显(带 method+id)。
	// 客户端应立即识别为"非 JSON-RPC 服务器",而不是空等超时。
	conn, serverWrite, serverRead := newPipeConn(t)
	defer conn.Close()
	defer serverWrite.Close()

	go func() {
		defer serverRead.Close()
		dec := json.NewDecoder(serverRead)
		var req map[string]json.RawMessage
		if err := dec.Decode(&req); err != nil {
			return
		}
		// 原样回显(包括 method 字段)。
		data, _ := json.Marshal(req)
		serverWrite.Write(data)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := conn.Call(ctx, "initialize", map[string]any{}, nil)
	if err == nil {
		t.Fatal("echoed request should be reported as error, got nil")
	}
	if !strings.Contains(err.Error(), "echoed") {
		t.Errorf("error should mention echoed request, got: %v", err)
	}
}

func TestConn_Call_ErrorPreservesCode(t *testing.T) {
	conn, serverWrite, serverRead := newPipeConn(t)
	defer conn.Close()
	defer serverWrite.Close()

	go func() {
		defer serverRead.Close()
		dec := json.NewDecoder(serverRead)
		var req map[string]json.RawMessage
		if err := dec.Decode(&req); err != nil {
			return
		}
		var id int64
		json.Unmarshal(req["id"], &id)
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"error": map[string]any{
				"code":    -32601,
				"message": "method not found",
			},
		}
		data, _ := json.Marshal(resp)
		serverWrite.Write(data)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := conn.Call(ctx, "test/method", nil, nil)
	if err == nil {
		t.Fatal("Call should fail with Error")
	}
	var rpcErr *Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("err should be *Error, got %T: %v", err, err)
	}
	if rpcErr.Code != -32601 {
		t.Errorf("Code = %d, want -32601", rpcErr.Code)
	}
	if rpcErr.Message != "method not found" {
		t.Errorf("Message = %q, want 'method not found'", rpcErr.Message)
	}
}

func TestConn_Call_CtxCancelCleansPending(t *testing.T) {
	conn, serverWrite, serverRead := newPipeConn(t)
	defer conn.Close()
	defer serverWrite.Close()

	go func() {
		defer serverRead.Close()
		buf := make([]byte, 1024)
		for {
			if _, err := serverRead.Read(buf); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = conn.Call(ctx, "test/noReply", nil, nil)

	conn.pendingMu.Lock()
	pendingCount := len(conn.pending)
	conn.pendingMu.Unlock()
	if pendingCount != 0 {
		t.Errorf("pending map should be empty after ctx cancel, has %d entries", pendingCount)
	}
}

func TestConn_Notify(t *testing.T) {
	conn, serverWrite, serverRead := newPipeConn(t)
	defer conn.Close()
	defer serverWrite.Close()

	done := make(chan struct{})
	go func() {
		defer serverRead.Close()
		dec := json.NewDecoder(serverRead)
		var msg map[string]json.RawMessage
		if err := dec.Decode(&msg); err != nil {
			t.Errorf("decode failed: %v", err)
			return
		}
		var method string
		json.Unmarshal(msg["method"], &method)
		if method != "test/notify" {
			t.Errorf("method = %q, want test/notify", method)
		}
		close(done)
	}()

	if err := conn.Notify("test/notify", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("Notify failed: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive notification")
	}
}

func TestConn_Reply(t *testing.T) {
	conn, serverWrite, serverRead := newPipeConn(t)
	defer conn.Close()
	defer serverWrite.Close()

	done := make(chan struct{})
	go func() {
		defer serverRead.Close()
		dec := json.NewDecoder(serverRead)
		var msg map[string]json.RawMessage
		if err := dec.Decode(&msg); err != nil {
			t.Errorf("decode failed: %v", err)
			return
		}
		if _, ok := msg["result"]; !ok {
			t.Error("response should contain result")
		}
		close(done)
	}()

	if err := conn.Reply(int64(1), map[string]any{"r": "v"}); err != nil {
		t.Fatalf("Reply failed: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive reply")
	}
}

func TestConn_OnNotification_Called(t *testing.T) {
	conn, serverWrite, _ := newPipeConn(t)
	defer conn.Close()
	defer serverWrite.Close()

	called := make(chan struct{}, 1)
	conn.OnNotification("test/event", func(params json.RawMessage) {
		close(called)
	})

	notif := map[string]any{
		"jsonrpc": "2.0",
		"method":  "test/event",
		"params":  map[string]any{},
	}
	data, _ := json.Marshal(notif)
	serverWrite.Write(data)

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("OnNotification handler not called")
	}
}

func TestConn_Close_StopsReadLoop(t *testing.T) {
	conn, _, _ := newPipeConn(t)
	conn.Close()
	conn.Close() // should not panic
}

func TestConn_Call_AfterClose(t *testing.T) {
	conn, _, _ := newPipeConn(t)
	conn.Close()

	err := conn.Call(context.Background(), "test/x", nil, nil)
	if !errors.Is(err, ErrConnectionClosed) {
		t.Errorf("Call after Close = %v, want ErrConnectionClosed", err)
	}
}

func TestConn_Notify_AfterClose(t *testing.T) {
	conn, _, _ := newPipeConn(t)
	conn.Close()

	err := conn.Notify("test/x", nil)
	if !errors.Is(err, ErrConnectionClosed) {
		t.Errorf("Notify after Close = %v, want ErrConnectionClosed", err)
	}
}

func TestConn_ConcurrentCalls(t *testing.T) {
	conn, serverWrite, serverRead := newPipeConn(t)
	defer conn.Close()
	defer serverWrite.Close()

	go func() {
		defer serverRead.Close()
		dec := json.NewDecoder(serverRead)
		for {
			var req map[string]json.RawMessage
			if err := dec.Decode(&req); err != nil {
				return
			}
			var id int64
			json.Unmarshal(req["id"], &id)
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result":  map[string]any{"id": id},
			}
			data, _ := json.Marshal(resp)
			serverWrite.Write(data)
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			var result struct {
				ID int64 `json:"id"`
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := conn.Call(ctx, "test/m", nil, &result); err != nil {
				t.Errorf("Call %d failed: %v", n, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestConn_ServerRequestDelivered(t *testing.T) {
	conn, serverWrite, _ := newPipeConn(t)
	defer conn.Close()
	defer serverWrite.Close()

	go func() {
		req := map[string]any{
			"jsonrpc": "2.0",
			"id":      int64(99),
			"method":  "workspace/configuration",
			"params":  map[string]any{},
		}
		data, _ := json.Marshal(req)
		serverWrite.Write(data)
	}()

	select {
	case req := <-conn.Requests():
		if req.Method != "workspace/configuration" {
			t.Errorf("Method = %q, want workspace/configuration", req.Method)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server request not delivered")
	}
}

func TestConn_MultipleMessages(t *testing.T) {
	conn, serverWrite, _ := newPipeConn(t)
	defer conn.Close()
	defer serverWrite.Close()

	calls := 0
	conn.OnNotification("n1", func(json.RawMessage) { calls++ })
	conn.OnNotification("n2", func(json.RawMessage) { calls++ })

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "n1"})
	_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "n2"})
	serverWrite.Write(buf.Bytes())

	deadline := time.After(2 * time.Second)
	for calls < 2 {
		select {
		case <-deadline:
			t.Fatalf("only %d/2 handlers called", calls)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestConn_Close_Idempotent(t *testing.T) {
	conn, _, _ := newPipeConn(t)
	conn.Close()
	conn.Close()
	conn.Close()
}

func TestConn_Close_ClosesDoneChannel(t *testing.T) {
	conn, _, _ := newPipeConn(t)

	select {
	case <-conn.Done():
		t.Fatal("Done() should not be closed before Close()")
	default:
	}

	conn.Close()

	select {
	case <-conn.Done():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Done() should be closed after Close()")
	}

	conn.Close()
	select {
	case <-conn.Done():
	default:
		t.Error("Done() should remain closed after second Close()")
	}
}

// TestConn_Close_NoPanicOnConcurrentResponse verifies that closing the Conn
// while readLoop is about to deliver a response does not panic.
//
// Before the fix, Close closed entry.ch; readLoop's subsequent
// `entry.ch <- resp` would panic on send-to-closed-channel. Now Close sends
// nil to the buffered channel instead, which is race-free.
func TestConn_Close_NoPanicOnConcurrentResponse(t *testing.T) {
	for i := 0; i < 50; i++ {
		conn, serverWrite, serverRead := newPipeConn(t)

		// Reader that emits a response immediately (races with Close).
		go func() {
			defer serverRead.Close()
			defer serverWrite.Close()
			dec := json.NewDecoder(serverRead)
			var req map[string]json.RawMessage
			if err := dec.Decode(&req); err != nil {
				return
			}
			var id int64
			_ = json.Unmarshal(req["id"], &id)
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result":  "ok",
			}
			data, _ := json.Marshal(resp)
			serverWrite.Write(data)
		}()

		// Call races with Close: whichever wins, neither path may panic.
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			defer conn.Close()
			// Small jitter to vary the race window across iterations.
			time.Sleep(time.Duration(i%5) * time.Microsecond)
		}()
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			_ = conn.Call(ctx, "ping", nil, nil)
		}()
		wg.Wait()
	}
}

func TestDecodeID(t *testing.T) {
	tests := []struct {
		input string
		want  any
	}{
		{`42`, int64(42)},
		{`"abc"`, "abc"},
		{`null`, nil},
	}
	for _, tt := range tests {
		got := DecodeID(json.RawMessage(tt.input))
		if got != tt.want {
			t.Errorf("DecodeID(%s) = %v (%T), want %v (%T)", tt.input, got, got, tt.want, tt.want)
		}
	}
}

func TestParseIDInt(t *testing.T) {
	i, ok := ParseIDInt(json.RawMessage(`42`))
	if !ok || i != 42 {
		t.Errorf("ParseIDInt(`42`) = %d, %v, want 42, true", i, ok)
	}
	_, ok = ParseIDInt(json.RawMessage(`"abc"`))
	if ok {
		t.Error("ParseIDInt(`abc`) should return false for string ID")
	}
}

func TestIsServerError(t *testing.T) {
	e := &Error{Code: -32601, Message: "not found"}
	if !errors.Is(e, &Error{Code: -32601}) {
		t.Error("errors.Is should match by Code")
	}
	rpcErr, ok := IsServerError(e)
	if !ok || rpcErr.Code != -32601 {
		t.Errorf("IsServerError failed: %v, %v", rpcErr, ok)
	}
	if _, ok := IsServerError(ErrConnectionClosed); ok {
		t.Error("ErrConnectionClosed should not be a server error")
	}
}

func TestNewStringConn(t *testing.T) {
	r := strings.NewReader(`{"jsonrpc":"2.0","method":"test","params":{}}`)
	w := &bytes.Buffer{}
	conn := NewConn(r, w, nil)
	defer conn.Close()

	select {
	case notif := <-conn.Notifications():
		if notif.Method != "test" {
			t.Errorf("Method = %q, want test", notif.Method)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("notification not delivered")
	}
}

// TestConn_ReadLoopEOFWakesPendingCall verifies that when the peer closes
// their write end (causing readLoop's decoder.Decode to return EOF), the
// connection is closed and pending Calls are woken with ErrConnectionClosed.
//
// Before the fix, readLoop returned on EOF without calling Close(), leaving
// pending Calls blocked forever on entry.ch.
func TestConn_ReadLoopEOFWakesPendingCall(t *testing.T) {
	conn, serverWrite, serverRead := newPipeConn(t)
	defer conn.Close()

	// Drain server-side reads so the client can send its request.
	go func() {
		defer serverRead.Close()
		buf := make([]byte, 1024)
		for {
			if _, err := serverRead.Read(buf); err != nil {
				return
			}
		}
	}()

	// Start a Call with a long timeout — it should NOT wait for the full
	// timeout; the EOF should wake it quickly.
	callDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		callDone <- conn.Call(ctx, "test/slow", nil, nil)
	}()

	// Give the Call time to register in pending.
	time.Sleep(50 * time.Millisecond)

	// Close the server's write end → client's readLoop sees EOF.
	serverWrite.Close()

	select {
	case err := <-callDone:
		if !errors.Is(err, ErrConnectionClosed) {
			t.Errorf("Call after EOF = %v, want ErrConnectionClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call did not return after EOF — pending was not woken")
	}

	// New Calls after EOF should also return ErrConnectionClosed.
	err := conn.Call(context.Background(), "test/afterEOF", nil, nil)
	if !errors.Is(err, ErrConnectionClosed) {
		t.Errorf("Call after EOF-driven Close = %v, want ErrConnectionClosed", err)
	}
}
