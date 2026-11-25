package editor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// fullBlobTilesetDoc builds a valid TilesetDoc with an 8-bit blob terrain ("grass") mapping all 47 corner-gated masks to local tile IDs 0..46, with explicit default tile 0.
func fullBlobTilesetDoc() *tilemap.TilesetDoc {
	def := 0
	doc := &tilemap.TilesetDoc{
		Version:    tilemap.TilesetFormatVersion,
		Name:       "grass_blob",
		TileWidth:  16,
		TileHeight: 16,
		Image:      "tiles/grass.png",
		Columns:    8,
		TileCount:  48,
		Terrains: []tilemap.TerrainDoc{
			{Name: "grass", Mode: "blob", Default: &def},
		},
	}
	id := 0
	for m := range 256 {
		mask := uint8(m)
		if !tilemap.ValidTerrainMask(tilemap.Autotile8, mask) {
			continue
		}
		doc.Tiles = append(doc.Tiles, tilemap.TileDoc{
			ID:        id,
			Terrain:   "grass",
			Neighbors: tilemap.TerrainMaskNeighbors(mask),
		})
		id++
	}
	return doc
}

// fullCardinalTilesetDoc builds a valid TilesetDoc with a 4-bit cardinal terrain ("stone") mapping all 16 masks to local tile IDs 0..15.
func fullCardinalTilesetDoc() *tilemap.TilesetDoc {
	doc := &tilemap.TilesetDoc{
		Version:    tilemap.TilesetFormatVersion,
		Name:       "stone_cardinal",
		TileWidth:  16,
		TileHeight: 16,
		Image:      "tiles/stone.png",
		Columns:    4,
		TileCount:  16,
		Terrains: []tilemap.TerrainDoc{
			{Name: "stone", Mode: "cardinal"},
		},
	}
	for m := range 16 {
		mask := uint8(m)
		doc.Tiles = append(doc.Tiles, tilemap.TileDoc{
			ID:        m,
			Terrain:   "stone",
			Neighbors: tilemap.TerrainMaskNeighbors(mask),
		})
	}
	return doc
}

func terrainTestEditor(t *testing.T, tsDoc *tilemap.TilesetDoc, w, h int) (*editor.Editor, *tilemap.TilesetDoc) {
	t.Helper()
	tsYAML, err := tilemap.EncodeTileset(tsDoc)
	if err != nil {
		t.Fatalf("EncodeTileset: %v", err)
	}
	emptyRow := strings.TrimSpace(strings.Repeat(". ", w))
	rows := make([]string, h)
	for i := range rows {
		rows[i] = emptyRow
	}
	sceneDoc := &sceneformat.SceneDoc{
		Version:       2,
		Name:          "Terrain Test",
		VirtualWidth:  float32(w * 16),
		VirtualHeight: float32(h * 16),
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "ground",
				Space: sceneformat.SpaceWorld,
				Order: 0,
				Tilemap: &sceneformat.TilemapDoc{
					TileWidth:   16,
					TileHeight:  16,
					Width:       w,
					Height:      h,
					TilesetFile: "tilesets/terrain.tileset.yaml",
					TileLayers: []sceneformat.TileLayerDoc{
						{Name: "floor", Rows: rows},
					},
				},
			},
		},
	}
	ed, err := editor.New(sceneDoc, sceneformat.FSAssets{
		FS: fstest.MapFS{
			"tilesets/terrain.tileset.yaml": {Data: tsYAML},
		},
	})
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if ed.BuildErr != nil {
		t.Fatalf("BuildErr: %v", ed.BuildErr)
	}
	ed.SetActiveLayer("ground")
	ed.SetCanvasSize(640, 480)
	return ed, tsDoc
}

func readEditorCells(t *testing.T, ed *editor.Editor) []uint32 {
	t.Helper()
	cells, err := ed.Doc.Layers[0].Tilemap.TileLayer("floor").Cells()
	if err != nil {
		t.Fatalf("Cells: %v", err)
	}
	return cells
}

