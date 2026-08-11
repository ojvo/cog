package store

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// MaxSegmentSize is the maximum size of a single WAL segment before rotation.
	MaxSegmentSize = 64 * 1024 * 1024 // 64MB
	// MergeThreshold is the number of read-only segments that triggers a merge.
	MergeThreshold = 5
)

// SegmentedWAL is a segment-based write-ahead log with rotation and background merge.
type SegmentedWAL struct {
	dir          string
	segments     []*Segment
	active       *Segment
	cache        map[string][]byte
	mu           sync.RWMutex
	seq          uint64
	closed       bool
	stopChan     chan struct{}
	mergeChan    chan struct{}
	wg           sync.WaitGroup
	syncInterval int64
}

// Segment is a single WAL segment file.
type Segment struct {
	id          uint64
	path        string
	file        *os.File
	bw          *bufio.Writer
	size        int64
	readOnly    bool
	mu          sync.Mutex
	createdAt   time.Time
	lastWrite   time.Time
	recordCount int64
}

// SegmentedWALStats is a runtime statistics snapshot of a SegmentedWAL.
type SegmentedWALStats struct {
	Dir          string
	Entries      int
	Segments     int
	Closed       bool
	Sequence     uint64
	ActiveSize   int64
	TotalRecords int64
}

// WALConfig configures SegmentedWAL behavior.
type WALConfig struct {
	SyncInterval time.Duration
}

// NewSegmentedWAL creates a new SegmentedWAL in dir.
func NewSegmentedWAL(dir string) (*SegmentedWAL, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create dir failed: %w", err)
	}

	w := &SegmentedWAL{
		dir:       dir,
		cache:     make(map[string][]byte),
		stopChan:  make(chan struct{}),
		mergeChan: make(chan struct{}, 1),
	}
	w.syncInterval = int64((100 * time.Millisecond).Nanoseconds())

	if err := w.recover(); err != nil {
		return nil, fmt.Errorf("recover failed: %w", err)
	}

	if w.active == nil {
		if err := w.createNewSegment(); err != nil {
			return nil, err
		}
	}

	w.wg.Add(2)
	go w.syncLoop()
	go w.mergeLoop()

	return w, nil
}

// NewSegmentedWALWithConfig creates a SegmentedWAL with the given config.
func NewSegmentedWALWithConfig(dir string, cfg WALConfig) (*SegmentedWAL, error) {
	w, err := NewSegmentedWAL(dir)
	if err != nil {
		return nil, err
	}
	if cfg.SyncInterval > 0 {
		atomic.StoreInt64(&w.syncInterval, int64(cfg.SyncInterval.Nanoseconds()))
	}
	return w, nil
}

// SetSyncInterval updates the sync interval.
func (w *SegmentedWAL) SetSyncInterval(d time.Duration) {
	if d > 0 {
		atomic.StoreInt64(&w.syncInterval, int64(d.Nanoseconds()))
	}
}

// Set stores a key-value pair.
func (w *SegmentedWAL) Set(key string, value []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return fmt.Errorf("WAL closed")
	}

	// Copy at the API boundary: the cache must not retain caller-owned memory.
	// This keeps in-memory reads consistent with the bytes persisted to the WAL.
	storedValue := append([]byte(nil), value...)
	nextSeq := w.seq + 1
	rec := logRecord{
		Seq:   nextSeq,
		Type:  recordTypeSet,
		Key:   key,
		Value: storedValue,
	}

	// Persist before publishing the mutation to the in-memory index. A failed
	// write must leave Get and a future recovery observing the same state.
	if err := w.writeToActive(rec); err != nil {
		return err
	}
	w.cache[key] = storedValue
	w.seq = nextSeq

	if w.active.size >= MaxSegmentSize {
		return w.rotate()
	}

	return nil
}

// Get retrieves the value for key.
func (w *SegmentedWAL) Get(key string) ([]byte, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	value, ok := w.cache[key]
	if !ok {
		return nil, false
	}

	result := make([]byte, len(value))
	copy(result, value)
	return result, true
}

// Delete removes a key.
func (w *SegmentedWAL) Delete(key string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return fmt.Errorf("WAL closed")
	}

	if _, ok := w.cache[key]; !ok {
		return nil
	}

	nextSeq := w.seq + 1
	rec := logRecord{
		Seq:  nextSeq,
		Type: recordTypeDel,
		Key:  key,
	}

	// As with Set, do not expose a deletion that did not reach the WAL.
	if err := w.writeToActive(rec); err != nil {
		return err
	}
	delete(w.cache, key)
	w.seq = nextSeq

	if w.active.size >= MaxSegmentSize {
		return w.rotate()
	}

	return nil
}

// Len returns the number of keys.
func (w *SegmentedWAL) Len() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.cache)
}

