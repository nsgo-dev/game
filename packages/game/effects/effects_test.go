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

func TestMain(m *testing.M) { nsgotest.Main(m) }

// invertSource turns the frame's colors around by Amount. Its local half shadows the package's const, which the chain must leave alone, and its helper shares its name with blurSource's.
const invertSource = `package main

// Amount is how far the colors turn, 0 to 1.
var Amount float

const half = 0.5

func turned(c vec4) vec4 {
	return vec4(c.a-c.r, c.a-c.g, c.a-c.b, c.a)
}

func Effect(pos vec2) vec4 {
	half := Input(pos)
	return half + (turned(half)-half)*Amount
}
`

// blurSource averages each point with the one Offset pixels to its right, kept inside the frame.
const blurSource = `package main

// Offset is how far to the right, in the frame's pixels, the second tap reads.
var Offset float

const half = 0.5

func turned(a, b vec4) vec4 {
	return (a + b) * half
}

func Effect(pos vec2) vec4 {
	size := InputSize()
	p := vec2(min(pos.x+Offset, size.x-half), pos.y)
	return turned(Input(pos), Input(p))
}
`

var invert = effects.Effect{Name: "invert", Source: invertSource, Reference: func(in effects.Input, u core.Uniforms, x, y float64) [4]float64 {
	amount := effects.Float(u, "Amount")
	c := in.At(x, y)
	t := [4]float64{c[3] - c[0], c[3] - c[1], c[3] - c[2], c[3]}
	for k := range c {
		c[k] += (t[k] - c[k]) * amount
	}
	return c
}}

var blur = effects.Effect{Name: "blur", Source: blurSource, Reference: func(in effects.Input, u core.Uniforms, x, y float64) [4]float64 {
	w, _ := in.Size()
	a, b := in.At(x, y), in.At(math.Min(x+effects.Float(u, "Offset"), w-0.5), y)
	for k := range a {
		a[k] = (a[k] + b[k]) * 0.5
	}
	return a
}}

// full sets the invert all the way and the blur's second tap one pixel to the right.
var full = []core.Uniforms{{"Amount": 1}, {"Offset": 1}}

// testFrame is an 8 by 6 frame of opaque bands and a translucent column, premultiplied.
func testFrame() *image.RGBA {
	f := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for y := range 6 {
		for x := range 8 {
			c := color.RGBA{uint8(x * 32), uint8(y * 48), uint8(255 - x*24), 255}
			if x == 5 {
				c = color.RGBA{c.R / 2, c.G / 2, c.B / 2, 128}
			}
			f.SetRGBA(x, y, c)
		}
	}
	return f
}

func TestTwoEffectsChainAndDrawTheirReferencePicture(t *testing.T) {
	chain := effects.MustCompile(invert, blur)
	if chain.Len() != 2 {
		t.Fatalf("Len %d, want 2", chain.Len())
	}
	got, err := chain.Reference(testFrame(), 16, 12, full)
	if err != nil {
		t.Fatal(err)
	}
	// A PNG holds straight colors: compare those, which a translucent pixel's premultiplied ones would round away from.
	straight := image.NewNRGBA(got.Rect)
	draw.Draw(straight, straight.Rect, got, got.Rect.Min, draw.Src)
	gametest.AssertGolden(t, "invert-then-blur", straight)

	// Output pixel (2, 0) maps onto frame point (1.25, 0.25): the blur averages the inverted pixels (1, 0) and (2, 0).
	inverted := func(x int) [3]float64 {
		return [3]float64{255 - float64(x*32), 255, float64(x * 24)}
	}
	a, b := inverted(1), inverted(2)
	px := got.RGBAAt(2, 0)
	for k, v := range []uint8{px.R, px.G, px.B} {
		if want := uint8(math.Round((a[k] + b[k]) / 2)); v != want {
			t.Fatalf("pixel (2, 0) channel %d = %d, want %d", k, v, want)
		}
	}
}

// square multiplies each color by itself, which the blur's average does not commute with.
var square = effects.Effect{Name: "square", Source: `package main

func Effect(pos vec2) vec4 {
	c := Input(pos)
	return c * c
}
`, Reference: func(in effects.Input, _ core.Uniforms, x, y float64) [4]float64 {
	c := in.At(x, y)
	for k := range c {
		c[k] *= c[k]
	}
	return c
}}

