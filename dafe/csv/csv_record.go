package csv

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// PipeSeparator is the default column delimiter for pipe-separated value files.
const PipeSeparator = '|'

// Record is a header→cell view of a single CSV row.
// Keys are the (trimmed) header labels, values are the trimmed cell strings.
type Record = map[string]string

// Records is a slice of Record, the result of reading a whole file.
type Records = []Record

// NewPipeReader returns a csv.Reader configured for pipe-separated input:
// Comma='|', LazyQuotes=true, leading-space trimming disabled. LazyQuotes
// allows a stray quote in a non-quoted field to be tolerated rather than
// failing the whole read, which is desirable for messy real-world PSV data.
func NewPipeReader(r io.Reader) *csv.Reader {
	c := csv.NewReader(r)
	c.Comma = PipeSeparator
	c.LazyQuotes = true
	c.TrimLeadingSpace = false
	return c
}

// ReadRecordsFromFile reads a pipe-separated file and returns its records.
// The first row is treated as the header; trailing/leading whitespace is
// trimmed from header labels and cell values.
func ReadRecordsFromFile(filename string) (Records, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return ReadPipeSeparatedLines(file)
}

// ReadPipeSeparatedLines reads pipe-separated records from r.
// The first row is the header; subsequent rows become Records keyed by the
// (trimmed) header labels. Rows shorter than the header are tolerated and
// only the available cells are populated; rows longer than the header are
// truncated to the header length.
func ReadPipeSeparatedLines(psr io.Reader) (Records, error) {
	r := NewPipeReader(psr)

	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	for i, s := range header {
		header[i] = strings.TrimSpace(s)
	}

	records := make(Records, 0, 8)
	for {
		values, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return records, err
		}
		record, err := NewRecord(header, values)
		if err != nil {
			return records, err
		}
		records = append(records, record)
	}
	return records, nil
}

// NewRecord pairs header keys with a row's values into a Record.
// Returns csv.ErrFieldCount when there are more values than keys; rows with
// fewer values than keys are populated partially (missing keys omitted).
func NewRecord(keys []string, values []string) (Record, error) {
	if len(keys) < len(values) {
		return nil, csv.ErrFieldCount
	}
	record := make(Record, len(values))
	for i, v := range values {
		record[keys[i]] = strings.TrimSpace(v)
	}
	return record, nil
}

// Unmarshal populates target (a pointer to a struct) from a Record.
// Field-to-key matching is case-sensitive and uses the field name as-is,
// matching the original lcsv behavior. A `datefmt:"..."` tag on a time.Time
// field selects the parse layout for that field; otherwise time fields are
// left untouched (no implicit default layout, unlike Reader[T]).
//
// Unsupported kinds are reported via the returned error rather than being
// silently logged, so callers can detect schema mismatches.
func Unmarshal(record Record, target interface{}) error {
	targetVal := reflect.ValueOf(target)
	if targetVal.Kind() != reflect.Ptr || targetVal.IsNil() {
		return fmt.Errorf("Unmarshal: target must be a non-nil pointer")
	}
	elem := targetVal.Elem()
	if elem.Kind() != reflect.Struct {
		return fmt.Errorf("Unmarshal: target must point to a struct, got %s", elem.Kind())
	}
	elemType := elem.Type()

	for f, v := range record {
		fieldVal := elem.FieldByName(f)
		if !fieldVal.IsValid() {
			// Unknown field name - skip silently to tolerate extra columns.
			continue
		}
		if !fieldVal.CanSet() {
			continue
		}

		// datefmt tag is only meaningful on time.Time fields.
		var datefmt string
		if structField, ok := elemType.FieldByName(f); ok {
			datefmt = structField.Tag.Get("datefmt")
		}

		if err := setRecordField(fieldVal, v, datefmt); err != nil {
			return fmt.Errorf("field %s: %w", f, err)
		}
	}
	return nil
}

// setRecordField is the field-level setter for Unmarshal. It supports the
// same primitive kinds as the Reader[T] setter, plus time.Time with an
// explicit datefmt layout. Unsupported kinds return an error so schema
// mismatches surface immediately instead of being silently dropped.
func setRecordField(field reflect.Value, v string, datefmt string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(v)

	case reflect.Uint:
		s, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return fmt.Errorf("parse uint %q: %w", v, err)
		}
		field.SetUint(s)

	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		s, err := strconv.ParseUint(v, 10, field.Type().Bits())
		if err != nil {
			return fmt.Errorf("parse uint %q: %w", v, err)
		}
		field.SetUint(s)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		s, err := strconv.ParseInt(v, 10, field.Type().Bits())
		if err != nil {
			return fmt.Errorf("parse int %q: %w", v, err)
		}
		field.SetInt(s)

	case reflect.Float32, reflect.Float64:
		s, err := strconv.ParseFloat(v, field.Type().Bits())
		if err != nil {
			return fmt.Errorf("parse float %q: %w", v, err)
		}
		field.SetFloat(s)

	case reflect.Bool:
		s, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("parse bool %q: %w", v, err)
		}
		field.SetBool(s)

	case reflect.Struct:
		// time.Time is the only struct kind we decode here.
		if field.Type().PkgPath() == "time" && field.Type().Name() == "Time" {
			if datefmt == "" {
				return fmt.Errorf("time.Time field requires a `datefmt` tag")
			}
			t, err := time.Parse(datefmt, v)
			if err != nil {
				return fmt.Errorf("parse time %q with %q: %w", v, datefmt, err)
			}
			field.Set(reflect.ValueOf(t))
			return nil
		}
		return fmt.Errorf("unsupported struct type %s", field.Type().String())

	default:
		return fmt.Errorf("unsupported field kind %s", field.Kind().String())
	}
	return nil
}
