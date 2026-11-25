package tilemap_test

import (
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestProjectionOrthogonalIsometricStaggeredRoundTrip(t *testing.T) {
	configs := []struct {
		name         string
		orientation  tilemap.Orientation
		staggerAxis  tilemap.StaggerAxis
		staggerIndex tilemap.StaggerIndex
		tw, th       float32
	}{
		{"orthogonal", tilemap.OrientationOrthogonal, tilemap.StaggerY, tilemap.StaggerOdd, 16, 16},
		{"isometric", tilemap.OrientationIsometric, tilemap.StaggerY, tilemap.StaggerOdd, 64, 32},
		{"staggered_y_odd", tilemap.OrientationStaggered, tilemap.StaggerY, tilemap.StaggerOdd, 64, 32},
		{"staggered_y_even", tilemap.OrientationStaggered, tilemap.StaggerY, tilemap.StaggerEven, 64, 32},
		{"staggered_x_odd", tilemap.OrientationStaggered, tilemap.StaggerX, tilemap.StaggerOdd, 64, 32},
		{"staggered_x_even", tilemap.OrientationStaggered, tilemap.StaggerX, tilemap.StaggerEven, 64, 32},
	}

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			m := tilemap.New(10, 10, tc.tw, tc.th)
			m.Orientation = tc.orientation
			m.StaggerAxis = tc.staggerAxis
			m.StaggerIndex = tc.staggerIndex

			for row := -4; row <= 4; row++ {
				for col := -4; col <= 4; col++ {
					pt := m.TileToWorld(col, row)
					gotCol, gotRow := m.WorldToTile(pt)
					if gotCol != col || gotRow != row {
						t.Fatalf("TileToWorld(%d,%d)=%+v -> WorldToTile=(%d,%d), want (%d,%d)",
							col, row, pt, gotCol, gotRow, col, row)
					}
					// Perturb strictly inside the tile diamond/box and verify same cell.
					for _, d := range [][2]float32{{0.15, 0.1}, {-0.15, 0.1}, {0.1, -0.15}, {-0.1, -0.15}} {
						p2 := core.Point{
							X: pt.X + core.Points(d[0]*tc.tw),
							Y: pt.Y + core.Points(d[1]*tc.th),
						}
						c2, r2 := m.WorldToTile(p2)
						if c2 != col || r2 != row {
							t.Fatalf("perturbed (%d,%d) delta %v -> (%d,%d), want (%d,%d)", col, row, d, c2, r2, col, row)
						}
					}
				}
			}

			wb := m.WorldBounds()
			if wb.Size.Width <= 0 || wb.Size.Height <= 0 {
				t.Fatalf("WorldBounds = %+v, want positive size", wb)
			}
		})
	}
}

