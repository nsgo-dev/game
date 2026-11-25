package tilemap_test

import (
	"errors"
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// worldPhysicsGrid adapts *tilemap.WorldStream to physics.TileGrid + physics.TileGridOrigin.
type worldPhysicsGrid struct {
	stream *tilemap.WorldStream
}

func (g worldPhysicsGrid) TileSize() (float32, float32)   { return g.stream.TileSize() }
func (g worldPhysicsGrid) GridSize() (int, int)           { return g.stream.GridSize() }
func (g worldPhysicsGrid) GridOrigin() (float32, float32) { return g.stream.GridOrigin() }
func (g worldPhysicsGrid) TileFlagsAt(col, row int) physics.TileFlags {
	tf := g.stream.GridFlagsAt(col, row)
	var out physics.TileFlags
	if tf&tilemap.TileSolid != 0 {
		out |= physics.TileSolid
	}
	if tf&tilemap.TileOneWay != 0 {
		out |= physics.TileOneWay
	}
	return out
}

// mapPhysicsGrid adapts a single *tilemap.Tilemap (with Origin) to physics.TileGrid + physics.TileGridOrigin.
type mapPhysicsGrid struct {
	m *tilemap.Tilemap
}

func (g mapPhysicsGrid) TileSize() (float32, float32) { return g.m.TileWidth, g.m.TileHeight }
func (g mapPhysicsGrid) GridSize() (int, int)         { return g.m.Width, g.m.Height }
func (g mapPhysicsGrid) GridOrigin() (float32, float32) {
	return float32(g.m.Origin.X), float32(g.m.Origin.Y)
}
func (g mapPhysicsGrid) TileFlagsAt(col, row int) physics.TileFlags {
	tf := g.m.FlagsAt(col, row)
	var out physics.TileFlags
	if tf&tilemap.TileSolid != 0 {
		out |= physics.TileSolid
	}
	if tf&tilemap.TileOneWay != 0 {
		out |= physics.TileOneWay
	}
	return out
}

func TestTilemapOriginDrawQueryAndProjections(t *testing.T) {
	pxImg := image.NewRGBA(image.Rect(0, 0, 32, 16))
	for y := range 16 {
		for x := range 32 {
			pxImg.SetRGBA(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	img := core.ImageSource{URI: "asset:tiles.png", Pixels: pxImg}
	ts := tilemap.NewGridTileset(1, img, 16, 16, 2, 2)

	m := tilemap.New(3, 2, 16, 16, ts)
	m.Origin = core.Point{X: 100, Y: -32}
	m.Layers = []tilemap.TileLayer{
		tilemap.NewTileLayer("ground", 3, 2, []uint32{
			1, 0, 2,
			0, 1, 0,
		}),
	}
	m.SetFlags(1, tilemap.TileSolid)
	m.SetFlags(2, tilemap.TileOneWay)

	// WorldBounds honors Origin.
	wantBounds := core.Bounds{
		Origin: core.Point{X: 100, Y: -32},
		Size:   core.Size{Width: 48, Height: 32},
	}
	if got := m.WorldBounds(); got != wantBounds {
		t.Fatalf("WorldBounds = %+v, want %+v", got, wantBounds)
	}

	// TileOrigin, TileToWorld, WorldToTile round-trip with Origin.
	if got := m.TileOrigin(2, 1); got != (core.Point{X: 132, Y: -16}) {
		t.Fatalf("TileOrigin(2,1) = %+v, want (132,-16)", got)
	}
	center := m.TileToWorld(2, 1)
	if center != (core.Point{X: 140, Y: -8}) {
		t.Fatalf("TileToWorld(2,1) = %+v, want (140,-8)", center)
	}
	if col, row := m.WorldToTile(center); col != 2 || row != 1 {
		t.Fatalf("WorldToTile(%+v) = (%d,%d), want (2,1)", center, col, row)
	}

	// TilesInRect honors Origin for both query rectangle and returned hit bounds.
	hits := m.TilesInRect(core.Bounds{
		Origin: core.Point{X: 100, Y: -32},
		Size:   core.Size{Width: 48, Height: 32},
	}, tilemap.TileSolid)
	if len(hits) != 2 {
		t.Fatalf("TilesInRect(solid) = %d hits, want 2: %+v", len(hits), hits)
	}
	if hits[0].Col != 0 || hits[0].Row != 0 || hits[0].Bounds != (core.Bounds{Origin: core.Point{X: 100, Y: -32}, Size: core.Size{Width: 16, Height: 16}}) {
		t.Fatalf("hits[0] = %+v", hits[0])
	}
	if hits[1].Col != 1 || hits[1].Row != 1 || hits[1].Bounds != (core.Bounds{Origin: core.Point{X: 116, Y: -16}, Size: core.Size{Width: 16, Height: 16}}) {
		t.Fatalf("hits[1] = %+v", hits[1])
	}
	// Query at local (0,0) must miss because map is at (100, -32).
	if miss := m.TilesInRect(core.Bounds{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: 48, Height: 32}}, tilemap.TileSolid); len(miss) != 0 {
		t.Fatalf("TilesInRect at (0,0) = %+v, want empty", miss)
	}

	// Raycast honors Origin for input points and returned hit.Point.
	hit, ok := m.Raycast(core.Point{X: 108, Y: -8}, core.Point{X: 140, Y: -8}, tilemap.TileSolid)
	if !ok || hit.Col != 1 || hit.Row != 1 || hit.Point != (core.Point{X: 116, Y: -8}) || hit.Normal != (core.Point{X: -1, Y: 0}) {
		t.Fatalf("Raycast = (%+v, %v), want col=1 row=1 point=(116,-8) normal=(-1,0)", hit, ok)
	}
	// Zero-length raycast inside solid cell at world origin.
	hit0, ok := m.Raycast(core.Point{X: 104, Y: -28}, core.Point{X: 104, Y: -28}, tilemap.TileSolid)
	if !ok || hit0.Col != 0 || hit0.Row != 0 || hit0.Point != (core.Point{X: 104, Y: -28}) {
		t.Fatalf("zero-length Raycast = (%+v, %v)", hit0, ok)
	}

	// AppendYSortTiles honors Origin for dst position and SortY.
	tiles := m.AppendYSortTiles(nil, &m.Layers[0], nil)
	if len(tiles) != 3 {
		t.Fatalf("AppendYSortTiles = %d, want 3", len(tiles))
	}
	// Tile (0,0) at Origin (100, -32): bottom edge SortY = -32 + 16 = -16.
	if tiles[0].SortY != -16 {
		t.Fatalf("tiles[0].SortY = %v, want -16", tiles[0].SortY)
	}
	// Tile (1,1) at (116, -16): bottom edge SortY = -16 + 16 = 0.
	if tiles[2].SortY != 0 {
		t.Fatalf("tiles[2].SortY = %v, want 0", tiles[2].SortY)
	}

	// Draw (live) and DrawLayer (cached) both draw at Origin + local tile positions, with camera culling.
	cam := &tilemap.Camera{X: 124, Y: -16, Width: 48, Height: 32}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "live", Style: core.Style{Width: core.Pt(48), Height: core.Pt(32)}, Draw: func(d *core.Drawing, _ core.Size) {
				m.Draw(d, cam)
			}},
			core.Canvas{TestID: "want", Style: core.Style{Width: core.Pt(48), Height: core.Pt(32)}, Draw: func(d *core.Drawing, _ core.Size) {
				d.DrawImageFrom(img, 0, 0, 16, 16, 100, -32, 16, 16)
				d.DrawImageFrom(img, 16, 0, 16, 16, 132, -32, 16, 16)
				d.DrawImageFrom(img, 0, 0, 16, 16, 116, -16, 16, 16)
			}},
		}}
	})
	sameOps(t, app.Find("live").Ops, app.Find("want").Ops)

	// Static cache also honors Origin when drawn (even if Origin changes after baking).
	if _, _, err := m.CacheStaticLayer(0, nil); err != nil {
		t.Fatalf("CacheStaticLayer: %v", err)
	}
	cache := m.LayerCache(0)
	appCached := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "cached", Style: core.Style{Width: core.Pt(48), Height: core.Pt(32)}, Draw: func(d *core.Drawing, _ core.Size) {
				m.Draw(d, cam)
			}},
			core.Canvas{TestID: "want", Style: core.Style{Width: core.Pt(48), Height: core.Pt(32)}, Draw: func(d *core.Drawing, _ core.Size) {
				d.DrawImageFrom(cache.Source, 0, 0, 48, 32, 100, -32, 48, 32)
			}},
		}}
	})
	sameOps(t, appCached.Find("cached").Ops, appCached.Find("want").Ops)

	// Projections (isometric, staggered, hexagonal) all round-trip with non-zero Origin.
	for _, orient := range []tilemap.Orientation{
		tilemap.OrientationIsometric,
		tilemap.OrientationStaggered,
		tilemap.OrientationHexagonal,
	} {
		pm := tilemap.New(4, 4, 32, 16)
		pm.Orientation = orient
		pm.Origin = core.Point{X: -40, Y: 75}
		for row := range 4 {
			for col := range 4 {
				pt := pm.TileToWorld(col, row)
				gc, gr := pm.WorldToTile(pt)
				if gc != col || gr != row {
					t.Fatalf("orient %d: TileToWorld(%d,%d)=%+v -> WorldToTile=(%d,%d)", orient, col, row, pt, gc, gr)
				}
			}
		}
		wb := pm.WorldBounds()
		pm0 := *pm
		pm0.Origin = core.Point{}
		wb0 := pm0.WorldBounds()
		if wb.Origin.X != wb0.Origin.X+pm.Origin.X || wb.Origin.Y != wb0.Origin.Y+pm.Origin.Y || wb.Size != wb0.Size {
			t.Fatalf("orient %d: WorldBounds=%+v, want shifted %+v by %+v", orient, wb, wb0, pm.Origin)
		}
	}

	// Single offset Tilemap works with physics.MoveAndSlide via TileGridOrigin.
	body := physics.NewBoxBody(102, -48, 12, 12)
	body.VY = 200
	res := physics.MoveAndSlide(body, mapPhysicsGrid{m: m}, 200*time.Millisecond)
	if !res.Grounded || body.Bounds().Y != -44 {
		t.Fatalf("offset MoveAndSlide = (y=%v grounded=%v), want y=-44 grounded=true", body.Bounds().Y, res.Grounded)
	}
}

