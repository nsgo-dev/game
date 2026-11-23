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

	"github.com/nsgo-dev/game/packages/game/effects"
	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// sceneFrame is a 32 by 18 frame of a sky gradient over a checkered floor, with a translucent square in the middle, premultiplied.
func sceneFrame() *image.RGBA {
	f := image.NewRGBA(image.Rect(0, 0, 32, 18))
	for y := range 18 {
		for x := range 32 {
			c := color.RGBA{uint8(40 + y*8), uint8(120 + y*6), 230, 255}
			if y >= 12 {
				c = color.RGBA{60, 160, 70, 255}
				if (x/4+y/3)%2 == 0 {
					c = color.RGBA{90, 200, 100, 255}
				}
			}
			if x >= 13 && x < 19 && y >= 6 && y < 12 {
				c = color.RGBA{120, 20, 20, 128}
			}
			f.SetRGBA(x, y, c)
		}
	}
	return f
}

// straight converts a premultiplied picture to the straight colors a PNG holds, which a translucent pixel's premultiplied ones would round away from.
func straight(img *image.RGBA) *image.NRGBA {
	out := image.NewNRGBA(img.Rect)
	draw.Draw(out, out.Rect, img, img.Rect.Min, draw.Src)
	return out
}

// reference draws sceneFrame through effs at twice its size with uniforms.
func reference(t *testing.T, effs []effects.Effect, uniforms []core.Uniforms) *image.RGBA {
	t.Helper()
	got, err := effects.MustCompile(effs...).Reference(sceneFrame(), 64, 36, uniforms)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

var (
	vignetteUniforms = core.Uniforms{"Strength": 0.9, "Radius": 0.35, "Softness": 0.6, "Color": core.RGB(0x100820)}
	flashUniforms    = core.Uniforms{"Amount": 0.4, "Color": core.RGB(0xffffff)}
	shakeUniforms    = core.Uniforms{"Amplitude": 3, "Time": 0.05, "Frequency": 12, "Phase": []float64{0.3, 1.7, 2.9, 4.1}}
)

func TestEachEffectDrawsItsReferencePicture(t *testing.T) {
	for _, tc := range []struct {
		name     string
		effs     []effects.Effect
		uniforms []core.Uniforms
	}{
		{"vignette", []effects.Effect{effects.VignetteEffect()}, []core.Uniforms{vignetteUniforms}},
		{"flash", []effects.Effect{effects.FlashEffect()}, []core.Uniforms{flashUniforms}},
		{"shake", []effects.Effect{effects.ShakeEffect()}, []core.Uniforms{shakeUniforms}},
		{"shake-vignette-flash", []effects.Effect{effects.ShakeEffect(), effects.VignetteEffect(), effects.FlashEffect()}, []core.Uniforms{shakeUniforms, vignetteUniforms, flashUniforms}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gametest.AssertGolden(t, tc.name, straight(reference(t, tc.effs, tc.uniforms)))
		})
	}
}

func TestTheEffectsCompileAloneAndChained(t *testing.T) {
	all := []effects.Effect{effects.ShakeEffect(), effects.VignetteEffect(), effects.FlashEffect()}
	chain, err := effects.Compile(all...)
	if err != nil {
		t.Fatal(err)
	}
	for i, names := range [][]string{{"Amplitude", "Time", "Frequency", "Phase"}, {"Strength", "Radius", "Softness", "Color"}, {"Amount", "Color"}} {
		for _, name := range names {
			if !chain.HasUniform(i, name) {
				t.Errorf("effect %d (%s) has no uniform %s", i, all[i].Name, name)
			}
		}
	}
	for _, e := range all {
		if _, err := effects.Compile(e); err != nil {
			t.Errorf("%s alone: %v", e.Name, err)
		}
		if e.Reference == nil {
			t.Errorf("%s has no Reference", e.Name)
		}
	}
}

