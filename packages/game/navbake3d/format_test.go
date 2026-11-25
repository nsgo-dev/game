package navbake3d_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/navbake3d"
)

func readRamp(t testing.TB) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/ramp.navmesh.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseRefusesInvalidFiles(t *testing.T) {
	ramp := string(readRamp(t))
	cases := []struct {
		name, data, field string
	}{
		{"empty", "", ""},
		{"trailing document", ramp + "---\nversion: 1\n", ""},
		{"zero setting", strings.Replace(ramp, "cellSize: 0.3", "cellSize: 0", 1), "settings.cellSize"},
		{"infinite setting", strings.Replace(ramp, "cellHeight: 0.2", "cellHeight: .inf", 1), "settings.cellHeight"},
		{"few verts per poly", strings.Replace(ramp, "maxVertsPerPoly: 6", "maxVertsPerPoly: 2", 1), "settings.maxVertsPerPoly"},
		{"no layers", strings.Replace(ramp, "layerMask: 4294967295", "layerMask: 0", 1), "settings.layerMask"},
		{"agent slope", strings.Replace(ramp, "maxSlope: 45", "maxSlope: 90", 1), "meshes[0].agent.maxSlope"},
		{"agent name", strings.Replace(ramp, "name: humanoid", "name: ' humanoid'", 1), "meshes[0].agent.name"},
		{"nan vertex", strings.Replace(ramp, "- [0, 0, 4]", "- [0, .nan, 4]", 1), "meshes[0].vertices[1]"},
		{"short vertex", strings.Replace(ramp, "- [0, 0, 4]", "- [0, 4]", 1), ""},
		{"vertex out of range", strings.Replace(ramp, "vertices: [3, 2, 4, 5]", "vertices: [3, 2, 4, 6]", 1), "meshes[0].polygons[1].vertices[3]"},
		{"repeated vertex", strings.Replace(ramp, "vertices: [3, 2, 4, 5]", "vertices: [3, 2, 4, 3]", 1), "meshes[0].polygons[1].vertices[3]"},
		{"two vertices", strings.Replace(ramp, "vertices: [3, 2, 4, 5]", "vertices: [3, 2]", 1), "meshes[0].polygons[1].vertices"},
		{"triangle out of range", strings.Replace(ramp, "- [0, 2, 3]", "- [0, 2, 4]", 1), "meshes[0].polygons[1].triangles[1]"},
		{"degenerate triangle", strings.Replace(ramp, "- [0, 2, 3]", "- [0, 2, 2]", 1), "meshes[0].polygons[1].triangles[1]"},
		{"no triangles", strings.Replace(ramp, "        triangles:\n          - [0, 1, 2]\n          - [0, 2, 3]\n", "        triangles: []\n", 1), "meshes[0].polygons[1].triangles"},
		{"nan detail", strings.Replace(ramp, "- [2, 0.75, 2]", "- [2, .nan, 2]", 1), "meshes[0].polygons[0].detail[0]"},
		{"duplicate agent", strings.Replace(ramp, "meshes:\n", "meshes:\n  - agent: {name: humanoid, radius: 1, height: 1, maxSlope: 1, maxClimb: 1}\n", 1), "meshes[1].agent.name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := navbake3d.ParseWith([]byte(c.data), "level.navmesh.yaml")
			if err == nil {
				t.Fatal("Parse accepted the file")
			}
			le, ok := loaderr.As(err)
			if !ok {
				t.Fatalf("error %v is not a *loaderr.Error", err)
			}
			if c.field != "" && le.Field != c.field {
				t.Fatalf("error field %q, want %q (%v)", le.Field, c.field, err)
			}
			if c.data != "" && le.File != "level.navmesh.yaml" {
				t.Fatalf("error names file %q", le.File)
			}
		})
	}
	if err := navbake3d.Validate(nil); err == nil {
		t.Fatal("Validate(nil) passed")
	}
	if _, err := navbake3d.Encode(&navbake3d.File{Version: 2}); err == nil {
		t.Fatal("Encode wrote an unsupported version")
	}
}

func TestLoadFS(t *testing.T) {
	f, err := navbake3d.LoadFS(os.DirFS("testdata"), "ramp.navmesh.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := f.Mesh("humanoid"); !ok || len(m.Polygons) != 2 {
		t.Fatalf("Mesh(humanoid) = %+v, %v", m, ok)
	}
	if _, err := navbake3d.LoadFS(os.DirFS("testdata"), "missing.navmesh.yaml"); err == nil || !strings.Contains(err.Error(), "missing.navmesh.yaml") {
		t.Fatalf("LoadFS of a missing file: %v", err)
	}
	var nilFile *navbake3d.File
	if _, ok := nilFile.Mesh("humanoid"); ok {
		t.Fatal("a nil file has a mesh")
	}
}

func TestNegativeZeroRoundTrips(t *testing.T) {
	f, err := navbake3d.Parse(readRamp(t))
	if err != nil {
		t.Fatal(err)
	}
	negZero := float32(0)
	negZero = -negZero
	f.Meshes[0].Vertices[0][1] = negZero
	data, err := navbake3d.Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, readRamp(t)) {
		// A -0 a caller writes encodes as "-0", which reads back as 0; Bake never writes one.
		g, err := navbake3d.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		if again, _ := navbake3d.Encode(g); !bytes.Equal(again, readRamp(t)) {
			t.Fatal("a file with -0 does not settle after one round trip")
		}
	}
}

func FuzzParse(f *testing.F) {
	f.Add(readRamp(f))
	f.Add([]byte("version: 1\nsettings: {cellSize: 1, cellHeight: 1, minRegionArea: 1, maxEdgeLength: 1, maxSimplificationError: 1, maxVertsPerPoly: 3, detailSampleDistance: 1, detailSampleMaxError: 1, layerMask: 1}\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		file, err := navbake3d.Parse(data)
		if err != nil {
			return
		}
		out, err := navbake3d.Encode(file)
		if err != nil {
			t.Fatalf("Encode of a parsed file: %v", err)
		}
		again, err := navbake3d.Parse(out)
		if err != nil {
			t.Fatalf("Parse of Encode's output: %v\n%s", err, out)
		}
		out2, err := navbake3d.Encode(again)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out, out2) {
			t.Fatalf("encoding is not stable:\n%s\n---\n%s", out, out2)
		}
		for i := range again.Meshes {
			again.Meshes[i].NavMesh()
		}
	})
}

func BenchmarkBakeReferenceLevel(b *testing.B) {
	g := referenceLevel(b)
	s := referenceSettings
	s.LayerMask = 1
	b.ReportAllocs()
	for b.Loop() {
		if _, err := navbake3d.BakeMesh(g, s, referenceAgent); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNavMeshFromBake(b *testing.B) {
	g := referenceLevel(b)
	m, err := navbake3d.BakeMesh(g, referenceSettings, referenceAgent)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		m.NavMesh()
	}
}
