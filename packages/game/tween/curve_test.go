package tween_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/game/packages/game/tween"
	"github.com/nsgo-dev/nsgo/core"
)

func pointDist(a, b core.Point) float64 {
	return math.Hypot(float64(b.X-a.X), float64(b.Y-a.Y))
}

func assertPointNear(t *testing.T, label string, got, want core.Point, tol float64) {
	t.Helper()
	if d := pointDist(got, want); d > tol || math.IsNaN(d) {
		t.Fatalf("%s: got %+v, want %+v (dist=%g > %g)", label, got, want, d, tol)
	}
}

func assertUnitTangent(t *testing.T, label string, tan core.Point, wantX, wantY, tol float64) {
	t.Helper()
	l := math.Hypot(float64(tan.X), float64(tan.Y))
	if !near(l, 1, tol) {
		t.Fatalf("%s: tangent %+v length = %g, want 1", label, tan, l)
	}
	if !near(float64(tan.X), wantX, tol) || !near(float64(tan.Y), wantY, tol) {
		t.Fatalf("%s: tangent = (%g, %g), want (%g, %g)", label, tan.X, tan.Y, wantX, wantY)
	}
}

func TestBezierAnalyticAccuracy(t *testing.T) {
	t.Run("straight line quadratic and cubic", func(t *testing.T) {
		p0 := core.Point{X: 10, Y: 20}
		pEnd := core.Point{X: 40, Y: 60} // dx=30, dy=40, len=50, unit=(0.6, 0.8)

		curves := []tween.Curve{
			tween.QuadraticBezier(p0, core.Point{X: 25, Y: 40}, pEnd),
			tween.CubicBezier(p0, core.Point{X: 20, Y: 33.333332}, core.Point{X: 30, Y: 46.666668}, pEnd),
		}
		for i, c := range curves {
			if c.Closed() {
				t.Fatalf("curve %d: Closed() = true, want false", i)
			}
			if !near(float64(c.Length()), 50, 1e-4) {
				t.Fatalf("curve %d: Length() = %g, want 50", i, c.Length())
			}
			assertPointNear(t, "t=-1", c.Point(-1), p0, 1e-5)
			assertPointNear(t, "t=0", c.Point(0), p0, 1e-5)
			assertPointNear(t, "t=0.5", c.Point(0.5), core.Point{X: 25, Y: 40}, 1e-4)
			assertPointNear(t, "t=1", c.Point(1), pEnd, 1e-5)
			assertPointNear(t, "t=2", c.Point(2), pEnd, 1e-5)
			assertPointNear(t, "t=NaN", c.Point(float32(math.NaN())), p0, 1e-5)

			assertUnitTangent(t, "Tangent(0)", c.Tangent(0), 0.6, 0.8, 1e-4)
			assertUnitTangent(t, "Tangent(0.5)", c.Tangent(0.5), 0.6, 0.8, 1e-4)
			assertUnitTangent(t, "Tangent(1)", c.Tangent(1), 0.6, 0.8, 1e-4)

			assertPointNear(t, "dist=0", c.PointAtDistance(0), p0, 1e-5)
			assertPointNear(t, "dist=25", c.PointAtDistance(25), core.Point{X: 25, Y: 40}, 1e-4)
			assertPointNear(t, "dist=50", c.PointAtDistance(50), pEnd, 1e-5)
			assertPointNear(t, "dist=999", c.PointAtDistance(999), pEnd, 1e-5)
			assertUnitTangent(t, "TangentAtDistance(25)", c.TangentAtDistance(25), 0.6, 0.8, 1e-4)
		}
	})

	t.Run("quadratic parabola exact analytic arc length", func(t *testing.T) {
		// B(t) = (t, 2t(1-t)) => int_0^1 sqrt(1 + (2-4t)^2) dt = sqrt(5)/2 + asinh(2)/4
		c := tween.QuadraticBezier(
			core.Point{X: 0, Y: 0},
			core.Point{X: 0.5, Y: 1},
			core.Point{X: 1, Y: 0},
		)
		wantLen := 0.5*math.Sqrt(5) + 0.25*math.Asinh(2)
		if !near(float64(c.Length()), wantLen, 1e-5) {
			t.Fatalf("parabola Length() = %.7f, want %.7f", c.Length(), wantLen)
		}
		// Vertex at t=0.5 is (0.5, 0.5) with horizontal tangent (1, 0), at exact half arc length.
		assertPointNear(t, "vertex", c.Point(0.5), core.Point{X: 0.5, Y: 0.5}, 1e-6)
		assertUnitTangent(t, "vertex tangent", c.Tangent(0.5), 1, 0, 1e-6)
		assertPointNear(t, "half distance", c.PointAtDistance(c.Length()/2), core.Point{X: 0.5, Y: 0.5}, 1e-5)
	})

	t.Run("quarter circle cubic bezier arc length and tangents", func(t *testing.T) {
		const r = 100.0
		k := r * 4.0 * (math.Sqrt2 - 1.0) / 3.0
		c := tween.CubicBezier(
			core.Point{X: core.Points(r), Y: 0},
			core.Point{X: core.Points(r), Y: core.Points(k)},
			core.Point{X: core.Points(k), Y: core.Points(r)},
			core.Point{X: 0, Y: core.Points(r)},
		)
		wantQuarterCircle := 0.5 * math.Pi * r
		// Standard cubic approximation matches pi*R/2 to within 0.02%.
		relErr := math.Abs(float64(c.Length())-wantQuarterCircle) / wantQuarterCircle
		if relErr > 0.0005 {
			t.Fatalf("quarter-circle Length() = %g, want ~%g (relErr=%g)", c.Length(), wantQuarterCircle, relErr)
		}
		mid := r * math.Sqrt2 / 2
		assertPointNear(t, "midpoint", c.Point(0.5), core.Point{X: core.Points(mid), Y: core.Points(mid)}, 1e-3)
		assertPointNear(t, "half-arc", c.PointAtDistance(c.Length()/2), core.Point{X: core.Points(mid), Y: core.Points(mid)}, 1e-3)

		assertUnitTangent(t, "start tangent", c.Tangent(0), 0, 1, 1e-5)
		assertUnitTangent(t, "mid tangent", c.Tangent(0.5), -math.Sqrt2/2, math.Sqrt2/2, 1e-5)
		assertUnitTangent(t, "end tangent", c.Tangent(1), -1, 0, 1e-5)
	})

	t.Run("degenerate control points stay safe", func(t *testing.T) {
		// Coincident start handles on cubic: p0 == p1 == p2 != p3.
		skew := tween.CubicBezier(
			core.Point{X: 0, Y: 0},
			core.Point{X: 0, Y: 0},
			core.Point{X: 0, Y: 0},
			core.Point{X: 100, Y: 0},
		)
		if !near(float64(skew.Length()), 100, 1e-3) {
			t.Fatalf("degenerate-start cubic Length() = %g, want 100", skew.Length())
		}
		assertUnitTangent(t, "degenerate start tangent", skew.Tangent(0), 1, 0, 1e-5)
		assertPointNear(t, "degenerate start half dist", skew.PointAtDistance(50), core.Point{X: 50, Y: 0}, 1e-3)

		// All coincident points: zero length, zero tangent, no NaN.
		p := core.Point{X: 7, Y: 9}
		for _, c := range []tween.Curve{
			tween.QuadraticBezier(p, p, p),
			tween.CubicBezier(p, p, p, p),
		} {
			if c.Length() != 0 {
				t.Fatalf("zero curve Length() = %g, want 0", c.Length())
			}
			assertPointNear(t, "zero point", c.Point(0.5), p, 1e-6)
			assertPointNear(t, "zero point dist", c.PointAtDistance(10), p, 1e-6)
			if tan := c.Tangent(0.5); tan != (core.Point{}) {
				t.Fatalf("zero curve Tangent() = %+v, want {0 0}", tan)
			}
		}
	})
}

