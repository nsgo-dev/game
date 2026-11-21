package sprite_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

// sheet is an atlas of a 16 by 16 hero and a coin trimmed to 8 by 10 of its 16 by 16.
var sheet = &sprite.Atlas{
	Image: core.ImageSource{URI: "asset:sheet.png"},
	Frames: map[string]sprite.Frame{
		"hero": {Name: "hero", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		"coin": {Name: "coin", X: 16, Y: 0, W: 8, H: 10, OffsetX: 4, OffsetY: 3, Width: 16, Height: 16},
	},
}

func TestABatchDrawsWhatHandWrittenCallsDraw(t *testing.T) {
	place := func(frame string, edit func(s *sprite.Sprite)) *sprite.Sprite {
		s := sprite.NewSprite(sheet.Frames[frame])
		edit(&s)
		return &s
	}
	batch := &sprite.Batch{Atlas: sheet, Sprites: []*sprite.Sprite{
		// Listed first, drawn last.
		place("hero", func(s *sprite.Sprite) { s.X, s.Y, s.Z = 10, 20, 1 }),
		place("hero", func(s *sprite.Sprite) { s.X, s.Y, s.OriginX, s.OriginY, s.ScaleX, s.ScaleY = 50, 50, 8, 8, 2, 2 }),
		place("hero", func(s *sprite.Sprite) { s.X, s.Y, s.OriginX, s.OriginY, s.Rotation = 100, 100, 8, 8, math.Pi/2 }),
		place("coin", func(s *sprite.Sprite) { s.X, s.FlipX = 200, true }),
		place("coin", func(s *sprite.Sprite) { s.X, s.Y, s.Alpha = 300, 40, 0.5 }),
		place("hero", func(s *sprite.Sprite) { s.Alpha = 0 }),
		place("hero", func(s *sprite.Sprite) { s.Frame = sprite.Frame{} }),
	}}
	first := batch.Sprites[0]
	img := sheet.Image
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "batch", Style: core.Style{Width: core.Pt(400), Height: core.Pt(200)}, Draw: func(d *core.Drawing, _ core.Size) {
				d.Translate(5, 5)
				batch.Draw(d)
			}},
			core.Canvas{TestID: "hand", Style: core.Style{Width: core.Pt(400), Height: core.Pt(200)}, Draw: func(d *core.Drawing, _ core.Size) {
				d.Translate(5, 5)
				// Twice the size about its center at 50, 50.
				d.DrawImageFrom(img, 0, 0, 16, 16, 34, 34, 32, 32)
				// A quarter turn about its center at 100, 100.
				d.Save().Transform(0, 1, -1, 0, 108, 92).DrawImageFrom(img, 0, 0, 16, 16, 0, 0, 16, 16).Restore()
				// Mirrored in its 16-point box at 200, 0: the trimmed part's left edge, 4 in, goes to 212.
				d.Save().Transform(-1, 0, 0, 1, 216, 0).DrawImageFrom(img, 16, 0, 8, 10, 4, 3, 8, 10).Restore()
				d.Save().SetAlpha(0.5).DrawImageFrom(img, 16, 0, 8, 10, 304, 43, 8, 10).Restore()
				d.DrawImageFrom(img, 0, 0, 16, 16, 10, 20, 16, 16)
			}},
		}}
	})
	got, want := app.Find("batch").Ops, app.Find("hand").Ops
	if len(got) == 0 {
		t.Fatal("the batch drew nothing")
	}
	sameOps(t, got, want)
	if batch.Sprites[0] != first {
		t.Fatal("drawing reordered the batch's sprites")
	}
}

// sameOps fails t unless got and want are the same operations, their numbers within float rounding, as a turn's sine and cosine are.
func sameOps(t *testing.T, got, want []core.DrawOp) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d ops, want %d:\n%+v\nwant:\n%+v", len(got), len(want), got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		near := func(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }
		gf := []float32{g.X, g.Y, g.X1, g.Y1, g.X2, g.Y2, g.W, g.H, g.R, g.A, g.A0, g.A1, g.RY}
		wf := []float32{w.X, w.Y, w.X1, w.Y1, w.X2, w.Y2, w.W, w.H, w.R, w.A, w.A0, w.A1, w.RY}
		for j := range gf {
			if !near(gf[j], wf[j]) {
				t.Fatalf("op %d is %+v, want %+v", i, g, w)
			}
		}
		if g.Kind != w.Kind || !reflect.DeepEqual(g.Image, w.Image) {
			t.Fatalf("op %d is %+v, want %+v", i, g, w)
		}
		// The rest are the same exactly.
		g.X, g.Y, g.X1, g.Y1, g.X2, g.Y2, g.W, g.H, g.R, g.A, g.A0, g.A1, g.RY = w.X, w.Y, w.X1, w.Y1, w.X2, w.Y2, w.W, w.H, w.R, w.A, w.A0, w.A1, w.RY
		g.Image = w.Image
		if !reflect.DeepEqual(g, w) {
			t.Fatalf("op %d is %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestNewSpriteDrawsTheFrameAtItsTopLeft(t *testing.T) {
	s := sprite.NewSprite(sheet.Frames["coin"])
	s.X, s.Y = 7, 9
	b := sprite.Batch{Atlas: sheet, Sprites: []*sprite.Sprite{&s}}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "batch", Style: core.Style{Width: core.Pt(40), Height: core.Pt(40)}, Draw: func(d *core.Drawing, _ core.Size) { b.Draw(d) }},
			core.Canvas{TestID: "hand", Style: core.Style{Width: core.Pt(40), Height: core.Pt(40)}, Draw: func(d *core.Drawing, _ core.Size) {
				// The trimmed part sits 4, 3 into the untrimmed frame.
				d.DrawImageFrom(sheet.Image, 16, 0, 8, 10, 11, 12, 8, 10)
			}},
		}}
	})
	sameOps(t, app.Find("batch").Ops, app.Find("hand").Ops)
}

func TestNewSpriteUsesTheFramesPivotAsItsOrigin(t *testing.T) {
	atlas, err := sprite.LoadAtlas(core.ImageSource{URI: "asset:items@2x.png"}, texturePackerHash)
	if err != nil {
		t.Fatal(err)
	}
	coin := sprite.NewSprite(atlas.Frames["coin.png"])
	if coin.OriginX != 10 || coin.OriginY != 10 {
		t.Fatalf("coin origin is %v, %v, want 10, 10", coin.OriginX, coin.OriginY)
	}
	coin.X, coin.Y = 30, 40

	flag := sprite.NewSprite(atlas.Frames["flag.png"])
	if flag.OriginX != 4 || flag.OriginY != 20 {
		t.Fatalf("flag origin is %v, %v, want 4, 20", flag.OriginX, flag.OriginY)
	}
	flag.X, flag.Y = 50, 60

	b := sprite.Batch{Atlas: atlas, Sprites: []*sprite.Sprite{&coin, &flag}}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "batch", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) { b.Draw(d) }},
			core.Canvas{TestID: "hand", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				// coin's untrimmed box sits at 20, 30 (30-10, 40-10); its trimmed part is 4, 3 inside it.
				d.DrawImageFrom(atlas.Image, 22, 1, 12, 14, 24, 33, 12, 14)
				// flag's untrimmed box sits at 46, 40 (50-4, 60-20); its trimmed part is 2, 1 inside it.
				d.DrawImageFrom(atlas.Image, 35, 1, 10, 18, 48, 41, 10, 18)
			}},
		}}
	})
	sameOps(t, app.Find("batch").Ops, app.Find("hand").Ops)
}