func TestTheChainsOrderIsTheDrawingOrder(t *testing.T) {
	ab := effects.MustCompile(square, blur)
	ba := effects.MustCompile(blur, square)
	if ab.Source() == ba.Source() {
		t.Fatal("chains in two orders compiled into the same source")
	}
	// The blur averages squares in one order, and the square squares averages in the other.
	x, _ := ab.Reference(testFrame(), 8, 6, []core.Uniforms{nil, {"Offset": 1}})
	y, _ := ba.Reference(testFrame(), 8, 6, []core.Uniforms{{"Offset": 1}})
	if slices.Equal(x.Pix, y.Pix) {
		t.Fatal("chains in two orders drew the same picture")
	}
}

func TestTheFusedSourceIsDeterministicAndRenamesEachEffect(t *testing.T) {
	a := effects.MustCompile(invert, blur).Source()
	b := effects.MustCompile(invert, blur).Source()
	if a != b {
		t.Fatal("the same effects compiled into two sources")
	}
	for _, want := range []string{
		"var P0_Amount float", "var P1_Offset float", "const p0_half = 0.5", "const p1_half = 0.5",
		"func p0_turned(c vec4) vec4", "func p1_turned(a, b vec4) vec4",
		// The invert's local half stays a local; its input is the frame.
		"half := image0At(pos)", "return half + (p0_turned(half)-half)*P0_Amount",
		// The blur's input is the invert.
		"size := image0Size()", "return p1_turned(p0_Effect(pos), p0_Effect(p))",
		"return p1_Effect(pos / rectSize() * image0Size())",
		// Comments stay.
		"// Amount is how far the colors turn, 0 to 1.",
	} {
		if !strings.Contains(a, want) {
			t.Errorf("the source lacks %q:\n%s", want, a)
		}
	}
	if strings.Count(a, "package main") != 1 {
		t.Errorf("the source has %d package clauses, want 1", strings.Count(a, "package main"))
	}
}

func TestNoEffectsDrawTheFrameAsItIs(t *testing.T) {
	chain := effects.MustCompile()
	frame := testFrame()
	got, err := chain.Reference(frame, 16, 12, nil)
	if err != nil {
		t.Fatal(err)
	}
	for y := range 12 {
		for x := range 16 {
			if got.RGBAAt(x, y) != frame.RGBAAt(x/2, y/2) {
				t.Fatalf("pixel (%d, %d) = %v, want the frame's (%d, %d) %v", x, y, got.RGBAAt(x, y), x/2, y/2, frame.RGBAAt(x/2, y/2))
			}
		}
	}
	if !strings.Contains(chain.Source(), "return image0At(pos / rectSize() * image0Size())") {
		t.Fatalf("the empty chain does not read the frame:\n%s", chain.Source())
	}
}

