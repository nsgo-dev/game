package scene3d

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

// ProjectionKind selects perspective or orthographic camera projection.
type ProjectionKind uint8

const (
	// ProjectionPerspective projects a perspective view frustum.
	ProjectionPerspective ProjectionKind = iota
	// ProjectionOrthographic projects an orthographic parallel view volume.
	ProjectionOrthographic
)

// Projection configures a Camera's projection matrix.
type Projection struct {
	Kind        ProjectionKind
	FOVY        float32 // Vertical field of view in radians (ProjectionPerspective)
	OrthoHeight float32 // Full vertical world height (ProjectionOrthographic)
	Aspect      float32 // Viewport width / height
	Near        float32 // Near clip plane distance
	Far         float32 // Far clip plane distance
}

// PerspectiveProjection returns a perspective Projection configuration.
func PerspectiveProjection(fovY, aspect, near, far float32) Projection {
	return Projection{
		Kind:   ProjectionPerspective,
		FOVY:   fovY,
		Aspect: aspect,
		Near:   near,
		Far:    far,
	}
}

// OrthographicProjection returns an orthographic Projection configuration.
func OrthographicProjection(orthoHeight, aspect, near, far float32) Projection {
	return Projection{
		Kind:        ProjectionOrthographic,
		OrthoHeight: orthoHeight,
		Aspect:      aspect,
		Near:        near,
		Far:         far,
	}
}

// Matrix builds the column-major projection matrix for p using [0, 1] NDC depth.
func (p Projection) Matrix() math3d.Mat4 {
	aspect := p.Aspect
	if aspect <= 0 {
		aspect = 1
	}
	switch p.Kind {
	case ProjectionOrthographic:
		h := p.OrthoHeight
		if h <= 0 {
			h = 2
		}
		near, far := p.Near, p.Far
		if near == 0 && far == 0 {
			near, far = 0.1, 1000
		} else if far <= near {
			far = near + 1000
		}
		halfH := h * 0.5
		halfW := halfH * aspect
		return math3d.Ortho(-halfW, halfW, -halfH, halfH, near, far)
	default:
		fovY := p.FOVY
		if fovY <= 0 {
			fovY = math3d.DegToRad(60)
		}
		near := p.Near
		if near <= 0 {
			near = 0.1
		}
		far := p.Far
		if far <= near {
			far = near + 1000
		}
		return math3d.Perspective(fovY, aspect, near, far)
	}
}

// Camera defines a 3D viewpoint and projection.
//
// In right-handed Y-up space, an identity Rot looks down -Z with +X right and
// +Y up.
type Camera struct {
	Pos        math3d.Vec3
	Rot        math3d.Quat
	Projection Projection
}

// NewPerspectiveCamera creates a perspective Camera looking down -Z.
func NewPerspectiveCamera(pos math3d.Vec3, fovY, aspect, near, far float32) *Camera {
	return &Camera{
		Pos:        pos,
		Rot:        math3d.QuatIdentity(),
		Projection: PerspectiveProjection(fovY, aspect, near, far),
	}
}

// NewOrthographicCamera creates an orthographic Camera looking down -Z.
func NewOrthographicCamera(pos math3d.Vec3, orthoHeight, aspect, near, far float32) *Camera {
	return &Camera{
		Pos:        pos,
		Rot:        math3d.QuatIdentity(),
		Projection: OrthographicProjection(orthoHeight, aspect, near, far),
	}
}

// LookAt orients the camera at its current Pos toward target with world up vector up.
func (c *Camera) LookAt(target, up math3d.Vec3) {
	c.Rot = math3d.LookRotation(target.Sub(c.Pos), up)
}

// SyncFromNode copies world position and rotation from node into c.
func (c *Camera) SyncFromNode(node *Node) {
	if node == nil {
		return
	}
	wt := node.WorldTransform()
	c.Pos = wt.Pos
	c.Rot = wt.Rot
}

// Forward returns the camera's unit world-space forward direction (-Z in local space).
func (c *Camera) Forward() math3d.Vec3 {
	return c.Rot.Norm().RotateVec3(math3d.Vec3Forward())
}

// Right returns the camera's unit world-space right direction (+X in local space).
func (c *Camera) Right() math3d.Vec3 {
	return c.Rot.Norm().RotateVec3(math3d.Vec3Right())
}

