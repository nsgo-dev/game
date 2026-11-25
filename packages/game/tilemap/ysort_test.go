package tilemap_test

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func TestYSortOrthogonalDefaultAndSortOriginY(t *testing.T) {
	ts := tilemap.NewGridTileset(1, core.ImageSource{URI: "asset:tiles.png"}, 16, 32, 4, 16)
	m := tilemap.New(4, 4, 16, 16, ts)
	layer := tilemap.NewTileLayer("trees", 4, 4, nil)
	layer.YSort = true
	m.Layers = append(m.Layers, layer)

	// Place tile GID 1 at (1, 1) and tile GID 2 at (1, 2).
	m.SetTile(0, 1, 1, 1) // default bottom edge = (1+1)*16 = 32
	m.SetTile(0, 1, 2, 2) // default bottom edge = (2+1)*16 = 48

	if !m.HasYSortLayers() {
		t.Fatal("expected HasYSortLayers() == true")
	}

	got := m.AppendYSortTiles(nil, &m.Layers[0], nil)
	if len(got) != 2 {
		t.Fatalf("got %d tiles, want 2", len(got))
	}
	if got[0].Col != 1 || got[0].Row != 1 || got[0].SortY != 32 {
		t.Fatalf("tile 0 = %+v, want col=1 row=1 SortY=32", got[0])
	}
	if got[1].Col != 1 || got[1].Row != 2 || got[1].SortY != 48 {
		t.Fatalf("tile 1 = %+v, want col=1 row=2 SortY=48", got[1])
	}

	// Shift tile 2's sort line upward by -24 (48 - 24 = 24 < 32), so it draws BEFORE tile 1!
	m.SetTileSortOriginY(2, -24)
	if off := m.TileSortOriginY(2); off != -24 {
		t.Fatalf("TileSortOriginY(2) = %v, want -24", off)
	}
	// Check alias "y_sort_origin" on tile 1 (+4 => 32 + 4 = 36).
	ts.TileProperties[0] = tilemap.Properties{
		"y_sort_origin": {Name: "y_sort_origin", Type: "float", Value: 4.0},
	}
	if off := m.TileSortOriginY(1); off != 4 {
		t.Fatalf("TileSortOriginY(1) = %v, want 4", off)
	}

	got = m.AppendYSortTiles(got[:0], &m.Layers[0], nil)
	if got[0].SortY != 36 || got[1].SortY != 24 {
		t.Fatalf("SortY after overrides = (%v, %v), want (36, 24)", got[0].SortY, got[1].SortY)
	}

	// DrawLayer on a YSort layer must sort by SortY (tile 2 before tile 1).
	var d, want core.Drawing
	m.DrawLayer(&d, &m.Layers[0], nil)
	// Tile 2 (localID 1, sx=16) is at row=2 (dstY = 3*16 - 32 = 16); tile 1 (localID 0, sx=0) is at row=1 (dstY = 2*16 - 32 = 0).
	want.DrawImageFrom(ts.Image, 16, 0, 16, 32, 16, 16, 16, 32)
	want.DrawImageFrom(ts.Image, 0, 0, 16, 32, 16, 0, 16, 32)
	gametest.AssertSameImages(t, d.Ops, want.Ops)

	// Even if a static cache exists on the layer, YSort bypasses the cache so tiles draw in SortY order.
	srcImg := image.NewRGBA(image.Rect(0, 0, 64, 32))
	srcImg.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	ts.Image.Pixels = srcImg
	if _, _, err := m.CacheStaticLayer(0, nil); err != nil {
		t.Fatalf("CacheStaticLayer: %v", err)
	}
	d.Ops = d.Ops[:0]
	want.Ops = want.Ops[:0]
	want.DrawImageFrom(ts.Image, 16, 0, 16, 32, 16, 16, 16, 32)
	want.DrawImageFrom(ts.Image, 0, 0, 16, 32, 16, 0, 16, 32)
	m.DrawLayer(&d, &m.Layers[0], nil)
	gametest.AssertSameImages(t, d.Ops, want.Ops)

	// Disabling YSort restores normal cached single-op drawing.
	m.SetLayerYSort(0, false)
	if m.HasYSortLayers() {
		t.Fatal("expected HasYSortLayers() == false after SetLayerYSort(0, false)")
	}
	d.Ops = d.Ops[:0]
	m.DrawLayer(&d, &m.Layers[0], nil)
	if len(d.Ops) != 1 {
		t.Fatalf("non-YSort cached DrawLayer emitted %d ops, want 1", len(d.Ops))
	}
}

