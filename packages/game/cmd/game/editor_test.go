package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editorcmd"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestEditorOpensStartScreenWithoutArgsAndDemoActionWorks(t *testing.T) {
	a, _, stderr := newTestApp(t)
	var opened *editor.Editor
	a.launch = func(ed *editor.Editor) { opened = ed }
	if code := run([]string{"editor"}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if opened == nil || !opened.StartScreenOpen() {
		t.Fatalf("launched %+v, want Start Screen open", opened)
	}
	tmpls := opened.ProjectTemplates()
	if len(tmpls) != len(projectTemplates) {
		t.Fatalf("Start Screen has %d templates, want %d from projectTemplates", len(tmpls), len(projectTemplates))
	}
	for i, want := range projectTemplates {
		if tmpls[i].Name != want.name || tmpls[i].Summary != want.summary {
			t.Errorf("template[%d] = %+v, want %q / %q", i, tmpls[i], want.name, want.summary)
		}
	}
	if err := opened.OpenDemo(); err != nil {
		t.Fatalf("OpenDemo: %v", err)
	}
	if opened.StartScreenOpen() || opened.Doc.Name != "Demo Room" || opened.FilePath != "" {
		t.Fatalf("after OpenDemo: startOpen=%v doc=%q file=%q, want Demo Room", opened.StartScreenOpen(), opened.Doc.Name, opened.FilePath)
	}
}

func TestEditorOpensSceneFile(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Cave",
		VirtualWidth:  320,
		VirtualHeight: 180,
	}
	data, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cave.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	a, _, stderr := newTestApp(t)
	var opened *editor.Editor
	a.launch = func(ed *editor.Editor) { opened = ed }
	if code := run([]string{"editor", path}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if opened == nil || opened.Doc.Name != "Cave" || opened.FilePath != path {
		t.Fatalf("launched %+v, want %s", opened, path)
	}
}

func TestEditorErrors(t *testing.T) {
	a, _, stderr := newTestApp(t)
	if code := run([]string{"editor", filepath.Join(t.TempDir(), "missing.yaml")}, a); code != 1 {
		t.Fatalf("missing scene exit %d, want 1", code)
	}
	if !strings.HasPrefix(stderr.String(), "game editor: open ") {
		t.Fatalf("stderr = %q", stderr)
	}

	a, _, _ = newTestApp(t)
	if code := run([]string{"editor", "a.yaml", "b.animation.yaml", "hero", "extra"}, a); code != 2 {
		t.Fatalf("four arguments exit %d, want 2", code)
	}
}

const heroClip = `type: animation
version: 1
name: Hop
length: 1
tracks:
  - property: x
    keys:
      - time: 0
        value: 64
      - time: 1
        value: 96
`

