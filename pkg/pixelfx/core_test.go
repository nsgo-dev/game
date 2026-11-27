package pixelfx_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/internal/pixelfxtest"
)

// drawShapes and makeSim draw what tools/pixelfxref draws with pixelfx's core into testdata/ref/core: keep them in step.

func drawShapes(p *pixelfx.Painter) {
	R, W := pixelfx.Ramps, pixelfx.White
	F, I := R["fire"], R["ice"]
	pick := func(cond bool, a, b uint32) uint32 {
		if cond {
			return a
		}
		return b
	}
	p.Circle(-50, -30, 9, F[5], 1)
	p.CircleFn(-50, -30, 6, func(d, _ float64) (uint32, float64) { return pick(d < 0.5, W, F[3]), 1 - d*0.5 })
	p.Ellipse(-20, -32, 12, 5, I[4], 0.8)
	p.RingFn(10, -30, 10, 3, func(v, _ float64) (uint32, float64) { return pick(v > 0.5, I[6], I[3]), 1 })
	p.ArcFn(40, -28, 14, 4, -2.5, 0.5, func(u, _ float64) (uint32, float64) { return pick(u < 0.5, F[6], F[4]), 1 - u*0.6 })
	p.ArcTaperFn(40, 0, 12, func(u float64) float64 { return 4 * math.Sin(u*math.Pi) }, 0, 3, func(float64, float64) (uint32, float64) { return F[7], 1 })
	p.BeamFn(-60, 10, -20, 30, 5, func(u, v float64) (uint32, float64) { return pick(v < 0.4, W, I[5]), 1 - u })
	p.Poly(pixelfx.Star(0, 15, 5, 12, 5, -math.Pi/2), R["gold"][5], 1)
	p.Tri(20, 30, 35, 10, 45, 35, R["poison"][4], 0.6)
	p.Rect(-70, 35, 20, 6, R["stone"][4], 0.9)
	p.LineFn(-75, -45, 70, 40, func(u float64) (uint32, float64) { return pick(u < 0.5, pixelfx.Patch, pixelfx.Ink), 1 - u*0.7 })
	p.Polyline(pixelfx.Ngon(60, 25, 8, 6, 0.2), W, 1)
	p.DiscFn(-30, 5, 7, func(d, _ float64) (uint32, float64) {
		return pick(d < 0.5, R["smoke"][5], R["smoke"][3]), 0.8 * (1 - d)
	})
	p.Glow(-50, -30, 14, F[5], 0.6)
	p.GlowLine(-60, 10, -20, 30, 6, I[5], 0.4)
	p.GlowRing(10, -30, 10, 4, I[6], 0.5)
	p.Sprite([]string{".##.", "#oo#", "#oo#", ".##."}, map[rune]uint32{'#': pixelfx.Hex("#222034"), 'o': pixelfx.Hex("#f53cd1")}, 50, -50, 0.7, pixelfx.SpriteOpts{Scale: 2, Flip: true})
	p.Text("PIXEL 42!", 0, 40, F[6], 1, pixelfx.TextOpts{Font: "big", Align: pixelfx.AlignCenter, Outline: pixelfx.Ink, Shadow: R["night"][3]})
	A := pixelfx.StockActor(1)
	p.MaskFn(A.Mask, -10, -10, func(mx, my int) (uint32, float64) { return pick((mx+my)%3 != 0, I[5], I[2]), 0.85 })
	p.Outline(A.Mask, -10, -10, W, 1)
	pixelfx.Sparkle(p, 30, -45, 6, R["gold"][5], R["gold"][3], 0.7)
	pixelfx.Flare(p, -40, 45, 9, 0.5, R["arcane"])
	pixelfx.Band(p, 0, 50, 30, 0.16, 3, 1, R["ice"], func(g float64) bool { return math.Sin(g) >= 0 }, true)
	pixelfx.Crystal(p, 65, 50, 22, 9, 3, R["ice"], 0.95, false, 0.4)
	p.Blend(5, 5, pixelfx.Patch, 0.4)
	p.Light(6, 6, W, 0.5)
}

type sim struct {
	ps     *pixelfx.Particles
	blazes *pixelfx.Blazes
	bits   *pixelfx.Bits
	lights *pixelfx.Lights
}