// Keys returns all keys.
func (w *SegmentedWAL) Keys() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	keys := make([]string, 0, len(w.cache))
	for k := range w.cache {
		keys = append(keys, k)
	}
	return keys
}

// SegmentCount returns the total number of segments (including active).
func (w *SegmentedWAL) SegmentCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.segments) + 1
}

// GetStats returns runtime statistics.
func (w *SegmentedWAL) GetStats() SegmentedWALStats {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var activeSize int64
	var totalRecords int64
	if w.active != nil {
		activeSize = w.active.size
		totalRecords += w.active.recordCount
	}
	for _, seg := range w.segments {
		totalRecords += seg.recordCount
	}

	return SegmentedWALStats{
		Dir:          w.dir,
		Entries:      len(w.cache),
		Segments:     len(w.segments) + 1,
		Closed:       w.closed,
		Sequence:     w.seq,
		ActiveSize:   activeSize,
		TotalRecords: totalRecords,
	}
}

// Close flushes and closes the WAL.
func (w *SegmentedWAL) Close() error {
	w.mu.Lock()

	if w.closed {
		w.mu.Unlock()
		return nil
	}

	w.closed = true
	close(w.stopChan)

	var syncErr error
	if w.active != nil && w.active.file != nil {
		if w.active.bw != nil {
			w.active.bw.Flush()
		}
		syncErr = w.active.file.Sync()
	}

	w.mu.Unlock()

	w.wg.Wait()

	w.mu.Lock()
	defer w.mu.Unlock()

	var closeErr error
	if w.active != nil {
		if err := w.active.Close(); err != nil {
			closeErr = err
		}
	}

	for _, seg := range w.segments {
		if err := seg.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}

	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

// writeToActive writes a record to the active segment (caller must hold w.mu).
func (w *SegmentedWAL) writeToActive(rec logRecord) error {
	data, err := rec.encode()
	if err != nil {
		return fmt.Errorf("encode failed: %w", err)
	}

	w.active.mu.Lock()
	n, err := w.active.bw.Write(data)
	if err != nil {
		w.active.mu.Unlock()
		return fmt.Errorf("write failed: %w", err)
	}
	w.active.size += int64(n)
	w.active.recordCount++
	w.active.lastWrite = time.Now()
	w.active.mu.Unlock()

	return nil
}

// rotate seals the active segment and creates a new one (caller must hold w.mu).
func (w *SegmentedWAL) rotate() error {
	if w.active.bw != nil {
		w.active.bw.Flush()
	}
	if err := w.active.file.Sync(); err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	w.active.readOnly = true
	w.segments = append(w.segments, w.active)

	if err := w.createNewSegment(); err != nil {
		return err
	}

	if len(w.segments) >= MergeThreshold {
		select {
		case w.mergeChan <- struct{}{}:
		default:
		}
	}

	return nil
}

// createNewSegment creates a new active segment (caller must hold w.mu).
func (w *SegmentedWAL) createNewSegment() error {
	id := w.seq + 1
	path := filepath.Join(w.dir, fmt.Sprintf("wal-%020d.log", id))

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("create segment file failed: %w", err)
	}

	w.active = &Segment{
		id:          id,
		path:        path,
		file:        file,
		bw:          bufio.NewWriterSize(file, 64*1024),
		size:        0,
		createdAt:   time.Now(),
		lastWrite:   time.Now(),
		recordCount: 0,
	}

	return nil
}

// recover loads existing segments and rebuilds the cache.
func (w *SegmentedWAL) recover() error {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return err
	}

	var segmentPaths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), "wal-") && strings.HasSuffix(entry.Name(), ".log") {
			segmentPaths = append(segmentPaths, filepath.Join(w.dir, entry.Name()))
		}
	}

	sort.Strings(segmentPaths)

	for i, path := range segmentPaths {
		file, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("open segment file failed: %w", err)
		}

		count, err := w.recoverSegment(file)
		if err != nil {
			walLogf("[WAL] recover segment %s failed: %v", path, err)
		}

		info, statErr := file.Stat()
		if statErr != nil {
			file.Close()
			return fmt.Errorf("stat segment file %s failed: %w", path, statErr)
		}
		id := extractSegmentID(path)
		seg := &Segment{
			id:          id,
			path:        path,
			file:        file,
			size:        info.Size(),
			lastWrite:   info.ModTime(),
			recordCount: count,
		}

		if i < len(segmentPaths)-1 {
			seg.readOnly = true
			w.segments = append(w.segments, seg)
		} else {
			seg.bw = bufio.NewWriterSize(file, 64*1024)
			w.active = seg
		}
	}

	walLogf("[WAL] recover done: %d segments, %d records", len(segmentPaths), len(w.cache))
	return nil
}

