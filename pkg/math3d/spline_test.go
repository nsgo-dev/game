package math3d_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/math3d"
)

func assertOrthonormalFrame(t *testing.T, label string, f math3d.CurveFrame) {
	t.Helper()
	const tol = 1e-3
	if !math3d.ApproxEqual(f.Tangent.Len(), 1, tol) {
		t.Fatalf("%s: |Tangent| = %v, want 1", label, f.Tangent.Len())
	}
	if !math3d.ApproxEqual(f.Up.Len(), 1, tol) {
		t.Fatalf("%s: |Up| = %v, want 1", label, f.Up.Len())
	}
	if !math3d.ApproxEqual(f.Right.Len(), 1, tol) {
		t.Fatalf("%s: |Right| = %v, want 1", label, f.Right.Len())
	}
	if !math3d.ApproxEqual(f.Tangent.Dot(f.Up), 0, tol) {
		t.Fatalf("%s: Tangent·Up = %v, want 0", label, f.Tangent.Dot(f.Up))
	}
	if !math3d.ApproxEqual(f.Tangent.Dot(f.Right), 0, tol) {
		t.Fatalf("%s: Tangent·Right = %v, want 0", label, f.Tangent.Dot(f.Right))
	}
	if !math3d.ApproxEqual(f.Up.Dot(f.Right), 0, tol) {
		t.Fatalf("%s: Up·Right = %v, want 0", label, f.Up.Dot(f.Right))
	}
	if got := f.Tangent.Cross(f.Up); !got.ApproxEqual(f.Right, tol) {
		t.Fatalf("%s: Tangent×Up = %v, want Right = %v", label, got, f.Right)
	}
	if got := f.Right.Cross(f.Tangent); !got.ApproxEqual(f.Up, tol) {
		t.Fatalf("%s: Right×Tangent = %v, want Up = %v", label, got, f.Up)
	}
}

func TestCurveFrameConversions(t *testing.T) {
	// Arbitrary right-handed orthonormal frame: forward along (1, 1, -1)/sqrt(3).
	tan := math3d.Vec3{X: 1, Y: 1, Z: -1}.Norm()
	right := tan.Cross(math3d.Vec3Up()).Norm()
	up := right.Cross(tan).Norm()
	pos := math3d.Vec3{X: 3, Y: -2, Z: 7}

	frame := math3d.CurveFrame{
		Position: pos,
		Tangent:  tan,
		Up:       up,
		Right:    right,
	}
	assertOrthonormalFrame(t, "manual", frame)

	rot := frame.Rotation()
	if !math3d.ApproxEqual(rot.Len(), 1, eps) {
		t.Fatalf("Rotation length = %v, want 1", rot.Len())
	}
	if got := rot.RotateVec3(math3d.Vec3Right()); !got.ApproxEqual(frame.Right, eps) {
		t.Fatalf("Rot * +X = %v, want Right = %v", got, frame.Right)
	}
	if got := rot.RotateVec3(math3d.Vec3Up()); !got.ApproxEqual(frame.Up, eps) {
		t.Fatalf("Rot * +Y = %v, want Up = %v", got, frame.Up)
	}
	if got := rot.RotateVec3(math3d.Vec3Forward()); !got.ApproxEqual(frame.Tangent, eps) {
		t.Fatalf("Rot * -Z = %v, want Tangent = %v", got, frame.Tangent)
	}

	tr := frame.Transform()
	if !tr.Pos.ApproxEqual(pos, eps) || !tr.Scale.ApproxEqual(math3d.Vec3One(), eps) {
		t.Fatalf("Transform = %+v", tr)
	}

	m := frame.Mat4()
	if !m.ApproxEqual(tr.ToMat4(), eps) {
		t.Fatalf("Mat4() != Transform().ToMat4()")
	}
	if !math3d.ApproxEqual(m.Det(), 1, eps) {
		t.Fatalf("Mat4().Det() = %v, want +1", m.Det())
	}
	if got := m.TransformPoint(math3d.Vec3Zero()); !got.ApproxEqual(pos, eps) {
		t.Fatalf("Mat4 * origin = %v, want %v", got, pos)
	}
	if got := m.TransformDir(math3d.Vec3Forward()); !got.ApproxEqual(frame.Tangent, eps) {
		t.Fatalf("Mat4 * forward = %v, want %v", got, frame.Tangent)
	}
	if got := m.TransformDir(math3d.Vec3Up()); !got.ApproxEqual(frame.Up, eps) {
		t.Fatalf("Mat4 * up = %v, want %v", got, frame.Up)
	}
	if got := m.TransformDir(math3d.Vec3Right()); !got.ApproxEqual(frame.Right, eps) {
		t.Fatalf("Mat4 * right = %v, want %v", got, frame.Right)
	}

	// Zero CurveFrame Rotation should safely return identity.
	if got := (math3d.CurveFrame{}).Rotation(); !got.ApproxEqual(math3d.QuatIdentity(), eps) {
		t.Fatalf("zero CurveFrame.Rotation() = %v, want identity", got)
	}
}

