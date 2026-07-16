package dafe

import (
	"encoding/csv"
	"fmt"
	"os"
	"reflect"
	"strings"
)

// Open reads a CSV file and returns all rows as [][]string.
// Lines starting with '#' are treated as comments.
func Open(path string) ([][]string, error) {
	fn, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fn.Close()

	reader := csv.NewReader(fn)
	reader.Comment = '#'

	return reader.ReadAll()
}

// CSV is a lightweight CSV file writer.
type CSV struct {
	Writer *csv.Writer
	File   *os.File
}

// NewCSV creates a new CSV file for writing.
func NewCSV(name string) (*CSV, error) {
	file, err := os.Create(name)
	if err != nil {
		return nil, err
	}

	return &CSV{
		Writer: csv.NewWriter(file),
		File:   file,
	}, nil
}

// Write writes a single record to the CSV file.
func (c *CSV) Write(record []string) error {
	return c.Writer.Write(record)
}

// WriteAll writes multiple records to the CSV file.
func (c *CSV) WriteAll(records [][]string) error {
	return c.Writer.WriteAll(records)
}

// Flush writes any buffered data to the underlying io.Writer.
func (c *CSV) Flush() {
	c.Writer.Flush()
}

// Close flushes buffered data and closes the file.
// Returns the first error encountered (flush or close).
func (c *CSV) Close() error {
	c.Flush()
	if err := c.Writer.Error(); err != nil {
		c.File.Close()
		return err
	}
	return c.File.Close()
}

// Writer is a generic CSV writer that maps structs to rows via reflection.
type Writer[T any] struct {
	writer  *csv.Writer
	headers []string
	// Cached field index mapping: header name → struct field index
	fieldIdx map[string]int
	typ      reflect.Type
}

// NewWriter creates a new generic Writer.
// If headers is nil, all struct fields are written in declaration order.
func NewWriter[T any](w *csv.Writer, headers []string) *Writer[T] {
	var zero T
	typ := reflect.TypeOf(zero)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	gw := &Writer[T]{
		writer:   w,
		headers:  headers,
		typ:      typ,
		fieldIdx: make(map[string]int),
	}

	if typ.Kind() == reflect.Struct {
		for i := 0; i < typ.NumField(); i++ {
			gw.fieldIdx[strings.ToLower(typ.Field(i).Name)] = i
		}
	}

	return gw
}

// Write writes a struct to the CSV as a single row.
// If headers are set, only those columns are written (in header order);
// otherwise all exported fields are written in declaration order.
func (w *Writer[T]) Write(item T) error {
	val := reflect.ValueOf(item)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return fmt.Errorf("expected struct, got %s", val.Kind())
	}

	var record []string

	if w.headers != nil {
		for _, h := range w.headers {
			if idx, ok := w.fieldIdx[strings.ToLower(h)]; ok && idx < val.NumField() {
				record = append(record, fmt.Sprintf("%v", val.Field(idx).Interface()))
			} else {
				record = append(record, "")
			}
		}
	} else {
		for i := 0; i < val.NumField(); i++ {
			record = append(record, fmt.Sprintf("%v", val.Field(i).Interface()))
		}
	}

	return w.writer.Write(record)
}
