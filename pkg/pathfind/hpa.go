package pathfind

import (
	"math"
	"slices"
)

const (
	// DefaultHPAClusterSize is the default width and height in cells of each HPA* cluster.
	DefaultHPAClusterSize = 16
	// DefaultHPAMaxEntranceSpan is the maximum contiguous walkable border span represented by a single entrance before splitting into endpoint entrances.
	DefaultHPAMaxEntranceSpan = 6
)

// ClusterID identifies a cluster in an HPA grid by its cluster column and row.
type ClusterID struct {
	Col, Row int
}

// ClusterRect describes the inclusive-exclusive cell bounds [MinCol, MaxCol) x [MinRow, MaxRow) of an HPA cluster.
type ClusterRect struct {
	ID     ClusterID
	MinCol int
	MinRow int
	MaxCol int
	MaxRow int
}

// HPAOptions configures an HPA hierarchical pathfinder.
type HPAOptions struct {
	// ClusterSize is the width and height of each cluster in cells.
	// Values <= 0 use DefaultHPAClusterSize (16); 1 is clamped to 2.
	ClusterSize int
	// MaxEntranceSpan is the maximum contiguous walkable border run represented by a single entrance.
	// Longer runs receive two entrances at their endpoints (plus a lowest-cost cell on weighted grids when cheaper).
	// Values <= 0 use DefaultHPAMaxEntranceSpan (6).
	MaxEntranceSpan int
	// Diagonal allows 8-way movement with step distance sqrt(2) on diagonals; false restricts movement to 4 cardinal directions.
	Diagonal bool
	// AllowCornerCutting permits diagonal moves when one or both adjacent cardinal cells are blocked.
	// When false (the default), diagonal moves never cut corners, matching NewAStar.
	AllowCornerCutting bool
}

// HPAStats reports structural and incremental-rebuild metrics for an HPA instance.
type HPAStats struct {
	ClusterCols          int
	ClusterRows          int
	ClusterSize          int
	NodeCount            int
	DirtyClusters        int
	TotalClusterRebuilds int
	LastRebuiltClusters  int
}

type hpaEdge struct {
	to   int
	cost float64
}

type hpaNode struct {
	cell       Cell
	cluster    int
	refCount   int
	alive      bool
	interEdges []hpaEdge
	intraEdges []hpaEdge
}

type hpaBorderLink struct {
	nodeA int
	nodeB int
}

type hpaCluster struct {
	minCol, minRow int
	maxCol, maxRow int // exclusive
	entrances      []int
}

type hpaOpenNode struct {
	id     int
	cellID int
	f      float64
	h      float64
}

// HPA implements Hierarchical Pathfinding A* (HPA*) over a square Grid, CostGrid, or EdgeCostGrid.
//
// It partitions the grid into fixed-size square clusters, places entrance nodes along shared cluster borders,
// precomputes intra-cluster shortest paths between entrances, and supports O(affected clusters) incremental updates
// via UpdateCell, UpdateRect, and RebuildDirty.
//
// All search and refinement buffers are reused across calls. An HPA is not safe for concurrent use by multiple goroutines.
type HPA struct {
	grid               Grid
	costGrid           CostGrid
	edgeCostGrid       EdgeCostGrid
	boolGrid           *BoolGrid
	costMap            *CostMap
	cols, rows         int
	clusterSize        int
	maxEntranceSpan    int
	clusterCols        int
	clusterRows        int
	diagonal           bool
	allowCornerCutting bool

	clusters []hpaCluster
	nodes    []hpaNode
	freeList []int
	liveNode int

	// Shared borders between adjacent clusters.
	// vBorders[cy*(clusterCols-1)+cx] separates (cx, cy) and (cx+1, cy).
	// hBorders[cy*clusterCols+cx] separates (cx, cy) and (cx, cy+1).
	// corners[cy*(clusterCols-1)+cx] holds diagonal corner links when Diagonal && AllowCornerCutting.
	vBorders [][]hpaBorderLink
	hBorders [][]hpaBorderLink
	corners  [][]hpaBorderLink

	// Dirty flags and queues so RebuildDirty touches only affected clusters/borders.
	dirtyClusterFlag []bool
	dirtyClusters    []int
	dirtyVFlag       []bool
	dirtyVBorders    []int
	dirtyHFlag       []bool
	dirtyHBorders    []int
	dirtyCFlag       []bool
	dirtyCorners     []int

	totalClusterRebuilds int
	lastRebuiltClusters  int

	// Reusable cluster-local Dijkstra/A* buffers (sized to clusterSize*clusterSize).
	localGen     uint32
	localSeen    []uint32
	localClosed  []uint32
	localG       []float64
	localParent  []int
	localHeapPos []int
	localOpen    []astarNode
	localRev     []Cell
	localTargets []uint32 // generation-stamped target marker for intra-cluster Dijkstra
	localTgtGen  uint32

	// Reusable abstract A* buffers.
	absGen       uint32
	absSeen      []uint32
	absClosed    []uint32
	absG         []float64
	absParent    []int
	absHeapPos   []int
	absOpen      []hpaOpenNode
	goalEdgeGen  []uint32
	goalEdgeCost []float64
	startEdges   []hpaEdge
	absPath      []int
}

// NewHPA builds an HPA* hierarchy over g with opts.
//
// If g is nil or empty, the returned HPA is valid but all searches return ok=false.
func NewHPA(g Grid, opts HPAOptions) *HPA {
	cs := opts.ClusterSize
	if cs <= 0 {
		cs = DefaultHPAClusterSize
	} else if cs < 2 {
		cs = 2
	}
	span := opts.MaxEntranceSpan
	if span <= 0 {
		span = DefaultHPAMaxEntranceSpan
	}

	h := &HPA{
		grid:               g,
		clusterSize:        cs,
		maxEntranceSpan:    span,
		diagonal:           opts.Diagonal,
		allowCornerCutting: opts.AllowCornerCutting,
	}
	if g == nil {
		return h
	}
	cols, rows := g.GridSize()
	if cols <= 0 || rows <= 0 {
		return h
	}
	h.cols, h.rows = cols, rows
	if eg, ok := g.(EdgeCostGrid); ok {
		h.edgeCostGrid = eg
	} else if cg, ok := g.(CostGrid); ok {
		h.costGrid = cg
	}
	if bg, ok := g.(*BoolGrid); ok {
		h.boolGrid = bg
	}
	if cm, ok := g.(*CostMap); ok {
		h.costMap = cm
	}

	h.clusterCols = (cols + cs - 1) / cs
	h.clusterRows = (rows + cs - 1) / cs
	numClusters := h.clusterCols * h.clusterRows
	h.clusters = make([]hpaCluster, numClusters)
	for cy := range h.clusterRows {
		minR := cy * cs
		maxR := min(minR+cs, rows)
		for cx := range h.clusterCols {
			minC := cx * cs
			maxC := min(minC+cs, cols)
			h.clusters[cy*h.clusterCols+cx] = hpaCluster{
				minCol: minC,
				minRow: minR,
				maxCol: maxC,
				maxRow: maxR,
			}
		}
	}

	if h.clusterCols > 1 {
		nv := (h.clusterCols - 1) * h.clusterRows
		h.vBorders = make([][]hpaBorderLink, nv)
		h.dirtyVFlag = make([]bool, nv)
	}
	if h.clusterRows > 1 {
		nh := h.clusterCols * (h.clusterRows - 1)
		h.hBorders = make([][]hpaBorderLink, nh)
		h.dirtyHFlag = make([]bool, nh)
	}
	if h.diagonal && h.allowCornerCutting && h.clusterCols > 1 && h.clusterRows > 1 {
		nc := (h.clusterCols - 1) * (h.clusterRows - 1)
		h.corners = make([][]hpaBorderLink, nc)
		h.dirtyCFlag = make([]bool, nc)
	}
	h.dirtyClusterFlag = make([]bool, numClusters)

	area := cs * cs
	h.localSeen = make([]uint32, area)
	h.localClosed = make([]uint32, area)
	h.localG = make([]float64, area)
	h.localParent = make([]int, area)
	h.localHeapPos = make([]int, area)
	h.localTargets = make([]uint32, area)

	// Build all borders and clusters initially.
	for i := range h.vBorders {
		h.rebuildVBorder(i)
	}
	for i := range h.hBorders {
		h.rebuildHBorder(i)
	}
	for i := range h.corners {
		h.rebuildCorner(i)
	}
	for cid := range numClusters {
		h.rebuildClusterIntra(cid)
	}
	h.totalClusterRebuilds = numClusters
	h.lastRebuiltClusters = numClusters
	return h
}

