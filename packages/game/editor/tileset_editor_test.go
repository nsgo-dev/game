package editor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/game/packages/game/tileshape"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// Acceptance #1: A tile marked solid in the tileset editor collides in the game through physics.MoveAndSlide with no Go code naming the tile.
func TestTilesetEditorSolidTileCollidesInGameWithMoveAndSlide(t *testing.T) {
	dir := t.TempDir()
	tsRel := "tilesets/dungeon.tileset.yaml"
	tsPath := filepath.Join(dir, filepath.FromSlash(tsRel))
	if err := os.MkdirAll(filepath.Dir(tsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	initial := &tilemap.TilesetDoc{
		Version:    1,
		Name:       "dungeon",
		TileWidth:  16,
		TileHeight: 16,
		Image:      "art/tiles/dungeon.png",
		Columns:    4,
		TileCount:  8,
	}
	raw, err := tilemap.EncodeTileset(initial)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tsPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	sceneDoc := &sceneformat.SceneDoc{
		Version:       2,
		Name:          "Dungeon",
		VirtualWidth:  64,
		VirtualHeight: 48,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "ground",
				Space: sceneformat.SpaceWorld,
				Tilemap: &sceneformat.TilemapDoc{
					TileWidth:   16,
					TileHeight:  16,
					Width:       4,
					Height:      3,
					TilesetFile: tsRel,
					TileLayers: []sceneformat.TileLayerDoc{
						{
							Name: "floor",
							Rows: []string{
								". . . .",
								". . . .",
								"0 0 0 0",
							},
						},
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
	ed.OnSaveTileset = func(data []byte) error {
		return os.WriteFile(tsPath, data, 0o644)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	// Open Tileset Editor from the Tile Palette, switch to Collision tab, mark tile 0 solid, and save.
	app.Press("id:tool-paint")
	app.Press("id:tile-palette-edit-tileset")
	app.Press("id:tileset-tab-collision")
	app.Press("id:tileset-tile-0")
	app.Press("id:tileset-coll-solid")
	app.Press("id:tileset-editor-save")

	// Verify round-trip unchanged on disk.
	saved, err := os.ReadFile(tsPath)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := tilemap.ParseTileset(saved)
	if err != nil {
		t.Fatalf("ParseTileset: %v", err)
	}
	reencoded, err := tilemap.EncodeTileset(parsed)
	if err != nil || !bytes.Equal(reencoded, saved) {
		t.Fatalf("saved tileset did not round-trip unchanged:\n%s", saved)
	}

	// Build the scene in the game from disk (with NO Go code calling SetFlags or naming tile 0).
	gameBuilt, err := sceneformat.Build(sceneDoc, sceneformat.FSAssets{FS: os.DirFS(dir)})
	if err != nil {
		t.Fatalf("sceneformat.Build: %v", err)
	}
	m := gameBuilt.Layer("ground").Map
	if m == nil {
		t.Fatal("built ground layer has nil Map")
	}

	// Collide both directly with *tilemap.Tilemap AND via tileshape.New through physics.MoveAndSlide.
	for name, grid := range map[string]physics.TileGrid{
		"tilemap":   m,
		"tileshape": tileshape.New(m, tileshape.Options{}),
	} {
		t.Run(name, func(t *testing.T) {
			body := &physics.Body{
				Shape: physics.BoxShape(physics.Box{X: 16, Y: 4, W: 12, H: 12}),
			}
			for range 30 {
				body.VY += 500 / 60.0
				physics.MoveAndSlide(body, grid, time.Second/60)
			}
			if !body.Grounded || body.Bounds().Bottom() != 32 {
				t.Fatalf("MoveAndSlide(%s): grounded=%v bottom=%v, want grounded on solid row 2 (y=32)", name, body.Grounded, body.Bounds().Bottom())
			}
		})
	}
}

// Acceptance #2: An animated tile animates in the editor viewport and the game.
func TestTilesetEditorAnimatedTileAnimatesInViewportAndGame(t *testing.T) {
	dir := t.TempDir()
	tsRel := "tilesets/water.tileset.yaml"
	tsPath := filepath.Join(dir, filepath.FromSlash(tsRel))
	if err := os.MkdirAll(filepath.Dir(tsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	initial := &tilemap.TilesetDoc{
		Version:    1,
		Name:       "water",
		TileWidth:  16,
		TileHeight: 16,
		Image:      "art/tiles/water.png",
		Columns:    4,
		TileCount:  4,
	}
	raw, err := tilemap.EncodeTileset(initial)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tsPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	sceneDoc := &sceneformat.SceneDoc{
		Version:       2,
		Name:          "Pool",
		VirtualWidth:  32,
		VirtualHeight: 32,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "water",
				Space: sceneformat.SpaceWorld,
				Tilemap: &sceneformat.TilemapDoc{
					TileWidth:   16,
					TileHeight:  16,
					Width:       1,
					Height:      1,
					TilesetFile: tsRel,
					TileLayers: []sceneformat.TileLayerDoc{
						{Name: "surface", Rows: []string{"0"}},
					},
				},
			},
		},
	}

	ed, err := editor.New(sceneDoc, sceneformat.FSAssets{FS: os.DirFS(dir)})
	if err != nil {
		t.Fatal(err)
	}
	ed.SetActiveLayer("water")
	ed.OnSaveTileset = func(data []byte) error {
		return os.WriteFile(tsPath, data, 0o644)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	app.Press("id:tool-paint")
	app.Press("id:tile-palette-edit-tileset")
	app.Press("id:tileset-tab-animation")
	app.Press("id:tileset-tile-0")

	// Add frame 0 (tile 0, 100ms) and frame 1 (tile 1, 100ms) through the UI.
	app.Clear("id:tileset-anim-add-tile")
	app.Type("id:tileset-anim-add-tile", "0")
	app.Clear("id:tileset-anim-add-duration")
	app.Type("id:tileset-anim-add-duration", "100ms")
	app.Press("id:tileset-anim-add-frame")

	app.Clear("id:tileset-anim-add-tile")
	app.Type("id:tileset-anim-add-tile", "1")
	app.Clear("id:tileset-anim-add-duration")
	app.Type("id:tileset-anim-add-duration", "100ms")
	app.Press("id:tileset-anim-add-frame")

	// Before stepping: viewport draws tile 0 (SX = 0) and preview shows tile 0.
	if ed.TilesetAnimPreviewTile() != 0 {
		t.Fatalf("initial preview tile = %d, want 0", ed.TilesetAnimPreviewTile())
	}
	draws0 := gametest.ImageDraws(app.Find("id:editor-viewport").Ops)
	if len(draws0) == 0 || draws0[0].SX != 0 {
		t.Fatalf("initial viewport tile draw = %+v, want SX=0", draws0)
	}

	// Advance time in edit mode via the live frame loop: BOTH the editor viewport AND the tileset editor preview advance to tile 1 (SX = 16)!
	app.Advance(120 * time.Millisecond)
	if ed.TilesetAnimPreviewTile() != 1 {
		t.Fatalf("preview tile after 120ms = %d, want 1", ed.TilesetAnimPreviewTile())
	}
	draws1 := gametest.ImageDraws(app.Find("id:editor-viewport").Ops)
	if len(draws1) == 0 || draws1[0].SX != 16 {
		t.Fatalf("viewport tile draw after 120ms = %+v, want SX=16", draws1)
	}

	// Save and verify the animated tile animates in the game too.
	app.Press("id:tileset-editor-save")
	gameBuilt, err := sceneformat.Build(sceneDoc, sceneformat.FSAssets{FS: os.DirFS(dir)})
	if err != nil {
		t.Fatalf("sceneformat.Build: %v", err)
	}
	anim := gameBuilt.Layer("water").Map.Tilesets[0].Animations[0]
	if anim == nil || anim.CurrentTileID() != 0 {
		t.Fatalf("game initial animation = %+v, want tile 0", anim)
	}
	gameBuilt.Update(120 * time.Millisecond)
	if anim.CurrentTileID() != 1 {
		t.Fatalf("game animation after 120ms = tile %d, want 1", anim.CurrentTileID())
	}
}

func TestTilesetEditorCollisionShapesCustomDataAndUndo(t *testing.T) {
	dir := t.TempDir()
	tsRel := "tilesets/cave.tileset.yaml"
	tsPath := filepath.Join(dir, filepath.FromSlash(tsRel))
	if err := os.MkdirAll(filepath.Dir(tsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	initial := &tilemap.TilesetDoc{
		Version:    1,
		Name:       "cave",
		TileWidth:  16,
		TileHeight: 16,
		Image:      "art/tiles/cave.png",
		Columns:    4,
		TileCount:  8,
	}
	raw, err := tilemap.EncodeTileset(initial)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tsPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	sceneDoc := &sceneformat.SceneDoc{
		Version:       2,
		Name:          "Cave",
		VirtualWidth:  64,
		VirtualHeight: 48,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "ground",
				Space: sceneformat.SpaceWorld,
				Tilemap: &sceneformat.TilemapDoc{
					TileWidth:   16,
					TileHeight:  16,
					Width:       2,
					Height:      2,
					TilesetFile: tsRel,
					Data:        []int{1, 2, 0, 0},
				},
			},
		},
	}

	ed, err := editor.New(sceneDoc, sceneformat.FSAssets{FS: os.DirFS(dir)})
	if err != nil {
		t.Fatal(err)
	}
	ed.SetActiveLayer("ground")
	ed.OnSaveTileset = func(data []byte) error {
		return os.WriteFile(tsPath, data, 0o644)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	// Switching to Paint tool shows the right-side Tile Inspector (matching Tiles.dc.html).
	app.Press("id:tool-paint")
	if !app.Exists("id:inspector-tile") {
		t.Fatal("expected right-side Tile Inspector when Paint tool is active on a YAML tileset layer")
	}
	if got := app.Find("id:tile-inspector-source").Text; got != "art/tiles/cave.png · cell 0, 0" {
		t.Fatalf("tile-inspector-source = %q, want %q", got, "art/tiles/cave.png · cell 0, 0")
	}
	if !app.Exists("id:tile-palette-rules-note") {
		t.Fatal("expected palette rules note from Tiles.dc.html")
	}

	// Mark tile 0 One-way from the right-side Tile Inspector, then Undo restores None.
	app.Press("id:tile-inspector-coll-oneway")
	info0, _ := ed.TilesetTileInfo(0)
	if !info0.OneWay || info0.Solid {
		t.Fatalf("tile 0 after inspector one-way = %+v", info0)
	}
	app.Press("id:toolbar-undo")
	info0, _ = ed.TilesetTileInfo(0)
	if info0.OneWay || info0.Solid {
		t.Fatalf("tile 0 after undo = %+v, want neither solid nor one-way", info0)
	}

	// Open Collision tab in Tileset Editor, set custom flag bit 2, add a rect and draw a polygon over the tile.
	app.Press("id:tile-inspector-open-editor")
	if ed.TilesetTab() != editor.TilesetTabCollision {
		t.Fatalf("tab = %q, want collision", ed.TilesetTab())
	}
	app.Press("id:tileset-flag-bit-2")
	app.Press("id:tileset-shape-add-rect")
	app.Press("id:tileset-shape-tool-polygon")
	// Click 3 points on the tile canvas and commit polygon.
	app.Post(func() {
		ed.AddTilesetDraftPolygonPoint(0, 16)
		ed.AddTilesetDraftPolygonPoint(16, 0)
		ed.AddTilesetDraftPolygonPoint(16, 16)
	})
	app.Settle()
	app.Press("id:tileset-shape-finish-poly")

	info0, _ = ed.TilesetTileInfo(0)
	if info0.Flags != 4 || len(info0.Shapes) != 2 || info0.Shapes[0].Kind != "rect" || info0.Shapes[1].Kind != "polygon" {
		t.Fatalf("tile 0 collision = %+v", info0)
	}
	// Verify polygon shape removal is undoable.
	app.Press("id:tileset-shape-remove-1")
	info0, _ = ed.TilesetTileInfo(0)
	if len(info0.Shapes) != 1 {
		t.Fatalf("shapes after remove = %d, want 1", len(info0.Shapes))
	}
	app.Press("id:toolbar-undo")
	info0, _ = ed.TilesetTileInfo(0)
	if len(info0.Shapes) != 2 {
		t.Fatalf("shapes after undo = %d, want 2", len(info0.Shapes))
	}

	// Switch to Data tab: set name, variant weight, and typed custom properties (string, int, float, bool).
	app.Press("id:tileset-tab-data")
	app.Clear("id:tileset-tile-name")
	app.Type("id:tileset-tile-name", "moss")
	app.Submit("id:tileset-tile-name")

	app.Clear("id:tileset-tile-weight")
	app.Type("id:tileset-tile-weight", "0.5")
	app.Submit("id:tileset-tile-weight")

	addProp := func(kind, key, val string) {
		app.Press("id:tileset-prop-type-" + kind)
		app.Clear("id:tileset-prop-key")
		app.Type("id:tileset-prop-key", key)
		app.Clear("id:tileset-prop-value")
		app.Type("id:tileset-prop-value", val)
		app.Press("id:tileset-prop-add")
	}
	addProp("string", "surface", "moss")
	addProp("int", "damage", "2")
	addProp("float", "friction", "0.75")
	addProp("bool", "slippery", "true")

	info0, _ = ed.TilesetTileInfo(0)
	if info0.Name != "moss" || info0.Weight == nil || *info0.Weight != 0.5 {
		t.Fatalf("tile 0 name/weight = (%q, %v)", info0.Name, info0.Weight)
	}
	if info0.Properties["surface"] != "moss" || info0.Properties["damage"] != 2 || info0.Properties["friction"] != 0.75 || info0.Properties["slippery"] != true {
		t.Fatalf("tile 0 properties = %#v", info0.Properties)
	}

	// Delete a property and undo it.
	app.Press("id:tileset-prop-delete-damage")
	info0, _ = ed.TilesetTileInfo(0)
	if _, exists := info0.Properties["damage"]; exists {
		t.Fatal("damage property was not deleted")
	}
	app.Press("id:toolbar-undo")
	info0, _ = ed.TilesetTileInfo(0)
	if info0.Properties["damage"] != 2 {
		t.Fatalf("damage property after undo = %v, want 2", info0.Properties["damage"])
	}

	// Save and verify canonical round-trip YAML.
	app.Press("id:tileset-editor-save")
	saved, err := os.ReadFile(tsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "name: moss\n") || !strings.Contains(string(saved), "weight: 0.5\n") {
		t.Fatalf("saved tileset missing name/weight:\n%s", saved)
	}
	parsed, err := tilemap.ParseTileset(saved)
	if err != nil {
		t.Fatalf("ParseTileset: %v", err)
	}
	again, err := tilemap.EncodeTileset(parsed)
	if err != nil || !bytes.Equal(again, saved) {
		t.Fatalf("saved tileset did not round-trip unchanged")
	}
}
