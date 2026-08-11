package conc

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type queCache struct {
	putNo uint32
	getNo uint32
	value interface{}
	_     [64 - 3*8]byte // Padding to avoid false sharing (assuming 64-byte cache line)
}

// lock free queue
type QueueLf struct {
	capacity uint32
	capMod   uint32
	putPos   uint32
	getPos   uint32
	cache    []queCache
	cond     *sync.Cond  // Condition variable for signaling
	closed   atomic.Bool // Flag to indicate if the queue is closed
}

func NewQueueLf(capacity uint32) *QueueLf {
	if capacity < 2 {
		capacity = 2
	}
	q := new(QueueLf)
	q.capacity = minQuantity(capacity)
	q.capMod = q.capacity - 1
	q.putPos = 0
	q.getPos = 0
	q.cache = make([]queCache, q.capacity)
	for i := range q.cache {
		cache := &q.cache[i]
		cache.getNo = uint32(i)
		cache.putNo = uint32(i)
	}
	cache := &q.cache[0]
	cache.getNo = q.capacity
	cache.putNo = q.capacity
	q.cond = sync.NewCond(&sync.Mutex{}) // Initialize condition variable
	q.closed.Store(false)
	return q
}

func (q *QueueLf) String() string {
	getPos := atomic.LoadUint32(&q.getPos)
	putPos := atomic.LoadUint32(&q.putPos)
	return fmt.Sprintf("Queue{capacity: %v, capMod: %v, putPos: %v, getPos: %v, closed: %v}",
		q.capacity, q.capMod, putPos, getPos, q.closed.Load())
}

func (q *QueueLf) Capacity() uint32 {
	return q.capacity
}

func (q *QueueLf) Quantity() uint32 {
	var getPos, putPos uint32

	// Load getPos and putPos atomically to get a consistent snapshot
	getPos = atomic.LoadUint32(&q.getPos)
	putPos = atomic.LoadUint32(&q.putPos)

	if putPos >= getPos {
		return putPos - getPos
	}
	return q.capacity - (getPos - putPos)
}

// Close the queue to prevent further puts. Get operations can still drain the queue.
func (q *QueueLf) Close() {
	q.cond.L.Lock()
	q.closed.Store(true)
	q.cond.Broadcast() // Wake up all waiting goroutines
	q.cond.L.Unlock()
}

const maxSpin = 16 // 最大自旋次数

// put queue functions
func (q *QueueLf) Put(val interface{}) (ok bool, quantity uint32) {
	var putPos, putPosNew uint32
	var cache *queCache

	for { // Retry loop
		if q.closed.Load() {
			return false, q.Quantity()
		}

		putPos = atomic.LoadUint32(&q.putPos)
		quantity = q.Quantity()
		if quantity >= q.capacity-1 {
			q.cond.L.Lock()
			// Double check inside lock
			if q.Quantity() < q.capacity-1 {
				q.cond.L.Unlock()
				continue
			}
			if q.closed.Load() {
				q.cond.L.Unlock()
				return false, q.Quantity()
			}
			q.cond.Wait() // Wait for space to become available
			q.cond.L.Unlock()
			continue // Recheck after waking up
		}

		putPosNew = putPos + 1
		if atomic.CompareAndSwapUint32(&q.putPos, putPos, putPosNew) {
			break // CAS success, exit retry loop
		}

		runtime.Gosched() // Only Gosched if CAS fails multiple times
		//time.Sleep(time.Microsecond) // Add a small delay before retrying
	}

	cache = &q.cache[putPosNew&q.capMod]

	// Once putPos is claimed via CAS, the slot MUST be filled regardless of
	// closed state. Returning early without filling would leave putPos advanced
	// but the slot empty, causing getters to spin forever waiting for data
	// that will never arrive. The closed check at the top of Put is sufficient
	// to prevent new puts from starting.
	spin := 0
	for {
		getNo := atomic.LoadUint32(&cache.getNo)
		putNo := atomic.LoadUint32(&cache.putNo)
		if putPosNew == putNo && getNo == putNo {
			cache.value = val
			atomic.AddUint32(&cache.putNo, q.capacity)
			q.cond.L.Lock()
			q.cond.Signal() // Signal a waiting getter
			q.cond.L.Unlock()
			return true, q.Quantity()
		} else {
			if spin < maxSpin {
				spin++
				runtime.Gosched() // 放弃时间片，让其他goroutine执行
			} else {
				// 如果自旋次数过多，强制休眠以减少CPU占用
				time.Sleep(time.Microsecond)
			}
		}
	}
}

