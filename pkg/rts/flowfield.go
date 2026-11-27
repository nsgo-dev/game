package rts

import (
	"math"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/math2d"
)

// FlowField holds, for every cell of an ai.Grid, the cost of the cheapest path to the nearest of its goal cells (the integration field) and the direction a unit standing in that cell moves to follow that path (the direction field).
//
// One field serves every unit heading for the same goals: building it costs one Dijkstra search over the grid, and each unit then reads its direction in constant time with Sample. Cells map to world space through Origin and CellSize: cell (col, row) covers [Origin.X+col*CellSize, Origin.X+(col+1)*CellSize) horizontally and the same along Y.
//
// A FlowField is not safe for concurrent use while it is being built.
type FlowField struct {
	// Origin is the world position of the top-left corner of cell (0, 0).
	Origin math2d.Vec2
	// CellSize is the width and height of a cell in world units; 0 or less, or a non-finite value, is treated as 1.
	CellSize float32
	// Diagonal allows 8-way movement, with diagonal steps costing sqrt(2) and never cutting the corner of a blocked cell; false restricts movement to the 4 cardinal directions. NewFlowField enables it.
	Diagonal bool

	cols, rows int
	goals      []ai.Cell
	request    []ai.Cell // the goals the last Build was given, valid or not, so the field can be built again from them
	cost       []float32
	enter      []float32
	vec        []math2d.Vec2
	open       []fieldNode
}

type fieldNode struct {
	id   int32
	cost float32
}

// Unreachable is the integration cost of a cell no goal can be reached from, and of every cell outside the grid.
var Unreachable = float32(math.Inf(1))

// NewFlowField returns an empty field with 8-way movement whose cells are cellSize world units wide, the top-left one starting at origin.
func NewFlowField(origin math2d.Vec2, cellSize float32) *FlowField {
	return &FlowField{Origin: origin, CellSize: cellSize, Diagonal: true}
}

// Build integrates g toward goals, replacing whatever the field held, and reports whether at least one goal is an in-bounds walkable cell. Goals that are out of bounds or blocked are ignored.
//
// Stepping into a cell costs the step's length (1, or sqrt(2) diagonally) times the cell's cost, which comes from g's MoveCost when g is an ai.CostGrid (values of 0 or less, and non-finite ones, count as 1, as ai.AStar treats them) and is 1 otherwise, so a cell's integration cost equals the cost ai.AStar finds from it to the nearest goal. Build reuses the field's buffers, so rebuilding a field of the same size allocates nothing.
func (f *FlowField) Build(g ai.Grid, goals ...ai.Cell) bool {
	f.goals = f.goals[:0]
	f.request = append(f.request[:0], goals...)
	if g == nil {
		f.resize(0, 0)
		return false
	}
	cols, rows := g.GridSize()
	if cols <= 0 || rows <= 0 {
		f.resize(0, 0)
		return false
	}
	f.resize(cols, rows)
	// Read the grid once: enter holds each cell's step cost, or Unreachable when blocked, so the search below makes no interface calls.
	cg, hasCost := g.(ai.CostGrid)
	for r := range rows {
		for c := range cols {
			id := r*cols + c
			f.cost[id] = Unreachable
			f.vec[id] = math2d.Vec2{}
			switch {
			case !g.Walkable(c, r):
				f.enter[id] = Unreachable
			case hasCost:
				f.enter[id] = moveCost(cg, c, r)
			default:
				f.enter[id] = 1
			}
		}
	}
	f.open = f.open[:0]
	for _, c := range goals {
		if !f.inBounds(c.Col, c.Row) {
			continue
		}
		id := int32(c.Row*cols + c.Col)
		if f.enter[id] == Unreachable || f.cost[id] == 0 {
			continue
		}
		f.cost[id] = 0
		f.goals = append(f.goals, c)
		f.push(fieldNode{id: id})
	}
	if len(f.goals) == 0 {
		return false
	}

	n := 4
	if f.Diagonal {
		n = 8
	}
	for len(f.open) > 0 {
		cur := f.pop()
		if cur.cost > f.cost[cur.id] {
			continue // stale heap entry
		}
		c, r := int(cur.id)%cols, int(cur.id)/cols
		// A unit steps from the neighbor into this cell, so this cell's cost prices the step.
		step := f.enter[cur.id]
		for i := range n {
			st := fieldSteps[i]
			nc, nr := c+st.dc, r+st.dr
			if !f.canStep(c, r, st) {
				continue
			}
			nid := int32(nr*cols + nc)
			if next := cur.cost + st.dist*step; next < f.cost[nid] {
				f.cost[nid] = next
				f.push(fieldNode{id: nid, cost: next})
			}
		}
	}
	f.buildDirections(n)
	return true
}

