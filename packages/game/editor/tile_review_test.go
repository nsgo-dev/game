package editor_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const dungeonTilesetYAML = `version: 1
name: dungeon
tileWidth: 16
tileHeight: 16
image: art/tiles/dungeon.png
columns: 6
tileCount: 18
terrains:
  - name: cave
    mode: cardinal
tiles:
  - id: 0
    name: stone
    solid: true
  - id: 1
    name: ledge_moss
    solid: true
  - id: 2
    name: brick
    solid: true
  - id: 3
    name: plank
    oneWay: true
  - id: 4
    name: ladder
  - id: 5
    name: water
  - id: 6
    name: cave_a
    solid: true
    terrain: cave
    neighbors: []
  - id: 7
    name: cave_b
    solid: true
    terrain: cave
    neighbors: [north]
  - id: 8
    name: cave_c
    solid: true
    terrain: cave
    neighbors: [east]
  - id: 9
    name: moss
    solid: true
    terrain: cave
    neighbors: [west]
  - id: 10
    name: cave_wall
    solid: true
    terrain: cave
    neighbors: [north, south]
  - id: 11
    name: cave_ceil
    solid: true
    terrain: cave
    neighbors: [east, west]
  - id: 12
    name: cave_corner_nw
    solid: true
    terrain: cave
    neighbors: [east, south]
  - id: 13
    name: cave_corner_ne
    solid: true
    terrain: cave
    neighbors: [west, south]
  - id: 14
    name: cave_fill
    solid: true
    terrain: cave
    neighbors: [north, east, south, west]
  - id: 15
    name: placeholder
    solid: true
`

