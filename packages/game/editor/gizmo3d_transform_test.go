package editor_test

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"github.com/nsgo-dev/nsgo/ui"
)

func quatAxisAngleDiff(a, b math3d.Quat) float64 {
	// Relative rotation qRel = a * b^-1; angle = 2 * acos(|w|).
	rel := a.Mul(b.Conj()).Norm()
	w := math.Min(1, math.Abs(float64(rel.W)))
	return 2 * math.Acos(w)
}

func worldRotOf(ed *editor.Editor, id string) math3d.Quat {
	q := math3d.QuatIdentity()
	var walk func(objs []sceneformat.ObjectDoc, parentQ math3d.Quat) bool
	walk = func(objs []sceneformat.ObjectDoc, parentQ math3d.Quat) bool {
		for i := range objs {
			o := &objs[i]
			cur := parentQ.Mul(sceneformat.ObjectLocalTransform3D(o).Rot).Norm()
			if o.ID == id {
				q = cur
				return true
			}
			if walk(o.Children, cur) {
				return true
			}
		}
		return false
	}
	for li := range ed.Doc.Layers {
		if walk(ed.Doc.Layers[li].Objects, math3d.QuatIdentity()) {
			break
		}
	}
	return q
}

func ringBasisVec3(axis editor.GizmoAxis3D) (u, v, n math3d.Vec3) {
	switch axis {
	case editor.Axis3DX:
		return math3d.Vec3{Y: 1}, math3d.Vec3{Z: 1}, math3d.Vec3{X: 1}
	case editor.Axis3DY:
		return math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}, math3d.Vec3{Y: 1}
	default:
		return math3d.Vec3{X: 1}, math3d.Vec3{Y: 1}, math3d.Vec3{Z: 1}
	}
}

// #892 Acceptance 1: On an object already rotated about two axes, dragging any rotation ring (X, Y, Z)
// rotates it strictly about the exact axis drawn in both World and Local space.
func TestGizmo3DQuaternionRotationAboutDrawnAxisWorldAndLocal(t *testing.T) {
	const w, h float32 = 640, 360
	const dragAngle float32 = 0.72 // ~41.3°

	for _, space := range []editor.GizmoSpace3D{editor.GizmoSpaceGlobal, editor.GizmoSpaceLocal} {
		for _, axis := range []editor.GizmoAxis3D{editor.Axis3DX, editor.Axis3DY, editor.Axis3DZ} {
			name := space.String() + "_" + axis.String()
			t.Run(name, func(t *testing.T) {
				ed := newTest3DEditor(t, sceneformat.ObjectDoc{
					ID:    "ship",
					Name:  "Ship",
					X:     12,
					Y:     -6,
					PosZ:  8,
					RotX:  0.65,
					RotY:  0.85,
					Depth: 16,
				})
				ed.Camera3D = editor.OrbitCamera3D{
					Target:   [3]float32{12, -6, 8},
					Yaw:      0.55,
					Pitch:    0.48,
					Distance: 180,
					FOV:      float32(math.Pi / 3),
					Near:     0.5,
					Far:      5000,
				}
				ed.SelectObject("ship")
				ed.SetGizmoMode3D(editor.Gizmo3DRotate)
				ed.SetGizmoSpace3D(space)

				objBefore, _, _ := ed.FindObject("ship")
				qBefore := sceneformat.ObjectLocalTransform3D(objBefore).Rot
				orient := ed.GizmoOrientation3D()
				if space == editor.GizmoSpaceGlobal && !orient.ApproxEqual(math3d.QuatIdentity(), 1e-4) {
					t.Fatalf("Global GizmoOrientation3D = %v, want identity", orient)
				}
				if space == editor.GizmoSpaceLocal && !orient.ApproxEqual(qBefore, 1e-4) {
					t.Fatalf("Local GizmoOrientation3D = %v, want %v", orient, qBefore)
				}

				origin := math3d.Vec3{X: objBefore.X, Y: objBefore.Y, Z: objBefore.PosZ}
				gLen := ed.GizmoLength3D([3]float32{origin.X, origin.Y, origin.Z})
				uLocal, vLocal, nLocal := ringBasisVec3(axis)
				drawnAxis := orient.RotateVec3(nLocal).Norm()

				// Choose a starting angle on the ring that is unambiguously away from the other rings.
				ringWorld := func(a float32) [3]float32 {
					ca, sa := float32(math.Cos(float64(a)))*gLen, float32(math.Sin(float64(a)))*gLen
					localPt := uLocal.Scale(ca).Add(vLocal.Scale(sa))
					wp := origin.Add(orient.RotateVec3(localPt))
					return [3]float32{wp.X, wp.Y, wp.Z}
				}

				startAngle := float32(0.7)
				// Ensure the picked screen point lands squarely on the intended ring.
				var sx0, sy0 float32
				picked := editor.Axis3DNone
				for _, cand := range []float32{0.7, 1.15, 2.1, 2.6, 3.8} {
					p0 := ringWorld(cand)
					x, y, _, vis := ed.Camera3D.Project(p0, w, h)
					if vis && ed.PickGizmo3D(x, y, w, h) == axis {
						startAngle = cand
						sx0, sy0 = x, y
						picked = axis
						break
					}
				}
				if picked != axis {
					t.Fatalf("could not pick %s ring in %s space", axis, space)
				}

				pMid := ringWorld(startAngle + dragAngle*0.5)
				pEnd := ringWorld(startAngle + dragAngle)
				sxM, syM, _, visM := ed.Camera3D.Project(pMid, w, h)
				sx1, sy1, _, vis1 := ed.Camera3D.Project(pEnd, w, h)
				if !visM || !vis1 {
					t.Fatal("ring drag target not visible")
				}

				if got := ed.BeginGizmoDrag3D(sx0, sy0, w, h); got != axis {
					t.Fatalf("BeginGizmoDrag3D = %v, want %v", got, axis)
				}
				ed.UpdateGizmoDrag3D(sxM, syM)
				ed.UpdateGizmoDrag3D(sx1, sy1)
				if !ed.EndGizmoDrag3D() {
					t.Fatal("EndGizmoDrag3D returned false")
				}

				objAfter, _, _ := ed.FindObject("ship")
				qAfter := sceneformat.ObjectLocalTransform3D(objAfter).Rot
				qWant := math3d.QuatFromAxisAngle(drawnAxis, dragAngle).Mul(qBefore).Norm()
				if diff := quatAxisAngleDiff(qAfter, qWant); diff > 0.025 {
					t.Fatalf("after %s rotate %s: orientation error = %.4f rad (got Euler (%v,%v,%v), qAfter=%v, qWant=%v)",
						space, axis, diff, objAfter.RotX, objAfter.RotY, objAfter.RotZ, qAfter, qWant)
				}

				// Verify the drawn axis itself is invariant under the applied rotation.
				qDelta := qAfter.Mul(qBefore.Conj()).Norm()
				rotatedAxis := qDelta.RotateVec3(drawnAxis)
				if !rotatedAxis.ApproxEqual(drawnAxis, 0.01) {
					t.Fatalf("drawn axis %v drifted to %v under %s rotate %s", drawnAxis, rotatedAxis, space, axis)
				}
			})
		}
	}
}

