package num

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestDecimal_Creation(t *testing.T) {
	d1 := NewDecimal(10.5)
	if d1.Float() != 10.5 {
		t.Errorf("NewDecimal(10.5) = %v, want 10.5", d1)
	}

	d2 := NewFromString("10.5")
	if d2.Float() != 10.5 {
		t.Errorf("NewFromString(\"10.5\") = %v, want 10.5", d2)
	}

	d3 := NewFromInt(10)
	if d3.Float() != 10.0 {
		t.Errorf("NewFromInt(10) = %v, want 10.0", d3)
	}

	d4 := NewFromString("invalid")
	if !d4.NaN() {
		t.Error("NewFromString(\"invalid\") should be NaN")
	}
}

func TestDecimal_Arithmetic(t *testing.T) {
	a := NewFromString("10")
	b := NewFromString("2")

	// Add
	if sum := a.Add(b); !sum.EQ(NewFromString("12")) {
		t.Errorf("10 + 2 = %v, want 12", sum)
	}

	// Sub
	if diff := a.Sub(b); !diff.EQ(NewFromString("8")) {
		t.Errorf("10 - 2 = %v, want 8", diff)
	}

	// Mul
	if prod := a.Mul(b); !prod.EQ(NewFromString("20")) {
		t.Errorf("10 * 2 = %v, want 20", prod)
	}

	// Div
	if quot := a.Div(b); !quot.EQ(NewFromString("5")) {
		t.Errorf("10 / 2 = %v, want 5", quot)
	}

	// Frac
	if frac := a.Frac(0.5); !frac.EQ(NewFromString("5")) {
		t.Errorf("10 * 0.5 = %v, want 5", frac)
	}

	// Neg
	if neg := a.Neg(); !neg.EQ(NewFromString("-10")) {
		t.Errorf("-10 = %v, want -10", neg)
	}

	// Abs
	c := NewFromString("-5")
	if abs := c.Abs(); !abs.EQ(NewFromString("5")) {
		t.Errorf("Abs(-5) = %v, want 5", abs)
	}

	// Pow
	if pow := b.Pow(3); !pow.EQ(NewFromString("8")) {
		t.Errorf("2^3 = %v, want 8", pow)
	}

	// Sqrt
	d := NewFromString("16")
	if sqrt := d.Sqrt(); !sqrt.EQ(NewFromString("4")) {
		t.Errorf("Sqrt(16) = %v, want 4", sqrt)
	}
}

func TestDecimal_Comparison(t *testing.T) {
	a := NewFromString("10")
	b := NewFromString("5")
	c := NewFromString("10")

	if !a.GT(b) {
		t.Error("10 should be GT 5")
	}
	if !a.GTE(b) {
		t.Error("10 should be GTE 5")
	}
	if !a.GTE(c) {
		t.Error("10 should be GTE 10")
	}
	if !b.LT(a) {
		t.Error("5 should be LT 10")
	}
	if !b.LTE(a) {
		t.Error("5 should be LTE 10")
	}
	if !a.EQ(c) {
		t.Error("10 should be EQ 10")
	}
	if a.EQ(b) {
		t.Error("10 should not be EQ 5")
	}
}

func TestDecimal_Aggregation(t *testing.T) {
	d1 := NewFromString("1")
	d2 := NewFromString("5")
	d3 := NewFromString("3")

	if max := MaxSlice(d1, d2, d3); !max.EQ(d2) {
		t.Errorf("MaxSlice = %v, want 5", max)
	}

	if min := MinSlice(d1, d2, d3); !min.EQ(d1) {
		t.Errorf("MinSlice = %v, want 1", min)
	}

	if MaxSlice().IsZero() != true {
		t.Error("MaxSlice() empty should be Zero")
	}

	if MinSlice().IsZero() != true {
		t.Error("MinSlice() empty should be Zero")
	}
}

func TestDecimal_JSON(t *testing.T) {
	type Data struct {
		Amount Decimal `json:"amount"`
	}

	// Test Marshal
	d := Data{Amount: NewFromString("12.34")}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	// Default is not quoted, unless MarshalQuoted is true. Default is false.
	// 12.34 might be represented as number or string depending on big.Float behavior but typically it's number if not quoted.
	// But let's check string content or unmarshal back.

	// Test Unmarshal
	var d2 Data
	if err := json.Unmarshal(b, &d2); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if !d2.Amount.EQ(d.Amount) {
		t.Errorf("JSON roundtrip failed: got %v, want %v", d2.Amount, d.Amount)
	}

	// Test Unmarshal from quoted string
	jsonStr := `{"amount": "56.78"}`
	var d3 Data
	if err := json.Unmarshal([]byte(jsonStr), &d3); err != nil {
		t.Fatalf("Unmarshal quoted failed: %v", err)
	}
	if !d3.Amount.EQ(NewFromString("56.78")) {
		t.Errorf("Unmarshal quoted failed: got %v, want 56.78", d3.Amount)
	}
}

func TestDecimal_NaN(t *testing.T) {
	nan := NewDecimal(math.NaN())
	if !nan.NaN() {
		t.Error("Expected NaN")
	}

	if nan.String() != "NaN" {
		t.Errorf("Expected NaN string, got %s", nan.String())
	}

	// Operations with NaN should result in NaN
	d := NewFromString("10")
	if !d.Add(nan).NaN() {
		t.Error("10 + NaN should be NaN")
	}
	if !nan.Add(d).NaN() {
		t.Error("NaN + 10 should be NaN")
	}

	// Comparison with NaN
	if d.EQ(nan) {
		t.Error("10 == NaN should be false")
	}
	if nan.EQ(nan) {
		t.Error("NaN == NaN should be false") // IEEE 754
	}
}

