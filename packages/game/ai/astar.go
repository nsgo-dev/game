package ai

import (
	"math"
	"slices"
)

// Cell is a column and row coordinate on a grid.
type Cell struct {
	Col, Row int
}

// Grid is a two-dimensional tile grid that AStar searches.
type Grid interface {
	// GridSize returns the grid's width in columns and height in rows.
	GridSize() (cols, rows int)
	// Walkable reports whether the cell at (col, row) can be entered.
	Walkable(col, row int) bool
}

// CostGrid is a Grid whose walkable cells carry individual traversal costs.
//
// MoveCost returns the cost multiplier for entering (col, row), at least 1 so the distance heuristic remains admissible; values less than or equal to 0 are treated as 1.
type CostGrid interface {
	Grid
	MoveCost(col, row int) float64
}

// BoolGrid is a Grid backed by a flat boolean slice. All cells start walkable.
type BoolGrid struct {
	cols, rows int
	blocked    []bool
}

// NewBoolGrid returns a cols-by-rows grid with every cell walkable.
func NewBoolGrid(cols, rows int) *BoolGrid {
	if cols < 0 {
		cols = 0
	}
	if rows < 0 {
		rows = 0
	}
	return &BoolGrid{
		cols:    cols,
		rows:    rows,
		blocked: make([]bool, cols*rows),
	}
}

// GridSize returns the grid's columns and rows.
func (g *BoolGrid) GridSize() (cols, rows int) { return g.cols, g.rows }

// Walkable reports whether (col, row) is in bounds and unblocked.
func (g *BoolGrid) Walkable(col, row int) bool {
	if col < 0 || col >= g.cols || row < 0 || row >= g.rows {
		return false
	}
	return !g.blocked[row*g.cols+col]
}

// SetWalkable marks (col, row) as walkable or blocked. Out-of-bounds coordinates are ignored.
func (g *BoolGrid) SetWalkable(col, row int, walkable bool) {
	if col < 0 || col >= g.cols || row < 0 || row >= g.rows {
		return
	}
	g.blocked[row*g.cols+col] = !walkable
}

// CostMap is a CostGrid backed by a flat cost slice. All cells start walkable with cost 1.
type CostMap struct {
	cols, rows int
	// costs holds 0 for default cost 1, positive finite values for custom costs, and negative values for blocked cells.
	costs []float64
}

// NewCostMap returns a cols-by-rows CostMap with every cell walkable at cost 1.
func NewCostMap(cols, rows int) *CostMap {
	if cols < 0 {
		cols = 0
	}
	if rows < 0 {
		rows = 0
	}
	return &CostMap{
		cols:  cols,
		rows:  rows,
		costs: make([]float64, cols*rows),
	}
}

// GridSize returns the map's columns and rows.
func (m *CostMap) GridSize() (cols, rows int) { return m.cols, m.rows }

// Walkable reports whether (col, row) is in bounds and not blocked.
func (m *CostMap) Walkable(col, row int) bool {
	if col < 0 || col >= m.cols || row < 0 || row >= m.rows {
		return false
	}
	c := m.costs[row*m.cols+col]
	return c >= 0 && !math.IsInf(c, 0) && !math.IsNaN(c)
}

// MoveCost returns the traversal cost of entering (col, row), or 1 if unset.
func (m *CostMap) MoveCost(col, row int) float64 {
	if col < 0 || col >= m.cols || row < 0 || row >= m.rows {
		return 1
	}
	c := m.costs[row*m.cols+col]
	if c <= 0 || math.IsInf(c, 0) || math.IsNaN(c) {
		return 1
	}
	return c
}

// SetWalkable marks (col, row) as walkable or blocked. Unblocking a cell restores its cost to 1.
func (m *CostMap) SetWalkable(col, row int, walkable bool) {
	if col < 0 || col >= m.cols || row < 0 || row >= m.rows {
		return
	}
	idx := row*m.cols + col
	if !walkable {
		m.costs[idx] = -1
	} else if m.costs[idx] < 0 {
		m.costs[idx] = 0
	}
}

// SetCost sets the traversal cost for (col, row). Negative, infinite or NaN costs mark the cell unwalkable; 0 restores the default cost of 1.
func (m *CostMap) SetCost(col, row int, cost float64) {
	if col < 0 || col >= m.cols || row < 0 || row >= m.rows {
		return
	}
	if cost < 0 || math.IsInf(cost, 0) || math.IsNaN(cost) {
		m.costs[row*m.cols+col] = -1
		return
	}
	m.costs[row*m.cols+col] = cost
}

