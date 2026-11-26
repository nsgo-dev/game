package editor_test

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

func assertVec3Near32(t *testing.T, label string, got, want [3]float32, tol float32) {
	t.Helper()
	assertNear32(t, label+".X", got[0], want[0], tol)
	assertNear32(t, label+".Y", got[1], want[1], tol)
	assertNear32(t, label+".Z", got[2], want[2], tol)
}

func cornersCenter3D(corners [8][3]float32) [3]float32 {
	var sum [3]float64
	for _, c := range corners {
		sum[0] += float64(c[0])
		sum[1] += float64(c[1])
		sum[2] += float64(c[2])
	}
	return [3]float32{float32(sum[0] / 8), float32(sum[1] / 8), float32(sum[2] / 8)}
}

func buildNodes3DForEditor(t *testing.T, ed *editor.Editor) *sceneformat.Nodes3D {
	t.Helper()
	doc := ed.Doc
	if ed.Built != nil && ed.Built.Doc != nil {
		doc = ed.Built.Doc
	}
	root := scene3d.NewNode("root")
	nodes, err := sceneformat.BuildNodes3D(doc, root)
	if err != nil {
		t.Fatalf("sceneformat.BuildNodes3D: %v", err)
	}
	return nodes
}

// Acceptance #1:
// A child of a rotated, non-uniformly scaled parent draws at the exact world transform
// BuildNodes3D produces, picking hits the child, and dragging its gizmo moves it under the
// pointer while writing its local transform in parent space.
func TestViewport3DNestedChildDrawsAtBuildNodes3DWorldTransformAndGizmoEditsInParentSpace(t *testing.T) {
	const w, h float32 = 640, 360

	parent := sceneformat.ObjectDoc{
		ID:     "rig",
		Name:   "Rig",
		X:      10,
		Y:      5,
		PosZ:   -4,
		RotX:   0.35,
		RotY:   float32(math.Pi / 2),
		RotZ:   -0.25,
		ScaleX: sceneformat.Float32(2),
		ScaleY: sceneformat.Float32(3),
		ScaleZ: sceneformat.Float32(0.5),
		Children: []sceneformat.ObjectDoc{
			{
				ID:     "arm",
				Name:   "Arm",
				X:      6,
				Y:      -2,
				PosZ:   8,
				RotY:   0.4,
				ScaleX: sceneformat.Float32(2),
				ScaleY: sceneformat.Float32(2),
				ScaleZ: sceneformat.Float32(2),
				Mesh:   "box",
			},
		},
	}

	ed := newTest3DEditor(t, parent)
	nodes := buildNodes3DForEditor(t, ed)
	armNode := nodes.Objects["arm"]
	if armNode == nil {
		t.Fatal("BuildNodes3D missing arm")
	}
	wantWorldMat := armNode.WorldMatrix()
	wantWorldPos := [3]float32{wantWorldMat[12], wantWorldMat[13], wantWorldMat[14]}

	// Verify Scene3D() node world matrix matches BuildNodes3D identically.
	built3D, err := ed.Scene3D()
	if err != nil {
		t.Fatalf("Scene3D: %v", err)
	}
	gotBuiltMat := built3D.Objects["arm"].WorldMatrix()
	for i := range 16 {
		assertNear32(t, fmt.Sprintf("Scene3D WorldMatrix[%d]", i), gotBuiltMat[i], wantWorldMat[i], 1e-4)
	}

	// Verify ObjectCorners3D matches BuildNodes3D transforming the unit box [-0.5, 0.5]^3 corners.
	armDoc, _, _ := ed.FindObject("arm")
	if armDoc == nil {
		t.Fatal("FindObject(arm) = nil")
	}
	corners := ed.ObjectCorners3D(armDoc)
	gotCenter := cornersCenter3D(corners)
	assertVec3Near32(t, "ObjectCorners3D center vs BuildNodes3D", gotCenter, wantWorldPos, 1e-3)

	// Verify DrawViewport3D renders cleanly with no NaN/Inf.
	d := &core.Drawing{}
	ed.DrawViewport3D(d, core.Size{Width: core.Points(w), Height: core.Points(h)})
	if len(d.Ops) == 0 {
		t.Fatal("DrawViewport3D emitted no draw operations")
	}

	// Frame camera on arm so it is cleanly visible and unobstructed.
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   wantWorldPos,
		Yaw:      0.55,
		Pitch:    0.4,
		Distance: 40,
		FOV:      float32(math.Pi / 3),
		Near:     0.1,
		Far:      1000,
	}

	// Picking at arm's composed world position hits "arm".
	cx, cy, _, vis := ed.Camera3D.Project(wantWorldPos, w, h)
	if !vis {
		t.Fatal("arm world position is not visible")
	}
	pickedID, ok := ed.Pick3DObject(cx, cy, w, h)
	if !ok || pickedID != "arm" {
		t.Fatalf("Pick3DObject at arm world pos = (%q, %v), want (arm, true)", pickedID, ok)
	}
	ed.HandleViewport3DPress(cx, cy, w, h)
	if ed.SelectedID != "arm" {
		t.Fatalf("SelectedID after press = %q, want arm", ed.SelectedID)
	}

	// Drag translate gizmo along each world axis: arm moves in world space by exact delta
	// while writing the exact inverted parent-space local transform!
	initialYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	ed.SetGizmoMode3D(editor.Gizmo3DTranslate)
	ed.SetGizmoSpace3D(editor.GizmoSpaceGlobal)

	const worldStep float32 = 6
	axes := []struct {
		axis editor.GizmoAxis3D
		dir  [3]float32
	}{
		{editor.Axis3DX, [3]float32{1, 0, 0}},
		{editor.Axis3DY, [3]float32{0, 1, 0}},
		{editor.Axis3DZ, [3]float32{0, 0, 1}},
	}
	for _, tc := range axes {
		curNodes := buildNodes3DForEditor(t, ed)
		curMat := curNodes.Objects["arm"].WorldMatrix()
		origin := [3]float32{curMat[12], curMat[13], curMat[14]}
		gLen := ed.GizmoLength3D(origin)

		startPt := [3]float32{
			origin[0] + tc.dir[0]*gLen*0.6,
			origin[1] + tc.dir[1]*gLen*0.6,
			origin[2] + tc.dir[2]*gLen*0.6,
		}
		endPt := [3]float32{
			startPt[0] + tc.dir[0]*worldStep,
			startPt[1] + tc.dir[1]*worldStep,
			startPt[2] + tc.dir[2]*worldStep,
		}
		sx0, sy0, _, vis0 := ed.Camera3D.Project(startPt, w, h)
		sx1, sy1, _, vis1 := ed.Camera3D.Project(endPt, w, h)
		if !vis0 || !vis1 {
			t.Fatalf("axis %s drag points not visible", tc.axis)
		}
		gotAxis := ed.BeginGizmoDrag3D(sx0, sy0, w, h)
		if gotAxis != tc.axis {
			t.Fatalf("BeginGizmoDrag3D(%s) = %v, want %v", tc.axis, gotAxis, tc.axis)
		}
		ed.UpdateGizmoDrag3D(sx1, sy1)
		if !ed.EndGizmoDrag3D() {
			t.Fatalf("EndGizmoDrag3D(%s) returned false", tc.axis)
		}

		// Verify BuildNodes3D world position moved by exact worldStep along tc.dir!
		afterNodes := buildNodes3DForEditor(t, ed)
		afterMat := afterNodes.Objects["arm"].WorldMatrix()
		gotPos := [3]float32{afterMat[12], afterMat[13], afterMat[14]}
		wantPos := [3]float32{
			origin[0] + tc.dir[0]*worldStep,
			origin[1] + tc.dir[1]*worldStep,
			origin[2] + tc.dir[2]*worldStep,
		}
		assertVec3Near32(t, tc.axis.String()+" world pos after drag", gotPos, wantPos, 0.05)

		// Verify local transform matches parentWorld^-1 * wantPos!
		parentInv, invOK := afterNodes.Objects["rig"].WorldMatrix().Inverse()
		if !invOK {
			t.Fatal("parent matrix not invertible")
		}
		wantLocal := parentInv.TransformPoint(math3d.Vec3{X: wantPos[0], Y: wantPos[1], Z: wantPos[2]})
		armDoc, _, _ = ed.FindObject("arm")
		assertVec3Near32(t, tc.axis.String()+" local pos", [3]float32{armDoc.X, armDoc.Y, armDoc.PosZ}, [3]float32{wantLocal.X, wantLocal.Y, wantLocal.Z}, 0.05)

		// Parent itself must remain untouched.
		rigDoc, _, _ := ed.FindObject("rig")
		if rigDoc.X != 10 || rigDoc.Y != 5 || rigDoc.PosZ != -4 {
			t.Fatalf("parent rig moved unexpectedly: (%v, %v, %v)", rigDoc.X, rigDoc.Y, rigDoc.PosZ)
		}
	}

	// Undo all 3 translate drags restores exact initial YAML.
	if ed.History.Len() != 3 {
		t.Fatalf("History.Len() = %d, want 3", ed.History.Len())
	}
	for range 3 {
		if !ed.Undo() {
			t.Fatal("Undo failed")
		}
	}
	afterUndoYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML after undo: %v", err)
	}
	if !bytes.Equal(afterUndoYAML, initialYAML) {
		t.Fatalf("YAML after undo differs:\ngot:\n%s\nwant:\n%s", afterUndoYAML, initialYAML)
	}
}

