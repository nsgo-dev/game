package ai

import (
	"math"
	"slices"

	"github.com/nsgo-dev/nsgo/core"
)

const (
	navEps    = 1e-5
	portalEps = 1e-4
)

type vec2 struct {
	x, y float64
}

func toVec2(p core.Point) vec2 { return vec2{float64(p.X), float64(p.Y)} }

func toPoint(v vec2) core.Point { return core.Point{X: core.Points(v.x), Y: core.Points(v.y)} }

func (a vec2) add(b vec2) vec2      { return vec2{a.x + b.x, a.y + b.y} }
func (a vec2) sub(b vec2) vec2      { return vec2{a.x - b.x, a.y - b.y} }
func (a vec2) scale(s float64) vec2 { return vec2{a.x * s, a.y * s} }
func (a vec2) dot(b vec2) float64   { return float64(a.x*b.x) + float64(a.y*b.y) }
func (a vec2) cross(b vec2) float64 { return float64(a.x*b.y) - float64(a.y*b.x) }
func (a vec2) lenSq() float64       { return float64(a.x*a.x) + float64(a.y*a.y) }
func (a vec2) len() float64         { return math.Hypot(a.x, a.y) }

func dist(a, b vec2) float64 { return math.Hypot(a.x-b.x, a.y-b.y) }
func distSq(a, b vec2) float64 {
	dx, dy := a.x-b.x, a.y-b.y
	return float64(dx*dx) + float64(dy*dy)
}
func vequal(a, b vec2) bool { return distSq(a, b) <= navEps*navEps }

// triArea2 returns twice the signed area of triangle (a, b, c): positive when a->b->c turns counter-clockwise.
func triArea2(a, b, c vec2) float64 {
	return float64((b.x-a.x)*(c.y-a.y)) - float64((b.y-a.y)*(c.x-a.x))
}

func closestPointOnSegment(p, a, b vec2) vec2 {
	ab := b.sub(a)
	l2 := ab.lenSq()
	if l2 <= 1e-18 {
		return a
	}
	t := p.sub(a).dot(ab) / l2
	t = min(max(t, 0), 1)
	return a.add(ab.scale(t))
}

type navPoly struct {
	verts                  []vec2
	minX, minY, maxX, maxY float64
	cx, cy                 float64
	surfArea               float64
	outPortals             []int
	area                   NavArea
	flags                  NavFlags
}

// NavLinkOffMesh is a link flag marking a portal as an off-mesh connection
// (such as a jump, drop-down, ladder, or teleport) rather than a shared polygon edge.
const NavLinkOffMesh uint32 = 1 << 0

// NavOffMeshLink describes a custom off-mesh connection between two points on a 2D NavMesh.
type NavOffMeshLink struct {
	// Start and End are the world-space endpoints on the mesh.
	Start core.Point
	End   core.Point
	// Bidirectional adds portals in both Start->End and End->Start directions when true.
	Bidirectional bool
	// Flags stores game-defined link flags; NavLinkOffMesh is always set automatically.
	Flags uint32
	// ID is an optional game-defined link identifier delivered in NavLinkEvent.LinkID.
	ID int
	// Cost overrides the A* traversal cost when positive; <= 0 uses Euclidean distance.
	Cost float32
}

// navPortal is a directed crossing from fromPoly to toPoly.
// For shared-edge portals, looking from inside fromPoly across the portal into toPoly,
// left is on the counter-clockwise side and right is on the clockwise side so triArea2(apex, right, left) >= 0.
// For off-mesh links, left and right both hold the entry point and endPos holds the exit point.
type navPortal struct {
	fromPoly int
	toPoly   int
	left     vec2
	right    vec2
	endPos   vec2
	cost     float64
	flags    uint32
	linkID   int
	offMesh  bool
}

// NavMesh is a 2D navigation mesh built from convex polygons connected across shared edges.
type NavMesh struct {
	polys        []navPoly
	portals      []navPortal
	bvhNodes     []navBVHNode2D
	bvhOrder     []int
	revision     uint64
	defaultQuery NavMeshQuery
}

// BuildNavMesh constructs a NavMesh from convex 2D polygons, normalizing winding order and automatically connecting adjacent polygons along shared edges (including partial collinear overlaps).
//
// Each polygon is assigned NavAreaDefault and NavFlagWalk. Degenerate polygons with fewer than three vertices or near-zero area are skipped.
func BuildNavMesh(polys [][]core.Point) *NavMesh {
	inputs := make([]NavPolygonInput, len(polys))
	for i, raw := range polys {
		inputs[i] = NavPolygonInput{
			Vertices: raw,
			Area:     NavAreaDefault,
			Flags:    NavFlagWalk,
		}
	}
	return BuildNavMeshWithAreas(inputs)
}

// BuildNavMeshWithAreas constructs a NavMesh from convex 2D polygons carrying per-polygon area IDs and runtime traversal flags, normalizing winding order and automatically connecting adjacent polygons along shared edges (including partial collinear overlaps).
//
// Polygons with zero Flags default to NavFlagWalk. Degenerate polygons with fewer than three vertices or near-zero area are skipped.
func BuildNavMeshWithAreas(polys []NavPolygonInput) *NavMesh {
	m := &NavMesh{revision: 1}
	for _, in := range polys {
		raw := in.Vertices
		if len(raw) < 3 {
			continue
		}
		verts := make([]vec2, len(raw))
		for i, p := range raw {
			verts[i] = toVec2(p)
		}
		area2 := polygonArea2(verts)
		if math.Abs(area2) <= 1e-9 {
			continue
		}
		// Normalize all polygons to positive signed area (CCW in standard (x, y) coordinates).
		if area2 < 0 {
			slices.Reverse(verts)
		}
		minX, minY := verts[0].x, verts[0].y
		maxX, maxY := verts[0].x, verts[0].y
		sumX, sumY := verts[0].x, verts[0].y
		for _, v := range verts[1:] {
			minX = min(minX, v.x)
			minY = min(minY, v.y)
			maxX = max(maxX, v.x)
			maxY = max(maxY, v.y)
			sumX += v.x
			sumY += v.y
		}
		invN := 1.0 / float64(len(verts))
		flags := in.Flags
		if flags == 0 {
			flags = NavFlagWalk
		}
		m.polys = append(m.polys, navPoly{
			verts:    verts,
			minX:     minX,
			minY:     minY,
			maxX:     maxX,
			maxY:     maxY,
			cx:       sumX * invN,
			cy:       sumY * invN,
			surfArea: 0.5 * math.Abs(area2),
			area:     in.Area,
			flags:    flags,
		})
	}

	m.buildBVH()

	// Detect shared edges using the spatial BVH, preserving deterministic (i, j) order.
	var candidates []int
	for i := 0; i < len(m.polys); i++ {
		pi := &m.polys[i]
		candidates = m.bvhQueryAABB(
			pi.minX-portalEps, pi.minY-portalEps,
			pi.maxX+portalEps, pi.maxY+portalEps,
			candidates[:0],
		)
		slices.Sort(candidates)
		for _, j := range candidates {
			if j <= i {
				continue
			}
			pj := &m.polys[j]
			if pi.maxX+portalEps < pj.minX || pj.maxX+portalEps < pi.minX ||
				pi.maxY+portalEps < pj.minY || pj.maxY+portalEps < pi.minY {
				continue
			}
			l, r, ok := sharedEdge(pi.verts, pj.verts)
			if !ok {
				continue
			}
			pidIJ := len(m.portals)
			m.portals = append(m.portals, navPortal{
				fromPoly: i,
				toPoly:   j,
				left:     l,
				right:    r,
			})
			pi.outPortals = append(pi.outPortals, pidIJ)

			pidJI := len(m.portals)
			m.portals = append(m.portals, navPortal{
				fromPoly: j,
				toPoly:   i,
				left:     r,
				right:    l,
			})
			pj.outPortals = append(pj.outPortals, pidJI)
		}
	}

	m.defaultQuery.mesh = m
	return m
}

