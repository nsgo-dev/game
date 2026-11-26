package editor_test

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func clipboardComponentRegistry(t *testing.T) *sceneformat.ComponentRegistry {
	t.Helper()
	reg := sceneformat.NewComponentRegistry()
	if err := reg.Register(sceneformat.ComponentType{
		Name: "button",
		Fields: []sceneformat.Field{
			{Name: "label", Kind: sceneformat.FieldString, Default: "Press"},
		},
		Events: []sceneformat.Event{
			{Name: "pressed"},
		},
	}); err != nil {
		t.Fatalf("register button: %v", err)
	}
	if err := reg.Register(sceneformat.ComponentType{
		Name: "door",
		Fields: []sceneformat.Field{
			{Name: "locked", Kind: sceneformat.FieldBool, Default: false},
		},
		Handlers: []sceneformat.Handler{
			{
				Name: "open",
				Params: []sceneformat.Field{
					{Name: "speed", Kind: sceneformat.FieldInt, Default: 1},
				},
			},
		},
	}); err != nil {
		t.Fatalf("register door: %v", err)
	}
	return reg
}

// #868 Acceptance #1: Duplicating an object with children and a connection between two of them
// yields new unique IDs with the connection pointing at the copies, next to the originals, in one undo step.
func TestDuplicateObjectWithChildrenAndInternalConnectionInOneUndoStep(t *testing.T) {
	reg := clipboardComponentRegistry(t)
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Puzzle Room",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:   "unit",
						Name: "Door Unit",
						X:    100,
						Y:    50,
						Children: []sceneformat.ObjectDoc{
							{
								ID:         "btn",
								Name:       "Switch",
								X:          -12,
								Y:          4,
								Components: map[string]any{"button": map[string]any{"label": "Open"}},
								Properties: map[string]any{"tag": "trigger"},
							},
							{
								ID:         "gate",
								Name:       "Gate",
								X:          24,
								Y:          0,
								Components: map[string]any{"door": map[string]any{"locked": true}},
								Properties: map[string]any{"hp": 10},
							},
						},
					},
					{
						ID:         "outside_door",
						X:          300,
						Y:          50,
						Components: map[string]any{"door": map[string]any{"locked": false}},
					},
				},
			},
		},
		Connections: []sceneformat.ConnectionDoc{
			{
				From:    "btn",
				Event:   "pressed",
				To:      "gate",
				Handler: "open",
				Args:    map[string]any{"speed": 3},
				Once:    true,
			},
			{
				From:    "btn",
				Event:   "pressed",
				To:      "outside_door",
				Handler: "open",
			},
		},
	}

	ed, err := editor.New(doc, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.SetComponentRegistry(reg); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	ed.SelectObject("unit")
	beforeYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML before: %v", err)
	}

	dupID, err := ed.DuplicateSelected()
	if err != nil {
		t.Fatalf("DuplicateSelected: %v", err)
	}
	if ed.History.Len() != 1 {
		t.Fatalf("History.Len() after Duplicate = %d, want 1 (one undo step)", ed.History.Len())
	}
	if dupID == "" || dupID == "unit" {
		t.Fatalf("dupID = %q, want fresh unique ID", dupID)
	}
	if ed.SelectedID != dupID {
		t.Fatalf("SelectedID after Duplicate = %q, want %q", ed.SelectedID, dupID)
	}

	dup, _, _ := ed.FindObject(dupID)
	if dup == nil {
		t.Fatalf("duplicated object %q not found", dupID)
	}
	if dup.X <= 100 || dup.Y <= 50 {
		t.Fatalf("duplicated object position = (%v, %v), want offset next to (100, 50)", dup.X, dup.Y)
	}
	if len(dup.Children) != 2 {
		t.Fatalf("duplicated children count = %d, want 2", len(dup.Children))
	}
	dupBtn, dupGate := dup.Children[0], dup.Children[1]
	if dupBtn.ID == "btn" || dupGate.ID == "gate" || dupBtn.ID == dupGate.ID {
		t.Fatalf("child IDs not unique: btn=%q gate=%q", dupBtn.ID, dupGate.ID)
	}
	if dupBtn.Properties["tag"] != "trigger" || dupGate.Properties["hp"] != 10 {
		t.Fatalf("duplicated child properties not preserved: btn=%v gate=%v", dupBtn.Properties, dupGate.Properties)
	}

	// Only the internal connection (btn -> gate) should be duplicated and rewritten to (dupBtn -> dupGate).
	if len(ed.Doc.Connections) != 3 {
		t.Fatalf("Connections count = %d, want 3 (2 original + 1 rewritten internal copy): %+v", len(ed.Doc.Connections), ed.Doc.Connections)
	}
	copiedConn := ed.Doc.Connections[2]
	if copiedConn.From != dupBtn.ID || copiedConn.To != dupGate.ID || copiedConn.Event != "pressed" || copiedConn.Handler != "open" || !copiedConn.Once || copiedConn.Args["speed"] != 3 {
		t.Fatalf("rewritten connection = %+v, want %s.pressed -> %s.open (speed=3, once=true)", copiedConn, dupBtn.ID, dupGate.ID)
	}

	// One Undo restores the scene byte-for-byte and re-selects "unit".
	if !ed.Undo() {
		t.Fatal("Undo returned false")
	}
	if ed.SelectedID != "unit" {
		t.Fatalf("SelectedID after Undo = %q, want unit", ed.SelectedID)
	}
	afterUndoYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML after Undo: %v", err)
	}
	if !bytes.Equal(afterUndoYAML, beforeYAML) {
		t.Fatalf("Undo did not restore scene byte-for-byte:\ngot:\n%s\nwant:\n%s", afterUndoYAML, beforeYAML)
	}

	// One Redo brings back the duplicate and its rewritten connection.
	if !ed.Redo() {
		t.Fatal("Redo returned false")
	}
	if obj, _, _ := ed.FindObject(dupID); obj == nil || len(ed.Doc.Connections) != 3 {
		t.Fatalf("Redo did not restore duplicate and connection (obj=%v, conns=%d)", obj != nil, len(ed.Doc.Connections))
	}
}