func TestReferenceReadsAFrameOffItsOrigin(t *testing.T) {
	big := image.NewRGBA(image.Rect(-3, 2, 10, 12))
	frame := big.SubImage(image.Rect(1, 4, 3, 6)).(*image.RGBA)
	frame.SetRGBA(1, 4, color.RGBA{10, 20, 30, 255})
	frame.SetRGBA(2, 5, color.RGBA{40, 50, 60, 255})
	got, err := effects.MustCompile().Reference(frame, 2, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.RGBAAt(0, 0) != (color.RGBA{10, 20, 30, 255}) || got.RGBAAt(1, 1) != (color.RGBA{40, 50, 60, 255}) {
		t.Fatalf("got %v and %v", got.RGBAAt(0, 0), got.RGBAAt(1, 1))
	}
}

func TestReferenceDrawsAtTheUniformsItIsGiven(t *testing.T) {
	chain := effects.MustCompile(invert, blur)
	at := func(u []core.Uniforms) *image.RGBA {
		t.Helper()
		got, err := chain.Reference(testFrame(), 8, 6, u)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	// Left out, the uniforms are 0, as on the GPU: no invert, and the blur's two taps read the same pixel, which draws the frame as it is.
	if got, frame := at(nil), testFrame(); !slices.Equal(got.Pix, frame.Pix) {
		t.Fatalf("no uniforms drew %v, want the frame %v", got.Pix[:8], frame.Pix[:8])
	}
	// Half an invert of an opaque pixel is mid gray, whatever its color.
	half := at([]core.Uniforms{{"Amount": core.Constant(0.5)}, {"Offset": float32(0)}})
	if px := half.RGBAAt(0, 0); px != (color.RGBA{128, 128, 128, 255}) {
		t.Fatalf("half an invert drew %v, want mid gray", px)
	}
	if slices.Equal(half.Pix, at(full).Pix) {
		t.Fatal("two amounts drew the same picture")
	}
	for name, tc := range map[string]struct {
		u    []core.Uniforms
		want string
	}{
		"unknown":  {[]core.Uniforms{{"Offset": 1}}, "effect 0 (invert) has no uniform Offset"},
		"extra":    {[]core.Uniforms{nil, nil, {"Amount": 1}}, "uniforms for 3 effects, but the chain has 2"},
		"animated": {[]core.Uniforms{{"Amount": []any{animatedNode{}}}}, "effect 0 (invert): uniform Amount follows an animated node"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := chain.Reference(testFrame(), 4, 4, tc.u); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// animatedNode is a node whose value only the platform knows, as a core.AnimatedValue's.
type animatedNode struct{ core.AnimatedNode }

func TestFloatReadsANumberUniform(t *testing.T) {
	u := core.Uniforms{"f": float32(1.5), "i": 2, "u": uint64(3), "b": true, "c": core.Constant(4), "v": []float64{1, 2}}
	for name, want := range map[string]float64{"f": 1.5, "i": 2, "u": 3, "b": 1, "c": 4, "v": 0, "missing": 0} {
		if got := effects.Float(u, name); got != want {
			t.Errorf("Float(%s) = %g, want %g", name, got, want)
		}
	}
}

func TestReferenceNeedsEveryEffectsReference(t *testing.T) {
	gpuOnly := invert
	gpuOnly.Reference = nil
	_, err := effects.MustCompile(blur, gpuOnly).Reference(testFrame(), 4, 4, nil)
	if err == nil || !strings.Contains(err.Error(), "effect 1 (invert) has no Reference") {
		t.Fatalf("err = %v", err)
	}
	empty, err := effects.MustCompile(invert).Reference(nil, 0, 3, full[:1])
	if err != nil || empty.Rect.Dx() != 0 {
		t.Fatalf("an empty picture: %v, %v", empty.Rect, err)
	}
	blank, err := effects.MustCompile(invert).Reference(nil, 2, 2, full[:1])
	if err != nil || slices.ContainsFunc(blank.Pix, func(b uint8) bool { return b != 0 }) {
		t.Fatalf("no frame drew %v, %v", blank.Pix, err)
	}
}

func TestCompileRefusesWhatTheChainCannotFuse(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{"no entry", "package main\n\nfunc Other(pos vec2) vec4 {\n\treturn vec4(0)\n}\n", "an effect declares func Effect(pos vec2) vec4"},
		{"declares Input", "package main\n\nfunc Input(pos vec2) vec4 {\n\treturn vec4(0)\n}\n\nfunc Effect(pos vec2) vec4 {\n\treturn Input(pos)\n}\n", "3:6: Input is the chain's"},
		{"declares Fragment", "package main\n\nfunc Fragment(dst vec4) vec4 {\n\treturn dst\n}\n\nfunc Effect(pos vec2) vec4 {\n\treturn vec4(0)\n}\n", "Fragment is the chain's"},
		{"imports", "package main\n\nimport \"math\"\n\nfunc Effect(pos vec2) vec4 {\n\treturn vec4(0)\n}\n", "3:8: an effect imports nothing"},
		{"reserved name", "package main\n\nfunc Effect(pos vec2) vec4 {\n\tp1_x := Input(pos)\n\treturn p1_x\n}\n", "4:2: p1_x: names starting with p or P"},
		{"reads an image", "package main\n\nfunc Effect(pos vec2) vec4 {\n\treturn image1At(pos)\n}\n", "4:9: an effect reads the frame with Input and InputSize, not image1At"},
		{"reads the view", "package main\n\nfunc Effect(pos vec2) vec4 {\n\treturn Input(pos * rectSize() / rectSize())\n}\n", "4:21: an effect works in the frame's pixels, which InputSize measures, not the view's rectSize"},
		{"reads the scale", "package main\n\nfunc Effect(pos vec2) vec4 {\n\treturn Input(pos) * pixelScale()\n}\n", "not the view's pixelScale"},
		{"compiler's name", "package main\n\nfunc Effect(pos vec2) vec4 {\n\tx := __imageSizes[0]\n\treturn Input(pos + x)\n}\n", "4:7: __imageSizes: names starting with __ are the shader compiler's"},
		{"syntax", "package main\n\nfunc Effect(pos vec2) vec4 {\n\treturn (\n}\n", "5:1: expected operand"},
		{"type", "package main\n\nfunc Effect(pos vec2) vec4 {\n\treturn Input(pos).xy\n}\n", "4:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := effects.Compile(invert, effects.Effect{Name: "bad", Source: tc.src})
			if err == nil {
				t.Fatal("it compiled")
			}
			if !strings.HasPrefix(err.Error(), "effects: effect 1 (bad): ") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want effect 1 (bad) and %q", err, tc.want)
			}
		})
	}
	if _, err := effects.Compile(effects.Effect{Source: "package main\n"}); err == nil || !strings.HasPrefix(err.Error(), "effects: effect 0: ") {
		t.Fatalf("an unnamed effect: %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("MustCompile of a bad effect did not panic")
		}
	}()
	effects.MustCompile(effects.Effect{Source: "package main\n"})
}

func TestUniformsTakeEachEffectsNames(t *testing.T) {
	chain := effects.MustCompile(invert, blur)
	if !chain.HasUniform(0, "Amount") || !chain.HasUniform(1, "Offset") || chain.HasUniform(0, "Offset") || chain.HasUniform(2, "Amount") || chain.HasUniform(-1, "Amount") {
		t.Fatal("HasUniform names the wrong effects' uniforms")
	}
	u := chain.Uniforms([]core.Uniforms{{"Amount": 0.5}, {"Offset": 2}})
	if len(u) != 2 || u["P0_Amount"] != 0.5 || u["P1_Offset"] != 2 {
		t.Fatalf("Uniforms = %v", u)
	}
	for _, p := range chain.Uniforms(nil) {
		t.Fatalf("no uniforms gave %v", p)
	}
	for name, per := range map[string][]core.Uniforms{
		"unknown": {{"Offset": 1}},
		"extra":   {nil, nil, {"Amount": 1}},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("it did not panic")
				}
			}()
			chain.Uniforms(per)
		})
	}
	for _, name := range []string{"P0_Amount", "P1_Offset"} {
		if !chain.Program().HasUniform(name) {
			t.Fatalf("the program has no uniform %s", name)
		}
	}
}

