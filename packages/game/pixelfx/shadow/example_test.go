package shadow_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/shadow"
)

// Importing the package registers its effects; render the black hole's frames for a sprite sheet.
func ExampleBlackHole() {
	e, _ := pixelfx.Lookup("shadow/black-hole")
	frames := pixelfx.Render(e, pixelfx.Options{}, 0)
	fmt.Println(e == shadow.BlackHole, e.Meta.Loop, len(frames.Pixels), frames.W, frames.H)
	// Output: true true 120 184 132
}