func polygonArea2(verts []vec2) float64 {
	var sum float64
	n := len(verts)
	for i := range n {
		a := verts[i]
		b := verts[(i+1)%n]
		sum += a.cross(b)
	}
	return sum
}

// sharedEdge finds the overlapping collinear edge segment between two CCW convex polygons on opposite sides of the edge.
// It returns (left, right, true) oriented from polygon a looking out into polygon b.
func sharedEdge(a, b []vec2) (left, right vec2, ok bool) {
	na, nb := len(a), len(b)
	for i := range na {
		p0, p1 := a[i], a[(i+1)%na]
		edgeP := p1.sub(p0)
		lenP := edgeP.len()
		if lenP <= portalEps {
			continue
		}
		u := edgeP.scale(1 / lenP)
		n := vec2{-u.y, u.x}
		for j := range nb {
			q0, q1 := b[j], b[(j+1)%nb]
			edgeQ := q1.sub(q0)
			if edgeQ.len() <= portalEps {
				continue
			}
			// Adjacent CCW polygons traverse a shared edge in opposite directions.
			if edgeQ.dot(u) >= 0 {
				continue
			}
			if math.Abs(q0.sub(p0).dot(n)) > portalEps || math.Abs(q1.sub(p0).dot(n)) > portalEps {
				continue
			}
			t0 := q0.sub(p0).dot(u)
			t1 := q1.sub(p0).dot(u)
			lo := max(0, min(t0, t1))
			hi := min(lenP, max(t0, t1))
			if hi-lo <= portalEps {
				continue
			}
			return p0.add(u.scale(hi)), p0.add(u.scale(lo)), true
		}
	}
	return vec2{}, vec2{}, false
}

// PolygonCount returns the number of valid convex polygons in the mesh.
func (m *NavMesh) PolygonCount() int {
	if m == nil {
		return 0
	}
	return len(m.polys)
}

// Polygon returns a copy of the vertices of polygon index, or nil if index is out of range.
func (m *NavMesh) Polygon(index int) []core.Point {
	if m == nil || index < 0 || index >= len(m.polys) {
		return nil
	}
	vs := m.polys[index].verts
	out := make([]core.Point, len(vs))
	for i, v := range vs {
		out[i] = toPoint(v)
	}
	return out
}

// Neighbors returns the indices of polygons sharing an edge with polygon index.
func (m *NavMesh) Neighbors(index int) []int {
	if m == nil || index < 0 || index >= len(m.polys) {
		return nil
	}
	ps := m.polys[index].outPortals
	out := make([]int, len(ps))
	for i, pid := range ps {
		out[i] = m.portals[pid].toPoly
	}
	return out
}

// PolygonArea returns the area ID of polygon index, or NavAreaDefault if index is out of range.
func (m *NavMesh) PolygonArea(index int) NavArea {
	if m == nil || index < 0 || index >= len(m.polys) {
		return NavAreaDefault
	}
	return m.polys[index].area
}

// SetPolygonArea updates the area ID of polygon index at runtime without rebuilding the mesh.
// Out-of-range indices are ignored.
func (m *NavMesh) SetPolygonArea(index int, area NavArea) {
	if m == nil || index < 0 || index >= len(m.polys) {
		return
	}
	if m.polys[index].area != area {
		m.polys[index].area = area
		m.revision++
	}
}

// PolygonFlags returns the runtime traversal flags of polygon index, or 0 if index is out of range.
func (m *NavMesh) PolygonFlags(index int) NavFlags {
	if m == nil || index < 0 || index >= len(m.polys) {
		return 0
	}
	return m.polys[index].flags
}

// SetPolygonFlags replaces the runtime traversal flags of polygon index without rebuilding the mesh.
// Out-of-range indices are ignored.
func (m *NavMesh) SetPolygonFlags(index int, flags NavFlags) {
	if m == nil || index < 0 || index >= len(m.polys) {
		return
	}
	if m.polys[index].flags != flags {
		m.polys[index].flags = flags
		m.revision++
	}
}

// AddPolygonFlags sets the bits in flags on polygon index at runtime without rebuilding the mesh.
// Out-of-range indices are ignored.
func (m *NavMesh) AddPolygonFlags(index int, flags NavFlags) {
	if m == nil || index < 0 || index >= len(m.polys) {
		return
	}
	next := m.polys[index].flags | flags
	if next != m.polys[index].flags {
		m.polys[index].flags = next
		m.revision++
	}
}

// ClearPolygonFlags clears the bits in flags on polygon index at runtime without rebuilding the mesh.
// Out-of-range indices are ignored.
func (m *NavMesh) ClearPolygonFlags(index int, flags NavFlags) {
	if m == nil || index < 0 || index >= len(m.polys) {
		return
	}
	next := m.polys[index].flags &^ flags
	if next != m.polys[index].flags {
		m.polys[index].flags = next
		m.revision++
	}
}

// FindPolygon returns the lowest index of a polygon containing p (including its edges and vertices), or -1 if p lies outside the mesh.
func (m *NavMesh) FindPolygon(p core.Point) int {
	if m == nil || len(m.polys) == 0 {
		return -1
	}
	v := toVec2(p)
	if !isFiniteVec2(v) {
		return -1
	}
	return m.bvhFindPoly(v, NavFilter{}, false)
}

