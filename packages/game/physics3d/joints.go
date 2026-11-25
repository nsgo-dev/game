package physics3d

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/internal/detmath"
)

// Joint3D constrains the relative motion of one or two bodies inside a World.
// Either body may be nil to anchor the constraint directly to the world frame.
type Joint3D interface {
	// Bodies returns the pair of bodies connected by the joint.
	Bodies() (*Body, *Body)
	// Prepare updates per-step cached values before velocity iterations.
	Prepare(dt float32)
	// SolveVelocity applies one sequential-impulse velocity iteration.
	SolveVelocity(dt float32)
	// SolvePosition applies one non-linear Gauss-Seidel positional and angular
	// correction iteration.
	SolvePosition(dt float32)
}

type collideConnectedJoint interface {
	collideConnected() bool
}

type breakableJoint interface {
	checkBreak() (JointBreakEvent, bool)
}

// JointBreakEvent reports a joint that broke during World.Step because its
// reaction force or torque exceeded its BreakForce or BreakTorque threshold.
type JointBreakEvent struct {
	Joint  Joint3D
	BodyA  *Body
	BodyB  *Body
	Force  Vec3
	Torque Vec3
}

// jointReaction tracks per-step accumulated constraint impulses and broken state
// shared by all built-in Joint3D implementations.
type jointReaction struct {
	stepLinearImpulse  Vec3
	stepAngularImpulse Vec3
	lastDt             float32
	broken             bool
}

func (r *jointReaction) resetStepReaction(dt float32) {
	r.stepLinearImpulse = Vec3{}
	r.stepAngularImpulse = Vec3{}
	r.lastDt = dt
}

func (r *jointReaction) addLinearImpulse(imp Vec3) {
	r.stepLinearImpulse = r.stepLinearImpulse.Add(imp)
}

func (r *jointReaction) addAngularImpulse(imp Vec3) {
	r.stepAngularImpulse = r.stepAngularImpulse.Add(imp)
}

// ReactionForce returns the world-space constraint force (in Newtons) applied to
// BodyB during the most recent World.Step.
func (r *jointReaction) ReactionForce() Vec3 {
	if r.lastDt <= 0 {
		return Vec3{}
	}
	return r.stepLinearImpulse.Scale(1 / r.lastDt)
}

// ReactionTorque returns the world-space constraint torque (in N*m) applied to
// BodyB during the most recent World.Step.
func (r *jointReaction) ReactionTorque() Vec3 {
	if r.lastDt <= 0 {
		return Vec3{}
	}
	return r.stepAngularImpulse.Scale(1 / r.lastDt)
}

// Broken reports whether the joint has exceeded its break threshold and been removed.
func (r *jointReaction) Broken() bool {
	return r.broken
}

func (r *jointReaction) evalBreak(j Joint3D, breakForce, breakTorque float32) (JointBreakEvent, bool) {
	if r.broken || r.lastDt <= 0 {
		return JointBreakEvent{}, false
	}
	if breakForce <= 0 && breakTorque <= 0 {
		return JointBreakEvent{}, false
	}
	force := r.ReactionForce()
	torque := r.ReactionTorque()
	exceeded := (breakForce > 0 && force.LenSq() > breakForce*breakForce) ||
		(breakTorque > 0 && torque.LenSq() > breakTorque*breakTorque)
	if !exceeded {
		return JointBreakEvent{}, false
	}
	r.broken = true
	a, b := j.Bodies()
	return JointBreakEvent{
		Joint:  j,
		BodyA:  a,
		BodyB:  b,
		Force:  force,
		Torque: torque,
	}, true
}

// softSpringParams computes implicit Euler ERP/CFM (gamma, bias) parameters for
// a 1D spring-damper with stiffness k >= 0, damping c >= 0, position error err,
// and step duration dt > 0.
func softSpringParams(stiffness, damping, err, dt float32) (gamma, bias float32, active bool) {
	if dt <= 0 {
		return 0, 0, false
	}
	k := max(stiffness, 0)
	c := max(damping, 0)
	denom := c + float32(dt*k)
	if denom <= 1e-8 {
		return 0, 0, false
	}
	gamma = 1 / (dt * denom)
	beta := (dt * k) / denom
	bias = beta * err / dt
	return gamma, bias, true
}

// AddJoint registers j in w so it is solved during World.Step and returns j.
func (w *World) AddJoint(j Joint3D) Joint3D {
	if j == nil {
		return nil
	}
	w.joints = append(w.joints, j)
	a, b := j.Bodies()
	if a != nil && !a.Static && !a.Kinematic {
		a.Wake()
	}
	if b != nil && !b.Static && !b.Kinematic {
		b.Wake()
	}
	return j
}

// RemoveJoint removes j from w.
func (w *World) RemoveJoint(j Joint3D) bool {
	for i, existing := range w.joints {
		if existing == j {
			w.joints = append(w.joints[:i], w.joints[i+1:]...)
			w.wakeAllDynamic()
			return true
		}
	}
	return false
}

// Joints returns all joints currently registered in w in insertion order.
func (w *World) Joints() []Joint3D {
	return w.joints
}

func (w *World) removeJointsForBody(id int) {
	if len(w.joints) == 0 {
		return
	}
	dst := w.joints[:0]
	for _, j := range w.joints {
		a, b := j.Bodies()
		if (a != nil && a.ID == id) || (b != nil && b.ID == id) {
			continue
		}
		dst = append(dst, j)
	}
	w.joints = dst
}

func (w *World) buildJointIgnoredPairs() map[pairKey]struct{} {
	if len(w.joints) == 0 {
		return nil
	}
	var pairs map[pairKey]struct{}
	for _, j := range w.joints {
		if cc, ok := j.(collideConnectedJoint); ok && cc.collideConnected() {
			continue
		}
		a, b := j.Bodies()
		if a == nil || b == nil || a.ID == b.ID {
			continue
		}
		if pairs == nil {
			pairs = make(map[pairKey]struct{}, len(w.joints))
		}
		k, _ := canonicalPairKey(a.ID, b.ID)
		pairs[k] = struct{}{}
	}
	return pairs
}

// symMat3 stores a symmetric 3x3 matrix:
//
//	[ a  d  e ]
//	[ d  b  f ]
//	[ e  f  c ]
type symMat3 struct {
	a, b, c float32
	d, e, f float32
}

