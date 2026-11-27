package postfx_test

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/game/pkg/postfx"
	"github.com/nsgo-dev/nsgo/core"
)

// bloomFrame is a 32 by 20 night scene, premultiplied: a dark blue sky, a white 4 by 4 lamp, a yellow window stripe, and a mid-gray wall too dim to glow, each far enough from the others that their glows do not meet at the default radius.
func bloomFrame() *image.RGBA {
	f := image.NewRGBA(image.Rect(0, 0, 32, 20))
	draw.Draw(f, f.Rect, image.NewUniform(color.RGBA{20, 24, 40, 255}), image.Point{}, draw.Src)
	fill := func(r image.Rectangle, c color.RGBA) {
		draw.Draw(f, r, image.NewUniform(c), image.Point{}, draw.Src)
	}
	fill(image.Rect(6, 4, 10, 8), color.RGBA{255, 255, 255, 255})
	fill(image.Rect(20, 3, 28, 5), color.RGBA{255, 220, 60, 255})
	fill(image.Rect(20, 13, 28, 18), color.RGBA{128, 128, 128, 255})
	return f
}

// bloomUniforms are the reference picture's: a strong bloom of what is brighter than 60%, reaching 4 pixels.
var bloomUniforms = []core.Uniforms{{"Threshold": 0.6, "Intensity": 1.5, "Radius": 4}}

// luma is a pixel's Rec. 709 luminance, from 0 to 255.
func luma(c color.RGBA) float64 {
	return 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
}

func TestBloomBrightensHighlightsAsTheReferencePictureDoes(t *testing.T) {
	chain := postfx.MustCompile(postfx.Bloom())
	frame := bloomFrame()
	got, err := chain.Reference(frame, 32, 20, bloomUniforms)
	if err != nil {
		t.Fatal(err)
	}
	gametest.AssertGolden(t, "bloom", got)

	for _, tc := range []struct {
		name string
		x, y int
	}{
		{"beside the lamp", 11, 5},
		{"above the lamp", 7, 2},
		{"below the window", 23, 6},
		{"left of the window", 18, 3},
	} {
		if a, b := luma(frame.RGBAAt(tc.x, tc.y)), luma(got.RGBAAt(tc.x, tc.y)); b <= a+8 {
			t.Errorf("%s (%d, %d): luminance %.1f, want brighter than the frame's %.1f", tc.name, tc.x, tc.y, b, a)
		}
	}
	// The glow fades with distance: a pixel 1 off the lamp glows more than one 3 off.
	if near, far := luma(got.RGBAAt(10, 5)), luma(got.RGBAAt(12, 5)); near <= far {
		t.Errorf("1 pixel off the lamp %.1f, 3 off %.1f: want the nearer brighter", near, far)
	}
	for _, tc := range []struct {
		name string
		x, y int
	}{
		{"the sky far from any highlight", 1, 18},
		{"the sky between the lamp and the wall", 14, 15},
		{"the wall, below the threshold", 24, 15},
	} {
		if a, b := frame.RGBAAt(tc.x, tc.y), got.RGBAAt(tc.x, tc.y); a != b {
			t.Errorf("%s (%d, %d) = %v, want the frame's %v", tc.name, tc.x, tc.y, b, a)
		}
	}
	// The lamp, already white, stays white.
	if px := got.RGBAAt(8, 6); px != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("the lamp = %v, want white", px)
	}
}

func TestBloomWithNoIntensityDrawsTheFrameAsItIs(t *testing.T) {
	chain := postfx.MustCompile(postfx.Bloom())
	frame := bloomFrame()
	for name, u := range map[string][]core.Uniforms{
		"no uniforms":                 nil,
		"no intensity":                {{"Threshold": 0, "Intensity": 0, "Radius": 6}},
		"no threshold":                {{"Threshold": 1, "Intensity": 2, "Radius": 6}},
		"NaN intensity":               {{"Threshold": 0, "Intensity": math.NaN(), "Radius": 6}},
		"NaN threshold, no intensity": {{"Threshold": math.NaN(), "Intensity": 0, "Radius": 6}},
	} {
		got, err := chain.Reference(frame, 32, 20, u)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got.Pix, frame.Pix) {
			t.Errorf("%s changed the frame", name)
		}
	}
}

// scale multiplies every channel by Gain, which can push a color above 1 or above its alpha between effects.
var scale = postfx.Effect{Name: "scale", Source: `package main

// Gain multiplies every channel.
var Gain float

func Effect(pos vec2) vec4 {
	return Input(pos) * Gain
}
`, Reference: func(in postfx.Input, u core.Uniforms, x, y float64) [4]float64 {
	c, gain := in.At(x, y), postfx.Float(u, "Gain")
	for k := range c {
		c[k] *= gain
	}
	return c
}}

func TestBloomWithNoIntensityPassesItsInputThroughInAChain(t *testing.T) {
	// Between a gain of 3 and one of 1/3, out-of-range values must reach the second scale untouched, so the frame comes back as it was.
	frame := bloomFrame()
	draw.Draw(frame, image.Rect(0, 0, 4, 4), image.NewUniform(color.RGBA{60, 30, 10, 128}), image.Point{}, draw.Src)
	without, err := postfx.MustCompile(scale, scale).Reference(frame, 32, 20, []core.Uniforms{{"Gain": 3}, {"Gain": 1.0 / 3}})
	if err != nil {
		t.Fatal(err)
	}
	for _, intensity := range []float64{0, -1, math.NaN()} {
		got, err := postfx.MustCompile(scale, postfx.Bloom(), scale).Reference(frame, 32, 20, []core.Uniforms{{"Gain": 3}, {"Threshold": 0.2, "Intensity": intensity, "Radius": 4}, {"Gain": 1.0 / 3}})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got.Pix, without.Pix) {
			t.Errorf("Bloom at intensity %v changed the chain's picture", intensity)
		}
	}
	if !slices.Equal(without.Pix, frame.Pix) {
		t.Error("scaling by 3 and by 1/3 changed the frame")
	}
}