func TestStraightLineAnalytic(t *testing.T) {
	a := math3d.Vec3{X: 1, Y: 2, Z: 3}
	b := math3d.Vec3{X: 4, Y: 6, Z: 3} // length = 5, dir = (0.6, 0.8, 0)
	wantLen := float32(5)
	wantDir := b.Sub(a).Norm()
	delta := b.Sub(a)

	curves := []struct {
		name  string
		curve math3d.Curve3D
	}{
		{"QuadraticBezier3D", math3d.NewQuadraticBezier3D(a, a.Lerp(b, 0.5), b)},
		{"CubicBezier3D", math3d.NewCubicBezier3D(a, a.Lerp(b, 1.0/3.0), a.Lerp(b, 2.0/3.0), b)},
		{"CubicHermite3D", math3d.NewCubicHermite3D(a, delta, b, delta)},
		{"HermiteSpline3D", math3d.NewHermiteSpline3D(
			[]math3d.Vec3{a, a.Lerp(b, 0.5), b},
			[]math3d.Vec3{delta.Scale(0.5), delta.Scale(0.5), delta.Scale(0.5)},
			false,
		)},
		{"CatmullRom3D", math3d.NewCatmullRom3D([]math3d.Vec3{a, a.Lerp(b, 0.5), b}, false)},
	}

	for _, tc := range curves {
		t.Run(tc.name, func(t *testing.T) {
			if tc.curve.Closed() {
				t.Fatalf("Closed() = true, want false")
			}
			if !math3d.ApproxEqual(tc.curve.Length(), wantLen, 1e-4) {
				t.Fatalf("Length() = %v, want %v", tc.curve.Length(), wantLen)
			}
			for i := 0; i <= 10; i++ {
				u := float32(i) / 10
				wantPos := a.Lerp(b, u)
				if got := tc.curve.Point(u); !got.ApproxEqual(wantPos, 1e-4) {
					t.Fatalf("Point(%v) = %v, want %v", u, got, wantPos)
				}
				if got := tc.curve.Tangent(u); !got.ApproxEqual(wantDir, 1e-4) {
					t.Fatalf("Tangent(%v) = %v, want %v", u, got, wantDir)
				}
				d := u * wantLen
				if gotT := tc.curve.DistanceToT(d); !math3d.ApproxEqual(gotT, u, 1e-4) {
					t.Fatalf("DistanceToT(%v) = %v, want %v", d, gotT, u)
				}
				if gotP := tc.curve.PointAtDistance(d); !gotP.ApproxEqual(wantPos, 1e-4) {
					t.Fatalf("PointAtDistance(%v) = %v, want %v", d, gotP, wantPos)
				}
				if gotTan := tc.curve.TangentAtDistance(d); !gotTan.ApproxEqual(wantDir, 1e-4) {
					t.Fatalf("TangentAtDistance(%v) = %v, want %v", d, gotTan, wantDir)
				}
			}
		})
	}
}

func TestQuadraticBezierParabolaAnalytic(t *testing.T) {
	// P(t) = (t, t^2, 0): parabola y = x^2 for x in [0, 1].
	// Exact arc length = 0.5*sqrt(5) + 0.25*asinh(2) ≈ 1.4789428575445974.
	q := math3d.NewQuadraticBezier3D(
		math3d.Vec3{X: 0, Y: 0, Z: 0},
		math3d.Vec3{X: 0.5, Y: 0, Z: 0},
		math3d.Vec3{X: 1, Y: 1, Z: 0},
	)
	wantLen := float32(0.5*math.Sqrt(5) + 0.25*math.Asinh(2))
	if !math3d.ApproxEqual(q.Length(), wantLen, 1e-5) {
		t.Fatalf("parabola Length() = %.7f, want %.7f", q.Length(), wantLen)
	}

	for i := 0; i <= 10; i++ {
		u := float32(i) / 10
		wantP := math3d.Vec3{X: u, Y: u * u, Z: 0}
		if got := q.Point(u); !got.ApproxEqual(wantP, 1e-5) {
			t.Fatalf("Point(%v) = %v, want %v", u, got, wantP)
		}
		wantDeriv := math3d.Vec3{X: 1, Y: 2 * u, Z: 0}
		if got := q.Derivative(u); !got.ApproxEqual(wantDeriv, 1e-5) {
			t.Fatalf("Derivative(%v) = %v, want %v", u, got, wantDeriv)
		}
		if got := q.Tangent(u); !got.ApproxEqual(wantDeriv.Norm(), 1e-5) {
			t.Fatalf("Tangent(%v) = %v, want %v", u, got, wantDeriv.Norm())
		}
	}
}