// Up returns the camera's unit world-space up direction (+Y in local space).
func (c *Camera) Up() math3d.Vec3 {
	return c.Rot.Norm().RotateVec3(math3d.Vec3Up())
}

// ViewMatrix returns the right-handed view matrix transforming world space into
// camera view space (inverse of the camera's rigid world transform).
func (c *Camera) ViewMatrix() math3d.Mat4 {
	q := c.Rot.Norm()
	r := q.RotateVec3(math3d.Vec3Right())
	u := q.RotateVec3(math3d.Vec3Up())
	b := q.RotateVec3(math3d.Vec3UnitZ()) // backward (-forward)
	return math3d.Mat4{
		r.X, u.X, b.X, 0,
		r.Y, u.Y, b.Y, 0,
		r.Z, u.Z, b.Z, 0,
		-r.Dot(c.Pos), -u.Dot(c.Pos), -b.Dot(c.Pos), 1,
	}
}

// ProjMatrix returns the camera's projection matrix.
func (c *Camera) ProjMatrix() math3d.Mat4 {
	return c.Projection.Matrix()
}

// ViewProj returns the combined projection-view matrix (Proj * View).
func (c *Camera) ViewProj() math3d.Mat4 {
	return c.ProjMatrix().Mul(c.ViewMatrix())
}

// Frustum extracts the camera's 6-plane view frustum in world space.
func (c *Camera) Frustum() math3d.Frustum {
	return math3d.FrustumFromMat4(c.ViewProj())
}

// ScreenRay unprojects pixel coordinates (screenX, screenY) in a viewport of
// size (viewW, viewH) through the inverse ViewProj matrix into a world-space
// Ray with unit direction.
//
// Screen coordinates use (0, 0) at top-left and (viewW, viewH) at bottom-right.
// Works identically for both perspective and orthographic cameras.
func (c *Camera) ScreenRay(screenX, screenY, viewW, viewH float32) math3d.Ray {
	if viewW <= 0 || viewH <= 0 {
		return math3d.Ray{Origin: c.Pos, Dir: c.Forward()}
	}
	invVP, ok := c.ViewProj().Inverse()
	if !ok {
		return math3d.Ray{Origin: c.Pos, Dir: c.Forward()}
	}
	ndcX := (2*screenX)/viewW - 1
	ndcY := 1 - (2*screenY)/viewH
	nearPt := invVP.TransformPoint(math3d.Vec3{X: ndcX, Y: ndcY, Z: 0})
	farPt := invVP.TransformPoint(math3d.Vec3{X: ndcX, Y: ndcY, Z: 1})
	dir := farPt.Sub(nearPt).Norm()
	return math3d.Ray{Origin: nearPt, Dir: dir}
}

// WorldToScreen projects a world-space point into viewport pixel coordinates
// (top-left origin), returning NDC depth in [0, 1] and whether the point lies
// within the camera frustum.
func (c *Camera) WorldToScreen(worldPos math3d.Vec3, viewW, viewH float32) (screen math3d.Vec2, depth float32, visible bool) {
	clip := c.ViewProj().MulVec4(worldPos.Vec4(1))
	if clip.W <= 1e-6 {
		return math3d.Vec2{}, 0, false
	}
	invW := 1 / clip.W
	ndcX := clip.X * invW
	ndcY := clip.Y * invW
	ndcZ := clip.Z * invW
	sx := (ndcX + 1) * 0.5 * viewW
	sy := (1 - ndcY) * 0.5 * viewH
	inView := ndcX >= -1 && ndcX <= 1 && ndcY >= -1 && ndcY <= 1 && ndcZ >= 0 && ndcZ <= 1
	return math3d.Vec2{X: sx, Y: sy}, ndcZ, inView
}

var (
	defaultMinPitch = -math3d.DegToRad(89)
	defaultMaxPitch = math3d.DegToRad(89)
)

func pitchBounds(minP, maxP float32) (float32, float32) {
	if minP == 0 && maxP == 0 {
		return defaultMinPitch, defaultMaxPitch
	}
	if minP > maxP {
		minP, maxP = maxP, minP
	}
	return minP, maxP
}

func dampingAlpha(rate float32, dt time.Duration) float32 {
	if rate <= 0 || dt <= 0 {
		return 1
	}
	return float32(1 - math.Exp(-float64(rate)*dt.Seconds()))
}

