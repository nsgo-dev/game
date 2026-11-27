package effects_test

import (
	"image"
	"image/color"
	"math"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/a11y"
	"github.com/nsgo-dev/game/pkg/effects"
	"github.com/nsgo-dev/nsgo/core"
)

func rgbaFromCore(c core.Color) color.RGBA {
	r, g, b, a := c.Components()
	return color.RGBA{
		R: uint8(math.Round(r * a * 255)),
		G: uint8(math.Round(g * a * 255)),
		B: uint8(math.Round(b * a * 255)),
		A: uint8(math.Round(a * 255)),
	}
}

func srgbDistRGBA(a, b color.RGBA) float64 {
	dr := float64(a.R) - float64(b.R)
	dg := float64(a.G) - float64(b.G)
	db := float64(a.B) - float64(b.B)
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

func TestColorVisionSimulationMatchesA11ySimulate(t *testing.T) {
	swatches := []core.Color{
		core.RGB(0x000000),
		core.RGB(0xFFFFFF),
		core.RGB(0x808080),
		core.RGB(0xCC2222),
		core.RGB(0x22AA22),
		core.RGB(0x2255CC),
		core.RGB(0xF0E442),
		core.RGB(0xE69F00),
		core.RGB(0xD982B5),
		core.RGB(0x56B4E9),
	}

	frame := image.NewRGBA(image.Rect(0, 0, len(swatches), 1))
	for i, c := range swatches {
		frame.SetRGBA(i, 0, rgbaFromCore(c))
	}

	cases := []struct {
		effMode  effects.ColorVisionMode
		a11yMode a11y.ColorVision
	}{
		{effects.ColorVisionNone, a11y.VisionNormal},
		{effects.ColorVisionProtanopia, a11y.VisionProtanopia},
		{effects.ColorVisionDeuteranopia, a11y.VisionDeuteranopia},
		{effects.ColorVisionTritanopia, a11y.VisionTritanopia},
		{effects.ColorVisionHighContrast, a11y.VisionHighContrast},
	}

	for _, tc := range cases {
		t.Run(tc.effMode.String(), func(t *testing.T) {
			chain := effects.MustCompile(effects.ColorVisionEffect(tc.effMode, 1.0, false))
			got, err := chain.Reference(frame, len(swatches), 1, nil)
			if err != nil {
				t.Fatalf("Reference failed: %v", err)
			}
			for i, c := range swatches {
				want := rgbaFromCore(a11y.Simulate(c, tc.a11yMode))
				px := got.RGBAAt(i, 0)
				if px != want {
					t.Errorf("swatch %d (%#08x): got %v, want %v (a11y.Simulate)", i, uint32(c), px, want)
				}
			}
		})
	}
}

func TestColorVisionCorrectionRaisesSimulatedSeparation(t *testing.T) {
	cases := []struct {
		name string
		mode effects.ColorVisionMode
		a, b core.Color
	}{
		{"protanopia red vs green", effects.ColorVisionProtanopia, core.RGB(0xCC2222), core.RGB(0x22AA22)},
		{"protanopia equal-luma red vs green", effects.ColorVisionProtanopia, core.RGB(0xCC3333), core.RGB(0x3E7B2E)},
		{"deuteranopia red vs green", effects.ColorVisionDeuteranopia, core.RGB(0xCC2222), core.RGB(0x22AA22)},
		{"deuteranopia equal-luma red vs green", effects.ColorVisionDeuteranopia, core.RGB(0xC84232), core.RGB(0x468834)},
		{"tritanopia blue vs green", effects.ColorVisionTritanopia, core.RGB(0x2255CC), core.RGB(0x22AA55)},
		{"tritanopia yellow vs pink", effects.ColorVisionTritanopia, core.RGB(0xEEEE22), core.RGB(0xEECCDD)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame := image.NewRGBA(image.Rect(0, 0, 2, 1))
			frame.SetRGBA(0, 0, rgbaFromCore(tc.a))
			frame.SetRGBA(1, 0, rgbaFromCore(tc.b))

			simOnly := effects.MustCompile(effects.ColorVisionEffect(tc.mode, 1.0, false))
			corrThenSim := effects.MustCompile(
				effects.ColorVisionEffect(tc.mode, 1.0, true),
				effects.ColorVisionEffect(tc.mode, 1.0, false),
			)

			uncorrSim, err := simOnly.Reference(frame, 2, 1, nil)
			if err != nil {
				t.Fatal(err)
			}
			corrSim, err := corrThenSim.Reference(frame, 2, 1, nil)
			if err != nil {
				t.Fatal(err)
			}

			distBefore := srgbDistRGBA(uncorrSim.RGBAAt(0, 0), uncorrSim.RGBAAt(1, 0))
			distAfter := srgbDistRGBA(corrSim.RGBAAt(0, 0), corrSim.RGBAAt(1, 0))
			if distAfter <= distBefore+5.0 {
				t.Errorf("%s: correction did not raise simulated separation: before=%.1f (%v vs %v), after=%.1f (%v vs %v)",
					tc.name, distBefore, uncorrSim.RGBAAt(0, 0), uncorrSim.RGBAAt(1, 0),
					distAfter, corrSim.RGBAAt(0, 0), corrSim.RGBAAt(1, 0))
			}
		})
	}
}

