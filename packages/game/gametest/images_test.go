package gametest_test

import (
	"image"
	"image/color"
	"math"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/nsgo/core"
)

func TestImageDrawsReadsDrawImageAndDrawAtlasAlike(t *testing.T) {
	img := core.ImageSource{URI: "asset:sheet.png"}
	tile := core.Bounds{Origin: core.Point{X: 16}, Size: core.Size{Width: 8, Height: 10}}

	var each core.Drawing
	each.Translate(5, 5)
	each.DrawImageFrom(img, 16, 0, 8, 10, 20, 30, 16, 20)
	each.Save().SetAlpha(0.5).SetComposite(core.CompositeLighter).Transform(0, 1, -1, 0, 100, 0).DrawImageFrom(img, 16, 0, 8, 10, 0, 0, 8, 10).Restore()

	var atlas core.Drawing
	atlas.Translate(5, 5)
	atlas.DrawAtlas(img, []core.Bounds{tile}, []core.RSXform{{SCos: 2, TX: 20, TY: 30}}, nil)
	atlas.Save().SetAlpha(1).SetComposite(core.CompositeLighter).DrawAtlas(img, []core.Bounds{tile}, []core.RSXform{{SSin: 1, TX: 100}}, []float32{0.5}).Restore()

	gametest.AssertSameImages(t, atlas.Ops, each.Ops)

	got := gametest.ImageDraws(atlas.Ops)
	want := []gametest.ImageDraw{
		{Image: img, SX: 16, SW: 8, SH: 10, Corners: [4]core.Point{{X: 25, Y: 35}, {X: 41, Y: 35}, {X: 41, Y: 55}, {X: 25, Y: 55}}, Alpha: 1},
		{Image: img, SX: 16, SW: 8, SH: 10, Corners: [4]core.Point{{X: 105, Y: 5}, {X: 105, Y: 13}, {X: 95, Y: 13}, {X: 95, Y: 5}}, Alpha: 0.5, Composite: core.CompositeLighter},
	}
	if len(got) != len(want) {
		t.Fatalf("%d image draws, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		for j := range w.Corners {
			if math.Abs(float64(g.Corners[j].X-w.Corners[j].X)) > 1e-4 || math.Abs(float64(g.Corners[j].Y-w.Corners[j].Y)) > 1e-4 {
				t.Fatalf("image draw %d is %+v, want %+v", i, g, w)
			}
		}
		g.Corners = w.Corners
		if !reflect.DeepEqual(g, w) {
			t.Fatalf("image draw %d is %+v, want %+v", i, g, w)
		}
	}
}

func TestRenderOpsDrawsAnAtlasAsItsSeparateImages(t *testing.T) {
	sheet := image.NewRGBA(image.Rect(0, 0, 16, 8))
	for y := range 8 {
		for x := range 16 {
			sheet.SetRGBA(x, y, color.RGBA{R: uint8(x * 16), G: uint8(y * 32), B: 200, A: 255})
		}
	}
	src := core.ImageSource{Pixels: sheet, Version: 1}
	left := core.Bounds{Size: core.Size{Width: 8, Height: 8}}
	right := core.Bounds{Origin: core.Point{X: 8}, Size: core.Size{Width: 8, Height: 8}}

	var each core.Drawing
	each.DrawImageFrom(src, 0, 0, 8, 8, 2, 2, 8, 8)
	each.DrawImageFrom(src, 8, 0, 8, 8, 12, 2, 16, 16)
	each.Save().SetAlpha(0.5).Transform(0, 1, -1, 0, 40, 4).DrawImageFrom(src, 0, 0, 8, 8, 0, 0, 8, 8).Restore()

	var atlas core.Drawing
	atlas.DrawAtlas(src, []core.Bounds{left, right, left}, []core.RSXform{{SCos: 1, TX: 2, TY: 2}, {SCos: 2, TX: 12, TY: 2}, {SSin: 1, TX: 40, TY: 4}}, []float32{1, 1, 0.5})

	got, want := gametest.RenderOps(48, 24, atlas.Ops), gametest.RenderOps(48, 24, each.Ops)
	if _, n := gametest.DiffImage(got, want); n != 0 {
		t.Fatalf("%d pixels differ between DrawAtlas and separate DrawImageFrom calls", n)
	}
	if got.RGBAAt(3, 3).A == 0 {
		t.Fatal("DrawAtlas drew nothing at its first tile")
	}
}
