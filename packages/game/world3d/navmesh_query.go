package world3d

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/random"
)

const navBVHLeafSize3D = 4

// navBVHNode3D is a flat, pointer-free 3D AABB bounding-volume hierarchy node.
// When count > 0, the node is a leaf covering bvhOrder[left : left+count].
// When count == 0, left and right are child node indices in bvhNodes.
type navBVHNode3D struct {
	bounds math3d.AABB
	left   int32
	right  int32
	count  int32
}

func (nm *NavMesh) buildBVH() {
	n := len(nm.Polys)
	if n == 0 {
		nm.bvhNodes = nil
		nm.bvhOrder = nil
		return
	}
	nm.bvhOrder = make([]int, n)
	for i := range n {
		nm.bvhOrder[i] = i
	}
	nm.bvhNodes = make([]navBVHNode3D, 0, max(1, (2*n)/navBVHLeafSize3D+4))
	nm.buildBVHNode(0, n, 0)
}

func (nm *NavMesh) buildBVHNode(start, end, depth int) int32 {
	nodeIdx := int32(len(nm.bvhNodes))
	nm.bvhNodes = append(nm.bvhNodes, navBVHNode3D{})

	p0 := &nm.Polys[nm.bvhOrder[start]]
	bounds := p0.Bounds
	cMin := p0.Center
	cMax := p0.Center
	for i := start + 1; i < end; i++ {
		p := &nm.Polys[nm.bvhOrder[i]]
		bounds = bounds.Union(p.Bounds)
		cMin = cMin.Min(p.Center)
		cMax = cMax.Max(p.Center)
	}

	count := end - start
	if count <= navBVHLeafSize3D || depth >= 56 {
		nm.bvhNodes[nodeIdx] = navBVHNode3D{
			bounds: bounds,
			left:   int32(start),
			count:  int32(count),
		}
		return nodeIdx
	}

	span := cMax.Sub(cMin)
	axis := 0 // 0=X, 1=Y, 2=Z
	if span.Y > span.X && span.Y >= span.Z {
		axis = 1
	} else if span.Z > span.X && span.Z > span.Y {
		axis = 2
	}

	axisVal := func(v math3d.Vec3) float32 {
		switch axis {
		case 1:
			return v.Y
		case 2:
			return v.Z
		default:
			return v.X
		}
	}

	splitVal := 0.5 * (axisVal(cMin) + axisVal(cMax))
	i, j := start, end-1
	for i <= j {
		if axisVal(nm.Polys[nm.bvhOrder[i]].Center) < splitVal {
			i++
		} else {
			nm.bvhOrder[i], nm.bvhOrder[j] = nm.bvhOrder[j], nm.bvhOrder[i]
			j--
		}
	}

	leftCount := i - start
	rightCount := end - i
	if leftCount == 0 || rightCount == 0 || leftCount < count/8 || rightCount < count/8 {
		sub := nm.bvhOrder[start:end]
		slices.SortFunc(sub, func(a, b int) int {
			ca := axisVal(nm.Polys[a].Center)
			cb := axisVal(nm.Polys[b].Center)
			if ca < cb {
				return -1
			}
			if ca > cb {
				return 1
			}
			return a - b
		})
		i = start + count/2
	}

	leftIdx := nm.buildBVHNode(start, i, depth+1)
	rightIdx := nm.buildBVHNode(i, end, depth+1)
	nm.bvhNodes[nodeIdx] = navBVHNode3D{
		bounds: bounds,
		left:   leftIdx,
		right:  rightIdx,
	}
	return nodeIdx
}

func (nm *NavMesh) rebuildPortalIndex() {
	n := len(nm.Polys)
	if cap(nm.linkOffset) < n+1 {
		nm.linkOffset = make([]int, n+1)
	} else {
		nm.linkOffset = nm.linkOffset[:n+1]
	}
	nm.linkOffset[0] = 0
	for i := range n {
		nm.linkOffset[i+1] = nm.linkOffset[i] + len(nm.Polys[i].Links)
	}
	total := nm.linkOffset[n]
	if cap(nm.portalFrom) < total {
		nm.portalFrom = make([]int, total)
		nm.portalLink = make([]NavLink, total)
	} else {
		nm.portalFrom = nm.portalFrom[:total]
		nm.portalLink = nm.portalLink[:total]
	}
	for i := range n {
		base := nm.linkOffset[i]
		for k, link := range nm.Polys[i].Links {
			nm.portalFrom[base+k] = i
			nm.portalLink[base+k] = link
		}
	}
}

