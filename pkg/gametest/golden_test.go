package gametest_test

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tilemap"
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

func TestRenderOpsAppliesImageTint(t *testing.T) {
	white := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			white.SetRGBA(x, y, color.RGBA{R: 200, G: 200, B: 200, A: 255})
		}
	}
	src := core.ImageSource{Pixels: white}
	var d core.Drawing
	d.DrawTintedImageFrom(src, core.RGBA(0x80ff00, 0.5), 0, 0, 4, 4, 0, 0, 4, 4)
	out := gametest.RenderOps(4, 4, d.Ops, gametest.RenderOptions{
		Background: color.RGBA{R: 0, G: 0, B: 0, A: 255},
	})
	px := out.RGBAAt(2, 2)
	// Expected: src=(200/255) * tint=(128/255, 255/255, 0) * alpha=(128/255) over black -> ~(50, 100, 0, 255).
	if px.R < 48 || px.R > 52 || px.G < 98 || px.G > 102 || px.B != 0 || px.A != 255 {
		t.Fatalf("tinted pixel = %+v, want approx {50, 100, 0, 255}", px)
	}
}

func TestRenderOpsZeroTintSkipsDrawing(t *testing.T) {
	white := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			white.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	src := core.ImageSource{Pixels: white}
	var d core.Drawing
	d.DrawTintedImageFrom(src, core.Color(0), 0, 0, 4, 4, 0, 0, 4, 4)
	d.DrawTintedImageIn(src, core.Color(0), 0, 0, 4, 4)
	bg := color.RGBA{R: 12, G: 34, B: 56, A: 255}
	out := gametest.RenderOps(4, 4, d.Ops, gametest.RenderOptions{
		Background: bg,
	})
	if px := out.RGBAAt(2, 2); px != bg {
		t.Fatalf("zero-tint pixel = %+v, want unchanged background %+v", px, bg)
	}
}

func TestCompareGoldenPartialAlphaRoundTrip(t *testing.T) {
	t.Run("premultiplied RGBA partial alpha pixels", func(t *testing.T) {
		// Include the exact reproducer ({R:1, A:200}) plus a full 0..255 alpha ramp
		// of valid premultiplied colors.
		img := image.NewRGBA(image.Rect(0, 0, 256, 2))
		img.SetRGBA(0, 0, color.RGBA{R: 1, G: 0, B: 0, A: 200})
		img.SetRGBA(1, 0, color.RGBA{R: 50, G: 100, B: 120, A: 128})
		img.SetRGBA(2, 0, color.RGBA{R: 1, G: 1, B: 1, A: 1})
		img.SetRGBA(3, 0, color.RGBA{R: 0, G: 0, B: 0, A: 0})
		for a := range 256 {
			r := uint8((a * 3) / 4)
			g := uint8(a / 2)
			b := uint8(a / 5)
			img.SetRGBA(a, 1, color.RGBA{R: r, G: g, B: b, A: uint8(a)})
		}

		path := filepath.Join(t.TempDir(), "partial_alpha.png")
		if _, _, err := gametest.CompareGolden(img, path, true); err != nil {
			t.Fatalf("CompareGolden(update=true): %v", err)
		}
		_, mismatched, err := gametest.CompareGolden(img, path, false)
		if err != nil {
			t.Fatalf("CompareGolden(update=false): %v", err)
		}
		if mismatched != 0 {
			t.Fatalf("partial-alpha RGBA round-trip had %d mismatched pixel(s), want 0", mismatched)
		}
	})

	t.Run("RenderOps translucent drawing on transparent background", func(t *testing.T) {
		var d core.Drawing
		d.SetAlpha(0.5)
		d.SetFillColor(core.RGB(0xe63946)).FillRect(2, 2, 12, 12)
		d.SetAlpha(0.35)
		d.SetStrokeColor(core.RGB(0x457b9d)).SetLineWidth(2).StrokeRect(4, 4, 8, 8)
		img := gametest.RenderOps(16, 16, d.Ops)

		path := filepath.Join(t.TempDir(), "translucent_ops.png")
		if _, _, err := gametest.CompareGolden(img, path, true); err != nil {
			t.Fatalf("CompareGolden(update=true): %v", err)
		}
		_, mismatched, err := gametest.CompareGolden(img, path, false)
		if err != nil {
			t.Fatalf("CompareGolden(update=false): %v", err)
		}
		if mismatched != 0 {
			t.Fatalf("translucent RenderOps round-trip had %d mismatched pixel(s), want 0", mismatched)
		}
	})

	t.Run("straight NRGBA partial alpha", func(t *testing.T) {
		img := image.NewNRGBA(image.Rect(0, 0, 4, 1))
		img.SetNRGBA(0, 0, color.NRGBA{R: 1, G: 0, B: 0, A: 200})
		img.SetNRGBA(1, 0, color.NRGBA{R: 255, G: 128, B: 64, A: 127})
		img.SetNRGBA(2, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 1})
		img.SetNRGBA(3, 0, color.NRGBA{R: 0, G: 0, B: 0, A: 0})

		path := filepath.Join(t.TempDir(), "nrgba_alpha.png")
		if _, _, err := gametest.CompareGolden(img, path, true); err != nil {
			t.Fatalf("CompareGolden(update=true): %v", err)
		}
		_, mismatched, err := gametest.CompareGolden(img, path, false)
		if err != nil || mismatched != 0 {
			t.Fatalf("NRGBA partial-alpha round-trip got mismatched=%d err=%v, want 0, nil", mismatched, err)
		}
	})

	t.Run("empty got reports dimension mismatch", func(t *testing.T) {
		want := image.NewRGBA(image.Rect(0, 0, 2, 2))
		want.SetRGBA(0, 0, color.RGBA{R: 100, G: 50, B: 25, A: 200})
		path := filepath.Join(t.TempDir(), "nonempty.png")
		if _, _, err := gametest.CompareGolden(want, path, true); err != nil {
			t.Fatalf("CompareGolden(update=true): %v", err)
		}
		_, mismatched, err := gametest.CompareGolden(image.NewRGBA(image.Rectangle{}), path, false)
		if err != nil {
			t.Fatalf("CompareGolden(empty got): %v", err)
		}
		if mismatched != 4 {
			t.Fatalf("mismatched = %d, want 4", mismatched)
		}
	})
}