func TestCubicBezierQuarterCircleAnalytic(t *testing.T) {
	// Standard cubic Bézier approximation of a quarter circle of radius R in XY plane.
	const R = float32(3.0)
	k := float32((4.0 / 3.0) * (math.Sqrt2 - 1.0))

	c := math3d.NewCubicBezier3D(
		math3d.Vec3{X: R, Y: 0, Z: 0},
		math3d.Vec3{X: R, Y: k * R, Z: 0},
		math3d.Vec3{X: k * R, Y: R, Z: 0},
		math3d.Vec3{X: 0, Y: R, Z: 0},
	)

	wantLen := float32(math.Pi * float64(R) / 2.0)
	// Cubic Bézier quarter-circle arc length differs from pi*R/2 by ~0.015%.
	if relErr := math.Abs(float64(c.Length()-wantLen)) / float64(wantLen); relErr > 5e-4 {
		t.Fatalf("quarter-circle Length() = %v, want %v (relErr = %v)", c.Length(), wantLen, relErr)
	}

	// Endpoint positions and tangents.
	if got := c.Point(0); !got.ApproxEqual(math3d.Vec3{X: R, Y: 0, Z: 0}, eps) {
		t.Fatalf("Point(0) = %v", got)
	}
	if got := c.Point(1); !got.ApproxEqual(math3d.Vec3{X: 0, Y: R, Z: 0}, eps) {
		t.Fatalf("Point(1) = %v", got)
	}
	if got := c.Tangent(0); !got.ApproxEqual(math3d.Vec3{X: 0, Y: 1, Z: 0}, eps) {
		t.Fatalf("Tangent(0) = %v, want +Y", got)
	}
	if got := c.Tangent(1); !got.ApproxEqual(math3d.Vec3{X: -1, Y: 0, Z: 0}, eps) {
		t.Fatalf("Tangent(1) = %v, want -X", got)
	}

	// Midpoint at 45 degrees.
	mid := float32(float64(R) * math.Sqrt2 / 2.0)
	if got := c.Point(0.5); !got.ApproxEqual(math3d.Vec3{X: mid, Y: mid, Z: 0}, 2e-3) {
		t.Fatalf("Point(0.5) = %v, want (%v, %v, 0)", got, mid, mid)
	}
	// All points stay within 0.03% of radius R.
	for i := 0; i <= 20; i++ {
		p := c.Point(float32(i) / 20)
		if !math3d.ApproxEqual(p.Len(), R, 1e-3*R) {
			t.Fatalf("Point(%v) radius = %v, want %v", float32(i)/20, p.Len(), R)
		}
	}
}

func TestCubicHermiteExactEndpointsAndMultiSegment(t *testing.T) {
	p0 := math3d.Vec3{X: 1, Y: -2, Z: 0.5}
	m0 := math3d.Vec3{X: 3, Y: 4, Z: -1}
	p1 := math3d.Vec3{X: 5, Y: 2, Z: -3}
	m1 := math3d.Vec3{X: -2, Y: 5, Z: 2}

	h := math3d.NewCubicHermite3D(p0, m0, p1, m1)
	if !h.Point(0).ApproxEqual(p0, 1e-5) {
		t.Fatalf("Point(0) = %v, want %v", h.Point(0), p0)
	}
	if !h.Point(1).ApproxEqual(p1, 1e-5) {
		t.Fatalf("Point(1) = %v, want %v", h.Point(1), p1)
	}
	if !h.Derivative(0).ApproxEqual(m0, 1e-5) {
		t.Fatalf("Derivative(0) = %v, want %v", h.Derivative(0), m0)
	}
	if !h.Derivative(1).ApproxEqual(m1, 1e-5) {
		t.Fatalf("Derivative(1) = %v, want %v", h.Derivative(1), m1)
	}
	if !h.Tangent(0).ApproxEqual(m0.Norm(), 1e-5) {
		t.Fatalf("Tangent(0) = %v, want %v", h.Tangent(0), m0.Norm())
	}
	if !h.Tangent(1).ApproxEqual(m1.Norm(), 1e-5) {
		t.Fatalf("Tangent(1) = %v, want %v", h.Tangent(1), m1.Norm())
	}

	// Multi-segment open and closed HermiteSpline3D.
	pts := []math3d.Vec3{
		{X: 0, Y: 0, Z: 0},
		{X: 2, Y: 3, Z: -1},
		{X: 5, Y: 1, Z: -4},
		{X: 3, Y: -2, Z: 2},
	}
	tans := []math3d.Vec3{
		{X: 1, Y: 2, Z: 0},
		{X: 3, Y: 0, Z: -2},
		{X: 0, Y: -3, Z: 1},
		{X: -2, Y: 1, Z: 2},
	}

	for _, closed := range []bool{false, true} {
		sp := math3d.NewHermiteSpline3D(pts, tans, closed)
		if sp.Closed() != closed {
			t.Fatalf("closed=%v: Closed() = %v", closed, sp.Closed())
		}
		nSeg := len(pts) - 1
		if closed {
			nSeg = len(pts)
		}
		if sp.SegmentCount() != nSeg {
			t.Fatalf("closed=%v: SegmentCount() = %d, want %d", closed, sp.SegmentCount(), nSeg)
		}

		// Control point interpolation and C1 derivative continuity at every knot.
		for i := range len(pts) {
			u := float32(i) / float32(nSeg)
			if got := sp.Point(u); !got.ApproxEqual(pts[i], 1e-4) {
				t.Fatalf("closed=%v: Point(%v) = %v, want %v", closed, u, got, pts[i])
			}
			wantDeriv := tans[i].Scale(float32(nSeg))
			if got := sp.Derivative(u); !got.ApproxEqual(wantDeriv, 1e-3) {
				t.Fatalf("closed=%v: Derivative(%v) = %v, want %v", closed, u, got, wantDeriv)
			}
			if i > 0 && (closed || i < len(pts)-1) {
				const dt = 1e-5
				dL := sp.Derivative(u - dt)
				dR := sp.Derivative(u + dt)
				if !dL.ApproxEqual(dR, 2e-2) {
					t.Fatalf("closed=%v knot %d: derivative discontinuous: left=%v right=%v", closed, i, dL, dR)
				}
				tL := sp.Tangent(u - dt)
				tR := sp.Tangent(u + dt)
				if !tL.ApproxEqual(tR, 2e-3) {
					t.Fatalf("closed=%v knot %d: tangent discontinuous: left=%v right=%v", closed, i, tL, tR)
				}
			}
		}

		if closed {
			if !sp.Point(0).ApproxEqual(sp.Point(1), 1e-4) {
				t.Fatalf("closed Hermite: Point(0) != Point(1)")
			}
			if !sp.Derivative(0).ApproxEqual(sp.Derivative(1), 1e-3) {
				t.Fatalf("closed Hermite: Derivative(0) != Derivative(1)")
			}
		}
	}
}

