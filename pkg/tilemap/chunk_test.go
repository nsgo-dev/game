package tilemap_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func makeChunkTestSheet() (*image.RGBA, *tilemap.Tileset, map[string]image.Image) {
	sheet := image.NewRGBA(image.Rect(0, 0, 64, 16))
	colors := []color.RGBA{
		{R: 220, G: 50, B: 50, A: 255},
		{R: 50, G: 200, B: 60, A: 255},
		{R: 60, G: 110, B: 230, A: 255},
		{R: 235, G: 200, B: 40, A: 255},
	}
	for ti, c := range colors {
		for y := range 16 {
			for x := range 16 {
				px := c
				// Give tile 0 distinct quadrants so flip verification works.
				if ti == 0 && x >= 8 && y < 8 {
					px = color.RGBA{R: 10, G: 240, B: 240, A: 255}
				}
				sheet.SetRGBA(ti*16+x, y, px)
			}
		}
	}
	imgSrc := core.ImageSource{URI: "asset:chunk-sheet.png"}
	ts := tilemap.NewGridTileset(1, imgSrc, 16, 16, 4, 4)
	return sheet, ts, map[string]image.Image{"asset:chunk-sheet.png": sheet}
}

func TestChunkedLayerNegativeCoordsFlagsRectAndRaycast(t *testing.T) {
	_, ts, _ := makeChunkTestSheet()
	m := tilemap.NewInfinite(16, 16, ts)
	m.Layers = []tilemap.TileLayer{tilemap.NewChunkedTileLayer("world", 16)}
	m.SetFlags(1, tilemap.TileSolid)
	m.SetFlags(2, tilemap.TileOneWay)

	// Setting 0 on unloaded space must not allocate chunks.
	m.SetTile(0, -100, -100, 0)
	if n := m.Layers[0].ChunkCount(); n != 0 {
		t.Fatalf("setting 0 allocated %d chunks, want 0", n)
	}

	// Place tiles across negative and positive quadrants (including flip bits).
	m.SetTile(0, -1, -1, tilemap.FlipHorizontal|1)
	m.SetTile(0, -17, -16, 1)
	m.SetTile(0, 0, 0, 2)
	m.SetTile(0, 32, -5, 1)

	if got := m.TileAt(0, -1, -1); got != tilemap.FlipHorizontal|1 {
		t.Fatalf("TileAt(-1,-1) = %#x, want %#x", got, tilemap.FlipHorizontal|1)
	}
	if got := m.TileAt(0, -17, -16); got != 1 {
		t.Fatalf("TileAt(-17,-16) = %d, want 1", got)
	}
	if got := m.TileAt(0, 0, 0); got != 2 {
		t.Fatalf("TileAt(0,0) = %d, want 2", got)
	}
	if got := m.TileAt(0, 32, -5); got != 1 {
		t.Fatalf("TileAt(32,-5) = %d, want 1", got)
	}
	if got := m.TileAt(0, -5000, 5000); got != 0 {
		t.Fatalf("unloaded TileAt(-5000,5000) = %d, want 0", got)
	}
	if n := m.Layers[0].ChunkCount(); n != 4 {
		t.Fatalf("ChunkCount = %d, want 4", n)
	}

	// Verify chunk coordinates for negative boundaries.
	coord, lx, ly := tilemap.TileToChunk(-1, -1, 16)
	if coord != (tilemap.ChunkCoord{X: -1, Y: -1}) || lx != 15 || ly != 15 {
		t.Fatalf("TileToChunk(-1,-1,16) = (%+v, %d, %d), want (-1,-1, 15, 15)", coord, lx, ly)
	}
	coord, lx, ly = tilemap.TileToChunk(-17, -16, 16)
	if coord != (tilemap.ChunkCoord{X: -2, Y: -1}) || lx != 15 || ly != 0 {
		t.Fatalf("TileToChunk(-17,-16,16) = (%+v, %d, %d), want (-2,-1, 15, 0)", coord, lx, ly)
	}

	// Flags & negative cell flag override.
	if f := m.FlagsAt(-1, -1); f != tilemap.TileSolid {
		t.Fatalf("FlagsAt(-1,-1) = %v, want TileSolid", f)
	}
	if f := m.FlagsAt(0, 0); f != tilemap.TileOneWay {
		t.Fatalf("FlagsAt(0,0) = %v, want TileOneWay", f)
	}
	m.SetCellFlags(-1, -1, 0)
	if f := m.FlagsAt(-1, -1); f != 0 {
		t.Fatalf("overridden FlagsAt(-1,-1) = %v, want 0", f)
	}
	m.ClearCellFlags(-1, -1)
	if f := m.FlagsAt(-1, -1); f != tilemap.TileSolid {
		t.Fatalf("restored FlagsAt(-1,-1) = %v, want TileSolid", f)
	}

	// TilesInRect spanning negative and positive coordinates.
	hits := m.TilesInRect(core.Bounds{
		Origin: core.Point{X: -32, Y: -32},
		Size:   core.Size{Width: 64, Height: 64},
	}, 0)
	if len(hits) != 2 || hits[0].Col != -1 || hits[0].Row != -1 || hits[0].TileID != 1 ||
		hits[1].Col != 0 || hits[1].Row != 0 || hits[1].TileID != 2 {
		t.Fatalf("TilesInRect(-32,-32,64,64) = %+v, want [(-1,-1,1), (0,0,2)]", hits)
	}
	solidHits := m.TilesInRect(core.Bounds{
		Origin: core.Point{X: -32, Y: -32},
		Size:   core.Size{Width: 64, Height: 64},
	}, tilemap.TileSolid)
	if len(solidHits) != 1 || solidHits[0].Col != -1 || solidHits[0].Row != -1 {
		t.Fatalf("solid TilesInRect = %+v, want [(-1,-1)]", solidHits)
	}

	// Raycast into negative coordinates from positive side.
	hit, ok := m.Raycast(core.Point{X: 24, Y: -8}, core.Point{X: -40, Y: -8}, tilemap.TileSolid)
	if !ok || hit.Col != -1 || hit.Row != -1 || hit.Normal != (core.Point{X: 1, Y: 0}) {
		t.Fatalf("Raycast(+X -> -X) = (%+v, %v), want (-1,-1) normal (1,0)", hit, ok)
	}
	// Raycast strictly inside negative coordinates hitting (-17, -16).
	hit2, ok2 := m.Raycast(core.Point{X: -400, Y: -248}, core.Point{X: -200, Y: -248}, tilemap.TileSolid)
	if !ok2 || hit2.Col != -17 || hit2.Row != -16 || hit2.Normal != (core.Point{X: -1, Y: 0}) {
		t.Fatalf("Raycast in negative space = (%+v, %v), want (-17,-16) normal (-1,0)", hit2, ok2)
	}
	// Raycast missing all solid tiles in sparse empty region.
	if miss, ok := m.Raycast(core.Point{X: -100, Y: 100}, core.Point{X: 100, Y: 100}, tilemap.TileSolid); ok {
		t.Fatalf("expected miss in empty space, got %+v", miss)
	}

	// FillRect across negative/positive chunk boundaries.
	m.FillRect(0, -2, -2, 4, 4, 3)
	for r := -2; r < 2; r++ {
		for c := -2; c < 2; c++ {
			if got := m.TileAt(0, c, r); got != 3 {
				t.Fatalf("FillRect TileAt(%d,%d) = %d, want 3", c, r, got)
			}
		}
	}
}

