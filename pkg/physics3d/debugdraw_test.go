//go:build !release

package physics3d_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/physics3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type seg2D struct {
	x0, y0, x1, y1 float32
	color          core.Color
	width          float32
}

// extractSegments replays recorded DrawOps to collect all stroked line segments
// and verifies Save/Restore balance and finite coordinates.
func extractSegments(t *testing.T, ops []core.DrawOp) []seg2D {
	t.Helper()
	var (
		segs      []seg2D
		curStroke core.Color
		curWidth  float32 = 1
		stack     []struct {
			c core.Color
			w float32
		}
		curX, curY float32
		hasPos     bool
		pathSegs   []seg2D
	)
	for i, op := range ops {
		for _, v := range []float32{op.X, op.Y, op.X1, op.Y1, op.X2, op.Y2, op.W, op.H, op.R} {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				t.Fatalf("op %d (%v) has non-finite value %v", i, op.Kind, v)
			}
		}
		switch op.Kind {
		case core.DrawSave:
			stack = append(stack, struct {
				c core.Color
				w float32
			}{curStroke, curWidth})
		case core.DrawRestore:
			if len(stack) == 0 {
				t.Fatalf("op %d: unmatched DrawRestore", i)
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			curStroke, curWidth = top.c, top.w
		case core.DrawStrokeColor:
			curStroke = op.Color
		case core.DrawLineWidth:
			curWidth = op.W
		case core.DrawBeginPath:
			pathSegs = pathSegs[:0]
			hasPos = false
		case core.DrawMoveTo:
			curX, curY = op.X, op.Y
			hasPos = true
		case core.DrawLineTo:
			if !hasPos {
				t.Fatalf("op %d: DrawLineTo without MoveTo", i)
			}
			pathSegs = append(pathSegs, seg2D{
				x0: curX, y0: curY, x1: op.X, y1: op.Y,
				color: curStroke, width: curWidth,
			})
			curX, curY = op.X, op.Y
		case core.DrawStroke:
			for _, s := range pathSegs {
				s.color = curStroke
				s.width = curWidth
				segs = append(segs, s)
			}
		}
	}
	if len(stack) != 0 {
		t.Fatalf("unbalanced DrawSave/DrawRestore: depth %d remaining", len(stack))
	}
	return segs
}

func renderCanvasOps(t *testing.T, w, h core.Points, draw func(d *core.Drawing)) []core.DrawOp {
	t.Helper()
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "debug",
			Style:  core.Style{Width: core.Pt(w), Height: core.Pt(h)},
			Draw: func(d *core.Drawing, _ core.Size) {
				draw(d)
			},
		}
	})
	return app.Find("debug").Ops
}

