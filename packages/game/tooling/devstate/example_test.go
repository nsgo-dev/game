//go:build !release

package devstate_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/packages/game/tooling/devstate"
	"github.com/nsgo-dev/nsgo/core"
)

type levelState struct {
	PlayerX, PlayerY float32
	Coins            int
}

// A Store keeps the running scene's state across a rebuild during development; Restore brings it back only when the state's shape has not changed since it was saved.
func ExampleStore() {
	root := core.NewRoot(core.RootOptions{})
	defer root.Dispose()

	store := devstate.NewStorageStore(root.Storage(), "devstate")
	store.SetLogger(func(string, ...any) {})
	if err := store.Save("level-2", 900, 15*time.Second, levelState{PlayerX: 120, PlayerY: 48, Coins: 7}); err != nil {
		fmt.Println(err)
		return
	}

	// After the rebuild:
	var st levelState
	res, err := store.Restore(&st)
	fmt.Println(err, res.Restored, res.SceneID, res.LoopTick, res.LoopElapsed, st)

	// A changed state type is skipped rather than half restored.
	var other struct{ Lives int }
	res, _ = store.Restore(&other)
	fmt.Println(res.Restored, res.SkipReason != "")
	// Output:
	// <nil> true level-2 900 15s {120 48 7}
	// false true
}
