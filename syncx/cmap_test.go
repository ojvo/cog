package syncx

import (
	"encoding/json"
	"hash/fnv"
	"slices"
	"sort"
	"strconv"
	"sync"
	"testing"
)

type animal struct {
	name string
}

func (a animal) String() string { return a.name }

func TestMapCreation(t *testing.T) {
	m := NewMap[animal]()
	if m.Count() != 0 {
		t.Error("new map should be empty")
	}
}

func TestMapSetAndCount(t *testing.T) {
	m := NewMap[animal]()
	m.Set("elephant", animal{"elephant"})
	m.Set("monkey", animal{"monkey"})
	if m.Count() != 2 {
		t.Error("map should contain exactly two elements")
	}
}

func TestMapSetIfAbsent(t *testing.T) {
	m := NewMap[animal]()
	elephant := animal{"elephant"}
	monkey := animal{"monkey"}
	m.SetIfAbsent("elephant", elephant)
	if ok := m.SetIfAbsent("elephant", monkey); ok {
		t.Error("SetIfAbsent should return false when entry already present")
	}
}

func TestMapSetIfExists(t *testing.T) {
	m := NewMap[string]()
	if ok := m.SetIfExists("k1", "v1"); ok {
		t.Error("SetIfExists should return false when key absent")
	}
	m.Set("k1", "v1")
	if ok := m.SetIfExists("k1", "v2"); !ok {
		t.Error("SetIfExists should return true when key present")
	}
	v, _ := m.Get("k1")
	if v != "v2" {
		t.Errorf("value not updated, got %s", v)
	}
}

func TestMapGet(t *testing.T) {
	m := NewMap[animal]()
	if _, ok := m.Get("missing"); ok {
		t.Error("ok should be false when item is missing")
	}
	elephant := animal{"elephant"}
	m.Set("elephant", elephant)
	v, ok := m.Get("elephant")
	if !ok {
		t.Error("ok should be true for stored item")
	}
	if v.name != "elephant" {
		t.Error("item was modified")
	}
}

func TestMapHas(t *testing.T) {
	m := NewMap[animal]()
	if m.Has("missing") {
		t.Error("missing element should not exist")
	}
	m.Set("elephant", animal{"elephant"})
	if !m.Has("elephant") {
		t.Error("element should exist")
	}
}

func TestMapRemove(t *testing.T) {
	m := NewMap[animal]()
	m.Set("monkey", animal{"monkey"})
	m.Remove("monkey")
	if m.Count() != 0 {
		t.Error("count should be zero after removal")
	}
	// Remove missing element is a no-op.
	m.Remove("noone")
}

func TestMapRemoveCb(t *testing.T) {
	m := NewMap[animal]()
	monkey := animal{"monkey"}
	elephant := animal{"elephant"}
	m.Set("monkey", monkey)
	m.Set("elephant", elephant)

	var (
		gotVal   animal
		wasFound bool
	)
	cb := func(val animal, exists bool) bool {
		gotVal = val
		wasFound = exists
		return val.name == "monkey"
	}

	if !m.RemoveCb("monkey", cb) {
		t.Error("RemoveCb should return true for removed key")
	}
	if gotVal != monkey {
		t.Error("wrong value provided to callback")
	}
	if !wasFound {
		t.Error("key was not found")
	}
	if m.Has("monkey") {
		t.Error("key was not removed")
	}

	// Elephant should not be removed because cb returns false.
	if m.RemoveCb("elephant", cb) {
		t.Error("RemoveCb should return false when cb returns false")
	}
	if !m.Has("elephant") {
		t.Error("elephant was removed unexpectedly")
	}

	// Unset key: cb sees zero value, exists=false.
	gotVal = animal{}
	if m.RemoveCb("horse", cb) {
		t.Error("RemoveCb should return false for absent key")
	}
	if wasFound {
		t.Error("absent key should report exists=false")
	}
}

