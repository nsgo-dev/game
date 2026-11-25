package editor_test

import (
	"bytes"
	"errors"
	"math"
	"os"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

type navTileResolver struct {
	sceneformat.AtlasMap
	files map[string][]byte
}

func (r navTileResolver) ReadFile(p string) ([]byte, error) {
	b, ok := r.files[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	return b, nil
}

func navPointsEqual(a, b []core.Point, eps float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(float64(a[i].X-b[i].X)) > eps || math.Abs(float64(a[i].Y-b[i].Y)) > eps {
			return false
		}
	}
	return true
}

// TestEditorNavRegionPathMatchesGame verifies Acceptance Criterion 1:
// A region drawn in the editor (with an obstacle cutout and subtracted tile collision)
// yields the exact same path in the editor's test-path tool and in the game.
func TestEditorNavRegionPathMatchesGame(t *testing.T) {
	tilesetYAML := []byte(`version: 1
name: arena
tileWidth: 20
tileHeight: 20
image: tiles.png
columns: 2
tileCount: 2
tiles:
  - id: 0
    solid: true
  - id: 1
`)
	doc := &sceneformat.SceneDoc{
		Version:       2,
		Name:          "Nav Arena",
		VirtualWidth:  320,
		VirtualHeight: 200,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "world",
				Space: sceneformat.SpaceWorld,
				Tilemap: &sceneformat.TilemapDoc{
					TilesetFile: "tiles/arena.tileset.yaml",
					Width:       16,
					Height:      10,
					TileWidth:   20,
					TileHeight:  20,
					TileLayers: []sceneformat.TileLayerDoc{
						{
							Name: "walls",
							Rows: []string{
								". . . . . . . 0 . . . . . . . .",
								". . . . . . . 0 . . . . . . . .",
								". . . . . . . 0 . . . . . . . .",
								". . . . . . . 0 . . . . . . . .",
								". . . . . . . 0 . . . . . . . .",
								". . . . . . . 0 . . . . . . . .",
								". . . . . . . . . . . . . . . .",
								". . . . . . . . . . . . . . . .",
								". . . . . . . . . . . . . . . .",
								". . . . . . . . . . . . . . . .",
							},
						},
					},
				},
			},
		},
	}
	resolver := navTileResolver{files: map[string][]byte{"tiles/arena.tileset.yaml": tilesetYAML}}
	ed, err := editor.New(doc, resolver)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetCanvasSize(320, 200)

	// Draw walkable navigation region in the editor and enable tile collision subtraction.
	id, err := ed.PlaceNavRegion2D("world", [][2]float64{
		{10, 10}, {310, 10}, {310, 190}, {10, 190},
	})
	if err != nil {
		t.Fatalf("PlaceNavRegion2D: %v", err)
	}
	if err := ed.SetComponentField(id, sceneformat.NavRegion2DComponentName, "subtractTileCollision", true); err != nil {
		t.Fatalf("SetComponentField(subtractTileCollision): %v", err)
	}
	if err := ed.SetComponentField(id, sceneformat.NavRegion2DComponentName, "agentRadius", 4.0); err != nil {
		t.Fatalf("SetComponentField(agentRadius): %v", err)
	}

	// Use the Test Path tool via viewport clicks: press start, then press end.
	ed.SetTool(editor.ToolNavTestPath)
	sx, sy := ed.WorldToCanvas(40, 40)
	ex, ey := ed.WorldToCanvas(260, 40)
	ed.HandleCanvasPress(sx, sy)
	p1 := ed.NavPathPreview2D()
	if !p1.HasStart || p1.HasEnd {
		t.Fatalf("after first press = %+v, want HasStart=true HasEnd=false", p1)
	}
	ed.HandleCanvasPress(ex, ey)
	preview := ed.NavPathPreview2D()
	if !preview.HasStart || !preview.HasEnd || !preview.Found || len(preview.Path) < 3 {
		t.Fatalf("after second press = %+v, want Found detour path with >= 3 points", preview)
	}

	// Save YAML from the editor, then load & build it as the game does.
	saved, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	gameDoc, err := sceneformat.ParseWith(saved, sceneformat.ParseOptions{
		Components: sceneformat.NavRegion2DComponentRegistry(),
	})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	reencoded, err := sceneformat.Encode(gameDoc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(saved, reencoded) {
		t.Fatalf("saved YAML did not round-trip byte-for-byte:\n--- saved ---\n%s\n--- reencoded ---\n%s", saved, reencoded)
	}
	gameBuilt, err := sceneformat.Build(gameDoc, resolver)
	if err != nil {
		t.Fatalf("game Build: %v", err)
	}
	if gameBuilt.NavMesh2D == nil {
		t.Fatal("expected game BuiltScene.NavMesh2D to be non-nil")
	}

	gamePath, ok := gameBuilt.NavMesh2D.FindPath(preview.Start, preview.End)
	if !ok {
		t.Fatal("expected game FindPath to succeed")
	}
	if !navPointsEqual(preview.Path, gamePath, 1e-4) {
		t.Fatalf("path mismatch:\n  editor=%v\n  game  =%v", preview.Path, gamePath)
	}

	// Pressing a third time starts a new test path at the new start point.
	nx, ny := ed.WorldToCanvas(60, 150)
	ed.HandleCanvasPress(nx, ny)
	p3 := ed.NavPathPreview2D()
	if !p3.HasStart || p3.HasEnd {
		t.Fatalf("after third press = %+v, want fresh start only", p3)
	}
}

