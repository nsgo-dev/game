package editor_test

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func newMultiSelectEditor(t *testing.T) *editor.Editor {
	t.Helper()
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Multi Select Room",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#141824",
		Camera:          &sceneformat.CameraDoc{X: 140, Y: 120},
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Order: 0,
				Objects: []sceneformat.ObjectDoc{
					{ID: "p1", Name: "Platform 1", X: 60, Y: 120, Properties: map[string]any{"tag": "floor", "weight": 1}},
					{ID: "p2", Name: "Platform 2", X: 100, Y: 120, Properties: map[string]any{"tag": "floor", "weight": 2}},
					{ID: "p3", Name: "Platform 3", X: 140, Y: 120, Properties: map[string]any{"tag": "floor", "weight": 3}},
					{ID: "p4", Name: "Platform 4", X: 180, Y: 120, Properties: map[string]any{"tag": "floor", "weight": 4}},
					{ID: "p5", Name: "Platform 5", X: 220, Y: 120, Properties: map[string]any{"tag": "floor", "weight": 5}},
					{ID: "outside", Name: "Far Away", X: 520, Y: 300},
				},
			},
		},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	return ed
}

// Acceptance #1: Marquee-selecting five objects and dragging them moves all five by the same offset in one undo step.
func TestMarqueeSelectFiveObjectsAndDragMovesAllInOneUndoStep(t *testing.T) {
	ed := newMultiSelectEditor(t)
	app := mountEditor(t, ed)

	// Drag a marquee on empty canvas enclosing p1..p5 (markers are 16x16 centered at X=60..220, Y=120).
	x0, y0 := ed.WorldToCanvas(30, 90)
	x1, y1 := ed.WorldToCanvas(250, 150)
	app.DragAt("id:editor-viewport", pt(x0, y0), pt(x1-x0, y1-y0))

	wantIDs := []string{"p1", "p2", "p3", "p4", "p5"}
	if got := ed.SelectedIDs(); !slices.Equal(got, wantIDs) {
		t.Fatalf("marquee SelectedIDs = %v, want %v", got, wantIDs)
	}
	if ed.SelectedID != "p1" {
		t.Fatalf("primary SelectedID = %q, want p1", ed.SelectedID)
	}
	if got := app.Find("id:inspector-selection-count").Text; got != "5 selected" {
		t.Fatalf("inspector header = %q, want %q", got, "5 selected")
	}

	before := saveYAML(t, ed)
	startX := map[string]float32{"p1": 60, "p2": 100, "p3": 140, "p4": 180, "p5": 220}

	// Drag one of the selected objects (p3 at 140, 120) by (+35, -20).
	gx, gy := ed.WorldToCanvas(140, 120)
	app.DragAt("id:editor-viewport", pt(gx, gy), pt(35, -20))

	if ed.History.Len() != 1 {
		t.Fatalf("History.Len() after dragging 5 objects = %d, want 1", ed.History.Len())
	}
	for _, id := range wantIDs {
		obj, _, _ := ed.FindObject(id)
		if !near(obj.X, startX[id]+35, 1e-3) || !near(obj.Y, 100, 1e-3) {
			t.Fatalf("%s moved to (%v, %v), want (%v, 100)", id, obj.X, obj.Y, startX[id]+35)
		}
	}
	if out, _, _ := ed.FindObject("outside"); out.X != 520 || out.Y != 300 {
		t.Fatalf("unselected outside moved to (%v, %v)", out.X, out.Y)
	}

	after := saveYAML(t, ed)
	undoInUI(app, ed)
	if got := saveYAML(t, ed); !bytes.Equal(got, before) {
		t.Fatalf("one Undo did not restore all five objects:\n%s\nwant:\n%s", got, before)
	}
	redoInUI(app, ed)
	if got := saveYAML(t, ed); !bytes.Equal(got, after) {
		t.Fatalf("one Redo did not re-apply move of all five objects")
	}
}

