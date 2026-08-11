package obs

import (
	"os"
	"sync"
	"testing"

	"ojv/cog/cam/event"
	"ojv/cog/store"
)

func newTestWALDir(t *testing.T) string {
	dir, err := os.MkdirTemp("", "cog-obs-wal-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestObservabilitySnapshot_EventHub(t *testing.T) {
	config := event.DefaultEventHubConfig()
	config.UseWorkerPool = false
	bus := event.NewEventHub(config)
	defer bus.Close()

	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe("obs_test", func(e event.Event) error {
		wg.Done()
		return nil
	})

	bus.Publish(event.NewEvent("obs_test", "data"))
	wg.Wait()

	snap := CollectObservabilitySnapshot(nil, nil, bus, nil)
	if snap.EventHub == nil {
		t.Fatal("expected non-nil EventHub snapshot")
	}
	if snap.EventHub.Subscriptions != 1 {
		t.Errorf("expected 1 subscription, got %d", snap.EventHub.Subscriptions)
	}
	if snap.WAL != nil {
		t.Error("expected nil WAL")
	}
	if snap.HTTP != nil {
		t.Error("expected nil HTTP")
	}
}

func TestObservabilitySnapshot_WAL(t *testing.T) {
	dir := newTestWALDir(t)
	w, err := store.NewSegmentedWAL(dir)
	if err != nil {
		t.Fatalf("NewSegmentedWAL failed: %v", err)
	}
	defer w.Close()

	w.Set("k", []byte("v"))

	snap := CollectObservabilitySnapshot(nil, nil, nil, w)
	if snap.WAL == nil {
		t.Fatal("snap.WAL should not be nil")
	}
	if snap.WAL.Entries != 1 {
		t.Errorf("snap.WAL.Entries = %d, want 1", snap.WAL.Entries)
	}
	if snap.HTTP != nil {
		t.Error("snap.HTTP should be nil")
	}
	if snap.WebSocket != nil {
		t.Error("snap.WebSocket should be nil")
	}
	if snap.EventHub != nil {
		t.Error("snap.EventHub should be nil")
	}
	if snap.CollectedAt.IsZero() {
		t.Error("snap.CollectedAt should not be zero")
	}
}

func TestObservabilitySnapshot_NilAll(t *testing.T) {
	snap := CollectObservabilitySnapshot(nil, nil, nil, nil)
	if snap.HTTP != nil || snap.WebSocket != nil || snap.WAL != nil || snap.EventHub != nil {
		t.Error("all fields should be nil")
	}
	if snap.CollectedAt.IsZero() {
		t.Error("CollectedAt should not be zero")
	}
}