func TestMat4AndProjectPerspective(t *testing.T) {
	vp := core.Bounds{
		Origin: core.Point{X: 100, Y: 50},
		Size:   core.Size{Width: 800, Height: 600},
	}
	aspect := float32(800.0 / 600.0)
	fovY := float32(math.Pi / 2) // 90 deg -> tan(45 deg) == 1
	near, far := float32(0.5), float32(100)

	proj := physics3d.Mat4Perspective(fovY, aspect, near, far)
	view := physics3d.Mat4LookAt(
		physics3d.Vec3{X: 0, Y: 0, Z: 10},
		physics3d.Vec3{X: 0, Y: 0, Z: 0},
		physics3d.Vec3{X: 0, Y: 1, Z: 0},
	)
	viewProj := physics3d.Mat4Mul(proj, view)

	// Identity check.
	if got := physics3d.Mat4Mul(physics3d.Mat4Identity(), viewProj); got != viewProj {
		t.Fatalf("Identity * M != M")
	}

	// Origin (0,0,0) is at z_view = -10 -> center of viewport (500, 350).
	pt, ndcZ, ok := physics3d.Project(viewProj, vp, physics3d.Vec3{})
	if !ok {
		t.Fatal("origin should project inside frustum")
	}
	if !approx(float32(pt.X), 500, 1e-3) || !approx(float32(pt.Y), 350, 1e-3) {
		t.Fatalf("origin projected to (%v, %v), want (500, 350)", pt.X, pt.Y)
	}
	if ndcZ <= -1 || ndcZ >= 1 {
		t.Fatalf("origin ndcZ = %v, want in (-1, 1)", ndcZ)
	}

	// At z = 0 (distance d = 10 from camera, tan(fovY/2) = 1):
	// Top of frustum is y = +10 -> ndcY = +1 -> screen Y = viewport.Origin.Y = 50.
	// Bottom is y = -10 -> ndcY = -1 -> screen Y = 50 + 600 = 650.
	// Right is x = +10 * aspect -> ndcX = +1 -> screen X = 100 + 800 = 900.
	// Left is x = -10 * aspect -> ndcX = -1 -> screen X = 100.
	cases := []struct {
		name         string
		p            physics3d.Vec3
		wantX, wantY float32
	}{
		{"top", physics3d.Vec3{X: 0, Y: 10, Z: 0}, 500, 50},
		{"bottom", physics3d.Vec3{X: 0, Y: -10, Z: 0}, 500, 650},
		{"right", physics3d.Vec3{X: 10 * aspect, Y: 0, Z: 0}, 900, 350},
		{"left", physics3d.Vec3{X: -10 * aspect, Y: 0, Z: 0}, 100, 350},
		{"half_up_right", physics3d.Vec3{X: 5 * aspect, Y: 5, Z: 0}, 700, 200},
		// Exact near plane: z_view = -0.5 -> world z = 9.5.
		{"on_near", physics3d.Vec3{X: 0, Y: 0, Z: 9.5}, 500, 350},
		// Exact far plane: z_view = -100 -> world z = -90.
		{"on_far", physics3d.Vec3{X: 0, Y: 0, Z: -90}, 500, 350},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, ok := physics3d.Project(viewProj, vp, tc.p)
			if !ok {
				t.Fatalf("point %v should be visible", tc.p)
			}
			if !approx(float32(got.X), tc.wantX, 1e-2) || !approx(float32(got.Y), tc.wantY, 1e-2) {
				t.Fatalf("Project(%v) = (%v, %v), want (%v, %v)", tc.p, got.X, got.Y, tc.wantX, tc.wantY)
			}
		})
	}

	// Points behind eye, between eye and near plane, or beyond far plane are clipped.
	clipped := []struct {
		name string
		p    physics3d.Vec3
	}{
		{"behind_eye", physics3d.Vec3{X: 0, Y: 0, Z: 15}},
		{"at_eye", physics3d.Vec3{X: 0, Y: 0, Z: 10}},
		{"between_eye_and_near", physics3d.Vec3{X: 0, Y: 0, Z: 9.8}},
		{"beyond_far", physics3d.Vec3{X: 0, Y: 0, Z: -95}},
	}
	for _, tc := range clipped {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, ok := physics3d.Project(viewProj, vp, tc.p); ok {
				t.Fatalf("point %v should be clipped", tc.p)
			}
		})
	}
}

func TestMat4AndProjectOrtho(t *testing.T) {
	vp := core.Bounds{
		Origin: core.Point{X: 20, Y: 40},
		Size:   core.Size{Width: 400, Height: 200},
	}
	proj := physics3d.Mat4Ortho(-10, 10, -5, 5, 1, 50)
	view := physics3d.Mat4LookAt(
		physics3d.Vec3{X: 0, Y: 0, Z: 10},
		physics3d.Vec3{X: 0, Y: 0, Z: 0},
		physics3d.Vec3{X: 0, Y: 1, Z: 0},
	)
	viewProj := physics3d.Mat4Mul(proj, view)

	// Ortho maps [-10, 10] -> [20, 420] and [-5, 5] -> [240, 40] (Y top-down), independent of depth.
	for _, z := range []float32{9, 0, -20, -40} {
		for _, tc := range []struct {
			x, y         float32
			wantX, wantY float32
		}{
			{-10, 5, 20, 40},
			{10, 5, 420, 40},
			{-10, -5, 20, 240},
			{10, -5, 420, 240},
			{5, -2.5, 320, 190},
		} {
			got, _, ok := physics3d.Project(viewProj, vp, physics3d.Vec3{X: tc.x, Y: tc.y, Z: z})
			if !ok {
				t.Fatalf("ortho point (%v,%v,%v) unexpectedly clipped", tc.x, tc.y, z)
			}
			if !approx(float32(got.X), tc.wantX, 1e-3) || !approx(float32(got.Y), tc.wantY, 1e-3) {
				t.Fatalf("ortho Project(%v,%v,%v) = (%v,%v), want (%v,%v)", tc.x, tc.y, z, got.X, got.Y, tc.wantX, tc.wantY)
			}
		}
	}

	// Behind near plane (z > 9 -> z_view > -1) and beyond far plane (z < -40 -> z_view < -50).
	if _, _, ok := physics3d.Project(viewProj, vp, physics3d.Vec3{Z: 9.5}); ok {
		t.Fatal("ortho point in front of near plane should be clipped")
	}
	if _, _, ok := physics3d.Project(viewProj, vp, physics3d.Vec3{Z: -45}); ok {
		t.Fatal("ortho point beyond far plane should be clipped")
	}
}

