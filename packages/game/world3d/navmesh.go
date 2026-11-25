package world3d

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/math3d"
)

const (
	defaultVerticalTolerance = 2.0
	sharedEdgeEps            = 1e-3
	minPortalWidth           = 1e-4
)

// NavLink describes a traversable 3D portal edge from one NavPoly to an adjacent
// NavPoly. Left and Right are ordered as seen when looking from the source
// polygon outward across the shared edge into Neighbor.
type NavLink struct {
	Neighbor int
	Left     math3d.Vec3
	Right    math3d.Vec3
}

// NavPoly is a single walkable convex 3D polygon in a NavMesh.
// Vertices are normalized to counter-clockwise winding when viewed from +Y so
// Normal always points upward (Normal.Y > 0).
type NavPoly struct {
	Vertices []math3d.Vec3
	Normal   math3d.Vec3
	PlaneD   float32
	Center   math3d.Vec3
	Bounds   math3d.AABB
	Links    []NavLink
}

// HeightAt returns the Y coordinate of the polygon's supporting 3D plane at (x, z).
func (p *NavPoly) HeightAt(x, z float32) float32 {
	if p == nil || math.Abs(float64(p.Normal.Y)) < 1e-7 {
		return 0
	}
	return -(p.Normal.X*x + p.Normal.Z*z + p.PlaneD) / p.Normal.Y
}

// ContainsXZ reports whether (x, z) lies inside or on the boundary of p's XZ projection.
func (p *NavPoly) ContainsXZ(x, z float32) bool {
	if p == nil || len(p.Vertices) < 3 {
		return false
	}
	const pad = 1e-4
	if x < p.Bounds.Min.X-pad || x > p.Bounds.Max.X+pad || z < p.Bounds.Min.Z-pad || z > p.Bounds.Max.Z+pad {
		return false
	}
	pt := math3d.Vec3{X: x, Z: z}
	n := len(p.Vertices)
	for i := range n {
		a := p.Vertices[i]
		b := p.Vertices[(i+1)%n]
		dx := b.X - a.X
		dz := b.Z - a.Z
		edgeLen := float32(math.Sqrt(float64(dx*dx + dz*dz)))
		// For CCW polygons viewed from +Y, interior points satisfy triArea2(a, b, pt) <= 0.
		if triArea2(a, b, pt) > 1e-4*edgeLen+1e-6 {
			return false
		}
	}
	return true
}

// NavMesh is a 3D navigation mesh built from convex polygons. It supports sloped
// ramps, obstacles, and vertically stacked walkable levels (bridges, overpasses,
// multi-story floors) linked strictly along shared 3D edges.
type NavMesh struct {
	Polys             []NavPoly
	VerticalTolerance float32
}

// BuildNavMesh constructs a NavMesh from convex 3D polygons, filtering out
// degenerate polygons and slopes steeper than maxSlopeRad radians from
// horizontal (upward normal angle from +Y). Adjacent walkable polygons are
// linked automatically along shared 3D edges (including collinear T-junction
// overlaps) so vertically separated levels never falsely connect.
//
// If maxSlopeRad < 0, a default limit of 45 degrees (pi/4) is used.
func BuildNavMesh(polys [][]math3d.Vec3, maxSlopeRad float32) *NavMesh {
	if maxSlopeRad < 0 {
		maxSlopeRad = math.Pi / 4
	}
	minNormalY := float32(math.Cos(float64(maxSlopeRad))) - 1e-5

	nm := &NavMesh{
		VerticalTolerance: defaultVerticalTolerance,
	}

	for _, raw := range polys {
		if len(raw) < 3 {
			continue
		}
		verts := make([]math3d.Vec3, len(raw))
		copy(verts, raw)

		normal := newellNormal(verts)
		if normal.LenSq() < 1e-10 {
			continue
		}
		normal = normal.Norm()

		// Normalize polygon winding to CCW when viewed from +Y.
		if normal.Y < 0 {
			reverseVec3(verts)
			normal = normal.Neg()
		}

		// Filter unwalkable steep surfaces (walls, cliffs, overhangs).
		if normal.Y < minNormalY || normal.Y < 1e-5 {
			continue
		}

		bounds := math3d.AABBEmpty()
		var sum math3d.Vec3
		for _, v := range verts {
			bounds = bounds.ExpandPoint(v)
			sum = sum.Add(v)
		}

		nm.Polys = append(nm.Polys, NavPoly{
			Vertices: verts,
			Normal:   normal,
			PlaneD:   -normal.Dot(verts[0]),
			Center:   sum.Scale(1 / float32(len(verts))),
			Bounds:   bounds,
		})
	}

	nm.linkPolygons()
	return nm
}

