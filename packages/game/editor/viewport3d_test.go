package editor_test

import (
	"bytes"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func newTest3DEditor(t *testing.T, objects ...sceneformat.ObjectDoc) *editor.Editor {
	t.Helper()
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "3D Test Scene",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#141824",
		Layers: []sceneformat.LayerDoc{
			{
				Name:    "world",
				Space:   sceneformat.SpaceWorld,
				Order:   0,
				Objects: objects,
			},
		},
	}
	ed, err := editor.New(doc, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetViewMode(editor.ViewMode3D)
	return ed
}

func assertNear32(t *testing.T, label string, got, want, tol float32) {
	t.Helper()
	if math.Abs(float64(got-want)) > float64(tol) {
		t.Fatalf("%s = %v, want %v (tol %v)", label, got, want, tol)
	}
}

// Orbit camera math: Orbit, Pan, Zoom, Project + ScreenRay round-trip accuracy.
func TestOrbitCamera3DMathAndProjectionRoundTrip(t *testing.T) {
	cam := editor.DefaultOrbitCamera3D()
	cam.Target = [3]float32{0, 0, 0}
	cam.Yaw = 0
	cam.Pitch = 0
	cam.Distance = 100

	// At Yaw=0, Pitch=0, Eye sits at (0, 0, 100) looking down -Z.
	eye := cam.Eye()
	assertNear32(t, "eye.X", eye[0], 0, 1e-4)
	assertNear32(t, "eye.Y", eye[1], 0, 1e-4)
	assertNear32(t, "eye.Z", eye[2], 100, 1e-4)

	// Orbit updates yaw & pitch and clamps pitch below +-pi/2.
	cam.Orbit(float32(math.Pi/2), 0.3)
	assertNear32(t, "yaw", cam.Yaw, float32(math.Pi/2), 1e-4)
	assertNear32(t, "pitch", cam.Pitch, 0.3, 1e-4)

	cam.Orbit(0, 10)
	if cam.Pitch >= float32(math.Pi/2) {
		t.Fatalf("pitch %v was not clamped below pi/2", cam.Pitch)
	}
	cam.Orbit(0, -20)
	if cam.Pitch <= -float32(math.Pi/2) {
		t.Fatalf("pitch %v was not clamped above -pi/2", cam.Pitch)
	}

	// Pan shifts Target along camera right & up basis vectors.
	cam.Yaw = 0.5
	cam.Pitch = 0.4
	cam.Target = [3]float32{1, 2, 3}
	right, up, _ := cam.Basis()
	cam.Pan(10, -5)
	assertNear32(t, "target.X", cam.Target[0], 1+right[0]*10-up[0]*5, 1e-4)
	assertNear32(t, "target.Y", cam.Target[1], 2+right[1]*10-up[1]*5, 1e-4)
	assertNear32(t, "target.Z", cam.Target[2], 3+right[2]*10-up[2]*5, 1e-4)

	// Zoom adjusts Distance and clamps to safe positive range.
	cam.Distance = 100
	cam.Zoom(25)
	assertNear32(t, "distance after zoom in", cam.Distance, 75, 1e-4)
	cam.Zoom(-40)
	assertNear32(t, "distance after zoom out", cam.Distance, 115, 1e-4)
	cam.Zoom(99999)
	if cam.Distance <= 0 {
		t.Fatalf("distance after extreme zoom = %v, want > 0", cam.Distance)
	}

	// Project + ScreenRay round-trip accuracy across multiple world points and camera angles.
	const w, h float32 = 640, 360
	cam = editor.DefaultOrbitCamera3D()
	cam.Target = [3]float32{10, -5, 8}
	cam.Yaw = 0.72
	cam.Pitch = 0.38
	cam.Distance = 180

	points := [][3]float32{
		{10, -5, 8},
		{0, 0, 0},
		{35, 22, -14},
		{-28, 12, 41},
		{5, -20, -30},
	}
	for _, p := range points {
		sx, sy, depth, vis := cam.Project(p, w, h)
		if !vis || depth <= cam.Near {
			t.Fatalf("expected point %v to be visible, got vis=%v depth=%v", p, vis, depth)
		}
		orig, dir := cam.ScreenRay(sx, sy, w, h)
		// Ray distance to point p: dist = |p - orig|, reconstructed = orig + dist * dir.
		dx := float64(p[0] - orig[0])
		dy := float64(p[1] - orig[1])
		dz := float64(p[2] - orig[2])
		rayDist := float32(math.Sqrt(dx*dx + dy*dy + dz*dz))
		rx := orig[0] + dir[0]*rayDist
		ry := orig[1] + dir[1]*rayDist
		rz := orig[2] + dir[2]*rayDist
		assertNear32(t, "roundtrip X", rx, p[0], 1e-2)
		assertNear32(t, "roundtrip Y", ry, p[1], 1e-2)
		assertNear32(t, "roundtrip Z", rz, p[2], 1e-2)
	}

	// Point behind the camera must not be visible.
	eye = cam.Eye()
	_, _, fwd := cam.Basis()
	behind := [3]float32{eye[0] - fwd[0]*20, eye[1] - fwd[1]*20, eye[2] - fwd[2]*20}
	if _, _, _, vis := cam.Project(behind, w, h); vis {
		t.Fatalf("expected point behind camera %v to have visible=false", behind)
	}
}

// 3D picking: clicking projected center picks closest overlapping 3D object; empty space clears selection.
func TestPick3DClosestObjectAndEmptySpaceClearsSelection(t *testing.T) {
	ed := newTest3DEditor(t,
		// Index 0: farther object along -Z
		sceneformat.ObjectDoc{ID: "far_box", X: 0, Y: 0, PosZ: -40, Depth: 16},
		// Index 1: nearer object directly in front of far_box along the camera ray
		sceneformat.ObjectDoc{ID: "near_box", X: 0, Y: 0, PosZ: 20, Depth: 16},
		// Index 2: separate object off to the right
		sceneformat.ObjectDoc{ID: "side_box", X: 60, Y: 0, PosZ: 0, Depth: 16},
	)
	// Place camera on +Z looking directly down -Z so near_box occludes far_box.
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{0, 0, 0},
		Yaw:      0,
		Pitch:    0,
		Distance: 200,
		FOV:      float32(math.Pi / 3),
		Near:     0.5,
		Far:      5000,
	}
	const w, h float32 = 640, 360

	// Clicking projected center of near_box must pick index 1 (near_box), not 0 (far_box).
	sx, sy, _, vis := ed.Camera3D.Project([3]float32{0, 0, 20}, w, h)
	if !vis {
		t.Fatal("near_box not visible")
	}
	idx, ok := ed.Pick3D(sx, sy, w, h)
	if !ok || idx != 1 {
		t.Fatalf("Pick3D at near_box = (%d, %v), want (1, true)", idx, ok)
	}

	// Clicking side_box picks index 2 and selects "side_box".
	sideX, sideY, _, vis := ed.Camera3D.Project([3]float32{60, 0, 0}, w, h)
	if !vis {
		t.Fatal("side_box not visible")
	}
	ed.HandleViewport3DPress(sideX, sideY, w, h)
	if ed.SelectedID != "side_box" {
		t.Fatalf("SelectedID after side_box press = %q, want side_box", ed.SelectedID)
	}

	// Clicking empty corner space returns (-1, false) and clears selection.
	if missIdx, missOK := ed.Pick3D(8, 8, w, h); missOK || missIdx != -1 {
		t.Fatalf("Pick3D on empty space = (%d, %v), want (-1, false)", missIdx, missOK)
	}
	ed.HandleViewport3DPress(8, 8, w, h)
	if ed.SelectedID != "" {
		t.Fatalf("SelectedID after empty press = %q, want empty", ed.SelectedID)
	}
}

