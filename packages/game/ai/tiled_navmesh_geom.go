package ai

import (
	"math"
	"slices"

	"github.com/nsgo-dev/nsgo/core"
)

const (
	tiledAreaEps  = 1e-6
	tiledSplitEps = 1e-7
	tiledVertEps  = 1e-5
	tiledMergeMax = 8
)

// cleanPoly2D normalizes a 2D polygon to CCW winding, removes near-duplicate
// consecutive vertices and collinear vertices, and returns nil if degenerate.
func cleanPoly2D(raw []vec2) []vec2 {
	if len(raw) < 3 {
		return nil
	}
	pts := make([]vec2, 0, len(raw))
	for _, p := range raw {
		if !isFiniteVec2(p) {
			return nil
		}
		if len(pts) == 0 || distSq(pts[len(pts)-1], p) > tiledVertEps*tiledVertEps {
			pts = append(pts, p)
		}
	}
	for len(pts) >= 2 && distSq(pts[0], pts[len(pts)-1]) <= tiledVertEps*tiledVertEps {
		pts = pts[:len(pts)-1]
	}
	if len(pts) < 3 {
		return nil
	}
	area2 := polygonArea2(pts)
	if math.Abs(area2) <= tiledAreaEps*2 {
		return nil
	}
	if area2 < 0 {
		slices.Reverse(pts)
	}
	// Remove collinear intermediate vertices.
	changed := true
	for changed && len(pts) >= 3 {
		changed = false
		n := len(pts)
		out := make([]vec2, 0, n)
		for i := range n {
			prev := pts[(i+n-1)%n]
			cur := pts[i]
			next := pts[(i+1)%n]
			d := next.sub(prev).len()
			if d > 1e-12 && math.Abs(triArea2(prev, cur, next)) <= tiledVertEps*d {
				changed = true
				continue
			}
			out = append(out, cur)
		}
		pts = out
	}
	if len(pts) < 3 || polygonArea2(pts) <= tiledAreaEps*2 {
		return nil
	}
	return pts
}

// isConvexCCW2D reports whether a cleaned CCW polygon is convex.
func isConvexCCW2D(pts []vec2) bool {
	n := len(pts)
	if n < 3 {
		return false
	}
	for i := range n {
		a := pts[i]
		b := pts[(i+1)%n]
		c := pts[(i+2)%n]
		if triArea2(a, b, c) < -1e-7 {
			return false
		}
	}
	return true
}

// splitConvexPoly2D splits a CCW convex polygon poly by the directed line a -> b.
// left receives the half-plane on the left (triArea2(a, b, p) >= 0);
// right receives the half-plane on the right (triArea2(a, b, p) <= 0).
func splitConvexPoly2D(poly []vec2, a, b vec2) (left, right []vec2) {
	edge := b.sub(a)
	edgeLen := edge.len()
	if edgeLen <= 1e-12 || len(poly) < 3 {
		return nil, nil
	}
	ux, uy := edge.x/edgeLen, edge.y/edgeLen
	nx, ny := -uy, ux // inward (left) unit normal

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
		d := (p.x-a.x)*nx + (p.y-a.y)*ny
		dists[i] = d
		if d > tiledSplitEps {
			hasPos = true
		} else if d < -tiledSplitEps {
			hasNeg = true
		}
	}
	if !hasNeg {
		out := make([]vec2, n)
		copy(out, poly)
		return out, nil
	}
	if !hasPos {
		out := make([]vec2, n)
		copy(out, poly)
		return nil, out
	}

	left = make([]vec2, 0, n+1)
	right = make([]vec2, 0, n+1)
	for i := range n {
		j := (i + 1) % n
		p0, p1 := poly[i], poly[j]
		d0, d1 := dists[i], dists[j]
		s0 := 0
		if d0 > tiledSplitEps {
			s0 = 1
		} else if d0 < -tiledSplitEps {
			s0 = -1
		}
		s1 := 0
		if d1 > tiledSplitEps {
			s1 = 1
		} else if d1 < -tiledSplitEps {
			s1 = -1
		}
		if s0 >= 0 {
			left = append(left, p0)
		}
		if s0 <= 0 {
			right = append(right, p0)
		}
		if (s0 > 0 && s1 < 0) || (s0 < 0 && s1 > 0) {
			t := d0 / (d0 - d1)
			mid := vec2{
				x: p0.x + (p1.x-p0.x)*t,
				y: p0.y + (p1.y-p0.y)*t,
			}
			left = append(left, mid)
			right = append(right, mid)
		}
	}
	left = cleanPoly2D(left)
	right = cleanPoly2D(right)
	return left, right
}

