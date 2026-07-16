package util

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var falseStringSet = map[string]struct{}{
	"": {}, "0": {}, "no": {}, "off": {}, "false": {}, "close": {},
	"关": {}, "否": {}, "假": {},
}

// ConvertOption controls ConvertInto tag matching behavior.
type ConvertOption struct {
	Tags []string // defaults to []string{"json"}
}

// AsBytes converts v to bytes. Numeric values are encoded in big-endian form;
// unsupported values fall back to their string form bytes.
func AsBytes(v any) []byte {
	if v == nil {
		return []byte{}
	}
	switch x := v.(type) {
	case []byte:
		return x
	case string:
		return []byte(x)
	case []bool:
		var b byte
		for _, bit := range x {
			b *= 2
			if bit {
				b++
			}
		}
		return AsBytes(b)
	case bool:
		if x {
			return []byte("true")
		}
		return []byte("false")
	}

	rv := reflect.ValueOf(v)
	buf := bytes.NewBuffer(nil)
	switch rv.Kind() {
	case reflect.Int:
		if strconv.IntSize == 32 {
			if err := binary.Write(buf, binary.BigEndian, int32(rv.Int())); err == nil {
				return buf.Bytes()
			}
		} else {
			if err := binary.Write(buf, binary.BigEndian, rv.Int()); err == nil {
				return buf.Bytes()
			}
		}
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		if err := binary.Write(buf, binary.BigEndian, v); err == nil {
			return buf.Bytes()
		}
	case reflect.Uint:
		if strconv.IntSize == 32 {
			if err := binary.Write(buf, binary.BigEndian, uint32(rv.Uint())); err == nil {
				return buf.Bytes()
			}
		} else {
			if err := binary.Write(buf, binary.BigEndian, rv.Uint()); err == nil {
				return buf.Bytes()
			}
		}
	}
	return []byte(AsString(v))
}

// AsString converts v to string using common fast paths, then JSON for complex values.
func AsString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case int:
		return strconv.Itoa(x)
	case int8:
		return strconv.Itoa(int(x))
	case int16:
		return strconv.Itoa(int(x))
	case int32:
		return strconv.Itoa(int(x))
	case int64:
		return strconv.FormatInt(x, 10)
	case uint:
		return strconv.FormatUint(uint64(x), 10)
	case uint8:
		return strconv.FormatUint(uint64(x), 10)
	case uint16:
		return strconv.FormatUint(uint64(x), 10)
	case uint32:
		return strconv.FormatUint(uint64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case string:
		return x
	case []byte:
		return string(x)
	case time.Time:
		return x.String()
	case time.Duration:
		return x.String()
	case fmt.Stringer:
		return x.String()
	case error:
		return x.Error()
	case io.Reader:
		bs, _ := io.ReadAll(x)
		return string(bs)
	}

	rv := reflect.ValueOf(v)
	kind := rv.Kind()
	switch kind {
	case reflect.Chan, reflect.Map, reflect.Slice, reflect.Func, reflect.Ptr,
		reflect.Interface, reflect.UnsafePointer:
		if rv.IsNil() {
			return ""
		}
	}
	if kind == reflect.Ptr {
		return AsString(rv.Elem().Interface())
	}
	if bs, err := json.Marshal(v); err == nil {
		return string(bs)
	}
	return fmt.Sprint(v)
}

