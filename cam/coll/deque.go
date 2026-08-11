package coll

// minCapacity is the smallest capacity that deque may have.
// Must be power of 2 for bitwise modulus: x % n == x & (n - 1).
const minCapacity = 16

// Deque is a generic double-ended queue backed by a power-of-2 ring buffer.
// It grows when full and shrinks when a quarter full, amortizing allocation
// cost. The zero value is not ready to use; call NewDeque.
type Deque[T any] struct {
	buf    []T
	head   int
	tail   int
	count  int
	minCap int
}

// NewDeque creates a new Deque, optionally setting the initial and minimum
// capacity. Values are rounded up to the nearest power of 2.
//
//	d := NewDeque[int](2048, 32) // initial cap 2048, min cap 32
//	d := NewDeque[int]()          // lazy allocation
func NewDeque[T any](size ...int) *Deque[T] {
	var capacity, minimum int
	if len(size) >= 1 {
		capacity = size[0]
		if len(size) >= 2 {
			minimum = size[1]
		}
	}

	mc := minCapacity
	for mc < minimum {
		mc <<= 1
	}

	var buf []T
	if capacity != 0 {
		bufSize := mc
		for bufSize < capacity {
			bufSize <<= 1
		}
		buf = make([]T, bufSize)
	}

	return &Deque[T]{
		buf:    buf,
		minCap: mc,
	}
}

// Cap returns the current capacity of the Deque.
func (q *Deque[T]) Cap() int {
	return len(q.buf)
}

// Len returns the number of elements currently stored.
func (q *Deque[T]) Len() int {
	return q.count
}

// PushBack appends an element to the back of the queue.
func (q *Deque[T]) PushBack(elem T) {
	q.growIfFull()
	q.buf[q.tail] = elem
	q.tail = q.next(q.tail)
	q.count++
}

// PushFront prepends an element to the front of the queue.
func (q *Deque[T]) PushFront(elem T) {
	q.growIfFull()
	q.head = q.prev(q.head)
	q.buf[q.head] = elem
	q.count++
}

// PopFront removes and returns the element from the front.
// Panics if the queue is empty.
func (q *Deque[T]) PopFront() T {
	if q.count <= 0 {
		panic("coll: PopFront() called on empty deque")
	}
	ret := q.buf[q.head]
	var zero T
	q.buf[q.head] = zero
	q.head = q.next(q.head)
	q.count--
	q.shrinkIfExcess()
	return ret
}

// PopBack removes and returns the element from the back.
// Panics if the queue is empty.
func (q *Deque[T]) PopBack() T {
	if q.count <= 0 {
		panic("coll: PopBack() called on empty deque")
	}
	q.tail = q.prev(q.tail)
	ret := q.buf[q.tail]
	var zero T
	q.buf[q.tail] = zero
	q.count--
	q.shrinkIfExcess()
	return ret
}

// Front returns the element at the front without removing it.
// Panics if the queue is empty.
func (q *Deque[T]) Front() T {
	if q.count <= 0 {
		panic("coll: Front() called on empty deque")
	}
	return q.buf[q.head]
}

// Back returns the element at the back without removing it.
// Panics if the queue is empty.
func (q *Deque[T]) Back() T {
	if q.count <= 0 {
		panic("coll: Back() called on empty deque")
	}
	return q.buf[q.prev(q.tail)]
}

// At returns the element at index i (0-based from front) without removal.
// Panics if the index is out of range.
func (q *Deque[T]) At(i int) T {
	if i < 0 || i >= q.count {
		panic("coll: At() called with index out of range")
	}
	return q.buf[(q.head+i)&(len(q.buf)-1)]
}

// Set puts the element at index i. Panics if the index is out of range.
func (q *Deque[T]) Set(i int, elem T) {
	if i < 0 || i >= q.count {
		panic("coll: Set() called with index out of range")
	}
	q.buf[(q.head+i)&(len(q.buf)-1)] = elem
}

// Clear removes all elements, retaining the current capacity.
func (q *Deque[T]) Clear() {
	modBits := len(q.buf) - 1
	var zero T
	for h := q.head; h != q.tail; h = (h + 1) & modBits {
		q.buf[h] = zero
	}
	q.head = 0
	q.tail = 0
	q.count = 0
}

// Rotate rotates the deque n steps front-to-back.
// If n is negative, rotates back-to-front.
func (q *Deque[T]) Rotate(n int) {
	if q.count <= 1 {
		return
	}
	n %= q.count
	if n == 0 {
		return
	}

	modBits := len(q.buf) - 1
	if q.head == q.tail {
		q.head = (q.head + n) & modBits
		q.tail = (q.tail + n) & modBits
		return
	}

	var zero T
	if n < 0 {
		for ; n < 0; n++ {
			q.head = (q.head - 1) & modBits
			q.tail = (q.tail - 1) & modBits
			q.buf[q.head] = q.buf[q.tail]
			q.buf[q.tail] = zero
		}
		return
	}

	for ; n > 0; n-- {
		q.buf[q.tail] = q.buf[q.head]
		q.buf[q.head] = zero
		q.head = (q.head + 1) & modBits
		q.tail = (q.tail + 1) & modBits
	}
}

// SetMinCapacity sets a minimum capacity of 2^minCapacityExp.
func (q *Deque[T]) SetMinCapacity(minCapacityExp uint) {
	if 1<<minCapacityExp > minCapacity {
		q.minCap = 1 << minCapacityExp
	} else {
		q.minCap = minCapacity
	}
}

func (q *Deque[T]) prev(i int) int {
	return (i - 1) & (len(q.buf) - 1)
}

func (q *Deque[T]) next(i int) int {
	return (i + 1) & (len(q.buf) - 1)
}

func (q *Deque[T]) growIfFull() {
	if q.count != len(q.buf) {
		return
	}
	if len(q.buf) == 0 {
		if q.minCap == 0 {
			q.minCap = minCapacity
		}
		q.buf = make([]T, q.minCap)
		return
	}
	q.resize()
}

func (q *Deque[T]) shrinkIfExcess() {
	if len(q.buf) > q.minCap && (q.count<<2) == len(q.buf) {
		q.resize()
	}
}

func (q *Deque[T]) resize() {
	newBuf := make([]T, q.count<<1)
	if q.tail > q.head {
		copy(newBuf, q.buf[q.head:q.tail])
	} else {
		n := copy(newBuf, q.buf[q.head:])
		copy(newBuf[n:], q.buf[:q.tail])
	}
	q.head = 0
	q.tail = q.count
	q.buf = newBuf
}
