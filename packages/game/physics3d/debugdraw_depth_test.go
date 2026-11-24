//go:build !release

package physics3d_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/physics3d"
	"github.com/nsgo-dev/nsgo/core"
)

// orthoZOViewProj is a [0, 1] depth orthographic camera at the origin looking down -Z, built with math3d as a render3d scene would.
func orthoZOViewProj() physics3d.Mat4 {
	proj := math3d.Ortho(-10, 10, -10, 10, 0.1, 100)
	view := math3d.LookAt(math3d.Vec3{}, math3d.Vec3{Z: -1}, math3d.Vec3Up())
	return physics3d.Mat4(proj.Mul(view))
}

func TestDebugDrawDepthZOClipsBehindOrthoCamera(t *testing.T) {
	vp := core.Bounds{Size: core.Size{Width: 200, Height: 200}}
	viewProj := orthoZOViewProj()
	behind := physics3d.BoxShape(physics3d.Box{Center: physics3d.Vec3{Z: 50}, HalfExtents: physics3d.Vec3{X: 1, Y: 1, Z: 1}})
	front := physics3d.BoxShape(physics3d.Box{Center: physics3d.Vec3{Z: -50}, HalfExtents: physics3d.Vec3{X: 1, Y: 1, Z: 1}})
	zo := physics3d.DefaultDebugStyle()
	zo.DepthZO = true

	count := func(shape physics3d.Shape, style physics3d.DebugStyle) int {
		return len(extractSegments(t, renderCanvasOps(t, 200, 200, func(d *core.Drawing) {
			physics3d.DrawShape(d, viewProj, vp, shape, style)
		})))
	}
	if n := count(behind, zo); n != 0 {
		t.Fatalf("DepthZO drew %d edges of a box 50 units behind a [0, 1] orthographic camera", n)
	}
	if n := count(front, zo); n != 12 {
		t.Fatalf("DepthZO drew %d edges of a box in front of the camera, want 12", n)
	}
	// The default [-1, 1] near plane is z >= -w, and w is 1 everywhere for an orthographic projection, so it keeps the box behind this camera: the reason DepthZO exists.
	if n := count(behind, physics3d.DefaultDebugStyle()); n == 0 {
		t.Fatal("default style clipped the box behind the camera; the DepthZO comparison above proves nothing")
	}

	// A contact behind the camera draws neither its marker nor its normal.
	contactOps := renderCanvasOps(t, 200, 200, func(d *core.Drawing) {
		physics3d.DrawContact(d, viewProj, vp, physics3d.Contact{Hit: true, Point: physics3d.Vec3{Z: 50}, Normal: physics3d.Vec3{Y: 1}}, zo)
	})
	if len(contactOps) != 0 {
		t.Fatalf("contact behind the camera emitted %d ops", len(contactOps))
	}
}

func TestDrawLine3DZOClipsAtZeroDepth(t *testing.T) {
	vp := core.Bounds{Size: core.Size{Width: 200, Height: 200}}
	viewProj := orthoZOViewProj()
	draw := func(line func(d *core.Drawing, viewProj physics3d.Mat4, vp core.Bounds, a, b physics3d.Vec3, col core.Color, w float32), a, b physics3d.Vec3) []seg2D {
		return extractSegments(t, renderCanvasOps(t, 200, 200, func(d *core.Drawing) {
			line(d, viewProj, vp, a, b, core.RGB(0xFF0000), 1)
		}))
	}
	if segs := draw(physics3d.DrawLine3DZO, physics3d.Vec3{X: -1, Z: 50}, physics3d.Vec3{X: 1, Z: 50}); len(segs) != 0 {
		t.Fatalf("DrawLine3DZO drew %d segments behind the camera", len(segs))
	}
	if segs := draw(physics3d.DrawLine3D, physics3d.Vec3{X: -1, Z: 50}, physics3d.Vec3{X: 1, Z: 50}); len(segs) != 1 {
		t.Fatalf("DrawLine3D drew %d segments, want the 1 its [-1, 1] near plane keeps", len(segs))
	}
	// A segment along the view axis from behind to in front is cut at the near plane, not dropped: from x = -10 at z = 10 to x = 10 at z = -10 it crosses the near plane z = -0.1 at x = 0.1, which maps to screen x = 101.
	segs := draw(physics3d.DrawLine3DZO, physics3d.Vec3{X: -10, Z: 10}, physics3d.Vec3{X: 10, Z: -10})
	if len(segs) != 1 {
		t.Fatalf("crossing segment produced %d segments, want 1", len(segs))
	}
	if !approx(segs[0].x0, 101, 1e-2) || !approx(segs[0].x1, 200, 1e-2) {
		t.Fatalf("clipped segment x = %v..%v, want 101..200", segs[0].x0, segs[0].x1)
	}
}
