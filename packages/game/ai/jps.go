package ai

import (
	"math"
	"slices"
)

// JPS implements Jump Point Search for 8-way uniform-cost square grids, returning exact optimal paths
// matching AStar with Diagonal=true while skipping symmetric open-space nodes.
//
// JPS assumes every walkable cell has uniform traversal cost 1 (ignoring CostGrid and EdgeCostGrid).
// Use NewJPS for a searcher with NoCornerCutting enabled by default.
// All internal search buffers are reused across calls; a JPS is not safe for concurrent use by multiple goroutines.
type JPS struct {
	// NoCornerCutting prevents diagonal moves when either adjacent cardinal cell is blocked.
	// NewJPS sets this to true, matching NewAStar.
	NoCornerCutting bool

	gen     uint32
	seen    []uint32
	closed  []uint32
	gScore  []float64
	parent  []int
	heapPos []int
	open    []astarNode
	jpBuf   []Cell
}

// NewJPS returns a JPS searcher with NoCornerCutting enabled by default.
func NewJPS() *JPS {
	return &JPS{NoCornerCutting: true}
}

// Find searches uniform-cost square grid g for an optimal 8-way path from start to goal,
// returning every cell along the path from start to goal inclusive and the exact octile path cost.
func (j *JPS) Find(g Grid, start, goal Cell) (path []Cell, cost float64, ok bool) {
	return j.FindInto(nil, g, start, goal)
}

// FindInto behaves like Find but writes the expanded cell path into dst[:0], avoiding allocations when dst has enough capacity.
func (j *JPS) FindInto(dst []Cell, g Grid, start, goal Cell) (path []Cell, cost float64, ok bool) {
	jps, cost, ok := j.findJumpPoints(g, start, goal)
	if !ok {
		return nil, 0, false
	}
	out := append(dst[:0], jps[0])
	for i := 1; i < len(jps); i++ {
		a := jps[i-1]
		b := jps[i]
		dx := signInt(b.Col - a.Col)
		dy := signInt(b.Row - a.Row)
		c, r := a.Col, a.Row
		for c != b.Col || r != b.Row {
			c += dx
			r += dy
			out = append(out, Cell{Col: c, Row: r})
		}
	}
	return out, cost, true
}

// FindJumpPoints searches uniform-cost square grid g and returns only the jump-point waypoints
// (start, turning points, and goal) together with the exact optimal 8-way cost.
func (j *JPS) FindJumpPoints(g Grid, start, goal Cell) (waypoints []Cell, cost float64, ok bool) {
	return j.FindJumpPointsInto(nil, g, start, goal)
}

// FindJumpPointsInto behaves like FindJumpPoints but writes the jump points into dst[:0].
func (j *JPS) FindJumpPointsInto(dst []Cell, g Grid, start, goal Cell) (waypoints []Cell, cost float64, ok bool) {
	jps, cost, ok := j.findJumpPoints(g, start, goal)
	if !ok {
		return nil, 0, false
	}
	return append(dst[:0], jps...), cost, true
}

type jpsGridView struct {
	g        Grid
	bg       *BoolGrid
	cols     int
	rows     int
	goalCol  int
	goalRow  int
	noCut    bool
	blocked  []bool
	fastBool bool
}

func (v *jpsGridView) walkable(c, r int) bool {
	if c < 0 || c >= v.cols || r < 0 || r >= v.rows {
		return false
	}
	if v.fastBool {
		return !v.blocked[r*v.cols+c]
	}
	return v.g.Walkable(c, r)
}