func TestYSortIsometricStaggeredAndHexagonal(t *testing.T) {
	// 1. Isometric: 64x32 diamond tiles, tall 64x96 tree tiles.
	isoTS := tilemap.NewGridTileset(1, core.ImageSource{URI: "asset:iso.png"}, 64, 96, 4, 8)
	iso := tilemap.New(4, 4, 64, 32, isoTS)
	iso.Orientation = tilemap.OrientationIsometric
	layer := tilemap.NewTileLayer("trees", 4, 4, nil)
	layer.YSort = true
	iso.Layers = append(iso.Layers, layer)

	// (col=1, row=2) -> s=3, ty = 3*16 = 48, bottom = 80
	// (col=2, row=1) -> s=3, ty = 3*16 = 48, bottom = 80
	// (col=2, row=2) -> s=4, ty = 4*16 = 64, bottom = 96
	iso.SetTile(0, 1, 2, 1)
	iso.SetTile(0, 2, 1, 1)
	iso.SetTile(0, 2, 2, 2)
	iso.SetTileSortOriginY(2, -16) // shifts (2,2) sort line to diamond center: 96 - 16 = 80

	got := iso.AppendYSortTiles(nil, &iso.Layers[0], nil)
	if len(got) != 3 {
		t.Fatalf("iso got %d tiles, want 3", len(got))
	}
	// Painter's order visits s=3 (col=1,row=2), s=3 (col=2,row=1), then s=4 (col=2,row=2).
	if got[0].Col != 1 || got[0].Row != 2 || got[0].SortY != 80 {
		t.Fatalf("iso[0] = %+v, want (1,2) SortY=80", got[0])
	}
	if got[1].Col != 2 || got[1].Row != 1 || got[1].SortY != 80 {
		t.Fatalf("iso[1] = %+v, want (2,1) SortY=80", got[1])
	}
	if got[2].Col != 2 || got[2].Row != 2 || got[2].SortY != 80 {
		t.Fatalf("iso[2] = %+v, want (2,2) SortY=80", got[2])
	}

	// Camera culling on isometric map: viewport only covers (2,2)'s canopy/body around x=[0..64], y=[0..96].
	cam := &tilemap.Camera{X: 32, Y: 48, Width: 20, Height: 20, Zoom: 1}
	culled := iso.AppendYSortTiles(nil, &iso.Layers[0], cam)
	found22 := false
	for _, ct := range culled {
		if ct.Col == 2 && ct.Row == 2 {
			found22 = true
		}
	}
	if !found22 {
		t.Fatalf("expected tall isometric tree at (2,2) to survive camera culling; got %+v", culled)
	}

	// 2. Staggered-X: non-staggered columns have smaller Y than staggered columns on the same row.
	stag := tilemap.New(3, 2, 32, 16, tilemap.NewGridTileset(1, core.ImageSource{URI: "asset:stag.png"}, 32, 16, 4, 4))
	stag.Orientation = tilemap.OrientationStaggered
	stag.StaggerAxis = tilemap.StaggerX
	stag.StaggerIndex = tilemap.StaggerOdd
	sl := tilemap.NewTileLayer("props", 3, 2, nil)
	sl.YSort = true
	stag.Layers = append(stag.Layers, sl)
	stag.SetTile(0, 0, 0, 1) // col 0 (even, non-staggered): ty=0, SortY=16
	stag.SetTile(0, 1, 0, 1) // col 1 (odd, staggered): ty=8, SortY=24
	sgot := stag.AppendYSortTiles(nil, &stag.Layers[0], nil)
	if len(sgot) != 2 || sgot[0].SortY != 16 || sgot[1].SortY != 24 {
		t.Fatalf("staggered-X SortY = %+v, want 16 then 24", sgot)
	}

	// 3. Hexagonal: verifies SortY matches TileOrigin.Y + TileHeight.
	hex := tilemap.New(3, 3, 32, 32, tilemap.NewGridTileset(1, core.ImageSource{URI: "asset:hex.png"}, 32, 32, 4, 4))
	hex.Orientation = tilemap.OrientationHexagonal
	hex.HexSideLength = 16
	hl := tilemap.NewTileLayer("hex", 3, 3, nil)
	hl.YSort = true
	hex.Layers = append(hex.Layers, hl)
	hex.SetTile(0, 1, 1, 1)
	hgot := hex.AppendYSortTiles(nil, &hex.Layers[0], nil)
	wantY := float32(hex.TileOrigin(1, 1).Y) + hex.TileHeight
	if len(hgot) != 1 || hgot[0].SortY != wantY {
		t.Fatalf("hex SortY = %+v, want %v", hgot, wantY)
	}
}