// FindPolygonWithFilter returns the lowest index of a polygon passing filter that contains p (including its edges and vertices), or -1 if none match.
func (m *NavMesh) FindPolygonWithFilter(p core.Point, filter NavFilter) int {
	if m == nil || len(m.polys) == 0 {
		return -1
	}
	v := toVec2(p)
	if !isFiniteVec2(v) {
		return -1
	}
	return m.bvhFindPoly(v, filter, true)
}

// Contains reports whether p lies inside or on the boundary of any polygon in the mesh.
func (m *NavMesh) Contains(p core.Point) bool {
	return m.FindPolygon(p) >= 0
}

// ContainsWithFilter reports whether p lies inside or on the boundary of any polygon passing filter.
func (m *NavMesh) ContainsWithFilter(p core.Point, filter NavFilter) bool {
	return m.FindPolygonWithFilter(p, filter) >= 0
}

// NearestPolygon returns the polygon closest to p and the closest point on or inside that polygon.
// When p lies inside a polygon, NearestPolygon returns that polygon and p itself.
func (m *NavMesh) NearestPolygon(p core.Point) (index int, point core.Point, ok bool) {
	return m.NearestPolygonWithFilter(p, NavFilter{})
}

// NearestPolygonWithFilter returns the polygon passing filter closest to p and the closest point on or inside that polygon.
// When p lies inside a matching polygon, NearestPolygonWithFilter returns that polygon and p itself.
func (m *NavMesh) NearestPolygonWithFilter(p core.Point, filter NavFilter) (index int, point core.Point, ok bool) {
	if m == nil || len(m.polys) == 0 {
		return -1, core.Point{}, false
	}
	v := toVec2(p)
	if !isFiniteVec2(v) {
		return -1, core.Point{}, false
	}
	if idx := m.bvhFindPoly(v, filter, true); idx >= 0 {
		return idx, p, true
	}
	bestIdx, bestPt, found := m.bvhNearestPoly(v, vec2{}, false, filter)
	if !found {
		return -1, core.Point{}, false
	}
	return bestIdx, toPoint(bestPt), true
}

func polyContains(poly *navPoly, p vec2) bool {
	if p.x < poly.minX-portalEps || p.x > poly.maxX+portalEps ||
		p.y < poly.minY-portalEps || p.y > poly.maxY+portalEps {
		return false
	}
	vs := poly.verts
	n := len(vs)
	for i := range n {
		a := vs[i]
		b := vs[(i+1)%n]
		edge := b.sub(a)
		edgeLen := edge.len()
		if edge.cross(p.sub(a)) < -portalEps*edgeLen {
			return false
		}
	}
	return true
}

// NavPathResult is the outcome of a bounded or incremental 2D NavMesh search.
type NavPathResult struct {
	Path       []core.Point
	Found      bool // true when a complete or partial path was produced
	Partial    bool // true when Path ends at the closest reachable point rather than goal
	Exhausted  bool // true when the search stopped because MaxExpansions was reached
	Expansions int  // total number of portal nodes expanded during the search
}

// Revision returns a monotonically increasing number bumped whenever the mesh topology, area costs/flags, or link flags change.
func (m *NavMesh) Revision() uint64 {
	if m == nil {
		return 0
	}
	return m.revision
}

// BumpRevision increments the mesh revision so NavAgents watching this mesh invalidate cached corridors and replan.
func (m *NavMesh) BumpRevision() {
	if m == nil {
		return
	}
	m.revision++
}

// AddOffMeshLink connects the polygons containing link.Start and link.End with a directed
// (or bidirectional) off-mesh portal and bumps the mesh revision.
// Returns false if the mesh is nil or either endpoint lies outside the mesh.
func (m *NavMesh) AddOffMeshLink(link NavOffMeshLink) bool {
	if m == nil || len(m.polys) == 0 {
		return false
	}
	fromPoly := m.FindPolygon(link.Start)
	toPoly := m.FindPolygon(link.End)
	if fromPoly < 0 || toPoly < 0 || fromPoly == toPoly {
		return false
	}
	sVec := toVec2(link.Start)
	eVec := toVec2(link.End)
	cost := float64(link.Cost)
	if !(cost > 0) || math.IsInf(cost, 0) {
		cost = dist(sVec, eVec)
	}
	flags := link.Flags | NavLinkOffMesh

	pid := len(m.portals)
	m.portals = append(m.portals, navPortal{
		fromPoly: fromPoly,
		toPoly:   toPoly,
		left:     sVec,
		right:    sVec,
		endPos:   eVec,
		cost:     cost,
		flags:    flags,
		linkID:   link.ID,
		offMesh:  true,
	})
	m.polys[fromPoly].outPortals = append(m.polys[fromPoly].outPortals, pid)

	if link.Bidirectional {
		rpid := len(m.portals)
		m.portals = append(m.portals, navPortal{
			fromPoly: toPoly,
			toPoly:   fromPoly,
			left:     eVec,
			right:    eVec,
			endPos:   sVec,
			cost:     cost,
			flags:    flags,
			linkID:   link.ID,
			offMesh:  true,
		})
		m.polys[toPoly].outPortals = append(m.polys[toPoly].outPortals, rpid)
	}
	m.revision++
	return true
}

// SetEdgeFlags sets custom flags on the shared-edge portal from fromPoly to toPoly and bumps the mesh revision.
// Returns false if no shared-edge portal connects fromPoly to toPoly.
func (m *NavMesh) SetEdgeFlags(fromPoly, toPoly int, flags uint32) bool {
	if m == nil || fromPoly < 0 || fromPoly >= len(m.polys) || toPoly < 0 || toPoly >= len(m.polys) {
		return false
	}
	changed := false
	for _, pid := range m.polys[fromPoly].outPortals {
		p := &m.portals[pid]
		if !p.offMesh && p.toPoly == toPoly {
			p.flags = flags
			changed = true
		}
	}
	if changed {
		m.revision++
	}
	return changed
}

// FindCorridor finds the sequence of polygon indices from start to goal using DefaultNavFilter and portal-graph A*.
// Returns (nil, false) if start or goal lies outside traversable polygons or in disconnected regions.
func (m *NavMesh) FindCorridor(start, goal core.Point) ([]int, bool) {
	return m.FindCorridorWithFilter(start, goal, DefaultNavFilter())
}

// FindCorridorWithFilter finds the minimum-cost sequence of polygon indices from start to goal restricted to filter.
// Returns (nil, false) if start or goal lies outside traversable polygons or in disconnected regions.
func (m *NavMesh) FindCorridorWithFilter(start, goal core.Point, filter NavFilter) ([]int, bool) {
	polys, _, ok := m.findCorridorWithFilter(toVec2(start), toVec2(goal), filter)
	if !ok {
		return nil, false
	}
	return polys, true
}

