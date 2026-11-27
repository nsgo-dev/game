package rts

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/math2d"
)

// SectorFlowField is a flow field integrated only across an active subset of square sectors (clusters),
// such as the cluster corridor produced by ai.HPA.FindClusters.
//
// On large maps, integrating only the corridor of traversed clusters avoids scanning the entire grid
// while providing the same constant-time Cost, Reachable, Direction, and smooth bilinear Sample queries
// as FlowField. Cells outside active sectors report Unreachable.
//
// A SectorFlowField reuses its internal buffers across Build calls and is not safe for concurrent use while building.
type SectorFlowField struct {
	// Origin is the world position of the top-left corner of cell (0, 0).
	Origin math2d.Vec2
	// CellSize is the width and height of a cell in world units; 0 or less, or a non-finite value, is treated as 1.
	CellSize float32
	// Diagonal allows 8-way movement, with diagonal steps costing sqrt(2) and never cutting the corner of a blocked cell;
	// false restricts movement to the 4 cardinal directions. NewSectorFlowField enables it.
	Diagonal bool

	cols, rows      int
	sectorSize      int
	sectorCols      int
	sectorRows      int
	sectorIndex     []int32 // sectorCols*sectorRows -> index in activeSectors, or -1 if inactive
	activeSectors   []ai.ClusterID
	goals           []ai.Cell
	cost            []float32
	enter           []float32
	vec             []math2d.Vec2
	open            []fieldNode
	clusterScratch  []ai.ClusterID
	expandedScratch []ai.ClusterID
}

// NewSectorFlowField returns an empty 8-way sector-limited flow field whose cells are cellSize world units wide, starting at origin.
func NewSectorFlowField(origin math2d.Vec2, cellSize float32) *SectorFlowField {
	return &SectorFlowField{Origin: origin, CellSize: cellSize, Diagonal: true}
}

// BuildAlongHPA queries hpa for the cluster corridor from start to goal, optionally expands it by padding rings of neighboring clusters
// (0 keeps the exact HPA* corridor), and integrates the flow field toward goal over those active sectors only.
//
// Because every step along the HPA* path stays inside the returned cluster corridor, start is guaranteed to be reachable from goal
// whenever hpa finds a path (provided f.Diagonal is compatible with hpa's movement rules).
func (f *SectorFlowField) BuildAlongHPA(hpa *ai.HPA, start, goal ai.Cell, padding int) bool {
	if hpa == nil || hpa.Grid() == nil {
		f.clearActive()
		return false
	}
	corridor, _, ok := hpa.FindClustersInto(f.clusterScratch[:0], start, goal)
	f.clusterScratch = corridor
	if !ok || len(corridor) == 0 {
		f.clearActive()
		return false
	}
	if padding <= 0 {
		return f.BuildSectors(hpa.Grid(), hpa.ClusterSize(), corridor, goal)
	}
	cc, cr := hpa.ClusterGridSize()
	exp := f.expandedScratch[:0]
	for _, cl := range corridor {
		minC := max(cl.Col-padding, 0)
		maxC := min(cl.Col+padding, cc-1)
		minR := max(cl.Row-padding, 0)
		maxR := min(cl.Row+padding, cr-1)
		for r := minR; r <= maxR; r++ {
			for c := minC; c <= maxC; c++ {
				exp = append(exp, ai.ClusterID{Col: c, Row: r})
			}
		}
	}
	f.expandedScratch = exp
	return f.BuildSectors(hpa.Grid(), hpa.ClusterSize(), exp, goal)
}

