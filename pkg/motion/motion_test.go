package motion_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/motion"
)

func almostEqual(a, b, eps float64) bool {
	return math.Abs(a-b) <= eps
}

func stepRate(steps int, total time.Duration) time.Duration {
	return total / time.Duration(steps)
}

func TestBulletRateIndependence(t *testing.T) {
	t.Run("constant velocity", func(t *testing.T) {
		run := func(hz int) (float64, float64) {
			b := motion.NewBullet(240, 0, 0, 30, true, false)
			x, y := 10.0, 20.0
			dt := time.Second / time.Duration(hz)
			for range hz {
				res := b.Step(x, y, dt, nil)
				x, y = res.X, res.Y
			}
			return x, y
		}
		x60, y60 := run(60)
		x120, y120 := run(120)
		if !almostEqual(x60, x120, 1e-6) || !almostEqual(y60, y120, 1e-6) {
			t.Fatalf("60Hz=(%v,%v) vs 120Hz=(%v,%v)", x60, y60, x120, y120)
		}
	})

	t.Run("linear acceleration", func(t *testing.T) {
		run := func(hz int) (float64, float64, float64) {
			b := motion.NewBullet(100, 80, 0, 0, true, false)
			x, y := 0.0, 0.0
			dt := time.Second / time.Duration(hz)
			for range hz {
				res := b.Step(x, y, dt, nil)
				x, y = res.X, res.Y
			}
			return x, y, b.Speed
		}
		x60, y60, v60 := run(60)
		x120, y120, v120 := run(120)
		// Exact closed form: x(1) = 100*1 + 0.5*80*1^2 = 140, v(1) = 180.
		if !almostEqual(x60, 140, 1e-5) || !almostEqual(x120, 140, 1e-5) {
			t.Fatalf("x60=%v x120=%v want 140", x60, x120)
		}
		if !almostEqual(y60, 0, 1e-9) || !almostEqual(y120, 0, 1e-9) {
			t.Fatalf("y60=%v y120=%v want 0", y60, y120)
		}
		if !almostEqual(v60, 180, 1e-5) || !almostEqual(v120, 180, 1e-5) {
			t.Fatalf("v60=%v v120=%v want 180", v60, v120)
		}
	})

	t.Run("gravity parabola", func(t *testing.T) {
		run := func(hz int) (float64, float64) {
			b := motion.NewBullet(100, 0, 200, 0, true, false)
			x, y := 0.0, 0.0
			dt := time.Second / time.Duration(hz)
			for range hz {
				res := b.Step(x, y, dt, nil)
				x, y = res.X, res.Y
			}
			return x, y
		}
		x60, y60 := run(60)
		x120, y120 := run(120)
		// Exact: x(1) = 100, y(1) = 0.5 * 200 * 1^2 = 100.
		if !almostEqual(x60, 100, 1e-5) || !almostEqual(x120, 100, 1e-5) {
			t.Fatalf("x60=%v x120=%v want 100", x60, x120)
		}
		if !almostEqual(y60, 100, 1e-5) || !almostEqual(y120, 100, 1e-5) {
			t.Fatalf("y60=%v y120=%v want 100", y60, y120)
		}
	})
}