// clipPolyToBox2D clips a CCW convex polygon against [minX, maxX] x [minY, maxY].
func clipPolyToBox2D(poly []vec2, minX, minY, maxX, maxY float64) []vec2 {
	if len(poly) < 3 {
		return nil
	}
	cur := poly
	// Clip against the 4 CCW edges of the box (keeping the left/interior side).
	edges := [4][2]vec2{
		{{minX, minY}, {maxX, minY}}, // y >= minY
		{{maxX, minY}, {maxX, maxY}}, // x <= maxX
		{{maxX, maxY}, {minX, maxY}}, // y <= maxY
		{{minX, maxY}, {minX, minY}}, // x >= minX
	}
	for _, e := range edges {
		cur, _ = splitConvexPoly2D(cur, e[0], e[1])
		if len(cur) < 3 {
			return nil
		}
	}
	return cur
}

// convexPolysOverlap2D reports whether two CCW convex polygons have a strictly
// positive-area interior intersection (using Separating Axis Theorem).
func convexPolysOverlap2D(a, b []vec2) bool {
	if len(a) < 3 || len(b) < 3 {
		return false
	}
	if hasSeparatingAxis2D(a, b) || hasSeparatingAxis2D(b, a) {
		return false
	}
	return true
}

func hasSeparatingAxis2D(poly, other []vec2) bool {
	n := len(poly)
	for i := range n {
		p0 := poly[i]
		p1 := poly[(i+1)%n]
		edge := p1.sub(p0)
		edgeLen := edge.len()
		if edgeLen <= 1e-12 {
			continue
		}
		// Outward normal for CCW polygon is (edge.y, -edge.x) / edgeLen.
		nx, ny := edge.y/edgeLen, -edge.x/edgeLen
		allOutside := true
		for _, q := range other {
			if (q.x-p0.x)*nx+(q.y-p0.y)*ny < -tiledSplitEps {
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

// carveConvexPoly2D subtracts convex CCW obstacle cutter from convex CCW polygon poly.
// It returns all remaining convex pieces outside cutter, plus the convex intersection
// inside cutter (nil if poly and cutter do not overlap in interior).
func carveConvexPoly2D(poly, cutter []vec2) (outside [][]vec2, inside []vec2) {
	if !convexPolysOverlap2D(poly, cutter) {
		return [][]vec2{poly}, nil
	}
	rem := poly
	n := len(cutter)
	for i := range n {
		a := cutter[i]
		b := cutter[(i+1)%n]
		inHalf, outHalf := splitConvexPoly2D(rem, a, b)
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

// pointInTri2D reports whether p lies inside or on the boundary of CCW triangle (a, b, c).
func pointInTri2D(p, a, b, c vec2) bool {
	return triArea2(a, b, p) >= -1e-9 &&
		triArea2(b, c, p) >= -1e-9 &&
		triArea2(c, a, p) >= -1e-9
}

// earClip2D decomposes a simple CCW 2D polygon into CCW triangles.
func earClip2D(pts []vec2) [][]vec2 {
	n := len(pts)
	if n < 3 {
		return nil
	}
	if n == 3 {
		return [][]vec2{{pts[0], pts[1], pts[2]}}
	}
	ring := make([]int, n)
	for i := range n {
		ring[i] = i
	}
	var tris [][]vec2
	guard := n * n
	for len(ring) > 3 && guard > 0 {
		guard--
		m := len(ring)
		clipped := false
		for i := range m {
			prev := pts[ring[(i+m-1)%m]]
			cur := pts[ring[i]]
			next := pts[ring[(i+1)%m]]
			if triArea2(prev, cur, next) <= 1e-9 {
				continue
			}
			ear := true
			for k := range m {
				if k == (i+m-1)%m || k == i || k == (i+1)%m {
					continue
				}
				if pointInTri2D(pts[ring[k]], prev, cur, next) {
					ear = false
					break
				}
			}
			if !ear {
				continue
			}
			if tri := cleanPoly2D([]vec2{prev, cur, next}); len(tri) == 3 {
				tris = append(tris, tri)
			}
			ring = append(ring[:i], ring[i+1:]...)
			clipped = true
			break
		}
		if !clipped {
			// Fallback for degenerate nearly-collinear vertices: clip the sharpest convex corner.
			best := -1
			bestArea := 0.0
			for i := range m {
				a := triArea2(pts[ring[(i+m-1)%m]], pts[ring[i]], pts[ring[(i+1)%m]])
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
			if tri := cleanPoly2D([]vec2{prev, cur, next}); len(tri) == 3 {
				tris = append(tris, tri)
			}
			ring = append(ring[:best], ring[best+1:]...)
		}
	}
	if len(ring) == 3 {
		if tri := cleanPoly2D([]vec2{pts[ring[0]], pts[ring[1]], pts[ring[2]]}); len(tri) == 3 {
			tris = append(tris, tri)
		}
	}
	return tris
}

// offsetConvexPoly2D expands a CCW convex polygon outward by margin > 0,
// beveling sharp corners so every resulting edge keeps at least margin from the original polygon.
func offsetConvexPoly2D(poly []vec2, margin float64) []vec2 {
	if margin <= 0 || len(poly) < 3 {
		return poly
	}
	n := len(poly)
	type offsetEdge struct {
		p0, p1 vec2
		nx, ny float64
	}
	edges := make([]offsetEdge, 0, n)
	for i := range n {
		a := poly[i]
		b := poly[(i+1)%n]
		d := b.sub(a)
		l := d.len()
		if l <= 1e-12 {
			continue
		}
		// Outward unit normal for CCW edge a->b is (d.y/l, -d.x/l).
		nx, ny := d.y/l, -d.x/l
		edges = append(edges, offsetEdge{
			p0: vec2{a.x + nx*margin, a.y + ny*margin},
			p1: vec2{b.x + nx*margin, b.y + ny*margin},
			nx: nx,
			ny: ny,
		})
	}
	m := len(edges)
	if m < 3 {
		return poly
	}
	out := make([]vec2, 0, m*2)
	for i := range m {
		e0 := edges[(i+m-1)%m]
		e1 := edges[i]
		// Intersect lines e0 and e1; bevel when acute (dot(n0, n1) < 0) to prevent long spikes.
		dot := e0.nx*e1.nx + e0.ny*e1.ny
		if dot < 0.0 {
			// Bevel at the midpoint normal direction at distance margin from the original vertex.
			out = append(out, e0.p1)
			v := poly[i]
			bx, by := e0.nx+e1.nx, e0.ny+e1.ny
			bl := math.Hypot(bx, by)
			if bl > 1e-9 {
				bx /= bl
				by /= bl
				// Intersect e0 and e1 with the bevel plane passing through v + margin*(bx, by).
				bp := vec2{v.x + bx*margin, v.y + by*margin}
				if pt, ok := intersectLines2D(e0.p0, e0.p1, bp, vec2{bp.x - by, bp.y + bx}); ok {
					out = append(out, pt)
				}
				if pt, ok := intersectLines2D(bp, vec2{bp.x - by, bp.y + bx}, e1.p0, e1.p1); ok {
					out = append(out, pt)
				}
			}
			out = append(out, e1.p0)
			continue
		}
		if pt, ok := intersectLines2D(e0.p0, e0.p1, e1.p0, e1.p1); ok {
			out = append(out, pt)
		} else {
			out = append(out, e0.p1, e1.p0)
		}
	}
	if cleaned := cleanPoly2D(out); len(cleaned) >= 3 {
		return cleaned
	}
	return poly
}

func intersectLines2D(a0, a1, b0, b1 vec2) (vec2, bool) {
	da := a1.sub(a0)
	db := b1.sub(b0)
	denom := da.cross(db)
	if math.Abs(denom) <= 1e-12 {
		return vec2{}, false
	}
	t := b0.sub(a0).cross(db) / denom
	return vec2{a0.x + da.x*t, a0.y + da.y*t}, true
}

// buildObstacleCutters2D converts a NavObstacle into one or more CCW convex polygons
// and returns their combined 2D AABB. Returns ok == false when the obstacle is invalid.
func buildObstacleCutters2D(obs NavObstacle) (cutters [][]vec2, minX, minY, maxX, maxY float64, ok bool) {
	margin := float64(obs.Margin)
	if !isFinite32(obs.Margin) || margin < 0 {
		return nil, 0, 0, 0, 0, false
	}

	switch obs.Kind {
	case NavObstacleBox:
		c := toVec2(obs.Center)
		h := toVec2(obs.HalfExtents)
		if !isFiniteVec2(c) || !isFiniteVec2(h) || h.x <= 0 || h.y <= 0 || !isFinite32(obs.Angle) {
			return nil, 0, 0, 0, 0, false
		}
		hx := h.x + margin
		hy := h.y + margin
		ang := float64(obs.Angle)
		cosA, sinA := math.Cos(ang), math.Sin(ang)
		local := [4]vec2{
			{-hx, -hy},
			{hx, -hy},
			{hx, hy},
			{-hx, hy},
		}
		quad := make([]vec2, 4)
		for i, lp := range local {
			quad[i] = vec2{
				x: c.x + lp.x*cosA - lp.y*sinA,
				y: c.y + lp.x*sinA + lp.y*cosA,
			}
		}
		cutters = [][]vec2{quad}

	case NavObstacleCircle:
		c := toVec2(obs.Center)
		r := float64(obs.Radius)
		if !isFiniteVec2(c) || !isFinite32(obs.Radius) || r <= 0 {
			return nil, 0, 0, 0, 0, false
		}
		segs := obs.Segments
		if segs <= 0 {
			segs = 12
		}
		segs = min(max(segs, 6), 64)
		// Circumscribed regular polygon guarantees the carved shape covers the entire disc of radius r + margin.
		rr := (r + margin) / math.Cos(math.Pi/float64(segs))
		poly := make([]vec2, segs)
		for i := range segs {
			ang := 2 * math.Pi * float64(i) / float64(segs)
			poly[i] = vec2{
				x: c.x + rr*math.Cos(ang),
				y: c.y + rr*math.Sin(ang),
			}
		}
		cutters = [][]vec2{poly}

	case NavObstaclePolygon:
		if len(obs.Vertices) < 3 {
			return nil, 0, 0, 0, 0, false
		}
		raw := make([]vec2, len(obs.Vertices))
		for i, p := range obs.Vertices {
			raw[i] = toVec2(p)
		}
		cleaned := cleanPoly2D(raw)
		if len(cleaned) < 3 {
			return nil, 0, 0, 0, 0, false
		}
		if isConvexCCW2D(cleaned) {
			if margin > 0 {
				cleaned = offsetConvexPoly2D(cleaned, margin)
			}
			cutters = [][]vec2{cleaned}
		} else {
			tris := earClip2D(cleaned)
			if len(tris) == 0 {
				return nil, 0, 0, 0, 0, false
			}
			for _, tri := range tris {
				if margin > 0 {
					tri = offsetConvexPoly2D(tri, margin)
				}
				cutters = append(cutters, tri)
			}
		}

	default:
		return nil, 0, 0, 0, 0, false
	}

	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for _, c := range cutters {
		for _, v := range c {
			minX = min(minX, v.x)
			minY = min(minY, v.y)
			maxX = max(maxX, v.x)
			maxY = max(maxY, v.y)
		}
	}
	return cutters, minX, minY, maxX, maxY, true
}

// tryMergeConvex2D attempts to merge two CCW convex polygons sharing a full edge
// into one CCW convex polygon with at most tiledMergeMax vertices.
func tryMergeConvex2D(a, b []vec2) ([]vec2, bool) {
	na, nb := len(a), len(b)
	if na+nb-2 > tiledMergeMax {
		return nil, false
	}
	for ia := range na {
		a0, a1 := a[ia], a[(ia+1)%na]
		for ib := range nb {
			b0, b1 := b[ib], b[(ib+1)%nb]
			// Shared full edge runs in opposite directions: a0 == b1 and a1 == b0.
			if distSq(a0, b1) > tiledVertEps*tiledVertEps || distSq(a1, b0) > tiledVertEps*tiledVertEps {
				continue
			}
			// Check convexity at both merge junctions.
			aPrev := a[(ia+na-1)%na]
			aNext := a[(ia+2)%na]
			bPrev := b[(ib+nb-1)%nb]
			bNext := b[(ib+2)%nb]
			if triArea2(aPrev, a0, bNext) < -1e-7 || triArea2(bPrev, b0, aNext) < -1e-7 {
				continue
			}
			merged := make([]vec2, 0, na+nb-2)
			for k := 1; k < na; k++ {
				merged = append(merged, a[(ia+k)%na])
			}
			for k := 1; k < nb; k++ {
				merged = append(merged, b[(ib+k)%nb])
			}
			cleaned := cleanPoly2D(merged)
			if len(cleaned) >= 3 && len(cleaned) <= tiledMergeMax && isConvexCCW2D(cleaned) {
				return cleaned, true
			}
		}
	}
	return nil, false
}

// buildPolyBVH2D constructs a flat AABB BVH over a slice of navPoly.
func buildPolyBVH2D(polys []navPoly) ([]navBVHNode2D, []int) {
	n := len(polys)
	if n == 0 {
		return nil, nil
	}
	order := make([]int, n)
	for i := range n {
		order[i] = i
	}
	nodes := make([]navBVHNode2D, 0, max(1, (2*n)/navBVHLeafSize+4))
	var build func(start, end, depth int) int32
	build = func(start, end, depth int) int32 {
		nodeIdx := int32(len(nodes))
		nodes = append(nodes, navBVHNode2D{})

		p0 := &polys[order[start]]
		minX, minY, maxX, maxY := p0.minX, p0.minY, p0.maxX, p0.maxY
		cMinX, cMinY, cMaxX, cMaxY := p0.cx, p0.cy, p0.cx, p0.cy
		for i := start + 1; i < end; i++ {
			p := &polys[order[i]]
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
			nodes[nodeIdx] = navBVHNode2D{
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

		i, j := start, end-1
		for i <= j {
			var c float64
			if splitY {
				c = polys[order[i]].cy
			} else {
				c = polys[order[i]].cx
			}
			if c < splitVal {
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
			if splitY {
				slices.SortFunc(sub, func(a, b int) int {
					if polys[a].cy < polys[b].cy {
						return -1
					}
					if polys[a].cy > polys[b].cy {
						return 1
					}
					return a - b
				})
			} else {
				slices.SortFunc(sub, func(a, b int) int {
					if polys[a].cx < polys[b].cx {
						return -1
					}
					if polys[a].cx > polys[b].cx {
						return 1
					}
					return a - b
				})
			}
			i = mid
		}

		leftIdx := build(start, i, depth+1)
		rightIdx := build(i, end, depth+1)
		nodes[nodeIdx] = navBVHNode2D{
			minX:  minX,
			minY:  minY,
			maxX:  maxX,
			maxY:  maxY,
			left:  leftIdx,
			right: rightIdx,
		}
		return nodeIdx
	}
	build(0, n, 0)
	return nodes, order
}

func makeNavPoly2D(verts []vec2, area NavArea, flags NavFlags) navPoly {
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
	if flags == 0 {
		flags = NavFlagWalk
	}
	return navPoly{
		verts:    verts,
		minX:     minX,
		minY:     minY,
		maxX:     maxX,
		maxY:     maxY,
		cx:       sumX * invN,
		cy:       sumY * invN,
		surfArea: 0.5 * math.Abs(polygonArea2(verts)),
		area:     area,
		flags:    flags,
	}
}

func polyPoints2D(verts []vec2) []core.Point {
	out := make([]core.Point, len(verts))
	for i, v := range verts {
		out[i] = toPoint(v)
	}
	return out
}