// TestViewport3D64LevelNestingMatchesBuildNodes3D verifies 64 levels of object nesting draw,
// compose world transforms matching BuildNodes3D, and pick cleanly.
func TestViewport3D64LevelNestingMatchesBuildNodes3D(t *testing.T) {
	const depth = sceneformat.MaxObjectDepth
	leaf := sceneformat.ObjectDoc{
		ID:   fmt.Sprintf("node_%d", depth),
		X:    1,
		Y:    0.5,
		PosZ: 0.25,
		Mesh: "box",
	}
	cur := leaf
	for level := depth - 1; level >= 1; level-- {
		var ry float32
		if level%8 == 0 {
			ry = 0.1
		}
		cur = sceneformat.ObjectDoc{
			ID:       fmt.Sprintf("node_%d", level),
			X:        0.5,
			Y:        0.2,
			PosZ:     0.1,
			RotY:     ry,
			Children: []sceneformat.ObjectDoc{cur},
		}
	}

	ed := newTest3DEditor(t, cur)
	if len(ed.Objects3D()) != depth {
		t.Fatalf("Objects3D len = %d, want %d", len(ed.Objects3D()), depth)
	}
	nodes := buildNodes3DForEditor(t, ed)
	leafID := fmt.Sprintf("node_%d", depth)
	wantMat := nodes.Objects[leafID].WorldMatrix()
	wantPos := [3]float32{wantMat[12], wantMat[13], wantMat[14]}

	leafDoc, _, _ := ed.FindObject(leafID)
	gotCenter := cornersCenter3D(ed.ObjectCorners3D(leafDoc))
	assertVec3Near32(t, "64-level leaf ObjectCorners3D center", gotCenter, wantPos, 1e-2)

	ed.Camera3D = editor.OrbitCamera3D{
		Target:   wantPos,
		Yaw:      0.4,
		Pitch:    0.3,
		Distance: 12,
		FOV:      float32(math.Pi / 3),
		Near:     0.1,
		Far:      1000,
	}
	const w, h float32 = 640, 360
	sx, sy, _, vis := ed.Camera3D.Project(wantPos, w, h)
	if !vis {
		t.Fatal("64-level leaf not visible")
	}
	if id, ok := ed.Pick3DObject(sx, sy, w, h); !ok || id != leafID {
		t.Fatalf("Pick3DObject on 64-level leaf = (%q, %v), want (%q, true)", id, ok, leafID)
	}
}