func TestOpenArgsOpensASavedSceneAndAnExistingClip(t *testing.T) {
	dir := t.TempDir()
	scene := filepath.Join(dir, "room.scene.yaml")
	clip := filepath.Join(dir, "hop.animation.yaml")
	demo, err := newDemoEditor()
	if err != nil {
		t.Fatalf("newDemoEditor: %v", err)
	}
	if err := demo.SaveFile(scene); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	if err := os.WriteFile(clip, []byte(heroClip), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := newDemoEditor()
	if err != nil {
		t.Fatalf("newDemoEditor: %v", err)
	}
	if err := editorcmd.Open(ed, []string{scene, clip}); err != nil {
		t.Fatalf("editorcmd.Open(scene, clip): %v", err)
	}
	if ed.Clip() == nil || ed.ClipTarget() != "hero" || ed.SelectedID != "hero" || ed.ClipPath() != clip {
		t.Fatalf("after editorcmd.Open: clip %v target %q selected %q path %q, want the clip on hero from %s", ed.Clip() != nil, ed.ClipTarget(), ed.SelectedID, ed.ClipPath(), clip)
	}
	if ed.OnSaveClip == nil {
		t.Fatal("OnSaveClip is not set")
	}
	data, err := ed.SaveClipYAML()
	if err != nil {
		t.Fatalf("SaveClipYAML: %v", err)
	}
	if string(data) != heroClip {
		t.Fatalf("clip after open and save:\n%s\nwant:\n%s", data, heroClip)
	}

	// Naming the object explicitly opens the clip on it.
	ed, _ = newDemoEditor()
	if err := editorcmd.Open(ed, []string{scene, clip, "hero"}); err != nil || ed.ClipTarget() != "hero" {
		t.Fatalf("editorcmd.Open with target hero: %v, target %q", err, ed.ClipTarget())
	}

	// A clip file that does not exist yet opens nothing but selects the object, so the Animate button starts a clip on it, and Save Clip writes the file.
	fresh := filepath.Join(dir, "new.animation.yaml")
	ed, _ = newDemoEditor()
	if err := editorcmd.Open(ed, []string{scene, fresh}); err != nil {
		t.Fatalf("editorcmd.Open with a new clip file: %v", err)
	}
	if ed.Clip() != nil || ed.SelectedID != "hero" || ed.OnSaveClip == nil {
		t.Fatalf("after editorcmd.Open with a new clip file: clip %v selected %q OnSaveClip %t", ed.Clip() != nil, ed.SelectedID, ed.OnSaveClip != nil)
	}
	if err := ed.OnSaveClip([]byte(heroClip)); err != nil {
		t.Fatalf("OnSaveClip: %v", err)
	}
	if got, err := os.ReadFile(fresh); err != nil || string(got) != heroClip {
		t.Fatalf("saved clip file: %q, %v", got, err)
	}

	ed, _ = newDemoEditor()
	if err := editorcmd.Open(ed, []string{scene, clip, "ghost"}); err == nil || !strings.Contains(err.Error(), `no object "ghost"`) {
		t.Fatalf("editorcmd.Open with a missing object: %v", err)
	}

	// The editor subcommand passes its arguments through editorcmd.Open.
	a, _, stderr := newTestApp(t)
	var opened *editor.Editor
	a.launch = func(ed *editor.Editor) { opened = ed }
	if code := run([]string{"editor", scene, clip}, a); code != 0 {
		t.Fatalf("editor scene clip exit %d (stderr: %s)", code, stderr)
	}
	if opened == nil || opened.ClipPath() != clip {
		t.Fatalf("editor scene clip launched %+v, want the clip %s open", opened, clip)
	}
}

const layeredProjectYAML = `version: 1
name: Cave
id: dev.example.cave
appVersion: 1.0.0
window:
  width: 320
  height: 180
autoload:
  mainScene: cave
  scenes:
    - name: cave
      path: scenes/cave.scene.yaml
`

const layeredCaveScene = `version: 2
name: Cave
layers:
  - name: ground
    tilemap:
      tileWidth: 16
      tileHeight: 16
      width: 2
      height: 1
      tilesetFile: tilesets/cave.tileset.yaml
      tileLayers:
        - name: floor
          rows:
            - 0 1
`

func TestEditorReadsTheOpenProjectsFiles(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string]string{
		"game.yaml":                  layeredProjectYAML,
		"scenes/cave.scene.yaml":     layeredCaveScene,
		"tilesets/cave.tileset.yaml": "version: 1\ntileWidth: 16\ntileHeight: 16\nimage: tiles/cave.png\ncolumns: 2\ntileCount: 2\n",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The project directory and a scene file inside it both read the tileset the scene names from the project root.
	for _, arg := range []string{dir, filepath.Join(dir, "scenes", "cave.scene.yaml")} {
		a, _, stderr := newTestApp(t)
		var opened *editor.Editor
		a.launch = func(ed *editor.Editor) { opened = ed }
		if code := run([]string{"editor", arg}, a); code != 0 {
			t.Fatalf("%s: exit %d (stderr: %s)", arg, code, stderr)
		}
		if opened.BuildErr != nil {
			t.Fatalf("%s: build: %v", arg, opened.BuildErr)
		}
		if m := opened.Built.Layer("ground").Map; m == nil || m.TileAt(0, 1, 0) != 2 {
			t.Fatalf("%s: ground map = %+v", arg, m)
		}
		opened.SetActiveLayer("ground")
		if p := opened.TilePalette(); len(p.Tiles) != 2 || p.Message != "" {
			t.Fatalf("%s: palette = %+v", arg, p)
		}
	}
}

// editorFixture is a project with its own atlas and two scene files, whose main scene draws the atlas and sets a component no registry declares.
const editorFixture = "testdata/editorproject"

func TestEditorOpensAProjectWithItsOwnAtlasesAndScenes(t *testing.T) {
	png, err := os.ReadFile(filepath.Join(editorFixture, "sprites", "actors.png"))
	if err != nil {
		t.Fatal(err)
	}
	a, _, stderr := newTestApp(t)
	var ed *editor.Editor
	a.launch = func(opened *editor.Editor) { ed = opened }
	if code := run([]string{"editor", editorFixture}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if ed == nil || ed.Doc.Name != "Main" || ed.ScenePath != "scenes/main.scene.yaml" || ed.BuildErr != nil {
		t.Fatalf("opened %+v", ed)
	}
	// The project has no editor of its own, so its components are free-form rather than the demo's.
	if ed.ComponentRegistry() != nil {
		t.Fatal("the project opened with a component registry")
	}
	hero := ed.Built.Object("hero")
	if hero == nil || hero.Atlas == nil || !bytes.Equal(hero.Atlas.Image.Data, png) {
		t.Fatalf("the hero does not draw the project's actors.png")
	}
	var keys []string
	for _, entry := range ed.Assets() {
		keys = append(keys, entry.Key())
	}
	if want := []string{"actors/crate", "actors/hero"}; !slices.Equal(keys, want) {
		t.Fatalf("the asset browser lists %q, want %q", keys, want)
	}

	id, err := ed.PlaceScene("actors", "scenes/crate.scene.yaml", 120, 60)
	if err != nil {
		t.Fatalf("PlaceScene: %v", err)
	}
	if box := ed.Built.Object(id + "/box"); box == nil || box.Atlas == nil || !bytes.Equal(box.Atlas.Image.Data, png) {
		t.Fatalf("the crate instance does not draw the project's actors.png")
	}
	if !ed.History.Undo() {
		t.Fatal("nothing to undo")
	}
	if obj, _, _ := ed.FindObject(id); obj != nil {
		t.Fatal("Undo left the crate instance")
	}
	if _, err := ed.PlaceScene("actors", "scenes/main.scene.yaml", 0, 0); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("placing the open scene in itself = %v, want a cycle", err)
	}

	// game check agrees with the editor on the fixture.
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"check", editorFixture}, a); code != 0 {
		t.Fatalf("game check: exit %d: %s", code, stderr)
	}
	if got, want := stdout.String(), "2 scenes, 0 maps, 0 tilesets, 1 atlas: no problems\n"; got != want {
		t.Errorf("game check: %q, want %q", got, want)
	}
}

