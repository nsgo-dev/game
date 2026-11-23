package physics3d

import (
	"cmp"
	"slices"
	"time"
)

const (
	defaultSolverIterations = 8
	restitutionVelThreshold = 1.0
	positionSlop            = 0.0002
	positionPercent         = 0.8
	sleepVelThreshold       = 0.05
	sleepAngVelThreshold    = 0.05
	sleepTimeThreshold      = 0.4
	wakeVelThreshold        = 0.2
)

// Body is a rigid body, kinematic body, static collider, or trigger volume in a World.
type Body struct {
	// ID uniquely identifies the body inside its World. AddBody assigns one
	// sequentially when ID is 0.
	ID int
	// Tag is an optional user label for identifying bodies in query results.
	Tag string
	// Shape is the body's collision primitive in local coordinates relative to Pos.
	Shape Shape
	// Pos is the body's world-space position.
	Pos Vec3
	// Vel is the body's linear velocity in units per second.
	Vel Vec3
	// Rot is the body's world-space orientation quaternion. A zero quaternion is
	// normalized to identity by AddBody and treated as identity by helpers.
	Rot Quat
	// AngVel is the body's angular velocity in radians per second.
	AngVel Vec3
	// Mass is the body's mass in kilograms. Zero mass marks a static body.
	Mass float32
	// InvMass is 1/Mass for dynamic bodies and 0 for static bodies.
	InvMass float32
	// InvInertia is the body's inverse rotational inertia. When <= 0 on a
	// dynamic body, AddBody computes an isotropic approximation from Shape and
	// Mass unless LockRotation is set.
	InvInertia float32
	// LockRotation keeps a dynamic body from rotating: InvInertia becomes 0
	// and angular impulses are ignored, which InvInertia == 0 alone could not
	// express while 0 also meant "compute a default" (#546).
	LockRotation bool
	// Restitution is the bounciness in [0, 1].
	Restitution float32
	// Friction is the Coulomb friction coefficient (>= 0).
	Friction float32
	// LinearDamping reduces velocity smoothly over time (1 / (1 + LinearDamping*dt)).
	LinearDamping float32
	// AngularDamping reduces angular velocity smoothly over time (1 / (1 + AngularDamping*dt)).
	AngularDamping float32
	// Static marks an immovable body with infinite mass (InvMass == 0).
	Static bool
	// Kinematic marks a body driven directly by user/animation code (infinite
	// mass to the solver and unaffected by gravity, while retaining Mass/InvMass
	// so it can transition cleanly to dynamic simulation).
	Kinematic bool
	// Sleeping marks a dynamic body currently at rest; the solver skips
	// integrating it until woken by Wake, ApplyImpulse, or an active collision.
	Sleeping bool
	// Layer is the collision layer bitmask this body belongs to (0 defaults to LayerAll).
	Layer LayerMask
	// Mask is the collision layer bitmask this body collides with (0 defaults to LayerAll).
	Mask LayerMask
	// IsTrigger marks a sensor volume that emits trigger ContactEvents without
	// applying impulse or positional resolution.
	IsTrigger bool

	sleepTimer float32
	// lastPos and lastRot are the pose at the end of the previous Step, so a kinematic body moved by setting Pos or Rot directly still wakes what it touches.
	lastPos  Vec3
	lastRot  Quat
	havePose bool
}

// NewStaticBody returns a static Body placed at pos with default friction.
func NewStaticBody(shape Shape, pos Vec3) Body {
	return Body{
		Shape:    shape,
		Pos:      pos,
		Rot:      QuatIdentity(),
		Static:   true,
		Friction: 0.5,
	}
}

// NewDynamicBody returns a dynamic Body placed at pos with the given mass and
// default friction (0.5), linear damping (0.01), and angular damping (0.05).
// If mass <= 0, the returned body is static.
func NewDynamicBody(shape Shape, pos Vec3, mass float32) Body {
	if mass <= 0 {
		return NewStaticBody(shape, pos)
	}
	return Body{
		Shape:          shape,
		Pos:            pos,
		Rot:            QuatIdentity(),
		Mass:           mass,
		InvMass:        1 / mass,
		Friction:       0.5,
		LinearDamping:  0.01,
		AngularDamping: 0.05,
	}
}

