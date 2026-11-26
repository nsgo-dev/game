package editor_test

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// layeredTileset is a YAML tileset of three 16x16 tiles in a 3-column grid image with a 1-point margin.
const layeredTileset = "version: 1\ntileWidth: 16\ntileHeight: 16\nimage: tiles/ground.png\ncolumns: 3\ntileCount: 3\nmargin: 1\ntiles:\n  - id: 0\n    solid: true\n"

// layeredEditor opens a scene whose "ground" layer is a 3x2 layered grid of the tile layers floor and deco, with the editor reading the project's files through FSAssets.
func layeredEditor(t *testing.T) *editor.Editor {
	t.Helper()
	doc, err := sceneformat.Parse([]byte(`version: 2
layers:
  - name: ground
    tilemap:
      tileWidth: 16
      tileHeight: 16
      width: 3
      height: 2
      tilesetFile: tilesets/ground.tileset.yaml
      tileLayers:
        - name: floor
          rows:
            - 0 0 0
            - 0 . .
        - name: deco
          rows:
            - . . .
            - . . .
`))
	if err != nil {
		t.Fatal(err)
	}
	ed, err := editor.New(doc, sceneformat.FSAssets{FS: fstest.MapFS{"tilesets/ground.tileset.yaml": {Data: []byte(layeredTileset)}}})
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if ed.BuildErr != nil {
		t.Fatalf("build: %v", ed.BuildErr)
	}
	ed.SetActiveLayer("ground")
	return ed
}

func TestPaintLayeredTilemap(t *testing.T) {
	ed := layeredEditor(t)
	tm := ed.Doc.Layers[0].Tilemap
	floor, deco := tm.TileLayer("floor"), tm.TileLayer("deco")

	// Paint writes tile ID - 1 with the active flips into the first tile layer by default.
	ed.SetActiveTileFlip(tilemap.FlipHorizontal | tilemap.FlipDiagonal | 1)
	if ed.ActiveTileFlip != tilemap.FlipHorizontal|tilemap.FlipDiagonal {
		t.Fatalf("ActiveTileFlip = %#x", ed.ActiveTileFlip)
	}
	if err := ed.PaintTile("ground", 2, 1, 3); err != nil {
		t.Fatalf("PaintTile: %v", err)
	}
	if floor.Rows[1] != "0 . 2hd" {
		t.Fatalf("floor row 1 = %q", floor.Rows[1])
	}
	if got := ed.Built.Layer("ground").Map.TileAt(0, 2, 1); got != 3|tilemap.FlipHorizontal|tilemap.FlipDiagonal {
		t.Fatalf("built map tile = %#x", got)
	}

	// The deco layer takes the next paints once it is active.
	if ed.SetActiveTileLayer("roof") {
		t.Fatal("SetActiveTileLayer accepted a tile layer that does not exist")
	}
	if !ed.SetActiveTileLayer("deco") {
		t.Fatal("SetActiveTileLayer refused deco")
	}
	ed.SetActiveTileFlip(0)
	if err := ed.FloodFillTile("ground", 0, 0, 2); err != nil {
		t.Fatalf("FloodFillTile: %v", err)
	}
	if !slices.Equal(deco.Rows, []string{"1 1 1", "1 1 1"}) || floor.Rows[0] != "0 0 0" {
		t.Fatalf("after fill: deco %q, floor %q", deco.Rows, floor.Rows)
	}
	if err := ed.EraseTile("ground", 1, 0); err != nil {
		t.Fatal(err)
	}
	if deco.Rows[0] != "1 . 1" {
		t.Fatalf("after erase: deco row 0 = %q", deco.Rows[0])
	}

	// Each edit is one undoable step.
	ed.Undo()
	ed.Undo()
	if !slices.Equal(deco.Rows, []string{". . .", ". . ."}) {
		t.Fatalf("after undoing fill and erase: deco %q", deco.Rows)
	}
	ed.Undo()
	if floor.Rows[1] != "0 . ." {
		t.Fatalf("after undoing paint: floor row 1 = %q", floor.Rows[1])
	}
	ed.Redo()
	if floor.Rows[1] != "0 . 2hd" {
		t.Fatalf("after redo: floor row 1 = %q", floor.Rows[1])
	}

	data, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "- 0 . 2hd\n") {
		t.Fatalf("saved scene:\n%s", data)
	}
	if err := ed.PaintTile("ground", 3, 0, 1); err == nil {
		t.Fatal("PaintTile outside the grid succeeded")
	}
}