func TestCatmullRomInterpolationAndContinuity(t *testing.T) {
	t.Run("open spline passes through all control points with C1 continuity", func(t *testing.T) {
		pts := []core.Point{
			{X: 0, Y: 0},
			{X: 30, Y: 80},
			{X: 110, Y: 20},
			{X: 200, Y: 100},
			{X: 260, Y: 40},
		}
		orig := append([]core.Point(nil), pts...)
		c := tween.NewCatmullRom(pts, false)
		// Mutating caller slice must not affect curve.
		pts[0] = core.Point{X: 9999, Y: 9999}

		if c.Closed() {
			t.Fatal("open CatmullRom Closed() = true, want false")
		}
		if c.Length() <= 0 {
			t.Fatalf("Length() = %g, want > 0", c.Length())
		}

		segs := float32(len(orig) - 1)
		for i, want := range orig {
			tt := float32(i) / segs
			assertPointNear(t, "control point", c.Point(tt), want, 1e-4)
		}

		// Verify C1 unit-tangent continuity across every interior control point.
		const eps = float32(1e-4)
		for i := 1; i < len(orig)-1; i++ {
			tt := float32(i) / segs
			left := c.Tangent(tt - eps)
			right := c.Tangent(tt + eps)
			if d := pointDist(left, right); d > 1e-2 {
				t.Fatalf("joint %d tangent discontinuity: left=%+v right=%+v (diff=%g)", i, left, right, d)
			}
		}
	})

	t.Run("closed spline loops smoothly through all control points", func(t *testing.T) {
		pts := []core.Point{
			{X: 0, Y: 0},
			{X: 100, Y: 0},
			{X: 100, Y: 100},
			{X: 0, Y: 100},
		}
		c := tween.NewCatmullRom(pts, true)
		if !c.Closed() {
			t.Fatal("closed CatmullRom Closed() = false, want true")
		}
		// Closed smooth loop around a 100x100 square has perimeter > 400.
		if c.Length() <= 400 {
			t.Fatalf("closed square CatmullRom Length() = %g, want > 400", c.Length())
		}
		for i, want := range pts {
			tt := float32(i) / float32(len(pts))
			assertPointNear(t, "closed control point", c.Point(tt), want, 1e-4)
		}
		assertPointNear(t, "closed wrap t=1", c.Point(1), pts[0], 1e-4)
		assertPointNear(t, "closed wrap dist=L", c.PointAtDistance(c.Length()), pts[0], 1e-3)

		// Seam tangent continuity across t=0 and t=1.
		tStart := c.Tangent(0)
		tEnd := c.Tangent(1)
		if d := pointDist(tStart, tEnd); d > 1e-3 {
			t.Fatalf("closed seam tangent mismatch: start=%+v end=%+v", tStart, tEnd)
		}
	})

	t.Run("collinear points stay on line and alpha variants work", func(t *testing.T) {
		pts := []core.Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 60, Y: 0}, {X: 100, Y: 0}}
		for _, alpha := range []float32{0, 0.5, 1} {
			c := tween.CatmullRom(pts, false, alpha)
			if !near(float64(c.Length()), 100, 1e-3) {
				t.Fatalf("alpha=%g collinear Length() = %g, want 100", alpha, c.Length())
			}
			for i := 0; i <= 20; i++ {
				p := c.Point(float32(i) / 20)
				if !near(float64(p.Y), 0, 1e-4) {
					t.Fatalf("alpha=%g drifted off line at step %d: %+v", alpha, i, p)
				}
			}
		}
	})

	t.Run("edge cases empty single two and duplicate points", func(t *testing.T) {
		empty := tween.NewCatmullRom(nil, false)
		if empty.Length() != 0 || empty.Point(0.5) != (core.Point{}) || empty.Tangent(0.5) != (core.Point{}) {
			t.Fatalf("empty CatmullRom unexpected state")
		}

		one := tween.NewCatmullRom([]core.Point{{X: 5, Y: 6}}, false)
		if one.Length() != 0 || one.Point(0.5) != (core.Point{X: 5, Y: 6}) {
			t.Fatalf("single-point CatmullRom unexpected state")
		}

		two := tween.NewCatmullRom([]core.Point{{X: 0, Y: 0}, {X: 30, Y: 40}}, false)
		if !near(float64(two.Length()), 50, 1e-4) {
			t.Fatalf("two-point CatmullRom Length() = %g, want 50", two.Length())
		}
		assertUnitTangent(t, "two-point tangent", two.Tangent(0.5), 0.6, 0.8, 1e-4)

		dup := tween.NewCatmullRom([]core.Point{
			{X: 0, Y: 0},
			{X: 0, Y: 0},
			{X: 40, Y: 0},
			{X: 40, Y: 0},
			{X: 40, Y: 30},
		}, false)
		if dup.Length() <= 50 {
			t.Fatalf("duplicate-filtered CatmullRom Length() = %g, want > 50", dup.Length())
		}
		if math.IsNaN(float64(dup.Point(0.5).X)) || math.IsNaN(float64(dup.Tangent(0.5).X)) {
			t.Fatal("duplicate-filtered CatmullRom produced NaN")
		}
	})
}

