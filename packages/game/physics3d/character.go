package physics3d

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/internal/detmath"
)

const (
	defaultMaxSlopeAngle = float32(45 * math.Pi / 180)
	defaultStepHeight    = 0.35
	ceilingNormalY       = -0.7
	contactSkin          = 1e-4
	crushDepthEps        = 1e-3
	crushOpposingDot     = -0.5
	maxDepenIters        = 8
	maxKinematicSubSteps = 64
)

// PlatformOnLeave controls how a CharacterController inherits the moving
// ground body's velocity when leaving it (by jumping or walking off).
type PlatformOnLeave int

const (
	// PlatformLeaveAddVelocity adds the full ground velocity at the contact
	// point to the character's velocity when leaving a moving ground body.
	// This is the default zero value.
	PlatformLeaveAddVelocity PlatformOnLeave = iota
	// PlatformLeaveAddUpwardVelocity adds the ground velocity when leaving a
	// moving ground body, clamping any downward (negative Y) component to zero
	// so jumping or stepping off a descending platform is not pulled downward.
	PlatformLeaveAddUpwardVelocity
	// PlatformLeaveDoNothing drops the platform's velocity upon leaving.
	PlatformLeaveDoNothing
)

// CharacterController is a kinematic capsule collider that moves through a
// World with walkable slope sliding, automatic stair stepping, headroom checks,
// tangential wall sliding, moving-platform carry, and kinematic push/crush
// resolution.
//
// Pos is the world-space position of the bottom of the capsule (the character's
// feet).
type CharacterController struct {
	// Pos is the world-space position of the bottom of the capsule (feet).
	Pos Vec3
	// Radius is the capsule radius.
	Radius float32
	// Height is the total height of the capsule (clamped to at least 2*Radius).
	Height float32
	// StepHeight is the maximum vertical step height climbed automatically.
	StepHeight float32
	// MaxSlopeAngle is the steepest walkable floor angle from horizontal, in
	// radians. Values <= 0 default to 45 degrees (pi/4).
	MaxSlopeAngle float32
	// Vel is the controller's velocity in units per second.
	Vel Vec3
	// Yaw is the character's facing angle in radians around the +Y axis.
	// When ApplyPlatformYaw is true, Move adds GroundYawDelta to Yaw while
	// carried by a rotating ground body.
	Yaw float32
	// ApplyPlatformYaw controls whether Move adds a rotating ground body's
	// per-step yaw delta to Yaw. NewCharacterController initializes it to true.
	// Position carry around a rotating ground body always applies regardless of
	// this setting.
	ApplyPlatformYaw bool
	// PlatformOnLeave controls how the controller inherits GroundVelocity when
	// jumping or walking off a moving ground body.
	PlatformOnLeave PlatformOnLeave
	// Grounded reports whether the controller ended the last Move on walkable ground.
	Grounded bool
	// GroundNormal is the outward surface normal of the ground when Grounded is true.
	GroundNormal Vec3
	// GroundBody is the Body supporting the character when Grounded is true, or
	// nil when airborne or standing on a static collider without a Body
	// (TriMeshCollider, HeightfieldCollider, or VoxelCollider).
	GroundBody *Body
	// GroundVelocity is the world-space velocity of GroundBody at the contact
	// point during the last Move, or zero when airborne or on static geometry.
	GroundVelocity Vec3
	// GroundYawDelta is the yaw rotation in radians around +Y imparted by the
	// ground body during the last Move.
	GroundYawDelta float32
	// OnWall reports whether a non-walkable wall or steep slope was contacted.
	OnWall bool
	// OnCeiling reports whether a ceiling surface was contacted.
	OnCeiling bool
	// Crushed reports whether the capsule remained squeezed between opposing
	// surfaces with no valid depenetration during the last Move.
	Crushed bool
	// IgnoreBodyID optionally ignores a body with this ID during movement queries.
	IgnoreBodyID int
	// Mask filters which body and static collider layers block the character
	// (0 defaults to LayerAll).
	Mask LayerMask
	// PushForce is the horizontal force in newtons the character applies to a dynamic body it walks into, so crates and barrels give way instead of acting as walls. 0 leaves dynamic bodies blocking the character like static ones.
	//
	// The controller is not a body in the World, so dynamic bodies do not collide with it on their own; to let them, add a kinematic body that follows Pos and set IgnoreBodyID to its ID (#546).
	PushForce float32
	// GravityScale multiplies the effective gravity applied during Move.
	// NewCharacterController initializes it to 1; on a zero-initialized
	// CharacterController literal it defaults to 1 unless SetGravityScale(0) is called.
	GravityScale float32

	gravityScaleSet bool
	pushes          []characterPush
	lastWorld       *World
	groundBodyPos   Vec3
	groundBodyRot   Quat
	hasGroundPose   bool
	airCarryVel     Vec3
	kinematicPoses  map[int]kinematicPose
}

type kinematicPose struct {
	body *Body
	pos  Vec3
	rot  Quat
}

// characterPush is a dynamic body the character walked into during one Move, with the horizontal wall normal facing the character.
type characterPush struct {
	body   *Body
	normal Vec3
}

// notePush records that the character pressed against b, once per body per slide; from is where the current slide's pushes start in c.pushes.
func (c *CharacterController) notePush(b *Body, wallN Vec3, from int) {
	if b == nil || b.Static || b.Kinematic || b.InvMass == 0 || b.IsTrigger {
		return
	}
	for i := from; i < len(c.pushes); i++ {
		if c.pushes[i].body == b {
			return
		}
	}
	c.pushes = append(c.pushes, characterPush{body: b, normal: wallN})
}

func (c *CharacterController) effectiveMask() LayerMask {
	return effectiveLayer(c.Mask)
}

// NewCharacterController returns a CharacterController at pos with the given
// capsule radius and height, a default StepHeight of 0.35, a 45-degree
// MaxSlopeAngle, and ApplyPlatformYaw enabled.
func NewCharacterController(pos Vec3, radius, height float32) *CharacterController {
	if height < 2*radius {
		height = 2 * radius
	}
	return &CharacterController{
		Pos:              pos,
		Radius:           radius,
		Height:           height,
		StepHeight:       defaultStepHeight,
		MaxSlopeAngle:    defaultMaxSlopeAngle,
		ApplyPlatformYaw: true,
		GravityScale:     1,
		gravityScaleSet:  true,
	}
}

// EffectiveGravity returns the scaled gravity acceleration acting on c in world,
// combining world.Gravity with any overlapping active trigger areas.
func (c *CharacterController) EffectiveGravity(world *World) Vec3 {
	if c == nil || world == nil || c.Radius <= 0 {
		return Vec3{}
	}
	eff := world.evaluateShapeAreasForCharacter(c.Shape(), c.IgnoreBodyID, c.effectiveMask())
	return eff.Gravity.Scale(c.EffectiveGravityScale())
}

// Capsule returns the controller's world-space capsule at Pos.
func (c *CharacterController) Capsule() Capsule {
	return c.capsuleAt(c.Pos)
}

// Shape returns the controller's world-space Shape at Pos.
func (c *CharacterController) Shape() Shape {
	return CapsuleShape(c.Capsule())
}

