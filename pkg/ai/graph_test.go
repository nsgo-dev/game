package ai_test

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func TestHexGridAllStaggerConfigsMatchReference(t *testing.T) {
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

	const cols, rows = 7, 7
	gs := ai.NewGraphSearch[ai.Cell]()

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			m := tilemap.New(cols, rows, 32, 36)
			m.Orientation = tilemap.OrientationHexagonal
			m.StaggerAxis = tc.axis
			m.StaggerIndex = tc.index

			hg := ai.NewHexGrid(m)
			astar := ai.NewAStar()
			astar.Topology = ai.TopologyFromTilemap(m)

			for sr := range rows {
				for sc := range cols {
					start := ai.Cell{Col: sc, Row: sr}
					for gr := range rows {
						for gc := range cols {
							goal := ai.Cell{Col: gc, Row: gr}
							path, cost, ok := gs.Find(hg, start, goal)
							if !ok {
								t.Fatalf("no path from %v to %v", start, goal)
							}
							wantSteps := m.HexDistance(sc, sr, gc, gr)
							if len(path)-1 != wantSteps {
								t.Fatalf("%v->%v: len-1 = %d, want HexDistance %d (%v)", start, goal, len(path)-1, wantSteps, path)
							}
							if math.Abs(cost-float64(wantSteps)) > 1e-9 {
								t.Fatalf("%v->%v: cost = %v, want %d", start, goal, cost, wantSteps)
							}
							// Must match ai.AStar path-for-path thanks to identical row-major tie-breaking.
							aPath, aCost, aOK := astar.Find(hg, start, goal)
							if !aOK || math.Abs(aCost-cost) > 1e-9 || !slices.Equal(path, aPath) {
								t.Fatalf("%v->%v: GraphSearch=(%v,%v) != AStar=(%v,%v,%v)", start, goal, path, cost, aPath, aCost, aOK)
							}
						}
					}
				}
			}
		})
	}
}