func TestPerChunkStaticCacheAndMinimalDirtyTracking(t *testing.T) {
	_, ts, images := makeChunkTestSheet()
	m := tilemap.NewInfinite(16, 16, ts)
	m.Layers = []tilemap.TileLayer{tilemap.NewChunkedTileLayer("ground", 4)}

	// Populate 4 chunks across negative and positive quadrants: (-1,-1), (0,-1), (-1,0), (0,0).
	m.SetTile(0, -1, -1, tilemap.FlipHorizontal|1)
	m.SetTile(0, 1, -1, 2)
	m.SetTile(0, -2, 1, 3)
	m.SetTile(0, 2, 2, 4)

	// CacheStaticLayer on a chunked layer returns a clear error directing to per-chunk caching.
	if _, _, err := m.CacheStaticLayer(0, images); err == nil {
		t.Fatal("expected CacheStaticLayer to return error on chunked layer")
	}

	// Bake all 4 dirty chunks.
	baked, err := m.CacheDirtyChunks(0, images)
	if err != nil {
		t.Fatalf("CacheDirtyChunks: %v", err)
	}
	if baked != 4 {
		t.Fatalf("initial CacheDirtyChunks baked = %d, want 4", baked)
	}
	if dirty := m.Layers[0].DirtyChunks(); len(dirty) != 0 {
		t.Fatalf("DirtyChunks after bake = %v, want empty", dirty)
	}

	// Drawing all 4 cached chunks emits strictly 4 DrawImage ops (1 per chunk).
	cam := &tilemap.Camera{X: 0, Y: 0, Zoom: 1, Width: 256, Height: 256}
	var d core.Drawing
	m.Draw(&d, cam)
	if len(d.Ops) != 4 {
		t.Fatalf("cached chunked draw emitted %d ops, want 4 (1 per chunk)", len(d.Ops))
	}
	for i, op := range d.Ops {
		if op.Kind != core.DrawImage || op.Image == nil || op.Image.Version != 1 {
			t.Fatalf("op[%d] = %+v, want DrawImage v1", i, op)
		}
	}

	// Edit a single tile in chunk (-1, -1): ONLY chunk (-1, -1) must become dirty!
	m.SetTile(0, -2, -2, 2)
	dirty := m.Layers[0].DirtyChunks()
	wantDirty := []tilemap.ChunkCoord{{X: -1, Y: -1}}
	if len(dirty) != 1 || dirty[0] != wantDirty[0] {
		t.Fatalf("DirtyChunks after single edit = %v, want %v", dirty, wantDirty)
	}
	if c := m.ChunkCache(0, tilemap.ChunkCoord{X: 0, Y: 0}); c == nil || c.Dirty || c.Version != 1 {
		t.Fatalf("unrelated chunk (0,0) cache = %+v, want clean v1", c)
	}

	// CacheDirtyChunks rebakes strictly the 1 dirty chunk, bumping only its Version to 2.
	rebaked, err := m.CacheDirtyChunks(0, images)
	if err != nil {
		t.Fatalf("second CacheDirtyChunks: %v", err)
	}
	if rebaked != 1 {
		t.Fatalf("rebaked = %d, want 1", rebaked)
	}
	if c := m.ChunkCache(0, tilemap.ChunkCoord{X: -1, Y: -1}); c == nil || c.Dirty || c.Version != 2 {
		t.Fatalf("edited chunk (-1,-1) cache = %+v, want clean v2", c)
	}
	if c := m.ChunkCache(0, tilemap.ChunkCoord{X: 0, Y: 0}); c == nil || c.Dirty || c.Version != 1 {
		t.Fatalf("untouched chunk (0,0) cache = %+v, want clean v1", c)
	}
	// Verify flipped pixel baked into chunk (-1,-1): tile (-1,-1) is FlipHorizontal|1;
	// original top-right quadrant of tile 0 is cyan (10,240,240), so flipped top-left pixel is cyan.
	cNeg := m.ChunkCache(0, tilemap.ChunkCoord{X: -1, Y: -1})
	// Chunk (-1,-1) has tiles at (-2,-2) and (-1,-1), so tight pixel bounds start at (-32,-32),
	// making (-1,-1) start at local pixel (16, 16).
	gotPx := cNeg.Pixels.RGBAAt(17, 17)
	wantCyan := color.RGBA{R: 10, G: 240, B: 240, A: 255}
	if gotPx != wantCyan {
		t.Fatalf("chunk (-1,-1) baked FlipH pixel = %v, want %v", gotPx, wantCyan)
	}

	// Camera culling only draws visible chunks (only chunk (0,0) overlaps [16,48)x[16,48)).
	d.Ops = d.Ops[:0]
	smallCam := &tilemap.Camera{X: 32, Y: 32, Zoom: 1, Width: 32, Height: 32}
	m.Draw(&d, smallCam)
	if len(d.Ops) != 1 {
		t.Fatalf("culled cached draw emitted %d ops, want 1", len(d.Ops))
	}
}