func TestCompareGoldenPartialAlphaDetectsChanges(t *testing.T) {
	base := image.NewRGBA(image.Rect(0, 0, 4, 1))
	base.SetRGBA(0, 0, color.RGBA{R: 1, G: 0, B: 0, A: 200})
	base.SetRGBA(1, 0, color.RGBA{R: 50, G: 100, B: 120, A: 128})
	base.SetRGBA(2, 0, color.RGBA{R: 10, G: 20, B: 30, A: 64})
	base.SetRGBA(3, 0, color.RGBA{R: 0, G: 0, B: 0, A: 0})

	path := filepath.Join(t.TempDir(), "alpha_diff.png")
	if _, _, err := gametest.CompareGolden(base, path, true); err != nil {
		t.Fatalf("CompareGolden(update=true): %v", err)
	}

	magenta := color.RGBA{R: 255, G: 0, B: 255, A: 255}

	cloneBase := func() *image.RGBA {
		c := image.NewRGBA(base.Rect)
		copy(c.Pix, base.Pix)
		return c
	}

	// Single-step color change on a partial-alpha pixel.
	colorChanged := cloneBase()
	colorChanged.SetRGBA(0, 0, color.RGBA{R: 2, G: 0, B: 0, A: 200})
	diff, mismatched, err := gametest.CompareGolden(colorChanged, path, false)
	if err != nil {
		t.Fatalf("CompareGolden(colorChanged): %v", err)
	}
	if mismatched != 1 {
		t.Fatalf("color change mismatched = %d, want 1", mismatched)
	}
	if diff.RGBAAt(0, 0) != magenta || diff.RGBAAt(1, 0) == magenta {
		t.Fatalf("unexpected diff pixels for color change: (0,0)=%+v (1,0)=%+v", diff.RGBAAt(0, 0), diff.RGBAAt(1, 0))
	}

	// Single-step alpha change on a partial-alpha pixel.
	alphaChanged := cloneBase()
	alphaChanged.SetRGBA(1, 0, color.RGBA{R: 50, G: 100, B: 120, A: 129})
	diff, mismatched, err = gametest.CompareGolden(alphaChanged, path, false)
	if err != nil {
		t.Fatalf("CompareGolden(alphaChanged): %v", err)
	}
	if mismatched != 1 {
		t.Fatalf("alpha change mismatched = %d, want 1", mismatched)
	}
	if diff.RGBAAt(1, 0) != magenta || diff.RGBAAt(0, 0) == magenta {
		t.Fatalf("unexpected diff pixels for alpha change: (0,0)=%+v (1,0)=%+v", diff.RGBAAt(0, 0), diff.RGBAAt(1, 0))
	}
}

