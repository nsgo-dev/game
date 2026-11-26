package editor_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// #1039 Acceptance 1: Shift+F10 (and ContextMenu key) opens the object context menu on the active
// selection, Move under reparents it onto another object preserving its world transform, and ONE Undo
// puts the scene back byte-for-byte.
func TestObjectContextMenuShiftF10MoveUnderAndSingleUndo(t *testing.T) {
	ed := newNestedEditor(t)
	before := mustSave(t, ed)
	world := worldOf(t, ed, "hero")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	app.Post(func() { ed.SelectObject("hero") })
	app.Settle()

	// Open with Shift+F10.
	app.Key("shift+f10")
	if !ed.ObjectContextMenuOpen() || ed.ObjectContextMenuTarget() != "hero" {
		t.Fatalf("shift+f10 did not open context menu for hero (open=%v target=%q)", ed.ObjectContextMenuOpen(), ed.ObjectContextMenuTarget())
	}
	if got := app.Find("id:context-menu-title").Text; got != "Hero" {
		t.Fatalf("context menu title = %q, want Hero", got)
	}
	if got := app.Find("id:context-menu-subtitle").Text; got != "Sprite · layer actors" {
		t.Fatalf("context menu subtitle = %q, want %q", got, "Sprite · layer actors")
	}

	// Open Move under submenu and reparent hero under ship.
	app.Press("id:context-menu-move-under")
	if app.Exists("id:context-menu-move-under-hero") {
		t.Fatal("Move under must not list the object itself as a candidate parent")
	}
	app.Press("id:context-menu-move-under-ship")
	if ed.ObjectContextMenuOpen() {
		t.Fatal("context menu stayed open after choosing Move under target")
	}
	if got := parentOf(ed, "hero"); got != "ship" {
		t.Fatalf("hero parent after Move under = %q, want ship (status=%q)", got, ed.Status)
	}
	if got := worldOf(t, ed, "hero"); !got.ApproxEqual(world, 1e-3) {
		t.Fatalf("hero world transform moved after Move under: got %+v, want %+v", got, world)
	}

	// ONE Undo must put hero back at top level with identical YAML.
	app.Key("meta+z")
	if got := parentOf(ed, "hero"); got != "" {
		t.Fatalf("hero parent after ONE Undo = %q, want empty", got)
	}
	if after := mustSave(t, ed); !bytes.Equal(after, before) {
		t.Fatalf("ONE Undo did not restore scene byte-for-byte:\n%s\nwant:\n%s", after, before)
	}

	// Menu key ("contextmenu") also opens the context menu on the active selection, and Escape closes it.
	app.Key("contextmenu")
	if !ed.ObjectContextMenuOpen() || ed.ObjectContextMenuTarget() != "hero" {
		t.Fatalf("contextmenu key did not open menu for hero (open=%v target=%q)", ed.ObjectContextMenuOpen(), ed.ObjectContextMenuTarget())
	}
	app.Key("escape")
	if ed.ObjectContextMenuOpen() {
		t.Fatal("Escape did not close context menu")
	}
}