func applyRuntimeAutotile(t *testing.T, tsDoc *tilemap.TilesetDoc, terrainName string, w, h int, occupied [][2]int) []uint32 {
	t.Helper()
	m := tilemap.New(w, h, 16, 16)
	if _, err := m.AddTilesetDoc(tsDoc, 1, tilemap.TilesetAssets{}); err != nil {
		t.Fatalf("AddTilesetDoc: %v", err)
	}
	data := make([]uint32, w*h)
	tr := m.Terrain(terrainName)
	if tr == nil {
		t.Fatalf("Terrain(%q) is nil", terrainName)
	}
	seedGID := tr.Autotile.Default
	if seedGID == 0 {
		seedGID = 1
	}
	for _, cell := range occupied {
		data[cell[1]*w+cell[0]] = seedGID
	}
	m.Layers = append(m.Layers, tilemap.NewTileLayer("floor", w, h, data))
	m.ApplyAutotile(0, tr.Autotile, tr.Contains)
	return m.Layers[0].Data
}

// Acceptance #1: Painting a blob of terrain in the editor gives the exact same tile IDs as ApplyAutotile with the same set.
func TestTerrainBrushMatchesApplyAutotileBlobAndCardinal(t *testing.T) {
	t.Run("8-bit blob via single stroke", func(t *testing.T) {
		ed, tsDoc := terrainTestEditor(t, fullBlobTilesetDoc(), 6, 6)
		ed.SetTool(editor.ToolTerrain)

		// Paint an irregular blob with inner corners, edges, peninsula, and a 3x3 block in ONE stroke:
		// (1,1)-(3,1), (3,2), (3,3)-(1,3), (1,2), (2,2), (4,2)
		strokePath := [][2]int{
			{1, 1}, {2, 1}, {3, 1},
			{3, 2}, {4, 2},
			{3, 2}, {3, 3}, {2, 3}, {1, 3},
			{1, 2}, {2, 2},
		}
		x0, y0 := ed.WorldToCanvas(float32(strokePath[0][0]*16+8), float32(strokePath[0][1]*16+8))
		if !ed.BeginTileStroke(editor.TileStrokeTerrain, x0, y0) {
			t.Fatal("BeginTileStroke(TileStrokeTerrain) returned false")
		}
		for _, p := range strokePath[1:] {
			x, y := ed.WorldToCanvas(float32(p[0]*16+8), float32(p[1]*16+8))
			ed.ContinueTileStroke(x, y)
		}
		ed.EndTileStroke()

		if ed.History.Len() != 1 {
			t.Fatalf("history len = %d, want 1 undo step for the whole terrain stroke", ed.History.Len())
		}

		// Collect every cell that ended up occupied by the stroke and compare against runtime ApplyAutotile.
		got := readEditorCells(t, ed)
		var occupied [][2]int
		for r := range 6 {
			for c := range 6 {
				if got[r*6+c] != 0 {
					occupied = append(occupied, [2]int{c, r})
				}
			}
		}
		if len(occupied) != 10 {
			t.Fatalf("painted %d cells, want 10", len(occupied))
		}
		want := applyRuntimeAutotile(t, tsDoc, "grass", 6, 6, occupied)
		if !slices.Equal(got, want) {
			t.Fatalf("editor blob cells =\n%v\nwant ApplyAutotile =\n%v", got, want)
		}

		// Verify the built runtime Tilemap in ed.Built matches too.
		builtMap := ed.Built.Layer("ground").Map
		for r := range 6 {
			for c := range 6 {
				if bg := builtMap.TileAt(0, c, r); bg != want[r*6+c] {
					t.Fatalf("Built map (%d,%d) = %d, want %d", c, r, bg, want[r*6+c])
				}
			}
		}

		// One Undo removes the entire blob cleanly.
		if !ed.Undo() {
			t.Fatal("Undo returned false")
		}
		for i, v := range readEditorCells(t, ed) {
			if v != 0 {
				t.Fatalf("after Undo cell %d = %d, want 0", i, v)
			}
		}
		if !ed.Redo() || !slices.Equal(readEditorCells(t, ed), want) {
			t.Fatalf("after Redo cells did not match ApplyAutotile")
		}
	})

	t.Run("4-bit cardinal via PaintTerrain", func(t *testing.T) {
		ed, tsDoc := terrainTestEditor(t, fullCardinalTilesetDoc(), 4, 4)
		cells := [][2]int{{1, 0}, {1, 1}, {2, 1}, {1, 2}, {0, 1}}
		for _, p := range cells {
			if err := ed.PaintTerrain("ground", p[0], p[1], "stone"); err != nil {
				t.Fatalf("PaintTerrain(%d,%d): %v", p[0], p[1], err)
			}
		}
		got := readEditorCells(t, ed)
		want := applyRuntimeAutotile(t, tsDoc, "stone", 4, 4, cells)
		if !slices.Equal(got, want) {
			t.Fatalf("editor cardinal cells = %v, want ApplyAutotile %v", got, want)
		}
	})
}

