package render3d_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// sweepCamera places a perspective camera dist away from target, rotated by yaw and pitch (intrinsic YXZ, like FPSController), so pitch ±90° looks straight down and up without a degenerate look-at.
func sweepCamera(target math3d.Vec3, yaw, pitch, dist float32, ortho bool) (*scene3d.Camera, render3d.CameraBasis) {
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3Zero(), math3d.DegToRad(60), 16.0/9.0, 0.1, 100)
	if ortho {
		cam = scene3d.NewOrthographicCamera(math3d.Vec3Zero(), 10, 16.0/9.0, 0.1, 100)
	}
	cam.Rot = math3d.QuatFromEuler(pitch, yaw, 0)
	cam.Pos = target.Sub(cam.Forward().Scale(dist))
	return cam, render3d.CameraBasis{
		Pos:          cam.Pos,
		Right:        cam.Right(),
		Up:           cam.Up(),
		Forward:      cam.Forward(),
		Orthographic: ortho,
	}
}

func approxVec(a, b math3d.Vec3, tol float32) bool { return a.ApproxEqual(b, tol) }

// checkFrame asserts q's axes are a unit right-handed orthonormal frame and its corners match its size.
func checkFrame(t *testing.T, label string, q render3d.BillboardQuad, w, h float32) {
	t.Helper()
	const tol = 1e-4
	for name, v := range map[string]math3d.Vec3{"right": q.Right, "up": q.Up, "normal": q.Normal} {
		if !v.IsFinite() || !math3d.ApproxEqual(v.Len(), 1, tol) {
			t.Fatalf("%s: %s = %+v, want a finite unit vector", label, name, v)
		}
	}
	if math.Abs(float64(q.Right.Dot(q.Up))) > tol || math.Abs(float64(q.Right.Dot(q.Normal))) > tol || math.Abs(float64(q.Up.Dot(q.Normal))) > tol {
		t.Fatalf("%s: axes not orthogonal: %+v", label, q)
	}
	if !approxVec(q.Right.Cross(q.Up), q.Normal, tol) {
		t.Fatalf("%s: right × up = %+v, want normal %+v", label, q.Right.Cross(q.Up), q.Normal)
	}
	if got := q.Corners[1].Dist(q.Corners[0]); !math3d.ApproxEqual(got, w, tol*10) {
		t.Fatalf("%s: bottom edge = %v, want %v", label, got, w)
	}
	if got := q.Corners[3].Dist(q.Corners[0]); !math3d.ApproxEqual(got, h, tol*10) {
		t.Fatalf("%s: left edge = %v, want %v", label, got, h)
	}
}

func TestBillboardFacesCameraAtEveryAngle(t *testing.T) {
	centers := []math3d.Vec3{{}, {X: 3, Y: -2, Z: 1}, {X: -4, Y: 5, Z: -6}}
	for _, ortho := range []bool{false, true} {
		for yawDeg := float32(0); yawDeg < 360; yawDeg += 15 {
			for pitchDeg := float32(-90); pitchDeg <= 90; pitchDeg += 15 {
				for _, target := range centers {
					yaw, pitch := math3d.DegToRad(yawDeg), math3d.DegToRad(pitchDeg)
					_, cam := sweepCamera(math3d.Vec3Zero(), yaw, pitch, 12, ortho)
					for _, roll := range []float32{0, 0.7, -2.5} {
						b := render3d.Billboard{Center: target, Width: 2, Height: 1, Rotation: roll}

						b.Mode = render3d.BillboardViewPlane
						q, ok := b.Quad(cam)
						if !ok {
							t.Fatalf("view-plane quad not built at yaw %v pitch %v", yawDeg, pitchDeg)
						}
						checkFrame(t, "view-plane", q, 2, 1)
						if !approxVec(q.Normal, cam.Forward.Neg(), 1e-4) {
							t.Fatalf("view-plane normal %+v at yaw %v pitch %v, want -forward %+v", q.Normal, yawDeg, pitchDeg, cam.Forward.Neg())
						}

						b.Mode = render3d.BillboardViewPoint
						q, ok = b.Quad(cam)
						if !ok {
							t.Fatalf("view-point quad not built at yaw %v pitch %v", yawDeg, pitchDeg)
						}
						checkFrame(t, "view-point", q, 2, 1)
						if dot := q.Normal.Dot(cam.ToCamera(q.Center)); !math3d.ApproxEqual(dot, 1, 1e-4) {
							t.Fatalf("view-point normal · toCamera = %v at yaw %v pitch %v ortho %v, want 1", dot, yawDeg, pitchDeg, ortho)
						}
					}
				}
			}
		}
	}
}

