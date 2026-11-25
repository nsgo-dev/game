package physics_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/nsgo/core"
)

func pts(xy ...float32) []core.Point {
	out := make([]core.Point, 0, len(xy)/2)
	for i := 0; i+1 < len(xy); i += 2 {
		out = append(out, core.Point{X: core.Points(xy[i]), Y: core.Points(xy[i+1])})
	}
	return out
}

func mustPolygon(t testing.TB, radius float32, xy ...float32) physics.Polygon {
	t.Helper()
	p, ok := physics.NewPolygon(pts(xy...), radius)
	if !ok {
		t.Fatalf("NewPolygon(%v, %v) failed", xy, radius)
	}
	return p
}

func signedArea(p physics.Polygon) float64 {
	var a float64
	for i := range p.Count {
		v, w := p.Vertices[i], p.Vertices[(i+1)%p.Count]
		a += float64(v.X)*float64(w.Y) - float64(w.X)*float64(v.Y)
	}
	return a * 0.5
}

func TestNewPolygonHull(t *testing.T) {
	// Unordered square corners plus an interior point, a duplicate and a point on an edge.
	p := mustPolygon(t, 0, 10, 10, 0, 0, 5, 5, 10, 0, 0, 10, 10, 10, 5, 0)
	if p.Count != 4 {
		t.Fatalf("Count = %d, want 4: %+v", p.Count, p.Vertices[:p.Count])
	}
	if a := signedArea(p); math.Abs(a-100) > 1e-9 {
		t.Fatalf("signed area = %v, want +100 (clockwise on screen)", a)
	}
	b := p.Bounds()
	if b != (physics.Box{X: 0, Y: 0, W: 10, H: 10}) {
		t.Fatalf("Bounds = %+v", b)
	}

	bad := []struct {
		name   string
		points []core.Point
		radius float32
	}{
		{"two points", pts(0, 0, 1, 1), 0},
		{"collinear", pts(0, 0, 5, 5, 10, 10), 0},
		{"nine-gon", regularPolygonPoints(9, 10), 0},
		{"negative radius", pts(0, 0, 1, 0, 0, 1), -1},
		{"nan radius", pts(0, 0, 1, 0, 0, 1), float32(math.NaN())},
		{"nan point", pts(0, 0, 1, 0, float32(math.NaN()), 1), 0},
		{"inf point", pts(0, 0, 1, 0, float32(math.Inf(1)), 1), 0},
	}
	for _, tc := range bad {
		if _, ok := physics.NewPolygon(tc.points, tc.radius); ok {
			t.Errorf("%s: NewPolygon succeeded, want failure", tc.name)
		}
	}
	if p8, ok := physics.NewPolygon(regularPolygonPoints(8, 10), 0); !ok || p8.Count != 8 {
		t.Fatalf("octagon: ok=%v count=%d", ok, p8.Count)
	}
}

func regularPolygonPoints(n int, r float64) []core.Point {
	out := make([]core.Point, n)
	for i := range out {
		a := 2 * math.Pi * float64(i) / float64(n)
		out[i] = core.Point{X: core.Points(r * math.Cos(a)), Y: core.Points(r * math.Sin(a))}
	}
	return out
}

func TestNewBoxPolygon(t *testing.T) {
	p := physics.NewBoxPolygon(5, 5, 10, 4, 0)
	if p.Count != 4 || signedArea(p) <= 0 {
		t.Fatalf("NewBoxPolygon = %+v", p)
	}
	if b := p.Bounds(); b != (physics.Box{X: 0, Y: 3, W: 10, H: 4}) {
		t.Fatalf("Bounds = %+v", b)
	}
	r := physics.NewBoxPolygon(0, 0, 2, 2, math.Pi/4)
	b := r.Bounds()
	if !near(b.W, float32(2*math.Sqrt2), 1e-5) || !near(b.H, float32(2*math.Sqrt2), 1e-5) {
		t.Fatalf("rotated bounds = %+v", b)
	}
}

