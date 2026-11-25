package editor_test

import (
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/scenemodel"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type editorLODModelResolver struct {
	models   map[string]*gltf.Document
	sidecars map[string]*importsettings.File
}

func (r *editorLODModelResolver) Model(path string) (*gltf.Document, error) {
	return r.models[path], nil
}

func (r *editorLODModelResolver) ImportSettings(path string) (*importsettings.File, error) {
	if r.sidecars == nil {
		return nil, nil
	}
	return r.sidecars[path], nil
}

func testTwoLODResolver(t *testing.T, ratio float32) (*editorLODModelResolver, uint64, uint64) {
	t.Helper()
	high, err := scenemodel.PrimitiveMesh(sceneformat.MeshSphere)
	if err != nil {
		t.Fatal(err)
	}
	low, err := scenemodel.PrimitiveMesh(sceneformat.MeshBox)
	if err != nil {
		t.Fatal(err)
	}
	toGLTF := func(name string, m render3d.Mesh) gltf.Mesh {
		return gltf.Mesh{
			Name: name,
			Primitives: []gltf.Primitive{{
				Mode:       gltf.ModeTriangles,
				Positions:  m.Positions,
				Normals:    m.Normals,
				TexCoords0: m.UVs,
				Indices:    m.Indices,
				Material:   -1,
			}},
		}
	}
	doc := &gltf.Document{
		Scene:  0,
		Scenes: []gltf.Scene{{Roots: []int{0, 1}}},
		Nodes: []gltf.Node{
			{Name: "Pillar", Mesh: 0, Parent: -1, Scale: math3d.Vec3One(), Rotation: math3d.QuatIdentity()},
			{Name: "Pillar_LOD1", Mesh: 1, Parent: -1, Scale: math3d.Vec3One(), Rotation: math3d.QuatIdentity()},
		},
		Meshes: []gltf.Mesh{
			toGLTF("Pillar", high),
			toGLTF("Pillar_LOD1", low),
		},
	}
	res := &editorLODModelResolver{
		models: map[string]*gltf.Document{"models/pillar.glb": doc},
		sidecars: map[string]*importsettings.File{
			"models/pillar.glb": {
				Version: importsettings.CurrentVersion,
				Model: &importsettings.Model{
					LODs: []importsettings.ModelLOD{
						{MinScreenRatio: ratio},
						{MinScreenRatio: 0},
					},
				},
			},
		},
	}
	return res, uint64(len(high.Indices) / 3), uint64(len(low.Indices) / 3)
}

func hasFillText(ops []core.DrawOp, want string) bool {
	for _, op := range ops {
		if op.Kind == core.DrawFillText && op.Text == want {
			return true
		}
	}
	return false
}

// TestEditor3DViewportLODSwitchesAtDeclaredRatio verifies acceptance criterion 1:
// a model with two LODs switches at the declared screen-ratio threshold in the
// editor 3D viewport, updating the LOD badge drawn on the canvas, the RenderStats
// overlay, and the 3D inspector readout.
func TestEditor3DViewportLODSwitchesAtDeclaredRatio(t *testing.T) {
	const threshold float32 = 0.30
	resolver, highTris, lowTris := testTwoLODResolver(t, threshold)

	ed := newTest3DEditor(t,
		sceneformat.ObjectDoc{ID: "pillar", Name: "Pillar", Model: "models/pillar.glb"},
	)
	ed.ModelResolver = resolver
	ed.SelectObject("pillar")

	// Start near enough that projected screen ratio >= 0.30 -> LOD0.
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{0, 0, 0},
		Yaw:      0,
		Pitch:    0,
		Distance: 3,
		FOV:      float32(math.Pi / 4),
		Near:     0.1,
		Far:      100,
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 720))

	if lv, ok := ed.ObjectLOD3D("pillar"); !ok || lv != 0 {
		t.Fatalf("near ObjectLOD3D(pillar) = (%d, %v), want (0, true)", lv, ok)
	}
	nearStats := ed.RenderStats3D()
	if nearStats.Triangles != highTris {
		t.Fatalf("near RenderStats.Triangles = %d, want %d", nearStats.Triangles, highTris)
	}
	if !hasFillText(app.Find("id:editor-viewport").Ops, "LOD0") {
		t.Fatal("expected 3D viewport DrawOps to draw LOD0 label when close")
	}
	if txt := app.Find("id:viewport3d-render-stats").Text; !strings.Contains(txt, "pillar=LOD0") {
		t.Fatalf("viewport3d-render-stats = %q, want pillar=LOD0", txt)
	}
	if txt := app.Find("id:inspector-lod-active").Text; !strings.Contains(txt, "LOD 0") {
		t.Fatalf("inspector-lod-active = %q, want LOD 0", txt)
	}

	// Dolly camera back so projected screen ratio drops below 0.30 -> switches to LOD1.
	app.Post(func() {
		ed.Camera3D.Distance = 12
		ed.SetShowLODOverlay(true) // keep enabled
		// Trigger a revision update via camera wheel or explicit Zoom.
		ed.HandleViewport3DWheel(-1)
	})
	app.Settle()

	if lv, ok := ed.ObjectLOD3D("pillar"); !ok || lv != 1 {
		t.Fatalf("far ObjectLOD3D(pillar) = (%d, %v), want (1, true)", lv, ok)
	}
	farStats := ed.RenderStats3D()
	if farStats.Triangles != lowTris {
		t.Fatalf("far RenderStats.Triangles = %d, want %d", farStats.Triangles, lowTris)
	}
	if !hasFillText(app.Find("id:editor-viewport").Ops, "LOD1") {
		t.Fatal("expected 3D viewport DrawOps to draw LOD1 label when far")
	}
	if txt := app.Find("id:viewport3d-render-stats").Text; !strings.Contains(txt, "pillar=LOD1") {
		t.Fatalf("viewport3d-render-stats = %q, want pillar=LOD1", txt)
	}
	if txt := app.Find("id:inspector-lod-active").Text; !strings.Contains(txt, "LOD 1") {
		t.Fatalf("inspector-lod-active = %q, want LOD 1", txt)
	}

	// Toggling the LOD overlay button hides the LOD badge on the canvas.
	app.Press("id:viewport3d-toggle-lod")
	if ed.ShowLODOverlay {
		t.Fatal("ShowLODOverlay remained true after pressing toggle")
	}
	if hasFillText(app.Find("id:editor-viewport").Ops, "LOD1") {
		t.Fatal("expected LOD1 label to be hidden when ShowLODOverlay is false")
	}

	// Toggling the Stats button hides the RenderStats HUD overlay.
	app.Press("id:viewport3d-toggle-stats")
	if ed.ShowRenderStats {
		t.Fatal("ShowRenderStats remained true after pressing toggle")
	}
}