// #868 Acceptance #2: Objects copied from one scene paste into another opened scene via
// the system clipboard as scene-format YAML, and pasting YAML that does not parse is refused in the status bar.
func TestCopyPasteAcrossScenesAndInvalidYAMLRefusedInStatusBar(t *testing.T) {
	reg := clipboardComponentRegistry(t)
	sceneA := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Scene A",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:   "contraption",
						Name: "Contraption",
						X:    80,
						Y:    60,
						Children: []sceneformat.ObjectDoc{
							{
								ID:         "plate",
								X:          -10,
								Y:          0,
								Components: map[string]any{"button": map[string]any{"label": "Step"}},
								Properties: map[string]any{"weight": 2},
							},
							{
								ID:         "hatch",
								X:          10,
								Y:          0,
								Components: map[string]any{"door": map[string]any{"locked": true}},
							},
						},
					},
				},
			},
		},
		Connections: []sceneformat.ConnectionDoc{
			{From: "plate", Event: "pressed", To: "hatch", Handler: "open", Args: map[string]any{"speed": 4}},
		},
	}

	ed, err := editor.New(sceneA, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.SetComponentRegistry(reg); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	ed.SelectObject("contraption")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	// Copy in Scene A via Cmd+C -> system clipboard receives valid scene-format YAML.
	app.Key("meta+c")
	if strings.TrimSpace(app.Clipboard) == "" {
		t.Fatal("system clipboard is empty after Cmd+C")
	}
	parsedClip, err := sceneformat.ParseWith([]byte(app.Clipboard), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("clipboard text is not valid scene-format YAML: %v\n%s", err, app.Clipboard)
	}
	if len(parsedClip.Layers) != 1 || len(parsedClip.Layers[0].Objects) != 1 || len(parsedClip.Connections) != 1 {
		t.Fatalf("unexpected clipboard scene structure: layers=%d conns=%d", len(parsedClip.Layers), len(parsedClip.Connections))
	}

	// Open Scene B with a different layer name and existing object.
	sceneB := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Scene B",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "stage",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{ID: "existing", X: 10, Y: 10},
				},
			},
		},
	}
	sceneBYAML, err := sceneformat.Encode(sceneB)
	if err != nil {
		t.Fatalf("Encode Scene B: %v", err)
	}
	app.Post(func() {
		if err := ed.OpenYAML(sceneBYAML); err != nil {
			t.Errorf("OpenYAML Scene B: %v", err)
		}
	})
	app.Settle()

	// Paste into Scene B via Cmd+V: IDs are unused in Scene B so originals are kept cleanly in one undo step.
	app.Key("meta+v")
	if ed.History.Len() != 1 {
		t.Fatalf("History.Len() after Paste in Scene B = %d, want 1", ed.History.Len())
	}
	pasted, li, _ := ed.FindObject("contraption")
	if pasted == nil || ed.Doc.Layers[li].Name != "stage" {
		t.Fatalf("pasted contraption not placed on active layer stage (obj=%v, li=%d)", pasted != nil, li)
	}
	if len(pasted.Children) != 2 || pasted.Children[0].ID != "plate" || pasted.Children[1].ID != "hatch" {
		t.Fatalf("pasted children = %+v, want [plate, hatch]", pasted.Children)
	}
	if len(ed.Doc.Connections) != 1 || ed.Doc.Connections[0].From != "plate" || ed.Doc.Connections[0].To != "hatch" {
		t.Fatalf("pasted connections = %+v, want plate -> hatch", ed.Doc.Connections)
	}

	// Paste a second time in Scene B: IDs are now taken so fresh IDs are allocated and connection rewritten.
	app.Key("meta+v")
	if ed.History.Len() != 2 {
		t.Fatalf("History.Len() after 2nd Paste = %d, want 2", ed.History.Len())
	}
	if len(ed.Doc.Layers[0].Objects) != 3 || len(ed.Doc.Connections) != 2 {
		t.Fatalf("after 2nd paste: objects=%d conns=%d, want 3 and 2", len(ed.Doc.Layers[0].Objects), len(ed.Doc.Connections))
	}
	secondRoot := ed.Doc.Layers[0].Objects[2]
	if secondRoot.ID == "contraption" || secondRoot.X == pasted.X && secondRoot.Y == pasted.Y {
		t.Fatalf("2nd paste did not allocate new ID or offset: id=%q pos=(%v,%v)", secondRoot.ID, secondRoot.X, secondRoot.Y)
	}
	if secondConn := ed.Doc.Connections[1]; secondConn.From != secondRoot.Children[0].ID || secondConn.To != secondRoot.Children[1].ID {
		t.Fatalf("2nd paste connection = %+v, want %s -> %s", secondConn, secondRoot.Children[0].ID, secondRoot.Children[1].ID)
	}

	// Pasting unparseable or invalid YAML is refused in the status bar with zero scene/history mutations.
	snapshotBeforeBad, _ := ed.SaveYAML()
	histBeforeBad := ed.History.Len()

	badPayloads := []struct {
		name string
		text string
	}{
		{"malformed_yaml", "not: [valid yaml"},
		{"non_scene_yaml", "hello: world\n"},
		{"empty_clipboard", "   \n"},
		{"no_objects", "version: 4\nlayers:\n  - name: empty\n"},
		{"unregistered_component", "version: 4\nlayers:\n  - name: a\n    objects:\n      - id: bad\n        components:\n          unknown_comp: {x: 1}\n"},
	}
	for _, tc := range badPayloads {
		app.Clipboard = tc.text
		app.Key("meta+v")
		if !strings.Contains(strings.ToLower(ed.Status), "error") && !strings.Contains(strings.ToLower(ed.Status), "paste failed") {
			t.Fatalf("Status after invalid paste %q = %q, want error message in status bar", tc.name, ed.Status)
		}
		if got := app.Find("id:editor-status-message").Text; got != ed.Status {
			t.Fatalf("status bar widget text after %q = %q, want %q", tc.name, got, ed.Status)
		}
		if ed.History.Len() != histBeforeBad {
			t.Fatalf("invalid paste %q modified history: len=%d, want %d", tc.name, ed.History.Len(), histBeforeBad)
		}
		afterBad, _ := ed.SaveYAML()
		if !bytes.Equal(afterBad, snapshotBeforeBad) {
			t.Fatalf("invalid paste %q mutated scene document", tc.name)
		}
	}
}

