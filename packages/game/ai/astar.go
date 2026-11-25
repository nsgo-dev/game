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
// Costs below 1 are clamped to 1 so a cheaper cell cannot make the distance
// heuristic overestimate and break A*'s optimality.
func (m *CostMap) MoveCost(col, row int) float64 {
	if col < 0 || col >= m.cols || row < 0 || row >= m.rows {
		return 1
	}
	c := m.costs[row*m.cols+col]
	if c < 1 || math.IsInf(c, 0) || math.IsNaN(c) {
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

// SetCost sets the traversal cost for (col, row). Negative, infinite or NaN costs mark the cell unwalkable; 0 or a cost below 1 restores the default cost of 1, as the distance heuristic assumes every step costs at least 1.
func (m *CostMap) SetCost(col, row int, cost float64) {
	if col < 0 || col >= m.cols || row < 0 || row >= m.rows {
		return
	}
	if cost < 0 || math.IsInf(cost, 0) || math.IsNaN(cost) {
		m.costs[row*m.cols+col] = -1
		return
	}
	if cost < 1 {
		cost = 0
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

// MoveCost returns Cost(col, row) or 1 when Cost is nil or returns a value
// below 1: the distance heuristic assumes every step costs at least 1.
func (f GridFunc) MoveCost(col, row int) float64 {
	if f.Cost == nil {
		return 1
	}
	c := f.Cost(col, row)
	if c < 1 || math.IsInf(c, 0) || math.IsNaN(c) {
		return 1
	}
	return c
}

// GridPathResult is the outcome of a bounded or incremental grid A* search.
type GridPathResult struct {
	Path       []Cell
	Cost       float64
	Found      bool // true when a complete or partial path was produced
	Partial    bool // true when Path ends at the closest explored cell rather than goal
	Exhausted  bool // true when the search stopped because MaxExpansions was reached
	Expansions int  // total number of grid cells expanded during the search
}

// AStar finds lowest-cost paths on a Grid, reusing its internal search buffers across calls.
//
// NewAStar returns a searcher with NoCornerCutting enabled by default. An AStar is not safe for concurrent use by multiple goroutines.
type AStar struct {
	// Diagonal allows 8-way movement with step distance sqrt(2) on diagonals; false restricts movement to 4 cardinal directions.
	// On hexagonal topologies Diagonal has no effect; TopologySquare4 and TopologySquare8 override Diagonal.
	Diagonal bool
	// NoCornerCutting prevents diagonal moves when either adjacent cardinal cell is blocked. It only takes effect when diagonal moves are enabled, and also prevents Theta* any-angle lines of sight from grazing blocked cell corners when AnyAngle is true.
	NoCornerCutting bool
	// AnyAngle enables Theta*-style any-angle pathfinding on square grids (TopologySquare, TopologySquare4, TopologySquare8),
	// linking neighbors directly to an ancestor cell whenever line of sight between cell centers is unobstructed across uniform-cost cells.
	// AnyAngle is ignored on hexagonal or staggered topologies and on EdgeCostGrid maps.
	// When smoothing a path for a body with positive clearance radius, prefer standard cell-by-cell A* (AnyAngle: false) followed by SmoothGridPath or SmoothGridPathInto.
	AnyAngle bool
	// Topology selects the cell adjacency and admissible distance heuristic. The zero value (TopologySquare) preserves square-grid behavior.
	Topology GridTopology
	// MaxExpansions caps the number of cells expanded per Find or FindInto call.
	// Zero or negative means unlimited expansions. FindWithOptions uses this limit when PathOptions.MaxExpansions is zero.
	MaxExpansions int

	astarState
}

type astarState struct {
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
	res := a.FindWithOptionsInto(dst, g, start, goal, PathOptions{})
	if !res.Found || res.Partial {
		return nil, 0, false
	}
	return res.Path, res.Cost, true
}

// FindWithOptions searches g from start to goal under opts, supporting an expansion budget
// and optional partial paths to the closest explored cell when the goal is unreachable or the budget runs out.
func (a *AStar) FindWithOptions(g Grid, start, goal Cell, opts PathOptions) GridPathResult {
	return a.FindWithOptionsInto(nil, g, start, goal, opts)
}

// FindWithOptionsInto behaves like FindWithOptions but writes the resulting path into dst[:0],
// avoiding allocations when dst has enough capacity.
func (a *AStar) FindWithOptionsInto(dst []Cell, g Grid, start, goal Cell, opts PathOptions) GridPathResult {
	var runner astarRunner
	runner.init(&a.astarState, a.Diagonal, a.NoCornerCutting, a.AnyAngle, a.Topology, effectiveAStarMaxExpansions(a, opts), opts.AllowPartial, g, start, goal)
	runner.step(&a.astarState, -1)
	return runner.resultInto(&a.astarState, dst)
}

// StartSearch begins an incremental grid A* search using a's configuration (Diagonal, NoCornerCutting, Topology, MaxExpansions).
// The returned AStarSearch owns independent search buffers so multiple searches may run across frames concurrently.
func (a *AStar) StartSearch(g Grid, start, goal Cell, opts PathOptions) *AStarSearch {
	return NewAStarSearch(a, g, start, goal, opts)
}

// AStarSearch runs a deterministic grid A* search incrementally across bounded Step calls.
//
// Stepping a search with any positive per-step expansion budget produces the exact same path,
// cost, partial flag, and expansion count as a single-shot AStar.FindWithOptions call.
// *AStarSearch implements IncrementalSearch.
type AStarSearch struct {
	state  astarState
	runner astarRunner
}

// NewAStarSearch initializes an incremental grid A* search copying configuration from a
// (when a is nil, defaults to 4-way TopologySquare with NoCornerCutting enabled).
func NewAStarSearch(a *AStar, g Grid, start, goal Cell, opts PathOptions) *AStarSearch {
	s := &AStarSearch{}
	s.Reset(a, g, start, goal, opts)
	return s
}

// Reset reinitializes s for a new query, reusing its internal search buffers.
func (s *AStarSearch) Reset(a *AStar, g Grid, start, goal Cell, opts PathOptions) {
	diagonal := false
	noCornerCutting := true
	anyAngle := false
	topology := TopologySquare
	if a != nil {
		diagonal = a.Diagonal
		noCornerCutting = a.NoCornerCutting
		anyAngle = a.AnyAngle
		topology = a.Topology
	}
	s.runner.init(&s.state, diagonal, noCornerCutting, anyAngle, topology, effectiveAStarMaxExpansions(a, opts), opts.AllowPartial, g, start, goal)
}

// Step advances the search by at most maxExpansions grid cell expansions and returns
// the updated status and exact number of expansions performed.
// When maxExpansions <= 0 or the search is already finished, Step performs 0 expansions.
func (s *AStarSearch) Step(maxExpansions int) (status SearchStatus, expansions int) {
	if s == nil {
		return SearchFailed, 0
	}
	if maxExpansions <= 0 || s.runner.status.Done() {
		return s.runner.status, 0
	}
	return s.runner.step(&s.state, maxExpansions)
}

// Status returns the search's current status.
func (s *AStarSearch) Status() SearchStatus {
	if s == nil {
		return SearchFailed
	}
	return s.runner.status
}

// Expansions returns the cumulative number of grid cells expanded so far.
func (s *AStarSearch) Expansions() int {
	if s == nil {
		return 0
	}
	return s.runner.expansions
}

// Result returns the search outcome once finished (or a result with Found == false while still in progress).
func (s *AStarSearch) Result() GridPathResult {
	return s.ResultInto(nil)
}

// ResultInto behaves like Result but writes the path into dst[:0] when finished.
func (s *AStarSearch) ResultInto(dst []Cell) GridPathResult {
	if s == nil {
		return GridPathResult{}
	}
	return s.runner.resultInto(&s.state, dst)
}

func effectiveAStarMaxExpansions(a *AStar, opts PathOptions) int {
	if opts.MaxExpansions > 0 {
		return opts.MaxExpansions
	}
	if opts.MaxExpansions < 0 {
		return 0
	}
	if a != nil && a.MaxExpansions > 0 {
		return a.MaxExpansions
	}
	return 0
}

type astarRunner struct {
	g               Grid
	eg              EdgeCostGrid
	hasEdgeCost     bool
	cg              CostGrid
	hasCost         bool
	cols            int
	rows            int
	start           Cell
	goal            Cell
	startID         int
	goalID          int
	goalReachable   bool
	sameStartGoal   bool
	diagonal        bool
	noCornerCutting bool
	anyAngle        bool
	topology        GridTopology
	maxExpansions   int
	allowPartial    bool
	gen             uint32

	status     SearchStatus
	exhausted  bool
	expansions int
	hasBest    bool
	bestID     int
	bestH      float64
	bestG      float64
}

func (r *astarRunner) heuristic(c1, r1, c2, r2 int) float64 {
	if r.anyAngle {
		return math.Hypot(float64(c2-c1), float64(r2-r1))
	}
	return r.topology.distance(c1, r1, c2, r2, r.diagonal)
}

func (r *astarRunner) init(state *astarState, diagonal, noCornerCutting, anyAngle bool, topology GridTopology, maxExpansions int, allowPartial bool, g Grid, start, goal Cell) {
	*r = astarRunner{
		g:               g,
		start:           start,
		goal:            goal,
		diagonal:        diagonal,
		noCornerCutting: noCornerCutting,
		topology:        topology,
		maxExpansions:   maxExpansions,
		allowPartial:    allowPartial,
		status:          SearchFailed,
	}
	if g == nil {
		return
	}
	cols, rows := g.GridSize()
	if cols <= 0 || rows <= 0 {
		return
	}
	if !inBounds(start, cols, rows) || !g.Walkable(start.Col, start.Row) {
		return
	}
	r.cols = cols
	r.rows = rows

	if start == goal {
		r.sameStartGoal = true
		r.status = SearchComplete
		return
	}

	goalReachable := inBounds(goal, cols, rows) && g.Walkable(goal.Col, goal.Row)
	if !goalReachable && !allowPartial {
		return
	}
	r.goalReachable = goalReachable
	r.startID = start.Row*cols + start.Col
	if goalReachable {
		r.goalID = goal.Row*cols + goal.Col
	} else {
		r.goalID = -1
	}

	r.eg, r.hasEdgeCost = g.(EdgeCostGrid)
	r.cg, r.hasCost = g.(CostGrid)
	r.anyAngle = anyAngle && !r.hasEdgeCost && (topology == TopologySquare || topology == TopologySquare4 || topology == TopologySquare8)

	state.prepare(cols * rows)
	r.gen = state.gen

	state.seen[r.startID] = r.gen
	state.gScore[r.startID] = 0
	state.parent[r.startID] = -1
	h0 := r.heuristic(start.Col, start.Row, goal.Col, goal.Row)
	state.heapPush(astarNode{id: r.startID, f: h0, h: h0})

	r.hasBest = true
	r.bestID = r.startID
	r.bestH = h0
	r.bestG = 0
	r.status = SearchInProgress
}

// step advances the runner by up to stepLimit expansions (< 0 means no per-step cap).
func (r *astarRunner) step(state *astarState, stepLimit int) (SearchStatus, int) {
	if r.status.Done() {
		return r.status, 0
	}

	budget := stepLimit
	if r.maxExpansions > 0 {
		rem := r.maxExpansions - r.expansions
		if rem <= 0 {
			r.finishExhausted()
			return r.status, 0
		}
		if budget < 0 || rem < budget {
			budget = rem
		}
	}

	var steps [8]neighborStep
	done := 0
	gen := r.gen
	cols, rows := r.cols, r.rows
	goal := r.goal
	g := r.g

	for len(state.open) > 0 && (budget < 0 || done < budget) {
		cur := state.heapPop()
		done++
		r.expansions++

		if r.goalReachable && cur.id == r.goalID {
			r.status = SearchComplete
			return r.status, done
		}
		state.closed[cur.id] = gen

		curG := state.gScore[cur.id]
		curParent := state.parent[cur.id]
		if r.allowPartial {
			if cur.h < r.bestH || (cur.h == r.bestH && (curG < r.bestG || (curG == r.bestG && cur.id < r.bestID))) {
				r.bestH = cur.h
				r.bestG = curG
				r.bestID = cur.id
			}
		}

		c := cur.id % cols
		row := cur.id / cols

		stepCount := r.topology.neighbors(c, row, r.diagonal, &steps)
		for i := range stepCount {
			st := steps[i]
			nc, nr := st.col, st.row
			if nc < 0 || nc >= cols || nr < 0 || nr >= rows {
				continue
			}
			nid := nr*cols + nc
			if state.closed[nid] == gen {
				continue
			}
			if !g.Walkable(nc, nr) {
				continue
			}
			if st.diag && r.noCornerCutting {
				if !g.Walkable(st.corner1Col, st.corner1Row) || !g.Walkable(st.corner2Col, st.corner2Row) {
					continue
				}
			}

			tileCost := 1.0
			if r.hasEdgeCost {
				ec := r.eg.EdgeCost(c, row, nc, nr)
				if ec < 0 || math.IsInf(ec, 0) || math.IsNaN(ec) {
					continue
				}
				if ec >= 1 {
					tileCost = ec
				}
			} else if r.hasCost {
				tc := r.cg.MoveCost(nc, nr)
				if tc > 0 && !math.IsInf(tc, 0) && !math.IsNaN(tc) {
					tileCost = tc
				}
			}
			ng := curG + st.dist*tileCost
			nParent := cur.id

			if r.anyAngle && curParent != -1 {
				pc := curParent % cols
				pr := curParent / cols
				if thetaLineOfSight(g, cols, rows, r.hasCost, r.cg, pc, pr, nc, nr, r.noCornerCutting) {
					losG := state.gScore[curParent] + math.Hypot(float64(nc-pc), float64(nr-pr))
					if losG <= ng {
						ng = losG
						nParent = curParent
					}
				}
			}

			if state.seen[nid] != gen {
				state.seen[nid] = gen
				state.gScore[nid] = ng
				state.parent[nid] = nParent
				h := r.heuristic(nc, nr, goal.Col, goal.Row)
				state.heapPush(astarNode{id: nid, f: ng + h, h: h})
			} else if ng < state.gScore[nid] {
				state.gScore[nid] = ng
				state.parent[nid] = nParent
				h := r.heuristic(nc, nr, goal.Col, goal.Row)
				if pos := state.heapPos[nid]; pos >= 0 {
					state.open[pos].f = ng + h
					state.open[pos].h = h
					state.siftUp(pos)
				} else {
					state.heapPush(astarNode{id: nid, f: ng + h, h: h})
				}
			}
		}
	}

	if len(state.open) == 0 {
		r.exhausted = false
		if r.allowPartial && r.hasBest {
			r.status = SearchPartial
		} else {
			r.status = SearchFailed
		}
	} else if r.maxExpansions > 0 && r.expansions >= r.maxExpansions {
		r.finishExhausted()
	}

	return r.status, done
}

func (r *astarRunner) finishExhausted() {
	r.exhausted = true
	if r.allowPartial && r.hasBest {
		r.status = SearchPartial
	} else {
		r.status = SearchFailed
	}
}

func (r *astarRunner) resultInto(state *astarState, dst []Cell) GridPathResult {
	res := GridPathResult{
		Exhausted:  r.exhausted,
		Expansions: r.expansions,
	}
	switch r.status {
	case SearchComplete:
		if r.sameStartGoal {
			res.Path = append(dst[:0], r.start)
			res.Found = true
			return res
		}
		res.Path = reconstructGridPath(dst, state.parent, r.cols, r.goalID)
		res.Cost = state.gScore[r.goalID]
		res.Found = true
		return res
	case SearchPartial:
		res.Path = reconstructGridPath(dst, state.parent, r.cols, r.bestID)
		res.Cost = state.gScore[r.bestID]
		res.Found = true
		res.Partial = true
		return res
	default:
		return res
	}
}

func reconstructGridPath(dst []Cell, parent []int, cols, endID int) []Cell {
	out := dst[:0]
	for id := endID; id != -1; id = parent[id] {
		out = append(out, Cell{Col: id % cols, Row: id / cols})
	}
	slices.Reverse(out)
	return out
}

func inBounds(c Cell, cols, rows int) bool {
	return c.Col >= 0 && c.Col < cols && c.Row >= 0 && c.Row < rows
}

func (a *AStar) heuristicFor(c1, r1, c2, r2 int, anyAngle bool) float64 {
	if anyAngle {
		return math.Hypot(float64(c1-c2), float64(r1-r2))
	}
	return a.Topology.distance(c1, r1, c2, r2, a.Diagonal)
}

func (a *astarState) prepare(n int) {
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

func (a *astarState) heapPush(node astarNode) {
	idx := len(a.open)
	a.open = append(a.open, node)
	a.heapPos[node.id] = idx
	a.siftUp(idx)
}

func (a *astarState) heapPop() astarNode {
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

func (a *astarState) siftUp(i int) {
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

func (a *astarState) siftDown(i int) {
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
