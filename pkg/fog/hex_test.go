package fog

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/tilemap"
)

func TestHexDiscCountsAndMatchesShadowcast(t *testing.T) {
	configs := []struct {
		name  string
		axis  tilemap.StaggerAxis
		index tilemap.StaggerIndex
	}{
		{"pointy_odd", tilemap.StaggerY, tilemap.StaggerOdd},
		{"pointy_even", tilemap.StaggerY, tilemap.StaggerEven},
		{"flat_odd", tilemap.StaggerX, tilemap.StaggerOdd},
		{"flat_even", tilemap.StaggerX, tilemap.StaggerEven},
	}

	const size = 19
	const ox, oy = 9, 9

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			m := tilemap.New(size, size, 32, 36)
			m.Orientation = tilemap.OrientationHexagonal
			m.StaggerAxis = tc.axis
			m.StaggerIndex = tc.index

			for r := range 7 {
				open := NewFromTilemap(m, 0)
				disc := fov(open, ox, oy, r)

				// Exact hex disc count is 1 + 3*r*(r+1) when completely inside the grid.
				count := 0
				for row := range size {
					for col := range size {
						seen := bit(disc, open.index(col, row))
						want := m.HexDistance(ox, oy, col, row) <= r
						if seen != want {
							t.Fatalf("r=%d cell (%d,%d): seen=%v, want HexDistance<=%d (%d)", r, col, row, seen, r, m.HexDistance(ox, oy, col, row))
						}
						if seen {
							count++
						}
					}
				}
				wantCount := 1 + 3*r*(r+1)
				if count != wantCount {
					t.Fatalf("r=%d saw %d hexes, want %d", r, count, wantCount)
				}

				// One distant opaque hex switches to hexShadowcast without obstructing anything in range.
				walled := NewFromTilemap(m, 0)
				walled.SetOpaque(0, 0, true)
				cast := fov(walled, ox, oy, r)
				clearBit(cast, 0)
				if !slices.Equal(disc, cast) {
					t.Fatalf("r=%d: hexShadowcast differs from hexDisc on open ground", r)
				}
			}
		})
	}
}

func TestHexShadowcastWallsBlockSight(t *testing.T) {
	m := tilemap.New(11, 11, 32, 36)
	m.Orientation = tilemap.OrientationHexagonal
	m.StaggerAxis = tilemap.StaggerY
	m.StaggerIndex = tilemap.StaggerOdd

	f := NewFromTilemap(m, 0)
	// Viewer at (3, 5), wall directly east at (4, 5) and neighbors around it forming a ridge.
	f.SetOpaque(4, 5, true)
	f.SetOpaque(4, 4, true)
	f.SetOpaque(4, 6, true)

	f.AddSource(Source{Team: 1, Col: 3, Row: 5, Radius: 5})
	f.Update()

	// The opaque wall hexes themselves are visible at the edge of vision.
	for _, wall := range [][2]int{{4, 5}, {4, 4}, {4, 6}} {
		if !f.Visible(1, wall[0], wall[1]) {
			t.Fatalf("opaque wall hex %v should be visible", wall)
		}
	}
	// Hexes directly behind the ridge are hidden.
	for _, hidden := range [][2]int{{5, 5}, {6, 5}, {7, 5}} {
		if f.Visible(1, hidden[0], hidden[1]) {
			t.Fatalf("hex %v behind ridge should be hidden", hidden)
		}
	}

	// Opening the middle hex of the ridge reveals hexes straight through the gap.
	f.SetOpaque(4, 5, false)
	if !f.Update() {
		t.Fatal("clearing wall did not update fog")
	}
	for _, revealed := range [][2]int{{5, 5}, {6, 5}, {7, 5}} {
		if !f.Visible(1, revealed[0], revealed[1]) {
			t.Fatalf("hex %v should be visible through opened gap", revealed)
		}
	}
}

