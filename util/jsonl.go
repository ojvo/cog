package util

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"sync"
)

// ErrStop 允许 callback 中止迭代而不被视为读取错误。
var ErrStop = errors.New("jsonl: stop iteration")

// initialChunk 是每次 Read 的块大小。足够大以摊薄 Read 系统调用开销
// 并给 SIMD 循环留出运行空间，足够小以至于即使是巨大的文件峰值内存
// 也保持有界。当单条记录超过此大小时按需翻倍扩容。
const initialChunk = 64 * 1024

// ForEachLine 从 r 中逐行读取原始字节并传递给 fn。
// 行尾换行符（如果存在）包含在传递给 fn 的切片中。
// 传递给 fn 的切片仅在回调期间有效——如需保留请复制。
//
// 实现要点：滚动缓冲区以 ~64KiB 块从 r 填充；每块用 bytes.IndexByte
// （在支持的架构上 AVX2 加速）扫描 '\n'。完整行立即分发；不完整的尾部
// 前移到缓冲区起始并与下一次 Read 拼接。这保留了 bufio 的早退和有界内存特性。
//
// 用 ForEachLine 而非 bufio.Scanner 的场景：大 JSONL 文件流式处理
// （session 回放、事件日志回放），需要 O(1) 内存而非 O(N) 全量加载。
func ForEachLine(r io.Reader, fn func(line []byte) error) error {
	// buf 保存 r 的未处理字节。cap 按需增长；len 是有效字节数；
	// lineStart 是当前（可能不完整的）行首字节索引。
	buf := make([]byte, 0, initialChunk)
	lineStart := 0
	var idleHits int

	for {
		// 确保有空间容纳下一次 Read。要么当前行超过 cap（lineStart==0，必须扩容），
		// 要么 lineStart 之前有已处理数据可以丢弃（lineStart>0）。
		if cap(buf) == len(buf) {
			if lineStart == 0 {
				grown := make([]byte, len(buf), cap(buf)*2)
				copy(grown, buf)
				buf = grown
			} else {
				copy(buf, buf[lineStart:])
				buf = buf[:len(buf)-lineStart]
				lineStart = 0
			}
		}

		n, err := r.Read(buf[len(buf):cap(buf)])
		if n > 0 {
			buf = buf[:len(buf)+n]
		}
		// 行为良好的 Reader 反复返回 (0, nil) 会陷入无限循环。
		// 在 100 次连续空读后报 ErrNoProgress——与 bufio.Reader 阈值一致，
		// 不会误判 TLS 握手、慢网关等合法空闲。
		if n == 0 && err == nil {
			idleHits++
			if idleHits >= 100 {
				return io.ErrNoProgress
			}
			continue
		}
		idleHits = 0

		// 分发 buf 中当前所有完整行。bytes.IndexByte 在 Go 1.21+ 的
		// amd64/arm64 上经 internal/bytealg SIMD 加速，缓冲区是唯一分配。
		for {
			idx := bytes.IndexByte(buf[lineStart:], '\n')
			if idx < 0 {
				break
			}
			nl := lineStart + idx
			line := buf[lineStart : nl+1] // 含尾部 '\n'，与 bufio 语义一致
			if cbErr := fn(line); cbErr != nil {
				if errors.Is(cbErr, ErrStop) {
					return nil
				}
				return cbErr
			}
			lineStart = nl + 1
		}

		if errors.Is(err, io.EOF) {
			if lineStart < len(buf) {
				// 最后一条记录无尾部换行符。
				line := buf[lineStart:]
				if cbErr := fn(line); cbErr != nil {
					if errors.Is(cbErr, ErrStop) {
						return nil
					}
					return cbErr
				}
			}
			return nil
		}
		if err != nil {
			// 非 nil 非 EOF 错误可能伴随 n>0。已分发的完整行保留；
			// 尾部字节丢弃——与 bufio 在错误时丢弃尾部一致。
			return err
		}
	}
}

// ---------------------------------------------------------------------------
// JSONLWriter — Append-only JSON-Lines writer
// ---------------------------------------------------------------------------

// JSONLWriter appends JSON-Lines to a file. Each write is a single line,
// avoiding the need to rewrite the entire file on each update.
//
// This is useful for append-heavy workloads like message history logging,
// where full-file serialization (as in a JSON file) becomes a bottleneck.
type JSONLWriter struct {
	mu   sync.Mutex
	file *os.File
	buf  *bufio.Writer
}

// NewJSONLWriter opens or creates a JSONL file for appending.
func NewJSONLWriter(path string) (*JSONLWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &JSONLWriter{
		file: f,
		buf:  bufio.NewWriter(f),
	}, nil
}

// Append writes a value as a single JSON line.
func (w *JSONLWriter) Append(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := w.buf.Write(data); err != nil {
		return err
	}
	if err := w.buf.WriteByte('\n'); err != nil {
		return err
	}
	return w.buf.Flush()
}

// Sync flushes buffered data and syncs the file to disk.
func (w *JSONLWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.buf.Flush(); err != nil {
		return err
	}
	return w.file.Sync()
}

// Close flushes and closes the file.
func (w *JSONLWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.buf.Flush(); err != nil {
		return err
	}
	return w.file.Close()
}

// ---------------------------------------------------------------------------
// JSONLReader — High-level JSON-Lines reader (based on ForEachLine)
// ---------------------------------------------------------------------------

// JSONLReader reads JSON-Lines from a file.
type JSONLReader struct{}

// NewJSONLReader creates a new JSONL reader.
func NewJSONLReader() *JSONLReader { return &JSONLReader{} }

// ReadAll reads all JSON lines from a file into a slice.
// Lines that fail to decode (e.g., a truncated last line from a crash
// mid-write) are skipped with a warning so that as much valid data as
// possible is recovered. This makes replay resilient to partial writes.
//
// 基于 ForEachLine 实现：SIMD 加速的行扫描（amd64/arm64 上 bytes.IndexByte
// 经 internal/bytealg 加速），动态扩容无固定行长度上限。
func (r *JSONLReader) ReadAll(path string, factory func() any) ([]any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var results []any
	err = ForEachLine(f, func(line []byte) error {
		// ForEachLine 的 line 包含尾部 '\n'。空行（仅 '\n'）跳过；
		// json.Unmarshal 忽略尾随空白，所以非空行可直接 unmarshal。
		if len(line) == 0 || (len(line) == 1 && line[0] == '\n') {
			return nil
		}
		v := factory()
		if err := json.Unmarshal(line, v); err != nil {
			log.Printf("jsonl: skip corrupt/partial line: %v", err)
			return nil
		}
		results = append(results, v)
		return nil
	})
	return results, err
}

// ReadLast reads the last N entries from a JSONL file.
func (r *JSONLReader) ReadLast(path string, n int, factory func() any) ([]any, error) {
	all, err := r.ReadAll(path, factory)
	if err != nil {
		return nil, err
	}
	if len(all) <= n {
		return all, nil
	}
	return all[len(all)-n:], nil
}