func TestEditorOpensASceneInsideAProjectWithTheProject(t *testing.T) {
	a, _, stderr := newTestApp(t)
	var ed *editor.Editor
	a.launch = func(opened *editor.Editor) { ed = opened }
	if code := run([]string{"editor", filepath.Join(editorFixture, "scenes", "crate.scene.yaml")}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if ed == nil || ed.Project == nil || ed.Doc.Name != "Crate" || ed.ScenePath != "scenes/crate.scene.yaml" || ed.BuildErr != nil {
		t.Fatalf("opened %+v", ed)
	}
	if _, err := ed.PlaceScene("main", "scenes/crate.scene.yaml", 0, 0); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("placing the open scene in itself = %v, want a cycle", err)
	}
}

func TestEditorRunsTheProjectsOwnEditor(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		run   []string
	}{
		{"gameeditor tag", map[string]string{
			"main.go":   "//go:build !gameeditor\n\npackage main\n\nfunc main() {}\n",
			"editor.go": "//go:build gameeditor\n\npackage main\n\nfunc main() {}\n",
		}, []string{"run", "-tags", "gameeditor", "."}},
		{"cmd/editor", map[string]string{
			"main.go":            "package main\n\nfunc main() {}\n",
			"cmd/editor/main.go": "package main\n\nimport \"github.com/nsgo-dev/game/packages/game/editor/editorcmd\"\n\nfunc main() { editorcmd.Main(editorcmd.Options{}) }\n",
		}, []string{"run", "./cmd/editor"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := generateProject(t, tc.files)
			writeFiles(t, dir, map[string]string{"scenes/room.scene.yaml": "version: 2\nlayers: []\n"})
			lib := t.TempDir()
			a, _, stderr := newTestApp(t)
			var gotDir string
			var gotArgs []string
			a.goRun = func(d string, args []string, _, _ io.Writer) error {
				gotDir, gotArgs = d, args
				return nil
			}
			scene := filepath.Join(dir, "scenes", "room.scene.yaml")
			if code := run([]string{"editor", "-library", lib, scene, "hop.animation.yaml", "hero"}, a); code != 0 {
				t.Fatalf("exit %d (stderr: %s)", code, stderr)
			}
			clip, err := filepath.Abs("hop.animation.yaml")
			if err != nil {
				t.Fatal(err)
			}
			want := append(slices.Clone(tc.run), "-library", lib, scene, clip, "hero")
			if gotDir != dir || !slices.Equal(gotArgs, want) {
				t.Fatalf("ran go %q in %s, want go %q in %s", gotArgs, gotDir, want, dir)
			}

			a, _, stderr = newTestApp(t)
			a.goRun = func(string, []string, io.Writer, io.Writer) error { return errors.New("exit status 1") }
			if code := run([]string{"editor", dir}, a); code != 1 || !strings.Contains(stderr.String(), "the project's editor (go "+strings.Join(tc.run, " ")) {
				t.Fatalf("a failing editor: exit %d: %s", code, stderr)
			}
		})
	}

	// A cmd/editor that does not use package editorcmd may not take game editor's arguments, so game editor opens the project itself and says why.
	other := generateProject(t, map[string]string{"cmd/editor/main.go": "package main\n\nfunc main() {}\n"})
	writeFiles(t, other, map[string]string{"scenes/room.scene.yaml": "version: 2\nname: Room\nlayers: []\n"})
	a, _, stderr := newTestApp(t)
	a.goRun = func(string, []string, io.Writer, io.Writer) error {
		t.Error("ran a cmd/editor without editorcmd")
		return nil
	}
	var shown *editor.Editor
	a.launch = func(opened *editor.Editor) { shown = opened }
	if code := run([]string{"editor", filepath.Join(other, "scenes", "room.scene.yaml")}, a); code != 0 || shown == nil || !strings.Contains(stderr.String(), "does not use package editorcmd") {
		t.Fatalf("exit %d (stderr: %s), opened %v", code, stderr, shown != nil)
	}

	// A project without one opens in game editor itself.
	dir := generateProject(t, nil)
	writeFiles(t, dir, map[string]string{"scenes/room.scene.yaml": "version: 2\nname: Room\nlayers: []\n"})
	a, _, stderr = newTestApp(t)
	var ed *editor.Editor
	a.launch = func(opened *editor.Editor) { ed = opened }
	if code := run([]string{"editor", filepath.Join(dir, "scenes", "room.scene.yaml")}, a); code != 0 || ed == nil || ed.Doc.Name != "Room" {
		t.Fatalf("exit %d (stderr: %s), opened %+v", code, stderr, ed)
	}
}

