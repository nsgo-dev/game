package editor_test

import (
	"bytes"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// newNestedEditor returns an editor over the test scene with a ship holding a turret holding a barrel on the actors layer, a free crate beside them, and a ui layer in screen space.
func newNestedEditor(t *testing.T) *editor.Editor {
	t.Helper()
	doc := testSceneDoc()
	actors := &doc.Layers[1]
	actors.Objects = append(actors.Objects,
		sceneformat.ObjectDoc{
			ID: "ship", X: 60, Y: 40,
			Children: []sceneformat.ObjectDoc{{
				ID: "turret", X: 10,
				Children: []sceneformat.ObjectDoc{{ID: "barrel", Name: "Barrel", X: 5, Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "slime"}}},
			}},
		},
		sceneformat.ObjectDoc{ID: "crate", X: 20, Y: 90},
	)
	doc.Layers = append(doc.Layers, sceneformat.LayerDoc{Name: "ui", Space: sceneformat.SpaceScreen, Order: 20})
	ed, err := editor.New(doc, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetActiveLayer("actors")
	return ed
}

func mustSave(t *testing.T, ed *editor.Editor) []byte {
	t.Helper()
	data, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	return data
}

func worldOf(t *testing.T, ed *editor.Editor, id string) math2d.Affine {
	t.Helper()
	if ed.BuildErr != nil {
		t.Fatalf("build error: %v", ed.BuildErr)
	}
	bo := ed.Built.Object(id)
	if bo == nil {
		t.Fatalf("built scene has no object %q", id)
	}
	return bo.World
}

func parentOf(ed *editor.Editor, id string) string {
	if bo := ed.Built.Object(id); bo != nil && bo.Parent != nil {
		return bo.Parent.ID
	}
	return ""
}

func TestNestedChildrenAreFoundSelectedAndEditedInLocalSpace(t *testing.T) {
	ed := newNestedEditor(t)
	obj, li, oi := ed.FindObject("barrel")
	if obj == nil || obj.ID != "barrel" || li != 1 || oi != 0 {
		t.Fatalf("FindObject(barrel) = %v, %d, %d", obj, li, oi)
	}
	if obj, li, oi := ed.FindObject("crate"); obj == nil || li != 1 || oi != 2 {
		t.Fatalf("FindObject(crate) = %v, %d, %d", obj, li, oi)
	}
	if obj, li, oi := ed.FindObject("nobody"); obj != nil || li != -1 || oi != -1 {
		t.Fatalf("FindObject(nobody) = %v, %d, %d", obj, li, oi)
	}
	if !ed.SelectObject("barrel") {
		t.Fatal("SelectObject on a nested child failed")
	}
	if sel, layer := ed.SelectedObject(); sel == nil || sel.ID != "barrel" || layer != "actors" {
		t.Fatalf("SelectedObject = %v, %q", sel, layer)
	}
	before := mustSave(t, ed)

	if err := ed.MoveObject("barrel", 7, 3); err != nil {
		t.Fatalf("MoveObject: %v", err)
	}
	if err := ed.RotateObject("barrel", 0.25); err != nil {
		t.Fatalf("RotateObject: %v", err)
	}
	if err := ed.SetObjectName("barrel", "Cannon"); err != nil {
		t.Fatalf("SetObjectName: %v", err)
	}
	if err := ed.SetProperty("barrel", "caliber", 3); err != nil {
		t.Fatalf("SetProperty: %v", err)
	}
	// The position is local: the barrel's world origin is the ship's and the turret's offsets plus its own.
	if w := worldOf(t, ed, "barrel"); w.Tx != 77 || w.Ty != 43 {
		t.Fatalf("barrel world origin = (%v, %v), want (77, 43)", w.Tx, w.Ty)
	}

	// Moving with the Move tool puts the child's origin under the press, in the parent's space.
	ed.SetTool(editor.ToolMove)
	ed.HandleCanvasPress(100, 50)
	if obj, _, _ := ed.FindObject("barrel"); obj.X != 30 || obj.Y != 10 {
		t.Fatalf("Move tool set the barrel to (%v, %v), want (30, 10) in the turret's space", obj.X, obj.Y)
	}

	if err := ed.DeleteObject("turret"); err != nil {
		t.Fatalf("DeleteObject nested: %v", err)
	}
	if obj, _, _ := ed.FindObject("barrel"); obj != nil {
		t.Fatal("deleting the turret left its child")
	}
	if ship, _, _ := ed.FindObject("ship"); ship == nil || ship.Children != nil {
		t.Fatalf("ship's children after deleting its only child = %v", ship.Children)
	}
	for ed.History.CanUndo() {
		ed.Undo()
	}
	if after := mustSave(t, ed); !bytes.Equal(after, before) {
		t.Fatalf("undoing every edit did not restore the scene:\n%s\nwant:\n%s", after, before)
	}
}

func TestHitTestPicksNestedChildrenByTheirWorldTransform(t *testing.T) {
	ed := newNestedEditor(t)
	if err := ed.ScaleObject("ship", 2, 2); err != nil {
		t.Fatalf("ScaleObject: %v", err)
	}
	// The barrel's 16x16 sprite has its origin at its top-left corner and is drawn at the ship's scale: from (90, 40) to (122, 72).
	if got := ed.HitTest(110, 60); got != "barrel" {
		t.Fatalf("HitTest on the barrel = %q, want barrel", got)
	}
	// The ship and the turret have no sprite: their marker boxes are 16 points wide, at the ship's scale 32.
	if got := ed.HitTest(62, 42); got != "ship" {
		t.Fatalf("HitTest on the ship = %q, want ship", got)
	}
	if got := ed.HitTest(140, 100); got != "" {
		t.Fatalf("HitTest on empty space = %q", got)
	}
}

func TestReparentKeepsTheWorldTransformAndUndoIsExact(t *testing.T) {
	ed := newNestedEditor(t)
	if err := ed.RotateObject("ship", 0.5); err != nil {
		t.Fatalf("RotateObject: %v", err)
	}
	if err := ed.ScaleObject("ship", 2, 2); err != nil {
		t.Fatalf("ScaleObject: %v", err)
	}
	ed.History.Clear()
	before := mustSave(t, ed)
	heroWorld := worldOf(t, ed, "hero")

	if err := ed.ReparentObject("hero", "turret", -1); err != nil {
		t.Fatalf("ReparentObject: %v", err)
	}
	if parentOf(ed, "hero") != "turret" {
		t.Fatalf("hero's parent = %q, want turret", parentOf(ed, "hero"))
	}
	if obj, _, oi := ed.FindObject("hero"); oi != 1 || obj.Rotation == 0 {
		t.Fatalf("hero is child %d with rotation %v, want the turret's last child, turned against the ship", oi, obj.Rotation)
	}
	if got := worldOf(t, ed, "hero"); !got.ApproxEqual(heroWorld, 1e-3) {
		t.Fatalf("hero's world transform moved: %+v, want %+v", got, heroWorld)
	}
	moved := mustSave(t, ed)
	if !ed.Undo() {
		t.Fatal("Undo failed")
	}
	if after := mustSave(t, ed); !bytes.Equal(after, before) {
		t.Fatalf("Undo did not restore the scene:\n%s\nwant:\n%s", after, before)
	}
	if !ed.Redo() {
		t.Fatal("Redo failed")
	}
	if after := mustSave(t, ed); !bytes.Equal(after, moved) {
		t.Fatalf("Redo did not re-apply the move:\n%s\nwant:\n%s", after, moved)
	}

	// Taking a child out to the top level of another layer keeps its world transform too.
	barrelWorld := worldOf(t, ed, "barrel")
	if err := ed.MoveObjectToLayer("barrel", "floor", 0); err != nil {
		t.Fatalf("MoveObjectToLayer: %v", err)
	}
	if obj, li, oi := ed.FindObject("barrel"); obj == nil || li != 0 || oi != 0 || parentOf(ed, "barrel") != "" {
		t.Fatalf("barrel is at layer %d index %d under %q", li, oi, parentOf(ed, "barrel"))
	}
	if got := worldOf(t, ed, "barrel"); !got.ApproxEqual(barrelWorld, 1e-3) {
		t.Fatalf("barrel's world transform moved: %+v, want %+v", got, barrelWorld)
	}
	// Back under the ship, as its first child.
	if err := ed.ReparentObject("barrel", "ship", 0); err != nil {
		t.Fatalf("ReparentObject back: %v", err)
	}
	if ship, _, _ := ed.FindObject("ship"); ship.Children[0].ID != "barrel" {
		t.Fatalf("ship's first child = %q", ship.Children[0].ID)
	}
	if got := worldOf(t, ed, "barrel"); !got.ApproxEqual(barrelWorld, 1e-3) {
		t.Fatalf("barrel's world transform moved back under the ship: %+v, want %+v", got, barrelWorld)
	}
	for ed.Undo() {
	}
	if after := mustSave(t, ed); !bytes.Equal(after, before) {
		t.Fatalf("undoing every move did not restore the scene:\n%s\nwant:\n%s", after, before)
	}
}

func TestReparentUnderAnUntransformedParentWritesPlainFields(t *testing.T) {
	ed := newNestedEditor(t)
	if err := ed.ReparentObject("crate", "turret", -1); err != nil {
		t.Fatalf("ReparentObject: %v", err)
	}
	crate, _, _ := ed.FindObject("crate")
	// 20 - 70 and 90 - 40, with no scale field written for a scale that stays 1.
	if crate.X != -50 || crate.Y != 50 || crate.Rotation != 0 || crate.ScaleX != nil || crate.ScaleY != nil {
		t.Fatalf("crate under the turret = %+v", *crate)
	}
}

func TestReparentRefusesCyclesDepthAndShear(t *testing.T) {
	ed := newNestedEditor(t)
	for _, tc := range []struct {
		name, id, parent, want string
	}{
		{"itself", "ship", "ship", "itself or one of its descendants"},
		{"descendant", "ship", "barrel", "itself or one of its descendants"},
		{"missing parent", "crate", "nobody", `parent object "nobody" not found`},
		{"missing object", "nobody", "ship", `object "nobody" not found`},
	} {
		if err := ed.ReparentObject(tc.id, tc.parent, -1); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
	if err := ed.MoveObjectToLayer("crate", "nowhere", -1); err == nil || !strings.Contains(err.Error(), `layer "nowhere" not found`) {
		t.Errorf("MoveObjectToLayer to a missing layer: err = %v", err)
	}

	// A chain of MaxObjectDepth objects cannot take one more level above it.
	deep := sceneformat.ObjectDoc{ID: "d1"}
	leaf := &deep
	for i := 2; i <= sceneformat.MaxObjectDepth; i++ {
		leaf.Children = []sceneformat.ObjectDoc{{ID: "d" + strconv.Itoa(i)}}
		leaf = &leaf.Children[0]
	}
	if _, err := ed.PlaceObject("actors", deep); err != nil {
		t.Fatalf("PlaceObject deep chain: %v", err)
	}
	if err := ed.ReparentObject("d1", "crate", -1); err == nil || !strings.Contains(err.Error(), "deeper than") {
		t.Errorf("moving a full-depth chain under another object: err = %v", err)
	}
	if err := ed.ReparentObject("crate", "d"+strconv.Itoa(sceneformat.MaxObjectDepth), -1); err == nil || !strings.Contains(err.Error(), "deeper than") {
		t.Errorf("moving an object under the deepest leaf: err = %v", err)
	}
	if err := ed.DeleteObject("d1"); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}

	// A sprite cannot keep its world transform under a turned parent stretched along one axis.
	if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "stretched", Rotation: 0.5, ScaleX: sceneformat.Float32(2)}); err != nil {
		t.Fatalf("PlaceObject stretched: %v", err)
	}
	ed.History.Clear()
	placed := mustSave(t, ed)
	if err := ed.ReparentObject("hero", "stretched", -1); err == nil || !strings.Contains(err.Error(), "cannot be written") {
		t.Errorf("moving a sprite under a sheared parent: err = %v", err)
	}
	if ed.History.CanUndo() {
		t.Error("a refused move reached the history")
	}
	if after := mustSave(t, ed); !bytes.Equal(after, placed) {
		t.Errorf("a refused move changed the scene")
	}
}

