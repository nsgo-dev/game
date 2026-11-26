package editor_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/physics3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func makeEditorTestGLTFBox(minV, maxV math3d.Vec3) []byte {
	verts := [8][3]float32{
		{minV.X, minV.Y, minV.Z},
		{maxV.X, minV.Y, minV.Z},
		{maxV.X, maxV.Y, minV.Z},
		{minV.X, maxV.Y, minV.Z},
		{minV.X, minV.Y, maxV.Z},
		{maxV.X, minV.Y, maxV.Z},
		{maxV.X, maxV.Y, maxV.Z},
		{minV.X, maxV.Y, maxV.Z},
	}
	indices := []uint16{
		4, 5, 6, 4, 6, 7,
		1, 0, 3, 1, 3, 2,
		5, 1, 2, 5, 2, 6,
		0, 4, 7, 0, 7, 3,
		7, 6, 2, 7, 2, 3,
		0, 1, 5, 0, 5, 4,
	}
	var bin bytes.Buffer
	for _, v := range verts {
		_ = binary.Write(&bin, binary.LittleEndian, v)
	}
	idxOffset := bin.Len()
	for _, ix := range indices {
		_ = binary.Write(&bin, binary.LittleEndian, ix)
	}
	for bin.Len()%4 != 0 {
		bin.WriteByte(0)
	}
	b64 := base64.StdEncoding.EncodeToString(bin.Bytes())
	return []byte(fmt.Sprintf(`{
  "asset": {"version": "2.0"},
  "scene": 0,
  "scenes": [{"nodes": [0]}],
  "nodes": [{"name": "Crate", "mesh": 0}],
  "meshes": [{
    "name": "CrateMesh",
    "primitives": [{
      "attributes": {"POSITION": 0},
      "indices": 1
    }]
  }],
  "buffers": [{"uri": "data:application/octet-stream;base64,%s", "byteLength": %d}],
  "bufferViews": [
    {"buffer": 0, "byteOffset": 0, "byteLength": %d, "target": 34962},
    {"buffer": 0, "byteOffset": %d, "byteLength": %d, "target": 34963}
  ],
  "accessors": [
    {"bufferView": 0, "componentType": 5126, "count": 8, "type": "VEC3", "min": [%g,%g,%g], "max": [%g,%g,%g]},
    {"bufferView": 1, "componentType": 5123, "count": %d, "type": "SCALAR"}
  ]
}`, b64, bin.Len(), idxOffset, idxOffset, len(indices)*2,
		minV.X, minV.Y, minV.Z, maxV.X, maxV.Y, maxV.Z, len(indices)))
}

