package physics3d

import "math"

// manifoldPoint is one contact point in a multi-point contact manifold between
// two shapes A and B, along a shared contact normal pointing from B toward A.
type manifoldPoint struct {
	point     Vec3
	depth     float32
	featureID uint32

	// Optional reference-plane geometry (in world space before body-local
	// conversion in World.Step) for exact per-point NGS positional correction.
	hasPlane  bool
	refIsA    bool
	refPoint  Vec3
	refNormal Vec3 // outward normal of reference face (points toward incident shape)
	incPoint  Vec3 // point on incident shape's surface (or capsule centerline when incRadius > 0)
	incRadius float32
}

// clipVert carries a float64 polygon vertex and its geometric feature ID through
// Sutherland-Hodgman clipping.
type clipVert struct {
	p  dv
	id uint32
}

// manifoldSlop keeps vertices up to 0.1 mm in front of a reference face so
// flat resting faces do not drop corners due to float32 rounding.
const manifoldSlop = 1e-4

// buildShapeManifold builds up to 4 contact points for the overlapping pair
// (a, b) given the representative contact c from TestShapes(a, b).
//
//   - Box-Box face contacts use Sutherland-Hodgman face clipping (up to 4 points
//     with persistent feature IDs and per-point depths).
//   - Capsule-on-face contacts (against boxes, convex hulls, or cylinder caps)
//     clip the capsule segment to the face prism (up to 2 points).
//   - Convex hull and cylinder contacts clip the incident feature against the
//     reference face (up to 4 points with per-point depths).
//   - Sphere contacts and edge-edge contacts return 1 point.
func buildShapeManifold(a, b Shape, c Contact, dst []manifoldPoint) []manifoldPoint {
	dst = dst[:0]
	switch {
	case a.Kind == ShapeBox && b.Kind == ShapeBox:
		if out := boxBoxManifold(a, b, c, dst); len(out) > 0 {
			return out
		}
	case a.Kind == ShapeCapsule && (b.Kind == ShapeBox || b.Kind == ShapeConvexHull || b.Kind == ShapeCylinder):
		if out := capsuleFaceManifold(a.Capsule, b, c, false, dst); len(out) > 0 {
			return out
		}
	case b.Kind == ShapeCapsule && (a.Kind == ShapeBox || a.Kind == ShapeConvexHull || a.Kind == ShapeCylinder):
		if out := capsuleFaceManifold(b.Capsule, a, c, true, dst); len(out) > 0 {
			return out
		}
	case a.Kind == ShapeCapsule && b.Kind == ShapeCapsule:
		if out := capsuleCapsuleManifold(a.Capsule, b.Capsule, c, dst); len(out) > 0 {
			return out
		}
	case isConvexKind(a) || isConvexKind(b):
		if out := convexShapeManifold(a, b, c, dst); len(out) > 0 {
			return out
		}
	}
	return append(dst, manifoldPoint{point: c.Point, depth: c.Depth})
}

// buildTriangleManifold builds up to 4 contact points for a shape against a
// static triangle (box face clipping, 2-point capsule-on-triangle, or convex
// hull / cylinder face clipping).
func buildTriangleManifold(s Shape, tri Triangle, c Contact, heightfieldFlipped bool, dst []manifoldPoint) []manifoldPoint {
	dst = dst[:0]
	if heightfieldFlipped {
		return append(dst, manifoldPoint{point: c.Point, depth: c.Depth})
	}
	switch s.Kind {
	case ShapeCapsule:
		if out := capsuleTriangleManifold(s.Capsule, tri, c, dst); len(out) > 0 {
			return out
		}
	case ShapeBox, ShapeConvexHull, ShapeCylinder:
		if out := shapeOnTriangleManifold(s, tri, c, dst); len(out) > 0 {
			return out
		}
	}
	return append(dst, manifoldPoint{point: c.Point, depth: c.Depth})
}

