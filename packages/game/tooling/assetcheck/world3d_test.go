package assetcheck_test

import (
	"strings"
	"testing"
	"testing/fstest"
)

const (
	sampleGLTF = `{
  "asset": {"version": "2.0"},
  "nodes": [
    {"name": "stone_floor", "mesh": 0},
    {"name": "stone_pillar", "mesh": 1}
  ],
  "meshes": [
    {"name": "stone_floor", "primitives": [{"attributes": {"POSITION": 0}}]},
    {"name": "stone_pillar", "primitives": [{"attributes": {"POSITION": 0}}]}
  ],
  "accessors": [
    {"min": [-1, -1, -1], "max": [1, 1, 1]}
  ]
}
`
	sampleGridMapYAML = `version: 1
name: Stone Keep
cellSize: [2, 2, 2]
meshLibrary: models/dungeon_tiles.gltf
boxes:
  - min: [0, 0, 0]
    max: [4, 0, 4]
    tile: stone_floor
cells:
  - at: [0, 1, 0]
    tile: stone_pillar
`
	sampleTerrainYAML = `version: 1
name: Alpine Valley
heightmap: terrain/height.png
cellSize: 2
heightScale: 40
chunkSize: 64
splatMaps:
  - terrain/splat0.png
layers:
  - name: grass
    albedo: textures/grass_albedo.png
    normal: textures/grass_normal.png
    height: textures/grass_height.png
`
	sampleFoliageYAML = `version: 1
name: Alpine Meadow
terrain: terrain/alpine.terrain.yaml
layers:
  - name: grass
    mesh: models/grass.glb
    material: materials/grass.material.yaml
    density: 1.5
    seed: 42
  - name: pine
    mesh: models/pine.glb
    densityMap: terrain/forest_density.png
    spacing: 4
    seed: 108
`
)

func world3DProject(t testing.TB) fstest.MapFS {
	t.Helper()
	return fstest.MapFS{
		"nsgo.yaml":                     {Data: []byte("name: World3D\n")},
		"levels/keep.gridmap.yaml":      {Data: []byte(sampleGridMapYAML)},
		"models/dungeon_tiles.gltf":     {Data: []byte(sampleGLTF)},
		"terrain/alpine.terrain.yaml":   {Data: []byte(sampleTerrainYAML)},
		"terrain/height.png":            {Data: pngBytes(t, 8, 8)},
		"terrain/splat0.png":            {Data: pngBytes(t, 4, 4)},
		"textures/grass_albedo.png":     {Data: pngBytes(t, 4, 4)},
		"textures/grass_normal.png":     {Data: pngBytes(t, 4, 4)},
		"textures/grass_height.png":     {Data: pngBytes(t, 4, 4)},
		"terrain/meadow.foliage.yaml":   {Data: []byte(sampleFoliageYAML)},
		"models/grass.glb":              {Data: []byte("glTF")},
		"models/pine.glb":               {Data: []byte("glTF")},
		"materials/grass.material.yaml": {Data: []byte("type: material\nversion: 1\nname: Grass\n")},
		"terrain/forest_density.png":    {Data: pngBytes(t, 4, 4)},
	}
}

func TestWorld3DCleanProject(t *testing.T) {
	r := mustCheck(t, world3DProject(t))
	if !r.OK() {
		t.Fatalf("problems in clean 3D project:\n%s", problems(r))
	}
	if r.GridMaps != 1 || r.Terrains != 1 || r.Foliage != 1 {
		t.Fatalf("counts = (gridMaps=%d, terrains=%d, foliage=%d), want (1, 1, 1)", r.GridMaps, r.Terrains, r.Foliage)
	}
	wantSummary := "0 scenes, 0 maps, 0 tilesets, 0 atlases, 1 grid map, 1 terrain, 1 foliage file: no problems"
	if got := r.Summary(); got != wantSummary {
		t.Fatalf("Summary() = %q, want %q", got, wantSummary)
	}
}

