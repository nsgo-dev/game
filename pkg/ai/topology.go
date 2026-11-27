package ai

import (
	"cmp"
	"fmt"
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/tilemap"
)

// GridTopology selects the cell adjacency and admissible distance heuristic used by AStar and movement-range queries.
//
// The zero value (TopologySquare) uses the square grid (4-way or 8-way depending on AStar.Diagonal), preserving backward compatibility with existing AStar callers.
type GridTopology uint8

const (
	// TopologySquare is an orthogonal (or diamond isometric) square grid.
	// With AStar.Diagonal false it uses 4-way cardinal neighbors and Manhattan distance;
	// with AStar.Diagonal true it uses 8-way neighbors (respecting NoCornerCutting) and octile distance.
	TopologySquare GridTopology = iota
	// TopologySquare4 forces 4-way cardinal adjacency and Manhattan distance regardless of AStar.Diagonal.
	TopologySquare4
	// TopologySquare8 forces 8-way adjacency (respecting AStar.NoCornerCutting) and octile distance regardless of AStar.Diagonal.
	TopologySquare8
	// TopologyHexPointyOdd is a pointy-top hexagonal grid with odd rows shifted right (StaggerY, StaggerOdd; Tiled default).
	TopologyHexPointyOdd
	// TopologyHexPointyEven is a pointy-top hexagonal grid with even rows shifted right (StaggerY, StaggerEven).
	TopologyHexPointyEven
	// TopologyHexFlatOdd is a flat-top hexagonal grid with odd columns shifted down (StaggerX, StaggerOdd).
	TopologyHexFlatOdd
	// TopologyHexFlatEven is a flat-top hexagonal grid with even columns shifted down (StaggerX, StaggerEven).
	TopologyHexFlatEven
	// TopologyStaggeredYOdd is a staggered isometric grid with odd rows shifted right (StaggerY, StaggerOdd).
	// With AStar.Diagonal false it moves along the 4 diamond edges; with AStar.Diagonal true it also allows the 4 vertex-adjacent diagonals.
	TopologyStaggeredYOdd
	// TopologyStaggeredYEven is a staggered isometric grid with even rows shifted right (StaggerY, StaggerEven).
	TopologyStaggeredYEven
	// TopologyStaggeredXOdd is a staggered isometric grid with odd columns shifted down (StaggerX, StaggerOdd).
	TopologyStaggeredXOdd
	// TopologyStaggeredXEven is a staggered isometric grid with even columns shifted down (StaggerX, StaggerEven).
	TopologyStaggeredXEven
)

// String returns the topology's canonical name.
func (t GridTopology) String() string {
	switch t {
	case TopologySquare:
		return "square"
	case TopologySquare4:
		return "square4"
	case TopologySquare8:
		return "square8"
	case TopologyHexPointyOdd:
		return "hex-pointy-odd"
	case TopologyHexPointyEven:
		return "hex-pointy-even"
	case TopologyHexFlatOdd:
		return "hex-flat-odd"
	case TopologyHexFlatEven:
		return "hex-flat-even"
	case TopologyStaggeredYOdd:
		return "staggered-y-odd"
	case TopologyStaggeredYEven:
		return "staggered-y-even"
	case TopologyStaggeredXOdd:
		return "staggered-x-odd"
	case TopologyStaggeredXEven:
		return "staggered-x-even"
	default:
		return fmt.Sprintf("ai.GridTopology(%d)", uint8(t))
	}
}

// IsHex reports whether t is one of the four hexagonal grid topologies.
func (t GridTopology) IsHex() bool {
	return t >= TopologyHexPointyOdd && t <= TopologyHexFlatEven
}

// IsStaggered reports whether t is one of the four staggered isometric grid topologies.
func (t GridTopology) IsStaggered() bool {
	return t >= TopologyStaggeredYOdd && t <= TopologyStaggeredXEven
}