func makeAdjacentLDtkProject(layout string, level0X, level0Y, level1X, level1Y int) []byte {
	return []byte(`{
		"defaultGridSize": 16,
		"worldLayout": "` + layout + `",
		"defs": {
			"tilesets": [
				{
					"uid": 1,
					"identifier": "Tiles",
					"relPath": "tiles.png",
					"pxWid": 32,
					"pxHei": 16,
					"tileGridSize": 16,
					"__cWid": 2,
					"__cHei": 1
				}
			],
			"layers": [
				{
					"uid": 10,
					"identifier": "Solid",
					"type": "IntGrid",
					"gridSize": 16,
					"intGridValues": [
						{"value": 1, "identifier": "solid", "color": "#FFFFFF"}
					]
				}
			]
		},
		"levels": [
			{
				"identifier": "Room_West",
				"iid": "iid-west",
				"uid": 100,
				"worldX": ` + itoa(level0X) + `,
				"worldY": ` + itoa(level0Y) + `,
				"worldDepth": 0,
				"pxWid": 64,
				"pxHei": 48,
				"__neighbours": [
					{"levelIid": "iid-east", "dir": "e"}
				],
				"layerInstances": [
					{
						"__identifier": "Visual",
						"__type": "Tiles",
						"__cWid": 4,
						"__cHei": 3,
						"__gridSize": 16,
						"__opacity": 1.0,
						"__tilesetDefUid": 1,
						"visible": true,
						"gridTiles": [
							{"px": [0, 32], "src": [0, 0], "f": 0, "t": 0},
							{"px": [48, 32], "src": [0, 0], "f": 0, "t": 0}
						]
					},
					{
						"__identifier": "Solid",
						"__type": "IntGrid",
						"__cWid": 4,
						"__cHei": 3,
						"__gridSize": 16,
						"__opacity": 1.0,
						"layerDefUid": 10,
						"visible": true,
						"intGridCsv": [
							1, 0, 0, 0,
							1, 0, 0, 0,
							1, 1, 1, 1
						]
					}
				]
			},
			{
				"identifier": "Room_East",
				"iid": "iid-east",
				"uid": 200,
				"worldX": ` + itoa(level1X) + `,
				"worldY": ` + itoa(level1Y) + `,
				"worldDepth": 0,
				"pxWid": 64,
				"pxHei": 48,
				"__neighbours": [
					{"levelUid": 100, "dir": "w"}
				],
				"layerInstances": [
					{
						"__identifier": "Visual",
						"__type": "Tiles",
						"__cWid": 4,
						"__cHei": 3,
						"__gridSize": 16,
						"__opacity": 1.0,
						"__tilesetDefUid": 1,
						"visible": true,
						"gridTiles": [
							{"px": [0, 32], "src": [16, 0], "f": 0, "t": 1},
							{"px": [48, 32], "src": [16, 0], "f": 0, "t": 1}
						]
					},
					{
						"__identifier": "Solid",
						"__type": "IntGrid",
						"__cWid": 4,
						"__cHei": 3,
						"__gridSize": 16,
						"__opacity": 1.0,
						"layerDefUid": 10,
						"visible": true,
						"intGridCsv": [
							0, 0, 0, 1,
							0, 0, 0, 1,
							1, 1, 1, 1
						]
					}
				]
			}
		]
	}`)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func TestLDtkAdjacentLevelsDrawSeamlesslyAndWalkAcrossSeam(t *testing.T) {
	img := core.ImageSource{URI: "asset:tiles.png"}
	resolve := func(relPath string) core.ImageSource { return core.ImageSource{URI: "asset:" + relPath} }

	cases := []struct {
		name           string
		layout         string
		x0, y0, x1, y1 int
	}{
		{name: "Free_ZeroOrigin", layout: "Free", x0: 0, y0: 0, x1: 64, y1: 0},
		{name: "GridVania_NegativeOrigin", layout: "GridVania", x0: -64, y0: 32, x1: 0, y1: 32},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := makeAdjacentLDtkProject(tc.layout, tc.x0, tc.y0, tc.x1, tc.y1)

			// 1. LoadLDtkWorld populates layout, origins, bounds, and resolved neighbours (both levelIid and levelUid).
			world, err := tilemap.LoadLDtkWorld(raw, "", resolve)
			if err != nil {
				t.Fatalf("LoadLDtkWorld: %v", err)
			}
			if world.Layout != tc.layout || len(world.Levels) != 2 {
				t.Fatalf("world = (layout=%q levels=%d), want (%q, 2)", world.Layout, len(world.Levels), tc.layout)
			}
			west := world.Level("Room_West")
			east := world.Level("Room_East")
			if west == nil || east == nil {
				t.Fatalf("missing levels: west=%v east=%v", west, east)
			}
			if len(west.Neighbours) != 1 || west.Neighbours[0] != (tilemap.LevelNeighbour{Identifier: "Room_East", IID: "iid-east", UID: 200, Dir: "e"}) {
				t.Fatalf("west neighbours = %+v", west.Neighbours)
			}
			if len(east.Neighbours) != 1 || east.Neighbours[0] != (tilemap.LevelNeighbour{Identifier: "Room_West", IID: "iid-west", UID: 100, Dir: "w"}) {
				t.Fatalf("east neighbours = %+v", east.Neighbours)
			}

			// 2. LoadLDtk also populates Origin and Neighbours on individual maps.
			eastMap, err := tilemap.LoadLDtk(raw, "Room_East", resolve)
			if err != nil {
				t.Fatalf("LoadLDtk(Room_East): %v", err)
			}
			if eastMap.Origin != (core.Point{X: core.Points(tc.x1), Y: core.Points(tc.y1)}) {
				t.Fatalf("eastMap.Origin = %+v, want (%d,%d)", eastMap.Origin, tc.x1, tc.y1)
			}
			if len(eastMap.Neighbours) != 1 || eastMap.Neighbours[0].Identifier != "Room_West" || eastMap.Neighbours[0].Dir != "w" {
				t.Fatalf("eastMap.Neighbours = %+v", eastMap.Neighbours)
			}

			// 3. Stream both levels around the seam and verify exact seamless draw ops.
			seamX := float32(tc.x1)
			seamY := float32(tc.y0)
			cam := &tilemap.Camera{X: seamX, Y: seamY + 24, Width: 128, Height: 48}
			stream := tilemap.NewWorldStream(tilemap.WorldStreamConfig{World: world})
			events := stream.UpdateCamera(cam)
			if len(events) != 2 || events[0].Kind != tilemap.WorldLevelEntered || events[1].Kind != tilemap.WorldLevelEntered {
				t.Fatalf("stream enter events = %+v, want 2 entered", events)
			}

			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return core.View{Children: []core.Node{
					core.Canvas{TestID: "world", Style: core.Style{Width: core.Pt(128), Height: core.Pt(48)}, Draw: func(d *core.Drawing, _ core.Size) {
						stream.Draw(d, cam)
					}},
					core.Canvas{TestID: "want", Style: core.Style{Width: core.Pt(128), Height: core.Pt(48)}, Draw: func(d *core.Drawing, _ core.Size) {
						// Room_West tiles at (x0+0, y0+32) and (x0+48, y0+32)
						d.DrawImageFrom(img, 0, 0, 16, 16, float32(tc.x0), float32(tc.y0+32), 16, 16)
						d.DrawImageFrom(img, 0, 0, 16, 16, float32(tc.x0+48), float32(tc.y0+32), 16, 16)
						// Room_East tiles at (x1+0, y1+32) and (x1+48, y1+32) — flush at seam x1!
						d.DrawImageFrom(img, 16, 0, 16, 16, float32(tc.x1), float32(tc.y1+32), 16, 16)
						d.DrawImageFrom(img, 16, 0, 16, 16, float32(tc.x1+48), float32(tc.y1+32), 16, 16)
					}},
				}}
			})
			sameOps(t, app.Find("world").Ops, app.Find("want").Ops)

			// 4. Cross-seam raycast from Room_West into Room_East's far right wall.
			rayFrom := core.Point{X: core.Points(tc.x0 + 24), Y: core.Points(tc.y0 + 20)}
			rayTo := core.Point{X: core.Points(tc.x1 + 60), Y: core.Points(tc.y1 + 20)}
			rh, ok := stream.Raycast(rayFrom, rayTo, tilemap.TileSolid)
			if !ok || rh.Point != (core.Point{X: core.Points(tc.x1 + 48), Y: core.Points(tc.y1 + 20)}) {
				t.Fatalf("cross-seam Raycast = (%+v, %v), want hit at (%d,%d)", rh, ok, tc.x1+48, tc.y1+20)
			}

			// 5. Walk a physics.Body from Room_West across the seam x=x1 into Room_East until hitting Room_East's right wall.
			grid := worldPhysicsGrid{stream: stream}
			body := physics.NewBoxBody(float32(tc.x0+24), float32(tc.y0+16), 12, 16)
			dt := time.Second / 60
			hitWall := false
			crossedSeam := false
			for step := range 120 {
				body.VX = 120
				body.VY = 120 // gravity pressing into floor across the seam
				res := physics.MoveAndSlide(body, grid, dt)
				if !res.Grounded {
					t.Fatalf("step %d: body lost ground at x=%v y=%v (seam=%v)", step, body.Bounds().X, body.Bounds().Y, seamX)
				}
				if body.Bounds().Y != float32(tc.y0+16) {
					t.Fatalf("step %d: body Y=%v, want %v", step, body.Bounds().Y, tc.y0+16)
				}
				if body.Bounds().X > seamX {
					crossedSeam = true
				}
				if res.OnRightWall {
					// Room_East right wall is at x1 + 48, body width is 12 -> stops at x1 + 36.
					wantX := float32(tc.x1 + 48 - 12)
					if body.Bounds().X != wantX {
						t.Fatalf("step %d: stopped at X=%v, want %v (snagged early?)", step, body.Bounds().X, wantX)
					}
					hitWall = true
					break
				}
			}
			if !crossedSeam || !hitWall {
				t.Fatalf("body did not cross seam cleanly and hit east wall: crossed=%v hitWall=%v x=%v", crossedSeam, hitWall, body.Bounds().X)
			}
		})
	}
}

