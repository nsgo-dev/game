package physics3d

import "math"

// CompoundChild is one child collider inside a compound Shape, positioned in
// the parent's local coordinate frame by Offset and Rot.
type CompoundChild struct {
	// Shape is the child collider primitive (Box, Sphere, Capsule, ConvexHull,
	// Cylinder, or nested ShapeCompound).
	Shape Shape `json:"shape"`
	// Offset is the child's local translation relative to the compound origin.
	Offset Vec3 `json:"offset"`
	// Rot is the child's local orientation quaternion. A zero quaternion is
	// treated as identity.
	Rot Quat `json:"rot"`
	// Mass is an optional explicit mass in kilograms for this child.
	// When Mass <= 0 and Density > 0, mass is computed as Density * volume(Shape).
	// Trigger children do not contribute to body mass, center of mass, or inertia.
	Mass float32 `json:"mass,omitempty"`
	// Density is an optional mass per unit volume (kg/m^3) used when Mass <= 0.
	Density float32 `json:"density,omitempty"`
	// Trigger marks this child as a sensor volume that emits trigger ContactEvents
	// without applying collision impulses or contributing to body mass/inertia.
	Trigger bool `json:"trigger,omitempty"`
	// Layer is an optional collision layer bitmask for this child.
	// Zero inherits the parent Body.Layer (which defaults to LayerAll when zero).
	Layer LayerMask `json:"layer,omitempty"`
	// Mask is an optional collision mask bitmask for this child.
	// Zero inherits the parent Body.Mask (which defaults to LayerAll when zero).
	Mask LayerMask `json:"mask,omitempty"`
}

// CompoundShape wraps children in a tagged Shape of kind ShapeCompound,
// copying the slice so later mutations to the caller's slice do not alias.
func CompoundShape(children ...CompoundChild) Shape {
	if len(children) == 0 {
		return Shape{Kind: ShapeCompound}
	}
	copied := make([]CompoundChild, len(children))
	for i, c := range children {
		c.Shape = cloneShape(c.Shape)
		copied[i] = c
	}
	return Shape{Kind: ShapeCompound, Children: copied}
}

// LocalShape returns c.Shape transformed into the parent's local frame by
// c.Offset and c.Rot.
func (c CompoundChild) LocalShape() Shape {
	if c.Offset == (Vec3{}) && c.Rot.IsIdentity() {
		return c.Shape
	}
	return c.Shape.Transformed(c.Offset, c.Rot)
}

// WorldShape returns c.Shape transformed into world space by applying c's local
// Offset and Rot first and then the parent's pos and rot.
func (c CompoundChild) WorldShape(parentPos Vec3, parentRot Quat) Shape {
	if parentRot.IsIdentity() {
		if c.Rot.IsIdentity() {
			return c.Shape.Translated(c.Offset.Add(parentPos))
		}
		return c.Shape.Transformed(c.Offset.Add(parentPos), c.Rot)
	}
	pRot := parentRot.Norm()
	worldOffset := pRot.RotateVec(c.Offset).Add(parentPos)
	if c.Rot.IsIdentity() {
		return c.Shape.Transformed(worldOffset, pRot)
	}
	combinedRot := pRot.Mul(c.Rot.Norm()).Norm()
	return c.Shape.Transformed(worldOffset, combinedRot)
}

// EffectiveLayer returns c's collision layer, inheriting parentLayer when c.Layer
// is zero (and defaulting to LayerAll when both are zero).
func (c CompoundChild) EffectiveLayer(parentLayer LayerMask) LayerMask {
	if c.Layer != 0 {
		return c.Layer
	}
	return effectiveLayer(parentLayer)
}

// EffectiveMask returns c's collision mask, inheriting parentMask when c.Mask is
// zero (and defaulting to LayerAll when both are zero).
func (c CompoundChild) EffectiveMask(parentMask LayerMask) LayerMask {
	if c.Mask != 0 {
		return c.Mask
	}
	return effectiveLayer(parentMask)
}

