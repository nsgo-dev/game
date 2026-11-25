package ai

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/random"
	"github.com/nsgo-dev/nsgo/core"
)

const navBVHLeafSize = 4

// navBVHNode2D is a flat, pointer-free 2D AABB bounding-volume hierarchy node.
// When count > 0, the node is a leaf covering bvhOrder[left : left+count].
// When count == 0, left and right are child node indices in bvhNodes.
type navBVHNode2D struct {
	minX, minY, maxX, maxY float64
	left                   int32
	right                  int32
	count                  int32
}

func (m *NavMesh) buildBVH() {
	n := len(m.polys)
	if n == 0 {
		m.bvhNodes = nil
		m.bvhOrder = nil
		return
	}
	m.bvhOrder = make([]int, n)
	for i := range n {
		m.bvhOrder[i] = i
	}
	m.bvhNodes = make([]navBVHNode2D, 0, max(1, (2*n)/navBVHLeafSize+4))
	m.buildBVHNode(0, n, 0)
}

func (m *NavMesh) buildBVHNode(start, end, depth int) int32 {
	nodeIdx := int32(len(m.bvhNodes))
	m.bvhNodes = append(m.bvhNodes, navBVHNode2D{})

	p0 := &m.polys[m.bvhOrder[start]]
	minX, minY, maxX, maxY := p0.minX, p0.minY, p0.maxX, p0.maxY
	cMinX, cMinY, cMaxX, cMaxY := p0.cx, p0.cy, p0.cx, p0.cy
	for i := start + 1; i < end; i++ {
		p := &m.polys[m.bvhOrder[i]]
		minX = min(minX, p.minX)
		minY = min(minY, p.minY)
		maxX = max(maxX, p.maxX)
		maxY = max(maxY, p.maxY)
		cMinX = min(cMinX, p.cx)
		cMinY = min(cMinY, p.cy)
		cMaxX = max(cMaxX, p.cx)
		cMaxY = max(cMaxY, p.cy)
	}

	count := end - start
	if count <= navBVHLeafSize || depth >= 56 {
		m.bvhNodes[nodeIdx] = navBVHNode2D{
			minX:  minX,
			minY:  minY,
			maxX:  maxX,
			maxY:  maxY,
			left:  int32(start),
			count: int32(count),
		}
		return nodeIdx
	}

	splitY := (cMaxY - cMinY) > (cMaxX - cMinX)
	mid := start + count/2
	var splitVal float64
	if splitY {
		splitVal = 0.5 * (cMinY + cMaxY)
	} else {
		splitVal = 0.5 * (cMinX + cMaxX)
	}

	// Deterministic in-place midpoint partition.
	i, j := start, end-1
	for i <= j {
		var c float64
		if splitY {
			c = m.polys[m.bvhOrder[i]].cy
		} else {
			c = m.polys[m.bvhOrder[i]].cx
		}
		if c < splitVal {
			i++
		} else {
			m.bvhOrder[i], m.bvhOrder[j] = m.bvhOrder[j], m.bvhOrder[i]
			j--
		}
	}

	leftCount := i - start
	rightCount := end - i
	// Fall back to deterministic centroid sort median split when midpoint partition is skewed.
	if leftCount == 0 || rightCount == 0 || leftCount < count/8 || rightCount < count/8 {
		sub := m.bvhOrder[start:end]
		if splitY {
			slices.SortFunc(sub, func(a, b int) int {
				if m.polys[a].cy < m.polys[b].cy {
					return -1
				}
				if m.polys[a].cy > m.polys[b].cy {
					return 1
				}
				return a - b
			})
		} else {
			slices.SortFunc(sub, func(a, b int) int {
				if m.polys[a].cx < m.polys[b].cx {
					return -1
				}
				if m.polys[a].cx > m.polys[b].cx {
					return 1
				}
				return a - b
			})
		}
		i = mid
	}

	leftIdx := m.buildBVHNode(start, i, depth+1)
	rightIdx := m.buildBVHNode(i, end, depth+1)
	m.bvhNodes[nodeIdx] = navBVHNode2D{
		minX:  minX,
		minY:  minY,
		maxX:  maxX,
		maxY:  maxY,
		left:  leftIdx,
		right: rightIdx,
	}
	return nodeIdx
}

func (m *NavMesh) bvhQueryAABB(minX, minY, maxX, maxY float64, dst []int) []int {
	if len(m.bvhNodes) == 0 {
		for i := range m.polys {
			p := &m.polys[i]
			if p.maxX >= minX && p.minX <= maxX && p.maxY >= minY && p.minY <= maxY {
				dst = append(dst, i)
			}
		}
		return dst
	}
	var stack [64]int32
	sp := 0
	stack[0] = 0
	sp = 1
	for sp > 0 {
		sp--
		n := &m.bvhNodes[stack[sp]]
		if n.maxX < minX || n.minX > maxX || n.maxY < minY || n.minY > maxY {
			continue
		}
		if n.count > 0 {
			end := int(n.left + n.count)
			for k := int(n.left); k < end; k++ {
				idx := m.bvhOrder[k]
				p := &m.polys[idx]
				if p.maxX >= minX && p.minX <= maxX && p.maxY >= minY && p.minY <= maxY {
					dst = append(dst, idx)
				}
			}
			continue
		}
		stack[sp] = n.right
		sp++
		stack[sp] = n.left
		sp++
	}
	return dst
}

// bvhFindPoly returns the lowest-index polygon containing v (optionally filtered), or -1.
func (m *NavMesh) bvhFindPoly(v vec2, filter NavFilter, useFilter bool) int {
	if len(m.bvhNodes) == 0 {
		for i := range m.polys {
			if useFilter && !filter.Allows(m.polys[i].area, m.polys[i].flags) {
				continue
			}
			if polyContains(&m.polys[i], v) {
				return i
			}
		}
		return -1
	}
	best := -1
	var stack [64]int32
	stack[0] = 0
	sp := 1
	for sp > 0 {
		sp--
		n := &m.bvhNodes[stack[sp]]
		if v.x < n.minX-portalEps || v.x > n.maxX+portalEps || v.y < n.minY-portalEps || v.y > n.maxY+portalEps {
			continue
		}
		if n.count > 0 {
			end := int(n.left + n.count)
			for k := int(n.left); k < end; k++ {
				idx := m.bvhOrder[k]
				if best >= 0 && idx >= best {
					continue
				}
				p := &m.polys[idx]
				if useFilter && !filter.Allows(p.area, p.flags) {
					continue
				}
				if polyContains(p, v) {
					best = idx
				}
			}
			continue
		}
		stack[sp] = n.right
		sp++
		stack[sp] = n.left
		sp++
	}
	return best
}

// bvhFindPolysAt appends all polygons containing v that pass filter (with positive finite cost) into dst, sorted by ascending index.
func (m *NavMesh) bvhFindPolysAt(v vec2, filter NavFilter, requireFiniteCost bool, dst []int) []int {
	dst = dst[:0]
	if len(m.bvhNodes) == 0 {
		for i := range m.polys {
			p := &m.polys[i]
			if !filter.Allows(p.area, p.flags) {
				continue
			}
			if requireFiniteCost {
				c := filter.AreaCost(p.area)
				if c <= 0 || math.IsNaN(c) || math.IsInf(c, 0) {
					continue
				}
			}
			if polyContains(p, v) {
				dst = append(dst, i)
			}
		}
		return dst
	}
	var stack [64]int32
	stack[0] = 0
	sp := 1
	for sp > 0 {
		sp--
		n := &m.bvhNodes[stack[sp]]
		if v.x < n.minX-portalEps || v.x > n.maxX+portalEps || v.y < n.minY-portalEps || v.y > n.maxY+portalEps {
			continue
		}
		if n.count > 0 {
			end := int(n.left + n.count)
			for k := int(n.left); k < end; k++ {
				idx := m.bvhOrder[k]
				p := &m.polys[idx]
				if !filter.Allows(p.area, p.flags) {
					continue
				}
				if requireFiniteCost {
					c := filter.AreaCost(p.area)
					if c <= 0 || math.IsNaN(c) || math.IsInf(c, 0) {
						continue
					}
				}
				if polyContains(p, v) {
					dst = append(dst, idx)
				}
			}
			continue
		}
		stack[sp] = n.right
		sp++
		stack[sp] = n.left
		sp++
	}
	// Insertion sort tiny result slice (typically 1..4 polygons) for ascending index order without allocations.
	for i := 1; i < len(dst); i++ {
		key := dst[i]
		j := i - 1
		for j >= 0 && dst[j] > key {
			dst[j+1] = dst[j]
			j--
		}
		dst[j+1] = key
	}
	return dst
}

func aabbDistSq2D(minX, minY, maxX, maxY float64, p vec2) float64 {
	var dx, dy float64
	if p.x < minX {
		dx = minX - p.x
	} else if p.x > maxX {
		dx = p.x - maxX
	}
	if p.y < minY {
		dy = minY - p.y
	} else if p.y > maxY {
		dy = p.y - maxY
	}
	return dx*dx + dy*dy
}

