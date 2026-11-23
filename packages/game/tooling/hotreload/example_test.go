//go:build !release

package hotreload_test

import (
	"fmt"
	"os"

	"github.com/nsgo-dev/game/packages/game/tooling/hotreload"
	"github.com/nsgo-dev/nsgo/core"
)

// A Watcher polls an asset directory during development and calls back on the engine goroutine when a watched file changes, so a running game picks up edited levels or sprites without a restart.
func ExampleNewWatcher() {
	dir, err := os.MkdirTemp("", "assets")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(dir)
	root := core.NewRoot(core.RootOptions{})
	defer root.Dispose()

	w := hotreload.NewWatcher(root, dir)
	defer w.Stop()
	w.Watch("levels/1.yaml", func(data []byte) { fmt.Println("level reloaded:", len(data), "bytes") })
	w.WatchPattern(".png", func(rel string, data []byte) { fmt.Println("image reloaded:", rel) })
	fmt.Println(w.Dir() == dir, w.Generation())
	// Output:
	// true 0
}
