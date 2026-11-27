package sprite_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// makeEffectAtlas builds a 32x16 atlas with two 16x16 frames packed edge to
// edge (no gutter) so outline tests also catch any neighbor-frame bleed:
//   - "hero" (0..15): a 10x10 centered block (3..12) containing red, blue,
//     pure black (to prove flash brightens black unlike multiply tint), and a
//     50%-alpha top-left accent pixel at (3, 3), surrounded by transparent pixels.
//   - "gem" (16..31): an opaque 16x16 bright green square filling the entire
//     adjacent frame right up to x=16.
func makeEffectAtlas() (*sprite.Atlas, *image.RGBA) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 16))
	// Frame 0: "hero" (0..15, 0..15).
	for y := 3; y <= 12; y++ {
		for x := 3; x <= 12; x++ {
			switch {
			case x == 3 && y == 3:
				// Premultiplied 50%-alpha red: straight (255, 0, 0) at A=128 -> (128, 0, 0, 128).
				img.SetRGBA(x, y, color.RGBA{R: 128, G: 0, B: 0, A: 128})
			case y < 7:
				// Pure red.
				img.SetRGBA(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
			case y < 10:
				// Pure blue.
				img.SetRGBA(x, y, color.RGBA{R: 0, G: 0, B: 255, A: 255})
			default:
				// Pure black (opaque) — multiply tint cannot brighten this, solid flash can.
				img.SetRGBA(x, y, color.RGBA{R: 0, G: 0, B: 0, A: 255})
			}
		}
	}
	// Frame 1: "gem" (16..31, 0..15) completely filled green.
	for y := range 16 {
		for x := 16; x < 32; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 0, G: 220, B: 80, A: 255})
		}
	}

	atlas := &sprite.Atlas{
		Image: core.ImageSource{Pixels: img, Version: 1},
		Frames: map[string]sprite.Frame{
			"hero": {Name: "hero", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			"gem":  {Name: "gem", X: 16, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			// Tightly cropped frame whose opaque green pixels reach all 4 edges and
			// directly touch more opaque green pixels at x=24 and y=10 in the atlas.
			"tight": {Name: "tight", X: 16, Y: 0, W: 8, H: 10, OffsetX: 4, OffsetY: 3, Width: 16, Height: 16},
		},
	}
	return atlas, img
}

// diffRegion counts pixels that differ between a and b inside rect.
func diffRegion(a, b *image.RGBA, rect image.Rectangle) int {
	n := 0
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				n++
			}
		}
	}
	return n
}

// diffOutside counts pixels that differ between a and b outside excl.
func diffOutside(a, b *image.RGBA, excl image.Rectangle) int {
	bounds := a.Bounds()
	n := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if image.Pt(x, y).In(excl) {
				continue
			}
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				n++
			}
		}
	}
	return n
}

func renderBatchScene(atlas *sprite.Atlas, sprites []*sprite.Sprite, w, h int, bg core.Color) (*image.RGBA, []core.DrawOp) {
	batch := &sprite.Batch{Atlas: atlas, Sprites: sprites}
	var d core.Drawing
	if bg != 0 {
		d.SetFillColor(bg).FillRect(0, 0, float32(w), float32(h))
	}
	batch.Draw(&d)
	return gametest.RenderOps(w, h, d.Ops), d.Ops
}