func encodeDungeonSheetPNG(t *testing.T) []byte {
	t.Helper()
	const cols, rows, cell = 6, 3, 16
	img := image.NewNRGBA(image.Rect(0, 0, cols*cell, rows*cell))
	swatches := []color.NRGBA{
		{74, 79, 92, 255},    // 0 stone
		{90, 143, 99, 255},   // 1 ledge_moss
		{54, 59, 70, 255},    // 2 brick
		{122, 102, 80, 255},  // 3 plank
		{138, 116, 88, 255},  // 4 ladder
		{58, 109, 132, 255},  // 5 water
		{92, 84, 74, 255},    // 6 cave_a
		{98, 89, 78, 255},    // 7 cave_b
		{89, 82, 72, 255},    // 8 cave_c
		{118, 181, 127, 255}, // 9 moss
		{46, 43, 39, 255},    // 10 cave_wall
		{56, 51, 45, 255},    // 11 cave_ceil
		{61, 56, 51, 255},    // 12 cave_corner_nw
		{65, 59, 53, 255},    // 13 cave_corner_ne
		{72, 66, 60, 255},    // 14 cave_fill
		{138, 79, 141, 255},  // 15 placeholder
		{122, 102, 80, 255},  // 16 extra
		{154, 161, 171, 255}, // 17 extra
	}
	for id, c := range swatches {
		cx, cy := (id%cols)*cell, (id/cols)*cell
		for y := range cell {
			for x := range cell {
				px := c
				if x == 0 || y == 0 {
					px.R = uint8(min(int(px.R)+18, 255))
					px.G = uint8(min(int(px.G)+18, 255))
					px.B = uint8(min(int(px.B)+18, 255))
				} else if x == cell-1 || y == cell-1 {
					px.R = uint8(max(int(px.R)-18, 0))
					px.G = uint8(max(int(px.G)-18, 0))
					px.B = uint8(max(int(px.B)-18, 0))
				}
				img.SetNRGBA(cx+x, cy+y, px)
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

// buildDungeonTileRows builds a 24x20 grid modeled on docs/design/editor/Tiles.dc.html.
// When withPlaceholders is true, three contiguous placeholder cells sit at (6,13), (7,13), (8,13);
// when false, the 11x5 pocket at columns 2..12, rows 12..16 is completely empty.
func buildDungeonTileRows(withPlaceholders bool) []string {
	const cols, rows = 24, 20
	grid := make([]uint32, cols*rows)
	set := func(c, r, localID int) {
		grid[r*cols+c] = uint32(localID + 1)
	}
	clearCell := func(c, r int) {
		grid[r*cols+c] = 0
	}
	for r := range rows {
		for c := range cols {
			switch {
			case r == 0 || c == 0 || c == cols-1:
				set(c, r, 2) // brick
			case r == 10:
				set(c, r, 1) // ledge_moss
			case r > 10:
				set(c, r, 0) // stone
			}
			// Known 11x5 empty pocket at cols 2..12, rows 12..16.
			if r >= 12 && r <= 16 && c >= 2 && c <= 12 {
				clearCell(c, r)
			}
			// Water pool on the right.
			if r >= 10 && r <= 12 && c >= 16 && c <= 19 {
				set(c, r, 5)
			}
			// Ladder at column 12 down into the pocket.
			if c == 12 && r >= 10 && r <= 11 {
				set(c, r, 4)
			}
		}
	}
	for _, c := range []int{8, 9, 10, 11} {
		set(c, 6, 3) // plank
	}
	if withPlaceholders {
		set(6, 13, 15)
		set(7, 13, 15)
		set(8, 13, 15)
	}
	out := make([]string, rows)
	for r := range rows {
		out[r] = sceneformat.FormatTileRow(grid[r*cols : (r+1)*cols])
	}
	return out
}

func newDungeonTileEditor(t *testing.T, withPlaceholders bool) *editor.Editor {
	t.Helper()
	dir := t.TempDir()
	write := func(rel string, data []byte) {
		t.Helper()
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("game.yaml", []byte(`version: 1
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
`))
	write("art/tiles/dungeon.png", encodeDungeonSheetPNG(t))
	write("dungeon.tileset.yaml", []byte(dungeonTilesetYAML))

	zoom := float32(1)
	doc := &sceneformat.SceneDoc{
		Version:       2,
		Name:          "level_01",
		VirtualWidth:  384,
		VirtualHeight: 320,
		Camera:        &sceneformat.CameraDoc{X: 192, Y: 160, Zoom: &zoom},
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "World",
				Space: sceneformat.SpaceWorld,
				Tilemap: &sceneformat.TilemapDoc{
					Width:       24,
					Height:      20,
					TileWidth:   16,
					TileHeight:  16,
					TilesetFile: "dungeon.tileset.yaml",
					TileLayers: []sceneformat.TileLayerDoc{
						{
							Name: "ground",
							Rows: buildDungeonTileRows(withPlaceholders),
						},
					},
				},
			},
			{
				Name:  "Actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{ID: "knight", X: 56, Y: 144},
				},
			},
		},
	}
	sceneBytes, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("sceneformat.Encode: %v", err)
	}
	write("scenes/level_01.scene.yaml", sceneBytes)

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.ProjectAssets = true
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.SetActiveLayer("World")
	return ed
}

func snapshotLayerCells(t *testing.T, ed *editor.Editor, layerName string) []uint32 {
	t.Helper()
	for i := range ed.Doc.Layers {
		if ed.Doc.Layers[i].Name == layerName {
			cells, err := ed.Doc.Layers[i].Tilemap.TileLayers[0].Cells()
			if err != nil {
				t.Fatalf("Cells: %v", err)
			}
			return cells
		}
	}
	t.Fatalf("layer %q not found", layerName)
	return nil
}

func TestTileQueryFindsKnownEmptyPocket(t *testing.T) {
	ed := newDungeonTileEditor(t, false)
	res, err := ed.QueryTiles(editor.TileQueryOptions{
		Layer:     "World",
		EmptyOnly: true,
		Rect:      &editor.TileRect{Col0: 0, Row0: 11, Col1: 23, Row1: 19},
	})
	if err != nil {
		t.Fatalf("QueryTiles: %v", err)
	}
	if res.Layer != "World" || res.Width != 24 || res.Height != 20 {
		t.Fatalf("unexpected result header: %+v", res)
	}
	if len(res.Regions) != 1 {
		t.Fatalf("Regions = %d, want 1 (%+v)", len(res.Regions), res.Regions)
	}
	pocket := res.Regions[0]
	wantBounds := editor.TileRect{Col0: 2, Row0: 12, Col1: 12, Row1: 16}
	if pocket.Bounds != wantBounds || pocket.Width != 11 || pocket.Height != 5 || pocket.CellCount != 55 || !pocket.Rectangular {
		t.Fatalf("pocket = %+v, want 11x5 rectangular pocket at %+v", pocket, wantBounds)
	}
	if !strings.Contains(pocket.Summary, "an 11 × 5 pocket at columns 2–12") {
		t.Fatalf("summary = %q, want 'an 11 × 5 pocket at columns 2–12...'", pocket.Summary)
	}
	if len(pocket.Cells) != 55 {
		t.Fatalf("len(pocket.Cells) = %d, want 55", len(pocket.Cells))
	}
}

// Acceptance criterion 1:
// An agent paint of a rectangle and an erase of 3 cells shows as 2 regions;
// reverting 1 region and clicking Keep keeps only the other region as ONE undoable step
// (Undo restores the original tilemap cleanly).
func TestTileReviewPaintAndEraseTwoRegionsRevertOneKeepOneUndoStep(t *testing.T) {
	ed := newDungeonTileEditor(t, true)
	original := snapshotLayerCells(t, ed, "World")
	histBefore := ed.History.Index()

	// 1. Agent paints a rectangle of 11 cave floor cells using 4 cave tiles.
	paintRes, err := ed.AgentPaintTiles(editor.AgentTilePaintRequest{
		Layer: "World",
		Weighted: []editor.AgentTileWeighted{
			{TileName: "cave_a", Weight: 1},
			{TileName: "cave_b", Weight: 1},
			{TileName: "cave_c", Weight: 1},
			{TileName: "moss", Weight: 0.5},
		},
		Seed:                 42,
		NoAdjacentDuplicates: true,
		Rects:                []editor.AgentTileRect{{Col0: 2, Row0: 16, Col1: 12, Row1: 16}},
		Label:                "Cave floor",
		TrackReview:          true,
	})
	if err != nil {
		t.Fatalf("AgentPaintTiles: %v", err)
	}
	if paintRes.ChangedCells != 11 || !paintRes.ReviewOpen || paintRes.RegionCount != 1 {
		t.Fatalf("paintRes = %+v", paintRes)
	}

	// 2. Agent erases 3 placeholder cells.
	eraseRes, err := ed.AgentEraseTiles(editor.AgentTileEraseRequest{
		Layer:       "World",
		Cells:       []editor.TileCoord{{Col: 6, Row: 13}, {Col: 7, Row: 13}, {Col: 8, Row: 13}},
		Label:       "Placeholders",
		TrackReview: true,
	})
	if err != nil {
		t.Fatalf("AgentEraseTiles: %v", err)
	}
	if eraseRes.ChangedCells != 3 || !eraseRes.ReviewOpen || eraseRes.RegionCount != 2 {
		t.Fatalf("eraseRes = %+v, want 2 regions", eraseRes)
	}

	regions := ed.TileReviewRegions()
	if len(regions) != 2 {
		t.Fatalf("len(regions) = %d, want 2", len(regions))
	}
	if regions[0].Kind != editor.TileReviewPainted || len(regions[0].Cells) != 11 {
		t.Fatalf("region[0] = %+v, want painted 11 cells", regions[0])
	}
	if !strings.Contains(regions[0].Detail, "from ") || !strings.Contains(regions[0].Detail, "cave tiles") {
		t.Fatalf("region[0].Detail = %q, want 'from N cave tiles'", regions[0].Detail)
	}
	if regions[1].Kind != editor.TileReviewErased || len(regions[1].Cells) != 3 {
		t.Fatalf("region[1] = %+v, want erased 3 cells", regions[1])
	}

	// 3. Revert region 1 (the erase of 3 placeholder cells).
	if err := ed.SetTileReviewRegionReverted(regions[1].ID, true); err != nil {
		t.Fatalf("SetTileReviewRegionReverted: %v", err)
	}
	if kept, total := ed.TileReviewKeptCount(); kept != 1 || total != 2 {
		t.Fatalf("kept/total = %d/%d, want 1/2", kept, total)
	}
	// Verify placeholder cells are immediately restored in the live map while cave floor remains painted.
	mid := snapshotLayerCells(t, ed, "World")
	for _, col := range []int{6, 7, 8} {
		if mid[13*24+col] != original[13*24+col] {
			t.Fatalf("placeholder (%d,13) = %d, want restored original %d", col, mid[13*24+col], original[13*24+col])
		}
	}
	for col := 2; col <= 12; col++ {
		if mid[16*24+col] == 0 {
			t.Fatalf("cave floor (%d,16) unexpectedly empty", col)
		}
	}

	// 4. Keep 1 of 2 -> commits ONLY the painted rectangle in ONE undoable step!
	keepRes, err := ed.KeepTileReview()
	if err != nil {
		t.Fatalf("KeepTileReview: %v", err)
	}
	if keepRes.ReviewOpen || keepRes.KeptRegions != 1 || keepRes.RegionCount != 2 || keepRes.ChangedCells != 11 {
		t.Fatalf("keepRes = %+v", keepRes)
	}
	if ed.TileReviewOpen() {
		t.Fatal("review should be closed after KeepTileReview")
	}
	if ed.History.Index() != histBefore+1 || ed.History.Len() != histBefore+1 {
		t.Fatalf("History index/len = %d/%d, want %d/%d (exactly ONE undo step)",
			ed.History.Index(), ed.History.Len(), histBefore+1, histBefore+1)
	}
	afterKeep := snapshotLayerCells(t, ed, "World")
	if !slices.Equal(afterKeep, mid) {
		t.Fatal("grid after Keep differs from composed kept state")
	}

	// 5. ONE Undo restores the exact original tilemap cleanly!
	if !ed.Undo() {
		t.Fatal("Undo() returned false")
	}
	afterUndo := snapshotLayerCells(t, ed, "World")
	if !slices.Equal(afterUndo, original) {
		t.Fatal("Undo() did not restore the exact original tilemap")
	}
	if ed.History.Index() != histBefore {
		t.Fatalf("History.Index() after Undo = %d, want %d", ed.History.Index(), histBefore)
	}

	// 6. Redo re-applies ONLY the kept painted rectangle.
	if !ed.Redo() {
		t.Fatal("Redo() returned false")
	}
	afterRedo := snapshotLayerCells(t, ed, "World")
	if !slices.Equal(afterRedo, afterKeep) {
		t.Fatal("Redo() did not re-apply the exact kept tilemap")
	}
}

func TestTileReviewAutotileBeforeAfterHoldBAndRevertAll(t *testing.T) {
	ed := newDungeonTileEditor(t, true)
	original := snapshotLayerCells(t, ed, "World")
	histBefore := ed.History.Index()

	// Paint cave floor, autotile cave edge, erase placeholders.
	if _, err := ed.AgentPaintTiles(editor.AgentTilePaintRequest{
		Layer:       "World",
		TileName:    "cave_a",
		Rects:       []editor.AgentTileRect{{Col0: 2, Row0: 16, Col1: 12, Row1: 16}},
		Label:       "Cave floor",
		TrackReview: true,
	}); err != nil {
		t.Fatalf("AgentPaintTiles: %v", err)
	}
	place := true
	autoRes, err := ed.AgentAutotile(editor.AgentTileAutotileRequest{
		Layer:   "World",
		Terrain: "cave",
		Rects: []editor.TileRect{
			{Col0: 2, Row0: 11, Col1: 11, Row1: 11},
			{Col0: 1, Row0: 12, Col1: 1, Row1: 16},
			{Col0: 13, Row0: 12, Col1: 13, Row1: 16},
		},
		Place:       &place,
		Label:       "Cave edge",
		TrackReview: true,
	})
	if err != nil {
		t.Fatalf("AgentAutotile: %v", err)
	}
	if autoRes.ChangedCells == 0 {
		t.Fatal("expected autotile to change cells")
	}
	if _, err := ed.AgentEraseTiles(editor.AgentTileEraseRequest{
		Layer:       "World",
		Cells:       []editor.TileCoord{{Col: 6, Row: 13}, {Col: 7, Row: 13}, {Col: 8, Row: 13}},
		Label:       "Placeholders",
		TrackReview: true,
	}); err != nil {
		t.Fatalf("AgentEraseTiles: %v", err)
	}

	regions := ed.TileReviewRegions()
	if len(regions) < 3 {
		t.Fatalf("expected at least 3 regions (painted, replaced, erased), got %d: %+v", len(regions), regions)
	}
	afterCells := snapshotLayerCells(t, ed, "World")

	// Hold B peeks Before; releasing B returns to After.
	if !ed.TileReviewHandleKeyEvent(core.KeyEvent{Key: "b"}, true) {
		t.Fatal("expected B keydown to be handled during tile review")
	}
	if !ed.TileReviewShowingBefore() || !ed.TileReviewPeekingBefore() {
		t.Fatal("expected Before peek active while holding B")
	}
	if !slices.Equal(snapshotLayerCells(t, ed, "World"), original) {
		t.Fatal("live grid did not show Before state while holding B")
	}
	ed.TileReviewHandleKeyEvent(core.KeyEvent{Key: "b"}, false)
	if ed.TileReviewShowingBefore() || ed.TileReviewPeekingBefore() {
		t.Fatal("expected After state after releasing B")
	}
	if !slices.Equal(snapshotLayerCells(t, ed, "World"), afterCells) {
		t.Fatal("live grid did not return to After state after releasing B")
	}

	// Modifier chords must not be swallowed.
	if ed.TileReviewHandleKeyEvent(core.KeyEvent{Key: "b", Modifiers: core.Modifiers{Control: true}}, true) {
		t.Fatal("Ctrl+B must not be swallowed")
	}
	if ed.TileReviewHandleKeyEvent(core.KeyEvent{Key: "b", Modifiers: core.Modifiers{Platform: true}}, true) {
		t.Fatal("Cmd+B must not be swallowed")
	}

	// Toggle Before / After via SetTileReviewViewMode.
	ed.SetTileReviewViewMode(editor.TileReviewBefore)
	if !ed.TileReviewShowingBefore() || !slices.Equal(snapshotLayerCells(t, ed, "World"), original) {
		t.Fatal("expected Before mode to show original tilemap")
	}
	ed.SetTileReviewViewMode(editor.TileReviewAfter)
	if ed.TileReviewShowingBefore() || !slices.Equal(snapshotLayerCells(t, ed, "World"), afterCells) {
		t.Fatal("expected After mode to show proposed tilemap")
	}

	// Revert all restores original tilemap and clean History.
	if err := ed.RevertAllTileReview(); err != nil {
		t.Fatalf("RevertAllTileReview: %v", err)
	}
	if ed.TileReviewOpen() {
		t.Fatal("review still open after RevertAllTileReview")
	}
	if !slices.Equal(snapshotLayerCells(t, ed, "World"), original) {
		t.Fatal("RevertAllTileReview did not restore original tilemap")
	}
	if ed.History.Index() != histBefore {
		t.Fatalf("History.Index() after RevertAll = %d, want %d", ed.History.Index(), histBefore)
	}
}

// Acceptance criterion 3:
// Real nsgotest UI verification + macOS screenshots of tile review After/Before, palette with TILESET & picked tile metadata, and viewport caption.
func TestTileReviewUIAndScreenshot(t *testing.T) {
	ed := newDungeonTileEditor(t, true)
	ed.SetLanguage(editor.LanguageEnglish)
	ed.SetAppearance(editor.AppearanceDark)
	original := snapshotLayerCells(t, ed, "World")
	ed.SetTool(editor.ToolPaintTile)
	ed.SetActiveTileID(10) // moss (local ID 9 -> cell 3, 1)

	if _, err := ed.AgentPaintTiles(editor.AgentTilePaintRequest{
		Layer: "World",
		Weighted: []editor.AgentTileWeighted{
			{TileName: "cave_a", Weight: 1},
			{TileName: "cave_b", Weight: 1},
			{TileName: "cave_c", Weight: 1},
			{TileName: "moss", Weight: 0.5},
		},
		Seed:                 7,
		NoAdjacentDuplicates: true,
		Rects:                []editor.AgentTileRect{{Col0: 2, Row0: 16, Col1: 12, Row1: 16}},
		Label:                "Cave floor",
		TrackReview:          true,
	}); err != nil {
		t.Fatalf("AgentPaintTiles: %v", err)
	}
	place := true
	if _, err := ed.AgentAutotile(editor.AgentTileAutotileRequest{
		Layer:   "World",
		Terrain: "cave",
		Rects: []editor.TileRect{
			{Col0: 2, Row0: 11, Col1: 11, Row1: 11},
			{Col0: 1, Row0: 12, Col1: 1, Row1: 16},
		},
		Place:       &place,
		Label:       "Cave edge",
		TrackReview: true,
	}); err != nil {
		t.Fatalf("AgentAutotile: %v", err)
	}
	if _, err := ed.AgentEraseTiles(editor.AgentTileEraseRequest{
		Layer:       "World",
		Cells:       []editor.TileCoord{{Col: 6, Row: 13}, {Col: 7, Row: 13}, {Col: 8, Row: 13}},
		Label:       "Placeholders",
		TrackReview: true,
	}); err != nil {
		t.Fatalf("AgentEraseTiles: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true}))

	// Verify viewport tile caption.
	if got := app.Find("id:viewport-tile-caption-layer").Text; got != "World" {
		t.Fatalf("caption layer = %q, want World", got)
	}
	if got := app.Find("id:viewport-tile-caption-dims").Text; got != "24 × 20 cells · 16 px" {
		t.Fatalf("caption dims = %q, want '24 × 20 cells · 16 px'", got)
	}

	// Verify tile palette header & picked tile name + atlas cell.
	if got := app.Find("id:tile-palette-title").Text; got != "TILESET" {
		t.Fatalf("palette title = %q, want TILESET", got)
	}
	if got := app.Find("id:tile-palette-tileset").Text; got != "dungeon.tileset.yaml" {
		t.Fatalf("palette tileset = %q, want dungeon.tileset.yaml", got)
	}
	if got := app.Find("id:tile-palette-selected-name").Text; got != "moss" {
		t.Fatalf("selected tile name = %q, want moss", got)
	}
	if got := app.Find("id:tile-palette-selected-meta").Text; !strings.Contains(got, "cell 3, 1") || !strings.Contains(got, "solid") || !strings.Contains(got, "cave") {
		t.Fatalf("selected tile meta = %q, want cell 3, 1 + solid + cave", got)
	}

	// Verify tile review bar, legend, and right-hand review panel.
	for _, id := range []string{
		"id:tile-review-bar",
		"id:tile-review-before",
		"id:tile-review-after",
		"id:tile-review-hold-b",
		"id:tile-review-legend",
		"id:tile-review-panel",
		"id:tile-review-header",
		"id:tile-review-region-0",
		"id:tile-review-region-1",
		"id:tile-review-region-2",
		"id:tile-review-keep",
		"id:tile-review-revert-all",
	} {
		if !app.Exists(id) {
			t.Fatalf("missing expected tile review UI node %s", id)
		}
	}
	if got := app.Find("id:tile-review-keep-text").Text; got != "Keep 3 of 3" {
		t.Fatalf("keep button text = %q, want 'Keep 3 of 3'", got)
	}

	// Real macOS screenshot of After state with region outlines, palette, caption, and review card.
	app.Screenshot("tile-review-after")

	// Toggle Before and capture screenshot.
	app.Press("id:tile-review-before")
	if !ed.TileReviewShowingBefore() {
		t.Fatal("expected Before view after clicking Before")
	}
	if app.Exists("id:tile-review-legend") {
		t.Fatal("legend should hide in Before view")
	}
	app.Screenshot("tile-review-before")

	// Return to After, revert region 2 (Placeholders), verify button updates to Restore & Keep 2 of 3, then Keep.
	app.Press("id:tile-review-after")
	app.Press("id:tile-review-region-toggle-2")
	if got := app.Find("id:tile-review-region-toggle-text-2").Text; got != "Restore" {
		t.Fatalf("reverted region toggle text = %q, want Restore", got)
	}
	if got := app.Find("id:tile-review-keep-text").Text; got != "Keep 2 of 3" {
		t.Fatalf("keep button text = %q, want 'Keep 2 of 3'", got)
	}

	app.Press("id:tile-review-keep")
	if ed.TileReviewOpen() {
		t.Fatal("tile review should close after pressing Keep")
	}
	if app.Exists("id:tile-review-panel") {
		t.Fatal("tile-review-panel should unmount after Keep")
	}

	// Undo once -> restores original tilemap cleanly!
	app.Press("id:toolbar-undo")
	if !slices.Equal(snapshotLayerCells(t, ed, "World"), original) {
		t.Fatal("toolbar Undo after Keep did not restore original tilemap")
	}
}