func TestMat4DegenerateInputs(t *testing.T) {
	if got := physics3d.Mat4Perspective(0, 1, 0.1, 10); got != physics3d.Mat4Identity() {
		t.Fatalf("zero fov perspective = %v, want identity", got)
	}
	if got := physics3d.Mat4Perspective(1, 1, 10, 5); got != physics3d.Mat4Identity() {
		t.Fatalf("near >= far perspective = %v, want identity", got)
	}
	if got := physics3d.Mat4Ortho(1, 1, -1, 1, 0.1, 10); got != physics3d.Mat4Identity() {
		t.Fatalf("degenerate ortho = %v, want identity", got)
	}
	// Camera looking straight down along -Y with world up +Y must stay finite.
	view := physics3d.Mat4LookAt(
		physics3d.Vec3{X: 0, Y: 10, Z: 0},
		physics3d.Vec3{X: 0, Y: 0, Z: 0},
		physics3d.Vec3{X: 0, Y: 1, Z: 0},
	)
	for i, v := range view {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			t.Fatalf("top-down LookAt[%d] = %v", i, v)
		}
	}
	// Zero viewport rejects projection.
	if _, _, ok := physics3d.Project(physics3d.Mat4Identity(), core.Bounds{}, physics3d.Vec3{}); ok {
		t.Fatal("zero viewport should not project")
	}
}

func TestBoxEdges(t *testing.T) {
	b := physics3d.Box{
		Center:      physics3d.Vec3{X: 1, Y: 2, Z: -3},
		HalfExtents: physics3d.Vec3{X: 0.5, Y: 1.5, Z: 2},
	}
	edges := physics3d.BoxEdges(b)
	mn, mx := b.Min(), b.Max()

	seen := make(map[[2]physics3d.Vec3]bool, 12)
	cornerCount := make(map[physics3d.Vec3]int, 8)
	for i, e := range edges {
		// Every endpoint must be an exact corner of b.
		for _, p := range e {
			if (p.X != mn.X && p.X != mx.X) || (p.Y != mn.Y && p.Y != mx.Y) || (p.Z != mn.Z && p.Z != mx.Z) {
				t.Fatalf("edge %d endpoint %v is not a box corner (min=%v max=%v)", i, p, mn, mx)
			}
			cornerCount[p]++
		}
		// Exactly one coordinate differs per edge.
		diffs := 0
		if e[0].X != e[1].X {
			diffs++
		}
		if e[0].Y != e[1].Y {
			diffs++
		}
		if e[0].Z != e[1].Z {
			diffs++
		}
		if diffs != 1 {
			t.Fatalf("edge %d (%v -> %v) differs in %d axes, want 1", i, e[0], e[1], diffs)
		}
		if seen[e] || seen[[2]physics3d.Vec3{e[1], e[0]}] {
			t.Fatalf("duplicate box edge %d: %v", i, e)
		}
		seen[e] = true
	}
	if len(cornerCount) != 8 {
		t.Fatalf("got %d unique corners, want 8", len(cornerCount))
	}
	for c, n := range cornerCount {
		if n != 3 {
			t.Fatalf("corner %v has degree %d, want 3", c, n)
		}
	}
}

