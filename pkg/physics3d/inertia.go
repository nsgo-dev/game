package physics3d

import "math"

// DiagSymMat3 returns a diagonal symmetric 3x3 matrix with entries diag.X, diag.Y, diag.Z.
func DiagSymMat3(diag Vec3) SymMat3 {
	return SymMat3{XX: diag.X, YY: diag.Y, ZZ: diag.Z}
}

// Solve solves the symmetric linear system m * x = rhs in float64 without FMA
// contraction, returning the zero vector when m is singular or non-finite.
func (m SymMat3) Solve(rhs Vec3) Vec3 {
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
		return Vec3{}
	}
	invDet := 1.0 / det
	rx, ry, rz := float64(rhs.X), float64(rhs.Y), float64(rhs.Z)
	out := Vec3{
		X: float32((float64(float64(c00*rx)+float64(c01*ry)) + float64(c02*rz)) * invDet),
		Y: float32((float64(float64(c01*rx)+float64(c11*ry)) + float64(c12*rz)) * invDet),
		Z: float32((float64(float64(c02*rx)+float64(c12*ry)) + float64(c22*rz)) * invDet),
	}
	if !isFiniteVec(out) {
		return Vec3{}
	}
	return out
}

func safeInv32(v float32) float32 {
	if !isFinite32(v) || v <= 0 {
		return 0
	}
	inv := 1 / v
	if !isFinite32(inv) {
		return 0
	}
	return inv
}

func safeInv64To32(v float64) float32 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0
	}
	inv := float32(1.0 / v)
	if !isFinite32(inv) {
		return 0
	}
	return inv
}

// isPositiveDefiniteSymMat3 reports whether m is finite and strictly symmetric
// positive-definite via Sylvester's criterion in FMA-free float64.
func isPositiveDefiniteSymMat3(m SymMat3) bool {
	if !isFiniteSymMat3(m) {
		return false
	}
	a, b, c := float64(m.XX), float64(m.XY), float64(m.XZ)
	d, e, f := float64(m.YY), float64(m.YZ), float64(m.ZZ)
	if a <= 0 || d <= 0 || f <= 0 {
		return false
	}
	if float64(a*d)-float64(b*b) <= 0 {
		return false
	}
	c00 := float64(d*f) - float64(e*e)
	c01 := float64(c*e) - float64(b*f)
	c02 := float64(b*e) - float64(c*d)
	det := float64(float64(a*c00)+float64(b*c01)) + float64(c*c02)
	return !math.IsNaN(det) && !math.IsInf(det, 0) && det > 1e-18
}

// parallelAxisShift shifts a body-space inertia tensor loc (computed about
// centroid) to a new reference point com using the parallel-axis theorem,
// floors the diagonal entries to minI, and returns its inverse.
func invertShiftedInertia(loc SymMat3, m float64, d Vec3) SymMat3 {
	dx, dy, dz := float64(d.X), float64(d.Y), float64(d.Z)
	dx2 := float64(dx * dx)
	dy2 := float64(dy * dy)
	dz2 := float64(dz * dz)

	ixx := float64(loc.XX) + float64(m*(dy2+dz2))
	iyy := float64(loc.YY) + float64(m*(dx2+dz2))
	izz := float64(loc.ZZ) + float64(m*(dx2+dy2))
	ixy := float64(loc.XY) - float64(m*float64(dx*dy))
	ixz := float64(loc.XZ) - float64(m*float64(dx*dz))
	iyz := float64(loc.YZ) - float64(m*float64(dy*dz))

	minI := 1e-4 * m
	if ixx < minI {
		ixx = minI
	}
	if iyy < minI {
		iyy = minI
	}
	if izz < minI {
		izz = minI
	}
	if ixy == 0 && ixz == 0 && iyz == 0 {
		return SymMat3{
			XX: safeInv64To32(ixx),
			YY: safeInv64To32(iyy),
			ZZ: safeInv64To32(izz),
		}
	}
	inertia := SymMat3{
		XX: float32(ixx),
		YY: float32(iyy),
		ZZ: float32(izz),
		XY: float32(ixy),
		XZ: float32(ixz),
		YZ: float32(iyz),
	}
	if isPositiveDefiniteSymMat3(inertia) {
		if inv, ok := inertia.Inverse(); ok {
			return inv
		}
	}
	return SymMat3{
		XX: safeInv64To32(ixx),
		YY: safeInv64To32(iyy),
		ZZ: safeInv64To32(izz),
	}
}

