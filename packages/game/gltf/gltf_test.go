package gltf_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
)

// --- Binary & GLB test builders ---

func appendFloat32s(buf *bytes.Buffer, vals ...float32) int {
	off := buf.Len()
	for _, v := range vals {
		_ = binary.Write(buf, binary.LittleEndian, v)
	}
	return off
}

func appendUint16s(buf *bytes.Buffer, vals ...uint16) int {
	off := buf.Len()
	for _, v := range vals {
		_ = binary.Write(buf, binary.LittleEndian, v)
	}
	return off
}

func pad4(buf *bytes.Buffer, padByte byte) {
	for buf.Len()%4 != 0 {
		buf.WriteByte(padByte)
	}
}

func buildGLB(jsonBytes, binBytes []byte, extraChunks ...struct {
	chunkType uint32
	payload   []byte
}) []byte {
	jBuf := bytes.NewBuffer(append([]byte(nil), jsonBytes...))
	pad4(jBuf, 0x20)

	var bBuf *bytes.Buffer
	if binBytes != nil {
		bBuf = bytes.NewBuffer(append([]byte(nil), binBytes...))
		pad4(bBuf, 0x00)
	}

	totalLen := 12 + 8 + jBuf.Len()
	if bBuf != nil {
		totalLen += 8 + bBuf.Len()
	}
	for _, ec := range extraChunks {
		p := len(ec.payload)
		p = (p + 3) &^ 3
		totalLen += 8 + p
	}

	out := bytes.NewBuffer(make([]byte, 0, totalLen))
	_ = binary.Write(out, binary.LittleEndian, uint32(0x46546C67)) // "glTF"
	_ = binary.Write(out, binary.LittleEndian, uint32(2))          // version 2
	_ = binary.Write(out, binary.LittleEndian, uint32(totalLen))

	_ = binary.Write(out, binary.LittleEndian, uint32(jBuf.Len()))
	_ = binary.Write(out, binary.LittleEndian, uint32(0x4E4F534A)) // "JSON"
	out.Write(jBuf.Bytes())

	if bBuf != nil {
		_ = binary.Write(out, binary.LittleEndian, uint32(bBuf.Len()))
		_ = binary.Write(out, binary.LittleEndian, uint32(0x004E4942)) // "BIN\0"
		out.Write(bBuf.Bytes())
	}

	for _, ec := range extraChunks {
		eb := bytes.NewBuffer(append([]byte(nil), ec.payload...))
		pad4(eb, 0x00)
		_ = binary.Write(out, binary.LittleEndian, uint32(eb.Len()))
		_ = binary.Write(out, binary.LittleEndian, ec.chunkType)
		out.Write(eb.Bytes())
	}
	return out.Bytes()
}

// buildKhronosBox builds a standard Khronos-spec unit Box ([-0.5, 0.5]^3,
// 24 vertices with per-face normals, 36 uint16 indices, red PBR material)
// returning the JSON map (with buffer URI customizable) and raw .bin bytes.
func buildKhronosBox() (map[string]any, []byte, []math3d.Vec3, []math3d.Vec3, []uint32) {
	type face struct {
		n     math3d.Vec3
		verts [4]math3d.Vec3
	}
	v3 := func(x, y, z float32) math3d.Vec3 { return math3d.Vec3{X: x, Y: y, Z: z} }
	faces := []face{
		{v3(0, 0, 1), [4]math3d.Vec3{v3(-0.5, -0.5, 0.5), v3(0.5, -0.5, 0.5), v3(0.5, 0.5, 0.5), v3(-0.5, 0.5, 0.5)}},
		{v3(0, 0, -1), [4]math3d.Vec3{v3(0.5, -0.5, -0.5), v3(-0.5, -0.5, -0.5), v3(-0.5, 0.5, -0.5), v3(0.5, 0.5, -0.5)}},
		{v3(1, 0, 0), [4]math3d.Vec3{v3(0.5, -0.5, 0.5), v3(0.5, -0.5, -0.5), v3(0.5, 0.5, -0.5), v3(0.5, 0.5, 0.5)}},
		{v3(-1, 0, 0), [4]math3d.Vec3{v3(-0.5, -0.5, -0.5), v3(-0.5, -0.5, 0.5), v3(-0.5, 0.5, 0.5), v3(-0.5, 0.5, -0.5)}},
		{v3(0, 1, 0), [4]math3d.Vec3{v3(-0.5, 0.5, 0.5), v3(0.5, 0.5, 0.5), v3(0.5, 0.5, -0.5), v3(-0.5, 0.5, -0.5)}},
		{v3(0, -1, 0), [4]math3d.Vec3{v3(-0.5, -0.5, -0.5), v3(0.5, -0.5, -0.5), v3(0.5, -0.5, 0.5), v3(-0.5, -0.5, 0.5)}},
	}

	var positions []math3d.Vec3
	var normals []math3d.Vec3
	var indices []uint32

	var bin bytes.Buffer
	idxOff := bin.Len()
	for fIdx, f := range faces {
		base := uint16(fIdx * 4)
		tri := [6]uint16{base, base + 1, base + 2, base, base + 2, base + 3}
		appendUint16s(&bin, tri[:]...)
		for _, id := range tri {
			indices = append(indices, uint32(id))
		}
		for _, v := range f.verts {
			positions = append(positions, v)
			normals = append(normals, f.n)
		}
	}
	idxLen := bin.Len() - idxOff
	pad4(&bin, 0)

	posOff := bin.Len()
	for _, p := range positions {
		appendFloat32s(&bin, p.X, p.Y, p.Z)
	}
	posLen := bin.Len() - posOff

	normOff := bin.Len()
	for _, n := range normals {
		appendFloat32s(&bin, n.X, n.Y, n.Z)
	}
	normLen := bin.Len() - normOff

	binBytes := bin.Bytes()

	doc := map[string]any{
		"asset": map[string]any{
			"version":   "2.0",
			"generator": "nsgo-gltf-test",
		},
		"scene": 0,
		"scenes": []any{
			map[string]any{"name": "Scene", "nodes": []int{0}},
		},
		"nodes": []any{
			map[string]any{"name": "BoxNode", "mesh": 0},
		},
		"materials": []any{
			map[string]any{
				"name": "Red",
				"pbrMetallicRoughness": map[string]any{
					"baseColorFactor": []float32{0.8, 0.0, 0.0, 1.0},
					"metallicFactor":  0.0,
					"roughnessFactor": 0.9,
				},
			},
		},
		"meshes": []any{
			map[string]any{
				"name": "BoxMesh",
				"primitives": []any{
					map[string]any{
						"attributes": map[string]int{
							"POSITION": 1,
							"NORMAL":   2,
						},
						"indices":  0,
						"material": 0,
						"mode":     4,
					},
				},
			},
		},
		"buffers": []any{
			map[string]any{
				"byteLength": len(binBytes),
			},
		},
		"bufferViews": []any{
			map[string]any{"buffer": 0, "byteOffset": idxOff, "byteLength": idxLen, "target": 34963},
			map[string]any{"buffer": 0, "byteOffset": posOff, "byteLength": posLen, "target": 34962},
			map[string]any{"buffer": 0, "byteOffset": normOff, "byteLength": normLen, "target": 34962},
		},
		"accessors": []any{
			map[string]any{"bufferView": 0, "componentType": 5123, "count": len(indices), "type": "SCALAR"},
			map[string]any{"bufferView": 1, "componentType": 5126, "count": len(positions), "type": "VEC3", "min": []float32{-0.5, -0.5, -0.5}, "max": []float32{0.5, 0.5, 0.5}},
			map[string]any{"bufferView": 2, "componentType": 5126, "count": len(normals), "type": "VEC3"},
		},
	}

	return doc, binBytes, positions, normals, indices
}

