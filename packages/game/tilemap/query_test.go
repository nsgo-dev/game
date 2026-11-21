package tilemap_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	tileHazard tilemap.TileFlags = 1 << 2
	tileWater  tilemap.TileFlags = 1 << 3
)

func makeQueryMap() *tilemap.Tilemap {
	// 5x5 map of 16x16 tiles ([0, 80) x [0, 80)).
	// Tile 1 = TileSolid
	// Tile 2 = TileOneWay
	// Tile 3 = tileHazard
	// Tile 4 = TileSolid | tileHazard
	m := tilemap.New(5, 5, 16, 16)
	m.SetFlags(1, tilemap.TileSolid)
	m.SetFlags(2, tilemap.TileOneWay)
	m.SetFlags(3, tileHazard)
	m.SetFlags(tilemap.FlipHorizontal|4, tilemap.TileSolid|tileHazard)

	// Layer 0 layout (5x5):
	// row 0: 0 0 1 0 0
	// row 1: 0 2 0 1 0
	// row 2: 1 0 0 0 3
	// row 3: 0 0 4 0 0
	// row 4: 0 0 0 0 1
	m.Layers = []tilemap.TileLayer{
		tilemap.NewTileLayer("base", 5, 5, []uint32{
			0, 0, 1, 0, 0,
			0, 2, 0, 1, 0,
			1, 0, 0, 0, 3,
			0, 0, tilemap.FlipVertical | 4, 0, 0,
			0, 0, 0, 0, 1,
		}),
	}
	return m
}

func TestTileFlagsAndCellOverrides(t *testing.T) {
	m := makeQueryMap()
	// Add a second layer placing a water tile on top of the solid tile at (2, 0).
	m.SetFlags(5, tileWater)
	overlay := tilemap.NewTileLayer("overlay", 5, 5, nil)
	overlay.Data[0*5+2] = 5
	m.Layers = append(m.Layers, overlay)

	// Combined flags across layers.
	if got := m.FlagsAt(2, 0); got != tilemap.TileSolid|tileWater {
		t.Fatalf("FlagsAt(2,0) = %v, want TileSolid|tileWater", got)
	}
	// Flipped tile GID still inherits its tile ID's flags.
	if got := m.FlagsAt(2, 3); got != tilemap.TileSolid|tileHazard {
		t.Fatalf("FlagsAt(2,3) = %v, want TileSolid|tileHazard", got)
	}
	if got := m.FlagsAt(1, 1); got != tilemap.TileOneWay {
		t.Fatalf("FlagsAt(1,1) = %v, want TileOneWay", got)
	}
	if got := m.FlagsAt(4, 2); got != tileHazard {
		t.Fatalf("FlagsAt(4,2) = %v, want tileHazard", got)
	}
	// Out of bounds returns 0.
	if got := m.FlagsAt(-1, 0); got != 0 {
		t.Fatalf("FlagsAt(-1,0) = %v, want 0", got)
	}
	if got := m.FlagsAt(5, 0); got != 0 {
		t.Fatalf("FlagsAt(5,0) = %v, want 0", got)
	}

	// Cell override takes precedence, including clearing a solid cell to 0 or marking an empty cell solid.
	m.SetCellFlags(2, 0, 0)
	if got := m.FlagsAt(2, 0); got != 0 {
		t.Fatalf("after SetCellFlags(2,0,0): FlagsAt = %v, want 0", got)
	}
	m.ClearCellFlags(2, 0)
	if got := m.FlagsAt(2, 0); got != tilemap.TileSolid|tileWater {
		t.Fatalf("after ClearCellFlags(2,0): FlagsAt = %v, want TileSolid|tileWater", got)
	}
	m.SetCellFlags(0, 0, tilemap.TileSolid)
	if got := m.FlagsAt(0, 0); got != tilemap.TileSolid {
		t.Fatalf("empty cell override FlagsAt(0,0) = %v, want TileSolid", got)
	}
}

