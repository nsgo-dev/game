// Package hotreload watches game assets on disk during development and delivers reloads on the NSGo engine goroutine without restarting the app.
//
// In release builds (-tags release), Watcher compiles to a zero-cost stub that starts no goroutines and performs no file I/O.
package hotreload

import "time"

// DefaultPollInterval is how often a Watcher checks watched files on disk when no interval is set.
const DefaultPollInterval = 150 * time.Millisecond

// DefaultDebounce is how long a changed file must keep the same metadata and content hash before its reload callback runs.
const DefaultDebounce = 40 * time.Millisecond

// Option configures a Watcher.
type Option func(*config)

type config struct {
	pollInterval time.Duration
	debounce     time.Duration
}

func defaultConfig() config {
	return config{
		pollInterval: DefaultPollInterval,
		debounce:     DefaultDebounce,
	}
}

// WithPollInterval sets how often the watcher checks disk for changes. Non-positive values keep DefaultPollInterval.
func WithPollInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.pollInterval = d
		}
	}
}

// WithDebounce sets how long a changed file must stay stable before its callback runs, preventing half-written reads during rapid saves. Negative values are ignored; 0 delivers as soon as a changed hash is read.
func WithDebounce(d time.Duration) Option {
	return func(c *config) {
		if d >= 0 {
			c.debounce = d
		}
	}
}