// TestNavRegionVertexEditUpdatesOverlayAndOneUndoReverts verifies Acceptance Criterion 2:
// Editing a vertex updates the built navmesh overlay immediately, and one Undo reverts both
// the polygon and the overlay back to their exact pre-drag state.
func TestNavRegionVertexEditUpdatesOverlayAndOneUndoReverts(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:       2,
		Name:          "Nav Handles",
		VirtualWidth:  320,
		VirtualHeight: 200,
		Layers: []sceneformat.LayerDoc{{
			Name:  "world",
			Space: sceneformat.SpaceWorld,
		}},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetCanvasSize(320, 200)

	// Place an initial concave-ready 4-vertex polygon centered at (160, 100).
	id, err := ed.PlaceNavRegion2D("world", [][2]float64{
		{40, 40}, {280, 40}, {280, 160}, {40, 160},
	})
	if err != nil {
		t.Fatalf("PlaceNavRegion2D: %v", err)
	}
	ed.SelectObject(id)

	// Set a test path that crosses near vertex 1 (280, 40).
	start := core.Point{X: 60, Y: 60}
	goal := core.Point{X: 260, Y: 60}
	origPath, ok := ed.SetNavTestPathPoints(start, goal)
	if !ok || len(origPath) != 2 {
		t.Fatalf("initial path = (%v, %v), want straight 2-point path", origPath, ok)
	}

	origYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML(before): %v", err)
	}
	origDraw := &core.Drawing{}
	ed.DrawViewport(origDraw, core.Size{Width: 320, Height: 200})

	// Pick vertex 1 at (280, 40) and drag it to (180, 100) so (260, 60) falls outside the region!
	vx, vy := ed.WorldToCanvas(280, 40)
	if idx := ed.PickNavRegionVertex2D(vx, vy); idx != 1 {
		t.Fatalf("PickNavRegionVertex2D = %d, want 1", idx)
	}
	if h := ed.PickGizmo2D(vx, vy); h != editor.Handle2DNavRegionVertex {
		t.Fatalf("PickGizmo2D at vertex = %v, want Handle2DNavRegionVertex", h)
	}
	if h := ed.BeginDrag2D(vx, vy); h != editor.Handle2DNavRegionVertex {
		t.Fatalf("BeginDrag2D = %v, want Handle2DNavRegionVertex", h)
	}

	tx, ty := ed.WorldToCanvas(180, 100)
	ed.UpdateDrag2D(tx, ty, false)

	// Live update during drag: overlay mesh and test path must already reflect the moved vertex!
	if ed.Built.NavMesh2D.Contains(goal) {
		t.Fatal("expected goal (260, 60) to become unwalkable immediately while dragging vertex 1 inward")
	}
	if p := ed.NavPathPreview2D(); p.Found {
		t.Fatalf("expected test path preview to update to unfound during drag, got %+v", p)
	}

	if !ed.EndDrag2D() {
		t.Fatal("EndDrag2D returned false, want true")
	}
	editedDraw := &core.Drawing{}
	ed.DrawViewport(editedDraw, core.Size{Width: 320, Height: 200})
	if bytes.Equal(origYAML, mustSaveYAML(t, ed)) {
		t.Fatal("expected YAML to change after vertex drag")
	}

	// ONE Undo must revert the vertex, YAML, built mesh overlay, and test path!
	if !ed.Undo() {
		t.Fatal("Undo failed")
	}
	if got := mustSaveYAML(t, ed); !bytes.Equal(origYAML, got) {
		t.Fatalf("YAML after one Undo does not match original:\n--- want ---\n%s\n--- got ---\n%s", origYAML, got)
	}
	if !ed.Built.NavMesh2D.Contains(goal) {
		t.Fatal("expected goal (260, 60) to be walkable again after one Undo")
	}
	if p := ed.NavPathPreview2D(); !p.Found || !navPointsEqual(p.Path, origPath, 1e-4) {
		t.Fatalf("test path after one Undo = %+v, want %v", p, origPath)
	}

	// Redo reapplies the vertex edit cleanly.
	if !ed.Redo() {
		t.Fatal("Redo failed")
	}
	if ed.Built.NavMesh2D.Contains(goal) {
		t.Fatal("expected goal (260, 60) to be unwalkable again after Redo")
	}
	_ = ed.Undo()

	// Edge midpoint drag inserts a new vertex and moves it in ONE undoable step.
	mx, my := ed.WorldToCanvas(160, 40) // midpoint of edge 0: (40,40)->(280,40)
	if idx := ed.PickNavRegionEdge2D(mx, my); idx != 0 {
		t.Fatalf("PickNavRegionEdge2D = %d, want 0", idx)
	}
	if h := ed.BeginDrag2D(mx, my); h != editor.Handle2DNavRegionEdgeMid {
		t.Fatalf("BeginDrag2D(edge mid) = %v, want Handle2DNavRegionEdgeMid", h)
	}
	dx, dy := ed.WorldToCanvas(160, 120)
	ed.UpdateDrag2D(dx, dy, false)
	if !ed.EndDrag2D() {
		t.Fatal("EndDrag2D(edge mid) returned false")
	}
	obj, _, _ := ed.FindObject(id)
	reg, ok, err := sceneformat.ExtractNavRegion2D(obj, nil)
	if err != nil || !ok || len(reg.Points) != 5 {
		t.Fatalf("after edge-mid drag points = %d (ok=%v err=%v), want 5", len(reg.Points), ok, err)
	}
	// Pulling the top edge midpoint down to (160, 120) creates a V-notch blocking straight line y=60!
	notchPath := ed.NavPathPreview2D()
	if !notchPath.Found || len(notchPath.Path) < 3 {
		t.Fatalf("expected V-notch polygon to bend path around (160, 120), got %+v", notchPath)
	}
	if !ed.Undo() {
		t.Fatal("Undo(edge mid) failed")
	}
	if got := mustSaveYAML(t, ed); !bytes.Equal(origYAML, got) {
		t.Fatalf("YAML after undoing edge-mid insert did not restore original bytes")
	}

	// Escape / CancelDrag2D puts the vertex back without adding a history step.
	if h := ed.BeginDrag2D(vx, vy); h != editor.Handle2DNavRegionVertex {
		t.Fatalf("BeginDrag2D(cancel) = %v", h)
	}
	ed.UpdateDrag2D(tx, ty, false)
	if !ed.CancelDrag2D() {
		t.Fatal("CancelDrag2D returned false")
	}
	if got := mustSaveYAML(t, ed); !bytes.Equal(origYAML, got) {
		t.Fatal("CancelDrag2D did not restore original YAML")
	}
	if !ed.Built.NavMesh2D.Contains(goal) {
		t.Fatal("CancelDrag2D did not rebuild NavMesh2D")
	}
}