func (c *CharacterController) capsuleAt(feet Vec3) Capsule {
	r := c.Radius
	topY := c.Height - r
	if topY < r {
		topY = r
	}
	return Capsule{
		A:      Vec3{X: feet.X, Y: feet.Y + r, Z: feet.Z},
		B:      Vec3{X: feet.X, Y: feet.Y + topY, Z: feet.Z},
		Radius: r,
	}
}

func (c *CharacterController) maxSlopeAngle() float32 {
	if c.MaxSlopeAngle <= 0 {
		return defaultMaxSlopeAngle
	}
	return c.MaxSlopeAngle
}

// MoveResult summarizes the outcome of a CharacterController.Move call.
type MoveResult struct {
	Pos            Vec3
	Grounded       bool
	GroundNormal   Vec3
	GroundBody     *Body
	GroundVelocity Vec3
	GroundYawDelta float32
	OnWall         bool
	OnCeiling      bool
	SteppedUp      bool
	Crushed        bool
}

// Move advances the character through world for dt.
//
// wishMove is the desired velocity in units per second: X and Z drive
// horizontal movement, while a non-zero Y sets Vel.Y directly (such as for a
// jump) and Y == 0 lets world.Gravity govern vertical motion.
func (c *CharacterController) Move(world *World, wishMove Vec3, dt time.Duration) MoveResult {
	if world == nil || dt <= 0 || c.Radius <= 0 {
		return MoveResult{
			Pos:            c.Pos,
			Grounded:       c.Grounded,
			GroundNormal:   c.GroundNormal,
			GroundBody:     c.GroundBody,
			GroundVelocity: c.GroundVelocity,
			GroundYawDelta: c.GroundYawDelta,
			OnWall:         c.OnWall,
			OnCeiling:      c.OnCeiling,
			Crushed:        c.Crushed,
		}
	}

	if c.lastWorld != world {
		c.lastWorld = world
		c.hasGroundPose = false
		c.kinematicPoses = nil
	}

	dtSec := float32(dt.Seconds())
	c.pushes = c.pushes[:0]
	maxSlope := c.maxSlopeAngle()
	minGroundY := float32(detmath.Cos(float64(maxSlope)))

	wasGrounded := c.Grounded
	prevGroundNormal := c.GroundNormal
	prevGroundBody := c.GroundBody
	prevGroundVel := c.GroundVelocity
	hadPrevGroundPose := c.hasGroundPose
	prevGroundPos := c.groundBodyPos
	prevGroundRot := c.groundBodyRot
	if prevGroundBody != nil && !c.bodyInWorld(world, prevGroundBody) {
		prevGroundBody = nil
		hadPrevGroundPose = false
		c.hasGroundPose = false
	}

	c.OnWall = false
	c.OnCeiling = false
	c.Crushed = false
	c.GroundYawDelta = 0
	steppedUp := false
	crushed := false

	jumping := wishMove.Y > 0 || (wishMove.Y == 0 && c.Vel.Y > 0)

	// Step 1: Carry the character by its previous kinematic or dynamic ground
	// body before resolving collisions and applying wishMove.
	var stepGroundVel Vec3
	if wasGrounded && prevGroundBody != nil && isMovableBody(prevGroundBody) {
		carryDisp, yawDelta, gVel := c.groundBodyMotion(prevGroundBody, c.Pos, dtSec, hadPrevGroundPose, prevGroundPos, prevGroundRot)
		stepGroundVel = gVel
		if !jumping {
			if carryDisp.LenSq() > 1e-12 {
				c.applyCarryDisplacement(world, prevGroundBody, carryDisp, minGroundY)
			}
			if yawDelta != 0 {
				c.GroundYawDelta = yawDelta
				if c.ApplyPlatformYaw {
					c.Yaw += yawDelta
				}
			}
		}
	}

	// Step 2: Push the character out of any kinematic bodies that moved into
	// the capsule since the last Move (sliding doors, walls, crushers, lifts).
	var skipIntrusionBody *Body
	if wasGrounded && prevGroundBody != nil {
		skipIntrusionBody = prevGroundBody
	}
	c.resolveKinematicIntrusions(world, skipIntrusionBody, minGroundY)

	// Step 3: Clear any remaining overlap and detect crushing between opposing
	// surfaces. When jumping off a rising platform, skip walkable floor
	// depenetration against prevGroundBody here so the upward jump is not
	// double-boosted; post-move depenetration ensures the capsule still clears it.
	var skipWalkableDepen *Body
	if wasGrounded && jumping && prevGroundBody != nil {
		skipWalkableDepen = prevGroundBody
	}
	var preCrushed bool
	c.Pos, preCrushed = c.depenetrateAndCheckCrush(world, c.Pos, minGroundY, skipWalkableDepen)
	crushed = crushed || preCrushed

	// Resolve area gravity, damping, and fluid drag at the character's current pose.
	eff := world.evaluateShapeAreasForCharacter(c.Shape(), c.IgnoreBodyID, c.effectiveMask())
	effGravity := eff.Gravity.Scale(c.EffectiveGravityScale())

	// Prepare velocity for this tick, applying PlatformOnLeave when jumping off
	// a moving ground body.
	appliedLeaveVel := false
	if wasGrounded && jumping && prevGroundBody != nil {
		gVel := stepGroundVel
		if gVel.LenSq() == 0 {
			gVel = prevGroundVel
		}
		leaveVel := platformLeaveVelocity(gVel, c.PlatformOnLeave)
		c.airCarryVel = Vec3{X: leaveVel.X, Z: leaveVel.Z}
		if wishMove.Y > 0 {
			c.Vel.Y = wishMove.Y + leaveVel.Y
		} else {
			c.Vel.Y += leaveVel.Y
		}
		appliedLeaveVel = true
	} else if wasGrounded {
		c.airCarryVel = Vec3{}
	}

	if wishMove.X != 0 || effGravity.X == 0 {
		c.Vel.X = wishMove.X + c.airCarryVel.X
	} else if appliedLeaveVel {
		c.Vel.X += c.airCarryVel.X
	}
	if wishMove.Z != 0 || effGravity.Z == 0 {
		c.Vel.Z = wishMove.Z + c.airCarryVel.Z
	} else if appliedLeaveVel {
		c.Vel.Z += c.airCarryVel.Z
	}
	if !appliedLeaveVel && wishMove.Y != 0 {
		c.Vel.Y = wishMove.Y
	}

	c.Vel = c.Vel.Add(effGravity.Scale(dtSec))
	if eff.FluidLinearDrag > 0 {
		factor := 1 / (1 + float32(eff.FluidLinearDrag*dtSec))
		c.Vel = eff.FluidVelocity.Add(c.Vel.Sub(eff.FluidVelocity).Scale(factor))
		c.airCarryVel = c.airCarryVel.Scale(factor)
	}
	if eff.HasLinearDamping && eff.LinearDamping > 0 {
		factor := 1 / (1 + float32(eff.LinearDamping*dtSec))
		c.Vel = c.Vel.Scale(factor)
		c.airCarryVel = c.airCarryVel.Scale(factor)
	}
	if wasGrounded && !jumping && c.Vel.Y < 0 && effGravity.Y < 0 {
		// Prevent unbounded negative velocity accumulation while walking on ground.
		c.Vel.Y = effGravity.Y * dtSec
	}

	// Phase 1: Horizontal move with walkable-slope gliding and stair step-up.
	horizDisp := Vec3{X: c.Vel.X * dtSec, Z: c.Vel.Z * dtSec}
	horizDist := horizDisp.Len()
	startPos := c.Pos
	horizGrounded := false
	var groundPoint Vec3
	hasGroundPoint := false

	if horizDist > 1e-6 {
		moveDisp := horizDisp
		// When grounded on a walkable slope and moving uphill, align the move
		// with the slope plane so the controller climbs smoothly at steady speed.
		if wasGrounded && !jumping && prevGroundNormal.Y >= minGroundY && prevGroundNormal.Y < 0.9999 {
			if horizDisp.Dot(prevGroundNormal) < 0 {
				dy := -(float32(horizDisp.X*prevGroundNormal.X) + float32(horizDisp.Z*prevGroundNormal.Z)) / prevGroundNormal.Y
				moveDisp = Vec3{X: horizDisp.X, Y: dy, Z: horizDisp.Z}.Norm().Scale(horizDist)
			}
		}

		normalRes := c.slideHorizontal(world, startPos, moveDisp, minGroundY, nil)
		// Pushes recorded so far belong to the plain slide; the step-up probe appends its own after them, and only the path Move commits keeps its pushes, so a body the probe merely touched, or one the character steps onto, is not shoved (#546).
		normalPushes := len(c.pushes)

		// Attempt automatic stair step-up when blocked by a wall while grounded.
		didStep := false
		if normalRes.hitWall && c.StepHeight > 0 && (wasGrounded || normalRes.hitGround) && !jumping {
			normalHoriz := Vec3{X: normalRes.pos.X - startPos.X, Z: normalRes.pos.Z - startPos.Z}
			if stepPos, landNormal, landBody, landPoint, ok := c.tryStepUp(world, startPos, horizDisp, normalHoriz.LenSq(), minGroundY); ok {
				c.pushes = append(c.pushes[:0], c.pushes[normalPushes:]...)
				c.Pos = stepPos
				steppedUp = true
				didStep = true
				c.Grounded = true
				c.GroundNormal = landNormal
				c.GroundBody = landBody
				groundPoint = landPoint
				hasGroundPoint = true
				if c.Vel.Y < 0 {
					c.Vel.Y = 0
				}
			}
		}

		if !didStep {
			c.pushes = c.pushes[:normalPushes]
			c.Pos = normalRes.pos
			c.OnWall = c.OnWall || normalRes.hitWall
			c.OnCeiling = c.OnCeiling || normalRes.hitCeiling
			if normalRes.hitWall && !wasGrounded && c.airCarryVel.LenSq() > 0 && normalRes.wallNormal.LenSq() > 0 {
				if c.airCarryVel.Dot(normalRes.wallNormal) < 0 {
					c.airCarryVel = projectOnPlane(c.airCarryVel, normalRes.wallNormal)
					c.airCarryVel.Y = 0
				}
			}
			if normalRes.hitGround {
				horizGrounded = true
			}
		}
	}

	// Phase 2: Vertical motion (jump/gravity/steep-slope slide) and ground snap.
	if steppedUp {
		// Step-up already landed cleanly on a verified walkable tread.
	} else if c.Vel.Y > 0 {
		// Rising (e.g., jumping).
		upDisp := Vec3{Y: c.Vel.Y * dtSec}
		hit := c.sweepCapsuleHit(world, c.Pos, upDisp, nil)
		c.Pos = hit.pos
		c.Grounded = false
		c.GroundNormal = Vec3{}
		c.GroundBody = nil
		if hit.hit {
			if hit.normal.Y <= ceilingNormalY {
				c.OnCeiling = true
			} else {
				c.OnWall = true
			}
			c.Vel.Y = 0
		}
	} else {
		// Falling or maintaining contact with walkable ground/slopes.
		snapped := false
		if wasGrounded && !jumping {
			slopeDrop := float32(horizDist*float32(detmath.Tan(float64(maxSlope)))) + 0.05
			snapDist := max(c.StepHeight+0.05, slopeDrop)
			if snapHit := c.sweepCapsuleHit(world, c.Pos, Vec3{Y: -snapDist}, nil); snapHit.hit && snapHit.normal.Y >= minGroundY {
				c.Pos = snapHit.pos
				c.Grounded = true
				c.GroundNormal = snapHit.normal
				c.GroundBody = snapHit.body
				groundPoint = snapHit.point
				hasGroundPoint = true
				c.Vel.Y = 0
				snapped = true
			}
		}

		if !snapped {
			gravDisp := Vec3{Y: c.Vel.Y * dtSec}
			vertPoint, vertHasPoint := c.slideVertical(world, gravDisp, dtSec, minGroundY, effGravity.Y)
			if c.Grounded && vertHasPoint {
				groundPoint = vertPoint
				hasGroundPoint = true
			}
			if !c.Grounded && horizGrounded {
				// Keep ground contact if we just moved onto a walkable surface
				// and the downward sweep did not hit anything lower.
				if probeHit := c.sweepCapsuleHit(world, c.Pos, Vec3{Y: -0.02}, nil); probeHit.hit && probeHit.normal.Y >= minGroundY {
					c.Pos = probeHit.pos
					c.Grounded = true
					c.GroundNormal = probeHit.normal
					c.GroundBody = probeHit.body
					groundPoint = probeHit.point
					hasGroundPoint = true
					c.Vel.Y = 0
				}
			}
		}
	}

	// Post-move depenetration when jumping off a moving ground body (ensures a
	// fast rising platform still clears the capsule under PlatformLeaveDoNothing).
	if skipWalkableDepen != nil {
		var postCrushed bool
		c.Pos, postCrushed = c.depenetrateAndCheckCrush(world, c.Pos, minGroundY, nil)
		crushed = crushed || postCrushed
	}

	// Apply PlatformOnLeave when walking off a moving ground body.
	if wasGrounded && !c.Grounded && !appliedLeaveVel && prevGroundBody != nil {
		gVel := stepGroundVel
		if gVel.LenSq() == 0 {
			gVel = prevGroundVel
		}
		leaveVel := platformLeaveVelocity(gVel, c.PlatformOnLeave)
		c.airCarryVel = Vec3{X: leaveVel.X, Z: leaveVel.Z}
		c.Vel = c.Vel.Add(leaveVel)
	}
	if c.Grounded {
		c.airCarryVel = Vec3{}
	}

	// Compute GroundVelocity at the final contact point and record poses for the
	// next Move call.
	if c.Grounded && c.GroundBody != nil {
		pt := c.Pos
		if hasGroundPoint {
			pt = groundPoint
		}
		usePrev := c.GroundBody == prevGroundBody && hadPrevGroundPose
		c.GroundVelocity = c.groundBodyVelocityAt(c.GroundBody, pt, dtSec, usePrev, prevGroundPos, prevGroundRot)
		c.groundBodyPos = c.GroundBody.Pos
		c.groundBodyRot = normalizedQuat(c.GroundBody.Rot)
		c.hasGroundPose = true
	} else {
		c.GroundBody = nil
		c.GroundVelocity = Vec3{}
		c.hasGroundPose = false
	}

	c.Crushed = crushed
	c.recordKinematicPoses(world)

	if c.PushForce > 0 {
		for _, push := range c.pushes {
			push.body.ApplyImpulse(push.normal.Neg().Scale(c.PushForce * dtSec))
		}
	}
	clear(c.pushes)
	c.pushes = c.pushes[:0]

	return MoveResult{
		Pos:            c.Pos,
		Grounded:       c.Grounded,
		GroundNormal:   c.GroundNormal,
		GroundBody:     c.GroundBody,
		GroundVelocity: c.GroundVelocity,
		GroundYawDelta: c.GroundYawDelta,
		OnWall:         c.OnWall,
		OnCeiling:      c.OnCeiling,
		SteppedUp:      steppedUp,
		Crushed:        c.Crushed,
	}
}