// Grid returns the underlying Grid this HPA hierarchy was built over.
func (h *HPA) Grid() Grid { return h.grid }

// GridSize returns the width and height of the underlying grid in cells.
func (h *HPA) GridSize() (cols, rows int) { return h.cols, h.rows }

// ClusterSize returns the configured cluster width and height in cells.
func (h *HPA) ClusterSize() int { return h.clusterSize }

// ClusterGridSize returns the number of cluster columns and cluster rows.
func (h *HPA) ClusterGridSize() (cols, rows int) { return h.clusterCols, h.clusterRows }

// Diagonal reports whether 8-way diagonal movement is enabled.
func (h *HPA) Diagonal() bool { return h.diagonal }

// AllowCornerCutting reports whether diagonal moves may cut blocked corners.
func (h *HPA) AllowCornerCutting() bool { return h.allowCornerCutting }

// ClusterAt returns the ClusterID containing cell c and whether c is in bounds.
func (h *HPA) ClusterAt(c Cell) (ClusterID, bool) {
	if c.Col < 0 || c.Col >= h.cols || c.Row < 0 || c.Row >= h.rows {
		return ClusterID{}, false
	}
	return ClusterID{Col: c.Col / h.clusterSize, Row: c.Row / h.clusterSize}, true
}

// ClusterRect returns the cell bounds of cluster id and whether id is in bounds.
func (h *HPA) ClusterRect(id ClusterID) (ClusterRect, bool) {
	if id.Col < 0 || id.Col >= h.clusterCols || id.Row < 0 || id.Row >= h.clusterRows {
		return ClusterRect{}, false
	}
	cl := h.clusters[id.Row*h.clusterCols+id.Col]
	return ClusterRect{
		ID:     id,
		MinCol: cl.minCol,
		MinRow: cl.minRow,
		MaxCol: cl.maxCol,
		MaxRow: cl.maxRow,
	}, true
}

// Stats returns current graph size and cluster rebuild counters.
func (h *HPA) Stats() HPAStats {
	return HPAStats{
		ClusterCols:          h.clusterCols,
		ClusterRows:          h.clusterRows,
		ClusterSize:          h.clusterSize,
		NodeCount:            h.liveNode,
		DirtyClusters:        len(h.dirtyClusters),
		TotalClusterRebuilds: h.totalClusterRebuilds,
		LastRebuiltClusters:  h.lastRebuiltClusters,
	}
}

// UpdateCell marks the cluster containing (col, row), and any shared border and neighboring cluster it touches, as dirty.
//
// Call RebuildDirty to apply pending updates immediately, or let the next Find / FindAbstract / FindClusters call rebuild them automatically.
func (h *HPA) UpdateCell(col, row int) {
	if col < 0 || col >= h.cols || row < 0 || row >= h.rows {
		return
	}
	cx := col / h.clusterSize
	cy := row / h.clusterSize
	cid := cy*h.clusterCols + cx
	h.markClusterDirty(cid)

	cl := h.clusters[cid]
	onLeft := col == cl.minCol && cx > 0
	onRight := col == cl.maxCol-1 && cx+1 < h.clusterCols
	onTop := row == cl.minRow && cy > 0
	onBottom := row == cl.maxRow-1 && cy+1 < h.clusterRows

	if onLeft {
		h.markVBorderDirty(cy*(h.clusterCols-1) + (cx - 1))
		h.markClusterDirty(cy*h.clusterCols + (cx - 1))
	}
	if onRight {
		h.markVBorderDirty(cy*(h.clusterCols-1) + cx)
		h.markClusterDirty(cy*h.clusterCols + (cx + 1))
	}
	if onTop {
		h.markHBorderDirty((cy-1)*h.clusterCols + cx)
		h.markClusterDirty((cy-1)*h.clusterCols + cx)
	}
	if onBottom {
		h.markHBorderDirty(cy*h.clusterCols + cx)
		h.markClusterDirty((cy+1)*h.clusterCols + cx)
	}
	if h.diagonal && h.allowCornerCutting {
		if onLeft && onTop {
			h.markCornerDirty((cy-1)*(h.clusterCols-1) + (cx - 1))
			h.markClusterDirty((cy-1)*h.clusterCols + (cx - 1))
		}
		if onRight && onTop {
			h.markCornerDirty((cy-1)*(h.clusterCols-1) + cx)
			h.markClusterDirty((cy-1)*h.clusterCols + (cx + 1))
		}
		if onLeft && onBottom {
			h.markCornerDirty(cy*(h.clusterCols-1) + (cx - 1))
			h.markClusterDirty((cy+1)*h.clusterCols + (cx - 1))
		}
		if onRight && onBottom {
			h.markCornerDirty(cy*(h.clusterCols-1) + cx)
			h.markClusterDirty((cy+1)*h.clusterCols + (cx + 1))
		}
	}
}