// TopologyFromTilemap returns the GridTopology matching m's Orientation, StaggerAxis and StaggerIndex.
//
// A nil tilemap, OrientationOrthogonal or OrientationIsometric returns TopologySquare (diamond isometric cells share the same 4/8-way (col, row) adjacency as orthogonal squares).
func TopologyFromTilemap(m *tilemap.Tilemap) GridTopology {
	if m == nil {
		return TopologySquare
	}
	switch m.Orientation {
	case tilemap.OrientationHexagonal:
		if m.StaggerAxis == tilemap.StaggerX {
			if m.StaggerIndex == tilemap.StaggerEven {
				return TopologyHexFlatEven
			}
			return TopologyHexFlatOdd
		}
		if m.StaggerIndex == tilemap.StaggerEven {
			return TopologyHexPointyEven
		}
		return TopologyHexPointyOdd

	case tilemap.OrientationStaggered:
		if m.StaggerAxis == tilemap.StaggerX {
			if m.StaggerIndex == tilemap.StaggerEven {
				return TopologyStaggeredXEven
			}
			return TopologyStaggeredXOdd
		}
		if m.StaggerIndex == tilemap.StaggerEven {
			return TopologyStaggeredYEven
		}
		return TopologyStaggeredYOdd

	default:
		return TopologySquare
	}
}

// EdgeCostGrid is a Grid whose traversal cost depends on both the source and destination cells (for example elevation steps, river crossings or zone-of-control penalties).
//
// EdgeCost returns the step cost multiplier for moving from (fromCol, fromRow) into (toCol, toRow). Negative, NaN or infinite values forbid that transition. During AStar searches, positive costs below 1 are clamped to 1 so the distance heuristic stays admissible; MovementRange accepts any non-negative finite cost.
type EdgeCostGrid interface {
	Grid
	EdgeCost(fromCol, fromRow, toCol, toRow int) float64
}

// ReachableCell is one cell returned by a movement-range query together with its minimum path cost from the start cell.
type ReachableCell struct {
	Cell Cell
	Cost float64
}

type neighborStep struct {
	col, row               int
	dist                   float64
	diag                   bool
	corner1Col, corner1Row int
	corner2Col, corner2Row int
}

var hexAxialDirs = [6][2]int{
	{1, 0},
	{1, -1},
	{0, -1},
	{-1, 0},
	{-1, 1},
	{0, 1},
}

func hexOffsetToAxial(t GridTopology, col, row int) (q, r int) {
	switch t {
	case TopologyHexFlatEven:
		return col, row - (col+(col&1))/2
	case TopologyHexFlatOdd:
		return col, row - (col-(col&1))/2
	case TopologyHexPointyEven:
		return col - (row+(row&1))/2, row
	default: // TopologyHexPointyOdd
		return col - (row-(row&1))/2, row
	}
}

func hexAxialToOffset(t GridTopology, q, r int) (col, row int) {
	switch t {
	case TopologyHexFlatEven:
		return q, r + (q+(q&1))/2
	case TopologyHexFlatOdd:
		return q, r + (q-(q&1))/2
	case TopologyHexPointyEven:
		return q + (r+(r&1))/2, r
	default: // TopologyHexPointyOdd
		return q + (r-(r&1))/2, r
	}
}

// staggeredToIso maps staggered isometric offset coordinates into an orthogonal isometric lattice (u, v) where the 4 diamond edges change u or v by ±1.
func staggeredToIso(t GridTopology, col, row int) (u, v int) {
	switch t {
	case TopologyStaggeredYEven:
		sx := 2*col - (row & 1)
		sy := row
		return (sx + sy) / 2, (sy - sx) / 2
	case TopologyStaggeredXOdd:
		sx := col
		sy := 2*row + (col & 1)
		return (sx + sy) / 2, (sy - sx) / 2
	case TopologyStaggeredXEven:
		sx := col
		sy := 2*row - (col & 1)
		return (sx + sy) / 2, (sy - sx) / 2
	default: // TopologyStaggeredYOdd
		sx := 2*col + (row & 1)
		sy := row
		return (sx + sy) / 2, (sy - sx) / 2
	}
}

