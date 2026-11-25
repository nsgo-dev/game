package physics3d

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/internal/detmath"
)

// Convex narrow phase (#808). Pairs that involve a ConvexHull or a Cylinder are tested with GJK, which finds the distance between two convex shapes or reports that they overlap, and EPA, which measures how deep an overlap is and along which direction. Spheres and capsules take part as a point or a segment plus a radius (their margin), so GJK alone resolves every shallow contact against them and EPA only runs when the cores themselves overlap. A contact's point is the centre of the patch where the two shapes' supporting features overlap, found by clipping one feature against the other, so a hull resting on a face or a cylinder standing on its end gets a point under its middle rather than at one corner. Everything runs in float64 in a fixed order with index tie-breaks, so the same inputs always give the same contact.

// dv is a float64 vector used inside the convex narrow phase.
type dv struct{ x, y, z float64 }

func toDV(v Vec3) dv { return dv{float64(v.X), float64(v.Y), float64(v.Z)} }

func (a dv) vec3() Vec3 { return Vec3{X: float32(a.x), Y: float32(a.y), Z: float32(a.z)} }

func (a dv) add(b dv) dv        { return dv{a.x + b.x, a.y + b.y, a.z + b.z} }
func (a dv) sub(b dv) dv        { return dv{a.x - b.x, a.y - b.y, a.z - b.z} }
func (a dv) scale(s float64) dv { return dv{float64(a.x * s), float64(a.y * s), float64(a.z * s)} }
func (a dv) neg() dv            { return dv{-a.x, -a.y, -a.z} }
func (a dv) dot(b dv) float64   { return float64(a.x*b.x) + float64(a.y*b.y) + float64(a.z*b.z) }
func (a dv) lenSq() float64     { return a.dot(a) }
func (a dv) length() float64    { return math.Sqrt(a.dot(a)) }
func (a dv) cross(b dv) dv {
	return dv{float64(a.y*b.z) - float64(a.z*b.y), float64(a.z*b.x) - float64(a.x*b.z), float64(a.x*b.y) - float64(a.y*b.x)}
}
func (a dv) lerp(b dv, t float64) dv { return a.add(b.sub(a).scale(t)) }

func (a dv) norm() dv {
	l := a.length()
	if l <= 1e-300 {
		return dv{}
	}
	return a.scale(1 / l)
}

// rotAxes returns the world-space images of the unit X, Y and Z axes under q.
func rotAxes(q Quat) [3]dv {
	q = normalizedQuat(q)
	x, y, z, w := float64(q.X), float64(q.Y), float64(q.Z), float64(q.W)
	return [3]dv{
		{1 - 2*(float64(y*y)+float64(z*z)), 2 * (float64(x*y) + float64(w*z)), 2 * (float64(x*z) - float64(w*y))},
		{2 * (float64(x*y) - float64(w*z)), 1 - 2*(float64(x*x)+float64(z*z)), 2 * (float64(y*z) + float64(w*x))},
		{2 * (float64(x*z) + float64(w*y)), 2 * (float64(y*z) - float64(w*x)), 1 - 2*(float64(x*x)+float64(y*y))},
	}
}

// cvxKind is the core geometry of a cvx.
type cvxKind uint8

const (
	cvxPoint cvxKind = iota
	cvxSegment
	cvxTriangle
	cvxBox
	cvxHull
	cvxCylinder
)

// cvx is a convex shape in world space as the convex narrow phase sees it: a core plus a margin (the radius of a sphere or capsule, 0 otherwise).
type cvx struct {
	kind   cvxKind
	margin float64
	// p holds a point's position, a segment's ends or a triangle's corners.
	p [3]dv
	// center is a box's, hull's or cylinder's frame origin; axes are its rotated X, Y and Z axes.
	center dv
	axes   [3]dv
	ext    dv
	hull   *hullGeom
	// axis is a cylinder's unit axis from its A cap to its B cap; half is its half height and radius its radius.
	axis   dv
	half   float64
	radius float64
}

// toCvx converts a shape to its convex narrow-phase form.
func toCvx(s Shape) cvx {
	switch s.Kind {
	case ShapeSphere:
		return cvx{kind: cvxPoint, p: [3]dv{toDV(s.Sphere.Center)}, margin: float64(max(s.Sphere.Radius, 0))}
	case ShapeCapsule:
		return cvx{kind: cvxSegment, p: [3]dv{toDV(s.Capsule.A), toDV(s.Capsule.B)}, margin: float64(max(s.Capsule.Radius, 0))}
	case ShapeBox:
		return cvx{kind: cvxBox, center: toDV(s.Box.Center), axes: rotAxes(s.Rot), ext: toDV(s.Box.HalfExtents)}
	case ShapeConvexHull:
		return cvx{kind: cvxHull, center: toDV(s.Hull.Center), axes: rotAxes(s.Rot), hull: s.Hull.geom}
	case ShapeCylinder:
		a, b := toDV(s.Cylinder.A), toDV(s.Cylinder.B)
		d := b.sub(a)
		l := d.length()
		axis, half := dv{0, 1, 0}, 0.0
		if l > 1e-12 {
			axis, half = d.scale(1/l), l/2
		}
		return cvx{kind: cvxCylinder, center: a.lerp(b, 0.5), axis: axis, half: half, radius: float64(max(s.Cylinder.Radius, 0))}
	default:
		return cvx{kind: cvxPoint}
	}
}