// UpdateRect marks every cluster and shared border overlapping the inclusive cell rectangle [minCol..maxCol] x [minRow..maxRow] as dirty.
func (h *HPA) UpdateRect(minCol, minRow, maxCol, maxRow int) {
	if h.cols <= 0 || h.rows <= 0 {
		return
	}
	if minCol > maxCol {
		minCol, maxCol = maxCol, minCol
	}
	if minRow > maxRow {
		minRow, maxRow = maxRow, minRow
	}
	if maxCol < 0 || maxRow < 0 || minCol >= h.cols || minRow >= h.rows {
		return
	}
	minCol = max(minCol, 0)
	minRow = max(minRow, 0)
	maxCol = min(maxCol, h.cols-1)
	maxRow = min(maxRow, h.rows-1)

	cx0 := minCol / h.clusterSize
	cx1 := maxCol / h.clusterSize
	cy0 := minRow / h.clusterSize
	cy1 := maxRow / h.clusterSize

	for cy := cy0; cy <= cy1; cy++ {
		for cx := cx0; cx <= cx1; cx++ {
			h.markClusterDirty(cy*h.clusterCols + cx)
			if cx < cx1 {
				h.markVBorderDirty(cy*(h.clusterCols-1) + cx)
			}
			if cy < cy1 {
				h.markHBorderDirty(cy*h.clusterCols + cx)
			}
			if h.diagonal && h.allowCornerCutting && cx < cx1 && cy < cy1 {
				h.markCornerDirty(cy*(h.clusterCols-1) + cx)
			}
		}
	}

	// Check outer boundary touches with untouched neighboring clusters.
	touchLeft := minCol == h.clusters[cx0].minCol && cx0 > 0
	touchRight := maxCol == h.clusters[cx1].maxCol-1 && cx1+1 < h.clusterCols
	touchTop := minRow == h.clusters[cy0*h.clusterCols].minRow && cy0 > 0
	touchBottom := maxRow == h.clusters[cy1*h.clusterCols].maxRow-1 && cy1+1 < h.clusterRows

	if touchLeft {
		for cy := cy0; cy <= cy1; cy++ {
			h.markVBorderDirty(cy*(h.clusterCols-1) + (cx0 - 1))
			h.markClusterDirty(cy*h.clusterCols + (cx0 - 1))
		}
	}
	if touchRight {
		for cy := cy0; cy <= cy1; cy++ {
			h.markVBorderDirty(cy*(h.clusterCols-1) + cx1)
			h.markClusterDirty(cy*h.clusterCols + (cx1 + 1))
		}
	}
	if touchTop {
		for cx := cx0; cx <= cx1; cx++ {
			h.markHBorderDirty((cy0-1)*h.clusterCols + cx)
			h.markClusterDirty((cy0-1)*h.clusterCols + cx)
		}
	}
	if touchBottom {
		for cx := cx0; cx <= cx1; cx++ {
			h.markHBorderDirty(cy1*h.clusterCols + cx)
			h.markClusterDirty((cy1+1)*h.clusterCols + cx)
		}
	}
	if h.diagonal && h.allowCornerCutting {
		if touchLeft && touchTop {
			h.markCornerDirty((cy0-1)*(h.clusterCols-1) + (cx0 - 1))
			h.markClusterDirty((cy0-1)*h.clusterCols + (cx0 - 1))
		}
		if touchRight && touchTop {
			h.markCornerDirty((cy0-1)*(h.clusterCols-1) + cx1)
			h.markClusterDirty((cy0-1)*h.clusterCols + (cx1 + 1))
		}
		if touchLeft && touchBottom {
			h.markCornerDirty(cy1*(h.clusterCols-1) + (cx0 - 1))
			h.markClusterDirty((cy1+1)*h.clusterCols + (cx0 - 1))
		}
		if touchRight && touchBottom {
			h.markCornerDirty(cy1*(h.clusterCols-1) + cx1)
			h.markClusterDirty((cy1+1)*h.clusterCols + (cx1 + 1))
		}
	}
}

func (h *HPA) markClusterDirty(cid int) {
	if !h.dirtyClusterFlag[cid] {
		h.dirtyClusterFlag[cid] = true
		h.dirtyClusters = append(h.dirtyClusters, cid)
	}
}

func (h *HPA) markVBorderDirty(idx int) {
	if !h.dirtyVFlag[idx] {
		h.dirtyVFlag[idx] = true
		h.dirtyVBorders = append(h.dirtyVBorders, idx)
	}
}

func (h *HPA) markHBorderDirty(idx int) {
	if !h.dirtyHFlag[idx] {
		h.dirtyHFlag[idx] = true
		h.dirtyHBorders = append(h.dirtyHBorders, idx)
	}
}

func (h *HPA) markCornerDirty(idx int) {
	if !h.dirtyCFlag[idx] {
		h.dirtyCFlag[idx] = true
		h.dirtyCorners = append(h.dirtyCorners, idx)
	}
}

func (h *HPA) hasDirty() bool {
	return len(h.dirtyClusters) > 0 || len(h.dirtyVBorders) > 0 || len(h.dirtyHBorders) > 0 || len(h.dirtyCorners) > 0
}

// RebuildDirty incrementally rebuilds only the borders and clusters marked dirty by UpdateCell or UpdateRect.
//
// It returns the number of clusters rebuilt.
func (h *HPA) RebuildDirty() int {
	if !h.hasDirty() {
		h.lastRebuiltClusters = 0
		return 0
	}
	for _, idx := range h.dirtyVBorders {
		h.dirtyVFlag[idx] = false
		h.rebuildVBorder(idx)
	}
	h.dirtyVBorders = h.dirtyVBorders[:0]

	for _, idx := range h.dirtyHBorders {
		h.dirtyHFlag[idx] = false
		h.rebuildHBorder(idx)
	}
	h.dirtyHBorders = h.dirtyHBorders[:0]

	for _, idx := range h.dirtyCorners {
		h.dirtyCFlag[idx] = false
		h.rebuildCorner(idx)
	}
	h.dirtyCorners = h.dirtyCorners[:0]

	rebuilt := len(h.dirtyClusters)
	for _, cid := range h.dirtyClusters {
		h.dirtyClusterFlag[cid] = false
		h.rebuildClusterIntra(cid)
	}
	h.dirtyClusters = h.dirtyClusters[:0]

	h.totalClusterRebuilds += rebuilt
	h.lastRebuiltClusters = rebuilt
	return rebuilt
}

// Find searches the hierarchical graph for a near-optimal path from start to goal,
// refining it into a contiguous concrete slice of walkable cells and returning the exact path cost.
func (h *HPA) Find(start, goal Cell) (path []Cell, cost float64, ok bool) {
	return h.FindInto(nil, start, goal)
}

// FindInto behaves like Find but writes the refined cell path into dst[:0], avoiding heap allocations when dst has enough capacity.
func (h *HPA) FindInto(dst []Cell, start, goal Cell) (path []Cell, cost float64, ok bool) {
	if !h.validateEndpoints(start, goal) {
		return nil, 0, false
	}
	if start == goal {
		return append(dst[:0], start), 0, true
	}
	if h.hasDirty() {
		h.RebuildDirty()
	}

	_, ok = h.searchAbstract(start, goal)
	if !ok {
		return nil, 0, false
	}

	out := append(dst[:0], start)
	totalCost := 0.0
	for i := 1; i < len(h.absPath); i++ {
		prevID := h.absPath[i-1]
		curID := h.absPath[i]
		prevCell := h.nodeCell(prevID, start, goal)
		curCell := h.nodeCell(curID, start, goal)
		if prevCell == curCell {
			continue
		}
		prevClus := h.nodeCluster(prevID, start, goal)
		curClus := h.nodeCluster(curID, start, goal)
		if prevClus != curClus {
			// Direct 1-step inter-cluster transition.
			stepDist := 1.0
			if prevCell.Col != curCell.Col && prevCell.Row != curCell.Row {
				stepDist = math.Sqrt2
			}
			sc := h.stepCost(prevCell.Col, prevCell.Row, curCell.Col, curCell.Row, stepDist)
			if sc < 0 {
				return nil, 0, false
			}
			out = append(out, curCell)
			totalCost += sc
		} else {
			var segCost float64
			var segOK bool
			out, segCost, segOK = h.refineIntraCluster(out, prevClus, prevCell, curCell)
			if !segOK {
				return nil, 0, false
			}
			totalCost += segCost
		}
	}
	return out, totalCost, true
}

// FindAbstract searches only the hierarchical abstract graph without refining intra-cluster segments,
// returning the sequence of entrance waypoints from start to goal inclusive and the path cost.
func (h *HPA) FindAbstract(start, goal Cell) (waypoints []Cell, cost float64, ok bool) {
	return h.FindAbstractInto(nil, start, goal)
}

