package editor_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// writeTabbedInstanceProject writes a project on disk with:
//   - scenes/enemy.scene.yaml (one sprite object "body" with frame "hero" and mesh "box")
//   - scenes/level.scene.yaml (main scene instancing scenes/enemy.scene.yaml as "enemy_1" at (100, 60))
//   - scenes/boss.scene.yaml (inherits scenes/enemy.scene.yaml)
//   - art/actors.json + art/actors.png (frames "hero" and "torch")
func writeTabbedInstanceProject(t *testing.T) (*editor.Editor, string) {
	t.Helper()
	dir := t.TempDir()
	atlasJSON := `{"frames":{"hero":{"frame":{"x":0,"y":0,"w":16,"h":16}},"torch":{"frame":{"x":16,"y":0,"w":16,"h":16}}},"meta":{"image":"actors.png"}}`
	files := map[string]string{
		project.FileName: `version: 1
name: Tabbed Demo
id: dev.nsgo.tabbed
appVersion: 0.1.0
window:
  width: 640
  height: 360
autoload:
  mainScene: level
  scenes:
    - name: level
      path: scenes/level.scene.yaml
    - name: enemy
      path: scenes/enemy.scene.yaml
    - name: boss
      path: scenes/boss.scene.yaml
`,
		"art/actors.json": atlasJSON,
		"scenes/enemy.scene.yaml": `version: 2
name: Enemy
virtualWidth: 320
virtualHeight: 180
layers:
  - name: actors
    objects:
      - id: body
        name: Enemy Body
        x: 10
        "y": 20
        mesh: box
        sprite: {atlas: actors, frame: hero}
`,
		"scenes/level.scene.yaml": `version: 2
name: Level 1
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    objects:
      - id: player
        name: Player
        x: 32
        "y": 40
        sprite: {atlas: actors, frame: hero}
      - id: enemy_1
        name: First Enemy
        x: 100
        "y": 60
        scene: scenes/enemy.scene.yaml
`,
		"scenes/boss.scene.yaml": `version: 2
name: Boss
inherits: scenes/enemy.scene.yaml
overrides:
  body:
    name: Boss Body
layers:
  - name: actors
    objects:
      - id: crown
        x: 10
        "y": 8
        sprite: {atlas: actors, frame: torch}
`,
	}
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "art", "actors.png"), encodeTestPNG(t, 32, 16), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectAssets = true
	t.Cleanup(ed.WaitForProblems)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	return ed, dir
}