// canStep reports whether a unit can move between (c, r) and its neighbor in direction st: the neighbor is in the grid and walkable, and a diagonal move does not cut the corner of a blocked cell.
func (f *FlowField) canStep(c, r int, st fieldStep) bool {
	nc, nr := c+st.dc, r+st.dr
	if !f.inBounds(nc, nr) || f.enter[nr*f.cols+nc] == Unreachable {
		return false
	}
	return !st.diag || f.enter[r*f.cols+nc] != Unreachable && f.enter[nr*f.cols+c] != Unreachable
}

// buildDirections points every reachable non-goal cell at the neighbor its cheapest path steps into, as a unit vector in world space.
func (f *FlowField) buildDirections(n int) {
	cols := f.cols
	for id, c := range f.cost {
		if c == 0 || c == Unreachable {
			continue
		}
		col, row := id%cols, id/cols
		best := float32(math.Inf(1))
		bestStep := -1
		for i := range n {
			st := fieldSteps[i]
			if !f.canStep(col, row, st) {
				continue
			}
			nid := (row+st.dr)*cols + col + st.dc
			v := f.cost[nid]
			if v == Unreachable || v >= c {
				continue
			}
			if v += st.dist * f.enter[nid]; v < best {
				best = v
				bestStep = i
			}
		}
		if bestStep >= 0 {
			st := fieldSteps[bestStep]
			f.vec[id] = math2d.Vec2{X: float32(st.dc), Y: float32(st.dr)}.Normalize()
		}
	}
}

type fieldStep struct {
	dc, dr int
	dist   float32
	diag   bool
}

// fieldSteps is the fixed neighbor order that makes integration and tie-breaking deterministic: the cardinal directions first, then the diagonals.
var fieldSteps = [8]fieldStep{
	{0, -1, 1, false},
	{1, 0, 1, false},
	{0, 1, 1, false},
	{-1, 0, 1, false},
	{1, -1, math.Sqrt2, true},
	{1, 1, math.Sqrt2, true},
	{-1, 1, math.Sqrt2, true},
	{-1, -1, math.Sqrt2, true},
}

func moveCost(g ai.CostGrid, col, row int) float32 {
	c := g.MoveCost(col, row)
	if !(c > 0) || math.IsInf(c, 0) {
		return 1
	}
	return float32(c)
}

func (f *FlowField) resize(cols, rows int) {
	f.cols, f.rows = cols, rows
	n := cols * rows
	if cap(f.cost) < n {
		f.cost = make([]float32, n)
		f.enter = make([]float32, n)
		f.vec = make([]math2d.Vec2, n)
	}
	f.cost = f.cost[:n]
	f.enter = f.enter[:n]
	f.vec = f.vec[:n]
}

func (f *FlowField) inBounds(col, row int) bool {
	return col >= 0 && col < f.cols && row >= 0 && row < f.rows
}

// Size returns the field's width in columns and height in rows, those of the grid it was last built on.
func (f *FlowField) Size() (cols, rows int) { return f.cols, f.rows }

// Goals returns the in-bounds walkable goal cells of the last Build, in the order given, without duplicates. The slice is the field's own and changes on the next Build.
func (f *FlowField) Goals() []ai.Cell { return f.goals }

// Cost returns the integration cost from (col, row) to the nearest goal, 0 on a goal and Unreachable for a cell outside the grid or with no path.
func (f *FlowField) Cost(col, row int) float32 {
	if !f.inBounds(col, row) {
		return Unreachable
	}
	return f.cost[row*f.cols+col]
}

// Reachable reports whether a goal can be reached from (col, row).
func (f *FlowField) Reachable(col, row int) bool { return f.Cost(col, row) != Unreachable }

// Direction returns the unit vector a unit in (col, row) moves along, or the zero vector on a goal, on an unreachable cell and outside the grid.
func (f *FlowField) Direction(col, row int) math2d.Vec2 {
	if !f.inBounds(col, row) {
		return math2d.Vec2{}
	}
	return f.vec[row*f.cols+col]
}

func (f *FlowField) cellSize() float32 {
	return validCellSize(f.CellSize)
}

func validCellSize(s float32) float32 {
	if !(s > 0) || math.IsInf(float64(s), 0) {
		return 1
	}
	return s
}

