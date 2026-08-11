package conc

// DrainOption configures Drain.
type DrainOption func(*drainConfig)

type drainConfig struct {
	onExit func()
	onItem func(any)
}

// WithDrainOnExit registers a callback invoked exactly once when the drain
// goroutine finishes (after the channel is closed by the sender).
func WithDrainOnExit(f func()) DrainOption {
	return func(c *drainConfig) { c.onExit = f }
}

// WithDrainHook registers a per-item callback invoked for each drained value,
// for counting/profiling. It must not block on the same channel.
func WithDrainHook(f func(any)) DrainOption {
	return func(c *drainConfig) { c.onItem = f }
}

// Drain runs a goroutine that consumes all values from ch until it is closed,
// then invokes the optional onExit callback. This is the escape hatch for
// "abandoned stream" patterns: the UI stops listening to a channel but the
// producer goroutine keeps sending; without a consumer it blocks once the
// buffer is full and leaks. Drain guarantees the producer can always make
// progress.
//
// Values are discarded; use WithDrainHook if you need to observe them.
// Drain returns immediately; the goroutine terminates on its own once the
// sender closes ch. A never-closed ch leaks the goroutine, which is inherent
// to the channel protocol.
func Drain[T any](ch <-chan T, opts ...DrainOption) {
	var cfg drainConfig
	for _, o := range opts {
		o(&cfg)
	}
	onItem := cfg.onItem
	onExit := cfg.onExit
	go func() {
		if onExit != nil {
			defer onExit()
		}
		if onItem != nil {
			for v := range ch {
				onItem(v)
			}
			return
		}
		for range ch {
		}
	}()
}