// Acceptance #2: Setting a shared component field on three selected objects with different values
// writes all three, and one Undo restores each one's own value.
func TestMultiSelectInspectorSetsSharedComponentFieldAndOneUndoRestoresEach(t *testing.T) {
	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{
		Name: "enemy",
		Doc:  "Enemy movement settings.",
		Fields: []sceneformat.Field{
			{Name: "speed", Kind: sceneformat.FieldFloat, Default: 40.0, Min: sceneformat.Float64(0), Max: sceneformat.Float64(500)},
			{Name: "active", Kind: sceneformat.FieldBool, Default: true},
			{Name: "ai", Kind: sceneformat.FieldString, Default: "patrol", Enum: []string{"patrol", "chase", "idle"}},
		},
	})
	reg.MustRegister(sceneformat.ComponentType{
		Name: "boss",
		Fields: []sceneformat.Field{
			{Name: "phase", Kind: sceneformat.FieldInt, Default: 1},
		},
	})

	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Enemies",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{
						ID: "e1", X: 60, Y: 80,
						Components: map[string]any{
							"enemy": map[string]any{"speed": 10.0, "active": true, "ai": "patrol"},
							"boss":  map[string]any{"phase": 2},
						},
					},
					{
						ID: "e2", X: 120, Y: 80,
						Components: map[string]any{
							"enemy": map[string]any{"speed": 25.0, "active": false, "ai": "chase"},
						},
					},
					{
						ID: "e3", X: 180, Y: 80,
						Components: map[string]any{
							"enemy": map[string]any{"speed": 42.5, "active": true, "ai": "idle"},
						},
					},
				},
			},
		},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.SetComponentRegistry(reg); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	ed.SelectObjectsWithPrimary([]string{"e1", "e2", "e3"}, "e1")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 1200))

	if got := app.Find("id:inspector-selection-count").Text; got != "3 selected" {
		t.Fatalf("selection count = %q, want %q", got, "3 selected")
	}
	if !strings.Contains(app.Find("id:inspector-multi-note").Text, "enemy") {
		t.Fatalf("multi note = %q, want mention of shared component enemy", app.Find("id:inspector-multi-note").Text)
	}
	// Unshared component "boss" must not appear in multi-selection inspector.
	if app.Exists("id:inspector-comp-boss") {
		t.Fatal("unshared component boss appeared in multi-select inspector")
	}
	// Shared component "enemy" appears, and speed is mixed (empty value text).
	if got := app.Find("id:inspector-comp-enemy-speed").Text; got != "" {
		t.Fatalf("mixed speed field text = %q, want empty (showing Mixed placeholder)", got)
	}

	before := saveYAML(t, ed)

	// Set shared component field enemy.speed to 80 via the Inspector UI.
	app.Type("id:inspector-comp-enemy-speed", "80\n")

	if ed.History.Len() != 1 {
		t.Fatalf("History.Len() after setting shared component field = %d, want 1", ed.History.Len())
	}
	for _, id := range []string{"e1", "e2", "e3"} {
		obj, _, _ := ed.FindObject(id)
		got := obj.Components["enemy"].(map[string]any)["speed"]
		if got != 80.0 {
			t.Fatalf("%s enemy.speed = %#v, want 80", id, got)
		}
	}
	if got := app.Find("id:inspector-comp-enemy-speed").Text; got != "80" {
		t.Fatalf("uniform speed field text = %q, want 80", got)
	}

	// One Undo restores each object's own original value (10, 25, 42.5).
	undoInUI(app, ed)
	wantSpeed := map[string]float64{"e1": 10.0, "e2": 25.0, "e3": 42.5}
	for id, want := range wantSpeed {
		obj, _, _ := ed.FindObject(id)
		got := obj.Components["enemy"].(map[string]any)["speed"]
		if got != want {
			t.Fatalf("after one Undo, %s enemy.speed = %#v, want %v", id, got, want)
		}
	}
	if got := saveYAML(t, ed); !bytes.Equal(got, before) {
		t.Fatalf("one Undo did not restore scene byte-for-byte:\n%s\nwant:\n%s", got, before)
	}

	// Redo writes all three back to 80.
	redoInUI(app, ed)
	for _, id := range []string{"e1", "e2", "e3"} {
		obj, _, _ := ed.FindObject(id)
		if got := obj.Components["enemy"].(map[string]any)["speed"]; got != 80.0 {
			t.Fatalf("after Redo, %s enemy.speed = %#v, want 80", id, got)
		}
	}
}