// recoverSegment replays records from r into the cache.
func (w *SegmentedWAL) recoverSegment(r io.Reader) (int64, error) {
	br := bufio.NewReader(r)
	var cnt int64

	for {
		rec, err := decodeRecord(br)
		if err != nil {
			if err == io.EOF {
				break
			}
			if err == io.ErrUnexpectedEOF {
				break
			}
			return cnt, err
		}

		if rec.Type == recordTypeDel {
			delete(w.cache, rec.Key)
		} else {
			w.cache[rec.Key] = rec.Value
		}
		cnt++

		if rec.Seq > w.seq {
			w.seq = rec.Seq
		}
	}

	return cnt, nil
}

// syncLoop periodically flushes the active segment.
func (w *SegmentedWAL) syncLoop() {
	defer w.wg.Done()

	interval := time.Duration(atomic.LoadInt64(&w.syncInterval))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopChan:
			return
		case <-ticker.C:
			newInterval := time.Duration(atomic.LoadInt64(&w.syncInterval))
			if newInterval != interval {
				interval = newInterval
				ticker.Stop()
				ticker = time.NewTicker(interval)
			}
			w.mu.RLock()
			if !w.closed && w.active != nil && w.active.file != nil {
				w.active.mu.Lock()
				if w.active.bw != nil {
					w.active.bw.Flush()
				}
				if err := w.active.file.Sync(); err != nil {
					walLogf("[WAL] sync error: %v", err)
				}
				w.active.mu.Unlock()
			}
			w.mu.RUnlock()
		}
	}
}

// mergeLoop merges old segments when triggered.
func (w *SegmentedWAL) mergeLoop() {
	defer w.wg.Done()

	for {
		select {
		case <-w.stopChan:
			return
		case <-w.mergeChan:
			if err := w.mergeOldSegments(); err != nil {
				walLogf("[WAL] merge failed: %v", err)
			}
		}
	}
}

// mergeOldSegments compacts all read-only segments into a single snapshot segment.
func (w *SegmentedWAL) mergeOldSegments() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.segments) < MergeThreshold {
		return nil
	}

	snapshotPath := filepath.Join(w.dir, "snapshot.tmp")
	snapshot, err := os.Create(snapshotPath)
	if err != nil {
		return err
	}

	bw := bufio.NewWriterSize(snapshot, 64*1024)

	// Assign globally increasing seq numbers starting from w.seq+1
	startSeq := w.seq + 1
	currentSeq := startSeq
	var recordCount int64
	var totalSize int64
	for k, v := range w.cache {
		rec := logRecord{
			Seq:   currentSeq,
			Type:  recordTypeSet,
			Key:   k,
			Value: v,
		}

		data, err := rec.encode()
		if err != nil {
			snapshot.Close()
			os.Remove(snapshotPath)
			return err
		}

		if _, err := bw.Write(data); err != nil {
			snapshot.Close()
			os.Remove(snapshotPath)
			return err
		}
		currentSeq++
		recordCount++
		totalSize += int64(len(data))
	}

	if err := bw.Flush(); err != nil {
		snapshot.Close()
		os.Remove(snapshotPath)
		return err
	}
	snapshot.Sync()

	info, _ := snapshot.Stat()
	if info != nil {
		totalSize = info.Size()
	}
	snapshot.Close()

	// Rename snapshot BEFORE deleting old segments (avoids data loss on rename failure)
	lastSeq := currentSeq - 1
	newPath := filepath.Join(w.dir, fmt.Sprintf("wal-%020d.log", lastSeq))
	if err := os.Rename(snapshotPath, newPath); err != nil {
		os.Remove(snapshotPath)
		return err
	}

	// Now safe to remove old segments
	for _, seg := range w.segments {
		seg.Close()
		os.Remove(seg.path)
	}
	w.segments = nil

	w.seq = lastSeq

	// Add the compacted segment as read-only with proper stats
	w.segments = append(w.segments, &Segment{
		id:          lastSeq,
		path:        newPath,
		readOnly:    true,
		size:        totalSize,
		recordCount: recordCount,
		createdAt:   time.Now(),
		lastWrite:   time.Now(),
	})

	walLogf("[WAL] merge done: %d records, seq %d", len(w.cache), w.seq)

	return nil
}

// Close flushes and closes the segment file.
func (s *Segment) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.file != nil {
		err := s.file.Close()
		s.file = nil
		return err
	}
	return nil
}

// extractSegmentID parses the segment ID from a file path.
func extractSegmentID(path string) uint64 {
	base := filepath.Base(path)
	base = strings.TrimPrefix(base, "wal-")
	base = strings.TrimSuffix(base, ".log")
	id, _ := strconv.ParseUint(base, 10, 64)
	return id
}