func TestPerSpriteEffectsLeaveNeighborsAndBackgroundUnchanged(t *testing.T) {
	const (
		canvasW = 96
		canvasH = 40
	)
	bg := core.RGB(0x1b263b)
	bgRGBA := color.RGBA{R: 0x1b, G: 0x26, B: 0x3b, A: 0xff}

	atlas, _ := makeEffectAtlas()
	makeScene := func(midEffect *sprite.Effect) (*image.RGBA, image.Rectangle, image.Rectangle, image.Rectangle) {
		left := sprite.NewSprite(atlas.Frames["hero"])
		left.X, left.Y = 4, 12

		mid := sprite.NewSprite(atlas.Frames["hero"])
		mid.X, mid.Y = 40, 12
		mid.Effect = midEffect

		right := sprite.NewSprite(atlas.Frames["gem"])
		right.X, right.Y = 76, 12

		img, _ := renderBatchScene(atlas, []*sprite.Sprite{&left, &mid, &right}, canvasW, canvasH, bg)
		leftRect := image.Rect(4, 12, 20, 28)
		midRect := image.Rect(40, 12, 56, 28)
		rightRect := image.Rect(76, 12, 92, 28)
		return img, leftRect, midRect, rightRect
	}

	baseline, leftRect, midRect, rightRect := makeScene(nil)

	t.Run("flash_half_white", func(t *testing.T) {
		got, _, _, _ := makeScene(&sprite.Effect{
			Flash: sprite.Flash{Color: core.RGB(0xffffff), Strength: 0.5},
		})
		if n := diffOutside(got, baseline, midRect); n != 0 {
			t.Fatalf("%d pixels changed outside target sprite (neighbors/background must be unchanged)", n)
		}
		if n := diffRegion(got, baseline, leftRect); n != 0 {
			t.Fatalf("left neighbor changed by %d pixels", n)
		}
		if n := diffRegion(got, baseline, rightRect); n != 0 {
			t.Fatalf("right neighbor changed by %d pixels", n)
		}
		// Transparent margin inside mid's box keeps exact backdrop color.
		if p := got.RGBAAt(40, 12); p != bgRGBA {
			t.Fatalf("transparent sprite margin = %v, want backdrop %v", p, bgRGBA)
		}
		// Pure black pixel at local (5, 11) -> canvas (45, 23) flashes halfway to white (128,128,128).
		pBlack := got.RGBAAt(45, 23)
		if pBlack.R < 126 || pBlack.R > 129 || pBlack.G < 126 || pBlack.G > 129 || pBlack.B < 126 || pBlack.B > 129 || pBlack.A != 255 {
			t.Fatalf("black pixel flashed 50%% white = %v, want ~(128,128,128,255)", pBlack)
		}
		// Pure red pixel at local (5, 5) -> canvas (45, 17) becomes (255, 128, 128, 255).
		pRed := got.RGBAAt(45, 17)
		if pRed.R != 255 || pRed.G < 126 || pRed.G > 129 || pRed.B < 126 || pRed.B > 129 {
			t.Fatalf("red pixel flashed 50%% white = %v, want ~(255,128,128,255)", pRed)
		}
	})

	t.Run("silhouette", func(t *testing.T) {
		got, _, _, _ := makeScene(&sprite.Effect{
			Flash: sprite.Silhouette(core.RGB(0x00ff80)),
		})
		if n := diffOutside(got, baseline, midRect); n != 0 {
			t.Fatalf("%d pixels changed outside target sprite", n)
		}
		// Opaque pixels of every original color become exact silhouette color.
		want := color.RGBA{R: 0x00, G: 0xff, B: 0x80, A: 0xff}
		for _, pt := range []image.Point{{45, 17}, {45, 20}, {45, 23}} {
			if p := got.RGBAAt(pt.X, pt.Y); p != want {
				t.Fatalf("silhouette pixel at %v = %v, want %v", pt, p, want)
			}
		}
		// Semi-transparent pixel at local (3,3) -> canvas (43,15) keeps 50% alpha over backdrop.
		pHalf := got.RGBAAt(43, 15)
		if pHalf == want || pHalf == bgRGBA || pHalf.G < 120 {
			t.Fatalf("translucent silhouette pixel = %v, want 50%% blend of #00ff80 over backdrop", pHalf)
		}
		// Transparent margin stays exact backdrop.
		if p := got.RGBAAt(41, 13); p != bgRGBA {
			t.Fatalf("transparent margin = %v, want backdrop %v", p, bgRGBA)
		}
	})

	t.Run("outline_1px", func(t *testing.T) {
		outlineColor := core.RGB(0xffdd00)
		wantOutline := color.RGBA{R: 0xff, G: 0xdd, B: 0x00, A: 0xff}
		got, _, _, _ := makeScene(&sprite.Effect{
			Outline: sprite.Outline{Color: outlineColor, Thickness: 1},
		})
		padded := midRect.Inset(-1)
		if n := diffOutside(got, baseline, padded); n != 0 {
			t.Fatalf("%d pixels changed outside target sprite's 1px outline bounds", n)
		}
		if n := diffRegion(got, baseline, leftRect); n != 0 || diffRegion(got, baseline, rightRect) != 0 {
			t.Fatalf("neighbor sprites were modified by outline")
		}
		// 4-neighbors of the opaque 10x10 block (43..52, 15..24) carry outline color.
		for _, pt := range []image.Point{{45, 14}, {45, 25}, {42, 18}, {53, 18}} {
			if p := got.RGBAAt(pt.X, pt.Y); p != wantOutline {
				t.Fatalf("outline pixel at %v = %v, want %v", pt, p, wantOutline)
			}
		}
		// Interior opaque pixels of mid remain identical to baseline.
		interior := image.Rect(44, 16, 52, 24)
		if n := diffRegion(got, baseline, interior); n != 0 {
			t.Fatalf("outline altered %d interior opaque pixels", n)
		}
	})

	t.Run("outline_2px_on_tight_frame_no_atlas_bleed", func(t *testing.T) {
		// "tight" sits right next to green "gem" at x=16 in the atlas.
		left := sprite.NewSprite(atlas.Frames["hero"])
		left.X, left.Y = 4, 12
		mid := sprite.NewSprite(atlas.Frames["tight"])
		mid.X, mid.Y = 40, 12
		right := sprite.NewSprite(atlas.Frames["gem"])
		right.X, right.Y = 76, 12

		baseTight, _ := renderBatchScene(atlas, []*sprite.Sprite{&left, &mid, &right}, canvasW, canvasH, bg)

		outlineColor := core.RGB(0xff00aa)
		wantOutline := color.RGBA{R: 0xff, G: 0x00, B: 0xaa, A: 0xff}
		mid.Effect = &sprite.Effect{
			Outline: sprite.Outline{Color: outlineColor, Thickness: 2},
		}
		gotTight, _ := renderBatchScene(atlas, []*sprite.Sprite{&left, &mid, &right}, canvasW, canvasH, bg)

		// "tight" occupies [44, 52) x [15, 25); with 2px pad -> [42, 54) x [13, 27).
		padded := image.Rect(42, 13, 54, 27)
		if n := diffOutside(gotTight, baseTight, padded); n != 0 {
			t.Fatalf("%d pixels changed outside tight sprite's 2px outline bounds", n)
		}
		// Right and bottom edges outside the tight frame box (where the atlas itself
		// has adjacent opaque green pixels) must carry the outline color, never bleed.
		for _, pt := range []image.Point{{52, 18}, {53, 18}, {48, 25}, {48, 26}, {42, 18}, {43, 18}} {
			if p := gotTight.RGBAAt(pt.X, pt.Y); p != wantOutline {
				t.Fatalf("outline pixel at %v = %v, want %v (check clipping/atlas bleed)", pt, p, wantOutline)
			}
		}
	})

	t.Run("dissolve_partial_with_edge_glow", func(t *testing.T) {
		edgeColor := core.RGB(0xff6600)
		edgeRGBA := color.RGBA{R: 0xff, G: 0x66, B: 0x00, A: 0xff}
		got, _, _, _ := makeScene(&sprite.Effect{
			Dissolve: sprite.Dissolve{
				Threshold: 0.45,
				EdgeWidth: 0.2,
				EdgeColor: edgeColor,
				Seed:      42,
			},
		})
		if n := diffOutside(got, baseline, midRect); n != 0 {
			t.Fatalf("%d pixels changed outside target sprite during dissolve", n)
		}
		var dissolved, edge, intact int
		for y := 16; y <= 24; y++ {
			for x := 44; x <= 52; x++ {
				p := got.RGBAAt(x, y)
				switch {
				case p == bgRGBA:
					dissolved++
				case p == edgeRGBA:
					edge++
				case p == baseline.RGBAAt(x, y):
					intact++
				}
			}
		}
		if dissolved == 0 || edge == 0 || intact == 0 {
			t.Fatalf("expected mix of dissolved (%d), edge (%d), and intact (%d) pixels", dissolved, edge, intact)
		}
	})

	t.Run("dissolve_full", func(t *testing.T) {
		got, _, _, _ := makeScene(&sprite.Effect{
			Dissolve: sprite.Dissolve{Threshold: 1},
		})
		if n := diffOutside(got, baseline, midRect); n != 0 {
			t.Fatalf("%d pixels changed outside target sprite on full dissolve", n)
		}
		for y := midRect.Min.Y; y < midRect.Max.Y; y++ {
			for x := midRect.Min.X; x < midRect.Max.X; x++ {
				if p := got.RGBAAt(x, y); p != bgRGBA {
					t.Fatalf("fully dissolved pixel at (%d,%d) = %v, want backdrop %v", x, y, p, bgRGBA)
				}
			}
		}
	})

	t.Run("dissolve_custom_noise_map", func(t *testing.T) {
		// Horizontal ramp: left half < 0.5, right half >= 0.5.
		noise := image.NewGray(image.Rect(0, 0, 16, 16))
		for y := range 16 {
			for x := range 16 {
				v := uint8(0)
				if x >= 8 {
					v = 255
				}
				noise.SetGray(x, y, color.Gray{Y: v})
			}
		}
		got, _, _, _ := makeScene(&sprite.Effect{
			Dissolve: sprite.Dissolve{Threshold: 0.5, Noise: noise},
		})
		if n := diffOutside(got, baseline, midRect); n != 0 {
			t.Fatalf("%d pixels changed outside target sprite with custom noise map", n)
		}
		// Left half of mid's opaque block (local x=4 -> canvas x=44) dissolved to backdrop.
		if p := got.RGBAAt(44, 17); p != bgRGBA {
			t.Fatalf("left half pixel = %v, want dissolved backdrop %v", p, bgRGBA)
		}
		// Right half of mid's opaque block (local x=10 -> canvas x=50) survived intact.
		if p := got.RGBAAt(50, 17); p != baseline.RGBAAt(50, 17) {
			t.Fatalf("right half pixel = %v, want intact %v", p, baseline.RGBAAt(50, 17))
		}
	})

	t.Run("palette_swap", func(t *testing.T) {
		got, _, _, _ := makeScene(&sprite.Effect{
			PaletteSwap: sprite.PaletteSwap{
				Entries: []sprite.PaletteEntry{
					{From: core.RGB(0xff0000), To: core.RGB(0x00e0ff)},
					{From: core.RGB(0x0000ff), To: core.RGB(0xffd000)},
				},
			},
		})
		if n := diffOutside(got, baseline, midRect); n != 0 {
			t.Fatalf("%d pixels changed outside target sprite during palette swap", n)
		}
		if p := got.RGBAAt(45, 17); p != (color.RGBA{R: 0x00, G: 0xe0, B: 0xff, A: 0xff}) {
			t.Fatalf("swapped red pixel = %v, want cyan", p)
		}
		if p := got.RGBAAt(45, 20); p != (color.RGBA{R: 0xff, G: 0xd0, B: 0x00, A: 0xff}) {
			t.Fatalf("swapped blue pixel = %v, want gold", p)
		}
		// Unmatched black pixel stays black.
		if p := got.RGBAAt(45, 23); p != (color.RGBA{R: 0, G: 0, B: 0, A: 255}) {
			t.Fatalf("unmatched black pixel = %v, want black", p)
		}
		// Translucent red pixel at (43, 15) remaps to translucent cyan over backdrop.
		pHalf := got.RGBAAt(43, 15)
		if pHalf.B < 120 || pHalf.G < 100 || pHalf.R > 30 {
			t.Fatalf("translucent swapped pixel = %v, want 50%% cyan over backdrop", pHalf)
		}
	})

	t.Run("custom_pixel_hook", func(t *testing.T) {
		got, _, _, _ := makeScene(&sprite.Effect{
			Custom: func(x, y, w, h int, r, g, b, a float32) (float32, float32, float32, float32) {
				if a <= 0 {
					return 0, 0, 0, 0
				}
				// Invert RGB while keeping alpha.
				return a - r, a - g, a - b, a
			},
		})
		if n := diffOutside(got, baseline, midRect); n != 0 {
			t.Fatalf("%d pixels changed outside target sprite with Custom hook", n)
		}
		// Black opaque pixel at (45, 23) inverted becomes pure white.
		if p := got.RGBAAt(45, 23); p != (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
			t.Fatalf("inverted black pixel = %v, want white", p)
		}
	})

	t.Run("combined_stages", func(t *testing.T) {
		got, _, _, _ := makeScene(&sprite.Effect{
			PaletteSwap: sprite.PaletteSwap{
				Entries: []sprite.PaletteEntry{{From: core.RGB(0xff0000), To: core.RGB(0x00ff00)}},
			},
			Flash:   sprite.Flash{Color: core.RGB(0xffffff), Strength: 0.5},
			Outline: sprite.Outline{Color: core.RGB(0x2080ff), Thickness: 1},
		})
		if n := diffOutside(got, baseline, midRect.Inset(-1)); n != 0 {
			t.Fatalf("%d pixels changed outside padded bounds on combined effect", n)
		}
		// Outline sits around the sprite in solid blue (not washed out by Flash).
		if p := got.RGBAAt(45, 14); p != (color.RGBA{R: 0x20, G: 0x80, B: 0xff, A: 0xff}) {
			t.Fatalf("outline pixel = %v, want #2080ff", p)
		}
		// Swapped green pixel flashed 50% white -> (128, 255, 128).
		p := got.RGBAAt(45, 17)
		if p.G != 255 || p.R < 126 || p.R > 129 || p.B < 126 || p.B > 129 {
			t.Fatalf("swapped+flashed pixel = %v, want ~(128,255,128,255)", p)
		}
	})
}

