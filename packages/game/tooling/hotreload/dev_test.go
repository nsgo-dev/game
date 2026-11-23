//go:build !release

package hotreload_test

import (
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/assets"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tooling/hotreload"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDevWatcherIsNilOutsideGameDev(t *testing.T) {
	t.Setenv(hotreload.AssetsDirEnv, "")
	if dir := hotreload.DevAssetsDir(); dir != "" {
		t.Fatalf("DevAssetsDir = %q, want empty", dir)
	}
	var w *hotreload.Watcher
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		w = hotreload.NewDevWatcher(cx)
		// Registering on the nil watcher does nothing, so games call these unconditionally.
		w.WatchAssets(assets.NewManager(cx), func(string, []string, error) { t.Error("nil watcher reloaded") })
		w.WatchAtlasFiles("a.json", "a.png", core.ImageSource{}, func(*sprite.Atlas, error) { t.Error("nil watcher reloaded") })
		return core.View{}
	})
	if w != nil {
		t.Fatalf("NewDevWatcher = %v, want nil outside game dev", w)
	}
}

func TestDevAssetsDirIsCleaned(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(hotreload.AssetsDirEnv, dir+string(filepath.Separator)+"."+string(filepath.Separator))
	if got := hotreload.DevAssetsDir(); got != dir {
		t.Fatalf("DevAssetsDir = %q, want %q", got, dir)
	}
}

// TestSavingAnAtlasUpdatesTheRunningGameWithinASecond is issue #454's acceptance: under game dev, which sets AssetsDirEnv, rewriting an atlas's JSON and image, as game pack does, updates the atlas the running game's asset manager holds and redraws its canvas in under a second, with the watcher's default timing.
func TestSavingAnAtlasUpdatesTheRunningGameWithinASecond(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sprites"), 0o755); err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(dir, "sprites", "hero.json")
	pngPath := filepath.Join(dir, "sprites", "hero.png")
	writeFile(t, jsonPath, atlasV1)
	writeFile(t, pngPath, "pixels v1")
	t.Setenv(hotreload.AssetsDirEnv, dir)

	var (
		mgr      *assets.Manager
		watcher  *hotreload.Watcher
		reloaded = map[string][]string{}
		draws    atomic.Int32
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithFS(os.DirFS(hotreload.DevAssetsDir())))
		mgr.Group("level").AddAtlas("hero", "asset:sprites/hero.json", core.Src("asset:sprites/hero.png"))
		mgr.LoadGroup("level", func(err error) {
			if err != nil {
				t.Errorf("LoadGroup: %v", err)
			}
		})
		watcher = hotreload.NewDevWatcher(cx)
		watcher.WatchAssets(mgr, func(path string, names []string, err error) {
			if err != nil {
				t.Errorf("reload %s: %v", path, err)
			}
			reloaded[path] = names
		})
		return core.Canvas{
			Style: core.Style{Width: core.Pt(64), Height: core.Pt(64)},
			Draw: func(*core.Drawing, core.Size) {
				_ = mgr.Revision()
				draws.Add(1)
			},
		}
	}, func(a *nsgotest.App) {
		a.ImageSizes = map[string]core.Size{"asset:sprites/hero.png": {Width: 32, Height: 16}}
	})
	if watcher == nil {
		t.Fatal("NewDevWatcher = nil under game dev")
	}
	app.WaitUntil(time.Second, func() bool { return mgr.Group("level").Done() })
	var held *sprite.Atlas
	app.Post(func() { held, _ = mgr.Atlas("hero") })
	app.Settle()
	if held == nil || len(held.Frames) != 1 {
		t.Fatalf("loaded atlas = %+v, want one frame", held)
	}
	initialDraws := draws.Load()

	start := time.Now()
	writeFile(t, jsonPath, atlasV2)
	writeFile(t, pngPath, "pixels v2")
	var frames int
	var image string
	app.WaitUntil(time.Second, func() bool {
		frames, image = len(held.Frames), string(held.Image.Data)
		return frames == 2 && image == "pixels v2" && draws.Load() > initialDraws
	})
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Fatalf("reload took %v, want < 1s", elapsed)
	}
	if run, ok := held.Frames["run"]; !ok || run.X != 16 {
		t.Fatalf("run frame = %+v, want the saved one", run)
	}
	if held.Image.URI != "asset:sprites/hero.png" {
		t.Fatalf("atlas image URI = %q, want it kept", held.Image.URI)
	}
	if !slices.Equal(reloaded["sprites/hero.json"], []string{"hero"}) || !slices.Equal(reloaded["sprites/hero.png"], []string{"hero"}) {
		t.Fatalf("onReload saw %v, want hero for both files", reloaded)
	}
}

func TestWatchAtlasFilesReloadsJSONAndImage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "hero.json"), atlasV1)
	writeFile(t, filepath.Join(dir, "hero.png"), "pixels v1")

	embedded := core.ImageSource{URI: "asset:hero.png", Data: []byte("embedded"), Size: core.Size{Width: 32, Height: 16}}
	var (
		last  atomic.Pointer[sprite.Atlas]
		calls atomic.Int32
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		w := hotreload.NewWatcher(cx, dir,
			hotreload.WithPollInterval(10*time.Millisecond),
			hotreload.WithDebounce(10*time.Millisecond),
		)
		w.WatchAtlasFiles("hero.json", "hero.png", embedded, func(a *sprite.Atlas, err error) {
			if err != nil {
				t.Errorf("WatchAtlasFiles: %v", err)
				return
			}
			last.Store(a)
			calls.Add(1)
		})
		return core.View{}
	})

	// The JSON alone changes: the atlas keeps the image the game gave.
	writeFile(t, filepath.Join(dir, "hero.json"), atlasV2)
	app.WaitUntil(time.Second, func() bool { return calls.Load() == 1 })
	if a := last.Load(); len(a.Frames) != 2 || string(a.Image.Data) != "embedded" {
		t.Fatalf("after JSON save: %d frames, image %q", len(a.Frames), a.Image.Data)
	}

	// Then the image: the atlas draws its bytes with the JSON last read.
	writeFile(t, filepath.Join(dir, "hero.png"), "pixels v2")
	app.WaitUntil(time.Second, func() bool { return calls.Load() == 2 })
	a := last.Load()
	if len(a.Frames) != 2 || string(a.Image.Data) != "pixels v2" || a.Image.URI != embedded.URI || a.Image.Size != embedded.Size {
		t.Fatalf("after image save: %d frames, image %+v", len(a.Frames), a.Image)
	}
}

func TestWatchAtlasFilesReportsMissingJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "hero.png"), "pixels v1")
	var gotErr atomic.Bool
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		w := hotreload.NewWatcher(cx, dir,
			hotreload.WithPollInterval(10*time.Millisecond),
			hotreload.WithDebounce(10*time.Millisecond),
		)
		w.WatchAtlasFiles("hero.json", "hero.png", core.ImageSource{URI: "asset:hero.png"}, func(a *sprite.Atlas, err error) {
			if err != nil && a == nil {
				gotErr.Store(true)
			}
		})
		return core.View{}
	})
	writeFile(t, filepath.Join(dir, "hero.png"), "pixels v2")
	app.WaitUntil(time.Second, gotErr.Load)
}
