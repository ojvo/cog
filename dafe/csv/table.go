package csv

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// ColumnType identifies the inferred data type of a column.
type ColumnType int

const (
	TypeString ColumnType = iota
	TypeInteger
	TypeBoolean
	TypeFloat
)

func (t ColumnType) String() string {
	switch t {
	case TypeString:
		return "String"
	case TypeInteger:
		return "Integer"
	case TypeBoolean:
		return "Boolean"
	case TypeFloat:
		return "Float"
	default:
		return "Unknown"
	}
}

// Table is the read-only interface for tabular CSV data.
type Table interface {
	RowCount() int
	ColumnNames() []string
	GetValue(row int, col string) (string, bool)
	IterateRows(func(row int, record map[string]string) bool)
}

// ColumnTable is a column-oriented Table with typed column access and
// statistics. After loading, each column's type is inferred by scanning all
// values; columns are then stored in typed slices for efficient access.
type ColumnTable struct {
	columns []*columnData
	index   map[string]int // column name → position
	rowCnt  int
}

type columnData struct {
	name   string
	typ    ColumnType
	strCol []string    // always populated (raw strings)
	intCol []int       // populated when typ == TypeInteger
	boolCol []bool     // populated when typ == TypeBoolean
	floatCol []float64 // populated when typ == TypeFloat

	statsCached *ColumnStats
}

// ColumnStats holds aggregate statistics for a numeric column.
type ColumnStats struct {
	Count       int
	NullCount   int
	UniqueCount int
	Min         float64
	Max         float64
	Sum         float64
	Mean        float64
}

// LoadTable reads a CSV file and returns a ColumnTable with inferred column types.
func LoadTable(filename string) (*ColumnTable, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReadTable(f)
}

// ReadTable reads CSV data from r and returns a ColumnTable.
func ReadTable(r io.Reader) (*ColumnTable, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true

	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	for i := range header {
		header[i] = strings.TrimSpace(header[i])
	}

	// Read all rows as strings first.
	var rows [][]string
	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		// Pad short rows.
		if len(row) < len(header) {
			row = append(row, make([]string, len(header)-len(row))...)
		}
		for i := range row {
			row[i] = strings.TrimSpace(row[i])
		}
		rows = append(rows, row)
	}

	// Build columns.
	ct := &ColumnTable{
		columns: make([]*columnData, len(header)),
		index:   make(map[string]int, len(header)),
		rowCnt:  len(rows),
	}
	for i, name := range header {
		col := &columnData{name: name, typ: TypeString}
		col.strCol = make([]string, len(rows))
		for r, row := range rows {
			col.strCol[r] = row[i]
		}
		ct.columns[i] = col
		ct.index[name] = i
	}

	// Infer types and convert.
	for _, col := range ct.columns {
		col.typ, col.intCol, col.boolCol, col.floatCol = inferColumnType(col.strCol)
	}

	return ct, nil
}

// inferColumnType scans all values and returns the most specific type that
// all non-empty values can be parsed as, along with the typed slice (or nil).
func inferColumnType(values []string) (typ ColumnType, ints []int, bools []bool, floats []float64) {
	// Try Integer → Boolean → Float → String.
	// Note: ParseBool accepts "1"/"0"/"true"/"false"/"True"/"False"/"TRUE"/"FALSE"/"t"/"f" etc.
	// We check Boolean before Float so "1"/"0" are treated as bool, not float.

	// 1. Try boolean: all non-empty values must parse as bool.
	isBool := true
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, err := strconv.ParseBool(v); err != nil {
			isBool = false
			break
		}
	}
	if isBool {
		bools = make([]bool, len(values))
		for i, v := range values {
			if v == "" {
				continue
			}
			b, _ := strconv.ParseBool(v)
			bools[i] = b
		}
		return TypeBoolean, nil, bools, nil
	}

	// 2. Try integer.
	isInt := true
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, err := strconv.ParseInt(v, 10, 64); err != nil {
			isInt = false
			break
		}
	}
	if isInt {
		ints = make([]int, len(values))
		for i, v := range values {
			if v == "" {
				continue
			}
			n, _ := strconv.ParseInt(v, 10, 64)
			ints[i] = int(n)
		}
		return TypeInteger, ints, nil, nil
	}

	// 3. Try float.
	isFloat := true
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			isFloat = false
			break
		}
	}
	if isFloat {
		floats = make([]float64, len(values))
		for i, v := range values {
			if v == "" {
				continue
			}
			f, _ := strconv.ParseFloat(v, 64)
			floats[i] = f
		}
		return TypeFloat, nil, nil, floats
	}

	return TypeString, nil, nil, nil
}