func TestConvexShapeBasics(t *testing.T) {
	square := physics.PolygonShape(mustPolygon(t, 0, 0, 0, 10, 0, 10, 10, 0, 10))
	rounded := physics.PolygonShape(mustPolygon(t, 1, 0, 0, 10, 0, 10, 10, 0, 10))
	capsule := physics.CapsuleShape(physics.Capsule{X1: 0, Y1: 0, X2: 10, Y2: 0, R: 2})
	segment := physics.SegmentShape(physics.Segment{X1: 0, Y1: 0, X2: 10, Y2: 0})
	chain := physics.ChainShape(pts(0, 0, 10, 0, 20, 5), false)

	cases := []struct {
		name   string
		s      physics.Shape
		bounds physics.Box
		in     [][2]float32
		out    [][2]float32
	}{
		{"polygon", square, physics.Box{X: 0, Y: 0, W: 10, H: 10}, [][2]float32{{5, 5}, {0, 0}, {10, 5}}, [][2]float32{{-0.1, 5}, {11, 11}}},
		{"rounded polygon", rounded, physics.Box{X: -1, Y: -1, W: 12, H: 12}, [][2]float32{{-1, 5}, {10.7, 10.7}}, [][2]float32{{10.8, 10.8}, {-1.1, 5}}},
		{"capsule", capsule, physics.Box{X: -2, Y: -2, W: 14, H: 4}, [][2]float32{{5, 2}, {-2, 0}, {11, 1}}, [][2]float32{{5, 2.1}, {12.1, 0}}},
		{"segment", segment, physics.Box{X: 0, Y: 0, W: 10, H: 0}, nil, [][2]float32{{5, 0}}},
		{"chain", chain, physics.Box{X: 0, Y: 0, W: 20, H: 5}, nil, [][2]float32{{5, 0}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.s.Empty() {
				t.Fatal("Empty() = true")
			}
			if b := tc.s.Bounds(); b != tc.bounds {
				t.Fatalf("Bounds = %+v, want %+v", b, tc.bounds)
			}
			for _, p := range tc.in {
				if !tc.s.Contains(p[0], p[1]) {
					t.Errorf("Contains(%v) = false", p)
				}
			}
			for _, p := range tc.out {
				if tc.s.Contains(p[0], p[1]) {
					t.Errorf("Contains(%v) = true", p)
				}
			}
			moved := tc.s.Translated(3, -2)
			if mb := moved.Bounds(); !near(mb.X, tc.bounds.X+3, 1e-5) || !near(mb.Y, tc.bounds.Y-2, 1e-5) || !near(mb.W, tc.bounds.W, 1e-5) || !near(mb.H, tc.bounds.H, 1e-5) {
				t.Fatalf("Translated bounds = %+v", mb)
			}
		})
	}

	empties := []physics.Shape{
		physics.PolygonShape(physics.Polygon{}),
		physics.CapsuleShape(physics.Capsule{X2: 5, R: 0}),
		physics.SegmentShape(physics.Segment{X1: 1, Y1: 1, X2: 1, Y2: 1}),
		physics.ChainShape(pts(0, 0), false),
		physics.ChainShape(pts(0, 0, 1, 0), true),
		{Kind: physics.ShapeChain},
	}
	for i, s := range empties {
		if !s.Empty() {
			t.Errorf("empties[%d] not empty", i)
		}
		if s.Bounds() != (physics.Box{}) {
			t.Errorf("empties[%d] bounds = %+v", i, s.Bounds())
		}
		if physics.TestShapes(s, square).Hit || physics.TestShapes(square, s).Hit {
			t.Errorf("empties[%d] collided", i)
		}
	}

	// ChainShape copies its points.
	src := pts(0, 0, 10, 0)
	c := physics.ChainShape(src, false)
	src[1].X = 99
	if c.Chain.Points[1].X != 10 {
		t.Fatal("ChainShape aliases its input")
	}
}