func TestModifierSelectionMarqueeTouchAndEscape(t *testing.T) {
	ed := newMultiSelectEditor(t)
	app := mountEditor(t, ed)

	// Plain click selects p1.
	p1x, p1y := ed.WorldToCanvas(60, 120)
	app.PressAt("id:editor-viewport", pt(p1x, p1y))
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"p1"}) {
		t.Fatalf("plain click SelectedIDs = %v, want [p1]", got)
	}

	// Shift+click adds p2 and p3.
	p2x, p2y := ed.WorldToCanvas(100, 120)
	p3x, p3y := ed.WorldToCanvas(140, 120)
	app.PressAtWith("id:editor-viewport", pt(p2x, p2y), core.Modifiers{Shift: true})
	app.PressAtWith("id:editor-viewport", pt(p3x, p3y), core.Modifiers{Shift: true})
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"p1", "p2", "p3"}) || ed.SelectedID != "p3" {
		t.Fatalf("after Shift+clicks: SelectedIDs=%v SelectedID=%q", got, ed.SelectedID)
	}

	// Mod+click toggles p2 off, then p4 on.
	app.PressAtWith("id:editor-viewport", pt(p2x, p2y), core.Modifiers{Control: true})
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"p1", "p3"}) {
		t.Fatalf("after Mod+click toggle off p2: SelectedIDs=%v", got)
	}
	p4x, p4y := ed.WorldToCanvas(180, 120)
	app.PressAtWith("id:editor-viewport", pt(p4x, p4y), core.Modifiers{Platform: true})
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"p1", "p3", "p4"}) || ed.SelectedID != "p4" {
		t.Fatalf("after Mod+click toggle on p4: SelectedIDs=%v SelectedID=%q", got, ed.SelectedID)
	}

	// Escape clears selection at rest.
	app.Key("escape")
	if ed.SelectionCount() != 0 || ed.SelectedID != "" {
		t.Fatalf("after Escape: SelectedIDs=%v SelectedID=%q, want empty", ed.SelectedIDs(), ed.SelectedID)
	}

	// Marquee enclose vs touch (Alt): p1's marker spans [52..68] x [112..128], p2 spans [92..108] x [112..128].
	// A box from X=40 to X=96 encloses p1 completely and only touches p2's left edge.
	ax, ay := ed.WorldToCanvas(40, 100)
	bx, by := ed.WorldToCanvas(96, 140)
	app.DragAt("id:editor-viewport", pt(ax, ay), pt(bx-ax, by-ay))
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"p1"}) {
		t.Fatalf("enclose marquee SelectedIDs = %v, want [p1]", got)
	}

	// With Alt held, the exact same box touches p2 too.
	app.KeyDown("alt")
	app.DragAt("id:editor-viewport", pt(ax, ay), pt(bx-ax, by-ay))
	app.KeyUp("alt")
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"p1", "p2"}) {
		t.Fatalf("touch (Alt) marquee SelectedIDs = %v, want [p1 p2]", got)
	}

	// Shift+marquee adds p4 to the existing [p1, p2] selection.
	cx, cy := ed.WorldToCanvas(160, 100)
	dx, dy := ed.WorldToCanvas(200, 140)
	app.KeyDown("shift")
	app.DragAt("id:editor-viewport", pt(cx, cy), pt(dx-cx, dy-cy))
	app.KeyUp("shift")
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"p1", "p2", "p4"}) {
		t.Fatalf("Shift+marquee SelectedIDs = %v, want [p1 p2 p4]", got)
	}

	// Escape during an active marquee cancels the marquee without clearing the existing selection.
	app.Post(func() {
		ed.BeginMarquee2D(ax, ay)
		ed.UpdateMarquee2D(bx, by, editor.MarqueeEnclose)
	})
	app.Settle()
	if !ed.MarqueeActive() {
		t.Fatal("expected marquee to be active")
	}
	app.Key("escape")
	if ed.MarqueeActive() {
		t.Fatal("Escape did not cancel active marquee")
	}
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"p1", "p2", "p4"}) {
		t.Fatalf("cancelling marquee changed selection to %v, want [p1 p2 p4]", got)
	}
}

func TestMultiSelectRotateScaleAndDeleteOneUndoStep(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Transform Pair",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{ID: "left", X: 100, Y: 100},
					{ID: "right", X: 200, Y: 100},
				},
			},
		},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SelectObjectsWithPrimary([]string{"left", "right"}, "left")
	app := mountEditor(t, ed)

	// 1. Rotate about common center (150, 100) by +90 degrees (quarter turn clockwise on screen).
	beforeRot := saveYAML(t, ed)
	cx, cy := ed.WorldToCanvas(150, 100)
	// Combined outline spans X=[92..208] (half=58), Y=[92..108] (half=8); far = hypot(58,8) ≈ 58.55; ring = max(72, 58.55+16) = 74.55.
	ringR := float32(math.Hypot(58, 8)) + 16
	if h := ed.PickGizmo2D(cx+ringR, cy); h != editor.Handle2DRotate {
		t.Fatalf("PickGizmo2D on multi-select ring = %v, want Rotate", h)
	}
	app.DragAt("id:editor-viewport", pt(cx+ringR, cy), pt(-ringR+ringR*0.7071, ringR*0.7071), pt(-ringR, ringR))
	left, _, _ := ed.FindObject("left")
	right, _, _ := ed.FindObject("right")
	if !near(left.X, 150, 1e-2) || !near(left.Y, 50, 1e-2) || !near(left.Rotation, math.Pi/2, 1e-3) {
		t.Fatalf("left after 90° multi-rotate = (%v, %v) rot=%v, want (150, 50) rot=pi/2", left.X, left.Y, left.Rotation)
	}
	if !near(right.X, 150, 1e-2) || !near(right.Y, 150, 1e-2) || !near(right.Rotation, math.Pi/2, 1e-3) {
		t.Fatalf("right after 90° multi-rotate = (%v, %v) rot=%v, want (150, 150) rot=pi/2", right.X, right.Y, right.Rotation)
	}
	if ed.History.Len() != 1 {
		t.Fatalf("History.Len() after multi-rotate = %d, want 1", ed.History.Len())
	}
	undoInUI(app, ed)
	if got := saveYAML(t, ed); !bytes.Equal(got, beforeRot) {
		t.Fatalf("Undo did not restore multi-rotate")
	}

	// 2. Scale along X about common center (150, 100) by 2x using the East handle (needs zoom so height >= 24).
	ed.SetView2D(editor.View2D{X: 150, Y: 100, Zoom: 2})
	ed.History.Clear()
	beforeScale := saveYAML(t, ed)
	ex, ey := ed.WorldToCanvas(208, 100) // right edge of combined outline (200+8)
	if h := ed.PickGizmo2D(ex, ey); h != editor.Handle2DScaleE {
		t.Fatalf("PickGizmo2D on East handle = %v, want Scale E", h)
	}
	// Distance from center (150) to grab (208) is 58 world units = 116 canvas pts at zoom 2; drag +116 pts -> 2x X scale.
	app.DragAt("id:editor-viewport", pt(ex, ey), pt(116, 0))
	left, _, _ = ed.FindObject("left")
	right, _, _ = ed.FindObject("right")
	if !near(left.X, 50, 1e-2) || !near(right.X, 250, 1e-2) {
		t.Fatalf("positions after 2x X scale = left(%v) right(%v), want 50 and 250", left.X, right.X)
	}
	if !near(left.EffectiveScaleX(), 2, 1e-3) || !near(right.EffectiveScaleX(), 2, 1e-3) || left.ScaleY != nil || right.ScaleY != nil {
		t.Fatalf("scales after 2x X scale: left=(%v,%v) right=(%v,%v)", left.EffectiveScaleX(), left.ScaleY, right.EffectiveScaleX(), right.ScaleY)
	}
	if ed.History.Len() != 1 {
		t.Fatalf("History.Len() after multi-scale = %d, want 1", ed.History.Len())
	}
	undoInUI(app, ed)
	if got := saveYAML(t, ed); !bytes.Equal(got, beforeScale) {
		t.Fatalf("Undo did not restore multi-scale")
	}

	// 3. Delete all selected objects in one undo step, and Undo restores all objects and selection.
	ed.History.Clear()
	beforeDel := saveYAML(t, ed)
	app.Key("delete")
	if len(ed.Doc.Layers[0].Objects) != 0 {
		t.Fatalf("objects left after Delete = %d, want 0", len(ed.Doc.Layers[0].Objects))
	}
	if ed.SelectionCount() != 0 {
		t.Fatalf("selection after Delete = %v, want empty", ed.SelectedIDs())
	}
	if ed.History.Len() != 1 {
		t.Fatalf("History.Len() after multi-delete = %d, want 1", ed.History.Len())
	}
	undoInUI(app, ed)
	if got := saveYAML(t, ed); !bytes.Equal(got, beforeDel) {
		t.Fatalf("Undo did not restore deleted objects")
	}
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"left", "right"}) {
		t.Fatalf("Undo did not restore multi-selection: got %v, want [left right]", got)
	}
}

