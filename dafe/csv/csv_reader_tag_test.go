package csv

import (
	"bytes"
	"errors"
	"strconv"
	"testing"
	"time"
)

// --- Tag-based column mapping ----------------------------------------------

func TestTag_SimpleForm(t *testing.T) {
	type User struct {
		Login string `csv:"login"`
		Email string `csv:"email"`
	}
	data := bytes.NewBufferString("login,email\nalice,alice@example.com")
	r := NewReader[User](data, ',', nil)
	users, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if users[0].Login != "alice" || users[0].Email != "alice@example.com" {
		t.Errorf("got %+v", users[0])
	}
}

func TestTag_HeaderAttribute(t *testing.T) {
	type Item struct {
		SKU string `csv:"header:product_sku"`
		Qty int    `csv:"header:quantity"`
	}
	data := bytes.NewBufferString("product_sku,quantity\nABC,5")
	r := NewReader[Item](data, ',', nil)
	items, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if items[0].SKU != "ABC" || items[0].Qty != 5 {
		t.Errorf("got %+v", items[0])
	}
}

func TestTag_IndexAttribute(t *testing.T) {
	// Index-based mapping uses column position; we pass explicit headers so the
	// first row is treated as data, not a header.
	type Point struct {
		X float64 `csv:"index:0"`
		Y float64 `csv:"index:1"`
	}
	data := bytes.NewBufferString("1.5,2.5\n3.0,4.0")
	r := NewReader[Point](data, ',', []string{"x", "y"})
	pts, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 {
		t.Fatalf("expected 2 points, got %d", len(pts))
	}
	if pts[0].X != 1.5 || pts[0].Y != 2.5 {
		t.Errorf("pt0 = %+v", pts[0])
	}
	if pts[1].X != 3.0 || pts[1].Y != 4.0 {
		t.Errorf("pt1 = %+v", pts[1])
	}
}

func TestTag_SkipField(t *testing.T) {
	type Row struct {
		Name string
		Salt string `csv:"-"`
		Age  int
	}
	data := bytes.NewBufferString("name,salt,age\nAlice,ignore,30")
	r := NewReader[Row](data, ',', nil)
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Name != "Alice" {
		t.Errorf("Name = %q", rows[0].Name)
	}
	if rows[0].Salt != "" {
		t.Errorf("Salt should be skipped (empty), got %q", rows[0].Salt)
	}
	if rows[0].Age != 30 {
		t.Errorf("Age = %d", rows[0].Age)
	}
}

func TestTag_MissingTaggedColumnIsError(t *testing.T) {
	type Row struct {
		Name string `csv:"display_name"`
	}
	data := bytes.NewBufferString("not_the_right_header,age\nAlice,30")
	r := NewReader[Row](data, ',', nil)
	_, err := r.ReadAll()
	if err == nil {
		t.Fatal("expected FieldNotFoundError for missing tagged column")
	}
	var fnfe FieldNotFoundError
	if !errors.As(err, &fnfe) {
		t.Errorf("expected FieldNotFoundError, got %T: %v", err, err)
	}
	if !errors.Is(err, ErrorFieldNotFound) {
		t.Errorf("error should wrap ErrorFieldNotFound: %v", err)
	}
}

// --- WithCheck ---------------------------------------------------------------

func TestWithCheck_Passes(t *testing.T) {
	type Row struct {
		Name string
		Age  int
	}
	data := bytes.NewBufferString("name,age\nAlice,30")
	r := NewReader[Row](data, ',', nil, WithCheck[Row]("name", "age"))
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
}

func TestWithCheck_MissingColumn(t *testing.T) {
	type Row struct {
		Name string
		Age  int
	}
	data := bytes.NewBufferString("name,email\nAlice,a@b.c")
	r := NewReader[Row](data, ',', nil, WithCheck[Row]("age"))
	_, err := r.ReadAll()
	if err == nil {
		t.Fatal("expected error for missing required column 'age'")
	}
	if !errors.Is(err, ErrorMissingRequiredColumn) {
		t.Errorf("error should wrap ErrorMissingRequiredColumn: %v", err)
	}
}

// --- Snake_case fallback -----------------------------------------------------

func TestSnakeCaseFallback(t *testing.T) {
	// Struct field "UserName" should match header "user_name".
	type Account struct {
		UserName  string
		UserID    int
		EmailAddr string
	}
	data := bytes.NewBufferString("user_name,user_id,email_addr\nalice,1,a@b.c")
	r := NewReader[Account](data, ',', nil)
	accs, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if accs[0].UserName != "alice" {
		t.Errorf("UserName = %q", accs[0].UserName)
	}
	if accs[0].UserID != 1 {
		t.Errorf("UserID = %d", accs[0].UserID)
	}
	if accs[0].EmailAddr != "a@b.c" {
		t.Errorf("EmailAddr = %q", accs[0].EmailAddr)
	}
}

func TestSnakeCaseFallback_ScreamingHeader(t *testing.T) {
	// SCREAMING_SNAKE header should also match CamelCase field after lowercasing.
	type Account struct {
		UserName string
	}
	data := bytes.NewBufferString("USER_NAME\nalice")
	r := NewReader[Account](data, ',', nil)
	accs, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if accs[0].UserName != "alice" {
		t.Errorf("UserName = %q", accs[0].UserName)
	}
}