func TestEffectParametersChangeEveryFrameWithoutLeaks(t *testing.T) {
	atlas, _ := makeEffectAtlas()
	fx := &sprite.Effect{}
	hero := sprite.NewSprite(atlas.Frames["hero"])
	hero.X, hero.Y = 8, 8
	hero.Effect = fx

	neighbor := sprite.NewSprite(atlas.Frames["gem"])
	neighbor.X, neighbor.Y = 32, 8

	batch := &sprite.Batch{Atlas: atlas, Sprites: []*sprite.Sprite{&hero, &neighbor}}

	// Frame 0: inactive Effect keeps both sprites in a single DrawAtlas call.
	var d core.Drawing
	batch.Draw(&d)
	if len(d.Ops) != 1 || d.Ops[0].Kind != core.DrawAtlas {
		t.Fatalf("inactive Effect broke DrawAtlas batching: %+v", d.Ops)
	}

	// Animate Dissolve.Threshold monotonically across 20 frames.
	prevOpaque := math.MaxInt
	var prevVersion uint64
	for frame := 1; frame <= 20; frame++ {
		fx.Dissolve = sprite.Dissolve{
			Threshold: float32(frame) / 21.0,
			Seed:      99,
		}
		d.Ops = d.Ops[:0]
		batch.Draw(&d)

		draws := gametest.ImageDraws(d.Ops)
		if len(draws) != 2 {
			t.Fatalf("frame %d: got %d image draws, want 2", frame, len(draws))
		}
		v := draws[0].Image.Version
		if v <= prevVersion {
			t.Fatalf("frame %d: ImageSource.Version did not advance (%d <= %d)", frame, v, prevVersion)
		}
		prevVersion = v

		rendered := gametest.RenderOps(64, 32, d.Ops)
		opaque := 0
		for y := 8; y < 24; y++ {
			for x := 8; x < 24; x++ {
				if rendered.RGBAAt(x, y).A > 0 {
					opaque++
				}
			}
		}
		if opaque > prevOpaque {
			t.Fatalf("frame %d: dissolve opaque pixel count increased from %d to %d", frame, prevOpaque, opaque)
		}
		prevOpaque = opaque
	}
	if prevOpaque >= 100 {
		t.Fatalf("expected dissolve to erode pixels over 20 frames, still had %d opaque pixels", prevOpaque)
	}

	// Unchanged parameters across consecutive frames must reuse the exact same Version (no re-render / upload).
	d.Ops = d.Ops[:0]
	batch.Draw(&d)
	vSame := gametest.ImageDraws(d.Ops)[0].Image.Version
	if vSame != prevVersion {
		t.Fatalf("unchanged effect bumped Version from %d to %d", prevVersion, vSame)
	}

	// Animate Flash.Strength every frame and verify steady-state buffer reuse (0 pixel-buffer allocs).
	fx.Dissolve = sprite.Dissolve{}
	fx.Flash = sprite.Flash{Color: core.RGB(0xffffff), Strength: 0.5}
	d.Ops = d.Ops[:0]
	batch.Draw(&d)
	firstBuf := gametest.ImageDraws(d.Ops)[0].Image.Pixels
	var prevBrightness uint8
	for i := 1; i <= 10; i++ {
		fx.Flash.Strength = float32(i) / 10.0
		d.Ops = d.Ops[:0]
		batch.Draw(&d)
		draws := gametest.ImageDraws(d.Ops)
		if draws[0].Image.Pixels != firstBuf {
			t.Fatalf("frame %d allocated a new *image.RGBA instead of reusing the slot buffer", i)
		}
		rendered := gametest.RenderOps(64, 32, d.Ops)
		// Black pixel at local (5, 11) -> canvas (13, 19) grows brighter every frame.
		b := rendered.RGBAAt(13, 19).R
		if b < prevBrightness {
			t.Fatalf("frame %d brightness decreased (%d < %d)", i, b, prevBrightness)
		}
		prevBrightness = b
	}
	if prevBrightness != 255 {
		t.Fatalf("final flash strength=1 brightness = %d, want 255", prevBrightness)
	}

	// Clearing the effect returns the batch to DrawAtlas.
	*fx = sprite.Effect{}
	d.Ops = d.Ops[:0]
	batch.Draw(&d)
	if len(d.Ops) != 1 || d.Ops[0].Kind != core.DrawAtlas {
		t.Fatalf("clearing Effect did not restore DrawAtlas batching: %+v", d.Ops)
	}
}