func TestReparentKeepsTheWorldTransformIn3D(t *testing.T) {
	ed := newNestedEditor(t)
	if err := ed.MoveObject3D("ship", 60, 40, 12); err != nil {
		t.Fatalf("MoveObject3D: %v", err)
	}
	if err := ed.RotateObject3D("ship", 0.3, 0.7, -0.2); err != nil {
		t.Fatalf("RotateObject3D: %v", err)
	}
	if err := ed.ScaleObject3D("ship", 2, 2, 2); err != nil {
		t.Fatalf("ScaleObject3D: %v", err)
	}
	if err := ed.MoveObject3D("crate", 5, -4, 9); err != nil {
		t.Fatalf("MoveObject3D crate: %v", err)
	}
	crate, _, _ := ed.FindObject("crate")
	cornersBefore := ed.ObjectCorners3D(crate)
	if err := ed.ReparentObject("crate", "turret", -1); err != nil {
		t.Fatalf("ReparentObject: %v", err)
	}
	crate, _, _ = ed.FindObject("crate")
	if crate.RotX == 0 && crate.RotY == 0 && crate.RotZ == 0 {
		t.Fatal("the crate's local rotation did not take the ship's turn out")
	}
	cornersAfter := ed.ObjectCorners3D(crate)
	for i := range cornersBefore {
		for a := range 3 {
			if d := math.Abs(float64(cornersAfter[i][a] - cornersBefore[i][a])); d > 1e-3 {
				t.Fatalf("corner %d moved from %v to %v", i, cornersBefore[i], cornersAfter[i])
			}
		}
	}
}

