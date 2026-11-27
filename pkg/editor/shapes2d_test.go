package editor_test

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func colliderShapeRegistry(t *testing.T) *sceneformat.ComponentRegistry {
	t.Helper()
	reg := sceneformat.NewComponentRegistry()
	if err := reg.Register(sceneformat.ComponentType{
		Name: "collider",
		Doc:  "2D world shape collider.",
		Fields: []sceneformat.Field{
			{Name: "box", Kind: sceneformat.FieldRect, Default: sceneformat.Rect{X: -12, Y: -10, Width: 24, Height: 20}, Doc: "Bounding box."},
			{Name: "anchor", Kind: sceneformat.FieldPoint, Default: sceneformat.Point{X: 20, Y: 0}, Doc: "Local anchor."},
			{Name: "range", Kind: sceneformat.FieldCircle, Default: sceneformat.Circle{X: 0, Y: 0, Radius: 16}, Doc: "Detection circle."},
			{Name: "poly", Kind: sceneformat.FieldPolygon, Default: []sceneformat.Point{{X: 0, Y: -20}, {X: 20, Y: 0}, {X: 0, Y: 20}}, Doc: "Polygon hull."},
		},
	}); err != nil {
		t.Fatalf("Register collider: %v", err)
	}
	if err := reg.Register(sceneformat.ComponentType{
		Name: "trigger",
		Doc:  "2D trigger area.",
		Fields: []sceneformat.Field{
			{Name: "area", Kind: sceneformat.FieldRect, Default: sceneformat.Rect{X: -10, Y: -10, Width: 20, Height: 20}},
		},
	}); err != nil {
		t.Fatalf("Register trigger: %v", err)
	}
	if err := reg.Register(sceneformat.ComponentType{
		Name: "navregion",
		Doc:  "2D navigation polygon region.",
		Fields: []sceneformat.Field{
			{Name: "poly", Kind: sceneformat.FieldPolygon, Default: []sceneformat.Point{{X: 0, Y: -20}, {X: 20, Y: 0}, {X: 0, Y: 20}}},
		},
	}); err != nil {
		t.Fatalf("Register navregion: %v", err)
	}
	return reg
}

func newShapeEditor(t *testing.T, objs ...sceneformat.ObjectDoc) (*editor.Editor, *sceneformat.ComponentRegistry) {
	t.Helper()
	reg := colliderShapeRegistry(t)
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Shapes",
		VirtualWidth:    320,
		VirtualHeight:   180,
		BackgroundColor: "#141824",
		Layers: []sceneformat.LayerDoc{{
			Name:    "actors",
			Space:   sceneformat.SpaceWorld,
			Objects: objs,
		}},
	}
	ed, err := editor.New(doc, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.SetComponentRegistry(reg); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	return ed, reg
}

func findHandle(t *testing.T, handles []editor.ShapeHandle2D, comp, field string, kind editor.ShapeHandleKind, idx int) editor.ShapeHandle2D {
	t.Helper()
	for _, h := range handles {
		if h.Component == comp && h.Field == field && h.Kind == kind && h.Index == idx {
			return h
		}
	}
	t.Fatalf("no handle (%s.%s %v idx=%d) in %+v", comp, field, kind, idx, handles)
	return editor.ShapeHandle2D{}
}

