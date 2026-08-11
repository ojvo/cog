package num

import "testing"

func BenchmarkDecimal_Add(b *testing.B) {
	d1 := NewDecimal(123.456)
	d2 := NewDecimal(789.012)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d1.Add(d2)
	}
}

func BenchmarkDecimal_Mul(b *testing.B) {
	d1 := NewDecimal(123.456)
	d2 := NewDecimal(789.012)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d1.Mul(d2)
	}
}

func BenchmarkDecimal_Div(b *testing.B) {
	d1 := NewDecimal(123.456)
	d2 := NewDecimal(789.012)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d1.Div(d2)
	}
}

func BenchmarkDecimal_Round(b *testing.B) {
	d := NewDecimal(123.456789)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Round(2)
	}
}

func BenchmarkDecimal_NewFromString(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewFromString("123.456789")
	}
}

func BenchmarkBytes_Hash(b *testing.B) {
	data := Bytes([]byte("benchmark-data-for-hashing"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data.Sha256()
	}
}

func BenchmarkBytes_Base64(b *testing.B) {
	data := Bytes([]byte("benchmark-data-for-encoding"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data.Base64()
	}
}
