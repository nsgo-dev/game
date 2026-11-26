package editor_test

import (
	"bytes"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// layerNames returns the names Layers lists, in draw order.
func layerNames(ed *editor.Editor) []string {
	var out []string
	for _, l := range ed.Layers() {
		out = append(out, l.Name)
	}
	return out
}

// A new scene gets a second layer from the panel, an object placed after selecting it lands on it, and the saved file lists both layers in the order chosen in the panel.
func TestLayersPanelAddsALayerThatTakesPlacedObjects(t *testing.T) {
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	app := startEditorView(t, ed)
	openLayersPanelInUI(app, ed)

	app.Press("id:layers-add")
	if got := layerNames(ed); !slices.Equal(got, []string{"main", "layer_2"}) {
		t.Fatalf("layers after Add = %v", got)
	}
	if ed.ActiveLayer != "layer_2" {
		t.Fatalf("the added layer is not active: %q", ed.ActiveLayer)
	}
	app.RightClick("id:layers-row-layer_2")
	app.Press("role:menuitem Rename")
	app.Clear("id:layers-rename-layer_2")
	app.Type("id:layers-rename-layer_2", "actors\n")
	if got := layerNames(ed); !slices.Equal(got, []string{"main", "actors"}) {
		t.Fatalf("layers after the rename = %v (status %q)", got, ed.Status)
	}

	app.Press("id:layers-row-main")
	if ed.ActiveLayer != "main" {
		t.Fatalf("pressing main made %q active", ed.ActiveLayer)
	}
	if got := allText(app.Find("id:status-active-layer")); !strings.Contains(got, "Layer main") {
		t.Fatalf("the status bar reads %q, want Layer main", got)
	}
	app.Press("id:layers-row-actors")
	if ed.ActiveLayer != "actors" {
		t.Fatalf("pressing actors made %q active", ed.ActiveLayer)
	}
	if got := allText(app.Find("id:status-active-layer")); !strings.Contains(got, "Layer actors") {
		t.Fatalf("the status bar reads %q, want Layer actors", got)
	}

	runEditorCommand(t, app, ed, editor.CmdToolPlace)
	px, py := ed.WorldToCanvas(100, 60)
	app.Touch("id:editor-viewport", core.Point{X: core.Points(px), Y: core.Points(py)}).Lift()
	if ed.SelectedID == "" {
		t.Fatalf("the press placed nothing (status %q)", ed.Status)
	}
	if _, layer := ed.SelectedObject(); layer != "actors" {
		t.Fatalf("the object landed on %q, want actors", layer)
	}

	// The actors layer goes under main: it draws first, so it is first in the file.
	app.RightClick("id:layers-row-actors")
	app.Press("role:menuitem Move Layer Up")
	if got := layerNames(ed); !slices.Equal(got, []string{"actors", "main"}) {
		t.Fatalf("layers after Move Layer Up = %v", got)
	}

	saved := mustSave(t, ed)
	doc, err := sceneformat.Parse(saved)
	if err != nil {
		t.Fatalf("Parse saved: %v", err)
	}
	if len(doc.Layers) != 2 || doc.Layers[0].Name != "actors" || doc.Layers[1].Name != "main" {
		t.Fatalf("saved layers = %+v", doc.Layers)
	}
	if len(doc.Layers[0].Objects) != 1 || doc.Layers[0].Objects[0].ID != ed.SelectedID || len(doc.Layers[1].Objects) != 0 {
		t.Fatalf("saved objects: actors %+v, main %+v", doc.Layers[0].Objects, doc.Layers[1].Objects)
	}
	if again, err := sceneformat.Encode(doc); err != nil || !bytes.Equal(again, saved) {
		t.Fatalf("the saved scene does not round-trip (%v)", err)
	}
}

// Hiding and showing a layer, and removing it and undoing the removal, give back the scene byte for byte; the removal takes the connections of its objects with it.
func TestLayersPanelHideShowAndRemoveRoundTrip(t *testing.T) {
	ed := newNestedEditor(t)
	ed.Doc.Connections = []sceneformat.ConnectionDoc{
		{From: "hero", Event: "pressed", To: "ship", Handler: "open"},
		{From: "barrel", Event: "fired", To: "hero", Handler: "hurt"},
	}
	ed.History.Clear()
	opened := mustSave(t, ed)
	app := startEditorView(t, ed)
	openLayersPanelInUI(app, ed)

	app.Press("id:layers-eye-actors")
	if info := layerInfoOf(t, ed, "actors"); info.Visible {
		t.Fatal("the eye button left the layer visible")
	}
	if hidden := mustSave(t, ed); !bytes.Contains(hidden, []byte("visible: false")) {
		t.Fatalf("hiding the layer did not write visible: false:\n%s", hidden)
	}
	if got := allText(app.Find("id:status-active-layer")); !strings.Contains(got, "Layer actors (hidden)") {
		t.Fatalf("the status bar reads %q, want the active layer marked hidden", got)
	}
	app.Press("id:layers-eye-actors")
	if shown := mustSave(t, ed); !bytes.Equal(shown, opened) {
		t.Fatalf("hiding and showing the layer changed the scene:\n%s\nwant:\n%s", shown, opened)
	}

	app.RightClick("id:layers-row-actors")
	app.Press("role:menuitem Remove Layer")
	if got := layerNames(ed); !slices.Equal(got, []string{"floor", "ui"}) {
		t.Fatalf("layers after Remove = %v", got)
	}
	if len(ed.Doc.Connections) != 0 {
		t.Fatalf("the removal kept connections of the layer's objects: %+v", ed.Doc.Connections)
	}
	if ed.ActiveLayer != "ui" {
		t.Fatalf("after removing the active layer %q is active, want the next one, ui", ed.ActiveLayer)
	}
	if app.Exists("id:scene-tree-layer-actors") {
		t.Fatal("the scene tree still lists the removed layer")
	}
	removed := mustSave(t, ed)

	undoInUI(app, ed)
	if after := mustSave(t, ed); !bytes.Equal(after, opened) {
		t.Fatalf("Undo did not restore the removed layer:\n%s\nwant:\n%s", after, opened)
	}
	if ed.ActiveLayer != "actors" {
		t.Fatalf("Undo left %q active, want actors", ed.ActiveLayer)
	}
	redoInUI(app, ed)
	if after := mustSave(t, ed); !bytes.Equal(after, removed) {
		t.Fatalf("Redo did not remove the layer again:\n%s\nwant:\n%s", after, removed)
	}
	for ed.History.CanUndo() {
		undoInUI(app, ed)
	}
	if after := mustSave(t, ed); !bytes.Equal(after, opened) {
		t.Fatalf("undoing every edit did not restore the scene:\n%s\nwant:\n%s", after, opened)
	}
}

func layerInfoOf(t *testing.T, ed *editor.Editor, name string) editor.LayerInfo {
	t.Helper()
	for _, l := range ed.Layers() {
		if l.Name == name {
			return l
		}
	}
	t.Fatalf("no layer %q in %v", name, layerNames(ed))
	return editor.LayerInfo{}
}

// Dragging a layer's row onto the top half of another's puts it before that layer in the draw order, and the space items of the menu set its space.
func TestLayersPanelDragReordersAndMenuSetsSpace(t *testing.T) {
	ed := newNestedEditor(t)
	opened := mustSave(t, ed)
	app := startEditorView(t, ed)
	openLayersPanelInUI(app, ed)

	drag(app, "id:layers-name-ui", "id:layers-row-floor", 0.1)
	if got := layerNames(ed); !slices.Equal(got, []string{"ui", "floor", "actors"}) {
		t.Fatalf("layers after dropping ui before floor = %v (status %q)", got, ed.Status)
	}
	drag(app, "id:layers-name-ui", "id:layers-row-actors", 0.9)
	if got := layerNames(ed); !slices.Equal(got, []string{"floor", "actors", "ui"}) {
		t.Fatalf("layers after dropping ui after actors = %v (status %q)", got, ed.Status)
	}
	app.RightClick("id:layers-row-floor")
	app.Press("role:menuitem Screen Space")
	if info := layerInfoOf(t, ed, "floor"); info.Space != sceneformat.SpaceScreen {
		t.Fatalf("floor's space after Screen Space = %q", info.Space)
	}
	if got := app.Find("id:layers-meta-floor").Text; got != "screen · tiles 4x4" {
		t.Fatalf("floor's row reads %q", got)
	}
	for ed.History.CanUndo() {
		undoInUI(app, ed)
	}
	if after := mustSave(t, ed); !bytes.Equal(after, opened) {
		t.Fatalf("undoing the drags and the space did not restore the scene:\n%s\nwant:\n%s", after, opened)
	}
}

// A locked layer keeps its objects from being picked or moved in the viewport and its tiles from being painted there, without touching the scene file or the history.
func TestLockedLayerIsNotPickedOrPainted(t *testing.T) {
	ed := newNestedEditor(t)
	opened := mustSave(t, ed)
	app := startEditorView(t, ed)
	openLayersPanelInUI(app, ed)

	app.Press("id:layers-lock-actors")
	if !ed.LayerLocked("actors") {
		t.Fatal("the lock button did not lock the layer")
	}
	if ed.History.CanUndo() || !bytes.Equal(mustSave(t, ed), opened) {
		t.Fatal("the lock reached the scene file or the history")
	}
	hx, hy := ed.WorldToCanvas(40, 40)
	if got := ed.HitTest(hx, hy); got != "" {
		t.Fatalf("HitTest picked %q on the locked layer", got)
	}
	if !ed.SelectObject("hero") {
		t.Fatal("SelectObject hero")
	}
	ed.SetTool(editor.ToolMove)
	mx, my := ed.WorldToCanvas(90, 90)
	ed.HandleCanvasPress(mx, my)
	if obj, _, _ := ed.FindObject("hero"); obj.X != 40 || obj.Y != 40 {
		t.Fatalf("the Move tool moved hero on the locked layer to (%v, %v)", obj.X, obj.Y)
	}
	ed.SetTool(editor.ToolPlace)
	ed.HandleCanvasPress(mx, my)
	if !strings.Contains(ed.Status, `layer "actors" is locked`) {
		t.Fatalf("placing on the locked layer: status %q", ed.Status)
	}

	app.Press("id:layers-row-floor")
	app.Press("id:layers-lock-floor")
	runEditorCommand(t, app, ed, editor.CmdToolPaintTile)
	tx, ty := ed.WorldToCanvas(24, 24)
	paint := func() {
		app.Touch("id:editor-viewport", core.Point{X: core.Points(tx), Y: core.Points(ty)}).Lift()
	}
	paint()
	if !bytes.Equal(mustSave(t, ed), opened) || ed.History.CanUndo() {
		t.Fatal("tools changed the scene on locked layers")
	}
	if got := allText(app.Find("id:status-active-layer")); !strings.Contains(got, "Layer floor (locked)") {
		t.Fatalf("the status bar reads %q, want the active layer marked locked", got)
	}
	app.Press("id:layers-lock-floor")
	paint()
	if bytes.Equal(mustSave(t, ed), opened) {
		t.Fatal("painting the unlocked layer changed nothing")
	}
	if err := ed.SetLayerLocked("nowhere", true); err == nil {
		t.Fatal("SetLayerLocked accepted a layer the scene does not have")
	}
}

func TestLayerEditsRefuseBadNamesAndUndo(t *testing.T) {
	ed := newNestedEditor(t)
	opened := mustSave(t, ed)

	for _, name := range []string{"", "actors"} {
		if err := ed.AddLayer(name, sceneformat.SpaceWorld); err == nil {
			t.Fatalf("AddLayer(%q) was accepted", name)
		}
		if err := ed.RenameLayer("floor", name); err == nil {
			t.Fatalf("RenameLayer(floor, %q) was accepted", name)
		}
	}
	if err := ed.AddLayer("fx", "sky"); err == nil {
		t.Fatal("AddLayer accepted an unknown space")
	}
	if err := ed.SetLayerSpace("floor", "sky"); err == nil {
		t.Fatal("SetLayerSpace accepted an unknown space")
	}
	for _, err := range []error{ed.RemoveLayer("nowhere"), ed.MoveLayer("nowhere", 0), ed.SetLayerVisible("nowhere", false), ed.RenameLayer("nowhere", "x")} {
		if err == nil {
			t.Fatal("an edit of a layer the scene does not have was accepted")
		}
	}
	if ed.History.CanUndo() {
		t.Fatal("a refused edit reached the history")
	}

	if err := ed.SetLayerLocked("actors", true); err != nil {
		t.Fatalf("SetLayerLocked: %v", err)
	}
	if err := ed.RenameLayer("actors", "cast"); err != nil {
		t.Fatalf("RenameLayer: %v", err)
	}
	if ed.ActiveLayer != "cast" || !ed.LayerLocked("cast") || ed.LayerLocked("actors") {
		t.Fatalf("the rename left active %q, lock on cast %v", ed.ActiveLayer, ed.LayerLocked("cast"))
	}
	if _, layer := mustFindLayer(t, ed, "hero"); layer != "cast" {
		t.Fatalf("hero is on %q after the rename", layer)
	}
	if err := ed.AddLayer("fx", sceneformat.SpaceScreen); err != nil {
		t.Fatalf("AddLayer: %v", err)
	}
	if got := layerNames(ed); !slices.Equal(got, []string{"floor", "cast", "ui", "fx"}) {
		t.Fatalf("layers after AddLayer = %v", got)
	}
	if info := layerInfoOf(t, ed, "fx"); info.Order != 20 || info.Space != sceneformat.SpaceScreen || info.Active {
		t.Fatalf("the added layer = %+v, want order 20, screen space, not active", info)
	}
	ed.SetActiveLayer("fx")
	if !ed.Undo() || ed.ActiveLayer != "floor" {
		t.Fatalf("undoing the addition of the active layer left %q active, want the first layer", ed.ActiveLayer)
	}
	if !ed.Undo() || ed.LayerLocked("cast") || !ed.LayerLocked("actors") {
		t.Fatal("undoing the rename did not move the lock back")
	}
	if after := mustSave(t, ed); !bytes.Equal(after, opened) {
		t.Fatalf("undoing the edits did not restore the scene:\n%s\nwant:\n%s", after, opened)
	}

	// A scene's last layer can go too, which leaves no active layer.
	for _, name := range []string{"floor", "actors", "ui"} {
		if err := ed.RemoveLayer(name); err != nil {
			t.Fatalf("RemoveLayer(%s): %v", name, err)
		}
	}
	if ed.ActiveLayer != "" || len(ed.Layers()) != 0 {
		t.Fatalf("after removing every layer: active %q, layers %v", ed.ActiveLayer, layerNames(ed))
	}
	for ed.Undo() {
	}
	if after := mustSave(t, ed); !bytes.Equal(after, opened) {
		t.Fatalf("undoing the removals did not restore the scene:\n%s\nwant:\n%s", after, opened)
	}
}

func mustFindLayer(t *testing.T, ed *editor.Editor, id string) (*sceneformat.ObjectDoc, string) {
	t.Helper()
	obj, li, _ := ed.FindObject(id)
	if obj == nil {
		t.Fatalf("no object %q", id)
	}
	return obj, ed.Doc.Layers[li].Name
}

const baseLayersScene = `version: 2
layers:
  - name: sky
    order: -10
  - name: ground
    objects:
      - id: rock
`

// In a scene that inherits another, the layers that extend the base's keep the name, order, space and visibility the base gives them, and a new layer may not take a base layer's name.
func TestLayerEditsInAnInheritingScene(t *testing.T) {
	resolver := sceneFiles{AtlasMap: testResolver(), files: map[string]string{"scenes/base.scene.yaml": baseLayersScene}}
	doc, err := sceneformat.Parse([]byte(`version: 2
inherits: scenes/base.scene.yaml
layerOverrides:
  sky:
    visible: false
layers:
  - name: ground
    objects:
      - id: tree
  - name: hud
    space: screen
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ed, err := editor.New(doc, resolver)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if ed.BuildErr != nil {
		t.Fatalf("build: %v", ed.BuildErr)
	}
	opened := mustSave(t, ed)
	if got := layerNames(ed); !slices.Equal(got, []string{"ground", "hud"}) {
		t.Fatalf("Layers = %v, want the scene's own layers", got)
	}
	if info := layerInfoOf(t, ed, "ground"); !info.Inherited || info.Objects != 1 {
		t.Fatalf("ground = %+v, want inherited with its one own object", info)
	}
	if info := layerInfoOf(t, ed, "hud"); info.Inherited || info.Space != sceneformat.SpaceScreen {
		t.Fatalf("hud = %+v", info)
	}
	for what, err := range map[string]error{
		"rename ground": ed.RenameLayer("ground", "floor"),
		"rename to sky": ed.RenameLayer("hud", "sky"),
		"add sky":       ed.AddLayer("sky", sceneformat.SpaceWorld),
		"move ground":   ed.MoveLayer("ground", 1),
	} {
		if err == nil {
			t.Errorf("%s was accepted", what)
		}
	}
	if err := ed.SetLayerVisible("ground", false); err != nil {
		t.Fatalf("SetLayerVisible on inherited ground should write layerOverrides: %v", err)
	}
	if err := ed.SetLayerSpace("ground", sceneformat.SpaceScreen); err != nil {
		t.Fatalf("SetLayerSpace on inherited ground should write layerOverrides: %v", err)
	}
	if lo, ok := ed.Doc.LayerOverrides["ground"]; !ok || lo.Visible == nil || *lo.Visible || lo.Space != sceneformat.SpaceScreen {
		t.Fatalf("LayerOverrides[ground] = %+v, want visible=false space=screen", lo)
	}
	if err := ed.RevertLayerOverrides("ground"); err != nil {
		t.Fatalf("RevertLayerOverrides: %v", err)
	}
	if _, hasGround := ed.Doc.LayerOverrides["ground"]; hasGround {
		t.Fatalf("LayerOverrides after revert = %+v, want ground removed", ed.Doc.LayerOverrides)
	}
	if err := ed.RenameLayer("hud", "overlay"); err != nil {
		t.Fatalf("RenameLayer of the scene's own layer: %v", err)
	}
	if err := ed.SetLayerVisible("overlay", false); err != nil {
		t.Fatalf("SetLayerVisible of the scene's own layer: %v", err)
	}
	if err := ed.RemoveLayer("ground"); err != nil {
		t.Fatalf("RemoveLayer of the extending layer: %v", err)
	}
	if ed.BuildErr != nil {
		t.Fatalf("build after the edits: %v", ed.BuildErr)
	}
	for ed.Undo() {
	}
	if after := mustSave(t, ed); !bytes.Equal(after, opened) {
		t.Fatalf("undoing the edits did not restore the scene:\n%s\nwant:\n%s", after, opened)
	}
}

// moveLayerDoc returns a scene of len(orders) layers named "a", "b", ..., each with the order orders gives it.
func moveLayerDoc(orders []int) *sceneformat.SceneDoc {
	doc := &sceneformat.SceneDoc{Version: sceneformat.CurrentVersion}
	for i, o := range orders {
		doc.Layers = append(doc.Layers, sceneformat.LayerDoc{Name: string(rune('a' + i)), Order: o})
	}
	return doc
}

// checkMoveLayer moves the layer m of the scene moveLayerDoc(orders) makes to position to of the draw order and checks that it draws there, that the other layers keep their draw order and their orders, and that Undo restores the file byte for byte.
func checkMoveLayer(t *testing.T, orders []int, m, to int) {
	t.Helper()
	ed, err := editor.New(moveLayerDoc(orders), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	before := mustSave(t, ed)
	names := layerNames(ed)
	name := string(rune('a' + m))
	want := slices.DeleteFunc(slices.Clone(names), func(n string) bool { return n == name })
	clamped := max(0, min(to, len(names)-1))
	want = slices.Insert(want, clamped, name)
	oldOrders := map[string]int{}
	for _, l := range ed.Layers() {
		oldOrders[l.Name] = l.Order
	}

	if err := ed.MoveLayer(name, to); err != nil {
		t.Fatalf("MoveLayer(%s, %d) with orders %v: %v", name, to, orders, err)
	}
	if got := layerNames(ed); !slices.Equal(got, want) {
		t.Fatalf("orders %v: MoveLayer(%s, %d) drew %v, want %v", orders, name, to, got, want)
	}
	for _, l := range ed.Layers() {
		if l.Name != name && l.Order != oldOrders[l.Name] {
			t.Fatalf("orders %v: MoveLayer(%s, %d) changed the order of %s", orders, name, to, l.Name)
		}
	}
	if slices.Equal(want, names) {
		if ed.History.CanUndo() {
			t.Fatalf("a move to where the layer is reached the history")
		}
		return
	}
	if !ed.Undo() {
		t.Fatal("nothing to undo after a move")
	}
	if after := mustSave(t, ed); !bytes.Equal(after, before) {
		t.Fatalf("orders %v: undoing MoveLayer(%s, %d) gave\n%s\nwant\n%s", orders, name, to, after, before)
	}
}

func TestMoveLayerKeepsTheOtherLayersInPlace(t *testing.T) {
	for _, tc := range []struct {
		orders []int
		m, to  int
	}{
		{[]int{0, 0, 0}, 2, 0},
		{[]int{0, 0, 0}, 0, 2},
		{[]int{0, 10, 20}, 2, 0},
		{[]int{0, 10, 20}, 0, 1},
		{[]int{20, 10, 0}, 0, 0},
		{[]int{5, 0, 5, 0}, 1, 3},
		{[]int{5, 0, 5, 0}, 2, 0},
		{[]int{1, 2}, 0, 9},
		{[]int{1, 2}, 1, -3},
		{[]int{3}, 0, 0},
	} {
		checkMoveLayer(t, tc.orders, tc.m, tc.to)
	}
}

func FuzzMoveLayer(f *testing.F) {
	f.Add([]byte{0, 0, 0}, uint8(2), int8(0))
	f.Add([]byte{0, 10, 20, 10}, uint8(1), int8(3))
	f.Add([]byte{255, 3, 3, 0, 9}, uint8(4), int8(1))
	f.Fuzz(func(t *testing.T, raw []byte, m uint8, to int8) {
		if len(raw) == 0 || len(raw) > 8 {
			return
		}
		orders := make([]int, len(raw))
		for i, b := range raw {
			// A small range of orders makes ties common.
			orders[i] = int(int8(b)) % 4
		}
		checkMoveLayer(t, orders, int(m)%len(orders), int(to))
	})
}

func TestLayersReportSpaceTilemapAndObjects(t *testing.T) {
	ed := newNestedEditor(t)
	if err := ed.SetLayerLocked("ui", true); err != nil {
		t.Fatalf("SetLayerLocked: %v", err)
	}
	actors := 0
	sceneformat.WalkObjects(ed.Doc.Layers[1].Objects, func(_, _ *sceneformat.ObjectDoc) bool { actors++; return true })
	got := ed.Layers()
	want := []editor.LayerInfo{
		{Name: "floor", Space: sceneformat.SpaceWorld, Visible: true, Tilemap: "tiles 4x4"},
		{Name: "actors", Space: sceneformat.SpaceWorld, Order: 10, Visible: true, Active: true, Objects: actors},
		{Name: "ui", Space: sceneformat.SpaceScreen, Order: 20, Visible: true, Locked: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Layers = %+v\nwant %+v", got, want)
	}
}

// Undoing a layer's addition takes its lock with it, so a later layer of the same name starts unlocked, and redoing the addition locks it again.
func TestUndoingALayerAdditionDropsItsLock(t *testing.T) {
	ed := newNestedEditor(t)
	if err := ed.AddLayer("extra", sceneformat.SpaceWorld); err != nil {
		t.Fatalf("AddLayer: %v", err)
	}
	if err := ed.SetLayerLocked("extra", true); err != nil {
		t.Fatalf("SetLayerLocked: %v", err)
	}
	ed.Undo()
	if ed.LayerLocked("extra") {
		t.Fatal("the undone layer's lock stayed behind")
	}
	ed.Redo()
	if !ed.LayerLocked("extra") {
		t.Fatal("redoing the addition did not lock the layer again")
	}
	ed.Undo()
	if err := ed.AddLayer("extra", sceneformat.SpaceWorld); err != nil {
		t.Fatalf("AddLayer again: %v", err)
	}
	if ed.LayerLocked("extra") || layerInfoOf(t, ed, "extra").Locked {
		t.Fatal("a new layer took the lock of an undone one of the same name")
	}
}

// Removing the active layer hands the active role to a layer that may paint another tileset, and the active tile follows, as SetActiveLayer keeps it; Undo brings back the layer and its tile.
func TestRemovingTheActiveLayerKeepsTheTileInThePalette(t *testing.T) {
	resolver := testResolver()
	resolver["small"] = &sprite.Atlas{
		Image:  core.ImageSource{URI: "asset:small.png"},
		Frames: map[string]sprite.Frame{"1": {Name: "1", W: 16, H: 16, Width: 16, Height: 16}},
	}
	doc := testSceneDoc()
	doc.Layers = slices.Insert(doc.Layers, 1, sceneformat.LayerDoc{
		Name:    "deco",
		Order:   5,
		Tilemap: &sceneformat.TilemapDoc{TileWidth: 16, TileHeight: 16, Width: 2, Height: 2, Tileset: "small", Data: []int{0, 0, 0, 0}},
	})
	ed, err := editor.New(doc, resolver)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetActiveLayer("floor")
	ed.SetActiveTileID(3)
	if err := ed.RemoveLayer("floor"); err != nil {
		t.Fatalf("RemoveLayer: %v", err)
	}
	if ed.ActiveLayer != "deco" || ed.ActiveTileID != 1 {
		t.Fatalf("after the removal the active layer is %q with tile %d, want deco with tile 1", ed.ActiveLayer, ed.ActiveTileID)
	}
	ed.Undo()
	if ed.ActiveLayer != "floor" || ed.ActiveTileID != 3 {
		t.Fatalf("after Undo the active layer is %q with tile %d, want floor with tile 3", ed.ActiveLayer, ed.ActiveTileID)
	}
}

// An object on a layer the scene does not draw is frozen in the viewports like one hidden in the scene tree: a selected one is not moved by the Move tool.
func TestHiddenLayerObjectsAreNotMovedInTheViewport(t *testing.T) {
	ed := newNestedEditor(t)
	if err := ed.SetLayerVisible("actors", false); err != nil {
		t.Fatalf("SetLayerVisible: %v", err)
	}
	if !ed.SelectObject("hero") {
		t.Fatal("SelectObject hero")
	}
	ed.SetTool(editor.ToolMove)
	mx, my := ed.WorldToCanvas(90, 90)
	ed.HandleCanvasPress(mx, my)
	if obj, _, _ := ed.FindObject("hero"); obj.X != 40 || obj.Y != 40 {
		t.Fatalf("the Move tool moved hero on the hidden layer to (%v, %v)", obj.X, obj.Y)
	}
	if err := ed.SetLayerVisible("actors", true); err != nil {
		t.Fatalf("SetLayerVisible: %v", err)
	}
	ed.HandleCanvasPress(mx, my)
	if obj, _, _ := ed.FindObject("hero"); obj.X == 40 && obj.Y == 40 {
		t.Fatal("the Move tool did not move hero once its layer was shown")
	}
}

// In a scene that inherits another, a new layer draws over the base's layers too, and the panel turns off the controls the base owns on a row that extends one of its layers.
func TestLayersPanelInAnInheritingScene(t *testing.T) {
	resolver := sceneFiles{AtlasMap: testResolver(), files: map[string]string{"scenes/base.scene.yaml": `version: 2
layers:
  - name: ground
  - name: fg
    order: 50
`}}
	doc, err := sceneformat.Parse([]byte(`version: 2
inherits: scenes/base.scene.yaml
layers:
  - name: ground
  - name: hud
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ed, err := editor.New(doc, resolver)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	app := startEditorView(t, ed)
	openLayersPanelInUI(app, ed)

	if !app.Enabled("id:layers-eye-ground") || !app.Enabled("id:layers-eye-hud") {
		t.Fatal("the eye button should be enabled for both inherited (via layerOverrides) and own layers")
	}
	app.RightClick("id:layers-row-ground")
	for _, id := range []string{"layers-rename-item-ground", "layers-down-ground"} {
		if app.Enabled("id:" + id) {
			t.Errorf("%s is enabled on a layer the base owns", id)
		}
	}
	for _, id := range []string{"layers-hide-ground", "layers-space-screen-ground"} {
		if !app.Enabled("id:" + id) {
			t.Errorf("%s should be enabled on an inherited layer via layerOverrides", id)
		}
	}
	app.Key("escape")

	app.Press("id:layers-add")
	added := ed.ActiveLayer
	if info := layerInfoOf(t, ed, added); info.Order != 50 || info.Inherited {
		t.Fatalf("the added layer = %+v, want order 50 to draw over the base's fg", info)
	}
	eff := ed.Built.Doc.Layers
	last := eff[0]
	for _, l := range eff {
		if l.Order >= last.Order {
			last = l
		}
	}
	if last.Name != added {
		t.Fatalf("the resolved scene draws %q last, want the added %q", last.Name, added)
	}
}