func (nm *NavMesh) linkPolygons() {
	n := len(nm.Polys)
	pad := math3d.Vec3{X: sharedEdgeEps, Y: sharedEdgeEps, Z: sharedEdgeEps}

	for i := range n {
		pi := &nm.Polys[i]
		bi := math3d.AABB{Min: pi.Bounds.Min.Sub(pad), Max: pi.Bounds.Max.Add(pad)}
		for j := i + 1; j < n; j++ {
			pj := &nm.Polys[j]
			if !bi.Intersects(pj.Bounds) {
				continue
			}
			left, right, ok := findSharedPortal(pi.Vertices, pj.Vertices)
			if !ok {
				continue
			}
			pi.Links = append(pi.Links, NavLink{
				Neighbor: j,
				Left:     left,
				Right:    right,
			})
			pj.Links = append(pj.Links, NavLink{
				Neighbor: i,
				Left:     right,
				Right:    left,
			})
		}
	}
}

// findSharedPortal checks whether two CCW convex polygons share a 3D edge (or
// collinear overlapping sub-edge) with opposing edge directions. On success,
// left and right are oriented for crossing from vertsA into vertsB.
func findSharedPortal(vertsA, vertsB []math3d.Vec3) (left, right math3d.Vec3, ok bool) {
	na := len(vertsA)
	nb := len(vertsB)
	for ia := range na {
		a0 := vertsA[ia]
		a1 := vertsA[(ia+1)%na]
		dA := a1.Sub(a0)
		lenA := dA.Len()
		if lenA < minPortalWidth {
			continue
		}
		u := dA.Scale(1 / lenA)

		for ib := range nb {
			b0 := vertsB[ib]
			b1 := vertsB[(ib+1)%nb]
			dB := b1.Sub(b0)
			if dB.LenSq() < minPortalWidth*minPortalWidth {
				continue
			}
			// Adjacent CCW polygons always traverse their shared edge in opposite directions.
			if dA.Dot(dB) >= 0 {
				continue
			}

			// Verify both endpoints of B lie on the 3D line of A within sharedEdgeEps.
			v0 := b0.Sub(a0)
			t0 := v0.Dot(u)
			if v0.Sub(u.Scale(t0)).Len() > sharedEdgeEps {
				continue
			}
			v1 := b1.Sub(a0)
			t1 := v1.Dot(u)
			if v1.Sub(u.Scale(t1)).Len() > sharedEdgeEps {
				continue
			}

			lo := max(float32(0), min(t0, t1))
			hi := min(lenA, max(t0, t1))
			if hi-lo <= minPortalWidth {
				continue
			}

			// Looking outward across CCW edge a0 -> a1 from inside A:
			// a0 (smaller parameter) is on the right, a1 (larger parameter) is on the left.
			right = a0.Add(u.Scale(lo))
			left = a0.Add(u.Scale(hi))
			return left, right, true
		}
	}
	return math3d.Vec3{}, math3d.Vec3{}, false
}

