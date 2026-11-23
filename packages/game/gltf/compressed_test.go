package gltf_test

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/gltf"
)

// The models under testdata/khronos are unmodified copies from KhronosGroup/glTF-Sample-Assets; each directory keeps the model's LICENSE.md. Their textures are left out because the loader never fetches images while loading.

// loadKhronos loads model's file under testdata/khronos/model/variant, with its glTF JSON rewritten by edit when edit is not nil.
func loadKhronos(t testing.TB, model, variant string, edit func(doc map[string]any)) *gltf.Document {
	t.Helper()
	fsys, _ := khronosFS(t, model+"/"+variant, model+".gltf", edit)
	doc, err := gltf.LoadFS(fsys, model+".gltf")
	if err != nil {
		t.Fatalf("load %s/%s: %v", model, variant, err)
	}
	return doc
}

// vertexTolerance bounds how far a Draco-decoded value may be from the uncompressed one. Draco quantizes each attribute to a grid (glTF-Pipeline, which made the Khronos samples, defaults to 11 bits for positions, 10 for texture coordinates and 8 for normals and other attributes), so the tolerance is a few grid steps of each attribute's range.
type vertexTolerance struct {
	position, normal, texcoord, generic float64
}

// vertexFeatures flattens everything stored per vertex in prim, in a fixed order, with the tolerance for each value.
func vertexFeatures(prim gltf.Primitive, i int, tol vertexTolerance) (vals, tols []float64) {
	add := func(t float64, v ...float32) {
		for _, x := range v {
			vals = append(vals, float64(x))
			tols = append(tols, t)
		}
	}
	add(tol.position, prim.Positions[i].X, prim.Positions[i].Y, prim.Positions[i].Z)
	if prim.Normals != nil {
		add(tol.normal, prim.Normals[i].X, prim.Normals[i].Y, prim.Normals[i].Z)
	}
	if prim.Tangents != nil {
		add(tol.normal, prim.Tangents[i].X, prim.Tangents[i].Y, prim.Tangents[i].Z, prim.Tangents[i].W)
	}
	if prim.TexCoords0 != nil {
		add(tol.texcoord, prim.TexCoords0[i].X, prim.TexCoords0[i].Y)
	}
	if prim.Joints0 != nil {
		j := prim.Joints0[i]
		add(0.5, float32(j[0]), float32(j[1]), float32(j[2]), float32(j[3]))
	}
	if prim.Weights0 != nil {
		add(tol.generic, prim.Weights0[i].X, prim.Weights0[i].Y, prim.Weights0[i].Z, prim.Weights0[i].W)
	}
	for _, target := range prim.Targets {
		if target.Positions != nil {
			add(tol.position, target.Positions[i].X, target.Positions[i].Y, target.Positions[i].Z)
		}
		if target.Normals != nil {
			add(tol.normal, target.Normals[i].X, target.Normals[i].Y, target.Normals[i].Z)
		}
	}
	return vals, tols
}

