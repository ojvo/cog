package bus

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type stringHandler struct {
	counter *int
	last    string
}

func (h *stringHandler) Dispatch(_ string, data []string) {
	*h.counter++
	for _, s := range data {
		h.last = s
	}
}

func TestBus_On_Trigger(t *testing.T) {
	b := NewBus[string]()
	n := 0
	h := &stringHandler{&n, ""}
	b.On("foo", h)
	b.Trigger("foo", "bar")
	if n != 1 {
		t.Errorf("counter = %d, want 1", n)
	}
	if h.last != "bar" {
		t.Errorf("last = %q, want %q", h.last, "bar")
	}
}

func TestBus_Trigger_NoListeners(t *testing.T) {
	// Trigger on a topic with no listeners should not panic.
	b := NewBus[any]()
	b.Trigger("nope")
}

func TestBus_Trigger_EmptyBroadcast(t *testing.T) {
	b := NewBus[string]()
	b.Broadcast()
}

func TestBus_Once(t *testing.T) {
	b := NewBus[string]()
	n := 0
	b.Once("foo", &stringHandler{&n, ""})
	b.Trigger("foo", "a").Trigger("foo", "b").Trigger("foo", "c")
	if n != 1 {
		t.Errorf("Once handler should be called once, got %d", n)
	}
	if b.Count("foo") != 0 {
		t.Errorf("Once handler should be auto-removed, count = %d", b.Count("foo"))
	}
}

func TestBus_Once_MultipleTopics(t *testing.T) {
	b := NewBus[string]()
	n1, n2 := 0, 0
	onFoo := &stringHandler{&n1, ""}
	onBar := &stringHandler{&n2, ""}
	b.Once("foo", onFoo)
	b.Once("bar", onBar)
	b.Trigger("foo", "x1").Trigger("foo", "x2")
	b.Trigger("bar", "y1").Trigger("bar", "y2")
	if n1 != 1 || n2 != 1 {
		t.Errorf("Once counters: foo=%d, bar=%d; want 1, 1", n1, n2)
	}
	if onFoo.last != "x1" {
		t.Errorf("foo last = %q, want %q", onFoo.last, "x1")
	}
	if onBar.last != "y1" {
		t.Errorf("bar last = %q, want %q", onBar.last, "y1")
	}
}

func TestBus_Off_RemovesSingleHandler(t *testing.T) {
	b := NewBus[string]()
	n1, n2 := 0, 0
	h1 := &stringHandler{&n1, ""}
	h2 := &stringHandler{&n2, ""}
	b.On("topic", h1).On("topic", h2)
	b.Off("topic", h1)
	b.Trigger("topic", "x")
	if n1 != 0 {
		t.Errorf("h1 should not be called after Off, got %d", n1)
	}
	if n2 != 1 {
		t.Errorf("h2 should be called once, got %d", n2)
	}
}

func TestBus_Off_RemovesEntireTopic(t *testing.T) {
	b := NewBus[string]()
	n := 0
	h := &stringHandler{&n, ""}
	b.On("topic", h)
	b.Off("topic") // no handlers specified -> remove entire topic
	b.Trigger("topic", "x")
	if n != 0 {
		t.Errorf("handler should not be called after Off, got %d", n)
	}
	if b.Count("topic") != 0 {
		t.Errorf("Count should be 0 after Off, got %d", b.Count("topic"))
	}
}

// TestBus_Off_MultipleMatches verifies the bug fix: the original jt code used
// range+append to remove elements, which silently skipped elements when
// multiple matches were present. Our filter-based implementation removes all
// matches correctly.
func TestBus_Off_MultipleMatches(t *testing.T) {
	b := NewBus[string]()
	n := 0
	h := &stringHandler{&n, ""}
	// Register the same handler multiple times.
	b.On("topic", h).On("topic", h).On("topic", h)
	if b.Count("topic") != 3 {
		t.Fatalf("Count = %d, want 3", b.Count("topic"))
	}
	// Remove all three instances of h in a single Off call.
	b.Off("topic", h, h, h)
	if b.Count("topic") != 0 {
		t.Errorf("after removing all matches, Count = %d, want 0", b.Count("topic"))
	}
	b.Trigger("topic", "x")
	if n != 0 {
		t.Errorf("handler should not be called after removing all matches, got %d", n)
	}
}