// BuildSectors integrates g toward goals restricting the search to cells inside the given sectorSize-by-sectorSize sectors.
//
// Duplicate and out-of-bounds sectors are ignored. Goals that are out of bounds, blocked, or outside active sectors are ignored.
// It reports whether at least one valid goal was seeded inside an active sector.
func (f *SectorFlowField) BuildSectors(g ai.Grid, sectorSize int, sectors []ai.ClusterID, goals ...ai.Cell) bool {
	f.clearActive()
	f.goals = f.goals[:0]
	if g == nil || sectorSize <= 0 {
		f.cols, f.rows = 0, 0
		return false
	}
	cols, rows := g.GridSize()
	if cols <= 0 || rows <= 0 {
		f.cols, f.rows = 0, 0
		return false
	}

	sCols := (cols + sectorSize - 1) / sectorSize
	sRows := (rows + sectorSize - 1) / sectorSize
	f.prepareGrid(cols, rows, sectorSize, sCols, sRows)

	// Deduplicate and sort active sectors in row-major order for deterministic layout.
	active := f.activeSectors[:0]
	for _, s := range sectors {
		if s.Col < 0 || s.Col >= sCols || s.Row < 0 || s.Row >= sRows {
			continue
		}
		idx := s.Row*sCols + s.Col
		if f.sectorIndex[idx] >= 0 {
			continue
		}
		f.sectorIndex[idx] = 0 // temporary visited mark
		active = append(active, s)
	}
	if len(active) == 0 {
		f.activeSectors = active
		return false
	}
	slices.SortFunc(active, func(a, b ai.ClusterID) int {
		if a.Row != b.Row {
			return a.Row - b.Row
		}
		return a.Col - b.Col
	})
	for i, s := range active {
		f.sectorIndex[s.Row*sCols+s.Col] = int32(i)
	}
	f.activeSectors = active

	area := sectorSize * sectorSize
	totalSlots := len(active) * area
	if cap(f.cost) < totalSlots {
		f.cost = make([]float32, totalSlots)
		f.enter = make([]float32, totalSlots)
		f.vec = make([]math2d.Vec2, totalSlots)
	}
	f.cost = f.cost[:totalSlots]
	f.enter = f.enter[:totalSlots]
	f.vec = f.vec[:totalSlots]

	// Read grid walkability and costs ONLY for cells inside active sectors.
	cg, hasCost := g.(ai.CostGrid)
	for si, s := range active {
		base := si * area
		minC := s.Col * sectorSize
		maxC := min(minC+sectorSize, cols)
		minR := s.Row * sectorSize
		maxR := min(minR+sectorSize, rows)
		for r := minR; r < maxR; r++ {
			lr := r - minR
			rowOff := base + lr*sectorSize
			for c := minC; c < maxC; c++ {
				slot := rowOff + (c - minC)
				f.cost[slot] = Unreachable
				f.vec[slot] = math2d.Vec2{}
				switch {
				case !g.Walkable(c, r):
					f.enter[slot] = Unreachable
				case hasCost:
					f.enter[slot] = moveCost(cg, c, r)
				default:
					f.enter[slot] = 1
				}
			}
		}
	}

	f.open = f.open[:0]
	for _, c := range goals {
		slot, ok := f.cellSlot(c.Col, c.Row)
		if !ok || f.enter[slot] == Unreachable || f.cost[slot] == 0 {
			continue
		}
		f.cost[slot] = 0
		f.goals = append(f.goals, c)
		f.push(fieldNode{id: int32(c.Row*cols + c.Col)})
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
		c, r := int(cur.id)%cols, int(cur.id)/cols
		curSlot, _ := f.cellSlot(c, r)
		if cur.cost > f.cost[curSlot] {
			continue
		}
		step := f.enter[curSlot]
		for i := range n {
			st := fieldSteps[i]
			nslot, ok := f.canStepSlot(c, r, st)
			if !ok {
				continue
			}
			if next := cur.cost + st.dist*step; next < f.cost[nslot] {
				f.cost[nslot] = next
				f.push(fieldNode{id: int32((r+st.dr)*cols + (c + st.dc)), cost: next})
			}
		}
	}

	f.buildDirections(n)
	return true
}

func (f *SectorFlowField) clearActive() {
	for _, s := range f.activeSectors {
		idx := s.Row*f.sectorCols + s.Col
		if idx >= 0 && idx < len(f.sectorIndex) {
			f.sectorIndex[idx] = -1
		}
	}
	f.activeSectors = f.activeSectors[:0]
}

func (f *SectorFlowField) prepareGrid(cols, rows, sectorSize, sCols, sRows int) {
	need := sCols * sRows
	resized := f.sectorCols != sCols || f.sectorRows != sRows || len(f.sectorIndex) < need
	f.cols, f.rows = cols, rows
	f.sectorSize = sectorSize
	f.sectorCols, f.sectorRows = sCols, sRows
	if resized {
		if cap(f.sectorIndex) < need {
			f.sectorIndex = make([]int32, need)
		} else {
			f.sectorIndex = f.sectorIndex[:need]
		}
		for i := range f.sectorIndex {
			f.sectorIndex[i] = -1
		}
	}
}

func (f *SectorFlowField) cellSlot(col, row int) (int, bool) {
	if col < 0 || col >= f.cols || row < 0 || row >= f.rows || f.sectorSize <= 0 {
		return -1, false
	}
	sc, sr := col/f.sectorSize, row/f.sectorSize
	si := f.sectorIndex[sr*f.sectorCols+sc]
	if si < 0 {
		return -1, false
	}
	lc, lr := col-sc*f.sectorSize, row-sr*f.sectorSize
	return int(si)*f.sectorSize*f.sectorSize + lr*f.sectorSize + lc, true
}

func (f *SectorFlowField) canStepSlot(c, r int, st fieldStep) (int, bool) {
	nc, nr := c+st.dc, r+st.dr
	nslot, ok := f.cellSlot(nc, nr)
	if !ok || f.enter[nslot] == Unreachable {
		return -1, false
	}
	if st.diag {
		s1, ok1 := f.cellSlot(nc, r)
		s2, ok2 := f.cellSlot(c, nr)
		if !ok1 || !ok2 || f.enter[s1] == Unreachable || f.enter[s2] == Unreachable {
			return -1, false
		}
	}
	return nslot, true
}