func cloneShape(s Shape) Shape {
	if s.Kind != ShapeCompound || len(s.Children) == 0 {
		return s
	}
	out := s
	out.Children = make([]CompoundChild, len(s.Children))
	for i, c := range s.Children {
		c.Shape = cloneShape(c.Shape)
		out.Children[i] = c
	}
	return out
}

func sanitizeShape(s Shape) Shape {
	if s.Kind != ShapeCompound {
		return s
	}
	out := cloneShape(s)
	for i := range out.Children {
		c := &out.Children[i]
		if !isFiniteVec(c.Offset) {
			c.Offset = Vec3{}
		}
		c.Rot = normalizedQuat(c.Rot)
		if !isFiniteQuat(c.Rot) {
			c.Rot = QuatIdentity()
		}
		if c.Rot.IsIdentity() {
			c.Rot = Quat{}
		}
		if !isFinite32(c.Mass) || c.Mass < 0 {
			c.Mass = 0
		}
		if !isFinite32(c.Density) || c.Density < 0 {
			c.Density = 0
		}
		c.Shape = sanitizeShape(c.Shape)
	}
	return out
}

// SymMat3 is a symmetric 3x3 matrix stored by its six unique entries:
//
//	[ XX  XY  XZ ]
//	[ XY  YY  YZ ]
//	[ XZ  YZ  ZZ ]
//
// All arithmetic avoids fused multiply-add contraction for cross-architecture
// determinism.
type SymMat3 struct {
	XX, YY, ZZ float32
	XY, XZ, YZ float32
}

// IsZero reports whether all entries of m are zero.
func (m SymMat3) IsZero() bool {
	return m.XX == 0 && m.YY == 0 && m.ZZ == 0 && m.XY == 0 && m.XZ == 0 && m.YZ == 0
}

// Add returns the component-wise sum m + b.
func (m SymMat3) Add(b SymMat3) SymMat3 {
	return SymMat3{
		XX: m.XX + b.XX,
		YY: m.YY + b.YY,
		ZZ: m.ZZ + b.ZZ,
		XY: m.XY + b.XY,
		XZ: m.XZ + b.XZ,
		YZ: m.YZ + b.YZ,
	}
}

// MulVec returns m * v without FMA contraction.
func (m SymMat3) MulVec(v Vec3) Vec3 {
	return Vec3{
		X: float32(float32(m.XX*v.X)+float32(m.XY*v.Y)) + float32(m.XZ*v.Z),
		Y: float32(float32(m.XY*v.X)+float32(m.YY*v.Y)) + float32(m.YZ*v.Z),
		Z: float32(float32(m.XZ*v.X)+float32(m.YZ*v.Y)) + float32(m.ZZ*v.Z),
	}
}

// QuadForm returns v^T * m * v.
func (m SymMat3) QuadForm(v Vec3) float32 {
	return v.Dot(m.MulVec(v))
}

// Rotated returns R * m * R^T where R is the 3x3 rotation matrix of unit
// quaternion q, computed without FMA contraction.
func (m SymMat3) Rotated(q Quat) SymMat3 {
	if q.IsIdentity() {
		return m
	}
	u := q.Norm()
	// Columns of R are the rotated basis vectors; rows of R are the rotated basis vectors of R^T.
	r0 := u.RotateVec(Vec3{X: 1})
	r1 := u.RotateVec(Vec3{Y: 1})
	r2 := u.RotateVec(Vec3{Z: 1})
	// Rows of R:
	row0 := Vec3{X: r0.X, Y: r1.X, Z: r2.X}
	row1 := Vec3{X: r0.Y, Y: r1.Y, Z: r2.Y}
	row2 := Vec3{X: r0.Z, Y: r1.Z, Z: r2.Z}

	mr0 := m.MulVec(row0)
	mr1 := m.MulVec(row1)
	mr2 := m.MulVec(row2)
	return SymMat3{
		XX: row0.Dot(mr0),
		YY: row1.Dot(mr1),
		ZZ: row2.Dot(mr2),
		XY: row0.Dot(mr1),
		XZ: row0.Dot(mr2),
		YZ: row1.Dot(mr2),
	}
}

