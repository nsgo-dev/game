//go:build !release

package hotreload_test

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tooling/hotreload"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const atlasV1 = `{
	"frames": {
		"idle": {"frame": {"x": 0, "y": 0, "w": 16, "h": 16}, "sourceSize": {"w": 16, "h": 16}}
	},
	"meta": {"scale": "1"}
}`

const atlasV2 = `{
	"frames": {
		"idle": {"frame": {"x": 0, "y": 0, "w": 16, "h": 16}, "sourceSize": {"w": 16, "h": 16}},
		"run":  {"frame": {"x": 16, "y": 0, "w": 16, "h": 16}, "sourceSize": {"w": 16, "h": 16}}
	},
	"meta": {"scale": "1"}
}`

func TestWatchAtlasReloadsOnEngineGoroutineAndRedrawsCanvas(t *testing.T) {
	dir := t.TempDir()
	atlasPath := filepath.Join(dir, "hero.json")
	if err := os.WriteFile(atlasPath, []byte(atlasV1), 0o644); err != nil {
		t.Fatal(err)
	}

	heroImg := core.ImageSource{URI: "asset:hero.png"}
	var (
		watcher     *hotreload.Watcher
		reloaded    atomic.Bool
		lastAtlas   *sprite.Atlas
		lastErr     error
		canvasDraws atomic.Int32
		stateSig    *core.Signal[int]
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		stateSig = core.NewSignal(cx, 0)
		watcher = hotreload.NewWatcher(cx, dir)
		watcher.WatchAtlas("hero.json", heroImg, func(a *sprite.Atlas, err error) {
			// Mutating a signal here verifies the callback runs on the engine goroutine.
			stateSig.Set(stateSig.Get() + 1)
			lastAtlas = a
			lastErr = err
			reloaded.Store(true)
		})
		return core.Canvas{
			Style: core.Style{Width: core.Pt(64), Height: core.Pt(64)},
			Draw: func(d *core.Drawing, _ core.Size) {
				_ = watcher.Generation()
				canvasDraws.Add(1)
			},
		}
	})

	initialDraws := canvasDraws.Load()
	if initialDraws == 0 {
		t.Fatalf("expected initial canvas draw")
	}

	start := time.Now()
	if err := os.WriteFile(atlasPath, []byte(atlasV2), 0o644); err != nil {
		t.Fatal(err)
	}

	app.WaitUntil(time.Second, func() bool {
		return reloaded.Load() && canvasDraws.Load() > initialDraws
	})
	elapsed := time.Since(start)
	if elapsed >= time.Second {
		t.Fatalf("reload took %v, want < 1s", elapsed)
	}
	if lastErr != nil {
		t.Fatalf("unexpected atlas reload error: %v", lastErr)
	}
	if lastAtlas == nil {
		t.Fatalf("reloaded atlas is nil")
	}
	if len(lastAtlas.Frames) != 2 {
		t.Fatalf("got %d frames, want 2", len(lastAtlas.Frames))
	}
	runFrame, ok := lastAtlas.Frames["run"]
	if !ok || runFrame.X != 16 || runFrame.W != 16 {
		t.Fatalf("unexpected run frame: %+v", runFrame)
	}
	var gen int
	app.Post(func() { gen = watcher.Version.Get() })
	app.Settle()
	if gen != 1 {
		t.Fatalf("watcher.Version = %d, want 1", gen)
	}
}

func TestWatchAtlasReportsParseErrorAndRecovers(t *testing.T) {
	dir := t.TempDir()
	atlasPath := filepath.Join(dir, "hero.json")
	if err := os.WriteFile(atlasPath, []byte(atlasV1), 0o644); err != nil {
		t.Fatal(err)
	}

	var (
		gotErr   atomic.Bool
		gotAtlas atomic.Bool
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		w := hotreload.NewWatcher(cx, dir,
			hotreload.WithPollInterval(25*time.Millisecond),
			hotreload.WithDebounce(25*time.Millisecond),
		)
		w.WatchAtlas("hero.json", core.ImageSource{URI: "asset:hero.png"}, func(a *sprite.Atlas, err error) {
			if err != nil {
				gotErr.Store(true)
			} else if a != nil && len(a.Frames) == 2 {
				gotAtlas.Store(true)
			}
		})
		return core.View{}
	})

	if err := os.WriteFile(atlasPath, []byte(`{"frames": [`), 0o644); err != nil {
		t.Fatal(err)
	}
	app.WaitUntil(time.Second, gotErr.Load)

	if err := os.WriteFile(atlasPath, []byte(atlasV2), 0o644); err != nil {
		t.Fatal(err)
	}
	app.WaitUntil(time.Second, gotAtlas.Load)
}

func TestRapidSavesDebounceIntoFinalContent(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "level.txt")
	if err := os.WriteFile(filePath, []byte("v0"), 0o644); err != nil {
		t.Fatal(err)
	}

	var (
		deliveries []string
		done       atomic.Bool
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		w := hotreload.NewWatcher(cx, dir,
			hotreload.WithPollInterval(20*time.Millisecond),
			hotreload.WithDebounce(80*time.Millisecond),
		)
		w.Watch("level.txt", func(data []byte) {
			s := string(data)
			deliveries = append(deliveries, s)
			if s == "final-complete-payload" {
				done.Store(true)
			}
		})
		return core.View{}
	})

	// Simulate rapid partial writes spaced closer than the debounce window.
	writes := []string{"part1", "part1-part2", "part1-part2-part3", "final-complete-payload"}
	for _, content := range writes {
		if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(15 * time.Millisecond)
	}

	app.WaitUntil(time.Second, done.Load)
	if len(deliveries) != 1 || deliveries[0] != "final-complete-payload" {
		t.Fatalf("deliveries = %q, want only final payload", deliveries)
	}
}

func TestUnchangedContentDoesNotTriggerReload(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(filePath, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		w := hotreload.NewWatcher(cx, dir,
			hotreload.WithPollInterval(20*time.Millisecond),
			hotreload.WithDebounce(20*time.Millisecond),
		)
		w.Watch("config.json", func([]byte) {
			calls.Add(1)
		})
		return core.View{}
	})

	// Bump mtime without changing file content.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filePath, future, future); err != nil {
		t.Fatal(err)
	}

	time.Sleep(120 * time.Millisecond)
	app.Settle()
	if calls.Load() != 0 {
		t.Fatalf("got %d reload calls for unchanged content, want 0", calls.Load())
	}
}

func TestWatchPatternTracksModifiedAndCreatedFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	seen := map[string]string{}
	var count atomic.Int32

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		w := hotreload.NewWatcher(cx, dir,
			hotreload.WithPollInterval(20*time.Millisecond),
			hotreload.WithDebounce(20*time.Millisecond),
		)
		w.WatchPattern("json", func(relPath string, data []byte) {
			seen[relPath] = string(data)
			count.Add(1)
		})
		return core.View{}
	})

	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte(`{"v":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "maps")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.json"), []byte(`{"map":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	app.WaitUntil(time.Second, func() bool {
		return count.Load() >= 2
	})

	if seen["a.json"] != `{"v":2}` {
		t.Fatalf("seen[a.json] = %q, want %q", seen["a.json"], `{"v":2}`)
	}
	if seen["maps/b.json"] != `{"map":true}` {
		t.Fatalf("seen[maps/b.json] = %q, want %q", seen["maps/b.json"], `{"map":true}`)
	}
	if _, ok := seen["ignore.txt"]; ok {
		t.Fatalf("unexpected callback for non-matching extension ignore.txt")
	}
}

