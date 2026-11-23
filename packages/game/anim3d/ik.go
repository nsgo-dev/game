package anim3d

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

// SolveTwoBoneIK analytically solves a two-bone inverse kinematics chain
// (root -> mid -> end) to place the end effector as close as possible to target
// while bending the hinge joint toward the pole target position in the
// root-target-pole plane.
//
// Bone lengths |mid - root| and |end - mid| are preserved exactly. Unreachable
// targets (farther than l1+l2 or closer than |l1-l2|) are clamped cleanly along
// the root-to-target axis without NaN or stretching. When one bone has zero length the other bone alone is aimed at the target; when both do, the chain is returned unchanged.
//
// Returned delta rotations satisfy:
//   - root.Add(rootRot.RotateVec3(mid.Sub(root))) == newMid
//   - newMid.Add(midRot.Mul(rootRot).RotateVec3(end.Sub(mid))) == newEnd
func SolveTwoBoneIK(root, mid, end, target, pole math3d.Vec3) (newMid, newEnd math3d.Vec3, rootRot, midRot math3d.Quat) {
	const eps = 1e-6

	upperVec := mid.Sub(root)
	lowerVec := end.Sub(mid)
	l1 := upperVec.Len()
	l2 := lowerVec.Len()
	toTarget := target.Sub(root)
	dist := toTarget.Len()

	// A zero-length bone leaves a one-bone chain, which still aims at the target instead of ignoring it (#546).
	switch {
	case l1 < eps && l2 < eps:
		return mid, end, math3d.QuatIdentity(), math3d.QuatIdentity()
	case l1 < eps:
		toT := target.Sub(mid)
		if toT.Len() < eps {
			return mid, end, math3d.QuatIdentity(), math3d.QuatIdentity()
		}
		midRot = quatFromTo(lowerVec, toT, arbitraryPerpendicular(lowerVec.Scale(1/l2)))
		return mid, mid.Add(midRot.RotateVec3(lowerVec)), math3d.QuatIdentity(), midRot
	case l2 < eps:
		if dist < eps {
			return mid, end, math3d.QuatIdentity(), math3d.QuatIdentity()
		}
		rootRot = quatFromTo(upperVec, toTarget, arbitraryPerpendicular(upperVec.Scale(1/l1)))
		newMid = root.Add(rootRot.RotateVec3(upperVec))
		return newMid, newMid.Add(rootRot.RotateVec3(lowerVec)), rootRot, math3d.QuatIdentity()
	}

	var dir math3d.Vec3
	if dist < eps {
		dir = upperVec.Scale(1 / l1)
	} else {
		dir = toTarget.Scale(1 / dist)
	}

	// Build unit bend direction perpendicular to dir, pointing toward pole.
	bendDir := orthogonalToward(pole.Sub(root), dir)
	if bendDir.LenSq() < eps*eps {
		bendDir = orthogonalToward(upperVec, dir)
	}
	if bendDir.LenSq() < eps*eps {
		bendDir = arbitraryPerpendicular(dir)
	}
	hingeAxis := dir.Cross(bendDir).Norm()

	minDist := float32(math.Abs(float64(l1 - l2)))
	maxDist := l1 + l2
	d := math3d.Clamp(dist, minDist, maxDist)

	if d < eps {
		// l1 ≈ l2 and target ≈ root: fold completely toward pole.
		newMid = root.Add(bendDir.Scale(l1))
		newEnd = newMid.Sub(bendDir.Scale(l2))
	} else {
		// Law of cosines for angle at root between dir and upper bone.
		cosA := (l1*l1 + d*d - l2*l2) / (2 * l1 * d)
		cosA = math3d.Clamp(cosA, -1, 1)
		sinA := float32(math.Sqrt(math.Max(0, 1-float64(cosA*cosA))))

		newMid = root.Add(dir.Scale(l1 * cosA)).Add(bendDir.Scale(l1 * sinA))
		newEnd = root.Add(dir.Scale(d))
	}

	newUpper := newMid.Sub(root)
	newLower := newEnd.Sub(newMid)

	rootRot = quatFromTo(upperVec, newUpper, hingeAxis)
	rotatedLower := rootRot.RotateVec3(lowerVec)
	midRot = quatFromTo(rotatedLower, newLower, hingeAxis)
	return newMid, newEnd, rootRot, midRot
}

