package render3d_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/nsgo/core"
)

func TestProjectSegmentNOKeepsNearHalfOfNOProjection(t *testing.T) {
	// OpenGL [-1, 1] projection: a point just past the near plane has clip z < 0 but is visible.
	proj := math3d.PerspectiveNO(1.0, 1, 1, 100)
	view := math3d.LookAt(math3d.Vec3{}, math3d.Vec3{Z: -1}, math3d.Vec3Up())
	vp := proj.Mul(view)
	a := math3d.Vec3{X: -0.1, Z: -1.2}
	b := math3d.Vec3{X: 0.1, Z: -1.2}
	if c := vp.MulVec4(a.Vec4(1)); c.Z >= 0 {
		t.Fatalf("test point has clip z %v; it must lie in the near half of the NO depth range", c.Z)
	}
	viewport := core.Bounds{Size: core.Size{Width: 100, Height: 100}}
	if _, _, _, _, ok := render3d.ProjectSegmentNO(a, b, vp, viewport); !ok {
		t.Fatal("segment in front of an NO camera's near plane was clipped")
	}
	// Behind the camera is still clipped.
	if _, _, _, _, ok := render3d.ProjectSegmentNO(math3d.Vec3{Z: 1}, math3d.Vec3{X: 1, Z: 1}, vp, viewport); ok {
		t.Fatal("segment behind the camera was drawn")
	}
}

func TestProjectSegmentClipsBehindOrthoCamera(t *testing.T) {
	// [0, 1] orthographic projection: w is 1 everywhere, so only clip z >= 0 marks the near plane.
	proj := math3d.Ortho(-10, 10, -10, 10, 0.1, 100)
	view := math3d.LookAt(math3d.Vec3{}, math3d.Vec3{Z: -1}, math3d.Vec3Up())
	vp := proj.Mul(view)
	viewport := core.Bounds{Size: core.Size{Width: 100, Height: 100}}
	if _, _, _, _, ok := render3d.ProjectSegment(math3d.Vec3{X: -1, Z: 50}, math3d.Vec3{X: 1, Z: 50}, vp, viewport); ok {
		t.Fatal("segment 50 units behind an orthographic camera was drawn")
	}
	if _, _, _, _, ok := render3d.ProjectSegment(math3d.Vec3{X: -1, Z: -50}, math3d.Vec3{X: 1, Z: -50}, vp, viewport); !ok {
		t.Fatal("segment 50 units in front of an orthographic camera was clipped")
	}
	// A segment crossing the near plane is cut there, not dropped.
	if _, _, _, _, ok := render3d.ProjectSegment(math3d.Vec3{Z: 50}, math3d.Vec3{Z: -50}, vp, viewport); !ok {
		t.Fatal("segment crossing an orthographic camera's near plane was dropped")
	}
}

func TestGizmoBehindOrthoCameraDrawsNothing(t *testing.T) {
	proj := math3d.Ortho(-10, 10, -10, 10, 0.1, 100)
	view := math3d.LookAt(math3d.Vec3{}, math3d.Vec3{Z: -1}, math3d.Vec3Up())
	vp := proj.Mul(view)
	viewport := core.Bounds{Size: core.Size{Width: 100, Height: 100}}
	var g render3d.Gizmo
	for _, s := range g.Segments(math3d.Vec3{Z: 50}, 1, render3d.GizmoTranslate) {
		if _, _, _, _, ok := render3d.ProjectSegment(s.A, s.B, vp, viewport); ok {
			t.Fatalf("gizmo segment %+v behind an orthographic camera projects as visible", s)
		}
	}
}

func TestAppendSegmentsReusesBuffer(t *testing.T) {
	var g render3d.Gizmo
	buf := make([]render3d.GizmoSegment, 0, 128)
	for _, mode := range []render3d.GizmoMode{render3d.GizmoTranslate, render3d.GizmoRotate, render3d.GizmoScale} {
		allocs := testing.AllocsPerRun(20, func() {
			buf = g.AppendSegments(buf[:0], math3d.Vec3{}, 1, mode)
		})
		if allocs != 0 {
			t.Fatalf("mode %v: AppendSegments allocated %v times per call", mode, allocs)
		}
		if len(buf) != len(g.Segments(math3d.Vec3{}, 1, mode)) {
			t.Fatalf("mode %v: AppendSegments and Segments disagree", mode)
		}
	}
}

func TestPlanarHandleDrawnWherePicked(t *testing.T) {
	var g render3d.Gizmo
	var lo, hi float32 = 1e9, -1e9
	for _, s := range g.Segments(math3d.Vec3{}, 1, render3d.GizmoTranslate) {
		if s.Axis != render3d.AxisXY {
			continue
		}
		for _, p := range []math3d.Vec3{s.A, s.B} {
			lo = min(lo, p.X, p.Y)
			hi = max(hi, p.X, p.Y)
		}
	}
	inside := func(u, v float32) bool {
		axis, _ := g.PickAxis(math3d.Ray{Origin: math3d.Vec3{X: u, Y: v, Z: 5}, Dir: math3d.Vec3{Z: -1}}, math3d.Vec3{}, 1, render3d.GizmoTranslate)
		return axis == render3d.AxisXY
	}
	// The corner nearest the origin lies within the axis shafts' pick tolerance, which take priority, so probe the middle and the outer corner.
	mid := (lo + hi) / 2
	if !inside(mid, mid) || !inside(hi-1e-3, hi-1e-3) || !inside(hi-1e-3, mid) {
		t.Fatalf("drawn square [%v, %v] is not pickable throughout", lo, hi)
	}
	if inside(hi+0.02, hi+0.02) {
		t.Fatalf("pick extends past the drawn square [%v, %v]", lo, hi)
	}
}

func TestTiedSortKeysKeepSubmissionOrder(t *testing.T) {
	box := math3d.AABB{Min: math3d.Vec3{X: -0.5, Y: -0.5, Z: -5.5}, Max: math3d.Vec3{X: 0.5, Y: 0.5, Z: -4.5}}
	items := make([]render3d.DrawItem, 40)
	for i := range items {
		items[i] = render3d.DrawItem{
			MeshID:      1,
			Blend:       render3d.BlendAlphaBlend,
			WorldBounds: box,
			Transform:   math3d.Translate(math3d.Vec3{X: float32(i)}),
		}
	}
	proj := math3d.Perspective(1.0, 1, 0.1, 100)
	view := math3d.LookAt(math3d.Vec3{}, math3d.Vec3{Z: -1}, math3d.Vec3Up())
	var q render3d.Queue
	plan := q.Build(proj.Mul(view), math3d.Vec3{}, items)
	for i, d := range plan.Transparent {
		if d.Transform != items[i].Transform {
			t.Fatalf("tied transparent draw %d came out of submission order", i)
		}
	}
}