// Acceptance #902: A box collider set in the editor collides in the game exactly where the editor draws it.
func TestBoxCollider3DEditorDrawMatchesGameCollision(t *testing.T) {
	ed := newTest3DEditor(t,
		sceneformat.ObjectDoc{
			ID:     "wall",
			X:      3.0,
			Y:      1.0,
			PosZ:   -2.0,
			RotY:   0.35,
			ScaleX: sceneformat.Float32(1.5),
			ScaleY: sceneformat.Float32(2.0),
			ScaleZ: sceneformat.Float32(1.0),
			Mesh:   "box",
			Components: map[string]any{
				"body3d": map[string]any{"version": 1, "kind": "static"},
				"collider3d": map[string]any{
					"version": 1,
					"shape":   "box",
					"width":   2.0,
					"height":  1.5,
					"depth":   1.2,
					"offsetX": 0.5,
					"offsetY": 0.25,
					"offsetZ": -0.3,
					"rotY":    0.25,
				},
			},
		},
	)
	ed.SelectObject("wall")

	// 1. Verify editor draws wireframe ops for the box collider.
	var withDebug, withoutDebug core.Drawing
	ed.SetShowPhysicsDebug(true)
	ed.DrawViewport3D(&withDebug, core.Size{Width: 640, Height: 360})
	ed.SetShowPhysicsDebug(false)
	ed.DrawViewport3D(&withoutDebug, core.Size{Width: 640, Height: 360})
	if len(withDebug.Ops) <= len(withoutDebug.Ops) {
		t.Fatalf("expected 3D collider wireframe draw ops (%d) > without (%d)", len(withDebug.Ops), len(withoutDebug.Ops))
	}

	// 2. Save the scene YAML from the editor and build the game's physics3d.World from it.
	raw, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	doc, err := sceneformat.ParseWith(raw, sceneformat.ParseOptions{
		File:       "scenes/wall.scene.yaml",
		Components: sceneformat.PhysicsComponentRegistry(),
	})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ps, err := sceneformat.BuildPhysics(built, nil, nil)
	if err != nil {
		t.Fatalf("BuildPhysics: %v", err)
	}

	// 3. Compare the exact 12 oriented box edges the editor draws against the game body's WorldShape.
	wallDoc := &doc.Layers[0].Objects[0]
	drawn := sceneformat.ExtractPhysicsDebug3DAt(wallDoc, sceneformat.ObjectLocalTransform3D(wallDoc))
	if len(drawn) != 1 || drawn[0].Shape.Kind != physics3d.ShapeBox {
		t.Fatalf("drawn shape = %+v, want 1 ShapeBox", drawn)
	}
	gameShape := ps.Bodies3D["wall"].WorldShape()
	drawnEdges := physics3d.OrientedBoxEdges(drawn[0].Shape.Box, drawn[0].Shape.Rot)
	gameEdges := physics3d.OrientedBoxEdges(gameShape.Box, gameShape.Rot)
	for i := range drawnEdges {
		for end := range 2 {
			a, b := drawnEdges[i][end], gameEdges[i][end]
			if a.Sub(b).Len() > 1e-4 {
				t.Fatalf("edge %d[%d] mismatch: editor draws %+v, game collides at %+v", i, end, a, b)
			}
		}
	}

	// 4. Verify raycast & dynamic collision hit the exact drawn box surface and miss just outside it.
	center := drawn[0].Shape.Box.Center
	rot := drawn[0].Shape.Rot
	uX := rot.RotateVec(physics3d.Vec3{X: 1})
	hx := drawn[0].Shape.Box.HalfExtents.X
	facePt := center.Add(uX.Scale(hx))

	// Ray starting 5 units outside the +X face aimed straight into the drawn face must hit at distance 5.
	rayOrigin := facePt.Add(uX.Scale(5.0))
	rayEnd := facePt.Sub(uX.Scale(5.0))
	hit, ok := ps.World3D.RaycastFirst(rayOrigin, rayEnd)
	if !ok {
		t.Fatal("raycast toward drawn +X collider face missed")
	}
	if hit.Point.Sub(facePt).Len() > 1e-3 {
		t.Fatalf("raycast hit %+v, want drawn +X face %+v", hit.Point, facePt)
	}

	// Ray skimming 0.05 units above the drawn +Y face must miss completely.
	uY := rot.RotateVec(physics3d.Vec3{Y: 1})
	hy := drawn[0].Shape.Box.HalfExtents.Y
	missOrigin := rayOrigin.Add(uY.Scale(hy + 0.05))
	missEnd := rayEnd.Add(uY.Scale(hy + 0.05))
	if _, hitAbove := ps.World3D.RaycastFirst(missOrigin, missEnd); hitAbove {
		t.Fatal("raycast just outside drawn +Y collider face unexpectedly hit")
	}

	// Dynamic probe launched straight into the drawn +X face stops at face + probe radius.
	probe := ps.World3D.AddBody(physics3d.NewDynamicBody(
		physics3d.SphereShape(physics3d.Sphere{Radius: 0.25}),
		facePt.Add(uX.Scale(1.0)),
		1.0,
	))
	probe.SetGravityScale(0)
	probe.LinearDamping = 0
	probe.Restitution = 0
	probe.Vel = uX.Scale(-4.0)
	for range 60 {
		ps.World3D.Step(time.Second / 60)
	}
	distFromCenterAlongX := probe.Pos.Sub(center).Dot(uX)
	wantDist := hx + 0.25
	if math.Abs(float64(distFromCenterAlongX-wantDist)) > 0.05 {
		t.Fatalf("probe stopped at %.3f along local +X, want %.3f (drawn face %.3f + radius 0.25)", distFromCenterAlongX, wantDist, hx)
	}
}

