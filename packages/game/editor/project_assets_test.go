package editor_test

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// assetsProject is a project with its own atlas, a main scene that draws it and another scene to instance.
var assetsProject = map[string]string{
	project.FileName:      "version: 1\nname: Assets\nid: dev.example.assets\nappVersion: 1.0.0\nwindow:\n  width: 320\n  height: 180\nautoload:\n  mainScene: main\n  scenes:\n    - name: main\n      path: scenes/main.scene.yaml\n",
	"sprites/actors.json": `{"frames": {"hero": {"frame": {"x": 0, "y": 0, "w": 16, "h": 16}}, "crate": {"frame": {"x": 16, "y": 0, "w": 16, "h": 16}}}, "meta": {"image": "actors.png"}}`,
	"sprites/actors.png":  "the project's actors image",
	"scenes/main.scene.yaml": `version: 2
name: Main
virtualWidth: 320
virtualHeight: 180
layers:
  - name: actors
    objects:
      - id: hero
        x: 40
        y: 30
        sprite: {atlas: actors, frame: hero}
        components:
          health: {hp: 3}
`,
	"scenes/crate.scene.yaml": "version: 2\nname: Crate\nlayers:\n  - name: main\n    objects:\n      - id: box\n        sprite: {atlas: actors, frame: crate}\n",
}

// newAssetsEditor returns an editor with ProjectAssets set and no component registry, on the project in dir.
func newAssetsEditor(t *testing.T) *editor.Editor {
	t.Helper()
	ed, err := editor.New(nil, sceneformat.AtlasMap{})
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectAssets = true
	return ed
}

func TestProjectAssetsDrawTheProjectsAtlasesInTheEditor(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, assetsProject)
	ed := newAssetsEditor(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if _, ok := ed.Resolver.(*project.Resolver); !ok {
		t.Fatalf("Resolver = %T, want the project's", ed.Resolver)
	}
	if ed.ScenePath != "scenes/main.scene.yaml" || ed.BuildErr != nil {
		t.Fatalf("opened %q, build error %v", ed.ScenePath, ed.BuildErr)
	}
	var keys []string
	for _, a := range ed.Assets() {
		keys = append(keys, a.Key())
	}
	if want := []string{"actors/crate", "actors/hero"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("Assets() = %q, want %q", keys, want)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))
	app.Find("id:asset-actors-hero")
	app.Find("id:asset-actors-crate")
	drawn := func() int {
		n := 0
		for _, im := range gametest.ImageDraws(app.Find("id:editor-viewport").Ops) {
			if bytes.Equal(im.Image.Data, []byte("the project's actors image")) {
				n++
			}
		}
		return n
	}
	if n := drawn(); n != 1 {
		t.Fatalf("the viewport draws the project's image %d times, want once for the hero", n)
	}

	// Placing the other scene file draws its crate from the same atlas, and Undo takes it back.
	id, err := ed.PlaceScene("actors", "scenes/crate.scene.yaml", 120, 60)
	if err != nil {
		t.Fatalf("PlaceScene: %v", err)
	}
	app.Settle()
	if box := ed.Built.Object(id + "/box"); box == nil || box.Atlas == nil {
		t.Fatalf("the instance's box did not build with its atlas")
	}
	if n := drawn(); n != 2 {
		t.Fatalf("after placing the crate the viewport draws the project's image %d times, want 2", n)
	}
	app.Press("id:toolbar-undo")
	if obj, _, _ := ed.FindObject(id); obj != nil {
		t.Fatalf("Undo left the instance %q", id)
	}
	if n := drawn(); n != 1 {
		t.Fatalf("after Undo the viewport draws the project's image %d times, want 1", n)
	}

	// The open scene, opened from the project, cannot instance itself.
	if _, err := ed.PlaceScene("actors", "scenes/main.scene.yaml", 0, 0); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("PlaceScene of the open scene = %v, want a cycle", err)
	}
}

