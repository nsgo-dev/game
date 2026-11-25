package world3d

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

const (
	tiledAreaEps3D  = 1e-6
	tiledSplitEps3D = 1e-6
	tiledVertEps3D  = 1e-5
	tiledMergeMax3D = 8
)

type vec2XZ struct {
	x, z float64
}

func (a vec2XZ) sub(b vec2XZ) vec2XZ { return vec2XZ{a.x - b.x, a.z - b.z} }
func (a vec2XZ) lenSq() float64      { return a.x*a.x + a.z*a.z }
func (a vec2XZ) len() float64        { return math.Hypot(a.x, a.z) }

func distSqXZ64(a, b vec2XZ) float64 {
	dx, dz := a.x-b.x, a.z-b.z
	return dx*dx + dz*dz
}

// triArea2XZ64 returns twice the signed area in XZ: (b.x-a.x)*(c.z-a.z) - (b.z-a.z)*(c.x-a.x).
// For polygons wound CCW when viewed from +Y, interior turns have triArea2XZ64 < 0.
func triArea2XZ64(a, b, c vec2XZ) float64 {
	return (b.x-a.x)*(c.z-a.z) - (b.z-a.z)*(c.x-a.x)
}

// polyArea2XZ64 returns twice the signed XZ area. Negative means CCW when viewed from +Y.
func polyArea2XZ64(pts []vec2XZ) float64 {
	var sum float64
	n := len(pts)
	for i := range n {
		a := pts[i]
		b := pts[(i+1)%n]
		sum += a.x*b.z - a.z*b.x
	}
	return sum
}

// cleanPolyXZ normalizes an XZ polygon to CCW-from-+Y winding (polyArea2XZ64 < 0),
// removes near-duplicate consecutive and collinear vertices, and returns nil if degenerate.
func cleanPolyXZ(raw []vec2XZ) []vec2XZ {
	if len(raw) < 3 {
		return nil
	}
	pts := make([]vec2XZ, 0, len(raw))
	for _, p := range raw {
		if math.IsNaN(p.x) || math.IsInf(p.x, 0) || math.IsNaN(p.z) || math.IsInf(p.z, 0) {
			return nil
		}
		if len(pts) == 0 || distSqXZ64(pts[len(pts)-1], p) > tiledVertEps3D*tiledVertEps3D {
			pts = append(pts, p)
		}
	}
	for len(pts) >= 2 && distSqXZ64(pts[0], pts[len(pts)-1]) <= tiledVertEps3D*tiledVertEps3D {
		pts = pts[:len(pts)-1]
	}
	if len(pts) < 3 {
		return nil
	}
	area2 := polyArea2XZ64(pts)
	if math.Abs(area2) <= tiledAreaEps3D*2 {
		return nil
	}
	// Normalize to CCW viewed from +Y (area2 < 0).
	if area2 > 0 {
		slices.Reverse(pts)
	}
	changed := true
	for changed && len(pts) >= 3 {
		changed = false
		n := len(pts)
		out := make([]vec2XZ, 0, n)
		for i := range n {
			prev := pts[(i+n-1)%n]
			cur := pts[i]
			next := pts[(i+1)%n]
			d := next.sub(prev).len()
			if d > 1e-12 && math.Abs(triArea2XZ64(prev, cur, next)) <= tiledVertEps3D*d {
				changed = true
				continue
			}
			out = append(out, cur)
		}
		pts = out
	}
	if len(pts) < 3 || polyArea2XZ64(pts) >= -tiledAreaEps3D*2 {
		return nil
	}
	return pts
}

// isConvexCCWXZ reports whether a cleaned CCW-from-+Y XZ polygon is convex.
func isConvexCCWXZ(pts []vec2XZ) bool {
	n := len(pts)
	if n < 3 {
		return false
	}
	for i := range n {
		if triArea2XZ64(pts[i], pts[(i+1)%n], pts[(i+2)%n]) > 1e-7 {
			return false
		}
	}
	return true
}