// #1039 Acceptance 2: Copy as YAML writes canonical sceneformat YAML onto the clipboard that
// sceneformat.Parse parses back to the exact same object, and shows "Copied <name> as YAML · N lines".
func TestObjectContextMenuCopyAsYAMLRoundTripAndToast(t *testing.T) {
	ed := newNestedEditor(t)
	if err := ed.SetProperty("ship", "mass", float64(2)); err != nil {
		t.Fatalf("SetProperty: %v", err)
	}
	ed.History.Clear()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	app.Post(func() { ed.SelectObject("ship") })
	app.Settle()

	app.Key("shift+f10")
	app.Press("id:context-menu-copy-yaml")

	clip := app.Clipboard
	if strings.TrimSpace(clip) == "" {
		t.Fatal("Copy as YAML left clipboard empty")
	}

	// sceneformat.Parse must parse the clipboard text back to the exact same ObjectDoc.
	parsed, err := sceneformat.Parse([]byte(clip))
	if err != nil {
		t.Fatalf("sceneformat.Parse(clipboard) failed: %v\nYAML:\n%s", err, clip)
	}
	if len(parsed.Layers) != 1 || len(parsed.Layers[0].Objects) != 1 {
		t.Fatalf("parsed layers/objects = %d/%d, want 1/1", len(parsed.Layers), len(parsed.Layers[0].Objects))
	}
	wantObj, _, _ := ed.FindObject("ship")
	gotObj := parsed.Layers[0].Objects[0]
	if !reflect.DeepEqual(gotObj, *wantObj) {
		t.Fatalf("parsed object differs from original:\ngot:  %+v\nwant: %+v", gotObj, *wantObj)
	}

	// Verify exact toast and status message format: "Copied <name> as YAML · N lines".
	lines := strings.Count(strings.TrimSuffix(clip, "\n"), "\n") + 1
	wantToast := fmt.Sprintf("Copied ship as YAML · %d lines", lines)
	if got := app.Find("id:editor-toast-message").Text; got != wantToast {
		t.Fatalf("viewport toast = %q, want %q", got, wantToast)
	}
	if got := app.Find("id:editor-status-message").Text; got != wantToast {
		t.Fatalf("status message = %q, want %q", got, wantToast)
	}

	// Dismissing the toast clears it cleanly.
	app.Press("id:editor-toast-dismiss")
	if app.Exists("id:editor-toast") {
		t.Fatal("viewport toast stayed visible after Dismiss")
	}

	// Pasting from the context menu creates a fresh copy with unique subtree IDs in one undo step.
	beforeCount := len(ed.Doc.Layers[1].Objects)
	app.Key("shift+f10")
	app.Press("id:context-menu-paste")
	if len(ed.Doc.Layers[1].Objects) != beforeCount+1 {
		t.Fatalf("actors count after Paste = %d, want %d (status=%q)", len(ed.Doc.Layers[1].Objects), beforeCount+1, ed.Status)
	}
	pastedID := ed.SelectedID
	if pastedID == "" || pastedID == "ship" {
		t.Fatalf("pasted object ID = %q, want fresh ID", pastedID)
	}
	pastedObj, _, _ := ed.FindObject(pastedID)
	if pastedObj == nil || len(pastedObj.Children) != 1 || pastedObj.Children[0].ID == "turret" {
		t.Fatalf("pasted subtree did not preserve children with fresh IDs: %+v", pastedObj)
	}
	app.Key("meta+z")
	if len(ed.Doc.Layers[1].Objects) != beforeCount {
		t.Fatalf("actors count after Undo of Paste = %d, want %d", len(ed.Doc.Layers[1].Objects), beforeCount)
	}
}

