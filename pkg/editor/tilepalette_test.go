package editor_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// paletteResolver adds a second tileset to testResolver: "walls", 8x8 tiles numbered 2, 5 and 10, with frames whose names are not tile IDs beside them.
func paletteResolver() sceneformat.AtlasMap {
	r := testResolver()
	r["walls"] = &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:walls.png"},
		Frames: map[string]sprite.Frame{
			"10":     {Name: "10", X: 16, Y: 0, W: 8, H: 8, Width: 8, Height: 8},
			"2":      {Name: "2", X: 0, Y: 0, W: 8, H: 8, Width: 8, Height: 8},
			"5":      {Name: "5", X: 8, Y: 0, W: 8, H: 8, Width: 8, Height: 8},
			"05":     {Name: "05", X: 24, Y: 0, W: 8, H: 8, Width: 8, Height: 8},
			"0":      {Name: "0", X: 32, Y: 0, W: 8, H: 8, Width: 8, Height: 8},
			"-1":     {Name: "-1", X: 40, Y: 0, W: 8, H: 8, Width: 8, Height: 8},
			"corner": {Name: "corner", X: 48, Y: 0, W: 8, H: 8, Width: 8, Height: 8},
		},
	}
	return r
}

// paletteSceneDoc is testSceneDoc with a "walls" layer whose 8x8 tilemap uses the walls tileset.
func paletteSceneDoc() *sceneformat.SceneDoc {
	doc := testSceneDoc()
	doc.Layers = append(doc.Layers, sceneformat.LayerDoc{
		Name:  "walls",
		Space: sceneformat.SpaceWorld,
		Order: 5,
		Tilemap: &sceneformat.TilemapDoc{
			TileWidth:  8,
			TileHeight: 8,
			Width:      2,
			Height:     2,
			Tileset:    "walls",
			Data:       []int{0, 0, 0, 0},
		},
	})
	return doc
}

func paletteIDs(p editor.TilePalette) []int {
	var ids []int
	for _, t := range p.Tiles {
		ids = append(ids, t.ID)
	}
	return ids
}