func TestHexShadowcastSymmetricAllStaggers(t *testing.T) {
	configs := []struct {
		name  string
		axis  tilemap.StaggerAxis
		index tilemap.StaggerIndex
	}{
		{"pointy_odd", tilemap.StaggerY, tilemap.StaggerOdd},
		{"pointy_even", tilemap.StaggerY, tilemap.StaggerEven},
		{"flat_odd", tilemap.StaggerX, tilemap.StaggerOdd},
		{"flat_even", tilemap.StaggerX, tilemap.StaggerEven},
	}

	const size = 9
	// Deterministic wall pattern with pillars, corridors, and clusters.
	walls := []uint64{0x0012_4811_2484_1021, 0x0000_0000_0152_a409}

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			f := New(size, size, 32, 36)
			f.Orientation = tilemap.OrientationHexagonal
			f.StaggerAxis = tc.axis
			f.StaggerIndex = tc.index
			for i := range size * size {
				if walls[i/64]&(1<<uint(i%64)) != 0 {
					f.SetOpaque(i%size, i/size, true)
				}
			}

			for r := 1; r <= 7; r += 2 {
				allVis := make([][]uint64, size*size)
				for i := range size * size {
					c, row := i%size, i/size
					if !f.Opaque(c, row) {
						allVis[i] = fov(f, c, row, r)
					}
				}
				for a := range size * size {
					if allVis[a] == nil {
						continue
					}
					for b := a + 1; b < size*size; b++ {
						if allVis[b] == nil {
							continue
						}
						ab := bit(allVis[a], b)
						ba := bit(allVis[b], a)
						if ab != ba {
							t.Fatalf("r=%d: (%d,%d) sees (%d,%d)=%v, reverse=%v", r, a%size, a/size, b%size, b/size, ab, ba)
						}
					}
				}
			}
		})
	}
}

func FuzzHexShadowcastSymmetric(f *testing.F) {
	f.Add([]byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc}, uint8(3), uint8(40), uint8(7), uint8(0))
	f.Add([]byte{0xff, 0x00, 0x81, 0x18, 0x42, 0x24}, uint8(0), uint8(99), uint8(9), uint8(3))
	f.Fuzz(func(t *testing.T, walls []byte, a, b, r, cfg uint8) {
		const size = 12
		m := New(size, size, 32, 36)
		m.Orientation = tilemap.OrientationHexagonal
		if cfg&1 != 0 {
			m.StaggerAxis = tilemap.StaggerX
		}
		if cfg&2 != 0 {
			m.StaggerIndex = tilemap.StaggerEven
		}
		for i := range size * size {
			if i/8 < len(walls) && walls[i/8]&(1<<(i%8)) != 0 {
				m.SetOpaque(i%size, i/size, true)
			}
		}
		ax, ay := int(a)%size, int(a/size)%size
		bx, by := int(b)%size, int(b/size)%size
		if m.Opaque(ax, ay) || m.Opaque(bx, by) {
			return
		}
		radius := int(r % 14)
		fromA := bit(fov(m, ax, ay, radius), m.index(bx, by))
		fromB := bit(fov(m, bx, by, radius), m.index(ax, ay))
		if fromA != fromB {
			t.Fatalf("cfg=%d r=%d: (%d,%d) sees (%d,%d)=%v, reverse=%v", cfg&3, radius, ax, ay, bx, by, fromA, fromB)
		}
	})
}

func TestStaggeredShadowcastDiscAndSymmetry(t *testing.T) {
	configs := []struct {
		name  string
		axis  tilemap.StaggerAxis
		index tilemap.StaggerIndex
	}{
		{"y_odd", tilemap.StaggerY, tilemap.StaggerOdd},
		{"y_even", tilemap.StaggerY, tilemap.StaggerEven},
		{"x_odd", tilemap.StaggerX, tilemap.StaggerOdd},
		{"x_even", tilemap.StaggerX, tilemap.StaggerEven},
	}

	const size = 11
	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			open := New(size, size, 64, 32)
			open.Orientation = tilemap.OrientationStaggered
			open.StaggerAxis = tc.axis
			open.StaggerIndex = tc.index

			walled := New(size, size, 64, 32)
			walled.Orientation = tilemap.OrientationStaggered
			walled.StaggerAxis = tc.axis
			walled.StaggerIndex = tc.index
			walled.SetOpaque(0, 0, true)

			for r := range 4 {
				disc := fov(open, 5, 5, r)
				cast := fov(walled, 5, 5, r)
				clearBit(cast, 0)
				if !slices.Equal(disc, cast) {
					t.Fatalf("r=%d: staggeredShadowcast differs from staggeredDisc", r)
				}
			}

			// Symmetry with obstacles.
			for _, w := range [][2]int{{4, 5}, {5, 4}, {6, 5}, {3, 3}, {7, 6}} {
				walled.SetOpaque(w[0], w[1], true)
			}
			for a := range size * size {
				ax, ay := a%size, a/size
				if walled.Opaque(ax, ay) {
					continue
				}
				visA := fov(walled, ax, ay, 5)
				for b := a + 1; b < size*size; b++ {
					bx, by := b%size, b/size
					if walled.Opaque(bx, by) {
						continue
					}
					visB := fov(walled, bx, by, 5)
					if bit(visA, b) != bit(visB, a) {
						t.Fatalf("(%d,%d) sees (%d,%d)=%v, reverse=%v", ax, ay, bx, by, bit(visA, b), bit(visB, a))
					}
				}
			}
		})
	}
}