func (j *JPS) findJumpPoints(g Grid, start, goal Cell) ([]Cell, float64, bool) {
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

	view := jpsGridView{
		g:       g,
		cols:    cols,
		rows:    rows,
		goalCol: goal.Col,
		goalRow: goal.Row,
		noCut:   j.NoCornerCutting,
	}
	if bg, ok := g.(*BoolGrid); ok {
		view.bg = bg
		view.blocked = bg.blocked
		view.fastBool = true
	}

	if !view.walkable(start.Col, start.Row) || !view.walkable(goal.Col, goal.Row) {
		return nil, 0, false
	}
	if start == goal {
		j.jpBuf = append(j.jpBuf[:0], start)
		return j.jpBuf, 0, true
	}

	j.prepare(cols * rows)
	gen := j.gen
	startID := start.Row*cols + start.Col
	goalID := goal.Row*cols + goal.Col

	j.seen[startID] = gen
	j.gScore[startID] = 0
	j.parent[startID] = -1
	h0 := octileDistance(start.Col, start.Row, goal.Col, goal.Row)
	j.heapPush(astarNode{id: startID, f: h0, h: h0})

	var dirs [8][2]int

	for len(j.open) > 0 {
		cur := j.heapPop()
		if cur.id == goalID {
			buf := j.jpBuf[:0]
			for id := goalID; id != -1; id = j.parent[id] {
				buf = append(buf, Cell{Col: id % cols, Row: id / cols})
			}
			slices.Reverse(buf)
			j.jpBuf = buf
			return j.jpBuf, j.gScore[goalID], true
		}
		j.closed[cur.id] = gen

		cx := cur.id % cols
		cy := cur.id / cols
		curG := j.gScore[cur.id]

		dirCount := j.prunedDirs(&view, cur.id, cx, cy, &dirs)
		for i := range dirCount {
			dx, dy := dirs[i][0], dirs[i][1]
			jx, jy, found := view.jump(cx, cy, dx, dy)
			if !found {
				continue
			}
			jid := jy*cols + jx
			if j.closed[jid] == gen {
				continue
			}

			var stepDist float64
			if dx == 0 || dy == 0 {
				stepDist = float64(absInt(jx-cx) + absInt(jy-cy))
			} else {
				stepDist = math.Sqrt2 * float64(absInt(jx-cx))
			}
			ng := curG + stepDist

			if j.seen[jid] != gen {
				j.seen[jid] = gen
				j.gScore[jid] = ng
				j.parent[jid] = cur.id
				h := octileDistance(jx, jy, goal.Col, goal.Row)
				j.heapPush(astarNode{id: jid, f: ng + h, h: h})
			} else if ng < j.gScore[jid] {
				j.gScore[jid] = ng
				j.parent[jid] = cur.id
				h := octileDistance(jx, jy, goal.Col, goal.Row)
				if pos := j.heapPos[jid]; pos >= 0 {
					j.open[pos].f = ng + h
					j.open[pos].h = h
					j.siftUp(pos)
				} else {
					j.heapPush(astarNode{id: jid, f: ng + h, h: h})
				}
			}
		}
	}

	return nil, 0, false
}

