package csv

import "fmt"

// Tag-definition / decode errors. They wrap an underlying sentinel error so
// callers can use errors.Is to classify failures while still getting the
// structural context (which tag, which field, which line).

// Sentinel errors returned by tag-based decoding.
var (
	// ErrorMissingCustomSetter is returned when a field opts into
	// useCustomSetter but the target struct does not implement CustomSetter.
	ErrorMissingCustomSetter = fmt.Errorf("cannot use custom data type without implementing CustomSetter interface")
	// ErrorUnsupportedDataType is returned when a field's type is not
	// supported by the built-in setter and the struct does not implement
	// CustomSetter either.
	ErrorUnsupportedDataType = fmt.Errorf("must implement CustomSetter interface when using unsupported data types")
	// ErrorInvalidIndex is returned when an "index" tag attribute is not a
	// non-negative integer.
	ErrorInvalidIndex = fmt.Errorf("index must be a non negative integer")
	// ErrorMalformedCsvTag is returned when a csv tag lacks both the header
	// and the index attribute.
	ErrorMalformedCsvTag = fmt.Errorf("you need to specify either the header or index")
	// ErrorUnexportedField is returned when a csv tag is placed on an
	// unexported struct field (which cannot be set via reflection).
	ErrorUnexportedField = fmt.Errorf("csv tags may not be set on unexported fields")
	// ErrorFieldNotFound is returned when a field referenced via a csv tag is
	// not present in the CSV header.
	ErrorFieldNotFound = fmt.Errorf("field not found in header")
	// ErrorMissingRequiredColumn is returned when a column declared as
	// required via WithCheck is absent from the CSV header.
	ErrorMissingRequiredColumn = fmt.Errorf("required column missing from header")
)

// CsvTagDefError wraps a malformed csv struct-tag definition.
type CsvTagDefError struct {
	CsvTag    string
	FieldName string
	Err       error
}

func (e CsvTagDefError) Error() string {
	return fmt.Sprintf("problem with csv tag definition %q on field %s: %v", e.CsvTag, e.FieldName, e.Err)
}

func (e CsvTagDefError) Unwrap() error { return e.Err }

// FieldNotFoundError reports that a field's csv tag referenced a header that
// is not present in the parsed CSV.
type FieldNotFoundError struct {
	FieldName  string
	HeaderName string
	Err        error
}

func (e FieldNotFoundError) Error() string {
	return fmt.Sprintf("field %s not found in header with label %q", e.FieldName, e.HeaderName)
}

func (e FieldNotFoundError) Unwrap() error { return e.Err }

// SetValueError wraps a value-conversion failure for a specific CSV cell.
type SetValueError struct {
	Line      int
	Value     string
	FieldName string
	Err       error
}

func (e SetValueError) Error() string {
	return fmt.Sprintf("record on line %d: problem setting value %q on field %s: %v", e.Line, e.Value, e.FieldName, e.Err)
}

func (e SetValueError) Unwrap() error { return e.Err }
