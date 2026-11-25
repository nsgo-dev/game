package physics

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/internal/detmath"
)

// Continuous collision. After positions are integrated, a dynamic body that is a Bullet, or any point of whose core moved farther than half its thickness this step, is swept from its start pose to its end pose against the static bodies its path crosses. Conservative advancement finds the time of impact with each: the body advances by the distance to the static shape divided by the fastest the two can approach, which never steps past the surface. The earliest impact moves the body back to that pose and removes its approach velocity at the contact point, with restitution as the contact solver applies it, so it stops at the first surface instead of passing through. A static surface the body rests on at the start of the step, one its manifolds name or one within speculativeDistance that the step does not drive it more than half its thickness into, never stops the sweep, and neither does a static piece lying wholly behind such a surface, so a body sliding fast over a tiled floor or a flat chain does not catch on the seams. A body driven deep into a static body it touches but has no contact with yet, such as a shot fired point-blank at a wall, stops where it is. An impact that would leave a body that is not a Bullet only shallowly inside the shape is left to the regular contacts, which resolve it without losing the step's motion. Fast dynamic bodies are not swept against each other.

const (
	// ccdTargetSeparation is the gap the sweep leaves between a body and the surface it hits.
	ccdTargetSeparation = 0.1
	// ccdTolerance is how far beyond ccdTargetSeparation conservative advancement may stop.
	ccdTolerance = 0.05
	// ccdMaxIterations bounds conservative advancement; it stops at a safe time of impact if it has not converged.
	ccdMaxIterations = 32
)

// sweepHull returns the rounded hull of a dynamic body's shape at pose (x, y, angle), with the radius of the smallest circle about (x, y) holding its core and its half thickness: the distance from (x, y) to its nearest face, plus the radius.
func (b *RigidBody) sweepHull(x, y, angle float64) (h hull, reach, half float64, ok bool) {
	switch b.Shape.Kind {
	case ShapeBox:
		if b.Shape.Box.Empty() {
			return hull{}, 0, 0, false
		}
		h = orientedBoxHull(x, y, float64(b.Shape.Box.W)*0.5, float64(b.Shape.Box.H)*0.5, angle)
	case ShapeCircle:
		if b.Shape.Circle.Empty() {
			return hull{}, 0, 0, false
		}
		h = pointHull(x, y, float64(b.Shape.Circle.R))
	case ShapePolygon, ShapeCapsule:
		if !b.hasLocal {
			return hull{}, 0, 0, false
		}
		h, ok = shapeHull(b.local.transformed(x, y, angle))
		if !ok {
			return hull{}, 0, 0, false
		}
	default:
		return hull{}, 0, 0, false
	}
	if !h.finite() {
		return hull{}, 0, 0, false
	}
	half = h.r
	if h.count >= 3 {
		face := math.Inf(1)
		for i := range h.count {
			face = min(face, dot2(h.n[i], vec2{h.v[i].x - x, h.v[i].y - y}))
		}
		half += max(0, face)
	}
	for _, v := range h.v[:h.count] {
		reach = max(reach, detmath.Hypot(v.x-x, v.y-y))
	}
	return h, reach, half, true
}

// hullSeparation returns the gap between rounded hulls a and b, the unit normal from a toward b and the closest core points, or false when their cores overlap.
func hullSeparation(a, b *hull) (sep float64, n, pa, pb vec2, ok bool) {
	if b.coreContains(a.v[0]) || a.coreContains(b.v[0]) {
		return 0, vec2{}, vec2{}, vec2{}, false
	}
	pa, pb, dist := closestCorePoints(a, b)
	if !(dist > 0) {
		return 0, vec2{}, vec2{}, vec2{}, false
	}
	n = vec2{(pb.x - pa.x) / dist, (pb.y - pa.y) / dist}
	return dist - a.r - b.r, n, pa, pb, true
}

// ccdHit is the earliest impact a sweep found.
type ccdHit struct {
	t      float64
	n      vec2
	point  vec2
	target *RigidBody
}

