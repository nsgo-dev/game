package editor

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

func TestTilePaletteCachesDiskReadsAndReviewCachesPerimeters(t *testing.T) {
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

	img := image.NewNRGBA(image.Rect(0, 0, 32, 16))
	img.SetNRGBA(0, 0, color.NRGBA{200, 100, 50, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	write("art/tiles.png", buf.Bytes())
	write("tiles.tileset.yaml", []byte(`version: 1
name: tiny
tileWidth: 16
tileHeight: 16
image: art/tiles.png
columns: 2
tileCount: 2
tiles:
  - id: 0
    name: grass
  - id: 1
    name: dirt
`))
	write("game.yaml", []byte(`version: 1
name: cache-test
id: dev.nsgo.cache
appVersion: 1.0.0
window:
  width: 320
  height: 240
autoload:
  mainScene: main
  scenes:
    - name: main
      path: scenes/main.scene.yaml
`))
	row := sceneformat.FormatTileRow(make([]uint32, 4))
	doc := &sceneformat.SceneDoc{
		Version:       2,
		Name:          "main",
		VirtualWidth:  320,
		VirtualHeight: 240,
		Layers: []sceneformat.LayerDoc{{
			Name:  "World",
			Space: sceneformat.SpaceWorld,
			Tilemap: &sceneformat.TilemapDoc{
				Width:       4,
				Height:      4,
				TileWidth:   16,
				TileHeight:  16,
				TilesetFile: "tiles.tileset.yaml",
				TileLayers:  []sceneformat.TileLayerDoc{{Name: "ground", Rows: []string{row, row, row, row}}},
			},
		}},
	}
	rawScene, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("sceneformat.Encode: %v", err)
	}
	write("scenes/main.scene.yaml", rawScene)

	ed, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ed.ProjectAssets = true
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.SetActiveLayer("World")

	// Repeated TilePalette() calls without file/asset revision changes must do 0 additional disk reads.
	p1 := ed.TilePalette()
	if len(p1.Tiles) != 2 || len(p1.Tiles[0].Image.Data) == 0 {
		t.Fatalf("expected 2 palette tiles with image, got %+v", p1)
	}
	readsAfterFirst := ed.paletteImgCache.diskReads
	if readsAfterFirst == 0 {
		t.Fatal("expected at least 1 disk read to populate palette image cache")
	}
	for range 10 {
		ed.notify()
		p := ed.TilePalette()
		if len(p.Tiles) != 2 || len(p.Tiles[0].Image.Data) == 0 {
			t.Fatalf("expected cached palette tiles with image, got %+v", p)
		}
	}
	if ed.paletteImgCache.diskReads != readsAfterFirst {
		t.Fatalf("diskReads after 10 cached TilePalette calls = %d, want %d", ed.paletteImgCache.diskReads, readsAfterFirst)
	}
	// Bumping filesRev invalidates the cache so edits on disk reload cleanly.
	ed.filesRev++
	_ = ed.TilePalette()
	if ed.paletteImgCache.diskReads != readsAfterFirst+1 {
		t.Fatalf("diskReads after filesRev bump = %d, want %d", ed.paletteImgCache.diskReads, readsAfterFirst+1)
	}

	// Paint a 2x1 region under review -> precomputed perimeter should have 6 edges and uiRev stays stable across unrelated notify().
	if _, err := ed.AgentPaintTiles(AgentTilePaintRequest{
		Layer:       "World",
		TileName:    "grass",
		Rects:       []AgentTileRect{{Col0: 0, Row0: 0, Col1: 1, Row1: 0}},
		TrackReview: true,
	}); err != nil {
		t.Fatalf("AgentPaintTiles: %v", err)
	}
	if ed.tileReview == nil || len(ed.tileReview.perimeters) != 1 || len(ed.tileReview.perimeters[0]) != 6 {
		t.Fatalf("expected 6 precomputed perimeter edges for 2x1 region, got %+v", ed.tileReview.perimeters)
	}
	rev1 := ed.tileReviewUIRev()
	if rev1 == 0 {
		t.Fatal("expected non-zero tileReviewUIRev when review is open")
	}
	ed.notify()
	if ed.tileReviewUIRev() != rev1 {
		t.Fatalf("unrelated notify() changed tileReviewUIRev from %d to %d", rev1, ed.tileReviewUIRev())
	}
	if err := ed.SetTileReviewRegionReverted(ed.tileReview.regions[0].ID, true); err != nil {
		t.Fatalf("SetTileReviewRegionReverted: %v", err)
	}
	if ed.tileReviewUIRev() == rev1 {
		t.Fatal("SetTileReviewRegionReverted did not bump tileReviewUIRev")
	}
}