func TestWorld3DBrokenFilesReportFileAndLine(t *testing.T) {
	cases := []struct {
		name   string
		change func(fsys fstest.MapFS)
		file   string
		marker string
		substr string
	}{
		{
			name: "gridmap unknown field",
			change: func(fsys fstest.MapFS) {
				fsys["levels/keep.gridmap.yaml"].Data = []byte(sampleGridMapYAML + "bogus: 1\n")
			},
			file:   "levels/keep.gridmap.yaml",
			marker: "bogus: 1",
			substr: "bogus",
		},
		{
			name: "gridmap missing meshLibrary",
			change: func(fsys fstest.MapFS) {
				delete(fsys, "models/dungeon_tiles.gltf")
			},
			file:   "levels/keep.gridmap.yaml",
			marker: "meshLibrary:",
			substr: "mesh library",
		},
		{
			name: "gridmap broken glTF meshLibrary",
			change: func(fsys fstest.MapFS) {
				fsys["models/dungeon_tiles.gltf"].Data = []byte("{not json")
			},
			file:   "models/dungeon_tiles.gltf",
			substr: "invalid character",
		},
		{
			name: "gridmap unknown box tile",
			change: func(fsys fstest.MapFS) {
				fsys["levels/keep.gridmap.yaml"].Data = []byte(strings.Replace(sampleGridMapYAML, "tile: stone_floor", "tile: lava_floor", 1))
			},
			file:   "levels/keep.gridmap.yaml",
			marker: "tile: lava_floor",
			substr: `grid map box tile "lava_floor": not found`,
		},
		{
			name: "gridmap unknown cell tile",
			change: func(fsys fstest.MapFS) {
				fsys["levels/keep.gridmap.yaml"].Data = []byte(strings.Replace(sampleGridMapYAML, "tile: stone_pillar", "tile: statue", 1))
			},
			file:   "levels/keep.gridmap.yaml",
			marker: "tile: statue",
			substr: `grid map cell tile "statue": not found`,
		},
		{
			name: "terrain unknown field",
			change: func(fsys fstest.MapFS) {
				fsys["terrain/alpine.terrain.yaml"].Data = []byte(sampleTerrainYAML + "extraKey: 1\n")
			},
			file:   "terrain/alpine.terrain.yaml",
			marker: "extraKey: 1",
			substr: "extraKey",
		},
		{
			name: "terrain missing heightmap",
			change: func(fsys fstest.MapFS) {
				delete(fsys, "terrain/height.png")
			},
			file:   "terrain/alpine.terrain.yaml",
			marker: "heightmap:",
			substr: "terrain heightmap",
		},
		{
			name: "terrain 1x1 heightmap too small",
			change: func(fsys fstest.MapFS) {
				fsys["terrain/height.png"] = &fstest.MapFile{Data: pngBytes(t, 1, 1)}
			},
			file:   "terrain/alpine.terrain.yaml",
			marker: "heightmap:",
			substr: "need at least 2x2",
		},
		{
			name: "terrain missing splat map",
			change: func(fsys fstest.MapFS) {
				delete(fsys, "terrain/splat0.png")
			},
			file:   "terrain/alpine.terrain.yaml",
			marker: "terrain/splat0.png",
			substr: "terrain splat map",
		},
		{
			name: "terrain missing layer albedo",
			change: func(fsys fstest.MapFS) {
				delete(fsys, "textures/grass_albedo.png")
			},
			file:   "terrain/alpine.terrain.yaml",
			marker: "albedo:",
			substr: "terrain layer albedo",
		},
		{
			name: "foliage unknown field",
			change: func(fsys fstest.MapFS) {
				fsys["terrain/meadow.foliage.yaml"].Data = []byte(sampleFoliageYAML + "unknownOpt: true\n")
			},
			file:   "terrain/meadow.foliage.yaml",
			marker: "unknownOpt: true",
			substr: "unknownOpt",
		},
		{
			name: "foliage missing terrain",
			change: func(fsys fstest.MapFS) {
				delete(fsys, "terrain/alpine.terrain.yaml")
			},
			file:   "terrain/meadow.foliage.yaml",
			marker: "terrain:",
			substr: "foliage terrain",
		},
		{
			name: "foliage missing mesh",
			change: func(fsys fstest.MapFS) {
				delete(fsys, "models/grass.glb")
			},
			file:   "terrain/meadow.foliage.yaml",
			marker: "models/grass.glb",
			substr: "foliage mesh",
		},
		{
			name: "foliage missing densityMap",
			change: func(fsys fstest.MapFS) {
				delete(fsys, "terrain/forest_density.png")
			},
			file:   "terrain/meadow.foliage.yaml",
			marker: "densityMap:",
			substr: "foliage densityMap",
		},
		{
			name: "foliage corrupt densityMap PNG",
			change: func(fsys fstest.MapFS) {
				fsys["terrain/forest_density.png"].Data = []byte("not a png")
			},
			file:   "terrain/meadow.foliage.yaml",
			marker: "densityMap:",
			substr: "does not decode",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := world3DProject(t)
			tc.change(fsys)
			r := mustCheck(t, fsys)
			if len(r.Problems) != 1 {
				t.Fatalf("got %d problems, want 1:\n%s", len(r.Problems), problems(r))
			}
			p := r.Problems[0]
			if p.File != tc.file {
				t.Fatalf("problem file = %q, want %q (%s)", p.File, tc.file, p)
			}
			if tc.marker != "" {
				wantLine := lineOf(t, string(fsys[tc.file].Data), tc.marker)
				if p.Line != wantLine {
					t.Fatalf("problem line = %d, want %d (%s)", p.Line, wantLine, p)
				}
			}
			if !strings.Contains(p.Message, tc.substr) {
				t.Fatalf("problem message %q does not contain %q", p.Message, tc.substr)
			}
		})
	}
}