func TestYSortCameraCullingTallAtlasTiles(t *testing.T) {
	// Atlas where frame 0 is 16x16 grass and frame 1 is a 16x48 tall tree.
	// NewAtlasTileset sets ts.TileHeight = 16 from frame 0, so culling MUST inspect ts.Frames for tall trees!
	atlas := &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:atlas.png"},
		Frames: map[string]sprite.Frame{
			"grass": {X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			"tree":  {X: 16, Y: 0, W: 16, H: 48, Width: 16, Height: 48},
		},
	}
	ts := tilemap.NewAtlasTileset(1, atlas, "grass", "tree")
	m := tilemap.New(10, 10, 16, 16, ts)
	l := tilemap.NewTileLayer("props", 10, 10, nil)
	l.YSort = true
	m.Layers = append(m.Layers, l)

	// Place a tall tree at (col=2, row=5): base cell Y is [80..96], canopy extends UP to Y=48!
	// Place a normal 16x16 grass tile at (col=3, row=5): Y is [80..96].
	// Place another grass tile completely offscreen at (col=9, row=9).
	m.SetTile(0, 2, 5, 2) // tree
	m.SetTile(0, 3, 5, 1) // grass on same row, completely below camera!
	m.SetTile(0, 9, 9, 1) // far offscreen

	// Camera visible bounds: X=[0..100], Y=[0..60].
	// Tree at (2,5) spans Y=[48..96], which intersects [0..60]!
	// Grass at (3,5) spans Y=[80..96], which does NOT intersect [0..60]!
	cam := &tilemap.Camera{X: 50, Y: 30, Width: 100, Height: 60, Zoom: 1}
	got := m.AppendYSortTiles(nil, &m.Layers[0], cam)
	if len(got) != 1 || got[0].Col != 2 || got[0].Row != 5 || got[0].GID != 2 {
		t.Fatalf("tall atlas culling got %+v, want only tall tree at (2,5)", got)
	}
	if got[0].SortY != 96 {
		t.Fatalf("tall tree SortY = %v, want 96", got[0].SortY)
	}
}

