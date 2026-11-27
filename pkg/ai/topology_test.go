package ai_test

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/tilemap"
)

func TestTopologyFromTilemap(t *testing.T) {
	if got := ai.TopologyFromTilemap(nil); got != ai.TopologySquare {
		t.Fatalf("nil tilemap topology = %v, want %v", got, ai.TopologySquare)
	}

	cases := []struct {
		name         string
		orientation  tilemap.Orientation
		staggerAxis  tilemap.StaggerAxis
		staggerIndex tilemap.StaggerIndex
		want         ai.GridTopology
		wantName     string
		wantHex      bool
		wantStag     bool
	}{
		{"orthogonal", tilemap.OrientationOrthogonal, tilemap.StaggerY, tilemap.StaggerOdd, ai.TopologySquare, "square", false, false},
		{"isometric", tilemap.OrientationIsometric, tilemap.StaggerY, tilemap.StaggerOdd, ai.TopologySquare, "square", false, false},
		{"hex_pointy_odd", tilemap.OrientationHexagonal, tilemap.StaggerY, tilemap.StaggerOdd, ai.TopologyHexPointyOdd, "hex-pointy-odd", true, false},
		{"hex_pointy_even", tilemap.OrientationHexagonal, tilemap.StaggerY, tilemap.StaggerEven, ai.TopologyHexPointyEven, "hex-pointy-even", true, false},
		{"hex_flat_odd", tilemap.OrientationHexagonal, tilemap.StaggerX, tilemap.StaggerOdd, ai.TopologyHexFlatOdd, "hex-flat-odd", true, false},
		{"hex_flat_even", tilemap.OrientationHexagonal, tilemap.StaggerX, tilemap.StaggerEven, ai.TopologyHexFlatEven, "hex-flat-even", true, false},
		{"staggered_y_odd", tilemap.OrientationStaggered, tilemap.StaggerY, tilemap.StaggerOdd, ai.TopologyStaggeredYOdd, "staggered-y-odd", false, true},
		{"staggered_y_even", tilemap.OrientationStaggered, tilemap.StaggerY, tilemap.StaggerEven, ai.TopologyStaggeredYEven, "staggered-y-even", false, true},
		{"staggered_x_odd", tilemap.OrientationStaggered, tilemap.StaggerX, tilemap.StaggerOdd, ai.TopologyStaggeredXOdd, "staggered-x-odd", false, true},
		{"staggered_x_even", tilemap.OrientationStaggered, tilemap.StaggerX, tilemap.StaggerEven, ai.TopologyStaggeredXEven, "staggered-x-even", false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tilemap.New(4, 4, 32, 32)
			m.Orientation = tc.orientation
			m.StaggerAxis = tc.staggerAxis
			m.StaggerIndex = tc.staggerIndex
			got := ai.TopologyFromTilemap(m)
			if got != tc.want {
				t.Fatalf("TopologyFromTilemap = %v, want %v", got, tc.want)
			}
			if got.String() != tc.wantName {
				t.Fatalf("String() = %q, want %q", got.String(), tc.wantName)
			}
			if got.IsHex() != tc.wantHex || got.IsStaggered() != tc.wantStag {
				t.Fatalf("IsHex=%v IsStaggered=%v, want %v %v", got.IsHex(), got.IsStaggered(), tc.wantHex, tc.wantStag)
			}
		})
	}
	if ai.TopologySquare4.String() != "square4" || ai.TopologySquare8.String() != "square8" {
		t.Fatalf("square topology names = %q, %q", ai.TopologySquare4.String(), ai.TopologySquare8.String())
	}
}

func TestHexNeighborsAndDistanceMatchTilemap(t *testing.T) {
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

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			m := tilemap.New(8, 8, 32, 36)
			m.Orientation = tilemap.OrientationHexagonal
			m.StaggerAxis = tc.axis
			m.StaggerIndex = tc.index
			topo := ai.TopologyFromTilemap(m)

			for r := -3; r <= 3; r++ {
				for c := -3; c <= 3; c++ {
					gotN := topo.Neighbors(ai.Cell{Col: c, Row: r})
					wantN := m.HexNeighbors(c, r)
					if len(gotN) != 6 {
						t.Fatalf("len(Neighbors(%d,%d)) = %d, want 6", c, r, len(gotN))
					}
					for i := range 6 {
						if gotN[i].Col != wantN[i][0] || gotN[i].Row != wantN[i][1] {
							t.Fatalf("neighbor %d of (%d,%d) = %v, want %v", i, c, r, gotN[i], wantN[i])
						}
					}
					for r2 := -2; r2 <= 2; r2++ {
						for c2 := -2; c2 <= 2; c2++ {
							gotD := topo.Distance(ai.Cell{Col: c, Row: r}, ai.Cell{Col: c2, Row: r2})
							wantD := float64(m.HexDistance(c, r, c2, r2))
							if gotD != wantD {
								t.Fatalf("Distance((%d,%d),(%d,%d)) = %v, want %v", c, r, c2, r2, gotD, wantD)
							}
						}
					}
				}
			}
		})
	}
}

