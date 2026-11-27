package postfx_test

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/game/pkg/postfx"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// paletteSpriteFrame is a 24 by 16 pixel-art scene with four key colors (armor red, trim gold, tunic blue, outline dark), an unmapped background color, a translucent red banner, and transparent corners, premultiplied.
func paletteSpriteFrame() *image.RGBA {
	f := image.NewRGBA(image.Rect(0, 0, 24, 16))
	fill := func(r image.Rectangle, c color.RGBA) {
		draw.Draw(f, r, image.NewUniform(c), image.Point{}, draw.Src)
	}
	// Unmapped sky background in the interior.
	fill(image.Rect(2, 2, 22, 14), color.RGBA{40, 50, 65, 255})
	// Four mapped bands.
	fill(image.Rect(4, 3, 10, 8), color.RGBA{208, 48, 48, 255})   // key 0: red
	fill(image.Rect(10, 3, 16, 8), color.RGBA{240, 192, 48, 255}) // key 1: gold
	fill(image.Rect(4, 8, 10, 13), color.RGBA{48, 96, 208, 255})  // key 2: blue
	fill(image.Rect(10, 8, 16, 13), color.RGBA{32, 24, 48, 255})  // key 3: dark
	// Translucent pixel of key 0 (straight red at alpha 128, premultiplied).
	fill(image.Rect(17, 4, 21, 12), color.RGBA{104, 24, 24, 128})
	return f
}

// swapPaletteTexture has row 0 = source keys (with a transparent slot at column 4), row 1 = ice theme, row 2 = poison theme.
func swapPaletteTexture() core.ImageSource {
	img := postfx.PaletteImage(
		[]core.Color{core.RGB(0xd03030), core.RGB(0xf0c030), core.RGB(0x3060d0), core.RGB(0x201830), core.Transparent},
		[]core.Color{core.RGB(0x40c8f0), core.RGB(0xe0f8ff), core.RGB(0x205090), core.RGB(0x102038), core.RGB(0xff00ff)},
		[]core.Color{core.RGB(0x8838c8), core.RGB(0xb8f040), core.RGB(0x386030), core.RGB(0x181020), core.RGB(0x00ffff)},
	)
	return core.ImageSource{Pixels: img, Version: 1}
}

// gbRampTexture has row 0 = Game Boy greens, row 1 = warm sepia.
func gbRampTexture() core.ImageSource {
	img := postfx.PaletteImage(
		[]core.Color{core.RGB(0x0f380f), core.RGB(0x306230), core.RGB(0x8bac0f), core.RGB(0x9bbc0f)},
		[]core.Color{core.RGB(0x2b1d14), core.RGB(0x6b4c35), core.RGB(0xb88b5a), core.RGB(0xf2d9b1)},
	)
	return core.ImageSource{Pixels: img, Version: 1}
}

func TestPaletteSwapReplacesKeyColorsAndMatchesReferencePicture(t *testing.T) {
	chain := postfx.MustCompile(postfx.PaletteSwap(swapPaletteTexture()))
	frame := paletteSpriteFrame()

	got, err := chain.Reference(frame, 24, 16, []core.Uniforms{{"Amount": 1, "Row": 1}})
	if err != nil {
		t.Fatal(err)
	}
	gametest.AssertGolden(t, "palette-swap", straight(got))

	// Every key color swapped to its row-1 target.
	for name, tc := range map[string]struct {
		x, y int
		want color.RGBA
	}{
		"red -> ice cyan":     {6, 5, color.RGBA{0x40, 0xc8, 0xf0, 255}},
		"gold -> ice white":   {12, 5, color.RGBA{0xe0, 0xf8, 0xff, 255}},
		"blue -> ice navy":    {6, 10, color.RGBA{0x20, 0x50, 0x90, 255}},
		"dark -> ice shadow":  {12, 10, color.RGBA{0x10, 0x20, 0x38, 255}},
		"unmapped sky stays":  {3, 3, color.RGBA{40, 50, 65, 255}},
		"transparent stays":   {0, 0, color.RGBA{0, 0, 0, 0}},
		"translucent swapped": {18, 6, color.RGBA{32, 100, 120, 128}},
	} {
		if px := got.RGBAAt(tc.x, tc.y); px != tc.want {
			t.Errorf("%s (%d, %d) = %v, want %v", name, tc.x, tc.y, px, tc.want)
		}
	}

	// Row 2 swaps to the poison theme.
	poison, err := chain.Reference(frame, 24, 16, []core.Uniforms{{"Amount": 1, "Row": 2}})
	if err != nil {
		t.Fatal(err)
	}
	if px := poison.RGBAAt(6, 5); px != (color.RGBA{0x88, 0x38, 0xc8, 255}) {
		t.Errorf("row 2 red = %v, want poison purple", px)
	}
	if px := poison.RGBAAt(12, 5); px != (color.RGBA{0xb8, 0xf0, 0x40, 255}) {
		t.Errorf("row 2 gold = %v, want poison lime", px)
	}
}

