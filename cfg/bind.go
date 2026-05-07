package cfg

import (
	"reflect"
	"strings"
	"sync"
	"time"
)

var structCache sync.Map // map[reflect.Type]*structMeta

type structMeta struct {
	fields []fieldMeta
}

type fieldMeta struct {
	key      string
	index    []int
	isStruct bool
	isDur    bool
}

// Bind maps config values to a target pointer.
// For scalar pointers (*string, *int, *bool, *float64, *time.Duration, *[]string, *map[string]string):
//
//	reads the value at `key` directly into the pointer.
//
// For struct pointers: binds fields using `cfg:"key"` tags (falls back to lowercase field name).
// Returns true if at least one value was bound.
func (c *Config) Bind(key string, target any) bool {
	if target == nil {
		return false
	}

	// Fast path: typed pointer targets
	switch t := target.(type) {
	case *string:
		if v, ok := c.get(key); ok {
			*t = asString(v)
			return true
		}
		return false
	case *int:
		if v, ok := c.get(key); ok {
			if i, err := asInt(v); err == nil {
				*t = i
				return true
			}
		}
		return false
	case *int64:
		if v, ok := c.get(key); ok {
			if i, err := asInt(v); err == nil {
				*t = int64(i)
				return true
			}
		}
		return false
	case *bool:
		if v, ok := c.get(key); ok {
			if b, err := asBool(v); err == nil {
				*t = b
				return true
			}
		}
		return false
	case *float64:
		if v, ok := c.get(key); ok {
			if f, err := asFloat64(v); err == nil {
				*t = f
				return true
			}
		}
		return false
	case *time.Duration:
		if v, ok := c.get(key); ok {
			if d, err := asDuration(v); err == nil {
				*t = d
				return true
			}
		}
		return false
	case *[]string:
		if v, ok := c.get(key); ok {
			if ss, err := asStrings(v); err == nil {
				*t = ss
				return true
			}
		}
		return false
	case *map[string]string:
		if m, ok := c.Map(key); ok {
			*t = m
			return true
		}
		return false
	}

	// Struct binding via reflection
	val := reflect.ValueOf(target)
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Struct {
		return false
	}
	return c.bindStruct(key, val.Elem())
}

func (c *Config) bindStruct(prefix string, sv reflect.Value) bool {
	meta := getStructMeta(sv.Type())
	bound := false

	for _, f := range meta.fields {
		fv := sv.FieldByIndex(f.index)
		fullKey := f.key
		if prefix != "" {
			fullKey = prefix + "." + f.key
		}

		if f.isStruct {
			if c.bindStruct(fullKey, fv) {
				bound = true
			}
			continue
		}

		val, ok := c.get(fullKey)
		if !ok {
			continue
		}
		bound = true

		switch fv.Kind() {
		case reflect.String:
			fv.SetString(asString(val))
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if f.isDur {
				if d, err := asDuration(val); err == nil {
					fv.SetInt(int64(d))
				}
			} else if i, err := asInt(val); err == nil {
				fv.SetInt(int64(i))
			}
		case reflect.Bool:
			if b, err := asBool(val); err == nil {
				fv.SetBool(b)
			}
		case reflect.Float32, reflect.Float64:
			if f2, err := asFloat64(val); err == nil {
				fv.SetFloat(f2)
			}
		case reflect.Slice:
			if fv.Type().Elem().Kind() == reflect.String {
				if ss, err := asStrings(val); err == nil {
					fv.Set(reflect.ValueOf(ss))
				}
			}
		}
	}
	return bound
}

func getStructMeta(typ reflect.Type) *structMeta {
	if cached, ok := structCache.Load(typ); ok {
		return cached.(*structMeta)
	}
	meta := &structMeta{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		key := field.Tag.Get("cfg")
		if key == "-" {
			continue
		}
		if key == "" {
			key = strings.ToLower(field.Name)
		}
		isDur := field.Type == reflect.TypeOf(time.Duration(0))
		isStruct := field.Type.Kind() == reflect.Struct && !isDur
		meta.fields = append(meta.fields, fieldMeta{
			key:      key,
			index:    field.Index,
			isStruct: isStruct,
			isDur:    isDur,
		})
	}
	structCache.Store(typ, meta)
	return meta
}