func TestBoxGLTFAndGLBIdentical(t *testing.T) {
	baseJSON, binBytes, wantPos, wantNorm, wantIdx := buildKhronosBox()

	// 1) Binary .glb (plus an unknown trailing chunk that must be skipped per spec).
	glbJSON, err := json.Marshal(baseJSON)
	if err != nil {
		t.Fatalf("marshal glb json: %v", err)
	}
	glbBytes := buildGLB(glbJSON, binBytes, struct {
		chunkType uint32
		payload   []byte
	}{chunkType: 0x58585858, payload: []byte("ignored-extension-chunk")})

	docGLB, err := gltf.Load(glbBytes, nil)
	if err != nil {
		t.Fatalf("Load GLB failed: %v", err)
	}

	// 2) JSON .gltf with external .bin via fstest.MapFS + LoadFS.
	baseJSON["buffers"].([]any)[0].(map[string]any)["uri"] = "box.bin"
	extJSON, err := json.Marshal(baseJSON)
	if err != nil {
		t.Fatalf("marshal ext json: %v", err)
	}
	fsys := fstest.MapFS{
		"models/box.gltf": {Data: extJSON},
		"models/box.bin":  {Data: binBytes},
	}
	docFS, err := gltf.LoadFS(fsys, "models/box.gltf")
	if err != nil {
		t.Fatalf("LoadFS failed: %v", err)
	}

	// 3) JSON .gltf with embedded base64 data URI.
	dataURI := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(binBytes)
	baseJSON["buffers"].([]any)[0].(map[string]any)["uri"] = dataURI
	uriJSON, err := json.Marshal(baseJSON)
	if err != nil {
		t.Fatalf("marshal data-uri json: %v", err)
	}
	docURI, err := gltf.Load(uriJSON, nil)
	if err != nil {
		t.Fatalf("Load data URI failed: %v", err)
	}

	docs := []struct {
		label string
		doc   *gltf.Document
	}{
		{"GLB", docGLB},
		{"LoadFS", docFS},
		{"DataURI", docURI},
	}

	wantBounds := math3d.AABB{
		Min: math3d.Vec3{X: -0.5, Y: -0.5, Z: -0.5},
		Max: math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5},
	}

	for _, tc := range docs {
		t.Run(tc.label, func(t *testing.T) {
			d := tc.doc
			if d.Asset.Version != "2.0" {
				t.Fatalf("version = %q, want 2.0", d.Asset.Version)
			}
			if len(d.Meshes) != 1 || len(d.Meshes[0].Primitives) != 1 {
				t.Fatalf("unexpected mesh/primitive count: %d", len(d.Meshes))
			}
			prim := d.Meshes[0].Primitives[0]
			if prim.Mode != gltf.ModeTriangles {
				t.Fatalf("mode = %d, want %d", prim.Mode, gltf.ModeTriangles)
			}
			if len(prim.Positions) != len(wantPos) {
				t.Fatalf("positions len = %d, want %d", len(prim.Positions), len(wantPos))
			}
			for i := range wantPos {
				if !prim.Positions[i].ApproxEqual(wantPos[i], 1e-6) {
					t.Fatalf("pos[%d] = %v, want %v", i, prim.Positions[i], wantPos[i])
				}
				if !prim.Normals[i].ApproxEqual(wantNorm[i], 1e-6) {
					t.Fatalf("norm[%d] = %v, want %v", i, prim.Normals[i], wantNorm[i])
				}
			}
			if len(prim.Indices) != len(wantIdx) {
				t.Fatalf("indices len = %d, want %d", len(prim.Indices), len(wantIdx))
			}
			for i := range wantIdx {
				if prim.Indices[i] != wantIdx[i] {
					t.Fatalf("idx[%d] = %d, want %d", i, prim.Indices[i], wantIdx[i])
				}
			}
			if !prim.Bounds.Min.ApproxEqual(wantBounds.Min, 1e-6) || !prim.Bounds.Max.ApproxEqual(wantBounds.Max, 1e-6) {
				t.Fatalf("bounds = %+v, want %+v", prim.Bounds, wantBounds)
			}
			if len(d.Materials) != 1 {
				t.Fatalf("materials len = %d, want 1", len(d.Materials))
			}
			mat := d.Materials[0]
			wantColor := math3d.Vec4{X: 0.8, Y: 0, Z: 0, W: 1}
			if !mat.BaseColorFactor.ApproxEqual(wantColor, 1e-6) {
				t.Fatalf("BaseColorFactor = %v, want %v", mat.BaseColorFactor, wantColor)
			}
			if mat.MetallicFactor != 0 || !math3d.ApproxEqual(mat.RoughnessFactor, 0.9, 1e-6) {
				t.Fatalf("metallic=%v roughness=%v, want 0, 0.9", mat.MetallicFactor, mat.RoughnessFactor)
			}
			if mat.AlphaMode != gltf.AlphaOpaque || mat.AlphaCutoff != 0.5 || mat.DoubleSided {
				t.Fatalf("unexpected material defaults: %+v", mat)
			}
		})
	}
}

