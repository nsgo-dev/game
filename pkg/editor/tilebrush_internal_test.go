package editor

import (
	"slices"
	"testing"
)

func TestBresenhamLineCases(t *testing.T) {
	collect := func(x0, y0, x1, y1 int) []cell {
		var out []cell
		bresenhamLine(x0, y0, x1, y1, func(c, r int) { out = append(out, cell{c, r}) })
		return out
	}
	for _, tc := range []struct {
		name           string
		x0, y0, x1, y1 int
		want           []cell
	}{
		{"single cell", 2, 2, 2, 2, []cell{{2, 2}}},
		{"horizontal", 0, 1, 3, 1, []cell{{0, 1}, {1, 1}, {2, 1}, {3, 1}}},
		{"horizontal reverse", 3, 1, 0, 1, []cell{{3, 1}, {2, 1}, {1, 1}, {0, 1}}},
		{"vertical", 2, 0, 2, 3, []cell{{2, 0}, {2, 1}, {2, 2}, {2, 3}}},
		{"diagonal 45", 0, 0, 3, 3, []cell{{0, 0}, {1, 1}, {2, 2}, {3, 3}}},
		{"anti-diagonal", 3, 0, 0, 3, []cell{{3, 0}, {2, 1}, {1, 2}, {0, 3}}},
		{"shallow slope", 0, 0, 4, 1, []cell{{0, 0}, {1, 0}, {2, 0}, {3, 1}, {4, 1}}},
		{"steep slope", 0, 0, 1, 4, []cell{{0, 0}, {0, 1}, {0, 2}, {1, 3}, {1, 4}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := collect(tc.x0, tc.y0, tc.x1, tc.y1)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("bresenhamLine(%d,%d -> %d,%d) = %v, want %v", tc.x0, tc.y0, tc.x1, tc.y1, got, tc.want)
			}
		})
	}
}

func TestBresenhamLineClippedBoundsExtremeCoordinates(t *testing.T) {
	var got []cell
	bresenhamLineClipped(-1000000, 1, 1000000, 1, 4, 4, func(c, r int) {
		got = append(got, cell{c, r})
	})
	want := []cell{{0, 1}, {1, 1}, {2, 1}, {3, 1}}
	if !slices.Equal(got, want) {
		t.Fatalf("clipped horizontal line = %v, want %v", got, want)
	}
	var outside []cell
	bresenhamLineClipped(-10, -10, -2, -2, 4, 4, func(c, r int) {
		outside = append(outside, cell{c, r})
	})
	if len(outside) != 0 {
		t.Fatalf("wholly outside line visited %v, want empty", outside)
	}
}

func TestRandomPickTileDeterminismAndWeights(t *testing.T) {
	pool := []WeightedTile{
		{TileID: 1, Weight: 9},
		{TileID: 2, Weight: 1},
	}
	// Fixed seed produces identical results on repeated calls.
	for r := range 10 {
		for c := range 10 {
			a := randomPickTile(12345, c, r, pool)
			b := randomPickTile(12345, c, r, pool)
			if a != b {
				t.Fatalf("non-deterministic pick at (%d,%d): %d vs %d", c, r, a, b)
			}
		}
	}
	// Weighted distribution over 400 cells reflects 9:1 weights (tile 1 dominates, tile 2 appears).
	counts := map[int]int{}
	for r := range 20 {
		for c := range 20 {
			counts[randomPickTile(77, c, r, pool)]++
		}
	}
	if counts[1] < 320 || counts[1] > 395 || counts[2] < 5 || counts[1]+counts[2] != 400 {
		t.Fatalf("weighted counts for 9:1 over 400 cells = %v, want ~360 vs ~40", counts)
	}
	// Different seeds yield different layouts.
	same := 0
	equalPool := []WeightedTile{{TileID: 1, Weight: 1}, {TileID: 2, Weight: 1}, {TileID: 3, Weight: 1}}
	for r := range 6 {
		for c := range 6 {
			if randomPickTile(1, c, r, equalPool) == randomPickTile(2, c, r, equalPool) {
				same++
			}
		}
	}
	if same == 36 {
		t.Fatal("different seeds produced identical 6x6 patterns")
	}
}
