package pack_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tooling/pack"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

// writeTestPNG writes img to path, creating parent directories as needed.
func writeTestPNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// patternImage returns a w by h NRGBA with a unique pixel value inside contentRect and transparent pixels outside it.
func patternImage(w, h int, contentRect image.Rectangle, seed uint8, semiAlpha bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := contentRect.Min.Y; y < contentRect.Max.Y; y++ {
		for x := contentRect.Min.X; x < contentRect.Max.X; x++ {
			a := uint8(255)
			if semiAlpha && (x+y)%3 == 0 {
				a = uint8(64 + ((x*13 + y*7) % 180))
			}
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8((x*17 + int(seed)*31) % 256),
				G: uint8((y*23 + int(seed)*19) % 256),
				B: uint8(((x+y)*11 + int(seed)*7) % 256),
				A: a,
			})
		}
	}
	return img
}

// assertFrameMatchesOriginal reconstructs the untrimmed image from atlasImg and fr and checks pixel-for-pixel equality with orig.
func assertFrameMatchesOriginal(t *testing.T, name string, atlasImg *image.NRGBA, fr sprite.Frame, orig *image.NRGBA) {
	t.Helper()
	ow, oh := orig.Bounds().Dx(), orig.Bounds().Dy()
	if int(fr.Width) != ow || int(fr.Height) != oh {
		t.Fatalf("frame %q source size is %gx%g, want %dx%d", name, fr.Width, fr.Height, ow, oh)
	}
	got := image.NewNRGBA(image.Rect(0, 0, ow, oh))
	fx, fy, fw, fh := int(fr.X), int(fr.Y), int(fr.W), int(fr.H)
	ox, oy := int(fr.OffsetX), int(fr.OffsetY)
	for y := range fh {
		for x := range fw {
			got.SetNRGBA(ox+x, oy+y, atlasImg.NRGBAAt(fx+x, fy+y))
		}
	}
	for y := range oh {
		for x := range ow {
			g := got.NRGBAAt(x, y)
			w := orig.NRGBAAt(x, y)
			if g != w {
				t.Fatalf("frame %q pixel (%d,%d) is %+v, want %+v", name, x, y, g, w)
			}
		}
	}
}