func TestTheViewDrawsTheFrameThroughTheChainWithUniformsOnTheFrameClock(t *testing.T) {
	chain := effects.MustCompile(invert, blur)
	frame := testFrame()
	var clock *core.AnimatedValue
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		clock = effects.Clock(cx, 2*time.Second)
		return effects.View{
			Chain:    chain,
			Frame:    core.ImageSource{Pixels: frame, Version: 1},
			Uniforms: []core.Uniforms{{"Amount": clock}, {"Offset": 0.25}},
			Style:    core.Style{Width: core.Pt(160), Height: core.Pt(120)},
			TestID:   "game",
		}
	})
	sh := app.FindMounted("id:game").Shader
	if sh == nil {
		t.Fatal("the view mounted no shader")
	}
	if sh.Role != nsgotest.ShaderFill || sh.WGSL {
		t.Fatalf("role %v, WGSL %v: want a Kage fill", sh.Role, sh.WGSL)
	}
	if !slices.ContainsFunc(sh.Nodes, func(n nsgotest.ShaderNode) bool { return n.Node == clock.AnimatedNodeID() }) {
		t.Fatalf("no lane follows the clock: %v", sh.Nodes)
	}
	if !slices.Contains(sh.Lanes, 0.25) {
		t.Fatalf("no lane holds the blur's offset: %v", sh.Lanes)
	}
	// The clock runs on the platform: frames pass with no new shader sent.
	app.Advance(time.Second)
	if app.FindMounted("id:game").Shader == nil {
		t.Fatal("the shader went")
	}
}