func TestLDtkMultiWorldAndLinearLayoutErrors(t *testing.T) {
	multiWorld := []byte(`{
		"defaultGridSize": 16,
		"worldLayout": null,
		"levels": [],
		"worlds": [
			{
				"identifier": "Overworld",
				"worldLayout": "Free",
				"levels": [
					{"identifier": "Meadow", "iid": "m1", "uid": 1, "worldX": 0, "worldY": 0, "pxWid": 64, "pxHei": 64, "__neighbours": [{"levelIid": "c1", "dir": ">"}], "layerInstances": []},
					{"identifier": "Canopy", "iid": "c1", "uid": 2, "worldX": 0, "worldY": 0, "worldDepth": 1, "pxWid": 64, "pxHei": 64, "__neighbours": [{"levelIid": "m1", "dir": "<"}], "layerInstances": []}
				]
			},
			{
				"identifier": "Dungeon",
				"worldLayout": "GridVania",
				"levels": [
					{"identifier": "Crypt", "iid": "d1", "uid": 3, "worldX": -128, "worldY": 256, "pxWid": 128, "pxHei": 128, "__neighbours": [], "layerInstances": []}
				]
			}
		]
	}`)

	if _, err := tilemap.LoadLDtkWorld(multiWorld, "", nil); err == nil {
		t.Fatal("expected error when multi-world project has >1 world and empty worldIdentifier")
	}
	ow, err := tilemap.LoadLDtkWorld(multiWorld, "Overworld", nil)
	if err != nil {
		t.Fatalf("LoadLDtkWorld(Overworld): %v", err)
	}
	if ow.Name != "Overworld" || len(ow.Levels) != 2 || ow.Levels[1].WorldDepth != 1 {
		t.Fatalf("Overworld = %+v", ow)
	}
	if ow.Levels[0].Neighbours[0].Dir != ">" || ow.Levels[0].Neighbours[0].Identifier != "Canopy" {
		t.Fatalf("Overworld[0] neighbours = %+v", ow.Levels[0].Neighbours)
	}
	dg, err := tilemap.LoadLDtkWorld(multiWorld, "Dungeon", nil)
	if err != nil {
		t.Fatalf("LoadLDtkWorld(Dungeon): %v", err)
	}
	if dg.Layout != "GridVania" || dg.Levels[0].Origin != (core.Point{X: -128, Y: 256}) {
		t.Fatalf("Dungeon = %+v", dg)
	}

	linear := []byte(`{"defaultGridSize":16,"worldLayout":"LinearHorizontal","levels":[{"identifier":"A","worldX":-1,"worldY":-1,"pxWid":32,"pxHei":32}]}`)
	_, err = tilemap.LoadLDtkWorld(linear, "", nil)
	if err == nil {
		t.Fatal("expected error for LinearHorizontal layout")
	}
	le, ok := loaderr.As(err)
	if !ok || le.Field != "worldLayout" || le.Line == 0 || le.Fix == "" {
		t.Fatalf("linear layout error = %v (loaderr=%+v), want loaderr on worldLayout", err, le)
	}
}