// boxFaceVertices writes the 4 corners of face (axis, sign) of box (center,
// axes, ext) in counter-clockwise order seen from outside, along with the face
// outward normal and the two tangent axes.
func boxFaceVertices(center dv, axes [3]dv, ext dv, axis int, sign float64) (verts [4]dv, faceN, uAxis, wAxis dv, uExt, wExt float64) {
	e := [3]float64{ext.x, ext.y, ext.z}
	u := (axis + 1) % 3
	w := (axis + 2) % 3
	faceN = axes[axis].scale(sign)
	uAxis = axes[u]
	wAxis = axes[w]
	uExt = e[u]
	wExt = e[w]
	faceCenter := center.add(faceN.scale(e[axis]))
	if sign > 0 {
		verts[0] = faceCenter.add(uAxis.scale(-uExt)).add(wAxis.scale(-wExt))
		verts[1] = faceCenter.add(uAxis.scale(uExt)).add(wAxis.scale(-wExt))
		verts[2] = faceCenter.add(uAxis.scale(uExt)).add(wAxis.scale(wExt))
		verts[3] = faceCenter.add(uAxis.scale(-uExt)).add(wAxis.scale(wExt))
	} else {
		verts[0] = faceCenter.add(uAxis.scale(-uExt)).add(wAxis.scale(-wExt))
		verts[1] = faceCenter.add(uAxis.scale(-uExt)).add(wAxis.scale(wExt))
		verts[2] = faceCenter.add(uAxis.scale(uExt)).add(wAxis.scale(wExt))
		verts[3] = faceCenter.add(uAxis.scale(uExt)).add(wAxis.scale(-wExt))
	}
	return verts, faceN, uAxis, wAxis, uExt, wExt
}

func bestBoxFaceAlong(axes [3]dv, dir dv) (axis int, sign float64, absDot float64) {
	best := -1.0
	for i := range 3 {
		d := axes[i].dot(dir)
		ad := math.Abs(d)
		if ad > best {
			best = ad
			axis = i
			if d >= 0 {
				sign = 1
			} else {
				sign = -1
			}
		}
	}
	return axis, sign, best
}

func faceIndex(axis int, sign float64) uint32 {
	if sign < 0 {
		return uint32(axis*2 + 1)
	}
	return uint32(axis * 2)
}

// clipPolyPlane clips poly against plane n · p <= d, tracking feature IDs for
// newly created intersection points.
func clipPolyPlane(dst, poly []clipVert, n dv, d float64, planeTag uint32) []clipVert {
	dst = dst[:0]
	nPts := len(poly)
	if nPts == 0 {
		return dst
	}
	const eps = 1e-9
	for i := range nPts {
		a := poly[i]
		b := poly[(i+1)%nPts]
		da := a.p.dot(n) - d
		db := b.p.dot(n) - d
		if da <= eps {
			dst = append(dst, a)
		}
		if (da < -eps && db > eps) || (da > eps && db < -eps) {
			t := da / (da - db)
			var id uint32
			if da > 0 {
				// Entering: associate with planeTag and incoming vertex b.
				id = (planeTag << 4) | (b.id & 0xF) | 0x100
			} else {
				// Exiting: associate with planeTag and outgoing vertex a.
				id = (planeTag << 4) | (a.id & 0xF) | 0x200
			}
			dst = append(dst, clipVert{p: a.p.lerp(b.p, t), id: id})
		}
	}
	return dst
}

