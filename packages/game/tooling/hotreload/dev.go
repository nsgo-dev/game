//go:build !release

package hotreload

import (
	"os"
	"path/filepath"

	"github.com/nsgo-dev/game/packages/game/assets"
	"github.com/nsgo-dev/game/packages/game/internal/devimage"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// DevAssetsDir returns the asset directory `game dev` passes the game in AssetsDirEnv, cleaned, or "" when the game does not run under `game dev`.
func DevAssetsDir() string {
	dir := os.Getenv(AssetsDirEnv)
	if dir == "" {
		return ""
	}
	return filepath.Clean(dir)
}

// NewDevWatcher starts a Watcher on DevAssetsDir under cx when the game runs under `game dev`, and returns nil otherwise. Every Watcher method does nothing on nil, so a game can call it unconditionally and register what it reloads:
//
//	w := hotreload.NewDevWatcher(cx)
//	w.WatchAssets(mgr, nil)
func NewDevWatcher(cx *core.Scope, opts ...Option) *Watcher {
	dir := DevAssetsDir()
	if dir == "" {
		return nil
	}
	return NewWatcher(cx, dir, opts...)
}

// WatchAssets reloads m's assets when their files change: it watches every file under the watcher's directory and passes a changed one to assets.Manager.Reload as "asset:<path>" on the engine goroutine, so an atlas's JSON or image, an image or raw bytes that m has loaded show the edit. The watcher's directory must be the one m's "asset:" URIs name. onReload, if not nil, is then called with the file's slash-separated path, the names of the assets Reload replaced and its error; files that replaced nothing and failed nothing are skipped.
func (w *Watcher) WatchAssets(m *assets.Manager, onReload func(path string, names []string, err error)) {
	if w == nil || m == nil || w.stopped.Load() {
		return
	}
	w.WatchPattern("", func(relPath string, data []byte) {
		names, err := m.Reload("asset:"+relPath, data)
		if onReload != nil && (len(names) > 0 || err != nil) {
			onReload(relPath, names, err)
		}
	})
}

// WatchAtlasFiles watches a sprite atlas's JSON at jsonPath and its image at imagePath, both relative to the watcher's directory, and delivers the atlas to onReload on the engine goroutine when either changes: the JSON parsed with sprite.LoadAtlas over img, which once the image file has changed shows that file's bytes, keeping img's URI and Size. Use it for an atlas the game loads itself, such as one embedded with go:embed; WatchAssets covers atlases an assets.Manager loads. If the JSON cannot be read when it is called, onReload gets that error once the image changes.
func (w *Watcher) WatchAtlasFiles(jsonPath, imagePath string, img core.ImageSource, onReload func(*sprite.Atlas, error)) {
	if w == nil || onReload == nil || w.stopped.Load() {
		return
	}
	jsonData, jsonErr := os.ReadFile(filepath.Join(w.dir, filepath.Clean(jsonPath)))
	var imageData []byte
	rebuild := func() {
		if jsonErr != nil {
			onReload(nil, jsonErr)
			return
		}
		src := img
		if imageData != nil {
			src = devimage.WithData(img, imageData)
		}
		onReload(sprite.LoadAtlas(src, jsonData))
	}
	w.Watch(jsonPath, func(data []byte) {
		jsonData, jsonErr = data, nil
		rebuild()
	})
	w.Watch(imagePath, func(data []byte) {
		imageData = data
		rebuild()
	})
}