// Inverse returns the inverse of m computed in float64 without FMA contraction,
// or (SymMat3{}, false) if m is singular or non-finite.
func (m SymMat3) Inverse() (SymMat3, bool) {
	a, b, c := float64(m.XX), float64(m.XY), float64(m.XZ)
	d, e, f := float64(m.YY), float64(m.YZ), float64(m.ZZ)

	c00 := float64(d*f) - float64(e*e)
	c01 := float64(c*e) - float64(b*f)
	c02 := float64(b*e) - float64(c*d)
	c11 := float64(a*f) - float64(c*c)
	c12 := float64(b*c) - float64(a*e)
	c22 := float64(a*d) - float64(b*b)

	det := float64(float64(a*c00)+float64(b*c01)) + float64(c*c02)
	if math.IsNaN(det) || math.IsInf(det, 0) || math.Abs(det) <= 1e-18 {
		return SymMat3{}, false
	}
	invDet := 1.0 / det
	out := SymMat3{
		XX: float32(c00 * invDet),
		YY: float32(c11 * invDet),
		ZZ: float32(c22 * invDet),
		XY: float32(c01 * invDet),
		XZ: float32(c02 * invDet),
		YZ: float32(c12 * invDet),
	}
	if !isFiniteSymMat3(out) {
		return SymMat3{}, false
	}
	return out, true
}

func isFiniteSymMat3(m SymMat3) bool {
	return isFinite32(m.XX) && isFinite32(m.YY) && isFinite32(m.ZZ) &&
		isFinite32(m.XY) && isFinite32(m.XZ) && isFinite32(m.YZ)
}

// CompoundMassProperties holds the combined mass, local center of mass, and
// local inertia tensor (about CenterOfMass) computed from a compound shape's
// solid children using the parallel-axis theorem.
type CompoundMassProperties struct {
	// Mass is the total mass of all solid children in kilograms.
	Mass float32
	// CenterOfMass is the combined center of mass in the compound's local frame.
	CenterOfMass Vec3
	// Inertia is the 3x3 symmetric inertia tensor about CenterOfMass in local space.
	Inertia SymMat3
	// InvInertia is the inverse of Inertia in local space.
	InvInertia SymMat3
	// ScalarInvI is the isotropic mean inverse inertia 3 / (Ixx + Iyy + Izz).
	ScalarInvI float32
}

type solidLeaf struct {
	shape        Shape
	centroid     Vec3
	volume       float64
	explicitMass float64
	hasExplicit  bool
}

func collectSolidLeaves(children []CompoundChild, parentPos Vec3, parentRot Quat, dst []solidLeaf) []solidLeaf {
	for _, c := range children {
		if c.Trigger {
			continue
		}
		ws := c.WorldShape(parentPos, parentRot)
		if ws.Kind == ShapeCompound {
			dst = collectSolidLeaves(ws.Children, Vec3{}, Quat{}, dst)
			continue
		}
		vol := shapeVolume3D(ws)
		var expM float64
		hasExp := false
		if c.Mass > 0 && isFinite32(c.Mass) {
			expM = float64(c.Mass)
			hasExp = true
		} else if c.Density > 0 && isFinite32(c.Density) && vol > 0 {
			expM = float64(c.Density) * vol
			hasExp = true
		}
		dst = append(dst, solidLeaf{
			shape:        ws,
			centroid:     primitiveCentroid(ws),
			volume:       vol,
			explicitMass: expM,
			hasExplicit:  hasExp,
		})
	}
	return dst
}

func primitiveCentroid(s Shape) Vec3 {
	switch s.Kind {
	case ShapeBox:
		return s.Box.Center
	case ShapeSphere:
		return s.Sphere.Center
	case ShapeCapsule:
		return s.Capsule.A.Add(s.Capsule.B).Scale(0.5)
	case ShapeConvexHull:
		if s.Hull.geom == nil {
			return s.Hull.Center
		}
		c := toCvx(s)
		return c.centroid().vec3()
	case ShapeCylinder:
		return s.Cylinder.Center()
	default:
		return Vec3{}
	}
}