func triCvx(t Triangle) cvx {
	return cvx{kind: cvxTriangle, p: [3]dv{toDV(t.A), toDV(t.B), toDV(t.C)}}
}

// valid reports whether c has a core GJK can use.
func (c *cvx) valid() bool {
	switch c.kind {
	case cvxHull:
		return c.hull != nil && len(c.hull.v) > 0
	case cvxCylinder:
		// A cylinder with no length or no radius has no volume and, like a raycast or an area, collisions ignore it.
		return c.radius > 0 && c.half > 0
	default:
		return true
	}
}

// local returns d expressed in a box's or hull's own axes.
func (c *cvx) local(d dv) dv {
	return dv{d.dot(c.axes[0]), d.dot(c.axes[1]), d.dot(c.axes[2])}
}

// world maps a point in a box's or hull's own axes to world space.
func (c *cvx) world(p dv) dv {
	return c.center.add(c.axes[0].scale(p.x)).add(c.axes[1].scale(p.y)).add(c.axes[2].scale(p.z))
}

// support returns the point of c's core furthest along d, picking the lowest index on ties.
func (c *cvx) support(d dv) dv {
	switch c.kind {
	case cvxPoint:
		return c.p[0]
	case cvxSegment:
		if c.p[1].dot(d) > c.p[0].dot(d) {
			return c.p[1]
		}
		return c.p[0]
	case cvxTriangle:
		best, bd := 0, c.p[0].dot(d)
		for i := 1; i < 3; i++ {
			if v := c.p[i].dot(d); v > bd {
				best, bd = i, v
			}
		}
		return c.p[best]
	case cvxBox:
		l := c.local(d)
		p := dv{signNZ(l.x) * c.ext.x, signNZ(l.y) * c.ext.y, signNZ(l.z) * c.ext.z}
		return c.world(p)
	case cvxHull:
		return c.world(c.hull.v[c.hull.supportIndex(c.local(d))])
	case cvxCylinder:
		du := d.dot(c.axis)
		p := c.center.add(c.axis.scale(signNZ(du) * c.half))
		radial := d.sub(c.axis.scale(du))
		if rl := radial.length(); rl > 1e-12*(1+d.length()) {
			p = p.add(radial.scale(c.radius / rl))
		}
		return p
	}
	return dv{}
}

// centroid returns a point inside c, used to orient normals when nothing better is known.
func (c *cvx) centroid() dv {
	switch c.kind {
	case cvxPoint:
		return c.p[0]
	case cvxSegment:
		return c.p[0].lerp(c.p[1], 0.5)
	case cvxTriangle:
		return c.p[0].add(c.p[1]).add(c.p[2]).scale(1.0 / 3)
	case cvxHull:
		return c.world(c.hull.centroid)
	default:
		return c.center
	}
}

// extent returns a length on the scale of c, used to scale tolerances.
func (c *cvx) extent() float64 {
	var e float64
	switch c.kind {
	case cvxSegment:
		e = c.p[1].sub(c.p[0]).length()
	case cvxTriangle:
		e = max(c.p[1].sub(c.p[0]).length(), c.p[2].sub(c.p[0]).length())
	case cvxBox:
		e = max(c.ext.x, c.ext.y, c.ext.z)
	case cvxHull:
		e = c.hull.radius
	case cvxCylinder:
		e = max(c.half, c.radius)
	}
	return e + c.margin
}

// cylinderDiscSides is how many points stand for a cylinder's cap when it is a contact feature.
const cylinderDiscSides = 16

