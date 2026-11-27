package ai

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// WaypointEdge is a directed outgoing edge in a WaypointGraph.
type WaypointEdge struct {
	To   int
	Cost float64
}

type waypointEdgeEntry struct {
	to       int
	cost     float64
	autoCost bool
}

type waypointNode struct {
	pos     Vec3
	active  bool
	enabled bool
	edges   []waypointEdgeEntry
}

// WaypointGraph is a weighted graph of hand-placed 2D or 3D waypoints implementing
// HeuristicGraph[int], ValidatedGraph[int], and NodeOrder[int].
//
// Each added waypoint receives a stable integer ID (0, 1, 2, ...). Removing a waypoint
// invalidates its ID without renumbering remaining waypoints. Edges may be directed or
// undirected, with either automatic Euclidean-distance costs (Connect / ConnectUndirected)
// or explicit non-negative weights (AddEdge / AddUndirectedEdge).
//
// Heuristic returns the Euclidean distance between two waypoints, which is admissible when
// every edge cost is at least Euclidean distance; use GraphSearch.FindDijkstra or
// FindWithHeuristic when graphs contain shortcut or portal edges cheaper than Euclidean distance.
type WaypointGraph struct {
	nodes []waypointNode
	count int
}

// NewWaypointGraph returns an empty WaypointGraph.
func NewWaypointGraph() *WaypointGraph {
	return &WaypointGraph{}
}

// Add adds a 2D waypoint at pos (with Z = 0) and returns its stable node ID.
func (g *WaypointGraph) Add(pos core.Point) int {
	return g.Add3D(Vec3{X: float32(pos.X), Y: float32(pos.Y)})
}

// Add3D adds a 3D waypoint at pos and returns its stable node ID.
func (g *WaypointGraph) Add3D(pos Vec3) int {
	id := len(g.nodes)
	g.nodes = append(g.nodes, waypointNode{
		pos:     pos,
		active:  true,
		enabled: true,
	})
	g.count++
	return id
}

// Remove deletes waypoint id and all edges incident to it while keeping all other node IDs stable.
// It returns false if id is invalid or already removed.
func (g *WaypointGraph) Remove(id int) bool {
	if !g.isActive(id) {
		return false
	}
	g.nodes[id] = waypointNode{}
	g.count--
	for i := range g.nodes {
		if !g.nodes[i].active {
			continue
		}
		g.removeEdgeFrom(i, id)
	}
	return true
}

// SetEnabled enables or disables an active waypoint without removing its edges.
// Disabled waypoints are skipped during searches and Nearest queries.
func (g *WaypointGraph) SetEnabled(id int, enabled bool) {
	if !g.isActive(id) {
		return
	}
	g.nodes[id].enabled = enabled
}

// Enabled reports whether id is an active, enabled waypoint.
func (g *WaypointGraph) Enabled(id int) bool {
	return g.ValidNode(id)
}

// SetPos updates the 2D position of waypoint id (setting Z = 0).
func (g *WaypointGraph) SetPos(id int, pos core.Point) {
	g.SetPos3D(id, Vec3{X: float32(pos.X), Y: float32(pos.Y)})
}

// SetPos3D updates the 3D position of waypoint id.
func (g *WaypointGraph) SetPos3D(id int, pos Vec3) {
	if !g.isActive(id) {
		return
	}
	g.nodes[id].pos = pos
}

// Pos returns the 2D position of active waypoint id, or (zero, false) if id is invalid or removed.
func (g *WaypointGraph) Pos(id int) (core.Point, bool) {
	if !g.isActive(id) {
		return core.Point{}, false
	}
	p := g.nodes[id].pos
	return core.Point{X: core.Points(p.X), Y: core.Points(p.Y)}, true
}

// Pos3D returns the 3D position of active waypoint id, or (zero, false) if id is invalid or removed.
func (g *WaypointGraph) Pos3D(id int) (Vec3, bool) {
	if !g.isActive(id) {
		return Vec3{}, false
	}
	return g.nodes[id].pos, true
}

// NodeCount returns the number of active (non-removed) waypoints in g.
func (g *WaypointGraph) NodeCount() int {
	if g == nil {
		return 0
	}
	return g.count
}

