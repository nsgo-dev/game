package scene_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func drawnImageURIs(ops []core.DrawOp) []string {
	var uris []string
	for _, op := range ops {
		if op.Image != nil && op.Image.URI != "" {
			uris = append(uris, op.Image.URI)
		}
	}
	return uris
}

func countNSGoImageOpAllocs(ops []core.DrawOp) int {
	// NSGo v0.3.0 allocates 1 heap object (&img) per DrawImage op and 3 heap objects
	// (AtlasSprites slice, &sprites, &img) per DrawAtlas op.
	total := 0
	for _, op := range ops {
		switch op.Kind {
		case core.DrawImage:
			total += 1
		case core.DrawAtlas:
			total += 3
		}
	}
	return total
}

func TestSceneYSortCharacterAroundTreeOrthogonal(t *testing.T) {
	// Ground tileset (16x16), tree tileset (16x48 tall tree), roof tileset (16x16).
	groundTS := tilemap.NewGridTileset(1, core.ImageSource{URI: "asset:ground.png"}, 16, 16, 1, 1)
	treeTS := tilemap.NewGridTileset(2, core.ImageSource{URI: "asset:tree.png"}, 16, 48, 1, 1)
	roofTS := tilemap.NewGridTileset(3, core.ImageSource{URI: "asset:roof.png"}, 16, 16, 1, 1)

	m := tilemap.New(6, 6, 16, 16, groundTS, treeTS, roofTS)
	ground := tilemap.NewTileLayer("ground", 6, 6, nil)
	trees := tilemap.NewTileLayer("trees", 6, 6, nil)
	trees.YSort = true
	roofs := tilemap.NewTileLayer("roofs", 6, 6, nil)
	m.Layers = []tilemap.TileLayer{ground, trees, roofs}

	// One ground tile, one tall tree at (col=3, row=4) (bottom edge = 5*16 = 80), one roof tile.
	m.SetTile(0, 3, 4, 1)
	m.SetTile(1, 3, 4, 2)
	m.SetTile(2, 3, 4, 3)

	heroAtlas := &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:hero.png"},
		Frames: map[string]sprite.Frame{
			"hero": {X: 0, Y: 0, W: 16, H: 24, Width: 16, Height: 24, PivotX: 8, PivotY: 24},
		},
	}
	hero := sprite.NewSprite(heroAtlas.Frames["hero"]) // OriginY=24 => hero.Y is character's feet
	hero.X = 56
	actors := &sprite.Batch{Atlas: heroAtlas, Sprites: []*sprite.Sprite{&hero}}

	cam := &tilemap.Camera{X: 56, Y: 64, Width: 160, Height: 160, Zoom: 1}
	sc := scene.NewScene(cam)
	world := sc.AddLayer(scene.NewLayer("world", scene.SpaceWorld, 0))
	world.DepthSort = scene.SortY
	world.AddTilemap(m)
	world.AddBatch(actors)

	// 1. Default bottom-edge sort line = 80.
	// Character above bottom edge (Y = 76) draws BEHIND tree, AFTER ground, BEFORE roof.
	hero.Y = 76
	var d core.Drawing
	sc.Draw(&d)
	wantBehind := []string{"asset:ground.png", "asset:hero.png", "asset:tree.png", "asset:roof.png"}
	if got := drawnImageURIs(d.Ops); !reflect.DeepEqual(got, wantBehind) {
		t.Fatalf("ortho above bottom edge: got %v, want %v", got, wantBehind)
	}

	// Character walks below bottom edge (Y = 84) -> draws IN FRONT of tree, STILL behind roof.
	hero.Y = 84
	d.Ops = d.Ops[:0]
	sc.Draw(&d)
	wantFront := []string{"asset:ground.png", "asset:tree.png", "asset:hero.png", "asset:roof.png"}
	if got := drawnImageURIs(d.Ops); !reflect.DeepEqual(got, wantFront) {
		t.Fatalf("ortho below bottom edge: got %v, want %v", got, wantFront)
	}

	// 2. Per-tile sort_origin_y = -8 moves the trunk sort line up from 80 to 72.
	m.SetTileSortOriginY(2, -8)

	// Character at Y = 70 (above trunk line 72) -> BEHIND tree.
	hero.Y = 70
	d.Ops = d.Ops[:0]
	sc.Draw(&d)
	if got := drawnImageURIs(d.Ops); !reflect.DeepEqual(got, wantBehind) {
		t.Fatalf("ortho above trunk line (70 < 72): got %v, want %v", got, wantBehind)
	}

	// Character at Y = 76 (below trunk line 72, even though above bottom edge 80) -> IN FRONT of tree!
	hero.Y = 76
	d.Ops = d.Ops[:0]
	sc.Draw(&d)
	if got := drawnImageURIs(d.Ops); !reflect.DeepEqual(got, wantFront) {
		t.Fatalf("ortho below trunk line (76 > 72): got %v, want %v", got, wantFront)
	}
}

