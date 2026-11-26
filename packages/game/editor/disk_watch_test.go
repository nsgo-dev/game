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
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func waitForDiskCondition(t *testing.T, app *nsgotest.App, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if app != nil {
			app.Settle()
		}
		if cond() {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	if app != nil {
		app.Settle()
	}
	if !cond() {
		t.Fatalf("timed out waiting for %s", desc)
	}
}

// TestDiskWatchReloadsCleanSceneAndRefusesOverwriteWhenDirty verifies issue #906 acceptance criterion 1:
// without unsaved edits, an external disk change reloads into the editor automatically;
// with unsaved edits, Save refuses to overwrite the file on disk and asks before overwriting.
func TestDiskWatchReloadsCleanSceneAndRefusesOverwriteWhenDirty(t *testing.T) {
	ed, dir := openTestProject(t)
	ed.DiskWatchInterval = 20 * time.Millisecond
	app := startEditorApp(t, ed)

	scenePath := filepath.Join(dir, "scenes", "room.yaml")

	// 1. Clean scene: rewrite on disk -> reloads automatically into the editor.
	extDoc := testSceneDoc()
	extDoc.Name = "Reloaded Room"
	extDoc.Layers[1].Objects[0].X = 123
	extDoc.Layers[1].Objects = append(extDoc.Layers[1].Objects, sceneformat.ObjectDoc{
		ID: "chest",
		X:  200,
		Y:  90,
	})
	extBytes, err := sceneformat.Encode(extDoc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scenePath, extBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	waitForDiskCondition(t, app, 2*time.Second, "clean scene automatic reload", func() bool {
		obj, _, _ := ed.FindObject("hero")
		chest, _, _ := ed.FindObject("chest")
		return ed.Doc.Name == "Reloaded Room" && obj != nil && obj.X == 123 && chest != nil
	})
	if ed.SceneDirty() || ed.HasUnsavedChanges() {
		t.Fatalf("reloaded clean scene reported dirty=%v unsaved=%v", ed.SceneDirty(), ed.HasUnsavedChanges())
	}
	if !app.Exists("id:disk-reload-notice") {
		t.Fatal("expected quiet reload notice after automatic reload")
	}
	if app.Exists("id:disk-reload-show-diff") {
		app.Press("id:disk-reload-show-diff")
		app.Settle()
		if !app.Exists("id:disk-reload-diff") {
			t.Fatal("expected diff details after pressing Show diff")
		}
	}
	app.Press("id:disk-reload-dismiss")
	app.Settle()

	// 2. Dirty scene: edit open scene, rewrite file on disk -> Save refuses and asks before overwriting.
	app.Post(func() {
		if err := ed.MoveObject("hero", 77, 88); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	if !ed.SceneDirty() {
		t.Fatal("expected scene to be dirty after MoveObject")
	}

	diskDoc2 := extDoc.Clone()
	diskDoc2.Name = "External Winner"
	diskDoc2.Layers[1].Objects[0].X = 321
	diskBytes2, err := sceneformat.Encode(diskDoc2)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scenePath, diskBytes2, 0o644); err != nil {
		t.Fatal(err)
	}

	// Programmatic Save refuses with ErrDiskConflict without overwriting disk.
	var saveErr error
	app.Post(func() {
		saveErr = ed.Save()
	})
	app.Settle()
	if !errors.Is(saveErr, editor.ErrDiskConflict) {
		t.Fatalf("Save() over externally modified file = %v, want ErrDiskConflict", saveErr)
	}
	onDisk, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != string(diskBytes2) {
		t.Fatalf("Save() overwrote externally modified file:\n%s", onDisk)
	}
	if !app.Exists("id:save-conflict-dialog") {
		t.Fatal("expected save-conflict-dialog to ask before overwriting")
	}

	// Cancel keeps both disk file and editor edits.
	app.Press("id:save-conflict-cancel")
	app.Settle()
	if app.Exists("id:save-conflict-dialog") || !ed.SceneDirty() {
		t.Fatalf("after Cancel: dialog=%v dirty=%v", app.Exists("id:save-conflict-dialog"), ed.SceneDirty())
	}
	if obj, _, _ := ed.FindObject("hero"); obj == nil || obj.X != 77 {
		t.Fatalf("after Cancel: hero=%+v, want X=77", obj)
	}

	// Saving via File menu asks again; confirming Overwrite writes editor's version.
	saveInUI(app, ed)
	if !app.Exists("id:save-conflict-dialog") {
		t.Fatal("pressing toolbar Save over conflict did not ask")
	}
	app.Press("id:save-conflict-overwrite")
	app.Settle()
	if app.Exists("id:save-conflict-dialog") || ed.SceneDirty() {
		t.Fatalf("after Overwrite: dialog=%v dirty=%v status=%q", app.Exists("id:save-conflict-dialog"), ed.SceneDirty(), ed.Status)
	}
	afterOverwrite, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(afterOverwrite), "x: 77") {
		t.Fatalf("after Overwrite file does not contain editor's edit:\n%s", afterOverwrite)
	}
}

// TestDiskConflictReloadAndKeepMine verifies that when a dirty scene changes on disk, nothing reloads
// under the author, Reload keeps the author's edits in History ("Unsaved edits before reload"),
// and Keep mine keeps the disk version in History ("Changed outside the editor") and allows Save.
func TestDiskConflictReloadAndKeepMine(t *testing.T) {
	ed, dir := openTestProject(t)
	ed.DiskWatchInterval = 20 * time.Millisecond
	// Autosave saves kept edits after AutoSaveDelay; push it past the test so a slow runner never saves the dirty scene between steps.
	ed.AutoSaveDelay = time.Hour
	app := startEditorApp(t, ed)
	scenePath := filepath.Join(dir, "scenes", "room.yaml")

	app.Post(func() {
		if err := ed.MoveObject("hero", 55, 66); err != nil {
			t.Error(err)
		}
	})
	app.Settle()

	ext := testSceneDoc()
	ext.Layers[1].Objects[0].X = 222
	extBytes, err := sceneformat.Encode(ext)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scenePath, extBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	waitForDiskCondition(t, app, 2*time.Second, "disk conflict banner", func() bool {
		return app.Exists("id:disk-conflict-bar")
	})
	// Nothing reloaded under the author.
	if obj, _, _ := ed.FindObject("hero"); obj == nil || obj.X != 55 {
		t.Fatalf("dirty scene reloaded under author: hero=%+v", obj)
	}

	// Keep mine keeps editor edits, records disk version in History, and lets Save succeed.
	keepID := app.Find("id:disk-conflict-keep").ID
	// The live problem check the edit and the disk change started finishes now; the banner must stay mounted so the press lands.
	ed.WaitForProblems()
	app.Settle()
	if got := app.Find("id:disk-conflict-keep").ID; got != keepID {
		t.Fatalf("a problem check rebuilt the disk conflict banner: Keep mine view %v became %v", keepID, got)
	}
	app.Press("id:disk-conflict-keep")
	app.Settle()
	if app.Exists("id:disk-conflict-bar") || !ed.SceneDirty() {
		t.Fatalf("after Keep mine: bar=%v dirty=%v", app.Exists("id:disk-conflict-bar"), ed.SceneDirty())
	}
	if got := ed.History.UndoLabel(); got != "Changed outside the editor" {
		t.Fatalf("UndoLabel after Keep mine = %q, want %q", got, "Changed outside the editor")
	}
	// Undoing Keep mine visits the disk version; redoing returns to editor's version.
	app.Post(func() { ed.Undo() })
	app.Settle()
	if obj, _, _ := ed.FindObject("hero"); obj == nil || obj.X != 222 || ed.SceneDirty() {
		t.Fatalf("undoing Keep mine: hero=%+v dirty=%v, want X=222 clean", obj, ed.SceneDirty())
	}
	app.Post(func() { ed.Redo() })
	app.Settle()
	if obj, _, _ := ed.FindObject("hero"); obj == nil || obj.X != 55 || !ed.SceneDirty() {
		t.Fatalf("redoing Keep mine: hero=%+v dirty=%v, want X=55 dirty", obj, ed.SceneDirty())
	}
	app.Post(func() {
		if err := ed.Save(); err != nil {
			t.Errorf("Save after Keep mine failed: %v", err)
		}
	})
	app.Settle()
	if ed.SceneDirty() {
		t.Fatal("expected clean scene after Save")
	}

	// Now test Reload from disk: edits stay in History as "Unsaved edits before reload".
	app.Post(func() {
		if err := ed.MoveObject("hero", 91, 92); err != nil {
			t.Error(err)
		}
	})
	app.Settle()

	ext2 := testSceneDoc()
	ext2.Layers[1].Objects[0].X = 333
	extBytes2, err := sceneformat.Encode(ext2)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scenePath, extBytes2, 0o644); err != nil {
		t.Fatal(err)
	}

	waitForDiskCondition(t, app, 2*time.Second, "second disk conflict banner", func() bool {
		return app.Exists("id:disk-conflict-bar")
	})
	app.Press("id:disk-conflict-reload")
	app.Settle()
	if app.Exists("id:disk-conflict-bar") || ed.SceneDirty() {
		t.Fatalf("after Reload: bar=%v dirty=%v", app.Exists("id:disk-conflict-bar"), ed.SceneDirty())
	}
	if obj, _, _ := ed.FindObject("hero"); obj == nil || obj.X != 333 {
		t.Fatalf("after Reload: hero=%+v, want X=333", obj)
	}
	if got := ed.History.UndoLabel(); got != "Unsaved edits before reload" {
		t.Fatalf("UndoLabel after Reload = %q, want %q", got, "Unsaved edits before reload")
	}
	app.Post(func() { ed.Undo() })
	app.Settle()
	if obj, _, _ := ed.FindObject("hero"); obj == nil || obj.X != 91 || !ed.SceneDirty() {
		t.Fatalf("undoing Reload: hero=%+v dirty=%v, want X=91 dirty", obj, ed.SceneDirty())
	}
}

// TestDiskConflictComparePerObjectMerge changes two objects in the editor and the same two plus a third on disk,
// picks per object through Compare (Mine / Disk), and verifies the merged scene.
func TestDiskConflictComparePerObjectMerge(t *testing.T) {
	ed, dir := openTestProject(t)
	scenePath := filepath.Join(dir, "scenes", "compare.scene.yaml")

	// Seed a base scene with hero, coin, and door.
	base := testSceneDoc()
	base.Layers[1].Objects = []sceneformat.ObjectDoc{
		{ID: "hero", Name: "Hero", X: 10, Y: 10, Z: 1},
		{ID: "coin", Name: "Coin", X: 20, Y: 20, Z: 1},
		{ID: "door", Name: "Door", X: 30, Y: 30, Z: 1},
	}
	baseBytes, err := sceneformat.Encode(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scenePath, baseBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenScene("scenes/compare.scene.yaml"); err != nil {
		t.Fatal(err)
	}

	app := startEditorApp(t, ed)

	// Editor edits two objects: hero and coin.
	app.Post(func() {
		if err := ed.MoveObject("hero", 100, 10); err != nil {
			t.Error(err)
		}
		if err := ed.MoveObject("coin", 200, 20); err != nil {
			t.Error(err)
		}
	})
	app.Settle()

	// Disk edits the same two objects (hero and coin) AND a third object (door), plus adds lever_door.
	disk := base.Clone()
	disk.Layers[1].Objects[0].X = 150 // hero
	disk.Layers[1].Objects[1].X = 250 // coin
	disk.Layers[1].Objects[2].X = 350 // door (only changed on disk)
	disk.Layers[1].Objects = append(disk.Layers[1].Objects, sceneformat.ObjectDoc{
		ID: "lever_door", Name: "Lever Door", X: 400, Y: 40,
	})
	diskBytes, err := sceneformat.Encode(disk)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scenePath, diskBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	app.Post(func() { ed.PollDiskChanges() })
	app.Settle()

	if !app.Exists("id:disk-conflict-bar") {
		t.Fatal("expected disk-conflict-bar after PollDiskChanges")
	}
	app.Press("id:disk-conflict-compare")
	app.Settle()
	if !app.Exists("id:disk-conflict-compare-panel") {
		t.Fatal("expected disk-conflict-compare-panel after pressing Compare")
	}

	// Pick Mine for hero, Disk for coin; door and lever_door are automatically Disk.
	app.Press("id:disk-conflict-mine-hero")
	app.Settle()
	app.Press("id:disk-conflict-disk-coin")
	app.Settle()
	app.Press("id:disk-conflict-apply-merge")
	app.Settle()

	if app.Exists("id:disk-conflict-bar") || app.Exists("id:disk-conflict-compare-panel") {
		t.Fatal("expected conflict bar and compare panel to close after applying merge")
	}
	hero, _, _ := ed.FindObject("hero")
	coin, _, _ := ed.FindObject("coin")
	door, _, _ := ed.FindObject("door")
	lever, _, _ := ed.FindObject("lever_door")
	if hero == nil || hero.X != 100 {
		t.Fatalf("merged hero = %+v, want X=100 (Mine)", hero)
	}
	if coin == nil || coin.X != 250 {
		t.Fatalf("merged coin = %+v, want X=250 (Disk)", coin)
	}
	if door == nil || door.X != 350 {
		t.Fatalf("merged door = %+v, want X=350 (Disk single-side)", door)
	}
	if lever == nil || lever.X != 400 {
		t.Fatalf("merged lever_door = %+v, want X=400 (Disk added)", lever)
	}
	if !ed.SceneDirty() {
		t.Fatal("merged scene should remain dirty until saved")
	}
	app.Post(func() {
		if err := ed.Save(); err != nil {
			t.Errorf("Save merged scene: %v", err)
		}
	})
	app.Settle()
	if ed.SceneDirty() {
		t.Fatal("expected clean scene after saving merged result")
	}
}

// TestDiskWatchClipProjectAndFileBrowser verifies watching for the open animation clip, game.yaml,
// automatic file browser refresh on project tree changes, and grouped git branch switch reloads.
func TestDiskWatchClipProjectAndFileBrowser(t *testing.T) {
	ed, dir := openTestProject(t)
	ed.DiskWatchInterval = 20 * time.Millisecond

	// Create and open a clip file.
	clipPath := filepath.Join(dir, "hero_idle.animation.yaml")
	clip := &resource.Animation{
		Header: resource.NewHeader(resource.KindAnimation),
		Name:   "idle",
		Length: 1.0,
		Loop:   true,
	}
	clipBytes, err := resource.Encode(clip)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(clipPath, clipBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenClipFile(clipPath, "hero"); err != nil {
		t.Fatal(err)
	}
	if ed.ClipDirty() || ed.HasUnsavedChanges() {
		t.Fatalf("freshly opened clip dirty=%v unsaved=%v", ed.ClipDirty(), ed.HasUnsavedChanges())
	}

	// Undo back to saved point clears ClipDirty and title mark.
	if err := ed.SetClipName("walk"); err != nil {
		t.Fatal(err)
	}
	if !ed.ClipDirty() || !ed.HasUnsavedChanges() || !strings.Contains(ed.WindowTitle(), "*") {
		t.Fatalf("edited clip: dirty=%v unsaved=%v title=%q", ed.ClipDirty(), ed.HasUnsavedChanges(), ed.WindowTitle())
	}
	ed.Undo()
	if ed.ClipDirty() || strings.Contains(ed.WindowTitle(), "*") {
		t.Fatalf("undoing clip edit back to saved point: dirty=%v title=%q", ed.ClipDirty(), ed.WindowTitle())
	}

	// Clean clip reloads automatically when changed on disk.
	clip2 := &resource.Animation{
		Header: resource.NewHeader(resource.KindAnimation),
		Name:   "disk_run",
		Length: 2.0,
		Loop:   false,
	}
	clipBytes2, err := resource.Encode(clip2)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(clipPath, clipBytes2, 0o644); err != nil {
		t.Fatal(err)
	}
	res := ed.PollDiskChanges()
	if ed.Clip() == nil || ed.Clip().Name != "disk_run" || ed.ClipDirty() || len(res.Reloaded) == 0 {
		t.Fatalf("clean clip reload: clip=%+v dirty=%v reloaded=%v", ed.Clip(), ed.ClipDirty(), res.Reloaded)
	}

	// Dirty clip refuses SaveClipFile overwrite until KeepLocalVersion.
	if err := ed.SetClipName("my_jump"); err != nil {
		t.Fatal(err)
	}
	clip3 := &resource.Animation{
		Header: resource.NewHeader(resource.KindAnimation),
		Name:   "ext_fall",
		Length: 1.5,
	}
	clipBytes3, err := resource.Encode(clip3)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(clipPath, clipBytes3, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.SaveClipFile(clipPath); !errors.Is(err, editor.ErrDiskConflict) {
		t.Fatalf("SaveClipFile over external change = %v, want ErrDiskConflict", err)
	}
	if err := ed.KeepLocalVersion(editor.DiskFileClip); err != nil {
		t.Fatal(err)
	}
	if err := ed.SaveClipFile(clipPath); err != nil || ed.ClipDirty() {
		t.Fatalf("SaveClipFile after KeepLocalVersion = %v, dirty=%v", err, ed.ClipDirty())
	}

	// Clean game.yaml reloads automatically when changed on disk.
	gamePath := filepath.Join(dir, project.FileName)
	projCopy := ed.Project.Clone()
	projCopy.Name = "Updated Project"
	projBytes, err := project.Encode(projCopy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gamePath, projBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	ed.PollDiskChanges()
	if ed.Project.Name != "Updated Project" || ed.ProjectDirty() {
		t.Fatalf("clean game.yaml reload: name=%q dirty=%v", ed.Project.Name, ed.ProjectDirty())
	}

	// Dirty game.yaml refuses SaveProject overwrite until resolved.
	if err := ed.EditProject("rename", func(f *project.File) { f.Name = "Local Name" }); err != nil {
		t.Fatal(err)
	}
	projCopy2 := projCopy.Clone()
	projCopy2.Name = "External Name"
	projBytes2, err := project.Encode(projCopy2)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gamePath, projBytes2, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.SaveProject(); !errors.Is(err, editor.ErrDiskConflict) {
		t.Fatalf("SaveProject over external change = %v, want ErrDiskConflict", err)
	}
	if err := ed.ReloadFromDisk(editor.DiskFileProject); err != nil {
		t.Fatal(err)
	}
	if ed.Project.Name != "External Name" || ed.ProjectDirty() {
		t.Fatalf("after ReloadFromDisk(project): name=%q dirty=%v", ed.Project.Name, ed.ProjectDirty())
	}

	// Close the animation panel so the file browser has full vertical space to show its rows.
	if err := ed.CloseClip(); err != nil {
		t.Fatal(err)
	}

	// File browser refreshes automatically when a new file appears in the project tree.
	app := startEditorApp(t, ed)
	openFileBrowserInUI(app, ed)
	newScene := testSceneDoc()
	newScene.Name = "Brand New"
	newSceneBytes, err := sceneformat.Encode(newScene)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes", "brand_new.scene.yaml"), newSceneBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	waitForDiskCondition(t, app, 2*time.Second, "file browser automatic refresh", func() bool {
		return app.Exists("id:files-row-scenes/brand_new.scene.yaml")
	})

	// Git branch switch batches multiple clean reloads into one History step.
	gitDir := filepath.Join(dir, ".git", "refs", "heads")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "main"), []byte("1111111111111111111111111111111111111111\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Seed git head state.
	app.Post(func() { ed.PollDiskChanges() })
	app.Settle()

	// Simulate switching to branch boss-room changing both game.yaml and scenes/room.yaml.
	if err := os.WriteFile(filepath.Join(gitDir, "boss-room"), []byte("2222222222222222222222222222222222222222\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/boss-room\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	projBoss := ed.Project.Clone()
	projBoss.Name = "Boss Room Project"
	projBossBytes, _ := project.Encode(projBoss)
	if err := os.WriteFile(gamePath, projBossBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	sceneBoss := ed.Doc.Clone()
	sceneBoss.Name = "Boss Room"
	sceneBossBytes, _ := sceneformat.Encode(sceneBoss)
	if err := os.WriteFile(filepath.Join(dir, "scenes", "room.yaml"), sceneBossBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	app.Post(func() { ed.PollDiskChanges() })
	app.Settle()

	if got := ed.History.UndoLabel(); got != "Switched branch · main → boss-room · 2 files" {
		t.Fatalf("grouped git branch switch UndoLabel = %q, want %q", got, "Switched branch · main → boss-room · 2 files")
	}
}

// TestClosingWindowWithUnsavedClipAsksAndCancelKeeps verifies acceptance criterion 2 with unsaved clip edits too.
func TestClosingWindowWithUnsavedClipAsksAndCancelKeeps(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("closes the headless model's window")
	}
	ed, dir := openTestProject(t)
	clipPath := filepath.Join(dir, "walk.animation.yaml")
	clip := &resource.Animation{
		Header: resource.NewHeader(resource.KindAnimation),
		Name:   "walk",
		Length: 1.0,
	}
	data, err := resource.Encode(clip)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(clipPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenClipFile(clipPath, "hero"); err != nil {
		t.Fatal(err)
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680), nsgotest.Environment(core.Environment{OS: core.OSMacOS}))

	app.Post(func() {
		if err := ed.SetClipName("sprint"); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	if !strings.Contains(app.Title, "*") {
		t.Fatalf("window title %q missing unsaved mark for dirty clip", app.Title)
	}
	if app.CloseWindow() {
		t.Fatal("window closed with unsaved clip edits without asking")
	}
	app.Settle()
	if !app.Exists("id:unsaved-dialog") {
		t.Fatal("closing window with unsaved clip edits did not ask")
	}
	app.Press("id:unsaved-cancel")
	app.Settle()
	if !ed.ClipDirty() || ed.Clip().Name != "sprint" || len(app.Query("id:scene-editor")) == 0 {
		t.Fatalf("after Cancel: dirty=%v clip=%+v windowOpen=%v", ed.ClipDirty(), ed.Clip(), len(app.Query("id:scene-editor")) > 0)
	}
}

// TestDiskWatchMultiTabSaveConflictAndSaveAllAtomicity verifies per-tab disk snapshots across tab switches,
// SaveTab/SaveAll upfront conflict checks on inactive tabs, ConfirmOverwriteSave for inactive tabs,
// and auto-clearing stale conflicts when an externally modified file reverts to its original bytes.
func TestDiskWatchMultiTabSaveConflictAndSaveAllAtomicity(t *testing.T) {
	ed, dir := openTestProject(t)
	roomPath := filepath.Join(dir, "scenes", "room.yaml")
	secondPath := filepath.Join(dir, "scenes", "second.scene.yaml")

	secondDoc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "Second Room",
		Layers: []sceneformat.LayerDoc{{
			Name:  "world",
			Space: sceneformat.SpaceWorld,
			Objects: []sceneformat.ObjectDoc{{
				ID:   "npc",
				Name: "NPC",
				X:    10,
				Y:    20,
			}},
		}},
	}
	secondBytes, err := sceneformat.Encode(secondDoc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, secondBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	// Edit tab 0 (room.yaml), then open tab 1 (second.scene.yaml) and edit tab 1 + game.yaml.
	if err := ed.MoveObject("hero", 88, 40); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenFile(secondPath); err != nil {
		t.Fatal(err)
	}
	if ed.TabCount() != 2 || ed.ActiveTab() != 1 {
		t.Fatalf("tabs=%d active=%d, want 2/1", ed.TabCount(), ed.ActiveTab())
	}
	if err := ed.MoveObject("npc", 55, 66); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetProjectName("Unsaved Project Name"); err != nil {
		t.Fatal(err)
	}

	// Externally modify inactive tab 0 (room.yaml) on disk.
	origRoomBytes, err := os.ReadFile(roomPath)
	if err != nil {
		t.Fatal(err)
	}
	extRoomDoc, err := sceneformat.Parse(origRoomBytes)
	if err != nil {
		t.Fatal(err)
	}
	extRoomDoc.Name = "External Room Edit"
	extRoomBytes, err := sceneformat.Encode(extRoomDoc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(roomPath, extRoomBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	origGameYAML, err := os.ReadFile(filepath.Join(dir, "game.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	// SaveTab on inactive tab 0 must fail with ErrDiskConflict without overwriting disk.
	if err := ed.SaveTab(0); !errors.Is(err, editor.ErrDiskConflict) {
		t.Fatalf("SaveTab(0) err = %v, want ErrDiskConflict", err)
	}
	// SaveAll must also fail upfront with ErrDiskConflict without partially saving game.yaml or tab 1.
	if err := ed.SaveAll(); !errors.Is(err, editor.ErrDiskConflict) {
		t.Fatalf("SaveAll() err = %v, want ErrDiskConflict", err)
	}
	if curGameYAML, _ := os.ReadFile(filepath.Join(dir, "game.yaml")); string(curGameYAML) != string(origGameYAML) {
		t.Fatal("SaveAll partially wrote game.yaml before failing on conflicting tab 0")
	}
	if curSecond, _ := os.ReadFile(secondPath); string(curSecond) != string(secondBytes) {
		t.Fatal("SaveAll partially wrote tab 1 before failing on conflicting tab 0")
	}

	// ConfirmOverwriteSave overwrites the conflicting inactive tab 0 cleanly.
	if err := ed.ConfirmOverwriteSave(); err != nil {
		t.Fatalf("ConfirmOverwriteSave(inactive tab 0): %v", err)
	}
	if ed.TabDirty(0) {
		t.Fatal("tab 0 still dirty after ConfirmOverwriteSave")
	}
	savedRoom, err := os.ReadFile(roomPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(savedRoom), "x: 88") {
		t.Fatalf("tab 0 overwrite missing local edit x: 88:\n%s", savedRoom)
	}

	// Switch back to tab 0, make a local edit, externally modify room.yaml, then revert room.yaml on disk.
	if err := ed.SelectTab(0); err != nil {
		t.Fatal(err)
	}
	if err := ed.MoveObject("hero", 120, 40); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(roomPath, extRoomBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	ed.PollDiskChanges()
	if _, ok := ed.PrimaryDiskConflict(); !ok {
		t.Fatal("expected active scene conflict after switching back to tab 0 and editing disk")
	}
	// Reverting room.yaml back to its recorded snapshot auto-clears the stale conflict.
	if err := os.WriteFile(roomPath, savedRoom, 0o644); err != nil {
		t.Fatal(err)
	}
	ed.PollDiskChanges()
	if _, ok := ed.PrimaryDiskConflict(); ok {
		t.Fatal("stale conflict remained after external file reverted to snapshot hash")
	}
	if err := ed.Save(); err != nil {
		t.Fatalf("Save after revert failed: %v", err)
	}
}

// TestDiskWatchMergePreservesNestedChildrenAndV4Environment verifies 3-way merge preserves nested child hierarchy
// and propagates non-conflicting v4 Environment metadata without mutating conflict snapshots.
func TestDiskWatchMergePreservesNestedChildrenAndV4Environment(t *testing.T) {
	ed, dir := openTestProject(t)
	roomPath := filepath.Join(dir, "scenes", "room.yaml")

	baseDoc := ed.Doc.Clone()
	baseDoc.Version = sceneformat.CurrentVersion
	heroLayerIdx, heroObjIdx := -1, -1
	for li := range baseDoc.Layers {
		for oi := range baseDoc.Layers[li].Objects {
			if baseDoc.Layers[li].Objects[oi].ID == "hero" {
				heroLayerIdx, heroObjIdx = li, oi
			}
		}
	}
	if heroLayerIdx < 0 {
		t.Fatal("hero object not found in test scene")
	}
	baseDoc.Layers[heroLayerIdx].Objects[heroObjIdx].Children = []sceneformat.ObjectDoc{{
		ID:   "sword",
		Name: "Sword",
		X:    4,
		Y:    0,
	}}
	baseBytes, err := sceneformat.Encode(baseDoc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(roomPath, baseBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.ReloadFromDisk(editor.DiskFileScene); err != nil {
		t.Fatal(err)
	}

	// Local edit: move hero.
	if err := ed.MoveObject("hero", 99, 40); err != nil {
		t.Fatal(err)
	}

	// External disk edit: add nested child "shield" under hero, set v4 Environment, and add a v4 3D camera object.
	diskDoc := baseDoc.Clone()
	diskDoc.Environment = &sceneformat.EnvironmentDoc{
		Sky: &sceneformat.SkyDoc{
			Kind:  sceneformat.SkyColor,
			Color: "#336699",
		},
		Tonemapping: sceneformat.TonemapACES,
	}
	diskDoc.Layers[heroLayerIdx].Objects[heroObjIdx].Children = append(diskDoc.Layers[heroLayerIdx].Objects[heroObjIdx].Children, sceneformat.ObjectDoc{
		ID:   "shield",
		Name: "Shield",
		X:    -4,
		Y:    2,
	})
	diskDoc.Layers[heroLayerIdx].Objects = append(diskDoc.Layers[heroLayerIdx].Objects, sceneformat.ObjectDoc{
		ID:   "cam3d",
		Name: "Main Camera",
		Camera: &sceneformat.Camera3DDoc{
			Current: true,
		},
	})
	diskBytes, err := sceneformat.Encode(diskDoc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(roomPath, diskBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	ed.PollDiskChanges()
	if err := ed.ApplySceneConflictMerge(); err != nil {
		t.Fatalf("ApplySceneConflictMerge: %v", err)
	}

	hero, _, _ := ed.FindObject("hero")
	if hero == nil || hero.X != 99 {
		t.Fatalf("merged hero = %+v, want X=99", hero)
	}
	foundChild := false
	for _, ch := range hero.Children {
		if ch.ID == "shield" {
			foundChild = true
		}
	}
	if !foundChild {
		t.Fatalf("disk-added child 'shield' was not preserved under hero.Children: %+v", hero.Children)
	}
	for _, top := range ed.Doc.Layers[heroLayerIdx].Objects {
		if top.ID == "shield" {
			t.Fatal("disk-added child 'shield' was flattened to top-level layer objects")
		}
	}
	if ed.Doc.Environment == nil || ed.Doc.Environment.Sky == nil || ed.Doc.Environment.Sky.Color != "#336699" || ed.Doc.Environment.Tonemapping != sceneformat.TonemapACES {
		t.Fatalf("merged Environment = %+v, want SkyColor #336699 and TonemapACES", ed.Doc.Environment)
	}
	camObj, _, _ := ed.FindObject("cam3d")
	if camObj == nil || camObj.Camera == nil || !camObj.Camera.Current {
		t.Fatalf("merged cam3d = %+v, want v4 Camera3DDoc with Current=true", camObj)
	}
	if ed.Doc.Version != sceneformat.CurrentVersion {
		t.Fatalf("merged Version = %d, want %d", ed.Doc.Version, sceneformat.CurrentVersion)
	}
}

// TestDiskWatchSymlinkSaveConflictAndCloseStopsWatcher verifies symlinked paths still trigger ErrDiskConflict
// and Editor.Close() stops the background watcher cleanly.
func TestDiskWatchSymlinkSaveConflictAndCloseStopsWatcher(t *testing.T) {
	ed, dir := openTestProject(t)
	ed.DiskWatchInterval = 20 * time.Millisecond
	realScene := filepath.Join(dir, "scenes", "room.yaml")
	linkDir := filepath.Join(t.TempDir(), "linked_scenes")
	if err := os.Symlink(filepath.Join(dir, "scenes"), linkDir); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	linkScene := filepath.Join(linkDir, "room.yaml")

	ed2, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed2.DiskWatchInterval = 20 * time.Millisecond
	if err := ed2.OpenFile(linkScene); err != nil {
		t.Fatalf("OpenFile(symlink): %v", err)
	}
	if err := ed2.MoveObject("hero", 77, 40); err != nil {
		t.Fatal(err)
	}
	extDoc := ed2.Doc.Clone()
	extDoc.Name = "External Via Real Path"
	extBytes, err := sceneformat.Encode(extDoc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(realScene, extBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	// Saving via the real path while opened via the symlink path must still detect ErrDiskConflict.
	if err := ed2.SaveFile(realScene); !errors.Is(err, editor.ErrDiskConflict) {
		t.Fatalf("SaveFile(realScene) after OpenFile(linkScene) = %v, want ErrDiskConflict", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))
	app.Settle()
	if err := ed.Close(); err != nil {
		t.Fatalf("Editor.Close: %v", err)
	}
	_ = ed2.Close()
}