// OrbitController orbits a Camera around Target using spherical coordinates
// (Yaw, Pitch, Distance) with clamped pitch, zoom limits, screen-plane panning,
// and optional exponential per-step damping.
type OrbitController struct {
	Target      math3d.Vec3
	Yaw         float32 // Radians around +Y (0 places camera at +Z looking down -Z)
	Pitch       float32 // Radians elevation above Target (clamped to [MinPitch, MaxPitch])
	Distance    float32 // Distance from Target (clamped to [MinDistance, MaxDistance])
	MinPitch    float32 // Defaults to -89° when MinPitch == MaxPitch == 0
	MaxPitch    float32 // Defaults to +89° when MinPitch == MaxPitch == 0
	MinDistance float32 // Minimum zoom distance (clamped >= 0.001)
	MaxDistance float32 // Maximum zoom distance (<= 0 means unbounded)
	Damping     float32 // Exponential damping rate in 1/s (<= 0 disables damping)

	initialized     bool
	desiredTarget   math3d.Vec3
	desiredYaw      float32
	desiredPitch    float32
	desiredDistance float32
}

// NewOrbitController creates an OrbitController orbiting target at distance
// with default pitch limits [-89°, +89°] and zoom limits [0.1, 1000].
func NewOrbitController(target math3d.Vec3, distance float32) *OrbitController {
	c := &OrbitController{
		Target:      target,
		Distance:    distance,
		MinPitch:    defaultMinPitch,
		MaxPitch:    defaultMaxPitch,
		MinDistance: 0.1,
		MaxDistance: 1000,
	}
	c.ensureInit()
	return c
}

func (c *OrbitController) ensureInit() {
	if c.initialized {
		return
	}
	minP, maxP := pitchBounds(c.MinPitch, c.MaxPitch)
	c.MinPitch, c.MaxPitch = minP, maxP
	c.Pitch = math3d.Clamp(c.Pitch, c.MinPitch, c.MaxPitch)
	c.Distance = c.clampDistance(c.Distance)
	c.desiredTarget = c.Target
	c.desiredYaw = c.Yaw
	c.desiredPitch = c.Pitch
	c.desiredDistance = c.Distance
	c.initialized = true
}

func (c *OrbitController) clampDistance(d float32) float32 {
	minD := c.MinDistance
	if minD < 1e-3 {
		minD = 1e-3
	}
	if d < minD {
		d = minD
	}
	if c.MaxDistance > 0 && d > c.MaxDistance {
		d = c.MaxDistance
	}
	return d
}

// Orbit adjusts the desired yaw and pitch by (deltaYaw, deltaPitch) radians,
// clamping pitch to [MinPitch, MaxPitch]. When Damping <= 0, current angles
// update immediately.
func (c *OrbitController) Orbit(deltaYaw, deltaPitch float32) {
	c.ensureInit()
	minP, maxP := pitchBounds(c.MinPitch, c.MaxPitch)
	c.desiredYaw += deltaYaw
	c.desiredPitch = math3d.Clamp(c.desiredPitch+deltaPitch, minP, maxP)
	if c.Damping <= 0 {
		c.Yaw = c.desiredYaw
		c.Pitch = c.desiredPitch
	}
}

// Zoom adjusts the desired orbit distance by delta (positive zooms out,
// negative zooms in), clamped to [MinDistance, MaxDistance].
func (c *OrbitController) Zoom(delta float32) {
	c.ensureInit()
	c.desiredDistance = c.clampDistance(c.desiredDistance + delta)
	if c.Damping <= 0 {
		c.Distance = c.desiredDistance
	}
}

// SetDistance sets the desired orbit distance, clamped to [MinDistance, MaxDistance].
func (c *OrbitController) SetDistance(distance float32) {
	c.ensureInit()
	c.desiredDistance = c.clampDistance(distance)
	if c.Damping <= 0 {
		c.Distance = c.desiredDistance
	}
}

// SetTarget updates the desired orbit target.
func (c *OrbitController) SetTarget(target math3d.Vec3) {
	c.ensureInit()
	c.desiredTarget = target
	if c.Damping <= 0 {
		c.Target = c.desiredTarget
	}
}

// SetAngles sets the desired yaw and pitch in radians, clamping pitch.
func (c *OrbitController) SetAngles(yaw, pitch float32) {
	c.ensureInit()
	minP, maxP := pitchBounds(c.MinPitch, c.MaxPitch)
	c.desiredYaw = yaw
	c.desiredPitch = math3d.Clamp(pitch, minP, maxP)
	if c.Damping <= 0 {
		c.Yaw = c.desiredYaw
		c.Pitch = c.desiredPitch
	}
}

