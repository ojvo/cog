package timer

import (
	"fmt"
	"math"
	"os"
	"sync"
	"time"
)

// @author qiang.ou<qingqianludao@gmail.com>

// Job 延时任务回调函数
type Job[V any] func(V)

// TimeWheel 时间轮
type TimeWheel[K comparable, V any] struct {
	interval   time.Duration // 指针每隔多久往前移动一格
	ticker     *time.Ticker
	slots      []*TaskList[K, V] // 时间轮槽
	timer      map[K]*Task[K, V]
	tick       int64             // 当前时间滴答数
	slotNum    int               // 槽数量
	slotMask   int64             // 槽数量掩码 (slotNum - 1)
	slotBits   int               // 槽数量位数 (2^slotBits = slotNum)
	startTime  time.Time         // 启动时间，用于校准Tick
	job        Job[V]            // 定时器回调函数
	stopChannel chan struct{}     // 停止定时器channel
	stopOnce    sync.Once         // 保证只关闭一次
	startOnce   sync.Once         // 保证只启动一次
	mu          sync.Mutex        // 互斥锁保护内部数据
	taskPool    sync.Pool         // 任务对象池
}

// Task 延时任务 (Intrusive List Node)
type Task[K comparable, V any] struct {
	next, prev *Task[K, V]     // 链表指针
	list       *TaskList[K, V] // 所属链表，用于快速删除

	expire int64 // 任务到期时间(Tick)
	key    K
	data   V
}

// TaskList 双向链表
type TaskList[K comparable, V any] struct {
	root Task[K, V] // 哨兵节点
}

func NewTaskList[K comparable, V any]() *TaskList[K, V] {
	l := &TaskList[K, V]{}
	l.root.next = &l.root
	l.root.prev = &l.root
	return l
}

func (l *TaskList[K, V]) PushBack(t *Task[K, V]) {
	t.next = &l.root
	t.prev = l.root.prev
	t.prev.next = t
	t.next.prev = t
	t.list = l
}

func (l *TaskList[K, V]) Remove(t *Task[K, V]) {
	if t.list != l {
		return
	}
	t.prev.next = t.next
	t.next.prev = t.prev
	t.next = nil
	t.prev = nil
	t.list = nil
}

func (l *TaskList[K, V]) Front() *Task[K, V] {
	if l.root.next == &l.root {
		return nil
	}
	return l.root.next
}

// New 创建时间轮
func New[K comparable, V any](interval time.Duration, slotNum int, job Job[V]) *TimeWheel[K, V] {
	if interval <= 0 || slotNum <= 0 || job == nil {
		return nil
	}

	// Round up slotNum to next power of 2 for bitwise optimization
	realSlotNum := 1
	slotBits := 0
	for realSlotNum < slotNum {
		realSlotNum <<= 1
		slotBits++
	}

	tw := &TimeWheel[K, V]{
		interval:    interval,
		slots:       make([]*TaskList[K, V], realSlotNum),
		timer:       make(map[K]*Task[K, V]),
		tick:        0,
		job:         job,
		slotNum:     realSlotNum,
		slotMask:    int64(realSlotNum - 1),
		slotBits:    slotBits,
		stopChannel: make(chan struct{}),
		taskPool: sync.Pool{
			New: func() interface{} {
				return &Task[K, V]{}
			},
		},
	}

	tw.initSlots()

	return tw
}

// 初始化槽
func (tw *TimeWheel[K, V]) initSlots() {
	for i := 0; i < tw.slotNum; i++ {
		tw.slots[i] = NewTaskList[K, V]()
	}
}

// Start 启动时间轮
func (tw *TimeWheel[K, V]) Start() {
	tw.startOnce.Do(func() {
		tw.startTime = time.Now()
		tw.ticker = time.NewTicker(tw.interval)
		go tw.start()
	})
}

// Stop 停止时间轮
func (tw *TimeWheel[K, V]) Stop() {
	tw.stopOnce.Do(func() {
		close(tw.stopChannel)
		
		// Clean up resources
		tw.mu.Lock()
		defer tw.mu.Unlock()
		
		// Clear all tasks in map and slots
		for key, task := range tw.timer {
			delete(tw.timer, key)
			if task.list != nil {
				task.list.Remove(task)
			}
			tw.putTask(task)
		}
	})
}