// splitConvexPolyXZ splits a CCW-from-+Y convex XZ polygon by directed edge a -> b.
// inside receives the interior half-plane (inward normal (dz, -dx) >= 0);
// outside receives the exterior half-plane (<= 0).
func splitConvexPolyXZ(poly []vec2XZ, a, b vec2XZ) (inside, outside []vec2XZ) {
	dx, dz := b.x-a.x, b.z-a.z
	edgeLen := math.Hypot(dx, dz)
	if edgeLen <= 1e-12 || len(poly) < 3 {
		return nil, nil
	}
	// Inward unit normal for CCW-from-+Y edge a -> b is (dz/edgeLen, -dx/edgeLen).
	nx, nz := dz/edgeLen, -dx/edgeLen

	n := len(poly)
	var dBuf [16]float64
	var dists []float64
	if n <= len(dBuf) {
		dists = dBuf[:n]
	} else {
		dists = make([]float64, n)
	}
	hasPos, hasNeg := false, false
	for i, p := range poly {
		d := (p.x-a.x)*nx + (p.z-a.z)*nz
		dists[i] = d
		if d > tiledSplitEps3D {
			hasPos = true
		} else if d < -tiledSplitEps3D {
			hasNeg = true
		}
	}
	if !hasNeg {
		out := make([]vec2XZ, n)
		copy(out, poly)
		return out, nil
	}
	if !hasPos {
		out := make([]vec2XZ, n)
		copy(out, poly)
		return nil, out
	}

	inside = make([]vec2XZ, 0, n+1)
	outside = make([]vec2XZ, 0, n+1)
	for i := range n {
		j := (i + 1) % n
		p0, p1 := poly[i], poly[j]
		d0, d1 := dists[i], dists[j]
		s0 := 0
		if d0 > tiledSplitEps3D {
			s0 = 1
		} else if d0 < -tiledSplitEps3D {
			s0 = -1
		}
		s1 := 0
		if d1 > tiledSplitEps3D {
			s1 = 1
		} else if d1 < -tiledSplitEps3D {
			s1 = -1
		}
		if s0 >= 0 {
			inside = append(inside, p0)
		}
		if s0 <= 0 {
			outside = append(outside, p0)
		}
		if (s0 > 0 && s1 < 0) || (s0 < 0 && s1 > 0) {
			t := d0 / (d0 - d1)
			mid := vec2XZ{
				x: p0.x + (p1.x-p0.x)*t,
				z: p0.z + (p1.z-p0.z)*t,
			}
			inside = append(inside, mid)
			outside = append(outside, mid)
		}
	}
	inside = cleanPolyXZ(inside)
	outside = cleanPolyXZ(outside)
	return inside, outside
}

// clipPolyToBoxXZ clips a CCW-from-+Y convex XZ polygon against [minX, maxX] x [minZ, maxZ].
func clipPolyToBoxXZ(poly []vec2XZ, minX, minZ, maxX, maxZ float64) []vec2XZ {
	if len(poly) < 3 {
		return nil
	}
	cur := poly
	// 4 CCW-from-+Y edges of [minX, maxX] x [minZ, maxZ]:
	edges := [4][2]vec2XZ{
		{{minX, minZ}, {minX, maxZ}}, // x >= minX
		{{minX, maxZ}, {maxX, maxZ}}, // z <= maxZ
		{{maxX, maxZ}, {maxX, minZ}}, // x <= maxX
		{{maxX, minZ}, {minX, minZ}}, // z >= minZ
	}
	for _, e := range edges {
		cur, _ = splitConvexPolyXZ(cur, e[0], e[1])
		if len(cur) < 3 {
			return nil
		}
	}
	return cur
}

// convexPolysOverlapXZ reports whether two CCW-from-+Y convex XZ polygons have strictly
// positive-area interior intersection.
func convexPolysOverlapXZ(a, b []vec2XZ) bool {
	if len(a) < 3 || len(b) < 3 {
		return false
	}
	if hasSeparatingAxisXZ(a, b) || hasSeparatingAxisXZ(b, a) {
		return false
	}
	return true
}

func hasSeparatingAxisXZ(poly, other []vec2XZ) bool {
	n := len(poly)
	for i := range n {
		p0 := poly[i]
		p1 := poly[(i+1)%n]
		dx, dz := p1.x-p0.x, p1.z-p0.z
		edgeLen := math.Hypot(dx, dz)
		if edgeLen <= 1e-12 {
			continue
		}
		// Outward unit normal for CCW-from-+Y edge is (-dz/edgeLen, dx/edgeLen).
		nx, nz := -dz/edgeLen, dx/edgeLen
		allOutside := true
		for _, q := range other {
			if (q.x-p0.x)*nx+(q.z-p0.z)*nz < -tiledSplitEps3D {
				allOutside = false
				break
			}
		}
		if allOutside {
			return true
		}
	}
	return false
}