func TestOpenProjectSceneOpensAnotherSceneOfTheProject(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{"scenes/bad.scene.yaml": "version: 2\nname: [\n"}
	for k, v := range assetsProject {
		files[k] = v
	}
	writeFiles(t, dir, files)
	ed := newAssetsEditor(t)
	if err := ed.OpenProjectScene(dir, filepath.Join(dir, "scenes", "crate.scene.yaml")); err != nil {
		t.Fatalf("OpenProjectScene: %v", err)
	}
	if ed.Project == nil || ed.Doc.Name != "Crate" || ed.ScenePath != "scenes/crate.scene.yaml" || ed.BuildErr != nil {
		t.Fatalf("opened %q (%q), project %v, build error %v", ed.Doc.Name, ed.ScenePath, ed.Project != nil, ed.BuildErr)
	}
	if _, err := ed.PlaceScene("main", "scenes/crate.scene.yaml", 0, 0); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("PlaceScene of the open scene = %v, want a cycle", err)
	}
	if _, err := ed.PlaceScene("main", "scenes/main.scene.yaml", 0, 0); err != nil {
		t.Fatalf("PlaceScene of the main scene: %v", err)
	}

	// A scene that does not open leaves the editor, and its resolver, as they were.
	resolver := ed.Resolver
	if err := ed.OpenProjectScene(dir, filepath.Join(dir, "scenes", "bad.scene.yaml")); err == nil {
		t.Fatal("OpenProjectScene opened a scene that does not parse")
	}
	if ed.Resolver != resolver || ed.Doc.Name != "Crate" {
		t.Fatalf("a failed open changed the editor: resolver %v, scene %q", ed.Resolver == resolver, ed.Doc.Name)
	}
}

func TestProjectAssetsListTheProjectsAtlasesAgainAfterAChange(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, assetsProject)
	ed := newAssetsEditor(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if n := len(ed.Assets()); n != 2 {
		t.Fatalf("Assets() has %d frames, want 2", n)
	}
	writeFiles(t, dir, map[string]string{"ui/icons.json": `{"frames": {"coin": {"frame": {"x": 0, "y": 0, "w": 8, "h": 8}}}, "meta": {"image": "icons.png"}}`, "ui/icons.png": "icons"})
	// The editor lists the atlases again once it changes the project's files, as saving a scene does.
	if err := ed.SaveFile(ed.FilePath); err != nil {
		t.Fatal(err)
	}
	if got := ed.Assets(); len(got) != 3 || got[2].Key() != "icons/coin" {
		t.Fatalf("after a save Assets() = %+v, want the new atlas listed", got)
	}
}

func TestOpenProjectKeepsTheResolverWithoutProjectAssets(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, assetsProject)
	resolver := sceneformat.AtlasMap{}
	ed, err := editor.New(nil, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := ed.Resolver.(sceneformat.AtlasMap); !ok || ed.BuildErr == nil {
		t.Fatalf("Resolver = %T, build error %v; want the editor's own resolver, which has no actors atlas", ed.Resolver, ed.BuildErr)
	}
}

func TestOpenProjectWithoutAMainSceneRebuildsTheOpenSceneAgainstIt(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, assetsProject)
	other := t.TempDir()
	writeFiles(t, other, map[string]string{project.FileName: "version: 1\nname: Other\nid: dev.example.other\nappVersion: 1.0.0\nwindow:\n  width: 320\n  height: 180\n"})
	ed := newAssetsEditor(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if ed.BuildErr != nil || ed.Built == nil {
		t.Fatalf("the main scene did not build: %v", ed.BuildErr)
	}
	// The other project has no main scene and no actors atlas: the scene left open builds against it at once, and fails to find the atlas.
	if err := ed.OpenProject(other); err != nil {
		t.Fatal(err)
	}
	if ed.Doc == nil || ed.Doc.Name != "Main" || ed.ScenePath != "" {
		t.Fatalf("open scene %+v at %q, want Main left open outside the project", ed.Doc, ed.ScenePath)
	}
	if ed.BuildErr == nil || !strings.Contains(ed.BuildErr.Error(), "actors") {
		t.Fatalf("BuildErr = %v, want the actors atlas the other project lacks", ed.BuildErr)
	}
}