// subtestTB mimics *testing.T's Fatalf/TempDir/Cleanup lifecycle so a test can
// run a failing AssertGoldenAt in an isolated sub-test and inspect disk state
// after the sub-test's cleanups (including any t.TempDir removals) have run.
type subtestTB struct {
	testing.TB
	failed   bool
	fatalMsg string
	cleanups []func()
}

func (s *subtestTB) Helper() {}

func (s *subtestTB) Cleanup(fn func()) {
	s.cleanups = append(s.cleanups, fn)
}

func (s *subtestTB) TempDir() string {
	dir, err := os.MkdirTemp("", "gametest-subtest-*")
	if err != nil {
		panic(err)
	}
	s.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func (s *subtestTB) Fatal(args ...any) {
	s.failed = true
	s.fatalMsg = fmt.Sprint(args...)
	runtime.Goexit()
}

func (s *subtestTB) Fatalf(format string, args ...any) {
	s.failed = true
	s.fatalMsg = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func runFailingSubtest(t *testing.T, fn func(tb testing.TB)) (failed bool, msg string) {
	t.Helper()
	tb := &subtestTB{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			for i := len(tb.cleanups) - 1; i >= 0; i-- {
				tb.cleanups[i]()
			}
		}()
		fn(tb)
	}()
	<-done
	return tb.failed, tb.fatalMsg
}

var failurePathsRe = regexp.MustCompile(`got=(\S+) diff=([^\s;]+)`)

func TestAssertGoldenAtPersistsFailureImages(t *testing.T) {
	t.Setenv("UPDATE_GOLDEN", "")

	wantImg := image.NewRGBA(image.Rect(0, 0, 4, 4))
	wantImg.SetRGBA(1, 1, color.RGBA{R: 40, G: 80, B: 120, A: 200})

	gotImg := image.NewRGBA(image.Rect(0, 0, 4, 4))
	gotImg.SetRGBA(1, 1, color.RGBA{R: 120, G: 40, B: 80, A: 200})

	magenta := color.RGBA{R: 255, G: 0, B: 255, A: 255}

	t.Run("default beside golden and cleaned up on pass", func(t *testing.T) {
		t.Setenv("GOLDEN_DIFF_DIR", "")
		dir := t.TempDir()
		goldenPath := filepath.Join(dir, "frame.png")
		if err := gametest.WritePNG(goldenPath, wantImg); err != nil {
			t.Fatalf("WritePNG: %v", err)
		}

		failed, msg := runFailingSubtest(t, func(tb testing.TB) {
			gametest.AssertGoldenAt(tb, goldenPath, gotImg)
		})
		if !failed {
			t.Fatal("expected AssertGoldenAt to fail on mismatch")
		}
		m := failurePathsRe.FindStringSubmatch(msg)
		if len(m) != 3 {
			t.Fatalf("failure message missing got/diff paths: %q", msg)
		}
		gotPath, diffPath := m[1], m[2]
		wantGotPath := filepath.Join(dir, "frame.got.png")
		wantDiffPath := filepath.Join(dir, "frame.diff.png")
		if gotPath != wantGotPath || diffPath != wantDiffPath {
			t.Fatalf("reported paths = (%q, %q), want (%q, %q)", gotPath, diffPath, wantGotPath, wantDiffPath)
		}

		if _, err := os.Stat(gotPath); err != nil {
			t.Fatalf("reported got PNG does not exist after sub-test ended: %v", err)
		}
		if _, err := os.Stat(diffPath); err != nil {
			t.Fatalf("reported diff PNG does not exist after sub-test ended: %v", err)
		}
		decodedGot, err := gametest.ReadPNG(gotPath)
		if err != nil {
			t.Fatalf("ReadPNG(%s): %v", gotPath, err)
		}
		if decodedGot.Bounds().Dx() != 4 || decodedGot.Bounds().Dy() != 4 {
			t.Fatalf("decoded got bounds = %v, want 4x4", decodedGot.Bounds())
		}
		decodedDiff, err := gametest.ReadPNG(diffPath)
		if err != nil {
			t.Fatalf("ReadPNG(%s): %v", diffPath, err)
		}
		if color.RGBAModel.Convert(decodedDiff.At(1, 1)) != magenta {
			t.Fatalf("diff pixel (1,1) = %v, want magenta", decodedDiff.At(1, 1))
		}

		// A subsequent passing assertion cleans up the stale failure images.
		gametest.AssertGoldenAt(t, goldenPath, wantImg)
		if _, err := os.Stat(gotPath); !os.IsNotExist(err) {
			t.Fatalf("stale got PNG still exists after passing assertion: err=%v", err)
		}
		if _, err := os.Stat(diffPath); !os.IsNotExist(err) {
			t.Fatalf("stale diff PNG still exists after passing assertion: err=%v", err)
		}
	})

	t.Run("GOLDEN_DIFF_DIR override", func(t *testing.T) {
		diffDir := t.TempDir()
		t.Setenv("GOLDEN_DIFF_DIR", diffDir)

		goldenDir := t.TempDir()
		goldenPath := filepath.Join(goldenDir, "hud.png")
		if err := gametest.WritePNG(goldenPath, wantImg); err != nil {
			t.Fatalf("WritePNG: %v", err)
		}

		failed, msg := runFailingSubtest(t, func(tb testing.TB) {
			gametest.AssertGoldenAt(tb, goldenPath, gotImg)
		})
		if !failed {
			t.Fatal("expected AssertGoldenAt to fail on mismatch")
		}
		m := failurePathsRe.FindStringSubmatch(msg)
		if len(m) != 3 {
			t.Fatalf("failure message missing got/diff paths: %q", msg)
		}
		gotPath, diffPath := m[1], m[2]
		wantGotPath := filepath.Join(diffDir, "hud.got.png")
		wantDiffPath := filepath.Join(diffDir, "hud.diff.png")
		if gotPath != wantGotPath || diffPath != wantDiffPath {
			t.Fatalf("reported paths = (%q, %q), want (%q, %q)", gotPath, diffPath, wantGotPath, wantDiffPath)
		}
		if _, err := gametest.ReadPNG(gotPath); err != nil {
			t.Fatalf("ReadPNG(%s): %v", gotPath, err)
		}
		if _, err := gametest.ReadPNG(diffPath); err != nil {
			t.Fatalf("ReadPNG(%s): %v", diffPath, err)
		}
		// Nothing written beside goldenPath when GOLDEN_DIFF_DIR is set.
		if _, err := os.Stat(filepath.Join(goldenDir, "hud.got.png")); !os.IsNotExist(err) {
			t.Fatalf("unexpected artifact beside golden when GOLDEN_DIFF_DIR is set")
		}
	})

	t.Run("GOLDEN_DIFF_DIR preserves subdirs under DefaultGoldenDir", func(t *testing.T) {
		workDir := t.TempDir()
		origWD, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(workDir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(origWD) })

		diffDir := t.TempDir()
		t.Setenv("GOLDEN_DIFF_DIR", diffDir)

		goldenPath := gametest.GoldenPath(gametest.DefaultGoldenDir, "menus/title")
		if err := gametest.WritePNG(goldenPath, wantImg); err != nil {
			t.Fatalf("WritePNG: %v", err)
		}

		failed, msg := runFailingSubtest(t, func(tb testing.TB) {
			gametest.AssertGoldenAt(tb, goldenPath, gotImg)
		})
		if !failed {
			t.Fatal("expected AssertGoldenAt to fail on mismatch")
		}
		m := failurePathsRe.FindStringSubmatch(msg)
		if len(m) != 3 {
			t.Fatalf("failure message missing got/diff paths: %q", msg)
		}
		wantGotPath := filepath.Join(diffDir, "menus", "title.got.png")
		wantDiffPath := filepath.Join(diffDir, "menus", "title.diff.png")
		if m[1] != wantGotPath || m[2] != wantDiffPath {
			t.Fatalf("reported paths = (%q, %q), want (%q, %q)", m[1], m[2], wantGotPath, wantDiffPath)
		}
		if _, err := gametest.ReadPNG(m[1]); err != nil {
			t.Fatalf("ReadPNG(%s): %v", m[1], err)
		}
		if _, err := gametest.ReadPNG(m[2]); err != nil {
			t.Fatalf("ReadPNG(%s): %v", m[2], err)
		}
	})
}