func TestMultiSelectParentAndChildDoesNotDoubleMoveOrFailDelete(t *testing.T) {
	ed := newNestedEditor(t)
	ed.SetCanvasSize(640, 360)
	ed.SelectObjectsWithPrimary([]string{"ship", "turret", "hero"}, "ship")
	before := saveYAML(t, ed)

	// Drag ship (at 60, 40; selected along with its child turret at local 10, 0 and sibling hero at 40, 40).
	sx, sy := ed.WorldToCanvas(60, 40)
	if h := ed.BeginDrag2D(sx, sy); h != editor.Handle2DBody {
		t.Fatalf("BeginDrag2D = %v, want Body", h)
	}
	ed.UpdateDrag2D(sx+20, sy+10, false)
	if !ed.EndDrag2D() {
		t.Fatal("EndDrag2D returned false")
	}
	ship, _, _ := ed.FindObject("ship")
	turret, _, _ := ed.FindObject("turret")
	hero, _, _ := ed.FindObject("hero")
	// ship moved by (+20, +10); turret's local offset must be unchanged so it didn't double-move; hero moved by (+20, +10).
	if !near(ship.X, 80, 1e-3) || !near(ship.Y, 50, 1e-3) {
		t.Fatalf("ship = (%v, %v), want (80, 50)", ship.X, ship.Y)
	}
	if !near(turret.X, 10, 1e-3) || !near(turret.Y, 0, 1e-3) {
		t.Fatalf("turret local pos changed to (%v, %v), want unchanged (10, 0)", turret.X, turret.Y)
	}
	if !near(hero.X, 60, 1e-3) || !near(hero.Y, 50, 1e-3) {
		t.Fatalf("hero = (%v, %v), want (60, 50)", hero.X, hero.Y)
	}
	if ed.History.Len() != 1 {
		t.Fatalf("History.Len() = %d, want 1", ed.History.Len())
	}
	ed.Undo()
	if got := saveYAML(t, ed); !bytes.Equal(got, before) {
		t.Fatal("Undo did not restore scene")
	}

	// Deleting parent + child + sibling in one step succeeds and Undo restores all.
	ed.History.Clear()
	if err := ed.DeleteSelected(); err != nil {
		t.Fatalf("DeleteSelected(parent+child+sibling): %v", err)
	}
	if o, _, _ := ed.FindObject("ship"); o != nil {
		t.Fatal("ship still present")
	}
	if o, _, _ := ed.FindObject("hero"); o != nil {
		t.Fatal("hero still present")
	}
	if ed.History.Len() != 1 {
		t.Fatalf("History.Len() after delete = %d, want 1", ed.History.Len())
	}
	ed.Undo()
	if got := saveYAML(t, ed); !bytes.Equal(got, before) {
		t.Fatal("Undo did not restore deleted parent+child+sibling")
	}
}