type slideResult struct {
	pos        Vec3
	hitGround  bool
	hitWall    bool
	wallNormal Vec3
	hitCeiling bool
}

// slideHorizontal moves from startPos by disp, gliding up walkable slopes and
// sliding tangentially along walls and steep slopes without gaining height on
// non-walkable surfaces.
func (c *CharacterController) slideHorizontal(world *World, startPos, disp Vec3, minGroundY float32, ignoreBody *Body) slideResult {
	res := slideResult{pos: startPos}
	pushFrom := len(c.pushes)
	totalLen := disp.Len()
	if totalLen <= 1e-7 {
		return res
	}

	maxStep := max(c.Radius*0.25, 0.02)
	steps := int(math.Ceil(float64(totalLen / maxStep)))
	if steps < 1 {
		steps = 1
	}
	stepDisp := disp.Scale(1 / float32(steps))

	for s := 0; s < steps; s++ {
		rem := stepDisp
		for iter := 0; iter < 4; iter++ {
			if rem.LenSq() <= 1e-12 {
				break
			}
			candPos := res.pos.Add(rem)
			contact, body, ok := c.deepestOverlapBodyIgnoring(world, candPos, ignoreBody)
			if !ok {
				res.pos = candPos
				break
			}

			n := contact.Normal
			if n.Y >= minGroundY {
				// Walkable floor or ramp: separate along the surface normal and
				// continue sliding along the plane.
				res.pos = candPos.Add(n.Scale(contact.Depth + contactSkin))
				res.hitGround = true
				rem = projectOnPlane(rem, n)
			} else if n.Y <= ceilingNormalY {
				res.pos = candPos.Add(n.Scale(contact.Depth + contactSkin))
				res.hitCeiling = true
				rem = projectOnPlane(rem, n)
			} else {
				// Vertical wall or steep slope (> MaxSlopeAngle): push back
				// purely horizontally so horizontal movement never climbs a
				// steep slope, and slide tangentially along the wall.
				res.hitWall = true
				wallN := Vec3{X: n.X, Z: n.Z}.Norm()
				if wallN.LenSq() == 0 {
					res.pos = candPos.Add(n.Scale(contact.Depth + contactSkin))
					break
				}
				res.wallNormal = wallN
				c.notePush(body, wallN, pushFrom)
				denom := wallN.Dot(n)
				push := contact.Depth + contactSkin
				if denom > 0.1 {
					push = (contact.Depth + contactSkin) / denom
				}
				res.pos = candPos.Add(wallN.Scale(push))
				// Slide remaining horizontal displacement tangentially along wallN.
				horizRem := Vec3{X: rem.X, Z: rem.Z}
				rem = projectOnPlane(horizRem, wallN)
			}
		}
		res.pos = c.depenetrateHorizontalIgnoring(world, res.pos, minGroundY, ignoreBody)
	}

	return res
}

