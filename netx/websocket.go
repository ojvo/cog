package netx

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// WebSocket opcodes (RFC 6455)
	OpcodeContinuation = 0x0
	OpcodeText         = 0x1
	OpcodeBinary       = 0x2
	OpcodeClose        = 0x8
	OpcodePing         = 0x9
	OpcodePong         = 0xA

	defaultPingInterval    = 30 * time.Second
	defaultPongWait        = 60 * time.Second
	defaultWriteWait       = 10 * time.Second
	defaultReadBufferSize  = 4096
	defaultWriteBufferSize = 4096
	defaultMaxMessageSize  = 512 * 1024 // 512KB
)

// WSConfig holds WebSocket client configuration.
type WSConfig struct {
	PingInterval    time.Duration
	PongWait        time.Duration
	WriteWait       time.Duration
	MaxMessageSize  int64
	FragmentSize    int64
	EnablePing      bool
	Headers         map[string]string
	TLSClientConfig *tls.Config
}

// DefaultWSConfig returns a production-ready default config.
func DefaultWSConfig() *WSConfig {
	return &WSConfig{
		PingInterval:   defaultPingInterval,
		PongWait:       defaultPongWait,
		WriteWait:      defaultWriteWait,
		MaxMessageSize: defaultMaxMessageSize,
		FragmentSize:   16 * 1024,
		EnablePing:     true,
	}
}

// WSMessageHandler handles incoming messages.
type WSMessageHandler func(messageType byte, data []byte)

// WSErrorHandler handles errors.
type WSErrorHandler func(err error)

// WSReconnectHandler is called after a successful reconnection.
type WSReconnectHandler func() error

// WSConnection is a WebSocket client with auto-reconnect, heartbeat,
// message fragmentation, and TLS support.
type WSConnection struct {
	url          string
	conn         net.Conn
	br           *bufio.Reader
	connected    int32
	closing      int32
	reconnecting int32
	connGen      uint64
	ctx          context.Context
	cancel       context.CancelFunc
	config       *WSConfig

	onMessage   WSMessageHandler
	onError     WSErrorHandler
	onReconnect WSReconnectHandler

	writeChan chan []byte
	writeMu   sync.Mutex

	pongHandler func(data string) error
	lastPong    time.Time
	pongMu      sync.RWMutex

	fragmentBuffer []byte
	fragmentOpcode byte

	wg        sync.WaitGroup
	closeOnce sync.Once
}