// FindAbstractInto behaves like FindAbstract but writes the abstract waypoints into dst[:0].
func (h *HPA) FindAbstractInto(dst []Cell, start, goal Cell) (waypoints []Cell, cost float64, ok bool) {
	if !h.validateEndpoints(start, goal) {
		return nil, 0, false
	}
	if start == goal {
		return append(dst[:0], start), 0, true
	}
	if h.hasDirty() {
		h.RebuildDirty()
	}
	totalCost, ok := h.searchAbstract(start, goal)
	if !ok {
		return nil, 0, false
	}
	out := dst[:0]
	for _, id := range h.absPath {
		c := h.nodeCell(id, start, goal)
		if len(out) == 0 || out[len(out)-1] != c {
			out = append(out, c)
		}
	}
	return out, totalCost, true
}

// FindClusters searches only the hierarchical abstract graph and returns the ordered list of clusters traversed from start to goal.
func (h *HPA) FindClusters(start, goal Cell) (clusters []ClusterID, cost float64, ok bool) {
	return h.FindClustersInto(nil, start, goal)
}

// FindClustersInto behaves like FindClusters but writes the traversed ClusterIDs into dst[:0].
func (h *HPA) FindClustersInto(dst []ClusterID, start, goal Cell) (clusters []ClusterID, cost float64, ok bool) {
	if !h.validateEndpoints(start, goal) {
		return nil, 0, false
	}
	if start == goal {
		cid := ClusterID{Col: start.Col / h.clusterSize, Row: start.Row / h.clusterSize}
		return append(dst[:0], cid), 0, true
	}
	if h.hasDirty() {
		h.RebuildDirty()
	}
	totalCost, ok := h.searchAbstract(start, goal)
	if !ok {
		return nil, 0, false
	}
	out := dst[:0]
	for _, id := range h.absPath {
		cid := h.nodeCluster(id, start, goal)
		cl := ClusterID{Col: cid % h.clusterCols, Row: cid / h.clusterCols}
		if len(out) == 0 || out[len(out)-1] != cl {
			out = append(out, cl)
		}
	}
	return out, totalCost, true
}

func (h *HPA) validateEndpoints(start, goal Cell) bool {
	if h.grid == nil || h.cols <= 0 || h.rows <= 0 {
		return false
	}
	if !inBounds(start, h.cols, h.rows) || !inBounds(goal, h.cols, h.rows) {
		return false
	}
	return h.walkable(start.Col, start.Row) && h.walkable(goal.Col, goal.Row)
}

func (h *HPA) walkable(col, row int) bool {
	if col < 0 || col >= h.cols || row < 0 || row >= h.rows {
		return false
	}
	if h.boolGrid != nil {
		return !h.boolGrid.blocked[row*h.cols+col]
	}
	if h.costMap != nil {
		c := h.costMap.costs[row*h.cols+col]
		return c >= 0 && !math.IsInf(c, 0) && !math.IsNaN(c)
	}
	return h.grid.Walkable(col, row)
}

// stepCost returns the cost of stepping from (fc, fr) to (tc, tr) with geometric length dist,
// or -1 if the step is forbidden. Assumes both cells are in bounds and walkable.
func (h *HPA) stepCost(fc, fr, tc, tr int, dist float64) float64 {
	if h.edgeCostGrid != nil {
		ec := h.edgeCostGrid.EdgeCost(fc, fr, tc, tr)
		if ec < 0 || math.IsInf(ec, 0) || math.IsNaN(ec) {
			return -1
		}
		if ec < 1 {
			return dist
		}
		return dist * ec
	}
	if h.costMap != nil {
		c := h.costMap.costs[tr*h.cols+tc]
		if c < 1 || math.IsInf(c, 0) || math.IsNaN(c) {
			return dist
		}
		return dist * c
	}
	if h.costGrid != nil {
		tcCost := h.costGrid.MoveCost(tc, tr)
		if tcCost < 1 || math.IsInf(tcCost, 0) || math.IsNaN(tcCost) {
			return dist
		}
		return dist * tcCost
	}
	return dist
}

func (h *HPA) heuristic(c1, r1, c2, r2 int) float64 {
	dx := c1 - c2
	if dx < 0 {
		dx = -dx
	}
	dy := r1 - r2
	if dy < 0 {
		dy = -dy
	}
	if !h.diagonal {
		return float64(dx + dy)
	}
	return float64(max(dx, dy)) + (math.Sqrt2-1)*float64(min(dx, dy))
}

// --- Border and entrance management ---

func (h *HPA) getOrCreateEntrance(cid int, cell Cell) int {
	cl := &h.clusters[cid]
	for _, nid := range cl.entrances {
		if h.nodes[nid].cell == cell {
			return nid
		}
	}
	var nid int
	if n := len(h.freeList); n > 0 {
		nid = h.freeList[n-1]
		h.freeList = h.freeList[:n-1]
		node := &h.nodes[nid]
		node.cell = cell
		node.cluster = cid
		node.refCount = 0
		node.alive = true
		node.interEdges = node.interEdges[:0]
		node.intraEdges = node.intraEdges[:0]
	} else {
		nid = len(h.nodes)
		h.nodes = append(h.nodes, hpaNode{
			cell:    cell,
			cluster: cid,
			alive:   true,
		})
	}
	h.liveNode++
	cl.entrances = append(cl.entrances, nid)
	return nid
}

func (h *HPA) clearBorderLinks(links []hpaBorderLink) []hpaBorderLink {
	for _, lk := range links {
		h.removeInterEdge(lk.nodeA, lk.nodeB)
		h.removeInterEdge(lk.nodeB, lk.nodeA)
		h.releaseEntrance(lk.nodeA)
		h.releaseEntrance(lk.nodeB)
	}
	return links[:0]
}

func (h *HPA) removeInterEdge(from, to int) {
	edges := h.nodes[from].interEdges
	for i := range edges {
		if edges[i].to == to {
			edges[i] = edges[len(edges)-1]
			h.nodes[from].interEdges = edges[:len(edges)-1]
			return
		}
	}
}

func (h *HPA) releaseEntrance(nid int) {
	node := &h.nodes[nid]
	node.refCount--
	if node.refCount > 0 {
		return
	}
	node.alive = false
	node.interEdges = node.interEdges[:0]
	node.intraEdges = node.intraEdges[:0]
	h.liveNode--
	cl := &h.clusters[node.cluster]
	for i, e := range cl.entrances {
		if e == nid {
			cl.entrances[i] = cl.entrances[len(cl.entrances)-1]
			cl.entrances = cl.entrances[:len(cl.entrances)-1]
			break
		}
	}
	h.freeList = append(h.freeList, nid)
}

func (h *HPA) addBorderLink(links []hpaBorderLink, cidA int, cellA Cell, cidB int, cellB Cell, dist float64) []hpaBorderLink {
	costAB := h.stepCost(cellA.Col, cellA.Row, cellB.Col, cellB.Row, dist)
	costBA := h.stepCost(cellB.Col, cellB.Row, cellA.Col, cellA.Row, dist)
	if costAB < 0 && costBA < 0 {
		return links
	}
	nodeA := h.getOrCreateEntrance(cidA, cellA)
	nodeB := h.getOrCreateEntrance(cidB, cellB)
	h.nodes[nodeA].refCount++
	h.nodes[nodeB].refCount++
	if costAB >= 0 {
		h.nodes[nodeA].interEdges = append(h.nodes[nodeA].interEdges, hpaEdge{to: nodeB, cost: costAB})
	}
	if costBA >= 0 {
		h.nodes[nodeB].interEdges = append(h.nodes[nodeB].interEdges, hpaEdge{to: nodeA, cost: costBA})
	}
	return append(links, hpaBorderLink{nodeA: nodeA, nodeB: nodeB})
}