// px reads a premultiplied pixel of a picture as numbers from 0 to 255.
func px(img *image.RGBA, x, y int) [4]float64 {
	c := img.RGBAAt(x, y)
	return [4]float64{float64(c.R), float64(c.G), float64(c.B), float64(c.A)}
}

func near(a, b [4]float64, tolerance float64) bool {
	for k := range a {
		if math.Abs(a[k]-b[k]) > tolerance {
			return false
		}
	}
	return true
}

func TestTheVignetteTurnsTheCornersAndLeavesTheCenter(t *testing.T) {
	frame := reference(t, nil, nil)
	hard := reference(t, []effects.Effect{effects.VignetteEffect()}, []core.Uniforms{{"Strength": 1, "Radius": 0.5, "Color": core.RGB(0x204060)}})
	// Inside the radius the frame is as it was; past it, with no softness, the color all the way, at each pixel's alpha.
	if got, want := px(hard, 32, 18), px(frame, 32, 18); got != want {
		t.Fatalf("center %v, want the frame's %v", got, want)
	}
	if got := px(hard, 0, 0); got != [4]float64{0x20, 0x40, 0x60, 255} {
		t.Fatalf("corner %v, want the color", got)
	}
	// Left out, the uniforms are 0: no strength draws the frame as it is.
	if none := reference(t, []effects.Effect{effects.VignetteEffect()}, nil); !slices.Equal(none.Pix, frame.Pix) {
		t.Fatal("a vignette of no strength changed the frame")
	}
	// A soft vignette darkens toward the corner, and half the strength half as far.
	soft := reference(t, []effects.Effect{effects.VignetteEffect()}, []core.Uniforms{{"Strength": 1, "Radius": 0.2, "Softness": 0.8}})
	half := reference(t, []effects.Effect{effects.VignetteEffect()}, []core.Uniforms{{"Strength": 0.5, "Radius": 0.2, "Softness": 0.8}})
	if a, b, c := px(soft, 8, 4)[2], px(soft, 4, 2)[2], px(soft, 0, 0)[2]; !(a > b && b > c && c == 0) {
		t.Fatalf("blue toward the corner %g, %g, %g: want falling to 0", a, b, c)
	}
	if got, want := px(half, 0, 0)[2], px(frame, 0, 0)[2]/2; math.Abs(got-want) > 1 {
		t.Fatalf("half the strength left blue %g at the corner, want %g", got, want)
	}
}

func TestTheFlashTurnsTheWholeFrameOpaque(t *testing.T) {
	frame := reference(t, nil, nil)
	full := reference(t, []effects.Effect{effects.FlashEffect()}, []core.Uniforms{{"Amount": 1, "Color": core.RGB(0xff8000)}})
	for y := range 36 {
		for x := range 64 {
			if got := px(full, x, y); got != [4]float64{255, 128, 0, 255} {
				t.Fatalf("pixel (%d, %d) = %v, want the flash's color", x, y, got)
			}
		}
	}
	// Half a white flash over the translucent square lifts it halfway to opaque white.
	half := reference(t, []effects.Effect{effects.FlashEffect()}, []core.Uniforms{{"Amount": 0.5, "Color": core.RGB(0xffffff)}})
	under := px(frame, 32, 18)
	want := [4]float64{}
	for k := range want {
		want[k] = under[k] + (255-under[k])*0.5
	}
	if got := px(half, 32, 18); !near(got, want, 1) {
		t.Fatalf("half a flash over the square %v, want %v", got, want)
	}
	// Past 1 the amount is 1, and below 0 it is 0.
	if over := reference(t, []effects.Effect{effects.FlashEffect()}, []core.Uniforms{{"Amount": 3, "Color": core.RGB(0xff8000)}}); !slices.Equal(over.Pix, full.Pix) {
		t.Fatal("an amount past 1 drew other than 1")
	}
	if under := reference(t, []effects.Effect{effects.FlashEffect()}, []core.Uniforms{{"Amount": -2, "Color": core.RGB(0xff8000)}}); !slices.Equal(under.Pix, frame.Pix) {
		t.Fatal("an amount below 0 drew other than the frame")
	}
}

