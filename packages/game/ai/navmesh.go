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
func (a vec2) dot(b vec2) float64   { return a.x*b.x + a.y*b.y }
func (a vec2) cross(b vec2) float64 { return a.x*b.y - a.y*b.x }
func (a vec2) lenSq() float64       { return a.x*a.x + a.y*a.y }
func (a vec2) len() float64         { return math.Hypot(a.x, a.y) }

func dist(a, b vec2) float64   { return math.Hypot(a.x-b.x, a.y-b.y) }
func distSq(a, b vec2) float64 { dx, dy := a.x-b.x, a.y-b.y; return dx*dx + dy*dy }
func vequal(a, b vec2) bool    { return distSq(a, b) <= navEps*navEps }

// triArea2 returns twice the signed area of triangle (a, b, c): positive when a->b->c turns counter-clockwise.
func triArea2(a, b, c vec2) float64 {
	return (b.x-a.x)*(c.y-a.y) - (b.y-a.y)*(c.x-a.x)
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
	outPortals             []int
}

// navPortal is a directed shared-edge crossing from fromPoly to toPoly.
// Looking from inside fromPoly across the portal into toPoly, left is on the counter-clockwise side and right is on the clockwise side so triArea2(apex, right, left) >= 0.
type navPortal struct {
	fromPoly int
	toPoly   int
	left     vec2
	right    vec2
}

// NavMesh is a 2D navigation mesh built from convex polygons connected across shared edges.
type NavMesh struct {
	polys   []navPoly
	portals []navPortal
}