func TestHexGridRangeWithPerCellCostsAndPathsAllStaggerConfigs(t *testing.T) {
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

	rng := rand.New(rand.NewPCG(831, 2026))
	gs := ai.NewGraphSearch[ai.Cell]()

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			for trial := range 10 {
				const cols, rows = 9, 9
				m := tilemap.New(cols, rows, 32, 36)
				m.Orientation = tilemap.OrientationHexagonal
				m.StaggerAxis = tc.axis
				m.StaggerIndex = tc.index

				costs := make([]float64, cols*rows)
				for r := range rows {
					for c := range cols {
						v := rng.Float64()
						if v < 0.2 {
							m.SetCellFlags(c, r, tilemap.TileSolid)
							costs[r*cols+c] = 1
						} else {
							// Include fractional, unit, and heavy terrain costs (including < 1 road tiles).
							costs[r*cols+c] = 0.5 + float64(rng.IntN(6))*0.5
						}
					}
				}
				start := ai.Cell{Col: rng.IntN(cols), Row: rng.IntN(rows)}
				m.ClearCellFlags(start.Col, start.Row)

				hg := ai.NewHexGrid(m)
				hg.Cost = func(col, row int) float64 {
					return costs[row*cols+col]
				}

				budget := 2.0 + float64(rng.IntN(7))*0.5
				got := gs.Range(hg, start, budget)

				// Independent brute-force Dijkstra reference over tilemap.HexNeighbors.
				want := hexRefDijkstra(m, hg, start, budget)

				if got.Len() != len(want) {
					t.Fatalf("trial %d: Range.Len() = %d, want %d", trial, got.Len(), len(want))
				}
				gotStart, ok := got.Start()
				if !ok || gotStart != start {
					t.Fatalf("trial %d: Start() = (%v, %v), want (%v, true)", trial, gotStart, ok, start)
				}
				if got.Budget() != budget {
					t.Fatalf("trial %d: Budget() = %v, want %v", trial, got.Budget(), budget)
				}

				nodes := got.Nodes()
				var pathBuf []ai.Cell
				for i, rn := range nodes {
					wc, ok := want[rn.Node]
					if !ok {
						t.Fatalf("trial %d: unexpected reachable cell %v", trial, rn.Node)
					}
					if math.Abs(rn.Cost-wc) > 1e-9 {
						t.Fatalf("trial %d: cell %v cost = %v, want %v", trial, rn.Node, rn.Cost, wc)
					}
					if rn.Cost > budget+1e-12 {
						t.Fatalf("trial %d: cell %v cost %v exceeds budget %v", trial, rn.Node, rn.Cost, budget)
					}
					if !got.Contains(rn.Node) {
						t.Fatalf("trial %d: Contains(%v) = false", trial, rn.Node)
					}
					if c, ok := got.Cost(rn.Node); !ok || c != rn.Cost {
						t.Fatalf("trial %d: Cost(%v) = (%v, %v), want (%v, true)", trial, rn.Node, c, ok, rn.Cost)
					}
					// Verify deterministic ascending cost, then row-major tie-break order.
					if i > 0 {
						prev := nodes[i-1]
						if rn.Cost < prev.Cost-1e-12 {
							t.Fatalf("trial %d: not sorted by cost: %v before %v", trial, prev, rn)
						}
						if rn.Cost == prev.Cost && !hg.NodeLess(prev.Node, rn.Node) {
							t.Fatalf("trial %d: tie-break order violated: %v before %v", trial, prev, rn)
						}
					}

					// Verify reconstructed path via both Path and PathInto.
					path, ok := got.Path(rn.Node)
					if !ok || len(path) == 0 {
						t.Fatalf("trial %d: Path(%v) failed", trial, rn.Node)
					}
					pathBuf, ok = got.PathInto(pathBuf[:0], rn.Node)
					if !ok || !slices.Equal(path, pathBuf) {
						t.Fatalf("trial %d: PathInto(%v) = (%v, %v), want %v", trial, rn.Node, pathBuf, ok, path)
					}
					if path[0] != start || path[len(path)-1] != rn.Node {
						t.Fatalf("trial %d: path endpoints %v..%v, want %v..%v", trial, path[0], path[len(path)-1], start, rn.Node)
					}
					if rn.Node == start {
						if _, hasParent := got.Parent(start); hasParent {
							t.Fatalf("trial %d: start should have no parent", trial)
						}
					} else {
						parent, hasParent := got.Parent(rn.Node)
						if !hasParent || parent != path[len(path)-2] {
							t.Fatalf("trial %d: Parent(%v) = (%v, %v), want %v", trial, rn.Node, parent, hasParent, path[len(path)-2])
						}
					}
					sum := 0.0
					for k := 1; k < len(path); k++ {
						if d := m.HexDistance(path[k-1].Col, path[k-1].Row, path[k].Col, path[k].Row); d != 1 {
							t.Fatalf("trial %d: non-adjacent hex step %v -> %v", trial, path[k-1], path[k])
						}
						if !hg.Walkable(path[k].Col, path[k].Row) {
							t.Fatalf("trial %d: path stepped on unwalkable %v", trial, path[k])
						}
						sum += costs[path[k].Row*cols+path[k].Col]
					}
					if math.Abs(sum-rn.Cost) > 1e-9 {
						t.Fatalf("trial %d: path cost sum = %v, want %v (path %v)", trial, sum, rn.Cost, path)
					}
				}

				// Verify every unreachable cell reports false.
				for r := range rows {
					for c := range cols {
						cell := ai.Cell{Col: c, Row: r}
						if _, reachable := want[cell]; reachable {
							continue
						}
						if got.Contains(cell) {
							t.Fatalf("trial %d: unreachable cell %v reported contained", trial, cell)
						}
						if _, ok := got.Cost(cell); ok {
							t.Fatalf("trial %d: unreachable cell %v returned cost", trial, cell)
						}
						if _, ok := got.Path(cell); ok {
							t.Fatalf("trial %d: unreachable cell %v returned path", trial, cell)
						}
					}
				}
			}
		})
	}
}