func TestTilesInRectEdgesCornersNegativeAndOutOfBounds(t *testing.T) {
	// 4x4 fully populated solid map ([0, 64) x [0, 64)).
	m := tilemap.New(4, 4, 16, 16)
	m.SetFlags(1, tilemap.TileSolid)
	m.SetFlags(2, tilemap.TileOneWay)
	data := make([]uint32, 16)
	for i := range data {
		data[i] = 1
	}
	data[1*4+1] = 2 // (1, 1) is one-way
	m.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("grid", 4, 4, data)}

	type cell struct{ col, row int }
	gotCells := func(hits []tilemap.TileHit) []cell {
		out := make([]cell, len(hits))
		for i, h := range hits {
			out[i] = cell{h.Col, h.Row}
		}
		return out
	}
	assertCells := func(name string, got []tilemap.TileHit, want []cell) {
		t.Helper()
		gc := gotCells(got)
		if len(gc) != len(want) {
			t.Fatalf("%s: got %v, want %v", name, gc, want)
		}
		for i := range want {
			if gc[i] != want[i] {
				t.Fatalf("%s: got %v, want %v", name, gc, want)
			}
		}
	}

	// Exact single tile [16, 32) x [16, 32) must NOT include adjacent edge/corner tiles.
	exactOne := m.TilesInRect(core.Bounds{
		Origin: core.Point{X: 16, Y: 16},
		Size:   core.Size{Width: 16, Height: 16},
	}, 0)
	assertCells("exact single tile", exactOne, []cell{{1, 1}})
	wantBounds := core.Bounds{Origin: core.Point{X: 16, Y: 16}, Size: core.Size{Width: 16, Height: 16}}
	if exactOne[0].Bounds != wantBounds || exactOne[0].Flags != tilemap.TileOneWay || exactOne[0].TileID != 2 {
		t.Fatalf("exact single tile hit = %+v, want bounds=%+v oneway id=2", exactOne[0], wantBounds)
	}

	// Exact single tile filtered by TileSolid misses the TileOneWay cell at (1, 1).
	assertCells("exact single tile solid filter", m.TilesInRect(wantBounds, tilemap.TileSolid), nil)

	// Spanning corner (16, 16) hits all 4 surrounding tiles in row-major order.
	cornerHits := m.TilesInRect(core.Bounds{
		Origin: core.Point{X: 15, Y: 15},
		Size:   core.Size{Width: 2, Height: 2},
	}, 0)
	assertCells("spanning corner (16,16)", cornerHits, []cell{{0, 0}, {1, 0}, {0, 1}, {1, 1}})

	// Spanning corner filtered to TileSolid skips (1, 1).
	cornerSolid := m.TilesInRect(core.Bounds{
		Origin: core.Point{X: 15, Y: 15},
		Size:   core.Size{Width: 2, Height: 2},
	}, tilemap.TileSolid)
	assertCells("spanning corner solid", cornerSolid, []cell{{0, 0}, {1, 0}, {0, 1}})

	// Touching outer left edge [-16, 0) x [0, 16) -> empty.
	assertCells("touching left edge outside", m.TilesInRect(core.Bounds{
		Origin: core.Point{X: -16, Y: 0},
		Size:   core.Size{Width: 16, Height: 16},
	}, 0), nil)

	// Touching outer right edge [64, 80) x [0, 16) -> empty.
	assertCells("touching right edge outside", m.TilesInRect(core.Bounds{
		Origin: core.Point{X: 64, Y: 0},
		Size:   core.Size{Width: 16, Height: 16},
	}, 0), nil)

	// Completely negative rect -> empty.
	assertCells("completely negative", m.TilesInRect(core.Bounds{
		Origin: core.Point{X: -64, Y: -64},
		Size:   core.Size{Width: 32, Height: 32},
	}, 0), nil)

	// Negative coords overlapping top-left tile [-8, 8) x [-8, 8) -> (0, 0).
	assertCells("partial negative overlap", m.TilesInRect(core.Bounds{
		Origin: core.Point{X: -8, Y: -8},
		Size:   core.Size{Width: 16, Height: 16},
	}, 0), []cell{{0, 0}})

	// Extending past bottom-right [56, 120) x [56, 120) -> (3, 3).
	assertCells("partial bottom-right overflow", m.TilesInRect(core.Bounds{
		Origin: core.Point{X: 56, Y: 56},
		Size:   core.Size{Width: 64, Height: 64},
	}, 0), []cell{{3, 3}})

	// Zero-size rect returns nil.
	assertCells("zero width", m.TilesInRect(core.Bounds{
		Origin: core.Point{X: 8, Y: 8},
		Size:   core.Size{Width: 0, Height: 16},
	}, 0), nil)
}

