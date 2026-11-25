package editormcp_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

const mcpDungeonTilesetYAML = `version: 1
name: dungeon
tileWidth: 16
tileHeight: 16
image: art/tiles/dungeon.png
columns: 4
tileCount: 12
terrains:
  - name: cave
    mode: cardinal
tiles:
  - id: 0
    name: stone
    solid: true
  - id: 1
    name: brick
    solid: true
  - id: 2
    name: cave_a
    solid: true
    terrain: cave
    neighbors: []
  - id: 3
    name: cave_b
    solid: true
    terrain: cave
    neighbors: [north]
  - id: 4
    name: cave_c
    solid: true
    terrain: cave
    neighbors: [east]
  - id: 5
    name: moss
    solid: true
    terrain: cave
    neighbors: [west]
  - id: 6
    name: cave_wall
    solid: true
    terrain: cave
    neighbors: [north, south]
  - id: 7
    name: cave_ceil
    solid: true
    terrain: cave
    neighbors: [east, west]
  - id: 8
    name: placeholder
    solid: true
`

func openTileFixture(t *testing.T, withPlaceholders bool) (*editor.Editor, string) {
	t.Helper()
	dir := t.TempDir()
	write := func(rel string, data string) {
		t.Helper()
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("game.yaml", `version: 1
name: dungeon-run
id: dev.nsgo.dungeon
appVersion: 1.0.0
window:
  width: 384
  height: 320
autoload:
  mainScene: level_01
  scenes:
    - name: level_01
      path: scenes/level_01.scene.yaml
`)
	write("dungeon.tileset.yaml", mcpDungeonTilesetYAML)

	const cols, rows = 24, 20
	grid := make([]uint32, cols*rows)
	for r := range rows {
		for c := range cols {
			if r == 0 || c == 0 || c == cols-1 {
				grid[r*cols+c] = 2 // brick
			} else if r >= 10 {
				grid[r*cols+c] = 1 // stone
			}
			// Known 11x5 empty pocket at columns 2..12, rows 12..16.
			if r >= 12 && r <= 16 && c >= 2 && c <= 12 {
				grid[r*cols+c] = 0
			}
		}
	}
	if withPlaceholders {
		grid[13*cols+6] = 9 // placeholder (local 8 -> GID 9)
		grid[13*cols+7] = 9
		grid[13*cols+8] = 9
	}
	rowStrs := make([]string, rows)
	for r := range rows {
		rowStrs[r] = sceneformat.FormatTileRow(grid[r*cols : (r+1)*cols])
	}
	doc := &sceneformat.SceneDoc{
		Version:       2,
		Name:          "level_01",
		VirtualWidth:  384,
		VirtualHeight: 320,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "World",
				Space: sceneformat.SpaceWorld,
				Tilemap: &sceneformat.TilemapDoc{
					Width:       cols,
					Height:      rows,
					TileWidth:   16,
					TileHeight:  16,
					TilesetFile: "dungeon.tileset.yaml",
					TileLayers:  []sceneformat.TileLayerDoc{{Name: "ground", Rows: rowStrs}},
				},
			},
		},
	}
	encoded, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("sceneformat.Encode: %v", err)
	}
	write("scenes/level_01.scene.yaml", string(encoded))

	ed := newEditor(t)
	ed.ProjectAssets = true
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.SetActiveLayer("World")
	return ed, dir
}

func readWorldCells(t *testing.T, ed *editor.Editor) []uint32 {
	t.Helper()
	cells, err := ed.Doc.Layers[0].Tilemap.TileLayers[0].Cells()
	if err != nil {
		t.Fatalf("Cells: %v", err)
	}
	return cells
}

// Acceptance criterion 2: tile_query finds a known empty pocket in a fixture tile layer.
func TestTileQueryFindsKnownEmptyPocketMCP(t *testing.T) {
	ed, _ := openTileFixture(t, false)
	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	res := call(t, cs, editormcp.ToolTileQuery, map[string]any{
		"layer": "World",
		"empty": true,
		"rect":  map[string]any{"col0": 0, "row0": 11, "col1": 23, "row1": 19},
	})
	out := mustOK(t, res)
	var qr editor.TileQueryResult
	structured(t, res, &qr)

	if qr.Layer != "World" || qr.MatchedCells != 55 || len(qr.Regions) != 1 {
		t.Fatalf("query result = %+v\ntext:\n%s", qr, out)
	}
	pocket := qr.Regions[0]
	wantBounds := editor.TileRect{Col0: 2, Row0: 12, Col1: 12, Row1: 16}
	if pocket.Bounds != wantBounds || pocket.Width != 11 || pocket.Height != 5 || pocket.CellCount != 55 || !pocket.Rectangular {
		t.Fatalf("pocket = %+v, want 11x5 at %+v", pocket, wantBounds)
	}
	if !strings.Contains(out, "an 11 × 5 pocket at columns 2–12") {
		t.Fatalf("text = %q, want pocket summary", out)
	}
}