// carveConvexPolyXZ subtracts CCW-from-+Y convex obstacle cutter from poly.
func carveConvexPolyXZ(poly, cutter []vec2XZ) (outside [][]vec2XZ, inside []vec2XZ) {
	if !convexPolysOverlapXZ(poly, cutter) {
		return [][]vec2XZ{poly}, nil
	}
	rem := poly
	n := len(cutter)
	for i := range n {
		a := cutter[i]
		b := cutter[(i+1)%n]
		inHalf, outHalf := splitConvexPolyXZ(rem, a, b)
		if len(outHalf) >= 3 {
			outside = append(outside, outHalf)
		}
		if len(inHalf) < 3 {
			rem = nil
			break
		}
		rem = inHalf
	}
	return outside, rem
}

func pointInTriXZ(p, a, b, c vec2XZ) bool {
	return triArea2XZ64(a, b, p) <= 1e-9 &&
		triArea2XZ64(b, c, p) <= 1e-9 &&
		triArea2XZ64(c, a, p) <= 1e-9
}

// earClipXZ decomposes a simple CCW-from-+Y XZ polygon into CCW-from-+Y triangles.
func earClipXZ(pts []vec2XZ) [][]vec2XZ {
	n := len(pts)
	if n < 3 {
		return nil
	}
	if n == 3 {
		return [][]vec2XZ{{pts[0], pts[1], pts[2]}}
	}
	ring := make([]int, n)
	for i := range n {
		ring[i] = i
	}
	var tris [][]vec2XZ
	guard := n * n
	for len(ring) > 3 && guard > 0 {
		guard--
		m := len(ring)
		clipped := false
		for i := range m {
			prev := pts[ring[(i+m-1)%m]]
			cur := pts[ring[i]]
			next := pts[ring[(i+1)%m]]
			if triArea2XZ64(prev, cur, next) >= -1e-9 {
				continue
			}
			ear := true
			for k := range m {
				if k == (i+m-1)%m || k == i || k == (i+1)%m {
					continue
				}
				if pointInTriXZ(pts[ring[k]], prev, cur, next) {
					ear = false
					break
				}
			}
			if !ear {
				continue
			}
			if tri := cleanPolyXZ([]vec2XZ{prev, cur, next}); len(tri) == 3 {
				tris = append(tris, tri)
			}
			ring = append(ring[:i], ring[i+1:]...)
			clipped = true
			break
		}
		if !clipped {
			best := -1
			bestArea := 0.0
			for i := range m {
				a := -triArea2XZ64(pts[ring[(i+m-1)%m]], pts[ring[i]], pts[ring[(i+1)%m]])
				if a > bestArea {
					bestArea = a
					best = i
				}
			}
			if best < 0 {
				break
			}
			prev := pts[ring[(best+m-1)%m]]
			cur := pts[ring[best]]
			next := pts[ring[(best+1)%m]]
			if tri := cleanPolyXZ([]vec2XZ{prev, cur, next}); len(tri) == 3 {
				tris = append(tris, tri)
			}
			ring = append(ring[:best], ring[best+1:]...)
		}
	}
	if len(ring) == 3 {
		if tri := cleanPolyXZ([]vec2XZ{pts[ring[0]], pts[ring[1]], pts[ring[2]]}); len(tri) == 3 {
			tris = append(tris, tri)
		}
	}
	return tris
}