// #892: Nested child under a rotated parent orients Local gizmo handles to parent*child rotation
// and rotates/translates strictly about the drawn axes in both World and Local space.
func TestGizmo3DNestedChildWithRotatedParentWorldAndLocal(t *testing.T) {
	const w, h float32 = 640, 360
	const dragAngle float32 = 0.6

	newNested := func(t *testing.T) *editor.Editor {
		t.Helper()
		ed := newTest3DEditor(t, sceneformat.ObjectDoc{
			ID:   "parent",
			Name: "Parent",
			X:    10,
			Y:    5,
			PosZ: -4,
			RotX: 0.4,
			RotY: 0.9,
			Children: []sceneformat.ObjectDoc{
				{
					ID:    "child",
					Name:  "Child",
					X:     15,
					Y:     8,
					PosZ:  6,
					RotX:  0.5,
					RotZ:  -0.35,
					Depth: 12,
				},
			},
		})
		ed.SelectObject("child")
		ed.FrameSelection()
		return ed
	}

	// Verify Local GizmoOrientation3D includes parent's accumulated rotation.
	ed := newNested(t)
	wantWorldQ := worldRotOf(ed, "child")
	ed.SetGizmoSpace3D(editor.GizmoSpaceLocal)
	if got := ed.GizmoOrientation3D(); !got.ApproxEqual(wantWorldQ, 1e-4) {
		t.Fatalf("nested Local GizmoOrientation3D = %v, want parent*child = %v", got, wantWorldQ)
	}

	// Verify rotating the nested child in BOTH World and Local space rotates its world orientation strictly about the drawn axis.
	for _, space := range []editor.GizmoSpace3D{editor.GizmoSpaceGlobal, editor.GizmoSpaceLocal} {
		for _, axis := range []editor.GizmoAxis3D{editor.Axis3DX, editor.Axis3DY, editor.Axis3DZ} {
			t.Run("rotate_"+space.String()+"_"+axis.String(), func(t *testing.T) {
				ed := newNested(t)
				ed.SetGizmoMode3D(editor.Gizmo3DRotate)
				ed.SetGizmoSpace3D(space)

				qWorldBefore := worldRotOf(ed, "child")
				orient := ed.GizmoOrientation3D()
				uLocal, vLocal, nLocal := ringBasisVec3(axis)
				drawnAxis := orient.RotateVec3(nLocal).Norm()

				// Get child world origin via ObjectAABB3D center (or Target from FrameSelection).
				origin := math3d.Vec3{X: ed.Camera3D.Target[0], Y: ed.Camera3D.Target[1], Z: ed.Camera3D.Target[2]}
				gLen := ed.GizmoLength3D([3]float32{origin.X, origin.Y, origin.Z})
				ringPt := func(a float32) [3]float32 {
					ca, sa := float32(math.Cos(float64(a)))*gLen, float32(math.Sin(float64(a)))*gLen
					wp := origin.Add(orient.RotateVec3(uLocal.Scale(ca).Add(vLocal.Scale(sa))))
					return [3]float32{wp.X, wp.Y, wp.Z}
				}

				sx0, sy0, _, _ := ed.Camera3D.Project(ringPt(0.7), w, h)
				sxM, syM, _, _ := ed.Camera3D.Project(ringPt(0.7+dragAngle*0.5), w, h)
				sx1, sy1, _, _ := ed.Camera3D.Project(ringPt(0.7+dragAngle), w, h)

				if !ed.BeginGizmoDragAxis3D(axis, sx0, sy0, w, h) {
					t.Fatalf("BeginGizmoDragAxis3D(%v) failed", axis)
				}
				ed.UpdateGizmoDrag3D(sxM, syM)
				ed.UpdateGizmoDrag3D(sx1, sy1)
				if !ed.EndGizmoDrag3D() {
					t.Fatal("EndGizmoDrag3D returned false")
				}

				qWorldAfter := worldRotOf(ed, "child")
				qWorldWant := math3d.QuatFromAxisAngle(drawnAxis, dragAngle).Mul(qWorldBefore).Norm()
				if diff := quatAxisAngleDiff(qWorldAfter, qWorldWant); diff > 0.025 {
					t.Fatalf("nested %s rotate %s: world orientation error = %.4f rad (got %v, want %v)",
						space, axis, diff, qWorldAfter, qWorldWant)
				}
			})
		}
	}

	// Verify Local Translate X on the nested child moves strictly along the child's world-oriented local X axis.
	t.Run("translate_local_X", func(t *testing.T) {
		ed := newNested(t)
		ed.SetGizmoMode3D(editor.Gizmo3DTranslate)
		ed.SetGizmoSpace3D(editor.GizmoSpaceLocal)

		orient := ed.GizmoOrientation3D()
		localXWorld := orient.RotateVec3(math3d.Vec3UnitX()).Norm()
		origin := math3d.Vec3{X: ed.Camera3D.Target[0], Y: ed.Camera3D.Target[1], Z: ed.Camera3D.Target[2]}
		gLen := ed.GizmoLength3D([3]float32{origin.X, origin.Y, origin.Z})

		p0 := origin.Add(localXWorld.Scale(gLen * 0.6))
		p1 := p0.Add(localXWorld.Scale(20))
		sx0, sy0, _, _ := ed.Camera3D.Project([3]float32{p0.X, p0.Y, p0.Z}, w, h)
		sx1, sy1, _, _ := ed.Camera3D.Project([3]float32{p1.X, p1.Y, p1.Z}, w, h)

		if got := ed.BeginGizmoDrag3D(sx0, sy0, w, h); got != editor.Axis3DX {
			t.Fatalf("BeginGizmoDrag3D local X = %v, want Axis3DX", got)
		}
		ed.UpdateGizmoDrag3D(sx1, sy1)
		if !ed.EndGizmoDrag3D() {
			t.Fatal("EndGizmoDrag3D returned false")
		}

		// Re-frame to measure the child's new world position.
		ed.FrameSelection()
		afterOrigin := math3d.Vec3{X: ed.Camera3D.Target[0], Y: ed.Camera3D.Target[1], Z: ed.Camera3D.Target[2]}
		moved := afterOrigin.Sub(origin)
		wantMoved := localXWorld.Scale(20)
		if !moved.ApproxEqual(wantMoved, 0.3) {
			t.Fatalf("nested local X move = %v, want %v", moved, wantMoved)
		}
	})
}

