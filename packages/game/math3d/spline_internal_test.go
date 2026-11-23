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