func TestYSortAnimatedTilesFlipsAndOpacity(t *testing.T) {
	ts := tilemap.NewGridTileset(1, core.ImageSource{URI: "asset:anim.png"}, 16, 16, 4, 8)
	ts.Animations = map[uint32]*tilemap.TileAnimation{
		0: {
			Frames: []tilemap.TileAnimFrame{
				{TileID: 0, Duration: 100 * time.Millisecond},
				{TileID: 2, Duration: 100 * time.Millisecond},
			},
		},
	}
	m := tilemap.New(2, 2, 16, 16, ts)
	l := tilemap.NewTileLayer("anim", 2, 2, nil)
	l.YSort = true
	l.Opacity = 0.5
	m.Layers = append(m.Layers, l)

	// GID 1 (localID 0) with FlipHorizontal, plus sort_origin_y = -4 on base tile.
	m.SetTile(0, 0, 0, 1|tilemap.FlipHorizontal)
	m.SetTileSortOriginY(1, -4)

	// Frame 0: sx = 0, FlipHorizontal at (0,0) -> Transform(-1, 0, 0, 1, 16, 0), Alpha=0.5.
	tiles := m.AppendYSortTiles(nil, &m.Layers[0], nil)
	if len(tiles) != 1 || tiles[0].SortY != 12 {
		t.Fatalf("frame 0 tiles = %+v, want 1 tile with SortY=12", tiles)
	}
	var d0, want0 core.Drawing
	tiles[0].Draw(&d0)
	want0.Save().SetAlpha(0.5).Transform(-1, 0, 0, 1, 16, 0).DrawImageFrom(ts.Image, 0, 0, 16, 16, 0, 0, 16, 16).Restore()
	sameOps(t, d0.Ops, want0.Ops)

	// Advance animation to frame 1 (TileID 2 -> sx = 32), SortY stays 12.
	m.Update(120 * time.Millisecond)
	tiles = m.AppendYSortTiles(tiles[:0], &m.Layers[0], nil)
	if len(tiles) != 1 || tiles[0].SortY != 12 {
		t.Fatalf("frame 1 tiles = %+v, want 1 tile with SortY=12", tiles)
	}
	var d1, want1 core.Drawing
	tiles[0].Draw(&d1)
	want1.Save().SetAlpha(0.5).Transform(-1, 0, 0, 1, 16, 0).DrawImageFrom(ts.Image, 32, 0, 16, 16, 0, 0, 16, 16).Restore()
	sameOps(t, d1.Ops, want1.Ops)
}

func TestYSortTiledProperties(t *testing.T) {
	raw := []byte(`{
		"width": 2,
		"height": 2,
		"tilewidth": 16,
		"tileheight": 16,
		"orientation": "orthogonal",
		"tilesets": [{
			"firstgid": 1,
			"name": "trees",
			"image": "trees.png",
			"tilewidth": 16,
			"tileheight": 32,
			"columns": 2,
			"tilecount": 2,
			"tiles": [
				{
					"id": 0,
					"properties": [
						{"name": "sort_origin_y", "type": "float", "value": -6}
					]
				},
				{
					"id": 1,
					"properties": [
						{"name": "ysort_origin", "type": "int", "value": -10}
					]
				}
			]
		}],
		"layers": [
			{
				"type": "tilelayer",
				"name": "ground",
				"width": 2,
				"height": 2,
				"visible": true,
				"opacity": 1,
				"data": [1, 0, 0, 0]
			},
			{
				"type": "tilelayer",
				"name": "trees",
				"width": 2,
				"height": 2,
				"visible": true,
				"opacity": 1,
				"properties": [
					{"name": "y_sort", "type": "bool", "value": true}
				],
				"data": [0, 1, 2, 0]
			}
		]
	}`)

	m, err := tilemap.LoadTiled(raw, tilemap.TilesetLoader{})
	if err != nil {
		t.Fatalf("LoadTiled: %v", err)
	}
	if m.Layer("ground").YSort {
		t.Fatal("ground should not have YSort enabled")
	}
	trees := m.Layer("trees")
	if trees == nil || !trees.YSort {
		t.Fatal("trees layer should have YSort enabled from Tiled property")
	}
	if !m.HasYSortLayers() {
		t.Fatal("HasYSortLayers() should be true")
	}
	if got := m.TileSortOriginY(1); got != -6 {
		t.Fatalf("TileSortOriginY(1) = %v, want -6", got)
	}
	if got := m.TileSortOriginY(2); got != -10 {
		t.Fatalf("TileSortOriginY(2) = %v, want -10", got)
	}

	tiles := m.AppendYSortTiles(nil, trees, nil)
	if len(tiles) != 2 {
		t.Fatalf("got %d trees, want 2", len(tiles))
	}
	// (1,0) GID 1: bottom=16, offset=-6 -> 10
	// (0,1) GID 2: bottom=32, offset=-10 -> 22
	if tiles[0].SortY != 10 || tiles[1].SortY != 22 {
		t.Fatalf("SortY = (%v, %v), want (10, 22)", tiles[0].SortY, tiles[1].SortY)
	}
}