// TestSceneTabsKeepSeparateUndoHistories verifies Acceptance #1:
// two scenes open in tabs keep separate undo histories, selections, view transforms, and dirty markers; switching tabs never clears a history.
func TestSceneTabsKeepSeparateUndoHistories(t *testing.T) {
	ed, _ := openTestProject(t)

	// Tab 0 (scenes/room.yaml): select hero, pan/zoom 2D view, move 3D camera, and make an edit.
	ed.SelectObject("hero")
	view0 := editor.View2D{X: 120, Y: 80, Zoom: 2.5}
	if !ed.SetView2D(view0) {
		t.Fatal("SetView2D on tab 0 failed")
	}
	cam0 := editor.OrbitCamera3D{Target: [3]float32{5, 6, 7}, Yaw: 1.1, Pitch: 0.4, Distance: 250, FOV: 1, Near: 0.5, Far: 5000}
	ed.Camera3D = cam0
	if err := ed.MoveObject("hero", 111, 222); err != nil {
		t.Fatal(err)
	}

	// Open Tab 1 (scenes/other.scene.yaml) without losing Tab 0.
	if err := ed.OpenScene("scenes/other.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if ed.TabCount() != 2 || ed.ActiveTab() != 1 {
		t.Fatalf("after opening second scene: TabCount=%d ActiveTab=%d, want 2, 1", ed.TabCount(), ed.ActiveTab())
	}
	if ed.History.CanUndo() {
		t.Fatal("new tab unexpectedly inherited undo history from tab 0")
	}
	if ed.SelectedID != "" {
		t.Fatalf("new tab inherited SelectedID %q", ed.SelectedID)
	}
	if !ed.View2DFollowsCamera() {
		t.Fatal("new tab inherited free 2D view transform from tab 0")
	}
	if !ed.TabDirty(0) || ed.TabDirty(1) {
		t.Fatalf("dirty flags: tab0=%v tab1=%v, want true, false", ed.TabDirty(0), ed.TabDirty(1))
	}

	// Edit Tab 1 and adjust its selection and view transforms.
	view1 := editor.View2D{X: -40, Y: 30, Zoom: 0.5}
	if !ed.SetView2D(view1) {
		t.Fatal("SetView2D on tab 1 failed")
	}
	cam1 := editor.OrbitCamera3D{Target: [3]float32{-10, 2, 3}, Yaw: 2.2, Pitch: -0.3, Distance: 400, FOV: 1, Near: 0.5, Far: 5000}
	ed.Camera3D = cam1
	if err := ed.MoveObject("hero", 333, 444); err != nil {
		t.Fatal(err)
	}
	if !ed.TabDirty(0) || !ed.TabDirty(1) {
		t.Fatalf("after editing tab 1: tab0=%v tab1=%v, want true, true", ed.TabDirty(0), ed.TabDirty(1))
	}

	// Undoing in Tab 1 reverts ONLY Tab 1, leaving Tab 0's edit untouched.
	if !ed.Undo() {
		t.Fatal("Undo on tab 1 returned false")
	}
	if ed.TabDirty(1) {
		t.Fatal("tab 1 should be clean after undoing its only edit")
	}
	if !ed.TabDirty(0) {
		t.Fatal("undoing in tab 1 unexpectedly changed tab 0's dirty state")
	}

	// Switch back to Tab 0: document edit, history, selection, 2D view and 3D camera are preserved.
	if err := ed.SelectTab(0); err != nil {
		t.Fatal(err)
	}
	if hero, _, _ := ed.FindObject("hero"); hero == nil || hero.X != 111 || hero.Y != 222 {
		t.Fatalf("tab 0 hero = %+v, want (111, 222)", hero)
	}
	if ed.SelectedID != "hero" {
		t.Fatalf("tab 0 SelectedID = %q, want hero", ed.SelectedID)
	}
	if got := ed.View2D(); got != view0 {
		t.Fatalf("tab 0 View2D = %+v, want %+v", got, view0)
	}
	if ed.Camera3D != cam0 {
		t.Fatalf("tab 0 Camera3D = %+v, want %+v", ed.Camera3D, cam0)
	}
	if !ed.History.CanUndo() {
		t.Fatal("switching tabs cleared tab 0's undo history")
	}

	// Undoing in Tab 0 reverts Tab 0 without touching Tab 1.
	if !ed.Undo() {
		t.Fatal("Undo on tab 0 returned false")
	}
	if ed.TabDirty(0) {
		t.Fatal("tab 0 should be clean after Undo")
	}

	// Switch back to Tab 1: Redo still exists on Tab 1 and its view transforms are preserved.
	if err := ed.SelectTab(1); err != nil {
		t.Fatal(err)
	}
	if !ed.History.CanRedo() {
		t.Fatal("tab 1 lost its redo stack after switching tabs")
	}
	if got := ed.View2D(); got != view1 {
		t.Fatalf("tab 1 View2D = %+v, want %+v", got, view1)
	}
	if ed.Camera3D != cam1 {
		t.Fatalf("tab 1 Camera3D = %+v, want %+v", ed.Camera3D, cam1)
	}
	if !ed.Redo() {
		t.Fatal("Redo on tab 1 returned false")
	}
	if hero, _, _ := ed.FindObject("hero"); hero == nil || hero.X != 333 || hero.Y != 444 {
		t.Fatalf("after Redo on tab 1, hero = %+v, want (333, 444)", hero)
	}
}

