package physics

import (
	"math"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// DefaultSolverIterations is the default number of sequential impulse iterations per RigidWorld.Step.
const DefaultSolverIterations = 10

// RigidBody is a 2D dynamic or static rigid body simulated by RigidWorld.
//
// (X, Y) is the world-space center of mass and Angle is the rotation in radians
// (increasing clockwise in NSGo's +X right, +Y down screen space).
// Shape holds the body's geometry (Box or Circle) and is synchronized with (X, Y)
// after each simulation step.
type RigidBody struct {
	ID             int
	Tag            string
	Shape          Shape
	X, Y           float32
	VX, VY         float32
	Angle          float32
	AngularVel     float32
	Mass           float32
	InvMass        float32
	Inertia        float32
	InvInertia     float32
	Restitution    float32
	Friction       float32
	LinearDamping  float32
	AngularDamping float32
	// GravityScale multiplies the effective gravity applied to this body during
	// RigidWorld.Step. NewRigidBox and NewRigidCircle initialize it to 1; on a
	// zero-initialized RigidBody literal it defaults to 1 unless SetGravityScale(0)
	// is called or GravityScale is modified after AddBody.
	GravityScale float32
	Static       bool
	Layer        LayerMask
	Mask         LayerMask
	UserData     any

	gravityScaleSet bool
	forceX          float64
	forceY          float64
	torque          float64
}

// NewRigidBox returns a dynamic rectangular RigidBody centered at (x, y) with size (w, h) and mass.
// If mass <= 0, the body is created as Static.
func NewRigidBox(x, y, w, h, mass float32) *RigidBody {
	b := &RigidBody{
		Shape:           BoxShape(Box{X: x - w*0.5, Y: y - h*0.5, W: w, H: h}),
		X:               x,
		Y:               y,
		Friction:        0.4,
		GravityScale:    1,
		gravityScaleSet: true,
	}
	if mass <= 0 {
		b.Static = true
		return b
	}
	b.Mass = mass
	b.InvMass = 1 / mass
	inertia := float64(mass) * (float64(w)*float64(w) + float64(h)*float64(h)) / 12.0
	if inertia > 0 {
		b.Inertia = float32(inertia)
		b.InvInertia = float32(1.0 / inertia)
	}
	return b
}

// NewStaticRigidBox returns a static (infinite-mass) rectangular RigidBody centered at (x, y) with size (w, h).
func NewStaticRigidBox(x, y, w, h float32) *RigidBody {
	return NewRigidBox(x, y, w, h, 0)
}

// NewRigidCircle returns a dynamic circular RigidBody centered at (x, y) with radius r and mass.
// If mass <= 0, the body is created as Static.
func NewRigidCircle(x, y, r, mass float32) *RigidBody {
	b := &RigidBody{
		Shape:           CircleShape(Circle{X: x, Y: y, R: r}),
		X:               x,
		Y:               y,
		Friction:        0.4,
		GravityScale:    1,
		gravityScaleSet: true,
	}
	if mass <= 0 {
		b.Static = true
		return b
	}
	b.Mass = mass
	b.InvMass = 1 / mass
	inertia := 0.5 * float64(mass) * float64(r) * float64(r)
	if inertia > 0 {
		b.Inertia = float32(inertia)
		b.InvInertia = float32(1.0 / inertia)
	}
	return b
}

// NewStaticRigidCircle returns a static (infinite-mass) circular RigidBody centered at (x, y) with radius r.
func NewStaticRigidCircle(x, y, r float32) *RigidBody {
	return NewRigidCircle(x, y, r, 0)
}

// LocalToWorld transforms a local-space point relative to b's center into world coordinates.
func (b *RigidBody) LocalToWorld(local core.Point) core.Point {
	c := math.Cos(float64(b.Angle))
	s := math.Sin(float64(b.Angle))
	lx, ly := float64(local.X), float64(local.Y)
	return core.Point{
		X: core.Points(float64(b.X) + lx*c - ly*s),
		Y: core.Points(float64(b.Y) + lx*s + ly*c),
	}
}

// WorldToLocal transforms a world-space point into b's local coordinate frame.
func (b *RigidBody) WorldToLocal(world core.Point) core.Point {
	c := math.Cos(float64(b.Angle))
	s := math.Sin(float64(b.Angle))
	dx := float64(world.X) - float64(b.X)
	dy := float64(world.Y) - float64(b.Y)
	return core.Point{
		X: core.Points(dx*c + dy*s),
		Y: core.Points(-dx*s + dy*c),
	}
}

// ApplyForce accumulates a world-space force applied to b's center of mass on the next Step.
func (b *RigidBody) ApplyForce(fx, fy float32) {
	if b == nil || b.Static {
		return
	}
	b.forceX += float64(fx)
	b.forceY += float64(fy)
}

// ApplyTorque accumulates a torque applied to b on the next Step.
func (b *RigidBody) ApplyTorque(torque float32) {
	if b == nil || b.Static {
		return
	}
	b.torque += float64(torque)
}

// ApplyImpulse immediately changes b's linear velocity by (ix, iy)*InvMass.
func (b *RigidBody) ApplyImpulse(ix, iy float32) {
	if b == nil || b.Static {
		return
	}
	b.prepareMass()
	b.VX += float32(float64(ix) * float64(b.InvMass))
	b.VY += float32(float64(iy) * float64(b.InvMass))
}

// ApplyImpulseAtPoint immediately applies world-space impulse (ix, iy) at worldPoint,
// updating both linear and angular velocity.
func (b *RigidBody) ApplyImpulseAtPoint(ix, iy float32, worldPoint core.Point) {
	if b == nil || b.Static {
		return
	}
	b.prepareMass()
	rx := float64(worldPoint.X) - float64(b.X)
	ry := float64(worldPoint.Y) - float64(b.Y)
	b.applyImpulseRaw(float64(ix), float64(iy), rx, ry)
}

func (b *RigidBody) applyImpulseRaw(ix, iy, rx, ry float64) {
	if b.Static {
		return
	}
	invM := float64(b.InvMass)
	invI := float64(b.InvInertia)
	b.VX = float32(float64(b.VX) + ix*invM)
	b.VY = float32(float64(b.VY) + iy*invM)
	b.AngularVel = float32(float64(b.AngularVel) + (rx*iy-ry*ix)*invI)
}

func (b *RigidBody) prepareMass() {
	if b.Static {
		b.InvMass = 0
		b.InvInertia = 0
		b.VX = 0
		b.VY = 0
		b.AngularVel = 0
		b.forceX = 0
		b.forceY = 0
		b.torque = 0
		return
	}
	if b.InvMass == 0 && b.Mass > 0 {
		b.InvMass = 1 / b.Mass
	}
	if b.InvInertia == 0 {
		if b.Inertia > 0 {
			b.InvInertia = 1 / b.Inertia
		} else if b.Mass > 0 {
			var inertia float64
			switch b.Shape.Kind {
			case ShapeBox:
				w, h := float64(b.Shape.Box.W), float64(b.Shape.Box.H)
				inertia = float64(b.Mass) * (w*w + h*h) / 12.0
			case ShapeCircle:
				r := float64(b.Shape.Circle.R)
				inertia = 0.5 * float64(b.Mass) * r * r
			}
			if inertia > 0 {
				b.Inertia = float32(inertia)
				b.InvInertia = float32(1.0 / inertia)
			}
		}
	}
}

func (b *RigidBody) syncShape() {
	switch b.Shape.Kind {
	case ShapeBox:
		b.Shape.Box.X = b.X - b.Shape.Box.W*0.5
		b.Shape.Box.Y = b.Y - b.Shape.Box.H*0.5
	case ShapeCircle:
		b.Shape.Circle.X = b.X
		b.Shape.Circle.Y = b.Y
	}
}

func (b *RigidBody) worldAABB() (minX, minY, maxX, maxY float64) {
	cx, cy := float64(b.X), float64(b.Y)
	switch b.Shape.Kind {
	case ShapeCircle:
		r := float64(b.Shape.Circle.R)
		return cx - r, cy - r, cx + r, cy + r
	case ShapeBox:
		hw := float64(b.Shape.Box.W) * 0.5
		hh := float64(b.Shape.Box.H) * 0.5
		c := math.Abs(math.Cos(float64(b.Angle)))
		s := math.Abs(math.Sin(float64(b.Angle)))
		ex := hw*c + hh*s
		ey := hw*s + hh*c
		return cx - ex, cy - ey, cx + ex, cy + ey
	default:
		return cx, cy, cx, cy
	}
}

// CanCollideRigid reports whether two rigid bodies are allowed to collide based on their Layer and Mask fields.
func CanCollideRigid(a, b *RigidBody) bool {
	if a == nil || b == nil {
		return false
	}
	return layersInteract(a.Layer, a.Mask, b.Layer, b.Mask)
}

// RigidWorld simulates 2D rigid bodies and joints with deterministic symplectic Euler integration,
// sequential impulse contact resolution with friction, and positional slop correction.
type RigidWorld struct {
	Bodies           []*RigidBody
	Joints           []Joint
	Triggers         []*Trigger
	GravityX         float32
	GravityY         float32
	SolverIterations int
}

// AddBody appends non-nil rigid bodies to w.Bodies.
func (w *RigidWorld) AddBody(bodies ...*RigidBody) {
	for _, b := range bodies {
		if b != nil {
			if !isFinite32(b.GravityScale) || (b.GravityScale == 0 && !b.gravityScaleSet) {
				b.GravityScale = 1
			}
			b.gravityScaleSet = true
			w.Bodies = append(w.Bodies, b)
		}
	}
}

// RemoveBody deletes the first occurrence of b from w.Bodies, preserving the order of remaining bodies.
func (w *RigidWorld) RemoveBody(b *RigidBody) bool {
	for i, existing := range w.Bodies {
		if existing == b {
			w.Bodies = append(w.Bodies[:i], w.Bodies[i+1:]...)
			return true
		}
	}
	return false
}

// AddJoint appends non-nil joints to w.Joints.
func (w *RigidWorld) AddJoint(joints ...Joint) {
	for _, j := range joints {
		if j != nil {
			w.Joints = append(w.Joints, j)
		}
	}
}

// RemoveJoint deletes the first occurrence of j from w.Joints, preserving the order of remaining joints.
func (w *RigidWorld) RemoveJoint(j Joint) bool {
	for i, existing := range w.Joints {
		if existing == j {
			w.Joints = append(w.Joints[:i], w.Joints[i+1:]...)
			return true
		}
	}
	return false
}

type rigidContactPoint struct {
	px, py      float64
	depth       float64
	rAx, rAy    float64
	rBx, rBy    float64
	massNormal  float64
	massTangent float64
	bounceBias  float64
	pn          float64
	pt          float64
}

type rigidManifold struct {
	a, b     *RigidBody
	nx, ny   float64 // unit normal pointing from A toward B
	friction float64
	count    int
	points   [2]rigidContactPoint
}

// Step advances all bodies and joints in w by dt using symplectic Euler integration,
// deterministic sequential impulse solving, and positional slop correction.
func (w *RigidWorld) Step(dt time.Duration) {
	if w == nil || dt <= 0 {
		return
	}
	h := dt.Seconds()
	if math.IsNaN(h) || math.IsInf(h, 0) || h <= 0 {
		return
	}

	iters := w.SolverIterations
	if iters <= 0 {
		iters = DefaultSolverIterations
	}

	// 1. Prepare body mass/inertia and integrate forces + gravity/areas into velocities.
	gx, gy := float64(w.GravityX), float64(w.GravityY)
	activeAreas := collectActiveTriggers(w.Triggers)
	for _, b := range w.Bodies {
		if b == nil {
			continue
		}
		b.prepareMass()
		if b.Static {
			continue
		}
		invM := float64(b.InvMass)
		invI := float64(b.InvInertia)
		gScale := float64(b.EffectiveGravityScale())

		var vx, vy, av float64
		if len(activeAreas) == 0 {
			vx = float64(b.VX) + (gx*gScale+b.forceX*invM)*h
			vy = float64(b.VY) + (gy*gScale+b.forceY*invM)*h
			av = float64(b.AngularVel) + (b.torque*invI)*h

			if b.LinearDamping > 0 {
				factor := 1.0 / (1.0 + float64(b.LinearDamping)*h)
				vx *= factor
				vy *= factor
			}
			if b.AngularDamping > 0 {
				factor := 1.0 / (1.0 + float64(b.AngularDamping)*h)
				av *= factor
			}
		} else {
			eff := w.evaluateRigidBodyAreas(b, activeAreas)
			effGX := (float64(eff.GravityX) + float64(eff.BuoyancyAccelX)) * gScale
			effGY := (float64(eff.GravityY) + float64(eff.BuoyancyAccelY)) * gScale
			vx = float64(b.VX) + (effGX+b.forceX*invM)*h
			vy = float64(b.VY) + (effGY+b.forceY*invM)*h
			av = float64(b.AngularVel) + (b.torque*invI)*h

			if eff.FluidLinearDrag > 0 {
				fvx, fvy := float64(eff.FluidVelX), float64(eff.FluidVelY)
				factor := 1.0 / (1.0 + float64(eff.FluidLinearDrag)*h)
				vx = fvx + (vx-fvx)*factor
				vy = fvy + (vy-fvy)*factor
			}
			if eff.LinearDamping > 0 {
				factor := 1.0 / (1.0 + float64(eff.LinearDamping)*h)
				vx *= factor
				vy *= factor
			}
			angDamp := float64(eff.AngularDamping) + float64(eff.FluidAngularDrag)
			if angDamp > 0 {
				factor := 1.0 / (1.0 + angDamp*h)
				av *= factor
			}
		}

		b.VX = float32(vx)
		b.VY = float32(vy)
		b.AngularVel = float32(av)
		b.forceX = 0
		b.forceY = 0
		b.torque = 0
	}

	// 2. Detect contact manifolds in deterministic pair order.
	restitutionThresh := max(1.0, 2.0*math.Hypot(gx, gy)*h)
	manifolds := w.detectContacts(restitutionThresh)

	// 3. Pre-step joints (applies spring forces and prepares rigid joint constraint matrices).
	for _, j := range w.Joints {
		if j != nil {
			j.preStep(h)
		}
	}

	// 4. Sequential impulse velocity solver.
	for range iters {
		for mi := range manifolds {
			solveManifoldVelocity(&manifolds[mi])
		}
		for _, j := range w.Joints {
			if j != nil {
				j.solveVelocity()
			}
		}
	}

	// 5. Integrate positions (symplectic Euler).
	for _, b := range w.Bodies {
		if b == nil || b.Static {
			continue
		}
		b.X = float32(float64(b.X) + float64(b.VX)*h)
		b.Y = float32(float64(b.Y) + float64(b.VY)*h)
		b.Angle = float32(float64(b.Angle) + float64(b.AngularVel)*h)
	}

	// 6. Positional slop correction (projection without injecting artificial bounce velocity).
	const (
		posIterations = 4
		posSlop       = 0.01
		posPercent    = 0.5
	)
	for range posIterations {
		for i := 0; i < len(w.Bodies); i++ {
			a := w.Bodies[i]
			if a == nil || a.Shape.Empty() {
				continue
			}
			for j := i + 1; j < len(w.Bodies); j++ {
				b := w.Bodies[j]
				if b == nil || b.Shape.Empty() {
					continue
				}
				if a.Static && b.Static {
					continue
				}
				if !CanCollideRigid(a, b) {
					continue
				}
				m, hit := collideRigidBodies(a, b)
				if !hit {
					continue
				}
				maxDepth := 0.0
				for k := range m.count {
					if m.points[k].depth > maxDepth {
						maxDepth = m.points[k].depth
					}
				}
				if maxDepth <= posSlop {
					continue
				}
				invMassSum := float64(a.InvMass) + float64(b.InvMass)
				if invMassSum <= 0 {
					continue
				}
				corr := (maxDepth - posSlop) * posPercent / invMassSum
				cx := m.nx * corr
				cy := m.ny * corr
				if !a.Static {
					a.X = float32(float64(a.X) - cx*float64(a.InvMass))
					a.Y = float32(float64(a.Y) - cy*float64(a.InvMass))
				}
				if !b.Static {
					b.X = float32(float64(b.X) + cx*float64(b.InvMass))
					b.Y = float32(float64(b.Y) + cy*float64(b.InvMass))
				}
			}
		}
		for _, j := range w.Joints {
			if j != nil {
				j.solvePosition()
			}
		}
	}

	// 7. Synchronize Shape geometry to updated centers.
	for _, b := range w.Bodies {
		if b != nil {
			b.syncShape()
		}
	}
}

func (w *RigidWorld) detectContacts(restitutionThresh float64) []rigidManifold {
	var manifolds []rigidManifold
	n := len(w.Bodies)
	for i := 0; i < n; i++ {
		a := w.Bodies[i]
		if a == nil || a.Shape.Empty() {
			continue
		}
		aMinX, aMinY, aMaxX, aMaxY := a.worldAABB()
		for j := i + 1; j < n; j++ {
			b := w.Bodies[j]
			if b == nil || b.Shape.Empty() {
				continue
			}
			if a.Static && b.Static {
				continue
			}
			if !CanCollideRigid(a, b) {
				continue
			}
			bMinX, bMinY, bMaxX, bMaxY := b.worldAABB()
			if aMaxX <= bMinX || bMaxX <= aMinX || aMaxY <= bMinY || bMaxY <= aMinY {
				continue
			}
			m, hit := collideRigidBodies(a, b)
			if !hit {
				continue
			}
			prepareManifold(&m, restitutionThresh)
			manifolds = append(manifolds, m)
		}
	}
	return manifolds
}

func prepareManifold(m *rigidManifold, restitutionThresh float64) {
	a, b := m.a, m.b
	m.friction = math.Sqrt(max(0, float64(a.Friction)) * max(0, float64(b.Friction)))
	e := max(0, max(float64(a.Restitution), float64(b.Restitution)))

	nx, ny := m.nx, m.ny
	tx, ty := -ny, nx
	invMA, invIA := float64(a.InvMass), float64(a.InvInertia)
	invMB, invIB := float64(b.InvMass), float64(b.InvInertia)

	for i := range m.count {
		cp := &m.points[i]
		cp.rAx = cp.px - float64(a.X)
		cp.rAy = cp.py - float64(a.Y)
		cp.rBx = cp.px - float64(b.X)
		cp.rBy = cp.py - float64(b.Y)

		rnA := cp.rAx*ny - cp.rAy*nx
		rnB := cp.rBx*ny - cp.rBy*nx
		kNormal := invMA + invMB + invIA*rnA*rnA + invIB*rnB*rnB
		if kNormal > 0 {
			cp.massNormal = 1.0 / kNormal
		}

		rtA := cp.rAx*ty - cp.rAy*tx
		rtB := cp.rBx*ty - cp.rBy*tx
		kTangent := invMA + invMB + invIA*rtA*rtA + invIB*rtB*rtB
		if kTangent > 0 {
			cp.massTangent = 1.0 / kTangent
		}

		// Relative velocity of B with respect to A at contact point.
		vAx := float64(a.VX) - float64(a.AngularVel)*cp.rAy
		vAy := float64(a.VY) + float64(a.AngularVel)*cp.rAx
		vBx := float64(b.VX) - float64(b.AngularVel)*cp.rBy
		vBy := float64(b.VY) + float64(b.AngularVel)*cp.rBx
		vn := (vBx-vAx)*nx + (vBy-vAy)*ny

		if e > 0 && -vn > restitutionThresh {
			cp.bounceBias = e * vn // negative, so -(vn + bounceBias) boosts separation impulse
		} else {
			cp.bounceBias = 0
		}
	}
}

func solveManifoldVelocity(m *rigidManifold) {
	a, b := m.a, m.b
	nx, ny := m.nx, m.ny
	tx, ty := -ny, nx

	for i := range m.count {
		cp := &m.points[i]

		// Normal impulse.
		vAx := float64(a.VX) - float64(a.AngularVel)*cp.rAy
		vAy := float64(a.VY) + float64(a.AngularVel)*cp.rAx
		vBx := float64(b.VX) - float64(b.AngularVel)*cp.rBy
		vBy := float64(b.VY) + float64(b.AngularVel)*cp.rBx
		vn := (vBx-vAx)*nx + (vBy-vAy)*ny

		dPn := cp.massNormal * (-(vn + cp.bounceBias))
		oldPn := cp.pn
		cp.pn = max(oldPn+dPn, 0)
		dPn = cp.pn - oldPn

		px, py := dPn*nx, dPn*ny
		a.applyImpulseRaw(-px, -py, cp.rAx, cp.rAy)
		b.applyImpulseRaw(px, py, cp.rBx, cp.rBy)

		// Friction impulse.
		if m.friction > 0 {
			vAx = float64(a.VX) - float64(a.AngularVel)*cp.rAy
			vAy = float64(a.VY) + float64(a.AngularVel)*cp.rAx
			vBx = float64(b.VX) - float64(b.AngularVel)*cp.rBy
			vBy = float64(b.VY) + float64(b.AngularVel)*cp.rBx
			vt := (vBx-vAx)*tx + (vBy-vAy)*ty

			dPt := cp.massTangent * (-vt)
			maxPt := m.friction * cp.pn
			oldPt := cp.pt
			cp.pt = min(max(oldPt+dPt, -maxPt), maxPt)
			dPt = cp.pt - oldPt

			ftx, fty := dPt*tx, dPt*ty
			a.applyImpulseRaw(-ftx, -fty, cp.rAx, cp.rAy)
			b.applyImpulseRaw(ftx, fty, cp.rBx, cp.rBy)
		}
	}
}

func collideRigidBodies(a, b *RigidBody) (rigidManifold, bool) {
	switch a.Shape.Kind {
	case ShapeCircle:
		switch b.Shape.Kind {
		case ShapeCircle:
			return collideRigidCircleCircle(a, b)
		case ShapeBox:
			return collideRigidCircleOBB(a, b)
		}
	case ShapeBox:
		switch b.Shape.Kind {
		case ShapeCircle:
			m, ok := collideRigidCircleOBB(b, a)
			if !ok {
				return rigidManifold{}, false
			}
			m.a, m.b = a, b
			m.nx, m.ny = -m.nx, -m.ny
			return m, true
		case ShapeBox:
			return collideRigidOBBOBB(a, b)
		}
	}
	return rigidManifold{}, false
}

func collideRigidCircleCircle(a, b *RigidBody) (rigidManifold, bool) {
	dx := float64(b.X) - float64(a.X)
	dy := float64(b.Y) - float64(a.Y)
	rA, rB := float64(a.Shape.Circle.R), float64(b.Shape.Circle.R)
	sumR := rA + rB
	distSq := dx*dx + dy*dy
	if distSq >= sumR*sumR {
		return rigidManifold{}, false
	}
	m := rigidManifold{a: a, b: b, count: 1}
	if distSq == 0 {
		m.nx, m.ny = 0, 1
		m.points[0] = rigidContactPoint{
			px:    float64(a.X),
			py:    float64(a.Y),
			depth: sumR,
		}
		return m, true
	}
	dist := math.Sqrt(distSq)
	nx, ny := dx/dist, dy/dist
	depth := sumR - dist
	mid := rA - depth*0.5
	m.nx, m.ny = nx, ny
	m.points[0] = rigidContactPoint{
		px:    float64(a.X) + nx*mid,
		py:    float64(a.Y) + ny*mid,
		depth: depth,
	}
	return m, true
}

// collideRigidCircleOBB tests circle a against oriented box b. Normal points from a toward b.
func collideRigidCircleOBB(a, b *RigidBody) (rigidManifold, bool) {
	r := float64(a.Shape.Circle.R)
	hw := float64(b.Shape.Box.W) * 0.5
	hh := float64(b.Shape.Box.H) * 0.5
	if r <= 0 || hw <= 0 || hh <= 0 {
		return rigidManifold{}, false
	}

	cosB := math.Cos(float64(b.Angle))
	sinB := math.Sin(float64(b.Angle))
	relX := float64(a.X) - float64(b.X)
	relY := float64(a.Y) - float64(b.Y)
	// Circle center in b's local frame.
	lx := relX*cosB + relY*sinB
	ly := -relX*sinB + relY*cosB

	cx := min(max(lx, -hw), hw)
	cy := min(max(ly, -hh), hh)
	dx := lx - cx
	dy := ly - cy
	distSq := dx*dx + dy*dy

	var lnx, lny, depth float64
	if distSq > 0 {
		if distSq >= r*r {
			return rigidManifold{}, false
		}
		dist := math.Sqrt(distSq)
		// Normal from circle A toward box B is opposite (circle - closest).
		lnx = -dx / dist
		lny = -dy / dist
		depth = r - dist
	} else {
		// Circle center lies inside box b: exit through nearest face.
		dTop := ly + hh
		dBottom := hh - ly
		dLeft := lx + hw
		dRight := hw - lx
		best := dTop
		lnx, lny = 0, 1 // outward face is (0, -1), so circle->box is (0, +1)
		cx, cy = lx, -hh
		if dBottom < best {
			best = dBottom
			lnx, lny = 0, -1
			cx, cy = lx, hh
		}
		if dLeft < best {
			best = dLeft
			lnx, lny = 1, 0
			cx, cy = -hw, ly
		}
		if dRight < best {
			best = dRight
			lnx, lny = -1, 0
			cx, cy = hw, ly
		}
		depth = best + r
	}

	// Transform normal and contact point back to world space.
	wnx := lnx*cosB - lny*sinB
	wny := lnx*sinB + lny*cosB
	wpx := float64(b.X) + cx*cosB - cy*sinB
	wpy := float64(b.Y) + cx*sinB + cy*cosB

	m := rigidManifold{a: a, b: b, nx: wnx, ny: wny, count: 1}
	m.points[0] = rigidContactPoint{
		px:    wpx,
		py:    wpy,
		depth: depth,
	}
	return m, true
}

type vec2 struct{ x, y float64 }

func dot2(a, b vec2) float64 { return a.x*b.x + a.y*b.y }

func clipSegmentToLine(in [2]vec2, count int, normal vec2, offset float64) ([2]vec2, int) {
	var out [2]vec2
	outCount := 0
	if count < 2 {
		return out, 0
	}
	d0 := dot2(normal, in[0]) - offset
	d1 := dot2(normal, in[1]) - offset
	if d0 <= 0 {
		out[outCount] = in[0]
		outCount++
	}
	if d1 <= 0 {
		out[outCount] = in[1]
		outCount++
	}
	if d0*d1 < 0 && outCount < 2 {
		t := d0 / (d0 - d1)
		out[outCount] = vec2{
			x: in[0].x + t*(in[1].x-in[0].x),
			y: in[0].y + t*(in[1].y-in[0].y),
		}
		outCount++
	}
	return out, outCount
}

// collideRigidOBBOBB computes a 1- or 2-point contact manifold between oriented boxes a and b
// using 2D SAT and Sutherland-Hodgman face clipping. Normal points from a toward b.
func collideRigidOBBOBB(a, b *RigidBody) (rigidManifold, bool) {
	hAx, hAy := float64(a.Shape.Box.W)*0.5, float64(a.Shape.Box.H)*0.5
	hBx, hBy := float64(b.Shape.Box.W)*0.5, float64(b.Shape.Box.H)*0.5
	if hAx <= 0 || hAy <= 0 || hBx <= 0 || hBy <= 0 {
		return rigidManifold{}, false
	}

	posA := vec2{float64(a.X), float64(a.Y)}
	posB := vec2{float64(b.X), float64(b.Y)}
	cosA, sinA := math.Cos(float64(a.Angle)), math.Sin(float64(a.Angle))
	cosB, sinB := math.Cos(float64(b.Angle)), math.Sin(float64(b.Angle))
	uA, vA := vec2{cosA, sinA}, vec2{-sinA, cosA}
	uB, vB := vec2{cosB, sinB}, vec2{-sinB, cosB}

	d := vec2{posB.x - posA.x, posB.y - posA.y}
	dAx, dAy := dot2(d, uA), dot2(d, vA)
	dBx, dBy := dot2(d, uB), dot2(d, vB)

	c00 := math.Abs(dot2(uA, uB))
	c01 := math.Abs(dot2(uA, vB))
	c10 := math.Abs(dot2(vA, uB))
	c11 := math.Abs(dot2(vA, vB))

	faceAx := math.Abs(dAx) - hAx - (c00*hBx + c01*hBy)
	if faceAx >= 0 {
		return rigidManifold{}, false
	}
	faceAy := math.Abs(dAy) - hAy - (c10*hBx + c11*hBy)
	if faceAy >= 0 {
		return rigidManifold{}, false
	}
	faceBx := math.Abs(dBx) - hBx - (c00*hAx + c10*hAy)
	if faceBx >= 0 {
		return rigidManifold{}, false
	}
	faceBy := math.Abs(dBy) - hBy - (c01*hAx + c11*hAy)
	if faceBy >= 0 {
		return rigidManifold{}, false
	}

	// Pick the axis with minimum penetration (largest separation < 0).
	const tieEps = 1e-5
	axis := 0 // 0: Ax, 1: Ay, 2: Bx, 3: By
	bestSep := faceAx
	if faceAy > bestSep+tieEps {
		bestSep = faceAy
		axis = 1
	}
	if faceBx > bestSep+tieEps {
		bestSep = faceBx
		axis = 2
	}
	if faceBy > bestSep+tieEps {
		bestSep = faceBy
		axis = 3
	}

	var (
		refPos        vec2
		refNormal     vec2 // outward normal of reference face
		sideAxis      vec2 // tangent along reference face
		refExtentN    float64
		refExtentSide float64
		incPos        vec2
		incU, incV    vec2
		incHX, incHY  float64
		contactNormal vec2 // always points from A toward B
	)

	switch axis {
	case 0: // A_x
		refPos = posA
		if dAx >= 0 {
			refNormal = uA
		} else {
			refNormal = vec2{-uA.x, -uA.y}
		}
		sideAxis = vA
		refExtentN, refExtentSide = hAx, hAy
		incPos, incU, incV, incHX, incHY = posB, uB, vB, hBx, hBy
		contactNormal = refNormal
	case 1: // A_y
		refPos = posA
		if dAy >= 0 {
			refNormal = vA
		} else {
			refNormal = vec2{-vA.x, -vA.y}
		}
		sideAxis = uA
		refExtentN, refExtentSide = hAy, hAx
		incPos, incU, incV, incHX, incHY = posB, uB, vB, hBx, hBy
		contactNormal = refNormal
	case 2: // B_x
		refPos = posB
		if dBx <= 0 {
			refNormal = uB
		} else {
			refNormal = vec2{-uB.x, -uB.y}
		}
		sideAxis = vB
		refExtentN, refExtentSide = hBx, hBy
		incPos, incU, incV, incHX, incHY = posA, uA, vA, hAx, hAy
		contactNormal = vec2{-refNormal.x, -refNormal.y}
	case 3: // B_y
		refPos = posB
		if dBy <= 0 {
			refNormal = vB
		} else {
			refNormal = vec2{-vB.x, -vB.y}
		}
		sideAxis = uB
		refExtentN, refExtentSide = hBy, hBx
		incPos, incU, incV, incHX, incHY = posA, uA, vA, hAx, hAy
		contactNormal = vec2{-refNormal.x, -refNormal.y}
	}

	// Find the incident face most anti-parallel to refNormal.
	incDotU := dot2(incU, refNormal)
	incDotV := dot2(incV, refNormal)
	var incFace [2]vec2
	if math.Abs(incDotU) > math.Abs(incDotV) {
		sign := 1.0
		if incDotU > 0 {
			sign = -1.0
		}
		fc := vec2{incPos.x + incU.x*sign*incHX, incPos.y + incU.y*sign*incHX}
		incFace[0] = vec2{fc.x - incV.x*incHY, fc.y - incV.y*incHY}
		incFace[1] = vec2{fc.x + incV.x*incHY, fc.y + incV.y*incHY}
	} else {
		sign := 1.0
		if incDotV > 0 {
			sign = -1.0
		}
		fc := vec2{incPos.x + incV.x*sign*incHY, incPos.y + incV.y*sign*incHY}
		incFace[0] = vec2{fc.x - incU.x*incHX, fc.y - incU.y*incHX}
		incFace[1] = vec2{fc.x + incU.x*incHX, fc.y + incU.y*incHX}
	}

	// Clip incident face against the two side planes of the reference face.
	sideOffset := dot2(refPos, sideAxis)
	clipped, nClip := clipSegmentToLine(incFace, 2, sideAxis, sideOffset+refExtentSide)
	if nClip < 2 {
		return rigidManifold{}, false
	}
	negSide := vec2{-sideAxis.x, -sideAxis.y}
	clipped, nClip = clipSegmentToLine(clipped, nClip, negSide, -sideOffset+refExtentSide)
	if nClip < 1 {
		return rigidManifold{}, false
	}

	// Keep clipped vertices behind the reference face.
	frontOffset := dot2(refPos, refNormal) + refExtentN
	m := rigidManifold{
		a:  a,
		b:  b,
		nx: contactNormal.x,
		ny: contactNormal.y,
	}
	for i := range nClip {
		sep := dot2(clipped[i], refNormal) - frontOffset
		if sep <= 1e-6 {
			depth := max(0, -sep)
			m.points[m.count] = rigidContactPoint{
				px:    clipped[i].x,
				py:    clipped[i].y,
				depth: depth,
			}
			m.count++
		}
	}
	if m.count == 0 {
		return rigidManifold{}, false
	}
	return m, true
}
