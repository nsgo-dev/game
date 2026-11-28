package physics

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/internal/detmath"
	"github.com/nsgo-dev/nsgo/core"
)

// speculativeDistance is how far apart a contact point of an overlapping pair may be and still join its manifold. A box resting a hair off level keeps both of its corners in the manifold, so the solver and warm starting see the same two points every step instead of rocking between one corner and the other.
const speculativeDistance = 1.0

// DefaultSolverIterations is the default solver iteration budget per RigidWorld.Step.
const DefaultSolverIterations = 10

// DefaultSubsteps is the default number of substeps per RigidWorld.Step.
const DefaultSubsteps = 4

const (
	// contactSubstepHertzFraction and contactDampingRatio configure the implicit-Euler soft contact push-out derived from the substep duration subH.
	contactSubstepHertzFraction = 0.5
	contactDampingRatio         = 2.0
	// contactSlop is the tiny overlap in pixels below which push-out rests so stacked contacts stay persistent across steps for warm starting.
	contactSlop = 0.01
	// contactMaxPushSpeed caps the soft contact push-out velocity in pixels per second so deep initial overlaps separate smoothly without launching bodies.
	contactMaxPushSpeed = 300.0
)

// RigidBody is a 2D dynamic or static rigid body simulated by RigidWorld.
//
// (X, Y) is the world-space center of mass and Angle is the rotation in radians (increasing clockwise in NSGo's +X right, +Y down screen space). Shape holds the body's geometry in world space and is synchronized with (X, Y) after each simulation step. A Box keeps its axis-aligned extents centered on (X, Y) and a Circle its center, with Angle applied by the solver; a Polygon, Capsule, Segment or Chain is stored rotated by Angle, so World queries and debug drawing see its true pose.
//
// A Polygon or Capsule assigned to Shape, in a struct literal or between steps, keeps its world position: the next Step moves (X, Y) to its Centroid, so the body turns about its center of mass, and replaces an Inertia the body derived itself (left zero, or computed by a constructor or an earlier Step) with the new shape's MomentOfInertia; an Inertia set to any other value is kept. A Segment or Chain is anchored at the current (X, Y).
//
// Segment and Chain shapes have no area, so a body holding one is always static.
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
	// Kinematic makes the body move only as it is told: by VX, VY and AngularVel, which Step integrates with no gravity, force, damping or area effect, or by SetTargetPose. The solver treats it as having infinite mass, so it pushes and carries the dynamic bodies it touches and nothing it touches moves it; it never collides with static or other kinematic bodies or tiles, and never sleeps. Step zeroes InvMass and InvInertia while it is set and derives them again from Mass and Inertia once it is cleared. Static takes precedence: a body with both set is static. A kinematic body never uses Mass, but NewRigidBox, NewRigidCircle and NewRigidBody make a body static when its mass is not positive, so build it with a positive mass or clear Static after building it with zero.
	Kinematic bool
	// Sensor makes the body detect overlaps instead of colliding: it takes part in no contact, is not hit by continuous collision, and reports a SensorEvent when a body that is not a sensor starts or stops overlapping it. A sensor moves as its kind (static, kinematic or dynamic) says, and never detects another sensor.
	Sensor bool
	Layer  LayerMask
	Mask   LayerMask
	// Bullet sweeps the body against static, kinematic and dynamic bodies and tiles on every step and stops it at the first surface it reaches, so it never passes into or through thin geometry, except that a surface it already touches at the start of a step and is driven into by no more than half its half thickness is left to the regular contacts. Without it the body is swept only on steps where a point of it moves farther than half its thickness, against static bodies and tiles only, and a shallow overlap is left to the regular contacts.
	Bullet   bool
	UserData any

	gravityScaleSet bool
	// sleeping reports that the body rests in a sleeping island: RigidWorld skips integrating and solving it until it is woken.
	sleeping bool
	// sleepEnv is the gravity, damping and fluid the body fell asleep in; a change wakes it.
	sleepEnv AreaEffect
	// sleepTime is how long, in seconds, the body has moved slower than the world's sleep thresholds.
	sleepTime float64
	// restX, restY and restAngle are the pose the body had at the end of the last step it took part in, which tells Step that it was moved by hand since.
	restX, restY, restAngle float32
	restValid               bool
	// stamp and index are scratch the current Step writes: stamp matches the world's when the body is in it, and index is its position in Bodies.
	stamp  uint64
	index  int
	forceX float64
	forceY float64
	torque float64

	// local holds a Polygon, Capsule, Segment or Chain shape relative to (X, Y) at Angle 0.
	local    Shape
	hasLocal bool
	// synced is the world Shape last derived from local at pose (syncX, syncY, syncAngle), with its bounds.
	synced                  Shape
	syncBounds              Box
	syncX, syncY, syncAngle float32
	// derivedInertia is the Inertia the body last derived from its shape; while Inertia still equals it, a new Shape recomputes it.
	derivedInertia float32

	// target is the pose SetTargetPose asked a kinematic body to reach on the next Step, when targetPending.
	target        rigidPose
	targetPending bool
	// targetVX, targetVY and targetAV are the velocity the last target gave the body, when targetDriven: a Step with no new target stops a body still moving at it.
	targetVX, targetVY, targetAV float32
	targetDriven                 bool

	// internalID is assigned by RigidWorld when ID is 0 so Restore matches pointers by identity without falling back to slice index.
	internalID uint64
}

// NewRigidBody returns a RigidBody holding s, a shape in world coordinates, with its center of mass at s's Centroid and its inertia computed from s for mass spread evenly over its area. If mass <= 0, or s is a Segment or Chain, the body is static. A Box or Circle gives the same body as NewRigidBox or NewRigidCircle.
func NewRigidBody(s Shape, mass float32) *RigidBody {
	switch s.Kind {
	case ShapeBox:
		cx, cy := s.Box.Center()
		return NewRigidBox(cx, cy, s.Box.W, s.Box.H, mass)
	case ShapeCircle:
		return NewRigidCircle(s.Circle.X, s.Circle.Y, s.Circle.R, mass)
	}
	cx, cy := s.Centroid()
	b := &RigidBody{Shape: s, X: cx, Y: cy, Friction: 0.4, GravityScale: 1, gravityScaleSet: true}
	b.ensureLocal()
	if mass <= 0 || s.Kind == ShapeSegment || s.Kind == ShapeChain || s.Empty() {
		b.Static = true
		return b
	}
	b.Mass = mass
	b.InvMass = 1 / mass
	if inertia := s.MomentOfInertia(mass); inertia > 0 {
		b.Inertia = inertia
		b.InvInertia = 1 / inertia
		b.derivedInertia = b.Inertia
	}
	return b
}

// NewStaticRigidBody returns a static (infinite-mass) RigidBody holding s, such as level terrain built from a Polygon, Segment or Chain.
func NewStaticRigidBody(s Shape) *RigidBody {
	return NewRigidBody(s, 0)
}

// hasFreeShape reports whether s is a kind the body stores in local space and rotates: a Polygon, Capsule, Segment or Chain.
func hasFreeShape(k ShapeKind) bool {
	return k == ShapePolygon || k == ShapeCapsule || k == ShapeSegment || k == ShapeChain
}

// ensureLocal re-derives the local shape when Shape was assigned since the last synchronization.
func (b *RigidBody) ensureLocal() {
	if !hasFreeShape(b.Shape.Kind) {
		b.hasLocal = false
		return
	}
	if b.hasLocal && b.Shape == b.synced {
		return
	}
	if (b.Shape.Kind == ShapePolygon || b.Shape.Kind == ShapeCapsule) && !b.Shape.Empty() {
		// The solver rotates the body about (X, Y) with an inertia taken about the shape's centroid, so the two must coincide.
		b.X, b.Y = b.Shape.Centroid()
		if b.Inertia == b.derivedInertia {
			b.Inertia, b.InvInertia = 0, 0
		}
	}
	b.local = b.Shape.toLocal(float64(b.X), float64(b.Y), float64(b.Angle))
	b.hasLocal = true
	b.setSynced(b.Shape)
}

func (b *RigidBody) setSynced(s Shape) {
	b.synced = s
	b.syncBounds = s.Bounds()
	b.syncX, b.syncY, b.syncAngle = b.X, b.Y, b.Angle
}

// poseShape returns the body's free shape in world space at its current pose.
func (b *RigidBody) poseShape() (Shape, Box) {
	if b.X == b.syncX && b.Y == b.syncY && b.Angle == b.syncAngle {
		return b.synced, b.syncBounds
	}
	s := b.local.transformed(float64(b.X), float64(b.Y), float64(b.Angle))
	return s, s.Bounds()
}

// rigidHull returns the rounded hull of a body's convex shape at its current pose.
func (b *RigidBody) rigidHull() (hull, bool) {
	switch b.Shape.Kind {
	case ShapeBox:
		if b.Shape.Box.Empty() {
			return hull{}, false
		}
		return orientedBoxHull(float64(b.X), float64(b.Y), float64(b.Shape.Box.W)*0.5, float64(b.Shape.Box.H)*0.5, float64(b.Angle)), true
	case ShapeCircle:
		if b.Shape.Circle.Empty() {
			return hull{}, false
		}
		return pointHull(float64(b.X), float64(b.Y), float64(b.Shape.Circle.R)), true
	case ShapePolygon, ShapeCapsule, ShapeSegment:
		if !b.hasLocal {
			return hull{}, false
		}
		s, _ := b.poseShape()
		return shapeHull(s)
	}
	return hull{}, false
}

