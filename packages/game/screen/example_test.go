package screen_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/screen"
	"github.com/nsgo-dev/nsgo/core"
)

// A Viewport fits a fixed game resolution into the window; with IntegerScale a pixel-art game scales by whole numbers only and the rest becomes bars.
func ExampleViewport() {
	vp := screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeFit, IntegerScale: true}
	window := core.Size{Width: 1366, Height: 768}
	p := vp.Layout(window)
	fmt.Println(p.ScaleX, p.Rect, len(p.Bars))

	game, inside := vp.ViewToGame(window, core.Point{X: 683, Y: 384})
	fmt.Println(game, inside)
	// Output:
	// 4 {{43 24} {1280 720}} 4
	// {160 90} true
}

// SafeRect is the part of the window clear of notches and rounded corners, where a HUD should go.
func ExampleSafeRect() {
	r := screen.SafeRect(core.Size{Width: 844, Height: 390}, core.Edges{Left: 47, Right: 47, Bottom: 21})
	fmt.Println(r)
	// Output:
	// {{47 0} {750 369}}
}

// Snap rounds a position to whole points so pixel art does not shimmer as it moves.
func ExampleSnap() {
	fmt.Println(screen.Snap(10.4, 20.6))
	// Output:
	// 10 21
}