func (h *HPA) borderCrossingMetric(cA, rA, cB, rB int) (float64, bool) {
	if !h.walkable(cA, rA) || !h.walkable(cB, rB) {
		return 0, false
	}
	cAB := h.stepCost(cA, rA, cB, rB, 1)
	cBA := h.stepCost(cB, rB, cA, rA, 1)
	if cAB < 0 && cBA < 0 {
		return 0, false
	}
	if cAB < 0 {
		return cBA, true
	}
	if cBA < 0 {
		return cAB, true
	}
	return 0.5 * (cAB + cBA), true
}

func (h *HPA) rebuildVBorder(idx int) {
	links := h.clearBorderLinks(h.vBorders[idx])
	cx := idx % (h.clusterCols - 1)
	cy := idx / (h.clusterCols - 1)
	cidA := cy*h.clusterCols + cx
	cidB := cidA + 1
	clA := h.clusters[cidA]
	xA := clA.maxCol - 1
	xB := xA + 1

	weighted := h.costGrid != nil || h.edgeCostGrid != nil
	r := clA.minRow
	for r < clA.maxRow {
		if _, ok := h.borderCrossingMetric(xA, r, xB, r); !ok {
			r++
			continue
		}
		segStart := r
		bestR := r
		bestMetric, _ := h.borderCrossingMetric(xA, r, xB, r)
		r++
		for r < clA.maxRow {
			m, ok := h.borderCrossingMetric(xA, r, xB, r)
			if !ok {
				break
			}
			if weighted && m < bestMetric {
				bestMetric = m
				bestR = r
			}
			r++
		}
		segEnd := r - 1
		segLen := segEnd - segStart + 1
		if segLen <= h.maxEntranceSpan {
			pick := segStart + (segLen-1)/2
			if weighted {
				midMetric, _ := h.borderCrossingMetric(xA, pick, xB, pick)
				if bestMetric < midMetric {
					pick = bestR
				}
			}
			links = h.addBorderLink(links, cidA, Cell{Col: xA, Row: pick}, cidB, Cell{Col: xB, Row: pick}, 1)
		} else {
			links = h.addBorderLink(links, cidA, Cell{Col: xA, Row: segStart}, cidB, Cell{Col: xB, Row: segStart}, 1)
			links = h.addBorderLink(links, cidA, Cell{Col: xA, Row: segEnd}, cidB, Cell{Col: xB, Row: segEnd}, 1)
			if weighted && bestR != segStart && bestR != segEnd {
				mStart, _ := h.borderCrossingMetric(xA, segStart, xB, segStart)
				mEnd, _ := h.borderCrossingMetric(xA, segEnd, xB, segEnd)
				if bestMetric < min(mStart, mEnd) {
					links = h.addBorderLink(links, cidA, Cell{Col: xA, Row: bestR}, cidB, Cell{Col: xB, Row: bestR}, 1)
				}
			}
		}
	}

	// When diagonal corner cutting is allowed, add diagonal squeeze links across blocked cardinal pairs.
	if h.diagonal && h.allowCornerCutting {
		for row := clA.minRow; row+1 < clA.maxRow; row++ {
			a0 := h.walkable(xA, row)
			b0 := h.walkable(xB, row)
			a1 := h.walkable(xA, row+1)
			b1 := h.walkable(xB, row+1)
			if a0 && b1 && (!b0 || !a1) {
				links = h.addBorderLink(links, cidA, Cell{Col: xA, Row: row}, cidB, Cell{Col: xB, Row: row + 1}, math.Sqrt2)
			}
			if a1 && b0 && (!a0 || !b1) {
				links = h.addBorderLink(links, cidA, Cell{Col: xA, Row: row + 1}, cidB, Cell{Col: xB, Row: row}, math.Sqrt2)
			}
		}
	}

	h.vBorders[idx] = links
}

func (h *HPA) rebuildHBorder(idx int) {
	links := h.clearBorderLinks(h.hBorders[idx])
	cx := idx % h.clusterCols
	cy := idx / h.clusterCols
	cidA := cy*h.clusterCols + cx
	cidB := cidA + h.clusterCols
	clA := h.clusters[cidA]
	yA := clA.maxRow - 1
	yB := yA + 1

	weighted := h.costGrid != nil || h.edgeCostGrid != nil
	c := clA.minCol
	for c < clA.maxCol {
		if _, ok := h.borderCrossingMetric(c, yA, c, yB); !ok {
			c++
			continue
		}
		segStart := c
		bestC := c
		bestMetric, _ := h.borderCrossingMetric(c, yA, c, yB)
		c++
		for c < clA.maxCol {
			m, ok := h.borderCrossingMetric(c, yA, c, yB)
			if !ok {
				break
			}
			if weighted && m < bestMetric {
				bestMetric = m
				bestC = c
			}
			c++
		}
		segEnd := c - 1
		segLen := segEnd - segStart + 1
		if segLen <= h.maxEntranceSpan {
			pick := segStart + (segLen-1)/2
			if weighted {
				midMetric, _ := h.borderCrossingMetric(pick, yA, pick, yB)
				if bestMetric < midMetric {
					pick = bestC
				}
			}
			links = h.addBorderLink(links, cidA, Cell{Col: pick, Row: yA}, cidB, Cell{Col: pick, Row: yB}, 1)
		} else {
			links = h.addBorderLink(links, cidA, Cell{Col: segStart, Row: yA}, cidB, Cell{Col: segStart, Row: yB}, 1)
			links = h.addBorderLink(links, cidA, Cell{Col: segEnd, Row: yA}, cidB, Cell{Col: segEnd, Row: yB}, 1)
			if weighted && bestC != segStart && bestC != segEnd {
				mStart, _ := h.borderCrossingMetric(segStart, yA, segStart, yB)
				mEnd, _ := h.borderCrossingMetric(segEnd, yA, segEnd, yB)
				if bestMetric < min(mStart, mEnd) {
					links = h.addBorderLink(links, cidA, Cell{Col: bestC, Row: yA}, cidB, Cell{Col: bestC, Row: yB}, 1)
				}
			}
		}
	}

	if h.diagonal && h.allowCornerCutting {
		for col := clA.minCol; col+1 < clA.maxCol; col++ {
			a0 := h.walkable(col, yA)
			b0 := h.walkable(col, yB)
			a1 := h.walkable(col+1, yA)
			b1 := h.walkable(col+1, yB)
			if a0 && b1 && (!b0 || !a1) {
				links = h.addBorderLink(links, cidA, Cell{Col: col, Row: yA}, cidB, Cell{Col: col + 1, Row: yB}, math.Sqrt2)
			}
			if a1 && b0 && (!a0 || !b1) {
				links = h.addBorderLink(links, cidA, Cell{Col: col + 1, Row: yA}, cidB, Cell{Col: col, Row: yB}, math.Sqrt2)
			}
		}
	}

	h.hBorders[idx] = links
}