// WorldShape returns the body's collision shape transformed by Pos and Rot.
func (b *Body) WorldShape() Shape {
	return b.Shape.Transformed(b.Pos, b.Rot)
}

// Wake clears b's sleeping state so it participates in subsequent steps.
func (b *Body) Wake() {
	b.Sleeping = false
	b.sleepTimer = 0
}

// ApplyImpulse adds impulse / Mass to a dynamic body's velocity and wakes it.
func (b *Body) ApplyImpulse(impulse Vec3) {
	if b.Static || b.Kinematic || b.InvMass == 0 {
		return
	}
	b.Wake()
	b.Vel = b.Vel.Add(impulse.Scale(b.InvMass))
}

// ApplyAngularImpulse adds impulse * InvInertia to a dynamic body's angular
// velocity and wakes it.
func (b *Body) ApplyAngularImpulse(impulse Vec3) {
	if b.Static || b.Kinematic || b.InvInertia == 0 {
		return
	}
	b.Wake()
	b.AngVel = b.AngVel.Add(impulse.Scale(b.InvInertia))
}

func computeDefaultInvInertia(shape Shape, mass float32) float32 {
	if mass <= 0 {
		return 0
	}
	var inertia float32
	switch shape.Kind {
	case ShapeSphere:
		r := shape.Sphere.Radius
		inertia = 0.4 * mass * r * r
	case ShapeBox:
		e := shape.Box.HalfExtents
		// An isotropic box approximation: each axis' moment is
		// (1/3) m (e_y² + e_z²), averaging to (1/3) m Σe². The old 0.4
		// coefficient was 1.8× too high for a cube (#546).
		inertia = mass * (e.X*e.X + e.Y*e.Y + e.Z*e.Z) / 3
	case ShapeCapsule:
		r := shape.Capsule.Radius
		lSq := shape.Capsule.B.Sub(shape.Capsule.A).LenSq()
		inertia = mass * (0.4*r*r + lSq/12.0)
	}
	minI := 1e-4 * mass
	if inertia < minI {
		inertia = minI
	}
	return 1 / inertia
}

// OverlapHit reports a body or static collider overlapped by World.QueryOverlap
// or World.OverlapLayer. Contact.Normal points from the collider toward the query shape.
type OverlapHit struct {
	Body        *Body
	TriMesh     *TriMeshCollider
	Heightfield *HeightfieldCollider
	Contact     Contact
}

type pairKey struct {
	a, b int
}

type cachedImpulse struct {
	normal         Vec3
	normalImpulse  float32
	tangentImpulse Vec3
}

type solverContact struct {
	a, b   *Body
	normal Vec3
	depth  float32
	point  Vec3
	// rA and rB are the contact point relative to each body's center, so impulses also apply torque (#546).
	rA, rB         Vec3
	restitution    float32
	friction       float32
	bias           float32
	normalImpulse  float32
	tangentImpulse Vec3
	isStaticTri    bool
	isHeightfield  bool
	tri            Triangle
	staticID       int
}

// World holds static, kinematic, and dynamic bodies, static triangle meshes and
// heightfields, and 3D joints, stepping them forward deterministically.
type World struct {
	// Gravity is the constant acceleration applied to dynamic bodies each step.
	// NewWorld initializes it to (0, -9.81, 0).
	Gravity Vec3
	// SolverIterations is the number of velocity and positional solver passes
	// per Step; values <= 0 default to 8.
	SolverIterations int
	// OnContact is invoked deterministically for non-trigger ContactEvents
	// (ContactEnter, ContactStay, ContactExit).
	OnContact func(ContactEvent)
	// OnTrigger is invoked deterministically for trigger ContactEvents
	// (ContactEnter, ContactStay, ContactExit).
	OnTrigger func(ContactEvent)

	bodies         []*Body
	nextID         int
	nextColliderID int
	cache          map[pairKey]cachedImpulse
	activeContacts map[pairKey]activeContactRecord
	contactEvents  []ContactEvent
	triMeshes      []*TriMeshCollider
	heightfields   []*HeightfieldCollider
	joints         []Joint3D
	ignoredPairs   map[pairKey]struct{}
}

