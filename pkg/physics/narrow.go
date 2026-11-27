package physics

import (
	"math"

	"github.com/nsgo-dev/game/internal/detmath"
)

// The narrowphase for polygons, capsules, segments and chains treats every convex shape as a rounded hull: a core of 1 to MaxPolygonVertices points (a point for a circle, a segment for a capsule or segment, a polygon for a box or polygon) grown by a radius. Overlap is the separating axis test on the cores plus the radii, and the contact manifold comes from clipping the incident edge against the reference face, as in Box2D's b2CollidePolygons.

const (
	// faceContactTolerance is how much smaller than the true core distance a face's separation may be while the contact still counts as face against face, which clips up to two points instead of one.
	faceContactTolerance = 1e-3
	// referenceTieTolerance keeps the reference face on shape A unless shape B's face separates by clearly more, so manifolds do not flip between frames.
	referenceTieTolerance = 1e-4
	// chainNormalTolerance is how close a contact normal must be to a chain segment's face normal to count as a face contact.
	chainNormalTolerance = 1e-6
)

// hull is a convex core of count points, wound with positive signed area, with the outward normal of each edge, grown by radius r.
type hull struct {
	v     [MaxPolygonVertices]vec2
	n     [MaxPolygonVertices]vec2
	count int
	r     float64
}

// edgeCount returns the number of faces of h: count for polygons, 2 (both sides) for segments and 0 for points.
func (h *hull) edgeCount() int {
	if h.count >= 3 {
		return h.count
	}
	if h.count == 2 {
		return 2
	}
	return 0
}

// edge returns the endpoints of face i.
func (h *hull) edge(i int) (vec2, vec2) {
	return h.v[i], h.v[(i+1)%h.count]
}

// finish computes the edge normals of h, collapsing a zero-length segment to a point.
func (h *hull) finish() {
	if h.count == 2 && h.v[0] == h.v[1] {
		h.count = 1
	}
	for i := range h.edgeCount() {
		a, b := h.edge(i)
		ex, ey := b.x-a.x, b.y-a.y
		if l := detmath.Hypot(ex, ey); l > 0 {
			h.n[i] = vec2{ey / l, -ex / l}
		} else {
			h.n[i] = vec2{}
		}
	}
}

func pointHull(x, y, r float64) hull {
	h := hull{count: 1, r: r}
	h.v[0] = vec2{x, y}
	return h
}

func segmentHull(a, b vec2, r float64) hull {
	h := hull{count: 2, r: r}
	h.v[0], h.v[1] = a, b
	h.finish()
	return h
}

func boxHull(b Box) hull {
	minX, minY := float64(b.X), float64(b.Y)
	maxX, maxY := minX+float64(b.W), minY+float64(b.H)
	h := hull{count: 4}
	h.v[0], h.v[1], h.v[2], h.v[3] = vec2{minX, minY}, vec2{maxX, minY}, vec2{maxX, maxY}, vec2{minX, maxY}
	h.finish()
	return h
}

// orientedBoxHull returns the hull of a box with half extents (hw, hh) centered at (cx, cy) and rotated by angle.
func orientedBoxHull(cx, cy, hw, hh, angle float64) hull {
	s, c := detmath.Sincos(angle)
	local := [4]vec2{{-hw, -hh}, {hw, -hh}, {hw, hh}, {-hw, hh}}
	h := hull{count: 4}
	for i, l := range local {
		h.v[i] = vec2{cx + float64(l.x*c) - float64(l.y*s), cy + float64(l.x*s) + float64(l.y*c)}
	}
	h.finish()
	return h
}

func polygonHull(p Polygon) hull {
	h := hull{count: p.Count, r: float64(p.Radius)}
	for i := range p.Count {
		h.v[i] = vec2{float64(p.Vertices[i].X), float64(p.Vertices[i].Y)}
	}
	h.finish()
	return h
}