func (h *HPA) rebuildCorner(idx int) {
	links := h.clearBorderLinks(h.corners[idx])
	cx := idx % (h.clusterCols - 1)
	cy := idx / (h.clusterCols - 1)
	cidTL := cy*h.clusterCols + cx
	cidTR := cidTL + 1
	cidBL := cidTL + h.clusterCols
	cidBR := cidBL + 1

	clTL := h.clusters[cidTL]
	xA := clTL.maxCol - 1
	xB := xA + 1
	yA := clTL.maxRow - 1
	yB := yA + 1

	tl := h.walkable(xA, yA)
	tr := h.walkable(xB, yA)
	bl := h.walkable(xA, yB)
	br := h.walkable(xB, yB)

	if tl && br && (!tr || !bl) {
		links = h.addBorderLink(links, cidTL, Cell{Col: xA, Row: yA}, cidBR, Cell{Col: xB, Row: yB}, math.Sqrt2)
	}
	if tr && bl && (!tl || !br) {
		links = h.addBorderLink(links, cidTR, Cell{Col: xB, Row: yA}, cidBL, Cell{Col: xA, Row: yB}, math.Sqrt2)
	}
	h.corners[idx] = links
}

func (h *HPA) cellID(c Cell) int { return c.Row*h.cols + c.Col }

// rebuildClusterIntra recomputes all intra-cluster shortest paths between entrances of cluster cid.
func (h *HPA) rebuildClusterIntra(cid int) {
	cl := &h.clusters[cid]
	// Sort entrances deterministically by row-major cell ID.
	slices.SortFunc(cl.entrances, func(a, b int) int {
		return h.cellID(h.nodes[a].cell) - h.cellID(h.nodes[b].cell)
	})
	for _, nid := range cl.entrances {
		h.nodes[nid].intraEdges = h.nodes[nid].intraEdges[:0]
		// Keep inter-edges deterministically ordered by destination cell ID.
		slices.SortFunc(h.nodes[nid].interEdges, func(a, b hpaEdge) int {
			return h.cellID(h.nodes[a.to].cell) - h.cellID(h.nodes[b.to].cell)
		})
	}
	n := len(cl.entrances)
	if n < 2 {
		return
	}

	symmetric := h.costGrid == nil && h.edgeCostGrid == nil
	cw := cl.maxCol - cl.minCol

	for i := range n {
		if symmetric && i == n-1 {
			break
		}
		startNode := cl.entrances[i]
		startCell := h.nodes[startNode].cell

		// Mark target entrances in localTargets.
		h.localTgtGen++
		if h.localTgtGen == 0 {
			clear(h.localTargets)
			h.localTgtGen = 1
		}
		tgen := h.localTgtGen
		remaining := 0
		jStart := 0
		if symmetric {
			jStart = i + 1
		}
		for j := jStart; j < n; j++ {
			if j == i {
				continue
			}
			c := h.nodes[cl.entrances[j]].cell
			lid := (c.Row-cl.minRow)*cw + (c.Col - cl.minCol)
			h.localTargets[lid] = tgen
			remaining++
		}

		h.runLocalDijkstra(cid, startCell, false, tgen, remaining)

		for j := jStart; j < n; j++ {
			if j == i {
				continue
			}
			dstNode := cl.entrances[j]
			dc := h.nodes[dstNode].cell
			lid := (dc.Row-cl.minRow)*cw + (dc.Col - cl.minCol)
			if h.localSeen[lid] == h.localGen {
				cost := h.localG[lid]
				h.nodes[startNode].intraEdges = append(h.nodes[startNode].intraEdges, hpaEdge{to: dstNode, cost: cost})
				if symmetric {
					h.nodes[dstNode].intraEdges = append(h.nodes[dstNode].intraEdges, hpaEdge{to: startNode, cost: cost})
				}
			}
		}
	}
}

// --- Cluster-local Dijkstra and A* ---

type hpaStep struct {
	dc, dr int
	dist   float64
	diag   bool
}

var hpaSteps = [8]hpaStep{
	{0, -1, 1, false},
	{1, 0, 1, false},
	{0, 1, 1, false},
	{-1, 0, 1, false},
	{1, -1, math.Sqrt2, true},
	{1, 1, math.Sqrt2, true},
	{-1, 1, math.Sqrt2, true},
	{-1, -1, math.Sqrt2, true},
}

func (h *HPA) nextLocalGen() uint32 {
	h.localGen++
	if h.localGen == 0 {
		clear(h.localSeen)
		clear(h.localClosed)
		h.localGen = 1
	}
	h.localOpen = h.localOpen[:0]
	return h.localGen
}

// runLocalDijkstra runs Dijkstra inside cluster cid from origin.
// When reverse is true, edges are traversed in reverse so localG[u] holds the forward shortest-path cost from u to origin.
// Stops early once remaining target cells (marked with tgen in localTargets) are settled.
func (h *HPA) runLocalDijkstra(cid int, origin Cell, reverse bool, tgen uint32, remaining int) {
	if remaining <= 0 {
		return
	}
	cl := h.clusters[cid]
	cw := cl.maxCol - cl.minCol
	ch := cl.maxRow - cl.minRow
	gen := h.nextLocalGen()

	startLID := (origin.Row-cl.minRow)*cw + (origin.Col - cl.minCol)
	h.localSeen[startLID] = gen
	h.localG[startLID] = 0
	h.localParent[startLID] = -1
	h.localPush(astarNode{id: startLID, f: 0, h: 0})

	stepCount := 4
	if h.diagonal {
		stepCount = 8
	}
	noCut := !h.allowCornerCutting

	for len(h.localOpen) > 0 {
		cur := h.localPop()
		h.localClosed[cur.id] = gen
		if h.localTargets[cur.id] == tgen {
			remaining--
			if remaining == 0 {
				return
			}
		}

		lx := cur.id % cw
		ly := cur.id / cw
		c := cl.minCol + lx
		r := cl.minRow + ly
		curG := h.localG[cur.id]

		for i := range stepCount {
			st := hpaSteps[i]
			nlx := lx + st.dc
			nly := ly + st.dr
			if nlx < 0 || nlx >= cw || nly < 0 || nly >= ch {
				continue
			}
			nlid := nly*cw + nlx
			if h.localClosed[nlid] == gen {
				continue
			}
			nc := cl.minCol + nlx
			nr := cl.minRow + nly
			if !h.walkable(nc, nr) {
				continue
			}
			if st.diag && noCut {
				if !h.walkable(c+st.dc, r) || !h.walkable(c, r+st.dr) {
					continue
				}
			}
			var sc float64
			if reverse {
				sc = h.stepCost(nc, nr, c, r, st.dist)
			} else {
				sc = h.stepCost(c, r, nc, nr, st.dist)
			}
			if sc < 0 {
				continue
			}
			ng := curG + sc
			if h.localSeen[nlid] != gen {
				h.localSeen[nlid] = gen
				h.localG[nlid] = ng
				h.localParent[nlid] = cur.id
				h.localPush(astarNode{id: nlid, f: ng, h: 0})
			} else if ng < h.localG[nlid] {
				h.localG[nlid] = ng
				h.localParent[nlid] = cur.id
				if pos := h.localHeapPos[nlid]; pos >= 0 {
					h.localOpen[pos].f = ng
					h.localSiftUp(pos)
				} else {
					h.localPush(astarNode{id: nlid, f: ng, h: 0})
				}
			}
		}
	}
}