// FindPolygon returns the index of the walkable polygon containing (p.X, p.Z)
// whose 3D surface height is closest to p.Y within VerticalTolerance.
//
// Stacked levels (such as a bridge above a ground floor) remain cleanly
// separated because the polygon minimizing |p.Y - poly.HeightAt(p.X, p.Z)| is
// chosen. Returns (-1, false) if no polygon matches.
func (nm *NavMesh) FindPolygon(p math3d.Vec3) (int, bool) {
	if nm == nil || len(nm.Polys) == 0 {
		return -1, false
	}
	tol := nm.VerticalTolerance
	if tol <= 0 {
		tol = defaultVerticalTolerance
	}
	bestIdx := -1
	bestDY := tol + 1
	for i := range nm.Polys {
		poly := &nm.Polys[i]
		if !poly.ContainsXZ(p.X, p.Z) {
			continue
		}
		dy := float32(math.Abs(float64(p.Y - poly.HeightAt(p.X, p.Z))))
		if dy <= tol && dy < bestDY {
			bestDY = dy
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return -1, false
	}
	return bestIdx, true
}

// NavPathResult is the outcome of a bounded or incremental 3D NavMesh search.
type NavPathResult struct {
	Path       []math3d.Vec3
	Found      bool // true when a complete or partial path was produced
	Partial    bool // true when Path ends at the closest reachable 3D point rather than goal
	Exhausted  bool // true when the search stopped because MaxExpansions was reached
	Expansions int  // total number of polygon nodes expanded during the search
}

// FindPath computes a shortest 3D path from start to goal across the NavMesh
// using 3D portal-graph A* followed by XZ-projected funnel smoothing with exact
// 3D height interpolation across sloped ramps and multi-level bridges.
//
// Returns (nil, false) if either endpoint is off the NavMesh or no connected
// corridor exists between them.
func (nm *NavMesh) FindPath(start, goal math3d.Vec3) ([]math3d.Vec3, bool) {
	res := nm.FindPathWithOptions(start, goal, ai.PathOptions{})
	if !res.Found || res.Partial {
		return nil, false
	}
	return res.Path, true
}

// FindPathWithOptions computes a 3D path from start to goal under opts, supporting
// an expansion budget and optional partial paths ending at the closest reachable 3D point
// on the NavMesh when goal is off-mesh, disconnected, or beyond MaxExpansions.
func (nm *NavMesh) FindPathWithOptions(start, goal math3d.Vec3, opts ai.PathOptions) NavPathResult {
	s := NewNavMeshSearch(nm, start, goal, opts)
	s.step(-1)
	return s.Result()
}

// StartSearch begins an incremental 3D NavMesh search from start to goal under opts.
func (nm *NavMesh) StartSearch(start, goal math3d.Vec3, opts ai.PathOptions) *NavMeshSearch {
	return NewNavMeshSearch(nm, start, goal, opts)
}

var _ ai.IncrementalSearch = (*NavMeshSearch)(nil)

// NavMeshSearch runs a deterministic 3D NavMesh A* search incrementally across bounded Step calls.
//
// Stepping a search with any positive per-step expansion budget produces the exact same path,
// partial flag, and expansion count as NavMesh.FindPathWithOptions.
// *NavMeshSearch implements ai.IncrementalSearch so it can be scheduled directly on ai.PathQueue.
type NavMeshSearch struct {
	nm        *NavMesh
	start     math3d.Vec3
	goal      math3d.Vec3
	startPoly int
	goalPoly  int
	opts      ai.PathOptions

	gScore     []float32
	parent     []int
	parentLink []NavLink
	entryPos   []math3d.Vec3
	closed     []bool
	heapPos    []int
	open       []astarNode

	status     ai.SearchStatus
	exhausted  bool
	expansions int
	samePoly   bool
	hasBest    bool
	bestPoly   int
	bestPt     math3d.Vec3
	bestH      float32
	bestG      float32
}

// NewNavMeshSearch initializes an incremental 3D NavMesh search from start to goal under opts.
func NewNavMeshSearch(nm *NavMesh, start, goal math3d.Vec3, opts ai.PathOptions) *NavMeshSearch {
	s := &NavMeshSearch{
		nm:        nm,
		start:     start,
		goal:      goal,
		startPoly: -1,
		goalPoly:  -1,
		opts:      opts,
		status:    ai.SearchFailed,
		bestPoly:  -1,
	}
	if nm == nil || len(nm.Polys) == 0 {
		return s
	}

	startPoly, ok := nm.FindPolygon(start)
	if !ok {
		return s
	}
	start.Y = nm.Polys[startPoly].HeightAt(start.X, start.Z)
	s.start = start
	s.startPoly = startPoly

	goalPoly, goalOK := nm.FindPolygon(goal)
	if goalOK {
		goal.Y = nm.Polys[goalPoly].HeightAt(goal.X, goal.Z)
		s.goal = goal
		s.goalPoly = goalPoly
		if startPoly == goalPoly {
			s.samePoly = true
			s.status = ai.SearchComplete
			return s
		}
	} else if !opts.AllowPartial {
		return s
	}

	n := len(nm.Polys)
	inf := float32(math.Inf(1))
	s.gScore = make([]float32, n)
	s.parent = make([]int, n)
	s.parentLink = make([]NavLink, n)
	s.entryPos = make([]math3d.Vec3, n)
	s.closed = make([]bool, n)
	s.heapPos = make([]int, n)
	for i := range n {
		s.gScore[i] = inf
		s.parent[i] = -1
		s.heapPos[i] = -1
	}

	s.gScore[startPoly] = 0
	s.entryPos[startPoly] = start
	if opts.AllowPartial {
		cp0 := closestPointOnPoly3D(&nm.Polys[startPoly], s.goal)
		s.hasBest = true
		s.bestPoly = startPoly
		s.bestPt = cp0
		s.bestH = cp0.Dist(s.goal)
		s.bestG = start.Dist(cp0)
	}

	h0 := start.Dist(s.goal)
	s.open = make([]astarNode, 0, min(n, 64))
	s.pushOrUpdate(startPoly, h0, h0)
	s.status = ai.SearchInProgress
	return s
}

// Step advances the search by at most maxExpansions polygon expansions and returns
// the updated status and exact number of expansions performed.
// When maxExpansions <= 0 or the search is already finished, Step performs 0 expansions.
func (s *NavMeshSearch) Step(maxExpansions int) (status ai.SearchStatus, expansions int) {
	if s == nil {
		return ai.SearchFailed, 0
	}
	if maxExpansions <= 0 || s.status.Done() {
		return s.status, 0
	}
	return s.step(maxExpansions)
}

func (s *NavMeshSearch) step(stepLimit int) (ai.SearchStatus, int) {
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

	nm := s.nm
	goal := s.goal
	done := 0

	for len(s.open) > 0 && (budget < 0 || done < budget) {
		curr := s.heapPop()
		done++
		s.expansions++
		u := curr.poly

		if s.goalPoly >= 0 && u == s.goalPoly {
			s.status = ai.SearchComplete
			return s.status, done
		}
		s.closed[u] = true

		uPos := s.entryPos[u]
		if s.opts.AllowPartial {
			cp := closestPointOnPoly3D(&nm.Polys[u], goal)
			h := cp.Dist(goal)
			g := s.gScore[u] + uPos.Dist(cp)
			if !s.hasBest || h < s.bestH || (h == s.bestH && (g < s.bestG || (g == s.bestG && u < s.bestPoly))) {
				s.hasBest = true
				s.bestPoly = u
				s.bestPt = cp
				s.bestH = h
				s.bestG = g
			}
		}

		for _, link := range nm.Polys[u].Links {
			v := link.Neighbor
			if s.closed[v] {
				continue
			}
			mid := link.Left.Lerp(link.Right, 0.5)
			tentativeG := s.gScore[u] + uPos.Dist(mid)
			if tentativeG < s.gScore[v] {
				s.gScore[v] = tentativeG
				s.parent[v] = u
				s.parentLink[v] = link
				s.entryPos[v] = mid
				h := mid.Dist(goal)
				s.pushOrUpdate(v, tentativeG+h, h)
			}
		}
	}

	if len(s.open) == 0 {
		s.exhausted = false
		if s.opts.AllowPartial && s.hasBest {
			s.status = ai.SearchPartial
		} else {
			s.status = ai.SearchFailed
		}
	} else if s.opts.MaxExpansions > 0 && s.expansions >= s.opts.MaxExpansions {
		s.finishExhausted()
	}

	return s.status, done
}

func (s *NavMeshSearch) finishExhausted() {
	s.exhausted = true
	if s.opts.AllowPartial && s.hasBest {
		s.status = ai.SearchPartial
	} else {
		s.status = ai.SearchFailed
	}
}

// Status returns the search's current status.
func (s *NavMeshSearch) Status() ai.SearchStatus {
	if s == nil {
		return ai.SearchFailed
	}
	return s.status
}

// Expansions returns the cumulative number of polygon nodes expanded so far.
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
	case ai.SearchComplete:
		res.Found = true
		if s.samePoly {
			if s.start.ApproxEqual(s.goal, 1e-5) {
				res.Path = []math3d.Vec3{s.start}
			} else {
				res.Path = []math3d.Vec3{s.start, s.goal}
			}
			return res
		}
		corridor, portals, ok := reconstructCorridor(s.startPoly, s.goalPoly, s.parent, s.parentLink)
		if !ok {
			res.Found = false
			return res
		}
		res.Path = s.nm.smoothCorridor(corridor, portals, s.start, s.goal)
		return res

	case ai.SearchPartial:
		res.Found = true
		res.Partial = true
		if s.bestPoly == s.startPoly {
			if s.start.ApproxEqual(s.bestPt, 1e-5) {
				res.Path = []math3d.Vec3{s.start}
			} else {
				res.Path = []math3d.Vec3{s.start, s.bestPt}
			}
			return res
		}
		corridor, portals, ok := reconstructCorridor(s.startPoly, s.bestPoly, s.parent, s.parentLink)
		if !ok {
			res.Found = false
			res.Partial = false
			return res
		}
		res.Path = s.nm.smoothCorridor(corridor, portals, s.start, s.bestPt)
		return res

	default:
		return res
	}
}