func makeSim() *sim {
	RAMP := pixelfx.Ramps
	R := pixelfx.NewRng(11)
	floor := func(float64) float64 { return 30 }
	s := &sim{bits: pixelfx.NewBits(floor), lights: &pixelfx.Lights{}}
	s.ps = pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Haze: pixelfx.Hex("#6a8cff"), OnLand: func(x, y float64) { s.blazes.Add(x, y, 1.2, 0.8) }})
	s.blazes = pixelfx.NewBlazes(s.ps)
	kinds := []pixelfx.Kind{pixelfx.Spark, pixelfx.Mote, pixelfx.Streak, pixelfx.Flake, pixelfx.Vapour, pixelfx.Dust, pixelfx.StarKind, pixelfx.Ember, pixelfx.Smoke, pixelfx.Fire, pixelfx.Brand, pixelfx.Grit}
	for i := range 96 {
		k, a, sp := kinds[i%len(kinds)], R.Range(0, math.Pi*2), R.Range(20, 160)
		var col uint32
		if i%5 != 0 {
			col = RAMP["ice"][(i*3)%8]
		}
		x, y := R.Range(-20, 20), R.Range(-10, 10)
		life := R.Range(0.4, 1.6)
		size := R.Range(0.5, 3)
		delay := 0.0
		if i%7 == 0 {
			delay = R.Range(0, 0.3)
		}
		var flag pixelfx.Flag
		switch {
		case i%4 == 0:
			flag = pixelfx.Smokes | pixelfx.Big
		case i%6 == 0:
			flag = pixelfx.Back
		}
		s.ps.Spawn(k, x, y, math.Cos(a)*sp, math.Sin(a)*sp-40, life, col, size, delay, flag)
	}
	for i := range 18 {
		var b pixelfx.Bit
		b.Kind = pixelfx.BitKind(i % 3)
		b.X = R.Range(-30, 30)
		b.Y = R.Range(-20, 0)
		b.VX = R.Range(-80, 80)
		b.VY = R.Range(-200, -40)
		b.A = R.Range(0, 6)
		b.VA = R.Range(-10, 10)
		b.S = R.Int(1, 4)
		b.C, b.G, b.Drag = RAMP["stone"][5], 0.8, 0.3
		if i%2 != 0 {
			b.Bounce = 0.4
		}
		b.Life = R.Range(0.8, 2)
		s.bits.Add(b)
	}
	s.lights.Add(0, 0, 40, RAMP["fire"][5], 0.7, 1.2)
	s.blazes.Add(-25, 30, 1.5, 0.9)
	return s
}

func (s *sim) update(dt float64) {
	s.bits.Update(dt)
	s.blazes.Update(dt)
	s.ps.Update(dt)
	s.lights.Update(dt)
}

func (s *sim) draw(p *pixelfx.Painter) {
	s.blazes.DrawBack(p)
	s.ps.Draw(p, pixelfx.LayerBack)
	s.bits.Draw(p)
	s.ps.Draw(p, pixelfx.LayerFront)
	s.lights.Draw(p)
}

func checkCore(t *testing.T, f *pixelfx.Frame, name string) {
	t.Helper()
	w, h, want, err := pixelfxtest.ReadPNG(pixelfxtest.Ref(t, "core", name))
	if err != nil {
		t.Fatal(err)
	}
	if w != f.W || h != f.H {
		t.Fatalf("%s is %dx%d", name, w, h)
	}
	d := pixelfxtest.Compare(pixelfx.Composite(f, nil, nil, 1), want, w, h)
	t.Logf("%s: %d pixels differ, largest difference %d", name, d.Differing, d.MaxDelta)
	if !d.OK() {
		t.Errorf("%s: %d of %d pixels differ from pixelfx's (largest difference %d), first at (%d, %d)", name, d.Differing, d.Total, d.MaxDelta, d.FirstX, d.FirstY)
	}
}

func TestPainterMatchesPixelfx(t *testing.T) {
	f := pixelfx.NewFrame(160, 120, 0, 0)
	for _, c := range []struct {
		name  string
		flip  bool
		angle float64
	}{{"plain", false, 0}, {"flip", true, 0}, {"turned", false, 0.3}} {
		f.Clear()
		drawShapes(f.Painter().Place(80, 60, c.flip, c.angle))
		checkCore(t, f, "shapes-"+c.name+".png")
	}
}

func TestSimulationMatchesPixelfx(t *testing.T) {
	f := pixelfx.NewFrame(160, 120, 0, 0)
	s := makeSim()
	drawn := 0
	for step := 1; step <= 120; step++ {
		s.update(1.0 / 120)
		if step == 30 || step == 72 || step == 120 {
			f.Clear()
			s.draw(f.Painter().Place(80, 60, false, 0))
			checkCore(t, f, fmt.Sprintf("sim-%03d.png", step))
			drawn++
		}
	}
	if drawn != 3 {
		t.Fatalf("checked %d frames", drawn)
	}
}

func TestBackdrops(t *testing.T) {
	for _, kind := range pixelfx.Backdrops {
		bg := pixelfx.Backdrop(kind, 32, 24, 16)
		if len(bg) != 32*24 || bg[0] == 0 {
			t.Errorf("%s backdrop: %d colors, first %#x", kind, len(bg), bg[0])
		}
	}
	if g := pixelfx.Backdrop("ground", 8, 8, 4); g[0] == g[4*8] {
		t.Error("the ground backdrop has no floor edge")
	}
}