func TestBulletBounceGoldenValues(t *testing.T) {
	// Vertical wall at x = 100 with outward normal (-1, 0).
	wallCast := func(x, y, dx, dy float64) (motion.SolidHit, bool) {
		if dx > 0 && x <= 100 && x+dx >= 100 {
			return motion.SolidHit{Fraction: (100 - x) / dx, NormalX: -1, NormalY: 0}, true
		}
		return motion.SolidHit{}, false
	}

	run := func(hz int) (float64, float64, float64, int) {
		b := motion.NewBullet(100, 0, 0, 0, true, true)
		x, y := 50.0, 25.0
		bounces := 0
		var lastRot float64
		dt := time.Second / time.Duration(hz)
		for range hz {
			res := b.Step(x, y, dt, wallCast)
			x, y = res.X, res.Y
			if res.Bounced {
				bounces += res.Bounces
			}
			if res.SetRot {
				lastRot = res.Rotation
			}
		}
		return x, y, lastRot, bounces
	}

	x60, y60, rot60, b60 := run(60)
	x120, y120, rot120, b120 := run(120)
	// Hits wall at x=100 at t=0.5s, bounces back to x=50 at t=1.0s.
	if b60 != 1 || b120 != 1 {
		t.Fatalf("bounces 60Hz=%d 120Hz=%d want 1", b60, b120)
	}
	if !almostEqual(x60, 50, 1e-3) || !almostEqual(x120, 50, 1e-3) {
		t.Fatalf("x60=%v x120=%v want ~50", x60, x120)
	}
	if !almostEqual(x60, x120, 1e-3) || !almostEqual(y60, 25, 1e-6) || !almostEqual(y120, 25, 1e-6) {
		t.Fatalf("60Hz=(%v,%v) 120Hz=(%v,%v)", x60, y60, x120, y120)
	}
	if !almostEqual(math.Abs(rot60), math.Pi, 1e-6) || !almostEqual(math.Abs(rot120), math.Pi, 1e-6) {
		t.Fatalf("rot60=%v rot120=%v want +-pi", rot60, rot120)
	}

	// 45-degree bounce off horizontal floor at y = 100 (normal (0, -1)):
	// Moving at +45 deg (vx = v/sqrt2, vy = v/sqrt2), after bounce heading is -45 deg.
	floorCast := func(x, y, dx, dy float64) (motion.SolidHit, bool) {
		if dy > 0 && y <= 100 && y+dy >= 100 {
			return motion.SolidHit{Fraction: (100 - y) / dy, NormalX: 0, NormalY: -1}, true
		}
		return motion.SolidHit{}, false
	}
	b := motion.NewBullet(100*math.Sqrt2, 0, 0, 45, true, true)
	res := b.Step(0, 50, time.Second, floorCast)
	if !res.Bounced {
		t.Fatal("expected bounce off floor")
	}
	// Hits (50, 100) at t=0.5s, ends at (100, 50) at t=1.0s with angle -45 deg.
	if !almostEqual(res.X, 100, 1e-3) || !almostEqual(res.Y, 50, 1e-3) {
		t.Fatalf("45-deg bounce pos=(%v,%v) want (100,50)", res.X, res.Y)
	}
	if !almostEqual(b.Angle, -45, 1e-5) {
		t.Fatalf("45-deg bounce angle=%v want -45", b.Angle)
	}
}

func TestSineGoldenValuesAndRateIndependence(t *testing.T) {
	// Golden values across waveforms at u = 0, 0.25, 0.5, 0.75.
	cases := []struct {
		wave motion.WaveKind
		u    float64
		want float64
	}{
		{motion.WaveSine, 0.0, 0},
		{motion.WaveSine, 0.25, 1},
		{motion.WaveSine, 0.5, 0},
		{motion.WaveSine, 0.75, -1},
		{motion.WaveTriangle, 0.0, 0},
		{motion.WaveTriangle, 0.25, 1},
		{motion.WaveTriangle, 0.5, 0},
		{motion.WaveTriangle, 0.75, -1},
		{motion.WaveSawtooth, 0.0, -1},
		{motion.WaveSawtooth, 0.25, -0.5},
		{motion.WaveSawtooth, 0.5, 0},
		{motion.WaveSawtooth, 0.75, 0.5},
		{motion.WaveReverseSawtooth, 0.0, 1},
		{motion.WaveReverseSawtooth, 0.25, 0.5},
		{motion.WaveReverseSawtooth, 0.5, 0},
		{motion.WaveReverseSawtooth, 0.75, -0.5},
		{motion.WaveSquare, 0.0, 1},
		{motion.WaveSquare, 0.25, 1},
		{motion.WaveSquare, 0.5, -1},
		{motion.WaveSquare, 0.75, -1},
	}
	for _, tc := range cases {
		got := motion.EvalWave(tc.wave, tc.u)
		if !almostEqual(got, tc.want, 1e-6) {
			t.Errorf("EvalWave(%q, %v) = %v, want %v", tc.wave, tc.u, got, tc.want)
		}
	}

	// Rate independence: 60 Hz vs 120 Hz over 1.0 s with period 1.5 s, magnitude 6.
	run := func(hz int) float64 {
		s := motion.NewSine(1.5, 6, motion.WaveSine, 0)
		y := 100.0
		dt := time.Second / time.Duration(hz)
		for range hz {
			y += s.Step(dt)
		}
		return y
	}
	y60 := run(60)
	y120 := run(120)
	want := 100.0 + 6.0*math.Sin(2*math.Pi*(1.0/1.5))
	if !almostEqual(y60, want, 1e-6) || !almostEqual(y120, want, 1e-6) {
		t.Fatalf("y60=%v y120=%v want=%v", y60, y120, want)
	}
}