func TestCutMultiSelectOneUndoStepAndPasteRestore(t *testing.T) {
	reg := clipboardComponentRegistry(t)
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Cut Test",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "main",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{ID: "keep", X: 10, Y: 10},
					{ID: "b1", X: 40, Y: 40, Components: map[string]any{"button": map[string]any{"label": "A"}}},
					{ID: "d1", X: 90, Y: 40, Components: map[string]any{"door": map[string]any{"locked": true}}},
				},
			},
		},
		Connections: []sceneformat.ConnectionDoc{
			{From: "b1", Event: "pressed", To: "d1", Handler: "open"},
		},
	}

	ed, err := editor.New(doc, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.SetComponentRegistry(reg); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	ed.SelectObjects([]string{"b1", "d1"})
	before, _ := ed.SaveYAML()

	var clipText string
	ed.WriteClipboard = func(text string) { clipText = text }
	ed.ReadClipboard = func(done func(string)) { done(clipText) }

	cutYAML, err := ed.CutSelected()
	if err != nil {
		t.Fatalf("CutSelected: %v", err)
	}
	if cutYAML == "" || clipText != cutYAML {
		t.Fatalf("CutSelected did not write scene YAML to clipboard")
	}
	if ed.History.Len() != 1 {
		t.Fatalf("CutSelected history len = %d, want 1", ed.History.Len())
	}
	if len(ed.Doc.Layers[0].Objects) != 1 || ed.Doc.Layers[0].Objects[0].ID != "keep" || len(ed.Doc.Connections) != 0 {
		t.Fatalf("CutSelected did not remove b1, d1 and their connection: objs=%+v conns=%+v", ed.Doc.Layers[0].Objects, ed.Doc.Connections)
	}

	// Undo restores both objects, connection, and multi-selection byte-for-byte.
	if !ed.Undo() {
		t.Fatal("Undo failed")
	}
	afterUndo, _ := ed.SaveYAML()
	if !bytes.Equal(afterUndo, before) {
		t.Fatalf("Undo of Cut did not restore scene:\ngot:\n%s\nwant:\n%s", afterUndo, before)
	}
	if got := ed.SelectedIDs(); len(got) != 2 || got[0] != "b1" || got[1] != "d1" {
		t.Fatalf("SelectedIDs after Undo = %v, want [b1 d1]", got)
	}

	// Redo cuts again, then Paste puts b1, d1 and their connection back with original IDs.
	ed.Redo()
	pastedIDs, err := ed.Paste()
	if err != nil {
		t.Fatalf("Paste after Cut: %v", err)
	}
	if len(pastedIDs) != 2 || pastedIDs[0] != "b1" || pastedIDs[1] != "d1" {
		t.Fatalf("pastedIDs after Cut = %v, want [b1 d1]", pastedIDs)
	}
	if len(ed.Doc.Connections) != 1 || ed.Doc.Connections[0].From != "b1" || ed.Doc.Connections[0].To != "d1" {
		t.Fatalf("pasted connection after Cut = %+v", ed.Doc.Connections)
	}
}

