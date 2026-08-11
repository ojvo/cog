package event

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"ojv/cog/cam/conc"
)

// Event 事件结构
type Event struct {
	Type      string
	Data      interface{}
	Timestamp int64           // 事件创建时间戳
	Metadata  interface{}     // 可用于存储额外信息
	Context   context.Context // 上下文支持，用于取消和传递值
	EventID   string          // 事件唯一标识符
	RequestID string          // 如果是请求事件，存储请求ID
}

// eventIDSeq guarantees EventID uniqueness even when multiple events are
// created within the same nanosecond (time.Now().UnixNano() alone collides
// under high concurrency or fast clocks).
var eventIDSeq uint64

// newEventID returns a globally-unique event identifier combining the event
// type, a nanosecond timestamp, and a monotonically increasing sequence.
func newEventID(eventType string) string {
	seq := atomic.AddUint64(&eventIDSeq, 1)
	return fmt.Sprintf("evt_%s_%d_%d", eventType, time.Now().UnixNano(), seq)
}

// NewEvent 创建新事件
func NewEvent(eventType string, data interface{}) Event {
	return Event{
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now().UnixNano(),
		Context:   context.Background(),
		EventID:   newEventID(eventType),
	}
}

// WithContext 设置事件上下文
func (e Event) WithContext(ctx context.Context) Event {
	e.Context = ctx
	return e
}

// WithMetadata 设置事件元数据
func (e Event) WithMetadata(metadata interface{}) Event {
	e.Metadata = metadata
	return e
}

// EventHandler 事件处理函数
type EventHandler func(event Event) error

// EventMiddleware transforms or rejects an event before dispatch.
// Return a non-nil error to drop the event silently.
type EventMiddleware func(Event) (Event, error)

// EventBatchHandler processes a batch of events of the same type.
type EventBatchHandler func(events []Event) error

// QueuePolicy controls backpressure when an async queue is full.
type QueuePolicy int

const (
	// DropOnFull discards events when the queue is full.
	DropOnFull QueuePolicy = iota
	// BlockOnFull blocks the publisher until space is available.
	BlockOnFull
)

// HandlerOptions 处理器选项
type HandlerOptions struct {
	// 是否异步处理
	Async bool
	// 处理超时时间
	Timeout time.Duration
	// 重试次数
	Retries int
	// 过滤器
	Filter func(event Event) bool
	// 错误处理
	OnError func(event Event, err error)
}

// DefaultHandlerOptions 默认处理器选项
func DefaultHandlerOptions() HandlerOptions {
	return HandlerOptions{
		Async:   true,
		Timeout: 5 * time.Second,
		Retries: 0,
		Filter:  nil,
		OnError: nil,
	}
}

// Subscription 订阅信息
type Subscription struct {
	ID      string
	Type    string
	Handler EventHandler
	Options HandlerOptions
}

// EventHub 事件总线
type EventHub struct {
	// 订阅映射表 eventType -> []Subscription
	subscriptions sync.Map
	// 保护 subscriptions map 中切片的修改（[]Subscription 不可比较，无法用 CAS）
	subMu sync.Mutex
	// 计数器，用于生成唯一ID
	idCounter int64
	mu        sync.Mutex
	// 配置
	config EventHubConfig
	// 工作池
	workerPool *conc.WorkerPool

	// 请求统计
	requestStats struct {
		totalRequests     int64
		successRequests   int64
		timeoutRequests   int64
		cancelledRequests int64
		failedRequests    int64
		statsMu           sync.RWMutex
	}

	// 限流器
	rateLimiter struct {
		enabled    bool
		rateLimit  int       // 每秒最大请求数
		tokens     int       // 当前可用令牌数
		lastRefill time.Time // 上次补充令牌时间
		mu         sync.Mutex
	}

	// 活跃请求跟踪
	activeRequests sync.Map // 存储 requestID -> RequestEvent

	// 事件处理记录，用于去重
	processedEvents struct {
		cache map[string]bool
		mu    sync.RWMutex
		// 最大缓存大小
		maxSize int
	}

	// Per-event-type flow metrics (processed/dropped counters)
	eventMetrics struct {
		processed sync.Map // map[string]*int64
		dropped   sync.Map // map[string]*int64
	}

	// Middleware chain (copy-on-write via atomic.Value)
	middlewares atomic.Value // []EventMiddleware

	// Per-event-type dedicated async workers: eventType -> []*asyncSubWorker
	asyncWorkers sync.Map

	// Per-event-type batch workers: eventType -> *batchSubWorker
	batchWorkers sync.Map

	// Lifecycle for async/batch workers
	hubCtx    context.Context
	hubCancel context.CancelFunc
	hubWg     sync.WaitGroup
	hubClosed atomic.Bool
}

// EventHubConfig 事件总线配置
type EventHubConfig struct {
	// 慢处理阈值
	SlowThreshold time.Duration
	// 是否使用工作池
	UseWorkerPool bool
	// 工作池大小
	WorkerPoolSize int
	// 队列大小
	QueueSize int
	// 日志函数
	LogFunc func(level string, format string, args ...interface{})
	// 是否启用事件去重
	EnableEventDeduplication bool
	// 是否检查重复订阅
	CheckDuplicateSubscription bool
}

// DefaultEventHubConfig 默认配置
func DefaultEventHubConfig() EventHubConfig {
	return EventHubConfig{
		SlowThreshold:              100 * time.Millisecond,
		UseWorkerPool:              true,
		WorkerPoolSize:             0, // 0表示使用CPU核心数
		QueueSize:                  1000,
		LogFunc:                    nil,
		EnableEventDeduplication:   false, // 默认不启用事件去重
		CheckDuplicateSubscription: false, // 默认不检查重复订阅
	}
}