func TestNavRegionHelpersInsertRemoveAndHitTest(t *testing.T) {
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetCanvasSize(640, 360)

	// PlaceNavRegion2D with nil points seeds a default 4-vertex square.
	id, err := ed.PlaceNavRegion2D("", nil)
	if err != nil {
		t.Fatalf("PlaceNavRegion2D: %v", err)
	}
	// Clicking inside the region away from the origin marker still selects it via hitTestNavRegion2D.
	ed.SelectObject("")
	cx, cy := ed.WorldToCanvas(40, 40)
	if hit := ed.HitTest(cx, cy); hit != id {
		t.Fatalf("HitTest inside navregion2d = %q, want %q", hit, id)
	}

	// Insert vertex, remove vertex, refuse dropping below 3 vertices, refuse non-finite points.
	if err := ed.InsertNavRegionVertex(id, 0, 0, -90); err != nil {
		t.Fatalf("InsertNavRegionVertex: %v", err)
	}
	if err := ed.RemoveNavRegionVertex(id, 1); err != nil {
		t.Fatalf("RemoveNavRegionVertex(1): %v", err)
	}
	if err := ed.RemoveNavRegionVertex(id, 0); err != nil {
		t.Fatalf("RemoveNavRegionVertex(0): %v", err)
	}
	if err := ed.RemoveNavRegionVertex(id, 0); err == nil {
		t.Fatal("expected RemoveNavRegionVertex below 3 vertices to be refused")
	}
	if err := ed.SetNavRegionPoints(id, [][2]float64{{0, 0}, {10, 0}}); err == nil {
		t.Fatal("expected SetNavRegionPoints with 2 points to be refused")
	}
	if err := ed.SetNavRegionPoints(id, [][2]float64{{0, 0}, {math.NaN(), 0}, {0, 10}}); err == nil {
		t.Fatal("expected SetNavRegionPoints with NaN to be refused")
	}

	// InitNavRegionPolygon resets to default 4-vertex polygon and is undoable.
	if err := ed.InitNavRegionPolygon(id); err != nil {
		t.Fatalf("InitNavRegionPolygon: %v", err)
	}
	obj, _, _ := ed.FindObject(id)
	reg, ok, err := sceneformat.ExtractNavRegion2D(obj, nil)
	if err != nil || !ok || len(reg.Points) != 4 {
		t.Fatalf("after InitNavRegionPolygon points=%d ok=%v err=%v, want 4", len(reg.Points), ok, err)
	}
}