// #892 Acceptance 2 (part 1): Plane handles (XY/XZ/YZ) move strictly along the two axes of that plane
// (in both World and Local space), screen-space center handle translates in the view plane,
// and uniform center scale handle scales all three axes proportionally.
func TestGizmo3DPlaneHandlesScreenCenterAndUniformScale(t *testing.T) {
	const w, h float32 = 640, 360

	newCam := func(target [3]float32) editor.OrbitCamera3D {
		return editor.OrbitCamera3D{
			Target:   target,
			Yaw:      0.65,
			Pitch:    0.55,
			Distance: 200,
			FOV:      float32(math.Pi / 3),
			Near:     0.5,
			Far:      5000,
		}
	}

	planeCases := []struct {
		axis   editor.GizmoAxis3D
		u, v   math3d.Vec3
		normal math3d.Vec3
	}{
		{editor.Axis3DXY, math3d.Vec3UnitX(), math3d.Vec3UnitY(), math3d.Vec3UnitZ()},
		{editor.Axis3DXZ, math3d.Vec3UnitX(), math3d.Vec3UnitZ(), math3d.Vec3UnitY()},
		{editor.Axis3DYZ, math3d.Vec3UnitY(), math3d.Vec3UnitZ(), math3d.Vec3UnitX()},
	}

	for _, space := range []editor.GizmoSpace3D{editor.GizmoSpaceGlobal, editor.GizmoSpaceLocal} {
		for _, tc := range planeCases {
			t.Run("plane_"+space.String()+"_"+tc.axis.String(), func(t *testing.T) {
				ed := newTest3DEditor(t, sceneformat.ObjectDoc{
					ID:    "box",
					RotX:  0.4,
					RotY:  0.6,
					Depth: 16,
				})
				ed.Camera3D = newCam([3]float32{0, 0, 0})
				ed.SelectObject("box")
				ed.SetGizmoMode3D(editor.Gizmo3DTranslate)
				ed.SetGizmoSpace3D(space)

				orient := ed.GizmoOrientation3D()
				gLen := ed.GizmoLength3D([3]float32{0, 0, 0})
				uW := orient.RotateVec3(tc.u).Norm()
				vW := orient.RotateVec3(tc.v).Norm()
				nW := orient.RotateVec3(tc.normal).Norm()

				// Click center of the drawn plane handle square ([0.05, 0.32] -> midpoint 0.185).
				mid := uW.Scale(0.185 * gLen).Add(vW.Scale(0.185 * gLen))
				dst := mid.Add(uW.Scale(18)).Add(vW.Scale(-12))

				sx0, sy0, _, vis0 := ed.Camera3D.Project([3]float32{mid.X, mid.Y, mid.Z}, w, h)
				sx1, sy1, _, vis1 := ed.Camera3D.Project([3]float32{dst.X, dst.Y, dst.Z}, w, h)
				if !vis0 || !vis1 {
					t.Fatal("plane handle points not visible")
				}

				if picked := ed.BeginGizmoDrag3D(sx0, sy0, w, h); picked != tc.axis {
					t.Fatalf("BeginGizmoDrag3D(%s %s) = %v, want %v", space, tc.axis, picked, tc.axis)
				}
				ed.UpdateGizmoDrag3D(sx1, sy1)
				if !ed.EndGizmoDrag3D() {
					t.Fatal("EndGizmoDrag3D returned false")
				}

				obj, _, _ := ed.FindObject("box")
				moved := math3d.Vec3{X: obj.X, Y: obj.Y, Z: obj.PosZ}
				assertNear32(t, "u delta", moved.Dot(uW), 18, 0.2)
				assertNear32(t, "v delta", moved.Dot(vW), -12, 0.2)
				assertNear32(t, "normal drift", moved.Dot(nW), 0, 1e-3)
			})
		}
	}

	t.Run("screen_space_center_handle", func(t *testing.T) {
		ed := newTest3DEditor(t, sceneformat.ObjectDoc{ID: "box", Depth: 16})
		ed.Camera3D = newCam([3]float32{0, 0, 0})
		ed.SelectObject("box")
		ed.SetGizmoMode3D(editor.Gizmo3DTranslate)

		right32, up32, _ := ed.Camera3D.Basis()
		right := math3d.Vec3{X: right32[0], Y: right32[1], Z: right32[2]}
		up := math3d.Vec3{X: up32[0], Y: up32[1], Z: up32[2]}

		sx0, sy0, _, _ := ed.Camera3D.Project([3]float32{0, 0, 0}, w, h)
		viewRay := ed.PickRay3D(sx0, sy0, w, h)
		target := right.Scale(22).Add(up.Scale(14))
		sx1, sy1, _, _ := ed.Camera3D.Project([3]float32{target.X, target.Y, target.Z}, w, h)

		if picked := ed.BeginGizmoDrag3D(sx0, sy0, w, h); picked != editor.Axis3DAll {
			t.Fatalf("BeginGizmoDrag3D at center = %v, want Axis3DAll", picked)
		}
		ed.UpdateGizmoDrag3D(sx1, sy1)
		if !ed.EndGizmoDrag3D() {
			t.Fatal("EndGizmoDrag3D(Axis3DAll) returned false")
		}

		obj, _, _ := ed.FindObject("box")
		moved := math3d.Vec3{X: obj.X, Y: obj.Y, Z: obj.PosZ}
		assertNear32(t, "screen center move X", moved.X, target.X, 0.2)
		assertNear32(t, "screen center move Y", moved.Y, target.Y, 0.2)
		assertNear32(t, "screen center move Z", moved.Z, target.Z, 0.2)
		assertNear32(t, "view plane normal drift", moved.Dot(viewRay.Dir.Norm()), 0, 0.05)
	})

	t.Run("uniform_scale_center_handle", func(t *testing.T) {
		ed := newTest3DEditor(t, sceneformat.ObjectDoc{
			ID:     "box",
			ScaleX: sceneformat.Float32(2.0),
			ScaleY: sceneformat.Float32(1.0),
			ScaleZ: sceneformat.Float32(0.5),
			Depth:  16,
		})
		ed.Camera3D = newCam([3]float32{0, 0, 0})
		ed.SelectObject("box")
		ed.SetGizmoMode3D(editor.Gizmo3DScale)

		gLen := ed.GizmoLength3D([3]float32{0, 0, 0})
		right32, _, _ := ed.Camera3D.Basis()
		right := math3d.Vec3{X: right32[0], Y: right32[1], Z: right32[2]}

		// Drag right by 0.5*gLen -> scale factor 1.5x across all 3 axes proportionally.
		sx0, sy0, _, _ := ed.Camera3D.Project([3]float32{0, 0, 0}, w, h)
		dst := right.Scale(gLen * 0.5)
		sx1, sy1, _, _ := ed.Camera3D.Project([3]float32{dst.X, dst.Y, dst.Z}, w, h)

		if picked := ed.BeginGizmoDrag3D(sx0, sy0, w, h); picked != editor.Axis3DAll {
			t.Fatalf("BeginGizmoDrag3D scale center = %v, want Axis3DAll", picked)
		}
		ed.UpdateGizmoDrag3D(sx1, sy1)
		if !ed.EndGizmoDrag3D() {
			t.Fatal("EndGizmoDrag3D uniform scale returned false")
		}

		obj, _, _ := ed.FindObject("box")
		assertNear32(t, "uniform ScaleX", obj.EffectiveScaleX(), 3.0, 0.05)
		assertNear32(t, "uniform ScaleY", obj.EffectiveScaleY(), 1.5, 0.05)
		assertNear32(t, "uniform ScaleZ", obj.EffectiveScaleZ(), 0.75, 0.05)

		// Drag left (inward) by 0.5*gLen -> scales all 3 axes down proportionally to 0.5x of current.
		inward := right.Scale(-gLen * 0.5)
		sx2, sy2, _, _ := ed.Camera3D.Project([3]float32{inward.X, inward.Y, inward.Z}, w, h)
		if picked := ed.BeginGizmoDrag3D(sx0, sy0, w, h); picked != editor.Axis3DAll {
			t.Fatalf("BeginGizmoDrag3D scale center inward = %v, want Axis3DAll", picked)
		}
		ed.UpdateGizmoDrag3D(sx2, sy2)
		if !ed.EndGizmoDrag3D() {
			t.Fatal("EndGizmoDrag3D inward uniform scale returned false")
		}
		obj, _, _ = ed.FindObject("box")
		assertNear32(t, "inward ScaleX", obj.EffectiveScaleX(), 1.5, 0.05)
		assertNear32(t, "inward ScaleY", obj.EffectiveScaleY(), 0.75, 0.05)
		assertNear32(t, "inward ScaleZ", obj.EffectiveScaleZ(), 0.375, 0.05)
	})
}

