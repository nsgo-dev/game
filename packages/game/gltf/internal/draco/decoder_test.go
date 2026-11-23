package draco

import (
	"encoding/json"
	"math"
	"os"
	"path"
	"testing"
)

// khronosStreams returns the Draco stream of every compressed primitive in a Khronos sample under the gltf package's testdata, keyed by model and primitive.
func khronosStreams(t testing.TB, model, gltfName string) [][]byte {
	t.Helper()
	dir := path.Join("..", "..", "testdata", "khronos", model, "glTF-Draco")
	raw, err := os.ReadFile(path.Join(dir, gltfName))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Buffers []struct {
			URI string `json:"uri"`
		} `json:"buffers"`
		BufferViews []struct {
			Buffer     int `json:"buffer"`
			ByteOffset int `json:"byteOffset"`
			ByteLength int `json:"byteLength"`
		} `json:"bufferViews"`
		Meshes []struct {
			Primitives []struct {
				Extensions struct {
					Draco *struct {
						BufferView int `json:"bufferView"`
					} `json:"KHR_draco_mesh_compression"`
				} `json:"extensions"`
			} `json:"primitives"`
		} `json:"meshes"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var out [][]byte
	for _, m := range doc.Meshes {
		for _, p := range m.Primitives {
			if p.Extensions.Draco == nil {
				continue
			}
			bv := doc.BufferViews[p.Extensions.Draco.BufferView]
			bin, err := os.ReadFile(path.Join(dir, doc.Buffers[bv.Buffer].URI))
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, bin[bv.ByteOffset:bv.ByteOffset+bv.ByteLength])
		}
	}
	return out
}

func allKhronosStreams(t testing.TB) [][]byte {
	var out [][]byte
	for _, m := range []string{"Box", "Avocado", "CesiumMilkTruck", "MorphPrimitivesTest", "RiggedSimple", "SunglassesKhronos"} {
		out = append(out, khronosStreams(t, m, m+".gltf")...)
	}
	return out
}

func TestDecodeKhronosBox(t *testing.T) {
	streams := khronosStreams(t, "Box", "Box.gltf")
	if len(streams) != 1 {
		t.Fatalf("got %d streams, want 1", len(streams))
	}
	mesh, err := Decode(streams[0], Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(mesh.Faces) != 36 || mesh.NumPoints != 24 {
		t.Fatalf("got %d indices and %d points, want 36 and 24", len(mesh.Faces), mesh.NumPoints)
	}
	// The Box glTF maps POSITION to unique id 1 and NORMAL to 0.
	pos, ok := mesh.Attribute(1)
	if !ok || pos.NumComponents() != 3 || !pos.IsFloat() {
		t.Fatalf("POSITION attribute missing or not a float VEC3")
	}
	nrm, ok := mesh.Attribute(0)
	if !ok || nrm.NumComponents() != 3 {
		t.Fatalf("NORMAL attribute missing")
	}
	for p := range mesh.NumPoints {
		var length float64
		for c := range 3 {
			v, err := pos.Value(p, c)
			if err != nil {
				t.Fatal(err)
			}
			// Every corner of the unit box lies at ±0.5 on each axis, up to quantization.
			if math.Abs(math.Abs(v)-0.5) > 1e-3 {
				t.Fatalf("point %d component %d = %v, want ±0.5", p, c, v)
			}
			n, _ := nrm.Value(p, c)
			length += n * n
		}
		if math.Abs(length-1) > 1e-3 {
			t.Fatalf("normal of point %d has length² %v, want 1", p, length)
		}
	}
	if _, ok := mesh.Attribute(7); ok {
		t.Fatal("Attribute(7) found an attribute the stream does not have")
	}
	if _, err := pos.Value(mesh.NumPoints, 0); err == nil {
		t.Fatal("Value past the last point returned no error")
	}
}

func TestDecodeKhronosStreams(t *testing.T) {
	for i, s := range allKhronosStreams(t) {
		mesh, err := Decode(s, Limits{})
		if err != nil {
			t.Fatalf("stream %d: %v", i, err)
		}
		for _, f := range mesh.Faces {
			if int(f) >= mesh.NumPoints {
				t.Fatalf("stream %d: face index %d out of %d points", i, f, mesh.NumPoints)
			}
		}
	}
}

func TestDecodeEnforcesLimits(t *testing.T) {
	s := khronosStreams(t, "Box", "Box.gltf")[0]
	if _, err := Decode(s, Limits{MaxFaces: 11}); err == nil {
		t.Fatal("12 faces under a limit of 11: want an error")
	}
	if _, err := Decode(s, Limits{MaxPoints: 23}); err == nil {
		t.Fatal("24 points under a limit of 23: want an error")
	}
	if _, err := Decode(s, Limits{MaxFaces: 12, MaxPoints: 24}); err != nil {
		t.Fatalf("exact limits: %v", err)
	}
}

func TestDecodeRejectsMalformed(t *testing.T) {
	for i, s := range allKhronosStreams(t) {
		full, err := Decode(s, Limits{})
		if err != nil {
			t.Fatal(err)
		}
		// Every proper prefix must fail cleanly: the decoder reads past no end and never panics. The limits are the ones the glTF loader derives from the accessors.
		limits := Limits{MaxFaces: len(full.Faces) / 3, MaxPoints: full.NumPoints}
		step := max(1, len(s)/128)
		for n := 0; n < len(s); n += step {
			if _, err := Decode(s[:n], limits); err == nil {
				t.Fatalf("stream %d truncated to %d of %d bytes decoded without error", i, n, len(s))
			}
		}
	}
	bad := append([]byte(nil), khronosStreams(t, "Box", "Box.gltf")[0]...)
	bad[5] = 1 // version 1.2
	if _, err := Decode(bad, Limits{}); err == nil {
		t.Fatal("bitstream 1.2: want an error")
	}
	bad[5], bad[7] = 2, 0 // a point cloud
	if _, err := Decode(bad, Limits{}); err == nil {
		t.Fatal("point cloud: want an error")
	}
	if _, err := Decode([]byte("DRAC0"), Limits{}); err == nil {
		t.Fatal("bad magic: want an error")
	}
}

// TestDecodeSurvivesBitFlips flips every bit of small real streams; the result may decode or fail, but must never panic or hang.
func TestDecodeSurvivesBitFlips(t *testing.T) {
	for _, s := range append(khronosStreams(t, "Box", "Box.gltf"), khronosStreams(t, "MorphPrimitivesTest", "MorphPrimitivesTest.gltf")...) {
		buf := append([]byte(nil), s...)
		for i := range buf {
			for bit := range 8 {
				buf[i] ^= 1 << bit
				_, _ = Decode(buf, Limits{MaxFaces: 1 << 12, MaxPoints: 1 << 12})
				buf[i] ^= 1 << bit
			}
		}
	}
}

func FuzzDecode(f *testing.F) {
	for _, s := range allKhronosStreams(f) {
		if len(s) <= 16<<10 {
			f.Add(s)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		mesh, err := Decode(data, Limits{MaxFaces: 1 << 14, MaxPoints: 1 << 14})
		if err != nil {
			return
		}
		for _, face := range mesh.Faces {
			if int(face) >= mesh.NumPoints {
				t.Fatalf("face index %d out of %d points", face, mesh.NumPoints)
			}
		}
		for id := range uint32(8) {
			if a, ok := mesh.Attribute(id); ok {
				for p := range min(mesh.NumPoints, 64) {
					for c := range a.NumComponents() {
						_, _ = a.Value(p, c)
					}
				}
			}
		}
	})
}

func BenchmarkDecode(b *testing.B) {
	streams := khronosStreams(b, "SunglassesKhronos", "SunglassesKhronos.gltf")
	var total int64
	for _, s := range streams {
		total += int64(len(s))
	}
	b.SetBytes(total)
	for b.Loop() {
		for _, s := range streams {
			if _, err := Decode(s, Limits{}); err != nil {
				b.Fatal(err)
			}
		}
	}
}