// AsInt64 converts v to int64. Supports decimal, 0x hex, 0b binary, and time.Duration strings.
func AsInt64(v any) int64 {
	if v == nil {
		return 0
	}
	switch x := v.(type) {
	case int:
		return int64(x)
	case int8:
		return int64(x)
	case int16:
		return int64(x)
	case int32:
		return int64(x)
	case int64:
		return x
	case uint:
		return int64(x)
	case uint8:
		return int64(x)
	case uint16:
		return int64(x)
	case uint32:
		return int64(x)
	case uint64:
		return int64(x)
	case float32:
		return int64(x)
	case float64:
		return int64(x)
	case bool:
		if x {
			return 1
		}
		return 0
	case []byte:
		return int64(binary.BigEndian.Uint64(padLeft(x, 8)))
	case []bool:
		var n int64
		for _, bit := range x {
			n *= 2
			if bit {
				n++
			}
		}
		return n
	case time.Time:
		return x.UnixNano()
	case time.Duration:
		return int64(x)
	}

	s := AsString(v)
	sign := int64(1)
	if len(s) > 0 {
		switch s[0] {
		case '-':
			sign = -1
			s = s[1:]
		case '+':
			s = s[1:]
		}
	}
	if len(s) > 2 && strings.EqualFold(s[:2], "0x") {
		if n, err := strconv.ParseInt(s[2:], 16, 64); err == nil {
			return n * sign
		}
	}
	if len(s) > 2 && strings.EqualFold(s[:2], "0b") {
		if n, err := strconv.ParseInt(s[2:], 2, 64); err == nil {
			return n * sign
		}
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n * sign
	}
	if d, err := time.ParseDuration(s); err == nil {
		return int64(d) * sign
	}
	return int64(AsFloat64(v))
}

// AsUint64 converts v to uint64. Supports decimal, 0x hex, 0b binary, and time.Duration strings.
func AsUint64(v any) uint64 {
	if v == nil {
		return 0
	}
	switch x := v.(type) {
	case int:
		return uint64(x)
	case int8:
		return uint64(x)
	case int16:
		return uint64(x)
	case int32:
		return uint64(x)
	case int64:
		return uint64(x)
	case uint:
		return uint64(x)
	case uint8:
		return uint64(x)
	case uint16:
		return uint64(x)
	case uint32:
		return uint64(x)
	case uint64:
		return x
	case float32:
		return uint64(x)
	case float64:
		return uint64(x)
	case bool:
		if x {
			return 1
		}
		return 0
	case []byte:
		return binary.BigEndian.Uint64(padLeft(x, 8))
	case []bool:
		var n uint64
		for _, bit := range x {
			n *= 2
			if bit {
				n++
			}
		}
		return n
	case time.Time:
		return uint64(x.UnixNano())
	case time.Duration:
		return uint64(x)
	}
	s := AsString(v)
	if len(s) > 2 && strings.EqualFold(s[:2], "0x") {
		if n, err := strconv.ParseUint(s[2:], 16, 64); err == nil {
			return n
		}
	}
	if len(s) > 2 && strings.EqualFold(s[:2], "0b") {
		if n, err := strconv.ParseUint(s[2:], 2, 64); err == nil {
			return n
		}
	}
	if n, err := strconv.ParseUint(s, 10, 64); err == nil {
		return n
	}
	if d, err := time.ParseDuration(s); err == nil {
		return uint64(d)
	}
	return uint64(AsFloat64(v))
}

// AsFloat64 converts v to float64. Supports IEEE754 bit decoding for uint32/uint64 and []byte.
func AsFloat64(v any) float64 {
	if v == nil {
		return 0
	}
	switch x := v.(type) {
	case float32:
		return AsFloat64(AsString(x))
	case float64:
		return x
	case uint32:
		return float64(mathFloat32frombits(x))
	case uint64:
		return mathFloat64frombits(x)
	case []byte:
		if len(x) <= 4 {
			return AsFloat64(mathFloat32frombits(binary.BigEndian.Uint32(padLeft(x, 4))))
		}
		return mathFloat64frombits(binary.BigEndian.Uint64(padLeft(x, 8)))
	default:
		n, _ := strconv.ParseFloat(AsString(v), 64)
		return n
	}
}

// AsBool converts v to bool. Empty/zero/off-like strings are false.
func AsBool(v any) bool {
	if v == nil {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case []byte, string:
		_, ok := falseStringSet[strings.ToLower(AsString(x))]
		return !ok
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr:
		return !rv.IsNil()
	case reflect.Map, reflect.Array, reflect.Slice:
		return rv.Len() != 0
	case reflect.Struct:
		return true
	default:
		_, ok := falseStringSet[strings.ToLower(AsString(v))]
		return !ok
	}
}

