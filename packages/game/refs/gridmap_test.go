package refs_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/refs"
)

func TestScanAndPlanMoveGridMap(t *testing.T) {
	fsys := fstest.MapFS{
		"scenes/level.scene.yaml": {Data: []byte(`version: 2
layers:
  - name: world
    objects:
      - id: keep
        gridMap: levels/dungeon.gridmap.yaml
`)},
		"levels/dungeon.gridmap.yaml": {Data: []byte(`version: 1
name: Dungeon
meshLibrary: models/tiles.gltf
cells:
  - at: [0, 0, 0]
    tile: floor
`)},
		"models/tiles.gltf": {Data: []byte(`{"asset":{"version":"2.0"}}`)},
	}

	proj, err := refs.Scan(fsys)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	var sawGridMap, sawMeshLib bool
	for _, r := range proj.Refs {
		if r.File == "scenes/level.scene.yaml" && r.Field == refs.FieldGridMap && r.Target == "levels/dungeon.gridmap.yaml" {
			sawGridMap = true
		}
		if r.File == "levels/dungeon.gridmap.yaml" && r.Field == refs.FieldMeshLibrary && r.Target == "models/tiles.gltf" {
			sawMeshLib = true
		}
	}
	if !sawGridMap || !sawMeshLib {
		t.Fatalf("missing refs: sawGridMap=%v sawMeshLib=%v refs=%+v", sawGridMap, sawMeshLib, proj.Refs)
	}

	// Moving the GridMap file rewrites the scene's gridMap reference.
	plan, err := refs.PlanMove(fsys, "levels/dungeon.gridmap.yaml", "maps/keep.gridmap.yaml")
	if err != nil {
		t.Fatalf("PlanMove gridmap: %v", err)
	}
	rewroteScene := false
	for _, w := range plan.Writes {
		if w.Path == "scenes/level.scene.yaml" && strings.Contains(string(w.Data), "gridMap: maps/keep.gridmap.yaml") {
			rewroteScene = true
		}
	}
	if !rewroteScene {
		t.Fatalf("PlanMove did not rewrite scene gridMap ref: %+v", plan.Writes)
	}

	// Moving the glTF mesh library rewrites the GridMap's meshLibrary reference.
	planLib, err := refs.PlanMove(fsys, "models/tiles.gltf", "assets/dungeon_tiles.gltf")
	if err != nil {
		t.Fatalf("PlanMove meshLibrary: %v", err)
	}
	rewroteGrid := false
	for _, w := range planLib.Writes {
		if w.Path == "levels/dungeon.gridmap.yaml" && strings.Contains(string(w.Data), "meshLibrary: assets/dungeon_tiles.gltf") {
			rewroteGrid = true
		}
	}
	if !rewroteGrid {
		t.Fatalf("PlanMove did not rewrite gridmap meshLibrary ref: %+v", planLib.Writes)
	}
}