// CellAt returns the cell holding world position p and whether it is inside the grid.
func (f *FlowField) CellAt(p math2d.Vec2) (ai.Cell, bool) {
	return cellAt(p, f.Origin, f.cellSize(), f.cols, f.rows)
}

// CellCenter returns the world position of the center of cell c.
func (f *FlowField) CellCenter(c ai.Cell) math2d.Vec2 {
	return cellCenter(c, f.Origin, f.cellSize())
}

func cellAt(p, origin math2d.Vec2, size float32, cols, rows int) (ai.Cell, bool) {
	x := float64((p.X - origin.X) / size)
	y := float64((p.Y - origin.Y) / size)
	if math.IsNaN(x) || math.IsNaN(y) {
		return ai.Cell{}, false
	}
	fx, fy := math.Floor(x), math.Floor(y)
	if fx < 0 || fy < 0 || fx >= float64(cols) || fy >= float64(rows) {
		return ai.Cell{Col: int(max(min(fx, float64(cols)), -1)), Row: int(max(min(fy, float64(rows)), -1))}, false
	}
	return ai.Cell{Col: int(fx), Row: int(fy)}, true
}

func cellCenter(c ai.Cell, origin math2d.Vec2, size float32) math2d.Vec2 {
	return math2d.Vec2{X: origin.X + (float32(c.Col)+0.5)*size, Y: origin.Y + (float32(c.Row)+0.5)*size}
}

// Sample returns the direction a unit at world position p moves along, blended bilinearly from the four cells around p so that units turn smoothly instead of in 45-degree steps. Only reachable cells take part in the blend, so a unit beside a wall is not pulled into it. ok is false when p is outside the grid or no reachable cell is near it; on a goal cell the direction is the zero vector and ok is true.
func (f *FlowField) Sample(p math2d.Vec2) (dir math2d.Vec2, ok bool) {
	c, in := f.CellAt(p)
	if !in || f.cost[c.Row*f.cols+c.Col] == Unreachable {
		return math2d.Vec2{}, false
	}
	if f.cost[c.Row*f.cols+c.Col] == 0 {
		return math2d.Vec2{}, true
	}
	size := f.cellSize()
	gx := (p.X-f.Origin.X)/size - 0.5
	gy := (p.Y-f.Origin.Y)/size - 0.5
	x0 := int(math.Floor(float64(gx)))
	y0 := int(math.Floor(float64(gy)))
	tx := gx - float32(x0)
	ty := gy - float32(y0)
	var sum math2d.Vec2
	var weight float32
	for _, s := range [4]struct {
		dc, dr int
		w      float32
	}{
		{0, 0, (1 - tx) * (1 - ty)},
		{1, 0, tx * (1 - ty)},
		{0, 1, (1 - tx) * ty},
		{1, 1, tx * ty},
	} {
		col, row := x0+s.dc, y0+s.dr
		if s.w <= 0 || !f.inBounds(col, row) {
			continue
		}
		id := row*f.cols + col
		if f.cost[id] == Unreachable || f.cost[id] == 0 {
			continue
		}
		sum = sum.Add(f.vec[id].Scale(s.w))
		weight += s.w
	}
	if weight <= 0 || sum.LenSq() < 1e-12 {
		return f.vec[c.Row*f.cols+c.Col], true
	}
	return sum.Normalize(), true
}

func (f *FlowField) push(n fieldNode) {
	f.open = append(f.open, n)
	i := len(f.open) - 1
	for i > 0 {
		p := (i - 1) / 2
		if !fieldLess(f.open[i], f.open[p]) {
			break
		}
		f.open[i], f.open[p] = f.open[p], f.open[i]
		i = p
	}
}

func (f *FlowField) pop() fieldNode {
	top := f.open[0]
	last := len(f.open) - 1
	f.open[0] = f.open[last]
	f.open = f.open[:last]
	i := 0
	for {
		l := 2*i + 1
		if l >= last {
			break
		}
		best := l
		if r := l + 1; r < last && fieldLess(f.open[r], f.open[l]) {
			best = r
		}
		if !fieldLess(f.open[best], f.open[i]) {
			break
		}
		f.open[i], f.open[best] = f.open[best], f.open[i]
		i = best
	}
	return top
}

// fieldLess orders the open set by cost, then by cell index, so equal-cost cells always settle in the same order.
func fieldLess(a, b fieldNode) bool {
	if a.cost != b.cost {
		return a.cost < b.cost
	}
	return a.id < b.id
}