func TestMapPop(t *testing.T) {
	m := NewMap[animal]()
	monkey := animal{"monkey"}
	m.Set("monkey", monkey)
	v, exists := m.Pop("monkey")
	if !exists || v != monkey {
		t.Error("Pop did not find the value")
	}
	if _, exists2 := m.Pop("monkey"); exists2 {
		t.Error("Pop should not find value twice")
	}
	if m.Count() != 0 {
		t.Error("count should be zero after Pop")
	}
}

func TestMapIsEmpty(t *testing.T) {
	m := NewMap[animal]()
	if !m.IsEmpty() {
		t.Error("new map should be empty")
	}
	m.Set("elephant", animal{"elephant"})
	if m.IsEmpty() {
		t.Error("map with items should not be empty")
	}
}

func TestMapIterBuffered(t *testing.T) {
	m := NewMap[animal]()
	for i := 0; i < 100; i++ {
		m.Set(strconv.Itoa(i), animal{strconv.Itoa(i)})
	}
	counter := 0
	for item := range m.IterBuffered() {
		if item.Val == (animal{}) {
			t.Error("expecting a non-zero value")
		}
		counter++
	}
	if counter != 100 {
		t.Errorf("expected 100 elements, got %d", counter)
	}
}

func TestMapIterCb(t *testing.T) {
	m := NewMap[animal]()
	for i := 0; i < 100; i++ {
		m.Set(strconv.Itoa(i), animal{strconv.Itoa(i)})
	}
	counter := 0
	m.IterCb(func(_ string, _ animal) { counter++ })
	if counter != 100 {
		t.Errorf("expected 100 elements, got %d", counter)
	}
}

func TestMapItems(t *testing.T) {
	m := NewMap[animal]()
	for i := 0; i < 100; i++ {
		m.Set(strconv.Itoa(i), animal{strconv.Itoa(i)})
	}
	if len(m.Items()) != 100 {
		t.Error("Items should contain 100 elements")
	}
}

func TestMapClear(t *testing.T) {
	m := NewMap[animal]()
	for i := 0; i < 100; i++ {
		m.Set(strconv.Itoa(i), animal{strconv.Itoa(i)})
	}
	m.Clear()
	if m.Count() != 0 {
		t.Error("count should be zero after Clear")
	}
}

func TestMapKeys(t *testing.T) {
	m := NewMap[animal]()
	for i := 0; i < 100; i++ {
		m.Set(strconv.Itoa(i), animal{strconv.Itoa(i)})
	}
	if len(m.Keys()) != 100 {
		t.Error("Keys should contain 100 elements")
	}
}

func TestMapValues(t *testing.T) {
	m := NewMap[int]()
	tests := []int{1, 3, 5, 7, 9, 2, 4, 6, 8, 0}
	for _, v := range tests {
		m.Set(strconv.Itoa(v), v)
	}
	values := m.Values()
	for _, v := range tests {
		if !slices.Contains(values, v) {
			t.Errorf("Values should contain %d", v)
		}
	}
}

func TestMapMSet(t *testing.T) {
	data := map[string]animal{
		"elephant": {"elephant"},
		"monkey":   {"monkey"},
	}
	m := NewMap[animal]()
	m.MSet(data)
	if m.Count() != 2 {
		t.Error("map should contain exactly two elements")
	}
}

func TestMapUpsert(t *testing.T) {
	dolphin := animal{"dolphin"}
	whale := animal{"whale"}
	tiger := animal{"tiger"}
	lion := animal{"lion"}

	cb := func(in animal) UpsertCb[animal] {
		return func(old animal, exists bool) animal {
			if !exists {
				return in
			}
			return animal{name: old.name + in.name}
		}
	}

	m := NewMap[animal]()
	m.Set("marine", dolphin)
	m.Upsert("marine", cb(whale))
	m.Upsert("predator", cb(tiger))
	m.Upsert("predator", cb(lion))

	if m.Count() != 2 {
		t.Error("map should contain exactly two elements")
	}
	marine, ok := m.Get("marine")
	if !ok || marine.name != "dolphinwhale" {
		t.Errorf("Upsert on existing key failed: %v", marine)
	}
	pred, ok := m.Get("predator")
	if !ok || pred.name != "tigerlion" {
		t.Errorf("Upsert on new key then update failed: %v", pred)
	}
}

