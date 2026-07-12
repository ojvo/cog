package num

import (
	"encoding/hex"
	"testing"
)

func TestBytes_Basic(t *testing.T) {
	b := Bytes("hello")

	if b.String() != "hello" {
		t.Errorf("String() = %s, want hello", b.String())
	}

	if b.Len() != 5 {
		t.Errorf("Len() = %d, want 5", b.Len())
	}

	if b.GetFirst() != 'h' {
		t.Errorf("GetFirst() = %c, want h", b.GetFirst())
	}

	if b.GetLast() != 'o' {
		t.Errorf("GetLast() = %c, want o", b.GetLast())
	}
}

func TestBytes_Encodings(t *testing.T) {
	b := Bytes("hello")

	// HEX
	h := b.HEX()
	expectedHex := hex.EncodeToString([]byte("hello"))
	if h != expectedHex {
		t.Errorf("HEX() = %s, want %s", h, expectedHex)
	}

	// Base64
	b64 := b.Base64()
	if b64 != "aGVsbG8=" {
		t.Errorf("Base64() = %s, want aGVsbG8=", b64)
	}
}

func TestBytes_Numeric(t *testing.T) {
	// Uint64 (Big Endian)
	// 1 -> 00 00 00 00 00 00 00 01
	val := uint64(0x1234567890ABCDEF)
	buf := make([]byte, 8)
	// Manual encoding for test
	buf[0] = 0x12
	buf[1] = 0x34
	buf[2] = 0x56
	buf[3] = 0x78
	buf[4] = 0x90
	buf[5] = 0xAB
	buf[6] = 0xCD
	buf[7] = 0xEF

	b := Bytes(buf)
	if b.Uint64() != val {
		t.Errorf("Uint64() = %x, want %x", b.Uint64(), val)
	}

	// Short buffer
	bShort := Bytes{0x01}
	if bShort.Uint64() != 1 {
		t.Errorf("Uint64() short = %d, want 1", bShort.Uint64())
	}
}

func TestBytes_Operations(t *testing.T) {
	b := Bytes("abc")

	// Reverse
	rev := b.Reverse()
	if rev.String() != "cba" {
		t.Errorf("Reverse() = %s, want cba", rev.String())
	}

	// AddByte
	added := b.AddByte(1)
	// 'a'+1 = 'b', 'b'+1 = 'c', 'c'+1 = 'd'
	if added.String() != "bcd" {
		t.Errorf("AddByte(1) = %s, want bcd", added.String())
	}

	// SubByte
	subbed := added.SubByte(1)
	if subbed.String() != "abc" {
		t.Errorf("SubByte(1) = %s, want abc", subbed.String())
	}

	// Sub / Add (aliases)
	subbed2 := b.Sub(1)
	if subbed2.String() != "`ab" { // 'a'-1 = 0x60 = '`'
		t.Errorf("Sub(1) = %s, want `ab", subbed2.String())
	}
	added2 := subbed2.Add(1)
	if added2.String() != "abc" {
		t.Errorf("Add(1) = %s, want abc", added2.String())
	}

	// Verify Sub == SubByte and Add == AddByte
	if !b.Sub(5).Equal(b.SubByte(5)) {
		t.Error("Sub != SubByte")
	}
	if !b.Add(5).Equal(b.AddByte(5)) {
		t.Error("Add != AddByte")
	}
}

func TestBytes_ReaderBuffer(t *testing.T) {
	b := Bytes("test")

	// Reader
	r := b.Reader()
	buf := make([]byte, 4)
	n, _ := r.Read(buf)
	if n != 4 || string(buf) != "test" {
		t.Error("Reader read failed")
	}

	// Buffer
	bf := b.Buffer()
	if bf.String() != "test" {
		t.Error("Buffer string mismatch")
	}
}

func TestBytes_BIN(t *testing.T) {
	b := Bytes{0b10101010} // 170
	bin := b.BIN()
	if bin != "10101010" {
		t.Errorf("BIN() = %s, want 10101010", bin)
	}
}