func TestTilePaletteListsTilesInIDOrder(t *testing.T) {
	ed, err := editor.New(paletteSceneDoc(), paletteResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetActiveLayer("walls")
	p := ed.TilePalette()
	if got, want := paletteIDs(p), []int{2, 5, 10}; !slices.Equal(got, want) {
		t.Fatalf("tile IDs = %v, want %v (numeric order, frames that are not canonical positive IDs left out)", got, want)
	}
	if p.Layer != "walls" || p.Tileset != "walls" || p.TileWidth != 8 || p.TileHeight != 8 || p.Message != "" {
		t.Fatalf("palette = %+v, want layer and tileset walls at 8x8 with no message", p)
	}
	if p.Tiles[1].Frame.X != 8 || p.Tiles[1].Image.URI != "asset:walls.png" {
		t.Fatalf("tile 5 = %+v, want the walls atlas's frame 5", p.Tiles[1])
	}
	if !p.Has(10) || p.Has(1) {
		t.Fatalf("Has(10), Has(1) = %v, %v, want true, false", p.Has(10), p.Has(1))
	}
}

func TestTilePaletteSaysWhyThereAreNoTiles(t *testing.T) {
	doc := paletteSceneDoc()
	doc.Layers = append(doc.Layers,
		sceneformat.LayerDoc{Name: "lost", Tilemap: &sceneformat.TilemapDoc{TileWidth: 8, TileHeight: 8, Width: 1, Height: 1, Tileset: "missing", Data: []int{0}}},
		sceneformat.LayerDoc{Name: "named", Tilemap: &sceneformat.TilemapDoc{TileWidth: 16, TileHeight: 16, Width: 1, Height: 1, Tileset: "actors", Data: []int{0}}},
		sceneformat.LayerDoc{Name: "tiled", Tilemap: &sceneformat.TilemapDoc{Source: "maps/cave.tmj"}},
	)
	// The scene does not build (its tilesets do not all resolve), which leaves it open in the editor with BuildErr set.
	ed, err := editor.New(doc, paletteResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}

	for _, tc := range []struct {
		layer, want string
	}{
		{"actors", `Layer "actors" has no inline tilemap to paint`},
		{"tiled", `Layer "tiled" has no inline tilemap to paint`},
		{"lost", `Tileset "missing" cannot be resolved`},
		{"named", `Tileset "actors" has no tile frames`},
	} {
		if !ed.SetActiveLayer(tc.layer) {
			t.Fatalf("SetActiveLayer(%q) = false", tc.layer)
		}
		p := ed.TilePalette()
		if len(p.Tiles) != 0 || !strings.HasPrefix(p.Message, tc.want) {
			t.Errorf("layer %s: palette = %+v, want no tiles and a message starting %q", tc.layer, p, tc.want)
		}
	}

	ed.ActiveLayer = "floor"
	ed.Resolver = nil
	if p := ed.TilePalette(); len(p.Tiles) != 0 || !strings.Contains(p.Message, "no asset resolver") {
		t.Errorf("without a resolver: palette = %+v, want no tiles and a message about the resolver", p)
	}
	ed.ActiveLayer = "gone"
	if p := ed.TilePalette(); len(p.Tiles) != 0 || p.Message != "No layer is selected" {
		t.Errorf("unknown layer: palette = %+v, want no tiles and no layer selected", p)
	}
}

func TestSetActiveLayerKeepsTheActiveTileInItsTileset(t *testing.T) {
	ed, err := editor.New(paletteSceneDoc(), paletteResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if ed.ActiveTileID != 1 {
		t.Fatalf("ActiveTileID = %d, want 1 on the floor layer", ed.ActiveTileID)
	}
	ed.SetActiveTileID(2)
	ed.SetActiveLayer("walls")
	if ed.ActiveTileID != 2 {
		t.Fatalf("ActiveTileID = %d, want 2 kept: the walls tileset has tile 2", ed.ActiveTileID)
	}
	ed.SetActiveTileID(10)
	ed.SetActiveLayer("floor")
	if ed.ActiveTileID != 1 {
		t.Fatalf("ActiveTileID = %d, want 1, the floor tileset's first tile, since it has no tile 10", ed.ActiveTileID)
	}
	ed.SetActiveTileID(3)
	ed.SetActiveLayer("actors")
	if ed.ActiveTileID != 3 {
		t.Fatalf("ActiveTileID = %d, want 3 kept on a layer without tiles", ed.ActiveTileID)
	}
}

// wallsFirstSceneDoc is paletteSceneDoc with the walls layer first, so it is the layer a newly opened scene starts on.
func wallsFirstSceneDoc() *sceneformat.SceneDoc {
	doc := paletteSceneDoc()
	n := len(doc.Layers)
	doc.Layers = append(doc.Layers[n-1:], doc.Layers[:n-1]...)
	return doc
}

func TestNewSelectsATileOfTheFirstLayer(t *testing.T) {
	ed, err := editor.New(wallsFirstSceneDoc(), paletteResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if ed.ActiveLayer != "walls" || ed.ActiveTileID != 2 {
		t.Fatalf("layer %q, ActiveTileID = %d, want walls and 2, its first tile, since the walls tileset has no tile 1", ed.ActiveLayer, ed.ActiveTileID)
	}
}

func TestOpenYAMLSelectsATileOfTheFirstLayer(t *testing.T) {
	src, err := editor.New(wallsFirstSceneDoc(), paletteResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	data, err := src.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	ed, err := editor.New(paletteSceneDoc(), paletteResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if ed.ActiveTileID != 1 {
		t.Fatalf("ActiveTileID = %d, want 1 on the floor layer", ed.ActiveTileID)
	}
	if err := ed.OpenYAML(data); err != nil {
		t.Fatalf("OpenYAML: %v", err)
	}
	if ed.ActiveLayer != "walls" || ed.ActiveTileID != 2 {
		t.Fatalf("layer %q, ActiveTileID = %d, want walls and 2, its first tile, since the walls tileset has no tile 1", ed.ActiveLayer, ed.ActiveTileID)
	}
	if err := ed.PaintTile(ed.ActiveLayer, 0, 0, ed.ActiveTileID); err != nil {
		t.Fatalf("PaintTile: %v", err)
	}
	if ed.BuildErr != nil {
		t.Fatalf("painting the opened walls layer fails to build: %v", ed.BuildErr)
	}
}

// The issue's first acceptance item: a tile chosen in the palette is the one Paint writes, and the saved scene file holds its ID.
func TestTilePaletteChoosesTheTilePaintSaves(t *testing.T) {
	ed := newTestEditor(t)
	path := filepath.Join(t.TempDir(), "level.scene.yaml")
	if err := ed.SaveFile(path); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	ed.SetActiveLayer("floor")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 680))

	if app.Exists("id:tile-palette") {
		t.Fatal("tile palette shows with the Select tool")
	}
	runEditorCommand(t, app, ed, editor.CmdToolPaintTile)
	if !app.Exists("id:tile-palette") {
		t.Fatal("tile palette does not show with the Paint tool")
	}
	for _, id := range []string{"1", "2", "3"} {
		if !app.Exists("id:tile-palette-" + id) {
			t.Fatalf("tile palette has no tile %s", id)
		}
	}
	if got := app.Find("id:tile-palette-tileset").Text; got != "tiles" {
		t.Fatalf("palette tileset = %q, want tiles", got)
	}
	thumb := app.Find("id:tile-palette-thumb-2")
	if thumb.Frame.Size.Width != 16 || thumb.Frame.Size.Height != 16 {
		t.Fatalf("tile 2 shows at %vx%v, want the tilemap's 16x16", thumb.Frame.Size.Width, thumb.Frame.Size.Height)
	}
	draws := gametest.ImageDraws(thumb.Ops)
	if len(draws) != 1 || draws[0].Image.URI != "asset:tiles.png" || draws[0].SX != 16 || draws[0].SW != 16 {
		t.Fatalf("tile 2 draws %+v, want frame 2 of tiles.png", draws)
	}
	if !app.Find("id:tile-palette-1").Props.Checked || app.Find("id:tile-palette-3").Props.Checked {
		t.Fatal("tile 1 should be the selected tile before a press")
	}
	if got := app.Find("id:editor-status-mode").Text; !strings.HasSuffix(got, "| Tile: 1") {
		t.Fatalf("status = %q, want the selected tile 1", got)
	}

	app.Press("id:tile-palette-3")
	if ed.ActiveTileID != 3 {
		t.Fatalf("ActiveTileID = %d after pressing tile 3, want 3", ed.ActiveTileID)
	}
	if !app.Find("id:tile-palette-3").Props.Checked || app.Find("id:tile-palette-1").Props.Checked {
		t.Fatal("tile 3 should be the only selected tile after its press")
	}
	if got := app.Find("id:editor-status-mode").Text; !strings.HasSuffix(got, "| Tile: 3") {
		t.Fatalf("status = %q, want the selected tile 3", got)
	}

	// Tile (1, 1) is empty; world (24, 24) is inside it.
	tx, ty := ed.WorldToCanvas(24, 24)
	app.PressAt("id:editor-viewport", core.Point{X: core.Points(tx), Y: core.Points(ty)})
	if got := ed.Doc.Layers[0].Tilemap.Data[1*4+1]; got != 3 {
		t.Fatalf("painted tile (1,1) = %d, want 3", got)
	}
	saveInUI(app, ed)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	saved, err := sceneformat.Parse(data)
	if err != nil {
		t.Fatalf("Parse saved scene: %v", err)
	}
	if got := saved.Layers[0].Tilemap.Data[1*4+1]; got != 3 {
		t.Fatalf("saved tile (1,1) = %d, want 3", got)
	}

	// The paint is one undoable step; picking a tile is not an edit.
	undoInUI(app, ed)
	if got := ed.Doc.Layers[0].Tilemap.Data[1*4+1]; got != 0 {
		t.Fatalf("tile (1,1) after Undo = %d, want 0", got)
	}
	if ed.History.CanUndo() {
		t.Fatal("history holds more than the paint")
	}
}

// The issue's second acceptance item: the palette shows the tileset of whichever layer is active.
func TestTilePaletteFollowsTheActiveLayer(t *testing.T) {
	ed, err := editor.New(paletteSceneDoc(), paletteResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 680))
	runEditorCommand(t, app, ed, editor.CmdToolFillTile)
	if !app.Exists("id:tile-palette-3") || app.Exists("id:tile-palette-10") {
		t.Fatal("palette should show the floor layer's tiles")
	}

	app.Post(func() { ed.SetActiveLayer("walls") })
	app.Settle()
	if got := app.Find("id:tile-palette-tileset").Text; got != "walls" {
		t.Fatalf("palette tileset = %q, want walls", got)
	}
	for _, id := range []string{"2", "5", "10"} {
		if !app.Exists("id:tile-palette-" + id) {
			t.Fatalf("palette has no walls tile %s", id)
		}
	}
	if app.Exists("id:tile-palette-1") || app.Exists("id:tile-palette-3") {
		t.Fatal("palette still shows floor tiles on the walls layer")
	}
	if thumb := app.Find("id:tile-palette-thumb-5"); thumb.Frame.Size.Width != 8 || thumb.Frame.Size.Height != 8 {
		t.Fatalf("walls tile shows at %vx%v, want 8x8", thumb.Frame.Size.Width, thumb.Frame.Size.Height)
	}
	if ed.ActiveTileID != 2 || !app.Find("id:tile-palette-2").Props.Checked {
		t.Fatalf("ActiveTileID = %d, want 2, the first walls tile, selected", ed.ActiveTileID)
	}

	app.Post(func() { ed.SetActiveLayer("actors") })
	app.Settle()
	if app.Exists("id:tile-palette-grid") {
		t.Fatal("palette shows a grid for a layer without a tilemap")
	}
	if got := app.Find("id:tile-palette-empty").Text; got != `Layer "actors" has no inline tilemap to paint` {
		t.Fatalf("empty palette says %q", got)
	}
	if got := app.Find("id:editor-status-mode").Text; strings.Contains(got, "Tile:") {
		t.Fatalf("status = %q shows a tile on a layer without tiles to pick", got)
	}

	app.Post(func() { ed.SetActiveLayer("walls") })
	app.Settle()
	if got := app.Find("id:editor-status-mode").Text; !strings.HasSuffix(got, "| Tile: 2") {
		t.Fatalf("status = %q, want the selected walls tile 2 with Fill", got)
	}
	runEditorCommand(t, app, ed, editor.CmdToolEraseTile)
	if got := app.Find("id:editor-status-mode").Text; strings.Contains(got, "Tile:") {
		t.Fatalf("status = %q shows a tile with Erase, which writes none", got)
	}

	app.Press("id:tool-select")
	if app.Exists("id:tile-palette") {
		t.Fatal("tile palette still shows after leaving the tile tools")
	}
}

func TestTilePaletteArrowKeysMoveTheSelection(t *testing.T) {
	ed, err := editor.New(paletteSceneDoc(), paletteResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetActiveLayer("walls")
	ed.SetTool(editor.ToolPaintTile)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 680), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	app.Focus("id:tile-palette-2")
	app.Key("right")
	if ed.ActiveTileID != 5 {
		t.Fatalf("ActiveTileID = %d after Right, want 5", ed.ActiveTileID)
	}
	app.Key("right")
	if ed.ActiveTileID != 10 {
		t.Fatalf("ActiveTileID = %d after a second Right, want 10", ed.ActiveTileID)
	}
	app.Key("left")
	if ed.ActiveTileID != 5 {
		t.Fatalf("ActiveTileID = %d after Left, want 5", ed.ActiveTileID)
	}
	// Up and Down step through the tiles in ID order too, as Left and Right do, rather than by rows of the wrapped grid.
	app.Key("down")
	if ed.ActiveTileID != 10 {
		t.Fatalf("ActiveTileID = %d after Down, want 10", ed.ActiveTileID)
	}
	app.Key("up")
	app.Key("up")
	if ed.ActiveTileID != 2 {
		t.Fatalf("ActiveTileID = %d after Up twice, want 2", ed.ActiveTileID)
	}
}

func TestTilePaletteHiddenIn3D(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetTool(editor.ToolPaintTile)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 680))
	if !app.Exists("id:tile-palette") {
		t.Fatal("tile palette does not show with the Paint tool in 2D")
	}
	app.Press("id:view-mode-3d")
	if app.Exists("id:tile-palette") {
		t.Fatal("tile palette shows in 3D")
	}
}

func TestTilePaletteFitsAWideTileInThePanel(t *testing.T) {
	doc := testSceneDoc()
	tm := doc.Layers[0].Tilemap
	tm.TileWidth, tm.TileHeight = 408, 204
	ed, err := editor.New(doc, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetTool(editor.ToolPaintTile)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 680))
	// The panel's tiles wrap in 203 points (224 less 10 of padding on each side and the left border of 1), and each tile's ring of 2 and padding of 2 on each side sit inside its box.
	thumb := app.Find("id:tile-palette-thumb-1")
	// 408x204 scales to 195x97.5, and layout rounds the height to a whole point.
	if thumb.Frame.Size.Width != 195 || thumb.Frame.Size.Height != 98 {
		t.Fatalf("a 408x204 tile shows at %vx%v, want it scaled to 195x98", thumb.Frame.Size.Width, thumb.Frame.Size.Height)
	}
	grid, tile := app.Find("id:tile-palette-grid"), app.Find("id:tile-palette-1")
	if tile.Frame.Size.Width > grid.Frame.Size.Width {
		t.Fatalf("the wide tile's box is %v wide, wider than the %v its grid wraps in", tile.Frame.Size.Width, grid.Frame.Size.Width)
	}
}
