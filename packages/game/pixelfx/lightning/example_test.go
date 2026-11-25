package lightning_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/lightning"
)

// Importing the package registers its effects; render a zap hit's frames for a sprite sheet.
func ExampleZapHit() {
	e, _ := pixelfx.Lookup("lightning/zap-hit")
	frames := pixelfx.Render(e, pixelfx.Options{}, 0)
	fmt.Println(e == lightning.ZapHit, e.Meta.Loop, len(frames.Pixels), frames.W, frames.H)
	// Output: true false 28 144 112
}