// Acceptance #1: A registered collider component with a rect field is drawn and resized by dragging in the viewport, and the saved field round-trips.
func TestColliderRectDrawnResizedAndRoundTrips(t *testing.T) {
	ed, reg := newShapeEditor(t, sceneformat.ObjectDoc{
		ID: "hero",
		X:  80,
		Y:  60,
		Components: map[string]any{
			"collider": map[string]any{
				"box": sceneformat.Rect{X: -10, Y: -10, Width: 20, Height: 20},
			},
		},
	})
	ed.SelectObject("hero")
	app := mountEditor(t, ed)
	ed.SetView2D(editor.View2D{X: 80, Y: 60, Zoom: 2})
	app.Settle()

	shapes := ed.VisibleShapes2D()
	foundBox := false
	for _, s := range shapes {
		if s.ObjectID == "hero" && s.Component == "collider" && s.Field == "box" && s.Kind == sceneformat.FieldRect && s.Selected && len(s.Points) == 4 {
			foundBox = true
		}
	}
	if !foundBox {
		t.Fatalf("collider.box not drawn in VisibleShapes2D: %+v", shapes)
	}

	before := saveYAML(t, ed)
	east := findHandle(t, ed.ShapeHandles2D(), "collider", "box", editor.ShapeHandleRectE, 0)
	// Zoom is 2, so dragging right by +20 canvas pts expands width by +10 world/local units.
	app.DragAt("id:editor-viewport", pt(east.Pos.X, east.Pos.Y), pt(20, 0))

	if ed.History.Len() != 1 {
		t.Fatalf("History.Len() = %d, want 1", ed.History.Len())
	}
	raw, ok := ed.ComponentField("hero", "collider", "box")
	if !ok {
		t.Fatal("collider.box missing after drag")
	}
	gotRect, err := sceneformat.ParseRect(raw)
	if err != nil {
		t.Fatalf("ParseRect: %v", err)
	}
	wantRect := sceneformat.Rect{X: -10, Y: -10, Width: 30, Height: 20}
	if !near(float32(gotRect.X), float32(wantRect.X), 1e-3) ||
		!near(float32(gotRect.Y), float32(wantRect.Y), 1e-3) ||
		!near(float32(gotRect.Width), float32(wantRect.Width), 1e-3) ||
		!near(float32(gotRect.Height), float32(wantRect.Height), 1e-3) {
		t.Fatalf("rect after east edge drag = %+v, want %+v", gotRect, wantRect)
	}

	// Verify deterministic YAML round-trip with the component registry.
	saved := saveYAML(t, ed)
	reparsed, err := sceneformat.ParseWith(saved, sceneformat.ParseOptions{File: "shapes.scene.yaml", Components: reg})
	if err != nil {
		t.Fatalf("ParseWith saved YAML: %v\n%s", err, saved)
	}
	reencoded, err := sceneformat.Encode(reparsed)
	if err != nil {
		t.Fatalf("Encode reparsed: %v", err)
	}
	if !bytes.Equal(saved, reencoded) {
		t.Fatalf("saved rect did not round-trip unchanged:\n--- saved ---\n%s\n--- reencoded ---\n%s", saved, reencoded)
	}

	// Undo restores the pre-drag YAML byte for byte; Redo reapplies.
	undoInUI(app, ed)
	if got := saveYAML(t, ed); !bytes.Equal(got, before) {
		t.Fatalf("Undo did not restore pre-drag YAML:\n%s\nwant:\n%s", got, before)
	}
	redoInUI(app, ed)
	if got := saveYAML(t, ed); !bytes.Equal(got, saved) {
		t.Fatalf("Redo did not restore resized YAML:\n%s\nwant:\n%s", got, saved)
	}
}