// NewRigidBox returns a dynamic rectangular RigidBody centered at (x, y) with size (w, h) and mass.
// If mass <= 0, the body is created as Static.
func NewRigidBox(x, y, w, h, mass float32) *RigidBody {
	b := &RigidBody{
		Shape:           BoxShape(Box{X: x - float32(w*0.5), Y: y - float32(h*0.5), W: w, H: h}),
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
	inertia := float64(mass) * (float64(float64(w)*float64(w)) + float64(float64(h)*float64(h))) / 12.0
	if inertia > 0 {
		b.Inertia = float32(inertia)
		b.InvInertia = float32(1.0 / inertia)
		b.derivedInertia = b.Inertia
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
		b.derivedInertia = b.Inertia
	}
	return b
}

// NewStaticRigidCircle returns a static (infinite-mass) circular RigidBody centered at (x, y) with radius r.
func NewStaticRigidCircle(x, y, r float32) *RigidBody {
	return NewRigidCircle(x, y, r, 0)
}

// LocalToWorld transforms a local-space point relative to b's center into world coordinates.
func (b *RigidBody) LocalToWorld(local core.Point) core.Point {
	s, c := detmath.Sincos(float64(b.Angle))
	lx, ly := float64(local.X), float64(local.Y)
	return core.Point{
		X: core.Points(float64(float64(b.X)+float64(lx*c)) - float64(ly*s)),
		Y: core.Points(float64(float64(b.Y)+float64(lx*s)) + float64(ly*c)),
	}
}

// WorldToLocal transforms a world-space point into b's local coordinate frame.
func (b *RigidBody) WorldToLocal(world core.Point) core.Point {
	s, c := detmath.Sincos(float64(b.Angle))
	dx := float64(world.X) - float64(b.X)
	dy := float64(world.Y) - float64(b.Y)
	return core.Point{
		X: core.Points(float64(dx*c) + float64(dy*s)),
		Y: core.Points(float64(-dx*s) + float64(dy*c)),
	}
}

// Sleeping reports whether b rests in a sleeping island. RigidWorld neither moves nor solves a sleeping body until something wakes it: Wake, a force, torque or impulse, a change to its pose or velocity, a joint or contact linking it to an awake body, or the removal or movement of a static body it touches.
func (b *RigidBody) Sleeping() bool {
	return b != nil && b.sleeping
}

// Wake makes b take part in the next Step and restarts the time it must rest before it can sleep again. The rest of b's island wakes with it on that step.
func (b *RigidBody) Wake() {
	if b == nil {
		return
	}
	b.sleeping = false
	b.sleepTime = 0
}

// ApplyForce accumulates a world-space force applied to b's center of mass on the next Step. A static or kinematic body ignores it.
func (b *RigidBody) ApplyForce(fx, fy float32) {
	if b == nil || b.fixed() {
		return
	}
	b.Wake()
	b.forceX += float64(fx)
	b.forceY += float64(fy)
}

// ApplyTorque accumulates a torque applied to b on the next Step. A static or kinematic body ignores it.
func (b *RigidBody) ApplyTorque(torque float32) {
	if b == nil || b.fixed() {
		return
	}
	b.Wake()
	b.torque += float64(torque)
}

// ApplyImpulse immediately changes b's linear velocity by (ix, iy)*InvMass. A static or kinematic body ignores it.
func (b *RigidBody) ApplyImpulse(ix, iy float32) {
	if b == nil || b.fixed() {
		return
	}
	b.prepareMass()
	b.Wake()
	b.VX += float32(float64(ix) * float64(b.InvMass))
	b.VY += float32(float64(iy) * float64(b.InvMass))
}

// ApplyImpulseAtPoint immediately applies world-space impulse (ix, iy) at worldPoint, updating both linear and angular velocity. A static or kinematic body ignores it.
func (b *RigidBody) ApplyImpulseAtPoint(ix, iy float32, worldPoint core.Point) {
	if b == nil || b.fixed() {
		return
	}
	b.prepareMass()
	b.Wake()
	rx := float64(worldPoint.X) - float64(b.X)
	ry := float64(worldPoint.Y) - float64(b.Y)
	b.applyImpulseRaw(float64(ix), float64(iy), rx, ry)
}

func (b *RigidBody) applyImpulseRaw(ix, iy, rx, ry float64) {
	if b.fixed() {
		return
	}
	invM := float64(b.InvMass)
	invI := float64(b.InvInertia)
	b.VX = float32(float64(b.VX) + float64(ix*invM))
	b.VY = float32(float64(b.VY) + float64(iy*invM))
	cross := float64(rx*iy) - float64(ry*ix)
	b.AngularVel = float32(float64(b.AngularVel) + float64(cross*invI))
}

func (b *RigidBody) applyImpulsePairRaw(ix0, iy0, rx0, ry0, ix1, iy1, rx1, ry1 float64) {
	if b.fixed() {
		return
	}
	invM := float64(b.InvMass)
	invI := float64(b.InvInertia)
	ix := ix0 + ix1
	iy := iy0 + iy1
	b.VX = float32(float64(b.VX) + float64(ix*invM))
	b.VY = float32(float64(b.VY) + float64(iy*invM))
	cross0 := float64(rx0*iy0) - float64(ry0*ix0)
	cross1 := float64(rx1*iy1) - float64(ry1*ix1)
	b.AngularVel = float32(float64(b.AngularVel) + float64((cross0+cross1)*invI))
}

func (b *RigidBody) prepareMass() {
	if b.Shape.Kind == ShapeSegment || b.Shape.Kind == ShapeChain {
		b.Static = true
	}
	if b.Static {
		b.sleeping = false
		b.sleepTime = 0
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
	if b.Kinematic {
		b.sleeping = false
		b.sleepTime = 0
		b.InvMass = 0
		b.InvInertia = 0
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
				inertia = float64(b.Mass) * (float64(w*w) + float64(h*h)) / 12.0
			case ShapeCircle:
				r := float64(b.Shape.Circle.R)
				inertia = 0.5 * float64(b.Mass) * r * r
			default:
				inertia = float64(b.Shape.MomentOfInertia(b.Mass))
			}
			if inertia > 0 {
				b.Inertia = float32(inertia)
				b.InvInertia = float32(1.0 / inertia)
				b.derivedInertia = b.Inertia
			}
		}
	}
}

func (b *RigidBody) syncShape() {
	switch b.Shape.Kind {
	case ShapeBox:
		b.Shape.Box.X = b.X - float32(b.Shape.Box.W*0.5)
		b.Shape.Box.Y = b.Y - float32(b.Shape.Box.H*0.5)
	case ShapeCircle:
		b.Shape.Circle.X = b.X
		b.Shape.Circle.Y = b.Y
	default:
		if !b.hasLocal || b.Shape != b.synced {
			return
		}
		if b.X == b.syncX && b.Y == b.syncY && b.Angle == b.syncAngle {
			return
		}
		b.Shape = b.local.transformed(float64(b.X), float64(b.Y), float64(b.Angle))
		b.setSynced(b.Shape)
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
		sinA, cosA := detmath.Sincos(float64(b.Angle))
		c := math.Abs(cosA)
		s := math.Abs(sinA)
		ex := float64(hw*c) + float64(hh*s)
		ey := float64(hw*s) + float64(hh*c)
		return cx - ex, cy - ey, cx + ex, cy + ey
	default:
		if !b.hasLocal {
			return cx, cy, cx, cy
		}
		_, bb := b.poseShape()
		return float64(bb.X), float64(bb.Y), float64(bb.X) + float64(bb.W), float64(bb.Y) + float64(bb.H)
	}
}

// CanCollideRigid reports whether two rigid bodies are allowed to collide based on their Layer and Mask fields.
func CanCollideRigid(a, b *RigidBody) bool {
	if a == nil || b == nil {
		return false
	}
	return layersInteract(a.Layer, a.Mask, b.Layer, b.Mask)
}

// RigidWorld simulates 2D rigid bodies and joints with deterministic substepped symplectic Euler integration, a sort-and-sweep broadphase, warm-started soft-constraint sequential impulse contact resolution with friction and relaxation, island sleeping and continuous collision of fast bodies against static ones.
//
// A dynamic body that moves slower than SleepLinearThreshold and turns slower than SleepAngularThreshold for TimeToSleep seconds is ready to sleep, and the island of bodies connected to it by contacts and joints falls asleep together once every body in it is ready. A sleeping body costs no integration or solving; see RigidBody.Sleeping for what wakes it.
//
// Static, kinematic (RigidBody.Kinematic) and dynamic bodies collide with each other, and dynamic ones with Tiles; sensors (RigidBody.Sensor) only detect overlaps. After each Step, ContactEvents and SensorEvents list the contacts and overlaps that began or ended, and Raycast, RaycastFirst, QueryPoint, QueryOverlap and ShapeCast query the bodies and tiles.
type RigidWorld struct {
	Bodies           []*RigidBody
	Joints           []Joint
	Triggers         []*Trigger
	GravityX         float32
	GravityY         float32
	SolverIterations int
	// Substeps is the number of solver substeps per Step; zero or negative means DefaultSubsteps.
	Substeps int
	// EnablePositionCleanup runs an optional post-step non-linear Gauss-Seidel
	// joint positional cleanup pass after all substeps finish; off by default
	// because the substepped solver resolves joint error via soft push-out.
	EnablePositionCleanup bool
	// DisableSleep keeps every body awake. Step wakes bodies that were sleeping when it is set.
	DisableSleep bool
	// SleepLinearThreshold is the speed in pixels per second below which a body counts as resting; zero, a negative value, NaN or an infinity means DefaultSleepLinearThreshold.
	SleepLinearThreshold float32
	// SleepAngularThreshold is the angular speed in radians per second below which a body counts as resting; zero, a negative value, NaN or an infinity means DefaultSleepAngularThreshold.
	SleepAngularThreshold float32
	// TimeToSleep is how many seconds every body of an island must rest before the island sleeps; zero, a negative value, NaN or an infinity means DefaultTimeToSleep.
	TimeToSleep float32
	// OnJointBreak is invoked deterministically for each joint broken during Step, in joint insertion order.
	OnJointBreak func(JointBreakEvent)
	// OnContact is invoked at the end of Step for each of ContactEvents, in order.
	OnContact func(ContactEvent)
	// OnSensor is invoked at the end of Step for each of SensorEvents, in order.
	OnSensor func(SensorEvent)

	// Tiles is an optional tile grid whose TileSolid cells collide with the dynamic bodies from every side and whose TileOneWay cells hold them up from above only. A tile's sides that a neighbouring solid tile covers never push a body, so bodies slide and roll across the seams of a tiled floor or wall. Tiles collide by TileLayer and TileMask like a body, and with TileFriction and TileRestitution.
	Tiles TileGrid
	// TileLayer and TileMask filter which bodies collide with Tiles, as a body's Layer and Mask do (zero means LayerAll).
	TileLayer LayerMask
	TileMask  LayerMask
	// TileFriction is the friction of Tiles, combined with a body's as two bodies' are; zero means DefaultTileFriction and a negative value makes tiles frictionless.
	TileFriction float32
	// TileRestitution is the restitution of Tiles, combined with a body's as two bodies' are.
	TileRestitution float32

	// contacts holds the impulses of last step's contact points, in deterministic order, for warm starting the next step; contacts between bodies that are both asleep or static carry over unchanged.
	contacts []rigidCachedManifold
	// lastDt is the length in seconds of the step that filled contacts, which scales its impulses to the next step's length.
	lastDt float64
	// stamp counts steps, marking which bodies are in Bodies during the current one.
	stamp uint64
	// Scratch reused by Step.
	scratch          rigidScratch
	jointBreakEvents []JointBreakEvent

	// tileBody stands for Tiles in contact manifolds, the contact cache and continuous collision: a static body whose sub names the cell (rigidTileGeom.cell).
	tileBody *RigidBody
	// tileGeom is the geometry of Tiles the current contact cache was built with.
	tileGeom rigidTileGeom
	// touching lists the pairs of bodies, and bodies and the tiles, in contact after the last Step, in deterministic order; contactEvents are the changes that Step found.
	touching      []rigidTouch
	contactEvents []ContactEvent
	// sensing lists the sensors and the bodies overlapping them after the last Step, in pair order; sensorEvents are the changes that Step found.
	sensing      []rigidSense
	sensorEvents []SensorEvent

	nextBodyID    uint64
	nextTriggerID uint64
	nextJointID   uint64
}

func (w *RigidWorld) ensureBodyID(b *RigidBody) {
	if b == nil || b.ID != 0 {
		return
	}
	if b.internalID == 0 {
		w.nextBodyID++
		b.internalID = w.nextBodyID
	} else if b.internalID > w.nextBodyID {
		w.nextBodyID = b.internalID
	}
}

func (w *RigidWorld) ensureTriggerID(tr *Trigger) {
	if tr == nil || tr.ID != 0 {
		return
	}
	if tr.internalID == 0 {
		w.nextTriggerID++
		tr.internalID = w.nextTriggerID
	} else if tr.internalID > w.nextTriggerID {
		w.nextTriggerID = tr.internalID
	}
}

func (w *RigidWorld) ensureJointID(j Joint) {
	ij, ok := j.(identifiableJoint)
	if !ok {
		return
	}
	if id := ij.jointInternalID(); id == 0 {
		w.nextJointID++
		ij.setJointInternalID(w.nextJointID)
	} else if id > w.nextJointID {
		w.nextJointID = id
	}
}

// JointBreakEvents returns the deterministic slice of JointBreakEvents produced
// by the most recent RigidWorld.Step, in joint insertion order.
func (w *RigidWorld) JointBreakEvents() []JointBreakEvent {
	if w == nil {
		return nil
	}
	return w.jointBreakEvents
}

// AddBody appends non-nil rigid bodies to w.Bodies.
func (w *RigidWorld) AddBody(bodies ...*RigidBody) {
	for _, b := range bodies {
		if b != nil {
			if !isFinite32(b.GravityScale) || (b.GravityScale == 0 && !b.gravityScaleSet) {
				b.GravityScale = 1
			}
			b.gravityScaleSet = true
			w.ensureBodyID(b)
			w.Bodies = append(w.Bodies, b)
		}
	}
}

// RemoveBody deletes the first occurrence of b from w.Bodies, preserving the order of remaining bodies, and wakes any sleeping bodies that touched or were jointed to b.
func (w *RigidWorld) RemoveBody(b *RigidBody) bool {
	for i, existing := range w.Bodies {
		if existing == b {
			w.Bodies = append(w.Bodies[:i], w.Bodies[i+1:]...)
			if b != nil {
				w.wakePartners(b)
			}
			return true
		}
	}
	return false
}

