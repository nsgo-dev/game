package physics3d_test

import (
	"errors"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/physics3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
)

type v3 = physics3d.Vec3

// cubePoints returns the corners of a cube with half-extent h plus points on its faces and inside it, which the hull must drop.
func cubePoints(h float32) []v3 {
	var pts []v3
	pts = append(pts, v3{}, v3{X: h / 2, Y: h / 3})
	for i := range 8 {
		p := v3{X: -h, Y: -h, Z: -h}
		if i&1 != 0 {
			p.X = h
		}
		if i&2 != 0 {
			p.Y = h
		}
		if i&4 != 0 {
			p.Z = h
		}
		pts = append(pts, p)
	}
	pts = append(pts, v3{X: h}, v3{Y: -h, Z: h / 2}, v3{X: h, Y: h})
	return pts
}

func cubeHull(t testing.TB, h float32) physics3d.ConvexHull {
	t.Helper()
	hull, err := physics3d.NewConvexHull(cubePoints(h), 0)
	if err != nil {
		t.Fatal(err)
	}
	return hull
}

// wedgeHull is a unit triangular prism: a ramp rising toward -X.
func wedgeHull(t testing.TB) physics3d.ConvexHull {
	t.Helper()
	hull, err := physics3d.NewConvexHull([]v3{
		{X: -0.5, Y: -0.5, Z: -0.5}, {X: 0.5, Y: -0.5, Z: -0.5}, {X: -0.5, Y: -0.5, Z: 0.5}, {X: 0.5, Y: -0.5, Z: 0.5},
		{X: -0.5, Y: 0.5, Z: -0.5}, {X: -0.5, Y: 0.5, Z: 0.5},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return hull
}

// rockPoints returns a deterministic lumpy point cloud around the origin, about size across.
func rockPoints(seed uint64, n int, size float32) []v3 {
	r := rand.New(rand.NewPCG(seed, 808))
	pts := make([]v3, n)
	for i := range pts {
		// A random direction on a squashed sphere with bumps.
		z := r.Float64()*2 - 1
		a := r.Float64() * 2 * math.Pi
		s := math.Sqrt(1 - z*z)
		rad := float64(size) * 0.5 * (0.75 + 0.25*r.Float64())
		pts[i] = v3{X: float32(s * math.Cos(a) * rad), Y: float32(z * rad * 0.7), Z: float32(s * math.Sin(a) * rad)}
	}
	return pts
}

func near(a, b, tol float32) bool { return float32(math.Abs(float64(a-b))) <= tol }

func nearVec(a, b v3, tol float32) bool {
	return near(a.X, b.X, tol) && near(a.Y, b.Y, tol) && near(a.Z, b.Z, tol)
}

func TestConvexHullOfCube(t *testing.T) {
	h := cubeHull(t, 1)
	if got := h.NumVertices(); got != 8 {
		t.Fatalf("cube hull has %d vertices, want 8", got)
	}
	faces := h.Faces()
	if len(faces) != 6 {
		t.Fatalf("cube hull has %d faces, want 6 merged quads", len(faces))
	}
	for i, f := range faces {
		if len(f) != 4 {
			t.Fatalf("face %d has %d vertices, want 4", i, len(f))
		}
	}
	if got := len(h.Edges()); got != 12 {
		t.Fatalf("cube hull has %d edges, want 12", got)
	}
	if !near(h.Volume(), 8, 1e-5) {
		t.Fatalf("volume = %v, want 8", h.Volume())
	}
	if !nearVec(h.Centroid(), v3{}, 1e-6) {
		t.Fatalf("centroid = %v, want origin", h.Centroid())
	}
	// A solid cube of side 2 and mass 3 has moments m(2²+2²)/12 = 2 about each axis and no products.
	it := h.InertiaTensor(3)
	want := [3]v3{{X: 2}, {Y: 2}, {Z: 2}}
	for i := range 3 {
		if !nearVec(it[i], want[i], 1e-5) {
			t.Fatalf("inertia row %d = %v, want %v", i, it[i], want[i])
		}
	}
	// Faces point outward and wind counter-clockwise seen from outside.
	verts := h.Vertices()
	for i, f := range faces {
		n := h.FaceNormals()[i]
		a, b, c := verts[f[0]], verts[f[1]], verts[f[2]]
		if b.Sub(a).Cross(c.Sub(a)).Dot(n) <= 0 || a.Dot(n) <= 0 {
			t.Fatalf("face %d %v with normal %v is not counter-clockwise and outward", i, f, n)
		}
	}
}

func TestConvexHullIsDeterministic(t *testing.T) {
	pts := rockPoints(1, 500, 2)
	a, err := physics3d.NewConvexHull(pts, 0)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		b, err := physics3d.NewConvexHull(pts, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(a.Vertices(), b.Vertices()) || !slices.EqualFunc(a.Faces(), b.Faces(), slices.Equal) || !slices.Equal(a.Edges(), b.Edges()) {
			t.Fatal("building the same hull twice gave different results")
		}
	}
}

func TestConvexHullVertexLimit(t *testing.T) {
	pts := rockPoints(2, 2000, 2)
	full, err := physics3d.NewConvexHull(pts, physics3d.MaxHullVertices)
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{8, 16, 32, physics3d.DefaultHullVertices} {
		h, err := physics3d.NewConvexHull(pts, limit)
		if err != nil {
			t.Fatal(err)
		}
		if n := h.NumVertices(); n > limit || n < 4 {
			t.Fatalf("limit %d: hull has %d vertices", limit, n)
		}
		// Every vertex is an input point and the reduced hull lies inside the full one.
		for _, v := range h.Vertices() {
			if !slices.Contains(pts, v) {
				t.Fatalf("limit %d: vertex %v is not an input point", limit, v)
			}
		}
		if h.Volume() > full.Volume()*1.0001 || h.Volume() < full.Volume()*0.3 {
			t.Fatalf("limit %d: volume %v against the full hull's %v", limit, h.Volume(), full.Volume())
		}
	}
	// Limits are clamped: 0 means the default and anything above the maximum is lowered to it.
	if h, _ := physics3d.NewConvexHull(pts, 0); h.NumVertices() > physics3d.DefaultHullVertices {
		t.Fatalf("default limit kept %d vertices", h.NumVertices())
	}
	if h, _ := physics3d.NewConvexHull(pts, 10000); h.NumVertices() > physics3d.MaxHullVertices {
		t.Fatalf("huge limit kept %d vertices", h.NumVertices())
	}
}

func TestConvexHullRejectsDegenerateInput(t *testing.T) {
	nan := float32(math.NaN())
	cases := map[string][]v3{
		"empty":      nil,
		"three":      {{}, {X: 1}, {Y: 1}},
		"same":       {{X: 1}, {X: 1}, {X: 1}, {X: 1}},
		"line":       {{}, {X: 1}, {X: 2}, {X: 3}, {X: 4}},
		"plane":      {{}, {X: 1}, {Z: 1}, {X: 1, Z: 1}, {X: 0.5, Z: 0.3}},
		"not finite": {{}, {X: 1}, {Y: 1}, {Z: 1}, {X: nan}},
	}
	for name, pts := range cases {
		if _, err := physics3d.NewConvexHull(pts, 0); !errors.Is(err, physics3d.ErrDegenerateHull) {
			t.Errorf("%s: err = %v, want ErrDegenerateHull", name, err)
		}
	}
	var zero physics3d.ConvexHull
	if !zero.IsEmpty() || zero.NumVertices() != 0 {
		t.Fatal("the zero hull is not empty")
	}
	if c := physics3d.TestShapes(physics3d.ConvexHullShape(zero), physics3d.SphereShape(physics3d.Sphere{Radius: 1})); c.Hit {
		t.Fatal("the empty hull collided")
	}
}

func TestConvexHullFromMesh(t *testing.T) {
	// A glTF primitive or a render3d.Mesh: a cube's eight corners plus an unreferenced far vertex.
	positions := []math3d.Vec3{
		{X: -1, Y: -1, Z: -1}, {X: 1, Y: -1, Z: -1}, {X: 1, Y: 1, Z: -1}, {X: -1, Y: 1, Z: -1},
		{X: -1, Y: -1, Z: 1}, {X: 1, Y: -1, Z: 1}, {X: 1, Y: 1, Z: 1}, {X: -1, Y: 1, Z: 1},
		{X: 50, Y: 50, Z: 50},
	}
	indices := []uint32{0, 2, 1, 0, 3, 2, 4, 5, 6, 4, 6, 7, 0, 1, 5, 0, 5, 4, 2, 3, 7, 2, 7, 6, 1, 2, 6, 1, 6, 5, 0, 4, 7, 0, 7, 3}
	h, err := physics3d.ConvexHullFromMesh(positions, indices, 0)
	if err != nil {
		t.Fatal(err)
	}
	if h.NumVertices() != 8 || !near(h.Volume(), 8, 1e-5) {
		t.Fatalf("hull from indexed mesh: %d vertices, volume %v", h.NumVertices(), h.Volume())
	}
	all, err := physics3d.ConvexHullFromMesh(positions, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if all.NumVertices() <= 4 || all.Volume() <= 8 {
		t.Fatalf("hull from every position should reach the far vertex: %d vertices, volume %v", all.NumVertices(), all.Volume())
	}
	if _, err := physics3d.ConvexHullFromMesh(positions, []uint32{0, 1, 99}, 0); err == nil {
		t.Fatal("an out-of-range index was accepted")
	}
}

// TestConvexHullFromGLTFAndRender3DMesh builds a hull from the Khronos Box sample's primitive, directly and through the render3d.Mesh it maps to.
func TestConvexHullFromGLTFAndRender3DMesh(t *testing.T) {
	doc, err := gltf.LoadFS(os.DirFS("../gltf/testdata/khronos/Box/glTF"), "Box.gltf")
	if err != nil {
		t.Fatal(err)
	}
	prim := doc.Meshes[0].Primitives[0]
	mesh := render3d.Mesh{Positions: prim.Positions, Normals: prim.Normals, Indices: prim.Indices}
	if err := mesh.Validate(); err != nil {
		t.Fatal(err)
	}
	fromPrim, err := physics3d.ConvexHullFromMesh(prim.Positions, prim.Indices, 0)
	if err != nil {
		t.Fatal(err)
	}
	fromMesh, err := physics3d.ConvexHullFromMesh(mesh.Positions, mesh.Indices, 0)
	if err != nil {
		t.Fatal(err)
	}
	// The sample is a unit cube centred on the origin, its 24 vertices (four per face, for flat normals) sharing eight corners.
	for name, h := range map[string]physics3d.ConvexHull{"primitive": fromPrim, "render3d mesh": fromMesh} {
		if h.NumVertices() != 8 || len(h.Faces()) != 6 || !near(h.Volume(), 1, 1e-5) || !nearVec(h.Centroid(), v3{}, 1e-6) {
			t.Fatalf("%s: %d vertices, %d faces, volume %v, centroid %v; want the unit cube", name, h.NumVertices(), len(h.Faces()), h.Volume(), h.Centroid())
		}
	}
	if !slices.Equal(fromPrim.Vertices(), fromMesh.Vertices()) {
		t.Fatal("the primitive and the render3d mesh gave different hulls")
	}
}

func TestConvexHullRecentered(t *testing.T) {
	h := wedgeHull(t)
	c := h.Centroid()
	if near(c.X, 0, 1e-3) {
		t.Fatalf("wedge centroid %v should lean toward -X", c)
	}
	r := h.Recentered()
	if !nearVec(r.Center, c, 1e-6) || !nearVec(r.Centroid(), c, 1e-6) {
		t.Fatalf("recentered hull: Center %v, Centroid %v, want both %v", r.Center, r.Centroid(), c)
	}
	// It covers the same space.
	ab, bb := physics3d.ConvexHullShape(h).Bounds(), physics3d.ConvexHullShape(r).Bounds()
	if !nearVec(ab.Min(), bb.Min(), 1e-6) || !nearVec(ab.Max(), bb.Max(), 1e-6) {
		t.Fatalf("bounds changed: %v -> %v", ab, bb)
	}
}

// TestConvexShapePairs checks every new shape pair against a configuration with a known answer: the contact's normal points from the second shape toward the first.
func TestConvexShapePairs(t *testing.T) {
	cube := physics3d.ConvexHullShape(cubeHull(t, 1))
	cyl := physics3d.CylinderShape(physics3d.VerticalCylinder(v3{}, 1, 2))
	lying := physics3d.CylinderShape(physics3d.Cylinder{A: v3{Z: -1}, B: v3{Z: 1}, Radius: 1})
	box := func(c v3) physics3d.Shape {
		return physics3d.BoxShape(physics3d.Box{Center: c, HalfExtents: v3{X: 1, Y: 1, Z: 1}})
	}
	sphere := func(c v3, r float32) physics3d.Shape {
		return physics3d.SphereShape(physics3d.Sphere{Center: c, Radius: r})
	}
	capsule := func(a, b v3, r float32) physics3d.Shape {
		return physics3d.CapsuleShape(physics3d.Capsule{A: a, B: b, Radius: r})
	}
	yaw := physics3d.QuatFromAxisAngle(v3{Y: 1}, math.Pi/4)
	r2, r3 := float32(math.Sqrt2), float32(math.Sqrt(3))
	// Edge on edge: a cube turned 45 degrees about Z, whose lowest edge runs along Z, crosses a box turned 45 degrees about X, whose highest edge runs along X.
	edgeDown := cube.Transformed(v3{Y: 2*r2 - 0.1}, physics3d.QuatFromAxisAngle(v3{Z: 1}, math.Pi/4))
	edgeUp := box(v3{}).Transformed(v3{}, physics3d.QuatFromAxisAngle(v3{X: 1}, math.Pi/4))
	// Vertex on face: a cube turned so that its (-1, -1, -1) corner points straight down.
	cornerDown := cube.Transformed(v3{Y: 1 + r3 - 0.1}, physics3d.QuatFromAxisAngle(v3{X: -1, Z: 1}.Norm(), float32(math.Acos(1/math.Sqrt(3)))))
	// A cylinder tilted 30 degrees about Z, resting on its lowest rim point, half its height times cos 30 plus its radius times sin 30 below its center.
	tiltAxis := v3{X: -0.5, Y: r3 / 2}
	rimCenter := v3{Y: 1 + r3/2 + 0.5 - 0.1}
	rimDown := physics3d.CylinderShape(physics3d.Cylinder{A: rimCenter.Sub(tiltAxis), B: rimCenter.Add(tiltAxis), Radius: 1})
	cases := []struct {
		name   string
		a, b   physics3d.Shape
		hit    bool
		normal v3
		depth  float32
	}{
		{"hull on box", cube.Translated(v3{Y: 1.9}), box(v3{}), true, v3{Y: 1}, 0.1},
		{"box under hull", box(v3{}), cube.Translated(v3{Y: 1.9}), true, v3{Y: -1}, 0.1},
		{"hull beside box apart", cube.Translated(v3{X: 2.1}), box(v3{}), false, v3{}, 0},
		{"hull on oriented box", cube.Translated(v3{Y: 1.95}), box(v3{}).Transformed(v3{}, yaw), true, v3{Y: 1}, 0.05},
		{"oriented hull on box", cube.Transformed(v3{Y: 1.9}, yaw), box(v3{}), true, v3{Y: 1}, 0.1},
		{"hull and sphere", cube, sphere(v3{X: 1.3}, 0.5), true, v3{X: -1}, 0.2},
		{"sphere and hull", sphere(v3{X: 1.3}, 0.5), cube, true, v3{X: 1}, 0.2},
		{"sphere inside hull", sphere(v3{X: 0.8}, 0.1), cube, true, v3{X: 1}, 0.3},
		{"hull and sphere apart", cube, sphere(v3{X: 1.6}, 0.5), false, v3{}, 0},
		{"hull and capsule", cube, capsule(v3{X: 1.4, Z: -3}, v3{X: 1.4, Z: 3}, 0.5), true, v3{X: -1}, 0.1},
		{"capsule through hull", capsule(v3{Y: 0.8, Z: -3}, v3{Y: 0.8, Z: 3}, 0.25), cube, true, v3{Y: 1}, 0.45},
		{"hull and hull", cube, cube.Translated(v3{X: 1.8}), true, v3{X: -1}, 0.2},
		{"hull stacked on hull", cube.Translated(v3{Y: 1.99}), cube, true, v3{Y: 1}, 0.01},
		{"hull and hull apart", cube, cube.Translated(v3{X: 2.01}), false, v3{}, 0},
		{"hull and cylinder", cube, cyl.Translated(v3{X: 1.8}), true, v3{X: -1}, 0.2},
		{"cylinder on box", cyl.Translated(v3{Y: 1.9}), box(v3{}), true, v3{Y: 1}, 0.1},
		{"cylinder beside box", cyl.Translated(v3{X: 1.95}), box(v3{}), true, v3{X: 1}, 0.05},
		{"cylinder on oriented box", cyl.Translated(v3{Y: 1.9}), box(v3{}).Transformed(v3{}, yaw), true, v3{Y: 1}, 0.1},
		{"lying cylinder on box", lying.Translated(v3{Y: 1.95}), box(v3{}), true, v3{Y: 1}, 0.05},
		{"cylinder and sphere on cap", cyl, sphere(v3{Y: 1.3}, 0.5), true, v3{Y: -1}, 0.2},
		{"cylinder and sphere on side", cyl, sphere(v3{X: 1.3}, 0.5), true, v3{X: -1}, 0.2},
		{"cylinder and sphere past rim", cyl, sphere(v3{X: 1.4, Y: 1.4}, 0.5), false, v3{}, 0},
		{"cylinder and capsule", cyl, capsule(v3{X: 1.3, Y: -3}, v3{X: 1.3, Y: 3}, 0.5), true, v3{X: -1}, 0.2},
		{"cylinders side by side", cyl, cyl.Translated(v3{X: 1.9}), true, v3{X: -1}, 0.1},
		{"cylinders stacked", cyl.Translated(v3{Y: 1.95}), cyl, true, v3{Y: 1}, 0.05},
		{"cylinders apart", cyl, cyl.Translated(v3{X: 2.05}), false, v3{}, 0},
		{"lying cylinders crossed", lying.Translated(v3{Y: 1.9}), physics3d.CylinderShape(physics3d.Cylinder{A: v3{X: -1}, B: v3{X: 1}, Radius: 1}), true, v3{Y: 1}, 0.1},
		{"cylinder and hull", cyl, cube.Translated(v3{Y: -1.9}), true, v3{Y: 1}, 0.1},
		{"hull edge across box edge", edgeDown, edgeUp, true, v3{Y: 1}, 0.1},
		{"hull edge across hull edge", edgeDown, cube.Transformed(v3{}, physics3d.QuatFromAxisAngle(v3{X: 1}, math.Pi/4)), true, v3{Y: 1}, 0.1},
		{"hull corner on box face", cornerDown, box(v3{}), true, v3{Y: 1}, 0.1},
		{"hull corner on cylinder cap", cornerDown, cyl, true, v3{Y: 1}, 0.1},
		{"tilted cylinder rim on box", rimDown, box(v3{}), true, v3{Y: 1}, 0.1},
		{"tilted cylinder rim on hull", rimDown, cube, true, v3{Y: 1}, 0.1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := physics3d.TestShapes(c.a, c.b)
			if got.Hit != c.hit {
				t.Fatalf("hit = %v, want %v (%+v)", got.Hit, c.hit, got)
			}
			if !c.hit {
				return
			}
			if got.Normal.Dot(c.normal) < 0.999 || !near(got.Depth, c.depth, 2e-3) {
				t.Fatalf("normal %v depth %v, want %v and %v", got.Normal, got.Depth, c.normal, c.depth)
			}
			if !near(got.Normal.Len(), 1, 1e-4) {
				t.Fatalf("normal %v is not unit length", got.Normal)
			}
			// Moving a by Normal*Depth separates the pair.
			moved := physics3d.TestShapes(c.a.Translated(got.Normal.Scale(got.Depth+2e-3)), c.b)
			if moved.Hit {
				t.Fatalf("moving a by Normal*Depth still overlaps: %+v", moved)
			}
			// The reversed test agrees with a flipped normal.
			rev := physics3d.TestShapes(c.b, c.a)
			if !rev.Hit || rev.Normal.Dot(c.normal) > -0.999 || !near(rev.Depth, c.depth, 2e-3) {
				t.Fatalf("reversed: %+v", rev)
			}
		})
	}
}

// TestConvexContactPointIsPatchCenter checks that a hull resting on a face and a cylinder on its end or side get a contact point in the middle of the touching area, which keeps them from rocking on the single point the solver gets per pair.
func TestConvexContactPointIsPatchCenter(t *testing.T) {
	floor := physics3d.BoxShape(physics3d.Box{Center: v3{X: 0.3, Y: -1}, HalfExtents: v3{X: 5, Y: 1, Z: 5}})
	cases := []struct {
		name string
		s    physics3d.Shape
		want v3
	}{
		{"hull face", physics3d.ConvexHullShape(cubeHull(t, 0.5)).Translated(v3{X: 1, Y: 0.49, Z: -2}), v3{X: 1, Z: -2}},
		{"cylinder end", physics3d.CylinderShape(physics3d.VerticalCylinder(v3{X: -1, Y: 0.49, Z: 1}, 0.4, 1)), v3{X: -1, Z: 1}},
		{"cylinder side", physics3d.CylinderShape(physics3d.Cylinder{A: v3{X: -1, Y: 0.39, Z: 2}, B: v3{X: 1, Y: 0.39, Z: 2}, Radius: 0.4}), v3{Z: 2}},
	}
	for _, c := range cases {
		got := physics3d.TestShapes(c.s, floor)
		if !got.Hit || !nearVec(got.Point, c.want, 0.01) {
			t.Errorf("%s: contact %+v, want point near %v", c.name, got, c.want)
		}
	}
}

func TestConvexAgainstTrianglesAndHeightfield(t *testing.T) {
	tri := physics3d.Triangle{A: v3{X: -5, Z: -5}, B: v3{X: 0, Z: 5}, C: v3{X: 5, Z: -5}}
	if tri.Normal().Y < 0 {
		tri.B, tri.C = tri.C, tri.B
	}
	hf := physics3d.NewHeightfieldCollider(9, 9, 1, v3{X: -4, Z: -4}, make([]float32, 81))
	shapes := map[string]physics3d.Shape{
		"hull":           physics3d.ConvexHullShape(cubeHull(t, 0.5)).Translated(v3{Y: 0.45}),
		"rotated hull":   physics3d.ConvexHullShape(cubeHull(t, 0.5)).Transformed(v3{Y: 0.45}, physics3d.QuatFromAxisAngle(v3{Y: 1}, 0.4)),
		"cylinder":       physics3d.CylinderShape(physics3d.VerticalCylinder(v3{Y: 0.45}, 0.5, 1)),
		"lying cylinder": physics3d.CylinderShape(physics3d.Cylinder{A: v3{X: -0.5, Y: 0.45}, B: v3{X: 0.5, Y: 0.45}, Radius: 0.5}),
	}
	for name, s := range shapes {
		for which, c := range map[string]physics3d.Contact{
			"triangle":    physics3d.TestShapeTriangle(s, tri),
			"heightfield": hf.TestShape(s),
		} {
			if !c.Hit || c.Normal.Dot(v3{Y: 1}) < 0.999 || !near(c.Depth, 0.05, 2e-3) {
				t.Errorf("%s on %s: %+v, want normal +Y depth 0.05", name, which, c)
			}
		}
		if c := physics3d.TestShapeTriangle(s.Translated(v3{Y: 0.1}), tri); c.Hit {
			t.Errorf("%s above the triangle hit it: %+v", name, c)
		}
		// Sunk below a heightfield cell, a shape is pushed up, not down through the terrain.
		if c := hf.TestShape(s.Translated(v3{Y: -0.6})); !c.Hit || c.Normal.Y < 0.999 || !near(c.Depth, 0.65, 5e-3) {
			t.Errorf("%s under the heightfield: %+v, want normal +Y depth 0.65", name, c)
		}
	}
}

// TestConvexGrazesLedgeEdge checks that a shape pushed sideways against the open edge of a ledge is pushed back out sideways by its small overlap, not down or up by the height of its face above or below the ledge.
func TestConvexGrazesLedgeEdge(t *testing.T) {
	shapes := map[string]physics3d.Shape{
		"hull":     physics3d.ConvexHullShape(cubeHull(t, 0.5)),
		"cylinder": physics3d.CylinderShape(physics3d.VerticalCylinder(v3{}, 0.5, 1)),
	}
	// A thin ledge at y 0.2 whose open edge at x 0.49 overlaps the shapes by 0.01.
	ledge := []v3{{X: 0.49, Y: 0.2, Z: -3}, {X: 0.49, Y: 0.2, Z: 3}, {X: 5, Y: 0.2, Z: 3}, {X: 5, Y: 0.2, Z: -3}}
	mesh := physics3d.NewTriMeshCollider(ledge, []uint32{0, 1, 2, 0, 2, 3})
	// A heightfield plateau at y 0.2 from x 0.5, beside a pit at y -2 up to x 0.4.
	const n = 21
	h := make([]float32, n*n)
	for z := range n {
		for x := range n {
			h[z*n+x] = -2
			if x >= 15 {
				h[z*n+x] = 0.2
			}
		}
	}
	hf := physics3d.NewHeightfieldCollider(n, n, 0.1, v3{X: -1, Z: -1}, h)
	for name, s := range shapes {
		for _, tri := range []physics3d.Triangle{{A: ledge[0], B: ledge[1], C: ledge[2]}, {A: ledge[0], B: ledge[2], C: ledge[3]}} {
			if c := physics3d.TestShapeTriangle(s, tri); c.Hit && (c.Normal.Dot(v3{X: -1}) < 0.999 || !near(c.Depth, 0.01, 2e-3)) {
				t.Errorf("%s grazing a ledge triangle: %+v, want normal -X depth 0.01", name, c)
			}
		}
		if c := mesh.TestShape(s); !c.Hit || c.Normal.Dot(v3{X: -1}) < 0.999 || !near(c.Depth, 0.01, 2e-3) {
			t.Errorf("%s grazing a ledge mesh: %+v, want normal -X depth 0.01", name, c)
		}
		// Beside the plateau, overlapping its top by 0.01 and the cliff below it by a few centimetres, the shape is pushed back from the cliff, not lifted 0.3 onto the plateau.
		if c := hf.TestShape(s.Translated(v3{X: 0.01})); !c.Hit || c.Normal.X > -0.9 || c.Depth > 0.1 {
			t.Errorf("%s beside a plateau: %+v, want a sideways push of a few centimetres", name, c)
		}
		// Resting on the ledge with its centroid over it, the shape still gets the ledge's face normal.
		if c := mesh.TestShape(s.Translated(v3{X: 1, Y: 0.69})); !c.Hit || c.Normal.Dot(v3{Y: 1}) < 0.999 || !near(c.Depth, 0.01, 2e-3) {
			t.Errorf("%s on the ledge: %+v, want normal +Y depth 0.01", name, c)
		}
	}
}

// TestZeroLengthCylinderIsIgnored checks that a cylinder with no height is treated the same way by collisions, raycasts and areas: as having no volume.
func TestZeroLengthCylinderIsIgnored(t *testing.T) {
	disc := physics3d.CylinderShape(physics3d.VerticalCylinder(v3{}, 1, 0))
	box := physics3d.BoxShape(physics3d.Box{HalfExtents: v3{X: 1, Y: 1, Z: 1}})
	if c := physics3d.TestShapes(disc, box); c.Hit {
		t.Errorf("a zero-length cylinder collided: %+v", c)
	}
	if h, ok := physics3d.RaycastShape(physics3d.Ray{Origin: v3{Y: 5}, Dir: v3{Y: -1}, MaxDist: 10}, disc); ok {
		t.Errorf("a ray hit a zero-length cylinder: %+v", h)
	}
	tri := physics3d.Triangle{A: v3{X: -5, Z: -5}, B: v3{X: 0, Z: 5}, C: v3{X: 5, Z: -5}}
	if c := physics3d.TestShapeTriangle(disc, tri); c.Hit {
		t.Errorf("a zero-length cylinder hit a triangle: %+v", c)
	}
}

func TestRaycastConvexShapes(t *testing.T) {
	cube := physics3d.ConvexHullShape(cubeHull(t, 1))
	cyl := physics3d.CylinderShape(physics3d.VerticalCylinder(v3{}, 1, 2))
	cases := []struct {
		name   string
		s      physics3d.Shape
		ray    physics3d.Ray
		hit    bool
		dist   float32
		normal v3
	}{
		{"hull face", cube, physics3d.Ray{Origin: v3{X: -5, Y: 0.3}, Dir: v3{X: 1}, MaxDist: 10}, true, 4, v3{X: -1}},
		{"hull top", cube, physics3d.Ray{Origin: v3{Y: 5, Z: 0.5}, Dir: v3{Y: -2}, MaxDist: 10}, true, 4, v3{Y: 1}},
		{"hull miss", cube, physics3d.Ray{Origin: v3{X: -5, Y: 1.2}, Dir: v3{X: 1}, MaxDist: 10}, false, 0, v3{}},
		{"hull short", cube, physics3d.Ray{Origin: v3{X: -5}, Dir: v3{X: 1}, MaxDist: 3.9}, false, 0, v3{}},
		{"hull inside", cube, physics3d.Ray{Origin: v3{X: 0.2}, Dir: v3{X: 1}, MaxDist: 5}, true, 0, v3{X: -1}},
		{"rotated hull", cube.Transformed(v3{}, physics3d.QuatFromAxisAngle(v3{Y: 1}, math.Pi/4)), physics3d.Ray{Origin: v3{X: -5, Z: 0.3}, Dir: v3{X: 1}, MaxDist: 10}, true, 5.3 - float32(math.Sqrt2), v3{X: -float32(math.Sqrt2) / 2, Z: float32(math.Sqrt2) / 2}},
		{"cylinder side", cyl, physics3d.Ray{Origin: v3{X: -5, Y: 0.5}, Dir: v3{X: 1}, MaxDist: 10}, true, 4, v3{X: -1}},
		{"cylinder cap", cyl, physics3d.Ray{Origin: v3{X: 0.5, Y: 5}, Dir: v3{Y: -1}, MaxDist: 10}, true, 4, v3{Y: 1}},
		{"cylinder bottom cap", cyl, physics3d.Ray{Origin: v3{Z: 0.9, Y: -5}, Dir: v3{Y: 1}, MaxDist: 10}, true, 4, v3{Y: -1}},
		{"cylinder past rim", cyl, physics3d.Ray{Origin: v3{X: 1.1, Y: 5}, Dir: v3{Y: -1}, MaxDist: 10}, false, 0, v3{}},
		{"cylinder over cap", cyl, physics3d.Ray{Origin: v3{X: -5, Y: 1.1}, Dir: v3{X: 1}, MaxDist: 10}, false, 0, v3{}},
		{"cylinder inside", cyl, physics3d.Ray{Origin: v3{Y: 0.5}, Dir: v3{Z: 1}, MaxDist: 10}, true, 0, v3{Z: -1}},
		{"lying cylinder", physics3d.CylinderShape(physics3d.Cylinder{A: v3{Z: -1}, B: v3{Z: 1}, Radius: 1}), physics3d.Ray{Origin: v3{Y: 5}, Dir: v3{Y: -1}, MaxDist: 10}, true, 4, v3{Y: 1}},
	}
	for _, c := range cases {
		h, ok := physics3d.RaycastShape(c.ray, c.s)
		if ok != c.hit {
			t.Errorf("%s: hit = %v, want %v (%+v)", c.name, ok, c.hit, h)
			continue
		}
		if ok && (!near(h.Distance, c.dist, 1e-4) || !nearVec(h.Normal, c.normal, 1e-4)) {
			t.Errorf("%s: distance %v normal %v, want %v and %v", c.name, h.Distance, h.Normal, c.dist, c.normal)
		}
	}
}

func TestConvexShapeBounds(t *testing.T) {
	// A cylinder tilted 90 degrees about Z lies along X.
	cyl := physics3d.CylinderShape(physics3d.VerticalCylinder(v3{}, 0.5, 4)).Transformed(v3{Y: 1}, physics3d.QuatFromAxisAngle(v3{Z: 1}, math.Pi/2))
	b := cyl.Bounds()
	if !nearVec(b.Center, v3{Y: 1}, 1e-5) || !nearVec(b.HalfExtents, v3{X: 2, Y: 0.5, Z: 0.5}, 1e-5) {
		t.Fatalf("lying cylinder bounds %+v", b)
	}
	hull := physics3d.ConvexHullShape(cubeHull(t, 1)).Transformed(v3{X: 3}, physics3d.QuatFromAxisAngle(v3{Y: 1}, math.Pi/4))
	b = hull.Bounds()
	r2 := float32(math.Sqrt2)
	if !nearVec(b.Center, v3{X: 3}, 1e-5) || !nearVec(b.HalfExtents, v3{X: r2, Y: 1, Z: r2}, 1e-5) {
		t.Fatalf("turned hull bounds %+v", b)
	}
}

func TestConvexShapeMass(t *testing.T) {
	w := physics3d.NewWorld()
	boxBody := w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: v3{X: 1, Y: 1, Z: 1}}), v3{}, 3))
	hullBody := w.AddBody(physics3d.NewDynamicBody(physics3d.ConvexHullShape(cubeHull(t, 1)), v3{X: 5}, 3))
	if !near(hullBody.InvInertia, boxBody.InvInertia, 1e-6) {
		t.Fatalf("a cube hull's inverse inertia %v differs from the box's %v", hullBody.InvInertia, boxBody.InvInertia)
	}
	// A solid cylinder of radius 1, height 2 and mass 18: m(6r²+h²)/18 = 10.
	cylBody := w.AddBody(physics3d.NewDynamicBody(physics3d.CylinderShape(physics3d.VerticalCylinder(v3{}, 1, 2)), v3{X: -5}, 18))
	if !near(cylBody.InvInertia, 0.1, 1e-6) {
		t.Fatalf("cylinder inverse inertia %v, want 0.1", cylBody.InvInertia)
	}
}