func TestAStarHexShortestPathMatchesHexDistance(t *testing.T) {
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

	const cols, rows = 8, 8
	g := ai.NewBoolGrid(cols, rows)

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			m := tilemap.New(cols, rows, 32, 36)
			m.Orientation = tilemap.OrientationHexagonal
			m.StaggerAxis = tc.axis
			m.StaggerIndex = tc.index

			a := ai.NewAStar()
			a.Topology = ai.TopologyFromTilemap(m)

			for sr := range rows {
				for sc := range cols {
					start := ai.Cell{Col: sc, Row: sr}
					for gr := range rows {
						for gc := range cols {
							goal := ai.Cell{Col: gc, Row: gr}
							path, cost, ok := a.Find(g, start, goal)
							if !ok {
								t.Fatalf("no path from %v to %v", start, goal)
							}
							wantSteps := m.HexDistance(sc, sr, gc, gr)
							if len(path)-1 != wantSteps {
								t.Fatalf("%v->%v: steps = %d, want HexDistance %d (path %v)", start, goal, len(path)-1, wantSteps, path)
							}
							if math.Abs(cost-float64(wantSteps)) > 1e-9 {
								t.Fatalf("%v->%v: cost = %v, want %d", start, goal, cost, wantSteps)
							}
							if path[0] != start || path[len(path)-1] != goal {
								t.Fatalf("path endpoints %v..%v, want %v..%v", path[0], path[len(path)-1], start, goal)
							}
							for i := 1; i < len(path); i++ {
								if d := m.HexDistance(path[i-1].Col, path[i-1].Row, path[i].Col, path[i].Row); d != 1 {
									t.Fatalf("non-adjacent hex step %v -> %v (HexDistance=%d)", path[i-1], path[i], d)
								}
							}
						}
					}
				}
			}
		})
	}
}

func TestAStarHexObstaclesAndWeightedTerrain(t *testing.T) {
	// 7x7 pointy-top odd-r hex map with a vertical wall at col 3 (rows 0..5) and a gap at (3, 6).
	m := tilemap.New(7, 7, 32, 36)
	m.Orientation = tilemap.OrientationHexagonal
	m.StaggerAxis = tilemap.StaggerY
	m.StaggerIndex = tilemap.StaggerOdd

	cm := ai.NewCostMap(7, 7)
	for r := range 6 {
		cm.SetWalkable(3, r, false)
	}
	cm.SetCost(2, 5, 4)

	topo := ai.TopologyFromTilemap(m)
	a := ai.NewAStar()
	a.Topology = topo

	start := ai.Cell{Col: 1, Row: 1}
	goal := ai.Cell{Col: 5, Row: 1}
	path, cost, ok := a.Find(cm, start, goal)
	if !ok {
		t.Fatal("expected hex detour path")
	}
	// Compare optimal cost against brute-force Dijkstra on the same hex map.
	ref := bruteForceDijkstra(cm, topo, false, true, start, math.Inf(1), nil)
	wantCost, reachable := ref[goal]
	if !reachable {
		t.Fatal("reference Dijkstra found goal unreachable")
	}
	if math.Abs(cost-wantCost) > 1e-9 {
		t.Fatalf("hex A* cost = %v, want brute-force Dijkstra %v (path %v)", cost, wantCost, path)
	}
	for i := 1; i < len(path); i++ {
		if !cm.Walkable(path[i].Col, path[i].Row) {
			t.Fatalf("path stepped on blocked hex %v", path[i])
		}
		if d := m.HexDistance(path[i-1].Col, path[i-1].Row, path[i].Col, path[i].Row); d != 1 {
			t.Fatalf("invalid hex step %v -> %v", path[i-1], path[i])
		}
	}
}

