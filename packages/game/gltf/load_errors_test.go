package gltf_test

import (
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/internal/loaderrtest"
)

func TestLoadErrorsNameFileLineAndFix(t *testing.T) {
	cases := []struct {
		name, json, field string
	}{
		{"version", `{"asset": {"version": "1.0"}}`, "asset.version"},
		{"buffer view", `{"asset": {"version": "2.0"}, "buffers": [{"byteLength": 4, "uri": "data:application/octet-stream;base64,AAAAAA=="}], "bufferViews": [{"buffer": 0, "byteLength": 4}, {"buffer": 3, "byteLength": 4}]}`, "bufferViews[1]"},
		{"node mesh", `{"asset": {"version": "2.0"}, "nodes": [{"name": "a"}, {"name": "b", "mesh": 9}]}`, "nodes[1]"},
		{"required extension", `{"asset": {"version": "2.0"}, "extensionsUsed": ["KHR_texture_basisu"], "extensionsRequired": ["KHR_texture_basisu"]}`, "extensionsRequired[0]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := loaderrtest.Indent(tc.json)
			_, err := gltf.LoadFS(fstest.MapFS{"models/hero.gltf": {Data: data}}, "models/hero.gltf")
			loaderrtest.ExpectField(t, err, "models/hero.gltf", data, tc.field)
		})
	}
	t.Run("syntax", func(t *testing.T) {
		_, err := gltf.LoadFS(fstest.MapFS{"hero.gltf": {Data: []byte("{\n  \"asset\": {\n    \"version\": \"2.0\"\n  },\n  \"nodes\": [}\n}")}}, "hero.gltf")
		loaderrtest.Expect(t, err, "hero.gltf", 5)
	})
	t.Run("missing", func(t *testing.T) {
		_, err := gltf.LoadFS(fstest.MapFS{}, "none.gltf")
		loaderrtest.Expect(t, err, "none.gltf", 0)
	})
}
