package physics3d

import (
	"cmp"
	"math"
	"slices"
	"time"
)

const (
	defaultSolverIterations = 8
	restitutionVelThreshold = 1.0
	positionSlop            = 0.001
	positionPercent         = 0.8
	manifoldPositionPercent = 0.2
	maxPositionCorrection   = 0.2
	warmStartMatchTolSq     = 0.04 * 0.04
	maxManifoldPoints       = 4
	// sleepMaxDepth is the deepest residual contact a body may keep and still fall asleep; a sleeping body left deeper than sleepWakeDepth by a neighbor wakes so the solver can push it out (#546).
	sleepMaxDepth  = 0.005
	sleepWakeDepth = 2 * sleepMaxDepth
	// shockMinCos is the smallest cosine between a contact normal and the up direction (-Gravity) for which the shock-propagation passes treat the lower body as immovable, about 45 degrees.
	shockMinCos = 0.7
	// shockKeepDepth is the overlap the shock pass leaves in a vertical contact. Resolving it fully would let the next step's integration separate a resting pair by a hair, dropping the contact and its warm-start impulse and making the stack bob; 1 mm keeps the contact alive across steps.
	shockKeepDepth = 0.001
	// shockMassRatio activates shock propagation on any vertical support column containing an upper body heavier than shockMassRatio times the dynamic body beneath it (#810).
	shockMassRatio = 4.0
	// shockStackThreshold activates shock propagation on vertical support columns taller than shockStackThreshold stacked bodies; normal stacks up to 10+ bodies settle via multi-point manifolds and warm starting alone without shock intervention (#810).
	shockStackThreshold = 12
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
	// InvInertia is the body's scalar inverse rotational inertia fallback.
	// AddBody computes both InvInertiaTensor and InvInertia from Shape and Mass
	// unless LockRotation is set or an explicit inertia override is provided.
	InvInertia float32
	// CenterOfMass is the body's center of mass in local coordinates relative to Pos.
	// For dynamic compound bodies, AddBody computes it from all solid children
	// unless already set or overridden with SetCenterOfMass.
	CenterOfMass Vec3
	// InvInertiaTensor is the 3x3 symmetric inverse inertia tensor in local
	// body space about CenterOfMass. AddBody computes it from Shape and Mass
	// (diagonal for axis-aligned boxes, spheres, and axis-aligned capsules or
	// cylinders; full symmetric for rotated, offset, convex-hull, and compound
	// shapes) unless LockRotation is set or an explicit override is provided via
	// InvInertiaTensor, SetInertia, SetInertiaTensor, or scalar InvInertia.
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
	// integrating it until woken by Wake, ApplyImpulse, or an active collision
	// or disturbance reaching its island.
	Sleeping bool
	// AllowSleep reports whether a dynamic body may fall asleep when resting.
	// NewDynamicBody initializes it to true; on a zero-initialized Body literal
	// it defaults to true unless SetAllowSleep(false) is called or AllowSleep is
	// modified after AddBody. Setting AllowSleep to false keeps this body and
	// every body in its contact/joint island awake.
	AllowSleep bool
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
	// UserData holds an arbitrary caller-owned value associated with this body.
	// It is excluded from snapshots and checksums, and preserved across in-place
	// World.Restore calls when the body is matched by ID.
	UserData any

	gravityScaleSet    bool
	allowSleepSet      bool
	customCenterOfMass bool
	customInertia      bool
	sleepTimer         float32
	// residualDepth is the deepest contact b was left in by the last Step's final position pass.
	residualDepth float32
	// lastPos and lastRot are the pose at the end of the previous Step, so a kinematic body moved by setting Pos or Rot directly still wakes what it touches.
	lastPos        Vec3
	lastRot        Quat
	havePose       bool
	restingOnVoxel bool
	islandStamp    uint64
	islandIdx      int
}