func offsetConvexPolyXZ(poly []vec2XZ, margin float64) []vec2XZ {
	if margin <= 0 || len(poly) < 3 {
		return poly
	}
	n := len(poly)
	type offsetEdge struct {
		p0, p1    vec2XZ
		origStart vec2XZ
		nx, nz    float64
	}
	edges := make([]offsetEdge, 0, n)
	for i := range n {
		a := poly[i]
		b := poly[(i+1)%n]
		dx, dz := b.x-a.x, b.z-a.z
		l := math.Hypot(dx, dz)
		if l <= 1e-12 {
			continue
		}
		// Outward unit normal for CCW-from-+Y edge a->b is (-dz/l, dx/l).
		nx, nz := -dz/l, dx/l
		edges = append(edges, offsetEdge{
			p0:        vec2XZ{a.x + nx*margin, a.z + nz*margin},
			p1:        vec2XZ{b.x + nx*margin, b.z + nz*margin},
			origStart: a,
			nx:        nx,
			nz:        nz,
		})
	}
	m := len(edges)
	if m < 3 {
		return poly
	}
	out := make([]vec2XZ, 0, m*2)
	for i := range m {
		e0 := edges[(i+m-1)%m]
		e1 := edges[i]
		dot := e0.nx*e1.nx + e0.nz*e1.nz
		if dot < 0.0 {
			out = append(out, e0.p1)
			v := e1.origStart
			bx, bz := e0.nx+e1.nx, e0.nz+e1.nz
			bl := math.Hypot(bx, bz)
			if bl > 1e-9 {
				bx /= bl
				bz /= bl
				bp := vec2XZ{v.x + bx*margin, v.z + bz*margin}
				bp2 := vec2XZ{bp.x - bz, bp.z + bx}
				if pt, ok := intersectLinesXZ(e0.p0, e0.p1, bp, bp2); ok {
					out = append(out, pt)
				}
				if pt, ok := intersectLinesXZ(bp, bp2, e1.p0, e1.p1); ok {
					out = append(out, pt)
				}
			}
			out = append(out, e1.p0)
			continue
		}
		if pt, ok := intersectLinesXZ(e0.p0, e0.p1, e1.p0, e1.p1); ok {
			out = append(out, pt)
		} else {
			out = append(out, e0.p1, e1.p0)
		}
	}
	if cleaned := cleanPolyXZ(out); len(cleaned) >= 3 {
		return cleaned
	}
	return poly
}

func intersectLinesXZ(a0, a1, b0, b1 vec2XZ) (vec2XZ, bool) {
	dax, daz := a1.x-a0.x, a1.z-a0.z
	dbx, dbz := b1.x-b0.x, b1.z-b0.z
	denom := dax*dbz - daz*dbx
	if math.Abs(denom) <= 1e-12 {
		return vec2XZ{}, false
	}
	t := ((b0.x-a0.x)*dbz - (b0.z-a0.z)*dbx) / denom
	return vec2XZ{a0.x + dax*t, a0.z + daz*t}, true
}