func TestTiledWorldMapsAndPatterns(t *testing.T) {
	worldJSON := []byte(`{
		"maps": [
			{
				"fileName": "hub.tmj",
				"x": 0,
				"y": 0,
				"width": 64,
				"height": 48
			},
			{
				"fileName": "secret_shrine.tmj",
				"x": -32,
				"y": 48
			},
			{
				"fileName": "zone_0_0.tmj",
				"x": 1000,
				"y": 2000,
				"width": 64,
				"height": 64
			}
		],
		"patterns": [
			{
				"regexp": "zone_(-?\\d+)_(-?\\d+)\\.tmj",
				"multiplierX": 64,
				"multiplierY": 48,
				"offsetX": 64,
				"offsetY": 0,
				"mapWidth": 64,
				"mapHeight": 48
			},
			{
				"regexp": "strip_(-?\\d+)\\.tmj",
				"multiplierX": -32,
				"offsetX": 0,
				"offsetY": -96,
				"mapHeight": 32
			}
		],
		"onlyShowAdjacentMaps": true,
		"type": "world"
	}`)

	tw, err := tilemap.ParseTiledWorld(worldJSON)
	if err != nil {
		t.Fatalf("ParseTiledWorld: %v", err)
	}
	if !tw.OnlyShowAdjacentMaps || len(tw.Maps) != 3 || len(tw.Patterns) != 2 {
		t.Fatalf("parsed TiledWorld = %+v", tw)
	}

	candidates := []string{
		"zone_2_-1.tmj",
		"zone_0_0.tmj", // already in explicit maps -> must NOT be duplicated
		"zone_1_0.tmj",
		"strip_2.tmj",
		"unrelated.tmj",
	}
	resolved := tw.Resolve(candidates)
	want := []tilemap.TiledWorldMap{
		{FileName: "hub.tmj", X: 0, Y: 0, Width: 64, Height: 48},
		{FileName: "secret_shrine.tmj", X: -32, Y: 48, Width: 0, Height: 0},
		{FileName: "zone_0_0.tmj", X: 1000, Y: 2000, Width: 64, Height: 64},
		// Pattern matches sorted lexicographically: strip_2, zone_1_0, zone_2_-1
		{FileName: "strip_2.tmj", X: -64, Y: -96, Width: 32, Height: 32},
		{FileName: "zone_1_0.tmj", X: 128, Y: 0, Width: 64, Height: 48},
		{FileName: "zone_2_-1.tmj", X: 192, Y: -48, Width: 64, Height: 48},
	}
	if len(resolved) != len(want) {
		t.Fatalf("Resolve len = %d, want %d: %+v", len(resolved), len(want), resolved)
	}
	for i := range want {
		if resolved[i] != want[i] {
			t.Fatalf("resolved[%d] = %+v, want %+v", i, resolved[i], want[i])
		}
	}

	// Verify LoadTiledWorld populates omitted dimensions (secret_shrine.tmj) by peeking header
	// while keeping explicitly sized maps lazy until streamed.
	makeTMJ := func(w, h int) []byte {
		return []byte(`{
			"width": ` + itoa(w) + `,
			"height": ` + itoa(h) + `,
			"tilewidth": 16,
			"tileheight": 16,
			"orientation": "orthogonal",
			"tilesets": [{"firstgid": 1, "name": "t", "tilewidth": 16, "tileheight": 16, "columns": 2, "tilecount": 2, "image": "t.png"}],
			"layers": [{"name": "ground", "type": "tilelayer", "width": ` + itoa(w) + `, "height": ` + itoa(h) + `, "data": [1,0,0,1]}]
		}`)
	}
	mapFiles := map[string][]byte{
		"hub.tmj":           makeTMJ(4, 3),
		"secret_shrine.tmj": makeTMJ(2, 2),
		"zone_0_0.tmj":      makeTMJ(4, 4),
		"strip_2.tmj":       makeTMJ(2, 2),
		"zone_1_0.tmj":      makeTMJ(4, 3),
		"zone_2_-1.tmj":     makeTMJ(4, 3),
	}
	// Fix data length to w*h for each map so LoadTiled succeeds cleanly.
	makeValidTMJ := func(w, h int) []byte {
		data := "["
		for i := range w * h {
			if i > 0 {
				data += ","
			}
			if i == 0 {
				data += "1"
			} else {
				data += "0"
			}
		}
		data += "]"
		return []byte(`{
			"width": ` + itoa(w) + `,
			"height": ` + itoa(h) + `,
			"tilewidth": 16,
			"tileheight": 16,
			"orientation": "orthogonal",
			"tilesets": [{"firstgid": 1, "name": "t", "tilewidth": 16, "tileheight": 16, "columns": 2, "tilecount": 2, "image": "t.png"}],
			"layers": [{"name": "ground", "type": "tilelayer", "width": ` + itoa(w) + `, "height": ` + itoa(h) + `, "data": ` + data + `}]
		}`)
	}
	_ = makeTMJ
	mapFiles["hub.tmj"] = makeValidTMJ(4, 3)
	mapFiles["secret_shrine.tmj"] = makeValidTMJ(2, 2)
	mapFiles["zone_0_0.tmj"] = makeValidTMJ(4, 4)
	mapFiles["strip_2.tmj"] = makeValidTMJ(2, 2)
	mapFiles["zone_1_0.tmj"] = makeValidTMJ(4, 3)
	mapFiles["zone_2_-1.tmj"] = makeValidTMJ(4, 3)

	loadCounts := make(map[string]int)
	world, err := tilemap.LoadTiledWorld(worldJSON, tilemap.TiledWorldLoader{
		LoadMap: func(fileName string) ([]byte, error) {
			loadCounts[fileName]++
			b, ok := mapFiles[fileName]
			if !ok {
				return nil, errors.New("not found")
			}
			return b, nil
		},
		ListFiles: func() ([]string, error) { return candidates, nil },
	})
	if err != nil {
		t.Fatalf("LoadTiledWorld: %v", err)
	}
	// Only secret_shrine.tmj lacked width/height, so only it was peeked.
	if loadCounts["secret_shrine.tmj"] != 1 || loadCounts["hub.tmj"] != 0 || loadCounts["zone_1_0.tmj"] != 0 {
		t.Fatalf("unexpected eager loads: %+v", loadCounts)
	}
	shrine := world.Level("secret_shrine.tmj")
	if shrine == nil || shrine.Bounds != (core.Bounds{Origin: core.Point{X: -32, Y: 48}, Size: core.Size{Width: 32, Height: 32}}) {
		t.Fatalf("shrine bounds = %+v, want (-32,48) 32x32", shrine)
	}
	// Loading a pattern-matched map sets its exact Tiled Origin.
	z10, err := world.LoadLevel("zone_1_0.tmj")
	if err != nil {
		t.Fatalf("LoadLevel(zone_1_0.tmj): %v", err)
	}
	if z10.Origin != (core.Point{X: 128, Y: 0}) || z10.WorldBounds() != (core.Bounds{Origin: core.Point{X: 128, Y: 0}, Size: core.Size{Width: 64, Height: 48}}) {
		t.Fatalf("zone_1_0 Origin=%+v WorldBounds=%+v", z10.Origin, z10.WorldBounds())
	}
}

