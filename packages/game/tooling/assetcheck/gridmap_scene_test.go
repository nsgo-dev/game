package assetcheck_test

import (
	"encoding/binary"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
)

func TestGridMapInSceneAndMissingTileFileAndLine(t *testing.T) {
	sceneYAML := `version: 2
name: Keep
layers:
  - name: world
    objects:
      - id: keep
        gridMap: levels/keep.gridmap.yaml
`
	gridYAML := `version: 1
name: Keep
cellSize: [2, 2, 2]
meshLibrary: models/tiles.glb
cells:
  - at: [0, 0, 0]
    tile: stone_floor
  - at: [1, 0, 0]
    tile: missing_arch
`
	jsonChunk := []byte(`{
  "asset": {"version": "2.0"},
  "nodes": [{"name": "stone_floor", "mesh": 0}],
  "meshes": [{"name": "stone_floor", "primitives": [{"attributes": {"POSITION": 0}}]}],
  "accessors": [{"min": [-1, -1, -1], "max": [1, 1, 1]}]
}`)
	for len(jsonChunk)%4 != 0 {
		jsonChunk = append(jsonChunk, ' ')
	}
	glb := make([]byte, 20+len(jsonChunk))
	binary.LittleEndian.PutUint32(glb[0:4], 0x46546C67)
	binary.LittleEndian.PutUint32(glb[4:8], 2)
	binary.LittleEndian.PutUint32(glb[8:12], uint32(len(glb)))
	binary.LittleEndian.PutUint32(glb[12:16], uint32(len(jsonChunk)))
	binary.LittleEndian.PutUint32(glb[16:20], 0x4E4F534A)
	copy(glb[20:], jsonChunk)

	fsys := fstest.MapFS{
		"scenes/keep.scene.yaml":   {Data: []byte(sceneYAML)},
		"levels/keep.gridmap.yaml": {Data: []byte(gridYAML)},
		"models/tiles.glb":         {Data: glb},
	}

	report, err := assetcheck.Check(fsys)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(report.Problems) != 1 {
		t.Fatalf("got %d problems, want 1: %+v", len(report.Problems), report.Problems)
	}
	p := report.Problems[0]
	if p.File != "levels/keep.gridmap.yaml" {
		t.Fatalf("problem file = %q, want levels/keep.gridmap.yaml", p.File)
	}
	if p.Line != 9 {
		t.Fatalf("problem line = %d, want 9 (problem: %v)", p.Line, p)
	}
	if !strings.Contains(p.Message, "missing_arch") {
		t.Fatalf("problem message %q does not mention missing_arch", p.Message)
	}

	// Missing gridMap referenced by scene reports exact scene file and line.
	delete(fsys, "levels/keep.gridmap.yaml")
	reportMissing, err := assetcheck.Check(fsys)
	if err != nil {
		t.Fatalf("Check missing: %v", err)
	}
	foundSceneErr := false
	for _, pr := range reportMissing.Problems {
		if pr.File == "scenes/keep.scene.yaml" && pr.Line == 7 && strings.Contains(pr.Message, "levels/keep.gridmap.yaml") {
			foundSceneErr = true
		}
	}
	if !foundSceneErr {
		t.Fatalf("expected scene missing gridMap problem at line 7, got %+v", reportMissing.Problems)
	}
}