func TestPolylineExactArcLength(t *testing.T) {
	open := tween.Polyline([]core.Point{
		{X: 0, Y: 0},
		{X: 30, Y: 0},
		{X: 30, Y: 40},
	}, false)
	if open.Closed() {
		t.Fatal("open Polyline Closed() = true, want false")
	}
	if !near(float64(open.Length()), 70, 1e-5) {
		t.Fatalf("open Polyline Length() = %g, want 70", open.Length())
	}
	assertPointNear(t, "dist=15", open.PointAtDistance(15), core.Point{X: 15, Y: 0}, 1e-5)
	assertUnitTangent(t, "tan dist=15", open.TangentAtDistance(15), 1, 0, 1e-5)
	assertPointNear(t, "dist=30", open.PointAtDistance(30), core.Point{X: 30, Y: 0}, 1e-5)
	assertPointNear(t, "dist=50", open.PointAtDistance(50), core.Point{X: 30, Y: 20}, 1e-5)
	assertUnitTangent(t, "tan dist=50", open.TangentAtDistance(50), 0, 1, 1e-5)
	assertPointNear(t, "dist=70", open.PointAtDistance(70), core.Point{X: 30, Y: 40}, 1e-5)

	// Closed 3-4-5 right triangle: perimeter = 30 + 40 + 50 = 120.
	tri := tween.Polyline([]core.Point{
		{X: 0, Y: 0},
		{X: 30, Y: 0},
		{X: 30, Y: 40},
	}, true)
	if !tri.Closed() {
		t.Fatal("closed Polyline Closed() = false, want true")
	}
	if !near(float64(tri.Length()), 120, 1e-5) {
		t.Fatalf("closed triangle Length() = %g, want 120", tri.Length())
	}
	assertPointNear(t, "closing midpoint", tri.PointAtDistance(95), core.Point{X: 15, Y: 20}, 1e-5)
	assertUnitTangent(t, "closing tangent", tri.TangentAtDistance(95), -0.6, -0.8, 1e-5)
	assertPointNear(t, "closed end", tri.Point(1), core.Point{X: 0, Y: 0}, 1e-5)

	// Degenerate zero-length segments inside a valid polyline still produce unit tangents.
	withDup := tween.Polyline([]core.Point{
		{X: 0, Y: 0},
		{X: 0, Y: 0},
		{X: 20, Y: 0},
	}, false)
	if !near(float64(withDup.Length()), 20, 1e-5) {
		t.Fatalf("withDup Length() = %g, want 20", withDup.Length())
	}
	assertUnitTangent(t, "withDup tangent(0)", withDup.Tangent(0), 1, 0, 1e-5)
}