// NewEventHub 创建事件总线
func NewEventHub(config EventHubConfig) *EventHub {
	if config.SlowThreshold == 0 {
		config.SlowThreshold = 100 * time.Millisecond
	}

	bus := &EventHub{
		subscriptions: sync.Map{},
		config:        config,
		processedEvents: struct {
			cache   map[string]bool
			mu      sync.RWMutex
			maxSize int
		}{
			cache:   make(map[string]bool),
			maxSize: 10000, // 可配置
		},
	}
	bus.middlewares.Store([]EventMiddleware{})
	bus.hubCtx, bus.hubCancel = context.WithCancel(context.Background())

	if config.UseWorkerPool {
		bus.workerPool = conc.NewWorkerPool(config.WorkerPoolSize, config.QueueSize)
		bus.workerPool.Start()
	}

	return bus
}

// Subscribe 订阅事件
// 添加检查函数是否已订阅
func (bus *EventHub) isHandlerSubscribed(eventType string, handler EventHandler) bool {
	if value, ok := bus.subscriptions.Load(eventType); ok {
		subs := value.([]Subscription)

		// 比较函数指针可能不可靠，这里只是一个简单示例
		handlerPtr := fmt.Sprintf("%p", handler)

		for _, sub := range subs {
			subHandlerPtr := fmt.Sprintf("%p", sub.Handler)
			if subHandlerPtr == handlerPtr {
				return true
			}
		}
	}
	return false
}

// 修改 Subscribe 方法
func (bus *EventHub) Subscribe(eventType string, handler EventHandler) string {
	if eventType == "" {
		bus.logf("ERROR", "尝试订阅空事件类型")
		return ""
	}
	if handler == nil {
		bus.logf("ERROR", "尝试为事件类型 %s 注册空处理函数", eventType)
		return ""
	}

	// 检查是否已订阅（根据配置决定是否启用）
	if bus.config.CheckDuplicateSubscription && bus.isHandlerSubscribed(eventType, handler) {
		bus.logf("WARN", "处理函数已订阅事件类型 %s，跳过重复订阅", eventType)
		return ""
	}

	return bus.SubscribeWithOptions(eventType, handler, DefaultHandlerOptions())
}

// Publish 发布事件
func (bus *EventHub) Publish(event Event) {
	if bus.hubClosed.Load() {
		bus.logf("WARN", "EventHub已关闭，忽略事件 %s", event.Type)
		bus.incEventDropped(event.Type)
		return
	}
	if event.Type == "" {
		bus.logf("ERROR", "尝试发布空类型事件")
		return
	}

	// 确保事件有唯一ID
	if event.EventID == "" {
		event.EventID = newEventID(event.Type)
	}

	// 原子地检查并标记事件已处理（消除 TOCTOU 竞态）
	if bus.config.EnableEventDeduplication && bus.checkAndMarkEvent(event.EventID) {
		bus.logf("DEBUG", "事件 %s (ID: %s) 已处理过，跳过", event.Type, event.EventID)
		return
	}

	// Apply middleware chain
	var mwOK bool
	if event, mwOK = bus.applyMiddlewares(event); !mwOK {
		return
	}

	// 获取订阅者
	subs := bus.getSubscriptions(event)

	bus.logf("DEBUG", "发布事件 %s (ID: %s) 给 %d 个订阅者", event.Type, event.EventID, len(subs))

	// Dispatch to regular subscribers
	for _, sub := range subs {
		// 创建副本避免闭包问题
		currentSub := sub
		currentEvent := event

		if sub.Options.Async {
			if bus.config.UseWorkerPool && bus.workerPool != nil {
				// 使用工作池，检查返回值
				if !bus.workerPool.Submit(func() {
					bus.executeHandler(currentSub, currentEvent)
				}) {
					bus.incEventDropped(currentEvent.Type)
					bus.logf("WARN", "工作池不可用，丢弃事件 %s (ID: %s)", currentEvent.Type, currentEvent.EventID)
				}
			} else {
				// 使用goroutine
				go bus.executeHandler(currentSub, currentEvent)
			}
		} else {
			// 同步执行
			bus.executeHandler(currentSub, currentEvent)
		}
	}

	// Dispatch to dedicated async workers (per-subscription bounded queue)
	if val, ok := bus.asyncWorkers.Load(event.Type); ok {
		for _, w := range val.([]*asyncSubWorker) {
			w.send(event)
		}
	}

	// Dispatch to batch worker (per-event-type)
	if val, ok := bus.batchWorkers.Load(event.Type); ok {
		val.(*batchSubWorker).send(event)
	}

	bus.incEventProcessed(event.Type)
}

// SubscribeWithOptions 使用自定义选项订阅事件
func (bus *EventHub) SubscribeWithOptions(eventType string, handler EventHandler, options HandlerOptions) string {
	id := bus.generateID(eventType)

	sub := Subscription{
		ID:      id,
		Type:    eventType,
		Handler: handler,
		Options: options,
	}

	bus.addSubscription(eventType, sub)
	return id
}

// Unsubscribe 取消订阅 - 修复并发安全问题
// Also handles async subscription IDs.
func (bus *EventHub) Unsubscribe(id string) bool {
	// Check async workers first
	if bus.UnsubscribeAsync(id) {
		return true
	}

	bus.subMu.Lock()
	defer bus.subMu.Unlock()

	var found bool
	var eventTypeToUpdate string
	var newSubs []Subscription

	bus.subscriptions.Range(func(key, value interface{}) bool {
		eventType := key.(string)
		subs := value.([]Subscription)

		for i, sub := range subs {
			if sub.ID == id {
				newSubs = make([]Subscription, 0, len(subs)-1)
				newSubs = append(newSubs, subs[:i]...)
				newSubs = append(newSubs, subs[i+1:]...)
				eventTypeToUpdate = eventType
				found = true
				return false
			}
		}
		return true
	})

	if found {
		if len(newSubs) == 0 {
			bus.subscriptions.Delete(eventTypeToUpdate)
		} else {
			bus.subscriptions.Store(eventTypeToUpdate, newSubs)
		}
	}

	return found
}