// SolveFABRIK solves a multi-joint inverse kinematics chain using the iterative
// FABRIK (Forward And Backward Reaching Inverse Kinematics) algorithm.
//
// The root joint joints[0] stays fixed and every bone segment length
// |joints[i+1] - joints[i]| is preserved exactly. The input slice is not mutated.
// If iterations <= 0 it defaults to 10; if tolerance <= 0 it defaults to 1e-4.
func SolveFABRIK(joints []math3d.Vec3, target math3d.Vec3, iterations int, tolerance float32) []math3d.Vec3 {
	n := len(joints)
	if n == 0 {
		return nil
	}
	out := make([]math3d.Vec3, n)
	copy(out, joints)
	if n == 1 {
		return out
	}

	if iterations <= 0 {
		iterations = 10
	}
	if tolerance <= 0 {
		tolerance = 1e-4
	}

	lengths := make([]float32, n-1)
	fallbackDirs := make([]math3d.Vec3, n-1)
	var totalLen float32
	for i := range n - 1 {
		seg := joints[i+1].Sub(joints[i])
		l := seg.Len()
		lengths[i] = l
		totalLen += l
		if l > 1e-7 {
			fallbackDirs[i] = seg.Scale(1 / l)
		} else {
			fallbackDirs[i] = math3d.Vec3Up()
		}
	}

	origin := joints[0]
	toTarget := target.Sub(origin)
	distToTarget := toTarget.Len()

	// Unreachable target: stretch straight toward target preserving exact bone lengths.
	if distToTarget >= totalLen {
		var dir math3d.Vec3
		if distToTarget > 1e-7 {
			dir = toTarget.Scale(1 / distToTarget)
		} else {
			dir = fallbackDirs[0]
		}
		out[0] = origin
		for i := range n - 1 {
			out[i+1] = out[i].Add(dir.Scale(lengths[i]))
		}
		return out
	}

	// Break collinearity singularity if the initial chain is straight along the
	// origin-to-target line and target lies inside reach.
	if n >= 3 && out[n-1].Dist(target) > tolerance && isChainCollinear(out, target) {
		var axis math3d.Vec3
		if distToTarget > 1e-7 {
			axis = toTarget.Scale(1 / distToTarget)
		} else {
			axis = fallbackDirs[0]
		}
		perp := arbitraryPerpendicular(axis)
		nudge := totalLen * 0.05
		for i := 1; i < n-1; i++ {
			out[i] = out[i].Add(perp.Scale(nudge))
		}
	}

	for range iterations {
		if out[n-1].Dist(target) <= tolerance {
			break
		}

		// Backward pass: pull end effector to target and propagate to root.
		out[n-1] = target
		for i := n - 2; i >= 0; i-- {
			d := out[i].Dist(out[i+1])
			if d < 1e-7 {
				out[i] = out[i+1].Sub(fallbackDirs[i].Scale(lengths[i]))
			} else {
				lambda := lengths[i] / d
				out[i] = out[i+1].Add(out[i].Sub(out[i+1]).Scale(lambda))
			}
		}

		// Forward pass: re-anchor root and propagate to end effector.
		out[0] = origin
		for i := range n - 1 {
			d := out[i+1].Dist(out[i])
			if d < 1e-7 {
				out[i+1] = out[i].Add(fallbackDirs[i].Scale(lengths[i]))
			} else {
				lambda := lengths[i] / d
				out[i+1] = out[i].Add(out[i+1].Sub(out[i]).Scale(lambda))
			}
		}
	}

	return out
}

func orthogonalToward(v, unitAxis math3d.Vec3) math3d.Vec3 {
	perp := v.Sub(unitAxis.Scale(v.Dot(unitAxis)))
	l := perp.Len()
	if l < 1e-6 {
		return math3d.Vec3{}
	}
	return perp.Scale(1 / l)
}

func arbitraryPerpendicular(unitAxis math3d.Vec3) math3d.Vec3 {
	ref := math3d.Vec3Up()
	if math.Abs(float64(unitAxis.Dot(ref))) > 0.9 {
		ref = math3d.Vec3Right()
	}
	return unitAxis.Cross(ref).Norm()
}

func quatFromTo(from, to, fallbackAxis math3d.Vec3) math3d.Quat {
	a := from.Norm()
	b := to.Norm()
	if a.LenSq() < 1e-12 || b.LenSq() < 1e-12 {
		return math3d.QuatIdentity()
	}
	dot := a.Dot(b)
	if dot >= 1-1e-6 {
		return math3d.QuatIdentity()
	}
	if dot <= -1+1e-6 {
		axis := fallbackAxis
		if axis.LenSq() < 1e-12 || math.Abs(float64(axis.Norm().Dot(a))) > 0.99 {
			axis = arbitraryPerpendicular(a)
		}
		return math3d.QuatFromAxisAngle(axis, math.Pi)
	}
	c := a.Cross(b)
	return math3d.Quat{
		X: c.X,
		Y: c.Y,
		Z: c.Z,
		W: 1 + dot,
	}.Norm()
}

func isChainCollinear(joints []math3d.Vec3, target math3d.Vec3) bool {
	origin := joints[0]
	var axis math3d.Vec3
	for i := 1; i < len(joints); i++ {
		if d := joints[i].Sub(origin); d.LenSq() > 1e-10 {
			axis = d.Norm()
			break
		}
	}
	if axis.LenSq() < 1e-10 {
		return false
	}
	check := func(p math3d.Vec3) bool {
		v := p.Sub(origin)
		perp := v.Sub(axis.Scale(v.Dot(axis)))
		return perp.LenSq() <= 1e-6
	}
	if !check(target) {
		return false
	}
	for i := 1; i < len(joints); i++ {
		if !check(joints[i]) {
			return false
		}
	}
	return true
}