func TestAccessorDecodingInterleavedNormalizedAndSparse(t *testing.T) {
	var bin bytes.Buffer

	// BufferView 0: Interleaved vertex stream, stride = 28 bytes:
	//   offset 0:  POSITION (VEC3 FLOAT, 12 bytes)
	//   offset 12: TEXCOORD_0 (VEC2 UNSIGNED_SHORT normalized, 4 bytes)
	//   offset 16: COLOR_0 (VEC4 UNSIGNED_BYTE normalized, 4 bytes)
	//   offset 20: signed normalized VEC2 BYTE (2 bytes) + 2 bytes padding
	//   offset 24: signed normalized SCALAR SHORT (2 bytes) + 2 bytes padding
	bv0Off := bin.Len()
	type interleavedVtx struct {
		pos    [3]float32
		uv     [2]uint16
		col    [4]uint8
		sbyte  [2]int8
		pad0   [2]byte
		sshort int16
		pad1   [2]byte
	}
	verts := []interleavedVtx{
		{pos: [3]float32{1, 2, 3}, uv: [2]uint16{0, 65535}, col: [4]uint8{255, 0, 128, 255}, sbyte: [2]int8{-128, 127}, sshort: -32768},
		{pos: [3]float32{4, 5, 6}, uv: [2]uint16{32768, 0}, col: [4]uint8{0, 255, 0, 128}, sbyte: [2]int8{-127, 0}, sshort: 32767},
	}
	for _, v := range verts {
		_ = binary.Write(&bin, binary.LittleEndian, v)
	}
	bv0Len := bin.Len() - bv0Off

	// BufferView 1: Sparse indices (UNSIGNED_BYTE: [1, 3])
	bv1Off := bin.Len()
	bin.Write([]byte{1, 3})
	bv1Len := bin.Len() - bv1Off
	pad4(&bin, 0)

	// BufferView 2: Sparse VEC3 FLOAT values for indices 1 and 3
	bv2Off := bin.Len()
	appendFloat32s(&bin, 10, 20, 30, 40, 50, 60)
	bv2Len := bin.Len() - bv2Off

	// BufferView 3: Base VEC3 FLOAT values (4 elements) for base+sparse test
	bv3Off := bin.Len()
	appendFloat32s(&bin, 1, 1, 1, 2, 2, 2, 3, 3, 3, 4, 4, 4)
	bv3Len := bin.Len() - bv3Off

	// BufferView 4: UNSIGNED_INT indices ([0, 1, 2])
	bv4Off := bin.Len()
	_ = binary.Write(&bin, binary.LittleEndian, uint32(0))
	_ = binary.Write(&bin, binary.LittleEndian, uint32(1))
	_ = binary.Write(&bin, binary.LittleEndian, uint32(2))
	bv4Len := bin.Len() - bv4Off

	raw := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"buffers": []any{
			map[string]any{
				"byteLength": bin.Len(),
				"uri":        "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(bin.Bytes()),
			},
		},
		"bufferViews": []any{
			map[string]any{"buffer": 0, "byteOffset": bv0Off, "byteLength": bv0Len, "byteStride": 28},
			map[string]any{"buffer": 0, "byteOffset": bv1Off, "byteLength": bv1Len},
			map[string]any{"buffer": 0, "byteOffset": bv2Off, "byteLength": bv2Len},
			map[string]any{"buffer": 0, "byteOffset": bv3Off, "byteLength": bv3Len},
			map[string]any{"buffer": 0, "byteOffset": bv4Off, "byteLength": bv4Len},
		},
		"accessors": []any{
			// 0: interleaved POSITION
			map[string]any{"bufferView": 0, "byteOffset": 0, "componentType": 5126, "count": 2, "type": "VEC3"},
			// 1: interleaved normalized U16 VEC2
			map[string]any{"bufferView": 0, "byteOffset": 12, "componentType": 5123, "normalized": true, "count": 2, "type": "VEC2"},
			// 2: interleaved normalized U8 VEC4
			map[string]any{"bufferView": 0, "byteOffset": 16, "componentType": 5121, "normalized": true, "count": 2, "type": "VEC4"},
			// 3: interleaved normalized BYTE VEC2 (tests -128 -> -1.0 clamping)
			map[string]any{"bufferView": 0, "byteOffset": 20, "componentType": 5120, "normalized": true, "count": 2, "type": "VEC2"},
			// 4: interleaved normalized SHORT SCALAR (tests -32768 -> -1.0 clamping)
			map[string]any{"bufferView": 0, "byteOffset": 24, "componentType": 5122, "normalized": true, "count": 2, "type": "SCALAR"},
			// 5: zero-initialized (no bufferView) + sparse VEC3
			map[string]any{
				"componentType": 5126,
				"count":         4,
				"type":          "VEC3",
				"sparse": map[string]any{
					"count":   2,
					"indices": map[string]any{"bufferView": 1, "componentType": 5121},
					"values":  map[string]any{"bufferView": 2},
				},
			},
			// 6: base bufferView + sparse VEC3
			map[string]any{
				"bufferView":    3,
				"componentType": 5126,
				"count":         4,
				"type":          "VEC3",
				"sparse": map[string]any{
					"count":   2,
					"indices": map[string]any{"bufferView": 1, "componentType": 5121},
					"values":  map[string]any{"bufferView": 2},
				},
			},
			// 7: UNSIGNED_INT indices
			map[string]any{"bufferView": 4, "componentType": 5125, "count": 3, "type": "SCALAR"},
		},
	}

	rawBytes, _ := json.Marshal(raw)
	doc, err := gltf.Load(rawBytes, nil)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	pos, err := doc.ReadVec3(0)
	if err != nil {
		t.Fatalf("ReadVec3(0): %v", err)
	}
	if !pos[0].ApproxEqual(math3d.Vec3{X: 1, Y: 2, Z: 3}, 1e-6) || !pos[1].ApproxEqual(math3d.Vec3{X: 4, Y: 5, Z: 6}, 1e-6) {
		t.Fatalf("interleaved pos = %v", pos)
	}

	uv, err := doc.ReadVec2(1)
	if err != nil {
		t.Fatalf("ReadVec2(1): %v", err)
	}
	if !uv[0].ApproxEqual(math3d.Vec2{X: 0, Y: 1}, 1e-5) || !uv[1].ApproxEqual(math3d.Vec2{X: 32768.0 / 65535.0, Y: 0}, 1e-5) {
		t.Fatalf("normalized u16 uv = %v", uv)
	}

	col, err := doc.ReadVec4(2)
	if err != nil {
		t.Fatalf("ReadVec4(2): %v", err)
	}
	if !col[0].ApproxEqual(math3d.Vec4{X: 1, Y: 0, Z: 128.0 / 255.0, W: 1}, 1e-5) {
		t.Fatalf("normalized u8 col[0] = %v", col[0])
	}

	sb, err := doc.ReadVec2(3)
	if err != nil {
		t.Fatalf("ReadVec2(3): %v", err)
	}
	if !sb[0].ApproxEqual(math3d.Vec2{X: -1, Y: 1}, 1e-6) || !sb[1].ApproxEqual(math3d.Vec2{X: -1, Y: 0}, 1e-6) {
		t.Fatalf("normalized byte vec2 = %v", sb)
	}

	ss, err := doc.ReadScalar(4)
	if err != nil {
		t.Fatalf("ReadScalar(4): %v", err)
	}
	if !math3d.ApproxEqual(ss[0], -1, 1e-6) || !math3d.ApproxEqual(ss[1], 1, 1e-6) {
		t.Fatalf("normalized short scalar = %v", ss)
	}

	sparseZero, err := doc.ReadVec3(5)
	if err != nil {
		t.Fatalf("ReadVec3(5) sparse zero: %v", err)
	}
	wantZero := []math3d.Vec3{
		{X: 0, Y: 0, Z: 0},
		{X: 10, Y: 20, Z: 30},
		{X: 0, Y: 0, Z: 0},
		{X: 40, Y: 50, Z: 60},
	}
	for i := range wantZero {
		if !sparseZero[i].ApproxEqual(wantZero[i], 1e-6) {
			t.Fatalf("sparseZero[%d] = %v, want %v", i, sparseZero[i], wantZero[i])
		}
	}

	sparseBase, err := doc.ReadVec3(6)
	if err != nil {
		t.Fatalf("ReadVec3(6) sparse base: %v", err)
	}
	wantBase := []math3d.Vec3{
		{X: 1, Y: 1, Z: 1},
		{X: 10, Y: 20, Z: 30},
		{X: 3, Y: 3, Z: 3},
		{X: 40, Y: 50, Z: 60},
	}
	for i := range wantBase {
		if !sparseBase[i].ApproxEqual(wantBase[i], 1e-6) {
			t.Fatalf("sparseBase[%d] = %v, want %v", i, sparseBase[i], wantBase[i])
		}
	}

	indices, err := doc.ReadIndices(7)
	if err != nil {
		t.Fatalf("ReadIndices(7): %v", err)
	}
	if len(indices) != 3 || indices[0] != 0 || indices[1] != 1 || indices[2] != 2 {
		t.Fatalf("indices = %v", indices)
	}
}

