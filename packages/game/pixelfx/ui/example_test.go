package ui_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/ui"
)

// Importing the package registers its effects; render the damage number's frames for a sprite sheet.
func ExampleDamageNumber() {
	e, _ := pixelfx.Lookup("ui/damage-number")
	frames := pixelfx.Render(e, pixelfx.Options{}, 0)
	fmt.Println(e == ui.DamageNumber, e.Meta.Loop, len(frames.Pixels), frames.W, frames.H)
	// Output: true false 33 76 68
}