// Connect adds or updates a directed edge from -> to whose cost is the live Euclidean distance
// between the two waypoints. Returns false if either endpoint is invalid or removed.
func (g *WaypointGraph) Connect(from, to int) bool {
	if !g.isActive(from) || !g.isActive(to) {
		return false
	}
	g.upsertEdge(from, waypointEdgeEntry{to: to, autoCost: true})
	return true
}

// ConnectUndirected adds or updates bidirectional edges between a and b whose cost is the live
// Euclidean distance between them. Returns false if either endpoint is invalid or removed.
func (g *WaypointGraph) ConnectUndirected(a, b int) bool {
	if !g.isActive(a) || !g.isActive(b) {
		return false
	}
	g.upsertEdge(a, waypointEdgeEntry{to: b, autoCost: true})
	if a != b {
		g.upsertEdge(b, waypointEdgeEntry{to: a, autoCost: true})
	}
	return true
}

// AddEdge adds or updates a directed edge from -> to with explicit non-negative finite cost.
// Returns false if either endpoint is invalid/removed or cost is negative, NaN, or infinite.
func (g *WaypointGraph) AddEdge(from, to int, cost float64) bool {
	if !g.isActive(from) || !g.isActive(to) || cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0) {
		return false
	}
	g.upsertEdge(from, waypointEdgeEntry{to: to, cost: cost})
	return true
}

// AddUndirectedEdge adds or updates bidirectional edges between a and b with explicit non-negative
// finite cost. Returns false if either endpoint is invalid/removed or cost is negative, NaN, or infinite.
func (g *WaypointGraph) AddUndirectedEdge(a, b int, cost float64) bool {
	if !g.isActive(a) || !g.isActive(b) || cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0) {
		return false
	}
	g.upsertEdge(a, waypointEdgeEntry{to: b, cost: cost})
	if a != b {
		g.upsertEdge(b, waypointEdgeEntry{to: a, cost: cost})
	}
	return true
}

// RemoveEdge removes the directed edge from -> to, returning true if it existed.
func (g *WaypointGraph) RemoveEdge(from, to int) bool {
	if !g.isActive(from) {
		return false
	}
	return g.removeEdgeFrom(from, to)
}

// RemoveUndirectedEdge removes both a -> b and b -> a, returning true if either existed.
func (g *WaypointGraph) RemoveUndirectedEdge(a, b int) bool {
	removedA := g.RemoveEdge(a, b)
	removedB := false
	if a != b {
		removedB = g.RemoveEdge(b, a)
	}
	return removedA || removedB
}

// Edges returns a copy of waypoint id's outgoing edges ordered by ascending destination ID.
func (g *WaypointGraph) Edges(id int) []WaypointEdge {
	if !g.isActive(id) {
		return nil
	}
	src := g.nodes[id].edges
	if len(src) == 0 {
		return nil
	}
	out := make([]WaypointEdge, 0, len(src))
	for _, e := range src {
		if !g.isActive(e.to) {
			continue
		}
		out = append(out, WaypointEdge{
			To:   e.to,
			Cost: g.edgeCost(id, e),
		})
	}
	return out
}

// Nearest returns the ID of the enabled waypoint closest to 2D point pos (in XY),
// breaking distance ties deterministically by lowest waypoint ID.
func (g *WaypointGraph) Nearest(pos core.Point) (int, bool) {
	return g.Nearest3D(Vec3{X: float32(pos.X), Y: float32(pos.Y)})
}

// Nearest3D returns the ID of the enabled waypoint closest to 3D point pos,
// breaking distance ties deterministically by lowest waypoint ID.
func (g *WaypointGraph) Nearest3D(pos Vec3) (int, bool) {
	if g == nil {
		return -1, false
	}
	bestID := -1
	bestDistSq := math.Inf(1)
	px, py, pz := float64(pos.X), float64(pos.Y), float64(pos.Z)
	for i := range g.nodes {
		nd := &g.nodes[i]
		if !nd.active || !nd.enabled {
			continue
		}
		dx := float64(nd.pos.X) - px
		dy := float64(nd.pos.Y) - py
		dz := float64(nd.pos.Z) - pz
		d2 := dx*dx + dy*dy + dz*dz
		if d2 < bestDistSq {
			bestDistSq = d2
			bestID = i
		}
	}
	if bestID < 0 {
		return -1, false
	}
	return bestID, true
}

