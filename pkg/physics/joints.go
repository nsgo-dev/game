package physics

import (
	"math"

	"github.com/nsgo-dev/game/internal/detmath"
	"github.com/nsgo-dev/nsgo/core"
)

// Joint constrains the relative motion of one or two RigidBodies inside RigidWorld.Step.
type Joint interface {
	preStep(dt float64)
	solveVelocity()
	solvePosition()
}

type jointBodies interface {
	jointBodyPair() (*RigidBody, *RigidBody)
}

type collideConnectedJoint interface {
	collideConnected() bool
}

type breakableJoint interface {
	checkBreak() (JointBreakEvent, bool)
}

type identifiableJoint interface {
	jointInternalID() uint64
	setJointInternalID(uint64)
}

type stepReactionJoint interface {
	resetStepReaction(dt float64)
}

// JointBreakEvent reports a joint that broke during RigidWorld.Step because its
// reaction force or torque exceeded its BreakForce or BreakTorque threshold.
type JointBreakEvent struct {
	Joint  Joint
	BodyA  *RigidBody
	BodyB  *RigidBody
	ForceX float32
	ForceY float32
	Torque float32
}

// jointReaction tracks per-step accumulated constraint impulses and broken state
// shared by all built-in Joint implementations.
type jointReaction struct {
	stepImpulseX   float64
	stepImpulseY   float64
	stepAngImpulse float64
	lastDt         float64
	broken         bool
	// internalID is assigned by RigidWorld so Restore matches joint pointers by identity without falling back to slice index.
	internalID uint64
}

func (r *jointReaction) jointInternalID() uint64      { return r.internalID }
func (r *jointReaction) setJointInternalID(id uint64) { r.internalID = id }

func (r *jointReaction) resetStepReaction(dt float64) {
	r.stepImpulseX = 0
	r.stepImpulseY = 0
	r.stepAngImpulse = 0
	r.lastDt = dt
}

func (r *jointReaction) addLinearImpulse(px, py float64) {
	r.stepImpulseX += px
	r.stepImpulseY += py
}

func (r *jointReaction) addAngularImpulse(ang float64) {
	r.stepAngImpulse += ang
}

// ReactionForce returns the world-space constraint force (fx, fy) applied to
// body B (or the attached body for MouseJoint) during the most recent RigidWorld.Step.
func (r *jointReaction) ReactionForce() (fx, fy float32) {
	if r.lastDt <= 0 {
		return 0, 0
	}
	return float32(r.stepImpulseX / r.lastDt), float32(r.stepImpulseY / r.lastDt)
}

// ReactionTorque returns the scalar constraint torque (clockwise positive in screen space)
// applied to body B during the most recent RigidWorld.Step.
func (r *jointReaction) ReactionTorque() float32 {
	if r.lastDt <= 0 {
		return 0
	}
	return float32(r.stepAngImpulse / r.lastDt)
}

// Broken reports whether the joint has exceeded its break threshold and been removed.
func (r *jointReaction) Broken() bool {
	return r.broken
}

func (r *jointReaction) evalBreak(j Joint, a, b *RigidBody, breakForce, breakTorque float32) (JointBreakEvent, bool) {
	if r.broken || r.lastDt <= 0 {
		return JointBreakEvent{}, false
	}
	if breakForce <= 0 && breakTorque <= 0 {
		return JointBreakEvent{}, false
	}
	fx, fy := r.ReactionForce()
	tq := r.ReactionTorque()
	fx64, fy64 := float64(fx), float64(fy)
	forceSq := float64(fx64*fx64) + float64(fy64*fy64)
	bf := float64(breakForce)
	exceeded := (breakForce > 0 && forceSq > float64(bf*bf)) ||
		(breakTorque > 0 && math.Abs(float64(tq)) > float64(breakTorque))
	if !exceeded {
		return JointBreakEvent{}, false
	}
	r.broken = true
	return JointBreakEvent{
		Joint:  j,
		BodyA:  a,
		BodyB:  b,
		ForceX: fx,
		ForceY: fy,
		Torque: tq,
	}, true
}