func staggeredNeighbors(t GridTopology, c, r int, diagonal bool, out *[8]neighborStep) int {
	var neC, neR, seC, seR, swC, swR, nwC, nwR int
	switch t {
	case TopologyStaggeredYOdd, TopologyStaggeredYEven:
		staggered := (r & 1) != 0
		if t == TopologyStaggeredYEven {
			staggered = !staggered
		}
		if staggered {
			neC, neR = c+1, r-1
			seC, seR = c+1, r+1
			swC, swR = c, r+1
			nwC, nwR = c, r-1
		} else {
			neC, neR = c, r-1
			seC, seR = c, r+1
			swC, swR = c-1, r+1
			nwC, nwR = c-1, r-1
		}
		out[0] = neighborStep{col: neC, row: neR, dist: 1}
		out[1] = neighborStep{col: seC, row: seR, dist: 1}
		out[2] = neighborStep{col: swC, row: swR, dist: 1}
		out[3] = neighborStep{col: nwC, row: nwR, dist: 1}
		if !diagonal {
			return 4
		}
		out[4] = neighborStep{col: c, row: r - 2, dist: math.Sqrt2, diag: true, corner1Col: nwC, corner1Row: nwR, corner2Col: neC, corner2Row: neR}
		out[5] = neighborStep{col: c + 1, row: r, dist: math.Sqrt2, diag: true, corner1Col: neC, corner1Row: neR, corner2Col: seC, corner2Row: seR}
		out[6] = neighborStep{col: c, row: r + 2, dist: math.Sqrt2, diag: true, corner1Col: seC, corner1Row: seR, corner2Col: swC, corner2Row: swR}
		out[7] = neighborStep{col: c - 1, row: r, dist: math.Sqrt2, diag: true, corner1Col: swC, corner1Row: swR, corner2Col: nwC, corner2Row: nwR}
		return 8

	default: // TopologyStaggeredXOdd, TopologyStaggeredXEven
		staggered := (c & 1) != 0
		if t == TopologyStaggeredXEven {
			staggered = !staggered
		}
		if staggered {
			neC, neR = c+1, r
			seC, seR = c+1, r+1
			swC, swR = c-1, r+1
			nwC, nwR = c-1, r
		} else {
			neC, neR = c+1, r-1
			seC, seR = c+1, r
			swC, swR = c-1, r
			nwC, nwR = c-1, r-1
		}
		out[0] = neighborStep{col: neC, row: neR, dist: 1}
		out[1] = neighborStep{col: seC, row: seR, dist: 1}
		out[2] = neighborStep{col: swC, row: swR, dist: 1}
		out[3] = neighborStep{col: nwC, row: nwR, dist: 1}
		if !diagonal {
			return 4
		}
		out[4] = neighborStep{col: c, row: r - 1, dist: math.Sqrt2, diag: true, corner1Col: nwC, corner1Row: nwR, corner2Col: neC, corner2Row: neR}
		out[5] = neighborStep{col: c + 2, row: r, dist: math.Sqrt2, diag: true, corner1Col: neC, corner1Row: neR, corner2Col: seC, corner2Row: seR}
		out[6] = neighborStep{col: c, row: r + 1, dist: math.Sqrt2, diag: true, corner1Col: seC, corner1Row: seR, corner2Col: swC, corner2Row: swR}
		out[7] = neighborStep{col: c - 2, row: r, dist: math.Sqrt2, diag: true, corner1Col: swC, corner1Row: swR, corner2Col: nwC, corner2Row: nwR}
		return 8
	}
}

func (t GridTopology) neighbors(c, r int, diagonal bool, out *[8]neighborStep) int {
	switch t {
	case TopologyHexPointyOdd, TopologyHexPointyEven, TopologyHexFlatOdd, TopologyHexFlatEven:
		q, ra := hexOffsetToAxial(t, c, r)
		for i, dir := range hexAxialDirs {
			nc, nr := hexAxialToOffset(t, q+dir[0], ra+dir[1])
			out[i] = neighborStep{col: nc, row: nr, dist: 1}
		}
		return 6

	case TopologyStaggeredYOdd, TopologyStaggeredYEven, TopologyStaggeredXOdd, TopologyStaggeredXEven:
		return staggeredNeighbors(t, c, r, diagonal, out)

	case TopologySquare4:
		diagonal = false
	case TopologySquare8:
		diagonal = true
	}

	out[0] = neighborStep{col: c, row: r - 1, dist: 1}
	out[1] = neighborStep{col: c + 1, row: r, dist: 1}
	out[2] = neighborStep{col: c, row: r + 1, dist: 1}
	out[3] = neighborStep{col: c - 1, row: r, dist: 1}
	if !diagonal {
		return 4
	}
	out[4] = neighborStep{col: c + 1, row: r - 1, dist: math.Sqrt2, diag: true, corner1Col: c + 1, corner1Row: r, corner2Col: c, corner2Row: r - 1}
	out[5] = neighborStep{col: c + 1, row: r + 1, dist: math.Sqrt2, diag: true, corner1Col: c + 1, corner1Row: r, corner2Col: c, corner2Row: r + 1}
	out[6] = neighborStep{col: c - 1, row: r + 1, dist: math.Sqrt2, diag: true, corner1Col: c - 1, corner1Row: r, corner2Col: c, corner2Row: r + 1}
	out[7] = neighborStep{col: c - 1, row: r - 1, dist: math.Sqrt2, diag: true, corner1Col: c - 1, corner1Row: r, corner2Col: c, corner2Row: r - 1}
	return 8
}

