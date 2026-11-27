package editor_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const projectYAML = `# The project file, with a comment an unedited save keeps.
version: 1
name: Demo
id: dev.example.demo
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.yaml
`

// writeEditorProject writes a project whose main scene is testSceneDoc, with an nsgo.yaml beside game.yaml, and returns its directory.
func writeEditorProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	scene, err := sceneformat.Encode(testSceneDoc())
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		project.FileName:     projectYAML,
		project.NSGoFileName: "name: Demo\nid: dev.example.demo\nversion: 1.0.0\ndev:\n  target: native\n",
		"scenes/room.yaml":   string(scene),
	}
	for name, data := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestOpenEditUndoAndSaveProject(t *testing.T) {
	dir := writeEditorProject(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.EditProject("rename", func(*project.File) {}); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("EditProject without a project = %v, want ErrNoProject", err)
	}
	if err := ed.SaveProject(); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("SaveProject without a project = %v, want ErrNoProject", err)
	}

	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if ed.Project == nil || ed.Project.Name != "Demo" || ed.ProjectPath != filepath.Join(dir, project.FileName) {
		t.Fatalf("Project = %+v at %q", ed.Project, ed.ProjectPath)
	}
	if ed.Doc.Name != testSceneDoc().Name || ed.FilePath != filepath.Join(dir, "scenes", "room.yaml") {
		t.Fatalf("OpenProject opened scene %q from %q, want the main scene", ed.Doc.Name, ed.FilePath)
	}
	if ed.ProjectDirty() {
		t.Fatal("an unedited project is dirty")
	}

	// An edit Validate refuses changes nothing.
	if err := ed.EditProject("zero window", func(f *project.File) { f.Window.Width = 0 }); err == nil {
		t.Fatal("EditProject accepted an invalid window")
	}
	if ed.Project.Window.Width != 640 || ed.History.CanUndo() {
		t.Fatal("a refused edit changed the project or the history")
	}

	if err := ed.EditProject("rename", func(f *project.File) {
		f.Name = "Demo Deluxe"
		f.Display = &project.Display{Width: 320, Height: 180}
	}); err != nil {
		t.Fatal(err)
	}
	if ed.Project.Name != "Demo Deluxe" || !ed.ProjectDirty() {
		t.Fatalf("after EditProject: name %q, dirty %v", ed.Project.Name, ed.ProjectDirty())
	}
	if !ed.Undo() || ed.Project.Name != "Demo" || ed.Project.Display != nil || ed.ProjectDirty() {
		t.Fatalf("after Undo: %+v, dirty %v", ed.Project, ed.ProjectDirty())
	}
	if !ed.Redo() || ed.Project.Name != "Demo Deluxe" {
		t.Fatalf("after Redo: %+v", ed.Project)
	}

	if err := ed.SaveProject(); err != nil {
		t.Fatal(err)
	}
	if ed.ProjectDirty() {
		t.Fatal("a saved project is dirty")
	}
	saved, err := project.Check(os.DirFS(dir))
	if err != nil {
		t.Fatalf("Check after SaveProject: %v", err)
	}
	if saved.Name != "Demo Deluxe" || saved.Display == nil || saved.Display.Width != 320 {
		t.Fatalf("saved project = %+v", saved)
	}
	nsgo, err := os.ReadFile(filepath.Join(dir, project.NSGoFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(nsgo), "name: Demo Deluxe\n") || !strings.Contains(string(nsgo), "target: native") {
		t.Fatalf("nsgo.yaml after SaveProject:\n%s", nsgo)
	}

	// Another editor reads the project back as saved.
	other, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := other.OpenProject(filepath.Join(dir, project.FileName)); err != nil || other.Project.Name != "Demo Deluxe" {
		t.Fatalf("reopen = %+v, %v", other.Project, err)
	}
}

func TestOpenProjectFailuresLeaveTheEditorAsItWas(t *testing.T) {
	ed := newTestEditor(t)
	if err := ed.OpenProject(t.TempDir()); err == nil {
		t.Fatal("OpenProject of a directory without game.yaml succeeded")
	}
	dir := writeEditorProject(t)
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte("version: 1\nname: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err == nil {
		t.Fatal("OpenProject of an invalid game.yaml succeeded")
	}
	dir = writeEditorProject(t)
	if err := os.Remove(filepath.Join(dir, "scenes", "room.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err == nil || !strings.Contains(err.Error(), "room") {
		t.Fatalf("OpenProject with a missing main scene = %v", err)
	}
	if ed.Project != nil || ed.Doc.Name != testSceneDoc().Name {
		t.Fatalf("a failed OpenProject changed the editor: project %+v, scene %q", ed.Project, ed.Doc.Name)
	}
}

func TestToolbarSaveWritesAnEditedProjectOnly(t *testing.T) {
	dir := writeEditorProject(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))

	// Saving the scene leaves an unedited game.yaml as written, its comment included.
	saveInUI(app, ed)
	data, err := os.ReadFile(filepath.Join(dir, project.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != projectYAML {
		t.Fatalf("an unedited game.yaml was rewritten:\n%s", data)
	}

	app.Post(func() {
		if err := ed.EditProject("version", func(f *project.File) { f.AppVersion = "1.1.0" }); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	saveInUI(app, ed)
	f, err := project.Check(os.DirFS(dir))
	if err != nil || f.AppVersion != "1.1.0" {
		t.Fatalf("after Save: %+v, %v", f, err)
	}
}

// writeBareProject writes a game.yaml named name with no main scene into a new directory and returns the directory.
func writeBareProject(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	data := "version: 1\nname: " + name + "\nid: dev.example.bare\nappVersion: 1.0.0\nwindow:\n  width: 640\n  height: 360\n"
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestOpenProjectDropsTheHistoryOfTheProjectBefore(t *testing.T) {
	ed := newTestEditor(t)
	if err := ed.OpenProject(writeEditorProject(t)); err != nil {
		t.Fatal(err)
	}
	if err := ed.EditProject("rename", func(f *project.File) { f.Name = "Demo Deluxe" }); err != nil {
		t.Fatal(err)
	}
	dir := writeBareProject(t, "Bare")
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if ed.History.CanUndo() || ed.History.CanRedo() {
		t.Fatal("the history of the project opened before survived OpenProject")
	}
	ed.History.Undo()
	if ed.Project.Name != "Bare" || ed.ProjectDirty() {
		t.Fatalf("after Undo the project is %q (dirty %v), want the unedited Bare", ed.Project.Name, ed.ProjectDirty())
	}
}

func TestToolbarSaveWritesAProjectWithoutAMainScene(t *testing.T) {
	dir := writeBareProject(t, "Bare")
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))
	app.Post(func() {
		if err := ed.EditProject("version", func(f *project.File) { f.AppVersion = "1.1.0" }); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	saveInUI(app, ed)
	f, err := project.Check(os.DirFS(dir))
	if err != nil || f.AppVersion != "1.1.0" {
		t.Fatalf("after Save: %+v, %v", f, err)
	}
	if ed.ProjectDirty() || strings.HasPrefix(ed.Status, "Save error") {
		t.Fatalf("after Save: dirty %v, status %q", ed.ProjectDirty(), ed.Status)
	}
}