func TestPasteAtHoveredPointerAndPasteIntoParent(t *testing.T) {
	ed := newNestedEditor(t)
	ed.SetCanvasSize(640, 360)
	ed.SelectObject("crate")

	if _, err := ed.CopySelected(); err != nil {
		t.Fatalf("CopySelected: %v", err)
	}

	// Hover 2D viewport at world (220, 140) and paste -> lands at (220, 140).
	cx, cy := ed.WorldToCanvas(220, 140)
	ed.SetViewportPointer2D(cx, cy)
	ids, err := ed.Paste()
	if err != nil {
		t.Fatalf("Paste at pointer: %v", err)
	}
	pasted, _, _ := ed.FindObject(ids[0])
	if pasted == nil || math.Abs(float64(pasted.X-220)) > 1e-2 || math.Abs(float64(pasted.Y-140)) > 1e-2 {
		t.Fatalf("pasted at pointer = (%v, %v), want (220, 140)", pasted.X, pasted.Y)
	}

	// PasteInto "ship" places copy as a child of "ship" in one undo step.
	ed.ClearViewportPointer()
	histBefore := ed.History.Len()
	childIDs, err := ed.PasteInto("ship")
	if err != nil {
		t.Fatalf("PasteInto ship: %v", err)
	}
	if ed.History.Len() != histBefore+1 {
		t.Fatalf("PasteInto history delta = %d, want 1", ed.History.Len()-histBefore)
	}
	if gotParent := parentOf(ed, childIDs[0]); gotParent != "ship" {
		t.Fatalf("pasted child parent = %q, want ship", gotParent)
	}

	// Pasting a 3D camera with current: true into a scene that already has a current camera
	// clears current on the copy so sceneformat.Validate succeeds.
	camDoc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Cameras",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "main",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:   "main_cam",
						X:    0,
						Y:    2,
						PosZ: 5,
						Camera: &sceneformat.Camera3DDoc{
							Projection: sceneformat.ProjectionPerspective,
							FOV:        sceneformat.Float32(60),
							Near:       sceneformat.Float32(0.1),
							Far:        sceneformat.Float32(100),
							Current:    true,
						},
					},
				},
			},
		},
	}
	camEd, err := editor.New(camDoc, nil)
	if err != nil {
		t.Fatalf("editor.New camDoc: %v", err)
	}
	camDupID, err := camEd.DuplicateObject("main_cam")
	if err != nil {
		t.Fatalf("DuplicateObject camera: %v", err)
	}
	camDup, _, _ := camEd.FindObject(camDupID)
	if camDup == nil || camDup.Camera == nil || camDup.Camera.Current {
		t.Fatalf("duplicated current camera should clear Current=false, got %+v", camDup)
	}
}

func TestPasteRewritesInstancedSceneConnectionsAndValidatesCycles(t *testing.T) {
	enemy := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "root",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{ID: "body", X: 0, Y: 0},
				},
			},
		},
	}
	resolver := sceneResolverMap{
		"scenes/enemy.scene.yaml": enemy,
	}
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Arena",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:        "enemy_1",
						X:         50,
						Y:         60,
						Scene:     "scenes/enemy.scene.yaml",
						Overrides: map[string]sceneformat.ObjectOverride{"body": {Alpha: sceneformat.Float32(0.5)}},
					},
					{
						ID: "alarm",
						X:  120,
						Y:  60,
					},
				},
			},
		},
		Connections: []sceneformat.ConnectionDoc{
			{From: "enemy_1/body", Event: "spotted", To: "alarm", Handler: "ring"},
		},
	}
	ed, err := editor.New(doc, resolver)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.ScenePath = "scenes/arena.scene.yaml"
	ed.SelectObjects([]string{"enemy_1", "alarm"})

	dups, err := ed.DuplicateSelectedObjects()
	if err != nil {
		t.Fatalf("DuplicateSelectedObjects: %v", err)
	}
	if len(dups) != 2 {
		t.Fatalf("dups = %v, want 2 IDs", dups)
	}
	dupEnemy, _, _ := ed.FindObject(dups[0])
	if dupEnemy == nil || dupEnemy.Scene != "scenes/enemy.scene.yaml" || len(dupEnemy.Overrides) != 1 {
		t.Fatalf("duplicated instancing object lost scene/overrides: %+v", dupEnemy)
	}
	if len(ed.Doc.Connections) != 2 {
		t.Fatalf("connections = %+v, want 2", ed.Doc.Connections)
	}
	wantFrom := dups[0] + "/body"
	if c := ed.Doc.Connections[1]; c.From != wantFrom || c.To != dups[1] {
		t.Fatalf("rewritten instanced connection = %+v, want %s -> %s", c, wantFrom, dups[1])
	}

	// Self-referencing scene cycle pasted from YAML must be refused through PlaceObject's checks.
	cycleYAML := "version: 4\nlayers:\n  - name: actors\n    objects:\n      - id: loop\n        scene: scenes/arena.scene.yaml\n"
	if _, err := ed.PasteYAML([]byte(cycleYAML)); err == nil {
		t.Fatal("PasteYAML with self-referencing scene cycle succeeded, want refusal")
	}
}