func TestMultiSelectGizmo3DTranslateRotateScale(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "3D Pair",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "main",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{ID: "a", X: -20, Y: 0, PosZ: 0, Mesh: "box"},
					{ID: "b", X: 20, Y: 0, PosZ: 0, Mesh: "box"},
				},
			},
		},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetViewMode(editor.ViewMode3D)
	ed.SetCanvasSize(640, 360)
	ed.SelectObjectsWithPrimary([]string{"a", "b"}, "a")

	// 1. Translate along X in 3D as one undo step.
	beforeT := saveYAML(t, ed)
	ed.SetGizmoMode3D(editor.Gizmo3DTranslate)
	ox, oy, _, _ := ed.Camera3D.Project([3]float32{0, 0, 0}, 640, 360)
	tx, ty, _, _ := ed.Camera3D.Project([3]float32{30, 0, 0}, 640, 360)
	if !ed.BeginGizmoDragAxis3D(editor.Axis3DX, ox, oy, 640, 360) {
		t.Fatal("BeginGizmoDragAxis3D Translate X failed")
	}
	ed.UpdateGizmoDrag3D(tx, ty)
	if !ed.EndGizmoDrag3D() {
		t.Fatal("EndGizmoDrag3D Translate X returned false")
	}
	a, _, _ := ed.FindObject("a")
	b, _, _ := ed.FindObject("b")
	if !near(a.X, 10, 1e-1) || !near(b.X, 50, 1e-1) {
		t.Fatalf("after 3D translate X by +30: a.X=%v b.X=%v, want 10 and 50", a.X, b.X)
	}
	if ed.History.Len() != 1 {
		t.Fatalf("History.Len() = %d, want 1", ed.History.Len())
	}
	ed.Undo()
	if got := saveYAML(t, ed); !bytes.Equal(got, beforeT) {
		t.Fatal("Undo did not restore 3D translate")
	}

	// 2. Rotate around Y about common center (0, 0, 0) in 3D as one undo step.
	ed.History.Clear()
	beforeR := saveYAML(t, ed)
	ed.SetGizmoMode3D(editor.Gizmo3DRotate)
	p0x, p0y, _, _ := ed.Camera3D.Project([3]float32{20, 0, 0}, 640, 360)
	p1x, p1y, _, _ := ed.Camera3D.Project([3]float32{0, 0, -20}, 640, 360)
	if !ed.BeginGizmoDragAxis3D(editor.Axis3DY, p0x, p0y, 640, 360) {
		t.Fatal("BeginGizmoDragAxis3D Rotate Y failed")
	}
	ed.UpdateGizmoDrag3D(p1x, p1y)
	if !ed.EndGizmoDrag3D() {
		t.Fatal("EndGizmoDrag3D Rotate Y returned false")
	}
	a, _, _ = ed.FindObject("a")
	b, _, _ = ed.FindObject("b")
	// Both orbited by +90° around Y: a(-20,0,0)->(0,0,20), b(20,0,0)->(0,0,-20).
	if !near(a.X, 0, 1e-1) || !near(a.PosZ, 20, 1e-1) || !near(b.X, 0, 1e-1) || !near(b.PosZ, -20, 1e-1) {
		t.Fatalf("after 3D rotate Y 90°: a=(%v,%v,%v) b=(%v,%v,%v)", a.X, a.Y, a.PosZ, b.X, b.Y, b.PosZ)
	}
	if ed.History.Len() != 1 {
		t.Fatalf("History.Len() after 3D rotate = %d, want 1", ed.History.Len())
	}
	ed.Undo()
	if got := saveYAML(t, ed); !bytes.Equal(got, beforeR) {
		t.Fatal("Undo did not restore 3D rotate")
	}
}

func TestSceneTreeFollowsAndDrivesMultiSelection(t *testing.T) {
	ed := newMultiSelectEditor(t)
	app := startEditorView(t, ed)

	// Click p1, then Mod+click p3 in the scene tree -> both selected in editor and inspector shows "2 selected".
	app.Press("id:scene-tree-label-p1")
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"p1"}) {
		t.Fatalf("after tree click p1: SelectedIDs=%v", got)
	}
	app.PressWith("id:scene-tree-label-p3", core.Modifiers{Platform: true})
	if got := ed.SelectedIDs(); len(got) != 2 || !ed.IsSelected("p1") || !ed.IsSelected("p3") {
		t.Fatalf("after Mod+click p3 in tree: SelectedIDs=%v", got)
	}
	if got := app.Find("id:inspector-selection-count").Text; got != "2 selected" {
		t.Fatalf("inspector header = %q, want 2 selected", got)
	}

	// Shift+click p5 in the scene tree extends the range selection (p3..p5 or p1..p5).
	app.PressWith("id:scene-tree-label-p5", core.Modifiers{Shift: true})
	if !ed.IsSelected("p5") || ed.SelectionCount() < 3 {
		t.Fatalf("after Shift+click p5 in tree: SelectedIDs=%v", ed.SelectedIDs())
	}

	// Updating selection in the editor syncs back into the tree without fighting.
	app.Post(func() { ed.SelectObjectsWithPrimary([]string{"p2", "p4"}, "p2") })
	app.Settle()
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"p2", "p4"}) {
		t.Fatalf("after editor SelectObjects: SelectedIDs=%v", got)
	}
	if got := app.Find("id:inspector-selection-count").Text; got != "2 selected" {
		t.Fatalf("inspector header after editor SelectObjects = %q, want 2 selected", got)
	}

	// Mod+clicking the last remaining selected row in the Scene Tree clears the selection.
	app.Post(func() { ed.SelectObject("p1") })
	app.Settle()
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"p1"}) {
		t.Fatalf("after SelectObject(p1): SelectedIDs=%v", got)
	}
	app.PressWith("id:scene-tree-label-p1", core.Modifiers{Platform: true})
	if got := ed.SelectionCount(); got != 0 {
		t.Fatalf("after Mod+click on last selected row p1: SelectionCount=%d SelectedIDs=%v, want 0", got, ed.SelectedIDs())
	}
}

