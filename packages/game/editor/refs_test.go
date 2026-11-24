package editor_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/refs"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
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
	if ed.Undo() {
		t.Fatal("a step from before the move could be undone")
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
}