// 3D gizmo drag tests: translate along X/Y/Z, rotate around X/Y/Z, scale along X/Y/Z.
func TestGizmo3DTranslateRotateScaleDrags(t *testing.T) {
	const w, h float32 = 640, 360

	newGizmoEditor := func(t *testing.T, mode editor.GizmoMode3D) *editor.Editor {
		t.Helper()
		ed := newTest3DEditor(t, sceneformat.ObjectDoc{
			ID:    "cube",
			Name:  "Cube",
			X:     0,
			Y:     0,
			PosZ:  0,
			Depth: 16,
		})
		ed.Camera3D = editor.OrbitCamera3D{
			Target:   [3]float32{0, 0, 0},
			Yaw:      0.65,
			Pitch:    0.55,
			Distance: 200,
			FOV:      float32(math.Pi / 3),
			Near:     0.5,
			Far:      5000,
		}
		ed.SelectObject("cube")
		ed.SetGizmoMode3D(mode)
		return ed
	}

	t.Run("translate_XYZ", func(t *testing.T) {
		axes := []struct {
			axis editor.GizmoAxis3D
			dir  [3]float32
		}{
			{editor.Axis3DX, [3]float32{1, 0, 0}},
			{editor.Axis3DY, [3]float32{0, 1, 0}},
			{editor.Axis3DZ, [3]float32{0, 0, 1}},
		}
		for _, tc := range axes {
			ed := newGizmoEditor(t, editor.Gizmo3DTranslate)
			gLen := ed.GizmoLength3D([3]float32{0, 0, 0})

			// Grab midpoint of the axis shaft and drag by +25 world units along that axis.
			startPt := [3]float32{tc.dir[0] * gLen * 0.6, tc.dir[1] * gLen * 0.6, tc.dir[2] * gLen * 0.6}
			endPt := [3]float32{startPt[0] + tc.dir[0]*25, startPt[1] + tc.dir[1]*25, startPt[2] + tc.dir[2]*25}

			sx0, sy0, _, vis0 := ed.Camera3D.Project(startPt, w, h)
			sx1, sy1, _, vis1 := ed.Camera3D.Project(endPt, w, h)
			if !vis0 || !vis1 {
				t.Fatalf("axis %s drag points not visible", tc.axis)
			}

			picked := ed.BeginGizmoDrag3D(sx0, sy0, w, h)
			if picked != tc.axis {
				t.Fatalf("BeginGizmoDrag3D picked %s, want %s", picked, tc.axis)
			}
			ed.UpdateGizmoDrag3D(sx1, sy1)
			if !ed.EndGizmoDrag3D() {
				t.Fatalf("EndGizmoDrag3D(%s) returned false, want true", tc.axis)
			}

			obj, _, _ := ed.FindObject("cube")
			assertNear32(t, tc.axis.String()+" translate X", obj.X, tc.dir[0]*25, 0.1)
			assertNear32(t, tc.axis.String()+" translate Y", obj.Y, tc.dir[1]*25, 0.1)
			assertNear32(t, tc.axis.String()+" translate PosZ", obj.PosZ, tc.dir[2]*25, 0.1)
		}
	})

	t.Run("rotate_XYZ", func(t *testing.T) {
		const targetAngle float64 = math.Pi / 4 // 45 degrees
		ringPoint := func(axis editor.GizmoAxis3D, radius float32, angle float64) [3]float32 {
			r := float64(radius)
			ca, sa := math.Cos(angle)*r, math.Sin(angle)*r
			switch axis {
			case editor.Axis3DX:
				return [3]float32{0, float32(ca), float32(sa)}
			case editor.Axis3DY:
				return [3]float32{float32(ca), 0, float32(-sa)}
			case editor.Axis3DZ:
				return [3]float32{float32(ca), float32(sa), 0}
			default:
				return [3]float32{}
			}
		}

		// Pick starting angles away from where two rings cross so screen-space ring pick is unambiguous.
		cases := []struct {
			axis       editor.GizmoAxis3D
			startAngle float64
		}{
			{editor.Axis3DX, 0.7},
			{editor.Axis3DY, 0.7},
			{editor.Axis3DZ, 0.7},
		}
		for _, tc := range cases {
			ed := newGizmoEditor(t, editor.Gizmo3DRotate)
			gLen := ed.GizmoLength3D([3]float32{0, 0, 0})

			p0 := ringPoint(tc.axis, gLen, tc.startAngle)
			pMid := ringPoint(tc.axis, gLen, tc.startAngle+targetAngle*0.5)
			p1 := ringPoint(tc.axis, gLen, tc.startAngle+targetAngle)

			sx0, sy0, _, vis0 := ed.Camera3D.Project(p0, w, h)
			sxM, syM, _, visM := ed.Camera3D.Project(pMid, w, h)
			sx1, sy1, _, vis1 := ed.Camera3D.Project(p1, w, h)
			if !vis0 || !visM || !vis1 {
				t.Fatalf("ring %s drag points not visible", tc.axis)
			}

			picked := ed.BeginGizmoDrag3D(sx0, sy0, w, h)
			if picked != tc.axis {
				t.Fatalf("BeginGizmoDrag3D(rotate) picked %s, want %s", picked, tc.axis)
			}
			ed.UpdateGizmoDrag3D(sxM, syM)
			ed.UpdateGizmoDrag3D(sx1, sy1)
			if !ed.EndGizmoDrag3D() {
				t.Fatalf("EndGizmoDrag3D(rotate %s) returned false", tc.axis)
			}

			obj, _, _ := ed.FindObject("cube")
			wantX, wantY, wantZ := float32(0), float32(0), float32(0)
			switch tc.axis {
			case editor.Axis3DX:
				wantX = float32(targetAngle)
			case editor.Axis3DY:
				wantY = float32(targetAngle)
			case editor.Axis3DZ:
				wantZ = float32(targetAngle)
			}
			assertNear32(t, tc.axis.String()+" RotX", obj.RotX, wantX, 0.02)
			assertNear32(t, tc.axis.String()+" RotY", obj.RotY, wantY, 0.02)
			assertNear32(t, tc.axis.String()+" RotZ", obj.RotZ, wantZ, 0.02)
		}
	})

	t.Run("scale_XYZ", func(t *testing.T) {
		axes := []struct {
			axis editor.GizmoAxis3D
			dir  [3]float32
		}{
			{editor.Axis3DX, [3]float32{1, 0, 0}},
			{editor.Axis3DY, [3]float32{0, 1, 0}},
			{editor.Axis3DZ, [3]float32{0, 0, 1}},
		}
		for _, tc := range axes {
			ed := newGizmoEditor(t, editor.Gizmo3DScale)
			gLen := ed.GizmoLength3D([3]float32{0, 0, 0})

			// Grab end handle at 1.0*gLen and drag outward to 1.5*gLen -> scale factor 1.5.
			startPt := [3]float32{tc.dir[0] * gLen, tc.dir[1] * gLen, tc.dir[2] * gLen}
			endPt := [3]float32{tc.dir[0] * gLen * 1.5, tc.dir[1] * gLen * 1.5, tc.dir[2] * gLen * 1.5}

			sx0, sy0, _, vis0 := ed.Camera3D.Project(startPt, w, h)
			sx1, sy1, _, vis1 := ed.Camera3D.Project(endPt, w, h)
			if !vis0 || !vis1 {
				t.Fatalf("scale %s points not visible", tc.axis)
			}

			picked := ed.BeginGizmoDrag3D(sx0, sy0, w, h)
			if picked != tc.axis {
				t.Fatalf("BeginGizmoDrag3D(scale) picked %s, want %s", picked, tc.axis)
			}
			ed.UpdateGizmoDrag3D(sx1, sy1)
			if !ed.EndGizmoDrag3D() {
				t.Fatalf("EndGizmoDrag3D(scale %s) returned false", tc.axis)
			}

			obj, _, _ := ed.FindObject("cube")
			wantX, wantY, wantZ := float32(1), float32(1), float32(1)
			switch tc.axis {
			case editor.Axis3DX:
				wantX = 1.5
			case editor.Axis3DY:
				wantY = 1.5
			case editor.Axis3DZ:
				wantZ = 1.5
			}
			assertNear32(t, tc.axis.String()+" ScaleX", obj.EffectiveScaleX(), wantX, 0.02)
			assertNear32(t, tc.axis.String()+" ScaleY", obj.EffectiveScaleY(), wantY, 0.02)
			assertNear32(t, tc.axis.String()+" ScaleZ", obj.EffectiveScaleZ(), wantZ, 0.02)
		}
	})
}