// NewStaticBody returns a static Body placed at pos with default friction.
func NewStaticBody(shape Shape, pos Vec3) Body {
	return Body{
		Shape:         shape,
		Pos:           pos,
		Rot:           QuatIdentity(),
		Static:        true,
		Friction:      0.5,
		AllowSleep:    true,
		allowSleepSet: true,
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
		AllowSleep:      true,
		allowSleepSet:   true,
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
	b.customCenterOfMass = false
	b.customInertia = false
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
			b.InvInertiaTensor = computeShapeInvInertiaTensor(b.Shape, b.Mass, b.CenterOfMass)
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

type cachedPoint struct {
	localA         Vec3
	localB         Vec3
	featureID      uint32
	normalImpulse  float32
	tangentImpulse Vec3
}

type cachedManifold struct {
	normal Vec3
	count  int
	points [maxManifoldPoints]cachedPoint
}

type solverContact struct {
	a, b           *Body
	childA, childB int
	pairPoint      Vec3
	normal         Vec3
	depth          float32
	point          Vec3
	// rA and rB are the contact point relative to each body's center of mass, so impulses also apply torque (#546).
	rA, rB         Vec3
	localA, localB Vec3
	featureID      uint32
	hasPlane       bool
	refIsA         bool
	localRefPoint  Vec3
	localRefNormal Vec3
	localIncPoint  Vec3
	incRadius      float32
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
	// DisableSleep keeps every body awake. Step wakes bodies that were sleeping
	// when it is set.
	DisableSleep bool
	// SleepLinearThreshold is the linear speed in units per second below which
	// a body counts as resting; zero, a negative value, NaN, or an infinity
	// means DefaultSleepLinearThreshold.
	SleepLinearThreshold float32
	// SleepAngularThreshold is the angular speed in radians per second below
	// which a body counts as resting; zero, a negative value, NaN, or an
	// infinity means DefaultSleepAngularThreshold.
	SleepAngularThreshold float32
	// TimeToSleep is how many seconds every body of an island must rest before
	// the island sleeps; zero, a negative value, NaN, or an infinity means
	// DefaultTimeToSleep.
	TimeToSleep float32
	// OnContact is invoked deterministically for non-trigger ContactEvents
	// (ContactEnter, ContactStay, ContactExit). Calling RemoveBody inside
	// OnContact immediately emits ContactExit for active overlaps of the removed
	// body and suppresses any remaining queued events for it in that step.
	OnContact func(ContactEvent)
	// OnTrigger is invoked deterministically for trigger ContactEvents
	// (ContactEnter, ContactStay, ContactExit). Calling RemoveBody inside
	// OnTrigger follows the same immediate-Exit and suppression rules as OnContact.
	OnTrigger func(ContactEvent)
	// OnJointBreak is invoked deterministically for each joint broken during
	// World.Step in joint insertion order.
	OnJointBreak func(JointBreakEvent)

	bodies             []*Body
	nextID             int
	nextColliderID     int
	cache              map[contactKey]cachedManifold
	activeContacts     map[contactKey]activeContactRecord
	nextActiveContacts map[contactKey]activeContactRecord
	contactEvents      []ContactEvent
	jointBreakEvents   []JointBreakEvent
	triMeshes          []*TriMeshCollider
	heightfields       []*HeightfieldCollider
	voxels             []*VoxelCollider
	joints             []Joint3D
	ignoredPairs       map[pairKey]struct{}
	disableShock       bool

	bp                    broadphase3D
	contactsBuf           []solverContact
	manifoldBuf           []manifoldPoint
	shockOrderBuf         []int
	shockHeightsBuf       []float32
	shockStackAbove       map[int]int
	shockColBuf           map[int]bool
	shockMaskBuf          []bool
	curKeysBuf            []contactKey
	exitKeysBuf           []contactKey
	pendingEventsBuf      []pendingContactEvent
	dispatchingContacts   bool
	removedDuringDispatch map[int]struct{}
	islandStamp           uint64
	islandParent          []int
	islandRest            []float32
	islandAwake           []uint8
}

// NewWorld returns a World with standard Y-up gravity (0, -9.81, 0) and 8
// solver iterations.
func NewWorld() *World {
	return &World{
		Gravity:          Vec3{Y: -9.81},
		SolverIterations: defaultSolverIterations,
		cache:            make(map[contactKey]cachedManifold),
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
				if !b.customCenterOfMass && b.CenterOfMass == (Vec3{}) {
					b.CenterOfMass = props.CenterOfMass
				}
				if !b.customInertia && b.InvInertiaTensor.IsZero() && b.InvInertia <= 0 {
					if b.CenterOfMass == props.CenterOfMass {
						b.InvInertiaTensor = props.InvInertia
						b.InvInertia = props.ScalarInvI
					} else {
						b.InvInertiaTensor = invertShiftedInertia(props.Inertia, float64(props.Mass), props.CenterOfMass.Sub(b.CenterOfMass))
						b.InvInertia = scalarInvInertiaFromTensor(b.InvInertiaTensor)
					}
				} else if !b.InvInertiaTensor.IsZero() && b.InvInertia <= 0 {
					b.InvInertia = scalarInvInertiaFromTensor(b.InvInertiaTensor)
				}
			} else if !b.customInertia && b.InvInertiaTensor.IsZero() && b.InvInertia <= 0 {
				b.InvInertia = computeDefaultInvInertia(b.Shape, b.Mass)
			}
		} else if !b.customInertia && b.InvInertiaTensor.IsZero() && b.InvInertia <= 0 {
			b.InvInertiaTensor = computeShapeInvInertiaTensor(b.Shape, b.Mass, b.CenterOfMass)
			b.InvInertia = computeDefaultInvInertia(b.Shape, b.Mass)
		} else if !b.InvInertiaTensor.IsZero() && b.InvInertia <= 0 {
			b.InvInertia = scalarInvInertiaFromTensor(b.InvInertiaTensor)
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
	if !b.AllowSleep && !b.allowSleepSet {
		b.AllowSleep = true
	}
	b.allowSleepSet = true
	if !b.AllowSleep || w.DisableSleep {
		b.Sleeping = false
		b.sleepTimer = 0
	}

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
// stacks react. When called from inside OnContact or OnTrigger during World.Step,
// active overlaps involving the removed body emit ContactExit immediately and
// any remaining queued events for that body in the same step are suppressed.
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
	if w.dispatchingContacts {
		if w.removedDuringDispatch == nil {
			w.removedDuringDispatch = make(map[int]struct{})
		}
		w.removedDuringDispatch[id] = struct{}{}
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
		w.cache = make(map[contactKey]cachedManifold)
	}

	// Reconcile any external pose/shape/static/trigger mutations once per step.
	w.bp.syncAllBodies(w)

	// Wake sleeping bodies and islands disturbed since the last step (direct
	// pose/velocity edits, DisableSleep, AllowSleep, kinematic/static motion,
	// voxels, active joint motors/drives), then wake any sleeping island
	// touched by an awake or moving body before velocity integration and
	// narrowphase contact collection so an awake body is never solved against
	// a sleeping one.
	w.wakeDisturbed(dtSec)
	jointIgnored := w.buildJointIgnoredPairs()
	pairs := w.bp.findBodyPairs()
	w.wakeTouched(pairs, jointIgnored)

	dynCount := len(w.bp.dynamicList)
	if cap(w.bp.dynPrevPos) < dynCount {
		w.bp.dynPrevPos = make([]Vec3, dynCount)
	} else {
		w.bp.dynPrevPos = w.bp.dynPrevPos[:dynCount]
	}
	prevPos := w.bp.dynPrevPos
	areaTriggers := w.collectActiveAreaTriggers()

	// 1. Integrate velocities (gravity, areas, damping, gyroscopic Euler torque).
	// Positions are integrated after the velocity solver so resting stacks cancel
	// gravity before moving, eliminating per-step floor penetration waves (#810).
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
			if b.AngVel.LenSq() > 0 {
				if angDamp := b.effectiveAngularDamping(); angDamp > 0 {
					b.AngVel = b.AngVel.Scale(1 / (1 + float32(angDamp*dtSec)))
				}
				integrateGyroscopic(b, dtSec)
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
		if b.AngVel.LenSq() > 0 {
			angDamp := eff.AngularDamping + eff.FluidAngularDrag
			if angDamp > 0 {
				b.AngVel = b.AngVel.Scale(1 / (1 + float32(angDamp*dtSec)))
			}
			integrateGyroscopic(b, dtSec)
		}
	}

	// 2. Deterministic narrowphase pair detection with up to 4-point
	// contact manifolds and per-point warm-start lookup.
	if w.nextActiveContacts == nil {
		w.nextActiveContacts = make(map[contactKey]activeContactRecord)
	} else {
		clear(w.nextActiveContacts)
	}
	currentPairs := w.nextActiveContacts

	contacts := w.contactsBuf[:0]
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
			return
		}
		if (a.Static || a.Kinematic || a.Sleeping) && (b.Static || b.Kinematic || b.Sleeping) {
			return
		}

		w.manifoldBuf = buildShapeManifold(shapeA, shapeB, c, w.manifoldBuf[:0])
		restitution := max(a.effectiveRestitution(), b.effectiveRestitution())
		friction := sqrt32(a.effectiveFriction() * b.effectiveFriction())
		cacheKey := contactKey{a: a.ID, b: b.ID, childA: ci, childB: cj}
		cached, warm := w.cache[cacheKey]
		warm = warm && cached.count > 0 && cached.normal.Dot(c.Normal) > 0.95
		var used [maxManifoldPoints]bool

		comA, comB := a.WorldCenterOfMass(), b.WorldCenterOfMass()
		invRotA := normalizedQuat(a.Rot).Conjugate()
		invRotB := normalizedQuat(b.Rot).Conjugate()
		for _, mp := range w.manifoldBuf {
			rA, rB := mp.point.Sub(comA), mp.point.Sub(comB)
			localA := invRotA.RotateVec(mp.point.Sub(a.Pos))
			localB := invRotB.RotateVec(mp.point.Sub(b.Pos))
			rv := pointVel(a, rA).Sub(pointVel(b, rB))
			vn := rv.Dot(c.Normal)
			var bias float32
			if vn < -restitutionVelThreshold {
				bias = -restitution * vn
			}
			sc := solverContact{
				a:           a,
				b:           b,
				childA:      ci,
				childB:      cj,
				pairPoint:   c.Point,
				normal:      c.Normal,
				depth:       mp.depth,
				point:       mp.point,
				rA:          rA,
				rB:          rB,
				localA:      localA,
				localB:      localB,
				featureID:   mp.featureID,
				hasPlane:    mp.hasPlane,
				refIsA:      mp.refIsA,
				incRadius:   mp.incRadius,
				restitution: restitution,
				friction:    friction,
				bias:        bias,
			}
			if mp.hasPlane {
				if mp.refIsA {
					sc.localRefPoint = invRotA.RotateVec(mp.refPoint.Sub(a.Pos))
					sc.localRefNormal = invRotA.RotateVec(mp.refNormal).Norm()
					sc.localIncPoint = invRotB.RotateVec(mp.incPoint.Sub(b.Pos))
				} else {
					sc.localRefPoint = invRotB.RotateVec(mp.refPoint.Sub(b.Pos))
					sc.localRefNormal = invRotB.RotateVec(mp.refNormal).Norm()
					sc.localIncPoint = invRotA.RotateVec(mp.incPoint.Sub(a.Pos))
				}
			}
			if warm {
				matchIdx := matchCachedPoint(&cached, &used, mp.featureID, localA, localB)
				if matchIdx >= 0 {
					used[matchIdx] = true
					cp := &cached.points[matchIdx]
					sc.normalImpulse = cp.normalImpulse
					sc.tangentImpulse = cp.tangentImpulse.Sub(c.Normal.Scale(cp.tangentImpulse.Dot(c.Normal)))
				}
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
						c := testShapeTriangleSolid(childShape, tri)
						if !c.Hit {
							return
						}
						invRotA := normalizedQuat(a.Rot).Conjugate()
						w.manifoldBuf = buildTriangleManifold(childShape, tri, c, false, w.manifoldBuf[:0])
						restitution := max(a.effectiveRestitution(), tm.Restitution)
						friction := sqrt32(a.effectiveFriction() * tm.Friction)
						for _, mp := range w.manifoldBuf {
							rA := mp.point.Sub(comA)
							vn := pointVel(a, rA).Dot(c.Normal)
							var bias float32
							if vn < -restitutionVelThreshold {
								bias = -restitution * vn
							}
							sc := solverContact{
								a:           a,
								childA:      ci,
								childB:      -1,
								pairPoint:   c.Point,
								normal:      c.Normal,
								depth:       mp.depth,
								point:       mp.point,
								rA:          rA,
								featureID:   mp.featureID,
								hasPlane:    mp.hasPlane,
								refIsA:      false,
								incRadius:   mp.incRadius,
								restitution: restitution,
								friction:    friction,
								bias:        bias,
								isStaticTri: true,
								tri:         tri,
								staticID:    tm.ID,
							}
							if mp.hasPlane {
								sc.localRefPoint = mp.refPoint
								sc.localRefNormal = mp.refNormal
								sc.localIncPoint = invRotA.RotateVec(mp.incPoint.Sub(a.Pos))
							}
							contacts = append(contacts, sc)
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
						rawC := testShapeTriangleSolid(childShape, tri)
						c := testShapeHeightfieldTriangleSolid(childShape, tri)
						if !c.Hit {
							return
						}
						flipped := rawC.Hit && rawC.Normal.Y < 0
						invRotA := normalizedQuat(a.Rot).Conjugate()
						w.manifoldBuf = buildTriangleManifold(childShape, tri, c, flipped, w.manifoldBuf[:0])
						restitution := max(a.effectiveRestitution(), hf.Restitution)
						friction := sqrt32(a.effectiveFriction() * hf.Friction)
						for _, mp := range w.manifoldBuf {
							rA := mp.point.Sub(comA)
							vn := pointVel(a, rA).Dot(c.Normal)
							var bias float32
							if vn < -restitutionVelThreshold {
								bias = -restitution * vn
							}
							sc := solverContact{
								a:             a,
								childA:        ci,
								childB:        -1,
								pairPoint:     c.Point,
								normal:        c.Normal,
								depth:         mp.depth,
								point:         mp.point,
								rA:            rA,
								featureID:     mp.featureID,
								hasPlane:      mp.hasPlane,
								refIsA:        false,
								incRadius:     mp.incRadius,
								restitution:   restitution,
								friction:      friction,
								bias:          bias,
								isStaticTri:   true,
								isHeightfield: true,
								tri:           tri,
								staticID:      hf.ID,
							}
							if mp.hasPlane {
								sc.localRefPoint = mp.refPoint
								sc.localRefNormal = mp.refNormal
								sc.localIncPoint = invRotA.RotateVec(mp.incPoint.Sub(a.Pos))
							}
							contacts = append(contacts, sc)
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
							pairPoint:   c.Point,
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

	// Sort contacts bottom-up by pair height (keeping all manifold points of a
	// pair contiguous) for fast, deterministic convergence on vertical stacks.
	slices.SortStableFunc(contacts, func(x, y solverContact) int {
		if c := cmp.Compare(x.pairPoint.Y, y.pairPoint.Y); c != 0 {
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
	// Multi-point manifolds attempt a regularized m x m (m <= 4) Cholesky block
	// normal solve first so flat resting faces distribute load symmetrically with
	// zero corner-order tipping torque, falling back to clamped per-point PGS
	// whenever any point would separate (lambda < 0). Friction runs after the
	// manifold's normal impulses so balanced faces never trigger spurious lateral
	// friction impulses.
	solveSingleNormal := func(c *solverContact) {
		invA, invIA := activeInvMass(c.a), activeInvInertia(c.a)
		var invB, invIB float32
		if c.b != nil {
			invB, invIB = activeInvMass(c.b), activeInvInertia(c.b)
		}
		_, hasTA := activeWorldInvInertia(c.a)
		_, hasTB := activeWorldInvInertia(c.b)
		if invA+invB+invIA+invIB == 0 && !hasTA && !hasTB {
			return
		}
		var kn float32
		if hasTA || hasTB {
			kn = contactEffectiveMassBodies(c.a, c.rA, c.b, c.rB, c.normal)
		} else {
			kn = contactEffectiveMass(invA, invIA, c.rA, invB, invIB, c.rB, c.normal)
		}
		if kn <= 0 {
			return
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
		if appliedJ != 0 {
			impN := c.normal.Scale(appliedJ)
			applyContactImpulse(c.a, c.rA, impN)
			applyContactImpulse(c.b, c.rB, impN.Neg())
		}
	}

	solveSingleFriction := func(c *solverContact) {
		if c.friction <= 0 || c.normalImpulse <= 0 {
			return
		}
		invA, invIA := activeInvMass(c.a), activeInvInertia(c.a)
		var invB, invIB float32
		if c.b != nil {
			invB, invIB = activeInvMass(c.b), activeInvInertia(c.b)
		}
		_, hasTA := activeWorldInvInertia(c.a)
		_, hasTB := activeWorldInvInertia(c.b)
		if invA+invB+invIA+invIB == 0 && !hasTA && !hasTB {
			return
		}
		rv := pointVel(c.a, c.rA).Sub(pointVel(c.b, c.rB))
		vt := rv.Sub(c.normal.Scale(rv.Dot(c.normal)))
		vtLen := vt.Len()
		if vtLen <= 1e-9 {
			return
		}
		tdir := vt.Scale(1 / vtLen)
		var kt float32
		if hasTA || hasTB {
			kt = contactEffectiveMassBodies(c.a, c.rA, c.b, c.rB, tdir)
		} else {
			kt = contactEffectiveMass(invA, invIA, c.rA, invB, invIB, c.rB, tdir)
		}
		if kt <= 0 {
			return
		}
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

	for iter := 0; iter < iters; iter++ {
		for start := 0; start < len(contacts); {
			end := manifoldGroupEnd(contacts, start)
			count := end - start
			if count >= 2 && count <= maxManifoldPoints && solveManifoldBlockNormal(contacts[start:end]) {
				// Block normal succeeded; solve friction across the manifold.
				if iter%2 == 0 {
					for i := start; i < end; i++ {
						solveSingleFriction(&contacts[i])
					}
				} else {
					for i := end - 1; i >= start; i-- {
						solveSingleFriction(&contacts[i])
					}
				}
			} else if iter%2 == 0 {
				for i := start; i < end; i++ {
					solveSingleNormal(&contacts[i])
				}
				for i := start; i < end; i++ {
					solveSingleFriction(&contacts[i])
				}
			} else {
				for i := end - 1; i >= start; i-- {
					solveSingleNormal(&contacts[i])
				}
				for i := end - 1; i >= start; i-- {
					solveSingleFriction(&contacts[i])
				}
			}
			start = end
		}

		for _, j := range w.joints {
			j.SolveVelocity(dtSec)
		}
	}

	// 4a. Check breakable joints against their accumulated step reaction force/torque
	// and remove broken joints before positional correction.
	w.evaluateBreakableJoints()

	// 4b. Scoped shock propagation, velocity half: only intervenes on vertical
	// columns with large upper-to-lower mass ratios (> shockMassRatio) or tall
	// stacks (> shockStackThreshold bodies) where iterative PGS alone converges
	// slowly; normal stacks up to 10+ bodies settle via multi-point manifolds
	// and warm starting without shock intervention (#810).
	up, haveUp := gravityUp(w.Gravity)
	var shock []int
	var shockMask []bool
	if haveUp && !w.disableShock {
		shock = w.shockOrder(contacts, up)
		shockMask = w.computeShockMask(contacts, shock, up)
		if shockMask != nil {
			propagateShockVelocity(contacts, shock, shockMask, up)
		}
	}

	// Refresh per-point warm-start cache with active body-body contacts, keeping
	// cached impulses for sleeping/fixed pairs that remain in contact so a
	// waking island resumes with its resting support.
	for k := range w.cache {
		ck, _ := canonicalContactKey(k.a, k.childA, k.b, k.childB)
		rec, still := currentPairs[ck]
		if !still || rec.isTrigger || rec.a.awakeDynamic() || rec.b.awakeDynamic() {
			delete(w.cache, k)
		}
	}
	for i := range contacts {
		c := &contacts[i]
		if c.isStaticTri || c.isVoxel || c.b == nil {
			continue
		}
		k := contactKey{a: c.a.ID, b: c.b.ID, childA: c.childA, childB: c.childB}
		cm := w.cache[k]
		if cm.count == 0 {
			cm.normal = c.normal
		}
		if cm.count < maxManifoldPoints {
			cm.points[cm.count] = cachedPoint{
				localA:         c.localA,
				localB:         c.localB,
				featureID:      c.featureID,
				normalImpulse:  c.normalImpulse,
				tangentImpulse: c.tangentImpulse,
			}
			cm.count++
			w.cache[k] = cm
		}
	}

	// 4c. Integrate positions and orientations using post-solver velocities.
	for _, pid := range w.bp.dynamicList {
		b := w.bp.proxies[pid].body
		if b.Static || b.Kinematic || b.Sleeping {
			continue
		}
		if b.CenterOfMass == (Vec3{}) {
			b.Pos = b.Pos.Add(b.Vel.Scale(dtSec))
			if b.AngVel.LenSq() > 0 {
				b.Rot = IntegrateQuat(b.Rot, b.AngVel, dtSec)
			}
		} else {
			comWorld := b.WorldCenterOfMass().Add(b.Vel.Scale(dtSec))
			if b.AngVel.LenSq() > 0 {
				b.Rot = IntegrateQuat(b.Rot, b.AngVel, dtSec)
			}
			b.Pos = comWorld.Sub(normalizedQuat(b.Rot).RotateVec(b.CenterOfMass))
		}
	}

	// 4d. Sweep fast bodies so they cannot tunnel through thin static geometry.
	for di, pid := range w.bp.dynamicList {
		b := w.bp.proxies[pid].body
		if b.Static || b.Kinematic || b.Sleeping || b.IsTrigger {
			continue
		}
		w.sweepFastBody(b, prevPos[di], jointIgnored)
	}

	// 5. Non-linear Gauss-Seidel positional/angular correction (zero velocity injection).
	// Multi-point planar manifolds are accumulated Jacobi-style across the pair's
	// active points before updating poses so flat faces produce zero symmetry-breaking
	// torque while tilted faces still receive full angular correction.
	for iter := 0; iter < iters; iter++ {
		for start := 0; start < len(contacts); {
			end := manifoldGroupEnd(contacts, start)
			first := &contacts[start]
			invA := activeInvMass(first.a)
			invTA := activeWorldInvInertiaMatrix(first.a)
			invB := float32(0)
			var invTB SymMat3
			if first.b != nil {
				invB = activeInvMass(first.b)
				invTB = activeWorldInvInertiaMatrix(first.b)
			}
			if invA+invB == 0 && invTA.IsZero() && invTB.IsZero() {
				start = end
				continue
			}

			if first.hasPlane {
				var activeIdx [maxManifoldPoints]int
				var activeN [maxManifoldPoints]Vec3
				var activeRA, activeRB [maxManifoldPoints]Vec3
				var activeRHS [maxManifoldPoints]float64
				active := 0
				comA := first.a.WorldCenterOfMass()
				var comB Vec3
				if first.b != nil {
					comB = first.b.WorldCenterOfMass()
				}
				for i := start; i < end && active < maxManifoldPoints; i++ {
					c := &contacts[i]
					if !c.hasPlane {
						continue
					}
					n, depth, pt := c.currentPlane()
					if depth <= positionSlop {
						continue
					}
					activeIdx[active] = i
					activeN[active] = n
					activeRA[active] = pt.Sub(comA)
					if c.b != nil {
						activeRB[active] = pt.Sub(comB)
					}
					activeRHS[active] = float64(min(float32((depth-positionSlop)*manifoldPositionPercent), maxPositionCorrection))
					active++
				}
				if active > 0 {
					var lam [maxManifoldPoints]float64
					solved := false
					if active == 1 {
						k := float64(invA) + float64(invB) + float64(invTA.QuadForm(activeRA[0].Cross(activeN[0]))) + float64(invTB.QuadForm(activeRB[0].Cross(activeN[0])))
						if k > 1e-12 {
							lam[0] = activeRHS[0] / k
							solved = true
						}
					} else {
						solved = solveBlockLCP(active, invA, invTA, invB, invTB, activeN[:active], activeRA[:active], activeRB[:active], activeRHS[:active], nil, lam[:active])
					}
					var linA, angA, linB, angB Vec3
					if solved {
						for k := range active {
							imp := activeN[k].Scale(float32(lam[k]))
							linA = linA.Add(imp.Scale(invA))
							if !invTA.IsZero() {
								angA = angA.Add(invTA.MulVec(activeRA[k].Cross(imp)))
							}
							if first.b != nil {
								linB = linB.Sub(imp.Scale(invB))
								if !invTB.IsZero() {
									angB = angB.Sub(invTB.MulVec(activeRB[k].Cross(imp)))
								}
							}
						}
					} else {
						scale := 1 / float32(active)
						for k := range active {
							effK := float32(float32(invA+invB)+invTA.QuadForm(activeRA[k].Cross(activeN[k]))) + invTB.QuadForm(activeRB[k].Cross(activeN[k]))
							if effK <= 1e-12 {
								continue
							}
							imp := activeN[k].Scale(float32(activeRHS[k]) * scale / effK)
							linA = linA.Add(imp.Scale(invA))
							if !invTA.IsZero() {
								angA = angA.Add(invTA.MulVec(activeRA[k].Cross(imp)))
							}
							if first.b != nil {
								linB = linB.Sub(imp.Scale(invB))
								if !invTB.IsZero() {
									angB = angB.Sub(invTB.MulVec(activeRB[k].Cross(imp)))
								}
							}
						}
					}
					if invA > 0 || !invTA.IsZero() {
						applyBodyPositionCorrection(first.a, linA, angA)
					}
					if first.b != nil && (invB > 0 || !invTB.IsZero()) {
						applyBodyPositionCorrection(first.b, linB, angB)
					}
				}
				_ = activeIdx
				start = end
				continue
			}

			invSum := invA + invB
			if invSum > 0 {
				cur := first.current()
				if cur.Hit && cur.Depth > positionSlop {
					corr := (cur.Depth - positionSlop) * positionPercent / invSum
					if invA > 0 {
						first.a.Pos = first.a.Pos.Add(cur.Normal.Scale(corr * invA))
					}
					if invB > 0 {
						first.b.Pos = first.b.Pos.Sub(cur.Normal.Scale(corr * invB))
					}
				}
			}
			start = end
		}

		for _, j := range w.joints {
			j.SolvePosition(dtSec)
		}
	}

	// 5b. Residual penetration check and scoped shock propagation (position half).
	w.resolveResidualPenetration(contacts, shock, shockMask, up)

	// Refresh dynamic broadphase proxies with final post-solver poses so
	// post-Step raycasts, overlaps, and character queries see exact positions.
	w.bp.syncDynamicBodies(w, prevPos)

	// 6. Put settled islands to sleep so resting stacks have zero drift.
	w.updateSleep(contacts, prevPos, dtSec)
	clear(contacts)
	w.contactsBuf = contacts[:0]

	// 7. A sleeping body whose solid contact (or active area trigger overlap) ended this step lost a support or force zone, so it wakes instead of hanging in place.
	// For compound bodies with multiple child contacts against the same neighbor, only wake when the body pair has no remaining solid contact on any child, so a slightly recessed child lifting clear on the first sleeping step does not cause a sleep/wake loop.
	var currentSolidPairs map[pairKey]struct{}
	wokeEnded := false
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
			wokeEnded = true
		}
		if rec.b != nil && rec.b.Sleeping {
			rec.b.Wake()
			wokeEnded = true
		}
	}
	if wokeEnded {
		w.wakeIslands()
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
		heights[i] = contacts[i].pairPoint.Dot(up)
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

// choleskySolveSub solves the subset activeIdx of symmetric matrix a for bSub
// into xSub in FMA-free float64.
func choleskySolveSub(a *[maxManifoldPoints][maxManifoldPoints]float64, b *[maxManifoldPoints]float64, activeIdx []int, xSub []float64) bool {
	k := len(activeIdx)
	if k == 0 {
		return true
	}
	var l [maxManifoldPoints][maxManifoldPoints]float64
	for i := range k {
		ai := activeIdx[i]
		for j := 0; j <= i; j++ {
			aj := activeIdx[j]
			s := a[ai][aj]
			for p := 0; p < j; p++ {
				s = s - float64(l[i][p]*l[j][p])
			}
			if i == j {
				if s <= 1e-18 || math.IsNaN(s) {
					return false
				}
				l[i][j] = math.Sqrt(s)
			} else {
				l[i][j] = s / l[j][j]
			}
		}
	}
	var y [maxManifoldPoints]float64
	for i := range k {
		s := b[activeIdx[i]]
		for p := 0; p < i; p++ {
			s = s - float64(l[i][p]*y[p])
		}
		y[i] = s / l[i][i]
	}
	for i := k - 1; i >= 0; i-- {
		s := y[i]
		for p := i + 1; p < k; p++ {
			s = s - float64(l[p][i]*xSub[p])
		}
		xSub[i] = s / l[i][i]
		if math.IsNaN(xSub[i]) || math.IsInf(xSub[i], 0) {
			return false
		}
	}
	return true
}

// solveBlockLCP solves the regularized m x m (2 <= m <= 4) complementarity
// problem A*(x - oldX) >= rhs, x >= 0, x · (A*(x - oldX) - rhs) = 0 in
// FMA-free float64 using a symmetric active-set Cholesky solver. When oldX is
// nil it is treated as zero. Because all violated constraints are deactivated
// or activated symmetrically, coplanar manifolds never incur corner-order
// tipping torque even when separating or partially lifting off.
func solveBlockLCP(
	m int,
	invA float32, invTA SymMat3,
	invB float32, invTB SymMat3,
	normals, rA, rB []Vec3,
	rhs, oldX, out []float64,
) bool {
	invM := float64(invA) + float64(invB)
	if invM == 0 && invTA.IsZero() && invTB.IsZero() {
		for i := range m {
			if oldX != nil {
				out[i] = oldX[i]
			} else {
				out[i] = 0
			}
		}
		return true
	}
	var rAxN, rBxN [maxManifoldPoints]Vec3
	var iArAxN, iBrBxN [maxManifoldPoints]Vec3
	for i := range m {
		rAxN[i] = rA[i].Cross(normals[i])
		iArAxN[i] = invTA.MulVec(rAxN[i])
		if invB > 0 || !invTB.IsZero() {
			rBxN[i] = rB[i].Cross(normals[i])
			iBrBxN[i] = invTB.MulVec(rBxN[i])
		}
	}
	var a [maxManifoldPoints][maxManifoldPoints]float64
	var diagSum float64
	for i := range m {
		for j := i; j < m; j++ {
			linTerm := float64(invM * float64(normals[i].Dot(normals[j])))
			v := linTerm + float64(rAxN[i].Dot(iArAxN[j])) + float64(rBxN[i].Dot(iBrBxN[j]))
			a[i][j] = v
			a[j][i] = v
		}
		diagSum += a[i][i]
	}
	if diagSum <= 1e-18 {
		for i := range m {
			if oldX != nil {
				out[i] = oldX[i]
			} else {
				out[i] = 0
			}
		}
		return true
	}
	reg := float64(1e-4 * (diagSum / float64(m)))
	for i := range m {
		a[i][i] = a[i][i] + reg
	}

	// Reformulate A*(x - oldX) = rhs as A*x = b with x >= 0.
	var b [maxManifoldPoints]float64
	for i := range m {
		s := rhs[i]
		if oldX != nil {
			for j := range m {
				s = s + float64(a[i][j]*oldX[j])
			}
		}
		b[i] = s
	}

	var active [maxManifoldPoints]bool
	for i := range m {
		active[i] = true
	}
	var x [maxManifoldPoints]float64
	for iter := 0; iter < 8; iter++ {
		var idxBuf [maxManifoldPoints]int
		idx := idxBuf[:0]
		for i := range m {
			if active[i] {
				idx = append(idx, i)
			}
		}
		for i := range m {
			x[i] = 0
		}
		if len(idx) > 0 {
			var xSub [maxManifoldPoints]float64
			if !choleskySolveSub(&a, &b, idx, xSub[:len(idx)]) {
				return false
			}
			removed := false
			for p, i := range idx {
				if xSub[p] < 0 {
					active[i] = false
					removed = true
				} else {
					x[i] = xSub[p]
				}
			}
			if removed {
				continue
			}
		}
		// Check complementarity of inactive points: w_i = (A*x)_i - b_i >= 0.
		minW := -1e-9
		var viol [maxManifoldPoints]float64
		for i := range m {
			if active[i] {
				continue
			}
			ax := 0.0
			for j := range m {
				if active[j] {
					ax = ax + float64(a[i][j]*x[j])
				}
			}
			w := ax - b[i]
			viol[i] = w
			if w < minW {
				minW = w
			}
		}
		added := false
		if minW < -1e-9 {
			tol := minW * 0.999999
			for i := range m {
				if !active[i] && viol[i] <= tol {
					active[i] = true
					added = true
				}
			}
		}
		if !added {
			for i := range m {
				out[i] = x[i]
			}
			return true
		}
	}
	return false
}

// solveManifoldBlockNormal solves the m x m (2 <= m <= 4) normal impulse LCP
// across all points of a single manifold simultaneously in FMA-free float64 so
// flat resting, separating, and partially lifting faces distribute load
// symmetrically without corner-order tipping torque.
func solveManifoldBlockNormal(group []solverContact) bool {
	m := len(group)
	first := &group[0]
	invA := activeInvMass(first.a)
	invTA := activeWorldInvInertiaMatrix(first.a)
	var invB float32
	var invTB SymMat3
	if first.b != nil {
		invB = activeInvMass(first.b)
		invTB = activeWorldInvInertiaMatrix(first.b)
	}
	if invA+invB == 0 && invTA.IsZero() && invTB.IsZero() {
		return true
	}

	var normals, rA, rB [maxManifoldPoints]Vec3
	var rhs, oldJ, newJD [maxManifoldPoints]float64
	for i := range m {
		c := &group[i]
		normals[i] = c.normal
		rA[i] = c.rA
		rB[i] = c.rB
		rv := pointVel(c.a, c.rA).Sub(pointVel(c.b, c.rB))
		vn := rv.Dot(c.normal)
		rhs[i] = -float64(vn - c.bias)
		oldJ[i] = float64(c.normalImpulse)
	}
	if !solveBlockLCP(m, invA, invTA, invB, invTB, normals[:m], rA[:m], rB[:m], rhs[:m], oldJ[:m], newJD[:m]) {
		return false
	}

	var newJ [maxManifoldPoints]float32
	for i := range m {
		nj := float32(newJD[i])
		if !isFinite32(nj) || nj < 0 {
			return false
		}
		newJ[i] = nj
	}

	// Apply all m normal impulse deltas simultaneously.
	var totalLin Vec3
	var totalAngA, totalAngB Vec3
	for i := range m {
		c := &group[i]
		applied := newJ[i] - c.normalImpulse
		c.normalImpulse = newJ[i]
		if applied == 0 {
			continue
		}
		imp := c.normal.Scale(applied)
		totalLin = totalLin.Add(imp)
		totalAngA = totalAngA.Add(c.rA.Cross(imp))
		if c.b != nil {
			totalAngB = totalAngB.Add(c.rB.Cross(imp))
		}
	}
	if invA > 0 {
		first.a.Vel = first.a.Vel.Add(totalLin.Scale(invA))
	}
	if !invTA.IsZero() {
		first.a.AngVel = first.a.AngVel.Add(invTA.MulVec(totalAngA))
	}
	if first.b != nil {
		if invB > 0 {
			first.b.Vel = first.b.Vel.Sub(totalLin.Scale(invB))
		}
		if !invTB.IsZero() {
			first.b.AngVel = first.b.AngVel.Sub(invTB.MulVec(totalAngB))
		}
	}
	return true
}

// manifoldGroupEnd returns the exclusive end index of the contiguous contact
// manifold starting at start (matching body pair, child indices, and static collider).
func manifoldGroupEnd(contacts []solverContact, start int) int {
	a := &contacts[start]
	end := start + 1
	for end < len(contacts) {
		b := &contacts[end]
		if b.a != a.a || b.b != a.b || b.childA != a.childA || b.childB != a.childB ||
			b.staticID != a.staticID || b.isStaticTri != a.isStaticTri || b.isVoxel != a.isVoxel {
			break
		}
		if a.isStaticTri && b.tri != a.tri {
			break
		}
		if a.isVoxel && (b.voxelX != a.voxelX || b.voxelY != a.voxelY || b.voxelZ != a.voxelZ) {
			break
		}
		end++
	}
	return end
}

// computeShockMask identifies which contacts belong to vertical support columns
// that still benefit from shock propagation: columns containing a high mass
// ratio (upper.Mass > shockMassRatio * lower.Mass) or taller than
// shockStackThreshold stacked bodies. It returns nil when no contact qualifies,
// so ordinary stacks (including 10-box uniform towers) bypass shock propagation
// completely with zero overhead and zero hysteresis.
func (w *World) computeShockMask(contacts []solverContact, order []int, up Vec3) []bool {
	if len(order) == 0 {
		return nil
	}
	if w.shockStackAbove != nil {
		clear(w.shockStackAbove)
	}
	if w.shockColBuf != nil {
		clear(w.shockColBuf)
	}
	anyShock := false

	// Pass 1 (top-down): accumulate stack depth above each dynamic body and
	// propagate high-mass-ratio / tall-column flags downward to the base.
	for endIdx := len(order); endIdx > 0; {
		iLast := order[endIdx-1]
		c0 := &contacts[iLast]
		startIdx := endIdx - 1
		for startIdx > 0 {
			cp := &contacts[order[startIdx-1]]
			if cp.a != c0.a || cp.b != c0.b || cp.childA != c0.childA || cp.childB != c0.childB || cp.staticID != c0.staticID {
				break
			}
			startIdx--
		}
		upper, lower := c0.a, c0.b
		side := shockUpper(c0, c0.normal, up)
		if side == -1 {
			upper, lower = c0.b, c0.a
		}
		if side != 0 && upper != nil && !upper.Static && !upper.Kinematic {
			chain := 1
			if w.shockStackAbove != nil {
				chain = w.shockStackAbove[upper.ID] + 1
			}
			heavyOnLight := lower != nil && !lower.Static && !lower.Kinematic && lower.Mass > 0 && upper.Mass > shockMassRatio*lower.Mass
			need := heavyOnLight || chain > shockStackThreshold || (w.shockColBuf != nil && w.shockColBuf[upper.ID])
			if need {
				if w.shockColBuf == nil {
					w.shockColBuf = make(map[int]bool)
				}
				w.shockColBuf[upper.ID] = true
				anyShock = true
			}
			if lower != nil && !lower.Static && !lower.Kinematic {
				if w.shockStackAbove == nil {
					w.shockStackAbove = make(map[int]int)
				}
				if chain > w.shockStackAbove[lower.ID] {
					w.shockStackAbove[lower.ID] = chain
				}
				if need {
					w.shockColBuf[lower.ID] = true
				}
			}
		}
		endIdx = startIdx
	}
	if !anyShock {
		return nil
	}

	// Pass 2 (bottom-up): propagate column flag upward so every pair in a
	// qualifying tall/heavy column receives consistent bottom-up shock help.
	if cap(w.shockMaskBuf) < len(contacts) {
		w.shockMaskBuf = make([]bool, len(contacts))
	} else {
		w.shockMaskBuf = w.shockMaskBuf[:len(contacts)]
		clear(w.shockMaskBuf)
	}
	mask := w.shockMaskBuf
	for idx := 0; idx < len(order); {
		i0 := order[idx]
		c0 := &contacts[i0]
		endIdx := idx + 1
		for endIdx < len(order) {
			cj := &contacts[order[endIdx]]
			if cj.a != c0.a || cj.b != c0.b || cj.childA != c0.childA || cj.childB != c0.childB || cj.staticID != c0.staticID {
				break
			}
			endIdx++
		}
		upper, lower := c0.a, c0.b
		side := shockUpper(c0, c0.normal, up)
		if side == -1 {
			upper, lower = c0.b, c0.a
		}
		if side != 0 && upper != nil && !upper.Static && !upper.Kinematic {
			inCol := w.shockColBuf[upper.ID] || (lower != nil && w.shockColBuf[lower.ID])
			if inCol {
				w.shockColBuf[upper.ID] = true
				for k := idx; k < endIdx; k++ {
					mask[order[k]] = true
				}
			}
		}
		idx = endIdx
	}
	return mask
}

// propagateShockVelocity visits qualifying vertical contacts in order (bottom
// up) and, for each manifold that still approaches its support, applies a
// symmetric block LCP impulse to the upper body alone that stops the approach,
// treating the lower body as immovable.
func propagateShockVelocity(contacts []solverContact, order []int, mask []bool, up Vec3) {
	for idx := 0; idx < len(order); {
		i0 := order[idx]
		c0 := &contacts[i0]
		endIdx := idx + 1
		for endIdx < len(order) {
			cj := &contacts[order[endIdx]]
			if cj.a != c0.a || cj.b != c0.b || cj.childA != c0.childA || cj.childB != c0.childB || cj.staticID != c0.staticID {
				break
			}
			endIdx++
		}
		if !mask[i0] {
			idx = endIdx
			continue
		}
		upper, lower, n := c0.a, c0.b, c0.normal
		side := shockUpper(c0, c0.normal, up)
		switch side {
		case 1:
		case -1:
			upper, lower, n = c0.b, c0.a, c0.normal.Neg()
		default:
			idx = endIdx
			continue
		}
		var normals, rUs, zeroRs [maxManifoldPoints]Vec3
		var rhs, dj [maxManifoldPoints]float64
		count := 0
		approaching := false
		for k := idx; k < endIdx && count < maxManifoldPoints; k++ {
			c := &contacts[order[k]]
			rU, rL := c.rA, c.rB
			if side == -1 {
				rU, rL = c.rB, c.rA
			}
			vUpper := pointVel(upper, rU).Dot(n)
			// Clamp lower's support velocity at <= 0 along up so shock
			// propagation stops downward approach without launching a heavy
			// upper body upward off a rebounding lighter support.
			vLower := min(pointVel(lower, rL).Dot(n), 0)
			vn := vUpper - vLower
			if vn < 0 {
				approaching = true
			}
			normals[count] = n
			rUs[count] = rU
			rhs[count] = -float64(vn)
			count++
		}
		if !approaching {
			idx = endIdx
			continue
		}
		invM := activeInvMass(upper)
		invT := activeWorldInvInertiaMatrix(upper)
		if invM == 0 && invT.IsZero() {
			idx = endIdx
			continue
		}
		solved := false
		if count == 1 {
			effK := invM + invT.QuadForm(rUs[0].Cross(n))
			if effK > 0 && rhs[0] > 0 {
				dj[0] = rhs[0] / float64(effK)
				solved = true
			}
		} else {
			solved = solveBlockLCP(count, invM, invT, 0, SymMat3{}, normals[:count], rUs[:count], zeroRs[:count], rhs[:count], nil, dj[:count])
		}
		if solved {
			var totalLin, totalAng Vec3
			for k := range count {
				if dj[k] <= 0 {
					continue
				}
				imp := n.Scale(float32(dj[k]))
				totalLin = totalLin.Add(imp)
				totalAng = totalAng.Add(rUs[k].Cross(imp))
			}
			if invM > 0 {
				upper.Vel = upper.Vel.Add(totalLin.Scale(invM))
			}
			if !invT.IsZero() {
				upper.AngVel = upper.AngVel.Add(invT.MulVec(totalAng))
			}
		} else {
			for k := range count {
				if rhs[k] <= 0 {
					continue
				}
				effK := invM + invT.QuadForm(rUs[k].Cross(n))
				if effK <= 0 {
					continue
				}
				applyContactImpulse(upper, rUs[k], n.Scale(float32(rhs[k])/effK))
			}
		}
		idx = endIdx
	}
}

// resolveResidualPenetration runs after the Gauss-Seidel position iterations. It visits contacts in order (bottom up along up) and, for contacts in shock-enabled columns (high mass ratios or tall stacks) that remain deeper than shockKeepDepth, moves only the upper body back to shockKeepDepth. Normal stacks resolved by multi-point manifolds and warm starting have mask == nil and bypass the shock push. It records on each body the deepest contact left afterwards, which gates sleeping, and wakes a sleeping body left deeper than sleepWakeDepth.
func (w *World) resolveResidualPenetration(contacts []solverContact, order []int, mask []bool, up Vec3) {
	for _, b := range w.bodies {
		if b.residualDepth != 0 {
			b.residualDepth = 0
		}
	}
	visit := func(i int, c *solverContact) {
		cur := c.current()
		if !cur.Hit || cur.Depth <= positionSlop {
			return
		}
		depth := cur.Depth
		if mask != nil && mask[i] && cur.Depth > shockKeepDepth {
			side := shockUpper(c, cur.Normal, up)
			corr := cur.Depth - shockKeepDepth
			switch side {
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
			visit(i, &contacts[i])
		}
	} else {
		for i := range contacts {
			visit(i, &contacts[i])
		}
	}
	wokeResidual := false
	for _, pid := range w.bp.dynamicList {
		b := w.bp.proxies[pid].body
		if b.Sleeping && b.residualDepth > sleepWakeDepth {
			b.Wake()
			wokeResidual = true
		}
	}
	if wokeResidual {
		w.wakeIslands()
	}
}

// matchCachedPoint finds the best unused slot in cached matching a new contact
// point by feature ID first, then by combined local-space proximity.
func matchCachedPoint(cached *cachedManifold, used *[maxManifoldPoints]bool, featureID uint32, localA, localB Vec3) int {
	if featureID != 0 {
		for k := 0; k < cached.count; k++ {
			if !used[k] && cached.points[k].featureID == featureID {
				return k
			}
		}
	}
	best := -1
	bestDistSq := float32(warmStartMatchTolSq)
	for k := 0; k < cached.count; k++ {
		if used[k] {
			continue
		}
		cp := &cached.points[k]
		if cp.localA == (Vec3{}) && cp.localB == (Vec3{}) && cp.featureID == 0 && cached.count == 1 {
			return k
		}
		dSq := localA.DistSq(cp.localA) + localB.DistSq(cp.localB)
		if dSq <= bestDistSq {
			bestDistSq = dSq
			best = k
		}
	}
	return best
}

// currentPlane recomputes the world push normal (from B toward A), penetration
// depth, and contact midpoint for a planar manifold point at the bodies'
// current poses.
func (c *solverContact) currentPlane() (normal Vec3, depth float32, point Vec3) {
	rotA := normalizedQuat(c.a.Rot)
	if c.b == nil {
		// Static triangle reference plane stored in world space.
		wRefN := c.localRefNormal
		wRefPt := c.localRefPoint
		wIncPt := c.a.Pos.Add(rotA.RotateVec(c.localIncPoint))
		depth = wRefPt.Sub(wIncPt).Dot(wRefN) + c.incRadius
		point = wIncPt.Sub(wRefN.Scale(c.incRadius - float32(depth*0.5)))
		return wRefN, depth, point
	}
	rotB := normalizedQuat(c.b.Rot)
	if c.refIsA {
		wRefN := rotA.RotateVec(c.localRefNormal)
		wRefPt := c.a.Pos.Add(rotA.RotateVec(c.localRefPoint))
		wIncPt := c.b.Pos.Add(rotB.RotateVec(c.localIncPoint))
		depth = wRefPt.Sub(wIncPt).Dot(wRefN) + c.incRadius
		point = wIncPt.Sub(wRefN.Scale(c.incRadius - float32(depth*0.5)))
		return wRefN.Neg(), depth, point
	}
	wRefN := rotB.RotateVec(c.localRefNormal)
	wRefPt := c.b.Pos.Add(rotB.RotateVec(c.localRefPoint))
	wIncPt := c.a.Pos.Add(rotA.RotateVec(c.localIncPoint))
	depth = wRefPt.Sub(wIncPt).Dot(wRefN) + c.incRadius
	point = wIncPt.Sub(wRefN.Scale(c.incRadius - float32(depth*0.5)))
	return wRefN, depth, point
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