func TestIsometricAndStaggeredDrawOrderAndCulling(t *testing.T) {
	img := core.ImageSource{URI: "asset:iso.png"}
	ts := tilemap.NewGridTileset(1, img, 64, 32, 4, 16)

	// 20x20 isometric map; verify painter's order (non-decreasing Y) and tight camera culling.
	iso := tilemap.New(20, 20, 64, 32, ts)
	iso.Orientation = tilemap.OrientationIsometric
	data := make([]uint32, 20*20)
	for i := range data {
		data[i] = uint32(i%4) + 1
	}
	iso.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("iso", 20, 20, data)}

	// Camera centered on tile (10, 10) with small viewport covering only a handful of tiles.
	center := iso.TileToWorld(10, 10)
	cam := &tilemap.Camera{X: float32(center.X), Y: float32(center.Y), Zoom: 1, Width: 128, Height: 64}

	// 4x4 staggered X map to verify strict painter's order across staggered columns.
	stagX := tilemap.New(4, 4, 64, 32, ts)
	stagX.Orientation = tilemap.OrientationStaggered
	stagX.StaggerAxis = tilemap.StaggerX
	stagX.StaggerIndex = tilemap.StaggerOdd
	stagX.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("stag", 4, 4, data[:16])}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "iso-full", Style: core.Style{Width: core.Pt(128), Height: core.Pt(64)}, Draw: func(d *core.Drawing, _ core.Size) {
				iso.Draw(d, nil)
			}},
			core.Canvas{TestID: "iso-culled", Style: core.Style{Width: core.Pt(128), Height: core.Pt(64)}, Draw: func(d *core.Drawing, _ core.Size) {
				iso.Draw(d, cam)
			}},
			core.Canvas{TestID: "stagx-full", Style: core.Style{Width: core.Pt(256), Height: core.Pt(160)}, Draw: func(d *core.Drawing, _ core.Size) {
				stagX.Draw(d, nil)
			}},
		}}
	})

	isoFull := app.Find("iso-full").Ops
	if len(isoFull) != 400 {
		t.Fatalf("iso-full drew %d ops, want 400", len(isoFull))
	}
	for i := 1; i < len(isoFull); i++ {
		if isoFull[i].Y < isoFull[i-1].Y {
			t.Fatalf("iso-full broke painter's order at op %d: Y=%g < prev Y=%g", i, isoFull[i].Y, isoFull[i-1].Y)
		}
	}

	isoCulled := app.Find("iso-culled").Ops
	if len(isoCulled) == 0 || len(isoCulled) >= 30 {
		t.Fatalf("iso-culled drew %d ops, want >0 and <30 (culled from 400)", len(isoCulled))
	}
	for i := 1; i < len(isoCulled); i++ {
		if isoCulled[i].Y < isoCulled[i-1].Y {
			t.Fatalf("iso-culled broke painter's order at op %d: Y=%g < prev Y=%g", i, isoCulled[i].Y, isoCulled[i-1].Y)
		}
	}

	stagOps := app.Find("stagx-full").Ops
	if len(stagOps) != 16 {
		t.Fatalf("stagx-full drew %d ops, want 16", len(stagOps))
	}
	for i := 1; i < len(stagOps); i++ {
		if stagOps[i].Y < stagOps[i-1].Y {
			t.Fatalf("stagx-full broke painter's order at op %d: Y=%g < prev Y=%g", i, stagOps[i].Y, stagOps[i-1].Y)
		}
	}
}

