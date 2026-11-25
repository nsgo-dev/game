package editor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

func TestMoveFileRewritesModels(t *testing.T) {
	dir := writeEditorProject(t)
	doc := testSceneDoc()
	doc.Layers[1].Objects[0].Model = "models/crate.glb"
	doc.Layers[1].Objects[0].ModelNode = "Lid"
	model := "models/crate.glb"
	doc.Layers[1].Objects = append(doc.Layers[1].Objects, sceneformat.ObjectDoc{ID: "pen", Scene: "scenes/pen.scene.yaml", Overrides: map[string]sceneformat.ObjectOverride{"post": {Model: &model}}})
	scene, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"scenes/room.yaml":        string(scene),
		"scenes/pen.scene.yaml":   "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: post\n        model: models/crate.glb\n",
		"models/crate.glb":        "glTF",
		"scenes/other.scene.yaml": "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: box\n        mesh: box\n",
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
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.ScenePath = "scenes/room.yaml"

	if err := ed.MoveFile("models/crate.glb", "props/crate.glb"); err != nil {
		t.Fatal(err)
	}
	// The open scene's objects and overrides name the model where it went, as the files on disk do.
	hero, _, _ := ed.FindObject("hero")
	pen, _, _ := ed.FindObject("pen")
	if hero == nil || hero.Model != "props/crate.glb" || hero.ModelNode != "Lid" || pen == nil || pen.Overrides["post"].Model == nil || *pen.Overrides["post"].Model != "props/crate.glb" {
		t.Fatalf("hero %+v, pen %+v", hero, pen)
	}
	for _, name := range []string{"scenes/room.yaml", "scenes/pen.scene.yaml"} {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "models/crate.glb") || !strings.Contains(string(data), "model: props/crate.glb") {
			t.Fatalf("%s:\n%s", name, data)
		}
	}
}
