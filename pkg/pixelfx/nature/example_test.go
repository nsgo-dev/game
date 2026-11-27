package nature_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/nature"
)

// Importing the package registers its effects; render the tornado's frames for a sprite sheet.
func ExampleTornado() {
	e, _ := pixelfx.Lookup("nature/tornado")
	frames := pixelfx.Render(e, pixelfx.Options{}, 0)
	fmt.Println(e == nature.Tornado, e.Meta.Loop, len(frames.Pixels), frames.W, frames.H)
	// Output: true true 60 196 168
}
