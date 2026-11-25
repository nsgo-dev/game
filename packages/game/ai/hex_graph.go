package ai

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/tilemap"
)

// HexGrid adapts a hexagonal tilemap.Tilemap to HeuristicGraph[Cell] (and Grid / EdgeCostGrid)
// using tilemap.Tilemap.HexNeighbors and tilemap.Tilemap.HexDistance across all stagger axes
// (StaggerX, StaggerY) and stagger indices (StaggerOdd, StaggerEven).
//
// Heuristic returns the exact cube step distance (Map.HexDistance), which is admissible whenever
// every step costs at least 1. When using sub-unit road costs (< 1), prefer GraphSearch.FindDijkstra,
// GraphSearch.Range, or GraphSearch.FindWithHeuristic with a scaled distance heuristic.
type HexGrid struct {
	// Map is the backing hexagonal tilemap. Must be non-nil with positive Width and Height.
	Map *tilemap.Tilemap
	// SolidMask selects which tilemap.TileFlags block entry into a cell.
	// When 0, defaults to tilemap.TileSolid unless Walk is set.
	SolidMask tilemap.TileFlags
	// Walk optionally overrides cell walkability for in-bounds cells.
	// When nil, a cell is walkable iff Map.FlagsAt(col, row)&effectiveSolidMask == 0.
	Walk func(col, row int) bool
	// Cost optionally returns the traversal cost of entering (col, row).
	// Negative, NaN, or infinite values mark the cell unwalkable when StepCost is nil.
	// When nil, every walkable cell has cost 1.
	Cost func(col, row int) float64
	// StepCost optionally returns the directed traversal cost from -> to, taking precedence over Cost.
	// Negative, NaN, or infinite values forbid moving along that edge.
	StepCost func(from, to Cell) float64
}

// NewHexGrid returns a HexGrid over m blocking tiles with tilemap.TileSolid and using unit step cost 1.
func NewHexGrid(m *tilemap.Tilemap) *HexGrid {
	return &HexGrid{
		Map:       m,
		SolidMask: tilemap.TileSolid,
	}
}

// GridSize returns the underlying tilemap's width and height in cells.
func (h *HexGrid) GridSize() (cols, rows int) {
	if h == nil || h.Map == nil || h.Map.Width <= 0 || h.Map.Height <= 0 {
		return 0, 0
	}
	return h.Map.Width, h.Map.Height
}

// Walkable reports whether (col, row) is in bounds and passable.
func (h *HexGrid) Walkable(col, row int) bool {
	if h == nil || h.Map == nil {
		return false
	}
	if col < 0 || col >= h.Map.Width || row < 0 || row >= h.Map.Height {
		return false
	}
	if h.Walk != nil {
		if !h.Walk(col, row) {
			return false
		}
	} else {
		mask := h.SolidMask
		if mask == 0 {
			mask = tilemap.TileSolid
		}
		if h.Map.FlagsAt(col, row)&mask != 0 {
			return false
		}
	}
	if h.Cost != nil && h.StepCost == nil {
		c := h.Cost(col, row)
		if c < 0 || math.IsNaN(c) || math.IsInf(c, 0) {
			return false
		}
	}
	return true
}

// ValidNode reports whether c is in bounds and passable.
func (h *HexGrid) ValidNode(c Cell) bool {
	return h.Walkable(c.Col, c.Row)
}

// MoveCost returns the traversal cost of entering (col, row) for CostGrid compatibility,
// clamping values below 1 to 1 so AStar's distance heuristic remains admissible.
func (h *HexGrid) MoveCost(col, row int) float64 {
	if h == nil || h.Cost == nil {
		return 1
	}
	c := h.Cost(col, row)
	if c < 1 || math.IsNaN(c) || math.IsInf(c, 0) {
		return 1
	}
	return c
}

// EdgeCost returns the directed traversal cost from (fromCol, fromRow) to (toCol, toRow)
// for EdgeCostGrid compatibility. Negative, NaN, or infinite values forbid the transition.
func (h *HexGrid) EdgeCost(fromCol, fromRow, toCol, toRow int) float64 {
	if h == nil {
		return -1
	}
	if h.StepCost != nil {
		return h.StepCost(Cell{Col: fromCol, Row: fromRow}, Cell{Col: toCol, Row: toRow})
	}
	if h.Cost != nil {
		return h.Cost(toCol, toRow)
	}
	return 1
}

// AppendNeighbors appends all passable hexagonal neighbor transitions of c onto dst
// using Map.HexNeighbors in deterministic order.
func (h *HexGrid) AppendNeighbors(dst []GraphEdge[Cell], c Cell) []GraphEdge[Cell] {
	if !h.ValidNode(c) {
		return dst
	}
	nbs := h.Map.HexNeighbors(c.Col, c.Row)
	for i := range nbs {
		nc, nr := nbs[i][0], nbs[i][1]
		if !h.Walkable(nc, nr) {
			continue
		}
		to := Cell{Col: nc, Row: nr}
		var cost float64
		switch {
		case h.StepCost != nil:
			cost = h.StepCost(c, to)
		case h.Cost != nil:
			cost = h.Cost(nc, nr)
		default:
			cost = 1
		}
		if cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0) {
			continue
		}
		dst = append(dst, GraphEdge[Cell]{To: to, Cost: cost})
	}
	return dst
}

// Heuristic returns the exact hexagonal step distance between from and goal via Map.HexDistance.
func (h *HexGrid) Heuristic(from, goal Cell) float64 {
	if h == nil || h.Map == nil {
		return 0
	}
	return float64(h.Map.HexDistance(from.Col, from.Row, goal.Col, goal.Row))
}

// NodeLess orders cells in row-major order, matching AStar's deterministic cell-ID tie-breaking.
func (h *HexGrid) NodeLess(a, b Cell) bool {
	if a.Row != b.Row {
		return a.Row < b.Row
	}
	return a.Col < b.Col
}

var (
	_ HeuristicGraph[Cell] = (*HexGrid)(nil)
	_ ValidatedGraph[Cell] = (*HexGrid)(nil)
	_ NodeOrder[Cell]      = (*HexGrid)(nil)
	_ Grid                 = (*HexGrid)(nil)
	_ CostGrid             = (*HexGrid)(nil)
	_ EdgeCostGrid         = (*HexGrid)(nil)
)