// computeShapeInvInertiaTensor computes the body-space 3x3 inverse inertia
// tensor of shape with mass about local point com. Axis-aligned boxes, spheres,
// and axis-aligned capsules/cylinders produce exact diagonal tensors; rotated,
// offset, convex-hull, and compound shapes produce full symmetric tensors.
func computeShapeInvInertiaTensor(shape Shape, mass float32, com Vec3) SymMat3 {
	if mass <= 0 || !isFinite32(mass) {
		return SymMat3{}
	}
	if shape.Kind == ShapeCompound {
		props := ComputeCompoundMassProperties(shape.Children, mass)
		if props.Mass <= 0 {
			return SymMat3{}
		}
		if com == props.CenterOfMass {
			return props.InvInertia
		}
		return invertShiftedInertia(props.Inertia, float64(props.Mass), props.CenterOfMass.Sub(com))
	}
	m := float64(mass)
	loc := primitiveLocalInertia(shape, m)
	d := primitiveCentroid(shape).Sub(com)
	return invertShiftedInertia(loc, m, d)
}

func scalarInvInertiaFromTensor(invT SymMat3) float32 {
	if invT.IsZero() {
		return 0
	}
	if inertia, ok := invT.Inverse(); ok {
		meanI := (float64(inertia.XX) + float64(inertia.YY) + float64(inertia.ZZ)) / 3.0
		if meanI > 0 && !math.IsNaN(meanI) && !math.IsInf(meanI, 0) {
			return float32(1.0 / meanI)
		}
	}
	var sum float64
	var count int
	if invT.XX > 0 {
		sum += float64(invT.XX)
		count++
	}
	if invT.YY > 0 {
		sum += float64(invT.YY)
		count++
	}
	if invT.ZZ > 0 {
		sum += float64(invT.ZZ)
		count++
	}
	if count == 0 {
		return 0
	}
	return float32(sum / float64(count))
}

// SetCenterOfMass overrides b's local center of mass (relative to b.Pos).
// For dynamic bodies without an explicit inertia override, SetCenterOfMass
// recomputes InvInertiaTensor about the new center of mass via the parallel-axis
// theorem. Calling SetCenterOfMass(Vec3{}) on a compound body pins its center
// of mass to the local origin even when AddBody would otherwise derive it from
// child shapes.
func (b *Body) SetCenterOfMass(com Vec3) {
	if b == nil {
		return
	}
	if !isFiniteVec(com) {
		com = Vec3{}
	}
	b.CenterOfMass = com
	b.customCenterOfMass = true
	if b.Static || b.Kinematic {
		return
	}
	if !b.LockRotation && !b.customInertia && b.Mass > 0 {
		b.InvInertiaTensor = computeShapeInvInertiaTensor(b.Shape, b.Mass, b.CenterOfMass)
		if b.Shape.Kind == ShapeCompound {
			b.InvInertia = scalarInvInertiaFromTensor(b.InvInertiaTensor)
		}
	}
	b.Wake()
}

// SetInertia overrides b's body-space diagonal inertia tensor (Ix, Iy, Iz) about
// CenterOfMass. Each positive finite component sets the inverse inertia along
// that local axis to 1/I; non-positive, non-finite, or overflowing components
// lock rotation about that local axis (inverse inertia 0).
func (b *Body) SetInertia(diag Vec3) {
	if b == nil {
		return
	}
	inv := SymMat3{
		XX: safeInv32(diag.X),
		YY: safeInv32(diag.Y),
		ZZ: safeInv32(diag.Z),
	}
	b.InvInertiaTensor = inv
	b.InvInertia = scalarInvInertiaFromTensor(inv)
	b.customInertia = true
	if !b.Static && !b.Kinematic {
		b.Wake()
	}
}

