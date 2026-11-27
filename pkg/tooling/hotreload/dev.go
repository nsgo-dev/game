//go:build !release

package hotreload

import (
	"os"
	"path/filepath"

	"github.com/nsgo-dev/game/internal/devimage"
	"github.com/nsgo-dev/game/pkg/assets"
	"github.com/nsgo-dev/game/pkg/sprite"
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

// DevScene returns the start scene path `game dev -scene` (or the editor's Run command) passes the game in SceneEnv, or "" when none was requested.
func DevScene() string {
	return os.Getenv(SceneEnv)
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

// WatchAssets reloads m's assets when their files change: it watches every file under the watcher's directory and passes a changed one to assets.Manager.Reload as "asset:<path>" on the engine goroutine, so an atlas's JSON or image, an image or raw bytes that m has loaded show the edit. The watcher's directory must be the one m reads "asset:" URIs from: a Manager made with assets.WithFS(os.DirFS(hotreload.DevAssetsDir())) under `game dev`. Registering reads no file that has not changed in the last two seconds, so a large asset directory does not hold up the game's start; the watcher compares the others' contents from the first change on. onReload, if not nil, is then called with the file's slash-separated path, the names of the assets Reload replaced and its error; files that replaced nothing and failed nothing are skipped.
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

// WatchAtlasFiles watches a sprite atlas's JSON at jsonPath and its image at imagePath, both relative to the watcher's directory, and delivers the atlas to onReload on the engine goroutine when either changes: the JSON parsed with sprite.LoadAtlas over img, which once the image file has changed shows that file's bytes, keeping img's Size and its URI, or for an img with several densities (Scales) naming the file at imagePath as an "asset:" URI so its density is that file's; pass the density the screen shows, such as sprites@2x.png on a 2x screen. Use it for an atlas the game loads itself, such as one embedded with go:embed; WatchAssets covers atlases an assets.Manager loads. If the JSON cannot be read when it is called, onReload gets that error once the image changes.
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
			if img.Scales != 0 || img.Variants != nil {
				// The file at imagePath is one of img's densities: name the source after it so its density stays the file's.
				src.URI = "asset:" + filepath.ToSlash(filepath.Clean(imagePath))
			}
			src = devimage.WithData(src, imageData)
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