func TestMultiSelectTabIsolationAndMarqueeCancel(t *testing.T) {
	dir := t.TempDir()
	sceneYAML := `version: 4
name: Tab Scene
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: p1
        x: 60
        y: 120
      - id: p2
        x: 100
        y: 120
      - id: p3
        x: 140
        y: 120
`
	fileA := filepath.Join(dir, "a.scene.yaml")
	fileB := filepath.Join(dir, "b.scene.yaml")
	if err := os.WriteFile(fileA, []byte(sceneYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte(sceneYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.OpenFile(fileA); err != nil {
		t.Fatalf("OpenFile(a): %v", err)
	}
	if err := ed.OpenFile(fileB); err != nil {
		t.Fatalf("OpenFile(b): %v", err)
	}
	if ed.TabCount() != 2 {
		t.Fatalf("TabCount = %d, want 2", ed.TabCount())
	}

	// Select [p1, p2] in tab 0 and start a marquee drag, then switch to tab 1.
	if err := ed.SelectTab(0); err != nil {
		t.Fatalf("SelectTab(0): %v", err)
	}
	ed.SelectObjectsWithPrimary([]string{"p1", "p2"}, "p2")
	ed.SetTool(editor.ToolSelect)
	if !ed.BeginMarquee2D(10, 10) {
		t.Fatal("BeginMarquee2D failed")
	}
	if err := ed.SelectTab(1); err != nil {
		t.Fatalf("SelectTab(1): %v", err)
	}
	if ed.MarqueeActive() {
		t.Fatal("SelectTab did not cancel active marquee drag")
	}
	if got := ed.SelectedIDs(); len(got) != 0 {
		t.Fatalf("tab 1 selection leaked from tab 0: got %v, want empty", got)
	}

	// Select p3 in tab 1, then switch back to tab 0 and verify [p1, p2] (primary p2) is restored.
	ed.SelectObject("p3")
	if err := ed.SelectTab(0); err != nil {
		t.Fatalf("SelectTab(0) return: %v", err)
	}
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"p1", "p2"}) || ed.SelectedID != "p2" {
		t.Fatalf("tab 0 selection after return = %v (primary=%q), want [p1 p2] (primary=p2)", got, ed.SelectedID)
	}
}

func TestMultiDragSingularParentDoesNotZeroTransform(t *testing.T) {
	zero := sceneformat.Float32(0)
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Singular Parent",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#141824",
		Camera:          &sceneformat.CameraDoc{X: 100, Y: 100},
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Order: 0,
				Objects: []sceneformat.ObjectDoc{
					{ID: "good", X: 100, Y: 50, PosZ: 10, Mesh: "box"},
					{
						ID:     "zero_parent",
						X:      10,
						Y:      10,
						ScaleX: zero,
						ScaleY: zero,
						ScaleZ: zero,
						Children: []sceneformat.ObjectDoc{
							{ID: "singular_child", X: 40, Y: 30, PosZ: 5, Mesh: "box"},
						},
					},
				},
			},
		},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetCanvasSize(640, 360)
	ed.SetTool(editor.ToolSelect)
	// Make singular_child the primary SelectedID so falling through to the single-object path would zero it out.
	ed.SelectObjectsWithPrimary([]string{"good", "singular_child"}, "singular_child")

	// 2D drag: only "good" survives parent inversion; dragging + canceling must not zero singular_child.
	gx, gy := ed.WorldToCanvas(100, 50)
	if h := ed.BeginDrag2D(gx, gy); h == editor.Handle2DNone {
		t.Fatal("BeginDrag2D returned Handle2DNone")
	}
	ed.UpdateDrag2D(gx+25, gy+15, false)
	if !ed.CancelDrag2D() {
		t.Fatal("CancelDrag2D returned false")
	}
	sc, _, _ := ed.FindObject("singular_child")
	good, _, _ := ed.FindObject("good")
	if sc.X != 40 || sc.Y != 30 {
		t.Fatalf("after 2D cancel, singular_child = (%v, %v), want (40, 30)", sc.X, sc.Y)
	}
	if good.X != 100 || good.Y != 50 {
		t.Fatalf("after 2D cancel, good = (%v, %v), want (100, 50)", good.X, good.Y)
	}

	// Commit 2D drag and verify good moved while singular_child stayed untouched.
	if h := ed.BeginDrag2D(gx, gy); h == editor.Handle2DNone {
		t.Fatal("BeginDrag2D commit pass returned Handle2DNone")
	}
	ed.UpdateDrag2D(gx+20, gy+10, false)
	if !ed.EndDrag2D() {
		t.Fatal("EndDrag2D returned false")
	}
	sc, _, _ = ed.FindObject("singular_child")
	good, _, _ = ed.FindObject("good")
	if sc.X != 40 || sc.Y != 30 {
		t.Fatalf("after 2D end, singular_child = (%v, %v), want (40, 30)", sc.X, sc.Y)
	}
	if !near(good.X, 120, 1e-2) || !near(good.Y, 60, 1e-2) {
		t.Fatalf("after 2D end, good = (%v, %v), want (120, 60)", good.X, good.Y)
	}
	ed.Undo()

	// 3D drag: same scenario — only "good" has an invertible parent transform.
	ed.SetViewMode(editor.ViewMode3D)
	ed.SetGizmoMode3D(editor.Gizmo3DTranslate)
	ed.Camera3D = editor.OrbitCamera3D{Target: [3]float32{60, 40, 5}, Distance: 200, Yaw: 0, Pitch: 0.4}
	sx0, sy0, _, _ := ed.Camera3D.Project([3]float32{60, 40, 5}, 640, 360)
	sx1, sy1, _, _ := ed.Camera3D.Project([3]float32{80, 40, 5}, 640, 360)
	if !ed.BeginGizmoDragAxis3D(editor.Axis3DX, sx0, sy0, 640, 360) {
		t.Fatal("BeginGizmoDragAxis3D failed")
	}
	ed.UpdateGizmoDrag3D(sx1, sy1)
	ed.CancelGizmoDrag3D()
	sc, _, _ = ed.FindObject("singular_child")
	good, _, _ = ed.FindObject("good")
	if sc.X != 40 || sc.Y != 30 || sc.PosZ != 5 {
		t.Fatalf("after 3D cancel, singular_child = (%v, %v, %v), want (40, 30, 5)", sc.X, sc.Y, sc.PosZ)
	}
	if !near(good.X, 100, 1e-2) || !near(good.Y, 50, 1e-2) || !near(good.PosZ, 10, 1e-2) {
		t.Fatalf("after 3D cancel, good = (%v, %v, %v), want (100, 50, 10)", good.X, good.Y, good.PosZ)
	}
}