type bvhDistItem struct {
	idx    int32
	distSq float64
}

// bvhNearestPoly finds the nearest point on any allowed polygon to v using branch-and-bound BVH traversal.
// When useExtent is true, only points within [v.x-ext.x, v.x+ext.x] x [v.y-ext.y, v.y+ext.y] are accepted;
// if a polygon overlaps the extent box while its unconstrained closest point lies outside the box,
// the polygon is clipped against the extent box to find the exact closest point inside the extent.
func (m *NavMesh) bvhNearestPoly(v, ext vec2, useExtent bool, filter NavFilter) (int, vec2, bool) {
	bMinX, bMinY := v.x-ext.x-portalEps, v.y-ext.y-portalEps
	bMaxX, bMaxY := v.x+ext.x+portalEps, v.y+ext.y+portalEps
	cMinX, cMinY := v.x-ext.x, v.y-ext.y
	cMaxX, cMaxY := v.x+ext.x, v.y+ext.y

	bestIdx := -1
	bestDistSq := math.Inf(1)
	var bestPt vec2
	var clipA, clipB [16]vec2

	checkPoly := func(idx int) {
		p := &m.polys[idx]
		if !filter.Allows(p.area, p.flags) {
			return
		}
		if useExtent && (p.maxX < bMinX || p.minX > bMaxX || p.maxY < bMinY || p.minY > bMaxY) {
			return
		}
		boxD2 := aabbDistSq2D(p.minX, p.minY, p.maxX, p.maxY, v)
		if boxD2 > bestDistSq {
			return
		}
		cp := closestPointInPoly(p, v)
		if useExtent && (math.Abs(cp.x-v.x) > ext.x+portalEps || math.Abs(cp.y-v.y) > ext.y+portalEps) {
			clipped := clipPolyAABB2DAny(p.verts, cMinX, cMinY, cMaxX, cMaxY, clipA[:0], clipB[:0])
			if len(clipped) == 0 {
				return
			}
			cp = closestPointOnConvex2D(clipped, v)
		}
		d2 := distSq(v, cp)
		if d2 < bestDistSq || (d2 == bestDistSq && (bestIdx < 0 || idx < bestIdx)) {
			bestDistSq = d2
			bestIdx = idx
			bestPt = cp
		}
	}

	if len(m.bvhNodes) == 0 {
		for i := range m.polys {
			checkPoly(i)
		}
		return bestIdx, bestPt, bestIdx >= 0
	}

	var stack [64]bvhDistItem
	root := &m.bvhNodes[0]
	if useExtent && (root.maxX < bMinX || root.minX > bMaxX || root.maxY < bMinY || root.minY > bMaxY) {
		return -1, vec2{}, false
	}
	stack[0] = bvhDistItem{idx: 0, distSq: aabbDistSq2D(root.minX, root.minY, root.maxX, root.maxY, v)}
	sp := 1

	for sp > 0 {
		sp--
		item := stack[sp]
		if item.distSq > bestDistSq {
			continue
		}
		n := &m.bvhNodes[item.idx]
		if n.count > 0 {
			end := int(n.left + n.count)
			for k := int(n.left); k < end; k++ {
				checkPoly(m.bvhOrder[k])
			}
			continue
		}

		lNode := &m.bvhNodes[n.left]
		rNode := &m.bvhNodes[n.right]
		lOk := !useExtent || !(lNode.maxX < bMinX || lNode.minX > bMaxX || lNode.maxY < bMinY || lNode.minY > bMaxY)
		rOk := !useExtent || !(rNode.maxX < bMinX || rNode.minX > bMaxX || rNode.maxY < bMinY || rNode.minY > bMaxY)
		lDist := math.Inf(1)
		if lOk {
			lDist = aabbDistSq2D(lNode.minX, lNode.minY, lNode.maxX, lNode.maxY, v)
		}
		rDist := math.Inf(1)
		if rOk {
			rDist = aabbDistSq2D(rNode.minX, rNode.minY, rNode.maxX, rNode.maxY, v)
		}

		// Push farther child first so the closer child is popped and shrinks bestDistSq first.
		if lDist <= rDist {
			if rOk && rDist <= bestDistSq {
				stack[sp] = bvhDistItem{idx: n.right, distSq: rDist}
				sp++
			}
			if lOk && lDist <= bestDistSq {
				stack[sp] = bvhDistItem{idx: n.left, distSq: lDist}
				sp++
			}
		} else {
			if lOk && lDist <= bestDistSq {
				stack[sp] = bvhDistItem{idx: n.left, distSq: lDist}
				sp++
			}
			if rOk && rDist <= bestDistSq {
				stack[sp] = bvhDistItem{idx: n.right, distSq: rDist}
				sp++
			}
		}
	}

	return bestIdx, bestPt, bestIdx >= 0
}

// NearestPoint returns the closest point on any polygon allowed by DefaultNavFilter within
// the axis-aligned search half-extents [p.X-extent.X, p.X+extent.X] x [p.Y-extent.Y, p.Y+extent.Y],
// together with that polygon's index.
//
// Returns (zero, -1, false) if m is empty, inputs are invalid/negative, or no allowed polygon point lies within extent.
func (m *NavMesh) NearestPoint(p, extent core.Point) (point core.Point, poly int, ok bool) {
	return m.NearestPointWithFilter(p, extent, DefaultNavFilter())
}

// NearestPointWithFilter returns the closest point on any polygon allowed by filter within
// the axis-aligned search half-extents [p.X-extent.X, p.X+extent.X] x [p.Y-extent.Y, p.Y+extent.Y],
// together with that polygon's index.
//
// Returns (zero, -1, false) if m is empty, inputs are invalid/negative, or no allowed polygon point lies within extent.
func (m *NavMesh) NearestPointWithFilter(p, extent core.Point, filter NavFilter) (point core.Point, poly int, ok bool) {
	if m == nil || len(m.polys) == 0 {
		return core.Point{}, -1, false
	}
	v := toVec2(p)
	ext := toVec2(extent)
	if !isFiniteVec2(v) || !isFiniteVec2(ext) || ext.x < 0 || ext.y < 0 {
		return core.Point{}, -1, false
	}
	if idx := m.bvhFindPoly(v, filter, true); idx >= 0 {
		return p, idx, true
	}
	idx, pt, found := m.bvhNearestPoly(v, ext, true, filter)
	if !found {
		return core.Point{}, -1, false
	}
	return toPoint(pt), idx, true
}

// NavRaycastHit describes the result of casting a segment along a 2D NavMesh surface.
type NavRaycastHit struct {
	// Point is the impact point on the obstructing edge when Hit is true,
	// or target when the ray reaches target unobstructed (Hit is false).
	Point core.Point
	// Normal is the unit inward normal of the boundary edge that stopped the ray
	// (pointing back into the walkable polygon, opposing the ray direction), or zero when Hit is false or Poly < 0.
	Normal core.Point
	// Poly is the index of the polygon containing Point, or -1 if start lies outside the allowed mesh.
	Poly int
	// T is the normalized distance along start->target in [0, 1].
	// T is 1 when target is reached unobstructed; T is 0 when start is outside the allowed mesh.
	T float32
	// Hit is true if a mesh boundary edge or disallowed polygon obstructed the ray (or if start is outside the allowed mesh).
	Hit bool
}

// Raycast casts a segment along the mesh surface from start toward target using DefaultNavFilter,
// walking shared-edge portals across allowed polygons until reaching target or hitting a boundary edge
// or disallowed polygon.
//
// The second return value equals hit.Hit: false means unobstructed line of sight across the mesh
// (hit.T == 1, hit.Point == target, hit.Poly >= 0); true means the segment was obstructed
// (or start lies outside the allowed mesh, in which case hit.Poly == -1 and hit.T == 0).
func (m *NavMesh) Raycast(start, target core.Point) (NavRaycastHit, bool) {
	return m.RaycastWithFilter(start, target, DefaultNavFilter())
}

// RaycastWithFilter behaves like Raycast while restricting traversal to polygons allowed by filter.
func (m *NavMesh) RaycastWithFilter(start, target core.Point, filter NavFilter) (NavRaycastHit, bool) {
	sVec := toVec2(start)
	tVec := toVec2(target)
	if m == nil || len(m.polys) == 0 || !isFiniteVec2(sVec) || !isFiniteVec2(tVec) {
		hit := NavRaycastHit{Point: start, Poly: -1, T: 0, Hit: true}
		return hit, true
	}
	var startBuf [16]int
	starts := m.bvhFindPolysAt(sVec, filter, false, startBuf[:0])
	if len(starts) == 0 {
		hit := NavRaycastHit{Point: start, Poly: -1, T: 0, Hit: true}
		return hit, true
	}
	startPoly := m.pickBestRayStartPoly(starts, sVec, tVec)
	hit, _, _ := m.raycastFromPoly(startPoly, sVec, tVec, filter, nil, false)
	return hit, hit.Hit
}

