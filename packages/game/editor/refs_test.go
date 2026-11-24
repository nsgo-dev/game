package editor_test

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/refs"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMoveFileRewritesReferences(t *testing.T) {
	dir := writeEditorProject(t)
	doc := testSceneDoc()
	doc.Layers[1].Objects[0].Material = "materials/stone.material.yaml"
	doc.Layers[1].Objects[0].Children = []sceneformat.ObjectDoc{{ID: "shield", Material: "materials/stone.material.yaml"}}
	scene, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"scenes/room.yaml":              string(scene),
		"materials/stone.material.yaml": "type: material\nversion: 1\nuid: mat0000000001\n",
		"scenes/other.scene.yaml":       "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: wall\n        material: materials/stone.material.yaml\n",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.MoveFile("a", "b"); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("MoveFile without a project = %v, want ErrNoProject", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.ScenePath = "scenes/room.yaml"
	// An unsaved edit survives the move.
	if err := ed.MoveObject("hero", 50, 60); err != nil {
		t.Fatal(err)
	}

	if err := ed.MoveFile("materials/stone.material.yaml", "materials/rock.material.yaml"); err != nil {
		t.Fatal(err)
	}
	hero, _, _ := ed.FindObject("hero")
	if hero == nil || hero.Material != "materials/rock.material.yaml" || hero.X != 50 || len(hero.Children) != 1 || hero.Children[0].Material != "materials/rock.material.yaml" {
		t.Fatalf("hero %+v", hero)
	}
	for _, name := range []string{"scenes/room.yaml", "scenes/other.scene.yaml"} {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "stone.material") || !strings.Contains(string(data), "material: materials/rock.material.yaml") {
			t.Fatalf("%s:\n%s", name, data)
		}
	}

	// Moving the open scene moves FilePath and ScenePath with it, and game.yaml's autoload.
	if err := ed.MoveFile("scenes", "levels"); err != nil {
		t.Fatal(err)
	}
	if ed.FilePath != filepath.Join(dir, "levels", "room.yaml") || ed.ScenePath != "levels/room.yaml" {
		t.Fatalf("FilePath %q, ScenePath %q", ed.FilePath, ed.ScenePath)
	}
	if got := ed.Project.Autoload.Scenes[0].Path; got != "levels/room.yaml" {
		t.Fatalf("autoload path %q", got)
	}
	if ed.ProjectDirty() {
		t.Fatal("the move left the project dirty")
	}
	data, err := os.ReadFile(filepath.Join(dir, "game.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Replace(projectYAML, "path: scenes/room.yaml", "path: levels/room.yaml", 1); string(data) != want {
		t.Fatalf("game.yaml:\n%s\nwant (comment kept):\n%s", data, want)
	}
	if _, err := os.Stat(filepath.Join(dir, refs.IndexFileName)); err != nil {
		t.Fatalf("no index: %v", err)
	}
	if err := ed.SaveFile(ed.FilePath); err != nil {
		t.Fatal(err)
	}
	p, err := refs.Scan(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	if probs := p.Check(); len(probs) != 0 {
		t.Fatalf("problems after the moves: %v", probs)
	}

	// A move the plan refuses changes nothing.
	if err := ed.MoveFile("levels/room.yaml", "levels/other.scene.yaml"); err == nil {
		t.Fatal("moving onto an existing file succeeded")
	}
	if ed.FilePath != filepath.Join(dir, "levels", "room.yaml") {
		t.Fatalf("FilePath %q after a refused move", ed.FilePath)
	}

	// Undo takes the moves back one at a time, and then the edit made before them.
	if !ed.Undo() {
		t.Fatalf("undoing the directory move failed: %s", ed.Status)
	}
	if ed.FilePath != filepath.Join(dir, "scenes", "room.yaml") || ed.ScenePath != "scenes/room.yaml" || ed.Project.Autoload.Scenes[0].Path != "scenes/room.yaml" {
		t.Fatalf("after undo: FilePath %q, ScenePath %q, autoload %q", ed.FilePath, ed.ScenePath, ed.Project.Autoload.Scenes[0].Path)
	}
	if _, err := os.Stat(filepath.Join(dir, "levels")); !os.IsNotExist(err) {
		t.Fatalf("levels is still there after undo: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "game.yaml")); err != nil || string(data) != projectYAML {
		t.Fatalf("game.yaml after undo (%v):\n%s", err, data)
	}
	if !ed.Undo() {
		t.Fatalf("undoing the material move failed: %s", ed.Status)
	}
	if hero, _, _ := ed.FindObject("hero"); hero.Material != "materials/stone.material.yaml" || hero.Children[0].Material != "materials/stone.material.yaml" || hero.X != 50 {
		t.Fatalf("hero after undo %+v", hero)
	}
	if _, err := os.Stat(filepath.Join(dir, "materials", "stone.material.yaml")); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "scenes", "other.scene.yaml")); err != nil || !strings.Contains(string(data), "material: materials/stone.material.yaml") {
		t.Fatalf("other.scene.yaml after undo (%v):\n%s", err, data)
	}
	if !ed.Undo() {
		t.Fatal("the edit made before the moves could not be undone")
	}
	if hero, _, _ := ed.FindObject("hero"); hero.X == 50 {
		t.Fatalf("hero still at x 50 after undoing its move: %+v", hero)
	}

	// Redo makes them again.
	for range 3 {
		if !ed.Redo() {
			t.Fatalf("redo failed: %s", ed.Status)
		}
	}
	if ed.FilePath != filepath.Join(dir, "levels", "room.yaml") {
		t.Fatalf("FilePath %q after redo", ed.FilePath)
	}
	if hero, _, _ := ed.FindObject("hero"); hero.Material != "materials/rock.material.yaml" || hero.X != 50 {
		t.Fatalf("hero after redo %+v", hero)
	}
	if p, err := refs.Scan(os.DirFS(dir)); err != nil {
		t.Fatal(err)
	} else if probs := p.Check(); len(probs) != 0 {
		t.Fatalf("problems after redo: %v", probs)
	}
}