func TestBillboardRollIsCounterClockwiseOnScreen(t *testing.T) {
	_, cam := sweepCamera(math3d.Vec3Zero(), 0, 0, 10, false)
	q, _ := render3d.Billboard{Width: 1, Height: 1, Rotation: math.Pi / 2}.Quad(cam)
	// A quarter turn counter-clockwise points the quad's right edge along the camera's up axis.
	if !approxVec(q.Right, cam.Up, 1e-5) || !approxVec(q.Up, cam.Right.Neg(), 1e-5) {
		t.Fatalf("rolled axes right %+v up %+v, want camera up and -right", q.Right, q.Up)
	}
	b := render3d.Billboard{Width: 1, Height: 1, Rotation: 1, Mode: render3d.BillboardAxial}
	q, _ = b.Quad(cam)
	if !approxVec(q.Up, math3d.Vec3Up(), 1e-6) {
		t.Fatalf("axial quad rolled: up = %+v", q.Up)
	}
}

func TestBillboardAxialStaysOnItsAxis(t *testing.T) {
	axes := []math3d.Vec3{{}, math3d.Vec3UnitY(), {X: 1, Y: 1}, math3d.Vec3UnitX().Neg()}
	for _, axis := range axes {
		want := math3d.Vec3Up()
		if axis.LenSq() > 0 {
			want = axis.Norm()
		}
		for yawDeg := float32(0); yawDeg < 360; yawDeg += 30 {
			for pitchDeg := float32(-90); pitchDeg <= 90; pitchDeg += 15 {
				_, cam := sweepCamera(math3d.Vec3{X: 1}, math3d.DegToRad(yawDeg), math3d.DegToRad(pitchDeg), 8, false)
				b := render3d.Billboard{Center: math3d.Vec3{X: 1}, Width: 1, Height: 3, Mode: render3d.BillboardAxial, Axis: axis}
				q, ok := b.Quad(cam)
				if !ok {
					t.Fatalf("axial quad not built")
				}
				checkFrame(t, "axial", q, 1, 3)
				if !approxVec(q.Up, want, 1e-5) {
					t.Fatalf("axial up = %+v, want axis %+v", q.Up, want)
				}
				toCam := cam.ToCamera(q.Center)
				proj := toCam.Sub(want.Scale(toCam.Dot(want)))
				if proj.Len() > 1e-3 {
					// Away from the axis the quad must face the camera as far as the axis allows.
					if dot := q.Normal.Dot(proj.Norm()); !math3d.ApproxEqual(dot, 1, 1e-4) {
						t.Fatalf("axial normal %+v does not face projected view %+v (axis %+v, yaw %v, pitch %v)", q.Normal, proj, axis, yawDeg, pitchDeg)
					}
				}
			}
		}
	}
}

func TestBillboardPivotTransformAndBounds(t *testing.T) {
	_, cam := sweepCamera(math3d.Vec3Zero(), 0, 0, 10, false)
	b := render3d.Billboard{Center: math3d.Vec3{X: 1, Y: 2, Z: 3}, Width: 4, Height: 2, Pivot: math3d.Vec2{Y: -0.5}}
	q, ok := b.Quad(cam)
	if !ok {
		t.Fatal("quad not built")
	}
	wantCorners := [4]math3d.Vec3{{X: -1, Y: 2, Z: 3}, {X: 3, Y: 2, Z: 3}, {X: 3, Y: 4, Z: 3}, {X: -1, Y: 4, Z: 3}}
	for i, c := range q.Corners {
		if !approxVec(c, wantCorners[i], 1e-5) {
			t.Fatalf("corner %d = %+v, want %+v", i, c, wantCorners[i])
		}
	}
	m := q.Transform()
	unit := [4]math3d.Vec3{{X: -0.5, Y: -0.5}, {X: 0.5, Y: -0.5}, {X: 0.5, Y: 0.5}, {X: -0.5, Y: 0.5}}
	for i, u := range unit {
		if got := m.TransformPoint(u); !approxVec(got, q.Corners[i], 1e-5) {
			t.Fatalf("Transform maps %+v to %+v, want corner %+v", u, got, q.Corners[i])
		}
	}
	box := q.Bounds()
	if !approxVec(box.Min, math3d.Vec3{X: -1, Y: 2, Z: 3}, 1e-5) || !approxVec(box.Max, math3d.Vec3{X: 3, Y: 4, Z: 3}, 1e-5) {
		t.Fatalf("Bounds = %+v", box)
	}
	item := q.DrawItem(render3d.DrawItem{ID: 9, MaterialID: 2})
	if item.ID != 9 || item.MaterialID != 2 || item.Blend != render3d.BlendAlphaBlend || item.Triangles != 2 || item.Vertices != 4 || item.WorldBounds != box {
		t.Fatalf("DrawItem = %+v", item)
	}
	if item := q.DrawItem(render3d.DrawItem{Blend: render3d.BlendAlphaMask}); item.Blend != render3d.BlendAlphaMask {
		t.Fatalf("DrawItem replaced an explicit blend: %v", item.Blend)
	}
}