func TestShapeMassProperties(t *testing.T) {
	square := physics.PolygonShape(mustPolygon(t, 0, 0, 0, 4, 0, 4, 2, 0, 2))
	box := physics.BoxShape(physics.Box{W: 4, H: 2})
	if a := square.Area(); !near(a, 8, 1e-5) {
		t.Fatalf("polygon area = %v", a)
	}
	if x, y := square.Centroid(); !near(x, 2, 1e-5) || !near(y, 1, 1e-5) {
		t.Fatalf("polygon centroid = (%v, %v)", x, y)
	}
	if i, want := square.MomentOfInertia(3), box.MomentOfInertia(3); !near(i, want, 1e-4) {
		t.Fatalf("polygon inertia = %v, want box's %v", i, want)
	}
	tri := physics.PolygonShape(mustPolygon(t, 0, 0, 0, 6, 0, 0, 3))
	if x, y := tri.Centroid(); !near(x, 2, 1e-5) || !near(y, 1, 1e-5) {
		t.Fatalf("triangle centroid = (%v, %v)", x, y)
	}
	// A right triangle with legs 6 and 3: I about the centroid = m (a^2 + b^2) / 18.
	if i := tri.MomentOfInertia(2); !near(i, 2*(36+9)/18.0, 1e-4) {
		t.Fatalf("triangle inertia = %v", i)
	}
	// A rounded polygon is heavier in area and inertia than its core.
	rounded := physics.PolygonShape(mustPolygon(t, 0.5, 0, 0, 4, 0, 4, 2, 0, 2))
	if rounded.Area() <= square.Area() || rounded.MomentOfInertia(3) <= square.MomentOfInertia(3) {
		t.Fatalf("rounded area %v inertia %v not above the core's", rounded.Area(), rounded.MomentOfInertia(3))
	}

	capsule := physics.CapsuleShape(physics.Capsule{X1: 0, Y1: 0, X2: 10, Y2: 0, R: 1})
	if a := capsule.Area(); !near(a, float32(math.Pi+20), 1e-4) {
		t.Fatalf("capsule area = %v", a)
	}
	if x, y := capsule.Centroid(); !near(x, 5, 1e-5) || !near(y, 0, 1e-5) {
		t.Fatalf("capsule centroid = (%v, %v)", x, y)
	}
	// A capsule of zero length is a circle.
	dot := physics.CapsuleShape(physics.Capsule{X1: 3, Y1: 3, X2: 3, Y2: 3, R: 2})
	circle := physics.CircleShape(physics.Circle{X: 3, Y: 3, R: 2})
	if i, want := dot.MomentOfInertia(5), circle.MomentOfInertia(5); !near(i, want, 1e-4) {
		t.Fatalf("zero-length capsule inertia = %v, want %v", i, want)
	}
	// A long thin capsule approaches a rod: m L^2 / 12.
	rod := physics.CapsuleShape(physics.Capsule{X1: 0, Y1: 0, X2: 100, Y2: 0, R: 0.01})
	if i := rod.MomentOfInertia(1); math.Abs(float64(i)-100*100/12.0)/(100*100/12.0) > 1e-3 {
		t.Fatalf("rod inertia = %v", i)
	}

	seg := physics.SegmentShape(physics.Segment{X1: 0, Y1: 0, X2: 10, Y2: 4})
	if seg.Area() != 0 || seg.MomentOfInertia(1) != 0 {
		t.Fatal("segment has area or inertia")
	}
	if x, y := seg.Centroid(); x != 5 || y != 2 {
		t.Fatalf("segment centroid = (%v, %v)", x, y)
	}
	chain := physics.ChainShape(pts(0, 0, 10, 0, 10, 30), false)
	if x, y := chain.Centroid(); !near(x, (10*5+30*10)/40.0, 1e-5) || !near(y, 30*15/40.0, 1e-5) {
		t.Fatalf("chain centroid = (%v, %v)", x, y)
	}
}

type pairCase struct {
	name                 string
	a, b                 physics.Shape
	hit                  bool
	nx, ny, depth        float32
	skipSeparationVerify bool
}

