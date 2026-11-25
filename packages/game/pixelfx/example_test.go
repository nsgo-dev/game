package pixelfx_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/fire"
)

// Play an effect and draw a frame of it: a torch, recolored blue, at its origin.
func ExampleNewPlayer() {
	e := fire.Torch
	p := pixelfx.NewPlayer(e, pixelfx.Options{
		X: float64(e.Meta.Origin[0]), Y: float64(e.Meta.Origin[1]),
		Values: map[string]pixelfx.Value{"palette": pixelfx.RampOf(pixelfx.Ramps["ice"])},
	})
	frame := pixelfx.NewFrame(e.Meta.Size[0], e.Meta.Size[1], 0, 0)
	for range 15 {
		p.Update(1.0 / 30)
	}
	p.Draw(frame, pixelfx.LayerBoth)
	rgba := pixelfx.Composite(frame, nil, nil, 1)
	fmt.Println(len(rgba) == e.Meta.Size[0]*e.Meta.Size[1]*4, p.T() > 0.49)
	// Output: true true
}

// Draw with the painter directly: a ring and a glow, mirrored about the origin.
func ExamplePainter() {
	f := pixelfx.NewFrame(16, 16, 0, 0)
	p := f.Painter().Place(8, 8, true, 0)
	p.Ring(0, 0, 6, 2, pixelfx.Patch, 1)
	p.Glow(0, 0, 5, pixelfx.White, 0.6)
	lit := 0
	for _, l := range f.Light {
		if l != 0 {
			lit++
		}
	}
	fmt.Println(f.Color[8*16+2] == pixelfx.Patch, lit > 50)
	// Output: true true
}

// Seeded randomness gives the same numbers on every run.
func ExampleRng() {
	r := pixelfx.NewRng(42)
	fmt.Printf("%.4f %.4f\n", r.Next(), r.Range(10, 20))
	// Output: 0.6011 14.4829
}
