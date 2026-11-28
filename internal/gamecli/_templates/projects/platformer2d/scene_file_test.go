package main

import (
	"bytes"
	_ "embed"
	"testing"

	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// levelSceneYAML is the level's entry scene, the file the editor opens as the project's main scene.
//
//go:embed scenes/level1.scene.yaml
var levelSceneYAML []byte

// TestLevelSceneIsCanonical checks that game.yaml autoloads scenes/level1.scene.yaml as the main scene and that the file is in the form sceneformat.Encode writes, so the editor's load and save changes nothing.
func TestLevelSceneIsCanonical(t *testing.T) {
	if a := Project.Autoload; a == nil || a.MainScene != "level1" || len(a.Scenes) != 1 || a.Scenes[0].Name != "level1" || a.Scenes[0].Path != "scenes/level1.scene.yaml" {
		t.Fatalf("game.yaml autoload = %+v, want main scene level1 at scenes/level1.scene.yaml", a)
	}
	doc, err := sceneformat.ParseWith(levelSceneYAML, sceneformat.ParseOptions{File: "scenes/level1.scene.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, levelSceneYAML) {
		t.Fatalf("scenes/level1.scene.yaml is not canonical; sceneformat.Encode writes:\n%s", out)
	}
}

// TestLevelSceneMatchesTheLevel checks that the scene places the player start, the lamp and the beacon on the cells data/level.yaml gives them, so the scene the editor shows does not drift from the grid the game runs.
func TestLevelSceneMatchesTheLevel(t *testing.T) {
	d := mustLoadData()
	doc, err := sceneformat.Parse(levelSceneYAML)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Name != d.level.Name {
		t.Errorf("scene name = %q, want the level's name %q", doc.Name, d.level.Name)
	}
	want := map[string]rune{"player_spawn": glyphStart, "checkpoint_lamp": glyphCheckpoint, "beacon": glyphGoal}
	cells := map[rune][2]float32{}
	ts := float32(d.level.TileSize)
	for r, row := range d.level.rows() {
		for c, g := range row {
			if g == glyphStart || g == glyphCheckpoint || g == glyphGoal {
				cells[g] = [2]float32{float32(c) * ts, float32(r) * ts}
			}
		}
	}
	seen := map[string]bool{}
	for _, l := range doc.Layers {
		for _, o := range l.Objects {
			g, ok := want[o.ID]
			if !ok {
				t.Errorf("scene object %q has no counterpart in data/level.yaml", o.ID)
				continue
			}
			seen[o.ID] = true
			if at := cells[g]; o.X != at[0] || o.Y != at[1] {
				t.Errorf("scene object %q is at (%g, %g), but data/level.yaml puts %q at (%g, %g)", o.ID, o.X, o.Y, g, at[0], at[1])
			}
		}
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("scene has no object %q", id)
		}
	}
}