// Pan shifts the orbit target within the camera's local right/up plane.
func (c *OrbitController) Pan(deltaRight, deltaUp float32) {
	c.ensureInit()
	right, up := orbitBasis(c.desiredYaw, c.desiredPitch)
	c.desiredTarget = c.desiredTarget.Add(right.Scale(deltaRight)).Add(up.Scale(deltaUp))
	if c.Damping <= 0 {
		c.Target = c.desiredTarget
	}
}

// Snap immediately sets current Target, Yaw, Pitch, and Distance to their
// desired values with no damping lag.
func (c *OrbitController) Snap() {
	c.ensureInit()
	minP, maxP := pitchBounds(c.MinPitch, c.MaxPitch)
	c.desiredPitch = math3d.Clamp(c.desiredPitch, minP, maxP)
	c.desiredDistance = c.clampDistance(c.desiredDistance)
	c.Target = c.desiredTarget
	c.Yaw = c.desiredYaw
	c.Pitch = c.desiredPitch
	c.Distance = c.desiredDistance
}

// Step advances orbit damping by dt. Call this inside game.Loop.Update.
func (c *OrbitController) Step(dt time.Duration) {
	c.ensureInit()
	minP, maxP := pitchBounds(c.MinPitch, c.MaxPitch)
	c.desiredPitch = math3d.Clamp(c.desiredPitch, minP, maxP)
	c.desiredDistance = c.clampDistance(c.desiredDistance)
	if c.Damping <= 0 {
		c.Snap()
		return
	}
	if dt <= 0 {
		return
	}
	alpha := dampingAlpha(c.Damping, dt)
	c.Target = c.Target.Lerp(c.desiredTarget, alpha)
	c.Yaw = math3d.Lerp(c.Yaw, c.desiredYaw, alpha)
	c.Pitch = math3d.Clamp(math3d.Lerp(c.Pitch, c.desiredPitch, alpha), minP, maxP)
	c.Distance = c.clampDistance(math3d.Lerp(c.Distance, c.desiredDistance, alpha))
}

// Apply writes the controller's current pose into cam.
func (c *OrbitController) Apply(cam *Camera) {
	if cam == nil {
		return
	}
	c.ensureInit()
	minP, maxP := pitchBounds(c.MinPitch, c.MaxPitch)
	pitch := math3d.Clamp(c.Pitch, minP, maxP)
	dist := c.clampDistance(c.Distance)
	offset := orbitOffset(c.Yaw, pitch, dist)
	cam.Pos = c.Target.Add(offset)
	cam.Rot = math3d.LookRotation(offset.Neg(), math3d.Vec3Up())
}

// Update advances damping by dt and applies the resulting pose to cam.
func (c *OrbitController) Update(cam *Camera, dt time.Duration) {
	c.Step(dt)
	c.Apply(cam)
}

func orbitOffset(yaw, pitch, distance float32) math3d.Vec3 {
	sp, cp := math.Sincos(float64(pitch))
	sy, cy := math.Sincos(float64(yaw))
	horiz := float64(distance) * cp
	return math3d.Vec3{
		X: float32(horiz * sy),
		Y: float32(float64(distance) * sp),
		Z: float32(horiz * cy),
	}
}

func orbitBasis(yaw, pitch float32) (right, up math3d.Vec3) {
	offset := orbitOffset(yaw, pitch, 1)
	rot := math3d.LookRotation(offset.Neg(), math3d.Vec3Up())
	return rot.RotateVec3(math3d.Vec3Right()), rot.RotateVec3(math3d.Vec3Up())
}

// FPSController implements a first-person camera controller with yaw/pitch
// orientation, clamped pitch, and local forward/right/up movement.
type FPSController struct {
	Pos       math3d.Vec3
	Yaw       float32 // Radians around +Y (0 faces -Z)
	Pitch     float32 // Radians around local +X (positive looks up)
	MinPitch  float32 // Defaults to -89° when MinPitch == MaxPitch == 0
	MaxPitch  float32 // Defaults to +89° when MinPitch == MaxPitch == 0
	MoveSpeed float32 // Speed multiplier in units/second (defaults to 1 when <= 0)
	Grounded  bool    // When true, forward/right move on the horizontal XZ plane and up moves along +Y
}