func (m *NavMesh) pickBestRayStartPoly(starts []int, sVec, tVec vec2) int {
	if len(starts) == 1 || vequal(sVec, tVec) {
		return starts[0]
	}
	dir := tVec.sub(sVec)
	bestPoly := starts[0]
	bestT := -1.0
	for _, idx := range starts {
		if polyContains(&m.polys[idx], tVec) {
			return idx
		}
		tExit, _, ok := polyRayExit2D(&m.polys[idx], sVec, dir)
		if ok && tExit > bestT {
			bestT = tExit
			bestPoly = idx
		}
	}
	return bestPoly
}

// polyRayExit2D returns the smallest parameter t >= 0 where ray s + t*dir exits convex CCW polygon poly,
// together with the unit inward normal of the exited edge.
func polyRayExit2D(poly *navPoly, s, dir vec2) (tExit float64, normal vec2, ok bool) {
	dirLen := dir.len()
	if dirLen <= 1e-12 {
		return 0, vec2{}, false
	}
	vs := poly.verts
	n := len(vs)
	bestT := math.Inf(1)
	var bestN vec2
	minDenom := -1e-9 * dirLen
	for i := range n {
		a := vs[i]
		b := vs[(i+1)%n]
		edge := b.sub(a)
		eLen := edge.len()
		if eLen <= 1e-12 {
			continue
		}
		inward := vec2{-edge.y / eLen, edge.x / eLen}
		denom := dir.dot(inward)
		if denom >= minDenom {
			continue
		}
		t := a.sub(s).dot(inward) / denom
		if t < bestT {
			bestT = t
			bestN = inward
		}
	}
	if math.IsInf(bestT, 1) {
		return 0, vec2{}, false
	}
	if bestT < 0 {
		bestT = 0
	}
	return bestT, bestN, true
}

// obstructingEdgeNormal2D resolves the inward normal of an obstructing edge at exitPt,
// preferring a non-portal boundary (or disallowed neighbor) edge when a ray hits a shared corner.
func (m *NavMesh) obstructingEdgeNormal2D(poly *navPoly, s, dir, exitPt vec2, tExit float64, fallback vec2, filter NavFilter) vec2 {
	dirLen := dir.len()
	if dirLen <= 1e-12 {
		return fallback
	}
	vs := poly.verts
	n := len(vs)
	minDenom := -1e-9 * dirLen
	for i := range n {
		a := vs[i]
		b := vs[(i+1)%n]
		edge := b.sub(a)
		eLen := edge.len()
		if eLen <= 1e-12 {
			continue
		}
		inward := vec2{-edge.y / eLen, edge.x / eLen}
		denom := dir.dot(inward)
		if denom >= minDenom {
			continue
		}
		t := a.sub(s).dot(inward) / denom
		if t < 0 {
			t = 0
		}
		if math.Abs(t-tExit) > 1e-6 {
			continue
		}
		// Check whether an allowed shared-edge portal covers this edge at exitPt.
		openPortal := false
		for _, pid := range poly.outPortals {
			p := m.portals[pid]
			if p.offMesh {
				continue
			}
			np := &m.polys[p.toPoly]
			if !filter.Allows(np.area, np.flags) {
				continue
			}
			if pointOnSegment2D(exitPt, p.left, p.right, portalEps) &&
				pointOnSegment2D(p.left, a, b, portalEps) &&
				pointOnSegment2D(p.right, a, b, portalEps) {
				openPortal = true
				break
			}
		}
		if !openPortal {
			return inward
		}
	}
	return fallback
}

func pointOnSegment2D(p, a, b vec2, eps float64) bool {
	cp := closestPointOnSegment(p, a, b)
	return distSq(p, cp) <= eps*eps
}

func (m *NavMesh) raycastFromPoly(startPoly int, sVec, tVec vec2, filter NavFilter, visitedOut []int, recordVisited bool) (NavRaycastHit, vec2, []int) {
	if recordVisited && (len(visitedOut) == 0 || visitedOut[len(visitedOut)-1] != startPoly) {
		visitedOut = append(visitedOut, startPoly)
	}
	if vequal(sVec, tVec) || polyContains(&m.polys[startPoly], tVec) {
		return NavRaycastHit{
			Point: toPoint(tVec),
			Poly:  startPoly,
			T:     1,
			Hit:   false,
		}, tVec, visitedOut
	}

	dir := tVec.sub(sVec)
	curPoly := startPoly
	curT := 0.0

	// Stack-allocated visited ring for typical raycasts (0 heap allocations); spills to slice only for >64 polys.
	var visitedArr [64]int
	visited := visitedArr[:0]
	visited = append(visited, curPoly)

	hasVisited := func(id int) bool {
		for _, v := range visited {
			if v == id {
				return true
			}
		}
		return false
	}

	maxSteps := len(m.polys) + 2
	for step := 0; step < maxSteps; step++ {
		poly := &m.polys[curPoly]
		if polyContains(poly, tVec) {
			return NavRaycastHit{
				Point: toPoint(tVec),
				Poly:  curPoly,
				T:     1,
				Hit:   false,
			}, tVec, visitedOut
		}

		tExit, exitNorm, ok := polyRayExit2D(poly, sVec, dir)
		if !ok {
			pt := sVec.add(dir.scale(curT))
			return NavRaycastHit{
				Point: toPoint(pt),
				Poly:  curPoly,
				T:     float32(curT),
				Hit:   true,
			}, pt, visitedOut
		}
		if tExit < curT {
			tExit = curT
		}
		if tExit >= 1.0-1e-9 {
			endPt := sVec.add(dir)
			if polyContains(poly, endPt) {
				return NavRaycastHit{
					Point: toPoint(tVec),
					Poly:  curPoly,
					T:     1,
					Hit:   false,
				}, tVec, visitedOut
			}
			tExit = 1.0
		}

		exitPt := sVec.add(dir.scale(tExit))

		// Look for a direct allowed shared-edge neighbor crossing exitPt that advances the ray.
		nextPoly := -1
		bestNextT := tExit + 1e-9
		reachedTarget := false
		for _, pid := range poly.outPortals {
			p := m.portals[pid]
			if p.offMesh || hasVisited(p.toPoly) {
				continue
			}
			np := &m.polys[p.toPoly]
			if !filter.Allows(np.area, np.flags) {
				continue
			}
			if !pointOnSegment2D(exitPt, p.left, p.right, portalEps) {
				continue
			}
			if polyContains(np, tVec) {
				nextPoly = p.toPoly
				reachedTarget = true
				break
			}
			nt, _, nok := polyRayExit2D(np, sVec, dir)
			if nok && (nt > bestNextT || (nextPoly >= 0 && nt == bestNextT && p.toPoly < nextPoly)) {
				bestNextT = nt
				nextPoly = p.toPoly
			}
		}

		if reachedTarget || nextPoly >= 0 {
			curPoly = nextPoly
			curT = tExit
			visited = append(visited, curPoly)
			if recordVisited && visitedOut[len(visitedOut)-1] != curPoly {
				visitedOut = append(visitedOut, curPoly)
			}
			if reachedTarget {
				return NavRaycastHit{
					Point: toPoint(tVec),
					Poly:  curPoly,
					T:     1,
					Hit:   false,
				}, tVec, visitedOut
			}
			continue
		}

		// Handle vertex grazing (e.g. crossing a shared corner where 4 quads or a triangle fan meet):
		// run a tiny local BFS across unvisited allowed polygons touching exitPt to see if the ray continues cleanly.
		bridgePoly, bridgeChain, bridgeLen := m.findCornerBridge2D(curPoly, exitPt, sVec, tVec, dir, tExit, filter, hasVisited)
		if bridgePoly >= 0 {
			for i := range bridgeLen {
				bp := bridgeChain[i]
				visited = append(visited, bp)
				if recordVisited && visitedOut[len(visitedOut)-1] != bp {
					visitedOut = append(visitedOut, bp)
				}
			}
			curPoly = bridgePoly
			curT = tExit
			continue
		}

		// Obstructed by a boundary edge or disallowed neighbor polygon.
		exitNorm = m.obstructingEdgeNormal2D(poly, sVec, dir, exitPt, tExit, exitNorm, filter)
		return NavRaycastHit{
			Point:  toPoint(exitPt),
			Normal: toPoint(exitNorm),
			Poly:   curPoly,
			T:      float32(min(max(tExit, 0), 1)),
			Hit:    true,
		}, exitPt, visitedOut
	}

	pt := sVec.add(dir.scale(curT))
	return NavRaycastHit{
		Point: toPoint(pt),
		Poly:  curPoly,
		T:     float32(min(max(curT, 0), 1)),
		Hit:   true,
	}, pt, visitedOut
}

