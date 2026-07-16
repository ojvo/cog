package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"ojv/cog/log"
)

// ErrConnectionClosed is returned when a Call or Notify is attempted on a
// closed connection, or when a pending Call is woken by Close.
var ErrConnectionClosed = errors.New("jsonrpc: connection closed")

// pendingEntry holds the channel for a pending Call waiting on a response.
type pendingEntry struct {
	ch chan *Response
}

// Conn is a bidirectional JSON-RPC 2.0 connection over io.Reader/io.Writer.
//
// It supports three message directions:
//   - client→server: Call (request+response) and Notify (one-way)
//   - server→client: Reply (response to a server-initiated request) and
//     inbound notifications/requests delivered via channels or handlers
//
// Lock split (prevents pipe write deadlock from blocking readLoop):
//   - writeMu: protects encoder.Encode (may block on pipe write)
//   - pendingMu: protects pending map + nextID (shared by readLoop and Call)
//
// Using a single mutex would deadlock: Call holds mu to write pipe → blocks,
// readLoop can't acquire mu to dispatch response → server pipe write also
// blocks → classic bidirectional pipe deadlock.
type Conn struct {
	encoder *json.Encoder
	decoder *json.Decoder
	writeMu sync.Mutex
	stdin   io.Closer // closed to trigger server-side EOF (used by Close)

	nextID    int64
	pending   map[int64]*pendingEntry
	pendingMu sync.Mutex

	handlersMu sync.RWMutex
	handlers   map[string]func(json.RawMessage)

	notifyChan  chan *Notification
	requestChan chan *Request
	done        chan struct{}
	closed      atomic.Bool
}

// NewConn creates a JSON-RPC connection and starts the read loop.
// stdin may be nil (for server→client only scenarios); when non-nil,
// Close will close it to unblock the decoder.
func NewConn(r io.Reader, w io.Writer, stdin io.Closer) *Conn {
	conn := &Conn{
		encoder:     json.NewEncoder(w),
		decoder:     json.NewDecoder(r),
		stdin:       stdin,
		pending:     make(map[int64]*pendingEntry),
		handlers:    make(map[string]func(json.RawMessage)),
		notifyChan:  make(chan *Notification, 100),
		requestChan: make(chan *Request, 100),
		done:        make(chan struct{}),
	}
	go conn.readLoop()
	return conn
}

// readLoop continuously reads JSON messages and dispatches them.
// Exits when decoder.Decode returns an error (EOF/pipe closed/malformed)
// or when done is closed.
func (c *Conn) readLoop() {
	for {
		select {
		case <-c.done:
			return
		default:
		}
		var raw map[string]json.RawMessage
		if err := c.decoder.Decode(&raw); err != nil {
			if !c.closed.Load() {
				log.Debugf("jsonrpc: readLoop exit: %v", err)
			}
			return
		}

		var method string
		if m, ok := raw["method"]; ok {
			if err := json.Unmarshal(m, &method); err != nil {
				log.Debugf("jsonrpc: skip message: method unmarshal failed: %v", err)
				continue
			}
		}

		var idRaw json.RawMessage
		hasID := false
		if i, ok := raw["id"]; ok {
			idRaw = i
			hasID = true
		}

		// Has ID + method: server→client request.
		if hasID && method != "" {
			req := &Request{
				JSONRPC: "2.0",
				ID:      DecodeID(idRaw),
				Method:  method,
			}
			if p, ok := raw["params"]; ok {
				req.Params = p
			}
			select {
			case c.requestChan <- req:
			default:
				log.Debugf("jsonrpc: request channel full, dropping %s", method)
			}
			continue
		}

		// Has ID, no method: response to client→server request.
		if hasID {
			intID, ok := ParseIDInt(idRaw)
			if !ok {
				continue
			}
			c.pendingMu.Lock()
			entry, ok := c.pending[intID]
			delete(c.pending, intID)
			c.pendingMu.Unlock()

			if ok {
				resp := &Response{JSONRPC: "2.0", ID: DecodeID(idRaw)}
				if r, ok := raw["result"]; ok {
					resp.Result = r
				}
				if e, ok := raw["error"]; ok {
					var rpcErr Error
					if err := json.Unmarshal(e, &rpcErr); err == nil {
						resp.Error = &rpcErr
					}
				}
				select {
				case entry.ch <- resp:
				default:
				}
			}
			continue
		}

		// No ID, has method: notification.
		if method != "" {
			c.handlersMu.RLock()
			handler, ok := c.handlers[method]
			c.handlersMu.RUnlock()
			if ok && handler != nil {
				var params json.RawMessage
				if p, ok := raw["params"]; ok {
					params = p
				}
				handler(params)
				continue
			}

			notif := &Notification{
				JSONRPC: "2.0",
				Method:  method,
			}
			if p, ok := raw["params"]; ok {
				notif.Params = p
			}
			select {
			case c.notifyChan <- notif:
			default:
				log.Debugf("jsonrpc: notify channel full, dropping %s", method)
			}
		}
	}
}

