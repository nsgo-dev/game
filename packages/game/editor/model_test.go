package editor_test

import (
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

func TestPlaceObjectChecksMeshAndModel(t *testing.T) {
	cases := []struct {
		name string
		obj  sceneformat.ObjectDoc
		want string
	}{
		{"unknown primitive", sceneformat.ObjectDoc{ID: "a", Mesh: "cube"}, `mesh "cube" is not a built-in primitive`},
		{"mesh and model", sceneformat.ObjectDoc{ID: "a", Mesh: "box", Model: "models/crate.glb"}, "names both mesh"},
		{"model not glTF", sceneformat.ObjectDoc{ID: "a", Model: "models/crate.obj"}, "must be a glTF file"},
		{"node without model", sceneformat.ObjectDoc{ID: "a", ModelNode: "Lid"}, "requires model"},
		{"child", sceneformat.ObjectDoc{ID: "a", Children: []sceneformat.ObjectDoc{{ID: "b", Mesh: "cube"}}}, `object "b" mesh "cube"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ed := newTestEditor(t)
			if _, err := ed.PlaceObject("", tc.obj); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("PlaceObject = %v, want an error with %q", err, tc.want)
			}
			if _, err := ed.SaveYAML(); err != nil {
				t.Fatalf("SaveYAML after a refused placement: %v", err)
			}
		})
	}
}

func TestPlaceModelObject(t *testing.T) {
	ed := newTestEditor(t)
	// A prefab's primitive is not given to an object that draws a model.
	ed.Prefabs["crate"] = editor.Prefab{Name: "Crate", Mesh: "box"}
	id, err := ed.PlaceObject("", sceneformat.ObjectDoc{Prefab: "crate", Model: "models/crate.glb"})
	if err != nil {
		t.Fatalf("PlaceObject: %v", err)
	}
	if o, _, _ := ed.FindObject(id); o == nil || o.Mesh != "" || o.Model != "models/crate.glb" {
		t.Fatalf("placed object %+v, want the model and no mesh", o)
	}
	if _, err := ed.SaveYAML(); err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}

	// Placing a model in a version 1 scene raises it to version 2, and Undo restores version 1.
	ed = newTestEditor(t)
	ed.Doc.Version = 1
	if _, err := ed.PlaceObject("", sceneformat.ObjectDoc{ID: "crate", Model: "models/crate.glb"}); err != nil {
		t.Fatalf("PlaceObject: %v", err)
	}
	if ed.Doc.Version != sceneformat.CurrentVersion {
		t.Fatalf("Version after placing a model = %d, want %d", ed.Doc.Version, sceneformat.CurrentVersion)
	}
	if _, err := ed.SaveYAML(); err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	ed.Undo()
	if ed.Doc.Version != 1 {
		t.Fatalf("Version after Undo = %d, want 1", ed.Doc.Version)
	}
}