func clamp64(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func normalizeAxis2D(ax, ay, defX, defY float64) (float64, float64) {
	n := detmath.Hypot(ax, ay)
	if n <= 1e-8 {
		return defX, defY
	}
	return ax / n, ay / n
}

func bodyWorldDir(b *RigidBody, local core.Point, defX, defY float64) (float64, float64) {
	lx, ly := normalizeAxis2D(float64(local.X), float64(local.Y), defX, defY)
	if b == nil {
		return lx, ly
	}
	s, c := detmath.Sincos(float64(b.Angle))
	wx := float64(lx*c) - float64(ly*s)
	wy := float64(lx*s) + float64(ly*c)
	return normalizeAxis2D(wx, wy, defX, defY)
}

func bodyLocalDir(b *RigidBody, world core.Point, defX, defY float64) core.Point {
	wx, wy := normalizeAxis2D(float64(world.X), float64(world.Y), defX, defY)
	if b == nil {
		return core.Point{X: core.Points(wx), Y: core.Points(wy)}
	}
	s, c := detmath.Sincos(float64(b.Angle))
	lx := float64(wx*c) + float64(wy*s)
	ly := float64(-wx*s) + float64(wy*c)
	lx, ly = normalizeAxis2D(lx, ly, defX, defY)
	return core.Point{X: core.Points(lx), Y: core.Points(ly)}
}

func applyAngularImpulse2D(a, b *RigidBody, imp float64) {
	if a != nil && !a.Static {
		a.AngularVel = float32(float64(a.AngularVel) - float64(imp*float64(a.InvInertia)))
	}
	if b != nil && !b.Static {
		b.AngularVel = float32(float64(b.AngularVel) + float64(imp*float64(b.InvInertia)))
	}
}

// softSpringParams64 computes implicit Euler ERP/CFM (gamma, bias) parameters for
// a 1D spring-damper with stiffness k >= 0, damping c >= 0, position error err,
// and step duration dt > 0.
func softSpringParams64(k, c, err, dt float64) (gamma, bias float64, ok bool) {
	if dt <= 0 {
		return 0, 0, false
	}
	k = max(0, k)
	c = max(0, c)
	denom := c + float64(dt*k)
	if denom <= 1e-12 {
		return 0, 0, false
	}
	gamma = 1.0 / float64(dt*denom)
	bias = float64(k*err) / denom
	return gamma, bias, true
}

// springCoeffsFromFreq converts natural frequency (Hz) and damping ratio for an
// effective mass into linear/angular stiffness k and damping c.
func springCoeffsFromFreq(effMass, freqHz, dampingRatio float64) (k, c float64) {
	omega := float64(2.0*math.Pi) * freqHz
	k = float64(effMass*omega) * omega
	c = float64(float64(2.0*effMass)*max(0, dampingRatio)) * omega
	return k, c
}

// symMat3_64 stores a symmetric 3x3 matrix:
//
//	[ a  d  e ]
//	[ d  b  f ]
//	[ e  f  c ]
type symMat3_64 struct {
	a, b, c float64
	d, e, f float64
}

func (m symMat3_64) solve(rx, ry, rz float64) (x, y, z float64, ok bool) {
	c00 := float64(m.b*m.c) - float64(m.f*m.f)
	c01 := float64(m.e*m.f) - float64(m.d*m.c)
	c02 := float64(m.d*m.f) - float64(m.e*m.b)
	c11 := float64(m.a*m.c) - float64(m.e*m.e)
	c12 := float64(m.d*m.e) - float64(m.a*m.f)
	c22 := float64(m.a*m.b) - float64(m.d*m.d)
	det := float64(float64(m.a*c00)+float64(m.d*c01)) + float64(m.e*c02)
	if math.Abs(det) <= 1e-12 {
		return 0, 0, 0, false
	}
	invDet := 1.0 / det
	x = float64((float64(float64(c00*rx)+float64(c01*ry)) + float64(c02*rz)) * invDet)
	y = float64((float64(float64(c01*rx)+float64(c11*ry)) + float64(c12*rz)) * invDet)
	z = float64((float64(float64(c02*rx)+float64(c12*ry)) + float64(c22*rz)) * invDet)
	return x, y, z, true
}

// DistanceJoint constrains two local anchor points on bodies A and B to stay at a target Length
// (or within [MinLength, MaxLength], or <= Length when MaxOnly is true for rope/tether mode).
// With default zero Compliance and Stiffness, it acts as a rigid rod; positive Compliance or
// Stiffness in (0, 1) softens the constraint.
type DistanceJoint struct {
	A, B             *RigidBody
	AnchorA          core.Point
	AnchorB          core.Point
	Length           float32
	MinLength        float32 // optional minimum distance (active when MinLength > 0 or MaxLength > 0)
	MaxLength        float32 // optional maximum distance (active when > 0; slack between MinLength and MaxLength)
	MaxOnly          bool    // rope / max-distance mode: slack when distance <= max length, pulls only when stretched
	Stiffness        float32
	Compliance       float32
	Damping          float32
	BreakForce       float32 // max reaction force before breaking (<= 0 disables)
	BreakTorque      float32 // max reaction torque before breaking (<= 0 disables)
	CollideConnected bool

	jointReaction
	initialized bool
	active      bool
	dt          float64
	ux, uy      float64
	rAx, rAy    float64
	rBx, rBy    float64
	effMass     float64
	accImpulse  float64
	mode        int // 0 = slack, 1 = bilateral fixed length, 2 = min limit, 3 = max limit (rope)
	targetLen   float64
}

// NewDistanceJoint creates a DistanceJoint between local anchors on a and b.
// If length <= 0 and both bodies are non-nil, Length is initialized to the current distance between the anchors.
func NewDistanceJoint(a, b *RigidBody, anchorA, anchorB core.Point, length float32) *DistanceJoint {
	j := &DistanceJoint{
		A:       a,
		B:       b,
		AnchorA: anchorA,
		AnchorB: anchorB,
		Length:  length,
	}
	if length > 0 {
		j.initialized = true
	} else if a != nil && b != nil {
		pA := a.LocalToWorld(anchorA)
		pB := b.LocalToWorld(anchorB)
		j.Length = float32(detmath.Hypot(float64(pB.X-pA.X), float64(pB.Y-pA.Y)))
		j.initialized = true
	}
	return j
}

// NewRopeJoint creates a DistanceJoint configured in rope/max-distance mode (MaxOnly = true),
// allowing the anchors to move closer than maxLength without pushing apart while pulling taut
// when distance reaches maxLength. If maxLength <= 0, it initializes to the current anchor distance.
func NewRopeJoint(a, b *RigidBody, anchorA, anchorB core.Point, maxLength float32) *DistanceJoint {
	j := NewDistanceJoint(a, b, anchorA, anchorB, maxLength)
	j.MaxOnly = true
	return j
}

func (j *DistanceJoint) jointBodyPair() (*RigidBody, *RigidBody) { return j.A, j.B }
func (j *DistanceJoint) collideConnected() bool                  { return j.CollideConnected }
func (j *DistanceJoint) checkBreak() (JointBreakEvent, bool) {
	return j.evalBreak(j, j.A, j.B, j.BreakForce, j.BreakTorque)
}

func (j *DistanceJoint) effectiveStiffness() float64 {
	if j.Stiffness <= 0 {
		return 1.0
	}
	if j.Stiffness > 1 {
		return 1.0
	}
	return float64(j.Stiffness)
}

func (j *DistanceJoint) effectiveRange() (lo, hi float64, unilateral bool) {
	if j.MaxOnly {
		hi = float64(j.MaxLength)
		if hi <= 0 {
			hi = max(0, float64(j.Length))
		}
		return 0, hi, true
	}
	if j.MinLength > 0 || j.MaxLength > 0 {
		lo = max(0, float64(j.MinLength))
		hi = float64(j.MaxLength)
		if hi <= 0 {
			hi = max(lo, float64(j.Length))
		}
		if hi < lo {
			hi = lo
		}
		return lo, hi, hi > lo+1e-6
	}
	l := max(0, float64(j.Length))
	return l, l, false
}

func (j *DistanceJoint) preStep(dt float64) {
	j.active = false
	j.accImpulse = 0
	j.mode = 0
	j.targetLen = 0
	if j.A == nil || j.B == nil || (j.A.Static && j.B.Static) {
		return
	}
	pA := j.A.LocalToWorld(j.AnchorA)
	pB := j.B.LocalToWorld(j.AnchorB)
	dx := float64(pB.X) - float64(pA.X)
	dy := float64(pB.Y) - float64(pA.Y)
	dist := detmath.Hypot(dx, dy)

	if !j.initialized && j.Length <= 0 && j.MaxLength <= 0 {
		j.Length = float32(dist)
		j.initialized = true
	}

	lo, hi, unilateral := j.effectiveRange()
	if !unilateral {
		j.mode = 1
		j.targetLen = lo
	} else if lo > 0 && dist <= lo+1e-4 {
		j.mode = 2
		j.targetLen = lo
	} else if dist >= hi-1e-4 {
		j.mode = 3
		j.targetLen = hi
	} else {
		j.dt = dt
		return
	}

	if dist > 1e-8 {
		j.ux, j.uy = dx/dist, dy/dist
	} else {
		j.ux, j.uy = 1, 0
	}

	j.rAx = float64(pA.X) - float64(j.A.X)
	j.rAy = float64(pA.Y) - float64(j.A.Y)
	j.rBx = float64(pB.X) - float64(j.B.X)
	j.rBy = float64(pB.Y) - float64(j.B.Y)

	rnA := float64(j.rAx*j.uy) - float64(j.rAy*j.ux)
	rnB := float64(j.rBx*j.uy) - float64(j.rBy*j.ux)
	k := float64(float64(j.A.InvMass)+float64(j.B.InvMass)) +
		float64(float64(float64(j.A.InvInertia)*rnA)*rnA) +
		float64(float64(float64(j.B.InvInertia)*rnB)*rnB)

	alpha := max(0, float64(j.Compliance)) / float64(dt*dt)
	denom := k + alpha
	if denom <= 1e-12 {
		return
	}
	j.effMass = 1.0 / denom
	j.dt = dt
	j.active = true
}

func (j *DistanceJoint) solveVelocity() {
	if !j.active || j.mode == 0 {
		return
	}
	vAx := float64(j.A.VX) - float64(float64(j.A.AngularVel)*j.rAy)
	vAy := float64(j.A.VY) + float64(float64(j.A.AngularVel)*j.rAx)
	vBx := float64(j.B.VX) - float64(float64(j.B.AngularVel)*j.rBy)
	vBy := float64(j.B.VY) + float64(float64(j.B.AngularVel)*j.rBx)

	vRel := float64((vBx-vAx)*j.ux) + float64((vBy-vAy)*j.uy)
	dLambda := float64(float64(-vRel*j.effMass) * j.effectiveStiffness())
	if j.Damping > 0 {
		// Additional viscous damping along the rod axis.
		damp := min(1.0, float64(j.Damping)*j.dt)
		dLambda = float64(dLambda * (1.0 - float64(0.5*damp)))
	}

	applied := dLambda
	if j.mode == 2 || j.mode == 3 {
		newAcc := j.accImpulse + dLambda
		if j.mode == 2 && newAcc < 0 {
			newAcc = 0
		} else if j.mode == 3 && newAcc > 0 {
			newAcc = 0
		}
		applied = newAcc - j.accImpulse
		j.accImpulse = newAcc
	} else {
		j.accImpulse += applied
	}
	if applied == 0 {
		return
	}

	px, py := float64(applied*j.ux), float64(applied*j.uy)
	j.A.applyImpulseRaw(-px, -py, j.rAx, j.rAy)
	j.B.applyImpulseRaw(px, py, j.rBx, j.rBy)
	j.addLinearImpulse(px, py)
}

func (j *DistanceJoint) solvePosition() {
	if j.A == nil || j.B == nil || (j.A.Static && j.B.Static) || j.dt <= 0 {
		return
	}
	pA := j.A.LocalToWorld(j.AnchorA)
	pB := j.B.LocalToWorld(j.AnchorB)
	dx := float64(pB.X) - float64(pA.X)
	dy := float64(pB.Y) - float64(pA.Y)
	dist := detmath.Hypot(dx, dy)

	lo, hi, unilateral := j.effectiveRange()
	var err float64
	if !unilateral {
		if !j.active {
			return
		}
		err = dist - lo
	} else if lo > 0 && dist < lo {
		err = dist - lo
	} else if dist > hi {
		err = dist - hi
	} else {
		return
	}
	if math.Abs(err) <= 1e-6 {
		return
	}
	ux, uy := 1.0, 0.0
	if dist > 1e-8 {
		ux, uy = dx/dist, dy/dist
	}

	rAx := float64(pA.X) - float64(j.A.X)
	rAy := float64(pA.Y) - float64(j.A.Y)
	rBx := float64(pB.X) - float64(j.B.X)
	rBy := float64(pB.Y) - float64(j.B.Y)

	rnA := float64(rAx*uy) - float64(rAy*ux)
	rnB := float64(rBx*uy) - float64(rBy*ux)
	k := float64(float64(j.A.InvMass)+float64(j.B.InvMass)) +
		float64(float64(float64(j.A.InvInertia)*rnA)*rnA) +
		float64(float64(float64(j.B.InvInertia)*rnB)*rnB)

	alpha := max(0, float64(j.Compliance)) / float64(j.dt*j.dt)
	denom := k + alpha
	if denom <= 1e-12 {
		return
	}
	lambda := -err * j.effectiveStiffness() / denom
	px, py := float64(lambda*ux), float64(lambda*uy)

	if !j.A.Static {
		j.A.X = float32(float64(j.A.X) - float64(px*float64(j.A.InvMass)))
		j.A.Y = float32(float64(j.A.Y) - float64(py*float64(j.A.InvMass)))
		crossA := float64(rAx*py) - float64(rAy*px)
		j.A.Angle = float32(float64(j.A.Angle) - float64(crossA*float64(j.A.InvInertia)))
	}
	if !j.B.Static {
		j.B.X = float32(float64(j.B.X) + float64(px*float64(j.B.InvMass)))
		j.B.Y = float32(float64(j.B.Y) + float64(py*float64(j.B.InvMass)))
		crossB := float64(rBx*py) - float64(rBy*px)
		j.B.Angle = float32(float64(j.B.Angle) + float64(crossB*float64(j.B.InvInertia)))
	}
}

// SpringJoint connects local anchor points on bodies A and B with a damped Hooke's law spring.
type SpringJoint struct {
	A, B             *RigidBody
	AnchorA          core.Point
	AnchorB          core.Point
	RestLength       float32
	Stiffness        float32
	Damping          float32
	BreakForce       float32 // max reaction force before breaking (<= 0 disables)
	BreakTorque      float32 // max reaction torque before breaking (<= 0 disables)
	CollideConnected bool

	jointReaction
	initialized bool
}

// NewSpringJoint creates a SpringJoint between local anchors on a and b.
// If restLength <= 0 and both bodies are non-nil, RestLength is initialized to the current anchor distance.
func NewSpringJoint(a, b *RigidBody, anchorA, anchorB core.Point, restLength, stiffness, damping float32) *SpringJoint {
	j := &SpringJoint{
		A:          a,
		B:          b,
		AnchorA:    anchorA,
		AnchorB:    anchorB,
		RestLength: restLength,
		Stiffness:  stiffness,
		Damping:    damping,
	}
	if restLength > 0 {
		j.initialized = true
	} else if a != nil && b != nil {
		pA := a.LocalToWorld(anchorA)
		pB := b.LocalToWorld(anchorB)
		j.RestLength = float32(detmath.Hypot(float64(pB.X-pA.X), float64(pB.Y-pA.Y)))
		j.initialized = true
	}
	return j
}

func (j *SpringJoint) jointBodyPair() (*RigidBody, *RigidBody) { return j.A, j.B }
func (j *SpringJoint) collideConnected() bool                  { return j.CollideConnected }
func (j *SpringJoint) checkBreak() (JointBreakEvent, bool) {
	return j.evalBreak(j, j.A, j.B, j.BreakForce, j.BreakTorque)
}

func (j *SpringJoint) preStep(dt float64) {
	if j.A == nil || j.B == nil || (j.A.Static && j.B.Static) {
		return
	}
	pA := j.A.LocalToWorld(j.AnchorA)
	pB := j.B.LocalToWorld(j.AnchorB)
	dx := float64(pB.X) - float64(pA.X)
	dy := float64(pB.Y) - float64(pA.Y)
	dist := detmath.Hypot(dx, dy)

	if !j.initialized && j.RestLength <= 0 {
		j.RestLength = float32(dist)
		j.initialized = true
	}

	var ux, uy float64
	if dist > 1e-8 {
		ux, uy = dx/dist, dy/dist
	} else {
		if j.RestLength <= 0 {
			return
		}
		ux, uy = 1, 0
	}

	rAx := float64(pA.X) - float64(j.A.X)
	rAy := float64(pA.Y) - float64(j.A.Y)
	rBx := float64(pB.X) - float64(j.B.X)
	rBy := float64(pB.Y) - float64(j.B.Y)

	vAx := float64(j.A.VX) - float64(float64(j.A.AngularVel)*rAy)
	vAy := float64(j.A.VY) + float64(float64(j.A.AngularVel)*rAx)
	vBx := float64(j.B.VX) - float64(float64(j.B.AngularVel)*rBy)
	vBy := float64(j.B.VY) + float64(float64(j.B.AngularVel)*rBx)
	vRel := float64((vBx-vAx)*ux) + float64((vBy-vAy)*uy)

	k := max(0, float64(j.Stiffness))
	c := max(0, float64(j.Damping))
	if k == 0 && c == 0 {
		return
	}

	rnA := float64(rAx*uy) - float64(rAy*ux)
	rnB := float64(rBx*uy) - float64(rBy*ux)
	mInv := float64(float64(j.A.InvMass)+float64(j.B.InvMass)) +
		float64(float64(float64(j.A.InvInertia)*rnA)*rnA) +
		float64(float64(float64(j.B.InvInertia)*rnB)*rnB)

	// Unconditionally stable implicit spring-damper impulse along the anchor axis.
	ext := dist - float64(j.RestLength)
	denom := 1.0 + float64(float64(dt*mInv)*(c+float64(dt*k)))
	impulse := -dt * (float64(k*ext) + float64(c*vRel)) / denom

	px, py := float64(impulse*ux), float64(impulse*uy)
	j.A.applyImpulseRaw(-px, -py, rAx, rAy)
	j.B.applyImpulseRaw(px, py, rBx, rBy)
	j.addLinearImpulse(px, py)
}

func (j *SpringJoint) solveVelocity() {}
func (j *SpringJoint) solvePosition() {}

// RevoluteJoint pins a local anchor on A to a local anchor on B so both world anchor points
// remain coincident while allowing relative rotation around the hinge, with optional angular
// limits, angular motor, and break thresholds.
type RevoluteJoint struct {
	A, B             *RigidBody
	AnchorA          core.Point
	AnchorB          core.Point
	ReferenceAngle   float32 // initial relative angle B.Angle - A.Angle
	EnableLimit      bool
	LowerAngle       float32 // radians
	UpperAngle       float32 // radians
	EnableMotor      bool
	MotorSpeed       float32 // target relative angular velocity (rad/s)
	MaxMotorTorque   float32 // maximum motor torque
	BreakForce       float32 // max reaction force before breaking (<= 0 disables)
	BreakTorque      float32 // max reaction torque before breaking (<= 0 disables)
	CollideConnected bool

	jointReaction
	active       bool
	dt           float64
	rAx, rAy     float64
	rBx, rBy     float64
	m11, m12     float64
	m22          float64
	motorMass    float64
	motorImpulse float64
	limitImpulse float64
	limitState   int // 0 = inactive, -1 = lower, +1 = upper, 2 = locked
}

// NewRevoluteJoint creates a RevoluteJoint pinning a and b together at worldPivot.
func NewRevoluteJoint(a, b *RigidBody, worldPivot core.Point) *RevoluteJoint {
	j := &RevoluteJoint{A: a, B: b}
	if a != nil {
		j.AnchorA = a.WorldToLocal(worldPivot)
	}
	if b != nil {
		j.AnchorB = b.WorldToLocal(worldPivot)
	}
	if a != nil && b != nil {
		j.ReferenceAngle = b.Angle - a.Angle
	}
	return j
}

func (j *RevoluteJoint) jointBodyPair() (*RigidBody, *RigidBody) { return j.A, j.B }
func (j *RevoluteJoint) collideConnected() bool                  { return j.CollideConnected }
func (j *RevoluteJoint) checkBreak() (JointBreakEvent, bool) {
	return j.evalBreak(j, j.A, j.B, j.BreakForce, j.BreakTorque)
}

// JointAngle returns the current relative rotation in radians: B.Angle - A.Angle - ReferenceAngle.
func (j *RevoluteJoint) JointAngle() float32 {
	if j.A == nil || j.B == nil {
		return 0
	}
	return float32((float64(j.B.Angle) - float64(j.A.Angle)) - float64(j.ReferenceAngle))
}

// JointSpeed returns the current relative angular velocity in rad/s: B.AngularVel - A.AngularVel.
func (j *RevoluteJoint) JointSpeed() float32 {
	if j.A == nil || j.B == nil {
		return 0
	}
	return j.B.AngularVel - j.A.AngularVel
}

func revoluteEffMass(a, b *RigidBody, rAx, rAy, rBx, rBy float64) (m11, m12, m22 float64, ok bool) {
	invMA, invIA := float64(a.InvMass), float64(a.InvInertia)
	invMB, invIB := float64(b.InvMass), float64(b.InvInertia)

	k11 := float64(invMA+invMB) + float64(float64(invIA*rAy)*rAy) + float64(float64(invIB*rBy)*rBy)
	k12 := float64(float64(-invIA*rAx)*rAy) - float64(float64(invIB*rBx)*rBy)
	k22 := float64(invMA+invMB) + float64(float64(invIA*rAx)*rAx) + float64(float64(invIB*rBx)*rBx)

	det := float64(k11*k22) - float64(k12*k12)
	if math.Abs(det) <= 1e-12 {
		return 0, 0, 0, false
	}
	invDet := 1.0 / det
	return k22 * invDet, -k12 * invDet, k11 * invDet, true
}

func (j *RevoluteJoint) preStep(dt float64) {
	j.active = false
	j.dt = dt
	j.motorMass = 0
	j.motorImpulse = 0
	j.limitImpulse = 0
	j.limitState = 0
	if j.A == nil || j.B == nil || (j.A.Static && j.B.Static) {
		return
	}
	pA := j.A.LocalToWorld(j.AnchorA)
	pB := j.B.LocalToWorld(j.AnchorB)
	j.rAx = float64(pA.X) - float64(j.A.X)
	j.rAy = float64(pA.Y) - float64(j.A.Y)
	j.rBx = float64(pB.X) - float64(j.B.X)
	j.rBy = float64(pB.Y) - float64(j.B.Y)

	m11, m12, m22, ok := revoluteEffMass(j.A, j.B, j.rAx, j.rAy, j.rBx, j.rBy)
	if !ok {
		return
	}
	j.m11, j.m12, j.m22 = m11, m12, m22

	invISum := float64(j.A.InvInertia) + float64(j.B.InvInertia)
	if invISum > 1e-12 {
		j.motorMass = 1.0 / invISum
		if j.EnableLimit && j.LowerAngle <= j.UpperAngle {
			angle := (float64(j.B.Angle) - float64(j.A.Angle)) - float64(j.ReferenceAngle)
			lower := float64(j.LowerAngle)
			upper := float64(j.UpperAngle)
			if math.Abs(upper-lower) <= 1e-6 {
				j.limitState = 2
			} else if angle <= lower+1e-4 {
				j.limitState = -1
			} else if angle >= upper-1e-4 {
				j.limitState = 1
			}
		}
	}

	j.active = true
}

func (j *RevoluteJoint) solveVelocity() {
	if !j.active {
		return
	}

	// 1. Angular motor (solved before limits so limits always override motor torque).
	if j.EnableMotor && j.motorMass > 0 && j.MaxMotorTorque > 0 && j.limitState != 2 && j.dt > 0 {
		wRel := float64(j.B.AngularVel) - float64(j.A.AngularVel)
		dLambda := float64((float64(j.MotorSpeed) - wRel) * j.motorMass)
		maxImp := float64(j.MaxMotorTorque) * j.dt
		oldImp := j.motorImpulse
		j.motorImpulse = clamp64(oldImp+dLambda, -maxImp, maxImp)
		applied := j.motorImpulse - oldImp
		if applied != 0 {
			applyAngularImpulse2D(j.A, j.B, applied)
			j.addAngularImpulse(applied)
		}
	}

	// 2. Point-to-point 2-DOF anchor constraint.
	vAx := float64(j.A.VX) - float64(float64(j.A.AngularVel)*j.rAy)
	vAy := float64(j.A.VY) + float64(float64(j.A.AngularVel)*j.rAx)
	vBx := float64(j.B.VX) - float64(float64(j.B.AngularVel)*j.rBy)
	vBy := float64(j.B.VY) + float64(float64(j.B.AngularVel)*j.rBx)

	dvx := vBx - vAx
	dvy := vBy - vAy
	px := -(float64(j.m11*dvx) + float64(j.m12*dvy))
	py := -(float64(j.m12*dvx) + float64(j.m22*dvy))

	j.A.applyImpulseRaw(-px, -py, j.rAx, j.rAy)
	j.B.applyImpulseRaw(px, py, j.rBx, j.rBy)
	j.addLinearImpulse(px, py)

	// 3. Angular limit constraint (solved after motor and anchor so limits strictly hold).
	if j.limitState != 0 && j.motorMass > 0 {
		wRel := float64(j.B.AngularVel) - float64(j.A.AngularVel)
		dLambda := float64(-wRel * j.motorMass)
		oldImp := j.limitImpulse
		newImp := oldImp + dLambda
		if j.limitState < 0 && newImp < 0 {
			newImp = 0
		} else if j.limitState == 1 && newImp > 0 {
			newImp = 0
		}
		applied := newImp - oldImp
		j.limitImpulse = newImp
		if applied != 0 {
			applyAngularImpulse2D(j.A, j.B, applied)
			j.addAngularImpulse(applied)
		}
	}
}

func (j *RevoluteJoint) solvePosition() {
	if !j.active {
		return
	}

	// 1. Clamp angular limit violation first so the subsequent point projection preserves the pivot.
	if j.EnableLimit && j.LowerAngle <= j.UpperAngle {
		invIA, invIB := float64(j.A.InvInertia), float64(j.B.InvInertia)
		invISum := invIA + invIB
		if invISum > 1e-12 {
			angle := (float64(j.B.Angle) - float64(j.A.Angle)) - float64(j.ReferenceAngle)
			lower, upper := float64(j.LowerAngle), float64(j.UpperAngle)
			var angErr float64
			if math.Abs(upper-lower) <= 1e-6 {
				angErr = angle - lower
			} else if angle < lower {
				angErr = angle - lower
			} else if angle > upper {
				angErr = angle - upper
			}
			if math.Abs(angErr) > 1e-6 {
				angImp := -angErr / invISum
				if !j.A.Static {
					j.A.Angle = float32(float64(j.A.Angle) - float64(angImp*invIA))
				}
				if !j.B.Static {
					j.B.Angle = float32(float64(j.B.Angle) + float64(angImp*invIB))
				}
			}
		}
	}

	// 2. Point-to-point positional projection.
	pA := j.A.LocalToWorld(j.AnchorA)
	pB := j.B.LocalToWorld(j.AnchorB)
	cx := float64(pB.X) - float64(pA.X)
	cy := float64(pB.Y) - float64(pA.Y)
	if float64(cx*cx)+float64(cy*cy) <= 1e-12 {
		return
	}

	rAx := float64(pA.X) - float64(j.A.X)
	rAy := float64(pA.Y) - float64(j.A.Y)
	rBx := float64(pB.X) - float64(j.B.X)
	rBy := float64(pB.Y) - float64(j.B.Y)

	m11, m12, m22, ok := revoluteEffMass(j.A, j.B, rAx, rAy, rBx, rBy)
	if !ok {
		return
	}
	px := -(float64(m11*cx) + float64(m12*cy))
	py := -(float64(m12*cx) + float64(m22*cy))

	if !j.A.Static {
		j.A.X = float32(float64(j.A.X) - float64(px*float64(j.A.InvMass)))
		j.A.Y = float32(float64(j.A.Y) - float64(py*float64(j.A.InvMass)))
		crossA := float64(rAx*py) - float64(rAy*px)
		j.A.Angle = float32(float64(j.A.Angle) - float64(crossA*float64(j.A.InvInertia)))
	}
	if !j.B.Static {
		j.B.X = float32(float64(j.B.X) + float64(px*float64(j.B.InvMass)))
		j.B.Y = float32(float64(j.B.Y) + float64(py*float64(j.B.InvMass)))
		crossB := float64(rBx*py) - float64(rBy*px)
		j.B.Angle = float32(float64(j.B.Angle) + float64(crossB*float64(j.B.InvInertia)))
	}
}

// PrismaticJoint (slider) constrains body B to translate relative to body A along
// LocalAxisA with locked relative orientation, optional translation limits, a linear
// motor, and break thresholds.
type PrismaticJoint struct {
	A, B             *RigidBody
	AnchorA          core.Point
	AnchorB          core.Point
	LocalAxisA       core.Point // unit slide axis in A's local space (defaults to {X: 1, Y: 0})
	ReferenceAngle   float32    // locked relative angle B.Angle - A.Angle
	EnableLimit      bool
	LowerTranslation float32
	UpperTranslation float32
	EnableMotor      bool
	MotorSpeed       float32 // target linear speed along slide axis (units/s)
	MaxMotorForce    float32 // max linear motor force
	BreakForce       float32 // max reaction force before breaking (<= 0 disables)
	BreakTorque      float32 // max reaction torque before breaking (<= 0 disables)
	CollideConnected bool

	jointReaction
	active       bool
	dt           float64
	ax, ay       float64 // world unit slide axis
	px, py       float64 // world unit perpendicular axis (-ay, ax)
	s1, s2       float64 // lever cross products for perpendicular constraint
	a1, a2       float64 // lever cross products for axis constraint
	perpM11      float64 // inverse of 2x2 [perp, angle] K matrix (or scalar 1/K11 when rot locked)
	perpM12      float64
	perpM22      float64
	rotLocked    bool
	motorMass    float64
	motorImpulse float64
	limitImpulse float64
	limitState   int // 0 = inactive, -1 = lower, +1 = upper, 2 = locked
}

// NewPrismaticJoint creates a PrismaticJoint sliding along worldAxis through worldAnchor.
// If worldAxis is zero, LocalAxisA defaults to {X: 1, Y: 0}.
func NewPrismaticJoint(a, b *RigidBody, worldAnchor, worldAxis core.Point) *PrismaticJoint {
	j := &PrismaticJoint{
		A:          a,
		B:          b,
		LocalAxisA: bodyLocalDir(a, worldAxis, 1, 0),
	}
	if a != nil {
		j.AnchorA = a.WorldToLocal(worldAnchor)
	}
	if b != nil {
		j.AnchorB = b.WorldToLocal(worldAnchor)
	}
	if a != nil && b != nil {
		j.ReferenceAngle = b.Angle - a.Angle
	}
	return j
}

func (j *PrismaticJoint) jointBodyPair() (*RigidBody, *RigidBody) { return j.A, j.B }
func (j *PrismaticJoint) collideConnected() bool                  { return j.CollideConnected }
func (j *PrismaticJoint) checkBreak() (JointBreakEvent, bool) {
	return j.evalBreak(j, j.A, j.B, j.BreakForce, j.BreakTorque)
}

// JointTranslation returns the current signed displacement of AnchorB relative to
// AnchorA along the world slide axis.
func (j *PrismaticJoint) JointTranslation() float32 {
	if j.A == nil || j.B == nil {
		return 0
	}
	pA := j.A.LocalToWorld(j.AnchorA)
	pB := j.B.LocalToWorld(j.AnchorB)
	ax, ay := bodyWorldDir(j.A, j.LocalAxisA, 1, 0)
	dx := float64(pB.X) - float64(pA.X)
	dy := float64(pB.Y) - float64(pA.Y)
	return float32(float64(dx*ax) + float64(dy*ay))
}

// JointSpeed returns the current relative linear velocity along the world slide axis.
func (j *PrismaticJoint) JointSpeed() float32 {
	if j.A == nil || j.B == nil {
		return 0
	}
	pA := j.A.LocalToWorld(j.AnchorA)
	pB := j.B.LocalToWorld(j.AnchorB)
	ax, ay := bodyWorldDir(j.A, j.LocalAxisA, 1, 0)
	rAx := float64(pA.X) - float64(j.A.X)
	rAy := float64(pA.Y) - float64(j.A.Y)
	rBx := float64(pB.X) - float64(j.B.X)
	rBy := float64(pB.Y) - float64(j.B.Y)
	dx := float64(pB.X) - float64(pA.X)
	dy := float64(pB.Y) - float64(pA.Y)
	a1 := float64((rAx+dx)*ay) - float64((rAy+dy)*ax)
	a2 := float64(rBx*ay) - float64(rBy*ax)
	vAxis := float64((float64(j.B.VX)-float64(j.A.VX))*ax) +
		float64((float64(j.B.VY)-float64(j.A.VY))*ay) +
		float64(a2*float64(j.B.AngularVel)) -
		float64(a1*float64(j.A.AngularVel))
	return float32(vAxis)
}

func prismaticPerpEffMass(invMA, invIA, invMB, invIB, s1, s2 float64) (m11, m12, m22 float64, rotLocked, ok bool) {
	k11 := float64(invMA+invMB) + float64(float64(invIA*s1)*s1) + float64(float64(invIB*s2)*s2)
	k12 := float64(invIA*s1) + float64(invIB*s2)
	k22 := invIA + invIB
	if k22 <= 1e-12 {
		if k11 <= 1e-12 {
			return 0, 0, 0, true, false
		}
		return 1.0 / k11, 0, 0, true, true
	}
	det := float64(k11*k22) - float64(k12*k12)
	if math.Abs(det) <= 1e-12 {
		return 0, 0, 0, false, false
	}
	invDet := 1.0 / det
	return k22 * invDet, -k12 * invDet, k11 * invDet, false, true
}

func (j *PrismaticJoint) preStep(dt float64) {
	j.active = false
	j.dt = dt
	j.motorMass = 0
	j.motorImpulse = 0
	j.limitImpulse = 0
	j.limitState = 0
	if j.A == nil || j.B == nil || (j.A.Static && j.B.Static) {
		return
	}

	pA := j.A.LocalToWorld(j.AnchorA)
	pB := j.B.LocalToWorld(j.AnchorB)
	rAx := float64(pA.X) - float64(j.A.X)
	rAy := float64(pA.Y) - float64(j.A.Y)
	rBx := float64(pB.X) - float64(j.B.X)
	rBy := float64(pB.Y) - float64(j.B.Y)
	dx := float64(pB.X) - float64(pA.X)
	dy := float64(pB.Y) - float64(pA.Y)

	ax, ay := bodyWorldDir(j.A, j.LocalAxisA, 1, 0)
	px, py := -ay, ax
	j.ax, j.ay = ax, ay
	j.px, j.py = px, py

	dRx, dRy := rAx+dx, rAy+dy
	j.s1 = float64(dRx*py) - float64(dRy*px)
	j.s2 = float64(rBx*py) - float64(rBy*px)
	j.a1 = float64(dRx*ay) - float64(dRy*ax)
	j.a2 = float64(rBx*ay) - float64(rBy*ax)

	invMA, invIA := float64(j.A.InvMass), float64(j.A.InvInertia)
	invMB, invIB := float64(j.B.InvMass), float64(j.B.InvInertia)

	m11, m12, m22, rotLocked, ok := prismaticPerpEffMass(invMA, invIA, invMB, invIB, j.s1, j.s2)
	if !ok {
		return
	}
	j.perpM11, j.perpM12, j.perpM22 = m11, m12, m22
	j.rotLocked = rotLocked

	kAxis := float64(invMA+invMB) + float64(float64(invIA*j.a1)*j.a1) + float64(float64(invIB*j.a2)*j.a2)
	if kAxis > 1e-12 {
		j.motorMass = 1.0 / kAxis
		if j.EnableLimit && j.LowerTranslation <= j.UpperTranslation {
			tr := float64(dx*ax) + float64(dy*ay)
			lower, upper := float64(j.LowerTranslation), float64(j.UpperTranslation)
			if math.Abs(upper-lower) <= 1e-6 {
				j.limitState = 2
			} else if tr <= lower+1e-4 {
				j.limitState = -1
			} else if tr >= upper-1e-4 {
				j.limitState = 1
			}
		}
	}

	j.active = true
}

func (j *PrismaticJoint) applyAxisImpulse(imp float64) {
	if imp == 0 {
		return
	}
	lx, ly := float64(imp*j.ax), float64(imp*j.ay)
	if !j.A.Static {
		j.A.VX = float32(float64(j.A.VX) - float64(lx*float64(j.A.InvMass)))
		j.A.VY = float32(float64(j.A.VY) - float64(ly*float64(j.A.InvMass)))
		j.A.AngularVel = float32(float64(j.A.AngularVel) - float64(float64(imp*j.a1)*float64(j.A.InvInertia)))
	}
	if !j.B.Static {
		j.B.VX = float32(float64(j.B.VX) + float64(lx*float64(j.B.InvMass)))
		j.B.VY = float32(float64(j.B.VY) + float64(ly*float64(j.B.InvMass)))
		j.B.AngularVel = float32(float64(j.B.AngularVel) + float64(float64(imp*j.a2)*float64(j.B.InvInertia)))
	}
	j.addLinearImpulse(lx, ly)
}

func (j *PrismaticJoint) solveVelocity() {
	if !j.active {
		return
	}

	// 1. Linear motor along slide axis (solved before limits).
	if j.EnableMotor && j.motorMass > 0 && j.MaxMotorForce > 0 && j.limitState != 2 && j.dt > 0 {
		vAxis := float64((float64(j.B.VX)-float64(j.A.VX))*j.ax) +
			float64((float64(j.B.VY)-float64(j.A.VY))*j.ay) +
			float64(j.a2*float64(j.B.AngularVel)) -
			float64(j.a1*float64(j.A.AngularVel))
		dLambda := float64((float64(j.MotorSpeed) - vAxis) * j.motorMass)
		maxImp := float64(j.MaxMotorForce) * j.dt
		oldImp := j.motorImpulse
		j.motorImpulse = clamp64(oldImp+dLambda, -maxImp, maxImp)
		j.applyAxisImpulse(j.motorImpulse - oldImp)
	}

	// 2. Coupled perpendicular + angular lock.
	cDotPerp := float64((float64(j.B.VX)-float64(j.A.VX))*j.px) +
		float64((float64(j.B.VY)-float64(j.A.VY))*j.py) +
		float64(j.s2*float64(j.B.AngularVel)) -
		float64(j.s1*float64(j.A.AngularVel))
	cDotAng := float64(j.B.AngularVel) - float64(j.A.AngularVel)

	var lamPerp, lamAng float64
	if j.rotLocked {
		lamPerp = float64(-cDotPerp * j.perpM11)
	} else {
		lamPerp = -(float64(j.perpM11*cDotPerp) + float64(j.perpM12*cDotAng))
		lamAng = -(float64(j.perpM12*cDotPerp) + float64(j.perpM22*cDotAng))
	}

	pLx, pLy := float64(lamPerp*j.px), float64(lamPerp*j.py)
	if !j.A.Static {
		j.A.VX = float32(float64(j.A.VX) - float64(pLx*float64(j.A.InvMass)))
		j.A.VY = float32(float64(j.A.VY) - float64(pLy*float64(j.A.InvMass)))
		angA := float64(lamPerp*j.s1) + lamAng
		j.A.AngularVel = float32(float64(j.A.AngularVel) - float64(angA*float64(j.A.InvInertia)))
	}
	if !j.B.Static {
		j.B.VX = float32(float64(j.B.VX) + float64(pLx*float64(j.B.InvMass)))
		j.B.VY = float32(float64(j.B.VY) + float64(pLy*float64(j.B.InvMass)))
		angB := float64(lamPerp*j.s2) + lamAng
		j.B.AngularVel = float32(float64(j.B.AngularVel) + float64(angB*float64(j.B.InvInertia)))
	}
	j.addLinearImpulse(pLx, pLy)
	j.addAngularImpulse(lamAng)

	// 3. Linear translation limits along slide axis.
	if j.limitState != 0 && j.motorMass > 0 {
		vAxis := float64((float64(j.B.VX)-float64(j.A.VX))*j.ax) +
			float64((float64(j.B.VY)-float64(j.A.VY))*j.ay) +
			float64(j.a2*float64(j.B.AngularVel)) -
			float64(j.a1*float64(j.A.AngularVel))
		dLambda := float64(-vAxis * j.motorMass)
		oldImp := j.limitImpulse
		newImp := oldImp + dLambda
		if j.limitState < 0 && newImp < 0 {
			newImp = 0
		} else if j.limitState == 1 && newImp > 0 {
			newImp = 0
		}
		j.limitImpulse = newImp
		j.applyAxisImpulse(newImp - oldImp)
	}
}

func (j *PrismaticJoint) solvePosition() {
	if !j.active {
		return
	}
	pA := j.A.LocalToWorld(j.AnchorA)
	pB := j.B.LocalToWorld(j.AnchorB)
	rAx := float64(pA.X) - float64(j.A.X)
	rAy := float64(pA.Y) - float64(j.A.Y)
	rBx := float64(pB.X) - float64(j.B.X)
	rBy := float64(pB.Y) - float64(j.B.Y)
	dx := float64(pB.X) - float64(pA.X)
	dy := float64(pB.Y) - float64(pA.Y)

	ax, ay := bodyWorldDir(j.A, j.LocalAxisA, 1, 0)
	px, py := -ay, ax
	dRx, dRy := rAx+dx, rAy+dy
	s1 := float64(dRx*py) - float64(dRy*px)
	s2 := float64(rBx*py) - float64(rBy*px)

	invMA, invIA := float64(j.A.InvMass), float64(j.A.InvInertia)
	invMB, invIB := float64(j.B.InvMass), float64(j.B.InvInertia)

	// 1. Correct perpendicular and angular drift.
	cPerp := float64(dx*px) + float64(dy*py)
	cAng := (float64(j.B.Angle) - float64(j.A.Angle)) - float64(j.ReferenceAngle)
	if math.Abs(cPerp) > 1e-6 || math.Abs(cAng) > 1e-6 {
		if m11, m12, m22, rotLocked, ok := prismaticPerpEffMass(invMA, invIA, invMB, invIB, s1, s2); ok {
			var lamPerp, lamAng float64
			if rotLocked {
				lamPerp = -cPerp * m11
			} else {
				lamPerp = -(float64(m11*cPerp) + float64(m12*cAng))
				lamAng = -(float64(m12*cPerp) + float64(m22*cAng))
			}
			pLx, pLy := float64(lamPerp*px), float64(lamPerp*py)
			if !j.A.Static {
				j.A.X = float32(float64(j.A.X) - float64(pLx*invMA))
				j.A.Y = float32(float64(j.A.Y) - float64(pLy*invMA))
				j.A.Angle = float32(float64(j.A.Angle) - float64((float64(lamPerp*s1)+lamAng)*invIA))
			}
			if !j.B.Static {
				j.B.X = float32(float64(j.B.X) + float64(pLx*invMB))
				j.B.Y = float32(float64(j.B.Y) + float64(pLy*invMB))
				j.B.Angle = float32(float64(j.B.Angle) + float64((float64(lamPerp*s2)+lamAng)*invIB))
			}
		}
	}

	// 2. Clamp translation limits along slide axis.
	if j.EnableLimit && j.LowerTranslation <= j.UpperTranslation {
		pA = j.A.LocalToWorld(j.AnchorA)
		pB = j.B.LocalToWorld(j.AnchorB)
		rAx = float64(pA.X) - float64(j.A.X)
		rAy = float64(pA.Y) - float64(j.A.Y)
		rBx = float64(pB.X) - float64(j.B.X)
		rBy = float64(pB.Y) - float64(j.B.Y)
		dx = float64(pB.X) - float64(pA.X)
		dy = float64(pB.Y) - float64(pA.Y)
		ax, ay = bodyWorldDir(j.A, j.LocalAxisA, 1, 0)
		tr := float64(dx*ax) + float64(dy*ay)
		lower, upper := float64(j.LowerTranslation), float64(j.UpperTranslation)

		var err float64
		if math.Abs(upper-lower) <= 1e-6 {
			err = tr - lower
		} else if tr < lower {
			err = tr - lower
		} else if tr > upper {
			err = tr - upper
		}
		if math.Abs(err) > 1e-6 {
			a1 := float64((rAx+dx)*ay) - float64((rAy+dy)*ax)
			a2 := float64(rBx*ay) - float64(rBy*ax)
			kAxis := float64(invMA+invMB) + float64(float64(invIA*a1)*a1) + float64(float64(invIB*a2)*a2)
			if kAxis > 1e-12 {
				lam := -err / kAxis
				lx, ly := float64(lam*ax), float64(lam*ay)
				if !j.A.Static {
					j.A.X = float32(float64(j.A.X) - float64(lx*invMA))
					j.A.Y = float32(float64(j.A.Y) - float64(ly*invMA))
					j.A.Angle = float32(float64(j.A.Angle) - float64(float64(lam*a1)*invIA))
				}
				if !j.B.Static {
					j.B.X = float32(float64(j.B.X) + float64(lx*invMB))
					j.B.Y = float32(float64(j.B.Y) + float64(ly*invMB))
					j.B.Angle = float32(float64(j.B.Angle) + float64(float64(lam*a2)*invIB))
				}
			}
		}
	}
}

// WeldJoint locks two bodies' relative position and angle at a shared anchor point.
// When FrequencyHz > 0 (with DampingRatio) or Stiffness > 0 (with Damping), it behaves
// as a soft damped spring holding the relative pose; otherwise it acts as a rigid weld.
type WeldJoint struct {
	A, B             *RigidBody
	AnchorA          core.Point
	AnchorB          core.Point
	ReferenceAngle   float32 // target relative angle B.Angle - A.Angle
	FrequencyHz      float32 // optional spring natural frequency in Hz (> 0 enables soft spring mode)
	DampingRatio     float32 // damping ratio for FrequencyHz (0 = undamped, 1 = critically damped)
	Stiffness        float32 // optional direct spring stiffness (used when FrequencyHz <= 0)
	Damping          float32 // optional direct damping (used when FrequencyHz <= 0)
	BreakForce       float32 // max reaction force before breaking (<= 0 disables)
	BreakTorque      float32 // max reaction torque before breaking (<= 0 disables)
	CollideConnected bool

	jointReaction
	active     bool
	soft       bool
	dt         float64
	rAx, rAy   float64
	rBx, rBy   float64
	k          symMat3_64
	gammaLin   float64
	gammaAng   float64
	biasX      float64
	biasY      float64
	biasAng    float64
	impulseX   float64
	impulseY   float64
	angImpulse float64
}

// NewWeldJoint creates a WeldJoint welding a and b together at worldAnchor with
// their current relative orientation.
func NewWeldJoint(a, b *RigidBody, worldAnchor core.Point) *WeldJoint {
	j := &WeldJoint{A: a, B: b}
	if a != nil {
		j.AnchorA = a.WorldToLocal(worldAnchor)
	}
	if b != nil {
		j.AnchorB = b.WorldToLocal(worldAnchor)
	}
	if a != nil && b != nil {
		j.ReferenceAngle = b.Angle - a.Angle
	}
	return j
}

func (j *WeldJoint) jointBodyPair() (*RigidBody, *RigidBody) { return j.A, j.B }
func (j *WeldJoint) collideConnected() bool                  { return j.CollideConnected }
func (j *WeldJoint) checkBreak() (JointBreakEvent, bool) {
	return j.evalBreak(j, j.A, j.B, j.BreakForce, j.BreakTorque)
}

func weldMassMatrix(a, b *RigidBody, rAx, rAy, rBx, rBy, gammaLin, gammaAng float64) symMat3_64 {
	invMA, invIA := float64(a.InvMass), float64(a.InvInertia)
	invMB, invIB := float64(b.InvMass), float64(b.InvInertia)
	invM := invMA + invMB
	return symMat3_64{
		a: invM + float64(float64(invIA*rAy)*rAy) + float64(float64(invIB*rBy)*rBy) + gammaLin,
		b: invM + float64(float64(invIA*rAx)*rAx) + float64(float64(invIB*rBx)*rBx) + gammaLin,
		c: float64(invIA+invIB) + gammaAng,
		d: float64(float64(-invIA*rAx)*rAy) - float64(float64(invIB*rBx)*rBy),
		e: float64(-invIA*rAy) - float64(invIB*rBy),
		f: float64(invIA*rAx) + float64(invIB*rBx),
	}
}

func (j *WeldJoint) preStep(dt float64) {
	j.active = false
	j.soft = false
	j.dt = dt
	j.gammaLin = 0
	j.gammaAng = 0
	j.biasX = 0
	j.biasY = 0
	j.biasAng = 0
	j.impulseX = 0
	j.impulseY = 0
	j.angImpulse = 0
	if j.A == nil || j.B == nil || (j.A.Static && j.B.Static) || dt <= 0 {
		return
	}

	pA := j.A.LocalToWorld(j.AnchorA)
	pB := j.B.LocalToWorld(j.AnchorB)
	j.rAx = float64(pA.X) - float64(j.A.X)
	j.rAy = float64(pA.Y) - float64(j.A.Y)
	j.rBx = float64(pB.X) - float64(j.B.X)
	j.rBy = float64(pB.Y) - float64(j.B.Y)

	invMSum := float64(j.A.InvMass) + float64(j.B.InvMass)
	invISum := float64(j.A.InvInertia) + float64(j.B.InvInertia)
	cx := float64(pB.X) - float64(pA.X)
	cy := float64(pB.Y) - float64(pA.Y)
	cAng := (float64(j.B.Angle) - float64(j.A.Angle)) - float64(j.ReferenceAngle)

	if j.FrequencyHz > 0 {
		j.soft = true
		freq := float64(j.FrequencyHz)
		zeta := max(0, float64(j.DampingRatio))
		if invMSum > 1e-12 {
			kLin, cLin := springCoeffsFromFreq(1.0/invMSum, freq, zeta)
			if g, bx, ok := softSpringParams64(kLin, cLin, cx, dt); ok {
				j.gammaLin = g
				j.biasX = bx
				_, j.biasY, _ = softSpringParams64(kLin, cLin, cy, dt)
			}
		}
		if invISum > 1e-12 {
			kAng, cAngD := springCoeffsFromFreq(1.0/invISum, freq, zeta)
			if g, ba, ok := softSpringParams64(kAng, cAngD, cAng, dt); ok {
				j.gammaAng = g
				j.biasAng = ba
			}
		}
	} else if j.Stiffness > 0 {
		j.soft = true
		kVal := float64(j.Stiffness)
		cVal := max(0, float64(j.Damping))
		if g, bx, ok := softSpringParams64(kVal, cVal, cx, dt); ok {
			j.gammaLin = g
			j.gammaAng = g
			j.biasX = bx
			_, j.biasY, _ = softSpringParams64(kVal, cVal, cy, dt)
			_, j.biasAng, _ = softSpringParams64(kVal, cVal, cAng, dt)
		}
	}

	j.k = weldMassMatrix(j.A, j.B, j.rAx, j.rAy, j.rBx, j.rBy, j.gammaLin, j.gammaAng)
	j.active = true
}

func (j *WeldJoint) solveVelocity() {
	if !j.active {
		return
	}
	vAx := float64(j.A.VX) - float64(float64(j.A.AngularVel)*j.rAy)
	vAy := float64(j.A.VY) + float64(float64(j.A.AngularVel)*j.rAx)
	vBx := float64(j.B.VX) - float64(float64(j.B.AngularVel)*j.rBy)
	vBy := float64(j.B.VY) + float64(float64(j.B.AngularVel)*j.rBx)

	dvx := vBx - vAx
	dvy := vBy - vAy
	dw := float64(j.B.AngularVel) - float64(j.A.AngularVel)

	rhsX := -(dvx + j.biasX + float64(j.gammaLin*j.impulseX))
	rhsY := -(dvy + j.biasY + float64(j.gammaLin*j.impulseY))
	rhsW := -(dw + j.biasAng + float64(j.gammaAng*j.angImpulse))

	dPx, dPy, dL, ok := j.k.solve(rhsX, rhsY, rhsW)
	if !ok {
		// Degenerate inertia (e.g., locked rotation on both bodies): fall back to 2x2 anchor solve.
		det2 := float64(j.k.a*j.k.b) - float64(j.k.d*j.k.d)
		if math.Abs(det2) <= 1e-12 {
			return
		}
		invDet2 := 1.0 / det2
		dPx = float64((float64(j.k.b*rhsX) - float64(j.k.d*rhsY)) * invDet2)
		dPy = float64((float64(-j.k.d*rhsX) + float64(j.k.a*rhsY)) * invDet2)
		dL = 0
	}

	j.impulseX += dPx
	j.impulseY += dPy
	j.angImpulse += dL

	j.A.applyImpulseRaw(-dPx, -dPy, j.rAx, j.rAy)
	j.B.applyImpulseRaw(dPx, dPy, j.rBx, j.rBy)
	applyAngularImpulse2D(j.A, j.B, dL)
	j.addLinearImpulse(dPx, dPy)
	j.addAngularImpulse(dL)
}

func (j *WeldJoint) solvePosition() {
	if !j.active || j.soft {
		return
	}
	pA := j.A.LocalToWorld(j.AnchorA)
	pB := j.B.LocalToWorld(j.AnchorB)
	cx := float64(pB.X) - float64(pA.X)
	cy := float64(pB.Y) - float64(pA.Y)
	cAng := (float64(j.B.Angle) - float64(j.A.Angle)) - float64(j.ReferenceAngle)
	if float64(cx*cx)+float64(cy*cy) <= 1e-12 && math.Abs(cAng) <= 1e-6 {
		return
	}

	rAx := float64(pA.X) - float64(j.A.X)
	rAy := float64(pA.Y) - float64(j.A.Y)
	rBx := float64(pB.X) - float64(j.B.X)
	rBy := float64(pB.Y) - float64(j.B.Y)
	km := weldMassMatrix(j.A, j.B, rAx, rAy, rBx, rBy, 0, 0)

	px, py, l, ok := km.solve(-cx, -cy, -cAng)
	if !ok {
		det2 := float64(km.a*km.b) - float64(km.d*km.d)
		if math.Abs(det2) <= 1e-12 {
			return
		}
		invDet2 := 1.0 / det2
		px = float64((float64(km.b*(-cx)) - float64(km.d*(-cy))) * invDet2)
		py = float64((float64(-km.d*(-cx)) + float64(km.a*(-cy))) * invDet2)
		l = 0
	}

	if !j.A.Static {
		j.A.X = float32(float64(j.A.X) - float64(px*float64(j.A.InvMass)))
		j.A.Y = float32(float64(j.A.Y) - float64(py*float64(j.A.InvMass)))
		crossA := float64(rAx*py) - float64(rAy*px) + l
		j.A.Angle = float32(float64(j.A.Angle) - float64(crossA*float64(j.A.InvInertia)))
	}
	if !j.B.Static {
		j.B.X = float32(float64(j.B.X) + float64(px*float64(j.B.InvMass)))
		j.B.Y = float32(float64(j.B.Y) + float64(py*float64(j.B.InvMass)))
		crossB := float64(rBx*py) - float64(rBy*px) + l
		j.B.Angle = float32(float64(j.B.Angle) + float64(crossB*float64(j.B.InvInertia)))
	}
}

// WheelJoint connects a chassis (A) and a wheel (B) with a 1-DOF suspension axis
// (spring + damping + optional translation limits) and a rotational drive motor.
type WheelJoint struct {
	A, B             *RigidBody
	AnchorA          core.Point // suspension mount in A's local space
	AnchorB          core.Point // wheel hub in B's local space
	LocalAxisA       core.Point // unit suspension axis in A's local space (defaults to {X: 0, Y: 1})
	FrequencyHz      float32    // optional suspension natural frequency in Hz (> 0 computes spring from effective mass)
	DampingRatio     float32    // damping ratio for FrequencyHz (e.g. 0.7)
	Stiffness        float32    // direct linear spring stiffness along suspension axis (used when FrequencyHz <= 0)
	Damping          float32    // direct linear damping along suspension axis (used when FrequencyHz <= 0)
	EnableLimit      bool       // enables suspension travel limits along LocalAxisA
	LowerTranslation float32
	UpperTranslation float32
	EnableMotor      bool
	MotorSpeed       float32 // target wheel angular velocity relative to chassis (rad/s)
	MaxMotorTorque   float32 // maximum drive motor torque
	BreakForce       float32 // max reaction force before breaking (<= 0 disables)
	BreakTorque      float32 // max reaction torque before breaking (<= 0 disables)
	CollideConnected bool

	jointReaction
	active        bool
	dt            float64
	ax, ay        float64 // world suspension axis
	px, py        float64 // world perpendicular axis (-ay, ax)
	sAx, sBx      float64 // lever arms for perpendicular constraint
	a1, a2        float64 // lever arms for suspension axis constraint
	perpMass      float64
	axisMass      float64
	springMass    float64
	motorMass     float64
	gamma         float64
	bias          float64
	springActive  bool
	springImpulse float64
	motorImpulse  float64
	limitImpulse  float64
	limitState    int // 0 = inactive, -1 = lower, +1 = upper, 2 = locked
}

// NewWheelJoint creates a WheelJoint attaching wheel b to chassis a at worldAnchor
// with suspension travel along worldAxis. If worldAxis is zero, it defaults to {X: 0, Y: 1}
// (vertical suspension in screen space).
func NewWheelJoint(a, b *RigidBody, worldAnchor, worldAxis core.Point) *WheelJoint {
	j := &WheelJoint{
		A:          a,
		B:          b,
		LocalAxisA: bodyLocalDir(a, worldAxis, 0, 1),
	}
	if a != nil {
		j.AnchorA = a.WorldToLocal(worldAnchor)
	}
	if b != nil {
		j.AnchorB = b.WorldToLocal(worldAnchor)
	}
	return j
}

func (j *WheelJoint) jointBodyPair() (*RigidBody, *RigidBody) { return j.A, j.B }
func (j *WheelJoint) collideConnected() bool                  { return j.CollideConnected }
func (j *WheelJoint) checkBreak() (JointBreakEvent, bool) {
	return j.evalBreak(j, j.A, j.B, j.BreakForce, j.BreakTorque)
}

// JointTranslation returns the current signed wheel displacement along the suspension axis.
func (j *WheelJoint) JointTranslation() float32 {
	if j.A == nil || j.B == nil {
		return 0
	}
	pA := j.A.LocalToWorld(j.AnchorA)
	pB := j.B.LocalToWorld(j.AnchorB)
	ax, ay := bodyWorldDir(j.A, j.LocalAxisA, 0, 1)
	dx := float64(pB.X) - float64(pA.X)
	dy := float64(pB.Y) - float64(pA.Y)
	return float32(float64(dx*ax) + float64(dy*ay))
}

// JointSpeed returns the wheel's angular speed relative to the chassis (B.AngularVel - A.AngularVel).
func (j *WheelJoint) JointSpeed() float32 {
	if j.A == nil || j.B == nil {
		return 0
	}
	return j.B.AngularVel - j.A.AngularVel
}

func (j *WheelJoint) preStep(dt float64) {
	j.active = false
	j.dt = dt
	j.perpMass = 0
	j.axisMass = 0
	j.springMass = 0
	j.motorMass = 0
	j.gamma = 0
	j.bias = 0
	j.springActive = false
	j.springImpulse = 0
	j.motorImpulse = 0
	j.limitImpulse = 0
	j.limitState = 0
	if j.A == nil || j.B == nil || (j.A.Static && j.B.Static) || dt <= 0 {
		return
	}

	pA := j.A.LocalToWorld(j.AnchorA)
	pB := j.B.LocalToWorld(j.AnchorB)
	rAx := float64(pA.X) - float64(j.A.X)
	rAy := float64(pA.Y) - float64(j.A.Y)
	rBx := float64(pB.X) - float64(j.B.X)
	rBy := float64(pB.Y) - float64(j.B.Y)
	dx := float64(pB.X) - float64(pA.X)
	dy := float64(pB.Y) - float64(pA.Y)

	ax, ay := bodyWorldDir(j.A, j.LocalAxisA, 0, 1)
	px, py := -ay, ax
	j.ax, j.ay = ax, ay
	j.px, j.py = px, py

	dRx, dRy := rAx+dx, rAy+dy
	j.sAx = float64(dRx*py) - float64(dRy*px)
	j.sBx = float64(rBx*py) - float64(rBy*px)
	j.a1 = float64(dRx*ay) - float64(dRy*ax)
	j.a2 = float64(rBx*ay) - float64(rBy*ax)

	invMA, invIA := float64(j.A.InvMass), float64(j.A.InvInertia)
	invMB, invIB := float64(j.B.InvMass), float64(j.B.InvInertia)

	kPerp := float64(invMA+invMB) + float64(float64(invIA*j.sAx)*j.sAx) + float64(float64(invIB*j.sBx)*j.sBx)
	if kPerp > 1e-12 {
		j.perpMass = 1.0 / kPerp
	}

	kAxis := float64(invMA+invMB) + float64(float64(invIA*j.a1)*j.a1) + float64(float64(invIB*j.a2)*j.a2)
	if kAxis > 1e-12 {
		j.axisMass = 1.0 / kAxis
		tr := float64(dx*ax) + float64(dy*ay)

		var kSpr, cSpr float64
		if j.FrequencyHz > 0 {
			kSpr, cSpr = springCoeffsFromFreq(j.axisMass, float64(j.FrequencyHz), max(0, float64(j.DampingRatio)))
		} else if j.Stiffness > 0 || j.Damping > 0 {
			kSpr = max(0, float64(j.Stiffness))
			cSpr = max(0, float64(j.Damping))
		}
		if kSpr > 0 || cSpr > 0 {
			if g, b, ok := softSpringParams64(kSpr, cSpr, tr, dt); ok {
				j.gamma = g
				j.bias = b
				j.springMass = 1.0 / (kAxis + g)
				j.springActive = true
			}
		}

		if j.EnableLimit && j.LowerTranslation <= j.UpperTranslation {
			lower, upper := float64(j.LowerTranslation), float64(j.UpperTranslation)
			if math.Abs(upper-lower) <= 1e-6 {
				j.limitState = 2
			} else if tr <= lower+1e-4 {
				j.limitState = -1
			} else if tr >= upper-1e-4 {
				j.limitState = 1
			}
		}
	}

	invISum := invIA + invIB
	if invISum > 1e-12 {
		j.motorMass = 1.0 / invISum
	}

	j.active = true
}

func (j *WheelJoint) applyWheelAxisImpulse(imp float64) {
	if imp == 0 {
		return
	}
	lx, ly := float64(imp*j.ax), float64(imp*j.ay)
	if !j.A.Static {
		j.A.VX = float32(float64(j.A.VX) - float64(lx*float64(j.A.InvMass)))
		j.A.VY = float32(float64(j.A.VY) - float64(ly*float64(j.A.InvMass)))
		j.A.AngularVel = float32(float64(j.A.AngularVel) - float64(float64(imp*j.a1)*float64(j.A.InvInertia)))
	}
	if !j.B.Static {
		j.B.VX = float32(float64(j.B.VX) + float64(lx*float64(j.B.InvMass)))
		j.B.VY = float32(float64(j.B.VY) + float64(ly*float64(j.B.InvMass)))
		j.B.AngularVel = float32(float64(j.B.AngularVel) + float64(float64(imp*j.a2)*float64(j.B.InvInertia)))
	}
	j.addLinearImpulse(lx, ly)
}

func (j *WheelJoint) solveVelocity() {
	if !j.active {
		return
	}

	// 1. Suspension spring along suspension axis.
	if j.springActive {
		vAxis := float64((float64(j.B.VX)-float64(j.A.VX))*j.ax) +
			float64((float64(j.B.VY)-float64(j.A.VY))*j.ay) +
			float64(j.a2*float64(j.B.AngularVel)) -
			float64(j.a1*float64(j.A.AngularVel))
		dLambda := float64(-j.springMass * (vAxis + j.bias + float64(j.gamma*j.springImpulse)))
		j.springImpulse += dLambda
		j.applyWheelAxisImpulse(dLambda)
	}

	// 2. Rotational drive motor.
	if j.EnableMotor && j.motorMass > 0 && j.MaxMotorTorque > 0 && j.dt > 0 {
		wRel := float64(j.B.AngularVel) - float64(j.A.AngularVel)
		dLambda := float64((float64(j.MotorSpeed) - wRel) * j.motorMass)
		maxImp := float64(j.MaxMotorTorque) * j.dt
		oldImp := j.motorImpulse
		j.motorImpulse = clamp64(oldImp+dLambda, -maxImp, maxImp)
		applied := j.motorImpulse - oldImp
		if applied != 0 {
			applyAngularImpulse2D(j.A, j.B, applied)
			j.addAngularImpulse(applied)
		}
	}

	// 3. Optional suspension translation limits along suspension axis.
	if j.limitState != 0 && j.axisMass > 0 {
		vAxis := float64((float64(j.B.VX)-float64(j.A.VX))*j.ax) +
			float64((float64(j.B.VY)-float64(j.A.VY))*j.ay) +
			float64(j.a2*float64(j.B.AngularVel)) -
			float64(j.a1*float64(j.A.AngularVel))
		dLambda := float64(-vAxis * j.axisMass)
		oldImp := j.limitImpulse
		newImp := oldImp + dLambda
		if j.limitState < 0 && newImp < 0 {
			newImp = 0
		} else if j.limitState == 1 && newImp > 0 {
			newImp = 0
		}
		j.limitImpulse = newImp
		j.applyWheelAxisImpulse(newImp - oldImp)
	}

	// 4. Perpendicular lock (keeps wheel hub aligned with suspension strut).
	if j.perpMass > 0 {
		vPerp := float64((float64(j.B.VX)-float64(j.A.VX))*j.px) +
			float64((float64(j.B.VY)-float64(j.A.VY))*j.py) +
			float64(j.sBx*float64(j.B.AngularVel)) -
			float64(j.sAx*float64(j.A.AngularVel))
		lam := float64(-vPerp * j.perpMass)
		pLx, pLy := float64(lam*j.px), float64(lam*j.py)
		if !j.A.Static {
			j.A.VX = float32(float64(j.A.VX) - float64(pLx*float64(j.A.InvMass)))
			j.A.VY = float32(float64(j.A.VY) - float64(pLy*float64(j.A.InvMass)))
			j.A.AngularVel = float32(float64(j.A.AngularVel) - float64(float64(lam*j.sAx)*float64(j.A.InvInertia)))
		}
		if !j.B.Static {
			j.B.VX = float32(float64(j.B.VX) + float64(pLx*float64(j.B.InvMass)))
			j.B.VY = float32(float64(j.B.VY) + float64(pLy*float64(j.B.InvMass)))
			j.B.AngularVel = float32(float64(j.B.AngularVel) + float64(float64(lam*j.sBx)*float64(j.B.InvInertia)))
		}
		j.addLinearImpulse(pLx, pLy)
	}
}

func (j *WheelJoint) solvePosition() {
	if !j.active {
		return
	}
	invMA, invIA := float64(j.A.InvMass), float64(j.A.InvInertia)
	invMB, invIB := float64(j.B.InvMass), float64(j.B.InvInertia)

	// 1. Optional suspension travel limits along suspension axis.
	if j.EnableLimit && j.LowerTranslation <= j.UpperTranslation {
		pA := j.A.LocalToWorld(j.AnchorA)
		pB := j.B.LocalToWorld(j.AnchorB)
		rAx := float64(pA.X) - float64(j.A.X)
		rAy := float64(pA.Y) - float64(j.A.Y)
		rBx := float64(pB.X) - float64(j.B.X)
		rBy := float64(pB.Y) - float64(j.B.Y)
		dx := float64(pB.X) - float64(pA.X)
		dy := float64(pB.Y) - float64(pA.Y)
		ax, ay := bodyWorldDir(j.A, j.LocalAxisA, 0, 1)
		tr := float64(dx*ax) + float64(dy*ay)
		lower, upper := float64(j.LowerTranslation), float64(j.UpperTranslation)
		var err float64
		if math.Abs(upper-lower) <= 1e-6 {
			err = tr - lower
		} else if tr < lower {
			err = tr - lower
		} else if tr > upper {
			err = tr - upper
		}
		if math.Abs(err) > 1e-6 {
			a1 := float64((rAx+dx)*ay) - float64((rAy+dy)*ax)
			a2 := float64(rBx*ay) - float64(rBy*ax)
			kAxis := float64(invMA+invMB) + float64(float64(invIA*a1)*a1) + float64(float64(invIB*a2)*a2)
			if kAxis > 1e-12 {
				lam := -err / kAxis
				lx, ly := float64(lam*ax), float64(lam*ay)
				if !j.A.Static {
					j.A.X = float32(float64(j.A.X) - float64(lx*invMA))
					j.A.Y = float32(float64(j.A.Y) - float64(ly*invMA))
					j.A.Angle = float32(float64(j.A.Angle) - float64(float64(lam*a1)*invIA))
				}
				if !j.B.Static {
					j.B.X = float32(float64(j.B.X) + float64(lx*invMB))
					j.B.Y = float32(float64(j.B.Y) + float64(ly*invMB))
					j.B.Angle = float32(float64(j.B.Angle) + float64(float64(lam*a2)*invIB))
				}
			}
		}
	}

	// 2. Perpendicular alignment projection.
	pA := j.A.LocalToWorld(j.AnchorA)
	pB := j.B.LocalToWorld(j.AnchorB)
	rAx := float64(pA.X) - float64(j.A.X)
	rAy := float64(pA.Y) - float64(j.A.Y)
	rBx := float64(pB.X) - float64(j.B.X)
	rBy := float64(pB.Y) - float64(j.B.Y)
	dx := float64(pB.X) - float64(pA.X)
	dy := float64(pB.Y) - float64(pA.Y)
	ax, ay := bodyWorldDir(j.A, j.LocalAxisA, 0, 1)
	px, py := -ay, ax
	cPerp := float64(dx*px) + float64(dy*py)
	if math.Abs(cPerp) <= 1e-6 {
		return
	}
	sAx := float64((rAx+dx)*py) - float64((rAy+dy)*px)
	sBx := float64(rBx*py) - float64(rBy*px)
	kPerp := float64(invMA+invMB) + float64(float64(invIA*sAx)*sAx) + float64(float64(invIB*sBx)*sBx)
	if kPerp <= 1e-12 {
		return
	}
	lam := -cPerp / kPerp
	pLx, pLy := float64(lam*px), float64(lam*py)
	if !j.A.Static {
		j.A.X = float32(float64(j.A.X) - float64(pLx*invMA))
		j.A.Y = float32(float64(j.A.Y) - float64(pLy*invMA))
		j.A.Angle = float32(float64(j.A.Angle) - float64(float64(lam*sAx)*invIA))
	}
	if !j.B.Static {
		j.B.X = float32(float64(j.B.X) + float64(pLx*invMB))
		j.B.Y = float32(float64(j.B.Y) + float64(pLy*invMB))
		j.B.Angle = float32(float64(j.B.Angle) + float64(float64(lam*sBx)*invIB))
	}
}

// MouseJoint (target joint) pulls a local Anchor on Body toward a settable world
// Target point using a damped spring capped at MaxForce.
type MouseJoint struct {
	Body         *RigidBody
	Anchor       core.Point // local anchor point on Body
	Target       core.Point // world-space target point
	MaxForce     float32    // maximum pulling force (must be > 0 to apply force)
	FrequencyHz  float32    // spring natural frequency in Hz (default 5 in NewMouseJoint)
	DampingRatio float32    // damping ratio for FrequencyHz (default 0.7 in NewMouseJoint)
	Stiffness    float32    // optional direct spring stiffness (used when FrequencyHz <= 0)
	Damping      float32    // optional direct damping (used when FrequencyHz <= 0)
	BreakForce   float32    // max reaction force before breaking (<= 0 disables)
	BreakTorque  float32    // max reaction torque before breaking (<= 0 disables)

	jointReaction
	active   bool
	dt       float64
	rX, rY   float64
	m11, m12 float64
	m22      float64
	gamma    float64
	biasX    float64
	biasY    float64
	impulseX float64
	impulseY float64
}

// NewMouseJoint creates a MouseJoint grabbing body at worldTarget with a maximum
// pulling force maxForce, default FrequencyHz = 5, and DampingRatio = 0.7.
func NewMouseJoint(body *RigidBody, worldTarget core.Point, maxForce float32) *MouseJoint {
	j := &MouseJoint{
		Body:         body,
		Target:       worldTarget,
		MaxForce:     maxForce,
		FrequencyHz:  5,
		DampingRatio: 0.7,
	}
	if body != nil {
		j.Anchor = body.WorldToLocal(worldTarget)
	}
	return j
}

// SetTarget updates the world-space target point that MouseJoint pulls Body.Anchor toward.
func (j *MouseJoint) SetTarget(target core.Point) {
	j.Target = target
}

func (j *MouseJoint) jointBodyPair() (*RigidBody, *RigidBody) { return nil, j.Body }
func (j *MouseJoint) checkBreak() (JointBreakEvent, bool) {
	return j.evalBreak(j, nil, j.Body, j.BreakForce, j.BreakTorque)
}

func (j *MouseJoint) preStep(dt float64) {
	j.active = false
	j.dt = dt
	j.gamma = 0
	j.biasX = 0
	j.biasY = 0
	j.impulseX = 0
	j.impulseY = 0
	if j.Body == nil || j.Body.Static || j.MaxForce <= 0 || dt <= 0 {
		return
	}

	invM := float64(j.Body.InvMass)
	invI := float64(j.Body.InvInertia)
	if invM <= 0 {
		return
	}
	mass := float64(j.Body.Mass)
	if mass <= 0 {
		mass = 1.0 / invM
	}

	var kSpr, cSpr float64
	if j.FrequencyHz > 0 {
		kSpr, cSpr = springCoeffsFromFreq(mass, float64(j.FrequencyHz), max(0, float64(j.DampingRatio)))
	} else if j.Stiffness > 0 || j.Damping > 0 {
		kSpr = max(0, float64(j.Stiffness))
		cSpr = max(0, float64(j.Damping))
	} else {
		return
	}

	p := j.Body.LocalToWorld(j.Anchor)
	cx := float64(p.X) - float64(j.Target.X)
	cy := float64(p.Y) - float64(j.Target.Y)
	g, bx, ok := softSpringParams64(kSpr, cSpr, cx, dt)
	if !ok {
		return
	}
	_, by, _ := softSpringParams64(kSpr, cSpr, cy, dt)

	j.rX = float64(p.X) - float64(j.Body.X)
	j.rY = float64(p.Y) - float64(j.Body.Y)

	k11 := invM + float64(float64(invI*j.rY)*j.rY) + g
	k12 := float64(-invI*j.rX) * j.rY
	k22 := invM + float64(float64(invI*j.rX)*j.rX) + g
	det := float64(k11*k22) - float64(k12*k12)
	if math.Abs(det) <= 1e-12 {
		return
	}
	invDet := 1.0 / det
	j.m11 = k22 * invDet
	j.m12 = -k12 * invDet
	j.m22 = k11 * invDet
	j.gamma = g
	j.biasX = bx
	j.biasY = by
	j.active = true
}

func (j *MouseJoint) solveVelocity() {
	if !j.active {
		return
	}
	vX := float64(j.Body.VX) - float64(float64(j.Body.AngularVel)*j.rY)
	vY := float64(j.Body.VY) + float64(float64(j.Body.AngularVel)*j.rX)

	rhsX := -(vX + j.biasX + float64(j.gamma*j.impulseX))
	rhsY := -(vY + j.biasY + float64(j.gamma*j.impulseY))

	dPx := float64(j.m11*rhsX) + float64(j.m12*rhsY)
	dPy := float64(j.m12*rhsX) + float64(j.m22*rhsY)

	oldX, oldY := j.impulseX, j.impulseY
	j.impulseX += dPx
	j.impulseY += dPy

	maxImp := float64(j.MaxForce) * j.dt
	impLen := detmath.Hypot(j.impulseX, j.impulseY)
	if impLen > maxImp && impLen > 0 {
		scale := maxImp / impLen
		j.impulseX *= scale
		j.impulseY *= scale
	}

	appliedX := j.impulseX - oldX
	appliedY := j.impulseY - oldY
	if appliedX == 0 && appliedY == 0 {
		return
	}
	j.Body.applyImpulseRaw(appliedX, appliedY, j.rX, j.rY)
	j.addLinearImpulse(appliedX, appliedY)
}

func (j *MouseJoint) solvePosition() {}