// ccdCandidate is one convex piece of a static body a sweep may hit: the whole body, or one segment of a chain.
type ccdCandidate struct {
	st *RigidBody
	h  hull
}

// ccdContact is a contact this step's manifolds give a dynamic body with a static one, with the unit normal from the dynamic body toward the static one.
type ccdContact struct {
	body, st *RigidBody
	sub      int
	n        vec2
}

// ccdSupport is a static surface a swept body rests on at the start of the step: the unit normal from the body toward it and the offset along that normal of the surface's nearest point. A static piece that lies wholly on the far side of a support, such as the next tile of a tiled floor or the next segment of a flat chain, cannot be reached without crossing the support, so the sweep ignores it instead of catching on the seam between the two.
type ccdSupport struct {
	n vec2
	d float64
}

// sweepFastBodies sweeps every awake dynamic body that needs it from its pose in starts to its integrated pose against the static bodies, and stops it at the first impact. manifolds are this step's contacts, which name the static surfaces each body rests on.
func (w *RigidWorld) sweepFastBodies(starts []rigidPose, manifolds []rigidManifold, restitutionThresh float64) {
	statics := w.scratch.statics[:0]
	for i, b := range w.Bodies {
		if b != nil && b.Static && !b.Shape.Empty() {
			statics = append(statics, i)
		}
	}
	w.scratch.statics = statics
	if len(statics) == 0 {
		return
	}
	contacts := w.scratch.ccdContacts[:0]
	for mi := range manifolds {
		m := &manifolds[mi]
		switch {
		case m.b.Static && m.a.awakeDynamic():
			contacts = append(contacts, ccdContact{body: m.a, st: m.b, sub: m.sub, n: vec2{m.nx, m.ny}})
		case m.a.Static && m.b.awakeDynamic():
			contacts = append(contacts, ccdContact{body: m.b, st: m.a, sub: m.sub, n: vec2{-m.nx, -m.ny}})
		}
	}
	w.scratch.ccdContacts = contacts
	for i, b := range w.Bodies {
		if !b.awakeDynamic() || b.Shape.Empty() {
			continue
		}
		s := starts[i]
		x0, y0, a0 := float64(s.x), float64(s.y), float64(s.angle)
		x1, y1, a1 := float64(b.X), float64(b.Y), float64(b.Angle)
		dx, dy, da := x1-x0, y1-y0, a1-a0
		_, reach, half, ok := b.sweepHull(x1, y1, a1)
		if !ok {
			continue
		}
		// The farthest any point of the core moves this step: the translation plus the turn at the core's reach.
		dist := detmath.Hypot(dx, dy) + float64(math.Abs(da)*reach)
		if !b.Bullet && !(dist > half) {
			continue
		}
		if math.IsNaN(dist) || math.IsInf(dist, 0) || math.IsNaN(da) || math.IsInf(da, 0) {
			continue
		}
		startHull, _, _, ok := b.sweepHull(x0, y0, a0)
		if !ok {
			continue
		}
		// The swept box holds the body's bounding circle along its whole path.
		bound := reach + float64(b.boundingRadius())
		minX, maxX := min(x0, x1)-bound, max(x0, x1)+bound
		minY, maxY := min(y0, y1)-bound, max(y0, y1)+bound
		candidates := w.scratch.ccdCandidates[:0]
		for _, si := range statics {
			st := w.Bodies[si]
			if !CanCollideRigid(b, st) {
				continue
			}
			sMinX, sMinY, sMaxX, sMaxY := st.worldAABB()
			if sMaxX < minX || maxX < sMinX || sMaxY < minY || maxY < sMinY {
				continue
			}
			candidates = appendStaticPieces(st, minX, minY, maxX, maxY, candidates)
		}
		w.scratch.ccdCandidates = candidates
		if len(candidates) == 0 {
			continue
		}
		// A support is a static surface the body touches at the start of the step and does not drive more than half its thickness into: one its manifolds name, or one within speculativeDistance of it.
		supports := w.scratch.ccdSupports[:0]
		shallow := func(n vec2, sep float64) bool {
			return float64(dx*n.x)+float64(dy*n.y)-max(0, sep) <= 0.5*half
		}
		for _, c := range contacts {
			if c.body != b || !shallow(c.n, 0) {
				continue
			}
			if h, ok := staticPiece(c.st, c.sub); ok {
				supports = append(supports, ccdSupport{n: c.n, d: nearOffset(&h, c.n)})
			}
		}
		for ci := range candidates {
			c := &candidates[ci]
			sep, n, _, _, ok := hullSeparation(&startHull, &c.h)
			if ok && sep <= speculativeDistance && shallow(n, sep) {
				supports = append(supports, ccdSupport{n: n, d: nearOffset(&c.h, n)})
			}
		}
		w.scratch.ccdSupports = supports
		best := ccdHit{t: math.Inf(1)}
		for ci := range candidates {
			c := &candidates[ci]
			if behindSupport(&c.h, supports) {
				continue
			}
			sweepHullAgainst(b, &c.h, c.st, x0, y0, a0, dx, dy, da, reach, half, &best)
		}
		if best.target == nil {
			continue
		}
		b.X = float32(x0 + float64(dx*best.t))
		b.Y = float32(y0 + float64(dy*best.t))
		b.Angle = float32(a0 + float64(da*best.t))
		b.resolveImpact(best, restitutionThresh)
	}
}

