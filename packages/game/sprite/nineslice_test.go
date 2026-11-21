package sprite_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

var panelFrame = sprite.Frame{Name: "panel", X: 10, Y: 20, W: 32, H: 24, Width: 32, Height: 24}

func TestNineSliceDrawsNineSubRectanglesStretched(t *testing.T) {
	img := sheet.Image
	ns := sprite.NineSlice{
		Image:  img,
		Frame:  panelFrame,
		Insets: sprite.Insets{Left: 6, Top: 4, Right: 8, Bottom: 5},
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "slice", Style: core.Style{Width: core.Pt(200), Height: core.Pt(120)}, Draw: func(d *core.Drawing, _ core.Size) {
				ns.Draw(d, 10, 15, 100, 60)
			}},
			core.Canvas{TestID: "hand", Style: core.Style{Width: core.Pt(200), Height: core.Pt(120)}, Draw: func(d *core.Drawing, _ core.Size) {
				// Source x: [10, 16, 34] widths [6, 18, 8]; dest x: [10, 16, 102] widths [6, 86, 8].
				// Source y: [20, 24, 39] heights [4, 15, 5]; dest y: [15, 19, 70] heights [4, 51, 5].
				d.DrawImageFrom(img, 10, 20, 6, 4, 10, 15, 6, 4)
				d.DrawImageFrom(img, 16, 20, 18, 4, 16, 15, 86, 4)
				d.DrawImageFrom(img, 34, 20, 8, 4, 102, 15, 8, 4)

				d.DrawImageFrom(img, 10, 24, 6, 15, 10, 19, 6, 51)
				d.DrawImageFrom(img, 16, 24, 18, 15, 16, 19, 86, 51)
				d.DrawImageFrom(img, 34, 24, 8, 15, 102, 19, 8, 51)

				d.DrawImageFrom(img, 10, 39, 6, 5, 10, 70, 6, 5)
				d.DrawImageFrom(img, 16, 39, 18, 5, 16, 70, 86, 5)
				d.DrawImageFrom(img, 34, 39, 8, 5, 102, 70, 8, 5)
			}},
		}}
	})
	sameOps(t, app.Find("slice").Ops, app.Find("hand").Ops)
}

func TestNineSliceHandlesZeroInsetsAndThreeSlice(t *testing.T) {
	img := sheet.Image
	zero := sprite.NineSlice{Image: img, Frame: panelFrame}
	horiz := sprite.NineSlice{Image: img, Frame: panelFrame, Insets: sprite.Insets{Left: 6, Right: 8}}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "slice", Style: core.Style{Width: core.Pt(200), Height: core.Pt(120)}, Draw: func(d *core.Drawing, _ core.Size) {
				zero.Draw(d, 0, 0, 80, 40)
				horiz.Draw(d, 5, 50, 60, 24)
			}},
			core.Canvas{TestID: "hand", Style: core.Style{Width: core.Pt(200), Height: core.Pt(120)}, Draw: func(d *core.Drawing, _ core.Size) {
				// Zero insets emit a single DrawImageFrom for the whole frame.
				d.DrawImageFrom(img, 10, 20, 32, 24, 0, 0, 80, 40)
				// Top/bottom zero insets emit only the three middle-row slices.
				d.DrawImageFrom(img, 10, 20, 6, 24, 5, 50, 6, 24)
				d.DrawImageFrom(img, 16, 20, 18, 24, 11, 50, 46, 24)
				d.DrawImageFrom(img, 34, 20, 8, 24, 57, 50, 8, 24)
			}},
		}}
	})
	sameOps(t, app.Find("slice").Ops, app.Find("hand").Ops)
}