// feature appends to dst the face, edge or vertex of c's surface (margin included) that lies furthest along the unit direction n, as a convex polygon in order, a segment, or a point.
func (c *cvx) feature(dst []dv, n dv) []dv {
	off := n.scale(c.margin)
	switch c.kind {
	case cvxPoint:
		return append(dst, c.p[0].add(off))
	case cvxSegment:
		d := c.p[1].sub(c.p[0])
		if l := d.length(); l > 1e-12 && math.Abs(d.dot(n))/l < 0.05 {
			return append(dst, c.p[0].add(off), c.p[1].add(off))
		}
		return append(dst, c.support(n).add(off))
	case cvxTriangle:
		return append(dst, c.p[0], c.p[1], c.p[2])
	case cvxBox:
		l := c.local(n)
		ax, s := 0, signNZ(l.x)
		best := math.Abs(l.x)
		if v := math.Abs(l.y); v > best {
			ax, s, best = 1, signNZ(l.y), v
		}
		if v := math.Abs(l.z); v > best {
			ax, s = 2, signNZ(l.z)
		}
		e := [3]float64{c.ext.x, c.ext.y, c.ext.z}
		u, w := (ax+1)%3, (ax+2)%3
		quad := [4][2]float64{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}}
		for _, q := range quad {
			var p [3]float64
			p[ax] = s * e[ax]
			p[u] = q[0] * e[u]
			p[w] = q[1] * e[w]
			dst = append(dst, c.world(dv{p[0], p[1], p[2]}))
		}
		return dst
	case cvxHull:
		f := &c.hull.faces[c.hull.supportFace(c.local(n))]
		for _, i := range f.idx {
			dst = append(dst, c.world(c.hull.v[i]))
		}
		return dst
	case cvxCylinder:
		du := n.dot(c.axis)
		radial := n.sub(c.axis.scale(du))
		if math.Abs(du) < 0.05 && radial.lenSq() > 1e-12 {
			r := radial.norm().scale(c.radius)
			cap := c.axis.scale(c.half)
			return append(dst, c.center.sub(cap).add(r), c.center.add(cap).add(r))
		}
		capC := c.center.add(c.axis.scale(signNZ(du) * c.half))
		u, v := perpBasis(c.axis)
		for i := range cylinderDiscSides {
			s, co := detmath.Sincos(2 * math.Pi * float64(i) / cylinderDiscSides)
			dst = append(dst, capC.add(u.scale(co*c.radius)).add(v.scale(s*c.radius)))
		}
		return dst
	}
	return dst
}

// perpBasis returns two unit vectors perpendicular to the unit vector a and to each other.
func perpBasis(a dv) (dv, dv) {
	ref := dv{0, 1, 0}
	if math.Abs(a.y) >= 0.9 {
		ref = dv{0, 0, 1}
	}
	u := a.cross(ref).norm()
	return u, u.cross(a)
}

