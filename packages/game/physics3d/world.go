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
	// sleepMaxDepth is the deepest residual contact a body may keep and still fall asleep; a sleeping body left deeper than sleepWakeDepth by a neighbor wakes so the solver can push it out (#546).
	sleepMaxDepth  = 0.005
	sleepWakeDepth = 2 * sleepMaxDepth
	// shockMinCos is the smallest cosine between a contact normal and the up direction (-Gravity) for which the shock-propagation passes treat the lower body as immovable, about 45 degrees.
	shockMinCos = 0.7
	// shockKeepDepth is the overlap the shock pass leaves in a vertical contact. Resolving it fully would let the next step's integration separate a resting pair by a hair, dropping the contact and its warm-start impulse and making the stack bob; 1 mm keeps the contact alive across steps.
	shockKeepDepth = 0.001
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
	// CenterOfMass is the body's center of mass in local coordinates relative to Pos.
	// For dynamic compound bodies, AddBody computes it from all solid children
	// unless already set.
	CenterOfMass Vec3
	// InvInertiaTensor is the optional 3x3 symmetric inverse inertia tensor in
	// local space about CenterOfMass. For dynamic compound bodies, AddBody
	// computes it via the parallel-axis theorem unless LockRotation is set or an
	// explicit InvInertiaTensor or InvInertia was provided. When zero, the solver
	// uses scalar InvInertia.
	InvInertiaTensor SymMat3
	// LockRotation keeps a dynamic body from rotating: InvInertia becomes 0
	// and angular impulses are ignored, which InvInertia == 0 alone could not
	// express while 0 also meant "compute a default" (#546).
	LockRotation bool
	// Restitution is the bounciness in [0, 1]. Overridden when Material is non-nil.
	Restitution float32
	// Friction is the Coulomb friction coefficient (>= 0). Overridden when Material is non-nil.
	Friction float32
	// LinearDamping reduces velocity smoothly over time (1 / (1 + LinearDamping*dt)). Overridden when Material is non-nil.
	LinearDamping float32
	// AngularDamping reduces angular velocity smoothly over time (1 / (1 + AngularDamping*dt)). Overridden when Material is non-nil.
	AngularDamping float32
	// Material optionally overrides Friction, Restitution, LinearDamping, and
	// AngularDamping with a shared PhysicsMaterial.
	Material *PhysicsMaterial
	// GravityScale multiplies the effective gravity applied to this body during
	// World.Step. NewDynamicBody initializes it to 1; on a zero-initialized Body
	// literal it defaults to 1 unless SetGravityScale(0) is called or GravityScale
	// is modified after AddBody.
	GravityScale float32
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
	// Area configures optional gravity, damping, and buoyancy/fluid drag overrides
	// applied to overlapping dynamic bodies when IsTrigger is true.
	Area AreaSettings

	gravityScaleSet bool
	sleepTimer      float32
	// residualDepth is the deepest contact b was left in by the last Step's final position pass.
	residualDepth float32
	// lastPos and lastRot are the pose at the end of the previous Step, so a kinematic body moved by setting Pos or Rot directly still wakes what it touches.
	lastPos        Vec3
	lastRot        Quat
	havePose       bool
	restingOnVoxel bool
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
// For a ShapeCompound with mass <= 0, if solid children specify Mass or Density,
// their combined mass is used. Otherwise mass <= 0 returns a static body.
func NewDynamicBody(shape Shape, pos Vec3, mass float32) Body {
	if mass <= 0 && shape.Kind == ShapeCompound {
		if props := ComputeCompoundMassProperties(shape.Children, 0); props.Mass > 0 {
			mass = props.Mass
		}
	}
	if mass <= 0 {
		return NewStaticBody(shape, pos)
	}
	return Body{
		Shape:           shape,
		Pos:             pos,
		Rot:             QuatIdentity(),
		Mass:            mass,
		InvMass:         1 / mass,
		Friction:        0.5,
		LinearDamping:   0.01,
		AngularDamping:  0.05,
		GravityScale:    1,
		gravityScaleSet: true,
	}
}

// WorldShape returns the body's collision shape transformed by Pos and Rot.
func (b *Body) WorldShape() Shape {
	return b.Shape.Transformed(b.Pos, b.Rot)
}

// WorldCenterOfMass returns b's center of mass in world coordinates.
// For bodies with zero local CenterOfMass, this equals b.Pos.
func (b *Body) WorldCenterOfMass() Vec3 {
	if b == nil {
		return Vec3{}
	}
	if b.CenterOfMass == (Vec3{}) {
		return b.Pos
	}
	return b.Pos.Add(normalizedQuat(b.Rot).RotateVec(b.CenterOfMass))
}

