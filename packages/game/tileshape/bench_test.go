package tileshape_test

import (
	"math/rand/v2"
	"testing"

	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/game/packages/game/tileshape"
)

// benchMap returns a 256 by 256 map of solid cells and slope, half and flipped tiles from testdata/slopes.tmj's tileset.
func benchMap(b *testing.B) *tilemap.Tilemap {
	src := loadSlopes(b)
	m := tilemap.New(256, 256, 16, 16, src.Tilesets...)
	m.Layers = append(m.Layers, tilemap.NewTileLayer("ground", 256, 256, nil))
	m.SetFlags(1, tilemap.TileSolid)
	gids := []uint32{0, 0, 0, 1, 1, 2, 3, 4, 5, 2 | tilemap.FlipHorizontal}
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range m.Layers[0].Data {
		m.Layers[0].Data[i] = gids[rng.IntN(len(gids))]
	}
	return m
}

// BenchmarkBuild measures building every chunk's geometry of a 256 by 256 map from scratch.
func BenchmarkBuild(b *testing.B) {
	m := benchMap(b)
	v := tileshape.New(m, tileshape.Options{})
	b.ReportAllocs()
	for b.Loop() {
		v.InvalidateAll()
		_ = v.Outline()
	}
}

// BenchmarkSetTileRebuild measures one SetTile and the rebuild of the chunks around it on a 256 by 256 map.
func BenchmarkSetTileRebuild(b *testing.B) {
	m := benchMap(b)
	v := tileshape.New(m, tileshape.Options{})
	_ = v.Outline()
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		i++
		m.SetTile(0, 100+i%40, 100, uint32(i%6))
		_ = v.Occluders()
	}
}

// BenchmarkTileFlagsAt measures the grid lookup physics makes for every cell a body touches.
func BenchmarkTileFlagsAt(b *testing.B) {
	m := benchMap(b)
	v := tileshape.New(m, tileshape.Options{})
	_ = v.TileFlagsAt(0, 0)
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		i++
		_ = v.TileFlagsAt(i%256, (i/256)%256)
	}
}
