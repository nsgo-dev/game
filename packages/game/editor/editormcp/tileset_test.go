package editormcp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tilemap"
)

func TestTilesetReadAndEditViaMCP(t *testing.T) {
	f := openFixture(t)

	tsRel := "tilesets/dungeon.tileset.yaml"
	tsPath := filepath.Join(f.dir, filepath.FromSlash(tsRel))
	if err := os.MkdirAll(filepath.Dir(tsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := &tilemap.TilesetDoc{
		Version:    1,
		Name:       "dungeon",
		TileWidth:  16,
		TileHeight: 16,
		Image:      "art/tiles/dungeon.png",
		Columns:    4,
		TileCount:  8,
	}
	raw, err := tilemap.EncodeTileset(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tsPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// 1. Read tileset from disk.
	readRes := call(t, cs, editormcp.ToolTilesetRead, map[string]any{"file": tsRel})
	mustOK(t, readRes)
	var tr editormcp.TilesetReadResult
	structured(t, readRes, &tr)
	if tr.File != tsRel || tr.Source != "disk" || tr.TileCount != 8 || tr.Unsaved {
		t.Fatalf("tileset_read initial = %+v", tr)
	}

	// 2. Edit tileset through MCP: name, solid, custom flags, weight, shapes, animation, custom properties, terrain — all in one undo step.
	editRes := call(t, cs, editormcp.ToolTilesetEdit, map[string]any{
		"file": tsRel,
		"ops": []map[string]any{
			{"op": "add_terrain", "name": "cave", "mode": "cardinal"},
			{
				"op":        "set_tile",
				"id":        0,
				"name":      "moss",
				"solid":     true,
				"flags":     4,
				"weight":    0.5,
				"terrain":   "cave",
				"neighbors": []string{"east"},
				"shapes": []map[string]any{
					{"kind": "polygon", "points": [][]float64{{0, 16}, {16, 0}, {16, 16}}},
				},
				"animation": []map[string]any{
					{"tile": 0, "duration": "150ms"},
					{"tile": 1, "duration": "150ms"},
				},
			},
			{"op": "set_property", "id": 0, "key": "friction", "value": 0.75},
			{"op": "set_property", "id": 0, "key": "slippery", "value": true},
		},
	})
	mustOK(t, editRes)
	var te editormcp.TilesetEditResult
	structured(t, editRes, &te)
	if te.File != tsRel || te.Applied != 4 || te.Saved || !te.Unsaved || te.Tiles != 1 {
		t.Fatalf("tileset_edit = %+v", te)
	}

	// 3. Reading through tileset_read AND scene_read sees the unsaved live editor state.
	liveRes := call(t, cs, editormcp.ToolTilesetRead, map[string]any{})
	mustOK(t, liveRes)
	var live editormcp.TilesetReadResult
	structured(t, liveRes, &live)
	if live.Source != "editor" || !live.Unsaved || len(live.Tiles) != 1 {
		t.Fatalf("live tileset_read = %+v", live)
	}
	tile0 := live.Tiles[0]
	if tile0.Name != "moss" || !tile0.Solid || tile0.Flags != 4 || tile0.Weight == nil || *tile0.Weight != 0.5 ||
		len(tile0.Shapes) != 1 || len(tile0.Animation) != 2 || tile0.Terrain != "cave" || tile0.Properties["slippery"] != true {
		t.Fatalf("live tile 0 = %+v", tile0)
	}
	sr := call(t, cs, editormcp.ToolSceneRead, map[string]any{"file": tsRel})
	if text := mustOK(t, sr); !strings.Contains(text, "name: moss") || !strings.Contains(text, "unsaved edits") {
		t.Fatalf("scene_read unsaved tileset:\n%s", text)
	}

	// 4. One Undo reverts the entire MCP tileset_edit call!
	if !f.ed.Undo() {
		t.Fatal("Undo returned false")
	}
	if len(f.ed.TilesetDoc().Tiles) != 0 || len(f.ed.TilesetDoc().Terrains) != 0 {
		t.Fatalf("after Undo, doc still has %+v", f.ed.TilesetDoc())
	}

	// 5. Redo + save via tileset_edit with save:true persists canonical YAML to disk.
	if !f.ed.Redo() {
		t.Fatal("Redo returned false")
	}
	saveRes := call(t, cs, editormcp.ToolTilesetEdit, map[string]any{
		"file": tsRel,
		"save": true,
		"ops": []map[string]any{
			{"op": "set_property", "id": 0, "key": "surface", "value": "stone"},
		},
	})
	mustOK(t, saveRes)
	onDisk, err := os.ReadFile(tsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(onDisk), "surface: stone") || !strings.Contains(string(onDisk), "weight: 0.5") {
		t.Fatalf("saved tileset on disk:\n%s", onDisk)
	}

	// 6. When the Tileset Editor is closed, tileset_read with no file falls back to the active layer's tilesetFile.
	if err := f.ed.CloseTileset(); err != nil {
		t.Fatalf("CloseTileset: %v", err)
	}
	f.ed.Doc.Layers[0].Tilemap = &sceneformat.TilemapDoc{
		TileWidth:   16,
		TileHeight:  16,
		Width:       2,
		Height:      1,
		TilesetFile: tsRel,
		Data:        []int{1, 0},
	}
	fallbackRes := call(t, cs, editormcp.ToolTilesetRead, nil)
	if text := mustOK(t, fallbackRes); !strings.Contains(text, tsRel) || !strings.Contains(text, "surface: stone") {
		t.Fatalf("tileset_read active-layer fallback:\n%s", text)
	}
}
