package editor_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
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
	if hit := ed.HitTest(100, 90); hit != id {
		t.Fatalf("HitTest on the instance = %q, want its root %q", hit, id)
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