// boundingRadius returns the rounding radius of b's shape, which sweepHull's reach leaves out.
func (b *RigidBody) boundingRadius() float32 {
	switch b.Shape.Kind {
	case ShapeCircle:
		return b.Shape.Circle.R
	case ShapePolygon:
		return b.Shape.Polygon.Radius
	case ShapeCapsule:
		return b.Shape.Capsule.R
	}
	return 0
}

// ccdSupportSlop is how far a static piece may reach toward the body past a support's surface and still count as lying behind it, which absorbs rounding between tiles meant to be flush.
const ccdSupportSlop = 0.5

// nearOffset returns the offset along unit normal n of the point of rounded hull h that reaches farthest against n.
func nearOffset(h *hull, n vec2) float64 {
	d := math.Inf(1)
	for _, v := range h.v[:h.count] {
		d = min(d, dot2(n, v))
	}
	return d - h.r
}

// behindSupport reports whether all of rounded hull h lies on the far side of one of supports.
func behindSupport(h *hull, supports []ccdSupport) bool {
	for _, s := range supports {
		if nearOffset(h, s.n) >= s.d-ccdSupportSlop {
			return true
		}
	}
	return false
}

// appendStaticPieces appends to dst the convex pieces of static body st that overlap the box (minX, minY)-(maxX, maxY): each segment of a chain, or the body's whole hull.
func appendStaticPieces(st *RigidBody, minX, minY, maxX, maxY float64, dst []ccdCandidate) []ccdCandidate {
	if st.Shape.Kind != ShapeChain {
		if h, ok := staticPiece(st, 0); ok {
			dst = append(dst, ccdCandidate{st: st, h: h})
		}
		return dst
	}
	if !st.hasLocal {
		return dst
	}
	s, _ := st.poseShape()
	c := s.Chain
	if c.Empty() {
		return dst
	}
	n := len(c.Points)
	for i := range c.SegmentCount() {
		p0, p1 := c.Points[i], c.Points[(i+1)%n]
		if float64(max(p0.X, p1.X)) < minX || maxX < float64(min(p0.X, p1.X)) || float64(max(p0.Y, p1.Y)) < minY || maxY < float64(min(p0.Y, p1.Y)) {
			continue
		}
		seg := segmentHull(vec2{float64(p0.X), float64(p0.Y)}, vec2{float64(p1.X), float64(p1.Y)}, 0)
		if seg.finite() {
			dst = append(dst, ccdCandidate{st: st, h: seg})
		}
	}
	return dst
}

