package util

import (
	"reflect"
	"time"
	"unsafe"
)

// DeepCopy creates a deep copy of src using reflection.
//
// Unlike the previous JSON round-trip approach, this handles unexported
// fields and preserves cyclic references without infinite recursion.
// Types implementing interface{ DeepCopy() any } are delegated to their
// custom copy method.
//
// Complexity is O(n) in the number of reachable values.
func DeepCopy[T any](src *T) *T {
	if src == nil {
		return nil
	}
	dst := reflect.New(reflect.TypeOf(src).Elem()).Elem()

	visited := make(map[unsafe.Pointer]reflect.Value)
	// Register the root so that cycles back to it are handled.
	visited[unsafe.Pointer(reflect.ValueOf(src).Pointer())] = dst.Addr()

	v := copyValue(reflect.ValueOf(src).Elem(), visited)
	dst.Set(v)
	return dst.Addr().Interface().(*T)
}

// copyValue performs a deep copy of a value. visited maps original
// pointers to their already-constructed copies, enabling cycle detection.
func copyValue(src reflect.Value, visited map[unsafe.Pointer]reflect.Value) reflect.Value {
	if !src.IsValid() {
		return src
	}

	// Custom DeepCopy interface (same as source deepcopy.go).
	if src.CanInterface() {
		if copier, ok := src.Interface().(interface{ DeepCopy() any }); ok {
			return reflect.ValueOf(copier.DeepCopy())
		}
	}

	switch src.Kind() {
	case reflect.Ptr:
		return copyPtr(src, visited)
	case reflect.Interface:
		if src.IsNil() {
			return reflect.Zero(src.Type())
		}
		return copyValue(src.Elem(), visited)
	case reflect.Struct:
		return copyStruct(src, visited)
	case reflect.Slice:
		if src.IsNil() {
			return reflect.Zero(src.Type())
		}
		dst := reflect.MakeSlice(src.Type(), src.Len(), src.Cap())
		for i := 0; i < src.Len(); i++ {
			dst.Index(i).Set(copyValue(src.Index(i), visited))
		}
		return dst
	case reflect.Map:
		if src.IsNil() {
			return reflect.Zero(src.Type())
		}
		dst := reflect.MakeMap(src.Type())
		for _, key := range src.MapKeys() {
			copiedKey := copyValue(key, visited)
			copiedVal := copyValue(src.MapIndex(key), visited)
			dst.SetMapIndex(copiedKey, copiedVal)
		}
		return dst
	case reflect.Array:
		dst := reflect.New(src.Type()).Elem()
		for i := 0; i < src.Len(); i++ {
			dst.Index(i).Set(copyValue(src.Index(i), visited))
		}
		return dst
	default:
		dst := reflect.New(src.Type()).Elem()
		dst.Set(src)
		return dst
	}
}

func copyPtr(src reflect.Value, visited map[unsafe.Pointer]reflect.Value) reflect.Value {
	if src.IsNil() {
		return reflect.Zero(src.Type())
	}

	key := unsafe.Pointer(src.Pointer())

	if existing, ok := visited[key]; ok {
		return existing
	}

	// Allocate the copy first, register it, then recurse.
	dst := reflect.New(src.Type().Elem())
	visited[key] = dst
	dst.Elem().Set(copyValue(src.Elem(), visited))
	return dst
}

func copyStruct(src reflect.Value, visited map[unsafe.Pointer]reflect.Value) reflect.Value {
	// time.Time has internal mutex; copy by value.
	if src.Type() == reflect.TypeOf(time.Time{}) {
		t := src.Interface().(time.Time)
		return reflect.ValueOf(t)
	}

	dst := reflect.New(src.Type()).Elem()
	for i := 0; i < src.NumField(); i++ {
		if src.Type().Field(i).PkgPath != "" {
			field := dst.Field(i)
			if field.CanAddr() {
				p := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr()))
				p.Elem().Set(src.Field(i))
			}
			continue
		}
		dst.Field(i).Set(copyValue(src.Field(i), visited))
	}
	return dst
}
