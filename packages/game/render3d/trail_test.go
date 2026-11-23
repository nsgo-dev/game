package render3d_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/particles"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// helixTrail records a helix around +Y, so the ribbon's tangent sweeps through many directions relative to any camera.
func helixTrail(cfg render3d.TrailConfig) *render3d.Trail {
	tr := render3d.NewTrail(cfg)
	for i := range 24 {
		a := float64(i) * 0.4
		tr.AddPoint(math3d.Vec3{X: float32(2 * math.Cos(a)), Y: float32(i) * 0.25, Z: float32(2 * math.Sin(a))})
	}
	return tr
}

func TestTrailRibbonFacesCameraAtEveryAngle(t *testing.T) {
	tr := helixTrail(render3d.TrailConfig{Width: 0.5})
	const tol = 1e-4
	for _, ortho := range []bool{false, true} {
		for yawDeg := float32(0); yawDeg < 360; yawDeg += 20 {
			for pitchDeg := float32(-90); pitchDeg <= 90; pitchDeg += 15 {
				_, cam := sweepCamera(math3d.Vec3{Y: 3}, math3d.DegToRad(yawDeg), math3d.DegToRad(pitchDeg), 15, ortho)
				verts := tr.Build(cam)
				pts := tr.Points()
				if len(verts) != 2*len(pts) {
					t.Fatalf("Build returned %d vertices for %d points", len(verts), len(pts))
				}
				for i, p := range pts {
					l, r := verts[2*i], verts[2*i+1]
					if l.V != 0 || r.V != 1 || l.U != r.U {
						t.Fatalf("vertex pair %d has UV (%v,%v) (%v,%v)", i, l.U, l.V, r.U, r.V)
					}
					side := l.Pos.Sub(r.Pos)
					if !math3d.ApproxEqual(side.Len(), 0.5, tol) || !approxVec(l.Pos.Add(r.Pos).Scale(0.5), p.Pos, tol) {
						t.Fatalf("point %d edge %+v is not a 0.5-wide span centred on %+v", i, side, p.Pos)
					}
					toCam := cam.ToCamera(p.Pos)
					if d := side.Norm().Dot(toCam); math.Abs(float64(d)) > 1e-3 {
						t.Fatalf("point %d edge tilts toward the camera (dot %v) at yaw %v pitch %v", i, d, yawDeg, pitchDeg)
					}
					if !math3d.ApproxEqual(l.Normal.Len(), 1, tol) || l.Normal.Dot(toCam) < -tol {
						t.Fatalf("point %d normal %+v faces away from the camera (yaw %v pitch %v ortho %v)", i, l.Normal, yawDeg, pitchDeg, ortho)
					}
				}
			}
		}
	}
}

func TestTrailHeadingStraightAtCamera(t *testing.T) {
	_, cam := sweepCamera(math3d.Vec3Zero(), 0, 0, 10, false) // eye at z=10
	tr := render3d.NewTrail(render3d.TrailConfig{Width: 1})
	for z := float32(-4); z <= 0; z++ {
		tr.AddPoint(math3d.Vec3{Z: z})
	}
	verts := tr.Build(cam)
	if len(verts) != 10 {
		t.Fatalf("got %d vertices, want 10", len(verts))
	}
	for i, v := range verts {
		if !v.Pos.IsFinite() || !v.Normal.IsFinite() {
			t.Fatalf("vertex %d not finite: %+v", i, v)
		}
	}
	for i := 0; i < len(verts); i += 2 {
		if w := verts[i].Pos.Dist(verts[i+1].Pos); !math3d.ApproxEqual(w, 1, 1e-5) {
			t.Fatalf("pair %d width = %v, want 1", i/2, w)
		}
	}
}

