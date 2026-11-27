package tileshape_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/game/pkg/tilemap/tileshape"
	"github.com/nsgo-dev/nsgo/core"
)

// FuzzView builds a View over arbitrary Tiled maps, edits a cell, and checks that the geometry never panics, stays finite and matches a fresh View after the edit.
func FuzzView(f *testing.F) {
	sample, err := os.ReadFile(filepath.Join("testdata", "slopes.tmj"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(sample, 3, 4, uint32(2|tilemap.FlipDiagonal))
	f.Add([]byte(`{"width":2,"height":2,"tilewidth":8,"tileheight":4,"orientation":"orthogonal","layers":[{"type":"tilelayer","data":[1,1,0,1]}],"tilesets":[{"firstgid":1,"tilewidth":16,"tileheight":16,"tiles":[{"id":0,"objectgroup":{"objects":[{"x":-4,"y":2,"width":30,"height":3,"ellipse":true,"rotation":33}]}}]}]}`), 0, 1, uint32(1))
	f.Fuzz(func(t *testing.T, data []byte, col, row int, gid uint32) {
		m, err := tilemap.LoadTiled(data, tilemap.TilesetLoader{})
		if err != nil || m.Width*m.Height > 4096 || m.Width > 512 || m.Height > 512 || len(m.Layers) > 4 {
			return
		}
		v := tileshape.New(m, tileshape.Options{})
		check := func(v *tileshape.View) {
			for _, s := range v.Outline() {
				for _, p := range []core.Point{s.A, s.B} {
					if math.IsNaN(float64(p.X)) || math.IsNaN(float64(p.Y)) {
						t.Fatalf("outline segment %v is not finite", s)
					}
				}
			}
			_ = v.Slopes()
			_ = v.Boxes()
			_ = v.Occluders()
			_ = v.AppendSlopesIn(nil, core.Bounds{Size: core.Size{Width: 64, Height: 64}})
		}
		check(v)
		if len(m.Layers) == 0 {
			return
		}
		m.SetTile(0, col, row, gid)
		check(v)
		fresh := tileshape.New(m, tileshape.Options{})
		if !sameSegments(v.Outline(), fresh.Outline()) || !sameSlopes(v.Slopes(), fresh.Slopes()) {
			t.Fatalf("after SetTile(%d, %d, %#x) the View differs from a fresh one", col, row, gid)
		}
	})
}
