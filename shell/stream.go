package shell

import (
	"bufio"
	"bytes"
	"math/rand"
	"sync"
	"time"
)

// ThreadSafeBuffer is a goroutine-safe bytes.Buffer.
type ThreadSafeBuffer struct {
	b  bytes.Buffer
	mu sync.Mutex
}

func (b *ThreadSafeBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *ThreadSafeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func (b *ThreadSafeBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Bytes()
}

func (b *ThreadSafeBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.b.Reset()
}

// OutputBuffer accumulates output and can split it into lines on demand.
type OutputBuffer struct {
	buf   *bytes.Buffer
	lines []string
	mu    sync.Mutex
}

// NewOutputBuffer creates a new OutputBuffer.
func NewOutputBuffer() *OutputBuffer {
	return &OutputBuffer{
		buf:   &bytes.Buffer{},
		lines: []string{},
	}
}

func (rw *OutputBuffer) Write(p []byte) (n int, err error) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	return rw.buf.Write(p)
}

// Lines returns all accumulated output split by newlines.
// Subsequent calls append to previously returned lines.
func (rw *OutputBuffer) Lines() []string {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	s := bufio.NewScanner(rw.buf)
	for s.Scan() {
		rw.lines = append(rw.lines, s.Text())
	}
	return rw.lines
}

// OutputStream is an io.Writer that splits output into lines and sends
// each line to a channel. It supports both blocking and non-blocking modes.
//
// In blocking mode (default), Write blocks if the channel is full.
// In non-blocking mode, lines that cannot be sent are silently dropped.
type OutputStream struct {
	streamChan  chan string
	bufSize     int
	buf         []byte
	lastChar    int
	nonBlocking bool
}

// NewOutputStream creates a new OutputStream on the given channel.
// Pass true as the optional second argument to enable non-blocking mode.
func NewOutputStream(streamChan chan string, nonBlocking ...bool) *OutputStream {
	nb := false
	if len(nonBlocking) > 0 {
		nb = nonBlocking[0]
	}
	return &OutputStream{
		streamChan:  streamChan,
		bufSize:     16384,
		buf:         make([]byte, 16384),
		lastChar:    0,
		nonBlocking: nb,
	}
}

// Write implements io.Writer. It splits p on newlines and sends each
// complete line to the channel. Incomplete trailing data is buffered
// until the next Write call.
func (rw *OutputStream) Write(p []byte) (n int, err error) {
	n = len(p)
	firstChar := 0

	for {
		newlineOffset := bytes.IndexByte(p[firstChar:], '\n')
		if newlineOffset < 0 {
			break
		}

		lastChar := firstChar + newlineOffset
		if newlineOffset > 0 && p[newlineOffset-1] == '\r' {
			lastChar--
		}

		var line string
		if rw.lastChar > 0 {
			line = string(rw.buf[0:rw.lastChar])
			rw.lastChar = 0
		}
		line += string(p[firstChar:lastChar])

		if rw.nonBlocking {
			select {
			case rw.streamChan <- line:
			default:
			}
		} else {
			rw.streamChan <- line
		}

		firstChar += newlineOffset + 1
	}

	if firstChar < n {
		remain := len(p[firstChar:])
		bufFree := len(rw.buf[rw.lastChar:])
		if remain > bufFree {
			var line string
			if rw.lastChar > 0 {
				line = string(rw.buf[0:rw.lastChar])
			}
			line += string(p[firstChar:])
			rw.streamChan <- line
			rw.lastChar = 0
			err = ErrLineBufferOverflow
			n = firstChar
			return
		}
		copy(rw.buf[rw.lastChar:], p[firstChar:])
		rw.lastChar += remain
	}

	return
}

// Lines returns the underlying channel for range-iteration.
func (rw *OutputStream) Lines() <-chan string {
	return rw.streamChan
}

// SetLineBufferSize resizes the internal line buffer.
func (rw *OutputStream) SetLineBufferSize(n int) {
	rw.bufSize = n
	rw.buf = make([]byte, rw.bufSize)
}

// -----------------------------------------------------------------------
// randString — lightweight random string for temp file names
// -----------------------------------------------------------------------

const (
	letterIdxBits = 6
	letterIdxMask = 1<<letterIdxBits - 1
	letterIdxMax  = 63 / letterIdxBits
	letterBytes   = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
)

var (
	randsrc = rand.NewSource(time.Now().UnixNano())
	randMu  sync.Mutex
)

// randString generates a random alphanumeric string of length n.
func randString(n int) string {
	randMu.Lock()
	defer randMu.Unlock()
	b := make([]byte, n)
	for i, cache, remain := n-1, randsrc.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = randsrc.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}
	return string(b)
}