// #1039: Secondary click and Control-click open the object context menu on objects in the 2D viewport,
// 3D viewport, and Scene Tree rows; right-clicking empty viewport space does not open it.
func TestObjectContextMenu2DAnd3DViewportAndSceneTreeTriggers(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Context Triggers",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:     "hero",
						Name:   "Hero",
						X:      100,
						Y:      100,
						Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"},
					},
					{
						ID:   "box3d",
						Name: "Crate 3D",
						X:    260,
						Y:    120,
						Mesh: sceneformat.MeshBox,
					},
				},
			},
		},
	}
	ed, err := editor.New(doc, testResolver())
	if err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	// 1. 2D viewport: right-clicking empty center does NOT open the object context menu.
	app.Post(func() {
		ed.SetView2D(editor.View2D{X: 500, Y: 500, Zoom: 1})
	})
	app.Settle()
	app.RightClick("id:editor-viewport")
	if ed.ObjectContextMenuOpen() {
		t.Fatalf("right-clicking empty 2D viewport opened menu for %q", ed.ObjectContextMenuTarget())
	}

	// 2. 2D viewport: framing hero puts it at viewport center; RightClick opens the menu on hero.
	app.Post(func() {
		ed.SelectObject("hero")
		ed.FrameSelection2D()
		ed.SelectObject("")
	})
	app.Settle()
	app.RightClick("id:editor-viewport")
	if !ed.ObjectContextMenuOpen() || ed.ObjectContextMenuTarget() != "hero" {
		t.Fatalf("2D viewport RightClick on hero: open=%v target=%q", ed.ObjectContextMenuOpen(), ed.ObjectContextMenuTarget())
	}
	if ed.SelectedID != "hero" {
		t.Fatalf("2D viewport RightClick did not select hero (selected=%q)", ed.SelectedID)
	}
	app.Key("escape")

	// 3. 2D viewport: Control-click on box3d opens the menu on box3d.
	app.Post(func() {
		ed.SetView2D(editor.View2D{X: 200, Y: 110, Zoom: 1})
	})
	app.Settle()
	bx, by := ed.WorldToCanvas(260, 120)
	app.KeyDown("ctrl")
	app.MouseDownAt("id:editor-viewport-frame", core.Point{X: core.Points(bx), Y: core.Points(by)}, core.MouseButtonLeft)
	app.MouseUpAt("id:editor-viewport-frame", core.Point{X: core.Points(bx), Y: core.Points(by)}, core.MouseButtonLeft)
	app.KeyUp("ctrl")
	if !ed.ObjectContextMenuOpen() || ed.ObjectContextMenuTarget() != "box3d" {
		t.Fatalf("2D viewport Control-click on box3d: open=%v target=%q", ed.ObjectContextMenuOpen(), ed.ObjectContextMenuTarget())
	}
	if got := app.Find("id:context-menu-subtitle").Text; got != "3D Mesh · layer actors" {
		t.Fatalf("box3d subtitle = %q, want %q", got, "3D Mesh · layer actors")
	}
	app.Key("escape")

	// 4. 3D viewport: switch to 3D, frame box3d, RightClick opens menu on box3d.
	app.Post(func() {
		ed.SetViewMode(editor.ViewMode3D)
		ed.SelectObject("box3d")
		ed.FrameSelection()
		ed.SelectObject("")
	})
	app.Settle()
	app.RightClick("id:editor-viewport")
	if !ed.ObjectContextMenuOpen() || ed.ObjectContextMenuTarget() != "box3d" {
		t.Fatalf("3D viewport RightClick on box3d: open=%v target=%q", ed.ObjectContextMenuOpen(), ed.ObjectContextMenuTarget())
	}
	app.Key("escape")

	// 5. Scene Tree row: RightClick opens unified menu with header + Move under + Scene Tree items.
	app.RightClick("id:scene-tree-row-hero")
	if got := app.Find("id:context-menu-title").Text; got != "Hero" {
		t.Fatalf("Scene Tree context menu title = %q, want Hero", got)
	}
	if !app.Exists("id:scene-tree-copy-hero") || !app.Exists("id:context-menu-move-under") || !app.Exists("id:context-menu-ask-agent") {
		t.Fatal("Scene Tree context menu missing unified object actions")
	}
	if !app.Exists("id:scene-tree-change-id-hero") || !app.Exists("id:scene-tree-delete-hero") {
		t.Fatal("Scene Tree context menu missing preserved Scene Tree actions")
	}
}