// tryStepUp attempts to climb a stair up to StepHeight from startPos along
// horizDisp, verifying headroom, greater horizontal progress than the blocked
// move, and a walkable landing surface.
func (c *CharacterController) tryStepUp(world *World, startPos, horizDisp Vec3, normalDistSq, minGroundY float32) (Vec3, Vec3, *Body, Vec3, bool) {
	// 1. Check headroom by sweeping straight up by StepHeight.
	upHit := c.sweepCapsuleHit(world, startPos, Vec3{Y: c.StepHeight}, nil)
	actualUp := upHit.pos.Y - startPos.Y
	if actualUp <= 1e-3 {
		return Vec3{}, Vec3{}, nil, Vec3{}, false
	}

	// 2. Advance horizontally at the raised height.
	fwdRes := c.slideHorizontal(world, upHit.pos, horizDisp, minGroundY, nil)
	stepHoriz := Vec3{X: fwdRes.pos.X - startPos.X, Z: fwdRes.pos.Z - startPos.Z}
	if stepHoriz.LenSq() <= normalDistSq+1e-6 {
		return Vec3{}, Vec3{}, nil, Vec3{}, false
	}

	// 3. Sweep back down to land on the step tread.
	landHit := c.sweepCapsuleHit(world, fwdRes.pos, Vec3{Y: -(actualUp + 0.02)}, nil)
	if !landHit.hit {
		return Vec3{}, Vec3{}, nil, Vec3{}, false
	}
	landPos := landHit.pos
	if landPos.Y > startPos.Y+c.StepHeight+1e-3 || landPos.Y < startPos.Y-0.02 {
		return Vec3{}, Vec3{}, nil, Vec3{}, false
	}

	// Verify the landing surface is truly walkable (not a steep slope).
	// A rounded capsule touching a flat stair corner can report an angled corner
	// normal even though the step tread itself is flat, so when landNormal is
	// angled we probe the tread directly ahead with a downward ray.
	groundNormal := landHit.normal
	groundBody := landHit.body
	groundPoint := landHit.point
	if groundNormal.Y < minGroundY {
		horizDir := Vec3{X: horizDisp.X, Z: horizDisp.Z}.Norm()
		rayStart := Vec3{
			X: fwdRes.pos.X + float32(horizDir.X*c.Radius),
			Y: upHit.pos.Y + 0.01,
			Z: fwdRes.pos.Z + float32(horizDir.Z*c.Radius),
		}
		rayEnd := Vec3{X: rayStart.X, Y: startPos.Y - 0.05, Z: rayStart.Z}
		rayHit, ok := c.raycastFirst(world, rayStart, rayEnd)
		if !ok || rayHit.T <= 0 || rayHit.Normal.Y < minGroundY || rayHit.Point.Y > startPos.Y+c.StepHeight+1e-3 {
			return Vec3{}, Vec3{}, nil, Vec3{}, false
		}
		groundNormal = rayHit.Normal
		groundBody = rayHit.Body
		groundPoint = rayHit.Point
	}

	if _, overlap := c.deepestOverlap(world, landPos); overlap {
		landPos = c.depenetrate(world, landPos, minGroundY)
		if _, still := c.deepestOverlap(world, landPos); still {
			return Vec3{}, Vec3{}, nil, Vec3{}, false
		}
	}

	return landPos, groundNormal, groundBody, groundPoint, true
}