// boxBoxManifold computes up to 4 contact points for two oriented boxes via
// Sutherland-Hodgman face clipping against the reference face's 4 side planes.
func boxBoxManifold(a, b Shape, c Contact, dst []manifoldPoint) []manifoldPoint {
	axA := rotAxes(a.Rot)
	axB := rotAxes(b.Rot)
	cA := toDV(a.Box.Center)
	cB := toDV(b.Box.Center)
	eA := toDV(a.Box.HalfExtents)
	eB := toDV(b.Box.HalfExtents)
	n := toDV(c.Normal).norm() // from B toward A

	// Best face on B along +n (facing A) and best face on A along -n (facing B).
	axIdxB, signB, dotB := bestBoxFaceAlong(axB, n)
	axIdxA, signA, dotA := bestBoxFaceAlong(axA, n.neg())

	// Edge-edge contact: neither box has a face aligned with the separating normal.
	if max(dotA, dotB) < 0.85 {
		return append(dst, manifoldPoint{
			point:     c.Point,
			depth:     c.Depth,
			featureID: 0xEE000000 | (faceIndex(axIdxA, signA) << 4) | faceIndex(axIdxB, signB),
		})
	}

	var refIsA bool
	var refCenter, refExt, incCenter, incExt dv
	var refAxes, incAxes [3]dv
	var refAxis int
	var refSign float64
	if dotB >= dotA-1e-4 {
		refIsA = false
		refCenter, refAxes, refExt = cB, axB, eB
		incCenter, incAxes, incExt = cA, axA, eA
		refAxis, refSign = axIdxB, signB
	} else {
		refIsA = true
		refCenter, refAxes, refExt = cA, axA, eA
		incCenter, incAxes, incExt = cB, axB, eB
		refAxis, refSign = axIdxA, signA
	}

	refVerts, refN, uAxis, wAxis, uExt, wExt := boxFaceVertices(refCenter, refAxes, refExt, refAxis, refSign)
	// Incident face is most anti-aligned with refN.
	incAxis, incSign, _ := bestBoxFaceAlong(incAxes, refN.neg())
	incVerts, _, _, _, _, _ := boxFaceVertices(incCenter, incAxes, incExt, incAxis, incSign)

	refF := faceIndex(refAxis, refSign)
	incF := faceIndex(incAxis, incSign)
	var baseTag uint32 = (refF << 16) | (incF << 12)
	if refIsA {
		baseTag |= 1 << 24
	}

	var bufA, bufB [16]clipVert
	poly := bufA[:0]
	for i := range 4 {
		poly = append(poly, clipVert{p: incVerts[i], id: uint32(i + 1)})
	}

	// Clip against the 4 side planes of the reference face.
	refFaceCenter := refVerts[0].add(refVerts[2]).scale(0.5)
	uCenter := refFaceCenter.dot(uAxis)
	wCenter := refFaceCenter.dot(wAxis)

	poly = clipPolyPlane(bufB[:0], poly, uAxis, uCenter+uExt, 1)
	if len(poly) == 0 {
		return dst
	}
	poly = clipPolyPlane(bufA[:0], poly, uAxis.neg(), -uCenter+uExt, 2)
	if len(poly) == 0 {
		return dst
	}
	poly = clipPolyPlane(bufB[:0], poly, wAxis, wCenter+wExt, 3)
	if len(poly) == 0 {
		return dst
	}
	poly = clipPolyPlane(bufA[:0], poly, wAxis.neg(), -wCenter+wExt, 4)
	if len(poly) == 0 {
		return dst
	}

	refD := refN.dot(refFaceCenter)
	var kept [16]manifoldCandidate
	nKept := 0
	for _, v := range poly {
		pen := refD - refN.dot(v.p)
		if pen < -manifoldSlop {
			continue
		}
		depth := math.Max(pen, 0)
		kept[nKept] = manifoldCandidate{
			point:    v.p.add(refN.scale(depth * 0.5)),
			incPoint: v.p,
			depth:    depth,
			id:       baseTag | (v.id & 0xFFF),
		}
		nKept++
		if nKept == len(kept) {
			break
		}
	}
	if nKept == 0 {
		return dst
	}
	chosenBuf, nChosen := reduceManifoldCandidates(kept[:nKept], refN)
	refPt3 := refFaceCenter.vec3()
	refN3 := refN.vec3().Norm()
	for _, mc := range chosenBuf[:nChosen] {
		dst = append(dst, manifoldPoint{
			point:     mc.point.vec3(),
			depth:     float32(mc.depth),
			featureID: mc.id,
			hasPlane:  true,
			refIsA:    refIsA,
			refPoint:  refPt3,
			refNormal: refN3,
			incPoint:  mc.incPoint.vec3(),
		})
	}
	return dst
}

