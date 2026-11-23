//go:build release

package hotreload

import (
	"github.com/nsgo-dev/game/packages/game/assets"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// Watcher is a zero-cost stub in release builds: it starts no goroutines and performs no disk polling.
type Watcher struct {
	// Version stays 0 in release builds.
	Version *core.Signal[int]
}

// NewWatcher returns a no-op Watcher in release builds.
func NewWatcher(cx *core.Scope, _ string, _ ...Option) *Watcher {
	var ver *core.Signal[int]
	if cx != nil {
		ver = core.NewSignal(cx, 0)
	}
	return &Watcher{Version: ver}
}

// Generation returns 0 in release builds.
func (w *Watcher) Generation() int {
	if w == nil || w.Version == nil {
		return 0
	}
	return w.Version.Get()
}

// Dir returns "" in release builds.
func (w *Watcher) Dir() string {
	return ""
}

// Stop is a no-op in release builds.
func (w *Watcher) Stop() {}

// Watch is a no-op in release builds.
func (w *Watcher) Watch(string, func([]byte)) {}

// WatchAtlas is a no-op in release builds.
func (w *Watcher) WatchAtlas(string, core.ImageSource, func(*sprite.Atlas, error)) {}

// WatchPattern is a no-op in release builds.
func (w *Watcher) WatchPattern(string, func(string, []byte)) {}

// WatchResources is a no-op in release builds.
func (w *Watcher) WatchResources(*resource.Library, func(string, error)) {}

// DevAssetsDir returns "" in release builds, which never run under `game dev`.
func DevAssetsDir() string { return "" }

// NewDevWatcher returns nil in release builds; Watcher's methods do nothing on nil.
func NewDevWatcher(*core.Scope, ...Option) *Watcher { return nil }

// WatchAssets is a no-op in release builds.
func (w *Watcher) WatchAssets(*assets.Manager, func(string, []string, error)) {}

// WatchAtlasFiles is a no-op in release builds.
func (w *Watcher) WatchAtlasFiles(string, string, core.ImageSource, func(*sprite.Atlas, error)) {}
