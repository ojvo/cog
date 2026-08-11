package timer

import (
	"sync"
	"sync/atomic"
	"time"
)

type ChainTicker struct {
	chansMu  sync.Mutex
	chans    map[chan time.Time]struct{}
	now      atomic.Value // stores time.Time
	stop     chan struct{}
	stopOnce sync.Once
	stopped  bool
}

func (t *ChainTicker) Add(ch chan time.Time) {
	t.chansMu.Lock()
	defer t.chansMu.Unlock()
	
	if t.stopped {
		return
	}
	t.chans[ch] = struct{}{}
}

func (t *ChainTicker) Remove(ch chan time.Time) {
	t.chansMu.Lock()
	delete(t.chans, ch)
	t.chansMu.Unlock()
}

func (t *ChainTicker) Stop() {
	t.stopOnce.Do(func() {
		t.chansMu.Lock()
		t.stopped = true
		t.chans = nil // Help GC
		t.chansMu.Unlock()
		close(t.stop)
	})
}

func (t *ChainTicker) LastTickTime() time.Time {
	val := t.now.Load()
	if val == nil {
		return time.Time{}
	}
	return val.(time.Time)
}

func NewChanTicker(tickInterval time.Duration) *ChainTicker {
	t := &ChainTicker{
		chans: make(map[chan time.Time]struct{}),
		stop:  make(chan struct{}),
	}
	t.now.Store(time.Now())
	tt := time.NewTicker(tickInterval)

	go func() {
		defer tt.Stop()
		for {
			select {
			case <-t.stop:
				return
			case now := <-tt.C:
				t.now.Store(now)
				t.chansMu.Lock()
				for ch := range t.chans {
					select {
					case ch <- now:
					default:
					}
				}
				t.chansMu.Unlock()
			}
		}
	}()

	return t
}