func TestPackedFolderLoadsWithSpriteLoadAtlasAndMatchesPixels(t *testing.T) {
	inDir := t.TempDir()
	outDir := t.TempDir()

	originals := map[string]*image.NRGBA{
		"opaque.png":          patternImage(16, 12, image.Rect(0, 0, 16, 12), 1, false),
		"trimmed.png":         patternImage(24, 20, image.Rect(3, 5, 17, 18), 2, true),
		"tiny.png":            patternImage(1, 1, image.Rect(0, 0, 1, 1), 3, false),
		"empty.png":           image.NewNRGBA(image.Rect(0, 0, 12, 10)),
		"enemies/slime.png":   patternImage(10, 14, image.Rect(2, 1, 9, 11), 4, true),
		"items/gems/ruby.png": patternImage(8, 8, image.Rect(0, 0, 8, 8), 5, false),
	}
	for rel, img := range originals {
		writeTestPNG(t, filepath.Join(inDir, filepath.FromSlash(rel)), img)
	}
	// Non-PNG and hidden files should be ignored.
	if err := os.WriteFile(filepath.Join(inDir, "readme.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inDir, ".hidden.png"), []byte("not a png"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := pack.DefaultOptions()
	opts.Name = "sprites"
	opts.Padding = 2

	res, err := pack.PackDir(inDir, opts)
	if err != nil {
		t.Fatalf("PackDir: %v", err)
	}
	if len(res.Pages) != 1 {
		t.Fatalf("got %d pages, want 1", len(res.Pages))
	}
	written, err := res.Write(outDir)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if len(written) != 2 {
		t.Fatalf("wrote %d files, want 2: %v", len(written), written)
	}

	jsonBytes, err := os.ReadFile(filepath.Join(outDir, "sprites.json"))
	if err != nil {
		t.Fatal(err)
	}
	pngFile, err := os.Open(filepath.Join(outDir, "sprites.png"))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(pngFile)
	_ = pngFile.Close()
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	atlasImg, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded atlas is %T, want *image.NRGBA", decoded)
	}

	atlas, err := sprite.LoadAtlas(core.ImageSource{URI: "asset:sprites.png"}, jsonBytes)
	if err != nil {
		t.Fatalf("sprite.LoadAtlas: %v", err)
	}
	if len(atlas.Frames) != len(originals) {
		t.Fatalf("loaded %d frames, want %d", len(atlas.Frames), len(originals))
	}

	for name, orig := range originals {
		fr, ok := atlas.Frames[name]
		if !ok {
			t.Fatalf("missing frame %q", name)
		}
		assertFrameMatchesOriginal(t, name, atlasImg, fr, orig)
	}

	// Verify exact trim offsets on asymmetric trimmed frame and fully transparent frame.
	trimmed := atlas.Frames["trimmed.png"]
	if trimmed.OffsetX != 3 || trimmed.OffsetY != 5 || trimmed.W != 14 || trimmed.H != 13 || trimmed.Width != 24 || trimmed.Height != 20 {
		t.Errorf("trimmed.png frame = %+v, want offset (3,5), size 14x13 in 24x20", trimmed)
	}
	empty := atlas.Frames["empty.png"]
	if empty.W != 0 || empty.H != 0 || empty.Width != 12 || empty.Height != 10 {
		t.Errorf("empty.png frame = %+v, want 0x0 in 12x10", empty)
	}

	// Verify minimum padding between every pair of non-empty frames.
	pageFrames := res.Pages[0].Frames
	for i := range pageFrames {
		if pageFrames[i].Frame.Empty() {
			continue
		}
		a := pageFrames[i].Frame
		for j := i + 1; j < len(pageFrames); j++ {
			if pageFrames[j].Frame.Empty() {
				continue
			}
			b := pageFrames[j].Frame
			expanded := image.Rect(a.Min.X, a.Min.Y, a.Max.X+opts.Padding, a.Max.Y+opts.Padding)
			if expanded.Overlaps(b) {
				t.Errorf("frames %q (%v) and %q (%v) violate %dpx padding", pageFrames[i].Name, a, pageFrames[j].Name, b, opts.Padding)
			}
		}
	}
}

func TestPackedAtlasDrawsExpectedBatchOps(t *testing.T) {
	inDir := t.TempDir()
	writeTestPNG(t, filepath.Join(inDir, "hero.png"), patternImage(16, 16, image.Rect(0, 0, 16, 16), 10, false))
	writeTestPNG(t, filepath.Join(inDir, "coin.png"), patternImage(16, 16, image.Rect(4, 3, 12, 13), 11, false))
	writeTestPNG(t, filepath.Join(inDir, "blank.png"), image.NewNRGBA(image.Rect(0, 0, 16, 16)))

	opts := pack.DefaultOptions()
	opts.Padding = 1
	res, err := pack.PackDir(inDir, opts)
	if err != nil {
		t.Fatal(err)
	}
	img := core.ImageSource{URI: "asset:" + res.Pages[0].ImageName}
	atlas, err := sprite.LoadAtlas(img, res.Pages[0].JSON)
	if err != nil {
		t.Fatalf("sprite.LoadAtlas: %v", err)
	}

	heroFrame := atlas.Frames["hero.png"]
	coinFrame := atlas.Frames["coin.png"]
	blankFrame := atlas.Frames["blank.png"]

	sHero := sprite.NewSprite(heroFrame)
	sHero.X, sHero.Y, sHero.Z = 10, 20, 2

	sCoin := sprite.NewSprite(coinFrame)
	sCoin.X, sCoin.Y, sCoin.Z = 50, 30, 1

	sCoinFlipped := sprite.NewSprite(coinFrame)
	sCoinFlipped.X, sCoinFlipped.Y, sCoinFlipped.FlipX, sCoinFlipped.Z = 100, 30, true, 1

	sBlank := sprite.NewSprite(blankFrame)
	sBlank.X, sBlank.Y, sBlank.Z = 200, 200, 3

	batch := &sprite.Batch{
		Atlas:   atlas,
		Sprites: []*sprite.Sprite{&sHero, &sCoin, &sCoinFlipped, &sBlank},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "batch", Style: core.Style{Width: core.Pt(300), Height: core.Pt(200)}, Draw: func(d *core.Drawing, _ core.Size) {
				batch.Draw(d)
			}},
			core.Canvas{TestID: "hand", Style: core.Style{Width: core.Pt(300), Height: core.Pt(200)}, Draw: func(d *core.Drawing, _ core.Size) {
				// Z=1 coin: trimmed 8x10 at offset (4,3), drawn at (50+4, 30+3).
				d.DrawImageFrom(img, coinFrame.X, coinFrame.Y, 8, 10, 54, 33, 8, 10)
				// Z=1 flipped coin: mirrored inside its 16x16 untrimmed box at (100,30).
				d.Save().Transform(-1, 0, 0, 1, 116, 30).DrawImageFrom(img, coinFrame.X, coinFrame.Y, 8, 10, 4, 3, 8, 10).Restore()
				// Z=2 hero: untrimmed 16x16 at (10,20).
				d.DrawImageFrom(img, heroFrame.X, heroFrame.Y, 16, 16, 10, 20, 16, 16)
				// Z=3 blank: fully transparent, emits no draw op.
			}},
		}}
	})
	sameOps(t, app.Find("batch").Ops, app.Find("hand").Ops)
}