type sceneResolverMap map[string]*sceneformat.SceneDoc

func (m sceneResolverMap) Atlas(string) (*sprite.Atlas, error) {
	return nil, nil
}

func (m sceneResolverMap) Scene(p string) (*sceneformat.SceneDoc, error) {
	if doc, ok := m[p]; ok {
		return doc.Clone(), nil
	}
	return nil, fmt.Errorf("scene not found: %s", p)
}

func TestGroupAndUngroupPreserveWorldTransforms2DAnd3D(t *testing.T) {
	// 2D test: rotated/scaled objects grouped at common centre and ungrouped after transforming group.
	ed := newNestedEditor(t)
	if err := ed.MoveObject("hero", 20, 40); err != nil {
		t.Fatal(err)
	}
	if err := ed.RotateObject("hero", 0.5); err != nil {
		t.Fatal(err)
	}
	if err := ed.ScaleObject("hero", 2, 2); err != nil {
		t.Fatal(err)
	}
	if err := ed.MoveObject("crate", 100, 80); err != nil {
		t.Fatal(err)
	}
	if err := ed.ScaleObject("crate", -1, 1); err != nil {
		t.Fatal(err)
	}
	ed.History.Clear()

	beforeYAML, _ := ed.SaveYAML()
	worldHero := worldOf(t, ed, "hero")
	worldCrate := worldOf(t, ed, "crate")

	ed.SelectObjects([]string{"hero", "crate"})
	groupID, err := ed.GroupSelected()
	if err != nil {
		t.Fatalf("GroupSelected: %v", err)
	}
	if ed.History.Len() != 1 {
		t.Fatalf("Group history len = %d, want 1", ed.History.Len())
	}
	if ed.SelectedID != groupID {
		t.Fatalf("SelectedID after Group = %q, want %q", ed.SelectedID, groupID)
	}
	groupObj, _, _ := ed.FindObject(groupID)
	if groupObj == nil {
		t.Fatalf("group object %q not found", groupID)
	}
	// Common centre of (20,40) and (100,80) is (60, 60).
	if math.Abs(float64(groupObj.X-60)) > 1e-3 || math.Abs(float64(groupObj.Y-60)) > 1e-3 {
		t.Fatalf("group centre = (%v, %v), want (60, 60)", groupObj.X, groupObj.Y)
	}
	if parentOf(ed, "hero") != groupID || parentOf(ed, "crate") != groupID {
		t.Fatalf("parents after Group: hero=%q crate=%q, want %q", parentOf(ed, "hero"), parentOf(ed, "crate"), groupID)
	}
	if got := worldOf(t, ed, "hero"); !got.ApproxEqual(worldHero, 1e-3) {
		t.Fatalf("hero world transform moved after Group: got %+v, want %+v", got, worldHero)
	}
	if got := worldOf(t, ed, "crate"); !got.ApproxEqual(worldCrate, 1e-3) {
		t.Fatalf("crate world transform moved after Group: got %+v, want %+v", got, worldCrate)
	}

	// Undo restores byte-for-byte identical YAML and original multi-selection.
	if !ed.Undo() {
		t.Fatal("Undo of Group failed")
	}
	afterUndo, _ := ed.SaveYAML()
	if !bytes.Equal(afterUndo, beforeYAML) {
		t.Fatalf("Undo of Group did not restore scene:\ngot:\n%s\nwant:\n%s", afterUndo, beforeYAML)
	}
	if got := ed.SelectedIDs(); len(got) != 2 || got[0] != "hero" || got[1] != "crate" {
		t.Fatalf("SelectedIDs after Group Undo = %v, want [hero crate]", got)
	}

	// Redo group, transform the group, then Ungroup and verify world transforms stay identical.
	ed.Redo()
	if err := ed.MoveObject(groupID, 90, 75); err != nil {
		t.Fatal(err)
	}
	if err := ed.RotateObject(groupID, 0.35); err != nil {
		t.Fatal(err)
	}
	beforeUngroupYAML, _ := ed.SaveYAML()
	movedHeroWorld := worldOf(t, ed, "hero")
	movedCrateWorld := worldOf(t, ed, "crate")
	histBeforeUngroup := ed.History.Len()

	lifted, err := ed.UngroupSelected()
	if err != nil {
		t.Fatalf("UngroupSelected: %v", err)
	}
	if ed.History.Len() != histBeforeUngroup+1 {
		t.Fatalf("Ungroup history delta = %d, want 1", ed.History.Len()-histBeforeUngroup)
	}
	if len(lifted) != 2 || lifted[0] != "hero" || lifted[1] != "crate" {
		t.Fatalf("lifted = %v, want [hero crate]", lifted)
	}
	if obj, _, _ := ed.FindObject(groupID); obj != nil {
		t.Fatalf("empty group parent %q was not removed on Ungroup", groupID)
	}
	if parentOf(ed, "hero") != "" || parentOf(ed, "crate") != "" {
		t.Fatalf("unparented children still have parents: hero=%q crate=%q", parentOf(ed, "hero"), parentOf(ed, "crate"))
	}
	if got := worldOf(t, ed, "hero"); !got.ApproxEqual(movedHeroWorld, 1e-3) {
		t.Fatalf("hero world transform moved after Ungroup: got %+v, want %+v", got, movedHeroWorld)
	}
	if got := worldOf(t, ed, "crate"); !got.ApproxEqual(movedCrateWorld, 1e-3) {
		t.Fatalf("crate world transform moved after Ungroup: got %+v, want %+v", got, movedCrateWorld)
	}

	// Undo of Ungroup puts the empty group and its children back byte-for-byte.
	ed.Undo()
	afterUngroupUndo, _ := ed.SaveYAML()
	if !bytes.Equal(afterUngroupUndo, beforeUngroupYAML) {
		t.Fatalf("Undo of Ungroup did not restore scene:\ngot:\n%s\nwant:\n%s", afterUngroupUndo, beforeUngroupYAML)
	}

	// Ungrouping a non-empty parent (ship carrying a property) lifts children while keeping ship intact.
	if err := ed.SetProperty("ship", "hull", 100); err != nil {
		t.Fatalf("SetProperty ship: %v", err)
	}
	shipLifted, err := ed.UngroupObject("ship")
	if err != nil {
		t.Fatalf("UngroupObject ship: %v", err)
	}
	if len(shipLifted) != 1 || shipLifted[0] != "turret" {
		t.Fatalf("UngroupObject ship lifted = %v, want [turret]", shipLifted)
	}
	if ship, _, _ := ed.FindObject("ship"); ship == nil || len(ship.Children) != 0 {
		t.Fatalf("non-empty parent ship should remain intact with 0 children: %+v", ship)
	}
	if parentOf(ed, "turret") != "" {
		t.Fatalf("turret should be lifted out of ship, parent = %q", parentOf(ed, "turret"))
	}

	// 3D test: group two 3D objects at distinct PosZ and verify common 3D centre + world positions preserved.
	doc3D := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "3D Group",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "world",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{ID: "cube_a", X: 2, Y: 4, PosZ: 6, RotY: 0.5, Mesh: "box"},
					{ID: "cube_b", X: 8, Y: 10, PosZ: 14, RotX: 0.25, Mesh: "sphere"},
				},
			},
		},
	}
	ed3D, err := editor.New(doc3D, nil)
	if err != nil {
		t.Fatalf("editor.New 3D: %v", err)
	}
	ed3D.SetViewMode(editor.ViewMode3D)
	gid3D, err := ed3D.GroupObjects("cube_a", "cube_b")
	if err != nil {
		t.Fatalf("GroupObjects 3D: %v", err)
	}
	g3, _, _ := ed3D.FindObject(gid3D)
	if g3 == nil || math.Abs(float64(g3.X-5)) > 1e-3 || math.Abs(float64(g3.Y-7)) > 1e-3 || math.Abs(float64(g3.PosZ-10)) > 1e-3 {
		t.Fatalf("3D group centre = (%v, %v, %v), want (5, 7, 10)", g3.X, g3.Y, g3.PosZ)
	}
	cubeA, _, _ := ed3D.FindObject("cube_a")
	cubeB, _, _ := ed3D.FindObject("cube_b")
	if math.Abs(float64(cubeA.X+3)) > 1e-3 || math.Abs(float64(cubeA.Y+3)) > 1e-3 || math.Abs(float64(cubeA.PosZ+4)) > 1e-3 {
		t.Fatalf("cube_a local pos = (%v,%v,%v), want (-3,-3,-4)", cubeA.X, cubeA.Y, cubeA.PosZ)
	}
	if math.Abs(float64(cubeB.X-3)) > 1e-3 || math.Abs(float64(cubeB.Y-3)) > 1e-3 || math.Abs(float64(cubeB.PosZ-4)) > 1e-3 {
		t.Fatalf("cube_b local pos = (%v,%v,%v), want (3,3,4)", cubeB.X, cubeB.Y, cubeB.PosZ)
	}
}

