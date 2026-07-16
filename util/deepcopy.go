package util

import (
	"encoding/json"
)

// DeepCopy creates a deep copy of the given value using JSON round-trip.
// This is the canonical implementation used across all packages.
// Returns nil if src is nil or if the copy fails.
func DeepCopy[T any](src *T) *T {
	if src == nil {
		return nil
	}
	bytes, err := json.Marshal(src)
	if err != nil {
		return nil
	}
	var dst T
	if err := json.Unmarshal(bytes, &dst); err != nil {
		return nil
	}
	return &dst
}