func TestRaycastSolidOneWayNormalsFractionsAndMisses(t *testing.T) {
	m := makeQueryMap()

	checkHit := func(name string, from, to core.Point, mask tilemap.TileFlags,
		wantCol, wantRow int, wantID uint32, wantFlags tilemap.TileFlags,
		wantPt, wantNorm core.Point, wantT float32) {
		t.Helper()
		hit, ok := m.Raycast(from, to, mask)
		if !ok {
			t.Fatalf("%s: expected hit, got miss", name)
		}
		if hit.Col != wantCol || hit.Row != wantRow || hit.TileID != wantID || hit.Flags != wantFlags {
			t.Fatalf("%s: cell=(%d,%d) id=%d flags=%v, want (%d,%d) id=%d flags=%v",
				name, hit.Col, hit.Row, hit.TileID, hit.Flags, wantCol, wantRow, wantID, wantFlags)
		}
		if !nearPt(hit.Point, wantPt, 1e-3) {
			t.Fatalf("%s: point=%+v, want %+v", name, hit.Point, wantPt)
		}
		if !nearPt(hit.Normal, wantNorm, 1e-3) {
			t.Fatalf("%s: normal=%+v, want %+v", name, hit.Normal, wantNorm)
		}
		if math.Abs(float64(hit.T-wantT)) > 1e-4 {
			t.Fatalf("%s: T=%g, want %g", name, hit.T, wantT)
		}
	}

	// Horizontal left-to-right hitting solid tile (2, 0) at x=32.
	checkHit("horizontal L->R solid",
		core.Point{X: 8, Y: 8}, core.Point{X: 56, Y: 8}, tilemap.TileSolid,
		2, 0, 1, tilemap.TileSolid,
		core.Point{X: 32, Y: 8}, core.Point{X: -1, Y: 0}, 0.5)

	// Horizontal right-to-left hitting solid tile (2, 0) at x=48.
	checkHit("horizontal R->L solid",
		core.Point{X: 72, Y: 8}, core.Point{X: 24, Y: 8}, tilemap.TileSolid,
		2, 0, 1, tilemap.TileSolid,
		core.Point{X: 48, Y: 8}, core.Point{X: 1, Y: 0}, 0.5)

	// Vertical top-to-bottom hitting one-way platform (1, 1) at y=16.
	checkHit("vertical T->B one-way",
		core.Point{X: 24, Y: 0}, core.Point{X: 24, Y: 64}, tilemap.TileOneWay,
		1, 1, 2, tilemap.TileOneWay,
		core.Point{X: 24, Y: 16}, core.Point{X: 0, Y: -1}, 0.25)

	// Same vertical ray with TileSolid passes through (1, 1) one-way and misses completely.
	if hit, ok := m.Raycast(core.Point{X: 24, Y: 0}, core.Point{X: 24, Y: 64}, tilemap.TileSolid); ok {
		t.Fatalf("TileSolid ray unexpectedly hit %+v through one-way platform", hit)
	}

	// Vertical bottom-to-top hitting solid+hazard tile (2, 3) at y=64.
	checkHit("vertical B->T solid+hazard",
		core.Point{X: 40, Y: 80}, core.Point{X: 40, Y: 32}, tilemap.TileSolid,
		2, 3, 4, tilemap.TileSolid|tileHazard,
		core.Point{X: 40, Y: 64}, core.Point{X: 0, Y: 1}, float32(16.0/48.0))

	// Diagonal ray from (0, 8) to (64, 40) (slope 1/2): crosses x=16 at y=16 (entering (1,1) one-way)
	// and with TileSolid continues to x=48, y=32? Let's trace: at x=48, y=32 (just below (3,1) [48,64)x[16,32)).
	// Aim directly at (3, 1) left face: from (8, 8) to (56, 32) -> at x=48, t=40/48=5/6, y=8+24*5/6=28 in [16,32).
	checkHit("diagonal hits left face of (3,1)",
		core.Point{X: 8, Y: 8}, core.Point{X: 56, Y: 32}, tilemap.TileSolid,
		3, 1, 1, tilemap.TileSolid,
		core.Point{X: 48, Y: 28}, core.Point{X: -1, Y: 0}, float32(40.0/48.0))

	// Custom flag filter hits hazard tile (4, 2) at x=64, ignoring solid tile (0, 2) behind/on other side.
	checkHit("custom flag hazard",
		core.Point{X: 32, Y: 40}, core.Point{X: 80, Y: 40}, tileHazard,
		4, 2, 3, tileHazard,
		core.Point{X: 64, Y: 40}, core.Point{X: -1, Y: 0}, float32(32.0/48.0))

	// Ray entering map from negative X hits solid border tile (0, 2) at x=0.
	checkHit("enter map from negative X",
		core.Point{X: -16, Y: 40}, core.Point{X: 16, Y: 40}, tilemap.TileSolid,
		0, 2, 1, tilemap.TileSolid,
		core.Point{X: 0, Y: 40}, core.Point{X: -1, Y: 0}, 0.5)

	// Ray starting on the exact right face of solid (0, 2) moving right into empty (1, 2) must MISS.
	if hit, ok := m.Raycast(core.Point{X: 16, Y: 40}, core.Point{X: 48, Y: 40}, tilemap.TileSolid); ok {
		t.Fatalf("ray moving away from solid boundary spuriously hit %+v", hit)
	}

	// Ray starting on exact right face of solid (0, 2) moving left INTO (0, 2) hits at T=0 with Normal=(+1,0).
	checkHit("start on face moving into solid",
		core.Point{X: 16, Y: 40}, core.Point{X: 0, Y: 40}, tilemap.TileSolid,
		0, 2, 1, tilemap.TileSolid,
		core.Point{X: 16, Y: 40}, core.Point{X: 1, Y: 0}, 0)

	// Ray ending exactly on solid face hits at T=1.
	checkHit("end exactly on face",
		core.Point{X: 16, Y: 8}, core.Point{X: 32, Y: 8}, tilemap.TileSolid,
		2, 0, 1, tilemap.TileSolid,
		core.Point{X: 32, Y: 8}, core.Point{X: -1, Y: 0}, 1)

	// Misses: too short, completely outside map, and grazing outer corner without entering interior.
	for _, tc := range []struct {
		name     string
		from, to core.Point
	}{
		{"segment stops before wall", core.Point{X: 8, Y: 8}, core.Point{X: 31, Y: 8}},
		{"completely outside map", core.Point{X: -40, Y: -40}, core.Point{X: -10, Y: -10}},
		{"above map", core.Point{X: 0, Y: -8}, core.Point{X: 80, Y: -8}},
		{"zero length on empty cell", core.Point{X: 8, Y: 8}, core.Point{X: 8, Y: 8}},
	} {
		if hit, ok := m.Raycast(tc.from, tc.to, tilemap.TileSolid); ok {
			t.Fatalf("%s: expected miss, got %+v", tc.name, hit)
		}
	}

	// Grazing corner (0, 0) of a map where (0, 0) is solid must MISS.
	cornerMap := tilemap.New(2, 2, 16, 16)
	cornerMap.SetFlags(1, tilemap.TileSolid)
	cornerMap.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("g", 2, 2, []uint32{
		1, 0,
		0, 1,
	})}
	if hit, ok := cornerMap.Raycast(core.Point{X: -16, Y: 16}, core.Point{X: 16, Y: -16}, tilemap.TileSolid); ok {
		t.Fatalf("grazing outer corner unexpectedly hit %+v", hit)
	}

	// Exact corner crossing into diagonal tile (1, 1) from empty (0, 0) gives diagonal normal.
	diagMap := tilemap.New(2, 2, 16, 16)
	diagMap.SetFlags(1, tilemap.TileSolid)
	diagMap.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("g", 2, 2, []uint32{
		0, 0,
		0, 1,
	})}
	inv := core.Points(-float32(1 / math.Sqrt2))
	hit, ok := diagMap.Raycast(core.Point{X: 0, Y: 0}, core.Point{X: 32, Y: 32}, tilemap.TileSolid)
	if !ok || hit.Col != 1 || hit.Row != 1 || !nearPt(hit.Point, core.Point{X: 16, Y: 16}, 1e-3) ||
		!nearPt(hit.Normal, core.Point{X: inv, Y: inv}, 1e-3) || math.Abs(float64(hit.T-0.5)) > 1e-4 {
		t.Fatalf("diagonal corner hit = %+v (ok=%v), want (1,1) at (16,16) norm (%g,%g) T=0.5", hit, ok, inv, inv)
	}

	// Diagonal wall (both orthogonal neighbors solid) blocks diagonal ray instead of tunneling through seam.
	seamMap := tilemap.New(2, 2, 16, 16)
	seamMap.SetFlags(1, tilemap.TileSolid)
	seamMap.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("g", 2, 2, []uint32{
		0, 1,
		1, 0,
	})}
	if hit, ok := seamMap.Raycast(core.Point{X: 0, Y: 0}, core.Point{X: 32, Y: 32}, tilemap.TileSolid); !ok || !nearPt(hit.Point, core.Point{X: 16, Y: 16}, 1e-3) {
		t.Fatalf("diagonal seam should block at (16,16), got hit=%+v ok=%v", hit, ok)
	}
}