type astarNode struct {
	poly int
	f    float32
	h    float32
}

// navNodeLess defines a strict total order for deterministic tie-breaking: lowest f, then lowest h, then lowest polygon index.
func navNodeLess(a, b astarNode) bool {
	if a.f != b.f {
		return a.f < b.f
	}
	if a.h != b.h {
		return a.h < b.h
	}
	return a.poly < b.poly
}

func (s *NavMeshSearch) pushOrUpdate(poly int, f, h float32) {
	if pos := s.heapPos[poly]; pos >= 0 {
		s.open[pos].f = f
		s.open[pos].h = h
		s.siftUp(pos)
		return
	}
	pos := len(s.open)
	s.open = append(s.open, astarNode{poly: poly, f: f, h: h})
	s.heapPos[poly] = pos
	s.siftUp(pos)
}

func (s *NavMeshSearch) heapPop() astarNode {
	top := s.open[0]
	s.heapPos[top.poly] = -1
	last := len(s.open) - 1
	if last == 0 {
		s.open = s.open[:0]
		return top
	}
	s.open[0] = s.open[last]
	s.heapPos[s.open[0].poly] = 0
	s.open = s.open[:last]
	s.siftDown(0)
	return top
}

func (s *NavMeshSearch) siftUp(i int) {
	for i > 0 {
		p := (i - 1) / 2
		if !navNodeLess(s.open[i], s.open[p]) {
			break
		}
		s.open[i], s.open[p] = s.open[p], s.open[i]
		s.heapPos[s.open[i].poly] = i
		s.heapPos[s.open[p].poly] = p
		i = p
	}
}

