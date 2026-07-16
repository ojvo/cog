package dafe

import (
	"encoding/csv"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// DefaultTimeFormat is the default layout used to parse time.Time fields.
const DefaultTimeFormat = "2006-01-02 15:04:05"

// Reader is a generic CSV reader that maps rows to struct T via reflection.
// Column-to-field mapping is case-insensitive by field name.
type Reader[T any] struct {
	reader      *csv.Reader
	headers     []string
	hasHeader   bool
	timeLayout  string
	headerMap   map[string]int
	typ         reflect.Type
	isPointer   bool
	initialized bool
}

// ReaderOption configures a Reader.
type ReaderOption[T any] func(*Reader[T])

// WithTimeLayout sets a custom time.Time parse layout.
func WithTimeLayout[T any](layout string) ReaderOption[T] {
	return func(r *Reader[T]) { r.timeLayout = layout }
}

// NewReader creates a new Reader.
// If headers is nil, the first row is treated as a header row.
// If headers is provided, those column names are used (no header row is consumed).
func NewReader[T any](r io.Reader, sep rune, headers []string, opts ...ReaderOption[T]) *Reader[T] {
	csvReader := csv.NewReader(r)
	csvReader.Comma = sep
	// Reuse underlying buffer for performance
	csvReader.ReuseRecord = true

	hasHeader := headers == nil

	reader := &Reader[T]{
		reader:     csvReader,
		headers:    headers,
		hasHeader:  hasHeader,
		timeLayout: DefaultTimeFormat,
	}
	for _, opt := range opts {
		opt(reader)
	}
	return reader
}

// initialize reads the header row if needed and caches reflection metadata.
func (r *Reader[T]) initialize() error {
	if r.initialized {
		return nil
	}

	// 1. Build header → column index map
	r.headerMap = make(map[string]int)
	if r.hasHeader {
		record, err := r.reader.Read()
		if err != nil {
			return err
		}
		for i, h := range record {
			r.headerMap[strings.ToLower(h)] = i
		}
	} else if r.headers != nil {
		for i, h := range r.headers {
			r.headerMap[strings.ToLower(h)] = i
		}
	} else {
		return fmt.Errorf("no headers found or provided")
	}

	// 2. Cache type info
	var zero T
	typ := reflect.TypeOf(zero)
	if typ.Kind() == reflect.Ptr {
		r.isPointer = true
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return fmt.Errorf("generic type T must be a struct, got %s", typ.Kind())
	}
	r.typ = typ
	r.initialized = true
	return nil
}

// Iterator provides a streaming interface for reading CSV records.
type Iterator[T any] struct {
	r   *Reader[T]
	val T
	err error
}

// Iterator returns an iterator for streaming records.
func (r *Reader[T]) Iterator() *Iterator[T] {
	return &Iterator[T]{r: r}
}

// Next advances the iterator to the next record.
// Returns false if there are no more records or if an error occurs.
func (it *Iterator[T]) Next() bool {
	if !it.r.initialized {
		if err := it.r.initialize(); err != nil {
			it.err = err
			return false
		}
	}

	record, err := it.r.reader.Read()
	if err != nil {
		if err != io.EOF {
			it.err = err
		}
		return false
	}

	valPtr := reflect.New(it.r.typ) // *Struct
	val := valPtr.Elem()            // Struct

	for j := 0; j < it.r.typ.NumField(); j++ {
		field := it.r.typ.Field(j)
		fieldVal := val.Field(j)
		if !fieldVal.CanSet() {
			continue
		}

		colIdx, ok := it.r.headerMap[strings.ToLower(field.Name)]
		if !ok || colIdx >= len(record) {
			continue
		}

		cellValue := strings.TrimSpace(record[colIdx])
		if cellValue == "" {
			continue
		}

		if err := setField(fieldVal, cellValue, it.r.timeLayout); err != nil {
			it.err = fmt.Errorf("field %s: %w", field.Name, err)
			return false
		}
	}

	if it.r.isPointer {
		it.val = valPtr.Interface().(T)
	} else {
		it.val = val.Interface().(T)
	}

	return true
}

// Row returns the current record.
func (it *Iterator[T]) Row() T {
	return it.val
}

// Err returns the first error encountered during iteration.
func (it *Iterator[T]) Err() error {
	return it.err
}

// ReadAll reads all remaining records.
func (r *Reader[T]) ReadAll() ([]T, error) {
	var result []T
	it := r.Iterator()
	for it.Next() {
		result = append(result, it.Row())
	}
	if it.Err() != nil {
		return nil, it.Err()
	}
	return result, nil
}

// setField sets a reflect.Value from a string cell, dispatching by field kind.
// Uses strconv for numeric parsing (faster than fmt.Sscan).
func setField(fieldVal reflect.Value, s string, timeLayout string) error {
	switch fieldVal.Kind() {
	case reflect.String:
		fieldVal.SetString(s)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("parse int %q: %w", s, err)
		}
		fieldVal.SetInt(v)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return fmt.Errorf("parse uint %q: %w", s, err)
		}
		fieldVal.SetUint(v)

	case reflect.Float32, reflect.Float64:
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("parse float %q: %w", s, err)
		}
		fieldVal.SetFloat(v)

	case reflect.Bool:
		v, err := strconv.ParseBool(s)
		if err != nil {
			return fmt.Errorf("parse bool %q: %w", s, err)
		}
		fieldVal.SetBool(v)

	case reflect.Struct:
		if fieldVal.Type().PkgPath() == "time" && fieldVal.Type().Name() == "Time" {
			t, err := time.Parse(timeLayout, s)
			if err != nil {
				return fmt.Errorf("parse time %q: %w", s, err)
			}
			fieldVal.Set(reflect.ValueOf(t))
		}

	default:
		// Fallback: fmt.Sscan for other types
		if _, err := fmt.Sscan(s, fieldVal.Addr().Interface()); err != nil {
			return fmt.Errorf("scan %q: %w", s, err)
		}
	}
	return nil
}