func TestAStarStaggeredIsometricPaths(t *testing.T) {
	configs := []struct {
		name  string
		axis  tilemap.StaggerAxis
		index tilemap.StaggerIndex
	}{
		{"stagger_y_odd", tilemap.StaggerY, tilemap.StaggerOdd},
		{"stagger_y_even", tilemap.StaggerY, tilemap.StaggerEven},
		{"stagger_x_odd", tilemap.StaggerX, tilemap.StaggerOdd},
		{"stagger_x_even", tilemap.StaggerX, tilemap.StaggerEven},
	}

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			m := tilemap.New(8, 8, 64, 32)
			m.Orientation = tilemap.OrientationStaggered
			m.StaggerAxis = tc.axis
			m.StaggerIndex = tc.index
			topo := ai.TopologyFromTilemap(m)

			// Verify every 4-way neighbor is physically adjacent by (±tw/2, ±th/2) in world space.
			for r := 1; r < 7; r++ {
				for c := 1; c < 7; c++ {
					center := m.TileToWorld(c, r)
					for _, nb := range topo.Neighbors(ai.Cell{Col: c, Row: r}) {
						nc := m.TileToWorld(nb.Col, nb.Row)
						dx := math.Abs(float64(nc.X - center.X))
						dy := math.Abs(float64(nc.Y - center.Y))
						if math.Abs(dx-32) > 1e-4 || math.Abs(dy-16) > 1e-4 {
							t.Fatalf("staggered neighbor %v of (%d,%d) has world delta (%v,%v), want (32,16)", nb, c, r, dx, dy)
						}
					}
				}
			}

			// Open-ground shortest paths match brute-force Dijkstra for both 4-way and 8-way (with obstacles).
			cm := ai.NewCostMap(8, 8)
			cm.SetWalkable(3, 3, false)
			cm.SetWalkable(3, 4, false)
			cm.SetCost(4, 2, 3)

			for _, diagonal := range []bool{false, true} {
				for _, noCut := range []bool{true, false} {
					a := ai.NewAStar()
					a.Topology = topo
					a.Diagonal = diagonal
					a.NoCornerCutting = noCut

					start := ai.Cell{Col: 1, Row: 1}
					goal := ai.Cell{Col: 6, Row: 6}
					ref := bruteForceDijkstra(cm, topo, diagonal, noCut, start, math.Inf(1), nil)
					wantCost, wantOK := ref[goal]
					path, cost, ok := a.Find(cm, start, goal)
					if ok != wantOK {
						t.Fatalf("diag=%v noCut=%v: ok=%v, want %v", diagonal, noCut, ok, wantOK)
					}
					if ok && math.Abs(cost-wantCost) > 1e-9 {
						t.Fatalf("diag=%v noCut=%v: cost=%v, want %v (path %v)", diagonal, noCut, cost, wantCost, path)
					}
				}
			}
		})
	}
}

type edgeCostAdapter struct {
	*ai.CostMap
	edge func(fromCol, fromRow, toCol, toRow int) float64
}

func (e edgeCostAdapter) EdgeCost(fromCol, fromRow, toCol, toRow int) float64 {
	return e.edge(fromCol, fromRow, toCol, toRow)
}

