package num

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
)

var (
	flZero = *big.NewFloat(0)

	// NaN == Not a Number
	NaN = NewDecimal(math.NaN())

	// ZERO == 0
	ZERO = NewFromString("0")

	// ONE == 1
	ONE = NewFromString("1")

	// TEN == 10
	TEN = NewFromString("10")

	// MarshalQuoted - can toggle this to true to marshal values as strings
	MarshalQuoted = false
)

// Decimal is the main exported type. It is a simple, immutable wrapper around a *big.Float
type Decimal struct {
	fl *big.Float
}

// NewDecimal creates a new Decimal type from a float value.
func NewDecimal(val float64) Decimal {
	var fl *big.Float

	defer func() {
		if r := recover(); r != nil {
			switch r.(type) {
			case big.ErrNaN:
				fl = nil
			}
		}
	}()

	fl = big.NewFloat(val)

	return Decimal{
		fl: fl,
	}
}

// NewFromString creates a new Decimal type from a string value.
func NewFromString(str string) Decimal {
	bfl := big.NewFloat(0)

	if _, _, err := bfl.Parse(str, 10); err != nil {
		return NaN
	}

	return Decimal{bfl}
}

// NewFromInt creates a new Decimal type from an int value
func NewFromInt(dec int) Decimal {
	return Decimal{big.NewFloat(float64(dec))}
}

// MaxSlice returns the max of a slice of decimals
func MaxSlice(decimals ...Decimal) Decimal {
	if anyNan(decimals...) {
		return NaN
	} else if len(decimals) == 0 {
		return ZERO
	}

	initial := NewFromString("-Inf")

	for _, decimal := range decimals {
		if decimal.GT(initial) {
			initial = decimal
		}
	}

	return initial
}

// MinSlice returns the min of a slice of decimals
func MinSlice(decimals ...Decimal) Decimal {
	if anyNan(decimals...) {
		return NaN
	} else if len(decimals) == 0 {
		return ZERO
	}

	initial := NewFromString("Inf")
	for _, decimal := range decimals {
		if decimal.LT(initial) {
			initial = decimal
		}
	}

	return initial
}

// Add adds a decimal instance to another Decimal instance.
func (d Decimal) Add(addend Decimal) Decimal {
	return nanGuard(func() Decimal {
		return Decimal{d.cpy().Add(d.fl, addend.fl)}
	}, d, addend)
}

// Sub subtracts another decimal instance from this Decimal instance.
func (d Decimal) Sub(subtrahend Decimal) Decimal {
	return nanGuard(func() Decimal {
		return Decimal{d.cpy().Sub(d.fl, subtrahend.fl)}
	}, d, subtrahend)
}

// Mul multiplies another decimal instance with this Decimal instance.
func (d Decimal) Mul(factor Decimal) Decimal {
	return nanGuard(func() Decimal {
		return Decimal{d.cpy().Mul(d.fl, factor.fl)}
	}, d, factor)
}

// Div divides this Decimal by the denominator passed.
func (d Decimal) Div(denominator Decimal) Decimal {
	return nanGuard(func() Decimal {
		return Decimal{d.cpy().Quo(d.fl, denominator.fl)}
	}, d, denominator)
}

// Frac returns another Decimal instance representing this Decimal multiplied by the
// provided float.
func (d Decimal) Frac(f float64) Decimal {
	factor := NewDecimal(f)
	return nanGuard(func() Decimal {
		return d.Mul(factor)
	}, d, factor)
}

// Neg returns this Decimal multiplied by -1.
func (d Decimal) Neg() Decimal {
	return nanGuard(func() Decimal {
		return d.Mul(NewDecimal(-1))
	}, d)
}

// Abs returns the absolute value of this Decimal
func (d Decimal) Abs() Decimal {
	if d.LT(ZERO) {
		return d.Neg()
	}

	return d
}

// Pow returns the decimal to the inputted power.
// For negative exponents returns 1 / d^|exp|; 0^(-n) returns NaN.
func (d Decimal) Pow(exp int) Decimal {
	return nanGuard(func() Decimal {
		if exp == 0 {
			return ONE
		}
		if exp < 0 {
			if d.IsZero() {
				return NaN
			}
			return ONE.Div(d.Pow(-exp))
		}
		// Fast exponentiation by squaring: O(log exp)
		result := ONE
		base := d
		for exp > 0 {
			if exp&1 == 1 {
				result = result.Mul(base)
			}
			exp >>= 1
			if exp > 0 {
				base = base.Mul(base)
			}
		}
		return result
	}, d)
}

// Sign returns -1 for negative, 0 for zero, 1 for positive.
func (d Decimal) Sign() int {
	if d.NaN() {
		return 0
	}
	return d.fl.Sign()
}

// Round returns d rounded to the specified number of decimal places.
// places > 0: keep n decimal places (e.g. Round(2) on 3.14159 → 3.14)
// places = 0: round to integer
// places < 0: round to tens/hundreds/etc (e.g. Round(-2) on 1234 → 1200)
func (d Decimal) Round(places int) Decimal {
	return nanGuard(func() Decimal {
		if places < 0 {
			shift := TEN.Pow(-places)
			return d.Div(shift).Round(0).Mul(shift)
		}
		// Use big.Float Text to perform rounding, then parse back
		s := d.fl.Text('f', places)
		f, _, err := new(big.Float).Parse(s, 10)
		if err != nil {
			return d
		}
		return Decimal{f}
	}, d)
}

