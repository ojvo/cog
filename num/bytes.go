package num

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"io"
	"math"
	"strconv"
	"strings"
)

type Bytes []byte
type Bs = Bytes // 简写

/* ------------- 基础接口 ------------- */

func (b Bytes) Len() int          { return len(b) }
func (b Bytes) Cap() int          { return cap(b) }
func (b Bytes) Error() string     { return b.String() }         // 使 Bytes 实现 error
func (b Bytes) Bytes() []byte     { return b }                  // 原始切片
func (b Bytes) Reader() io.Reader { return bytes.NewReader(b) } // io.Reader
func (b Bytes) Buffer() *bytes.Buffer {
	return bytes.NewBuffer(b.Copy()) // 拷贝一份，避免复用时污染
}
func (b Bytes) WriteTo(w io.Writer) (int64, error) { // io.WriterTo
	n, err := w.Write(b)
	return int64(n), err
}

/* ------------- 常用工具 ------------- */

func (b Bytes) Copy() Bytes {
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp
}

func (b Bytes) Equal(other Bytes) bool {
	if (b == nil) != (other == nil) {
		return false
	}
	return bytes.Equal(b, other)
}

func (b Bytes) Append(p ...byte) Bytes { return append(b, p...) }

func (b Bytes) Upper() Bytes { return bytes.ToUpper(b) }
func (b Bytes) Lower() Bytes { return bytes.ToLower(b) }

func (b Bytes) Sum() byte { // 所有字节累加，溢出按 uint8 规则回绕
	var s byte
	for _, v := range b {
		s += v
	}
	return s
}

/* ------------- 字符串表示 ------------- */

func (b Bytes) String() string { return string(b) }
func (b Bytes) UTF8() string   { return string(b) }
func (b Bytes) ASCII() string  { return string(b) }
func (b Bytes) HEX() string    { return hex.EncodeToString(b) }
func (b Bytes) Base64() string { return base64.StdEncoding.EncodeToString(b) }
func (b Bytes) HEXBase64() string {
	return Bytes(b.HEX()).Base64() // 先转 HEX 再转 Base64
}

/* ------------- 数值解析 ------------- */

func (b Bytes) GetFirst() byte { // 不存在返回 0
	if len(b) > 0 {
		return b[0]
	}
	return 0
}
func (b Bytes) GetLast() byte { // 不存在返回 0
	if l := len(b); l > 0 {
		return b[l-1]
	}
	return 0
}

// 大端转 uint64；超过 8 字节时保留低 8 字节；不足时高位补 0。
func (b Bytes) Uint64() uint64 {
	if len(b) == 0 {
		return 0
	}
	if len(b) > 8 {
		b = b[len(b)-8:]
	}
	var buf [8]byte
	copy(buf[8-len(b):], b) // 高位填 0
	return binary.BigEndian.Uint64(buf[:])
}
func (b Bytes) Int64() int64 { return int64(b.Uint64()) }

// ASCII 数字串 → int
func (b Bytes) ASCIIToInt() (int, error) { return strconv.Atoi(b.ASCII()) }
func (b Bytes) ASCIIToFloat64(decimals int) (float64, error) {
	i, err := b.ASCIIToInt()
	if err != nil {
		return 0, err
	}
	return float64(i) / math.Pow10(decimals), nil
}

// HEX 编码 → int（16 进制解析）
func (b Bytes) HEXToInt() (int, error) {
	v, err := strconv.ParseInt(b.HEX(), 16, 64)
	return int(v), err
}
func (b Bytes) HEXToFloat64(decimals int) (float64, error) {
	i, err := b.HEXToInt()
	if err != nil {
		return 0, err
	}
	return float64(i) / math.Pow10(decimals), nil
}

/* ------------- 进制字符串 ------------- */

// 二进制字符串（无空格，一字节 8 位）
func (b Bytes) BINStr() string { return byteToBinStr(b) }
func (b Bytes) BIN() string    { return byteToBinStr(b) }