// finite reports whether every core point and the radius of h are finite numbers; a NaN or infinite coordinate would otherwise give a contact with a zero normal.
func (h *hull) finite() bool {
	if math.IsNaN(h.r) || math.IsInf(h.r, 0) {
		return false
	}
	for _, v := range h.v[:h.count] {
		if math.IsNaN(v.x) || math.IsNaN(v.y) || math.IsInf(v.x, 0) || math.IsInf(v.y, 0) {
			return false
		}
	}
	return true
}

// shapeHull returns the rounded hull of every shape but a chain, or false when s is empty or has a coordinate that is not finite.
func shapeHull(s Shape) (hull, bool) {
	h, ok := rawShapeHull(s)
	if !ok || !h.finite() {
		return hull{}, false
	}
	return h, true
}

func rawShapeHull(s Shape) (hull, bool) {
	if s.Empty() {
		return hull{}, false
	}
	switch s.Kind {
	case ShapeBox:
		return boxHull(s.Box), true
	case ShapeCircle:
		return pointHull(float64(s.Circle.X), float64(s.Circle.Y), float64(s.Circle.R)), true
	case ShapePolygon:
		return polygonHull(s.Polygon), true
	case ShapeCapsule:
		c := s.Capsule
		return segmentHull(vec2{float64(c.X1), float64(c.Y1)}, vec2{float64(c.X2), float64(c.Y2)}, float64(c.R)), true
	case ShapeSegment:
		g := s.Segment
		return segmentHull(vec2{float64(g.X1), float64(g.Y1)}, vec2{float64(g.X2), float64(g.Y2)}, 0), true
	}
	return hull{}, false
}

// hasArea reports whether h encloses area: a polygon core or a positive radius.
func (h *hull) hasArea() bool { return h.count >= 3 || h.r > 0 }

// bounds returns the axis-aligned extents of h including its radius.
func (h *hull) bounds() (minX, minY, maxX, maxY float64) {
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for _, v := range h.v[:h.count] {
		minX, maxX = min(minX, v.x), max(maxX, v.x)
		minY, maxY = min(minY, v.y), max(maxY, v.y)
	}
	return minX - h.r, minY - h.r, maxX + h.r, maxY + h.r
}

// coreContains reports whether p lies inside or on the core polygon of h (only polygons have an inside).
func (h *hull) coreContains(p vec2) bool {
	if h.count < 3 {
		return false
	}
	for i := range h.count {
		if dot2(h.n[i], vec2{p.x - h.v[i].x, p.y - h.v[i].y}) > 0 {
			return false
		}
	}
	return true
}

// coreDistance returns the distance from p to the core of h, which is 0 inside a polygon core.
func (h *hull) coreDistance(p vec2) float64 {
	if h.coreContains(p) {
		return 0
	}
	if h.count == 1 {
		return detmath.Hypot(p.x-h.v[0].x, p.y-h.v[0].y)
	}
	best := math.Inf(1)
	edges := h.count
	if h.count == 2 {
		edges = 1
	}
	for i := range edges {
		a, b := h.edge(i)
		q := closestOnSegment(a, b, p)
		best = min(best, detmath.Hypot(p.x-q.x, p.y-q.y))
	}
	return best
}

// contains reports whether p lies inside or on the boundary of the rounded hull.
func (h *hull) contains(p vec2) bool {
	if !h.hasArea() {
		return false
	}
	return h.coreDistance(p) <= h.r
}

// closestOnSegment returns the point of segment ab closest to p.
func closestOnSegment(a, b, p vec2) vec2 {
	ex, ey := b.x-a.x, b.y-a.y
	ll := float64(ex*ex) + float64(ey*ey)
	if ll == 0 {
		return a
	}
	t := min(max((float64((p.x-a.x)*ex)+float64((p.y-a.y)*ey))/ll, 0), 1)
	return vec2{a.x + float64(ex*t), a.y + float64(ey*t)}
}