func TestMapGetOrInsert(t *testing.T) {
	m := NewMap[string]()
	if v := m.GetOrInsert("k1", func() string { return "v1" }); v != "v1" {
		t.Errorf("expected v1, got %s", v)
	}
	if v := m.GetOrInsert("k1", func() string { return "v2" }); v != "v1" {
		t.Errorf("expected existing v1, got %s", v)
	}
}

func TestMapGetOrInsertConcurrent(t *testing.T) {
	m := NewMap[int]()
	const routines = 10
	var wg sync.WaitGroup
	wg.Add(routines)
	for i := 0; i < routines; i++ {
		go func(idx int) {
			defer wg.Done()
			v := m.GetOrInsert("k", func() int { return idx })
			if v < 0 || v >= routines {
				t.Errorf("unexpected value %d", v)
			}
		}(i)
	}
	wg.Wait()
	if m.Count() != 1 {
		t.Error("map should contain only one value")
	}
}

func TestMapGetCb(t *testing.T) {
	m := NewMap[string]()
	m.Set("key1", "value1")
	called := false
	m.GetCb("key1", func(v string, exist bool) {
		called = true
		if !exist || v != "value1" {
			t.Errorf("unexpected (%s, %v)", v, exist)
		}
	})
	if !called {
		t.Error("callback was not invoked")
	}
	// Missing key path.
	called = false
	m.GetCb("missing", func(v string, exist bool) {
		called = true
		if exist {
			t.Error("missing key should report exist=false")
		}
	})
	if !called {
		t.Error("callback was not invoked for missing key")
	}
}

func TestMapConcurrent(t *testing.T) {
	m := NewMap[int]()
	const iterations = 1000
	ch := make(chan int, iterations)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations/2; i++ {
			m.Set(strconv.Itoa(i), i)
			v, _ := m.Get(strconv.Itoa(i))
			ch <- v
		}
	}()
	go func() {
		defer wg.Done()
		for i := iterations / 2; i < iterations; i++ {
			m.Set(strconv.Itoa(i), i)
			v, _ := m.Get(strconv.Itoa(i))
			ch <- v
		}
	}()
	wg.Wait()
	close(ch)

	var got [iterations]int
	counter := 0
	for elem := range ch {
		got[counter] = elem
		counter++
	}
	if counter != iterations {
		t.Fatalf("expected %d, got %d", iterations, counter)
	}
	sort.Ints(got[:])
	for i := 0; i < iterations; i++ {
		if i != got[i] {
			t.Errorf("missing value %d", i)
		}
	}
	if m.Count() != iterations {
		t.Errorf("expected %d elements, got %d", iterations, m.Count())
	}
}

func TestMapJSONMarshal(t *testing.T) {
	m := NewMap[int]()
	m.Set("a", 1)
	m.Set("b", 2)
	j, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	// JSON object key order is unspecified; parse back and compare.
	var got map[string]int
	if err := json.Unmarshal(j, &got); err != nil {
		t.Fatal(err)
	}
	if got["a"] != 1 || got["b"] != 2 || len(got) != 2 {
		t.Errorf("unexpected json: %s", j)
	}
}