func TestChunkPersistenceRoundTripByteForByte(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format tilemap.ChunkFormat
		useDir bool
	}{
		{"yaml_memory", tilemap.ChunkFormatYAML, false},
		{"json_memory", tilemap.ChunkFormatJSON, false},
		{"yaml_dir", tilemap.ChunkFormatYAML, true},
		{"json_dir", tilemap.ChunkFormatJSON, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ts, _ := makeChunkTestSheet()
			m := tilemap.NewInfinite(16, 16, ts)
			m.Layers = []tilemap.TileLayer{tilemap.NewChunkedTileLayer("world", 16)}
			m.SetFlags(1, tilemap.TileSolid)
			m.SetFlags(2, tilemap.TileOneWay)

			var store tilemap.ChunkStore
			var dirStore *tilemap.DirChunkStore
			var memStore *tilemap.MemoryChunkStore
			if tc.useDir {
				dirStore = tilemap.NewDirChunkStore(t.TempDir(), tc.format)
				store = dirStore
			} else {
				memStore = tilemap.NewMemoryChunkStore()
				store = memStore
			}

			genCalls := 0
			stream := tilemap.NewChunkStream(tilemap.ChunkStreamConfig{
				Map:          m,
				LayerIndex:   0,
				LoadRadius:   1,
				UnloadRadius: 2,
				Format:       tc.format,
				Store:        store,
				Generator: func(coord tilemap.ChunkCoord, ch *tilemap.Chunk) error {
					genCalls++
					// Deterministic procedural terrain.
					for ly := range ch.Size {
						for lx := range ch.Size {
							if (coord.X+coord.Y+lx+ly)&3 == 0 {
								ch.Tiles[ly*ch.Size+lx] = 1
							}
						}
					}
					return nil
				},
			})

			// Load around negative chunk (-3, -2).
			originPos := core.Point{X: core.Points(-3*16*16 + 32), Y: core.Points(-2*16*16 + 32)}
			stream.Update(originPos)

			targetCoord := tilemap.ChunkCoord{X: -3, Y: -2}
			chBefore := m.ChunkAt(0, targetCoord)
			if chBefore == nil {
				t.Fatalf("chunk %+v not loaded", targetCoord)
			}
			if chBefore.Modified {
				t.Fatal("freshly generated chunk should have Modified=false")
			}

			// Edit several tiles in (-3, -2), including flip flags and clearing a tile.
			baseCol, baseRow := targetCoord.X*16, targetCoord.Y*16
			m.SetTile(0, baseCol+0, baseRow+0, tilemap.FlipHorizontal|1)
			m.SetTile(0, baseCol+5, baseRow+7, tilemap.FlipVertical|tilemap.FlipDiagonal|2)
			m.SetTile(0, baseCol+15, baseRow+15, 3)
			m.SetTile(0, baseCol+4, baseRow+0, 0)

			if !chBefore.Modified {
				t.Fatal("edited chunk should have Modified=true")
			}

			beforeBytes, err := tilemap.EncodeChunk(chBefore, tc.format)
			if err != nil {
				t.Fatalf("EncodeChunk before unload: %v", err)
			}
			beforeTiles := append([]uint32(nil), chBefore.Tiles...)

			// Walk far away so (-3, -2) unloads and is saved to Store.
			farPos := core.Point{X: 100 * 16 * 16, Y: 100 * 16 * 16}
			stream.Update(farPos)
			if m.ChunkAt(0, targetCoord) != nil {
				t.Fatalf("chunk %+v should have unloaded", targetCoord)
			}

			// Verify only modified chunks were persisted to Store (not untouched generated chunks).
			if memStore != nil && memStore.Count() != 1 {
				t.Fatalf("MemoryChunkStore count = %d, want 1 (only modified chunk saved)", memStore.Count())
			}
			if dirStore != nil {
				diskBytes, err := os.ReadFile(dirStore.Path(targetCoord))
				if err != nil {
					t.Fatalf("read saved chunk file: %v", err)
				}
				if !bytes.Equal(diskBytes, beforeBytes) {
					t.Fatalf("saved file bytes differ from EncodeChunk output")
				}
			}

			// Walk back to (-3, -2): must reload from Store without calling Generator for targetCoord.
			genBeforeReturn := genCalls
			stream.Update(originPos)
			chAfter := m.ChunkAt(0, targetCoord)
			if chAfter == nil {
				t.Fatalf("chunk %+v did not reload", targetCoord)
			}
			if chAfter.Modified {
				t.Fatal("reloaded chunk should start with Modified=false")
			}
			// 9 chunks in LoadRadius=1 around (-3,-2): 8 untouched regenerated + 1 reloaded from Store.
			if genCalls-genBeforeReturn != 8 {
				t.Fatalf("generator calls on return = %d, want 8 (target chunk loaded from store)", genCalls-genBeforeReturn)
			}

			// Verify every tile and serialized bytes match byte-for-byte!
			for i := range beforeTiles {
				if chAfter.Tiles[i] != beforeTiles[i] {
					t.Fatalf("tile[%d] = %#x, want %#x", i, chAfter.Tiles[i], beforeTiles[i])
				}
			}
			afterBytes, err := tilemap.EncodeChunk(chAfter, tc.format)
			if err != nil {
				t.Fatalf("EncodeChunk after reload: %v", err)
			}
			if !bytes.Equal(beforeBytes, afterBytes) {
				t.Fatalf("round-trip chunk text mismatch:\nbefore:\n%s\nafter:\n%s", beforeBytes, afterBytes)
			}

			// Verify query APIs reflect restored flags & tiles.
			if f := m.FlagsAt(baseCol+0, baseRow+0); f != tilemap.TileSolid {
				t.Fatalf("restored FlagsAt = %v, want TileSolid", f)
			}
			if got := m.TileAt(0, baseCol+5, baseRow+7); got != tilemap.FlipVertical|tilemap.FlipDiagonal|2 {
				t.Fatalf("restored flipped GID = %#x, want %#x", got, tilemap.FlipVertical|tilemap.FlipDiagonal|2)
			}

			if err := stream.Close(); err != nil {
				t.Fatalf("stream.Close: %v", err)
			}
			if m.Layers[0].ChunkCount() != 0 {
				t.Fatalf("chunks remaining after Close = %d, want 0", m.Layers[0].ChunkCount())
			}
		})
	}
}

