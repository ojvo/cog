package coll

import (
	"fmt"
	"strings"
	"sync"
)

// Set is a generic set interface.
type Set[T comparable] interface {
	Add(items ...T)
	Remove(items ...T)
	Clear()
	Contains(items ...T) bool
	Len() int
	Same(other Set[T]) bool
	Values() []T
	String() string
}

// Hset is a thread-safe set implementation backed by a map.
type Hset[T comparable] struct {
	items map[T]struct{}
	mu    sync.RWMutex
}

// NewHset creates a new Hset.
func NewHset[T comparable]() *Hset[T] {
	return &Hset[T]{
		items: make(map[T]struct{}),
	}
}

// Add adds items to the set.
func (s *Hset[T]) Add(items ...T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range items {
		s.items[item] = struct{}{}
	}
}

// Remove removes items from the set.
func (s *Hset[T]) Remove(items ...T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range items {
		delete(s.items, item)
	}
}

// Clear removes all items from the set.
func (s *Hset[T]) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = make(map[T]struct{})
}

// Contains checks if the set contains all the given items.
func (s *Hset[T]) Contains(items ...T) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range items {
		if _, ok := s.items[item]; !ok {
			return false
		}
	}
	return true
}

// Len returns the number of items in the set.
func (s *Hset[T]) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}

// Empty checks if the set is empty.
func (s *Hset[T]) Empty() bool {
	return s.Len() == 0
}

// Values returns a slice of all items in the set.
func (s *Hset[T]) Values() []T {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := make([]T, 0, len(s.items))
	for item := range s.items {
		values = append(values, item)
	}
	return values
}

// Same checks if two sets contain the same elements.
func (s *Hset[T]) Same(other Set[T]) bool {
	if s == nil && other == nil {
		return true
	}
	if s == nil || other == nil {
		return false
	}

	if s.Len() != other.Len() {
		return false
	}

	myValues := s.Values()
	return other.Contains(myValues...)
}

// String returns a string representation of the set.
func (s *Hset[T]) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]string, 0, len(s.items))
	for k := range s.items {
		items = append(items, fmt.Sprintf("%v", k))
	}
	return "Hset{" + strings.Join(items, ", ") + "}"
}