func TestMoveFileUndoRemovesMadeDirectoriesAndReportsABlockedStep(t *testing.T) {
	dir := writeEditorProject(t)
	mat := filepath.Join(dir, "materials", "stone.material.yaml")
	if err := os.MkdirAll(filepath.Dir(mat), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mat, []byte("type: material\nversion: 1\nuid: mat0000000001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	if err := ed.MoveFile("materials/stone.material.yaml", "art/stone/stone.material.yaml"); err != nil {
		t.Fatal(err)
	}
	if got := ed.History.UndoLabel(); got != "Move materials/stone.material.yaml to art/stone/stone.material.yaml" {
		t.Fatalf("undo label %q", got)
	}
	if !ed.Undo() {
		t.Fatalf("undo failed: %s", ed.Status)
	}
	if _, err := os.Stat(filepath.Join(dir, "art")); !os.IsNotExist(err) {
		t.Fatalf("the directories the move made are still there: %v", err)
	}
	if _, err := os.Stat(mat); err != nil {
		t.Fatal(err)
	}

	// A file put where the redo would move to blocks it: the step stays undone and the status bar says why.
	if err := os.MkdirAll(filepath.Join(dir, "art", "stone"), 0o755); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(dir, "art", "stone", "stone.material.yaml")
	if err := os.WriteFile(blocker, []byte("type: material\nversion: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ed.Redo() {
		t.Fatal("a redo onto an existing file succeeded")
	}
	if !strings.HasPrefix(ed.Status, "Redo failed: ") || !ed.History.CanRedo() {
		t.Fatalf("status %q, can redo %v", ed.Status, ed.History.CanRedo())
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if !ed.Redo() {
		t.Fatalf("redo failed once unblocked: %s", ed.Status)
	}
	// The directory that was there before the redo is not one the move made, so undo keeps it.
	if !ed.Undo() {
		t.Fatalf("undo failed: %s", ed.Status)
	}
	if _, err := os.Stat(filepath.Join(dir, "art", "stone")); err != nil {
		t.Fatalf("undo removed a directory the move did not make: %v", err)
	}
}

// TestMoveFileUndoIsExact moves files and undoes the moves: every file is back byte for byte, a reference that already named the destination is left alone, a path a moved Tiled map wrote as ./tiles.png keeps that form, and an index the move created goes again. A file changed since the move has its references rewritten back instead.
func TestMoveFileUndoIsExact(t *testing.T) {
	dir := writeEditorProject(t)
	writeFiles(t, dir, map[string]string{
		"materials/stone.material.yaml": "type: material\nversion: 1\nuid: mat0000000001\n",
		"scenes/other.scene.yaml":       "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: wall\n        material: materials/stone.material.yaml\n",
		"scenes/ghost.scene.yaml":       "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: wall\n        material: materials/rock.material.yaml\n",
		"maps/level.tmj":                `{"tilesets": [{"image": "./tiles.png"}]}` + "\n",
		"maps/tiles.png":                "png",
	})
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	before := readTree(t, dir)

	if err := ed.MoveFile("materials/stone.material.yaml", "materials/rock.material.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.MoveFile("maps/level.tmj", "levels/level.tmj"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "levels", "level.tmj")); err != nil || !strings.Contains(string(data), `"../maps/tiles.png"`) {
		t.Fatalf("level.tmj after the move (%v):\n%s", err, data)
	}
	for range 2 {
		if !ed.Undo() {
			t.Fatalf("undo failed: %s", ed.Status)
		}
	}
	if got := readTree(t, dir); !maps.Equal(got, before) {
		t.Fatalf("after undo:\n%v\nwant:\n%v", got, before)
	}

	// A file edited after the move keeps the edit, and its reference goes back.
	if err := ed.MoveFile("materials/stone.material.yaml", "materials/rock.material.yaml"); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "scenes", "other.scene.yaml")
	data, err := os.ReadFile(other)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, append([]byte("# edited\n"), data...), 0o644); err != nil {
		t.Fatal(err)
	}
	if !ed.Undo() {
		t.Fatalf("undo failed: %s", ed.Status)
	}
	if data, err := os.ReadFile(other); err != nil || string(data) != "# edited\n"+before["scenes/other.scene.yaml"] {
		t.Fatalf("other.scene.yaml after undo (%v):\n%s", err, data)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "scenes", "ghost.scene.yaml")); err != nil || string(data) != before["scenes/ghost.scene.yaml"] {
		t.Fatalf("ghost.scene.yaml after undo (%v):\n%s", err, data)
	}
}