func (t GridTopology) distance(c1, r1, c2, r2 int, diagonal bool) float64 {
	switch t {
	case TopologyHexPointyOdd, TopologyHexPointyEven, TopologyHexFlatOdd, TopologyHexFlatEven:
		q1, ra1 := hexOffsetToAxial(t, c1, r1)
		q2, ra2 := hexOffsetToAxial(t, c2, r2)
		dq := q1 - q2
		dr := ra1 - ra2
		ds := -dq - dr
		if dq < 0 {
			dq = -dq
		}
		if dr < 0 {
			dr = -dr
		}
		if ds < 0 {
			ds = -ds
		}
		return float64((dq + dr + ds) / 2)

	case TopologyStaggeredYOdd, TopologyStaggeredYEven, TopologyStaggeredXOdd, TopologyStaggeredXEven:
		u1, v1 := staggeredToIso(t, c1, r1)
		u2, v2 := staggeredToIso(t, c2, r2)
		du := u1 - u2
		if du < 0 {
			du = -du
		}
		dv := v1 - v2
		if dv < 0 {
			dv = -dv
		}
		if !diagonal {
			return float64(du + dv)
		}
		return float64(max(du, dv)) + (math.Sqrt2-1)*float64(min(du, dv))

	case TopologySquare4:
		diagonal = false
	case TopologySquare8:
		diagonal = true
	}

	dx := c1 - c2
	if dx < 0 {
		dx = -dx
	}
	dy := r1 - r2
	if dy < 0 {
		dy = -dy
	}
	if !diagonal {
		return float64(dx + dy)
	}
	return float64(max(dx, dy)) + (math.Sqrt2-1)*float64(min(dx, dy))
}

// Neighbors returns the adjacent cells of c under t in deterministic order.
//
// For TopologySquare and TopologyStaggered* it returns the 4 edge-adjacent neighbors; use TopologySquare8 for 8-way square neighbors, or NeighborsInto with an explicit diagonal flag via AStar.
func (t GridTopology) Neighbors(c Cell) []Cell {
	return t.NeighborsInto(nil, c)
}

// NeighborsInto appends the adjacent cells of c under t into dst[:0].
func (t GridTopology) NeighborsInto(dst []Cell, c Cell) []Cell {
	var steps [8]neighborStep
	n := t.neighbors(c.Col, c.Row, false, &steps)
	out := dst[:0]
	for i := range n {
		out = append(out, Cell{Col: steps[i].col, Row: steps[i].row})
	}
	return out
}

// Distance returns the exact open-ground step distance (and admissible A* heuristic) between a and b under t.
//
// For TopologySquare and TopologyStaggered* it computes 4-way distance; TopologySquare8 computes octile distance; hexagonal topologies compute exact cube distance matching tilemap.Tilemap.HexDistance.
func (t GridTopology) Distance(a, b Cell) float64 {
	return t.distance(a.Col, a.Row, b.Col, b.Row, false)
}

// MovementRange runs a Dijkstra search from start on g under a.Topology (and a.Diagonal / a.NoCornerCutting) and returns every reachable cell whose minimum traversal cost is at most budget, ordered by ascending cost then row-major cell index.
//
// It reuses a's internal search buffers and allocates only the returned slice.
func (a *AStar) MovementRange(g Grid, start Cell, budget float64) []ReachableCell {
	return a.MovementRangeWithCost(nil, g, start, budget, nil)
}

// MovementRangeInto behaves like MovementRange but writes results into dst[:0], avoiding heap allocations when dst has enough capacity.
func (a *AStar) MovementRangeInto(dst []ReachableCell, g Grid, start Cell, budget float64) []ReachableCell {
	return a.MovementRangeWithCost(dst, g, start, budget, nil)
}