func TestHexGridCustomMaskWalkAndStepCost(t *testing.T) {
	m := tilemap.New(5, 5, 32, 36)
	m.Orientation = tilemap.OrientationHexagonal
	m.StaggerAxis = tilemap.StaggerY
	m.StaggerIndex = tilemap.StaggerOdd

	const flagWater tilemap.TileFlags = 1 << 4
	m.SetCellFlags(2, 1, flagWater)
	m.SetCellFlags(2, 2, flagWater)
	m.SetCellFlags(2, 3, tilemap.TileSolid)

	hg := &ai.HexGrid{
		Map:       m,
		SolidMask: tilemap.TileSolid | flagWater,
		StepCost: func(from, to ai.Cell) float64 {
			if to.Row == 0 {
				return 5 // expensive northern ridge
			}
			return 1
		},
	}

	gs := ai.NewGraphSearch[ai.Cell]()
	start := ai.Cell{Col: 1, Row: 2}
	goal := ai.Cell{Col: 3, Row: 2}
	path, cost, ok := gs.Find(hg, start, goal)
	if !ok {
		t.Fatal("expected detour path around water/solid barrier")
	}
	for _, c := range path {
		if (c.Col == 2 && (c.Row == 1 || c.Row == 2 || c.Row == 3)) || c.Row == 0 {
			t.Fatalf("path stepped on blocked or expensive cell %v: %v", c, path)
		}
	}
	// Verify Dijkstra agrees with A* on the same graph.
	dPath, dCost, dOK := gs.FindDijkstra(hg, start, goal)
	if !dOK || math.Abs(dCost-cost) > 1e-9 || !slices.Equal(path, dPath) {
		t.Fatalf("Dijkstra=(%v, %v, %v), want A*=(%v, %v)", dPath, dCost, dOK, path, cost)
	}

	// Negative Cost blocks a cell when StepCost is nil.
	hgCost := ai.NewHexGrid(m)
	hgCost.Cost = func(col, row int) float64 {
		if col == 1 && row == 2 {
			return -1
		}
		return 1
	}
	if _, _, ok := gs.Find(hgCost, start, goal); ok {
		t.Fatal("negative-cost start cell should be rejected")
	}
	if r := gs.Range(hgCost, start, 5); r.Len() != 0 {
		t.Fatalf("negative-cost start should yield empty range, got %d", r.Len())
	}
}

func hexRefDijkstra(m *tilemap.Tilemap, hg *ai.HexGrid, start ai.Cell, budget float64) map[ai.Cell]float64 {
	cols, rows := m.Width, m.Height
	if !hg.Walkable(start.Col, start.Row) || budget < 0 || math.IsNaN(budget) {
		return nil
	}
	n := cols * rows
	dist := make([]float64, n)
	used := make([]bool, n)
	for i := range dist {
		dist[i] = math.Inf(1)
	}
	dist[start.Row*cols+start.Col] = 0
	out := make(map[ai.Cell]float64)

	for {
		best := -1
		bestD := math.Inf(1)
		for i := range n {
			if !used[i] && dist[i] < bestD {
				bestD = dist[i]
				best = i
			}
		}
		if best < 0 || bestD > budget {
			break
		}
		used[best] = true
		c, r := best%cols, best/cols
		out[ai.Cell{Col: c, Row: r}] = bestD

		for _, nb := range m.HexNeighbors(c, r) {
			nc, nr := nb[0], nb[1]
			if !hg.Walkable(nc, nr) {
				continue
			}
			sc := hg.EdgeCost(c, r, nc, nr)
			if sc < 0 || math.IsNaN(sc) || math.IsInf(sc, 0) {
				continue
			}
			nd := bestD + sc
			nid := nr*cols + nc
			if nd <= budget && nd < dist[nid] {
				dist[nid] = nd
			}
		}
	}
	return out
}