// 添加一个新方法，用于检查事件类型是否有订阅者
func (bus *EventHub) HasSubscribers(eventType string) bool {
	if value, ok := bus.subscriptions.Load(eventType); ok {
		subs := value.([]Subscription)
		return len(subs) > 0
	}
	return false
}

// 添加一个新方法，用于获取事件类型的订阅者数量
func (bus *EventHub) GetSubscriberCount(eventType string) int {
	if value, ok := bus.subscriptions.Load(eventType); ok {
		subs := value.([]Subscription)
		return len(subs)
	}
	return 0
}

// 添加一个新方法，用于批量取消订阅
func (bus *EventHub) UnsubscribeAll(eventType string) int {
	bus.subMu.Lock()
	defer bus.subMu.Unlock()

	if value, ok := bus.subscriptions.Load(eventType); ok {
		subs := value.([]Subscription)
		count := len(subs)
		bus.subscriptions.Delete(eventType)
		return count
	}
	return 0
}

// checkAndMarkEvent atomically checks whether eventID was already processed
// and, if not, marks it as processed. Returns true if the event was already
// seen (caller should skip it). The check-and-mark runs under a single write
// lock, eliminating the TOCTOU race that existed when isEventProcessed (RLock)
// and markEventProcessed (Lock) were separate critical sections.
func (bus *EventHub) checkAndMarkEvent(eventID string) bool {
	bus.processedEvents.mu.Lock()
	defer bus.processedEvents.mu.Unlock()

	if bus.processedEvents.cache[eventID] {
		return true
	}
	// Bound memory: when full, evict ~10% of entries instead of wiping the
	// entire cache. A full wipe would forget all recently-seen events and
	// allow duplicates through during the window before the cache repopulates.
	if len(bus.processedEvents.cache) >= bus.processedEvents.maxSize {
		evict := bus.processedEvents.maxSize / 10
		if evict < 1 {
			evict = 1
		}
		n := 0
		for k := range bus.processedEvents.cache {
			delete(bus.processedEvents.cache, k)
			n++
			if n >= evict {
				break
			}
		}
	}
	bus.processedEvents.cache[eventID] = true
	return false
}

// PublishAndWait 发布事件并等待所有处理完成
func (bus *EventHub) PublishAndWait(event Event) []error {
	if bus.hubClosed.Load() {
		bus.logf("WARN", "EventHub已关闭，忽略事件 %s", event.Type)
		bus.incEventDropped(event.Type)
		return []error{fmt.Errorf("EventHub已关闭")}
	}
	if event.Type == "" {
		return []error{fmt.Errorf("尝试发布空类型事件")}
	}

	// 确保事件有唯一ID
	if event.EventID == "" {
		event.EventID = newEventID(event.Type)
	}

	// 原子地检查并标记事件已处理（消除 TOCTOU 竞态）
	if bus.config.EnableEventDeduplication && bus.checkAndMarkEvent(event.EventID) {
		bus.logf("DEBUG", "事件 %s (ID: %s) 已处理过，跳过", event.Type, event.EventID)
		return nil
	}

	// Apply middleware chain
	var mwOK bool
	if event, mwOK = bus.applyMiddlewares(event); !mwOK {
		return nil
	}

	subs := bus.getSubscriptions(event)
	if len(subs) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(subs))

	wg.Add(len(subs))
	for _, sub := range subs {
		go func(s Subscription) {
			defer wg.Done()
			if err := bus.executeHandlerWithTimeout(s, event); err != nil {
				errChan <- err
			}
		}(sub)
	}

	wg.Wait()
	close(errChan)

	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	return errors
}

// 内部方法

func (bus *EventHub) getSubscriptions(event Event) []Subscription {
	if value, ok := bus.subscriptions.Load(event.Type); ok {
		subs := value.([]Subscription)
		result := make([]Subscription, 0, len(subs))

		for _, sub := range subs {
			if sub.Options.Filter == nil || sub.Options.Filter(event) {
				result = append(result, sub)
			}
		}

		return result
	}

	return nil
}

func (bus *EventHub) executeHandler(sub Subscription, event Event) {
	defer func() {
		if r := recover(); r != nil {
			bus.logf("ERROR", "处理事件 %s 时发生panic: %v", event.Type, r)
		}
	}()

	startTime := time.Now()

	// 执行处理函数
	err := sub.Handler(event)

	// 处理错误
	if err != nil && sub.Options.OnError != nil {
		// 使用工作池处理错误回调
		bus.submitTask(func() {
			sub.Options.OnError(event, err)
		})
	}

	elapsed := time.Since(startTime)
	if elapsed > bus.config.SlowThreshold {
		bus.logf("WARN", "事件 %s 处理耗时 %v", event.Type, elapsed)
	}
}

