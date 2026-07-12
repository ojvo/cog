package util

import (
	"sync"
	"testing"
	"time"
)

func TestMetrics_Counter(t *testing.T) {
	as := NewAssert(t)
	m := NewMetrics()

	m.Inc("requests")
	as.Equal(int64(1), m.GetCounter("requests"))

	m.AddCounter("requests", 5)
	as.Equal(int64(6), m.GetCounter("requests"))

	// Concurrency test
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Inc("concurrent")
		}()
	}
	wg.Wait()
	as.Equal(int64(100), m.GetCounter("concurrent"))
}

func TestMetrics_Gauge(t *testing.T) {
	as := NewAssert(t)
	m := NewMetrics()

	m.SetGauge("memory", 1024)
	as.Equal(int64(1024), m.GetGauge("memory"))

	m.AddGauge("memory", -24)
	as.Equal(int64(1000), m.GetGauge("memory"))
}

func TestMetrics_Histogram(t *testing.T) {
	as := NewAssert(t)
	m := NewMetrics()

	m.AddSample("latency", 10)
	m.AddSample("latency", 20)
	m.AddSample("latency", 30)

	h := m.GetHistogram("latency")
	as.Equal(int64(3), h["count"])
	as.Equal(int64(60), h["sum"])
	as.Equal(int64(10), h["min"])
	as.Equal(int64(30), h["max"])
	as.Equal(int64(20), h["avg"])
}

func TestMetrics_Timer(t *testing.T) {
	as := NewAssert(t)
	m := NewMetrics()

	stop := m.StartTimer("db_query")
	time.Sleep(10 * time.Millisecond)
	stop()

	tStats := m.GetTimer("db_query")
	as.Equal(int64(1), tStats["count"])
	as.True(tStats["total_ns"].(int64) > 0)
}

func TestMetrics_GetAll_Reset(t *testing.T) {
	as := NewAssert(t)
	m := NewMetrics()

	m.Inc("foo")
	m.SetGauge("bar", 100)
	
	all := m.GetAll()
	counters := all["counters"].(map[string]int64)
	as.Equal(int64(1), counters["foo"])

	m.Reset()
	as.Equal(int64(0), m.GetCounter("foo"))
}