// NewWorld returns a World with standard Y-up gravity (0, -9.81, 0) and 8
// solver iterations.
func NewWorld() *World {
	return &World{
		Gravity:          Vec3{Y: -9.81},
		SolverIterations: defaultSolverIterations,
		cache:            make(map[pairKey]cachedImpulse),
		activeContacts:   make(map[pairKey]activeContactRecord),
	}
}

// AddBody copies b into w, normalizes its ID, reassigning an explicit ID that is already taken,, orientation, mass, and inertia
// fields, and returns a pointer to the stored body.
func (w *World) AddBody(b Body) *Body {
	// An explicit ID that is already taken would alias two bodies in the pair
	// cache, so it is reassigned a fresh one instead; callers that need a
	// stable handle keep the returned pointer.
	if b.ID <= 0 || w.bodyByID(b.ID) != nil {
		w.nextID++
		b.ID = w.nextID
	} else if b.ID >= w.nextID {
		w.nextID = b.ID
	}

	b.Rot = normalizedQuat(b.Rot)
	if !isFiniteQuat(b.Rot) {
		b.Rot = QuatIdentity()
	}
	// Non-finite inputs would poison every body the solver touches, so they fall back to defaults (#546).
	if !isFiniteVec(b.Vel) {
		b.Vel = Vec3{}
	}
	if !isFiniteVec(b.AngVel) {
		b.AngVel = Vec3{}
	}
	if !isFinite32(b.Mass) {
		b.Mass = 0
	}
	if !isFinite32(b.InvMass) {
		b.InvMass = 0
	}
	if !isFinite32(b.InvInertia) {
		b.InvInertia = 0
	}

	if b.Kinematic && !b.Static && b.Mass <= 0 && b.InvMass <= 0 {
		// A kinematic body without mass stays kinematic (it used to become static and stop pushing things); it has infinite mass to the solver either way.
		b.Mass, b.InvMass, b.InvInertia = 0, 0, 0
	} else if b.Static || (b.Mass <= 0 && b.InvMass <= 0) {
		b.Static = true
		b.Mass = 0
		b.InvMass = 0
		b.InvInertia = 0
		b.Vel = Vec3{}
		b.AngVel = Vec3{}
		b.Sleeping = false
	} else {
		if b.InvMass <= 0 {
			b.InvMass = 1 / b.Mass
		} else if b.Mass <= 0 {
			b.Mass = 1 / b.InvMass
		}
		if b.LockRotation {
			b.InvInertia = 0
			b.AngVel = Vec3{}
		} else if b.InvInertia <= 0 {
			b.InvInertia = computeDefaultInvInertia(b.Shape, b.Mass)
		}
	}

	if b.Friction < 0 {
		b.Friction = 0
	}
	if b.Restitution < 0 {
		b.Restitution = 0
	}
	if b.Restitution > 1 {
		b.Restitution = 1
	}
	if b.AngularDamping < 0 {
		b.AngularDamping = 0
	}

	stored := new(Body)
	*stored = b
	w.bodies = append(w.bodies, stored)
	return stored
}

// bodyByID returns the body stored with id, or nil when none is.
func (w *World) bodyByID(id int) *Body {
	for _, b := range w.bodies {
		if b.ID == id {
			return b
		}
	}
	return nil
}

