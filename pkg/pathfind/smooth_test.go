package pathfind_test

import (
	"math"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/pathfind"
	"github.com/nsgo-dev/nsgo/core"
)

func gridFromASCII(rows []string) *pathfind.BoolGrid {
	h := len(rows)
	w := len(rows[0])
	g := pathfind.NewBoolGrid(w, h)
	for r, line := range rows {
		for c := range w {
			if line[c] == '#' {
				g.SetWalkable(c, r, false)
			}
		}
	}
	return g
}

// independentPointBoxDist computes the Euclidean distance from (px, py) to closed box [minX, maxX] x [minY, maxY].
func independentPointBoxDist(px, py, minX, minY, maxX, maxY float64) float64 {
	var dx, dy float64
	if px < minX {
		dx = minX - px
	} else if px > maxX {
		dx = px - maxX
	}
	if py < minY {
		dy = minY - py
	} else if py > maxY {
		dy = py - maxY
	}
	return math.Hypot(dx, dy)
}

// independentSegmentBoxDist computes exact Euclidean distance between segment ab and closed box [minX, maxX] x [minY, maxY]
// independently from pathfind's internal helpers, and cross-checks dense samples along the segment.
func independentSegmentBoxDist(a, b core.Point, minX, minY, maxX, maxY float64) float64 {
	ax, ay := float64(a.X), float64(a.Y)
	bx, by := float64(b.X), float64(b.Y)

	// Check slab intersection first.
	t0, t1 := 0.0, 1.0
	dx, dy := bx-ax, by-ay
	intersects := true
	if dx == 0 {
		if ax < minX || ax > maxX {
			intersects = false
		}
	} else {
		tx0 := (minX - ax) / dx
		tx1 := (maxX - ax) / dx
		if tx0 > tx1 {
			tx0, tx1 = tx1, tx0
		}
		t0 = max(t0, tx0)
		t1 = min(t1, tx1)
		if t0 > t1 {
			intersects = false
		}
	}
	if intersects {
		if dy == 0 {
			if ay < minY || ay > maxY {
				intersects = false
			}
		} else {
			ty0 := (minY - ay) / dy
			ty1 := (maxY - ay) / dy
			if ty0 > ty1 {
				ty0, ty1 = ty1, ty0
			}
			t0 = max(t0, ty0)
			t1 = min(t1, ty1)
			if t0 > t1 {
				intersects = false
			}
		}
	}
	if intersects {
		return 0
	}

	best := independentPointBoxDist(ax, ay, minX, minY, maxX, maxY)
	best = min(best, independentPointBoxDist(bx, by, minX, minY, maxX, maxY))

	corners := [4][2]float64{
		{minX, minY},
		{maxX, minY},
		{maxX, maxY},
		{minX, maxY},
	}
	l2 := dx*dx + dy*dy
	for _, corner := range corners {
		var t float64
		if l2 > 0 {
			t = ((corner[0]-ax)*dx + (corner[1]-ay)*dy) / l2
			t = min(max(t, 0), 1)
		}
		px := ax + t*dx
		py := ay + t*dy
		best = min(best, math.Hypot(corner[0]-px, corner[1]-py))
	}

	// Dense sample verification: no sampled point along the segment may be closer than exact best.
	const samples = 64
	for s := range samples + 1 {
		t := float64(s) / samples
		px := ax + t*dx
		py := ay + t*dy
		d := independentPointBoxDist(px, py, minX, minY, maxX, maxY)
		best = min(best, d)
	}
	return best
}