func (c *CharacterController) raycastFirst(world *World, from, to Vec3) (RaycastHit, bool) {
	delta := to.Sub(from)
	maxDist := delta.Len()
	if maxDist <= 0 {
		return RaycastHit{}, false
	}
	ray := Ray{Origin: from, Dir: delta, MaxDist: maxDist}
	mask := c.effectiveMask()
	var best RaycastHit
	found := false
	for _, b := range world.bodies {
		if b.ID == c.IgnoreBodyID || b.IsTrigger {
			continue
		}
		if (effectiveLayer(b.Layer) & mask) == 0 {
			continue
		}
		if h, ok := RaycastShape(ray, b.WorldShape()); ok {
			h.Body = b
			if !found || h.T < best.T {
				best = h
				found = true
			}
		}
	}
	for _, tm := range world.triMeshes {
		if (effectiveLayer(tm.Layer) & mask) == 0 {
			continue
		}
		if h, ok := tm.Raycast(ray); ok {
			h.TriMesh = tm
			if !found || h.T < best.T {
				best = h
				found = true
			}
		}
	}
	for _, hf := range world.heightfields {
		if (effectiveLayer(hf.Layer) & mask) == 0 {
			continue
		}
		if h, ok := hf.Raycast(ray); ok {
			h.Heightfield = hf
			if !found || h.T < best.T {
				best = h
				found = true
			}
		}
	}
	for _, vc := range world.voxels {
		if (effectiveLayer(vc.Layer) & mask) == 0 {
			continue
		}
		if h, ok := vc.Raycast(ray); ok {
			h.Voxel = vc
			if !found || h.T < best.T {
				best = h
				found = true
			}
		}
	}
	return best, found
}

// slideVertical moves along a downward gravity displacement, landing cleanly on
// walkable ground or sliding downhill on slopes steeper than MaxSlopeAngle.
func (c *CharacterController) slideVertical(world *World, disp Vec3, dtSec, minGroundY, gravityY float32) (Vec3, bool) {
	rem := disp
	c.Grounded = false
	c.GroundNormal = Vec3{}
	c.GroundBody = nil

	for iter := 0; iter < 3; iter++ {
		if rem.LenSq() <= 1e-12 {
			break
		}
		hit := c.sweepCapsuleHit(world, c.Pos, rem, nil)
		moved := hit.pos.Sub(c.Pos)
		c.Pos = hit.pos
		if !hit.hit {
			break
		}

		hitNormal := hit.normal
		if hitNormal.Y >= minGroundY {
			c.Grounded = true
			c.GroundNormal = hitNormal
			c.GroundBody = hit.body
			c.Vel.Y = 0
			return hit.point, true
		}
		if hitNormal.Y <= ceilingNormalY {
			c.OnCeiling = true
		} else {
			c.OnWall = true
		}

		// Steep slope (0 < hitNormal.Y < minGroundY): slide downhill along the
		// plane's steepest-descent direction with exact inclined-plane gravity
		// acceleration (g_slope.Y = g.Y * sin^2(theta)).
		if hitNormal.Y > 1e-4 && c.Vel.Y < 0 {
			downhill := Vec3{Y: -1}.Sub(hitNormal.Scale(-hitNormal.Y)).Norm()
			if downhill.Y < -1e-4 {
				sinSq := 1 - float32(hitNormal.Y*hitNormal.Y)
				if moved.LenSq() > 1e-6 {
					// Initial impact from free-fall: project vertical velocity onto slope once.
					c.Vel.Y *= sinSq
				} else {
					// Already sliding on the slope: cancel only the normal component of
					// this tick's gravity so velocity accelerates smoothly downhill.
					c.Vel.Y -= float32(float32(float32(gravityY*hitNormal.Y)*hitNormal.Y) * dtSec)
				}
				remFrac := float32(1)
				if totalLen := rem.Len(); totalLen > 1e-6 {
					remFrac = clamp32(1-moved.Len()/totalLen, 0, 1)
				}
				speedAlongSlope := c.Vel.Y / downhill.Y // positive downhill speed
				rem = downhill.Scale(speedAlongSlope * dtSec * remFrac)
				continue
			}
		}

		leftover := rem.Sub(moved)
		rem = projectOnPlane(leftover, hitNormal)
		if rem.Y > 0 {
			rem.Y = 0
		}
	}
	return Vec3{}, false
}

type sweepHit struct {
	pos    Vec3
	normal Vec3
	point  Vec3
	body   *Body
	hit    bool
}

type bodyShapeCandidate struct {
	body  *Body
	shape Shape
}

// sweepCapsuleHit sweeps the controller capsule from startPos to startPos + delta,
// stopping at the first contact whose normal opposes delta.
func (c *CharacterController) sweepCapsuleHit(world *World, startPos, delta Vec3, ignoreBody *Body) sweepHit {
	dist := delta.Len()
	if dist <= 1e-7 {
		return sweepHit{pos: startPos}
	}
	dir := delta.Scale(1 / dist)

	// Build a swept broadphase AABB to filter candidate bodies once.
	b0 := CapsuleShape(c.capsuleAt(startPos)).Bounds()
	b1 := CapsuleShape(c.capsuleAt(startPos.Add(delta))).Bounds()
	pad := Vec3{X: 1e-3, Y: 1e-3, Z: 1e-3}
	sweptBounds := BoxFromMinMax(
		Vec3{X: min(b0.Min().X, b1.Min().X), Y: min(b0.Min().Y, b1.Min().Y), Z: min(b0.Min().Z, b1.Min().Z)}.Sub(pad),
		Vec3{X: max(b0.Max().X, b1.Max().X), Y: max(b0.Max().Y, b1.Max().Y), Z: max(b0.Max().Z, b1.Max().Z)}.Add(pad),
	)

	mask := c.effectiveMask()
	var candidates []bodyShapeCandidate
	for _, b := range world.bodies {
		if b == ignoreBody || b.ID == c.IgnoreBodyID || b.IsTrigger {
			continue
		}
		if (effectiveLayer(b.Layer) & mask) == 0 {
			continue
		}
		ws := b.WorldShape()
		if aabbOverlap(sweptBounds, ws.Bounds()) {
			candidates = append(candidates, bodyShapeCandidate{body: b, shape: ws})
		}
	}
	var meshTris []Triangle
	for _, tm := range world.triMeshes {
		if (effectiveLayer(tm.Layer) & mask) == 0 {
			continue
		}
		if !aabbOverlap(sweptBounds, tm.Bounds) {
			continue
		}
		tm.QueryTriangles(sweptBounds, func(tri Triangle) {
			if aabbOverlap(sweptBounds, tri.Bounds()) {
				meshTris = append(meshTris, tri)
			}
		})
	}
	var hfTris []Triangle
	for _, hf := range world.heightfields {
		if (effectiveLayer(hf.Layer) & mask) == 0 {
			continue
		}
		if !aabbOverlap(sweptBounds, hf.Bounds) {
			continue
		}
		hf.QueryTriangles(sweptBounds, func(tri Triangle) {
			if aabbOverlap(sweptBounds, tri.Bounds()) {
				hfTris = append(hfTris, tri)
			}
		})
	}
	var voxelCols []*VoxelCollider
	for _, vc := range world.voxels {
		if (effectiveLayer(vc.Layer) & mask) == 0 {
			continue
		}
		voxelCols = append(voxelCols, vc)
	}
	if len(candidates) == 0 && len(meshTris) == 0 && len(hfTris) == 0 && len(voxelCols) == 0 {
		return sweepHit{pos: startPos.Add(delta)}
	}

	testOpposing := func(pos Vec3) (Contact, *Body, bool) {
		cap := c.capsuleAt(pos)
		capShape := CapsuleShape(cap)
		var best Contact
		var bestBody *Body
		found := false
		consider := func(ct Contact, b *Body) {
			if !ct.Hit {
				return
			}
			// Only block the sweep with surfaces facing against the motion.
			if ct.Normal.Dot(dir) >= -1e-4 {
				return
			}
			if !found || ct.Depth > best.Depth {
				best = ct
				bestBody = b
				found = true
			}
		}
		for _, cand := range candidates {
			consider(TestShapes(capShape, cand.shape), cand.body)
		}
		for _, tri := range meshTris {
			consider(TestCapsuleTriangle(cap, tri), nil)
		}
		for _, tri := range hfTris {
			consider(testShapeHeightfieldTriangle(capShape, tri), nil)
		}
		for _, vc := range voxelCols {
			vc.forEachContact(capShape, func(_, _, _ int, ct Contact) {
				consider(ct, nil)
			})
		}
		return best, bestBody, found
	}

	maxStep := max(c.Radius*0.25, 0.02)
	steps := int(math.Ceil(float64(dist / maxStep)))
	if steps < 1 {
		steps = 1
	}

	tPrev := float32(0)
	for i := 1; i <= steps; i++ {
		tCur := float32(i) / float32(steps)
		pos := startPos.Add(delta.Scale(tCur))
		if ct, body, hit := testOpposing(pos); hit {
			lo, hi := tPrev, tCur
			bestContact := ct
			bestBody := body
			for b := 0; b < 8; b++ {
				mid := (lo + hi) * 0.5
				if mct, mbody, mhit := testOpposing(startPos.Add(delta.Scale(mid))); mhit {
					hi = mid
					bestContact = mct
					bestBody = mbody
				} else {
					lo = mid
				}
			}
			return sweepHit{
				pos:    startPos.Add(delta.Scale(lo)),
				normal: bestContact.Normal,
				point:  bestContact.Point,
				body:   bestBody,
				hit:    true,
			}
		}
		tPrev = tCur
	}

	return sweepHit{pos: startPos.Add(delta)}
}

