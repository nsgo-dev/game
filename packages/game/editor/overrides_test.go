package editor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func setupOverrideProject(t *testing.T, extraFiles map[string]string) (string, *editor.Editor) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		project.FileName: `version: 1
name: Override Demo
id: dev.nsgo.overrides
appVersion: 0.1.0
window:
  width: 640
  height: 360
autoload:
  mainScene: level
  scenes:
    - name: level
      path: scenes/level.scene.yaml
    - name: slime
      path: scenes/slime.scene.yaml
    - name: base
      path: scenes/base.scene.yaml
`,
		"scenes/slime.scene.yaml": `version: 2
layers:
  - name: actors
    objects:
      - id: body
        name: Slime Body
        x: 10
        "y": 20
        components:
          health:
            hp: 30
            armor: 2
      - id: eye
        x: 4
        "y": 2
connections:
  - from: body
    event: hit
    to: eye
    handler: blink
`,
		"scenes/base.scene.yaml": `version: 3
layers:
  - name: ground
    objects:
      - id: hero
        name: Hero
        x: 40
        "y": 50
        components:
          health:
            hp: 100
            armor: 5
  - name: sky
    parallaxX: 0.5
    objects:
      - id: cloud
        x: 120
        "y": 30
`,
		"scenes/level.scene.yaml": `version: 2
layers:
  - name: actors
    objects:
      - id: marker
        x: 0
        "y": 0
`,
	}
	for k, v := range extraFiles {
		files[k] = v
	}
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.ProjectAssets = true
	t.Cleanup(ed.WaitForProblems)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	return dir, ed
}