func pointConstraintMassMatrix(invMA, invIA float32, rA Vec3, invMB, invIB float32, rB Vec3) symMat3 {
	invM := invMA + invMB
	return symMat3{
		a: float32(invM+float32(invIA*(float32(rA.Y*rA.Y)+float32(rA.Z*rA.Z)))) + float32(invIB*(float32(rB.Y*rB.Y)+float32(rB.Z*rB.Z))),
		b: float32(invM+float32(invIA*(float32(rA.X*rA.X)+float32(rA.Z*rA.Z)))) + float32(invIB*(float32(rB.X*rB.X)+float32(rB.Z*rB.Z))),
		c: float32(invM+float32(invIA*(float32(rA.X*rA.X)+float32(rA.Y*rA.Y)))) + float32(invIB*(float32(rB.X*rB.X)+float32(rB.Y*rB.Y))),
		d: float32(float32(-invIA*rA.X)*rA.Y) - float32(float32(invIB*rB.X)*rB.Y),
		e: float32(float32(-invIA*rA.X)*rA.Z) - float32(float32(invIB*rB.X)*rB.Z),
		f: float32(float32(-invIA*rA.Y)*rA.Z) - float32(float32(invIB*rB.Y)*rB.Z),
	}
}

func (m symMat3) solve(rhs Vec3) Vec3 {
	c00 := float32(m.b*m.c) - float32(m.f*m.f)
	c01 := float32(m.e*m.f) - float32(m.d*m.c)
	c02 := float32(m.d*m.f) - float32(m.e*m.b)
	c11 := float32(m.a*m.c) - float32(m.e*m.e)
	c12 := float32(m.d*m.e) - float32(m.a*m.f)
	c22 := float32(m.a*m.b) - float32(m.d*m.d)
	det := float32(float32(m.a*c00)+float32(m.d*c01)) + float32(m.e*c02)
	if abs32(det) <= 1e-12 {
		return Vec3{}
	}
	invDet := 1 / det
	return Vec3{
		X: (float32(float32(c00*rhs.X)+float32(c01*rhs.Y)) + float32(c02*rhs.Z)) * invDet,
		Y: (float32(float32(c01*rhs.X)+float32(c11*rhs.Y)) + float32(c12*rhs.Z)) * invDet,
		Z: (float32(float32(c02*rhs.X)+float32(c12*rhs.Y)) + float32(c22*rhs.Z)) * invDet,
	}
}

func bodyKinematics(b *Body) (pos Vec3, rot Quat, vel, angVel Vec3, invM, invI float32) {
	if b == nil {
		return Vec3{}, QuatIdentity(), Vec3{}, Vec3{}, 0, 0
	}
	return b.Pos, normalizedQuat(b.Rot), b.Vel, b.AngVel, activeInvMass(b), activeInvInertia(b)
}

func wakeIfDynamic(b *Body) {
	if b != nil && !b.Static && !b.Kinematic {
		b.Wake()
	}
}

func applyAnchorImpulse(a, b *Body, rA, rB, impulse Vec3) {
	if a != nil {
		invM := activeInvMass(a)
		invI := activeInvInertia(a)
		if invM > 0 {
			a.Vel = a.Vel.Sub(impulse.Scale(invM))
		}
		if invI > 0 {
			a.AngVel = a.AngVel.Sub(rA.Cross(impulse).Scale(invI))
		}
	}
	if b != nil {
		invM := activeInvMass(b)
		invI := activeInvInertia(b)
		if invM > 0 {
			b.Vel = b.Vel.Add(impulse.Scale(invM))
		}
		if invI > 0 {
			b.AngVel = b.AngVel.Add(rB.Cross(impulse).Scale(invI))
		}
	}
}

func applyAngularImpulsePair(a, b *Body, impulse Vec3) {
	if a != nil {
		if invI := activeInvInertia(a); invI > 0 {
			a.AngVel = a.AngVel.Sub(impulse.Scale(invI))
		}
	}
	if b != nil {
		if invI := activeInvInertia(b); invI > 0 {
			b.AngVel = b.AngVel.Add(impulse.Scale(invI))
		}
	}
}

func applyAnchorPositionCorrection(a, b *Body, rA, rB, corr Vec3) {
	if a != nil {
		invM := activeInvMass(a)
		invI := activeInvInertia(a)
		if invM > 0 {
			a.Pos = a.Pos.Sub(corr.Scale(invM))
		}
		if invI > 0 {
			dTheta := rA.Cross(corr).Scale(-invI)
			a.Rot = IntegrateQuat(a.Rot, dTheta, 1)
		}
	}
	if b != nil {
		invM := activeInvMass(b)
		invI := activeInvInertia(b)
		if invM > 0 {
			b.Pos = b.Pos.Add(corr.Scale(invM))
		}
		if invI > 0 {
			dTheta := rB.Cross(corr).Scale(invI)
			b.Rot = IntegrateQuat(b.Rot, dTheta, 1)
		}
	}
}

func applyAngularPositionCorrection(a, b *Body, axis Vec3, angle float32) {
	invIA := float32(0)
	invIB := float32(0)
	if a != nil {
		invIA = activeInvInertia(a)
	}
	if b != nil {
		invIB = activeInvInertia(b)
	}
	invISum := invIA + invIB
	if invISum <= 0 || abs32(angle) <= 1e-7 {
		return
	}
	if invIA > 0 {
		a.Rot = IntegrateQuat(a.Rot, axis.Scale(-angle*(invIA/invISum)), 1)
	}
	if invIB > 0 {
		b.Rot = IntegrateQuat(b.Rot, axis.Scale(angle*(invIB/invISum)), 1)
	}
}

func solvePointAnchorVelocity(a, b *Body, localAnchorA, localAnchorB Vec3) Vec3 {
	_, rotA, velA, angA, invMA, invIA := bodyKinematics(a)
	_, rotB, velB, angB, invMB, invIB := bodyKinematics(b)
	if invMA+invMB+invIA+invIB == 0 {
		return Vec3{}
	}
	rA := rotA.RotateVec(localAnchorA)
	rB := rotB.RotateVec(localAnchorB)
	vA := velA.Add(angA.Cross(rA))
	vB := velB.Add(angB.Cross(rB))
	vRel := vB.Sub(vA)
	k := pointConstraintMassMatrix(invMA, invIA, rA, invMB, invIB, rB)
	j := k.solve(vRel.Neg())
	applyAnchorImpulse(a, b, rA, rB, j)
	return j
}

func solvePointAnchorPosition(a, b *Body, localAnchorA, localAnchorB Vec3, stiffness float32) {
	posA, rotA, _, _, invMA, invIA := bodyKinematics(a)
	posB, rotB, _, _, invMB, invIB := bodyKinematics(b)
	if invMA+invMB+invIA+invIB == 0 {
		return
	}
	rA := rotA.RotateVec(localAnchorA)
	rB := rotB.RotateVec(localAnchorB)
	err := posB.Add(rB).Sub(posA.Add(rA))
	if err.LenSq() <= 1e-10 {
		return
	}
	k := pointConstraintMassMatrix(invMA, invIA, rA, invMB, invIB, rB)
	p := k.solve(err.Scale(-stiffness))
	applyAnchorPositionCorrection(a, b, rA, rB, p)
}