func TestSphereEdges(t *testing.T) {
	s := physics3d.Sphere{Center: physics3d.Vec3{X: 2, Y: -1, Z: 4}, Radius: 1.5}
	edges := physics3d.SphereEdges(s, 16)
	if len(edges) != 48 {
		t.Fatalf("got %d sphere edges, want 48", len(edges))
	}
	for i, e := range edges {
		for _, p := range e {
			if !approx(p.Dist(s.Center), s.Radius, 1e-4) {
				t.Fatalf("sphere edge %d vertex %v distance %v, want %v", i, p, p.Dist(s.Center), s.Radius)
			}
		}
	}
	// Verify all three great-circle planes are present (Z==center.Z, Y==center.Y, X==center.X).
	var xy, xz, yz int
	for _, e := range edges {
		if approx(e[0].Z, s.Center.Z, 1e-5) && approx(e[1].Z, s.Center.Z, 1e-5) {
			xy++
		}
		if approx(e[0].Y, s.Center.Y, 1e-5) && approx(e[1].Y, s.Center.Y, 1e-5) {
			xz++
		}
		if approx(e[0].X, s.Center.X, 1e-5) && approx(e[1].X, s.Center.X, 1e-5) {
			yz++
		}
	}
	if xy != 16 || xz != 16 || yz != 16 {
		t.Fatalf("great circle counts XY=%d XZ=%d YZ=%d, want 16 each", xy, xz, yz)
	}
	if got := physics3d.SphereEdges(physics3d.Sphere{Radius: 0}, 16); got != nil {
		t.Fatalf("zero-radius sphere edges = %v, want nil", got)
	}
}

func TestCapsuleEdgesVerticalAndTilted(t *testing.T) {
	distToSeg := func(a, b, p physics3d.Vec3) float32 {
		ab := b.Sub(a)
		l2 := ab.LenSq()
		if l2 == 0 {
			return p.Dist(a)
		}
		t := p.Sub(a).Dot(ab) / l2
		if t < 0 {
			t = 0
		} else if t > 1 {
			t = 1
		}
		return p.Dist(a.Add(ab.Scale(t)))
	}

	cases := []struct {
		name string
		cap  physics3d.Capsule
	}{
		{"vertical", physics3d.VerticalCapsule(physics3d.Vec3{X: 1, Y: 2, Z: -1}, 0.5, 2.5)},
		{"tilted", physics3d.Capsule{A: physics3d.Vec3{X: -1, Y: 0, Z: 2}, B: physics3d.Vec3{X: 2, Y: 3, Z: 1}, Radius: 0.75}},
		{"horizontal_x", physics3d.Capsule{A: physics3d.Vec3{X: -2}, B: physics3d.Vec3{X: 2}, Radius: 0.4}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			edges := physics3d.CapsuleEdges(tc.cap, 16)
			// 2*16 rings + 4 rails + 4*8 hemisphere arcs = 68.
			if len(edges) != 68 {
				t.Fatalf("got %d capsule edges, want 68", len(edges))
			}
			axis := tc.cap.B.Sub(tc.cap.A).Norm()
			apexA := tc.cap.A.Sub(axis.Scale(tc.cap.Radius))
			apexB := tc.cap.B.Add(axis.Scale(tc.cap.Radius))
			sawApexA, sawApexB := false, false
			for i, e := range edges {
				for _, p := range e {
					if d := distToSeg(tc.cap.A, tc.cap.B, p); !approx(d, tc.cap.Radius, 1e-4) {
						t.Fatalf("edge %d vertex %v dist to segment = %v, want %v", i, p, d, tc.cap.Radius)
					}
					if approxVec(p, apexA, 1e-4) {
						sawApexA = true
					}
					if approxVec(p, apexB, 1e-4) {
						sawApexB = true
					}
				}
			}
			if !sawApexA || !sawApexB {
				t.Fatalf("capsule wireframe missing hemisphere apexes (A=%v B=%v)", sawApexA, sawApexB)
			}
		})
	}

	// Degenerate capsule (A == B) falls back to sphere edges.
	deg := physics3d.Capsule{A: physics3d.Vec3{Y: 1}, B: physics3d.Vec3{Y: 1}, Radius: 0.5}
	if got := physics3d.CapsuleEdges(deg, 12); len(got) != 36 {
		t.Fatalf("degenerate capsule edges = %d, want 36 (sphere)", len(got))
	}
}

