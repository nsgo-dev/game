package transitions_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/transitions"
)

// Importing the package registers its screen transitions; render the blinds transition for a sprite sheet.
func ExampleBlinds() {
	e, _ := pixelfx.Lookup("transitions/blinds")
	frames := pixelfx.Render(e, pixelfx.Options{}, 0)
	fmt.Println(e == transitions.Blinds, e.Meta.Loop, len(frames.Pixels), frames.W, frames.H)
	// Output: true false 61 320 180
}