func TestChunkDocValidationErrors(t *testing.T) {
	badYAML := []struct {
		name string
		data string
	}{
		{"empty", "   \n"},
		{"unknown field", "version: 1\nx: 0\ny: 0\nsize: 2\nruns: [\"0*4\"]\nextra: true\n"},
		{"wrong version", "version: 2\nx: 0\ny: 0\nsize: 2\nruns: [\"0*4\"]\n"},
		{"non-pow2 size", "version: 1\nx: 0\ny: 0\nsize: 3\nruns: [\"0*9\"]\n"},
		{"short runs", "version: 1\nx: 0\ny: 0\nsize: 2\nruns: [\"0*3\"]\n"},
		{"long runs", "version: 1\nx: 0\ny: 0\nsize: 2\nruns: [\"0*5\"]\n"},
		{"bad token", "version: 1\nx: 0\ny: 0\nsize: 2\nruns: [\"abc\"]\n"},
		{"zero count", "version: 1\nx: 0\ny: 0\nsize: 2\nruns: [\"1*0\", \"0*4\"]\n"},
		{"trailing doc", "version: 1\nx: 0\ny: 0\nsize: 2\nruns: [\"0*4\"]\n---\nversion: 1\n"},
	}
	for _, tc := range badYAML {
		if _, err := tilemap.DecodeChunkYAML([]byte(tc.data)); err == nil {
			t.Errorf("DecodeChunkYAML(%s): expected error, got nil", tc.name)
		}
	}

	badJSON := []struct {
		name string
		data string
	}{
		{"empty", " "},
		{"unknown field", `{"version":1,"x":0,"y":0,"size":2,"runs":["0*4"],"extra":1}`},
		{"wrong version", `{"version":99,"x":0,"y":0,"size":2,"runs":["0*4"]}`},
		{"trailing data", `{"version":1,"x":0,"y":0,"size":2,"runs":["0*4"]} {}`},
	}
	for _, tc := range badJSON {
		if _, err := tilemap.DecodeChunkJSON([]byte(tc.data)); err == nil {
			t.Errorf("DecodeChunkJSON(%s): expected error, got nil", tc.name)
		}
	}
}