func TestTrailRecordingAndExpiry(t *testing.T) {
	tr := render3d.NewTrail(render3d.TrailConfig{MaxPoints: 3, Lifetime: 100 * time.Millisecond, MinDistance: 0.5})
	tr.AddPoint(math3d.Vec3{})
	tr.AddPoint(math3d.Vec3{X: 0.2}) // closer than MinDistance
	tr.AddPoint(math3d.Vec3{X: float32(math.NaN())})
	tr.AddPoint(math3d.Vec3{X: 1})
	tr.AddPoint(math3d.Vec3{X: 3})
	if tr.PointCount() != 3 || tr.Points()[2].Length != 3 {
		t.Fatalf("points = %+v", tr.Points())
	}
	tr.AddPoint(math3d.Vec3{X: 4}) // drops the oldest and rebases lengths
	pts := tr.Points()
	if len(pts) != 3 || pts[0].Pos.X != 1 || pts[0].Length != 0 || pts[2].Length != 3 {
		t.Fatalf("after overflow points = %+v", pts)
	}

	tr.Update(60 * time.Millisecond)
	tr.AddPoint(math3d.Vec3{X: 4}) // a duplicate refreshes the head's age
	if pts := tr.Points(); pts[2].Age != 0 || pts[0].Age != 60*time.Millisecond {
		t.Fatalf("ages = %+v", pts)
	}
	tr.Update(0)
	tr.Update(-time.Second)
	tr.Update(50 * time.Millisecond)
	if pts := tr.Points(); len(pts) != 1 || pts[0].Pos.X != 4 || pts[0].Length != 0 {
		t.Fatalf("after expiry points = %+v", pts)
	}
	if tr.Build(render3d.CameraBasis{}) != nil {
		t.Fatal("Build with one point returned vertices")
	}
	tr.Clear()
	if tr.PointCount() != 0 {
		t.Fatal("Clear kept points")
	}

	// A huge dt expires everything instead of wrapping ages negative.
	tr = render3d.NewTrail(render3d.TrailConfig{})
	tr.AddPoint(math3d.Vec3{})
	tr.AddPoint(math3d.Vec3{X: 1})
	tr.Update(time.Millisecond)
	tr.AddPoint(math3d.Vec3{X: 2})
	tr.Update(math.MaxInt64)
	if tr.PointCount() != 0 {
		t.Fatalf("Update(MaxInt64) kept points %+v", tr.Points())
	}

	tr = helixTrail(render3d.TrailConfig{})
	tr.SetConfig(render3d.TrailConfig{MaxPoints: 5})
	if tr.PointCount() != 5 || tr.Points()[0].Length != 0 || tr.Config().MaxPoints != 5 {
		t.Fatalf("SetConfig trim: %d points, first %+v", tr.PointCount(), tr.Points()[0])
	}
}

func TestTrailWidthColorAndBounds(t *testing.T) {
	head, tail := core.Color(0xff8000ff), core.Color(0x0040ff00)
	cfg := render3d.TrailConfig{
		Width:         2,
		WidthCurve:    particles.Curve{{T: 0, Value: 1}, {T: 1, Value: 0}},
		ColorGradient: particles.ColorCurve{{T: 0, Color: head}, {T: 1, Color: tail}},
	}
	tr := render3d.NewTrail(cfg)
	cfg.WidthCurve[0].Value = 9 // the trail keeps its own copy
	for x := float32(0); x <= 4; x++ {
		tr.AddPoint(math3d.Vec3{X: x})
	}
	if w, c := tr.EvaluateAt(0); w != 2 || c != head {
		t.Fatalf("head = %v %v", w, c)
	}
	if w, c := tr.EvaluateAt(2); w != 0 || c != tail {
		t.Fatalf("tail (clamped) = %v %v", w, c)
	}
	_, cam := sweepCamera(math3d.Vec3{X: 2}, 0, 0, 10, false)
	verts := tr.Build(cam)
	if verts[0].U != 1 || verts[8].U != 0 || verts[8].Color != head || verts[0].Pos != verts[1].Pos {
		t.Fatalf("tail %+v head %+v", verts[0], verts[8])
	}
	if w := verts[8].Pos.Dist(verts[9].Pos); !math3d.ApproxEqual(w, 2, 1e-5) {
		t.Fatalf("head width = %v, want 2", w)
	}

	box := tr.Bounds()
	if !approxVec(box.Min, math3d.Vec3{X: -1, Y: -1, Z: -1}, 1e-6) || !approxVec(box.Max, math3d.Vec3{X: 5, Y: 1, Z: 1}, 1e-6) {
		t.Fatalf("Bounds = %+v", box)
	}
	item := tr.DrawItem(render3d.DrawItem{ID: 4})
	if item.ID != 4 || item.Blend != render3d.BlendAlphaBlend || item.Triangles != 8 || item.Vertices != 10 || item.WorldBounds != box || item.Transform != math3d.Mat4Identity() {
		t.Fatalf("DrawItem = %+v", item)
	}
	if b := render3d.NewTrail(render3d.TrailConfig{}).Bounds(); !b.Empty() {
		t.Fatalf("empty trail bounds = %+v", b)
	}

	// Defaults: 0.25 world units wide, opaque white.
	def := render3d.NewTrail(render3d.TrailConfig{})
	if w, c := def.EvaluateAt(0.5); w != 0.25 || c != core.RGB(0xffffff) {
		t.Fatalf("defaults = %v %v", w, c)
	}

	// Age fades the ribbon toward its tail values.
	aged := render3d.NewTrail(render3d.TrailConfig{Width: 2, Lifetime: time.Second, WidthCurve: particles.Curve{{T: 0, Value: 1}, {T: 1, Value: 0}}})
	aged.AddPoint(math3d.Vec3{})
	aged.AddPoint(math3d.Vec3{X: 1})
	aged.Update(500 * time.Millisecond)
	v := aged.Build(cam)
	if w := v[2].Pos.Dist(v[3].Pos); !math3d.ApproxEqual(w, 1, 1e-5) {
		t.Fatalf("half-aged head width = %v, want 1", w)
	}
}