// #892 Acceptance 2 (part 2): Snapping for 3D translate (planes + local axes), rotate (about drawn axis),
// and uniform scale lands on exact multiples of the configured step and supports modifier inversion.
func TestGizmo3DSnappingAndModifierInversion(t *testing.T) {
	const w, h float32 = 640, 360
	cam := editor.OrbitCamera3D{
		Target:   [3]float32{0, 0, 0},
		Yaw:      0.65,
		Pitch:    0.55,
		Distance: 200,
		FOV:      float32(math.Pi / 3),
		Near:     0.5,
		Far:      5000,
	}

	assertMult := func(t *testing.T, label string, v, step float64) {
		t.Helper()
		rem := math.Abs(v - math.Round(v/step)*step)
		if rem > 1e-3 {
			t.Fatalf("%s = %v is not a multiple of %v (rem=%v)", label, v, step, rem)
		}
	}

	// 1. Planar XY translate snap to step 8, and Ctrl/Cmd invert disables snap.
	t.Run("translate_plane_snap_and_invert", func(t *testing.T) {
		ed := newTest3DEditor(t, sceneformat.ObjectDoc{ID: "box", Depth: 16})
		ed.Camera3D = cam
		ed.SelectObject("box")
		ed.SetGizmoMode3D(editor.Gizmo3DTranslate)
		ed.SetGridSize(8)
		ed.SetSnapToGrid(true)

		gLen := ed.GizmoLength3D([3]float32{0, 0, 0})
		p0 := [3]float32{0.185 * gLen, 0.185 * gLen, 0}
		p1 := [3]float32{p0[0] + 19.3, p0[1] + 11.7, 0}
		sx0, sy0, _, _ := ed.Camera3D.Project(p0, w, h)
		sx1, sy1, _, _ := ed.Camera3D.Project(p1, w, h)

		if got := ed.BeginGizmoDrag3D(sx0, sy0, w, h); got != editor.Axis3DXY {
			t.Fatalf("picked %v, want Axis3DXY", got)
		}
		ed.UpdateGizmoDrag3D(sx1, sy1)
		ed.EndGizmoDrag3D()

		obj, _, _ := ed.FindObject("box")
		assertMult(t, "snapped X", float64(obj.X), 8)
		assertMult(t, "snapped Y", float64(obj.Y), 8)
		assertNear32(t, "snapped X exact", obj.X, 16, 1e-3)
		assertNear32(t, "snapped Y exact", obj.Y, 8, 1e-3)
		assertNear32(t, "snapped Z", obj.PosZ, 0, 1e-3)

		// Now invert snapping: drag free to non-multiple.
		ed.Undo()
		ed.SetSnapInvert(true)
		defer ed.SetSnapInvert(false)
		ed.BeginGizmoDrag3D(sx0, sy0, w, h)
		ed.UpdateGizmoDrag3D(sx1, sy1)
		ed.EndGizmoDrag3D()

		obj, _, _ = ed.FindObject("box")
		assertNear32(t, "inverted unsnapped X", obj.X, 19.3, 0.2)
		assertNear32(t, "inverted unsnapped Y", obj.Y, 11.7, 0.2)
	})

	// 2. Quaternion rotation snap on a 2-axis rotated object lands on exact 15° increments about drawn axis.
	t.Run("rotate_quaternion_snap_15deg", func(t *testing.T) {
		ed := newTest3DEditor(t, sceneformat.ObjectDoc{
			ID:    "box",
			RotX:  0.4,
			RotY:  0.7,
			Depth: 16,
		})
		ed.Camera3D = cam
		ed.SelectObject("box")
		ed.SetGizmoMode3D(editor.Gizmo3DRotate)
		ed.SetGizmoSpace3D(editor.GizmoSpaceLocal)
		ed.SetRotationSnapDeg(15)
		ed.SetSnapRotation(true)

		objBefore, _, _ := ed.FindObject("box")
		qBefore := sceneformat.ObjectLocalTransform3D(objBefore).Rot
		orient := ed.GizmoOrientation3D()
		gLen := ed.GizmoLength3D([3]float32{0, 0, 0})
		uLocal, vLocal, nLocal := ringBasisVec3(editor.Axis3DX)
		drawnAxis := orient.RotateVec3(nLocal).Norm()

		ringPt := func(a float32) [3]float32 {
			ca, sa := float32(math.Cos(float64(a)))*gLen, float32(math.Sin(float64(a)))*gLen
			wp := orient.RotateVec3(uLocal.Scale(ca).Add(vLocal.Scale(sa)))
			return [3]float32{wp.X, wp.Y, wp.Z}
		}
		dragRad := float32(23.0 * math.Pi / 180.0) // snaps to 30°? 23/15 = 1.533 -> snaps to 30°!
		sx0, sy0, _, _ := ed.Camera3D.Project(ringPt(0.7), w, h)
		sx1, sy1, _, _ := ed.Camera3D.Project(ringPt(0.7+dragRad), w, h)

		if !ed.BeginGizmoDragAxis3D(editor.Axis3DX, sx0, sy0, w, h) {
			t.Fatal("BeginGizmoDragAxis3D rotate X failed")
		}
		ed.UpdateGizmoDrag3D(sx1, sy1)
		ed.EndGizmoDrag3D()

		objAfter, _, _ := ed.FindObject("box")
		qAfter := sceneformat.ObjectLocalTransform3D(objAfter).Rot
		wantAngle := float32(30.0 * math.Pi / 180.0)
		qWant := math3d.QuatFromAxisAngle(drawnAxis, wantAngle).Mul(qBefore).Norm()
		if diff := quatAxisAngleDiff(qAfter, qWant); diff > 0.015 {
			t.Fatalf("snapped local rotate X error = %.4f rad, want exact 30° about drawn axis", diff)
		}
	})

	// 3. Uniform scale snap to 0.25 step, plus modifier inversion when SnapScale is off.
	t.Run("uniform_scale_snap_and_invert", func(t *testing.T) {
		ed := newTest3DEditor(t, sceneformat.ObjectDoc{ID: "box", Depth: 16})
		ed.Camera3D = cam
		ed.SelectObject("box")
		ed.SetGizmoMode3D(editor.Gizmo3DScale)
		ed.SetScaleSnapStep(0.25)
		ed.SetSnapScale(false)
		ed.SetSnapInvert(true) // temporary snap ON via modifier!
		defer ed.SetSnapInvert(false)

		gLen := ed.GizmoLength3D([3]float32{0, 0, 0})
		right32, _, _ := ed.Camera3D.Basis()
		right := math3d.Vec3{X: right32[0], Y: right32[1], Z: right32[2]}
		sx0, sy0, _, _ := ed.Camera3D.Project([3]float32{0, 0, 0}, w, h)
		dst := right.Scale(gLen * 0.61) // raw factor 1.61 -> snaps to 1.50
		sx1, sy1, _, _ := ed.Camera3D.Project([3]float32{dst.X, dst.Y, dst.Z}, w, h)

		if got := ed.BeginGizmoDrag3D(sx0, sy0, w, h); got != editor.Axis3DAll {
			t.Fatalf("picked %v, want Axis3DAll", got)
		}
		ed.UpdateGizmoDrag3D(sx1, sy1)
		ed.EndGizmoDrag3D()

		obj, _, _ := ed.FindObject("box")
		assertMult(t, "uniform ScaleX", float64(obj.EffectiveScaleX()), 0.25)
		assertMult(t, "uniform ScaleY", float64(obj.EffectiveScaleY()), 0.25)
		assertMult(t, "uniform ScaleZ", float64(obj.EffectiveScaleZ()), 0.25)
		assertNear32(t, "uniform ScaleX exact", obj.EffectiveScaleX(), 1.5, 1e-3)
		assertNear32(t, "uniform ScaleY exact", obj.EffectiveScaleY(), 1.5, 1e-3)
		assertNear32(t, "uniform ScaleZ exact", obj.EffectiveScaleZ(), 1.5, 1e-3)
	})
}