// NewWSConnection creates a new WebSocket connection.
func NewWSConnection(wsURL string, config *WSConfig) *WSConnection {
	if config == nil {
		config = DefaultWSConfig()
	} else {
		// Fill zero-value numeric/duration fields with defaults so callers
		// can pass a partial config (e.g. &WSConfig{EnablePing: false})
		// without hitting immediate i/o timeouts. Bool fields are kept as-is
		// since zero (false) is a valid intent.
		if config.PingInterval == 0 {
			config.PingInterval = defaultPingInterval
		}
		if config.PongWait == 0 {
			config.PongWait = defaultPongWait
		}
		if config.WriteWait == 0 {
			config.WriteWait = defaultWriteWait
		}
		if config.MaxMessageSize == 0 {
			config.MaxMessageSize = defaultMaxMessageSize
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &WSConnection{
		url:       wsURL,
		ctx:       ctx,
		cancel:    cancel,
		config:    config,
		writeChan: make(chan []byte, 256),
		lastPong:  time.Now(),
	}
}

// SetMessageHandler sets the message callback.
func (ws *WSConnection) SetMessageHandler(handler WSMessageHandler) {
	ws.onMessage = handler
}

// SetErrorHandler sets the error callback.
func (ws *WSConnection) SetErrorHandler(handler WSErrorHandler) {
	ws.onError = handler
}

// SetReconnectHandler sets the callback invoked after a successful reconnect.
func (ws *WSConnection) SetReconnectHandler(handler WSReconnectHandler) {
	ws.onReconnect = handler
}

// SetPongHandler sets the pong handler.
func (ws *WSConnection) SetPongHandler(handler func(data string) error) {
	ws.pongHandler = handler
}

func (ws *WSConnection) isClosing() bool {
	return atomic.LoadInt32(&ws.closing) == 1
}

// Connect establishes the WebSocket connection and starts background loops.
func (ws *WSConnection) Connect() error {
	if ws.isClosing() {
		return fmt.Errorf("connection is closing")
	}
	if atomic.LoadInt32(&ws.connected) == 1 {
		return nil
	}

	u, err := url.Parse(ws.url)
	if err != nil {
		return fmt.Errorf("invalid WebSocket URL: %w", err)
	}

	host := u.Host
	if !strings.Contains(host, ":") {
		if u.Scheme == "wss" {
			host = host + ":443"
		} else {
			host = host + ":80"
		}
	}

	var conn net.Conn
	if u.Scheme == "wss" {
		tlsConfig := ws.config.TLSClientConfig
		if tlsConfig == nil {
			tlsConfig = &tls.Config{ServerName: strings.Split(u.Host, ":")[0]}
		}
		tlsConn, err := tls.Dial("tcp", host, tlsConfig)
		if err != nil {
			return fmt.Errorf("TLS connection failed: %w", err)
		}
		conn = tlsConn
	} else {
		tcpConn, err := net.Dial("tcp", host)
		if err != nil {
			return fmt.Errorf("TCP connection failed: %w", err)
		}
		conn = tcpConn
	}

	ws.br = bufio.NewReaderSize(conn, defaultReadBufferSize)

	if err := ws.performHandshake(conn, u); err != nil {
		conn.Close()
		return fmt.Errorf("WebSocket handshake failed: %w", err)
	}

	ws.conn = conn
	atomic.StoreInt32(&ws.connected, 1)
	generation := atomic.AddUint64(&ws.connGen, 1)
	ws.lastPong = time.Now()

	ws.wg.Add(1)
	go func() {
		defer ws.wg.Done()
		ws.readLoop(generation)
	}()

	ws.wg.Add(1)
	go func() {
		defer ws.wg.Done()
		ws.writeLoop(generation)
	}()

	if ws.config.EnablePing {
		ws.wg.Add(1)
		go func() {
			defer ws.wg.Done()
			ws.pingLoop(generation)
		}()
	}

	return nil
}

func (ws *WSConnection) performHandshake(conn net.Conn, u *url.URL) error {
	key := generateWebSocketKey()

	hostHeader := u.Host
	if strings.Contains(u.Host, ":") {
		hostHeader = strings.Split(u.Host, ":")[0]
	}

	path := u.Path
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path = path + "?" + u.RawQuery
	}

	var reqBuilder strings.Builder
	fmt.Fprintf(&reqBuilder, "GET %s HTTP/1.1\r\n", path)
	fmt.Fprintf(&reqBuilder, "Host: %s\r\n", hostHeader)
	fmt.Fprintf(&reqBuilder, "Upgrade: websocket\r\n")
	fmt.Fprintf(&reqBuilder, "Connection: Upgrade\r\n")
	fmt.Fprintf(&reqBuilder, "Sec-WebSocket-Key: %s\r\n", key)
	fmt.Fprintf(&reqBuilder, "Sec-WebSocket-Version: 13\r\n")
	fmt.Fprintf(&reqBuilder, "Origin: %s://%s\r\n", u.Scheme, hostHeader)

	for k, v := range ws.config.Headers {
		fmt.Fprintf(&reqBuilder, "%s: %s\r\n", k, v)
	}

	fmt.Fprintf(&reqBuilder, "\r\n")

	conn.SetWriteDeadline(time.Now().Add(ws.config.WriteWait))
	_, err := conn.Write([]byte(reqBuilder.String()))
	if err != nil {
		return err
	}

	conn.SetReadDeadline(time.Now().Add(ws.config.PongWait))
	resp, err := http.ReadResponse(ws.br, &http.Request{Method: "GET"})
	if err != nil {
		return err
	}

	if resp.StatusCode != 101 {
		return fmt.Errorf("handshake failed: status %s", resp.Status)
	}

	if strings.ToLower(resp.Header.Get("Upgrade")) != "websocket" ||
		strings.ToLower(resp.Header.Get("Connection")) != "upgrade" {
		return fmt.Errorf("handshake failed: invalid upgrade headers")
	}

	expectedAccept := computeWSAcceptKey(key)
	if resp.Header.Get("Sec-WebSocket-Accept") != expectedAccept {
		return fmt.Errorf("invalid Sec-WebSocket-Accept")
	}

	return nil
}

func isNetworkCloseError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "use of closed network connection") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "EOF")
}