// FindPath finds the shortest path inside the navigation mesh from start to goal using DefaultNavFilter, portal-graph A*, and Simple Stupid Funnel Algorithm string-pulling.
//
// It returns (nil, false) if start or goal lies outside traversable polygons or in disconnected regions.
func (m *NavMesh) FindPath(start, goal core.Point) ([]core.Point, bool) {
	return m.FindPathWithFilter(start, goal, DefaultNavFilter())
}

// FindPathWithFilter finds the minimum-cost path inside the navigation mesh from start to goal restricted to polygons allowed by filter, weighting traversal through each polygon by filter.AreaCost(area) and using an admissible distance heuristic scaled by the minimum allowed area cost.
//
// It returns (nil, false) if start or goal lies outside traversable polygons or in disconnected regions.
func (m *NavMesh) FindPathWithFilter(start, goal core.Point, filter NavFilter) ([]core.Point, bool) {
	res := m.FindPathWithFilterAndOptions(start, goal, filter, PathOptions{})
	if !res.Found || res.Partial {
		return nil, false
	}
	return res.Path, true
}

// FindPathWithOptions finds a path inside the navigation mesh under DefaultNavFilter and opts,
// supporting an expansion budget and optional partial paths to the closest reachable point when
// goal is outside the mesh, in a disconnected region, or beyond MaxExpansions.
func (m *NavMesh) FindPathWithOptions(start, goal core.Point, opts PathOptions) NavPathResult {
	return m.FindPathWithFilterAndOptions(start, goal, DefaultNavFilter(), opts)
}

// FindPathWithFilterAndOptions finds a minimum-cost path inside the navigation mesh restricted to
// polygons allowed by filter under opts, supporting an expansion budget and optional partial paths.
func (m *NavMesh) FindPathWithFilterAndOptions(start, goal core.Point, filter NavFilter, opts PathOptions) NavPathResult {
	s := NewNavMeshSearchWithFilter(m, start, goal, filter, opts)
	s.step(-1)
	return s.Result()
}

// StartSearch begins an incremental 2D NavMesh path search from start to goal under DefaultNavFilter and opts.
func (m *NavMesh) StartSearch(start, goal core.Point, opts PathOptions) *NavMeshSearch {
	return NewNavMeshSearchWithFilter(m, start, goal, DefaultNavFilter(), opts)
}

// StartSearchWithFilter begins an incremental 2D NavMesh path search from start to goal under filter and opts.
func (m *NavMesh) StartSearchWithFilter(start, goal core.Point, filter NavFilter, opts PathOptions) *NavMeshSearch {
	return NewNavMeshSearchWithFilter(m, start, goal, filter, opts)
}

// NavMeshSearch runs a deterministic 2D NavMesh portal A* search incrementally across bounded Step calls.
//
// Stepping a search with any positive per-step expansion budget produces the exact same path,
// partial flag, and expansion count as NavMesh.FindPathWithOptions / FindPathWithFilterAndOptions.
// *NavMeshSearch implements IncrementalSearch.
type NavMeshSearch struct {
	m          *NavMesh
	start      core.Point
	goal       core.Point
	sVec       vec2
	gVec       vec2
	opts       PathOptions
	allowed    []bool
	polyCost   []float64
	minCost    float64
	isGoalPoly []bool
	goalNode   int

	gScore  []float64
	parent  []int
	entryPt []vec2
	closed  []bool
	heapPos []int
	open    []astarNode

	status       SearchStatus
	exhausted    bool
	expansions   int
	samePoly     bool
	directPoly   int
	hasBest      bool
	bestPortalID int
	bestPt       vec2
	bestH        float64
	bestG        float64
}

// NewNavMeshSearch initializes an incremental 2D NavMesh search from start to goal under DefaultNavFilter and opts.
func NewNavMeshSearch(m *NavMesh, start, goal core.Point, opts PathOptions) *NavMeshSearch {
	return NewNavMeshSearchWithFilter(m, start, goal, DefaultNavFilter(), opts)
}

// NewNavMeshSearchWithFilter initializes an incremental 2D NavMesh search from start to goal under filter and opts.
func NewNavMeshSearchWithFilter(m *NavMesh, start, goal core.Point, filter NavFilter, opts PathOptions) *NavMeshSearch {
	s := &NavMeshSearch{
		m:            m,
		start:        start,
		goal:         goal,
		sVec:         toVec2(start),
		gVec:         toVec2(goal),
		opts:         opts,
		status:       SearchFailed,
		directPoly:   -1,
		bestPortalID: -1,
	}
	if m == nil || len(m.polys) == 0 {
		return s
	}

	allowed := make([]bool, len(m.polys))
	polyCost := make([]float64, len(m.polys))
	minCost := math.Inf(1)

	var startPolys []int
	isGoalPoly := make([]bool, len(m.polys))
	hasGoal := false
	directPossible := false
	directCost := math.Inf(1)

	for i := range m.polys {
		p := &m.polys[i]
		if !filter.Allows(p.area, p.flags) {
			continue
		}
		c := filter.AreaCost(p.area)
		if c <= 0 || math.IsNaN(c) || math.IsInf(c, 0) {
			continue
		}
		allowed[i] = true
		polyCost[i] = c
		if c < minCost {
			minCost = c
		}

		inStart := polyContains(p, s.sVec)
		inGoal := polyContains(p, s.gVec)
		if inStart && inGoal {
			if vequal(s.sVec, s.gVec) {
				s.samePoly = true
				s.directPoly = i
				s.status = SearchComplete
				return s
			}
			dc := dist(s.sVec, s.gVec) * c
			if dc < directCost {
				directCost = dc
				directPossible = true
				s.directPoly = i
			}
		}
		if inStart {
			startPolys = append(startPolys, i)
		}
		if inGoal {
			isGoalPoly[i] = true
			hasGoal = true
		}
	}
	if len(startPolys) == 0 {
		return s
	}
	if !hasGoal && !opts.AllowPartial {
		return s
	}
	if directPossible && directCost <= minCost*dist(s.sVec, s.gVec)+1e-9 {
		s.samePoly = true
		s.status = SearchComplete
		return s
	}

	if opts.AllowPartial {
		for _, sp := range startPolys {
			cp := closestPointInPoly(&m.polys[sp], s.gVec)
			h := dist(cp, s.gVec)
			g := dist(s.sVec, cp) * polyCost[sp]
			if !s.hasBest || h < s.bestH || (h == s.bestH && g < s.bestG) {
				s.hasBest = true
				s.bestPortalID = -1
				s.bestPt = cp
				s.bestH = h
				s.bestG = g
			}
		}
	}

	// Portal-graph A* over directed portals plus one virtual goal node.
	goalNode := len(m.portals)
	nodeCount := goalNode + 1

	s.allowed = allowed
	s.polyCost = polyCost
	s.minCost = minCost
	s.isGoalPoly = isGoalPoly
	s.goalNode = goalNode
	s.gScore = make([]float64, nodeCount)
	for i := range s.gScore {
		s.gScore[i] = math.Inf(1)
	}
	s.parent = make([]int, nodeCount)
	for i := range s.parent {
		s.parent[i] = -1
	}
	s.entryPt = make([]vec2, nodeCount)
	s.closed = make([]bool, nodeCount)
	s.heapPos = make([]int, nodeCount)
	for i := range s.heapPos {
		s.heapPos[i] = -1
	}
	s.open = make([]astarNode, 0, min(nodeCount, 64))

	if directPossible {
		s.gScore[goalNode] = directCost
		s.parent[goalNode] = -1
		s.pushOrUpdate(goalNode, directCost, 0)
	}

	for _, sp := range startPolys {
		spCost := polyCost[sp]
		for _, pid := range m.polys[sp].outPortals {
			p := m.portals[pid]
			if !allowed[p.toPoly] {
				continue
			}
			var g float64
			var exitPt vec2
			if p.offMesh {
				g = dist(s.sVec, p.left)*spCost + p.cost
				exitPt = p.endPos
			} else {
				exitPt = bestPortalPoint(s.sVec, s.gVec, p.left, p.right)
				g = dist(s.sVec, exitPt) * spCost
			}
			if g < s.gScore[pid] {
				s.gScore[pid] = g
				s.entryPt[pid] = exitPt
				s.parent[pid] = -1
				h := dist(exitPt, s.gVec) * minCost
				s.pushOrUpdate(pid, g+h, h)
			}
		}
	}

	if len(s.open) == 0 {
		if opts.AllowPartial && s.hasBest {
			s.status = SearchPartial
		} else {
			s.status = SearchFailed
		}
		return s
	}

	s.status = SearchInProgress
	return s
}