func TestWaypointGraph2D3DAndOperations(t *testing.T) {
	wg := ai.NewWaypointGraph()

	// Build a 2D diamond with a portal shortcut and a one-way bridge.
	a := wg.Add(core.Point{X: 0, Y: 0})
	b := wg.Add(core.Point{X: 10, Y: 0})
	c := wg.Add(core.Point{X: 10, Y: 10})
	d := wg.Add(core.Point{X: 0, Y: 10})
	e := wg.Add(core.Point{X: 20, Y: 10})

	if wg.NodeCount() != 5 {
		t.Fatalf("NodeCount = %d, want 5", wg.NodeCount())
	}

	wg.ConnectUndirected(a, b) // cost 10
	wg.ConnectUndirected(b, c) // cost 10
	wg.ConnectUndirected(a, d) // cost 10
	wg.ConnectUndirected(d, c) // cost 10
	wg.AddEdge(c, e, 10)       // directed c -> e only
	wg.AddEdge(b, e, 50)       // expensive directed edge b -> e

	gs := ai.NewGraphSearch[int]()

	// A* from a to e: a->b->c->e (30) and a->d->c->e (30); tie-break prefers lower ID b over d.
	path, cost, ok := gs.Find(wg, a, e)
	if !ok {
		t.Fatal("expected waypoint path a -> e")
	}
	if math.Abs(cost-30) > 1e-9 || !slices.Equal(path, []int{a, b, c, e}) {
		t.Fatalf("a->e = (%v, %v), want ([0 1 2 4], 30)", path, cost)
	}

	// Directed edge c -> e is not traversable in reverse.
	if _, _, ok := gs.Find(wg, e, a); ok {
		t.Fatal("expected no reverse path from e to a across one-way edge")
	}

	// Disabling b forces path through d.
	wg.SetEnabled(b, false)
	if wg.Enabled(b) {
		t.Fatal("b should be disabled")
	}
	path2, cost2, ok2 := gs.Find(wg, a, e)
	if !ok2 || math.Abs(cost2-30) > 1e-9 || !slices.Equal(path2, []int{a, d, c, e}) {
		t.Fatalf("a->e with b disabled = (%v, %v, %v), want ([0 3 2 4], 30)", path2, cost2, ok2)
	}

	// Nearest skips disabled b, picking c over b at (9, 8) and breaking exact ties by lowest ID.
	if nearest, ok := wg.Nearest(core.Point{X: 5, Y: 5}); !ok || nearest != a {
		t.Fatalf("Nearest(5,5) = (%d, %v), want (%d, true) by lowest-ID tie-break", nearest, ok, a)
	}

	wg.SetEnabled(b, true)

	// Moving b updates Connect auto-cost edges dynamically.
	wg.SetPos(b, core.Point{X: 10, Y: -20})
	// Now a->b is sqrt(500) ≈ 22.36, so a->d->c->e (30) strictly beats a->b->c->e.
	pathMoved, costMoved, okMoved := gs.Find(wg, a, e)
	if !okMoved || math.Abs(costMoved-30) > 1e-9 || !slices.Equal(pathMoved, []int{a, d, c, e}) {
		t.Fatalf("after moving b: (%v, %v, %v), want ([0 3 2 4], 30)", pathMoved, costMoved, okMoved)
	}

	// Points conversion helper.
	pts := wg.Points(pathMoved)
	wantPts := []core.Point{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 20, Y: 10}}
	if !slices.Equal(pts, wantPts) {
		t.Fatalf("Points = %v, want %v", pts, wantPts)
	}

	// Shortcut edge cheaper than Euclidean distance: Dijkstra finds it optimally.
	wg.AddEdge(a, e, 5)
	dPath, dCost, dOK := gs.FindDijkstra(wg, a, e)
	if !dOK || math.Abs(dCost-5) > 1e-9 || !slices.Equal(dPath, []int{a, e}) {
		t.Fatalf("Dijkstra shortcut = (%v, %v, %v), want ([0 4], 5)", dPath, dCost, dOK)
	}

	// Range query on waypoint graph with budget 20 from a: reaches a(0), e(5), d(10), c(20).
	rng := gs.Range(wg, a, 20)
	wantRange := []ai.ReachableNode[int]{
		{Node: a, Cost: 0},
		{Node: e, Cost: 5},
		{Node: d, Cost: 10},
		{Node: c, Cost: 20},
	}
	if !slices.Equal(rng.Nodes(), wantRange) {
		t.Fatalf("waypoint range = %v, want %v", rng.Nodes(), wantRange)
	}
	if cp, ok := rng.Path(c); !ok || !slices.Equal(cp, []int{a, d, c}) {
		t.Fatalf("range path to c = (%v, %v), want ([0 3 2], true)", cp, ok)
	}

	// Remove node d keeps IDs of a, b, c, e intact and cleans up incident edges.
	if !wg.Remove(d) || wg.Remove(d) {
		t.Fatal("Remove(d) should succeed once and then return false")
	}
	if wg.NodeCount() != 4 {
		t.Fatalf("NodeCount after Remove = %d, want 4", wg.NodeCount())
	}
	for _, edge := range wg.Edges(a) {
		if edge.To == d {
			t.Fatalf("removed node d still appears in a's edges: %v", wg.Edges(a))
		}
	}
	if _, _, ok := gs.Find(wg, a, d); ok {
		t.Fatal("search to removed node should fail")
	}

	// Edge validation rejects negative, NaN, Inf.
	if wg.AddEdge(a, c, -1) || wg.AddEdge(a, c, math.NaN()) || wg.AddEdge(a, c, math.Inf(1)) {
		t.Fatal("invalid edge costs should be rejected")
	}
	if wg.RemoveEdge(a, e) != true || wg.RemoveEdge(a, e) != false {
		t.Fatal("RemoveEdge should return true once then false")
	}
}