func TestHexagonalProjectionNeighborsDistanceAndDraw(t *testing.T) {
	configs := []struct {
		name         string
		axis         tilemap.StaggerAxis
		index        tilemap.StaggerIndex
		tw, th, side float32
	}{
		{"pointy_odd", tilemap.StaggerY, tilemap.StaggerOdd, 32, 36, 18},
		{"pointy_even", tilemap.StaggerY, tilemap.StaggerEven, 32, 36, 18},
		{"flat_odd", tilemap.StaggerX, tilemap.StaggerOdd, 36, 32, 18},
		{"flat_even", tilemap.StaggerX, tilemap.StaggerEven, 36, 32, 18},
		{"pointy_default_side", tilemap.StaggerY, tilemap.StaggerOdd, 32, 36, 0},
	}

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			m := tilemap.New(8, 8, tc.tw, tc.th)
			m.Orientation = tilemap.OrientationHexagonal
			m.StaggerAxis = tc.axis
			m.StaggerIndex = tc.index
			m.HexSideLength = tc.side

			for row := -3; row <= 3; row++ {
				for col := -3; col <= 3; col++ {
					center := m.TileToWorld(col, row)
					gc, gr := m.WorldToTile(center)
					if gc != col || gr != row {
						t.Fatalf("hex round-trip (%d,%d) -> %+v -> (%d,%d)", col, row, center, gc, gr)
					}
					// Slightly offset interior point must still cube-round to (col, row).
					pOff := core.Point{X: center.X + core.Points(tc.tw*0.18), Y: center.Y - core.Points(tc.th*0.12)}
					if oc, or := m.WorldToTile(pOff); oc != col || or != row {
						t.Fatalf("hex interior (%d,%d) -> (%d,%d)", col, row, oc, or)
					}

					if d := m.HexDistance(col, row, col, row); d != 0 {
						t.Fatalf("HexDistance self = %d, want 0", d)
					}

					neighbors := m.HexNeighbors(col, row)
					seen := make(map[[2]int]bool, 6)
					for _, n := range neighbors {
						if seen[n] {
							t.Fatalf("duplicate hex neighbor %v for (%d,%d)", n, col, row)
						}
						seen[n] = true
						if d := m.HexDistance(col, row, n[0], n[1]); d != 1 {
							t.Fatalf("HexDistance((%d,%d), %v) = %d, want 1", col, row, n, d)
						}
						// Reciprocity: (col, row) must be a neighbor of n.
						back := m.HexNeighbors(n[0], n[1])
						found := false
						for _, b := range back {
							if b == [2]int{col, row} {
								found = true
								break
							}
						}
						if !found {
							t.Fatalf("neighbor reciprocity failed: (%d,%d) -> %v -> %v", col, row, n, back)
						}
					}
				}
			}

			// Multi-step distance sanity: walking 3 steps in one neighbor direction gives distance <= 3 and exact for straight axial ray.
			n0 := m.HexNeighbors(0, 0)[0]
			n1 := m.HexNeighbors(n0[0], n0[1])[0]
			n2 := m.HexNeighbors(n1[0], n1[1])[0]
			if d := m.HexDistance(0, 0, n2[0], n2[1]); d != 3 {
				t.Fatalf("3-step straight hex distance = %d, want 3", d)
			}
		})
	}

	// Verify visible-region hex drawing and painter's order.
	img := core.ImageSource{URI: "asset:hex.png"}
	ts := tilemap.NewGridTileset(1, img, 32, 36, 4, 16)
	hm := tilemap.New(12, 12, 32, 36, ts)
	hm.Orientation = tilemap.OrientationHexagonal
	hm.StaggerAxis = tilemap.StaggerY
	hm.StaggerIndex = tilemap.StaggerOdd
	hm.HexSideLength = 18
	data := make([]uint32, 144)
	for i := range data {
		data[i] = 1
	}
	hm.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("hex", 12, 12, data)}
	c := hm.TileToWorld(5, 5)
	cam := &tilemap.Camera{X: float32(c.X), Y: float32(c.Y), Zoom: 1, Width: 96, Height: 80}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{TestID: "hex", Style: core.Style{Width: core.Pt(96), Height: core.Pt(80)}, Draw: func(d *core.Drawing, _ core.Size) {
			hm.Draw(d, cam)
		}}
	})
	ops := app.Find("hex").Ops
	if len(ops) == 0 || len(ops) >= 40 {
		t.Fatalf("culled hex drew %d ops, want >0 and <40 (from 144)", len(ops))
	}
	for i := 1; i < len(ops); i++ {
		if ops[i].Y < ops[i-1].Y {
			t.Fatalf("hex draw broke painter's order at op %d: Y=%g < prev Y=%g", i, ops[i].Y, ops[i-1].Y)
		}
	}
}