func TestMapJSONUnmarshal(t *testing.T) {
	m := NewMap[string]()
	if err := json.Unmarshal([]byte(`{"k1":"v1","k2":"v2"}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.Count() != 2 {
		t.Error("expected 2 entries")
	}
	if v, _ := m.Get("k1"); v != "v1" {
		t.Errorf("expected v1, got %s", v)
	}

	// Invalid JSON should error.
	m2 := NewMap[string]()
	if err := json.Unmarshal([]byte(`{"k1":}`), &m2); err == nil {
		t.Error("expected error for invalid JSON")
	}

	// Non-object JSON should error.
	m3 := NewMap[string]()
	if err := json.Unmarshal([]byte(`["not","object"]`), &m3); err == nil {
		t.Error("expected error for non-object JSON")
	}
}

func TestMapEmptyJSON(t *testing.T) {
	m := NewMap[string]()
	j, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(j) != "{}" {
		t.Errorf("empty map should marshal to {}, got %s", j)
	}
}

func TestMapEmptyKey(t *testing.T) {
	m := NewMap[animal]()
	m.Set("", animal{"elephant"})
	v, ok := m.Get("")
	if !ok || v.name != "elephant" {
		t.Error("empty string key handling failed")
	}
	m.Remove("")
	if m.Count() != 0 {
		t.Error("count should be zero after removing empty key")
	}
}

func TestMapFnv32(t *testing.T) {
	key := []byte("ABC")
	hasher := fnv.New32()
	if _, err := hasher.Write(key); err != nil {
		t.Fatal(err)
	}
	if fnv32(string(key)) != hasher.Sum32() {
		t.Errorf("fnv32 produced %d, hash/fnv.New32 produced %d",
			fnv32(string(key)), hasher.Sum32())
	}
}

func TestMapStrfnv32(t *testing.T) {
	a := animal{"elephant"}
	if strfnv32(a) != fnv32("elephant") {
		t.Error("strfnv32 should hash via String() representation")
	}
}

func TestMapNewStringerMap(t *testing.T) {
	m := NewStringerMap[animal, int]()
	if m.Count() != 0 {
		t.Error("new map should be empty")
	}
	cat := animal{"cat"}
	m.Set(cat, 1)
	v, ok := m.Get(cat)
	if !ok || v != 1 {
		t.Error("Stringer-keyed map should retrieve stored value")
	}
}

func TestMapNewMapWithCustom(t *testing.T) {
	// All keys hash to 5, so they share one shard.
	sharding := func(_ string) uint32 { return 5 }
	m := NewMapWithCustom[string, int](sharding)
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)
	want := map[string]int{"a": 1, "b": 2, "c": 3}
	for k, v := range want {
		got, ok := m.Get(k)
		if !ok || got != v {
			t.Errorf("Get(%s) = (%d, %v), want (%d, true)", k, got, ok, v)
		}
	}
}

func TestMapNewMapWithShards_NonPositive(t *testing.T) {
	m := NewMapWithShards[string, int](0, fnv32)
	m.Set("k", 1)
	if v, _ := m.Get("k"); v != 1 {
		t.Error("zero shard count should fall back to defaultShards")
	}
}

func TestMapKeysWhenRemoving(t *testing.T) {
	m := NewMap[animal]()
	for i := 0; i < 100; i++ {
		m.Set(strconv.Itoa(i), animal{strconv.Itoa(i)})
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			m.Remove(strconv.Itoa(n))
		}(i)
	}
	wg.Wait()
	for _, k := range m.Keys() {
		if k == "" {
			t.Error("empty key returned")
		}
	}
}

func TestMapUnDrainedIterBuffered(t *testing.T) {
	m := NewMap[animal]()
	for i := 0; i < 100; i++ {
		m.Set(strconv.Itoa(i), animal{strconv.Itoa(i)})
	}
	counter := 0
	ch := m.IterBuffered()
	for item := range ch {
		if item.Val == (animal{}) {
			t.Error("expecting a non-zero value")
		}
		counter++
		if counter == 42 {
			break
		}
	}
	for i := 100; i < 200; i++ {
		m.Set(strconv.Itoa(i), animal{strconv.Itoa(i)})
	}
	for item := range ch {
		if item.Val == (animal{}) {
			t.Error("expecting a non-zero value")
		}
		counter++
	}
	if counter != 100 {
		t.Errorf("snapshot should contain 100 elements, got %d", counter)
	}
	counter = 0
	for range m.IterBuffered() {
		counter++
	}
	if counter != 200 {
		t.Errorf("full iteration should yield 200 elements, got %d", counter)
	}
}
