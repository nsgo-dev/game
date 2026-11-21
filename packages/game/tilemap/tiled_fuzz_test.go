package tilemap_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func FuzzLoadTiled(f *testing.F) {
	sampleMap, err := os.ReadFile(filepath.Join("testdata", "sample.tmj"))
	if err != nil {
		f.Fatalf("read sample.tmj: %v", err)
	}
	sampleTileset, err := os.ReadFile(filepath.Join("testdata", "tilesets", "props.tsj"))
	if err != nil {
		f.Fatalf("read props.tsj: %v", err)
	}

	f.Add(sampleMap, sampleTileset)
	f.Add(sampleMap, []byte(`{}`))
	f.Add([]byte(`{"width":2,"height":2,"tilewidth":16,"tileheight":16,"orientation":"orthogonal"}`), []byte{})
	f.Add([]byte(`{}`), []byte{})

	f.Fuzz(func(t *testing.T, mapData, extTilesetData []byte) {
		m, err := tilemap.LoadTiled(mapData, tilemap.TilesetLoader{
			LoadJSON: func(string) ([]byte, error) {
				return extTilesetData, nil
			},
			LoadImage: func(imagePath string) (core.ImageSource, error) {
				return core.ImageSource{URI: "asset:" + imagePath}, nil
			},
		})
		if err != nil || m == nil {
			return
		}

		m.Update(16 * time.Millisecond)
		m.Update(250 * time.Millisecond)

		tw, th := float64(m.TileWidth), float64(m.TileHeight)
		if math.IsNaN(tw) || math.IsInf(tw, 0) || math.IsNaN(th) || math.IsInf(th, 0) || tw <= 0 || th <= 0 {
			return
		}
		// Keep probe extents bounded to a handful of tiles so extreme fuzzed
		// dimensions cannot turn a single fuzz iteration into a billion-cell walk.
		spanX := float32(math.Min(tw*8, 1e6))
		spanY := float32(math.Min(th*6, 1e6))

		var d core.Drawing
		cam := &tilemap.Camera{X: spanX / 2, Y: spanY / 2, Zoom: 1, Width: spanX, Height: spanY}
		m.Draw(&d, cam)
		if m.Width <= 64 && m.Height <= 64 {
			d.Ops = d.Ops[:0]
			m.Draw(&d, nil)
		}

		_ = m.TilesInRect(core.Bounds{
			Origin: core.Point{X: 0, Y: 0},
			Size:   core.Size{Width: core.Points(spanX), Height: core.Points(spanY)},
		}, tilemap.TileSolid)
		_ = m.TilesInRect(core.Bounds{
			Origin: core.Point{X: core.Points(-tw), Y: core.Points(-th)},
			Size:   core.Size{Width: core.Points(spanX), Height: core.Points(spanY)},
		}, 0)

		_, _ = m.Raycast(
			core.Point{X: 0, Y: 0},
			core.Point{X: core.Points(spanX), Y: core.Points(spanY)},
			tilemap.TileSolid,
		)
		_, _ = m.Raycast(
			core.Point{X: core.Points(spanX), Y: core.Points(spanY)},
			core.Point{X: 0, Y: 0},
			0,
		)
	})
}