func TestParseTiledWorldErrors(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantField string
	}{
		{name: "malformed_json", input: `{"maps": [`, wantField: ""},
		{name: "bad_type", input: `{"type": "map", "maps": []}`, wantField: "type"},
		{name: "empty_fileName", input: `{"maps": [{"fileName": "", "x": 0, "y": 0}]}`, wantField: "maps[0].fileName"},
		{name: "neg_width", input: `{"maps": [{"fileName": "a.tmj", "width": -1}]}`, wantField: "maps[0].width"},
		{name: "neg_height", input: `{"maps": [{"fileName": "a.tmj", "height": -5}]}`, wantField: "maps[0].height"},
		{name: "empty_regexp", input: `{"patterns": [{"regexp": ""}]}`, wantField: "patterns[0].regexp"},
		{name: "invalid_regexp", input: `{"patterns": [{"regexp": "map_([0-9+\\.tmj"}]}`, wantField: "patterns[0].regexp"},
		{name: "no_capture_groups", input: `{"patterns": [{"regexp": "map_\\d+\\.tmj"}]}`, wantField: "patterns[0].regexp"},
		{name: "neg_mapWidth", input: `{"patterns": [{"regexp": "m(\\d+)", "mapWidth": -16}]}`, wantField: "patterns[0].mapWidth"},
		{name: "neg_mapHeight", input: `{"patterns": [{"regexp": "m(\\d+)", "mapHeight": -16}]}`, wantField: "patterns[0].mapHeight"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tilemap.ParseTiledWorld([]byte(tc.input))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			le, ok := loaderr.As(err)
			if !ok {
				t.Fatalf("error %T is not *loaderr.Error: %v", err, err)
			}
			if le.Line == 0 || le.Fix == "" {
				t.Fatalf("loaderr missing line or fix: %+v", le)
			}
			if tc.wantField != "" && le.Field != tc.wantField {
				t.Fatalf("loaderr.Field = %q, want %q (err=%v)", le.Field, tc.wantField, err)
			}
		})
	}
}

