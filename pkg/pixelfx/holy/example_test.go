package holy_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/holy"
)

// Importing the package registers its effects; render a smite's frames for a sprite sheet.
func ExampleSmite() {
	e, _ := pixelfx.Lookup("holy/smite")
	frames := pixelfx.Render(e, pixelfx.Options{}, 0)
	fmt.Println(e == holy.Smite, e.Meta.Loop, len(frames.Pixels), frames.W, frames.H)
	// Output: true false 85 224 176
}