// bruteForceDijkstra is an independent O(V^2) reference implementation used to cross-check A* and MovementRange.
func bruteForceDijkstra(g ai.Grid, topo ai.GridTopology, diagonal, noCut bool, start ai.Cell, budget float64, edgeCost func(from, to ai.Cell) float64) map[ai.Cell]float64 {
	cols, rows := g.GridSize()
	if cols <= 0 || rows <= 0 || start.Col < 0 || start.Col >= cols || start.Row < 0 || start.Row >= rows || !g.Walkable(start.Col, start.Row) || budget < 0 || math.IsNaN(budget) {
		return nil
	}
	eg, hasEdge := g.(ai.EdgeCostGrid)
	cg, hasCost := g.(ai.CostGrid)

	n := cols * rows
	dist := make([]float64, n)
	used := make([]bool, n)
	for i := range dist {
		dist[i] = math.Inf(1)
	}
	dist[start.Row*cols+start.Col] = 0

	effDiag := diagonal
	switch topo {
	case ai.TopologySquare4:
		effDiag = false
	case ai.TopologySquare8:
		effDiag = true
	case ai.TopologyHexPointyOdd, ai.TopologyHexPointyEven, ai.TopologyHexFlatOdd, ai.TopologyHexFlatEven:
		effDiag = false
	}

	out := make(map[ai.Cell]float64)
	for {
		bestID := -1
		bestD := math.Inf(1)
		for i := range n {
			if !used[i] && dist[i] < bestD {
				bestD = dist[i]
				bestID = i
			}
		}
		if bestID < 0 || bestD > budget {
			break
		}
		used[bestID] = true
		c, r := bestID%cols, bestID/cols
		from := ai.Cell{Col: c, Row: r}
		out[from] = bestD

		for _, st := range refNeighbors(topo, c, r, effDiag) {
			nc, nr := st.col, st.row
			if nc < 0 || nc >= cols || nr < 0 || nr >= rows || !g.Walkable(nc, nr) {
				continue
			}
			if st.diag && noCut {
				if !g.Walkable(st.c1, st.r1) || !g.Walkable(st.c2, st.r2) {
					continue
				}
			}
			var mul float64
			switch {
			case edgeCost != nil:
				mul = edgeCost(from, ai.Cell{Col: nc, Row: nr})
				if mul < 0 || math.IsInf(mul, 0) || math.IsNaN(mul) {
					continue
				}
			case hasEdge:
				mul = eg.EdgeCost(c, r, nc, nr)
				if mul < 0 || math.IsInf(mul, 0) || math.IsNaN(mul) {
					continue
				}
			case hasCost:
				tc := cg.MoveCost(nc, nr)
				if tc > 0 && !math.IsInf(tc, 0) && !math.IsNaN(tc) {
					mul = tc
				} else {
					mul = 1
				}
			default:
				mul = 1
			}
			nd := bestD + st.dist*mul
			nid := nr*cols + nc
			if nd <= budget && nd < dist[nid] {
				dist[nid] = nd
			}
		}
	}
	return out
}

type refStep struct {
	col, row int
	dist     float64
	diag     bool
	c1, r1   int
	c2, r2   int
}

func refNeighbors(topo ai.GridTopology, c, r int, diagonal bool) []refStep {
	if topo.IsHex() {
		nbs := topo.Neighbors(ai.Cell{Col: c, Row: r})
		out := make([]refStep, len(nbs))
		for i, nb := range nbs {
			out[i] = refStep{col: nb.Col, row: nb.Row, dist: 1}
		}
		return out
	}
	if topo.IsStaggered() {
		nbs := topo.Neighbors(ai.Cell{Col: c, Row: r})
		// nbs order is NE, SE, SW, NW
		out := []refStep{
			{col: nbs[0].Col, row: nbs[0].Row, dist: 1},
			{col: nbs[1].Col, row: nbs[1].Row, dist: 1},
			{col: nbs[2].Col, row: nbs[2].Row, dist: 1},
			{col: nbs[3].Col, row: nbs[3].Row, dist: 1},
		}
		if !diagonal {
			return out
		}
		if topo == ai.TopologyStaggeredYOdd || topo == ai.TopologyStaggeredYEven {
			return append(out,
				refStep{col: c, row: r - 2, dist: math.Sqrt2, diag: true, c1: nbs[3].Col, r1: nbs[3].Row, c2: nbs[0].Col, r2: nbs[0].Row},
				refStep{col: c + 1, row: r, dist: math.Sqrt2, diag: true, c1: nbs[0].Col, r1: nbs[0].Row, c2: nbs[1].Col, r2: nbs[1].Row},
				refStep{col: c, row: r + 2, dist: math.Sqrt2, diag: true, c1: nbs[1].Col, r1: nbs[1].Row, c2: nbs[2].Col, r2: nbs[2].Row},
				refStep{col: c - 1, row: r, dist: math.Sqrt2, diag: true, c1: nbs[2].Col, r1: nbs[2].Row, c2: nbs[3].Col, r2: nbs[3].Row},
			)
		}
		return append(out,
			refStep{col: c, row: r - 1, dist: math.Sqrt2, diag: true, c1: nbs[3].Col, r1: nbs[3].Row, c2: nbs[0].Col, r2: nbs[0].Row},
			refStep{col: c + 2, row: r, dist: math.Sqrt2, diag: true, c1: nbs[0].Col, r1: nbs[0].Row, c2: nbs[1].Col, r2: nbs[1].Row},
			refStep{col: c, row: r + 1, dist: math.Sqrt2, diag: true, c1: nbs[1].Col, r1: nbs[1].Row, c2: nbs[2].Col, r2: nbs[2].Row},
			refStep{col: c - 2, row: r, dist: math.Sqrt2, diag: true, c1: nbs[2].Col, r1: nbs[2].Row, c2: nbs[3].Col, r2: nbs[3].Row},
		)
	}
	out := []refStep{
		{col: c, row: r - 1, dist: 1},
		{col: c + 1, row: r, dist: 1},
		{col: c, row: r + 1, dist: 1},
		{col: c - 1, row: r, dist: 1},
	}
	if !diagonal {
		return out
	}
	return append(out,
		refStep{col: c + 1, row: r - 1, dist: math.Sqrt2, diag: true, c1: c + 1, r1: r, c2: c, r2: r - 1},
		refStep{col: c + 1, row: r + 1, dist: math.Sqrt2, diag: true, c1: c + 1, r1: r, c2: c, r2: r + 1},
		refStep{col: c - 1, row: r + 1, dist: math.Sqrt2, diag: true, c1: c - 1, r1: r, c2: c, r2: r + 1},
		refStep{col: c - 1, row: r - 1, dist: math.Sqrt2, diag: true, c1: c - 1, r1: r, c2: c, r2: r - 1},
	)
}