// AddJoint appends non-nil joints to w.Joints and wakes any sleeping bodies they connect.
func (w *RigidWorld) AddJoint(joints ...Joint) {
	for _, j := range joints {
		if j != nil {
			w.ensureJointID(j)
			w.Joints = append(w.Joints, j)
			a, b := jointPair(j)
			a.wakeIfSleeping()
			b.wakeIfSleeping()
		}
	}
}

// RemoveJoint deletes the first occurrence of j from w.Joints, preserving the order of remaining joints, and wakes any sleeping bodies it connected.
func (w *RigidWorld) RemoveJoint(j Joint) bool {
	for i, existing := range w.Joints {
		if existing == j {
			w.Joints = append(w.Joints[:i], w.Joints[i+1:]...)
			if j != nil {
				a, b := jointPair(j)
				a.wakeIfSleeping()
				b.wakeIfSleeping()
			}
			return true
		}
	}
	return false
}

type rigidContactPoint struct {
	// id names the features in contact, which match the point to its cached impulses on the next step.
	id          uint32
	px, py      float64
	depth       float64
	rAx, rAy    float64
	rBx, rBy    float64
	massNormal  float64
	massTangent float64
	// approachVn is the pre-solve relative normal velocity (vB - vA)·n captured before substeps, used to apply restitution once after the last substep.
	approachVn float64
	pn         float64
	pt         float64
	warmPn     float64
	warmPt     float64
	pushPn     float64
	totalPn    float64
	cachePn    float64
	cachePt    float64
}

type rigidManifold struct {
	a, b *RigidBody
	// sub numbers the manifolds of one pair: the chain segment index against a Chain, 0 otherwise.
	sub         int
	nx, ny      float64 // unit normal pointing from A toward B
	friction    float64
	restitution float64
	// approach is the fastest any point of the manifold closed along the normal before this step's solve, in pixels per second, and 0 when none did.
	approach     float64
	biasRate     float64
	massScale    float64
	impulseScale float64
	count        int
	points       [2]rigidContactPoint
}

// Step advances all bodies and joints in w by dt using substepped symplectic Euler integration, deterministic warm-started soft-constraint sequential impulse solving with relaxation against other bodies and Tiles, restitution applied once after the last substep, and continuous collision for fast bodies, then puts resting islands to sleep and reports the contacts and sensor overlaps that began or ended (ContactEvents, SensorEvents).
func (w *RigidWorld) Step(dt time.Duration) {
	if w == nil || dt <= 0 {
		return
	}
	h := dt.Seconds()
	if math.IsNaN(h) || math.IsInf(h, 0) || h <= 0 {
		return
	}

	substeps := w.Substeps
	if substeps <= 0 {
		substeps = DefaultSubsteps
	}
	iters := w.SolverIterations
	if iters <= 0 {
		iters = DefaultSolverIterations
	}
	subH := h / float64(substeps)
	invSubH := float64(substeps) / h
	velIters := max(1, (iters+substeps-1)/substeps)
	pushIters := 2
	relaxIters := 1
	jointIters := velIters

	// 1. Prepare body mass/inertia, then wake the islands that were disturbed since the last step.
	w.stamp++
	for i, b := range w.Bodies {
		if b == nil {
			continue
		}
		b.stamp = w.stamp
		b.index = i
		b.ensureLocal()
		b.prepareMass()
		b.driveKinematic(h)
	}
	w.prepareTiles()
	activeAreas := collectActiveTriggers(w.Triggers)
	w.wakeDisturbed(activeAreas)

	// Record start poses and reset per-body substep deltas (used to update contact separations without re-running collision detection).
	starts := w.scratch.starts[:0]
	deltas := w.scratch.deltas[:0]
	for _, b := range w.Bodies {
		if b == nil {
			starts = append(starts, rigidPose{})
		} else {
			starts = append(starts, rigidPose{x: b.X, y: b.Y, angle: b.Angle})
		}
		deltas = append(deltas, rigidBodyDelta{cosDA: 1})
	}
	w.scratch.starts = starts
	w.scratch.deltas = deltas

	w.jointBreakEvents = w.jointBreakEvents[:0]
	ignoredPairs := w.buildJointIgnoredPairs()

	// 2. Find candidate pairs with the broadphase once per step, wake sleeping bodies an awake one touches, and detect contact manifolds in deterministic pair order.
	gx, gy := float64(w.GravityX), float64(w.GravityY)
	restitutionThresh := max(1.0, 2.0*detmath.Hypot(gx, gy)*h)
	pairs := w.broadphase()
	w.wakeTouched(pairs)
	manifolds := w.detectContacts(pairs, h, subH, ignoredPairs)
	w.updateContactEvents(manifolds, ignoredPairs)

	// 3. Collect awake joints, build the spanning-forest anchor tree and pinched high-mass-ratio contact pairs, and reset full-step reaction accumulators once before substeps.
	joints := w.awakeJoints()
	for _, j := range joints {
		if rj, ok := j.(stepReactionJoint); ok {
			rj.resetStepReaction(h)
		}
	}
	w.scratch.jointTree.build(w, joints)
	pinchPairs := w.findPinchPairs(manifolds)

	// 4. Solve in substeps: integrate velocities, warm-start contacts, solve velocity constraints without penetration bias, solve soft contact and joint push-out into pseudo-velocities, integrate positions, then relax velocities without bias.
	for sub := range substeps {
		w.integrateSubstepVelocities(activeAreas, gx, gy, subH)
		if sub == 0 {
			w.warmStart(manifolds, subH)
			for mi := range manifolds {
				m := &manifolds[mi]
				for k := range m.count {
					m.points[k].warmPn = m.points[k].pn
					m.points[k].warmPt = m.points[k].pt
				}
			}
		} else {
			for mi := range manifolds {
				applyManifoldWarmStart(&manifolds[mi], deltas)
			}
		}
		for _, j := range joints {
			j.preStep(subH)
		}
		for iter := range velIters {
			if (sub+iter)&1 == 0 {
				for mi := range manifolds {
					solveManifoldVelocitySubstep(&manifolds[mi], deltas, invSubH)
				}
			} else {
				for mi := len(manifolds) - 1; mi >= 0; mi-- {
					solveManifoldVelocitySubstep(&manifolds[mi], deltas, invSubH)
				}
			}
			for _, pp := range pinchPairs {
				solvePinchPairVelocity(&manifolds[pp[0]], &manifolds[pp[1]], w.Bodies[pp[2]], deltas, invSubH)
			}
			if iter < jointIters {
				w.scratch.jointTree.solve(w, deltas, jointTreeVelocity, subH)
				if (sub+iter)&1 == 0 {
					for ji, j := range joints {
						skipAnchor := w.scratch.jointTree.inTree[ji]
						if sj, ok := j.(substepJoint2D); ok {
							sj.solveVelocitySubstep(skipAnchor)
						} else {
							j.solveVelocity()
						}
					}
				} else {
					for ji := len(joints) - 1; ji >= 0; ji-- {
						j := joints[ji]
						skipAnchor := w.scratch.jointTree.inTree[ji]
						if sj, ok := j.(substepJoint2D); ok {
							sj.solveVelocitySubstep(skipAnchor)
						} else {
							j.solveVelocity()
						}
					}
				}
			}
		}
		for iter := velIters; iter < jointIters; iter++ {
			w.scratch.jointTree.solve(w, deltas, jointTreeVelocity, subH)
			for ji, j := range joints {
				skipAnchor := w.scratch.jointTree.inTree[ji]
				if sj, ok := j.(substepJoint2D); ok {
					sj.solveVelocitySubstep(skipAnchor)
				} else {
					j.solveVelocity()
				}
			}
		}

		// Reset push-out pseudo-velocities and push impulses for this substep, then solve soft contact and rigid joint push-out (linear + angular, zero velocity injection).
		for i := range deltas {
			deltas[i].pvx = 0
			deltas[i].pvy = 0
			deltas[i].pav = 0
		}
		for mi := range manifolds {
			m := &manifolds[mi]
			m.points[0].pushPn = 0
			m.points[1].pushPn = 0
		}
		for iter := range pushIters {
			if (sub+iter)&1 == 0 {
				for mi := range manifolds {
					solveManifoldPushOut(&manifolds[mi], deltas)
				}
			} else {
				for mi := len(manifolds) - 1; mi >= 0; mi-- {
					solveManifoldPushOut(&manifolds[mi], deltas)
				}
			}
			for _, pp := range pinchPairs {
				solvePinchPairPushOut(&manifolds[pp[0]], &manifolds[pp[1]], w.Bodies[pp[2]], deltas)
			}
			w.scratch.jointTree.solve(w, deltas, jointTreePushOut, subH)
			for ji, j := range joints {
				skipAnchor := w.scratch.jointTree.inTree[ji]
				if sj, ok := j.(substepJoint2D); ok {
					sj.solvePushOut(deltas, skipAnchor)
				}
			}
		}

		// Integrate positions for this substep using velocity + soft push-out pseudo-velocity, and update per-body deltas from start poses.
		lastSub := sub == substeps-1
		for i, b := range w.Bodies {
			if b == nil || b.Static || b.sleeping {
				continue
			}
			if b.Kinematic && b.targetPending && lastSub {
				// A target lands the body exactly on its pose, which integrating the velocity that leads there may miss by rounding.
				b.X, b.Y, b.Angle = b.target.x, b.target.y, b.target.angle
				b.targetPending = false
			} else {
				d := &deltas[i]
				b.X = float32(float64(b.X) + float64((float64(b.VX)+d.pvx)*subH))
				b.Y = float32(float64(b.Y) + float64((float64(b.VY)+d.pvy)*subH))
				b.Angle = float32(float64(b.Angle) + float64((float64(b.AngularVel)+d.pav)*subH))
			}
			updateBodyDelta(&deltas[i], b, starts[i])
		}

		// Relaxation iterations without bias in alternating manifold order so velocities stay consistent with the updated poses and stacks/chains settle cleanly.
		for iter := range relaxIters {
			if (sub+iter)&1 == 0 {
				for mi := len(manifolds) - 1; mi >= 0; mi-- {
					solveManifoldVelocitySubstep(&manifolds[mi], deltas, invSubH)
				}
			} else {
				for mi := range manifolds {
					solveManifoldVelocitySubstep(&manifolds[mi], deltas, invSubH)
				}
			}
			for _, pp := range pinchPairs {
				solvePinchPairVelocity(&manifolds[pp[0]], &manifolds[pp[1]], w.Bodies[pp[2]], deltas, invSubH)
			}
			w.scratch.jointTree.solve(w, deltas, jointTreeRelax, subH)
			for ji, j := range joints {
				skipAnchor := w.scratch.jointTree.inTree[ji]
				if sj, ok := j.(substepJoint2D); ok {
					sj.solveRelax(skipAnchor)
				}
			}
		}

		for mi := range manifolds {
			m := &manifolds[mi]
			for k := range m.count {
				m.points[k].totalPn += m.points[k].pn
				if sub > 0 {
					m.points[k].warmPn = m.points[k].pn
					m.points[k].warmPt = m.points[k].pt
				}
			}
		}
	}

	// Clear accumulated external forces once after all substeps have integrated them.
	for _, b := range w.Bodies {
		if b == nil || b.fixed() || b.sleeping {
			continue
		}
		b.forceX = 0
		b.forceY = 0
		b.torque = 0
	}

	// Scale the final substep's relaxed impulses back to full-step length h for the contact cache.
	subScale := float64(substeps)
	for mi := range manifolds {
		m := &manifolds[mi]
		for k := range m.count {
			m.points[k].cachePn = m.points[k].pn * subScale
			m.points[k].cachePt = m.points[k].pt * subScale
		}
	}

	// 5. Store warm-start impulses (before restitution), then apply restitution once after the last substep.
	w.storeContacts(manifolds, h)
	for mi := range manifolds {
		applyManifoldRestitution(&manifolds[mi], deltas, restitutionThresh)
	}

	// 6. Evaluate break thresholds and remove broken joints deterministically.
	if w.evaluateBreakableJoints() {
		ignoredPairs = w.buildJointIgnoredPairs()
		joints = w.awakeJoints()
	}

	if w.EnablePositionCleanup {
		for range 4 {
			for _, j := range w.Joints {
				if j != nil {
					j.solvePosition()
				}
			}
		}
	}

	// 7. Sweep fast bodies against static ones, and wake any sleeping body overlapped by a moved body.
	w.sweepFastBodies(starts, manifolds, restitutionThresh, ignoredPairs)
	w.wakePostStep(starts, ignoredPairs)

	// 8. Synchronize Shape geometry to updated centers.
	for _, b := range w.Bodies {
		if b != nil {
			b.syncShape()
		}
	}

	// 9. Put islands that have rested long enough to sleep, and remember each pose, so a body moved by hand before the next step is noticed.
	w.updateSleep(manifolds, joints, activeAreas, h)
	for _, b := range w.Bodies {
		if b != nil {
			b.restX, b.restY, b.restAngle = b.X, b.Y, b.Angle
			b.restValid = true
		}
	}

	// 10. Find the overlaps of sensors at the bodies' final poses.
	w.updateSensorEvents()

	// 11. Dispatch deterministic joint-break, contact and sensor callbacks.
	if w.OnJointBreak != nil {
		for _, ev := range w.jointBreakEvents {
			w.OnJointBreak(ev)
		}
	}
	if w.OnContact != nil {
		for _, ev := range w.contactEvents {
			w.OnContact(ev)
		}
	}
	if w.OnSensor != nil {
		for _, ev := range w.sensorEvents {
			w.OnSensor(ev)
		}
	}
}