func TestConvexShapePairs(t *testing.T) {
	square := physics.PolygonShape(mustPolygon(t, 0, 0, 0, 10, 0, 10, 10, 0, 10))
	poly := func(x0, y0, x1, y1 float32) physics.Shape {
		return physics.PolygonShape(mustPolygon(t, 0, x0, y0, x1, y0, x1, y1, x0, y1))
	}
	roundedSquare := physics.PolygonShape(mustPolygon(t, 1, 0, 0, 10, 0, 10, 10, 0, 10))
	circle := func(x, y, r float32) physics.Shape { return physics.CircleShape(physics.Circle{X: x, Y: y, R: r}) }
	box := func(x, y, w, h float32) physics.Shape { return physics.BoxShape(physics.Box{X: x, Y: y, W: w, H: h}) }
	capsule := func(x1, y1, x2, y2, r float32) physics.Shape {
		return physics.CapsuleShape(physics.Capsule{X1: x1, Y1: y1, X2: x2, Y2: y2, R: r})
	}
	segment := func(x1, y1, x2, y2 float32) physics.Shape {
		return physics.SegmentShape(physics.Segment{X1: x1, Y1: y1, X2: x2, Y2: y2})
	}
	flatChain := physics.ChainShape(pts(0, 0, 10, 0, 20, 0), false)
	ledge := physics.ChainShape(pts(0, 0, 10, 0, 10, 10), false)
	room := physics.ChainShape(pts(0, 0, 10, 0, 10, 10, 0, 10), true)
	baseCapsule := capsule(0, 0, 10, 0, 2)
	baseSegment := segment(0, 0, 10, 0)
	s2 := float32(math.Sqrt2 / 2)

	cases := []pairCase{
		// Polygon and polygon.
		{name: "polygon/polygon separated", a: poly(20, 0, 30, 10), b: square},
		{name: "polygon/polygon touching edge", a: poly(10, 0, 20, 10), b: square},
		{name: "polygon/polygon touching corner", a: poly(10, 10, 20, 20), b: square},
		{name: "polygon/polygon overlap", a: poly(8, 2, 18, 8), b: square, hit: true, nx: 1, ny: 0, depth: 2},
		{name: "polygon/polygon contained", a: poly(2, 3, 4, 9), b: square, hit: true, nx: -1, ny: 0, depth: 4},
		{name: "polygon/rotated polygon", a: physics.PolygonShape(physics.NewBoxPolygon(5, 12, 4, 4, math.Pi/4)), b: square, hit: true, nx: 0, ny: 1, depth: float32(2*math.Sqrt2 - 2)},
		// Polygon and box.
		{name: "box/polygon overlap", a: box(-3, 2, 4, 4), b: square, hit: true, nx: -1, ny: 0, depth: 1},
		{name: "box/polygon touching", a: box(-4, 2, 4, 4), b: square},
		// Polygon and circle.
		{name: "circle/polygon face", a: circle(13, 5, 4), b: square, hit: true, nx: 1, ny: 0, depth: 1},
		{name: "circle/polygon touching", a: circle(14, 5, 4), b: square},
		{name: "circle/polygon corner", a: circle(13, 13, 5), b: square, hit: true, nx: s2, ny: s2, depth: float32(5 - 3*math.Sqrt2)},
		{name: "circle/polygon corner separated", a: circle(14, 14, 5), b: square},
		{name: "circle/polygon contained", a: circle(5, 2, 1), b: square, hit: true, nx: 0, ny: -1, depth: 3},
		// Rounded polygon.
		{name: "circle/rounded polygon face", a: circle(13, 5, 3), b: roundedSquare, hit: true, nx: 1, ny: 0, depth: 1},
		{name: "circle/rounded polygon corner miss", a: circle(11.5, 11.5, 1), b: roundedSquare},
		{name: "circle/rounded polygon corner", a: circle(11, 11, 1), b: roundedSquare, hit: true, nx: s2, ny: s2, depth: float32(2 - math.Sqrt2)},
		{name: "box/rounded polygon", a: box(10.5, 0, 5, 5), b: roundedSquare, hit: true, nx: 1, ny: 0, depth: 0.5},
		{name: "rounded/rounded polygon", a: physics.PolygonShape(mustPolygon(t, 1, 11, 0, 21, 0, 21, 10, 11, 10)), b: roundedSquare, hit: true, nx: 1, ny: 0, depth: 1},
		// Capsule.
		{name: "circle/capsule side", a: circle(5, 5, 4), b: baseCapsule, hit: true, nx: 0, ny: 1, depth: 1},
		{name: "circle/capsule end", a: circle(14, 0, 3), b: baseCapsule, hit: true, nx: 1, ny: 0, depth: 1},
		{name: "circle/capsule touching", a: circle(5, 6, 4), b: baseCapsule},
		{name: "circle/capsule contained", a: circle(5, 0.5, 0.5), b: baseCapsule, hit: true, nx: 0, ny: 1, depth: 2},
		{name: "box/capsule", a: box(3, 1, 4, 4), b: baseCapsule, hit: true, nx: 0, ny: 1, depth: 1},
		{name: "polygon/capsule", a: poly(3, 1, 7, 5), b: baseCapsule, hit: true, nx: 0, ny: 1, depth: 1},
		{name: "capsule/capsule parallel", a: capsule(0, 3, 10, 3, 2), b: baseCapsule, hit: true, nx: 0, ny: 1, depth: 1},
		{name: "capsule/capsule crossing", a: capsule(4, -5, 4, 5, 1), b: baseCapsule, hit: true, nx: -1, ny: 0, depth: 7},
		{name: "capsule/capsule end to end", a: capsule(13, 0, 20, 0, 2), b: baseCapsule, hit: true, nx: 1, ny: 0, depth: 1},
		{name: "capsule/capsule separated", a: capsule(0, 5, 10, 5, 1), b: baseCapsule},
		// Segment.
		{name: "circle/segment above", a: circle(5, 1, 2), b: baseSegment, hit: true, nx: 0, ny: 1, depth: 1},
		{name: "circle/segment below", a: circle(5, -1, 2), b: baseSegment, hit: true, nx: 0, ny: -1, depth: 1},
		{name: "circle/segment touching", a: circle(5, 2, 2), b: baseSegment},
		{name: "circle/segment end", a: circle(11, 0, 2), b: baseSegment, hit: true, nx: 1, ny: 0, depth: 1},
		{name: "box/segment straddling", a: box(2, -1, 4, 3), b: baseSegment, hit: true, nx: 0, ny: 1, depth: 1},
		{name: "box/segment contained", a: box(-5, -4, 20, 10), b: baseSegment, hit: true, nx: 0, ny: 1, depth: 4},
		{name: "polygon/segment", a: poly(2, -3, 6, 1), b: baseSegment, hit: true, nx: 0, ny: -1, depth: 1},
		{name: "capsule/segment", a: capsule(2, 1, 8, 1, 2), b: baseSegment, hit: true, nx: 0, ny: 1, depth: 1},
		{name: "segment/segment crossing", a: segment(5, -5, 5, 5), b: baseSegment},
		{name: "segment/segment collinear", a: segment(5, 0, 15, 0), b: baseSegment},
		// Chain.
		{name: "circle/chain seam", a: circle(10, 1, 2), b: flatChain, hit: true, nx: 0, ny: 1, depth: 1},
		{name: "circle/chain below", a: circle(5, -1, 2), b: flatChain, hit: true, nx: 0, ny: -1, depth: 1},
		{name: "circle/chain open end", a: circle(21, 0, 2), b: flatChain, hit: true, nx: 1, ny: 0, depth: 1},
		{name: "circle/chain convex vertex", a: circle(11, -1, 2), b: ledge, hit: true, nx: s2, ny: -s2, depth: float32(2 - math.Sqrt2)},
		{name: "box/chain seam", a: box(8, -1.5, 4, 2), b: flatChain, hit: true, nx: 0, ny: -1, depth: 0.5},
		{name: "capsule/chain seam", a: capsule(6, -1.5, 14, -1.5, 2), b: flatChain, hit: true, nx: 0, ny: -1, depth: 0.5},
		{name: "polygon/chain", a: poly(12, -1, 16, 3), b: flatChain, hit: true, nx: 0, ny: 1, depth: 1},
		{name: "circle/chain loop wall", a: circle(5, 1, 2), b: room, hit: true, nx: 0, ny: 1, depth: 1},
		{name: "circle/chain loop hollow", a: circle(5, 5, 1), b: room},
		{name: "chain/chain", a: flatChain, b: room},
		{name: "segment/chain", a: segment(5, -5, 5, 5), b: flatChain},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := physics.TestShapes(tc.a, tc.b)
			rev := physics.TestShapes(tc.b, tc.a)
			if !tc.hit {
				if got.Hit || rev.Hit {
					t.Fatalf("expected no hit, got %+v / reversed %+v", got, rev)
				}
				return
			}
			expectContact(t, got, tc.nx, tc.ny, tc.depth)
			expectContact(t, rev, -tc.nx, -tc.ny, tc.depth)
			if !tc.b.Contains(got.PointX, got.PointY) && tc.b.Kind != physics.ShapeSegment && tc.b.Kind != physics.ShapeChain {
				// The contact point lies between the two surfaces, so within the rounded extent of b.
				bb := tc.b.Bounds()
				if got.PointX < bb.X-tc.depth || got.PointX > bb.X+bb.W+tc.depth || got.PointY < bb.Y-tc.depth || got.PointY > bb.Y+bb.H+tc.depth {
					t.Fatalf("contact point (%v, %v) far from b %+v", got.PointX, got.PointY, bb)
				}
			}
			expectSeparated(t, tc.a, tc.b, got)
		})
	}
}

