package gltf_test

import (
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"testing"

	"github.com/nsgo-dev/game/packages/game/gltf"
)

func FuzzLoad(f *testing.F) {
	// Valid JSON glTF minimal seed.
	f.Add([]byte(`{"asset":{"version":"2.0"}}`))

	// Valid Khronos Box GLB seed.
	boxJSON, boxBin, _, _, _ := buildKhronosBox()
	boxJSONBytes, _ := json.Marshal(boxJSON)
	f.Add(buildGLB(boxJSONBytes, boxBin))

	// Self-contained Draco and meshopt documents.
	f.Add(inlineBuffers(f, "Box/glTF-Draco", "Box.gltf"))
	f.Add(inlineBuffers(f, "MeshoptCubeTest/glTF-Meshopt", "MeshoptCubeTest.gltf"))

	// Corrupt / truncated GLB & JSON seeds.
	f.Add([]byte{})
	f.Add([]byte("glTF"))
	f.Add([]byte("glTF\x02\x00\x00\x00\x0c\x00\x00\x00"))
	f.Add([]byte("glTF\x02\x00\x00\x00\xff\xff\xff\xff"))
	f.Add([]byte("glTF\x01\x00\x00\x00\x14\x00\x00\x00\x00\x00\x00\x00JSON"))
	f.Add([]byte(`{"asset":{"version":"2.0"},"accessors":[{"count":999999999,"componentType":5126,"type":"VEC3"}]}`))
	f.Add([]byte(`{"asset":{"version":"2.0"},"nodes":[{"children":[0]}]}`))
	f.Add([]byte(`{"asset":{"version":"2.0"},"buffers":[{"uri":"data:;base64,!!!","byteLength":4}]}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := gltf.Load(data, nil)
		if err != nil || doc == nil {
			return
		}
		// Exercise post-load operations to verify they never panic on valid parsed docs.
		_ = doc.ComputeWorldMatrices()
		for i := range doc.Meshes {
			for _, prim := range doc.Meshes[i].Primitives {
				_ = doc.MaterialForPrimitive(prim, 0)
				_ = doc.MaterialForPrimitive(prim, -1)
				if len(prim.Targets) > 0 && len(prim.Positions) <= 4096 {
					weights := make([]float32, len(prim.Targets))
					weights[0] = 0.5
					_ = gltf.EvaluateMorphPositions(prim, weights, nil)
				}
			}
		}
		for i := range doc.Materials {
			if ref := doc.Materials[i].BaseColorTexture; ref != nil && ref.Transform != nil {
				_, _ = ref.Transform.ApplyUV(0.25, 0.75)
			}
		}
		for i := range doc.Animations {
			_ = doc.Animations[i].Sample(0)
			_ = doc.Animations[i].Sample(doc.Animations[i].Duration * 0.5)
		}
	})
}

// inlineBuffers returns a Khronos fixture's glTF JSON with every external buffer embedded as a base64 data URI, so Load needs no resolver.
func inlineBuffers(t testing.TB, dir, name string) []byte {
	t.Helper()
	fsys, _ := khronosFS(t, dir, name, nil)
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, b := range doc["buffers"].([]any) {
		buf := b.(map[string]any)
		uri, ok := buf["uri"].(string)
		if !ok {
			continue
		}
		bin, err := fs.ReadFile(fsys, uri)
		if err != nil {
			t.Fatal(err)
		}
		buf["uri"] = "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(bin)
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