func updateBodyDelta(d *rigidBodyDelta, b *RigidBody, start rigidPose) {
	d.dx = float64(b.X) - float64(start.x)
	d.dy = float64(b.Y) - float64(start.y)
	da := float64(b.Angle) - float64(start.angle)
	if da == 0 || b.Shape.Kind == ShapeCircle {
		d.sinDA = 0
		d.cosDA = 1
	} else {
		d.sinDA, d.cosDA = detmath.Sincos(da)
	}
}

func (w *RigidWorld) integrateSubstepVelocities(activeAreas []activeAreaEntry, gx, gy, subH float64) {
	for _, b := range w.Bodies {
		if b == nil || b.fixed() || b.sleeping {
			continue
		}
		invM := float64(b.InvMass)
		invI := float64(b.InvInertia)
		gScale := float64(b.EffectiveGravityScale())

		var vx, vy, av float64
		if len(activeAreas) == 0 {
			ax := float64(gx*gScale) + float64(b.forceX*invM)
			ay := float64(gy*gScale) + float64(b.forceY*invM)
			vx = float64(b.VX) + float64(ax*subH)
			vy = float64(b.VY) + float64(ay*subH)
			av = float64(b.AngularVel) + float64(float64(b.torque*invI)*subH)

			if b.LinearDamping > 0 {
				factor := 1.0 / (1.0 + float64(float64(b.LinearDamping)*subH))
				vx *= factor
				vy *= factor
			}
			if b.AngularDamping > 0 {
				factor := 1.0 / (1.0 + float64(float64(b.AngularDamping)*subH))
				av *= factor
			}
		} else {
			eff := w.evaluateRigidBodyAreas(b, activeAreas)
			effGX := float64((float64(eff.GravityX) + float64(eff.BuoyancyAccelX)) * gScale)
			effGY := float64((float64(eff.GravityY) + float64(eff.BuoyancyAccelY)) * gScale)
			ax := effGX + float64(b.forceX*invM)
			ay := effGY + float64(b.forceY*invM)
			vx = float64(b.VX) + float64(ax*subH)
			vy = float64(b.VY) + float64(ay*subH)
			av = float64(b.AngularVel) + float64(float64(b.torque*invI)*subH)

			if eff.FluidLinearDrag > 0 {
				fvx, fvy := float64(eff.FluidVelX), float64(eff.FluidVelY)
				factor := 1.0 / (1.0 + float64(float64(eff.FluidLinearDrag)*subH))
				vx = fvx + float64((vx-fvx)*factor)
				vy = fvy + float64((vy-fvy)*factor)
			}
			if eff.LinearDamping > 0 {
				factor := 1.0 / (1.0 + float64(float64(eff.LinearDamping)*subH))
				vx *= factor
				vy *= factor
			}
			angDamp := float64(eff.AngularDamping) + float64(eff.FluidAngularDrag)
			if angDamp > 0 {
				factor := 1.0 / (1.0 + float64(angDamp*subH))
				av *= factor
			}
		}

		b.VX = float32(vx)
		b.VY = float32(vy)
		b.AngularVel = float32(av)
	}
}

func (w *RigidWorld) buildJointIgnoredPairs() map[[2]*RigidBody]struct{} {
	if len(w.Joints) == 0 {
		return nil
	}
	pairs := w.scratch.jointIgnored
	if pairs != nil {
		clear(pairs)
	}
	hasAny := false
	for _, j := range w.Joints {
		if j == nil {
			continue
		}
		if cc, ok := j.(collideConnectedJoint); ok && cc.collideConnected() {
			continue
		}
		jb, ok := j.(jointBodies)
		if !ok {
			continue
		}
		a, b := jb.jointBodyPair()
		if a == nil || b == nil || a == b {
			continue
		}
		if pairs == nil {
			pairs = make(map[[2]*RigidBody]struct{}, len(w.Joints)*2)
			w.scratch.jointIgnored = pairs
		}
		pairs[[2]*RigidBody{a, b}] = struct{}{}
		pairs[[2]*RigidBody{b, a}] = struct{}{}
		hasAny = true
	}
	if !hasAny {
		return nil
	}
	return pairs
}

func (w *RigidWorld) evaluateBreakableJoints() bool {
	if len(w.Joints) == 0 {
		return false
	}
	oldLen := len(w.Joints)
	dst := w.Joints[:0]
	brokeAny := false
	for _, j := range w.Joints {
		if bj, ok := j.(breakableJoint); ok {
			if ev, broken := bj.checkBreak(); broken {
				w.jointBreakEvents = append(w.jointBreakEvents, ev)
				// Removing the last support of a sleeping island must let its
				// bodies fall instead of freezing them at the broken pose.
				ev.BodyA.Wake()
				ev.BodyB.Wake()
				brokeAny = true
				continue
			}
		}
		dst = append(dst, j)
	}
	clear(w.Joints[len(dst):oldLen])
	w.Joints = dst
	return brokeAny
}

// detectContacts returns the contact manifolds of the candidate pairs in which at least one body is awake and dynamic, neither is a sensor and no joint in ignoredPairs keeps them apart, in pair order, followed by those of each awake dynamic body with Tiles in body order, all prepared for solving.
func (w *RigidWorld) detectContacts(pairs []uint64, h, subH float64, ignoredPairs map[[2]*RigidBody]struct{}) []rigidManifold {
	manifolds := w.scratch.manifolds[:0]
	for _, key := range pairs {
		a, b := w.Bodies[int(key>>32)], w.Bodies[int(uint32(key))]
		if (!a.awakeDynamic() && !b.awakeDynamic()) || a.Sensor || b.Sensor {
			continue
		}
		if ignoredPairs != nil {
			if _, skip := ignoredPairs[[2]*RigidBody{a, b}]; skip {
				continue
			}
		}
		first := len(manifolds)
		manifolds = collideRigidPair(a, b, manifolds)
		for k := first; k < len(manifolds); k++ {
			prepareManifold(&manifolds[k], h, subH)
		}
	}
	manifolds = w.detectTileContacts(manifolds, h, subH)
	w.scratch.manifolds = manifolds
	return manifolds
}

// makeContactSoftness returns the implicit-Euler soft-constraint parameters (biasRate, massScale, impulseScale) for natural frequency hertz, damping ratio zeta and substep subH.
func makeContactSoftness(hertz, zeta, subH float64) (biasRate, massScale, impulseScale float64) {
	if hertz <= 0 || subH <= 0 {
		return 0, 1, 0
	}
	omega := float64(float64(2.0*math.Pi) * hertz)
	hOmega := float64(subH * omega)
	a1 := float64(2.0*zeta) + hOmega
	a2 := float64(hOmega * a1)
	a3 := 1.0 / (1.0 + a2)
	return omega / a1, a2 * a3, a3
}

func prepareManifold(m *rigidManifold, h, subH float64) {
	a, b := m.a, m.b
	m.friction = math.Sqrt(max(0, float64(a.Friction)) * max(0, float64(b.Friction)))
	m.restitution = max(0, max(float64(a.Restitution), float64(b.Restitution)))
	if subH > 0 {
		hertz := contactSubstepHertzFraction / subH
		m.biasRate, m.massScale, m.impulseScale = makeContactSoftness(hertz, contactDampingRatio, subH)
	} else {
		m.biasRate, m.massScale, m.impulseScale = 0, 1, 0
	}

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

		rnA := float64(cp.rAx*ny) - float64(cp.rAy*nx)
		rnB := float64(cp.rBx*ny) - float64(cp.rBy*nx)
		kNormal := float64(invMA+invMB) + float64(float64(invIA*rnA)*rnA) + float64(float64(invIB*rnB)*rnB)
		if kNormal > 0 {
			cp.massNormal = 1.0 / kNormal
		}

		rtA := float64(cp.rAx*ty) - float64(cp.rAy*tx)
		rtB := float64(cp.rBx*ty) - float64(cp.rBy*tx)
		kTangent := float64(invMA+invMB) + float64(float64(invIA*rtA)*rtA) + float64(float64(invIB*rtB)*rtB)
		if kTangent > 0 {
			cp.massTangent = 1.0 / kTangent
		}

		// Relative velocity of B with respect to A at contact point before solving.
		vAx := float64(a.VX) - float64(float64(a.AngularVel)*cp.rAy)
		vAy := float64(a.VY) + float64(float64(a.AngularVel)*cp.rAx)
		vBx := float64(b.VX) - float64(float64(b.AngularVel)*cp.rBy)
		vBy := float64(b.VY) + float64(float64(b.AngularVel)*cp.rBx)
		vn := float64((vBx-vAx)*nx) + float64((vBy-vAy)*ny)
		cp.approachVn = vn
		m.approach = max(m.approach, -vn)
	}
}

// applyManifoldWarmStart re-applies the current per-substep impulses of m at the bodies' updated lever arms for substeps after the first.
func applyManifoldWarmStart(m *rigidManifold, deltas []rigidBodyDelta) {
	a, b := m.a, m.b
	nx, ny := m.nx, m.ny
	tx, ty := -ny, nx

	var dA, dB rigidBodyDelta
	if a.Static {
		dA.cosDA = 1
	} else {
		dA = deltas[a.index]
	}
	if b.Static {
		dB.cosDA = 1
	} else {
		dB = deltas[b.index]
	}
	rotA := dA.sinDA != 0 || dA.cosDA != 1
	rotB := dB.sinDA != 0 || dB.cosDA != 1

	if m.count == 2 {
		cp0, cp1 := &m.points[0], &m.points[1]
		cp0.pn, cp0.pt = cp0.warmPn, cp0.warmPt
		cp1.pn, cp1.pt = cp1.warmPn, cp1.warmPt
		if cp0.pn != 0 || cp0.pt != 0 || cp1.pn != 0 || cp1.pt != 0 {
			rAx0, rAy0 := cp0.rAx, cp0.rAy
			rAx1, rAy1 := cp1.rAx, cp1.rAy
			if rotA {
				rAx0 = float64(cp0.rAx*dA.cosDA) - float64(cp0.rAy*dA.sinDA)
				rAy0 = float64(cp0.rAx*dA.sinDA) + float64(cp0.rAy*dA.cosDA)
				rAx1 = float64(cp1.rAx*dA.cosDA) - float64(cp1.rAy*dA.sinDA)
				rAy1 = float64(cp1.rAx*dA.sinDA) + float64(cp1.rAy*dA.cosDA)
			}
			rBx0, rBy0 := cp0.rBx, cp0.rBy
			rBx1, rBy1 := cp1.rBx, cp1.rBy
			if rotB {
				rBx0 = float64(cp0.rBx*dB.cosDA) - float64(cp0.rBy*dB.sinDA)
				rBy0 = float64(cp0.rBx*dB.sinDA) + float64(cp0.rBy*dB.cosDA)
				rBx1 = float64(cp1.rBx*dB.cosDA) - float64(cp1.rBy*dB.sinDA)
				rBy1 = float64(cp1.rBx*dB.sinDA) + float64(cp1.rBy*dB.cosDA)
			}
			px0 := float64(cp0.pn*nx) + float64(cp0.pt*tx)
			py0 := float64(cp0.pn*ny) + float64(cp0.pt*ty)
			px1 := float64(cp1.pn*nx) + float64(cp1.pt*tx)
			py1 := float64(cp1.pn*ny) + float64(cp1.pt*ty)
			a.applyImpulsePairRaw(-px0, -py0, rAx0, rAy0, -px1, -py1, rAx1, rAy1)
			b.applyImpulsePairRaw(px0, py0, rBx0, rBy0, px1, py1, rBx1, rBy1)
		}
		return
	}
	for i := range m.count {
		cp := &m.points[i]
		cp.pn = cp.warmPn
		cp.pt = cp.warmPt
		if cp.pn == 0 && cp.pt == 0 {
			continue
		}
		rAx, rAy := cp.rAx, cp.rAy
		if rotA {
			rAx = float64(cp.rAx*dA.cosDA) - float64(cp.rAy*dA.sinDA)
			rAy = float64(cp.rAx*dA.sinDA) + float64(cp.rAy*dA.cosDA)
		}
		rBx, rBy := cp.rBx, cp.rBy
		if rotB {
			rBx = float64(cp.rBx*dB.cosDA) - float64(cp.rBy*dB.sinDA)
			rBy = float64(cp.rBx*dB.sinDA) + float64(cp.rBy*dB.cosDA)
		}
		px := float64(cp.pn*nx) + float64(cp.pt*tx)
		py := float64(cp.pn*ny) + float64(cp.pt*ty)
		a.applyImpulseRaw(-px, -py, rAx, rAy)
		b.applyImpulseRaw(px, py, rBx, rBy)
	}
}