// Floor returns the greatest integer value less than or equal to d.
func (d Decimal) Floor() Decimal {
	return nanGuard(func() Decimal {
		i := new(big.Int)
		d.fl.Int(i) // truncates toward zero
		// For negative numbers with fractional part, subtract 1
		if d.fl.Sign() < 0 && !d.fl.IsInt() {
			i.Sub(i, big.NewInt(1))
		}
		return Decimal{new(big.Float).SetInt(i)}
	}, d)
}

// Ceil returns the smallest integer value greater than or equal to d.
func (d Decimal) Ceil() Decimal {
	return nanGuard(func() Decimal {
		i := new(big.Int)
		d.fl.Int(i) // truncates toward zero
		// For positive numbers with fractional part, add 1
		if d.fl.Sign() > 0 && !d.fl.IsInt() {
			i.Add(i, big.NewInt(1))
		}
		return Decimal{new(big.Float).SetInt(i)}
	}, d)
}

// Sqrt returns the decimal's square root
func (d Decimal) Sqrt() Decimal {
	return nanGuard(func() Decimal {
		return Decimal{d.cpy().Sqrt(d.cpy())}
	}, d)
}

// EQ returns true if this Decimal exactly equals the provided decimal.
func (d Decimal) EQ(other Decimal) bool {
	if anyNan(d, other) {
		return false
	}

	return d.Cmp(other) == 0
}

// LT returns true if this decimal is less than the provided decimal.
func (d Decimal) LT(other Decimal) bool {
	if anyNan(d, other) {
		return false
	}

	return d.Cmp(other) < 0
}

// LTE returns true if this decimal is less or equal to the provided decimal.
func (d Decimal) LTE(other Decimal) bool {
	if anyNan(d, other) {
		return false
	}

	return d.Cmp(other) <= 0
}

// GT returns true if this decimal is greater than the provided decimal.
func (d Decimal) GT(other Decimal) bool {
	if anyNan(d, other) {
		return false
	}

	return d.Cmp(other) > 0
}

// GTE returns true if this decimal is greater than or equal to the provided decimal.
func (d Decimal) GTE(other Decimal) bool {
	if anyNan(d, other) {
		return false
	}

	return d.Cmp(other) >= 0
}

// Cmp will return 1 if this decimal is greater than the provided, 0 if they are the same, and -1 if it is less.
func (d Decimal) Cmp(other Decimal) int {
	if anyNan(d, other) {
		return 0
	}

	return d.fl.Cmp(other.fl)
}

// Float will return this Decimal as a float value.
// Note that there may be some loss of precision in this operation.
func (d Decimal) Float() float64 {
	if d.NaN() {
		return math.NaN()
	}

	f, _ := d.fl.Float64()
	return f
}

// Int64 converts to int64 (truncates toward zero). Returns 0 if NaN.
func (d Decimal) Int64() int64 {
	if d.NaN() {
		return 0
	}
	i, _ := d.fl.Int64()
	return i
}

// Zero will return true if this Decimal is equal to 0.
// Deprecated: Use IsZero instead
func (d Decimal) Zero() bool {
	return d.IsZero()
}

// NaN returns true if the underlying is not a valid number
func (d Decimal) NaN() bool {
	return d.fl == nil
}

// IsZero will return true if this Decimal is equal to 0.
func (d Decimal) IsZero() bool {
	if d.NaN() {
		return false
	}

	return d.fl == nil || d.fl.Cmp(&flZero) == 0
}

func (d Decimal) String() string {
	if d.NaN() {
		return "NaN"
	}

	if d.fl == nil {
		d.fl = new(big.Float)
	}

	return d.fl.String()
}

// FormattedString returns the string value of the number to the requested precision
func (d Decimal) FormattedString(places int) string {
	if d.NaN() {
		return d.String()
	}

	format := "%." + fmt.Sprint(places) + "f"
	fl := d.Float()
	return fmt.Sprintf(format, fl)
}

// MarshalJSON implements the json.Marshaler interface
func (d Decimal) MarshalJSON() ([]byte, error) {
	if d.NaN() {
		return []byte("null"), nil
	}
	if MarshalQuoted {
		return []byte("\"" + d.String() + "\""), nil
	}

	return d.fl.MarshalText()
}

// UnmarshalJSON implements the json.Unmarshaler interface
func (d *Decimal) UnmarshalJSON(b []byte) error {
	if d.fl == nil {
		d.fl = big.NewFloat(0)
	}

	if isQuoted(b) {
		b = b[1 : len(b)-1]
	}

	return d.fl.UnmarshalText(b)
}

func isQuoted(b []byte) bool {
	quoteByte := byte('"')
	return len(b) > 0 && b[0] == quoteByte && b[len(b)-1] == quoteByte
}

// Value implements the sql.Valuer interface
func (d Decimal) Value() (driver.Value, error) {
	return d.String(), nil
}

// Scan implements the sql.Scanner interface
func (d *Decimal) Scan(src interface{}) error {
	switch src := src.(type) {
	case string:
		return json.Unmarshal([]byte(src), d)
	case []byte:
		return json.Unmarshal(src, d)
	default:
		return errors.New(fmt.Sprint("Passed value ", src, " should be a string"))
	}
}

func (d Decimal) cpy() *big.Float {
	cpy := new(big.Float)
	return cpy.Copy(d.fl)
}

func anyNan(decimals ...Decimal) bool {
	for _, decimal := range decimals {
		if decimal.NaN() {
			return true
		}
	}

	return false
}

func nanGuard(yeildFunc func() Decimal, decimals ...Decimal) Decimal {
	if anyNan(decimals...) {
		return NaN
	}

	return yeildFunc()
}