func TestRuntimeSetTileFillRectQueriesAndDraw(t *testing.T) {
	img := core.ImageSource{URI: "asset:tiles.png"}
	ts := tilemap.NewGridTileset(1, img, 16, 16, 4, 16)
	m := tilemap.New(5, 3, 16, 16, ts)
	m.SetFlags(1, tilemap.TileSolid)
	m.SetFlags(2, tilemap.TileOneWay)
	m.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("main", 5, 3, nil)}

	// Fill a solid vertical wall at col=2, rows 0..2 (also exercises out-of-bounds clipping).
	m.FillRect(0, 2, -1, 1, 5, 1)
	for r := range 3 {
		if m.TileAt(0, 2, r) != 1 || m.FlagsAt(2, r) != tilemap.TileSolid {
			t.Fatalf("cell (2,%d): gid=%d flags=%v, want 1/TileSolid", r, m.TileAt(0, 2, r), m.FlagsAt(2, r))
		}
	}

	// Raycast hits the solid wall at (2, 1).
	hit, ok := m.Raycast(core.Point{X: 8, Y: 24}, core.Point{X: 72, Y: 24}, tilemap.TileSolid)
	if !ok || hit.Col != 2 || hit.Row != 1 {
		t.Fatalf("raycast before carve = (%+v, %v), want hit at (2,1)", hit, ok)
	}

	// Carve a doorway at (2, 1) with SetTile and place a one-way platform at (3, 1).
	m.SetTile(0, 2, 1, 0)
	m.SetTile(0, 3, 1, 2)

	if m.FlagsAt(2, 1) != 0 {
		t.Fatalf("FlagsAt(2,1) after carve = %v, want 0", m.FlagsAt(2, 1))
	}
	if _, ok := m.Raycast(core.Point{X: 8, Y: 24}, core.Point{X: 72, Y: 24}, tilemap.TileSolid); ok {
		t.Fatal("raycast should pass through carved doorway at (2,1)")
	}
	hits := m.TilesInRect(core.Bounds{Origin: core.Point{X: 32, Y: 16}, Size: core.Size{Width: 32, Height: 16}}, tilemap.TileOneWay)
	if len(hits) != 1 || hits[0].Col != 3 || hits[0].Row != 1 || hits[0].TileID != 2 {
		t.Fatalf("TilesInRect after runtime edit = %+v, want single one-way hit at (3,1)", hits)
	}

	// Draw immediately reflects the 3 remaining non-empty tiles: (2,0), (2,2), (3,1).
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{TestID: "rt", Style: core.Style{Width: core.Pt(80), Height: core.Pt(48)}, Draw: func(d *core.Drawing, _ core.Size) {
			m.Draw(d, nil)
		}}
	})
	if ops := app.Find("rt").Ops; len(ops) != 3 {
		t.Fatalf("Draw emitted %d ops after runtime edit, want 3", len(ops))
	}
}

