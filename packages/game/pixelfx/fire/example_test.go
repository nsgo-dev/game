package fire_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/fire"
)

// Importing the package registers its effects; render the torch's frames for a sprite sheet.
func ExampleTorch() {
	e, _ := pixelfx.Lookup("fire/torch")
	frames := pixelfx.Render(e, pixelfx.Options{}, 0)
	fmt.Println(e == fire.Torch, e.Meta.Loop, len(frames.Pixels), frames.W, frames.H)
	// Output: true true 60 72 120
}
