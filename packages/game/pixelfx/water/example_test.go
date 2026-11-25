package water_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/water"
)

// Importing the package registers its effects; render the splash's frames for a sprite sheet.
func ExampleSplash() {
	e, _ := pixelfx.Lookup("water/splash")
	frames := pixelfx.Render(e, pixelfx.Options{}, 0)
	fmt.Println(e == water.Splash, e.Meta.Loop, len(frames.Pixels), frames.W, frames.H)
	// Output: true false 76 176 100
}