// TestConvexShapesDeterministic checks that the same inputs give bit-identical contacts.
func TestConvexShapesDeterministic(t *testing.T) {
	a := physics.PolygonShape(physics.NewBoxPolygon(5.3, 11.1, 4.2, 3.9, 0.37))
	b := physics.CapsuleShape(physics.Capsule{X1: 0, Y1: 12, X2: 12, Y2: 13, R: 1.5})
	c := physics.ChainShape(pts(0, 14, 4, 13.5, 9, 13.8, 14, 14), false)
	first := [3]physics.Contact{physics.TestShapes(a, b), physics.TestShapes(a, c), physics.TestShapes(b, c)}
	for range 100 {
		again := [3]physics.Contact{physics.TestShapes(a, b), physics.TestShapes(a, c), physics.TestShapes(b, c)}
		if again != first {
			t.Fatalf("contacts changed between runs: %+v vs %+v", again, first)
		}
	}
	for i, ct := range first {
		if !ct.Hit {
			t.Fatalf("contact %d missed", i)
		}
	}
}

func TestConvexShapeRaycasts(t *testing.T) {
	square := physics.PolygonShape(mustPolygon(t, 0, 0, 0, 10, 0, 10, 10, 0, 10))
	rounded := physics.PolygonShape(mustPolygon(t, 2, 0, 0, 10, 0, 10, 10, 0, 10))
	capsule := physics.CapsuleShape(physics.Capsule{X1: 0, Y1: 0, X2: 10, Y2: 0, R: 2})
	segment := physics.SegmentShape(physics.Segment{X1: 0, Y1: -5, X2: 0, Y2: 5})
	chain := physics.ChainShape(pts(0, 10, 10, 10, 20, 0), false)

	cases := []struct {
		name     string
		shape    physics.Shape
		from, to core.Point
		hit      bool
		t        float32
		px, py   float32
		nx, ny   float32
	}{
		{"polygon face", square, core.Point{X: -10, Y: 5}, core.Point{X: 10, Y: 5}, true, 0.5, 0, 5, -1, 0},
		{"polygon miss", square, core.Point{X: -10, Y: 15}, core.Point{X: 20, Y: 15}, false, 0, 0, 0, 0, 0},
		{"polygon from inside", square, core.Point{X: 5, Y: 5}, core.Point{X: 20, Y: 5}, true, 0, 5, 5, 0, 0},
		{"polygon leaving an edge", square, core.Point{X: 10, Y: 5}, core.Point{X: 20, Y: 5}, false, 0, 0, 0, 0, 0},
		{"polygon short", square, core.Point{X: -10, Y: 5}, core.Point{X: -1, Y: 5}, false, 0, 0, 0, 0, 0},
		{"rounded face", rounded, core.Point{X: 5, Y: -10}, core.Point{X: 5, Y: 10}, true, 0.4, 5, -2, 0, -1},
		{"rounded corner", rounded, core.Point{X: -10, Y: -10}, core.Point{X: 0, Y: 0}, true, float32(1 - 2/(10*math.Sqrt2)), float32(-math.Sqrt2), float32(-math.Sqrt2), float32(-math.Sqrt2 / 2), float32(-math.Sqrt2 / 2)},
		{"rounded from inside", rounded, core.Point{X: -1, Y: 5}, core.Point{X: -10, Y: 5}, true, 0, -1, 5, 0, 0},
		{"capsule side", capsule, core.Point{X: 5, Y: 10}, core.Point{X: 5, Y: 0}, true, 0.8, 5, 2, 0, 1},
		{"capsule end", capsule, core.Point{X: 20, Y: 0}, core.Point{X: 10, Y: 0}, true, 0.8, 12, 0, 1, 0},
		{"segment from left", segment, core.Point{X: -10, Y: 0}, core.Point{X: 10, Y: 0}, true, 0.5, 0, 0, -1, 0},
		{"segment from right", segment, core.Point{X: 10, Y: 1}, core.Point{X: -10, Y: 1}, true, 0.5, 0, 1, 1, 0},
		{"segment parallel", segment, core.Point{X: 0, Y: -10}, core.Point{X: 0, Y: 10}, false, 0, 0, 0, 0, 0},
		{"chain nearest segment", chain, core.Point{X: 5, Y: -10}, core.Point{X: 5, Y: 30}, true, 0.5, 5, 10, 0, -1},
		{"chain second segment", chain, core.Point{X: 15, Y: -5}, core.Point{X: 15, Y: 15}, true, 0.5, 15, 5, float32(-math.Sqrt2 / 2), float32(-math.Sqrt2 / 2)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := &physics.World{}
			w.Add(&physics.Body{Shape: tc.shape, Tag: "s"})
			h, ok := w.RaycastFirst(tc.from, tc.to, physics.QueryBodies)
			if ok != tc.hit {
				t.Fatalf("hit = %v (%+v), want %v", ok, h, tc.hit)
			}
			if !ok {
				return
			}
			if h.Body == nil || h.Body.Tag != "s" {
				t.Fatalf("hit body = %+v", h.Body)
			}
			if !near(h.T, tc.t, 1e-4) || !near(float32(h.Point.X), tc.px, 1e-3) || !near(float32(h.Point.Y), tc.py, 1e-3) || !near(h.NormalX, tc.nx, 1e-4) || !near(h.NormalY, tc.ny, 1e-4) {
				t.Fatalf("hit = T %v at (%v, %v) normal (%v, %v); want T %v at (%v, %v) normal (%v, %v)", h.T, h.Point.X, h.Point.Y, h.NormalX, h.NormalY, tc.t, tc.px, tc.py, tc.nx, tc.ny)
			}
		})
	}
}

