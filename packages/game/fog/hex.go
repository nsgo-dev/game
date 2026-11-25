package fog

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/tilemap"
)

func (f *Fog) hexOffsetToAxial(col, row int) (q, r int) {
	if f.StaggerAxis == tilemap.StaggerX {
		if f.StaggerIndex == tilemap.StaggerEven {
			return col, row - (col+(col&1))/2
		}
		return col, row - (col-(col&1))/2
	}
	if f.StaggerIndex == tilemap.StaggerEven {
		return col - (row+(row&1))/2, row
	}
	return col - (row-(row&1))/2, row
}

func (f *Fog) hexAxialToOffset(q, r int) (col, row int) {
	if f.StaggerAxis == tilemap.StaggerX {
		if f.StaggerIndex == tilemap.StaggerEven {
			return q, r + (q+(q&1))/2
		}
		return q, r + (q-(q&1))/2
	}
	if f.StaggerIndex == tilemap.StaggerEven {
		return q + (r+(r&1))/2, r
	}
	return q + (r-(r&1))/2, r
}

// hexSextantAxial maps a sextant-local (depth, k) position (0 <= k <= depth) around axial origin (q0, r0) into axial (q, r) coordinates.
func hexSextantAxial(sextant, q0, r0, d, k int) (q, r int) {
	switch sextant {
	case 0:
		return q0 + d - k, r0 + k
	case 1:
		return q0 - k, r0 + d
	case 2:
		return q0 - d, r0 + d - k
	case 3:
		return q0 + k - d, r0 - k
	case 4:
		return q0 + k, r0 - d
	default:
		return q0 + d, r0 + k - d
	}
}

// hexDisc sets every in-bounds cell within hex distance r of (ox, oy) in vis, for an open hex grid.
func (f *Fog) hexDisc(ox, oy, r int, vis []uint64) {
	q0, r0 := f.hexOffsetToAxial(ox, oy)
	for dq := -r; dq <= r; dq++ {
		drMin := max(-r, -dq-r)
		drMax := min(r, -dq+r)
		for dr := drMin; dr <= drMax; dr++ {
			col, row := f.hexAxialToOffset(q0+dq, r0+dr)
			if f.InBounds(col, row) {
				setBit(vis, f.index(col, row))
			}
		}
	}
}

// hexShadowcast runs symmetric 6-sextant shadowcasting with exact integer slopes in axial/cube coordinates, revealing every cell within hex distance r that has unobstructed sight from (ox, oy).
func (f *Fog) hexShadowcast(ox, oy, r int, vis []uint64) {
	setBit(vis, f.index(ox, oy))
	if r <= 0 {
		return
	}
	q0, r0 := f.hexOffsetToAxial(ox, oy)
	for sextant := range 6 {
		f.stack = append(f.stack[:0], scanRow{depth: 1, start: slope{0, 1}, end: slope{1, 1}})
		for len(f.stack) > 0 {
			row := f.stack[len(f.stack)-1]
			f.stack = f.stack[:len(f.stack)-1]
			f.scanHex(sextant, q0, r0, row, r, vis)
		}
	}
}

// scanHex walks one ring of a hex sextant at row.depth, revealing cells and pushing unobstructed sub-arcs onto f.stack.
func (f *Fog) scanHex(sextant, q0, r0 int, row scanRow, r int, vis []uint64) {
	if row.depth > r {
		return
	}
	d := row.depth
	minK := max(0, floorDiv(2*d*row.start.num+row.start.den, 2*row.start.den))
	maxK := min(d, ceilDiv(2*d*row.end.num-row.end.den, 2*row.end.den))
	const (
		none = iota
		wall
		floor
	)
	prev := none
	start := row.start
	for k := minK; k <= maxK; k++ {
		q, ra := hexSextantAxial(sextant, q0, r0, d, k)
		x, y := f.hexAxialToOffset(q, ra)
		inGrid := f.InBounds(x, y)
		kind := floor
		if !inGrid || bit(f.opaque, f.index(x, y)) {
			kind = wall
		}
		symmetric := k*start.den >= d*start.num && k*row.end.den <= d*row.end.num
		if inGrid && (kind == wall || symmetric) {
			setBit(vis, f.index(x, y))
		}
		if prev == wall && kind == floor {
			start = tileSlope(d, k)
		}
		if prev == floor && kind == wall {
			f.stack = append(f.stack, scanRow{depth: d + 1, start: start, end: tileSlope(d, k)})
		}
		prev = kind
	}
	if prev == floor {
		f.stack = append(f.stack, scanRow{depth: d + 1, start: start, end: row.end})
	}
}