func TestPathFollowerConstantSpeedOnSkewedCubicBezier(t *testing.T) {
	// Heavily skewed cubic Bézier: control handles bunched tightly near p0 so parametric speed |B'(t)|
	// varies by more than 100x across t in [0, 1].
	curve := tween.CubicBezier(
		core.Point{X: 0, Y: 0},
		core.Point{X: 1, Y: 0},
		core.Point{X: 3, Y: 2},
		core.Point{X: 400, Y: 300},
	)

	// Confirm the raw parametric curve IS heavily non-uniform (sanity check on the test setup).
	rawStepStart := pointDist(curve.Point(0), curve.Point(0.05))
	rawStepEnd := pointDist(curve.Point(0.95), curve.Point(1.0))
	if rawStepEnd/rawStepStart < 20 {
		t.Fatalf("expected heavily skewed parametric speed ratio > 20, got %g", rawStepEnd/rawStepStart)
	}

	const (
		speed = float32(100) // 100 units/sec
		dt    = 20 * time.Millisecond
		want  = 2.0 // 100 * 0.02 = 2.0 units per step
	)
	f := tween.NewPathFollower(curve, tween.FollowerOptions{
		Speed:           speed,
		OrientToTangent: true,
	})

	prev := f.Position()
	steps := 0
	for !f.Done() {
		f.Update(dt)
		cur := f.Position()
		if !f.Done() {
			moved := pointDist(prev, cur)
			relErr := math.Abs(moved-want) / want
			if relErr >= 0.01 {
				t.Fatalf("step %d: moved %g units, want %g (relErr=%.4f%% >= 1%%)", steps, moved, want, relErr*100)
			}
			steps++
		}
		prev = cur
	}
	if steps < 200 {
		t.Fatalf("expected > 200 constant-speed steps on curve of length %g, got %d", curve.Length(), steps)
	}
	assertPointNear(t, "final position", f.Position(), core.Point{X: 400, Y: 300}, 1e-3)
}