func TestReorderAndDropObject(t *testing.T) {
	ed := newNestedEditor(t)
	order := func(parent string) []string {
		var objs []sceneformat.ObjectDoc
		if parent == "" {
			objs = ed.Doc.Layers[1].Objects
		} else {
			obj, _, _ := ed.FindObject(parent)
			objs = obj.Children
		}
		var ids []string
		for _, o := range objs {
			ids = append(ids, o.ID)
		}
		return ids
	}
	if err := ed.ReorderObject("crate", 0); err != nil {
		t.Fatalf("ReorderObject: %v", err)
	}
	if got := order(""); !reflect.DeepEqual(got, []string{"crate", "hero", "ship"}) {
		t.Fatalf("after moving the crate first: %v", got)
	}
	if err := ed.ReorderObject("crate", -1); err != nil {
		t.Fatalf("ReorderObject last: %v", err)
	}
	if got := order(""); !reflect.DeepEqual(got, []string{"hero", "ship", "crate"}) {
		t.Fatalf("after moving the crate last: %v", got)
	}
	n := ed.History.Index()
	if err := ed.ReorderObject("crate", 2); err != nil || ed.History.Index() != n {
		t.Fatalf("a reorder that changes nothing: err %v, history %d -> %d", err, n, ed.History.Index())
	}
	// Dropping before a later sibling of the same parent lands just before it.
	if err := ed.DropObject("hero", "crate", editor.DropBefore); err != nil {
		t.Fatalf("DropObject before: %v", err)
	}
	if got := order(""); !reflect.DeepEqual(got, []string{"ship", "hero", "crate"}) {
		t.Fatalf("after dropping hero before crate: %v", got)
	}
	if err := ed.DropObject("crate", "ship", editor.DropBefore); err != nil {
		t.Fatalf("DropObject before an earlier sibling: %v", err)
	}
	if got := order(""); !reflect.DeepEqual(got, []string{"crate", "ship", "hero"}) {
		t.Fatalf("after dropping crate before ship: %v", got)
	}
	if err := ed.DropObject("crate", "turret", editor.DropAfter); err != nil {
		t.Fatalf("DropObject after a child: %v", err)
	}
	if got := order("ship"); !reflect.DeepEqual(got, []string{"turret", "crate"}) {
		t.Fatalf("ship's children after dropping crate after turret: %v", got)
	}
	if err := ed.DropObject("hero", "crate", editor.DropInto); err != nil {
		t.Fatalf("DropObject into: %v", err)
	}
	if parentOf(ed, "hero") != "crate" {
		t.Fatalf("hero's parent after dropping it into the crate = %q", parentOf(ed, "hero"))
	}
	if err := ed.DropObject("ship", "hero", editor.DropAfter); err == nil {
		t.Fatal("dropping the ship beside its own descendant was not refused")
	}
}

