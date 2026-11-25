package scenemodel_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/scenemodel"
)

// crateGLBPath is the textured model the tests' project names, which TestCrateFixtureIsCurrent keeps equal to what crateGLB generates.
var crateGLBPath = filepath.Join("testdata", "project", "models", "crate.glb")

// crateTexture returns the 8 by 8 sRGB checkerboard of orange and dark brown planks the crate's Wood material samples.
func crateTexture() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			c := color.NRGBA{R: 230, G: 140, B: 40, A: 255}
			if (x/2+y/2)%2 == 1 {
				c = color.NRGBA{R: 70, G: 40, B: 20, A: 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// crateGLB builds a binary glTF model with the same bytes on every run: node "Crate" draws mesh "CrateMesh", a 1 m box textured with crateTexture, and its child node "Lid", 0.6 m above it and turned a quarter turn about Y, draws mesh "LidMesh", a box 1.1 wide, 0.2 tall and 1.1 deep in an untextured brass material.
func crateGLB(t testing.TB) []byte {
	t.Helper()
	box, err := scenemodel.PrimitiveMesh("box")
	if err != nil {
		t.Fatal(err)
	}
	var bin bytes.Buffer
	type view struct {
		Buffer     int `json:"buffer"`
		ByteOffset int `json:"byteOffset"`
		ByteLength int `json:"byteLength"`
		Target     int `json:"target,omitempty"`
	}
	var views []view
	addView := func(data []byte, target int) int {
		for bin.Len()%4 != 0 {
			bin.WriteByte(0)
		}
		views = append(views, view{ByteOffset: bin.Len(), ByteLength: len(data), Target: target})
		bin.Write(data)
		return len(views) - 1
	}
	var accessors []map[string]any
	addAccessor := func(a map[string]any) int {
		accessors = append(accessors, a)
		return len(accessors) - 1
	}
	floats := func(vs ...float32) []byte {
		out := make([]byte, 4*len(vs))
		for i, v := range vs {
			binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(v))
		}
		return out
	}
	addBox := func(sx, sy, sz float32) map[string]int {
		var pos, nrm, uv []float32
		minP := [3]float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
		maxP := [3]float32{-math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}
		for i, p := range box.Positions {
			q := [3]float32{p.X * sx, p.Y * sy, p.Z * sz}
			for k := range 3 {
				minP[k], maxP[k] = min(minP[k], q[k]), max(maxP[k], q[k])
			}
			pos = append(pos, q[0], q[1], q[2])
			n := box.Normals[i]
			nrm = append(nrm, n.X, n.Y, n.Z)
			uv = append(uv, box.UVs[i].X, box.UVs[i].Y)
		}
		idx := make([]byte, 2*len(box.Indices))
		for i, v := range box.Indices {
			binary.LittleEndian.PutUint16(idx[2*i:], uint16(v))
		}
		n := len(box.Positions)
		return map[string]int{
			"POSITION":   addAccessor(map[string]any{"bufferView": addView(floats(pos...), 34962), "componentType": 5126, "count": n, "type": "VEC3", "min": minP[:], "max": maxP[:]}),
			"NORMAL":     addAccessor(map[string]any{"bufferView": addView(floats(nrm...), 34962), "componentType": 5126, "count": n, "type": "VEC3"}),
			"TEXCOORD_0": addAccessor(map[string]any{"bufferView": addView(floats(uv...), 34962), "componentType": 5126, "count": n, "type": "VEC2"}),
			"indices":    addAccessor(map[string]any{"bufferView": addView(idx, 34963), "componentType": 5123, "count": len(box.Indices), "type": "SCALAR"}),
		}
	}
	crate := addBox(1, 1, 1)
	lidAcc := addBox(1.1, 0.2, 1.1)
	var pngBuf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&pngBuf, crateTexture()); err != nil {
		t.Fatal(err)
	}
	imageView := addView(pngBuf.Bytes(), 0)
	for bin.Len()%4 != 0 {
		bin.WriteByte(0)
	}
	primitive := func(acc map[string]int, material int) map[string]any {
		return map[string]any{
			"attributes": map[string]int{"POSITION": acc["POSITION"], "NORMAL": acc["NORMAL"], "TEXCOORD_0": acc["TEXCOORD_0"]},
			"indices":    acc["indices"],
			"material":   material,
		}
	}
	quarter := float32(math.Sqrt2 / 2)
	doc := map[string]any{
		"asset":  map[string]any{"version": "2.0", "generator": "scenemodel test fixture"},
		"scene":  0,
		"scenes": []any{map[string]any{"name": "Scene", "nodes": []int{0}}},
		"nodes": []any{
			map[string]any{"name": "Crate", "mesh": 0, "children": []int{1}},
			map[string]any{"name": "Lid", "mesh": 1, "translation": []float32{0, 0.6, 0}, "rotation": []float32{0, quarter, 0, quarter}},
		},
		"meshes": []any{
			map[string]any{"name": "CrateMesh", "primitives": []any{primitive(crate, 0)}},
			map[string]any{"name": "LidMesh", "primitives": []any{primitive(lidAcc, 1)}},
		},
		"materials": []any{
			map[string]any{"name": "Wood", "pbrMetallicRoughness": map[string]any{"baseColorTexture": map[string]int{"index": 0}, "metallicFactor": 0, "roughnessFactor": 0.8}},
			map[string]any{"name": "Brass", "pbrMetallicRoughness": map[string]any{"baseColorFactor": []float32{0.9, 0.6, 0.15, 1}, "metallicFactor": 1, "roughnessFactor": 0.35}},
		},
		"textures":    []any{map[string]int{"sampler": 0, "source": 0}},
		"samplers":    []any{map[string]int{"magFilter": 9728, "minFilter": 9728}},
		"images":      []any{map[string]any{"name": "planks", "bufferView": imageView, "mimeType": "image/png"}},
		"accessors":   accessors,
		"bufferViews": views,
		"buffers":     []any{map[string]int{"byteLength": bin.Len()}},
	}
	js, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	for len(js)%4 != 0 {
		js = append(js, ' ')
	}
	var out bytes.Buffer
	header := func(vs ...uint32) {
		for _, v := range vs {
			_ = binary.Write(&out, binary.LittleEndian, v)
		}
	}
	header(0x46546C67, 2, uint32(12+8+len(js)+8+bin.Len()))
	header(uint32(len(js)), 0x4E4F534A)
	out.Write(js)
	header(uint32(bin.Len()), 0x004E4942)
	out.Write(bin.Bytes())
	return out.Bytes()
}

// writePNG encodes img into w as a PNG.
func writePNG(t testing.TB, w io.Writer, img image.Image) {
	t.Helper()
	if err := png.Encode(w, img); err != nil {
		t.Fatal(err)
	}
}

// TestCrateFixtureIsCurrent keeps testdata/project/models/crate.glb equal to what crateGLB builds; UPDATE_GOLDEN=1 rewrites it.
func TestCrateFixtureIsCurrent(t *testing.T) {
	want := crateGLB(t)
	if gametest.ShouldUpdateGolden() {
		if err := os.WriteFile(crateGLBPath, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(crateGLBPath)
	if err != nil {
		t.Fatalf("read %s: %v (UPDATE_GOLDEN=1 writes it)", crateGLBPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is out of date; run the tests with UPDATE_GOLDEN=1", crateGLBPath)
	}
}