// RecomputeMassProperties recomputes Mass, InvMass, CenterOfMass, InvInertia,
// and InvInertiaTensor from b.Shape for a dynamic body. For compound bodies,
// solid children determine the combined center of mass and parallel-axis inertia
// tensor; trigger children are ignored.
func (b *Body) RecomputeMassProperties() {
	if b == nil || b.Static || b.Kinematic {
		return
	}
	if b.Shape.Kind == ShapeCompound {
		props := ComputeCompoundMassProperties(b.Shape.Children, b.Mass)
		if props.Mass <= 0 {
			return
		}
		b.Mass = props.Mass
		b.InvMass = 1 / props.Mass
		b.CenterOfMass = props.CenterOfMass
		if b.LockRotation {
			b.InvInertia = 0
			b.InvInertiaTensor = SymMat3{}
			b.AngVel = Vec3{}
		} else {
			b.InvInertia = props.ScalarInvI
			b.InvInertiaTensor = props.InvInertia
		}
		return
	}
	b.CenterOfMass = Vec3{}
	b.InvInertiaTensor = SymMat3{}
	if b.Mass > 0 {
		b.InvMass = 1 / b.Mass
		if b.LockRotation {
			b.InvInertia = 0
			b.AngVel = Vec3{}
		} else {
			b.InvInertia = computeDefaultInvInertia(b.Shape, b.Mass)
		}
	}
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

// ApplyAngularImpulse adds the angular velocity delta produced by impulse to a
// dynamic body (using InvInertiaTensor when set, otherwise scalar InvInertia)
// and wakes it.
func (b *Body) ApplyAngularImpulse(impulse Vec3) {
	if b.Static || b.Kinematic || b.LockRotation || (b.InvInertia == 0 && b.InvInertiaTensor.IsZero()) {
		return
	}
	b.Wake()
	b.applyAngularImpulseWorld(impulse)
}

// ApplyImpulseAtPoint applies linear impulse at worldPoint, imparting both
// linear velocity and torque about WorldCenterOfMass, and wakes b.
func (b *Body) ApplyImpulseAtPoint(impulse, worldPoint Vec3) {
	if b.Static || b.Kinematic || b.InvMass == 0 {
		return
	}
	b.Wake()
	b.Vel = b.Vel.Add(impulse.Scale(b.InvMass))
	r := worldPoint.Sub(b.WorldCenterOfMass())
	b.applyAngularImpulseWorld(r.Cross(impulse))
}

func computeDefaultInvInertia(shape Shape, mass float32) float32 {
	if mass <= 0 {
		return 0
	}
	var inertia float32
	// center is the shape's centroid in body space; a shape offset from the body origin rotates about the origin, so the parallel-axis term m|center|² is added (#546).
	var center Vec3
	switch shape.Kind {
	case ShapeSphere:
		r := shape.Sphere.Radius
		inertia = float32(0.4 * mass * r * r)
		center = shape.Sphere.Center
	case ShapeBox:
		e := shape.Box.HalfExtents
		// An isotropic box approximation: each axis' moment is (1/3) m (e_y² + e_z²) for half extents e, and the mean of the three is (2/9) m Σe², which is (2/3) m e² for a cube (#546).
		sumSq := float32(float32(e.X*e.X)+float32(e.Y*e.Y)) + float32(e.Z*e.Z)
		inertia = float32(float32(mass*sumSq)*2) / 9
		center = shape.Box.Center
	case ShapeCapsule:
		r := shape.Capsule.Radius
		lSq := shape.Capsule.B.Sub(shape.Capsule.A).LenSq()
		inertia = float32(mass * (float32(float32(0.4*r)*r) + float32(lSq/12.0)))
		center = shape.Capsule.A.Add(shape.Capsule.B).Scale(0.5)
	case ShapeConvexHull:
		// The mean of the hull's principal moments, from its exact inertia tensor for uniform density (#808).
		if g := shape.Hull.geom; g != nil {
			inertia = mass * float32((g.inertia[0][0]+g.inertia[1][1]+g.inertia[2][2])/3)
			c := toCvx(shape)
			center = c.centroid().vec3()
		}
	case ShapeCylinder:
		// The mean of a solid cylinder's moments: m(3r²+h²)/12 about the two axes across it and mr²/2 about its own, which averages to m(6r²+h²)/18 (#808).
		r := shape.Cylinder.Radius
		h := shape.Cylinder.Height()
		inertia = mass * (float32(6*r*r) + float32(h*h)) / 18
		center = shape.Cylinder.Center()
	case ShapeCompound:
		if props := ComputeCompoundMassProperties(shape.Children, mass); props.ScalarInvI > 0 {
			return props.ScalarInvI
		}
	}
	inertia = float32(inertia) + float32(mass*center.LenSq())
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
	ChildIndex  int
	TriMesh     *TriMeshCollider
	Heightfield *HeightfieldCollider
	Voxel       *VoxelCollider
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
	a, b           *Body
	childA, childB int
	normal         Vec3
	depth          float32
	point          Vec3
	// rA and rB are the contact point relative to each body's center of mass, so impulses also apply torque (#546).
	rA, rB         Vec3
	restitution    float32
	friction       float32
	bias           float32
	normalImpulse  float32
	tangentImpulse Vec3
	isStaticTri    bool
	isHeightfield  bool
	isVoxel        bool
	tri            Triangle
	voxel          *VoxelCollider
	voxelX         int
	voxelY         int
	voxelZ         int
	staticID       int
}

// World holds static, kinematic, and dynamic bodies, static triangle meshes,
// heightfields, voxel colliders, and 3D joints, stepping them forward deterministically.
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
	// OnJointBreak is invoked deterministically for each joint broken during
	// World.Step in joint insertion order.
	OnJointBreak func(JointBreakEvent)

	bodies             []*Body
	nextID             int
	nextColliderID     int
	cache              map[contactKey]cachedImpulse
	activeContacts     map[contactKey]activeContactRecord
	nextActiveContacts map[contactKey]activeContactRecord
	contactEvents      []ContactEvent
	jointBreakEvents   []JointBreakEvent
	triMeshes          []*TriMeshCollider
	heightfields       []*HeightfieldCollider
	voxels             []*VoxelCollider
	joints             []Joint3D
	ignoredPairs       map[pairKey]struct{}

	bp              broadphase3D
	contactsBuf     []solverContact
	shockOrderBuf   []int
	shockHeightsBuf []float32
	curKeysBuf      []contactKey
	exitKeysBuf     []contactKey
}