// barryGoldmanEval evaluates a single Catmull-Rom segment directly via the
// Barry-Goldman pyramid to cross-check our cubic Hermite conversion.
func barryGoldmanEval(p0, p1, p2, p3 math3d.Vec3, alpha, u float64) math3d.Vec3 {
	t0 := 0.0
	t1 := t0 + math.Pow(float64(p1.Sub(p0).Len()), alpha)
	t2 := t1 + math.Pow(float64(p2.Sub(p1).Len()), alpha)
	t3 := t2 + math.Pow(float64(p3.Sub(p2).Len()), alpha)
	tau := t1 + u*(t2-t1)

	lerp := func(a, b math3d.Vec3, ta, tb float64) math3d.Vec3 {
		w := float32((tau - ta) / (tb - ta))
		return a.Lerp(b, w)
	}
	a1 := lerp(p0, p1, t0, t1)
	a2 := lerp(p1, p2, t1, t2)
	a3 := lerp(p2, p3, t2, t3)
	b1 := lerp(a1, a2, t0, t2)
	b2 := lerp(a2, a3, t1, t3)
	return lerp(b1, b2, t1, t2)
}

func TestCatmullRomInterpolationAndContinuity(t *testing.T) {
	pts := []math3d.Vec3{
		{X: 0, Y: 0, Z: 0},
		{X: 1, Y: 2, Z: -1},
		{X: 5, Y: 1, Z: -4}, // deliberately non-uniform chord spacing
		{X: 7, Y: 4, Z: 0},
		{X: 4, Y: -1, Z: 3},
	}

	for _, alpha := range []float32{0.0, 0.5, 1.0} {
		for _, closed := range []bool{false, true} {
			cr := math3d.NewCatmullRom3DAlpha(pts, alpha, closed)
			if cr.Alpha() != alpha {
				t.Fatalf("Alpha() = %v, want %v", cr.Alpha(), alpha)
			}
			nSeg := len(pts) - 1
			if closed {
				nSeg = len(pts)
			}
			if cr.SegmentCount() != nSeg {
				t.Fatalf("SegmentCount() = %d, want %d", cr.SegmentCount(), nSeg)
			}

			// Exact control-point interpolation.
			for i := range len(pts) {
				u := float32(i) / float32(nSeg)
				if got := cr.Point(u); !got.ApproxEqual(pts[i], 1e-4) {
					t.Fatalf("alpha=%v closed=%v: Point(%v) = %v, want %v", alpha, closed, u, got, pts[i])
				}
			}

			// Cross-check interior segment 1 (pts[0], pts[1], pts[2], pts[3]) against Barry-Goldman pyramid.
			for k := 0; k <= 10; k++ {
				localU := float64(k) / 10.0
				globalT := float32((1.0 + localU) / float64(nSeg))
				want := barryGoldmanEval(pts[0], pts[1], pts[2], pts[3], float64(alpha), localU)
				if got := cr.Point(globalT); !got.ApproxEqual(want, 1e-3) {
					t.Fatalf("alpha=%v closed=%v localU=%v: Point = %v, want Barry-Goldman %v", alpha, closed, localU, got, want)
				}
			}

			// C1 continuity across every interior control point (unit tangent + arc-length derivative).
			for i := 1; i < len(pts); i++ {
				if !closed && i == len(pts)-1 {
					continue
				}
				u := float32(i) / float32(nSeg)
				const dt = 1e-5
				tL := cr.Tangent(u - dt)
				tR := cr.Tangent(u + dt)
				if !tL.ApproxEqual(tR, 2e-3) {
					t.Fatalf("alpha=%v closed=%v knot %d: tangent jump left=%v right=%v", alpha, closed, i, tL, tR)
				}
				// Uniform Catmull-Rom (alpha=0) is also C1 in raw parameter t.
				if alpha == 0 {
					dL := cr.Derivative(u - dt)
					dR := cr.Derivative(u + dt)
					if !dL.ApproxEqual(dR, 2e-2) {
						t.Fatalf("uniform knot %d: derivative jump left=%v right=%v", i, dL, dR)
					}
				}
			}

			// Closed loop continuity and wrapping.
			if closed {
				if !cr.Point(0).ApproxEqual(cr.Point(1), 1e-4) {
					t.Fatalf("alpha=%v: closed Point(0) != Point(1)", alpha)
				}
				if !cr.Tangent(0).ApproxEqual(cr.Tangent(1), 1e-4) {
					t.Fatalf("alpha=%v: closed Tangent(0) != Tangent(1)", alpha)
				}
				if !cr.Point(1.25).ApproxEqual(cr.Point(0.25), 1e-4) {
					t.Fatalf("alpha=%v: closed wrap Point(1.25) != Point(0.25)", alpha)
				}
				if !cr.Point(-0.25).ApproxEqual(cr.Point(0.75), 1e-4) {
					t.Fatalf("alpha=%v: closed wrap Point(-0.25) != Point(0.75)", alpha)
				}
				L := cr.Length()
				if !cr.PointAtDistance(1.3*L).ApproxEqual(cr.PointAtDistance(0.3*L), 1e-3) {
					t.Fatalf("alpha=%v: closed wrap PointAtDistance(1.3L) != PointAtDistance(0.3L)", alpha)
				}
				if !cr.PointAtDistance(-0.2*L).ApproxEqual(cr.PointAtDistance(0.8*L), 1e-3) {
					t.Fatalf("alpha=%v: closed wrap PointAtDistance(-0.2L) != PointAtDistance(0.8L)", alpha)
				}
			}
		}
	}

	// Default NewCatmullRom3D uses centripetal alpha = 0.5.
	def := math3d.NewCatmullRom3D(pts, false)
	if def.Alpha() != 0.5 {
		t.Fatalf("NewCatmullRom3D default alpha = %v, want 0.5", def.Alpha())
	}
}