// Undo/Redo integration test: translate/rotate/scale in 3D view -> Undo() restores original -> Redo() restores edited.
func TestGizmo3DUndoRedoRestoresExactTransformsAndYAML(t *testing.T) {
	const w, h float32 = 640, 360
	ed := newTest3DEditor(t, sceneformat.ObjectDoc{
		ID:   "crate",
		Name: "Crate",
		X:    10,
		Y:    5,
	})
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{10, 5, 0},
		Yaw:      0.65,
		Pitch:    0.55,
		Distance: 200,
		FOV:      float32(math.Pi / 3),
		Near:     0.5,
		Far:      5000,
	}
	ed.SelectObject("crate")

	mustYAML := func(step string) []byte {
		t.Helper()
		b, err := ed.SaveYAML()
		if err != nil {
			t.Fatalf("%s: SaveYAML: %v", step, err)
		}
		return b
	}

	snap0 := mustYAML("initial")

	// 1. Translate along Z via gizmo drag (multiple intermediate updates -> single undo step).
	ed.SetGizmoMode3D(editor.Gizmo3DTranslate)
	pos0 := [3]float32{10, 5, 0}
	gLen := ed.GizmoLength3D(pos0)
	sx0, sy0, _, _ := ed.Camera3D.Project([3]float32{10, 5, gLen * 0.6}, w, h)
	sxM, syM, _, _ := ed.Camera3D.Project([3]float32{10, 5, gLen*0.6 + 10}, w, h)
	sx1, sy1, _, _ := ed.Camera3D.Project([3]float32{10, 5, gLen*0.6 + 20}, w, h)
	if axis := ed.BeginGizmoDrag3D(sx0, sy0, w, h); axis != editor.Axis3DZ {
		t.Fatalf("translate picked %s, want Z", axis)
	}
	ed.UpdateGizmoDrag3D(sxM, syM)
	ed.UpdateGizmoDrag3D(sx1, sy1)
	if !ed.EndGizmoDrag3D() {
		t.Fatal("expected translate drag to commit")
	}
	snap1 := mustYAML("after translate Z")

	// 2. Rotate around Y via gizmo drag.
	ed.SetGizmoMode3D(editor.Gizmo3DRotate)
	obj, _, _ := ed.FindObject("crate")
	pos1 := [3]float32{obj.X, obj.Y, obj.PosZ}
	gLen = ed.GizmoLength3D(pos1)
	r0 := [3]float32{pos1[0] + float32(math.Cos(0.7))*gLen, pos1[1], pos1[2] - float32(math.Sin(0.7))*gLen}
	r1 := [3]float32{pos1[0] + float32(math.Cos(1.3))*gLen, pos1[1], pos1[2] - float32(math.Sin(1.3))*gLen}
	rsx0, rsy0, _, _ := ed.Camera3D.Project(r0, w, h)
	rsx1, rsy1, _, _ := ed.Camera3D.Project(r1, w, h)
	if axis := ed.BeginGizmoDrag3D(rsx0, rsy0, w, h); axis != editor.Axis3DY {
		t.Fatalf("rotate picked %s, want Y", axis)
	}
	ed.UpdateGizmoDrag3D(rsx1, rsy1)
	if !ed.EndGizmoDrag3D() {
		t.Fatal("expected rotate drag to commit")
	}
	snap2 := mustYAML("after rotate Y")

	// 3. Scale along Z via gizmo drag.
	ed.SetGizmoMode3D(editor.Gizmo3DScale)
	ssx0, ssy0, _, _ := ed.Camera3D.Project([3]float32{pos1[0], pos1[1], pos1[2] + gLen}, w, h)
	ssx1, ssy1, _, _ := ed.Camera3D.Project([3]float32{pos1[0], pos1[1], pos1[2] + gLen*1.8}, w, h)
	if axis := ed.BeginGizmoDrag3D(ssx0, ssy0, w, h); axis != editor.Axis3DZ {
		t.Fatalf("scale picked %s, want Z", axis)
	}
	ed.UpdateGizmoDrag3D(ssx1, ssy1)
	if !ed.EndGizmoDrag3D() {
		t.Fatal("expected scale drag to commit")
	}
	snap3 := mustYAML("after scale Z")

	if ed.History.Len() != 3 {
		t.Fatalf("History.Len() = %d, want 3 (one per completed gizmo drag)", ed.History.Len())
	}

	// Undo all 3 steps and assert exact YAML equality at every step!
	wantUndo := [][]byte{snap2, snap1, snap0}
	for i, want := range wantUndo {
		if !ed.Undo() {
			t.Fatalf("Undo step %d returned false", i)
		}
		got := mustYAML("undo")
		if !bytes.Equal(got, want) {
			t.Fatalf("after Undo %d YAML differs:\ngot:\n%s\nwant:\n%s", i, got, want)
		}
	}
	// Verify initial transform is completely restored (including nil ScaleZ!).
	obj, _, _ = ed.FindObject("crate")
	if obj.X != 10 || obj.Y != 5 || obj.PosZ != 0 || obj.RotX != 0 || obj.RotY != 0 || obj.RotZ != 0 || obj.ScaleZ != nil {
		t.Fatalf("crate not cleanly restored after full undo: %+v", obj)
	}

	// Redo all 3 steps and assert exact YAML equality at every step!
	wantRedo := [][]byte{snap1, snap2, snap3}
	for i, want := range wantRedo {
		if !ed.Redo() {
			t.Fatalf("Redo step %d returned false", i)
		}
		got := mustYAML("redo")
		if !bytes.Equal(got, want) {
			t.Fatalf("after Redo %d YAML differs:\ngot:\n%s\nwant:\n%s", i, got, want)
		}
	}
}