// TestBus_Off_MultipleMatchesOneByOne verifies the bug fix when matches are
// interleaved with non-matches: range+append would skip the element
// immediately after a removed one.
func TestBus_Off_MultipleMatchesOneByOne(t *testing.T) {
	b := NewBus[string]()
	n1, n2 := 0, 0
	target := &stringHandler{&n1, ""}
	other := &stringHandler{&n2, ""}
	// Interleave target and other handlers.
	b.On("t", target).On("t", other).On("t", target).On("t", other).On("t", target)
	if b.Count("t") != 5 {
		t.Fatalf("Count = %d, want 5", b.Count("t"))
	}
	// Remove all target handlers. The original bug would skip the `other`
	// handler immediately following each `target`, leaving some targets in.
	b.Off("t", target)
	if b.Count("t") != 2 {
		t.Errorf("after removing targets, Count = %d, want 2 (other handlers)", b.Count("t"))
	}
	b.Trigger("t", "x")
	if n1 != 0 {
		t.Errorf("target should not be called, got %d", n1)
	}
	if n2 != 2 {
		t.Errorf("other should be called twice, got %d", n2)
	}
}

func TestBus_OffCb(t *testing.T) {
	b := NewBus[string]()
	n := 0
	h := &stringHandler{&n, ""}
	b.On("foo", h).On("bar", h)

	var (
		called bool
		count  int
		exists bool
	)
	b.OffCb("foo", func(c int, e bool) {
		called = true
		count = c
		exists = e
	}, h)

	if !called {
		t.Error("callback should be invoked")
	}
	if !exists {
		t.Error("exists should be true (topic still has an entry due to Upsert)")
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}

	b.Trigger("foo", "x").Trigger("bar", "x")
	if n != 1 {
		t.Errorf("counter = %d, want 1 (bar only)", n)
	}
}

func TestBus_OffCb_NonExistentTopic(t *testing.T) {
	b := NewBus[string]()
	called := false
	var exists bool
	b.OffCb("missing", func(c int, e bool) {
		called = true
		exists = e
	})
	if !called {
		t.Error("callback should be invoked even for missing topic")
	}
	if exists {
		t.Error("exists should be false for missing topic (no handlers specified path)")
	}
}

func TestBus_Count(t *testing.T) {
	b := NewBus[string]()
	n := 0
	h := &stringHandler{&n, ""}
	b.On("foo", h).On("foo", h).On("bar", h)
	if b.Count("foo") != 2 {
		t.Errorf("Count(foo) = %d, want 2", b.Count("foo"))
	}
	if b.Count("bar") != 1 {
		t.Errorf("Count(bar) = %d, want 1", b.Count("bar"))
	}
	if b.Count("missing") != 0 {
		t.Errorf("Count(missing) = %d, want 0", b.Count("missing"))
	}
}

func TestBus_Total(t *testing.T) {
	b := NewBus[string]()
	n := 0
	h := &stringHandler{&n, ""}
	if b.Total() != 0 {
		t.Errorf("Total = %d, want 0", b.Total())
	}
	b.On("foo", h).On("bar", h).On("baz", h)
	if b.Total() != 3 {
		t.Errorf("Total = %d, want 3", b.Total())
	}
	b.Off("foo", h)
	if b.Total() != 2 {
		t.Errorf("Total after Off = %d, want 2", b.Total())
	}
	b.Clean()
	if b.Total() != 0 {
		t.Errorf("Total after Clean = %d, want 0", b.Total())
	}
}

func TestBus_Broadcast(t *testing.T) {
	b := NewBus[string]()
	n1, n2, n3 := 0, 0, 0
	b.On("t1", &stringHandler{&n1, ""})
	b.On("t2", &stringHandler{&n2, ""})
	b.Once("t3", &stringHandler{&n3, ""})
	b.Broadcast("data")
	if n1 != 1 || n2 != 1 || n3 != 1 {
		t.Errorf("after first Broadcast: n1=%d, n2=%d, n3=%d; want 1,1,1", n1, n2, n3)
	}
	// Once handler should be removed; second Broadcast should not invoke it.
	b.Broadcast("data2")
	if n1 != 2 || n2 != 2 || n3 != 1 {
		t.Errorf("after second Broadcast: n1=%d, n2=%d, n3=%d; want 2,2,1", n1, n2, n3)
	}
	if b.Count("t3") != 0 {
		t.Errorf("Once handler should be removed, Count(t3) = %d", b.Count("t3"))
	}
}

func TestBus_AllowAsterisk(t *testing.T) {
	b := NewBus[string]().AllowAsterisk()
	topicN, allN := 0, 0
	b.On("specific", &stringHandler{&topicN, ""})
	b.On(ALL, &stringHandler{&allN, ""})
	b.Trigger("specific", "x")
	if topicN != 1 || allN != 1 {
		t.Errorf("AllowAsterisk: topic=%d, all=%d; want 1, 1", topicN, allN)
	}
}