// Acceptance #902: Dragging a size handle changes the field as one undo step.
func TestCollider3DGizmoSizeHandlesDragAndSingleUndoStep(t *testing.T) {
	const w, h float32 = 640, 360

	setup := func(t *testing.T, col map[string]any) *editor.Editor {
		t.Helper()
		ed := newTest3DEditor(t, sceneformat.ObjectDoc{
			ID:   "prop",
			X:    0,
			Y:    0,
			PosZ: 0,
			Mesh: "box",
			Components: map[string]any{
				"collider3d": col,
			},
		})
		ed.Camera3D = editor.OrbitCamera3D{
			Target:   [3]float32{0, 0, 0},
			Yaw:      0.65,
			Pitch:    0.45,
			Distance: 12,
			FOV:      float32(math.Pi / 3),
			Near:     0.1,
			Far:      1000,
		}
		ed.SelectObject("prop")
		ed.SetGizmoMode3D(editor.Gizmo3DCollider)
		return ed
	}

	findHandle := func(t *testing.T, ed *editor.Editor, axis editor.GizmoAxis3D, sign float32) editor.ColliderHandle3D {
		t.Helper()
		obj, _, _ := ed.FindObject("prop")
		for _, hh := range ed.ColliderHandles3D(obj) {
			if hh.Axis == axis && hh.Sign == sign {
				return hh
			}
		}
		t.Fatalf("no handle for axis=%s sign=%v", axis, sign)
		return editor.ColliderHandle3D{}
	}

	t.Run("box_handles_one_undo_step_and_exact_yaml_restore", func(t *testing.T) {
		ed := setup(t, map[string]any{
			"version": 1,
			"shape":   "box",
			"width":   2.0,
			"height":  2.0,
			"depth":   2.0,
		})
		snap0, err := ed.SaveYAML()
		if err != nil {
			t.Fatal(err)
		}

		// Drag +X handle outward by +0.75 world units across 3 intermediate updates => width increases by +1.5 to 3.5 as ONE undo step.
		hx := findHandle(t, ed, editor.Axis3DX, +1)
		sx0, sy0, _, vis0 := ed.Camera3D.Project(hx.Pos, w, h)
		sxM, syM, _, visM := ed.Camera3D.Project([3]float32{hx.Pos[0] + 0.35, hx.Pos[1], hx.Pos[2]}, w, h)
		sx1, sy1, _, vis1 := ed.Camera3D.Project([3]float32{hx.Pos[0] + 0.75, hx.Pos[1], hx.Pos[2]}, w, h)
		if !vis0 || !visM || !vis1 {
			t.Fatal("box +X handle points not visible")
		}
		if picked := ed.PickGizmo3D(sx0, sy0, w, h); picked != editor.Axis3DX {
			t.Fatalf("PickGizmo3D(+X) = %v, want Axis3DX", picked)
		}
		if axis := ed.BeginGizmoDrag3D(sx0, sy0, w, h); axis != editor.Axis3DX {
			t.Fatalf("BeginGizmoDrag3D(+X) = %v, want Axis3DX", axis)
		}
		ed.UpdateGizmoDrag3D(sxM, syM)
		ed.UpdateGizmoDrag3D(sx1, sy1)
		if !ed.EndGizmoDrag3D() {
			t.Fatal("EndGizmoDrag3D returned false")
		}
		if ed.History.Len() != 1 {
			t.Fatalf("History.Len() = %d, want 1", ed.History.Len())
		}
		obj, _, _ := ed.FindObject("prop")
		gotW := obj.Components["collider3d"].(map[string]any)["width"].(float64)
		assertNear32(t, "width after +X drag", float32(gotW), 3.5, 0.05)

		// Drag -Z handle outward by +0.5 along its outward direction (-Z) => depth increases by +1.0 to 3.0.
		hz := findHandle(t, ed, editor.Axis3DZ, -1)
		zx0, zy0, _, _ := ed.Camera3D.Project(hz.Pos, w, h)
		zx1, zy1, _, _ := ed.Camera3D.Project([3]float32{hz.Pos[0], hz.Pos[1], hz.Pos[2] - 0.5}, w, h)
		if axis := ed.BeginGizmoDrag3D(zx0, zy0, w, h); axis != editor.Axis3DZ {
			t.Fatalf("BeginGizmoDrag3D(-Z) = %v, want Axis3DZ", axis)
		}
		ed.UpdateGizmoDrag3D(zx1, zy1)
		if !ed.EndGizmoDrag3D() {
			t.Fatal("EndGizmoDrag3D(-Z) returned false")
		}
		if ed.History.Len() != 2 {
			t.Fatalf("History.Len() = %d, want 2", ed.History.Len())
		}
		gotD := obj.Components["collider3d"].(map[string]any)["depth"].(float64)
		assertNear32(t, "depth after -Z drag", float32(gotD), 3.0, 0.05)

		// Cancel drag restores field and adds no history entry.
		hy := findHandle(t, ed, editor.Axis3DY, +1)
		yx0, yy0, _, _ := ed.Camera3D.Project(hy.Pos, w, h)
		yx1, yy1, _, _ := ed.Camera3D.Project([3]float32{hy.Pos[0], hy.Pos[1] + 1.0, hy.Pos[2]}, w, h)
		ed.BeginGizmoDrag3D(yx0, yy0, w, h)
		ed.UpdateGizmoDrag3D(yx1, yy1)
		ed.CancelGizmoDrag3D()
		if ed.History.Len() != 2 {
			t.Fatalf("History.Len() after Cancel = %d, want 2", ed.History.Len())
		}
		gotH := obj.Components["collider3d"].(map[string]any)["height"].(float64)
		assertNear32(t, "height after cancel", float32(gotH), 2.0, 1e-4)

		// Undo both steps and verify byte-for-byte YAML equality.
		if !ed.Undo() || !ed.Undo() {
			t.Fatal("expected two Undo steps")
		}
		afterUndo, err := ed.SaveYAML()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(afterUndo, snap0) {
			t.Fatalf("YAML after Undo mismatch:\ngot:\n%s\nwant:\n%s", afterUndo, snap0)
		}
	})

	t.Run("sphere_capsule_cylinder_handles", func(t *testing.T) {
		// Sphere: drag +X handle by +0.4 => radius increases from 1.0 to 1.4.
		edSph := setup(t, map[string]any{"version": 1, "shape": "sphere", "radius": 1.0})
		hs := findHandle(t, edSph, editor.Axis3DX, +1)
		x0, y0, _, _ := edSph.Camera3D.Project(hs.Pos, w, h)
		x1, y1, _, _ := edSph.Camera3D.Project([3]float32{hs.Pos[0] + 0.4, hs.Pos[1], hs.Pos[2]}, w, h)
		if axis := edSph.BeginGizmoDrag3D(x0, y0, w, h); axis != editor.Axis3DX {
			t.Fatalf("sphere pick = %v", axis)
		}
		edSph.UpdateGizmoDrag3D(x1, y1)
		edSph.EndGizmoDrag3D()
		obj, _, _ := edSph.FindObject("prop")
		assertNear32(t, "sphere radius", float32(obj.Components["collider3d"].(map[string]any)["radius"].(float64)), 1.4, 0.05)

		// Capsule: +Y adjusts height, +X adjusts radius.
		edCap := setup(t, map[string]any{"version": 1, "shape": "capsule", "radius": 0.5, "height": 2.0})
		hy := findHandle(t, edCap, editor.Axis3DY, +1)
		x0, y0, _, _ = edCap.Camera3D.Project(hy.Pos, w, h)
		x1, y1, _, _ = edCap.Camera3D.Project([3]float32{hy.Pos[0], hy.Pos[1] + 0.5, hy.Pos[2]}, w, h)
		edCap.BeginGizmoDrag3D(x0, y0, w, h)
		edCap.UpdateGizmoDrag3D(x1, y1)
		edCap.EndGizmoDrag3D()
		obj, _, _ = edCap.FindObject("prop")
		assertNear32(t, "capsule height", float32(obj.Components["collider3d"].(map[string]any)["height"].(float64)), 3.0, 0.05)

		// Cylinder: +X adjusts radius, +Y adjusts height.
		edCyl := setup(t, map[string]any{"version": 1, "shape": "cylinder", "radius": 0.5, "height": 1.5})
		hx := findHandle(t, edCyl, editor.Axis3DX, +1)
		x0, y0, _, _ = edCyl.Camera3D.Project(hx.Pos, w, h)
		x1, y1, _, _ = edCyl.Camera3D.Project([3]float32{hx.Pos[0] + 0.3, hx.Pos[1], hx.Pos[2]}, w, h)
		edCyl.BeginGizmoDrag3D(x0, y0, w, h)
		edCyl.UpdateGizmoDrag3D(x1, y1)
		edCyl.EndGizmoDrag3D()
		obj, _, _ = edCyl.FindObject("prop")
		assertNear32(t, "cylinder radius", float32(obj.Components["collider3d"].(map[string]any)["radius"].(float64)), 0.8, 0.05)
	})
}