func TestTilePaletteShowsALayeredGridsTileset(t *testing.T) {
	ed := layeredEditor(t)
	p := ed.TilePalette()
	if p.Message != "" || p.Tileset != "tilesets/ground.tileset.yaml" || !slices.Equal(p.TileLayers, []string{"floor", "deco"}) || p.TileLayer != "floor" {
		t.Fatalf("palette = %+v", p)
	}
	if got := paletteIDs(p); !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("tile IDs = %v", got)
	}
	if f := p.Tiles[1].Frame; f.X != 17 || f.Y != 1 || f.W != 16 || p.Tiles[1].Image.URI != "tiles/ground.png" {
		t.Fatalf("tile 2 = %+v", p.Tiles[1])
	}

	// A resolver that reads no files cannot show the tileset.
	noFiles, err := editor.New(ed.Doc, sceneformat.AtlasMap{})
	if err != nil {
		t.Fatal(err)
	}
	noFiles.SetActiveLayer("ground")
	if p := noFiles.TilePalette(); len(p.Tiles) != 0 || !strings.Contains(p.Message, "FileResolver") {
		t.Fatalf("palette without file access = %+v", p)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 680))
	runEditorCommand(t, app, ed, editor.CmdToolPaintTile)
	if !app.Exists("id:tile-layer-controls") {
		t.Fatal("the palette of a layered grid shows no tile layer controls")
	}
	app.Press("id:tile-layer-deco")
	if ed.ActiveTileLayer != "deco" {
		t.Fatalf("ActiveTileLayer = %q after pressing deco", ed.ActiveTileLayer)
	}
	if app.Find("id:tile-flip-v").Props.Pressed {
		t.Fatal("V flip button is pressed before clicking it")
	}
	app.Press("id:tile-flip-v")
	if ed.ActiveTileFlip != tilemap.FlipVertical || !app.Find("id:tile-flip-v").Props.Pressed {
		t.Fatalf("after pressing V: ActiveTileFlip = %#x, pressed = %v", ed.ActiveTileFlip, app.Find("id:tile-flip-v").Props.Pressed)
	}
	app.Press("id:tile-flip-v")
	if ed.ActiveTileFlip != 0 || app.Find("id:tile-flip-v").Props.Pressed {
		t.Fatalf("after pressing V again: ActiveTileFlip = %#x, pressed = %v", ed.ActiveTileFlip, app.Find("id:tile-flip-v").Props.Pressed)
	}
	app.Press("id:tile-flip-v")
	if ed.ActiveTileFlip != tilemap.FlipVertical || !app.Find("id:tile-flip-v").Props.Pressed {
		t.Fatalf("after re-enabling V: ActiveTileFlip = %#x, pressed = %v", ed.ActiveTileFlip, app.Find("id:tile-flip-v").Props.Pressed)
	}
	app.Press("id:tile-palette-2")
	if ed.ActiveTileID != 2 {
		t.Fatalf("ActiveTileID = %d", ed.ActiveTileID)
	}
	// The palette and the status bar name a tile by the local ID the rows write, not the paint value.
	if !app.Exists("label:Tile 0") || app.Exists("label:Tile 3") {
		t.Fatal("palette labels are not the tileset's local IDs 0 to 2")
	}
	if got := app.Find("id:editor-status-mode").Text; !strings.HasSuffix(got, "| Tile: 1") {
		t.Fatalf("status = %q, want local tile 1", got)
	}
	draws := gametest.ImageDraws(app.Find("id:tile-palette-thumb-2").Ops)
	if len(draws) != 1 || draws[0].Image.URI != "tiles/ground.png" || draws[0].SX != 17 {
		t.Fatalf("tile 2 thumbnail draws %+v", draws)
	}
	// Frame the grid so its cells are on the canvas, then press inside cell (2, 0).
	ed.FrameAll2D()
	tx, ty := ed.WorldToCanvas(40, 8)
	app.PressAt("id:editor-viewport", core.Point{X: core.Points(tx), Y: core.Points(ty)})
	if got := ed.Doc.Layers[0].Tilemap.TileLayer("deco").Rows[0]; got != ". . 1v" {
		t.Fatalf("deco row 0 after painting (2, 0) = %q", got)
	}
}