// prunedDirs writes the natural and forced search directions from (cx, cy) into dirs in deterministic order.
func (j *JPS) prunedDirs(v *jpsGridView, curID, cx, cy int, dirs *[8][2]int) int {
	pid := j.parent[curID]
	if pid == -1 {
		// Start node: try all 8 directions in deterministic cardinal-then-diagonal order.
		dirs[0] = [2]int{0, -1}
		dirs[1] = [2]int{1, 0}
		dirs[2] = [2]int{0, 1}
		dirs[3] = [2]int{-1, 0}
		dirs[4] = [2]int{1, -1}
		dirs[5] = [2]int{1, 1}
		dirs[6] = [2]int{-1, 1}
		dirs[7] = [2]int{-1, -1}
		return 8
	}

	px := pid % v.cols
	py := pid / v.cols
	dx := signInt(cx - px)
	dy := signInt(cy - py)
	n := 0

	if dx != 0 && dy != 0 {
		// Diagonal arrival: natural neighbors are (dx, 0), (0, dy), (dx, dy).
		dirs[n] = [2]int{dx, 0}
		n++
		dirs[n] = [2]int{0, dy}
		n++
		dirs[n] = [2]int{dx, dy}
		n++
		if !v.noCut {
			if !v.walkable(cx-dx, cy) && v.walkable(cx-dx, cy+dy) {
				dirs[n] = [2]int{-dx, dy}
				n++
			}
			if !v.walkable(cx, cy-dy) && v.walkable(cx+dx, cy-dy) {
				dirs[n] = [2]int{dx, -dy}
				n++
			}
		}
		return n
	}

	if dx != 0 {
		// Horizontal arrival.
		dirs[n] = [2]int{dx, 0}
		n++
		if v.noCut {
			fwdWalk := v.walkable(cx+dx, cy)
			if !v.walkable(cx-dx, cy-1) && v.walkable(cx, cy-1) {
				dirs[n] = [2]int{0, -1}
				n++
				if fwdWalk {
					dirs[n] = [2]int{dx, -1}
					n++
				}
			}
			if !v.walkable(cx-dx, cy+1) && v.walkable(cx, cy+1) {
				dirs[n] = [2]int{0, 1}
				n++
				if fwdWalk {
					dirs[n] = [2]int{dx, 1}
					n++
				}
			}
		} else {
			if !v.walkable(cx, cy-1) && v.walkable(cx+dx, cy-1) {
				dirs[n] = [2]int{dx, -1}
				n++
			}
			if !v.walkable(cx, cy+1) && v.walkable(cx+dx, cy+1) {
				dirs[n] = [2]int{dx, 1}
				n++
			}
		}
		return n
	}

	// Vertical arrival (dy != 0).
	dirs[n] = [2]int{0, dy}
	n++
	if v.noCut {
		fwdWalk := v.walkable(cx, cy+dy)
		if !v.walkable(cx-1, cy-dy) && v.walkable(cx-1, cy) {
			dirs[n] = [2]int{-1, 0}
			n++
			if fwdWalk {
				dirs[n] = [2]int{-1, dy}
				n++
			}
		}
		if !v.walkable(cx+1, cy-dy) && v.walkable(cx+1, cy) {
			dirs[n] = [2]int{1, 0}
			n++
			if fwdWalk {
				dirs[n] = [2]int{1, dy}
				n++
			}
		}
	} else {
		if !v.walkable(cx-1, cy) && v.walkable(cx-1, cy+dy) {
			dirs[n] = [2]int{-1, dy}
			n++
		}
		if !v.walkable(cx+1, cy) && v.walkable(cx+1, cy+dy) {
			dirs[n] = [2]int{1, dy}
			n++
		}
	}
	return n
}

func (v *jpsGridView) jump(x, y, dx, dy int) (int, int, bool) {
	if dy == 0 {
		return v.jumpHoriz(x, y, dx)
	}
	if dx == 0 {
		return v.jumpVert(x, y, dy)
	}
	return v.jumpDiag(x, y, dx, dy)
}

func (v *jpsGridView) jumpHoriz(x, y, dx int) (int, int, bool) {
	for {
		nx := x + dx
		if !v.walkable(nx, y) {
			return 0, 0, false
		}
		if nx == v.goalCol && y == v.goalRow {
			return nx, y, true
		}
		if v.noCut {
			if (!v.walkable(x, y-1) && v.walkable(nx, y-1)) ||
				(!v.walkable(x, y+1) && v.walkable(nx, y+1)) {
				return nx, y, true
			}
		} else {
			if (!v.walkable(nx, y-1) && v.walkable(nx+dx, y-1)) ||
				(!v.walkable(nx, y+1) && v.walkable(nx+dx, y+1)) {
				return nx, y, true
			}
		}
		x = nx
	}
}