// NewFPSController creates an FPSController at pos with pitch clamped to [-89°, +89°].
func NewFPSController(pos math3d.Vec3) *FPSController {
	return &FPSController{
		Pos:       pos,
		MinPitch:  defaultMinPitch,
		MaxPitch:  defaultMaxPitch,
		MoveSpeed: 1,
	}
}

// Look adjusts Yaw and Pitch by (deltaYaw, deltaPitch) radians, clamping Pitch
// to [MinPitch, MaxPitch].
func (c *FPSController) Look(deltaYaw, deltaPitch float32) {
	minP, maxP := pitchBounds(c.MinPitch, c.MaxPitch)
	c.Yaw += deltaYaw
	c.Pitch = math3d.Clamp(c.Pitch+deltaPitch, minP, maxP)
}

// Rotation returns the unit orientation quaternion for (Yaw, Pitch).
func (c *FPSController) Rotation() math3d.Quat {
	minP, maxP := pitchBounds(c.MinPitch, c.MaxPitch)
	pitch := math3d.Clamp(c.Pitch, minP, maxP)
	return math3d.QuatFromEuler(pitch, c.Yaw, 0)
}

// Forward returns the controller's local unit forward vector.
func (c *FPSController) Forward() math3d.Vec3 {
	return c.Rotation().RotateVec3(math3d.Vec3Forward())
}

// Right returns the controller's local unit right vector.
func (c *FPSController) Right() math3d.Vec3 {
	return c.Rotation().RotateVec3(math3d.Vec3Right())
}

// Up returns the controller's local unit up vector.
func (c *FPSController) Up() math3d.Vec3 {
	return c.Rotation().RotateVec3(math3d.Vec3Up())
}

// Move translates Pos along the controller's local forward, right, and up axes
// scaled by MoveSpeed and dt.
func (c *FPSController) Move(forward, right, up float32, dt time.Duration) {
	if dt <= 0 {
		return
	}
	speed := c.MoveSpeed
	if speed <= 0 {
		speed = 1
	}
	scale := speed * float32(dt.Seconds())

	var fwdAxis, rightAxis, upAxis math3d.Vec3
	if c.Grounded {
		yawRot := math3d.QuatFromEuler(0, c.Yaw, 0)
		fwdAxis = yawRot.RotateVec3(math3d.Vec3Forward())
		rightAxis = yawRot.RotateVec3(math3d.Vec3Right())
		upAxis = math3d.Vec3Up()
	} else {
		rot := c.Rotation()
		fwdAxis = rot.RotateVec3(math3d.Vec3Forward())
		rightAxis = rot.RotateVec3(math3d.Vec3Right())
		upAxis = rot.RotateVec3(math3d.Vec3Up())
	}

	delta := fwdAxis.Scale(forward).Add(rightAxis.Scale(right)).Add(upAxis.Scale(up))
	c.Pos = c.Pos.Add(delta.Scale(scale))
}

// Apply writes the controller's current Pos and Rotation into cam.
func (c *FPSController) Apply(cam *Camera) {
	if cam == nil {
		return
	}
	cam.Pos = c.Pos
	cam.Rot = c.Rotation()
}

// ThirdPersonController follows a moving target position and facing direction
// with configurable offset, trailing distance, height, look-ahead, exponential
// per-step smoothing, and optional raycast obstruction pullback.
type ThirdPersonController struct {
	// Offset shifts the target pivot in target-local coordinates (+X right, +Y up, +Z forward).
	Offset math3d.Vec3
	// Distance is the trailing distance behind the target pivot.
	Distance float32
	// Height is the vertical elevation above the target pivot.
	Height float32
	// LookAhead shifts the look point forward along target facing from the pivot.
	LookAhead float32
	// PositionSmoothing is the exponential smoothing rate (1/s) for camera Pos (<= 0 snaps).
	PositionSmoothing float32
	// LookSmoothing is the exponential smoothing rate (1/s) for LookTarget (<= 0 snaps).
	LookSmoothing float32
	// ObstructionPadding keeps the pulled-back camera this distance in front of an obstacle.
	ObstructionPadding float32
	// MinDistance is the closest distance the camera pulls to the pivot when obstructed.
	MinDistance float32
	// ObstructionCheck tests the segment from the target pivot (from) to the camera
	// position (to). When obstructed, it returns the normalized hit fraction t in [0, 1]
	// along from->to and true.
	ObstructionCheck func(from, to math3d.Vec3) (float32, bool)

	// Pos is the current world-space camera position.
	Pos math3d.Vec3
	// LookTarget is the current world-space point the camera is looking at.
	LookTarget math3d.Vec3
}