func (c *CharacterController) deepestOverlap(world *World, pos Vec3) (Contact, bool) {
	ct, _, ok := c.deepestOverlapBodyIgnoring(world, pos, nil)
	return ct, ok
}

// deepestOverlapBodyIgnoring returns the deepest contact at pos and the body it
// came from (or nil for a triangle mesh, heightfield, or voxel collider),
// optionally skipping ignoreBody.
func (c *CharacterController) deepestOverlapBodyIgnoring(world *World, pos Vec3, ignoreBody *Body) (Contact, *Body, bool) {
	capShape := CapsuleShape(c.capsuleAt(pos))
	capBounds := capShape.Bounds()
	mask := c.effectiveMask()
	var best Contact
	var bestBody *Body
	found := false
	for _, b := range world.bodies {
		if b == ignoreBody || b.ID == c.IgnoreBodyID || b.IsTrigger {
			continue
		}
		if (effectiveLayer(b.Layer) & mask) == 0 {
			continue
		}
		ws := b.WorldShape()
		if !aabbOverlap(capBounds, ws.Bounds()) {
			continue
		}
		if ct := TestShapes(capShape, ws); ct.Hit {
			if !found || ct.Depth > best.Depth {
				best = ct
				bestBody = b
				found = true
			}
		}
	}
	for _, tm := range world.triMeshes {
		if (effectiveLayer(tm.Layer) & mask) == 0 {
			continue
		}
		if !aabbOverlap(capBounds, tm.Bounds) {
			continue
		}
		if ct := tm.TestShape(capShape); ct.Hit {
			if !found || ct.Depth > best.Depth {
				best = ct
				bestBody = nil
				found = true
			}
		}
	}
	for _, hf := range world.heightfields {
		if (effectiveLayer(hf.Layer) & mask) == 0 {
			continue
		}
		if !aabbOverlap(capBounds, hf.Bounds) {
			continue
		}
		if ct := hf.TestShape(capShape); ct.Hit {
			if !found || ct.Depth > best.Depth {
				best = ct
				bestBody = nil
				found = true
			}
		}
	}
	for _, vc := range world.voxels {
		if (effectiveLayer(vc.Layer) & mask) == 0 {
			continue
		}
		if ct := vc.TestShape(capShape); ct.Hit {
			if !found || ct.Depth > best.Depth {
				best = ct
				bestBody = nil
				found = true
			}
		}
	}
	return best, bestBody, found
}

type overlapContact struct {
	contact Contact
	body    *Body
}

func (c *CharacterController) collectOverlaps(world *World, pos Vec3, skipWalkableBody *Body, minGroundY float32) []overlapContact {
	capShape := CapsuleShape(c.capsuleAt(pos))
	capBounds := capShape.Bounds()
	mask := c.effectiveMask()
	var out []overlapContact
	for _, b := range world.bodies {
		if b.ID == c.IgnoreBodyID || b.IsTrigger {
			continue
		}
		if (effectiveLayer(b.Layer) & mask) == 0 {
			continue
		}
		ws := b.WorldShape()
		if !aabbOverlap(capBounds, ws.Bounds()) {
			continue
		}
		if ct := TestShapes(capShape, ws); ct.Hit {
			if b == skipWalkableBody && ct.Normal.Y >= minGroundY {
				continue
			}
			out = append(out, overlapContact{contact: ct, body: b})
		}
	}
	for _, tm := range world.triMeshes {
		if (effectiveLayer(tm.Layer) & mask) == 0 {
			continue
		}
		if !aabbOverlap(capBounds, tm.Bounds) {
			continue
		}
		if ct := tm.TestShape(capShape); ct.Hit {
			out = append(out, overlapContact{contact: ct})
		}
	}
	for _, hf := range world.heightfields {
		if (effectiveLayer(hf.Layer) & mask) == 0 {
			continue
		}
		if !aabbOverlap(capBounds, hf.Bounds) {
			continue
		}
		if ct := hf.TestShape(capShape); ct.Hit {
			out = append(out, overlapContact{contact: ct})
		}
	}
	for _, vc := range world.voxels {
		if (effectiveLayer(vc.Layer) & mask) == 0 {
			continue
		}
		if ct := vc.TestShape(capShape); ct.Hit {
			out = append(out, overlapContact{contact: ct})
		}
	}
	return out
}

func (c *CharacterController) depenetrate(world *World, pos Vec3, minGroundY float32) Vec3 {
	for iter := 0; iter < 4; iter++ {
		ct, ok := c.deepestOverlap(world, pos)
		if !ok || ct.Depth <= 1e-6 {
			break
		}
		_ = minGroundY
		pos = pos.Add(ct.Normal.Scale(ct.Depth + contactSkin))
	}
	return pos
}