// AsStrings converts any slice-like or scalar value to []string.
func AsStrings(v any) []string {
	items := AsAnySlice(v)
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, AsString(it))
	}
	return out
}

// AsInts converts any slice-like or scalar value to []int.
func AsInts(v any) []int {
	items := AsAnySlice(v)
	out := make([]int, 0, len(items))
	for _, it := range items {
		out = append(out, int(AsInt64(it)))
	}
	return out
}

// AsInt64s converts any slice-like or scalar value to []int64.
func AsInt64s(v any) []int64 {
	items := AsAnySlice(v)
	out := make([]int64, 0, len(items))
	for _, it := range items {
		out = append(out, AsInt64(it))
	}
	return out
}

// AsAnySlice converts common slices/arrays or a scalar to []any.
func AsAnySlice(v any) []any {
	if v == nil {
		return nil
	}
	if xs, ok := v.([]any); ok {
		return xs
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = rv.Index(i).Interface()
		}
		return out
	default:
		return []any{v}
	}
}

// AsAnyMap converts v to map[string]any by field/tag or map key conversion.
func AsAnyMap(v any) map[string]any {
	m := make(map[string]any)
	_ = ConvertInto(v, &m)
	return m
}

// AsStringMap converts v to map[string]string by field/tag or map key conversion.
func AsStringMap(v any) map[string]string {
	m := make(map[string]string)
	_ = ConvertInto(v, &m)
	return m
}

// ConvertInto converts src into dst using field-name/tag-aware reflection mapping.
// For struct/map/slice targets, JSON is tried first when src is a JSON string/bytes.
func ConvertInto(src, dst any, opts ...ConvertOption) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("%v", e)
		}
	}()
	return convertInto(src, dst, opts...)
}

// CopyValue deep-copies v preserving Go kinds (ptr/struct/map/slice recursively).
func CopyValue(v any) any {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64, complex64, complex128,
		string, bool:
		return x
	default:
		orig := reflect.ValueOf(v)
		res := reflect.New(orig.Type()).Elem()
		copySameKindValue(res, orig)
		return res.Interface()
	}
}

func convertInto(src, dst any, opts ...ConvertOption) error {
	if src == nil || dst == nil {
		return nil
	}
	sv := reflect.ValueOf(src)
	st := reflect.TypeOf(src)
	dv := reflect.ValueOf(dst)
	dt := reflect.TypeOf(dst)
	if dt.Kind() != reflect.Ptr {
		return errorsNew("参数(ptr)需要指针类型")
	}
	for dt.Kind() == reflect.Ptr {
		if dv.IsNil() {
			dv.Set(reflect.New(dt.Elem()))
		}
		dv = dv.Elem()
		dt = dt.Elem()
	}
	for st.Kind() == reflect.Ptr {
		sv = sv.Elem()
		st = st.Elem()
	}

	switch dt.Kind() {
	case reflect.Struct, reflect.Map, reflect.Slice:
		switch x := sv.Interface().(type) {
		case string, []byte:
			bs := AsBytes(x)
			if len(bs) >= 2 && (bs[0] == '[' || bs[0] == '{') && (bs[len(bs)-1] == ']' || bs[len(bs)-1] == '}') {
				return json.Unmarshal(bs, dst)
			}
		}
	}

	switch dt.Kind() {
	case reflect.Struct:
		return copyStructValue(dv, sv, opts...)
	case reflect.Map:
		return copyMapValue(dv, sv, opts...)
	case reflect.Slice:
		return copySliceValue(dv, sv, opts...)
	default:
		copyBaseKindValue(dv, sv)
		return nil
	}
}