// sceneformat YAML round-trip test: edit object in 3D view -> serialize -> reload -> exact 3D transform preserved.
func TestEditor3DYAMLRoundTrip(t *testing.T) {
	ed := newTest3DEditor(t, sceneformat.ObjectDoc{
		ID:   "pillar",
		Name: "Stone Pillar",
	})
	ed.SelectObject("pillar")

	if err := ed.MoveObject3D("pillar", 24.5, -12.25, 38.75); err != nil {
		t.Fatalf("MoveObject3D: %v", err)
	}
	if err := ed.RotateObject3D("pillar", 0.3, 1.1, -0.45); err != nil {
		t.Fatalf("RotateObject3D: %v", err)
	}
	if err := ed.ScaleObject3D("pillar", 1.25, 2.5, 0.8); err != nil {
		t.Fatalf("ScaleObject3D: %v", err)
	}
	if err := ed.SetObjectDepth("pillar", 32); err != nil {
		t.Fatalf("SetObjectDepth: %v", err)
	}
	if err := ed.SetObjectMesh("pillar", "cylinder"); err != nil {
		t.Fatalf("SetObjectMesh: %v", err)
	}

	raw, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}

	reloaded, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatalf("editor.New reloaded: %v", err)
	}
	if err := reloaded.OpenYAML(raw); err != nil {
		t.Fatalf("OpenYAML: %v", err)
	}

	if !reflect.DeepEqual(reloaded.Doc, ed.Doc) {
		t.Fatalf("reloaded 3D SceneDoc mismatch:\ngot:  %#v\nwant: %#v", reloaded.Doc, ed.Doc)
	}

	got, _, _ := reloaded.FindObject("pillar")
	if got == nil {
		t.Fatal("pillar missing after reload")
	}
	if got.X != 24.5 || got.Y != -12.25 || got.PosZ != 38.75 ||
		got.RotX != 0.3 || got.RotY != 1.1 || got.RotZ != -0.45 ||
		got.EffectiveScaleX() != 1.25 || got.EffectiveScaleY() != 2.5 || got.EffectiveScaleZ() != 0.8 ||
		got.Depth != 32 || got.Mesh != "cylinder" {
		t.Fatalf("unexpected reloaded 3D pillar: %+v", got)
	}
}