// NewThirdPersonController returns a ThirdPersonController with balanced defaults.
func NewThirdPersonController() *ThirdPersonController {
	return &ThirdPersonController{
		Distance:           5,
		Height:             2,
		PositionSmoothing:  8,
		LookSmoothing:      12,
		ObstructionPadding: 0.15,
		MinDistance:        0.2,
	}
}

func (c *ThirdPersonController) computeGoals(targetPos, targetForward math3d.Vec3) (pivot, desiredPos, desiredLook math3d.Vec3) {
	fwd := targetForward
	if fwd.LenSq() < 1e-12 {
		fwd = math3d.Vec3Forward()
	} else {
		fwd = fwd.Norm()
	}
	right := fwd.Cross(math3d.Vec3Up())
	if right.LenSq() < 1e-12 {
		right = math3d.Vec3Right()
	} else {
		right = right.Norm()
	}
	up := right.Cross(fwd).Norm()

	pivot = targetPos.
		Add(right.Scale(c.Offset.X)).
		Add(up.Scale(c.Offset.Y)).
		Add(fwd.Scale(c.Offset.Z))

	desiredPos = pivot.
		Sub(fwd.Scale(c.Distance)).
		Add(math3d.Vec3Up().Scale(c.Height))

	desiredLook = pivot.Add(fwd.Scale(c.LookAhead))
	return pivot, desiredPos, desiredLook
}

func (c *ThirdPersonController) resolveObstruction(pivot, camPos math3d.Vec3) math3d.Vec3 {
	if c.ObstructionCheck == nil {
		return camPos
	}
	t, blocked := c.ObstructionCheck(pivot, camPos)
	if !blocked {
		return camPos
	}
	t = math3d.Clamp(t, 0, 1)
	seg := camPos.Sub(pivot)
	segLen := seg.Len()
	if segLen < 1e-6 {
		return pivot
	}
	hitDist := t * segLen
	pad := max(float32(0), c.ObstructionPadding)
	pullDist := hitDist - pad
	if pullDist < c.MinDistance {
		pullDist = min(c.MinDistance, hitDist)
	}
	if pullDist < 0 {
		pullDist = 0
	}
	return pivot.Add(seg.Scale(pullDist / segLen))
}

// Snap immediately moves Pos and LookTarget to the ideal follow configuration
// for (targetPos, targetForward), respecting ObstructionCheck.
func (c *ThirdPersonController) Snap(targetPos, targetForward math3d.Vec3) {
	pivot, desiredPos, desiredLook := c.computeGoals(targetPos, targetForward)
	c.Pos = c.resolveObstruction(pivot, desiredPos)
	c.LookTarget = desiredLook
}

// Step advances the third-person camera toward (targetPos, targetForward) by dt
// using exponential smoothing and pulls in front of any obstruction immediately.
func (c *ThirdPersonController) Step(targetPos, targetForward math3d.Vec3, dt time.Duration) {
	pivot, desiredPos, desiredLook := c.computeGoals(targetPos, targetForward)
	desiredPos = c.resolveObstruction(pivot, desiredPos)

	if c.PositionSmoothing <= 0 {
		c.Pos = desiredPos
	} else if dt > 0 {
		c.Pos = c.Pos.Lerp(desiredPos, dampingAlpha(c.PositionSmoothing, dt))
	}
	// Guarantee the smoothed position never remains behind a wall.
	c.Pos = c.resolveObstruction(pivot, c.Pos)

	if c.LookSmoothing <= 0 {
		c.LookTarget = desiredLook
	} else if dt > 0 {
		c.LookTarget = c.LookTarget.Lerp(desiredLook, dampingAlpha(c.LookSmoothing, dt))
	}
}

// Apply writes the controller's current Pos and orientation into cam.
func (c *ThirdPersonController) Apply(cam *Camera) {
	if cam == nil {
		return
	}
	cam.Pos = c.Pos
	cam.LookAt(c.LookTarget, math3d.Vec3Up())
}

// Update steps the controller by dt and applies the resulting pose to cam.
func (c *ThirdPersonController) Update(cam *Camera, targetPos, targetForward math3d.Vec3, dt time.Duration) {
	c.Step(targetPos, targetForward, dt)
	c.Apply(cam)
}
