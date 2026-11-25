package pixelfx_test

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// dot is a test effect: one pixel of its palette's brightest step at its origin, and a count of the updates it was given.
func dot(updates *int) *pixelfx.Effect {
	return &pixelfx.Effect{
		Meta: pixelfx.Meta{Name: "Dot", Size: [2]int{8, 8}, Origin: [2]int{4, 4}, Duration: 1, FPS: 10, Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#000000", "#808080", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(3)},
		}},
		New: func(o *pixelfx.Options) pixelfx.Instance {
			C := o.Palette()
			return &pixelfx.Funcs{
				UpdateFunc: func(float64) { *updates++ },
				DrawFunc:   func(p *pixelfx.Painter, _ float64) { p.Px(0, 0, C[len(C)-1]) },
			}
		},
	}
}

func TestOptionsTakeDefaultsAndFitRamps(t *testing.T) {
	var n int
	p := pixelfx.NewPlayer(dot(&n), pixelfx.Options{Values: map[string]pixelfx.Value{"palette": pixelfx.Palette("#ff0000", "#00ff00", "#0000ff", "#ffff00", "#00ffff")}})
	if got := p.Options.Palette(); len(got) != 3 || pixelfx.ToHex(got[2]) != "#00ffff" || pixelfx.ToHex(got[0]) != "#ff0000" {
		t.Fatalf("a 5-step palette over a 3-step default resamples to %v", got)
	}
	if p.Options.Seed() != 3 {
		t.Fatalf("seed %v, want the default 3", p.Options.Seed())
	}
	if p.Options.Actor != nil {
		t.Fatal("an effect without an actor got one")
	}
}

func TestPlayerStepsAt120HzAndFadesWhenStopped(t *testing.T) {
	var n int
	e := dot(&n)
	e.Meta.Loop = true
	p := pixelfx.NewPlayer(e, pixelfx.Options{X: 4, Y: 4})
	p.Update(1.0 / 30)
	if n != 4 {
		t.Fatalf("a 1/30 s update ran %d steps, want 4", n)
	}
	f := pixelfx.NewFrame(8, 8, 0, 0)
	p.Draw(f, pixelfx.LayerBoth)
	if f.Color[4*8+4] != pixelfx.White {
		t.Fatal("the dot is not at the origin")
	}
	p.Stop()
	p.Update(0.31)
	if !p.Done() {
		t.Fatal("a stopped loop is not done after fading")
	}
}

func TestRegistry(t *testing.T) {
	var n int
	e := dot(&n)
	pixelfx.Register("test/dot", e)
	if got, ok := pixelfx.Lookup("test/dot"); !ok || got != e || e.ID != "test/dot" {
		t.Fatalf("Lookup gives %v, %v", got, ok)
	}
	if !slices.IsSorted(pixelfx.IDs()) || !slices.Contains(pixelfx.IDs(), "test/dot") {
		t.Fatalf("IDs %v", pixelfx.IDs())
	}
	for _, bad := range []string{"test/dot", "nocategory", "a/b/c", "/x"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Register(%q) did not panic", bad)
				}
			}()
			pixelfx.Register(bad, dot(&n))
		}()
	}
}

func TestValuesRoundTripThroughText(t *testing.T) {
	for _, v := range []pixelfx.Value{pixelfx.Num(-1.25), pixelfx.Bool(true), pixelfx.Color("#a1b2c3"), pixelfx.Palette("#000000", "#ffffff"), pixelfx.Text("LEVEL UP"), pixelfx.Nums(1, 2.5)} {
		got, err := pixelfx.ParseValue(v.Kind(), v.String())
		if err != nil || got.String() != v.String() || got.Kind() != v.Kind() {
			t.Errorf("%s %q reads back as %v (%v)", v.Kind(), v.String(), got, err)
		}
	}
	for _, bad := range []struct {
		k pixelfx.ValueKind
		s string
	}{{pixelfx.KindNumber, "fast"}, {pixelfx.KindNumber, "NaN"}, {pixelfx.KindNumber, "4abc"}, {pixelfx.KindNumber, "1 2"}, {pixelfx.KindNumber, "1,2"}, {pixelfx.KindNumber, "1e999"}, {pixelfx.KindNumbers, "1,2x"}, {pixelfx.KindColor, "fff"}, {pixelfx.KindBool, "yes"}, {pixelfx.KindRamp, "#000,#zz"}, {pixelfx.KindNone, "x"}} {
		if _, err := pixelfx.ParseValue(bad.k, bad.s); err == nil {
			t.Errorf("ParseValue(%s, %q) succeeded", bad.k, bad.s)
		}
	}
}

func FuzzParseValue(f *testing.F) {
	for _, s := range []string{"1", "-0.5", "true", "#fff", "#000000,#ffffff", "1,2,3", "", ",", "#"} {
		f.Add(uint8(pixelfx.KindNumber), s)
		f.Add(uint8(pixelfx.KindRamp), s)
	}
	f.Fuzz(func(t *testing.T, kind uint8, s string) {
		v, err := pixelfx.ParseValue(pixelfx.ValueKind(kind%7), s)
		if err != nil {
			return
		}
		if v.Kind() == pixelfx.KindNumber {
			x, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil {
				t.Fatalf("%q read as the number %s, but is not one: %v", s, v.String(), err)
			}
			if pixelfx.Num(x).String() != v.String() {
				t.Fatalf("%q read as %s, not %v", s, v.String(), x)
			}
		}
		again, err := pixelfx.ParseValue(v.Kind(), v.String())
		if err != nil {
			t.Fatalf("%q read as %s %q, which does not read back: %v", s, v.Kind(), v.String(), err)
		}
		if again.String() != v.String() {
			t.Fatalf("%q reads back as %q, not %q", s, again.String(), v.String())
		}
	})
}

