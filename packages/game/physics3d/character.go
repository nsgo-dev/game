package physics3d

import (
	"math"
	"time"
)

const (
	defaultMaxSlopeAngle = float32(45 * math.Pi / 180)
	defaultStepHeight    = 0.35
	ceilingNormalY       = -0.7
	contactSkin          = 1e-4
)

// CharacterController is a kinematic capsule collider that moves through a
// World with walkable slope sliding, automatic stair stepping, headroom checks,
// and tangential wall sliding.
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
	// Grounded reports whether the controller ended the last Move on walkable ground.
	Grounded bool
	// GroundNormal is the outward surface normal of the ground when Grounded is true.
	GroundNormal Vec3
	// OnWall reports whether a non-walkable wall or steep slope was contacted.
	OnWall bool
	// OnCeiling reports whether a ceiling surface was contacted.
	OnCeiling bool
	// IgnoreBodyID optionally ignores a body with this ID during movement queries.
	IgnoreBodyID int
	// Mask filters which body and static collider layers block the character
	// (0 defaults to LayerAll).
	Mask LayerMask
	// PushForce is the horizontal force in newtons the character applies to a dynamic body it walks into, so crates and barrels give way instead of acting as walls. 0 leaves dynamic bodies blocking the character like static ones.
	//
	// The controller is not a body in the World, so dynamic bodies do not collide with it on their own; to let them, add a kinematic body that follows Pos and set IgnoreBodyID to its ID (#546).
	PushForce float32

	pushes []characterPush
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
// capsule radius and height, a default StepHeight of 0.35, and a 45-degree
// MaxSlopeAngle.
func NewCharacterController(pos Vec3, radius, height float32) *CharacterController {
	if height < 2*radius {
		height = 2 * radius
	}
	return &CharacterController{
		Pos:           pos,
		Radius:        radius,
		Height:        height,
		StepHeight:    defaultStepHeight,
		MaxSlopeAngle: defaultMaxSlopeAngle,
	}
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
	Pos          Vec3
	Grounded     bool
	GroundNormal Vec3
	OnWall       bool
	OnCeiling    bool
	SteppedUp    bool
}