// AddTimer 添加定时器 key为定时器唯一标识
func (tw *TimeWheel[K, V]) AddTimer(delay time.Duration, key K, data V) {
	if delay <= 0 {
		delay = tw.interval
	}

	tw.mu.Lock()
	defer tw.mu.Unlock()

	// 检查是否已停止
	select {
	case <-tw.stopChannel:
		return
	default:
	}

	// Upsert: 如果已存在，先删除
	if oldTask, ok := tw.timer[key]; ok {
		oldTask.list.Remove(oldTask)
		tw.putTask(oldTask)
	}

	pos, expire := tw.getPositionAndExpire(delay)
	task := tw.getTask()
	task.expire = expire
	task.key = key
	task.data = data

	tw.slots[pos].PushBack(task)
	tw.timer[key] = task
}

// RemoveTimer 删除定时器
func (tw *TimeWheel[K, V]) RemoveTimer(key K) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if task, ok := tw.timer[key]; ok {
		task.list.Remove(task)
		delete(tw.timer, key)
		tw.putTask(task)
	}
}

func (tw *TimeWheel[K, V]) start() {
	defer tw.ticker.Stop()
	for {
		select {
		case now := <-tw.ticker.C:
			tw.tickHandler(now)
		case <-tw.stopChannel:
			return
		}
	}
}

func (tw *TimeWheel[K, V]) tickHandler(now time.Time) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	// Calculate expected ticks based on elapsed time to handle missed ticks/lag
	elapsed := now.Sub(tw.startTime)
	expectedTick := int64(elapsed / tw.interval)
	
	// Catch up if we are behind
	// Optimization: If the lag is larger than the wheel size (slotNum),
	// we can skip the incremental tick processing and just scan all slots once.
	// This avoids O(N) loop where N is the number of missed ticks (which can be huge after system sleep).
	if expectedTick-tw.tick >= int64(tw.slotNum) {
		tw.tick = expectedTick
		for i := 0; i < tw.slotNum; i++ {
			tw.scanAndRunTask(tw.slots[i])
		}
		return
	}

	for tw.tick < expectedTick {
		tw.tick++
		currentPos := int(tw.tick & tw.slotMask)
		l := tw.slots[currentPos]
		tw.scanAndRunTask(l)
	}
}

// 扫描链表中过期定时器, 并执行回调函数
func (tw *TimeWheel[K, V]) scanAndRunTask(l *TaskList[K, V]) {
	for t := l.root.next; t != &l.root; {
		next := t.next // 保存下一个节点，因为t可能会被移除

		if t.expire > tw.tick {
			t = next
			continue
		}

		go func(d V) {
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "TimeWheel job panic: %v\n", r)
				}
			}()
			tw.job(d)
		}(t.data)

		l.Remove(t)
		delete(tw.timer, t.key)
		tw.putTask(t)

		t = next
	}
}

func (tw *TimeWheel[K, V]) getPositionAndExpire(d time.Duration) (pos int, expire int64) {
	delay := int64(d)
	interval := int64(tw.interval)
	// Round up to the next tick: a delay of exactly one interval should fire
	// after one tick, not two. Previously `delay/interval + 1` caused an
	// off-by-one (e.g. delay=interval waited 2 intervals). Ceiling division
	// guarantees at least one tick of delay while avoiding the extra +1.
	steps := (delay + interval - 1) / interval
	if steps < 1 {
		steps = 1
	}

	// Check for overflow: if tw.tick + steps > MaxInt64
	if steps > math.MaxInt64-tw.tick {
		expire = math.MaxInt64
	} else {
		expire = tw.tick + steps
	}

	pos = int(expire & tw.slotMask)
	return
}

func (tw *TimeWheel[K, V]) getTask() *Task[K, V] {
	return tw.taskPool.Get().(*Task[K, V])
}

func (tw *TimeWheel[K, V]) putTask(t *Task[K, V]) {
	// Reset fields to avoid memory leaks or dirty data
	t.next = nil
	t.prev = nil
	t.list = nil
	var zeroK K
	t.key = zeroK
	var zeroV V
	t.data = zeroV
	tw.taskPool.Put(t)
}