func TestMultipleEffectedSpritesInSameBatchDoNotClobberBuffers(t *testing.T) {
	atlas, _ := makeEffectAtlas()
	redFlash := sprite.NewSprite(atlas.Frames["hero"])
	redFlash.X, redFlash.Y = 4, 4
	redFlash.Effect = &sprite.Effect{Flash: sprite.Silhouette(core.RGB(0xff2020))}

	greenFlash := sprite.NewSprite(atlas.Frames["hero"])
	greenFlash.X, greenFlash.Y = 28, 4
	greenFlash.Effect = &sprite.Effect{Flash: sprite.Silhouette(core.RGB(0x20ff20))}

	plain := sprite.NewSprite(atlas.Frames["hero"])
	plain.X, plain.Y = 52, 4

	got, _ := renderBatchScene(atlas, []*sprite.Sprite{&redFlash, &greenFlash, &plain}, 76, 24, 0)

	if p := got.RGBAAt(9, 15); p != (color.RGBA{R: 0xff, G: 0x20, B: 0x20, A: 0xff}) {
		t.Fatalf("first effected sprite pixel = %v, want red silhouette", p)
	}
	if p := got.RGBAAt(33, 15); p != (color.RGBA{R: 0x20, G: 0xff, B: 0x20, A: 0xff}) {
		t.Fatalf("second effected sprite pixel = %v, want green silhouette", p)
	}
	if p := got.RGBAAt(57, 15); p != (color.RGBA{R: 0, G: 0, B: 0, A: 0xff}) {
		t.Fatalf("plain sprite pixel = %v, want original black", p)
	}
}