func TestWorldStreamEnterLeaveAndHysteresis(t *testing.T) {
	makeLevel := func(id string, x, y float32) tilemap.WorldLevel {
		orig := core.Point{X: core.Points(x), Y: core.Points(y)}
		return tilemap.WorldLevel{
			ID:     id,
			Origin: orig,
			Bounds: core.Bounds{Origin: orig, Size: core.Size{Width: 100, Height: 100}},
			Load: func() (*tilemap.Tilemap, error) {
				m := tilemap.New(10, 10, 10, 10)
				m.Origin = orig
				m.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("g", 10, 10, nil)}
				m.SetTile(0, 0, 0, 1)
				m.SetFlags(1, tilemap.TileSolid)
				return m, nil
			},
		}
	}

	world := &tilemap.LevelWorld{
		Layout: "Free",
		Levels: []tilemap.WorldLevel{
			makeLevel("A", 0, 0),
			makeLevel("B", 100, 0),
			makeLevel("C", 200, 0),
		},
	}

	var log []string
	stream := tilemap.NewWorldStream(tilemap.WorldStreamConfig{
		World:        world,
		LoadMargin:   10,
		UnloadMargin: 40,
		OnEnter:      func(l *tilemap.WorldLevel, _ *tilemap.Tilemap) { log = append(log, "+"+l.ID) },
		OnLeave:      func(l *tilemap.WorldLevel, _ *tilemap.Tilemap) { log = append(log, "-"+l.ID) },
	})

	// Region [20, 60] x [20, 60]: loadRect [10, 70] -> only A loads.
	ev := stream.Update(core.Bounds{Origin: core.Point{X: 20, Y: 20}, Size: core.Size{Width: 40, Height: 40}})
	if len(ev) != 1 || ev[0].Kind != tilemap.WorldLevelEntered || ev[0].Level.ID != "A" {
		t.Fatalf("step 1 events = %+v", ev)
	}
	if stream.LoadedCount() != 1 || stream.Map("A") == nil {
		t.Fatalf("step 1 loaded = %d", stream.LoadedCount())
	}

	// Move to [120, 160]: loadRect [110, 170] loads B; keepRect [80, 200] still overlaps A [0, 100) so A stays!
	ev = stream.Update(core.Bounds{Origin: core.Point{X: 120, Y: 20}, Size: core.Size{Width: 40, Height: 40}})
	if len(ev) != 1 || ev[0].Kind != tilemap.WorldLevelEntered || ev[0].Level.ID != "B" {
		t.Fatalf("step 2 events = %+v (expected hysteresis to keep A)", ev)
	}
	if stream.LoadedCount() != 2 {
		t.Fatalf("step 2 loaded = %d, want 2", stream.LoadedCount())
	}

	// Move to [195, 235]: keepRect [155, 275] drops A, loadRect [185, 245] loads C.
	ev = stream.Update(core.Bounds{Origin: core.Point{X: 195, Y: 20}, Size: core.Size{Width: 40, Height: 40}})
	if len(ev) != 2 || ev[0].Kind != tilemap.WorldLevelLeft || ev[0].Level.ID != "A" || ev[1].Kind != tilemap.WorldLevelEntered || ev[1].Level.ID != "C" {
		t.Fatalf("step 3 events = %+v", ev)
	}
	if stream.Map("A") != nil || stream.Map("B") == nil || stream.Map("C") == nil {
		t.Fatalf("step 3 loaded maps mismatch: A=%v B=%v C=%v", stream.Map("A"), stream.Map("B"), stream.Map("C"))
	}

	// TilesInRect spans B and C.
	hits := stream.TilesInRect(core.Bounds{Origin: core.Point{X: 100, Y: 0}, Size: core.Size{Width: 120, Height: 20}}, tilemap.TileSolid)
	if len(hits) != 2 || hits[0].Bounds.Origin != (core.Point{X: 100, Y: 0}) || hits[1].Bounds.Origin != (core.Point{X: 200, Y: 0}) {
		t.Fatalf("TilesInRect across B & C = %+v", hits)
	}

	// Close unloads B and C in deterministic order.
	closeEv := stream.Close()
	if len(closeEv) != 2 || closeEv[0].Level.ID != "B" || closeEv[1].Level.ID != "C" || stream.LoadedCount() != 0 {
		t.Fatalf("Close events = %+v, loaded=%d", closeEv, stream.LoadedCount())
	}

	wantLog := []string{"+A", "+B", "-A", "+C", "-B", "-C"}
	if len(log) != len(wantLog) {
		t.Fatalf("callback log = %v, want %v", log, wantLog)
	}
	for i := range wantLog {
		if log[i] != wantLog[i] {
			t.Fatalf("callback log = %v, want %v", log, wantLog)
		}
	}
}