func assertSmoothedClearance(t *testing.T, g pathfind.Grid, pts []core.Point, opts pathfind.GridPathOptions) {
	t.Helper()
	cols, rows := g.GridSize()
	w := float64(opts.CellWidth)
	if w <= 0 {
		w = float64(opts.CellSize)
	}
	if w <= 0 {
		w = 1
	}
	h := float64(opts.CellHeight)
	if h <= 0 {
		h = float64(opts.CellSize)
	}
	if h <= 0 {
		h = 1
	}
	r := float64(opts.Radius)
	if r < 0 {
		r = 0
	}
	ox, oy := float64(opts.Origin.X), float64(opts.Origin.Y)

	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		for row := range rows {
			for col := range cols {
				if g.Walkable(col, row) {
					continue
				}
				x0 := ox + float64(col)*w
				y0 := oy + float64(row)*h
				d := independentSegmentBoxDist(a, b, x0, y0, x0+w, y0+h)
				if r == 0 {
					if d <= 1e-7 {
						t.Fatalf("segment %d (%v -> %v) entered/grazed blocked cell (%d,%d) box [%.2f..%.2f, %.2f..%.2f] at dist=%g",
							i, a, b, col, row, x0, x0+w, y0, y0+h, d)
					}
				} else if d < r-1e-6 {
					t.Fatalf("segment %d (%v -> %v) passed within radius %.4f of blocked cell (%d,%d): dist=%.6f",
						i, a, b, r, col, row, d)
				}
			}
		}
	}
}

func TestSmoothGridPathOpenGroundCollapsesDiagonal(t *testing.T) {
	g := pathfind.NewBoolGrid(16, 12)
	start := pathfind.Cell{Col: 1, Row: 1}
	goal := pathfind.Cell{Col: 14, Row: 9}
	opts := pathfind.GridPathOptions{CellSize: 16, Radius: 5}

	for _, diag := range []bool{false, true} {
		a := pathfind.NewAStar()
		a.Diagonal = diag
		raw, _, ok := a.Find(g, start, goal)
		if !ok {
			t.Fatalf("diag=%v: expected path", diag)
		}
		if len(raw) < 10 {
			t.Fatalf("diag=%v: expected staircase path, got len=%d", diag, len(raw))
		}

		smoothed := pathfind.SmoothGridPath(g, raw, opts)
		want := []core.Point{opts.CellCenter(start), opts.CellCenter(goal)}
		if !slices.Equal(smoothed, want) {
			t.Fatalf("diag=%v: smoothed = %v, want single straight segment %v", diag, smoothed, want)
		}
	}
}