// Call sends a request and waits for the response or ctx cancellation.
//
// On ctx cancellation, the pending entry is deleted to prevent a late
// response from writing to a closed channel. On error response, returns
// *Error preserving the server's Code.
func (c *Conn) Call(ctx context.Context, method string, params any, result any) error {
	if c.closed.Load() {
		return ErrConnectionClosed
	}

	id := atomic.AddInt64(&c.nextID, 1)
	entry := &pendingEntry{ch: make(chan *Response, 1)}

	c.pendingMu.Lock()
	if c.closed.Load() {
		c.pendingMu.Unlock()
		return ErrConnectionClosed
	}
	c.pending[id] = entry
	c.pendingMu.Unlock()

	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
	}
	if params != nil {
		p, err := json.Marshal(params)
		if err != nil {
			c.pendingMu.Lock()
			delete(c.pending, id)
			c.pendingMu.Unlock()
			return err
		}
		req.Params = p
	}

	c.writeMu.Lock()
	err := c.encoder.Encode(req)
	c.writeMu.Unlock()
	if err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return err
	}

	select {
	case resp := <-entry.ch:
		if resp == nil {
			return ErrConnectionClosed
		}
		if resp.Error != nil {
			return resp.Error
		}
		if result != nil && resp.Result != nil {
			return json.Unmarshal(resp.Result, result)
		}
		return nil
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return ctx.Err()
	}
}

// Notify sends a one-way notification (no ID, no response).
func (c *Conn) Notify(method string, params any) error {
	if c.closed.Load() {
		return ErrConnectionClosed
	}
	notif := Notification{
		JSONRPC: "2.0",
		Method:  method,
	}
	if params != nil {
		p, err := json.Marshal(params)
		if err != nil {
			return err
		}
		notif.Params = p
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.encoder.Encode(notif)
}

// Reply sends a response to a server→client request.
func (c *Conn) Reply(id any, result any) error {
	if c.closed.Load() {
		return ErrConnectionClosed
	}
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
	}
	if result != nil {
		r, err := json.Marshal(result)
		if err != nil {
			return err
		}
		resp.Result = r
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.encoder.Encode(resp)
}

// OnNotification registers a handler for the given method.
// readLoop will invoke the handler directly when a matching notification arrives.
// A nil handler unregisters.
func (c *Conn) OnNotification(method string, handler func(json.RawMessage)) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.handlers[method] = handler
}

// Notifications returns the channel for notifications without a registered handler.
func (c *Conn) Notifications() <-chan *Notification {
	return c.notifyChan
}

// Requests returns the channel for server→client requests.
func (c *Conn) Requests() <-chan *Request {
	return c.requestChan
}

// Done returns a channel that is closed when the connection is closed,
// allowing goroutines to exit select loops.
func (c *Conn) Done() <-chan struct{} {
	return c.done
}

// Close closes the connection. Closes done + stdin to unblock readLoop.
// Wakes all pending Call goroutines with ErrConnectionClosed.
// Idempotent.
func (c *Conn) Close() {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	close(c.done)
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	c.pendingMu.Lock()
	for id, entry := range c.pending {
		select {
		case entry.ch <- nil:
		default:
		}
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()
}

// DecodeID decodes a json.RawMessage ID to any (preserving number/string/null).
func DecodeID(raw json.RawMessage) any {
	if string(raw) == "null" {
		return nil
	}
	var i int64
	if err := json.Unmarshal(raw, &i); err == nil {
		return i
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return nil
}

// ParseIDInt decodes a json.RawMessage ID to int64 (numbers only).
func ParseIDInt(raw json.RawMessage) (int64, bool) {
	var i int64
	if err := json.Unmarshal(raw, &i); err == nil {
		return i, true
	}
	return 0, false
}
