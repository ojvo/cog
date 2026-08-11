package csv

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

// CsvMarshal may be implemented by a field's type (or its pointer) to take
// full control of converting a CSV cell into the field value. When a field's
// address implements CsvMarshal, setField delegates to FromString and skips
// the built-in type dispatch. This is the reflection-based analogue of
// encoding.TextUnmarshaler.
type CsvMarshal interface {
	FromString(string) error
}

// CustomSetter may be implemented by the target struct (pointer receiver) to
// centralize parsing of fields whose types are not natively supported. When a
// field opts in via the `useCustomSetter` tag attribute, the struct's
// CustomSetter method is invoked with (fieldName, rawCell) instead of
// reflecting into the field directly.
type CustomSetter interface {
	CustomSetter(fieldName string, value string) error
}

// csvTagInfo is the parsed form of a `csv:"..."` struct tag.
//
// Tag syntax (attributes joined by ';'):
//
//	csv:"-"                        skip this field
//	csv:"header_name"              simple form: header to match (lower-cased)
//	csv:"header:Name"              explicit header attribute
//	csv:"index:2"                  explicit column index (headerless match)
//	csv:"header:Name;useCustomSetter"  dispatch to CustomSetter instead of reflect
//
// When neither header nor index is provided, the field name is used (with
// snake_case fallback) for header matching, preserving the legacy behavior.
type csvTagInfo struct {
	headerName      string
	columnIndex     int // -1 means "not set"
	useCustomSetter bool
	skip            bool
}

const (
	csvTagName       = "csv"
	csvAttrDelim     = ";"
	csvValueDelim    = ":"
	csvAttrHeader    = "header"
	csvAttrIndex     = "index"
	csvAttrUseCustom = "useCustomSetter"
	csvAttrSkip      = "-"
	csvIndexNotSet   = -1
)

var (
	customSetterType = reflect.TypeOf((*CustomSetter)(nil)).Elem()
	timeDurationType = reflect.TypeOf(time.Duration(0))
)

// parseCsvTag parses a `csv:"..."` struct tag into a csvTagInfo.
// An empty tag returns an empty info (no header, no index, no skip) which
// causes the caller to fall back to field-name matching.
func parseCsvTag(tag string) csvTagInfo {
	if tag == "" {
		return csvTagInfo{columnIndex: csvIndexNotSet}
	}
	if tag == csvAttrSkip {
		return csvTagInfo{skip: true, columnIndex: csvIndexNotSet}
	}

	info := csvTagInfo{columnIndex: csvIndexNotSet}
	hasHeader := false
	hasIndex := false

	for _, attr := range strings.Split(tag, csvAttrDelim) {
		kv := strings.SplitN(attr, csvValueDelim, 2)
		key := kv[0]
		var value string
		if len(kv) > 1 {
			value = kv[1]
		}
		switch key {
		case csvAttrHeader:
			hasHeader = true
			info.headerName = value
		case csvAttrIndex:
			hasIndex = true
			if value == "" {
				// Treated as malformed; leave columnIndex unset.
				continue
			}
			idx, err := strconv.Atoi(value)
			if err != nil || idx < 0 {
				// Defer error reporting to the caller via columnIndex sentinel.
				// We keep columnIndex = -1 so the field won't match; a missing
				// required field will surface as FieldNotFoundError.
				continue
			}
			info.columnIndex = idx
		case csvAttrUseCustom:
			info.useCustomSetter = true
		case "":
			// empty segment (e.g. trailing ';') - ignore
		default:
			// Simple form: csv:"some_header" - the current segment is a
			// header name. Only adopt if no explicit header/index attribute
			// has been seen, so `csv:"color;useCustomSetter"` parses as
			// header=color + useCustomSetter=true rather than swallowing the
			// whole tag string as a header name.
			if !hasHeader && !hasIndex {
				info.headerName = key
				hasHeader = true
			}
		}
	}
	return info
}

// Reader is a generic CSV reader that maps rows to struct T via reflection.
//
// Column-to-field mapping is resolved per field in the following precedence:
//
//  1. `csv:"header:Name"` attribute (explicit header)
//  2. `csv:"index:N"` attribute (explicit position)
//  3. Field name (case-insensitive)
//  4. Snake-cased field name (case-insensitive)
//
// Fields tagged `csv:"-"` are skipped. Fields without a csv tag fall back to
// field-name matching, preserving backward compatibility.
type Reader[T any] struct {
	reader       *csv.Reader
	headers      []string
	hasHeader    bool
	timeLayout   string
	headerMap    map[string]int
	requiredKeys []string
	typ          reflect.Type
	isPointer    bool
	initialized  bool
}

// ReaderOption configures a Reader.
type ReaderOption[T any] func(*Reader[T])

// WithTimeLayout sets a custom time.Time parse layout.
func WithTimeLayout[T any](layout string) ReaderOption[T] {
	return func(r *Reader[T]) { r.timeLayout = layout }
}

