package gametest_test

import (
	"image"
	"image/color"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

// makeAtlasPixels builds a deterministic 32x16 sprite sheet with two 16x16 tiles:
// left (0..15): checker-bordered blue/cyan hero tile; right (16..31): warm gold coin tile.
func makeAtlasPixels() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 32, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			c := color.RGBA{R: 40, G: 130, B: 230, A: 255}
			if x == 0 || x == 15 || y == 0 || y == 15 {
				c = color.RGBA{R: 15, G: 45, B: 110, A: 255}
			} else if (x+y)%4 == 0 {
				c = color.RGBA{R: 120, G: 210, B: 255, A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	for y := 0; y < 16; y++ {
		for x := 16; x < 32; x++ {
			dx, dy := x-24, y-8
			if dx*dx+dy*dy <= 30 {
				img.SetRGBA(x, y, color.RGBA{R: 245, G: 195, B: 40, A: 255})
			}
		}
	}
	return img
}

func renderTestScene(t *testing.T, shiftRect bool) *image.RGBA {
	t.Helper()
	sheetPixels := makeAtlasPixels()
	src := core.ImageSource{Pixels: sheetPixels, Version: 1}
	atlas := &sprite.Atlas{
		Image: src,
		Frames: map[string]sprite.Frame{
			"hero": {Name: "hero", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			"coin": {Name: "coin", X: 16, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		},
	}

	ts := tilemap.NewGridTileset(1, src, 16, 16, 2, 2)
	tm := tilemap.New(3, 2, 16, 16, ts)
	tm.Layers = []tilemap.TileLayer{
		tilemap.NewTileLayer("ground", 3, 2, []uint32{
			1, 2, 1,
			2, 1, 2,
		}),
	}
	cam := &tilemap.Camera{X: 24, Y: 16, Zoom: 1, Width: 48, Height: 32}

	hero := sprite.NewSprite(atlas.Frames["hero"])
	hero.X, hero.Y = 64, 16
	hero.ScaleX, hero.ScaleY = 1.5, 1.5

	coin := sprite.NewSprite(atlas.Frames["coin"])
	coin.X, coin.Y = 92, 20
	coin.Alpha = 0.6

	batch := &sprite.Batch{Atlas: atlas, Sprites: []*sprite.Sprite{&hero, &coin}}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "scene",
			Style:  core.Style{Width: core.Pt(128), Height: core.Pt(64)},
			Draw: func(d *core.Drawing, _ core.Size) {
				// Backdrop fill.
				d.SetFillColor(core.RGB(0x1b263b)).FillRect(0, 0, 128, 64)

				// Translated & scaled tilemap region.
				d.Save()
				d.Translate(4, 16)
				cam.Apply(d)
				tm.Draw(d, cam)
				d.Restore()

				// Sprite batch (scaled + translucent sprites).
				batch.Draw(d)

				// Foreground UI badge with stroke + translucent fill.
				rx := float32(60)
				if shiftRect {
					rx = 72
				}
				d.Save()
				d.SetAlpha(0.85)
				d.SetFillColor(core.RGB(0xe63946)).FillRect(rx, 44, 24, 12)
				d.SetStrokeColor(core.RGB(0xf1faee)).SetLineWidth(2).StrokeRect(rx, 44, 24, 12)
				d.Restore()
			},
		}
	})

	ops := app.Find("scene").Ops
	if len(ops) == 0 {
		t.Fatal("canvas recorded no draw ops")
	}
	return gametest.RenderOps(128, 64, ops)
}

func TestGoldenSceneMatchesFixture(t *testing.T) {
	got := renderTestScene(t, false)
	gametest.AssertGolden(t, "scene", got)
}

func TestGoldenDetectsDeliberateDrawingChange(t *testing.T) {
	goldenPath := gametest.GoldenPath(gametest.DefaultGoldenDir, "scene")

	// 1. Matching output passes with zero mismatched pixels.
	matching := renderTestScene(t, false)
	diff, mismatched, err := gametest.CompareGolden(matching, goldenPath, false)
	if err != nil {
		t.Fatalf("CompareGolden(matching): %v", err)
	}
	if mismatched != 0 {
		t.Fatalf("matching scene had %d mismatched pixels, want 0", mismatched)
	}
	if diff == nil || diff.Bounds() != matching.Bounds() {
		t.Fatalf("unexpected diff bounds %v, want %v", diff.Bounds(), matching.Bounds())
	}

	// 2. Deliberate drawing change fails with a non-empty diff pinpointing changed pixels in magenta.
	changed := renderTestScene(t, true)
	diff, mismatched, err = gametest.CompareGolden(changed, goldenPath, false)
	if err != nil {
		t.Fatalf("CompareGolden(changed): %v", err)
	}
	if mismatched == 0 {
		t.Fatal("deliberate drawing change produced 0 mismatched pixels, want > 0")
	}

	magenta := color.RGBA{R: 255, G: 0, B: 255, A: 255}
	magentaCount := 0
	// Verify magenta pixels exist specifically in the shifted badge area (y in [43..57], x in [59..97]).
	inChangedBand := 0
	for y := 0; y < diff.Bounds().Dy(); y++ {
		for x := 0; x < diff.Bounds().Dx(); x++ {
			if diff.RGBAAt(x, y) == magenta {
				magentaCount++
				if x >= 58 && x <= 98 && y >= 42 && y <= 58 {
					inChangedBand++
				}
			}
		}
	}
	if magentaCount != mismatched {
		t.Fatalf("diff has %d magenta pixels, want %d (mismatched count)", magentaCount, mismatched)
	}
	if inChangedBand != magentaCount {
		t.Fatalf("expected all %d magenta pixels inside changed badge region, got %d inside", magentaCount, inChangedBand)
	}
	// Verify an unaffected pixel (top-left backdrop at (2, 2)) is NOT marked magenta.
	if gotPx := diff.RGBAAt(2, 2); gotPx == magenta {
		t.Fatalf("unaffected pixel (2,2) was unexpectedly marked magenta")
	}
}

func TestDiffImageHandlesDimensionMismatch(t *testing.T) {
	a := image.NewRGBA(image.Rect(0, 0, 4, 4))
	b := image.NewRGBA(image.Rect(0, 0, 6, 4))
	diff, mismatched := gametest.DiffImage(a, b)
	if diff.Bounds().Dx() != 6 || diff.Bounds().Dy() != 4 {
		t.Fatalf("diff size = %dx%d, want 6x4", diff.Bounds().Dx(), diff.Bounds().Dy())
	}
	if mismatched != 8 {
		t.Fatalf("mismatched = %d, want 8 extra column pixels", mismatched)
	}
	magenta := color.RGBA{R: 255, G: 0, B: 255, A: 255}
	if diff.RGBAAt(5, 0) != magenta {
		t.Fatalf("extra pixel (5,0) = %+v, want magenta", diff.RGBAAt(5, 0))
	}
}

func TestCompareGoldenUpdateAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roundtrip.png")
	img := gametest.RenderOps(16, 16, (&core.Drawing{}).
		SetFillColor(core.RGB(0x2a9d8f)).
		FillRect(2, 2, 12, 12).Ops)

	if _, _, err := gametest.CompareGolden(img, path, false); err == nil {
		t.Fatal("expected error for missing golden file")
	}
	if _, _, err := gametest.CompareGolden(img, path, true); err != nil {
		t.Fatalf("CompareGolden(update=true): %v", err)
	}
	_, mismatched, err := gametest.CompareGolden(img, path, false)
	if err != nil || mismatched != 0 {
		t.Fatalf("round-trip compare got mismatched=%d err=%v, want 0, nil", mismatched, err)
	}
}