// GridFunc adapts callback functions to CostGrid, letting any tilemap or custom map plug into AStar in a few lines.
type GridFunc struct {
	Cols, Rows int
	// Walk reports whether (col, row) is walkable; a nil Walk treats all in-bounds cells as walkable.
	Walk func(col, row int) bool
	// Cost returns the traversal cost of entering (col, row); a nil Cost uses 1 everywhere.
	Cost func(col, row int) float64
}

// GridSize returns Cols and Rows.
func (f GridFunc) GridSize() (cols, rows int) { return f.Cols, f.Rows }

// Walkable reports whether (col, row) is in bounds and Walk allows it.
func (f GridFunc) Walkable(col, row int) bool {
	if col < 0 || col >= f.Cols || row < 0 || row >= f.Rows {
		return false
	}
	if f.Walk == nil {
		return true
	}
	return f.Walk(col, row)
}

// MoveCost returns Cost(col, row) or 1 when Cost is nil or returns a non-positive value.
func (f GridFunc) MoveCost(col, row int) float64 {
	if f.Cost == nil {
		return 1
	}
	c := f.Cost(col, row)
	if c <= 0 || math.IsInf(c, 0) || math.IsNaN(c) {
		return 1
	}
	return c
}

// AStar finds lowest-cost paths on a Grid, reusing its internal search buffers across calls.
//
// NewAStar returns a searcher with NoCornerCutting enabled by default. An AStar is not safe for concurrent use by multiple goroutines.
type AStar struct {
	// Diagonal allows 8-way movement with step distance sqrt(2) on diagonals; false restricts movement to 4 cardinal directions.
	Diagonal bool
	// NoCornerCutting prevents diagonal moves when either adjacent cardinal cell is blocked. It only takes effect when Diagonal is true.
	NoCornerCutting bool

	gen     uint32
	seen    []uint32
	closed  []uint32
	gScore  []float64
	parent  []int
	heapPos []int
	open    []astarNode
}

type astarNode struct {
	id int
	f  float64
	h  float64
}

// NewAStar returns an AStar searcher with 4-way movement and NoCornerCutting enabled by default.
func NewAStar() *AStar {
	return &AStar{NoCornerCutting: true}
}

// Find searches g for a minimum-cost path from start to goal, returning the cells from start to goal inclusive and the total path cost.
//
// It reuses all internal search buffers and allocates only the returned path slice.
func (a *AStar) Find(g Grid, start, goal Cell) (path []Cell, cost float64, ok bool) {
	return a.FindInto(nil, g, start, goal)
}

// FindInto behaves like Find but writes the resulting path into dst[:0], avoiding allocations when dst has enough capacity.
func (a *AStar) FindInto(dst []Cell, g Grid, start, goal Cell) (path []Cell, cost float64, ok bool) {
	if g == nil {
		return nil, 0, false
	}
	cols, rows := g.GridSize()
	if cols <= 0 || rows <= 0 {
		return nil, 0, false
	}
	if !inBounds(start, cols, rows) || !inBounds(goal, cols, rows) {
		return nil, 0, false
	}
	if !g.Walkable(start.Col, start.Row) || !g.Walkable(goal.Col, goal.Row) {
		return nil, 0, false
	}
	if start == goal {
		return append(dst[:0], start), 0, true
	}

	n := cols * rows
	a.prepare(n)

	cg, hasCost := g.(CostGrid)

	startID := start.Row*cols + start.Col
	goalID := goal.Row*cols + goal.Col
	gen := a.gen

	a.seen[startID] = gen
	a.gScore[startID] = 0
	a.parent[startID] = -1
	h0 := a.heuristic(start.Col, start.Row, goal.Col, goal.Row)
	a.heapPush(astarNode{id: startID, f: h0, h: h0})

	// Fixed neighbor order guarantees deterministic exploration and tie-breaking.
	type step struct {
		dc, dr int
		dist   float64
		diag   bool
	}
	steps := [...]step{
		{0, -1, 1, false},
		{1, 0, 1, false},
		{0, 1, 1, false},
		{-1, 0, 1, false},
		{1, -1, math.Sqrt2, true},
		{1, 1, math.Sqrt2, true},
		{-1, 1, math.Sqrt2, true},
		{-1, -1, math.Sqrt2, true},
	}
	stepCount := 4
	if a.Diagonal {
		stepCount = 8
	}

	for len(a.open) > 0 {
		cur := a.heapPop()
		if cur.id == goalID {
			out := dst[:0]
			for id := goalID; id != -1; id = a.parent[id] {
				out = append(out, Cell{Col: id % cols, Row: id / cols})
			}
			slices.Reverse(out)
			return out, a.gScore[goalID], true
		}
		a.closed[cur.id] = gen

		c := cur.id % cols
		r := cur.id / cols
		curG := a.gScore[cur.id]

		for i := range stepCount {
			st := steps[i]
			nc, nr := c+st.dc, r+st.dr
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
				if !g.Walkable(c+st.dc, r) || !g.Walkable(c, r+st.dr) {
					continue
				}
			}

			tileCost := 1.0
			if hasCost {
				tc := cg.MoveCost(nc, nr)
				if tc > 0 && !math.IsInf(tc, 0) && !math.IsNaN(tc) {
					tileCost = tc
				}
			}
			ng := curG + st.dist*tileCost

			if a.seen[nid] != gen {
				a.seen[nid] = gen
				a.gScore[nid] = ng
				a.parent[nid] = cur.id
				h := a.heuristic(nc, nr, goal.Col, goal.Row)
				a.heapPush(astarNode{id: nid, f: ng + h, h: h})
			} else if ng < a.gScore[nid] {
				a.gScore[nid] = ng
				a.parent[nid] = cur.id
				h := a.heuristic(nc, nr, goal.Col, goal.Row)
				if pos := a.heapPos[nid]; pos >= 0 {
					a.open[pos].f = ng + h
					a.open[pos].h = h
					a.siftUp(pos)
				} else {
					a.heapPush(astarNode{id: nid, f: ng + h, h: h})
				}
			}
		}
	}

	return nil, 0, false
}