// SetInertiaTensor overrides b's body-space 3x3 symmetric inertia tensor about
// CenterOfMass. Full inversion requires a symmetric positive-definite tensor;
// if inertia is singular or not positive-definite, positive finite diagonal
// entries are inverted per axis.
func (b *Body) SetInertiaTensor(inertia SymMat3) {
	if b == nil {
		return
	}
	var inv SymMat3
	if isPositiveDefiniteSymMat3(inertia) {
		if m, ok := inertia.Inverse(); ok {
			inv = m
		}
	}
	if inv.IsZero() && isFiniteSymMat3(inertia) {
		inv.XX = safeInv32(inertia.XX)
		inv.YY = safeInv32(inertia.YY)
		inv.ZZ = safeInv32(inertia.ZZ)
	}
	b.InvInertiaTensor = inv
	b.InvInertia = scalarInvInertiaFromTensor(inv)
	b.customInertia = true
	if !b.Static && !b.Kinematic {
		b.Wake()
	}
}

// WorldInvInertiaTensor returns b's world-space 3x3 inverse inertia tensor
// R * InvInertiaTensor * R^T, or the zero matrix when b is nil, static,
// kinematic, or has LockRotation set. When InvInertiaTensor is zero and scalar
// InvInertia > 0, it returns an isotropic diagonal tensor.
func (b *Body) WorldInvInertiaTensor() SymMat3 {
	if b == nil || b.Static || b.Kinematic || b.LockRotation {
		return SymMat3{}
	}
	if !b.InvInertiaTensor.IsZero() {
		return b.InvInertiaTensor.Rotated(normalizedQuat(b.Rot))
	}
	if b.InvInertia > 0 {
		return SymMat3{XX: b.InvInertia, YY: b.InvInertia, ZZ: b.InvInertia}
	}
	return SymMat3{}
}

// activeWorldInvInertiaMatrix returns b's world-space inverse inertia tensor
// for the solver (zero when static, kinematic, sleeping, or rotation-locked).
func activeWorldInvInertiaMatrix(b *Body) SymMat3 {
	if b == nil || b.Sleeping {
		return SymMat3{}
	}
	return b.WorldInvInertiaTensor()
}

// applyBodyPositionCorrection translates b's world center of mass by linDelta
// and rotates b about its center of mass by angDelta (axis * angle), keeping
// b.Pos and b.Rot consistent.
func applyBodyPositionCorrection(b *Body, linDelta, angDelta Vec3) {
	if b == nil || b.Static || b.Kinematic || b.Sleeping {
		return
	}
	if b.CenterOfMass == (Vec3{}) {
		b.Pos = b.Pos.Add(linDelta)
		if angDelta.LenSq() > 0 {
			b.Rot = IntegrateQuat(b.Rot, angDelta, 1)
		}
		return
	}
	comWorld := b.WorldCenterOfMass().Add(linDelta)
	if angDelta.LenSq() > 0 {
		b.Rot = IntegrateQuat(b.Rot, angDelta, 1)
	}
	b.Pos = comWorld.Sub(normalizedQuat(b.Rot).RotateVec(b.CenterOfMass))
}

// sym3d is a float64 symmetric 3x3 matrix used for FMA-free gyroscopic integration.
type sym3d struct {
	xx, yy, zz float64
	xy, xz, yz float64
}

func (m sym3d) mulVec(v dv) dv {
	return dv{
		x: float64(float64(m.xx*v.x)+float64(m.xy*v.y)) + float64(m.xz*v.z),
		y: float64(float64(m.xy*v.x)+float64(m.yy*v.y)) + float64(m.yz*v.z),
		z: float64(float64(m.xz*v.x)+float64(m.yz*v.y)) + float64(m.zz*v.z),
	}
}