func TestSetObjectIDRewritesConnectionsAndFollowsTheSession(t *testing.T) {
	ed := newNestedEditor(t)
	ed.Doc.Connections = []sceneformat.ConnectionDoc{
		{From: "barrel", Event: "fired", To: "crate", Handler: "break"},
		{From: "crate", Event: "broke", To: "barrel", Handler: "reload"},
		{From: "crate", Event: "broke", To: "hero", Handler: "cheer"},
	}
	ed.Rebuild()
	ed.SelectObject("barrel")
	if err := ed.SetObjectHidden("barrel", true); err != nil {
		t.Fatalf("SetObjectHidden: %v", err)
	}
	before := mustSave(t, ed)

	for _, bad := range []struct{ id, want string }{
		{"", "must not be empty"},
		{"crate", `duplicate object id "crate"`},
		{"turret", `duplicate object id "turret"`},
	} {
		if err := ed.SetObjectID("barrel", bad.id); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("SetObjectID(barrel, %q): err = %v, want %q", bad.id, err, bad.want)
		}
	}
	if err := ed.SetObjectID("nobody", "x"); err == nil {
		t.Error("changing the ID of a missing object was not refused")
	}

	if err := ed.SetObjectID("barrel", "cannon"); err != nil {
		t.Fatalf("SetObjectID: %v", err)
	}
	want := []sceneformat.ConnectionDoc{
		{From: "cannon", Event: "fired", To: "crate", Handler: "break"},
		{From: "crate", Event: "broke", To: "cannon", Handler: "reload"},
		{From: "crate", Event: "broke", To: "hero", Handler: "cheer"},
	}
	if !reflect.DeepEqual(ed.Doc.Connections, want) {
		t.Fatalf("connections = %+v, want %+v", ed.Doc.Connections, want)
	}
	if ed.SelectedID != "cannon" || !ed.ObjectHidden("cannon") || ed.ObjectHidden("barrel") {
		t.Fatalf("selection %q, hidden cannon %v, hidden barrel %v", ed.SelectedID, ed.ObjectHidden("cannon"), ed.ObjectHidden("barrel"))
	}
	if obj, _, _ := ed.FindObject("cannon"); obj == nil || obj.Name != "Barrel" {
		t.Fatalf("FindObject(cannon) = %v", obj)
	}
	if !ed.Undo() {
		t.Fatal("Undo failed")
	}
	if after := mustSave(t, ed); !bytes.Equal(after, before) {
		t.Fatalf("Undo did not restore the scene:\n%s\nwant:\n%s", after, before)
	}
	if ed.SelectedID != "barrel" || !ed.ObjectHidden("barrel") {
		t.Fatalf("after Undo: selection %q, hidden barrel %v", ed.SelectedID, ed.ObjectHidden("barrel"))
	}
}