func TestSmoothGridPathNeverViolatesRadiusAroundWalls(t *testing.T) {
	cases := []struct {
		name        string
		grid        []string
		start       pathfind.Cell
		goal        pathfind.Cell
		diagonal    bool
		opts        pathfind.GridPathOptions
		wantLenEq   int // >0 checks exact waypoint count against reference
		wantShorter bool
	}{
		{
			name: "single_wall_corner_radius_5_keeps_corner_clearance",
			// Path from (0,1) to (2,2) around blocked (2,1).
			// Segment (0,1)->(2,2) passes at dist 8/sqrt(5) ~= 3.578 from (2,1), so radius 5 must keep (1,2).
			grid: []string{
				"....",
				"..#.",
				"....",
			},
			start:     pathfind.Cell{Col: 0, Row: 1},
			goal:      pathfind.Cell{Col: 2, Row: 2},
			opts:      pathfind.GridPathOptions{CellSize: 16, Radius: 5},
			wantLenEq: 3,
		},
		{
			name: "single_wall_corner_radius_3_clears_straight",
			// Same geometry, but radius 3 < 3.578 clears (2,1) directly in one straight segment.
			grid: []string{
				"....",
				"..#.",
				"....",
			},
			start:     pathfind.Cell{Col: 0, Row: 1},
			goal:      pathfind.Cell{Col: 2, Row: 2},
			opts:      pathfind.GridPathOptions{CellSize: 16, Radius: 3},
			wantLenEq: 2,
		},
		{
			name: "l_corridor_radius_half_cell",
			// Narrow 1-cell L corridor with radius == CellSize/2 (8.0): collapses straight runs to 3 waypoints.
			grid: []string{
				"#####",
				"#...#",
				"###.#",
				"###.#",
				"#####",
			},
			start:     pathfind.Cell{Col: 1, Row: 1},
			goal:      pathfind.Cell{Col: 3, Row: 3},
			opts:      pathfind.GridPathOptions{CellSize: 16, Radius: 8},
			wantLenEq: 3,
		},
		{
			name: "u_detour_around_vertical_wall",
			grid: []string{
				".........",
				"...#.....",
				"...#.....",
				"...#.....",
				"...#.....",
				".........",
			},
			start:       pathfind.Cell{Col: 1, Row: 3},
			goal:        pathfind.Cell{Col: 7, Row: 3},
			opts:        pathfind.GridPathOptions{CellSize: 16, Radius: 6},
			wantShorter: true,
		},
		{
			name: "pillars_and_doorways_nonzero_origin",
			grid: []string{
				"############",
				"#....#.....#",
				"#.#..#..#..#",
				"#....#.....#",
				"#....#..#..#",
				"#........#.#",
				"#..#.......#",
				"############",
			},
			start:       pathfind.Cell{Col: 1, Row: 1},
			goal:        pathfind.Cell{Col: 10, Row: 1},
			diagonal:    true,
			opts:        pathfind.GridPathOptions{Origin: core.Point{X: -64, Y: 120}, CellSize: 16, Radius: 5.5},
			wantShorter: true,
		},
		{
			name: "zigzag_narrow_corridor_zero_radius",
			grid: []string{
				"########",
				"#..#...#",
				"##.#.#.#",
				"#....#.#",
				"#.##.#.#",
				"#....#.#",
				"########",
			},
			start:       pathfind.Cell{Col: 1, Row: 1},
			goal:        pathfind.Cell{Col: 6, Row: 5},
			opts:        pathfind.GridPathOptions{CellSize: 20, Radius: 0},
			wantShorter: true,
		},
		{
			name: "rectangular_tiles_around_obstacle",
			grid: []string{
				".......",
				"...#...",
				"...#...",
				".......",
			},
			start:       pathfind.Cell{Col: 0, Row: 1},
			goal:        pathfind.Cell{Col: 6, Row: 2},
			opts:        pathfind.GridPathOptions{Origin: core.Point{X: 10, Y: -20}, CellWidth: 32, CellHeight: 16, Radius: 6},
			wantShorter: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := gridFromASCII(tc.grid)
			a := pathfind.NewAStar()
			a.Diagonal = tc.diagonal
			raw, _, ok := a.Find(g, tc.start, tc.goal)
			if !ok {
				t.Fatalf("expected A* path from %v to %v", tc.start, tc.goal)
			}

			pts := pathfind.SmoothGridPath(g, raw, tc.opts)
			if len(pts) < 2 {
				t.Fatalf("smoothed len = %d, want >= 2", len(pts))
			}
			if pts[0] != tc.opts.CellCenter(tc.start) {
				t.Fatalf("first waypoint = %v, want %v", pts[0], tc.opts.CellCenter(tc.start))
			}
			if pts[len(pts)-1] != tc.opts.CellCenter(tc.goal) {
				t.Fatalf("last waypoint = %v, want %v", pts[len(pts)-1], tc.opts.CellCenter(tc.goal))
			}
			if tc.wantLenEq > 0 && len(pts) != tc.wantLenEq {
				t.Fatalf("smoothed len = %d (%v), want %d (raw len=%d)", len(pts), pts, tc.wantLenEq, len(raw))
			}
			if tc.wantShorter && len(pts) >= len(raw) {
				t.Fatalf("expected smoothing to drop cells: smoothed len=%d >= raw len=%d", len(pts), len(raw))
			}
			assertSmoothedClearance(t, g, pts, tc.opts)
		})
	}
}

func TestSmoothGridPathRadiusExceedingHalfCell(t *testing.T) {
	// Radius 10 > CellSize/2 (8):
	// 1) In a 1-cell narrow corridor, smoothing conservatively keeps all grid steps (never jumps across walls).
	corridor := gridFromASCII([]string{
		"#####",
		"#...#",
		"#####",
	})
	raw := []pathfind.Cell{{Col: 1, Row: 1}, {Col: 2, Row: 1}, {Col: 3, Row: 1}}
	opts := pathfind.GridPathOptions{CellSize: 16, Radius: 10}
	got := pathfind.SmoothGridPath(corridor, raw, opts)
	if len(got) != len(raw) {
		t.Fatalf("narrow corridor with radius>cell/2: len=%d, want %d (conservative fallback)", len(got), len(raw))
	}

	// 2) In a wide-open room (no blocked cells within radius 10), smoothing still collapses to [start, end].
	wide := gridFromASCII([]string{
		"########",
		"#......#",
		"#......#",
		"#......#",
		"#......#",
		"########",
	})
	a := pathfind.NewAStar()
	wideRaw, _, ok := a.Find(wide, pathfind.Cell{Col: 2, Row: 2}, pathfind.Cell{Col: 5, Row: 3})
	if !ok {
		t.Fatal("expected path in wide room")
	}
	wideSmoothed := pathfind.SmoothGridPath(wide, wideRaw, opts)
	if len(wideSmoothed) != 2 {
		t.Fatalf("wide room with radius=10: smoothed len=%d (%v), want 2", len(wideSmoothed), wideSmoothed)
	}
	assertSmoothedClearance(t, wide, wideSmoothed, opts)
}