// Acceptance #2: Adding and dragging a polygon vertex is one undo step each.
func TestPolygonAddDragAndRemoveVertexUndoSteps(t *testing.T) {
	ed, _ := newShapeEditor(t, sceneformat.ObjectDoc{
		ID: "zone",
		X:  100,
		Y:  80,
		Components: map[string]any{
			"navregion": map[string]any{
				"poly": []sceneformat.Point{
					{X: 0, Y: -20},
					{X: 20, Y: 0},
					{X: 0, Y: 20},
				},
			},
		},
	})
	ed.SelectObject("zone")
	app := mountEditor(t, ed)
	ed.SetView2D(editor.View2D{X: 100, Y: 80, Zoom: 2})
	app.Settle()

	beforeAdd := saveYAML(t, ed)

	// Step 1: Click edge 0 midpoint handle ((0,-20) -> (20,0), midpoint = (10,-10)) to insert a vertex.
	mid0 := findHandle(t, ed.ShapeHandles2D(), "navregion", "poly", editor.ShapeHandlePolygonMidpoint, 0)
	app.Post(func() {
		ed.HandleCanvasPress(mid0.Pos.X, mid0.Pos.Y)
	})
	app.Settle()

	if ed.History.Len() != 1 {
		t.Fatalf("History.Len() after adding vertex = %d, want 1", ed.History.Len())
	}
	raw, _ := ed.ComponentField("zone", "navregion", "poly")
	pts, err := sceneformat.ParsePolygon(raw)
	if err != nil {
		t.Fatalf("ParsePolygon: %v", err)
	}
	if len(pts) != 4 || !near(float32(pts[1].X), 10, 1e-3) || !near(float32(pts[1].Y), -10, 1e-3) {
		t.Fatalf("polygon after add = %+v, want 4 vertices with [1]=(10,-10)", pts)
	}
	afterAdd := saveYAML(t, ed)

	// Step 2: Drag the newly inserted vertex (index 1) by (+16, -8) canvas pts -> (+8, -4) local units.
	v1 := findHandle(t, ed.ShapeHandles2D(), "navregion", "poly", editor.ShapeHandlePolygonVertex, 1)
	app.DragAt("id:editor-viewport", pt(v1.Pos.X, v1.Pos.Y), pt(16, -8))

	if ed.History.Len() != 2 {
		t.Fatalf("History.Len() after add + drag = %d, want 2", ed.History.Len())
	}
	raw, _ = ed.ComponentField("zone", "navregion", "poly")
	pts, _ = sceneformat.ParsePolygon(raw)
	if len(pts) != 4 || !near(float32(pts[1].X), 18, 1e-3) || !near(float32(pts[1].Y), -14, 1e-3) {
		t.Fatalf("polygon after drag = %+v, want [1]=(18,-14)", pts)
	}

	// Undo once reverts only the drag.
	undoInUI(app, ed)
	if got := saveYAML(t, ed); !bytes.Equal(got, afterAdd) {
		t.Fatalf("first Undo did not revert drag back to afterAdd")
	}
	// Undo twice reverts the vertex addition completely.
	undoInUI(app, ed)
	if got := saveYAML(t, ed); !bytes.Equal(got, beforeAdd) {
		t.Fatalf("second Undo did not restore original scene")
	}

	// Redo twice, then remove vertex 1 via secondary click (right-click) in one undo step.
	redoInUI(app, ed)
	redoInUI(app, ed)
	v1 = findHandle(t, ed.ShapeHandles2D(), "navregion", "poly", editor.ShapeHandlePolygonVertex, 1)
	app.Post(func() {
		if !ed.HandleCanvasSecondaryPress(v1.Pos.X, v1.Pos.Y) {
			t.Error("HandleCanvasSecondaryPress did not remove polygon vertex")
		}
	})
	app.Settle()

	if ed.History.Len() != 3 {
		t.Fatalf("History.Len() after remove = %d, want 3", ed.History.Len())
	}
	raw, _ = ed.ComponentField("zone", "navregion", "poly")
	pts, _ = sceneformat.ParsePolygon(raw)
	if len(pts) != 3 {
		t.Fatalf("polygon after remove has %d vertices, want 3", len(pts))
	}
	// Removing below 3 vertices is refused.
	if err := ed.RemovePolygonVertex("zone", "navregion", "poly", 0); err == nil {
		t.Fatal("RemovePolygonVertex allowed removing below 3 vertices")
	}
}