func TestFitCollider3DToModelBoundsAndInspectorButton(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "terrain"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Off-center glTF box: min=(-1, 0, -2), max=(3, 4, 2) => size=(4,4,4), center=(1,2,0).
	gltfBytes := makeEditorTestGLTFBox(math3d.Vec3{X: -1, Y: 0, Z: -2}, math3d.Vec3{X: 3, Y: 4, Z: 2})
	if err := os.WriteFile(filepath.Join(dir, "models", "crate.gltf"), gltfBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	// Also create a heightmap + terrain file for wireframe rendering verification.
	hmImg := image.NewGray(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			hmImg.SetGray(x, y, color.Gray{Y: 128})
		}
	}
	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, hmImg)
	if err := os.WriteFile(filepath.Join(dir, "terrain", "h.png"), pngBuf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "terrain", "ground.terrain.yaml"), []byte("version: 1\nheightmap: terrain/h.png\ncellSize: 2\nheightScale: 4\nlayers:\n  - name: grass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte("version: 1\nname: FitTest\nid: dev.nsgo.fittest\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sceneYAML := `version: 3
name: Crate Room
layers:
  - name: world
    space: world
    objects:
      - id: crate
        model: models/crate.gltf
        components:
          collider3d:
            version: 1
            shape: box
            width: 1
            height: 1
            depth: 1
      - id: cyl
        x: 6
        mesh: cylinder
        components:
          collider3d:
            version: 1
            shape: cylinder
            radius: 0.2
            height: 0.5
      - id: hull_obj
        x: -6
        model: models/crate.gltf
        components:
          collider3d:
            version: 1
            shape: convex
      - id: tri_obj
        x: 10
        model: models/crate.gltf
        components:
          collider3d:
            version: 1
            shape: trimesh
      - id: terr_obj
        x: -10
        components:
          collider3d:
            version: 1
            shape: heightfield
            terrain: terrain/ground.terrain.yaml
`
	scenePath := filepath.Join(dir, "room.scene.yaml")
	if err := os.WriteFile(scenePath, []byte(sceneYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if err := ed.OpenFile(scenePath); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	ed.SetViewMode(editor.ViewMode3D)
	ed.SelectObject("crate")

	snapBefore, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 1400))

	// Switch GizmoMode to Gizmo3DCollider.
	app.Post(func() { ed.SetGizmoMode3D(editor.Gizmo3DCollider) })
	app.Settle()
	if ed.GizmoMode != editor.Gizmo3DCollider {
		t.Fatalf("GizmoMode = %v, want Gizmo3DCollider", ed.GizmoMode)
	}

	// Press Inspector's "Fit to Model Bounds" button on crate.
	app.Press("id:inspector-comp-collider3d-fit-bounds")
	app.Settle()

	crate, _, _ := ed.FindObject("crate")
	col := crate.Components["collider3d"].(map[string]any)
	assertNear32(t, "fitted width", float32(col["width"].(float64)), 4.0, 1e-3)
	assertNear32(t, "fitted height", float32(col["height"].(float64)), 4.0, 1e-3)
	assertNear32(t, "fitted depth", float32(col["depth"].(float64)), 4.0, 1e-3)
	assertNear32(t, "fitted offsetX", float32(col["offsetX"].(float64)), 1.0, 1e-3)
	assertNear32(t, "fitted offsetY", float32(col["offsetY"].(float64)), 2.0, 1e-3)
	if _, hasOffZ := col["offsetZ"]; hasOffZ && col["offsetZ"].(float64) != 0 {
		t.Fatalf("expected offsetZ 0/omitted, got %v", col["offsetZ"])
	}
	if ed.History.Len() != 1 {
		t.Fatalf("History.Len() after Fit = %d, want 1", ed.History.Len())
	}

	// Undo restores the exact pre-fit scene YAML.
	if !ed.Undo() {
		t.Fatal("Undo after Fit failed")
	}
	afterUndo, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterUndo, snapBefore) {
		t.Fatalf("YAML after Undo of Fit mismatch:\ngot:\n%s\nwant:\n%s", afterUndo, snapBefore)
	}

	// Also verify FitCollider3DToBounds works on cylinder primitive (radius=0.5, height=1.0).
	if err := ed.FitCollider3DToBounds("cyl"); err != nil {
		t.Fatalf("FitCollider3DToBounds(cyl): %v", err)
	}
	cylObj, _, _ := ed.FindObject("cyl")
	cylCol := cylObj.Components["collider3d"].(map[string]any)
	assertNear32(t, "cyl radius", float32(cylCol["radius"].(float64)), 0.5, 1e-3)
	assertNear32(t, "cyl height", float32(cylCol["height"].(float64)), 1.0, 1e-3)

	// Verify DrawViewport3D draws wireframes + collider handles cleanly for cylinder, convex, trimesh, and heightfield.
	var withDebug, withoutDebug core.Drawing
	ed.SetShowPhysicsDebug(true)
	ed.DrawViewport3D(&withDebug, core.Size{Width: 640, Height: 360})
	ed.SetShowPhysicsDebug(false)
	ed.SetGizmoMode3D(editor.Gizmo3DTranslate)
	ed.DrawViewport3D(&withoutDebug, core.Size{Width: 640, Height: 360})
	if len(withDebug.Ops) <= len(withoutDebug.Ops) {
		t.Fatalf("expected debug drawing ops for all 3D collider shapes (%d) > without (%d)", len(withDebug.Ops), len(withoutDebug.Ops))
	}
}

