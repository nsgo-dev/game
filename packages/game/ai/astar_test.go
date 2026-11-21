package ai_test

import (
	"math"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestAStarOptimal4WayAroundObstacles(t *testing.T) {
	// 7x5 reference grid with a vertical wall having a single gap at row 4.
	// S . . # . . .
	// . . . # . . .
	// . . . # . . .
	// . . . # . . .
	// . . . . . . G
	g := ai.NewBoolGrid(7, 5)
	for r := range 4 {
		g.SetWalkable(3, r, false)
	}

	a := ai.NewAStar()
	start := ai.Cell{Col: 0, Row: 0}
	goal := ai.Cell{Col: 6, Row: 4}
	path, cost, ok := a.Find(g, start, goal)
	if !ok {
		t.Fatal("expected a path, got none")
	}
	// Minimum Manhattan distance from (0,0) to (6,4) is 10; the gap at (3,4) lies on a Manhattan shortest path.
	if cost != 10 {
		t.Fatalf("cost = %v, want 10", cost)
	}
	verifyPath(t, g, path, start, goal, false, true)
	if len(path) != 11 {
		t.Fatalf("path len = %d, want 11: %v", len(path), path)
	}

	// Block row 4 too and open a detour at row 0? Instead test a U-shaped detour requiring 14 steps.
	g2 := ai.NewBoolGrid(7, 5)
	for r := 1; r < 5; r++ {
		g2.SetWalkable(3, r, false)
	}
	// S at (1,4), G at (5,4), wall at col 3 rows 1..4 -> must go up to row 0:
	// (1,4) -> (1,0) [4] -> (5,0) [4] -> (5,4) [4] = 12 steps.
	s2, e2 := ai.Cell{Col: 1, Row: 4}, ai.Cell{Col: 5, Row: 4}
	p2, c2, ok2 := a.Find(g2, s2, e2)
	if !ok2 {
		t.Fatal("expected detour path, got none")
	}
	if c2 != 12 {
		t.Fatalf("detour cost = %v, want 12", c2)
	}
	verifyPath(t, g2, p2, s2, e2, false, true)
}

func TestAStar8WayAndCornerCutting(t *testing.T) {
	// Two walls meeting diagonally at (1,0) and (0,1):
	// S # .
	// # G .
	// . . .
	g := ai.NewBoolGrid(3, 3)
	g.SetWalkable(1, 0, false)
	g.SetWalkable(0, 1, false)

	start := ai.Cell{Col: 0, Row: 0}
	goal := ai.Cell{Col: 1, Row: 1}

	// Default NewAStar has NoCornerCutting: true -> cannot squeeze diagonally from (0,0) to (1,1).
	noCut := ai.NewAStar()
	noCut.Diagonal = true
	if !noCut.NoCornerCutting {
		t.Fatal("NewAStar must default NoCornerCutting to true")
	}
	if _, _, ok := noCut.Find(g, start, goal); ok {
		t.Fatal("expected blocked path when NoCornerCutting is true")
	}

	// With NoCornerCutting: false -> direct diagonal step of cost sqrt(2).
	cut := ai.NewAStar()
	cut.Diagonal = true
	cut.NoCornerCutting = false
	path, cost, ok := cut.Find(g, start, goal)
	if !ok {
		t.Fatal("expected diagonal squeeze when NoCornerCutting is false")
	}
	if math.Abs(cost-math.Sqrt2) > 1e-9 {
		t.Fatalf("cost = %v, want sqrt(2) (%v)", cost, math.Sqrt2)
	}
	if !slices.Equal(path, []ai.Cell{start, goal}) {
		t.Fatalf("path = %v, want [%v %v]", path, start, goal)
	}

	// Single corner obstacle at (1,0) on open 3x3:
	// S # .
	// . . G
	// With NoCornerCutting: true, moving from (0,0) to (1,1) is blocked by (1,0),
	// so shortest path is (0,0)->(0,1)->(1,1)->(2,1) (cost 3) vs (0,0)->(0,1)->(1,2)->(2,1) (1+2*sqrt(2) = 3.828).
	g2 := ai.NewBoolGrid(3, 3)
	g2.SetWalkable(1, 0, false)
	p2, c2, ok2 := noCut.Find(g2, ai.Cell{Col: 0, Row: 0}, ai.Cell{Col: 2, Row: 1})
	if !ok2 {
		t.Fatal("expected path around single corner")
	}
	if math.Abs(c2-3.0) > 1e-9 {
		t.Fatalf("cost = %v, want 3", c2)
	}
	verifyPath(t, g2, p2, ai.Cell{Col: 0, Row: 0}, ai.Cell{Col: 2, Row: 1}, true, true)
}

func TestAStarWeightedTerrainCostMap(t *testing.T) {
	// 5x3 map: row 1 (cols 1..3) is heavy mud (cost 5 each), rows 0 and 2 are cost 1.
	// Start (0,1), Goal (4,1):
	// Straight through mud: enter (1,1)=5, (2,1)=5, (3,1)=5, (4,1)=1 -> total 16.
	// Detour along row 0: (0,0)=1, (1,0)=1, (2,0)=1, (3,0)=1, (4,0)=1, (4,1)=1 -> total 6.
	cm := ai.NewCostMap(5, 3)
	for c := 1; c <= 3; c++ {
		cm.SetCost(c, 1, 5)
	}

	a := ai.NewAStar()
	start, goal := ai.Cell{Col: 0, Row: 1}, ai.Cell{Col: 4, Row: 1}
	path, cost, ok := a.Find(cm, start, goal)
	if !ok {
		t.Fatal("expected weighted path")
	}
	if math.Abs(cost-6) > 1e-9 {
		t.Fatalf("cost = %v, want 6 (detour around mud)", cost)
	}
	for _, cell := range path {
		if cell.Row == 1 && cell.Col >= 1 && cell.Col <= 3 {
			t.Fatalf("path stepped into expensive mud at %v: %v", cell, path)
		}
	}

	// Lower mud cost to 1.2 so straight through (1.2*3 + 1 = 4.6) beats detour (6).
	for c := 1; c <= 3; c++ {
		cm.SetCost(c, 1, 1.2)
	}
	path2, cost2, ok2 := a.Find(cm, start, goal)
	if !ok2 {
		t.Fatal("expected straight mud path")
	}
	if math.Abs(cost2-4.6) > 1e-9 {
		t.Fatalf("cost = %v, want 4.6", cost2)
	}
	if len(path2) != 5 {
		t.Fatalf("path = %v, want straight 5-cell path", path2)
	}
}

func TestAStarGridFuncAdapter(t *testing.T) {
	// Three-line adapter over an arbitrary tile array.
	tiles := [][]int{
		{0, 0, 1, 0},
		{0, 2, 1, 0},
		{0, 0, 0, 0},
	}
	g := ai.GridFunc{
		Cols: 4, Rows: 3,
		Walk: func(c, r int) bool { return tiles[r][c] != 1 },
		Cost: func(c, r int) float64 {
			if tiles[r][c] == 2 {
				return 4
			}
			return 1
		},
	}
	a := ai.NewAStar()
	path, cost, ok := a.Find(g, ai.Cell{Col: 0, Row: 0}, ai.Cell{Col: 3, Row: 0})
	if !ok {
		t.Fatal("expected path through GridFunc")
	}
	// Optimal avoids (1,1) cost 4: (0,0)->(0,1)->(0,2)->(1,2)->(2,2)->(3,2)->(3,1)->(3,0) = 7.
	if math.Abs(cost-7) > 1e-9 {
		t.Fatalf("cost = %v, want 7 (path %v)", cost, path)
	}
}

func TestAStarEdgeCasesAndDeterminism(t *testing.T) {
	g := ai.NewBoolGrid(5, 5)
	g.SetWalkable(2, 2, false)
	a := ai.NewAStar()

	// Start == Goal.
	p, cost, ok := a.Find(g, ai.Cell{Col: 1, Row: 1}, ai.Cell{Col: 1, Row: 1})
	if !ok || cost != 0 || !slices.Equal(p, []ai.Cell{{Col: 1, Row: 1}}) {
		t.Fatalf("start==goal gave (%v, %v, %v)", p, cost, ok)
	}

	// Blocked start or goal or out of bounds.
	if _, _, ok := a.Find(g, ai.Cell{Col: 2, Row: 2}, ai.Cell{Col: 4, Row: 4}); ok {
		t.Fatal("blocked start should fail")
	}
	if _, _, ok := a.Find(g, ai.Cell{Col: 0, Row: 0}, ai.Cell{Col: 2, Row: 2}); ok {
		t.Fatal("blocked goal should fail")
	}
	if _, _, ok := a.Find(g, ai.Cell{Col: -1, Row: 0}, ai.Cell{Col: 4, Row: 4}); ok {
		t.Fatal("out-of-bounds start should fail")
	}
	if _, _, ok := a.Find(nil, ai.Cell{}, ai.Cell{}); ok {
		t.Fatal("nil grid should fail")
	}

	// Unreachable goal.
	walled := ai.NewBoolGrid(4, 4)
	for r := range 4 {
		walled.SetWalkable(2, r, false)
	}
	if _, _, ok := a.Find(walled, ai.Cell{Col: 0, Row: 0}, ai.Cell{Col: 3, Row: 3}); ok {
		t.Fatal("unreachable goal should fail")
	}

	// Determinism across repeated runs on a wide open grid with many equal-cost paths.
	open := ai.NewBoolGrid(12, 12)
	a.Diagonal = true
	ref, refCost, ok := a.Find(open, ai.Cell{Col: 0, Row: 0}, ai.Cell{Col: 11, Row: 8})
	if !ok {
		t.Fatal("expected path on open grid")
	}
	for i := range 30 {
		got, gotCost, ok := a.Find(open, ai.Cell{Col: 0, Row: 0}, ai.Cell{Col: 11, Row: 8})
		if !ok || gotCost != refCost || !slices.Equal(got, ref) {
			t.Fatalf("run %d differed: got (%v, %v), want (%v, %v)", i, got, gotCost, ref, refCost)
		}
	}
}

func TestAStarReusesBuffersWithZeroAllocations(t *testing.T) {
	g := ai.NewBoolGrid(32, 32)
	for r := 4; r < 28; r++ {
		g.SetWalkable(16, r, false)
	}

	a := ai.NewAStar()
	a.Diagonal = true
	start := ai.Cell{Col: 2, Row: 16}
	goal := ai.Cell{Col: 29, Row: 16}
	buf := make([]ai.Cell, 0, 128)

	// Warm up internal buffers once.
	var ok bool
	buf, _, ok = a.FindInto(buf, g, start, goal)
	if !ok {
		t.Fatal("warm-up search failed")
	}

	allocs := testing.AllocsPerRun(50, func() {
		var cost float64
		buf, cost, ok = a.FindInto(buf[:0], g, start, goal)
		if !ok || cost <= 0 {
			t.Fatal("search failed during alloc check")
		}
	})
	if allocs != 0 {
		t.Fatalf("FindInto allocated %v times per run, want 0", allocs)
	}

	// Even Find (without dst) allocates zero times on an unreachable search.
	for r := range 32 {
		g.SetWalkable(16, r, false)
	}
	if _, _, ok := a.Find(g, start, goal); ok {
		t.Fatal("expected unreachable")
	}
	unreachableAllocs := testing.AllocsPerRun(50, func() {
		if _, _, ok := a.Find(g, start, goal); ok {
			t.Fatal("expected unreachable")
		}
	})
	if unreachableAllocs != 0 {
		t.Fatalf("unreachable Find allocated %v times per run, want 0", unreachableAllocs)
	}
}

func verifyPath(t *testing.T, g ai.Grid, path []ai.Cell, start, goal ai.Cell, diagonal, noCornerCut bool) {
	t.Helper()
	if len(path) == 0 {
		t.Fatal("empty path")
	}
	if path[0] != start {
		t.Fatalf("path starts at %v, want %v", path[0], start)
	}
	if path[len(path)-1] != goal {
		t.Fatalf("path ends at %v, want %v", path[len(path)-1], goal)
	}
	for i, c := range path {
		if !g.Walkable(c.Col, c.Row) {
			t.Fatalf("path[%d] = %v is not walkable", i, c)
		}
		if i == 0 {
			continue
		}
		prev := path[i-1]
		dc := c.Col - prev.Col
		dr := c.Row - prev.Row
		if dc < -1 || dc > 1 || dr < -1 || dr > 1 || (dc == 0 && dr == 0) {
			t.Fatalf("invalid step from %v to %v", prev, c)
		}
		if !diagonal && dc != 0 && dr != 0 {
			t.Fatalf("unexpected diagonal step from %v to %v", prev, c)
		}
		if diagonal && noCornerCut && dc != 0 && dr != 0 {
			if !g.Walkable(prev.Col+dc, prev.Row) || !g.Walkable(prev.Col, prev.Row+dr) {
				t.Fatalf("corner cut from %v to %v", prev, c)
			}
		}
	}
}

func TestAStarClampsSubUnitTerrainCosts(t *testing.T) {
	// The distance heuristic assumes every step costs at least 1, so a cheaper
	// cell is clamped rather than allowed to make the heuristic overestimate.
	cm := ai.NewCostMap(3, 1)
	cm.SetCost(1, 0, 0.25)
	if got := cm.MoveCost(1, 0); got != 1 {
		t.Fatalf("MoveCost of a 0.25 cost = %v, want the clamped 1", got)
	}

	f := ai.GridFunc{Cols: 3, Rows: 1, Cost: func(col, row int) float64 { return 0.5 }}
	if got := f.MoveCost(1, 0); got != 1 {
		t.Fatalf("GridFunc MoveCost of a 0.5 cost = %v, want the clamped 1", got)
	}

	path, cost, ok := ai.NewAStar().Find(cm, ai.Cell{Col: 0}, ai.Cell{Col: 2})
	if !ok {
		t.Fatal("expected a path")
	}
	if math.Abs(cost-2) > 1e-9 {
		t.Fatalf("cost = %v, want 2 for a two-step path", cost)
	}
	if len(path) != 3 {
		t.Fatalf("path = %v, want three cells", path)
	}
}