// #922 Acceptance: creating a project from the Start Screen with the starter template runs `game new`
// with output shown, opens its game.yaml, builds & passes `go tool nsgo test ./...`, and persists in
// Recent projects across an editor restart.
func TestEditorStartScreenCreateStarterBuildsAndPersistsRecent(t *testing.T) {
	recentFile := filepath.Join(t.TempDir(), "recent-projects.yaml")
	parentDir := t.TempDir()

	// Stub `go get` and `go mod tidy` inside `game new` so creation runs offline while streaming output.
	var goCalls [][]string
	savedRunGo := runGo
	runGo = func(dir string, stdout, _ io.Writer, args ...string) error {
		goCalls = append(goCalls, append([]string{dir}, args...))
		fmt.Fprintf(stdout, "ran go %s\n", strings.Join(args, " "))
		return nil
	}
	t.Cleanup(func() { runGo = savedRunGo })

	a, _, stderr := newTestApp(t)
	a.recentProjectsPath = recentFile
	var ed *editor.Editor
	a.launch = func(e *editor.Editor) { ed = e }
	if code := run([]string{"editor"}, a); code != 0 {
		t.Fatalf("game editor exit %d (stderr: %s)", code, stderr)
	}
	if ed == nil || !ed.StartScreenOpen() {
		t.Fatalf("expected Start Screen open, got %+v", ed)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))

	if !app.Exists("id:editor-start-screen") || !app.Exists("id:start-template-starter") {
		t.Fatal("Start Screen or starter template card not rendered")
	}

	app.Post(func() {
		ed.SelectProjectTemplate("starter")
		ed.SetStartProjectParentDir(parentDir)
		ed.SetStartProjectName("star-quest")
		ed.SetStartProjectModule("example.com/star-quest")
	})
	app.Settle()

	app.Press("id:start-create")
	waitDone := make(chan struct{})
	go func() {
		ed.WaitForCreateProject()
		close(waitDone)
	}()
	waitDeadline := time.After(30 * time.Second)
	for {
		app.Settle()
		select {
		case <-waitDone:
			app.Settle()
			goto createDone
		case <-time.After(5 * time.Millisecond):
		case <-waitDeadline:
			t.Fatal("timed out waiting for StartCreateProject to finish")
		}
	}