// TryPut attempts to put a value into the queue without blocking.
// Returns true if successful, false if the queue is full or closed.
func (q *QueueLf) TryPut(val interface{}) (ok bool, quantity uint32) {
	if q.closed.Load() {
		return false, q.Quantity()
	}

	putPos := atomic.LoadUint32(&q.putPos)
	quantity = q.Quantity()

	if quantity >= q.capacity-1 {
		return false, quantity
	}

	putPosNew := putPos + 1
	if !atomic.CompareAndSwapUint32(&q.putPos, putPos, putPosNew) {
		return false, quantity
	}

	cache := &q.cache[putPosNew&q.capMod]
	// Once putPos is claimed via CAS, the slot MUST be filled (see Put comment).
	spin := 0
	for {
		getNo := atomic.LoadUint32(&cache.getNo)
		putNo := atomic.LoadUint32(&cache.putNo)
		if putPosNew == putNo && getNo == putNo {
			cache.value = val
			atomic.AddUint32(&cache.putNo, q.capacity)
			q.cond.L.Lock()
			q.cond.Signal()
			q.cond.L.Unlock()
			return true, q.Quantity()
		} else {
			if spin < maxSpin {
				spin++
				runtime.Gosched()
			} else {
				time.Sleep(time.Microsecond)
			}
		}
	}
}

// puts queue functions
func (q *QueueLf) Puts(values []interface{}) (puts, quantity uint32) {
	var putPos, putPosNew, putCnt uint32

	for {
		if q.closed.Load() {
			return 0, q.Quantity()
		}

		putPos = atomic.LoadUint32(&q.putPos)
		quantity = q.Quantity()

		available := q.capacity - 1 - quantity
		if available == 0 {
			q.cond.L.Lock()
			// Double check inside lock
			if q.capacity-1-q.Quantity() > 0 {
				q.cond.L.Unlock()
				continue
			}
			if q.closed.Load() {
				q.cond.L.Unlock()
				return 0, q.Quantity()
			}
			q.cond.Wait() // Wait for space to become available
			q.cond.L.Unlock()
			continue // Recheck after waking up
		}

		if capPuts := uint32(len(values)); available >= capPuts {
			putCnt = capPuts
		} else {
			putCnt = available
		}

		putPosNew = putPos + putCnt

		if atomic.CompareAndSwapUint32(&q.putPos, putPos, putPosNew) {
			break
		}

		runtime.Gosched()
		//time.Sleep(time.Microsecond)
	}

	for posNew, v := putPos+1, uint32(0); v < putCnt; posNew, v = posNew+1, v+1 {
		var cache *queCache = &q.cache[posNew&q.capMod]
		spin := 0
		for {
			getNo := atomic.LoadUint32(&cache.getNo)
			putNo := atomic.LoadUint32(&cache.putNo)
			if posNew == putNo && getNo == putNo {
				cache.value = values[v]
				atomic.AddUint32(&cache.putNo, q.capacity)
				break
			} else {
				if spin < maxSpin {
					spin++
					runtime.Gosched() // 放弃时间片，让其他goroutine执行
				} else {
					time.Sleep(time.Microsecond)
				}
			}
		}
	}

	q.cond.L.Lock()
	q.cond.Broadcast() // Signal waiting getters
	q.cond.L.Unlock()
	return putCnt, q.Quantity()
}

