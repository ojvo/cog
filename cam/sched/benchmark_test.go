package sched

import (
	"testing"
	"time"
)

func BenchmarkAdd(b *testing.B) {
	s := New()
	defer s.Stop()

	task := &Task{
		Interval: time.Minute,
		TaskFunc: func() error { return nil },
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := s.Add(task)
		if err != nil {
			b.Fatalf("Add failed: %s", err)
		}
	}
	b.StopTimer()
	// cleanup: drop all tasks so the scheduler doesn't keep firing timers
	for id := range s.Tasks() {
		s.Del(id)
	}
}

func BenchmarkLookup(b *testing.B) {
	s := New()
	defer s.Stop()

	id, err := s.Add(&Task{
		Interval: time.Minute,
		TaskFunc: func() error { return nil },
	})
	if err != nil {
		b.Fatalf("Add failed: %s", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := s.Lookup(id)
		if err != nil {
			b.Fatalf("Lookup failed: %s", err)
		}
	}
}

func BenchmarkTasksSnapshot(b *testing.B) {
	s := New()
	defer s.Stop()

	for i := 0; i < 100; i++ {
		err := s.AddWithID(int64(i+1), &Task{
			Interval: time.Minute,
			TaskFunc: func() error { return nil },
		})
		if err != nil {
			b.Fatalf("AddWithID failed: %s", err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.Tasks()
	}
}