// requireSameGeometry checks that got draws the same triangles as want with the same per-vertex data, allowing Draco to reorder, renumber, merge and split vertices. Every uncompressed triangle must pair with a distinct decoded triangle whose three vertices, in the same winding, are each within tolerance of the uncompressed ones.
func requireSameGeometry(t *testing.T, name string, want, got gltf.Primitive, tol vertexTolerance) {
	t.Helper()
	if len(got.Indices) != len(want.Indices) {
		t.Fatalf("%s: %d indices, want %d", name, len(got.Indices), len(want.Indices))
	}
	if (got.Normals == nil) != (want.Normals == nil) || (got.Tangents == nil) != (want.Tangents == nil) ||
		(got.TexCoords0 == nil) != (want.TexCoords0 == nil) || (got.Joints0 == nil) != (want.Joints0 == nil) ||
		(got.Weights0 == nil) != (want.Weights0 == nil) || len(got.Targets) != len(want.Targets) {
		t.Fatalf("%s: decoded attributes differ from the uncompressed ones", name)
	}

	type triangle struct {
		verts [3][]float64
		cx    float64 // sum of the vertices' x, a cheap key to find candidates
	}
	var tols []float64
	build := func(p gltf.Primitive) []triangle {
		out := make([]triangle, len(p.Indices)/3)
		for f := range out {
			for k := range 3 {
				var vals []float64
				vals, tols = vertexFeatures(p, int(p.Indices[3*f+k]), tol)
				out[f].verts[k] = vals
				out[f].cx += vals[0]
			}
		}
		return out
	}
	wantTris, gotTris := build(want), build(got)
	slices.SortFunc(gotTris, func(a, b triangle) int { return cmp.Compare(a.cx, b.cx) })
	// distance is the largest per-value difference between two vertices in units of tolerance.
	distance := func(a, b []float64) float64 {
		d := 0.0
		for k := range a {
			d = max(d, math.Abs(a[k]-b[k])/tols[k])
		}
		return d
	}
	used := make([]bool, len(gotTris))
	for f, w := range wantTris {
		// Quantization can merge nearby vertices, which leaves near-duplicate triangles; taking the closest unused candidate rather than the first one within tolerance keeps them from stealing each other's match.
		lo, _ := slices.BinarySearchFunc(gotTris, w.cx-3*tols[0], func(a triangle, x float64) int { return cmp.Compare(a.cx, x) })
		best, bestDist := -1, 1.0
		for g := lo; g < len(gotTris) && gotTris[g].cx <= w.cx+3*tols[0]; g++ {
			if used[g] {
				continue
			}
			gv := gotTris[g].verts
			for r := range 3 {
				d := max(distance(w.verts[0], gv[r]), distance(w.verts[1], gv[(r+1)%3]), distance(w.verts[2], gv[(r+2)%3]))
				if d <= bestDist {
					best, bestDist = g, d
				}
			}
		}
		found := best >= 0
		if found {
			used[best] = true
		}
		if !found {
			t.Fatalf("%s: uncompressed triangle %d %v has no decoded counterpart", name, f, w.verts)
		}
	}
}

func TestDracoKhronosSamplesMatchUncompressed(t *testing.T) {
	tol := vertexTolerance{normal: 0.02, generic: 2.0 / 255}
	// Together these cover sequential and edgebreaker (standard and valence) connectivity; the generic, integer, quantization and normal attribute decoders; and the difference, parallelogram, constrained multi-parallelogram, portable texture coordinate and geometric normal predictions.
	for _, model := range []string{"Box", "Avocado", "CesiumMilkTruck", "MorphPrimitivesTest", "RiggedSimple", "SunglassesKhronos"} {
		t.Run(model, func(t *testing.T) {
			want := loadKhronos(t, model, "glTF", nil)
			got := loadKhronos(t, model, "glTF-Draco", dropRequiredWebP)
			if len(got.Meshes) != len(want.Meshes) {
				t.Fatalf("%d meshes, want %d", len(got.Meshes), len(want.Meshes))
			}
			for mi := range want.Meshes {
				if len(got.Meshes[mi].Primitives) != len(want.Meshes[mi].Primitives) {
					t.Fatalf("mesh %d: %d primitives, want %d", mi, len(got.Meshes[mi].Primitives), len(want.Meshes[mi].Primitives))
				}
				for pi, wp := range want.Meshes[mi].Primitives {
					gp := got.Meshes[mi].Primitives[pi]
					// Allow two grid steps of 11-bit positions over the mesh's extent and 10-bit texture coordinates over their range.
					size := wp.Bounds.Max.Sub(wp.Bounds.Min)
					ptol := tol
					ptol.position = max(2*float64(max(size.X, size.Y, size.Z))/(1<<11-1), 1e-6)
					ptol.texcoord = max(2*uvRange(wp)/(1<<10-1), 1e-6)
					requireSameGeometry(t, fmt.Sprintf("mesh %d primitive %d", mi, pi), wp, gp, ptol)
					if gp.Bounds.Min.Sub(wp.Bounds.Min).Len() > float32(2*ptol.position) || gp.Bounds.Max.Sub(wp.Bounds.Max).Len() > float32(2*ptol.position) {
						t.Fatalf("mesh %d primitive %d: bounds %v, want %v", mi, pi, gp.Bounds, wp.Bounds)
					}
				}
			}
		})
	}
}