createDone:

	createdDir := filepath.Join(parentDir, "star-quest")
	wantGameYAML := filepath.Join(createdDir, project.FileName)
	if ed.StartScreenOpen() {
		t.Fatalf("Start Screen remained open after creating starter (err=%v, status=%q)", ed.CreateProjectErr(), ed.Status)
	}
	if ed.Project == nil || ed.ProjectPath != wantGameYAML || ed.Project.Name != "star-quest" {
		t.Fatalf("opened project = %+v at %q, want star-quest at %s", ed.Project, ed.ProjectPath, wantGameYAML)
	}
	if len(goCalls) != 2 || !slices.Equal(goCalls[0][1:3], []string{"get", "-tool"}) || !slices.Equal(goCalls[1][1:], []string{"mod", "tidy"}) {
		t.Fatalf("goCalls = %q, want [go get -tool ..., go mod tidy]", goCalls)
	}
	outLog := strings.Join(ed.CreateProjectOutput(), "\n")
	if !strings.Contains(outLog, "ran go get -tool") || !strings.Contains(outLog, "ran go mod tidy") {
		t.Fatalf("CreateProjectOutput missing streamed go output:\n%s", outLog)
	}

	// Acceptance check 1: the created starter project builds and passes `go tool nsgo test ./...`.
	if !testing.Short() {
		gameDir, err := filepath.Abs("../..")
		if err != nil {
			t.Fatal(err)
		}
		env := append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOFLAGS="+strings.TrimSpace(os.Getenv("GOFLAGS")+" -mod=mod"))
		goCmd := func(args ...string) {
			t.Helper()
			cmd := exec.Command("go", args...)
			cmd.Dir, cmd.Env = createdDir, env
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("go %s in created starter: %v\n%s", strings.Join(args, " "), err, out)
			}
		}
		goCmd("mod", "edit", "-replace="+gameModule+"="+gameDir, "-tool="+gameTool)
		goCmd("tool", "nsgo", "test", "./...")
	}

	// Acceptance check 2: after restarting `game editor`, the opened project appears in Recent projects.
	a2, _, stderr2 := newTestApp(t)
	a2.recentProjectsPath = recentFile
	var ed2 *editor.Editor
	a2.launch = func(e *editor.Editor) { ed2 = e }
	if code := run([]string{"editor"}, a2); code != 0 {
		t.Fatalf("second game editor exit %d (stderr: %s)", code, stderr2)
	}
	if ed2 == nil || !ed2.StartScreenOpen() {
		t.Fatalf("second launch did not open Start Screen: %+v", ed2)
	}
	recent := ed2.RecentProjects()
	if len(recent) != 1 || recent[0].Name != "star-quest" || recent[0].Path != createdDir || recent[0].Missing {
		t.Fatalf("restarted RecentProjects = %+v, want [star-quest at %s (present)]", recent, createdDir)
	}
	app2 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed2)
	}, nsgotest.Size(1280, 800))
	if !app2.Exists("id:start-recent-section") || !app2.Exists("id:start-recent-0") {
		t.Fatal("restarted Start Screen did not render recent project row")
	}
}
