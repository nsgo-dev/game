package rts

import (
	"math"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/pathfind"
)

// MoveOrder sends a group of units to a target in formation.
type MoveOrder struct {
	// Target is the world position the formation is centered on.
	Target math2d.Vec2
	// Formation lays out the units around Target. A zero Formation packs them into a box whose spacing fits the largest unit with the crowd's Gap.
	Formation Formation
	// Facing is the direction the formation faces; the zero vector faces from the group's centroid toward Target.
	Facing math2d.Vec2
	// KeepPace caps every unit at the slowest unit's speed, so the group moves and arrives together.
	KeepPace bool
}

// Move orders the units in ids that exist to take up o's formation around o.Target and returns how many were ordered.
//
// The slots are placed around the target facing o.Facing, and any slot that falls on a blocked cell, or on a cell the group cannot reach, moves to the nearest free cell that can be reached. Units are matched to slots with Assign, and the whole group shares one flow field whose goals are the slots' cells, built once (or found in the crowd's FieldCache) however many units there are. Each unit follows that field until it can see its own slot within the crowd's DirectRange, then walks straight to it; a unit that reaches the formation's area with a wall between it and its slot follows its own pathfind.AStar path there for the rest of the order, planning it afresh whenever the crowd pushes it off it.
//
// When the units cannot reach o.Target, as when it lies on an island, the formation centers on the nearest cell they can reach instead. When o.Target is outside the grid, or the units stand nowhere they could walk from, Move orders nobody and returns 0.
func (c *Crowd) Move(ids []UnitID, o MoveOrder) int {
	idx := make([]int, 0, len(ids))
	seen := make(map[UnitID]bool, len(ids))
	for _, id := range ids {
		if i, ok := c.index[id]; ok && !seen[id] {
			seen[id] = true
			idx = append(idx, i)
		}
	}
	n := len(idx)
	if n == 0 {
		return 0
	}
	targetCell, in := c.cellAt(o.Target)
	if !in {
		return 0
	}
	// The field from the target cell tells which cells the group can reach, so slots can be moved off walls and islands.
	reach, ok := c.fields.Field(targetCell)
	if !ok {
		near, found := c.nearestWalkable(targetCell)
		if !found {
			return 0
		}
		targetCell = near
		reach, _ = c.fields.Field(targetCell)
	}
	center := o.Target
	if !c.anyReachable(reach, idx) {
		// No unit can get to the target, as when it is on an island or behind a closed wall: go to the nearest place they can reach instead.
		cell, found := c.nearestToUnits(idx, o.Target)
		if !found {
			return 0
		}
		targetCell = cell
		center = reach.CellCenter(cell)
		reach, _ = c.fields.Field(targetCell)
	}

	positions := make([]math2d.Vec2, n)
	var centroid math2d.Vec2
	maxRadius := float32(0)
	slowest := float32(math.Inf(1))
	for k, i := range idx {
		u := &c.units[i]
		positions[k] = u.Pos
		centroid = centroid.Add(u.Pos)
		maxRadius = max(maxRadius, u.Radius)
		slowest = min(slowest, u.MaxSpeed)
	}
	centroid = centroid.Scale(1 / float32(n))

	f := o.Formation
	if f.Validate() != nil {
		if f.Shape == "" {
			f.Shape = ShapeBox
		}
		if !(f.Spacing > 0) || !finite(f.Spacing) {
			f.Spacing = 2*maxRadius + c.opts.Gap + 0.1*c.opts.CellSize
		}
	}
	facing := o.Facing
	if facing.LenSq() < 1e-12 || !finite(facing.X) || !finite(facing.Y) {
		facing = center.Sub(centroid)
	}
	slots := f.Layout(nil, n)
	targets := Place(nil, slots, center, facing)
	assigned := Assign(nil, positions, slots, facing)

	// Move slots off blocked or unreachable cells, one slot per cell where the map allows.
	taken := make(map[pathfind.Cell]bool, n)
	for _, t := range targets {
		if cell, in := c.cellAt(t); in && reach.Reachable(cell.Col, cell.Row) {
			taken[cell] = true
		}
	}
	for k, t := range targets {
		cell, in := c.cellAt(t)
		if in && reach.Reachable(cell.Col, cell.Row) {
			continue
		}
		if free, found := c.nearestReachable(reach, t, taken); found {
			taken[free] = true
			targets[k] = reach.CellCenter(free)
		} else {
			targets[k] = reach.CellCenter(targetCell)
		}
	}

	// Keep each slot a body's width clear of walls so the unit sent there can stand on it.
	for k, t := range targets {
		targets[k] = c.pushOut(t, maxRadius, nil)
	}

	goals := make([]pathfind.Cell, 0, n)
	for _, t := range targets {
		if cell, in := c.cellAt(t); in {
			goals = append(goals, cell)
		}
	}
	field, ok := c.fields.Field(goals...)
	if !ok {
		field = reach
	}
	for k, i := range idx {
		u := &c.units[i]
		u.Target = targets[assigned[k]]
		u.State = Moving
		u.field, u.direct, u.detour = field, false, false
		u.path, u.pathIdx = u.path[:0], 0
		u.speedCap = u.MaxSpeed
		if o.KeepPace {
			u.speedCap = slowest
		}
	}
	return n
}