func TestBytes_MultiByteInt(t *testing.T) {
	// Uint16 big-endian: 0x1234
	b16 := Bytes{0x12, 0x34}
	if b16.Uint16() != 0x1234 {
		t.Errorf("Uint16() = %x, want 1234", b16.Uint16())
	}
	if b16.Int16() != 0x1234 {
		t.Errorf("Int16() = %d, want %d", b16.Int16(), 0x1234)
	}

	// Short buffer Uint16: {0x01} → 1
	if (Bytes{0x01}).Uint16() != 1 {
		t.Error("Uint16 short buffer failed")
	}

	// Uint32 big-endian: 0x12345678
	b32 := Bytes{0x12, 0x34, 0x56, 0x78}
	if b32.Uint32() != 0x12345678 {
		t.Errorf("Uint32() = %x, want 12345678", b32.Uint32())
	}
	if b32.Int32() != 0x12345678 {
		t.Errorf("Int32() = %d, want %d", b32.Int32(), 0x12345678)
	}

	// Short buffer Uint32: {0x01, 0x02} → 0x0102
	if (Bytes{0x01, 0x02}).Uint32() != 0x0102 {
		t.Error("Uint32 short buffer failed")
	}
}

func TestBytes_GetNegativeIndex(t *testing.T) {
	b := Bytes("hello")
	if b.Get(-1) != 'o' {
		t.Errorf("Get(-1) = %c, want o", b.Get(-1))
	}
	if b.Get(-5) != 'h' {
		t.Errorf("Get(-5) = %c, want h", b.Get(-5))
	}
	if b.Get(0) != 'h' {
		t.Errorf("Get(0) = %c, want h", b.Get(0))
	}
	if b.Get(10) != 0 {
		t.Errorf("Get(10) = %d, want 0", b.Get(10))
	}
}

func TestBytes_SplitOps(t *testing.T) {
	b := Bytes("a,b,c,d")
	parts := b.Split([]byte(","))
	if len(parts) != 4 {
		t.Fatalf("Split got %d parts, want 4", len(parts))
	}
	if parts[0].String() != "a" || parts[3].String() != "d" {
		t.Errorf("Split result unexpected: %v", parts)
	}

	// SplitByLength
	chunks := Bytes("abcdef").SplitByLength(2)
	if len(chunks) != 3 {
		t.Fatalf("SplitByLength got %d chunks, want 3", len(chunks))
	}
	if chunks[0].String() != "ab" || chunks[2].String() != "ef" {
		t.Errorf("SplitByLength unexpected: %v", chunks)
	}

	// SplitByLength with remainder
	chunks2 := Bytes("abcde").SplitByLength(2)
	if len(chunks2) != 3 || chunks2[2].String() != "e" {
		t.Errorf("SplitByLength remainder unexpected: %v", chunks2)
	}
}

func TestBytes_SearchOps(t *testing.T) {
	b := Bytes("  hello world  ")
	if !b.Contains([]byte("world")) {
		t.Error("Contains(world) should be true")
	}
	if !b.HasPrefix([]byte("  he")) {
		t.Error("HasPrefix should be true")
	}
	if !b.HasSuffix([]byte("ld  ")) {
		t.Error("HasSuffix should be true")
	}
	if b.TrimSpace().String() != "hello world" {
		t.Errorf("TrimSpace = %q", b.TrimSpace().String())
	}
}

func TestBytes_Hash(t *testing.T) {
	b := Bytes("hello")

	// MD5
	if b.Md5() != "5d41402abc4b2a76b9719d911017c592" {
		t.Errorf("Md5() = %s", b.Md5())
	}

	// SHA1
	if b.Sha1() != "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d" {
		t.Errorf("Sha1() = %s", b.Sha1())
	}

	// SHA256
	if b.Sha256() != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Errorf("Sha256() = %s", b.Sha256())
	}
}

func TestBytes_Endian(t *testing.T) {
	src := Bytes{11, 12, 13, 14, 15, 16, 17, 18}

	// "21" swaps pairs: {12,11,14,13,16,15,18,17}
	r1 := src.Endian("21")
	want1 := Bytes{12, 11, 14, 13, 16, 15, 18, 17}
	if !r1.Equal(want1) {
		t.Errorf("Endian(\"21\") = %v, want %v", r1, want1)
	}

	// "4321" reverses groups of 4: {14,13,12,11,18,17,16,15}
	r2 := src.Endian("4321")
	want2 := Bytes{14, 13, 12, 11, 18, 17, 16, 15}
	if !r2.Equal(want2) {
		t.Errorf("Endian(\"4321\") = %v, want %v", r2, want2)
	}

	// Empty
	if (Bytes{}).Endian("21") != nil {
		t.Error("Endian on empty should return nil")
	}
	if src.Endian("") != nil {
		t.Error("Endian with empty order should return nil")
	}
}