// RemoveBody removes the body with the given ID, preserving deterministic order
// of the remaining bodies, emitting ContactExit events for any active overlaps,
// removing joints attached to the body, and waking dynamic bodies so supported
// stacks react.
func (w *World) RemoveBody(id int) bool {
	idx := -1
	for i, b := range w.bodies {
		if b.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false
	}
	w.bodies = append(w.bodies[:idx], w.bodies[idx+1:]...)
	w.removeJointsForBody(id)
	// Drop warm-start impulses involving id, or a later body that reuses the ID would start from this one's contacts (#546).
	for k := range w.cache {
		if k.a == id || k.b == id {
			delete(w.cache, k)
		}
	}
	if len(w.ignoredPairs) > 0 {
		for k := range w.ignoredPairs {
			if k.a == id || k.b == id {
				delete(w.ignoredPairs, k)
			}
		}
	}
	w.emitRemovedBodyExits(id)
	w.wakeAllDynamic()
	return true
}

// Bodies returns the bodies currently in w in deterministic insertion order.
func (w *World) Bodies() []*Body {
	return w.bodies
}

// Raycast casts a segment from from to to against all bodies and static
// colliders in w and returns all hits sorted by ascending T in [0, 1].
func (w *World) Raycast(from, to Vec3) []RaycastHit {
	return w.RaycastLayer(from, to, LayerAll)
}

// RaycastFirst casts a segment from from to to against all bodies and static
// colliders in w and returns the closest hit, if any.
func (w *World) RaycastFirst(from, to Vec3) (RaycastHit, bool) {
	return w.RaycastFirstLayer(from, to, LayerAll)
}

// QueryOverlap returns every body and static collider in w overlapping shape,
// in deterministic insertion order. Each hit's Contact.Normal points from the
// collider toward shape.
func (w *World) QueryOverlap(shape Shape) []OverlapHit {
	return w.OverlapLayer(shape, LayerAll)
}