func invSymMat3Float64(m SymMat3) (sym3d, bool) {
	a, b, c := float64(m.XX), float64(m.XY), float64(m.XZ)
	d, e, f := float64(m.YY), float64(m.YZ), float64(m.ZZ)
	if b == 0 && c == 0 && e == 0 {
		if a <= 1e-18 || d <= 1e-18 || f <= 1e-18 {
			return sym3d{}, false
		}
		return sym3d{xx: 1.0 / a, yy: 1.0 / d, zz: 1.0 / f}, true
	}
	c00 := float64(d*f) - float64(e*e)
	c01 := float64(c*e) - float64(b*f)
	c02 := float64(b*e) - float64(c*d)
	c11 := float64(a*f) - float64(c*c)
	c12 := float64(b*c) - float64(a*e)
	c22 := float64(a*d) - float64(b*b)
	det := float64(float64(a*c00)+float64(b*c01)) + float64(c*c02)
	if math.IsNaN(det) || math.IsInf(det, 0) || math.Abs(det) <= 1e-18 {
		return sym3d{}, false
	}
	invDet := 1.0 / det
	out := sym3d{
		xx: c00 * invDet,
		yy: c11 * invDet,
		zz: c22 * invDet,
		xy: c01 * invDet,
		xz: c02 * invDet,
		yz: c12 * invDet,
	}
	if math.IsNaN(out.xx) || math.IsNaN(out.yy) || math.IsNaN(out.zz) ||
		math.IsInf(out.xx, 0) || math.IsInf(out.yy, 0) || math.IsInf(out.zz, 0) {
		return sym3d{}, false
	}
	return out, true
}

// solve3x3 solves A * x = b for a general 3x3 matrix A in float64 without FMA.
func solve3x3(a [3][3]float64, b [3]float64) (dv, bool) {
	// Gaussian elimination with partial pivoting.
	for col := range 3 {
		piv := col
		best := math.Abs(a[col][col])
		for row := col + 1; row < 3; row++ {
			if v := math.Abs(a[row][col]); v > best {
				best = v
				piv = row
			}
		}
		if best <= 1e-18 || math.IsNaN(best) {
			return dv{}, false
		}
		if piv != col {
			a[col], a[piv] = a[piv], a[col]
			b[col], b[piv] = b[piv], b[col]
		}
		invP := 1.0 / a[col][col]
		for row := col + 1; row < 3; row++ {
			f := a[row][col] * invP
			for k := col + 1; k < 3; k++ {
				a[row][k] = a[row][k] - float64(f*a[col][k])
			}
			b[row] = b[row] - float64(f*b[col])
		}
	}
	var x [3]float64
	for i := 2; i >= 0; i-- {
		s := b[i]
		for k := i + 1; k < 3; k++ {
			s = s - float64(a[i][k]*x[k])
		}
		x[i] = s / a[i][i]
		if math.IsNaN(x[i]) || math.IsInf(x[i], 0) {
			return dv{}, false
		}
	}
	return dv{x[0], x[1], x[2]}, true
}