func inBounds(c Cell, cols, rows int) bool {
	return c.Col >= 0 && c.Col < cols && c.Row >= 0 && c.Row < rows
}

func (a *AStar) heuristic(c1, r1, c2, r2 int) float64 {
	dx := c1 - c2
	if dx < 0 {
		dx = -dx
	}
	dy := r1 - r2
	if dy < 0 {
		dy = -dy
	}
	if !a.Diagonal {
		return float64(dx + dy)
	}
	return float64(max(dx, dy)) + (math.Sqrt2-1)*float64(min(dx, dy))
}

func (a *AStar) prepare(n int) {
	if cap(a.seen) < n {
		a.seen = make([]uint32, n)
		a.closed = make([]uint32, n)
		a.gScore = make([]float64, n)
		a.parent = make([]int, n)
		a.heapPos = make([]int, n)
		a.gen = 0
	} else {
		a.seen = a.seen[:n]
		a.closed = a.closed[:n]
		a.gScore = a.gScore[:n]
		a.parent = a.parent[:n]
		a.heapPos = a.heapPos[:n]
	}
	a.gen++
	if a.gen == 0 {
		clear(a.seen)
		clear(a.closed)
		a.gen = 1
	}
	a.open = a.open[:0]
}

// astarLess defines a strict total order for deterministic tie-breaking: lowest f, then lowest h, then lowest cell id.
func astarLess(x, y astarNode) bool {
	if x.f != y.f {
		return x.f < y.f
	}
	if x.h != y.h {
		return x.h < y.h
	}
	return x.id < y.id
}

func (a *AStar) heapPush(node astarNode) {
	idx := len(a.open)
	a.open = append(a.open, node)
	a.heapPos[node.id] = idx
	a.siftUp(idx)
}

func (a *AStar) heapPop() astarNode {
	top := a.open[0]
	a.heapPos[top.id] = -1
	last := len(a.open) - 1
	if last == 0 {
		a.open = a.open[:0]
		return top
	}
	a.open[0] = a.open[last]
	a.heapPos[a.open[0].id] = 0
	a.open = a.open[:last]
	a.siftDown(0)
	return top
}

func (a *AStar) siftUp(i int) {
	for i > 0 {
		p := (i - 1) / 2
		if !astarLess(a.open[i], a.open[p]) {
			break
		}
		a.open[i], a.open[p] = a.open[p], a.open[i]
		a.heapPos[a.open[i].id] = i
		a.heapPos[a.open[p].id] = p
		i = p
	}
}

func (a *AStar) siftDown(i int) {
	n := len(a.open)
	for {
		l := 2*i + 1
		if l >= n {
			break
		}
		best := l
		if r := l + 1; r < n && astarLess(a.open[r], a.open[l]) {
			best = r
		}
		if !astarLess(a.open[best], a.open[i]) {
			break
		}
		a.open[i], a.open[best] = a.open[best], a.open[i]
		a.heapPos[a.open[i].id] = i
		a.heapPos[a.open[best].id] = best
		i = best
	}
}
