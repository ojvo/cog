package util

import "sort"

// SortedKeys returns the keys of a set in ascending order. It exists so callers
// that need deterministic iteration over a set never re-implement the
// collect-and-sort dance (or forget the sort).
func SortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
