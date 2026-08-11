package dafe

import (
	"encoding/json"
	"fmt"
	"reflect"

	"ojv/cog/util"
)

// ConvertOption controls ConvertInto tag matching behavior.
type ConvertOption struct {
	Tags []string // defaults to []string{"json"}
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

func convertInto(src, dst any, opts ...ConvertOption) error {
	if src == nil || dst == nil {
		return nil
	}
	sv := reflect.ValueOf(src)
	st := reflect.TypeOf(src)
	dv := reflect.ValueOf(dst)
	dt := reflect.TypeOf(dst)
	if dt.Kind() != reflect.Ptr {
		return fmt.Errorf("参数(ptr)需要指针类型")
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
			bs := util.AsBytes(x)
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
			copyBaseKindValue(key, reflect.ValueOf(util.AsString(x.Key().Interface())))
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
			m[util.AsString(x.Key().Interface())] = x.Value()
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
		copyBaseKindValue(result, reflect.ValueOf(util.AsString(original.Interface())))
	case reflect.Bool:
		copyBaseKindValue(result, reflect.ValueOf(util.AsBool(original.Interface())))
	case reflect.Int:
		copyBaseKindValue(result, reflect.ValueOf(int(util.AsInt64(original.Interface()))))
	case reflect.Int8:
		copyBaseKindValue(result, reflect.ValueOf(int8(util.AsInt64(original.Interface()))))
	case reflect.Int16:
		copyBaseKindValue(result, reflect.ValueOf(int16(util.AsInt64(original.Interface()))))
	case reflect.Int32:
		copyBaseKindValue(result, reflect.ValueOf(int32(util.AsInt64(original.Interface()))))
	case reflect.Int64:
		copyBaseKindValue(result, reflect.ValueOf(util.AsInt64(original.Interface())))
	case reflect.Uint:
		copyBaseKindValue(result, reflect.ValueOf(uint(util.AsUint64(original.Interface()))))
	case reflect.Uint8:
		copyBaseKindValue(result, reflect.ValueOf(uint8(util.AsUint64(original.Interface()))))
	case reflect.Uint16:
		copyBaseKindValue(result, reflect.ValueOf(uint16(util.AsUint64(original.Interface()))))
	case reflect.Uint32:
		copyBaseKindValue(result, reflect.ValueOf(uint32(util.AsUint64(original.Interface()))))
	case reflect.Uint64:
		copyBaseKindValue(result, reflect.ValueOf(util.AsUint64(original.Interface())))
	case reflect.Float32:
		copyBaseKindValue(result, reflect.ValueOf(float32(util.AsFloat64(original.Interface()))))
	case reflect.Float64:
		copyBaseKindValue(result, reflect.ValueOf(util.AsFloat64(original.Interface())))
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
