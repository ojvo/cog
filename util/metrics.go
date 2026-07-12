package util

import (
	"sync"
	"sync/atomic"
	"time"
)

// Metrics 通用指标收集器
type Metrics struct {
	counters   sync.Map // 计数器 map[string]*int64
	gauges     sync.Map // 仪表盘 map[string]*int64
	histograms sync.Map // 直方图 map[string]*Histogram
	timers     sync.Map // 计时器 map[string]*Timer
	startTime  time.Time
}

// Histogram 直方图数据结构
type Histogram struct {
	count int64
	sum   int64
	min   int64
	max   int64
	mutex sync.RWMutex
}

// Timer 计时器数据结构
type Timer struct {
	count    int64
	totalNs  int64
	mutex    sync.RWMutex
	startMap sync.Map // 记录每个goroutine的开始时间
}

// NewMetrics 创建新的指标收集器
func NewMetrics() *Metrics {
	return &Metrics{
		startTime: time.Now(),
	}
}

// Counter 相关方法

// Inc 增加计数器值
func (m *Metrics) Inc(name string) {
	m.AddCounter(name, 1)
}

// AddCounter 增加计数器特定值
func (m *Metrics) AddCounter(name string, delta int64) {
	val, _ := m.counters.LoadOrStore(name, new(int64))
	atomic.AddInt64(val.(*int64), delta)
}

// GetCounter 获取计数器值
func (m *Metrics) GetCounter(name string) int64 {
	val, ok := m.counters.Load(name)
	if !ok {
		return 0
	}
	return atomic.LoadInt64(val.(*int64))
}

// Gauge 相关方法

// SetGauge 设置仪表盘值
func (m *Metrics) SetGauge(name string, value int64) {
	val, _ := m.gauges.LoadOrStore(name, new(int64))
	atomic.StoreInt64(val.(*int64), value)
}

// AddGauge 增加仪表盘值
func (m *Metrics) AddGauge(name string, delta int64) {
	val, _ := m.gauges.LoadOrStore(name, new(int64))
	atomic.AddInt64(val.(*int64), delta)
}

// GetGauge 获取仪表盘值
func (m *Metrics) GetGauge(name string) int64 {
	val, ok := m.gauges.Load(name)
	if !ok {
		return 0
	}
	return atomic.LoadInt64(val.(*int64))
}

// Histogram 相关方法

// AddSample 添加样本到直方图
func (m *Metrics) AddSample(name string, value int64) {
	var h *Histogram
	val, ok := m.histograms.Load(name)
	if !ok {
		h = &Histogram{
			min: value,
			max: value,
		}
		val, _ = m.histograms.LoadOrStore(name, h)
		h = val.(*Histogram)
	} else {
		h = val.(*Histogram)
	}

	h.mutex.Lock()
	defer h.mutex.Unlock()

	h.count++
	h.sum += value
	if value < h.min {
		h.min = value
	}
	if value > h.max {
		h.max = value
	}
}

// GetHistogram 获取直方图统计信息
func (m *Metrics) GetHistogram(name string) map[string]int64 {
	val, ok := m.histograms.Load(name)
	if !ok {
		return map[string]int64{
			"count": 0,
			"sum":   0,
			"min":   0,
			"max":   0,
			"avg":   0,
		}
	}

	h := val.(*Histogram)
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	avg := int64(0)
	if h.count > 0 {
		avg = h.sum / h.count
	}

	return map[string]int64{
		"count": h.count,
		"sum":   h.sum,
		"min":   h.min,
		"max":   h.max,
		"avg":   avg,
	}
}

// Timer 相关方法

// StartTimer 开始计时
func (m *Metrics) StartTimer(name string) func() {
	val, _ := m.timers.LoadOrStore(name, &Timer{})
	timer := val.(*Timer)

	// 使用goroutine ID作为key
	id := getGoroutineID()
	timer.startMap.Store(id, time.Now())

	return func() {
		startTime, ok := timer.startMap.Load(id)
		if !ok {
			return
		}
		timer.startMap.Delete(id)

		elapsed := time.Since(startTime.(time.Time)).Nanoseconds()
		timer.mutex.Lock()
		defer timer.mutex.Unlock()
		timer.count++
		timer.totalNs += elapsed
	}
}

// GetTimer 获取计时器统计信息
func (m *Metrics) GetTimer(name string) map[string]interface{} {
	val, ok := m.timers.Load(name)
	if !ok {
		return map[string]interface{}{
			"count":        int64(0),
			"total_ns":     int64(0),
			"avg_ns":       int64(0),
			"avg_ms":       float64(0),
			"total_ms":     float64(0),
			"calls_per_sec": float64(0),
		}
	}

	timer := val.(*Timer)
	timer.mutex.RLock()
	defer timer.mutex.RUnlock()

	avgNs := int64(0)
	if timer.count > 0 {
		avgNs = timer.totalNs / timer.count
	}

	uptime := time.Since(m.startTime).Seconds()
	callsPerSec := float64(0)
	if uptime > 0 {
		callsPerSec = float64(timer.count) / uptime
	}

	return map[string]interface{}{
		"count":        timer.count,
		"total_ns":     timer.totalNs,
		"avg_ns":       avgNs,
		"avg_ms":       float64(avgNs) / 1000000.0,
		"total_ms":     float64(timer.totalNs) / 1000000.0,
		"calls_per_sec": callsPerSec,
	}
}

// 通用方法

// GetAll 获取所有指标
func (m *Metrics) GetAll() map[string]interface{} {
	result := map[string]interface{}{
		"uptime_seconds": time.Since(m.startTime).Seconds(),
		"counters":       map[string]int64{},
		"gauges":         map[string]int64{},
		"histograms":     map[string]map[string]int64{},
		"timers":         map[string]map[string]interface{}{},
	}

	counters := result["counters"].(map[string]int64)
	m.counters.Range(func(key, value interface{}) bool {
		counters[key.(string)] = atomic.LoadInt64(value.(*int64))
		return true
	})

	gauges := result["gauges"].(map[string]int64)
	m.gauges.Range(func(key, value interface{}) bool {
		gauges[key.(string)] = atomic.LoadInt64(value.(*int64))
		return true
	})

	histograms := result["histograms"].(map[string]map[string]int64)
	m.histograms.Range(func(key, value interface{}) bool {
		histograms[key.(string)] = m.GetHistogram(key.(string))
		return true
	})

	timers := result["timers"].(map[string]map[string]interface{})
	m.timers.Range(func(key, value interface{}) bool {
		timers[key.(string)] = m.GetTimer(key.(string))
		return true
	})

	return result
}

// Reset 重置所有指标
func (m *Metrics) Reset() {
	m.counters = sync.Map{}
	m.gauges = sync.Map{}
	m.histograms = sync.Map{}
	m.timers = sync.Map{}
	m.startTime = time.Now()
}

// 辅助函数

// goroutineCounter generates unique IDs for StartTimer calls.
var goroutineCounter atomic.Uint64

// getGoroutineID returns a unique identifier for each StartTimer call.
// Uses an atomic counter to guarantee uniqueness across concurrent goroutines.
func getGoroutineID() uint64 {
	return goroutineCounter.Add(1)
}