package editor_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// openTestProject opens the project writeEditorProject writes, with its main scene scenes/room.yaml, and a second scene scenes/other.scene.yaml, and returns the editor and the project's directory.
func openTestProject(t *testing.T) (*editor.Editor, string) {
	t.Helper()
	dir := writeEditorProject(t)
	other := testSceneDoc()
	other.Name = "Other"
	data, err := sceneformat.Encode(other)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes", "other.scene.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	// Opening the project starts the Problems check in the background, which must finish before the directory goes.
	t.Cleanup(ed.WaitForProblems)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	return ed, dir
}

// checkProject runs what `game check` runs on dir and fails on any problem.
func checkProject(t *testing.T, dir string) {
	t.Helper()
	report, err := assetcheck.Check(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range report.Problems {
		t.Errorf("game check: %v", p)
	}
}

func TestSceneDirtyFollowsEditsUndoAndSave(t *testing.T) {
	ed, dir := openTestProject(t)
	if ed.SceneDirty() || ed.HasUnsavedChanges() {
		t.Fatal("a scene just opened is dirty")
	}
	if ed.ScenePath != "scenes/room.yaml" || ed.SceneLabel() != "scenes/room.yaml" {
		t.Fatalf("ScenePath %q, SceneLabel %q, want the main scene's path in the project", ed.ScenePath, ed.SceneLabel())
	}
	if err := ed.MoveObject("hero", 99, 98); err != nil {
		t.Fatal(err)
	}
	if !ed.SceneDirty() || !ed.HasUnsavedChanges() || !strings.HasPrefix(ed.WindowTitle(), "scenes/room.yaml*") {
		t.Fatalf("after an edit: dirty %v, title %q", ed.SceneDirty(), ed.WindowTitle())
	}
	ed.Undo()
	if ed.SceneDirty() || strings.Contains(ed.WindowTitle(), "*") {
		t.Fatalf("undoing back to the saved state leaves the scene dirty, title %q", ed.WindowTitle())
	}
	ed.Redo()
	if !ed.SceneDirty() {
		t.Fatal("redoing the edit leaves the scene clean")
	}
	if err := ed.Save(); err != nil {
		t.Fatal(err)
	}
	if ed.SceneDirty() {
		t.Fatal("a saved scene is dirty")
	}
	data, err := os.ReadFile(filepath.Join(dir, "scenes", "room.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := ed.SaveYAML(); string(data) != string(want) {
		t.Fatalf("the file holds\n%s\nwant\n%s", data, want)
	}
	// Undoing past the save makes the scene differ from the file again.
	ed.Undo()
	if !ed.SceneDirty() {
		t.Fatal("undoing past the save leaves the scene clean")
	}
}

func TestSaveThroughOnSaveMarksTheSceneSaved(t *testing.T) {
	ed := newTestEditor(t)
	if ed.SceneDirty() {
		t.Fatal("the scene the editor starts on is dirty")
	}
	if err := ed.MoveObject("hero", 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := ed.Save(); !errors.Is(err, editor.ErrNoSceneFile) || !ed.SceneDirty() {
		t.Fatalf("Save without a file = %v (dirty %v), want ErrNoSceneFile", err, ed.SceneDirty())
	}
	var saved []byte
	ed.OnSave = func(data []byte) error {
		saved = data
		return nil
	}
	if err := ed.Save(); err != nil || ed.SceneDirty() || len(saved) == 0 {
		t.Fatalf("Save through OnSave = %v, dirty %v, %d bytes", err, ed.SceneDirty(), len(saved))
	}
	ed.OnSave = func([]byte) error { return errors.New("disk full") }
	if err := ed.MoveObject("hero", 3, 4); err != nil {
		t.Fatal(err)
	}
	if err := ed.Save(); err == nil || !ed.SceneDirty() {
		t.Fatalf("a failed OnSave = %v, dirty %v, want its error and the scene still dirty", err, ed.SceneDirty())
	}
}

func TestOpenNewAndSaveSceneAs(t *testing.T) {
	ed, dir := openTestProject(t)
	ed.OnSave = func([]byte) error { return errors.New("OnSave persisted the scene opened before") }

	if err := ed.OpenScene("scenes/other.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if ed.Doc.Name != "Other" || ed.ScenePath != "scenes/other.scene.yaml" || ed.FilePath != filepath.Join(dir, "scenes", "other.scene.yaml") || ed.OnSave != nil || ed.SceneDirty() {
		t.Fatalf("after OpenScene: %q at %q (%q), OnSave kept %v, dirty %v", ed.Doc.Name, ed.ScenePath, ed.FilePath, ed.OnSave != nil, ed.SceneDirty())
	}
	for _, bad := range []string{"../outside.scene.yaml", "/abs.scene.yaml", "", `scenes\win.scene.yaml`} {
		if err := ed.OpenScene(bad); err == nil {
			t.Errorf("OpenScene(%q) succeeded", bad)
		}
		if _, err := ed.NewScene(bad); err == nil {
			t.Errorf("NewScene(%q) succeeded", bad)
		}
	}

	// The project's own file names are refused even where no such file exists yet, since the project's tools would read a scene there as something else.
	for _, bad := range []string{project.NSGoFileName, "sub/" + project.NSGoFileName, "sub/" + project.FileName} {
		if _, err := ed.NewScene(bad); err == nil || errors.Is(err, editor.ErrFileExists) {
			t.Errorf("NewScene(%q) = %v, want the project file's name refused", bad, err)
		}
		if _, err := ed.SaveSceneAs(bad); err == nil || errors.Is(err, editor.ErrFileExists) {
			t.Errorf("SaveSceneAs(%q) = %v, want the project file's name refused", bad, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "sub")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused path made its directory: %v", err)
	}

	// A path ending in .scene gets .yaml, and an extension in upper case is trimmed from the scene's name.
	for _, c := range []struct{ in, rel, name string }{
		{"levels/Hall.scene", "levels/Hall.scene.yaml", "Hall"},
		{"levels/Big.YAML", "levels/Big.YAML", "Big"},
		{"levels/Loud.Scene.YML", "levels/Loud.Scene.YML", "Loud"},
	} {
		rel, err := ed.NewScene(c.in)
		if err != nil {
			t.Fatal(err)
		}
		if rel != c.rel || ed.Doc.Name != c.name {
			t.Errorf("NewScene(%q) = %q named %q, want %q named %q", c.in, rel, ed.Doc.Name, c.rel, c.name)
		}
	}

	rel, err := ed.NewScene("levels/cave")
	if err != nil {
		t.Fatal(err)
	}
	if rel != "levels/cave.scene.yaml" || ed.ScenePath != rel || ed.Doc.Name != "cave" || ed.SceneDirty() || ed.History.CanUndo() {
		t.Fatalf("after NewScene: %q, ScenePath %q, name %q, dirty %v", rel, ed.ScenePath, ed.Doc.Name, ed.SceneDirty())
	}
	// The project's window is 640x360 and it has no display, so the scene takes the window's size.
	if ed.Doc.VirtualWidth != 640 || ed.Doc.VirtualHeight != 360 {
		t.Fatalf("the new scene is %vx%v, want the project's 640x360", ed.Doc.VirtualWidth, ed.Doc.VirtualHeight)
	}
	if _, err := ed.NewScene("levels/cave.scene.yaml"); !errors.Is(err, editor.ErrFileExists) {
		t.Fatalf("NewScene over a file = %v, want ErrFileExists", err)
	}

	if _, err := ed.PlaceObject("main", sceneformat.ObjectDoc{ID: "torch", X: 4, Y: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.SaveSceneAs("scenes/other.scene.yaml"); !errors.Is(err, editor.ErrFileExists) {
		t.Fatalf("SaveSceneAs over another file = %v, want ErrFileExists", err)
	}
	rel, err = ed.SaveSceneAs("levels/deep/cave2")
	if err != nil {
		t.Fatal(err)
	}
	if rel != "levels/deep/cave2.scene.yaml" || ed.ScenePath != rel || ed.FilePath != filepath.Join(dir, "levels", "deep", "cave2.scene.yaml") || ed.SceneDirty() {
		t.Fatalf("after SaveSceneAs: %q, ScenePath %q, FilePath %q, dirty %v", rel, ed.ScenePath, ed.FilePath, ed.SceneDirty())
	}
	// Saving as the scene's own path saves it.
	if err := ed.MoveObject("torch", 6, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.SaveSceneAs(rel); err != nil || ed.SceneDirty() {
		t.Fatalf("SaveSceneAs to its own path = %v, dirty %v", err, ed.SceneDirty())
	}
	if _, err := ed.SaveSceneAs("levels/"); err == nil {
		t.Fatal("SaveSceneAs to a directory succeeded")
	}
}

func TestSceneFileOperationsNeedAProject(t *testing.T) {
	ed := newTestEditor(t)
	if err := ed.OpenScene("scenes/a.scene.yaml"); !errors.Is(err, editor.ErrNoProject) {
		t.Errorf("OpenScene = %v, want ErrNoProject", err)
	}
	if _, err := ed.NewScene("scenes/a.scene.yaml"); !errors.Is(err, editor.ErrNoProject) {
		t.Errorf("NewScene = %v, want ErrNoProject", err)
	}
	if _, err := ed.SaveSceneAs("scenes/a.scene.yaml"); !errors.Is(err, editor.ErrNoProject) {
		t.Errorf("SaveSceneAs = %v, want ErrNoProject", err)
	}
	if err := ed.RevertScene(); !errors.Is(err, editor.ErrNoSceneFile) {
		t.Errorf("RevertScene = %v, want ErrNoSceneFile", err)
	}
	if ed.SceneLabel() != "Untitled" {
		t.Errorf("SceneLabel = %q, want Untitled", ed.SceneLabel())
	}
}

func TestRevertSceneIsUndoable(t *testing.T) {
	ed, dir := openTestProject(t)
	if err := ed.MoveObject("hero", 50, 60); err != nil {
		t.Fatal(err)
	}
	if err := ed.RevertScene(); err != nil {
		t.Fatal(err)
	}
	if obj, _, _ := ed.FindObject("hero"); obj == nil || obj.X == 50 || ed.SceneDirty() {
		t.Fatalf("after RevertScene: hero %+v, dirty %v", obj, ed.SceneDirty())
	}
	if !ed.Undo() {
		t.Fatal("RevertScene left nothing to undo")
	}
	if obj, _, _ := ed.FindObject("hero"); obj == nil || obj.X != 50 || obj.Y != 60 || !ed.SceneDirty() {
		t.Fatalf("after undoing the revert: hero %+v, dirty %v", obj, ed.SceneDirty())
	}

	// A file changed on disk since it was opened is what Revert puts back, and the scene is clean against it.
	changed := testSceneDoc()
	changed.Name = "Changed Outside"
	data, err := sceneformat.Encode(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes", "room.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.RevertScene(); err != nil || ed.Doc.Name != "Changed Outside" || ed.SceneDirty() {
		t.Fatalf("RevertScene after an outside change = %v: %q, dirty %v", err, ed.Doc.Name, ed.SceneDirty())
	}

	// A file that no longer parses changes nothing.
	if err := os.WriteFile(filepath.Join(dir, "scenes", "room.yaml"), []byte("version: 2\nbogus: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.MoveObject("hero", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := ed.RevertScene(); err == nil || !ed.SceneDirty() {
		t.Fatalf("RevertScene of a broken file = %v, dirty %v", err, ed.SceneDirty())
	}
}

func TestMovingTheOpenSceneKeepsItClean(t *testing.T) {
	ed, _ := openTestProject(t)
	if err := ed.MoveFile("scenes/room.yaml", "levels/room.yaml"); err != nil {
		t.Fatal(err)
	}
	if ed.ScenePath != "levels/room.yaml" || ed.SceneDirty() {
		t.Fatalf("after the move: ScenePath %q, dirty %v", ed.ScenePath, ed.SceneDirty())
	}
}

// TestMovingAnAssetOfAnUnreadSceneLeavesItDirty moves an asset that the open scene names while refs does not read the scene's file, which then keeps the old path on disk: the scene shows as dirty, so the rewritten reference is not lost, while a scene whose file the move rewrote stays clean.
func TestMovingAnAssetOfAnUnreadSceneLeavesItDirty(t *testing.T) {
	ed, dir := openTestProject(t)
	room := testSceneDoc()
	room.Layers[1].Objects[0].Material = "materials/stone.material.yaml"
	plain := testSceneDoc()
	plain.Name = "Plain"
	plain.Layers[1].Objects[0].Material = "materials/moss.material.yaml"
	files := map[string]*sceneformat.SceneDoc{"scenes/room.yaml": room, "levels/plain.yaml": plain}
	for name, doc := range files {
		data, err := sceneformat.Encode(doc)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "materials"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"stone", "moss"} {
		if err := os.WriteFile(filepath.Join(dir, "materials", name+".material.yaml"), []byte("type: material\nversion: 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The main scene, which game.yaml names, is one refs reads and rewrites.
	if err := ed.RevertScene(); err != nil {
		t.Fatal(err)
	}
	if err := ed.MoveFile("materials/stone.material.yaml", "materials/rock.material.yaml"); err != nil {
		t.Fatal(err)
	}
	if ed.SceneDirty() {
		t.Fatal("moving an asset of a scene whose file the move rewrote left it dirty")
	}

	// levels/plain.yaml has no .scene.yaml name and nothing names it, so the move does not touch it.
	if err := ed.OpenScene("levels/plain.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.MoveFile("materials/moss.material.yaml", "materials/lichen.material.yaml"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "levels", "plain.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "materials/moss.material.yaml") {
		t.Fatalf("the move rewrote levels/plain.yaml, which this test needs left alone:\n%s", data)
	}
	if hero, _, _ := ed.FindObject("hero"); hero == nil || hero.Material != "materials/lichen.material.yaml" || !ed.SceneDirty() {
		t.Fatalf("after moving the asset: hero %+v, dirty %v, want the new path and a dirty scene", hero, ed.SceneDirty())
	}
	if !ed.Undo() || ed.SceneDirty() {
		t.Fatalf("undoing the move leaves the scene dirty %v", ed.SceneDirty())
	}
}

// startEditorApp shows ed in an EditorView at the size `game editor` opens.
func startEditorApp(t *testing.T, ed *editor.Editor) *nsgotest.App {
	t.Helper()
	return nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))
}

// TestUnsavedMarkerAndModS edits a scene in the editor: the toolbar's marker and the window title show the unsaved edit, Undo back to the saved state clears them, and Mod+S saves.
func TestUnsavedMarkerAndModS(t *testing.T) {
	ed, dir := openTestProject(t)
	_ = ed.SetAutoSave(false)
	app := startEditorApp(t, ed)
	if app.Exists("id:toolbar-unsaved") || app.Title != "scenes/room.yaml - NSGo Scene Editor" {
		t.Fatalf("a clean scene shows the marker (%v), title %q", app.Exists("id:toolbar-unsaved"), app.Title)
	}
	if got := strings.TrimSpace(allText(app.Find("id:toolbar-file"))); got != "room" {
		t.Fatalf("the header breadcrumb shows %q, want room", got)
	}
	if got := strings.TrimSpace(allText(app.Find("id:rail-file-scene"))); got != "scenes/room.yaml" {
		t.Fatalf("the left rail FILES shows %q, want scenes/room.yaml", got)
	}
	app.Press("id:toolbar-file")
	app.Settle()
	if app.Enabled("id:toolbar-revert") {
		t.Fatal("Revert is enabled on a clean scene")
	}
	app.Key("escape")
	app.Settle()

	app.Post(func() {
		if err := ed.MoveObject("hero", 70, 80); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	if !app.Exists("id:toolbar-unsaved") || app.Title != "scenes/room.yaml* - NSGo Scene Editor" {
		t.Fatalf("an edit shows no marker (%v), title %q", app.Exists("id:toolbar-unsaved"), app.Title)
	}
	undoInUI(app, ed)
	if app.Exists("id:toolbar-unsaved") || strings.Contains(app.Title, "*") {
		t.Fatalf("undoing back to the saved state keeps the marker, title %q", app.Title)
	}
	redoInUI(app, ed)
	if !app.Exists("id:toolbar-unsaved") {
		t.Fatal("redoing the edit shows no marker")
	}

	app.Key("mod+s")
	app.Settle()
	if app.Exists("id:toolbar-unsaved") || ed.SceneDirty() {
		t.Fatalf("Mod+S left the marker, status %q", ed.Status)
	}
	data, err := os.ReadFile(filepath.Join(dir, "scenes", "room.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := sceneformat.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if hero := doc.Layers[1].Objects[0]; hero.X != 70 || hero.Y != 80 {
		t.Fatalf("Mod+S wrote hero at %v,%v", hero.X, hero.Y)
	}

	// Revert puts back the file, and Undo the edit it reverted.
	app.Post(func() {
		if err := ed.MoveObject("hero", 1, 1); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	app.Press("id:toolbar-file")
	app.Settle()
	app.Press("id:toolbar-revert")
	app.Settle()
	if obj, _, _ := ed.FindObject("hero"); obj.X != 70 || app.Exists("id:toolbar-unsaved") {
		t.Fatalf("after Revert hero is at %v, marker %v", obj.X, app.Exists("id:toolbar-unsaved"))
	}
	undoInUI(app, ed)
	if obj, _, _ := ed.FindObject("hero"); obj.X != 1 || !app.Exists("id:toolbar-unsaved") {
		t.Fatalf("after undoing Revert hero is at %v, marker %v", obj.X, app.Exists("id:toolbar-unsaved"))
	}
}

// TestOpenFromTheFileBrowserOpensTabs opens scenes from the file browser into tabs without prompting or losing edits, switches to an already-open tab, and asks only when closing a dirty tab.
func TestOpenFromTheFileBrowserOpensTabs(t *testing.T) {
	ed, dir := openTestProject(t)
	_ = ed.SetAutoSave(false)
	app := startEditorApp(t, ed)
	openFileBrowserInUI(app, ed)
	edit := func(x float32) {
		app.Post(func() {
			if err := ed.MoveObject("hero", x, x); err != nil {
				t.Error(err)
			}
		})
		app.Settle()
	}

	// Double-pressing another scene opens it in a second tab without prompting.
	app.DoublePress("id:files-row-scenes/other.scene.yaml")
	app.Settle()
	if app.Exists("id:unsaved-dialog") || ed.ScenePath != "scenes/other.scene.yaml" || ed.TabCount() != 2 {
		t.Fatalf("opening a second scene: prompt %v, ScenePath %q, tabs %d", app.Exists("id:unsaved-dialog"), ed.ScenePath, ed.TabCount())
	}

	edit(33)
	// Opening an already-open scene from the file browser switches to its tab without prompting and keeps the first tab's dirty edit and undo history.
	app.Press("id:files-row-scenes/room.yaml")
	app.Settle()
	app.Press("id:files-open")
	app.Settle()
	if app.Exists("id:unsaved-dialog") || ed.ScenePath != "scenes/room.yaml" || ed.TabCount() != 2 || !ed.TabDirty(1) {
		t.Fatalf("switching via file browser: prompt %v, ScenePath %q, tabs %d, tab1 dirty %v", app.Exists("id:unsaved-dialog"), ed.ScenePath, ed.TabCount(), ed.TabDirty(1))
	}

	// Closing the dirty tab asks: Cancel keeps it open and dirty, Save saves it and closes it.
	app.Press("id:scene-tab-close-1")
	app.Settle()
	if !app.Exists("id:unsaved-dialog") {
		t.Fatal("closing a dirty tab did not ask")
	}
	app.Press("id:unsaved-cancel")
	app.Settle()
	if app.Exists("id:unsaved-dialog") || ed.TabCount() != 2 || !ed.TabDirty(1) {
		t.Fatalf("after Cancel: prompt %v, tabs %d, tab1 dirty %v", app.Exists("id:unsaved-dialog"), ed.TabCount(), ed.TabDirty(1))
	}

	app.Press("id:scene-tab-close-1")
	app.Settle()
	app.Press("id:unsaved-save")
	app.Settle()
	if ed.TabCount() != 1 || ed.ScenePath != "scenes/room.yaml" || ed.HasUnsavedChanges() {
		t.Fatalf("after Save close: tabs %d, ScenePath %q, unsaved %v, status %q", ed.TabCount(), ed.ScenePath, ed.HasUnsavedChanges(), ed.Status)
	}
	data, err := os.ReadFile(filepath.Join(dir, "scenes", "other.scene.yaml"))
	if err != nil || !strings.Contains(string(data), "x: 33") {
		t.Fatalf("Save did not write the edit (%v):\n%s", err, data)
	}

	edit(44)
	// Double-pressing the open scene's row does not reload it or clear its undo history.
	app.DoublePress("id:files-row-scenes/room.yaml")
	app.Settle()
	if app.Exists("id:unsaved-dialog") || !ed.History.CanUndo() {
		t.Fatalf("double-pressing the open scene: prompt %v, undo %v", app.Exists("id:unsaved-dialog"), ed.History.CanUndo())
	}
	// Editing the shared project and discarding a dirty scene tab keeps the shared project edit intact.
	app.Post(func() {
		if err := ed.EditProject("rename", func(f *project.File) { f.Name = "Renamed" }); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	app.DoublePress("id:files-row-scenes/other.scene.yaml")
	app.Settle()
	app.Press("id:scene-tab-close-0")
	app.Settle()
	app.Press("id:unsaved-discard")
	app.Settle()
	if ed.ScenePath != "scenes/other.scene.yaml" || ed.TabCount() != 1 || ed.SceneDirty() || !ed.ProjectDirty() || ed.Project.Name != "Renamed" {
		t.Fatalf("after Discard tab close: ScenePath %q, tabs %d, sceneDirty %v, projectDirty %v, project %q", ed.ScenePath, ed.TabCount(), ed.SceneDirty(), ed.ProjectDirty(), ed.Project.Name)
	}
	data, err = os.ReadFile(filepath.Join(dir, "scenes", "room.yaml"))
	if err != nil || strings.Contains(string(data), "x: 44") {
		t.Fatalf("Discard wrote the edit (%v):\n%s", err, data)
	}
}

// TestNewSceneAndSaveAsFromTheToolbar makes a scene with New, which `game check` passes once saved, and saves it under another path with Save As.
func TestNewSceneAndSaveAsFromTheToolbar(t *testing.T) {
	dir := writeBareProject(t, "Bare")
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	_ = ed.SetAutoSave(false)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	app := startEditorApp(t, ed)

	app.Press("id:toolbar-file")
	app.Settle()
	app.Press("id:toolbar-new")
	app.Settle()
	if !app.Exists("id:scene-path-dialog") {
		t.Fatal("New shows no path dialog")
	}
	app.Clear("id:scene-path-input")
	app.Type("id:scene-path-input", "levels/first")
	app.Settle()
	app.Press("id:scene-path-confirm")
	app.Settle()
	if got := strings.TrimSpace(allText(app.Find("id:toolbar-file"))); ed.ScenePath != "levels/first.scene.yaml" || got != "first" {
		t.Fatalf("after New: ScenePath %q, toolbar-file %q, status %q", ed.ScenePath, got, ed.Status)
	}
	app.Post(func() {
		if _, err := ed.PlaceObject("main", sceneformat.ObjectDoc{ID: "door", X: 10, Y: 20}); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	saveInUI(app, ed)
	if ed.SceneDirty() {
		t.Fatalf("Save left the new scene dirty, status %q", ed.Status)
	}
	checkProject(t, dir)

	app.Press("id:toolbar-file")
	app.Settle()
	app.Press("id:toolbar-save-as")
	app.Settle()
	app.Clear("id:scene-path-input")
	app.Type("id:scene-path-input", "levels/second.scene.yaml")
	app.Settle()
	app.Press("id:scene-path-confirm")
	app.Settle()
	if ed.ScenePath != "levels/second.scene.yaml" || ed.SceneDirty() {
		t.Fatalf("after Save As: ScenePath %q, dirty %v, status %q", ed.ScenePath, ed.SceneDirty(), ed.Status)
	}
	for _, name := range []string{"first", "second"} {
		data, err := os.ReadFile(filepath.Join(dir, "levels", name+".scene.yaml"))
		if err != nil || !strings.Contains(string(data), "id: door") {
			t.Fatalf("%s.scene.yaml (%v):\n%s", name, err, data)
		}
	}
	checkProject(t, dir)

	// New Scene while the active tab has unsaved edits opens the new scene in a new tab without prompting and keeps the previous tab dirty.
	app.Post(func() {
		if err := ed.MoveObject("door", 1, 2); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	app.Press("id:toolbar-file")
	app.Settle()
	app.Press("id:toolbar-new")
	app.Settle()
	app.Clear("id:scene-path-input")
	app.Type("id:scene-path-input", "levels/third")
	app.Settle()
	app.Press("id:scene-path-confirm")
	app.Settle()
	if app.Exists("id:unsaved-dialog") || ed.ScenePath != "levels/third.scene.yaml" || ed.TabCount() != 2 || !ed.TabDirty(0) || ed.SceneDirty() {
		t.Fatalf("after New in tab: prompt %v, ScenePath %q, tabs %d, tab0 dirty %v, active dirty %v", app.Exists("id:unsaved-dialog"), ed.ScenePath, ed.TabCount(), ed.TabDirty(0), ed.SceneDirty())
	}
	if _, err := os.Stat(filepath.Join(dir, "levels", "third.scene.yaml")); err != nil {
		t.Fatalf("New did not create levels/third.scene.yaml: %v", err)
	}
}

// TestQuitAsksWhileEditsAreUnsaved quits with and without unsaved edits.
func TestQuitAsksWhileEditsAreUnsaved(t *testing.T) {
	ed, _ := openTestProject(t)
	_ = ed.SetAutoSave(false)
	app := startEditorApp(t, ed)

	app.Post(func() {
		if err := ed.MoveObject("hero", 5, 5); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	app.RequestQuit(false)
	app.Settle()
	if !app.Exists("id:unsaved-dialog") || app.Quits != 0 {
		t.Fatalf("quitting over unsaved edits: prompt %v, quits %d", app.Exists("id:unsaved-dialog"), app.Quits)
	}
	app.Press("id:unsaved-cancel")
	app.Settle()
	if app.Quits != 0 || !ed.SceneDirty() {
		t.Fatalf("after Cancel: quits %d, dirty %v", app.Quits, ed.SceneDirty())
	}

	// A forced quit, as at logout, cannot wait for an answer.
	app.RequestQuit(true)
	app.Settle()
	if app.Exists("id:unsaved-dialog") {
		t.Fatal("a forced quit asked")
	}

	app.RequestQuit(false)
	app.Settle()
	quits := app.Quits
	app.Press("id:unsaved-save")
	app.Settle()
	if ed.SceneDirty() || app.Quits != quits+1 {
		t.Fatalf("Save before quitting: dirty %v, quits %d, want %d, status %q", ed.SceneDirty(), app.Quits, quits+1, ed.Status)
	}
	quits = app.Quits
	app.Post(func() {
		if err := ed.MoveObject("hero", 6, 6); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	app.RequestQuit(false)
	app.Settle()
	app.Press("id:unsaved-discard")
	app.Settle()
	if app.Quits != quits+1 {
		t.Fatalf("Discard did not quit: quits %d, want %d", app.Quits, quits+1)
	}
}

// TestQuitDuringPlaySaves asks to quit while Play runs over unsaved edits: Save stops Play, saves the scene as it was before Play, and quits.
func TestQuitDuringPlaySaves(t *testing.T) {
	ed, dir := openTestProject(t)
	app := startEditorApp(t, ed)
	app.Post(func() {
		if err := ed.MoveObject("hero", 9, 9); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	app.Press("id:toolbar-play")
	app.Settle()
	if !ed.Playing() {
		t.Fatal("Play did not start")
	}
	app.RequestQuit(false)
	app.Settle()
	if !app.Exists("id:unsaved-dialog") {
		t.Fatal("quitting during Play over unsaved edits did not ask")
	}
	app.Press("id:unsaved-save")
	app.Settle()
	if ed.Playing() || ed.SceneDirty() || app.Quits != 1 {
		t.Fatalf("after Save: playing %v, dirty %v, quits %d, status %q", ed.Playing(), ed.SceneDirty(), app.Quits, ed.Status)
	}
	data, err := os.ReadFile(filepath.Join(dir, "scenes", "room.yaml"))
	if err != nil || !strings.Contains(string(data), "x: 9") {
		t.Fatalf("Save did not write the edit made before Play (%v):\n%s", err, data)
	}
}

// TestAppNameTitlesTheWindow gives the editor its own name, as an editor built on this package does, and the window title ends with it.
func TestAppNameTitlesTheWindow(t *testing.T) {
	ed, _ := openTestProject(t)
	ed.AppName = "Field Medic Editor"
	app := startEditorApp(t, ed)
	if app.Title != "scenes/room.yaml - Field Medic Editor" {
		t.Fatalf("title %q, want the editor's own name", app.Title)
	}
}

// TestDiscardFollowsTheProjectBack discards a project edit that added an audio bus being auditioned when switching away from the project: the audition through the bus that is gone stops, as it does when the edit is undone.
func TestDiscardFollowsTheProjectBack(t *testing.T) {
	ed, dir := openTestProject(t)
	_ = ed.SetAutoSave(false)
	rawWAV, _, _ := makeTestAudioWAV(t)
	if err := os.MkdirAll(filepath.Join(dir, "audio"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "audio", "impulse.wav"), rawWAV, 0o644); err != nil {
		t.Fatal(err)
	}
	app := startEditorApp(t, ed)
	app.Post(func() {
		if err := ed.AddAudioBus("sfx2"); err != nil {
			t.Error(err)
		}
		if err := ed.AuditionAudioBus("sfx2", "audio/impulse.wav"); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	if ed.AuditionBus() != "sfx2" || !ed.ProjectDirty() {
		t.Fatalf("auditioning %q, project dirty %v, want the new bus and a dirty project", ed.AuditionBus(), ed.ProjectDirty())
	}
	app.Post(func() {
		if err := ed.RequestOpenDemo(); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	app.Press("id:unsaved-discard")
	app.Settle()
	if ed.HasUnsavedChanges() {
		t.Fatalf("after Discard: unsaved %v", ed.HasUnsavedChanges())
	}
	if ed.Auditioning() || ed.AuditionBus() != "" || len(ed.AudioBuses()) != 0 {
		t.Fatalf("after Discard: auditioning %v through %q, buses %v, want the audition stopped with its bus", ed.Auditioning(), ed.AuditionBus(), ed.AudioBuses())
	}
}

// TestSaveLeavesAnUneditedSceneAsWritten saves with only the project edited: the scene file, comments included, is not rewritten.
func TestSaveLeavesAnUneditedSceneAsWritten(t *testing.T) {
	ed, dir := openTestProject(t)
	scene := filepath.Join(dir, "scenes", "room.yaml")
	data, err := os.ReadFile(scene)
	if err != nil {
		t.Fatal(err)
	}
	written := append([]byte("# kept by hand\n"), data...)
	if err := os.WriteFile(scene, written, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenScene("scenes/room.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.EditProject("rename", func(f *project.File) { f.Name = "Renamed" }); err != nil {
		t.Fatal(err)
	}
	if err := ed.Save(); err != nil || ed.HasUnsavedChanges() {
		t.Fatalf("Save = %v, unsaved %v", err, ed.HasUnsavedChanges())
	}
	got, err := os.ReadFile(scene)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(written) {
		t.Fatalf("Save rewrote the unedited scene:\n%s", got)
	}
}

// TestClosingTheWindowAsksWhileEditsAreUnsaved closes the window, as its close button does, over unsaved edits on macOS, where closing the last window ends the app without asking OnQuitRequest: Cancel keeps the window and the edits, and Discard closes it.
func TestClosingTheWindowAsksWhileEditsAreUnsaved(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("closes the headless model's window")
	}
	ed, _ := openTestProject(t)
	_ = ed.SetAutoSave(false)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680), nsgotest.Environment(core.Environment{OS: core.OSMacOS}))
	app.Post(func() {
		if err := ed.MoveObject("hero", 5, 5); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	if app.CloseWindow() {
		t.Fatal("the window closed over unsaved edits without asking")
	}
	app.Settle()
	if !app.Exists("id:unsaved-dialog") {
		t.Fatal("closing the window over unsaved edits did not ask")
	}
	app.Press("id:unsaved-cancel")
	app.Settle()
	if !ed.SceneDirty() || app.Exists("id:unsaved-dialog") {
		t.Fatalf("after Cancel: dirty %v, prompt %v", ed.SceneDirty(), app.Exists("id:unsaved-dialog"))
	}
	if app.CloseWindow() {
		t.Fatal("the window closed over unsaved edits without asking")
	}
	app.Settle()
	app.Press("id:unsaved-discard")
	app.Settle()
	if len(app.Query("id:scene-editor")) != 0 {
		t.Fatal("Discard left the window open")
	}
}

// TestSearchResultOpensAnotherSceneInATab presses a search result in a scene that is not open while the project has unsaved edits: the result's scene opens in a new tab without prompting, clears OnSave on the new tab, and keeps the previous tab and shared project edits intact.
func TestSearchResultOpensAnotherSceneInATab(t *testing.T) {
	dir := writeSearchFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))
	app.Post(func() {
		if err := ed.OpenProject(dir); err != nil {
			t.Error(err)
		}
		ed.OnSave = func([]byte) error { return nil }
		if err := ed.EditProject("rename", func(f *project.File) { f.Name = "Renamed" }); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	openFileBrowserInUI(app, ed)
	app.Press("id:files-search")
	app.Settle()
	app.Type("id:project-search-input", "goblin_king")
	app.Settle()

	app.Press("id:search-result-0")
	app.Settle()
	if app.Exists("id:unsaved-dialog") {
		t.Fatal("opening a search result's scene in a tab should not prompt")
	}
	if ed.ScenePath != "scenes/dungeon.scene.yaml" || ed.SelectedID != "goblin_king" || ed.TabCount() != 2 || !ed.ProjectDirty() || ed.OnSave != nil {
		t.Fatalf("after opening search result: scene %q, selected %q, tabs %d, projectDirty %v, OnSave kept %v", ed.ScenePath, ed.SelectedID, ed.TabCount(), ed.ProjectDirty(), ed.OnSave != nil)
	}
}

// TestCommandPaletteOpensAnotherSceneInATab opens another scene from the Command Palette while the active tab has unsaved edits: the scene opens in a new tab without prompting, keeping the first tab dirty with its OnSave and undo history, and giving the new tab OnSave == nil.
func TestCommandPaletteOpensAnotherSceneInATab(t *testing.T) {
	ed, _ := openTestProject(t)
	app := startEditorApp(t, ed)
	app.Post(func() {
		ed.OnSave = func([]byte) error { return nil }
		if err := ed.MoveObject("hero", 70, 80); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	app.Post(ed.OpenCommandPalette)
	app.Settle()
	app.Type("id:editor-command-palette >> role:textbox", "other.scene")
	app.Key("enter")
	app.Settle()
	if app.Exists("id:unsaved-dialog") {
		t.Fatal("opening a scene from the palette in a tab should not prompt")
	}
	if ed.ScenePath != "scenes/other.scene.yaml" || ed.Doc.Name != "Other" || ed.TabCount() != 2 || !ed.TabDirty(0) || ed.SceneDirty() || ed.OnSave != nil {
		t.Fatalf("after palette open: scene %q (%q), tabs %d, tab0 dirty %v, active dirty %v, OnSave kept %v", ed.ScenePath, ed.Doc.Name, ed.TabCount(), ed.TabDirty(0), ed.SceneDirty(), ed.OnSave != nil)
	}
}

// TestToolbarHeightStableWhenSceneBecomesDirty ensures showing or hiding the dirty "*" marker never changes the toolbar's height or shifts the viewport canvas vertically.
func TestToolbarHeightStableWhenSceneBecomesDirty(t *testing.T) {
	ed, _ := openTestProject(t)
	app := startEditorApp(t, ed)

	tbClean := app.Find("id:editor-toolbar").Bounds()
	vpClean := app.Find("id:editor-viewport").Bounds()
	if app.Exists("id:toolbar-unsaved") {
		t.Fatal("clean scene should not show toolbar-unsaved")
	}

	app.Post(func() {
		if err := ed.MoveObject("hero", 75, 80); err != nil {
			t.Error(err)
		}
	})
	app.Settle()

	if !app.Exists("id:toolbar-unsaved") {
		t.Fatal("dirty scene should show toolbar-unsaved")
	}
	tbDirty := app.Find("id:editor-toolbar").Bounds()
	vpDirty := app.Find("id:editor-viewport").Bounds()
	if tbDirty.Size.Height != tbClean.Size.Height || vpDirty.Origin.Y != vpClean.Origin.Y || vpDirty.Size.Height != vpClean.Size.Height {
		t.Fatalf("toolbar/viewport shifted when scene became dirty: toolbar height %v -> %v, viewport Y %v -> %v, viewport height %v -> %v",
			tbClean.Size.Height, tbDirty.Size.Height, vpClean.Origin.Y, vpDirty.Origin.Y, vpClean.Size.Height, vpDirty.Size.Height)
	}
}
