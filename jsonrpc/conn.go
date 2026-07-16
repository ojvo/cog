package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"c.n/ojv/cog/log"
)

// ErrConnectionClosed is returned when a Call or Notify is attempted on a
// closed connection, or when a pending Call is woken by Close.
var ErrConnectionClosed = errors.New("jsonrpc: connection closed")

type pendingEntry struct {
	ch chan *Response
}

// Conn is a bidirectional JSON-RPC 2.0 connection over io.Reader/io.Writer.
type Conn struct {
	encoder *json.Encoder
	decoder *json.Decoder
	writeMu sync.Mutex
	stdin   io.Closer

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

		if hasID && method != "" {
			req := &Request{JSONRPC: "2.0", ID: DecodeID(idRaw), Method: method}
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

			notif := &Notification{JSONRPC: "2.0", Method: method}
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

	req := Request{JSONRPC: "2.0", ID: id, Method: method}
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
	err := c.encoder.Encode(&req)
	c.writeMu.Unlock()
	if err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return err
	}

	select {
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return ctx.Err()
	case <-c.done:
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return ErrConnectionClosed
	case resp := <-entry.ch:
		if resp == nil {
			return ErrConnectionClosed
		}
		if resp.Error != nil {
			return resp.Error
		}
		if result != nil && len(resp.Result) > 0 {
			return json.Unmarshal(resp.Result, result)
		}
		return nil
	}
}

func (c *Conn) Notify(method string, params any) error {
	if c.closed.Load() {
		return ErrConnectionClosed
	}
	n := Notification{JSONRPC: "2.0", Method: method}
	if params != nil {
		p, err := json.Marshal(params)
		if err != nil {
			return err
		}
		n.Params = p
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.encoder.Encode(&n)
}

func (c *Conn) Reply(id ID, result any, rpcErr *Error) error {
	if c.closed.Load() {
		return ErrConnectionClosed
	}
	resp := Response{JSONRPC: "2.0", ID: id, Error: rpcErr}
	if rpcErr == nil && result != nil {
		p, err := json.Marshal(result)
		if err != nil {
			return err
		}
		resp.Result = p
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.encoder.Encode(&resp)
}

func (c *Conn) OnNotification(method string, fn func(json.RawMessage)) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.handlers[method] = fn
}

func (c *Conn) Notifications() <-chan *Notification { return c.notifyChan }
func (c *Conn) Requests() <-chan *Request         { return c.requestChan }

func (c *Conn) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(c.done)
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	c.pendingMu.Lock()
	for id, entry := range c.pending {
		delete(c.pending, id)
		close(entry.ch)
	}
	c.pendingMu.Unlock()
	return nil
}
