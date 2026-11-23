package math3d

import (
	"math"
	"testing"
)

func TestDistanceToTPrecisionIsRelativeToTableInterval(t *testing.T) {
	// 20,000 segments with zero end tangents: inside each segment's first table interval the speed rises from 0, so the linear first guess is off by a large share of the interval. A Newton tolerance of 1e-6 x the whole length is about 0.6 of one interval here and accepted that guess (#546).
	const n = 20001
	points := make([]Vec3, n)
	tangents := make([]Vec3, n)
	for i := range n {
		points[i] = Vec3{X: float32(i) * 0.01}
	}
	c := NewHermiteSpline3D(points, tangents, false)
	core := &c.core
	interval := float64(core.table[1].s - core.table[0].s)
	// Early segments keep t and d well inside float32 precision, so any miss is the solver's.
	for _, seg := range []int{0, 1, 2, 5} {
		lo := seg * core.samplesPerSeg
		target := float64(float32((float64(core.table[lo].s) + float64(core.table[lo+1].s)) / 2))
		tt := core.distanceToT(float32(target))
		u := float64(tt)*float64(len(core.segments)) - float64(seg)
		got := float64(core.table[lo].s) + integrateCubic(core.segments[seg], 0, u)
		if rel := math.Abs(got-target) / interval; rel > 0.01 {
			t.Fatalf("segment %d: arc length at DistanceToT(d) misses d by %.1f%% of a table interval", seg, rel*100)
		}
	}
}

func TestToEulerKeepsRollNearGimbalPole(t *testing.T) {
	for _, off := range []float32{1e-3, 3e-4, 1e-4} {
		pitch := float32(math.Pi/2) - off
		q := QuatFromEuler(pitch, 0.7, 0.3)
		p, y, r := q.ToEuler()
		back := QuatFromEuler(p, y, r)
		if absf(back.Dot(q)) < 1-1e-6 {
			t.Fatalf("pitch pi/2-%v: ToEuler round trip = (%v, %v, %v), quaternion error %v", off, p, y, r, 1-absf(back.Dot(q)))
		}
	}
}

func TestRayHitsTinyTrianglesAndShortDirections(t *testing.T) {
	const s = 1e-4
	r := Ray{Origin: Vec3{X: s / 4, Y: s / 4, Z: 1}, Dir: Vec3{Z: -1}}
	if _, _, _, ok := r.IntersectTriangle(Vec3{}, Vec3{X: s}, Vec3{Y: s}); !ok {
		t.Fatal("ray missed a 1e-4 triangle")
	}
	short := Ray{Origin: Vec3{X: 0.25, Y: 0.25, Z: 1}, Dir: Vec3{Z: -1e-4}}
	if _, _, _, ok := short.IntersectTriangle(Vec3{}, Vec3{X: 1}, Vec3{Y: 1}); !ok {
		t.Fatal("ray with a 1e-4 direction missed a unit triangle")
	}
	if _, ok := short.IntersectPlane(Plane{Normal: Vec3{Z: 1}}); !ok {
		t.Fatal("ray with a 1e-4 direction missed the plane")
	}
}

func TestUniformCatmullRomCoincidentNeighborTangent(t *testing.T) {
	p0 := Vec3{}
	p2 := Vec3{X: 2}
	p3 := Vec3{X: 4}
	// A coincident neighbor (p0 == p1) under uniform knots gives the standard tangent (p2 - p0) / 2.
	seg := cubicFromCatmullRom(p0, p0, p2, p3, 0)
	if got, want := seg.deriv(0), (Vec3{X: 1}); !got.ApproxEqual(want, 1e-5) {
		t.Fatalf("uniform tangent at a coincident neighbor = %v, want %v", got, want)
	}
}

func TestFrustumKeepsPointsOnFarPlane(t *testing.T) {
	view := LookAt(Vec3{X: 3, Y: 2, Z: 7}, Vec3{X: 3, Y: 2, Z: 6}, Vec3Up())
	// Far/near ratios up to 1e4; beyond that a float32 matrix cannot place its far plane this precisely.
	for _, far := range []float32{100, 1000} {
		vp := Perspective(1.0, 16.0/9, 0.1, far).Mul(view)
		for _, f := range []Frustum{FrustumFromMat4(vp), FrustumFromMat4NO(vp)} {
			for i := range 50 {
				x := float32(i-25) / 100 * far
				p := Vec3{X: 3 + x*0.5, Y: 2, Z: 7 - far}
				if !f.ContainsPoint(p) {
					t.Fatalf("far %v: point %v on the far plane was culled", far, p)
				}
				if !f.IntersectsAABB(AABB{Min: p, Max: p}) {
					t.Fatalf("far %v: degenerate box %v on the far plane was culled", far, p)
				}
			}
		}
	}
}

func TestHermiteMissingTangentsKeepMoving(t *testing.T) {
	points := []Vec3{{}, {X: 1, Y: 1}, {X: 2}, {X: 3, Y: 1}}
	c := NewHermiteSpline3D(points, nil, false)
	for i := range points {
		tt := float32(i) / float32(len(points)-1)
		if d := c.Derivative(tt); d.LenSq() < 1e-6 {
			t.Fatalf("missing tangent at point %d stopped the curve (derivative %v)", i, d)
		}
	}
	// The tangent at points[1] is the central difference (points[2] - points[0]) / 2 in segment parameter u.
	got := c.core.segments[1].deriv(0)
	if want := (Vec3{X: 1}); !got.ApproxEqual(want, 1e-5) {
		t.Fatalf("interior tangent = %v, want %v", got, want)
	}
}