func TestConstantSpeedArcLengthSampling(t *testing.T) {
	curves := []struct {
		name  string
		curve math3d.Curve3D
	}{
		{
			name: "QuadraticBezier_Skewed",
			curve: math3d.NewQuadraticBezier3D(
				math3d.Vec3{X: 0, Y: 0, Z: 0},
				math3d.Vec3{X: 0.2, Y: 6, Z: 0}, // heavily non-uniform speed
				math3d.Vec3{X: 10, Y: 0, Z: 3},
			),
		},
		{
			name: "CubicBezier_ClusteredHandles",
			curve: math3d.NewCubicBezier3D(
				math3d.Vec3{X: 0, Y: 0, Z: 0},
				math3d.Vec3{X: 0.1, Y: 0.2, Z: 0},
				math3d.Vec3{X: 9.8, Y: 5.0, Z: -4.0},
				math3d.Vec3{X: 10, Y: 0, Z: -5},
			),
		},
		{
			name: "CubicHermite_HighSpeed",
			curve: math3d.NewCubicHermite3D(
				math3d.Vec3{X: 0, Y: 0, Z: 0},
				math3d.Vec3{X: 25, Y: 10, Z: 0},
				math3d.Vec3{X: 5, Y: 2, Z: -6},
				math3d.Vec3{X: 1, Y: -8, Z: -10},
			),
		},
		{
			name: "HermiteSpline_Multi",
			curve: math3d.NewHermiteSpline3D(
				[]math3d.Vec3{
					{X: 0, Y: 0, Z: 0},
					{X: 1, Y: 2, Z: -1},
					{X: 8, Y: 3, Z: -2},
					{X: 10, Y: 0, Z: -8},
				},
				[]math3d.Vec3{
					{X: 2, Y: 4, Z: 0},
					{X: 6, Y: 1, Z: -1},
					{X: 4, Y: -2, Z: -5},
					{X: 0, Y: -3, Z: -4},
				},
				false,
			),
		},
		{
			name: "CatmullRom_Closed3D",
			curve: math3d.NewCatmullRom3D(
				[]math3d.Vec3{
					{X: 0, Y: 0, Z: 0},
					{X: 2, Y: 3, Z: -2},
					{X: 8, Y: 1, Z: -5},
					{X: 6, Y: -2, Z: 2},
					{X: 1, Y: -1, Z: 3},
				},
				true,
			),
		},
	}

	const N = 100
	for _, tc := range curves {
		t.Run(tc.name, func(t *testing.T) {
			L := tc.curve.Length()
			if L <= 0 {
				t.Fatalf("Length() = %v, want > 0", L)
			}
			expectedStep := L / float32(N)
			prev := tc.curve.PointAtDistance(0)
			var maxRelErr float32
			for i := 1; i <= N; i++ {
				d := float32(i) * expectedStep
				curr := tc.curve.PointAtDistance(d)
				step := curr.Dist(prev)
				relErr := float32(math.Abs(float64(step-expectedStep))) / expectedStep
				if relErr > maxRelErr {
					maxRelErr = relErr
				}
				prev = curr
			}
			if maxRelErr >= 0.01 {
				t.Fatalf("constant-speed spacing max relative variance = %.4f%%, want < 1%%", maxRelErr*100)
			}
		})
	}
}