// findCornerBridge2D searches a small vertex neighborhood around exitPt for an allowed polygon
// connected via shared edges touching exitPt that strictly advances the ray past tExit (or contains target).
func (m *NavMesh) findCornerBridge2D(
	startPoly int,
	exitPt, sVec, tVec, dir vec2,
	tExit float64,
	filter NavFilter,
	alreadyVisited func(int) bool,
) (int, [32]int, int) {
	const maxLocal = 32
	var qPoly [maxLocal]int
	var qParent [maxLocal]int
	var outChain [maxLocal]int
	qPoly[0] = startPoly
	qParent[0] = -1
	head, tail := 0, 1

	inLocal := func(id int) bool {
		for i := range tail {
			if qPoly[i] == id {
				return true
			}
		}
		return false
	}

	for head < tail {
		cur := qPoly[head]
		curIdx := head
		head++

		for _, pid := range m.polys[cur].outPortals {
			p := m.portals[pid]
			if p.offMesh || alreadyVisited(p.toPoly) || inLocal(p.toPoly) {
				continue
			}
			np := &m.polys[p.toPoly]
			if !filter.Allows(np.area, np.flags) {
				continue
			}
			if !pointOnSegment2D(exitPt, p.left, p.right, portalEps) {
				continue
			}
			if tail >= maxLocal {
				break
			}
			childIdx := tail
			qPoly[tail] = p.toPoly
			qParent[tail] = curIdx
			tail++

			advances := polyContains(np, tVec)
			if !advances {
				if nt, _, nok := polyRayExit2D(np, sVec, dir); nok && nt > tExit+1e-9 {
					advances = true
				}
			}
			if advances {
				var rev [maxLocal]int
				n := 0
				for idx := childIdx; idx > 0; idx = qParent[idx] {
					rev[n] = qPoly[idx]
					n++
				}
				for i := range n {
					outChain[i] = rev[n-1-i]
				}
				return p.toPoly, outChain, n
			}
		}
	}
	return -1, outChain, 0
}

// NavMoveResult describes the outcome of sliding a position across a 2D NavMesh surface.
type NavMoveResult struct {
	// Point is the final position clamped onto the mesh surface.
	Point core.Point
	// Poly is the index of the final polygon containing Point, or -1 if start lies outside the allowed mesh.
	Poly int
	// Visited is the ordered sequence of polygon indices traversed from start to Point.
	Visited []int
	// HitBoundary is true if movement was constrained by a mesh boundary edge or disallowed polygon.
	HitBoundary bool
}

// MoveAlongSurface slides an agent from start toward target while staying on polygons allowed by DefaultNavFilter.
//
// Returns (NavMoveResult{Point: start, Poly: -1}, false) if start lies outside the allowed mesh.
func (m *NavMesh) MoveAlongSurface(start, target core.Point) (NavMoveResult, bool) {
	return m.MoveAlongSurfaceWithFilter(start, target, DefaultNavFilter())
}

// MoveAlongSurfaceWithFilter slides an agent from start toward target while staying on polygons allowed by filter.
//
// Returns (NavMoveResult{Point: start, Poly: -1}, false) if start lies outside the allowed mesh.
func (m *NavMesh) MoveAlongSurfaceWithFilter(start, target core.Point, filter NavFilter) (NavMoveResult, bool) {
	return m.moveAlongSurfaceInto(nil, start, target, filter)
}

func (m *NavMesh) moveAlongSurfaceInto(visitedDst []int, start, target core.Point, filter NavFilter) (NavMoveResult, bool) {
	sVec := toVec2(start)
	tVec := toVec2(target)
	if m == nil || len(m.polys) == 0 || !isFiniteVec2(sVec) || !isFiniteVec2(tVec) {
		return NavMoveResult{Point: start, Poly: -1}, false
	}
	var startBuf [16]int
	starts := m.bvhFindPolysAt(sVec, filter, false, startBuf[:0])
	if len(starts) == 0 {
		return NavMoveResult{Point: start, Poly: -1}, false
	}

	curPoly := m.pickBestRayStartPoly(starts, sVec, tVec)
	cur := sVec
	desired := tVec
	visited := append(visitedDst[:0], curPoly)
	hitBoundary := false

	const maxSlideIters = 8
	for range maxSlideIters {
		var hit NavRaycastHit
		var hitPos vec2
		hit, hitPos, visited = m.raycastFromPoly(curPoly, cur, desired, filter, visited, true)
		curPoly = hit.Poly
		cur = hitPos
		if !hit.Hit {
			break
		}
		hitBoundary = true
		norm := toVec2(hit.Normal)
		if norm.lenSq() <= 1e-12 {
			break
		}
		rem := desired.sub(cur)
		// Project remaining displacement onto the boundary tangent (perpendicular to inward normal).
		outComp := rem.dot(norm)
		slide := rem.sub(norm.scale(outComp))
		if slide.lenSq() <= 1e-10 {
			break
		}
		// Ensure monotonic progress toward the original target to prevent corner oscillation.
		if slide.dot(tVec.sub(cur)) <= 1e-9 {
			break
		}
		desired = cur.add(slide)
		if vequal(cur, desired) {
			break
		}
	}

	// Clamp final point strictly onto curPoly to eliminate any floating-point boundary drift.
	cur = closestPointInPoly(&m.polys[curPoly], cur)
	return NavMoveResult{
		Point:       toPoint(cur),
		Poly:        curPoly,
		Visited:     visited,
		HitBoundary: hitBoundary,
	}, true
}

// RandomPoint picks an area-weighted uniformly random point anywhere on polygons allowed by DefaultNavFilter,
// using rng. Results are bit-identical across runs and platforms for the same seed.
//
// Returns (zero, -1, false) if m is empty, rng is nil, or no polygons are allowed.
func (m *NavMesh) RandomPoint(rng *random.Stream) (point core.Point, poly int, ok bool) {
	return m.RandomPointWithFilter(rng, DefaultNavFilter())
}

// RandomPointWithFilter picks an area-weighted uniformly random point anywhere on polygons allowed by filter,
// using rng. Results are bit-identical across runs and platforms for the same seed.
//
// Returns (zero, -1, false) if m is empty, rng is nil, or no polygons pass filter.
func (m *NavMesh) RandomPointWithFilter(rng *random.Stream, filter NavFilter) (point core.Point, poly int, ok bool) {
	if m == nil || len(m.polys) == 0 || rng == nil {
		return core.Point{}, -1, false
	}
	var totalArea float64
	lastAllowed := -1
	for i := range m.polys {
		p := &m.polys[i]
		if !filter.Allows(p.area, p.flags) || p.surfArea <= 0 {
			continue
		}
		totalArea += p.surfArea
		lastAllowed = i
	}
	if lastAllowed < 0 || totalArea <= 0 {
		return core.Point{}, -1, false
	}

	target := float64(rng.Float64() * totalArea)
	var acc float64
	chosen := lastAllowed
	for i := range m.polys {
		p := &m.polys[i]
		if !filter.Allows(p.area, p.flags) || p.surfArea <= 0 {
			continue
		}
		acc += p.surfArea
		if target < acc {
			chosen = i
			break
		}
	}

	pt := sampleConvexPoly2D(m.polys[chosen].verts, rng)
	return toPoint(pt), chosen, true
}

// sampleConvexPoly2D samples a point uniformly by area inside convex CCW polygon verts
// using only IEEE-754 +, -, *, / (no sqrt or trig) so output is bit-identical across architectures.
func sampleConvexPoly2D(verts []vec2, rng *random.Stream) vec2 {
	n := len(verts)
	if n == 0 {
		return vec2{}
	}
	if n < 3 {
		return verts[0]
	}
	triIdx := 1
	if n > 3 {
		var total2 float64
		for i := 1; i < n-1; i++ {
			a2 := math.Abs(triArea2(verts[0], verts[i], verts[i+1]))
			total2 += a2
		}
		if total2 > 0 {
			r := float64(rng.Float64() * total2)
			var acc float64
			triIdx = n - 2
			for i := 1; i < n-1; i++ {
				a2 := math.Abs(triArea2(verts[0], verts[i], verts[i+1]))
				acc += a2
				if r < acc {
					triIdx = i
					break
				}
			}
		}
	}
	a, b, c := verts[0], verts[triIdx], verts[triIdx+1]
	u := rng.Float64()
	v := rng.Float64()
	if u+v > 1 {
		u = 1 - u
		v = 1 - v
	}
	w := 1 - u - v
	// Explicit float64(...) conversions prevent platform-specific FMA contraction per Go spec.
	return vec2{
		x: float64(w*a.x) + float64(u*b.x) + float64(v*c.x),
		y: float64(w*a.y) + float64(u*b.y) + float64(v*c.y),
	}
}

