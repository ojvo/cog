package obs

import (
	"time"

	"ojv/cog/cam/event"
	"ojv/cog/netx"
	"ojv/cog/store"
)

// ObservabilitySnapshot aggregates runtime stats from multiple components.
// Nil fields indicate the component was not provided.
type ObservabilitySnapshot struct {
	CollectedAt time.Time
	HTTP        *netx.HTTPClientStats
	WebSocket   *netx.WSConnectionStats
	EventHub    *event.EventHubSnapshot
	WAL         *store.SegmentedWALStats
}

// CollectObservabilitySnapshot gathers runtime stats from the provided components.
// Any nil argument is skipped.
func CollectObservabilitySnapshot(httpClient *netx.HTTPClient, ws *netx.WSConnection, hub *event.EventHub, wal *store.SegmentedWAL) ObservabilitySnapshot {
	s := ObservabilitySnapshot{CollectedAt: time.Now()}

	if httpClient != nil {
		h := httpClient.GetStats()
		s.HTTP = &h
	}
	if ws != nil {
		wss := ws.GetStats()
		s.WebSocket = &wss
	}
	if hub != nil {
		hs := hub.GetSnapshot()
		s.EventHub = &hs
	}
	if wal != nil {
		w := wal.GetStats()
		s.WAL = &w
	}

	return s
}