func TestDisposingScopeStopsWatcher(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "hero.json")
	if err := os.WriteFile(filePath, []byte(atlasV1), 0o644); err != nil {
		t.Fatal(err)
	}

	var (
		shown *core.Signal[bool]
		calls atomic.Int32
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shown = core.NewSignal(cx, true)
		return core.Show{
			When: shown.Get,
			Then: func(cx *core.Scope) core.Node {
				w := hotreload.NewWatcher(cx, dir,
					hotreload.WithPollInterval(20*time.Millisecond),
					hotreload.WithDebounce(20*time.Millisecond),
				)
				w.Watch("hero.json", func([]byte) {
					calls.Add(1)
				})
				return core.View{}
			},
		}
	})

	// Dispose the child scope.
	app.Post(func() { shown.Set(false) })
	app.Settle()

	if err := os.WriteFile(filePath, []byte(atlasV2), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond)
	app.Settle()

	if calls.Load() != 0 {
		t.Fatalf("got %d callbacks after scope disposal, want 0", calls.Load())
	}
}

func TestSameLengthWriteWithinTimestampGranularityIsSeen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	if err := os.WriteFile(path, []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	var got atomic.Value
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		w := hotreload.NewWatcher(cx, dir,
			hotreload.WithPollInterval(20*time.Millisecond),
			hotreload.WithDebounce(20*time.Millisecond),
		)
		w.Watch("a.json", func(data []byte) { got.Store(string(data)) })
		return core.View{}
	})

	// The same length within the file system's timestamp granularity: size and modification time both stay as they were.
	if err := os.WriteFile(path, []byte(`{"v":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}

	app.WaitUntil(time.Second, func() bool {
		v, _ := got.Load().(string)
		return v == `{"v":2}`
	})
}