// refineIntraCluster runs cluster-local A* from start to goal inside cluster cid and appends the path cells after start into out.
func (h *HPA) refineIntraCluster(out []Cell, cid int, start, goal Cell) ([]Cell, float64, bool) {
	if start == goal {
		return out, 0, true
	}
	cl := h.clusters[cid]
	cw := cl.maxCol - cl.minCol
	ch := cl.maxRow - cl.minRow
	gen := h.nextLocalGen()

	startLID := (start.Row-cl.minRow)*cw + (start.Col - cl.minCol)
	goalLID := (goal.Row-cl.minRow)*cw + (goal.Col - cl.minCol)

	h.localSeen[startLID] = gen
	h.localG[startLID] = 0
	h.localParent[startLID] = -1
	h0 := h.heuristic(start.Col, start.Row, goal.Col, goal.Row)
	h.localPush(astarNode{id: startLID, f: h0, h: h0})

	stepCount := 4
	if h.diagonal {
		stepCount = 8
	}
	noCut := !h.allowCornerCutting

	for len(h.localOpen) > 0 {
		cur := h.localPop()
		if cur.id == goalLID {
			rev := h.localRev[:0]
			for id := goalLID; id != startLID; id = h.localParent[id] {
				rev = append(rev, Cell{Col: cl.minCol + id%cw, Row: cl.minRow + id/cw})
			}
			for i := len(rev) - 1; i >= 0; i-- {
				out = append(out, rev[i])
			}
			h.localRev = rev
			return out, h.localG[goalLID], true
		}
		h.localClosed[cur.id] = gen

		lx := cur.id % cw
		ly := cur.id / cw
		c := cl.minCol + lx
		r := cl.minRow + ly
		curG := h.localG[cur.id]

		for i := range stepCount {
			st := hpaSteps[i]
			nlx := lx + st.dc
			nly := ly + st.dr
			if nlx < 0 || nlx >= cw || nly < 0 || nly >= ch {
				continue
			}
			nlid := nly*cw + nlx
			if h.localClosed[nlid] == gen {
				continue
			}
			nc := cl.minCol + nlx
			nr := cl.minRow + nly
			if !h.walkable(nc, nr) {
				continue
			}
			if st.diag && noCut {
				if !h.walkable(c+st.dc, r) || !h.walkable(c, r+st.dr) {
					continue
				}
			}
			sc := h.stepCost(c, r, nc, nr, st.dist)
			if sc < 0 {
				continue
			}
			ng := curG + sc
			if h.localSeen[nlid] != gen {
				h.localSeen[nlid] = gen
				h.localG[nlid] = ng
				h.localParent[nlid] = cur.id
				hv := h.heuristic(nc, nr, goal.Col, goal.Row)
				h.localPush(astarNode{id: nlid, f: ng + hv, h: hv})
			} else if ng < h.localG[nlid] {
				h.localG[nlid] = ng
				h.localParent[nlid] = cur.id
				hv := h.heuristic(nc, nr, goal.Col, goal.Row)
				if pos := h.localHeapPos[nlid]; pos >= 0 {
					h.localOpen[pos].f = ng + hv
					h.localOpen[pos].h = hv
					h.localSiftUp(pos)
				} else {
					h.localPush(astarNode{id: nlid, f: ng + hv, h: hv})
				}
			}
		}
	}
	return out, 0, false
}

// --- Abstract graph A* ---

func (h *HPA) prepareAbs(n int) uint32 {
	if cap(h.absSeen) < n {
		h.absSeen = make([]uint32, n)
		h.absClosed = make([]uint32, n)
		h.absG = make([]float64, n)
		h.absParent = make([]int, n)
		h.absHeapPos = make([]int, n)
		h.goalEdgeGen = make([]uint32, n)
		h.goalEdgeCost = make([]float64, n)
		h.absGen = 0
	} else {
		h.absSeen = h.absSeen[:n]
		h.absClosed = h.absClosed[:n]
		h.absG = h.absG[:n]
		h.absParent = h.absParent[:n]
		h.absHeapPos = h.absHeapPos[:n]
		h.goalEdgeGen = h.goalEdgeGen[:n]
		h.goalEdgeCost = h.goalEdgeCost[:n]
	}
	h.absGen++
	if h.absGen == 0 {
		clear(h.absSeen)
		clear(h.absClosed)
		clear(h.goalEdgeGen)
		h.absGen = 1
	}
	h.absOpen = h.absOpen[:0]
	return h.absGen
}

func (h *HPA) nodeCell(id int, start, goal Cell) Cell {
	n := len(h.nodes)
	if id == n {
		return start
	}
	if id == n+1 {
		return goal
	}
	return h.nodes[id].cell
}

func (h *HPA) nodeCluster(id int, start, goal Cell) int {
	n := len(h.nodes)
	if id == n {
		return (start.Row/h.clusterSize)*h.clusterCols + (start.Col / h.clusterSize)
	}
	if id == n+1 {
		return (goal.Row/h.clusterSize)*h.clusterCols + (goal.Col / h.clusterSize)
	}
	return h.nodes[id].cluster
}