func TestBuiltinCommandsHaveNoShortcutClashesAndEditMenuContextMenuWork(t *testing.T) {
	ed := newNestedEditor(t)

	// Verify ZERO same-scope shortcut collisions across all built-in commands on both Apple and non-Apple platforms.
	seenApple := make(map[string]string)
	seenOther := make(map[string]string)
	resolve := func(sc, mod string) string {
		parts := strings.Split(sc, "+")
		for i := 0; i < len(parts)-1; i++ {
			if parts[i] == "mod" {
				parts[i] = mod
			}
		}
		return strings.Join(parts, "+")
	}
	for _, cmd := range ed.Commands() {
		scope := cmd.Scope
		if scope == "" {
			scope = editor.ScopeGlobal
		}
		shortcuts := []string{}
		if cmd.Shortcut != "" {
			shortcuts = append(shortcuts, cmd.Shortcut)
		}
		shortcuts = append(shortcuts, cmd.AlternateShortcuts...)
		for _, sc := range shortcuts {
			norm, err := editor.NormalizeShortcut(sc)
			if err != nil {
				t.Fatalf("command %q invalid shortcut %q: %v", cmd.ID, sc, err)
			}
			ak := scope + "\x00" + resolve(norm, "meta")
			if prev, dup := seenApple[ak]; dup {
				t.Fatalf("Apple shortcut clash in scope %q on %q between %q and %q", scope, resolve(norm, "meta"), prev, cmd.ID)
			}
			seenApple[ak] = cmd.ID

			ok := scope + "\x00" + resolve(norm, "ctrl")
			if prev, dup := seenOther[ok]; dup {
				t.Fatalf("Ctrl shortcut clash in scope %q on %q between %q and %q", scope, resolve(norm, "ctrl"), prev, cmd.ID)
			}
			seenOther[ok] = cmd.ID
		}
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	// Verify Edit menu exposes Copy, Cut, Paste, Paste as Child, Duplicate, Group, Ungroup with exact shortcuts.
	menuChecks := []struct {
		path     []string
		shortcut string
	}{
		{[]string{"Edit", "Cut"}, "mod+x"},
		{[]string{"Edit", "Copy"}, "mod+c"},
		{[]string{"Edit", "Paste"}, "mod+v"},
		{[]string{"Edit", "Paste as Child"}, "mod+shift+v"},
		{[]string{"Edit", "Duplicate"}, "mod+d"},
		{[]string{"Edit", "Group"}, "mod+g"},
		{[]string{"Edit", "Ungroup"}, "mod+shift+g"},
	}
	for _, tc := range menuChecks {
		item, ok := findMenuItem(app.Menu, tc.path...)
		if !ok {
			t.Fatalf("menu item %v not found", tc.path)
		}
		if item.Shortcut != tc.shortcut {
			t.Fatalf("menu item %v shortcut = %q, want %q", tc.path, item.Shortcut, tc.shortcut)
		}
		if !item.NotInText {
			t.Fatalf("menu item %v must set NotInText=true", tc.path)
		}
	}

	// Drive Group & Ungroup via keyboard shortcuts (Mod+G, Mod+Shift+G).
	app.Post(func() { ed.SelectObjects([]string{"hero", "crate"}) })
	app.Settle()
	app.Key("meta+g")
	gid := ed.SelectedID
	if gid == "" || parentOf(ed, "hero") != gid || parentOf(ed, "crate") != gid {
		t.Fatalf("Cmd+G did not group hero and crate under %q", gid)
	}
	app.Key("meta+shift+g")
	if obj, _, _ := ed.FindObject(gid); obj != nil || parentOf(ed, "hero") != "" || parentOf(ed, "crate") != "" {
		t.Fatalf("Cmd+Shift+G did not ungroup %q", gid)
	}

	// Drive multi-select Copy and Paste as Child via Scene Tree context menu.
	// Ungroup left [hero, crate] selected, so copying crate copies both and Paste as Child puts both into ship.
	app.RightClick("id:scene-tree-row-crate")
	app.Press("role:menuitem Copy")
	if strings.TrimSpace(app.Clipboard) == "" {
		t.Fatal("Scene Tree Copy did not populate clipboard")
	}
	app.RightClick("id:scene-tree-row-ship")
	app.Press("role:menuitem Paste as Child")
	ship, _, _ := ed.FindObject("ship")
	if len(ship.Children) != 3 {
		t.Fatalf("Scene Tree Paste as Child into ship: children=%d, want 3 (turret + copied hero & crate)", len(ship.Children))
	}
}

func TestMultiTabCopyPasteMultiSelectAndAsyncGuard(t *testing.T) {
	dir := t.TempDir()
	sceneA := filepath.Join(dir, "a.scene.yaml")
	sceneB := filepath.Join(dir, "b.scene.yaml")
	yamlA := `version: 2
name: SceneA
layers:
  - name: main
    objects:
      - id: alpha
        x: 10
        y: 20
      - id: beta
        x: 30
        y: 40
`
	yamlB := `version: 2
name: SceneB
layers:
  - name: main
    objects:
      - id: alpha
        x: 100
        y: 200
`
	if err := os.WriteFile(sceneA, []byte(yamlA), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sceneB, []byte(yamlB), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenFile(sceneA); err != nil {
		t.Fatalf("OpenFile(A): %v", err)
	}
	if err := ed.OpenFile(sceneB); err != nil {
		t.Fatalf("OpenFile(B): %v", err)
	}
	if ed.TabCount() != 2 {
		t.Fatalf("TabCount = %d, want 2", ed.TabCount())
	}

	// Select tab A and multi-select [alpha, beta].
	if err := ed.SelectTab(0); err != nil {
		t.Fatal(err)
	}
	ed.SelectObjects([]string{"alpha", "beta"})
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"alpha", "beta"}) {
		t.Fatalf("tab A SelectedIDs = %v, want [alpha beta]", got)
	}

	// Copy from tab A and paste once in tab A (advancing pasteStreak to 1).
	clipYAML, err := ed.CopySelected()
	if err != nil {
		t.Fatalf("CopySelected: %v", err)
	}
	if _, err := ed.Paste(); err != nil {
		t.Fatalf("Paste in tab A: %v", err)
	}
	// Restore multi-select on tab A, switch to tab B, verify tab A's multi-select does NOT leak into tab B.
	ed.SelectObjects([]string{"alpha", "beta"})
	if err := ed.SelectTab(1); err != nil {
		t.Fatal(err)
	}
	if got := ed.SelectedIDs(); len(got) != 0 {
		t.Fatalf("tab B inherited stale SelectedIDs = %v, want empty", got)
	}

	// Paste in tab B: pasteStreak must have reset on tab switch (offset = 1x defaultPasteOffset2D = +16, not +32).
	pastedB, err := ed.Paste()
	if err != nil {
		t.Fatalf("Paste in tab B: %v", err)
	}
	if len(pastedB) != 2 {
		t.Fatalf("pastedB = %v, want 2 objects", pastedB)
	}
	obj0, _, _ := ed.FindObject(pastedB[0])
	if obj0 == nil || obj0.X != 26 || obj0.Y != 36 {
		t.Fatalf("tab B pasted first object at (%v, %v), want (26, 36) with reset pasteStreak", obj0.X, obj0.Y)
	}

	// Switch back to tab A: multi-selection [alpha, beta] must still be intact.
	if err := ed.SelectTab(0); err != nil {
		t.Fatal(err)
	}
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"alpha", "beta"}) {
		t.Fatalf("tab A SelectedIDs after round-trip = %v, want [alpha beta]", got)
	}

	// Async ReadClipboard guard: if the active tab changes before the callback fires, nothing is pasted.
	var pending func(string)
	ed.ReadClipboard = func(done func(string)) {
		pending = done
	}
	if _, err := ed.Paste(); err != nil {
		t.Fatalf("async Paste start: %v", err)
	}
	if pending == nil {
		t.Fatal("expected async ReadClipboard callback")
	}
	// Switch to tab B before delivering clipboard text.
	if err := ed.SelectTab(1); err != nil {
		t.Fatal(err)
	}
	beforeCount := len(ed.Doc.Layers[0].Objects)
	pending(clipYAML)
	if afterCount := len(ed.Doc.Layers[0].Objects); afterCount != beforeCount {
		t.Fatalf("stale async clipboard callback mutated tab B: objects %d -> %d", beforeCount, afterCount)
	}
}

