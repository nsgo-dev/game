package lights2d_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/lights2d"
	"github.com/nsgo-dev/nsgo/core"
)

// A Scene darkens the level with an ambient color and adds lights; EvaluateAt reads the light reaching a point, which game logic can use for stealth.
func ExampleScene() {
	torch := lights2d.NewLight(100, 100, 80, core.RGB(0xffffff))
	torch.CastShadows = true
	scene := &lights2d.Scene{
		Ambient:   core.RGBA(0x000000, 0.75),
		Lights:    []*lights2d.Light{torch},
		Occluders: []lights2d.Occluder{lights2d.RectOccluder(core.Bounds{Origin: core.Point{X: 140, Y: 80}, Size: core.Size{Width: 10, Height: 40}})},
	}
	for _, x := range []float32{100, 130, 170} {
		r, _, _ := scene.EvaluateAt(x, 100)
		fmt.Printf("x=%v: %.2f\n", x, r)
	}
	// Output:
	// x=100: 1.00
	// x=130: 0.39
	// x=170: 0.00
}

// Visible tests line of sight between two points past the occluders, as an enemy's view check does.
func ExampleVisible() {
	wall := lights2d.SegmentOccluder(lights2d.Segment{A: core.Point{X: 50, Y: 0}, B: core.Point{X: 50, Y: 100}})
	guard := core.Point{X: 0, Y: 50}
	fmt.Println(lights2d.Visible(guard, core.Point{X: 100, Y: 50}, []lights2d.Occluder{wall}))
	fmt.Println(lights2d.Visible(guard, core.Point{X: 40, Y: 90}, []lights2d.Occluder{wall}))
	// Output:
	// false
	// true
}

// VisibilityPolygon returns the area a light or viewer sees around the occluders, which PointInPolygon then tests points against.
func ExampleVisibilityPolygon() {
	box := lights2d.RectOccluder(core.Bounds{Origin: core.Point{X: 20, Y: -5}, Size: core.Size{Width: 10, Height: 10}})
	area := lights2d.VisibilityPolygon(core.Point{}, 50, []lights2d.Occluder{box})
	fmt.Println(len(area) > 0)
	fmt.Println(lights2d.PointInPolygon(core.Point{X: 10, Y: 0}, area), lights2d.PointInPolygon(core.Point{X: 40, Y: 0}, area))
	// Output:
	// true
	// true false
}

// A Light fades from full strength at its center to nothing at its radius.
func ExampleLight_AttenuationAt() {
	l := lights2d.NewLight(0, 0, 100, core.RGB(0xffcc88))
	for _, d := range []float32{0, 50, 100} {
		fmt.Printf("%.2f\n", l.AttenuationAt(d, 0))
	}
	// Output:
	// 1.00
	// 0.25
	// 0.00
}