func TestNearPlaneClipping(t *testing.T) {
	vp := core.Bounds{
		Origin: core.Point{X: 0, Y: 0},
		Size:   core.Size{Width: 800, Height: 600},
	}
	proj := physics3d.Mat4Perspective(float32(math.Pi/2), 800.0/600.0, 1.0, 100.0)
	view := physics3d.Mat4LookAt(
		physics3d.Vec3{X: 0, Y: 0, Z: 0},
		physics3d.Vec3{X: 0, Y: 0, Z: -1},
		physics3d.Vec3{X: 0, Y: 1, Z: 0},
	)
	viewProj := physics3d.Mat4Mul(proj, view)

	// Segment starts behind the camera at (+0.4, +0.4, +3) and goes to (+0.4, +0.4, -5).
	// It crosses the near plane z = -1 at (+0.4, +0.4, -1), where:
	//   ndcX = 0.4 / (1 * aspect) = 0.3 -> screen X = 400 + 0.3*400 = 520
	//   ndcY = 0.4 / 1 = 0.4            -> screen Y = 300 - 0.4*300 = 180
	// Far endpoint (+0.4, +0.4, -5):
	//   ndcX = 0.4 / (5 * aspect) = 0.06 -> screen X = 400 + 0.06*400 = 424
	//   ndcY = 0.4 / 5 = 0.08            -> screen Y = 300 - 0.08*300 = 276
	a := physics3d.Vec3{X: 0.4, Y: 0.4, Z: 3}
	b := physics3d.Vec3{X: 0.4, Y: 0.4, Z: -5}

	ops := renderCanvasOps(t, 800, 600, func(d *core.Drawing) {
		physics3d.DrawLine3D(d, viewProj, vp, a, b, core.RGB(0xFF0000), 2)
	})
	segs := extractSegments(t, ops)
	if len(segs) != 1 {
		t.Fatalf("got %d clipped segments, want 1", len(segs))
	}
	s := segs[0]
	if !approx(s.x0, 520, 1e-2) || !approx(s.y0, 180, 1e-2) {
		t.Fatalf("near-clipped start = (%v, %v), want (520, 180)", s.x0, s.y0)
	}
	if !approx(s.x1, 424, 1e-2) || !approx(s.y1, 276, 1e-2) {
		t.Fatalf("far end = (%v, %v), want (424, 276)", s.x1, s.y1)
	}

	// Segment completely behind camera produces 0 ops.
	behindOps := renderCanvasOps(t, 800, 600, func(d *core.Drawing) {
		physics3d.DrawLine3D(d, viewProj, vp,
			physics3d.Vec3{X: -1, Y: 0, Z: 2},
			physics3d.Vec3{X: 1, Y: 1, Z: 5},
			core.RGB(0xFF0000), 2)
	})
	if len(behindOps) != 0 {
		t.Fatalf("segment behind camera emitted %d ops, want 0", len(behindOps))
	}
}