// BuildNavMesh constructs a NavMesh from convex 2D polygons, normalizing winding order and automatically connecting adjacent polygons along shared edges (including partial collinear overlaps).
//
// Degenerate polygons with fewer than three vertices or near-zero area are skipped.
func BuildNavMesh(polys [][]core.Point) *NavMesh {
	m := &NavMesh{}
	for _, raw := range polys {
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
		for _, v := range verts[1:] {
			minX = min(minX, v.x)
			minY = min(minY, v.y)
			maxX = max(maxX, v.x)
			maxY = max(maxY, v.y)
		}
		m.polys = append(m.polys, navPoly{
			verts: verts,
			minX:  minX,
			minY:  minY,
			maxX:  maxX,
			maxY:  maxY,
		})
	}

	// Detect shared edges between every pair of polygons.
	for i := 0; i < len(m.polys); i++ {
		pi := &m.polys[i]
		for j := i + 1; j < len(m.polys); j++ {
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

// FindPolygon returns the lowest index of a polygon containing p (including its edges and vertices), or -1 if p lies outside the mesh.
func (m *NavMesh) FindPolygon(p core.Point) int {
	if m == nil {
		return -1
	}
	v := toVec2(p)
	for i := range m.polys {
		if polyContains(&m.polys[i], v) {
			return i
		}
	}
	return -1
}

// Contains reports whether p lies inside or on the boundary of any polygon in the mesh.
func (m *NavMesh) Contains(p core.Point) bool {
	return m.FindPolygon(p) >= 0
}

// NearestPolygon returns the polygon closest to p and the closest point on or inside that polygon.
// When p lies inside a polygon, NearestPolygon returns that polygon and p itself.
func (m *NavMesh) NearestPolygon(p core.Point) (index int, point core.Point, ok bool) {
	if m == nil || len(m.polys) == 0 {
		return -1, core.Point{}, false
	}
	if idx := m.FindPolygon(p); idx >= 0 {
		return idx, p, true
	}
	v := toVec2(p)
	bestIdx := -1
	bestDistSq := math.Inf(1)
	var bestPt vec2
	for i := range m.polys {
		vs := m.polys[i].verts
		n := len(vs)
		for k := range n {
			cp := closestPointOnSegment(v, vs[k], vs[(k+1)%n])
			d2 := distSq(v, cp)
			if d2 < bestDistSq {
				bestDistSq = d2
				bestIdx = i
				bestPt = cp
			}
		}
	}
	if bestIdx < 0 {
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

// FindPath finds the shortest path inside the navigation mesh from start to goal using portal-graph A* followed by Simple Stupid Funnel Algorithm string-pulling.
//
// It returns (nil, false) if start or goal lies outside the mesh or in disconnected regions.
func (m *NavMesh) FindPath(start, goal core.Point) ([]core.Point, bool) {
	if m == nil || len(m.polys) == 0 {
		return nil, false
	}
	sVec := toVec2(start)
	gVec := toVec2(goal)

	var startPolys []int
	isGoalPoly := make([]bool, len(m.polys))
	hasGoal := false
	for i := range m.polys {
		inStart := polyContains(&m.polys[i], sVec)
		inGoal := polyContains(&m.polys[i], gVec)
		if inStart && inGoal {
			if vequal(sVec, gVec) {
				return []core.Point{start}, true
			}
			return []core.Point{start, goal}, true
		}
		if inStart {
			startPolys = append(startPolys, i)
		}
		if inGoal {
			isGoalPoly[i] = true
			hasGoal = true
		}
	}
	if len(startPolys) == 0 || !hasGoal {
		return nil, false
	}

	// Portal-graph A* over directed portals plus one virtual goal node.
	goalNode := len(m.portals)
	nodeCount := goalNode + 1

	gScore := make([]float64, nodeCount)
	for i := range gScore {
		gScore[i] = math.Inf(1)
	}
	parent := make([]int, nodeCount)
	for i := range parent {
		parent[i] = -1
	}
	entryPt := make([]vec2, nodeCount)
	closed := make([]bool, nodeCount)
	heapPos := make([]int, nodeCount)
	for i := range heapPos {
		heapPos[i] = -1
	}
	open := make([]astarNode, 0, min(nodeCount, 64))

	pushOrUpdate := func(id int, f, h float64) {
		if pos := heapPos[id]; pos >= 0 {
			open[pos].f = f
			open[pos].h = h
			navSiftUp(open, heapPos, pos)
			return
		}
		pos := len(open)
		open = append(open, astarNode{id: id, f: f, h: h})
		heapPos[id] = pos
		navSiftUp(open, heapPos, pos)
	}

	for _, sp := range startPolys {
		for _, pid := range m.polys[sp].outPortals {
			p := m.portals[pid]
			pt := bestPortalPoint(sVec, gVec, p.left, p.right)
			g := dist(sVec, pt)
			if g < gScore[pid] {
				gScore[pid] = g
				entryPt[pid] = pt
				parent[pid] = -1
				h := dist(pt, gVec)
				pushOrUpdate(pid, g+h, h)
			}
		}
	}

	found := false
	for len(open) > 0 {
		cur := navHeapPop(&open, heapPos)
		if cur.id == goalNode {
			found = true
			break
		}
		closed[cur.id] = true

		portal := m.portals[cur.id]
		curG := gScore[cur.id]
		curPt := entryPt[cur.id]

		if isGoalPoly[portal.toPoly] {
			ng := curG + dist(curPt, gVec)
			if ng < gScore[goalNode] {
				gScore[goalNode] = ng
				parent[goalNode] = cur.id
				pushOrUpdate(goalNode, ng, 0)
			}
		}

		for _, npid := range m.polys[portal.toPoly].outPortals {
			if closed[npid] {
				continue
			}
			np := m.portals[npid]
			if np.toPoly == portal.fromPoly {
				continue
			}
			pt := bestPortalPoint(curPt, gVec, np.left, np.right)
			ng := curG + dist(curPt, pt)
			if ng < gScore[npid] {
				gScore[npid] = ng
				entryPt[npid] = pt
				parent[npid] = cur.id
				h := dist(pt, gVec)
				pushOrUpdate(npid, ng+h, h)
			}
		}
	}

	if !found {
		return nil, false
	}

	var portalIDs []int
	for id := parent[goalNode]; id != -1; id = parent[id] {
		portalIDs = append(portalIDs, id)
	}
	slices.Reverse(portalIDs)

	rawPath := m.stringPull(sVec, gVec, portalIDs)
	clean := simplifyCollinear(rawPath)
	out := make([]core.Point, len(clean))
	for i, v := range clean {
		out[i] = toPoint(v)
	}
	return out, true
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

// stringPull runs the Simple Stupid Funnel Algorithm across portalIDs from start to goal.
func (m *NavMesh) stringPull(start, goal vec2, portalIDs []int) []vec2 {
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