func (bus *EventHub) executeHandlerWithTimeout(sub Subscription, event Event) (err error) {
	if sub.Options.Timeout <= 0 {
		err = sub.Handler(event)
		return
	}

	ctx, cancel := context.WithTimeout(event.Context, sub.Options.Timeout)
	defer cancel()
	event.Context = ctx

	// Buffered result channel: the goroutine can always send its outcome
	// without blocking, even after the timeout path has already returned.
	// This avoids the data race on a shared err variable and ensures the
	// goroutine does not leak waiting on an unbuffered channel. The goroutine
	// itself is not killable (Go has no goroutine kill); handlers MUST honor
	// ctx.Done() to terminate promptly on timeout.
	type result struct{ err error }
	resultCh := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				bus.logf("ERROR", "处理事件 %s 时发生panic: %v", event.Type, r)
				resultCh <- result{err: fmt.Errorf("事件 %s 处理panic: %v", event.Type, r)}
			}
		}()
		resultCh <- result{err: sub.Handler(event)}
	}()

	select {
	case r := <-resultCh:
		return r.err
	case <-ctx.Done():
		return fmt.Errorf("处理事件 %s 超时", event.Type)
	}
}

func (bus *EventHub) generateID(eventType string) string {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	bus.idCounter++
	return fmt.Sprintf("sub_%s_%d", eventType, bus.idCounter)
}

func (bus *EventHub) addSubscription(eventType string, sub Subscription) {
	bus.subMu.Lock()
	defer bus.subMu.Unlock()

	actual, ok := bus.subscriptions.Load(eventType)
	if !ok {
		bus.subscriptions.Store(eventType, []Subscription{sub})
		return
	}

	subs := actual.([]Subscription)
	newSubs := append(subs, sub)
	bus.subscriptions.Store(eventType, newSubs)
}

func (bus *EventHub) logf(level string, format string, args ...interface{}) {
	if bus.config.LogFunc != nil {
		bus.config.LogFunc(level, format, args...)
	}
}

// Close 关闭事件总线
func (bus *EventHub) Close() {
	if !bus.hubClosed.CompareAndSwap(false, true) {
		return
	}

	// Stop batch workers
	bus.batchWorkers.Range(func(key, value interface{}) bool {
		value.(*batchSubWorker).stop()
		return true
	})

	// Stop async workers
	bus.asyncWorkers.Range(func(key, value interface{}) bool {
		for _, w := range value.([]*asyncSubWorker) {
			w.stop()
		}
		return true
	})

	// Cancel context and wait for all async/batch goroutines with timeout
	bus.hubCancel()

	done := make(chan struct{})
	go func() {
		bus.hubWg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		bus.logf("ERROR", "EventHub Close 超时，部分 goroutine 未退出")
	}

	// Stop worker pool
	if bus.workerPool != nil {
		bus.workerPool.Stop()
	}
}

// IEventHub 事件中心接口
// 更新 IEventHub 接口，包含请求-响应相关方法
type IEventHub interface {
	// 基本方法
	Subscribe(eventType string, handler EventHandler) string
	SubscribeWithOptions(eventType string, handler EventHandler, options HandlerOptions) string
	Unsubscribe(id string) bool
	Publish(event Event)
	PublishAndWait(event Event) []error
	Close()

	// 请求-响应方法
	Request(req RequestEvent, timeout time.Duration) (ResponseEvent, error)
	RequestWithOptions(req RequestEvent, options EvtReqOpts) (ResponseEvent, error)
	CancelRequest(requestID string) bool
	Respond(req RequestEvent, success bool, data interface{}, err error)

	// 请求管理和监控方法
	GetActiveRequestsCount() int
	GetRequestStats() map[string]int64
	EnableRateLimiter(rateLimit int)
}

// 为了保持向后兼容，保留 MessageBus 类型作为 EventHub 的别名
type MessageBus = EventHub
type IMessageBus = IEventHub

// NewMessageBus 创建兼容旧接口的消息总线
func NewMessageBus(slowProcessThreshold time.Duration) *EventHub {
	config := DefaultEventHubConfig()
	config.LogFunc = func(level string, format string, args ...interface{}) {
		//Infof(format, args...) // 替换为你的日志记录函数
	}
	config.SlowThreshold = slowProcessThreshold
	return NewEventHub(config)
}

// 统一 EventFilter 类型定义，使用类型别名
type EventFilter = func(event Event) bool

// SubscriptionOptions 订阅选项
type SubscriptionOptions struct {
	// 处理优先级，数字越小优先级越高
	Priority int
	// 事件过滤器，返回true表示处理该事件
	Filter EventFilter
	// 是否异步处理
	Async bool
	// 处理超时时间
	Timeout time.Duration
}

// 添加请求-响应支持相关结构体和方法

// requestIDSeq guarantees RequestID uniqueness (same rationale as eventIDSeq).
var requestIDSeq uint64

// AsRequest 将事件标记为请求事件
func (e Event) AsRequest() Event {
	seq := atomic.AddUint64(&requestIDSeq, 1)
	e.RequestID = fmt.Sprintf("req_%s_%d_%d", e.Type, time.Now().UnixNano(), seq)
	return e
}

// RequestEvent 请求事件结构
type RequestEvent struct {
	Event                           // 关联的事件
	ResponseChan chan ResponseEvent // 用于接收响应的通道
	CancelFunc   context.CancelFunc // 用于取消请求的函数
}

// ResponseEvent 响应事件结构
type ResponseEvent struct {
	Event   Event       // 关联的事件
	Success bool        // 操作是否成功
	Error   error       // 如果失败，错误信息
	Data    interface{} // 响应数据
}

// NewRequestEvent 创建新请求事件
func NewRequestEvent(eventType string, data interface{}) RequestEvent {
	// 创建可取消的上下文
	ctx, cancelFunc := context.WithCancel(context.Background())

	// 创建基础事件并标记为请求
	event := NewEvent(eventType, data).
		WithContext(ctx).
		AsRequest()

	return RequestEvent{
		Event:        event,
		ResponseChan: make(chan ResponseEvent, 1), // 缓冲为1，避免阻塞
		CancelFunc:   cancelFunc,
	}
}

