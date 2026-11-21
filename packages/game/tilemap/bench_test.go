package tilemap_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func buildBenchTilemap1000() *tilemap.Tilemap {
	const size = 1000
	ts := tilemap.NewGridTileset(1, core.ImageSource{URI: "asset:bench-tiles.png"}, 16, 16, 8, 64)
	m := tilemap.New(size, size, 16, 16, ts)

	ground := make([]uint32, size*size)
	props := make([]uint32, size*size)
	for row := range size {
		rowOff := row * size
		for col := range size {
			gid := uint32((row+col)%8 + 1)
			ground[rowOff+col] = gid
			if (col%17 == 0 && row%17 == 0) || col == 550 {
				props[rowOff+col] = 9
			}
		}
	}
	m.Layers = []tilemap.TileLayer{
		tilemap.NewTileLayer("ground", size, size, ground),
		tilemap.NewTileLayer("props", size, size, props),
	}
	m.SetFlags(9, tilemap.TileSolid)
	return m
}

func BenchmarkTilemapDrawVisible1000x1000(b *testing.B) {
	m := buildBenchTilemap1000()
	cam := &tilemap.Camera{
		X:      8000,
		Y:      8000,
		Zoom:   1,
		Width:  640,
		Height: 360,
	}
	d := core.Drawing{Ops: make([]core.DrawOp, 0, 2048)}
	m.Draw(&d, cam)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		d.Ops = d.Ops[:0]
		m.Draw(&d, cam)
	}
	if len(d.Ops) == 0 {
		b.Fatal("expected draw ops")
	}
}

func BenchmarkLoadTiled(b *testing.B) {
	mapData, err := os.ReadFile(filepath.Join("testdata", "sample.tmj"))
	if err != nil {
		b.Fatal(err)
	}
	propsData, err := os.ReadFile(filepath.Join("testdata", "tilesets", "props.tsj"))
	if err != nil {
		b.Fatal(err)
	}
	loader := tilemap.TilesetLoader{
		LoadJSON: func(string) ([]byte, error) {
			return propsData, nil
		},
		LoadImage: func(imagePath string) (core.ImageSource, error) {
			return core.ImageSource{URI: "asset:" + imagePath}, nil
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	var last *tilemap.Tilemap
	for range b.N {
		m, err := tilemap.LoadTiled(mapData, loader)
		if err != nil {
			b.Fatal(err)
		}
		last = m
	}
	if last == nil || len(last.Layers) == 0 {
		b.Fatal("expected loaded layers")
	}
}

func BenchmarkRaycast(b *testing.B) {
	m := buildBenchTilemap1000()
	from := core.Point{X: 500*16 + 8, Y: 500*16 + 8}
	to := core.Point{X: 560*16 + 8, Y: 525*16 + 8}

	b.ReportAllocs()
	b.ResetTimer()
	var hit tilemap.RaycastHit
	var ok bool
	for range b.N {
		hit, ok = m.Raycast(from, to, tilemap.TileSolid)
	}
	if !ok || hit.Col != 550 {
		b.Fatalf("unexpected raycast hit: ok=%v hit=%+v", ok, hit)
	}
}