// #892 Acceptance 3 + UI/shortcuts/status bar: W/E/R switch 3D gizmo modes (without clashing with 2D E),
// Local/World toggle works from toolbar, shortcut T, and command registry, status bar shows space & live
// drag delta, and every drag commits as one undo step and round-trips through scene YAML.
func TestGizmo3DShortcutsSpaceToggleStatusDeltaUndoAndYAML(t *testing.T) {
	ed := newTest3DEditor(t, sceneformat.ObjectDoc{
		ID:    "crate",
		Name:  "Crate",
		X:     4,
		Y:     2,
		PosZ:  -3,
		RotX:  0.3,
		RotY:  0.5,
		Depth: 16,
	})
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{4, 2, -3},
		Yaw:      0.65,
		Pitch:    0.55,
		Distance: 200,
		FOV:      float32(math.Pi / 3),
		Near:     0.5,
		Far:      5000,
	}
	ed.SelectObject("crate")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	// 1. W / E / R switch 3D gizmo modes in 3D view.
	app.Key("e")
	if ed.GizmoMode != editor.Gizmo3DRotate {
		t.Fatalf("key e in 3D: GizmoMode = %v, want Rotate", ed.GizmoMode)
	}
	app.Key("r")
	if ed.GizmoMode != editor.Gizmo3DScale {
		t.Fatalf("key r in 3D: GizmoMode = %v, want Scale", ed.GizmoMode)
	}
	app.Key("w")
	if ed.GizmoMode != editor.Gizmo3DTranslate {
		t.Fatalf("key w in 3D: GizmoMode = %v, want Translate", ed.GizmoMode)
	}

	// Verify 2D Erase Tile still owns E in 2D view (scoped shortcuts!).
	app.Key("alt+2")
	app.Key("e")
	if ed.Tool != editor.ToolEraseTile {
		t.Fatalf("key e in 2D: Tool = %v, want ToolEraseTile", ed.Tool)
	}
	app.Key("alt+3")

	// 2. Local / Global toggle via 3D bottom bar button, shortcut T, command registry, and status bar readout.
	if got := app.Find("id:editor-status-mode").Text; !strings.Contains(got, "Translate · Global") {
		t.Fatalf("initial 3D status bar = %q, want Translate · Global", got)
	}
	app.Press("id:view3d-space-local")
	if ed.GizmoSpace3D != editor.GizmoSpaceLocal {
		t.Fatalf("after view3d-space-local button: GizmoSpace3D = %v, want Local", ed.GizmoSpace3D)
	}
	if got := app.Find("id:editor-status-mode").Text; !strings.Contains(got, "Translate · Local") {
		t.Fatalf("local 3D status bar = %q, want Translate · Local", got)
	}
	app.Key("t")
	if ed.GizmoSpace3D != editor.GizmoSpaceGlobal {
		t.Fatalf("after key t: GizmoSpace3D = %v, want Global", ed.GizmoSpace3D)
	}
	if err := ed.ExecuteCommand(editor.CmdGizmoSpace3D); err != nil {
		t.Fatalf("ExecuteCommand(CmdGizmoSpace3D): %v", err)
	}
	app.Settle()
	if ed.GizmoSpace3D != editor.GizmoSpaceLocal {
		t.Fatalf("after ExecuteCommand(CmdGizmoSpace3D): GizmoSpace3D = %v, want Local", ed.GizmoSpace3D)
	}

	// 3. Live drag delta in status bar + single undo step + exact YAML round-trip for plane translate, rotate, uniform scale.
	vw, vh := ed.CanvasSize()
	mustYAML := func(label string) []byte {
		t.Helper()
		b, err := ed.SaveYAML()
		if err != nil {
			t.Fatalf("%s: SaveYAML: %v", label, err)
		}
		return b
	}
	snap0 := mustYAML("initial")

	// Plane XY translate drag (in Global mode for exact coordinate check).
	ed.SetGizmoSpace3D(editor.GizmoSpaceGlobal)
	app.Settle()
	origin := [3]float32{4, 2, -3}
	gLen := ed.GizmoLength3D(origin)
	p0 := [3]float32{origin[0] + 0.185*gLen, origin[1] + 0.185*gLen, origin[2]}
	p1 := [3]float32{p0[0] + 10, p0[1] + 5, origin[2]}
	sx0, sy0, _, _ := ed.Camera3D.Project(p0, vw, vh)
	sx1, sy1, _, _ := ed.Camera3D.Project(p1, vw, vh)

	app.Post(func() {
		if axis := ed.BeginGizmoDrag3D(sx0, sy0, vw, vh); axis != editor.Axis3DXY {
			t.Errorf("picked %v, want Axis3DXY", axis)
		}
		ed.UpdateGizmoDrag3D(sx1, sy1)
	})
	app.Settle()
	deltaMsg := app.Find("id:editor-status-message").Text
	if !strings.Contains(deltaMsg, "dx") || !strings.Contains(deltaMsg, "dy") || !strings.Contains(deltaMsg, "dz") {
		t.Fatalf("status bar during translate drag = %q, want dx/dy/dz readout", deltaMsg)
	}
	app.Post(func() { ed.EndGizmoDrag3D() })
	app.Settle()
	if ed.GizmoDragDelta3D() != "" {
		t.Fatalf("GizmoDragDelta3D after End = %q, want empty", ed.GizmoDragDelta3D())
	}
	snap1 := mustYAML("after plane translate")

	// Rotate drag -> status bar shows angle in degrees (°).
	app.Key("e")
	obj, _, _ := ed.FindObject("crate")
	pos1 := math3d.Vec3{X: obj.X, Y: obj.Y, Z: obj.PosZ}
	gLen = ed.GizmoLength3D([3]float32{pos1.X, pos1.Y, pos1.Z})
	uZ, vZ, _ := ringBasisVec3(editor.Axis3DZ)
	r0 := pos1.Add(uZ.Scale(float32(math.Cos(0.7)) * gLen)).Add(vZ.Scale(float32(math.Sin(0.7)) * gLen))
	r1 := pos1.Add(uZ.Scale(float32(math.Cos(1.2)) * gLen)).Add(vZ.Scale(float32(math.Sin(1.2)) * gLen))
	rsx0, rsy0, _, _ := ed.Camera3D.Project([3]float32{r0.X, r0.Y, r0.Z}, vw, vh)
	rsx1, rsy1, _, _ := ed.Camera3D.Project([3]float32{r1.X, r1.Y, r1.Z}, vw, vh)

	app.Post(func() {
		if !ed.BeginGizmoDragAxis3D(editor.Axis3DZ, rsx0, rsy0, vw, vh) {
			t.Error("BeginGizmoDragAxis3D rotate Z failed")
		}
		ed.UpdateGizmoDrag3D(rsx1, rsy1)
	})
	app.Settle()
	rotMsg := app.Find("id:editor-status-message").Text
	if !strings.Contains(rotMsg, "°") {
		t.Fatalf("status bar during rotate drag = %q, want degree readout (°)", rotMsg)
	}
	app.Post(func() { ed.EndGizmoDrag3D() })
	app.Settle()
	snap2 := mustYAML("after rotate")

	// Uniform scale drag -> status bar shows scale factor (x).
	app.Key("r")
	right32, _, _ := ed.Camera3D.Basis()
	right := math3d.Vec3{X: right32[0], Y: right32[1], Z: right32[2]}
	ssx0, ssy0, _, _ := ed.Camera3D.Project([3]float32{pos1.X, pos1.Y, pos1.Z}, vw, vh)
	sEnd := pos1.Add(right.Scale(gLen * 0.5))
	ssx1, ssy1, _, _ := ed.Camera3D.Project([3]float32{sEnd.X, sEnd.Y, sEnd.Z}, vw, vh)

	app.Post(func() {
		if axis := ed.BeginGizmoDrag3D(ssx0, ssy0, vw, vh); axis != editor.Axis3DAll {
			t.Errorf("picked %v, want Axis3DAll", axis)
		}
		ed.UpdateGizmoDrag3D(ssx1, ssy1)
	})
	app.Settle()
	scaleMsg := app.Find("id:editor-status-message").Text
	if !strings.Contains(scaleMsg, "x") || !strings.Contains(scaleMsg, "Scale") {
		t.Fatalf("status bar during uniform scale drag = %q, want scale factor readout", scaleMsg)
	}
	app.Post(func() { ed.EndGizmoDrag3D() })
	app.Settle()
	snap3 := mustYAML("after uniform scale")

	// Verify 3 drags == exactly 3 undo steps.
	if ed.History.Len() != 3 {
		t.Fatalf("History.Len() = %d, want 3 (one per drag)", ed.History.Len())
	}

	// Verify YAML round-trip through OpenYAML reproduces exact SceneDoc and canonical YAML.
	reloaded, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatalf("editor.New reloaded: %v", err)
	}
	if err := reloaded.OpenYAML(snap3); err != nil {
		t.Fatalf("OpenYAML: %v", err)
	}
	reencoded, err := reloaded.SaveYAML()
	if err != nil {
		t.Fatalf("reloaded SaveYAML: %v", err)
	}
	if !bytes.Equal(reencoded, snap3) {
		t.Fatalf("YAML round-trip mismatch:\ngot:\n%s\nwant:\n%s", reencoded, snap3)
	}

	// Undo all 3 steps and verify byte-exact YAML restoration at every step.
	for i, want := range [][]byte{snap2, snap1, snap0} {
		if !ed.Undo() {
			t.Fatalf("Undo %d returned false", i)
		}
		if got := mustYAML("undo"); !bytes.Equal(got, want) {
			t.Fatalf("after Undo %d YAML differs:\ngot:\n%s\nwant:\n%s", i, got, want)
		}
	}
	// Redo all 3 steps and verify byte-exact YAML restoration.
	for i, want := range [][]byte{snap1, snap2, snap3} {
		if !ed.Redo() {
			t.Fatalf("Redo %d returned false", i)
		}
		if got := mustYAML("redo"); !bytes.Equal(got, want) {
			t.Fatalf("after Redo %d YAML differs:\ngot:\n%s\nwant:\n%s", i, got, want)
		}
	}
}