func TestBus_Asterisk_NotEnabledByDefault(t *testing.T) {
	b := NewBus[string]() // no AllowAsterisk
	topicN, allN := 0, 0
	b.On("specific", &stringHandler{&topicN, ""})
	b.On(ALL, &stringHandler{&allN, ""})
	b.Trigger("specific", "x")
	if topicN != 1 {
		t.Errorf("topic handler should be called, got %d", topicN)
	}
	if allN != 0 {
		t.Errorf("ALL handler should not be called without AllowAsterisk, got %d", allN)
	}
}

func TestBus_Trigger_ALL_Direct(t *testing.T) {
	b := NewBus[string]()
	n := 0
	b.On(ALL, &stringHandler{&n, ""})
	b.Trigger(ALL, "x")
	if n != 1 {
		t.Errorf("direct Trigger(ALL) should call ALL handler, got %d", n)
	}
}

func TestBus_Chaining(t *testing.T) {
	b := NewBus[string]()
	n1, n2, n3 := 0, 0, 0
	b.On("e1", &stringHandler{&n1, ""}).
		On("e2", &stringHandler{&n2, ""}).
		Once("e3", &stringHandler{&n3, ""})
	b.Trigger("e1", "d1").Trigger("e2", "d2").Trigger("e3", "d3")
	if n1 != 1 || n2 != 1 || n3 != 1 {
		t.Errorf("chaining trigger: n1=%d, n2=%d, n3=%d; want 1,1,1", n1, n2, n3)
	}
	if b.Clean().Total() != 0 {
		t.Errorf("Clean should leave Total=0, got %d", b.Total())
	}
}

type intEvent struct{ sum *int }

func (e *intEvent) Dispatch(_ string, data []int) {
	for _, v := range data {
		*e.sum += v
	}
}

func TestBus_GenericTypes_Int(t *testing.T) {
	b := NewBus[int]()
	sum := 0
	b.On("nums", &intEvent{&sum})
	b.Trigger("nums", 1, 2, 3, 4, 5)
	if sum != 15 {
		t.Errorf("sum = %d, want 15", sum)
	}
}

type complexData struct {
	ID   int
	Name string
}

type complexHandler struct {
	lastID   *int
	lastName *string
}

func (h *complexHandler) Dispatch(_ string, data []complexData) {
	if len(data) > 0 {
		*h.lastID = data[len(data)-1].ID
		*h.lastName = data[len(data)-1].Name
	}
}

func TestBus_GenericTypes_Complex(t *testing.T) {
	b := NewBus[complexData]()
	var lastID int
	var lastName string
	h := &complexHandler{&lastID, &lastName}
	b.On("c", h)
	b.Trigger("c", complexData{1, "Alice"}, complexData{2, "Bob"})
	if lastID != 2 || lastName != "Bob" {
		t.Errorf("complex dispatch: lastID=%d, lastName=%q; want 2, Bob", lastID, lastName)
	}
}

func TestBus_ConcurrentTrigger(t *testing.T) {
	b := NewBus[string]()
	var counter int32
	type atomicHandler struct{ c *int32 }
	h := &atomicStringHandler{&counter}
	b.On("concurrent", h)
	var wg sync.WaitGroup
	workers := 100
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			b.Trigger("concurrent", "data")
		}()
	}
	wg.Wait()
	if atomic.LoadInt32(&counter) != int32(workers) {
		t.Errorf("counter = %d, want %d", counter, workers)
	}
}

type atomicStringHandler struct{ c *int32 }

func (h *atomicStringHandler) Dispatch(_ string, _ []string) {
	atomic.AddInt32(h.c, 1)
	time.Sleep(time.Millisecond)
}

func TestBus_Race(t *testing.T) {
	b := NewBus[string]()
	var counter int32
	h := &atomicStringHandler{&counter}
	b.On("foo", h)
	var wg sync.WaitGroup
	workers := 5
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			b.Trigger("foo")
		}()
	}
	wg.Wait()
	if atomic.LoadInt32(&counter) != int32(workers) {
		t.Errorf("counter = %d, want %d", counter, workers)
	}
}

func TestBus_Off_NonExistentTopic(t *testing.T) {
	b := NewBus[string]()
	n := 0
	h := &stringHandler{&n, ""}
	// Off on a topic that doesn't exist should not panic.
	b.Off("missing", h)
	b.Trigger("missing", "x")
}
