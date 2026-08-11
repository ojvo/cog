package timer

import (
	"fmt"
	"testing"
	"time"
)

func BenchmarkTimeWheel_Add(b *testing.B) {
	tw := New[int, any](time.Millisecond, 100, func(data any) {})
	tw.Start()
	defer tw.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tw.AddTimer(time.Second, i, nil)
	}
}

func BenchmarkTimeWheel_AddRemove(b *testing.B) {
	tw := New[int, any](time.Millisecond, 100, func(data any) {})
	tw.Start()
	defer tw.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tw.AddTimer(time.Second, i, nil)
		tw.RemoveTimer(i)
	}
}

func BenchmarkTimeWheel_ParallelAdd(b *testing.B) {
	tw := New[string, any](time.Millisecond, 100, func(data any) {})
	tw.Start()
	defer tw.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			i++
			tw.AddTimer(time.Second, fmt.Sprintf("%d-%d", time.Now().UnixNano(), i), nil)
		}
	})
}