func TestUntrimmedAndScaledPacking(t *testing.T) {
	orig := patternImage(20, 16, image.Rect(4, 2, 16, 14), 7, false)
	opts := pack.Options{
		Name:    "sheet@2x",
		Padding: 2,
		Scale:   2,
		Trim:    false,
	}
	res, err := pack.PackImages([]pack.Input{{Name: "icon.png", Image: orig}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	page := res.Pages[0]
	if page.Frames[0].Trimmed {
		t.Fatalf("expected Trimmed=false when Trim option is disabled")
	}
	atlas, err := sprite.LoadAtlas(core.ImageSource{URI: "asset:sheet@2x.png"}, page.JSON)
	if err != nil {
		t.Fatalf("sprite.LoadAtlas: %v", err)
	}
	fr := atlas.Frames["icon.png"]
	// Scale 2 halves 20x16 pixels into 10x8 points.
	if fr.X != 0 || fr.Y != 0 || fr.W != 10 || fr.H != 8 || fr.Width != 10 || fr.Height != 8 || fr.OffsetX != 0 || fr.OffsetY != 0 {
		t.Fatalf("scaled frame = %+v, want 0,0 10x8 untrimmed", fr)
	}
	// Pixel-level check on the raw pixel coordinates before scale division.
	raw := page.Frames[0]
	for y := range 16 {
		for x := range 20 {
			if g, w := page.Image.NRGBAAt(raw.Frame.Min.X+x, raw.Frame.Min.Y+y), orig.NRGBAAt(x, y); g != w {
				t.Fatalf("pixel (%d,%d) = %+v, want %+v", x, y, g, w)
			}
		}
	}
}

func TestExtrudeCopiesBorderAndCornerPixels(t *testing.T) {
	orig := patternImage(6, 5, image.Rect(1, 1, 5, 4), 9, false) // trimmed to 4x3
	opts := pack.DefaultOptions()
	opts.Extrude = 2
	opts.Padding = 2

	res, err := pack.PackImages([]pack.Input{
		{Name: "a.png", Image: orig},
		{Name: "b.png", Image: patternImage(4, 4, image.Rect(0, 0, 4, 4), 12, false)},
	}, opts)
	if err != nil {
		t.Fatal(err)
	}
	page := res.Pages[0]
	atlas, err := sprite.LoadAtlas(core.ImageSource{URI: "asset:atlas.png"}, page.JSON)
	if err != nil {
		t.Fatal(err)
	}
	assertFrameMatchesOriginal(t, "a.png", page.Image, atlas.Frames["a.png"], orig)

	var aFrame pack.Frame
	for _, f := range page.Frames {
		if f.Name == "a.png" {
			aFrame = f
		}
	}
	r := aFrame.Frame
	tl := page.Image.NRGBAAt(r.Min.X, r.Min.Y)
	tr := page.Image.NRGBAAt(r.Max.X-1, r.Min.Y)
	bl := page.Image.NRGBAAt(r.Min.X, r.Max.Y-1)
	br := page.Image.NRGBAAt(r.Max.X-1, r.Max.Y-1)

	for e := 1; e <= opts.Extrude; e++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if got, want := page.Image.NRGBAAt(x, r.Min.Y-e), page.Image.NRGBAAt(x, r.Min.Y); got != want {
				t.Fatalf("top extrude (%d,%d) = %+v, want %+v", x, r.Min.Y-e, got, want)
			}
			if got, want := page.Image.NRGBAAt(x, r.Max.Y-1+e), page.Image.NRGBAAt(x, r.Max.Y-1); got != want {
				t.Fatalf("bottom extrude (%d,%d) = %+v, want %+v", x, r.Max.Y-1+e, got, want)
			}
		}
		for y := r.Min.Y; y < r.Max.Y; y++ {
			if got, want := page.Image.NRGBAAt(r.Min.X-e, y), page.Image.NRGBAAt(r.Min.X, y); got != want {
				t.Fatalf("left extrude (%d,%d) = %+v, want %+v", r.Min.X-e, y, got, want)
			}
			if got, want := page.Image.NRGBAAt(r.Max.X-1+e, y), page.Image.NRGBAAt(r.Max.X-1, y); got != want {
				t.Fatalf("right extrude (%d,%d) = %+v, want %+v", r.Max.X-1+e, y, got, want)
			}
		}
		for ex := 1; ex <= opts.Extrude; ex++ {
			if got := page.Image.NRGBAAt(r.Min.X-ex, r.Min.Y-e); got != tl {
				t.Fatalf("top-left corner extrude = %+v, want %+v", got, tl)
			}
			if got := page.Image.NRGBAAt(r.Max.X-1+ex, r.Min.Y-e); got != tr {
				t.Fatalf("top-right corner extrude = %+v, want %+v", got, tr)
			}
			if got := page.Image.NRGBAAt(r.Min.X-ex, r.Max.Y-1+e); got != bl {
				t.Fatalf("bottom-left corner extrude = %+v, want %+v", got, bl)
			}
			if got := page.Image.NRGBAAt(r.Max.X-1+ex, r.Max.Y-1+e); got != br {
				t.Fatalf("bottom-right corner extrude = %+v, want %+v", got, br)
			}
		}
	}
}

