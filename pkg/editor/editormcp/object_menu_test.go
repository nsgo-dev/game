package editormcp_test

import (
	"bytes"
	"reflect"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// #1039: Every object context menu action has a matching editormcp tool backed by the same Editor methods.
func TestObjectContextMenuMCPTools(t *testing.T) {
	f := openFixture(t)
	// Add two prefab instances so Select same prefab, Group, and Move under all have rich targets.
	if _, err := f.ed.PlaceObject("actors", sceneformat.ObjectDoc{
		ID:     "slime_1",
		Name:   "Slime 01",
		Prefab: "slime",
		X:      120,
		Y:      80,
		Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "slime"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ed.PlaceObject("actors", sceneformat.ObjectDoc{
		ID:     "slime_2",
		Name:   "Slime 02",
		Prefab: "slime",
		X:      180,
		Y:      80,
		Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "slime"},
	}); err != nil {
		t.Fatal(err)
	}
	f.ed.History.Clear()

	var clipboard string
	f.ed.WriteClipboard = func(text string) { clipboard = text }
	f.ed.ReadClipboard = func(done func(string)) { done(clipboard) }

	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// 1. object_copy_yaml copies canonical sceneformat YAML that sceneformat.Parse parses back identically.
	res := call(t, cs, editormcp.ToolObjectCopyYAML, map[string]any{"id": "slime_1"})
	mustOK(t, res)
	var copyOut editormcp.ObjectCopyYAMLResult
	structured(t, res, &copyOut)
	if copyOut.Lines <= 0 || copyOut.YAML != clipboard {
		t.Fatalf("object_copy_yaml result = %+v, clipboard len = %d", copyOut, len(clipboard))
	}
	parsed, err := sceneformat.Parse([]byte(copyOut.YAML))
	if err != nil {
		t.Fatalf("sceneformat.Parse(object_copy_yaml): %v", err)
	}
	wantObj, _, _ := f.ed.FindObject("slime_1")
	if !reflect.DeepEqual(parsed.Layers[0].Objects[0], *wantObj) {
		t.Fatalf("parsed object mismatch:\ngot:  %+v\nwant: %+v", parsed.Layers[0].Objects[0], *wantObj)
	}

	// 2. object_paste_yaml places a copy from clipboard in one undo step.
	res = call(t, cs, editormcp.ToolObjectPasteYAML, map[string]any{})
	mustOK(t, res)
	var pasteOut editormcp.ObjectPasteYAMLResult
	structured(t, res, &pasteOut)
	if pasteOut.ID == "" || pasteOut.ID == "slime_1" {
		t.Fatalf("pasted ID = %q", pasteOut.ID)
	}
	f.ed.Undo()
	if obj, _, _ := f.ed.FindObject(pasteOut.ID); obj != nil {
		t.Fatalf("Undo did not remove pasted object %q", pasteOut.ID)
	}

	// 3. object_duplicate clones an object in one undo step.
	res = call(t, cs, editormcp.ToolObjectDuplicate, map[string]any{"id": "slime_1"})
	mustOK(t, res)
	var dupOut editormcp.ObjectDuplicateResult
	structured(t, res, &dupOut)
	if dupOut.ID == "" || dupOut.ID == "slime_1" {
		t.Fatalf("duplicate ID = %q", dupOut.ID)
	}
	f.ed.Undo()
	if obj, _, _ := f.ed.FindObject(dupOut.ID); obj != nil {
		t.Fatalf("Undo did not remove duplicate %q", dupOut.ID)
	}

	// 4. object_move_under lists candidates and reparents in one undo step.
	res = call(t, cs, editormcp.ToolObjectMoveUnder, map[string]any{"id": "slime_1", "listCandidates": true})
	mustOK(t, res)
	var candOut editormcp.ObjectMoveUnderResult
	structured(t, res, &candOut)
	if len(candOut.Candidates) == 0 {
		t.Fatal("expected non-empty Move under candidates")
	}
	for _, c := range candOut.Candidates {
		if c.ID == "slime_1" {
			t.Fatal("Move under candidates must exclude self")
		}
	}

	beforeMove, err := f.ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	res = call(t, cs, editormcp.ToolObjectMoveUnder, map[string]any{"id": "slime_1", "parent": "hero"})
	mustOK(t, res)
	heroObj, _, _ := f.ed.FindObject("hero")
	if heroObj == nil || len(heroObj.Children) != 1 || heroObj.Children[0].ID != "slime_1" {
		t.Fatalf("hero children after object_move_under = %+v", heroObj)
	}
	f.ed.Undo()
	afterMoveUndo, err := f.ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeMove, afterMoveUndo) {
		t.Fatalf("ONE Undo of object_move_under did not restore scene:\n%s\nwant:\n%s", afterMoveUndo, beforeMove)
	}

	// 5. object_select_same_prefab selects both slimes and selection_get reports selectedIds.
	res = call(t, cs, editormcp.ToolObjectSelectSamePrefab, map[string]any{"id": "slime_1"})
	mustOK(t, res)
	var prefOut editormcp.ObjectSelectSamePrefabResult
	structured(t, res, &prefOut)
	if !slices.Equal(prefOut.SelectedIDs, []string{"slime_1", "slime_2"}) {
		t.Fatalf("object_select_same_prefab = %v, want [slime_1 slime_2]", prefOut.SelectedIDs)
	}
	selRes := call(t, cs, editormcp.ToolSelectionGet, map[string]any{})
	mustOK(t, selRes)
	var selOut editormcp.Selection
	structured(t, selRes, &selOut)
	if !slices.Equal(selOut.SelectedIDs, []string{"slime_1", "slime_2"}) {
		t.Fatalf("selection_get.SelectedIDs = %v, want [slime_1 slime_2]", selOut.SelectedIDs)
	}

	// 6. object_group groups the objects it names in one undo step.
	res = call(t, cs, editormcp.ToolObjectGroup, map[string]any{"ids": selOut.SelectedIDs, "name": "Slimes"})
	mustOK(t, res)
	var grpOut editormcp.ObjectGroupResult
	structured(t, res, &grpOut)
	grpObj, _, _ := f.ed.FindObject(grpOut.GroupID)
	if grpObj == nil || grpObj.Name != "Slimes" || len(grpObj.Children) != 2 {
		t.Fatalf("group object = %+v", grpObj)
	}
	f.ed.Undo()
	if obj, _, _ := f.ed.FindObject(grpOut.GroupID); obj != nil {
		t.Fatalf("ONE Undo did not remove group %q", grpOut.GroupID)
	}

	// 7. object_visibility hides and locks an object without adding undo history.
	res = call(t, cs, editormcp.ToolObjectVisibility, map[string]any{"id": "hero", "hidden": true, "locked": true})
	mustOK(t, res)
	var visOut editormcp.ObjectVisibilityResult
	structured(t, res, &visOut)
	if !visOut.Hidden || !visOut.Locked || !f.ed.ObjectHidden("hero") || !f.ed.ObjectLocked("hero") {
		t.Fatalf("object_visibility = %+v", visOut)
	}

	// 8. object_find_usages finds usages in the open project.
	res = call(t, cs, editormcp.ToolObjectFindUsages, map[string]any{"id": "hero"})
	mustOK(t, res)
	var usageOut editor.ObjectUsagesResult
	structured(t, res, &usageOut)
	if usageOut.Count == 0 {
		t.Fatalf("object_find_usages(hero) count = 0, want >= 1")
	}
}