func (s *NavMeshSearch) pushOrUpdate(id int, f, h float64) {
	if pos := s.heapPos[id]; pos >= 0 {
		s.open[pos].f = f
		s.open[pos].h = h
		navSiftUp(s.open, s.heapPos, pos)
		return
	}
	pos := len(s.open)
	s.open = append(s.open, astarNode{id: id, f: f, h: h})
	s.heapPos[id] = pos
	navSiftUp(s.open, s.heapPos, pos)
}

// Step advances the search by at most maxExpansions portal node expansions and returns
// the updated status and exact number of expansions performed.
// When maxExpansions <= 0 or the search is already finished, Step performs 0 expansions.
func (s *NavMeshSearch) Step(maxExpansions int) (status SearchStatus, expansions int) {
	if s == nil {
		return SearchFailed, 0
	}
	if maxExpansions <= 0 || s.status.Done() {
		return s.status, 0
	}
	return s.step(maxExpansions)
}

func (s *NavMeshSearch) step(stepLimit int) (SearchStatus, int) {
	if s.status.Done() {
		return s.status, 0
	}

	budget := stepLimit
	if s.opts.MaxExpansions > 0 {
		rem := s.opts.MaxExpansions - s.expansions
		if rem <= 0 {
			s.finishExhausted()
			return s.status, 0
		}
		if budget < 0 || rem < budget {
			budget = rem
		}
	}

	m := s.m
	done := 0
	for len(s.open) > 0 && (budget < 0 || done < budget) {
		cur := navHeapPop(&s.open, s.heapPos)
		done++
		s.expansions++

		if cur.id == s.goalNode {
			s.status = SearchComplete
			return s.status, done
		}
		s.closed[cur.id] = true

		portal := m.portals[cur.id]
		curG := s.gScore[cur.id]
		curPt := s.entryPt[cur.id]
		toCost := s.polyCost[portal.toPoly]

		if s.opts.AllowPartial {
			cp := closestPointInPoly(&m.polys[portal.toPoly], s.gVec)
			h := dist(cp, s.gVec)
			g := curG + dist(curPt, cp)*toCost
			if !s.hasBest || h < s.bestH || (h == s.bestH && (g < s.bestG || (g == s.bestG && cur.id < s.bestPortalID))) {
				s.hasBest = true
				s.bestPortalID = cur.id
				s.bestPt = cp
				s.bestH = h
				s.bestG = g
			}
		}

		if s.isGoalPoly[portal.toPoly] {
			ng := curG + dist(curPt, s.gVec)*toCost
			if ng < s.gScore[s.goalNode] {
				s.gScore[s.goalNode] = ng
				s.parent[s.goalNode] = cur.id
				s.pushOrUpdate(s.goalNode, ng, 0)
			}
		}

		for _, npid := range m.polys[portal.toPoly].outPortals {
			if s.closed[npid] {
				continue
			}
			np := m.portals[npid]
			if (!np.offMesh && np.toPoly == portal.fromPoly) || !s.allowed[np.toPoly] {
				continue
			}
			var ng float64
			var exitPt vec2
			if np.offMesh {
				ng = curG + dist(curPt, np.left)*toCost + np.cost
				exitPt = np.endPos
			} else {
				exitPt = bestPortalPoint(curPt, s.gVec, np.left, np.right)
				ng = curG + dist(curPt, exitPt)*toCost
			}
			if ng < s.gScore[npid] {
				s.gScore[npid] = ng
				s.entryPt[npid] = exitPt
				s.parent[npid] = cur.id
				h := dist(exitPt, s.gVec) * s.minCost
				s.pushOrUpdate(npid, ng+h, h)
			}
		}
	}

	if len(s.open) == 0 {
		s.exhausted = false
		if s.opts.AllowPartial && s.hasBest {
			s.status = SearchPartial
		} else {
			s.status = SearchFailed
		}
	} else if s.opts.MaxExpansions > 0 && s.expansions >= s.opts.MaxExpansions {
		s.finishExhausted()
	}

	return s.status, done
}

func (s *NavMeshSearch) finishExhausted() {
	s.exhausted = true
	if s.opts.AllowPartial && s.hasBest {
		s.status = SearchPartial
	} else {
		s.status = SearchFailed
	}
}

// Status returns the search's current status.
func (s *NavMeshSearch) Status() SearchStatus {
	if s == nil {
		return SearchFailed
	}
	return s.status
}

// Expansions returns the cumulative number of portal nodes expanded so far.
func (s *NavMeshSearch) Expansions() int {
	if s == nil {
		return 0
	}
	return s.expansions
}