func TestNavRegionUIWorkflowOverlayToggleAndTestPathTool(t *testing.T) {
	ed, err := editor.New(&sceneformat.SceneDoc{
		Version:       2,
		Name:          "Nav UI",
		VirtualWidth:  320,
		VirtualHeight: 200,
		Layers: []sceneformat.LayerDoc{{
			Name:  "world",
			Space: sceneformat.SpaceWorld,
		}},
	}, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	id, err := ed.PlaceNavRegion2D("world", [][2]float64{
		{20, 20}, {300, 20}, {300, 180}, {20, 180},
	})
	if err != nil {
		t.Fatalf("PlaceNavRegion2D: %v", err)
	}
	ed.SelectObject(id)

	app := mountEditor(t, ed)

	// Inspector shows navregion2d fields and Reset button.
	if !app.Exists("id:inspector-comp-navregion2d") {
		t.Fatal("expected inspector-comp-navregion2d to be mounted")
	}
	if !app.Exists("id:inspector-navregion-init-polygon") {
		t.Fatal("expected inspector-navregion-init-polygon to be mounted")
	}

	// Verify built navmesh overlay draws into the canvas when enabled and disappears when toggled off.
	opsOn := app.Find("id:editor-viewport").Ops
	if !ed.ShowNavMesh2D {
		t.Fatal("expected ShowNavMesh2D true by default")
	}
	app.Press("id:view-navmesh2d-overlay")
	if ed.ShowNavMesh2D {
		t.Fatal("expected ShowNavMesh2D false after pressing overlay toggle")
	}
	opsOff := app.Find("id:editor-viewport").Ops
	if len(opsOff) >= len(opsOn) {
		t.Fatalf("expected disabling 2D navmesh overlay to reduce canvas draw ops: on=%d off=%d", len(opsOn), len(opsOff))
	}
	app.Press("id:view-navmesh2d-overlay")
	if !ed.ShowNavMesh2D {
		t.Fatal("expected ShowNavMesh2D true after pressing overlay toggle again")
	}

	// Drag a vertex handle directly through EditorView and verify one Undo reverts it.
	ed.History.Clear()
	before := saveYAML(t, ed)
	vx, vy := ed.WorldToCanvas(300, 20)
	app.DragAt("id:editor-viewport", pt(vx, vy), pt(-40, 20))
	obj, _, _ := ed.FindObject(id)
	reg, ok, err := sceneformat.ExtractNavRegion2D(obj, nil)
	if err != nil || !ok || math.Abs(reg.Points[1][0]-260) > 1.5 {
		t.Fatalf("vertex 1 after UI drag = %+v (ok=%v err=%v), want X near 260", reg.Points, ok, err)
	}
	checkOneUndoStep(t, app, ed, before)

	// Activate Test Path tool via toolbar button, click start and end, then clear with Escape.
	app.Press("id:tool-nav-test-path")
	if ed.Tool != editor.ToolNavTestPath {
		t.Fatalf("Tool = %v, want ToolNavTestPath", ed.Tool)
	}
	app.Post(func() {
		sx, sy := ed.WorldToCanvas(50, 50)
		ex, ey := ed.WorldToCanvas(260, 140)
		ed.HandleCanvasPress(sx, sy)
		ed.HandleCanvasPress(ex, ey)
	})
	app.Settle()
	if p := ed.NavPathPreview2D(); !p.Found || len(p.Path) < 2 {
		t.Fatalf("expected Test Path preview to find path in UI, got %+v", p)
	}

	// Escape clears the test path preview.
	app.Key("escape")
	app.Settle()
	if p := ed.NavPathPreview2D(); p.HasStart || p.HasEnd || p.Found {
		t.Fatalf("expected Escape to clear test path preview, got %+v", p)
	}

	// Command palette commands work cleanly too.
	if err := ed.ExecuteCommand(editor.CmdToggleNavMesh2D); err != nil {
		t.Fatalf("ExecuteCommand(CmdToggleNavMesh2D): %v", err)
	}
	if ed.ShowNavMesh2D {
		t.Fatal("CmdToggleNavMesh2D did not toggle ShowNavMesh2D off")
	}
	if err := ed.ExecuteCommand(editor.CmdToolNavTestPath); err != nil {
		t.Fatalf("ExecuteCommand(CmdToolNavTestPath): %v", err)
	}
	if ed.Tool != editor.ToolNavTestPath {
		t.Fatalf("CmdToolNavTestPath Tool = %v, want ToolNavTestPath", ed.Tool)
	}
}

func mustSaveYAML(t *testing.T, ed *editor.Editor) []byte {
	t.Helper()
	b, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	return b
}

func TestNavRegionParallaxPlayModeNilAndNonFinite(t *testing.T) {
	// 1. Nil *Editor receiver guards.
	var nilEd *editor.Editor
	nilEd.SetShowNavMesh2D(false)
	nilEd.ClearNavPathPreview2D()
	if p := nilEd.NavPathPreview2D(); p.HasStart || p.HasEnd || p.Found {
		t.Fatalf("nil NavPathPreview2D = %+v, want zero", p)
	}
	if path, ok := nilEd.SetNavTestPathPoints(core.Point{}, core.Point{X: 10}); ok || path != nil {
		t.Fatalf("nil SetNavTestPathPoints = (%v, %v), want (nil, false)", path, ok)
	}
	if _, err := nilEd.PlaceNavRegion2D("world", nil); err == nil {
		t.Fatal("expected error from nil PlaceNavRegion2D")
	}
	if err := nilEd.SetNavRegionPoints("r1", [][2]float64{{0, 0}, {10, 0}, {10, 10}}); err == nil {
		t.Fatal("expected error from nil SetNavRegionPoints")
	}
	if err := nilEd.InsertNavRegionVertex("r1", 0, 5, 0); err == nil {
		t.Fatal("expected error from nil InsertNavRegionVertex")
	}
	if err := nilEd.RemoveNavRegionVertex("r1", 0); err == nil {
		t.Fatal("expected error from nil RemoveNavRegionVertex")
	}
	if idx := nilEd.PickNavRegionVertex2D(0, 0); idx != -1 {
		t.Fatalf("nil PickNavRegionVertex2D = %d, want -1", idx)
	}
	if idx := nilEd.PickNavRegionEdge2D(0, 0); idx != -1 {
		t.Fatalf("nil PickNavRegionEdge2D = %d, want -1", idx)
	}

	// 2. Parallax layer vertex pick & drag alignment (#1108).
	px, py := float32(0.5), float32(0.25)
	doc := &sceneformat.SceneDoc{
		Version:       3,
		Name:          "Parallax Nav",
		VirtualWidth:  320,
		VirtualHeight: 200,
		Layers: []sceneformat.LayerDoc{{
			Name:      "bg",
			Space:     sceneformat.SpaceWorld,
			ParallaxX: &px,
			ParallaxY: &py,
		}},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetCanvasSize(640, 400)
	// Pan the 2D view so parallax layer coordinates diverge from 1:1 world coordinates.
	ed.PanView2D(80, -40)
	id, err := ed.PlaceNavRegion2D("bg", [][2]float64{
		{0, 0}, {100, 0}, {100, 80}, {0, 80},
	})
	if err != nil {
		t.Fatalf("PlaceNavRegion2D on parallax layer: %v", err)
	}
	ed.SelectObject(id)

	// Convert local/layer vertex (100, 0) to canvas coordinates using the layer's parallax.
	v := ed.View2D()
	cw, ch := ed.CanvasSize()
	toCanvas := func(lx, ly float32) (float32, float32) {
		return cw/2 + (lx-v.X*px)*v.Zoom, ch/2 + (ly-v.Y*py)*v.Zoom
	}
	cx0, cy0 := toCanvas(100, 0)
	if got := ed.PickNavRegionVertex2D(cx0, cy0); got != 1 {
		t.Fatalf("PickNavRegionVertex2D on parallax layer = %d, want 1", got)
	}
	if h := ed.BeginDrag2D(cx0, cy0); h != editor.Handle2DNavRegionVertex {
		t.Fatalf("BeginDrag2D on parallax vertex = %v, want Handle2DNavRegionVertex", h)
	}
	cx1, cy1 := toCanvas(140, 20)
	ed.UpdateDrag2D(cx1, cy1, false)
	if !ed.EndDrag2D() {
		t.Fatal("EndDrag2D on parallax vertex returned false")
	}
	obj, _, _ := ed.FindObject(id)
	reg, ok, err := sceneformat.ExtractNavRegion2D(obj, nil)
	if err != nil || !ok {
		t.Fatalf("ExtractNavRegion2D after parallax drag: ok=%v err=%v", ok, err)
	}
	if math.Abs(reg.Points[1][0]-140) > 0.5 || math.Abs(reg.Points[1][1]-20) > 0.5 {
		t.Fatalf("parallax dragged vertex = %v, want (140, 20)", reg.Points[1])
	}

	// 3. Non-finite guards in SetNavTestPathPoints & HandleCanvasPress(ToolNavTestPath).
	nan := core.Points(math.NaN())
	inf := core.Points(math.Inf(1))
	if path, found := ed.SetNavTestPathPoints(core.Point{X: nan, Y: 0}, core.Point{X: 10, Y: 10}); found || path != nil {
		t.Fatalf("SetNavTestPathPoints(NaN) = (%v, %v), want (nil, false)", path, found)
	}
	if path, found := ed.SetNavTestPathPoints(core.Point{X: 0, Y: 0}, core.Point{X: inf, Y: 10}); found || path != nil {
		t.Fatalf("SetNavTestPathPoints(+Inf) = (%v, %v), want (nil, false)", path, found)
	}
	ed.SetTool(editor.ToolNavTestPath)
	ed.HandleCanvasPress(float32(math.NaN()), 10)
	ed.HandleCanvasPress(10, float32(math.Inf(-1)))
	if p := ed.NavPathPreview2D(); p.HasStart || p.HasEnd {
		t.Fatalf("non-finite HandleCanvasPress mutated preview: %+v", p)
	}

	// 4. Play-mode refusal on all navregion2d mutators and Test Path presses.
	app := mountEditor(t, ed)
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
	})
	app.Settle()
	if !ed.Playing() {
		t.Fatal("expected editor to enter Play mode")
	}
	if _, err := ed.PlaceNavRegion2D("bg", nil); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("PlaceNavRegion2D while playing = %v, want ErrPlaying", err)
	}
	if err := ed.SetNavRegionPoints(id, [][2]float64{{0, 0}, {20, 0}, {20, 20}}); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("SetNavRegionPoints while playing = %v, want ErrPlaying", err)
	}
	if err := ed.InsertNavRegionVertex(id, 0, 10, 0); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("InsertNavRegionVertex while playing = %v, want ErrPlaying", err)
	}
	if err := ed.RemoveNavRegionVertex(id, 0); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("RemoveNavRegionVertex while playing = %v, want ErrPlaying", err)
	}
	ed.HandleCanvasPress(cx0, cy0)
	if p := ed.NavPathPreview2D(); p.HasStart || p.HasEnd {
		t.Fatalf("HandleCanvasPress(ToolNavTestPath) while playing mutated preview: %+v", p)
	}
	app.Post(func() {
		ed.Stop()
	})
	app.Settle()
}
