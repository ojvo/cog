package store

import (
	"fmt"
	"testing"
)

func BenchmarkCache_Set(b *testing.B) {
	c := NewCache() // default: no janitor

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Set(fmt.Sprintf("key-%d", i), i, DefaultExpiration)
	}
}

func BenchmarkCache_Get(b *testing.B) {
	c := NewCache()

	for i := 0; i < 10000; i++ {
		c.Set(fmt.Sprintf("key-%d", i), i, DefaultExpiration)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Get(fmt.Sprintf("key-%d", i%10000))
	}
}

func BenchmarkSegmentedWAL_Set(b *testing.B) {
	dir := b.TempDir()
	w, err := NewSegmentedWAL(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer w.Close()

	val := []byte("benchmark-value")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Set(fmt.Sprintf("key-%d", i), val)
	}
}

func BenchmarkSegmentedWAL_Get(b *testing.B) {
	dir := b.TempDir()
	w, err := NewSegmentedWAL(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer w.Close()

	val := []byte("benchmark-value")
	for i := 0; i < 1000; i++ {
		w.Set(fmt.Sprintf("key-%d", i), val)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Get(fmt.Sprintf("key-%d", i%1000))
	}
}