// findPinchPairs finds dynamic bodies pinched between two opposing contacts whose partner inverse masses sum to less than half the body's own inverse mass (high mass ratio sandwiches such as 100:1 or 1000:1 on a static or heavy support). Solving each pair as a 2-manifold block eliminates the ill-conditioned Gauss-Seidel spectral radius in one pass without modifying masses or freezing bodies.
func (w *RigidWorld) findPinchPairs(manifolds []rigidManifold) [][3]int {
	pairs := w.scratch.pinchPairs[:0]
	if len(manifolds) < 2 {
		w.scratch.pinchPairs = pairs
		return pairs
	}
	n := len(w.Bodies)
	best := w.scratch.pinchBest[:0]
	second := w.scratch.pinchSecond[:0]
	for range n {
		best = append(best, -1)
		second = append(second, -1)
	}
	otherInvM := func(m *rigidManifold, body *RigidBody) float64 {
		if m.a == body {
			return float64(m.b.InvMass)
		}
		return float64(m.a.InvMass)
	}
	considerBest := func(body *RigidBody, mi int, invMO float64) {
		if body.fixed() || body.sleeping {
			return
		}
		bi := body.index
		prev := best[bi]
		if prev < 0 || invMO < otherInvM(&manifolds[prev], body) {
			best[bi] = mi
		}
	}
	for mi := range manifolds {
		m := &manifolds[mi]
		if m.count == 0 {
			continue
		}
		considerBest(m.a, mi, float64(m.b.InvMass))
		considerBest(m.b, mi, float64(m.a.InvMass))
	}
	normalOn := func(m *rigidManifold, body *RigidBody) (float64, float64) {
		if m.b == body {
			return m.nx, m.ny
		}
		return -m.nx, -m.ny
	}
	considerSecond := func(body *RigidBody, mi int, invMO float64) {
		if body.fixed() || body.sleeping {
			return
		}
		bi := body.index
		b0 := best[bi]
		if b0 < 0 || b0 == mi {
			return
		}
		n0x, n0y := normalOn(&manifolds[b0], body)
		n1x, n1y := normalOn(&manifolds[mi], body)
		if float64(n0x*n1x)+float64(n0y*n1y) > -0.5 {
			return
		}
		prev := second[bi]
		if prev < 0 || invMO < otherInvM(&manifolds[prev], body) {
			second[bi] = mi
		}
	}
	for mi := range manifolds {
		m := &manifolds[mi]
		if m.count == 0 {
			continue
		}
		considerSecond(m.a, mi, float64(m.b.InvMass))
		considerSecond(m.b, mi, float64(m.a.InvMass))
	}
	for bi, b := range w.Bodies {
		if b == nil || b.fixed() || b.sleeping {
			continue
		}
		m0, m1 := best[bi], second[bi]
		if m0 < 0 || m1 < 0 {
			continue
		}
		invM0 := otherInvM(&manifolds[m0], b)
		invM1 := otherInvM(&manifolds[m1], b)
		if invM0+invM1 > 0 && invM0+invM1 < float64(0.5*float64(b.InvMass)) {
			pairs = append(pairs, [3]int{m0, m1, bi})
		}
	}
	w.scratch.pinchBest = best
	w.scratch.pinchSecond = second
	w.scratch.pinchPairs = pairs
	return pairs
}

func bodyDeltaFor(b *RigidBody, deltas []rigidBodyDelta) *rigidBodyDelta {
	if b.Static {
		return nil
	}
	return &deltas[b.index]
}

func manifoldAvgArmsAndSep(m *rigidManifold, deltas []rigidBodyDelta) (rAx, rAy, rBx, rBy, sep float64) {
	dA := bodyDeltaFor(m.a, deltas)
	dB := bodyDeltaFor(m.b, deltas)
	rotA := dA != nil && (dA.sinDA != 0 || dA.cosDA != 1)
	rotB := dB != nil && (dB.sinDA != 0 || dB.cosDA != 1)
	var dAx, dAy, dBx, dBy float64
	if dA != nil {
		dAx, dAy = dA.dx, dA.dy
	}
	if dB != nil {
		dBx, dBy = dB.dx, dB.dy
	}
	invCount := 1.0 / float64(m.count)
	for i := range m.count {
		cp := &m.points[i]
		ax, ay := cp.rAx, cp.rAy
		if rotA {
			ax = float64(cp.rAx*dA.cosDA) - float64(cp.rAy*dA.sinDA)
			ay = float64(cp.rAx*dA.sinDA) + float64(cp.rAy*dA.cosDA)
		}
		bx, by := cp.rBx, cp.rBy
		if rotB {
			bx = float64(cp.rBx*dB.cosDA) - float64(cp.rBy*dB.sinDA)
			by = float64(cp.rBx*dB.sinDA) + float64(cp.rBy*dB.cosDA)
		}
		rAx += ax
		rAy += ay
		rBx += bx
		rBy += by
		dpX := (dBx + (bx - cp.rBx)) - (dAx + (ax - cp.rAx))
		dpY := (dBy + (by - cp.rBy)) - (dAy + (ay - cp.rAy))
		sep += -cp.depth + float64(dpX*m.nx) + float64(dpY*m.ny)
	}
	return rAx * invCount, rAy * invCount, rBx * invCount, rBy * invCount, sep * invCount
}

func distributeManifoldNormalImpulse(m *rigidManifold, deltas []rigidBodyDelta, newTotal float64) {
	oldTotal := m.points[0].pn
	if m.count == 2 {
		oldTotal += m.points[1].pn
	}
	dTotal := newTotal - oldTotal
	if dTotal == 0 {
		return
	}
	dA := bodyDeltaFor(m.a, deltas)
	dB := bodyDeltaFor(m.b, deltas)
	rotA := dA != nil && (dA.sinDA != 0 || dA.cosDA != 1)
	rotB := dB != nil && (dB.sinDA != 0 || dB.cosDA != 1)
	var dP [2]float64
	if m.count == 1 {
		dP[0] = dTotal
		m.points[0].pn = newTotal
	} else if dTotal >= 0 || oldTotal <= 0 {
		half := float64(dTotal * 0.5)
		dP[0], dP[1] = half, half
		m.points[0].pn = max(0, m.points[0].pn+half)
		m.points[1].pn = max(0, m.points[1].pn+half)
	} else {
		scale := newTotal / oldTotal
		n0 := float64(m.points[0].pn * scale)
		n1 := float64(m.points[1].pn * scale)
		dP[0] = n0 - m.points[0].pn
		dP[1] = n1 - m.points[1].pn
		m.points[0].pn = n0
		m.points[1].pn = n1
	}
	if m.count == 2 {
		cp0, cp1 := &m.points[0], &m.points[1]
		rAx0, rAy0 := cp0.rAx, cp0.rAy
		rAx1, rAy1 := cp1.rAx, cp1.rAy
		if rotA {
			rAx0 = float64(cp0.rAx*dA.cosDA) - float64(cp0.rAy*dA.sinDA)
			rAy0 = float64(cp0.rAx*dA.sinDA) + float64(cp0.rAy*dA.cosDA)
			rAx1 = float64(cp1.rAx*dA.cosDA) - float64(cp1.rAy*dA.sinDA)
			rAy1 = float64(cp1.rAx*dA.sinDA) + float64(cp1.rAy*dA.cosDA)
		}
		rBx0, rBy0 := cp0.rBx, cp0.rBy
		rBx1, rBy1 := cp1.rBx, cp1.rBy
		if rotB {
			rBx0 = float64(cp0.rBx*dB.cosDA) - float64(cp0.rBy*dB.sinDA)
			rBy0 = float64(cp0.rBx*dB.sinDA) + float64(cp0.rBy*dB.cosDA)
			rBx1 = float64(cp1.rBx*dB.cosDA) - float64(cp1.rBy*dB.sinDA)
			rBy1 = float64(cp1.rBx*dB.sinDA) + float64(cp1.rBy*dB.cosDA)
		}
		px0, py0 := dP[0]*m.nx, dP[0]*m.ny
		px1, py1 := dP[1]*m.nx, dP[1]*m.ny
		m.a.applyImpulsePairRaw(-px0, -py0, rAx0, rAy0, -px1, -py1, rAx1, rAy1)
		m.b.applyImpulsePairRaw(px0, py0, rBx0, rBy0, px1, py1, rBx1, rBy1)
		return
	}
	if dP[0] != 0 {
		cp := &m.points[0]
		rAx, rAy := cp.rAx, cp.rAy
		if rotA {
			rAx = float64(cp.rAx*dA.cosDA) - float64(cp.rAy*dA.sinDA)
			rAy = float64(cp.rAx*dA.sinDA) + float64(cp.rAy*dA.cosDA)
		}
		rBx, rBy := cp.rBx, cp.rBy
		if rotB {
			rBx = float64(cp.rBx*dB.cosDA) - float64(cp.rBy*dB.sinDA)
			rBy = float64(cp.rBx*dB.sinDA) + float64(cp.rBy*dB.cosDA)
		}
		px, py := dP[0]*m.nx, dP[0]*m.ny
		m.a.applyImpulseRaw(-px, -py, rAx, rAy)
		m.b.applyImpulseRaw(px, py, rBx, rBy)
	}
}