func TestSceneYSortCharacterAroundTreeIsometric(t *testing.T) {
	// Isometric 64x32 diamonds with a tall 64x96 tree tile and neighbor tiles.
	groundTS := tilemap.NewGridTileset(1, core.ImageSource{URI: "asset:iso-ground.png"}, 64, 32, 1, 1)
	backTreeTS := tilemap.NewGridTileset(2, core.ImageSource{URI: "asset:iso-back-tree.png"}, 64, 96, 1, 1)
	mainTreeTS := tilemap.NewGridTileset(3, core.ImageSource{URI: "asset:iso-main-tree.png"}, 64, 96, 1, 1)
	frontTreeTS := tilemap.NewGridTileset(4, core.ImageSource{URI: "asset:iso-front-tree.png"}, 64, 96, 1, 1)

	m := tilemap.New(5, 5, 64, 32, groundTS, backTreeTS, mainTreeTS, frontTreeTS)
	m.Orientation = tilemap.OrientationIsometric

	ground := tilemap.NewTileLayer("ground", 5, 5, nil)
	trees := tilemap.NewTileLayer("trees", 5, 5, nil)
	trees.YSort = true
	m.Layers = []tilemap.TileLayer{ground, trees}

	// Ground under (2,2), plus three trees along the isometric depth axis:
	// (1,1): ty = 2*16 = 32, bottom = 64
	// (2,2): ty = 4*16 = 64, bottom = 96
	// (3,3): ty = 6*16 = 96, bottom = 128
	m.SetTile(0, 2, 2, 1)
	m.SetTile(1, 1, 1, 2)
	m.SetTile(1, 2, 2, 3)
	m.SetTile(1, 3, 3, 4)

	heroAtlas := &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:hero.png"},
		Frames: map[string]sprite.Frame{
			"hero": {X: 0, Y: 0, W: 16, H: 32, Width: 16, Height: 32, PivotX: 8, PivotY: 32},
		},
	}
	hero := sprite.NewSprite(heroAtlas.Frames["hero"])
	treeCenter := m.TileToWorld(2, 2) // (32, 80)
	hero.X = float32(treeCenter.X)
	actors := &sprite.Batch{Atlas: heroAtlas, Sprites: []*sprite.Sprite{&hero}}

	cam := &tilemap.Camera{X: float32(treeCenter.X), Y: float32(treeCenter.Y), Width: 320, Height: 240, Zoom: 1}
	sc := scene.NewScene(cam)
	world := sc.AddLayer(scene.NewLayer("world", scene.SpaceWorld, 0))
	world.DepthSort = scene.SortY
	world.AddTilemap(m)
	world.AddBatch(actors)

	// 1. Default bottom-edge sort line for main tree (2,2) is 96 (back=64, front=128).
	// Character walking behind main tree (Y = 84: between back=64 and main=96):
	hero.Y = 84
	var d core.Drawing
	sc.Draw(&d)
	wantBehindMain := []string{
		"asset:iso-ground.png",
		"asset:iso-back-tree.png",
		"asset:hero.png",
		"asset:iso-main-tree.png",
		"asset:iso-front-tree.png",
	}
	if got := drawnImageURIs(d.Ops); !reflect.DeepEqual(got, wantBehindMain) {
		t.Fatalf("iso behind main tree (Y=84 < 96): got %v, want %v", got, wantBehindMain)
	}

	// Character walks in front of main tree (Y = 104: between main=96 and front=128):
	hero.Y = 104
	d.Ops = d.Ops[:0]
	sc.Draw(&d)
	wantFrontMain := []string{
		"asset:iso-ground.png",
		"asset:iso-back-tree.png",
		"asset:iso-main-tree.png",
		"asset:hero.png",
		"asset:iso-front-tree.png",
	}
	if got := drawnImageURIs(d.Ops); !reflect.DeepEqual(got, wantFrontMain) {
		t.Fatalf("iso in front of main tree (Y=104 > 96): got %v, want %v", got, wantFrontMain)
	}

	// 2. Trunk sort origin at diamond center: sort_origin_y = -16 => main tree sort line = 96 - 16 = 80.
	m.SetTileSortOriginY(3, -16)

	// Above trunk line (Y = 76 < 80) -> behind main tree.
	hero.Y = 76
	d.Ops = d.Ops[:0]
	sc.Draw(&d)
	if got := drawnImageURIs(d.Ops); !reflect.DeepEqual(got, wantBehindMain) {
		t.Fatalf("iso above trunk line (76 < 80): got %v, want %v", got, wantBehindMain)
	}

	// Below trunk line (Y = 84 > 80) -> in front of main tree!
	hero.Y = 84
	d.Ops = d.Ops[:0]
	sc.Draw(&d)
	if got := drawnImageURIs(d.Ops); !reflect.DeepEqual(got, wantFrontMain) {
		t.Fatalf("iso below trunk line (84 > 80): got %v, want %v", got, wantFrontMain)
	}
}