// gyroJacobian builds J = I + s * ([w]x * I - [I*w]x) in float64 without FMA.
func gyroJacobian(ib sym3d, w, iw dv, s float64) [3][3]float64 {
	// [w]x * I:
	// row 0: [  0, -wz,  wy] * I
	// row 1: [ wz,   0, -wx] * I
	// row 2: [-wy,  wx,   0] * I
	wi00 := float64(-w.z*ib.xy) + float64(w.y*ib.xz)
	wi01 := float64(-w.z*ib.yy) + float64(w.y*ib.yz)
	wi02 := float64(-w.z*ib.yz) + float64(w.y*ib.zz)

	wi10 := float64(w.z*ib.xx) - float64(w.x*ib.xz)
	wi11 := float64(w.z*ib.xy) - float64(w.x*ib.yz)
	wi12 := float64(w.z*ib.xz) - float64(w.x*ib.zz)

	wi20 := float64(-w.y*ib.xx) + float64(w.x*ib.xy)
	wi21 := float64(-w.y*ib.xy) + float64(w.x*ib.yy)
	wi22 := float64(-w.y*ib.xz) + float64(w.x*ib.yz)

	// -[I*w]x:
	// [  0,   iw.z, -iw.y]
	// [-iw.z,   0,   iw.x]
	// [ iw.y, -iw.x,   0 ]
	return [3][3]float64{
		{ib.xx + float64(s*wi00), ib.xy + float64(s*(wi01+iw.z)), ib.xz + float64(s*(wi02-iw.y))},
		{ib.xy + float64(s*(wi10-iw.z)), ib.yy + float64(s*wi11), ib.yz + float64(s*(wi12+iw.x))},
		{ib.xz + float64(s*(wi20+iw.y)), ib.yz + float64(s*(wi21-iw.x)), ib.zz + float64(s*wi22)},
	}
}

// integrateGyroscopic advances b's angular velocity over dt under Euler's
// torque-free rigid-body equation I * dw/dt + w x (I * w) = 0 using implicit
// midpoint integration (which preserves rotational kinetic energy and angular
// momentum magnitude, producing the intermediate-axis / Dzhanibekov flip for
// asymmetric bodies while leaving isotropic bodies unchanged).
func integrateGyroscopic(b *Body, dt float32) {
	if b.LockRotation || b.InvInertiaTensor.IsZero() || dt <= 0 {
		return
	}
	invT := b.InvInertiaTensor
	// Isotropic tensors have w x (I * w) == 0 identically.
	if invT.XY == 0 && invT.XZ == 0 && invT.YZ == 0 && invT.XX == invT.YY && invT.YY == invT.ZZ {
		return
	}
	ib, ok := invSymMat3Float64(invT)
	if !ok {
		return
	}
	axes := rotAxes(b.Rot)
	wWorld := toDV(b.AngVel)
	// Transform world angular velocity into body space: w0 = R^T * wWorld.
	w0 := dv{wWorld.dot(axes[0]), wWorld.dot(axes[1]), wWorld.dot(axes[2])}
	iw0 := ib.mulVec(w0)
	if w0.cross(iw0).lenSq() <= 1e-24 {
		return
	}

	h := float64(dt)
	ke0 := w0.dot(iw0)

	// Implicit midpoint: R(w) = I*(w - w0) + h * wm x (I * wm) = 0, wm = 0.5*(w0 + w).
	w := w0
	converged := true
	for range 3 {
		wm := w0.add(w).scale(0.5)
		iwm := ib.mulVec(wm)
		r := ib.mulVec(w.sub(w0)).add(wm.cross(iwm).scale(h))
		if r.lenSq() <= 1e-24 {
			break
		}
		jac := gyroJacobian(ib, wm, iwm, 0.5*h)
		dw, solved := solve3x3(jac, [3]float64{-r.x, -r.y, -r.z})
		if !solved {
			converged = false
			break
		}
		w = w.add(dw)
	}

	if !converged || w.dot(ib.mulVec(w)) > float64(ke0*(1+1e-3))+1e-18 {
		// Fall back to one-step implicit Euler, which is unconditionally dissipative.
		f := w0.cross(iw0).scale(h)
		jac := gyroJacobian(ib, w0, iw0, h)
		if dw, solved := solve3x3(jac, [3]float64{-f.x, -f.y, -f.z}); solved {
			w = w0.add(dw)
		} else {
			return
		}
	}

	// Transform body-space angular velocity back to world space: wWorld = R * w.
	wNew := axes[0].scale(w.x).add(axes[1].scale(w.y)).add(axes[2].scale(w.z))
	out := wNew.vec3()
	if isFiniteVec(out) {
		b.AngVel = out
	}
}