// staticPiece returns piece sub of static body st: segment sub of a chain, or the body's whole hull.
func staticPiece(st *RigidBody, sub int) (hull, bool) {
	if st.Shape.Kind != ShapeChain {
		h, ok := st.rigidHull()
		return h, ok && h.finite()
	}
	if !st.hasLocal {
		return hull{}, false
	}
	s, _ := st.poseShape()
	c := s.Chain
	if c.Empty() || sub < 0 || sub >= c.SegmentCount() {
		return hull{}, false
	}
	p0, p1 := c.Points[sub], c.Points[(sub+1)%len(c.Points)]
	h := segmentHull(vec2{float64(p0.X), float64(p0.Y)}, vec2{float64(p1.X), float64(p1.Y)}, 0)
	return h, h.finite()
}

// sweepHullAgainst runs conservative advancement of b along its motion against target and records an impact earlier than best. The time advances by the gap over the fastest approach along the normal, the translation's component plus the most rotation can bring a core point closer, so it never passes the surface.
func sweepHullAgainst(b *RigidBody, target *hull, st *RigidBody, x0, y0, a0, dx, dy, da, reach, half float64, best *ccdHit) {
	spin := math.Abs(da) * reach
	t := 0.0
	for iter := range ccdMaxIterations {
		h, _, _, ok := b.sweepHull(x0+float64(dx*t), y0+float64(dy*t), a0+float64(da*t))
		if !ok {
			return
		}
		sep, n, pa, _, ok := hullSeparation(&h, target)
		if !ok {
			// Cores overlapping: the regular contacts own this pair.
			return
		}
		if iter == 0 && sep <= ccdTargetSeparation {
			// Touching at the start: a body that would go no more than half its thickness into the target is left to the regular contacts, and one driven deeper, like a shot fired point-blank at a wall, stops where it is.
			if float64(dx*n.x)+float64(dy*n.y)-max(0, sep) <= 0.5*half {
				return
			}
			if t < best.t {
				*best = ccdHit{t: t, n: n, point: vec2{pa.x + float64(n.x*h.r), pa.y + float64(n.y*h.r)}, target: st}
			}
			return
		}
		approach := float64(dx*n.x) + float64(dy*n.y) + spin
		if sep <= ccdTargetSeparation+ccdTolerance || iter == ccdMaxIterations-1 {
			// Left to run, the body would end this far inside the target; the regular contacts resolve a shallow overlap that keeps the body's center on its side. A Bullet stops at the surface regardless.
			if !b.Bullet && float64((1-t)*approach)-sep <= 0.5*half {
				return
			}
			if t < best.t {
				*best = ccdHit{t: t, n: n, point: vec2{pa.x + float64(n.x*h.r), pa.y + float64(n.y*h.r)}, target: st}
			}
			return
		}
		if approach <= 0 {
			return
		}
		t += (sep - ccdTargetSeparation) / approach
		if t >= 1 || t >= best.t {
			return
		}
	}
}

// resolveImpact removes b's velocity into the surface it hit, at the contact point, bouncing it by the pair's restitution when it struck faster than restitutionThresh.
func (b *RigidBody) resolveImpact(hit ccdHit, restitutionThresh float64) {
	n := hit.n
	rx := hit.point.x - float64(b.X)
	ry := hit.point.y - float64(b.Y)
	vx := float64(b.VX) - float64(float64(b.AngularVel)*ry)
	vy := float64(b.VY) + float64(float64(b.AngularVel)*rx)
	vn := float64(vx*n.x) + float64(vy*n.y)
	if !(vn > 0) {
		return
	}
	e := max(0, max(float64(b.Restitution), float64(hit.target.Restitution)))
	if vn <= restitutionThresh {
		e = 0
	}
	rn := float64(rx*n.y) - float64(ry*n.x)
	k := float64(b.InvMass) + float64(float64(float64(b.InvInertia)*rn)*rn)
	if !(k > 0) {
		return
	}
	j := float64((1+e)*vn) / k
	b.applyImpulseRaw(-float64(j*n.x), -float64(j*n.y), rx, ry)
}
