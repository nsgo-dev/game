package effects_test

import (
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/effects"
	"github.com/nsgo-dev/nsgo/core"
)

// FuzzCompile checks that any source either fails to compile with an error naming the effect, or compiles alone and chained after another effect into a program whose source declares the effect under the chain's names.
func FuzzCompile(f *testing.F) {
	for _, src := range []string{
		invertSource,
		blurSource,
		"package main\n\nfunc Effect(pos vec2) vec4 {\n\treturn Input(pos)\n}\n",
		"package main\n\nvar Tint vec4\n\nfunc Effect(pos vec2) vec4 {\n\treturn Input(pos) * Tint\n}\n",
		"package main\n\nfunc Effect(pos vec2) vec4 {\n\tInput := pos\n\treturn vec4(Input, 0, 1)\n}\n",
		"package main\n\nfunc Effect(pos vec2) vec4 {\n\treturn (\n}\n",
		"package main\n\nimport \"math\"\n",
		"",
	} {
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, src string) {
		e := effects.Effect{Name: "fuzz", Source: src}
		c, err := effects.Compile(effects.Effect{Source: blurSource}, e)
		if err != nil {
			if !strings.HasPrefix(err.Error(), "effects: ") {
				t.Fatalf("an error without the package's prefix: %v", err)
			}
			return
		}
		if c.Len() != 2 || !strings.Contains(c.Source(), "p1_Effect") {
			t.Fatalf("a chain of %d effects:\n%s", c.Len(), c.Source())
		}
		if _, err := effects.Compile(e); err != nil {
			t.Fatalf("it compiled chained but not alone: %v", err)
		}
	})
}

// FuzzScreenEffects checks that vignette, flash and shake draw a premultiplied picture at any uniforms, NaN and infinities among them, and that the shake moves the frame no farther than its amplitude.
func FuzzScreenEffects(f *testing.F) {
	f.Add(0.9, 0.35, 0.6, 0.4, 3.0, 0.05, 12.0, 0.3)
	f.Add(1.0, 0.0, 0.0, 1.0, 0.0, 0.0, 0.0, 0.0)
	f.Add(-1.0, 2.0, -3.0, -0.5, 1e9, 1e6, -5.0, 100.0)
	f.Add(math.NaN(), math.Inf(1), math.Inf(-1), math.NaN(), math.Inf(1), math.NaN(), 1.0, math.NaN())
	chain := effects.MustCompile(effects.ShakeEffect(), effects.VignetteEffect(), effects.FlashEffect())
	f.Fuzz(func(t *testing.T, strength, radius, softness, amount, amplitude, seconds, frequency, phase float64) {
		u := []core.Uniforms{
			{"Amplitude": amplitude, "Time": seconds, "Frequency": frequency, "Phase": []float64{phase, -phase, 2 * phase, 0}},
			{"Strength": strength, "Radius": radius, "Softness": softness, "Color": []float64{softness, radius, strength}},
			{"Amount": amount, "Color": core.RGB(0x80ff40)},
		}
		img, err := chain.Reference(sceneFrame(), 16, 9, u)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < len(img.Pix); i += 4 {
			p := img.Pix[i : i+4]
			if p[0] > p[3] || p[1] > p[3] || p[2] > p[3] {
				t.Fatalf("pixel %d is not premultiplied: %v", i/4, p)
			}
		}
		x, y := effects.ShakeOffset(amplitude, seconds, frequency, [4]float64{phase, -phase, 2 * phase, 0})
		if limit := math.Abs(amplitude) * (1 + 1e-9); math.Abs(x) > limit || math.Abs(y) > limit {
			t.Fatalf("offset (%g, %g) past amplitude %g", x, y, amplitude)
		}
	})
}