func TestWaypointGraph3DAndEdgeOrderDeterminism(t *testing.T) {
	// Construct two identical 3D graphs with edges inserted in opposite orders.
	build := func(reverse bool) (*ai.WaypointGraph, int, int) {
		g := ai.NewWaypointGraph()
		n0 := g.Add3D(ai.Vec3{X: 0, Y: 0, Z: 0})
		n1 := g.Add3D(ai.Vec3{X: 3, Y: 4, Z: 0}) // dist 5 from n0
		n2 := g.Add3D(ai.Vec3{X: 0, Y: 4, Z: 3}) // dist 5 from n0
		n3 := g.Add3D(ai.Vec3{X: 3, Y: 4, Z: 3}) // dist 3 from n1 and n2
		edges := [][2]int{{n0, n1}, {n0, n2}, {n1, n3}, {n2, n3}}
		if reverse {
			slices.Reverse(edges)
		}
		for _, e := range edges {
			g.ConnectUndirected(e[0], e[1])
		}
		return g, n0, n3
	}

	g1, s1, e1 := build(false)
	g2, s2, e2 := build(true)
	gs := ai.NewGraphSearch[int]()

	p1, c1, ok1 := gs.Find(g1, s1, e1)
	p2, c2, ok2 := gs.Find(g2, s2, e2)
	if !ok1 || !ok2 || math.Abs(c1-8) > 1e-9 || c1 != c2 || !slices.Equal(p1, p2) {
		t.Fatalf("3D waypoint mismatch: (%v,%v,%v) vs (%v,%v,%v)", p1, c1, ok1, p2, c2, ok2)
	}
	if !slices.Equal(p1, []int{0, 1, 3}) {
		t.Fatalf("tie-break chose %v, want [0 1 3]", p1)
	}
	if id, ok := g1.Nearest3D(ai.Vec3{X: 2.9, Y: 4, Z: 2.9}); !ok || id != 3 {
		t.Fatalf("Nearest3D = (%d, %v), want (3, true)", id, ok)
	}
	pts3 := g1.Points3D(p1)
	want3 := []ai.Vec3{{X: 0, Y: 0, Z: 0}, {X: 3, Y: 4, Z: 0}, {X: 3, Y: 4, Z: 3}}
	if !slices.Equal(pts3, want3) {
		t.Fatalf("Points3D = %v, want %v", pts3, want3)
	}
}

type cell3D struct {
	X, Y, Z int
}

type grid3D struct {
	sx, sy, sz int
	blocked    map[cell3D]bool
}

func (g *grid3D) ValidNode(c cell3D) bool {
	if c.X < 0 || c.X >= g.sx || c.Y < 0 || c.Y >= g.sy || c.Z < 0 || c.Z >= g.sz {
		return false
	}
	return !g.blocked[c]
}