// 添加请求统计方法
func (bus *EventHub) recordRequestStat(status string) {
	bus.requestStats.statsMu.Lock()
	defer bus.requestStats.statsMu.Unlock()

	bus.requestStats.totalRequests++

	switch status {
	case "success":
		bus.requestStats.successRequests++
	case "timeout":
		bus.requestStats.timeoutRequests++
	case "cancelled":
		bus.requestStats.cancelledRequests++
	case "failed":
		bus.requestStats.failedRequests++
	}
}

// 获取请求统计信息
func (bus *EventHub) GetRequestStats() map[string]int64 {
	bus.requestStats.statsMu.RLock()
	defer bus.requestStats.statsMu.RUnlock()

	return map[string]int64{
		"total":     bus.requestStats.totalRequests,
		"success":   bus.requestStats.successRequests,
		"timeout":   bus.requestStats.timeoutRequests,
		"cancelled": bus.requestStats.cancelledRequests,
		"failed":    bus.requestStats.failedRequests,
	}
}

// 添加限流器初始化
func (bus *EventHub) EnableRateLimiter(rateLimit int) {
	bus.rateLimiter.mu.Lock()
	defer bus.rateLimiter.mu.Unlock()

	bus.rateLimiter.enabled = true
	bus.rateLimiter.rateLimit = rateLimit
	bus.rateLimiter.tokens = rateLimit
	bus.rateLimiter.lastRefill = time.Now()
}

// 限流器检查
func (bus *EventHub) checkRateLimit() bool {
	if !bus.rateLimiter.enabled {
		return true // 未启用限流器，直接通过
	}

	bus.rateLimiter.mu.Lock()
	defer bus.rateLimiter.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(bus.rateLimiter.lastRefill)

	// 根据时间流逝补充令牌
	newTokens := int(elapsed.Seconds() * float64(bus.rateLimiter.rateLimit))
	if newTokens > 0 {
		bus.rateLimiter.tokens = min(bus.rateLimiter.rateLimit, bus.rateLimiter.tokens+newTokens)
		bus.rateLimiter.lastRefill = now
	}

	// 检查是否有可用令牌
	if bus.rateLimiter.tokens > 0 {
		bus.rateLimiter.tokens--
		return true
	}

	return false // 无可用令牌，请求被限流
}

func (bus *EventHub) submitTask(task func()) {
	if bus.config.UseWorkerPool && bus.workerPool != nil {
		if bus.workerPool.Submit(task) {
			return
		}
		// 工作池不可用，fallback 到 goroutine 保证回调不丢失
		bus.logf("WARN", "工作池不可用，回调降级为 goroutine 执行")
	}
	go task()
}

// 提取请求处理公共逻辑
// 修改 processRequestWithCallbacks 方法，使用工作池处理回调
func (bus *EventHub) processRequestWithCallbacks(
	req RequestEvent,
	timeout time.Duration,
	onSuccess func(ResponseEvent),
	onTimeout func(RequestEvent),
	onCancel func(RequestEvent, error),
) {
	// 等待响应、超时或取消
	select {
	case resp := <-req.ResponseChan:
		bus.recordRequestStat("success")
		if onSuccess != nil {
			// 使用工作池处理成功回调
			bus.submitTask(func() {
				onSuccess(resp)
			})
		}
	case <-time.After(timeout):
		// 不 close 通道：避免 Respond() 并发发送时 panic。
		// 通道由 GC 回收，Respond 有 1s 超时不会永久阻塞。
		bus.recordRequestStat("timeout")
		if onTimeout != nil {
			// 使用工作池处理超时回调
			bus.submitTask(func() {
				onTimeout(req)
			})
		}
	case <-req.Context.Done():
		// 请求被取消
		bus.recordRequestStat("cancelled")
		if onCancel != nil {
			// 使用工作池处理取消回调
			bus.submitTask(func() {
				onCancel(req, req.Context.Err())
			})
		}
	}
}

// 修改 PublishRequest 方法，使用提取的公共逻辑
func (bus *EventHub) Request(req RequestEvent, timeout time.Duration) (ResponseEvent, error) {
	options := DefaultEvtReqOpts()
	options.Async = true
	options.Timeout = timeout

	return bus.RequestWithOptions(req, options)
}

// RespondTo 响应请求
func (bus *EventHub) Respond(req RequestEvent, success bool, data interface{}, err error) {
	if req.ResponseChan == nil {
		bus.logf("ERROR", "响应通道为空: %s", req.Event.Type)
		return
	}

	resp := ResponseEvent{
		Event:   NewEvent(req.Event.Type+"_response", data),
		Success: success,
		Error:   err,
		Data:    data,
	}

	// ResponseChan is buffered(1): at most one response is meaningful. If the
	// channel is already full, a previous response is pending — a second
	// Respond call is a programming error. Drop and log instead of spawning a
	// goroutine that blocks for 1s (which leaks under repeated misuse).
	select {
	case req.ResponseChan <- resp:
		// 成功发送响应
	default:
		bus.logf("WARN", "响应通道已满，丢弃重复响应: %s", req.Event.Type)
	}
}

// 完善请求-响应模式，增加异步回调支持

// EvtReqOpts 请求选项
type EvtReqOpts struct {
	Timeout    time.Duration                     // 超时时间
	Async      bool                              // 是否异步处理响应
	OnResponse func(response ResponseEvent)      // 异步响应回调
	OnTimeout  func(req RequestEvent)            // 超时回调
	OnError    func(req RequestEvent, err error) // 错误回调
	Context    context.Context                   // 请求上下文
}

// DefaultEvtReqOpts 默认请求选项
func DefaultEvtReqOpts() EvtReqOpts {
	return EvtReqOpts{
		Timeout:    5 * time.Second,
		Async:      false,
		OnResponse: nil,
		OnTimeout:  nil,
		OnError:    nil,
		Context:    context.Background(),
	}
}