func primitiveLocalInertia(s Shape, m float64) SymMat3 {
	if m <= 0 {
		return SymMat3{}
	}
	switch s.Kind {
	case ShapeBox:
		hx := float64(s.Box.HalfExtents.X)
		hy := float64(s.Box.HalfExtents.Y)
		hz := float64(s.Box.HalfExtents.Z)
		hx2 := float64(hx * hx)
		hy2 := float64(hy * hy)
		hz2 := float64(hz * hz)
		k := m / 3.0
		diag := SymMat3{
			XX: float32(k * (hy2 + hz2)),
			YY: float32(k * (hx2 + hz2)),
			ZZ: float32(k * (hx2 + hy2)),
		}
		if rot, ok := s.orientedBox(); ok {
			return diag.Rotated(rot)
		}
		return diag
	case ShapeSphere:
		r := float64(s.Sphere.Radius)
		is := float32(0.4 * m * r * r)
		return SymMat3{XX: is, YY: is, ZZ: is}
	case ShapeCapsule:
		r := float64(s.Capsule.Radius)
		r2 := float64(r * r)
		seg := s.Capsule.B.Sub(s.Capsule.A)
		lSq := float64(seg.LenSq())
		if lSq <= 1e-12 {
			is := float32(0.4 * m * r2)
			return SymMat3{XX: is, YY: is, ZZ: is}
		}
		l := math.Sqrt(lSq)
		vCyl := float64(math.Pi * r2 * l)
		vSph := float64((4.0 / 3.0) * math.Pi * r2 * r)
		vTot := vCyl + vSph
		if vTot <= 0 {
			is := float32(0.4 * m * r2)
			return SymMat3{XX: is, YY: is, ZZ: is}
		}
		mCyl := m * (vCyl / vTot)
		mSph := m * (vSph / vTot)
		iAxis := float64(mCyl*(0.5*r2)) + float64(mSph*(0.4*r2))
		iPerp := float64(mCyl*(float64(0.25*r2)+lSq/12.0)) +
			float64(mSph*(float64(float64(0.4*r2)+float64(0.25*lSq))+float64(0.375*r*l)))
		invL := 1.0 / l
		ux := float64(seg.X) * invL
		uy := float64(seg.Y) * invL
		uz := float64(seg.Z) * invL
		diff := iAxis - iPerp
		return SymMat3{
			XX: float32(iPerp + float64(diff*float64(ux*ux))),
			YY: float32(iPerp + float64(diff*float64(uy*uy))),
			ZZ: float32(iPerp + float64(diff*float64(uz*uz))),
			XY: float32(diff * float64(ux*uy)),
			XZ: float32(diff * float64(ux*uz)),
			YZ: float32(diff * float64(uy*uz)),
		}
	case ShapeConvexHull:
		g := s.Hull.geom
		if g == nil {
			return SymMat3{}
		}
		loc := SymMat3{
			XX: float32(m * g.inertia[0][0]),
			YY: float32(m * g.inertia[1][1]),
			ZZ: float32(m * g.inertia[2][2]),
			XY: float32(m * g.inertia[0][1]),
			XZ: float32(m * g.inertia[0][2]),
			YZ: float32(m * g.inertia[1][2]),
		}
		if !s.Rot.IsIdentity() {
			return loc.Rotated(s.Rot)
		}
		return loc
	case ShapeCylinder:
		r := float64(s.Cylinder.Radius)
		r2 := float64(r * r)
		seg := s.Cylinder.B.Sub(s.Cylinder.A)
		lSq := float64(seg.LenSq())
		if lSq <= 1e-12 {
			is := float32(0.5 * m * r2)
			return SymMat3{XX: is, YY: is, ZZ: is}
		}
		l := math.Sqrt(lSq)
		iAxis := float64(0.5 * m * r2)
		iPerp := float64(m * (float64(0.25*r2) + lSq/12.0))
		invL := 1.0 / l
		ux := float64(seg.X) * invL
		uy := float64(seg.Y) * invL
		uz := float64(seg.Z) * invL
		diff := iAxis - iPerp
		return SymMat3{
			XX: float32(iPerp + float64(diff*float64(ux*ux))),
			YY: float32(iPerp + float64(diff*float64(uy*uy))),
			ZZ: float32(iPerp + float64(diff*float64(uz*uz))),
			XY: float32(diff * float64(ux*uy)),
			XZ: float32(diff * float64(ux*uz)),
			YZ: float32(diff * float64(uy*uz)),
		}
	default:
		return SymMat3{}
	}
}