func buildObstacleCutters3D(obs NavObstacle) (cutters [][]vec2XZ, minX, minZ, maxX, maxZ float64, minY, maxY float32, unboundedY bool, ok bool) {
	margin := float64(obs.Margin)
	if !isFinite32(obs.Margin) || margin < 0 {
		return nil, 0, 0, 0, 0, 0, 0, false, false
	}

	switch obs.Kind {
	case NavObstacleBox:
		if !isFiniteVec3(obs.Center) || !isFiniteVec3(obs.HalfExtents) || !isFinite32(obs.Yaw) ||
			obs.HalfExtents.X <= 0 || obs.HalfExtents.Y <= 0 || obs.HalfExtents.Z <= 0 {
			return nil, 0, 0, 0, 0, 0, 0, false, false
		}
		hx := float64(obs.HalfExtents.X) + margin
		hz := float64(obs.HalfExtents.Z) + margin
		ang := float64(obs.Yaw)
		cosY, sinY := math.Cos(ang), math.Sin(ang)
		// CCW viewed from +Y: (-hx, -hz) -> (-hx, +hz) -> (+hx, +hz) -> (+hx, -hz).
		local := [4]vec2XZ{
			{-hx, -hz},
			{-hx, hz},
			{hx, hz},
			{hx, -hz},
		}
		quad := make([]vec2XZ, 4)
		cx, cz := float64(obs.Center.X), float64(obs.Center.Z)
		for i, lp := range local {
			// Standard Y-axis rotation: x' = x*cosY + z*sinY, z' = -x*sinY + z*cosY.
			quad[i] = vec2XZ{
				x: cx + lp.x*cosY + lp.z*sinY,
				z: cz - lp.x*sinY + lp.z*cosY,
			}
		}
		cutters = [][]vec2XZ{quad}
		minY = obs.Center.Y - obs.HalfExtents.Y
		maxY = obs.Center.Y + obs.HalfExtents.Y

	case NavObstacleCylinder:
		if !isFiniteVec3(obs.Center) || !isFinite32(obs.Radius) || obs.Radius <= 0 || !isFinite32(obs.Height) || obs.Height < 0 {
			return nil, 0, 0, 0, 0, 0, 0, false, false
		}
		segs := obs.Segments
		if segs <= 0 {
			segs = 12
		}
		segs = min(max(segs, 6), 64)
		r := (float64(obs.Radius) + margin) / math.Cos(math.Pi/float64(segs))
		poly := make([]vec2XZ, segs)
		cx, cz := float64(obs.Center.X), float64(obs.Center.Z)
		// CCW viewed from +Y runs clockwise in (x, z), i.e., decreasing angle: (cos(-a), sin(-a)).
		for i := range segs {
			ang := -2 * math.Pi * float64(i) / float64(segs)
			poly[i] = vec2XZ{
				x: cx + r*math.Cos(ang),
				z: cz + r*math.Sin(ang),
			}
		}
		cutters = [][]vec2XZ{poly}
		if obs.Height == 0 {
			unboundedY = true
		} else {
			halfH := obs.Height * 0.5
			minY = obs.Center.Y - halfH
			maxY = obs.Center.Y + halfH
		}

	case NavObstaclePolygon:
		if len(obs.Vertices) < 3 || !isFinite32(obs.MinY) || !isFinite32(obs.MaxY) {
			return nil, 0, 0, 0, 0, 0, 0, false, false
		}
		raw := make([]vec2XZ, len(obs.Vertices))
		for i, v := range obs.Vertices {
			if !isFiniteVec3(v) {
				return nil, 0, 0, 0, 0, 0, 0, false, false
			}
			raw[i] = vec2XZ{x: float64(v.X), z: float64(v.Z)}
		}
		cleaned := cleanPolyXZ(raw)
		if len(cleaned) < 3 {
			return nil, 0, 0, 0, 0, 0, 0, false, false
		}
		if isConvexCCWXZ(cleaned) {
			if margin > 0 {
				cleaned = offsetConvexPolyXZ(cleaned, margin)
			}
			cutters = [][]vec2XZ{cleaned}
		} else {
			tris := earClipXZ(cleaned)
			if len(tris) == 0 {
				return nil, 0, 0, 0, 0, 0, 0, false, false
			}
			for _, tri := range tris {
				if margin > 0 {
					tri = offsetConvexPolyXZ(tri, margin)
				}
				cutters = append(cutters, tri)
			}
		}
		if obs.MinY == obs.MaxY {
			unboundedY = true
		} else {
			minY = min(obs.MinY, obs.MaxY)
			maxY = max(obs.MinY, obs.MaxY)
		}

	default:
		return nil, 0, 0, 0, 0, 0, 0, false, false
	}

	minX, minZ = math.Inf(1), math.Inf(1)
	maxX, maxZ = math.Inf(-1), math.Inf(-1)
	for _, c := range cutters {
		for _, v := range c {
			minX = min(minX, v.x)
			minZ = min(minZ, v.z)
			maxX = max(maxX, v.x)
			maxZ = max(maxZ, v.z)
		}
	}
	return cutters, minX, minZ, maxX, maxZ, minY, maxY, unboundedY, true
}

func tryMergeConvexXZ(a, b []vec2XZ) ([]vec2XZ, bool) {
	na, nb := len(a), len(b)
	if na+nb-2 > tiledMergeMax3D {
		return nil, false
	}
	for ia := range na {
		a0, a1 := a[ia], a[(ia+1)%na]
		for ib := range nb {
			b0, b1 := b[ib], b[(ib+1)%nb]
			if distSqXZ64(a0, b1) > tiledVertEps3D*tiledVertEps3D || distSqXZ64(a1, b0) > tiledVertEps3D*tiledVertEps3D {
				continue
			}
			aPrev := a[(ia+na-1)%na]
			aNext := a[(ia+2)%na]
			bPrev := b[(ib+nb-1)%nb]
			bNext := b[(ib+2)%nb]
			if triArea2XZ64(aPrev, a0, bNext) > 1e-7 || triArea2XZ64(bPrev, b0, aNext) > 1e-7 {
				continue
			}
			merged := make([]vec2XZ, 0, na+nb-2)
			for k := 1; k < na; k++ {
				merged = append(merged, a[(ia+k)%na])
			}
			for k := 1; k < nb; k++ {
				merged = append(merged, b[(ib+k)%nb])
			}
			cleaned := cleanPolyXZ(merged)
			if len(cleaned) >= 3 && len(cleaned) <= tiledMergeMax3D && isConvexCCWXZ(cleaned) {
				return cleaned, true
			}
		}
	}
	return nil, false
}