func TestSceneYSortCullingAnimatedTieBreakAndAddTileLayer(t *testing.T) {
	ts := tilemap.NewGridTileset(1, core.ImageSource{URI: "asset:torch.png"}, 16, 16, 4, 4)
	ts.Animations = map[uint32]*tilemap.TileAnimation{
		0: {
			Frames: []tilemap.TileAnimFrame{
				{TileID: 0, Duration: 100 * time.Millisecond},
				{TileID: 1, Duration: 100 * time.Millisecond},
			},
		},
	}

	m := tilemap.New(10, 10, 16, 16, ts)
	bg := tilemap.NewTileLayer("bg", 10, 10, nil)
	props := tilemap.NewTileLayer("props", 10, 10, nil)
	props.YSort = true
	m.Layers = []tilemap.TileLayer{bg, props}

	m.SetTile(0, 1, 1, 1) // animated bg tile
	m.SetTile(1, 1, 1, 1) // animated Y-sorted torch at row 1 (SortY = 32)
	m.SetTile(1, 9, 9, 1) // offscreen Y-sorted torch (culled)

	heroAtlas := &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:hero.png"},
		Frames: map[string]sprite.Frame{
			"hero": {X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16, PivotX: 8, PivotY: 16},
		},
	}
	hero := sprite.NewSprite(heroAtlas.Frames["hero"])
	hero.X, hero.Y = 24, 32 // exact tie with torch SortY=32!
	actors := &sprite.Batch{Atlas: heroAtlas, Sprites: []*sprite.Sprite{&hero}}

	// Put bg and props onto the same Y-sorted scene layer via AddTileLayer so we test both
	// single-layer binding and non-duplicate Tilemap.Update(dt) advancement.
	cam := &tilemap.Camera{X: 32, Y: 32, Width: 80, Height: 80, Zoom: 1}
	sc := scene.NewScene(cam)
	world := sc.AddLayer(scene.NewLayer("world", scene.SpaceWorld, 0))
	world.DepthSort = scene.SortY
	world.AddTileLayer(m, &m.Layers[0]) // bg (non-YSort)
	world.AddTileLayer(m, &m.Layers[1]) // props (YSort) added BEFORE actors
	world.AddBatch(actors)

	// Advance by 60ms: if Tilemap.Update ran twice (120ms), animation would prematurely flip to frame 1!
	sc.Update(60 * time.Millisecond)
	if got := ts.Animations[0].Index(); got != 0 {
		t.Fatalf("after 60ms animation index = %d, want 0 (Tilemap.Update must not double-step)", got)
	}
	// Advance another 60ms (total 120ms) -> frame 1 (SrcX = 16).
	sc.Update(60 * time.Millisecond)
	if got := ts.Animations[0].Index(); got != 1 {
		t.Fatalf("after 120ms animation index = %d, want 1", got)
	}

	var d core.Drawing
	sc.Draw(&d)

	// Expected: bg(1,1), props(1,1) [offscreen (9,9) culled!], hero (tie at Y=32 preserves insertion order: props before actors).
	wantURIs := []string{"asset:torch.png", "asset:torch.png", "asset:hero.png"}
	if got := drawnImageURIs(d.Ops); !reflect.DeepEqual(got, wantURIs) {
		t.Fatalf("got drawn URIs %v, want %v", got, wantURIs)
	}
	// Verify animated tile used frame 1 (SrcX = 16) via exact expected draw ops.
	var want core.Drawing
	want.Save()
	cam.Apply(&want)
	want.DrawImageFrom(ts.Image, 16, 0, 16, 16, 16, 16, 16, 16)       // bg(1,1)
	want.DrawImageFrom(ts.Image, 16, 0, 16, 16, 16, 16, 16, 16)       // props(1,1)
	want.DrawImageFrom(heroAtlas.Image, 0, 0, 16, 16, 16, 16, 16, 16) // hero at (24,32) with pivot (8,16) -> (16,16)
	want.Restore()
	sameOps(t, d.Ops, want.Ops)
}

