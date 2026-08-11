package csv

import (
	"encoding/csv"
	"os"
	"reflect"
	"testing"
)

func TestOpen(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "example.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	content := "name,age\nAlice,30\nBob,25"
	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	rows, err := Open(tmpfile.Name())
	if err != nil {
		t.Fatal(err)
	}

	expected := [][]string{
		{"name", "age"},
		{"Alice", "30"},
		{"Bob", "25"},
	}

	if !reflect.DeepEqual(rows, expected) {
		t.Errorf("expected %v, got %v", expected, rows)
	}
}

func TestOpen_WithComments(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "comments.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	content := "# this is a comment\nname,age\nAlice,30"
	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	rows, err := Open(tmpfile.Name())
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) != 2 {
		t.Errorf("expected 2 rows (comment skipped), got %d", len(rows))
	}
}

func TestCSV_Write(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "write_test.csv")
	if err != nil {
		t.Fatal(err)
	}
	name := tmpfile.Name()
	tmpfile.Close()
	defer os.Remove(name)

	c, err := NewCSV(name)
	if err != nil {
		t.Fatal(err)
	}

	data := [][]string{
		{"id", "value"},
		{"1", "test"},
		{"2", "quoted,value"},
	}

	if err := c.WriteAll(data); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	readRows, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(readRows, data) {
		t.Errorf("expected %v, got %v", data, readRows)
	}
}

func TestGenericWriter(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "generic_write.csv")
	if err != nil {
		t.Fatal(err)
	}
	name := tmpfile.Name()
	tmpfile.Close()
	defer os.Remove(name)

	c, err := NewCSV(name)
	if err != nil {
		t.Fatal(err)
	}

	type User struct {
		Name string
		Age  int
	}

	gw := NewWriter[User](c.Writer, []string{"Name", "Age"})

	c.Write([]string{"Name", "Age"})

	if err := gw.Write(User{Name: "Alice", Age: 30}); err != nil {
		t.Fatal(err)
	}
	if err := gw.Write(User{Name: "Bob", Age: 25}); err != nil {
		t.Fatal(err)
	}
	c.Close()

	rows, err := Open(name)
	if err != nil {
		t.Fatal(err)
	}

	expected := [][]string{
		{"Name", "Age"},
		{"Alice", "30"},
		{"Bob", "25"},
	}

	if !reflect.DeepEqual(rows, expected) {
		t.Errorf("expected %v, got %v", expected, rows)
	}
}

func TestGenericWriter_NoHeaders(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "generic_noheaders.csv")
	if err != nil {
		t.Fatal(err)
	}
	name := tmpfile.Name()
	tmpfile.Close()
	defer os.Remove(name)

	c, err := NewCSV(name)
	if err != nil {
		t.Fatal(err)
	}

	type Point struct {
		X float64
		Y float64
	}

	gw := NewWriter[Point](c.Writer, nil)

	if err := gw.Write(Point{X: 1.5, Y: 2.5}); err != nil {
		t.Fatal(err)
	}
	c.Close()

	rows, err := Open(name)
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0][0] != "1.5" || rows[0][1] != "2.5" {
		t.Errorf("expected [1.5, 2.5], got %v", rows[0])
	}
}