func TestEffectWithTransformsTintFlipAndMask(t *testing.T) {
	atlas, _ := makeEffectAtlas()
	mask := sprite.RectMask(core.Bounds{
		Origin: core.Point{X: 20, Y: 10},
		Size:   core.Size{Width: 16, Height: 16},
	})
	sp := sprite.NewSprite(atlas.Frames["hero"])
	sp.X, sp.Y = 20, 10
	sp.FlipX = true
	sp.Alpha = 0.5
	sp.Tint = core.RGB(0x80ff80)
	sp.Blend = core.CompositeLighter
	sp.Mask = &mask
	sp.Effect = &sprite.Effect{
		Flash:   sprite.Flash{Color: core.RGB(0xffffff), Strength: 0.5},
		Outline: sprite.Outline{Color: core.RGB(0xffcc00), Thickness: 1},
	}

	batch := &sprite.Batch{Atlas: atlas, Sprites: []*sprite.Sprite{&sp}}
	var d core.Drawing
	batch.Draw(&d)

	saves, restores := 0, 0
	hasClip, hasAlpha, hasBlend := false, false, false
	for _, op := range d.Ops {
		switch op.Kind {
		case core.DrawSave:
			saves++
		case core.DrawRestore:
			restores++
		case core.DrawClip:
			hasClip = true
		case core.DrawAlpha:
			hasAlpha = true
		case core.DrawComposite:
			if op.Composite == core.CompositeLighter {
				hasBlend = true
			}
		}
	}
	if saves == 0 || saves != restores {
		t.Fatalf("unbalanced Save/Restore: saves=%d restores=%d", saves, restores)
	}
	if !hasClip || !hasAlpha || !hasBlend {
		t.Fatalf("expected clip=%v alpha=%v blend=%v all true", hasClip, hasAlpha, hasBlend)
	}
}