func TestSetObjectIDRenamesTheObjectsOfAnInstanceAndOverrides(t *testing.T) {
	doc := testSceneDoc()
	resolver := sceneFiles{AtlasMap: testResolver(), files: map[string]string{"scenes/slime.scene.yaml": slimeScene}}
	ed, err := editor.New(doc, resolver)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	inst, err := ed.PlaceScene("actors", "scenes/slime.scene.yaml", 0, 0)
	if err != nil {
		t.Fatalf("PlaceScene: %v", err)
	}
	ed.Doc.Connections = []sceneformat.ConnectionDoc{
		{From: inst + "/body", Event: "hit", To: "hero", Handler: "hurt"},
		{From: "hero", Event: "hit", To: inst, Handler: "split"},
	}
	ed.Rebuild()
	// The instanced objects' IDs are taken too.
	if err := ed.SetObjectID("hero", inst+"/body"); err == nil {
		t.Fatal("taking the ID of an instanced object was not refused")
	}
	if err := ed.SetObjectID(inst, "boss"); err != nil {
		t.Fatalf("SetObjectID: %v", err)
	}
	want := []sceneformat.ConnectionDoc{
		{From: "boss/body", Event: "hit", To: "hero", Handler: "hurt"},
		{From: "hero", Event: "hit", To: "boss", Handler: "split"},
	}
	if !reflect.DeepEqual(ed.Doc.Connections, want) {
		t.Fatalf("connections = %+v, want %+v", ed.Doc.Connections, want)
	}
	if ed.BuildErr != nil || ed.Built.Object("boss/body") == nil {
		t.Fatalf("the renamed instance does not build its objects under the new ID (%v)", ed.BuildErr)
	}

	// A scene that inherits another overrides the base's objects by ID: renaming one of its own objects leaves those overrides alone, and the base's IDs are taken.
	base := `version: 2
layers:
  - name: actors
    objects:
      - id: gate
        x: 1
`
	child := &sceneformat.SceneDoc{Version: sceneformat.CurrentVersion, Inherits: "scenes/base.scene.yaml",
		Overrides: map[string]sceneformat.ObjectOverride{"gate": {X: sceneformat.Float32(9)}},
		Layers:    []sceneformat.LayerDoc{{Name: "extra", Objects: []sceneformat.ObjectDoc{{ID: "lever"}}}}}
	inherit, err := editor.New(child, sceneFiles{AtlasMap: testResolver(), files: map[string]string{"scenes/base.scene.yaml": base}})
	if err != nil {
		t.Fatalf("editor.New inheriting: %v", err)
	}
	if err := inherit.SetObjectID("lever", "gate"); err == nil {
		t.Fatal("taking the ID of an inherited object was not refused")
	}
	if err := inherit.SetObjectID("lever", "switch"); err != nil {
		t.Fatalf("SetObjectID in an inheriting scene: %v", err)
	}
	if _, ok := inherit.Doc.Overrides["gate"]; !ok {
		t.Fatal("an override of an inherited object was renamed")
	}
}