// nsgotest UI test rendering the 3D viewport, toggling modes/gizmos, editing 3D inspector fields,
// and verifying canvas draw operations are emitted cleanly.
func TestEditor3DViewportUIRenderingAndControls(t *testing.T) {
	ed := newTest3DEditor(t,
		sceneformat.ObjectDoc{ID: "box_a", Name: "Box A", X: 0, Y: 8, PosZ: 0, Depth: 16},
		sceneformat.ObjectDoc{ID: "box_b", Name: "Box B", X: 48, Y: 8, PosZ: -24, Depth: 16},
	)
	ed.SetViewMode(editor.ViewMode2D) // start in 2D to verify toolbar toggle to 3D

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 720))

	// Toggle into 3D viewport mode via toolbar button.
	app.Press("id:view-mode-3d")
	if ed.ViewMode != editor.ViewMode3D {
		t.Fatalf("ViewMode = %v, want ViewMode3D", ed.ViewMode)
	}

	// Select box_a by clicking its projected 3D center on the canvas, which fills its panel.
	vw, vh := ed.CanvasSize()
	sx, sy, _, vis := ed.Camera3D.Project([3]float32{0, 8, 0}, vw, vh)
	if !vis {
		t.Fatal("box_a not visible in default 3D camera")
	}
	app.PressAt("id:editor-viewport", core.Point{X: core.Points(sx), Y: core.Points(sy)})
	if ed.SelectedID != "box_a" {
		t.Fatalf("SelectedID after 3D canvas press = %q, want box_a", ed.SelectedID)
	}

	// Verify 3D inspector controls are mounted for the selected object.
	app.Find("id:inspector-pos-z")
	app.Find("id:inspector-rot-x")
	app.Find("id:inspector-rot-y")
	app.Find("id:inspector-rot-z")
	app.Find("id:inspector-scale-z")
	app.Find("id:inspector-depth")
	app.Find("id:inspector-mesh")

	// Pick a built-in primitive in the 3D Inspector's Mesh field.
	app.Press("id:inspector-mesh")
	app.Press("role:option sphere")
	app.Settle()
	if obj, _, _ := ed.FindObject("box_a"); obj == nil || obj.Mesh != "sphere" {
		t.Fatalf("box_a.Mesh after inspector edit = %+v, want sphere", obj)
	}
	// A name that is not a built-in primitive is refused.
	if err := ed.SetObjectMesh("box_a", "cube"); err == nil {
		t.Fatal("SetObjectMesh accepted cube, which is not a built-in primitive")
	}

	// Verify Translate, Rotate, and Scale gizmo buttons render cleanly with canvas draw ops.
	checkDrawOps := func(label string) {
		t.Helper()
		ops := app.Find("id:editor-viewport").Ops
		if len(ops) == 0 {
			t.Fatalf("%s: expected non-empty DrawOps on 3D canvas", label)
		}
		var strokes, fills, lines int
		for _, op := range ops {
			switch op.Kind {
			case core.DrawStroke:
				strokes++
			case core.DrawFill:
				fills++
			case core.DrawLineTo:
				lines++
			}
			// Guard against any NaN/Inf coordinate leaking into canvas DrawOps.
			for _, f := range []float32{op.X, op.Y, op.X1, op.Y1, op.X2, op.Y2, op.W, op.H} {
				if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
					t.Fatalf("%s: non-finite draw coordinate in op %+v", label, op)
				}
			}
		}
		// Expect ground grid + 3 origin axes + 2 object wireframes + 3 gizmo axes -> >= 8 strokes and many line segments.
		if strokes < 8 || lines < 40 || fills < 1 {
			t.Fatalf("%s: unexpected 3D draw counts: strokes=%d lines=%d fills=%d", label, strokes, lines, fills)
		}
	}

	app.Press("id:gizmo-translate")
	if ed.GizmoMode != editor.Gizmo3DTranslate {
		t.Fatalf("GizmoMode = %v, want Translate", ed.GizmoMode)
	}
	checkDrawOps("3D Translate")

	app.Press("id:gizmo-rotate")
	if ed.GizmoMode != editor.Gizmo3DRotate {
		t.Fatalf("GizmoMode = %v, want Rotate", ed.GizmoMode)
	}
	checkDrawOps("3D Rotate")

	app.Press("id:gizmo-scale")
	if ed.GizmoMode != editor.Gizmo3DScale {
		t.Fatalf("GizmoMode = %v, want Scale", ed.GizmoMode)
	}
	checkDrawOps("3D Scale")

	// Toggle back to 2D view mode cleanly.
	app.Press("id:view-mode-2d")
	if ed.ViewMode != editor.ViewMode2D {
		t.Fatalf("ViewMode after toggle back = %v, want ViewMode2D", ed.ViewMode)
	}
}