func TestChainPlaysCuesAtTheirTimes(t *testing.T) {
	var n int
	child := dot(&n)
	e := &pixelfx.Effect{
		Meta: pixelfx.Meta{Size: [2]int{40, 8}, Origin: [2]int{4, 4}, Duration: 1, FPS: 10},
		New:  pixelfx.Chain([]pixelfx.Cue{{FX: child, T: 0.5, XFn: func(t float64) float64 { return t * 20 }}}),
	}
	p := pixelfx.NewPlayer(e, pixelfx.Options{X: 4, Y: 4})
	f := pixelfx.NewFrame(40, 8, 0, 0)
	draw := func() int {
		f.Clear()
		p.Draw(f, pixelfx.LayerBoth)
		for i, c := range f.Color {
			if c != 0 {
				return i % 40
			}
		}
		return -1
	}
	p.Update(0.4)
	if x := draw(); x != -1 {
		t.Fatalf("the cue drew at %d before its time", x)
	}
	p.Update(0.2)
	if x := draw(); x != 16 {
		t.Fatalf("at 0.6 s the cue is at x %d, want 4 + 0.6 * 20 = 16", x)
	}
	p2 := pixelfx.NewPlayer(e, pixelfx.Options{X: 30, Y: 4, Flip: true})
	p2.Update(0.6)
	f.Clear()
	p2.Draw(f, pixelfx.LayerBoth)
	if f.Color[4*40+30-12-1] == 0 && f.Color[4*40+30-12] == 0 {
		t.Fatal("a flipped chain does not mirror its cues' x")
	}
}

func TestShiftKeysAndHits(t *testing.T) {
	keys := pixelfx.ShiftKeys([]pixelfx.Key{{T: 0.1, Pose: pixelfx.Ready.With(pixelfx.X.Is(2))}}, 1, 5, pixelfx.Pose{}.With(pixelfx.Ghost.Is(1)))
	if keys[0].T != 1.1 || keys[0].Pose.Or(pixelfx.X, 0) != 7 || !keys[0].Pose.Has(pixelfx.Ghost) {
		t.Fatalf("shifted key %+v", keys[0])
	}
	if hits := pixelfx.ShiftHits([]pixelfx.Hit{{T: 0.2}}, 0.5); hits[0].T != 0.7 {
		t.Fatalf("shifted hit at %v", hits[0].T)
	}
}

func TestOptionsKeepDefaultsForValuesTheyCannotUse(t *testing.T) {
	var n int
	for _, c := range []struct {
		name string
		v    pixelfx.Value
	}{{"empty ramp", pixelfx.RampOf(nil)}, {"empty palette", pixelfx.Palette()}, {"text for a ramp", pixelfx.Text("x")}, {"number for a ramp", pixelfx.Num(2)}} {
		p := pixelfx.NewPlayer(dot(&n), pixelfx.Options{}.Set("palette", c.v))
		if got := p.Options.Palette(); len(got) != 3 || pixelfx.ToHex(got[2]) != "#ffffff" {
			t.Errorf("%s: palette %v, want the default", c.name, got)
		}
		p.Draw(pixelfx.NewFrame(8, 8, 0, 0), pixelfx.LayerBoth)
	}
	p := pixelfx.NewPlayer(dot(&n), pixelfx.Options{}.Set("seed", pixelfx.Text("x")))
	if got := p.Options.Seed(); got != 3 {
		t.Errorf("text for the seed gives seed %v, want the default 3", got)
	}
	p = pixelfx.NewPlayer(dot(&n), pixelfx.Options{}.Set("palette", pixelfx.Color("#ff0000")))
	if got := p.Options.Palette(); len(got) != 3 || pixelfx.ToHex(got[0]) != "#ff0000" || pixelfx.ToHex(got[2]) != "#ff0000" {
		t.Errorf("a color for the palette gives %v, want a ramp of 3 of that color", got)
	}
	p = pixelfx.NewPlayer(dot(&n), pixelfx.Options{}.Set("extra", pixelfx.Text("kept")))
	if got := p.Options.Text("extra"); got != "kept" {
		t.Errorf("an option the meta does not name gives %q, want it kept", got)
	}
}

func TestUpdateIgnoresBadSteps(t *testing.T) {
	var n int
	p := pixelfx.NewPlayer(dot(&n), pixelfx.Options{})
	for _, dt := range []float64{math.NaN(), -1, math.Inf(1), math.Inf(-1)} {
		p.Update(dt)
	}
	if p.T() != 0 || n != 0 {
		t.Fatalf("after bad steps T is %v and %d updates ran, want 0 and 0", p.T(), n)
	}
	p.Update(0.05)
	if p.T() != 0.05 || n != 6 {
		t.Fatalf("after 0.05 s T is %v and %d updates ran, want 0.05 and 6", p.T(), n)
	}
}

func TestValueKindStringNamesUnknownKinds(t *testing.T) {
	if got := pixelfx.KindNumbers.String(); got != "numbers" {
		t.Errorf("KindNumbers is %q", got)
	}
	if got := pixelfx.ValueKind(9).String(); got != "ValueKind(9)" {
		t.Errorf("ValueKind(9) is %q", got)
	}
}

func TestStockActorIsACopy(t *testing.T) {
	a := pixelfx.StockActor(2)
	a.Pixels[0]++
	a.Mask.Data[0]++
	b := pixelfx.StockActor(2)
	if b.Pixels[0] == a.Pixels[0] || b.Mask.Data[0] == a.Mask.Data[0] {
		t.Fatal("changing one stock actor changed the next")
	}
}