// 修改 PublishRequestWithOptions 方法，使用提取的公共逻辑
func (bus *EventHub) RequestWithOptions(req RequestEvent, options EvtReqOpts) (ResponseEvent, error) {
	// 检查限流
	if !bus.checkRateLimit() {
		// bus.logf("DEBUG", "Request limited")
		if options.OnError != nil {
			options.OnError(req, fmt.Errorf("请求被限流"))
		}
		return ResponseEvent{
			Success: false,
			Error:   fmt.Errorf("请求被限流"),
		}, fmt.Errorf("请求被限流")
	}

	// 设置上下文（如果提供了自定义上下文）
	if options.Context != nil && options.Context != context.Background() {
		// 创建可取消的子上下文
		ctx, cancelFunc := context.WithCancel(options.Context)
		req.Context = ctx
		req.CancelFunc = cancelFunc
	}

	// 记录活跃请求
	bus.activeRequests.Store(req.RequestID, req)

	// 如果是异步模式且提供了回调函数
	if options.Async && options.OnResponse != nil {
		// 使用辅助方法提交异步处理任务
		bus.submitTask(func() {
			defer bus.activeRequests.Delete(req.RequestID)

			// 发布请求事件 - 将RequestEvent作为Data传递，以便订阅者可以访问RequestEvent
			eventToPublish := req.Event
			eventToPublish.Data = req
			bus.Publish(eventToPublish)

			// 使用公共处理逻辑
			bus.processRequestWithCallbacks(
				req,
				options.Timeout,
				options.OnResponse,
				options.OnTimeout,
				options.OnError,
			)
		})

		// 异步模式返回空响应和nil错误
		return ResponseEvent{}, nil
	}

	// 同步模式
	defer bus.activeRequests.Delete(req.RequestID)

	// 发布请求事件 - 将RequestEvent作为Data传递，以便订阅者可以访问RequestEvent
	eventToPublish := req.Event
	eventToPublish.Data = req
	bus.Publish(eventToPublish)

	// 使用通道和等待组来同步结果
	var result ResponseEvent
	var resultErr error
	done := make(chan struct{})

	bus.processRequestWithCallbacks(
		req,
		options.Timeout,
		func(resp ResponseEvent) {
			result = resp
			resultErr = nil
			close(done)
		},
		func(req RequestEvent) {
			if options.OnTimeout != nil {
				options.OnTimeout(req)
			}
			result = ResponseEvent{
				Success: false,
				Error:   fmt.Errorf("请求超时"),
			}
			resultErr = fmt.Errorf("请求超时")
			close(done)
		},
		func(req RequestEvent, err error) {
			if options.OnError != nil {
				options.OnError(req, err)
			}
			result = ResponseEvent{
				Success: false,
				Error:   err,
			}
			resultErr = err
			close(done)
		},
	)

	<-done
	return result, resultErr
}

// 添加取消请求的方法
func (bus *EventHub) CancelRequest(requestID string) bool {
	if value, ok := bus.activeRequests.Load(requestID); ok {
		req := value.(RequestEvent)
		if req.CancelFunc != nil {
			req.CancelFunc() // 调用取消函数
			return true
		}
	}
	return false
}