func TestBillboardRejectsInvalidInput(t *testing.T) {
	_, cam := sweepCamera(math3d.Vec3Zero(), 0, 0, 10, false)
	inf := float32(math.Inf(1))
	nan := float32(math.NaN())
	for _, b := range []render3d.Billboard{
		{Width: 0, Height: 1},
		{Width: 1, Height: -1},
		{Width: inf, Height: 1},
		{Width: nan, Height: 1},
		{Width: 1, Height: 1, Center: math3d.Vec3{X: nan}},
	} {
		if _, ok := b.Quad(cam); ok {
			t.Fatalf("Quad accepted %+v", b)
		}
	}
	// Bad pivot and rotation fall back to zero instead of poisoning the quad.
	q, ok := render3d.Billboard{Width: 1, Height: 1, Pivot: math3d.Vec2{X: nan}, Rotation: inf}.Quad(cam)
	if !ok || !q.Center.IsFinite() || !approxVec(q.Center, math3d.Vec3Zero(), 1e-6) {
		t.Fatalf("Quad with bad pivot = %+v, %v", q, ok)
	}
	// A broken camera basis still yields an orthonormal frame.
	q, ok = render3d.Billboard{Width: 1, Height: 1}.Quad(render3d.CameraBasis{})
	if !ok {
		t.Fatal("Quad failed with a zero camera basis")
	}
	checkFrame(t, "zero basis", q, 1, 1)
}

func TestNewCameraBasis(t *testing.T) {
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 2, Y: 3, Z: 9}, math3d.DegToRad(60), 1, 0.1, 100)
	cam.LookAt(math3d.Vec3{X: -1, Y: 0, Z: 0}, math3d.Vec3Up())
	basis, ok := render3d.NewCameraBasis(cam.ViewMatrix(), cam.ProjMatrix())
	if !ok {
		t.Fatal("NewCameraBasis failed")
	}
	if !approxVec(basis.Pos, cam.Pos, 1e-4) || !approxVec(basis.Right, cam.Right(), 1e-5) || !approxVec(basis.Up, cam.Up(), 1e-5) || !approxVec(basis.Forward, cam.Forward(), 1e-5) || basis.Orthographic {
		t.Fatalf("basis = %+v", basis)
	}
	ortho := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 5}, 10, 1, 0.1, 100)
	basis, ok = render3d.NewCameraBasis(ortho.ViewMatrix(), ortho.ProjMatrix())
	if !ok || !basis.Orthographic {
		t.Fatalf("orthographic basis = %+v, %v", basis, ok)
	}
	if got := basis.ToCamera(math3d.Vec3{X: 7, Y: -3}); !approxVec(got, math3d.Vec3UnitZ(), 1e-6) {
		t.Fatalf("orthographic ToCamera = %+v, want +Z", got)
	}
	if _, ok := render3d.NewCameraBasis(math3d.Mat4{}, cam.ProjMatrix()); ok {
		t.Fatal("NewCameraBasis accepted a singular view")
	}
	persp := render3d.CameraBasis{Pos: math3d.Vec3{Z: 4}, Forward: math3d.Vec3Forward()}
	if got := persp.ToCamera(persp.Pos); !approxVec(got, math3d.Vec3UnitZ(), 1e-6) {
		t.Fatalf("ToCamera at the eye = %+v, want -forward", got)
	}
}

