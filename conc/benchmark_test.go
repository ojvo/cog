package conc

import (
	"sync/atomic"
	"testing"
)

func BenchmarkWorkerPool_Submit(b *testing.B) {
	pool := NewWorkerPool(4, 1024)
	pool.Start()
	defer pool.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Submit(func() {})
	}
}

func BenchmarkQueueLf_PutGet(b *testing.B) {
	q := NewQueueLf(1024)
	defer q.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Put(i)
		q.Get()
	}
}

func BenchmarkQueueLf_TryPut(b *testing.B) {
	q := NewQueueLf(1024)
	defer q.Close()

	var n int32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if ok, _ := q.TryPut(i); ok {
			atomic.AddInt32(&n, 1)
		}
	}
}

func BenchmarkQueueLf_Puts(b *testing.B) {
	q := NewQueueLf(4096)
	defer q.Close()

	values := make([]interface{}, 16)
	for i := range values {
		values[i] = i
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Puts(values)
		for j := 0; j < len(values); j++ {
			q.Get()
		}
	}
}
