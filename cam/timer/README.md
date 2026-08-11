# lpr.ws/cor/timer

`lpr.ws/cor/timer` 是一个高性能、类型安全（泛型）的 Go 语言定时器工具库。它专为高并发场景设计，提供了比标准库 `time.AfterFunc` 更高效的海量定时任务管理能力。

## 核心组件

### 1. TimeWheel[K, V] (通用时间轮)

基于时间轮算法实现的延迟任务调度器，支持泛型。

#### 核心特性
- **泛型支持**：`TimeWheel[K, V]` 支持自定义 Key 和 Data 类型，编译期保证类型安全，无运行时反射开销。
- **高性能**：
  - 添加/删除任务复杂度为 O(1)。
  - 使用 `sync.Pool` 复用 Task 对象，实现热路径零内存分配 (Zero Allocation)。
  - 内部使用位运算优化槽位计算。
  - 引入 `math/rand/v2` 优化并发随机性能。
- **健壮性**：
  - **Tick 追赶机制**：自动处理系统休眠或卡顿导致的 Ticker 滞后，针对长时滞后有批量扫描优化 (O(Slots))。
  - **Panic 恢复**：任务回调在独立 Goroutine 中执行并受 Recover 保护。
  - **并发安全**：全链路线程安全。

#### 适用场景
- **海量连接心跳检测**：如 IM、游戏网关中管理数万个连接的超时断开。
- **请求超时控制**：RPC 框架中的请求超时管理。
- **高吞吐延迟任务**：需要频繁创建/销毁定时器的场景。

#### 使用示例

```go
package main

import (
	"fmt"
	"time"
	"lpr.ws/cor/timer"
)

type MyContext struct {
	UserID int
}

func main() {
	// 1. 创建时间轮
	// interval: 时间精度 (如 100ms)
	// slotNum: 槽位数量 (内部会自动向上取整为 2 的幂)
	// job: 回调函数
	tw := timer.New[string, *MyContext](100*time.Millisecond, 60, func(ctx *MyContext) {
		fmt.Printf("User %d timeout\n", ctx.UserID)
	})

	// 2. 启动
	tw.Start()
	defer tw.Stop() // 务必在退出时停止，释放资源

	// 3. 添加定时任务 (Key="u1001")
	// 如果 Key 已存在，会更新过期时间 (Upsert)
	tw.AddTimer(5*time.Second, "u1001", &MyContext{UserID: 1001})

	// 4. 删除任务
	tw.RemoveTimer("u1001")

	// 5. 0 延时会自动在下一 Tick 执行
	tw.AddTimer(0, "now", &MyContext{UserID: 0})
	
	select {}
}
```

### 2. ChainTicker (链式 Ticker)

允许多个消费者共享同一个 Ticker 源的广播机制。

#### 适用场景
- 多个模块需要基于完全相同的时间节拍进行同步操作。

#### 使用示例
```go
ticker := timer.NewChanTicker(time.Second)
defer ticker.Stop()

ch1 := make(chan time.Time, 1)
ch2 := make(chan time.Time, 1)

// 注册监听
ticker.Add(ch1)
ticker.Add(ch2)

// 移除监听
ticker.Remove(ch1)
```

### 3. 工具函数

- **SleepSecond(st, et int)**: 随机休眠 `[st, et)` 秒。
- **SleepMs(st, et int)**: 随机休眠 `[st, et)` 毫秒。
- **CallerPerHour(ctx, f)**: 在每小时整点执行函数 `f` (包含启动时立即执行一次)。

## 最佳实践与避坑指南 (Caveats)

1.  **Key 的唯一性**
    - `TimeWheel` 是基于 Map 索引任务的。调用 `AddTimer` 时，如果 `Key` 已经存在，**会先删除旧任务，再添加新任务**（重置过期时间）。
    - 适用：心跳保活（每次收到包就 Update 一下）。
    - 不适用：需要同一 Key 共存多个任务的场景（建议在 Key 中拼接 UUID 或使用不同 Key）。

2.  **精度与性能权衡**
    - `interval` 决定了时间轮的精度。
    - 建议值：`10ms` - `1s`。
    - 不要设置过小（如 `1ns`、`1us`），会导致 CPU 空转处理 Tick。
    - 也不要设置过大，否则任务调度的延迟误差会变大。

3.  **SlotNum (槽位数) 选择**
    - 影响时间轮一轮的总时间 (`interval * slotNum`)。
    - 尽量让大多数任务的延时落在 `interval * slotNum` 范围内，这样任务分布更均匀，哈希冲突（同一个槽内链表过长）概率更低。
    - 内部会自动将 slotNum 调整为 2 的 n 次幂以便进行位运算优化。

4.  **回调函数的执行**
    - `job` 回调是在**新的 Goroutine** 中异步执行的。
    - 优点：单个任务阻塞或 Panic 不会影响时间轮调度。
    - 注意：如果任务执行非常频繁且耗时，可能会产生大量 Goroutine，建议在回调中自行控制并发或使用 Worker Pool（如果业务逻辑极重）。

5.  **资源释放**
    - 创建 `TimeWheel` 或 `ChainTicker` 后，不再使用时请务必调用 `Stop()`，否则内部的 `time.Ticker` 和 Goroutine 可能会泄漏。

6.  **系统休眠/唤醒**
    - 本库已针对系统休眠（如笔记本合盖）做了优化（Bulk Catch-up）。
    - 唤醒时，所有在休眠期间过期的任务会**立即被调度执行**。请确保你的业务逻辑能处理这种瞬间流量爆发。