// buildPolyBVH3D constructs a flat 3D AABB BVH over polys.
func buildPolyBVH3D(polys []NavPoly) ([]navBVHNode3D, []int32) {
	n := len(polys)
	if n == 0 {
		return nil, nil
	}
	order := make([]int32, n)
	for i := range n {
		order[i] = int32(i)
	}
	nodes := make([]navBVHNode3D, 0, max(1, (2*n)/navBVHLeafSize3D+4))
	var build func(start, end, depth int) int32
	build = func(start, end, depth int) int32 {
		nodeIdx := int32(len(nodes))
		nodes = append(nodes, navBVHNode3D{})

		p0 := &polys[order[start]]
		bounds := p0.Bounds
		cMin := p0.Center
		cMax := p0.Center
		for i := start + 1; i < end; i++ {
			p := &polys[order[i]]
			bounds = bounds.Union(p.Bounds)
			cMin = cMin.Min(p.Center)
			cMax = cMax.Max(p.Center)
		}

		count := end - start
		if count <= navBVHLeafSize3D || depth >= 56 {
			nodes[nodeIdx] = navBVHNode3D{
				bounds: bounds,
				left:   int32(start),
				count:  int32(count),
			}
			return nodeIdx
		}

		span := cMax.Sub(cMin)
		axis := 0
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
			if axisVal(polys[order[i]].Center) < splitVal {
				i++
			} else {
				order[i], order[j] = order[j], order[i]
				j--
			}
		}
		leftCount := i - start
		rightCount := end - i
		if leftCount == 0 || rightCount == 0 || leftCount < count/8 || rightCount < count/8 {
			sub := order[start:end]
			slices.SortFunc(sub, func(a, b int32) int {
				ca := axisVal(polys[a].Center)
				cb := axisVal(polys[b].Center)
				if ca < cb {
					return -1
				}
				if ca > cb {
					return 1
				}
				return int(a - b)
			})
			i = start + count/2
		}
		leftIdx := build(start, i, depth+1)
		rightIdx := build(i, end, depth+1)
		nodes[nodeIdx] = navBVHNode3D{
			bounds: bounds,
			left:   leftIdx,
			right:  rightIdx,
		}
		return nodeIdx
	}
	build(0, n, 0)
	return nodes, order
}

type plane3D struct {
	normal math3d.Vec3
	planeD float32
}

func (pl plane3D) heightAt(x, z float64) float32 {
	if math.Abs(float64(pl.normal.Y)) < 1e-7 {
		return 0
	}
	return float32(-(float64(float64(pl.normal.X)*x) + float64(float64(pl.normal.Z)*z) + float64(pl.planeD)) / float64(pl.normal.Y))
}

func samePlane3D(a, b plane3D) bool {
	return a.normal.Dot(b.normal) >= 1-1e-4 && math.Abs(float64(a.planeD-b.planeD)) <= 1e-3
}

func liftPolyTo3D(xz []vec2XZ, pl plane3D, area NavArea, flags NavFlags) NavPoly {
	verts := make([]math3d.Vec3, len(xz))
	bounds := math3d.AABBEmpty()
	var sum math3d.Vec3
	for i, p := range xz {
		v := math3d.Vec3{X: float32(p.x), Y: pl.heightAt(p.x, p.z), Z: float32(p.z)}
		verts[i] = v
		bounds = bounds.ExpandPoint(v)
		sum = sum.Add(v)
	}
	if flags == 0 {
		flags = NavFlagWalk
	}
	return NavPoly{
		Vertices: verts,
		Normal:   pl.normal,
		PlaneD:   pl.planeD,
		Center:   sum.Scale(1 / float32(len(verts))),
		Bounds:   bounds,
		Area:     area,
		Flags:    flags,
	}
}
