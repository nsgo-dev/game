package gltf_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/gltf"
)

// triangleDoc returns a one-triangle glTF document whose buffer holds three positions (bufferView 0), three uint16 indices (bufferView 1) and the extra bytes extra appends (bufferView 2). edit may change the JSON before it is encoded.
func triangleDoc(t *testing.T, positions [9]float32, indices [3]uint16, extra func(buf *bytes.Buffer), edit func(doc map[string]any)) []byte {
	t.Helper()
	var buf bytes.Buffer
	appendFloat32s(&buf, positions[:]...)
	appendUint16s(&buf, indices[:]...)
	pad4(&buf, 0)
	extraOff := buf.Len()
	if extra != nil {
		extra(&buf)
	}
	extraLen := buf.Len() - extraOff
	views := []any{
		map[string]any{"buffer": 0, "byteOffset": 0, "byteLength": 36},
		map[string]any{"buffer": 0, "byteOffset": 36, "byteLength": 6},
	}
	if extraLen > 0 {
		views = append(views, map[string]any{"buffer": 0, "byteOffset": extraOff, "byteLength": extraLen})
	}
	doc := map[string]any{
		"asset":       map[string]any{"version": "2.0"},
		"buffers":     []any{map[string]any{"byteLength": buf.Len(), "uri": "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())}},
		"bufferViews": views,
		"accessors": []any{
			map[string]any{"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3"},
			map[string]any{"bufferView": 1, "componentType": 5123, "count": 3, "type": "SCALAR"},
		},
		"meshes": []any{map[string]any{"primitives": []any{map[string]any{"attributes": map[string]any{"POSITION": 0}, "indices": 1}}}},
		"nodes":  []any{map[string]any{"mesh": 0}},
		"scenes": []any{map[string]any{"nodes": []any{0}}},
	}
	if edit != nil {
		edit(doc)
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

var unitTri = [9]float32{0, 0, 0, 1, 0, 0, 0, 1, 0}

func wantLoadError(t *testing.T, data []byte, substr string) {
	t.Helper()
	_, err := gltf.Load(data, nil)
	if err == nil || !strings.Contains(err.Error(), substr) {
		t.Fatalf("Load error = %v, want one mentioning %q", err, substr)
	}
}

func TestLoadValidatesPrimitiveData(t *testing.T) {
	if _, err := gltf.Load(triangleDoc(t, unitTri, [3]uint16{0, 1, 2}, nil, nil), nil); err != nil {
		t.Fatalf("valid triangle: %v", err)
	}
	wantLoadError(t, triangleDoc(t, unitTri, [3]uint16{0, 1, 3}, nil, nil), "past the 3 vertices")

	nan := float32(math.NaN())
	wantLoadError(t, triangleDoc(t, [9]float32{0, 0, 0, nan, 0, 0, 0, 1, 0}, [3]uint16{0, 1, 2}, nil, nil), "POSITION element 1 is not finite")

	// NORMAL with two elements against three positions.
	wantLoadError(t, triangleDoc(t, unitTri, [3]uint16{0, 1, 2}, func(b *bytes.Buffer) {
		appendFloat32s(b, 0, 0, 1, 0, 0, 1)
	}, func(doc map[string]any) {
		doc["accessors"] = append(doc["accessors"].([]any), map[string]any{"bufferView": 2, "componentType": 5126, "count": 2, "type": "VEC3"})
		prim := doc["meshes"].([]any)[0].(map[string]any)["primitives"].([]any)[0].(map[string]any)
		prim["attributes"].(map[string]any)["NORMAL"] = 2
	}), "attribute NORMAL has 2 elements, want 3")

	// A morph target with the wrong count, and mesh weights that do not match the target count.
	morph := func(count int, weights []float32) func(doc map[string]any) {
		return func(doc map[string]any) {
			doc["accessors"] = append(doc["accessors"].([]any), map[string]any{"bufferView": 2, "componentType": 5126, "count": count, "type": "VEC3"})
			mesh := doc["meshes"].([]any)[0].(map[string]any)
			mesh["primitives"].([]any)[0].(map[string]any)["targets"] = []any{map[string]any{"POSITION": 2}}
			if weights != nil {
				mesh["weights"] = weights
			}
		}
	}
	three := func(b *bytes.Buffer) { appendFloat32s(b, make([]float32, 9)...) }
	wantLoadError(t, triangleDoc(t, unitTri, [3]uint16{0, 1, 2}, func(b *bytes.Buffer) { appendFloat32s(b, make([]float32, 6)...) }, morph(2, nil)), "target 0 POSITION has 2 elements")
	wantLoadError(t, triangleDoc(t, unitTri, [3]uint16{0, 1, 2}, three, morph(3, []float32{0.5, 0.5})), "2 weights for 1 morph targets")
	if _, err := gltf.Load(triangleDoc(t, unitTri, [3]uint16{0, 1, 2}, three, morph(3, []float32{0.5})), nil); err != nil {
		t.Fatalf("valid morph target: %v", err)
	}
}

func TestLoadValidatesSparseOrderAndJointRange(t *testing.T) {
	// Sparse indices 2 then 1 are not strictly increasing.
	sparse := triangleDoc(t, unitTri, [3]uint16{0, 1, 2}, func(b *bytes.Buffer) {
		appendUint16s(b, 2, 1)
		pad4(b, 0)
		appendFloat32s(b, 5, 5, 5, 6, 6, 6)
	}, func(doc map[string]any) {
		views := doc["bufferViews"].([]any)
		ev := views[2].(map[string]any)
		off := ev["byteOffset"].(int)
		views[2] = map[string]any{"buffer": 0, "byteOffset": off, "byteLength": 4}
		doc["bufferViews"] = append(views, map[string]any{"buffer": 0, "byteOffset": off + 4, "byteLength": 24})
		acc := doc["accessors"].([]any)[0].(map[string]any)
		acc["sparse"] = map[string]any{"count": 2, "indices": map[string]any{"bufferView": 2, "componentType": 5123}, "values": map[string]any{"bufferView": 3}}
	})
	wantLoadError(t, sparse, "strictly increase")

	// JOINTS_0 references joint 3 of a one-joint skin.
	joints := triangleDoc(t, unitTri, [3]uint16{0, 1, 2}, func(b *bytes.Buffer) {
		b.Write([]byte{0, 0, 0, 0, 0, 0, 0, 0, 3, 0, 0, 0})
	}, func(doc map[string]any) {
		doc["accessors"] = append(doc["accessors"].([]any), map[string]any{"bufferView": 2, "componentType": 5121, "count": 3, "type": "VEC4"})
		prim := doc["meshes"].([]any)[0].(map[string]any)["primitives"].([]any)[0].(map[string]any)
		prim["attributes"].(map[string]any)["JOINTS_0"] = 2
		doc["nodes"] = []any{map[string]any{"mesh": 0, "skin": 0}, map[string]any{}}
		doc["skins"] = []any{map[string]any{"joints": []any{1}}}
	})
	wantLoadError(t, joints, "joint 3 is past skin 0's 1 joints")
}

func TestLoadEnforcesMinVersionAndSingleJSONChunk(t *testing.T) {
	withMin := func(min, version string) []byte {
		return triangleDoc(t, unitTri, [3]uint16{0, 1, 2}, nil, func(doc map[string]any) {
			doc["asset"] = map[string]any{"version": version, "minVersion": min}
		})
	}
	if _, err := gltf.Load(withMin("2.0", "2.0"), nil); err != nil {
		t.Fatalf("minVersion 2.0: %v", err)
	}
	wantLoadError(t, withMin("2.1", "2.1"), "requires glTF 2.1")
	wantLoadError(t, withMin("2.1", "2.0"), "newer than asset.version")
	wantLoadError(t, withMin("two", "2.0"), "invalid asset.minVersion")

	jsonDoc := triangleDoc(t, unitTri, [3]uint16{0, 1, 2}, nil, nil)
	glb := buildGLB(jsonDoc, nil, struct {
		chunkType uint32
		payload   []byte
	}{chunkType: 0x4E4F534A, payload: []byte(`{"asset":{"version":"2.0"}}`)})
	wantLoadError(t, glb, "second JSON chunk")
}

func TestShearedNodeMatrixIsMarkedNotTRS(t *testing.T) {
	data := triangleDoc(t, unitTri, [3]uint16{0, 1, 2}, nil, func(doc map[string]any) {
		doc["nodes"] = []any{map[string]any{"mesh": 0, "matrix": []float32{1, 0, 0, 0, 0.5, 1, 0, 0, 0, 0, 1, 0, 4, 5, 6, 1}}}
	})
	doc, err := gltf.Load(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	n := doc.Nodes[0]
	if !n.HasMatrix || !n.MatrixNotTRS {
		t.Fatalf("sheared matrix node: HasMatrix %v MatrixNotTRS %v, want both", n.HasMatrix, n.MatrixNotTRS)
	}
	if n.Translation.X != 4 || n.Translation.Y != 5 || n.Translation.Z != 6 {
		t.Fatalf("Translation = %v, want the matrix translation (4, 5, 6)", n.Translation)
	}
}