func TestRotateRateIndependence(t *testing.T) {
	run := func(hz int) (float64, float64) {
		r := motion.NewRotate(90, 60)
		rot := 0.0
		dt := time.Second / time.Duration(hz)
		for range hz {
			rot += r.Step(dt)
		}
		return rot, r.Speed
	}
	rot60, spd60 := run(60)
	rot120, spd120 := run(120)
	// Exact: dDeg = 90*1 + 0.5*60*1^2 = 120 deg; final speed = 150 deg/s.
	wantRad := 120 * math.Pi / 180
	if !almostEqual(rot60, wantRad, 1e-6) || !almostEqual(rot120, wantRad, 1e-6) {
		t.Fatalf("rot60=%v rot120=%v want=%v", rot60, rot120, wantRad)
	}
	if !almostEqual(spd60, 150, 1e-5) || !almostEqual(spd120, 150, 1e-5) {
		t.Fatalf("spd60=%v spd120=%v want 150", spd60, spd120)
	}
}

func TestOrbitRateIndependence(t *testing.T) {
	run := func(hz int) (float64, float64) {
		o := motion.NewOrbit(32, 90, 0, true)
		var res motion.OrbitStepResult
		dt := time.Second / time.Duration(hz)
		for range hz {
			res = o.Step(100, 50, dt)
		}
		return res.X, res.Y
	}
	x60, y60 := run(60)
	x120, y120 := run(120)
	// After 1s at 90 deg/s, angle = 90 deg => (100, 50 + 32).
	if !almostEqual(x60, 100, 1e-5) || !almostEqual(y60, 82, 1e-5) {
		t.Fatalf("60Hz=(%v,%v) want (100,82)", x60, y60)
	}
	if !almostEqual(x60, x120, 1e-6) || !almostEqual(y60, y120, 1e-6) {
		t.Fatalf("60Hz=(%v,%v) vs 120Hz=(%v,%v)", x60, y60, x120, y120)
	}
}

func TestMoveToRateIndependenceAndSolids(t *testing.T) {
	run := func(hz int) (float64, float64, bool) {
		m := motion.NewMoveTo(120, 240, false)
		m.SetTarget(200, 0)
		x, y := 0.0, 0.0
		arrived := false
		dt := time.Second / time.Duration(hz)
		for range hz {
			res := m.Step(x, y, dt, nil)
			x, y = res.X, res.Y
			if res.Arrived {
				arrived = true
			}
		}
		return x, y, arrived
	}
	// Ramp from 0 to 120 at 240 px/s^2 takes 0.5s covering 30px, then 0.5s at 120 covers 60px => 90px at t=1s.
	x60, y60, a60 := run(60)
	x120, y120, a120 := run(120)
	if a60 || a120 {
		t.Fatal("should not have arrived at t=1s")
	}
	if !almostEqual(x60, 90, 1e-5) || !almostEqual(x120, 90, 1e-5) || !almostEqual(y60, 0, 1e-9) || !almostEqual(y120, 0, 1e-9) {
		t.Fatalf("60Hz=(%v,%v) 120Hz=(%v,%v) want (90,0)", x60, y60, x120, y120)
	}

	// Stop on solid wall at x=50.
	wall := func(x, y, dx, dy float64) (motion.SolidHit, bool) {
		if dx > 0 && x <= 50 && x+dx >= 50 {
			return motion.SolidHit{Fraction: (50 - x) / dx, NormalX: -1}, true
		}
		return motion.SolidHit{}, false
	}
	m := motion.NewMoveTo(100, 0, true)
	m.SetTarget(200, 0)
	res := m.Step(0, 0, time.Second, wall)
	if !res.Blocked || res.Arrived || !almostEqual(res.X, 50, 1e-6) || m.Active {
		t.Fatalf("StopOnSolids res=%+v active=%v", res, m.Active)
	}
}

