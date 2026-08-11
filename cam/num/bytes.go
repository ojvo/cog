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

// NewBs converts v into Bytes.
func NewBs(v interface{}) Bs {
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		return Bytes(x)
	case string:
		return Bytes(x)
	default:
		return Bytes([]byte(strconv.FormatBool(v == x)))
	}
}

/* ------------- 基础接口 ------------- */

func (b Bytes) Len() int          { return len(b) }
func (b Bytes) Cap() int          { return cap(b) }
func (b Bytes) Error() string     { return b.String() }
func (b Bytes) Bytes() []byte     { return b }
func (b Bytes) Reader() io.Reader { return bytes.NewReader(b) }
func (b Bytes) Buffer() *bytes.Buffer {
	return bytes.NewBuffer(b.Copy())
}
func (b Bytes) WriteTo(w io.Writer) (int64, error) {
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
func (b Bytes) Upper() Bytes           { return bytes.ToUpper(b) }
func (b Bytes) Lower() Bytes           { return bytes.ToLower(b) }

func (b Bytes) Sum() byte {
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
	return Bytes(b.HEX()).Base64()
}

/* ------------- 数值解析 ------------- */

func (b Bytes) GetFirst() byte {
	if len(b) > 0 {
		return b[0]
	}
	return 0
}
func (b Bytes) GetLast() byte {
	if l := len(b); l > 0 {
		return b[l-1]
	}
	return 0
}

func (b Bytes) Uint64() uint64 {
	if len(b) == 0 {
		return 0
	}
	if len(b) > 8 {
		b = b[len(b)-8:]
	}
	var buf [8]byte
	copy(buf[8-len(b):], b)
	return binary.BigEndian.Uint64(buf[:])
}
func (b Bytes) Int64() int64 { return int64(b.Uint64()) }
func (b Bytes) Uint() uint   { return uint(b.Uint64()) }
func (b Bytes) Int() int     { return int(b.Int64()) }
func (b Bytes) Uint8() uint8 { return uint8(b.Uint64()) }
func (b Bytes) Int8() int8   { return int8(b.Int64()) }

func (b Bytes) ASCIIToInt() (int, error) { return strconv.Atoi(b.ASCII()) }
func (b Bytes) UTF8ToInt() (int, error)  { return b.ASCIIToInt() }
func (b Bytes) ASCIIToFloat64(decimals int) (float64, error) {
	i, err := b.ASCIIToInt()
	if err != nil {
		return 0, err
	}
	return float64(i) / math.Pow10(decimals), nil
}
func (b Bytes) UTF8ToFloat64(decimals int) (float64, error) { return b.ASCIIToFloat64(decimals) }

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

func (b Bytes) BINStr() string { return byteToBinStr(b) }
func (b Bytes) BIN() string    { return byteToBinStr(b) }
func (b Bytes) OCT() string {
	if len(b) == 0 {
		return strings.Repeat("0", 22)
	}
	return fmtOctUint64(b.Uint64())
}

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

func fmtOctUint64(n uint64) string {
	if n == 0 {
		return strings.Repeat("0", 21) + "0"
	}
	s := strconv.FormatUint(n, 8)
	if len(s) >= 22 {
		return s
	}
	return strings.Repeat("0", 22-len(s)) + s
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

func (b Bytes) Sub(sub byte) Bytes {
	res := make([]byte, len(b))
	for i, v := range b {
		res[i] = v - sub
	}
	return res
}
func (b Bytes) SubByte(sub byte) Bytes { return b.Sub(sub) }

func (b Bytes) Add(add byte) Bytes {
	res := make([]byte, len(b))
	for i, v := range b {
		res[i] = v + add
	}
	return res
}
func (b Bytes) AddByte(add byte) Bytes { return b.Add(add) }

func Reverse(bs []byte) []byte {
	x := make([]byte, len(bs))
	for i, v := range bs {
		x[len(bs)-i-1] = v
	}
	return x
}

/* ------------- 多字节整数解析 (大端) ------------- */

func (b Bytes) Uint16() uint16 {
	if len(b) >= 2 {
		return binary.BigEndian.Uint16(b)
	}
	var buf [2]byte
	copy(buf[2-len(b):], b)
	return binary.BigEndian.Uint16(buf[:])
}
func (b Bytes) Int16() int16 { return int16(b.Uint16()) }

func (b Bytes) Uint32() uint32 {
	if len(b) >= 4 {
		return binary.BigEndian.Uint32(b)
	}
	var buf [4]byte
	copy(buf[4-len(b):], b)
	return binary.BigEndian.Uint32(buf[:])
}
func (b Bytes) Int32() int32 { return int32(b.Uint32()) }

func (b Bytes) Float64frombits() float64 { return math.Float64frombits(b.Uint64()) }
func (b Bytes) Float32frombits() float32 { return math.Float32frombits(b.Uint32()) }
func (b Bytes) Float64() float64 {
	if len(b) <= 4 {
		return float64(b.Float32frombits())
	}
	return b.Float64frombits()
}
func (b Bytes) Float32() float32 { return float32(b.Float64()) }
func (b Bytes) Float() float64   { return b.Float64() }
func (b Bytes) Bool() bool {
	return len(b) > 0 && !(len(b) == 1 && b[0] == 0)
}

/* ------------- 负索引访问 ------------- */

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

func (b Bytes) Split(sep []byte) []Bytes {
	parts := bytes.Split(b, sep)
	result := make([]Bytes, len(parts))
	for i := range parts {
		result[i] = parts[i]
	}
	return result
}

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

func (b Bytes) TrimSpace() Bytes            { return bytes.TrimSpace(b) }
func (b Bytes) Contains(sub []byte) bool    { return bytes.Contains(b, sub) }
func (b Bytes) HasPrefix(prefix []byte) bool { return bytes.HasPrefix(b, prefix) }
func (b Bytes) HasSuffix(suffix []byte) bool { return bytes.HasSuffix(b, suffix) }

/* ------------- 哈希 ------------- */

func (b Bytes) Md5() string {
	sum := md5.Sum(b)
	return hex.EncodeToString(sum[:])
}
func (b Bytes) Sha1() string {
	sum := sha1.Sum(b)
	return hex.EncodeToString(sum[:])
}
func (b Bytes) Sha256() string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

/* ------------- 字节序重排 ------------- */

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
	capHint := len(b) * effLen / len(order)
	result := make(Bytes, 0, capHint+1)
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
