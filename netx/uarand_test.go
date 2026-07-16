package netx

import (
	"strings"
	"testing"
)

func TestGetRandomUserAgent(t *testing.T) {
	// Should return a non-empty string from the default list.
	ua := GetRandomUserAgent()
	if ua == "" {
		t.Error("GetRandomUserAgent returned empty string")
	}
	// Should be one of the defaultUserAgents entries.
	found := false
	for _, candidate := range defaultUserAgents {
		if ua == candidate {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("GetRandomUserAgent returned unknown UA: %q", ua)
	}
}

func TestGetRandomUserAgent_Distribution(t *testing.T) {
	// Call GetRandomUserAgent many times and verify we get at least 2
	// distinct user agents (statistical sanity check).
	seen := make(map[string]int)
	const n = 200
	for i := 0; i < n; i++ {
		ua := GetRandomUserAgent()
		seen[ua]++
	}
	if len(seen) < 2 {
		t.Errorf("expected at least 2 distinct UAs over %d calls, got %d", n, len(seen))
	}
}

func TestNewUserAgentRandomizer(t *testing.T) {
	// Create a randomizer with the default list.
	r := NewUserAgentRandomizer(NewTestRandomizer())
	ua := r.GetRandom()
	if ua == "" {
		t.Error("GetRandom returned empty string")
	}
}

func TestNewUserAgentRandomizerWithList(t *testing.T) {
	customList := []string{"CustomBot/1.0", "CustomBot/2.0"}
	r := NewUserAgentRandomizerWithList(customList)
	ua := r.GetRandom()
	if ua != "CustomBot/1.0" && ua != "CustomBot/2.0" {
		t.Errorf("GetRandom returned %q, want one of custom list", ua)
	}
}

func TestDefaultUserAgents_NonEmpty(t *testing.T) {
	if len(defaultUserAgents) == 0 {
		t.Error("defaultUserAgents should not be empty")
	}
}

func TestDefaultUserAgents_ContainsCommonBrowsers(t *testing.T) {
	// The 141KB list should contain at least some common browser markers.
	joined := strings.Join(defaultUserAgents, " ")
	for _, marker := range []string{"Mozilla", "Chrome", "Safari"} {
		if !strings.Contains(joined, marker) {
			t.Errorf("defaultUserAgents should contain %q", marker)
		}
	}
}

// NewTestRandomizer creates a deterministic Randomizer for testing.
type testRandomizer struct {
	vals []int
	idx  int
}

func NewTestRandomizer() *testRandomizer {
	return &testRandomizer{vals: []int{0}}
}

func (t *testRandomizer) Seed(_ int64) {}

func (t *testRandomizer) Intn(n int) int {
	if len(t.vals) == 0 {
		return 0
	}
	v := t.vals[t.idx%len(t.vals)]
	t.idx++
	if v >= n {
		return n - 1
	}
	return v
}