func solveFullOrientationVelocity(a, b *Body) Vec3 {
	_, _, _, angA, _, invIA := bodyKinematics(a)
	_, _, _, angB, _, invIB := bodyKinematics(b)
	invISum := invIA + invIB
	if invISum <= 0 {
		return Vec3{}
	}
	wRel := angB.Sub(angA)
	imp := wRel.Scale(-1 / invISum)
	applyAngularImpulsePair(a, b, imp)
	return imp
}

func solveFullOrientationPosition(a, b *Body, refRelRot Quat, stiffness float32) {
	_, rotA, _, _, _, invIA := bodyKinematics(a)
	_, rotB, _, _, _, invIB := bodyKinematics(b)
	invISum := invIA + invIB
	if invISum <= 0 {
		return
	}
	targetB := rotA.Mul(normalizedQuat(refRelRot)).Norm()
	qErr := targetB.Mul(rotB.Conjugate()).Norm()
	axis, angle := qErr.ToAxisAngle()
	if angle <= 1e-6 {
		return
	}
	applyAngularPositionCorrection(a, b, axis, angle*stiffness)
}

// DistanceJoint constrains the distance between anchor points on BodyA and BodyB within [MinDistance, MaxDistance] with optional spring stiffness and damping.
//
// The joint is slack while the distance is inside the range: Stiffness and Damping soften the limits rather than pull toward a rest length. For a spring with a rest length L, set MinDistance and MaxDistance both to L; for a soft rope, set MinDistance to 0.
type DistanceJoint struct {
	BodyA, BodyB     *Body
	LocalAnchorA     Vec3
	LocalAnchorB     Vec3
	MinDistance      float32
	MaxDistance      float32
	Stiffness        float32 // 0 = rigid constraint; > 0 = soft spring stiffness
	Damping          float32 // >= 0 damping coefficient
	BreakForce       float32 // max reaction force in N before breaking (<= 0 disables)
	BreakTorque      float32 // max reaction torque in N*m before breaking (<= 0 disables)
	CollideConnected bool

	jointReaction
	accImpulse float32
	mode       int // 0 = slack, 1 = locked, 2 = min limit, 3 = max limit
	targetDist float32
	gamma      float32
	bias       float32
}

// NewDistanceJoint creates a DistanceJoint between a and b. If maxDist < minDist,
// maxDist is set equal to minDist (a fixed-length rod).
func NewDistanceJoint(a, b *Body, localAnchorA, localAnchorB Vec3, minDist, maxDist float32) *DistanceJoint {
	if minDist < 0 {
		minDist = 0
	}
	if maxDist < minDist {
		maxDist = minDist
	}
	return &DistanceJoint{
		BodyA:        a,
		BodyB:        b,
		LocalAnchorA: localAnchorA,
		LocalAnchorB: localAnchorB,
		MinDistance:  minDist,
		MaxDistance:  maxDist,
	}
}

func (j *DistanceJoint) collideConnected() bool { return j.CollideConnected }

func (j *DistanceJoint) checkBreak() (JointBreakEvent, bool) {
	return j.evalBreak(j, j.BreakForce, j.BreakTorque)
}

// Bodies returns the connected bodies.
func (j *DistanceJoint) Bodies() (*Body, *Body) { return j.BodyA, j.BodyB }

func (j *DistanceJoint) effectiveLimits() (float32, float32) {
	lo := j.MinDistance
	if lo < 0 {
		lo = 0
	}
	hi := j.MaxDistance
	if hi < lo {
		hi = lo
	}
	return lo, hi
}

// Prepare computes the active distance mode and spring ERP/CFM parameters for dt.
func (j *DistanceJoint) Prepare(dt float32) {
	j.resetStepReaction(dt)
	j.accImpulse = 0
	j.mode = 0
	j.gamma = 0
	j.bias = 0

	posA, rotA, _, _, invMA, invIA := bodyKinematics(j.BodyA)
	posB, rotB, _, _, invMB, invIB := bodyKinematics(j.BodyB)
	if invMA+invMB+invIA+invIB == 0 {
		return
	}

	rA := rotA.RotateVec(j.LocalAnchorA)
	rB := rotB.RotateVec(j.LocalAnchorB)
	dist := posB.Add(rB).Sub(posA.Add(rA)).Len()
	lo, hi := j.effectiveLimits()

	if hi <= lo+1e-6 {
		j.mode = 1
		j.targetDist = lo
	} else if dist < lo {
		j.mode = 2
		j.targetDist = lo
	} else if dist > hi {
		j.mode = 3
		j.targetDist = hi
	} else {
		return
	}

	if j.Stiffness > 0 && dt > 0 {
		k := j.Stiffness
		c := max(j.Damping, 0)
		denom := c + float32(dt*k)
		if denom > 1e-8 {
			j.gamma = 1 / (dt * denom)
			beta := (dt * k) / denom
			j.bias = beta * (dist - j.targetDist) / dt
		}
	}
}

// SolveVelocity applies one velocity iteration along the anchor axis.
func (j *DistanceJoint) SolveVelocity(dt float32) {
	if j.mode == 0 {
		return
	}
	posA, rotA, velA, angA, invMA, invIA := bodyKinematics(j.BodyA)
	posB, rotB, velB, angB, invMB, invIB := bodyKinematics(j.BodyB)
	if invMA+invMB+invIA+invIB == 0 {
		return
	}

	rA := rotA.RotateVec(j.LocalAnchorA)
	rB := rotB.RotateVec(j.LocalAnchorB)
	diff := posB.Add(rB).Sub(posA.Add(rA))
	dist := diff.Len()
	var n Vec3
	if dist > 1e-6 {
		n = diff.Scale(1 / dist)
	} else {
		n = Vec3{Y: -1}
	}

	rAxn := rA.Cross(n)
	rBxn := rB.Cross(n)
	w := float32(float32(invMA+invMB)+float32(invIA*rAxn.LenSq())) + float32(invIB*rBxn.LenSq())
	if w <= 1e-12 {
		return
	}

	vA := velA.Add(angA.Cross(rA))
	vB := velB.Add(angB.Cross(rB))
	vRelN := vB.Sub(vA).Dot(n)

	var dLambda float32
	if j.Stiffness > 0 {
		dLambda = -(float32(vRelN+j.bias) + float32(j.gamma*j.accImpulse)) / (w + j.gamma)
	} else {
		dampBias := float32(0)
		if j.Damping > 0 && dt > 0 {
			// Gentle velocity damping along the distance axis.
			dampFactor := clamp32(j.Damping*dt, 0, 1)
			dampBias = vRelN * dampFactor
		}
		dLambda = -(vRelN + dampBias) / w
	}

	newAcc := j.accImpulse + dLambda
	switch j.mode {
	case 2: // Min limit: push apart only (positive impulse on B along n).
		if newAcc < 0 {
			newAcc = 0
		}
	case 3: // Max limit: pull together only (negative impulse on B along n).
		if newAcc > 0 {
			newAcc = 0
		}
	}
	applied := newAcc - j.accImpulse
	j.accImpulse = newAcc
	if applied != 0 {
		imp := n.Scale(applied)
		applyAnchorImpulse(j.BodyA, j.BodyB, rA, rB, imp)
		j.addLinearImpulse(imp)
	}
}