func TestAutotile4And8BitCornerGatingAndSetAutotile(t *testing.T) {
	// Exhaustively check all 256 3x3 neighbor patterns: ComputeMask8 must produce exactly 47 unique blob masks.
	unique := make(map[uint8]bool)
	offsets := [8][2]int{
		{0, -1}, {1, 0}, {0, 1}, {-1, 0},
		{1, -1}, {1, 1}, {-1, 1}, {-1, -1},
	}
	for bits := range 256 {
		match := func(c, r int) bool {
			for i, o := range offsets {
				if c == o[0] && r == o[1] {
					return (bits & (1 << i)) != 0
				}
			}
			return false
		}
		m8 := tilemap.ComputeMask8(match, 0, 0)
		unique[m8] = true
		m4 := tilemap.ComputeMask4(match, 0, 0)
		if m4 != uint8(bits&0x0F) {
			t.Fatalf("bits=%08b: ComputeMask4=%#x, want %#x", bits, m4, bits&0x0F)
		}
		// Verify diagonal corner gating: NE requires N and E, etc.
		if m8&tilemap.MaskNorthEast != 0 && (m8&(tilemap.MaskNorth|tilemap.MaskEast) != tilemap.MaskNorth|tilemap.MaskEast) {
			t.Fatalf("bits=%08b: NE bit set without N&E: %08b", bits, m8)
		}
		if m8&tilemap.MaskSouthEast != 0 && (m8&(tilemap.MaskSouth|tilemap.MaskEast) != tilemap.MaskSouth|tilemap.MaskEast) {
			t.Fatalf("bits=%08b: SE bit set without S&E: %08b", bits, m8)
		}
		if m8&tilemap.MaskSouthWest != 0 && (m8&(tilemap.MaskSouth|tilemap.MaskWest) != tilemap.MaskSouth|tilemap.MaskWest) {
			t.Fatalf("bits=%08b: SW bit set without S&W: %08b", bits, m8)
		}
		if m8&tilemap.MaskNorthWest != 0 && (m8&(tilemap.MaskNorth|tilemap.MaskWest) != tilemap.MaskNorth|tilemap.MaskWest) {
			t.Fatalf("bits=%08b: NW bit set without N&W: %08b", bits, m8)
		}
	}
	if len(unique) != 47 {
		t.Fatalf("ComputeMask8 produced %d unique masks across all 256 neighbor patterns, want 47", len(unique))
	}

	// Build an 8-bit AutotileSet encoding GID = 100 + uint32(mask) with fallback Default = 999.
	tiles8 := make(map[uint8]uint32, 47)
	for mask := range unique {
		tiles8[mask] = 100 + uint32(mask)
	}
	// Remove isolated mask 0 to verify Default fallback.
	delete(tiles8, 0)
	set8 := tilemap.AutotileSet{
		Mode:    tilemap.Autotile8,
		Tiles:   tiles8,
		Default: 999,
	}

	m := tilemap.New(4, 4, 16, 16)
	m.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("terrain", 4, 4, nil)}
	// Paint a 2x2 block at (1,1)..(2,2) plus an isolated tile at (0,0).
	m.FillRect(0, 1, 1, 2, 2, 1)
	m.SetTile(0, 0, 0, 1)

	isTerrain := func(gid uint32) bool { return gid != 0 }
	m.ApplyAutotile(0, set8, isTerrain)

	// (0,0) has diagonal neighbor (1,1) ONLY (no E or S cardinal), so SE must be gated off -> mask 0 -> Default 999.
	if got := m.TileAt(0, 0, 0); got != 999 {
		t.Fatalf("isolated (0,0) with ungated SE diagonal got GID %d, want 999 (Default)", got)
	}
	// (1,1) has E=(2,1), S=(1,2), SE=(2,2), and NW=(0,0) gated off because N and W are empty -> mask E|S|SE.
	want11 := 100 + uint32(tilemap.MaskEast|tilemap.MaskSouth|tilemap.MaskSouthEast)
	if got := m.TileAt(0, 1, 1); got != want11 {
		t.Fatalf("(1,1) got GID %d, want %d (E|S|SE)", got, want11)
	}

	// Now place terrain at (1, 0) and (0, 1) via SetAutotile: (0,0) and (1,1) must both update their corner-gated masks!
	m.SetAutotile(0, 1, 0, true, set8, isTerrain)
	m.SetAutotile(0, 0, 1, true, set8, isTerrain)

	// Now (0,0) has E=(1,0), S=(0,1), AND SE=(1,1) -> E|S|SE!
	want00 := 100 + uint32(tilemap.MaskEast|tilemap.MaskSouth|tilemap.MaskSouthEast)
	if got := m.TileAt(0, 0, 0); got != want00 {
		t.Fatalf("after SetAutotile neighbors, (0,0) got GID %d, want %d (E|S|SE)", got, want00)
	}
	// (1,1) now has N=(1,0), W=(0,1), NW=(0,0), E=(2,1), S=(1,2), SE=(2,2)!
	want11After := 100 + uint32(tilemap.MaskNorth|tilemap.MaskEast|tilemap.MaskSouth|tilemap.MaskWest|tilemap.MaskSouthEast|tilemap.MaskNorthWest)
	if got := m.TileAt(0, 1, 1); got != want11After {
		t.Fatalf("after SetAutotile neighbors, (1,1) got GID %d, want %d", got, want11After)
	}

	// Removing (1, 0) clears (1,0) and gates off NW on (1,1) and SE on (0,0).
	m.SetAutotile(0, 1, 0, false, set8, isTerrain)
	if m.TileAt(0, 1, 0) != 0 {
		t.Fatalf("removed cell (1,0) = %d, want 0", m.TileAt(0, 1, 0))
	}
	want00Removed := 100 + uint32(tilemap.MaskSouth)
	if got := m.TileAt(0, 0, 0); got != want00Removed {
		t.Fatalf("after removing (1,0), (0,0) got GID %d, want %d (S only)", got, want00Removed)
	}
}

