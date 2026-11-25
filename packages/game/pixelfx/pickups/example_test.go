package pickups_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/pickups"
)

// Importing the package registers its effects; render the spinning coin's frames for a sprite sheet.
func ExampleCoinSpin() {
	e, _ := pixelfx.Lookup("pickups/coin-spin")
	frames := pixelfx.Render(e, pixelfx.Options{}, 0)
	fmt.Println(e == pickups.CoinSpin, e.Meta.Loop, len(frames.Pixels), frames.W, frames.H)
	// Output: true true 48 56 56
}