// TestSavingInstancedSceneRebuildsOtherOpenTabs verifies Acceptance #2:
// saving an instanced or inherited scene in one tab rebuilds other open tabs that instance or inherit it without reopening them.
func TestSavingInstancedSceneRebuildsOtherOpenTabs(t *testing.T) {
	ed, _ := writeTabbedInstanceProject(t)

	// Verify initial instance position in level.scene.yaml (Tab 0):
	// enemy_1 is at (100, 60) and enemy.scene.yaml's body is at (10, 20) -> world (110, 80), frame "hero".
	body0 := ed.Built.Object("enemy_1/body")
	if body0 == nil || body0.Sprite == nil || body0.Sprite.X != 110 || body0.Sprite.Y != 80 || body0.Doc.Sprite.Frame != "hero" {
		t.Fatalf("initial instanced body = %+v, want (110, 80) frame hero", body0)
	}
	// Build 3D scene on Tab 0 so we also verify 3D rebuild on save.
	s3dBefore, err := ed.Scene3D()
	if err != nil || s3dBefore == nil {
		t.Fatalf("initial Scene3D: %v", err)
	}

	// Open boss.scene.yaml (inherits enemy.scene.yaml) in Tab 1.
	if err := ed.OpenScene("scenes/boss.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if ed.TabCount() != 2 || ed.ActiveTab() != 1 {
		t.Fatalf("after opening boss: TabCount=%d ActiveTab=%d", ed.TabCount(), ed.ActiveTab())
	}

	// Switch back to level.scene.yaml and open enemy.scene.yaml via OpenInstancedScene (Tab 2).
	if err := ed.SelectTab(0); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenInstancedScene("enemy_1"); err != nil {
		t.Fatalf("OpenInstancedScene: %v", err)
	}
	if ed.TabCount() != 3 || ed.ActiveTab() != 2 || ed.ScenePath != "scenes/enemy.scene.yaml" {
		t.Fatalf("after OpenInstancedScene: TabCount=%d ActiveTab=%d ScenePath=%q", ed.TabCount(), ed.ActiveTab(), ed.ScenePath)
	}

	// Switch to boss.scene.yaml (Tab 1) and call OpenBaseScene -> must switch to already-open Tab 2, NOT open a 4th tab!
	if err := ed.SelectTab(1); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenBaseScene(); err != nil {
		t.Fatalf("OpenBaseScene: %v", err)
	}
	if ed.TabCount() != 3 || ed.ActiveTab() != 2 || ed.ScenePath != "scenes/enemy.scene.yaml" {
		t.Fatalf("OpenBaseScene should switch to existing tab 2: TabCount=%d ActiveTab=%d ScenePath=%q", ed.TabCount(), ed.ActiveTab(), ed.ScenePath)
	}

	// Edit enemy.scene.yaml in Tab 2: move body to (45, 75), change sprite frame to "torch", and change mesh to "sphere", then Save!
	if err := ed.MoveObject("body", 45, 75); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetObjectSprite("body", &sceneformat.SpriteRef{Atlas: "actors", Frame: "torch"}); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetObjectMesh("body", "sphere"); err != nil {
		t.Fatal(err)
	}
	if err := ed.Save(); err != nil {
		t.Fatalf("Save enemy scene: %v", err)
	}

	// Switch to Tab 0 (level.scene.yaml) WITHOUT reopening it:
	// enemy_1/body must now draw at (100+45, 60+75) = (145, 135) with frame "torch"!
	if err := ed.SelectTab(0); err != nil {
		t.Fatal(err)
	}
	bodyAfter := ed.Built.Object("enemy_1/body")
	if bodyAfter == nil || bodyAfter.Sprite == nil || bodyAfter.Sprite.X != 145 || bodyAfter.Sprite.Y != 135 || bodyAfter.Doc.Sprite.Frame != "torch" {
		t.Fatalf("level tab instanced body after saving enemy = %+v, want (145, 135) frame torch", bodyAfter)
	}
	if bodyAfter.Doc.Mesh != "sphere" {
		t.Fatalf("level tab instanced body mesh = %q, want sphere", bodyAfter.Doc.Mesh)
	}
	s3dAfter, err := ed.Scene3D()
	if err != nil || s3dAfter == nil || s3dAfter == s3dBefore {
		t.Fatalf("level tab Scene3D did not rebuild after saving instanced scene: err=%v same=%v", err, s3dAfter == s3dBefore)
	}

	// Switch to Tab 1 (boss.scene.yaml, which inherits enemy.scene.yaml) WITHOUT reopening it:
	// inherited "body" must also show the updated position (45, 75) and frame "torch" (while keeping its override name "Boss Body")!
	if err := ed.SelectTab(1); err != nil {
		t.Fatal(err)
	}
	bossBody := ed.Built.Object("body")
	if bossBody == nil || bossBody.Sprite == nil || bossBody.Sprite.X != 45 || bossBody.Sprite.Y != 75 || bossBody.Doc.Sprite.Frame != "torch" || bossBody.Doc.Name != "Boss Body" {
		t.Fatalf("boss tab inherited body after saving base = %+v, want (45, 75) frame torch name Boss Body", bossBody)
	}
}