// Points converts a slice of waypoint IDs into their 2D positions.
func (g *WaypointGraph) Points(ids []int) []core.Point {
	return g.PointsInto(nil, ids)
}

// PointsInto writes the 2D positions of ids into dst[:0], avoiding allocations when dst has enough capacity.
func (g *WaypointGraph) PointsInto(dst []core.Point, ids []int) []core.Point {
	out := dst[:0]
	if g == nil {
		return out
	}
	for _, id := range ids {
		if p, ok := g.Pos(id); ok {
			out = append(out, p)
		}
	}
	return out
}

// Points3D converts a slice of waypoint IDs into their 3D positions.
func (g *WaypointGraph) Points3D(ids []int) []Vec3 {
	return g.Points3DInto(nil, ids)
}

// Points3DInto writes the 3D positions of ids into dst[:0], avoiding allocations when dst has enough capacity.
func (g *WaypointGraph) Points3DInto(dst []Vec3, ids []int) []Vec3 {
	out := dst[:0]
	if g == nil {
		return out
	}
	for _, id := range ids {
		if p, ok := g.Pos3D(id); ok {
			out = append(out, p)
		}
	}
	return out
}

// ValidNode reports whether id is an active, enabled waypoint in g.
func (g *WaypointGraph) ValidNode(id int) bool {
	if !g.isActive(id) {
		return false
	}
	return g.nodes[id].enabled
}

// AppendNeighbors appends all outgoing edges from id to enabled destination waypoints onto dst
// in deterministic ascending destination ID order.
func (g *WaypointGraph) AppendNeighbors(dst []GraphEdge[int], id int) []GraphEdge[int] {
	if !g.ValidNode(id) {
		return dst
	}
	edges := g.nodes[id].edges
	for i := range edges {
		e := edges[i]
		if !g.ValidNode(e.to) {
			continue
		}
		dst = append(dst, GraphEdge[int]{
			To:   e.to,
			Cost: g.edgeCost(id, e),
		})
	}
	return dst
}

// Heuristic returns the Euclidean distance between waypoints from and goal.
func (g *WaypointGraph) Heuristic(from, goal int) float64 {
	if !g.isActive(from) || !g.isActive(goal) {
		return 0
	}
	return waypointDist(g.nodes[from].pos, g.nodes[goal].pos)
}

// NodeLess breaks search ties deterministically by ascending waypoint ID.
func (g *WaypointGraph) NodeLess(a, b int) bool {
	return a < b
}

func (g *WaypointGraph) isActive(id int) bool {
	return g != nil && id >= 0 && id < len(g.nodes) && g.nodes[id].active
}

func (g *WaypointGraph) edgeCost(from int, e waypointEdgeEntry) float64 {
	if e.autoCost {
		return waypointDist(g.nodes[from].pos, g.nodes[e.to].pos)
	}
	return e.cost
}

func (g *WaypointGraph) upsertEdge(from int, entry waypointEdgeEntry) {
	edges := g.nodes[from].edges
	// Keep edges sorted by ascending destination ID for deterministic traversal.
	pos := 0
	for pos < len(edges) && edges[pos].to < entry.to {
		pos++
	}
	if pos < len(edges) && edges[pos].to == entry.to {
		edges[pos] = entry
		return
	}
	edges = append(edges, waypointEdgeEntry{})
	copy(edges[pos+1:], edges[pos:])
	edges[pos] = entry
	g.nodes[from].edges = edges
}

func (g *WaypointGraph) removeEdgeFrom(from, to int) bool {
	edges := g.nodes[from].edges
	for i := range edges {
		if edges[i].to == to {
			copy(edges[i:], edges[i+1:])
			edges[len(edges)-1] = waypointEdgeEntry{}
			g.nodes[from].edges = edges[:len(edges)-1]
			return true
		}
		if edges[i].to > to {
			break
		}
	}
	return false
}

func waypointDist(a, b Vec3) float64 {
	dx := float64(b.X) - float64(a.X)
	dy := float64(b.Y) - float64(a.Y)
	dz := float64(b.Z) - float64(a.Z)
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

var (
	_ HeuristicGraph[int] = (*WaypointGraph)(nil)
	_ ValidatedGraph[int] = (*WaypointGraph)(nil)
	_ NodeOrder[int]      = (*WaypointGraph)(nil)
)