func TestPathFollowerOrientDurationEaseLoopsAndCallbacks(t *testing.T) {
	const ms = time.Millisecond

	t.Run("OrientToTangent and RotationOffset", func(t *testing.T) {
		const r = 100.0
		k := r * 4.0 * (math.Sqrt2 - 1.0) / 3.0
		arc := tween.CubicBezier(
			core.Point{X: core.Points(r), Y: 0},
			core.Point{X: core.Points(r), Y: core.Points(k)},
			core.Point{X: core.Points(k), Y: core.Points(r)},
			core.Point{X: 0, Y: core.Points(r)},
		)
		const rotOff = float32(math.Pi / 2)
		f := tween.NewPathFollower(arc, tween.FollowerOptions{
			Duration:        time.Second,
			OrientToTangent: true,
			RotationOffset:  rotOff,
		})
		// At start: tangent is (0, 1) -> atan2 = pi/2, plus offset pi/2 = pi.
		if !near(float64(f.Angle()), math.Pi, 1e-4) {
			t.Fatalf("start angle = %g, want pi", f.Angle())
		}
		// At midpoint: tangent is (-sqrt2/2, sqrt2/2) -> atan2 = 3*pi/4, plus offset = 5*pi/4.
		f.Seek(0.5)
		if !near(float64(f.Angle()), 5*math.Pi/4, 1e-3) {
			t.Fatalf("mid angle = %g, want 5*pi/4", f.Angle())
		}
		// At end: tangent is (-1, 0) -> atan2 = pi, plus offset = 3*pi/2.
		f.Seek(1.0)
		if !near(float64(f.Angle()), 3*math.Pi/2, 1e-4) {
			t.Fatalf("end angle = %g, want 3*pi/2", f.Angle())
		}

		// With OrientToTangent=false, Angle() stays fixed at RotationOffset.
		fixed := tween.NewPathFollower(arc, tween.FollowerOptions{
			Duration:        time.Second,
			OrientToTangent: false,
			RotationOffset:  0.75,
		})
		fixed.Seek(0.5)
		if fixed.Angle() != 0.75 {
			t.Fatalf("fixed angle = %g, want 0.75", fixed.Angle())
		}
	})

	t.Run("Duration Ease OnUpdate and single-fire OnComplete", func(t *testing.T) {
		line := tween.Polyline([]core.Point{{X: 0, Y: 0}, {X: 100, Y: 0}}, false)
		quadIn := func(u float32) float32 { return u * u }

		updates := 0
		completes := 0
		var lastPos core.Point
		f := tween.NewPathFollower(line, tween.FollowerOptions{
			Duration: time.Second,
			Ease:     quadIn,
			OnUpdate: func(pos core.Point, angle float32) {
				updates++
				lastPos = pos
			},
			OnComplete: func() {
				completes++
			},
		})

		for range 5 {
			f.Update(100 * ms)
		}
		// At t=0.5, quadIn(0.5) = 0.25 -> distance 25, x=25.
		if !near(float64(f.Progress()), 0.5, 1e-5) {
			t.Fatalf("progress at 500ms = %g, want 0.5", f.Progress())
		}
		if !near(float64(f.Distance()), 25, 1e-4) {
			t.Fatalf("distance at 500ms = %g, want 25", f.Distance())
		}
		assertPointNear(t, "pos at 500ms", f.Position(), core.Point{X: 25, Y: 0}, 1e-4)
		if f.Done() || completes != 0 {
			t.Fatalf("premature completion: done=%v completes=%d", f.Done(), completes)
		}

		for range 5 {
			f.Update(100 * ms)
		}
		if !f.Done() {
			t.Fatal("expected Done() after 10*100ms")
		}
		if completes != 1 {
			t.Fatalf("completes = %d, want 1", completes)
		}
		assertPointNear(t, "lastPos", lastPos, core.Point{X: 100, Y: 0}, 1e-4)

		// Extra updates after completion do not re-fire OnComplete or OnUpdate.
		prevUpdates := updates
		f.Update(500 * ms)
		f.Step(0.5)
		if completes != 1 || updates != prevUpdates {
			t.Fatalf("post-done updates mutated callbacks: completes=%d updates=%d (was %d)", completes, updates, prevUpdates)
		}

		// Reset allows replaying cleanly and firing OnComplete again once.
		f.Reset()
		if f.Done() || f.Progress() != 0 || f.Distance() != 0 {
			t.Fatalf("after Reset: done=%v progress=%g dist=%g", f.Done(), f.Progress(), f.Distance())
		}
		f.Step(1.0)
		if !f.Done() || completes != 2 {
			t.Fatalf("after replay: done=%v completes=%d, want true and 2", f.Done(), completes)
		}

		// EaseFromCurve wraps core.EaseInOut cleanly.
		fCurve := tween.NewPathFollower(line, tween.FollowerOptions{
			Duration: time.Second,
			Ease:     tween.EaseFromCurve(core.EaseInOut),
		})
		fCurve.Update(500 * ms)
		if !near(float64(fCurve.Distance()), 50, 1e-3) {
			t.Fatalf("EaseInOut midpoint distance = %g, want 50", fCurve.Distance())
		}
	})

	t.Run("LoopRepeat wraps cleanly", func(t *testing.T) {
		line := tween.Polyline([]core.Point{{X: 0, Y: 0}, {X: 100, Y: 0}}, false)
		completes := 0
		f := tween.NewPathFollower(line, tween.FollowerOptions{
			Speed:      100,
			Loop:       tween.LoopRepeat,
			OnComplete: func() { completes++ },
		})

		f.Update(1250 * ms) // 1.25 passes -> dist 25
		if f.Done() || completes != 0 {
			t.Fatalf("LoopRepeat should not complete: done=%v completes=%d", f.Done(), completes)
		}
		assertPointNear(t, "repeat 1.25", f.Position(), core.Point{X: 25, Y: 0}, 1e-3)

		f.Update(2500 * ms) // 3.75 passes -> dist 75
		assertPointNear(t, "repeat 3.75", f.Position(), core.Point{X: 75, Y: 0}, 1e-3)
	})

	t.Run("LoopPingPong oscillates and flips tangent", func(t *testing.T) {
		line := tween.Polyline([]core.Point{{X: 0, Y: 0}, {X: 100, Y: 0}}, false)
		f := tween.NewPathFollower(line, tween.FollowerOptions{
			Duration:        time.Second,
			Loop:            tween.LoopPingPong,
			OrientToTangent: true,
		})

		f.Update(250 * ms) // forward 0.25
		assertPointNear(t, "pingpong 0.25", f.Position(), core.Point{X: 25, Y: 0}, 1e-3)
		assertUnitTangent(t, "pingpong forward tan", f.Tangent(), 1, 0, 1e-4)
		if !near(float64(f.Angle()), 0, 1e-4) {
			t.Fatalf("forward angle = %g, want 0", f.Angle())
		}

		f.Update(1000 * ms) // 1.25 -> backward at 0.75
		assertPointNear(t, "pingpong 1.25", f.Position(), core.Point{X: 75, Y: 0}, 1e-3)
		assertUnitTangent(t, "pingpong backward tan", f.Tangent(), -1, 0, 1e-4)
		if !near(math.Abs(float64(f.Angle())), math.Pi, 1e-4) {
			t.Fatalf("backward angle = %g, want +-pi", f.Angle())
		}

		f.Update(1000 * ms) // 2.25 -> forward again at 0.25
		assertPointNear(t, "pingpong 2.25", f.Position(), core.Point{X: 25, Y: 0}, 1e-3)
		assertUnitTangent(t, "pingpong cycle 2 forward tan", f.Tangent(), 1, 0, 1e-4)
		if f.Done() {
			t.Fatal("LoopPingPong should never report Done()")
		}
	})

	t.Run("Timeline Group and game.Loop integration", func(t *testing.T) {
		curve := tween.Polyline([]core.Point{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}}, false)

		var followPos core.Point
		var followAngle float32
		tl := tween.NewTimeline()
		tl.Append(tween.Follow(curve, 200*ms, core.Linear, func(pos core.Point, angle float32) {
			followPos = pos
			followAngle = angle
		}))

		tl.Seek(50 * ms) // 25% of length 200 = dist 50 -> (50, 0), angle 0
		assertPointNear(t, "timeline Follow seek 50ms", followPos, core.Point{X: 50, Y: 0}, 1e-3)
		if !near(float64(followAngle), 0, 1e-4) {
			t.Fatalf("timeline Follow angle at 50ms = %g, want 0", followAngle)
		}
		tl.Seek(150 * ms) // 75% of length 200 = dist 150 -> (100, 50), angle pi/2
		assertPointNear(t, "timeline Follow seek 150ms", followPos, core.Point{X: 100, Y: 50}, 1e-3)
		if !near(float64(followAngle), math.Pi/2, 1e-4) {
			t.Fatalf("timeline Follow angle at 150ms = %g, want pi/2", followAngle)
		}

		// PathFollower.Tween() runs on a Timeline and game.Loop deterministically.
		completed := 0
		pf := tween.NewPathFollower(curve, tween.FollowerOptions{
			Speed:           1000, // 200 units / 1000 = 200ms
			OrientToTangent: true,
			OnComplete:      func() { completed++ },
		})
		tl2 := tween.NewTimeline()
		tl2.Append(pf.Tween())
		tl2.Play()

		l := &game.Loop{Rate: 50, Update: tl2.Update}
		app := startLoop(t, l)
		app.Advance(200 * ms)

		if !tl2.Done() || !pf.Done() || completed != 1 {
			t.Fatalf("after loop: tl.Done=%v pf.Done=%v completed=%d", tl2.Done(), pf.Done(), completed)
		}
		assertPointNear(t, "pf end", pf.Position(), core.Point{X: 100, Y: 100}, 1e-3)
	})

	t.Run("nil and degenerate curves do not panic", func(t *testing.T) {
		f := tween.NewPathFollower(nil, tween.FollowerOptions{Speed: 50, OrientToTangent: true, RotationOffset: 1})
		f.Update(100 * ms)
		if !f.Done() || f.Angle() != 1 || f.Position() != (core.Point{}) {
			t.Fatalf("nil-curve follower unexpected state: done=%v angle=%g pos=%+v", f.Done(), f.Angle(), f.Position())
		}

		zeroSpeed := tween.NewPathFollower(tween.Polyline([]core.Point{{X: 0, Y: 0}, {X: 10, Y: 0}}, false), tween.FollowerOptions{})
		zeroSpeed.Update(time.Second)
		if zeroSpeed.Done() || zeroSpeed.Distance() != 0 {
			t.Fatalf("zero-speed follower should remain stationary")
		}
	})
}