// A 3D gizmo drag must be reachable from real pointer input on the mounted
// editor, not just from the model's Begin/Update/End methods.
func TestEditor3DGizmoDragThroughMountedUI(t *testing.T) {
	ed := newTest3DEditor(t, sceneformat.ObjectDoc{
		ID:    "cube",
		Name:  "Cube",
		X:     0,
		Y:     0,
		PosZ:  0,
		Depth: 16,
	})
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{0, 0, 0},
		Yaw:      0.65,
		Pitch:    0.55,
		Distance: 200,
		FOV:      float32(math.Pi / 3),
		Near:     0.5,
		Far:      5000,
	}
	ed.SelectObject("cube")
	ed.SetGizmoMode3D(editor.Gizmo3DTranslate)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 720))
	if ed.ViewMode != editor.ViewMode3D {
		t.Fatalf("ViewMode = %v, want ViewMode3D", ed.ViewMode)
	}

	vw, vh := ed.CanvasSize()
	gLen := ed.GizmoLength3D([3]float32{0, 0, 0})
	startPt := [3]float32{gLen * 0.6, 0, 0}
	endPt := [3]float32{gLen*0.6 + 25, 0, 0}
	sx0, sy0, _, vis0 := ed.Camera3D.Project(startPt, vw, vh)
	sx1, sy1, _, vis1 := ed.Camera3D.Project(endPt, vw, vh)
	if !vis0 || !vis1 {
		t.Fatal("gizmo drag points are not visible")
	}

	app.DragAt("id:editor-viewport-frame",
		core.Point{X: core.Points(sx0), Y: core.Points(sy0)},
		core.Point{X: core.Points(sx1 - sx0), Y: core.Points(sy1 - sy0)})

	obj, _, _ := ed.FindObject("cube")
	if obj == nil {
		t.Fatal("cube disappeared after the drag")
	}
	assertNear32(t, "dragged cube X", obj.X, 25, 0.5)
	if n := ed.History.Len(); n < 1 {
		t.Fatalf("history entries after a gizmo drag = %d, want the drag recorded", n)
	}
	if !ed.Undo() {
		t.Fatal("Undo after a gizmo drag returned false")
	}
	obj, _, _ = ed.FindObject("cube")
	assertNear32(t, "cube X after undo", obj.X, 0, 0.5)
}

