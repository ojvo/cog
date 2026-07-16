package netx

import (
	"math/rand"
	"sync"
	"time"
)

// Randomizer provides entropy for UserAgentRandomizer. Implementations must
// be safe for concurrent use by callers that do not synchronize access; the
// default *rand.Rand is NOT safe, so UserAgentRandomizer guards it with a
// mutex.
type Randomizer interface {
	Seed(n int64)
	Intn(n int) int
}

// UserAgentRandomizer holds a list of user-agent strings and a random source
// for concurrent-safe random selection.
//
// The zero value is not usable; construct via NewUserAgentRandomizer or
// NewUserAgentRandomizerWithList.
type UserAgentRandomizer struct {
	rng        Randomizer
	userAgents []string
	mu         sync.Mutex
}

// defaultUA is the package-level randomizer backed by the built-in
// defaultUserAgents list.
var defaultUA = NewUserAgentRandomizer(rand.New(rand.NewSource(time.Now().UnixNano())))

// NewUserAgentRandomizer creates a UserAgentRandomizer using the built-in
// defaultUserAgents list and the provided random source.
func NewUserAgentRandomizer(r Randomizer) *UserAgentRandomizer {
	return &UserAgentRandomizer{
		rng:        r,
		userAgents: defaultUserAgents,
	}
}

// NewUserAgentRandomizerWithList creates a UserAgentRandomizer with a custom
// user-agent list. A new random source seeded by the current time is used.
func NewUserAgentRandomizerWithList(userAgents []string) *UserAgentRandomizer {
	return &UserAgentRandomizer{
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
		userAgents: userAgents,
	}
}

// GetRandom returns a random user-agent string from the list. Safe for
// concurrent use.
func (u *UserAgentRandomizer) GetRandom() string {
	u.mu.Lock()
	n := u.rng.Intn(len(u.userAgents))
	u.mu.Unlock()
	return u.userAgents[n]
}

// GetRandomUserAgent returns a random user-agent from the default list. It
// is the package-level convenience wrapper around the default randomizer.
func GetRandomUserAgent() string {
	return defaultUA.GetRandom()
}
