package gltf_test

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/nsgo-dev/game/packages/game/gltf"
)

// floats packs little-endian float32 values as a glTF buffer stores them.
func floats(vs ...float32) []byte {
	b := make([]byte, 0, 4*len(vs))
	for _, v := range vs {
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(v))
	}
	return b
}

// sceneJSON is a small glTF document: a triangle mesh on a child node, and an animation that slides the parent node along +X over one second.
func sceneJSON() []byte {
	var bin []byte
	bin = append(bin, floats(0, 0, 0, 1, 0, 0, 0, 1, 0)...) // 3 positions, offset 0
	bin = append(bin, floats(0, 1)...)                      // 2 key times, offset 36
	bin = append(bin, floats(0, 0, 0, 4, 0, 0)...)          // 2 translations, offset 44
	uri := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(bin)
	return fmt.Appendf(nil, `{
	"asset": {"version": "2.0"},
	"scene": 0,
	"scenes": [{"nodes": [0]}],
	"nodes": [
		{"name": "cart", "translation": [10, 0, 0], "children": [1]},
		{"name": "sail", "translation": [0, 2, 0], "mesh": 0}
	],
	"meshes": [{"primitives": [{"attributes": {"POSITION": 0}}]}],
	"buffers": [{"byteLength": %d, "uri": %q}],
	"bufferViews": [
		{"buffer": 0, "byteOffset": 0, "byteLength": 36},
		{"buffer": 0, "byteOffset": 36, "byteLength": 8},
		{"buffer": 0, "byteOffset": 44, "byteLength": 24}
	],
	"accessors": [
		{"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3", "min": [0, 0, 0], "max": [1, 1, 0]},
		{"bufferView": 1, "componentType": 5126, "count": 2, "type": "SCALAR", "min": [0], "max": [1]},
		{"bufferView": 2, "componentType": 5126, "count": 2, "type": "VEC3"}
	],
	"animations": [{
		"name": "roll",
		"samplers": [{"input": 1, "output": 2}],
		"channels": [{"sampler": 0, "target": {"node": 0, "path": "translation"}}]
	}]
}`, len(bin), uri)
}

// Load parses a .gltf or .glb document and decodes its meshes' vertex data.
func ExampleLoad() {
	doc, err := gltf.Load(sceneJSON(), nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	prim := doc.Meshes[0].Primitives[0]
	fmt.Println(doc.Nodes[1].Name, len(prim.Positions), prim.Positions[1])
	fmt.Println(prim.Bounds.Min, prim.Bounds.Max)
	// Output:
	// sail 3 {1 0 0}
	// {0 0 0} {1 1 0}
}

// ComputeWorldMatrices composes each node's transform with its parents', so a child placed relative to its parent gets its place in the scene.
func ExampleDocument_ComputeWorldMatrices() {
	doc, err := gltf.Load(sceneJSON(), nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	if err := doc.ComputeWorldMatrices(); err != nil {
		fmt.Println(err)
		return
	}
	m := doc.Nodes[1].WorldMatrix
	fmt.Println(m[12], m[13], m[14])
	// Output:
	// 10 2 0
}

// An Animation samples its channels at a time, and Apply writes the sampled values into the document's nodes.
func ExampleAnimation() {
	doc, err := gltf.Load(sceneJSON(), nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	anim := &doc.Animations[0]
	for _, s := range anim.Sample(0.25) {
		fmt.Println(s.Node, s.Path, s.Translation)
	}
	anim.Apply(doc, 0.5)
	fmt.Println(doc.Nodes[0].Translation)
	// Output:
	// 0 translation {1 0 0}
	// {2 0 0}
}
