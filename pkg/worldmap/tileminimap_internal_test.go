package worldmap

import (
	"image"
	"image/color"
	"testing"

	"github.com/nsgo-dev/game/pkg/fog"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func TestTileMinimapSkipsFullRebuildsAndUnchangedFog(t *testing.T) {
	sheet := image.NewRGBA(image.Rect(0, 0, 16, 8))
	for y := range 8 {
		for x := range 8 {
			sheet.SetRGBA(x, y, color.RGBA{R: 30, G: 140, B: 40, A: 255})
			sheet.SetRGBA(8+x, y, color.RGBA{R: 40, G: 80, B: 200, A: 255})
		}
	}
	ts := tilemap.NewGridTileset(1, core.ImageSource{Pixels: sheet}, 8, 8, 2, 2)
	m := tilemap.New(64, 64, 16, 16, ts)
	data := make([]uint32, 64*64)
	for i := range data {
		data[i] = 1
	}
	m.Layers = append(m.Layers, tilemap.NewTileLayer("ground", 64, 64, data))

	tm := NewTileMinimap(m, nil)
	if tm.terrainRebuilds != 1 || tm.cellUpdates != 0 {
		t.Fatalf("after NewTileMinimap: rebuilds=%d cellUpdates=%d, want 1, 0", tm.terrainRebuilds, tm.cellUpdates)
	}

	f := fog.New(64, 64, 16, 16)
	f.AddSource(fog.Source{Team: 1, Col: 10, Row: 10, Radius: 6})
	f.Update()
	style := DefaultTileMinimapStyle()

	// First sync composites fog once.
	tm.Sync(f, 1, &style)
	if tm.fogComposites != 1 {
		t.Fatalf("after first fog sync: fogComposites=%d, want 1", tm.fogComposites)
	}

	// Repeating sync with unchanged fog.Revision performs 0 fog composites and 0 terrain rebuilds.
	for range 5 {
		tm.Sync(f, 1, &style)
	}
	if tm.fogComposites != 1 || tm.terrainRebuilds != 1 {
		t.Fatalf("after unchanged frames: fogComposites=%d rebuilds=%d, want 1, 1", tm.fogComposites, tm.terrainRebuilds)
	}

	// Single SetTile updates exactly 1 cell in both terrain and composited without a full terrain rebuild or fog recomposite.
	m.SetTile(0, 10, 10, 2)
	tm.Sync(f, 1, &style)
	if tm.terrainRebuilds != 1 || tm.cellUpdates != 1 || tm.fogComposites != 1 {
		t.Fatalf("after single SetTile: rebuilds=%d cellUpdates=%d fogComposites=%d, want 1, 1, 1",
			tm.terrainRebuilds, tm.cellUpdates, tm.fogComposites)
	}

	// Advancing fog.Revision triggers exactly 1 fog recomposite.
	f.Explore(1, 30, 30)
	tm.Sync(f, 1, &style)
	if tm.fogComposites != 2 {
		t.Fatalf("after fog change: fogComposites=%d, want 2", tm.fogComposites)
	}
}