func TestDecimal_FormattedString(t *testing.T) {
	d := NewFromString("12.3456")
	s := d.FormattedString(2)
	if s != "12.35" { // Rounding behavior of printf %.2f
		t.Errorf("FormattedString(2) = %s, want 12.35", s)
	}
}

func TestDecimal_Sign(t *testing.T) {
	if NewFromString("3.14").Sign() != 1 {
		t.Error("Sign(3.14) should be 1")
	}
	if NewFromString("0").Sign() != 0 {
		t.Error("Sign(0) should be 0")
	}
	if NewFromString("-2.5").Sign() != -1 {
		t.Error("Sign(-2.5) should be -1")
	}
	if NaN.Sign() != 0 {
		t.Error("Sign(NaN) should be 0")
	}
}

func TestDecimal_Round(t *testing.T) {
	cases := []struct {
		input  string
		places int
		want   string
	}{
		{"3.14159", 2, "3.14"},
		{"3.145", 2, "3.15"}, // round half up
		{"3.144", 2, "3.14"},
		{"1234.5678", 0, "1235"},
		{"1234", -2, "1200"}, // round to hundreds
		{"1251", -2, "1300"},
		{"-3.145", 2, "-3.15"}, // negative rounding
	}
	for _, c := range cases {
		got := NewFromString(c.input).Round(c.places).String()
		// big.Float.Text may produce trailing zeros, compare via Float
		gotF := NewFromString(got).Float()
		wantF := NewFromString(c.want).Float()
		if math.Abs(gotF-wantF) > 1e-9 {
			t.Errorf("Round(%s, %d) = %s, want ~%s", c.input, c.places, got, c.want)
		}
	}

	// NaN safety
	if !NaN.Round(2).NaN() {
		t.Error("NaN.Round() should return NaN")
	}
}

func TestDecimal_Floor(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"3.7", "3"},
		{"3.0", "3"},
		{"-3.2", "-4"},
		{"-3.0", "-3"},
		{"0.5", "0"},
		{"-0.5", "-1"},
	}
	for _, c := range cases {
		got := NewFromString(c.input).Floor().String()
		if got != c.want {
			t.Errorf("Floor(%s) = %s, want %s", c.input, got, c.want)
		}
	}

	if !NaN.Floor().NaN() {
		t.Error("NaN.Floor() should return NaN")
	}
}

func TestDecimal_Ceil(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"3.2", "4"},
		{"3.0", "3"},
		{"-3.7", "-3"},
		{"-3.0", "-3"},
		{"0.5", "1"},
		{"-0.5", "0"},
	}
	for _, c := range cases {
		got := NewFromString(c.input).Ceil().String()
		if got != c.want {
			t.Errorf("Ceil(%s) = %s, want %s", c.input, got, c.want)
		}
	}

	if !NaN.Ceil().NaN() {
		t.Error("NaN.Ceil() should return NaN")
	}
}

func TestDecimal_Int64(t *testing.T) {
	cases := []struct {
		input string
		want  int64
	}{
		{"123", 123},
		{"123.99", 123}, // truncates toward zero
		{"-456", -456},
		{"-456.78", -456}, // truncates toward zero
		{"0", 0},
		{"0.9", 0},
		{"-0.9", 0},
	}
	for _, c := range cases {
		got := NewFromString(c.input).Int64()
		if got != c.want {
			t.Errorf("Int64(%s) = %d, want %d", c.input, got, c.want)
		}
	}

	// NaN returns 0
	if NaN.Int64() != 0 {
		t.Errorf("NaN.Int64() = %d, want 0", NaN.Int64())
	}
}

func TestDecimal_Pow(t *testing.T) {
	cases := []struct {
		base string
		exp  int
		want string
	}{
		// Positive exponents (fast exponentiation by squaring)
		{"2", 0, "1"},
		{"2", 1, "2"},
		{"2", 3, "8"},
		{"2", 10, "1024"},
		{"3", 4, "81"},
		{"5", 3, "125"},
		// Negative exponents: d^(-n) = 1 / d^n
		{"2", -1, "0.5"},
		{"2", -2, "0.25"},
		{"10", -1, "0.1"},
		{"4", -3, "0.015625"}, // 1/64
	}
	for _, c := range cases {
		got := NewFromString(c.base).Pow(c.exp)
		want := NewFromString(c.want)
		if !got.EQ(want) {
			t.Errorf("Pow(%s, %d) = %s, want %s", c.base, c.exp, got.String(), c.want)
		}
	}

	// 0^(-n) is undefined → NaN
	if !NewFromString("0").Pow(-1).NaN() {
		t.Error("0^(-1) should be NaN")
	}

	// NaN propagation
	if !NaN.Pow(2).NaN() {
		t.Error("NaN^2 should be NaN")
	}
}

func TestDecimal_NaNGMarshalJSON(t *testing.T) {
	// NaN should marshal to "null" without panic
	b, err := json.Marshal(NaN)
	if err != nil {
		t.Fatalf("Marshal NaN failed: %v", err)
	}
	if string(b) != "null" {
		t.Errorf("Marshal NaN = %s, want null", string(b))
	}

	// Verify in struct
	type Data struct {
		Amount Decimal `json:"amount"`
	}
	b2, err := json.Marshal(Data{Amount: NaN})
	if err != nil {
		t.Fatalf("Marshal struct with NaN failed: %v", err)
	}
	if !strings.Contains(string(b2), "null") {
		t.Errorf("Expected null in output, got %s", string(b2))
	}
}