func TestShowAllShapesToggleAndGridSnapping(t *testing.T) {
	ed, _ := newShapeEditor(t,
		sceneformat.ObjectDoc{
			ID: "a",
			X:  40,
			Y:  40,
			Components: map[string]any{
				"trigger": map[string]any{
					"area": sceneformat.Rect{X: -10, Y: -10, Width: 20, Height: 20},
				},
			},
		},
		sceneformat.ObjectDoc{
			ID: "b",
			X:  160,
			Y:  60,
			Components: map[string]any{
				"collider": map[string]any{
					"anchor": sceneformat.Point{X: 20, Y: 10},
					"range":  sceneformat.Circle{X: 0, Y: 0, Radius: 16},
				},
			},
		},
	)
	ed.SelectObject("a")
	app := mountEditor(t, ed)
	ed.SetView2D(editor.View2D{X: 100, Y: 60, Zoom: 1})
	app.Settle()

	// By default only selected object "a" shapes draw.
	for _, s := range ed.VisibleShapes2D() {
		if s.ObjectID != "a" {
			t.Fatalf("unexpected non-selected shape drawn while ShowAllShapes is off: %+v", s)
		}
	}

	// Toolbar toggle turns ShowAllShapes on and draws both "a" and "b".
	app.Post(func() { ed.SetShowAllShapes(true) })
	app.Settle()
	if !ed.ShowAllShapes {
		t.Fatal("toolbar Shapes toggle did not enable ShowAllShapes")
	}
	seen := map[string]bool{}
	for _, s := range ed.VisibleShapes2D() {
		seen[s.ObjectID] = true
	}
	if !seen["a"] || !seen["b"] {
		t.Fatalf("ShowAllShapes did not draw both objects: seen=%v", seen)
	}

	// Hidden objects are skipped even with ShowAllShapes on.
	app.Post(func() { ed.SetObjectHidden("b", true) })
	app.Settle()
	for _, s := range ed.VisibleShapes2D() {
		if s.ObjectID == "b" {
			t.Fatal("hidden object shape was drawn")
		}
	}
	app.Post(func() {
		ed.SetObjectHidden("b", false)
		ed.SelectObject("b")
		ed.SetGridSize(10)
	})
	app.Settle()

	// Turn Snap on via toolbar and drag point & circle radius handles.
	app.Press("id:viewport-bar-snap")
	if !ed.SnapToGrid {
		t.Fatal("toolbar Snap toggle did not enable SnapToGrid")
	}

	ptHandle := findHandle(t, ed.ShapeHandles2D(), "collider", "anchor", editor.ShapeHandlePoint, 0)
	// Drag by (+13, +7) from (20, 10) -> (33, 17) snaps to (30, 20) on a 10-unit grid.
	app.DragAt("id:editor-viewport", pt(ptHandle.Pos.X, ptHandle.Pos.Y), pt(13, 7))
	rawPt, _ := ed.ComponentField("b", "collider", "anchor")
	gotPt, _ := sceneformat.ParsePoint(rawPt)
	if gotPt != (sceneformat.Point{X: 30, Y: 20}) {
		t.Fatalf("snapped point = %+v, want {30, 20}", gotPt)
	}

	// Drag circle +X radius handle by (+12, 0) from r=16 -> 28 snaps to 30.
	radHandle := findHandle(t, ed.ShapeHandles2D(), "collider", "range", editor.ShapeHandleCircleRadius, 0)
	app.DragAt("id:editor-viewport", pt(radHandle.Pos.X, radHandle.Pos.Y), pt(12, 0))
	rawCirc, _ := ed.ComponentField("b", "collider", "range")
	gotCirc, _ := sceneformat.ParseCircle(rawCirc)
	if gotCirc != (sceneformat.Circle{X: 0, Y: 0, Radius: 30}) {
		t.Fatalf("snapped circle = %+v, want {0, 0, 30}", gotCirc)
	}

	// Escape cancels an active shape drag without recording an undo step.
	histBefore := ed.History.Len()
	radHandle = findHandle(t, ed.ShapeHandles2D(), "collider", "range", editor.ShapeHandleCircleRadius, 0)
	app.Post(func() {
		if _, ok := ed.BeginShapeDrag2D(radHandle.Pos.X, radHandle.Pos.Y); !ok {
			t.Error("BeginShapeDrag2D failed")
		}
		ed.UpdateShapeDrag2D(radHandle.Pos.X+50, radHandle.Pos.Y)
	})
	app.Settle()
	if !ed.DraggingShape2D() {
		t.Fatal("DraggingShape2D() = false during drag")
	}
	app.Key("escape")
	if ed.DraggingShape2D() {
		t.Fatal("Escape did not cancel shape drag")
	}
	if ed.History.Len() != histBefore {
		t.Fatalf("cancelled shape drag modified history: %d != %d", ed.History.Len(), histBefore)
	}
	rawCirc, _ = ed.ComponentField("b", "collider", "range")
	gotCirc, _ = sceneformat.ParseCircle(rawCirc)
	if gotCirc.Radius != 30 {
		t.Fatalf("cancelled shape drag did not restore radius: got %v, want 30", gotCirc.Radius)
	}
}

func TestShapeDragUnderRotatedObject(t *testing.T) {
	ed, _ := newShapeEditor(t, sceneformat.ObjectDoc{
		ID:       "tilted",
		X:        100,
		Y:        100,
		Rotation: float32(math.Pi / 2), // 90 degrees clockwise: local +X maps to world +Y
		Components: map[string]any{
			"collider": map[string]any{
				"box": sceneformat.Rect{X: -10, Y: -10, Width: 20, Height: 20},
			},
		},
	})
	ed.SelectObject("tilted")
	app := mountEditor(t, ed)
	ed.SetView2D(editor.View2D{X: 100, Y: 100, Zoom: 1})
	app.Settle()

	east := findHandle(t, ed.ShapeHandles2D(), "collider", "box", editor.ShapeHandleRectE, 0)
	// Under 90deg clockwise rotation, local +X points downward (+Y on canvas).
	app.DragAt("id:editor-viewport", pt(east.Pos.X, east.Pos.Y), pt(0, 15))
	raw, _ := ed.ComponentField("tilted", "collider", "box")
	got, _ := sceneformat.ParseRect(raw)
	if !near(float32(got.Width), 35, 1e-2) || !near(float32(got.Height), 20, 1e-2) {
		t.Fatalf("rotated rect after dragging along local +X = %+v, want width~35 height~20", got)
	}
}