func TestWalker10000TilesFourDirectionsBoundedMemory(t *testing.T) {
	_, ts, _ := makeChunkTestSheet()
	m := tilemap.NewInfinite(16, 16, ts)
	m.Layers = []tilemap.TileLayer{tilemap.NewChunkedTileLayer("world", 16)}
	m.SetFlags(1, tilemap.TileSolid)

	store := tilemap.NewMemoryChunkStore()
	const loadR, unloadR = 2, 3
	maxAllowedChunks := (2*unloadR + 1) * (2*unloadR + 1)

	stream := tilemap.NewChunkStream(tilemap.ChunkStreamConfig{
		Map:          m,
		LayerIndex:   0,
		LoadRadius:   loadR,
		UnloadRadius: unloadR,
		Format:       tilemap.ChunkFormatYAML,
		Store:        store,
		Generator: func(coord tilemap.ChunkCoord, ch *tilemap.Chunk) error {
			for ly := range ch.Size {
				rowOff := ly * ch.Size
				for lx := range ch.Size {
					if (coord.X*31+coord.Y*17+lx+ly)&7 == 0 {
						ch.Tiles[rowOff+lx] = 1
					} else {
						ch.Tiles[rowOff+lx] = 2
					}
				}
			}
			return nil
		},
	})
	defer func() { _ = stream.Close() }()

	cam := &tilemap.Camera{Zoom: 1, Width: 320, Height: 180}
	d := core.Drawing{Ops: make([]core.DrawOp, 0, 512)}

	maxObserved := 0
	walk := func(dxTiles, dyTiles, steps int) {
		for s := range steps {
			wx := float32(dxTiles * s * 16)
			wy := float32(dyTiles * s * 16)
			stream.Update(core.Point{X: core.Points(wx), Y: core.Points(wy)})
			cam.X, cam.Y = wx, wy
			d.Ops = d.Ops[:0]
			m.Draw(&d, cam)
			if len(d.Ops) == 0 {
				t.Fatalf("expected non-empty draw ops at (%g,%g)", wx, wy)
			}
			if n := m.Layers[0].ChunkCount(); n > maxObserved {
				maxObserved = n
			}
			if n := m.Layers[0].ChunkCount(); n > maxAllowedChunks {
				t.Fatalf("loaded chunks %d exceeded bound %d at (%g,%g)", n, maxAllowedChunks, wx, wy)
			}
		}
	}

	// Cross 10,000 tiles in +X, -X, +Y, -Y (stepping 4 tiles per frame = 2,500 frames per direction).
	walk(4, 0, 2501)
	walk(-4, 0, 2501)
	walk(0, 4, 2501)
	walk(0, -4, 2501)

	if maxObserved == 0 || maxObserved > maxAllowedChunks {
		t.Fatalf("maxObserved chunks = %d, want in (0, %d]", maxObserved, maxAllowedChunks)
	}
}