func TestWireframesAlignWithProjectedMeshVertices(t *testing.T) {
	vp := core.Bounds{
		Origin: core.Point{X: 40, Y: 20},
		Size:   core.Size{Width: 640, Height: 480},
	}
	view := physics3d.Mat4LookAt(
		physics3d.Vec3{X: 4, Y: 3, Z: 8},
		physics3d.Vec3{X: 0, Y: 0.5, Z: 0},
		physics3d.Vec3{X: 0, Y: 1, Z: 0},
	)
	cameras := []struct {
		name     string
		viewProj physics3d.Mat4
	}{
		{
			name:     "perspective",
			viewProj: physics3d.Mat4Mul(physics3d.Mat4Perspective(float32(60*math.Pi/180), 640.0/480.0, 0.1, 100), view),
		},
		{
			name:     "orthographic",
			viewProj: physics3d.Mat4Mul(physics3d.Mat4Ortho(-6, 6, -4.5, 4.5, 0.1, 100), view),
		},
	}

	box := physics3d.Box{
		Center:      physics3d.Vec3{X: -1.5, Y: 0.5, Z: 0},
		HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5},
	}
	sphere := physics3d.Sphere{
		Center: physics3d.Vec3{X: 0, Y: 1.0, Z: -0.5},
		Radius: 0.6,
	}
	capsule := physics3d.VerticalCapsule(physics3d.Vec3{X: 1.5, Y: 1.0, Z: 0.25}, 0.35, 1.8)
	style := physics3d.DefaultDebugStyle()
	style.SphereSegments = 12

	for _, cam := range cameras {
		t.Run(cam.name, func(t *testing.T) {
			checkShape := func(name string, shape physics3d.Shape, expected [][2]physics3d.Vec3, wantCol core.Color) {
				t.Helper()
				ops := renderCanvasOps(t, 720, 520, func(d *core.Drawing) {
					physics3d.DrawShape(d, cam.viewProj, vp, shape, style)
				})
				segs := extractSegments(t, ops)
				if len(segs) != len(expected) {
					t.Fatalf("%s: got %d drawn segments, want %d", name, len(segs), len(expected))
				}
				for i, e := range expected {
					p0, _, ok0 := physics3d.Project(cam.viewProj, vp, e[0])
					p1, _, ok1 := physics3d.Project(cam.viewProj, vp, e[1])
					if !ok0 || !ok1 {
						t.Fatalf("%s edge %d unexpectedly outside frustum", name, i)
					}
					g := segs[i]
					if g.color != wantCol {
						t.Fatalf("%s edge %d color = %#x, want %#x", name, i, g.color, wantCol)
					}
					if !approx(g.x0, float32(p0.X), 1e-2) || !approx(g.y0, float32(p0.Y), 1e-2) ||
						!approx(g.x1, float32(p1.X), 1e-2) || !approx(g.y1, float32(p1.Y), 1e-2) {
						t.Fatalf("%s edge %d = (%v,%v)->(%v,%v), want (%v,%v)->(%v,%v)",
							name, i, g.x0, g.y0, g.x1, g.y1, p0.X, p0.Y, p1.X, p1.Y)
					}
				}
			}

			boxEdges := physics3d.BoxEdges(box)
			checkShape("box", physics3d.BoxShape(box), boxEdges[:], style.BoxColor)
			checkShape("sphere", physics3d.SphereShape(sphere), physics3d.SphereEdges(sphere, style.SphereSegments), style.SphereColor)
			checkShape("capsule", physics3d.CapsuleShape(capsule), physics3d.CapsuleEdges(capsule, style.SphereSegments), style.CapsuleColor)
			cyl := physics3d.VerticalCylinder(physics3d.Vec3{X: 2.5, Y: 0.5, Z: -0.5}, 0.4, 1)
			checkShape("cylinder", physics3d.CylinderShape(cyl), physics3d.CylinderEdges(cyl, style.SphereSegments), style.CylinderColor)
			hullShape := physics3d.ConvexHullShape(wedgeHull(t).Translated(physics3d.Vec3{X: -3, Y: 0.5})).Transformed(physics3d.Vec3{}, physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, 0.3))
			checkShape("hull", hullShape, physics3d.ConvexHullEdges(hullShape.Hull, hullShape.Rot), style.HullColor)
		})
	}
}