func buildSceneYSort100x100(useTiledOrder bool) (*scene.Scene, *tilemap.Camera) {
	const size = 100
	var m *tilemap.Tilemap
	if useTiledOrder {
		// Minimal valid Tiled map with layerOrder populated to verify VisitLayers is zero-alloc too.
		raw := []byte(`{
			"width": 100, "height": 100, "tilewidth": 16, "tileheight": 16,
			"orientation": "orthogonal",
			"tilesets": [{"firstgid": 1, "name": "t", "image": "asset:t.png", "tilewidth": 16, "tileheight": 32, "columns": 8, "tilecount": 64}],
			"layers": [
				{"type": "tilelayer", "name": "ground", "width": 100, "height": 100, "visible": true, "opacity": 1},
				{"type": "tilelayer", "name": "trees", "width": 100, "height": 100, "visible": true, "opacity": 1,
				 "properties": [{"name": "y_sort", "type": "bool", "value": true}]}
			]
		}`)
		var err error
		m, err = tilemap.LoadTiled(raw, tilemap.TilesetLoader{})
		if err != nil {
			panic(err)
		}
		m.Layers[0].Data = make([]uint32, size*size)
		m.Layers[1].Data = make([]uint32, size*size)
		for row := range size {
			off := row * size
			for col := range size {
				m.Layers[0].Data[off+col] = 1
				m.Layers[1].Data[off+col] = uint32((row+col)%8 + 1)
			}
		}
	} else {
		ts := tilemap.NewGridTileset(1, core.ImageSource{URI: "asset:t.png"}, 16, 32, 8, 64)
		m = tilemap.New(size, size, 16, 16, ts)
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
	sc := scene.NewScene(cam)
	world := sc.AddLayer(scene.NewLayer("world", scene.SpaceWorld, 0))
	world.DepthSort = scene.SortY
	world.AddTilemap(m)
	world.AddBatch(batch)
	return sc, cam
}

func TestSceneYSortZeroAllocs100x100(t *testing.T) {
	for _, tc := range []struct {
		name  string
		tiled bool
		full  bool
	}{
		{name: "full-100x100-code", tiled: false, full: true},
		{name: "culled-100x100-code", tiled: false, full: false},
		{name: "culled-100x100-tiled", tiled: true, full: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc, _ := buildSceneYSort100x100(tc.tiled)
			if tc.full {
				sc.Camera = nil
			}
			d := core.Drawing{Ops: make([]core.DrawOp, 0, 12000)}
			// Warmup frame to size scratch buffers.
			sc.Draw(&d)
			if len(d.Ops) == 0 {
				t.Fatal("expected draw ops")
			}

			allocs := testing.AllocsPerRun(20, func() {
				d.Ops = d.Ops[:0]
				sc.Draw(&d)
			})
			wantMax := float64(countNSGoImageOpAllocs(d.Ops))
			if allocs > wantMax {
				t.Fatalf("Scene.Draw (%s) allocated %v times/frame, want <= %v (NSGo DrawImage/DrawAtlas ops only; 0 in scene/tilemap)", tc.name, allocs, wantMax)
			}
		})
	}
}

func BenchmarkSceneYSort100x100(b *testing.B) {
	sc, _ := buildSceneYSort100x100(false)
	sc.Camera = nil // full 100x100 layer (10,000 tiles + 32 sprites)
	d := core.Drawing{Ops: make([]core.DrawOp, 0, 12000)}
	sc.Draw(&d)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		d.Ops = d.Ops[:0]
		sc.Draw(&d)
	}
	if len(d.Ops) == 0 {
		b.Fatal("expected draw ops")
	}
}

func BenchmarkSceneYSort100x100Culled(b *testing.B) {
	sc, _ := buildSceneYSort100x100(true)
	d := core.Drawing{Ops: make([]core.DrawOp, 0, 4096)}
	sc.Draw(&d)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		d.Ops = d.Ops[:0]
		sc.Draw(&d)
	}
	if len(d.Ops) == 0 {
		b.Fatal("expected draw ops")
	}
}