// TestSharpPolygonRaycastMatchesBox checks a polygon built from a box casts exactly like the box.
func TestSharpPolygonRaycastMatchesBox(t *testing.T) {
	b := physics.Box{X: 3, Y: 4, W: 12, H: 7}
	p := physics.PolygonShape(mustPolygon(t, 0, b.X, b.Y, b.X+b.W, b.Y, b.X+b.W, b.Y+b.H, b.X, b.Y+b.H))
	for i := range 64 {
		a := 2 * math.Pi * float64(i) / 64
		from := core.Point{X: core.Points(9 + 20*math.Cos(a)), Y: core.Points(7.5 + 20*math.Sin(a))}
		to := core.Point{X: core.Points(9 - 3*math.Cos(a)), Y: core.Points(7 - 2*math.Sin(a))}
		wb := &physics.World{Bodies: []*physics.Body{{Shape: physics.BoxShape(b)}}}
		wp := &physics.World{Bodies: []*physics.Body{{Shape: p}}}
		hb, okB := wb.RaycastFirst(from, to, physics.QueryBodies)
		hp, okP := wp.RaycastFirst(from, to, physics.QueryBodies)
		if okB != okP || !near(hb.T, hp.T, 1e-5) || hb.NormalX != hp.NormalX || hb.NormalY != hp.NormalY {
			t.Fatalf("ray %d: box %v %+v, polygon %v %+v", i, okB, hb, okP, hp)
		}
	}
}