func byteToBinStr(bs []byte) string {
	if len(bs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.Grow(len(bs) * 8)
	for _, v := range bs {
		for i := 7; i >= 0; i-- {
			if v&(1<<uint(i)) != 0 {
				sb.WriteByte('1')
			} else {
				sb.WriteByte('0')
			}
		}
	}
	return sb.String()
}

/* ------------- 顺序/位运算 ------------- */

func (b Bytes) Reverse() Bytes {
	n := len(b)
	r := make([]byte, n)
	for i, v := range b {
		r[n-1-i] = v
	}
	return r
}

func (b Bytes) ReverseASCII() string  { return b.Reverse().ASCII() }
func (b Bytes) ReverseHEX() string    { return b.Reverse().HEX() }
func (b Bytes) ReverseBase64() string { return b.Reverse().Base64() }

// Sub subtracts sub from each byte.
func (b Bytes) Sub(sub byte) Bytes {
	res := make([]byte, len(b))
	for i, v := range b {
		res[i] = v - sub
	}
	return res
}

// SubByte is an alias for Sub.
func (b Bytes) SubByte(sub byte) Bytes { return b.Sub(sub) }

// Add adds add to each byte.
func (b Bytes) Add(add byte) Bytes {
	res := make([]byte, len(b))
	for i, v := range b {
		res[i] = v + add
	}
	return res
}

// AddByte is an alias for Add.
func (b Bytes) AddByte(add byte) Bytes { return b.Add(add) }

func Reverse(bs []byte) []byte {
	x := make([]byte, len(bs))
	for i, v := range bs {
		x[len(bs)-i-1] = v
	}
	return x
}

/* ------------- 多字节整数解析 (大端) ------------- */

// Uint16 interprets first 2 bytes as big-endian uint16.
func (b Bytes) Uint16() uint16 {
	if len(b) >= 2 {
		return binary.BigEndian.Uint16(b)
	}
	var buf [2]byte
	copy(buf[2-len(b):], b)
	return binary.BigEndian.Uint16(buf[:])
}

// Int16 returns int16(b.Uint16()).
func (b Bytes) Int16() int16 { return int16(b.Uint16()) }

// Uint32 interprets first 4 bytes as big-endian uint32.
func (b Bytes) Uint32() uint32 {
	if len(b) >= 4 {
		return binary.BigEndian.Uint32(b)
	}
	var buf [4]byte
	copy(buf[4-len(b):], b)
	return binary.BigEndian.Uint32(buf[:])
}

// Int32 returns int32(b.Uint32()).
func (b Bytes) Int32() int32 { return int32(b.Uint32()) }

/* ------------- 负索引访问 ------------- */

// Get returns byte at idx, supporting negative index (Python-style: -1 = last).
func (b Bytes) Get(idx int) byte {
	if i := b.getIdx(idx); i >= 0 {
		return b[i]
	}
	return 0
}

func (b Bytes) getIdx(idx int) int {
	n := len(b)
	if idx >= 0 && idx < n {
		return idx
	}
	if idx < 0 && -idx <= n {
		return n + idx
	}
	return -1
}

/* ------------- 分割与查找 ------------- */

// Split splits by sep into []Bytes.
func (b Bytes) Split(sep []byte) []Bytes {
	parts := bytes.Split(b, sep)
	result := make([]Bytes, len(parts))
	for i := range parts {
		result[i] = parts[i]
	}
	return result
}

// SplitByLength splits into chunks of the given length.
func (b Bytes) SplitByLength(length int) []Bytes {
	if length <= 0 {
		return nil
	}
	var result []Bytes
	for len(b) > length {
		result = append(result, b[:length])
		b = b[length:]
	}
	return append(result, b)
}

// TrimSpace removes leading/trailing whitespace.
func (b Bytes) TrimSpace() Bytes { return bytes.TrimSpace(b) }

// Contains reports whether subslice is present.
func (b Bytes) Contains(sub []byte) bool { return bytes.Contains(b, sub) }

// HasPrefix reports whether b begins with prefix.
func (b Bytes) HasPrefix(prefix []byte) bool { return bytes.HasPrefix(b, prefix) }

// HasSuffix reports whether b ends with suffix.
func (b Bytes) HasSuffix(suffix []byte) bool { return bytes.HasSuffix(b, suffix) }

/* ------------- 哈希 ------------- */

// Md5 returns the MD5 hex digest of b.
func (b Bytes) Md5() string {
	sum := md5.Sum(b)
	return hex.EncodeToString(sum[:])
}

// Sha1 returns the SHA1 hex digest of b.
func (b Bytes) Sha1() string {
	sum := sha1.Sum(b)
	return hex.EncodeToString(sum[:])
}

// Sha256 returns the SHA256 hex digest of b.
func (b Bytes) Sha256() string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

/* ------------- 字节序重排 ------------- */

// Endian reorders bytes by a positional pattern string.
// Each digit '1'-'9' selects a byte from the current group; '_' skips a position.
// Example: {11,12,13,14,15,16,17,18} "21" → {12,11,14,13,16,15,18,17}
//
//	{11,12,13,14,15,16,17,18} "4321" → {14,13,12,11,18,17,16,15}
func (b Bytes) Endian(order string) Bytes {
	if len(b) == 0 || len(order) == 0 {
		return nil
	}

	effLen := 0
	for i := 0; i < len(order); i++ {
		if order[i] != '_' {
			effLen++
		}
	}
	if effLen == 0 {
		return nil
	}

	cap := len(b) * effLen / len(order)
	result := make(Bytes, 0, cap+1)

	sub := 0
	for i := 0; ; i++ {
		baseIdx := i * len(order)
		for _, v := range order {
			if len(result)+sub == len(b) {
				return result
			}
			if v == '_' {
				sub++
			} else if v >= '1' && v <= '9' {
				offset := int(v - '1')
				result = append(result, b.Get(baseIdx+offset))
			}
		}
	}
}