func TestEffectFromEncodedPNGData(t *testing.T) {
	_, rgba := makeEffectAtlas()
	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	atlas := &sprite.Atlas{
		Image: core.ImageSource{Data: buf.Bytes(), Version: 1},
		Frames: map[string]sprite.Frame{
			"hero": {Name: "hero", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		},
	}
	sp := sprite.NewSprite(atlas.Frames["hero"])
	sp.X, sp.Y = 4, 4
	sp.Effect = &sprite.Effect{Flash: sprite.Silhouette(core.RGB(0x4080ff))}

	got, _ := renderBatchScene(atlas, []*sprite.Sprite{&sp}, 24, 24, 0)
	want := color.RGBA{R: 0x40, G: 0x80, B: 0xff, A: 0xff}
	if p := got.RGBAAt(9, 15); p != want {
		t.Fatalf("encoded PNG silhouette pixel = %v, want %v", p, want)
	}
}

func TestEffectURIOnlyFallback(t *testing.T) {
	// URI-only atlas has neither Pixels nor Data.
	uriAtlas := &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:sheet.png"},
		Frames: map[string]sprite.Frame{
			"hero": {Name: "hero", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		},
	}

	plain := sprite.NewSprite(uriAtlas.Frames["hero"])
	plain.X = 0

	silhouette := sprite.NewSprite(uriAtlas.Frames["hero"])
	silhouette.X = 20
	silhouette.Effect = &sprite.Effect{Flash: sprite.Silhouette(core.RGB(0x000000))}

	gone := sprite.NewSprite(uriAtlas.Frames["hero"])
	gone.X = 40
	gone.Effect = &sprite.Effect{Dissolve: sprite.Dissolve{Threshold: 1}}

	outlined := sprite.NewSprite(uriAtlas.Frames["hero"])
	outlined.X = 60
	outlined.Effect = &sprite.Effect{Outline: sprite.Outline{Color: core.RGB(0xffffff), Thickness: 1}}

	batch := &sprite.Batch{Atlas: uriAtlas, Sprites: []*sprite.Sprite{&plain, &silhouette, &gone, &outlined}}
	var d core.Drawing
	batch.Draw(&d)

	draws := gametest.ImageDraws(d.Ops)
	if len(draws) != 3 {
		t.Fatalf("got %d image draws, want 3 (fully dissolved skipped)", len(draws))
	}
	if draws[0].Tinted {
		t.Fatalf("plain sprite should not be tinted")
	}
	if !draws[1].Tinted || draws[1].Tint != core.RGB(0x000000) {
		t.Fatalf("black silhouette URI fallback got tinted=%v tint=%#x, want black tint", draws[1].Tinted, draws[1].Tint)
	}
	if draws[2].Tinted {
		t.Fatalf("outline-only URI fallback should draw sprite untinted")
	}
	// No canvas-wide composite op was mutated.
	for _, op := range d.Ops {
		if op.Kind == core.DrawComposite {
			t.Fatalf("URI fallback unexpectedly set composite %v", op.Composite)
		}
	}
}

func TestEffectApplyHelperAndEdgeCases(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))

	var nilFX *sprite.Effect
	if nilFX.Active() || nilFX.OutlinePadding() != 0 {
		t.Fatalf("nil Effect should be inactive with 0 padding")
	}
	if (&sprite.Effect{
		Flash:    sprite.Flash{Color: core.RGB(0xffffff), Strength: nan},
		Outline:  sprite.Outline{Color: core.RGB(0xffffff), Thickness: nan},
		Dissolve: sprite.Dissolve{Threshold: nan},
	}).Active() {
		t.Fatalf("NaN parameters should be inactive")
	}
	if pad := (&sprite.Effect{Outline: sprite.Outline{Color: core.RGB(0xffffff), Thickness: inf}}).OutlinePadding(); pad != 64 {
		t.Fatalf("infinite outline thickness padding = %d, want clamped 64", pad)
	}
	if out, pad := nilFX.Apply(nil, sprite.Frame{W: 16, H: 16}); out != nil || pad != 0 {
		t.Fatalf("Apply(nil) = (%v, %d), want (nil, 0)", out, pad)
	}

	_, rgba := makeEffectAtlas()
	fx := &sprite.Effect{
		PaletteSwap: sprite.PaletteSwap{
			Entries:   []sprite.PaletteEntry{{From: core.RGB(0xfd0201), To: core.RGB(0x102030)}},
			Tolerance: 3,
		},
		Outline: sprite.Outline{Color: core.RGB(0xffffff), Thickness: 0.5},
	}
	out, pad := fx.Apply(rgba, sprite.Frame{X: 0, Y: 0, W: 16, H: 16})
	if pad != 1 || out.Bounds().Dx() != 18 || out.Bounds().Dy() != 18 {
		t.Fatalf("Apply with Thickness 0.5 got bounds %v pad %d, want 18x18 pad 1", out.Bounds(), pad)
	}
	// Red pixel (255,0,0) matched (253,2,1) within Tolerance 3 -> (0x10, 0x20, 0x30).
	if p := out.RGBAAt(5+pad, 5+pad); p != (color.RGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xff}) {
		t.Fatalf("tolerance palette swap pixel = %v, want (16,32,48,255)", p)
	}
	// Fractional outline thickness 0.5 produces ~50%-alpha white outline around the opaque block.
	if p := out.RGBAAt(5+pad, 2+pad); p.A < 126 || p.A > 129 {
		t.Fatalf("fractional thickness=0.5 outline pixel = %v, want ~50%% alpha white", p)
	}
}