// Regression tests for grazing-ray plane drags, invalid-ray uniform scale (no snap-to-1.0x),
// consistent near-zero single/multi-object scale clamping, non-finite ancestor rotations,
// and Spec Light theme contrast on 3D gizmo center ring & active highlight.
func TestGizmo3DGrazingRayScaleClampFiniteAncestorAndSpecLightContrast(t *testing.T) {
	const w, h float32 = 640, 360

	t.Run("grazing_ray_plane_drag_does_not_explode", func(t *testing.T) {
		ed := newTest3DEditor(t, sceneformat.ObjectDoc{ID: "box", Depth: 16})
		// Pitch = 0.005 rad puts the camera nearly coplanar with the XZ plane (|dot(dir, +Y)| < 0.08).
		ed.Camera3D = editor.OrbitCamera3D{
			Target:   [3]float32{0, 0, 0},
			Yaw:      0,
			Pitch:    0.005,
			Distance: 200,
			FOV:      float32(math.Pi / 3),
			Near:     0.5,
			Far:      5000,
		}
		ed.SelectObject("box")
		ed.SetGizmoMode3D(editor.Gizmo3DTranslate)
		if !ed.BeginGizmoDragAxis3D(editor.Axis3DXZ, w/2, h/2, w, h) {
			t.Fatal("BeginGizmoDragAxis3D(Axis3DXZ) failed")
		}
		ed.UpdateGizmoDrag3D(w/2+40, h/2-2)
		ed.EndGizmoDrag3D()

		obj, _, _ := ed.FindObject("box")
		if obj.X != 0 || obj.Y != 0 || obj.PosZ != 0 {
			t.Fatalf("grazing XZ plane drag moved box to (%v, %v, %v), want unchanged (0, 0, 0)", obj.X, obj.Y, obj.PosZ)
		}
	})

	t.Run("invalid_ray_uniform_scale_preserves_previous_scale", func(t *testing.T) {
		ed := newTest3DEditor(t, sceneformat.ObjectDoc{ID: "box", Depth: 16})
		ed.Camera3D = editor.OrbitCamera3D{
			Target:   [3]float32{0, 0, 0},
			Yaw:      0.65,
			Pitch:    0.55,
			Distance: 200,
			FOV:      float32(math.Pi / 3),
			Near:     0.5,
			Far:      5000,
		}
		ed.SelectObject("box")
		ed.SetGizmoMode3D(editor.Gizmo3DScale)

		gLen := ed.GizmoLength3D([3]float32{0, 0, 0})
		right32, _, _ := ed.Camera3D.Basis()
		right := math3d.Vec3{X: right32[0], Y: right32[1], Z: right32[2]}
		sx0, sy0, _, _ := ed.Camera3D.Project([3]float32{0, 0, 0}, w, h)
		dst := right.Scale(gLen * 0.5)
		sx1, sy1, _, _ := ed.Camera3D.Project([3]float32{dst.X, dst.Y, dst.Z}, w, h)

		if !ed.BeginGizmoDragAxis3D(editor.Axis3DAll, sx0, sy0, w, h) {
			t.Fatal("BeginGizmoDragAxis3D(Axis3DAll) failed")
		}
		ed.UpdateGizmoDrag3D(sx1, sy1)
		obj, _, _ := ed.FindObject("box")
		scaled := obj.EffectiveScaleX()
		if scaled < 1.4 {
			t.Fatalf("after valid drag ScaleX = %v, want ~1.5", scaled)
		}

		// Extreme off-screen coordinate causes a grazing/behind-plane ray: must preserve scaled value, NOT snap back to 1.0x!
		ed.UpdateGizmoDrag3D(1e7, 0)
		obj, _, _ = ed.FindObject("box")
		assertNear32(t, "ScaleX preserved after grazing ray", obj.EffectiveScaleX(), scaled, 1e-4)
		ed.EndGizmoDrag3D()
	})

	t.Run("near_zero_single_and_multi_uniform_scale_clamp_consistently", func(t *testing.T) {
		cam := editor.OrbitCamera3D{
			Target:   [3]float32{0, 0, 0},
			Yaw:      0,
			Pitch:    0,
			Distance: 200,
			FOV:      float32(math.Pi / 3),
			Near:     0.5,
			Far:      5000,
		}

		// Single object starting at small ScaleX = 0.05 dragged near zero: every axis must stay >= 0.001 (minScale2D).
		edSingle := newTest3DEditor(t, sceneformat.ObjectDoc{
			ID:     "small",
			ScaleX: sceneformat.Float32(0.05),
			ScaleY: sceneformat.Float32(1.0),
			ScaleZ: sceneformat.Float32(2.0),
			Depth:  16,
		})
		edSingle.Camera3D = cam
		edSingle.SelectObject("small")
		edSingle.SetGizmoMode3D(editor.Gizmo3DScale)
		gLen := edSingle.GizmoLength3D([3]float32{0, 0, 0})
		right32, _, _ := cam.Basis()
		right := math3d.Vec3{X: right32[0], Y: right32[1], Z: right32[2]}
		sx0, sy0, _, _ := cam.Project([3]float32{0, 0, 0}, w, h)
		// Drag left by 0.9999*gLen -> raw factor ~0.0001 -> clamped to 0.001, base*factor = 0.00005 -> clamped to 0.001.
		nearZero := right.Scale(-gLen * 0.9999)
		sx1, sy1, _, _ := cam.Project([3]float32{nearZero.X, nearZero.Y, nearZero.Z}, w, h)
		if !edSingle.BeginGizmoDragAxis3D(editor.Axis3DAll, sx0, sy0, w, h) {
			t.Fatal("single BeginGizmoDragAxis3D failed")
		}
		edSingle.UpdateGizmoDrag3D(sx1, sy1)
		edSingle.EndGizmoDrag3D()
		sObj, _, _ := edSingle.FindObject("small")
		if sObj.EffectiveScaleX() < 1e-3-1e-6 || sObj.EffectiveScaleY() < 1e-3-1e-6 || sObj.EffectiveScaleZ() < 1e-3-1e-6 {
			t.Fatalf("single near-zero uniform scale violated minScale2D: (%v, %v, %v)",
				sObj.EffectiveScaleX(), sObj.EffectiveScaleY(), sObj.EffectiveScaleZ())
		}

		// Multi-object uniform scale dragged to near-zero must clamp cleanly to minScale2D instead of freezing at 1.0.
		edMulti := newTest3DEditor(t,
			sceneformat.ObjectDoc{ID: "a", X: -10, ScaleX: sceneformat.Float32(0.05), Depth: 16},
			sceneformat.ObjectDoc{ID: "b", X: 10, ScaleX: sceneformat.Float32(1.0), Depth: 16},
		)
		edMulti.Camera3D = cam
		edMulti.SelectObjects([]string{"a", "b"})
		edMulti.SetGizmoMode3D(editor.Gizmo3DScale)
		if !edMulti.BeginGizmoDragAxis3D(editor.Axis3DAll, sx0, sy0, w, h) {
			t.Fatal("multi BeginGizmoDragAxis3D failed")
		}
		edMulti.UpdateGizmoDrag3D(sx1, sy1)
		if !edMulti.EndGizmoDrag3D() {
			t.Fatal("multi near-zero uniform scale froze instead of clamping")
		}
		objA, _, _ := edMulti.FindObject("a")
		objB, _, _ := edMulti.FindObject("b")
		if math.Abs(float64(objA.EffectiveScaleX())) < 1e-3-1e-6 || math.Abs(float64(objB.EffectiveScaleX())) < 1e-3-1e-6 {
			t.Fatalf("multi scale violated minScale2D: A=%v B=%v", objA.EffectiveScaleX(), objB.EffectiveScaleX())
		}
		if objB.EffectiveScaleX() > 0.01 {
			t.Fatalf("multi near-zero scale did not clamp down: B.ScaleX = %v, want ~0.001", objB.EffectiveScaleX())
		}
	})

	t.Run("non_finite_ancestor_rotation_falls_back_cleanly", func(t *testing.T) {
		ed := newTest3DEditor(t, sceneformat.ObjectDoc{
			ID:   "parent",
			Name: "Parent",
			Children: []sceneformat.ObjectDoc{{
				ID:    "child",
				Name:  "Child",
				RotY:  float32(math.Pi / 2),
				Depth: 16,
			}},
		})
		parent, _, _ := ed.FindObject("parent")
		parent.RotX = float32(math.NaN())
		ed.SelectObject("child")
		ed.SetGizmoSpace3D(editor.GizmoSpaceLocal)

		orient := ed.GizmoOrientation3D()
		if math.IsNaN(float64(orient.X)) || math.IsNaN(float64(orient.Y)) || math.IsNaN(float64(orient.Z)) || math.IsNaN(float64(orient.W)) {
			t.Fatalf("GizmoOrientation3D with NaN ancestor = %v, want finite", orient)
		}
		wantChildQ := math3d.QuatFromAxisAngle(math3d.Vec3UnitY(), float32(math.Pi/2))
		if !orient.ApproxEqual(wantChildQ, 1e-3) {
			t.Fatalf("GizmoOrientation3D with NaN ancestor = %v, want child's finite RotY %v", orient, wantChildQ)
		}
	})

	t.Run("spec_light_and_dark_gizmo_accent_contrast", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			pal  editor.SpecPalette
		}{
			{"light", editor.LightSpecPalette()},
			{"dark", editor.DarkSpecPalette()},
		} {
			centerRing := editor.CompositeOver(ui.Alpha(tc.pal.TextPrimary, 0.9), tc.pal.Viewport)
			if r := editor.ContrastRatio(centerRing, tc.pal.Viewport); r < 3.0 {
				t.Errorf("%s: center translate ring contrast %.2f:1, want >= 3.0:1", tc.name, r)
			}
			if r := editor.ContrastRatio(tc.pal.Warning.Fg, tc.pal.Viewport); r < 3.0 {
				t.Errorf("%s: active handle highlight Warning.Fg contrast %.2f:1, want >= 3.0:1", tc.name, r)
			}
		}
	})
}