// get queue functions
func (q *QueueLf) Get() (val interface{}, ok bool, quantity uint32) {
	var getPos, getPosNew uint32
	var cache *queCache

	for {
		if q.closed.Load() && q.Quantity() == 0 {
			return nil, false, q.Quantity()
		}

		getPos = atomic.LoadUint32(&q.getPos)
		quantity = q.Quantity()

		if quantity < 1 {
			q.cond.L.Lock()
			// Double check inside lock
			if q.Quantity() >= 1 {
				q.cond.L.Unlock()
				continue
			}
			if q.closed.Load() && q.Quantity() == 0 {
				q.cond.L.Unlock()
				return nil, false, q.Quantity()
			}
			q.cond.Wait() // Wait for items to become available
			q.cond.L.Unlock()
			continue
		}

		getPosNew = getPos + 1
		if atomic.CompareAndSwapUint32(&q.getPos, getPos, getPosNew) {
			break
		}

		runtime.Gosched()
		//time.Sleep(time.Microsecond)
	}

	cache = &q.cache[getPosNew&q.capMod]
	spin := 0
	for {
		getNo := atomic.LoadUint32(&cache.getNo)
		putNo := atomic.LoadUint32(&cache.putNo)
		if getPosNew == getNo && getNo == putNo-q.capacity {
			val = cache.value
			cache.value = nil
			atomic.AddUint32(&cache.getNo, q.capacity)
			q.cond.L.Lock()
			q.cond.Signal() // Signal a waiting putter
			q.cond.L.Unlock()
			return val, true, q.Quantity()
		} else {
			if spin < maxSpin {
				spin++
				runtime.Gosched() // 放弃时间片，让其他goroutine执行
			} else {
				time.Sleep(time.Microsecond)
			}
		}
	}
}

// gets queue functions
func (q *QueueLf) Gets(values []interface{}) (gets, quantity uint32) {
	var getPos, getPosNew, getCnt uint32

	for {
		if q.closed.Load() && q.Quantity() == 0 {
			return 0, q.Quantity()
		}

		getPos = atomic.LoadUint32(&q.getPos)
		quantity = q.Quantity()

		if quantity < 1 {
			q.cond.L.Lock()
			// Double check inside lock
			if q.Quantity() >= 1 {
				q.cond.L.Unlock()
				continue
			}
			if q.closed.Load() && q.Quantity() == 0 {
				q.cond.L.Unlock()
				return 0, q.Quantity()
			}
			q.cond.Wait() // Wait for items to become available
			q.cond.L.Unlock()
			continue
		}

		if size := uint32(len(values)); quantity >= size {
			getCnt = size
		} else {
			getCnt = quantity
		}
		getPosNew = getPos + getCnt

		if atomic.CompareAndSwapUint32(&q.getPos, getPos, getPosNew) {
			break
		}

		runtime.Gosched()
		//time.Sleep(time.Microsecond)
	}

	for posNew, v := getPos+1, uint32(0); v < getCnt; posNew, v = posNew+1, v+1 {
		var cache *queCache = &q.cache[posNew&q.capMod]
		spin := 0
		for {
			getNo := atomic.LoadUint32(&cache.getNo)
			putNo := atomic.LoadUint32(&cache.putNo)
			if posNew == getNo && getNo == putNo-q.capacity {
				values[v] = cache.value
				cache.value = nil
				atomic.AddUint32(&cache.getNo, q.capacity)
				break
			} else {
				if spin < maxSpin {
					spin++
					runtime.Gosched() // 放弃时间片，让其他goroutine执行
				} else {
					time.Sleep(time.Microsecond)
				}
			}
		}
	}

	q.cond.L.Lock()
	q.cond.Broadcast() // Signal waiting putters
	q.cond.L.Unlock()
	return getCnt, q.Quantity()
}

// round 到最近的2的倍数
func minQuantity(v uint32) uint32 {
	v--
	v |= v >> 1
	v |= v >> 2
	v |= v >> 4
	v |= v >> 8
	v |= v >> 16
	v++
	return v
}