func TestSmoothGridPathEdgeCasesAndDeterminism(t *testing.T) {
	g := gridFromASCII([]string{
		"......",
		"..#...",
		"......",
		"...#..",
		"......",
	})
	opts := pathfind.GridPathOptions{Origin: core.Point{X: 100.3, Y: -50.7}, CellSize: 16, Radius: 4}

	// Empty path.
	if got := pathfind.SmoothGridPath(g, nil, opts); len(got) != 0 {
		t.Fatalf("empty path gave %v, want empty", got)
	}
	// 1-cell path.
	if got := pathfind.SmoothGridPath(g, []pathfind.Cell{{Col: 1, Row: 2}}, opts); !slices.Equal(got, []core.Point{opts.CellCenter(pathfind.Cell{Col: 1, Row: 2})}) {
		t.Fatalf("1-cell path gave %v", got)
	}
	// 2-cell path.
	two := []pathfind.Cell{{Col: 0, Row: 0}, {Col: 1, Row: 0}}
	if got := pathfind.SmoothGridPath(g, two, opts); !slices.Equal(got, []core.Point{opts.CellCenter(two[0]), opts.CellCenter(two[1])}) {
		t.Fatalf("2-cell path gave %v", got)
	}
	// nil grid converts cell centers without panicking or dropping points.
	three := []pathfind.Cell{{Col: 0, Row: 0}, {Col: 1, Row: 0}, {Col: 2, Row: 0}}
	if got := pathfind.SmoothGridPath(nil, three, opts); len(got) != 3 {
		t.Fatalf("nil grid gave len=%d, want 3", len(got))
	}
	// Zero-value GridPathOptions defaults to 1x1 cells at origin (0,0) with radius 0.
	open := pathfind.NewBoolGrid(5, 5)
	rawOpen, _, _ := pathfind.NewAStar().Find(open, pathfind.Cell{Col: 0, Row: 0}, pathfind.Cell{Col: 4, Row: 3})
	zeroGot := pathfind.SmoothGridPath(open, rawOpen, pathfind.GridPathOptions{})
	wantZero := []core.Point{{X: 0.5, Y: 0.5}, {X: 4.5, Y: 3.5}}
	if !slices.Equal(zeroGot, wantZero) {
		t.Fatalf("zero opts gave %v, want %v", zeroGot, wantZero)
	}
	// Negative radius behaves like radius 0 (still rejects corner cuts even with fractional Origin).
	cornerGrid := gridFromASCII([]string{
		".#",
		"..",
	})
	cornerPath := []pathfind.Cell{{Col: 0, Row: 0}, {Col: 0, Row: 1}, {Col: 1, Row: 1}}
	negOpts := pathfind.GridPathOptions{Origin: core.Point{X: 100.3, Y: -50.7}, CellSize: 16, Radius: -3}
	negGot := pathfind.SmoothGridPath(cornerGrid, cornerPath, negOpts)
	if len(negGot) != 3 {
		t.Fatalf("negative radius cut corner at fractional origin: got %v, want len 3", negGot)
	}

	// SmoothGridPathInto overwrites stale dst elements and preserves capacity.
	raw, _, ok := pathfind.NewAStar().Find(g, pathfind.Cell{Col: 0, Row: 0}, pathfind.Cell{Col: 5, Row: 4})
	if !ok {
		t.Fatal("expected path")
	}
	dst := make([]core.Point, 16, 32)
	for i := range dst {
		dst[i] = core.Point{X: 999, Y: 999}
	}
	ref := pathfind.SmoothGridPathInto(dst, g, raw, opts)
	if cap(ref) != 32 {
		t.Fatalf("dst capacity changed: got %d, want 32", cap(ref))
	}
	assertSmoothedClearance(t, g, ref, opts)

	// Determinism across repeated runs.
	refCopy := slices.Clone(ref)
	for i := range 30 {
		got := pathfind.SmoothGridPathInto(dst, g, raw, opts)
		if !slices.Equal(got, refCopy) {
			t.Fatalf("run %d differed: got %v, want %v", i, got, refCopy)
		}
	}
}