func TestMultiPagePackingAndDeterminism(t *testing.T) {
	inDir := t.TempDir()
	originals := make(map[string]*image.NRGBA)
	for i := range 6 {
		name := filepath.Join("tiles", string(rune('a'+i))+".png")
		img := patternImage(40, 40, image.Rect(0, 0, 40, 40), uint8(i+1), false)
		originals[filepath.ToSlash(name)] = img
		writeTestPNG(t, filepath.Join(inDir, name), img)
	}

	opts := pack.DefaultOptions()
	opts.Name = "tiles"
	opts.MaxWidth = 90
	opts.MaxHeight = 90
	opts.Padding = 2

	res1, err := pack.PackDir(inDir, opts)
	if err != nil {
		t.Fatal(err)
	}
	res2, err := pack.PackDir(inDir, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res1.Pages) != 2 {
		t.Fatalf("got %d pages, want 2", len(res1.Pages))
	}

	seen := make(map[string]bool)
	for i, p := range res1.Pages {
		wantImg := "tiles-" + string(rune('0'+i)) + ".png"
		wantJSON := "tiles-" + string(rune('0'+i)) + ".json"
		if p.ImageName != wantImg || p.JSONName != wantJSON {
			t.Errorf("page %d names = (%q, %q), want (%q, %q)", i, p.ImageName, p.JSONName, wantImg, wantJSON)
		}
		if p.Image.Bounds().Dx() > opts.MaxWidth || p.Image.Bounds().Dy() > opts.MaxHeight {
			t.Errorf("page %d size %v exceeds max %dx%d", i, p.Image.Bounds().Size(), opts.MaxWidth, opts.MaxHeight)
		}
		if !bytes.Equal(p.JSON, res2.Pages[i].JSON) {
			t.Errorf("page %d JSON is not deterministic", i)
		}
		if !bytes.Equal(p.Image.Pix, res2.Pages[i].Image.Pix) {
			t.Errorf("page %d pixels are not deterministic", i)
		}

		atlas, err := sprite.LoadAtlas(core.ImageSource{URI: "asset:" + p.ImageName}, p.JSON)
		if err != nil {
			t.Fatalf("page %d LoadAtlas: %v", i, err)
		}
		for name, fr := range atlas.Frames {
			if seen[name] {
				t.Fatalf("duplicate frame %q across pages", name)
			}
			seen[name] = true
			assertFrameMatchesOriginal(t, name, p.Image, fr, originals[name])
		}
	}
	if len(seen) != len(originals) {
		t.Fatalf("packed %d frames across pages, want %d", len(seen), len(originals))
	}
}

