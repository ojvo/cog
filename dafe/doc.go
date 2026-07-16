// Package dafe provides generic CSV struct mapping utilities:
//
//   - Reader[T] / Iterator[T]: stream CSV rows into typed structs via reflection
//   - Writer[T]:               write typed structs to CSV rows
//   - CSV / Open:              lightweight file-level helpers
//
// The generic reader/writer use reflection to map CSV columns to struct fields
// by name (case-insensitive). Supported field types: string, int/int8/.../int64,
// float32/float64, time.Time, and any type parseable via fmt.Sscan.
package dafe