func distributeManifoldPushImpulse(m *rigidManifold, deltas []rigidBodyDelta, newTotal float64) {
	oldTotal := m.points[0].pushPn
	if m.count == 2 {
		oldTotal += m.points[1].pushPn
	}
	dTotal := newTotal - oldTotal
	if dTotal == 0 {
		return
	}
	dA := bodyDeltaFor(m.a, deltas)
	dB := bodyDeltaFor(m.b, deltas)
	rotA := dA != nil && (dA.sinDA != 0 || dA.cosDA != 1)
	rotB := dB != nil && (dB.sinDA != 0 || dB.cosDA != 1)
	var dP [2]float64
	if m.count == 1 {
		dP[0] = dTotal
		m.points[0].pushPn = newTotal
	} else if dTotal >= 0 || oldTotal <= 0 {
		half := float64(dTotal * 0.5)
		dP[0], dP[1] = half, half
		m.points[0].pushPn = max(0, m.points[0].pushPn+half)
		m.points[1].pushPn = max(0, m.points[1].pushPn+half)
	} else {
		scale := newTotal / oldTotal
		n0 := float64(m.points[0].pushPn * scale)
		n1 := float64(m.points[1].pushPn * scale)
		dP[0] = n0 - m.points[0].pushPn
		dP[1] = n1 - m.points[1].pushPn
		m.points[0].pushPn = n0
		m.points[1].pushPn = n1
	}
	applyPush := func(body *RigidBody, d *rigidBodyDelta, ix, iy, rx, ry float64) {
		if body.fixed() || d == nil {
			return
		}
		invM := float64(body.InvMass)
		invI := float64(body.InvInertia)
		d.pvx += float64(ix * invM)
		d.pvy += float64(iy * invM)
		cross := float64(rx*iy) - float64(ry*ix)
		d.pav += float64(cross * invI)
	}
	for i := range m.count {
		if dP[i] == 0 {
			continue
		}
		cp := &m.points[i]
		rAx, rAy := cp.rAx, cp.rAy
		if rotA {
			rAx = float64(cp.rAx*dA.cosDA) - float64(cp.rAy*dA.sinDA)
			rAy = float64(cp.rAx*dA.sinDA) + float64(cp.rAy*dA.cosDA)
		}
		rBx, rBy := cp.rBx, cp.rBy
		if rotB {
			rBx = float64(cp.rBx*dB.cosDA) - float64(cp.rBy*dB.sinDA)
			rBy = float64(cp.rBx*dB.sinDA) + float64(cp.rBy*dB.cosDA)
		}
		px, py := dP[i]*m.nx, dP[i]*m.ny
		applyPush(m.a, dA, -px, -py, rAx, rAy)
		applyPush(m.b, dB, px, py, rBx, rBy)
	}
}

func pinchPairMatrix(m0, m1 *rigidManifold, shared *RigidBody, deltas []rigidBodyDelta) (k00, k01, k11, det float64, rA0x, rA0y, rB0x, rB0y, sep0, rA1x, rA1y, rB1x, rB1y, sep1 float64, ok bool) {
	rA0x, rA0y, rB0x, rB0y, sep0 = manifoldAvgArmsAndSep(m0, deltas)
	rA1x, rA1y, rB1x, rB1y, sep1 = manifoldAvgArmsAndSep(m1, deltas)

	rnA0 := float64(rA0x*m0.ny) - float64(rA0y*m0.nx)
	rnB0 := float64(rB0x*m0.ny) - float64(rB0y*m0.nx)
	rnA1 := float64(rA1x*m1.ny) - float64(rA1y*m1.nx)
	rnB1 := float64(rB1x*m1.ny) - float64(rB1y*m1.nx)

	k00 = float64(m0.a.InvMass) + float64(m0.b.InvMass) + float64(float64(float64(m0.a.InvInertia)*rnA0)*rnA0) + float64(float64(float64(m0.b.InvInertia)*rnB0)*rnB0)
	k11 = float64(m1.a.InvMass) + float64(m1.b.InvMass) + float64(float64(float64(m1.a.InvInertia)*rnA1)*rnA1) + float64(float64(float64(m1.b.InvInertia)*rnB1)*rnB1)

	s0, rL0n := -1.0, rnA0
	if m0.b == shared {
		s0, rL0n = 1.0, rnB0
	}
	s1, rL1n := -1.0, rnA1
	if m1.b == shared {
		s1, rL1n = 1.0, rnB1
	}
	sign := s0 * s1
	nDot := float64(m0.nx*m1.nx) + float64(m0.ny*m1.ny)
	k01 = sign * (float64(float64(shared.InvMass)*nDot) + float64(float64(float64(shared.InvInertia)*rL0n)*rL1n))
	det = float64(k00*k11) - float64(k01*k01)
	ok = det > 1e-12
	return
}

func solvePinchPairVelocity(m0, m1 *rigidManifold, shared *RigidBody, deltas []rigidBodyDelta, invSubH float64) {
	k00, k01, k11, det, rA0x, rA0y, rB0x, rB0y, sep0, rA1x, rA1y, rB1x, rB1y, sep1, ok := pinchPairMatrix(m0, m1, shared, deltas)
	if !ok {
		return
	}
	relVn := func(m *rigidManifold, rAx, rAy, rBx, rBy float64) float64 {
		vAx := float64(m.a.VX) - float64(float64(m.a.AngularVel)*rAy)
		vAy := float64(m.a.VY) + float64(float64(m.a.AngularVel)*rAx)
		vBx := float64(m.b.VX) - float64(float64(m.b.AngularVel)*rBy)
		vBy := float64(m.b.VY) + float64(float64(m.b.AngularVel)*rBx)
		return float64((vBx-vAx)*m.nx) + float64((vBy-vAy)*m.ny)
	}
	vn0 := relVn(m0, rA0x, rA0y, rB0x, rB0y)
	vn1 := relVn(m1, rA1x, rA1y, rB1x, rB1y)
	var bias0, bias1 float64
	if sep0 > 0 {
		bias0 = sep0 * invSubH
	}
	if sep1 > 0 {
		bias1 = sep1 * invSubH
	}
	p0 := m0.points[0].pn
	if m0.count == 2 {
		p0 += m0.points[1].pn
	}
	p1 := m1.points[0].pn
	if m1.count == 2 {
		p1 += m1.points[1].pn
	}
	b0 := (vn0 + bias0) - (float64(k00*p0) + float64(k01*p1))
	b1 := (vn1 + bias1) - (float64(k01*p0) + float64(k11*p1))
	x0, x1, solved := solve2x2LCP(k00, k01, k11, det, b0, b1)
	if !solved {
		return
	}
	distributeManifoldNormalImpulse(m0, deltas, x0)
	distributeManifoldNormalImpulse(m1, deltas, x1)
}

func solvePinchPairPushOut(m0, m1 *rigidManifold, shared *RigidBody, deltas []rigidBodyDelta) {
	k00, k01, k11, det, rA0x, rA0y, rB0x, rB0y, sep0, rA1x, rA1y, rB1x, rB1y, sep1, ok := pinchPairMatrix(m0, m1, shared, deltas)
	if !ok {
		return
	}
	var bias0, bias1 float64
	if pen0 := sep0 + contactSlop; pen0 < 0 {
		bias0 = max(float64(m0.biasRate*pen0), -contactMaxPushSpeed)
	}
	if pen1 := sep1 + contactSlop; pen1 < 0 {
		bias1 = max(float64(m1.biasRate*pen1), -contactMaxPushSpeed)
	}
	if bias0 == 0 && bias1 == 0 {
		return
	}
	relPv := func(m *rigidManifold, rAx, rAy, rBx, rBy float64) float64 {
		var vAx, vAy, vBx, vBy float64
		if dA := bodyDeltaFor(m.a, deltas); dA != nil {
			vAx = dA.pvx - float64(dA.pav*rAy)
			vAy = dA.pvy + float64(dA.pav*rAx)
		}
		if dB := bodyDeltaFor(m.b, deltas); dB != nil {
			vBx = dB.pvx - float64(dB.pav*rBy)
			vBy = dB.pvy + float64(dB.pav*rBx)
		}
		return float64((vBx-vAx)*m.nx) + float64((vBy-vAy)*m.ny)
	}
	vn0 := relPv(m0, rA0x, rA0y, rB0x, rB0y)
	vn1 := relPv(m1, rA1x, rA1y, rB1x, rB1y)
	p0 := m0.points[0].pushPn
	if m0.count == 2 {
		p0 += m0.points[1].pushPn
	}
	p1 := m1.points[0].pushPn
	if m1.count == 2 {
		p1 += m1.points[1].pushPn
	}
	b0 := (vn0 + bias0) - (float64(k00*p0) + float64(k01*p1))
	b1 := (vn1 + bias1) - (float64(k01*p0) + float64(k11*p1))
	x0, x1, solved := solve2x2LCP(k00, k01, k11, det, b0, b1)
	if !solved {
		return
	}
	distributeManifoldPushImpulse(m0, deltas, x0)
	distributeManifoldPushImpulse(m1, deltas, x1)
}

// solveManifoldVelocitySubstep solves the non-penetration and Coulomb friction velocity constraints for m without injecting position push-out bias.
func solveManifoldVelocitySubstep(m *rigidManifold, deltas []rigidBodyDelta, invSubH float64) {
	if m.count == 0 {
		return
	}
	a, b := m.a, m.b
	nx, ny := m.nx, m.ny
	tx, ty := -ny, nx

	var dA, dB rigidBodyDelta
	if a.Static {
		dA.cosDA = 1
	} else {
		dA = deltas[a.index]
	}
	if b.Static {
		dB.cosDA = 1
	} else {
		dB = deltas[b.index]
	}
	rotA := dA.sinDA != 0 || dA.cosDA != 1
	rotB := dB.sinDA != 0 || dB.cosDA != 1

	var rAx, rAy, rBx, rBy [2]float64
	var specBias [2]float64
	for i := range m.count {
		cp := &m.points[i]
		rAx[i], rAy[i] = cp.rAx, cp.rAy
		if rotA {
			rAx[i] = float64(cp.rAx*dA.cosDA) - float64(cp.rAy*dA.sinDA)
			rAy[i] = float64(cp.rAx*dA.sinDA) + float64(cp.rAy*dA.cosDA)
		}
		rBx[i], rBy[i] = cp.rBx, cp.rBy
		if rotB {
			rBx[i] = float64(cp.rBx*dB.cosDA) - float64(cp.rBy*dB.sinDA)
			rBy[i] = float64(cp.rBx*dB.sinDA) + float64(cp.rBy*dB.cosDA)
		}
		dpX := (dB.dx + (rBx[i] - cp.rBx)) - (dA.dx + (rAx[i] - cp.rAx))
		dpY := (dB.dy + (rBy[i] - cp.rBy)) - (dA.dy + (rAy[i] - cp.rAy))
		sep := -cp.depth + float64(dpX*nx) + float64(dpY*ny)
		if sep > 0 {
			specBias[i] = sep * invSubH
		}
	}

	// Normal impulses: solve 2-point contacts simultaneously with a 2x2 block LCP so flat stacks stay symmetric without tipping from sequential point order.
	solvedBlock := false
	if m.count == 2 {
		invMA, invIA := float64(a.InvMass), float64(a.InvInertia)
		invMB, invIB := float64(b.InvMass), float64(b.InvInertia)
		invM := invMA + invMB
		rnA0 := float64(rAx[0]*ny) - float64(rAy[0]*nx)
		rnB0 := float64(rBx[0]*ny) - float64(rBy[0]*nx)
		rnA1 := float64(rAx[1]*ny) - float64(rAy[1]*nx)
		rnB1 := float64(rBx[1]*ny) - float64(rBy[1]*nx)

		k11 := invM + float64(float64(invIA*rnA0)*rnA0) + float64(float64(invIB*rnB0)*rnB0)
		k22 := invM + float64(float64(invIA*rnA1)*rnA1) + float64(float64(invIB*rnB1)*rnB1)
		k12 := invM + float64(float64(invIA*rnA0)*rnA1) + float64(float64(invIB*rnB0)*rnB1)
		det := float64(k11*k22) - float64(k12*k12)
		if det > 1e-12 && float64(k11*k11) < float64(1000.0*det) {
			cp0, cp1 := &m.points[0], &m.points[1]
			vAx0 := float64(a.VX) - float64(float64(a.AngularVel)*rAy[0])
			vAy0 := float64(a.VY) + float64(float64(a.AngularVel)*rAx[0])
			vBx0 := float64(b.VX) - float64(float64(b.AngularVel)*rBy[0])
			vBy0 := float64(b.VY) + float64(float64(b.AngularVel)*rBx[0])
			vn0 := float64((vBx0-vAx0)*nx) + float64((vBy0-vAy0)*ny)

			vAx1 := float64(a.VX) - float64(float64(a.AngularVel)*rAy[1])
			vAy1 := float64(a.VY) + float64(float64(a.AngularVel)*rAx[1])
			vBx1 := float64(b.VX) - float64(float64(b.AngularVel)*rBy[1])
			vBy1 := float64(b.VY) + float64(float64(b.AngularVel)*rBx[1])
			vn1 := float64((vBx1-vAx1)*nx) + float64((vBy1-vAy1)*ny)

			p0, p1 := cp0.pn, cp1.pn
			b0 := (vn0 + specBias[0]) - (float64(k11*p0) + float64(k12*p1))
			b1 := (vn1 + specBias[1]) - (float64(k12*p0) + float64(k22*p1))

			x0, x1, ok := solve2x2LCP(k11, k12, k22, det, b0, b1)
			if ok {
				solvedBlock = true
				dP0 := x0 - p0
				dP1 := x1 - p1
				cp0.pn = x0
				cp1.pn = x1
				px0, py0 := dP0*nx, dP0*ny
				px1, py1 := dP1*nx, dP1*ny
				a.applyImpulsePairRaw(-px0, -py0, rAx[0], rAy[0], -px1, -py1, rAx[1], rAy[1])
				b.applyImpulsePairRaw(px0, py0, rBx[0], rBy[0], px1, py1, rBx[1], rBy[1])
			}
		}
	}
	if !solvedBlock {
		for i := range m.count {
			cp := &m.points[i]
			vAx := float64(a.VX) - float64(float64(a.AngularVel)*rAy[i])
			vAy := float64(a.VY) + float64(float64(a.AngularVel)*rAx[i])
			vBx := float64(b.VX) - float64(float64(b.AngularVel)*rBy[i])
			vBy := float64(b.VY) + float64(float64(b.AngularVel)*rBx[i])
			vn := float64((vBx-vAx)*nx) + float64((vBy-vAy)*ny)

			dPn := float64(cp.massNormal * (-(vn + specBias[i])))
			oldPn := cp.pn
			cp.pn = max(oldPn+dPn, 0)
			dPn = cp.pn - oldPn

			px, py := dPn*nx, dPn*ny
			a.applyImpulseRaw(-px, -py, rAx[i], rAy[i])
			b.applyImpulseRaw(px, py, rBx[i], rBy[i])
		}
	}

	// Friction impulses.
	if m.friction > 0 {
		for i := range m.count {
			cp := &m.points[i]
			vAx := float64(a.VX) - float64(float64(a.AngularVel)*rAy[i])
			vAy := float64(a.VY) + float64(float64(a.AngularVel)*rAx[i])
			vBx := float64(b.VX) - float64(float64(b.AngularVel)*rBy[i])
			vBy := float64(b.VY) + float64(float64(b.AngularVel)*rBx[i])
			vt := float64((vBx-vAx)*tx) + float64((vBy-vAy)*ty)

			dPt := float64(cp.massTangent * (-vt))
			maxPt := m.friction * cp.pn
			oldPt := cp.pt
			cp.pt = min(max(oldPt+dPt, -maxPt), maxPt)
			dPt = cp.pt - oldPt

			ftx, fty := dPt*tx, dPt*ty
			a.applyImpulseRaw(-ftx, -fty, rAx[i], rAy[i])
			b.applyImpulseRaw(ftx, fty, rBx[i], rBy[i])
		}
	}
}