func TestWorldQueriesAcceptConvexShapes(t *testing.T) {
	ramp := &physics.Body{Tag: "ramp", Shape: physics.PolygonShape(mustPolygon(t, 0, 0, 100, 100, 100, 100, 50))}
	pill := &physics.Body{Tag: "pill", Shape: physics.CapsuleShape(physics.Capsule{X1: 150, Y1: 50, X2: 150, Y2: 80, R: 5})}
	ledge := &physics.Body{Tag: "ledge", Shape: physics.SegmentShape(physics.Segment{X1: 200, Y1: 60, X2: 260, Y2: 60})}
	hills := &physics.Body{Tag: "hills", Shape: physics.ChainShape(pts(300, 100, 340, 80, 380, 100), false)}
	w := &physics.World{}
	w.Add(ramp, pill, ledge, hills)

	if got := w.QueryPoint(90, 95); len(got) != 1 || got[0] != ramp {
		t.Fatalf("QueryPoint in ramp = %v", got)
	}
	if got := w.QueryPoint(10, 60); len(got) != 0 {
		t.Fatalf("QueryPoint above the ramp's slope = %v", got)
	}
	if got := w.QueryPoint(150, 84); len(got) != 1 || got[0] != pill {
		t.Fatalf("QueryPoint in capsule cap = %v", got)
	}
	if got := w.QueryPoint(230, 60); len(got) != 0 {
		t.Fatalf("QueryPoint on a segment = %v", got)
	}

	probe := physics.CircleShape(physics.Circle{X: 230, Y: 62, R: 4})
	hits := w.QueryOverlap(probe)
	if len(hits) != 1 || hits[0].Body != ledge || !near(hits[0].Contact.NormalY, 1, 1e-5) || !near(hits[0].Contact.Depth, 2, 1e-5) {
		t.Fatalf("QueryOverlap on ledge = %+v", hits)
	}
	wide := physics.BoxShape(physics.Box{X: 90, Y: 40, W: 300, H: 50})
	hits = w.QueryOverlap(wide)
	var tags []string
	for _, h := range hits {
		tags = append(tags, h.Body.Tag)
	}
	if len(tags) != 4 || tags[0] != "ramp" || tags[1] != "pill" || tags[2] != "ledge" || tags[3] != "hills" {
		t.Fatalf("QueryOverlap order = %v", tags)
	}

	hit, ok := w.RaycastFirst(core.Point{X: 340, Y: 0}, core.Point{X: 340, Y: 200}, physics.QueryBodies)
	if !ok || hit.Body != hills || !near(float32(hit.Point.Y), 80, 1e-4) {
		t.Fatalf("RaycastFirst onto chain = %+v %v", hit, ok)
	}
	all := w.Raycast(core.Point{X: 0, Y: 70}, core.Point{X: 400, Y: 70}, physics.QueryBodies)
	if len(all) != 2 || all[0].Body != ramp || all[1].Body != pill {
		t.Fatalf("Raycast across = %+v", all)
	}
}

func TestTriggerWithConvexShapes(t *testing.T) {
	var entered, exited []string
	w := &physics.World{}
	zone := &physics.Trigger{
		Shape:   physics.PolygonShape(mustPolygon(t, 0, 0, 0, 20, 0, 10, 20)),
		OnEnter: func(b *physics.Body) { entered = append(entered, b.Tag) },
		OnExit:  func(b *physics.Body) { exited = append(exited, b.Tag) },
	}
	w.AddTrigger(zone)
	pill := &physics.Body{Tag: "pill", Shape: physics.CapsuleShape(physics.Capsule{X1: -20, Y1: 5, X2: -10, Y2: 5, R: 2})}
	w.Add(pill)
	w.StepTriggers()
	pill.Translate(15, 0)
	w.StepTriggers()
	pill.Translate(40, 0)
	w.StepTriggers()
	if len(entered) != 1 || len(exited) != 1 {
		t.Fatalf("entered %v exited %v", entered, exited)
	}
}

func TestMoveAndSlideKeepsConvexShape(t *testing.T) {
	grid := physics.NewGrid(10, 10, 16, 16)
	grid.FillRect(0, 8, 10, 1, physics.TileSolid)
	shapes := []physics.Shape{
		physics.PolygonShape(mustPolygon(t, 0, 30, 80, 42, 80, 36, 96)),
		physics.CapsuleShape(physics.Capsule{X1: 36, Y1: 80, X2: 36, Y2: 96, R: 6}),
	}
	for _, s := range shapes {
		b := &physics.Body{Shape: s, VY: 600}
		res := physics.MoveAndSlide(b, grid, 100*1e6)
		bb := b.Bounds()
		if !res.Grounded || !near(bb.Y+bb.H, 128, 1e-3) {
			t.Fatalf("%v: grounded %v bottom %v", s.Kind, res.Grounded, bb.Y+bb.H)
		}
		if b.Shape.Kind != s.Kind || !near(bb.W, s.Bounds().W, 1e-4) || !near(bb.H, s.Bounds().H, 1e-4) {
			t.Fatalf("%v: shape changed to %+v", s.Kind, b.Shape)
		}
	}
}