// Acceptance criterion 1 via MCP tools:
// tile_paint rectangle + tile_erase 3 cells -> 2 regions -> revert 1 region -> keep -> 1 undo step restores original tilemap cleanly.
func TestTileMCPPaintEraseAutotileAndReviewFlow(t *testing.T) {
	ed, _ := openTileFixture(t, true)
	original := readWorldCells(t, ed)
	histBefore := ed.History.Index()

	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// 1. Paint a rectangle with weighted cave tiles.
	paintRes := call(t, cs, editormcp.ToolTilePaint, map[string]any{
		"layer": "World",
		"rect":  map[string]any{"col0": 2, "row0": 16, "col1": 12, "row1": 16},
		"weighted": []any{
			map[string]any{"tileName": "cave_a", "weight": 1},
			map[string]any{"tileName": "cave_b", "weight": 1},
			map[string]any{"tileName": "cave_c", "weight": 1},
			map[string]any{"tileName": "moss", "weight": 0.5},
		},
		"seed":                 9,
		"noAdjacentDuplicates": true,
		"label":                "Cave floor",
	})
	mustOK(t, paintRes)
	var paintOut editor.AgentTileEditResult
	structured(t, paintRes, &paintOut)
	if paintOut.ChangedCells != 11 || !paintOut.ReviewOpen || paintOut.RegionCount != 1 {
		t.Fatalf("paintOut = %+v", paintOut)
	}

	// 2. Erase 3 placeholder cells.
	eraseRes := call(t, cs, editormcp.ToolTileErase, map[string]any{
		"layer": "World",
		"cells": []any{
			map[string]any{"col": 6, "row": 13},
			map[string]any{"col": 7, "row": 13},
			map[string]any{"col": 8, "row": 13},
		},
		"label": "Placeholders",
	})
	mustOK(t, eraseRes)

	// 3. Inspect review status -> 2 regions (painted + erased).
	stRes := call(t, cs, editormcp.ToolTileReviewStatus, map[string]any{})
	mustOK(t, stRes)
	var st editormcp.TileReviewStatusResult
	structured(t, stRes, &st)
	if !st.Open || st.TotalRegions != 2 || st.KeptRegions != 2 || st.TotalCells != 14 {
		t.Fatalf("status = %+v", st)
	}

	// 4. Revert region 1 (erased), restore it, then revert again by ID.
	revRes := call(t, cs, editormcp.ToolTileReviewRevertRegion, map[string]any{"index": 1})
	mustOK(t, revRes)
	structured(t, revRes, &st)
	if st.KeptRegions != 1 || !st.Regions[1].Reverted {
		t.Fatalf("after revert by index: %+v", st)
	}

	restRes := call(t, cs, editormcp.ToolTileReviewRestoreRegion, map[string]any{"id": st.Regions[1].ID})
	mustOK(t, restRes)
	structured(t, restRes, &st)
	if st.KeptRegions != 2 || st.Regions[1].Reverted {
		t.Fatalf("after restore by id: %+v", st)
	}

	revRes2 := call(t, cs, editormcp.ToolTileReviewRevertRegion, map[string]any{"id": st.Regions[1].ID})
	mustOK(t, revRes2)

	// 5. Toggle Before/After via tile_review_status.
	beforeRes := call(t, cs, editormcp.ToolTileReviewStatus, map[string]any{"view": "before"})
	mustOK(t, beforeRes)
	structured(t, beforeRes, &st)
	if !st.ShowingBefore || !slices.Equal(readWorldCells(t, ed), original) {
		t.Fatal("expected view=before to show original cells")
	}
	afterRes := call(t, cs, editormcp.ToolTileReviewStatus, map[string]any{"view": "after", "focus": st.Regions[0].ID})
	mustOK(t, afterRes)
	structured(t, afterRes, &st)
	if st.ShowingBefore || st.FocusedRegion != st.Regions[0].ID {
		t.Fatalf("after focus status = %+v", st)
	}

	// 6. Keep 1 of 2 -> ONE undo step, only painted rectangle kept.
	keepRes := call(t, cs, editormcp.ToolTileReviewKeep, map[string]any{})
	mustOK(t, keepRes)
	var keepOut editor.AgentTileEditResult
	structured(t, keepRes, &keepOut)
	if keepOut.ReviewOpen || keepOut.KeptRegions != 1 || keepOut.RegionCount != 2 || keepOut.ChangedCells != 11 {
		t.Fatalf("keepOut = %+v", keepOut)
	}
	if ed.History.Index() != histBefore+1 {
		t.Fatalf("History.Index() = %d, want %d", ed.History.Index(), histBefore+1)
	}
	keptCells := readWorldCells(t, ed)
	for _, col := range []int{6, 7, 8} {
		if keptCells[13*24+col] != 9 {
			t.Fatalf("reverted placeholder (%d,13) = %d, want 9", col, keptCells[13*24+col])
		}
	}
	for col := 2; col <= 12; col++ {
		if keptCells[16*24+col] == 0 {
			t.Fatalf("kept cave floor (%d,16) unexpectedly empty", col)
		}
	}

	// 7. Single Undo restores the exact original tilemap.
	if !ed.Undo() {
		t.Fatal("Undo returned false")
	}
	if !slices.Equal(readWorldCells(t, ed), original) {
		t.Fatal("Undo did not restore original tilemap cleanly")
	}

	// 8. Test tile_autotile + tile_review_revert_all.
	autoRes := call(t, cs, editormcp.ToolTileAutotile, map[string]any{
		"layer":   "World",
		"terrain": "cave",
		"rect":    map[string]any{"col0": 2, "row0": 11, "col1": 11, "row1": 11},
		"place":   true,
	})
	mustOK(t, autoRes)
	var autoOut editor.AgentTileEditResult
	structured(t, autoRes, &autoOut)
	if autoOut.ChangedCells != 10 || !autoOut.ReviewOpen {
		t.Fatalf("autoOut = %+v", autoOut)
	}
	revertAllRes := call(t, cs, editormcp.ToolTileReviewRevertAll, map[string]any{})
	mustOK(t, revertAllRes)
	if ed.TileReviewOpen() {
		t.Fatal("review should close after tile_review_revert_all")
	}
	if !slices.Equal(readWorldCells(t, ed), original) {
		t.Fatal("tile_review_revert_all did not restore original tilemap")
	}
}
