package spawn_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/spawn"
)

// Importing the package registers its effects; render the poof's frames for a sprite sheet.
func ExamplePoof() {
	e, _ := pixelfx.Lookup("spawn/poof")
	frames := pixelfx.Render(e, pixelfx.Options{}, 0)
	fmt.Println(e == spawn.Poof, e.Meta.Loop, len(frames.Pixels), frames.W, frames.H)
	// Output: true false 49 112 92
}
