package fog

// This file implements symmetric shadowcasting (Albert Ford, "Symmetric Shadowcasting", 2021) with exact integer slopes, so a floor cell A sees a floor cell B exactly when B sees A, and the result never depends on floating-point rounding.

// slope is the exact fraction num/den with den > 0.
type slope struct{ num, den int }

// scanRow is one row of a quadrant at depth cells from the origin, between two slopes.
type scanRow struct {
	depth      int
	start, end slope
}

// quadrant turns a (depth, col) position in one of four 90-degree cones around (ox, oy) into a grid cell.
type quadrant struct {
	dir    int
	ox, oy int
}

func (q quadrant) cell(depth, col int) (x, y int) {
	switch q.dir {
	case 0: // north
		return q.ox + col, q.oy - depth
	case 1: // east
		return q.ox + depth, q.oy + col
	case 2: // south
		return q.ox + col, q.oy + depth
	default: // west
		return q.ox - depth, q.oy + col
	}
}

// floorDiv returns floor(a/b) for b > 0.
func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && a < 0 {
		q--
	}
	return q
}

// ceilDiv returns ceil(a/b) for b > 0.
func ceilDiv(a, b int) int {
	q := a / b
	if a%b != 0 && a > 0 {
		q++
	}
	return q
}

// tileSlope is the slope of a cell's left edge, (2*col-1)/(2*depth).
func tileSlope(depth, col int) slope { return slope{2*col - 1, 2 * depth} }

// shadowcast sets in vis every grid cell a source at (ox, oy) with radius r can see past opaque cells.
func (f *Fog) shadowcast(ox, oy, r int, vis []uint64) {
	setBit(vis, f.index(ox, oy))
	for dir := range 4 {
		q := quadrant{dir: dir, ox: ox, oy: oy}
		f.stack = append(f.stack[:0], scanRow{depth: 1, start: slope{-1, 1}, end: slope{1, 1}})
		for len(f.stack) > 0 {
			row := f.stack[len(f.stack)-1]
			f.stack = f.stack[:len(f.stack)-1]
			f.scan(q, row, r, vis)
		}
	}
}

// scan walks one row of q, revealing cells and pushing the rows beyond it that light still reaches.
func (f *Fog) scan(q quadrant, row scanRow, r int, vis []uint64) {
	if row.depth > r {
		return
	}
	d := row.depth
	// Round depth*start half up and depth*end half down to the first and last column the row covers.
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
		x, y := q.cell(d, col)
		inGrid := f.InBounds(x, y)
		kind := floor
		if !inGrid || bit(f.opaque, f.index(x, y)) {
			kind = wall
		}
		// A wall is seen whenever the row reaches it; a floor cell only when its center lies inside the row's slopes, which keeps sight symmetric.
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