func TestLoadTiledInfiniteSparseWidelySeparatedChunks(t *testing.T) {
	// Two 16x16 chunks separated by 20,000 tiles (-10000,-10000 and +10000,+10000).
	// A flattened dense array would require 20016*20016*4 > 1.6 GB; sparse chunked loading allocates only 2 chunks!
	chunkData := make([]uint32, 16*16)
	chunkData[0] = 1
	chunkData[16*16-1] = tilemap.FlipHorizontal | 2

	var dataBuf bytes.Buffer
	dataBuf.WriteByte('[')
	for i, v := range chunkData {
		if i > 0 {
			dataBuf.WriteByte(',')
		}
		fmt.Fprintf(&dataBuf, "%d", v)
	}
	dataBuf.WriteByte(']')

	raw := fmt.Sprintf(`{
		"width": 0, "height": 0, "tilewidth": 16, "tileheight": 16,
		"orientation": "orthogonal",
		"infinite": true,
		"tilesets": [{
			"firstgid": 1, "name": "t", "image": "tiles.png",
			"tilewidth": 16, "tileheight": 16, "columns": 2, "tilecount": 4,
			"tiles": [{"id": 0, "properties": [{"name": "solid", "type": "bool", "value": true}]}]
		}],
		"layers": [{
			"name": "world", "type": "tilelayer", "visible": true, "opacity": 1,
			"chunks": [
				{"x": -10000, "y": -10000, "width": 16, "height": 16, "data": %s},
				{"x": 10000, "y": 10000, "width": 16, "height": 16, "data": %s}
			]
		}]
	}`, dataBuf.String(), dataBuf.String())

	m, err := tilemap.LoadTiled([]byte(raw), tilemap.TilesetLoader{})
	if err != nil {
		t.Fatalf("LoadTiled sparse infinite: %v", err)
	}
	layer := m.Layer("world")
	if layer == nil || !layer.IsChunked() {
		t.Fatalf("expected sparse chunked layer, got %+v", layer)
	}
	if layer.EffectiveChunkSize() != 16 {
		t.Fatalf("EffectiveChunkSize = %d, want 16", layer.EffectiveChunkSize())
	}
	if got := layer.ChunkCount(); got != 2 {
		t.Fatalf("ChunkCount = %d, want 2 (sparse, not flattened)", got)
	}

	// Query negative and positive distant coordinates directly.
	if got := m.TileAt(0, -10000, -10000); got != 1 {
		t.Fatalf("TileAt(-10000,-10000) = %d, want 1", got)
	}
	if got := m.TileAt(0, -10000+15, -10000+15); got != tilemap.FlipHorizontal|2 {
		t.Fatalf("TileAt(-9985,-9985) = %#x, want %#x", got, tilemap.FlipHorizontal|2)
	}
	if got := m.TileAt(0, 10000, 10000); got != 1 {
		t.Fatalf("TileAt(10000,10000) = %d, want 1", got)
	}
	if got := m.TileAt(0, 0, 0); got != 0 {
		t.Fatalf("TileAt(0,0) = %d, want 0", got)
	}
	if f := m.FlagsAt(-10000, -10000); f != tilemap.TileSolid {
		t.Fatalf("FlagsAt(-10000,-10000) = %v, want TileSolid", f)
	}

	// TilesInRect & Raycast at negative distant chunk.
	hits := m.TilesInRect(core.Bounds{
		Origin: core.Point{X: -10000 * 16, Y: -10000 * 16},
		Size:   core.Size{Width: 16, Height: 16},
	}, tilemap.TileSolid)
	if len(hits) != 1 || hits[0].Col != -10000 || hits[0].Row != -10000 {
		t.Fatalf("TilesInRect at -10000 = %+v", hits)
	}
	hit, ok := m.Raycast(
		core.Point{X: -10002*16 + 8, Y: -10000*16 + 8},
		core.Point{X: -9999*16 + 8, Y: -10000*16 + 8},
		tilemap.TileSolid,
	)
	if !ok || hit.Col != -10000 || hit.Row != -10000 {
		t.Fatalf("Raycast at -10000 = (%+v, %v)", hit, ok)
	}

	// Culled draw centered on (-10000, -10000) draws only the 2 tiles in that chunk.
	cam := &tilemap.Camera{X: -10000*16 + 128, Y: -10000*16 + 128, Zoom: 1, Width: 300, Height: 300}
	var d core.Drawing
	m.Draw(&d, cam)
	drawCount := 0
	for _, op := range d.Ops {
		if op.Kind == core.DrawImage {
			drawCount++
		}
	}
	if drawCount != 2 {
		t.Fatalf("culled draw at (-10000,-10000) emitted %d DrawImage ops, want 2", drawCount)
	}
}