func (s *NavMeshSearch) siftDown(i int) {
	n := len(s.open)
	for {
		l := 2*i + 1
		if l >= n {
			break
		}
		best := l
		if r := l + 1; r < n && navNodeLess(s.open[r], s.open[l]) {
			best = r
		}
		if !navNodeLess(s.open[best], s.open[i]) {
			break
		}
		s.open[i], s.open[best] = s.open[best], s.open[i]
		s.heapPos[s.open[i].poly] = i
		s.heapPos[s.open[best].poly] = best
		i = best
	}
}

func closestPointOnPoly3D(poly *NavPoly, p math3d.Vec3) math3d.Vec3 {
	// Orthogonal projection of p onto the polygon's supporting plane.
	signedDist := poly.Normal.Dot(p) + poly.PlaneD
	proj := p.Sub(poly.Normal.Scale(signedDist))
	if poly.ContainsXZ(proj.X, proj.Z) {
		proj.Y = poly.HeightAt(proj.X, proj.Z)
		return proj
	}
	vs := poly.Vertices
	n := len(vs)
	bestDistSq := float32(math.Inf(1))
	var bestPt math3d.Vec3
	for k := range n {
		cp := closestPointOnSegment3D(p, vs[k], vs[(k+1)%n])
		d2 := p.Sub(cp).LenSq()
		if d2 < bestDistSq {
			bestDistSq = d2
			bestPt = cp
		}
	}
	return bestPt
}