// Acceptance #1:
// Moving an instanced child writes one override keyed by its ID inside the instanced scene,
// the saved file keeps the scene reference, and Revert removes the override.
func TestAcceptance1MoveInstancedChildWritesOverrideAndReverts(t *testing.T) {
	_, ed := setupOverrideProject(t, nil)
	ed.SetActiveLayer("actors")

	app := startEditorView(t, ed)

	// Place a scene instance via the File Browser quick-place button.
	app.Press("id:files-place-scenes/slime.scene.yaml")
	if ed.BuildErr != nil {
		t.Fatalf("BuildErr after placing scene from file browser: %v", ed.BuildErr)
	}
	instID := ed.SelectedID
	if instID == "" {
		t.Fatal("expected placed scene instance to be selected")
	}
	instObj, _, _ := ed.FindObject(instID)
	if instObj == nil || instObj.Scene != "scenes/slime.scene.yaml" {
		t.Fatalf("placed object = %+v, want scene reference scenes/slime.scene.yaml", instObj)
	}

	preEditYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}

	// Verify instanced child is visible in scene tree and styled as inherited.
	bodyID := instID + "/body"
	app.Post(func() { ed.SelectObject(bodyID) })
	app.Settle()
	app.Find("id:scene-tree-row-" + bodyID)
	app.Find("id:scene-tree-inherited-" + bodyID)
	app.Find("id:inspector-instance-bar")
	if app.Enabled("id:inspector-delete") {
		t.Fatal("Delete button must be disabled for instanced children")
	}

	// Verify viewport HitTest can pick the instanced child directly.
	bodyBuilt := ed.Built.Object(bodyID)
	if bodyBuilt == nil {
		t.Fatalf("resolved scene missing %q", bodyID)
	}
	wx, wy := bodyBuilt.World.Tx, bodyBuilt.World.Ty
	cx, cy := ed.WorldToCanvas(wx, wy)
	if hit := ed.HitTest(cx, cy); hit != bodyID {
		t.Fatalf("HitTest at instanced child (%v, %v) = %q, want %q", cx, cy, hit, bodyID)
	}

	// Move the instanced child via 2D viewport gizmo drag (one undo step).
	if h := ed.BeginDrag2D(cx, cy); h == editor.Handle2DNone {
		t.Fatalf("BeginDrag2D on instanced child %q returned Handle2DNone", bodyID)
	}
	tx, ty := ed.WorldToCanvas(wx+24, wy-12)
	ed.UpdateDrag2D(tx, ty, false)
	if !ed.EndDrag2D() {
		t.Fatal("EndDrag2D on instanced child returned false")
	}
	app.Settle()

	// Verify one override keyed by "body" inside the instanced scene.
	instObj, _, _ = ed.FindObject(instID)
	if len(instObj.Overrides) != 1 {
		t.Fatalf("instance overrides count = %d (%+v), want 1 keyed by \"body\"", len(instObj.Overrides), instObj.Overrides)
	}
	ov, ok := instObj.Overrides["body"]
	if !ok {
		t.Fatalf("instance overrides keys = %+v, want key \"body\"", instObj.Overrides)
	}
	if ov.X == nil || ov.Y == nil {
		t.Fatalf("body override = %+v, want X and Y set", ov)
	}

	// Saved YAML must keep the scene reference and round-trip cleanly.
	saved, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	if !bytes.Contains(saved, []byte("scene: scenes/slime.scene.yaml")) {
		t.Fatalf("saved YAML lost scene reference:\n%s", saved)
	}
	if !bytes.Contains(saved, []byte("overrides:")) || !bytes.Contains(saved, []byte("body:")) {
		t.Fatalf("saved YAML missing overrides.body:\n%s", saved)
	}
	reloaded, err := sceneformat.Parse(saved)
	if err != nil {
		t.Fatalf("Parse saved YAML: %v", err)
	}
	if again, err := sceneformat.Encode(reloaded); err != nil || !bytes.Equal(again, saved) {
		t.Fatalf("saved YAML failed round-trip (err %v)", err)
	}

	// Inspector marks overridden fields and provides per-field Revert and Revert All.
	app.Find("id:inspector-override-dot-x")
	app.Find("id:inspector-revert-x")
	app.Find("id:inspector-override-dot-y")
	app.Find("id:inspector-revert-y")

	// Per-field Revert removes X while leaving Y.
	app.Press("id:inspector-revert-x")
	if ed.HasObjectOverride(bodyID, "x") {
		t.Fatal("expected x override to be removed after per-field Revert")
	}
	if !ed.HasObjectOverride(bodyID, "y") {
		t.Fatal("expected y override to remain after reverting only x")
	}

	// Revert All removes remaining overrides and cleans up empty overrides map.
	app.Press("id:inspector-revert-all")
	instObj, _, _ = ed.FindObject(instID)
	if instObj.Overrides != nil {
		t.Fatalf("Overrides after Revert All = %+v, want nil", instObj.Overrides)
	}
	afterRevert, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(afterRevert, []byte("overrides:")) {
		t.Fatalf("saved YAML should not contain empty overrides block after Revert:\n%s", afterRevert)
	}
	if !bytes.Equal(afterRevert, preEditYAML) {
		t.Fatalf("YAML after Revert does not match pre-edit YAML:\ngot:\n%s\nwant:\n%s", afterRevert, preEditYAML)
	}

	// Undo restores the override, and undoing all three steps restores preEditYAML.
	if !ed.Undo() {
		t.Fatal("Undo Revert All failed")
	}
	if !ed.HasObjectOverride(bodyID, "y") {
		t.Fatal("Undo Revert All did not restore y override")
	}
	ed.Undo() // undo revert x
	ed.Undo() // undo drag move
	if afterUndo, err := ed.SaveYAML(); err != nil || !bytes.Equal(afterUndo, preEditYAML) {
		t.Fatalf("Undo did not restore pre-edit YAML (err %v):\n%s", err, afterUndo)
	}
}