func TestMultiSelectHardeningNaNInfPlayLayerDemoDuplicateAndNotify(t *testing.T) {
	ed := newMultiSelectEditor(t)
	ed.SetCanvasSize(640, 360)
	ed.SetTool(editor.ToolSelect)

	// 1. SelectMarquee2D rejects NaN/Inf without mutating selection.
	ed.SelectObject("p1")
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	if res := ed.SelectMarquee2D(nan, 0, 100, 100, false, false, editor.MarqueeEnclose); res != nil {
		t.Fatalf("SelectMarquee2D(NaN) = %v, want nil", res)
	}
	if res := ed.SelectMarquee2D(0, 0, inf, 100, false, false, editor.MarqueeEnclose); res != nil {
		t.Fatalf("SelectMarquee2D(+Inf) = %v, want nil", res)
	}
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"p1"}) {
		t.Fatalf("selection changed after non-finite SelectMarquee2D: %v", got)
	}

	// 2. EndMarquee2D triggers notify even when the selection set does not change.
	x0, y0 := ed.WorldToCanvas(40, 100)
	x1, y1 := ed.WorldToCanvas(80, 140)
	if !ed.BeginMarquee2D(x0, y0) {
		t.Fatal("BeginMarquee2D failed")
	}
	ed.UpdateMarquee2D(x1, y1, editor.MarqueeEnclose)
	beforeEnd := ed.Revision()
	ed.EndMarquee2D()
	if ed.Revision() <= beforeEnd {
		t.Fatalf("EndMarquee2D did not notify when selection remained unchanged (before=%d after=%d)", beforeEnd, ed.Revision())
	}

	// 3. NaN/Inf during 2D multi-rotate does not poison drag state.
	ed.SelectObjectsWithPrimary([]string{"p1", "p2"}, "p1")
	rx0, ry0 := ed.WorldToCanvas(80, 120)
	if h := ed.BeginDrag2D(rx0, ry0); h == editor.Handle2DNone {
		t.Fatal("BeginDrag2D failed")
	}
	ed.UpdateDrag2D(nan, inf, false)
	ed.UpdateDrag2D(rx0+15, ry0+10, false)
	if !ed.EndDrag2D() {
		t.Fatal("EndDrag2D after NaN update returned false")
	}
	p1, _, _ := ed.FindObject("p1")
	if math.IsNaN(float64(p1.X)) || math.IsNaN(float64(p1.Y)) || !near(p1.X, 75, 1e-2) {
		t.Fatalf("after 2D drag with NaN interlude, p1 = (%v, %v), want (75, 130)", p1.X, p1.Y)
	}
	ed.Undo()

	// 4. NaN/Inf in 3D multi-drag & zero-scale guard.
	ed.SetViewMode(editor.ViewMode3D)
	ed.SetGizmoMode3D(editor.Gizmo3DScale)
	ed.Camera3D = editor.OrbitCamera3D{Target: [3]float32{80, 120, 0}, Distance: 200, Yaw: 0, Pitch: 0.3}
	s0x, s0y, _, _ := ed.Camera3D.Project([3]float32{80, 120, 0}, 640, 360)
	if !ed.BeginGizmoDragAxis3D(editor.Axis3DX, s0x, s0y, 640, 360) {
		t.Fatal("BeginGizmoDragAxis3D failed")
	}
	ed.UpdateGizmoDrag3D(nan, inf)
	ed.CancelGizmoDrag3D()
	p1, _, _ = ed.FindObject("p1")
	if math.IsNaN(float64(p1.X)) || math.IsNaN(float64(p1.EffectiveScaleX())) || p1.EffectiveScaleX() == 0 {
		t.Fatalf("after 3D NaN drag, p1 invalid: X=%v ScaleX=%v", p1.X, p1.EffectiveScaleX())
	}
	ed.SetViewMode(editor.ViewMode2D)

	// 5. RemoveLayer prunes removed objects from multi-selection and Undo restores full multi-selection.
	if err := ed.AddLayer("extra", sceneformat.SpaceWorld); err != nil {
		t.Fatalf("AddLayer: %v", err)
	}
	if _, err := ed.PlaceObject("extra", sceneformat.ObjectDoc{ID: "extra_obj", X: 20, Y: 20}); err != nil {
		t.Fatalf("PlaceObject: %v", err)
	}
	ed.SelectObjectsWithPrimary([]string{"p1", "extra_obj"}, "extra_obj")
	if err := ed.RemoveLayer("extra"); err != nil {
		t.Fatalf("RemoveLayer: %v", err)
	}
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"p1"}) || ed.SelectedID != "p1" {
		t.Fatalf("after RemoveLayer(extra): SelectedIDs=%v primary=%q, want [p1] primary=p1", got, ed.SelectedID)
	}
	if !ed.Undo() {
		t.Fatal("Undo RemoveLayer returned false")
	}
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"p1", "extra_obj"}) || ed.SelectedID != "extra_obj" {
		t.Fatalf("after Undo RemoveLayer: SelectedIDs=%v primary=%q, want [p1 extra_obj] primary=extra_obj", got, ed.SelectedID)
	}

	// 6. DuplicateSelected Redo restores all duplicated objects selected.
	ed.SelectObjectsWithPrimary([]string{"p1", "p2"}, "p2")
	if _, err := ed.DuplicateSelected(); err != nil {
		t.Fatalf("DuplicateSelected: %v", err)
	}
	dupIDs := ed.SelectedIDs()
	if len(dupIDs) != 2 {
		t.Fatalf("after DuplicateSelected: SelectedIDs=%v, want 2", dupIDs)
	}
	if !ed.Undo() {
		t.Fatal("Undo DuplicateSelected returned false")
	}
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"p1", "p2"}) {
		t.Fatalf("after Undo DuplicateSelected: SelectedIDs=%v, want [p1 p2]", got)
	}
	if !ed.Redo() {
		t.Fatal("Redo DuplicateSelected returned false")
	}
	if got := ed.SelectedIDs(); !slices.Equal(got, dupIDs) {
		t.Fatalf("after Redo DuplicateSelected: SelectedIDs=%v, want %v", got, dupIDs)
	}

	// 7. Multi-inspector integer overflow guard & invalid input revert, plus StartPlay marquee cancel.
	app := startEditorView(t, ed)
	app.Post(func() {
		if !ed.BeginMarquee2D(x0, y0) {
			t.Errorf("BeginMarquee2D before Play failed")
		}
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
		if ed.MarqueeActive() {
			t.Errorf("Play did not cancel active marquee")
		}
		if err := ed.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
		ed.SelectObjectsWithPrimary([]string{"p1", "p2"}, "p1")
	})
	app.Settle()
	app.Focus("id:inspector-z")
	app.Type("id:inspector-z", "1e30")
	app.Submit("id:inspector-z")
	app.Settle()
	p1, _, _ = ed.FindObject("p1")
	p2, _, _ := ed.FindObject("p2")
	if p1.Z != 0 || p2.Z != 0 {
		t.Fatalf("overflow input 1e30 mutated Z: p1.Z=%d p2.Z=%d, want 0", p1.Z, p2.Z)
	}
	app.Focus("id:inspector-x")
	app.Type("id:inspector-x", "not-a-number")
	app.Submit("id:inspector-x")
	app.Settle()
	if got := app.Find("id:inspector-x").Text; got != "" {
		t.Fatalf("invalid numeric submit did not revert draft: got %q, want empty mixed value", got)
	}

	// 9. OpenDemo clears stale selectedIDs and active marquee.
	app.Post(func() {
		ed.SelectObjectsWithPrimary([]string{"p1", "p2"}, "p1")
		ed.BeginMarquee2D(10, 10)
		if err := ed.OpenDemo(); err != nil {
			t.Errorf("OpenDemo: %v", err)
		}
	})
	app.Settle()
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"hero"}) || ed.MarqueeActive() {
		t.Fatalf("after OpenDemo: SelectedIDs=%v MarqueeActive=%v, want [hero] false", got, ed.MarqueeActive())
	}
}