func copyMapValue(result, original reflect.Value, opts ...ConvertOption) error {
	if result.Kind() != reflect.Map {
		return nil
	}
	if result.IsNil() {
		result.Set(reflect.MakeMap(result.Type()))
	}
	switch original.Kind() {
	case reflect.Map:
		x := original.MapRange()
		for x.Next() {
			key := reflect.New(result.Type().Key()).Elem()
			copyBaseKindValue(key, reflect.ValueOf(AsString(x.Key().Interface())))
			val := reflect.New(result.Type().Elem()).Elem()
			if err := copyValueInto(val, x.Value(), opts...); err != nil {
				return err
			}
			result.SetMapIndex(key, val)
		}
	case reflect.Struct:
		tags := resolveTags(opts...)
		for i := 0; i < original.NumField(); i++ {
			f := original.Type().Field(i)
			key := fieldKey(f, tags)
			mv := reflect.New(result.Type().Elem()).Elem()
			if err := copyValueInto(mv, original.Field(i), opts...); err != nil {
				return err
			}
			mk := reflect.New(result.Type().Key()).Elem()
			copyBaseKindValue(mk, reflect.ValueOf(key))
			result.SetMapIndex(mk, mv)
		}
	}
	return nil
}

func copyStructValue(result, original reflect.Value, opts ...ConvertOption) error {
	if result.Kind() != reflect.Struct {
		return nil
	}
	tags := resolveTags(opts...)
	switch original.Kind() {
	case reflect.Struct:
		fieldMap := map[string]reflect.Value{}
		tagMaps := make([]map[string]reflect.Value, len(tags))
		for i := 0; i < original.NumField(); i++ {
			of := original.Type().Field(i)
			fieldMap[of.Name] = original.Field(i)
			for ti, tag := range tags {
				if key, ok := of.Tag.Lookup(tag); ok {
					if tagMaps[ti] == nil {
						tagMaps[ti] = map[string]reflect.Value{}
					}
					tagMaps[ti][key] = original.Field(i)
				}
			}
		}
		for i := 0; i < result.NumField(); i++ {
			f := result.Field(i)
			if !f.CanSet() {
				continue
			}
			rf := result.Type().Field(i)
			var src reflect.Value
			matched := false
			for ti, tag := range tags {
				if key, ok := rf.Tag.Lookup(tag); ok {
					if src, matched = tagMaps[ti][key]; matched {
						break
					}
				}
			}
			if !matched {
				src = fieldMap[rf.Name]
			}
			if err := copyValueInto(f, src, opts...); err != nil {
				return err
			}
		}
	case reflect.Map:
		m := map[string]reflect.Value{}
		x := original.MapRange()
		for x.Next() {
			m[AsString(x.Key().Interface())] = x.Value()
		}
		for i := 0; i < result.NumField(); i++ {
			f := result.Field(i)
			if !f.CanSet() {
				continue
			}
			rf := result.Type().Field(i)
			var src reflect.Value
			matched := false
			for _, tag := range tags {
				if key, ok := rf.Tag.Lookup(tag); ok {
					if src, matched = m[key]; matched {
						break
					}
				}
			}
			if !matched {
				src = m[rf.Name]
			}
			if err := copyValueInto(f, src, opts...); err != nil {
				return err
			}
		}
	}
	return nil
}

func copySliceValue(result, original reflect.Value, opts ...ConvertOption) error {
	if result.Kind() != reflect.Slice {
		return nil
	}
	switch original.Kind() {
	case reflect.Slice:
		result.Set(reflect.MakeSlice(result.Type(), original.Len(), original.Len()))
		for i := 0; i < original.Len(); i++ {
			if err := copyValueInto(result.Index(i), original.Index(i), opts...); err != nil {
				return err
			}
		}
	default:
		result.Set(reflect.MakeSlice(result.Type(), 1, 1))
		if err := copyValueInto(result.Index(0), original, opts...); err != nil {
			return err
		}
	}
	return nil
}

func copyValueInto(result, original reflect.Value, opts ...ConvertOption) error {
	if !result.CanAddr() || !original.IsValid() || !original.CanInterface() {
		return nil
	}
	return convertInto(original.Interface(), result.Addr().Interface(), opts...)
}