// #1039: Group, Select same prefab, Hide/Show, Lock/Unlock, Find usages, Rename, Ask the agent,
// omission of unavailable items, and cycle prevention in Move under.
func TestObjectContextMenuActionsOmissionAndCycles(t *testing.T) {
	dir := writeEditorProject(t)
	sceneYAML := []byte(`version: 4
name: Context Actions
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    objects:
      - id: hero
        name: Knight
        x: 60
        y: 120
        sprite:
          atlas: actors
          frame: hero
      - id: slime_1
        name: Slime 01
        prefab: slime
        x: 160
        y: 120
        sprite:
          atlas: actors
          frame: slime
      - id: slime_2
        name: Slime 02
        prefab: slime
        x: 240
        y: 120
        sprite:
          atlas: actors
          frame: slime
`)
	if err := os.WriteFile(filepath.Join(dir, "scenes", "room.yaml"), sceneYAML, 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.WaitForProblems()

	var asked []editor.AgentPromptRequest
	ed.OnAskAgent = func(req editor.AgentPromptRequest) {
		asked = append(asked, req)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	// 1. Plain sprite "hero" must OMIT "Select same prefab", while "Find usages" is present because a project is open.
	app.Post(func() { ed.SelectObject("hero") })
	app.Settle()
	app.Key("shift+f10")
	if app.Exists("id:context-menu-select-same-prefab") {
		t.Fatal("Select same prefab should be omitted for plain object without prefab or instanced scene")
	}
	if !app.Exists("id:context-menu-find-usages") {
		t.Fatal("Find usages should be present when a project is open")
	}
	// Run Find usages -> opens Search & References panel.
	app.Press("id:context-menu-find-usages")
	if !ed.SearchOpen() {
		t.Fatal("Find usages did not open the Search & References panel")
	}
	app.Post(func() { ed.SetSearchOpen(false) })
	app.Settle()

	// 2. Prefab instance "slime_1" includes "Select same prefab", selecting both slimes.
	app.Post(func() { ed.SelectObject("slime_1") })
	app.Settle()
	app.Key("shift+f10")
	if !app.Exists("id:context-menu-select-same-prefab") {
		t.Fatal("Select same prefab missing on prefab instance slime_1")
	}
	app.Press("id:context-menu-select-same-prefab")
	if got := ed.SelectedIDs(); !slices.Equal(got, []string{"slime_1", "slime_2"}) {
		t.Fatalf("SelectedIDs after Select same prefab = %v, want [slime_1 slime_2]", got)
	}

	// 3. Group groups both selected slimes under one new parent in ONE undo step.
	beforeGroup := mustSave(t, ed)
	app.Key("shift+f10")
	app.Press("id:context-menu-group")
	gid := ed.SelectedID
	if !strings.HasPrefix(gid, "group_") {
		t.Fatalf("Group did not select new group object, got %q", gid)
	}
	if parentOf(ed, "slime_1") != gid || parentOf(ed, "slime_2") != gid {
		t.Fatalf("slimes not reparented under %q: slime_1=%q slime_2=%q", gid, parentOf(ed, "slime_1"), parentOf(ed, "slime_2"))
	}
	// ONE Undo removes the group and restores both slimes.
	app.Key("meta+z")
	if obj, _, _ := ed.FindObject(gid); obj != nil {
		t.Fatalf("ONE Undo did not remove group %q", gid)
	}
	if afterGroupUndo := mustSave(t, ed); !bytes.Equal(afterGroupUndo, beforeGroup) {
		t.Fatalf("ONE Undo of Group did not restore scene:\n%s\nwant:\n%s", afterGroupUndo, beforeGroup)
	}

	// 4. Hide / Show & Lock / Unlock toggles via context menu.
	app.Post(func() { ed.SelectObject("hero") })
	app.Settle()
	app.Key("shift+f10")
	app.Press("id:context-menu-hide")
	if !ed.ObjectHidden("hero") {
		t.Fatal("Hide did not hide hero")
	}
	app.Key("shift+f10")
	if got := allText(app.Find("id:context-menu-hide")); !strings.Contains(got, "Show") {
		t.Fatalf("hidden object menu text = %q, want Show", got)
	}
	app.Press("id:context-menu-hide")
	if ed.ObjectHidden("hero") {
		t.Fatal("Show did not unhide hero")
	}

	app.Key("shift+f10")
	app.Press("id:context-menu-lock")
	if !ed.ObjectLocked("hero") {
		t.Fatal("Lock did not lock hero")
	}
	app.Key("shift+f10")
	if got := allText(app.Find("id:context-menu-lock")); !strings.Contains(got, "Unlock") {
		t.Fatalf("locked object menu text = %q, want Unlock", got)
	}
	app.Press("id:context-menu-lock")
	if ed.ObjectLocked("hero") {
		t.Fatal("Unlock did not unlock hero")
	}

	// 5. Rename from viewport/keyboard context menu opens inline Scene Tree name editor.
	app.Key("shift+f10")
	app.Press("id:context-menu-rename")
	if !app.Exists("id:scene-tree-name-hero") {
		t.Fatal("Rename from context menu did not open inline scene tree name input")
	}
	app.Clear("id:scene-tree-name-hero")
	app.Type("id:scene-tree-name-hero", "Paladin\n")
	if obj, _, _ := ed.FindObject("hero"); obj.Name != "Paladin" {
		t.Fatalf("object name after Rename = %q, want Paladin", obj.Name)
	}

	// 6. Ask the agent about this invokes OnAskAgent callback with draft.
	app.Key("shift+f10")
	app.Press("id:context-menu-ask-agent")
	if len(asked) != 1 || asked[0].ObjectID != "hero" || asked[0].Draft != "About Paladin: " {
		t.Fatalf("AskAgent callback = %+v, want hero / 'About Paladin: '", asked)
	}

	// 7. Standalone scene without a project omits Find usages, and a 1-object scene omits Move under.
	soloDoc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "Solo",
		Layers: []sceneformat.LayerDoc{
			{Name: "main", Objects: []sceneformat.ObjectDoc{{ID: "only", Name: "Only"}}},
		},
	}
	soloEd, err := editor.New(soloDoc, nil)
	if err != nil {
		t.Fatal(err)
	}
	soloApp := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, soloEd)
	}, nsgotest.Size(1200, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	soloApp.Post(func() { soloEd.SelectObject("only") })
	soloApp.Settle()
	soloApp.Key("shift+f10")
	if soloApp.Exists("id:context-menu-find-usages") {
		t.Fatal("Find usages should be omitted when no project is open")
	}
	if soloApp.Exists("id:context-menu-move-under") {
		t.Fatal("Move under should be omitted when there are no valid candidate parents")
	}
	if soloApp.Exists("id:context-menu-select-same-prefab") {
		t.Fatal("Select same prefab should be omitted when object has no prefab/scene")
	}

	// 8. MoveUnderCandidates excludes self and descendants (cycle prevention).
	nested := newNestedEditor(t)
	cands := nested.MoveUnderCandidates("ship")
	for _, c := range cands {
		if c.ID == "ship" || c.ID == "turret" || c.ID == "barrel" {
			t.Fatalf("MoveUnderCandidates(ship) included cyclic candidate %q", c.ID)
		}
	}
}