// MovementRangeWithCost behaves like MovementRangeInto, and when edgeCost is non-nil uses edgeCost(from, to) as the step cost multiplier instead of CostGrid or EdgeCostGrid.
//
// Returning a negative, NaN or infinite value from edgeCost forbids moving along that edge. Non-negative finite values (including costs between 0 and 1, such as roads) are multiplied by the step's geometric length (1 for cardinal/hex/staggered edges, sqrt(2) for diagonals).
func (a *AStar) MovementRangeWithCost(dst []ReachableCell, g Grid, start Cell, budget float64, edgeCost func(from, to Cell) float64) []ReachableCell {
	out := dst[:0]
	if g == nil || budget < 0 || math.IsNaN(budget) {
		return out
	}
	cols, rows := g.GridSize()
	if cols <= 0 || rows <= 0 {
		return out
	}
	if !inBounds(start, cols, rows) || !g.Walkable(start.Col, start.Row) {
		return out
	}

	n := cols * rows
	a.prepare(n)

	eg, hasEdgeCost := g.(EdgeCostGrid)
	cg, hasCost := g.(CostGrid)

	startID := start.Row*cols + start.Col
	gen := a.gen

	a.seen[startID] = gen
	a.gScore[startID] = 0
	a.parent[startID] = -1
	a.heapPush(astarNode{id: startID, f: 0, h: 0})

	var steps [8]neighborStep
	prevID := -1
	prevCost := 0.0
	needsSort := false

	for len(a.open) > 0 {
		cur := a.heapPop()
		if cur.f > budget {
			break
		}
		a.closed[cur.id] = gen

		c := cur.id % cols
		r := cur.id / cols
		curG := a.gScore[cur.id]
		if prevID >= 0 && !needsSort && (curG < prevCost || (curG == prevCost && cur.id < prevID)) {
			needsSort = true
		}
		prevID = cur.id
		prevCost = curG
		out = append(out, ReachableCell{Cell: Cell{Col: c, Row: r}, Cost: curG})

		stepCount := a.Topology.neighbors(c, r, a.Diagonal, &steps)
		from := Cell{Col: c, Row: r}

		for i := range stepCount {
			st := steps[i]
			nc, nr := st.col, st.row
			if nc < 0 || nc >= cols || nr < 0 || nr >= rows {
				continue
			}
			nid := nr*cols + nc
			if a.closed[nid] == gen {
				continue
			}
			if !g.Walkable(nc, nr) {
				continue
			}
			if st.diag && a.NoCornerCutting {
				if !g.Walkable(st.corner1Col, st.corner1Row) || !g.Walkable(st.corner2Col, st.corner2Row) {
					continue
				}
			}

			var stepMul float64
			switch {
			case edgeCost != nil:
				stepMul = edgeCost(from, Cell{Col: nc, Row: nr})
				if stepMul < 0 || math.IsInf(stepMul, 0) || math.IsNaN(stepMul) {
					continue
				}
			case hasEdgeCost:
				stepMul = eg.EdgeCost(c, r, nc, nr)
				if stepMul < 0 || math.IsInf(stepMul, 0) || math.IsNaN(stepMul) {
					continue
				}
			case hasCost:
				tc := cg.MoveCost(nc, nr)
				if tc > 0 && !math.IsInf(tc, 0) && !math.IsNaN(tc) {
					stepMul = tc
				} else {
					stepMul = 1
				}
			default:
				stepMul = 1
			}

			ng := curG + st.dist*stepMul
			if ng > budget {
				continue
			}

			if a.seen[nid] != gen {
				a.seen[nid] = gen
				a.gScore[nid] = ng
				a.parent[nid] = cur.id
				a.heapPush(astarNode{id: nid, f: ng, h: 0})
			} else if ng < a.gScore[nid] {
				a.gScore[nid] = ng
				a.parent[nid] = cur.id
				if pos := a.heapPos[nid]; pos >= 0 {
					a.open[pos].f = ng
					a.siftUp(pos)
				} else {
					a.heapPush(astarNode{id: nid, f: ng, h: 0})
				}
			}
		}
	}

	if needsSort {
		slices.SortFunc(out, compareReachableCell)
	}

	return out
}

func compareReachableCell(a, b ReachableCell) int {
	if c := cmp.Compare(a.Cost, b.Cost); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Cell.Row, b.Cell.Row); c != 0 {
		return c
	}
	return cmp.Compare(a.Cell.Col, b.Cell.Col)
}

// ReachableCells returns every cell reachable from start on g within budget under topology, ordered by ascending cost then row-major cell index.
//
// When edgeCost is non-nil it overrides CostGrid and EdgeCostGrid with a custom step cost multiplier. For repeated zero-allocation queries, configure an AStar with Topology and call MovementRangeInto or MovementRangeWithCost.
func ReachableCells(g Grid, topology GridTopology, start Cell, budget float64, edgeCost func(from, to Cell) float64) []ReachableCell {
	a := NewAStar()
	a.Topology = topology
	return a.MovementRangeWithCost(nil, g, start, budget, edgeCost)
}
