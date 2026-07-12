package netx

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// startWSEchoServer starts a minimal WebSocket echo server for testing.
func startWSEchoServer(t *testing.T) (string, func()) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()

	stop := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-stop:
					return
				default:
					return
				}
			}

			go func(c net.Conn) {
				defer c.Close()

				// Read HTTP request
				buf := make([]byte, 4096)
				n, err := c.Read(buf)
				if err != nil {
					return
				}

				req := string(buf[:n])
				if !strings.Contains(req, "Upgrade: websocket") {
					return
				}

				// Extract Sec-WebSocket-Key
				lines := strings.Split(req, "\r\n")
				var wsKey string
				for _, line := range lines {
					if strings.HasPrefix(line, "Sec-WebSocket-Key: ") {
						wsKey = strings.TrimPrefix(line, "Sec-WebSocket-Key: ")
						break
					}
				}
				if wsKey == "" {
					return
				}

				// Compute accept key
				const magic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
				h := sha1.New()
				h.Write([]byte(wsKey + magic))
				acceptKey := base64.StdEncoding.EncodeToString(h.Sum(nil))

				// Send handshake response
				resp := fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\n"+
					"Upgrade: websocket\r\n"+
					"Connection: Upgrade\r\n"+
					"Sec-WebSocket-Accept: %s\r\n\r\n", acceptKey)
				_, _ = c.Write([]byte(resp))

				// Echo loop
				for {
					c.SetReadDeadline(time.Now().Add(30 * time.Second))
					header := make([]byte, 2)
					_, err := io.ReadFull(c, header)
					if err != nil {
						return
					}

					opcode := header[0] & 0x0F
					masked := (header[1] & 0x80) != 0
					payloadLen := int64(header[1] & 0x7F)

					if payloadLen == 126 {
						extLen := make([]byte, 2)
						if _, err := io.ReadFull(c, extLen); err != nil {
							return
						}
						payloadLen = int64(binary.BigEndian.Uint16(extLen))
					}

					var maskKey []byte
					if masked {
						maskKey = make([]byte, 4)
						if _, err := io.ReadFull(c, maskKey); err != nil {
							return
						}
					}

					payload := make([]byte, payloadLen)
					if _, err := io.ReadFull(c, payload); err != nil {
						return
					}

					if masked {
						for i := range payload {
							payload[i] ^= maskKey[i%4]
						}
					}

					// Handle control frames
					if opcode == OpcodeClose {
						return
					}
					if opcode == OpcodePing {
						// Reply with Pong
						frame := buildTestFrame(OpcodePong, payload, false)
						_, _ = c.Write(frame)
						continue
					}

					// Echo back as text
					frame := buildTestFrame(OpcodeText, payload, false)
					_, _ = c.Write(frame)
				}
			}(conn)
		}
	}()

	cleanup := func() {
		close(stop)
		_ = ln.Close()
		time.Sleep(100 * time.Millisecond)
	}

	return "ws://" + addr, cleanup
}

func buildTestFrame(opcode byte, payload []byte, mask bool) []byte {
	frame := make([]byte, 0, len(payload)+14)
	b0 := byte(opcode) | 0x80 // FIN=1
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
		frame = append(frame, 127)
		for i := 7; i >= 0; i-- {
			frame = append(frame, byte(payloadLen>>(i*8)))
		}
	}

	frame = append(frame, payload...)
	return frame
}

