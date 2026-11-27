package assetbuild

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

// FuzzPackGLB checks that packing any model never panics, and that a GLB it packs loads on its own and packs again to the same bytes.
func FuzzPackGLB(f *testing.F) {
	buf := base64.StdEncoding.EncodeToString(make([]byte, 36))
	f.Add([]byte(`{"asset":{"version":"2.0"},"scenes":[{"nodes":[0]}],"nodes":[{"mesh":0}],"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}],"buffers":[{"uri":"data:application/octet-stream;base64,` + buf + `","byteLength":36}],"bufferViews":[{"buffer":0,"byteLength":36}],"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3","min":[0,0,0],"max":[0,0,0]}]}`))
	f.Add([]byte(`{"asset":{"version":"2.0"},"images":[{"uri":"data:image/png;base64,iVBORw0KGgo="}]}`))
	f.Add([]byte(`{"asset":{"version":"2.0"},"buffers":[{"byteLength":4}],"bufferViews":[{"buffer":0,"byteLength":4,"extensions":{"EXT_meshopt_compression":{"buffer":0,"byteLength":4,"count":1,"byteStride":4,"mode":"ATTRIBUTES"}}}]}`))
	for _, name := range []string{"crate.glb", "gem.glb"} {
		if data, err := os.ReadFile(filepath.Join("..", "..", "placeholder", "pack", "models", name)); err == nil {
			f.Add(data)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := packGLB(data, nil, modelSettings{Scale: 1, Animations: true})
		if err != nil {
			return
		}
		again, err := packGLB(out, nil, modelSettings{Scale: 1, Animations: true})
		if err != nil {
			t.Fatalf("packing a packed GLB: %v", err)
		}
		if !bytes.Equal(again, out) {
			t.Fatal("packing a packed GLB changed it")
		}
	})
}