// TestViewport3DHierarchicalPickingSecondPressAndAltModifier verifies:
//   - first press picks the deepest hit object (both mesh and non-mesh box inside a scaled parent)
//   - a second press on the already-selected child steps up to its parent (and instance root)
//     even when clicking directly over the gizmo origin, eventually cycling back to the child
//   - Alt/Option+press directly selects the parent (for local children) or owning scene instance root
//     (for instanced children), and composes with Shift/Mod multi-selection.
func TestViewport3DHierarchicalPickingSecondPressAndAltModifier(t *testing.T) {
	const w, h float32 = 640, 360

	t.Run("local_nested_hierarchy_and_enclosing_scaled_parent", func(t *testing.T) {
		ed := newTest3DEditor(t, sceneformat.ObjectDoc{
			ID:     "ship",
			X:      0,
			Y:      0,
			PosZ:   0,
			ScaleX: sceneformat.Float32(3),
			ScaleY: sceneformat.Float32(3),
			ScaleZ: sceneformat.Float32(3),
			Children: []sceneformat.ObjectDoc{
				{
					ID:     "turret",
					ScaleX: sceneformat.Float32(0.5),
					ScaleY: sceneformat.Float32(0.5),
					ScaleZ: sceneformat.Float32(0.5),
					Children: []sceneformat.ObjectDoc{
						{
							ID:     "barrel",
							ScaleX: sceneformat.Float32(0.5),
							ScaleY: sceneformat.Float32(0.5),
							ScaleZ: sceneformat.Float32(0.5),
						},
					},
				},
			},
		})
		ed.Camera3D = editor.OrbitCamera3D{
			Target:   [3]float32{0, 0, 0},
			Yaw:      0,
			Pitch:    0,
			Distance: 200,
			FOV:      float32(math.Pi / 3),
			Near:     0.5,
			Far:      5000,
		}
		sx, sy, _, vis := ed.Camera3D.Project([3]float32{0, 0, 0}, w, h)
		if !vis {
			t.Fatal("origin not visible")
		}

		// Non-geometry boxes overlap with ship's outer box entered FIRST along the ray:
		// Pick3DObject must still pick the deepest hit descendant ("barrel")!
		if id, ok := ed.Pick3DObject(sx, sy, w, h); !ok || id != "barrel" {
			t.Fatalf("Pick3DObject on nested overlapping boxes = (%q, %v), want (barrel, true)", id, ok)
		}

		// 1st press selects deepest child "barrel".
		ed.HandleViewport3DPress(sx, sy, w, h)
		if ed.SelectedID != "barrel" {
			t.Fatalf("1st press SelectedID = %q, want barrel", ed.SelectedID)
		}
		// 2nd press at the exact same pixel (over the gizmo origin!) selects parent "turret".
		ed.HandleViewport3DPress(sx, sy, w, h)
		if ed.SelectedID != "turret" {
			t.Fatalf("2nd press SelectedID = %q, want turret", ed.SelectedID)
		}
		// 3rd press steps up to grandparent "ship".
		ed.HandleViewport3DPress(sx, sy, w, h)
		if ed.SelectedID != "ship" {
			t.Fatalf("3rd press SelectedID = %q, want ship", ed.SelectedID)
		}
		// 4th press cycles back to deepest hit "barrel".
		ed.HandleViewport3DPress(sx, sy, w, h)
		if ed.SelectedID != "barrel" {
			t.Fatalf("4th press SelectedID = %q, want barrel", ed.SelectedID)
		}

		// Alt/Option+press from empty selection directly picks parent "turret".
		ed.ClearSelection()
		ed.HandleViewport3DPressWithModifiers(sx, sy, w, h, false, false, true)
		if ed.SelectedID != "turret" {
			t.Fatalf("Alt+press SelectedID = %q, want turret", ed.SelectedID)
		}
	})

	t.Run("instanced_scene_second_press_and_alt_instance_root", func(t *testing.T) {
		doc := &sceneformat.SceneDoc{
			Version: sceneformat.CurrentVersion,
			Layers: []sceneformat.LayerDoc{
				{
					Name: "world",
					Objects: []sceneformat.ObjectDoc{
						{
							ID:    "turret_1",
							Scene: "scenes/turret.scene.yaml",
							X:     5,
							Y:     0,
							PosZ:  0,
						},
						{
							ID:   "crate",
							X:    -10,
							Y:    0,
							PosZ: 0,
							Mesh: "box",
						},
					},
				},
			},
		}
		ed, err := editor.New(doc, sceneFiles{
			AtlasMap: testResolver(),
			files: map[string]string{
				"scenes/turret.scene.yaml": `version: 4
layers:
  - name: parts
    objects:
      - id: base
        y: 0
        children:
          - id: barrel
            x: 0
            y: 2
            posZ: 0
            scaleX: 4
            scaleY: 4
            scaleZ: 4
            mesh: box
`,
			},
		})
		if err != nil {
			t.Fatalf("editor.New: %v", err)
		}
		ed.SetViewMode(editor.ViewMode3D)
		ed.SetCanvasSize(w, h)
		ed.Camera3D = editor.OrbitCamera3D{
			Target:   [3]float32{5, 2, 0},
			Yaw:      0.3,
			Pitch:    0.2,
			Distance: 30,
			FOV:      float32(math.Pi / 3),
			Near:     0.1,
			Far:      1000,
		}

		sx, sy, _, vis := ed.Camera3D.Project([3]float32{5, 2, 0}, w, h)
		if !vis {
			t.Fatal("instanced barrel not visible")
		}

		// 1st press picks deepest instanced child "turret_1/barrel".
		ed.HandleCanvasPress(sx, sy)
		if ed.SelectedID != "turret_1/barrel" {
			t.Fatalf("1st press = %q, want turret_1/barrel", ed.SelectedID)
		}
		// 2nd press walks up to its parent inside the instance "turret_1/base".
		ed.HandleCanvasPress(sx, sy)
		if ed.SelectedID != "turret_1/base" {
			t.Fatalf("2nd press = %q, want turret_1/base", ed.SelectedID)
		}
		// 3rd press walks up to the instance root "turret_1".
		ed.HandleCanvasPress(sx, sy)
		if ed.SelectedID != "turret_1" {
			t.Fatalf("3rd press = %q, want turret_1", ed.SelectedID)
		}
		// 4th press cycles back to deepest hit "turret_1/barrel".
		ed.HandleCanvasPress(sx, sy)
		if ed.SelectedID != "turret_1/barrel" {
			t.Fatalf("4th press = %q, want turret_1/barrel", ed.SelectedID)
		}

		// Alt/Option+press jumps straight to the owning instance root "turret_1" even from 2 levels deep!
		ed.ClearSelection()
		ed.HandleCanvasPressWithModifiers(sx, sy, false, false, true)
		if ed.SelectedID != "turret_1" {
			t.Fatalf("Alt+press on nested instanced child = %q, want instance root turret_1", ed.SelectedID)
		}

		// Shift+Alt+press adds the instance root to an existing selection.
		ed.SelectObject("crate")
		ed.HandleCanvasPressWithModifiers(sx, sy, true, false, true)
		if got := ed.SelectedIDs(); !slices.Equal(got, []string{"crate", "turret_1"}) {
			t.Fatalf("Shift+Alt+press SelectedIDs = %v, want [crate turret_1]", got)
		}
	})
}