func TestWSConnection_ConnectAndEcho(t *testing.T) {
	wsURL, cleanup := startWSEchoServer(t)
	defer cleanup()

	ws := NewWSConnection(wsURL, &WSConfig{
		PingInterval:   100 * time.Second, // Disable ping for test
		PongWait:       30 * time.Second,
		WriteWait:      5 * time.Second,
		MaxMessageSize: 512 * 1024,
		FragmentSize:   0, // No fragmentation
		EnablePing:     false,
	})

	if err := ws.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer ws.Close()

	if !ws.IsConnected() {
		t.Error("expected connected")
	}

	// Set up message handler
	received := make(chan []byte, 1)
	ws.SetMessageHandler(func(msgType byte, data []byte) {
		received <- data
	})

	// Send a message
	if err := ws.WriteText([]byte("hello world")); err != nil {
		t.Fatalf("WriteText failed: %v", err)
	}

	// Wait for echo
	select {
	case data := <-received:
		if string(data) != "hello world" {
			t.Errorf("got %q, want 'hello world'", string(data))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for echo")
	}
}

func TestWSConnection_WriteJSON(t *testing.T) {
	wsURL, cleanup := startWSEchoServer(t)
	defer cleanup()

	ws := NewWSConnection(wsURL, &WSConfig{
		PongWait:       30 * time.Second,
		WriteWait:      5 * time.Second,
		MaxMessageSize: 512 * 1024,
		EnablePing:     false,
	})

	if err := ws.Connect(); err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	received := make(chan []byte, 1)
	ws.SetMessageHandler(func(msgType byte, data []byte) {
		received <- data
	})

	type testMsg struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	if err := ws.WriteJSON(testMsg{Name: "test", Age: 42}); err != nil {
		t.Fatal(err)
	}

	select {
	case data := <-received:
		if !strings.Contains(string(data), `"name":"test"`) {
			t.Errorf("unexpected response: %s", string(data))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestWSConnection_Stats(t *testing.T) {
	wsURL, cleanup := startWSEchoServer(t)
	defer cleanup()

	ws := NewWSConnection(wsURL, &WSConfig{
		PongWait:       30 * time.Second,
		WriteWait:      5 * time.Second,
		MaxMessageSize: 512 * 1024,
		EnablePing:     false,
	})

	_ = ws.Connect()
	defer ws.Close()

	stats := ws.GetStats()
	if !stats.Connected {
		t.Error("stats should show connected")
	}
	if stats.WriteQueueCap != 256 {
		t.Errorf("writeQueueCap = %d, want 256", stats.WriteQueueCap)
	}
}

func TestWSConnection_LargeMessage(t *testing.T) {
	wsURL, cleanup := startWSEchoServer(t)
	defer cleanup()

	ws := NewWSConnection(wsURL, &WSConfig{
		PongWait:       30 * time.Second,
		WriteWait:      5 * time.Second,
		MaxMessageSize: 512 * 1024,
		FragmentSize:   0, // No fragmentation (echo server doesn't reassemble)
		EnablePing:     false,
	})

	if err := ws.Connect(); err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	received := make(chan []byte, 1)
	ws.SetMessageHandler(func(msgType byte, data []byte) {
		received <- data
	})

	// Send 10KB message (single frame, > 126 bytes so uses 16-bit length)
	largeMsg := strings.Repeat("x", 10000)
	if err := ws.WriteText([]byte(largeMsg)); err != nil {
		t.Fatal(err)
	}

	select {
	case data := <-received:
		if len(data) != 10000 {
			t.Errorf("got %d bytes, want 10000", len(data))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestWSConnection_PingPong(t *testing.T) {
	wsURL, cleanup := startWSEchoServer(t)
	defer cleanup()

	ws := NewWSConnection(wsURL, &WSConfig{
		PingInterval:   100 * time.Millisecond,
		PongWait:       5 * time.Second,
		WriteWait:      5 * time.Second,
		MaxMessageSize: 512 * 1024,
		EnablePing:     true,
	})

	if err := ws.Connect(); err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	// Wait for ping/pong cycle
	time.Sleep(300 * time.Millisecond)

	stats := ws.GetStats()
	if stats.LastPongAgo > 5*time.Second {
		t.Errorf("lastPongAgo = %v, expected recent", stats.LastPongAgo)
	}
}

func TestWSConnection_Close(t *testing.T) {
	wsURL, cleanup := startWSEchoServer(t)
	defer cleanup()

	ws := NewWSConnection(wsURL, &WSConfig{
		EnablePing: false,
	})

	if err := ws.Connect(); err != nil {
		t.Fatal(err)
	}

	ws.Close()

	if ws.IsConnected() {
		t.Error("expected not connected after close")
	}

	stats := ws.GetStats()
	if !stats.Closing {
		t.Error("stats should show closing")
	}
}

func TestWSConnection_ErrorHandler(t *testing.T) {
	// Connect to a non-existent server
	ws := NewWSConnection("ws://127.0.0.1:0", &WSConfig{
		EnablePing:   false,
		PongWait:     1 * time.Second,
		WriteWait:    1 * time.Second,
		PingInterval: 100 * time.Second,
	})

	errChan := make(chan error, 5)
	ws.SetErrorHandler(func(err error) {
		errChan <- err
	})

	// Connect should fail
	err := ws.Connect()
	if err == nil {
		ws.Close()
		t.Fatal("expected connection error")
	}

	ws.Close()
}

func TestWSConnection_DefaultConfig(t *testing.T) {
	config := DefaultWSConfig()
	if config.PingInterval != 30*time.Second {
		t.Errorf("PingInterval = %v, want 30s", config.PingInterval)
	}
	if config.MaxMessageSize != 512*1024 {
		t.Errorf("MaxMessageSize = %d, want %d", config.MaxMessageSize, 512*1024)
	}
	if !config.EnablePing {
		t.Error("EnablePing should be true by default")
	}
}

func TestWSConnection_NilConfig(t *testing.T) {
	ws := NewWSConnection("ws://localhost", nil)
	if ws.config == nil {
		t.Error("nil config should default to DefaultWSConfig")
	}
}

// Suppress unused import warning for http (used in handshake parsing reference)
var _ = http.MethodGet
