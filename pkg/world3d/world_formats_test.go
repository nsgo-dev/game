package world3d_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/world3d"
)

const sampleDungeonGLTF = `{
  "asset": {"version": "2.0"},
  "nodes": [
    {"name": "stone_floor", "mesh": 0},
    {"name": "stone_pillar", "mesh": 1}
  ],
  "meshes": [
    {"name": "stone_floor", "primitives": [{"attributes": {"POSITION": 0}, "material": 0}]},
    {"name": "stone_pillar", "primitives": [{"attributes": {"POSITION": 1}, "material": 1}]}
  ],
  "accessors": [
    {"min": [-1, -1, -1], "max": [1, 1, 1]},
    {"min": [-0.5, -1, -0.5], "max": [0.5, 1, 0.5]}
  ]
}`

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func makeSampleHeightImage() *image.Gray16 {
	img := image.NewGray16(image.Rect(0, 0, 9, 9))
	for r := range 9 {
		for c := range 9 {
			// Gentle dome rising from 0 at corners to ~0.75 at center.
			dx := float64(c - 4)
			dy := float64(r - 4)
			h := 0.75 * math.Exp(-(dx*dx+dy*dy)/12.0)
			img.SetGray16(c, r, color.Gray16{Y: uint16(math.Round(h * 65535.0))})
		}
	}
	return img
}

func makeSampleDensityImage() *image.Gray {
	img := image.NewGray(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			v := uint8(64 + (x+y)*30)
			img.SetGray(x, y, color.Gray{Y: v})
		}
	}
	return img
}

func TestGridMapYAMLEquivalentToCodeExample(t *testing.T) {
	yamlBytes, err := os.ReadFile("testdata/dungeon.gridmap.yaml")
	if err != nil {
		t.Fatal(err)
	}

	// Build expected GridMap directly in Go code (matching ExampleGridMap + rotated pillars).
	codeLib, err := world3d.LoadMeshLibraryFromGLTF([]byte(sampleDungeonGLTF))
	if err != nil {
		t.Fatalf("LoadMeshLibraryFromGLTF: %v", err)
	}
	want := world3d.NewGridMap(math3d.Vec3{X: 2, Y: 2, Z: 2}, codeLib)
	floorDef, _ := codeLib.ByName("stone_floor")
	pillarDef, _ := codeLib.ByName("stone_pillar")
	want.FillBox(world3d.GridCoord{X: 0, Y: 0, Z: 0}, world3d.GridCoord{X: 4, Y: 0, Z: 4}, floorDef.ID, 0)
	want.SetCell(world3d.GridCoord{X: 0, Y: 1, Z: 0}, pillarDef.ID, 0)
	want.SetCell(world3d.GridCoord{X: 4, Y: 1, Z: 0}, pillarDef.ID, 1)

	// Load the same world from YAML + glTF via fs.FS.
	fsys := fstest.MapFS{
		"levels/dungeon.gridmap.yaml": {Data: yamlBytes},
		"models/dungeon_tiles.gltf":   {Data: []byte(sampleDungeonGLTF)},
	}
	got, doc, err := world3d.LoadGridMapFS(fsys, "levels/dungeon.gridmap.yaml")
	if err != nil {
		t.Fatalf("LoadGridMapFS: %v", err)
	}
	if doc.Name != "Stone Keep" {
		t.Fatalf("doc.Name = %q, want %q", doc.Name, "Stone Keep")
	}
	if got.Count() != want.Count() {
		t.Fatalf("Count() = %d, want %d", got.Count(), want.Count())
	}
	if got.Bounds() != want.Bounds() {
		t.Fatalf("Bounds() = %v, want %v", got.Bounds(), want.Bounds())
	}
	if got.CellSize != want.CellSize || got.Origin != want.Origin {
		t.Fatalf("CellSize/Origin = (%v, %v), want (%v, %v)", got.CellSize, got.Origin, want.CellSize, want.Origin)
	}

	for y := range 2 {
		for z := range 5 {
			for x := range 5 {
				c := world3d.GridCoord{X: x, Y: y, Z: z}
				gc, gok := got.Cell(c)
				wc, wok := want.Cell(c)
				if gok != wok || gc != wc {
					t.Fatalf("Cell(%v) = (%+v, %v), want (%+v, %v)", c, gc, gok, wc, wok)
				}
			}
		}
	}

	ray := math3d.Ray{Origin: math3d.Vec3{X: 3, Y: 10, Z: 3}, Dir: math3d.Vec3{Y: -1}}
	ghit, gok := got.Raycast(ray, 50)
	whit, wok := want.Raycast(ray, 50)
	if gok != wok || ghit != whit {
		t.Fatalf("Raycast = (%+v, %v), want (%+v, %v)", ghit, gok, whit, wok)
	}

	gBatches := got.BuildInstances(nil, nil)
	wBatches := want.BuildInstances(nil, nil)
	if len(gBatches) != len(wBatches) {
		t.Fatalf("BuildInstances len = %d, want %d", len(gBatches), len(wBatches))
	}
	for i := range gBatches {
		if gBatches[i].MeshID != wBatches[i].MeshID || gBatches[i].MaterialID != wBatches[i].MaterialID || len(gBatches[i].Transforms) != len(wBatches[i].Transforms) {
			t.Fatalf("batch[%d] = (%d,%d,%d), want (%d,%d,%d)", i,
				gBatches[i].MeshID, gBatches[i].MaterialID, len(gBatches[i].Transforms),
				wBatches[i].MeshID, wBatches[i].MaterialID, len(wBatches[i].Transforms))
		}
	}

	// Exporting the code-built GridMap back to YAML matches the fixture byte-for-byte.
	exportedDoc, err := want.ToDoc("Stone Keep", "models/dungeon_tiles.gltf")
	if err != nil {
		t.Fatalf("ToDoc: %v", err)
	}
	encoded, err := world3d.EncodeGridMap(exportedDoc)
	if err != nil {
		t.Fatalf("EncodeGridMap: %v", err)
	}
	if !bytes.Equal(encoded, yamlBytes) {
		t.Fatalf("ToDoc+EncodeGridMap differs from testdata/dungeon.gridmap.yaml:\n--- got ---\n%s\n--- want ---\n%s", encoded, yamlBytes)
	}
}