func TestDrawWorldCharacterContactAndRaycast(t *testing.T) {
	vp := core.Bounds{
		Origin: core.Point{X: 0, Y: 0},
		Size:   core.Size{Width: 800, Height: 600},
	}
	view := physics3d.Mat4LookAt(
		physics3d.Vec3{X: 0, Y: 2, Z: 10},
		physics3d.Vec3{X: 0, Y: 0, Z: 0},
		physics3d.Vec3{X: 0, Y: 1, Z: 0},
	)
	viewProj := physics3d.Mat4Mul(physics3d.Mat4Perspective(float32(60*math.Pi/180), 800.0/600.0, 0.1, 100), view)
	style := physics3d.DefaultDebugStyle()
	style.SphereSegments = 12

	// World with a static floor box and an overlapping dynamic sphere so a contact is generated.
	world := physics3d.NewWorld()
	world.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 3, Y: 0.5, Z: 3}}),
		physics3d.Vec3{Y: -0.5},
	))
	world.AddBody(physics3d.NewDynamicBody(
		physics3d.SphereShape(physics3d.Sphere{Radius: 0.6}),
		physics3d.Vec3{Y: 0.4},
		1.0,
	))

	worldOps := renderCanvasOps(t, 800, 600, func(d *core.Drawing) {
		physics3d.DrawWorld(d, viewProj, vp, world, style)
	})
	worldSegs := extractSegments(t, worldOps)
	// 12 box edges + 36 sphere edges + 1 contact normal = 49 segments.
	if len(worldSegs) != 49 {
		t.Fatalf("DrawWorld emitted %d segments, want 49", len(worldSegs))
	}
	var boxCnt, sphereCnt, normalCnt int
	for _, s := range worldSegs {
		switch s.color {
		case style.BoxColor:
			boxCnt++
		case style.SphereColor:
			sphereCnt++
		case style.NormalColor:
			normalCnt++
		}
	}
	if boxCnt != 12 || sphereCnt != 36 || normalCnt != 1 {
		t.Fatalf("DrawWorld segment colors: box=%d sphere=%d normal=%d, want 12/36/1", boxCnt, sphereCnt, normalCnt)
	}
	// Contact marker uses DrawArc + DrawFill with ContactColor.
	hasContactArc := false
	for _, op := range worldOps {
		if op.Kind == core.DrawArc {
			hasContactArc = true
			break
		}
	}
	if !hasContactArc {
		t.Fatal("DrawWorld missing contact point marker arc for overlapping pair")
	}

	// Character controller (grounded) emits capsule edges + 1 ground normal.
	ch := physics3d.NewCharacterController(physics3d.Vec3{X: 1, Y: 0, Z: 0}, 0.4, 1.8)
	ch.Grounded = true
	ch.GroundNormal = physics3d.Vec3{Y: 1}
	charOps := renderCanvasOps(t, 800, 600, func(d *core.Drawing) {
		physics3d.DrawCharacter(d, viewProj, vp, ch, style)
	})
	charSegs := extractSegments(t, charOps)
	wantCapEdges := len(physics3d.CapsuleEdges(ch.Capsule(), style.SphereSegments))
	if len(charSegs) != wantCapEdges+1 {
		t.Fatalf("DrawCharacter emitted %d segments, want %d", len(charSegs), wantCapEdges+1)
	}

	// Non-hitting contact emits 0 ops.
	missContactOps := renderCanvasOps(t, 800, 600, func(d *core.Drawing) {
		physics3d.DrawContact(d, viewProj, vp, physics3d.Contact{Hit: false}, style)
	})
	if len(missContactOps) != 0 {
		t.Fatalf("non-hitting contact emitted %d ops, want 0", len(missContactOps))
	}

	// Raycast hit draws ray segment to hit.Point + normal segment + hit point arc.
	ray := physics3d.Ray{
		Origin:  physics3d.Vec3{X: -3, Y: 0.4, Z: 0},
		Dir:     physics3d.Vec3{X: 1, Y: 0, Z: 0},
		MaxDist: 6,
	}
	hit, ok := physics3d.RaycastSphere(ray, physics3d.Sphere{Center: physics3d.Vec3{Y: 0.4}, Radius: 0.6})
	if !ok {
		t.Fatal("expected ray to hit sphere")
	}
	rayHitOps := renderCanvasOps(t, 800, 600, func(d *core.Drawing) {
		physics3d.DrawRaycast(d, viewProj, vp, ray, hit, true, style)
	})
	rayHitSegs := extractSegments(t, rayHitOps)
	if len(rayHitSegs) != 2 {
		t.Fatalf("DrawRaycast(hit) emitted %d segments, want 2 (ray + normal)", len(rayHitSegs))
	}
	wantRayStart, _, _ := physics3d.Project(viewProj, vp, ray.Origin)
	wantRayEnd, _, _ := physics3d.Project(viewProj, vp, hit.Point)
	if !approx(rayHitSegs[0].x0, float32(wantRayStart.X), 1e-2) || !approx(rayHitSegs[0].y0, float32(wantRayStart.Y), 1e-2) ||
		!approx(rayHitSegs[0].x1, float32(wantRayEnd.X), 1e-2) || !approx(rayHitSegs[0].y1, float32(wantRayEnd.Y), 1e-2) {
		t.Fatalf("DrawRaycast(hit) ray segment misaligned: got (%v,%v)->(%v,%v), want (%v,%v)->(%v,%v)",
			rayHitSegs[0].x0, rayHitSegs[0].y0, rayHitSegs[0].x1, rayHitSegs[0].y1,
			wantRayStart.X, wantRayStart.Y, wantRayEnd.X, wantRayEnd.Y)
	}

	// Raycast miss draws only the full ray segment.
	rayMissOps := renderCanvasOps(t, 800, 600, func(d *core.Drawing) {
		physics3d.DrawRaycast(d, viewProj, vp, ray, physics3d.RaycastHit{}, false, style)
	})
	rayMissSegs := extractSegments(t, rayMissOps)
	if len(rayMissSegs) != 1 || rayMissSegs[0].color != style.RayColor {
		t.Fatalf("DrawRaycast(miss) emitted %+v, want 1 RayColor segment", rayMissSegs)
	}
}

