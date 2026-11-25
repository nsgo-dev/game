package editor_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// sceneFiles resolves atlases and the scene files held in its map.
type sceneFiles struct {
	sceneformat.AtlasMap
	files map[string]string
}

func (s sceneFiles) Scene(path string) (*sceneformat.SceneDoc, error) {
	src, ok := s.files[path]
	if !ok {
		return nil, fmt.Errorf("no scene %q", path)
	}
	return sceneformat.Parse([]byte(src))
}

const slimeScene = `version: 2
layers:
  - name: root
    objects:
      - id: body
        sprite: {atlas: actors, frame: slime}
      - id: eye
        x: 4
`

func TestPlaceSceneInstancesAFileAndSavesTheReference(t *testing.T) {
	doc := testSceneDoc()
	doc.Version = 1
	resolver := sceneFiles{AtlasMap: testResolver(), files: map[string]string{
		"scenes/slime.scene.yaml": slimeScene,
		"scenes/loop.scene.yaml":  "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: me\n        scene: scenes/loop.scene.yaml\n",
	}}
	ed, err := editor.New(doc, resolver)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	before, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}

	id, err := ed.PlaceScene("actors", "scenes/slime.scene.yaml", 100, 90)
	if err != nil {
		t.Fatalf("PlaceScene: %v", err)
	}
	if !strings.HasPrefix(id, "slime_") {
		t.Fatalf("instance id = %q, want one named after the file", id)
	}
	if ed.Doc.Version != sceneformat.CurrentVersion {
		t.Fatalf("version after placing an instance = %d, want %d", ed.Doc.Version, sceneformat.CurrentVersion)
	}
	if ed.BuildErr != nil {
		t.Fatalf("Rebuild after PlaceScene: %v", ed.BuildErr)
	}
	body := ed.Built.Object(id + "/body")
	if body == nil || body.Parent == nil || body.Parent.ID != id || body.Sprite.X != 100 || body.Sprite.Y != 90 {
		t.Fatalf("instanced body = %+v, want it drawn under %q at (100, 90)", body, id)
	}
	if ed.Built.Object(id+"/eye") == nil {
		t.Fatal("instanced eye was not built")
	}
	if hit := ed.HitTest(100, 90); !strings.HasPrefix(hit, id+"/") {
		t.Fatalf("HitTest on the instance = %q, want an instanced child under %q/", hit, id)
	}

	saved, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	if !bytes.Contains(saved, []byte("scene: scenes/slime.scene.yaml")) || bytes.Contains(saved, []byte("frame: slime")) {
		t.Fatalf("saved scene should hold the reference and not the instanced objects:\n%s", saved)
	}
	reloaded, err := sceneformat.Parse(saved)
	if err != nil {
		t.Fatalf("Parse saved scene: %v", err)
	}
	if again, err := sceneformat.Encode(reloaded); err != nil || !bytes.Equal(again, saved) {
		t.Fatalf("saved scene does not round-trip (err %v)", err)
	}

	if !ed.Undo() {
		t.Fatal("Undo returned false")
	}
	if ed.Doc.Version != 1 {
		t.Fatalf("version after Undo = %d, want 1", ed.Doc.Version)
	}
	if after, err := ed.SaveYAML(); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("Undo did not restore the scene (err %v):\n%s", err, after)
	}

	if _, err := ed.PlaceScene("actors", "scenes/missing.scene.yaml", 0, 0); err == nil || !strings.Contains(err.Error(), "missing.scene.yaml") {
		t.Fatalf("PlaceScene of a missing file: %v", err)
	}
	if _, err := ed.PlaceScene("actors", "scenes/loop.scene.yaml", 0, 0); err == nil || !strings.Contains(err.Error(), "scene cycle") {
		t.Fatalf("PlaceScene of a cyclic file: %v", err)
	}
	if _, err := ed.PlaceScene("actors", "/abs.scene.yaml", 0, 0); err == nil || !strings.Contains(err.Error(), "slash-separated path") {
		t.Fatalf("PlaceScene of an absolute path: %v", err)
	}
	if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "stray", Overrides: map[string]sceneformat.ObjectOverride{"body": {X: sceneformat.Float32(1)}}}); err == nil || !strings.Contains(err.Error(), "overrides require a scene") {
		t.Fatalf("PlaceObject with overrides and no scene: %v", err)
	}

	plain := newTestEditor(t)
	if _, err := plain.PlaceScene("actors", "scenes/slime.scene.yaml", 0, 0); err == nil || !strings.Contains(err.Error(), "SceneResolver") {
		t.Fatalf("PlaceScene without a SceneResolver: %v", err)
	}
}