// solveManifoldPushOut resolves overlap softly at the contact points into per-body pseudo-velocities (deltas[i].pvx/pvy/pav), applying both linear and angular correction without injecting bounce into body velocities.
func solveManifoldPushOut(m *rigidManifold, deltas []rigidBodyDelta) {
	if m.count == 0 {
		return
	}
	a, b := m.a, m.b
	nx, ny := m.nx, m.ny

	var dA, dB *rigidBodyDelta
	var zeroA, zeroB rigidBodyDelta
	zeroA.cosDA, zeroB.cosDA = 1, 1
	if a.Static {
		dA = &zeroA
	} else {
		dA = &deltas[a.index]
	}
	if b.Static {
		dB = &zeroB
	} else {
		dB = &deltas[b.index]
	}
	rotA := dA.sinDA != 0 || dA.cosDA != 1
	rotB := dB.sinDA != 0 || dB.cosDA != 1

	var rAx, rAy, rBx, rBy [2]float64
	var bias [2]float64
	anyPen := false
	for i := range m.count {
		cp := &m.points[i]
		rAx[i], rAy[i] = cp.rAx, cp.rAy
		if rotA {
			rAx[i] = float64(cp.rAx*dA.cosDA) - float64(cp.rAy*dA.sinDA)
			rAy[i] = float64(cp.rAx*dA.sinDA) + float64(cp.rAy*dA.cosDA)
		}
		rBx[i], rBy[i] = cp.rBx, cp.rBy
		if rotB {
			rBx[i] = float64(cp.rBx*dB.cosDA) - float64(cp.rBy*dB.sinDA)
			rBy[i] = float64(cp.rBx*dB.sinDA) + float64(cp.rBy*dB.cosDA)
		}
		dpX := (dB.dx + (rBx[i] - cp.rBx)) - (dA.dx + (rAx[i] - cp.rAx))
		dpY := (dB.dy + (rBy[i] - cp.rBy)) - (dA.dy + (rAy[i] - cp.rAy))
		sep := -cp.depth + float64(dpX*nx) + float64(dpY*ny)
		pen := sep + contactSlop
		if pen < 0 {
			bias[i] = max(float64(m.biasRate*pen), -contactMaxPushSpeed)
			anyPen = true
		}
	}
	if !anyPen && m.points[0].pushPn == 0 && (m.count < 2 || m.points[1].pushPn == 0) {
		return
	}

	applyPush := func(body *RigidBody, d *rigidBodyDelta, ix, iy, rx, ry float64) {
		if body.fixed() {
			return
		}
		invM := float64(body.InvMass)
		invI := float64(body.InvInertia)
		d.pvx += float64(ix * invM)
		d.pvy += float64(iy * invM)
		cross := float64(rx*iy) - float64(ry*ix)
		d.pav += float64(cross * invI)
	}

	solvedBlock := false
	if m.count == 2 {
		invMA, invIA := float64(a.InvMass), float64(a.InvInertia)
		invMB, invIB := float64(b.InvMass), float64(b.InvInertia)
		invM := invMA + invMB
		rnA0 := float64(rAx[0]*ny) - float64(rAy[0]*nx)
		rnB0 := float64(rBx[0]*ny) - float64(rBy[0]*nx)
		rnA1 := float64(rAx[1]*ny) - float64(rAy[1]*nx)
		rnB1 := float64(rBx[1]*ny) - float64(rBy[1]*nx)

		k11 := invM + float64(float64(invIA*rnA0)*rnA0) + float64(float64(invIB*rnB0)*rnB0)
		k22 := invM + float64(float64(invIA*rnA1)*rnA1) + float64(float64(invIB*rnB1)*rnB1)
		k12 := invM + float64(float64(invIA*rnA0)*rnA1) + float64(float64(invIB*rnB0)*rnB1)
		a11 := k11 / m.massScale
		a22 := k22 / m.massScale
		det := float64(a11*a22) - float64(k12*k12)
		if det > 1e-12 && float64(a11*a11) < float64(1000.0*det) {
			cp0, cp1 := &m.points[0], &m.points[1]
			vAx0 := dA.pvx - float64(dA.pav*rAy[0])
			vAy0 := dA.pvy + float64(dA.pav*rAx[0])
			vBx0 := dB.pvx - float64(dB.pav*rBy[0])
			vBy0 := dB.pvy + float64(dB.pav*rBx[0])
			vn0 := float64((vBx0-vAx0)*nx) + float64((vBy0-vAy0)*ny)

			vAx1 := dA.pvx - float64(dA.pav*rAy[1])
			vAy1 := dA.pvy + float64(dA.pav*rAx[1])
			vBx1 := dB.pvx - float64(dB.pav*rBy[1])
			vBy1 := dB.pvy + float64(dB.pav*rBx[1])
			vn1 := float64((vBx1-vAx1)*nx) + float64((vBy1-vAy1)*ny)

			p0, p1 := cp0.pushPn, cp1.pushPn
			b0 := (vn0 + bias[0]) - (float64(k11*p0) + float64(k12*p1))
			b1 := (vn1 + bias[1]) - (float64(k12*p0) + float64(k22*p1))

			x0, x1, ok := solve2x2LCP(a11, k12, a22, det, b0, b1)
			if ok {
				solvedBlock = true
				dP0 := x0 - p0
				dP1 := x1 - p1
				cp0.pushPn = x0
				cp1.pushPn = x1
				px0, py0 := dP0*nx, dP0*ny
				px1, py1 := dP1*nx, dP1*ny
				applyPush(a, dA, -px0, -py0, rAx[0], rAy[0])
				applyPush(b, dB, px0, py0, rBx[0], rBy[0])
				applyPush(a, dA, -px1, -py1, rAx[1], rAy[1])
				applyPush(b, dB, px1, py1, rBx[1], rBy[1])
			}
		}
	}
	if !solvedBlock {
		for i := range m.count {
			cp := &m.points[i]
			vAx := dA.pvx - float64(dA.pav*rAy[i])
			vAy := dA.pvy + float64(dA.pav*rAx[i])
			vBx := dB.pvx - float64(dB.pav*rBy[i])
			vBy := dB.pvy + float64(dB.pav*rBx[i])
			vn := float64((vBx-vAx)*nx) + float64((vBy-vAy)*ny)

			dPn := float64(float64(-m.massScale*cp.massNormal)*(vn+bias[i])) - float64(m.impulseScale*cp.pushPn)
			oldPn := cp.pushPn
			cp.pushPn = max(oldPn+dPn, 0)
			dPn = cp.pushPn - oldPn

			px, py := dPn*nx, dPn*ny
			applyPush(a, dA, -px, -py, rAx[i], rAy[i])
			applyPush(b, dB, px, py, rBx[i], rBy[i])
		}
	}
}

func solve2x2LCP(a11, a12, a22, det, b0, b1 float64) (x0, x1 float64, ok bool) {
	invDet := 1.0 / det
	x0 = -(float64(a22*b0) - float64(a12*b1)) * invDet
	x1 = -(float64(-a12*b0) + float64(a11*b1)) * invDet
	if x0 >= 0 && x1 >= 0 {
		return x0, x1, true
	}
	x0 = -b0 / a11
	x1 = 0
	if x0 >= 0 && float64(a12*x0)+b1 >= 0 {
		return x0, x1, true
	}
	x0 = 0
	x1 = -b1 / a22
	if x1 >= 0 && float64(a12*x1)+b0 >= 0 {
		return x0, x1, true
	}
	if b0 >= 0 && b1 >= 0 {
		return 0, 0, true
	}
	return 0, 0, false
}

func applyManifoldRestitution(m *rigidManifold, deltas []rigidBodyDelta, restitutionThresh float64) {
	if !(m.restitution > 0) {
		return
	}
	a, b := m.a, m.b
	nx, ny := m.nx, m.ny

	var dA, dB rigidBodyDelta
	if a.Static {
		dA.cosDA = 1
	} else {
		dA = deltas[a.index]
	}
	if b.Static {
		dB.cosDA = 1
	} else {
		dB = deltas[b.index]
	}
	rotA := dA.sinDA != 0 || dA.cosDA != 1
	rotB := dB.sinDA != 0 || dB.cosDA != 1

	for i := range m.count {
		cp := &m.points[i]
		if !(-cp.approachVn > restitutionThresh) {
			continue
		}
		if cp.depth < 0 && cp.totalPn <= 0 {
			continue
		}
		rAx, rAy := cp.rAx, cp.rAy
		if rotA {
			rAx = float64(cp.rAx*dA.cosDA) - float64(cp.rAy*dA.sinDA)
			rAy = float64(cp.rAx*dA.sinDA) + float64(cp.rAy*dA.cosDA)
		}
		rBx, rBy := cp.rBx, cp.rBy
		if rotB {
			rBx = float64(cp.rBx*dB.cosDA) - float64(cp.rBy*dB.sinDA)
			rBy = float64(cp.rBx*dB.sinDA) + float64(cp.rBy*dB.cosDA)
		}

		vAx := float64(a.VX) - float64(float64(a.AngularVel)*rAy)
		vAy := float64(a.VY) + float64(float64(a.AngularVel)*rAx)
		vBx := float64(b.VX) - float64(float64(b.AngularVel)*rBy)
		vBy := float64(b.VY) + float64(float64(b.AngularVel)*rBx)
		vn := float64((vBx-vAx)*nx) + float64((vBy-vAy)*ny)

		targetVn := float64(-m.restitution * cp.approachVn)
		if vn >= targetVn {
			continue
		}
		dPn := cp.massNormal * (targetVn - vn)
		px, py := dPn*nx, dPn*ny
		a.applyImpulseRaw(-px, -py, rAx, rAy)
		b.applyImpulseRaw(px, py, rBx, rBy)
	}
}