func BenchmarkTestShapesHullHull(b *testing.B) {
	h, err := physics3d.NewConvexHull(rockPoints(3, 200, 1), 32)
	if err != nil {
		b.Fatal(err)
	}
	a := physics3d.ConvexHullShape(h)
	o := physics3d.ConvexHullShape(h).Transformed(v3{X: 0.7, Y: 0.2}, physics3d.QuatFromAxisAngle(v3{X: 1, Y: 1}, 0.7))
	for b.Loop() {
		physics3d.TestShapes(a, o)
	}
}

func BenchmarkTestShapesCylinderBox(b *testing.B) {
	c := physics3d.CylinderShape(physics3d.VerticalCylinder(v3{Y: 0.95}, 0.5, 2)).Transformed(v3{}, physics3d.QuatFromAxisAngle(v3{X: 1}, 0.2))
	box := physics3d.BoxShape(physics3d.Box{Center: v3{Y: -1}, HalfExtents: v3{X: 5, Y: 1, Z: 5}})
	for b.Loop() {
		physics3d.TestShapes(c, box)
	}
}

func BenchmarkNewConvexHull(b *testing.B) {
	pts := rockPoints(4, 5000, 1)
	for b.Loop() {
		if _, err := physics3d.NewConvexHull(pts, physics3d.DefaultHullVertices); err != nil {
			b.Fatal(err)
		}
	}
}
