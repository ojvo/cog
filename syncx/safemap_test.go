package syncx

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestSafeMap_SetGet(t *testing.T) {
	s := NewSafeMap[string, int]()
	s.Set("k1", 10)
	v, ok := s.Get("k1")
	if !ok || v != 10 {
		t.Errorf("Get(k1) = (%d, %v), want (10, true)", v, ok)
	}
	if _, ok := s.Get("missing"); ok {
		t.Error("missing key should report ok=false")
	}
}

func TestSafeMap_Del(t *testing.T) {
	s := NewSafeMap[string, int]()
	s.Set("k1", 1)
	s.Del("k1")
	if s.Count() != 0 {
		t.Error("Count should be 0 after Del")
	}
	s.Del("missing") // no-op
}

func TestSafeMap_Count(t *testing.T) {
	s := NewSafeMap[string, int]()
	if s.Count() != 0 {
		t.Error("new SafeMap should be empty")
	}
	s.Set("a", 1)
	s.Set("b", 2)
	if s.Count() != 2 {
		t.Errorf("Count = %d, want 2", s.Count())
	}
}

func TestSafeMap_View(t *testing.T) {
	s := NewSafeMap[string, int]()
	s.Set("k1", 10)
	s.Set("k2", 20)
	var keys []string
	s.View(func(k string, _ int) { keys = append(keys, k) })
	want := []string{"k1", "k2"}
	if len(keys) != 2 || !slices.Contains(keys, "k1") || !slices.Contains(keys, "k2") {
		t.Errorf("View collected %v, want %v", keys, want)
	}
}

func TestSafeMap_Clone(t *testing.T) {
	s := NewSafeMap[string, int]()
	s.Set("k1", 1)
	s.Set("k2", 2)
	c := s.Clone()
	if len(c) != 2 || c["k1"] != 1 || c["k2"] != 2 {
		t.Errorf("Clone = %v, want {k1:1, k2:2}", c)
	}
	// Mutating clone should not affect original.
	c["k3"] = 3
	if _, ok := s.Get("k3"); ok {
		t.Error("mutating Clone should not affect original")
	}
}

func TestSafeMap_Find(t *testing.T) {
	s := NewSafeMap[string, int]()
	s.Set("k1", 10)
	s.Set("k2", 20)

	results := make(map[string]int)
	s.Find(func(key string, value int, exist bool) {
		if exist {
			results[key] = value
		} else {
			results[key] = -1
		}
	}, "k1", "k3")

	if results["k1"] != 10 {
		t.Errorf("k1 should be 10, got %d", results["k1"])
	}
	if results["k3"] != -1 {
		t.Errorf("k3 should be -1 (missing), got %d", results["k3"])
	}
}

func TestSafeMap_FindEmpty(t *testing.T) {
	s := NewSafeMap[string, int]()
	called := false
	s.Find(func(_ string, _ int, _ bool) { called = true })
	if called {
		t.Error("Find with no keys should not invoke callback")
	}
}

func TestSafeMap_Update(t *testing.T) {
	s := NewSafeMap[string, int]()
	s.Update(func(m map[string]int) {
		m["k1"] = 10
		m["k2"] = 20
	})
	if v, _ := s.Get("k1"); v != 10 {
		t.Errorf("Update did not persist k1, got %d", v)
	}
	if s.Count() != 2 {
		t.Errorf("Count = %d, want 2", s.Count())
	}
}

func TestSafeMap_GetCb(t *testing.T) {
	s := NewSafeMap[string, int]()
	s.Set("k1", 42)
	called := false
	s.GetCb("k1", func(v int, exist bool) {
		called = true
		if !exist || v != 42 {
			t.Errorf("GetCb got (%d, %v), want (42, true)", v, exist)
		}
	})
	if !called {
		t.Error("GetCb callback not invoked")
	}
}

func TestSafeMap_Clear(t *testing.T) {
	s := NewSafeMap[string, int]()
	s.Set("k1", 1)
	s.Set("k2", 2)
	s.Clear()
	if s.Count() != 0 {
		t.Error("Count should be 0 after Clear")
	}
}

func TestSafeMap_MarshalJSON(t *testing.T) {
	s := NewSafeMap[string, int]()
	s.Set("k1", 1)
	s.Set("k2", 2)
	data, err := s.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]int
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["k1"] != 1 || got["k2"] != 2 {
		t.Errorf("MarshalJSON = %s, want {k1:1,k2:2}", data)
	}
}

func TestSafeMap_UnmarshalJSON(t *testing.T) {
	s := NewSafeMap[string, int]()
	if err := json.Unmarshal([]byte(`{"k1":10,"k2":20}`), &s); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Get("k1"); v != 10 {
		t.Errorf("k1 = %d, want 10", v)
	}
	if v, _ := s.Get("k2"); v != 20 {
		t.Errorf("k2 = %d, want 20", v)
	}
}