func (h *HPA) searchAbstract(start, goal Cell) (float64, bool) {
	n := len(h.nodes)
	startVirt := n
	goalVirt := n + 1
	gen := h.prepareAbs(n + 2)

	startCID := (start.Row/h.clusterSize)*h.clusterCols + (start.Col / h.clusterSize)
	goalCID := (goal.Row/h.clusterSize)*h.clusterCols + (goal.Col / h.clusterSize)

	// 1. Connect startVirt to entrances in startCID (and directly to goalVirt if in same cluster).
	h.startEdges = h.startEdges[:0]
	startCl := h.clusters[startCID]
	startCW := startCl.maxCol - startCl.minCol

	h.localTgtGen++
	if h.localTgtGen == 0 {
		clear(h.localTargets)
		h.localTgtGen = 1
	}
	tgen := h.localTgtGen
	remaining := 0
	for _, eid := range startCl.entrances {
		ec := h.nodes[eid].cell
		lid := (ec.Row-startCl.minRow)*startCW + (ec.Col - startCl.minCol)
		if h.localTargets[lid] != tgen {
			h.localTargets[lid] = tgen
			remaining++
		}
	}
	if startCID == goalCID {
		glid := (goal.Row-startCl.minRow)*startCW + (goal.Col - startCl.minCol)
		if h.localTargets[glid] != tgen {
			h.localTargets[glid] = tgen
			remaining++
		}
	}
	if remaining > 0 {
		h.runLocalDijkstra(startCID, start, false, tgen, remaining)
		for _, eid := range startCl.entrances {
			ec := h.nodes[eid].cell
			lid := (ec.Row-startCl.minRow)*startCW + (ec.Col - startCl.minCol)
			if h.localSeen[lid] == h.localGen {
				h.startEdges = append(h.startEdges, hpaEdge{to: eid, cost: h.localG[lid]})
			}
		}
		if startCID == goalCID {
			glid := (goal.Row-startCl.minRow)*startCW + (goal.Col - startCl.minCol)
			if h.localSeen[glid] == h.localGen {
				h.startEdges = append(h.startEdges, hpaEdge{to: goalVirt, cost: h.localG[glid]})
			}
		}
	}
	if len(h.startEdges) == 0 {
		return 0, false
	}

	// 2. Connect entrances in goalCID to goalVirt via a single reverse Dijkstra from goal.
	goalCl := h.clusters[goalCID]
	goalCW := goalCl.maxCol - goalCl.minCol
	hasGoalEntrance := false
	if len(goalCl.entrances) > 0 {
		h.localTgtGen++
		if h.localTgtGen == 0 {
			clear(h.localTargets)
			h.localTgtGen = 1
		}
		gtgen := h.localTgtGen
		grem := 0
		for _, eid := range goalCl.entrances {
			ec := h.nodes[eid].cell
			lid := (ec.Row-goalCl.minRow)*goalCW + (ec.Col - goalCl.minCol)
			if h.localTargets[lid] != gtgen {
				h.localTargets[lid] = gtgen
				grem++
			}
		}
		h.runLocalDijkstra(goalCID, goal, true, gtgen, grem)
		for _, eid := range goalCl.entrances {
			ec := h.nodes[eid].cell
			lid := (ec.Row-goalCl.minRow)*goalCW + (ec.Col - goalCl.minCol)
			if h.localSeen[lid] == h.localGen {
				h.goalEdgeGen[eid] = gen
				h.goalEdgeCost[eid] = h.localG[lid]
				hasGoalEntrance = true
			}
		}
	}
	// If goal is in a different cluster and no entrance can reach goal, goal is unreachable.
	if startCID != goalCID && !hasGoalEntrance {
		return 0, false
	}

	// 3. Run abstract A* from startVirt to goalVirt.
	h.absSeen[startVirt] = gen
	h.absG[startVirt] = 0
	h.absParent[startVirt] = -1
	h0 := h.heuristic(start.Col, start.Row, goal.Col, goal.Row)
	h.absPush(hpaOpenNode{id: startVirt, cellID: h.cellID(start), f: h0, h: h0})

	relax := func(fromID int, curG float64, toID int, edgeCost float64) {
		if h.absClosed[toID] == gen {
			return
		}
		ng := curG + edgeCost
		if h.absSeen[toID] != gen {
			h.absSeen[toID] = gen
			h.absG[toID] = ng
			h.absParent[toID] = fromID
			tc := h.nodeCell(toID, start, goal)
			hv := h.heuristic(tc.Col, tc.Row, goal.Col, goal.Row)
			h.absPush(hpaOpenNode{id: toID, cellID: h.cellID(tc), f: ng + hv, h: hv})
		} else if ng < h.absG[toID] {
			h.absG[toID] = ng
			h.absParent[toID] = fromID
			tc := h.nodeCell(toID, start, goal)
			hv := h.heuristic(tc.Col, tc.Row, goal.Col, goal.Row)
			if pos := h.absHeapPos[toID]; pos >= 0 {
				h.absOpen[pos].f = ng + hv
				h.absOpen[pos].h = hv
				h.absSiftUp(pos)
			} else {
				h.absPush(hpaOpenNode{id: toID, cellID: h.cellID(tc), f: ng + hv, h: hv})
			}
		}
	}

	for len(h.absOpen) > 0 {
		cur := h.absPop()
		if cur.id == goalVirt {
			path := h.absPath[:0]
			for id := goalVirt; id != -1; id = h.absParent[id] {
				path = append(path, id)
			}
			slices.Reverse(path)
			h.absPath = path
			return h.absG[goalVirt], true
		}
		h.absClosed[cur.id] = gen
		curG := h.absG[cur.id]

		if cur.id == startVirt {
			for _, e := range h.startEdges {
				relax(cur.id, curG, e.to, e.cost)
			}
			continue
		}

		node := &h.nodes[cur.id]
		for _, e := range node.interEdges {
			relax(cur.id, curG, e.to, e.cost)
		}
		for _, e := range node.intraEdges {
			relax(cur.id, curG, e.to, e.cost)
		}
		if h.goalEdgeGen[cur.id] == gen {
			relax(cur.id, curG, goalVirt, h.goalEdgeCost[cur.id])
		}
	}
	return 0, false
}

// --- Heaps ---

func (h *HPA) localPush(node astarNode) {
	idx := len(h.localOpen)
	h.localOpen = append(h.localOpen, node)
	h.localHeapPos[node.id] = idx
	h.localSiftUp(idx)
}

func (h *HPA) localPop() astarNode {
	top := h.localOpen[0]
	h.localHeapPos[top.id] = -1
	last := len(h.localOpen) - 1
	if last == 0 {
		h.localOpen = h.localOpen[:0]
		return top
	}
	h.localOpen[0] = h.localOpen[last]
	h.localHeapPos[h.localOpen[0].id] = 0
	h.localOpen = h.localOpen[:last]
	h.localSiftDown(0)
	return top
}

func (h *HPA) localSiftUp(i int) {
	for i > 0 {
		p := (i - 1) / 2
		if !astarLess(h.localOpen[i], h.localOpen[p]) {
			break
		}
		h.localOpen[i], h.localOpen[p] = h.localOpen[p], h.localOpen[i]
		h.localHeapPos[h.localOpen[i].id] = i
		h.localHeapPos[h.localOpen[p].id] = p
		i = p
	}
}

func (h *HPA) localSiftDown(i int) {
	n := len(h.localOpen)
	for {
		l := 2*i + 1
		if l >= n {
			break
		}
		best := l
		if r := l + 1; r < n && astarLess(h.localOpen[r], h.localOpen[l]) {
			best = r
		}
		if !astarLess(h.localOpen[best], h.localOpen[i]) {
			break
		}
		h.localOpen[i], h.localOpen[best] = h.localOpen[best], h.localOpen[i]
		h.localHeapPos[h.localOpen[i].id] = i
		h.localHeapPos[h.localOpen[best].id] = best
		i = best
	}
}

// hpaOpenLess defines a strict deterministic order independent of freelist node IDs:
// lowest f, then lowest h, then lowest row-major cellID, then lowest id.
func hpaOpenLess(x, y hpaOpenNode) bool {
	if x.f != y.f {
		return x.f < y.f
	}
	if x.h != y.h {
		return x.h < y.h
	}
	if x.cellID != y.cellID {
		return x.cellID < y.cellID
	}
	return x.id < y.id
}

func (h *HPA) absPush(node hpaOpenNode) {
	idx := len(h.absOpen)
	h.absOpen = append(h.absOpen, node)
	h.absHeapPos[node.id] = idx
	h.absSiftUp(idx)
}

func (h *HPA) absPop() hpaOpenNode {
	top := h.absOpen[0]
	h.absHeapPos[top.id] = -1
	last := len(h.absOpen) - 1
	if last == 0 {
		h.absOpen = h.absOpen[:0]
		return top
	}
	h.absOpen[0] = h.absOpen[last]
	h.absHeapPos[h.absOpen[0].id] = 0
	h.absOpen = h.absOpen[:last]
	h.absSiftDown(0)
	return top
}

func (h *HPA) absSiftUp(i int) {
	for i > 0 {
		p := (i - 1) / 2
		if !hpaOpenLess(h.absOpen[i], h.absOpen[p]) {
			break
		}
		h.absOpen[i], h.absOpen[p] = h.absOpen[p], h.absOpen[i]
		h.absHeapPos[h.absOpen[i].id] = i
		h.absHeapPos[h.absOpen[p].id] = p
		i = p
	}
}

func (h *HPA) absSiftDown(i int) {
	n := len(h.absOpen)
	for {
		l := 2*i + 1
		if l >= n {
			break
		}
		best := l
		if r := l + 1; r < n && hpaOpenLess(h.absOpen[r], h.absOpen[l]) {
			best = r
		}
		if !hpaOpenLess(h.absOpen[best], h.absOpen[i]) {
			break
		}
		h.absOpen[i], h.absOpen[best] = h.absOpen[best], h.absOpen[i]
		h.absHeapPos[h.absOpen[i].id] = i
		h.absHeapPos[h.absOpen[best].id] = best
		i = best
	}
}