// Result returns the search outcome once finished (or a result with Found == false while still in progress).
func (s *NavMeshSearch) Result() NavPathResult {
	if s == nil {
		return NavPathResult{}
	}
	res := NavPathResult{
		Exhausted:  s.exhausted,
		Expansions: s.expansions,
	}
	switch s.status {
	case SearchComplete:
		res.Found = true
		if s.samePoly || s.parent[s.goalNode] == -1 {
			if vequal(s.sVec, s.gVec) {
				res.Path = []core.Point{s.start}
			} else {
				res.Path = []core.Point{s.start, s.goal}
			}
			return res
		}
		var portalIDs []int
		for id := s.parent[s.goalNode]; id != -1; id = s.parent[id] {
			portalIDs = append(portalIDs, id)
		}
		slices.Reverse(portalIDs)
		res.Path = s.m.finalizeCorridor(s.sVec, s.gVec, portalIDs)
		return res

	case SearchPartial:
		res.Found = true
		res.Partial = true
		if s.bestPortalID < 0 {
			if vequal(s.sVec, s.bestPt) {
				res.Path = []core.Point{s.start}
			} else {
				res.Path = []core.Point{s.start, toPoint(s.bestPt)}
			}
			return res
		}
		var portalIDs []int
		for id := s.bestPortalID; id != -1; id = s.parent[id] {
			portalIDs = append(portalIDs, id)
		}
		slices.Reverse(portalIDs)
		res.Path = s.m.finalizeCorridor(s.sVec, s.bestPt, portalIDs)
		return res

	default:
		return res
	}
}

func (m *NavMesh) findCorridorWithFilter(sVec, gVec vec2, filter NavFilter) ([]int, []int, bool) {
	s := NewNavMeshSearchWithFilter(m, toPoint(sVec), toPoint(gVec), filter, PathOptions{})
	s.step(-1)
	if s.status != SearchComplete {
		return nil, nil, false
	}
	if s.samePoly || s.parent[s.goalNode] == -1 {
		dp := s.directPoly
		if dp < 0 {
			dp = m.FindPolygonWithFilter(toPoint(sVec), filter)
		}
		if dp < 0 {
			return nil, nil, false
		}
		return []int{dp}, nil, true
	}
	var portalIDs []int
	for id := s.parent[s.goalNode]; id != -1; id = s.parent[id] {
		portalIDs = append(portalIDs, id)
	}
	slices.Reverse(portalIDs)
	polys := make([]int, len(portalIDs)+1)
	polys[0] = m.portals[portalIDs[0]].fromPoly
	for i, pid := range portalIDs {
		polys[i+1] = m.portals[pid].toPoly
	}
	return polys, portalIDs, true
}

func (m *NavMesh) finalizeCorridor(start, end vec2, portalIDs []int) []core.Point {
	rawPath := m.stringPull(start, end, portalIDs)
	clean := simplifyCollinear(rawPath)
	out := make([]core.Point, len(clean))
	for i, v := range clean {
		out[i] = toPoint(v)
	}
	return out
}

func (m *NavMesh) polyClosestPoint(polyIdx int, p vec2) vec2 {
	return closestPointInPoly(&m.polys[polyIdx], p)
}

func closestPointInPoly(poly *navPoly, p vec2) vec2 {
	if polyContains(poly, p) {
		return p
	}
	vs := poly.verts
	n := len(vs)
	bestDistSq := math.Inf(1)
	var bestPt vec2
	for k := range n {
		cp := closestPointOnSegment(p, vs[k], vs[(k+1)%n])
		d2 := distSq(p, cp)
		if d2 < bestDistSq {
			bestDistSq = d2
			bestPt = cp
		}
	}
	return bestPt
}

// bestPortalPoint returns the point p on segment [left, right] minimizing dist(from, p) + dist(p, goal).
func bestPortalPoint(from, goal, left, right vec2) vec2 {
	seg := right.sub(left)
	segLen := seg.len()
	if segLen <= 1e-12 {
		return left
	}
	u := seg.scale(1 / segLen)
	n := vec2{-u.y, u.x}
	a1 := math.Abs(from.sub(left).dot(n))
	a2 := math.Abs(goal.sub(left).dot(n))
	s1 := from.sub(left).dot(u)
	s2 := goal.sub(left).dot(u)
	var s float64
	if a1+a2 > 1e-12 {
		s = (s1*a2 + s2*a1) / (a1 + a2)
	} else {
		s = 0.5 * (s1 + s2)
	}
	s = min(max(s, 0), segLen)
	return left.add(u.scale(s))
}

// stringPull runs the Simple Stupid Funnel Algorithm across portalIDs from start to goal,
// splitting cleanly across any off-mesh links.
func (m *NavMesh) stringPull(start, goal vec2, portalIDs []int) []vec2 {
	hasOffMesh := false
	for _, pid := range portalIDs {
		if m.portals[pid].offMesh {
			hasOffMesh = true
			break
		}
	}
	if !hasOffMesh {
		return m.stringPullSegment(start, goal, portalIDs)
	}

	var out []vec2
	segStart := start
	segBegin := 0
	for i, pid := range portalIDs {
		p := m.portals[pid]
		if !p.offMesh {
			continue
		}
		seg := m.stringPullSegment(segStart, p.left, portalIDs[segBegin:i])
		for _, pt := range seg {
			if len(out) == 0 || !vequal(out[len(out)-1], pt) {
				out = append(out, pt)
			}
		}
		if len(out) == 0 || !vequal(out[len(out)-1], p.endPos) {
			out = append(out, p.endPos)
		}
		segStart = p.endPos
		segBegin = i + 1
	}
	tail := m.stringPullSegment(segStart, goal, portalIDs[segBegin:])
	for _, pt := range tail {
		if len(out) == 0 || !vequal(out[len(out)-1], pt) {
			out = append(out, pt)
		}
	}
	return out
}

