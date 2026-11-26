package editor_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func startEditorView(t *testing.T, ed *editor.Editor) *nsgotest.App {
	t.Helper()
	return nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))
}

// A child two levels deep is selected from the tree, moved in the viewport and renamed from its row, and the saved scene differs from the opened one in those fields alone.
func TestSceneTreeSelectsMovesAndRenamesADeepChild(t *testing.T) {
	ed := newNestedEditor(t)
	opened := mustSave(t, ed)
	app := startEditorView(t, ed)

	app.Find("id:scene-tree-layer-actors")
	if !app.Exists("id:scene-tree-label-ship") || app.Exists("id:scene-tree-label-turret") {
		t.Fatal("the tree does not start with the layer open and its objects closed")
	}
	app.Press("label:ship >> role:button Expand")
	app.Press("label:turret >> role:button Expand")
	app.Press("id:scene-tree-label-barrel")
	if ed.SelectedID != "barrel" {
		t.Fatalf("selected %q from the tree, want barrel", ed.SelectedID)
	}
	if got := app.Find("id:inspector-object-id").Text; got != "barrel" {
		t.Fatalf("the inspector shows %q, want barrel", got)
	}

	app.Press("id:tool-move")
	// The canvas shows the scene through the 2D view, so the world point (100, 50) is where WorldToCanvas puts it.
	px, py := ed.WorldToCanvas(100, 50)
	app.Touch("id:editor-viewport", core.Point{X: core.Points(px), Y: core.Points(py)}).Lift()

	app.Key("f2")
	app.Clear("id:scene-tree-name-barrel")
	app.Type("id:scene-tree-name-barrel", "Cannon\n")
	if app.Exists("id:scene-tree-name-barrel") {
		t.Fatal("the name field stayed open after Return")
	}
	if got := app.Find("id:scene-tree-label-barrel").Text; got != "Cannon" {
		t.Fatalf("the row reads %q after the rename, want Cannon", got)
	}

	want, err := sceneformat.Parse(opened)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	barrel := &want.Layers[1].Objects[1].Children[0].Children[0]
	barrel.X, barrel.Y, barrel.Name = 30, 10, "Cannon"
	wantYAML, err := sceneformat.Encode(want)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	saved := mustSave(t, ed)
	if !bytes.Equal(saved, wantYAML) {
		t.Fatalf("saved scene:\n%s\nwant:\n%s", saved, wantYAML)
	}
	reloaded, err := sceneformat.Parse(saved)
	if err != nil {
		t.Fatalf("Parse saved: %v", err)
	}
	if again, err := sceneformat.Encode(reloaded); err != nil || !bytes.Equal(again, saved) {
		t.Fatalf("the saved scene does not round-trip (%v)", err)
	}
}

// Dragging a row onto another makes it that object's child at the same world position, and Undo puts it back exactly.
func TestSceneTreeDragMakesAChildAndUndoRestoresIt(t *testing.T) {
	ed := newNestedEditor(t)
	if err := ed.RotateObject("ship", 0.4); err != nil {
		t.Fatalf("RotateObject: %v", err)
	}
	ed.History.Clear()
	before := mustSave(t, ed)
	world := worldOf(t, ed, "hero")
	app := startEditorView(t, ed)

	drag(app, "id:scene-tree-label-hero", "id:scene-tree-row-ship", 0.5)
	if parentOf(ed, "hero") != "ship" {
		t.Fatalf("hero's parent after the drop = %q, want ship (status %q)", parentOf(ed, "hero"), ed.Status)
	}
	if got := worldOf(t, ed, "hero"); !got.ApproxEqual(world, 1e-3) {
		t.Fatalf("hero's world transform moved: %+v, want %+v", got, world)
	}
	if !app.Exists("id:scene-tree-label-hero") {
		t.Fatal("the drop target did not open to show its new child")
	}

	undoInUI(app, ed)
	if after := mustSave(t, ed); !bytes.Equal(after, before) {
		t.Fatalf("Undo did not restore the scene:\n%s\nwant:\n%s", after, before)
	}

	// Near a row's bottom edge the drop makes a sibling after it; on a layer's row, a top-level object of that layer.
	drag(app, "id:scene-tree-label-hero", "id:scene-tree-row-crate", 0.95)
	if _, _, oi := ed.FindObject("hero"); oi != 2 || parentOf(ed, "hero") != "" {
		t.Fatalf("hero dropped after the crate is at %d under %q", oi, parentOf(ed, "hero"))
	}
	drag(app, "id:scene-tree-label-crate", "id:scene-tree-layer-ui", 0.5)
	if _, li, _ := ed.FindObject("crate"); li != 2 {
		t.Fatalf("crate dropped on the ui layer is on layer %d", li)
	}
}

// drag drags the view from by a finger onto the view to, at frac of its height.
func drag(app *nsgotest.App, from, to string, frac float32) {
	src := app.Find(from).Bounds()
	dst := app.Find(to).Bounds()
	start := core.Point{X: 4, Y: 4}
	finger := app.Touch(from, start)
	finger.Move(core.Point{X: 16, Y: 4})
	finger.Move(core.Point{X: dst.Origin.X - src.Origin.X + 10, Y: dst.Origin.Y - src.Origin.Y + dst.Size.Height*core.Points(frac)})
	finger.Lift()
}