func (ws *WSConnection) readLoop(generation uint64) {
	defer func() {
		if generation == atomic.LoadUint64(&ws.connGen) {
			atomic.StoreInt32(&ws.connected, 0)
		}
	}()

	for {
		if generation != atomic.LoadUint64(&ws.connGen) {
			return
		}
		if ws.isClosing() {
			return
		}

		select {
		case <-ws.ctx.Done():
			return
		default:
		}

		ws.conn.SetReadDeadline(time.Now().Add(ws.config.PongWait))

		payload, opcode, fin, err := ws.readFrame()
		if err != nil {
			if ws.isClosing() {
				return
			}
			if ws.onError != nil && !isNetworkCloseError(err) {
				ws.onError(err)
			}
			ws.scheduleReconnect(generation)
			return
		}

		switch opcode {
		case OpcodePing:
			_ = ws.WriteControl(OpcodePong, payload)
			continue
		case OpcodePong:
			ws.pongMu.Lock()
			ws.lastPong = time.Now()
			ws.pongMu.Unlock()
			if ws.pongHandler != nil {
				_ = ws.pongHandler(string(payload))
			}
			continue
		case OpcodeClose:
			ws.scheduleReconnect(generation)
			return
		}

		if !fin {
			currentLen := 0
			if ws.fragmentBuffer != nil {
				currentLen = len(ws.fragmentBuffer)
			}
			if int64(currentLen+len(payload)) > ws.config.MaxMessageSize {
				if ws.onError != nil {
					ws.onError(fmt.Errorf("fragmented message too large > %d", ws.config.MaxMessageSize))
				}
				ws.fragmentBuffer = nil
				ws.conn.Close()
				return
			}
			if ws.fragmentBuffer == nil {
				ws.fragmentOpcode = opcode
				ws.fragmentBuffer = payload
			} else {
				ws.fragmentBuffer = append(ws.fragmentBuffer, payload...)
			}
			continue
		}

		var fullPayload []byte
		var fullOpcode byte

		if ws.fragmentBuffer != nil {
			fullPayload = append(ws.fragmentBuffer, payload...)
			fullOpcode = ws.fragmentOpcode
			ws.fragmentBuffer = nil
		} else {
			fullPayload = payload
			fullOpcode = opcode
		}

		if int64(len(fullPayload)) > ws.config.MaxMessageSize {
			if ws.onError != nil {
				ws.onError(fmt.Errorf("message too large: %d bytes", len(fullPayload)))
			}
			continue
		}

		if ws.onMessage != nil {
			ws.onMessage(fullOpcode, fullPayload)
		}
	}
}

func (ws *WSConnection) writeLoop(generation uint64) {
	for {
		if generation != atomic.LoadUint64(&ws.connGen) {
			return
		}

		select {
		case <-ws.ctx.Done():
			return
		case data, ok := <-ws.writeChan:
			if !ok {
				return
			}
			if ws.isClosing() {
				return
			}

			ws.writeMu.Lock()
			ws.conn.SetWriteDeadline(time.Now().Add(ws.config.WriteWait))
			_, err := ws.conn.Write(data)
			ws.writeMu.Unlock()

			if err != nil {
				if !ws.isClosing() {
					if ws.onError != nil && !isNetworkCloseError(err) {
						ws.onError(fmt.Errorf("write error: %w", err))
					}
					if generation == atomic.LoadUint64(&ws.connGen) {
						atomic.StoreInt32(&ws.connected, 0)
					}
					if ws.conn != nil {
						ws.conn.Close()
					}
					ws.scheduleReconnect(generation)
				}
				return
			}
		}
	}
}

func (ws *WSConnection) pingLoop(generation uint64) {
	ticker := time.NewTicker(ws.config.PingInterval)
	defer ticker.Stop()

	for {
		if generation != atomic.LoadUint64(&ws.connGen) {
			return
		}

		select {
		case <-ws.ctx.Done():
			return
		case <-ticker.C:
			if ws.isClosing() {
				return
			}

			if atomic.LoadInt32(&ws.connected) == 1 {
				ws.pongMu.RLock()
				lastPong := ws.lastPong
				ws.pongMu.RUnlock()

				if time.Since(lastPong) > ws.config.PongWait {
					if ws.onError != nil && !ws.isClosing() {
						ws.onError(fmt.Errorf("pong timeout"))
					}
					if ws.conn != nil {
						ws.conn.Close()
					}
					ws.scheduleReconnect(generation)
					return
				}

				if err := ws.WriteControl(OpcodePing, []byte("ping")); err != nil && ws.onError != nil && !ws.isClosing() {
					ws.onError(err)
				}
			}
		}
	}
}