func TestChunkedLayerYSortProjectedAndAutotile(t *testing.T) {
	_, ts, _ := makeChunkTestSheet()
	m := tilemap.NewInfinite(16, 16, ts)
	layer := tilemap.NewChunkedTileLayer("trees", 8)
	layer.YSort = true
	m.Layers = []tilemap.TileLayer{layer}

	m.SetTile(0, -3, -2, 1)
	m.SetTile(0, 2, 1, 2)

	tiles := m.AppendYSortTiles(nil, &m.Layers[0], nil)
	if len(tiles) != 2 {
		t.Fatalf("AppendYSortTiles returned %d tiles, want 2", len(tiles))
	}
	if tiles[0].Col != -3 || tiles[0].Row != -2 || tiles[1].Col != 2 || tiles[1].Row != 1 {
		t.Fatalf("AppendYSortTiles order = %+v", tiles)
	}

	// Isometric chunked layer draws cleanly with negative coordinates.
	iso := tilemap.NewInfinite(32, 16, ts)
	iso.Orientation = tilemap.OrientationIsometric
	iso.Layers = []tilemap.TileLayer{tilemap.NewChunkedTileLayer("iso", 8)}
	iso.SetTile(0, -2, -1, 1)
	iso.SetTile(0, 1, 1, 2)
	var d core.Drawing
	iso.Draw(&d, nil)
	if len(d.Ops) != 2 {
		t.Fatalf("isometric chunked draw ops = %d, want 2", len(d.Ops))
	}

	// SetAutotile and ApplyAutotile across negative coordinates.
	auto := tilemap.NewInfinite(16, 16, ts)
	auto.Layers = []tilemap.TileLayer{tilemap.NewChunkedTileLayer("terrain", 4)}
	set := tilemap.AutotileSet{
		Mode: tilemap.Autotile4,
		Tiles: map[uint8]uint32{
			0:                 1,
			tilemap.MaskEast:  2,
			tilemap.MaskWest:  3,
			tilemap.MaskSouth: 4,
		},
		Default: 1,
	}
	auto.SetAutotile(0, -1, -1, true, set, nil)
	auto.SetAutotile(0, 0, -1, true, set, nil)
	if got := auto.TileAt(0, -1, -1); got != 2 {
		t.Fatalf("autotile(-1,-1) = %d, want 2 (MaskEast)", got)
	}
	if got := auto.TileAt(0, 0, -1); got != 3 {
		t.Fatalf("autotile(0,-1) = %d, want 3 (MaskWest)", got)
	}

	// Verify DirChunkStore filename convention for negative coordinates.
	dir := t.TempDir()
	ds := tilemap.NewDirChunkStore(dir, tilemap.ChunkFormatYAML)
	if got, want := ds.Path(tilemap.ChunkCoord{X: -4, Y: 7}), filepath.Join(dir, "chunk_-4_7.yaml"); got != want {
		t.Fatalf("DirChunkStore.Path = %q, want %q", got, want)
	}
}

func TestChunkWalkerBenchHelperSanity(t *testing.T) {
	// Ensure math import and WorldBounds on chunked maps work as expected.
	_, ts, _ := makeChunkTestSheet()
	m := tilemap.NewInfinite(16, 16, ts)
	m.Layers = []tilemap.TileLayer{tilemap.NewChunkedTileLayer("g", 4)}
	m.SetTile(0, -1, -1, 1)
	wb := m.WorldBounds()
	if math.Abs(float64(wb.Origin.X)-(-64)) > 1e-4 || math.Abs(float64(wb.Size.Width)-64) > 1e-4 {
		t.Fatalf("chunked WorldBounds = %+v, want origin (-64,-64) size 64x64", wb)
	}
}
