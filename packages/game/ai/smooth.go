package ai

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// GridPathOptions configures world-space conversion and body clearance for SmoothGridPath, SmoothGridPathInto, and GridLineOfSight.
//
// The zero value uses 1x1 world-unit cells rooted at (0, 0) with a zero-width line-of-sight ray (still preventing corner cuts across blocked cells).
type GridPathOptions struct {
	// Origin is the world-space coordinate of cell (0, 0)'s top-left corner.
	Origin core.Point
	// CellSize is the width and height of one square grid cell in world units.
	// Positive CellWidth or CellHeight values override CellSize on their respective axes.
	// When no positive size is set on an axis, that axis defaults to 1.
	CellSize float32
	// CellWidth overrides CellSize along the X axis when positive, supporting rectangular tiles.
	CellWidth float32
	// CellHeight overrides CellSize along the Y axis when positive, supporting rectangular tiles.
	CellHeight float32
	// Radius is the circular body radius in world units that smoothed segments must keep clear of every blocked cell.
	// Values less than or equal to 0 test a zero-width ray that still rejects corner-cutting through blocked cells.
	Radius float32
}

// CellCenter returns the world-space center of cell c under o.
func (o GridPathOptions) CellCenter(c Cell) core.Point {
	ox, oy, w, h, _ := resolveGridPathOptions(o)
	return core.Point{
		X: core.Points(ox + (float64(c.Col)+0.5)*w),
		Y: core.Points(oy + (float64(c.Row)+0.5)*h),
	}
}

func resolveGridPathOptions(o GridPathOptions) (ox, oy, w, h, r float64) {
	ox = float64(o.Origin.X)
	oy = float64(o.Origin.Y)

	w = float64(o.CellWidth)
	if w <= 0 {
		w = float64(o.CellSize)
	}
	if w <= 0 {
		w = 1
	}

	h = float64(o.CellHeight)
	if h <= 0 {
		h = float64(o.CellSize)
	}
	if h <= 0 {
		h = 1
	}

	r = float64(o.Radius)
	if r < 0 || math.IsNaN(r) {
		r = 0
	}
	return ox, oy, w, h, r
}

// GridLineOfSight reports whether a circular body of radius opts.Radius can move straight from a to b on g without passing within opts.Radius of any blocked cell (!g.Walkable(col, row)).
//
// Returns false when g is nil, has non-positive dimensions, or either endpoint lies outside the grid's world bounds.
func GridLineOfSight(g Grid, a, b core.Point, opts GridPathOptions) bool {
	if g == nil {
		return false
	}
	cols, rows := g.GridSize()
	if cols <= 0 || rows <= 0 {
		return false
	}
	ox, oy, w, h, r := resolveGridPathOptions(opts)
	return gridSegmentClear(g, cols, rows, float64(a.X)-ox, float64(a.Y)-oy, float64(b.X)-ox, float64(b.Y)-oy, w, h, r)
}

// SmoothGridPath converts a grid cell path (such as from AStar.Find) into world-space waypoints, dropping intermediate cells whenever a body of radius opts.Radius can walk straight between kept waypoints without passing within opts.Radius of any blocked cell.
//
// SmoothGridPath checks walkability via g.Walkable and ignores CostGrid terrain weights. When smoothing with a positive opts.Radius, pair it with standard cell-by-cell AStar (AnyAngle: false) so corner cells remain available when the body radius cannot clear a corner directly.
//
// It allocates only the returned waypoint slice; use SmoothGridPathInto to reuse a caller-owned slice with zero allocations.
func SmoothGridPath(g Grid, path []Cell, opts GridPathOptions) []core.Point {
	return SmoothGridPathInto(nil, g, path, opts)
}