func (g *grid3D) AppendNeighbors(dst []ai.GraphEdge[cell3D], c cell3D) []ai.GraphEdge[cell3D] {
	dirs := [6]cell3D{
		{1, 0, 0}, {-1, 0, 0},
		{0, 1, 0}, {0, -1, 0},
		{0, 0, 1}, {0, 0, -1},
	}
	for _, d := range dirs {
		n := cell3D{X: c.X + d.X, Y: c.Y + d.Y, Z: c.Z + d.Z}
		if g.ValidNode(n) {
			dst = append(dst, ai.GraphEdge[cell3D]{To: n, Cost: 1})
		}
	}
	return dst
}

func (g *grid3D) Heuristic(a, b cell3D) float64 {
	dx := a.X - b.X
	if dx < 0 {
		dx = -dx
	}
	dy := a.Y - b.Y
	if dy < 0 {
		dy = -dy
	}
	dz := a.Z - b.Z
	if dz < 0 {
		dz = -dz
	}
	return float64(dx + dy + dz)
}

func TestGraphSearchCustom3DGridAndEdgeCases(t *testing.T) {
	g := &grid3D{
		sx: 5, sy: 5, sz: 5,
		blocked: map[cell3D]bool{
			{1, 0, 0}: true,
			{1, 1, 0}: true,
			{1, 0, 1}: true,
		},
	}
	gs := ai.NewGraphSearch[cell3D]()
	gs.Less = func(a, b cell3D) bool {
		if a.Z != b.Z {
			return a.Z < b.Z
		}
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.X < b.X
	}

	start := cell3D{0, 0, 0}
	goal := cell3D{4, 4, 4}
	path, cost, ok := gs.Find(g, start, goal)
	if !ok || math.Abs(cost-12) > 1e-9 || len(path) != 13 {
		t.Fatalf("3D A* = (%d nodes, %v, %v), want (13, 12, true)", len(path), cost, ok)
	}

	// Range on 3D grid with budget 2.
	rng := gs.Range(g, start, 2)
	if !rng.Contains(cell3D{0, 2, 0}) || rng.Contains(cell3D{1, 0, 0}) || rng.Contains(cell3D{0, 3, 0}) {
		t.Fatalf("unexpected 3D range contents: %v", rng.Nodes())
	}

	// GraphFunc with 0-cost edges, negative/NaN/Inf edges, and MaxExpansions.
	fg := ai.GraphFunc[string]{
		Neighbors: func(dst []ai.GraphEdge[string], n string) []ai.GraphEdge[string] {
			switch n {
			case "A":
				return append(dst,
					ai.GraphEdge[string]{To: "B", Cost: 0},
					ai.GraphEdge[string]{To: "C", Cost: -5},
					ai.GraphEdge[string]{To: "C", Cost: math.NaN()},
					ai.GraphEdge[string]{To: "C", Cost: math.Inf(1)},
					ai.GraphEdge[string]{To: "D", Cost: 10},
				)
			case "B":
				return append(dst, ai.GraphEdge[string]{To: "D", Cost: 2})
			default:
				return dst
			}
		},
		Estimate: func(from, goal string) float64 { return math.NaN() }, // sanitized safely to 0
	}
	sgs := ai.NewGraphSearch[string]()
	sp, sc, sok := sgs.Find(fg, "A", "D")
	if !sok || sc != 2 || !slices.Equal(sp, []string{"A", "B", "D"}) {
		t.Fatalf("GraphFunc = (%v, %v, %v), want ([A B D], 2, true)", sp, sc, sok)
	}

	// Budget 0 includes 0-cost neighbor B!
	r0 := sgs.Range(fg, "A", 0)
	if !r0.Contains("A") || !r0.Contains("B") || r0.Contains("D") {
		t.Fatalf("budget=0 with 0-cost edge gave %v", r0.Nodes())
	}

	// MaxExpansions stops search early.
	sgs.MaxExpansions = 1
	if _, _, ok := sgs.Find(fg, "A", "D"); ok {
		t.Fatal("MaxExpansions=1 should fail before reaching D")
	}
	sgs.MaxExpansions = 0

	// Edge cases: nil graph, start == goal, negative/NaN budget.
	if _, _, ok := sgs.Find(nil, "A", "B"); ok {
		t.Fatal("nil graph should fail")
	}
	if p, c, ok := sgs.Find(fg, "A", "A"); !ok || c != 0 || !slices.Equal(p, []string{"A"}) {
		t.Fatalf("start==goal gave (%v, %v, %v)", p, c, ok)
	}
	if rn := sgs.Range(fg, "A", -1); rn.Len() != 0 {
		t.Fatalf("negative budget gave %d nodes, want 0", rn.Len())
	}
	if rnan := sgs.Range(fg, "A", math.NaN()); rnan.Len() != 0 {
		t.Fatalf("NaN budget gave %d nodes, want 0", rnan.Len())
	}
}