func TestMovementRangeMatchesBruteForceDijkstraOnRandomCostMaps(t *testing.T) {
	topologies := []ai.GridTopology{
		ai.TopologySquare4,
		ai.TopologySquare8,
		ai.TopologyHexPointyOdd,
		ai.TopologyHexPointyEven,
		ai.TopologyHexFlatOdd,
		ai.TopologyHexFlatEven,
		ai.TopologyStaggeredYOdd,
		ai.TopologyStaggeredYEven,
		ai.TopologyStaggeredXOdd,
		ai.TopologyStaggeredXEven,
	}

	rng := rand.New(rand.NewPCG(854, 2026))

	for _, topo := range topologies {
		t.Run(topo.String(), func(t *testing.T) {
			a := ai.NewAStar()
			a.Topology = topo
			if topo == ai.TopologyStaggeredYOdd || topo == ai.TopologyStaggeredXEven {
				a.Diagonal = true
			}

			for trial := range 15 {
				const cols, rows = 10, 10
				cm := ai.NewCostMap(cols, rows)
				for r := range rows {
					for c := range cols {
						v := rng.Float64()
						if v < 0.18 {
							cm.SetWalkable(c, r, false)
						} else {
							cm.SetCost(c, r, 1+float64(rng.IntN(5))*0.5)
						}
					}
				}
				start := ai.Cell{Col: rng.IntN(cols), Row: rng.IntN(rows)}
				cm.SetWalkable(start.Col, start.Row, true)
				budget := 2.0 + float64(rng.IntN(8))

				got := a.MovementRange(cm, start, budget)
				want := bruteForceDijkstra(cm, topo, a.Diagonal, a.NoCornerCutting, start, budget, nil)

				if len(got) != len(want) {
					t.Fatalf("trial %d: len(got)=%d, want %d", trial, len(got), len(want))
				}
				for i, rc := range got {
					wc, ok := want[rc.Cell]
					if !ok {
						t.Fatalf("trial %d: unexpected cell %v in MovementRange", trial, rc.Cell)
					}
					if math.Abs(rc.Cost-wc) > 1e-9 {
						t.Fatalf("trial %d: cell %v cost=%v, want %v", trial, rc.Cell, rc.Cost, wc)
					}
					if i > 0 {
						prev := got[i-1]
						if rc.Cost < prev.Cost-1e-12 {
							t.Fatalf("trial %d: results not sorted by ascending cost: %v before %v", trial, prev, rc)
						}
					}
				}
			}
		})
	}
}