func closestPointOnSegment3D(p, a, b math3d.Vec3) math3d.Vec3 {
	ab := b.Sub(a)
	l2 := ab.LenSq()
	if l2 <= 1e-12 {
		return a
	}
	t := p.Sub(a).Dot(ab) / l2
	t = math3d.Clamp(t, 0, 1)
	return a.Add(ab.Scale(t))
}

func reconstructCorridor(startPoly, goalPoly int, parent []int, parentLink []NavLink) ([]int, []NavLink, bool) {
	var revPolys []int
	var revLinks []NavLink

	cur := goalPoly
	for cur != startPoly {
		p := parent[cur]
		if p < 0 {
			return nil, nil, false
		}
		revPolys = append(revPolys, cur)
		revLinks = append(revLinks, parentLink[cur])
		cur = p
	}
	revPolys = append(revPolys, startPoly)

	// Reverse into start -> goal order.
	corridor := make([]int, len(revPolys))
	for i := range revPolys {
		corridor[i] = revPolys[len(revPolys)-1-i]
	}
	portals := make([]NavLink, len(revLinks))
	for i := range revLinks {
		portals[i] = revLinks[len(revLinks)-1-i]
	}
	return corridor, portals, true
}

type funnelCorner struct {
	pos       math3d.Vec3
	portalIdx int
}

// smoothCorridor runs the Simple Stupid Funnel Algorithm in XZ across the
// ordered 3D portal corridor, then inserts exact 3D portal-edge crossings
// wherever consecutive polygons change plane/slope so the path conforms
// tightly to ramps, ridges, and multi-level bridges.
func (nm *NavMesh) smoothCorridor(corridor []int, portals []NavLink, start, goal math3d.Vec3) []math3d.Vec3 {
	m := len(portals) + 2
	portalLeft := make([]math3d.Vec3, m)
	portalRight := make([]math3d.Vec3, m)

	portalLeft[0] = start
	portalRight[0] = start
	for i, p := range portals {
		portalLeft[i+1] = p.Left
		portalRight[i+1] = p.Right
	}
	portalLeft[m-1] = goal
	portalRight[m-1] = goal

	corners := []funnelCorner{{pos: start, portalIdx: 0}}

	portalApex := start
	leftPt := portalLeft[0]
	rightPt := portalRight[0]
	apexIdx, leftIdx, rightIdx := 0, 0, 0

	for i := 1; i < m; i++ {
		left := portalLeft[i]
		right := portalRight[i]

		// Update right funnel boundary.
		if triArea2(portalApex, rightPt, right) <= 0 {
			if sameXZ(portalApex, rightPt) || sameXZ(portalApex, leftPt) || triArea2(portalApex, leftPt, right) > 0 {
				rightPt = right
				rightIdx = i
			} else {
				// Right crossed over left: commit leftPt as a corner.
				corners = appendCorner(corners, funnelCorner{pos: leftPt, portalIdx: leftIdx})
				portalApex = leftPt
				apexIdx = leftIdx
				leftPt = portalApex
				rightPt = portalApex
				leftIdx = apexIdx
				rightIdx = apexIdx
				i = apexIdx
				continue
			}
		}

		// Update left funnel boundary.
		if triArea2(portalApex, leftPt, left) >= 0 {
			if sameXZ(portalApex, leftPt) || sameXZ(portalApex, rightPt) || triArea2(portalApex, rightPt, left) < 0 {
				leftPt = left
				leftIdx = i
			} else {
				// Left crossed over right: commit rightPt as a corner.
				corners = appendCorner(corners, funnelCorner{pos: rightPt, portalIdx: rightIdx})
				portalApex = rightPt
				apexIdx = rightIdx
				leftPt = portalApex
				rightPt = portalApex
				leftIdx = apexIdx
				rightIdx = apexIdx
				i = apexIdx
				continue
			}
		}
	}

	corners = appendCorner(corners, funnelCorner{pos: goal, portalIdx: m - 1})

	// Insert exact 3D portal crossings where consecutive polygons have different
	// planes (e.g. floor -> ramp -> bridge) so Y follows the 3D surface exactly.
	out := make([]math3d.Vec3, 0, len(corners)+len(portals))
	out = append(out, corners[0].pos)

	for c := 1; c < len(corners); c++ {
		prev := corners[c-1]
		next := corners[c]
		for j := prev.portalIdx + 1; j < next.portalIdx; j++ {
			pA := &nm.Polys[corridor[j-1]]
			pB := &nm.Polys[corridor[j]]
			if samePlane(pA, pB) {
				continue
			}
			link := portals[j-1]
			s := intersectXZPortalParam(prev.pos, next.pos, link.Left, link.Right)
			cross := link.Left.Lerp(link.Right, s)
			if !out[len(out)-1].ApproxEqual(cross, 1e-4) {
				out = append(out, cross)
			}
		}
		if !out[len(out)-1].ApproxEqual(next.pos, 1e-4) {
			out = append(out, next.pos)
		}
	}

	return out
}