// Acceptance #3: A plugin tool receives drag begin, move and end with canvas coordinates (and hover/cancel).
func TestPluginToolReceivesDragAndHoverCallbacksWithCanvasCoords(t *testing.T) {
	type event struct {
		kind   string
		x, y   float32
		active bool
	}
	var events []event

	dir := writeEditorProject(t)
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(projectYAML+"editor:\n  plugins: [handles]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.RegisterPlugin(editor.Plugin{
		Name: "handles",
		Tools: []editor.PluginTool{{
			ID:    "custom",
			Label: "Custom Handles",
			OnDragBegin: func(_ *editor.Editor, x, y float32) bool {
				events = append(events, event{kind: "begin", x: x, y: y})
				return true
			},
			OnDragMove: func(_ *editor.Editor, x, y float32) {
				events = append(events, event{kind: "move", x: x, y: y})
			},
			OnDragEnd: func(_ *editor.Editor, x, y float32) {
				events = append(events, event{kind: "end", x: x, y: y})
			},
			OnDragCancel: func(_ *editor.Editor) {
				events = append(events, event{kind: "cancel"})
			},
			OnHover: func(_ *editor.Editor, x, y float32, active bool) {
				events = append(events, event{kind: "hover", x: x, y: y, active: active})
			},
		}},
	}); err != nil {
		t.Fatalf("RegisterPlugin drag-only tool: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	app := mountEditor(t, ed)
	app.Post(func() {
		if err := ed.SetPluginTool("handles", "custom"); err != nil {
			t.Error(err)
		}
	})
	app.Settle()

	// Hover enters and leaves the viewport.
	app.Post(func() {
		ed.SetPluginHover2D(42, 64)
		ed.ClearPluginHover2D()
	})
	app.Settle()

	// Drag through the mounted viewport canvas.
	app.DragAt("id:editor-viewport", pt(120, 90), pt(15, 5), pt(45, 25))

	if len(events) < 5 {
		t.Fatalf("expected hover + begin + move(s) + end, got %+v", events)
	}
	wantPrefix := []event{
		{kind: "hover", x: 42, y: 64, active: true},
		{kind: "hover", x: 42, y: 64, active: false},
		{kind: "begin", x: 120, y: 90},
	}
	if !reflect.DeepEqual(events[:3], wantPrefix) {
		t.Fatalf("prefix events = %+v, want %+v", events[:3], wantPrefix)
	}
	moves := events[3 : len(events)-1]
	if len(moves) == 0 {
		t.Fatalf("expected OnDragMove events, got %+v", events)
	}
	for i, mv := range moves {
		if mv.kind != "move" {
			t.Fatalf("events[%d] = %+v, want move", i+3, mv)
		}
	}
	if lastMove := moves[len(moves)-1]; !near(lastMove.x, 165, 1e-3) || !near(lastMove.y, 115, 1e-3) {
		t.Fatalf("last move = %+v, want (165, 115)", lastMove)
	}
	wantEnd := event{kind: "end", x: 165, y: 115}
	if gotEnd := events[len(events)-1]; gotEnd != wantEnd {
		t.Fatalf("end event = %+v, want %+v", gotEnd, wantEnd)
	}

	// Escape cancels an active plugin tool drag.
	events = nil
	app.Post(func() {
		if !ed.BeginPluginDrag2D(50, 50) {
			t.Error("BeginPluginDrag2D failed")
		}
		ed.UpdatePluginDrag2D(70, 60)
	})
	app.Settle()
	app.Key("escape")

	wantCancel := []event{
		{kind: "begin", x: 50, y: 50},
		{kind: "move", x: 70, y: 60},
		{kind: "cancel"},
	}
	if !reflect.DeepEqual(events, wantCancel) {
		t.Fatalf("plugin cancel events:\n got: %+v\nwant: %+v", events, wantCancel)
	}
}

func TestInspectorDisplaysAndEditsShapeFields(t *testing.T) {
	ed, _ := newShapeEditor(t, sceneformat.ObjectDoc{
		ID: "hero",
		X:  40,
		Y:  40,
		Components: map[string]any{
			"collider": map[string]any{
				"box": sceneformat.Rect{X: -8, Y: -8, Width: 16, Height: 16},
			},
		},
	})
	ed.SelectObject("hero")
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 1400))

	if got := fieldText(app.Find("id:inspector-comp-collider-box-width")); got != "16" {
		t.Fatalf("box width field = %q, want 16", got)
	}
	if got := app.Find("id:inspector-comp-collider-poly").Text; got != "3 vertices" {
		t.Fatalf("poly summary = %q, want 3 vertices", got)
	}
	app.Press("id:inspector-comp-collider-poly-add")
	if got := app.Find("id:inspector-comp-collider-poly").Text; got != "4 vertices" {
		t.Fatalf("poly summary after + Vertex = %q, want 4 vertices", got)
	}
}