func TestEffectShaderProgramCompilesAndBindsUniforms(t *testing.T) {
	prog, err := sprite.EffectProgram()
	if err != nil {
		t.Fatalf("EffectProgram() failed: %v", err)
	}
	if prog == nil {
		t.Fatal("EffectProgram() returned nil program")
	}

	atlas, _ := makeEffectAtlas()
	noise := image.NewGray(image.Rect(0, 0, 8, 8))
	sp := sprite.NewSprite(atlas.Frames["hero"])
	sp.ScaleX, sp.ScaleY = 2, 2
	sp.Effect = &sprite.Effect{
		Flash:   sprite.Flash{Color: core.RGB(0xffffff), Strength: 0.4},
		Outline: sprite.Outline{Color: core.RGB(0xffcc00), Thickness: 2},
		Dissolve: sprite.Dissolve{
			Threshold: 0.3,
			EdgeWidth: 0.1,
			EdgeColor: core.RGB(0xff4400),
			Seed:      7,
			Scale:     2,
			Noise:     noise,
		},
		PaletteSwap: sprite.PaletteSwap{
			Entries: []sprite.PaletteEntry{
				{From: core.RGB(0xff0000), To: core.RGB(0x00ffff)},
			},
			Tolerance: 2,
		},
	}

	sh, err := sprite.EffectShader(atlas, sp)
	if err != nil {
		t.Fatalf("EffectShader() failed: %v", err)
	}
	if len(sh.Images) != 2 {
		t.Fatalf("EffectShader attached %d images, want 2 (atlas + noise)", len(sh.Images))
	}
	// Frame 16x16 + 2px pad each side = 20x20, scaled 2x = 40x40.
	if sh.Style.Width != core.Pt(40) || sh.Style.Height != core.Pt(40) {
		t.Fatalf("EffectShader size = (%v, %v), want (40, 40)", sh.Style.Width, sh.Style.Height)
	}

	// Verify the shader mounts cleanly in nsgotest with an AnimatedValue driving FlashStrength.
	var thresh *core.Signal[float64]
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		thresh = core.NewSignal(cx, 0.25)
		pulse := core.NewAnimatedValue(cx, 0.5)
		sh.TestID = "fx-sprite"
		sh.Uniforms["FlashStrength"] = pulse
		sh.UniformsFunc = func() core.Uniforms {
			return core.Uniforms{"DissolveThreshold": thresh.Get()}
		}
		return sh
	})
	if app.Find("fx-sprite") == nil {
		t.Fatal("fx-sprite shader node not found")
	}
	// Update dissolve threshold for the next frame.
	app.Post(func() { thresh.Set(0.75) })
	app.Settle()
	if app.Find("fx-sprite") == nil {
		t.Fatal("fx-sprite missing after uniform update")
	}
}

