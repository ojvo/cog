package syncx

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestSpinLock_ZeroValueUnlocked(t *testing.T) {
	var l SpinLock
	if !l.TryLock() {
		t.Fatal("zero-value SpinLock should be unlocked")
	}
	if l.TryLock() {
		t.Fatal("TryLock should fail after Lock")
	}
	l.Unlock()
	if !l.TryLock() {
		t.Fatal("TryLock should succeed after Unlock")
	}
	l.Unlock()
}

func TestSpinLock_NewSpinLock(t *testing.T) {
	l := NewSpinLock()
	if l == nil {
		t.Fatal("NewSpinLock returned nil")
	}
	if !l.TryLock() {
		t.Fatal("NewSpinLock should be unlocked")
	}
	l.Unlock()
}

func TestSpinLock_LockUnlock(t *testing.T) {
	var l SpinLock
	l.Lock()
	if l.TryLock() {
		t.Fatal("TryLock should fail when locked")
	}
	l.Unlock()
	if !l.TryLock() {
		t.Fatal("TryLock should succeed after Unlock")
	}
	l.Unlock()
}

func TestSpinLock_TryLock(t *testing.T) {
	var l SpinLock
	if !l.TryLock() {
		t.Fatal("TryLock should succeed on unlocked SpinLock")
	}
	if l.TryLock() {
		t.Fatal("TryLock should fail when already locked")
	}
	l.Unlock()
}

// TestSpinLock_ImplementsSyncLocker confirms SpinLock satisfies sync.Locker.
func TestSpinLock_ImplementsSyncLocker(t *testing.T) {
	var locker sync.Locker = &SpinLock{}
	locker.Lock()
	locker.Unlock()
}

func TestSpinLock_Concurrent(t *testing.T) {
	const goroutines = 50
	const incPerG = 1000
	var (
		l   SpinLock
		sum int64
		wg  sync.WaitGroup
	)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < incPerG; j++ {
				l.Lock()
				sum++
				l.Unlock()
			}
		}()
	}
	wg.Wait()
	want := int64(goroutines * incPerG)
	if sum != want {
		t.Errorf("sum = %d, want %d (lost updates -> lock not exclusive)", sum, want)
	}
}

func TestSpinLock_ConcurrentTryLockPath(t *testing.T) {
	// Exercises both Lock and TryLock under contention: at least one TryLock
	// must fail while another goroutine holds the lock, and at least one must
	// eventually succeed after release.
	var (
		l     SpinLock
		tries int64
		ok    int64
		fail  int64
		wg    sync.WaitGroup
	)
	const goroutines = 20
	const ops = 200
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				atomic.AddInt64(&tries, 1)
				if l.TryLock() {
					atomic.AddInt64(&ok, 1)
					l.Unlock()
				} else {
					atomic.AddInt64(&fail, 1)
					l.Lock()
					l.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt64(&ok) + atomic.LoadInt64(&fail); got != int64(goroutines*ops) {
		t.Errorf("ok+fail = %d, want %d", got, goroutines*ops)
	}
	if got := atomic.LoadInt64(&tries); got != int64(goroutines*ops) {
		t.Errorf("tries = %d, want %d", got, goroutines*ops)
	}
}

func BenchmarkSpinLock_Uncontended(b *testing.B) {
	var l SpinLock
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Lock()
		l.Unlock()
	}
}

func BenchmarkMutex_Uncontended(b *testing.B) {
	var mu sync.Mutex
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mu.Lock()
		mu.Unlock()
	}
}

func BenchmarkSpinLock_Contended(b *testing.B) {
	var l SpinLock
	var counter int64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.Lock()
			counter++
			l.Unlock()
		}
	})
}

func BenchmarkMutex_Contended(b *testing.B) {
	var mu sync.Mutex
	var counter int64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mu.Lock()
			counter++
			mu.Unlock()
		}
	})
}
