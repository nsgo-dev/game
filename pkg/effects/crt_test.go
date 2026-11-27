package effects_test

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/effects"
	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// crtUniforms are the reference picture's: a classic CRT curve with crisp scanlines.
var crtUniforms = []core.Uniforms{{"Curvature": 0.25, "Scanlines": 0.35}}

func TestCRTCurvesTheFrameAndDrawsScanlinesAsTheReferencePictureDoes(t *testing.T) {
	chain := effects.MustCompile(effects.CRT())
	got, err := chain.Reference(sceneFrame(), 64, 36, crtUniforms)
	if err != nil {
		t.Fatal(err)
	}
	gametest.AssertGolden(t, "crt", straight(got))

	black := color.RGBA{0, 0, 0, 255}
	// The four corners pull inward into the opaque black bezel.
	for _, pt := range [][2]int{{0, 0}, {63, 0}, {0, 35}, {63, 35}} {
		if got := got.RGBAAt(pt[0], pt[1]); got != black {
			t.Errorf("corner (%d, %d) = %v, want opaque black bezel", pt[0], pt[1], got)
		}
	}
	// The four edge midpoints stay pinned to the frame border and show the scene, not the bezel.
	for _, pt := range [][2]int{{32, 0}, {32, 35}, {0, 18}, {63, 18}} {
		if got := got.RGBAAt(pt[0], pt[1]); got == black {
			t.Errorf("edge midpoint (%d, %d) drew black bezel, want the frame", pt[0], pt[1])
		}
	}
	// Along the vertical center (zero horizontal curvature), even rows are bright scanline peaks and odd rows are darkened troughs.
	bright := luma(got.RGBAAt(32, 2))
	dark := luma(got.RGBAAt(32, 3))
	if bright <= dark+20 {
		t.Errorf("center scanline peak %.1f vs trough %.1f: want alternating bright and dark rows", bright, dark)
	}
}

func TestCRTWithZeroUniformsDrawsTheFrameAsItIs(t *testing.T) {
	chain := effects.MustCompile(effects.CRT())
	frame := sceneFrame()
	for name, u := range map[string][]core.Uniforms{
		"no uniforms":   nil,
		"zeros":         {{"Curvature": 0, "Scanlines": 0, "Lines": 0}},
		"negatives":     {{"Curvature": -0.5, "Scanlines": -1, "Lines": -10}},
		"NaN":           {{"Curvature": math.NaN(), "Scanlines": math.NaN(), "Lines": math.NaN()}},
		"lines only":    {{"Lines": 100}},
		"NaN curvature": {{"Curvature": math.NaN(), "Scanlines": 0}},
	} {
		got, err := chain.Reference(frame, 32, 18, u)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got.Pix, frame.Pix) {
			t.Errorf("%s changed the frame", name)
		}
	}
}

func TestCRTWithZeroUniformsPassesItsInputThroughInAChain(t *testing.T) {
	frame := sceneFrame()
	without, err := effects.MustCompile(scale, scale).Reference(frame, 32, 18, []core.Uniforms{{"Gain": 3}, {"Gain": 1.0 / 3}})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []core.Uniforms{
		nil,
		{"Curvature": 0, "Scanlines": 0},
		{"Curvature": -1, "Scanlines": -1},
		{"Curvature": math.NaN(), "Scanlines": math.NaN()},
	} {
		got, err := effects.MustCompile(scale, effects.CRT(), scale).Reference(frame, 32, 18, []core.Uniforms{{"Gain": 3}, u, {"Gain": 1.0 / 3}})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got.Pix, without.Pix) {
			t.Errorf("CRT at %v changed the chain's picture", u)
		}
	}
}