// clipPolyAABB2DAny clips convex polygon verts against [minX, maxX] x [minY, maxY] using Sutherland-Hodgman,
// preserving degenerate 1- or 2-vertex intersections (point or segment) as well as full polygons.
func clipPolyAABB2DAny(verts []vec2, minX, minY, maxX, maxY float64, bufA, bufB []vec2) []vec2 {
	src := append(bufA[:0], verts...)
	dst := bufB[:0]

	clipEdge := func(in []vec2, out []vec2, inside func(vec2) bool, intersect func(vec2, vec2) vec2) []vec2 {
		out = out[:0]
		n := len(in)
		if n == 0 {
			return out
		}
		prev := in[n-1]
		prevIn := inside(prev)
		for i := range n {
			cur := in[i]
			curIn := inside(cur)
			if curIn {
				if !prevIn {
					out = append(out, intersect(prev, cur))
				}
				out = append(out, cur)
			} else if prevIn {
				out = append(out, intersect(prev, cur))
			}
			prev = cur
			prevIn = curIn
		}
		return out
	}

	src = clipEdge(src, dst,
		func(p vec2) bool { return p.x >= minX },
		func(a, b vec2) vec2 {
			t := (minX - a.x) / (b.x - a.x)
			return vec2{x: minX, y: a.y + float64(t*(b.y-a.y))}
		},
	)
	if len(src) == 0 {
		return src[:0]
	}
	dst = bufA[:0]
	src = clipEdge(src, dst,
		func(p vec2) bool { return p.x <= maxX },
		func(a, b vec2) vec2 {
			t := (maxX - a.x) / (b.x - a.x)
			return vec2{x: maxX, y: a.y + float64(t*(b.y-a.y))}
		},
	)
	if len(src) == 0 {
		return src[:0]
	}
	dst = bufB[:0]
	src = clipEdge(src, dst,
		func(p vec2) bool { return p.y >= minY },
		func(a, b vec2) vec2 {
			t := (minY - a.y) / (b.y - a.y)
			return vec2{x: a.x + float64(t*(b.x-a.x)), y: minY}
		},
	)
	if len(src) == 0 {
		return src[:0]
	}
	dst = bufA[:0]
	src = clipEdge(src, dst,
		func(p vec2) bool { return p.y <= maxY },
		func(a, b vec2) vec2 {
			t := (maxY - a.y) / (b.y - a.y)
			return vec2{x: a.x + float64(t*(b.x-a.x)), y: maxY}
		},
	)
	return src
}

// closestPointOnConvex2D returns the closest point to p on a non-empty convex CCW polygon (or 1-/2-vertex segment).
func closestPointOnConvex2D(verts []vec2, p vec2) vec2 {
	n := len(verts)
	if n == 1 {
		return verts[0]
	}
	if n == 2 {
		return closestPointOnSegment(p, verts[0], verts[1])
	}
	inside := true
	for i := range n {
		a := verts[i]
		b := verts[(i+1)%n]
		edge := b.sub(a)
		edgeLen := edge.len()
		if edge.cross(p.sub(a)) < -portalEps*edgeLen {
			inside = false
			break
		}
	}
	if inside {
		return p
	}
	bestDistSq := math.Inf(1)
	var bestPt vec2
	for i := range n {
		cp := closestPointOnSegment(p, verts[i], verts[(i+1)%n])
		d2 := distSq(p, cp)
		if d2 < bestDistSq {
			bestDistSq = d2
			bestPt = cp
		}
	}
	return bestPt
}

// clipPolyAABB2D clips convex polygon verts against [minX, maxX] x [minY, maxY] using Sutherland-Hodgman.
func clipPolyAABB2D(verts []vec2, minX, minY, maxX, maxY float64, bufA, bufB []vec2) []vec2 {
	src := clipPolyAABB2DAny(verts, minX, minY, maxX, maxY, bufA, bufB)
	if len(src) < 3 {
		return src[:0]
	}
	return src
}

// RandomPointAround picks an area-weighted uniformly random point within Euclidean distance radius
// of center on polygons allowed by DefaultNavFilter that are reachable from center's polygon.
// Disconnected islands are never returned, and results are bit-identical across runs and platforms for the same seed.
//
// Returns (zero, -1, false) if m is empty, rng is nil, radius < 0, or center lies outside the allowed mesh.
func (m *NavMesh) RandomPointAround(center core.Point, radius float32, rng *random.Stream) (point core.Point, poly int, ok bool) {
	return m.RandomPointAroundWithFilter(center, radius, rng, DefaultNavFilter())
}

// RandomPointAroundWithFilter behaves like RandomPointAround while restricting reachability and sampling to polygons allowed by filter.
func (m *NavMesh) RandomPointAroundWithFilter(center core.Point, radius float32, rng *random.Stream, filter NavFilter) (point core.Point, poly int, ok bool) {
	return m.randomPointAround(center, radius, rng, filter, nil)
}

type clippedPoly2D struct {
	polyIdx   int
	vertStart int
	vertCount int
	area      float64
}

func (m *NavMesh) randomPointAround(center core.Point, radius float32, rng *random.Stream, filter NavFilter, q *NavMeshQuery) (core.Point, int, bool) {
	cVec := toVec2(center)
	r := float64(radius)
	if m == nil || len(m.polys) == 0 || rng == nil || !isFiniteVec2(cVec) || math.IsNaN(r) || math.IsInf(r, 0) || r < 0 {
		return core.Point{}, -1, false
	}
	startPoly := m.bvhFindPoly(cVec, filter, true)
	if startPoly < 0 {
		return core.Point{}, -1, false
	}
	if r == 0 {
		return center, startPoly, true
	}

	rSq := float64(r * r)
	minX, minY := cVec.x-r, cVec.y-r
	maxX, maxY := cVec.x+r, cVec.y+r

	// BFS over connected allowed polygons intersecting circle(center, radius).
	var queue []int
	var visitedMark []uint32
	var visitGen uint32
	var clippedPolys []clippedPoly2D
	var clippedVerts []vec2
	if q != nil {
		q.ensureBFS(len(m.polys))
		q.bfsGen++
		if q.bfsGen == 0 {
			clear(q.bfsVisited[:cap(q.bfsVisited)])
			q.bfsGen = 1
		}
		visitGen = q.bfsGen
		visitedMark = q.bfsVisited
		queue = q.bfsQueue[:0]
		clippedPolys = q.clippedPolys[:0]
		clippedVerts = q.clippedVerts[:0]
	} else {
		visitedMark = make([]uint32, len(m.polys))
		visitGen = 1
	}

	var bufA, bufB [16]vec2
	var totalArea float64

	enqueue := func(idx int) {
		visitedMark[idx] = visitGen
		queue = append(queue, idx)
	}
	enqueue(startPoly)

	for head := 0; head < len(queue); head++ {
		cur := queue[head]
		poly := &m.polys[cur]

		clipped := clipPolyAABB2D(poly.verts, minX, minY, maxX, maxY, bufA[:0], bufB[:0])
		if len(clipped) >= 3 {
			a := 0.5 * math.Abs(polygonArea2(clipped))
			if a > 1e-12 {
				vs := len(clippedVerts)
				clippedVerts = append(clippedVerts, clipped...)
				clippedPolys = append(clippedPolys, clippedPoly2D{
					polyIdx:   cur,
					vertStart: vs,
					vertCount: len(clipped),
					area:      a,
				})
				totalArea += a
			}
		}

		for _, pid := range poly.outPortals {
			p := m.portals[pid]
			next := p.toPoly
			if visitedMark[next] == visitGen {
				continue
			}
			np := &m.polys[next]
			if !filter.Allows(np.area, np.flags) {
				continue
			}
			if np.maxX < minX-portalEps || np.minX > maxX+portalEps || np.maxY < minY-portalEps || np.minY > maxY+portalEps {
				continue
			}
			cp := closestPointInPoly(np, cVec)
			if distSq(cp, cVec) <= rSq+1e-9 {
				enqueue(next)
			}
		}
	}

	if q != nil {
		q.bfsQueue = queue
		q.clippedPolys = clippedPolys
		q.clippedVerts = clippedVerts
	}

	if len(clippedPolys) == 0 || totalArea <= 0 {
		return center, startPoly, true
	}

	// Exact area-weighted rejection sampling against circle(center, radius).
	// Because each polygon is already clipped to the square [center-r, center+r]^2,
	// acceptance probability on open space is pi/4 (~78.5%).
	const maxRejectAttempts = 64
	var lastPt vec2
	lastPoly := startPoly
	for range maxRejectAttempts {
		pick := float64(rng.Float64() * totalArea)
		var acc float64
		chosen := clippedPolys[len(clippedPolys)-1]
		for i := range clippedPolys {
			acc += clippedPolys[i].area
			if pick < acc {
				chosen = clippedPolys[i]
				break
			}
		}
		verts := clippedVerts[chosen.vertStart : chosen.vertStart+chosen.vertCount]
		pt := sampleConvexPoly2D(verts, rng)
		lastPt = pt
		lastPoly = chosen.polyIdx
		if distSq(pt, cVec) <= rSq {
			return toPoint(pt), chosen.polyIdx, true
		}
	}

	// Deterministic fallback for extreme degenerate sliver polygons:
	// slide from closest point in lastPoly toward lastPt to lie strictly within radius.
	anchor := closestPointInPoly(&m.polys[lastPoly], cVec)
	d := lastPt.sub(anchor)
	if distSq(anchor, cVec) <= rSq {
		// Binary search t in [0, 1] along convex segment anchor->lastPt without math.Sqrt.
		lo, hi := 0.0, 1.0
		for range 24 {
			mid := 0.5 * (lo + hi)
			cand := vec2{
				x: anchor.x + float64(mid*d.x),
				y: anchor.y + float64(mid*d.y),
			}
			if distSq(cand, cVec) <= rSq {
				lo = mid
			} else {
				hi = mid
			}
		}
		res := vec2{
			x: anchor.x + float64(lo*d.x),
			y: anchor.y + float64(lo*d.y),
		}
		return toPoint(res), lastPoly, true
	}
	return center, startPoly, true
}

