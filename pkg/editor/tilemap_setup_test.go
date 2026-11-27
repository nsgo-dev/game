package editor_test

import (
	"bytes"
	"math"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// tilemapSetupResolver provides "tiles" (1, 2, 3), "alt" (1, 2, 3, 4), "narrow" (1, 2), and "actors" (non-tile frames).
func tilemapSetupResolver() sceneformat.AtlasMap {
	r := testResolver()
	r["alt"] = &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:alt.png"},
		Frames: map[string]sprite.Frame{
			"1": {Name: "1", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			"2": {Name: "2", X: 16, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			"3": {Name: "3", X: 32, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			"4": {Name: "4", X: 48, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		},
	}
	r["narrow"] = &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:narrow.png"},
		Frames: map[string]sprite.Frame{
			"1": {Name: "1", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			"2": {Name: "2", X: 16, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		},
	}
	return r
}

// Acceptance #1: A new scene can get a painted tilemap without editing YAML by hand.
func TestNewSceneGetsPaintedTilemapFromUI(t *testing.T) {
	ed, err := editor.New(nil, tilemapSetupResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	initialYAML := mustSave(t, ed)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 900))

	// Switch to Paint tool on the new scene's layer (which has no tilemap yet), then press Set Up Tilemap.
	runEditorCommand(t, app, ed, editor.CmdToolPaintTile)
	if !app.Exists("id:tile-palette-setup-layer") {
		t.Fatal("expected Set Up Tilemap button in empty tile palette for a layer with no tilemap")
	}
	app.Press("id:tile-palette-setup-layer")
	if !app.Exists("id:inspector-layer-tilemap-add") {
		t.Fatal("expected Add Tilemap controls in Layer Inspector")
	}

	// Choose tileset atlas "tiles", tile size 16x16, grid size 6x4, and add the tilemap.
	app.Press("id:inspector-layer-tilemap-add-tileset")
	app.Press("role:option tiles")
	app.Clear("id:inspector-layer-tilemap-add-tile-w")
	app.Type("id:inspector-layer-tilemap-add-tile-w", "16")
	app.Submit("id:inspector-layer-tilemap-add-tile-w")
	app.Clear("id:inspector-layer-tilemap-add-tile-h")
	app.Type("id:inspector-layer-tilemap-add-tile-h", "16")
	app.Submit("id:inspector-layer-tilemap-add-tile-h")
	app.Clear("id:inspector-layer-tilemap-add-width")
	app.Type("id:inspector-layer-tilemap-add-width", "6")
	app.Submit("id:inspector-layer-tilemap-add-width")
	app.Clear("id:inspector-layer-tilemap-add-height")
	app.Type("id:inspector-layer-tilemap-add-height", "4")
	app.Submit("id:inspector-layer-tilemap-add-height")
	app.Press("id:inspector-layer-tilemap-add")

	tm := ed.Doc.Layers[0].Tilemap
	if tm == nil || tm.Tileset != "tiles" || tm.TileWidth != 16 || tm.TileHeight != 16 || tm.Width != 6 || tm.Height != 4 {
		t.Fatalf("added tilemap = %+v, want tiles 6x4 (16x16)", tm)
	}
	if ed.BuildErr != nil {
		t.Fatalf("BuildErr after adding tilemap: %v", ed.BuildErr)
	}

	// Frame the new tilemap in the 2D viewport, pick tile 2 in the palette, and paint cell (2, 1).
	app.Press("id:viewport-bar-fit")
	app.Press("id:tile-palette-2")
	cx, cy := ed.WorldToCanvas(2*16+8, 1*16+8)
	app.PressAt("id:editor-viewport", core.Point{X: core.Points(cx), Y: core.Points(cy)})

	if got := ed.Doc.Layers[0].Tilemap.Data[1*6+2]; got != 2 {
		t.Fatalf("painted cell (2,1) = %d, want 2", got)
	}

	// Saved file parses and encodes unchanged.
	saved := mustSave(t, ed)
	parsed, err := sceneformat.Parse(saved)
	if err != nil {
		t.Fatalf("Parse saved: %v", err)
	}
	again, err := sceneformat.Encode(parsed)
	if err != nil || !bytes.Equal(again, saved) {
		t.Fatalf("saved scene does not round-trip unchanged (%v)", err)
	}
	if parsed.Layers[0].Tilemap.Data[1*6+2] != 2 {
		t.Fatalf("parsed cell (2,1) = %d, want 2", parsed.Layers[0].Tilemap.Data[1*6+2])
	}

	// Undoing paint and add restores the original scene byte-for-byte.
	undoInUI(app, ed)
	undoInUI(app, ed)
	if after := mustSave(t, ed); !bytes.Equal(after, initialYAML) {
		t.Fatalf("Undo did not restore new scene:\ngot:\n%s\nwant:\n%s", after, initialYAML)
	}
}

// Acceptance #2: Growing a map to the left and top keeps every painted tile at its world position,
// and the saved file round-trips through Parse and Encode unchanged.
func TestResizeTilemapLeftTopKeepsWorldPositionsAndRoundTrips(t *testing.T) {
	ed, err := editor.New(testSceneDoc(), tilemapSetupResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SelectObject("")
	ed.SetActiveLayer("floor")
	// Record world positions of every painted tile before resizing.
	spritePositions := func() map[[2]float32]string {
		out := make(map[[2]float32]string)
		for _, sp := range ed.Built.Layer("floor").Tilemap.Sprites {
			out[[2]float32{sp.X, sp.Y}] = sp.Frame.Name
		}
		return out
	}
	beforePos := spritePositions()
	if len(beforePos) == 0 {
		t.Fatal("expected painted tiles in testSceneDoc")
	}
	beforeYAML := mustSave(t, ed)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 1000))

	// Grow 2 columns to the left, 1 column to the right, 3 rows to the top, and 1 row to the bottom via UI.
	app.Clear("id:inspector-layer-tilemap-resize-left")
	app.Type("id:inspector-layer-tilemap-resize-left", "2")
	app.Submit("id:inspector-layer-tilemap-resize-left")
	app.Clear("id:inspector-layer-tilemap-resize-right")
	app.Type("id:inspector-layer-tilemap-resize-right", "1")
	app.Submit("id:inspector-layer-tilemap-resize-right")
	app.Clear("id:inspector-layer-tilemap-resize-top")
	app.Type("id:inspector-layer-tilemap-resize-top", "3")
	app.Submit("id:inspector-layer-tilemap-resize-top")
	app.Clear("id:inspector-layer-tilemap-resize-bottom")
	app.Type("id:inspector-layer-tilemap-resize-bottom", "1")
	app.Submit("id:inspector-layer-tilemap-resize-bottom")
	app.Press("id:inspector-layer-tilemap-resize")

	if ed.BuildErr != nil {
		t.Fatalf("BuildErr after resize: %v", ed.BuildErr)
	}
	tm := ed.Doc.Layers[0].Tilemap
	if tm.Width != 7 || tm.Height != 8 {
		t.Fatalf("resized size = %dx%d, want 7x8", tm.Width, tm.Height)
	}
	if tm.OffsetX != -32 || tm.OffsetY != -48 {
		t.Fatalf("resized offset = (%v, %v), want (-32, -48)", tm.OffsetX, tm.OffsetY)
	}

	// Every painted tile sits at the exact same world position.
	afterPos := spritePositions()
	if len(afterPos) != len(beforePos) {
		t.Fatalf("painted tile count = %d, want %d", len(afterPos), len(beforePos))
	}
	for pos, frame := range beforePos {
		if got := afterPos[pos]; got != frame {
			t.Fatalf("tile at world %v = %q, want %q", pos, got, frame)
		}
	}

	// Newly added left/top cells at negative world coordinates are addressable and paintable.
	cx, cy := ed.WorldToCanvas(-24, -40) // inside col=0 (-32..-16), row=0 (-48..-32)
	col, row, ok := ed.CanvasToTile("floor", cx, cy)
	if !ok || col != 0 || row != 0 {
		t.Fatalf("CanvasToTile(-24, -40) = (%d, %d, %v), want (0, 0, true)", col, row, ok)
	}
	if err := ed.PaintTile("floor", col, row, 3); err != nil {
		t.Fatalf("PaintTile at new top-left cell: %v", err)
	}
	if got := spritePositions()[[2]float32{-32, -48}]; got != "3" {
		t.Fatalf("painted top-left cell world pos (-32, -48) = %q, want 3", got)
	}

	// Round-trip through Parse and Encode unchanged.
	saved := mustSave(t, ed)
	parsed, err := sceneformat.Parse(saved)
	if err != nil {
		t.Fatalf("Parse resized scene: %v", err)
	}
	again, err := sceneformat.Encode(parsed)
	if err != nil || !bytes.Equal(again, saved) {
		t.Fatalf("resized scene does not round-trip unchanged (%v):\ngot:\n%s\nwant:\n%s", err, again, saved)
	}

	// Undo paint + resize restores original bytes and world positions.
	ed.Undo()
	ed.Undo()
	if restored := mustSave(t, ed); !bytes.Equal(restored, beforeYAML) {
		t.Fatalf("Undo did not restore original scene:\ngot:\n%s\nwant:\n%s", restored, beforeYAML)
	}
	if ed.Doc.Layers[0].Tilemap.OffsetX != 0 || ed.Doc.Layers[0].Tilemap.OffsetY != 0 {
		t.Fatalf("offset after undo = (%v, %v), want (0, 0)", ed.Doc.Layers[0].Tilemap.OffsetX, ed.Doc.Layers[0].Tilemap.OffsetY)
	}
}

// Moving a layer's tilemap into top-level tilemaps (ref) and back inline is undoable, works from UI,
// preserves painted cells while referenced, and keeps top-level entries shared by another layer.
func TestExtractAndInlineLayerTilemap(t *testing.T) {
	ed, err := editor.New(testSceneDoc(), tilemapSetupResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SelectObject("")
	ed.SetActiveLayer("floor")
	beforeYAML := mustSave(t, ed)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 1000))

	// Move inline tilemap into top-level tilemaps under "floor_grid".
	app.Clear("id:inspector-layer-tilemap-ref-name")
	app.Type("id:inspector-layer-tilemap-ref-name", "floor_grid")
	app.Press("id:inspector-layer-tilemap-extract")

	if tm := ed.Doc.Layers[0].Tilemap; tm == nil || tm.Ref != "floor_grid" {
		t.Fatalf("layer tilemap after extract = %+v, want ref floor_grid", tm)
	}
	if top := ed.Doc.Tilemaps["floor_grid"]; top == nil || top.Width != 4 || top.Height != 4 {
		t.Fatalf("top-level tilemap = %+v, want 4x4 grid", top)
	}
	if !app.Exists("id:inspector-layer-tilemap-inline") {
		t.Fatal("expected Move Inline button after extracting to ref")
	}

	// Resizing through a ref edits the top-level tilemap.
	app.Post(func() {
		if err := ed.ResizeLayerTilemap("floor", 1, 0, 0, 0); err != nil {
			t.Errorf("ResizeLayerTilemap through ref: %v", err)
		}
	})
	app.Settle()
	if top := ed.Doc.Tilemaps["floor_grid"]; top.Width != 5 || top.OffsetX != -16 {
		t.Fatalf("top-level after resize = width %d offsetX %v, want 5 and -16", top.Width, top.OffsetX)
	}
	undoInUI(app, ed)

	// Move back inline via UI: removes unreferenced top-level entry and matches original YAML byte-for-byte.
	app.Press("id:inspector-layer-tilemap-inline")
	if ed.Doc.Tilemaps != nil {
		t.Fatalf("top-level tilemaps after moving sole ref back inline = %+v, want nil", ed.Doc.Tilemaps)
	}
	if after := mustSave(t, ed); !bytes.Equal(after, beforeYAML) {
		t.Fatalf("extract + inline did not round-trip to original YAML:\ngot:\n%s\nwant:\n%s", after, beforeYAML)
	}

	// Undo inline restores ref; undo extract restores inline.
	ed.Undo()
	if ed.Doc.Layers[0].Tilemap.Ref != "floor_grid" || ed.Doc.Tilemaps["floor_grid"] == nil {
		t.Fatal("Undo of inline did not restore top-level ref")
	}
	ed.Undo()
	if after := mustSave(t, ed); !bytes.Equal(after, beforeYAML) {
		t.Fatalf("Undo of extract did not restore original YAML:\ngot:\n%s\nwant:\n%s", after, beforeYAML)
	}

	// When two layers reference the same top-level tilemap, inlining one keeps the top-level entry for the other.
	if err := ed.ExtractLayerTilemap("floor", "shared_grid"); err != nil {
		t.Fatalf("ExtractLayerTilemap: %v", err)
	}
	ed.Doc.Layers[1].Tilemap = &sceneformat.TilemapDoc{Ref: "shared_grid"}
	ed.Rebuild()
	if err := ed.InlineLayerTilemap("floor"); err != nil {
		t.Fatalf("InlineLayerTilemap shared: %v", err)
	}
	if ed.Doc.Layers[0].Tilemap.IsRef() || ed.Doc.Tilemaps["shared_grid"] == nil {
		t.Fatalf("shared inline broke top-level entry: layer0=%+v tilemaps=%+v", ed.Doc.Layers[0].Tilemap, ed.Doc.Tilemaps)
	}
	if ed.BuildErr != nil {
		t.Fatalf("BuildErr after inlining shared ref: %v", ed.BuildErr)
	}
}

// Changing tileset atlas updates the palette when frames cover painted IDs, refuses missing frames with a status message, and is undoable.
func TestChangeLayerTilemapTilesetAndRefuseMissingFrames(t *testing.T) {
	ed, err := editor.New(testSceneDoc(), tilemapSetupResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	// Paint tile 3 so "narrow" (which only has frames 1 and 2) is missing frame "3".
	if err := ed.PaintTile("floor", 1, 1, 3); err != nil {
		t.Fatalf("PaintTile: %v", err)
	}
	ed.SelectObject("")
	ed.SetActiveLayer("floor")
	beforeYAML := mustSave(t, ed)
	histBefore := ed.History.Len()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 1000))

	// Refused in UI: switching to "narrow" reports missing frame "3" in Status and leaves doc/history unchanged.
	app.Press("id:inspector-layer-tilemap-tileset")
	app.Press("role:option narrow")
	if ed.Doc.Layers[0].Tilemap.Tileset != "tiles" {
		t.Fatalf("tileset changed to %q despite missing frame 3", ed.Doc.Layers[0].Tilemap.Tileset)
	}
	if !strings.Contains(ed.Status, `missing frame "3"`) {
		t.Fatalf("Status = %q, want missing frame \"3\"", ed.Status)
	}
	if ed.History.Len() != histBefore {
		t.Fatalf("refused tileset edit recorded history (%d -> %d)", histBefore, ed.History.Len())
	}

	// Refused via Go API for unknown atlas and actors atlas (missing 1, 2, 3).
	if err := ed.SetLayerTilemapTileset("floor", "unknown"); err == nil || !strings.Contains(err.Error(), "cannot be resolved") {
		t.Fatalf("unknown atlas err = %v", err)
	}
	if err := ed.SetLayerTilemapTileset("floor", "actors"); err == nil || !strings.Contains(err.Error(), "missing frames 1, 3") {
		t.Fatalf("actors atlas err = %v", err)
	}

	// Allowed in UI: switching to "alt" (which has frames 1, 2, 3, 4) succeeds and updates palette.
	app.Press("id:inspector-layer-tilemap-tileset")
	app.Press("role:option alt")
	if ed.Doc.Layers[0].Tilemap.Tileset != "alt" {
		t.Fatalf("tileset = %q, want alt", ed.Doc.Layers[0].Tilemap.Tileset)
	}
	if ed.BuildErr != nil {
		t.Fatalf("BuildErr after switching to alt: %v", ed.BuildErr)
	}
	if p := ed.TilePalette(); !p.Has(4) {
		t.Fatalf("palette after switching to alt = %+v, want tile 4", p.Tiles)
	}

	// Undo restores original tileset byte-for-byte.
	ed.Undo()
	if after := mustSave(t, ed); !bytes.Equal(after, beforeYAML) {
		t.Fatalf("Undo did not restore tileset:\ngot:\n%s\nwant:\n%s", after, beforeYAML)
	}
}

// Removing a layer's tilemap is undoable, works from UI, and leaves top-level tilemaps intact when removing a Ref.
func TestRemoveLayerTilemapAndUndo(t *testing.T) {
	ed, err := editor.New(testSceneDoc(), tilemapSetupResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SelectObject("")
	ed.SetActiveLayer("floor")
	beforeYAML := mustSave(t, ed)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 1000))

	app.Press("id:inspector-layer-tilemap-remove")
	if ed.Doc.Layers[0].Tilemap != nil {
		t.Fatalf("tilemap not removed: %+v", ed.Doc.Layers[0].Tilemap)
	}
	if !app.Exists("id:inspector-layer-tilemap-add") {
		t.Fatal("expected Add Tilemap controls after removing tilemap")
	}

	ed.Undo()
	if after := mustSave(t, ed); !bytes.Equal(after, beforeYAML) {
		t.Fatalf("Undo of remove did not restore tilemap:\ngot:\n%s\nwant:\n%s", after, beforeYAML)
	}
}