func TestCRTScanlinesWorkWithoutCurvatureAndWithCustomLines(t *testing.T) {
	chain := effects.MustCompile(effects.CRT())
	white := image.NewRGBA(image.Rect(0, 0, 16, 12))
	draw.Draw(white, white.Rect, image.NewUniform(color.RGBA{255, 255, 255, 255}), image.Point{}, draw.Src)

	// At 2x scale with Lines left 0 (one scanline per frame row), even output rows are peaks and odd rows are darkened by Scanlines.
	got, err := chain.Reference(white, 32, 24, []core.Uniforms{{"Scanlines": 0.5}})
	if err != nil {
		t.Fatal(err)
	}
	for y := range 24 {
		want := color.RGBA{255, 255, 255, 255}
		if y%2 == 1 {
			want = color.RGBA{128, 128, 128, 255}
		}
		if px := got.RGBAAt(8, y); px != want {
			t.Fatalf("2x row %d = %v, want %v", y, px, want)
		}
	}

	// At 1x scale, setting Lines to half the height draws alternating bright and dark rows.
	got1x, err := chain.Reference(white, 16, 12, []core.Uniforms{{"Scanlines": 0.5, "Lines": 6}})
	if err != nil {
		t.Fatal(err)
	}
	for y := range 12 {
		want := color.RGBA{255, 255, 255, 255}
		if y%2 == 1 {
			want = color.RGBA{128, 128, 128, 255}
		}
		if px := got1x.RGBAAt(8, y); px != want {
			t.Fatalf("1x row %d = %v, want %v", y, px, want)
		}
	}

	// Scanlines darken RGB toward black while preserving each pixel's alpha.
	translucent := image.NewRGBA(image.Rect(0, 0, 8, 8))
	draw.Draw(translucent, translucent.Rect, image.NewUniform(color.RGBA{120, 60, 30, 128}), image.Point{}, draw.Src)
	gotAlpha, err := chain.Reference(translucent, 16, 16, []core.Uniforms{{"Scanlines": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if peak := gotAlpha.RGBAAt(4, 0); peak != (color.RGBA{120, 60, 30, 128}) {
		t.Errorf("translucent peak = %v, want original pixel", peak)
	}
	if trough := gotAlpha.RGBAAt(4, 1); trough != (color.RGBA{0, 0, 0, 128}) {
		t.Errorf("translucent trough = %v, want black at alpha 128", trough)
	}
}

func TestCRTCurvatureWorksWithoutScanlines(t *testing.T) {
	chain := effects.MustCompile(effects.CRT())
	// A symmetric 16 by 16 frame of vertical stripes.
	sym := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			d := x
			if d >= 8 {
				d = 15 - d
			}
			sym.SetRGBA(x, y, color.RGBA{uint8(40 + d*24), uint8(80 + y*10), 200, 255})
		}
	}
	gentle, err := chain.Reference(sym, 32, 32, []core.Uniforms{{"Curvature": 0.2}})
	if err != nil {
		t.Fatal(err)
	}
	strong, err := chain.Reference(sym, 32, 32, []core.Uniforms{{"Curvature": 0.6}})
	if err != nil {
		t.Fatal(err)
	}
	// Horizontal symmetry holds for a symmetric frame.
	for y := range 32 {
		for x := range 16 {
			if a, b := gentle.RGBAAt(x, y), gentle.RGBAAt(31-x, y); a != b {
				t.Fatalf("row %d col %d = %v != mirrored %v", y, x, a, b)
			}
		}
	}
	// Greater curvature puts more pixels into the black bezel.
	black := color.RGBA{0, 0, 0, 255}
	countBezel := func(img *image.RGBA) int {
		n := 0
		for y := range img.Rect.Dy() {
			for x := range img.Rect.Dx() {
				if img.RGBAAt(x, y) == black {
					n++
				}
			}
		}
		return n
	}
	gBezel, sBezel := countBezel(gentle), countBezel(strong)
	if gBezel == 0 || sBezel <= gBezel {
		t.Fatalf("bezel pixels gentle=%d, strong=%d: want 0 < gentle < strong", gBezel, sBezel)
	}
}

func TestCRTChainsAfterOtherEffectsWithOneTap(t *testing.T) {
	chain := effects.MustCompile(effects.ShakeEffect(), effects.Bloom(), effects.VignetteEffect(), effects.CRT(), effects.FlashEffect())
	for _, name := range []string{"P3_Curvature", "P3_Scanlines", "P3_Lines"} {
		if !chain.Program().HasUniform(name) {
			t.Errorf("the program has no uniform %s", name)
		}
	}
	// CRT calls its upstream effect once in the fused source so it does not multiply Bloom's taps.
	if !strings.Contains(chain.Source(), "c := p2_Effect(samplePos)") || strings.Count(chain.Source(), "p2_Effect(") != 2 {
		t.Errorf("CRT does not call upstream effect once:\n%s", chain.Source())
	}

	// Chained after invert, the screen shows inverted colors while the curved bezel stays opaque black.
	invCRT := effects.MustCompile(invert, effects.CRT())
	got, err := invCRT.Reference(sceneFrame(), 64, 36, []core.Uniforms{{"Amount": 1}, crtUniforms[0]})
	if err != nil {
		t.Fatal(err)
	}
	if px := got.RGBAAt(0, 0); px != (color.RGBA{0, 0, 0, 255}) {
		t.Errorf("corner bezel after invert = %v, want opaque black", px)
	}
	if p := effects.CRT(); p.Name != "crt" || p.Reference == nil || !strings.Contains(p.Source, "var Curvature float") {
		t.Errorf("CRT() = %q, source %d bytes", p.Name, len(p.Source))
	}
}

func TestCRTViewMountsWithAnimatedUniform(t *testing.T) {
	chain := effects.MustCompile(effects.CRT())
	var clock *core.AnimatedValue
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		clock = effects.Clock(cx, time.Second)
		return effects.View{
			Chain:    chain,
			Frame:    core.ImageSource{Pixels: sceneFrame(), Version: 1},
			Uniforms: []core.Uniforms{{"Curvature": 0.25, "Scanlines": clock, "Lines": 18}},
			Style:    core.Style{Width: core.Pt(320), Height: core.Pt(180)},
			TestID:   "crt",
		}
	})
	sh := app.FindMounted("id:crt").Shader
	if sh == nil {
		t.Fatal("the CRT view mounted no shader")
	}
	if sh.Role != nsgotest.ShaderFill || sh.WGSL {
		t.Fatalf("role %v, WGSL %v: want a Kage fill", sh.Role, sh.WGSL)
	}
	if !slices.ContainsFunc(sh.Nodes, func(n nsgotest.ShaderNode) bool { return n.Node == clock.AnimatedNodeID() }) {
		t.Fatalf("no lane follows the clock: %v", sh.Nodes)
	}
	if !slices.Contains(sh.Lanes, 0.25) || !slices.Contains(sh.Lanes, 18) {
		t.Fatalf("lanes %v: want Curvature 0.25 and Lines 18", sh.Lanes)
	}
}