// Step advances the rigid-body simulation by dt using symplectic Euler
// integration, deterministic collision detection, warm-started iterative
// impulse resolution with Coulomb friction, 3D joint solving, positional
// correction, and deterministic contact/trigger event dispatch.
func (w *World) Step(dt time.Duration) {
	if dt <= 0 {
		return
	}
	if len(w.bodies) == 0 {
		if len(w.activeContacts) > 0 {
			w.reconcileContactEvents(make(map[pairKey]activeContactRecord))
		} else {
			w.contactEvents = w.contactEvents[:0]
		}
		return
	}
	dtSec := float32(dt.Seconds())
	iters := w.SolverIterations
	if iters <= 0 {
		iters = defaultSolverIterations
	}
	if w.cache == nil {
		w.cache = make(map[pairKey]cachedImpulse)
	}

	// Wake sleeping bodies that a moving kinematic body touches or that are jointed to a moving body, before anything treats them as immovable.
	w.wakeForMotion()

	// Prepare joints first so active motors can wake sleeping bodies before integration.
	for _, j := range w.joints {
		j.Prepare(dtSec)
	}

	n := len(w.bodies)
	prevPos := make([]Vec3, n)

	// 1. Symplectic Euler integration: update velocities, then tentative poses.
	for i, b := range w.bodies {
		prevPos[i] = b.Pos
		if b.Static || b.Kinematic || b.Sleeping {
			continue
		}
		b.Vel = b.Vel.Add(w.Gravity.Scale(dtSec))
		if b.LinearDamping > 0 {
			b.Vel = b.Vel.Scale(1 / (1 + b.LinearDamping*dtSec))
		}
		b.Pos = b.Pos.Add(b.Vel.Scale(dtSec))

		if b.AngVel.LenSq() > 0 {
			if b.AngularDamping > 0 {
				b.AngVel = b.AngVel.Scale(1 / (1 + b.AngularDamping*dtSec))
			}
			b.Rot = IntegrateQuat(b.Rot, b.AngVel, dtSec)
		}
	}

	jointIgnored := w.buildJointIgnoredPairs()

	// 1b. Sweep fast bodies so they cannot tunnel through thin static geometry.
	for i, b := range w.bodies {
		if b.Static || b.Kinematic || b.Sleeping || b.IsTrigger {
			continue
		}
		w.sweepFastBody(b, prevPos[i], jointIgnored)
	}

	// 2. Deterministic broadphase + narrowphase pair detection.
	shapes := make([]Shape, n)
	bounds := make([]Box, n)
	for i, b := range w.bodies {
		shapes[i] = b.WorldShape()
		bounds[i] = shapes[i].Bounds()
	}

	currentPairs := make(map[pairKey]activeContactRecord)

	var contacts []solverContact
	for i := 0; i < n; i++ {
		a := w.bodies[i]
		for j := i + 1; j < n; j++ {
			b := w.bodies[j]
			isTrig := a.IsTrigger || b.IsTrigger
			if a.Static && b.Static && !isTrig {
				continue
			}
			if !CanCollide(a, b) {
				continue
			}
			if w.isPairIgnored(a.ID, b.ID) {
				continue
			}
			key, swapped := canonicalPairKey(a.ID, b.ID)
			if _, ok := jointIgnored[key]; ok {
				continue
			}
			if !aabbOverlap(bounds[i], bounds[j]) {
				continue
			}
			c := TestShapes(shapes[i], shapes[j])
			if !c.Hit {
				continue
			}

			evA, evB, evNorm := a, b, c.Normal
			if swapped {
				evA, evB, evNorm = b, a, c.Normal.Neg()
			}
			currentPairs[key] = activeContactRecord{
				a:         evA,
				b:         evB,
				point:     c.Point,
				normal:    evNorm,
				depth:     c.Depth,
				isTrigger: isTrig,
			}

			if isTrig {
				continue
			}

			// Wake a sleeping dynamic body struck by a moving dynamic or kinematic body. This runs before the inactive-pair skip, which would otherwise let a kinematic body pass through sleeping ones.
			if a.Sleeping && isMoving(b) {
				a.Wake()
			}
			if b.Sleeping && isMoving(a) {
				b.Wake()
			}
			if (a.Static || a.Kinematic || a.Sleeping) && (b.Static || b.Kinematic || b.Sleeping) {
				continue
			}

			// Compute restitution bias once from the pre-solver closing velocity at the contact point.
			rA, rB := c.Point.Sub(a.Pos), c.Point.Sub(b.Pos)
			rv := pointVel(a, rA).Sub(pointVel(b, rB))
			vn := rv.Dot(c.Normal)
			restitution := max(a.Restitution, b.Restitution)
			var bias float32
			if vn < -restitutionVelThreshold {
				bias = -restitution * vn
			}
			friction := sqrt32(a.Friction * b.Friction)

			sc := solverContact{
				a:           a,
				b:           b,
				normal:      c.Normal,
				depth:       c.Depth,
				point:       c.Point,
				rA:          rA,
				rB:          rB,
				restitution: restitution,
				friction:    friction,
				bias:        bias,
			}

			// Warm-start from the previous step's accumulated impulse when the
			// contact normal remains aligned.
			cacheKey := pairKey{a: a.ID, b: b.ID}
			if cached, ok := w.cache[cacheKey]; ok && cached.normal.Dot(c.Normal) > 0.95 {
				sc.normalImpulse = cached.normalImpulse
				sc.tangentImpulse = cached.tangentImpulse.Sub(c.Normal.Scale(cached.tangentImpulse.Dot(c.Normal)))
			}
			contacts = append(contacts, sc)
		}
	}

	// 2b. Narrowphase against static triangle-mesh and heightfield colliders.
	if len(w.triMeshes) > 0 || len(w.heightfields) > 0 {
		for i, a := range w.bodies {
			if a.Static || a.Kinematic || a.Sleeping || a.IsTrigger {
				continue
			}
			for _, tm := range w.triMeshes {
				if !canCollideMasks(a.Layer, a.Mask, tm.Layer, tm.Mask) {
					continue
				}
				if !aabbOverlap(bounds[i], tm.Bounds) {
					continue
				}
				tm.QueryTriangles(bounds[i], func(tri Triangle) {
					if !aabbOverlap(bounds[i], tri.Bounds()) {
						return
					}
					c := TestShapeTriangle(shapes[i], tri)
					if !c.Hit {
						return
					}
					rA := c.Point.Sub(a.Pos)
					vn := pointVel(a, rA).Dot(c.Normal)
					restitution := max(a.Restitution, tm.Restitution)
					var bias float32
					if vn < -restitutionVelThreshold {
						bias = -restitution * vn
					}
					contacts = append(contacts, solverContact{
						a:           a,
						normal:      c.Normal,
						depth:       c.Depth,
						point:       c.Point,
						rA:          rA,
						restitution: restitution,
						friction:    sqrt32(a.Friction * tm.Friction),
						bias:        bias,
						isStaticTri: true,
						tri:         tri,
						staticID:    tm.ID,
					})
				})
			}
			for _, hf := range w.heightfields {
				if !canCollideMasks(a.Layer, a.Mask, hf.Layer, hf.Mask) {
					continue
				}
				if !aabbOverlap(bounds[i], hf.Bounds) {
					continue
				}
				hf.QueryTriangles(bounds[i], func(tri Triangle) {
					if !aabbOverlap(bounds[i], tri.Bounds()) {
						return
					}
					c := testShapeHeightfieldTriangle(shapes[i], tri)
					if !c.Hit {
						return
					}
					rA := c.Point.Sub(a.Pos)
					vn := pointVel(a, rA).Dot(c.Normal)
					restitution := max(a.Restitution, hf.Restitution)
					var bias float32
					if vn < -restitutionVelThreshold {
						bias = -restitution * vn
					}
					contacts = append(contacts, solverContact{
						a:             a,
						normal:        c.Normal,
						depth:         c.Depth,
						point:         c.Point,
						rA:            rA,
						restitution:   restitution,
						friction:      sqrt32(a.Friction * hf.Friction),
						bias:          bias,
						isStaticTri:   true,
						isHeightfield: true,
						tri:           tri,
						staticID:      hf.ID,
					})
				})
			}
		}
	}

	// Sort contacts bottom-up (ascending Y, then body IDs) for fast, deterministic
	// convergence on vertical stacks.
	slices.SortStableFunc(contacts, func(x, y solverContact) int {
		if c := cmp.Compare(x.point.Y, y.point.Y); c != 0 {
			return c
		}
		if c := cmp.Compare(x.a.ID, y.a.ID); c != 0 {
			return c
		}
		xbID, ybID := 0, 0
		if x.b != nil {
			xbID = x.b.ID
		} else {
			xbID = -x.staticID
		}
		if y.b != nil {
			ybID = y.b.ID
		} else {
			ybID = -y.staticID
		}
		return cmp.Compare(xbID, ybID)
	})

	// 3. Apply warm-start impulses.
	for i := range contacts {
		c := &contacts[i]
		if c.isStaticTri {
			continue
		}
		total := c.normal.Scale(c.normalImpulse).Add(c.tangentImpulse)
		applyContactImpulse(c.a, c.rA, total)
		applyContactImpulse(c.b, c.rB, total.Neg())
	}

	// Refresh joint states against post-integration poses.
	for _, j := range w.joints {
		j.Prepare(dtSec)
	}

	// 4. Iterative sequential-impulse velocity solver (contacts + 3D joints).
	for iter := 0; iter < iters; iter++ {
		for i := range contacts {
			c := &contacts[i]
			invA, invIA := activeInvMass(c.a), activeInvInertia(c.a)
			var invB, invIB float32
			if c.b != nil {
				invB, invIB = activeInvMass(c.b), activeInvInertia(c.b)
			}
			if invA+invB+invIA+invIB == 0 {
				continue
			}

			// Normal impulse with accumulated clamping >= 0, using the effective mass at the contact point.
			kn := contactEffectiveMass(invA, invIA, c.rA, invB, invIB, c.rB, c.normal)
			if kn <= 0 {
				continue
			}
			rv := pointVel(c.a, c.rA).Sub(pointVel(c.b, c.rB))
			vn := rv.Dot(c.normal)
			dj := -(vn - c.bias) / kn
			newJ := c.normalImpulse + dj
			if newJ < 0 {
				newJ = 0
			}
			appliedJ := newJ - c.normalImpulse
			c.normalImpulse = newJ

			impN := c.normal.Scale(appliedJ)
			applyContactImpulse(c.a, c.rA, impN)
			applyContactImpulse(c.b, c.rB, impN.Neg())

			// Isotropic 3D Coulomb friction impulse clamped to friction * normalImpulse. Applied at the contact point, it makes spheres roll and boxes tip.
			if c.friction > 0 && c.normalImpulse > 0 {
				rv = pointVel(c.a, c.rA).Sub(pointVel(c.b, c.rB))
				vt := rv.Sub(c.normal.Scale(rv.Dot(c.normal)))
				vtLen := vt.Len()
				if vtLen > 1e-9 {
					tdir := vt.Scale(1 / vtLen)
					kt := contactEffectiveMass(invA, invIA, c.rA, invB, invIB, c.rB, tdir)
					if kt > 0 {
						newT := c.tangentImpulse.Add(tdir.Scale(-vtLen / kt))
						newT = newT.Sub(c.normal.Scale(newT.Dot(c.normal)))
						maxF := c.friction * c.normalImpulse
						if tLen := newT.Len(); tLen > maxF && tLen > 0 {
							newT = newT.Scale(maxF / tLen)
						}
						appliedT := newT.Sub(c.tangentImpulse)
						c.tangentImpulse = newT
						applyContactImpulse(c.a, c.rA, appliedT)
						applyContactImpulse(c.b, c.rB, appliedT.Neg())
					}
				}
			}
		}

		for _, j := range w.joints {
			j.SolveVelocity(dtSec)
		}
	}

	// Refresh warm-start cache with active body-body contacts only.
	clear(w.cache)
	for i := range contacts {
		c := &contacts[i]
		if c.isStaticTri || c.b == nil {
			continue
		}
		w.cache[pairKey{a: c.a.ID, b: c.b.ID}] = cachedImpulse{
			normal:         c.normal,
			normalImpulse:  c.normalImpulse,
			tangentImpulse: c.tangentImpulse,
		}
	}

	// 5. Non-linear Gauss-Seidel positional/angular correction (zero velocity injection).
	for iter := 0; iter < iters; iter++ {
		for i := range contacts {
			c := &contacts[i]
			invA := activeInvMass(c.a)
			invB := float32(0)
			if c.b != nil {
				invB = activeInvMass(c.b)
			}
			invSum := invA + invB
			if invSum == 0 {
				continue
			}
			var cur Contact
			if c.isStaticTri {
				if c.isHeightfield {
					cur = testShapeHeightfieldTriangle(c.a.WorldShape(), c.tri)
				} else {
					cur = TestShapeTriangle(c.a.WorldShape(), c.tri)
				}
			} else {
				cur = TestShapes(c.a.WorldShape(), c.b.WorldShape())
			}
			if !cur.Hit || cur.Depth <= positionSlop {
				continue
			}
			corr := (cur.Depth - positionSlop) * positionPercent / invSum
			if invA > 0 {
				c.a.Pos = c.a.Pos.Add(cur.Normal.Scale(corr * invA))
			}
			if invB > 0 {
				c.b.Pos = c.b.Pos.Sub(cur.Normal.Scale(corr * invB))
			}
		}

		for _, j := range w.joints {
			j.SolvePosition(dtSec)
		}
	}

	// 6. Put settled bodies to sleep so resting stacks have zero drift.
	const (
		sleepVelSq    = sleepVelThreshold * sleepVelThreshold
		sleepAngVelSq = sleepAngVelThreshold * sleepAngVelThreshold
	)
	invDtSq := 1 / (dtSec * dtSec)
	for i, b := range w.bodies {
		if b.Static || b.Kinematic || b.Sleeping {
			continue
		}
		dispSq := b.Pos.Sub(prevPos[i]).LenSq() * invDtSq
		if b.Vel.LenSq() < sleepVelSq && dispSq < sleepVelSq && b.AngVel.LenSq() < sleepAngVelSq {
			b.sleepTimer += dtSec
			if b.sleepTimer >= sleepTimeThreshold {
				b.Sleeping = true
				b.Vel = Vec3{}
				b.AngVel = Vec3{}
			}
		} else {
			b.sleepTimer = 0
		}
	}

	// 7. A sleeping body whose solid contact ended this step lost a support (or a neighbor moved away), so it wakes and falls instead of hanging in the air.
	for k, rec := range w.activeContacts {
		if _, still := currentPairs[k]; still || rec.isTrigger {
			continue
		}
		if rec.a != nil && rec.a.Sleeping {
			rec.a.Wake()
		}
		if rec.b != nil && rec.b.Sleeping {
			rec.b.Wake()
		}
	}
	for _, b := range w.bodies {
		b.lastPos, b.lastRot, b.havePose = b.Pos, b.Rot, true
	}

	// 8. Reconcile active contacts and emit deterministic Enter/Stay/Exit events.
	w.reconcileContactEvents(currentPairs)
}

