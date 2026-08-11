package num

import "testing"

func TestBytes_OCT(t *testing.T) {
	b := Bytes{0x01}
	got := b.OCT()
	want := "0000000000000000000001"
	if got != want {
		t.Fatalf("OCT() = %s, want %s", got, want)
	}
}

func TestBytes_FloatBits(t *testing.T) {
	b32 := Bytes{0x3f, 0x80, 0x00, 0x00}
	if got := b32.Float32frombits(); got != 1 {
		t.Fatalf("Float32frombits = %v", got)
	}
	if got := b32.Float64(); got != 1 {
		t.Fatalf("Float64 short bits = %v", got)
	}
	b64 := Bytes{0x3f, 0xf0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	if got := b64.Float64frombits(); got != 1 {
		t.Fatalf("Float64frombits = %v", got)
	}
}

func TestBytes_UTF8Numeric(t *testing.T) {
	b := Bytes("1234")
	if got, err := b.UTF8ToInt(); err != nil || got != 1234 {
		t.Fatalf("UTF8ToInt = (%d,%v)", got, err)
	}
	b2 := Bytes("1234")
	if got, err := b2.UTF8ToFloat64(2); err != nil || got != 12.34 {
		t.Fatalf("UTF8ToFloat64 = (%v,%v)", got, err)
	}
}

func TestBytes_BoolAndAliases(t *testing.T) {
	if !(Bytes{1}).Bool() {
		t.Fatal("Bool() should be true")
	}
	if (Bytes{0}).Bool() {
		t.Fatal("Bool() should be false")
	}
	if got := (Bytes{0x01}).Uint(); got != 1 {
		t.Fatalf("Uint() = %d", got)
	}
	if got := (Bytes{0x01}).Int(); got != 1 {
		t.Fatalf("Int() = %d", got)
	}
	if got := (Bytes{0x01}).Uint8(); got != 1 {
		t.Fatalf("Uint8() = %d", got)
	}
	if got := (Bytes{0x01}).Int8(); got != 1 {
		t.Fatalf("Int8() = %d", got)
	}
}

func TestNewBs(t *testing.T) {
	b := NewBs("abc")
	if b.String() != "abc" {
		t.Fatalf("NewBs string = %q", b.String())
	}
	b2 := NewBs([]byte("xy"))
	if b2.String() != "xy" {
		t.Fatalf("NewBs bytes = %q", b2.String())
	}
}