// ComputeCompoundMassProperties computes the combined mass, local center of
// mass, and 3x3 symmetric inertia tensor (using the parallel-axis theorem) from
// all non-trigger children.
//
// When totalMass > 0, the returned Mass matches totalMass (distributing mass
// across unspecified children by volume or scaling explicit child masses to
// preserve relative ratios). When totalMass <= 0, Mass is the sum of explicit
// child masses and densities (0 if no child specifies Mass or Density).
func ComputeCompoundMassProperties(children []CompoundChild, totalMass float32) CompoundMassProperties {
	leaves := collectSolidLeaves(children, Vec3{}, Quat{}, nil)
	if len(leaves) == 0 {
		return CompoundMassProperties{}
	}

	var sumExp, sumExpVol, sumUnspecVol float64
	numExp, numUnspec := 0, 0
	for _, lf := range leaves {
		if lf.hasExplicit {
			sumExp += lf.explicitMass
			sumExpVol += lf.volume
			numExp++
		} else {
			sumUnspecVol += lf.volume
			numUnspec++
		}
	}

	masses := make([]float64, len(leaves))
	targetMass := float64(totalMass)
	if !isFinite32(totalMass) || targetMass <= 0 {
		targetMass = 0
	}

	if targetMass > 0 {
		switch {
		case numUnspec == 0:
			scale := targetMass / sumExp
			for i, lf := range leaves {
				masses[i] = lf.explicitMass * scale
			}
		case numExp == 0:
			for i, lf := range leaves {
				if sumUnspecVol > 0 {
					masses[i] = targetMass * (lf.volume / sumUnspecVol)
				} else {
					masses[i] = targetMass / float64(numUnspec)
				}
			}
		default:
			if targetMass > sumExp {
				rem := targetMass - sumExp
				for i, lf := range leaves {
					if lf.hasExplicit {
						masses[i] = lf.explicitMass
					} else if sumUnspecVol > 0 {
						masses[i] = rem * (lf.volume / sumUnspecVol)
					} else {
						masses[i] = rem / float64(numUnspec)
					}
				}
			} else {
				avgDensity := 1.0
				if sumExpVol > 0 {
					avgDensity = sumExp / sumExpVol
				}
				var rawTotal float64
				for i, lf := range leaves {
					if lf.hasExplicit {
						masses[i] = lf.explicitMass
					} else if lf.volume > 0 {
						masses[i] = avgDensity * lf.volume
					} else {
						masses[i] = sumExp / float64(numExp)
					}
					rawTotal += masses[i]
				}
				scale := targetMass / rawTotal
				for i := range masses {
					masses[i] *= scale
				}
			}
		}
	} else {
		if sumExp <= 0 {
			return CompoundMassProperties{}
		}
		avgDensity := 0.0
		if sumExpVol > 0 {
			avgDensity = sumExp / sumExpVol
		}
		for i, lf := range leaves {
			if lf.hasExplicit {
				masses[i] = lf.explicitMass
			} else if avgDensity > 0 && lf.volume > 0 {
				masses[i] = avgDensity * lf.volume
			}
		}
	}

	var totalM, cx, cy, cz float64
	for i, lf := range leaves {
		m := masses[i]
		if m <= 0 {
			continue
		}
		totalM += m
		cx += float64(m * float64(lf.centroid.X))
		cy += float64(m * float64(lf.centroid.Y))
		cz += float64(m * float64(lf.centroid.Z))
	}
	if totalM <= 0 {
		return CompoundMassProperties{}
	}
	com := Vec3{
		X: float32(cx / totalM),
		Y: float32(cy / totalM),
		Z: float32(cz / totalM),
	}

	var ixx, iyy, izz, ixy, ixz, iyz float64
	for i, lf := range leaves {
		m := masses[i]
		if m <= 0 {
			continue
		}
		loc := primitiveLocalInertia(lf.shape, m)
		dx := float64(lf.centroid.X) - float64(com.X)
		dy := float64(lf.centroid.Y) - float64(com.Y)
		dz := float64(lf.centroid.Z) - float64(com.Z)
		dx2 := float64(dx * dx)
		dy2 := float64(dy * dy)
		dz2 := float64(dz * dz)

		ixx += float64(loc.XX) + float64(m*(dy2+dz2))
		iyy += float64(loc.YY) + float64(m*(dx2+dz2))
		izz += float64(loc.ZZ) + float64(m*(dx2+dy2))
		ixy += float64(loc.XY) - float64(m*float64(dx*dy))
		ixz += float64(loc.XZ) - float64(m*float64(dx*dz))
		iyz += float64(loc.YZ) - float64(m*float64(dy*dz))
	}

	minI := 1e-4 * totalM
	if ixx < minI {
		ixx = minI
	}
	if iyy < minI {
		iyy = minI
	}
	if izz < minI {
		izz = minI
	}

	inertia := SymMat3{
		XX: float32(ixx),
		YY: float32(iyy),
		ZZ: float32(izz),
		XY: float32(ixy),
		XZ: float32(ixz),
		YZ: float32(iyz),
	}
	invI, ok := inertia.Inverse()
	if !ok {
		invI = SymMat3{
			XX: float32(1.0 / ixx),
			YY: float32(1.0 / iyy),
			ZZ: float32(1.0 / izz),
		}
	}
	meanI := (ixx + iyy + izz) / 3.0
	if meanI < minI {
		meanI = minI
	}
	return CompoundMassProperties{
		Mass:         float32(totalM),
		CenterOfMass: com,
		Inertia:      inertia,
		InvInertia:   invI,
		ScalarInvI:   float32(1.0 / meanI),
	}
}