// isMoving reports whether b is moving fast enough to wake a sleeping body it touches: an awake dynamic body above the wake threshold, or a kinematic body with velocity or one moved directly since the last step.
func isMoving(b *Body) bool {
	const wakeVelSq = wakeVelThreshold * wakeVelThreshold
	switch {
	case b == nil || b.Static || b.Sleeping:
		return false
	case b.Vel.LenSq() > wakeVelSq || b.AngVel.LenSq() > wakeVelSq:
		return true
	case b.Kinematic:
		return b.havePose && (b.Pos != b.lastPos || b.Rot != b.lastRot)
	default:
		return false
	}
}

// wakeForMotion wakes sleeping bodies jointed to a moving body; contacts with moving bodies wake in the pair loop of Step.
func (w *World) wakeForMotion() {
	for _, j := range w.joints {
		a, b := j.Bodies()
		if a != nil && a.Sleeping && isMoving(b) {
			a.Wake()
		}
		if b != nil && b.Sleeping && isMoving(a) {
			b.Wake()
		}
	}
}

func isFiniteVec(v Vec3) bool {
	return isFinite32(v.X) && isFinite32(v.Y) && isFinite32(v.Z)
}

func isFiniteQuat(q Quat) bool {
	return isFinite32(q.X) && isFinite32(q.Y) && isFinite32(q.Z) && isFinite32(q.W)
}