func TestClipPreviewDrawsEveryTileOfALayeredGrid(t *testing.T) {
	// The scene camera shows one 16x16 cell of the 3x2 grid, but the 2D view pans past it, so both the edit scene and an open clip's preview draw all six tiles.
	doc, err := sceneformat.Parse([]byte(`version: 2
virtualWidth: 16
virtualHeight: 16
layers:
  - name: ground
    tilemap:
      tileWidth: 16
      tileHeight: 16
      width: 3
      height: 2
      tilesetFile: tilesets/ground.tileset.yaml
      tileLayers:
        - name: floor
          rows:
            - 0 0 0
            - 0 0 0
    objects:
      - id: hero
        x: 8
        y: 8
`))
	if err != nil {
		t.Fatal(err)
	}
	ed, err := editor.New(doc, sceneformat.FSAssets{FS: fstest.MapFS{"tilesets/ground.tileset.yaml": {Data: []byte(layeredTileset)}}})
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if ed.BuildErr != nil {
		t.Fatalf("build: %v", ed.BuildErr)
	}
	tileDraws := func(s *sceneformat.BuiltScene) int {
		var d core.Drawing
		s.Draw(&d)
		var probe core.Drawing
		probe.DrawImageFrom(core.ImageSource{URI: "x"}, 0, 0, 1, 1, 0, 0, 1, 1)
		n := 0
		for _, op := range d.Ops {
			if op.Kind == probe.Ops[0].Kind {
				n++
			}
		}
		return n
	}
	if got := tileDraws(ed.ActiveScene()); got != 6 {
		t.Fatalf("edit scene drew %d tiles, want 6", got)
	}
	if err := ed.NewClip("hero", "Bob", time.Second); err != nil {
		t.Fatalf("NewClip: %v", err)
	}
	preview := ed.ActiveScene()
	if preview == ed.Built || ed.ClipError() != nil {
		t.Fatalf("no clip preview (error %v)", ed.ClipError())
	}
	if got := tileDraws(preview); got != 6 {
		t.Fatalf("clip preview drew %d tiles, want 6", got)
	}
}

func TestTileStrokeAndPickOnLayeredTilemap(t *testing.T) {
	ed := layeredEditor(t)
	deco := ed.Doc.Layers[0].Tilemap.TileLayer("deco")
	ed.SetActiveTileLayer("deco")
	ed.SetActiveTileID(3)
	ed.SetActiveTileFlip(tilemap.FlipHorizontal)

	x0, y0 := ed.WorldToCanvas(8, 8)
	x1, y1 := ed.WorldToCanvas(40, 8)
	if !ed.BeginTileStroke(editor.TileStrokePaint, x0, y0) {
		t.Fatal("BeginTileStroke refused layered tilemap")
	}
	ed.ContinueTileStroke(x1, y1)
	ed.EndTileStroke()
	if deco.Rows[0] != "2h 2h 2h" {
		t.Fatalf("deco row 0 after paint stroke = %q, want %q", deco.Rows[0], "2h 2h 2h")
	}
	ed.Undo()
	if deco.Rows[0] != ". . ." {
		t.Fatalf("deco row 0 after single undo = %q, want empty", deco.Rows[0])
	}
	ed.Redo()
	if deco.Rows[0] != "2h 2h 2h" {
		t.Fatalf("deco row 0 after redo = %q", deco.Rows[0])
	}

	// Cancelling an erase stroke restores the layered row without recording history.
	if !ed.BeginTileStroke(editor.TileStrokeErase, x0, y0) {
		t.Fatal("BeginTileStroke erase refused")
	}
	ed.ContinueTileStroke(x1, y1)
	if deco.Rows[0] != ". . ." {
		t.Fatalf("deco row 0 mid-erase = %q", deco.Rows[0])
	}
	ed.CancelTileStroke()
	if deco.Rows[0] != "2h 2h 2h" {
		t.Fatalf("deco row 0 after cancel = %q, want restored", deco.Rows[0])
	}

	// PickTile reads the active tile layer, sets ActiveTileID and copies its flips.
	ed.SetActiveTileID(1)
	ed.SetActiveTileFlip(0)
	if !ed.PickTile("ground", 1, 0) {
		t.Fatal("PickTile on filled layered cell returned false")
	}
	if ed.ActiveTileID != 3 || ed.ActiveTileFlip != tilemap.FlipHorizontal || ed.Status != "Picked tile 2" {
		t.Fatalf("after PickTile: id=%d flip=%#x status=%q", ed.ActiveTileID, ed.ActiveTileFlip, ed.Status)
	}
	if ed.PickTile("ground", 0, 1) {
		t.Fatal("PickTile on empty layered cell returned true")
	}
	if ed.ActiveTileID != 3 || ed.ActiveTileFlip != tilemap.FlipHorizontal {
		t.Fatalf("empty PickTile changed selection: id=%d flip=%#x", ed.ActiveTileID, ed.ActiveTileFlip)
	}
}