func FuzzTestShapesConvex(f *testing.F) {
	f.Add(uint8(2), uint8(3), float32(1), float32(2), float32(0.5), float32(3), float32(0.2), float32(1))
	f.Add(uint8(4), uint8(5), float32(-3), float32(0), float32(0), float32(6), float32(1.5), float32(0))
	f.Add(uint8(6), uint8(2), float32(0), float32(0), float32(0), float32(0), float32(0), float32(0))
	f.Fuzz(func(t *testing.T, ka, kb uint8, x, y, angle, size, radius, extra float32) {
		for _, v := range []float32{x, y, angle, size, radius, extra} {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || math.Abs(float64(v)) > 1e4 {
				return
			}
		}
		a := fuzzShape(ka, x, y, angle, size, radius, extra)
		b := fuzzShape(kb, 1, 2, -angle*0.5, 4, radius*0.5, 2)
		c := physics.TestShapes(a, b)
		r := physics.TestShapes(b, a)
		for _, ct := range []physics.Contact{c, r} {
			if !ct.Hit {
				continue
			}
			l := math.Hypot(float64(ct.NormalX), float64(ct.NormalY))
			if math.IsNaN(l) || math.Abs(l-1) > 1e-3 || !(ct.Depth > 0) || math.IsInf(float64(ct.Depth), 0) || math.IsNaN(float64(ct.PointX)) || math.IsNaN(float64(ct.PointY)) {
				t.Fatalf("bad contact %+v for %+v / %+v", ct, a, b)
			}
		}
		w := &physics.World{Bodies: []*physics.Body{{Shape: b}}}
		for _, h := range w.Raycast(core.Point{X: core.Points(x), Y: core.Points(y)}, core.Point{X: core.Points(extra), Y: core.Points(size)}, physics.QueryBodies) {
			if math.IsNaN(float64(h.T)) || h.T < 0 || h.T > 1 || math.IsNaN(float64(h.NormalX)) {
				t.Fatalf("bad raycast hit %+v", h)
			}
		}
	})
}

func fuzzShape(kind uint8, x, y, angle, size, radius, extra float32) physics.Shape {
	size = float32(math.Abs(float64(size))) + 0.01
	radius = float32(math.Abs(float64(radius)))
	switch kind % 7 {
	case 0:
		return physics.BoxShape(physics.Box{X: x, Y: y, W: size, H: size + extra*0.1})
	case 1:
		return physics.CircleShape(physics.Circle{X: x, Y: y, R: size})
	case 2:
		p := physics.NewBoxPolygon(x, y, size, size*0.5+0.01, angle)
		p.Radius = radius
		return physics.PolygonShape(p)
	case 3:
		p, ok := physics.NewPolygon(pts(x, y, x+size, y+extra, x+extra, y+size), radius)
		if !ok {
			return physics.CircleShape(physics.Circle{X: x, Y: y, R: size})
		}
		return physics.PolygonShape(p)
	case 4:
		return physics.CapsuleShape(physics.Capsule{X1: x, Y1: y, X2: x + extra, Y2: y + size, R: radius + 0.01})
	case 5:
		return physics.SegmentShape(physics.Segment{X1: x, Y1: y, X2: x + size, Y2: y + extra})
	default:
		return physics.ChainShape(pts(x, y, x+size, y+extra, x+2*size, y, x+3*size, y-extra), extra > 0)
	}
}

func BenchmarkTestShapesPolygonPolygon(b *testing.B) {
	p := physics.PolygonShape(physics.NewBoxPolygon(0, 0, 10, 10, 0.3))
	q := physics.PolygonShape(mustPolygon(b, 0.5, 6, -4, 14, -2, 16, 5, 9, 9, 4, 3))
	b.ReportAllocs()
	for b.Loop() {
		physics.TestShapes(p, q)
	}
}

func BenchmarkTestShapesCapsuleChain(b *testing.B) {
	terrain := make([]float32, 0, 128)
	for i := range 64 {
		terrain = append(terrain, float32(i*10), float32(100+5*math.Sin(float64(i))))
	}
	chain := physics.ChainShape(pts(terrain...), false)
	c := physics.CapsuleShape(physics.Capsule{X1: 300, Y1: 80, X2: 300, Y2: 96, R: 6})
	b.ReportAllocs()
	for b.Loop() {
		physics.TestShapes(c, chain)
	}
}
