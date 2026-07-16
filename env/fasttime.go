package env

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// FastTime caches time.Now() in atomics for high-frequency reads.
type FastTime struct {
	running atomic.Bool
	t       atomic.Value // time.Time
	unix    atomic.Int64
	unixN   atomic.Int64
	unixU   atomic.Uint32
	unixNU  atomic.Uint32
	fmtVal  atomic.Value // string
	fmtNow  atomic.Value // []byte
	cancel  context.CancelFunc
	ticker  *time.Ticker
}

var (
	onceFastTime sync.Once
	defaultFast  *FastTime
)

func init() {
	onceFastTime.Do(func() {
		defaultFast = NewFastTime().Start(context.Background(), 100*time.Millisecond)
	})
}

// NewFastTime creates a new FastTime instance.
func NewFastTime() *FastTime {
	f := &FastTime{}
	f.fmtVal.Store(time.RFC3339)
	f.fmtNow.Store([]byte{})
	return f.refresh()
}

func (f *FastTime) refresh() *FastTime {
	n := time.Now()
	f.t.Store(n)
	ut := n.Unix()
	unt := n.UnixNano()
	f.unix.Store(ut)
	f.unixN.Store(unt)
	f.unixU.Store(uint32(ut))
	f.unixNU.Store(uint32(unt))
	form := f.fmtVal.Load().(string)
	f.fmtNow.Store(n.AppendFormat(make([]byte, 0, len(form)), form))
	return f
}

// SetFormat changes the cached formatted time layout.
func (f *FastTime) SetFormat(format string) *FastTime {
	f.fmtVal.Store(format)
	return f.refresh()
}

// Start starts background refreshing at dur intervals.
func (f *FastTime) Start(ctx context.Context, dur time.Duration) *FastTime {
	if f.running.Load() {
		f.Stop()
	}
	if dur <= 0 {
		dur = 100 * time.Millisecond
	}
	f.refresh()
	ct, cancel := context.WithCancel(ctx)
	f.cancel = cancel
	f.ticker = time.NewTicker(dur)
	f.running.Store(true)
	go func() {
		for {
			select {
			case <-ct.Done():
				f.ticker.Stop()
				f.running.Store(false)
				return
			case <-f.ticker.C:
				f.refresh()
			}
		}
	}()
	return f
}

// Stop stops background refresh.
func (f *FastTime) Stop() {
	if f.cancel != nil {
		f.cancel()
	}
}

func (f *FastTime) Now() time.Time       { return f.t.Load().(time.Time) }
func (f *FastTime) UnixNow() int64       { return f.unix.Load() }
func (f *FastTime) UnixNanoNow() int64   { return f.unixN.Load() }
func (f *FastTime) UnixUNow() uint32     { return f.unixU.Load() }
func (f *FastTime) UnixUNanoNow() uint32 { return f.unixNU.Load() }
func (f *FastTime) FormattedNow() []byte {
	bs := f.fmtNow.Load().([]byte)
	cp := make([]byte, len(bs))
	copy(cp, bs)
	return cp
}

// Package-level default helpers.
func Now() time.Time                                       { return defaultFast.Now() }
func UnixNow() int64                                       { return defaultFast.UnixNow() }
func UnixNanoNow() int64                                   { return defaultFast.UnixNanoNow() }
func UnixUNow() uint32                                     { return defaultFast.UnixUNow() }
func UnixUNanoNow() uint32                                 { return defaultFast.UnixUNanoNow() }
func FormattedNow() []byte                                 { return defaultFast.FormattedNow() }
func Stop()                                                { defaultFast.Stop() }
func Start(ctx context.Context, d time.Duration) *FastTime { return defaultFast.Start(ctx, d) }