func TestLoadTiledInfiniteImageAndNestedGroupLayers(t *testing.T) {
	raw := []byte(`{
		"width": 0, "height": 0, "tilewidth": 16, "tileheight": 16,
		"orientation": "orthogonal",
		"infinite": true,
		"tilesets": [
			{"firstgid": 1, "name": "t", "image": "tiles.png", "tilewidth": 16, "tileheight": 16, "columns": 2, "tilecount": 4}
		],
		"layers": [
			{
				"name": "sky", "type": "imagelayer", "image": "sky.png",
				"imagewidth": 64, "imageheight": 32,
				"offsetx": 0, "offsety": 0, "opacity": 1, "visible": true,
				"parallaxx": 0.5, "parallaxy": 1, "repeatx": true, "repeaty": false
			},
			{
				"name": "world", "type": "group", "offsetx": 4, "offsety": 6, "opacity": 0.5, "visible": true,
				"layers": [
					{
						"name": "sub", "type": "group", "offsetx": 2, "offsety": -2, "opacity": 0.5, "visible": true,
						"layers": [
							{
								"name": "ground", "type": "tilelayer", "visible": true, "opacity": 1,
								"chunks": [
									{"x": -2, "y": -1, "width": 2, "height": 1, "data": [1, 0]},
									{"x": 0, "y": 0, "width": 2, "height": 1, "data": [0, 2]}
								]
							},
							{
								"name": "fog", "type": "imagelayer", "image": "fog.png",
								"imagewidth": 32, "imageheight": 32,
								"offsetx": 10, "offsety": 5, "opacity": 0.8, "visible": true
							}
						]
					}
				]
			}
		]
	}`)

	m, err := tilemap.LoadTiled(raw, tilemap.TilesetLoader{
		LoadImage: func(p string) (core.ImageSource, error) {
			return core.ImageSource{URI: "asset:" + p}, nil
		},
	})
	if err != nil {
		t.Fatalf("LoadTiled infinite/image/group: %v", err)
	}
	if !m.Infinite {
		t.Fatal("expected m.Infinite = true")
	}
	if len(m.ImageLayers) != 2 {
		t.Fatalf("len(ImageLayers) = %d, want 2", len(m.ImageLayers))
	}
	sky := m.ImageLayer("sky")
	if sky == nil || !sky.RepeatX || sky.RepeatY || sky.ParallaxX != 0.5 || sky.Image.URI != "asset:sky.png" {
		t.Fatalf("sky layer = %+v", sky)
	}
	fog := m.ImageLayer("fog")
	if fog == nil || fog.OffsetX != 16 || fog.OffsetY != 9 || math.Abs(float64(fog.Opacity-0.2)) > 1e-5 {
		t.Fatalf("nested group fog layer = %+v, want offset (16,9) opacity 0.2", fog)
	}

	ground := m.Layer("ground")
	if ground == nil {
		t.Fatal("ground layer missing")
	}
	// Infinite Tiled layers load directly into sparse chunked layers preserving real tile coordinates.
	// Chunks span x in [-2, 2) -> width 4, y in [-1, 1) -> height 2, start (-2, -1),
	// while OffsetX/OffsetY carry only the accumulated group pixel offset (6, 4), opacity = 0.25.
	if !ground.IsChunked() {
		t.Fatal("expected infinite Tiled layer to load as a sparse chunked layer")
	}
	if ground.StartX != -2 || ground.StartY != -1 || ground.Width != 4 || ground.Height != 2 {
		t.Fatalf("ground bounds = start(%d,%d) size %dx%d, want start(-2,-1) size 4x2",
			ground.StartX, ground.StartY, ground.Width, ground.Height)
	}
	if ground.OffsetX != 6 || ground.OffsetY != 4 || ground.Opacity != 0.25 {
		t.Fatalf("ground offset=(%g,%g) opacity=%g, want (6,4) 0.25", ground.OffsetX, ground.OffsetY, ground.Opacity)
	}
	if got := m.TileAt(0, -2, -1); got != 1 {
		t.Fatalf("TileAt(0, -2, -1) = %d, want 1", got)
	}
	if got := m.TileAt(0, 1, 0); got != 2 {
		t.Fatalf("TileAt(0, 1, 0) = %d, want 2", got)
	}

	// Verify layer draw order: sky (with repeatx) BEFORE ground tiles (at negative & positive world coords) BEFORE fog!
	cam := &tilemap.Camera{X: 0, Y: 0, Zoom: 1, Width: 128, Height: 64}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{TestID: "inf", Style: core.Style{Width: core.Pt(128), Height: core.Pt(64)}, Draw: func(d *core.Drawing, _ core.Size) {
			m.Draw(d, cam)
		}}
	})
	var uris []string
	var tileDst [][2]float32
	for _, op := range app.Find("inf").Ops {
		if op.Kind == core.DrawImage && op.Image != nil {
			uris = append(uris, op.Image.URI)
			if op.Image.URI == "asset:tiles.png" {
				tileDst = append(tileDst, [2]float32{op.X, op.Y})
			}
		}
	}
	// Expect sky repeated across [-64, 64) -> 2 draws, then 2 ground tiles, then 1 fog draw = 5 draws.
	if len(uris) != 5 || uris[0] != "asset:sky.png" || uris[1] != "asset:sky.png" ||
		uris[2] != "asset:tiles.png" || uris[3] != "asset:tiles.png" || uris[4] != "asset:fog.png" {
		t.Fatalf("draw image order = %v, want [sky, sky, tiles, tiles, fog]", uris)
	}
	// Tile 1 was at chunk (-2,-1) + group offset (6,4) = (-26, -12).
	// Tile 2 was at chunk (1, 0) + group offset (6,4) = (22, 4).
	if tileDst[0] != [2]float32{-26, -12} || tileDst[1] != [2]float32{22, 4} {
		t.Fatalf("infinite tile positions = %v, want [[-26 -12] [22 4]]", tileDst)
	}

	// Also verify Tiled hexagonal orientation metadata parsing.
	hexJSON := []byte(`{
		"width": 2, "height": 2, "tilewidth": 32, "tileheight": 36,
		"orientation": "hexagonal", "staggeraxis": "x", "staggerindex": "even", "hexsidelength": 18,
		"layers": []
	}`)
	hm, err := tilemap.LoadTiled(hexJSON, tilemap.TilesetLoader{})
	if err != nil {
		t.Fatalf("LoadTiled hex: %v", err)
	}
	if hm.Orientation != tilemap.OrientationHexagonal || hm.StaggerAxis != tilemap.StaggerX ||
		hm.StaggerIndex != tilemap.StaggerEven || hm.HexSideLength != 18 {
		t.Fatalf("hex metadata = %+v", hm)
	}
}