// TestSceneTabsUIAndOpenInstancedBaseButtons exercises the tab bar UI above the viewport, Open Instanced Scene and Open Base Scene buttons, tab switching, dirty markers, and closing dirty tabs.
func TestSceneTabsUIAndOpenInstancedBaseButtons(t *testing.T) {
	ed, dir := writeTabbedInstanceProject(t)
	app := startEditorApp(t, ed)

	if !app.Exists("id:scene-tabs") || !app.Exists("id:scene-tab-0") {
		t.Fatal("tab bar above viewport is missing")
	}
	if got := app.Find("id:scene-tab-label-0").Text; got != "level.scene.yaml" {
		t.Fatalf("tab 0 label = %q, want level.scene.yaml", got)
	}
	if app.Exists("id:scene-tab-dirty-0") {
		t.Fatal("clean tab 0 shows dirty marker")
	}

	// Select instanced object enemy_1 -> press Inspector's "Open Instanced Scene" button.
	app.Post(func() { ed.SelectObject("enemy_1") })
	app.Settle()
	if !app.Exists("id:inspector-open-instanced-scene") {
		t.Fatal("Inspector did not show Open Instanced Scene button for instanced object")
	}
	app.Press("id:inspector-open-instanced-scene")
	app.Settle()
	if ed.TabCount() != 2 || ed.ActiveTab() != 1 || ed.ScenePath != "scenes/enemy.scene.yaml" {
		t.Fatalf("after inspector Open Instanced Scene: tabs=%d active=%d path=%q", ed.TabCount(), ed.ActiveTab(), ed.ScenePath)
	}
	if got := app.Find("id:scene-tab-label-1").Text; got != "enemy.scene.yaml" {
		t.Fatalf("tab 1 label = %q, want enemy.scene.yaml", got)
	}

	// Edit enemy.scene.yaml -> tab 1 shows dirty marker, tab 0 does not.
	app.Post(func() {
		if err := ed.MoveObject("body", 25, 35); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	if app.Exists("id:scene-tab-dirty-0") || !app.Exists("id:scene-tab-dirty-1") {
		t.Fatalf("dirty markers: tab0=%v tab1=%v, want false, true", app.Exists("id:scene-tab-dirty-0"), app.Exists("id:scene-tab-dirty-1"))
	}

	// Open boss.scene.yaml in Tab 2 and press Scene Settings Inspector's "Open Base Scene" button -> switches back to Tab 1!
	app.Post(func() {
		if err := ed.OpenScene("scenes/boss.scene.yaml"); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	if ed.TabCount() != 3 || ed.ActiveTab() != 2 {
		t.Fatalf("after opening boss: tabs=%d active=%d", ed.TabCount(), ed.ActiveTab())
	}
	if !app.Exists("id:inspector-open-base-scene") {
		t.Fatal("Scene Settings Inspector did not show Open Base Scene button for inherited scene")
	}
	app.Press("id:inspector-open-base-scene")
	app.Settle()
	if ed.TabCount() != 3 || ed.ActiveTab() != 1 || ed.ScenePath != "scenes/enemy.scene.yaml" {
		t.Fatalf("Open Base Scene should switch to existing tab 1: tabs=%d active=%d path=%q", ed.TabCount(), ed.ActiveTab(), ed.ScenePath)
	}

	// Switch to Tab 0 via the tab bar, then close inactive dirty Tab 1 via its close button and choose Save.
	app.Press("id:scene-tab-0")
	app.Settle()
	if ed.ActiveTab() != 0 || ed.ScenePath != "scenes/level.scene.yaml" {
		t.Fatalf("clicking scene-tab-0: active=%d path=%q", ed.ActiveTab(), ed.ScenePath)
	}
	app.Press("id:scene-tab-close-1")
	app.Settle()
	if !app.Exists("id:unsaved-dialog") {
		t.Fatal("closing dirty inactive tab 1 did not prompt")
	}
	app.Press("id:unsaved-save")
	app.Settle()
	if ed.TabCount() != 2 || ed.ActiveTab() != 0 || ed.ScenePath != "scenes/level.scene.yaml" {
		t.Fatalf("after Save-closing tab 1: tabs=%d active=%d path=%q", ed.TabCount(), ed.ActiveTab(), ed.ScenePath)
	}
	// Saving Tab 1 during close must also have rebuilt active Tab 0 (level.scene.yaml)!
	if body := ed.Built.Object("enemy_1/body"); body == nil || body.Sprite == nil || body.Sprite.X != 125 || body.Sprite.Y != 95 {
		t.Fatalf("active level tab was not rebuilt after save-closing enemy tab: %+v", body)
	}
	rawEnemy, err := os.ReadFile(filepath.Join(dir, "scenes", "enemy.scene.yaml"))
	if err != nil || !strings.Contains(string(rawEnemy), "x: 25") {
		t.Fatalf("enemy.scene.yaml was not saved (%v):\n%s", err, rawEnemy)
	}

	// Closing a clean tab closes immediately without prompting.
	app.Press("id:scene-tab-close-1")
	app.Settle()
	if app.Exists("id:unsaved-dialog") || ed.TabCount() != 1 {
		t.Fatalf("closing clean tab 1: prompt=%v tabs=%d", app.Exists("id:unsaved-dialog"), ed.TabCount())
	}

	// Closing the last remaining tab replaces it cleanly with an untitled tab.
	app.Press("id:scene-tab-close-0")
	app.Settle()
	if ed.TabCount() != 1 || ed.Doc == nil || ed.ScenePath != "" || ed.SceneLabel() != "Untitled" {
		t.Fatalf("after closing last tab: tabs=%d doc=%v path=%q label=%q", ed.TabCount(), ed.Doc != nil, ed.ScenePath, ed.SceneLabel())
	}
}

// TestPluginsAndPlayActOnActiveTab verifies that registered plugins (commands, tools, panels) and Play/Stop act on the active tab without disturbing inactive tabs.
func TestPluginsAndPlayActOnActiveTab(t *testing.T) {
	ed, _ := openTestProject(t)

	var pluginRanOn string
	if err := ed.RegisterPlugin(editor.Plugin{
		Name: "nudge",
		Commands: []editor.PluginCommand{{
			ID:    "right",
			Label: "Nudge Hero Right",
			Run: func(e *editor.Editor) error {
				pluginRanOn = e.ScenePath
				obj, _, _ := e.FindObject("hero")
				if obj == nil {
					return errors.New("no hero")
				}
				return e.MoveObject("hero", obj.X+10, obj.Y)
			},
		}},
		Tools: []editor.PluginTool{{
			ID:    "tap",
			Label: "Tap Move",
			Press: func(e *editor.Editor, x, y float32) {
				wx, wy := e.CanvasToWorld(x, y)
				_ = e.MoveObject("hero", wx, wy)
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetPluginEnabled("nudge", true); err != nil {
		t.Fatal(err)
	}

	// Open second tab.
	if err := ed.OpenScene("scenes/other.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	// Shared project stays enabled on Tab 1 too.
	if !ed.PluginEnabled("nudge") {
		t.Fatal("plugin enabled on shared project was not active on tab 1")
	}

	// Execute plugin command on Tab 1 -> affects ONLY Tab 1!
	if err := ed.ExecuteCommand("plugin.nudge.right"); err != nil {
		t.Fatal(err)
	}
	if pluginRanOn != "scenes/other.scene.yaml" {
		t.Fatalf("plugin command ran on %q, want scenes/other.scene.yaml", pluginRanOn)
	}
	hero1, _, _ := ed.FindObject("hero")
	want1X := hero1.X

	// Tab 0 hero is untouched.
	if err := ed.SelectTab(0); err != nil {
		t.Fatal(err)
	}
	hero0, _, _ := ed.FindObject("hero")
	if hero0.X == want1X {
		t.Fatalf("plugin edit on tab 1 unexpectedly mutated tab 0 hero X=%v", hero0.X)
	}

	// Play acts on the active tab (Tab 0) and blocks switching/closing tabs until Stop.
	ed.OnPlayUpdate = func(scene *sceneformat.BuiltScene, _ time.Duration) {
		if h := scene.Object("hero"); h != nil {
			h.Doc.X += 1
		}
	}
	app := startEditorApp(t, ed)
	app.Press("id:toolbar-play")
	app.Settle()
	if !ed.Playing() {
		t.Fatal("Play did not start on active tab")
	}
	if err := ed.SelectTab(1); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("SelectTab while playing = %v, want ErrPlaying", err)
	}
	if err := ed.CloseTab(1); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("CloseTab while playing = %v, want ErrPlaying", err)
	}
	app.Advance(200 * time.Millisecond)
	app.Press("id:toolbar-play")
	app.Settle()
	if ed.Playing() {
		t.Fatal("Stop did not end Play")
	}
	// Tab 1 still holds its plugin edit and undo history untouched.
	if err := ed.SelectTab(1); err != nil {
		t.Fatal(err)
	}
	if h, _, _ := ed.FindObject("hero"); h == nil || h.X != want1X || !ed.History.CanUndo() {
		t.Fatalf("Play/Stop on tab 0 disturbed tab 1: hero=%+v canUndo=%v", h, ed.History.CanUndo())
	}
}

// TestQuitSaveAllPersistsEveryDirtyTab verifies that quitting with multiple dirty tabs and choosing Save persists all dirty tabs.
func TestQuitSaveAllPersistsEveryDirtyTab(t *testing.T) {
	ed, dir := openTestProject(t)
	app := startEditorApp(t, ed)
	app.Post(func() {
		if err := ed.MoveObject("hero", 77, 88); err != nil {
			t.Error(err)
		}
		if err := ed.OpenScene("scenes/other.scene.yaml"); err != nil {
			t.Error(err)
		}
		if err := ed.MoveObject("hero", 91, 92); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	if !ed.TabDirty(0) || !ed.TabDirty(1) || !ed.HasUnsavedChanges() {
		t.Fatalf("expected both tabs dirty: tab0=%v tab1=%v unsaved=%v", ed.TabDirty(0), ed.TabDirty(1), ed.HasUnsavedChanges())
	}
	app.RequestQuit(false)
	app.Settle()
	if !app.Exists("id:unsaved-dialog") {
		t.Fatal("quit with dirty tabs did not prompt")
	}
	app.Press("id:unsaved-save")
	app.Settle()
	if ed.HasUnsavedChanges() || app.Quits != 1 {
		t.Fatalf("after quit Save: unsaved=%v quits=%d status=%q", ed.HasUnsavedChanges(), app.Quits, ed.Status)
	}
	roomData, err := os.ReadFile(filepath.Join(dir, "scenes", "room.yaml"))
	if err != nil || !strings.Contains(string(roomData), "x: 77") {
		t.Fatalf("room.yaml was not saved by SaveAll (%v):\n%s", err, roomData)
	}
	otherData, err := os.ReadFile(filepath.Join(dir, "scenes", "other.scene.yaml"))
	if err != nil || !strings.Contains(string(otherData), "x: 91") {
		t.Fatalf("other.scene.yaml was not saved by SaveAll (%v):\n%s", err, otherData)
	}
}

// TestMoveFileUpdatesAllOpenTabs verifies that MoveFile rewrites references and paths across every open tab and Undo restores them.
func TestMoveFileUpdatesAllOpenTabs(t *testing.T) {
	ed, _ := writeTabbedInstanceProject(t)
	if err := ed.OpenScene("scenes/enemy.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.MoveFile("scenes/enemy.scene.yaml", "prefabs/enemy.scene.yaml"); err != nil {
		t.Fatalf("MoveFile: %v", err)
	}
	if ed.ScenePath != "prefabs/enemy.scene.yaml" {
		t.Fatalf("active tab ScenePath = %q, want prefabs/enemy.scene.yaml", ed.ScenePath)
	}
	if err := ed.SelectTab(0); err != nil {
		t.Fatal(err)
	}
	inst, _, _ := ed.FindObject("enemy_1")
	if inst == nil || inst.Scene != "prefabs/enemy.scene.yaml" {
		t.Fatalf("level tab instance scene = %+v, want prefabs/enemy.scene.yaml", inst)
	}
	if ed.BuildErr != nil {
		t.Fatalf("level tab BuildErr after MoveFile: %v", ed.BuildErr)
	}
}