// SmoothGridPathInto behaves like SmoothGridPath but appends the smoothed waypoints into dst[:0], avoiding heap allocations when dst has enough capacity.
func SmoothGridPathInto(dst []core.Point, g Grid, path []Cell, opts GridPathOptions) []core.Point {
	out := dst[:0]
	n := len(path)
	if n == 0 {
		return out
	}

	ox, oy, w, h, r := resolveGridPathOptions(opts)
	cellPt := func(c Cell) core.Point {
		return core.Point{
			X: core.Points(ox + (float64(c.Col)+0.5)*w),
			Y: core.Points(oy + (float64(c.Row)+0.5)*h),
		}
	}

	if n == 1 {
		return append(out, cellPt(path[0]))
	}
	if g == nil {
		for _, c := range path {
			out = append(out, cellPt(c))
		}
		return out
	}
	cols, rows := g.GridSize()
	if cols <= 0 || rows <= 0 {
		for _, c := range path {
			out = append(out, cellPt(c))
		}
		return out
	}

	anchor := 0
	anchorPt := cellPt(path[0])
	out = append(out, anchorPt)

	for anchor < n-1 {
		next := anchor + 1
		nextPt := cellPt(path[next])

		// Scan backwards from the goal so open-ground staircases collapse in a single LOS check
		// and non-monotonic visibility around obstacles still finds the farthest clear waypoint.
		for cand := n - 1; cand > anchor+1; cand-- {
			candPt := cellPt(path[cand])
			var clear bool
			if r <= 0 {
				// Use exact origin-canceled cell centers so float32 Origin rounding cannot mask a corner graze.
				ax := (float64(path[anchor].Col) + 0.5) * w
				ay := (float64(path[anchor].Row) + 0.5) * h
				bx := (float64(path[cand].Col) + 0.5) * w
				by := (float64(path[cand].Row) + 0.5) * h
				clear = gridSegmentClear(g, cols, rows, ax, ay, bx, by, w, h, 0)
			} else {
				// Test the exact float32 waypoints that will be returned so every emitted segment satisfies dist >= Radius.
				clear = gridSegmentClear(g, cols, rows, float64(anchorPt.X)-ox, float64(anchorPt.Y)-oy, float64(candPt.X)-ox, float64(candPt.Y)-oy, w, h, r)
			}
			if clear {
				next = cand
				nextPt = candPt
				break
			}
		}

		anchor = next
		anchorPt = nextPt
		out = append(out, anchorPt)
	}

	return out
}

// gridSegmentClear reports whether the segment (ax, ay)->(bx, by) in grid-origin-shifted world coordinates
// stays at least distance r away from every blocked in-bounds cell AABB.
func gridSegmentClear(g Grid, cols, rows int, ax, ay, bx, by, w, h, r float64) bool {
	maxWorldX := float64(cols) * w
	maxWorldY := float64(rows) * h
	if ax < 0 || ax > maxWorldX || ay < 0 || ay > maxWorldY ||
		bx < 0 || bx > maxWorldX || by < 0 || by > maxWorldY {
		return false
	}

	bandPad := max(1e-9, 1e-9*max(w, h))
	pad := r + bandPad

	segMinX, segMaxX := ax, bx
	if segMinX > segMaxX {
		segMinX, segMaxX = segMaxX, segMinX
	}
	segMinY, segMaxY := ay, by
	if segMinY > segMaxY {
		segMinY, segMaxY = segMaxY, segMinY
	}

	minCol := max(0, int(math.Floor((segMinX-pad)/w)))
	maxCol := min(cols-1, int(math.Floor((segMaxX+pad)/w)))
	minRow := max(0, int(math.Floor((segMinY-pad)/h)))
	maxRow := min(rows-1, int(math.Floor((segMaxY+pad)/h)))
	if minCol > maxCol || minRow > maxRow {
		return true
	}

	zeroRadius := r <= 0
	rSq := r * r
	zeroEps := 1e-9 * min(w, h)
	zeroEpsSq := zeroEps * zeroEps

	dx := bx - ax
	dy := by - ay

	if math.Abs(dx) >= math.Abs(dy) && dx != 0 {
		invDX := 1.0 / dx
		for col := minCol; col <= maxCol; col++ {
			t0 := (float64(col)*w - pad - ax) * invDX
			t1 := (float64(col+1)*w + pad - ax) * invDX
			if t0 > t1 {
				t0, t1 = t1, t0
			}
			if t0 < 0 {
				t0 = 0
			}
			if t1 > 1 {
				t1 = 1
			}
			if t0 > t1 {
				continue
			}
			y0 := ay + t0*dy
			y1 := ay + t1*dy
			if y0 > y1 {
				y0, y1 = y1, y0
			}
			rStart := max(minRow, int(math.Floor((y0-pad)/h)))
			rEnd := min(maxRow, int(math.Floor((y1+pad)/h)))
			for row := rStart; row <= rEnd; row++ {
				if !cellClearsSegment(g, col, row, ax, ay, bx, by, w, h, rSq, zeroRadius, zeroEpsSq) {
					return false
				}
			}
		}
		return true
	}

	if dy != 0 {
		invDY := 1.0 / dy
		for row := minRow; row <= maxRow; row++ {
			t0 := (float64(row)*h - pad - ay) * invDY
			t1 := (float64(row+1)*h + pad - ay) * invDY
			if t0 > t1 {
				t0, t1 = t1, t0
			}
			if t0 < 0 {
				t0 = 0
			}
			if t1 > 1 {
				t1 = 1
			}
			if t0 > t1 {
				continue
			}
			x0 := ax + t0*dx
			x1 := ax + t1*dx
			if x0 > x1 {
				x0, x1 = x1, x0
			}
			cStart := max(minCol, int(math.Floor((x0-pad)/w)))
			cEnd := min(maxCol, int(math.Floor((x1+pad)/w)))
			for col := cStart; col <= cEnd; col++ {
				if !cellClearsSegment(g, col, row, ax, ay, bx, by, w, h, rSq, zeroRadius, zeroEpsSq) {
					return false
				}
			}
		}
		return true
	}

	// Degenerate zero-length segment (a == b).
	for row := minRow; row <= maxRow; row++ {
		for col := minCol; col <= maxCol; col++ {
			if !cellClearsSegment(g, col, row, ax, ay, bx, by, w, h, rSq, zeroRadius, zeroEpsSq) {
				return false
			}
		}
	}
	return true
}

