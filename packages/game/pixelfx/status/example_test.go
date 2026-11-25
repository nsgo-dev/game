package status_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/status"
)

// Importing the package registers its effects; render the bleeding status frames for a sprite sheet.
func ExampleBleeding() {
	e, _ := pixelfx.Lookup("status/bleeding")
	frames := pixelfx.Render(e, pixelfx.Options{}, 0)
	fmt.Println(e == status.Bleeding, e.Meta.Loop, len(frames.Pixels), frames.W, frames.H)
	// Output: true true 72 60 56
}