func TestGraphSearchReusedBuffersAllocations(t *testing.T) {
	// 1. HexGrid repeated searches: FindInto / FindDijkstraInto / RangeInto / PathInto -> 0 allocs/op;
	// Find -> 1 alloc/op (only the returned path slice); unreachable Find -> 0 allocs/op.
	m := tilemap.New(20, 20, 32, 36)
	m.Orientation = tilemap.OrientationHexagonal
	m.StaggerAxis = tilemap.StaggerY
	m.StaggerIndex = tilemap.StaggerOdd
	for r := 3; r < 17; r++ {
		m.SetCellFlags(10, r, tilemap.TileSolid)
	}
	hg := ai.NewHexGrid(m)
	gs := ai.NewGraphSearch[ai.Cell]()

	start := ai.Cell{Col: 2, Row: 10}
	goal := ai.Cell{Col: 17, Row: 10}
	pathBuf := make([]ai.Cell, 0, 64)
	var ok bool

	// Warm up buffers.
	pathBuf, _, ok = gs.FindInto(pathBuf, hg, start, goal)
	if !ok {
		t.Fatal("warm-up HexGrid FindInto failed")
	}
	rangeDst := gs.RangeInto(nil, hg, start, 6)
	if rangeDst.Len() == 0 {
		t.Fatal("warm-up HexGrid RangeInto returned empty")
	}
	targetCell := rangeDst.Nodes()[rangeDst.Len()-1].Node

	if allocs := testing.AllocsPerRun(50, func() {
		pathBuf, _, ok = gs.FindInto(pathBuf[:0], hg, start, goal)
		if !ok {
			t.Fatal("FindInto failed")
		}
	}); allocs != 0 {
		t.Fatalf("HexGrid FindInto allocated %v times/run, want 0", allocs)
	}

	if allocs := testing.AllocsPerRun(50, func() {
		p, _, found := gs.Find(hg, start, goal)
		if !found || len(p) == 0 {
			t.Fatal("Find failed")
		}
	}); allocs != 1 {
		t.Fatalf("HexGrid Find allocated %v times/run, want 1 (result path slice only)", allocs)
	}

	if allocs := testing.AllocsPerRun(50, func() {
		pathBuf, _, ok = gs.FindDijkstraInto(pathBuf[:0], hg, start, goal)
		if !ok {
			t.Fatal("FindDijkstraInto failed")
		}
	}); allocs != 0 {
		t.Fatalf("HexGrid FindDijkstraInto allocated %v times/run, want 0", allocs)
	}

	if allocs := testing.AllocsPerRun(50, func() {
		rangeDst = gs.RangeInto(rangeDst, hg, start, 6)
		if rangeDst.Len() == 0 {
			t.Fatal("RangeInto failed")
		}
	}); allocs != 0 {
		t.Fatalf("HexGrid RangeInto allocated %v times/run, want 0", allocs)
	}

	if allocs := testing.AllocsPerRun(50, func() {
		pathBuf, ok = rangeDst.PathInto(pathBuf[:0], targetCell)
		if !ok {
			t.Fatal("PathInto failed")
		}
	}); allocs != 0 {
		t.Fatalf("GraphRange.PathInto allocated %v times/run, want 0", allocs)
	}

	// Unreachable Find allocates 0 times.
	for r := range 20 {
		m.SetCellFlags(10, r, tilemap.TileSolid)
	}
	if _, _, ok := gs.Find(hg, start, goal); ok {
		t.Fatal("expected unreachable hex goal")
	}
	if allocs := testing.AllocsPerRun(50, func() {
		if _, _, ok := gs.Find(hg, start, goal); ok {
			t.Fatal("expected unreachable hex goal")
		}
	}); allocs != 0 {
		t.Fatalf("unreachable HexGrid Find allocated %v times/run, want 0", allocs)
	}

	// 2. WaypointGraph repeated searches -> 0 allocs/op.
	wg := ai.NewWaypointGraph()
	const gridN = 6
	ids := make([]int, gridN*gridN)
	for r := range gridN {
		for c := range gridN {
			ids[r*gridN+c] = wg.Add(core.Point{X: core.Points(c * 16), Y: core.Points(r * 16)})
		}
	}
	for r := range gridN {
		for c := range gridN {
			id := ids[r*gridN+c]
			if c+1 < gridN {
				wg.ConnectUndirected(id, ids[r*gridN+c+1])
			}
			if r+1 < gridN {
				wg.ConnectUndirected(id, ids[(r+1)*gridN+c])
			}
		}
	}

	wgs := ai.NewGraphSearch[int]()
	idBuf := make([]int, 0, 32)
	ptBuf := make([]core.Point, 0, 32)
	idBuf, _, ok = wgs.FindInto(idBuf, wg, ids[0], ids[len(ids)-1])
	if !ok {
		t.Fatal("warm-up WaypointGraph FindInto failed")
	}
	wRange := wgs.RangeInto(nil, wg, ids[0], 64)

	if allocs := testing.AllocsPerRun(50, func() {
		idBuf, _, ok = wgs.FindInto(idBuf[:0], wg, ids[0], ids[len(ids)-1])
		if !ok {
			t.Fatal("WaypointGraph FindInto failed")
		}
		ptBuf = wg.PointsInto(ptBuf[:0], idBuf)
	}); allocs != 0 {
		t.Fatalf("WaypointGraph FindInto+PointsInto allocated %v times/run, want 0", allocs)
	}

	if allocs := testing.AllocsPerRun(50, func() {
		wRange = wgs.RangeInto(wRange, wg, ids[0], 64)
		if wRange.Len() == 0 {
			t.Fatal("WaypointGraph RangeInto failed")
		}
	}); allocs != 0 {
		t.Fatalf("WaypointGraph RangeInto allocated %v times/run, want 0", allocs)
	}
}