func TestCollider3DGizmoBlockedByGridMapAndTerrainBrush(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version: 3,
		Layers: []sceneformat.LayerDoc{{
			Name:  "world",
			Space: "world",
			Objects: []sceneformat.ObjectDoc{{
				ID: "prop",
				Components: map[string]any{
					"collider3d": map[string]any{"version": 1, "shape": "box", "width": 2.0, "height": 2.0, "depth": 2.0},
				},
			}},
		}},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.SetViewMode(editor.ViewMode3D)
	ed.SetGizmoMode3D(editor.Gizmo3DCollider)
	ed.SelectObject("prop")
	ed.Camera3D.Target = [3]float32{0, 0, 0}
	ed.Camera3D.Yaw = 0.5
	ed.Camera3D.Pitch = 0.35
	ed.Camera3D.Distance = 8

	const w, h float32 = 800, 600
	obj, _, _ := ed.FindObject("prop")
	handles := ed.ColliderHandles3D(obj)
	if len(handles) == 0 {
		t.Fatal("expected box collider handles")
	}
	sx, sy, _, vis := ed.Camera3D.Project(handles[0].Pos, w, h)
	if !vis {
		t.Fatal("handle not visible")
	}

	// GridMap paint tool active blocks PickGizmo3D, BeginGizmoDrag3D, and BeginGizmoDragAxis3D.
	ed.SetGridMapTool(editor.GridMapToolPaint)
	if ax := ed.PickGizmo3D(sx, sy, w, h); ax != editor.Axis3DNone {
		t.Fatalf("PickGizmo3D with GridMap tool = %v, want Axis3DNone", ax)
	}
	if ax := ed.BeginGizmoDrag3D(sx, sy, w, h); ax != editor.Axis3DNone {
		t.Fatalf("BeginGizmoDrag3D with GridMap tool = %v, want Axis3DNone", ax)
	}
	if ed.BeginGizmoDragAxis3D(editor.Axis3DX, sx, sy, w, h) {
		t.Fatal("BeginGizmoDragAxis3D with GridMap tool = true, want false")
	}
	ed.SetGridMapTool(editor.GridMapToolNone)

	// 3D Terrain brush active blocks PickGizmo3D, BeginGizmoDrag3D, and BeginGizmoDragAxis3D.
	if err := ed.NewTerrain(editor.NewTerrainOptions{}); err != nil {
		t.Fatalf("NewTerrain: %v", err)
	}
	ed.SetTerrainBrush(editor.TerrainBrush{Mode: editor.TerrainBrushRaise, Radius: 2, Strength: 0.5})
	if ax := ed.PickGizmo3D(sx, sy, w, h); ax != editor.Axis3DNone {
		t.Fatalf("PickGizmo3D with Terrain brush = %v, want Axis3DNone", ax)
	}
	if ax := ed.BeginGizmoDrag3D(sx, sy, w, h); ax != editor.Axis3DNone {
		t.Fatalf("BeginGizmoDrag3D with Terrain brush = %v, want Axis3DNone", ax)
	}
	if ed.BeginGizmoDragAxis3D(editor.Axis3DX, sx, sy, w, h) {
		t.Fatal("BeginGizmoDragAxis3D with Terrain brush = true, want false")
	}

	// Clearing the brush re-enables the collider gizmo.
	ed.SetTerrainBrush(editor.TerrainBrush{Mode: editor.TerrainBrushNone})
	if !ed.BeginGizmoDragAxis3D(editor.Axis3DX, sx, sy, w, h) {
		t.Fatal("BeginGizmoDragAxis3D after clearing Terrain brush = false, want true")
	}
	ed.CancelGizmoDrag3D()
}