func (c *CharacterController) depenetrateAndCheckCrush(world *World, pos Vec3, minGroundY float32, skipWalkableBody *Body) (Vec3, bool) {
	type seenNormal struct {
		normal  Vec3
		movable bool
	}
	var seen []seenNormal
	addSeen := func(n Vec3, movable bool) {
		for i := range seen {
			if seen[i].normal.Dot(n) > 0.999 {
				if movable {
					seen[i].movable = true
				}
				return
			}
		}
		seen = append(seen, seenNormal{normal: n, movable: movable})
	}

	prevPos := pos
	var prevNormal Vec3
	hasPrevNormal := false
	for iter := 0; iter < maxDepenIters; iter++ {
		overlaps := c.collectOverlaps(world, pos, skipWalkableBody, minGroundY)
		if len(overlaps) == 0 {
			return pos, false
		}
		bestIdx := 0
		for i := range overlaps {
			if overlaps[i].contact.Depth > crushDepthEps {
				addSeen(overlaps[i].contact.Normal, isMovableBody(overlaps[i].body))
			}
			if overlaps[i].contact.Depth > overlaps[bestIdx].contact.Depth {
				bestIdx = i
			}
		}
		best := overlaps[bestIdx].contact
		if best.Depth <= 1e-6 {
			return pos, false
		}
		prevPos = pos
		push := best.Normal.Scale(best.Depth + contactSkin)
		if hasPrevNormal {
			dot := best.Normal.Dot(prevNormal)
			if dot < -0.1 && dot > -0.9 {
				// Exact 2-plane wedge escape: move perpendicular to prevNormal
				// while clearing best.Normal in a single step instead of oscillating.
				t := projectOnPlane(best.Normal, prevNormal)
				if tLenSq := t.LenSq(); tLenSq > 0.15 {
					push = t.Scale((best.Depth + contactSkin) / tLenSq)
				}
			}
		}
		pos = pos.Add(push)
		prevNormal = best.Normal
		hasPrevNormal = true
	}

	// Re-check residual overlaps after exhausting depenetration iterations.
	rem := c.collectOverlaps(world, pos, skipWalkableBody, minGroundY)
	maxDepth := float32(0)
	for _, ov := range rem {
		if ov.contact.Depth > maxDepth {
			maxDepth = ov.contact.Depth
		}
		if ov.contact.Depth > crushDepthEps {
			addSeen(ov.contact.Normal, isMovableBody(ov.body))
		}
	}
	if maxDepth <= crushDepthEps {
		return pos, false
	}

	crushed := false
	for i := 0; i < len(seen) && !crushed; i++ {
		for j := i + 1; j < len(seen); j++ {
			if (seen[i].movable || seen[j].movable) && seen[i].normal.Dot(seen[j].normal) < crushOpposingDot {
				crushed = true
				break
			}
		}
	}
	if crushed {
		// Leave the capsule centered between the opposing surfaces rather than
		// oscillating onto one side.
		mid := pos.Add(prevPos).Scale(0.5)
		midOverlaps := c.collectOverlaps(world, mid, skipWalkableBody, minGroundY)
		midMax := float32(0)
		for _, ov := range midOverlaps {
			if ov.contact.Depth > midMax {
				midMax = ov.contact.Depth
			}
		}
		if midMax <= maxDepth {
			pos = mid
		}
	}
	return pos, crushed
}

func (c *CharacterController) depenetrateHorizontalIgnoring(world *World, pos Vec3, minGroundY float32, ignoreBody *Body) Vec3 {
	for iter := 0; iter < 4; iter++ {
		ct, _, ok := c.deepestOverlapBodyIgnoring(world, pos, ignoreBody)
		if !ok || ct.Depth <= 1e-6 {
			break
		}
		n := ct.Normal
		if n.Y >= minGroundY || n.Y <= ceilingNormalY {
			pos = pos.Add(n.Scale(ct.Depth + contactSkin))
		} else {
			wallN := Vec3{X: n.X, Z: n.Z}.Norm()
			if wallN.LenSq() == 0 {
				pos = pos.Add(n.Scale(ct.Depth + contactSkin))
			} else {
				denom := wallN.Dot(n)
				push := ct.Depth + contactSkin
				if denom > 0.1 {
					push = (ct.Depth + contactSkin) / denom
				}
				pos = pos.Add(wallN.Scale(push))
			}
		}
	}
	return pos
}

func (c *CharacterController) bodyInWorld(world *World, target *Body) bool {
	if target == nil || target.ID == c.IgnoreBodyID || target.IsTrigger {
		return false
	}
	if (effectiveLayer(target.Layer) & c.effectiveMask()) == 0 {
		return false
	}
	for _, b := range world.bodies {
		if b == target {
			return true
		}
	}
	return false
}

func (c *CharacterController) groundBodyMotion(b *Body, contactPoint Vec3, dtSec float32, hadPrev bool, prevPos Vec3, prevRot Quat) (disp Vec3, yawDelta float32, vel Vec3) {
	if b == nil || !isMovableBody(b) {
		return Vec3{}, 0, Vec3{}
	}
	curPos := b.Pos
	curRot := normalizedQuat(b.Rot)

	if !hadPrev {
		if kp, ok := c.kinematicPoses[b.ID]; ok && kp.body == b {
			prevPos = kp.pos
			prevRot = kp.rot
			hadPrev = true
		}
	}

	if hadPrev && (curPos != prevPos || curRot != prevRot) {
		deltaRot := curRot.Mul(prevRot.Conjugate()).Norm()
		rPrev := contactPoint.Sub(prevPos)
		targetPos := curPos.Add(deltaRot.RotateVec(rPrev))
		disp = targetPos.Sub(contactPoint)
		yawDelta = quatYawDelta(deltaRot)
		if !b.Kinematic {
			vel = pointVel(b, targetPos.Sub(curPos))
		} else {
			linVel := curPos.Sub(prevPos).Scale(1 / dtSec)
			axis, angle := deltaRot.ToAxisAngle()
			angVel := axis.Scale(angle / dtSec)
			vel = linVel.Add(angVel.Cross(targetPos.Sub(curPos)))
		}
		return disp, yawDelta, vel
	}

	// Fallback for stationary conveyor-style kinematic bodies driven solely by
	// Vel/AngVel, or dynamic bodies whose velocity is non-zero before World.Step.
	if b.Vel.LenSq() > 0 || b.AngVel.LenSq() > 0 {
		r := contactPoint.Sub(curPos)
		vel = pointVel(b, r)
		if b.Kinematic {
			if b.AngVel.LenSq() > 1e-12 {
				dq := IntegrateQuat(QuatIdentity(), b.AngVel, dtSec)
				yawDelta = quatYawDelta(dq)
				disp = b.Vel.Scale(dtSec).Add(dq.RotateVec(r).Sub(r))
			} else {
				disp = b.Vel.Scale(dtSec)
			}
		}
		return disp, yawDelta, vel
	}
	return Vec3{}, 0, Vec3{}
}

func (c *CharacterController) groundBodyVelocityAt(b *Body, contactPoint Vec3, dtSec float32, usePrev bool, prevPos Vec3, prevRot Quat) Vec3 {
	if b == nil || !isMovableBody(b) {
		return Vec3{}
	}
	if !b.Kinematic {
		return pointVel(b, contactPoint.Sub(b.Pos))
	}
	curPos := b.Pos
	curRot := normalizedQuat(b.Rot)
	if !usePrev {
		if kp, ok := c.kinematicPoses[b.ID]; ok && kp.body == b {
			prevPos = kp.pos
			prevRot = kp.rot
			usePrev = true
		}
	}
	if usePrev && (curPos != prevPos || curRot != prevRot) {
		deltaRot := curRot.Mul(prevRot.Conjugate()).Norm()
		linVel := curPos.Sub(prevPos).Scale(1 / dtSec)
		axis, angle := deltaRot.ToAxisAngle()
		angVel := axis.Scale(angle / dtSec)
		return linVel.Add(angVel.Cross(contactPoint.Sub(curPos)))
	}
	if b.Vel.LenSq() > 0 || b.AngVel.LenSq() > 0 {
		return pointVel(b, contactPoint.Sub(curPos))
	}
	return Vec3{}
}