func activeInvMass(b *Body) float32 {
	if b.Static || b.Kinematic || b.Sleeping {
		return 0
	}
	return b.InvMass
}

func activeInvInertia(b *Body) float32 {
	if b.Static || b.Kinematic || b.Sleeping {
		return 0
	}
	return b.InvInertia
}

// pointVel returns the velocity of the point at offset r from b's center, including rotation. A nil body (static geometry) does not move.
func pointVel(b *Body, r Vec3) Vec3 {
	if b == nil {
		return Vec3{}
	}
	return b.Vel.Add(b.AngVel.Cross(r))
}

// applyContactImpulse applies impulse p at offset r from b's center to its linear and angular velocity.
func applyContactImpulse(b *Body, r, p Vec3) {
	if b == nil {
		return
	}
	if invM := activeInvMass(b); invM > 0 {
		b.Vel = b.Vel.Add(p.Scale(invM))
	}
	if invI := activeInvInertia(b); invI > 0 {
		b.AngVel = b.AngVel.Add(r.Cross(p).Scale(invI))
	}
}

// contactEffectiveMass returns the inverse effective mass of a contact along unit direction d.
func contactEffectiveMass(invA, invIA float32, rA Vec3, invB, invIB float32, rB Vec3, d Vec3) float32 {
	return invA + invB + invIA*rA.Cross(d).LenSq() + invIB*rB.Cross(d).LenSq()
}