func TestGridLineOfSight(t *testing.T) {
	g := gridFromASCII([]string{
		"....",
		"..#.",
		"....",
	})
	opts := pathfind.GridPathOptions{Origin: core.Point{X: 10, Y: 20}, CellSize: 16, Radius: 4}

	if pathfind.GridLineOfSight(nil, core.Point{}, core.Point{}, opts) {
		t.Fatal("nil grid must return false")
	}
	// Out of bounds endpoint returns false.
	if pathfind.GridLineOfSight(g, core.Point{X: 0, Y: 28}, opts.CellCenter(pathfind.Cell{Col: 1, Row: 0}), opts) {
		t.Fatal("out-of-bounds endpoint must return false")
	}
	// Clear horizontal segment across row 0.
	if !pathfind.GridLineOfSight(g, opts.CellCenter(pathfind.Cell{Col: 0, Row: 0}), opts.CellCenter(pathfind.Cell{Col: 3, Row: 0}), opts) {
		t.Fatal("expected clear LOS across row 0")
	}
	// Blocked segment through (2,1).
	if pathfind.GridLineOfSight(g, opts.CellCenter(pathfind.Cell{Col: 1, Row: 1}), opts.CellCenter(pathfind.Cell{Col: 3, Row: 1}), opts) {
		t.Fatal("expected blocked LOS through (2,1)")
	}
	// Degenerate zero-length segment in open space vs inside blocked cell.
	cOpen := opts.CellCenter(pathfind.Cell{Col: 0, Row: 0})
	if !pathfind.GridLineOfSight(g, cOpen, cOpen, opts) {
		t.Fatal("expected clear zero-length LOS in open cell")
	}
	cBlocked := opts.CellCenter(pathfind.Cell{Col: 2, Row: 1})
	if pathfind.GridLineOfSight(g, cBlocked, cBlocked, opts) {
		t.Fatal("expected blocked zero-length LOS inside wall")
	}
}