func compoundBounds(children []CompoundChild) Box {
	hasAny := false
	var acc Box
	for _, c := range children {
		ls := c.LocalShape()
		if ls.Kind == ShapeCompound && len(ls.Children) == 0 {
			continue
		}
		cb := ls.Bounds()
		if !hasAny {
			acc = cb
			hasAny = true
		} else {
			acc = unionBounds(acc, cb)
		}
	}
	return acc
}

func testCompoundShapes(a, b Shape) Contact {
	var best Contact
	if a.Kind == ShapeCompound {
		for _, ca := range a.Children {
			c := TestShapes(ca.LocalShape(), b)
			if c.Hit && (!best.Hit || c.Depth > best.Depth) {
				best = c
			}
		}
		return best
	}
	if b.Kind == ShapeCompound {
		for _, cb := range b.Children {
			c := TestShapes(a, cb.LocalShape())
			if c.Hit && (!best.Hit || c.Depth > best.Depth) {
				best = c
			}
		}
		return best
	}
	return Contact{}
}

// testShapesSolid tests two shapes for solid overlap while skipping any nested
// CompoundChild marked Trigger == true.
func testShapesSolid(a, b Shape) Contact {
	if a.Kind != ShapeCompound && b.Kind != ShapeCompound {
		return TestShapes(a, b)
	}
	var best Contact
	if a.Kind == ShapeCompound {
		for _, ca := range a.Children {
			if ca.Trigger {
				continue
			}
			c := testShapesSolid(ca.LocalShape(), b)
			if c.Hit && (!best.Hit || c.Depth > best.Depth) {
				best = c
			}
		}
		return best
	}
	for _, cb := range b.Children {
		if cb.Trigger {
			continue
		}
		c := testShapesSolid(a, cb.LocalShape())
		if c.Hit && (!best.Hit || c.Depth > best.Depth) {
			best = c
		}
	}
	return best
}