func (ws *WSConnection) scheduleReconnect(generation uint64) {
	if ws.isClosing() {
		return
	}
	if generation != atomic.LoadUint64(&ws.connGen) {
		return
	}
	if !atomic.CompareAndSwapInt32(&ws.reconnecting, 0, 1) {
		return
	}

	go func(gen uint64) {
		defer atomic.StoreInt32(&ws.reconnecting, 0)
		ws.reconnect(gen)
	}(generation)
}

func (ws *WSConnection) reconnect(generation uint64) {
	if ws.isClosing() {
		return
	}

	retryCount := 0
	maxRetries := 10

	for retryCount < maxRetries {
		if generation != atomic.LoadUint64(&ws.connGen) {
			return
		}
		if ws.isClosing() {
			return
		}

		select {
		case <-ws.ctx.Done():
			return
		default:
		}

		retryCount++
		waitTime := time.Duration(retryCount) * 2 * time.Second
		if waitTime > 30*time.Second {
			waitTime = 30 * time.Second
		}

		timer := time.NewTimer(waitTime)
		select {
		case <-ws.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		err := ws.Connect()
		if err == nil {
			if ws.onReconnect != nil {
				if err := ws.onReconnect(); err != nil && ws.onError != nil {
					ws.onError(fmt.Errorf("reconnect callback failed: %w", err))
				}
			}
			return
		}

		if ws.onError != nil && !ws.isClosing() {
			ws.onError(fmt.Errorf("reconnect attempt %d/%d failed: %w", retryCount, maxRetries, err))
		}
	}
}

// WriteMessage sends a text or binary message with optional fragmentation.
func (ws *WSConnection) WriteMessage(opcode byte, data []byte) error {
	if atomic.LoadInt32(&ws.connected) == 0 {
		return fmt.Errorf("connection not established")
	}
	if ws.isClosing() {
		return fmt.Errorf("connection is closing")
	}

	fragmentSize := int(ws.config.FragmentSize)
	if fragmentSize <= 0 || len(data) <= fragmentSize {
		frame, err := ws.buildFrame(opcode, data, true, true)
		if err != nil {
			return err
		}
		return ws.sendFrame(frame)
	}

	total := len(data)
	offset := 0

	for offset < total {
		end := offset + fragmentSize
		if end > total {
			end = total
		}

		chunk := data[offset:end]
		isLast := end == total

		frameOpcode := opcode
		if offset > 0 {
			frameOpcode = OpcodeContinuation
		}

		frame, err := ws.buildFrame(frameOpcode, chunk, true, isLast)
		if err != nil {
			return err
		}
		if err := ws.sendFrame(frame); err != nil {
			return err
		}

		offset = end
	}

	return nil
}

func (ws *WSConnection) sendFrame(frame []byte) error {
	select {
	case ws.writeChan <- frame:
		return nil
	case <-time.After(ws.config.WriteWait):
		return fmt.Errorf("write timeout")
	case <-ws.ctx.Done():
		return fmt.Errorf("connection closed")
	}
}

// WriteText sends a text message.
func (ws *WSConnection) WriteText(data []byte) error {
	return ws.WriteMessage(OpcodeText, data)
}

// WriteBinary sends a binary message.
func (ws *WSConnection) WriteBinary(data []byte) error {
	return ws.WriteMessage(OpcodeBinary, data)
}

// WriteJSON marshals v as JSON and sends it as a text message.
func (ws *WSConnection) WriteJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("json marshal error: %w", err)
	}
	return ws.WriteText(data)
}

// WriteControl sends a control frame (ping/pong/close) directly,
// bypassing the write queue.
func (ws *WSConnection) WriteControl(opcode byte, data []byte) error {
	if atomic.LoadInt32(&ws.connected) == 0 {
		return fmt.Errorf("connection not established")
	}
	if ws.isClosing() {
		return nil
	}

	frame, err := ws.buildFrame(opcode, data, true, true)
	if err != nil {
		return err
	}

	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()

	ws.conn.SetWriteDeadline(time.Now().Add(ws.config.WriteWait))
	_, err = ws.conn.Write(frame)

	if ws.isClosing() || isNetworkCloseError(err) {
		return nil
	}

	return err
}