func TestNineSliceShrinksCornersProportionallyWhenSmallerThanMargins(t *testing.T) {
	img := sheet.Image
	ns := sprite.NineSlice{
		Image:  img,
		Frame:  panelFrame,
		Insets: sprite.Insets{Left: 12, Top: 6, Right: 4, Bottom: 6},
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "slice", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				// Both axes smaller than margins (16 -> 8, 12 -> 6): only the 4 corners draw at half size.
				ns.Draw(d, 2, 3, 8, 6)
				// Width smaller than margins (16 -> 8), height larger (20): 4 corners + left and right edges.
				ns.Draw(d, 20, 10, 8, 20)
				// Degenerate sizes draw nothing.
				ns.Draw(d, 0, 0, 0, 20)
				ns.Draw(d, 0, 0, 20, -1)
				(&sprite.NineSlice{Image: img}).Draw(d, 0, 0, 20, 20)
			}},
			core.Canvas{TestID: "hand", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				// Half-size corners: left 6, right 2, top 3, bottom 3.
				d.DrawImageFrom(img, 10, 20, 12, 6, 2, 3, 6, 3)
				d.DrawImageFrom(img, 38, 20, 4, 6, 8, 3, 2, 3)
				d.DrawImageFrom(img, 10, 38, 12, 6, 2, 6, 6, 3)
				d.DrawImageFrom(img, 38, 38, 4, 6, 8, 6, 2, 3)

				// Shrunk width (6 + 2), full height (6 + 8 + 6).
				d.DrawImageFrom(img, 10, 20, 12, 6, 20, 10, 6, 6)
				d.DrawImageFrom(img, 38, 20, 4, 6, 26, 10, 2, 6)
				d.DrawImageFrom(img, 10, 26, 12, 12, 20, 16, 6, 8)
				d.DrawImageFrom(img, 38, 26, 4, 12, 26, 16, 2, 8)
				d.DrawImageFrom(img, 10, 38, 12, 6, 20, 24, 6, 6)
				d.DrawImageFrom(img, 38, 38, 4, 6, 26, 24, 2, 6)
			}},
		}}
	})
	sameOps(t, app.Find("slice").Ops, app.Find("hand").Ops)
}

func TestNineSliceTilesCenterWithCroppedTrailingTiles(t *testing.T) {
	img := sheet.Image
	// 20x20 frame with 5pt margins -> 10x10 center tile.
	frame := sprite.Frame{Name: "box", X: 0, Y: 0, W: 20, H: 20, Width: 20, Height: 20}
	ns := sprite.NineSlice{
		Image:      img,
		Frame:      frame,
		Insets:     sprite.Insets{Left: 5, Top: 5, Right: 5, Bottom: 5},
		TileCenter: true,
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "slice", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				// Center dest is 15 wide x 12 high: 2 cols (10, 5) x 2 rows (10, 2).
				ns.Draw(d, 0, 0, 25, 22)
			}},
			core.Canvas{TestID: "hand", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				d.DrawImageFrom(img, 0, 0, 5, 5, 0, 0, 5, 5)
				d.DrawImageFrom(img, 5, 0, 10, 5, 5, 0, 15, 5)
				d.DrawImageFrom(img, 15, 0, 5, 5, 20, 0, 5, 5)

				d.DrawImageFrom(img, 0, 5, 5, 10, 0, 5, 5, 12)
				// Tiled 10x10 center across 15x12.
				d.DrawImageFrom(img, 5, 5, 10, 10, 5, 5, 10, 10)
				d.DrawImageFrom(img, 5, 5, 5, 10, 15, 5, 5, 10)
				d.DrawImageFrom(img, 5, 5, 10, 2, 5, 15, 10, 2)
				d.DrawImageFrom(img, 5, 5, 5, 2, 15, 15, 5, 2)
				d.DrawImageFrom(img, 15, 5, 5, 10, 20, 5, 5, 12)

				d.DrawImageFrom(img, 0, 15, 5, 5, 0, 17, 5, 5)
				d.DrawImageFrom(img, 5, 15, 10, 5, 5, 17, 15, 5)
				d.DrawImageFrom(img, 15, 15, 5, 5, 20, 17, 5, 5)
			}},
		}}
	})
	sameOps(t, app.Find("slice").Ops, app.Find("hand").Ops)
}