// NavMeshQuery holds reusable generation-stamped scratch buffers for zero-allocation
// pathfinding and spatial queries over a 2D NavMesh.
//
// Create one NavMeshQuery per worker or system with NewNavMeshQuery(mesh) when running
// repeated queries or querying a shared read-only NavMesh from separate goroutines.
// A NavMeshQuery is not safe for concurrent use by multiple goroutines.
type NavMeshQuery struct {
	mesh *NavMesh

	// Generation-stamped A* scratch buffers.
	gen     uint32
	seen    []uint32
	closed  []uint32
	gScore  []float64
	parent  []int
	entryPt []vec2
	heapPos []int
	open    []astarNode

	goalGen    uint32
	goalMark   []uint32
	startPolys []int
	goalPolys  []int

	// Reusable funnel & simplification buffers.
	portalIDs     []int
	funnelPortals [][2]vec2
	rawPath       []vec2
	cleanPath     []vec2

	// Reusable BFS / clipping buffers for RandomPointAround.
	bfsGen       uint32
	bfsVisited   []uint32
	bfsQueue     []int
	clippedPolys []clippedPoly2D
	clippedVerts []vec2
}

// NewNavMeshQuery creates a reusable query instance bound to mesh.
func NewNavMeshQuery(mesh *NavMesh) *NavMeshQuery {
	return &NavMeshQuery{mesh: mesh}
}

// Mesh returns the NavMesh currently bound to q.
func (q *NavMeshQuery) Mesh() *NavMesh {
	if q == nil {
		return nil
	}
	return q.mesh
}

// SetMesh rebinds q to mesh while keeping its allocated scratch capacity for reuse.
func (q *NavMeshQuery) SetMesh(mesh *NavMesh) {
	if q == nil {
		return
	}
	q.mesh = mesh
}

func (q *NavMeshQuery) ensurePathBuffers(nPolys, nodeCount int) {
	if cap(q.goalMark) < nPolys {
		q.goalMark = make([]uint32, nPolys)
	} else {
		q.goalMark = q.goalMark[:nPolys]
	}
	if cap(q.seen) < nodeCount {
		q.seen = make([]uint32, nodeCount)
		q.closed = make([]uint32, nodeCount)
		q.gScore = make([]float64, nodeCount)
		q.parent = make([]int, nodeCount)
		q.entryPt = make([]vec2, nodeCount)
		q.heapPos = make([]int, nodeCount)
	} else {
		q.seen = q.seen[:nodeCount]
		q.closed = q.closed[:nodeCount]
		q.gScore = q.gScore[:nodeCount]
		q.parent = q.parent[:nodeCount]
		q.entryPt = q.entryPt[:nodeCount]
		q.heapPos = q.heapPos[:nodeCount]
	}
}

func (q *NavMeshQuery) ensureBFS(nPolys int) {
	if cap(q.bfsVisited) < nPolys {
		q.bfsVisited = make([]uint32, nPolys)
	} else {
		q.bfsVisited = q.bfsVisited[:nPolys]
	}
}

// minFilterCost returns an O(1) admissible lower bound on positive area costs for filter.
func minFilterCost(filter NavFilter) float64 {
	minCost := 1.0
	for area, c := range filter.AreaCosts {
		if filter.ExcludedAreas[area] {
			continue
		}
		if c > 0 && !math.IsNaN(c) && !math.IsInf(c, 0) && c < minCost {
			minCost = c
		}
	}
	return minCost
}

// FindPathInto finds the shortest path inside q's mesh from start to goal using DefaultNavFilter,
// writing waypoints into dst[:0] and reusing q's internal buffers so repeated calls perform zero heap allocations
// once dst and q are warmed up.
func (q *NavMeshQuery) FindPathInto(dst []core.Point, start, goal core.Point) ([]core.Point, bool) {
	return q.FindPathWithFilterInto(dst, start, goal, DefaultNavFilter())
}

// FindPathWithFilterInto finds the minimum-cost path inside q's mesh from start to goal restricted to filter,
// writing waypoints into dst[:0] and reusing q's internal buffers with zero heap allocations after warm-up.
func (q *NavMeshQuery) FindPathWithFilterInto(dst []core.Point, start, goal core.Point, filter NavFilter) ([]core.Point, bool) {
	res := q.FindPathWithFilterAndOptionsInto(dst, start, goal, filter, PathOptions{})
	if !res.Found || res.Partial {
		return nil, false
	}
	return res.Path, true
}

type navAStarResult2D struct {
	foundComplete bool
	directPoly    int
	hasBest       bool
	bestPortalID  int
	bestPt        vec2
	exhausted     bool
	expansions    int
	goalNode      int
}

func (q *NavMeshQuery) runPortalAStar(sVec, gVec vec2, filter NavFilter, opts PathOptions) navAStarResult2D {
	m := q.mesh
	nPolys := len(m.polys)
	goalNode := len(m.portals)
	nodeCount := goalNode + 1
	q.ensurePathBuffers(nPolys, nodeCount)

	q.startPolys = m.bvhFindPolysAt(sVec, filter, true, q.startPolys[:0])
	if len(q.startPolys) == 0 {
		return navAStarResult2D{directPoly: -1, bestPortalID: -1, goalNode: goalNode}
	}

	q.goalPolys = m.bvhFindPolysAt(gVec, filter, true, q.goalPolys[:0])
	hasGoal := len(q.goalPolys) > 0
	if !hasGoal && !opts.AllowPartial {
		return navAStarResult2D{directPoly: -1, bestPortalID: -1, goalNode: goalNode}
	}

	q.goalGen++
	if q.goalGen == 0 {
		clear(q.goalMark[:cap(q.goalMark)])
		q.goalGen = 1
	}
	goalGen := q.goalGen
	for _, gp := range q.goalPolys {
		q.goalMark[gp] = goalGen
	}

	minCost := minFilterCost(filter)

	directPossible := false
	directCost := math.Inf(1)
	directPoly := -1
	for _, sp := range q.startPolys {
		if q.goalMark[sp] == goalGen {
			if vequal(sVec, gVec) {
				q.parent[goalNode] = -1
				return navAStarResult2D{foundComplete: true, directPoly: sp, bestPortalID: -1, goalNode: goalNode}
			}
			c := filter.AreaCost(m.polys[sp].area)
			dc := dist(sVec, gVec) * c
			if dc < directCost {
				directCost = dc
				directPossible = true
				directPoly = sp
			}
		}
	}
	if directPossible && directCost <= minCost*dist(sVec, gVec)+1e-9 {
		q.parent[goalNode] = -1
		return navAStarResult2D{foundComplete: true, directPoly: directPoly, bestPortalID: -1, goalNode: goalNode}
	}

	hasBest := false
	bestPortalID := -1
	var bestPt vec2
	var bestH, bestG float64
	if opts.AllowPartial {
		for _, sp := range q.startPolys {
			cp := closestPointInPoly(&m.polys[sp], gVec)
			h := dist(cp, gVec)
			g := dist(sVec, cp) * filter.AreaCost(m.polys[sp].area)
			if !hasBest || h < bestH || (h == bestH && g < bestG) {
				hasBest = true
				bestPortalID = -1
				bestPt = cp
				bestH = h
				bestG = g
			}
		}
	}

	q.gen++
	if q.gen == 0 {
		clear(q.seen[:cap(q.seen)])
		clear(q.closed[:cap(q.closed)])
		q.gen = 1
	}
	gen := q.gen
	q.open = q.open[:0]

	pushOrUpdate := func(id int, g, f, h float64, parent int, pt vec2) {
		if q.seen[id] != gen {
			q.seen[id] = gen
			q.gScore[id] = g
			q.parent[id] = parent
			q.entryPt[id] = pt
			pos := len(q.open)
			q.open = append(q.open, astarNode{id: id, f: f, h: h})
			q.heapPos[id] = pos
			navSiftUp(q.open, q.heapPos, pos)
			return
		}
		if g >= q.gScore[id] {
			return
		}
		q.gScore[id] = g
		q.parent[id] = parent
		q.entryPt[id] = pt
		pos := q.heapPos[id]
		if pos >= 0 {
			q.open[pos].f = f
			q.open[pos].h = h
			navSiftUp(q.open, q.heapPos, pos)
			return
		}
		pos = len(q.open)
		q.open = append(q.open, astarNode{id: id, f: f, h: h})
		q.heapPos[id] = pos
		navSiftUp(q.open, q.heapPos, pos)
	}

	if directPossible {
		pushOrUpdate(goalNode, directCost, directCost, 0, -1, gVec)
	}

	for _, sp := range q.startPolys {
		spCost := filter.AreaCost(m.polys[sp].area)
		for _, pid := range m.polys[sp].outPortals {
			p := m.portals[pid]
			tp := &m.polys[p.toPoly]
			if !filter.Allows(tp.area, tp.flags) {
				continue
			}
			tc := filter.AreaCost(tp.area)
			if tc <= 0 || math.IsNaN(tc) || math.IsInf(tc, 0) {
				continue
			}
			var g float64
			var exitPt vec2
			if p.offMesh {
				g = dist(sVec, p.left)*spCost + p.cost
				exitPt = p.endPos
			} else {
				exitPt = bestPortalPoint(sVec, gVec, p.left, p.right)
				g = dist(sVec, exitPt) * spCost
			}
			if q.seen[pid] != gen || g < q.gScore[pid] {
				h := dist(exitPt, gVec) * minCost
				pushOrUpdate(pid, g, g+h, h, -1, exitPt)
			}
		}
	}

	expansions := 0
	foundComplete := false
	exhausted := false

	for len(q.open) > 0 {
		if opts.MaxExpansions > 0 && expansions >= opts.MaxExpansions {
			exhausted = true
			break
		}
		cur := navHeapPop(&q.open, q.heapPos)
		expansions++

		if cur.id == goalNode {
			foundComplete = true
			break
		}
		q.closed[cur.id] = gen

		portal := m.portals[cur.id]
		curG := q.gScore[cur.id]
		curPt := q.entryPt[cur.id]
		toPoly := &m.polys[portal.toPoly]
		toCost := filter.AreaCost(toPoly.area)

		if opts.AllowPartial {
			cp := closestPointInPoly(toPoly, gVec)
			h := dist(cp, gVec)
			g := curG + dist(curPt, cp)*toCost
			if !hasBest || h < bestH || (h == bestH && (g < bestG || (g == bestG && cur.id < bestPortalID))) {
				hasBest = true
				bestPortalID = cur.id
				bestPt = cp
				bestH = h
				bestG = g
			}
		}

		if q.goalMark[portal.toPoly] == goalGen {
			ng := curG + dist(curPt, gVec)*toCost
			if q.seen[goalNode] != gen || ng < q.gScore[goalNode] {
				pushOrUpdate(goalNode, ng, ng, 0, cur.id, gVec)
			}
		}

		for _, npid := range toPoly.outPortals {
			if q.closed[npid] == gen {
				continue
			}
			np := m.portals[npid]
			if !np.offMesh && np.toPoly == portal.fromPoly {
				continue
			}
			ntp := &m.polys[np.toPoly]
			if !filter.Allows(ntp.area, ntp.flags) {
				continue
			}
			ntc := filter.AreaCost(ntp.area)
			if ntc <= 0 || math.IsNaN(ntc) || math.IsInf(ntc, 0) {
				continue
			}
			var ng float64
			var exitPt vec2
			if np.offMesh {
				ng = curG + dist(curPt, np.left)*toCost + np.cost
				exitPt = np.endPos
			} else {
				exitPt = bestPortalPoint(curPt, gVec, np.left, np.right)
				ng = curG + dist(curPt, exitPt)*toCost
			}
			if q.seen[npid] != gen || ng < q.gScore[npid] {
				h := dist(exitPt, gVec) * minCost
				pushOrUpdate(npid, ng, ng+h, h, cur.id, exitPt)
			}
		}
	}

	if !foundComplete && len(q.open) > 0 && opts.MaxExpansions > 0 && expansions >= opts.MaxExpansions {
		exhausted = true
	}

	return navAStarResult2D{
		foundComplete: foundComplete,
		directPoly:    directPoly,
		hasBest:       hasBest,
		bestPortalID:  bestPortalID,
		bestPt:        bestPt,
		exhausted:     exhausted,
		expansions:    expansions,
		goalNode:      goalNode,
	}
}

