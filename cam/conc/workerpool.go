package conc

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Task 表示要执行的任务
type Task func()

// WorkerPoolStats is a runtime statistics snapshot of a WorkerPool.
type WorkerPoolStats struct {
	Workers        int
	Running        bool
	QueueSize      uint32
	QueueUsed      uint32
	TasksTotal     int64
	TasksCompleted int64
}

// WorkerPool 是一个简单的工作池实现
type WorkerPool struct {
	// 任务队列
	taskQueue *QueueLf
	// 工作线程数量
	workers int
	// 控制信号
	ctx    context.Context
	cancel context.CancelFunc
	// 状态
	running int32
	// 等待所有工作线程退出
	wg sync.WaitGroup
	// 统计信息
	taskCount     int64
	taskCompleted int64
}

// NewWorkerPool 创建一个新的工作池
// workers: 工作线程数量，如果为0则使用CPU核心数
// queueSize: 任务队列大小
func NewWorkerPool(workers int, queueSize int) *WorkerPool {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if queueSize <= 0 {
		queueSize = 1024
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &WorkerPool{
		taskQueue: NewQueueLf(uint32(queueSize)),
		workers:   workers,
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Start 启动工作池
func (p *WorkerPool) Start() {
	if !atomic.CompareAndSwapInt32(&p.running, 0, 1) {
		return // 已经在运行
	}

	p.wg.Add(p.workers)
	for i := 0; i < p.workers; i++ {
		go p.worker()
	}
}

// Stop 停止工作池
// 先关闭队列，让 worker 排空剩余任务后退出，最后取消 context
func (p *WorkerPool) Stop() {
	if !atomic.CompareAndSwapInt32(&p.running, 1, 0) {
		return // 已经停止
	}

	p.taskQueue.Close() // 先关闭队列，让 worker 排空
	p.wg.Wait()
	p.cancel() // 最后取消 context 释放资源
}

// Submit 提交一个任务到工作池
// 如果队列已满，会阻塞直到有空间或工作池关闭
func (p *WorkerPool) Submit(task Task) bool {
	if atomic.LoadInt32(&p.running) == 0 {
		return false
	}

	ok, _ := p.taskQueue.Put(task)
	if ok {
		atomic.AddInt64(&p.taskCount, 1)
	}
	return ok
}

// SubmitWithTimeout 提交一个任务到工作池，带超时控制
// 如果在超时时间内无法提交，返回false
func (p *WorkerPool) SubmitWithTimeout(task Task, timeout time.Duration) bool {
	if atomic.LoadInt32(&p.running) == 0 {
		return false
	}

	deadline := time.Now().Add(timeout)
	for {
		if ok, _ := p.taskQueue.TryPut(task); ok {
			atomic.AddInt64(&p.taskCount, 1)
			return true
		}

		if time.Now().After(deadline) {
			return false
		}

		time.Sleep(time.Millisecond)
	}
}

// SubmitWait 提交一个任务并阻塞等待其执行完成。
// 返回 false 表示池未运行或任务未能入队。
//
// 注意：从同一个池的 worker 内调用 SubmitWait 会死锁（worker 阻塞等待自己）。
// 此类场景应使用 Submit 异步派发。
//
// 行为保证：一旦 Submit 返回 true，任务必定在 Stop 返回前被执行
// （Stop 会排空队列后才会终止 worker）。
func (p *WorkerPool) SubmitWait(task Task) bool {
	if atomic.LoadInt32(&p.running) == 0 {
		return false
	}
	done := make(chan struct{})
	if !p.Submit(func() {
		defer close(done)
		task()
	}) {
		return false
	}
	<-done
	return true
}

// worker 工作线程函数
func (p *WorkerPool) worker() {
	defer p.wg.Done()

	for {
		// 尝试获取任务
		task, ok, _ := p.taskQueue.Get()
		if !ok {
			// 队列已关闭且为空
			return
		}

		// 执行任务
		if fn, ok := task.(Task); ok {
			func() {
				defer func() {
					if r := recover(); r != nil {
						fmt.Printf("WorkerPool: 任务执行panic: %v\n", r)
					}
				}()
				fn()
			}()
			atomic.AddInt64(&p.taskCompleted, 1)
		}
	}
}

// Stats 返回工作池的统计信息
func (p *WorkerPool) Stats() WorkerPoolStats {
	return WorkerPoolStats{
		Workers:        p.workers,
		Running:        atomic.LoadInt32(&p.running) == 1,
		QueueSize:      p.taskQueue.Capacity(),
		QueueUsed:      p.taskQueue.Quantity(),
		TasksTotal:     atomic.LoadInt64(&p.taskCount),
		TasksCompleted: atomic.LoadInt64(&p.taskCompleted),
	}
}

// 全局默认工作池
var (
	defaultPool     *WorkerPool
	defaultPoolOnce sync.Once
)

// GetDefaultPool 获取默认工作池
func GetDefaultPool() *WorkerPool {
	defaultPoolOnce.Do(func() {
		workers := runtime.NumCPU() * 2
		queueSize := 10000
		defaultPool = NewWorkerPool(workers, queueSize)
		defaultPool.Start()
	})
	return defaultPool
}

// Submit 提交任务到默认工作池
func Submit(task Task) bool {
	return GetDefaultPool().Submit(task)
}

// SubmitWithTimeout 提交任务到默认工作池，带超时控制
func SubmitWithTimeout(task Task, timeout time.Duration) bool {
	return GetDefaultPool().SubmitWithTimeout(task, timeout)
}

// SubmitWait 提交任务到默认工作池并阻塞等待其执行完成
func SubmitWait(task Task) bool {
	return GetDefaultPool().SubmitWait(task)
}