func TestProjectedCellAtAndSnapshotRoundTrip(t *testing.T) {
	configs := []struct {
		name        string
		orientation tilemap.Orientation
		axis        tilemap.StaggerAxis
		index       tilemap.StaggerIndex
		tw, th      float32
		side        float32
	}{
		{"isometric", tilemap.OrientationIsometric, tilemap.StaggerY, tilemap.StaggerOdd, 64, 32, 0},
		{"staggered_y_odd", tilemap.OrientationStaggered, tilemap.StaggerY, tilemap.StaggerOdd, 64, 32, 0},
		{"staggered_x_even", tilemap.OrientationStaggered, tilemap.StaggerX, tilemap.StaggerEven, 64, 32, 0},
		{"hex_pointy_odd", tilemap.OrientationHexagonal, tilemap.StaggerY, tilemap.StaggerOdd, 32, 36, 18},
		{"hex_flat_even", tilemap.OrientationHexagonal, tilemap.StaggerX, tilemap.StaggerEven, 36, 32, 18},
	}

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			m := tilemap.New(6, 6, tc.tw, tc.th)
			m.Orientation = tc.orientation
			m.StaggerAxis = tc.axis
			m.StaggerIndex = tc.index
			m.HexSideLength = tc.side
			m.SetCellFlags(3, 2, tilemap.TileSolid)

			f := NewFromTilemap(m, tilemap.TileSolid)
			if f.Orientation != tc.orientation || f.StaggerAxis != tc.axis || f.StaggerIndex != tc.index || f.HexSideLength != tc.side {
				t.Fatalf("NewFromTilemap did not copy projection fields")
			}

			// CellAt and MoveTo round-trip every tile center.
			scout := f.AddSource(Source{Team: 1, Col: 0, Row: 0, Radius: 2})
			for row := range 6 {
				for col := range 6 {
					pt := m.TileToWorld(col, row)
					gc, gr, ok := f.CellAt(pt)
					if !ok || gc != col || gr != row {
						t.Fatalf("CellAt(%+v) = (%d,%d,%v), want (%d,%d,true)", pt, gc, gr, ok, col, row)
					}
				}
			}
			target := m.TileToWorld(2, 2)
			f.MoveTo(scout, target)
			f.Update()
			if !f.VisibleAt(1, target) {
				t.Fatal("scout cell not visible at its world center")
			}

			// Snapshot round-trip preserves orientation, stagger, hex side length, and exact visibility.
			raw, err := json.Marshal(f)
			if err != nil {
				t.Fatalf("MarshalJSON: %v", err)
			}
			var loaded Fog
			if err := json.Unmarshal(raw, &loaded); err != nil {
				t.Fatalf("UnmarshalJSON: %v\n%s", err, raw)
			}
			again, err := json.Marshal(&loaded)
			if err != nil {
				t.Fatalf("second MarshalJSON: %v", err)
			}
			if !bytes.Equal(raw, again) {
				t.Fatalf("snapshot not stable:\n%s\n%s", raw, again)
			}
			for row := range 6 {
				for col := range 6 {
					if loaded.State(1, col, row) != f.State(1, col, row) {
						t.Fatalf("cell (%d,%d) state after load = %v, want %v", col, row, loaded.State(1, col, row), f.State(1, col, row))
					}
				}
			}
		})
	}

	// Invalid projection fields are rejected with ErrInvalidSnapshot.
	bad := []string{
		`{"version":1,"width":2,"height":2,"tileWidth":16,"tileHeight":16,"orientation":"polar","nextSource":0}`,
		`{"version":1,"width":2,"height":2,"tileWidth":16,"tileHeight":16,"orientation":"hexagonal","staggerAxis":"z","nextSource":0}`,
		`{"version":1,"width":2,"height":2,"tileWidth":16,"tileHeight":16,"orientation":"hexagonal","staggerIndex":"third","nextSource":0}`,
		`{"version":1,"width":2,"height":2,"tileWidth":16,"tileHeight":16,"orientation":"hexagonal","hexSideLength":-4,"nextSource":0}`,
	}
	for _, raw := range bad {
		var f Fog
		if err := json.Unmarshal([]byte(raw), &f); !errors.Is(err, ErrInvalidSnapshot) {
			t.Fatalf("Unmarshal(%s) err = %v, want ErrInvalidSnapshot", raw, err)
		}
	}
}