// closestSegmentPoints returns the closest points of segments p1q1 and p2q2 and the squared distance between them (Ericson, Real-Time Collision Detection 5.1.9).
func closestSegmentPoints(p1, q1, p2, q2 vec2) (c1, c2 vec2, distSq float64) {
	d1 := vec2{q1.x - p1.x, q1.y - p1.y}
	d2 := vec2{q2.x - p2.x, q2.y - p2.y}
	r := vec2{p1.x - p2.x, p1.y - p2.y}
	a := dot2(d1, d1)
	e := dot2(d2, d2)
	f := dot2(d2, r)
	var s, t float64
	switch {
	case a == 0 && e == 0:
		s, t = 0, 0
	case a == 0:
		s = 0
		t = min(max(f/e, 0), 1)
	default:
		c := dot2(d1, r)
		if e == 0 {
			t = 0
			s = min(max(-c/a, 0), 1)
		} else {
			b := dot2(d1, d2)
			denom := float64(a*e) - float64(b*b)
			if denom != 0 {
				s = min(max((float64(b*f)-float64(c*e))/denom, 0), 1)
			}
			t = (float64(b*s) + f) / e
			if t < 0 {
				t = 0
				s = min(max(-c/a, 0), 1)
			} else if t > 1 {
				t = 1
				s = min(max((b-c)/a, 0), 1)
			}
		}
	}
	c1 = vec2{p1.x + float64(d1.x*s), p1.y + float64(d1.y*s)}
	c2 = vec2{p2.x + float64(d2.x*t), p2.y + float64(d2.y*t)}
	dx, dy := c1.x-c2.x, c1.y-c2.y
	return c1, c2, float64(dx*dx) + float64(dy*dy)
}

// coreSegments returns how many segments make up the boundary of h's core (one for a point or a segment).
func (h *hull) coreSegments() int {
	if h.count >= 3 {
		return h.count
	}
	return 1
}

func (h *hull) coreSegment(i int) (vec2, vec2) {
	if h.count == 1 {
		return h.v[0], h.v[0]
	}
	return h.edge(i)
}

// closestCorePoints returns the closest points between the boundaries of the cores of a and b, and their distance. Callers use it only when the cores are disjoint.
func closestCorePoints(a, b *hull) (pa, pb vec2, dist float64) {
	best := math.Inf(1)
	for i := range a.coreSegments() {
		a0, a1 := a.coreSegment(i)
		for j := range b.coreSegments() {
			b0, b1 := b.coreSegment(j)
			c1, c2, dsq := closestSegmentPoints(a0, a1, b0, b1)
			if dsq < best {
				best, pa, pb = dsq, c1, c2
			}
		}
	}
	return pa, pb, math.Sqrt(best)
}

// maxFaceSeparation returns the largest separation of b's core from a face of a's core, and that face, or -1 when a has no faces.
func maxFaceSeparation(a, b *hull) (float64, int) {
	best, edge := math.Inf(-1), -1
	for i := range a.edgeCount() {
		n, v := a.n[i], a.v[i]
		sep := math.Inf(1)
		for _, w := range b.v[:b.count] {
			sep = min(sep, dot2(n, vec2{w.x - v.x, w.y - v.y}))
		}
		if sep > best {
			best, edge = sep, i
		}
	}
	return best, edge
}

// capSeparation returns the separation of b's core beyond either end of a's segment core, the axes the separating axis test needs for segments besides their faces.
func capSeparation(a, b *hull) float64 {
	if a.count != 2 {
		return math.Inf(-1)
	}
	d := vec2{a.v[1].x - a.v[0].x, a.v[1].y - a.v[0].y}
	l := detmath.Hypot(d.x, d.y)
	if l == 0 {
		return math.Inf(-1)
	}
	d = vec2{d.x / l, d.y / l}
	s1, s0 := math.Inf(1), math.Inf(1)
	for _, w := range b.v[:b.count] {
		s1 = min(s1, dot2(d, vec2{w.x - a.v[1].x, w.y - a.v[1].y}))
		s0 = min(s0, -dot2(d, vec2{w.x - a.v[0].x, w.y - a.v[0].y}))
	}
	return max(s0, s1)
}