func TestAStarAnyAngleThetaStar(t *testing.T) {
	// 1. Open ground collapses to 2-cell direct line with exact Euclidean cost (both 4-way and 8-way).
	open := pathfind.NewBoolGrid(14, 10)
	start := pathfind.Cell{Col: 1, Row: 1}
	goal := pathfind.Cell{Col: 12, Row: 8}
	wantDist := math.Hypot(11, 7)

	for _, diag := range []bool{false, true} {
		a := pathfind.NewAStar()
		a.Diagonal = diag
		a.AnyAngle = true
		path, cost, ok := a.Find(open, start, goal)
		if !ok {
			t.Fatalf("diag=%v: expected Theta* path", diag)
		}
		if !slices.Equal(path, []pathfind.Cell{start, goal}) {
			t.Fatalf("diag=%v: Theta* open path = %v, want [%v %v]", diag, path, start, goal)
		}
		if math.Abs(cost-wantDist) > 1e-9 {
			t.Fatalf("diag=%v: Theta* cost = %.9f, want %.9f", diag, cost, wantDist)
		}
	}

	// 2. Around a wall corner: Theta* produces a strictly shorter any-angle path than 8-way A*.
	wall := gridFromASCII([]string{
		".#....",
		"......",
		"......",
	})
	a8 := pathfind.NewAStar()
	a8.Diagonal = true
	_, cost8, ok8 := a8.Find(wall, pathfind.Cell{Col: 0, Row: 0}, pathfind.Cell{Col: 5, Row: 2})
	if !ok8 {
		t.Fatal("expected 8-way path")
	}

	theta := pathfind.NewAStar()
	theta.Diagonal = true
	theta.AnyAngle = true
	tPath, tCost, tOk := theta.Find(wall, pathfind.Cell{Col: 0, Row: 0}, pathfind.Cell{Col: 5, Row: 2})
	if !tOk {
		t.Fatal("expected Theta* path around corner")
	}
	if tCost >= cost8-1e-6 {
		t.Fatalf("expected Theta* cost (%.6f) strictly less than 8-way A* (%.6f)", tCost, cost8)
	}
	// Verify every Theta* segment does not cut into or graze blocked cells (radius 0).
	unitOpts := pathfind.GridPathOptions{CellSize: 1, Radius: 0}
	tPts := make([]core.Point, len(tPath))
	for i, c := range tPath {
		tPts[i] = unitOpts.CellCenter(c)
	}
	assertSmoothedClearance(t, wall, tPts, unitOpts)

	// 3. Respects NoCornerCutting on diagonal pinch.
	pinch := gridFromASCII([]string{
		".#.",
		"#..",
		"...",
	})
	thetaNoCut := pathfind.NewAStar()
	thetaNoCut.Diagonal = true
	thetaNoCut.AnyAngle = true
	if _, _, ok := thetaNoCut.Find(pinch, pathfind.Cell{Col: 0, Row: 0}, pathfind.Cell{Col: 1, Row: 1}); ok {
		t.Fatal("expected Theta* with NoCornerCutting=true to reject diagonal squeeze")
	}
	thetaCut := pathfind.NewAStar()
	thetaCut.Diagonal = true
	thetaCut.NoCornerCutting = false
	thetaCut.AnyAngle = true
	cutPath, cutCost, cutOk := thetaCut.Find(pinch, pathfind.Cell{Col: 0, Row: 0}, pathfind.Cell{Col: 1, Row: 1})
	if !cutOk || len(cutPath) != 2 || math.Abs(cutCost-math.Sqrt2) > 1e-9 {
		t.Fatalf("Theta* with NoCornerCutting=false gave (%v, %v, %v)", cutPath, cutCost, cutOk)
	}

	// 4. CostMap: Theta* does not shortcut through expensive mud (cost > 1).
	cm := pathfind.NewCostMap(6, 4)
	for c := 1; c <= 4; c++ {
		cm.SetCost(c, 1, 10)
	}
	cmPath, cmCost, cmOk := theta.Find(cm, pathfind.Cell{Col: 0, Row: 1}, pathfind.Cell{Col: 5, Row: 1})
	if !cmOk {
		t.Fatal("expected CostMap Theta* path")
	}
	if cmCost >= 10 {
		t.Fatalf("Theta* cut through expensive mud: cost=%v path=%v", cmCost, cmPath)
	}
	for _, c := range cmPath {
		if c.Row == 1 && c.Col >= 1 && c.Col <= 4 {
			t.Fatalf("Theta* stepped onto expensive mud %v in %v", c, cmPath)
		}
	}

	// 5. GridFunc with nil Cost works with Theta*.
	gf := pathfind.GridFunc{
		Cols: 8, Rows: 6,
		Walk: func(c, r int) bool { return open.Walkable(c, r) },
	}
	gfPath, _, gfOk := theta.Find(gf, pathfind.Cell{Col: 0, Row: 0}, pathfind.Cell{Col: 7, Row: 4})
	if !gfOk || len(gfPath) != 2 {
		t.Fatalf("GridFunc Theta* path = %v (ok=%v), want 2-cell straight line", gfPath, gfOk)
	}

	// 6. Determinism across repeated runs.
	maze := gridFromASCII([]string{
		"............",
		"..###.......",
		"....#..###..",
		"....#..#....",
		".......#....",
		"............",
	})
	refPath, refCost, ok := theta.Find(maze, pathfind.Cell{Col: 0, Row: 0}, pathfind.Cell{Col: 11, Row: 5})
	if !ok {
		t.Fatal("expected maze Theta* path")
	}
	for i := range 30 {
		p, c, ok := theta.Find(maze, pathfind.Cell{Col: 0, Row: 0}, pathfind.Cell{Col: 11, Row: 5})
		if !ok || c != refCost || !slices.Equal(p, refPath) {
			t.Fatalf("Theta* run %d differed: (%v, %v) vs (%v, %v)", i, p, c, refPath, refCost)
		}
	}

	// 7. Zero allocations with FindInto after warm-up.
	buf := make([]pathfind.Cell, 0, 64)
	buf, _, ok = theta.FindInto(buf, maze, pathfind.Cell{Col: 0, Row: 0}, pathfind.Cell{Col: 11, Row: 5})
	if !ok {
		t.Fatal("warm-up Theta* FindInto failed")
	}
	allocs := testing.AllocsPerRun(50, func() {
		buf, _, ok = theta.FindInto(buf[:0], maze, pathfind.Cell{Col: 0, Row: 0}, pathfind.Cell{Col: 11, Row: 5})
		if !ok {
			t.Fatal("Theta* FindInto failed")
		}
	})
	if allocs != 0 {
		t.Fatalf("Theta* FindInto allocated %v times per run, want 0", allocs)
	}
}

