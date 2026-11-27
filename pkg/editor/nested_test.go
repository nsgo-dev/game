package editor_test

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/sceneformat"
)

func TestNestedObjectsKeepUniqueIDsAndSurviveUndo(t *testing.T) {
	ed := newTestEditor(t)
	before, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}

	ship := sceneformat.ObjectDoc{
		ID: "ship",
		X:  100,
		Children: []sceneformat.ObjectDoc{
			{ID: "turret", X: 10, Children: []sceneformat.ObjectDoc{{ID: "barrel", X: 5}}},
		},
	}
	if _, err := ed.PlaceObject("actors", ship); err != nil {
		t.Fatalf("PlaceObject with children: %v", err)
	}
	// The placed tree is a copy: changing the caller's value must not reach the scene.
	ship.Children[0].X = 999

	// An ID taken by a nested child is not free, whether given or nested inside a new object.
	if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "barrel"}); err == nil || !strings.Contains(err.Error(), `duplicate object id "barrel"`) {
		t.Fatalf("placing a duplicate of a nested ID: err = %v", err)
	}
	if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "other", Children: []sceneformat.ObjectDoc{{ID: "turret"}}}); err == nil || !strings.Contains(err.Error(), `duplicate object id "turret"`) {
		t.Fatalf("placing a child that duplicates a nested ID: err = %v", err)
	}
	if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "twins", Children: []sceneformat.ObjectDoc{{ID: "twin"}, {ID: "twin"}}}); err == nil || !strings.Contains(err.Error(), `duplicate object id "twin"`) {
		t.Fatalf("placing a tree with a repeated ID: err = %v", err)
	}
	if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "orphan", Children: []sceneformat.ObjectDoc{{Name: "no id"}}}); err == nil || !strings.Contains(err.Error(), "id must not be empty") {
		t.Fatalf("placing a child without an ID: err = %v", err)
	}

	withShip, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML with ship: %v", err)
	}
	reloaded, err := sceneformat.Parse(withShip)
	if err != nil {
		t.Fatalf("Parse saved scene: %v", err)
	}
	built, err := sceneformat.Build(reloaded, testResolver())
	if err != nil {
		t.Fatalf("Build saved scene: %v", err)
	}
	barrel := built.Object("barrel")
	if barrel == nil || barrel.Parent == nil || barrel.Parent.ID != "turret" || barrel.Parent.Parent.ID != "ship" {
		t.Fatalf("barrel did not load under turret under ship")
	}
	if barrel.World.Tx != 115 {
		t.Fatalf("barrel world x = %v, want 115 (100 + 10 + 5)", barrel.World.Tx)
	}

	if err := ed.DeleteObject("ship"); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	if !ed.Undo() {
		t.Fatal("Undo delete failed")
	}
	restored, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML after undo: %v", err)
	}
	if !bytes.Equal(restored, withShip) {
		t.Fatalf("undoing the delete did not restore the tree:\n%s\nwant:\n%s", restored, withShip)
	}
	if !ed.Undo() {
		t.Fatal("Undo place failed")
	}
	if after, _ := ed.SaveYAML(); !bytes.Equal(after, before) {
		t.Fatalf("undoing the place did not restore the scene")
	}

	// Generated IDs skip ones that nested children hold.
	ed.Doc.Layers[1].Objects = append(ed.Doc.Layers[1].Objects, sceneformat.ObjectDoc{ID: "holder", Children: []sceneformat.ObjectDoc{{ID: "crate_1"}, {ID: "crate_2"}}})
	id, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{Name: "crate"})
	if err != nil {
		t.Fatalf("PlaceObject generated ID: %v", err)
	}
	if id == "crate_1" || id == "crate_2" {
		t.Fatalf("generated ID %q collides with a nested child", id)
	}
	if _, err := ed.SaveYAML(); err != nil {
		t.Fatalf("SaveYAML after generated ID: %v", err)
	}
}

func TestNestedTreeShapeIsCheckedBeforeItReachesHistory(t *testing.T) {
	ed := newTestEditor(t)

	// A tree deeper than MaxObjectDepth would leave a scene that neither builds nor saves, so it never reaches the undo history.
	deep := sceneformat.ObjectDoc{ID: "n0"}
	leaf := &deep
	for i := 1; i <= sceneformat.MaxObjectDepth; i++ {
		leaf.Children = []sceneformat.ObjectDoc{{ID: "n" + strconv.Itoa(i)}}
		leaf = &leaf.Children[0]
	}
	if _, err := ed.PlaceObject("actors", deep); err == nil || !strings.Contains(err.Error(), "deeper than") {
		t.Fatalf("placing a tree past MaxObjectDepth: err = %v", err)
	}
	if ed.History.CanUndo() {
		t.Fatal("a refused tree was pushed onto the undo history")
	}

	// A sheared sprite cannot be drawn, whether placed that way or made so by a later rotate or scale of its parent.
	if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{
		ID:       "stretched",
		ScaleX:   sceneformat.Float32(2),
		Children: []sceneformat.ObjectDoc{{ID: "tilted", Rotation: 0.5, Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"}}},
	}); err == nil || !strings.Contains(err.Error(), "sheared") {
		t.Fatalf("placing a sheared sprite: err = %v", err)
	}
	if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{
		ID:       "parent",
		Children: []sceneformat.ObjectDoc{{ID: "turned", Rotation: 0.5, Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"}}},
	}); err != nil {
		t.Fatalf("PlaceObject: %v", err)
	}
	saved, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	if err := ed.ScaleObject("parent", 2, 1); err == nil || !strings.Contains(err.Error(), "sheared") {
		t.Fatalf("ScaleObject shearing a child sprite: err = %v", err)
	}
	if err := ed.ScaleObject3D("parent", 1, 3, 1); err == nil || !strings.Contains(err.Error(), "sheared") {
		t.Fatalf("ScaleObject3D shearing a child sprite: err = %v", err)
	}
	if after, _ := ed.SaveYAML(); !bytes.Equal(after, saved) || ed.BuildErr != nil {
		t.Fatalf("a refused scale changed the scene (build error %v)", ed.BuildErr)
	}

	// Uniform scales, and a scale that collapses an axis to zero, keep every sprite drawable.
	if err := ed.ScaleObject("parent", 2, 2); err != nil {
		t.Fatalf("ScaleObject uniform: %v", err)
	}
	if err := ed.ScaleObject("parent", 0, 1); err != nil {
		t.Fatalf("ScaleObject collapsing x: %v", err)
	}
	if ed.BuildErr != nil {
		t.Fatalf("Rebuild after an allowed scale: %v", ed.BuildErr)
	}
}