func cellClearsSegment(g Grid, col, row int, ax, ay, bx, by, w, h, rSq float64, zeroRadius bool, zeroEpsSq float64) bool {
	if g.Walkable(col, row) {
		return true
	}
	x0 := float64(col) * w
	y0 := float64(row) * h
	d2 := segmentBoxDistSq(ax, ay, bx, by, x0, y0, x0+w, y0+h)
	if zeroRadius {
		return d2 > zeroEpsSq
	}
	return d2 >= rSq
}

// thetaLineOfSight reports whether the straight line between cell centers (c0, r0) and (c1, r1)
// in unit-cell space is unobstructed (and uniform cost 1 when hasCost is true).
func thetaLineOfSight(g Grid, cols, rows int, hasCost bool, cg CostGrid, c0, r0, c1, r1 int, noCornerCut bool) bool {
	ax := float64(c0) + 0.5
	ay := float64(r0) + 0.5
	bx := float64(c1) + 0.5
	by := float64(r1) + 0.5

	const pad = 1e-9
	minCol := max(0, min(c0, c1)-1)
	maxCol := min(cols-1, max(c0, c1)+1)
	minRow := max(0, min(r0, r1)-1)
	maxRow := min(rows-1, max(r0, r1)+1)

	dx := bx - ax
	dy := by - ay

	if math.Abs(dx) >= math.Abs(dy) && dx != 0 {
		invDX := 1.0 / dx
		for col := minCol; col <= maxCol; col++ {
			t0 := (float64(col) - pad - ax) * invDX
			t1 := (float64(col+1) + pad - ax) * invDX
			if t0 > t1 {
				t0, t1 = t1, t0
			}
			if t0 < 0 {
				t0 = 0
			}
			if t1 > 1 {
				t1 = 1
			}
			if t0 > t1 {
				continue
			}
			y0 := ay + t0*dy
			y1 := ay + t1*dy
			if y0 > y1 {
				y0, y1 = y1, y0
			}
			rStart := max(minRow, int(math.Floor(y0-pad)))
			rEnd := min(maxRow, int(math.Floor(y1+pad)))
			for row := rStart; row <= rEnd; row++ {
				if !thetaCellClear(g, hasCost, cg, col, row, ax, ay, bx, by, noCornerCut) {
					return false
				}
			}
		}
		return true
	}

	if dy != 0 {
		invDY := 1.0 / dy
		for row := minRow; row <= maxRow; row++ {
			t0 := (float64(row) - pad - ay) * invDY
			t1 := (float64(row+1) + pad - ay) * invDY
			if t0 > t1 {
				t0, t1 = t1, t0
			}
			if t0 < 0 {
				t0 = 0
			}
			if t1 > 1 {
				t1 = 1
			}
			if t0 > t1 {
				continue
			}
			x0 := ax + t0*dx
			x1 := ax + t1*dx
			if x0 > x1 {
				x0, x1 = x1, x0
			}
			cStart := max(minCol, int(math.Floor(x0-pad)))
			cEnd := min(maxCol, int(math.Floor(x1+pad)))
			for col := cStart; col <= cEnd; col++ {
				if !thetaCellClear(g, hasCost, cg, col, row, ax, ay, bx, by, noCornerCut) {
					return false
				}
			}
		}
		return true
	}

	return true
}

func thetaCellClear(g Grid, hasCost bool, cg CostGrid, col, row int, ax, ay, bx, by float64, noCornerCut bool) bool {
	walkable := g.Walkable(col, row)
	unitCost := true
	if walkable && hasCost {
		tc := cg.MoveCost(col, row)
		if tc > 1 || math.IsInf(tc, 0) || math.IsNaN(tc) {
			unitCost = false
		}
	}
	if walkable && unitCost {
		return true
	}
	x0 := float64(col)
	y0 := float64(row)
	if noCornerCut {
		return segmentBoxDistSq(ax, ay, bx, by, x0, y0, x0+1, y0+1) > 1e-18
	}
	return !segmentIntersectsOpenBox(ax, ay, bx, by, x0, y0, x0+1, y0+1)
}

