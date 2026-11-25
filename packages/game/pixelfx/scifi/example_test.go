package scifi_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/scifi"
)

// Importing the package registers its effects; render the energy core's frames for a sprite sheet.
func ExampleEnergyCore() {
	e, _ := pixelfx.Lookup("scifi/energy-core")
	frames := pixelfx.Render(e, pixelfx.Options{}, 0)
	fmt.Println(e == scifi.EnergyCore, e.Meta.Loop, len(frames.Pixels), frames.W, frames.H)
	// Output: true true 72 112 100
}