// WithCheck validates that the given column names are present in the CSV
// header during the first Next()/ReadAll() call. Missing columns cause
// initialize to return a FieldNotFoundError wrapping ErrorMissingRequiredColumn.
func WithCheck[T any](keys ...string) ReaderOption[T] {
	return func(r *Reader[T]) { r.requiredKeys = append(r.requiredKeys, keys...) }
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

	// 2. Validate required columns (WithCheck)
	for _, key := range r.requiredKeys {
		if _, ok := r.headerMap[strings.ToLower(key)]; !ok {
			return FieldNotFoundError{
				FieldName:  key,
				HeaderName: key,
				Err:        ErrorMissingRequiredColumn,
			}
		}
	}

	// 3. Cache type info
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

// lookupColumnIndex resolves the column index for a field based on its csv tag.
// Returns (idx, true) when a column is found, (0, false) otherwise.
//
// Lookup precedence:
//  1. tag headerName (lower-cased)
//  2. tag columnIndex (if set)
//  3. field name (lower-cased)
//  4. snake_case(field name) (lower-cased)
func (r *Reader[T]) lookupColumnIndex(field reflect.StructField, info csvTagInfo) (int, bool) {
	if info.headerName != "" {
		if idx, ok := r.headerMap[strings.ToLower(info.headerName)]; ok {
			return idx, true
		}
		return 0, false
	}
	if info.columnIndex >= 0 {
		return info.columnIndex, true
	}
	if idx, ok := r.headerMap[strings.ToLower(field.Name)]; ok {
		return idx, true
	}
	if idx, ok := r.headerMap[strings.ToLower(ToSnake(field.Name, false))]; ok {
		return idx, true
	}
	return 0, false
}

// Iterator provides a streaming interface for reading CSV records.
type Iterator[T any] struct {
	r   *Reader[T]
	val T
	err error
	// line is the 1-based data-row counter (header excluded) for error context.
	line int
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
	it.line++

	valPtr := reflect.New(it.r.typ) // *Struct
	val := valPtr.Elem()            // Struct

	// Cache whether the struct pointer implements CustomSetter so we don't
	// re-check via Implements() on every field.
	supportsCustomSetter := valPtr.Type().Implements(customSetterType)

	for j := 0; j < it.r.typ.NumField(); j++ {
		field := it.r.typ.Field(j)
		fieldVal := val.Field(j)
		if !fieldVal.CanSet() {
			continue
		}

		info := parseCsvTag(field.Tag.Get(csvTagName))
		if info.skip {
			continue
		}

		colIdx, ok := it.r.lookupColumnIndex(field, info)
		if !ok {
			// Field explicitly tagged with header/index is required; missing
			// column surfaces as FieldNotFoundError so callers can detect
			// schema drift instead of silently dropping the value.
			if info.headerName != "" || info.columnIndex >= 0 {
				it.err = FieldNotFoundError{
					FieldName:  field.Name,
					HeaderName: info.headerName,
					Err:        ErrorFieldNotFound,
				}
				return false
			}
			continue
		}
		if colIdx >= len(record) {
			continue
		}

		cellValue := strings.TrimSpace(record[colIdx])
		if cellValue == "" {
			continue
		}

		// CustomSetter takes precedence over both CsvMarshal and the
		// built-in setter; it must be opted-in via the tag.
		if info.useCustomSetter {
			if !supportsCustomSetter {
				it.err = CsvTagDefError{
					CsvTag:    field.Tag.Get(csvTagName),
					FieldName: field.Name,
					Err:       ErrorMissingCustomSetter,
				}
				return false
			}
			method := valPtr.MethodByName("CustomSetter")
			results := method.Call([]reflect.Value{
				reflect.ValueOf(field.Name),
				reflect.ValueOf(cellValue),
			})
			if errVal := results[0]; !errVal.IsNil() {
				it.err = SetValueError{
					Line:      it.line,
					Value:     cellValue,
					FieldName: field.Name,
					Err:       results[0].Interface().(error),
				}
				return false
			}
			continue
		}

		if err := setField(fieldVal, cellValue, it.r.timeLayout); err != nil {
			it.err = SetValueError{
				Line:      it.line,
				Value:     cellValue,
				FieldName: field.Name,
				Err:       err,
			}
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
//
// Resolution order:
//  1. If the field's address implements CsvMarshal, delegate to FromString.
//  2. Otherwise dispatch by reflect.Kind (string/bool/int/uint/float/struct).
//  3. time.Duration fields (Kind == Int64, Type == time.Duration) accept both
//     duration strings ("1h30m") and plain integers.
//  4. Fallback: fmt.Sscan for other types.
func setField(fieldVal reflect.Value, s string, timeLayout string) error {
	// 1. CsvMarshal hook (must be addressable so the pointer can implement it).
	if fieldVal.CanAddr() && fieldVal.Addr().CanInterface() {
		if m, ok := fieldVal.Addr().Interface().(CsvMarshal); ok {
			if err := m.FromString(s); err != nil {
				return fmt.Errorf("FromString: %w", err)
			}
			return nil
		}
	}

	switch fieldVal.Kind() {
	case reflect.String:
		fieldVal.SetString(s)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("parse int %q: %w", s, err)
		}
		fieldVal.SetInt(v)

	case reflect.Int64:
		// time.Duration is an int64; accept duration strings when the field
		// type is time.Duration, otherwise treat as a plain integer.
		if fieldVal.Type() == timeDurationType {
			if d, err := time.ParseDuration(s); err == nil {
				fieldVal.SetInt(int64(d))
				return nil
			}
		}
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
