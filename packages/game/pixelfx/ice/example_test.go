package ice_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/ice"
)

// Importing the package registers its effects; render a frost nova's frames for a sprite sheet.
func ExampleFrostNova() {
	e, _ := pixelfx.Lookup("ice/frost-nova")
	frames := pixelfx.Render(e, pixelfx.Options{}, 0)
	fmt.Println(e == ice.FrostNova, e.Meta.Loop, len(frames.Pixels), frames.W, frames.H)
	// Output: true false 103 288 192
}