// SolvePosition corrects distance drift for rigid (Stiffness <= 0) DistanceJoints.
func (j *DistanceJoint) SolvePosition(dt float32) {
	_ = dt
	if j.Stiffness > 0 {
		return
	}
	posA, rotA, _, _, invMA, invIA := bodyKinematics(j.BodyA)
	posB, rotB, _, _, invMB, invIB := bodyKinematics(j.BodyB)
	if invMA+invMB+invIA+invIB == 0 {
		return
	}
	rA := rotA.RotateVec(j.LocalAnchorA)
	rB := rotB.RotateVec(j.LocalAnchorB)
	diff := posB.Add(rB).Sub(posA.Add(rA))
	dist := diff.Len()
	lo, hi := j.effectiveLimits()

	var err float32
	if hi <= lo+1e-6 {
		err = dist - lo
	} else if dist < lo {
		err = dist - lo
	} else if dist > hi {
		err = dist - hi
	} else {
		return
	}
	if abs32(err) <= 1e-5 {
		return
	}

	var n Vec3
	if dist > 1e-6 {
		n = diff.Scale(1 / dist)
	} else {
		n = Vec3{Y: -1}
	}
	rAxn := rA.Cross(n)
	rBxn := rB.Cross(n)
	w := float32(float32(invMA+invMB)+float32(invIA*rAxn.LenSq())) + float32(invIB*rBxn.LenSq())
	if w <= 1e-12 {
		return
	}
	corr := n.Scale(-err * 0.8 / w)
	applyAnchorPositionCorrection(j.BodyA, j.BodyB, rA, rB, corr)
}

// BallJoint is a 3D ball-and-socket point constraint with an optional symmetric
// cone swing limit.
type BallJoint struct {
	BodyA, BodyB     *Body
	LocalAnchorA     Vec3
	LocalAnchorB     Vec3
	ConeLimit        float32 // maximum half-angle in radians (> 0 enables cone limit)
	LocalAxisA       Vec3    // cone reference axis in A's local space (defaults to +Y)
	LocalAxisB       Vec3    // bone/body axis in B's local space (defaults to +Y)
	BreakForce       float32 // max reaction force in N before breaking (<= 0 disables)
	BreakTorque      float32 // max reaction torque in N*m before breaking (<= 0 disables)
	CollideConnected bool

	jointReaction
	swingImpulse float32
}

// NewBallJoint creates a BallJoint locking LocalAnchorA on a to LocalAnchorB on b.
func NewBallJoint(a, b *Body, localAnchorA, localAnchorB Vec3) *BallJoint {
	return &BallJoint{
		BodyA:        a,
		BodyB:        b,
		LocalAnchorA: localAnchorA,
		LocalAnchorB: localAnchorB,
		LocalAxisA:   Vec3{Y: 1},
		LocalAxisB:   Vec3{Y: 1},
	}
}

func (j *BallJoint) collideConnected() bool { return j.CollideConnected }

func (j *BallJoint) checkBreak() (JointBreakEvent, bool) {
	return j.evalBreak(j, j.BreakForce, j.BreakTorque)
}

// Bodies returns the connected bodies.
func (j *BallJoint) Bodies() (*Body, *Body) { return j.BodyA, j.BodyB }

func (j *BallJoint) localAxes() (Vec3, Vec3) {
	axA := j.LocalAxisA.Norm()
	if axA.LenSq() == 0 {
		axA = Vec3{Y: 1}
	}
	axB := j.LocalAxisB.Norm()
	if axB.LenSq() == 0 {
		axB = axA
	}
	return axA, axB
}

// Prepare resets per-step accumulated limit impulses.
func (j *BallJoint) Prepare(dt float32) {
	j.resetStepReaction(dt)
	j.swingImpulse = 0
}

// SolveVelocity solves the 3D socket point constraint and optional cone swing limit.
func (j *BallJoint) SolveVelocity(dt float32) {
	_ = dt
	j.addLinearImpulse(solvePointAnchorVelocity(j.BodyA, j.BodyB, j.LocalAnchorA, j.LocalAnchorB))

	if j.ConeLimit <= 0 {
		return
	}
	_, rotA, _, angA, _, invIA := bodyKinematics(j.BodyA)
	_, rotB, _, angB, _, invIB := bodyKinematics(j.BodyB)
	invISum := invIA + invIB
	if invISum <= 0 {
		return
	}

	axA, axB := j.localAxes()
	wA := rotA.RotateVec(axA)
	wB := rotB.RotateVec(axB)
	dot := clamp32(wA.Dot(wB), -1, 1)
	theta := float32(detmath.Acos(float64(dot)))
	if theta <= j.ConeLimit {
		return
	}

	swingAxis := wA.Cross(wB).Norm()
	if swingAxis.LenSq() == 0 {
		swingAxis, _ = orthonormalBasis(wA)
	}
	// Positive relative angular velocity around swingAxis increases theta.
	wRel := angB.Sub(angA).Dot(swingAxis)
	dLambda := -wRel / invISum
	newAcc := j.swingImpulse + dLambda
	if newAcc > 0 {
		newAcc = 0
	}
	applied := newAcc - j.swingImpulse
	j.swingImpulse = newAcc
	if applied != 0 {
		imp := swingAxis.Scale(applied)
		applyAngularImpulsePair(j.BodyA, j.BodyB, imp)
		j.addAngularImpulse(imp)
	}
}

// SolvePosition corrects socket anchor drift and optional cone swing violations.
func (j *BallJoint) SolvePosition(dt float32) {
	_ = dt
	solvePointAnchorPosition(j.BodyA, j.BodyB, j.LocalAnchorA, j.LocalAnchorB, 0.8)

	if j.ConeLimit <= 0 {
		return
	}
	_, rotA, _, _, _, invIA := bodyKinematics(j.BodyA)
	_, rotB, _, _, _, invIB := bodyKinematics(j.BodyB)
	if invIA+invIB <= 0 {
		return
	}
	axA, axB := j.localAxes()
	wA := rotA.RotateVec(axA)
	wB := rotB.RotateVec(axB)
	dot := clamp32(wA.Dot(wB), -1, 1)
	theta := float32(detmath.Acos(float64(dot)))
	if theta <= j.ConeLimit+1e-5 {
		return
	}
	swingAxis := wA.Cross(wB).Norm()
	if swingAxis.LenSq() == 0 {
		swingAxis, _ = orthonormalBasis(wA)
	}
	excess := (theta - j.ConeLimit) * 0.8
	applyAngularPositionCorrection(j.BodyA, j.BodyB, swingAxis, -excess)
}