func (m *NavMesh) stringPullSegment(start, goal vec2, portalIDs []int) []vec2 {
	type funnelPortal struct {
		left, right vec2
	}
	portals := make([]funnelPortal, 0, len(portalIDs)+2)
	portals = append(portals, funnelPortal{left: start, right: start})
	for _, pid := range portalIDs {
		p := m.portals[pid]
		portals = append(portals, funnelPortal{left: p.left, right: p.right})
	}
	portals = append(portals, funnelPortal{left: goal, right: goal})

	path := []vec2{start}
	portalApex := start
	portalLeft := start
	portalRight := start
	apexIndex := 0
	leftIndex := 0
	rightIndex := 0

	for i := 1; i < len(portals); i++ {
		left := portals[i].left
		right := portals[i].right

		// Update right vertex.
		if triArea2(portalApex, portalRight, right) >= 0 {
			if vequal(portalApex, portalRight) || vequal(portalApex, portalLeft) || triArea2(portalApex, portalLeft, right) < 0 {
				portalRight = right
				rightIndex = i
			} else {
				// Right crossed over left: insert left into path and restart from left.
				if !vequal(path[len(path)-1], portalLeft) {
					path = append(path, portalLeft)
				}
				portalApex = portalLeft
				apexIndex = leftIndex
				portalLeft = portalApex
				portalRight = portalApex
				leftIndex = apexIndex
				rightIndex = apexIndex
				i = apexIndex
				continue
			}
		}

		// Update left vertex.
		if triArea2(portalApex, portalLeft, left) <= 0 {
			if vequal(portalApex, portalLeft) || vequal(portalApex, portalRight) || triArea2(portalApex, portalRight, left) > 0 {
				portalLeft = left
				leftIndex = i
			} else {
				// Left crossed over right: insert right into path and restart from right.
				if !vequal(path[len(path)-1], portalRight) {
					path = append(path, portalRight)
				}
				portalApex = portalRight
				apexIndex = rightIndex
				portalLeft = portalApex
				portalRight = portalApex
				leftIndex = apexIndex
				rightIndex = apexIndex
				i = apexIndex
				continue
			}
		}
	}

	if !vequal(path[len(path)-1], goal) {
		path = append(path, goal)
	}
	return path
}

// nextFunnelCorner returns the first visible funnel corner from pos along portalIDs toward goal.
// isGoal is true when the returned corner is the final goal with straight line of sight through the corridor.
// offMeshIdx is the index into portalIDs if the returned corner is an off-mesh link entry point, or -1 otherwise.
func (m *NavMesh) nextFunnelCorner(pos, goal vec2, portalIDs []int) (corner vec2, isGoal bool, offMeshIdx int) {
	if len(portalIDs) == 0 {
		return goal, true, -1
	}
	if m.portals[portalIDs[0]].offMesh {
		return m.portals[portalIDs[0]].left, false, 0
	}

	endPt := goal
	endIsGoal := true
	endOffMesh := -1
	segLen := len(portalIDs)
	for i, pid := range portalIDs {
		if m.portals[pid].offMesh {
			endPt = m.portals[pid].left
			endIsGoal = false
			endOffMesh = i
			segLen = i
			break
		}
	}

	type funnelPortal struct {
		left, right vec2
	}
	portals := make([]funnelPortal, 0, segLen+2)
	portals = append(portals, funnelPortal{left: pos, right: pos})
	for i := range segLen {
		p := m.portals[portalIDs[i]]
		portals = append(portals, funnelPortal{left: p.left, right: p.right})
	}
	portals = append(portals, funnelPortal{left: endPt, right: endPt})

	const cornerEpsSq = 1e-6 // 1e-3 world units
	portalApex := pos
	portalLeft := pos
	portalRight := pos
	apexIndex := 0
	leftIndex := 0
	rightIndex := 0

	for i := 1; i < len(portals); i++ {
		left := portals[i].left
		right := portals[i].right

		if triArea2(portalApex, portalRight, right) >= 0 {
			if vequal(portalApex, portalRight) || vequal(portalApex, portalLeft) || triArea2(portalApex, portalLeft, right) < 0 {
				portalRight = right
				rightIndex = i
			} else {
				if distSq(pos, portalLeft) > cornerEpsSq {
					return portalLeft, false, -1
				}
				portalApex = portalLeft
				apexIndex = leftIndex
				portalLeft = portalApex
				portalRight = portalApex
				leftIndex = apexIndex
				rightIndex = apexIndex
				i = apexIndex
				continue
			}
		}

		if triArea2(portalApex, portalLeft, left) <= 0 {
			if vequal(portalApex, portalLeft) || vequal(portalApex, portalRight) || triArea2(portalApex, portalRight, left) > 0 {
				portalLeft = left
				leftIndex = i
			} else {
				if distSq(pos, portalRight) > cornerEpsSq {
					return portalRight, false, -1
				}
				portalApex = portalRight
				apexIndex = rightIndex
				portalLeft = portalApex
				portalRight = portalApex
				leftIndex = apexIndex
				rightIndex = apexIndex
				i = apexIndex
				continue
			}
		}
	}

	return endPt, endIsGoal, endOffMesh
}

func simplifyCollinear(pts []vec2) []vec2 {
	if len(pts) <= 2 {
		return pts
	}
	out := make([]vec2, 0, len(pts))
	out = append(out, pts[0])
	for i := 1; i < len(pts)-1; i++ {
		a := out[len(out)-1]
		b := pts[i]
		c := pts[i+1]
		ac := c.sub(a)
		acLen := ac.len()
		if acLen > 1e-12 && math.Abs(triArea2(a, b, c)) <= navEps*acLen {
			t := b.sub(a).dot(ac) / (acLen * acLen)
			if t >= -navEps && t <= 1+navEps {
				continue
			}
		}
		out = append(out, b)
	}
	out = append(out, pts[len(pts)-1])
	return out
}

func navHeapPop(open *[]astarNode, heapPos []int) astarNode {
	s := *open
	top := s[0]
	heapPos[top.id] = -1
	last := len(s) - 1
	if last == 0 {
		*open = s[:0]
		return top
	}
	s[0] = s[last]
	heapPos[s[0].id] = 0
	s = s[:last]
	*open = s
	navSiftDown(s, heapPos, 0)
	return top
}

func navSiftUp(s []astarNode, heapPos []int, i int) {
	for i > 0 {
		p := (i - 1) / 2
		if !astarLess(s[i], s[p]) {
			break
		}
		s[i], s[p] = s[p], s[i]
		heapPos[s[i].id] = i
		heapPos[s[p].id] = p
		i = p
	}
}

// navMeshScratch holds reusable buffers for zero-allocation NavMesh path-length queries.
type navMeshScratch struct {
	startPolys    []int
	isGoalPoly    []bool
	gScore        []float64
	parent        []int
	entryPt       []vec2
	closed        []bool
	heapPos       []int
	open          []astarNode
	portalIDs     []int
	funnelPortals [][2]vec2
	rawPath       []vec2
}