// segmentBoxDistSq returns the exact squared Euclidean distance between segment (ax, ay)->(bx, by)
// and closed axis-aligned rectangle [minX, maxX] x [minY, maxY].
func segmentBoxDistSq(ax, ay, bx, by, minX, minY, maxX, maxY float64) float64 {
	if segmentIntersectsBox(ax, ay, bx, by, minX, minY, maxX, maxY) {
		return 0
	}
	best := pointBoxDistSq(ax, ay, minX, minY, maxX, maxY)
	if d := pointBoxDistSq(bx, by, minX, minY, maxX, maxY); d < best {
		best = d
	}
	if d := pointSegmentDistSq(minX, minY, ax, ay, bx, by); d < best {
		best = d
	}
	if d := pointSegmentDistSq(maxX, minY, ax, ay, bx, by); d < best {
		best = d
	}
	if d := pointSegmentDistSq(maxX, maxY, ax, ay, bx, by); d < best {
		best = d
	}
	if d := pointSegmentDistSq(minX, maxY, ax, ay, bx, by); d < best {
		best = d
	}
	return best
}

// segmentIntersectsBox reports whether segment (ax, ay)->(bx, by) intersects or touches the closed box [minX, maxX] x [minY, maxY].
func segmentIntersectsBox(ax, ay, bx, by, minX, minY, maxX, maxY float64) bool {
	t0, t1 := 0.0, 1.0
	dx := bx - ax
	dy := by - ay

	if dx == 0 {
		if ax < minX || ax > maxX {
			return false
		}
	} else {
		inv := 1.0 / dx
		tx0 := (minX - ax) * inv
		tx1 := (maxX - ax) * inv
		if tx0 > tx1 {
			tx0, tx1 = tx1, tx0
		}
		if tx0 > t0 {
			t0 = tx0
		}
		if tx1 < t1 {
			t1 = tx1
		}
		if t0 > t1 {
			return false
		}
	}

	if dy == 0 {
		if ay < minY || ay > maxY {
			return false
		}
	} else {
		inv := 1.0 / dy
		ty0 := (minY - ay) * inv
		ty1 := (maxY - ay) * inv
		if ty0 > ty1 {
			ty0, ty1 = ty1, ty0
		}
		if ty0 > t0 {
			t0 = ty0
		}
		if ty1 < t1 {
			t1 = ty1
		}
		if t0 > t1 {
			return false
		}
	}

	return true
}

// segmentIntersectsOpenBox reports whether segment (ax, ay)->(bx, by) enters the strict interior (minX, maxX) x (minY, maxY).
func segmentIntersectsOpenBox(ax, ay, bx, by, minX, minY, maxX, maxY float64) bool {
	t0, t1 := 0.0, 1.0
	dx := bx - ax
	dy := by - ay
	const eps = 1e-12

	if dx == 0 {
		if ax <= minX+eps || ax >= maxX-eps {
			return false
		}
	} else {
		inv := 1.0 / dx
		tx0 := (minX - ax) * inv
		tx1 := (maxX - ax) * inv
		if tx0 > tx1 {
			tx0, tx1 = tx1, tx0
		}
		if tx0 > t0 {
			t0 = tx0
		}
		if tx1 < t1 {
			t1 = tx1
		}
		if t0 >= t1-eps {
			return false
		}
	}

	if dy == 0 {
		if ay <= minY+eps || ay >= maxY-eps {
			return false
		}
	} else {
		inv := 1.0 / dy
		ty0 := (minY - ay) * inv
		ty1 := (maxY - ay) * inv
		if ty0 > ty1 {
			ty0, ty1 = ty1, ty0
		}
		if ty0 > t0 {
			t0 = ty0
		}
		if ty1 < t1 {
			t1 = ty1
		}
		if t0 >= t1-eps {
			return false
		}
	}

	return t0 < t1-eps
}

func pointBoxDistSq(px, py, minX, minY, maxX, maxY float64) float64 {
	var dx, dy float64
	if px < minX {
		dx = minX - px
	} else if px > maxX {
		dx = px - maxX
	}
	if py < minY {
		dy = minY - py
	} else if py > maxY {
		dy = py - maxY
	}
	return dx*dx + dy*dy
}

func pointSegmentDistSq(px, py, ax, ay, bx, by float64) float64 {
	abx := bx - ax
	aby := by - ay
	l2 := abx*abx + aby*aby
	if l2 <= 1e-24 {
		dx := px - ax
		dy := py - ay
		return dx*dx + dy*dy
	}
	t := ((px-ax)*abx + (py-ay)*aby) / l2
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	cx := ax + t*abx
	cy := ay + t*aby
	dx := px - cx
	dy := py - cy
	return dx*dx + dy*dy
}