func TestSceneTreeContextMenuTogglesAndReveal(t *testing.T) {
	ed := newNestedEditor(t)
	saved := mustSave(t, ed)
	app := startEditorView(t, ed)

	// Selecting in a viewport selects the row and opens the rows above it.
	app.Post(func() { ed.SelectObject("barrel") })
	app.Settle()
	if !app.Exists("id:scene-tree-label-barrel") {
		t.Fatal("selecting the barrel elsewhere did not reveal its row")
	}

	app.Press("id:scene-tree-eye-hero")
	app.Press("id:scene-tree-lock-crate")
	if !ed.ObjectHidden("hero") || !ed.ObjectLocked("crate") {
		t.Fatalf("hidden hero %v, locked crate %v", ed.ObjectHidden("hero"), ed.ObjectLocked("crate"))
	}
	if after := mustSave(t, ed); !bytes.Equal(after, saved) || ed.History.CanUndo() {
		t.Fatal("the eye and lock toggles reached the scene file or the history")
	}

	app.RightClick("id:scene-tree-row-crate")
	app.Press("role:menuitem Make Child of Previous")
	if parentOf(ed, "crate") != "ship" {
		t.Fatalf("crate's parent after Make Child of Previous = %q", parentOf(ed, "crate"))
	}
	app.RightClick("id:scene-tree-row-crate")
	app.Press("role:menuitem Move Up")
	if ship, _, _ := ed.FindObject("ship"); ship.Children[0].ID != "crate" {
		t.Fatalf("ship's first child after Move Up = %q", ship.Children[0].ID)
	}
	app.RightClick("id:scene-tree-row-crate")
	app.Press("role:menuitem Move Out of Parent")
	if _, _, oi := ed.FindObject("crate"); parentOf(ed, "crate") != "" || oi != 2 {
		t.Fatalf("crate after Move Out of Parent is at %d under %q", oi, parentOf(ed, "crate"))
	}
	app.RightClick("id:scene-tree-row-crate")
	app.Press("role:menuitem Change ID")
	app.Clear("id:scene-tree-id-crate")
	app.Type("id:scene-tree-id-crate", "box\n")
	if obj, _, _ := ed.FindObject("box"); obj == nil || !ed.ObjectLocked("box") {
		t.Fatalf("Change ID did not rename the crate and keep its lock (status %q)", ed.Status)
	}
	if !app.Exists("id:scene-tree-label-box") {
		t.Fatal("the renamed object's row is missing")
	}
	app.RightClick("id:scene-tree-row-box")
	app.Press("role:menuitem Move to Layer ui")
	if _, li, _ := ed.FindObject("box"); li != 2 {
		t.Fatalf("box after Move to Layer ui is on layer %d", li)
	}
	for ed.History.CanUndo() {
		undoInUI(app, ed)
	}
	if after := mustSave(t, ed); !bytes.Equal(after, saved) {
		t.Fatalf("undoing the menu's edits did not restore the scene:\n%s\nwant:\n%s", after, saved)
	}
	// Escape leaves a rename without changing the name.
	app.Post(func() { ed.SelectObject("hero") })
	app.Settle()
	app.Key("f2")
	app.Clear("id:scene-tree-name-hero")
	app.Type("id:scene-tree-name-hero", "Nobody")
	app.Key("escape")
	if app.Exists("id:scene-tree-name-hero") {
		t.Fatal("Escape left the name field open")
	}
	if obj, _, _ := ed.FindObject("hero"); obj.Name != "Hero" {
		t.Fatalf("Escape kept the typed name %q", obj.Name)
	}

	// A layer's row makes its layer the active one and leaves no object selected.
	app.Press("id:scene-tree-layer-ui")
	if ed.ActiveLayer != "ui" || ed.SelectedID != "" {
		t.Fatalf("after pressing the ui layer: active %q, selected %q", ed.ActiveLayer, ed.SelectedID)
	}
}

// A hidden object, with its descendants, draws nothing in either viewport: the viewport draws as it does with the object deleted.
func TestHiddenObjectsAreNotDrawnInEitherViewport(t *testing.T) {
	for _, mode := range []editor.ViewMode{editor.ViewMode2D, editor.ViewMode3D} {
		shown := newNestedEditor(t)
		hidden := newNestedEditor(t)
		deleted := newNestedEditor(t)
		if err := hidden.SetObjectHidden("ship", true); err != nil {
			t.Fatalf("SetObjectHidden: %v", err)
		}
		if err := deleted.DeleteObject("ship"); err != nil {
			t.Fatalf("DeleteObject: %v", err)
		}
		for _, ed := range []*editor.Editor{shown, hidden, deleted} {
			ed.SetViewMode(mode)
		}
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			canvas := func(id string, ed *editor.Editor) core.Node {
				return core.Canvas{TestID: id, Style: core.Style{Width: core.Pt(160), Height: core.Pt(120)}, Draw: ed.DrawViewport}
			}
			return core.View{Children: []core.Node{canvas("shown", shown), canvas("hidden", hidden), canvas("deleted", deleted)}}
		})
		shownOps, hiddenOps, deletedOps := app.Find("shown").Ops, app.Find("hidden").Ops, app.Find("deleted").Ops
		if mode == editor.ViewMode2D {
			gametest.AssertSameImages(t, hiddenOps, deletedOps)
			if len(gametest.ImageDraws(shownOps)) == len(gametest.ImageDraws(hiddenOps)) {
				t.Fatal("2D: hiding the ship left its barrel drawn")
			}
			continue
		}
		if got, want := fmt.Sprint(hiddenOps), fmt.Sprint(deletedOps); got != want {
			t.Fatalf("3D: the viewport with the ship hidden draws\n%s\nwant as with it deleted:\n%s", got, want)
		}
		if fmt.Sprint(shownOps) == fmt.Sprint(hiddenOps) {
			t.Fatal("3D: hiding the ship changed nothing drawn")
		}
	}
}