func (f *SectorFlowField) buildDirections(n int) {
	sSize := f.sectorSize
	area := sSize * sSize
	for si, s := range f.activeSectors {
		base := si * area
		minC := s.Col * sSize
		maxC := min(minC+sSize, f.cols)
		minR := s.Row * sSize
		maxR := min(minR+sSize, f.rows)
		for r := minR; r < maxR; r++ {
			rowOff := base + (r-minR)*sSize
			for c := minC; c < maxC; c++ {
				slot := rowOff + (c - minC)
				curCost := f.cost[slot]
				if curCost == 0 || curCost == Unreachable {
					continue
				}
				best := float32(math.Inf(1))
				bestStep := -1
				for i := range n {
					st := fieldSteps[i]
					nslot, ok := f.canStepSlot(c, r, st)
					if !ok {
						continue
					}
					v := f.cost[nslot]
					if v == Unreachable || v >= curCost {
						continue
					}
					if v += st.dist * f.enter[nslot]; v < best {
						best = v
						bestStep = i
					}
				}
				if bestStep >= 0 {
					st := fieldSteps[bestStep]
					f.vec[slot] = math2d.Vec2{X: float32(st.dc), Y: float32(st.dr)}.Normalize()
				}
			}
		}
	}
}

// Size returns the full grid width and height in cells that the field was built over.
func (f *SectorFlowField) Size() (cols, rows int) { return f.cols, f.rows }

// ActiveSectors returns the number of sectors currently integrated in the field.
func (f *SectorFlowField) ActiveSectors() int { return len(f.activeSectors) }

// SectorActive reports whether cell (col, row) belongs to an active sector.
func (f *SectorFlowField) SectorActive(col, row int) bool {
	_, ok := f.cellSlot(col, row)
	return ok
}

// Goals returns the in-bounds walkable goal cells inside active sectors from the last build.
func (f *SectorFlowField) Goals() []ai.Cell { return f.goals }

// Cost returns the integration cost from (col, row) to the nearest goal, 0 on a goal,
// and Unreachable for blocked cells, cells outside active sectors, or cells outside the grid.
func (f *SectorFlowField) Cost(col, row int) float32 {
	slot, ok := f.cellSlot(col, row)
	if !ok {
		return Unreachable
	}
	return f.cost[slot]
}

// Reachable reports whether a goal can be reached from (col, row) within the active sectors.
func (f *SectorFlowField) Reachable(col, row int) bool { return f.Cost(col, row) != Unreachable }

// Direction returns the unit vector a unit in (col, row) moves along, or the zero vector on a goal, unreachable cell, inactive sector, or outside the grid.
func (f *SectorFlowField) Direction(col, row int) math2d.Vec2 {
	slot, ok := f.cellSlot(col, row)
	if !ok {
		return math2d.Vec2{}
	}
	return f.vec[slot]
}

// CellAt returns the cell holding world position p and whether it is inside the grid.
func (f *SectorFlowField) CellAt(p math2d.Vec2) (ai.Cell, bool) {
	return cellAt(p, f.Origin, validCellSize(f.CellSize), f.cols, f.rows)
}

// CellCenter returns the world position of the center of cell c.
func (f *SectorFlowField) CellCenter(c ai.Cell) math2d.Vec2 {
	return cellCenter(c, f.Origin, validCellSize(f.CellSize))
}

// Sample returns the bilinearly blended movement direction at world position p,
// ignoring walls and inactive sectors just like FlowField.Sample.
func (f *SectorFlowField) Sample(p math2d.Vec2) (dir math2d.Vec2, ok bool) {
	c, in := f.CellAt(p)
	if !in {
		return math2d.Vec2{}, false
	}
	curSlot, active := f.cellSlot(c.Col, c.Row)
	if !active || f.cost[curSlot] == Unreachable {
		return math2d.Vec2{}, false
	}
	if f.cost[curSlot] == 0 {
		return math2d.Vec2{}, true
	}
	size := validCellSize(f.CellSize)
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
		if s.w <= 0 {
			continue
		}
		slot, ok := f.cellSlot(x0+s.dc, y0+s.dr)
		if !ok || f.cost[slot] == Unreachable || f.cost[slot] == 0 {
			continue
		}
		sum = sum.Add(f.vec[slot].Scale(s.w))
		weight += s.w
	}
	if weight <= 0 || sum.LenSq() < 1e-12 {
		return f.vec[curSlot], true
	}
	return sum.Normalize(), true
}

func (f *SectorFlowField) push(n fieldNode) {
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

func (f *SectorFlowField) pop() fieldNode {
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