func TestTheShakeMovesTheFrameByItsOffset(t *testing.T) {
	frame := sceneFrame()
	// At Time 0 with phases a quarter turn on, the frame moves by Amplitude along both directions.
	phase := []float64{math.Pi / 2, math.Pi / 2, math.Pi / 2, math.Pi / 2}
	x, y := effects.ShakeOffset(4, 0, 12, [4]float64(phase))
	if math.Abs(x-4) > 1e-9 || math.Abs(y-4) > 1e-9 {
		t.Fatalf("offset (%g, %g), want (4, 4)", x, y)
	}
	got, err := effects.MustCompile(effects.ShakeEffect()).Reference(frame, 32, 18, []core.Uniforms{{"Amplitude": 4, "Frequency": 12, "Phase": phase}})
	if err != nil {
		t.Fatal(err)
	}
	for py := range 18 {
		for pxl := range 32 {
			// The pixel 4 up and to the left, the frame's edge where that is off it.
			want := frame.RGBAAt(max(pxl-4, 0), max(py-4, 0))
			if got.RGBAAt(pxl, py) != want {
				t.Fatalf("pixel (%d, %d) = %v, want %v", pxl, py, got.RGBAAt(pxl, py), want)
			}
		}
	}
	// No amplitude draws the frame as it is.
	still, _ := effects.MustCompile(effects.ShakeEffect()).Reference(frame, 32, 18, []core.Uniforms{{"Time": 0.3, "Frequency": 12, "Phase": phase}})
	if !slices.Equal(still.Pix, frame.Pix) {
		t.Fatal("a shake of no amplitude moved the frame")
	}
	// The frame sways over time.
	a, b := []float64{}, []float64{}
	for i := range 8 {
		x, y := effects.ShakeOffset(1, float64(i)/48, 12, [4]float64{0.3, 1.7, 2.9, 4.1})
		a, b = append(a, x), append(b, y)
	}
	if slices.Min(a) > -0.2 || slices.Max(a) < 0.2 || slices.Min(b) > -0.2 || slices.Max(b) < 0.2 {
		t.Fatalf("the offset hardly sways over a cycle: %v, %v", a, b)
	}
}

func TestTheReferenceReadsVectorUniformsAsTheGPUFillsLanes(t *testing.T) {
	flash := effects.MustCompile(effects.FlashEffect())
	want, err := flash.Reference(sceneFrame(), 8, 4, []core.Uniforms{{"Amount": 1, "Color": core.RGB(0x336699)}})
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]any{
		"float64":   []float64{0.2, 0.4, 0.6},
		"float32":   []float32{0.2, 0.4, 0.6},
		"constants": []core.AnimatedNode{core.Constant(0.2), core.Constant(0.4), core.Constant(0.6)},
		"any":       []any{0.2, core.Constant(0.4), float32(0.6)},
	} {
		got, err := flash.Reference(sceneFrame(), 8, 4, []core.Uniforms{{"Amount": 1, "Color": c}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !slices.Equal(got.Pix, want.Pix) {
			t.Errorf("%s drew %v, want %v", name, got.Pix[:4], want.Pix[:4])
		}
	}
	// Lanes left out are 0.
	got, _ := flash.Reference(sceneFrame(), 1, 1, []core.Uniforms{{"Amount": 1, "Color": []int{1}}})
	if c := got.RGBAAt(0, 0); c != (color.RGBA{255, 0, 0, 255}) {
		t.Fatalf("a color of one lane drew %v, want red", c)
	}
	// A node only the platform knows is refused.
	if _, err := flash.Reference(sceneFrame(), 1, 1, []core.Uniforms{{"Color": []core.AnimatedNode{animatedNode{}}}}); err == nil || !strings.Contains(err.Error(), "uniform Color follows an animated node") {
		t.Fatalf("err = %v", err)
	}
}

// screen mounts a view of sceneFrame through shake, vignette and flash, with a button that triggers both, and returns the app and the flash and shake.
func screen(t *testing.T, seed uint64, reduce func() bool, opts ...nsgotest.Option) (*nsgotest.App, *effects.Flash, *effects.Shake) {
	t.Helper()
	chain := effects.MustCompile(effects.ShakeEffect(), effects.VignetteEffect(), effects.FlashEffect())
	var flash *effects.Flash
	var shake *effects.Shake
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		flash = effects.NewFlash(cx, effects.FlashConfig{Color: core.RGB(0xffffff), ReduceMotion: reduce})
		shake = effects.NewShake(cx, effects.ShakeConfig{Seed: seed, ReduceMotion: reduce})
		return core.View{Children: []core.Node{
			effects.View{
				Chain:    chain,
				Frame:    core.ImageSource{Pixels: sceneFrame(), Version: 1},
				Uniforms: []core.Uniforms{shake.Uniforms(), vignetteUniforms, flash.Uniforms()},
				Style:    core.Style{Width: core.Pt(320), Height: core.Pt(180)},
				TestID:   "game",
			},
			core.View{TestID: "hit", Style: core.Style{Width: core.Pt(40), Height: core.Pt(40)}, OnPress: func(core.PressEvent) {
				flash.Trigger(0.8, 400*time.Millisecond)
				shake.Trigger(6, 500*time.Millisecond)
			}},
			core.View{TestID: "stop", Style: core.Style{Width: core.Pt(40), Height: core.Pt(40)}, OnPress: func(core.PressEvent) {
				flash.Stop()
				shake.Stop()
			}},
		}}
	}, opts...)
	return app, flash, shake
}

