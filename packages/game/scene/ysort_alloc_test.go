package scene

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func TestLayerBuildDrawEntriesYSort100x100ZeroAllocs(t *testing.T) {
	const size = 100
	raw := []byte(`{
		"width": 100, "height": 100, "tilewidth": 16, "tileheight": 16,
		"orientation": "orthogonal",
		"tilesets": [{"firstgid": 1, "name": "t", "image": "asset:t.png", "tilewidth": 16, "tileheight": 32, "columns": 8, "tilecount": 64}],
		"layers": [
			{"type": "tilelayer", "name": "ground", "width": 100, "height": 100, "visible": true, "opacity": 1},
			{"type": "tilelayer", "name": "trees", "width": 100, "height": 100, "visible": true, "opacity": 1,
			 "properties": [{"name": "y_sort", "type": "bool", "value": true}]},
			{"type": "tilelayer", "name": "roofs", "width": 100, "height": 100, "visible": true, "opacity": 1}
		]
	}`)
	m, err := tilemap.LoadTiled(raw, tilemap.TilesetLoader{})
	if err != nil {
		t.Fatalf("LoadTiled: %v", err)
	}
	m.Layers[0].Data = make([]uint32, size*size)
	m.Layers[1].Data = make([]uint32, size*size)
	m.Layers[2].Data = make([]uint32, size*size)
	for row := range size {
		off := row * size
		for col := range size {
			m.Layers[0].Data[off+col] = 1
			m.Layers[1].Data[off+col] = uint32((row+col)%8 + 1)
		}
	}
	m.SetTileSortOriginY(2, -6)

	heroAtlas := &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:hero.png"},
		Frames: map[string]sprite.Frame{
			"hero": {X: 0, Y: 0, W: 16, H: 24, Width: 16, Height: 24, PivotX: 8, PivotY: 24},
		},
	}
	sprites := make([]*sprite.Sprite, 32)
	for i := range sprites {
		sp := sprite.NewSprite(heroAtlas.Frames["hero"])
		sp.X = 700 + float32(i*8)
		sp.Y = 700 + float32(i*12)
		sprites[i] = &sp
	}
	batch := &sprite.Batch{Atlas: heroAtlas, Sprites: sprites}

	cam := &tilemap.Camera{X: 800, Y: 800, Zoom: 1, Width: 640, Height: 360}
	layer := NewLayer("world", SpaceWorld, 0)
	layer.DepthSort = SortY
	layer.AddTilemap(m)
	layer.AddBatch(batch)

	// Warm up both full 100x100 (10,000 tiles + 32 sprites + ground + roofs) and camera-culled builds.
	layer.entries = layer.buildDrawEntries(layer.entries, nil, true)
	if len(layer.entries) != 100*100+32+2 {
		t.Fatalf("full buildDrawEntries len = %d, want %d", len(layer.entries), 100*100+32+2)
	}
	clear(layer.entries)
	clear(layer.ysortTiles)

	fullAllocs := testing.AllocsPerRun(20, func() {
		layer.entries = layer.buildDrawEntries(layer.entries, nil, true)
		clear(layer.entries)
		clear(layer.ysortTiles)
	})
	if fullAllocs != 0 {
		t.Fatalf("full 100x100 buildDrawEntries allocated %v times/frame, want 0", fullAllocs)
	}

	culledAllocs := testing.AllocsPerRun(50, func() {
		layer.entries = layer.buildDrawEntries(layer.entries, cam, true)
		clear(layer.entries)
		clear(layer.ysortTiles)
	})
	if culledAllocs != 0 {
		t.Fatalf("culled 100x100 buildDrawEntries allocated %v times/frame, want 0", culledAllocs)
	}
}

func BenchmarkSceneYSortBuild100x100(b *testing.B) {
	const size = 100
	ts := tilemap.NewGridTileset(1, core.ImageSource{URI: "asset:t.png"}, 16, 32, 8, 64)
	m := tilemap.New(size, size, 16, 16, ts)
	trees := make([]uint32, size*size)
	for row := range size {
		off := row * size
		for col := range size {
			trees[off+col] = uint32((row+col)%8 + 1)
		}
	}
	l := tilemap.NewTileLayer("trees", size, size, trees)
	l.YSort = true
	m.Layers = []tilemap.TileLayer{l}
	m.SetTileSortOriginY(2, -6)

	heroAtlas := &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:hero.png"},
		Frames: map[string]sprite.Frame{
			"hero": {X: 0, Y: 0, W: 16, H: 24, Width: 16, Height: 24, PivotX: 8, PivotY: 24},
		},
	}
	sprites := make([]*sprite.Sprite, 32)
	for i := range sprites {
		sp := sprite.NewSprite(heroAtlas.Frames["hero"])
		sp.X = 700 + float32(i*8)
		sp.Y = 700 + float32(i*12)
		sprites[i] = &sp
	}
	batch := &sprite.Batch{Atlas: heroAtlas, Sprites: sprites}

	layer := NewLayer("world", SpaceWorld, 0)
	layer.DepthSort = SortY
	layer.AddTilemap(m)
	layer.AddBatch(batch)

	layer.entries = layer.buildDrawEntries(layer.entries, nil, true)
	clear(layer.entries)
	clear(layer.ysortTiles)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		layer.entries = layer.buildDrawEntries(layer.entries, nil, true)
		clear(layer.entries)
		clear(layer.ysortTiles)
	}
	if cap(layer.entries) < size*size+32 {
		b.Fatal("expected preallocated entries")
	}
}