func TestCurveFrameVerticalClimbNoFlip(t *testing.T) {
	// Semicircular vertical climb in the XY plane:
	// starts moving +X (t=0), climbs straight up +Y (parallel to Vec3Up!) at t=0.5,
	// exits moving -X (t=1). A naive LookRotation(tan, Vec3Up) flips 180° at t=0.5;
	// RMF keeps Right=(0,0,1) constant and smoothly pitches Up from +Y -> -X -> -Y.
	const R = float32(4.0)
	// Two quarter-circle cubic Bézier segments assembled as a Hermite spline (exact circle tangent continuity).
	k := float32((4.0 / 3.0) * (math.Sqrt2 - 1.0))
	m := 3 * k * R // Hermite endpoint tangent magnitude for a quarter-circle cubic Bézier
	climb := math3d.NewHermiteSpline3D(
		[]math3d.Vec3{
			{X: 0, Y: 0, Z: 0},
			{X: R, Y: R, Z: 0},
			{X: 0, Y: 2 * R, Z: 0},
		},
		[]math3d.Vec3{
			{X: m, Y: 0, Z: 0},
			{X: 0, Y: m, Z: 0},
			{X: -m, Y: 0, Z: 0},
		},
		false,
	)

	// Confirm the tangent truly passes straight through +Y at t = 0.5.
	if got := climb.Tangent(0.5); !got.ApproxEqual(math3d.Vec3Up(), 1e-4) {
		t.Fatalf("vertical climb midpoint Tangent(0.5) = %v, want +Y", got)
	}

	const N = 80
	prev := climb.FrameAt(0, math3d.Vec3Up())
	assertOrthonormalFrame(t, "climb[0]", prev)

	for i := 1; i <= N; i++ {
		u := float32(i) / float32(N)
		f := climb.FrameAt(u, math3d.Vec3Up())
		assertOrthonormalFrame(t, "climb", f)

		// Right must remain locked to +Z with zero flip through vertical +Y!
		if !f.Right.ApproxEqual(math3d.Vec3{X: 0, Y: 0, Z: 1}, 1e-2) {
			t.Fatalf("t=%v: Right = %v, want (0, 0, 1)", u, f.Right)
		}
		// Smoothness check: no sudden flip between adjacent frames.
		if dot := prev.Up.Dot(f.Up); dot < 0.99 {
			t.Fatalf("t=%v: Up flipped/jumped (prev·curr = %v)", u, dot)
		}
		if dot := prev.Right.Dot(f.Right); dot < 0.99 {
			t.Fatalf("t=%v: Right flipped/jumped (prev·curr = %v)", u, dot)
		}
		prev = f
	}

	// Verify expected pitch rotation: at t=0.5 (tangent=+Y), Up should be (-1, 0, 0).
	mid := climb.FrameAt(0.5, math3d.Vec3Up())
	if !mid.Up.ApproxEqual(math3d.Vec3{X: -1, Y: 0, Z: 0}, 1e-2) {
		t.Fatalf("t=0.5: Up = %v, want (-1, 0, 0)", mid.Up)
	}
}

func TestCurveFrameSCurveInflectionNoFlip(t *testing.T) {
	// Planar XZ S-curve cubic Bézier with an exact inflection point at t=0.5
	// where Frenet-Serret normal flips 180 degrees. RMF keeps Up=(0,1,0) smooth.
	sCurve := math3d.NewCubicBezier3D(
		math3d.Vec3{X: 0, Y: 0, Z: 0},
		math3d.Vec3{X: 2, Y: 0, Z: 4},
		math3d.Vec3{X: 4, Y: 0, Z: -4},
		math3d.Vec3{X: 6, Y: 0, Z: 0},
	)

	const N = 80
	prev := sCurve.FrameAtDistance(0)
	assertOrthonormalFrame(t, "scurve[0]", prev)
	L := sCurve.Length()

	for i := 1; i <= N; i++ {
		d := float32(i) / float32(N) * L
		f := sCurve.FrameAtDistance(d)
		assertOrthonormalFrame(t, "scurve", f)

		if !f.Up.ApproxEqual(math3d.Vec3Up(), 1e-3) {
			t.Fatalf("d=%v: Up = %v, want (0, 1, 0) through inflection", d, f.Up)
		}
		if dot := prev.Up.Dot(f.Up); dot < 0.999 {
			t.Fatalf("d=%v: Up discontinuity (dot=%v)", d, dot)
		}
		if dot := prev.Right.Dot(f.Right); dot < 0.95 {
			t.Fatalf("d=%v: Right flip (dot=%v)", d, dot)
		}
		prev = f
	}
}

