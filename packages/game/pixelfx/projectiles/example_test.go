package projectiles_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/projectiles"
)

// Importing the package registers its effects; render the arrow's frames for a sprite sheet.
func ExampleArrow() {
	e, _ := pixelfx.Lookup("projectiles/arrow")
	frames := pixelfx.Render(e, pixelfx.Options{}, 0)
	fmt.Println(e == projectiles.Arrow, e.Meta.Loop, len(frames.Pixels), frames.W, frames.H)
	// Output: true true 18 112 32
}