// manifoldPoint is one contact point. id names the pair of features (reference face, incident vertex or clipping plane) that produced it, so the solver can carry the point's impulses to the next step while the same features stay in contact.
type manifoldPoint struct {
	x, y  float64
	depth float64
	id    uint32
}

// Feature ids of contact points that no face clipping produced.
const (
	// pointFeatureID marks the single point of two rounded hulls whose cores are points, or whose closest core features are a vertex and a vertex or edge.
	pointFeatureID uint32 = 1 << 16
)

// clipFeatureID returns the id of a point clipped from incident edge inc against reference face ref. code is 0 or 1 for an incident vertex kept by clipping, 2 or 3 for a point cut by a side plane of the reference face, 4 for a lone incident point and 5 + i for the fallback deepest incident vertex i. flip reports that the reference face is on shape B.
func clipFeatureID(flip bool, ref, inc int, code uint32) uint32 {
	id := uint32(ref)<<12 | uint32(inc)<<8 | code
	if flip {
		id |= 1 << 15
	}
	return id
}

// hullManifold holds up to two contact points sharing one unit normal that points from shape A toward shape B. sub is the index of the chain segment that produced it, 0 for other shapes.
type hullManifold struct {
	nx, ny float64
	count  int
	sub    int
	points [2]manifoldPoint
}

func (m *hullManifold) maxDepth() float64 {
	d := 0.0
	for i := range m.count {
		d = max(d, m.points[i].depth)
	}
	return d
}

// collideHulls returns the contact manifold of rounded hulls a and b, with the normal pointing from a toward b. Points report a positive depth; touching hulls report no points. Two hulls without area (segments) never collide.
func collideHulls(a, b *hull) hullManifold {
	return collideHullsWithin(a, b, 0)
}

// collideHullsWithin is collideHulls that also keeps, in a manifold of overlapping hulls, the clipped points still apart by less than margin, with a negative depth.
func collideHullsWithin(a, b *hull, margin float64) hullManifold {
	if !a.hasArea() && !b.hasArea() {
		return hullManifold{}
	}
	total := a.r + b.r
	if a.count == 1 && b.count == 1 {
		dx, dy := b.v[0].x-a.v[0].x, b.v[0].y-a.v[0].y
		distSq := float64(dx*dx) + float64(dy*dy)
		if distSq >= total*total {
			return hullManifold{}
		}
		if distSq == 0 {
			m := hullManifold{nx: 0, ny: 1, count: 1}
			m.points[0] = manifoldPoint{x: a.v[0].x, y: a.v[0].y, depth: total, id: pointFeatureID}
			return m
		}
		dist := math.Sqrt(distSq)
		return singlePointManifold(a, b, a.v[0], b.v[0], dist)
	}

	sepA, edgeA := maxFaceSeparation(a, b)
	sepB, edgeB := maxFaceSeparation(b, a)
	maxSep := max(sepA, sepB, capSeparation(a, b), capSeparation(b, a))
	if maxSep >= total {
		return hullManifold{}
	}
	if maxSep > 0 {
		// The cores are disjoint: the rounding radii make the contact.
		pa, pb, dist := closestCorePoints(a, b)
		if dist >= total {
			return hullManifold{}
		}
		if dist > 0 {
			refA := edgeA >= 0 && sepA >= dist-faceContactTolerance
			refB := edgeB >= 0 && sepB >= dist-faceContactTolerance
			if refA && (!refB || sepA+referenceTieTolerance >= sepB) {
				return clipHulls(a, edgeA, b, false, margin)
			}
			if refB {
				return clipHulls(b, edgeB, a, true, margin)
			}
			return singlePointManifold(a, b, pa, pb, dist)
		}
	}
	// The cores overlap or touch: the face of least penetration is the reference.
	if edgeB >= 0 && (edgeA < 0 || sepB > sepA+referenceTieTolerance) {
		return clipHulls(b, edgeB, a, true, margin)
	}
	if edgeA < 0 {
		return hullManifold{}
	}
	return clipHulls(a, edgeA, b, false, margin)
}