func TestCurveFrameClosed3DLoopHolonomyAndCustomWorldUp(t *testing.T) {
	// Non-planar 3D closed rollercoaster loop (has non-zero geometric holonomy twist).
	loop := math3d.NewCatmullRom3D(
		[]math3d.Vec3{
			{X: 0, Y: 0, Z: 0},
			{X: 4, Y: 3, Z: -2},
			{X: 6, Y: 6, Z: -6},
			{X: 2, Y: 5, Z: -9},
			{X: -3, Y: 2, Z: -5},
			{X: -2, Y: -1, Z: -1},
		},
		true,
	)

	// Default worldUp: smooth and seamless across t=0 / t=1.
	f0 := loop.FrameAt(0, math3d.Vec3Up())
	f1 := loop.FrameAt(1, math3d.Vec3Up())
	assertOrthonormalFrame(t, "loop[0]", f0)
	assertOrthonormalFrame(t, "loop[1]", f1)

	if !f0.Position.ApproxEqual(f1.Position, 1e-3) ||
		!f0.Tangent.ApproxEqual(f1.Tangent, 1e-3) ||
		!f0.Up.ApproxEqual(f1.Up, 1e-2) ||
		!f0.Right.ApproxEqual(f1.Right, 1e-2) {
		t.Fatalf("closed 3D loop frame seam mismatch:\nf0=%+v\nf1=%+v", f0, f1)
	}

	const N = 120
	prev := f0
	for i := 1; i <= N; i++ {
		u := float32(i) / float32(N)
		f := loop.FrameAt(u, math3d.Vec3Up())
		assertOrthonormalFrame(t, "loop", f)
		if dot := prev.Up.Dot(f.Up); dot < 0.95 {
			t.Fatalf("loop t=%v: Up jumped (dot=%v)", u, dot)
		}
		prev = f
	}

	// FrameAtDistance agrees with FrameAt(DistanceToT(d), Vec3Up()).
	d := 0.37 * loop.Length()
	fd := loop.FrameAtDistance(d)
	ft := loop.FrameAt(loop.DistanceToT(d), math3d.Vec3Up())
	if !fd.Position.ApproxEqual(ft.Position, 1e-4) ||
		!fd.Tangent.ApproxEqual(ft.Tangent, 1e-4) ||
		!fd.Up.ApproxEqual(ft.Up, 1e-4) ||
		!fd.Right.ApproxEqual(ft.Right, 1e-4) {
		t.Fatalf("FrameAtDistance != FrameAt(DistanceToT, Vec3Up): %+v vs %+v", fd, ft)
	}

	// Custom worldUp aligns the initial frame's Up with customUp (projected orthogonal to Tangent(0))
	// and remains seamless on the closed loop.
	customUp := math3d.Vec3{X: 0, Y: 0, Z: 1}
	cf0 := loop.FrameAt(0, customUp)
	cf1 := loop.FrameAt(1, customUp)
	assertOrthonormalFrame(t, "custom[0]", cf0)
	assertOrthonormalFrame(t, "custom[1]", cf1)
	wantU0 := customUp.Sub(cf0.Tangent.Scale(customUp.Dot(cf0.Tangent))).Norm()
	if !cf0.Up.ApproxEqual(wantU0, 1e-3) {
		t.Fatalf("custom worldUp at t=0: Up = %v, want %v", cf0.Up, wantU0)
	}
	if !cf0.Up.ApproxEqual(cf1.Up, 1e-2) {
		t.Fatalf("custom worldUp closed seam mismatch: %v vs %v", cf0.Up, cf1.Up)
	}
}

func TestCurveEdgeCasesAndDegenerates(t *testing.T) {
	// Zero-length degenerate Bézier.
	p := math3d.Vec3{X: 2, Y: 3, Z: 4}
	deg := math3d.NewCubicBezier3D(p, p, p, p)
	if deg.Length() != 0 {
		t.Fatalf("degenerate Length() = %v, want 0", deg.Length())
	}
	if !deg.Point(0.5).ApproxEqual(p, eps) {
		t.Fatalf("degenerate Point(0.5) = %v, want %v", deg.Point(0.5), p)
	}
	if deg.DistanceToT(1) != 0 {
		t.Fatalf("degenerate DistanceToT(1) = %v, want 0", deg.DistanceToT(1))
	}
	assertOrthonormalFrame(t, "degenerate", deg.FrameAt(0.5, math3d.Vec3Up()))

	// Coincident first two control points (P0 == P1 -> P'(0) == 0, limiting tangent still valid).
	cusp := math3d.NewQuadraticBezier3D(
		math3d.Vec3{X: 0, Y: 0, Z: 0},
		math3d.Vec3{X: 0, Y: 0, Z: 0},
		math3d.Vec3{X: 0, Y: 0, Z: -5},
	)
	if got := cusp.Tangent(0); !got.ApproxEqual(math3d.Vec3Forward(), 1e-3) {
		t.Fatalf("coincident P0==P1 Tangent(0) = %v, want -Z", got)
	}

	// Open clamping on t and dist.
	line := math3d.NewQuadraticBezier3D(
		math3d.Vec3{X: 0, Y: 0, Z: 0},
		math3d.Vec3{X: 5, Y: 0, Z: 0},
		math3d.Vec3{X: 10, Y: 0, Z: 0},
	)
	if !line.Point(-1).ApproxEqual(math3d.Vec3Zero(), eps) {
		t.Fatalf("open clamp Point(-1) = %v", line.Point(-1))
	}
	if !line.Point(2).ApproxEqual(math3d.Vec3{X: 10, Y: 0, Z: 0}, eps) {
		t.Fatalf("open clamp Point(2) = %v", line.Point(2))
	}
	if line.DistanceToT(-5) != 0 || line.DistanceToT(100) != 1 {
		t.Fatalf("open clamp DistanceToT failed")
	}

	// Empty and single-point splines never panic.
	emptyH := math3d.NewHermiteSpline3D(nil, nil, false)
	if emptyH.Length() != 0 || emptyH.SegmentCount() != 0 {
		t.Fatalf("empty HermiteSpline3D unexpected state")
	}
	assertOrthonormalFrame(t, "emptyH", emptyH.FrameAt(0.5, math3d.Vec3Up()))

	singleCR := math3d.NewCatmullRom3D([]math3d.Vec3{p}, false)
	if singleCR.Length() != 0 || !singleCR.Point(0.5).ApproxEqual(p, eps) {
		t.Fatalf("single-point CatmullRom3D unexpected state")
	}
	assertOrthonormalFrame(t, "singleCR", singleCR.FrameAtDistance(1))

	// Nil receivers never panic.
	var nilQB *math3d.QuadraticBezier3D
	var nilCB *math3d.CubicBezier3D
	var nilCH *math3d.CubicHermite3D
	var nilHS *math3d.HermiteSpline3D
	var nilCR *math3d.CatmullRom3D
	for _, c := range []math3d.Curve3D{nilQB, nilCB, nilCH, nilHS, nilCR} {
		_ = c.Point(0.5)
		_ = c.Tangent(0.5)
		_ = c.Length()
		_ = c.DistanceToT(1)
		_ = c.PointAtDistance(1)
		_ = c.TangentAtDistance(1)
		assertOrthonormalFrame(t, "nil", c.FrameAt(0.5, math3d.Vec3Up()))
		assertOrthonormalFrame(t, "nilDist", c.FrameAtDistance(1))
		_ = c.Closed()
	}
}