// NewWorld returns a World with standard Y-up gravity (0, -9.81, 0) and 8
// solver iterations.
func NewWorld() *World {
	return &World{
		Gravity:          Vec3{Y: -9.81},
		SolverIterations: defaultSolverIterations,
		cache:            make(map[contactKey]cachedImpulse),
		activeContacts:   make(map[contactKey]activeContactRecord),
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

	b.Shape = sanitizeShape(b.Shape)
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
	if !isFiniteVec(b.CenterOfMass) {
		b.CenterOfMass = Vec3{}
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
	if !isFiniteSymMat3(b.InvInertiaTensor) {
		b.InvInertiaTensor = SymMat3{}
	}

	if b.Shape.Kind == ShapeCompound && !b.Static && !b.Kinematic && b.Mass <= 0 && b.InvMass <= 0 {
		if props := ComputeCompoundMassProperties(b.Shape.Children, 0); props.Mass > 0 {
			b.Mass = props.Mass
			b.InvMass = 1 / props.Mass
		}
	}

	if b.Kinematic && !b.Static && b.Mass <= 0 && b.InvMass <= 0 {
		// A kinematic body without mass stays kinematic (it used to become static and stop pushing things); it has infinite mass to the solver either way.
		b.Mass, b.InvMass, b.InvInertia = 0, 0, 0
		b.InvInertiaTensor = SymMat3{}
	} else if b.Static || (b.Mass <= 0 && b.InvMass <= 0) {
		b.Static = true
		b.Mass = 0
		b.InvMass = 0
		b.InvInertia = 0
		b.InvInertiaTensor = SymMat3{}
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
			b.InvInertiaTensor = SymMat3{}
			b.AngVel = Vec3{}
		} else if b.Shape.Kind == ShapeCompound {
			props := ComputeCompoundMassProperties(b.Shape.Children, b.Mass)
			if props.Mass > 0 {
				if b.CenterOfMass == (Vec3{}) {
					b.CenterOfMass = props.CenterOfMass
				}
				if b.InvInertiaTensor.IsZero() && b.InvInertia <= 0 {
					b.InvInertiaTensor = props.InvInertia
					b.InvInertia = props.ScalarInvI
				}
			} else if b.InvInertia <= 0 {
				b.InvInertia = computeDefaultInvInertia(b.Shape, b.Mass)
			}
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
	if !isFinite32(b.GravityScale) || (b.GravityScale == 0 && !b.gravityScaleSet) {
		b.GravityScale = 1
	}
	b.gravityScaleSet = true

	stored := new(Body)
	*stored = b
	w.bodies = append(w.bodies, stored)
	w.bp.addBody(w, stored)
	if stored.IsTrigger && stored.Area.Active() {
		w.wakeAllDynamic()
	}
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
	w.bp.removeBody(w, idx)
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

// SyncBroadphase refreshes the broadphase acceleration structure for any bodies
// whose Pos, Rot, Shape, Static, or IsTrigger fields were mutated directly
// since the last World.Step. AddBody, RemoveBody, AddTriMesh, RemoveTriMesh,
// AddHeightfield, RemoveHeightfield, Restore, and Step maintain the broadphase
// automatically.
func (w *World) SyncBroadphase() {
	if w == nil {
		return
	}
	w.bp.syncAllBodies(w)
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
	w.jointBreakEvents = w.jointBreakEvents[:0]
	if len(w.bodies) == 0 {
		if len(w.activeContacts) > 0 {
			if w.nextActiveContacts == nil {
				w.nextActiveContacts = make(map[contactKey]activeContactRecord)
			} else {
				clear(w.nextActiveContacts)
			}
			w.reconcileContactEvents(w.nextActiveContacts)
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
		w.cache = make(map[contactKey]cachedImpulse)
	}

	// Reconcile any external pose/shape/static/trigger mutations once per step.
	w.bp.syncAllBodies(w)

	// Wake sleeping bodies that a moving kinematic body touches, that are jointed
	// to a moving body, or whose voxel terrain support changed, before anything
	// treats them as immovable.
	w.wakeForMotion()
	w.wakeForVoxels()

	// Prepare joints first so active motors can wake sleeping bodies before integration.
	for _, j := range w.joints {
		j.Prepare(dtSec)
	}

	dynCount := len(w.bp.dynamicList)
	if cap(w.bp.dynPrevPos) < dynCount {
		w.bp.dynPrevPos = make([]Vec3, dynCount)
	} else {
		w.bp.dynPrevPos = w.bp.dynPrevPos[:dynCount]
	}
	prevPos := w.bp.dynPrevPos
	areaTriggers := w.collectActiveAreaTriggers()

	// 1. Symplectic Euler integration: update velocities, then tentative poses.
	for di, pid := range w.bp.dynamicList {
		b := w.bp.proxies[pid].body
		prevPos[di] = b.Pos
		if b.Static || b.Kinematic || b.Sleeping {
			continue
		}
		gScale := b.EffectiveGravityScale()
		if len(areaTriggers) == 0 {
			b.Vel = b.Vel.Add(w.Gravity.Scale(gScale * dtSec))
			if linDamp := b.effectiveLinearDamping(); linDamp > 0 {
				b.Vel = b.Vel.Scale(1 / (1 + float32(linDamp*dtSec)))
			}
			if b.CenterOfMass == (Vec3{}) {
				b.Pos = b.Pos.Add(b.Vel.Scale(dtSec))
				if b.AngVel.LenSq() > 0 {
					if angDamp := b.effectiveAngularDamping(); angDamp > 0 {
						b.AngVel = b.AngVel.Scale(1 / (1 + float32(angDamp*dtSec)))
					}
					b.Rot = IntegrateQuat(b.Rot, b.AngVel, dtSec)
				}
			} else {
				comWorld := b.WorldCenterOfMass().Add(b.Vel.Scale(dtSec))
				if b.AngVel.LenSq() > 0 {
					if angDamp := b.effectiveAngularDamping(); angDamp > 0 {
						b.AngVel = b.AngVel.Scale(1 / (1 + float32(angDamp*dtSec)))
					}
					b.Rot = IntegrateQuat(b.Rot, b.AngVel, dtSec)
				}
				b.Pos = comWorld.Sub(normalizedQuat(b.Rot).RotateVec(b.CenterOfMass))
			}
			continue
		}

		eff := w.evaluateBodyAreas(b, areaTriggers)
		accel := eff.Gravity.Add(eff.BuoyancyAccel).Scale(gScale)
		b.Vel = b.Vel.Add(accel.Scale(dtSec))

		if eff.FluidLinearDrag > 0 {
			factor := 1 / (1 + float32(eff.FluidLinearDrag*dtSec))
			b.Vel = eff.FluidVelocity.Add(b.Vel.Sub(eff.FluidVelocity).Scale(factor))
		}
		if eff.LinearDamping > 0 {
			b.Vel = b.Vel.Scale(1 / (1 + float32(eff.LinearDamping*dtSec)))
		}
		if b.CenterOfMass == (Vec3{}) {
			b.Pos = b.Pos.Add(b.Vel.Scale(dtSec))
			if b.AngVel.LenSq() > 0 {
				angDamp := eff.AngularDamping + eff.FluidAngularDrag
				if angDamp > 0 {
					b.AngVel = b.AngVel.Scale(1 / (1 + float32(angDamp*dtSec)))
				}
				b.Rot = IntegrateQuat(b.Rot, b.AngVel, dtSec)
			}
		} else {
			comWorld := b.WorldCenterOfMass().Add(b.Vel.Scale(dtSec))
			if b.AngVel.LenSq() > 0 {
				angDamp := eff.AngularDamping + eff.FluidAngularDrag
				if angDamp > 0 {
					b.AngVel = b.AngVel.Scale(1 / (1 + float32(angDamp*dtSec)))
				}
				b.Rot = IntegrateQuat(b.Rot, b.AngVel, dtSec)
			}
			b.Pos = comWorld.Sub(normalizedQuat(b.Rot).RotateVec(b.CenterOfMass))
		}
	}

	jointIgnored := w.buildJointIgnoredPairs()

	// 1b. Sweep fast bodies so they cannot tunnel through thin static geometry.
	for di, pid := range w.bp.dynamicList {
		b := w.bp.proxies[pid].body
		if b.Static || b.Kinematic || b.Sleeping || b.IsTrigger {
			continue
		}
		w.sweepFastBody(b, prevPos[di], jointIgnored)
	}

	// 2. Deterministic broadphase + narrowphase pair detection.
	w.bp.syncDynamicBodies(w, prevPos)

	if w.nextActiveContacts == nil {
		w.nextActiveContacts = make(map[contactKey]activeContactRecord)
	} else {
		clear(w.nextActiveContacts)
	}
	currentPairs := w.nextActiveContacts

	contacts := w.contactsBuf[:0]
	pairs := w.bp.findBodyPairs()
	var singleContact [1]Contact
	recordContact := func(a, b *Body, ci, cj int, shapeA, shapeB Shape, c Contact, isTrig bool) {
		key, swapped := canonicalContactKey(a.ID, ci, b.ID, cj)
		evA, evB, evChildA, evChildB, evNorm := a, b, ci, cj, c.Normal
		if swapped {
			evA, evB, evChildA, evChildB, evNorm = b, a, cj, ci, c.Normal.Neg()
		}
		currentPairs[key] = activeContactRecord{
			a:         evA,
			b:         evB,
			childA:    evChildA,
			childB:    evChildB,
			point:     c.Point,
			normal:    evNorm,
			depth:     c.Depth,
			isTrigger: isTrig,
		}

		if isTrig {
			if a.IsTrigger && a.Area.Active() && b.Sleeping && isMoving(a) {
				b.Wake()
			}
			if b.IsTrigger && b.Area.Active() && a.Sleeping && isMoving(b) {
				a.Wake()
			}
			return
		}

		// Wake a sleeping dynamic body struck by a moving dynamic or kinematic body. This runs before the inactive-pair skip, which would otherwise let a kinematic body pass through sleeping ones.
		if a.Sleeping && isMoving(b) {
			a.Wake()
		}
		if b.Sleeping && isMoving(a) {
			b.Wake()
		}
		if (a.Static || a.Kinematic || a.Sleeping) && (b.Static || b.Kinematic || b.Sleeping) {
			return
		}

		// A pair with a convex hull or a cylinder gets up to four points spread over the touching area, so a face or a cap resting on another body stays flat instead of rocking about the single point TestShapes reports (#808).
		var points []Contact
		if isConvexKind(shapeA) || isConvexKind(shapeB) {
			points = shapeManifold(shapeA, shapeB, c)
		} else {
			singleContact[0] = c
			points = singleContact[:]
		}
		restitution := max(a.effectiveRestitution(), b.effectiveRestitution())
		friction := sqrt32(a.effectiveFriction() * b.effectiveFriction())
		cacheKey := contactKey{a: a.ID, b: b.ID, childA: ci, childB: cj}
		cached, warm := w.cache[cacheKey]
		warm = warm && cached.normal.Dot(c.Normal) > 0.95
		comA, comB := a.WorldCenterOfMass(), b.WorldCenterOfMass()
		for _, pc := range points {
			// Compute restitution bias once from the pre-solver closing velocity at the contact point.
			rA, rB := pc.Point.Sub(comA), pc.Point.Sub(comB)
			rv := pointVel(a, rA).Sub(pointVel(b, rB))
			vn := rv.Dot(pc.Normal)
			var bias float32
			if vn < -restitutionVelThreshold {
				bias = -restitution * vn
			}
			sc := solverContact{
				a:           a,
				b:           b,
				childA:      ci,
				childB:      cj,
				normal:      pc.Normal,
				depth:       pc.Depth,
				point:       pc.Point,
				rA:          rA,
				rB:          rB,
				restitution: restitution,
				friction:    friction,
				bias:        bias,
			}
			// Warm-start from the previous step's accumulated impulse, shared among the pair's points, when the contact normal remains aligned.
			if warm {
				share := 1 / float32(len(points))
				sc.normalImpulse = cached.normalImpulse * share
				sc.tangentImpulse = cached.tangentImpulse.Sub(c.Normal.Scale(cached.tangentImpulse.Dot(c.Normal))).Scale(share)
			}
			contacts = append(contacts, sc)
		}
	}

	for _, pair := range pairs {
		pA := &w.bp.proxies[w.bp.bodyProxies[pair.i]]
		pB := &w.bp.proxies[w.bp.bodyProxies[pair.j]]
		a := pA.body
		b := pB.body
		if w.isPairIgnored(a.ID, b.ID) {
			continue
		}
		pairK, _ := canonicalPairKey(a.ID, b.ID)
		if _, ok := jointIgnored[pairK]; ok {
			continue
		}
		if a.Shape.Kind != ShapeCompound && b.Shape.Kind != ShapeCompound {
			isTrig := a.IsTrigger || b.IsTrigger
			if a.Static && b.Static && !isTrig {
				continue
			}
			if !CanCollide(a, b) {
				continue
			}
			if !aabbOverlap(pA.worldBounds, pB.worldBounds) {
				continue
			}
			c := TestShapes(pA.worldShape, pB.worldShape)
			if !c.Hit {
				continue
			}
			recordContact(a, b, -1, -1, pA.worldShape, pB.worldShape, c, isTrig)
			continue
		}

		if !aabbOverlap(pA.worldBounds, pB.worldBounds) {
			continue
		}
		countA := a.childCount()
		countB := b.childCount()
		if countA == 0 || countB == 0 {
			continue
		}
		for ia := 0; ia < countA; ia++ {
			ci := -1
			shapeA := pA.worldShape
			if a.Shape.Kind == ShapeCompound {
				ci = ia
				shapeA = pA.worldShape.Children[ia].LocalShape()
			}
			layerA, maskA, trigA := a.childEffective(ci)
			boundsA := shapeA.Bounds()
			for ib := 0; ib < countB; ib++ {
				cj := -1
				shapeB := pB.worldShape
				if b.Shape.Kind == ShapeCompound {
					cj = ib
					shapeB = pB.worldShape.Children[ib].LocalShape()
				}
				layerB, maskB, trigB := b.childEffective(cj)
				isTrig := trigA || trigB
				if a.Static && b.Static && !isTrig {
					continue
				}
				if !canCollideMasks(layerA, maskA, layerB, maskB) {
					continue
				}
				if !aabbOverlap(boundsA, shapeB.Bounds()) {
					continue
				}
				var c Contact
				if isTrig {
					c = TestShapes(shapeA, shapeB)
				} else {
					c = testShapesSolid(shapeA, shapeB)
				}
				if !c.Hit {
					continue
				}
				recordContact(a, b, ci, cj, shapeA, shapeB, c, isTrig)
			}
		}
	}

	// 2b. Narrowphase against static triangle-mesh, heightfield, and voxel colliders.
	if len(w.triMeshes) > 0 || len(w.heightfields) > 0 || len(w.voxels) > 0 {
		hasStaticMeshesOrHF := len(w.triMeshes) > 0 || len(w.heightfields) > 0
		for _, pid := range w.bp.dynamicList {
			p := &w.bp.proxies[pid]
			a := p.body
			if a.Static || a.Kinematic || a.Sleeping || a.IsTrigger {
				continue
			}
			a.restingOnVoxel = false
			comA := a.WorldCenterOfMass()
			var tms []*TriMeshCollider
			var hfs []*HeightfieldCollider
			if hasStaticMeshesOrHF {
				tms, hfs = w.bp.queryStaticMeshesAndHeightfields(p.worldBounds)
			}
			testStaticForChild := func(ci int, childShape Shape, childBounds Box, layerA, maskA LayerMask) {
				for _, tm := range tms {
					if !canCollideMasks(layerA, maskA, tm.Layer, tm.Mask) {
						continue
					}
					if !aabbOverlap(childBounds, tm.Bounds) {
						continue
					}
					tm.QueryTriangles(childBounds, func(tri Triangle) {
						if !aabbOverlap(childBounds, tri.Bounds()) {
							return
						}
						for _, c := range shapeTriangleContacts(childShape, tri, false) {
							rA := c.Point.Sub(comA)
							vn := pointVel(a, rA).Dot(c.Normal)
							restitution := max(a.effectiveRestitution(), tm.Restitution)
							var bias float32
							if vn < -restitutionVelThreshold {
								bias = -restitution * vn
							}
							contacts = append(contacts, solverContact{
								a:           a,
								childA:      ci,
								childB:      -1,
								normal:      c.Normal,
								depth:       c.Depth,
								point:       c.Point,
								rA:          rA,
								restitution: restitution,
								friction:    sqrt32(a.effectiveFriction() * tm.Friction),
								bias:        bias,
								isStaticTri: true,
								tri:         tri,
								staticID:    tm.ID,
							})
						}
					})
				}
				for _, hf := range hfs {
					if !canCollideMasks(layerA, maskA, hf.Layer, hf.Mask) {
						continue
					}
					if !aabbOverlap(childBounds, hf.Bounds) {
						continue
					}
					hf.QueryTriangles(childBounds, func(tri Triangle) {
						if !aabbOverlap(childBounds, tri.Bounds()) {
							return
						}
						for _, c := range shapeTriangleContacts(childShape, tri, true) {
							rA := c.Point.Sub(comA)
							vn := pointVel(a, rA).Dot(c.Normal)
							restitution := max(a.effectiveRestitution(), hf.Restitution)
							var bias float32
							if vn < -restitutionVelThreshold {
								bias = -restitution * vn
							}
							contacts = append(contacts, solverContact{
								a:             a,
								childA:        ci,
								childB:        -1,
								normal:        c.Normal,
								depth:         c.Depth,
								point:         c.Point,
								rA:            rA,
								restitution:   restitution,
								friction:      sqrt32(a.effectiveFriction() * hf.Friction),
								bias:          bias,
								isStaticTri:   true,
								isHeightfield: true,
								tri:           tri,
								staticID:      hf.ID,
							})
						}
					})
				}
				for _, vc := range w.voxels {
					if !canCollideMasks(layerA, maskA, vc.Layer, vc.Mask) {
						continue
					}
					vc.forEachContact(childShape, func(vx, vy, vz int, c Contact) {
						a.restingOnVoxel = true
						rA := c.Point.Sub(comA)
						vn := pointVel(a, rA).Dot(c.Normal)
						restitution := max(a.effectiveRestitution(), vc.Restitution)
						var bias float32
						if vn < -restitutionVelThreshold {
							bias = -restitution * vn
						}
						contacts = append(contacts, solverContact{
							a:           a,
							childA:      ci,
							childB:      -1,
							normal:      c.Normal,
							depth:       c.Depth,
							point:       c.Point,
							rA:          rA,
							restitution: restitution,
							friction:    sqrt32(a.effectiveFriction() * vc.Friction),
							bias:        bias,
							isVoxel:     true,
							voxel:       vc,
							voxelX:      vx,
							voxelY:      vy,
							voxelZ:      vz,
							staticID:    vc.ID,
						})
					})
				}
			}

			if a.Shape.Kind != ShapeCompound {
				testStaticForChild(-1, p.worldShape, p.worldBounds, a.Layer, a.Mask)
			} else {
				for ci, child := range p.worldShape.Children {
					if child.Trigger {
						continue
					}
					cs := child.LocalShape()
					layerA, maskA, _ := a.childEffective(ci)
					testStaticForChild(ci, cs, cs.Bounds(), layerA, maskA)
				}
			}
		}
	}

	// Sort contacts bottom-up (ascending Y, then body IDs, then child indices)
	// for fast, deterministic convergence on vertical stacks and compound bodies.
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
		if c := cmp.Compare(xbID, ybID); c != 0 {
			return c
		}
		if c := cmp.Compare(x.childA, y.childA); c != 0 {
			return c
		}
		return cmp.Compare(x.childB, y.childB)
	})

	// 3. Apply warm-start impulses.
	for i := range contacts {
		c := &contacts[i]
		if c.isStaticTri || c.isVoxel {
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
			_, hasTA := activeWorldInvInertia(c.a)
			_, hasTB := activeWorldInvInertia(c.b)
			if invA+invB+invIA+invIB == 0 && !hasTA && !hasTB {
				continue
			}

			// Normal impulse with accumulated clamping >= 0, using the effective mass at the contact point.
			var kn float32
			if hasTA || hasTB {
				kn = contactEffectiveMassBodies(c.a, c.rA, c.b, c.rB, c.normal)
			} else {
				kn = contactEffectiveMass(invA, invIA, c.rA, invB, invIB, c.rB, c.normal)
			}
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
					var kt float32
					if hasTA || hasTB {
						kt = contactEffectiveMassBodies(c.a, c.rA, c.b, c.rB, tdir)
					} else {
						kt = contactEffectiveMass(invA, invIA, c.rA, invB, invIB, c.rB, tdir)
					}
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

	// 4a. Check breakable joints against their accumulated step reaction force/torque
	// and remove broken joints before positional correction.
	w.evaluateBreakableJoints()

	// 4b. Shock propagation, velocity half: sequential impulses converge slowly when a heavy body rests on a light one, and the leftover approach velocity makes stacks bob instead of settling. One bottom-up pass stops each upper body against the one below it, already final (#546).
	up, haveUp := gravityUp(w.Gravity)
	var shock []int
	if haveUp {
		shock = w.shockOrder(contacts, up)
		propagateShockVelocity(contacts, shock, up)
	}

	// Refresh warm-start cache with active body-body contacts only.
	clear(w.cache)
	for i := range contacts {
		c := &contacts[i]
		if c.isStaticTri || c.isVoxel || c.b == nil {
			continue
		}
		// A pair with several manifold points caches their total, which the next step shares out again.
		k := contactKey{a: c.a.ID, b: c.b.ID, childA: c.childA, childB: c.childB}
		if prev, ok := w.cache[k]; ok {
			prev.normalImpulse += c.normalImpulse
			prev.tangentImpulse = prev.tangentImpulse.Add(c.tangentImpulse)
			w.cache[k] = prev
			continue
		}
		w.cache[k] = cachedImpulse{
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
			cur := c.current()
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

	// 5b. Shock propagation, position half: one bottom-up pass that resolves each vertical contact by moving only its upper body, so a tall stack or a heavy body on a light one does not keep the penetration that Gauss-Seidel leaves under large mass ratios (#546).
	w.resolveResidualPenetration(contacts, shock, up)
	clear(contacts)
	w.contactsBuf = contacts[:0]

	// Refresh dynamic broadphase proxies with final post-solver poses so
	// post-Step raycasts, overlaps, and character queries see exact positions.
	w.bp.syncDynamicBodies(w, nil)

	// 6. Put settled bodies to sleep so resting stacks have zero drift.
	const (
		sleepVelSq    = sleepVelThreshold * sleepVelThreshold
		sleepAngVelSq = sleepAngVelThreshold * sleepAngVelThreshold
	)
	invDtSq := 1 / (dtSec * dtSec)
	for di, pid := range w.bp.dynamicList {
		b := w.bp.proxies[pid].body
		if b.Static || b.Kinematic || b.Sleeping {
			continue
		}
		dispSq := b.Pos.Sub(prevPos[di]).LenSq() * invDtSq
		// A body still sunk into a neighbor stays awake: asleep, the solver would skip the pair and freeze that penetration in place.
		if b.Vel.LenSq() < sleepVelSq && dispSq < sleepVelSq && b.AngVel.LenSq() < sleepAngVelSq && b.residualDepth <= sleepMaxDepth {
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

	// 7. A sleeping body whose solid contact (or active area trigger overlap) ended this step lost a support or force zone, so it wakes instead of hanging in place.
	// For compound bodies with multiple child contacts against the same neighbor, only wake when the body pair has no remaining solid contact on any child, so a slightly recessed child lifting clear on the first sleeping step does not cause a sleep/wake loop.
	var currentSolidPairs map[pairKey]struct{}
	for k, rec := range w.activeContacts {
		if _, still := currentPairs[k]; still {
			continue
		}
		if rec.isTrigger {
			activeArea := (rec.a != nil && rec.a.IsTrigger && rec.a.Area.Active()) ||
				(rec.b != nil && rec.b.IsTrigger && rec.b.Area.Active())
			if !activeArea {
				continue
			}
		} else if k.childA >= 0 || k.childB >= 0 {
			if currentSolidPairs == nil {
				currentSolidPairs = make(map[pairKey]struct{}, len(currentPairs))
				for ck, crec := range currentPairs {
					if !crec.isTrigger {
						currentSolidPairs[pairKey{a: ck.a, b: ck.b}] = struct{}{}
					}
				}
			}
			if _, stillTouching := currentSolidPairs[pairKey{a: k.a, b: k.b}]; stillTouching {
				continue
			}
		}
		if rec.a != nil && rec.a.Sleeping {
			rec.a.Wake()
		}
		if rec.b != nil && rec.b.Sleeping {
			rec.b.Wake()
		}
	}
	for _, b := range w.bodies {
		if !b.havePose || b.lastPos != b.Pos || b.lastRot != b.Rot {
			b.lastPos, b.lastRot, b.havePose = b.Pos, b.Rot, true
		}
	}

	// 8. Reconcile active contacts and emit deterministic Enter/Stay/Exit and joint-break events.
	w.reconcileContactEvents(currentPairs)
	if w.OnJointBreak != nil {
		for _, ev := range w.jointBreakEvents {
			w.OnJointBreak(ev)
		}
	}
}

// JointBreakEvents returns the deterministic slice of JointBreakEvents produced
// by the most recent World.Step, in joint insertion order.
func (w *World) JointBreakEvents() []JointBreakEvent {
	return w.jointBreakEvents
}

func (w *World) evaluateBreakableJoints() {
	if len(w.joints) == 0 {
		return
	}
	dst := w.joints[:0]
	brokeAny := false
	for _, j := range w.joints {
		if bj, ok := j.(breakableJoint); ok {
			if ev, broken := bj.checkBreak(); broken {
				w.jointBreakEvents = append(w.jointBreakEvents, ev)
				brokeAny = true
				continue
			}
		}
		dst = append(dst, j)
	}
	w.joints = dst
	if brokeAny {
		w.wakeAllDynamic()
	}
}

// gravityUp returns the unit direction opposite gravity, and false when gravity is zero or not finite, which leaves "up" undefined and turns shock propagation off.
func gravityUp(g Vec3) (Vec3, bool) {
	if !isFiniteVec(g) || g.LenSq() <= 1e-12 {
		return Vec3{}, false
	}
	return g.Neg().Norm(), true
}

// shockOrder returns the indices of contacts sorted from the lowest contact point along up to the highest. contacts is already in a deterministic order, so the stable sort keeps ties deterministic.
func (w *World) shockOrder(contacts []solverContact, up Vec3) []int {
	n := len(contacts)
	if cap(w.shockOrderBuf) < n {
		w.shockOrderBuf = make([]int, n)
	} else {
		w.shockOrderBuf = w.shockOrderBuf[:n]
	}
	if cap(w.shockHeightsBuf) < n {
		w.shockHeightsBuf = make([]float32, n)
	} else {
		w.shockHeightsBuf = w.shockHeightsBuf[:n]
	}
	order := w.shockOrderBuf
	heights := w.shockHeightsBuf
	for i := range contacts {
		order[i] = i
		heights[i] = contacts[i].point.Dot(up)
	}
	slices.SortStableFunc(order, func(x, y int) int { return cmp.Compare(heights[x], heights[y]) })
	return order
}

// shockUpper reports which side of a contact with normal n (pointing from b toward a) is on top: +1 for a, -1 for b, 0 for a contact more than acos(shockMinCos) away from vertical or one without a body on top.
func shockUpper(c *solverContact, n, up Vec3) int {
	switch nu := n.Dot(up); {
	case nu >= shockMinCos:
		return 1
	case nu <= -shockMinCos && c.b != nil:
		return -1
	default:
		return 0
	}
}

// propagateShockVelocity visits contacts in order (bottom up) and, for each vertical one whose bodies still approach, applies an impulse to the upper body alone that stops the approach, treating the lower body as immovable. Contacts that already separate, as after a bounce, are left alone.
func propagateShockVelocity(contacts []solverContact, order []int, up Vec3) {
	for _, i := range order {
		c := &contacts[i]
		upper, lower, rU, rL, n := c.a, c.b, c.rA, c.rB, c.normal
		switch shockUpper(c, c.normal, up) {
		case 1:
		case -1:
			upper, lower, rU, rL, n = c.b, c.a, c.rB, c.rA, c.normal.Neg()
		default:
			continue
		}
		invM, invI := activeInvMass(upper), activeInvInertia(upper)
		_, hasT := activeWorldInvInertia(upper)
		if invM+invI == 0 && !hasT {
			continue
		}
		vn := pointVel(upper, rU).Sub(pointVel(lower, rL)).Dot(n)
		if vn >= 0 {
			continue
		}
		var k float32
		if hasT {
			k = contactEffectiveMassBodies(upper, rU, nil, Vec3{}, n)
		} else {
			k = contactEffectiveMass(invM, invI, rU, 0, 0, Vec3{}, n)
		}
		if k > 0 {
			applyContactImpulse(upper, rU, n.Scale(-vn/k))
		}
	}
}

// resolveResidualPenetration runs after the Gauss-Seidel position iterations. It visits contacts in order (bottom up along up) and moves only the upper body of each vertical contact deeper than shockKeepDepth back to that depth, so the correction below is final by the time the body above is placed. Side contacts, contacts whose upper body cannot move, and every contact when order is nil (no gravity) keep the Gauss-Seidel result. It records on each body the deepest contact left afterwards, which gates sleeping, and wakes a sleeping body left deeper than sleepWakeDepth.
func (w *World) resolveResidualPenetration(contacts []solverContact, order []int, up Vec3) {
	for _, b := range w.bodies {
		if b.residualDepth != 0 {
			b.residualDepth = 0
		}
	}
	visit := func(c *solverContact) {
		cur := c.current()
		if !cur.Hit || cur.Depth <= positionSlop {
			return
		}
		depth := cur.Depth
		if order != nil && cur.Depth > shockKeepDepth {
			corr := cur.Depth - shockKeepDepth
			switch shockUpper(c, cur.Normal, up) {
			case 1:
				if activeInvMass(c.a) > 0 {
					c.a.Pos = c.a.Pos.Add(cur.Normal.Scale(corr))
					depth = shockKeepDepth
				}
			case -1:
				if activeInvMass(c.b) > 0 {
					c.b.Pos = c.b.Pos.Sub(cur.Normal.Scale(corr))
					depth = shockKeepDepth
				}
			}
		}
		c.a.residualDepth = max(c.a.residualDepth, depth)
		if c.b != nil {
			c.b.residualDepth = max(c.b.residualDepth, depth)
		}
	}
	if order != nil {
		for _, i := range order {
			visit(&contacts[i])
		}
	} else {
		for i := range contacts {
			visit(&contacts[i])
		}
	}
	for _, pid := range w.bp.dynamicList {
		b := w.bp.proxies[pid].body
		if b.Sleeping && b.residualDepth > sleepWakeDepth {
			b.Wake()
		}
	}
}

// shapeTriangleContacts returns the contacts a body's world shape s makes with a static triangle: the manifold for a convex hull or cylinder, and the single contact of testShapeTriangleSolid (with the heightfield correction when heightfield is set) otherwise.
func shapeTriangleContacts(s Shape, tri Triangle, heightfield bool) []Contact {
	if isConvexKind(s) {
		return triangleManifold(s, tri, heightfield)
	}
	var c Contact
	if heightfield {
		c = testShapeHeightfieldTriangleSolid(s, tri)
	} else {
		c = testShapeTriangleSolid(s, tri)
	}
	if !c.Hit {
		return nil
	}
	return []Contact{c}
}

// current re-tests c's pair at the bodies' present poses.
func (c *solverContact) current() Contact {
	switch {
	case c.isVoxel:
		return c.voxel.testVoxel(c.a.worldChildShape(c.childA), c.voxelX, c.voxelY, c.voxelZ)
	case c.isHeightfield:
		return testShapeHeightfieldTriangleSolid(c.a.worldChildShape(c.childA), c.tri)
	case c.isStaticTri:
		return testShapeTriangleSolid(c.a.worldChildShape(c.childA), c.tri)
	default:
		return testShapesSolid(c.a.worldChildShape(c.childA), c.b.worldChildShape(c.childB))
	}
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

// applyContactImpulse applies impulse p at offset r from b's center of mass to its linear and angular velocity.
func applyContactImpulse(b *Body, r, p Vec3) {
	if b == nil {
		return
	}
	if invM := activeInvMass(b); invM > 0 {
		b.Vel = b.Vel.Add(p.Scale(invM))
	}
	if invT, ok := activeWorldInvInertia(b); ok {
		b.AngVel = b.AngVel.Add(invT.MulVec(r.Cross(p)))
		return
	}
	if invI := activeInvInertia(b); invI > 0 {
		b.AngVel = b.AngVel.Add(r.Cross(p).Scale(invI))
	}
}

// contactEffectiveMass returns the inverse effective mass of a contact along unit direction d.
func contactEffectiveMass(invA, invIA float32, rA Vec3, invB, invIB float32, rB Vec3, d Vec3) float32 {
	tA := float32(invIA * rA.Cross(d).LenSq())
	tB := float32(invIB * rB.Cross(d).LenSq())
	return float32(float32(invA+invB)+tA) + tB
}