// singlePointManifold returns a one-point manifold between the closest core points pa (on a) and pb (on b) at distance dist > 0.
func singlePointManifold(a, b *hull, pa, pb vec2, dist float64) hullManifold {
	nx, ny := (pb.x-pa.x)/dist, (pb.y-pa.y)/dist
	depth := a.r + b.r - dist
	m := hullManifold{nx: nx, ny: ny, count: 1}
	// Midway between the surface of a (pa + n*ra) and the surface of b (pb - n*rb).
	m.points[0] = manifoldPoint{
		x:     float64(0.5*(pa.x+pb.x)) + float64(0.5*nx*(a.r-b.r)),
		y:     float64(0.5*(pa.y+pb.y)) + float64(0.5*ny*(a.r-b.r)),
		depth: depth,
		id:    pointFeatureID,
	}
	return m
}

// clipHulls clips the incident face of inc against face edge of ref and returns the manifold. flip reports that ref is shape B, so the normal is reversed to point from A toward B. Clipped points apart by less than margin are kept with a negative depth, as long as one point overlaps.
func clipHulls(ref *hull, edge int, inc *hull, flip bool, margin float64) hullManifold {
	nR := ref.n[edge]
	v1, v2 := ref.edge(edge)
	total := ref.r + inc.r

	var pts [2]vec2
	var ids [2]uint32
	np := 0
	if inc.count == 1 {
		pts[0] = inc.v[0]
		ids[0] = clipFeatureID(flip, edge, 0, 4)
		np = 1
	} else {
		// The incident face is the one most anti-parallel to the reference normal.
		k, best := 0, math.Inf(1)
		for i := range inc.edgeCount() {
			if d := dot2(inc.n[i], nR); d < best {
				best, k = d, i
			}
		}
		w1, w2 := inc.edge(k)
		t := vec2{v2.x - v1.x, v2.y - v1.y}
		if l := detmath.Hypot(t.x, t.y); l > 0 {
			t = vec2{t.x / l, t.y / l}
		}
		inIDs := [2]uint32{clipFeatureID(flip, edge, k, 0), clipFeatureID(flip, edge, k, 1)}
		clipped, clippedIDs, n := clipSegmentToLine([2]vec2{w1, w2}, inIDs, 2, vec2{-t.x, -t.y}, -dot2(t, v1), clipFeatureID(flip, edge, k, 2))
		clipped, clippedIDs, n = clipSegmentToLine(clipped, clippedIDs, n, t, dot2(t, v2), clipFeatureID(flip, edge, k, 3))
		if n == 0 {
			// The incident face misses the reference face's span: fall back to the deepest incident vertex.
			deep, depth := 0, math.Inf(1)
			for i := range inc.count {
				if d := dot2(nR, inc.v[i]); d < depth {
					depth, deep = d, i
				}
			}
			pts[0] = inc.v[deep]
			ids[0] = clipFeatureID(flip, edge, 0, 5+uint32(deep))
			np = 1
		} else {
			pts, ids = clipped, clippedIDs
			np = n
		}
	}

	m := hullManifold{nx: nR.x, ny: nR.y}
	touching := false
	rA, rB := ref.r, inc.r
	if flip {
		m.nx, m.ny = -nR.x, -nR.y
		rA, rB = inc.r, ref.r
	}
	for pi, p := range pts[:np] {
		s := dot2(nR, vec2{p.x - v1.x, p.y - v1.y})
		if s >= total+margin {
			continue
		}
		if !(s >= total) {
			touching = true
		}
		// Midway between the two surfaces along the normal.
		var off float64
		if flip {
			off = 0.5 * (rA + s - rB)
		} else {
			off = 0.5 * (rA - s - rB)
		}
		m.points[m.count] = manifoldPoint{x: p.x + float64(m.nx*off), y: p.y + float64(m.ny*off), depth: total - s, id: ids[pi]}
		m.count++
	}
	if !touching {
		return hullManifold{}
	}
	return m
}