func TestColorVisionSeverityNeutralGrayAndAlpha(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 4, 1))
	frame.SetRGBA(0, 0, color.RGBA{128, 128, 128, 255}) // neutral gray
	frame.SetRGBA(1, 0, color.RGBA{0, 0, 0, 0})         // transparent
	frame.SetRGBA(2, 0, color.RGBA{102, 17, 17, 128})   // premultiplied translucent red (50% alpha)
	frame.SetRGBA(3, 0, color.RGBA{255, 255, 255, 255}) // pure white

	for _, mode := range []effects.ColorVisionMode{
		effects.ColorVisionProtanopia,
		effects.ColorVisionDeuteranopia,
		effects.ColorVisionTritanopia,
		effects.ColorVisionHighContrast,
	} {
		// Severity 0 or NaN is an exact identity pass-through.
		for _, sev := range []float64{0, -0.5, math.NaN()} {
			for _, correct := range []bool{false, true} {
				chain := effects.MustCompile(effects.ColorVisionEffect(mode, sev, correct))
				got, err := chain.Reference(frame, 4, 1, nil)
				if err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(got.Pix, frame.Pix) {
					t.Errorf("%s sev=%v correct=%v: expected identity pass-through", mode, sev, correct)
				}
			}
		}

		// Neutral grays, pure white, and transparent pixels stay invariant; alpha is preserved.
		for _, correct := range []bool{false, true} {
			chain := effects.MustCompile(effects.ColorVisionEffect(mode, 1.0, correct))
			got, err := chain.Reference(frame, 4, 1, nil)
			if err != nil {
				t.Fatal(err)
			}
			if d := srgbDistRGBA(got.RGBAAt(0, 0), frame.RGBAAt(0, 0)); d > 2.0 {
				t.Errorf("%s correct=%v: neutral gray drifted by %.2f (%v -> %v)", mode, correct, d, frame.RGBAAt(0, 0), got.RGBAAt(0, 0))
			}
			if px := got.RGBAAt(1, 0); px != (color.RGBA{0, 0, 0, 0}) {
				t.Errorf("%s correct=%v: transparent pixel = %v, want zero", mode, correct, px)
			}
			if px := got.RGBAAt(2, 0); px.A != 128 {
				t.Errorf("%s correct=%v: translucent pixel alpha = %d, want 128", mode, correct, px.A)
			}
			if px := got.RGBAAt(3, 0); px != (color.RGBA{255, 255, 255, 255}) {
				t.Errorf("%s correct=%v: pure white = %v, want white", mode, correct, px)
			}
		}
	}

	// Partial severity interpolates smoothly between original and full transform.
	redFrame := image.NewRGBA(image.Rect(0, 0, 1, 1))
	redFrame.SetRGBA(0, 0, rgbaFromCore(core.RGB(0xCC2222)))
	full, err := effects.MustCompile(effects.ColorVisionEffect(effects.ColorVisionProtanopia, 1.0, false)).Reference(redFrame, 1, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	half, err := effects.MustCompile(effects.ColorVisionEffect(effects.ColorVisionProtanopia, 0.5, false)).Reference(redFrame, 1, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	dHalf := srgbDistRGBA(redFrame.RGBAAt(0, 0), half.RGBAAt(0, 0))
	dFull := srgbDistRGBA(redFrame.RGBAAt(0, 0), full.RGBAAt(0, 0))
	if !(dHalf > 0 && dHalf < dFull) {
		t.Errorf("expected half severity distance %.1f strictly between 0 and full %.1f", dHalf, dFull)
	}
}

func TestColorVisionChainsWithOtherEffectsAndParsesModes(t *testing.T) {
	// Verify Kage shader compiles cleanly when fused in a chain with Bloom and VignetteEffect.
	chain, err := effects.Compile(
		effects.Bloom(),
		effects.ColorVisionEffect(effects.ColorVisionDeuteranopia, 0.85, true),
		effects.VignetteEffect(),
	)
	if err != nil {
		t.Fatalf("Compile chain with ColorVisionEffect failed: %v", err)
	}
	if chain.Len() != 3 {
		t.Fatalf("chain.Len() = %d, want 3", chain.Len())
	}

	// Verify HighContrast correction increases contrast between dark and light colors.
	pair := image.NewRGBA(image.Rect(0, 0, 2, 1))
	pair.SetRGBA(0, 0, color.RGBA{70, 70, 70, 255})
	pair.SetRGBA(1, 0, color.RGBA{185, 185, 185, 255})
	hc, err := effects.MustCompile(effects.ColorVisionEffect(effects.ColorVisionHighContrast, 1.0, true)).Reference(pair, 2, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hc.RGBAAt(0, 0).R >= 70 || hc.RGBAAt(1, 0).R <= 185 {
		t.Errorf("high-contrast did not expand tonal range: got %v and %v", hc.RGBAAt(0, 0), hc.RGBAAt(1, 0))
	}

	// ParseColorVisionMode round-trips and handles aliases.
	for _, tc := range []struct {
		in   string
		want effects.ColorVisionMode
		ok   bool
	}{
		{"none", effects.ColorVisionNone, true},
		{"normal", effects.ColorVisionNone, true},
		{"protanopia", effects.ColorVisionProtanopia, true},
		{"deuteranopia", effects.ColorVisionDeuteranopia, true},
		{"tritanopia", effects.ColorVisionTritanopia, true},
		{"high-contrast", effects.ColorVisionHighContrast, true},
		{"high_contrast", effects.ColorVisionHighContrast, true},
		{"unknown", effects.ColorVisionNone, false},
	} {
		got, ok := effects.ParseColorVisionMode(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseColorVisionMode(%q) = (%v, %v), want (%v, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