// Invalid sizes, overflows, v1/v2 version upgrades, layered grid resizing, inherited layers, and Play mode guards.
func TestTilemapSetupValidationVersionAndLayeredResize(t *testing.T) {
	ed, err := editor.New(nil, tilemapSetupResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}

	// TileAtlasNames lists only atlases with positive integer tile frames.
	if got := ed.TileAtlasNames(); !slices.Equal(got, []string{"alt", "narrow", "tiles"}) {
		t.Fatalf("TileAtlasNames = %v, want [alt narrow tiles]", got)
	}

	// Invalid sizes and missing tilesets on AddLayerTilemap.
	for _, tc := range []struct {
		name          string
		ts            string
		tw, th, w, h  int
		wantErrSubstr string
	}{
		{"zero_tile_w", "tiles", 0, 16, 4, 4, "tile size"},
		{"neg_tile_h", "tiles", 16, -8, 4, 4, "tile size"},
		{"zero_grid_w", "tiles", 16, 16, 0, 4, "grid size"},
		{"neg_grid_h", "tiles", 16, 16, 4, -1, "grid size"},
		{"overflow_grid", "tiles", 16, 16, 10000, 10000, "exceeds"},
		{"empty_tileset", "", 16, 16, 4, 4, "must not be empty"},
		{"missing_tileset", "missing", 16, 16, 4, 4, "cannot be resolved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ed.AddLayerTilemap("main", tc.ts, tc.tw, tc.th, tc.w, tc.h)
			if err == nil || !strings.Contains(err.Error(), tc.wantErrSubstr) {
				t.Fatalf("AddLayerTilemap = %v, want substring %q", err, tc.wantErrSubstr)
			}
		})
	}

	// Version 1 scene stays v1 on right/bottom resize, raises to v3 on left/top resize, and Undo restores v1.
	v1Doc := testSceneDoc()
	v1Doc.Version = 1
	v1Ed, err := editor.New(v1Doc, tilemapSetupResolver())
	if err != nil {
		t.Fatalf("v1 editor.New: %v", err)
	}
	v1Before := mustSave(t, v1Ed)
	if err := v1Ed.ResizeLayerTilemap("floor", 0, 2, 0, 1); err != nil {
		t.Fatalf("v1 right/bottom resize: %v", err)
	}
	if v1Ed.Doc.Version != 1 {
		t.Fatalf("v1 right/bottom resize bumped version to %d, want 1", v1Ed.Doc.Version)
	}
	if err := v1Ed.ResizeLayerTilemap("floor", 1, 0, 1, 0); err != nil {
		t.Fatalf("v1 left/top resize: %v", err)
	}
	if v1Ed.Doc.Version != sceneformat.CurrentVersion {
		t.Fatalf("v1 left/top resize version = %d, want %d", v1Ed.Doc.Version, sceneformat.CurrentVersion)
	}
	v1Ed.Undo()
	if v1Ed.Doc.Version != 1 {
		t.Fatalf("Undo left/top resize version = %d, want 1", v1Ed.Doc.Version)
	}
	v1Ed.Undo()
	if restored := mustSave(t, v1Ed); !bytes.Equal(restored, v1Before) {
		t.Fatalf("Undo all v1 resizes changed YAML:\ngot:\n%s\nwant:\n%s", restored, v1Before)
	}

	// Shrinking to <= 0 is refused.
	if err := v1Ed.ResizeLayerTilemap("floor", -4, 0, 0, 0); err == nil || !strings.Contains(err.Error(), "must be positive") {
		t.Fatalf("shrink to 0 width err = %v", err)
	}
	// Invalid tile size is refused; valid tile size is undoable.
	if err := v1Ed.SetLayerTilemapTileSize("floor", 0, 16); err == nil || !strings.Contains(err.Error(), "must be positive") {
		t.Fatalf("zero tile width err = %v", err)
	}
	if err := v1Ed.SetLayerTilemapTileSize("floor", 32, 24); err != nil {
		t.Fatalf("SetLayerTilemapTileSize: %v", err)
	}
	if v1Ed.Doc.Layers[0].Tilemap.TileWidth != 32 || v1Ed.Doc.Layers[0].Tilemap.TileHeight != 24 {
		t.Fatalf("tile size = %dx%d, want 32x24", v1Ed.Doc.Layers[0].Tilemap.TileWidth, v1Ed.Doc.Layers[0].Tilemap.TileHeight)
	}
	v1Ed.Undo()
	if v1Ed.Doc.Layers[0].Tilemap.TileWidth != 16 || v1Ed.Doc.Layers[0].Tilemap.TileHeight != 16 {
		t.Fatalf("tile size after undo = %dx%d, want 16x16", v1Ed.Doc.Layers[0].Tilemap.TileWidth, v1Ed.Doc.Layers[0].Tilemap.TileHeight)
	}

	// Layered tilemap resize shifts all tile layers and updates Map.Origin.
	fsys := fstest.MapFS{
		"tilesets/cave.tileset.yaml": {Data: []byte("version: 1\nname: cave\ntileWidth: 16\ntileHeight: 16\nimage: tiles/cave.png\ncolumns: 2\ntileCount: 4\n")},
	}
	layeredDoc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "Layered Resize",
		Layers: []sceneformat.LayerDoc{{
			Name: "ground",
			Tilemap: &sceneformat.TilemapDoc{
				TileWidth:   16,
				TileHeight:  16,
				Width:       2,
				Height:      2,
				TilesetFile: "tilesets/cave.tileset.yaml",
				TileLayers: []sceneformat.TileLayerDoc{
					{Name: "floor", Rows: []string{"0 .", ". 1h"}},
				},
			},
		}},
	}
	layeredEd, err := editor.New(layeredDoc, sceneformat.FSAssets{FS: fsys})
	if err != nil {
		t.Fatalf("layered editor.New: %v", err)
	}
	if err := layeredEd.ResizeLayerTilemap("ground", 1, 0, 2, 0); err != nil {
		t.Fatalf("layered ResizeLayerTilemap: %v", err)
	}
	ltm := layeredEd.Doc.Layers[0].Tilemap
	if ltm.Width != 3 || ltm.Height != 4 || ltm.OffsetX != -16 || ltm.OffsetY != -32 {
		t.Fatalf("layered resized = %dx%d offset (%v, %v)", ltm.Width, ltm.Height, ltm.OffsetX, ltm.OffsetY)
	}
	wantRows := []string{". . .", ". . .", ". 0 .", ". . 1h"}
	if !slices.Equal(ltm.TileLayers[0].Rows, wantRows) {
		t.Fatalf("layered rows = %v, want %v", ltm.TileLayers[0].Rows, wantRows)
	}
	if layeredEd.Built.Layer("ground").Map.Origin != (core.Point{X: -16, Y: -32}) {
		t.Fatalf("layered Map.Origin = %+v, want (-16, -32)", layeredEd.Built.Layer("ground").Map.Origin)
	}
	if err := layeredEd.SetLayerTilemapTileset("ground", "tiles"); err == nil || !strings.Contains(err.Error(), "layered tilesetFile") {
		t.Fatalf("SetLayerTilemapTileset on layered = %v", err)
	}

	// Play mode refuses all tilemap setup mutations.
	_ = startEditorView(t, v1Ed)
	if err := v1Ed.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	defer func() { _ = v1Ed.Stop() }()
	if err := v1Ed.ResizeLayerTilemap("floor", 1, 0, 0, 0); err != editor.ErrPlaying {
		t.Fatalf("ResizeLayerTilemap while playing = %v, want ErrPlaying", err)
	}
	if err := v1Ed.RemoveLayerTilemap("floor"); err != editor.ErrPlaying {
		t.Fatalf("RemoveLayerTilemap while playing = %v, want ErrPlaying", err)
	}
}