func TestGridMapYAMLCanonicalOrderAndValidation(t *testing.T) {
	// Unsorted boxes and cells encode deterministically in (y, z, x) order without mutating doc.
	unsorted := []byte(`version: 1
name: Unsorted
boxes:
  - min: [2, 1, 0]
    max: [3, 1, 0]
    tile: wall
  - min: [0, 0, 0]
    max: [1, 0, 1]
    tile: floor
cells:
  - at: [5, 0, 2]
    tile: torch
    orientation: 2
  - at: [1, 0, 0]
    tile: door
`)
	doc, err := world3d.ParseGridMap(unsorted)
	if err != nil {
		t.Fatalf("ParseGridMap: %v", err)
	}
	out1, err := world3d.EncodeGridMap(doc)
	if err != nil {
		t.Fatalf("EncodeGridMap: %v", err)
	}
	// Original doc must not be mutated.
	if doc.Boxes[0].Tile != "wall" || doc.Cells[0].Tile != "torch" {
		t.Fatalf("EncodeGridMap mutated caller's GridMapDoc")
	}
	doc2, err := world3d.ParseGridMap(out1)
	if err != nil {
		t.Fatalf("ParseGridMap(re-encoded): %v", err)
	}
	if doc2.Boxes[0].Tile != "floor" || doc2.Boxes[1].Tile != "wall" {
		t.Fatalf("canonical box order wrong: %+v", doc2.Boxes)
	}
	if doc2.Cells[0].Tile != "door" || doc2.Cells[1].Tile != "torch" {
		t.Fatalf("canonical cell order wrong: %+v", doc2.Cells)
	}
	out2, err := world3d.EncodeGridMap(doc2)
	if err != nil {
		t.Fatalf("EncodeGridMap(2): %v", err)
	}
	if !bytes.Equal(out1, out2) {
		t.Fatalf("round-trip changed bytes:\n%s\nvs\n%s", out1, out2)
	}

	badCases := []struct {
		name string
		yaml string
		want string
	}{
		{"empty", "", "empty grid map"},
		{"unknown field", "version: 1\nextra: true\n", "extra"},
		{"trailing doc", "version: 1\n---\nversion: 1\n", "trailing"},
		{"bad version", "version: 2\n", "version"},
		{"zero cellSize", "version: 1\ncellSize: [1, 0, 1]\n", "cellSize"},
		{"missing box min", "version: 1\nboxes: [{max: [1, 1, 1], tile: a}]\n", "min"},
		{"missing box max", "version: 1\nboxes: [{min: [0, 0, 0], tile: a}]\n", "max"},
		{"inverted box", "version: 1\nboxes: [{min: [2, 0, 0], max: [1, 0, 0], tile: a}]\n", "less than min"},
		{"overlapping boxes", "version: 1\nboxes: [{min: [0, 0, 0], max: [2, 0, 2], tile: a}, {min: [2, 0, 2], max: [4, 0, 4], tile: b}]\n", "overlaps"},
		{"bad box orient", "version: 1\nboxes: [{min: [0, 0, 0], max: [1, 0, 0], tile: a, orientation: 24}]\n", "orientation"},
		{"empty box tile", "version: 1\nboxes: [{min: [0, 0, 0], max: [1, 0, 0], tile: ''}]\n", "tile"},
		{"missing cell at", "version: 1\ncells: [{tile: a}]\n", "at"},
		{"duplicate cell", "version: 1\ncells: [{at: [1, 2, 3], tile: a}, {at: [1, 2, 3], tile: b}]\n", "duplicate cell"},
		{"bad cell orient", "version: 1\ncells: [{at: [0, 0, 0], tile: a, orientation: 30}]\n", "orientation"},
		{"empty cell tile", "version: 1\ncells: [{at: [0, 0, 0], tile: ''}]\n", "tile"},
	}
	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := world3d.ParseGridMap([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if _, ok := loaderr.As(err); !ok {
				t.Fatalf("error %T is not *loaderr.Error: %v", err, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.want)
			}
		})
	}
}