// HingeJoint constrains two bodies to rotate around a single shared axis at an
// anchor point, with optional angular limits, velocity motor, spring/position
// drive, and break thresholds.
type HingeJoint struct {
	BodyA, BodyB     *Body
	LocalAnchorA     Vec3
	LocalAnchorB     Vec3
	LocalAxisA       Vec3 // hinge axis in A's local space (defaults to +Y)
	LocalAxisB       Vec3 // hinge axis in B's local space (defaults to LocalAxisA)
	EnableLimits     bool
	MinAngle         float32 // radians
	MaxAngle         float32 // radians
	MotorTargetVel   float32 // desired angular velocity around hinge axis (rad/s)
	MotorMaxTorque   float32 // max motor torque (N*m); motor active when > 0
	DriveTargetAngle float32 // target angle in radians for spring/position drive
	DriveStiffness   float32 // spring stiffness in N*m/rad (> 0 enables position spring)
	DriveDamping     float32 // angular damping in N*m*s/rad (>= 0)
	DriveMaxTorque   float32 // max drive torque in N*m (<= 0 means unlimited)
	BreakForce       float32 // max reaction force in N before breaking (<= 0 disables)
	BreakTorque      float32 // max reaction torque in N*m before breaking (<= 0 disables)
	CollideConnected bool

	jointReaction
	refInitialized bool
	refPerpA       Vec3
	refPerpB       Vec3
	motorImpulse   float32
	driveImpulse   float32
	driveGamma     float32
	driveBias      float32
	driveActive    bool
	limitImpulse   float32
	limitState     int // 0 = none, -1 = below min, +1 = above max
	// angle is the unwrapped hinge angle at the last Prepare, so Angle can continue past ±pi instead of jumping to the other end of the range (#546).
	angle float32
}

// NewHingeJoint creates a HingeJoint between a and b around localAxis.
func NewHingeJoint(a, b *Body, localAnchorA, localAnchorB, localAxis Vec3) *HingeJoint {
	ax := localAxis.Norm()
	if ax.LenSq() == 0 {
		ax = Vec3{Y: 1}
	}
	j := &HingeJoint{
		BodyA:        a,
		BodyB:        b,
		LocalAnchorA: localAnchorA,
		LocalAnchorB: localAnchorB,
		LocalAxisA:   ax,
		LocalAxisB:   ax,
	}
	j.initReference()
	return j
}

func (j *HingeJoint) collideConnected() bool { return j.CollideConnected }

func (j *HingeJoint) checkBreak() (JointBreakEvent, bool) {
	return j.evalBreak(j, j.BreakForce, j.BreakTorque)
}

// Bodies returns the connected bodies.
func (j *HingeJoint) Bodies() (*Body, *Body) { return j.BodyA, j.BodyB }

func (j *HingeJoint) localAxes() (Vec3, Vec3) {
	axA := j.LocalAxisA.Norm()
	if axA.LenSq() == 0 {
		axA = Vec3{Y: 1}
	}
	axB := j.LocalAxisB.Norm()
	if axB.LenSq() == 0 {
		axB = axA
	}
	return axA, axB
}

func (j *HingeJoint) initReference() {
	if j.refInitialized {
		return
	}
	axA, axB := j.localAxes()
	_, rotA, _, _, _, _ := bodyKinematics(j.BodyA)
	_, rotB, _, _, _, _ := bodyKinematics(j.BodyB)
	perpA, _ := orthonormalBasis(axA)
	worldPerp := rotA.RotateVec(perpA)
	perpB := rotB.Conjugate().RotateVec(worldPerp)
	perpB = perpB.Sub(axB.Scale(perpB.Dot(axB))).Norm()
	if perpB.LenSq() == 0 {
		perpB, _ = orthonormalBasis(axB)
	}
	j.refPerpA = perpA
	j.refPerpB = perpB
	j.refInitialized = true
}

// Angle returns the signed hinge rotation in radians around the hinge axis from the initial relative orientation.
//
// The angle is tracked across steps, so a hinge that keeps turning reports 3*pi/2 rather than -pi/2 and limits beyond ±pi, such as a valve with MaxAngle 4*pi, work. It assumes the hinge turns less than pi per step.
func (j *HingeJoint) Angle() float32 {
	return j.angle + wrapPi(j.rawAngle()-j.angle)
}

// wrapPi maps a to [-pi, pi).
func wrapPi(a float32) float32 {
	w := math.Mod(float64(a)+math.Pi, 2*math.Pi)
	if w < 0 {
		w += 2 * math.Pi
	}
	return float32(w - math.Pi)
}

// rawAngle returns the hinge angle in [-pi, pi].
func (j *HingeJoint) rawAngle() float32 {
	j.initReference()
	axA, _ := j.localAxes()
	_, rotA, _, _, _, _ := bodyKinematics(j.BodyA)
	_, rotB, _, _, _, _ := bodyKinematics(j.BodyB)
	a1 := rotA.RotateVec(axA)
	curA := rotA.RotateVec(j.refPerpA)
	curB := rotB.RotateVec(j.refPerpB)
	curBProj := curB.Sub(a1.Scale(curB.Dot(a1)))
	if curBProj.LenSq() <= 1e-10 {
		return 0
	}
	curBProj = curBProj.Norm()
	sinA := a1.Dot(curA.Cross(curBProj))
	cosA := curA.Dot(curBProj)
	return float32(detmath.Atan2(float64(sinA), float64(cosA)))
}

// Prepare initializes the reference frame, wakes motorized or spring-driven bodies, and checks limits.
func (j *HingeJoint) Prepare(dt float32) {
	j.resetStepReaction(dt)
	j.initReference()
	j.angle = j.Angle()
	j.motorImpulse = 0
	j.driveImpulse = 0
	j.driveGamma = 0
	j.driveBias = 0
	j.driveActive = false
	j.limitImpulse = 0
	j.limitState = 0

	if j.MotorMaxTorque > 0 && abs32(j.MotorTargetVel) > 1e-6 {
		wakeIfDynamic(j.BodyA)
		wakeIfDynamic(j.BodyB)
	}

	if (j.DriveStiffness > 0 || j.DriveDamping > 0) && dt > 0 {
		err := j.angle - j.DriveTargetAngle
		if gamma, bias, ok := softSpringParams(j.DriveStiffness, j.DriveDamping, err, dt); ok {
			j.driveGamma = gamma
			j.driveBias = bias
			j.driveActive = true
			if j.DriveStiffness > 0 && abs32(err) > 1e-4 {
				wakeIfDynamic(j.BodyA)
				wakeIfDynamic(j.BodyB)
			}
		}
	}

	if j.EnableLimits && j.MinAngle <= j.MaxAngle {
		ang := j.angle
		if ang <= j.MinAngle+1e-4 {
			j.limitState = -1
		} else if ang >= j.MaxAngle-1e-4 {
			j.limitState = 1
		}
	}
}