func TestTilemapSetupNilReceiverOverflowAndTerrainOffsetStroke(t *testing.T) {
	// Nil Editor receiver returns errors instead of panicking on every exported method.
	var nilEd *editor.Editor
	if err := nilEd.AddLayerTilemap("floor", "tiles", 16, 16, 4, 4); err == nil {
		t.Fatal("nil AddLayerTilemap succeeded")
	}
	if err := nilEd.RemoveLayerTilemap("floor"); err == nil {
		t.Fatal("nil RemoveLayerTilemap succeeded")
	}
	if err := nilEd.SetLayerTilemapTileset("floor", "tiles"); err == nil {
		t.Fatal("nil SetLayerTilemapTileset succeeded")
	}
	if err := nilEd.SetLayerTilemapTileSize("floor", 16, 16); err == nil {
		t.Fatal("nil SetLayerTilemapTileSize succeeded")
	}
	if err := nilEd.ResizeLayerTilemap("floor", 1, 0, 0, 0); err == nil {
		t.Fatal("nil ResizeLayerTilemap succeeded")
	}
	if err := nilEd.ExtractLayerTilemap("floor", "tm"); err == nil {
		t.Fatal("nil ExtractLayerTilemap succeeded")
	}
	if err := nilEd.InlineLayerTilemap("floor"); err == nil {
		t.Fatal("nil InlineLayerTilemap succeeded")
	}

	// ResizeLayerTilemap rejects extreme edge deltas that would overflow int/int64/float32.
	ed, err := editor.New(nil, tilemapSetupResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.AddLayerTilemap("", "tiles", 16, 16, 2, 2); err != nil {
		t.Fatalf("AddLayerTilemap: %v", err)
	}
	overflowCases := [][4]int{
		{math.MaxInt, math.MaxInt, 0, 0},
		{-math.MaxInt, math.MaxInt, 0, 0},
		{math.MinInt, 0, 0, 0},
		{0, 0, math.MaxInt, -math.MaxInt},
		{1<<24 + 1, 0, 0, 0},
	}
	for _, tc := range overflowCases {
		if err := ed.ResizeLayerTilemap("", tc[0], tc[1], tc[2], tc[3]); err == nil {
			t.Fatalf("ResizeLayerTilemap(%v) succeeded, want overflow/bounds error", tc)
		}
	}

	// Terrain autotile stroke aligns on an offset tilemap grown to the left and top.
	ted, _ := terrainTestEditor(t, fullBlobTilesetDoc(), 2, 2)
	if err := ted.ResizeLayerTilemap("ground", 2, 0, 1, 0); err != nil {
		t.Fatalf("ResizeLayerTilemap terrain: %v", err)
	}
	tm := ted.Doc.Layers[0].Tilemap
	if tm.OffsetX != -32 || tm.OffsetY != -16 || tm.Width != 4 || tm.Height != 3 {
		t.Fatalf("resized terrain map = %dx%d offset (%v, %v)", tm.Width, tm.Height, tm.OffsetX, tm.OffsetY)
	}
	ted.SetTool(editor.ToolTerrain)
	// Cell (0,0) covers world [-32,-16) x [-16,0); cell (1,0) covers [-16,0) x [-16,0).
	cx0, cy0 := ted.WorldToCanvas(-24, -8)
	cx1, cy1 := ted.WorldToCanvas(-8, -8)
	if !ted.BeginTileStroke(editor.TileStrokeTerrain, cx0, cy0) {
		t.Fatal("BeginTileStroke(TileStrokeTerrain) on negative-offset cell returned false")
	}
	ted.ContinueTileStroke(cx1, cy1)
	ted.EndTileStroke()
	cells := readEditorCells(t, ted)
	if cells[0] == 0 || cells[1] == 0 {
		t.Fatalf("expected terrain cells (0,0) and (1,0) painted on offset map, got %v", cells)
	}
	if cells[2] != 0 || cells[4] != 0 {
		t.Fatalf("unexpected terrain spill outside stroke on offset map: %v", cells)
	}
}
