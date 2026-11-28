package main

import (
	"bytes"
	_ "embed"
	"testing"

	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// townSceneYAML is the town's entry scene, the file the editor opens as the project's main scene.
//
//go:embed scenes/town.scene.yaml
var townSceneYAML []byte

// TestTownSceneIsCanonical checks that game.yaml autoloads scenes/town.scene.yaml as the main scene and that the file is in the form sceneformat.Encode writes, so the editor's load and save changes nothing.
func TestTownSceneIsCanonical(t *testing.T) {
	if a := Project.Autoload; a == nil || a.MainScene != "town" || len(a.Scenes) != 1 || a.Scenes[0].Name != "town" || a.Scenes[0].Path != "scenes/town.scene.yaml" {
		t.Fatalf("game.yaml autoload = %+v, want main scene town at scenes/town.scene.yaml", a)
	}
	doc, err := sceneformat.ParseWith(townSceneYAML, sceneformat.ParseOptions{File: "scenes/town.scene.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, townSceneYAML) {
		t.Fatalf("scenes/town.scene.yaml is not canonical; sceneformat.Encode writes:\n%s", out)
	}
}

// TestTownSceneMatchesTheWorld checks that the scene has one object for each person, item and exit of the town in data/world.yaml, on the same tile, so the scene the editor shows does not drift from the map the game runs.
func TestTownSceneMatchesTheWorld(t *testing.T) {
	c, err := loadContent()
	if err != nil {
		t.Fatal(err)
	}
	town := c.world.Maps[0]
	doc, err := sceneformat.Parse(townSceneYAML)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Name != town.Name {
		t.Errorf("scene name = %q, want the town's name %q", doc.Name, town.Name)
	}
	ts := float32(c.world.TileSize)
	want := map[string][2]float32{}
	at := func(cell Cell) [2]float32 { return [2]float32{float32(cell.X()) * ts, float32(cell.Y()) * ts} }
	for _, p := range town.People {
		want[p.ID] = at(p.At)
	}
	for _, it := range town.Items {
		want[it.ID] = at(it.At)
	}
	for _, e := range town.Exits {
		want["exit-"+e.To.Map] = at(e.At)
	}
	seen := map[string]bool{}
	for _, l := range doc.Layers {
		for _, o := range l.Objects {
			pos, ok := want[o.ID]
			if !ok {
				t.Errorf("scene object %q has no counterpart in the town of data/world.yaml", o.ID)
				continue
			}
			seen[o.ID] = true
			if o.X != pos[0] || o.Y != pos[1] {
				t.Errorf("scene object %q is at (%g, %g), but data/world.yaml puts it at (%g, %g)", o.ID, o.X, o.Y, pos[0], pos[1])
			}
		}
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("scene has no object %q for the town in data/world.yaml", id)
		}
	}
}