// clipSegmentToPrism clips the segment [s0, s1] against the infinite prism of
// convex polygon ref extruded along unit normal fn. It returns up to 2 clipped
// endpoints with feature IDs (1 for s0, 2 for s1, or side-plane ID if clipped).
func clipSegmentToPrism(s0, s1 dv, ref []dv, fn dv) (p0, p1 dv, id0, id1 uint32, ok bool) {
	p0, p1 = s0, s1
	id0, id1 = 1, 2
	centre := ref[0]
	for _, p := range ref[1:] {
		centre = centre.add(p)
	}
	centre = centre.scale(1.0 / float64(len(ref)))
	for i := range ref {
		e0, e1 := ref[i], ref[(i+1)%len(ref)]
		sideN := e1.sub(e0).cross(fn)
		if sideN.dot(centre.sub(e0)) > 0 {
			sideN = sideN.neg()
		}
		if sideN.lenSq() <= 1e-300 {
			continue
		}
		d := sideN.dot(e0)
		d0 := p0.dot(sideN) - d
		d1 := p1.dot(sideN) - d
		if d0 > 0 && d1 > 0 {
			return dv{}, dv{}, 0, 0, false
		}
		if d0 > 0 {
			p0 = p0.lerp(p1, d0/(d0-d1))
			id0 = uint32(16 + i)
		} else if d1 > 0 {
			p1 = p0.lerp(p1, d0/(d0-d1))
			id1 = uint32(32 + i)
		}
	}
	return p0, p1, id0, id1, true
}

// capsuleFaceManifold computes up to 2 contact points when a capsule rests
// against a planar face of a box, convex hull, or cylinder cap.
// capsuleIsB is false when capsule is shape A (normal points B -> A), true when
// capsule is shape B (normal points B -> A, so face normal points along -c.Normal).
func capsuleFaceManifold(cap Capsule, faceShape Shape, c Contact, capsuleIsB bool, dst []manifoldPoint) []manifoldPoint {
	n := toDV(c.Normal).norm() // points from B toward A
	faceDir := n               // points from faceShape toward capsule
	if capsuleIsB {
		faceDir = n.neg()
	}
	oc := toCvx(faceShape)
	var fBuf [cylinderDiscSides + 4]dv
	ref := oc.feature(fBuf[:0], faceDir)
	if len(ref) < 3 {
		return dst
	}
	fn := polygonNormal(ref)
	if fn.lenSq() <= 1e-12 {
		return dst
	}
	if fn.dot(faceDir) < 0 {
		fn = fn.neg()
	}
	if fn.dot(faceDir) < 0.7 {
		return dst
	}
	return capsuleAgainstPolygon(cap, ref, fn, capsuleIsB, dst)
}

func capsuleTriangleManifold(cap Capsule, tri Triangle, c Contact, dst []manifoldPoint) []manifoldPoint {
	ref := [3]dv{toDV(tri.A), toDV(tri.B), toDV(tri.C)}
	fn := ref[1].sub(ref[0]).cross(ref[2].sub(ref[0])).norm()
	if fn.lenSq() <= 1e-12 {
		return dst
	}
	n := toDV(c.Normal).norm()
	if fn.dot(n) < 0 {
		fn = fn.neg()
	}
	if fn.dot(n) < 0.7 {
		return dst
	}
	return capsuleAgainstPolygon(cap, ref[:], fn, false, dst)
}

