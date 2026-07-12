package event

import (
	"sync/atomic"
	"testing"
)

func BenchmarkEventHub_Publish(b *testing.B) {
	config := DefaultEventHubConfig()
	config.UseWorkerPool = false
	bus := NewEventHub(config)
	defer bus.Close()

	var count int64
	bus.Subscribe("bench", func(e Event) error {
		atomic.AddInt64(&count, 1)
		return nil
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Publish(NewEvent("bench", i))
	}
}

func BenchmarkEventHub_PublishAsync(b *testing.B) {
	config := DefaultEventHubConfig()
	config.UseWorkerPool = true
	config.WorkerPoolSize = 4
	config.QueueSize = 4096
	bus := NewEventHub(config)
	defer bus.Close()

	bus.Subscribe("bench", func(e Event) error {
		return nil
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Publish(NewEvent("bench", i))
	}
}

func BenchmarkEventHub_SubscribeUnsubscribe(b *testing.B) {
	config := DefaultEventHubConfig()
	config.UseWorkerPool = false
	bus := NewEventHub(config)
	defer bus.Close()

	handler := func(e Event) error { return nil }

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := bus.Subscribe("bench", handler)
		bus.Unsubscribe(id)
	}
}
