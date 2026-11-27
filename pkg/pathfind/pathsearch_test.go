package pathfind_test

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/pathfind"
	"github.com/nsgo-dev/nsgo/core"
)

func verifyWalkablePath(t *testing.T, g pathfind.Grid, path []pathfind.Cell, start pathfind.Cell, diagonal, noCornerCut bool) {
	t.Helper()
	if len(path) == 0 {
		t.Fatal("empty path")
	}
	if path[0] != start {
		t.Fatalf("path starts at %v, want %v", path[0], start)
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

func assertGridResultEqual(t *testing.T, label string, got, want pathfind.GridPathResult) {
	t.Helper()
	if got.Found != want.Found || got.Partial != want.Partial || got.Exhausted != want.Exhausted || got.Expansions != want.Expansions {
		t.Fatalf("%s metadata mismatch:\ngot  = {Found:%v Partial:%v Exhausted:%v Expansions:%d}\nwant = {Found:%v Partial:%v Exhausted:%v Expansions:%d}",
			label, got.Found, got.Partial, got.Exhausted, got.Expansions,
			want.Found, want.Partial, want.Exhausted, want.Expansions)
	}
	if math.Abs(got.Cost-want.Cost) > 1e-12 {
		t.Fatalf("%s cost = %v, want %v", label, got.Cost, want.Cost)
	}
	if !slices.Equal(got.Path, want.Path) {
		t.Fatalf("%s path mismatch:\ngot  = %v\nwant = %v", label, got.Path, want.Path)
	}
}

func assertNavResultEqual(t *testing.T, label string, got, want pathfind.NavPathResult) {
	t.Helper()
	if got.Found != want.Found || got.Partial != want.Partial || got.Exhausted != want.Exhausted || got.Expansions != want.Expansions {
		t.Fatalf("%s metadata mismatch:\ngot  = {Found:%v Partial:%v Exhausted:%v Expansions:%d}\nwant = {Found:%v Partial:%v Exhausted:%v Expansions:%d}",
			label, got.Found, got.Partial, got.Exhausted, got.Expansions,
			want.Found, want.Partial, want.Exhausted, want.Expansions)
	}
	if !slices.Equal(got.Path, want.Path) {
		t.Fatalf("%s path mismatch:\ngot  = %v\nwant = %v", label, got.Path, want.Path)
	}
}

func TestAStarPartialPathWalledOffGoal(t *testing.T) {
	// 7x5 grid split completely by a vertical wall at col 3 (rows 0..4 blocked).
	g := pathfind.NewBoolGrid(7, 5)
	for r := range 5 {
		g.SetWalkable(3, r, false)
	}

	a := pathfind.NewAStar()
	start := pathfind.Cell{Col: 0, Row: 0}
	goal := pathfind.Cell{Col: 6, Row: 4}

	// Without AllowPartial, walled-off goal fails cleanly.
	noPartial := a.FindWithOptions(g, start, goal, pathfind.PathOptions{AllowPartial: false})
	if noPartial.Found || noPartial.Partial || noPartial.Exhausted {
		t.Fatalf("expected clean failure without AllowPartial, got %+v", noPartial)
	}
	if noPartial.Expansions == 0 {
		t.Fatal("expected reachable component to be explored before failing")
	}

	// With AllowPartial, returns optimal path to (2, 4) — the reachable cell closest to (6, 4).
	res := a.FindWithOptions(g, start, goal, pathfind.PathOptions{AllowPartial: true})
	if !res.Found || !res.Partial || res.Exhausted {
		t.Fatalf("expected partial non-exhausted result, got %+v", res)
	}
	wantEnd := pathfind.Cell{Col: 2, Row: 4}
	if len(res.Path) == 0 || res.Path[len(res.Path)-1] != wantEnd {
		t.Fatalf("partial path end = %v, want %v (full path: %v)", res.Path, wantEnd, res.Path)
	}
	if math.Abs(res.Cost-6) > 1e-9 {
		t.Fatalf("partial path cost = %v, want 6", res.Cost)
	}
	verifyWalkablePath(t, g, res.Path, start, false, true)

	// Clicking directly on a blocked wall cell with AllowPartial walks up to the nearest adjacent cell (2, 2).
	wallGoal := pathfind.Cell{Col: 3, Row: 2}
	wallRes := a.FindWithOptions(g, start, wallGoal, pathfind.PathOptions{AllowPartial: true})
	if !wallRes.Found || !wallRes.Partial || wallRes.Exhausted {
		t.Fatalf("expected partial path to blocked goal, got %+v", wallRes)
	}
	if wallRes.Path[len(wallRes.Path)-1] != (pathfind.Cell{Col: 2, Row: 2}) {
		t.Fatalf("blocked-goal partial end = %v, want (2,2)", wallRes.Path[len(wallRes.Path)-1])
	}
	verifyWalkablePath(t, g, wallRes.Path, start, false, true)

	// Clicking out-of-bounds goal with AllowPartial walks to nearest reachable in-bounds cell (2, 4).
	oobRes := a.FindWithOptions(g, start, pathfind.Cell{Col: 20, Row: 10}, pathfind.PathOptions{AllowPartial: true})
	if !oobRes.Found || !oobRes.Partial || oobRes.Exhausted {
		t.Fatalf("expected partial path to out-of-bounds goal, got %+v", oobRes)
	}
	if oobRes.Path[len(oobRes.Path)-1] != wantEnd {
		t.Fatalf("oob-goal partial end = %v, want %v", oobRes.Path[len(oobRes.Path)-1], wantEnd)
	}

	// Completely boxed-in start with AllowPartial returns [start] flagged partial.
	boxed := pathfind.NewBoolGrid(3, 3)
	boxed.SetWalkable(1, 0, false)
	boxed.SetWalkable(0, 1, false)
	boxedRes := a.FindWithOptions(boxed, pathfind.Cell{Col: 0, Row: 0}, pathfind.Cell{Col: 2, Row: 2}, pathfind.PathOptions{AllowPartial: true})
	if !boxedRes.Found || !boxedRes.Partial || boxedRes.Exhausted || !slices.Equal(boxedRes.Path, []pathfind.Cell{{Col: 0, Row: 0}}) {
		t.Fatalf("boxed-in partial result = %+v, want partial [0,0]", boxedRes)
	}

	// Unwalkable start always fails even with AllowPartial.
	if badStart := a.FindWithOptions(g, pathfind.Cell{Col: 3, Row: 0}, goal, pathfind.PathOptions{AllowPartial: true}); badStart.Found {
		t.Fatalf("blocked start must fail, got %+v", badStart)
	}

	// Start == goal succeeds immediately without expanding nodes.
	same := a.FindWithOptions(g, start, start, pathfind.PathOptions{MaxExpansions: 1, AllowPartial: true})
	if !same.Found || same.Partial || same.Exhausted || same.Expansions != 0 || !slices.Equal(same.Path, []pathfind.Cell{start}) {
		t.Fatalf("start==goal result = %+v", same)
	}
}

func TestAStarExpansionBudgetAndZeroAlloc(t *testing.T) {
	g := pathfind.NewBoolGrid(30, 30)
	// Barrier forcing a long detour around col 15.
	for r := range 26 {
		g.SetWalkable(15, r, false)
	}

	start := pathfind.Cell{Col: 0, Row: 0}
	goal := pathfind.Cell{Col: 29, Row: 0}

	a := pathfind.NewAStar()
	full := a.FindWithOptions(g, start, goal, pathfind.PathOptions{})
	if !full.Found || full.Partial || full.Exhausted {
		t.Fatalf("expected full path, got %+v", full)
	}
	if full.Expansions <= 25 {
		t.Fatalf("expected > 25 expansions on detour map, got %d", full.Expansions)
	}

	// Budget smaller than full search: without AllowPartial -> failed + exhausted.
	const budget = 20
	failBounded := a.FindWithOptions(g, start, goal, pathfind.PathOptions{MaxExpansions: budget, AllowPartial: false})
	if failBounded.Found || failBounded.Partial || !failBounded.Exhausted || failBounded.Expansions != budget {
		t.Fatalf("expected exhausted failure at budget %d, got %+v", budget, failBounded)
	}

	// Budget smaller than full search: with AllowPartial -> partial + exhausted + exact budget expansions.
	partBounded := a.FindWithOptions(g, start, goal, pathfind.PathOptions{MaxExpansions: budget, AllowPartial: true})
	if !partBounded.Found || !partBounded.Partial || !partBounded.Exhausted || partBounded.Expansions != budget {
		t.Fatalf("expected exhausted partial at budget %d, got %+v", budget, partBounded)
	}
	verifyWalkablePath(t, g, partBounded.Path, start, false, true)
	if len(partBounded.Path) < 2 {
		t.Fatalf("expected partial path to advance toward goal, got %v", partBounded.Path)
	}

	// AStar.MaxExpansions applies to Find and as default for FindWithOptions, while negative opts overrides it.
	a.MaxExpansions = budget
	if _, _, ok := a.Find(g, start, goal); ok {
		t.Fatal("Find should fail when AStar.MaxExpansions is exhausted")
	}
	fallbackPart := a.FindWithOptions(g, start, goal, pathfind.PathOptions{AllowPartial: true})
	assertGridResultEqual(t, "AStar.MaxExpansions fallback", fallbackPart, partBounded)

	overrideUnlimited := a.FindWithOptions(g, start, goal, pathfind.PathOptions{MaxExpansions: -1})
	assertGridResultEqual(t, "negative MaxExpansions overrides AStar.MaxExpansions", overrideUnlimited, full)

	// Zero heap allocations on FindWithOptionsInto after warm-up.
	a.MaxExpansions = 0
	buf := make([]pathfind.Cell, 0, 128)
	buf = a.FindWithOptionsInto(buf, g, start, goal, pathfind.PathOptions{MaxExpansions: budget, AllowPartial: true}).Path
	allocs := testing.AllocsPerRun(50, func() {
		r := a.FindWithOptionsInto(buf[:0], g, start, goal, pathfind.PathOptions{MaxExpansions: budget, AllowPartial: true})
		if !r.Found || !r.Partial || !r.Exhausted {
			t.Fatal("unexpected result in alloc check")
		}
		buf = r.Path
	})
	if allocs != 0 {
		t.Fatalf("FindWithOptionsInto allocated %v times per run, want 0", allocs)
	}
}

func buildComplexMazeCostMap() *pathfind.CostMap {
	cm := pathfind.NewCostMap(24, 20)
	for r := range 16 {
		cm.SetWalkable(6, r, false)
	}
	for r := 4; r < 20; r++ {
		cm.SetWalkable(12, r, false)
	}
	for r := range 15 {
		cm.SetWalkable(18, r, false)
	}
	// Heavy mud patch encouraging curved detours.
	for c := 7; c <= 11; c++ {
		for r := 6; r <= 12; r++ {
			cm.SetCost(c, r, 4.0)
		}
	}
	return cm
}

func TestAStarIncrementalMatchesSingleShot(t *testing.T) {
	cm := buildComplexMazeCostMap()
	walled := buildComplexMazeCostMap()
	for r := range 20 {
		walled.SetWalkable(18, r, false)
	}

	type testCase struct {
		name  string
		astar *pathfind.AStar
		grid  pathfind.Grid
		start pathfind.Cell
		goal  pathfind.Cell
		opts  pathfind.PathOptions
	}

	a4 := pathfind.NewAStar()
	a8 := pathfind.NewAStar()
	a8.Diagonal = true
	aHex := pathfind.NewAStar()
	aHex.Topology = pathfind.TopologyHexPointyOdd

	cases := []testCase{
		{
			name:  "4way_complete",
			astar: a4,
			grid:  cm,
			start: pathfind.Cell{Col: 1, Row: 1},
			goal:  pathfind.Cell{Col: 22, Row: 2},
			opts:  pathfind.PathOptions{AllowPartial: true},
		},
		{
			name:  "8way_complete",
			astar: a8,
			grid:  cm,
			start: pathfind.Cell{Col: 1, Row: 1},
			goal:  pathfind.Cell{Col: 22, Row: 2},
			opts:  pathfind.PathOptions{AllowPartial: true},
		},
		{
			name:  "hex_complete",
			astar: aHex,
			grid:  cm,
			start: pathfind.Cell{Col: 1, Row: 1},
			goal:  pathfind.Cell{Col: 22, Row: 2},
			opts:  pathfind.PathOptions{AllowPartial: true},
		},
		{
			name:  "walled_partial",
			astar: a8,
			grid:  walled,
			start: pathfind.Cell{Col: 1, Row: 1},
			goal:  pathfind.Cell{Col: 22, Row: 10},
			opts:  pathfind.PathOptions{AllowPartial: true},
		},
		{
			name:  "budget_exhausted_partial",
			astar: a4,
			grid:  cm,
			start: pathfind.Cell{Col: 1, Row: 1},
			goal:  pathfind.Cell{Col: 22, Row: 2},
			opts:  pathfind.PathOptions{MaxExpansions: 53, AllowPartial: true},
		},
		{
			name:  "budget_exhausted_fail",
			astar: a4,
			grid:  cm,
			start: pathfind.Cell{Col: 1, Row: 1},
			goal:  pathfind.Cell{Col: 22, Row: 2},
			opts:  pathfind.PathOptions{MaxExpansions: 53, AllowPartial: false},
		},
	}

	stepBudgets := []int{1, 5, 17, 64}

	for _, tc := range cases {
		want := tc.astar.FindWithOptions(tc.grid, tc.start, tc.goal, tc.opts)

		for _, stepSize := range stepBudgets {
			search := tc.astar.StartSearch(tc.grid, tc.start, tc.goal, tc.opts)

			// Step(0) and Step(-3) must do 0 work.
			if _, spent := search.Step(0); spent != 0 {
				t.Fatalf("%s step(0) spent %d, want 0", tc.name, spent)
			}
			if _, spent := search.Step(-3); spent != 0 {
				t.Fatalf("%s step(-3) spent %d, want 0", tc.name, spent)
			}

			totalSpent := 0
			for !search.Status().Done() {
				// While in progress, Result().Found must be false.
				if search.Result().Found {
					t.Fatalf("%s returned Found=true while still in progress", tc.name)
				}
				_, spent := search.Step(stepSize)
				if spent < 0 || spent > stepSize {
					t.Fatalf("%s step(%d) spent %d, exceeded budget", tc.name, stepSize, spent)
				}
				totalSpent += spent
			}

			// Extra Step after Done must do 0 work.
			if _, spent := search.Step(stepSize); spent != 0 {
				t.Fatalf("%s post-done step spent %d, want 0", tc.name, spent)
			}
			if totalSpent != search.Expansions() {
				t.Fatalf("%s totalSpent=%d != search.Expansions()=%d", tc.name, totalSpent, search.Expansions())
			}

			got := search.Result()
			assertGridResultEqual(t, fmt.Sprintf("%s/step=%d", tc.name, stepSize), got, want)
		}
	}

	// Verify AStarSearch.Reset produces identical results when reused across queries.
	reused := a8.StartSearch(cm, pathfind.Cell{Col: 1, Row: 1}, pathfind.Cell{Col: 22, Row: 2}, pathfind.PathOptions{})
	for !reused.Status().Done() {
		reused.Step(7)
	}
	want1 := a8.FindWithOptions(cm, pathfind.Cell{Col: 1, Row: 1}, pathfind.Cell{Col: 22, Row: 2}, pathfind.PathOptions{})
	assertGridResultEqual(t, "reused first", reused.Result(), want1)

	reused.Reset(a8, walled, pathfind.Cell{Col: 1, Row: 1}, pathfind.Cell{Col: 22, Row: 10}, pathfind.PathOptions{AllowPartial: true})
	for !reused.Status().Done() {
		reused.Step(3)
	}
	want2 := a8.FindWithOptions(walled, pathfind.Cell{Col: 1, Row: 1}, pathfind.Cell{Col: 22, Row: 10}, pathfind.PathOptions{AllowPartial: true})
	assertGridResultEqual(t, "reused second", reused.Result(), want2)
}

func buildGridNavMesh2D(cols, rows int, blocked map[pathfind.Cell]bool) *pathfind.NavMesh {
	var polys [][]core.Point
	const cellSize = 10
	for r := range rows {
		for c := range cols {
			if blocked[pathfind.Cell{Col: c, Row: r}] {
				continue
			}
			x0, y0 := float32(c*cellSize), float32(r*cellSize)
			polys = append(polys, rectPoly(x0, y0, x0+cellSize, y0+cellSize))
		}
	}
	return pathfind.BuildNavMesh(polys)
}

func TestNavMesh2DPartialAndBudgeted(t *testing.T) {
	// Two islands: left corridor [0..20]x[0..10] (2 connected quads) and disconnected island [30..40]x[0..10].
	mesh := pathfind.BuildNavMesh([][]core.Point{
		rectPoly(0, 0, 10, 10),
		rectPoly(10, 0, 20, 10),
		rectPoly(30, 0, 40, 10),
	})

	start := pt(2, 5)
	goalIsland := pt(35, 5)

	// Disconnected without AllowPartial fails.
	if r := mesh.FindPathWithOptions(start, goalIsland, pathfind.PathOptions{}); r.Found || r.Partial {
		t.Fatalf("expected disconnected failure without AllowPartial, got %+v", r)
	}

	// Disconnected with AllowPartial reaches the closest boundary point (20, 5) on the reachable island.
	part := mesh.FindPathWithOptions(start, goalIsland, pathfind.PathOptions{AllowPartial: true})
	if !part.Found || !part.Partial || part.Exhausted {
		t.Fatalf("expected partial path to disconnected island, got %+v", part)
	}
	assertPathPoints(t, part.Path, []core.Point{pt(2, 5), pt(20, 5)}, 1e-4)

	// Goal outside the mesh entirely (in the gap at (24, 8)) with AllowPartial reaches (20, 8).
	outside := mesh.FindPathWithOptions(start, pt(24, 8), pathfind.PathOptions{AllowPartial: true})
	if !outside.Found || !outside.Partial || outside.Exhausted {
		t.Fatalf("expected partial path to outside goal, got %+v", outside)
	}
	assertPathPoints(t, outside.Path, []core.Point{pt(2, 5), pt(20, 8)}, 1e-4)

	// Isolated single polygon with outside goal finishes immediately with 0 expansions.
	single := pathfind.BuildNavMesh([][]core.Point{rectPoly(0, 0, 10, 10)})
	iso := single.FindPathWithOptions(pt(2, 5), pt(25, 5), pathfind.PathOptions{AllowPartial: true})
	if !iso.Found || !iso.Partial || iso.Exhausted || iso.Expansions != 0 {
		t.Fatalf("isolated polygon partial result = %+v", iso)
	}
	assertPathPoints(t, iso.Path, []core.Point{pt(2, 5), pt(10, 5)}, 1e-4)
}

func TestNavMesh2DIncrementalMatchesSingleShot(t *testing.T) {
	// 8x6 room grid with internal walls creating a winding corridor.
	blocked := map[pathfind.Cell]bool{}
	for r := range 5 {
		blocked[pathfind.Cell{Col: 2, Row: r}] = true
	}
	for r := 1; r < 6; r++ {
		blocked[pathfind.Cell{Col: 5, Row: r}] = true
	}
	mesh := buildGridNavMesh2D(8, 6, blocked)

	// Also build a completely walled-off version.
	walledBlocked := map[pathfind.Cell]bool{}
	for k, v := range blocked {
		walledBlocked[k] = v
	}
	walledBlocked[pathfind.Cell{Col: 5, Row: 0}] = true
	walledMesh := buildGridNavMesh2D(8, 6, walledBlocked)

	start := pt(5, 5)
	goal := pt(75, 55)

	scenarios := []struct {
		name string
		mesh *pathfind.NavMesh
		opts pathfind.PathOptions
	}{
		{"complete", mesh, pathfind.PathOptions{AllowPartial: true}},
		{"walled_partial", walledMesh, pathfind.PathOptions{AllowPartial: true}},
		{"exhausted_partial", mesh, pathfind.PathOptions{MaxExpansions: 12, AllowPartial: true}},
		{"exhausted_fail", mesh, pathfind.PathOptions{MaxExpansions: 12, AllowPartial: false}},
	}

	for _, sc := range scenarios {
		want := sc.mesh.FindPathWithOptions(start, goal, sc.opts)
		for _, stepSize := range []int{1, 5, 17} {
			search := sc.mesh.StartSearch(start, goal, sc.opts)
			totalSpent := 0
			for !search.Status().Done() {
				_, spent := search.Step(stepSize)
				if spent < 0 || spent > stepSize {
					t.Fatalf("%s step(%d) spent %d > budget", sc.name, stepSize, spent)
				}
				totalSpent += spent
			}
			if totalSpent != search.Expansions() {
				t.Fatalf("%s totalSpent=%d != Expansions()=%d", sc.name, totalSpent, search.Expansions())
			}
			assertNavResultEqual(t, sc.name, search.Result(), want)
		}
	}
}

func TestPathQueueStrictBudgetPriorityCancelAndDeterminism(t *testing.T) {
	cm := buildComplexMazeCostMap()
	a := pathfind.NewAStar()
	a.Diagonal = true

	mesh := buildGridNavMesh2D(8, 6, map[pathfind.Cell]bool{
		{Col: 2, Row: 0}: true, {Col: 2, Row: 1}: true, {Col: 2, Row: 2}: true, {Col: 2, Row: 3}: true,
		{Col: 5, Row: 2}: true, {Col: 5, Row: 3}: true, {Col: 5, Row: 4}: true, {Col: 5, Row: 5}: true,
	})

	// 1. FIFO + priority ordering + cancellation + re-entrant submission inside callback.
	q := pathfind.NewPathQueue(pathfind.PathQueueConfig{
		DefaultBudget: 11,
		Policy:        pathfind.PathQueueFIFO,
	})

	var doneOrder []pathfind.PathRequestID
	var results = map[pathfind.PathRequestID]pathfind.GridPathResult{}

	idLow1, sLow1 := q.SubmitGrid(a, cm, pathfind.Cell{Col: 1, Row: 1}, pathfind.Cell{Col: 22, Row: 2}, pathfind.PathOptions{}, 0, func(id pathfind.PathRequestID, res pathfind.GridPathResult) {
		doneOrder = append(doneOrder, id)
		results[id] = res
	})
	idCancel, _ := q.SubmitGrid(a, cm, pathfind.Cell{Col: 1, Row: 2}, pathfind.Cell{Col: 22, Row: 18}, pathfind.PathOptions{}, 5, func(id pathfind.PathRequestID, _ pathfind.GridPathResult) {
		t.Errorf("canceled request %d callback must not run", id)
	})
	var reentrantID pathfind.PathRequestID
	idHigh, _ := q.SubmitGrid(a, cm, pathfind.Cell{Col: 1, Row: 1}, pathfind.Cell{Col: 22, Row: 2}, pathfind.PathOptions{}, 10, func(id pathfind.PathRequestID, res pathfind.GridPathResult) {
		doneOrder = append(doneOrder, id)
		results[id] = res
		// Re-entrantly submit a new high-priority immediate request from inside callback.
		reentrantID, _ = q.SubmitGrid(a, cm, pathfind.Cell{Col: 3, Row: 3}, pathfind.Cell{Col: 3, Row: 3}, pathfind.PathOptions{}, 20, func(rid pathfind.PathRequestID, rres pathfind.GridPathResult) {
			doneOrder = append(doneOrder, rid)
			results[rid] = rres
		})
	})
	idNav, sNav := q.SubmitNavMesh(mesh, pt(5, 5), pt(75, 55), pathfind.PathOptions{AllowPartial: true}, 0, func(id pathfind.PathRequestID, _ pathfind.NavPathResult) {
		doneOrder = append(doneOrder, id)
	})

	if !q.IsPending(idCancel) || !q.Cancel(idCancel) || q.IsPending(idCancel) {
		t.Fatal("Cancel did not remove pending request cleanly")
	}
	if q.Cancel(idCancel) {
		t.Fatal("second Cancel should return false")
	}

	// Step(0) does nothing.
	if spent := q.Step(0); spent != 0 {
		t.Fatalf("Step(0) spent %d, want 0", spent)
	}

	for q.Pending() > 0 {
		spent := q.StepDefault()
		if spent < 0 || spent > 11 {
			t.Fatalf("StepDefault spent %d, exceeded budget 11", spent)
		}
	}

	if q.Completed() != 4 {
		t.Fatalf("Completed = %d, want 4", q.Completed())
	}
	wantOrder := []pathfind.PathRequestID{idHigh, reentrantID, idLow1, idNav}
	if !slices.Equal(doneOrder, wantOrder) {
		t.Fatalf("completion order = %v, want %v", doneOrder, wantOrder)
	}
	wantGrid := a.FindWithOptions(cm, pathfind.Cell{Col: 1, Row: 1}, pathfind.Cell{Col: 22, Row: 2}, pathfind.PathOptions{})
	assertGridResultEqual(t, "queue low1", results[idLow1], wantGrid)
	assertGridResultEqual(t, "queue low1 polled", sLow1.Result(), wantGrid)
	assertNavResultEqual(t, "queue nav", sNav.Result(), mesh.FindPathWithOptions(pt(5, 5), pt(75, 55), pathfind.PathOptions{AllowPartial: true}))

	// 2. Round-robin fairness: all equal-priority requests make progress in the very first step.
	rr := pathfind.NewPathQueue(pathfind.PathQueueConfig{
		Policy: pathfind.PathQueueRoundRobin,
	})
	var rrSearches []*pathfind.AStarSearch
	for i := range 5 {
		_, s := rr.SubmitGrid(a, cm, pathfind.Cell{Col: 1, Row: i + 1}, pathfind.Cell{Col: 22, Row: 18 - i}, pathfind.PathOptions{}, 0, nil)
		rrSearches = append(rrSearches, s)
	}
	spent1 := rr.Step(25)
	if spent1 != 25 {
		t.Fatalf("round-robin first step spent %d, want 25", spent1)
	}
	for i, s := range rrSearches {
		if s.Expansions() == 0 {
			t.Fatalf("round-robin starved request %d on first step", i)
		}
	}
	rr.Clear()
	if rr.Pending() != 0 {
		t.Fatalf("Clear left %d pending requests, want 0", rr.Pending())
	}

	// Verify SearchStatus.String() names and nil receiver safety.
	if pathfind.SearchInProgress.String() != "in_progress" || pathfind.SearchComplete.String() != "complete" ||
		pathfind.SearchPartial.String() != "partial" || pathfind.SearchFailed.String() != "failed" {
		t.Fatalf("unexpected SearchStatus.String() values")
	}
	var nilGridSearch *pathfind.AStarSearch
	var nilNavSearch *pathfind.NavMeshSearch
	var nilQueue *pathfind.PathQueue
	if st, n := nilGridSearch.Step(5); st != pathfind.SearchFailed || n != 0 {
		t.Fatalf("nil AStarSearch.Step = (%v, %d)", st, n)
	}
	if st, n := nilNavSearch.Step(5); st != pathfind.SearchFailed || n != 0 {
		t.Fatalf("nil NavMeshSearch.Step = (%v, %d)", st, n)
	}
	if n := nilQueue.Step(5); n != 0 || nilQueue.Pending() != 0 {
		t.Fatalf("nil PathQueue behaved unexpectedly")
	}

	// 3. Determinism across repeated runs on a large mixed batch with strict per-step budget.
	runBatch := func() ([]int, []pathfind.GridPathResult) {
		bq := pathfind.NewPathQueue(pathfind.PathQueueConfig{Policy: pathfind.PathQueueRoundRobin, SliceSize: 7})
		searches := make([]*pathfind.AStarSearch, 20)
		for i := range 20 {
			start := pathfind.Cell{Col: i % 5, Row: (i * 3) % 18}
			goal := pathfind.Cell{Col: 19 + i%5, Row: (i * 5) % 19}
			_, searches[i] = bq.SubmitGrid(a, cm, start, goal, pathfind.PathOptions{AllowPartial: true}, i%3, nil)
		}
		var perStep []int
		const budget = 31
		for bq.Pending() > 0 {
			used := bq.Step(budget)
			if used < 0 || used > budget {
				t.Fatalf("step used %d > budget %d", used, budget)
			}
			perStep = append(perStep, used)
		}
		out := make([]pathfind.GridPathResult, len(searches))
		for i, s := range searches {
			out[i] = s.Result()
		}
		return perStep, out
	}

	refSteps, refRes := runBatch()
	for run := range 5 {
		gotSteps, gotRes := runBatch()
		if !slices.Equal(gotSteps, refSteps) {
			t.Fatalf("run %d per-step expansions differed: %v vs %v", run, gotSteps, refSteps)
		}
		for i := range refRes {
			assertGridResultEqual(t, "determinism", gotRes[i], refRes[i])
		}
	}
}

func BenchmarkPathQueueBudgetedStep(b *testing.B) {
	// 64x64 maze with multiple long corridors.
	g := pathfind.NewBoolGrid(64, 64)
	for r := range 56 {
		g.SetWalkable(16, r, false)
	}
	for r := 8; r < 64; r++ {
		g.SetWalkable(32, r, false)
	}
	for r := range 56 {
		g.SetWalkable(48, r, false)
	}

	a := pathfind.NewAStar()
	a.Diagonal = true
	const perStepBudget = 64

	q := pathfind.NewPathQueue(pathfind.PathQueueConfig{
		DefaultBudget: perStepBudget,
		Policy:        pathfind.PathQueueRoundRobin,
		SliceSize:     16,
	})
	refill := func() {
		for i := range 32 {
			start := pathfind.Cell{Col: 1 + i%10, Row: 1 + (i*2)%50}
			goal := pathfind.Cell{Col: 52 + i%10, Row: 1 + (i*3)%50}
			q.SubmitGrid(a, g, start, goal, pathfind.PathOptions{AllowPartial: true}, i%3, nil)
		}
	}
	refill()

	b.ResetTimer()
	for range b.N {
		if q.Pending() < 16 {
			refill()
		}
		used := q.Step(perStepBudget)
		if used > perStepBudget {
			b.Fatalf("PathQueue.Step used %d expansions, exceeding per-step budget %d", used, perStepBudget)
		}
	}
}