// FindPathWithFilterAndOptionsInto runs a bounded or partial 2D NavMesh search restricted to filter under opts,
// writing the resulting waypoints into dst[:0] and reusing q's internal buffers with zero heap allocations after warm-up.
func (q *NavMeshQuery) FindPathWithFilterAndOptionsInto(dst []core.Point, start, goal core.Point, filter NavFilter, opts PathOptions) NavPathResult {
	if q == nil || q.mesh == nil || len(q.mesh.polys) == 0 {
		return NavPathResult{}
	}
	sVec := toVec2(start)
	gVec := toVec2(goal)
	if !isFiniteVec2(sVec) || !isFiniteVec2(gVec) {
		return NavPathResult{}
	}

	sr := q.runPortalAStar(sVec, gVec, filter, opts)
	res := NavPathResult{
		Exhausted:  sr.exhausted,
		Expansions: sr.expansions,
	}

	if sr.foundComplete {
		res.Found = true
		if q.parent[sr.goalNode] == -1 {
			if vequal(sVec, gVec) {
				res.Path = append(dst[:0], start)
			} else {
				res.Path = append(dst[:0], start, goal)
			}
			return res
		}
		q.portalIDs = q.portalIDs[:0]
		for id := q.parent[sr.goalNode]; id != -1; id = q.parent[id] {
			q.portalIDs = append(q.portalIDs, id)
		}
		slices.Reverse(q.portalIDs)
		res.Path = q.finalizeCorridorInto(dst, sVec, gVec, q.portalIDs)
		return res
	}

	if opts.AllowPartial && sr.hasBest {
		res.Found = true
		res.Partial = true
		if sr.bestPortalID < 0 {
			if vequal(sVec, sr.bestPt) {
				res.Path = append(dst[:0], start)
			} else {
				res.Path = append(dst[:0], start, toPoint(sr.bestPt))
			}
			return res
		}
		q.portalIDs = q.portalIDs[:0]
		for id := sr.bestPortalID; id != -1; id = q.parent[id] {
			q.portalIDs = append(q.portalIDs, id)
		}
		slices.Reverse(q.portalIDs)
		res.Path = q.finalizeCorridorInto(dst, sVec, sr.bestPt, q.portalIDs)
		return res
	}

	return res
}

// FindCorridorInto finds the polygon corridor from start to goal under DefaultNavFilter,
// writing polygon indices into dst[:0] with zero heap allocations after warm-up.
func (q *NavMeshQuery) FindCorridorInto(dst []int, start, goal core.Point) ([]int, bool) {
	return q.FindCorridorWithFilterInto(dst, start, goal, DefaultNavFilter())
}

// FindCorridorWithFilterInto finds the minimum-cost polygon corridor from start to goal restricted to filter,
// writing polygon indices into dst[:0] with zero heap allocations after warm-up.
func (q *NavMeshQuery) FindCorridorWithFilterInto(dst []int, start, goal core.Point, filter NavFilter) ([]int, bool) {
	if q == nil || q.mesh == nil || len(q.mesh.polys) == 0 {
		return nil, false
	}
	sVec := toVec2(start)
	gVec := toVec2(goal)
	if !isFiniteVec2(sVec) || !isFiniteVec2(gVec) {
		return nil, false
	}
	sr := q.runPortalAStar(sVec, gVec, filter, PathOptions{})
	if !sr.foundComplete {
		return nil, false
	}
	if q.parent[sr.goalNode] == -1 {
		if sr.directPoly < 0 {
			return nil, false
		}
		return append(dst[:0], sr.directPoly), true
	}
	q.portalIDs = q.portalIDs[:0]
	for id := q.parent[sr.goalNode]; id != -1; id = q.parent[id] {
		q.portalIDs = append(q.portalIDs, id)
	}
	slices.Reverse(q.portalIDs)
	m := q.mesh
	out := append(dst[:0], m.portals[q.portalIDs[0]].fromPoly)
	for _, pid := range q.portalIDs {
		out = append(out, m.portals[pid].toPoly)
	}
	return out, true
}

func (q *NavMeshQuery) finalizeCorridorInto(dst []core.Point, start, end vec2, portalIDs []int) []core.Point {
	q.rawPath = q.stringPullInto(q.rawPath[:0], start, end, portalIDs)
	q.cleanPath = simplifyCollinearInto(q.cleanPath[:0], q.rawPath)
	out := dst[:0]
	for _, v := range q.cleanPath {
		out = append(out, toPoint(v))
	}
	return out
}

func (q *NavMeshQuery) stringPullInto(out []vec2, start, goal vec2, portalIDs []int) []vec2 {
	m := q.mesh
	hasOffMesh := false
	for _, pid := range portalIDs {
		if m.portals[pid].offMesh {
			hasOffMesh = true
			break
		}
	}
	if !hasOffMesh {
		return q.stringPullSegmentInto(out, start, goal, portalIDs)
	}

	segStart := start
	segBegin := 0
	for i, pid := range portalIDs {
		p := m.portals[pid]
		if !p.offMesh {
			continue
		}
		out = q.stringPullSegmentInto(out, segStart, p.left, portalIDs[segBegin:i])
		if len(out) == 0 || !vequal(out[len(out)-1], p.endPos) {
			out = append(out, p.endPos)
		}
		segStart = p.endPos
		segBegin = i + 1
	}
	return q.stringPullSegmentInto(out, segStart, goal, portalIDs[segBegin:])
}

