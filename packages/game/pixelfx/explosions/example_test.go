package explosions_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/explosions"
)

// A bigger, greener blast: the explosion's own options, over its defaults.
func ExampleSmallExplosion() {
	e := explosions.SmallExplosion
	o := pixelfx.Options{X: 150, Y: 196}.Set("size", pixelfx.Num(1.3)).Set("palette", pixelfx.RampOf(pixelfx.Ramps["poison"]))
	p := pixelfx.NewPlayer(e, o)
	frame := pixelfx.NewFrame(e.Meta.Size[0], e.Meta.Size[1], 0, 0)
	for !p.Done() {
		p.Update(1.0 / 30)
	}
	p.Draw(frame, pixelfx.LayerBoth)
	fmt.Printf("%s, %.1f s, done %v\n", e.ID, p.T(), p.Done())
	// Output: explosions/small-explosion, 5.4 s, done true
}

// Importing the package registers its effects; render a cartoon hit that says BAM! instead of POW!.
func ExampleCartoonPow() {
	e, _ := pixelfx.Lookup("explosions/cartoon-pow")
	frames := pixelfx.Render(e, pixelfx.Options{}.Set("text", pixelfx.Text("BAM!")), 0)
	fmt.Println(e == explosions.CartoonPow, len(frames.Pixels), frames.W, frames.H)
	// Output: true 33 184 144
}