func buildYSortMap100x100(orient tilemap.Orientation) *tilemap.Tilemap {
	const size = 100
	tw, th := float32(16), float32(16)
	tileImgH := float32(32)
	if orient == tilemap.OrientationIsometric {
		tw, th = 64, 32
		tileImgH = 64
	}
	ts := tilemap.NewGridTileset(1, core.ImageSource{URI: "asset:ysort-100.png"}, tw, tileImgH, 8, 64)
	m := tilemap.New(size, size, tw, th, ts)
	m.Orientation = orient

	data := make([]uint32, size*size)
	for row := range size {
		off := row * size
		for col := range size {
			data[off+col] = uint32((row+col)%8 + 1)
		}
	}
	layer := tilemap.NewTileLayer("trees", size, size, data)
	layer.YSort = true
	m.Layers = []tilemap.TileLayer{layer}
	m.SetTileSortOriginY(3, -4)
	return m
}

func TestYSortZeroAllocs100x100(t *testing.T) {
	m := buildYSortMap100x100(tilemap.OrientationOrthogonal)
	layer := &m.Layers[0]
	cam := &tilemap.Camera{X: 800, Y: 800, Zoom: 1, Width: 640, Height: 360}
	d := core.Drawing{Ops: make([]core.DrawOp, 0, 64)}

	// 1. Collect + sort for a full 100x100 layer (10,000 tiles) and camera-culled view: strictly 0 allocs/frame.
	buf := make([]tilemap.YSortTile, 0, 100*100)
	buf = m.AppendYSortTiles(buf[:0], layer, nil)
	tilemap.SortYSortTiles(buf)
	if len(buf) != 100*100 {
		t.Fatalf("full 100x100 collected %d tiles, want 10000", len(buf))
	}
	fullSortAllocs := testing.AllocsPerRun(20, func() {
		buf = m.AppendYSortTiles(buf[:0], layer, nil)
		tilemap.SortYSortTiles(buf)
	})
	if fullSortAllocs != 0 {
		t.Fatalf("full 100x100 AppendYSortTiles+SortYSortTiles allocated %v times/frame, want 0", fullSortAllocs)
	}

	culledSortAllocs := testing.AllocsPerRun(50, func() {
		buf = m.AppendYSortTiles(buf[:0], layer, cam)
		tilemap.SortYSortTiles(buf)
	})
	if culledSortAllocs != 0 {
		t.Fatalf("culled 100x100 AppendYSortTiles+SortYSortTiles allocated %v times/frame, want 0", culledSortAllocs)
	}

	iso := buildYSortMap100x100(tilemap.OrientationIsometric)
	isoCam := &tilemap.Camera{X: 0, Y: 1600, Zoom: 1, Width: 640, Height: 360}
	isoBuf := make([]tilemap.YSortTile, 0, 1024)
	isoBuf = iso.AppendYSortTiles(isoBuf[:0], &iso.Layers[0], isoCam)
	tilemap.SortYSortTiles(isoBuf)
	isoSortAllocs := testing.AllocsPerRun(50, func() {
		isoBuf = iso.AppendYSortTiles(isoBuf[:0], &iso.Layers[0], isoCam)
		tilemap.SortYSortTiles(isoBuf)
	})
	if isoSortAllocs != 0 {
		t.Fatalf("isometric 100x100 AppendYSortTiles+SortYSortTiles allocated %v times/frame, want 0", isoSortAllocs)
	}

	// 2. Full DrawLayer / Draw emits a single DrawAtlas op for the run; NSGo v0.3.0's Drawing.DrawAtlas
	// itself allocates 3 objects per op (the copied AtlasSprites slice, &sprites, and &img). tilemap must add 0.
	m.DrawLayer(&d, layer, nil)
	if len(d.Ops) != 1 || d.Ops[0].Kind != core.DrawAtlas || d.Ops[0].Atlas == nil || len(*d.Ops[0].Atlas) != 100*100 {
		t.Fatalf("full 100x100 Y-sorted DrawLayer ops = %d (kind=%v), want 1 DrawAtlas with 10000 tiles", len(d.Ops), d.Ops[0].Kind)
	}
	fullDrawAllocs := testing.AllocsPerRun(20, func() {
		d.Ops = d.Ops[:0]
		m.DrawLayer(&d, layer, nil)
	})
	if fullDrawAllocs > 3*float64(len(d.Ops)) {
		t.Fatalf("full 100x100 Y-sorted DrawLayer allocated %v times for %d DrawAtlas ops (want <= %d)", fullDrawAllocs, len(d.Ops), 3*len(d.Ops))
	}

	d.Ops = d.Ops[:0]
	m.Draw(&d, cam)
	culledDrawAllocs := testing.AllocsPerRun(50, func() {
		d.Ops = d.Ops[:0]
		m.Draw(&d, cam)
	})
	if culledDrawAllocs > 3*float64(len(d.Ops)) {
		t.Fatalf("culled 100x100 Y-sorted Draw allocated %v times for %d DrawAtlas ops (want <= %d)", culledDrawAllocs, len(d.Ops), 3*len(d.Ops))
	}
}