// TestEditor3DViewportOccluderWallCullsAndDraws verifies acceptance criterion 2:
// an occluder wall culls objects behind it, RenderStats.Occluded reports them in
// the viewport HUD overlay, and the editor draws the occluder volume.
func TestEditor3DViewportOccluderWallCullsAndDraws(t *testing.T) {
	wallSize := [3]float32{12, 8, 1}
	ed := newTest3DEditor(t,
		sceneformat.ObjectDoc{
			ID:       "wall",
			Name:     "Occluder Wall",
			PosZ:     -4,
			Occluder: &sceneformat.OccluderDoc{Mode: sceneformat.OccluderBox, Size: &wallSize},
		},
		sceneformat.ObjectDoc{ID: "hidden_a", X: 0, Y: 0, PosZ: -10, Mesh: sceneformat.MeshBox},
		sceneformat.ObjectDoc{ID: "hidden_b", X: 1.5, Y: 0, PosZ: -12, Mesh: sceneformat.MeshSphere},
		sceneformat.ObjectDoc{ID: "front", X: 0, Y: 0, PosZ: -1, Mesh: sceneformat.MeshBox},
	)
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{0, 0, -10},
		Yaw:      0,
		Pitch:    0,
		Distance: 15, // Eye at (0, 0, 5) looking down -Z through wall at z=-4
		FOV:      float32(math.Pi / 3),
		Near:     0.1,
		Far:      100,
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 720))

	stats := ed.RenderStats3D()
	if stats.Occluded != 2 {
		t.Fatalf("RenderStats3D().Occluded = %d, want 2 (stats=%s)", stats.Occluded, stats.Summary())
	}
	if !ed.ObjectOccluded3D("hidden_a") || !ed.ObjectOccluded3D("hidden_b") {
		t.Fatalf("expected hidden_a and hidden_b to be occluded, got (%v, %v)", ed.ObjectOccluded3D("hidden_a"), ed.ObjectOccluded3D("hidden_b"))
	}
	if ed.ObjectOccluded3D("front") {
		t.Fatal("front object in front of wall should not be occluded")
	}

	if txt := app.Find("id:viewport3d-render-stats").Text; !strings.Contains(txt, "Occluded: 2") {
		t.Fatalf("viewport3d-render-stats = %q, want Occluded: 2", txt)
	}
	if !hasFillText(app.Find("id:editor-viewport").Ops, "OCC BOX") {
		t.Fatal("expected 3D viewport to draw OCC BOX occluder visualization")
	}

	// Toggling the Occluders toolbar button hides the occluder drawing.
	app.Press("id:viewport3d-toggle-occluders")
	if ed.ShowOccluders {
		t.Fatal("ShowOccluders remained true after pressing toggle")
	}
	if hasFillText(app.Find("id:editor-viewport").Ops, "OCC BOX") {
		t.Fatal("expected OCC BOX visualization to hide when ShowOccluders is false")
	}

	// Editing an object's occluder through SetObjectOccluder supports Undo/Redo.
	if err := ed.SetObjectOccluder("front", &sceneformat.OccluderDoc{Mode: sceneformat.OccluderModel, DoubleSided: true}); err != nil {
		t.Fatalf("SetObjectOccluder(front, model): %v", err)
	}
	if obj, _, _ := ed.FindObject("front"); obj.Occluder == nil || obj.Occluder.Mode != sceneformat.OccluderModel || !obj.Occluder.DoubleSided {
		t.Fatalf("front.Occluder after set = %+v", obj.Occluder)
	}
	if !ed.Undo() {
		t.Fatal("Undo SetObjectOccluder failed")
	}
	if obj, _, _ := ed.FindObject("front"); obj.Occluder != nil {
		t.Fatalf("front.Occluder after undo = %+v, want nil", obj.Occluder)
	}
}