// Acceptance #2: Erasing terrain fixes the neighbors' tiles, and one Undo restores the stroke.
func TestTerrainEraseFixesNeighborsAndOneUndoRestoresStroke(t *testing.T) {
	ed, tsDoc := terrainTestEditor(t, fullBlobTilesetDoc(), 5, 5)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 680))

	app.Press("id:tool-terrain")
	if ed.Tool != editor.ToolTerrain {
		t.Fatalf("Tool = %v, want ToolTerrain", ed.Tool)
	}
	if !strings.Contains(app.Find("id:editor-status-mode").Text, "| Terrain: grass") {
		t.Fatalf("status bar = %q, want Terrain: grass", app.Find("id:editor-status-mode").Text)
	}

	// Paint a full 3x3 blob at (1,1)..(3,3) in one stroke.
	ed.FrameAll2D()
	dragBlob := func(btn core.MouseButton, pts [][2]int) {
		p0 := canvasAt(ed, float32(pts[0][0]*16+8), float32(pts[0][1]*16+8))
		app.MouseDownAt("id:editor-viewport-frame", p0, btn)
		for _, pt := range pts[1:] {
			app.HoverAt("id:editor-viewport-frame", canvasAt(ed, float32(pt[0]*16+8), float32(pt[1]*16+8)))
		}
		last := pts[len(pts)-1]
		app.MouseUpAt("id:editor-viewport-frame", canvasAt(ed, float32(last[0]*16+8), float32(last[1]*16+8)), btn)
	}

	dragBlob(core.MouseButtonLeft, [][2]int{
		{1, 1}, {3, 1}, {3, 2}, {1, 2}, {1, 3}, {3, 3},
	})
	beforeErase := readEditorCells(t, ed)
	wantFull := applyRuntimeAutotile(t, tsDoc, "grass", 5, 5, [][2]int{
		{1, 1}, {2, 1}, {3, 1},
		{1, 2}, {2, 2}, {3, 2},
		{1, 3}, {2, 3}, {3, 3},
	})
	if !slices.Equal(beforeErase, wantFull) {
		t.Fatalf("3x3 painted blob = %v, want %v", beforeErase, wantFull)
	}
	stepsBeforeErase := ed.History.Len()

	// Erase (2,1) and (2,2) in ONE right-button drag with ToolTerrain.
	dragBlob(core.MouseButtonRight, [][2]int{{2, 1}, {2, 2}})

	afterErase := readEditorCells(t, ed)
	if afterErase[1*5+2] != 0 || afterErase[2*5+2] != 0 {
		t.Fatalf("erased cells (2,1),(2,2) = (%d,%d), want (0,0)", afterErase[1*5+2], afterErase[2*5+2])
	}
	// Every remaining neighbor (including diagonal corners like (1,1) and (3,3)!) must match ApplyAutotile for the U-shaped remaining blob.
	wantAfterErase := applyRuntimeAutotile(t, tsDoc, "grass", 5, 5, [][2]int{
		{1, 1}, {3, 1},
		{1, 2}, {3, 2},
		{1, 3}, {2, 3}, {3, 3},
	})
	if !slices.Equal(afterErase, wantAfterErase) {
		t.Fatalf("after erase cells =\n%v\nwant ApplyAutotile =\n%v", afterErase, wantAfterErase)
	}
	if ed.History.Len() != stepsBeforeErase+1 {
		t.Fatalf("erase stroke recorded %d steps, want 1", ed.History.Len()-stepsBeforeErase)
	}

	// ONE Undo restores BOTH the erased cells AND every neighbor's previous tile ID.
	app.Press("id:toolbar-undo")
	if got := readEditorCells(t, ed); !slices.Equal(got, beforeErase) {
		t.Fatalf("after one Undo cells =\n%v\nwant exact pre-erase blob =\n%v", got, beforeErase)
	}
	// Redo re-applies the erase and neighbor updates.
	app.Press("id:toolbar-redo")
	if got := readEditorCells(t, ed); !slices.Equal(got, wantAfterErase) {
		t.Fatalf("after Redo cells =\n%v\nwant post-erase =\n%v", got, wantAfterErase)
	}
}