func TestBillboardModeString(t *testing.T) {
	for m, want := range map[render3d.BillboardMode]string{
		render3d.BillboardViewPlane: "view_plane",
		render3d.BillboardViewPoint: "view_point",
		render3d.BillboardAxial:     "axial",
		render3d.BillboardMode(99):  "unknown",
	} {
		if got := m.String(); got != want {
			t.Fatalf("%d.String() = %q, want %q", m, got, want)
		}
	}
}

func fillColors(ops []core.DrawOp) []core.Color {
	var out []core.Color
	for _, op := range ops {
		if op.Kind == core.DrawFillColor {
			out = append(out, op.Color)
		}
	}
	return out
}

func TestBillboardRendererDraw(t *testing.T) {
	sc, cam := sweepCamera(math3d.Vec3Zero(), 0, 0, 10, false) // eye at z=10 looking down -Z
	vp := sc.ViewProj()
	viewport := core.Bounds{Size: core.Size{Width: 320, Height: 180}}
	near := core.Color(0x00ff00ff)
	far := core.Color(0xff0000ff)
	mid := core.Color(0x0000ffff)
	items := []render3d.Billboard{
		{Center: math3d.Vec3{Z: 5}, Width: 1, Height: 1, Color: near},
		{Center: math3d.Vec3{Z: -20}, Width: 1, Height: 1, Color: far},
		{Center: math3d.Vec3{Z: 0}, Width: 1, Height: 1, Color: mid},
		{Center: math3d.Vec3{Z: 30}, Width: 1, Height: 1, Color: core.Color(0xffffffff)}, // behind the eye
		{Center: math3d.Vec3{Z: 0}, Width: 1, Height: 1, Color: core.Color(0xffffff00)},  // fully transparent
		{Center: math3d.Vec3{Z: 0}, Width: 0, Height: 1},                                 // not built
	}
	// A quad lying along the view axis that straddles the near plane gets clipped instead of wrapping around the screen.
	straddle := []render3d.Billboard{{Center: math3d.Vec3{Y: -1, Z: 10}, Width: 1, Height: 4, Mode: render3d.BillboardAxial, Axis: math3d.Vec3UnitZ(), Color: mid}}
	textured := []render3d.Billboard{{Center: math3d.Vec3{}, Width: 2, Height: 2, Texture: core.ImageSource{URI: "asset:spark.png"}, Color: core.Color(0xffffff80)}}

	var r render3d.BillboardRenderer
	var drawn, drawnStraddle, drawnTex int
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "sorted", Style: core.Style{Width: core.Pt(320), Height: core.Pt(180)}, Draw: func(d *core.Drawing, _ core.Size) {
				drawn = r.Draw(d, items, cam, vp, viewport)
			}},
			core.Canvas{TestID: "straddle", Style: core.Style{Width: core.Pt(320), Height: core.Pt(180)}, Draw: func(d *core.Drawing, _ core.Size) {
				drawnStraddle = r.Draw(d, straddle, cam, vp, viewport)
			}},
			core.Canvas{TestID: "textured", Style: core.Style{Width: core.Pt(320), Height: core.Pt(180)}, Draw: func(d *core.Drawing, _ core.Size) {
				drawnTex = r.Draw(d, textured, cam, vp, viewport)
			}},
		}}
	})

	if drawn != 3 {
		t.Fatalf("drawn = %d, want 3", drawn)
	}
	got := fillColors(app.Find("sorted").Ops)
	want := []core.Color{far, mid, near}
	if len(got) != len(want) {
		t.Fatalf("fill colors = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("draw order = %v, want back to front %v", got, want)
		}
	}
	// The middle quad at the origin is 1×1 world units; check its projected corners against the camera.
	var xs, ys []float32
	ops := app.Find("sorted").Ops
	fills := 0
	for _, op := range ops {
		if op.Kind == core.DrawFill {
			fills++
		}
		if fills == 1 && (op.Kind == core.DrawMoveTo || op.Kind == core.DrawLineTo) {
			xs, ys = append(xs, op.X), append(ys, op.Y)
		}
	}
	if len(xs) != 4 {
		t.Fatalf("middle quad has %d vertices, want 4", len(xs))
	}
	for i, c := range []math3d.Vec3{{X: -0.5, Y: -0.5}, {X: 0.5, Y: -0.5}, {X: 0.5, Y: 0.5}, {X: -0.5, Y: 0.5}} {
		s, _, _ := sc.WorldToScreen(c, 320, 180)
		if !math3d.ApproxEqual(xs[i], s.X, 1e-2) || !math3d.ApproxEqual(ys[i], s.Y, 1e-2) {
			t.Fatalf("corner %d at (%v, %v), want (%v, %v)", i, xs[i], ys[i], s.X, s.Y)
		}
	}

	if drawnStraddle != 1 {
		t.Fatalf("straddling quad drawn = %d, want 1", drawnStraddle)
	}
	for _, op := range app.Find("straddle").Ops {
		if (op.Kind == core.DrawMoveTo || op.Kind == core.DrawLineTo) && (op.Y < 89 || op.Y > 1e5 || isNaN(op.X)) {
			t.Fatalf("clipped quad vertex escaped below the horizon: %+v", op)
		}
	}

	if drawnTex != 1 {
		t.Fatalf("textured drawn = %d, want 1", drawnTex)
	}
	var images, clips, transforms int
	var alpha float32
	for _, op := range app.Find("textured").Ops {
		switch op.Kind {
		case core.DrawImage:
			images++
		case core.DrawClip:
			clips++
		case core.DrawTransform:
			transforms++
		case core.DrawAlpha:
			alpha = op.A
		}
	}
	if images != 1 || clips != 1 || transforms != 1 || !math3d.ApproxEqual(alpha, 128.0/255, 1e-3) {
		t.Fatalf("textured ops: images %d clips %d transforms %d alpha %v", images, clips, transforms, alpha)
	}
}