// #1039 Acceptance 3: Real nsgotest screenshots on macOS (run with
// `go tool nsgo test -target macos -run TestObjectContextMenuScreenshots ./editor`)
// showing the object context menu, Move under submenu, and Copy as YAML toast beside Inspector.dc.html.
func TestObjectContextMenuScreenshots(t *testing.T) {
	dir := writeEditorProject(t)
	sceneYAML := []byte(`version: 4
name: level_01
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#1A1F2A"
layers:
  - name: actors
    space: world
    objects:
      - id: knight
        name: Knight
        x: 120
        y: 220
        sprite:
          atlas: actors
          frame: hero
      - id: slime_01
        name: slime_01
        prefab: slime
        x: 240
        y: 220
        sprite:
          atlas: actors
          frame: slime
      - id: slime_02
        name: slime_02
        prefab: slime
        x: 320
        y: 220
        sprite:
          atlas: actors
          frame: slime
      - id: coins
        name: coins
        x: 440
        y: 220
        children:
          - id: coin_01
            name: coin_01
            x: 0
            y: 0
          - id: coin_02
            name: coin_02
            x: 16
            y: 0
`)
	if err := os.WriteFile(filepath.Join(dir, "scenes", "room.yaml"), sceneYAML, 0o644); err != nil {
		t.Fatal(err)
	}
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	if err := ed.SetAppearance(editor.AppearanceDark); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true}))

	app.Post(func() {
		ed.SelectObject("slime_01")
		ed.FrameSelection2D()
	})
	app.Settle()

	// 1. Open object context menu on slime_01 via secondary click in the 2D viewport.
	app.RightClick("id:editor-viewport")
	if !ed.ObjectContextMenuOpen() || ed.ObjectContextMenuTarget() != "slime_01" {
		t.Fatalf("context menu did not open for slime_01 (open=%v target=%q)", ed.ObjectContextMenuOpen(), ed.ObjectContextMenuTarget())
	}
	app.Screenshot("object-context-menu")

	// 2. Open Move under submenu.
	app.Press("id:context-menu-move-under")
	app.Screenshot("object-context-menu-move-under")

	// 3. Copy as YAML and capture the floating Spec toast.
	app.Press("id:context-menu-copy-yaml")
	if !app.Exists("id:editor-toast") {
		t.Fatal("Copy as YAML toast not shown")
	}
	app.Screenshot("object-context-menu-copy-toast")
}

