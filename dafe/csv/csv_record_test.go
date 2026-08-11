package csv

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewPipeReader_ReadsPipeSeparated(t *testing.T) {
	data := bytes.NewBufferString("id|name|age\n1|Alice|30\n2|Bob|25")
	records, err := ReadPipeSeparatedLines(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0]["name"] != "Alice" {
		t.Errorf("expected name 'Alice', got %q", records[0]["name"])
	}
	if records[1]["age"] != "25" {
		t.Errorf("expected age '25', got %q", records[1]["age"])
	}
}

func TestReadRecordsFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "psv.txt")
	content := "k1|k2\nv1|v2\n"
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	records, err := ReadRecordsFromFile(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0]["k1"] != "v1" || records[0]["k2"] != "v2" {
		t.Errorf("unexpected record %v", records[0])
	}
}

func TestReadRecordsFromFile_MissingFile(t *testing.T) {
	_, err := ReadRecordsFromFile(filepath.Join(t.TempDir(), "nope.txt"))
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestNewRecord_TooManyValues(t *testing.T) {
	keys := []string{"a", "b"}
	values := []string{"1", "2", "3"}
	_, err := NewRecord(keys, values)
	if err == nil {
		t.Error("expected error when values exceed keys")
	}
}

func TestNewRecord_FewerValues(t *testing.T) {
	keys := []string{"a", "b", "c"}
	values := []string{"1", "2"}
	rec, err := NewRecord(keys, values)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec) != 2 {
		t.Fatalf("expected 2 keys populated, got %d", len(rec))
	}
	if _, ok := rec["c"]; ok {
		t.Error("missing column 'c' should not be in record")
	}
}

func TestNewRecord_TrimsWhitespace(t *testing.T) {
	keys := []string{"name", "age"}
	values := []string{"  Alice  ", " 30 "}
	rec, err := NewRecord(keys, values)
	if err != nil {
		t.Fatal(err)
	}
	if rec["name"] != "Alice" {
		t.Errorf("expected trimmed 'Alice', got %q", rec["name"])
	}
	if rec["age"] != "30" {
		t.Errorf("expected trimmed '30', got %q", rec["age"])
	}
}

func TestUnmarshal_Primitives(t *testing.T) {
	rec := Record{
		"Name":    "Alice",
		"Age":     "30",
		"Score":   "95.5",
		"Active":  "true",
		"Count":   "7",
		"Unknown": "ignored", // extra column should be skipped silently
	}

	type Person struct {
		Name   string
		Age    int
		Score  float64
		Active bool
		Count  uint
	}
	var p Person
	if err := Unmarshal(rec, &p); err != nil {
		t.Fatal(err)
	}
	if p.Name != "Alice" {
		t.Errorf("Name = %q", p.Name)
	}
	if p.Age != 30 {
		t.Errorf("Age = %d", p.Age)
	}
	if p.Score != 95.5 {
		t.Errorf("Score = %v", p.Score)
	}
	if !p.Active {
		t.Errorf("Active = %v", p.Active)
	}
	if p.Count != 7 {
		t.Errorf("Count = %d", p.Count)
	}
}

func TestUnmarshal_DateFmt(t *testing.T) {
	rec := Record{"When": "2023-05-15 14:30:00"}
	type Event struct {
		When time.Time `datefmt:"2006-01-02 15:04:05"`
	}
	var e Event
	if err := Unmarshal(rec, &e); err != nil {
		t.Fatal(err)
	}
	if e.When.Year() != 2023 || e.When.Month() != time.May || e.When.Day() != 15 {
		t.Errorf("When = %v", e.When)
	}
}

func TestUnmarshal_DateFmtMissing(t *testing.T) {
	rec := Record{"When": "2023-05-15"}
	type Event struct {
		When time.Time
	}
	var e Event
	err := Unmarshal(rec, &e)
	if err == nil {
		t.Error("expected error: time.Time without datefmt tag")
	}
}

func TestUnmarshal_NonPointerTarget(t *testing.T) {
	type S struct{ X int }
	var s S // not a pointer
	err := Unmarshal(Record{"X": "1"}, s)
	if err == nil {
		t.Error("expected error for non-pointer target")
	}
}

func TestUnmarshal_NilPointer(t *testing.T) {
	type S struct{ X int }
	var s *S
	err := Unmarshal(Record{"X": "1"}, s)
	if err == nil {
		t.Error("expected error for nil pointer")
	}
}

func TestUnmarshal_PointerToSlice(t *testing.T) {
	type S struct{ X int }
	err := Unmarshal(Record{"X": "1"}, &[]S{})
	if err == nil {
		t.Error("expected error for pointer to slice")
	}
}

func TestUnmarshal_BadValue(t *testing.T) {
	rec := Record{"Age": "not-a-number"}
	type P struct{ Age int }
	var p P
	err := Unmarshal(rec, &p)
	if err == nil {
		t.Error("expected error for non-integer age")
	}
}

func TestUnmarshal_DoesNotPanicOnUnknownKind(t *testing.T) {
	//chan field kind is unsupported; ensure we get an error not a panic.
	type C struct {
		Ch chan int
	}
	var c C
	err := Unmarshal(Record{"Ch": "x"}, &c)
	if err == nil {
		t.Error("expected error for unsupported kind chan")
	}
}

func TestRecordsType_Editable(t *testing.T) {
	// Records is a type alias for []Record; ensure slice ops work as expected.
	rs := Records{{"a": "1"}, {"a": "2"}}
	rs = append(rs, Record{"a": "3"})
	if len(rs) != 3 {
		t.Fatalf("len = %d", len(rs))
	}
}

func TestUnmarshal_ErrorWrapping(t *testing.T) {
	// Verify error chain has the field name for diagnostics.
	rec := Record{"Age": "not-a-number"}
	type P struct{ Age int }
	var p P
	err := Unmarshal(rec, &p)
	if err == nil {
		t.Fatal("expected error")
	}
	if !contains(err.Error(), "field Age") {
		t.Errorf("expected error to mention field name; got %v", err)
	}
}

func contains(s, sub string) bool {
	return bytes.Contains([]byte(s), []byte(sub))
}

// Ensure errors.Is works on the typed errors we expose.
func TestTypedErrors_Is(t *testing.T) {
	if !errors.Is(FieldNotFoundError{Err: ErrorFieldNotFound}, ErrorFieldNotFound) {
		t.Error("FieldNotFoundError should unwrap to ErrorFieldNotFound")
	}
	if !errors.Is(CsvTagDefError{Err: ErrorMalformedCsvTag}, ErrorMalformedCsvTag) {
		t.Error("CsvTagDefError should unwrap to ErrorMalformedCsvTag")
	}
	if !errors.Is(SetValueError{Err: ErrorUnsupportedDataType}, ErrorUnsupportedDataType) {
		t.Error("SetValueError should unwrap to ErrorUnsupportedDataType")
	}
}