// 添加获取活跃请求数的方法
func (bus *EventHub) GetActiveRequestsCount() int {
	count := 0
	bus.activeRequests.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// incEventProcessed atomically increments the processed counter for an event type.
func (bus *EventHub) incEventProcessed(eventType string) {
	val, _ := bus.eventMetrics.processed.LoadOrStore(eventType, new(int64))
	atomic.AddInt64(val.(*int64), 1)
}

// incEventDropped atomically increments the dropped counter for an event type.
func (bus *EventHub) incEventDropped(eventType string) {
	val, _ := bus.eventMetrics.dropped.LoadOrStore(eventType, new(int64))
	atomic.AddInt64(val.(*int64), 1)
}

// GetEventMetrics returns per-event-type processed and dropped counters.
func (bus *EventHub) GetEventMetrics() map[string]map[string]int64 {
	result := make(map[string]map[string]int64)

	bus.eventMetrics.processed.Range(func(key, value interface{}) bool {
		eventType := key.(string)
		count := atomic.LoadInt64(value.(*int64))
		if result[eventType] == nil {
			result[eventType] = make(map[string]int64)
		}
		result[eventType]["processed"] = count
		return true
	})

	bus.eventMetrics.dropped.Range(func(key, value interface{}) bool {
		eventType := key.(string)
		count := atomic.LoadInt64(value.(*int64))
		if result[eventType] == nil {
			result[eventType] = make(map[string]int64)
		}
		result[eventType]["dropped"] = count
		return true
	})

	return result
}

// EventHubSnapshot captures a runtime snapshot of the EventHub state.
type EventHubSnapshot struct {
	Closed        bool
	Subscriptions int
	AsyncWorkers  int
	BatchWorkers  int
	Metrics       map[string]map[string]int64
	RequestStats  map[string]int64
}

// GetSnapshot returns a runtime snapshot of the EventHub for observability.
func (bus *EventHub) GetSnapshot() EventHubSnapshot {
	subCount := 0
	bus.subscriptions.Range(func(_, value interface{}) bool {
		subCount += len(value.([]Subscription))
		return true
	})

	asyncCount := 0
	bus.asyncWorkers.Range(func(_, value interface{}) bool {
		asyncCount += len(value.([]*asyncSubWorker))
		return true
	})

	batchCount := 0
	bus.batchWorkers.Range(func(_, _ interface{}) bool {
		batchCount++
		return true
	})

	return EventHubSnapshot{
		Closed:        bus.hubClosed.Load(),
		Subscriptions: subCount,
		AsyncWorkers:  asyncCount,
		BatchWorkers:  batchCount,
		Metrics:       bus.GetEventMetrics(),
		RequestStats:  bus.GetRequestStats(),
	}
}

// ============================================================================
// Middleware Support
// ============================================================================

// applyMiddlewares runs the middleware chain. Returns (event, true) if the
// event passed all middlewares, or (event, false) if a middleware rejected it.
func (bus *EventHub) applyMiddlewares(event Event) (Event, bool) {
	middlewares := bus.middlewares.Load().([]EventMiddleware)
	for _, mw := range middlewares {
		var err error
		event, err = mw(event)
		if err != nil {
			bus.logf("DEBUG", "事件 %s (ID: %s) 被中间件拒绝: %v", event.Type, event.EventID, err)
			return event, false
		}
	}
	return event, true
}

// Use adds a global middleware applied to all events before dispatch.
// Middlewares are applied in registration order.
func (bus *EventHub) Use(mw EventMiddleware) {
	old := bus.middlewares.Load().([]EventMiddleware)
	next := make([]EventMiddleware, len(old)+1)
	copy(next, old)
	next[len(old)] = mw
	bus.middlewares.Store(next)
}

// ============================================================================
// Async Subscription with Bounded Queue
// ============================================================================

// AsyncSubOption configures a dedicated async subscription.
type AsyncSubOption func(*asyncSubConfig)

type asyncSubConfig struct {
	concurrency int
	bufferSize  int
	policy      QueuePolicy
}

// WithAsyncConcurrency sets the number of goroutines processing events.
func WithAsyncConcurrency(n int) AsyncSubOption {
	return func(c *asyncSubConfig) { c.concurrency = n }
}

// WithAsyncBufferSize sets the bounded queue size.
func WithAsyncBufferSize(size int) AsyncSubOption {
	return func(c *asyncSubConfig) { c.bufferSize = size }
}

// WithQueuePolicy sets the backpressure policy when the queue is full.
func WithQueuePolicy(policy QueuePolicy) AsyncSubOption {
	return func(c *asyncSubConfig) { c.policy = policy }
}

type asyncSubWorker struct {
	id          string
	eventType   string
	queue       chan Event
	handler     EventHandler
	concurrency int
	policy      QueuePolicy
	hub         *EventHub
	stopCh      chan struct{}
	stopped     atomic.Bool
}

// SubscribeAsync creates a dedicated async worker with a bounded queue and
// configurable concurrency. Unlike Subscribe with Async=true (which uses a
// shared WorkerPool), each async subscription has its own queue and goroutine
// pool, providing per-subscription backpressure control.
// The returned ID can be used with Unsubscribe or UnsubscribeAsync.
func (bus *EventHub) SubscribeAsync(eventType string, handler EventHandler, opts ...AsyncSubOption) string {
	if eventType == "" || handler == nil {
		return ""
	}
	if bus.hubClosed.Load() {
		bus.logf("WARN", "EventHub已关闭，无法订阅异步事件 %s", eventType)
		return ""
	}

	cfg := asyncSubConfig{
		concurrency: 1,
		bufferSize:  256,
		policy:      DropOnFull,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.concurrency < 1 {
		cfg.concurrency = 1
	}

	id := bus.generateID(eventType)
	w := &asyncSubWorker{
		id:          id,
		eventType:   eventType,
		queue:       make(chan Event, cfg.bufferSize),
		handler:     handler,
		concurrency: cfg.concurrency,
		policy:      cfg.policy,
		hub:         bus,
		stopCh:      make(chan struct{}),
	}

	// Register worker (copy-on-write append)
	for {
		val, ok := bus.asyncWorkers.Load(eventType)
		if !ok {
			if _, loaded := bus.asyncWorkers.LoadOrStore(eventType, []*asyncSubWorker{w}); loaded {
				continue
			}
			break
		}
		existing := val.([]*asyncSubWorker)
		next := make([]*asyncSubWorker, len(existing)+1)
		copy(next, existing)
		next[len(existing)] = w
		if bus.asyncWorkers.CompareAndSwap(eventType, existing, next) {
			break
		}
	}

	// Start goroutines
	bus.hubWg.Add(cfg.concurrency)
	for i := 0; i < cfg.concurrency; i++ {
		go w.run()
	}

	return id
}

func (w *asyncSubWorker) send(event Event) {
	if w.stopped.Load() {
		return
	}
	if w.policy == BlockOnFull {
		select {
		case w.queue <- event:
		case <-w.stopCh:
		case <-w.hub.hubCtx.Done():
		}
	} else {
		select {
		case w.queue <- event:
		default:
			w.hub.incEventDropped(event.Type)
			w.hub.logf("WARN", "异步队列已满，丢弃事件 %s", event.Type)
		}
	}
}

func (w *asyncSubWorker) run() {
	defer w.hub.hubWg.Done()
	for {
		select {
		case <-w.stopCh:
			w.drain()
			return
		case <-w.hub.hubCtx.Done():
			w.drain()
			return
		case event := <-w.queue:
			w.execute(event)
		}
	}
}

func (w *asyncSubWorker) drain() {
	for {
		select {
		case event := <-w.queue:
			w.execute(event)
		default:
			return
		}
	}
}

func (w *asyncSubWorker) execute(event Event) {
	defer func() {
		if r := recover(); r != nil {
			w.hub.logf("ERROR", "异步处理事件 %s 时发生panic: %v", event.Type, r)
		}
	}()
	if err := w.handler(event); err != nil {
		w.hub.logf("ERROR", "异步处理事件 %s 失败: %v", event.Type, err)
	}
}

func (w *asyncSubWorker) stop() {
	if !w.stopped.CompareAndSwap(false, true) {
		return
	}
	close(w.stopCh)
}

// UnsubscribeAsync removes an async subscription by ID.
func (bus *EventHub) UnsubscribeAsync(id string) bool {
	var found bool
	bus.asyncWorkers.Range(func(key, value interface{}) bool {
		workers := value.([]*asyncSubWorker)
		for i, w := range workers {
			if w.id == id {
				w.stop()
				next := make([]*asyncSubWorker, len(workers)-1)
				copy(next, workers[:i])
				copy(next[i:], workers[i+1:])
				if len(next) == 0 {
					bus.asyncWorkers.Delete(key)
				} else {
					bus.asyncWorkers.Store(key, next)
				}
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// ============================================================================
// Batch Subscription
// ============================================================================

// BatchSubOption configures a batch subscription.
type BatchSubOption func(*batchSubConfig)

type batchSubConfig struct {
	batchSize     int
	flushInterval time.Duration
	maxRetry      int
}

// WithBatchSize sets the maximum batch size before a flush is triggered.
func WithBatchSize(size int) BatchSubOption {
	return func(c *batchSubConfig) { c.batchSize = size }
}

// WithFlushInterval sets the maximum time between flushes.
func WithFlushInterval(d time.Duration) BatchSubOption {
	return func(c *batchSubConfig) { c.flushInterval = d }
}

// WithBatchMaxRetry sets the number of retry attempts for a failed batch.
func WithBatchMaxRetry(n int) BatchSubOption {
	return func(c *batchSubConfig) { c.maxRetry = n }
}

type batchSubWorker struct {
	eventType string
	queue     chan Event
	handler   EventBatchHandler
	config    batchSubConfig
	hub       *EventHub
	stopCh    chan struct{}
	done      chan struct{}
	stopped   atomic.Bool
}

// SubscribeBatch registers a batch handler for an event type. Events are
// accumulated and flushed when the batch reaches batchSize or the flush
// interval elapses. Only one batch handler per event type is allowed;
// calling SubscribeBatch again for the same type replaces the previous one.
func (bus *EventHub) SubscribeBatch(eventType string, handler EventBatchHandler, opts ...BatchSubOption) string {
	if eventType == "" || handler == nil {
		return ""
	}
	if bus.hubClosed.Load() {
		bus.logf("WARN", "EventHub已关闭，无法订阅批量事件 %s", eventType)
		return ""
	}

	cfg := batchSubConfig{
		batchSize:     100,
		flushInterval: 200 * time.Millisecond,
		maxRetry:      0,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.batchSize < 1 {
		cfg.batchSize = 100
	}
	if cfg.flushInterval <= 0 {
		cfg.flushInterval = 200 * time.Millisecond
	}

	// Stop existing worker for this event type
	if val, ok := bus.batchWorkers.Load(eventType); ok {
		val.(*batchSubWorker).stop()
	}

	id := bus.generateID(eventType)
	w := &batchSubWorker{
		eventType: eventType,
		queue:     make(chan Event, 1024),
		handler:   handler,
		config:    cfg,
		hub:       bus,
		stopCh:    make(chan struct{}),
		done:      make(chan struct{}),
	}

	bus.batchWorkers.Store(eventType, w)

	bus.hubWg.Add(1)
	go w.run()

	return id
}

func (w *batchSubWorker) send(event Event) {
	if w.stopped.Load() {
		return
	}
	select {
	case w.queue <- event:
	default:
		w.hub.incEventDropped(event.Type)
		w.hub.logf("WARN", "批量队列已满，丢弃事件 %s", event.Type)
	}
}

func (w *batchSubWorker) run() {
	defer func() {
		close(w.done)
		w.hub.hubWg.Done()
	}()

	buffer := make([]Event, 0, w.config.batchSize)
	ticker := time.NewTicker(w.config.flushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(buffer) == 0 {
			return
		}
		batch := make([]Event, len(buffer))
		copy(batch, buffer)
		buffer = buffer[:0]

		var lastErr error
		for i := 0; i <= w.config.maxRetry; i++ {
			err := w.safeCall(batch)
			if err == nil {
				return
			}
			lastErr = err
			if i < w.config.maxRetry {
				time.Sleep(time.Duration(i+1) * 100 * time.Millisecond)
			}
		}
		w.hub.logf("ERROR", "批量处理事件 %s 失败 (%d次重试): %v", w.eventType, w.config.maxRetry, lastErr)
	}

	for {
		select {
		case <-w.stopCh:
			w.drainAndFlush(&buffer, flush)
			return
		case <-w.hub.hubCtx.Done():
			w.drainAndFlush(&buffer, flush)
			return
		case event := <-w.queue:
			buffer = append(buffer, event)
			if len(buffer) >= w.config.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// drainAndFlush drains the queue and flushes remaining events.
func (w *batchSubWorker) drainAndFlush(buffer *[]Event, flush func()) {
	for len(w.queue) > 0 {
		*buffer = append(*buffer, <-w.queue)
		if len(*buffer) >= w.config.batchSize {
			flush()
		}
	}
	flush()
}

func (w *batchSubWorker) safeCall(batch []Event) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("batch handler panic: %v", r)
		}
	}()
	return w.handler(batch)
}

func (w *batchSubWorker) stop() {
	if !w.stopped.CompareAndSwap(false, true) {
		return
	}
	close(w.stopCh)
	<-w.done
}

// UnsubscribeBatch removes the batch handler for an event type.
func (bus *EventHub) UnsubscribeBatch(eventType string) bool {
	val, ok := bus.batchWorkers.LoadAndDelete(eventType)
	if !ok {
		return false
	}
	val.(*batchSubWorker).stop()
	return true
}