func (ws *WSConnection) buildFrame(opcode byte, payload []byte, mask bool, fin bool) ([]byte, error) {
	frame := make([]byte, 0, len(payload)+14)

	b0 := byte(opcode)
	if fin {
		b0 |= 0x80
	}
	frame = append(frame, b0)

	payloadLen := len(payload)
	if payloadLen < 126 {
		if mask {
			frame = append(frame, 0x80|byte(payloadLen))
		} else {
			frame = append(frame, byte(payloadLen))
		}
	} else if payloadLen < 65536 {
		if mask {
			frame = append(frame, 0x80|126)
		} else {
			frame = append(frame, 126)
		}
		frame = append(frame, byte(payloadLen>>8), byte(payloadLen))
	} else {
		if mask {
			frame = append(frame, 0x80|127)
		} else {
			frame = append(frame, 127)
		}
		for i := 7; i >= 0; i-- {
			frame = append(frame, byte(payloadLen>>(i*8)))
		}
	}

	if mask {
		maskKey := make([]byte, 4)
		_, _ = rand.Read(maskKey)
		frame = append(frame, maskKey...)

		for i, b := range payload {
			frame = append(frame, b^maskKey[i%4])
		}
	} else {
		frame = append(frame, payload...)
	}

	return frame, nil
}

func (ws *WSConnection) readFrame() (payload []byte, opcode byte, fin bool, err error) {
	header := make([]byte, 2)
	_, err = io.ReadFull(ws.br, header)
	if err != nil {
		return nil, 0, false, err
	}

	fin = (header[0] & 0x80) != 0
	opcode = header[0] & 0x0F
	masked := (header[1] & 0x80) != 0
	payloadLen := int64(header[1] & 0x7F)

	if payloadLen == 126 {
		extLen := make([]byte, 2)
		_, err = io.ReadFull(ws.br, extLen)
		if err != nil {
			return nil, 0, false, err
		}
		payloadLen = int64(binary.BigEndian.Uint16(extLen))
	} else if payloadLen == 127 {
		extLen := make([]byte, 8)
		_, err = io.ReadFull(ws.br, extLen)
		if err != nil {
			return nil, 0, false, err
		}
		payloadLen = int64(binary.BigEndian.Uint64(extLen))
	}

	if payloadLen > ws.config.MaxMessageSize {
		return nil, 0, false, fmt.Errorf("message too large: %d bytes", payloadLen)
	}

	var maskKey []byte
	if masked {
		maskKey = make([]byte, 4)
		_, err = io.ReadFull(ws.br, maskKey)
		if err != nil {
			return nil, 0, false, err
		}
	}

	payload = make([]byte, payloadLen)
	_, err = io.ReadFull(ws.br, payload)
	if err != nil {
		return nil, 0, false, err
	}

	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}

	return payload, opcode, fin, nil
}

// Close gracefully shuts down the connection.
func (ws *WSConnection) Close() error {
	ws.closeOnce.Do(func() {
		atomic.StoreInt32(&ws.closing, 1)

		if ws.conn != nil && atomic.LoadInt32(&ws.connected) == 1 {
			closeFrame, _ := ws.buildFrame(OpcodeClose, []byte{}, true, true)
			ws.conn.SetWriteDeadline(time.Now().Add(ws.config.WriteWait))
			ws.writeMu.Lock()
			_, _ = ws.conn.Write(closeFrame)
			ws.writeMu.Unlock()
		}

		ws.cancel()

		if ws.conn != nil {
			_ = ws.conn.Close()
		}

		ws.wg.Wait()
	})

	return nil
}

// IsConnected returns true if the connection is active.
func (ws *WSConnection) IsConnected() bool {
	return atomic.LoadInt32(&ws.connected) == 1 && !ws.isClosing()
}

// WSConnectionStats is a snapshot of WebSocket runtime statistics.
type WSConnectionStats struct {
	Connected     bool
	Closing       bool
	Reconnecting  bool
	ConnectionGen uint64
	WriteQueueLen int
	WriteQueueCap int
	LastPongAgo   time.Duration
}

// GetStats returns a snapshot of runtime statistics.
func (ws *WSConnection) GetStats() WSConnectionStats {
	ws.pongMu.RLock()
	lastPong := ws.lastPong
	ws.pongMu.RUnlock()

	return WSConnectionStats{
		Connected:     atomic.LoadInt32(&ws.connected) == 1,
		Closing:       atomic.LoadInt32(&ws.closing) == 1,
		Reconnecting:  atomic.LoadInt32(&ws.reconnecting) == 1,
		ConnectionGen: atomic.LoadUint64(&ws.connGen),
		WriteQueueLen: len(ws.writeChan),
		WriteQueueCap: cap(ws.writeChan),
		LastPongAgo:   time.Since(lastPong),
	}
}

func generateWebSocketKey() string {
	key := make([]byte, 16)
	_, _ = rand.Read(key)
	return base64.StdEncoding.EncodeToString(key)
}

func computeWSAcceptKey(key string) string {
	const magic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.New()
	h.Write([]byte(key + magic))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