func appendCorner(corners []funnelCorner, c funnelCorner) []funnelCorner {
	last := corners[len(corners)-1]
	if last.pos.ApproxEqual(c.pos, 1e-4) {
		if c.portalIdx > last.portalIdx {
			corners[len(corners)-1].portalIdx = c.portalIdx
		}
		return corners
	}
	return append(corners, c)
}

func samePlane(a, b *NavPoly) bool {
	return a.Normal.Dot(b.Normal) >= 1-1e-4 && math.Abs(float64(a.PlaneD-b.PlaneD)) <= 1e-3
}

// intersectXZPortalParam returns parameter s in [0, 1] where the XZ line
// segment a0->a1 intersects the portal edge b0->b1.
func intersectXZPortalParam(a0, a1, b0, b1 math3d.Vec3) float32 {
	daX, daZ := a1.X-a0.X, a1.Z-a0.Z
	dbX, dbZ := b1.X-b0.X, b1.Z-b0.Z
	denom := daX*dbZ - daZ*dbX
	if math.Abs(float64(denom)) < 1e-7 {
		// Segment is nearly parallel to portal: project midpoint onto portal.
		midX := 0.5 * (a0.X + a1.X)
		midZ := 0.5 * (a0.Z + a1.Z)
		lenSq := dbX*dbX + dbZ*dbZ
		if lenSq < 1e-12 {
			return 0.5
		}
		s := ((midX-b0.X)*dbX + (midZ-b0.Z)*dbZ) / lenSq
		return math3d.Clamp(s, 0, 1)
	}
	num := (b0.X-a0.X)*daZ - (b0.Z-a0.Z)*daX
	return math3d.Clamp(num/denom, 0, 1)
}

// triArea2 returns twice the signed area of triangle (a, b, c) projected onto XZ.
func triArea2(a, b, c math3d.Vec3) float32 {
	return (b.X-a.X)*(c.Z-a.Z) - (b.Z-a.Z)*(c.X-a.X)
}

func sameXZ(a, b math3d.Vec3) bool {
	dx := a.X - b.X
	dz := a.Z - b.Z
	return dx*dx+dz*dz < 1e-10
}

func newellNormal(verts []math3d.Vec3) math3d.Vec3 {
	var n math3d.Vec3
	count := len(verts)
	for i := range count {
		a := verts[i]
		b := verts[(i+1)%count]
		n.X += (a.Y - b.Y) * (a.Z + b.Z)
		n.Y += (a.Z - b.Z) * (a.X + b.X)
		n.Z += (a.X - b.X) * (a.Y + b.Y)
	}
	return n
}

func reverseVec3(s []math3d.Vec3) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