// chainSegmentManifold returns the manifold of segment i of chain c against hull o, with the normal pointing from the segment toward o, after removing the ghost contacts its interior vertices would cause.
func chainSegmentManifold(c *Chain, i int, o *hull, margin float64) (hullManifold, bool) {
	n := len(c.Points)
	p0 := vec2{float64(c.Points[i].X), float64(c.Points[i].Y)}
	p1 := vec2{float64(c.Points[(i+1)%n].X), float64(c.Points[(i+1)%n].Y)}
	seg := segmentHull(p0, p1, 0)
	if seg.count != 2 || !seg.finite() || !o.finite() {
		return hullManifold{}, false
	}
	// Cheap rejection on bounds before the full test.
	sMinX, sMinY, sMaxX, sMaxY := seg.bounds()
	oMinX, oMinY, oMaxX, oMaxY := o.bounds()
	if sMaxX < oMinX || oMaxX < sMinX || sMaxY < oMinY || oMaxY < sMinY {
		return hullManifold{}, false
	}
	m := collideHullsWithin(&seg, o, margin)
	if m.count == 0 {
		return m, false
	}

	dir := vec2{p1.x - p0.x, p1.y - p0.y}
	length := detmath.Hypot(dir.x, dir.y)
	dir = vec2{dir.x / length, dir.y / length}
	nSeg := seg.n[0]
	mn := vec2{m.nx, m.ny}
	side, faceEdge := 1.0, 0
	mnDot := dot2(mn, nSeg)
	if math.Abs(mnDot) < 0.5 {
		var cx, cy float64
		for k := range o.count {
			cx += o.v[k].x
			cy += o.v[k].y
		}
		invC := 1.0 / float64(o.count)
		cx = float64(cx * invC)
		cy = float64(cy * invC)
		if dot2(vec2{cx - p0.x, cy - p0.y}, nSeg) < 0 {
			side, faceEdge = -1, 1
		}
	} else if mnDot < 0 {
		side, faceEdge = -1, 1
	}
	nF := vec2{nSeg.x * side, nSeg.y * side}
	if dot2(mn, nF) >= 1-chainNormalTolerance {
		return m, true
	}

	// The normal leans toward an end of the segment, so the contact involves that vertex.
	var vertex, ghost vec2
	hasGhost := false
	atEnd := dot2(dir, mn) > 0
	if atEnd {
		vertex = p1
		if c.Loop || i+2 < n {
			ghost = vec2{float64(c.Points[(i+2)%n].X), float64(c.Points[(i+2)%n].Y)}
			hasGhost = true
		}
	} else {
		vertex = p0
		if c.Loop || i > 0 {
			ghost = vec2{float64(c.Points[(i+n-1)%n].X), float64(c.Points[(i+n-1)%n].Y)}
			hasGhost = true
		}
	}
	if !hasGhost {
		// The open end of the chain behaves like a lone segment's end.
		return m, true
	}
	toGhost := vec2{ghost.x - vertex.x, ghost.y - vertex.y}
	gl := detmath.Hypot(toGhost.x, toGhost.y)
	if gl > 0 && dot2(nF, toGhost) < -1e-9*gl {
		// A convex vertex owns the normals between this face and the neighbour's; beyond them the neighbour's face owns the contact.
		t := vec2{toGhost.x / gl, toGhost.y / gl}
		if atEnd {
			if dot2(mn, t) <= chainNormalTolerance {
				return m, true
			}
		} else if dot2(mn, vec2{-t.x, -t.y}) >= -chainNormalTolerance {
			return m, true
		}
		return hullManifold{}, false
	}
	// A flat or concave vertex has no normals of its own: push along this segment's face, as the neighbour does.
	forced := clipHulls(&seg, faceEdge, o, false, margin)
	return forced, forced.count > 0
}

