package world3d_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/world3d"
)

func TestLoadMeshLibraryErrorsNameFileLineAndFix(t *testing.T) {
	cases := []struct {
		name, json, field string
	}{
		{"version", `{"asset": {"version": "1.0"}}`, "asset.version"},
		{"node mesh", `{"asset": {"version": "2.0"}, "meshes": [{"name": "floor"}], "nodes": [{"mesh": 0}, {"name": "wall", "mesh": 4}]}`, "nodes[1].mesh"},
		{"accessor", `{"asset": {"version": "2.0"}, "meshes": [{"name": "floor", "primitives": [{"attributes": {"POSITION": 2}}]}]}`, "meshes[0].primitives[0].attributes.POSITION"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := loaderrtest.Indent(tc.json)
			_, err := world3d.LoadMeshLibraryFromGLTF(data)
			loaderrtest.ExpectField(t, loaderr.WithFile(err, "tiles.gltf"), "tiles.gltf", data, tc.field)
		})
	}
	t.Run("syntax", func(t *testing.T) {
		_, err := world3d.LoadMeshLibraryFromGLTF([]byte("{\n  \"asset\": {\n    \"version\": 2.0\n  }\n}"))
		loaderrtest.Expect(t, loaderr.WithFile(err, "tiles.gltf"), "tiles.gltf", 3)
	})
}