func TestTiledPolylineAndPolygonCurves(t *testing.T) {
	// Load the real Tiled .tmj fixture from tilemap/testdata/sample.tmj.
	samplePath := filepath.Join("..", "tilemap", "testdata", "sample.tmj")
	raw, err := os.ReadFile(samplePath)
	if err != nil {
		t.Fatalf("read sample.tmj: %v", err)
	}
	m, err := tilemap.LoadTiled(raw, tilemap.TilesetLoader{
		LoadJSON: func(source string) ([]byte, error) {
			return os.ReadFile(filepath.Join("..", "tilemap", "testdata", source))
		},
	})
	if err != nil {
		t.Fatalf("LoadTiled(sample.tmj): %v", err)
	}

	entities := m.ObjectLayer("entities")
	if entities == nil {
		t.Fatal("missing entities object layer")
	}

	var patrolObj, rampObj tilemap.Object
	for _, obj := range entities.Objects {
		switch obj.Name {
		case "patrol":
			patrolObj = obj
		case "ramp":
			rampObj = obj
		}
	}

	// Patrol polyline: X=4, Y=8, points (0,0), (20,0), (20,12) => world (4,8), (24,8), (24,20), length = 32.
	patrolPoly := tween.CurveFromTiledObject(patrolObj, false)
	if patrolPoly.Closed() {
		t.Fatal("patrol polyline Closed() = true, want false")
	}
	if !near(float64(patrolPoly.Length()), 32, 1e-4) {
		t.Fatalf("patrol polyline Length() = %g, want 32", patrolPoly.Length())
	}
	assertPointNear(t, "patrol start", patrolPoly.Point(0), core.Point{X: 4, Y: 8}, 1e-5)
	assertPointNear(t, "patrol corner", patrolPoly.PointAtDistance(20), core.Point{X: 24, Y: 8}, 1e-5)
	assertPointNear(t, "patrol end", patrolPoly.Point(1), core.Point{X: 24, Y: 20}, 1e-5)

	// Smooth Catmull-Rom version of patrol polyline passes through all 3 world points.
	patrolSmooth := tween.CurveFromTiledObject(patrolObj, true)
	if patrolSmooth.Closed() {
		t.Fatal("patrol smooth Closed() = true, want false")
	}
	assertPointNear(t, "patrol smooth start", patrolSmooth.Point(0), core.Point{X: 4, Y: 8}, 1e-4)
	assertPointNear(t, "patrol smooth corner", patrolSmooth.Point(0.5), core.Point{X: 24, Y: 8}, 1e-4)
	assertPointNear(t, "patrol smooth end", patrolSmooth.Point(1), core.Point{X: 24, Y: 20}, 1e-4)

	// Follow the Tiled patrol path at constant speed 16 pts/sec (2 seconds total).
	follower := tween.NewPathFollower(patrolPoly, tween.FollowerOptions{
		Speed:           16,
		OrientToTangent: true,
	})
	follower.Update(1250 * time.Millisecond) // 20 points -> exact corner (24, 8)
	assertPointNear(t, "follower at corner", follower.Position(), core.Point{X: 24, Y: 8}, 1e-3)
	follower.Update(750 * time.Millisecond) // +12 points -> exact end (24, 20)
	if !follower.Done() {
		t.Fatal("patrol follower should finish after 2s")
	}
	assertPointNear(t, "follower at end", follower.Position(), core.Point{X: 24, Y: 20}, 1e-3)
	if !near(float64(follower.Angle()), math.Pi/2, 1e-4) {
		t.Fatalf("patrol follower end angle = %g, want pi/2", follower.Angle())
	}

	// Ramp polygon: X=16, Y=32, points (0,0), (16,0), (16,-8) => world (16,32), (32,32), (32,24).
	// Closed perimeter = 16 + 8 + hypot(16, 8) = 24 + sqrt(320).
	wantRampLen := 24 + math.Hypot(16, 8)
	rampPoly := tween.PolylineFromTiledObject(rampObj)
	if !rampPoly.Closed() {
		t.Fatal("ramp polygon Closed() = false, want true")
	}
	if !near(float64(rampPoly.Length()), wantRampLen, 1e-4) {
		t.Fatalf("ramp polygon Length() = %g, want %g", rampPoly.Length(), wantRampLen)
	}
	assertPointNear(t, "ramp start", rampPoly.Point(0), core.Point{X: 16, Y: 32}, 1e-5)
	assertPointNear(t, "ramp end wrap", rampPoly.Point(1), core.Point{X: 16, Y: 32}, 1e-5)

	rampSmooth := tween.CatmullRomFromTiledObject(rampObj)
	if !rampSmooth.Closed() {
		t.Fatal("ramp smooth polygon Closed() = false, want true")
	}
	assertPointNear(t, "ramp smooth v0", rampSmooth.Point(0), core.Point{X: 16, Y: 32}, 1e-4)
	assertPointNear(t, "ramp smooth v1", rampSmooth.Point(1.0/3.0), core.Point{X: 32, Y: 32}, 1e-4)
	assertPointNear(t, "ramp smooth v2", rampSmooth.Point(2.0/3.0), core.Point{X: 32, Y: 24}, 1e-4)
	assertPointNear(t, "ramp smooth wrap", rampSmooth.Point(1), core.Point{X: 16, Y: 32}, 1e-4)

	// Follow closed smooth Tiled polygon with LoopRepeat over 1.5 laps.
	loopFollower := tween.NewPathFollower(rampSmooth, tween.FollowerOptions{
		Duration:        time.Second,
		Loop:            tween.LoopRepeat,
		OrientToTangent: true,
	})
	loopFollower.Update(1500 * time.Millisecond)
	if loopFollower.Done() {
		t.Fatal("LoopRepeat on closed polygon should never complete")
	}
	assertPointNear(t, "loop follower at 1.5 laps", loopFollower.Position(), rampSmooth.PointAtDistance(rampSmooth.Length()/2), 1e-3)
}