func TestTerrainStrokeCancelAndLockedLayer(t *testing.T) {
	ed, _ := terrainTestEditor(t, fullBlobTilesetDoc(), 4, 4)
	ed.SetTool(editor.ToolTerrain)

	// Paint (1,1) first so cancelling a neighbor stroke must also restore (1,1)'s tile.
	if err := ed.PaintTerrain("ground", 1, 1, "grass"); err != nil {
		t.Fatal(err)
	}
	before := readEditorCells(t, ed)
	x0, y0 := ed.WorldToCanvas(2*16+8, 1*16+8)
	x1, y1 := ed.WorldToCanvas(3*16+8, 1*16+8)
	if !ed.BeginTileStroke(editor.TileStrokeTerrain, x0, y0) {
		t.Fatal("BeginTileStroke failed")
	}
	ed.ContinueTileStroke(x1, y1)
	if mid := readEditorCells(t, ed); mid[1*4+1] == before[1*4+1] {
		t.Fatal("neighbor (1,1) did not update mid-stroke")
	}
	ed.CancelTileStroke()
	if got := readEditorCells(t, ed); !slices.Equal(got, before) {
		t.Fatalf("after CancelTileStroke cells = %v, want %v", got, before)
	}
	if ed.History.Len() != 1 {
		t.Fatalf("cancelled stroke changed history len to %d, want 1", ed.History.Len())
	}

	// Locked and hidden layers refuse terrain strokes.
	if err := ed.SetLayerLocked("ground", true); err != nil {
		t.Fatal(err)
	}
	if ed.BeginTileStroke(editor.TileStrokeTerrain, x0, y0) {
		t.Fatal("terrain stroke started on locked layer")
	}
	_ = ed.SetLayerLocked("ground", false)
	_ = ed.SetLayerVisible("ground", false)
	if ed.BeginTileStroke(editor.TileStrokeTerrain, x0, y0) {
		t.Fatal("terrain stroke started on hidden layer")
	}
}