func BenchmarkSpriteEffects(b *testing.B) {
	makeAtlas := func(size int) *sprite.Atlas {
		img := image.NewRGBA(image.Rect(0, 0, size, size))
		margin := max(1, size/8)
		for y := margin; y < size-margin; y++ {
			for x := margin; x < size-margin; x++ {
				img.SetRGBA(x, y, color.RGBA{R: 220, G: 80, B: 40, A: 255})
			}
		}
		return &sprite.Atlas{
			Image: core.ImageSource{Pixels: img, Version: 1},
			Frames: map[string]sprite.Frame{
				"s": {Name: "s", W: float32(size), H: float32(size), Width: float32(size), Height: float32(size)},
			},
		}
	}

	cases := []struct {
		name string
		size int
		step func(fx *sprite.Effect, i int)
	}{
		{
			name: "flash_16x16_changing",
			size: 16,
			step: func(fx *sprite.Effect, i int) {
				fx.Flash = sprite.Flash{Color: core.RGB(0xffffff), Strength: float32(i%100+1) / 100}
			},
		},
		{
			name: "flash_32x32_changing",
			size: 32,
			step: func(fx *sprite.Effect, i int) {
				fx.Flash = sprite.Flash{Color: core.RGB(0xffffff), Strength: float32(i%100+1) / 100}
			},
		},
		{
			name: "outline1_32x32_changing",
			size: 32,
			step: func(fx *sprite.Effect, i int) {
				fx.Outline = sprite.Outline{Color: core.RGB(0xffdd00), Thickness: 1 + float32(i%2)*0.25}
			},
		},
		{
			name: "outline2_32x32_changing",
			size: 32,
			step: func(fx *sprite.Effect, i int) {
				fx.Outline = sprite.Outline{Color: core.RGB(0xffdd00), Thickness: 2 + float32(i%2)*0.25}
			},
		},
		{
			name: "dissolve_32x32_changing",
			size: 32,
			step: func(fx *sprite.Effect, i int) {
				fx.Dissolve = sprite.Dissolve{
					Threshold: float32(i%98+1) / 100,
					EdgeWidth: 0.1,
					EdgeColor: core.RGB(0xff6600),
					Seed:      1,
				}
			},
		},
		{
			name: "palette_swap_32x32_static_cached",
			size: 32,
			step: func(fx *sprite.Effect, _ int) {
				if len(fx.PaletteSwap.Entries) == 0 {
					fx.PaletteSwap = sprite.PaletteSwap{
						Entries: []sprite.PaletteEntry{{From: core.RGB(0xdc5028), To: core.RGB(0x28a0dc)}},
					}
				}
			},
		},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			atlas := makeAtlas(tc.size)
			fx := &sprite.Effect{}
			sp := sprite.NewSprite(atlas.Frames["s"])
			sp.Effect = fx
			batch := &sprite.Batch{Atlas: atlas, Sprites: []*sprite.Sprite{&sp}}
			var d core.Drawing
			tc.step(fx, 0)
			batch.Draw(&d)
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				d.Ops = d.Ops[:0]
				tc.step(fx, i+1)
				batch.Draw(&d)
			}
		})
	}
}