// collideRigidPair appends the contact manifolds of a and b to out, with normals pointing from a toward b: one for convex shapes, and one per touching segment against a Chain.
func collideRigidPair(a, b *RigidBody, out []rigidManifold) []rigidManifold {
	ak, bk := a.Shape.Kind, b.Shape.Kind
	if (ak == ShapeBox || ak == ShapeCircle) && (bk == ShapeBox || bk == ShapeCircle) {
		if m, hit := collideRigidBodies(a, b); hit {
			out = append(out, m)
		}
		return out
	}
	if ak == ShapeChain || bk == ShapeChain {
		if ak == bk {
			return out
		}
		chainBody, other, flip := a, b, false
		if bk == ShapeChain {
			chainBody, other, flip = b, a, true
		}
		if !chainBody.hasLocal {
			return out
		}
		h, ok := other.rigidHull()
		if !ok {
			return out
		}
		s, _ := chainBody.poseShape()
		if s.Chain.Empty() {
			return out
		}
		var buf [8]hullManifold
		for _, m := range chainManifolds(s.Chain, &h, buf[:0], speculativeDistance) {
			if flip {
				m.nx, m.ny = -m.nx, -m.ny
			}
			out = append(out, rigidManifoldFrom(m, a, b))
		}
		return out
	}
	ha, okA := a.rigidHull()
	hb, okB := b.rigidHull()
	if !okA || !okB {
		return out
	}
	if m := collideHullsWithin(&ha, &hb, speculativeDistance); m.count > 0 {
		out = append(out, rigidManifoldFrom(m, a, b))
	}
	return out
}

func rigidManifoldFrom(m hullManifold, a, b *RigidBody) rigidManifold {
	rm := rigidManifold{a: a, b: b, sub: m.sub, nx: m.nx, ny: m.ny, count: m.count}
	for i := range m.count {
		p := m.points[i]
		rm.points[i] = rigidContactPoint{id: p.id, px: p.x, py: p.y, depth: p.depth}
	}
	return rm
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
	distSq := float64(dx*dx) + float64(dy*dy)
	if distSq >= float64(sumR*sumR) {
		return rigidManifold{}, false
	}
	m := rigidManifold{a: a, b: b, count: 1}
	if distSq == 0 {
		m.nx, m.ny = 0, 1
		m.points[0] = rigidContactPoint{
			id:    pointFeatureID,
			px:    float64(a.X),
			py:    float64(a.Y),
			depth: sumR,
		}
		return m, true
	}
	dist := math.Sqrt(distSq)
	nx, ny := dx/dist, dy/dist
	depth := sumR - dist
	mid := rA - float64(depth*0.5)
	m.nx, m.ny = nx, ny
	m.points[0] = rigidContactPoint{
		id:    pointFeatureID,
		px:    float64(a.X) + float64(nx*mid),
		py:    float64(a.Y) + float64(ny*mid),
		depth: depth,
	}
	return m, true
}

// collideRigidCircleOBB tests circle a against oriented box b. Normal points from a toward b.
func collideRigidCircleOBB(a, b *RigidBody) (rigidManifold, bool) {
	r := float64(a.Shape.Circle.R)
	hw := float64(float64(b.Shape.Box.W) * 0.5)
	hh := float64(float64(b.Shape.Box.H) * 0.5)
	if r <= 0 || hw <= 0 || hh <= 0 {
		return rigidManifold{}, false
	}

	sinB, cosB := detmath.Sincos(float64(b.Angle))
	relX := float64(a.X) - float64(b.X)
	relY := float64(a.Y) - float64(b.Y)
	// Circle center in b's local frame.
	lx := float64(relX*cosB) + float64(relY*sinB)
	ly := float64(-relX*sinB) + float64(relY*cosB)

	cx := min(max(lx, -hw), hw)
	cy := min(max(ly, -hh), hh)
	dx := lx - cx
	dy := ly - cy
	distSq := float64(dx*dx) + float64(dy*dy)

	var lnx, lny, depth float64
	// face is 0 when the circle's center is outside the box, or the box face it exits through, numbering the point's feature.
	var face uint32
	if distSq > 0 {
		if distSq >= float64(r*r) {
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
		face = 1
		if dBottom < best {
			best = dBottom
			lnx, lny = 0, -1
			cx, cy = lx, hh
			face = 2
		}
		if dLeft < best {
			best = dLeft
			lnx, lny = 1, 0
			cx, cy = -hw, ly
			face = 3
		}
		if dRight < best {
			best = dRight
			lnx, lny = -1, 0
			cx, cy = hw, ly
			face = 4
		}
		depth = best + r
	}

	// Transform normal and contact point back to world space.
	wnx := float64(lnx*cosB) - float64(lny*sinB)
	wny := float64(lnx*sinB) + float64(lny*cosB)
	wpx := float64(float64(b.X)+float64(cx*cosB)) - float64(cy*sinB)
	wpy := float64(float64(b.Y)+float64(cx*sinB)) + float64(cy*cosB)

	m := rigidManifold{a: a, b: b, nx: wnx, ny: wny, count: 1}
	m.points[0] = rigidContactPoint{
		id:    pointFeatureID | face,
		px:    wpx,
		py:    wpy,
		depth: depth,
	}
	return m, true
}

type vec2 struct{ x, y float64 }

func dot2(a, b vec2) float64 { return float64(a.x*b.x) + float64(a.y*b.y) }

func clipSegmentToLine(in [2]vec2, ids [2]uint32, count int, normal vec2, offset float64, clipID uint32) ([2]vec2, [2]uint32, int) {
	var out [2]vec2
	var outIDs [2]uint32
	outCount := 0
	if count < 2 {
		return out, outIDs, 0
	}
	d0 := dot2(normal, in[0]) - offset
	d1 := dot2(normal, in[1]) - offset
	if d0 <= 0 {
		out[outCount], outIDs[outCount] = in[0], ids[0]
		outCount++
	}
	if d1 <= 0 {
		out[outCount], outIDs[outCount] = in[1], ids[1]
		outCount++
	}
	if d0*d1 < 0 && outCount < 2 {
		t := d0 / (d0 - d1)
		out[outCount] = vec2{
			x: in[0].x + float64(t*(in[1].x-in[0].x)),
			y: in[0].y + float64(t*(in[1].y-in[0].y)),
		}
		outIDs[outCount] = clipID
		outCount++
	}
	return out, outIDs, outCount
}

// collideRigidOBBOBB computes a 1- or 2-point contact manifold between oriented boxes a and b
// using 2D SAT and Sutherland-Hodgman face clipping. Normal points from a toward b.
func collideRigidOBBOBB(a, b *RigidBody) (rigidManifold, bool) {
	hAx, hAy := float64(float64(a.Shape.Box.W)*0.5), float64(float64(a.Shape.Box.H)*0.5)
	hBx, hBy := float64(float64(b.Shape.Box.W)*0.5), float64(float64(b.Shape.Box.H)*0.5)
	if hAx <= 0 || hAy <= 0 || hBx <= 0 || hBy <= 0 {
		return rigidManifold{}, false
	}

	posA := vec2{float64(a.X), float64(a.Y)}
	posB := vec2{float64(b.X), float64(b.Y)}
	sinA, cosA := detmath.Sincos(float64(a.Angle))
	sinB, cosB := detmath.Sincos(float64(b.Angle))
	uA, vA := vec2{cosA, sinA}, vec2{-sinA, cosA}
	uB, vB := vec2{cosB, sinB}, vec2{-sinB, cosB}

	d := vec2{posB.x - posA.x, posB.y - posA.y}
	dAx, dAy := dot2(d, uA), dot2(d, vA)
	dBx, dBy := dot2(d, uB), dot2(d, vB)

	c00 := math.Abs(dot2(uA, uB))
	c01 := math.Abs(dot2(uA, vB))
	c10 := math.Abs(dot2(vA, uB))
	c11 := math.Abs(dot2(vA, vB))

	faceAx := math.Abs(dAx) - hAx - (float64(c00*hBx) + float64(c01*hBy))
	if faceAx >= 0 {
		return rigidManifold{}, false
	}
	faceAy := math.Abs(dAy) - hAy - (float64(c10*hBx) + float64(c11*hBy))
	if faceAy >= 0 {
		return rigidManifold{}, false
	}
	faceBx := math.Abs(dBx) - hBx - (float64(c00*hAx) + float64(c10*hAy))
	if faceBx >= 0 {
		return rigidManifold{}, false
	}
	faceBy := math.Abs(dBy) - hBy - (float64(c01*hAx) + float64(c11*hAy))
	if faceBy >= 0 {
		return rigidManifold{}, false
	}

	// Pick the axis with minimum penetration (largest separation < 0). A later axis must separate by tieEps more to win, as in Box2D's b2CollidePolygons, so two stacked boxes whose faces separate almost equally keep the same reference face, and their contact points the same feature ids, from one step to the next.
	const tieEps = 0.05
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
		refNegated    bool // the reference face is on the negative side of its box axis
	)

	switch axis {
	case 0: // A_x
		refPos = posA
		if dAx >= 0 {
			refNormal = uA
		} else {
			refNormal = vec2{-uA.x, -uA.y}
			refNegated = true
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
			refNegated = true
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
			refNegated = true
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
			refNegated = true
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
	// incFaceID numbers the incident face 0 to 3 so each clipped point's feature id names it.
	var incFaceID int
	if math.Abs(incDotU) > math.Abs(incDotV) {
		sign := 1.0
		if incDotU > 0 {
			sign = -1.0
			incFaceID = 1
		}
		fc := vec2{incPos.x + float64(float64(incU.x*sign)*incHX), incPos.y + float64(float64(incU.y*sign)*incHX)}
		incFace[0] = vec2{fc.x - float64(incV.x*incHY), fc.y - float64(incV.y*incHY)}
		incFace[1] = vec2{fc.x + float64(incV.x*incHY), fc.y + float64(incV.y*incHY)}
	} else {
		sign := 1.0
		incFaceID = 2
		if incDotV > 0 {
			sign = -1.0
			incFaceID = 3
		}
		fc := vec2{incPos.x + float64(float64(incV.x*sign)*incHY), incPos.y + float64(float64(incV.y*sign)*incHY)}
		incFace[0] = vec2{fc.x - float64(incU.x*incHX), fc.y - float64(incU.y*incHX)}
		incFace[1] = vec2{fc.x + float64(incU.x*incHX), fc.y + float64(incU.y*incHX)}
	}

	// Clip incident face against the two side planes of the reference face.
	sideOffset := dot2(refPos, sideAxis)
	refFaceID := axis << 1
	if refNegated {
		refFaceID |= 1
	}
	inIDs := [2]uint32{clipFeatureID(false, refFaceID, incFaceID, 0), clipFeatureID(false, refFaceID, incFaceID, 1)}
	clipped, clippedIDs, nClip := clipSegmentToLine(incFace, inIDs, 2, sideAxis, sideOffset+refExtentSide, clipFeatureID(false, refFaceID, incFaceID, 2))
	if nClip < 2 {
		return rigidManifold{}, false
	}
	negSide := vec2{-sideAxis.x, -sideAxis.y}
	clipped, clippedIDs, nClip = clipSegmentToLine(clipped, clippedIDs, nClip, negSide, -sideOffset+refExtentSide, clipFeatureID(false, refFaceID, incFaceID, 3))
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
	touching := false
	for i := range nClip {
		sep := dot2(clipped[i], refNormal) - frontOffset
		if sep > speculativeDistance {
			continue
		}
		depth := -sep
		if sep <= 1e-6 {
			depth = max(0, -sep)
			touching = true
		}
		m.points[m.count] = rigidContactPoint{
			px:    clipped[i].x,
			py:    clipped[i].y,
			depth: depth,
			id:    clippedIDs[i],
		}
		m.count++
	}
	if !touching {
		return rigidManifold{}, false
	}
	return m, true
}