func (q *NavMeshQuery) stringPullSegmentInto(out []vec2, start, goal vec2, portalIDs []int) []vec2 {
	m := q.mesh
	q.funnelPortals = append(q.funnelPortals[:0], [2]vec2{start, start})
	for _, pid := range portalIDs {
		p := m.portals[pid]
		q.funnelPortals = append(q.funnelPortals, [2]vec2{p.left, p.right})
	}
	q.funnelPortals = append(q.funnelPortals, [2]vec2{goal, goal})

	if len(out) == 0 || !vequal(out[len(out)-1], start) {
		out = append(out, start)
	}
	portalApex := start
	portalLeft := start
	portalRight := start
	apexIndex := 0
	leftIndex := 0
	rightIndex := 0

	for i := 1; i < len(q.funnelPortals); i++ {
		left := q.funnelPortals[i][0]
		right := q.funnelPortals[i][1]

		if triArea2(portalApex, portalRight, right) >= 0 {
			if vequal(portalApex, portalRight) || vequal(portalApex, portalLeft) || triArea2(portalApex, portalLeft, right) < 0 {
				portalRight = right
				rightIndex = i
			} else {
				if !vequal(out[len(out)-1], portalLeft) {
					out = append(out, portalLeft)
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
				if !vequal(out[len(out)-1], portalRight) {
					out = append(out, portalRight)
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

	if !vequal(out[len(out)-1], goal) {
		out = append(out, goal)
	}
	return out
}

func simplifyCollinearInto(out, pts []vec2) []vec2 {
	if len(pts) <= 2 {
		return append(out[:0], pts...)
	}
	out = append(out[:0], pts[0])
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

// NearestPoint delegates to q's NavMesh.NearestPoint.
func (q *NavMeshQuery) NearestPoint(p, extent core.Point) (core.Point, int, bool) {
	if q == nil || q.mesh == nil {
		return core.Point{}, -1, false
	}
	return q.mesh.NearestPoint(p, extent)
}

// NearestPointWithFilter delegates to q's NavMesh.NearestPointWithFilter.
func (q *NavMeshQuery) NearestPointWithFilter(p, extent core.Point, filter NavFilter) (core.Point, int, bool) {
	if q == nil || q.mesh == nil {
		return core.Point{}, -1, false
	}
	return q.mesh.NearestPointWithFilter(p, extent, filter)
}

// Raycast delegates to q's NavMesh.Raycast.
func (q *NavMeshQuery) Raycast(start, target core.Point) (NavRaycastHit, bool) {
	if q == nil || q.mesh == nil {
		return NavRaycastHit{Point: start, Poly: -1, Hit: true}, true
	}
	return q.mesh.Raycast(start, target)
}

// RaycastWithFilter delegates to q's NavMesh.RaycastWithFilter.
func (q *NavMeshQuery) RaycastWithFilter(start, target core.Point, filter NavFilter) (NavRaycastHit, bool) {
	if q == nil || q.mesh == nil {
		return NavRaycastHit{Point: start, Poly: -1, Hit: true}, true
	}
	return q.mesh.RaycastWithFilter(start, target, filter)
}

// RandomPoint delegates to q's NavMesh.RandomPoint.
func (q *NavMeshQuery) RandomPoint(rng *random.Stream) (core.Point, int, bool) {
	if q == nil || q.mesh == nil {
		return core.Point{}, -1, false
	}
	return q.mesh.RandomPoint(rng)
}

// RandomPointWithFilter delegates to q's NavMesh.RandomPointWithFilter.
func (q *NavMeshQuery) RandomPointWithFilter(rng *random.Stream, filter NavFilter) (core.Point, int, bool) {
	if q == nil || q.mesh == nil {
		return core.Point{}, -1, false
	}
	return q.mesh.RandomPointWithFilter(rng, filter)
}

// RandomPointAround picks a random reachable point within radius of center using q's reusable BFS and clipping buffers.
func (q *NavMeshQuery) RandomPointAround(center core.Point, radius float32, rng *random.Stream) (core.Point, int, bool) {
	return q.RandomPointAroundWithFilter(center, radius, rng, DefaultNavFilter())
}

// RandomPointAroundWithFilter picks a random reachable point within radius of center allowed by filter using q's reusable buffers.
func (q *NavMeshQuery) RandomPointAroundWithFilter(center core.Point, radius float32, rng *random.Stream, filter NavFilter) (core.Point, int, bool) {
	if q == nil || q.mesh == nil {
		return core.Point{}, -1, false
	}
	return q.mesh.randomPointAround(center, radius, rng, filter, q)
}

// MoveAlongSurface delegates to q's NavMesh.MoveAlongSurface.
func (q *NavMeshQuery) MoveAlongSurface(start, target core.Point) (NavMoveResult, bool) {
	return q.MoveAlongSurfaceWithFilter(start, target, DefaultNavFilter())
}

// MoveAlongSurfaceWithFilter delegates to q's NavMesh.MoveAlongSurfaceWithFilter.
func (q *NavMeshQuery) MoveAlongSurfaceWithFilter(start, target core.Point, filter NavFilter) (NavMoveResult, bool) {
	if q == nil || q.mesh == nil {
		return NavMoveResult{Point: start, Poly: -1}, false
	}
	return q.mesh.moveAlongSurfaceInto(nil, start, target, filter)
}

// MoveAlongSurfaceInto slides from start toward target under DefaultNavFilter, appending visited polygon IDs into visitedDst[:0].
func (q *NavMeshQuery) MoveAlongSurfaceInto(visitedDst []int, start, target core.Point) (NavMoveResult, bool) {
	return q.MoveAlongSurfaceWithFilterInto(visitedDst, start, target, DefaultNavFilter())
}

// MoveAlongSurfaceWithFilterInto slides from start toward target under filter, appending visited polygon IDs into visitedDst[:0].
func (q *NavMeshQuery) MoveAlongSurfaceWithFilterInto(visitedDst []int, start, target core.Point, filter NavFilter) (NavMoveResult, bool) {
	if q == nil || q.mesh == nil {
		return NavMoveResult{Point: start, Poly: -1}, false
	}
	return q.mesh.moveAlongSurfaceInto(visitedDst, start, target, filter)
}

// FindPathInto behaves like FindPath but writes waypoints into dst[:0] and reuses the mesh's internal search buffers
// so repeated calls allocate zero heap memory once warmed up.
//
// FindPathInto is not safe for concurrent use on the same NavMesh; construct separate NewNavMeshQuery instances
// when running concurrent path searches against a shared read-only NavMesh.
func (m *NavMesh) FindPathInto(dst []core.Point, start, goal core.Point) ([]core.Point, bool) {
	return m.FindPathWithFilterInto(dst, start, goal, DefaultNavFilter())
}

// FindPathWithFilterInto behaves like FindPathWithFilter but writes waypoints into dst[:0] and reuses
// the mesh's internal search buffers with zero heap allocations after warm-up.
//
// FindPathWithFilterInto is not safe for concurrent use on the same NavMesh; use NewNavMeshQuery per goroutine.
func (m *NavMesh) FindPathWithFilterInto(dst []core.Point, start, goal core.Point, filter NavFilter) ([]core.Point, bool) {
	if m == nil {
		return nil, false
	}
	m.defaultQuery.mesh = m
	return m.defaultQuery.FindPathWithFilterInto(dst, start, goal, filter)
}

// FindPathWithFilterAndOptionsInto behaves like FindPathWithFilterAndOptions but writes waypoints into dst[:0]
// and reuses the mesh's internal search buffers with zero heap allocations after warm-up.
func (m *NavMesh) FindPathWithFilterAndOptionsInto(dst []core.Point, start, goal core.Point, filter NavFilter, opts PathOptions) NavPathResult {
	if m == nil {
		return NavPathResult{}
	}
	m.defaultQuery.mesh = m
	return m.defaultQuery.FindPathWithFilterAndOptionsInto(dst, start, goal, filter, opts)
}

// FindCorridorInto behaves like FindCorridor but writes polygon indices into dst[:0] and reuses
// the mesh's internal search buffers with zero heap allocations after warm-up.
//
// FindCorridorInto is not safe for concurrent use on the same NavMesh; use NewNavMeshQuery per goroutine.
func (m *NavMesh) FindCorridorInto(dst []int, start, goal core.Point) ([]int, bool) {
	return m.FindCorridorWithFilterInto(dst, start, goal, DefaultNavFilter())
}

// FindCorridorWithFilterInto behaves like FindCorridorWithFilter but writes polygon indices into dst[:0]
// and reuses the mesh's internal search buffers with zero heap allocations after warm-up.
func (m *NavMesh) FindCorridorWithFilterInto(dst []int, start, goal core.Point, filter NavFilter) ([]int, bool) {
	if m == nil {
		return nil, false
	}
	m.defaultQuery.mesh = m
	return m.defaultQuery.FindCorridorWithFilterInto(dst, start, goal, filter)
}