func signNZ(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

// simplexVert is one vertex of a GJK or EPA simplex: w = a - b for support points a of shape A and b of shape B.
type simplexVert struct{ w, a, b dv }

func minkowski(a, b *cvx, d dv) simplexVert {
	pa := a.support(d)
	pb := b.support(d.neg())
	return simplexVert{w: pa.sub(pb), a: pa, b: pb}
}

// simplex is GJK's working simplex with the barycentric weights of its point closest to the origin.
type simplex struct {
	v  [4]simplexVert
	bc [4]float64
	n  int
}

// closest reduces s to the smallest sub-simplex holding its point closest to the origin, stores that point's barycentric weights, and returns it. It reports false when the origin lies inside a full tetrahedron.
func (s *simplex) closest() (dv, bool) {
	switch s.n {
	case 1:
		s.bc[0] = 1
		return s.v[0].w, true
	case 2:
		a, b := s.v[0].w, s.v[1].w
		ab := b.sub(a)
		den := ab.lenSq()
		t := 0.0
		if den > 0 {
			t = math.Max(0, math.Min(1, -a.dot(ab)/den))
		}
		switch t {
		case 0:
			s.n, s.bc[0] = 1, 1
			return a, true
		case 1:
			s.v[0], s.n, s.bc[0] = s.v[1], 1, 1
			return b, true
		}
		s.bc[0], s.bc[1] = 1-t, t
		return a.lerp(b, t), true
	case 3:
		return s.closestTriangle(0, 1, 2)
	case 4:
		return s.closestTetra()
	}
	return dv{}, true
}

// closestTriangle finds the point of triangle (s.v[i], s.v[j], s.v[k]) closest to the origin (Ericson, Real-Time Collision Detection 5.1.5) and reduces s to the feature it lies on.
func (s *simplex) closestTriangle(i, j, k int) (dv, bool) {
	va, vb, vc := s.v[i], s.v[j], s.v[k]
	a, b, c := va.w, vb.w, vc.w
	ab, ac := b.sub(a), c.sub(a)
	d1, d2 := ab.dot(a.neg()), ac.dot(a.neg())
	set := func(vs ...simplexVert) {
		copy(s.v[:], vs)
		s.n = len(vs)
	}
	if d1 <= 0 && d2 <= 0 {
		set(va)
		s.bc[0] = 1
		return a, true
	}
	d3, d4 := ab.dot(b.neg()), ac.dot(b.neg())
	if d3 >= 0 && d4 <= d3 {
		set(vb)
		s.bc[0] = 1
		return b, true
	}
	vcc := float64(d1*d4) - float64(d3*d2)
	if vcc <= 0 && d1 >= 0 && d3 <= 0 {
		t := d1 / (d1 - d3)
		set(va, vb)
		s.bc[0], s.bc[1] = 1-t, t
		return a.add(ab.scale(t)), true
	}
	d5, d6 := ab.dot(c.neg()), ac.dot(c.neg())
	if d6 >= 0 && d5 <= d6 {
		set(vc)
		s.bc[0] = 1
		return c, true
	}
	vbb := float64(d5*d2) - float64(d1*d6)
	if vbb <= 0 && d2 >= 0 && d6 <= 0 {
		t := d2 / (d2 - d6)
		set(va, vc)
		s.bc[0], s.bc[1] = 1-t, t
		return a.add(ac.scale(t)), true
	}
	vaa := float64(d3*d6) - float64(d5*d4)
	if vaa <= 0 && (d4-d3) >= 0 && (d5-d6) >= 0 {
		t := (d4 - d3) / ((d4 - d3) + (d5 - d6))
		set(vb, vc)
		s.bc[0], s.bc[1] = 1-t, t
		return b.add(c.sub(b).scale(t)), true
	}
	den := vaa + vbb + vcc
	if den == 0 {
		set(va)
		s.bc[0] = 1
		return a, true
	}
	v, w := vbb/den, vcc/den
	set(va, vb, vc)
	s.bc[0], s.bc[1], s.bc[2] = 1-v-w, v, w
	return a.add(ab.scale(v)).add(ac.scale(w)), true
}

// closestTetra handles a four-point simplex: the origin is either inside it or closest to one of the faces it lies outside.
func (s *simplex) closestTetra() (dv, bool) {
	faces := [4][4]int{{0, 1, 2, 3}, {0, 3, 1, 2}, {0, 2, 3, 1}, {1, 3, 2, 0}}
	orig := *s
	best := math.Inf(1)
	var bestS simplex
	var bestP dv
	outside := false
	for _, f := range faces {
		a, b, c, d := orig.v[f[0]].w, orig.v[f[1]].w, orig.v[f[2]].w, orig.v[f[3]].w
		n := b.sub(a).cross(c.sub(a))
		sd := n.dot(d.sub(a))
		so := n.dot(a.neg())
		// The origin is outside a face when it lies on the other side of it from the fourth vertex; a flat tetrahedron encloses nothing, so every face is tried.
		if sd != 0 && so*sd >= 0 {
			continue
		}
		outside = true
		t := orig
		p, _ := t.closestTriangle(f[0], f[1], f[2])
		if ds := p.lenSq(); ds < best {
			best, bestS, bestP = ds, t, p
		}
	}
	if !outside {
		return dv{}, false
	}
	*s = bestS
	return bestP, true
}

// witness returns the points of A and B that the simplex's closest point is made of.
func (s *simplex) witness() (dv, dv) {
	var pa, pb dv
	for i := range s.n {
		pa = pa.add(s.v[i].a.scale(s.bc[i]))
		pb = pb.add(s.v[i].b.scale(s.bc[i]))
	}
	return pa, pb
}

const gjkMaxIterations = 64

// gjk runs GJK on the cores of a and b. It returns the closest points of the two cores when they are apart, or overlap true with the final simplex when they intersect (or touch within tol).
func gjk(a, b *cvx, tol float64) (s simplex, pa, pb dv, overlap bool) {
	d := a.centroid().sub(b.centroid())
	if d.lenSq() <= tol*tol {
		d = dv{1, 0, 0}
	}
	s.v[0] = minkowski(a, b, d.neg())
	s.n = 1
	s.bc[0] = 1
	v := s.v[0].w
	for range gjkMaxIterations {
		vv := v.lenSq()
		if vv <= tol*tol {
			return s, dv{}, dv{}, true
		}
		w := minkowski(a, b, v.neg())
		if vv-v.dot(w.w) <= float64(1e-10*vv)+float64(tol*tol) {
			break
		}
		dup := false
		for i := range s.n {
			if s.v[i].w == w.w {
				dup = true
			}
		}
		if dup {
			break
		}
		s.v[s.n] = w
		s.n++
		nv, ok := s.closest()
		if !ok {
			return s, dv{}, dv{}, true
		}
		v = nv
		if nv.lenSq() >= vv*(1-1e-12) {
			break
		}
	}
	pa, pb = s.witness()
	return s, pa, pb, false
}

const (
	epaMaxVerts = 96
	epaMaxFaces = 2 * epaMaxVerts
)

type epaFace struct {
	i     [3]int
	n     dv
	d     float64
	alive bool
}

// epaResult is the penetration EPA measured: moving A by -n*depth separates the cores; pa and pb are the deepest points of each.
type epaResult struct {
	n      dv
	depth  float64
	pa, pb dv
}

// epa expands GJK's overlapping simplex into the Minkowski difference of the cores of a and b until it finds the face closest to the origin.
func epa(a, b *cvx, s simplex, tol float64) (epaResult, bool) {
	var verts [epaMaxVerts]simplexVert
	nv := copy(verts[:], s.v[:s.n])
	if !epaBlowUp(a, b, &verts, &nv, tol) {
		return epaResult{}, false
	}
	var faces [epaMaxFaces]epaFace
	nf := 0
	inner := verts[0].w.add(verts[1].w).add(verts[2].w).add(verts[3].w).scale(0.25)
	mk := func(i, j, k int) bool {
		if nf == epaMaxFaces {
			return false
		}
		va, vb, vc := verts[i].w, verts[j].w, verts[k].w
		n := vb.sub(va).cross(vc.sub(va))
		l := n.length()
		if l <= 1e-300 {
			return false
		}
		n = n.scale(1 / l)
		faces[nf] = epaFace{i: [3]int{i, j, k}, n: n, d: n.dot(va), alive: true}
		nf++
		return true
	}
	for _, f := range [4][3]int{{0, 1, 2}, {0, 3, 1}, {0, 2, 3}, {1, 3, 2}} {
		i, j, k := f[0], f[1], f[2]
		n := verts[j].w.sub(verts[i].w).cross(verts[k].w.sub(verts[i].w))
		if n.dot(verts[i].w.sub(inner)) < 0 {
			j, k = k, j
		}
		if !mk(i, j, k) {
			return epaResult{}, false
		}
	}

	best := 0
	for range 3 * epaMaxVerts {
		best = -1
		for i := range nf {
			if faces[i].alive && (best < 0 || faces[i].d < faces[best].d) {
				best = i
			}
		}
		if best < 0 {
			return epaResult{}, false
		}
		f := faces[best]
		w := minkowski(a, b, f.n)
		if w.w.dot(f.n)-f.d <= tol || nv == epaMaxVerts {
			break
		}
		verts[nv] = w
		nv++
		// Remove every face that sees the new point and stitch its horizon to it.
		var horizon [epaMaxFaces][2]int
		nh := 0
		for i := range nf {
			if !faces[i].alive || faces[i].n.dot(w.w.sub(verts[faces[i].i[0]].w)) <= tol*1e-3 {
				continue
			}
			faces[i].alive = false
			for e := range 3 {
				p, q := faces[i].i[e], faces[i].i[(e+1)%3]
				// An edge shared with another removed face appears in reverse; both copies cancel.
				found := -1
				for h := range nh {
					if horizon[h] == [2]int{q, p} {
						found = h
						break
					}
				}
				if found >= 0 {
					nh--
					horizon[found] = horizon[nh]
					continue
				}
				if nh == len(horizon) {
					return epaFinish(faces[best], verts[:nv])
				}
				horizon[nh] = [2]int{p, q}
				nh++
			}
		}
		if faces[best].alive {
			// Numerically the chosen face did not see its own support point; stop with it.
			break
		}
		for h := range nh {
			if !mk(horizon[h][0], horizon[h][1], nv-1) {
				return epaFinish(f, verts[:nv])
			}
		}
		best = -1
	}
	if best < 0 {
		for i := range nf {
			if faces[i].alive && (best < 0 || faces[i].d < faces[best].d) {
				best = i
			}
		}
		if best < 0 {
			return epaResult{}, false
		}
	}
	return epaFinish(faces[best], verts[:nv])
}

// epaFinish turns the closest face into a penetration result.
func epaFinish(f epaFace, verts []simplexVert) (epaResult, bool) {
	va, vb, vc := verts[f.i[0]], verts[f.i[1]], verts[f.i[2]]
	d := math.Max(f.d, 0)
	u, v, w := barycentric(f.n.scale(d), va.w, vb.w, vc.w)
	pa := va.a.scale(u).add(vb.a.scale(v)).add(vc.a.scale(w))
	pb := va.b.scale(u).add(vb.b.scale(v)).add(vc.b.scale(w))
	return epaResult{n: f.n, depth: d, pa: pa, pb: pb}, true
}

// barycentric returns the barycentric weights of p (assumed in the plane) in triangle (a, b, c), clamped to the triangle.
func barycentric(p, a, b, c dv) (float64, float64, float64) {
	v0, v1, v2 := b.sub(a), c.sub(a), p.sub(a)
	d00, d01, d11 := v0.dot(v0), v0.dot(v1), v1.dot(v1)
	d20, d21 := v2.dot(v0), v2.dot(v1)
	den := float64(d00*d11) - float64(d01*d01)
	if den <= 1e-300 {
		return 1, 0, 0
	}
	v := (float64(d11*d20) - float64(d01*d21)) / den
	w := (float64(d00*d21) - float64(d01*d20)) / den
	v, w = math.Max(v, 0), math.Max(w, 0)
	if s := v + w; s > 1 {
		v, w = v/s, w/s
	}
	return 1 - v - w, v, w
}

// epaBlowUp grows a GJK simplex that ended with fewer than four points (the origin on a face, edge or vertex of it) into a tetrahedron of support points.
func epaBlowUp(a, b *cvx, verts *[epaMaxVerts]simplexVert, nv *int, tol float64) bool {
	axes := [6]dv{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
	for *nv < 4 {
		var dirs [6]dv
		nd := 0
		switch *nv {
		case 1:
			dirs = axes
			nd = 6
		case 2:
			d := verts[1].w.sub(verts[0].w)
			for _, ax := range axes[0:6:6] {
				if c := d.cross(ax); c.lenSq() > 1e-12*d.lenSq() && nd < 6 {
					dirs[nd] = c.norm()
					nd++
				}
			}
		case 3:
			n := verts[1].w.sub(verts[0].w).cross(verts[2].w.sub(verts[0].w)).norm()
			if n.lenSq() == 0 {
				return false
			}
			dirs[0], dirs[1] = n, n.neg()
			nd = 2
		}
		bestD := tol
		var best simplexVert
		found := false
		for _, d := range dirs[:nd] {
			w := minkowski(a, b, d)
			var dist float64
			switch *nv {
			case 1:
				dist = w.w.sub(verts[0].w).length()
			case 2:
				p0, p1 := verts[0].w, verts[1].w
				e := p1.sub(p0)
				dist = e.cross(w.w.sub(p0)).length() / math.Max(e.length(), 1e-300)
			case 3:
				n := verts[1].w.sub(verts[0].w).cross(verts[2].w.sub(verts[0].w)).norm()
				dist = math.Abs(n.dot(w.w.sub(verts[0].w)))
			}
			if dist > bestD {
				bestD, best, found = dist, w, true
			}
		}
		if !found {
			return false
		}
		verts[*nv] = best
		*nv++
	}
	return true
}

// convexTol returns the length tolerance used for a pair of shapes.
func convexTol(a, b *cvx) float64 {
	return 1e-6 * (1 + a.extent() + b.extent())
}

// testConvex tests two convex shapes for overlap. The normal points from b toward a.
func testConvex(a, b cvx) Contact {
	if !a.valid() || !b.valid() {
		return Contact{}
	}
	tol := convexTol(&a, &b)
	margin := a.margin + b.margin
	s, pa, pb, overlap := gjk(&a, &b, tol)
	var n dv
	var depth float64
	if !overlap {
		d := pa.sub(pb)
		dist := d.length()
		if dist >= margin {
			return Contact{}
		}
		if dist > tol {
			n = d.scale(1 / dist)
			depth = margin - dist
			pa, pb = pa.sub(n.scale(a.margin)), pb.add(n.scale(b.margin))
		} else {
			overlap = true
		}
	}
	if overlap {
		r, ok := epa(&a, &b, s, tol*10)
		if !ok {
			// The cores only touch, with nothing to expand: push apart along the line between their centroids.
			n = a.centroid().sub(b.centroid()).norm()
			if n.lenSq() == 0 {
				n = dv{0, 1, 0}
			}
			depth = margin
			pa, pb = a.support(n.neg()), b.support(n)
		} else {
			n = r.n.neg()
			depth = r.depth + margin
			pa, pb = r.pa.sub(n.scale(a.margin)), r.pb.add(n.scale(b.margin))
		}
	}
	if !(depth > 0) {
		return Contact{}
	}
	point := contactPatch(&a, &b, n, pa.lerp(pb, 0.5))
	return Contact{Hit: true, Normal: n.vec3().Norm(), Depth: float32(depth), Point: point.vec3()}
}

// testConvexTriangle tests a convex shape against a triangle; the normal points from the triangle toward the shape. A triangle is usually one of many in a mesh or a heightfield, whose shared edges are not real edges of the surface, so whenever part of the shape's face toward the triangle reaches through the triangle's plane inside its outline, the contact uses the face normal, the deepest such point and the middle of that part. Only a shape that meets the triangle at its outline alone gets the GJK and EPA contact, which may be along an edge.
func testConvexTriangle(s cvx, tri Triangle) Contact {
	var buf [maxPatchPoints]dv
	c, _ := convexTriangle(s, tri, buf[:0])
	return c
}

// faceNormalSlop, times one plus the shape's extent, is how much deeper than the GJK and EPA depth a triangle's face-normal contact may be and still replace it when the shape's centroid is not over the triangle.
const faceNormalSlop = 0.02

// insidePrism reports whether p lies inside the infinite prism swept by the convex polygon poly along the unit normal n.
func insidePrism(p dv, poly []dv, n dv) bool {
	var buf [1]dv
	var bufB [2]dv
	return len(clipToPrism(buf[:0], bufB[:0], []dv{p}, poly, n)) == 1
}

// maxPatchPoints bounds the points a contact patch keeps before it is reduced to a manifold.
const maxPatchPoints = 2 * (cylinderDiscSides + 4)

// convexTriangle is testConvexTriangle that also appends to pts the points of the face patch, when the contact came from one.
func convexTriangle(s cvx, tri Triangle, pts []dv) (Contact, []dv) {
	t := triCvx(tri)
	c := testConvex(s, t)
	if !c.Hit {
		return c, pts
	}
	tn := t.p[1].sub(t.p[0]).cross(t.p[2].sub(t.p[0])).norm()
	if tn.lenSq() == 0 {
		return c, pts
	}
	side := s.centroid().sub(t.p[0]).dot(tn)
	if side < 0 || (side == 0 && toDV(c.Normal).dot(tn) < 0) {
		tn = tn.neg()
	}
	var buf [cylinderDiscSides + 4]dv
	inc := s.feature(buf[:0], tn.neg())
	var bufC, bufD [maxPatchPoints]dv
	clipped := clipToPrism(bufC[:0], bufD[:0], inc, t.p[:], tn)
	d := tn.dot(t.p[0])
	var sum dv
	depth := 0.0
	n0 := len(pts)
	for _, p := range clipped {
		if pen := d - tn.dot(p); pen > 0 {
			m := p.add(tn.scale(pen * 0.5))
			sum = sum.add(m)
			pts = append(pts, m)
			depth = max(depth, pen)
		}
	}
	k := len(pts) - n0
	if k == 0 {
		return c, pts
	}
	// The face normal stands in for an edge normal only where the triangle could be part of a larger surface the shape rests on: its depth must stay close to the GJK and EPA depth, or the shape's centroid must lie over the triangle. A shape that grazes a ledge's open edge from the side otherwise would be pushed down or lifted by the whole height of its face above or below the ledge.
	if slop := faceNormalSlop * (1 + s.extent()); depth > float64(c.Depth)+float64(slop) && !insidePrism(s.centroid(), t.p[:], tn) {
		return c, pts[:n0]
	}
	return Contact{Hit: true, Normal: tn.vec3().Norm(), Depth: float32(depth), Point: sum.scale(1 / float64(k)).vec3()}, pts
}

// contactPatch returns the centre of the region where a's feature facing b and b's feature facing a overlap, seen along n (from b toward a), or fallback when patchPoints finds no such region.
func contactPatch(a, b *cvx, n, fallback dv) dv {
	var buf [maxPatchPoints]dv
	pts := patchPoints(a, b, n, buf[:0])
	if len(pts) == 0 {
		return fallback
	}
	var sum dv
	for _, p := range pts {
		sum = sum.add(p)
	}
	return sum.scale(1 / float64(len(pts)))
}

// patchPoints appends to dst the points of the region where a's feature facing b and b's feature facing a overlap, seen along n (from b toward a). The reference feature is the face more nearly perpendicular to n (b's on a tie); the other, incident, feature is clipped to the reference face's sides, its points that lie behind the reference face (or within a millimetre in front of it) are kept, and each is replaced by the midpoint between it and the reference face along n. Two parallel edges give the ends of their shared stretch and two crossing edges their closest points. It appends nothing when the features are a vertex and something other than a face, or when nothing survives the clipping.
func patchPoints(a, b *cvx, n dv, dst []dv) []dv {
	var bufA, bufB [cylinderDiscSides + 4]dv
	fa := a.feature(bufA[:0], n.neg())
	fb := b.feature(bufB[:0], n)
	na, nb := polygonNormal(fa), polygonNormal(fb)
	ref, inc, refN, fn := fb, fa, n, nb
	switch {
	case len(fa) >= 3 && (len(fb) < 3 || math.Abs(na.dot(n)) > math.Abs(nb.dot(n))+1e-9):
		ref, inc, refN, fn = fa, fb, n.neg(), na
	case len(fb) >= 3:
	case len(fa) == 2 && len(fb) == 2:
		return segmentPatch(fa, fb, n, dst)
	default:
		return dst
	}
	// fn is the reference face's outward normal; refN points from the reference shape toward the incident one.
	if fn.dot(refN) < 0 {
		fn = fn.neg()
	}
	cosN := fn.dot(refN)
	if cosN < 1e-3 {
		return dst
	}
	fd := fn.dot(ref[0])
	var bufC, bufD [maxPatchPoints]dv
	clipped := clipToPrism(bufC[:0], bufD[:0], inc, ref, n)
	// Points up to about a millimetre in front of the reference face count as touching, as cornersInside does for boxes, so a body resting almost flat gets a point under its middle instead of rocking between its edges.
	slop := 1e-3 * (1 + min(a.extent(), b.extent()))
	for _, p := range clipped {
		// t is how far p must move along refN to reach the reference face; positive means p is behind it.
		t := (fd - fn.dot(p)) / cosN
		if t < -slop {
			continue
		}
		dst = append(dst, p.add(refN.scale(t*0.5)))
	}
	return dst
}

// clipToPrism clips inc (a polygon, a segment or a point) to the infinite prism swept by the convex polygon ref along n, using bufA and bufB as scratch space, and returns what is left.
func clipToPrism(bufA, bufB, inc, ref []dv, n dv) []dv {
	clipped := append(bufA, inc...)
	scratch := bufB
	centre := ref[0]
	for _, p := range ref[1:] {
		centre = centre.add(p)
	}
	centre = centre.scale(1 / float64(len(ref)))
	for i := range ref {
		p0, p1 := ref[i], ref[(i+1)%len(ref)]
		sideN := p1.sub(p0).cross(n)
		if sideN.dot(centre.sub(p0)) > 0 {
			sideN = sideN.neg()
		}
		if sideN.lenSq() <= 1e-300 {
			continue
		}
		clipped, scratch = clipByPlane(scratch[:0], clipped, sideN, sideN.dot(p0)), clipped
		if len(clipped) == 0 {
			return nil
		}
	}
	return clipped
}

// polygonNormal returns the unit normal of a polygon of three or more points by Newell's method, following their winding, or zero for fewer points.
func polygonNormal(poly []dv) dv {
	if len(poly) < 3 {
		return dv{}
	}
	var n dv
	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]
		n = n.add(dv{float64((a.y - b.y) * (a.z + b.z)), float64((a.z - b.z) * (a.x + b.x)), float64((a.x - b.x) * (a.y + b.y))})
	}
	return n.norm()
}