func (v *jpsGridView) jumpVert(x, y, dy int) (int, int, bool) {
	for {
		ny := y + dy
		if !v.walkable(x, ny) {
			return 0, 0, false
		}
		if x == v.goalCol && ny == v.goalRow {
			return x, ny, true
		}
		if v.noCut {
			if (!v.walkable(x-1, y) && v.walkable(x-1, ny)) ||
				(!v.walkable(x+1, y) && v.walkable(x+1, ny)) {
				return x, ny, true
			}
		} else {
			if (!v.walkable(x-1, ny) && v.walkable(x-1, ny+dy)) ||
				(!v.walkable(x+1, ny) && v.walkable(x+1, ny+dy)) {
				return x, ny, true
			}
		}
		y = ny
	}
}

func (v *jpsGridView) jumpDiag(x, y, dx, dy int) (int, int, bool) {
	for {
		nx, ny := x+dx, y+dy
		if !v.walkable(nx, ny) {
			return 0, 0, false
		}
		if v.noCut {
			if !v.walkable(nx, y) || !v.walkable(x, ny) {
				return 0, 0, false
			}
		}
		if nx == v.goalCol && ny == v.goalRow {
			return nx, ny, true
		}
		if !v.noCut {
			if (!v.walkable(x, ny) && v.walkable(x, ny+dy)) ||
				(!v.walkable(nx, y) && v.walkable(nx+dx, y)) {
				return nx, ny, true
			}
		}
		if _, _, ok := v.jumpHoriz(nx, ny, dx); ok {
			return nx, ny, true
		}
		if _, _, ok := v.jumpVert(nx, ny, dy); ok {
			return nx, ny, true
		}
		x, y = nx, ny
	}
}

func octileDistance(c1, r1, c2, r2 int) float64 {
	dx := absInt(c1 - c2)
	dy := absInt(r1 - r2)
	return float64(max(dx, dy)) + (math.Sqrt2-1)*float64(min(dx, dy))
}

func signInt(v int) int {
	if v < 0 {
		return -1
	}
	if v > 0 {
		return 1
	}
	return 0
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (j *JPS) prepare(n int) {
	if cap(j.seen) < n {
		j.seen = make([]uint32, n)
		j.closed = make([]uint32, n)
		j.gScore = make([]float64, n)
		j.parent = make([]int, n)
		j.heapPos = make([]int, n)
		j.gen = 0
	} else {
		j.seen = j.seen[:n]
		j.closed = j.closed[:n]
		j.gScore = j.gScore[:n]
		j.parent = j.parent[:n]
		j.heapPos = j.heapPos[:n]
	}
	j.gen++
	if j.gen == 0 {
		clear(j.seen)
		clear(j.closed)
		j.gen = 1
	}
	j.open = j.open[:0]
}

func (j *JPS) heapPush(node astarNode) {
	idx := len(j.open)
	j.open = append(j.open, node)
	j.heapPos[node.id] = idx
	j.siftUp(idx)
}

func (j *JPS) heapPop() astarNode {
	top := j.open[0]
	j.heapPos[top.id] = -1
	last := len(j.open) - 1
	if last == 0 {
		j.open = j.open[:0]
		return top
	}
	j.open[0] = j.open[last]
	j.heapPos[j.open[0].id] = 0
	j.open = j.open[:last]
	j.siftDown(0)
	return top
}

func (j *JPS) siftUp(i int) {
	for i > 0 {
		p := (i - 1) / 2
		if !astarLess(j.open[i], j.open[p]) {
			break
		}
		j.open[i], j.open[p] = j.open[p], j.open[i]
		j.heapPos[j.open[i].id] = i
		j.heapPos[j.open[p].id] = p
		i = p
	}
}

func (j *JPS) siftDown(i int) {
	n := len(j.open)
	for {
		l := 2*i + 1
		if l >= n {
			break
		}
		best := l
		if r := l + 1; r < n && astarLess(j.open[r], j.open[l]) {
			best = r
		}
		if !astarLess(j.open[best], j.open[i]) {
			break
		}
		j.open[i], j.open[best] = j.open[best], j.open[i]
		j.heapPos[j.open[i].id] = i
		j.heapPos[j.open[best].id] = best
		i = best
	}
}