func TestClipboardNonFiniteGuardsAndMergeConflictDisable(t *testing.T) {
	ed, err := editor.New(&sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "Guards",
		Layers: []sceneformat.LayerDoc{
			{
				Name: "main",
				Objects: []sceneformat.ObjectDoc{
					{
						ID: "parent",
						X:  10,
						Y:  20,
						Children: []sceneformat.ObjectDoc{
							{ID: "child", X: 5, Y: 5},
						},
					},
				},
			},
		},
	}, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	ed.SelectObject("parent")
	clipYAML, err := ed.CopySelected()
	if err != nil {
		t.Fatalf("CopySelected: %v", err)
	}

	// Non-finite pointer inputs must be ignored so subsequent pastes remain valid.
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	ed.SetViewportPointer2D(nan, 10)
	ed.SetViewportPointer2D(10, inf)
	ed.SetViewportPointer3D(nan, inf)
	if ids, err := ed.PasteYAML([]byte(clipYAML)); err != nil || len(ids) != 1 {
		t.Fatalf("PasteYAML after non-finite pointer calls failed: ids=%v err=%v", ids, err)
	}

	// Non-finite PasteOptions must be refused cleanly without mutating the scene.
	badOpts := []editor.PasteOptions{
		{At: &[2]float32{nan, 0}},
		{At3D: &[3]float32{0, inf, 0}},
		{Offset: &[2]float32{0, nan}},
		{Offset3D: &[3]float32{inf, 0, 0}},
	}
	for i, opts := range badOpts {
		if _, err := ed.PasteYAMLWith([]byte(clipYAML), opts); err == nil {
			t.Fatalf("badOpts[%d]: expected error for non-finite coordinates", i)
		}
	}

	// Open a Merge Conflict session and verify all edit/clipboard/group/tile-region commands report Disabled.
	baseDoc := ed.Doc.Clone()
	mineDoc := ed.Doc.Clone()
	mineDoc.Layers[0].Objects[0].X = 99
	theirsDoc := ed.Doc.Clone()
	theirsDoc.Layers[0].Objects[0].X = 123
	if err := ed.OpenMergeConflictSession(editor.MergeConflictSessionConfig{
		ScenePath: "scenes/guards.scene.yaml",
		BaseDoc:   baseDoc,
		MineDoc:   mineDoc,
		TheirsDoc: theirsDoc,
	}); err != nil {
		t.Fatalf("OpenMergeConflictSession: %v", err)
	}
	if !ed.MergeConflictOpen() {
		t.Fatal("expected MergeConflictOpen() == true")
	}
	for _, cmdID := range []string{
		editor.CmdCopy,
		editor.CmdCut,
		editor.CmdPaste,
		editor.CmdPasteInto,
		editor.CmdDuplicate,
		editor.CmdGroup,
		editor.CmdUngroup,
		editor.CmdCopyTileRegion,
		editor.CmdCutTileRegion,
		editor.CmdPasteTileRegion,
	} {
		cmd, ok := ed.Command(cmdID)
		if !ok {
			t.Fatalf("missing command %q", cmdID)
		}
		if cmd.Enabled != nil && cmd.Enabled() {
			t.Fatalf("command %q must be disabled while MergeConflictOpen() is true", cmdID)
		}
	}
}