// End-to-end Tileset Editor test: open tileset from palette, add terrain, click edges & corners, handle conflicts & reassign, save YAML, and paint with Terrain brush.
func TestTilesetEditorAssignsEdgesAndCornersAndSaves(t *testing.T) {
	dir := t.TempDir()
	tsPath := filepath.Join(dir, "tilesets", "cave.tileset.yaml")
	if err := os.MkdirAll(filepath.Dir(tsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	initialTS := &tilemap.TilesetDoc{
		Version:    1,
		Name:       "cave",
		TileWidth:  16,
		TileHeight: 16,
		Image:      "tiles/cave.png",
		Columns:    4,
		TileCount:  8,
	}
	rawTS, err := tilemap.EncodeTileset(initialTS)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tsPath, rawTS, 0o644); err != nil {
		t.Fatal(err)
	}

	sceneDoc := &sceneformat.SceneDoc{
		Version:       2,
		Name:          "Cave",
		VirtualWidth:  64,
		VirtualHeight: 64,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "ground",
				Space: sceneformat.SpaceWorld,
				Tilemap: &sceneformat.TilemapDoc{
					TileWidth:   16,
					TileHeight:  16,
					Width:       3,
					Height:      3,
					TilesetFile: "tilesets/cave.tileset.yaml",
					TileLayers: []sceneformat.TileLayerDoc{
						{Name: "floor", Rows: []string{". . .", ". . .", ". . ."}},
					},
				},
			},
		},
	}
	ed, err := editor.New(sceneDoc, sceneformat.FSAssets{FS: os.DirFS(dir)})
	if err != nil {
		t.Fatal(err)
	}
	ed.SetActiveLayer("ground")
	var savedBytes []byte
	ed.OnSaveTileset = func(data []byte) error {
		savedBytes = append([]byte(nil), data...)
		return os.WriteFile(tsPath, data, 0o644)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	// Shortcut 't' switches to ToolTerrain.
	app.Key("t")
	if ed.Tool != editor.ToolTerrain {
		t.Fatalf("Tool = %v after pressing 't', want ToolTerrain", ed.Tool)
	}
	if !app.Exists("id:tile-terrain-empty") {
		t.Fatal("expected empty-terrains hint before adding a terrain")
	}

	// Open the tileset editor from the palette.
	app.Press("id:tile-palette-edit-tileset")
	if !app.Exists("id:tileset-editor-panel") || !ed.TilesetOpen() {
		t.Fatal("Tileset Editor panel did not open")
	}

	// Add an 8-bit blob terrain named "moss".
	app.Focus("id:tileset-new-terrain-name")
	app.Type("id:tileset-new-terrain-name", "moss")
	app.Press("id:tileset-new-terrain-mode-blob")
	app.Press("id:tileset-add-terrain")
	if ed.TilesetSelectedTerrain() != "moss" {
		t.Fatalf("selected terrain = %q, want moss", ed.TilesetSelectedTerrain())
	}

	// Assign tile 0 as mask 0 (center) and explicit default.
	app.Press("id:tileset-tile-0")
	app.Press("id:tileset-mask-center")
	app.Press("id:tileset-set-default")

	// Assign tile 1 to [east].
	app.Press("id:tileset-tile-1")
	app.Press("id:tileset-mask-e")

	// Select tile 2: click East -> conflicts with tile 1 without corrupting tile 1!
	app.Press("id:tileset-tile-2")
	app.Press("id:tileset-mask-e")
	if ed.TilesetMaskConflictTile() != 1 || !app.Exists("id:tileset-mask-conflict") {
		t.Fatalf("expected conflict with tile 1, got %d", ed.TilesetMaskConflictTile())
	}
	// Click South -> mask becomes [east, south] (free!) -> immediately assigned to tile 2!
	app.Press("id:tileset-mask-s")
	if ed.TilesetMaskConflictTile() != -1 {
		t.Fatalf("conflict did not clear after adding South: %d", ed.TilesetMaskConflictTile())
	}
	// Click SE corner -> adds southeast -> [east, south, southeast].
	app.Press("id:tileset-mask-se")

	// Select tile 3: click NE corner directly -> auto-enables north, east, northeast!
	app.Press("id:tileset-tile-3")
	app.Press("id:tileset-mask-ne")
	doc := ed.TilesetDoc()
	tr := doc.Terrain("moss", 1)
	if tr == nil {
		t.Fatal("moss terrain missing from TilesetDoc")
	}
	if tr.Autotile.Default != 1 {
		t.Fatalf("Default = %d, want 1 (tile 0)", tr.Autotile.Default)
	}
	if got := tr.Autotile.Tile(tilemap.MaskEast); got != 2 {
		t.Fatalf("mask E -> GID %d, want 2 (tile 1)", got)
	}
	if got := tr.Autotile.Tile(tilemap.MaskEast | tilemap.MaskSouth | tilemap.MaskSouthEast); got != 3 {
		t.Fatalf("mask E|S|SE -> GID %d, want 3 (tile 2)", got)
	}
	if got := tr.Autotile.Tile(tilemap.MaskNorth | tilemap.MaskEast | tilemap.MaskNorthEast); got != 4 {
		t.Fatalf("mask N|E|NE -> GID %d, want 4 (tile 3)", got)
	}

	// Clicking North on tile 3 clears North AND Northeast, leaving East (conflict with tile 1).
	app.Press("id:tileset-mask-n")
	if ed.TilesetMaskConflictTile() != 1 {
		t.Fatalf("after clearing N on tile 3, conflict = %d, want 1", ed.TilesetMaskConflictTile())
	}
	// Press Reassign -> moves [east] from tile 1 to tile 3 in one undo step, then Undo restores tile 1.
	app.Press("id:tileset-mask-reassign")
	if got := ed.TilesetDoc().Terrain("moss", 1).Autotile.Tile(tilemap.MaskEast); got != 4 {
		t.Fatalf("after reassign, mask E -> GID %d, want 4 (tile 3)", got)
	}
	app.Press("id:toolbar-undo")
	if got := ed.TilesetDoc().Terrain("moss", 1).Autotile.Tile(tilemap.MaskEast); got != 2 {
		t.Fatalf("after undo of reassign, mask E -> GID %d, want 2 (tile 1)", got)
	}

	// Save the tileset and verify deterministic round-trip YAML on disk.
	if !ed.TilesetDirty() {
		t.Fatal("TilesetDirty is false before saving")
	}
	app.Press("id:tileset-editor-save")
	if ed.TilesetDirty() || len(savedBytes) == 0 {
		t.Fatalf("after save: dirty=%v bytes=%d", ed.TilesetDirty(), len(savedBytes))
	}
	parsed, err := tilemap.ParseTileset(savedBytes)
	if err != nil {
		t.Fatalf("ParseTileset(saved): %v", err)
	}
	reencoded, err := tilemap.EncodeTileset(parsed)
	if err != nil || string(reencoded) != string(savedBytes) {
		t.Fatalf("saved tileset is not canonical YAML:\n%s", savedBytes)
	}

	// Verify cardinal mode disables corner buttons.
	app.Focus("id:tileset-new-terrain-name")
	app.Type("id:tileset-new-terrain-name", "brick")
	app.Press("id:tileset-new-terrain-mode-cardinal")
	app.Press("id:tileset-add-terrain")
	if !app.Find("id:tileset-mask-ne").Props.Disabled {
		t.Fatal("NE corner button should be disabled in cardinal (4-bit) mode")
	}
	if app.Find("id:tileset-mask-n").Props.Disabled {
		t.Fatal("N edge button should be enabled in cardinal (4-bit) mode")
	}

	// Switch back to "moss" and paint two adjacent cells (0,0) and (1,0) -> (0,0) must get tile 1 (GID 2, mask East).
	app.Press("id:tile-terrain-moss")
	if err := ed.PaintTerrain("ground", 0, 0, ""); err != nil {
		t.Fatalf("PaintTerrain(0,0): %v", err)
	}
	if err := ed.PaintTerrain("ground", 1, 0, ""); err != nil {
		t.Fatalf("PaintTerrain(1,0): %v", err)
	}
	cells := readEditorCells(t, ed)
	if cells[0] != 2 || cells[1] != 1 {
		t.Fatalf("painted moss cells (0,0),(1,0) = (%d,%d), want (2,1) (mask E -> tile 1, mask W -> default tile 0)", cells[0], cells[1])
	}

	// Verify ApplicationMenu includes Terrain tool under View > Tools.
	if item, ok := findMenuItem(app.Menu, "View", "Tools", "Terrain"); !ok || !item.Checked {
		t.Fatalf("View > Tools > Terrain menu item = %+v, ok=%v", item, ok)
	}

	app.Press("id:tileset-editor-close")
	if ed.TilesetOpen() || app.Exists("id:tileset-editor-panel") {
		t.Fatal("Tileset Editor did not close")
	}
	_ = fmt.Sprint()
}