func BenchmarkYSortCollectAndSort100x100(b *testing.B) {
	m := buildYSortMap100x100(tilemap.OrientationOrthogonal)
	layer := &m.Layers[0]
	buf := make([]tilemap.YSortTile, 0, 100*100)
	buf = m.AppendYSortTiles(buf[:0], layer, nil)
	tilemap.SortYSortTiles(buf)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		buf = m.AppendYSortTiles(buf[:0], layer, nil)
		tilemap.SortYSortTiles(buf)
	}
	if len(buf) != 100*100 {
		b.Fatalf("got %d tiles, want 10000", len(buf))
	}
}

func BenchmarkYSortLayer100x100(b *testing.B) {
	m := buildYSortMap100x100(tilemap.OrientationOrthogonal)
	layer := &m.Layers[0]
	d := core.Drawing{Ops: make([]core.DrawOp, 0, 64)}
	m.DrawLayer(&d, layer, nil)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		d.Ops = d.Ops[:0]
		m.DrawLayer(&d, layer, nil)
	}
	if len(d.Ops) != 1 {
		b.Fatalf("got %d ops, want 1 DrawAtlas op", len(d.Ops))
	}
}

func BenchmarkYSortLayer100x100Culled(b *testing.B) {
	m := buildYSortMap100x100(tilemap.OrientationOrthogonal)
	cam := &tilemap.Camera{X: 800, Y: 800, Zoom: 1, Width: 640, Height: 360}
	d := core.Drawing{Ops: make([]core.DrawOp, 0, 64)}
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

func BenchmarkYSortLayer100x100Isometric(b *testing.B) {
	m := buildYSortMap100x100(tilemap.OrientationIsometric)
	cam := &tilemap.Camera{X: 0, Y: 1600, Zoom: 1, Width: 640, Height: 360}
	d := core.Drawing{Ops: make([]core.DrawOp, 0, 64)}
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