// --- CsvMarshal interface ----------------------------------------------------

// money is a custom type that implements CsvMarshal.
type money struct {
	cents int64
}

func (m *money) FromString(s string) error {
	cents, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	m.cents = cents
	return nil
}

func TestCsvMarshal_Interface(t *testing.T) {
	type Order struct {
		Amount money
	}
	data := bytes.NewBufferString("amount\n1234")
	r := NewReader[Order](data, ',', nil)
	orders, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if orders[0].Amount.cents != 1234 {
		t.Errorf("Amount.cents = %d", orders[0].Amount.cents)
	}
}

func TestCsvMarshal_ErrorPropagates(t *testing.T) {
	type Order struct {
		Amount money
	}
	data := bytes.NewBufferString("amount\nnot-a-number")
	r := NewReader[Order](data, ',', nil)
	_, err := r.ReadAll()
	if err == nil {
		t.Fatal("expected error from CsvMarshal FromString failure")
	}
	var sve SetValueError
	if !errors.As(err, &sve) {
		t.Errorf("expected SetValueError, got %T: %v", err, err)
	}
}

// --- CustomSetter interface --------------------------------------------------

// widget implements CustomSetter at the struct level. Two fields opt in via
// the useCustomSetter tag attribute; the dispatch goes through CustomSetter
// instead of reflection.
type widget struct {
	Color int    `csv:"color;useCustomSetter"`
	Code  int    `csv:"code;useCustomSetter"`
	Note  string // populated by CustomSetter, not directly from CSV
}

func (w *widget) CustomSetter(fieldName, value string) error {
	v, err := strconv.Atoi(value)
	if err != nil {
		return err
	}
	switch fieldName {
	case "Color":
		w.Color = v
	case "Code":
		w.Code = v
	default:
		return errors.New("unknown field " + fieldName)
	}
	return nil
}

func TestCustomSetter_Dispatch(t *testing.T) {
	data := bytes.NewBufferString("color,code\n7,42")
	r := NewReader[widget](data, ',', nil)
	items, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 widget, got %d", len(items))
	}
	if items[0].Color != 7 {
		t.Errorf("Color = %d", items[0].Color)
	}
	if items[0].Code != 42 {
		t.Errorf("Code = %d", items[0].Code)
	}
}

func TestCustomSetter_NotImplementedErrors(t *testing.T) {
	type Bad struct {
		Field int `csv:"x;useCustomSetter"`
	}
	data := bytes.NewBufferString("x\n1")
	r := NewReader[Bad](data, ',', nil)
	_, err := r.ReadAll()
	if err == nil {
		t.Fatal("expected CsvTagDefError for missing CustomSetter")
	}
	if !errors.Is(err, ErrorMissingCustomSetter) {
		t.Errorf("error should wrap ErrorMissingCustomSetter: %v", err)
	}
}

func TestCustomSetter_ErrorBecomesSetValueError(t *testing.T) {
	data := bytes.NewBufferString("color,code\nnot-a-number,42")
	r := NewReader[widget](data, ',', nil)
	_, err := r.ReadAll()
	if err == nil {
		t.Fatal("expected error from CustomSetter parsing failure")
	}
	var sve SetValueError
	if !errors.As(err, &sve) {
		t.Errorf("expected SetValueError, got %T: %v", err, err)
	}
	if sve.FieldName != "Color" {
		t.Errorf("expected field 'Color', got %q", sve.FieldName)
	}
}

// --- time.Duration -----------------------------------------------------------

func TestReader_DurationField(t *testing.T) {
	type Job struct {
		Elapsed time.Duration
	}
	data := bytes.NewBufferString("elapsed\n1h30m\n45s")
	r := NewReader[Job](data, ',', nil)
	jobs, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
	if jobs[0].Elapsed != 90*time.Minute {
		t.Errorf("job0 Elapsed = %v", jobs[0].Elapsed)
	}
	if jobs[1].Elapsed != 45*time.Second {
		t.Errorf("job1 Elapsed = %v", jobs[1].Elapsed)
	}
}

func TestReader_PlainInt64NotDuration(t *testing.T) {
	// A non-Duration int64 field should accept plain integers; values that
	// happen to parse as durations (e.g. "1m") must not be misinterpreted.
	type Counter struct {
		N int64
	}
	data := bytes.NewBufferString("n\n123456789")
	r := NewReader[Counter](data, ',', nil)
	cs, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if cs[0].N != 123456789 {
		t.Errorf("N = %d", cs[0].N)
	}
}

// --- Backward compatibility (regression) -------------------------------------

func TestReader_NoTagFallsBackToFieldName(t *testing.T) {
	// Ensure that fields without a csv tag still match by field name.
	type Plain struct {
		Name string
		Age  int
	}
	data := bytes.NewBufferString("name,age\nAlice,30")
	r := NewReader[Plain](data, ',', nil)
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Name != "Alice" || rows[0].Age != 30 {
		t.Errorf("got %+v", rows[0])
	}
}