func TestSmoothGridPathIntoZeroAllocations(t *testing.T) {
	g := pathfind.NewBoolGrid(32, 32)
	for r := 4; r < 26; r++ {
		g.SetWalkable(12, r, false)
	}
	for r := 6; r < 28; r++ {
		g.SetWalkable(22, r, false)
	}

	a := pathfind.NewAStar()
	raw, _, ok := a.Find(g, pathfind.Cell{Col: 2, Row: 16}, pathfind.Cell{Col: 29, Row: 16})
	if !ok {
		t.Fatal("expected path")
	}
	opts := pathfind.GridPathOptions{CellSize: 16, Radius: 5}
	dst := make([]core.Point, 0, len(raw))

	// Warm up.
	dst = pathfind.SmoothGridPathInto(dst, g, raw, opts)
	if len(dst) < 2 || len(dst) >= len(raw) {
		t.Fatalf("unexpected smoothed len %d (raw %d)", len(dst), len(raw))
	}
	assertSmoothedClearance(t, g, dst, opts)

	allocs := testing.AllocsPerRun(100, func() {
		dst = pathfind.SmoothGridPathInto(dst, g, raw, opts)
	})
	if allocs != 0 {
		t.Fatalf("SmoothGridPathInto allocated %v times per run, want 0", allocs)
	}
}

func BenchmarkSmoothGridPathInto(b *testing.B) {
	g := pathfind.NewBoolGrid(32, 32)
	for r := 4; r < 26; r++ {
		g.SetWalkable(12, r, false)
	}
	for r := 6; r < 28; r++ {
		g.SetWalkable(22, r, false)
	}
	a := pathfind.NewAStar()
	raw, _, ok := a.Find(g, pathfind.Cell{Col: 2, Row: 16}, pathfind.Cell{Col: 29, Row: 16})
	if !ok {
		b.Fatal("expected path")
	}
	opts := pathfind.GridPathOptions{CellSize: 16, Radius: 5}
	dst := make([]core.Point, 0, len(raw))
	dst = pathfind.SmoothGridPathInto(dst, g, raw, opts)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		dst = pathfind.SmoothGridPathInto(dst, g, raw, opts)
	}
	if len(dst) == 0 {
		b.Fatal("empty result")
	}
}

func BenchmarkAStarAnyAngleFindInto(b *testing.B) {
	g := pathfind.NewBoolGrid(32, 32)
	for r := 4; r < 26; r++ {
		g.SetWalkable(16, r, false)
	}
	a := pathfind.NewAStar()
	a.Diagonal = true
	a.AnyAngle = true
	start := pathfind.Cell{Col: 2, Row: 16}
	goal := pathfind.Cell{Col: 29, Row: 16}
	buf := make([]pathfind.Cell, 0, 128)
	var ok bool
	buf, _, ok = a.FindInto(buf, g, start, goal)
	if !ok {
		b.Fatal("warm-up failed")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		buf, _, ok = a.FindInto(buf, g, start, goal)
		if !ok {
			b.Fatal("search failed")
		}
	}
}