func shapeHasTriggerChild(s Shape) bool {
	if s.Kind != ShapeCompound {
		return false
	}
	for _, c := range s.Children {
		if c.Trigger || shapeHasTriggerChild(c.Shape) {
			return true
		}
	}
	return false
}

func bodyIsBroadphaseDynamic(b *Body) bool {
	return !b.Static || b.IsTrigger || shapeHasTriggerChild(b.Shape)
}

func raycastCompound(ray Ray, children []CompoundChild) (RaycastHit, bool) {
	var best RaycastHit
	found := false
	for i, c := range children {
		if h, ok := RaycastShape(ray, c.LocalShape()); ok {
			if !found || h.Distance < best.Distance {
				h.ChildIndex = i
				best = h
				found = true
			}
		}
	}
	return best, found
}

// worldChildShape returns the world-space Shape for child idx of b, or b.WorldShape()
// when idx < 0 or b is not a compound body.
func (b *Body) worldChildShape(idx int) Shape {
	if idx < 0 || b.Shape.Kind != ShapeCompound || idx >= len(b.Shape.Children) {
		return b.WorldShape()
	}
	return b.Shape.Children[idx].WorldShape(b.Pos, b.Rot)
}

func (b *Body) childCount() int {
	if b.Shape.Kind == ShapeCompound {
		return len(b.Shape.Children)
	}
	return 1
}

func (b *Body) childEffective(idx int) (layer, mask LayerMask, isTrigger bool) {
	if idx < 0 || b.Shape.Kind != ShapeCompound || idx >= len(b.Shape.Children) {
		return effectiveLayer(b.Layer), effectiveLayer(b.Mask), b.IsTrigger
	}
	c := b.Shape.Children[idx]
	return c.EffectiveLayer(b.Layer), c.EffectiveMask(b.Mask), b.IsTrigger || c.Trigger
}

// activeWorldInvInertia returns b's world-space inverse inertia tensor when b
// is dynamic, awake, rotation-unlocked, and carries a non-zero InvInertiaTensor.
func activeWorldInvInertia(b *Body) (SymMat3, bool) {
	if b == nil || b.Static || b.Kinematic || b.Sleeping || b.LockRotation || b.InvInertiaTensor.IsZero() {
		return SymMat3{}, false
	}
	return b.InvInertiaTensor.Rotated(normalizedQuat(b.Rot)), true
}

func bodyAngularEffectiveMass(b *Body, r, d Vec3) float32 {
	if b == nil {
		return 0
	}
	if invT, ok := activeWorldInvInertia(b); ok {
		return invT.QuadForm(r.Cross(d))
	}
	invI := activeInvInertia(b)
	if invI <= 0 {
		return 0
	}
	return float32(invI * r.Cross(d).LenSq())
}

func contactEffectiveMassBodies(a *Body, rA Vec3, b *Body, rB Vec3, d Vec3) float32 {
	var invA, invB float32
	if a != nil {
		invA = activeInvMass(a)
	}
	if b != nil {
		invB = activeInvMass(b)
	}
	tA := bodyAngularEffectiveMass(a, rA, d)
	tB := bodyAngularEffectiveMass(b, rB, d)
	return float32(float32(invA+invB)+tA) + tB
}

func (b *Body) applyAngularImpulseWorld(torqueImpulse Vec3) {
	if b == nil || b.Static || b.Kinematic || b.LockRotation {
		return
	}
	if !b.InvInertiaTensor.IsZero() {
		invT := b.InvInertiaTensor.Rotated(normalizedQuat(b.Rot))
		b.AngVel = b.AngVel.Add(invT.MulVec(torqueImpulse))
		return
	}
	if b.InvInertia > 0 {
		b.AngVel = b.AngVel.Add(torqueImpulse.Scale(b.InvInertia))
	}
}