func TestAllTransparentFolderProducesValidPNGAndAtlas(t *testing.T) {
	inDir := t.TempDir()
	outDir := t.TempDir()
	writeTestPNG(t, filepath.Join(inDir, "empty.png"), image.NewNRGBA(image.Rect(0, 0, 8, 8)))

	res, err := pack.PackDir(inDir, pack.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.Write(outDir); err != nil {
		t.Fatalf("Write: %v", err)
	}
	atlas, err := sprite.LoadAtlas(core.ImageSource{URI: "asset:atlas.png"}, res.Pages[0].JSON)
	if err != nil {
		t.Fatalf("sprite.LoadAtlas: %v", err)
	}
	if fr := atlas.Frames["empty.png"]; fr.W != 0 || fr.H != 0 || fr.Width != 8 || fr.Height != 8 {
		t.Fatalf("empty.png frame = %+v", fr)
	}
}

func TestPackErrors(t *testing.T) {
	small := patternImage(8, 8, image.Rect(0, 0, 8, 8), 1, false)
	big := patternImage(65, 16, image.Rect(0, 0, 65, 16), 2, false)

	cases := []struct {
		name   string
		inputs []pack.Input
		opts   pack.Options
		want   string
	}{
		{
			name:   "exceeds max width",
			inputs: []pack.Input{{Name: "big.png", Image: big}},
			opts:   pack.Options{MaxWidth: 64, MaxHeight: 64, Trim: true},
			want:   "exceeds max page size",
		},
		{
			name:   "exceeds max width with extrude",
			inputs: []pack.Input{{Name: "edge.png", Image: patternImage(63, 10, image.Rect(0, 0, 63, 10), 1, false)}},
			opts:   pack.Options{MaxWidth: 64, MaxHeight: 64, Extrude: 1, Trim: true},
			want:   "exceeds max page size",
		},
		{
			name:   "empty inputs",
			inputs: nil,
			opts:   pack.DefaultOptions(),
			want:   "no input images",
		},
		{
			name:   "duplicate names",
			inputs: []pack.Input{{Name: "a.png", Image: small}, {Name: "a.png", Image: small}},
			opts:   pack.DefaultOptions(),
			want:   "duplicate",
		},
		{
			name:   "nil image",
			inputs: []pack.Input{{Name: "a.png", Image: nil}},
			opts:   pack.DefaultOptions(),
			want:   "nil image",
		},
		{
			name:   "negative padding",
			inputs: []pack.Input{{Name: "a.png", Image: small}},
			opts:   pack.Options{Padding: -1},
			want:   "padding",
		},
		{
			name:   "negative extrude",
			inputs: []pack.Input{{Name: "a.png", Image: small}},
			opts:   pack.Options{Extrude: -1},
			want:   "extrude",
		},
		{
			name:   "negative scale",
			inputs: []pack.Input{{Name: "a.png", Image: small}},
			opts:   pack.Options{Scale: -2},
			want:   "scale",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pack.PackImages(tc.inputs, tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
		})
	}

	t.Run("empty dir", func(t *testing.T) {
		_, err := pack.PackDir(t.TempDir(), pack.DefaultOptions())
		if err == nil || !strings.Contains(err.Error(), "no PNG files") {
			t.Fatalf("err = %v, want no PNG files", err)
		}
	})

	t.Run("corrupt png", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "bad.png"), []byte("not a png"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := pack.PackDir(dir, pack.DefaultOptions())
		if err == nil || !strings.Contains(err.Error(), "decode") {
			t.Fatalf("err = %v, want decode error", err)
		}
	})
}

// sameOps fails t unless got and want are identical draw operations within float rounding.
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
		g.X, g.Y, g.X1, g.Y1, g.X2, g.Y2, g.W, g.H, g.R, g.A, g.A0, g.A1, g.RY = w.X, w.Y, w.X1, w.Y1, w.X2, w.Y2, w.W, w.H, w.R, w.A, w.A0, w.A1, w.RY
		g.Image = w.Image
		if !reflect.DeepEqual(g, w) {
			t.Fatalf("op %d is %+v, want %+v", i, got[i], want[i])
		}
	}
}
