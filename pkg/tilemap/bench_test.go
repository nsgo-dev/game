package tilemap_test

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/tilemap"
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

// BenchmarkChunkedWalker10000StreamAndDraw steps a walker across 10,000 tiles in +X, -X, +Y, and -Y
// while streaming chunks on demand and culling/drawing each frame, verifying bounded active chunks and smooth frame time.
func BenchmarkChunkedWalker10000StreamAndDraw(b *testing.B) {
	ts := tilemap.NewGridTileset(1, core.ImageSource{URI: "asset:bench-tiles.png"}, 16, 16, 8, 64)
	m := tilemap.NewInfinite(16, 16, ts)
	m.Layers = []tilemap.TileLayer{tilemap.NewChunkedTileLayer("world", 16)}

	const loadR, unloadR = 2, 3
	maxAllowed := (2*unloadR + 1) * (2*unloadR + 1)
	stream := tilemap.NewChunkStream(tilemap.ChunkStreamConfig{
		Map:          m,
		LayerIndex:   0,
		LoadRadius:   loadR,
		UnloadRadius: unloadR,
		Generator: func(coord tilemap.ChunkCoord, ch *tilemap.Chunk) error {
			base := coord.X*31 + coord.Y*17
			for ly := range ch.Size {
				rowOff := ly * ch.Size
				for lx := range ch.Size {
					ch.Tiles[rowOff+lx] = uint32(((base + lx + ly) & 7) + 1)
				}
			}
			return nil
		},
	})
	defer func() { _ = stream.Close() }()

	cam := &tilemap.Camera{Zoom: 1, Width: 320, Height: 180}
	d := core.Drawing{Ops: make([]core.DrawOp, 0, 512)}

	// Pre-build a 4-leg path crossing 10,000 tiles in +X, -X, +Y, -Y (40,000 tiles total).
	const legTiles = 10000
	const strideTiles = 1
	stepsPerLeg := legTiles / strideTiles
	totalSteps := stepsPerLeg * 4
	posAt := func(step int) core.Point {
		idx := step % totalSteps
		leg := idx / stepsPerLeg
		off := core.Points((idx % stepsPerLeg) * strideTiles * 16)
		switch leg {
		case 0:
			return core.Point{X: off, Y: 0}
		case 1:
			return core.Point{X: -off, Y: 0}
		case 2:
			return core.Point{X: 0, Y: off}
		default:
			return core.Point{X: 0, Y: -off}
		}
	}

	// Warm initial neighborhood around origin.
	stream.Update(core.Point{})
	m.Draw(&d, cam)

	maxChunks := 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		p := posAt(i)
		stream.Update(p)
		cam.X, cam.Y = float32(p.X), float32(p.Y)
		d.Ops = d.Ops[:0]
		m.Draw(&d, cam)
		if n := m.Layers[0].ChunkCount(); n > maxChunks {
			maxChunks = n
		}
	}
	b.StopTimer()
	if maxChunks > maxAllowed {
		b.Fatalf("loaded chunks %d exceeded bound %d", maxChunks, maxAllowed)
	}
	b.ReportMetric(float64(maxChunks), "max_chunks")
}

// BenchmarkChunkedWalker10000CachedDraw benchmarks per-chunk static caching + culled drawing
// as a walker moves across 10,000 tiles in each direction (+X, -X, +Y, -Y).
func BenchmarkChunkedWalker10000CachedDraw(b *testing.B) {
	sheet := image.NewRGBA(image.Rect(0, 0, 64, 16))
	for i := range 4 {
		c := color.RGBA{R: uint8(40 + i*50), G: uint8(180 - i*30), B: uint8(60 + i*40), A: 255}
		for y := range 16 {
			for x := range 16 {
				sheet.SetRGBA(i*16+x, y, c)
			}
		}
	}
	ts := tilemap.NewGridTileset(1, core.ImageSource{URI: "asset:bench-cached.png", Pixels: sheet}, 16, 16, 4, 4)
	m := tilemap.NewInfinite(16, 16, ts)
	m.Layers = []tilemap.TileLayer{tilemap.NewChunkedTileLayer("world", 16)}

	const loadR, unloadR = 2, 3
	maxAllowed := (2*unloadR + 1) * (2*unloadR + 1)
	stream := tilemap.NewChunkStream(tilemap.ChunkStreamConfig{
		Map:          m,
		LayerIndex:   0,
		LoadRadius:   loadR,
		UnloadRadius: unloadR,
		Generator: func(coord tilemap.ChunkCoord, ch *tilemap.Chunk) error {
			base := coord.X*31 + coord.Y*17
			for ly := range ch.Size {
				rowOff := ly * ch.Size
				for lx := range ch.Size {
					ch.Tiles[rowOff+lx] = uint32(((base + lx + ly) & 3) + 1)
				}
			}
			return nil
		},
		OnLoad: func(coord tilemap.ChunkCoord, _ *tilemap.Chunk) {
			_, _ = m.CacheStaticChunk(0, coord, nil)
		},
	})
	defer func() { _ = stream.Close() }()

	cam := &tilemap.Camera{Zoom: 1, Width: 320, Height: 180}
	d := core.Drawing{Ops: make([]core.DrawOp, 0, 32)}

	const stepsPerLeg = 10000
	const totalSteps = stepsPerLeg * 4
	posAt := func(step int) core.Point {
		idx := step % totalSteps
		leg := idx / stepsPerLeg
		off := core.Points((idx % stepsPerLeg) * 16)
		switch leg {
		case 0:
			return core.Point{X: off, Y: 0}
		case 1:
			return core.Point{X: -off, Y: 0}
		case 2:
			return core.Point{X: 0, Y: off}
		default:
			return core.Point{X: 0, Y: -off}
		}
	}

	stream.Update(core.Point{})
	m.Draw(&d, cam)

	maxChunks := 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		p := posAt(i)
		stream.Update(p)
		cam.X, cam.Y = float32(p.X), float32(p.Y)
		d.Ops = d.Ops[:0]
		m.Draw(&d, cam)
		if n := m.Layers[0].ChunkCount(); n > maxChunks {
			maxChunks = n
		}
	}
	b.StopTimer()
	if maxChunks > maxAllowed {
		b.Fatalf("loaded chunks %d exceeded bound %d", maxChunks, maxAllowed)
	}
	b.ReportMetric(float64(maxChunks), "max_chunks")
}