// value returns an animated node's value as the platform's animator has it.
func value(app *nsgotest.App, n core.AnimatedNode) float64 {
	return app.AnimatedValue(n.AnimatedNodeID())
}

func phases(app *nsgotest.App, s *effects.Shake) [4]float64 {
	var out [4]float64
	for i, n := range s.Uniforms()["Phase"].([]core.AnimatedNode) {
		out[i] = value(app, n)
	}
	return out
}

func TestTheFlashAndShakeFadeOnTheFrameClock(t *testing.T) {
	app, flash, shake := screen(t, 7, nil, nsgotest.ManualFrames())
	sh := app.FindMounted("id:game").Shader
	for _, n := range []core.AnimatedNode{flash.Amount(), shake.Amplitude()} {
		if !slices.ContainsFunc(sh.Nodes, func(s nsgotest.ShaderNode) bool { return s.Node == n.AnimatedNodeID() }) {
			t.Fatalf("no lane follows node %d: %v", n.AnimatedNodeID(), sh.Nodes)
		}
	}
	if value(app, flash.Amount()) != 0 || value(app, shake.Amplitude()) != 0 {
		t.Fatal("the flash or the shake runs before a trigger")
	}
	app.Press("id:hit")
	if got := value(app, flash.Amount()); got != 0.8 {
		t.Fatalf("the flash starts at %g, want 0.8", got)
	}
	if got := value(app, shake.Amplitude()); got != 6 {
		t.Fatalf("the shake starts at %g, want 6", got)
	}
	app.Advance(200 * time.Millisecond)
	f, a := value(app, flash.Amount()), value(app, shake.Amplitude())
	if !(f > 0 && f < 0.8) || !(a > 0 && a < 6) {
		t.Fatalf("halfway the flash is %g and the shake %g, want between", f, a)
	}
	clock := shake.Uniforms()["Time"].(core.AnimatedNode)
	if got := value(app, clock); math.Abs(got-0.2) > 0.02 {
		t.Fatalf("the shake's time is %g after 200 ms", got)
	}
	app.Advance(400 * time.Millisecond)
	if value(app, flash.Amount()) != 0 || value(app, shake.Amplitude()) != 0 {
		t.Fatalf("after the fade the flash is %g and the shake %g", value(app, flash.Amount()), value(app, shake.Amplitude()))
	}
	// Stop puts both at rest at once.
	app.Press("id:hit")
	app.Advance(50 * time.Millisecond)
	app.Press("id:stop")
	if value(app, flash.Amount()) != 0 || value(app, shake.Amplitude()) != 0 || value(app, clock) != 0 {
		t.Fatal("Stop left the flash or the shake running")
	}
}