// Move advances the character through world for dt.
//
// wishMove is the desired velocity in units per second: X and Z drive
// horizontal movement, while a non-zero Y sets Vel.Y directly (such as for a
// jump) and Y == 0 lets world.Gravity govern vertical motion.
func (c *CharacterController) Move(world *World, wishMove Vec3, dt time.Duration) MoveResult {
	if world == nil || dt <= 0 || c.Radius <= 0 {
		return MoveResult{
			Pos:          c.Pos,
			Grounded:     c.Grounded,
			GroundNormal: c.GroundNormal,
			OnWall:       c.OnWall,
			OnCeiling:    c.OnCeiling,
		}
	}

	dtSec := float32(dt.Seconds())
	c.pushes = c.pushes[:0]
	maxSlope := c.maxSlopeAngle()
	minGroundY := float32(math.Cos(float64(maxSlope)))

	// Clear any minor initial overlap before moving.
	c.Pos = c.depenetrate(world, c.Pos, minGroundY)

	wasGrounded := c.Grounded
	prevGroundNormal := c.GroundNormal
	c.OnWall = false
	c.OnCeiling = false
	steppedUp := false

	// Prepare velocity for this tick.
	c.Vel.X = wishMove.X
	c.Vel.Z = wishMove.Z
	jumping := false
	if wishMove.Y != 0 {
		c.Vel.Y = wishMove.Y
		if wishMove.Y > 0 {
			jumping = true
		}
	} else if c.Vel.Y > 0 {
		jumping = true
	}

	c.Vel = c.Vel.Add(world.Gravity.Scale(dtSec))
	if wasGrounded && !jumping && c.Vel.Y < 0 {
		// Prevent unbounded negative velocity accumulation while walking on ground.
		c.Vel.Y = world.Gravity.Y * dtSec
	}

	// Phase 1: Horizontal move with walkable-slope gliding and stair step-up.
	horizDisp := Vec3{X: c.Vel.X * dtSec, Z: c.Vel.Z * dtSec}
	horizDist := horizDisp.Len()
	startPos := c.Pos
	horizGrounded := false
	var horizGroundNormal Vec3

	if horizDist > 1e-6 {
		moveDisp := horizDisp
		// When grounded on a walkable slope and moving uphill, align the move
		// with the slope plane so the controller climbs smoothly at steady speed.
		if wasGrounded && !jumping && prevGroundNormal.Y >= minGroundY && prevGroundNormal.Y < 0.9999 {
			if horizDisp.Dot(prevGroundNormal) < 0 {
				dy := -(horizDisp.X*prevGroundNormal.X + horizDisp.Z*prevGroundNormal.Z) / prevGroundNormal.Y
				moveDisp = Vec3{X: horizDisp.X, Y: dy, Z: horizDisp.Z}.Norm().Scale(horizDist)
			}
		}

		normalRes := c.slideHorizontal(world, startPos, moveDisp, minGroundY)
		// Pushes recorded so far belong to the plain slide; the step-up probe appends its own after them, and only the path Move commits keeps its pushes, so a body the probe merely touched, or one the character steps onto, is not shoved (#546).
		normalPushes := len(c.pushes)

		// Attempt automatic stair step-up when blocked by a wall while grounded.
		didStep := false
		if normalRes.hitWall && c.StepHeight > 0 && (wasGrounded || normalRes.hitGround) && !jumping {
			normalHoriz := Vec3{X: normalRes.pos.X - startPos.X, Z: normalRes.pos.Z - startPos.Z}
			if stepPos, landNormal, ok := c.tryStepUp(world, startPos, horizDisp, normalHoriz.LenSq(), minGroundY); ok {
				c.pushes = append(c.pushes[:0], c.pushes[normalPushes:]...)
				c.Pos = stepPos
				steppedUp = true
				didStep = true
				c.Grounded = true
				c.GroundNormal = landNormal
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
			if normalRes.hitGround {
				horizGrounded = true
				horizGroundNormal = normalRes.groundNormal
			}
		}
	}

	// Phase 2: Vertical motion (jump/gravity/steep-slope slide) and ground snap.
	if steppedUp {
		// Step-up already landed cleanly on a verified walkable tread.
	} else if c.Vel.Y > 0 {
		// Rising (e.g., jumping).
		upDisp := Vec3{Y: c.Vel.Y * dtSec}
		hitPos, hitNormal, hit := c.sweepCapsule(world, c.Pos, upDisp)
		c.Pos = hitPos
		c.Grounded = false
		c.GroundNormal = Vec3{}
		if hit {
			if hitNormal.Y <= ceilingNormalY {
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
			slopeDrop := horizDist*float32(math.Tan(float64(maxSlope))) + 0.05
			snapDist := max(c.StepHeight+0.05, slopeDrop)
			if snapPos, snapNormal, hit := c.sweepCapsule(world, c.Pos, Vec3{Y: -snapDist}); hit && snapNormal.Y >= minGroundY {
				c.Pos = snapPos
				c.Grounded = true
				c.GroundNormal = snapNormal
				c.Vel.Y = 0
				snapped = true
			}
		}

		if !snapped {
			gravDisp := Vec3{Y: c.Vel.Y * dtSec}
			c.slideVertical(world, gravDisp, dtSec, minGroundY)
			if !c.Grounded && horizGrounded {
				// Keep ground contact if we just moved onto a walkable surface
				// and the downward sweep did not hit anything lower.
				if probePos, probeNormal, hit := c.sweepCapsule(world, c.Pos, Vec3{Y: -0.02}); hit && probeNormal.Y >= minGroundY {
					c.Pos = probePos
					c.Grounded = true
					c.GroundNormal = probeNormal
					c.Vel.Y = 0
				} else {
					_ = horizGroundNormal
				}
			}
		}
	}

	if c.PushForce > 0 {
		for _, push := range c.pushes {
			push.body.ApplyImpulse(push.normal.Neg().Scale(c.PushForce * dtSec))
		}
	}
	clear(c.pushes)
	c.pushes = c.pushes[:0]

	return MoveResult{
		Pos:          c.Pos,
		Grounded:     c.Grounded,
		GroundNormal: c.GroundNormal,
		OnWall:       c.OnWall,
		OnCeiling:    c.OnCeiling,
		SteppedUp:    steppedUp,
	}
}

type slideResult struct {
	pos          Vec3
	hitGround    bool
	groundNormal Vec3
	hitWall      bool
	hitCeiling   bool
}

// slideHorizontal moves from startPos by disp, gliding up walkable slopes and
// sliding tangentially along walls and steep slopes without gaining height on
// non-walkable surfaces.
func (c *CharacterController) slideHorizontal(world *World, startPos, disp Vec3, minGroundY float32) slideResult {
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
			contact, body, ok := c.deepestOverlapBody(world, candPos)
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
				res.groundNormal = n
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
		res.pos = c.depenetrateHorizontal(world, res.pos, minGroundY)
	}

	return res
}

// tryStepUp attempts to climb a stair up to StepHeight from startPos along
// horizDisp, verifying headroom, greater horizontal progress than the blocked
// move, and a walkable landing surface.
func (c *CharacterController) tryStepUp(world *World, startPos, horizDisp Vec3, normalDistSq, minGroundY float32) (Vec3, Vec3, bool) {
	// 1. Check headroom by sweeping straight up by StepHeight.
	upPos, _, _ := c.sweepCapsule(world, startPos, Vec3{Y: c.StepHeight})
	actualUp := upPos.Y - startPos.Y
	if actualUp <= 1e-3 {
		return Vec3{}, Vec3{}, false
	}

	// 2. Advance horizontally at the raised height.
	fwdRes := c.slideHorizontal(world, upPos, horizDisp, minGroundY)
	stepHoriz := Vec3{X: fwdRes.pos.X - startPos.X, Z: fwdRes.pos.Z - startPos.Z}
	if stepHoriz.LenSq() <= normalDistSq+1e-6 {
		return Vec3{}, Vec3{}, false
	}

	// 3. Sweep back down to land on the step tread.
	landPos, landNormal, hit := c.sweepCapsule(world, fwdRes.pos, Vec3{Y: -(actualUp + 0.02)})
	if !hit {
		return Vec3{}, Vec3{}, false
	}
	if landPos.Y > startPos.Y+c.StepHeight+1e-3 || landPos.Y < startPos.Y-0.02 {
		return Vec3{}, Vec3{}, false
	}

	// Verify the landing surface is truly walkable (not a steep slope).
	// A rounded capsule touching a flat stair corner can report an angled corner
	// normal even though the step tread itself is flat, so when landNormal is
	// angled we probe the tread directly ahead with a downward ray.
	groundNormal := landNormal
	if landNormal.Y < minGroundY {
		horizDir := Vec3{X: horizDisp.X, Z: horizDisp.Z}.Norm()
		rayStart := Vec3{
			X: fwdRes.pos.X + horizDir.X*c.Radius,
			Y: upPos.Y + 0.01,
			Z: fwdRes.pos.Z + horizDir.Z*c.Radius,
		}
		rayEnd := Vec3{X: rayStart.X, Y: startPos.Y - 0.05, Z: rayStart.Z}
		rayHit, ok := c.raycastFirst(world, rayStart, rayEnd)
		if !ok || rayHit.T <= 0 || rayHit.Normal.Y < minGroundY || rayHit.Point.Y > startPos.Y+c.StepHeight+1e-3 {
			return Vec3{}, Vec3{}, false
		}
		groundNormal = rayHit.Normal
	}

	if _, overlap := c.deepestOverlap(world, landPos); overlap {
		landPos = c.depenetrate(world, landPos, minGroundY)
		if _, still := c.deepestOverlap(world, landPos); still {
			return Vec3{}, Vec3{}, false
		}
	}

	return landPos, groundNormal, true
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
func (c *CharacterController) slideVertical(world *World, disp Vec3, dtSec, minGroundY float32) {
	rem := disp
	c.Grounded = false
	c.GroundNormal = Vec3{}

	for iter := 0; iter < 3; iter++ {
		if rem.LenSq() <= 1e-12 {
			break
		}
		hitPos, hitNormal, hit := c.sweepCapsule(world, c.Pos, rem)
		moved := hitPos.Sub(c.Pos)
		c.Pos = hitPos
		if !hit {
			break
		}

		if hitNormal.Y >= minGroundY {
			c.Grounded = true
			c.GroundNormal = hitNormal
			c.Vel.Y = 0
			break
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
				sinSq := 1 - hitNormal.Y*hitNormal.Y
				if moved.LenSq() > 1e-6 {
					// Initial impact from free-fall: project vertical velocity onto slope once.
					c.Vel.Y *= sinSq
				} else {
					// Already sliding on the slope: cancel only the normal component of
					// this tick's gravity so velocity accelerates smoothly downhill.
					c.Vel.Y -= world.Gravity.Y * hitNormal.Y * hitNormal.Y * dtSec
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
}

// sweepCapsule sweeps the controller capsule from startPos to startPos + delta,
// stopping at the first contact whose normal opposes delta.
func (c *CharacterController) sweepCapsule(world *World, startPos, delta Vec3) (Vec3, Vec3, bool) {
	dist := delta.Len()
	if dist <= 1e-7 {
		return startPos, Vec3{}, false
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
	var candidates []Shape
	for _, b := range world.bodies {
		if b.ID == c.IgnoreBodyID || b.IsTrigger {
			continue
		}
		if (effectiveLayer(b.Layer) & mask) == 0 {
			continue
		}
		ws := b.WorldShape()
		if aabbOverlap(sweptBounds, ws.Bounds()) {
			candidates = append(candidates, ws)
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
		return startPos.Add(delta), Vec3{}, false
	}

	testOpposing := func(pos Vec3) (Contact, bool) {
		cap := c.capsuleAt(pos)
		capShape := CapsuleShape(cap)
		var best Contact
		found := false
		consider := func(ct Contact) {
			if !ct.Hit {
				return
			}
			// Only block the sweep with surfaces facing against the motion.
			if ct.Normal.Dot(dir) >= -1e-4 {
				return
			}
			if !found || ct.Depth > best.Depth {
				best = ct
				found = true
			}
		}
		for _, ws := range candidates {
			consider(TestShapes(capShape, ws))
		}
		for _, tri := range meshTris {
			consider(TestCapsuleTriangle(cap, tri))
		}
		for _, tri := range hfTris {
			consider(testShapeHeightfieldTriangle(capShape, tri))
		}
		for _, vc := range voxelCols {
			vc.forEachContact(capShape, func(_, _, _ int, ct Contact) {
				consider(ct)
			})
		}
		return best, found
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
		if ct, hit := testOpposing(pos); hit {
			lo, hi := tPrev, tCur
			bestContact := ct
			for b := 0; b < 8; b++ {
				mid := (lo + hi) * 0.5
				if mct, mhit := testOpposing(startPos.Add(delta.Scale(mid))); mhit {
					hi = mid
					bestContact = mct
				} else {
					lo = mid
				}
			}
			return startPos.Add(delta.Scale(lo)), bestContact.Normal, true
		}
		tPrev = tCur
	}

	return startPos.Add(delta), Vec3{}, false
}

func (c *CharacterController) deepestOverlap(world *World, pos Vec3) (Contact, bool) {
	ct, _, ok := c.deepestOverlapBody(world, pos)
	return ct, ok
}

// deepestOverlapBody returns the deepest contact at pos and the body it came from, or nil for a triangle mesh or heightfield.
func (c *CharacterController) deepestOverlapBody(world *World, pos Vec3) (Contact, *Body, bool) {
	capShape := CapsuleShape(c.capsuleAt(pos))
	capBounds := capShape.Bounds()
	mask := c.effectiveMask()
	var best Contact
	var bestBody *Body
	found := false
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

func (c *CharacterController) depenetrateHorizontal(world *World, pos Vec3, minGroundY float32) Vec3 {
	for iter := 0; iter < 4; iter++ {
		ct, ok := c.deepestOverlap(world, pos)
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

func projectOnPlane(v, n Vec3) Vec3 {
	return v.Sub(n.Scale(v.Dot(n)))
}