func (c *CharacterController) applyCarryDisplacement(world *World, groundBody *Body, disp Vec3, minGroundY float32) {
	// Fast path: unobstructed straight carry with zero splitting error.
	direct := c.sweepCapsuleHit(world, c.Pos, disp, groundBody)
	if !direct.hit {
		c.Pos = direct.pos
		return
	}

	// Obstructed carry: resolve horizontal and vertical components cleanly
	// against non-ground obstacles so walls scrape and ceilings stop the lift.
	horiz := Vec3{X: disp.X, Z: disp.Z}
	vert := Vec3{Y: disp.Y}
	doVert := func() {
		if abs32(vert.Y) <= 1e-7 {
			return
		}
		vh := c.sweepCapsuleHit(world, c.Pos, vert, groundBody)
		c.Pos = vh.pos
		if vh.hit {
			if vh.normal.Y <= ceilingNormalY {
				c.OnCeiling = true
			} else if vh.normal.Y < minGroundY {
				c.OnWall = true
			}
		}
	}
	doHoriz := func() {
		if horiz.LenSq() <= 1e-12 {
			return
		}
		hRes := c.slideHorizontal(world, c.Pos, horiz, minGroundY, groundBody)
		c.Pos = hRes.pos
		c.OnWall = c.OnWall || hRes.hitWall
		c.OnCeiling = c.OnCeiling || hRes.hitCeiling
	}
	if disp.Y > 0 {
		doVert()
		doHoriz()
	} else {
		doHoriz()
		doVert()
	}
}

func (c *CharacterController) resolveKinematicIntrusions(world *World, skipBody *Body, minGroundY float32) {
	if len(c.kinematicPoses) == 0 {
		return
	}
	mask := c.effectiveMask()
	stepSize := max(c.Radius*0.35, 0.02)
	pad := Vec3{X: 1e-3, Y: 1e-3, Z: 1e-3}

	for _, b := range world.bodies {
		if !b.Kinematic || b == skipBody || b.ID == c.IgnoreBodyID || b.IsTrigger {
			continue
		}
		if (effectiveLayer(b.Layer) & mask) == 0 {
			continue
		}
		prev, ok := c.kinematicPoses[b.ID]
		if !ok || prev.body != b {
			continue
		}
		curPos := b.Pos
		curRot := normalizedQuat(b.Rot)
		if curPos == prev.pos && curRot == prev.rot {
			continue
		}

		// Swept AABB broadphase reject.
		b0 := b.Shape.Transformed(prev.pos, prev.rot).Bounds()
		b1 := b.WorldShape().Bounds()
		swept := BoxFromMinMax(
			Vec3{X: min(b0.Min().X, b1.Min().X), Y: min(b0.Min().Y, b1.Min().Y), Z: min(b0.Min().Z, b1.Min().Z)}.Sub(pad),
			Vec3{X: max(b0.Max().X, b1.Max().X), Y: max(b0.Max().Y, b1.Max().Y), Z: max(b0.Max().Z, b1.Max().Z)}.Add(pad),
		)
		if !aabbOverlap(swept, CapsuleShape(c.capsuleAt(c.Pos)).Bounds()) {
			continue
		}

		linDist := curPos.Sub(prev.pos).Len()
		angDist := float32(prev.rot.AngleTo(curRot) * maxShapeExtent(b.Shape))
		travel := linDist + angDist
		steps := int(math.Ceil(float64(travel / stepSize)))
		if steps < 1 {
			steps = 1
		}
		if steps > maxKinematicSubSteps {
			steps = maxKinematicSubSteps
		}

		for s := 1; s <= steps; s++ {
			t := float32(s) / float32(steps)
			posT := lerpVec3(prev.pos, curPos, t)
			rotT := prev.rot.Slerp(curRot, t)
			shapeT := b.Shape.Transformed(posT, rotT)

			ct := TestShapes(CapsuleShape(c.capsuleAt(c.Pos)), shapeT)
			if !ct.Hit || ct.Depth <= 1e-6 {
				continue
			}
			if ct.Normal.Y <= ceilingNormalY {
				c.OnCeiling = true
			} else if ct.Normal.Y < minGroundY {
				c.OnWall = true
			}

			push := ct.Normal.Scale(ct.Depth + contactSkin)
			target := c.Pos.Add(push)
			hit := c.sweepCapsuleHit(world, c.Pos, push, b)
			c.Pos = hit.pos
			if hit.hit {
				if hit.normal.Y <= ceilingNormalY {
					c.OnCeiling = true
				} else if hit.normal.Y < minGroundY {
					c.OnWall = true
				}
				// If the obstructing surface is angled (not head-on opposing),
				// slide along its plane by the exact distance needed to clear ct.Normal.
				if dot := hit.normal.Dot(ct.Normal); dot > -0.9 {
					rem := target.Sub(hit.pos)
					remDepth := rem.Dot(ct.Normal)
					t := projectOnPlane(ct.Normal, hit.normal)
					if remDepth > 1e-6 && t.LenSq() > 0.15 {
						escape := t.Scale(remDepth / t.LenSq())
						sHit := c.sweepCapsuleHit(world, c.Pos, escape, b)
						c.Pos = sHit.pos
					}
				}
			}
		}
	}
}

func (c *CharacterController) recordKinematicPoses(world *World) {
	if c.kinematicPoses == nil {
		hasAny := false
		for _, b := range world.bodies {
			if b.Kinematic {
				hasAny = true
				break
			}
		}
		if !hasAny {
			return
		}
		c.kinematicPoses = make(map[int]kinematicPose)
	}
	// Rebuild deterministically (no map iteration) if stale entries accumulate.
	if len(c.kinematicPoses) > len(world.bodies)*2+8 {
		c.kinematicPoses = make(map[int]kinematicPose, len(world.bodies))
	}
	for _, b := range world.bodies {
		if !b.Kinematic {
			continue
		}
		c.kinematicPoses[b.ID] = kinematicPose{
			body: b,
			pos:  b.Pos,
			rot:  normalizedQuat(b.Rot),
		}
	}
}

func isMovableBody(b *Body) bool {
	return b != nil && (b.Kinematic || !b.Static)
}

func platformLeaveVelocity(groundVel Vec3, mode PlatformOnLeave) Vec3 {
	switch mode {
	case PlatformLeaveDoNothing:
		return Vec3{}
	case PlatformLeaveAddUpwardVelocity:
		if groundVel.Y < 0 {
			groundVel.Y = 0
		}
		return groundVel
	default:
		return groundVel
	}
}

func quatYawDelta(dq Quat) float32 {
	u := normalizedQuat(dq)
	if u.W < 0 {
		u = Quat{X: -u.X, Y: -u.Y, Z: -u.Z, W: -u.W}
	}
	if abs32(u.Y) <= 1e-8 && abs32(u.W) <= 1e-8 {
		return 0
	}
	return float32(2 * detmath.Atan2(float64(u.Y), float64(u.W)))
}

func maxShapeExtent(s Shape) float32 {
	switch s.Kind {
	case ShapeBox:
		return s.Box.Center.Len() + s.Box.HalfExtents.Len()
	case ShapeSphere:
		return s.Sphere.Center.Len() + s.Sphere.Radius
	case ShapeCapsule:
		return max(s.Capsule.A.Len(), s.Capsule.B.Len()) + s.Capsule.Radius
	default:
		return 0
	}
}

func projectOnPlane(v, n Vec3) Vec3 {
	return v.Sub(n.Scale(v.Dot(n)))
}