func TestTerrainYAMLBuildAndChunkSizeEquivalentToCode(t *testing.T) {
	hmImg := makeSampleHeightImage()
	splatImg := makeFourCornerSplat()
	lowH := image.NewGray(image.Rect(0, 0, 2, 2))
	highH := image.NewGray(image.Rect(0, 0, 2, 2))
	for y := range 2 {
		for x := range 2 {
			lowH.SetGray(x, y, color.Gray{Y: 26})
			highH.SetGray(x, y, color.Gray{Y: 230})
		}
	}

	terrainYAML := []byte(`# yaml-language-server: $schema=https://game.nsgo.dev/schemas/terrain.v1.json
version: 1
name: Canyon
heightmap: terrain/canyon_height.png
cellSize: 2
heightScale: 40
origin:
  - -8
  - 1
  - -8
chunkSize: 4
splatMaps:
  - terrain/canyon_splat0.png
heightBlendContrast: 0.65
layers:
  - name: grass
    height: textures/grass_height.png
    uvScale:
      - 8
      - 8
  - name: dirt
    uvScale:
      - 6
      - 6
  - name: rock
    height: textures/rock_height.png
    uvScale:
      - 4
      - 4
    heightBias: 0.1
  - name: snow
`)

	// Verify round-trip changes no byte.
	doc, err := world3d.ParseTerrain(terrainYAML)
	if err != nil {
		t.Fatalf("ParseTerrain: %v", err)
	}
	encoded, err := world3d.EncodeTerrain(doc)
	if err != nil {
		t.Fatalf("EncodeTerrain: %v", err)
	}
	if !bytes.Equal(encoded, terrainYAML) {
		t.Fatalf("terrain round-trip changed bytes:\n--- got ---\n%s\n--- want ---\n%s", encoded, terrainYAML)
	}
	if doc.EffectiveChunkSize() != 4 {
		t.Fatalf("EffectiveChunkSize = %d, want 4", doc.EffectiveChunkSize())
	}

	// Build expected Heightmap + TerrainSplatMap + TerrainLOD directly in code.
	wantHM := world3d.LoadHeightmapGray(hmImg, 2, 40)
	wantHM.Origin = math3d.Vec3{X: -8, Y: 1, Z: -8}
	wantSplat, err := world3d.LoadTerrainSplatMap([]world3d.TerrainLayerInfo{
		{Name: "grass", UVScale: math3d.Vec2{X: 8, Y: 8}, HeightMap: world3d.LoadTerrainHeightLayer(lowH)},
		{Name: "dirt", UVScale: math3d.Vec2{X: 6, Y: 6}},
		{Name: "rock", UVScale: math3d.Vec2{X: 4, Y: 4}, HeightBias: 0.1, HeightMap: world3d.LoadTerrainHeightLayer(highH)},
		{Name: "snow", UVScale: math3d.Vec2{X: 1, Y: 1}},
	}, splatImg)
	if err != nil {
		t.Fatalf("LoadTerrainSplatMap: %v", err)
	}
	wantSplat.HeightBlendContrast = 0.65
	wantHM.Splat = wantSplat
	wantLOD := world3d.NewTerrainLOD(wantHM, world3d.TerrainLODConfig{ChunkCells: 4})

	// Load the same terrain from fs.FS.
	fsys := fstest.MapFS{
		"terrain/canyon.terrain.yaml": {Data: terrainYAML},
		"terrain/canyon_height.png":   {Data: encodePNG(t, hmImg)},
		"terrain/canyon_splat0.png":   {Data: encodePNG(t, splatImg)},
		"textures/grass_height.png":   {Data: encodePNG(t, lowH)},
		"textures/rock_height.png":    {Data: encodePNG(t, highH)},
	}
	gotHM, gotDoc, err := world3d.LoadTerrainFS(fsys, "terrain/canyon.terrain.yaml")
	if err != nil {
		t.Fatalf("LoadTerrainFS: %v", err)
	}
	gotLOD := gotDoc.BuildLOD(gotHM)

	if gotHM.Bounds() != wantHM.Bounds() {
		t.Fatalf("Bounds = %v, want %v", gotHM.Bounds(), wantHM.Bounds())
	}
	if gotLOD.ChunkCells() != wantLOD.ChunkCells() || gotLOD.ChunkWorldSize() != wantLOD.ChunkWorldSize() {
		t.Fatalf("LOD chunks = (%d, %v), want (%d, %v)", gotLOD.ChunkCells(), gotLOD.ChunkWorldSize(), wantLOD.ChunkCells(), wantLOD.ChunkWorldSize())
	}
	for _, pt := range [][2]float32{{-8, -8}, {0, 0}, {3.5, -2.5}, {8, 8}} {
		gy, gok := gotHM.HeightAt(pt[0], pt[1])
		wy, wok := wantHM.HeightAt(pt[0], pt[1])
		if gok != wok || !math3d.ApproxEqual(gy, wy, 1e-5) {
			t.Fatalf("HeightAt(%v) = (%v, %v), want (%v, %v)", pt, gy, gok, wy, wok)
		}
		gn := gotHM.NormalAt(pt[0], pt[1])
		wn := wantHM.NormalAt(pt[0], pt[1])
		if !gn.ApproxEqual(wn, 1e-5) {
			t.Fatalf("NormalAt(%v) = %v, want %v", pt, gn, wn)
		}
		gl, glok := gotHM.LayerAt(pt[0], pt[1])
		wl, wlok := wantHM.LayerAt(pt[0], pt[1])
		if glok != wlok || gl != wl {
			t.Fatalf("LayerAt(%v) = (%+v, %v), want (%+v, %v)", pt, gl, glok, wl, wlok)
		}
	}

	// Negative chunkSize is rejected.
	if _, err := world3d.ParseTerrain([]byte("version: 1\nchunkSize: -4\nlayers: [{name: grass}]\n")); err == nil || !strings.Contains(err.Error(), "chunkSize") {
		t.Fatalf("expected chunkSize error, got %v", err)
	}
}

