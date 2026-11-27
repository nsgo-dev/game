package critters_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/critters"
)

// Importing the package registers its effects; render the bat's frames for a sprite sheet.
func ExampleBat() {
	e, _ := pixelfx.Lookup("critters/bat")
	frames := pixelfx.Render(e, pixelfx.Options{}, 0)
	fmt.Println(e == critters.Bat, e.Meta.Loop, len(frames.Pixels), frames.W, frames.H)
	// Output: true true 72 112 80
}