func TestBloomsRadiusIsHowFarTheGlowReaches(t *testing.T) {
	chain := postfx.MustCompile(postfx.Bloom())
	frame := bloomFrame()
	at := func(radius float64) color.RGBA {
		t.Helper()
		got, err := chain.Reference(frame, 32, 20, []core.Uniforms{{"Threshold": 0.6, "Intensity": 1.5, "Radius": radius}})
		if err != nil {
			t.Fatal(err)
		}
		// The pixel whose center is 3.5 pixels right of the lamp's right edge.
		return got.RGBAAt(13, 5)
	}
	if px := at(2); px != frame.RGBAAt(13, 5) {
		t.Errorf("a radius of 2 reached 3.5 pixels: %v", px)
	}
	if px := at(8); luma(px) <= luma(frame.RGBAAt(13, 5)) {
		t.Errorf("a radius of 8 did not reach 3.5 pixels: %v", px)
	}
}

func TestBloomKeepsColorsPremultiplied(t *testing.T) {
	chain := postfx.MustCompile(postfx.Bloom())
	frame := image.NewRGBA(image.Rect(0, 0, 12, 12))
	// A translucent white spot over transparency glows into it.
	draw.Draw(frame, image.Rect(5, 5, 7, 7), image.NewUniform(color.RGBA{200, 200, 200, 200}), image.Point{}, draw.Src)
	got, err := chain.Reference(frame, 12, 12, []core.Uniforms{{"Threshold": 0.2, "Intensity": 4, "Radius": 3}})
	if err != nil {
		t.Fatal(err)
	}
	glowed := false
	for i := 0; i < len(got.Pix); i += 4 {
		p := got.Pix[i : i+4]
		if p[0] > p[3] || p[1] > p[3] || p[2] > p[3] {
			t.Fatalf("pixel %d = %v: a color above its alpha", i/4, p)
		}
		if frame.Pix[i+3] == 0 && p[3] > 0 {
			glowed = true
		}
	}
	if !glowed {
		t.Fatal("no transparent pixel took the glow")
	}
}

func TestBloomChainsAfterAnotherEffect(t *testing.T) {
	chain := postfx.MustCompile(invert, postfx.Bloom())
	for _, name := range []string{"P1_Threshold", "P1_Intensity", "P1_Radius"} {
		if !chain.Program().HasUniform(name) {
			t.Errorf("the program has no uniform %s", name)
		}
	}
	if !strings.Contains(chain.Source(), "p1_bright(p0_Effect(") {
		t.Errorf("the bloom does not read the invert:\n%s", chain.Source())
	}
	// Inverted, the dark sky turns bright and glows over the lamp, which turned black.
	got, err := chain.Reference(bloomFrame(), 32, 20, []core.Uniforms{{"Amount": 1}, bloomUniforms[0]})
	if err != nil {
		t.Fatal(err)
	}
	if px := got.RGBAAt(8, 6); px.R < 32 {
		t.Errorf("the inverted lamp = %v, want lit by the sky's glow", px)
	}
	if px := got.RGBAAt(7, 5); px == (color.RGBA{0, 0, 0, 255}) {
		t.Errorf("the inverted lamp's inside drew black, want it lit")
	}
	if p := postfx.Bloom(); p.Name != "bloom" || p.Reference == nil || !strings.Contains(p.Source, "var Threshold float") {
		t.Errorf("Bloom() = %q, source %d bytes", p.Name, len(p.Source))
	}
}

// FuzzBloomReference checks that any uniforms, NaN and infinities included, draw an opaque frame opaque: the glow only adds light, and no NaN reaches a pixel's alpha.
func FuzzBloomReference(f *testing.F) {
	f.Add(0.6, 1.5, 4.0)
	f.Add(0.0, 0.0, 0.0)
	f.Add(1.0, 2.0, 100.0)
	f.Add(-1.0, -3.0, -4.0)
	f.Add(0.99999, 1e9, 1e-9)
	f.Add(math.NaN(), 1.5, 4.0)
	f.Add(math.Inf(-1), 1.5, 4.0)
	f.Add(math.Inf(1), 1.5, 4.0)
	f.Add(0.6, math.NaN(), 4.0)
	f.Add(0.6, math.Inf(1), 4.0)
	f.Add(0.0, math.Inf(1), 4.0)
	f.Add(0.6, 1.5, math.NaN())
	f.Add(0.6, 1.5, math.Inf(1))
	chain := postfx.MustCompile(postfx.Bloom())
	frame := bloomFrame()
	f.Fuzz(func(t *testing.T, threshold, intensity, radius float64) {
		got, err := chain.Reference(frame, 16, 10, []core.Uniforms{{"Threshold": threshold, "Intensity": intensity, "Radius": radius}})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < len(got.Pix); i += 4 {
			p := got.Pix[i : i+4]
			if p[3] != 255 {
				t.Fatalf("pixel %d = %v: an opaque frame drew a translucent pixel", i/4, p)
			}
		}
	})
}

func BenchmarkBloomReference(b *testing.B) {
	chain := postfx.MustCompile(postfx.Bloom())
	frame := image.NewRGBA(image.Rect(0, 0, 320, 180))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := chain.Reference(frame, 320, 180, bloomUniforms); err != nil {
			b.Fatal(err)
		}
	}
}