// SolveVelocity solves the hinge anchor, axis alignment, motor, spring/position drive, and angular limits.
func (j *HingeJoint) SolveVelocity(dt float32) {
	j.addLinearImpulse(solvePointAnchorVelocity(j.BodyA, j.BodyB, j.LocalAnchorA, j.LocalAnchorB))

	_, rotA, _, angA, _, invIA := bodyKinematics(j.BodyA)
	_, _, _, angB, _, invIB := bodyKinematics(j.BodyB)
	invISum := invIA + invIB
	if invISum <= 0 {
		return
	}

	axA, _ := j.localAxes()
	a1 := rotA.RotateVec(axA)
	t1, t2 := orthonormalBasis(a1)

	// 1. Lock the 2 rotational DOFs perpendicular to the hinge axis.
	wRel := angB.Sub(angA)
	perpErr := t1.Scale(wRel.Dot(t1)).Add(t2.Scale(wRel.Dot(t2)))
	perpImp := perpErr.Scale(-1 / invISum)
	applyAngularImpulsePair(j.BodyA, j.BodyB, perpImp)
	j.addAngularImpulse(perpImp)

	// 2. Angular velocity motor around the hinge axis.
	if j.MotorMaxTorque > 0 && dt > 0 {
		_, _, _, angA, _, _ = bodyKinematics(j.BodyA)
		_, _, _, angB, _, _ = bodyKinematics(j.BodyB)
		wAxis := angB.Sub(angA).Dot(a1)
		dLambda := (j.MotorTargetVel - wAxis) / invISum
		maxImp := j.MotorMaxTorque * dt
		newAcc := clamp32(j.motorImpulse+dLambda, -maxImp, maxImp)
		applied := newAcc - j.motorImpulse
		j.motorImpulse = newAcc
		if applied != 0 {
			imp := a1.Scale(applied)
			applyAngularImpulsePair(j.BodyA, j.BodyB, imp)
			j.addAngularImpulse(imp)
		}
	}

	// 2b. Angular spring / position drive around the hinge axis.
	if j.driveActive && dt > 0 {
		_, _, _, angA, _, _ = bodyKinematics(j.BodyA)
		_, _, _, angB, _, _ = bodyKinematics(j.BodyB)
		wAxis := angB.Sub(angA).Dot(a1)
		dLambda := -(float32(wAxis+j.driveBias) + float32(j.driveGamma*j.driveImpulse)) / (invISum + j.driveGamma)
		newAcc := j.driveImpulse + dLambda
		if j.DriveMaxTorque > 0 {
			maxImp := j.DriveMaxTorque * dt
			newAcc = clamp32(newAcc, -maxImp, maxImp)
		}
		applied := newAcc - j.driveImpulse
		j.driveImpulse = newAcc
		if applied != 0 {
			imp := a1.Scale(applied)
			applyAngularImpulsePair(j.BodyA, j.BodyB, imp)
			j.addAngularImpulse(imp)
		}
	}

	// 3. Angular limits (solved after motor and drive so limits always override them).
	if j.limitState != 0 {
		_, _, _, angA, _, _ = bodyKinematics(j.BodyA)
		_, _, _, angB, _, _ = bodyKinematics(j.BodyB)
		wAxis := angB.Sub(angA).Dot(a1)
		dLambda := -wAxis / invISum
		newAcc := j.limitImpulse + dLambda
		if j.limitState < 0 {
			// Below MinAngle: only allow positive impulse (pushing angle upward).
			if newAcc < 0 {
				newAcc = 0
			}
		} else {
			// Above MaxAngle: only allow negative impulse (pushing angle downward).
			if newAcc > 0 {
				newAcc = 0
			}
		}
		applied := newAcc - j.limitImpulse
		j.limitImpulse = newAcc
		if applied != 0 {
			imp := a1.Scale(applied)
			applyAngularImpulsePair(j.BodyA, j.BodyB, imp)
			j.addAngularImpulse(imp)
		}
	}
}

// SolvePosition corrects hinge anchor, axis alignment, and angular limit violations.
func (j *HingeJoint) SolvePosition(dt float32) {
	_ = dt
	solvePointAnchorPosition(j.BodyA, j.BodyB, j.LocalAnchorA, j.LocalAnchorB, 0.8)

	_, rotA, _, _, _, invIA := bodyKinematics(j.BodyA)
	_, rotB, _, _, _, invIB := bodyKinematics(j.BodyB)
	if invIA+invIB <= 0 {
		return
	}

	// 1. Re-align hinge axes a1 and a2.
	axA, axB := j.localAxes()
	a1 := rotA.RotateVec(axA)
	a2 := rotB.RotateVec(axB)
	cross := a2.Cross(a1)
	sinErr := cross.Len()
	if sinErr > 1e-6 {
		cosErr := clamp32(a1.Dot(a2), -1, 1)
		errAngle := float32(detmath.Atan2(float64(sinErr), float64(cosErr)))
		applyAngularPositionCorrection(j.BodyA, j.BodyB, cross.Scale(1/sinErr), errAngle*0.8)
	}

	// 2. Clamp hinge angle within [MinAngle, MaxAngle].
	if j.EnableLimits && j.MinAngle <= j.MaxAngle {
		ang := j.Angle()
		_, rotA, _, _, _, _ = bodyKinematics(j.BodyA)
		a1 = rotA.RotateVec(axA)
		if ang < j.MinAngle-1e-5 {
			applyAngularPositionCorrection(j.BodyA, j.BodyB, a1, (j.MinAngle-ang)*0.8)
		} else if ang > j.MaxAngle+1e-5 {
			applyAngularPositionCorrection(j.BodyA, j.BodyB, a1, -(ang-j.MaxAngle)*0.8)
		}
	}
}

// SliderJoint constrains two bodies to translate along a single shared axis
// without relative rotation, with optional linear limits, velocity motor,
// spring/position drive, and break thresholds.
type SliderJoint struct {
	BodyA, BodyB     *Body
	LocalAnchorA     Vec3
	LocalAnchorB     Vec3
	LocalAxisA       Vec3 // slide axis in A's local space (defaults to +X)
	EnableLimits     bool
	MinTranslation   float32
	MaxTranslation   float32
	MotorTargetVel   float32 // desired linear speed along slide axis (units/s)
	MotorMaxForce    float32 // max linear motor force (N); active when > 0
	DriveTargetPos   float32 // target translation along slide axis for spring/position drive
	DriveStiffness   float32 // linear spring stiffness in N/m (> 0 enables position spring)
	DriveDamping     float32 // linear damping in N*s/m (>= 0)
	DriveMaxForce    float32 // max drive force in N (<= 0 means unlimited)
	BreakForce       float32 // max reaction force in N before breaking (<= 0 disables)
	BreakTorque      float32 // max reaction torque in N*m before breaking (<= 0 disables)
	CollideConnected bool

	jointReaction
	refRelRot      Quat
	refInitialized bool
	motorImpulse   float32
	driveImpulse   float32
	driveGamma     float32
	driveBias      float32
	driveActive    bool
	limitImpulse   float32
	limitState     int
}

