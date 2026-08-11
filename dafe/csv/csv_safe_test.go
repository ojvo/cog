package csv

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestCSVWriter_AppendAndRead(t *testing.T) {
	// Setup temporary file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test_csv_append.csv")
	headers := []string{"Name", "Age", "City"}

	// Test NewCSVWriter
	writer, err := NewCSVWriter(tmpFile, headers)
	if err != nil {
		t.Fatalf("NewCSVWriter failed: %v", err)
	}

	// Test AppendRow
	row1 := []string{"Alice", "30", "New York"}
	if err := writer.AppendRow(row1); err != nil {
		t.Errorf("AppendRow failed: %v", err)
	}

	row2 := []string{"Bob", "25", "Los Angeles"}
	if err := writer.AppendRow(row2); err != nil {
		t.Errorf("AppendRow failed: %v", err)
	}

	// Test NewCSVReader
	reader, err := NewCSVReader(tmpFile)
	if err != nil {
		t.Fatalf("NewCSVReader failed: %v", err)
	}

	// Test GetHeaders
	gotHeaders := reader.GetHeaders()
	if !reflect.DeepEqual(gotHeaders, headers) {
		t.Errorf("GetHeaders = %v, want %v", gotHeaders, headers)
	}

	// Test ReadRows
	rows, err := reader.ReadRows()
	if err != nil {
		t.Fatalf("ReadRows failed: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("Expected 2 rows, got %d", len(rows))
	}

	if !reflect.DeepEqual(rows[0], row1) {
		t.Errorf("Row 1 mismatch: got %v, want %v", rows[0], row1)
	}
	if !reflect.DeepEqual(rows[1], row2) {
		t.Errorf("Row 2 mismatch: got %v, want %v", rows[1], row2)
	}
}

func TestCSVWriter_UpdateRow(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test_csv_update.csv")
	headers := []string{"ID", "Value"}

	writer, err := NewCSVWriter(tmpFile, headers)
	if err != nil {
		t.Fatalf("NewCSVWriter failed: %v", err)
	}

	// Add initial data
	writer.AppendRow([]string{"1", "A"})
	writer.AppendRow([]string{"2", "B"})

	// Update existing row (ID=1)
	// Assuming ID is at index 0
	newRow1 := []string{"1", "A_Updated"}
	if err := writer.UpdateRow(0, "1", newRow1); err != nil {
		t.Errorf("UpdateRow existing failed: %v", err)
	}

	// Update non-existing row (ID=3) -> Should append
	newRow3 := []string{"3", "C"}
	if err := writer.UpdateRow(0, "3", newRow3); err != nil {
		t.Errorf("UpdateRow new failed: %v", err)
	}

	// Verify
	reader, err := NewCSVReader(tmpFile)
	if err != nil {
		t.Fatalf("NewCSVReader failed: %v", err)
	}
	rows, err := reader.ReadRows()
	if err != nil {
		t.Fatalf("ReadRows failed: %v", err)
	}

	expectedRows := [][]string{
		{"1", "A_Updated"},
		{"2", "B"},
		{"3", "C"},
	}

	if len(rows) != 3 {
		t.Fatalf("Expected 3 rows, got %d", len(rows))
	}

	for i, row := range rows {
		if !reflect.DeepEqual(row, expectedRows[i]) {
			t.Errorf("Row %d mismatch: got %v, want %v", i, row, expectedRows[i])
		}
	}
}

func TestCSVWriter_Concurrency(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test_csv_concurrency.csv")
	headers := []string{"ID"}

	writer, err := NewCSVWriter(tmpFile, headers)
	if err != nil {
		t.Fatalf("NewCSVWriter failed: %v", err)
	}

	concurrency := 10
	done := make(chan bool)

	for i := 0; i < concurrency; i++ {
		go func(val string) {
			writer.AppendRow([]string{val})
			done <- true
		}(string(rune('A' + i)))
	}

	for i := 0; i < concurrency; i++ {
		<-done
	}

	// Verify count
	reader, err := NewCSVReader(tmpFile)
	if err != nil {
		t.Fatalf("NewCSVReader failed: %v", err)
	}
	rows, err := reader.ReadRows()
	if err != nil {
		t.Fatalf("ReadRows failed: %v", err)
	}

	if len(rows) != concurrency {
		t.Errorf("Expected %d rows, got %d", len(rows), concurrency)
	}
}

func TestCSVReader_FileNotExist(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "non_existent.csv")

	_, err := NewCSVReader(tmpFile)
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}

// TestCSVWriter_UpdateRow_NegativeKeyColumn verifies that UpdateRow rejects
// a negative keyColumn with an error rather than panicking on slice access.
func TestCSVWriter_UpdateRow_NegativeKeyColumn(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test_csv_negkey.csv")
	writer, err := NewCSVWriter(tmpFile, []string{"ID", "V"})
	if err != nil {
		t.Fatalf("NewCSVWriter failed: %v", err)
	}
	err = writer.UpdateRow(-1, "x", []string{"x", "y"})
	if err == nil {
		t.Fatal("UpdateRow with keyColumn=-1 should return error")
	}
}
