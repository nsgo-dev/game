package sceneformat_test

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

func TestNavRegion2DParseRoundTripAndBuild(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "canonical", "nav_room.scene.yaml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	reg := sceneformat.NavRegion2DComponentRegistry()
	doc, err := sceneformat.ParseWith(raw, sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	encoded, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(raw, encoded) {
		t.Fatalf("round-trip YAML mismatch:\n--- want ---\n%s\n--- got ---\n%s", raw, encoded)
	}

	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if built.NavBake2D == nil || built.NavMesh2D == nil {
		t.Fatalf("expected NavBake2D and NavMesh2D to be populated, got bake=%v mesh=%v", built.NavBake2D, built.NavMesh2D)
	}
	if built.NavMesh2D.PolygonCount() == 0 {
		t.Fatal("expected non-empty built 2D navmesh")
	}

	// Center of pillar_cutout at (160, 90) must be blocked.
	if built.NavMesh2D.Contains(core.Point{X: 160, Y: 90}) {
		t.Fatal("expected obstacle cutout center (160, 90) to be unwalkable")
	}
	// Endpoints on opposite sides of the pillar must connect around it, not straight through.
	start := core.Point{X: 60, Y: 90}
	goal := core.Point{X: 260, Y: 90}
	path, ok := built.NavMesh2D.FindPath(start, goal)
	if !ok || len(path) < 3 {
		t.Fatalf("FindPath around pillar = (%v, %v), want detour path with >= 3 points", path, ok)
	}
	// Verify the path actually deviates vertically around the pillar (y outside [50, 130]).
	deviated := false
	for _, p := range path {
		if float64(p.Y) <= 50.5 || float64(p.Y) >= 129.5 {
			deviated = true
			break
		}
	}
	if !deviated {
		t.Fatalf("expected path %v to detour past pillar y in [50, 130]", path)
	}
}

type navTileAssets struct {
	sceneformat.AtlasMap
	files map[string][]byte
}

func (a navTileAssets) ReadFile(p string) ([]byte, error) {
	b, ok := a.files[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	return b, nil
}

func TestNavRegion2DTileCollisionSubtraction(t *testing.T) {
	tilesetYAML := []byte(`version: 1
name: world
tileWidth: 16
tileHeight: 16
image: tiles.png
columns: 3
tileCount: 3
tiles:
  - id: 0
    solid: true
  - id: 1
  - id: 2
    shapes:
      - kind: polygon
        points: [[0, 0], [16, 0], [16, 16]]
`)
	// 8x5 grid (128x80). Solid wall (tile 0) in column 4, rows 0..3; row 4 is an open doorway.
	// Also place a sloped triangle tile (tile 2) at (col=1, row=1).
	makeScene := func(subtract bool, tileLayers string) []byte {
		subStr := "false"
		if subtract {
			subStr = "true"
		}
		tl := ""
		if tileLayers != "" {
			tl = "\n            tileLayers: [" + tileLayers + "]"
		}
		return []byte(`version: 2
name: Tile Nav
virtualWidth: 128
virtualHeight: 80
layers:
  - name: world
    space: world
    tilemap:
      tilesetFile: tiles/world.tileset.yaml
      width: 8
      height: 5
      tileWidth: 16
      tileHeight: 16
      tileLayers:
        - name: walls
          rows:
            - ". . . . 0 . . ."
            - ". 2 . . 0 . . ."
            - ". . . . 0 . . ."
            - ". . . . 0 . . ."
            - ". . . . . . . ."
        - name: decor
          rows:
            - ". . . . . . . ."
            - ". . . . . . . ."
            - ". . . . . . . ."
            - ". . . . . . . ."
            - ". . . . 0 . . ."
    objects:
      - id: arena
        x: 0
        "y": 0
        components:
          navregion2d:
            version: 1
            mode: walkable
            points: [[0, 0], [128, 0], [128, 80], [0, 80]]
            subtractTileCollision: ` + subStr + tl + `
`)
	}

	resolver := navTileAssets{
		files: map[string][]byte{"tiles/world.tileset.yaml": tilesetYAML},
	}
	reg := sceneformat.NavRegion2DComponentRegistry()
	start := core.Point{X: 16, Y: 16}
	goal := core.Point{X: 112, Y: 16}

	// 1. Without tile collision subtraction: straight 2-point path straight across the wall.
	docNoSub, err := sceneformat.ParseWith(makeScene(false, ""), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("ParseWith(no sub): %v", err)
	}
	builtNoSub, err := sceneformat.Build(docNoSub, resolver)
	if err != nil {
		t.Fatalf("Build(no sub): %v", err)
	}
	pNoSub, ok := builtNoSub.NavMesh2D.FindPath(start, goal)
	if !ok || len(pNoSub) != 2 {
		t.Fatalf("without subtraction path = (%v, %v), want straight 2-point path", pNoSub, ok)
	}

	// 2. With tile collision subtraction on "walls" layer only: doorway at row 4 (y in [64, 80]) is open,
	// wall center (72, 24) and upper-right triangle interior (28, 20) are subtracted!
	docWalls, err := sceneformat.ParseWith(makeScene(true, "walls"), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("ParseWith(walls): %v", err)
	}
	builtWalls, err := sceneformat.Build(docWalls, resolver)
	if err != nil {
		t.Fatalf("Build(walls): %v", err)
	}
	if builtWalls.NavMesh2D.Contains(core.Point{X: 72, Y: 24}) {
		t.Fatal("expected solid wall cell (col=4, row=1) to be unwalkable")
	}
	if builtWalls.NavMesh2D.Contains(core.Point{X: 28, Y: 20}) {
		t.Fatal("expected solid triangular tile shape at (col=1, row=1) to be subtracted")
	}
	pWalls, ok := builtWalls.NavMesh2D.FindPath(start, goal)
	if !ok || len(pWalls) < 3 {
		t.Fatalf("with walls subtracted path = (%v, %v), want detour through row 4 doorway", pWalls, ok)
	}
	passedDoorway := false
	for _, p := range pWalls {
		if p.Y >= 63.5 {
			passedDoorway = true
			break
		}
	}
	if !passedDoorway {
		t.Fatalf("expected path %v to route down through row 4 doorway (y >= 64)", pWalls)
	}

	// 3. With all tile layers subtracted (empty tileLayers): "decor" plugs row 4, sealing the wall completely!
	docAll, err := sceneformat.ParseWith(makeScene(true, ""), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("ParseWith(all): %v", err)
	}
	builtAll, err := sceneformat.Build(docAll, resolver)
	if err != nil {
		t.Fatalf("Build(all): %v", err)
	}
	if pAll, ok := builtAll.NavMesh2D.FindPath(start, goal); ok {
		t.Fatalf("expected completely sealed wall across all layers to block path, got %v", pAll)
	}
}

func TestNavRegion2DOverlappingOppositeWindingAndChildTransform(t *testing.T) {
	// Region A is CW; Region B is CCW and overlaps Region A; Region B is also under a translated+mirrored parent.
	// Both must union cleanly rather than cancelling out where they overlap!
	raw := []byte(`version: 2
name: Union Regions
virtualWidth: 320
virtualHeight: 180
layers:
  - name: world
    space: world
    objects:
      - id: reg_a
        x: 0
        "y": 0
        components:
          navregion2d:
            version: 1
            points: [[0, 0], [100, 0], [100, 60], [0, 60]]
      - id: parent_xform
        x: 160
        "y": 0
        scaleX: -1
        children:
          - id: reg_b
            x: 0
            "y": 0
            components:
              navregion2d:
                version: 1
                points: [[-40, 0], [-40, 60], [80, 60], [80, 0]]
`)
	doc, err := sceneformat.ParseWith(raw, sceneformat.ParseOptions{Components: sceneformat.NavRegion2DComponentRegistry()})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// parent_xform at x=160 with scaleX=-1 maps local x in [-40, 80] to world x in [80, 200],
	// overlapping reg_a [0, 100] on [80, 100]!
	if !built.NavMesh2D.Contains(core.Point{X: 90, Y: 30}) {
		t.Fatal("overlap point (90, 30) should stay walkable, not cancel out")
	}
	path, ok := built.NavMesh2D.FindPath(core.Point{X: 10, Y: 30}, core.Point{X: 190, Y: 30})
	if !ok || len(path) < 2 {
		t.Fatalf("FindPath across unioned regions = (%v, %v), want connected path", path, ok)
	}
	// SyncNavMesh2D after moving reg_a should rebuild cleanly.
	doc.Layers[0].Objects[0].Y = 10
	built.Object("reg_a").Sync()
	if err := built.SyncNavMesh2D(); err != nil {
		t.Fatalf("SyncNavMesh2D: %v", err)
	}
	if built.NavMesh2D.Contains(core.Point{X: 10, Y: 5}) {
		t.Fatal("expected SyncNavMesh2D to reflect updated object Y=10")
	}
}

func TestNavRegion2DWithOffMeshLinkAndValidation(t *testing.T) {
	// Two disjoint islands connected by a nav_link component.
	reg := sceneformat.NavRegion2DComponentRegistry()
	if err := sceneformat.RegisterNavLink(reg); err != nil {
		t.Fatalf("RegisterNavLink: %v", err)
	}
	raw := []byte(`version: 2
name: Islands
virtualWidth: 320
virtualHeight: 180
layers:
  - name: world
    space: world
    objects:
      - id: island_left
        x: 0
        "y": 0
        components:
          navregion2d:
            version: 1
            points: [[0, 0], [80, 0], [80, 80], [0, 80]]
      - id: island_right
        x: 160
        "y": 0
        components:
          navregion2d:
            version: 1
            points: [[0, 0], [80, 0], [80, 80], [0, 80]]
      - id: jump_bridge
        x: 70
        "y": 40
        components:
          nav_link:
            endX: 100
            endY: 0
            bidirectional: true
`)
	doc, err := sceneformat.ParseWith(raw, sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	path, ok := built.NavMesh2D.FindPath(core.Point{X: 20, Y: 40}, core.Point{X: 220, Y: 40})
	if !ok || len(path) < 2 {
		t.Fatalf("FindPath across nav_link = (%v, %v), want path across bridge", path, ok)
	}
	if math.Abs(float64(path[len(path)-1].X)-220) > 1e-3 {
		t.Fatalf("path goal = %v, want (220, 40)", path[len(path)-1])
	}

	// Scene without any navregion2d keeps NavMesh2D and NavBake2D nil.
	emptyDoc := &sceneformat.SceneDoc{
		Version: 1,
		Name:    "No Nav",
		Layers:  []sceneformat.LayerDoc{{Name: "main", Space: sceneformat.SpaceWorld}},
	}
	emptyBuilt, err := sceneformat.Build(emptyDoc, nil)
	if err != nil {
		t.Fatalf("Build(empty): %v", err)
	}
	if emptyBuilt.NavBake2D != nil || emptyBuilt.NavMesh2D != nil {
		t.Fatalf("expected nil NavBake2D/NavMesh2D when no navregion2d exists")
	}

	// Disabled region produces an empty mesh without error.
	disabledDoc := &sceneformat.SceneDoc{
		Version: 1,
		Name:    "Disabled Nav",
		Layers: []sceneformat.LayerDoc{{
			Name:  "main",
			Space: sceneformat.SpaceWorld,
			Objects: []sceneformat.ObjectDoc{{
				ID: "r1",
				Components: map[string]any{
					sceneformat.NavRegion2DComponentName: map[string]any{
						"enabled": false,
						"points":  [][2]float64{{0, 0}, {50, 0}, {50, 50}},
					},
				},
			}},
		}},
	}
	disabledBuilt, err := sceneformat.Build(disabledDoc, nil)
	if err != nil {
		t.Fatalf("Build(disabled): %v", err)
	}
	if disabledBuilt.NavMesh2D == nil || disabledBuilt.NavMesh2D.PolygonCount() != 0 {
		t.Fatalf("expected empty non-nil NavMesh2D for disabled region, got %v", disabledBuilt.NavMesh2D)
	}

	// Invalid mode fails Build.
	badDoc := &sceneformat.SceneDoc{
		Version: 1,
		Name:    "Bad Nav",
		Layers: []sceneformat.LayerDoc{{
			Name:  "main",
			Space: sceneformat.SpaceWorld,
			Objects: []sceneformat.ObjectDoc{{
				ID: "bad",
				Components: map[string]any{
					sceneformat.NavRegion2DComponentName: map[string]any{
						"mode": "flying",
					},
				},
			}},
		}},
	}
	if _, err := sceneformat.Build(badDoc, nil); err == nil {
		t.Fatal("expected Build to reject invalid navregion2d mode")
	}
}

func TestNavRegion2DOverflowAndDegeneratePolygons(t *testing.T) {
	// float64 values that overflow float32 must be rejected cleanly rather than leaking +Inf.
	overflowCases := []struct {
		name  string
		comp  map[string]any
		world *math2d.Affine
	}{
		{
			name: "agentRadius overflow",
			comp: map[string]any{
				"agentRadius": 1e40,
				"points":      [][2]float64{{0, 0}, {10, 0}, {10, 10}},
			},
		},
		{
			name: "offsetX overflow",
			comp: map[string]any{
				"offsetX": 1e40,
				"points":  [][2]float64{{0, 0}, {10, 0}, {10, 10}},
			},
		},
		{
			name: "point coordinate overflow",
			comp: map[string]any{
				"points": [][2]float64{{1e40, 0}, {10, 0}, {10, 10}},
			},
		},
		{
			name: "point plus offset overflow",
			comp: map[string]any{
				"offsetX": float64(math.MaxFloat32),
				"points":  [][2]float64{{float64(math.MaxFloat32), 0}, {10, 0}, {10, 10}},
			},
		},
		{
			name: "world transform overflow",
			comp: map[string]any{
				"points": [][2]float64{{1e20, 0}, {10, 0}, {10, 10}},
			},
			world: &math2d.Affine{A: 1e20, D: 1},
		},
	}
	for _, tc := range overflowCases {
		t.Run(tc.name, func(t *testing.T) {
			obj := &sceneformat.ObjectDoc{
				ID:         "ov",
				Components: map[string]any{sceneformat.NavRegion2DComponentName: tc.comp},
			}
			if _, _, err := sceneformat.ExtractNavRegion2D(obj, tc.world); err == nil {
				t.Fatalf("ExtractNavRegion2D(%s) succeeded, want float32 overflow error", tc.name)
			}
		})
	}

	// Degenerate zero-area (collinear) polygon produces an empty mesh without error.
	collinearDoc := &sceneformat.SceneDoc{
		Version: 1,
		Name:    "Collinear",
		Layers: []sceneformat.LayerDoc{{
			Name:  "world",
			Space: sceneformat.SpaceWorld,
			Objects: []sceneformat.ObjectDoc{{
				ID: "line",
				Components: map[string]any{
					sceneformat.NavRegion2DComponentName: map[string]any{
						"points": [][2]float64{{0, 0}, {50, 0}, {100, 0}},
					},
				},
			}},
		}},
	}
	colBuilt, err := sceneformat.Build(collinearDoc, nil)
	if err != nil {
		t.Fatalf("Build(collinear): %v", err)
	}
	if colBuilt.NavMesh2D == nil || colBuilt.NavMesh2D.PolygonCount() != 0 {
		t.Fatalf("collinear polygon NavMesh2D = %v, want empty non-nil mesh", colBuilt.NavMesh2D)
	}

	// Self-intersecting (bowtie) polygon builds deterministically without panicking.
	bowtieDoc := &sceneformat.SceneDoc{
		Version: 1,
		Name:    "Bowtie",
		Layers: []sceneformat.LayerDoc{{
			Name:  "world",
			Space: sceneformat.SpaceWorld,
			Objects: []sceneformat.ObjectDoc{{
				ID: "bowtie",
				Components: map[string]any{
					sceneformat.NavRegion2DComponentName: map[string]any{
						"points": [][2]float64{{0, 0}, {100, 100}, {100, 0}, {0, 100}},
					},
				},
			}},
		}},
	}
	b1, err := sceneformat.Build(bowtieDoc, nil)
	if err != nil {
		t.Fatalf("Build(bowtie): %v", err)
	}
	b2, err := sceneformat.Build(bowtieDoc, nil)
	if err != nil {
		t.Fatalf("Build(bowtie) second run: %v", err)
	}
	if b1.NavMesh2D == nil || b2.NavMesh2D == nil || b1.NavMesh2D.PolygonCount() != b2.NavMesh2D.PolygonCount() {
		t.Fatalf("bowtie build non-deterministic: %v vs %v", b1.NavMesh2D, b2.NavMesh2D)
	}
}