func TestPaletteSwapBlendsRowsAndAmount(t *testing.T) {
	chain := postfx.MustCompile(postfx.PaletteSwap(swapPaletteTexture()))
	frame := paletteSpriteFrame()

	// Row 1.5 blends halfway between row 1 (0x40, 0xc8, 0xf0) and row 2 (0x88, 0x38, 0xc8).
	midRow, err := chain.Reference(frame, 24, 16, []core.Uniforms{{"Amount": 1, "Row": 1.5}})
	if err != nil {
		t.Fatal(err)
	}
	wantMidRow := color.RGBA{uint8(math.Round((0x40 + 0x88) / 2.0)), uint8(math.Round((0xc8 + 0x38) / 2.0)), uint8(math.Round((0xf0 + 0xc8) / 2.0)), 255}
	if px := midRow.RGBAAt(6, 5); px != wantMidRow {
		t.Errorf("Row 1.5 = %v, want %v", px, wantMidRow)
	}

	// Amount 0.5 blends halfway between source red (0xd0, 0x30, 0x30) and row 1 ice (0x40, 0xc8, 0xf0).
	halfAmt, err := chain.Reference(frame, 24, 16, []core.Uniforms{{"Amount": 0.5, "Row": 1}})
	if err != nil {
		t.Fatal(err)
	}
	wantHalfAmt := color.RGBA{uint8(math.Round((0xd0 + 0x40) / 2.0)), uint8(math.Round((0x30 + 0xc8) / 2.0)), uint8(math.Round((0x30 + 0xf0) / 2.0)), 255}
	if px := halfAmt.RGBAAt(6, 5); px != wantHalfAmt {
		t.Errorf("Amount 0.5 = %v, want %v", px, wantHalfAmt)
	}

	// Out-of-bounds rows clamp cleanly to 0 and height-1.
	clampedHigh, err := chain.Reference(frame, 24, 16, []core.Uniforms{{"Amount": 1, "Row": 99}})
	if err != nil {
		t.Fatal(err)
	}
	row2, _ := chain.Reference(frame, 24, 16, []core.Uniforms{{"Amount": 1, "Row": 2}})
	if !slices.Equal(clampedHigh.Pix, row2.Pix) {
		t.Error("Row 99 did not clamp to the last row")
	}
	clampedLow, err := chain.Reference(frame, 24, 16, []core.Uniforms{{"Amount": 1, "Row": -5}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(clampedLow.Pix, frame.Pix) {
		t.Error("Row -5 did not clamp to row 0 (identity)")
	}
}

func TestPaletteSwapToleranceMatchesExactNearestAndQuantized(t *testing.T) {
	pal := postfx.PaletteImage(
		[]core.Color{core.RGB(0xff0000), core.RGB(0x00ff00), core.RGB(0x0000ff)},
		[]core.Color{core.RGB(0x00ffff), core.RGB(0xff00ff), core.RGB(0xffff00)},
	)
	chain := postfx.MustCompile(postfx.PaletteSwap(core.ImageSource{Pixels: pal, Version: 1}))
	frame := image.NewRGBA(image.Rect(0, 0, 3, 1))
	frame.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})   // exact red
	frame.SetRGBA(1, 0, color.RGBA{254, 0, 0, 255})   // 1/255 off red
	frame.SetRGBA(2, 0, color.RGBA{20, 20, 220, 255}) // near blue

	// Default tolerance (0 -> 0.002): only exact 8-bit red matches.
	exact, err := chain.Reference(frame, 3, 1, []core.Uniforms{{"Amount": 1, "Row": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if exact.RGBAAt(0, 0) != (color.RGBA{0, 255, 255, 255}) {
		t.Errorf("exact red = %v, want cyan", exact.RGBAAt(0, 0))
	}
	if exact.RGBAAt(1, 0) != (color.RGBA{254, 0, 0, 255}) {
		t.Errorf("off-by-1 red = %v, want unchanged", exact.RGBAAt(1, 0))
	}
	if exact.RGBAAt(2, 0) != (color.RGBA{20, 20, 220, 255}) {
		t.Errorf("near blue = %v, want unchanged", exact.RGBAAt(2, 0))
	}

	// Tolerance 0.02 (~5/255): matches exact red and off-by-1 red, not far blue.
	fuzzy, err := chain.Reference(frame, 3, 1, []core.Uniforms{{"Amount": 1, "Row": 1, "Tolerance": 0.02}})
	if err != nil {
		t.Fatal(err)
	}
	if fuzzy.RGBAAt(1, 0) != (color.RGBA{0, 255, 255, 255}) {
		t.Errorf("fuzzy off-by-1 red = %v, want cyan", fuzzy.RGBAAt(1, 0))
	}
	if fuzzy.RGBAAt(2, 0) != (color.RGBA{20, 20, 220, 255}) {
		t.Errorf("fuzzy near blue = %v, want unchanged", fuzzy.RGBAAt(2, 0))
	}

	// Tolerance 1: snaps every pixel to its nearest key (blue -> yellow).
	nearest, err := chain.Reference(frame, 3, 1, []core.Uniforms{{"Amount": 1, "Row": 1, "Tolerance": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if nearest.RGBAAt(2, 0) != (color.RGBA{255, 255, 0, 255}) {
		t.Errorf("nearest blue = %v, want yellow", nearest.RGBAAt(2, 0))
	}

	// Single-row palette with Tolerance 1 quantizes every pixel to the closest palette entry.
	quantPal := postfx.PaletteImage([]core.Color{core.RGB(0x101010), core.RGB(0xf0e020)})
	quantChain := postfx.MustCompile(postfx.PaletteSwap(core.ImageSource{Pixels: quantPal, Version: 1}))
	q, err := quantChain.Reference(frame, 3, 1, []core.Uniforms{{"Amount": 1, "Tolerance": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if q.RGBAAt(2, 0) != (color.RGBA{0x10, 0x10, 0x10, 255}) {
		t.Errorf("quantized blue = %v, want dark", q.RGBAAt(2, 0))
	}
}

func TestPaletteSwapTranslucentReplacementPreservesPremultipliedAlpha(t *testing.T) {
	pal := postfx.PaletteImage(
		[]core.Color{core.RGB(0xff0000), core.RGB(0x00ff00)},
		[]core.Color{core.Transparent, core.RGBA(0x0000ff, 0.5)},
	)
	chain := postfx.MustCompile(postfx.PaletteSwap(core.ImageSource{Pixels: pal, Version: 1}))
	frame := image.NewRGBA(image.Rect(0, 0, 2, 1))
	frame.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})
	frame.SetRGBA(1, 0, color.RGBA{0, 255, 0, 255})

	got, err := chain.Reference(frame, 2, 1, []core.Uniforms{{"Amount": 1, "Row": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if px := got.RGBAAt(0, 0); px != (color.RGBA{0, 0, 0, 0}) {
		t.Errorf("transparent target = %v, want transparent", px)
	}
	if px := got.RGBAAt(1, 0); px.R != 0 || px.G != 0 || px.B > px.A || px.A < 126 || px.A > 129 {
		t.Errorf("half-alpha blue target = %v, want premultiplied ~half-alpha blue", px)
	}
}

func TestPaletteRampMapsLuminanceAndMatchesReferencePicture(t *testing.T) {
	chain := postfx.MustCompile(postfx.PaletteRamp(gbRampTexture()))
	got, err := chain.Reference(sceneFrame(), 32, 18, []core.Uniforms{{"Amount": 1, "Row": 0}})
	if err != nil {
		t.Fatal(err)
	}
	gametest.AssertGolden(t, "palette-ramp", straight(got))

	// Exact four grayscale stops map to the four Game Boy columns.
	ramp := image.NewRGBA(image.Rect(0, 0, 5, 1))
	ramp.SetRGBA(0, 0, color.RGBA{0, 0, 0, 255})       // black -> col 0
	ramp.SetRGBA(1, 0, color.RGBA{85, 85, 85, 255})    // 1/3 -> col 1
	ramp.SetRGBA(2, 0, color.RGBA{170, 170, 170, 255}) // 2/3 -> col 2
	ramp.SetRGBA(3, 0, color.RGBA{255, 255, 255, 255}) // white -> col 3
	ramp.SetRGBA(4, 0, color.RGBA{128, 128, 128, 128}) // translucent straight white -> col 3 at half alpha

	out, err := chain.Reference(ramp, 5, 1, []core.Uniforms{{"Amount": 1, "Row": 0}})
	if err != nil {
		t.Fatal(err)
	}
	for x, want := range []color.RGBA{
		{0x0f, 0x38, 0x0f, 255},
		{0x30, 0x62, 0x30, 255},
		{0x8b, 0xac, 0x0f, 255},
		{0x9b, 0xbc, 0x0f, 255},
		{uint8(math.Round(0x9b * 128.0 / 255.0)), uint8(math.Round(0xbc * 128.0 / 255.0)), uint8(math.Round(0x0f * 128.0 / 255.0)), 128},
	} {
		if px := out.RGBAAt(x, 0); px != want {
			t.Errorf("stop %d = %v, want %v", x, px, want)
		}
	}

	// Row 1 selects the sepia ramp.
	sepia, err := chain.Reference(ramp, 5, 1, []core.Uniforms{{"Amount": 1, "Row": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if px := sepia.RGBAAt(3, 0); px != (color.RGBA{0xf2, 0xd9, 0xb1, 255}) {
		t.Errorf("sepia white = %v, want cream", px)
	}
}

func TestPaletteRamp256MapsEveryGrayscaleStepExactly(t *testing.T) {
	row := make([]core.Color, 256)
	for i := range 256 {
		row[i] = core.RGB(uint32(255-i)<<16 | uint32(i)<<8 | uint32(i/2))
	}
	pal := postfx.PaletteImage(row)
	chain := postfx.MustCompile(postfx.PaletteRamp(core.ImageSource{Pixels: pal, Version: 1}))

	frame := image.NewRGBA(image.Rect(0, 0, 256, 1))
	for i := range 256 {
		v := uint8(i)
		frame.SetRGBA(i, 0, color.RGBA{v, v, v, 255})
	}
	got, err := chain.Reference(frame, 256, 1, []core.Uniforms{{"Amount": 1}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 256 {
		want := color.RGBA{uint8(255 - i), uint8(i), uint8(i / 2), 255}
		if px := got.RGBAAt(i, 0); px != want {
			t.Fatalf("grayscale %d = %v, want %v", i, px, want)
		}
	}
}

func TestPaletteEffectsPassThroughAtZeroAmountInAChain(t *testing.T) {
	frame := paletteSpriteFrame()
	without, err := postfx.MustCompile(scale, scale).Reference(frame, 24, 16, []core.Uniforms{{"Gain": 3}, {"Gain": 1.0 / 3}})
	if err != nil {
		t.Fatal(err)
	}
	for name, eff := range map[string]postfx.Effect{
		"swap": postfx.PaletteSwap(swapPaletteTexture()),
		"ramp": postfx.PaletteRamp(gbRampTexture()),
	} {
		chain := postfx.MustCompile(scale, eff, scale)
		for _, amt := range []float64{0, -1, math.NaN()} {
			got, err := chain.Reference(frame, 24, 16, []core.Uniforms{{"Gain": 3}, {"Amount": amt, "Row": 1}, {"Gain": 1.0 / 3}})
			if err != nil {
				t.Fatalf("%s at %v: %v", name, amt, err)
			}
			if !slices.Equal(got.Pix, without.Pix) {
				t.Errorf("%s at amount %v changed the chain's picture", name, amt)
			}
		}
	}
}

func TestPaletteCompilesChainedAssignsImageSlotsAndEnforcesLimit(t *testing.T) {
	swap := postfx.PaletteSwap(swapPaletteTexture())
	ramp := postfx.PaletteRamp(gbRampTexture())
	chain, err := postfx.Compile(postfx.Bloom(), swap, ramp)
	if err != nil {
		t.Fatal(err)
	}
	if chain.HasTexture(0) || !chain.HasTexture(1) || !chain.HasTexture(2) || chain.HasTexture(-1) || chain.HasTexture(3) {
		t.Fatal("HasTexture reported wrong slots")
	}
	for _, want := range []string{"image1At", "image1Size", "image2At", "image2Size", "P1_Amount", "P1_Row", "P1_Tolerance", "P2_Amount", "P2_Row"} {
		if !strings.Contains(chain.Source(), want) {
			t.Errorf("fused source lacks %q:\n%s", want, chain.Source())
		}
	}
	for _, name := range []string{"P1_Amount", "P1_Row", "P1_Tolerance", "P2_Amount", "P2_Row"} {
		if !chain.Program().HasUniform(name) {
			t.Errorf("program lacks uniform %s", name)
		}
	}

	// Up to 3 textured effects compile; a 4th is refused.
	if _, err := postfx.Compile(swap, ramp, swap); err != nil {
		t.Fatalf("3 textured effects failed: %v", err)
	}
	if _, err := postfx.Compile(swap, ramp, swap, ramp); err == nil || !strings.Contains(err.Error(), "at most 3 effect textures") {
		t.Fatalf("4 textured effects err = %v, want max texture error", err)
	}

	// Declaring Texture or TextureSize is refused.
	for _, decl := range []string{
		"package main\n\nfunc Texture(pos vec2) vec4 {\n\treturn vec4(0)\n}\n\nfunc Effect(pos vec2) vec4 {\n\treturn Input(pos)\n}\n",
		"package main\n\nfunc TextureSize() vec2 {\n\treturn vec2(0)\n}\n\nfunc Effect(pos vec2) vec4 {\n\treturn Input(pos)\n}\n",
	} {
		if _, err := postfx.Compile(postfx.Effect{Name: "bad", Source: decl}); err == nil || !strings.Contains(err.Error(), "is the chain's") {
			t.Fatalf("err = %v, want chain reserved error", err)
		}
	}
}

func TestPaletteDecodesPNGDataAndOverridesTexturesOnViewAndReference(t *testing.T) {
	// Encode the swap palette as PNG bytes to verify core.ImageSource.Data support.
	var buf bytes.Buffer
	if err := png.Encode(&buf, swapPaletteTexture().Pixels); err != nil {
		t.Fatal(err)
	}
	pngSrc := core.ImageSource{Data: buf.Bytes()}
	frame := paletteSpriteFrame()

	fromPixels, err := postfx.MustCompile(postfx.PaletteSwap(swapPaletteTexture())).Reference(frame, 24, 16, []core.Uniforms{{"Amount": 1, "Row": 1}})
	if err != nil {
		t.Fatal(err)
	}
	fromPNG, err := postfx.MustCompile(postfx.PaletteSwap(pngSrc)).Reference(frame, 24, 16, []core.Uniforms{{"Amount": 1, "Row": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(fromPixels.Pix, fromPNG.Pix) {
		t.Fatal("PNG palette texture drew differently from Pixels palette texture")
	}

	// ReferenceWithTextures overrides an effect's texture without recompiling.
	unbound := postfx.MustCompile(postfx.PaletteSwap(core.ImageSource{}))
	unchanged, err := unbound.Reference(frame, 24, 16, []core.Uniforms{{"Amount": 1, "Row": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(unchanged.Pix, frame.Pix) {
		t.Fatal("empty texture changed the frame")
	}
	overridden, err := unbound.ReferenceWithTextures(frame, 24, 16, []core.Uniforms{{"Amount": 1, "Row": 1}}, []core.ImageSource{swapPaletteTexture()})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(overridden.Pix, fromPixels.Pix) {
		t.Fatal("ReferenceWithTextures did not apply the override palette")
	}

	// A URI-only texture returns a clear error in Reference.
	uriChain := postfx.MustCompile(postfx.PaletteSwap(core.ImageSource{URI: "asset:palette.png"}))
	if _, err := uriChain.Reference(frame, 24, 16, []core.Uniforms{{"Amount": 1, "Row": 1}}); err == nil || !strings.Contains(err.Error(), "has no Pixels or Data") {
		t.Fatalf("URI texture err = %v, want missing pixels error", err)
	}

	// ReferenceWithTextures rejects extra entries and non-textured targets.
	bloomChain := postfx.MustCompile(postfx.Bloom())
	if _, err := bloomChain.ReferenceWithTextures(frame, 24, 16, nil, []core.ImageSource{pngSrc}); err == nil || !strings.Contains(err.Error(), "reads no texture") {
		t.Fatalf("non-textured override err = %v", err)
	}
	if _, err := bloomChain.ReferenceWithTextures(frame, 24, 16, nil, []core.ImageSource{{}, {}}); err == nil || !strings.Contains(err.Error(), "textures for 2 effects") {
		t.Fatalf("extra textures err = %v", err)
	}
}

func TestViewBindsPaletteTextureAndReactsToTexturesFunc(t *testing.T) {
	palA := swapPaletteTexture()
	palB := gbRampTexture()
	chain := postfx.MustCompile(postfx.Bloom(), postfx.PaletteSwap(palA))
	frame := paletteSpriteFrame()

	// Static View binds [frame, palA].
	sh := (postfx.View{
		Chain:    chain,
		Frame:    core.ImageSource{Pixels: frame, Version: 1},
		Uniforms: []core.Uniforms{nil, {"Amount": 1, "Row": 1}},
	}).Render(nil).(core.Shader)
	if len(sh.Images) != 2 || sh.Images[0].Pixels != frame || sh.Images[1].Pixels != palA.Pixels {
		t.Fatalf("Images = %v, want [frame, palA]", sh.Images)
	}

	// View.Textures overrides effect 1's palette.
	shOver := (postfx.View{
		Chain:    chain,
		Frame:    core.ImageSource{Pixels: frame, Version: 1},
		Textures: []core.ImageSource{{}, palB},
	}).Render(nil).(core.Shader)
	if len(shOver.Images) != 2 || shOver.Images[1].Pixels != palB.Pixels {
		t.Fatalf("overridden Images = %v, want palB in slot 1", shOver.Images)
	}

	// View.TexturesFunc follows a signal and mounts cleanly under nsgotest with animated uniforms.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		useB := core.NewSignal(cx, false)
		clock := postfx.Clock(cx, time.Second)
		return core.View{Children: []core.Node{
			postfx.View{
				Chain: chain,
				Frame: core.ImageSource{Pixels: frame, Version: 1},
				TexturesFunc: func() []core.ImageSource {
					if useB.Get() {
						return []core.ImageSource{{}, palB}
					}
					return []core.ImageSource{{}, palA}
				},
				Uniforms: []core.Uniforms{nil, {"Amount": 1, "Row": clock}},
				Style:    core.Style{Width: core.Pt(120), Height: core.Pt(80)},
				TestID:   "palview",
			},
			core.View{TestID: "toggle", Style: core.Style{Width: core.Pt(20), Height: core.Pt(20)}, OnPress: func(core.PressEvent) {
				useB.Set(true)
			}},
		}}
	})
	if app.FindMounted("id:palview").Shader == nil {
		t.Fatal("palette view mounted no shader")
	}
	app.Press("id:toggle")
	app.Advance(250 * time.Millisecond)
	if app.FindMounted("id:palview").Shader == nil {
		t.Fatal("palette view lost shader after toggle")
	}
}

func TestChainImagesPanicsOnInvalidTextureTargets(t *testing.T) {
	chain := postfx.MustCompile(postfx.Bloom())
	frame := core.ImageSource{Pixels: paletteSpriteFrame()}
	for name, per := range map[string][]core.ImageSource{
		"extra":       {{}, {}},
		"non-texture": {swapPaletteTexture()},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("did not panic")
				}
			}()
			chain.Images(frame, per)
		})
	}
}

func TestPaletteImageHandlesEmptyAndRaggedRows(t *testing.T) {
	if empty := postfx.PaletteImage(); empty.Rect.Dx() != 0 || empty.Rect.Dy() != 0 {
		t.Fatalf("empty PaletteImage = %v, want 0x0", empty.Rect)
	}
	img := postfx.PaletteImage(
		[]core.Color{core.RGB(0xff0000)},
		[]core.Color{core.RGB(0x00ff00), core.RGB(0x0000ff)},
	)
	if img.Rect != image.Rect(0, 0, 2, 2) {
		t.Fatalf("rect = %v, want 2x2", img.Rect)
	}
	if px := img.RGBAAt(1, 0); px != (color.RGBA{0, 0, 0, 0}) {
		t.Fatalf("padded cell = %v, want transparent", px)
	}
	if px := img.RGBAAt(1, 1); px != (color.RGBA{0, 0, 255, 255}) {
		t.Fatalf("(1, 1) = %v, want blue", px)
	}
}

// FuzzPaletteReference checks that any uniforms, NaN and infinities included, keep colors premultiplied, opaque frames opaque (with opaque palettes), and transparent pixels transparent.
func FuzzPaletteReference(f *testing.F) {
	f.Add(1.0, 1.0, 0.0)
	f.Add(0.5, 1.5, 0.05)
	f.Add(0.0, 0.0, 1.0)
	f.Add(-1.0, -2.0, -0.5)
	f.Add(1e9, 1e9, 1e9)
	f.Add(math.NaN(), math.NaN(), math.NaN())
	f.Add(math.Inf(1), math.Inf(-1), math.Inf(1))
	swapChain := postfx.MustCompile(postfx.PaletteSwap(swapPaletteTexture()))
	rampChain := postfx.MustCompile(postfx.PaletteRamp(gbRampTexture()))
	frame := paletteSpriteFrame()
	f.Fuzz(func(t *testing.T, amount, row, tol float64) {
		for name, got := range map[string]*image.RGBA{
			"swap": func() *image.RGBA {
				img, err := swapChain.Reference(frame, 12, 8, []core.Uniforms{{"Amount": amount, "Row": row, "Tolerance": tol}})
				if err != nil {
					t.Fatal(err)
				}
				return img
			}(),
			"ramp": func() *image.RGBA {
				img, err := rampChain.Reference(frame, 12, 8, []core.Uniforms{{"Amount": amount, "Row": row}})
				if err != nil {
					t.Fatal(err)
				}
				return img
			}(),
		} {
			for i := 0; i < len(got.Pix); i += 4 {
				p := got.Pix[i : i+4]
				if p[0] > p[3] || p[1] > p[3] || p[2] > p[3] {
					t.Fatalf("%s pixel %d is not premultiplied: %v", name, i/4, p)
				}
			}
			// Top-left corner of paletteSpriteFrame is transparent and must remain transparent.
			if corner := got.RGBAAt(0, 0); corner != (color.RGBA{0, 0, 0, 0}) {
				t.Fatalf("%s transparent corner became %v", name, corner)
			}
		}
	})
}

func BenchmarkPaletteSwapReference(b *testing.B) {
	chain := postfx.MustCompile(postfx.PaletteSwap(swapPaletteTexture()))
	frame := image.NewRGBA(image.Rect(0, 0, 160, 90))
	u := []core.Uniforms{{"Amount": 1, "Row": 1}}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := chain.Reference(frame, 160, 90, u); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPaletteRampReference(b *testing.B) {
	chain := postfx.MustCompile(postfx.PaletteRamp(gbRampTexture()))
	frame := image.NewRGBA(image.Rect(0, 0, 160, 90))
	u := []core.Uniforms{{"Amount": 1, "Row": 0}}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := chain.Reference(frame, 160, 90, u); err != nil {
			b.Fatal(err)
		}
	}
}