func TestCollider3DCapsuleDragClampsHeightAndRadius(t *testing.T) {
	setupCap := func(t *testing.T) *editor.Editor {
		t.Helper()
		doc := &sceneformat.SceneDoc{
			Version: 3,
			Layers: []sceneformat.LayerDoc{{
				Name:  "world",
				Space: "world",
				Objects: []sceneformat.ObjectDoc{{
					ID: "cap",
					Components: map[string]any{
						"collider3d": map[string]any{"version": 1, "shape": "capsule", "radius": 0.6, "height": 2.0},
					},
				}},
			}},
		}
		ed, err := editor.New(doc, nil)
		if err != nil {
			t.Fatal(err)
		}
		ed.SetViewMode(editor.ViewMode3D)
		ed.SetGizmoMode3D(editor.Gizmo3DCollider)
		ed.SelectObject("cap")
		ed.Camera3D.Target = [3]float32{0, 0, 0}
		ed.Camera3D.Yaw = 0.5
		ed.Camera3D.Pitch = 0.25
		ed.Camera3D.Distance = 8
		return ed
	}
	const w, h float32 = 800, 600

	// Dragging capsule +Y handle far downward must clamp height >= 2*radius (1.2).
	edH := setupCap(t)
	obj, _, _ := edH.FindObject("cap")
	var hy editor.ColliderHandle3D
	for _, hh := range edH.ColliderHandles3D(obj) {
		if hh.Axis == editor.Axis3DY && hh.Sign > 0 {
			hy = hh
			break
		}
	}
	x0, y0, _, _ := edH.Camera3D.Project(hy.Pos, w, h)
	x1, y1, _, _ := edH.Camera3D.Project([3]float32{hy.Pos[0], hy.Pos[1] - 2.0, hy.Pos[2]}, w, h)
	if ax := edH.BeginGizmoDrag3D(x0, y0, w, h); ax != editor.Axis3DY {
		t.Fatalf("capsule height pick = %v, want Axis3DY", ax)
	}
	edH.UpdateGizmoDrag3D(x1, y1)
	edH.EndGizmoDrag3D()
	obj, _, _ = edH.FindObject("cap")
	gotH := obj.Components["collider3d"].(map[string]any)["height"].(float64)
	assertNear32(t, "clamped capsule height", float32(gotH), 1.2, 1e-3)

	// Dragging capsule +X handle far outward must clamp radius <= height/2 (1.0).
	edR := setupCap(t)
	obj, _, _ = edR.FindObject("cap")
	var hx editor.ColliderHandle3D
	for _, hh := range edR.ColliderHandles3D(obj) {
		if hh.Axis == editor.Axis3DX && hh.Sign > 0 {
			hx = hh
			break
		}
	}
	x0, y0, _, _ = edR.Camera3D.Project(hx.Pos, w, h)
	x1, y1, _, _ = edR.Camera3D.Project([3]float32{hx.Pos[0] + 2.0, hx.Pos[1], hx.Pos[2]}, w, h)
	if ax := edR.BeginGizmoDrag3D(x0, y0, w, h); ax != editor.Axis3DX {
		t.Fatalf("capsule radius pick = %v, want Axis3DX", ax)
	}
	edR.UpdateGizmoDrag3D(x1, y1)
	edR.EndGizmoDrag3D()
	obj, _, _ = edR.FindObject("cap")
	gotR := obj.Components["collider3d"].(map[string]any)["radius"].(float64)
	assertNear32(t, "clamped capsule radius", float32(gotR), 1.0, 1e-3)
}
