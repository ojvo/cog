package csv

import (
	"strings"
	"testing"
)

func TestReadTable_TypeInference(t *testing.T) {
	data := `name,age,active,score
Alice,30,true,95.5
Bob,25,false,87.2
Charlie,35,true,100.0
`
	ct, err := ReadTable(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	if ct.RowCount() != 3 {
		t.Errorf("RowCount: got %d, want 3", ct.RowCount())
	}

	if got := ct.ColumnType("name"); got != TypeString {
		t.Errorf("name type: got %s, want String", got)
	}
	if got := ct.ColumnType("age"); got != TypeInteger {
		t.Errorf("age type: got %s, want Integer", got)
	}
	if got := ct.ColumnType("active"); got != TypeBoolean {
		t.Errorf("active type: got %s, want Boolean", got)
	}
	if got := ct.ColumnType("score"); got != TypeFloat {
		t.Errorf("score type: got %s, want Float", got)
	}
}

func TestReadTable_TypedAccess(t *testing.T) {
	data := `name,age,active,score
Alice,30,true,95.5
Bob,25,false,87.2
`
	ct, err := ReadTable(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	names, ok := ct.StringColumn("name")
	if !ok {
		t.Fatal("StringColumn(name) failed")
	}
	if len(names) != 2 || names[0] != "Alice" || names[1] != "Bob" {
		t.Errorf("names: %v", names)
	}

	ages, ok := ct.IntColumn("age")
	if !ok {
		t.Fatal("IntColumn(age) failed")
	}
	if len(ages) != 2 || ages[0] != 30 || ages[1] != 25 {
		t.Errorf("ages: %v", ages)
	}

	acts, ok := ct.BoolColumn("active")
	if !ok {
		t.Fatal("BoolColumn(active) failed")
	}
	if len(acts) != 2 || acts[0] != true || acts[1] != false {
		t.Errorf("acts: %v", acts)
	}

	scores, ok := ct.FloatColumn("score")
	if !ok {
		t.Fatal("FloatColumn(score) failed")
	}
	if len(scores) != 2 || scores[0] != 95.5 || scores[1] != 87.2 {
		t.Errorf("scores: %v", scores)
	}
}

func TestReadTable_GetValue(t *testing.T) {
	data := `name,age
Alice,30
Bob,25
`
	ct, err := ReadTable(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	v, ok := ct.GetValue(0, "name")
	if !ok || v != "Alice" {
		t.Errorf("GetValue(0,name): %q ok=%v", v, ok)
	}

	v, ok = ct.GetValue(1, "age")
	if !ok || v != "25" {
		t.Errorf("GetValue(1,age): %q ok=%v", v, ok)
	}

	_, ok = ct.GetValue(0, "nonexistent")
	if ok {
		t.Error("GetValue should return false for nonexistent column")
	}

	_, ok = ct.GetValue(99, "name")
	if ok {
		t.Error("GetValue should return false for out-of-range row")
	}
}

func TestReadTable_IterateRows(t *testing.T) {
	data := `name,age
Alice,30
Bob,25
Charlie,35
`
	ct, err := ReadTable(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	count := 0
	ct.IterateRows(func(row int, rec map[string]string) bool {
		count++
		if row == 1 && rec["name"] != "Bob" {
			t.Errorf("row 1 name: %q", rec["name"])
		}
		return row < 1 // stop after 2 rows
	})
	if count != 2 {
		t.Errorf("iterate count: got %d, want 2", count)
	}
}

func TestReadTable_Stats(t *testing.T) {
	data := `name,age,score
Alice,30,95.5
Bob,25,87.2
Charlie,35,100.0
`
	ct, err := ReadTable(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	s := ct.Stats("age")
	if s == nil {
		t.Fatal("Stats(age) returned nil")
	}
	if s.Count != 3 || s.NullCount != 0 {
		t.Errorf("age stats: count=%d null=%d", s.Count, s.NullCount)
	}
	if s.Min != 25 || s.Max != 35 {
		t.Errorf("age stats: min=%v max=%v", s.Min, s.Max)
	}
	if s.Sum != 90 {
		t.Errorf("age stats: sum=%v", s.Sum)
	}
	if s.Mean != 30 {
		t.Errorf("age stats: mean=%v", s.Mean)
	}
	if s.UniqueCount != 3 {
		t.Errorf("age stats: unique=%d", s.UniqueCount)
	}

	// String column stats (no min/max/sum/mean).
	s2 := ct.Stats("name")
	if s2 == nil {
		t.Fatal("Stats(name) returned nil")
	}
	if s2.Count != 3 || s2.UniqueCount != 3 {
		t.Errorf("name stats: count=%d unique=%d", s2.Count, s2.UniqueCount)
	}
}

func TestReadTable_NullValues(t *testing.T) {
	data := `name,age
Alice,30
Bob,
Charlie,35
`
	ct, err := ReadTable(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	s := ct.Stats("age")
	if s.NullCount != 1 {
		t.Errorf("null count: got %d, want 1", s.NullCount)
	}
	if s.Count != 3 {
		t.Errorf("count: got %d, want 3", s.Count)
	}
}

func TestReadTable_TypeDegradation(t *testing.T) {
	// "123" looks like int, but "hello" forces string.
	data := `val
123
hello
`
	ct, err := ReadTable(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if ct.ColumnType("val") != TypeString {
		t.Errorf("val type: got %s, want String", ct.ColumnType("val"))
	}

	// "true"/"false" → bool, but "maybe" → string.
	data2 := `flag
true
maybe
`
	ct2, err := ReadTable(strings.NewReader(data2))
	if err != nil {
		t.Fatal(err)
	}
	if ct2.ColumnType("flag") != TypeString {
		t.Errorf("flag type: got %s, want String", ct2.ColumnType("flag"))
	}
}

func TestReadTable_EmptyFile(t *testing.T) {
	_, err := ReadTable(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestReadTable_ShortRows(t *testing.T) {
	data := `a,b,c
1,2,3
4,5
`
	ct, err := ReadTable(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	v, ok := ct.GetValue(1, "c")
	if !ok || v != "" {
		t.Errorf("padded short row: got %q ok=%v", v, ok)
	}
}