func TestCompoundDebugDraw(t *testing.T) {
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 10, Y: 0.5, Z: 10}}),
		physics3d.Vec3{Y: -0.5},
	))
	hull, err := physics3d.NewConvexHull(rockPoints(3, 20, 0.3), 12)
	if err != nil {
		t.Fatalf("NewConvexHull: %v", err)
	}
	shape := physics3d.CompoundShape(
		physics3d.CompoundChild{Shape: physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.3, Y: 0.3, Z: 0.3}}), Offset: physics3d.Vec3{X: -1, Y: 0.25}},
		physics3d.CompoundChild{Shape: physics3d.SphereShape(physics3d.Sphere{Radius: 0.3}), Offset: physics3d.Vec3{X: 1, Y: 0.25}},
		physics3d.CompoundChild{Shape: physics3d.CapsuleShape(physics3d.VerticalCapsule(physics3d.Vec3{}, 0.2, 0.8)), Offset: physics3d.Vec3{Y: 1}},
		physics3d.CompoundChild{Shape: physics3d.ConvexHullShape(hull), Offset: physics3d.Vec3{Z: 1, Y: 0.4}},
		physics3d.CompoundChild{Shape: physics3d.CylinderShape(physics3d.VerticalCylinder(physics3d.Vec3{}, 0.2, 0.6)), Offset: physics3d.Vec3{Z: -1, Y: 0.4}},
	)
	b := w.AddBody(physics3d.NewDynamicBody(shape, physics3d.Vec3{}, 5))

	vp := core.Bounds{Size: core.Size{Width: 800, Height: 600}}
	view := physics3d.Mat4LookAt(physics3d.Vec3{X: 0, Y: 3, Z: 8}, physics3d.Vec3{}, physics3d.Vec3{Y: 1})
	proj := physics3d.Mat4Perspective(math.Pi/3, 800.0/600.0, 0.1, 100)
	viewProj := physics3d.Mat4Mul(proj, view)
	style := physics3d.DefaultDebugStyle()

	shapeOps := renderCanvasOps(t, 800, 600, func(d *core.Drawing) {
		physics3d.DrawShape(d, viewProj, vp, b.WorldShape(), style)
	})
	shapeSegs := extractSegments(t, shapeOps)
	if len(shapeSegs) == 0 {
		t.Fatal("DrawShape produced no segments for ShapeCompound")
	}
	seenColors := map[core.Color]bool{}
	for _, seg := range shapeSegs {
		seenColors[seg.color] = true
	}
	for _, want := range []core.Color{style.BoxColor, style.SphereColor, style.CapsuleColor, style.HullColor, style.CylinderColor} {
		if !seenColors[want] {
			t.Fatalf("DrawShape missing child color %v", want)
		}
	}

	worldOps := renderCanvasOps(t, 800, 600, func(d *core.Drawing) {
		physics3d.DrawWorld(d, viewProj, vp, w, style)
	})
	worldSegs := extractSegments(t, worldOps)
	if len(worldSegs) <= len(shapeSegs) {
		t.Fatalf("DrawWorld emitted %d segments, expected more than shape alone (%d)", len(worldSegs), len(shapeSegs))
	}
}