func TestTheShakesPhasesFollowItsSeed(t *testing.T) {
	run := func(seed uint64) [][4]float64 {
		app, _, shake := screen(t, seed, nil, nsgotest.ManualFrames())
		var out [][4]float64
		for range 3 {
			app.Press("id:hit")
			out = append(out, phases(app, shake))
		}
		return out
	}
	a, b := run(7), run(7)
	if !slices.Equal(a, b) {
		t.Fatalf("the same seed drew %v, then %v", a, b)
	}
	if a[0] == a[1] || a[1] == a[2] {
		t.Fatalf("triggers drew the same phases: %v", a)
	}
	for _, p := range a {
		for _, v := range p {
			if v < 0 || v >= 2*math.Pi {
				t.Fatalf("a phase %g outside a turn", v)
			}
		}
	}
	// Another seed draws other phases.
	if c := run(8); c[0] == a[0] {
		t.Fatalf("seeds 7 and 8 drew the same phases %v", c[0])
	}
}

func TestReducedMotionKeepsTheFlashAndShakeStill(t *testing.T) {
	for name, tc := range map[string]struct {
		reduce func() bool
		opts   []nsgotest.Option
	}{
		"player":   {func() bool { return true }, nil},
		"platform": {nil, []nsgotest.Option{nsgotest.Environment(core.Environment{ReduceMotion: true})}},
	} {
		t.Run(name, func(t *testing.T) {
			app, flash, shake := screen(t, 7, tc.reduce, append(tc.opts, nsgotest.ManualFrames())...)
			app.Press("id:hit")
			if value(app, flash.Amount()) != 0 || value(app, shake.Amplitude()) != 0 || phases(app, shake) != [4]float64{} {
				t.Fatal("reduced motion let the flash or the shake run")
			}
		})
	}
	// The player's choice over the platform's: motion they allow runs on a platform that reduces it.
	app, flash, _ := screen(t, 7, func() bool { return false }, nsgotest.Environment(core.Environment{ReduceMotion: true}), nsgotest.ManualFrames())
	app.Press("id:hit")
	if value(app, flash.Amount()) != 0.8 {
		t.Fatal("the player's choice of motion did not run the flash")
	}
}

func TestTriggerRefusesWhatCannotShake(t *testing.T) {
	var flash *effects.Flash
	var shake *effects.Shake
	got := map[string]bool{}
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		flash = effects.NewFlash(cx, effects.FlashConfig{})
		shake = effects.NewShake(cx, effects.ShakeConfig{Frequency: math.NaN()})
		got["flash 0"] = flash.Trigger(0, time.Second)
		got["flash NaN"] = flash.Trigger(math.NaN(), time.Second)
		got["flash no fade"] = flash.Trigger(1, 0)
		got["shake 0"] = shake.Trigger(0, time.Second)
		got["shake inf"] = shake.Trigger(math.Inf(1), time.Second)
		got["shake no time"] = shake.Trigger(3, -time.Second)
		return core.View{}
	})
	for name, ran := range got {
		if ran {
			t.Errorf("%s ran", name)
		}
	}
	if f := shake.Uniforms()["Frequency"]; f != float64(effects.DefaultShakeFrequency) {
		t.Fatalf("a NaN frequency became %v, want the default", f)
	}
}

func BenchmarkEffectReferences(b *testing.B) {
	frame := image.NewRGBA(image.Rect(0, 0, 320, 180))
	chain := effects.MustCompile(effects.ShakeEffect(), effects.VignetteEffect(), effects.FlashEffect())
	u := []core.Uniforms{shakeUniforms, vignetteUniforms, flashUniforms}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := chain.Reference(frame, 320, 180, u); err != nil {
			b.Fatal(err)
		}
	}
}