func TestMovementRangeWithEdgeCostsAndEdgeCases(t *testing.T) {
	// Elevation map: climbing uphill costs 1 + diff, moving along roads costs 0.5, cliff drop > 2 is blocked (-1).
	elev := [][]int{
		{0, 0, 1, 4},
		{0, 1, 2, 2},
		{0, 0, 1, 1},
		{0, 0, 0, 1},
	}
	cm := ai.NewCostMap(4, 4)
	edgeFn := func(from, to ai.Cell) float64 {
		diff := elev[to.Row][to.Col] - elev[from.Row][from.Col]
		if diff > 2 || diff < -2 {
			return -1
		}
		if diff > 0 {
			return 1 + float64(diff)
		}
		if elev[from.Row][from.Col] == 0 && elev[to.Row][to.Col] == 0 {
			return 0.5 // road bonus
		}
		return 1
	}

	topo := ai.TopologyHexPointyOdd
	start := ai.Cell{Col: 0, Row: 0}
	budget := 3.0

	got := ai.ReachableCells(cm, topo, start, budget, edgeFn)
	want := bruteForceDijkstra(cm, topo, false, true, start, budget, edgeFn)
	if len(got) != len(want) {
		t.Fatalf("ReachableCells len=%d, want %d: %v", len(got), len(want), got)
	}
	for _, rc := range got {
		if wc, ok := want[rc.Cell]; !ok || math.Abs(rc.Cost-wc) > 1e-9 {
			t.Fatalf("cell %v cost=%v, want %v (ok=%v)", rc.Cell, rc.Cost, wc, ok)
		}
	}

	// Same edge costs via EdgeCostGrid interface.
	eg := edgeCostAdapter{
		CostMap: cm,
		edge: func(fc, fr, tc, tr int) float64 {
			return edgeFn(ai.Cell{Col: fc, Row: fr}, ai.Cell{Col: tc, Row: tr})
		},
	}
	a := ai.NewAStar()
	a.Topology = topo
	gotIface := a.MovementRange(eg, start, budget)
	if !slices.Equal(got, gotIface) {
		t.Fatalf("EdgeCostGrid MovementRange differed from callback:\ngot:  %v\nwant: %v", gotIface, got)
	}

	// Edge cases: budget == 0, negative budget, NaN, nil grid, blocked start.
	if r0 := a.MovementRange(cm, start, 0); !slices.Equal(r0, []ai.ReachableCell{{Cell: start, Cost: 0}}) {
		t.Fatalf("budget=0 gave %v, want [{%v 0}]", r0, start)
	}
	if rn := a.MovementRange(cm, start, -1); len(rn) != 0 {
		t.Fatalf("negative budget gave %v, want empty", rn)
	}
	if rnan := a.MovementRange(cm, start, math.NaN()); len(rnan) != 0 {
		t.Fatalf("NaN budget gave %v, want empty", rnan)
	}
	if rnil := a.MovementRange(nil, start, 5); len(rnil) != 0 {
		t.Fatalf("nil grid gave %v, want empty", rnil)
	}
	cm.SetWalkable(start.Col, start.Row, false)
	if rb := a.MovementRange(cm, start, 5); len(rb) != 0 {
		t.Fatalf("blocked start gave %v, want empty", rb)
	}
}

func TestHexAStarAndMovementRangeZeroAllocations(t *testing.T) {
	g := ai.NewBoolGrid(24, 24)
	for r := 4; r < 20; r++ {
		g.SetWalkable(12, r, false)
	}
	a := ai.NewAStar()
	a.Topology = ai.TopologyHexPointyOdd

	pathBuf := make([]ai.Cell, 0, 64)
	rangeBuf := make([]ai.ReachableCell, 0, 256)
	start := ai.Cell{Col: 3, Row: 10}
	goal := ai.Cell{Col: 20, Row: 10}

	// Warm up internal buffers.
	var ok bool
	pathBuf, _, ok = a.FindInto(pathBuf, g, start, goal)
	if !ok {
		t.Fatal("warm-up hex FindInto failed")
	}
	rangeBuf = a.MovementRangeInto(rangeBuf, g, start, 6)
	if len(rangeBuf) == 0 {
		t.Fatal("warm-up MovementRangeInto returned empty")
	}

	findAllocs := testing.AllocsPerRun(50, func() {
		pathBuf, _, ok = a.FindInto(pathBuf[:0], g, start, goal)
		if !ok {
			t.Fatal("hex FindInto failed")
		}
	})
	if findAllocs != 0 {
		t.Fatalf("hex FindInto allocated %v times per run, want 0", findAllocs)
	}

	rangeAllocs := testing.AllocsPerRun(50, func() {
		rangeBuf = a.MovementRangeInto(rangeBuf[:0], g, start, 6)
		if len(rangeBuf) == 0 {
			t.Fatal("MovementRangeInto returned empty")
		}
	})
	if rangeAllocs != 0 {
		t.Fatalf("MovementRangeInto allocated %v times per run, want 0", rangeAllocs)
	}
}