func TestEditor3DViewportShowsTheClipPose(t *testing.T) {
	ed := newTest3DEditor(t, sceneformat.ObjectDoc{ID: "box_a", X: 0, Y: 8, PosZ: 0, Depth: 16})
	if err := ed.NewClip("box_a", "Lift", time.Second); err != nil {
		t.Fatalf("NewClip: %v", err)
	}
	if _, _, err := ed.KeyProperty("", "posZ", 0); err != nil {
		t.Fatalf("KeyProperty posZ @0: %v", err)
	}
	tr, k, err := ed.KeyProperty("", "posZ", time.Second)
	if err != nil {
		t.Fatalf("KeyProperty posZ @1s: %v", err)
	}
	if err := ed.SetKeyValue(tr, k, 80); err != nil {
		t.Fatalf("SetKeyValue: %v", err)
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return editor.EditorView(cx, ed) }, nsgotest.Size(1200, 720))
	ops := func(at time.Duration) string {
		t.Helper()
		app.Post(func() { ed.SeekClip(at) })
		app.Settle()
		return fmt.Sprint(app.Find("id:editor-viewport").Ops)
	}
	start, end := ops(0), ops(time.Second)
	if start == end {
		t.Fatal("the 3D viewport draws the same at the clip's start and end, though posZ moves from 0 to 80")
	}
	if got := ed.ActiveScene().Object("box_a").Doc.PosZ; got != 80 {
		t.Fatalf("posed posZ = %v, want 80", got)
	}
	if obj, _, _ := ed.FindObject("box_a"); obj.PosZ != 0 {
		t.Fatalf("the edited scene took the animated posZ %v", obj.PosZ)
	}
	app.Post(func() { _ = ed.CloseClip() })
	app.Settle()
	// Closing the clip closes the animation panel, so the canvas, which fills its panel, grows: the scene as edited is drawn at the new size.
	view := app.Find("id:editor-viewport")
	want := &core.Drawing{}
	ed.DrawViewport(want, view.Bounds().Size)
	if got := fmt.Sprint(view.Ops); got != fmt.Sprint(want.Ops) || got == end {
		t.Fatal("after closing the clip the 3D viewport does not draw the scene as edited")
	}
}