// clipByPlane keeps the part of poly (a polygon, a segment or a point) on the side of the plane n·p <= d, appending it to dst.
func clipByPlane(dst, poly []dv, n dv, d float64) []dv {
	switch len(poly) {
	case 1:
		if poly[0].dot(n) <= d {
			dst = append(dst, poly[0])
		}
		return dst
	case 2:
		a, b := poly[0], poly[1]
		da, db := a.dot(n)-d, b.dot(n)-d
		switch {
		case da <= 0 && db <= 0:
			return append(dst, a, b)
		case da > 0 && db > 0:
			return dst
		case da > 0:
			return append(dst, a.lerp(b, da/(da-db)), b)
		default:
			return append(dst, a, a.lerp(b, da/(da-db)))
		}
	}
	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]
		da, db := a.dot(n)-d, b.dot(n)-d
		if da <= 0 {
			dst = append(dst, a)
		}
		if (da < 0 && db > 0) || (da > 0 && db < 0) {
			dst = append(dst, a.lerp(b, da/(da-db)))
		}
	}
	return dst
}

// segmentPatch appends the points two edges in contact share: the ends of their common stretch when they are parallel, or the midpoint of their closest points when they cross.
func segmentPatch(fa, fb []dv, n dv, dst []dv) []dv {
	da, db := fa[1].sub(fa[0]), fb[1].sub(fb[0])
	la, lb := da.length(), db.length()
	if la <= 1e-12 || lb <= 1e-12 {
		return dst
	}
	if da.cross(db).length() > 0.05*la*lb {
		ca, cb := closestPointsSegments(fa[0].vec3(), fa[1].vec3(), fb[0].vec3(), fb[1].vec3())
		return append(dst, toDV(ca).lerp(toDV(cb), 0.5))
	}
	u := db.scale(1 / lb)
	t0, t1 := fa[0].sub(fb[0]).dot(u), fa[1].sub(fb[0]).dot(u)
	lo, hi := math.Max(min(t0, t1), 0), math.Min(max(t0, t1), lb)
	if lo > hi {
		return dst
	}
	// Each end sits halfway between b's edge and a's along n.
	gap := fa[0].sub(fb[0]).dot(n) / 2
	return append(dst, fb[0].add(u.scale(lo)).add(n.scale(gap)), fb[0].add(u.scale(hi)).add(n.scale(gap)))
}