// Acceptance #2:
// Moving an object inside a 3D scene instance (including under a rotated, scaled instance root)
// draws and picks the gizmo at the child's world position, writes an override entry under the
// instance's overrides without modifying the source scene file, round-trips through YAML,
// and undoes cleanly in ONE undo step.
func TestViewport3DSceneInstanceChildGizmoWritesOverrideRoundTripsAndUndoesInOneStep(t *testing.T) {
	dir := t.TempDir()
	propYAML := `version: 4
layers:
  - name: parts
    objects:
      - id: base
        x: 0
        "y": 0
        posZ: 0
        children:
          - id: cannon
            x: 2
            "y": 1
            posZ: 3
            scaleX: 2
            scaleY: 2
            scaleZ: 2
            mesh: box
      - id: antenna
        x: -3
        "y": 2
        posZ: 0
        scaleX: 2
        scaleY: 2
        scaleZ: 2
        mesh: box
`
	levelYAML := `version: 4
name: 3D Instance Level
virtualWidth: 640
virtualHeight: 360
layers:
  - name: world
    objects:
      - id: turret_1
        scene: scenes/prop.scene.yaml
        x: 10
        "y": 4
        posZ: -2
        rotY: 1.5707964
        scaleX: 2
        scaleY: 2
        scaleZ: 2
`
	files := map[string]string{
		project.FileName: `version: 1
name: 3D Instance Test
id: dev.nsgo.instance3d
appVersion: 0.1.0
window:
  width: 640
  height: 360
autoload:
  mainScene: level
  scenes:
    - name: level
      path: scenes/level.scene.yaml
    - name: prop
      path: scenes/prop.scene.yaml
`,
		"scenes/prop.scene.yaml":  propYAML,
		"scenes/level.scene.yaml": levelYAML,
	}
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	propPath := filepath.Join(dir, "scenes", "prop.scene.yaml")
	propBefore, err := os.ReadFile(propPath)
	if err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.ProjectAssets = true
	t.Cleanup(ed.WaitForProblems)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.SetViewMode(editor.ViewMode3D)
	const w, h float32 = 640, 360
	ed.SetCanvasSize(w, h)

	const childID = "turret_1/cannon"
	nodesBefore := buildNodes3DForEditor(t, ed)
	startMat := nodesBefore.Objects[childID].WorldMatrix()
	startWorld := [3]float32{startMat[12], startMat[13], startMat[14]}

	ed.Camera3D = editor.OrbitCamera3D{
		Target:   startWorld,
		Yaw:      0.6,
		Pitch:    0.45,
		Distance: 45,
		FOV:      float32(math.Pi / 3),
		Near:     0.1,
		Far:      1000,
	}

	// 1. Pick the instanced child in the 3D viewport.
	cx, cy, _, vis := ed.Camera3D.Project(startWorld, w, h)
	if !vis {
		t.Fatal("instanced child not visible")
	}
	ed.HandleViewport3DPress(cx, cy, w, h)
	if ed.SelectedID != childID {
		t.Fatalf("SelectedID after press on instanced child = %q, want %q", ed.SelectedID, childID)
	}

	preDragYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML pre-drag: %v", err)
	}
	histBefore := ed.History.Len()

	// 2. Verify gizmo draws and picks on the instanced child at its world position,
	//    and drag it along world +X across multiple update steps.
	ed.SetGizmoMode3D(editor.Gizmo3DTranslate)
	ed.SetGizmoSpace3D(editor.GizmoSpaceGlobal)
	gLen := ed.GizmoLength3D(startWorld)
	dragStart := [3]float32{startWorld[0] + gLen*0.6, startWorld[1], startWorld[2]}
	dragMid := [3]float32{dragStart[0] + 4, dragStart[1], dragStart[2]}
	dragEnd := [3]float32{dragStart[0] + 10, dragStart[1], dragStart[2]}

	sx0, sy0, _, vis0 := ed.Camera3D.Project(dragStart, w, h)
	sxM, syM, _, visM := ed.Camera3D.Project(dragMid, w, h)
	sx1, sy1, _, vis1 := ed.Camera3D.Project(dragEnd, w, h)
	if !vis0 || !visM || !vis1 {
		t.Fatal("gizmo drag points not visible")
	}

	// CancelGizmoDrag3D first: verify cancel leaves zero history and untouched YAML.
	if axis := ed.BeginGizmoDrag3D(sx0, sy0, w, h); axis != editor.Axis3DX {
		t.Fatalf("BeginGizmoDrag3D on instanced child = %v, want Axis3DX", axis)
	}
	ed.UpdateGizmoDrag3D(sxM, syM)
	ed.CancelGizmoDrag3D()
	if ed.History.Len() != histBefore {
		t.Fatalf("CancelGizmoDrag3D polluted History: got %d, want %d", ed.History.Len(), histBefore)
	}
	if afterCancel, err := ed.SaveYAML(); err != nil || !bytes.Equal(afterCancel, preDragYAML) {
		t.Fatalf("CancelGizmoDrag3D did not restore YAML (err %v)", err)
	}

	// Now perform the committed multi-step drag.
	if axis := ed.BeginGizmoDrag3D(sx0, sy0, w, h); axis != editor.Axis3DX {
		t.Fatalf("BeginGizmoDrag3D on instanced child = %v, want Axis3DX", axis)
	}
	ed.UpdateGizmoDrag3D(sxM, syM)
	ed.UpdateGizmoDrag3D(sx1, sy1)
	if !ed.EndGizmoDrag3D() {
		t.Fatal("EndGizmoDrag3D on instanced child returned false")
	}

	// Must commit in exactly ONE undo step!
	if ed.History.Len() != histBefore+1 {
		t.Fatalf("History.Len() after 3D gizmo drag on instanced child = %d, want %d (one undo step)", ed.History.Len(), histBefore+1)
	}

	// World position moved by +10 along world +X; because turret_1 is rotated +90 deg around Y
	// and scaled 2x, world +X maps to parent local +Z (delta_local_z = +10 / 2 = +5 -> PosZ = 3 + 5 = 8)!
	nodesAfter := buildNodes3DForEditor(t, ed)
	afterMat := nodesAfter.Objects[childID].WorldMatrix()
	afterWorld := [3]float32{afterMat[12], afterMat[13], afterMat[14]}
	wantWorld := [3]float32{startWorld[0] + 10, startWorld[1], startWorld[2]}
	assertVec3Near32(t, "instanced child world pos after drag", afterWorld, wantWorld, 0.05)

	instRoot, _, _ := ed.FindObject("turret_1")
	if instRoot == nil || len(instRoot.Overrides) != 1 {
		t.Fatalf("turret_1.Overrides = %+v, want 1 override entry for cannon", instRoot)
	}
	ov, ok := instRoot.Overrides["cannon"]
	if !ok {
		t.Fatalf("turret_1.Overrides keys = %+v, want key \"cannon\"", instRoot.Overrides)
	}
	if ov.PosZ == nil {
		t.Fatalf("cannon override = %+v, want PosZ override set", ov)
	}
	assertNear32(t, "cannon override PosZ", *ov.PosZ, 8, 0.05)

	// 3. Save to disk, verify source scene file is untouched, and verify YAML round-trip!
	if err := ed.Save(); err != nil {
		t.Fatalf("Save level: %v", err)
	}
	propAfter, err := os.ReadFile(propPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(propBefore, propAfter) {
		t.Fatalf("instanced source scene file was modified!\nbefore:\n%s\nafter:\n%s", propBefore, propAfter)
	}
	savedLevel, err := os.ReadFile(filepath.Join(dir, "scenes", "level.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(savedLevel, []byte("scene: scenes/prop.scene.yaml")) || !bytes.Contains(savedLevel, []byte("overrides:")) || !bytes.Contains(savedLevel, []byte("cannon:")) {
		t.Fatalf("saved level YAML missing scene reference or cannon override:\n%s", savedLevel)
	}
	reloadedDoc, err := sceneformat.Parse(savedLevel)
	if err != nil {
		t.Fatalf("Parse saved level YAML: %v", err)
	}
	reEncoded, err := sceneformat.Encode(reloadedDoc)
	if err != nil || !bytes.Equal(reEncoded, savedLevel) {
		t.Fatalf("saved level YAML failed round-trip (err %v)", err)
	}

	// Re-open project in a fresh Editor and verify exact resolved 3D world position survives round-trip!
	ed2, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("fresh editor.New: %v", err)
	}
	ed2.ProjectAssets = true
	t.Cleanup(ed2.WaitForProblems)
	if err := ed2.OpenProject(dir); err != nil {
		t.Fatalf("fresh OpenProject: %v", err)
	}
	reloadedNodes := buildNodes3DForEditor(t, ed2)
	reloadedMat := reloadedNodes.Objects[childID].WorldMatrix()
	assertVec3Near32(t, "reloaded instanced child world pos", [3]float32{reloadedMat[12], reloadedMat[13], reloadedMat[14]}, wantWorld, 0.05)

	// 4. Undo in ONE step restores exact pre-drag YAML (including pruning empty overrides map)!
	if !ed.Undo() {
		t.Fatal("Undo failed")
	}
	if ed.History.CanUndo() {
		t.Fatalf("expected single Undo to undo the entire drag, but CanUndo() is still true (len=%d)", ed.History.Len())
	}
	afterUndoYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML after undo: %v", err)
	}
	if !bytes.Equal(afterUndoYAML, preDragYAML) {
		t.Fatalf("YAML after single Undo differs from pre-drag YAML:\ngot:\n%s\nwant:\n%s", afterUndoYAML, preDragYAML)
	}
	if instRoot, _, _ = ed.FindObject("turret_1"); instRoot.Overrides != nil {
		t.Fatalf("turret_1.Overrides after Undo = %+v, want nil", instRoot.Overrides)
	}
	// Redo restores the override cleanly.
	if !ed.Redo() {
		t.Fatal("Redo failed")
	}
	if !ed.HasObjectOverride(childID, "posZ") {
		t.Fatal("expected posZ override after Redo")
	}

	// 5. Multi-selecting two instanced children and dragging the 3D gizmo commits both in ONE undo step!
	if !ed.SelectObjects([]string{"turret_1/cannon", "turret_1/antenna"}) {
		t.Fatal("SelectObjects on two instanced children failed")
	}
	multiHistBefore := ed.History.Len()
	// Recompute multi-selection gizmo origin (midpoint of both instanced children).
	curNodes := buildNodes3DForEditor(t, ed)
	p1 := curNodes.Objects["turret_1/cannon"].WorldMatrix()
	p2 := curNodes.Objects["turret_1/antenna"].WorldMatrix()
	midOrigin := [3]float32{(p1[12] + p2[12]) * 0.5, (p1[13] + p2[13]) * 0.5, (p1[14] + p2[14]) * 0.5}
	ed.Camera3D.Target = midOrigin
	gLenMulti := ed.GizmoLength3D(midOrigin)
	ms0, my0, _, mvis0 := ed.Camera3D.Project([3]float32{midOrigin[0], midOrigin[1] + gLenMulti*0.6, midOrigin[2]}, w, h)
	ms1, my1, _, mvis1 := ed.Camera3D.Project([3]float32{midOrigin[0], midOrigin[1] + gLenMulti*0.6 + 6, midOrigin[2]}, w, h)
	if !mvis0 || !mvis1 {
		t.Fatal("multi-select gizmo points not visible")
	}
	if axis := ed.BeginGizmoDrag3D(ms0, my0, w, h); axis != editor.Axis3DY {
		t.Fatalf("multi-select BeginGizmoDrag3D = %v, want Axis3DY", axis)
	}
	ed.UpdateGizmoDrag3D(ms1, my1)
	if !ed.EndGizmoDrag3D() {
		t.Fatal("multi-select EndGizmoDrag3D returned false")
	}
	if ed.History.Len() != multiHistBefore+1 {
		t.Fatalf("multi-select drag recorded %d history steps, want 1", ed.History.Len()-multiHistBefore)
	}
	if !ed.HasObjectOverride("turret_1/cannon", "y") || !ed.HasObjectOverride("turret_1/antenna", "y") {
		t.Fatalf("expected both instanced children to have y override after multi-select drag: cannon=%v antenna=%v",
			ed.ObjectOverrideFields("turret_1/cannon"), ed.ObjectOverrideFields("turret_1/antenna"))
	}
	if !ed.Undo() {
		t.Fatal("Undo multi-select drag failed")
	}
	if ed.HasObjectOverride("turret_1/antenna", "y") {
		t.Fatal("expected antenna y override to be cleared after single Undo of multi-select drag")
	}
}