func TestPlaceSceneRefusesAnInstanceThatLeadsBackToTheOpenScene(t *testing.T) {
	onDisk, err := sceneformat.Encode(testSceneDoc())
	if err != nil {
		t.Fatal(err)
	}
	resolver := sceneFiles{AtlasMap: testResolver(), files: map[string]string{
		"scenes/level.scene.yaml":  string(onDisk),
		"scenes/holder.scene.yaml": "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: level\n        scene: scenes/level.scene.yaml\n",
		"scenes/slime.scene.yaml":  slimeScene,
	}}
	ed, err := editor.New(testSceneDoc(), resolver)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.ScenePath = "scenes/level.scene.yaml"

	for _, p := range []string{"scenes/level.scene.yaml", "scenes/holder.scene.yaml"} {
		if _, err := ed.PlaceScene("actors", p, 0, 0); err == nil || !strings.Contains(err.Error(), "scene cycle") {
			t.Fatalf("PlaceScene(%q) into the open scene: %v, want a scene cycle", p, err)
		}
	}
	if ed.History.Len() != 0 {
		t.Fatalf("refused placements left %d commands in history", ed.History.Len())
	}
	if _, err := ed.PlaceScene("actors", "scenes/slime.scene.yaml", 0, 0); err != nil {
		t.Fatalf("PlaceScene of an unrelated file: %v", err)
	}
	if ed.BuildErr != nil {
		t.Fatalf("Rebuild: %v", ed.BuildErr)
	}

	if err := ed.OpenYAML(onDisk); err != nil {
		t.Fatalf("OpenYAML: %v", err)
	}
	if ed.ScenePath != "" {
		t.Fatalf("OpenYAML kept ScenePath %q", ed.ScenePath)
	}
}

func TestPlaceObjectChecksInheritedIDsAndInstances(t *testing.T) {
	resolver := sceneFiles{AtlasMap: testResolver(), files: map[string]string{
		"scenes/base.scene.yaml":  "version: 2\nlayers:\n  - name: actors\n    objects:\n      - id: hero\n        sprite: {atlas: actors, frame: hero}\n      - id: hero_1\n        x: 8\n",
		"scenes/slime.scene.yaml": slimeScene,
	}}
	doc, err := sceneformat.Parse([]byte("version: 2\ninherits: scenes/base.scene.yaml\nlayers:\n  - name: actors\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ed, err := editor.New(doc, resolver)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if ed.BuildErr != nil {
		t.Fatalf("Rebuild: %v", ed.BuildErr)
	}

	if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "hero"}); err == nil || !strings.Contains(err.Error(), "duplicate object id") {
		t.Fatalf("PlaceObject reusing an inherited id: %v", err)
	}
	id, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{Name: "hero"})
	if err != nil {
		t.Fatalf("PlaceObject: %v", err)
	}
	if id == "hero" || id == "hero_1" {
		t.Fatalf("generated id %q is already inherited from the base", id)
	}
	if ed.BuildErr != nil {
		t.Fatalf("Rebuild after PlaceObject: %v", ed.BuildErr)
	}

	history := ed.History.Len()
	bad := []struct {
		name string
		obj  sceneformat.ObjectDoc
		want string
	}{
		{"missing file", sceneformat.ObjectDoc{ID: "e1", Scene: "scenes/missing.scene.yaml"}, "missing.scene.yaml"},
		{"bad override value", sceneformat.ObjectDoc{ID: "e2", Scene: "scenes/slime.scene.yaml", Overrides: map[string]sceneformat.ObjectOverride{"body": {Alpha: sceneformat.Float32(2)}}}, "alpha"},
		{"unknown override target", sceneformat.ObjectDoc{ID: "e3", Scene: "scenes/slime.scene.yaml", Overrides: map[string]sceneformat.ObjectOverride{"nose": {X: sceneformat.Float32(1)}}}, "nose"},
		{"nested instance", sceneformat.ObjectDoc{ID: "e4", Children: []sceneformat.ObjectDoc{{ID: "e5", Scene: "scenes/missing.scene.yaml"}}}, "missing.scene.yaml"},
	}
	for _, tc := range bad {
		if _, err := ed.PlaceObject("actors", tc.obj); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("PlaceObject with a %s: %v, want an error naming %q", tc.name, err, tc.want)
		}
	}
	if ed.History.Len() != history {
		t.Fatalf("refused placements changed history from %d to %d commands", history, ed.History.Len())
	}

	inst, err := ed.PlaceScene("actors", "scenes/slime.scene.yaml", 0, 0)
	if err != nil {
		t.Fatalf("PlaceScene: %v", err)
	}
	if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: inst + "/body"}); err == nil {
		t.Fatalf("PlaceObject reusing the instanced id %q was accepted", inst+"/body")
	}
}

func TestPlayModeSharesTheResolvedSceneWithDoc(t *testing.T) {
	resolver := sceneFiles{AtlasMap: testResolver(), files: map[string]string{"scenes/slime.scene.yaml": slimeScene}}
	ed, err := editor.New(testSceneDoc(), resolver)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetActiveLayer("actors")
	id, err := ed.PlaceScene("actors", "scenes/slime.scene.yaml", 10, 10)
	if err != nil {
		t.Fatalf("PlaceScene: %v", err)
	}
	prePlay, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	ed.OnPlayUpdate = func(scene *sceneformat.BuiltScene, _ time.Duration) {
		if body := scene.Object(id + "/body"); body != nil {
			body.Doc.X++
		}
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))
	app.Press("id:toolbar-play")
	if !ed.Playing() {
		t.Fatal("expected editor to be playing after pressing Play")
	}
	app.Advance(time.Second)

	obj, _, _ := ed.FindObject(id)
	if obj == nil || len(obj.Children) == 0 || obj.Children[0].ID != id+"/body" || obj.Children[0].X != float32(ed.PlaySteps()) {
		t.Fatalf("instance in Doc during play = %+v, want the resolved body moved %d times", obj, ed.PlaySteps())
	}

	app.Press("id:toolbar-play")
	if ed.Playing() {
		t.Fatal("expected editor to stop after pressing Stop")
	}
	if after, err := ed.SaveYAML(); err != nil || !bytes.Equal(after, prePlay) {
		t.Fatalf("Stop did not restore the scene with its reference (err %v):\n%s", err, after)
	}
}