// anyReachable reports whether f reaches the cell of any of the units at indices idx.
func (c *Crowd) anyReachable(f *FlowField, idx []int) bool {
	for _, i := range idx {
		if cell, in := c.cellAt(c.units[i].Pos); in && f.Reachable(cell.Col, cell.Row) {
			return true
		}
	}
	return false
}

// nearestToUnits returns the cell, among those the units at indices idx can reach, whose center is nearest p.
func (c *Crowd) nearestToUnits(idx []int, p math2d.Vec2) (pathfind.Cell, bool) {
	goals := make([]pathfind.Cell, 0, len(idx))
	for _, i := range idx {
		if cell, in := c.cellAt(c.units[i].Pos); in {
			goals = append(goals, cell)
		}
	}
	// Walkability is symmetric, so the cells reachable from the units are those a field toward them reaches. The field is one-off and stays out of the cache.
	f := NewFlowField(c.opts.Origin, c.opts.CellSize)
	if !f.Build(c.grid, goals...) {
		return pathfind.Cell{}, false
	}
	return c.nearestReachable(f, p, nil)
}

// nearestWalkable returns the walkable cell nearest c, searching outward ring by ring.
func (c *Crowd) nearestWalkable(cell pathfind.Cell) (pathfind.Cell, bool) {
	cols, rows := c.gridSize()
	for r := 1; r < max(cols, rows); r++ {
		for _, cand := range ring(cell, r) {
			if cand.Col >= 0 && cand.Row >= 0 && cand.Col < cols && cand.Row < rows && c.walkable(cand.Col, cand.Row) {
				return cand, true
			}
		}
	}
	return pathfind.Cell{}, false
}

// nearestReachable returns the cell reachable in f, not in taken, whose center is nearest p, searching outward ring by ring from p's cell; when every reachable cell is taken it returns the nearest reachable one.
func (c *Crowd) nearestReachable(f *FlowField, p math2d.Vec2, taken map[pathfind.Cell]bool) (pathfind.Cell, bool) {
	cols, rows := c.gridSize()
	start, _ := c.cellAt(p)
	var fallback pathfind.Cell
	haveFallback := false
	for r := 0; r <= max(cols, rows); r++ {
		best := pathfind.Cell{}
		bestDist := float32(math.Inf(1))
		for _, cand := range ring(start, r) {
			if !f.Reachable(cand.Col, cand.Row) {
				continue
			}
			d := f.CellCenter(cand).DistSq(p)
			if !haveFallback {
				fallback, haveFallback = cand, true
			}
			if taken[cand] {
				continue
			}
			if d < bestDist {
				best, bestDist = cand, d
			}
		}
		if bestDist != float32(math.Inf(1)) {
			return best, true
		}
	}
	return fallback, haveFallback
}

// ring returns the cells at Chebyshev distance r from c, in a fixed clockwise order starting at the top-left corner.
func ring(c pathfind.Cell, r int) []pathfind.Cell {
	if r == 0 {
		return []pathfind.Cell{c}
	}
	out := make([]pathfind.Cell, 0, 8*r)
	for x := -r; x <= r; x++ {
		out = append(out, pathfind.Cell{Col: c.Col + x, Row: c.Row - r})
	}
	for y := -r + 1; y <= r; y++ {
		out = append(out, pathfind.Cell{Col: c.Col + r, Row: c.Row + y})
	}
	for x := r - 1; x >= -r; x-- {
		out = append(out, pathfind.Cell{Col: c.Col + x, Row: c.Row + r})
	}
	for y := r - 1; y > -r; y-- {
		out = append(out, pathfind.Cell{Col: c.Col - r, Row: c.Row + y})
	}
	return out
}