func (f *Fog) staggeredToIso(col, row int) (u, v int) {
	if f.StaggerAxis == tilemap.StaggerX {
		sx := col
		var sy int
		if f.StaggerIndex == tilemap.StaggerEven {
			sy = 2*row - (col & 1)
		} else {
			sy = 2*row + (col & 1)
		}
		return (sx + sy) / 2, (sy - sx) / 2
	}
	var sx int
	if f.StaggerIndex == tilemap.StaggerEven {
		sx = 2*col - (row & 1)
	} else {
		sx = 2*col + (row & 1)
	}
	sy := row
	return (sx + sy) / 2, (sy - sx) / 2
}

func (f *Fog) isoToStaggered(u, v int) (col, row int) {
	sx := u - v
	sy := u + v
	if f.StaggerAxis == tilemap.StaggerX {
		col = sx
		if f.StaggerIndex == tilemap.StaggerEven {
			row = (sy + (col & 1)) / 2
		} else {
			row = (sy - (col & 1)) / 2
		}
		return col, row
	}
	row = sy
	if f.StaggerIndex == tilemap.StaggerEven {
		col = (sx + (row & 1)) / 2
	} else {
		col = (sx - (row & 1)) / 2
	}
	return col, row
}

// staggeredDisc sets every in-bounds staggered isometric cell whose diamond-lattice offset lies in the rounded disc of radius r.
func (f *Fog) staggeredDisc(ox, oy, r int, vis []uint64) {
	u0, v0 := f.staggeredToIso(ox, oy)
	for dv := -r; dv <= r; dv++ {
		limit := r*r + r - dv*dv
		w := int(math.Sqrt(float64(limit)))
		for w*w > limit {
			w--
		}
		for (w+1)*(w+1) <= limit {
			w++
		}
		for du := -w; du <= w; du++ {
			col, row := f.isoToStaggered(u0+du, v0+dv)
			if f.InBounds(col, row) {
				setBit(vis, f.index(col, row))
			}
		}
	}
}

// staggeredShadowcast runs symmetric shadowcasting in the staggered grid's diamond-isometric lattice (u, v).
func (f *Fog) staggeredShadowcast(ox, oy, r int, vis []uint64) {
	setBit(vis, f.index(ox, oy))
	u0, v0 := f.staggeredToIso(ox, oy)
	for dir := range 4 {
		q := quadrant{dir: dir, ox: u0, oy: v0}
		f.stack = append(f.stack[:0], scanRow{depth: 1, start: slope{-1, 1}, end: slope{1, 1}})
		for len(f.stack) > 0 {
			row := f.stack[len(f.stack)-1]
			f.stack = f.stack[:len(f.stack)-1]
			f.scanStaggered(q, row, r, vis)
		}
	}
}

func (f *Fog) scanStaggered(q quadrant, row scanRow, r int, vis []uint64) {
	if row.depth > r {
		return
	}
	d := row.depth
	minCol := floorDiv(2*d*row.start.num+row.start.den, 2*row.start.den)
	maxCol := ceilDiv(2*d*row.end.num-row.end.den, 2*row.end.den)
	const (
		none = iota
		wall
		floor
	)
	prev := none
	start := row.start
	for col := minCol; col <= maxCol; col++ {
		u, v := q.cell(d, col)
		x, y := f.isoToStaggered(u, v)
		inGrid := f.InBounds(x, y)
		kind := floor
		if !inGrid || bit(f.opaque, f.index(x, y)) {
			kind = wall
		}
		symmetric := col*start.den >= d*start.num && col*row.end.den <= d*row.end.num
		if inGrid && (kind == wall || symmetric) && inRange(d, col, r) {
			setBit(vis, f.index(x, y))
		}
		if prev == wall && kind == floor {
			start = tileSlope(d, col)
		}
		if prev == floor && kind == wall {
			f.stack = append(f.stack, scanRow{depth: d + 1, start: start, end: tileSlope(d, col)})
		}
		prev = kind
	}
	if prev == floor {
		f.stack = append(f.stack, scanRow{depth: d + 1, start: start, end: row.end})
	}
}
