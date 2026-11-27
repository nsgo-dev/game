package tilemap_test

import (
	"math"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	fuzzLDtkMaxLayerTiles = 16 << 20
	fuzzLDtkMaxTotalTiles = 64 << 20
)

func checkFuzzedLDtkMap(t *testing.T, m *tilemap.Tilemap) int {
	t.Helper()
	if m == nil {
		t.Fatal("LDtk loader returned nil Tilemap with nil error")
	}
	if m.Width < 0 || m.Height < 0 {
		t.Fatalf("Tilemap negative size %dx%d", m.Width, m.Height)
	}
	total := 0
	for i := range m.Layers {
		l := &m.Layers[i]
		if l.Width <= 0 || l.Height <= 0 {
			t.Fatalf("layer %d (%q) has non-positive size %dx%d", i, l.Name, l.Width, l.Height)
		}
		if l.Width > math.MaxInt/l.Height {
			t.Fatalf("layer %d (%q) size %dx%d overflows", i, l.Name, l.Width, l.Height)
		}
		cells := l.Width * l.Height
		if cells > fuzzLDtkMaxLayerTiles {
			t.Fatalf("layer %d (%q) has %d cells, exceeds max %d", i, l.Name, cells, fuzzLDtkMaxLayerTiles)
		}
		if len(l.Data) != cells {
			t.Fatalf("layer %d (%q) len(Data) = %d, want %d (%dx%d)", i, l.Name, len(l.Data), cells, l.Width, l.Height)
		}
		total += cells
	}
	return total
}

func FuzzLoadLDtk(f *testing.F) {
	f.Add([]byte(sampleLDtkJSON))
	f.Add([]byte(groupedLDtkJSON))
	f.Add([]byte(stackedLDtkJSON))
	f.Add([]byte(pivotEntitiesLDtkJSON))
	f.Add(makeAdjacentLDtkProject("Free", 0, 0, 64, 0))
	f.Add(makeAdjacentLDtkProject("GridVania", 0, 0, 64, 0))
	f.Add([]byte(`{"defaultGridSize":16,"worldLayout":null,"levels":[],"worlds":[{"identifier":"Overworld","worldLayout":"Free","levels":[{"identifier":"Meadow","iid":"m1","uid":1,"worldX":0,"worldY":0,"pxWid":64,"pxHei":64,"layerInstances":[]}]}]}`))
	f.Add([]byte(`{"defaultGridSize":16,"worldLayout":"LinearHorizontal","levels":[{"identifier":"A","pxWid":32,"pxHei":32}]}`))
	f.Add([]byte(`{"defaultGridSize":16,"levels":[{"identifier":"L","pxWid":0,"pxHei":0,"layerInstances":[{"__identifier":"G","__type":"IntGrid","__cWid":0,"__cHei":0,"__gridSize":16,"intGridCsv":[1]}]}]}`))
	f.Add([]byte(`{"defAultGridSiZe":1,"levels":[{"lAYerInstAnCes":[{"__tYpe":"IntGrid","intGridCsv":[1]}]}]}`))
	f.Add([]byte(`{"defaultGridSize":16,"levels":[{"identifier":"L","pxWid":64,"pxHei":64,"layerInstances":[{"__identifier":"T","__type":"Tiles","__cWid":20000,"__cHei":20000,"__gridSize":16}]}]}`))
	f.Add([]byte(`{"defaultGridSize":16,"levels":[{"identifier":"L","pxWid":64,"pxHei":64,"layerInstances":[{"__identifier":"T","__type":"Tiles","__cWid":4294967296,"__cHei":4294967297,"__gridSize":16}]}]}`))
	f.Add([]byte(`{"defaultGridSize":16,"levels":[{"identifier":"L","pxWid":32,"pxHei":16,"layerInstances":[{"__identifier":"G","__type":"IntGrid","__cWid":2,"__cHei":1,"__gridSize":16,"intGridCsv":[1,0,1]}]}]}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"levels":[]}`))

	opts := tilemap.LDtkOptions{
		ResolveImage: func(relPath string) core.ImageSource {
			return core.ImageSource{URI: "asset:" + relPath}
		},
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if all, err := tilemap.LoadLDtkAllWith(data, opts); err == nil {
			keys := make([]string, 0, len(all))
			for k := range all {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			totalCells := 0
			for i, k := range keys {
				m := all[k]
				totalCells += checkFuzzedLDtkMap(t, m)
				if i < 4 {
					exerciseFuzzedTilemap(m)
				}
			}
			if totalCells > fuzzLDtkMaxTotalTiles {
				t.Fatalf("LoadLDtkAllWith allocated %d cells across %d levels, exceeds max %d", totalCells, len(all), fuzzLDtkMaxTotalTiles)
			}
		}

		if w, err := tilemap.LoadLDtkWorldWith(data, "", opts); err == nil && w != nil {
			limit := min(len(w.Levels), 4)
			for i := range limit {
				lvl := &w.Levels[i]
				if lvl.Bounds.Size.Width < 0 || lvl.Bounds.Size.Height < 0 {
					t.Fatalf("WorldLevel[%d] (%q) has negative bounds %+v", i, lvl.ID, lvl.Bounds)
				}
				if m, err := lvl.Load(); err == nil {
					if cells := checkFuzzedLDtkMap(t, m); cells > fuzzLDtkMaxTotalTiles {
						t.Fatalf("WorldLevel[%d].Load() allocated %d cells, exceeds max %d", i, cells, fuzzLDtkMaxTotalTiles)
					}
					exerciseFuzzedTilemap(m)
				}
			}
		}
	})
}