func TestWrapBoundDestroyOutside(t *testing.T) {
	bounds := motion.Bounds{MinX: 0, MinY: 0, MaxX: 320, MaxY: 180}

	w := motion.NewWrap(0)
	nx, ny, wrapped := w.Apply(330, -15, bounds)
	if !wrapped || !almostEqual(nx, 10, 1e-9) || !almostEqual(ny, 165, 1e-9) {
		t.Fatalf("Wrap = (%v,%v,%v) want (10,165,true)", nx, ny, wrapped)
	}

	bEdge := motion.NewBound(true)
	bx, by := bEdge.Apply(5, 175, bounds, 10, 10, 10, 10)
	if !almostEqual(bx, 10, 1e-9) || !almostEqual(by, 170, 1e-9) {
		t.Fatalf("Bound(edge) = (%v,%v) want (10,170)", bx, by)
	}
	bOrigin := motion.NewBound(false)
	bx, by = bOrigin.Apply(-5, 200, bounds, 10, 10, 10, 10)
	if !almostEqual(bx, 0, 1e-9) || !almostEqual(by, 180, 1e-9) {
		t.Fatalf("Bound(origin) = (%v,%v) want (0,180)", bx, by)
	}

	d := motion.NewDestroyOutside(64)
	if d.Outside(380, 90, bounds) {
		t.Fatal("expected inside margin")
	}
	if !d.Outside(385, 90, bounds) {
		t.Fatal("expected outside margin")
	}
}

func TestFadeAndFlashRateIndependence(t *testing.T) {
	runFade := func(hz int) (float64, bool) {
		f := motion.NewFade(0.5, 0.5, 1.0, true, true)
		var res motion.FadeStepResult
		dt := time.Second / time.Duration(hz)
		// Run for 1.5s => halfway through fadeOut (alpha = 0.5).
		for range (hz * 3) / 2 {
			res = f.Step(dt)
		}
		return res.Alpha, res.Finished
	}
	a60, fin60 := runFade(60)
	a120, fin120 := runFade(120)
	if fin60 || fin120 || !almostEqual(a60, 0.5, 1e-5) || !almostEqual(a120, 0.5, 1e-5) {
		t.Fatalf("Fade at 1.5s: 60Hz=(%v,%v) 120Hz=(%v,%v) want (0.5,false)", a60, fin60, a120, fin120)
	}

	// Flash: on=0.05, off=0.05, duration=0.25
	runFlash := func(hz int) (bool, bool) {
		fl := motion.NewFlash(0.05, 0.05)
		fl.Trigger(0.25)
		dt := stepRate(hz, time.Second)
		var res motion.FlashStepResult
		// Step 0.075s => in the 'on' half of first cycle (0.05..0.10) => visible.
		steps := (hz * 3) / 40
		for range steps {
			res = fl.Step(dt)
		}
		return res.Visible, res.Active
	}
	v40, act40 := runFlash(40)
	v80, act80 := runFlash(80)
	if !v40 || !act40 || !v80 || !act80 {
		t.Fatalf("Flash at 0.075s: 40Hz=(%v,%v) 80Hz=(%v,%v) want (true,true)", v40, act40, v80, act80)
	}
}

func TestZeroAllocHotPaths(t *testing.T) {
	b := motion.NewBullet(240, 20, 0, 45, true, false)
	s := motion.NewSine(1.5, 6, motion.WaveSine, 0)
	r := motion.NewRotate(180, 10)
	o := motion.NewOrbit(32, 90, 0, true)
	m := motion.NewMoveTo(120, 60, false)
	m.SetTarget(10000, 10000)
	w := motion.NewWrap(0)
	bd := motion.NewBound(true)
	do := motion.NewDestroyOutside(64)
	fa := motion.NewFade(0.5, 10, 0.5, true, true)
	fl := motion.NewFlash(0.05, 0.05)
	fl.Trigger(10)
	bounds := motion.Bounds{MinX: 0, MinY: 0, MaxX: 320, MaxY: 180}
	dt := time.Second / 60

	allocs := testing.AllocsPerRun(100, func() {
		res := b.Step(10, 10, dt, nil)
		_ = s.Step(dt)
		_ = r.Step(dt)
		_ = o.Step(100, 100, dt)
		_ = m.Step(res.X, res.Y, dt, nil)
		_, _, _ = w.Apply(res.X, res.Y, bounds)
		_, _ = bd.Apply(res.X, res.Y, bounds, 8, 8, 8, 8)
		_ = do.Outside(res.X, res.Y, bounds)
		_ = fa.Step(dt)
		_ = fl.Step(dt)
	})
	if allocs > 0 {
		t.Fatalf("motion hot path allocated %v times per step, want 0", allocs)
	}
}