func (nm *NavMesh) bvhQueryAABB(box math3d.AABB, dst []int) []int {
	if len(nm.bvhNodes) == 0 || len(nm.bvhOrder) != len(nm.Polys) {
		for i := range nm.Polys {
			if nm.Polys[i].Bounds.Intersects(box) {
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
		n := &nm.bvhNodes[stack[sp]]
		if !n.bounds.Intersects(box) {
			continue
		}
		if n.count > 0 {
			end := int(n.left + n.count)
			for k := int(n.left); k < end; k++ {
				idx := nm.bvhOrder[k]
				if nm.Polys[idx].Bounds.Intersects(box) {
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

func (nm *NavMesh) bvhFindPoly(p math3d.Vec3, tol float32, filter NavFilter) (int, bool) {
	const xzPad float32 = 1e-4
	minX, maxX := p.X-xzPad, p.X+xzPad
	minY, maxY := p.Y-tol-xzPad, p.Y+tol+xzPad
	minZ, maxZ := p.Z-xzPad, p.Z+xzPad

	bestIdx := -1
	bestDY := tol + 1

	checkPoly := func(idx int) {
		poly := &nm.Polys[idx]
		if !filter.Allows(poly.Area, poly.Flags) {
			return
		}
		if !poly.ContainsXZ(p.X, p.Z) {
			return
		}
		dy := float32(math.Abs(float64(p.Y - poly.HeightAt(p.X, p.Z))))
		if dy <= tol && (dy < bestDY || (dy == bestDY && (bestIdx < 0 || idx < bestIdx))) {
			bestDY = dy
			bestIdx = idx
		}
	}

	if len(nm.bvhNodes) == 0 || len(nm.bvhOrder) != len(nm.Polys) {
		for i := range nm.Polys {
			checkPoly(i)
		}
		return bestIdx, bestIdx >= 0
	}

	var stack [64]int32
	stack[0] = 0
	sp := 1
	for sp > 0 {
		sp--
		n := &nm.bvhNodes[stack[sp]]
		if n.bounds.Max.X < minX || n.bounds.Min.X > maxX ||
			n.bounds.Max.Y < minY || n.bounds.Min.Y > maxY ||
			n.bounds.Max.Z < minZ || n.bounds.Min.Z > maxZ {
			continue
		}
		if n.count > 0 {
			end := int(n.left + n.count)
			for k := int(n.left); k < end; k++ {
				checkPoly(nm.bvhOrder[k])
			}
			continue
		}
		stack[sp] = n.right
		sp++
		stack[sp] = n.left
		sp++
	}
	return bestIdx, bestIdx >= 0
}

// bvhFindPolysAt appends all polygons containing (p.X, p.Z) within vertical tolerance of the closest matching level
// into dst, sorted by ascending index.
func (nm *NavMesh) bvhFindPolysAt(p math3d.Vec3, tol float32, filter NavFilter, requireFiniteCost bool, dst []int) []int {
	const xzPad float32 = 1e-4
	const levelEps float32 = 1e-4
	minX, maxX := p.X-xzPad, p.X+xzPad
	minY, maxY := p.Y-tol-xzPad, p.Y+tol+xzPad
	minZ, maxZ := p.Z-xzPad, p.Z+xzPad

	dst = dst[:0]
	var dyBuf [32]float32
	dys := dyBuf[:0]
	bestDY := tol + 1

	consider := func(idx int) {
		poly := &nm.Polys[idx]
		if !filter.Allows(poly.Area, poly.Flags) {
			return
		}
		if requireFiniteCost {
			c := filter.AreaCost(poly.Area)
			if c <= 0 || math.IsNaN(c) || math.IsInf(c, 0) {
				return
			}
		}
		if !poly.ContainsXZ(p.X, p.Z) {
			return
		}
		dy := float32(math.Abs(float64(p.Y - poly.HeightAt(p.X, p.Z))))
		if dy <= tol {
			dst = append(dst, idx)
			dys = append(dys, dy)
			if dy < bestDY {
				bestDY = dy
			}
		}
	}

	if len(nm.bvhNodes) == 0 || len(nm.bvhOrder) != len(nm.Polys) {
		for i := range nm.Polys {
			consider(i)
		}
	} else {
		var stack [64]int32
		stack[0] = 0
		sp := 1
		for sp > 0 {
			sp--
			n := &nm.bvhNodes[stack[sp]]
			if n.bounds.Max.X < minX || n.bounds.Min.X > maxX ||
				n.bounds.Max.Y < minY || n.bounds.Min.Y > maxY ||
				n.bounds.Max.Z < minZ || n.bounds.Min.Z > maxZ {
				continue
			}
			if n.count > 0 {
				end := int(n.left + n.count)
				for k := int(n.left); k < end; k++ {
					consider(nm.bvhOrder[k])
				}
				continue
			}
			stack[sp] = n.right
			sp++
			stack[sp] = n.left
			sp++
		}
	}

	// Keep only polygons on the vertically closest level.
	outLen := 0
	for i, idx := range dst {
		if dys[i] <= bestDY+levelEps {
			dst[outLen] = idx
			outLen++
		}
	}
	dst = dst[:outLen]

	// Insertion sort tiny result slice for deterministic ascending index order.
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

func aabbDistSq3D(b math3d.AABB, p math3d.Vec3) float64 {
	var dx, dy, dz float64
	if p.X < b.Min.X {
		dx = float64(b.Min.X - p.X)
	} else if p.X > b.Max.X {
		dx = float64(p.X - b.Max.X)
	}
	if p.Y < b.Min.Y {
		dy = float64(b.Min.Y - p.Y)
	} else if p.Y > b.Max.Y {
		dy = float64(p.Y - b.Max.Y)
	}
	if p.Z < b.Min.Z {
		dz = float64(b.Min.Z - p.Z)
	} else if p.Z > b.Max.Z {
		dz = float64(p.Z - b.Max.Z)
	}
	return dx*dx + dy*dy + dz*dz
}

type bvhDistItem3D struct {
	idx    int32
	distSq float64
}

func (nm *NavMesh) bvhNearestPoly(p, ext math3d.Vec3, useExtent bool, filter NavFilter) (int, math3d.Vec3, bool) {
	const eps float32 = 1e-4
	queryBox := math3d.AABB{
		Min: math3d.Vec3{X: p.X - ext.X - eps, Y: p.Y - ext.Y - eps, Z: p.Z - ext.Z - eps},
		Max: math3d.Vec3{X: p.X + ext.X + eps, Y: p.Y + ext.Y + eps, Z: p.Z + ext.Z + eps},
	}
	pd := toVec3d(p)
	cMinX, cMinY, cMinZ := pd.x-float64(ext.X), pd.y-float64(ext.Y), pd.z-float64(ext.Z)
	cMaxX, cMaxY, cMaxZ := pd.x+float64(ext.X), pd.y+float64(ext.Y), pd.z+float64(ext.Z)

	bestIdx := -1
	bestDistSq := math.Inf(1)
	var bestPt math3d.Vec3
	var clipA, clipB [24]vec3d

	checkPoly := func(idx int) {
		poly := &nm.Polys[idx]
		if !filter.Allows(poly.Area, poly.Flags) {
			return
		}
		if useExtent && !poly.Bounds.Intersects(queryBox) {
			return
		}
		if aabbDistSq3D(poly.Bounds, p) > bestDistSq {
			return
		}
		cp := closestPointOnNavPoly(poly, p)
		if useExtent && (math.Abs(float64(cp.X-p.X)) > float64(ext.X)+float64(eps) ||
			math.Abs(float64(cp.Y-p.Y)) > float64(ext.Y)+float64(eps) ||
			math.Abs(float64(cp.Z-p.Z)) > float64(ext.Z)+float64(eps)) {
			clipped := clipPolyAABB3DAny(poly.Vertices, cMinX, cMinY, cMinZ, cMaxX, cMaxY, cMaxZ, clipA[:0], clipB[:0])
			if len(clipped) == 0 {
				return
			}
			cp = toVec3f(closestPointOnConvex3Dd(clipped, poly, pd))
		}
		d2 := distSq3D(p, cp)
		if d2 < bestDistSq || (d2 == bestDistSq && (bestIdx < 0 || idx < bestIdx)) {
			bestDistSq = d2
			bestIdx = idx
			bestPt = cp
		}
	}

	if len(nm.bvhNodes) == 0 || len(nm.bvhOrder) != len(nm.Polys) {
		for i := range nm.Polys {
			checkPoly(i)
		}
		return bestIdx, bestPt, bestIdx >= 0
	}

	root := &nm.bvhNodes[0]
	if useExtent && !root.bounds.Intersects(queryBox) {
		return -1, math3d.Vec3{}, false
	}

	var stack [64]bvhDistItem3D
	stack[0] = bvhDistItem3D{idx: 0, distSq: aabbDistSq3D(root.bounds, p)}
	sp := 1
	for sp > 0 {
		sp--
		item := stack[sp]
		if item.distSq > bestDistSq {
			continue
		}
		n := &nm.bvhNodes[item.idx]
		if n.count > 0 {
			end := int(n.left + n.count)
			for k := int(n.left); k < end; k++ {
				checkPoly(nm.bvhOrder[k])
			}
			continue
		}

		lNode := &nm.bvhNodes[n.left]
		rNode := &nm.bvhNodes[n.right]
		lOk := !useExtent || lNode.bounds.Intersects(queryBox)
		rOk := !useExtent || rNode.bounds.Intersects(queryBox)
		lDist := math.Inf(1)
		if lOk {
			lDist = aabbDistSq3D(lNode.bounds, p)
		}
		rDist := math.Inf(1)
		if rOk {
			rDist = aabbDistSq3D(rNode.bounds, p)
		}

		if lDist <= rDist {
			if rOk && rDist <= bestDistSq {
				stack[sp] = bvhDistItem3D{idx: n.right, distSq: rDist}
				sp++
			}
			if lOk && lDist <= bestDistSq {
				stack[sp] = bvhDistItem3D{idx: n.left, distSq: lDist}
				sp++
			}
		} else {
			if lOk && lDist <= bestDistSq {
				stack[sp] = bvhDistItem3D{idx: n.left, distSq: lDist}
				sp++
			}
			if rOk && rDist <= bestDistSq {
				stack[sp] = bvhDistItem3D{idx: n.right, distSq: rDist}
				sp++
			}
		}
	}
	return bestIdx, bestPt, bestIdx >= 0
}

// NearestPoint returns the closest 3D point on any walkable polygon allowed by DefaultNavFilter
// within the 3D axis-aligned search half-extents [p - extent, p + extent], together with that polygon's index.
//
// Because the search bounds are 3D, stacked floors and bridges outside [p.Y-extent.Y, p.Y+extent.Y]
// are cleanly excluded. Returns (zero, -1, false) if nm is empty, inputs are invalid/negative,
// or no allowed polygon point lies within extent.
func (nm *NavMesh) NearestPoint(p, extent math3d.Vec3) (point math3d.Vec3, poly int, ok bool) {
	return nm.NearestPointWithFilter(p, extent, DefaultNavFilter())
}

// NearestPointWithFilter behaves like NearestPoint while restricting matches to polygons allowed by filter.
func (nm *NavMesh) NearestPointWithFilter(p, extent math3d.Vec3, filter NavFilter) (point math3d.Vec3, poly int, ok bool) {
	if nm == nil || len(nm.Polys) == 0 || !isFiniteVec3(p) || !isFiniteVec3(extent) ||
		extent.X < 0 || extent.Y < 0 || extent.Z < 0 {
		return math3d.Vec3{}, -1, false
	}
	idx, pt, found := nm.bvhNearestPoly(p, extent, true, filter)
	if !found {
		return math3d.Vec3{}, -1, false
	}
	return pt, idx, true
}

// NavRaycastHit describes the result of casting a segment along a 3D NavMesh surface.
type NavRaycastHit struct {
	// Point is the 3D impact point on the obstructing boundary edge when Hit is true,
	// or the surface-snapped target point when the ray reaches target unobstructed (Hit is false).
	Point math3d.Vec3
	// Normal is the unit 3D surface-tangent inward normal of the boundary edge that stopped the ray
	// (pointing back into the walkable polygon, opposing the ray), or zero when Hit is false or Poly < 0.
	Normal math3d.Vec3
	// Poly is the index of the polygon containing Point, or -1 if start lies outside the allowed mesh.
	Poly int
	// T is the normalized horizontal progression along start->target in [0, 1].
	// T is 1 when target is reached unobstructed; T is 0 when start lies outside the allowed mesh.
	T float32
	// Hit is true if a mesh boundary edge or disallowed polygon obstructed the ray (or start is outside the allowed mesh).
	Hit bool
}

// Raycast casts a segment along the 3D mesh surface from start toward target using DefaultNavFilter,
// walking shared 3D polygon edges across allowed polygons until reaching target or hitting a boundary edge
// or disallowed polygon. Vertically stacked levels never falsely connect because only shared 3D edges are crossed.
//
// The second return value equals hit.Hit: false means unobstructed traversal to target (hit.T == 1, hit.Poly >= 0);
// true means the segment was obstructed (or start lies outside the allowed mesh, with hit.Poly == -1 and hit.T == 0).
func (nm *NavMesh) Raycast(start, target math3d.Vec3) (NavRaycastHit, bool) {
	return nm.RaycastWithFilter(start, target, DefaultNavFilter())
}

// RaycastWithFilter behaves like Raycast while restricting traversal to polygons allowed by filter.
func (nm *NavMesh) RaycastWithFilter(start, target math3d.Vec3, filter NavFilter) (NavRaycastHit, bool) {
	if nm == nil || len(nm.Polys) == 0 || !isFiniteVec3(start) || !isFiniteVec3(target) {
		hit := NavRaycastHit{Point: start, Poly: -1, T: 0, Hit: true}
		return hit, true
	}
	tol := nm.VerticalTolerance
	if tol <= 0 {
		tol = defaultVerticalTolerance
	}
	var startBuf [16]int
	starts := nm.bvhFindPolysAt(start, tol, filter, false, startBuf[:0])
	if len(starts) == 0 {
		hit := NavRaycastHit{Point: start, Poly: -1, T: 0, Hit: true}
		return hit, true
	}
	startPoly := nm.pickBestRayStartPoly(starts, start, target)
	hit, _, _, _ := nm.raycastFromPolyXZ(startPoly, float64(start.X), float64(start.Z), float64(target.X), float64(target.Z), filter, nil, false)
	return hit, hit.Hit
}

func (nm *NavMesh) pickBestRayStartPoly(starts []int, start, target math3d.Vec3) int {
	if len(starts) == 1 || sameXZ(start, target) {
		return starts[0]
	}
	dx := float64(target.X - start.X)
	dz := float64(target.Z - start.Z)
	bestPoly := starts[0]
	bestT := -1.0
	for _, idx := range starts {
		if nm.Polys[idx].ContainsXZ(target.X, target.Z) {
			return idx
		}
		tExit, _, _, ok := polyRayExitXZ(&nm.Polys[idx], float64(start.X), float64(start.Z), dx, dz)
		if ok && tExit > bestT {
			bestT = tExit
			bestPoly = idx
		}
	}
	return bestPoly
}

func polyInwardNormal3D(poly *NavPoly, edgeIdx int) math3d.Vec3 {
	vs := poly.Vertices
	n := len(vs)
	a := vs[edgeIdx]
	b := vs[(edgeIdx+1)%n]
	edge3D := b.Sub(a)
	inward3D := poly.Normal.Cross(edge3D)
	if inward3D.LenSq() > 1e-12 {
		return inward3D.Norm()
	}
	ex := b.X - a.X
	ez := b.Z - a.Z
	return math3d.Vec3{X: ez, Z: -ex}.Norm()
}

// polyRayExitXZ returns the smallest parameter t >= 0 where XZ ray (sx, sz) + t*(dx, dz) exits CCW-from-+Y polygon poly,
// together with the 3D surface-tangent inward unit normal and the hit edge index.
func polyRayExitXZ(poly *NavPoly, sx, sz, dx, dz float64) (tExit float64, normal math3d.Vec3, edgeIdx int, ok bool) {
	dirLen := math.Hypot(dx, dz)
	if dirLen <= 1e-12 {
		return 0, math3d.Vec3{}, -1, false
	}
	vs := poly.Vertices
	n := len(vs)
	bestT := math.Inf(1)
	bestEdge := -1
	minDenom := -1e-9 * dirLen
	for i := range n {
		a := vs[i]
		b := vs[(i+1)%n]
		ex := float64(b.X - a.X)
		ez := float64(b.Z - a.Z)
		eLen := math.Hypot(ex, ez)
		if eLen <= 1e-12 {
			continue
		}
		// For CCW polygons viewed from +Y, inward XZ normal is (ez, -ex) / eLen.
		nx := ez / eLen
		nz := -ex / eLen
		denom := dx*nx + dz*nz
		if denom >= minDenom {
			continue
		}
		t := ((float64(a.X)-sx)*nx + (float64(a.Z)-sz)*nz) / denom
		if t < bestT {
			bestT = t
			bestEdge = i
		}
	}
	if bestEdge < 0 || math.IsInf(bestT, 1) {
		return 0, math3d.Vec3{}, -1, false
	}
	if bestT < 0 {
		bestT = 0
	}
	return bestT, polyInwardNormal3D(poly, bestEdge), bestEdge, true
}

// obstructingEdgeNormal3D resolves the 3D surface-tangent inward normal at (ex, ez),
// preferring a non-portal boundary (or disallowed neighbor) edge when a ray hits a shared corner.
func (nm *NavMesh) obstructingEdgeNormal3D(poly *NavPoly, sx, sz, dx, dz, ex, ez, tExit float64, fallback math3d.Vec3, filter NavFilter) math3d.Vec3 {
	dirLen := math.Hypot(dx, dz)
	if dirLen <= 1e-12 {
		return fallback
	}
	vs := poly.Vertices
	n := len(vs)
	minDenom := -1e-9 * dirLen
	for i := range n {
		a := vs[i]
		b := vs[(i+1)%n]
		edx := float64(b.X - a.X)
		edz := float64(b.Z - a.Z)
		eLen := math.Hypot(edx, edz)
		if eLen <= 1e-12 {
			continue
		}
		nx := edz / eLen
		nz := -edx / eLen
		denom := dx*nx + dz*nz
		if denom >= minDenom {
			continue
		}
		t := ((float64(a.X)-sx)*nx + (float64(a.Z)-sz)*nz) / denom
		if t < 0 {
			t = 0
		}
		if math.Abs(t-tExit) > 1e-6 {
			continue
		}
		openPortal := false
		for _, link := range poly.Links {
			if link.OffMesh {
				continue
			}
			np := &nm.Polys[link.Neighbor]
			if !filter.Allows(np.Area, np.Flags) {
				continue
			}
			if pointOnSegmentXZ(ex, ez, link.Left, link.Right, sharedEdgeEps) &&
				pointOnSegmentXZ(float64(link.Left.X), float64(link.Left.Z), a, b, sharedEdgeEps) &&
				pointOnSegmentXZ(float64(link.Right.X), float64(link.Right.Z), a, b, sharedEdgeEps) {
				openPortal = true
				break
			}
		}
		if !openPortal {
			return polyInwardNormal3D(poly, i)
		}
	}
	return fallback
}

func pointOnSegmentXZ(px, pz float64, a, b math3d.Vec3, eps float64) bool {
	ax, az := float64(a.X), float64(a.Z)
	bx, bz := float64(b.X), float64(b.Z)
	abx, abz := bx-ax, bz-az
	l2 := abx*abx + abz*abz
	var cx, cz float64
	if l2 <= 1e-18 {
		cx, cz = ax, az
	} else {
		t := ((px-ax)*abx + (pz-az)*abz) / l2
		t = min(max(t, 0), 1)
		cx = ax + abx*t
		cz = az + abz*t
	}
	dx, dz := px-cx, pz-cz
	return dx*dx+dz*dz <= eps*eps
}

func (nm *NavMesh) raycastFromPolyXZ(startPoly int, sx, sz, tx, tz float64, filter NavFilter, visitedOut []int, recordVisited bool) (NavRaycastHit, float64, float64, []int) {
	if recordVisited && (len(visitedOut) == 0 || visitedOut[len(visitedOut)-1] != startPoly) {
		visitedOut = append(visitedOut, startPoly)
	}
	dx, dz := tx-sx, tz-sz
	tfX, tfZ := float32(tx), float32(tz)
	if dx*dx+dz*dz < 1e-12 || nm.Polys[startPoly].ContainsXZ(tfX, tfZ) {
		pt := math3d.Vec3{X: tfX, Y: nm.Polys[startPoly].HeightAt(tfX, tfZ), Z: tfZ}
		return NavRaycastHit{
			Point: pt,
			Poly:  startPoly,
			T:     1,
			Hit:   false,
		}, tx, tz, visitedOut
	}

	curPoly := startPoly
	curT := 0.0

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

	maxSteps := len(nm.Polys) + 2
	for step := 0; step < maxSteps; step++ {
		poly := &nm.Polys[curPoly]
		if poly.ContainsXZ(tfX, tfZ) {
			pt := math3d.Vec3{X: tfX, Y: poly.HeightAt(tfX, tfZ), Z: tfZ}
			return NavRaycastHit{
				Point: pt,
				Poly:  curPoly,
				T:     1,
				Hit:   false,
			}, tx, tz, visitedOut
		}

		tExit, exitNorm, _, ok := polyRayExitXZ(poly, sx, sz, dx, dz)
		if !ok {
			ex, ez := sx+dx*curT, sz+dz*curT
			px, pz := float32(ex), float32(ez)
			return NavRaycastHit{
				Point: math3d.Vec3{X: px, Y: poly.HeightAt(px, pz), Z: pz},
				Poly:  curPoly,
				T:     float32(curT),
				Hit:   true,
			}, ex, ez, visitedOut
		}
		if tExit < curT {
			tExit = curT
		}
		if tExit >= 1.0-1e-9 {
			if poly.ContainsXZ(tfX, tfZ) {
				pt := math3d.Vec3{X: tfX, Y: poly.HeightAt(tfX, tfZ), Z: tfZ}
				return NavRaycastHit{
					Point: pt,
					Poly:  curPoly,
					T:     1,
					Hit:   false,
				}, tx, tz, visitedOut
			}
			tExit = 1.0
		}

		ex := sx + dx*tExit
		ez := sz + dz*tExit

		nextPoly := -1
		bestNextT := tExit + 1e-9
		reachedTarget := false
		for _, link := range poly.Links {
			if link.OffMesh || hasVisited(link.Neighbor) {
				continue
			}
			np := &nm.Polys[link.Neighbor]
			if !filter.Allows(np.Area, np.Flags) {
				continue
			}
			if !pointOnSegmentXZ(ex, ez, link.Left, link.Right, sharedEdgeEps) {
				continue
			}
			if np.ContainsXZ(tfX, tfZ) {
				nextPoly = link.Neighbor
				reachedTarget = true
				break
			}
			nt, _, _, nok := polyRayExitXZ(np, sx, sz, dx, dz)
			if nok && (nt > bestNextT || (nextPoly >= 0 && nt == bestNextT && link.Neighbor < nextPoly)) {
				bestNextT = nt
				nextPoly = link.Neighbor
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
				np := &nm.Polys[curPoly]
				pt := math3d.Vec3{X: tfX, Y: np.HeightAt(tfX, tfZ), Z: tfZ}
				return NavRaycastHit{
					Point: pt,
					Poly:  curPoly,
					T:     1,
					Hit:   false,
				}, tx, tz, visitedOut
			}
			continue
		}

		bridgePoly, bridgeChain, bridgeLen := nm.findCornerBridge3D(curPoly, ex, ez, sx, sz, dx, dz, tfX, tfZ, tExit, filter, hasVisited)
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

		exitNorm = nm.obstructingEdgeNormal3D(poly, sx, sz, dx, dz, ex, ez, tExit, exitNorm, filter)
		hx := float32(ex)
		hz := float32(ez)
		return NavRaycastHit{
			Point:  math3d.Vec3{X: hx, Y: poly.HeightAt(hx, hz), Z: hz},
			Normal: exitNorm,
			Poly:   curPoly,
			T:      float32(min(max(tExit, 0), 1)),
			Hit:    true,
		}, ex, ez, visitedOut
	}

	ex, ez := sx+dx*curT, sz+dz*curT
	px, pz := float32(ex), float32(ez)
	return NavRaycastHit{
		Point: math3d.Vec3{X: px, Y: nm.Polys[curPoly].HeightAt(px, pz), Z: pz},
		Poly:  curPoly,
		T:     float32(min(max(curT, 0), 1)),
		Hit:   true,
	}, ex, ez, visitedOut
}

func (nm *NavMesh) findCornerBridge3D(
	startPoly int,
	ex, ez, sx, sz, dx, dz float64,
	tfX, tfZ float32,
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
		for _, link := range nm.Polys[cur].Links {
			if link.OffMesh || alreadyVisited(link.Neighbor) || inLocal(link.Neighbor) {
				continue
			}
			np := &nm.Polys[link.Neighbor]
			if !filter.Allows(np.Area, np.Flags) {
				continue
			}
			if !pointOnSegmentXZ(ex, ez, link.Left, link.Right, sharedEdgeEps) {
				continue
			}
			if tail >= maxLocal {
				break
			}
			childIdx := tail
			qPoly[tail] = link.Neighbor
			qParent[tail] = curIdx
			tail++

			advances := np.ContainsXZ(tfX, tfZ)
			if !advances {
				if nt, _, _, nok := polyRayExitXZ(np, sx, sz, dx, dz); nok && nt > tExit+1e-9 {
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
				return link.Neighbor, outChain, n
			}
		}
	}
	return -1, outChain, 0
}

// NavMoveResult describes the outcome of sliding a 3D position across a world3d.NavMesh surface.
type NavMoveResult struct {
	// Point is the final 3D position clamped onto the mesh surface.
	Point math3d.Vec3
	// Poly is the index of the final polygon containing Point, or -1 if start lies outside the allowed mesh.
	Poly int
	// Visited is the ordered sequence of polygon indices traversed from start to Point.
	Visited []int
	// HitBoundary is true if movement was constrained by a mesh boundary edge or disallowed polygon.
	HitBoundary bool
}

// MoveAlongSurface slides an agent from start toward target while staying on 3D polygons allowed by DefaultNavFilter.
// Vertically stacked levels stay cleanly separated because only shared 3D polygon edges are traversed.
//
// Returns (NavMoveResult{Point: start, Poly: -1}, false) if start lies outside the allowed mesh.
func (nm *NavMesh) MoveAlongSurface(start, target math3d.Vec3) (NavMoveResult, bool) {
	return nm.MoveAlongSurfaceWithFilter(start, target, DefaultNavFilter())
}

// MoveAlongSurfaceWithFilter slides an agent from start toward target while staying on polygons allowed by filter.
func (nm *NavMesh) MoveAlongSurfaceWithFilter(start, target math3d.Vec3, filter NavFilter) (NavMoveResult, bool) {
	return nm.moveAlongSurfaceInto(nil, start, target, filter)
}

func (nm *NavMesh) moveAlongSurfaceInto(visitedDst []int, start, target math3d.Vec3, filter NavFilter) (NavMoveResult, bool) {
	if nm == nil || len(nm.Polys) == 0 || !isFiniteVec3(start) || !isFiniteVec3(target) {
		return NavMoveResult{Point: start, Poly: -1}, false
	}
	tol := nm.VerticalTolerance
	if tol <= 0 {
		tol = defaultVerticalTolerance
	}
	var startBuf [16]int
	starts := nm.bvhFindPolysAt(start, tol, filter, false, startBuf[:0])
	if len(starts) == 0 {
		return NavMoveResult{Point: start, Poly: -1}, false
	}

	curPoly := nm.pickBestRayStartPoly(starts, start, target)
	curX, curZ := float64(start.X), float64(start.Z)
	desX, desZ := float64(target.X), float64(target.Z)
	origX, origZ := desX, desZ
	visited := append(visitedDst[:0], curPoly)
	hitBoundary := false

	const maxSlideIters = 8
	for range maxSlideIters {
		var hit NavRaycastHit
		hit, curX, curZ, visited = nm.raycastFromPolyXZ(curPoly, curX, curZ, desX, desZ, filter, visited, true)
		curPoly = hit.Poly
		if !hit.Hit {
			break
		}
		hitBoundary = true
		// Project remaining horizontal XZ displacement onto the hit boundary edge's XZ tangent.
		nx, nz := float64(hit.Normal.X), float64(hit.Normal.Z)
		nLen := math.Hypot(nx, nz)
		if nLen <= 1e-9 {
			break
		}
		nx /= nLen
		nz /= nLen
		remX := desX - curX
		remZ := desZ - curZ
		outComp := remX*nx + remZ*nz
		slideX := remX - nx*outComp
		slideZ := remZ - nz*outComp
		if slideX*slideX+slideZ*slideZ <= 1e-10 {
			break
		}
		// Require monotonic progress toward target in XZ to avoid corner oscillation.
		toTargetX := origX - curX
		toTargetZ := origZ - curZ
		if slideX*toTargetX+slideZ*toTargetZ <= 1e-9 {
			break
		}
		desX = curX + slideX
		desZ = curZ + slideZ
		if (desX-curX)*(desX-curX)+(desZ-curZ)*(desZ-curZ) < 1e-12 {
			break
		}
	}

	// Clamp final point strictly onto curPoly.
	fx, fz := float32(curX), float32(curZ)
	cur := closestPointOnNavPoly(&nm.Polys[curPoly], math3d.Vec3{X: fx, Y: nm.Polys[curPoly].HeightAt(fx, fz), Z: fz})
	return NavMoveResult{
		Point:       cur,
		Poly:        curPoly,
		Visited:     visited,
		HitBoundary: hitBoundary,
	}, true
}

type vec3d struct {
	x, y, z float64
}

func toVec3d(v math3d.Vec3) vec3d {
	return vec3d{float64(v.X), float64(v.Y), float64(v.Z)}
}

func toVec3f(v vec3d) math3d.Vec3 {
	return math3d.Vec3{X: float32(v.x), Y: float32(v.y), Z: float32(v.z)}
}

func triArea3Dd(a, b, c vec3d) float64 {
	ux, uy, uz := b.x-a.x, b.y-a.y, b.z-a.z
	vx, vy, vz := c.x-a.x, c.y-a.y, c.z-a.z
	cx := float64(uy*vz) - float64(uz*vy)
	cy := float64(uz*vx) - float64(ux*vz)
	cz := float64(ux*vy) - float64(uy*vx)
	return 0.5 * math.Sqrt(float64(cx*cx)+float64(cy*cy)+float64(cz*cz))
}

func polySurfaceArea3D(verts []math3d.Vec3) float64 {
	if len(verts) < 3 {
		return 0
	}
	v0 := toVec3d(verts[0])
	var sum float64
	for i := 1; i < len(verts)-1; i++ {
		sum += triArea3Dd(v0, toVec3d(verts[i]), toVec3d(verts[i+1]))
	}
	return sum
}

func polySurfaceArea3Dd(verts []vec3d) float64 {
	if len(verts) < 3 {
		return 0
	}
	v0 := verts[0]
	var sum float64
	for i := 1; i < len(verts)-1; i++ {
		sum += triArea3Dd(v0, verts[i], verts[i+1])
	}
	return sum
}

func (nm *NavMesh) polyAreaAt(i int) float64 {
	if i < len(nm.polyAreas) {
		return nm.polyAreas[i]
	}
	return polySurfaceArea3D(nm.Polys[i].Vertices)
}

// RandomPoint picks a 3D surface-area-weighted uniformly random point anywhere on walkable polygons
// allowed by DefaultNavFilter using rng. Results are bit-identical across runs and platforms for the same seed.
//
// Returns (zero, -1, false) if nm is empty, rng is nil, or no polygons are allowed.
func (nm *NavMesh) RandomPoint(rng *random.Stream) (point math3d.Vec3, poly int, ok bool) {
	return nm.RandomPointWithFilter(rng, DefaultNavFilter())
}

// RandomPointWithFilter behaves like RandomPoint while restricting sampling to polygons allowed by filter.
func (nm *NavMesh) RandomPointWithFilter(rng *random.Stream, filter NavFilter) (point math3d.Vec3, poly int, ok bool) {
	if nm == nil || len(nm.Polys) == 0 || rng == nil {
		return math3d.Vec3{}, -1, false
	}
	var totalArea float64
	lastAllowed := -1
	for i := range nm.Polys {
		p := &nm.Polys[i]
		if !filter.Allows(p.Area, p.Flags) {
			continue
		}
		a := nm.polyAreaAt(i)
		if a <= 0 {
			continue
		}
		totalArea += a
		lastAllowed = i
	}
	if lastAllowed < 0 || totalArea <= 0 {
		return math3d.Vec3{}, -1, false
	}

	target := float64(rng.Float64() * totalArea)
	var acc float64
	chosen := lastAllowed
	for i := range nm.Polys {
		p := &nm.Polys[i]
		if !filter.Allows(p.Area, p.Flags) {
			continue
		}
		a := nm.polyAreaAt(i)
		if a <= 0 {
			continue
		}
		acc += a
		if target < acc {
			chosen = i
			break
		}
	}

	var vBuf [16]vec3d
	vs := vBuf[:0]
	for _, v := range nm.Polys[chosen].Vertices {
		vs = append(vs, toVec3d(v))
	}
	pt := sampleConvexPoly3Dd(vs, rng)
	return toVec3f(pt), chosen, true
}

func sampleConvexPoly3Dd(verts []vec3d, rng *random.Stream) vec3d {
	n := len(verts)
	if n == 0 {
		return vec3d{}
	}
	if n < 3 {
		return verts[0]
	}
	triIdx := 1
	if n > 3 {
		var total float64
		for i := 1; i < n-1; i++ {
			total += triArea3Dd(verts[0], verts[i], verts[i+1])
		}
		if total > 0 {
			r := float64(rng.Float64() * total)
			var acc float64
			triIdx = n - 2
			for i := 1; i < n-1; i++ {
				acc += triArea3Dd(verts[0], verts[i], verts[i+1])
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
	// Explicit float64(...) prevents platform-dependent FMA contraction per Go spec.
	return vec3d{
		x: float64(w*a.x) + float64(u*b.x) + float64(v*c.x),
		y: float64(w*a.y) + float64(u*b.y) + float64(v*c.y),
		z: float64(w*a.z) + float64(u*b.z) + float64(v*c.z),
	}
}

func clipPolyAABB3DAny(verts []math3d.Vec3, minX, minY, minZ, maxX, maxY, maxZ float64, bufA, bufB []vec3d) []vec3d {
	src := bufA[:0]
	for _, v := range verts {
		src = append(src, toVec3d(v))
	}

	clipPlane := func(in, out []vec3d, inside func(vec3d) bool, intersect func(vec3d, vec3d) vec3d) []vec3d {
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

	lerp := func(a, b vec3d, t float64) vec3d {
		return vec3d{
			x: a.x + float64(t*(b.x-a.x)),
			y: a.y + float64(t*(b.y-a.y)),
			z: a.z + float64(t*(b.z-a.z)),
		}
	}

	src = clipPlane(src, bufB[:0],
		func(p vec3d) bool { return p.x >= minX },
		func(a, b vec3d) vec3d {
			v := lerp(a, b, (minX-a.x)/(b.x-a.x))
			v.x = minX
			return v
		},
	)
	if len(src) == 0 {
		return src[:0]
	}
	src = clipPlane(src, bufA[:0],
		func(p vec3d) bool { return p.x <= maxX },
		func(a, b vec3d) vec3d {
			v := lerp(a, b, (maxX-a.x)/(b.x-a.x))
			v.x = maxX
			return v
		},
	)
	if len(src) == 0 {
		return src[:0]
	}
	src = clipPlane(src, bufB[:0],
		func(p vec3d) bool { return p.y >= minY },
		func(a, b vec3d) vec3d {
			v := lerp(a, b, (minY-a.y)/(b.y-a.y))
			v.y = minY
			return v
		},
	)
	if len(src) == 0 {
		return src[:0]
	}
	src = clipPlane(src, bufA[:0],
		func(p vec3d) bool { return p.y <= maxY },
		func(a, b vec3d) vec3d {
			v := lerp(a, b, (maxY-a.y)/(b.y-a.y))
			v.y = maxY
			return v
		},
	)
	if len(src) == 0 {
		return src[:0]
	}
	src = clipPlane(src, bufB[:0],
		func(p vec3d) bool { return p.z >= minZ },
		func(a, b vec3d) vec3d {
			v := lerp(a, b, (minZ-a.z)/(b.z-a.z))
			v.z = minZ
			return v
		},
	)
	if len(src) == 0 {
		return src[:0]
	}
	src = clipPlane(src, bufA[:0],
		func(p vec3d) bool { return p.z <= maxZ },
		func(a, b vec3d) vec3d {
			v := lerp(a, b, (maxZ-a.z)/(b.z-a.z))
			v.z = maxZ
			return v
		},
	)
	return src
}

func clipPolyAABB3D(verts []math3d.Vec3, minX, minY, minZ, maxX, maxY, maxZ float64, bufA, bufB []vec3d) []vec3d {
	src := clipPolyAABB3DAny(verts, minX, minY, minZ, maxX, maxY, maxZ, bufA, bufB)
	if len(src) < 3 {
		return src[:0]
	}
	return src
}

func closestPointOnSegment3Dd(p, a, b vec3d) vec3d {
	abx, aby, abz := b.x-a.x, b.y-a.y, b.z-a.z
	l2 := float64(abx*abx) + float64(aby*aby) + float64(abz*abz)
	if l2 <= 1e-18 {
		return a
	}
	num := float64((p.x-a.x)*abx) + float64((p.y-a.y)*aby) + float64((p.z-a.z)*abz)
	t := min(max(num/l2, 0), 1)
	return vec3d{
		x: a.x + float64(abx*t),
		y: a.y + float64(aby*t),
		z: a.z + float64(abz*t),
	}
}

// closestPointOnConvex3Dd returns the closest point to p on a non-empty planar convex 3D polygon
// (or 1-/2-vertex degenerate intersection) lying on poly's supporting plane, using FMA-free float64 math.
func closestPointOnConvex3Dd(verts []vec3d, poly *NavPoly, p vec3d) vec3d {
	n := len(verts)
	if n == 1 {
		return verts[0]
	}
	if n == 2 {
		return closestPointOnSegment3Dd(p, verts[0], verts[1])
	}
	nx, ny, nz := float64(poly.Normal.X), float64(poly.Normal.Y), float64(poly.Normal.Z)
	planeD := float64(poly.PlaneD)
	signedDist := float64(nx*p.x) + float64(ny*p.y) + float64(nz*p.z) + planeD
	proj := vec3d{
		x: p.x - float64(nx*signedDist),
		y: p.y - float64(ny*signedDist),
		z: p.z - float64(nz*signedDist),
	}
	inside := true
	for i := range n {
		a := verts[i]
		b := verts[(i+1)%n]
		edx, edz := b.x-a.x, b.z-a.z
		eLen := math.Sqrt(float64(edx*edx) + float64(edz*edz))
		// For CCW polygons viewed from +Y, interior points satisfy (b.x-a.x)*(p.z-a.z) - (b.z-a.z)*(p.x-a.x) <= 0.
		cross := float64(edx*(proj.z-a.z)) - float64(edz*(proj.x-a.x))
		if cross > 1e-4*eLen+1e-6 {
			inside = false
			break
		}
	}
	if inside {
		if math.Abs(ny) >= 1e-7 {
			proj.y = -(float64(nx*proj.x) + float64(nz*proj.z) + planeD) / ny
		}
		return proj
	}
	bestDistSq := math.Inf(1)
	var bestPt vec3d
	for i := range n {
		cp := closestPointOnSegment3Dd(p, verts[i], verts[(i+1)%n])
		d2 := distSq3Dd(p, cp)
		if d2 < bestDistSq {
			bestDistSq = d2
			bestPt = cp
		}
	}
	return bestPt
}

func distSq3Dd(a, b vec3d) float64 {
	dx, dy, dz := a.x-b.x, a.y-b.y, a.z-b.z
	return float64(dx*dx) + float64(dy*dy) + float64(dz*dz)
}

// RandomPointAround picks a 3D surface-area-weighted uniformly random point within 3D Euclidean distance radius
// of center (snapped onto center's walkable polygon) on polygons allowed by DefaultNavFilter that are reachable
// from center's polygon. Disconnected islands and unreachable stacked floors are never returned, and results are
// bit-identical across runs and platforms for the same seed.
//
// Returns (zero, -1, false) if nm is empty, rng is nil, radius < 0, or center lies outside the allowed mesh.
func (nm *NavMesh) RandomPointAround(center math3d.Vec3, radius float32, rng *random.Stream) (point math3d.Vec3, poly int, ok bool) {
	return nm.RandomPointAroundWithFilter(center, radius, rng, DefaultNavFilter())
}

// RandomPointAroundWithFilter behaves like RandomPointAround while restricting reachability and sampling to filter.
func (nm *NavMesh) RandomPointAroundWithFilter(center math3d.Vec3, radius float32, rng *random.Stream, filter NavFilter) (point math3d.Vec3, poly int, ok bool) {
	return nm.randomPointAround(center, radius, rng, filter, nil)
}

type clippedPoly3D struct {
	polyIdx   int
	vertStart int
	vertCount int
	area      float64
}

func (nm *NavMesh) randomPointAround(center math3d.Vec3, radius float32, rng *random.Stream, filter NavFilter, q *NavMeshQuery) (math3d.Vec3, int, bool) {
	r := float64(radius)
	if nm == nil || len(nm.Polys) == 0 || rng == nil || !isFiniteVec3(center) || math.IsNaN(r) || math.IsInf(r, 0) || r < 0 {
		return math3d.Vec3{}, -1, false
	}
	startPoly, ok := nm.FindPolygonWithFilter(center, filter)
	if !ok {
		return math3d.Vec3{}, -1, false
	}
	snapped := math3d.Vec3{X: center.X, Y: nm.Polys[startPoly].HeightAt(center.X, center.Z), Z: center.Z}
	if r == 0 {
		return snapped, startPoly, true
	}

	cVec := toVec3d(snapped)
	rSq := float64(r * r)
	minX, minY, minZ := cVec.x-r, cVec.y-r, cVec.z-r
	maxX, maxY, maxZ := cVec.x+r, cVec.y+r, cVec.z+r
	box := math3d.AABB{
		Min: math3d.Vec3{X: float32(minX) - sharedEdgeEps, Y: float32(minY) - sharedEdgeEps, Z: float32(minZ) - sharedEdgeEps},
		Max: math3d.Vec3{X: float32(maxX) + sharedEdgeEps, Y: float32(maxY) + sharedEdgeEps, Z: float32(maxZ) + sharedEdgeEps},
	}

	var queue []int
	var visitedMark []uint32
	var visitGen uint32
	var clippedPolys []clippedPoly3D
	var clippedVerts []vec3d
	if q != nil {
		q.ensureBFS(len(nm.Polys))
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
		visitedMark = make([]uint32, len(nm.Polys))
		visitGen = 1
	}

	var bufA, bufB [24]vec3d
	var totalArea float64

	enqueue := func(idx int) {
		visitedMark[idx] = visitGen
		queue = append(queue, idx)
	}
	enqueue(startPoly)

	for head := 0; head < len(queue); head++ {
		cur := queue[head]
		poly := &nm.Polys[cur]

		clipped := clipPolyAABB3D(poly.Vertices, minX, minY, minZ, maxX, maxY, maxZ, bufA[:0], bufB[:0])
		if len(clipped) >= 3 {
			a := polySurfaceArea3Dd(clipped)
			if a > 1e-12 {
				vs := len(clippedVerts)
				clippedVerts = append(clippedVerts, clipped...)
				clippedPolys = append(clippedPolys, clippedPoly3D{
					polyIdx:   cur,
					vertStart: vs,
					vertCount: len(clipped),
					area:      a,
				})
				totalArea += a
			}
		}

		var nBuf [24]vec3d
		for _, link := range poly.Links {
			next := link.Neighbor
			if visitedMark[next] == visitGen {
				continue
			}
			np := &nm.Polys[next]
			if !filter.Allows(np.Area, np.Flags) {
				continue
			}
			if !np.Bounds.Intersects(box) {
				continue
			}
			nVerts := nBuf[:0]
			for _, v := range np.Vertices {
				nVerts = append(nVerts, toVec3d(v))
			}
			cp := closestPointOnConvex3Dd(nVerts, np, cVec)
			if distSq3Dd(cp, cVec) <= rSq+1e-9 {
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
		return snapped, startPoly, true
	}

	const maxRejectAttempts = 64
	var lastPt vec3d
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
		pt := sampleConvexPoly3Dd(verts, rng)
		lastPt = pt
		lastPoly = chosen.polyIdx
		if distSq3Dd(pt, cVec) <= rSq {
			return toVec3f(pt), chosen.polyIdx, true
		}
	}

	var lastVerts [24]vec3d
	lv := lastVerts[:0]
	for _, v := range nm.Polys[lastPoly].Vertices {
		lv = append(lv, toVec3d(v))
	}
	anchor := closestPointOnConvex3Dd(lv, &nm.Polys[lastPoly], cVec)
	if distSq3Dd(anchor, cVec) <= rSq {
		dx, dy, dz := lastPt.x-anchor.x, lastPt.y-anchor.y, lastPt.z-anchor.z
		lo, hi := 0.0, 1.0
		for range 24 {
			mid := 0.5 * (lo + hi)
			cand := vec3d{
				x: anchor.x + float64(mid*dx),
				y: anchor.y + float64(mid*dy),
				z: anchor.z + float64(mid*dz),
			}
			if distSq3Dd(cand, cVec) <= rSq {
				lo = mid
			} else {
				hi = mid
			}
		}
		res := vec3d{
			x: anchor.x + float64(lo*dx),
			y: anchor.y + float64(lo*dy),
			z: anchor.z + float64(lo*dz),
		}
		return toVec3f(res), lastPoly, true
	}
	return snapped, startPoly, true
}

// NavMeshQuery holds reusable generation-stamped scratch buffers for zero-allocation
// 3D pathfinding and spatial queries over a world3d.NavMesh.
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
	entryPt []math3d.Vec3
	heapPos []int
	open    []navAStarNode

	goalGen    uint32
	goalMark   []uint32
	startPolys []int
	goalPolys  []int

	// Reusable corridor & funnel buffers.
	portalIDs   []int
	corridor    []int
	portals     []NavLink
	portalLeft  []math3d.Vec3
	portalRight []math3d.Vec3
	corners     []funnelCorner

	// Reusable BFS / clipping buffers for RandomPointAround.
	bfsGen       uint32
	bfsVisited   []uint32
	bfsQueue     []int
	clippedPolys []clippedPoly3D
	clippedVerts []vec3d
}

// NewNavMeshQuery creates a reusable 3D query instance bound to mesh.
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
		q.entryPt = make([]math3d.Vec3, nodeCount)
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

func minFilterCost3D(filter NavFilter) float64 {
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

// FindPathInto finds the shortest 3D path from start to goal under DefaultNavFilter,
// writing waypoints into dst[:0] and reusing q's internal buffers so repeated calls perform
// zero heap allocations after warm-up.
func (q *NavMeshQuery) FindPathInto(dst []math3d.Vec3, start, goal math3d.Vec3) ([]math3d.Vec3, bool) {
	return q.FindPathWithFilterInto(dst, start, goal, DefaultNavFilter())
}

// FindPathWithFilterInto finds the minimum-cost 3D path from start to goal restricted to filter,
// writing waypoints into dst[:0] with zero heap allocations after warm-up.
func (q *NavMeshQuery) FindPathWithFilterInto(dst []math3d.Vec3, start, goal math3d.Vec3, filter NavFilter) ([]math3d.Vec3, bool) {
	res := q.FindPathWithFilterAndOptionsInto(dst, start, goal, filter, ai.PathOptions{})
	if !res.Found || res.Partial {
		return nil, false
	}
	return res.Path, true
}

type navAStarResult3D struct {
	foundComplete bool
	directPoly    int
	hasBest       bool
	bestPortalID  int
	bestPt        math3d.Vec3
	exhausted     bool
	expansions    int
	goalNode      int
	startSnapped  math3d.Vec3
	goalSnapped   math3d.Vec3
}

func (q *NavMeshQuery) runPortalAStar(start, goal math3d.Vec3, filter NavFilter, opts ai.PathOptions) navAStarResult3D {
	nm := q.mesh
	n := len(nm.Polys)
	if len(nm.linkOffset) != n+1 {
		nm.rebuildPortalIndex()
	}

	tol := nm.VerticalTolerance
	if tol <= 0 {
		tol = defaultVerticalTolerance
	}

	totalPortals := nm.linkOffset[n]
	goalNode := totalPortals
	nodeCount := goalNode + 1
	q.ensurePathBuffers(n, nodeCount)

	q.startPolys = nm.bvhFindPolysAt(start, tol, filter, true, q.startPolys[:0])
	if len(q.startPolys) == 0 {
		return navAStarResult3D{directPoly: -1, bestPortalID: -1, goalNode: goalNode}
	}
	start.Y = nm.Polys[q.startPolys[0]].HeightAt(start.X, start.Z)

	q.goalPolys = nm.bvhFindPolysAt(goal, tol, filter, true, q.goalPolys[:0])
	hasGoal := len(q.goalPolys) > 0
	if hasGoal {
		goal.Y = nm.Polys[q.goalPolys[0]].HeightAt(goal.X, goal.Z)
	} else if !opts.AllowPartial {
		return navAStarResult3D{directPoly: -1, bestPortalID: -1, goalNode: goalNode}
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

	minCost := minFilterCost3D(filter)

	directPossible := false
	directCost := math.Inf(1)
	directPoly := -1
	if hasGoal {
		for _, sp := range q.startPolys {
			if q.goalMark[sp] == goalGen {
				if start.ApproxEqual(goal, 1e-5) {
					q.parent[goalNode] = -1
					return navAStarResult3D{
						foundComplete: true,
						directPoly:    sp,
						bestPortalID:  -1,
						goalNode:      goalNode,
						startSnapped:  start,
						goalSnapped:   goal,
					}
				}
				c := filter.AreaCost(nm.Polys[sp].Area)
				dc := dist3D(start, goal) * c
				if dc < directCost {
					directCost = dc
					directPossible = true
					directPoly = sp
				}
			}
		}
		if directPossible && directCost <= minCost*dist3D(start, goal)+1e-9 {
			q.parent[goalNode] = -1
			return navAStarResult3D{
				foundComplete: true,
				directPoly:    directPoly,
				bestPortalID:  -1,
				goalNode:      goalNode,
				startSnapped:  start,
				goalSnapped:   goal,
			}
		}
	}

	hasBest := false
	bestPortalID := -1
	var bestPt math3d.Vec3
	var bestH, bestG float64
	if opts.AllowPartial {
		for _, sp := range q.startPolys {
			cp := closestPointOnNavPoly(&nm.Polys[sp], goal)
			h := dist3D(cp, goal)
			g := dist3D(start, cp) * filter.AreaCost(nm.Polys[sp].Area)
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

	pushOrUpdate := func(id int, g, f, h float64, parent int, pt math3d.Vec3) {
		if q.seen[id] != gen {
			q.seen[id] = gen
			q.gScore[id] = g
			q.parent[id] = parent
			q.entryPt[id] = pt
			pos := len(q.open)
			q.open = append(q.open, navAStarNode{id: id, f: f, h: h})
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
		q.open = append(q.open, navAStarNode{id: id, f: f, h: h})
		q.heapPos[id] = pos
		navSiftUp(q.open, q.heapPos, pos)
	}

	if directPossible {
		pushOrUpdate(goalNode, directCost, directCost, 0, -1, goal)
	}

	for _, sp := range q.startPolys {
		spCost := filter.AreaCost(nm.Polys[sp].Area)
		base := nm.linkOffset[sp]
		for k, link := range nm.Polys[sp].Links {
			np := &nm.Polys[link.Neighbor]
			if !filter.Allows(np.Area, np.Flags) {
				continue
			}
			nc := filter.AreaCost(np.Area)
			if nc <= 0 || math.IsNaN(nc) || math.IsInf(nc, 0) {
				continue
			}
			pid := base + k
			var g float64
			var exitPt math3d.Vec3
			if link.OffMesh {
				cost := float64(link.Cost)
				if !(cost > 0) {
					cost = dist3D(link.Left, link.End)
				}
				g = dist3D(start, link.Left)*spCost + cost
				exitPt = link.End
			} else {
				exitPt = bestPortalPoint3D(start, goal, link.Left, link.Right)
				g = dist3D(start, exitPt) * spCost
			}
			if q.seen[pid] != gen || g < q.gScore[pid] {
				h := dist3D(exitPt, goal) * minCost
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

		fromPoly := nm.portalFrom[cur.id]
		link := nm.portalLink[cur.id]
		toPolyIdx := link.Neighbor
		toPoly := &nm.Polys[toPolyIdx]
		curG := q.gScore[cur.id]
		curPt := q.entryPt[cur.id]
		toCost := filter.AreaCost(toPoly.Area)

		if opts.AllowPartial {
			cp := closestPointOnNavPoly(toPoly, goal)
			h := dist3D(cp, goal)
			g := curG + dist3D(curPt, cp)*toCost
			if !hasBest || h < bestH || (h == bestH && (g < bestG || (g == bestG && cur.id < bestPortalID))) {
				hasBest = true
				bestPortalID = cur.id
				bestPt = cp
				bestH = h
				bestG = g
			}
		}

		if q.goalMark[toPolyIdx] == goalGen {
			ng := curG + dist3D(curPt, goal)*toCost
			if q.seen[goalNode] != gen || ng < q.gScore[goalNode] {
				pushOrUpdate(goalNode, ng, ng, 0, cur.id, goal)
			}
		}

		base := nm.linkOffset[toPolyIdx]
		for k, nlink := range toPoly.Links {
			npid := base + k
			if q.closed[npid] == gen {
				continue
			}
			if !nlink.OffMesh && nlink.Neighbor == fromPoly {
				continue
			}
			ntp := &nm.Polys[nlink.Neighbor]
			if !filter.Allows(ntp.Area, ntp.Flags) {
				continue
			}
			ntc := filter.AreaCost(ntp.Area)
			if ntc <= 0 || math.IsNaN(ntc) || math.IsInf(ntc, 0) {
				continue
			}
			var ng float64
			var exitPt math3d.Vec3
			if nlink.OffMesh {
				cost := float64(nlink.Cost)
				if !(cost > 0) {
					cost = dist3D(nlink.Left, nlink.End)
				}
				ng = curG + dist3D(curPt, nlink.Left)*toCost + cost
				exitPt = nlink.End
			} else {
				exitPt = bestPortalPoint3D(curPt, goal, nlink.Left, nlink.Right)
				ng = curG + dist3D(curPt, exitPt)*toCost
			}
			if q.seen[npid] != gen || ng < q.gScore[npid] {
				h := dist3D(exitPt, goal) * minCost
				pushOrUpdate(npid, ng, ng+h, h, cur.id, exitPt)
			}
		}
	}

	if !foundComplete && len(q.open) > 0 && opts.MaxExpansions > 0 && expansions >= opts.MaxExpansions {
		exhausted = true
	}

	return navAStarResult3D{
		foundComplete: foundComplete,
		directPoly:    directPoly,
		hasBest:       hasBest,
		bestPortalID:  bestPortalID,
		bestPt:        bestPt,
		exhausted:     exhausted,
		expansions:    expansions,
		goalNode:      goalNode,
		startSnapped:  start,
		goalSnapped:   goal,
	}
}

// FindPathWithFilterAndOptionsInto runs a bounded or partial 3D NavMesh search restricted to filter under opts,
// writing the resulting waypoints into dst[:0] and reusing q's internal buffers with zero heap allocations after warm-up.
func (q *NavMeshQuery) FindPathWithFilterAndOptionsInto(dst []math3d.Vec3, start, goal math3d.Vec3, filter NavFilter, opts ai.PathOptions) NavPathResult {
	if q == nil || q.mesh == nil || len(q.mesh.Polys) == 0 || !isFiniteVec3(start) || !isFiniteVec3(goal) {
		return NavPathResult{}
	}

	sr := q.runPortalAStar(start, goal, filter, opts)
	res := NavPathResult{
		Exhausted:  sr.exhausted,
		Expansions: sr.expansions,
	}

	if sr.foundComplete {
		res.Found = true
		if q.parent[sr.goalNode] == -1 {
			if sr.startSnapped.ApproxEqual(sr.goalSnapped, 1e-5) {
				res.Path = append(dst[:0], sr.startSnapped)
			} else {
				res.Path = append(dst[:0], sr.startSnapped, sr.goalSnapped)
			}
			return res
		}
		q.reconstructCorridorInto(q.parent[sr.goalNode])
		res.Path = q.smoothCorridorInto(dst[:0], q.corridor, q.portals, sr.startSnapped, sr.goalSnapped)
		return res
	}

	if opts.AllowPartial && sr.hasBest {
		res.Found = true
		res.Partial = true
		if sr.bestPortalID < 0 {
			if sr.startSnapped.ApproxEqual(sr.bestPt, 1e-5) {
				res.Path = append(dst[:0], sr.startSnapped)
			} else {
				res.Path = append(dst[:0], sr.startSnapped, sr.bestPt)
			}
			return res
		}
		q.reconstructCorridorInto(sr.bestPortalID)
		res.Path = q.smoothCorridorInto(dst[:0], q.corridor, q.portals, sr.startSnapped, sr.bestPt)
		return res
	}

	return res
}

// FindCorridorInto finds the 3D polygon corridor from start to goal under DefaultNavFilter,
// writing polygon indices into dst[:0] with zero heap allocations after warm-up.
func (q *NavMeshQuery) FindCorridorInto(dst []int, start, goal math3d.Vec3) ([]int, bool) {
	return q.FindCorridorWithFilterInto(dst, start, goal, DefaultNavFilter())
}

// FindCorridorWithFilterInto finds the minimum-cost 3D polygon corridor from start to goal restricted to filter,
// writing polygon indices into dst[:0] with zero heap allocations after warm-up.
func (q *NavMeshQuery) FindCorridorWithFilterInto(dst []int, start, goal math3d.Vec3, filter NavFilter) ([]int, bool) {
	if q == nil || q.mesh == nil || len(q.mesh.Polys) == 0 || !isFiniteVec3(start) || !isFiniteVec3(goal) {
		return nil, false
	}
	sr := q.runPortalAStar(start, goal, filter, ai.PathOptions{})
	if !sr.foundComplete {
		return nil, false
	}
	if q.parent[sr.goalNode] == -1 {
		if sr.directPoly < 0 {
			return nil, false
		}
		return append(dst[:0], sr.directPoly), true
	}
	q.reconstructCorridorInto(q.parent[sr.goalNode])
	return append(dst[:0], q.corridor...), true
}

func (q *NavMeshQuery) reconstructCorridorInto(lastPortalID int) {
	nm := q.mesh
	q.portalIDs = q.portalIDs[:0]
	for id := lastPortalID; id != -1; id = q.parent[id] {
		q.portalIDs = append(q.portalIDs, id)
	}
	slices.Reverse(q.portalIDs)

	n := len(q.portalIDs)
	q.corridor = append(q.corridor[:0], nm.portalFrom[q.portalIDs[0]])
	q.portals = q.portals[:0]
	for i := range n {
		l := nm.portalLink[q.portalIDs[i]]
		q.portals = append(q.portals, l)
		q.corridor = append(q.corridor, l.Neighbor)
	}
}

func (q *NavMeshQuery) smoothCorridorInto(out []math3d.Vec3, corridor []int, portals []NavLink, start, goal math3d.Vec3) []math3d.Vec3 {
	hasOffMesh := false
	for _, p := range portals {
		if p.OffMesh {
			hasOffMesh = true
			break
		}
	}
	if !hasOffMesh {
		return q.smoothCorridorSegmentInto(out, corridor, portals, start, goal)
	}

	segStart := start
	segBegin := 0
	for i, link := range portals {
		if !link.OffMesh {
			continue
		}
		out = q.smoothCorridorSegmentInto(out, corridor[segBegin:i+1], portals[segBegin:i], segStart, link.Left)
		if len(out) == 0 || !out[len(out)-1].ApproxEqual(link.End, 1e-4) {
			out = append(out, link.End)
		}
		segStart = link.End
		segBegin = i + 1
	}
	return q.smoothCorridorSegmentInto(out, corridor[segBegin:], portals[segBegin:], segStart, goal)
}

func (q *NavMeshQuery) smoothCorridorSegmentInto(out []math3d.Vec3, corridor []int, portals []NavLink, start, goal math3d.Vec3) []math3d.Vec3 {
	if len(portals) == 0 {
		if len(out) == 0 || !out[len(out)-1].ApproxEqual(start, 1e-4) {
			out = append(out, start)
		}
		if !out[len(out)-1].ApproxEqual(goal, 1e-4) {
			out = append(out, goal)
		}
		return out
	}

	m := len(portals) + 2
	if cap(q.portalLeft) < m {
		q.portalLeft = make([]math3d.Vec3, m)
		q.portalRight = make([]math3d.Vec3, m)
	} else {
		q.portalLeft = q.portalLeft[:m]
		q.portalRight = q.portalRight[:m]
	}

	q.portalLeft[0] = start
	q.portalRight[0] = start
	for i, p := range portals {
		q.portalLeft[i+1] = p.Left
		q.portalRight[i+1] = p.Right
	}
	q.portalLeft[m-1] = goal
	q.portalRight[m-1] = goal

	q.corners = append(q.corners[:0], funnelCorner{pos: start, portalIdx: 0})

	portalApex := start
	leftPt := q.portalLeft[0]
	rightPt := q.portalRight[0]
	apexIdx, leftIdx, rightIdx := 0, 0, 0

	for i := 1; i < m; i++ {
		left := q.portalLeft[i]
		right := q.portalRight[i]

		if triArea2(portalApex, rightPt, right) <= 0 {
			if sameXZ(portalApex, rightPt) || sameXZ(portalApex, leftPt) || triArea2(portalApex, leftPt, right) > 0 {
				rightPt = right
				rightIdx = i
			} else {
				q.corners = appendCorner(q.corners, funnelCorner{pos: leftPt, portalIdx: leftIdx})
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

		if triArea2(portalApex, leftPt, left) >= 0 {
			if sameXZ(portalApex, leftPt) || sameXZ(portalApex, rightPt) || triArea2(portalApex, rightPt, left) < 0 {
				leftPt = left
				leftIdx = i
			} else {
				q.corners = appendCorner(q.corners, funnelCorner{pos: rightPt, portalIdx: rightIdx})
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

	q.corners = appendCorner(q.corners, funnelCorner{pos: goal, portalIdx: m - 1})

	nm := q.mesh
	if len(out) == 0 || !out[len(out)-1].ApproxEqual(q.corners[0].pos, 1e-4) {
		out = append(out, q.corners[0].pos)
	}
	for c := 1; c < len(q.corners); c++ {
		prev := q.corners[c-1]
		next := q.corners[c]
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

// NearestPoint delegates to q's NavMesh.NearestPoint.
func (q *NavMeshQuery) NearestPoint(p, extent math3d.Vec3) (math3d.Vec3, int, bool) {
	if q == nil || q.mesh == nil {
		return math3d.Vec3{}, -1, false
	}
	return q.mesh.NearestPoint(p, extent)
}

// NearestPointWithFilter delegates to q's NavMesh.NearestPointWithFilter.
func (q *NavMeshQuery) NearestPointWithFilter(p, extent math3d.Vec3, filter NavFilter) (math3d.Vec3, int, bool) {
	if q == nil || q.mesh == nil {
		return math3d.Vec3{}, -1, false
	}
	return q.mesh.NearestPointWithFilter(p, extent, filter)
}

// Raycast delegates to q's NavMesh.Raycast.
func (q *NavMeshQuery) Raycast(start, target math3d.Vec3) (NavRaycastHit, bool) {
	if q == nil || q.mesh == nil {
		return NavRaycastHit{Point: start, Poly: -1, Hit: true}, true
	}
	return q.mesh.Raycast(start, target)
}

// RaycastWithFilter delegates to q's NavMesh.RaycastWithFilter.
func (q *NavMeshQuery) RaycastWithFilter(start, target math3d.Vec3, filter NavFilter) (NavRaycastHit, bool) {
	if q == nil || q.mesh == nil {
		return NavRaycastHit{Point: start, Poly: -1, Hit: true}, true
	}
	return q.mesh.RaycastWithFilter(start, target, filter)
}

// RandomPoint delegates to q's NavMesh.RandomPoint.
func (q *NavMeshQuery) RandomPoint(rng *random.Stream) (math3d.Vec3, int, bool) {
	if q == nil || q.mesh == nil {
		return math3d.Vec3{}, -1, false
	}
	return q.mesh.RandomPoint(rng)
}

// RandomPointWithFilter delegates to q's NavMesh.RandomPointWithFilter.
func (q *NavMeshQuery) RandomPointWithFilter(rng *random.Stream, filter NavFilter) (math3d.Vec3, int, bool) {
	if q == nil || q.mesh == nil {
		return math3d.Vec3{}, -1, false
	}
	return q.mesh.RandomPointWithFilter(rng, filter)
}

// RandomPointAround picks a random reachable 3D point within radius of center using q's reusable buffers.
func (q *NavMeshQuery) RandomPointAround(center math3d.Vec3, radius float32, rng *random.Stream) (math3d.Vec3, int, bool) {
	return q.RandomPointAroundWithFilter(center, radius, rng, DefaultNavFilter())
}

// RandomPointAroundWithFilter picks a random reachable 3D point within radius of center allowed by filter using q's reusable buffers.
func (q *NavMeshQuery) RandomPointAroundWithFilter(center math3d.Vec3, radius float32, rng *random.Stream, filter NavFilter) (math3d.Vec3, int, bool) {
	if q == nil || q.mesh == nil {
		return math3d.Vec3{}, -1, false
	}
	return q.mesh.randomPointAround(center, radius, rng, filter, q)
}

// MoveAlongSurface delegates to q's NavMesh.MoveAlongSurface.
func (q *NavMeshQuery) MoveAlongSurface(start, target math3d.Vec3) (NavMoveResult, bool) {
	return q.MoveAlongSurfaceWithFilter(start, target, DefaultNavFilter())
}

// MoveAlongSurfaceWithFilter delegates to q's NavMesh.MoveAlongSurfaceWithFilter.
func (q *NavMeshQuery) MoveAlongSurfaceWithFilter(start, target math3d.Vec3, filter NavFilter) (NavMoveResult, bool) {
	if q == nil || q.mesh == nil {
		return NavMoveResult{Point: start, Poly: -1}, false
	}
	return q.mesh.moveAlongSurfaceInto(nil, start, target, filter)
}

// MoveAlongSurfaceInto slides from start toward target under DefaultNavFilter, writing visited polygon IDs into visitedDst[:0].
func (q *NavMeshQuery) MoveAlongSurfaceInto(visitedDst []int, start, target math3d.Vec3) (NavMoveResult, bool) {
	return q.MoveAlongSurfaceWithFilterInto(visitedDst, start, target, DefaultNavFilter())
}

// MoveAlongSurfaceWithFilterInto slides from start toward target under filter, writing visited polygon IDs into visitedDst[:0].
func (q *NavMeshQuery) MoveAlongSurfaceWithFilterInto(visitedDst []int, start, target math3d.Vec3, filter NavFilter) (NavMoveResult, bool) {
	if q == nil || q.mesh == nil {
		return NavMoveResult{Point: start, Poly: -1}, false
	}
	return q.mesh.moveAlongSurfaceInto(visitedDst, start, target, filter)
}

// FindPathInto behaves like FindPath but writes waypoints into dst[:0] and reuses the mesh's internal search buffers
// so repeated calls perform zero heap allocations after warm-up.
//
// FindPathInto is not safe for concurrent use on the same NavMesh; construct separate NewNavMeshQuery instances
// when running concurrent path searches against a shared read-only NavMesh.
func (nm *NavMesh) FindPathInto(dst []math3d.Vec3, start, goal math3d.Vec3) ([]math3d.Vec3, bool) {
	return nm.FindPathWithFilterInto(dst, start, goal, DefaultNavFilter())
}

// FindPathWithFilterInto behaves like FindPathWithFilter but writes waypoints into dst[:0] and reuses
// the mesh's internal search buffers with zero heap allocations after warm-up.
func (nm *NavMesh) FindPathWithFilterInto(dst []math3d.Vec3, start, goal math3d.Vec3, filter NavFilter) ([]math3d.Vec3, bool) {
	if nm == nil {
		return nil, false
	}
	nm.defaultQuery.mesh = nm
	return nm.defaultQuery.FindPathWithFilterInto(dst, start, goal, filter)
}

// FindPathWithFilterAndOptionsInto behaves like FindPathWithFilterAndOptions but writes waypoints into dst[:0]
// and reuses the mesh's internal search buffers with zero heap allocations after warm-up.
func (nm *NavMesh) FindPathWithFilterAndOptionsInto(dst []math3d.Vec3, start, goal math3d.Vec3, filter NavFilter, opts ai.PathOptions) NavPathResult {
	if nm == nil {
		return NavPathResult{}
	}
	nm.defaultQuery.mesh = nm
	return nm.defaultQuery.FindPathWithFilterAndOptionsInto(dst, start, goal, filter, opts)
}

// FindCorridorInto behaves like FindCorridor but writes polygon indices into dst[:0] and reuses
// the mesh's internal search buffers with zero heap allocations after warm-up.
//
// FindCorridorInto is not safe for concurrent use on the same NavMesh; use NewNavMeshQuery per goroutine.
func (nm *NavMesh) FindCorridorInto(dst []int, start, goal math3d.Vec3) ([]int, bool) {
	return nm.FindCorridorWithFilterInto(dst, start, goal, DefaultNavFilter())
}

// FindCorridorWithFilterInto behaves like FindCorridorWithFilter but writes polygon indices into dst[:0]
// and reuses the mesh's internal search buffers with zero heap allocations after warm-up.
func (nm *NavMesh) FindCorridorWithFilterInto(dst []int, start, goal math3d.Vec3, filter NavFilter) ([]int, bool) {
	if nm == nil {
		return nil, false
	}
	nm.defaultQuery.mesh = nm
	return nm.defaultQuery.FindCorridorWithFilterInto(dst, start, goal, filter)
}