// NewSliderJoint creates a SliderJoint sliding along localAxis.
func NewSliderJoint(a, b *Body, localAnchorA, localAnchorB, localAxis Vec3) *SliderJoint {
	ax := localAxis.Norm()
	if ax.LenSq() == 0 {
		ax = Vec3{X: 1}
	}
	j := &SliderJoint{
		BodyA:        a,
		BodyB:        b,
		LocalAnchorA: localAnchorA,
		LocalAnchorB: localAnchorB,
		LocalAxisA:   ax,
	}
	j.initReference()
	return j
}

func (j *SliderJoint) collideConnected() bool { return j.CollideConnected }

func (j *SliderJoint) checkBreak() (JointBreakEvent, bool) {
	return j.evalBreak(j, j.BreakForce, j.BreakTorque)
}

// Bodies returns the connected bodies.
func (j *SliderJoint) Bodies() (*Body, *Body) { return j.BodyA, j.BodyB }

func (j *SliderJoint) localAxis() Vec3 {
	ax := j.LocalAxisA.Norm()
	if ax.LenSq() == 0 {
		return Vec3{X: 1}
	}
	return ax
}

func (j *SliderJoint) initReference() {
	if j.refInitialized {
		return
	}
	_, rotA, _, _, _, _ := bodyKinematics(j.BodyA)
	_, rotB, _, _, _, _ := bodyKinematics(j.BodyB)
	j.refRelRot = rotA.Conjugate().Mul(rotB).Norm()
	j.refInitialized = true
}

// Translation returns the current signed displacement of BodyB's anchor
// relative to BodyA's anchor along the world slide axis.
func (j *SliderJoint) Translation() float32 {
	posA, rotA, _, _, _, _ := bodyKinematics(j.BodyA)
	posB, rotB, _, _, _, _ := bodyKinematics(j.BodyB)
	axis := rotA.RotateVec(j.localAxis())
	diff := posB.Add(rotB.RotateVec(j.LocalAnchorB)).Sub(posA.Add(rotA.RotateVec(j.LocalAnchorA)))
	return diff.Dot(axis)
}

// Prepare initializes the reference rotation, wakes motorized or spring-driven bodies, and checks linear limits.
func (j *SliderJoint) Prepare(dt float32) {
	j.resetStepReaction(dt)
	j.initReference()
	j.motorImpulse = 0
	j.driveImpulse = 0
	j.driveGamma = 0
	j.driveBias = 0
	j.driveActive = false
	j.limitImpulse = 0
	j.limitState = 0

	if j.MotorMaxForce > 0 && abs32(j.MotorTargetVel) > 1e-6 {
		wakeIfDynamic(j.BodyA)
		wakeIfDynamic(j.BodyB)
	}

	tr := j.Translation()
	if (j.DriveStiffness > 0 || j.DriveDamping > 0) && dt > 0 {
		err := tr - j.DriveTargetPos
		if gamma, bias, ok := softSpringParams(j.DriveStiffness, j.DriveDamping, err, dt); ok {
			j.driveGamma = gamma
			j.driveBias = bias
			j.driveActive = true
			if j.DriveStiffness > 0 && abs32(err) > 1e-4 {
				wakeIfDynamic(j.BodyA)
				wakeIfDynamic(j.BodyB)
			}
		}
	}

	if j.EnableLimits && j.MinTranslation <= j.MaxTranslation {
		if tr <= j.MinTranslation+1e-4 {
			j.limitState = -1
		} else if tr >= j.MaxTranslation-1e-4 {
			j.limitState = 1
		}
	}
}

// SolveVelocity solves orientation lock, perpendicular linear lock, linear motor, spring/position drive, and limits.
func (j *SliderJoint) SolveVelocity(dt float32) {
	j.addAngularImpulse(solveFullOrientationVelocity(j.BodyA, j.BodyB))

	_, rotA, velA, angA, invMA, invIA := bodyKinematics(j.BodyA)
	_, rotB, velB, angB, invMB, invIB := bodyKinematics(j.BodyB)
	invMSum := invMA + invMB
	if invMSum+invIA+invIB == 0 {
		return
	}

	axis := rotA.RotateVec(j.localAxis())
	t1, t2 := orthonormalBasis(axis)
	rA := rotA.RotateVec(j.LocalAnchorA)
	rB := rotB.RotateVec(j.LocalAnchorB)

	solveDir := func(dir Vec3) {
		_, _, velA, angA, _, _ = bodyKinematics(j.BodyA)
		_, _, velB, angB, _, _ = bodyKinematics(j.BodyB)
		rAxd := rA.Cross(dir)
		rBxd := rB.Cross(dir)
		w := float32(invMSum+float32(invIA*rAxd.LenSq())) + float32(invIB*rBxd.LenSq())
		if w <= 1e-12 {
			return
		}
		vRel := velB.Add(angB.Cross(rB)).Sub(velA.Add(angA.Cross(rA)))
		jScalar := -vRel.Dot(dir) / w
		imp := dir.Scale(jScalar)
		applyAnchorImpulse(j.BodyA, j.BodyB, rA, rB, imp)
		j.addLinearImpulse(imp)
	}

	// 1. Lock the 2 translational DOFs perpendicular to the slide axis.
	solveDir(t1)
	solveDir(t2)

	if invMSum <= 0 {
		return
	}

	// 2. Linear velocity motor along axis.
	if j.MotorMaxForce > 0 && dt > 0 {
		_, _, velA, angA, _, _ = bodyKinematics(j.BodyA)
		_, _, velB, angB, _, _ = bodyKinematics(j.BodyB)
		vRel := velB.Add(angB.Cross(rB)).Sub(velA.Add(angA.Cross(rA)))
		vAxis := vRel.Dot(axis)
		dLambda := (j.MotorTargetVel - vAxis) / invMSum
		maxImp := j.MotorMaxForce * dt
		newAcc := clamp32(j.motorImpulse+dLambda, -maxImp, maxImp)
		applied := newAcc - j.motorImpulse
		j.motorImpulse = newAcc
		if applied != 0 {
			imp := axis.Scale(applied)
			applyAnchorImpulse(j.BodyA, j.BodyB, Vec3{}, Vec3{}, imp)
			j.addLinearImpulse(imp)
		}
	}

	// 2b. Linear spring / position drive along axis.
	if j.driveActive && dt > 0 {
		_, _, velA, angA, _, _ = bodyKinematics(j.BodyA)
		_, _, velB, angB, _, _ = bodyKinematics(j.BodyB)
		vRel := velB.Add(angB.Cross(rB)).Sub(velA.Add(angA.Cross(rA)))
		vAxis := vRel.Dot(axis)
		dLambda := -(float32(vAxis+j.driveBias) + float32(j.driveGamma*j.driveImpulse)) / (invMSum + j.driveGamma)
		newAcc := j.driveImpulse + dLambda
		if j.DriveMaxForce > 0 {
			maxImp := j.DriveMaxForce * dt
			newAcc = clamp32(newAcc, -maxImp, maxImp)
		}
		applied := newAcc - j.driveImpulse
		j.driveImpulse = newAcc
		if applied != 0 {
			imp := axis.Scale(applied)
			applyAnchorImpulse(j.BodyA, j.BodyB, Vec3{}, Vec3{}, imp)
			j.addLinearImpulse(imp)
		}
	}

	// 3. Linear limits along axis.
	if j.limitState != 0 {
		_, _, velA, angA, _, _ = bodyKinematics(j.BodyA)
		_, _, velB, angB, _, _ = bodyKinematics(j.BodyB)
		vRel := velB.Add(angB.Cross(rB)).Sub(velA.Add(angA.Cross(rA)))
		vAxis := vRel.Dot(axis)
		dLambda := -vAxis / invMSum
		newAcc := j.limitImpulse + dLambda
		if j.limitState < 0 {
			if newAcc < 0 {
				newAcc = 0
			}
		} else {
			if newAcc > 0 {
				newAcc = 0
			}
		}
		applied := newAcc - j.limitImpulse
		j.limitImpulse = newAcc
		if applied != 0 {
			imp := axis.Scale(applied)
			applyAnchorImpulse(j.BodyA, j.BodyB, Vec3{}, Vec3{}, imp)
			j.addLinearImpulse(imp)
		}
	}
}

