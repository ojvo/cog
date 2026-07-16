package bus

import (
	"reflect"
	"sync/atomic"

	"c.n/ojv/cog/syncx"
)

// ALL is the reserved topic that, when AllowAsterisk is enabled, also receives
// every Trigger dispatched to any other topic. It can also be subscribed to
// directly to receive Broadcast messages or explicit Trigger(ALL) calls.
const ALL = "*"

// OffCallback is invoked after Off/OffCb removes handlers. count is the number
// of handlers remaining on the topic after removal; exists reports whether the
// topic still has any entry in the bus (true whenever the topic key is
// present, even with zero handlers, due to the underlying Upsert semantics).
type OffCallback func(count int, exists bool)

// Event is the handler contract for payloads of type T.
type Event[T any] interface {
	Dispatch(topic string, data []T)
}

// event wraps a handler with bookkeeping metadata.
type event[T any] struct {
	handler   Event[T]
	topic     string
	tag       reflect.Value
	isUnique  bool
	hasCalled uint32
}

func newEvent[T any](h Event[T], topic string, isUnique bool) *event[T] {
	return &event[T]{
		handler:  h,
		topic:    topic,
		tag:      reflect.ValueOf(h),
		isUnique: isUnique,
	}
}

// Bus is a generic in-process pub/sub event bus. It is safe for concurrent
// use. The zero value is not usable; construct via NewBus.
type Bus[T any] struct {
	allowAsterisk bool
	events        syncx.ConcurrentMap[string, []*event[T]]
}

// NewBus creates an empty Bus.
func NewBus[T any]() *Bus[T] {
	return &Bus[T]{
		events: syncx.NewMap[[]*event[T]](),
	}
}

// AllowAsterisk enables dispatching Trigger events to the ALL ("*") topic's
// handlers in addition to the named topic. Returns b for chaining.
func (b *Bus[T]) AllowAsterisk() *Bus[T] {
	b.allowAsterisk = true
	return b
}

// On registers a persistent handler for topic. Returns b for chaining.
func (b *Bus[T]) On(topic string, h Event[T]) *Bus[T] {
	b.addEvent(topic, false, h)
	return b
}

// Once registers a handler that is auto-removed after its first dispatch.
// Returns b for chaining.
func (b *Bus[T]) Once(topic string, h Event[T]) *Bus[T] {
	b.addEvent(topic, true, h)
	return b
}

// Off removes the given handlers from topic. If no handlers are given, the
// entire topic is removed. Returns b for chaining.
func (b *Bus[T]) Off(topic string, hs ...Event[T]) *Bus[T] {
	b.removeEventsCb(topic, hs)
	return b
}

// OffCb is like Off but invokes cb after removal. Returns b for chaining.
func (b *Bus[T]) OffCb(topic string, cb OffCallback, hs ...Event[T]) *Bus[T] {
	b.removeEventsCb(topic, hs, cb)
	return b
}

// Clean removes all topics and handlers. Returns b for chaining.
func (b *Bus[T]) Clean() *Bus[T] {
	b.events.Clear()
	return b
}

// Trigger dispatches msg to handlers of topic. When AllowAsterisk is set and
// topic != ALL, the ALL handlers also receive msg. Returns b for chaining.
func (b *Bus[T]) Trigger(topic string, msg ...T) *Bus[T] {
	b.dispatch(topic, msg)
	return b
}

// Broadcast dispatches msg to every handler on every topic. Returns b for
// chaining.
func (b *Bus[T]) Broadcast(msg ...T) *Bus[T] {
	b.broadcast(msg)
	return b
}

// Count returns the number of handlers registered under topic.
func (b *Bus[T]) Count(topic string) int {
	es, _ := b.events.Get(topic)
	return len(es)
}

// Total returns the number of topics that have at least one registered entry.
func (b *Bus[T]) Total() int {
	return b.events.Count()
}

func (b *Bus[T]) addEvent(topic string, isUnique bool, h Event[T]) {
	b.events.Upsert(topic, func(old []*event[T], _ bool) []*event[T] {
		return append(old, newEvent(h, topic, isUnique))
	})
}

func (b *Bus[T]) removeEventsCb(topic string, hs []Event[T], cb ...OffCallback) {
	if len(hs) == 0 {
		b.events.RemoveCb(topic, func(_ []*event[T], exists bool) bool {
			for _, callback := range cb {
				callback(0, exists)
			}
			return true
		})
		return
	}

	b.events.Upsert(topic, func(old []*event[T], exist bool) []*event[T] {
		if !exist || len(old) == 0 {
			return []*event[T]{}
		}
		// Build a filter list of handler identity tags once, then keep only
		// the events whose tag does not match any. This replaces the
		// original range+append removal which silently skipped elements when
		// multiple matches were present.
		tags := make([]reflect.Value, 0, len(hs))
		for _, h := range hs {
			tags = append(tags, reflect.ValueOf(h))
		}
		filtered := make([]*event[T], 0, len(old))
		for _, e := range old {
			matched := false
			for _, t := range tags {
				if e.tag == t {
					matched = true
					break
				}
			}
			if !matched {
				filtered = append(filtered, e)
			}
		}
		return filtered
	})

	b.events.RemoveCb(topic, func(value []*event[T], exists bool) bool {
		count := len(value)
		for _, callback := range cb {
			callback(count, exists)
		}
		return count == 0
	})
}

func (b *Bus[T]) dispatch(topic string, data []T) {
	removes := make(map[string][]Event[T])
	events := make([]*event[T], 0)

	b.events.GetCb(topic, func(es []*event[T], exists bool) {
		if !exists {
			return
		}
		events = append(events, es...)
	})

	if b.allowAsterisk && topic != ALL {
		b.events.GetCb(ALL, func(es []*event[T], exists bool) {
			if !exists {
				return
			}
			events = append(events, es...)
		})
	}

	dispatch(topic, events, removes, data)

	for k, v := range removes {
		b.removeEventsCb(k, v)
	}
}

func (b *Bus[T]) broadcast(data []T) {
	removes := make(map[string][]Event[T])
	all := make([]*event[T], 0)

	b.events.IterCb(func(_ string, es []*event[T]) {
		all = append(all, es...)
	})

	dispatch("", all, removes, data)

	for k, v := range removes {
		b.removeEventsCb(k, v)
	}
}

func dispatch[T any](topic string, events []*event[T], removes map[string][]Event[T], data []T) {
	for _, e := range events {
		if !e.isUnique {
			e.handler.Dispatch(topic, data)
			continue
		}
		if atomic.CompareAndSwapUint32(&e.hasCalled, 0, 1) {
			e.handler.Dispatch(topic, data)
			removes[e.topic] = append(removes[e.topic], e.handler)
		}
	}
}