func TestTheViewFollowsItsFrameAndUniformsFuncs(t *testing.T) {
	chain := effects.MustCompile(invert)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		amount := core.NewSignal(cx, 0.25)
		version := core.NewSignal(cx, uint64(1))
		frame := testFrame()
		return core.View{Children: []core.Node{
			effects.View{
				Chain:        chain,
				FrameFunc:    func() core.ImageSource { return core.ImageSource{Pixels: frame, Version: version.Get()} },
				UniformsFunc: func() []core.Uniforms { return []core.Uniforms{{"Amount": amount.Get()}} },
				Style:        core.Style{Width: core.Pt(80), Height: core.Pt(60)},
				TestID:       "game",
			},
			core.View{TestID: "more", Style: core.Style{Width: core.Pt(40), Height: core.Pt(40)}, OnPress: func(core.PressEvent) {
				amount.Set(0.75)
				version.Set(version.GetUntracked() + 1)
			}},
			// A nil chain draws the frame as it is.
			effects.View{Frame: core.ImageSource{Pixels: frame}, TestID: "plain"},
		}}
	})
	if !slices.Contains(app.FindMounted("id:game").Shader.Lanes, 0.25) {
		t.Fatalf("lanes %v, want the amount 0.25", app.FindMounted("id:game").Shader.Lanes)
	}
	app.Press("id:more")
	lanes := app.FindMounted("id:game").Shader.Lanes
	if !slices.Contains(lanes, 0.75) {
		t.Fatalf("lanes %v, want the amount 0.75", lanes)
	}
	if app.FindMounted("id:plain").Shader == nil {
		t.Fatal("a nil chain mounted no shader")
	}
}

// nsgotest's mounted shader does not list the images it reads (nsgotest.Shader has no Images, nsgo-dev/nsgo#2251), so the view's own shader shows that the frame is its image0.
func TestTheViewsFrameIsItsShadersImage(t *testing.T) {
	chain := effects.MustCompile(invert)
	frame := testFrame()
	sh, ok := effects.View{Chain: chain, Frame: core.ImageSource{Pixels: frame, Version: 1}}.Render(nil).(core.Shader)
	if !ok {
		t.Fatal("the view is not a core.Shader")
	}
	if sh.Program != chain.Program() || sh.ImagesFunc != nil || len(sh.Images) != 1 || sh.Images[0].Pixels != frame || sh.Images[0].Version != 1 {
		t.Fatalf("images %v, func %v: want the frame at version 1 as image0 of the chain's program", sh.Images, sh.ImagesFunc != nil)
	}
	version := uint64(1)
	sh = effects.View{Chain: chain, FrameFunc: func() core.ImageSource { return core.ImageSource{Pixels: frame, Version: version} }}.Render(nil).(core.Shader)
	if sh.Images != nil || sh.ImagesFunc == nil {
		t.Fatalf("a FrameFunc gave images %v and no ImagesFunc", sh.Images)
	}
	for want := range uint64(3) {
		version = want
		if got := sh.ImagesFunc(); len(got) != 1 || got[0].Pixels != frame || got[0].Version != want {
			t.Fatalf("ImagesFunc() = %v, want the frame at version %d", got, want)
		}
	}
	if plain := (effects.View{Frame: core.ImageSource{Pixels: frame}}).Render(nil).(core.Shader); plain.Program == nil || len(plain.Images) != 1 || plain.Images[0].Pixels != frame {
		t.Fatalf("a nil chain gave program %v and images %v", plain.Program, plain.Images)
	}
}

func BenchmarkCompile(b *testing.B) {
	for b.Loop() {
		effects.MustCompile(invert, blur)
	}
}

func BenchmarkReference(b *testing.B) {
	chain := effects.MustCompile(invert, blur)
	frame := image.NewRGBA(image.Rect(0, 0, 320, 180))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := chain.Reference(frame, 320, 180, full); err != nil {
			b.Fatal(err)
		}
	}
}