func TestHiddenAndLockedObjectsInTheViewports(t *testing.T) {
	ed := newNestedEditor(t)
	saved := mustSave(t, ed)
	// hero's sprite covers (32, 32) to (48, 48).
	if got := ed.HitTest(40, 40); got != "hero" {
		t.Fatalf("HitTest(hero) = %q", got)
	}
	if err := ed.SetObjectLocked("hero", true); err != nil {
		t.Fatalf("SetObjectLocked: %v", err)
	}
	if got := ed.HitTest(40, 40); got != "" {
		t.Fatalf("HitTest on a locked object = %q", got)
	}
	if err := ed.SetObjectLocked("hero", false); err != nil {
		t.Fatalf("SetObjectLocked off: %v", err)
	}
	if err := ed.SetObjectHidden("hero", true); err != nil {
		t.Fatalf("SetObjectHidden: %v", err)
	}
	if got := ed.HitTest(40, 40); got != "" {
		t.Fatalf("HitTest on a hidden object = %q", got)
	}
	// Locking a parent locks its descendants: the barrel under the locked ship cannot be picked.
	if got := ed.HitTest(80, 45); got != "barrel" {
		t.Fatalf("HitTest(barrel) = %q", got)
	}
	if err := ed.SetObjectLocked("ship", true); err != nil {
		t.Fatalf("SetObjectLocked ship: %v", err)
	}
	if got := ed.HitTest(80, 45); got != "" {
		t.Fatalf("HitTest on a child of a locked object = %q", got)
	}
	if !ed.ObjectLocked("ship") || ed.ObjectLocked("barrel") || !ed.ObjectHidden("hero") {
		t.Fatal("ObjectLocked/ObjectHidden do not report the objects' own toggles")
	}
	if ed.History.CanUndo() {
		t.Fatal("a viewport toggle reached the undo history")
	}
	if after := mustSave(t, ed); !bytes.Equal(after, saved) {
		t.Fatalf("a viewport toggle reached the scene file:\n%s", after)
	}
	if err := ed.SetObjectHidden("nobody", true); err == nil {
		t.Fatal("hiding a missing object was not refused")
	}
	if err := ed.OpenYAML(saved); err != nil {
		t.Fatalf("OpenYAML: %v", err)
	}
	if ed.ObjectHidden("hero") || ed.ObjectLocked("ship") {
		t.Fatal("opening a scene kept the last one's toggles")
	}
}

