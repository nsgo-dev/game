package gametest_test

import (
	"image"
	"image/color"
	"math"
	"reflect"
	"runtime"
	"testing"

	"github.com/nsgo-dev/game/pkg/gametest"
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

func TestImageDrawsSizesANaturalSizeImageAndReadsTintSamplingAndClip(t *testing.T) {
	pixels := core.ImageSource{Pixels: image.NewRGBA(image.Rect(0, 0, 16, 8)), Version: 1}
	sized := core.ImageSource{URI: "asset:icon.png", Size: core.Size{Width: 10, Height: 4}}
	red := core.Color(0xff0000ff)

	var d core.Drawing
	d.DrawImage(pixels, 5, 6)
	d.DrawImage(sized, 1, 2)
	d.Save().BeginPath().Rect(0, 0, 10, 10).Clip().SetImageSmoothing(false).DrawTintedImage(sized, red, 0, 0).Restore()
	d.DrawImageFrom(pixels, 0, 0, 4, 4, 0, 0, 4, 4)

	got := gametest.ImageDraws(d.Ops)
	if len(got) != 4 {
		t.Fatalf("%d image draws, want 4: %+v", len(got), got)
	}
	if want := [4]core.Point{{X: 5, Y: 6}, {X: 21, Y: 6}, {X: 21, Y: 14}, {X: 5, Y: 14}}; got[0].Corners != want {
		t.Fatalf("an image of pixels at its natural size lands at %v, want %v", got[0].Corners, want)
	}
	if want := [4]core.Point{{X: 1, Y: 2}, {X: 11, Y: 2}, {X: 11, Y: 6}, {X: 1, Y: 6}}; got[1].Corners != want {
		t.Fatalf("an image with a Size at its natural size lands at %v, want %v", got[1].Corners, want)
	}
	tinted := got[2]
	if !tinted.Tinted || tinted.Tint != red || tinted.Sampling == got[1].Sampling {
		t.Fatalf("tinted, unsmoothed image is %+v, want tinted %v and a sampling other than %v", tinted, red, got[1].Sampling)
	}
	if len(tinted.Clip) != 2 || tinted.Clip[0].Op.Kind != core.DrawRect || tinted.Clip[1].Op.Kind != core.DrawClip {
		t.Fatalf("clip is %+v, want the rectangle and the Clip", tinted.Clip)
	}
	if len(got[3].Clip) != 0 {
		t.Fatalf("the image after Restore is still clipped: %+v", got[3].Clip)
	}
}

// fatalRecorder is a testing.TB whose Fatalf notes the failure and stops the goroutine, as testing.T's does.
type fatalRecorder struct {
	testing.TB
	failed bool
}

func (r *fatalRecorder) Helper() {}

func (r *fatalRecorder) Fatalf(string, ...any) {
	r.failed = true
	runtime.Goexit()
}

// assertFails reports whether AssertSameImages fails got against want.
func assertFails(got, want []core.DrawOp) bool {
	r := &fatalRecorder{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		gametest.AssertSameImages(r, got, want)
	}()
	<-done
	return r.failed
}

func TestAssertSameImagesTellsDrawsApart(t *testing.T) {
	img := core.ImageSource{URI: "asset:icon.png", Size: core.Size{Width: 10, Height: 4}}
	draw := func(f func(d *core.Drawing)) []core.DrawOp {
		var d core.Drawing
		f(&d)
		return d.Ops
	}
	plain := draw(func(d *core.Drawing) { d.DrawImage(img, 1, 2) })
	if assertFails(draw(func(d *core.Drawing) { d.DrawImageIn(img, 1, 2, 10, 4) }), plain) {
		t.Fatal("a natural-size DrawImage differs from a DrawImageIn of the same size")
	}
	for name, ops := range map[string][]core.DrawOp{
		"another size": draw(func(d *core.Drawing) { d.DrawImageIn(img, 1, 2, 20, 4) }),
		"a tint":       draw(func(d *core.Drawing) { d.DrawTintedImage(img, core.Color(0xff0000ff), 1, 2) }),
		"sampling":     draw(func(d *core.Drawing) { d.SetImageSmoothing(false).DrawImage(img, 1, 2) }),
		"a clip":       draw(func(d *core.Drawing) { d.BeginPath().Rect(0, 0, 4, 4).Clip().DrawImage(img, 1, 2) }),
	} {
		if !assertFails(ops, plain) {
			t.Errorf("AssertSameImages missed %s", name)
		}
	}
}
