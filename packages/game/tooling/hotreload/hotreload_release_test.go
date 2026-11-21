//go:build release

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

func TestReleaseWatcherIsANoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hero.json")
	if err := os.WriteFile(path, []byte(`{"frames":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var (
		watcher *hotreload.Watcher
		calls   atomic.Int32
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		watcher = hotreload.NewWatcher(cx, dir,
			hotreload.WithPollInterval(10*time.Millisecond),
			hotreload.WithDebounce(10*time.Millisecond),
		)
		watcher.Watch("hero.json", func([]byte) { calls.Add(1) })
		watcher.WatchAtlas("hero.json", core.ImageSource{URI: "asset:hero.png"}, func(*sprite.Atlas, error) { calls.Add(1) })
		watcher.WatchPattern(".json", func(string, []byte) { calls.Add(1) })
		return core.View{}
	})

	if err := os.WriteFile(path, []byte(`{"frames":{"a":{"frame":{"x":0,"y":0,"w":8,"h":8}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	app.Settle()

	if calls.Load() != 0 {
		t.Fatalf("release watcher fired %d callbacks, want 0", calls.Load())
	}
	var gen int
	app.Post(func() { gen = watcher.Generation() })
	app.Settle()
	if gen != 0 {
		t.Fatalf("release watcher generation = %d, want 0", gen)
	}
	watcher.Stop()
}