// Acceptance #2:
// In a scene that inherits another, changing an inherited object's component field
// writes overrides only, and the base file is unchanged.
func TestAcceptance2InheritedSceneComponentOverrideLeavesBaseUnchanged(t *testing.T) {
	dir, ed := setupOverrideProject(t, map[string]string{
		"scenes/level.scene.yaml": `version: 3
inherits: scenes/base.scene.yaml
layers:
  - name: ground
`,
	})
	basePath := filepath.Join(dir, "scenes", "base.scene.yaml")
	baseBefore, err := os.ReadFile(basePath)
	if err != nil {
		t.Fatal(err)
	}

	app := startEditorView(t, ed)

	// Inherited object "hero" appears in the scene tree and is selectable.
	app.Post(func() { ed.SelectObject("hero") })
	app.Settle()
	app.Find("id:scene-tree-row-hero")
	app.Find("id:scene-tree-inherited-hero")
	app.Find("id:inspector-instance-bar")
	app.Find("id:inspector-open-base-scene")

	if ed.ObjectOrigin("hero") != editor.ObjectInherited {
		t.Fatalf("ObjectOrigin(hero) = %v, want ObjectInherited", ed.ObjectOrigin("hero"))
	}

	// Change component field health.hp on inherited object hero.
	if err := ed.SetComponentField("hero", "health", "hp", 250); err != nil {
		t.Fatalf("SetComponentField on inherited hero: %v", err)
	}
	app.Settle()

	// Verify overrides map on SceneDoc, resolved value, and field-level override detection.
	if len(ed.Doc.Overrides) != 1 {
		t.Fatalf("Doc.Overrides = %+v, want 1 entry for hero", ed.Doc.Overrides)
	}
	if !ed.HasObjectOverride("hero", "component:health.hp") {
		t.Fatal("expected HasObjectOverride(hero, component:health.hp) = true")
	}
	if ed.HasObjectOverride("hero", "component:health.armor") {
		t.Fatal("expected unchanged sibling field component:health.armor not to be marked overridden")
	}
	if got := ed.ObjectBaseValue("hero", "component:health.hp"); got != "100" {
		t.Fatalf("ObjectBaseValue(hero, component:health.hp) = %q, want \"100\"", got)
	}
	resolvedHero, _ := ed.FindResolvedObject("hero")
	if resolvedHero == nil {
		t.Fatal("FindResolvedObject(hero) returned nil")
	}
	hpMap, _ := resolvedHero.Components["health"].(map[string]any)
	if hpMap["hp"] != 250 || hpMap["armor"] != 5 {
		t.Fatalf("resolved hero.health = %+v, want hp=250 armor=5", hpMap)
	}

	// Save the inheriting scene and verify overrides-only YAML + unchanged base file.
	if err := ed.Save(); err != nil {
		t.Fatalf("Save inheriting scene: %v", err)
	}
	levelBytes, err := os.ReadFile(filepath.Join(dir, "scenes", "level.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(levelBytes, []byte("inherits: scenes/base.scene.yaml")) || !bytes.Contains(levelBytes, []byte("overrides:")) {
		t.Fatalf("saved inheriting scene missing inherits/overrides:\n%s", levelBytes)
	}
	if bytes.Contains(levelBytes, []byte("- id: hero")) {
		t.Fatalf("saved inheriting scene should write overrides only, not duplicate hero in layers:\n%s", levelBytes)
	}
	baseAfter, err := os.ReadFile(basePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baseBefore, baseAfter) {
		t.Fatalf("base scene file was modified!\nbefore:\n%s\nafter:\n%s", baseBefore, baseAfter)
	}

	// Inspector shows override marker and per-field Revert clears the override cleanly.
	app.Find("id:inspector-override-dot-component:health.hp")
	app.Press("id:inspector-revert-component:health.hp")
	if ed.Doc.Overrides != nil {
		t.Fatalf("Doc.Overrides after per-field revert = %+v, want nil", ed.Doc.Overrides)
	}
	resolvedHero, _ = ed.FindResolvedObject("hero")
	hpMap, _ = resolvedHero.Components["health"].(map[string]any)
	if hpMap["hp"] != 100 {
		t.Fatalf("resolved hero.health.hp after revert = %v, want 100", hpMap["hp"])
	}
}

func TestMakeInstanceLocalSingleUndoStep(t *testing.T) {
	_, ed := setupOverrideProject(t, nil)
	ed.SetActiveLayer("actors")
	instID, err := ed.PlaceScene("actors", "scenes/slime.scene.yaml", 80, 60)
	if err != nil {
		t.Fatalf("PlaceScene: %v", err)
	}
	bodyID := instID + "/body"
	if err := ed.MoveObject(bodyID, 25, 35); err != nil {
		t.Fatalf("MoveObject instanced child: %v", err)
	}
	// Attach a local child under the instance root too; Make Local must keep it.
	localChildID, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "hat", X: 0, Y: -10})
	if err != nil {
		t.Fatalf("PlaceObject hat: %v", err)
	}
	if err := ed.ReparentObject(localChildID, instID, -1); err != nil {
		t.Fatalf("ReparentObject hat under instance: %v", err)
	}
	beforeLocal, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	historyBefore := ed.History.Len()

	app := startEditorView(t, ed)
	app.Post(func() { ed.SelectObject(instID) })
	app.Settle()
	app.Press("id:inspector-make-local")

	if ed.History.Len() != historyBefore+1 {
		t.Fatalf("MakeInstanceLocal recorded %d history steps, want 1", ed.History.Len()-historyBefore)
	}
	root, _, _ := ed.FindObject(instID)
	if root == nil || root.Scene != "" || root.Overrides != nil {
		t.Fatalf("root after Make Local = %+v, want empty Scene and nil Overrides", root)
	}
	body, _, _ := ed.FindObject(bodyID)
	if body == nil || body.X != 25 || body.Y != 35 {
		t.Fatalf("local body after Make Local = %+v, want local object at (25, 35)", body)
	}
	if hat, _, _ := ed.FindObject("hat"); hat == nil {
		t.Fatal("local child 'hat' was lost during MakeInstanceLocal")
	}
	// Prefixed connection from slime.scene.yaml should be preserved locally.
	foundConn := false
	for _, c := range ed.Doc.Connections {
		if c.From == bodyID && c.To == instID+"/eye" && c.Event == "hit" && c.Handler == "blink" {
			foundConn = true
		}
	}
	if !foundConn {
		t.Fatalf("connections after Make Local = %+v, want prefixed hit->blink connection", ed.Doc.Connections)
	}

	// Single Undo restores the exact scene reference, overrides, children, and connections.
	if !ed.Undo() {
		t.Fatal("Undo MakeInstanceLocal failed")
	}
	afterUndo, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterUndo, beforeLocal) {
		t.Fatalf("Undo MakeInstanceLocal did not restore YAML:\ngot:\n%s\nwant:\n%s", afterUndo, beforeLocal)
	}
}