func TestFoliageYAMLEquivalentToCodeExample(t *testing.T) {
	yamlBytes, err := os.ReadFile("testdata/meadow.foliage.yaml")
	if err != nil {
		t.Fatal(err)
	}
	hm := world3d.LoadHeightmapGray(makeSampleHeightImage(), 8.0, 20.0)
	densityImg := makeSampleDensityImage()
	densityMap := world3d.LoadDensityMapGray(densityImg)

	// Build expected foliage fields directly in Go code.
	wantGrass := world3d.Scatter(hm, world3d.FoliageLayer{
		Name:            "grass",
		MeshID:          10,
		MaterialID:      20,
		Density:         1.5,
		MinScale:        0.8,
		MaxScale:        1.3,
		AlignToNormal:   0.6,
		MaxSlopeDeg:     35,
		BaseMaxDistance: 80,
		Seed:            42,
	}, hm.Bounds())
	wantTrees := world3d.Scatter(hm, world3d.FoliageLayer{
		Name:            "pine_tree",
		MeshID:          11,
		Density:         1.0 / (4.0 * 4.0), // spacing: 4
		MinScale:        0.85,
		MaxScale:        1.25,
		MaxSlopeDeg:     28,
		MinHeight:       2,
		MaxHeight:       32,
		DensityMap:      densityMap,
		BaseMaxDistance: 160,
		BoundingRadius:  3.5,
		Seed:            108,
	}, hm.Bounds())

	fsys := fstest.MapFS{
		"terrain/meadow.foliage.yaml": {Data: yamlBytes},
		"terrain/forest_density.png":  {Data: encodePNG(t, densityImg)},
	}
	doc, err := world3d.LoadFoliageFS(fsys, "terrain/meadow.foliage.yaml")
	if err != nil {
		t.Fatalf("LoadFoliageFS: %v", err)
	}
	meshIDs := map[string]uint32{
		"models/grass_clump.glb": 10,
		"models/pine_tree.glb":   11,
	}
	matIDs := map[string]uint32{
		"materials/grass.material.yaml": 20,
	}
	fields, err := doc.ScatterAll(hm, hm.Bounds(), world3d.FoliageBuildOptions{
		ResolveMesh:     func(p string) (uint32, error) { return meshIDs[p], nil },
		ResolveMaterial: func(p string) (uint32, error) { return matIDs[p], nil },
		FS:              fsys,
	})
	if err != nil {
		t.Fatalf("ScatterAll: %v", err)
	}
	if len(fields) != 2 {
		t.Fatalf("len(fields) = %d, want 2", len(fields))
	}

	camPos := math3d.Vec3{X: 32, Y: 40, Z: 32}
	view := math3d.LookAt(camPos, math3d.Vec3{X: 32, Y: 0, Z: 32}, math3d.Vec3{X: 0, Y: 0, Z: -1})
	proj := math3d.Perspective(math3d.DegToRad(70), 1.5, 0.1, 300)
	frustum := math3d.FrustumFromMat4(proj.Mul(view))
	q := world3d.DefaultQualityProfile(world3d.QualityHigh)

	for i, wantField := range []*world3d.FoliageField{wantGrass, wantTrees} {
		gotField := fields[i]
		if gotField.TotalCount() == 0 {
			t.Fatalf("layer %d scattered 0 instances", i)
		}
		if gotField.TotalCount() != wantField.TotalCount() || gotField.ChunkCount() != wantField.ChunkCount() || gotField.Bounds() != wantField.Bounds() {
			t.Fatalf("layer %d summary = (%d, %d, %v), want (%d, %d, %v)",
				i, gotField.TotalCount(), gotField.ChunkCount(), gotField.Bounds(),
				wantField.TotalCount(), wantField.ChunkCount(), wantField.Bounds())
		}
		gotVis := gotField.CollectVisible(camPos, frustum, q, nil)
		wantVis := wantField.CollectVisible(camPos, frustum, q, nil)
		if len(gotVis) != len(wantVis) {
			t.Fatalf("layer %d CollectVisible len = %d, want %d", i, len(gotVis), len(wantVis))
		}
		for j := range gotVis {
			if gotVis[j] != wantVis[j] {
				t.Fatalf("layer %d visible[%d] = %+v, want %+v", i, j, gotVis[j], wantVis[j])
			}
		}
	}

	// FoliageLayerToDoc exports a runtime layer cleanly and round-trips through EncodeFoliage.
	roundDoc := &world3d.FoliageDoc{
		Version: world3d.FoliageFormatVersion,
		Layers: []world3d.FoliageLayerDoc{
			world3d.FoliageLayerToDoc(wantGrass.Layer, "models/grass_clump.glb", "materials/grass.material.yaml", ""),
		},
	}
	encoded, err := world3d.EncodeFoliage(roundDoc)
	if err != nil {
		t.Fatalf("EncodeFoliage: %v", err)
	}
	reparsed, err := world3d.ParseFoliage(encoded)
	if err != nil {
		t.Fatalf("ParseFoliage: %v", err)
	}
	reencoded, err := world3d.EncodeFoliage(reparsed)
	if err != nil {
		t.Fatalf("EncodeFoliage(2): %v", err)
	}
	if !bytes.Equal(encoded, reencoded) {
		t.Fatalf("FoliageLayerToDoc round-trip changed bytes")
	}
}