// chainManifolds appends to out the manifold of every segment of chain c touching hull o, with normals pointing from the chain toward o. Two segments that share a vertex both report a contact at that vertex when o rests on a peak or a flat seam; a point already reported by an earlier segment with the same normal is dropped, so a solver that applies every manifold pushes o out of the vertex once, not twice.
func chainManifolds(c *Chain, o *hull, out []hullManifold, margin float64) []hullManifold {
	if c.Empty() || !o.hasArea() {
		return out
	}
	start := len(out)
	for i := range c.SegmentCount() {
		m, ok := chainSegmentManifold(c, i, o, margin)
		if !ok {
			continue
		}
		m = withoutReportedPoints(m, out[start:])
		m.sub = i
		if m.count > 0 {
			out = append(out, m)
		}
	}
	return out
}

// chainDuplicateTolerance is how close two contact points and normals from neighbouring chain segments must be to count as the same contact.
const chainDuplicateTolerance = 1e-6

// withoutReportedPoints returns m without the points that a manifold in kept already reports with the same normal.
func withoutReportedPoints(m hullManifold, kept []hullManifold) hullManifold {
	for _, k := range kept {
		if math.Abs(k.nx-m.nx) > chainDuplicateTolerance || math.Abs(k.ny-m.ny) > chainDuplicateTolerance {
			continue
		}
		n := 0
		for _, p := range m.points[:m.count] {
			dup := false
			for _, q := range k.points[:k.count] {
				if math.Abs(p.x-q.x) <= chainDuplicateTolerance && math.Abs(p.y-q.y) <= chainDuplicateTolerance {
					dup = true
					break
				}
			}
			if !dup {
				m.points[n] = p
				n++
			}
		}
		m.count = n
		if n == 0 {
			break
		}
	}
	return m
}

// contactFromManifold turns a manifold whose normal points from A toward B into a Contact whose normal points from B toward A, at the deepest depth and the mean of the points.
func contactFromManifold(m hullManifold) Contact {
	if m.count == 0 {
		return Contact{}
	}
	depth := m.maxDepth()
	if depth <= 0 {
		return Contact{}
	}
	var px, py float64
	for i := range m.count {
		px += m.points[i].x
		py += m.points[i].y
	}
	px /= float64(m.count)
	py /= float64(m.count)
	return Contact{
		Hit:     true,
		NormalX: float32(-m.nx),
		NormalY: float32(-m.ny),
		Depth:   float32(depth),
		PointX:  float32(px),
		PointY:  float32(py),
	}
}

// testShapesConvex tests every pair TestShapes does not route to the box and circle tests.
func testShapesConvex(a, b Shape) Contact {
	if a.Empty() || b.Empty() {
		return Contact{}
	}
	if a.Kind == ShapeChain || b.Kind == ShapeChain {
		chain, other, chainIsA := a.Chain, b, true
		if b.Kind == ShapeChain {
			if a.Kind == ShapeChain {
				return Contact{}
			}
			chain, other, chainIsA = b.Chain, a, false
		}
		h, ok := shapeHull(other)
		if !ok {
			return Contact{}
		}
		var best hullManifold
		bestDepth := 0.0
		for i := range chain.SegmentCount() {
			m, ok := chainSegmentManifold(chain, i, &h, 0)
			if !ok {
				continue
			}
			if d := m.maxDepth(); d > bestDepth {
				best, bestDepth = m, d
			}
		}
		if !chainIsA {
			// The manifold normal points from the chain (B) toward A; contactFromManifold expects A toward B.
			best.nx, best.ny = -best.nx, -best.ny
		}
		return contactFromManifold(best)
	}
	ha, okA := shapeHull(a)
	hb, okB := shapeHull(b)
	if !okA || !okB {
		return Contact{}
	}
	m := collideHulls(&ha, &hb)
	return contactFromManifold(m)
}
