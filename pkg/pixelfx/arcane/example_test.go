package arcane_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/arcane"
)

// Importing the package registers its effects; render a rune circle's frames for a sprite sheet.
func ExampleRuneCircle() {
	e, _ := pixelfx.Lookup("arcane/rune-circle")
	frames := pixelfx.Render(e, pixelfx.Options{}, 0)
	fmt.Println(e == arcane.RuneCircle, e.Meta.Loop, len(frames.Pixels), frames.W, frames.H)
	// Output: true true 120 136 100
}