func TestFoliageYAMLValidation(t *testing.T) {
	badCases := []struct {
		name string
		yaml string
		want string
	}{
		{"empty", "", "empty foliage"},
		{"unknown field", "version: 1\nfoo: 1\nlayers: [{name: a, mesh: m.glb, density: 1}]\n", "foo"},
		{"trailing doc", "version: 1\nlayers: [{name: a, mesh: m.glb, density: 1}]\n---\nversion: 1\n", "trailing"},
		{"bad version", "version: 2\nlayers: [{name: a, mesh: m.glb, density: 1}]\n", "version"},
		{"no layers", "version: 1\nlayers: []\n", "layers"},
		{"empty name", "version: 1\nlayers: [{name: '', mesh: m.glb, density: 1}]\n", "name"},
		{"dup name", "version: 1\nlayers: [{name: a, mesh: m.glb, density: 1}, {name: a, mesh: n.glb, spacing: 2}]\n", "duplicate"},
		{"empty mesh", "version: 1\nlayers: [{name: a, density: 1}]\n", "mesh"},
		{"neither density nor spacing", "version: 1\nlayers: [{name: a, mesh: m.glb}]\n", "density or spacing"},
		{"both density and spacing", "version: 1\nlayers: [{name: a, mesh: m.glb, density: 1, spacing: 2}]\n", "not both"},
		{"negative density", "version: 1\nlayers: [{name: a, mesh: m.glb, density: -1}]\n", "density"},
		{"negative spacing", "version: 1\nlayers: [{name: a, mesh: m.glb, spacing: -2}]\n", "spacing"},
		{"minScale > maxScale", "version: 1\nlayers: [{name: a, mesh: m.glb, density: 1, minScale: 2, maxScale: 1}]\n", "maxScale"},
		{"align > 1", "version: 1\nlayers: [{name: a, mesh: m.glb, density: 1, alignToNormal: 1.5}]\n", "alignToNormal"},
		{"slope > 90", "version: 1\nlayers: [{name: a, mesh: m.glb, density: 1, maxSlopeDeg: 95}]\n", "maxSlopeDeg"},
		{"minSlope > maxSlope", "version: 1\nlayers: [{name: a, mesh: m.glb, density: 1, minSlopeDeg: 40, maxSlopeDeg: 20}]\n", "maxSlopeDeg"},
		{"minHeight > maxHeight", "version: 1\nlayers: [{name: a, mesh: m.glb, density: 1, minHeight: 20, maxHeight: 5}]\n", "maxHeight"},
		{"negative baseMaxDistance", "version: 1\nlayers: [{name: a, mesh: m.glb, density: 1, baseMaxDistance: -5}]\n", "baseMaxDistance"},
		{"negative boundingRadius", "version: 1\nlayers: [{name: a, mesh: m.glb, density: 1, boundingRadius: -1}]\n", "boundingRadius"},
	}
	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := world3d.ParseFoliage([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if _, ok := loaderr.As(err); !ok {
				t.Fatalf("error %T is not *loaderr.Error: %v", err, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.want)
			}
		})
	}
}