func TestMoveFileUndoFailsWhenBlocked(t *testing.T) {
	dir := writeEditorProject(t)
	mat := filepath.Join(dir, "materials", "stone.material.yaml")
	writeFiles(t, dir, map[string]string{"materials/stone.material.yaml": "type: material\nversion: 1\nuid: mat0000000001\n"})
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.MoveFile("materials/stone.material.yaml", "art/stone/stone.material.yaml"); err != nil {
		t.Fatal(err)
	}

	// A file put where the undo would move back to blocks it: the move stays applied and the status bar says why.
	if err := os.WriteFile(mat, []byte("type: material\nversion: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ed.Undo() {
		t.Fatal("an undo onto an existing file succeeded")
	}
	if !strings.HasPrefix(ed.Status, "Undo failed: ") || !ed.History.CanUndo() || ed.History.CanRedo() {
		t.Fatalf("status %q, can undo %v, can redo %v", ed.Status, ed.History.CanUndo(), ed.History.CanRedo())
	}
	if _, err := os.Stat(filepath.Join(dir, "art", "stone", "stone.material.yaml")); err != nil {
		t.Fatalf("the failed undo moved the file: %v", err)
	}
	if err := os.Remove(mat); err != nil {
		t.Fatal(err)
	}

	// A directory the move made that now holds another file stays; the one left empty goes.
	writeFiles(t, dir, map[string]string{"art/notes.txt": "keep"})
	if !ed.Undo() {
		t.Fatalf("undo failed once unblocked: %s", ed.Status)
	}
	if _, err := os.Stat(mat); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "art", "stone")); !os.IsNotExist(err) {
		t.Fatalf("art/stone is still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "art", "notes.txt")); err != nil {
		t.Fatalf("undo removed a directory holding another file: %v", err)
	}
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// readTree returns every file under dir by its slash-separated path, with its contents.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	err := fs.WalkDir(os.DirFS(dir), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(os.DirFS(dir), p)
		out[p] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestProjectFiles(t *testing.T) {
	dir := writeEditorProject(t)
	for _, name := range []string{".git/HEAD", "_scratch/a.txt", "testdata/x.txt", "art/.hidden.png", "art/_hero.png", "art/hero.png"} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ed.ProjectFiles(); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("ProjectFiles without a project = %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	got, err := ed.ProjectFiles()
	if err != nil {
		t.Fatal(err)
	}
	want := []editor.ProjectFile{
		{Path: "art", Dir: true},
		{Path: "art/.hidden.png"},
		{Path: "art/_hero.png"},
		{Path: "art/hero.png"},
		{Path: "game.yaml"},
		{Path: "nsgo.yaml"},
		{Path: "scenes", Dir: true},
		{Path: "scenes/room.yaml"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("ProjectFiles = %+v, want %+v", got, want)
	}
}

// TestFileBrowserMovesAFile renames a material from the file browser: the references to it follow, the list shows the new name, and the toolbar's Undo moves it back.
func TestFileBrowserMovesAFile(t *testing.T) {
	dir := writeEditorProject(t)
	for name, data := range map[string]string{
		"materials/stone.material.yaml": "type: material\nversion: 1\nuid: mat0000000001\n",
		"scenes/other.scene.yaml":       "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: wall\n        material: materials/stone.material.yaml\n",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))
	if app.Exists("id:file-browser") {
		t.Fatal("the file browser shows without a project")
	}
	app.Post(func() {
		if err := ed.OpenProject(dir); err != nil {
			t.Error(err)
		}
	})
	app.Settle()

	if app.Enabled("id:files-move") {
		t.Fatal("Move is enabled with nothing selected")
	}
	app.Press("id:files-row-materials/stone.material.yaml")
	app.Settle()
	app.Clear("id:files-path")
	app.Type("id:files-path", "materials/rock.material.yaml")
	app.Settle()
	app.Press("id:files-move")
	app.Settle()

	other := filepath.Join(dir, "scenes", "other.scene.yaml")
	if data, err := os.ReadFile(other); err != nil || !strings.Contains(string(data), "material: materials/rock.material.yaml") {
		t.Fatalf("other.scene.yaml after the move (%v):\n%s\nstatus %q", err, data, ed.Status)
	}
	if !app.Exists("id:files-row-materials/rock.material.yaml") || app.Exists("id:files-row-materials/stone.material.yaml") {
		t.Fatal("the file list does not show the new name")
	}

	app.Press("id:toolbar-undo")
	app.Settle()
	if data, err := os.ReadFile(other); err != nil || !strings.Contains(string(data), "material: materials/stone.material.yaml") {
		t.Fatalf("other.scene.yaml after undo (%v):\n%s", err, data)
	}
	if !app.Exists("id:files-row-materials/stone.material.yaml") {
		t.Fatal("the file list does not show the name undo brought back")
	}
	// The selection was the name undo took away, so nothing is selected any more.
	if got := app.Find("id:files-path").Text; got != "" || app.Enabled("id:files-move") {
		t.Fatalf("after undo the field holds %q and Move is enabled %v", got, app.Enabled("id:files-move"))
	}

	// A scene saved under a new name shows in the list.
	app.Post(func() {
		if err := ed.SaveFile(filepath.Join(dir, "scenes", "copy.yaml")); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	if !app.Exists("id:files-row-scenes/copy.yaml") {
		t.Fatal("the file list does not show the scene saved under a new name")
	}
}