func capsuleAgainstPolygon(cap Capsule, ref []dv, fn dv, capsuleIsB bool, dst []manifoldPoint) []manifoldPoint {
	s0, s1 := toDV(cap.A), toDV(cap.B)
	q0, q1, id0, id1, ok := clipSegmentToPrism(s0, s1, ref, fn)
	if !ok {
		return dst
	}
	r := float64(max(cap.Radius, 0))
	fd := fn.dot(ref[0])
	pen0 := (fd - fn.dot(q0)) + r
	pen1 := (fd - fn.dot(q1)) + r

	keep0 := pen0 >= -manifoldSlop
	keep1 := pen1 >= -manifoldSlop
	if !keep0 && !keep1 {
		return dst
	}

	refPt3 := ref[0].vec3()
	refN3 := fn.vec3().Norm()
	baseTag := uint32(0xCA000000)
	if capsuleIsB {
		baseTag |= 1 << 20
	}

	emit := func(q dv, pen float64, id uint32) {
		depth := math.Max(pen, 0)
		pt := q.sub(fn.scale(r - float64(depth*0.5)))
		dst = append(dst, manifoldPoint{
			point:     pt.vec3(),
			depth:     float32(depth),
			featureID: baseTag | id,
			hasPlane:  true,
			refIsA:    capsuleIsB,
			refPoint:  refPt3,
			refNormal: refN3,
			incPoint:  q.vec3(),
			incRadius: float32(r),
		})
	}

	if keep0 && keep1 {
		// Collapse nearly identical in-plane points (e.g. a vertical capsule standing on end).
		diff := q1.sub(q0)
		inPlane := diff.sub(fn.scale(diff.dot(fn)))
		minSep := 1e-4 * (1 + r)
		if inPlane.lenSq() <= minSep*minSep {
			if pen1 > pen0 {
				emit(q1, pen1, id1)
			} else {
				emit(q0, pen0, id0)
			}
			return dst
		}
		emit(q0, pen0, id0)
		emit(q1, pen1, id1)
		return dst
	}
	if keep0 {
		emit(q0, pen0, id0)
	} else {
		emit(q1, pen1, id1)
	}
	return dst
}

// capsuleCapsuleManifold returns 2 points when two capsules lie nearly parallel
// with an overlapping stretch, or falls back to the single contact point.
func capsuleCapsuleManifold(a, b Capsule, c Contact, dst []manifoldPoint) []manifoldPoint {
	a0, a1 := toDV(a.A), toDV(a.B)
	b0, b1 := toDV(b.A), toDV(b.B)
	da, db := a1.sub(a0), b1.sub(b0)
	la, lb := da.length(), db.length()
	if la <= 1e-6 || lb <= 1e-6 {
		return dst
	}
	if da.cross(db).length() > 0.05*la*lb {
		return dst
	}
	u := db.scale(1.0 / lb)
	t0 := a0.sub(b0).dot(u)
	t1 := a1.sub(b0).dot(u)
	lo := math.Max(min(t0, t1), 0)
	hi := math.Min(max(t0, t1), lb)
	minSep := 1e-4 * (1 + float64(a.Radius+b.Radius))
	if hi-lo <= minSep {
		return dst
	}
	n := toDV(c.Normal).norm()
	rSum := float64(a.Radius + b.Radius)
	emitEnd := func(t float64, id uint32) {
		pb := b0.add(u.scale(t))
		// Corresponding point on segment A.
		ta := pb.sub(a0).dot(da) / (la * la)
		ta = math.Max(0, math.Min(1, ta))
		pa := a0.add(da.scale(ta))
		dist := pa.sub(pb).dot(n)
		pen := rSum - dist
		if pen < -manifoldSlop {
			return
		}
		depth := math.Max(pen, 0)
		pt := pb.add(n.scale(float64(b.Radius) - float64(depth*0.5)))
		dst = append(dst, manifoldPoint{
			point:     pt.vec3(),
			depth:     float32(depth),
			featureID: 0xCC000000 | id,
		})
	}
	emitEnd(lo, 1)
	emitEnd(hi, 2)
	return dst
}

type manifoldCandidate struct {
	point    dv
	incPoint dv
	depth    float64
	id       uint32
}