func BenchmarkHexGridAStar(b *testing.B) {
	m := tilemap.New(32, 32, 32, 36)
	m.Orientation = tilemap.OrientationHexagonal
	m.StaggerAxis = tilemap.StaggerY
	m.StaggerIndex = tilemap.StaggerOdd
	for r := 4; r < 28; r++ {
		m.SetCellFlags(16, r, tilemap.TileSolid)
	}
	hg := ai.NewHexGrid(m)
	gs := ai.NewGraphSearch[ai.Cell]()
	start := ai.Cell{Col: 2, Row: 16}
	goal := ai.Cell{Col: 29, Row: 16}
	buf := make([]ai.Cell, 0, 128)
	buf, _, _ = gs.FindInto(buf, hg, start, goal)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		buf, _, _ = gs.FindInto(buf[:0], hg, start, goal)
	}
}

func BenchmarkHexGridRange(b *testing.B) {
	m := tilemap.New(32, 32, 32, 36)
	m.Orientation = tilemap.OrientationHexagonal
	m.StaggerAxis = tilemap.StaggerY
	m.StaggerIndex = tilemap.StaggerOdd
	hg := ai.NewHexGrid(m)
	gs := ai.NewGraphSearch[ai.Cell]()
	start := ai.Cell{Col: 16, Row: 16}
	dst := gs.RangeInto(nil, hg, start, 6)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		dst = gs.RangeInto(dst, hg, start, 6)
	}
}