func TestTrailDraw(t *testing.T) {
	sc, cam := sweepCamera(math3d.Vec3Zero(), 0, 0, 10, false)
	vp := sc.ViewProj()
	viewport := core.Bounds{Size: core.Size{Width: 320, Height: 180}}

	solid := render3d.NewTrail(render3d.TrailConfig{Width: 0.5, Color: core.Color(0x112233ff)})
	for x := float32(-2); x <= 2; x++ {
		solid.AddPoint(math3d.Vec3{X: x})
	}
	grad := render3d.NewTrail(render3d.TrailConfig{Width: 0.5, ColorGradient: particles.ColorCurve{{T: 0, Color: core.Color(0xff0000ff)}, {T: 1, Color: core.Color(0x0000ffff)}}})
	for x := float32(-2); x <= 2; x++ {
		grad.AddPoint(math3d.Vec3{X: x})
	}
	// Runs from in front of the camera to behind it: the segments behind are dropped, the crossing one clipped.
	crossing := render3d.NewTrail(render3d.TrailConfig{Width: 0.5, Color: core.Color(0x445566ff)})
	for z := float32(0); z <= 14; z += 2 {
		crossing.AddPoint(math3d.Vec3{Y: -1, Z: z})
	}

	var nSolid, nGrad, nCross int
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "solid", Style: core.Style{Width: core.Pt(320), Height: core.Pt(180)}, Draw: func(d *core.Drawing, _ core.Size) {
				nSolid = solid.Draw(d, cam, vp, viewport)
			}},
			core.Canvas{TestID: "grad", Style: core.Style{Width: core.Pt(320), Height: core.Pt(180)}, Draw: func(d *core.Drawing, _ core.Size) {
				nGrad = grad.Draw(d, cam, vp, viewport)
			}},
			core.Canvas{TestID: "crossing", Style: core.Style{Width: core.Pt(320), Height: core.Pt(180)}, Draw: func(d *core.Drawing, _ core.Size) {
				nCross = crossing.Draw(d, cam, vp, viewport)
			}},
		}}
	})
	if nSolid != 4 || nGrad != 4 {
		t.Fatalf("segments drawn: solid %d gradient %d, want 4", nSolid, nGrad)
	}
	count := func(id string, kind core.DrawKind) int {
		n := 0
		for _, op := range app.Find(id).Ops {
			if op.Kind == kind {
				n++
			}
		}
		return n
	}
	if count("solid", core.DrawFill) != 4 || count("solid", core.DrawFillColor) != 4 {
		t.Fatal("solid trail did not fill 4 colored segments")
	}
	if count("grad", core.DrawFillGradient) != 4 {
		t.Fatalf("gradient fills = %d, want 4", count("grad", core.DrawFillGradient))
	}
	// Points at z=0..8 are in front of the near plane (eye z=10, near 0.1); the 8→10 segment is clipped, 10→14 is behind.
	if nCross != 5 {
		t.Fatalf("crossing segments drawn = %d, want 5", nCross)
	}
	for _, op := range app.Find("crossing").Ops {
		if (op.Kind == core.DrawMoveTo || op.Kind == core.DrawLineTo) && (op.Y < 90 || isNaN(op.Y)) {
			t.Fatalf("clipped ribbon vertex escaped above the horizon: %+v", op)
		}
	}
}

func TestTrailBuildDoesNotAllocate(t *testing.T) {
	tr := helixTrail(render3d.TrailConfig{})
	_, cam := sweepCamera(math3d.Vec3{Y: 3}, 0.3, 0.2, 15, false)
	tr.Build(cam)
	if allocs := testing.AllocsPerRun(50, func() { tr.Build(cam) }); allocs != 0 {
		t.Fatalf("Build allocates %v times per call", allocs)
	}
}

func BenchmarkTrailBuild(b *testing.B) {
	tr := helixTrail(render3d.TrailConfig{})
	_, cam := sweepCamera(math3d.Vec3{Y: 3}, 0.3, 0.2, 15, false)
	b.ReportAllocs()
	for b.Loop() {
		tr.Build(cam)
	}
}