// ---- Table interface ----

func (t *ColumnTable) RowCount() int { return t.rowCnt }

func (t *ColumnTable) ColumnNames() []string {
	names := make([]string, len(t.columns))
	for i, c := range t.columns {
		names[i] = c.name
	}
	return names
}

func (t *ColumnTable) GetValue(row int, col string) (string, bool) {
	idx, ok := t.index[col]
	if !ok || row < 0 || row >= t.rowCnt {
		return "", false
	}
	c := t.columns[idx]
	return c.strCol[row], true
}

func (t *ColumnTable) IterateRows(fn func(row int, record map[string]string) bool) {
	for r := 0; r < t.rowCnt; r++ {
		rec := make(map[string]string, len(t.columns))
		for _, c := range t.columns {
			rec[c.name] = c.strCol[r]
		}
		if !fn(r, rec) {
			return
		}
	}
}

// ---- Typed column access ----

func (t *ColumnTable) ColumnType(col string) ColumnType {
	if idx, ok := t.index[col]; ok {
		return t.columns[idx].typ
	}
	return TypeString
}

func (t *ColumnTable) StringColumn(col string) ([]string, bool) {
	idx, ok := t.index[col]
	if !ok {
		return nil, false
	}
	return t.columns[idx].strCol, true
}

func (t *ColumnTable) IntColumn(col string) ([]int, bool) {
	idx, ok := t.index[col]
	if !ok || t.columns[idx].typ != TypeInteger {
		return nil, false
	}
	return t.columns[idx].intCol, true
}

func (t *ColumnTable) BoolColumn(col string) ([]bool, bool) {
	idx, ok := t.index[col]
	if !ok || t.columns[idx].typ != TypeBoolean {
		return nil, false
	}
	return t.columns[idx].boolCol, true
}

func (t *ColumnTable) FloatColumn(col string) ([]float64, bool) {
	idx, ok := t.index[col]
	if !ok || t.columns[idx].typ != TypeFloat {
		return nil, false
	}
	return t.columns[idx].floatCol, true
}

// ---- Statistics ----

// Stats computes and returns statistics for a numeric column.
// Returns nil for non-numeric columns.
func (t *ColumnTable) Stats(col string) *ColumnStats {
	idx, ok := t.index[col]
	if !ok {
		return nil
	}
	c := t.columns[idx]
	if c.statsCached != nil {
		return c.statsCached
	}

	s := &ColumnStats{Count: t.rowCnt}
	unique := make(map[string]struct{})

	switch c.typ {
	case TypeInteger:
		for i, v := range c.intCol {
			if c.strCol[i] == "" {
				s.NullCount++
				continue
			}
			unique[c.strCol[i]] = struct{}{}
			if s.NullCount == i { // first non-null
				s.Min = float64(v)
				s.Max = float64(v)
			}
			f := float64(v)
			if f < s.Min {
				s.Min = f
			}
			if f > s.Max {
				s.Max = f
			}
			s.Sum += f
		}
	case TypeFloat:
		for i, v := range c.floatCol {
			if c.strCol[i] == "" {
				s.NullCount++
				continue
			}
			unique[c.strCol[i]] = struct{}{}
			if s.NullCount == i {
				s.Min = v
				s.Max = v
			}
			if v < s.Min {
				s.Min = v
			}
			if v > s.Max {
				s.Max = v
			}
			s.Sum += v
		}
	default:
		// For string/bool columns, only compute count/unique/null.
		for _, v := range c.strCol {
			if v == "" {
				s.NullCount++
				continue
			}
			unique[v] = struct{}{}
		}
		s.UniqueCount = len(unique)
		c.statsCached = s
		return s
	}

	validCount := s.Count - s.NullCount
	if validCount > 0 {
		s.Mean = s.Sum / float64(validCount)
	}
	s.UniqueCount = len(unique)
	c.statsCached = s
	return s
}
