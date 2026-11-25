package physics

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/internal/detmath"
	"github.com/nsgo-dev/nsgo/core"
)

// Joint constrains the relative motion of two RigidBodies inside RigidWorld.Step.
type Joint interface {
	preStep(dt float64)
	solveVelocity()
	solvePosition()
}

// DistanceJoint constrains two local anchor points on bodies A and B to stay at a target Length.
// With default zero Compliance and Stiffness, it acts as a rigid rod; positive Compliance or
// Stiffness in (0, 1) softens the constraint.
type DistanceJoint struct {
	A, B       *RigidBody
	AnchorA    core.Point
	AnchorB    core.Point
	Length     float32
	Stiffness  float32
	Compliance float32
	Damping    float32

	initialized bool
	active      bool
	dt          float64
	ux, uy      float64
	rAx, rAy    float64
	rBx, rBy    float64
	effMass     float64
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

func (j *DistanceJoint) effectiveStiffness() float64 {
	if j.Stiffness <= 0 {
		return 1.0
	}
	if j.Stiffness > 1 {
		return 1.0
	}
	return float64(j.Stiffness)
}

func (j *DistanceJoint) preStep(dt float64) {
	j.active = false
	if j.A == nil || j.B == nil || (j.A.Static && j.B.Static) {
		return
	}
	pA := j.A.LocalToWorld(j.AnchorA)
	pB := j.B.LocalToWorld(j.AnchorB)
	dx := float64(pB.X) - float64(pA.X)
	dy := float64(pB.Y) - float64(pA.Y)
	dist := detmath.Hypot(dx, dy)

	if !j.initialized && j.Length <= 0 {
		j.Length = float32(dist)
		j.initialized = true
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

	alpha := max(0, float64(j.Compliance)) / (dt * dt)
	denom := k + alpha
	if denom <= 1e-12 {
		return
	}
	j.effMass = 1.0 / denom
	j.dt = dt
	j.active = true
}

func (j *DistanceJoint) solveVelocity() {
	if !j.active {
		return
	}
	vAx := float64(j.A.VX) - float64(float64(j.A.AngularVel)*j.rAy)
	vAy := float64(j.A.VY) + float64(float64(j.A.AngularVel)*j.rAx)
	vBx := float64(j.B.VX) - float64(float64(j.B.AngularVel)*j.rBy)
	vBy := float64(j.B.VY) + float64(float64(j.B.AngularVel)*j.rBx)

	vRel := float64((vBx-vAx)*j.ux) + float64((vBy-vAy)*j.uy)
	lambda := -vRel * j.effMass * j.effectiveStiffness()
	if j.Damping > 0 {
		// Additional viscous damping along the rod axis.
		damp := min(1.0, float64(j.Damping)*j.dt)
		lambda *= 1.0 - float64(0.5*damp)
	}
	px, py := lambda*j.ux, lambda*j.uy
	j.A.applyImpulseRaw(-px, -py, j.rAx, j.rAy)
	j.B.applyImpulseRaw(px, py, j.rBx, j.rBy)
}

func (j *DistanceJoint) solvePosition() {
	if !j.active {
		return
	}
	pA := j.A.LocalToWorld(j.AnchorA)
	pB := j.B.LocalToWorld(j.AnchorB)
	dx := float64(pB.X) - float64(pA.X)
	dy := float64(pB.Y) - float64(pA.Y)
	dist := detmath.Hypot(dx, dy)
	err := dist - float64(j.Length)
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

	alpha := max(0, float64(j.Compliance)) / (j.dt * j.dt)
	denom := k + alpha
	if denom <= 1e-12 {
		return
	}
	lambda := -err * j.effectiveStiffness() / denom
	px, py := lambda*ux, lambda*uy

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
	A, B       *RigidBody
	AnchorA    core.Point
	AnchorB    core.Point
	RestLength float32
	Stiffness  float32
	Damping    float32

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

	px, py := impulse*ux, impulse*uy
	j.A.applyImpulseRaw(-px, -py, rAx, rAy)
	j.B.applyImpulseRaw(px, py, rBx, rBy)
}

func (j *SpringJoint) solveVelocity() {}
func (j *SpringJoint) solvePosition() {}

// RevoluteJoint pins a local anchor on A to a local anchor on B so both world anchor points
// remain coincident while allowing free relative rotation around the hinge.
type RevoluteJoint struct {
	A, B    *RigidBody
	AnchorA core.Point
	AnchorB core.Point

	active   bool
	rAx, rAy float64
	rBx, rBy float64
	m11, m12 float64
	m22      float64
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
	return j
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

func (j *RevoluteJoint) preStep(_ float64) {
	j.active = false
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
	j.active = true
}

func (j *RevoluteJoint) solveVelocity() {
	if !j.active {
		return
	}
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
}

func (j *RevoluteJoint) solvePosition() {
	if !j.active {
		return
	}
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