func isNaN(v float32) bool { return v != v }

func FuzzBillboardFacesCamera(f *testing.F) {
	f.Add(float32(0), float32(0), float32(0), float32(0), float32(0), float32(0), uint8(0), false)
	f.Add(float32(1.3), float32(math.Pi/2), float32(0.4), float32(3), float32(-1), float32(2), uint8(1), false)
	f.Add(float32(-2), float32(-math.Pi/2), float32(9), float32(-4), float32(5), float32(-6), uint8(2), true)
	f.Fuzz(func(t *testing.T, yaw, pitch, roll, cx, cy, cz float32, mode uint8, ortho bool) {
		for _, v := range []float32{yaw, pitch, roll, cx, cy, cz} {
			if isNaN(v) || math.IsInf(float64(v), 0) || math.Abs(float64(v)) > 1e3 {
				t.Skip()
			}
		}
		center := math3d.Vec3{X: cx, Y: cy, Z: cz}
		_, cam := sweepCamera(center.Add(math3d.Vec3{X: 0.5}), yaw, pitch, 9, ortho)
		b := render3d.Billboard{Center: center, Width: 1.5, Height: 0.5, Rotation: roll, Mode: render3d.BillboardMode(mode % 3)}
		q, ok := b.Quad(cam)
		if !ok {
			t.Fatalf("quad not built for %+v", b)
		}
		checkFrame(t, b.Mode.String(), q, 1.5, 0.5)
		toCam := cam.ToCamera(q.Center)
		dot := q.Normal.Dot(toCam)
		switch b.Mode {
		case render3d.BillboardViewPoint:
			if !math3d.ApproxEqual(dot, 1, 1e-3) {
				t.Fatalf("view-point normal · toCamera = %v", dot)
			}
		case render3d.BillboardViewPlane:
			if !approxVec(q.Normal, cam.Forward.Neg(), 1e-4) {
				t.Fatalf("view-plane normal = %+v, want %+v", q.Normal, cam.Forward.Neg())
			}
		default:
			if dot < -1e-4 {
				t.Fatalf("axial normal faces away: %v", dot)
			}
		}
	})
}

func BenchmarkBillboardQuad(b *testing.B) {
	_, cam := sweepCamera(math3d.Vec3Zero(), 0.4, 0.3, 10, false)
	bb := render3d.Billboard{Center: math3d.Vec3{X: 1, Y: 2}, Width: 1, Height: 1, Mode: render3d.BillboardViewPoint, Rotation: 0.3}
	b.ReportAllocs()
	for b.Loop() {
		if _, ok := bb.Quad(cam); !ok {
			b.Fatal("quad not built")
		}
	}
}