func copyBaseKindValue(result, original reflect.Value) {
	if !original.IsValid() {
		return
	}
	if result.Kind() == original.Kind() {
		if result.CanSet() {
			result.Set(original)
		}
		return
	}
	switch result.Kind() {
	case reflect.Interface:
		if result.CanSet() {
			result.Set(original)
		}
	case reflect.String:
		copyBaseKindValue(result, reflect.ValueOf(AsString(original.Interface())))
	case reflect.Bool:
		copyBaseKindValue(result, reflect.ValueOf(AsBool(original.Interface())))
	case reflect.Int:
		copyBaseKindValue(result, reflect.ValueOf(int(AsInt64(original.Interface()))))
	case reflect.Int8:
		copyBaseKindValue(result, reflect.ValueOf(int8(AsInt64(original.Interface()))))
	case reflect.Int16:
		copyBaseKindValue(result, reflect.ValueOf(int16(AsInt64(original.Interface()))))
	case reflect.Int32:
		copyBaseKindValue(result, reflect.ValueOf(int32(AsInt64(original.Interface()))))
	case reflect.Int64:
		copyBaseKindValue(result, reflect.ValueOf(AsInt64(original.Interface())))
	case reflect.Uint:
		copyBaseKindValue(result, reflect.ValueOf(uint(AsUint64(original.Interface()))))
	case reflect.Uint8:
		copyBaseKindValue(result, reflect.ValueOf(uint8(AsUint64(original.Interface()))))
	case reflect.Uint16:
		copyBaseKindValue(result, reflect.ValueOf(uint16(AsUint64(original.Interface()))))
	case reflect.Uint32:
		copyBaseKindValue(result, reflect.ValueOf(uint32(AsUint64(original.Interface()))))
	case reflect.Uint64:
		copyBaseKindValue(result, reflect.ValueOf(AsUint64(original.Interface())))
	case reflect.Float32:
		copyBaseKindValue(result, reflect.ValueOf(float32(AsFloat64(original.Interface()))))
	case reflect.Float64:
		copyBaseKindValue(result, reflect.ValueOf(AsFloat64(original.Interface())))
	}
}

func copySameKindValue(result, original reflect.Value) {
	if result.Kind() != original.Kind() {
		return
	}
	switch original.Kind() {
	case reflect.Ptr:
		if !original.IsNil() {
			result.Set(reflect.New(original.Type().Elem()))
			copySameKindValue(result.Elem(), original.Elem())
		}
	case reflect.Struct:
		for i := 0; i < original.NumField(); i++ {
			if result.Field(i).CanSet() {
				copySameKindValue(result.Field(i), original.Field(i))
			}
		}
	case reflect.Map:
		if !original.IsNil() {
			result.Set(reflect.MakeMap(original.Type()))
			for _, key := range original.MapKeys() {
				dk := reflect.New(key.Type()).Elem()
				copySameKindValue(dk, key)
				dv := reflect.New(original.MapIndex(key).Type()).Elem()
				copySameKindValue(dv, original.MapIndex(key))
				result.SetMapIndex(dk, dv)
			}
		}
	case reflect.Slice:
		if !original.IsNil() {
			result.Set(reflect.MakeSlice(original.Type(), original.Len(), original.Cap()))
			for i := 0; i < original.Len(); i++ {
				copySameKindValue(result.Index(i), original.Index(i))
			}
		}
	default:
		result.Set(original)
	}
}

func resolveTags(opts ...ConvertOption) []string {
	if len(opts) > 0 && len(opts[0].Tags) > 0 {
		return opts[0].Tags
	}
	return []string{"json"}
}

func fieldKey(f reflect.StructField, tags []string) string {
	for _, tag := range tags {
		if key, ok := f.Tag.Lookup(tag); ok {
			return key
		}
	}
	return f.Name
}

func padLeft(b []byte, length int) []byte {
	if len(b) >= length {
		return b[:length]
	}
	out := make([]byte, length)
	copy(out[length-len(b):], b)
	return out
}

func errorsNew(s string) error { return fmt.Errorf(s) }

// Small wrappers to avoid importing math just for two calls in this file.
func mathFloat32frombits(v uint32) float32 { return math.Float32frombits(v) }
func mathFloat64frombits(v uint64) float64 { return math.Float64frombits(v) }