// SolvePosition corrects orientation, perpendicular drift, and linear limit violations.
func (j *SliderJoint) SolvePosition(dt float32) {
	_ = dt
	j.initReference()
	solveFullOrientationPosition(j.BodyA, j.BodyB, j.refRelRot, 0.8)

	posA, rotA, _, _, invMA, invIA := bodyKinematics(j.BodyA)
	posB, rotB, _, _, invMB, invIB := bodyKinematics(j.BodyB)
	invMSum := invMA + invMB
	if invMSum+invIA+invIB == 0 {
		return
	}

	axis := rotA.RotateVec(j.localAxis())
	rA := rotA.RotateVec(j.LocalAnchorA)
	rB := rotB.RotateVec(j.LocalAnchorB)
	diff := posB.Add(rB).Sub(posA.Add(rA))
	along := diff.Dot(axis)
	perp := diff.Sub(axis.Scale(along))

	if perp.LenSq() > 1e-10 {
		k := pointConstraintMassMatrix(invMA, invIA, rA, invMB, invIB, rB)
		p := k.solve(perp.Scale(-0.8))
		// Keep correction perpendicular to axis so we don't fight slide translation.
		p = p.Sub(axis.Scale(p.Dot(axis)))
		applyAnchorPositionCorrection(j.BodyA, j.BodyB, rA, rB, p)
	}

	if j.EnableLimits && j.MinTranslation <= j.MaxTranslation && invMSum > 0 {
		tr := j.Translation()
		var err float32
		if tr < j.MinTranslation-1e-5 {
			err = tr - j.MinTranslation
		} else if tr > j.MaxTranslation+1e-5 {
			err = tr - j.MaxTranslation
		}
		if err != 0 {
			corr := axis.Scale(-err * 0.8 / invMSum)
			applyAnchorPositionCorrection(j.BodyA, j.BodyB, Vec3{}, Vec3{}, corr)
		}
	}
}

// FixedJoint rigidly locks both relative position and relative orientation
// between BodyA and BodyB.
type FixedJoint struct {
	BodyA, BodyB     *Body
	LocalAnchorA     Vec3
	LocalAnchorB     Vec3
	RefRelRot        Quat
	BreakForce       float32 // max reaction force in N before breaking (<= 0 disables)
	BreakTorque      float32 // max reaction torque in N*m before breaking (<= 0 disables)
	CollideConnected bool

	jointReaction
	refInitialized bool
}

// NewFixedJoint creates a FixedJoint locking a and b at their current relative
// orientation and specified local anchor points.
func NewFixedJoint(a, b *Body, localAnchorA, localAnchorB Vec3) *FixedJoint {
	j := &FixedJoint{
		BodyA:        a,
		BodyB:        b,
		LocalAnchorA: localAnchorA,
		LocalAnchorB: localAnchorB,
	}
	j.initReference()
	return j
}

func (j *FixedJoint) collideConnected() bool { return j.CollideConnected }

func (j *FixedJoint) checkBreak() (JointBreakEvent, bool) {
	return j.evalBreak(j, j.BreakForce, j.BreakTorque)
}

// Bodies returns the connected bodies.
func (j *FixedJoint) Bodies() (*Body, *Body) { return j.BodyA, j.BodyB }

func (j *FixedJoint) initReference() {
	if j.refInitialized {
		return
	}
	if j.RefRelRot.LenSq() > 1e-12 {
		j.RefRelRot = j.RefRelRot.Norm()
		j.refInitialized = true
		return
	}
	_, rotA, _, _, _, _ := bodyKinematics(j.BodyA)
	_, rotB, _, _, _, _ := bodyKinematics(j.BodyB)
	j.RefRelRot = rotA.Conjugate().Mul(rotB).Norm()
	j.refInitialized = true
}

// Prepare captures the reference relative orientation if not already set.
func (j *FixedJoint) Prepare(dt float32) {
	j.resetStepReaction(dt)
	j.initReference()
}

// SolveVelocity solves the 6-DOF linear and angular lock.
func (j *FixedJoint) SolveVelocity(dt float32) {
	_ = dt
	j.addLinearImpulse(solvePointAnchorVelocity(j.BodyA, j.BodyB, j.LocalAnchorA, j.LocalAnchorB))
	j.addAngularImpulse(solveFullOrientationVelocity(j.BodyA, j.BodyB))
}

// SolvePosition corrects linear and angular drift from the locked relative pose.
func (j *FixedJoint) SolvePosition(dt float32) {
	_ = dt
	j.initReference()
	solvePointAnchorPosition(j.BodyA, j.BodyB, j.LocalAnchorA, j.LocalAnchorB, 0.8)
	solveFullOrientationPosition(j.BodyA, j.BodyB, j.RefRelRot, 0.8)
}