func TestCacheStaticLayerSingleOpFlipsAndInvalidation(t *testing.T) {
	// Create a 32x16 tileset image with 2 16x16 tiles:
	// Tile 0 (GID 1): top-left pixel RED, top-right GREEN, bottom-left BLUE, bottom-right YELLOW.
	// Tile 1 (GID 2): solid CYAN.
	sheet := image.NewRGBA(image.Rect(0, 0, 32, 16))
	red := color.RGBA{R: 255, G: 0, B: 0, A: 255}
	green := color.RGBA{R: 0, G: 255, B: 0, A: 255}
	blue := color.RGBA{R: 0, G: 0, B: 255, A: 255}
	yellow := color.RGBA{R: 255, G: 255, B: 0, A: 255}
	cyan := color.RGBA{R: 0, G: 255, B: 255, A: 255}
	for y := range 16 {
		for x := range 16 {
			switch {
			case x < 8 && y < 8:
				sheet.SetRGBA(x, y, red)
			case x >= 8 && y < 8:
				sheet.SetRGBA(x, y, green)
			case x < 8 && y >= 8:
				sheet.SetRGBA(x, y, blue)
			default:
				sheet.SetRGBA(x, y, yellow)
			}
			sheet.SetRGBA(16+x, y, cyan)
		}
	}

	imgSrc := core.ImageSource{URI: "asset:sheet.png"}
	ts := tilemap.NewGridTileset(1, imgSrc, 16, 16, 2, 2)
	m := tilemap.New(2, 2, 16, 16, ts)
	m.Layers = []tilemap.TileLayer{
		tilemap.NewTileLayer("static", 2, 2, []uint32{
			1, tilemap.FlipHorizontal | 1,
			tilemap.FlipVertical | 1, 2,
		}),
	}
	images := map[string]image.Image{"asset:sheet.png": sheet}

	// Bake static cache.
	rgba, src, err := m.CacheStaticLayer(0, images)
	if err != nil {
		t.Fatalf("CacheStaticLayer: %v", err)
	}
	if rgba.Bounds() != image.Rect(0, 0, 32, 32) || src.Pixels != rgba || src.Version != 1 {
		t.Fatalf("baked cache metadata = bounds %v version %d", rgba.Bounds(), src.Version)
	}
	// Verify baked flip pixels:
	// (0,0) unflipped -> top-left is RED.
	if got := rgba.RGBAAt(1, 1); got != red {
		t.Fatalf("tile(0,0) pixel(1,1) = %v, want red", got)
	}
	// (1,0) FlipHorizontal -> top-left (x=16) is GREEN (originally top-right).
	if got := rgba.RGBAAt(17, 1); got != green {
		t.Fatalf("tile(1,0) FlipH pixel(17,1) = %v, want green", got)
	}
	// (0,1) FlipVertical -> top-left (y=16) is BLUE (originally bottom-left).
	if got := rgba.RGBAAt(1, 17); got != blue {
		t.Fatalf("tile(0,1) FlipV pixel(1,17) = %v, want blue", got)
	}
	// (1,1) GID 2 -> CYAN.
	if got := rgba.RGBAAt(17, 17); got != cyan {
		t.Fatalf("tile(1,1) pixel(17,17) = %v, want cyan", got)
	}

	// Drawing the cached layer must emit 1 single DrawImageFrom op!
	var tick *core.Signal[int]
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		tick = core.NewSignal(cx, 0)
		return core.Canvas{TestID: "cached", Style: core.Style{Width: core.Pt(32), Height: core.Pt(32)}, Draw: func(d *core.Drawing, _ core.Size) {
			_ = tick.Get()
			m.Draw(d, nil)
		}}
	})
	ops := app.Find("cached").Ops
	if len(ops) != 1 || ops[0].Kind != core.DrawImage || ops[0].Image == nil || ops[0].Image.Pixels == nil ||
		ops[0].Image.Pixels.Bounds() != rgba.Bounds() || ops[0].Image.Pixels.RGBAAt(1, 1) != red || ops[0].Image.Version != 1 {
		t.Fatalf("cached draw ops = %+v, want 1 single DrawImageFrom using cached Pixels v1", ops)
	}

	// Modifying a tile via SetTile invalidates the cache and falls back to live 4-tile drawing!
	m.SetTile(0, 0, 0, 2)
	if lc := m.LayerCache(0); lc == nil || !lc.Dirty {
		t.Fatalf("LayerCache(0) after SetTile = %+v, want Dirty=true", lc)
	}
	app.Post(func() { tick.Set(1) })
	app.Settle()
	liveOps := app.Find("cached").Ops
	liveDraws := 0
	for _, op := range liveOps {
		if op.Kind == core.DrawImage {
			liveDraws++
		}
	}
	if liveDraws != 4 {
		t.Fatalf("after SetTile invalidation drew %d DrawImage ops (%d total ops), want 4 live tile DrawImage ops", liveDraws, len(liveOps))
	}

	// Re-caching bumps Version to 2, updates pixel (1,1) to cyan, and restores 1-op drawing.
	rgba2, src2, err := m.CacheStaticLayer(0, images)
	if err != nil {
		t.Fatalf("re-CacheStaticLayer: %v", err)
	}
	if src2.Version != 2 || rgba2.RGBAAt(1, 1) != cyan {
		t.Fatalf("re-cached version=%d pixel(1,1)=%v, want v2 cyan", src2.Version, rgba2.RGBAAt(1, 1))
	}
	app.Post(func() { tick.Set(2) })
	app.Settle()
	recachedOps := app.Find("cached").Ops
	if len(recachedOps) != 1 || recachedOps[0].Image.Version != 2 {
		t.Fatalf("re-cached draw ops = %+v, want 1 op at Version 2", recachedOps)
	}

	// Animated tile layer must fail static caching with a clear error.
	ts.Animations = map[uint32]*tilemap.TileAnimation{
		0: {Frames: []tilemap.TileAnimFrame{{TileID: 0, Duration: 100 * time.Millisecond}}},
	}
	m.SetTile(0, 0, 0, 1)
	if _, _, err := m.CacheStaticLayer(0, images); err == nil {
		t.Fatal("expected CacheStaticLayer to reject animated tile layer")
	}
}