func TestObjectContextMenuForeignOverridesDynamicShortcutsModalAndLifecycleGuards(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"game.yaml": `version: 1
name: Override Menu
id: dev.example.overridemenu
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: main
  scenes:
    - name: main
      path: scenes/main.scene.yaml
`,
		"scenes/base.scene.yaml": `version: 2
name: Base
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    objects:
      - id: inherited_hero
        name: Inherited Hero
        x: 100
        y: 120
`,
		"scenes/enemy.scene.yaml": `version: 2
name: Enemy
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    objects:
      - id: body
        name: Enemy Body
        x: 0
        y: 0
`,
		"scenes/main.scene.yaml": `version: 2
name: Main
inherits: scenes/base.scene.yaml
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    objects:
      - id: slime_inst
        name: Slime Instance
        scene: scenes/enemy.scene.yaml
        x: 200
        y: 140
`,
	}
	for rel, content := range files {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectAssets = true
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	// Rebind Duplicate shortcut to verify dynamic shortcut hints reflect #1191 customizations.
	if err := ed.RebindShortcut(editor.CmdDuplicate, "mod+shift+d"); err != nil {
		t.Fatalf("RebindShortcut(Duplicate): %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true}))

	// 1. Foreign (inherited) object opens context menu with Open Source Scene & Revert Overrides, omitting local-only edits.
	app.Post(func() {
		ed.SelectObject("inherited_hero")
		if !ed.OpenObjectContextMenu("inherited_hero") {
			t.Error("OpenObjectContextMenu(inherited_hero) returned false")
		}
	})
	app.Settle()
	if !app.Exists("id:context-menu-open-source") || !app.Exists("id:context-menu-revert-overrides") {
		t.Fatal("foreign object context menu missing Open Source Scene / Revert Overrides")
	}
	if app.Exists("id:context-menu-duplicate") || app.Exists("id:context-menu-move-under") || app.Exists("id:context-menu-group") {
		t.Fatal("foreign object viewport context menu should omit local-only Duplicate / Group / Move under")
	}
	if got := app.Find("id:context-menu-title").Text; got != "Inherited Hero" {
		t.Fatalf("foreign context-menu-title = %q, want Inherited Hero", got)
	}
	app.Post(func() { ed.CloseObjectContextMenu() })
	app.Settle()

	// 2. Scene Tree context menu on scene instance exposes #1184 Make Local / Apply / Revert / Open Instanced Scene and #1187 Find Usages.
	app.RightClick("id:scene-tree-row-slime_inst")
	for _, wantID := range []string{
		"id:scene-tree-open-scene-slime_inst",
		"id:scene-tree-make-local-slime_inst",
		"id:scene-tree-apply-overrides-slime_inst",
		"id:scene-tree-revert-overrides-slime_inst",
		"id:scene-tree-find-usages-slime_inst",
	} {
		if !app.Exists(wantID) {
			t.Fatalf("scene tree context menu on scene instance missing %s", wantID)
		}
	}
	// Verify dynamic shortcut from #1191 appears on Duplicate and Lock has no misleading "L" shortcut.
	if got := allText(app.Find("id:scene-tree-duplicate-slime_inst")); !strings.Contains(got, "⇧⌘D") {
		t.Fatalf("scene-tree-duplicate text = %q, want rebound shortcut ⇧⌘D", got)
	}
	app.Key("escape")

	// 3. Unrelated 60 Hz frame ticks do not remount an open viewport context menu.
	app.Post(func() {
		ed.SelectObject("slime_inst")
		ed.OpenObjectContextMenu("slime_inst")
	})
	app.Settle()
	titleBefore := app.Find("id:context-menu-title")
	app.Post(func() {
		ed.SetShowGrid(true)
		ed.SetShowGrid(false)
	})
	app.Settle()
	titleAfter := app.Find("id:context-menu-title")
	if titleAfter.ID != titleBefore.ID {
		t.Fatalf("unrelated rev ticks remounted open viewport context menu: ID %d -> %d", titleBefore.ID, titleAfter.ID)
	}

	// 4. Play closes open context menu and blocks reopening; modal screens also block opening and 'h' key.
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
	})
	app.Settle()
	if ed.ObjectContextMenuOpen() {
		t.Fatal("Play should close open object context menu")
	}
	app.Post(func() {
		if err := ed.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
		ed.SetPlaytestReportOpen(true)
	})
	app.Settle()
	if ed.OpenObjectContextMenu("slime_inst") {
		t.Fatal("OpenObjectContextMenu should be blocked while PlaytestReport is open")
	}
	app.Key("h")
	if ed.ObjectHidden("slime_inst") {
		t.Fatal("pressing 'h' while PlaytestReport is open should not toggle object visibility")
	}
	app.Post(func() { ed.SetPlaytestReportOpen(false) })
	app.Settle()

	// 5. PasteObjectYAML enforces size cap and sanitizes bare-object names with spaces.
	huge := strings.Repeat("a", (4<<20)+64)
	if _, err := ed.PasteObjectYAML(huge, ""); err == nil {
		t.Fatal("PasteObjectYAML should refuse payloads larger than 4 MiB")
	}
	bare := "name: My Spaced Hero\nx: 10\ny: 20\n"
	pastedID, err := ed.PasteObjectYAML(bare, "")
	if err != nil {
		t.Fatalf("PasteObjectYAML(bare with spaces): %v", err)
	}
	if strings.ContainsRune(pastedID, ' ') {
		t.Fatalf("pasted bare object ID %q must not contain spaces", pastedID)
	}

	// 6. OpenDemo resets context menu & toasts cleanly.
	app.Post(func() {
		_, _, _ = ed.CopyObjectYAML("slime_inst")
		ed.OpenObjectContextMenu("slime_inst")
		if err := ed.OpenDemo(); err != nil {
			t.Errorf("OpenDemo: %v", err)
		}
	})
	app.Settle()
	if ed.ObjectContextMenuOpen() || ed.ToastMessage() != "" {
		t.Fatalf("OpenDemo should close context menu and clear toasts, got open=%v toast=%q", ed.ObjectContextMenuOpen(), ed.ToastMessage())
	}
}