func TestNestedNodeHierarchyAndCycleRejection(t *testing.T) {
	// 4-level hierarchy:
	//   Node 0 (Root): explicit matrix = Translate(10, 0, 0) * Scale(2, 2, 2)
	//   Node 1 (Child): TRS = T(0, 5, 0), R = RotY(90 deg), S = (1, 1, 1)
	//   Node 2 (Grandchild): TRS = T(3, 0, 0), R = Identity, S = (0.5, 2, 1)
	//   Node 3 (Great-grandchild): explicit matrix = Translate(0, 1, 4)
	m0 := math3d.Translate(math3d.Vec3{X: 10, Y: 0, Z: 0}).Mul(math3d.Scale(math3d.Vec3{X: 2, Y: 2, Z: 2}))
	q1 := math3d.QuatFromAxisAngle(math3d.Vec3Up(), math.Pi/2)
	m1 := math3d.TRS(math3d.Vec3{X: 0, Y: 5, Z: 0}, q1, math3d.Vec3One())
	m2 := math3d.TRS(math3d.Vec3{X: 3, Y: 0, Z: 0}, math3d.QuatIdentity(), math3d.Vec3{X: 0.5, Y: 2, Z: 1})
	m3 := math3d.Translate(math3d.Vec3{X: 0, Y: 1, Z: 4})

	raw := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"scene": 0,
		"scenes": []any{
			map[string]any{"name": "Hierarchy", "nodes": []int{0}},
		},
		"nodes": []any{
			map[string]any{"name": "Root", "matrix": m0[:], "children": []int{1}},
			map[string]any{"name": "Child", "translation": []float32{0, 5, 0}, "rotation": []float32{q1.X, q1.Y, q1.Z, q1.W}, "children": []int{2}},
			map[string]any{"name": "Grandchild", "translation": []float32{3, 0, 0}, "scale": []float32{0.5, 2, 1}, "children": []int{3}},
			map[string]any{"name": "Leaf", "matrix": m3[:]},
		},
	}

	rawBytes, _ := json.Marshal(raw)
	doc, err := gltf.Load(rawBytes, nil)
	if err != nil {
		t.Fatalf("Load hierarchy failed: %v", err)
	}

	wantW0 := m0
	wantW1 := wantW0.Mul(m1)
	wantW2 := wantW1.Mul(m2)
	wantW3 := wantW2.Mul(m3)

	wants := []math3d.Mat4{wantW0, wantW1, wantW2, wantW3}
	wantParents := []int{-1, 0, 1, 2}
	for i := range wants {
		if doc.Nodes[i].Parent != wantParents[i] {
			t.Fatalf("node %d parent = %d, want %d", i, doc.Nodes[i].Parent, wantParents[i])
		}
		if !doc.Nodes[i].WorldMatrix.ApproxEqual(wants[i], 1e-5) {
			t.Fatalf("node %d WorldMatrix =\n%v\nwant:\n%v", i, doc.Nodes[i].WorldMatrix, wants[i])
		}
	}

	// Verify transforming the origin through Leaf's WorldMatrix matches step-by-step math:
	// Leaf origin (0,0,0) -> m3: (0,1,4) -> m2: (3,2,4) -> m1(rotY 90deg + T(0,5,0)): (4,7,-3) -> m0(*2 + T(10,0,0)): (18,14,-6)
	leafOriginWorld := doc.Nodes[3].WorldMatrix.TransformPoint(math3d.Vec3Zero())
	wantLeafPt := math3d.Vec3{X: 18, Y: 14, Z: -6}
	if !leafOriginWorld.ApproxEqual(wantLeafPt, 1e-4) {
		t.Fatalf("leaf world point = %v, want %v", leafOriginWorld, wantLeafPt)
	}

	// Verify runtime mutation + ComputeWorldMatrices updates descendants.
	doc.Nodes[2].SetTransform(math3d.Transform{
		Pos:   math3d.Vec3{X: 0, Y: 0, Z: 0},
		Rot:   math3d.QuatIdentity(),
		Scale: math3d.Vec3One(),
	})
	if err := doc.ComputeWorldMatrices(); err != nil {
		t.Fatalf("recompute world matrices: %v", err)
	}
	wantW3Updated := wantW1.Mul(m3)
	if !doc.Nodes[3].WorldMatrix.ApproxEqual(wantW3Updated, 1e-5) {
		t.Fatalf("updated node 3 WorldMatrix mismatch")
	}

	// Cycle detection cases:
	cycleCases := []struct {
		name  string
		nodes []any
	}{
		{
			name: "self-loop",
			nodes: []any{
				map[string]any{"children": []int{0}},
			},
		},
		{
			name: "two-node cycle",
			nodes: []any{
				map[string]any{"children": []int{1}},
				map[string]any{"children": []int{0}},
			},
		},
		{
			name: "disconnected cycle",
			nodes: []any{
				map[string]any{"children": []int{1}},
				map[string]any{},
				map[string]any{"children": []int{3}},
				map[string]any{"children": []int{2}},
			},
		},
		{
			name: "diamond multiple parents",
			nodes: []any{
				map[string]any{"children": []int{1, 2}},
				map[string]any{"children": []int{3}},
				map[string]any{"children": []int{3}},
				map[string]any{},
			},
		},
	}
	for _, tc := range cycleCases {
		t.Run("reject_"+tc.name, func(t *testing.T) {
			bad, _ := json.Marshal(map[string]any{
				"asset": map[string]any{"version": "2.0"},
				"nodes": tc.nodes,
			})
			if _, err := gltf.Load(bad, nil); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestMorphTargetsAndWeightsAnimation(t *testing.T) {
	// AnimatedMorphCube-style 2-target mesh:
	// 4 base vertices (a quad), Target 0 expands +X by 1, Target 1 expands +Y by 2.
	var bin bytes.Buffer

	posOff := appendFloat32s(&bin,
		0, 0, 0,
		1, 0, 0,
		1, 1, 0,
		0, 1, 0,
	)
	posLen := bin.Len() - posOff

	normOff := appendFloat32s(&bin,
		0, 0, 1,
		0, 0, 1,
		0, 0, 1,
		0, 0, 1,
	)
	normLen := bin.Len() - normOff

	// Target 0 position deltas: (+1, 0, 0) on every vertex
	t0PosOff := appendFloat32s(&bin,
		1, 0, 0,
		1, 0, 0,
		1, 0, 0,
		1, 0, 0,
	)
	t0PosLen := bin.Len() - t0PosOff

	// Target 1 position deltas: (0, +2, +0.5) on every vertex
	t1PosOff := appendFloat32s(&bin,
		0, 2, 0.5,
		0, 2, 0.5,
		0, 2, 0.5,
		0, 2, 0.5,
	)
	t1PosLen := bin.Len() - t1PosOff

	// Target 0 normal deltas: (+1, 0, 0)
	t0NormOff := appendFloat32s(&bin,
		1, 0, 0,
		1, 0, 0,
		1, 0, 0,
		1, 0, 0,
	)
	t0NormLen := bin.Len() - t0NormOff

	// Animation times: [0.0, 1.0, 2.0]
	timeOff := appendFloat32s(&bin, 0.0, 1.0, 2.0)
	timeLen := bin.Len() - timeOff

	// Animation weights (2 targets * 3 keyframes = 6 SCALAR floats):
	// t=0: [0.0, 0.0], t=1: [1.0, 0.5], t=2: [0.0, 1.0]
	wOff := appendFloat32s(&bin, 0.0, 0.0, 1.0, 0.5, 0.0, 1.0)
	wLen := bin.Len() - wOff

	raw := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"buffers": []any{
			map[string]any{"byteLength": bin.Len()},
		},
		"bufferViews": []any{
			map[string]any{"buffer": 0, "byteOffset": posOff, "byteLength": posLen},
			map[string]any{"buffer": 0, "byteOffset": normOff, "byteLength": normLen},
			map[string]any{"buffer": 0, "byteOffset": t0PosOff, "byteLength": t0PosLen},
			map[string]any{"buffer": 0, "byteOffset": t1PosOff, "byteLength": t1PosLen},
			map[string]any{"buffer": 0, "byteOffset": t0NormOff, "byteLength": t0NormLen},
			map[string]any{"buffer": 0, "byteOffset": timeOff, "byteLength": timeLen},
			map[string]any{"buffer": 0, "byteOffset": wOff, "byteLength": wLen},
		},
		"accessors": []any{
			map[string]any{"bufferView": 0, "componentType": 5126, "count": 4, "type": "VEC3"},
			map[string]any{"bufferView": 1, "componentType": 5126, "count": 4, "type": "VEC3"},
			map[string]any{"bufferView": 2, "componentType": 5126, "count": 4, "type": "VEC3"},
			map[string]any{"bufferView": 3, "componentType": 5126, "count": 4, "type": "VEC3"},
			map[string]any{"bufferView": 4, "componentType": 5126, "count": 4, "type": "VEC3"},
			map[string]any{"bufferView": 5, "componentType": 5126, "count": 3, "type": "SCALAR"},
			map[string]any{"bufferView": 6, "componentType": 5126, "count": 6, "type": "SCALAR"},
		},
		"meshes": []any{
			map[string]any{
				"name":    "MorphQuad",
				"weights": []float32{0.25, 0.75},
				"primitives": []any{
					map[string]any{
						"attributes": map[string]int{"POSITION": 0, "NORMAL": 1},
						"targets": []any{
							map[string]int{"POSITION": 2, "NORMAL": 4},
							map[string]int{"POSITION": 3},
						},
					},
				},
			},
		},
		"nodes": []any{
			map[string]any{"name": "MorphNode", "mesh": 0},
		},
		"animations": []any{
			map[string]any{
				"name": "MorphAnim",
				"samplers": []any{
					map[string]any{"input": 5, "output": 6, "interpolation": "LINEAR"},
				},
				"channels": []any{
					map[string]any{"sampler": 0, "target": map[string]any{"node": 0, "path": "weights"}},
				},
			},
		},
	}

	jsonBytes, _ := json.Marshal(raw)
	glbBytes := buildGLB(jsonBytes, bin.Bytes())
	doc, err := gltf.Load(glbBytes, nil)
	if err != nil {
		t.Fatalf("Load morph GLB: %v", err)
	}

	prim := doc.Meshes[0].Primitives[0]
	if len(prim.Targets) != 2 {
		t.Fatalf("targets len = %d, want 2", len(prim.Targets))
	}
	if len(doc.Animations) != 1 || !math3d.ApproxEqual(doc.Animations[0].Duration, 2.0, 1e-6) {
		t.Fatalf("unexpected animation duration: %+v", doc.Animations)
	}

	// Sample at t = 0.5 -> linear midpoint between [0, 0] and [1, 0.5] = [0.5, 0.25]
	samples := doc.Animations[0].Sample(0.5)
	if len(samples) != 1 || samples[0].Path != gltf.PathWeights || len(samples[0].Weights) != 2 {
		t.Fatalf("unexpected sample at t=0.5: %+v", samples)
	}
	if !math3d.ApproxEqual(samples[0].Weights[0], 0.5, 1e-6) || !math3d.ApproxEqual(samples[0].Weights[1], 0.25, 1e-6) {
		t.Fatalf("weights at t=0.5 = %v, want [0.5, 0.25]", samples[0].Weights)
	}

	// Pre-allocated buffer reuse check:
	scratch := make([]math3d.Vec3, 0, 4)
	morphed := gltf.EvaluateMorphPositions(prim, samples[0].Weights, scratch)
	// Delta = 0.5*(1,0,0) + 0.25*(0,2,0.5) = (0.5, 0.5, 0.125)
	wantDelta := math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.125}
	for i, base := range prim.Positions {
		want := base.Add(wantDelta)
		if !morphed[i].ApproxEqual(want, 1e-6) {
			t.Fatalf("morphed[%d] = %v, want %v", i, morphed[i], want)
		}
	}

	// Verify morphed normals at weight [1, 0]: (0,0,1) + (1,0,0) normalized -> (1/sqrt2, 0, 1/sqrt2)
	morphedNorms := gltf.EvaluateMorphNormals(prim, []float32{1, 0}, nil)
	invSqrt2 := float32(1 / math.Sqrt2)
	wantNorm := math3d.Vec3{X: invSqrt2, Y: 0, Z: invSqrt2}
	for i, n := range morphedNorms {
		if !n.ApproxEqual(wantNorm, 1e-5) {
			t.Fatalf("morphedNorm[%d] = %v, want %v", i, n, wantNorm)
		}
	}

	// Apply animation at t=1.5 -> weights = [0.5, 0.75]
	doc.Animations[0].Apply(doc, 1.5)
	if len(doc.Nodes[0].Weights) != 2 ||
		!math3d.ApproxEqual(doc.Nodes[0].Weights[0], 0.5, 1e-6) ||
		!math3d.ApproxEqual(doc.Nodes[0].Weights[1], 0.75, 1e-6) {
		t.Fatalf("applied node weights = %v, want [0.5, 0.75]", doc.Nodes[0].Weights)
	}
}

func makePNGBytes(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := range 2 {
		for x := range 2 {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func makeJPEGBytes(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := range 2 {
		for x := range 2 {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return buf.Bytes()
}

func TestTextureTransformMaterialVariantsUnlitAndImages(t *testing.T) {
	pngRed := makePNGBytes(t, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	pngGreen := makePNGBytes(t, color.RGBA{R: 0, G: 255, B: 0, A: 255})
	jpgBlue := makeJPEGBytes(t, color.RGBA{R: 0, G: 0, B: 255, A: 255})

	var bin bytes.Buffer
	imgOff := bin.Len()
	bin.Write(pngRed)
	imgLen := bin.Len() - imgOff
	pad4(&bin, 0)

	posOff := appendFloat32s(&bin, 0, 0, 0, 1, 0, 0, 0, 1, 0)
	posLen := bin.Len() - posOff

	raw := map[string]any{
		"asset":          map[string]any{"version": "2.0"},
		"extensionsUsed": []string{"KHR_texture_transform", "KHR_materials_variants", "KHR_materials_unlit"},
		"extensions": map[string]any{
			"KHR_materials_variants": map[string]any{
				"variants": []any{
					map[string]any{"name": "Midnight"},
					map[string]any{"name": "Beach"},
					map[string]any{"name": "UnmappedVariant"},
				},
			},
		},
		"buffers": []any{
			map[string]any{"byteLength": bin.Len(), "uri": "data.bin"},
		},
		"bufferViews": []any{
			map[string]any{"buffer": 0, "byteOffset": imgOff, "byteLength": imgLen},
			map[string]any{"buffer": 0, "byteOffset": posOff, "byteLength": posLen},
		},
		"accessors": []any{
			map[string]any{"bufferView": 1, "componentType": 5126, "count": 3, "type": "VEC3"},
		},
		"samplers": []any{
			map[string]any{"magFilter": 9729, "minFilter": 9987, "wrapS": 33071},
		},
		"images": []any{
			map[string]any{"name": "BufPNG", "bufferView": 0, "mimeType": "image/png"},
			map[string]any{"name": "DataURIPNG", "uri": "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngGreen)},
			map[string]any{"name": "ExtJPG", "uri": "textures/blue.jpg"},
		},
		"textures": []any{
			map[string]any{"sampler": 0, "source": 0},
		},
		"materials": []any{
			map[string]any{
				"name":        "DefaultMat",
				"alphaMode":   "MASK",
				"alphaCutoff": 0.25,
				"doubleSided": true,
				"pbrMetallicRoughness": map[string]any{
					"baseColorTexture": map[string]any{
						"index":    0,
						"texCoord": 0,
						"extensions": map[string]any{
							"KHR_texture_transform": map[string]any{
								"offset":   []float32{0.1, 0.2},
								"rotation": float32(math.Pi / 2),
								"scale":    []float32{2.0, 3.0},
								"texCoord": 1,
							},
						},
					},
				},
				"normalTexture":    map[string]any{"index": 0, "scale": 0.75},
				"occlusionTexture": map[string]any{"index": 0, "strength": 0.5},
			},
			map[string]any{
				"name": "MidnightUnlit",
				"extensions": map[string]any{
					"KHR_materials_unlit": map[string]any{},
				},
			},
			map[string]any{
				"name": "BeachMat",
			},
		},
		"meshes": []any{
			map[string]any{
				"primitives": []any{
					map[string]any{
						"attributes": map[string]int{"POSITION": 0},
						"material":   0,
						"extensions": map[string]any{
							"KHR_materials_variants": map[string]any{
								"mappings": []any{
									map[string]any{"material": 1, "variants": []int{0}},
									map[string]any{"material": 2, "variants": []int{1}},
								},
							},
						},
					},
				},
			},
		},
	}

	jsonBytes, _ := json.Marshal(raw)
	fsys := fstest.MapFS{
		"scene.gltf":        {Data: jsonBytes},
		"data.bin":          {Data: bin.Bytes()},
		"textures/blue.jpg": {Data: jpgBlue},
	}
	doc, err := gltf.LoadFS(fsys, "scene.gltf")
	if err != nil {
		t.Fatalf("LoadFS failed: %v", err)
	}

	// Verify KHR_texture_transform exact T * R * S order:
	// u=0.5, v=0.25 -> scale (2,3) -> (1.0, 0.75)
	// rotate by +pi/2 (cos=0, sin=1): u' = 0*1 + 1*0.75 = 0.75, v' = -1*1 + 0*0.75 = -1.0
	// offset (+0.1, +0.2): (0.85, -0.8)
	bct := doc.Materials[0].BaseColorTexture
	if bct == nil || bct.Transform == nil {
		t.Fatalf("missing BaseColorTexture or Transform")
	}
	if bct.EffectiveTexCoord() != 1 {
		t.Fatalf("EffectiveTexCoord = %d, want 1", bct.EffectiveTexCoord())
	}
	uOut, vOut := bct.Transform.ApplyUV(0.5, 0.25)
	if !math3d.ApproxEqual(uOut, 0.85, 1e-5) || !math3d.ApproxEqual(vOut, -0.8, 1e-5) {
		t.Fatalf("ApplyUV(0.5, 0.25) = (%v, %v), want (0.85, -0.8)", uOut, vOut)
	}
	// Verify Matrix() matches ApplyUV!
	matUV := bct.Transform.Matrix().MulVec3(math3d.Vec3{X: 0.5, Y: 0.25, Z: 1})
	if !math3d.ApproxEqual(matUV.X, uOut, 1e-5) || !math3d.ApproxEqual(matUV.Y, vOut, 1e-5) {
		t.Fatalf("Matrix().MulVec3 = (%v, %v), want (%v, %v)", matUV.X, matUV.Y, uOut, vOut)
	}

	// Verify material properties & KHR_materials_unlit:
	if doc.Materials[0].AlphaMode != gltf.AlphaMask || doc.Materials[0].AlphaCutoff != 0.25 || !doc.Materials[0].DoubleSided {
		t.Fatalf("unexpected material 0 flags: %+v", doc.Materials[0])
	}
	if doc.Materials[0].NormalTexture.Scale != 0.75 || doc.Materials[0].OcclusionTexture.Strength != 0.5 {
		t.Fatalf("unexpected normal/occlusion factors")
	}
	if doc.Materials[0].Unlit || !doc.Materials[1].Unlit {
		t.Fatalf("unexpected Unlit flags: mat0=%v mat1=%v", doc.Materials[0].Unlit, doc.Materials[1].Unlit)
	}

	// Verify sampler defaults (wrapT omitted -> WrapRepeat):
	if doc.Samplers[0].WrapS != gltf.WrapClampToEdge || doc.Samplers[0].WrapT != gltf.WrapRepeat {
		t.Fatalf("unexpected sampler wraps: %+v", doc.Samplers[0])
	}

	// Verify KHR_materials_variants switching:
	prim := doc.Meshes[0].Primitives[0]
	if got := doc.MaterialForPrimitive(prim, -1); got != 0 {
		t.Fatalf("variant -1 -> material %d, want 0", got)
	}
	if got := doc.MaterialForPrimitive(prim, 0); got != 1 {
		t.Fatalf("variant 0 (Midnight) -> material %d, want 1", got)
	}
	if got := doc.MaterialForPrimitive(prim, 1); got != 2 {
		t.Fatalf("variant 1 (Beach) -> material %d, want 2", got)
	}
	if got := doc.MaterialForPrimitive(prim, 2); got != 0 {
		t.Fatalf("variant 2 (unmapped) -> material %d, want fallback 0", got)
	}
	if got := doc.MaterialForPrimitive(prim, 99); got != 0 {
		t.Fatalf("variant 99 (out of range) -> material %d, want fallback 0", got)
	}

	// Verify DecodeImage across BufferView PNG, DataURI PNG, and external FS JPEG:
	for i := range 3 {
		img, err := doc.DecodeImage(i)
		if err != nil {
			t.Fatalf("DecodeImage(%d): %v", i, err)
		}
		if img.Bounds().Dx() != 2 || img.Bounds().Dy() != 2 {
			t.Fatalf("image %d size = %v, want 2x2", i, img.Bounds())
		}
		r, g, b, _ := img.At(0, 0).RGBA()
		switch i {
		case 0:
			if r>>8 != 255 || g>>8 != 0 || b>>8 != 0 {
				t.Fatalf("img 0 pixel = (%d,%d,%d), want red", r>>8, g>>8, b>>8)
			}
		case 1:
			if r>>8 != 0 || g>>8 != 255 || b>>8 != 0 {
				t.Fatalf("img 1 pixel = (%d,%d,%d), want green", r>>8, g>>8, b>>8)
			}
		case 2:
			if b>>8 < 240 || r>>8 > 15 || g>>8 > 15 {
				t.Fatalf("img 2 pixel = (%d,%d,%d), want blue", r>>8, g>>8, b>>8)
			}
		}
	}
}

func TestSkinnedAndAnimatedWithExtrasAndAllInterpolations(t *testing.T) {
	var bin bytes.Buffer

	// 2 vertices
	posOff := appendFloat32s(&bin, 0, 0, 0, 0, 2, 0)
	posLen := bin.Len() - posOff

	// JOINTS_0 (VEC4 UNSIGNED_BYTE)
	jointsOff := bin.Len()
	bin.Write([]byte{0, 1, 0, 0, 1, 0, 0, 0})
	jointsLen := bin.Len() - jointsOff

	// WEIGHTS_0 (VEC4 UNSIGNED_BYTE normalized)
	weightsOff := bin.Len()
	bin.Write([]byte{255, 0, 0, 0, 128, 127, 0, 0})
	weightsLen := bin.Len() - weightsOff

	// InverseBindMatrices for 2 joints
	ibm0 := math3d.Mat4Identity()
	ibm1 := math3d.Translate(math3d.Vec3{X: 0, Y: -1, Z: 0})
	ibmOff := appendFloat32s(&bin, ibm0[:]...)
	appendFloat32s(&bin, ibm1[:]...)
	ibmLen := bin.Len() - ibmOff

	// Animation times: [0.0, 2.0]
	timeOff := appendFloat32s(&bin, 0.0, 2.0)
	timeLen := bin.Len() - timeOff

	// Channel 0: CUBICSPLINE translation on node 1 (inTangent, value, outTangent) x 2 keys
	// Key 0 (t=0): in=(0,0,0), val=(0,1,0), out=(0,2,0)
	// Key 1 (t=2): in=(0,2,0), val=(0,5,0), out=(0,0,0)
	// Since slope is constant 2 over dt=2 and v1-v0=4, p(t) = 1 + 2*t -> at t=1 (s=0.5), y = 3.0!
	cubicOff := appendFloat32s(&bin,
		0, 0, 0, 0, 1, 0, 0, 2, 0,
		0, 2, 0, 0, 5, 0, 0, 0, 0,
	)
	cubicLen := bin.Len() - cubicOff

	// Channel 1: LINEAR rotation on node 1 from Identity to RotY(90 deg) stored with negated sign (-q)
	// to verify shortest-path Slerp!
	q0 := math3d.QuatIdentity()
	q1 := math3d.QuatFromAxisAngle(math3d.Vec3Up(), math.Pi/2)
	rotOff := appendFloat32s(&bin,
		q0.X, q0.Y, q0.Z, q0.W,
		-q1.X, -q1.Y, -q1.Z, -q1.W,
	)
	rotLen := bin.Len() - rotOff

	// Channel 2: STEP scale on node 1 from (1,1,1) to (2,3,4)
	scaleOff := appendFloat32s(&bin, 1, 1, 1, 2, 3, 4)
	scaleLen := bin.Len() - scaleOff

	raw := map[string]any{
		"asset":  map[string]any{"version": "2.0"},
		"extras": map[string]any{"author": "nsgo"},
		"buffers": []any{
			map[string]any{"byteLength": bin.Len()},
		},
		"bufferViews": []any{
			map[string]any{"buffer": 0, "byteOffset": posOff, "byteLength": posLen},
			map[string]any{"buffer": 0, "byteOffset": jointsOff, "byteLength": jointsLen},
			map[string]any{"buffer": 0, "byteOffset": weightsOff, "byteLength": weightsLen},
			map[string]any{"buffer": 0, "byteOffset": ibmOff, "byteLength": ibmLen},
			map[string]any{"buffer": 0, "byteOffset": timeOff, "byteLength": timeLen},
			map[string]any{"buffer": 0, "byteOffset": cubicOff, "byteLength": cubicLen},
			map[string]any{"buffer": 0, "byteOffset": rotOff, "byteLength": rotLen},
			map[string]any{"buffer": 0, "byteOffset": scaleOff, "byteLength": scaleLen},
		},
		"accessors": []any{
			map[string]any{"bufferView": 0, "componentType": 5126, "count": 2, "type": "VEC3"},
			map[string]any{"bufferView": 1, "componentType": 5121, "count": 2, "type": "VEC4"},
			map[string]any{"bufferView": 2, "componentType": 5121, "normalized": true, "count": 2, "type": "VEC4"},
			map[string]any{"bufferView": 3, "componentType": 5126, "count": 2, "type": "MAT4"},
			map[string]any{"bufferView": 4, "componentType": 5126, "count": 2, "type": "SCALAR"},
			map[string]any{"bufferView": 5, "componentType": 5126, "count": 6, "type": "VEC3"},
			map[string]any{"bufferView": 6, "componentType": 5126, "count": 2, "type": "VEC4"},
			map[string]any{"bufferView": 7, "componentType": 5126, "count": 2, "type": "VEC3"},
		},
		"meshes": []any{
			map[string]any{
				"primitives": []any{
					map[string]any{
						"attributes": map[string]int{
							"POSITION":  0,
							"JOINTS_0":  1,
							"WEIGHTS_0": 2,
						},
					},
				},
			},
		},
		"nodes": []any{
			map[string]any{"name": "RootJoint", "children": []int{1}, "extras": map[string]any{"boneID": 100}},
			map[string]any{"name": "ChildJoint", "translation": []float32{0, 1, 0}},
			map[string]any{"name": "SkinnedMesh", "mesh": 0, "skin": 0},
		},
		"skins": []any{
			map[string]any{
				"name":                "Armature",
				"joints":              []int{0, 1},
				"skeleton":            0,
				"inverseBindMatrices": 3,
			},
		},
		"animations": []any{
			map[string]any{
				"name":   "Walk",
				"extras": map[string]any{"loop": true},
				"samplers": []any{
					map[string]any{"input": 4, "output": 5, "interpolation": "CUBICSPLINE"},
					map[string]any{"input": 4, "output": 6, "interpolation": "LINEAR"},
					map[string]any{"input": 4, "output": 7, "interpolation": "STEP"},
				},
				"channels": []any{
					map[string]any{"sampler": 0, "target": map[string]any{"node": 1, "path": "translation"}},
					map[string]any{"sampler": 1, "target": map[string]any{"node": 1, "path": "rotation"}},
					map[string]any{"sampler": 2, "target": map[string]any{"node": 1, "path": "scale"}},
				},
			},
		},
	}

	jsonBytes, _ := json.Marshal(raw)
	doc, err := gltf.Load(buildGLB(jsonBytes, bin.Bytes()), nil)
	if err != nil {
		t.Fatalf("Load skinned GLB: %v", err)
	}

	// Verify skin & vertex skinning attributes:
	if len(doc.Skins) != 1 {
		t.Fatalf("skins len = %d, want 1", len(doc.Skins))
	}
	skin := doc.Skins[0]
	if skin.Skeleton != 0 || len(skin.Joints) != 2 || skin.Joints[0] != 0 || skin.Joints[1] != 1 {
		t.Fatalf("unexpected skin joints/skeleton: %+v", skin)
	}
	if !skin.InverseBindMatrices[0].ApproxEqual(ibm0, 1e-6) || !skin.InverseBindMatrices[1].ApproxEqual(ibm1, 1e-6) {
		t.Fatalf("unexpected IBMs: %+v", skin.InverseBindMatrices)
	}

	prim := doc.Meshes[0].Primitives[0]
	if prim.Joints0[0] != [4]uint16{0, 1, 0, 0} || prim.Joints0[1] != [4]uint16{1, 0, 0, 0} {
		t.Fatalf("unexpected Joints0: %v", prim.Joints0)
	}
	if !prim.Weights0[0].ApproxEqual(math3d.Vec4{X: 1, Y: 0, Z: 0, W: 0}, 1e-5) ||
		!prim.Weights0[1].ApproxEqual(math3d.Vec4{X: 128.0 / 255.0, Y: 127.0 / 255.0, Z: 0, W: 0}, 1e-5) {
		t.Fatalf("unexpected Weights0: %v", prim.Weights0)
	}

	// Verify extras preserved on document, node, and animation:
	var animExtras struct {
		Loop bool `json:"loop"`
	}
	if err := json.Unmarshal(doc.Animations[0].Extras, &animExtras); err != nil || !animExtras.Loop {
		t.Fatalf("unexpected animation extras: %s (err=%v)", string(doc.Animations[0].Extras), err)
	}
	var nodeExtras struct {
		BoneID int `json:"boneID"`
	}
	if err := json.Unmarshal(doc.Nodes[0].Extras, &nodeExtras); err != nil || nodeExtras.BoneID != 100 {
		t.Fatalf("unexpected node extras: %s (err=%v)", string(doc.Nodes[0].Extras), err)
	}

	// Verify animation sampling at t = 1.0:
	anim := &doc.Animations[0]
	samples := anim.Sample(1.0)
	if len(samples) != 3 {
		t.Fatalf("samples len = %d, want 3", len(samples))
	}
	// CUBICSPLINE translation at t=1.0 -> (0, 3, 0)
	if !samples[0].Translation.ApproxEqual(math3d.Vec3{X: 0, Y: 3, Z: 0}, 1e-5) {
		t.Fatalf("cubic translation at t=1 = %v, want (0,3,0)", samples[0].Translation)
	}
	// LINEAR rotation at t=1.0 -> RotY(45 deg) via shortest path
	wantQHalf := math3d.QuatFromAxisAngle(math3d.Vec3Up(), math.Pi/4)
	if !samples[1].Rotation.ApproxEqual(wantQHalf, 1e-5) {
		t.Fatalf("slerp rotation at t=1 = %v, want %v", samples[1].Rotation, wantQHalf)
	}
	// STEP scale at t=1.0 (before t=2.0) -> still (1, 1, 1)
	if !samples[2].Scale.ApproxEqual(math3d.Vec3One(), 1e-6) {
		t.Fatalf("step scale at t=1 = %v, want (1,1,1)", samples[2].Scale)
	}
	// STEP scale at t=2.0 -> (2, 3, 4)
	atEnd := anim.Sample(2.0)
	if !atEnd[2].Scale.ApproxEqual(math3d.Vec3{X: 2, Y: 3, Z: 4}, 1e-6) {
		t.Fatalf("step scale at t=2 = %v, want (2,3,4)", atEnd[2].Scale)
	}
}
