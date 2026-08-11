package syncx

import (
	"sync/atomic"
	"testing"
)

// --- Use ---

func TestUse_Basic(t *testing.T) {
	u := NewUse()
	if u.Used() {
		t.Error("new Use should not be used")
	}
	if !u.Use() {
		t.Error("first Use should succeed")
	}
	if !u.Used() {
		t.Error("Used should be true after successful Use")
	}
	if u.Use() {
		t.Error("second Use should fail")
	}
	if !u.UnUse() {
		t.Error("UnUse should succeed when used")
	}
	if u.Used() {
		t.Error("Used should be false after UnUse")
	}
}

func TestUse_UnUse_NotUsed(t *testing.T) {
	u := NewUse()
	if u.UnUse() {
		t.Error("UnUse should fail when not used")
	}
}

func TestUse_Callbacks(t *testing.T) {
	useCalled := false
	unuseCalled := false
	u := &Use{
		OnUse:   func() { useCalled = true },
		OnUnUse: func() { unuseCalled = true },
	}
	u.Use()
	if !useCalled {
		t.Error("OnUse should be called on successful Use")
	}
	u.UnUse()
	if !unuseCalled {
		t.Error("OnUnUse should be called on successful UnUse")
	}
}

// --- Once ---

func TestOnce_Do(t *testing.T) {
	var count int32
	o := &Once{}
	if !o.Do(func() { atomic.AddInt32(&count, 1) }) {
		t.Error("first Do should return true")
	}
	if o.Do(func() { atomic.AddInt32(&count, 1) }) {
		t.Error("second Do should return false")
	}
	if atomic.LoadInt32(&count) != 1 {
		t.Errorf("fn should be called once, got %d", count)
	}
	if !o.Done() {
		t.Error("Done should be true after Do")
	}
}

func TestOnce_Done_InitiallyFalse(t *testing.T) {
	o := &Once{}
	if o.Done() {
		t.Error("Done should be false before Do")
	}
}

// --- Bool ---

func TestBool_DefaultFalse(t *testing.T) {
	b := &Bool{}
	if b.IsTrue() {
		t.Error("zero Bool should be false")
	}
}

func TestBool_NewBool(t *testing.T) {
	if !NewBool(true).IsTrue() {
		t.Error("NewBool(true) should be true")
	}
	if NewBool(false).IsTrue() {
		t.Error("NewBool(false) should be false")
	}
}

func TestBool_Set(t *testing.T) {
	b := NewBool(false)
	if old := b.Set(true); old {
		t.Error("Set(true) on false should return old=false")
	}
	if !b.IsTrue() {
		t.Error("should be true after Set(true)")
	}
	if old := b.Set(true); !old {
		t.Error("Set(true) on true should return old=true")
	}
	if old := b.Set(false); !old {
		t.Error("Set(false) on true should return old=true")
	}
	if b.IsTrue() {
		t.Error("should be false after Set(false)")
	}
}

func TestBool_ListenTrue(t *testing.T) {
	b := NewBool(false)
	called := false
	if !b.ListenTrue(func() { called = true }) {
		t.Error("ListenTrue on false should return true")
	}
	if !called {
		t.Error("callback should be invoked on transition")
	}
	if !b.IsTrue() {
		t.Error("should be true after ListenTrue")
	}
	// Already true: no transition, no callback.
	called = false
	if b.ListenTrue(func() { called = true }) {
		t.Error("ListenTrue on true should return false")
	}
	if called {
		t.Error("callback should not be invoked when already true")
	}
}

func TestBool_ListenFalse(t *testing.T) {
	b := NewBool(true)
	called := false
	if !b.ListenFalse(func() { called = true }) {
		t.Error("ListenFalse on true should return true")
	}
	if !called {
		t.Error("callback should be invoked on transition")
	}
	if b.IsTrue() {
		t.Error("should be false after ListenFalse")
	}
	// Already false: no transition.
	called = false
	if b.ListenFalse(func() { called = true }) {
		t.Error("ListenFalse on false should return false")
	}
	if called {
		t.Error("callback should not be invoked when already false")
	}
}

func TestBool_ListenTrue_NilCallback(t *testing.T) {
	b := NewBool(false)
	if !b.ListenTrue(nil) {
		t.Error("ListenTrue with nil callback should still transition")
	}
	if !b.IsTrue() {
		t.Error("should be true after ListenTrue(nil)")
	}
}
