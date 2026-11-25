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

// WithDebounce sets how long a changed file must stay stable before its callback runs, preventing half-written reads during rapid saves. When debouncing is enabled (d > 0), empty (0-byte) files wait longer to settle so truncate-then-write editor saves do not deliver a transient empty payload before the content arrives. Negative values are ignored; 0 delivers as soon as a changed hash is read.
func WithDebounce(d time.Duration) Option {
	return func(c *config) {
		if d >= 0 {
			c.debounce = d
		}
	}
}

// AssetsDirEnv is the environment variable `game dev` sets to the absolute path of the directory a game's "asset:" URIs name, its resources directory by default, for NewDevWatcher to watch.
const AssetsDirEnv = "NSGO_GAME_ASSETS"

// SceneEnv is the environment variable `game dev -scene` (and the editor's Run command when starting at the open scene) sets to the slash-separated scene path relative to the project root, for DevScene to read.
const SceneEnv = "NSGO_GAME_SCENE"
