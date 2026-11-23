//go:build !release

package hotreload_test

import (
	"fmt"
	"os"

	"github.com/nsgo-dev/game/packages/game/tooling/hotreload"
	"github.com/nsgo-dev/nsgo/core"
)

// NewWatcher starts polling an asset directory during development; Watch and WatchPattern register callbacks that run on the engine goroutine when a watched file changes, so a running game picks up edited levels or sprites without a restart. The watcher polls on wall time in the background, so this example shows only the setup: nothing has been reloaded yet and Generation is 0.
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