// reduceManifoldCandidates reduces candidates to at most 4 points in a
// deterministic order without heap allocation, keeping the deepest point first
// and then selecting up to 3 more points that maximize the projected contact
// polygon area.
func reduceManifoldCandidates(cands []manifoldCandidate, n dv) (out [4]manifoldCandidate, count int) {
	if len(cands) <= 4 {
		return out, copy(out[:], cands)
	}
	n = n.norm()
	// 1. Deepest point (lowest index breaks ties).
	i0 := 0
	for i := 1; i < len(cands); i++ {
		if cands[i].depth > cands[i0].depth+1e-12 {
			i0 = i
		}
	}
	// 2. Furthest point from i0 in the contact plane.
	projDistSq := func(a, b dv) float64 {
		d := b.sub(a)
		d = d.sub(n.scale(d.dot(n)))
		return d.lenSq()
	}
	i1 := -1
	bestD := -1.0
	for i := range cands {
		if i == i0 {
			continue
		}
		if d := projDistSq(cands[i0].point, cands[i].point); d > bestD+1e-12 {
			bestD = d
			i1 = i
		}
	}
	if i1 < 0 {
		out[0] = cands[i0]
		return out, 1
	}
	// 3. Point maximizing triangle area with (i0, i1).
	triArea2 := func(a, b, c dv) float64 {
		return b.sub(a).cross(c.sub(a)).dot(n)
	}
	i2 := -1
	bestA := -1.0
	for i := range cands {
		if i == i0 || i == i1 {
			continue
		}
		if a := math.Abs(triArea2(cands[i0].point, cands[i1].point, cands[i].point)); a > bestA+1e-12 {
			bestA = a
			i2 = i
		}
	}
	if i2 < 0 {
		out[0], out[1] = cands[i0], cands[i1]
		return out, 2
	}
	// Orient (i0, i1, i2) positively around n so exterior triangles have positive area.
	if triArea2(cands[i0].point, cands[i1].point, cands[i2].point) < 0 {
		i1, i2 = i2, i1
	}
	// 4. Point maximizing the additional quad area outside any edge of triangle (i0, i1, i2).
	i3 := -1
	bestQ := 0.0
	for i := range cands {
		if i == i0 || i == i1 || i == i2 {
			continue
		}
		p := cands[i].point
		a0 := triArea2(cands[i1].point, cands[i0].point, p)
		a1 := triArea2(cands[i2].point, cands[i1].point, p)
		a2 := triArea2(cands[i0].point, cands[i2].point, p)
		q := max(a0, a1, a2)
		if q > bestQ+1e-12 {
			bestQ = q
			i3 = i
		}
	}
	if i3 < 0 {
		out[0], out[1], out[2] = cands[i0], cands[i1], cands[i2]
		return out, 3
	}
	out[0], out[1], out[2], out[3] = cands[i0], cands[i1], cands[i2], cands[i3]
	return out, 4
}