// FuzzCRTReference checks that any uniforms, NaN and infinities included, keep an opaque frame opaque and a translucent frame premultiplied.
func FuzzCRTReference(f *testing.F) {
	f.Add(0.25, 0.35, 0.0)
	f.Add(0.0, 0.0, 0.0)
	f.Add(4.0, 1.0, 16384.0)
	f.Add(-1.0, -2.0, -3.0)
	f.Add(1e9, 1e9, 1e9)
	f.Add(math.NaN(), 0.5, 0.0)
	f.Add(0.25, math.NaN(), 0.0)
	f.Add(0.25, 0.5, math.NaN())
	f.Add(math.Inf(1), math.Inf(1), math.Inf(1))
	f.Add(math.Inf(-1), math.Inf(-1), math.Inf(-1))
	chain := effects.MustCompile(effects.CRT())
	opaque := bloomFrame()
	mixed := sceneFrame()
	f.Fuzz(func(t *testing.T, curvature, scanlines, lines float64) {
		u := []core.Uniforms{{"Curvature": curvature, "Scanlines": scanlines, "Lines": lines}}
		got, err := chain.Reference(opaque, 16, 10, u)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < len(got.Pix); i += 4 {
			if p := got.Pix[i : i+4]; p[3] != 255 {
				t.Fatalf("opaque pixel %d = %v: an opaque frame drew a translucent pixel", i/4, p)
			}
		}
		gotMixed, err := chain.Reference(mixed, 16, 9, u)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < len(gotMixed.Pix); i += 4 {
			p := gotMixed.Pix[i : i+4]
			if p[0] > p[3] || p[1] > p[3] || p[2] > p[3] {
				t.Fatalf("mixed pixel %d = %v: not premultiplied", i/4, p)
			}
		}
	})
}

func BenchmarkCRTReference(b *testing.B) {
	chain := effects.MustCompile(effects.CRT())
	frame := image.NewRGBA(image.Rect(0, 0, 320, 180))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := chain.Reference(frame, 320, 180, crtUniforms); err != nil {
			b.Fatal(err)
		}
	}
}