func TestSamplingZeroAllocations(t *testing.T) {
	curves := []struct {
		name  string
		curve math3d.Curve3D
	}{
		{
			"QuadraticBezier3D",
			math3d.NewQuadraticBezier3D(
				math3d.Vec3{X: 0, Y: 0, Z: 0},
				math3d.Vec3{X: 2, Y: 4, Z: -1},
				math3d.Vec3{X: 5, Y: 1, Z: -3},
			),
		},
		{
			"CubicBezier3D",
			math3d.NewCubicBezier3D(
				math3d.Vec3{X: 0, Y: 0, Z: 0},
				math3d.Vec3{X: 1, Y: 3, Z: 0},
				math3d.Vec3{X: 4, Y: 3, Z: -2},
				math3d.Vec3{X: 5, Y: 0, Z: -4},
			),
		},
		{
			"CubicHermite3D",
			math3d.NewCubicHermite3D(
				math3d.Vec3{X: 0, Y: 0, Z: 0},
				math3d.Vec3{X: 2, Y: 3, Z: 0},
				math3d.Vec3{X: 4, Y: 1, Z: -3},
				math3d.Vec3{X: 1, Y: -2, Z: -2},
			),
		},
		{
			"HermiteSpline3D",
			math3d.NewHermiteSpline3D(
				[]math3d.Vec3{{X: 0, Y: 0, Z: 0}, {X: 2, Y: 3, Z: -1}, {X: 5, Y: 1, Z: -4}},
				[]math3d.Vec3{{X: 1, Y: 2, Z: 0}, {X: 3, Y: 0, Z: -2}, {X: 1, Y: -2, Z: -1}},
				true,
			),
		},
		{
			"CatmullRom3D",
			math3d.NewCatmullRom3D(
				[]math3d.Vec3{{X: 0, Y: 0, Z: 0}, {X: 2, Y: 3, Z: -1}, {X: 5, Y: 1, Z: -4}, {X: 3, Y: -2, Z: 1}},
				true,
			),
		},
	}

	var (
		sinkVec  math3d.Vec3
		sinkF32  float32
		sinkFrm  math3d.CurveFrame
		sinkQuat math3d.Quat
		sinkTr   math3d.Transform
		sinkMat  math3d.Mat4
	)

	for _, tc := range curves {
		c := tc.curve
		d := c.Length() * 0.43
		up := math3d.Vec3Up()

		t.Run(tc.name, func(t *testing.T) {
			checks := []struct {
				op string
				fn func()
			}{
				{"Point", func() { sinkVec = c.Point(0.37) }},
				{"Tangent", func() { sinkVec = c.Tangent(0.37) }},
				{"Length", func() { sinkF32 = c.Length() }},
				{"DistanceToT", func() { sinkF32 = c.DistanceToT(d) }},
				{"PointAtDistance", func() { sinkVec = c.PointAtDistance(d) }},
				{"TangentAtDistance", func() { sinkVec = c.TangentAtDistance(d) }},
				{"FrameAt", func() { sinkFrm = c.FrameAt(0.37, up) }},
				{"FrameAtDistance", func() { sinkFrm = c.FrameAtDistance(d) }},
				{"FrameMethods", func() {
					f := c.FrameAtDistance(d)
					sinkQuat = f.Rotation()
					sinkTr = f.Transform()
					sinkMat = f.Mat4()
				}},
			}
			for _, chk := range checks {
				allocs := testing.AllocsPerRun(100, chk.fn)
				if allocs != 0 {
					t.Errorf("%s.%s allocated %.1f times per run, want 0", tc.name, chk.op, allocs)
				}
			}
		})
	}

	_ = sinkVec
	_ = sinkF32
	_ = sinkFrm
	_ = sinkQuat
	_ = sinkTr
	_ = sinkMat
}

func TestCurveFrameWithCoincidentFirstPoints(t *testing.T) {
	// #546: p1 == p0 makes the tangent at t=0 zero; the frame table used to
	// return zero Up/Right vectors for the whole curve.
	p0 := math3d.Vec3{X: 0, Y: 0, Z: 0}
	curve := math3d.NewCubicBezier3D(p0, p0,
		math3d.Vec3{X: 1, Y: 0, Z: 0},
		math3d.Vec3{X: 2, Y: 3, Z: 0})
	for _, tt := range []float32{0, 0.1, 0.5, 1} {
		f := curve.FrameAt(tt, math3d.Vec3Up())
		if f.Tangent.LenSq() < 1e-6 || f.Up.LenSq() < 1e-6 || f.Right.LenSq() < 1e-6 {
			t.Fatalf("frame(%v) = tan:%+v up:%+v right:%+v, want non-zero axes", tt, f.Tangent, f.Up, f.Right)
		}
		if dot := f.Tangent.Dot(f.Up); dot > 1e-4 || dot < -1e-4 {
			t.Fatalf("frame(%v) axes not orthogonal: dot = %v", tt, dot)
		}
	}
}