// convexShapeManifold computes up to 4 contact points with per-point depths and
// reference-plane metadata for pairs involving a ConvexHull or Cylinder.
func convexShapeManifold(a, b Shape, c Contact, dst []manifoldPoint) []manifoldPoint {
	ca, cb := toCvx(a), toCvx(b)
	n := toDV(c.Normal).norm() // from B toward A

	var bufA, bufB [cylinderDiscSides + 4]dv
	fa := ca.feature(bufA[:0], n.neg())
	fb := cb.feature(bufB[:0], n)
	na, nb := polygonNormal(fa), polygonNormal(fb)

	ref, inc, refN, fn := fb, fa, n, nb
	refIsA := false
	switch {
	case len(fa) >= 3 && (len(fb) < 3 || math.Abs(na.dot(n)) > math.Abs(nb.dot(n))+1e-9):
		ref, inc, refN, fn = fa, fb, n.neg(), na
		refIsA = true
	case len(fb) >= 3:
	case len(fa) == 2 && len(fb) == 2:
		var segBuf [2]dv
		pts := segmentPatch(fa, fb, n, segBuf[:0])
		for _, p := range pts {
			dst = append(dst, manifoldPoint{point: p.vec3(), depth: c.Depth})
		}
		return dst
	default:
		return dst
	}

	if fn.dot(refN) < 0 {
		fn = fn.neg()
	}
	cosN := fn.dot(refN)
	if cosN < 0.85 {
		return dst
	}
	fd := fn.dot(ref[0])
	var bufC, bufD [maxPatchPoints]dv
	clipped := clipToPrism(bufC[:0], bufD[:0], inc, ref, n)
	slop := 1e-3 * (1 + min(ca.extent(), cb.extent()))
	var cands [maxPatchPoints]manifoldCandidate
	nCands := 0
	for _, p := range clipped {
		t := (fd - fn.dot(p)) / cosN
		if t < -slop {
			continue
		}
		depth := math.Max(t, 0)
		cands[nCands] = manifoldCandidate{
			point:    p.add(refN.scale(depth * 0.5)),
			incPoint: p,
			depth:    depth,
		}
		nCands++
		if nCands == len(cands) {
			break
		}
	}
	if nCands == 0 {
		return dst
	}
	chosenBuf, nChosen := reduceManifoldCandidates(cands[:nCands], fn)
	refPt3 := ref[0].vec3()
	refN3 := fn.vec3().Norm()
	usePlane := cosN >= 0.95
	for _, mc := range chosenBuf[:nChosen] {
		dst = append(dst, manifoldPoint{
			point:     mc.point.vec3(),
			depth:     float32(mc.depth),
			hasPlane:  usePlane,
			refIsA:    refIsA,
			refPoint:  refPt3,
			refNormal: refN3,
			incPoint:  mc.incPoint.vec3(),
		})
	}
	return dst
}

// shapeOnTriangleManifold clips a box, convex hull, or cylinder feature against
// a static triangle face and returns up to 4 contact points with per-point depths.
func shapeOnTriangleManifold(s Shape, tri Triangle, c Contact, dst []manifoldPoint) []manifoldPoint {
	t := [3]dv{toDV(tri.A), toDV(tri.B), toDV(tri.C)}
	fn := t[1].sub(t[0]).cross(t[2].sub(t[0])).norm()
	if fn.lenSq() <= 1e-12 {
		return dst
	}
	n := toDV(c.Normal).norm()
	if fn.dot(n) < 0 {
		fn = fn.neg()
	}
	cosN := fn.dot(n)
	if cosN < 0.9 {
		return dst
	}
	cs := toCvx(s)
	var fBuf [cylinderDiscSides + 4]dv
	inc := cs.feature(fBuf[:0], fn.neg())
	if len(inc) < 2 {
		return dst
	}
	var bufC, bufD [maxPatchPoints]dv
	clipped := clipToPrism(bufC[:0], bufD[:0], inc, t[:], fn)
	fd := fn.dot(t[0])
	slop := 1e-3 * (1 + cs.extent())
	var cands [maxPatchPoints]manifoldCandidate
	nCands := 0
	for _, p := range clipped {
		pen := fd - fn.dot(p)
		if pen < -slop {
			continue
		}
		depth := math.Max(pen, 0)
		cands[nCands] = manifoldCandidate{
			point:    p.add(fn.scale(depth * 0.5)),
			incPoint: p,
			depth:    depth,
		}
		nCands++
		if nCands == len(cands) {
			break
		}
	}
	if nCands == 0 {
		return dst
	}
	chosenBuf, nChosen := reduceManifoldCandidates(cands[:nCands], fn)
	refPt3 := t[0].vec3()
	refN3 := fn.vec3().Norm()
	usePlane := cosN >= 0.95
	for _, mc := range chosenBuf[:nChosen] {
		dst = append(dst, manifoldPoint{
			point:     mc.point.vec3(),
			depth:     float32(mc.depth),
			hasPlane:  usePlane,
			refIsA:    false,
			refPoint:  refPt3,
			refNormal: refN3,
			incPoint:  mc.incPoint.vec3(),
		})
	}
	return dst
}