func TestApplyOverridesToSourceSingleUndoStep(t *testing.T) {
	dir, ed := setupOverrideProject(t, nil)
	slimePath := filepath.Join(dir, "scenes", "slime.scene.yaml")
	slimeOriginal, err := os.ReadFile(slimePath)
	if err != nil {
		t.Fatal(err)
	}

	ed.SetActiveLayer("actors")
	inst1, err := ed.PlaceScene("actors", "scenes/slime.scene.yaml", 50, 50)
	if err != nil {
		t.Fatalf("PlaceScene 1: %v", err)
	}
	inst2, err := ed.PlaceScene("actors", "scenes/slime.scene.yaml", 150, 50)
	if err != nil {
		t.Fatalf("PlaceScene 2: %v", err)
	}
	if count, scenes := ed.CountSceneInstances("scenes/slime.scene.yaml"); count != 2 || scenes != 1 {
		t.Fatalf("CountSceneInstances = (%d, %d), want (2, 1)", count, scenes)
	}

	// Open slime.scene.yaml in another tab (clean) to verify live tab sync, then switch back to level.
	if err := ed.OpenInstancedScene(inst1); err != nil {
		t.Fatalf("OpenInstancedScene: %v", err)
	}
	if ed.ScenePath != "scenes/slime.scene.yaml" {
		t.Fatalf("active tab = %q, want scenes/slime.scene.yaml", ed.ScenePath)
	}
	if err := ed.SelectTab(0); err != nil {
		t.Fatalf("SelectTab(0): %v", err)
	}

	// Override body position and component on instance 1.
	if err := ed.MoveObject(inst1+"/body", 99, 77); err != nil {
		t.Fatalf("MoveObject: %v", err)
	}
	if err := ed.SetComponentField(inst1+"/body", "health", "hp", 88); err != nil {
		t.Fatalf("SetComponentField: %v", err)
	}
	levelBeforeApply, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	historyBefore := ed.History.Len()

	app := startEditorView(t, ed)
	app.Post(func() { ed.SelectObject(inst1) })
	app.Settle()
	app.Press("id:inspector-apply-to-source")

	if ed.History.Len() != historyBefore+1 {
		t.Fatalf("ApplyOverridesToSource recorded %d history steps, want 1", ed.History.Len()-historyBefore)
	}
	// Instance 1 overrides should now be cleared.
	obj1, _, _ := ed.FindObject(inst1)
	if obj1.Overrides != nil {
		t.Fatalf("inst1.Overrides after Apply = %+v, want nil", obj1.Overrides)
	}
	// Source file on disk should hold the applied values.
	slimeAfterApply, err := os.ReadFile(slimePath)
	if err != nil {
		t.Fatal(err)
	}
	parsedSlime, err := sceneformat.Parse(slimeAfterApply)
	if err != nil {
		t.Fatalf("Parse updated slime: %v", err)
	}
	if parsedSlime.Layers[0].Objects[0].X != 99 || parsedSlime.Layers[0].Objects[0].Y != 77 {
		t.Fatalf("updated source body pos = (%v, %v), want (99, 77)", parsedSlime.Layers[0].Objects[0].X, parsedSlime.Layers[0].Objects[0].Y)
	}
	hpMap, _ := parsedSlime.Layers[0].Objects[0].Components["health"].(map[string]any)
	if hpMap["hp"] != 88 {
		t.Fatalf("updated source body hp = %v, want 88", hpMap["hp"])
	}
	// Instance 2 should automatically see the new source values after rebuild!
	if body2, _ := ed.FindResolvedObject(inst2 + "/body"); body2 == nil || body2.X != 99 || body2.Y != 77 {
		t.Fatalf("inst2/body after Apply = %+v, want X=99 Y=77 from updated source", body2)
	}

	// Single Undo restores BOTH the source file on disk byte-for-byte AND instance 1's overrides!
	if !ed.Undo() {
		t.Fatal("Undo ApplyOverridesToSource failed")
	}
	slimeAfterUndo, err := os.ReadFile(slimePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(slimeAfterUndo, slimeOriginal) {
		t.Fatalf("Undo ApplyOverridesToSource did not restore source file:\ngot:\n%s\nwant:\n%s", slimeAfterUndo, slimeOriginal)
	}
	levelAfterUndo, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(levelAfterUndo, levelBeforeApply) {
		t.Fatalf("Undo ApplyOverridesToSource did not restore level YAML:\ngot:\n%s\nwant:\n%s", levelAfterUndo, levelBeforeApply)
	}

	// Redo re-applies to source cleanly.
	if !ed.Redo() {
		t.Fatal("Redo ApplyOverridesToSource failed")
	}
	if obj1, _, _ = ed.FindObject(inst1); obj1.Overrides != nil {
		t.Fatalf("inst1.Overrides after Redo Apply = %+v, want nil", obj1.Overrides)
	}
	if body2, _ := ed.FindResolvedObject(inst2 + "/body"); body2 == nil || body2.X != 99 {
		t.Fatalf("inst2/body after Redo Apply = %+v, want X=99", body2)
	}
}

func TestPlacePrefabFromAssetBrowserAndCanvas(t *testing.T) {
	ed := newTestEditor(t)
	ed.Prefabs = map[string]editor.Prefab{
		"coin": {
			Name:   "coin",
			Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"},
		},
	}
	ed.SetActiveLayer("actors")

	app := startEditorView(t, ed)
	app.Find("id:prefab-list")
	app.Press("id:prefab-place-coin")
	if ed.SelectedID == "" {
		t.Fatal("expected prefab-place-coin to place and select an object")
	}
	placed, _, _ := ed.FindObject(ed.SelectedID)
	if placed == nil || placed.Prefab != "coin" {
		t.Fatalf("placed prefab object = %+v, want prefab=coin", placed)
	}

	// Clicking the prefab row selects it and activates ToolPlace so a canvas press places another.
	app.Press("id:prefab-coin")
	if ed.SelectedPrefab != "coin" || ed.Tool != editor.ToolPlace {
		t.Fatalf("after clicking prefab-coin: SelectedPrefab=%q Tool=%v, want coin / ToolPlace", ed.SelectedPrefab, ed.Tool)
	}
	beforeCount := len(ed.Doc.Layers[1].Objects)
	ed.SetCanvasSize(320, 180)
	cx, cy := ed.WorldToCanvas(120, 80)
	ed.HandleCanvasPress(cx, cy)
	if len(ed.Doc.Layers[1].Objects) != beforeCount+1 {
		t.Fatalf("canvas press with SelectedPrefab did not place prefab: count=%d, want %d", len(ed.Doc.Layers[1].Objects), beforeCount+1)
	}
	second := ed.Doc.Layers[1].Objects[len(ed.Doc.Layers[1].Objects)-1]
	if second.Prefab != "coin" {
		t.Fatalf("second placed object prefab = %q, want coin", second.Prefab)
	}
}

func TestOverrideRefusedWhenResolveFails(t *testing.T) {
	dir, ed := setupOverrideProject(t, nil)
	ed.SetActiveLayer("actors")
	instID, err := ed.PlaceScene("actors", "scenes/slime.scene.yaml", 20, 20)
	if err != nil {
		t.Fatalf("PlaceScene: %v", err)
	}
	bodyID := instID + "/body"

	// Invalid override value (alpha out of [0, 1]) is refused by validation/resolve.
	historyBefore := ed.History.Len()
	if err := ed.SetObjectAlpha(bodyID, 2.5); err == nil {
		t.Fatal("expected SetObjectAlpha(2.5) override to be refused")
	}
	if ed.History.Len() != historyBefore {
		t.Fatal("refused override polluted History")
	}

	// Corrupt the source file on disk and rebuild so Resolve fails -> edits must be refused.
	if err := os.WriteFile(filepath.Join(dir, "scenes", "slime.scene.yaml"), []byte("version: 999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ed.ReloadAssets()
	if ed.BuildErr == nil {
		t.Fatal("expected BuildErr after corrupting source scene file")
	}
	if err := ed.MoveObject(bodyID, 100, 100); err == nil || !strings.Contains(err.Error(), "resolve") {
		t.Fatalf("MoveObject when Resolve fails = %v, want resolve refusal", err)
	}
	if ed.History.Len() != historyBefore {
		t.Fatal("refused override during broken resolve polluted History")
	}
}

func TestOpenSourceSceneFromInstancedChildAndInheritedObject(t *testing.T) {
	_, ed := setupOverrideProject(t, map[string]string{
		"scenes/level.scene.yaml": `version: 3
inherits: scenes/base.scene.yaml
layers:
  - name: actors
    objects:
      - id: slime_1
        scene: scenes/slime.scene.yaml
`,
	})
	app := startEditorView(t, ed)

	// Open instanced scene from instanced child via Inspector button.
	app.Post(func() { ed.SelectObject("slime_1/body") })
	app.Settle()
	app.Press("id:inspector-open-instanced-scene")
	if ed.ScenePath != "scenes/slime.scene.yaml" {
		t.Fatalf("after Open Instanced Scene from child: ScenePath = %q, want scenes/slime.scene.yaml", ed.ScenePath)
	}

	// Switch back to level and open base scene from inherited object via Inspector button.
	app.Post(func() { _ = ed.SelectTab(0); ed.SelectObject("hero") })
	app.Settle()
	app.Press("id:inspector-open-base-scene")
	if ed.ScenePath != "scenes/base.scene.yaml" {
		t.Fatalf("after Open Base Scene from inherited object: ScenePath = %q, want scenes/base.scene.yaml", ed.ScenePath)
	}
}

func TestFileBrowserPlaceSceneButton(t *testing.T) {
	_, ed := setupOverrideProject(t, nil)
	ed.SetActiveLayer("actors")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))

	// Select slime scene in File Browser, then press the header Place button.
	app.Press("id:files-row-scenes/slime.scene.yaml")
	if !app.Enabled("id:files-place-scene") {
		t.Fatal("files-place-scene should be enabled when another scene file is selected")
	}
	app.Press("id:files-place-scene")
	obj, _, _ := ed.FindObject(ed.SelectedID)
	if obj == nil || obj.Scene != "scenes/slime.scene.yaml" {
		t.Fatalf("object placed via files-place-scene = %+v, want scenes/slime.scene.yaml", obj)
	}
}