// findPathLength computes the shortest path length inside m from start to goal using
// reusable scratch buffers so repeated queries perform zero heap allocations after warm-up.
func (m *NavMesh) findPathLength(s *navMeshScratch, start, goal vec2) (float64, bool) {
	if m == nil || len(m.polys) == 0 {
		return 0, false
	}
	nPolys := len(m.polys)
	s.startPolys = s.startPolys[:0]
	if cap(s.isGoalPoly) < nPolys {
		s.isGoalPoly = make([]bool, nPolys)
	} else {
		s.isGoalPoly = s.isGoalPoly[:nPolys]
		clear(s.isGoalPoly)
	}
	hasGoal := false
	for i := range m.polys {
		inStart := polyContains(&m.polys[i], start)
		inGoal := polyContains(&m.polys[i], goal)
		if inStart && inGoal {
			if vequal(start, goal) {
				return 0, true
			}
			return dist(start, goal), true
		}
		if inStart {
			s.startPolys = append(s.startPolys, i)
		}
		if inGoal {
			s.isGoalPoly[i] = true
			hasGoal = true
		}
	}
	if len(s.startPolys) == 0 || !hasGoal {
		return 0, false
	}

	goalNode := len(m.portals)
	nodeCount := goalNode + 1

	if cap(s.gScore) < nodeCount {
		s.gScore = make([]float64, nodeCount)
	} else {
		s.gScore = s.gScore[:nodeCount]
	}
	if cap(s.parent) < nodeCount {
		s.parent = make([]int, nodeCount)
	} else {
		s.parent = s.parent[:nodeCount]
	}
	if cap(s.entryPt) < nodeCount {
		s.entryPt = make([]vec2, nodeCount)
	} else {
		s.entryPt = s.entryPt[:nodeCount]
	}
	if cap(s.closed) < nodeCount {
		s.closed = make([]bool, nodeCount)
	} else {
		s.closed = s.closed[:nodeCount]
	}
	if cap(s.heapPos) < nodeCount {
		s.heapPos = make([]int, nodeCount)
	} else {
		s.heapPos = s.heapPos[:nodeCount]
	}
	for i := range nodeCount {
		s.gScore[i] = math.Inf(1)
		s.parent[i] = -1
		s.closed[i] = false
		s.heapPos[i] = -1
	}
	s.open = s.open[:0]

	pushOrUpdate := func(id int, f, h float64) {
		if pos := s.heapPos[id]; pos >= 0 {
			s.open[pos].f = f
			s.open[pos].h = h
			navSiftUp(s.open, s.heapPos, pos)
			return
		}
		pos := len(s.open)
		s.open = append(s.open, astarNode{id: id, f: f, h: h})
		s.heapPos[id] = pos
		navSiftUp(s.open, s.heapPos, pos)
	}

	for _, sp := range s.startPolys {
		for _, pid := range m.polys[sp].outPortals {
			p := m.portals[pid]
			pt := bestPortalPoint(start, goal, p.left, p.right)
			g := dist(start, pt)
			if g < s.gScore[pid] {
				s.gScore[pid] = g
				s.entryPt[pid] = pt
				s.parent[pid] = -1
				h := dist(pt, goal)
				pushOrUpdate(pid, g+h, h)
			}
		}
	}

	found := false
	for len(s.open) > 0 {
		cur := navHeapPop(&s.open, s.heapPos)
		if cur.id == goalNode {
			found = true
			break
		}
		s.closed[cur.id] = true

		portal := m.portals[cur.id]
		curG := s.gScore[cur.id]
		curPt := s.entryPt[cur.id]

		if s.isGoalPoly[portal.toPoly] {
			ng := curG + dist(curPt, goal)
			if ng < s.gScore[goalNode] {
				s.gScore[goalNode] = ng
				s.parent[goalNode] = cur.id
				pushOrUpdate(goalNode, ng, 0)
			}
		}

		for _, npid := range m.polys[portal.toPoly].outPortals {
			if s.closed[npid] {
				continue
			}
			np := m.portals[npid]
			if np.toPoly == portal.fromPoly {
				continue
			}
			pt := bestPortalPoint(curPt, goal, np.left, np.right)
			ng := curG + dist(curPt, pt)
			if ng < s.gScore[npid] {
				s.gScore[npid] = ng
				s.entryPt[npid] = pt
				s.parent[npid] = cur.id
				h := dist(pt, goal)
				pushOrUpdate(npid, ng+h, h)
			}
		}
	}

	if !found {
		return 0, false
	}

	s.portalIDs = s.portalIDs[:0]
	for id := s.parent[goalNode]; id != -1; id = s.parent[id] {
		s.portalIDs = append(s.portalIDs, id)
	}
	slices.Reverse(s.portalIDs)

	s.funnelPortals = append(s.funnelPortals[:0], [2]vec2{start, start})
	for _, pid := range s.portalIDs {
		p := m.portals[pid]
		s.funnelPortals = append(s.funnelPortals, [2]vec2{p.left, p.right})
	}
	s.funnelPortals = append(s.funnelPortals, [2]vec2{goal, goal})

	s.rawPath = append(s.rawPath[:0], start)
	portalApex := start
	portalLeft := start
	portalRight := start
	apexIndex := 0
	leftIndex := 0
	rightIndex := 0

	for i := 1; i < len(s.funnelPortals); i++ {
		left := s.funnelPortals[i][0]
		right := s.funnelPortals[i][1]

		if triArea2(portalApex, portalRight, right) >= 0 {
			if vequal(portalApex, portalRight) || vequal(portalApex, portalLeft) || triArea2(portalApex, portalLeft, right) < 0 {
				portalRight = right
				rightIndex = i
			} else {
				if !vequal(s.rawPath[len(s.rawPath)-1], portalLeft) {
					s.rawPath = append(s.rawPath, portalLeft)
				}
				portalApex = portalLeft
				apexIndex = leftIndex
				portalLeft = portalApex
				portalRight = portalApex
				leftIndex = apexIndex
				rightIndex = apexIndex
				i = apexIndex
				continue
			}
		}

		if triArea2(portalApex, portalLeft, left) <= 0 {
			if vequal(portalApex, portalLeft) || vequal(portalApex, portalRight) || triArea2(portalApex, portalRight, left) > 0 {
				portalLeft = left
				leftIndex = i
			} else {
				if !vequal(s.rawPath[len(s.rawPath)-1], portalRight) {
					s.rawPath = append(s.rawPath, portalRight)
				}
				portalApex = portalRight
				apexIndex = rightIndex
				portalLeft = portalApex
				portalRight = portalApex
				leftIndex = apexIndex
				rightIndex = apexIndex
				i = apexIndex
				continue
			}
		}
	}
	if !vequal(s.rawPath[len(s.rawPath)-1], goal) {
		s.rawPath = append(s.rawPath, goal)
	}

	var total float64
	for i := 1; i < len(s.rawPath); i++ {
		total += dist(s.rawPath[i-1], s.rawPath[i])
	}
	return total, true
}

func navSiftDown(s []astarNode, heapPos []int, i int) {
	n := len(s)
	for {
		l := 2*i + 1
		if l >= n {
			break
		}
		best := l
		if r := l + 1; r < n && astarLess(s[r], s[l]) {
			best = r
		}
		if !astarLess(s[best], s[i]) {
			break
		}
		s[i], s[best] = s[best], s[i]
		heapPos[s[i].id] = i
		heapPos[s[best].id] = best
		i = best
	}
}