func TestViewport3DListsDrawsAndPicksNestedChildren(t *testing.T) {
	ed := newNestedEditor(t)
	ed.SetViewMode(editor.ViewMode3D)
	refs := ed.Objects3D()
	var ids, parents []string
	for _, r := range refs {
		ids = append(ids, r.ID)
		parents = append(parents, r.ParentID)
	}
	if want := []string{"hero", "ship", "turret", "barrel", "crate"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("Objects3D = %v, want %v", ids, want)
	}
	if want := []string{"", "", "ship", "turret", ""}; !reflect.DeepEqual(parents, want) {
		t.Fatalf("Objects3D parents = %v, want %v", parents, want)
	}
	if refs[3].ObjectIndex != 0 || refs[3].LayerIndex != 1 {
		t.Fatalf("barrel ref = %+v", refs[3])
	}

	// Move the ship out of the way of the others along z, so its subtree stands alone, then pick the barrel at its world position.
	if err := ed.MoveObject3D("ship", 0, 0, 200); err != nil {
		t.Fatalf("MoveObject3D: %v", err)
	}
	if err := ed.MoveObject3D("turret", 0, 0, 0); err != nil {
		t.Fatalf("MoveObject3D turret: %v", err)
	}
	if err := ed.MoveObject3D("barrel", 60, 0, 0); err != nil {
		t.Fatalf("MoveObject3D barrel: %v", err)
	}
	barrel, _, _ := ed.FindObject("barrel")
	corners := ed.ObjectCorners3D(barrel)
	var center [3]float32
	for _, c := range corners {
		for a := range 3 {
			center[a] += c[a] / 8
		}
	}
	if math.Abs(float64(center[0]-60)) > 1e-3 || math.Abs(float64(center[2]-200)) > 1e-3 {
		t.Fatalf("barrel's 3D box is centered at %v, want (60, 0, 200)", center)
	}
	ed.Camera3D.Target = [3]float32{60, 0, 200}
	ed.Camera3D.Distance = 120
	vw, vh := ed.ViewportSize()
	sx, sy, _, ok := ed.Camera3D.Project(center, vw, vh)
	if !ok {
		t.Fatal("the barrel is not in view")
	}
	if id, ok := ed.Pick3DObject(sx, sy, vw, vh); !ok || id != "barrel" {
		t.Fatalf("Pick3DObject at the barrel = %q, %v", id, ok)
	}
	if err := ed.SetObjectLocked("turret", true); err != nil {
		t.Fatalf("SetObjectLocked: %v", err)
	}
	if id, ok := ed.Pick3DObject(sx, sy, vw, vh); ok && id == "barrel" {
		t.Fatal("picked the barrel under a locked turret")
	}
	if err := ed.SetObjectLocked("turret", false); err != nil {
		t.Fatalf("SetObjectLocked off: %v", err)
	}
	if err := ed.SetObjectHidden("barrel", true); err != nil {
		t.Fatalf("SetObjectHidden: %v", err)
	}
	if id, ok := ed.Pick3DObject(sx, sy, vw, vh); ok && id == "barrel" {
		t.Fatal("picked a hidden barrel")
	}
}

func TestGizmoTranslateMovesANestedChildAlongTheWorldAxis(t *testing.T) {
	ed := newNestedEditor(t)
	ed.SetViewMode(editor.ViewMode3D)
	// The ship is turned a quarter about z, so the world x axis is the turret's -y.
	if err := ed.RotateObject3D("ship", 0, 0, math.Pi/2); err != nil {
		t.Fatalf("RotateObject3D: %v", err)
	}
	ed.SelectObject("barrel")
	barrel, _, _ := ed.FindObject("barrel")
	start := center3D(ed.ObjectCorners3D(barrel))
	ed.Camera3D.Target = start
	vw, vh := ed.ViewportSize()
	sx, sy, _, ok := ed.Camera3D.Project(start, vw, vh)
	if !ok {
		t.Fatal("the barrel is not in view")
	}
	if !ed.BeginGizmoDragAxis3D(editor.Axis3DX, sx, sy, vw, vh) {
		t.Fatal("BeginGizmoDragAxis3D failed")
	}
	ed.UpdateGizmoDrag3D(sx+30, sy)
	if !ed.EndGizmoDrag3D() {
		t.Fatal("the drag committed nothing")
	}
	barrel, _, _ = ed.FindObject("barrel")
	end := center3D(ed.ObjectCorners3D(barrel))
	if math.Abs(float64(end[0]-start[0])) < 1 || math.Abs(float64(end[1]-start[1])) > 1e-3 || math.Abs(float64(end[2]-start[2])) > 1e-3 {
		t.Fatalf("the barrel moved from %v to %v, want along world x only", start, end)
	}
	if math.Abs(float64(barrel.X-5)) > 1e-4 {
		t.Fatalf("the barrel's local x changed to %v; the world x axis is its parent's y", barrel.X)
	}
}

func center3D(corners [8][3]float32) [3]float32 {
	var c [3]float32
	for _, p := range corners {
		for a := range 3 {
			c[a] += p[a] / 8
		}
	}
	return c
}