// dropRequiredWebP lets SunglassesKhronos's Draco variant load: it requires EXT_texture_webp for its textures, which the loader never reads while loading geometry.
func dropRequiredWebP(doc map[string]any) {
	req, _ := doc["extensionsRequired"].([]any)
	doc["extensionsRequired"] = slices.DeleteFunc(req, func(v any) bool { return v == "EXT_texture_webp" })
}

func uvRange(p gltf.Primitive) float64 {
	if len(p.TexCoords0) == 0 {
		return 0
	}
	lo, hi := p.TexCoords0[0], p.TexCoords0[0]
	for _, uv := range p.TexCoords0 {
		lo.X, lo.Y = min(lo.X, uv.X), min(lo.Y, uv.Y)
		hi.X, hi.Y = max(hi.X, uv.X), max(hi.Y, uv.Y)
	}
	return float64(max(hi.X-lo.X, hi.Y-lo.Y))
}

// khronosFS returns the files of testdata/khronos/dir with the glTF JSON in name rewritten by edit, and a set recording which files were read.
func khronosFS(t testing.TB, dir, name string, edit func(doc map[string]any)) (fs.FS, map[string]bool) {
	t.Helper()
	src := os.DirFS("testdata/khronos/" + dir)
	fsys := fstest.MapFS{}
	entries, err := fs.ReadDir(src, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := fs.ReadFile(src, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		fsys[e.Name()] = &fstest.MapFile{Data: data}
	}
	if edit != nil {
		var doc map[string]any
		if err := json.Unmarshal(fsys[name].Data, &doc); err != nil {
			t.Fatal(err)
		}
		edit(doc)
		data, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		fsys[name].Data = data
	}
	read := make(map[string]bool)
	return recordingFS{fsys, read}, read
}

type recordingFS struct {
	fstest.MapFS
	read map[string]bool
}

func (r recordingFS) ReadFile(name string) ([]byte, error) {
	r.read[name] = true
	return r.MapFS.ReadFile(name)
}

// stripMeshopt removes the extension from every bufferView and buffer, leaving a plain glTF that reads the uncompressed fallback buffer.
func stripMeshopt(doc map[string]any) {
	for _, key := range []string{"bufferViews", "buffers"} {
		for _, v := range doc[key].([]any) {
			obj := v.(map[string]any)
			if exts, ok := obj["extensions"].(map[string]any); ok {
				delete(exts, "KHR_meshopt_compression")
				if len(exts) == 0 {
					delete(obj, "extensions")
				}
			}
		}
	}
	doc["extensionsUsed"] = []any{"KHR_mesh_quantization"}
}

// renameMeshopt rewrites the KHR extension as EXT_meshopt_compression, which shares its JSON and its version 0 bitstream.
func renameMeshopt(doc map[string]any) {
	data, _ := json.Marshal(doc)
	clear(doc)
	_ = json.Unmarshal([]byte(strings.ReplaceAll(string(data), "KHR_meshopt_compression", "EXT_meshopt_compression")), &doc)
}

func TestMeshoptKhronosSampleMatchesFallback(t *testing.T) {
	refFS, _ := khronosFS(t, "MeshoptCubeTest/glTF", "MeshoptCubeTest.gltf", stripMeshopt)
	want, err := gltf.LoadFS(refFS, "MeshoptCubeTest.gltf")
	if err != nil {
		t.Fatalf("uncompressed: %v", err)
	}

	optionalFS, optionalRead := khronosFS(t, "MeshoptCubeTest/glTF", "MeshoptCubeTest.gltf", nil)
	requiredFS, _ := khronosFS(t, "MeshoptCubeTest/glTF-Meshopt", "MeshoptCubeTest.gltf", nil)
	variants := map[string]fs.FS{"optional": optionalFS, "required": requiredFS}
	for name, fsys := range variants {
		t.Run(name, func(t *testing.T) {
			got, err := gltf.LoadFS(fsys, "MeshoptCubeTest.gltf")
			if err != nil {
				t.Fatal(err)
			}
			requireSameMeshopt(t, want, got)
		})
	}
	if optionalRead["MeshoptCubeTestFallback.bin"] {
		t.Fatal("loading the compressed views fetched the fallback buffer")
	}

	// Rows 2 to 4 of the model use only the version 0 codec and filters EXT_meshopt_compression defines; the COLOR filter in row 5 is KHR-only.
	extFS, _ := khronosFS(t, "MeshoptCubeTest/glTF-Meshopt", "MeshoptCubeTest.gltf", renameMeshopt)
	if _, err := gltf.LoadFS(extFS, "MeshoptCubeTest.gltf"); err == nil || !strings.Contains(err.Error(), "COLOR") {
		t.Fatalf("EXT_meshopt_compression with the COLOR filter: got %v, want an error naming COLOR", err)
	}
}

// sameTriangles reports whether two triangle lists hold the same triangles in the same order and winding. The TRIANGLES codec may rotate the vertices of a triangle, so each triangle is compared up to rotation.
func sameTriangles(a, b []uint32) bool {
	if len(a) != len(b) || len(a)%3 != 0 {
		return false
	}
	for f := 0; f < len(a); f += 3 {
		x, y := a[f:f+3], b[f:f+3]
		if !slices.Equal(x, y) && !slices.Equal(x, []uint32{y[1], y[2], y[0]}) && !slices.Equal(x, []uint32{y[2], y[0], y[1]}) {
			return false
		}
	}
	return true
}

// requireSameMeshopt compares two loads of MeshoptCubeTest element by element: meshopt keeps vertex order, only its filters are lossy, and its triangle codec may rotate triangles.
func requireSameMeshopt(t *testing.T, want, got *gltf.Document) {
	t.Helper()
	const filterTol = 1.0 / 127 // an 8-bit octahedral or color value
	for mi := range want.Meshes {
		for pi, wp := range want.Meshes[mi].Primitives {
			gp := got.Meshes[mi].Primitives[pi]
			name := fmt.Sprintf("mesh %d primitive %d", mi, pi)
			if !sameTriangles(gp.Indices, wp.Indices) {
				t.Fatalf("%s: indices %v, want %v", name, gp.Indices, wp.Indices)
			}
			if len(gp.Positions) != len(wp.Positions) || len(gp.Normals) != len(wp.Normals) || len(gp.Colors0) != len(wp.Colors0) {
				t.Fatalf("%s: attribute counts differ", name)
			}
			for i := range wp.Positions {
				if d := gp.Positions[i].Sub(wp.Positions[i]).Len(); d > 1e-3*max(1, wp.Positions[i].Len()) {
					t.Fatalf("%s: position %d is %v, want %v", name, i, gp.Positions[i], wp.Positions[i])
				}
			}
			for i := range wp.Normals {
				if d := gp.Normals[i].Sub(wp.Normals[i]).Len(); d > 2*filterTol {
					t.Fatalf("%s: normal %d is %v, want %v", name, i, gp.Normals[i], wp.Normals[i])
				}
			}
			for i := range wp.Colors0 {
				if d := gp.Colors0[i].Sub(wp.Colors0[i]).Len(); d > 2*filterTol {
					t.Fatalf("%s: color %d is %v, want %v", name, i, gp.Colors0[i], wp.Colors0[i])
				}
			}
		}
	}
	if len(got.Animations) != len(want.Animations) {
		t.Fatalf("%d animations, want %d", len(got.Animations), len(want.Animations))
	}
	for ai, wa := range want.Animations {
		for ci, wc := range wa.Channels {
			gc := got.Animations[ai].Channels[ci]
			if !slices.Equal(gc.Times, wc.Times) || len(gc.Values) != len(wc.Values) {
				t.Fatalf("animation %d channel %d: keyframes differ", ai, ci)
			}
			for k := range wc.Values {
				if math.Abs(float64(gc.Values[k]-wc.Values[k])) > 1e-3 {
					t.Fatalf("animation %d channel %d value %d is %v, want %v", ai, ci, k, gc.Values[k], wc.Values[k])
				}
			}
		}
	}
}

// loadEdited loads a Khronos fixture whose glTF JSON edit rewrites, returning the load error.
func loadEdited(t *testing.T, dir, name string, edit func(doc map[string]any)) (*gltf.Document, error) {
	t.Helper()
	fsys, _ := khronosFS(t, dir, name, edit)
	return gltf.LoadFS(fsys, name)
}

// at walks a decoded JSON document along keys (strings for objects, ints for arrays).
func at(doc any, keys ...any) map[string]any {
	for _, k := range keys {
		switch k := k.(type) {
		case string:
			doc = doc.(map[string]any)[k]
		case int:
			doc = doc.([]any)[k]
		}
	}
	return doc.(map[string]any)
}

func TestDracoRejectsInconsistentPrimitives(t *testing.T) {
	dracoExt := func(doc map[string]any) map[string]any {
		return at(doc, "meshes", 0, "primitives", 0, "extensions", "KHR_draco_mesh_compression")
	}
	cases := []struct {
		name string
		edit func(doc map[string]any)
		want string
	}{
		{"attribute not on the primitive", func(doc map[string]any) { at(dracoExt(doc), "attributes")["TEXCOORD_0"] = 2.0 }, "is not an attribute of the primitive"},
		{"unknown attribute id", func(doc map[string]any) { at(dracoExt(doc), "attributes")["POSITION"] = 9.0 }, "no attribute with id 9"},
		{"accessor counts disagree", func(doc map[string]any) { at(doc, "accessors", 1)["count"] = 25.0 }, "disagree"},
		{"vertex count differs from the stream", func(doc map[string]any) {
			at(doc, "accessors", 1)["count"] = 25.0
			at(doc, "accessors", 2)["count"] = 25.0
		}, "decoded 24 vertices"},
		{"no indices", func(doc map[string]any) { delete(at(doc, "meshes", 0, "primitives", 0), "indices") }, "needs an indices accessor"},
		{"point mode", func(doc map[string]any) { at(doc, "meshes", 0, "primitives", 0)["mode"] = 0.0 }, "needs mode TRIANGLES"},
		{"bad bufferView", func(doc map[string]any) { dracoExt(doc)["bufferView"] = 7.0 }, "no valid bufferView"},
		{"float indices", func(doc map[string]any) { at(doc, "accessors", 0)["componentType"] = 5126.0 }, "not an unsigned integer"},
		{"negative normals in unsigned bytes", func(doc map[string]any) { at(doc, "accessors", 1)["componentType"] = 5121.0 }, "does not fit"},
		{"corrupt stream", func(doc map[string]any) {
			at(doc, "bufferViews", 0)["byteOffset"] = 1.0
			at(doc, "bufferViews", 0)["byteLength"] = 117.0
		}, "KHR_draco_mesh_compression"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadEdited(t, "Box/glTF-Draco", "Box.gltf", tc.edit)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestDracoTriangleStripDecodesAsList(t *testing.T) {
	doc, err := loadEdited(t, "Box/glTF-Draco", "Box.gltf", func(doc map[string]any) {
		at(doc, "meshes", 0, "primitives", 0)["mode"] = 5.0
	})
	if err != nil {
		t.Fatal(err)
	}
	if prim := doc.Meshes[0].Primitives[0]; prim.Mode != gltf.ModeTriangles || len(prim.Indices) != 36 {
		t.Fatalf("mode %d with %d indices, want TRIANGLES with 36", prim.Mode, len(prim.Indices))
	}
}

func TestMeshoptRejectsInvalidViews(t *testing.T) {
	// bufferView 23 holds ATTRIBUTES and 24 INDICES data, both compressed into fallback buffer 1.
	ext := func(doc map[string]any, view int) map[string]any {
		return at(doc, "bufferViews", view, "extensions", "KHR_meshopt_compression")
	}
	cases := []struct {
		name string
		edit func(doc map[string]any)
		want string
	}{
		{"unknown mode", func(doc map[string]any) { ext(doc, 23)["mode"] = "STRIPS" }, "unknown mode"},
		{"unknown filter", func(doc map[string]any) { ext(doc, 23)["filter"] = "SHARPEN" }, "unknown filter"},
		{"filter on indices", func(doc map[string]any) { ext(doc, 24)["filter"] = "OCTAHEDRAL" }, "does not take filter"},
		{"missing count", func(doc map[string]any) { delete(ext(doc, 23), "count") }, "needs buffer"},
		{"length is not stride times count", func(doc map[string]any) { ext(doc, 23)["count"] = ext(doc, 23)["count"].(float64) + 1 }, "is not"},
		{"source in the fallback buffer", func(doc map[string]any) { ext(doc, 23)["buffer"] = 1.0 }, "reads compressed data from fallback buffer"},
		{"source out of bounds", func(doc map[string]any) { ext(doc, 23)["byteLength"] = 1e9 }, "exceeds buffer 0 bounds"},
		{"uncompressed view of the fallback buffer", func(doc map[string]any) { at(doc, "bufferViews", 0)["buffer"] = 1.0 }, "without decompressing it"},
		{"corrupt stream", func(doc map[string]any) { ext(doc, 23)["byteOffset"] = ext(doc, 23)["byteOffset"].(float64) + 4 }, "meshopt"},
		{"oversized placeholder", func(doc map[string]any) { at(doc, "buffers", 1)["byteLength"] = 1e10 }, "safety limit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadEdited(t, "MeshoptCubeTest/glTF-Meshopt", "MeshoptCubeTest.gltf", tc.edit)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestMeshoptUntaggedPlaceholderBuffer(t *testing.T) {
	// A URI-less buffer that only compressed views decode into is a placeholder even without the fallback tag.
	if _, err := loadEdited(t, "MeshoptCubeTest/glTF-Meshopt", "MeshoptCubeTest.gltf", func(doc map[string]any) {
		delete(at(doc, "buffers", 1), "extensions")
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMeshoptDoesNotModifyResolvedBuffers(t *testing.T) {
	// Without the fallback tag the uncompressed buffer is fetched; decoding into it must not write through to the resolver's bytes.
	fsys, _ := khronosFS(t, "MeshoptCubeTest/glTF", "MeshoptCubeTest.gltf", func(doc map[string]any) {
		delete(at(doc, "buffers", 1), "extensions")
	})
	data, err := fs.ReadFile(fsys, "MeshoptCubeTest.gltf")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, name := range []string{"MeshoptCubeTest.bin", "MeshoptCubeTestFallback.bin"} {
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			t.Fatal(err)
		}
		// Zero the uncompressed copy so any write through it would show.
		if name == "MeshoptCubeTestFallback.bin" {
			clear(b)
		}
		files[name] = b
	}
	doc, err := gltf.Load(data, func(uri string) ([]byte, error) { return files[uri], nil })
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(files["MeshoptCubeTestFallback.bin"], func(b byte) bool { return b != 0 }) {
		t.Fatal("decoding wrote into the resolver's buffer")
	}
	if !slices.ContainsFunc(doc.Buffers[1].Data, func(b byte) bool { return b != 0 }) {
		t.Fatal("decoded views are missing from the document's copy of the buffer")
	}
}
