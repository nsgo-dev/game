package motion

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/internal/detmath"
)

const (
	degToRad = math.Pi / 180.0
	radToDeg = 180.0 / math.Pi
)

// WaveKind identifies the periodic waveform used by Sine.
type WaveKind string

const (
	// WaveSine oscillates as sin(2*pi*u) in [-1, 1].
	WaveSine WaveKind = "sine"
	// WaveTriangle oscillates linearly from 0 -> 1 -> -1 -> 0 in [-1, 1].
	WaveTriangle WaveKind = "triangle"
	// WaveSawtooth ramps linearly from -1 to +1 over each period, jumping back to -1 at u = 1.
	WaveSawtooth WaveKind = "sawtooth"
	// WaveReverseSawtooth ramps linearly from +1 to -1 over each period, jumping back to +1 at u = 1.
	WaveReverseSawtooth WaveKind = "reverse_sawtooth"
	// WaveSquare holds +1 for the first half of each period [0, 0.5) and -1 for [0.5, 1).
	WaveSquare WaveKind = "square"
)

// Bounds is an axis-aligned 2D rectangle [MinX, MaxX] x [MinY, MaxY].
type Bounds struct {
	MinX, MinY float64
	MaxX, MaxY float64
}

// Valid reports whether b has positive finite width and height.
func (b Bounds) Valid() bool {
	return isFinite(b.MinX) && isFinite(b.MinY) && isFinite(b.MaxX) && isFinite(b.MaxY) &&
		b.MaxX > b.MinX && b.MaxY > b.MinY
}

// Expand returns b widened outward by margin on all four sides (negative margin is treated as 0).
func (b Bounds) Expand(margin float64) Bounds {
	if margin <= 0 || !isFinite(margin) {
		return b
	}
	return Bounds{
		MinX: b.MinX - margin,
		MinY: b.MinY - margin,
		MaxX: b.MaxX + margin,
		MaxY: b.MaxY + margin,
	}
}

// SolidHit is the earliest continuous contact along a motion sweep from (x, y) by (dx, dy).
type SolidHit struct {
	// Fraction is the normalized time of impact in [0, 1].
	Fraction float64
	// NormalX and NormalY form the unit surface normal pointing out of the solid.
	NormalX, NormalY float64
}

// SolidCastFunc sweeps an object from (x, y) by displacement (dx, dy) and reports the earliest
// solid hit in [0, 1], if any.
type SolidCastFunc func(x, y, dx, dy float64) (SolidHit, bool)

// Bullet advances a 2D projectile with speed, linear acceleration along its travel
// direction, vertical gravity (+Y down), optional orientation alignment and continuous
// bouncing off solids.
type Bullet struct {
	// Speed is the current scalar travel speed in points per second along Angle.
	Speed float64
	// Acceleration is the linear acceleration along the velocity direction in points/s^2.
	Acceleration float64
	// Gravity is the downward (+Y) acceleration in points/s^2.
	Gravity float64
	// Angle is the heading in degrees (0 = +X right, 90 = +Y down).
	Angle float64
	// SetAngle reports whether the object's rotation should track the heading.
	SetAngle bool
	// BounceOffSolids reflects velocity across solid surface normals when a SolidCastFunc is supplied.
	BounceOffSolids bool
	// Enabled controls whether Step advances the bullet.
	Enabled bool

	vx, vy   float64
	hasVel   bool
	lastSpd  float64
	lastAng  float64
	maxSteps int
}

// BulletStepResult reports the updated position, heading and bounce info for one Bullet.Step call.
type BulletStepResult struct {
	X, Y      float64
	Rotation  float64
	SetRot    bool
	Bounced   bool
	Bounces   int
	HitNormal [2]float64
}

// NewBullet returns an enabled Bullet initialized from speed, acceleration, gravity and angle (degrees).
func NewBullet(speed, acceleration, gravity, angleDeg float64, setAngle, bounceOffSolids bool) Bullet {
	b := Bullet{
		Speed:           speed,
		Acceleration:    acceleration,
		Gravity:         gravity,
		Angle:           angleDeg,
		SetAngle:        setAngle,
		BounceOffSolids: bounceOffSolids,
		Enabled:         true,
	}
	b.syncVelocityFromPolar()
	return b
}

func (b *Bullet) syncVelocityFromPolar() {
	rad := b.Angle * degToRad
	s, c := detmath.Sincos(rad)
	b.vx = b.Speed * c
	b.vy = b.Speed * s
	b.lastSpd = b.Speed
	b.lastAng = b.Angle
	b.hasVel = true
}

func (b *Bullet) ensureVelocity() {
	if !b.hasVel || b.Speed != b.lastSpd || b.Angle != b.lastAng {
		b.syncVelocityFromPolar()
	}
}

// SetSpeed updates the bullet's scalar speed while preserving its current heading.
func (b *Bullet) SetSpeed(speed float64) {
	if !isFinite(speed) {
		return
	}
	b.ensureVelocity()
	b.Speed = speed
	b.syncVelocityFromPolar()
}

// SetHeading updates the bullet's heading in degrees while preserving its current speed.
func (b *Bullet) SetHeading(angleDeg float64) {
	if !isFinite(angleDeg) {
		return
	}
	b.ensureVelocity()
	b.Angle = angleDeg
	b.syncVelocityFromPolar()
}

// Velocity returns the current (vx, vy) velocity in points per second.
func (b *Bullet) Velocity() (vx, vy float64) {
	b.ensureVelocity()
	return b.vx, b.vy
}

// Step advances the bullet from (x, y) by dt. When BounceOffSolids is true and cast is non-nil,
// the displacement is swept continuously against solids and reflected across the contact normal.
func (b *Bullet) Step(x, y float64, dt time.Duration, cast SolidCastFunc) BulletStepResult {
	res := BulletStepResult{X: x, Y: y}
	if b == nil || !b.Enabled || dt <= 0 {
		return res
	}
	b.ensureVelocity()

	rem := dt.Seconds()
	if !isFinite(rem) || rem <= 0 {
		return res
	}

	maxBounces := b.maxSteps
	if maxBounces <= 0 {
		maxBounces = 4
	}

	const skin = 1e-4
	curX, curY := x, y

	for iter := 0; iter <= maxBounces && rem > 1e-12; iter++ {
		dx, dy, nextVX, nextVY := b.integrateSegment(rem)
		if !b.BounceOffSolids || cast == nil || (dx == 0 && dy == 0) {
			curX += dx
			curY += dy
			b.vx, b.vy = nextVX, nextVY
			b.updatePolarFromVelocity()
			break
		}

		hit, ok := cast(curX, curY, dx, dy)
		if !ok {
			curX += dx
			curY += dy
			b.vx, b.vy = nextVX, nextVY
			b.updatePolarFromVelocity()
			break
		}

		t := clamp(hit.Fraction, 0, 1)
		hitDT := rem * t
		if hitDT > 0 {
			hdx, hdy, hvx, hvy := b.integrateSegment(hitDT)
			curX += hdx
			curY += hdy
			b.vx, b.vy = hvx, hvy
		}

		nx, ny := hit.NormalX, hit.NormalY
		nlen := detmath.Hypot(nx, ny)
		if nlen > 1e-12 {
			nx /= nlen
			ny /= nlen
		}

		// Reflect velocity across the outward unit normal if moving into the surface.
		dot := b.vx*nx + b.vy*ny
		if dot < 0 {
			b.vx -= 2 * dot * nx
			b.vy -= 2 * dot * ny
		} else if nlen > 1e-12 {
			// Already separating or tangent; still flip along normal if stuck at Fraction == 0.
			if t == 0 && dot == 0 {
				b.vx = -b.vx
				b.vy = -b.vy
			}
		}

		// Push slightly off the contact surface so the next sub-sweep does not re-hit at t == 0.
		if nlen > 1e-12 {
			curX += nx * skin
			curY += ny * skin
		}

		b.updatePolarFromVelocity()
		res.Bounced = true
		res.Bounces++
		res.HitNormal = [2]float64{nx, ny}

		rem -= hitDT
		if t == 0 && iter == maxBounces {
			break
		}
	}

	res.X = curX
	res.Y = curY
	if b.SetAngle && (b.vx != 0 || b.vy != 0) {
		res.Rotation = detmath.Atan2(b.vy, b.vx)
		res.SetRot = true
	}
	return res
}

// integrateSegment computes exact rate-independent displacement and end velocity for pure
// linear acceleration, pure gravity, or constant velocity, and midpoint RK2 when both are non-zero.
func (b *Bullet) integrateSegment(dt float64) (dx, dy, nextVX, nextVY float64) {
	vx0, vy0 := b.vx, b.vy
	acc := b.Acceleration
	grav := b.Gravity

	switch {
	case acc == 0 && grav == 0:
		return vx0 * dt, vy0 * dt, vx0, vy0

	case acc == 0:
		// Exact parabolic motion: rate-independent at any step size.
		dx = vx0 * dt
		dy = vy0*dt + 0.5*grav*dt*dt
		return dx, dy, vx0, vy0 + grav*dt

	case grav == 0:
		// Exact constant acceleration along heading: rate-independent at any step size.
		rad := b.Angle * degToRad
		s, c := detmath.Sincos(rad)
		v0 := b.Speed
		v1 := v0 + acc*dt
		ds := (v0 + v1) * 0.5 * dt
		return ds * c, ds * s, v1 * c, v1 * s

	default:
		// Midpoint RK2 when both tangential acceleration and gravity act together.
		ax0, ay0 := accelVec(vx0, vy0, b.Angle, acc, grav)
		vxMid := vx0 + ax0*(0.5*dt)
		vyMid := vy0 + ay0*(0.5*dt)
		axMid, ayMid := accelVec(vxMid, vyMid, b.Angle, acc, grav)
		return vxMid * dt, vyMid * dt, vx0 + axMid*dt, vy0 + ayMid*dt
	}
}

func accelVec(vx, vy, fallbackAngleDeg, acc, grav float64) (ax, ay float64) {
	spd := detmath.Hypot(vx, vy)
	if spd > 1e-12 {
		ax = acc * (vx / spd)
		ay = acc*(vy/spd) + grav
		return ax, ay
	}
	rad := fallbackAngleDeg * degToRad
	s, c := detmath.Sincos(rad)
	return acc * c, acc*s + grav
}

func (b *Bullet) updatePolarFromVelocity() {
	spd := detmath.Hypot(b.vx, b.vy)
	if spd > 1e-12 {
		if b.Speed < 0 && b.Gravity == 0 {
			// Keep signed speed aligned with heading when reversing under deceleration.
			rad := b.Angle * degToRad
			s, c := detmath.Sincos(rad)
			if b.vx*c+b.vy*s < 0 {
				b.Speed = -spd
			} else {
				b.Speed = spd
			}
		} else {
			b.Speed = spd
			b.Angle = detmath.Atan2(b.vy, b.vx) * radToDeg
		}
	} else {
		b.Speed = 0
	}
	b.lastSpd = b.Speed
	b.lastAng = b.Angle
	b.hasVel = true
}

// Sine oscillates a scalar quantity periodically around its authored baseline using
// additive deltas, so it composes cleanly with other behaviors (such as Bullet).
type Sine struct {
	// Period is the duration of one full oscillation cycle in seconds (> 0).
	Period float64
	// Magnitude is the peak amplitude in property units (points, degrees, scale, or alpha).
	Magnitude float64
	// Wave selects the periodic waveform (defaults to WaveSine when empty).
	Wave WaveKind
	// Phase is the initial cycle offset (0.25 = quarter period, 0.5 = half period).
	Phase float64
	// Enabled controls whether Step advances the oscillation.
	Enabled bool

	elapsed    float64
	lastOffset float64
}

// NewSine returns an enabled Sine oscillator.
func NewSine(period, magnitude float64, wave WaveKind, phase float64) Sine {
	if wave == "" {
		wave = WaveSine
	}
	return Sine{
		Period:    period,
		Magnitude: magnitude,
		Wave:      wave,
		Phase:     phase,
		Enabled:   true,
	}
}

// EvalWave evaluates wave at cycle position u (any finite real number) and returns a value in [-1, 1].
func EvalWave(wave WaveKind, u float64) float64 {
	if !isFinite(u) {
		return 0
	}
	// Wrap u into [0, 1).
	f := u - math.Floor(u)
	if f >= 1 {
		f = 0
	}
	switch wave {
	case "", WaveSine:
		return detmath.Sin(2 * math.Pi * f)
	case WaveTriangle:
		// 0 -> 1 on [0, 0.25], 1 -> -1 on [0.25, 0.75], -1 -> 0 on [0.75, 1)
		switch {
		case f < 0.25:
			return 4 * f
		case f < 0.75:
			return 2 - 4*f
		default:
			return 4*f - 4
		}
	case WaveSawtooth:
		return 2*f - 1
	case WaveReverseSawtooth:
		return 1 - 2*f
	case WaveSquare:
		if f < 0.5 {
			return 1
		}
		return -1
	default:
		return detmath.Sin(2 * math.Pi * f)
	}
}

// Value returns the current oscillation offset at the accumulated elapsed time.
func (s *Sine) Value() float64 {
	if s == nil || s.Period <= 0 || !isFinite(s.Period) || !isFinite(s.Magnitude) {
		return 0
	}
	u := s.elapsed/s.Period + s.Phase
	return EvalWave(s.Wave, u) * s.Magnitude
}

// Elapsed returns the accumulated simulated time in seconds.
func (s *Sine) Elapsed() float64 {
	if s == nil {
		return 0
	}
	return s.elapsed
}

// Reset sets elapsed time and applied offset back to zero.
func (s *Sine) Reset() {
	if s == nil {
		return
	}
	s.elapsed = 0
	s.lastOffset = 0
}

// Step advances the oscillator by dt and returns the incremental delta (newValue - lastValue)
// to add to the target property.
func (s *Sine) Step(dt time.Duration) (delta float64) {
	if s == nil || !s.Enabled || dt <= 0 || s.Period <= 0 || !isFinite(s.Period) {
		return 0
	}
	s.elapsed += dt.Seconds()
	next := s.Value()
	delta = next - s.lastOffset
	s.lastOffset = next
	return delta
}

// Rotate spins an object continuously at Speed degrees per second with optional angular Acceleration.
type Rotate struct {
	// Speed is the angular velocity in degrees per second (positive = clockwise in Y-down 2D).
	Speed float64
	// Acceleration is the angular acceleration in degrees per second squared.
	Acceleration float64
	// Enabled controls whether Step advances the rotation.
	Enabled bool
}

// NewRotate returns an enabled Rotate behavior.
func NewRotate(speedDeg, accelDeg float64) Rotate {
	return Rotate{
		Speed:        speedDeg,
		Acceleration: accelDeg,
		Enabled:      true,
	}
}

// Step advances the angular velocity by dt using exact constant-acceleration integration
// and returns the incremental rotation in radians to add to the object's rotation.
func (r *Rotate) Step(dt time.Duration) (deltaRad float64) {
	if r == nil || !r.Enabled || dt <= 0 {
		return 0
	}
	secs := dt.Seconds()
	v0 := r.Speed
	v1 := v0 + r.Acceleration*secs
	dDeg := (v0 + v1) * 0.5 * secs
	r.Speed = v1
	return dDeg * degToRad
}

// Orbit moves an object along a circle of Radius around a center point at Speed degrees per second.
type Orbit struct {
	// Radius is the orbit radius in points (>= 0).
	Radius float64
	// Speed is the angular velocity in degrees per second.
	Speed float64
	// Angle is the current orbit angle in degrees (0 = +X right, 90 = +Y down).
	Angle float64
	// SetAngle reports whether the object's rotation should match the orbit angle.
	SetAngle bool
	// Enabled controls whether Step advances the orbit.
	Enabled bool
}

// OrbitStepResult holds the updated position and optional rotation from one Orbit.Step call.
type OrbitStepResult struct {
	X, Y     float64
	Rotation float64
	SetRot   bool
}

// NewOrbit returns an enabled Orbit behavior.
func NewOrbit(radius, speedDeg, angleDeg float64, setAngle bool) Orbit {
	return Orbit{
		Radius:   radius,
		Speed:    speedDeg,
		Angle:    angleDeg,
		SetAngle: setAngle,
		Enabled:  true,
	}
}

// Step advances the orbit angle by dt and computes the object's new position around (centerX, centerY).
func (o *Orbit) Step(centerX, centerY float64, dt time.Duration) OrbitStepResult {
	if o == nil || !o.Enabled || dt <= 0 {
		rad := 0.0
		if o != nil {
			rad = o.Angle * degToRad
		}
		s, c := detmath.Sincos(rad)
		r := 0.0
		if o != nil {
			r = o.Radius
		}
		return OrbitStepResult{
			X:        centerX + r*c,
			Y:        centerY + r*s,
			Rotation: rad,
			SetRot:   o != nil && o.SetAngle,
		}
	}
	o.Angle += o.Speed * dt.Seconds()
	rad := o.Angle * degToRad
	s, c := detmath.Sincos(rad)
	return OrbitStepResult{
		X:        centerX + o.Radius*c,
		Y:        centerY + o.Radius*s,
		Rotation: rad,
		SetRot:   o.SetAngle,
	}
}

// MoveTo drives an object straight toward (TargetX, TargetY) at up to MaxSpeed points per second,
// optionally accelerating smoothly and stopping on solid collisions.
type MoveTo struct {
	// MaxSpeed is the top speed in points per second (>= 0).
	MaxSpeed float64
	// Acceleration is the linear ramp-up in points/s^2 (0 reaches MaxSpeed immediately).
	Acceleration float64
	// StopOnSolids halts movement when a solid is encountered along the path.
	StopOnSolids bool
	// TargetX and TargetY are the current destination coordinates.
	TargetX, TargetY float64
	// Active reports whether the object is currently moving toward the target.
	Active bool
	// Enabled controls whether Step advances movement.
	Enabled bool

	speed float64
}

// MoveToStepResult reports the updated position and arrival/block flags from one MoveTo.Step call.
type MoveToStepResult struct {
	X, Y    float64
	Arrived bool
	Blocked bool
}

// NewMoveTo returns an enabled (initially idle) MoveTo behavior.
func NewMoveTo(maxSpeed, acceleration float64, stopOnSolids bool) MoveTo {
	return MoveTo{
		MaxSpeed:     maxSpeed,
		Acceleration: acceleration,
		StopOnSolids: stopOnSolids,
		Enabled:      true,
	}
}

// SetTarget assigns a new destination and starts moving toward it.
func (m *MoveTo) SetTarget(tx, ty float64) {
	if m == nil || !isFinite(tx) || !isFinite(ty) {
		return
	}
	m.TargetX = tx
	m.TargetY = ty
	m.Active = true
}

// Stop halts movement and resets current speed to zero.
func (m *MoveTo) Stop() {
	if m == nil {
		return
	}
	m.Active = false
	m.speed = 0
}

// Speed returns the current instantaneous speed in points per second.
func (m *MoveTo) Speed() float64 {
	if m == nil {
		return 0
	}
	return m.speed
}

// Step advances from (x, y) toward (TargetX, TargetY) by dt using exact piecewise
// constant-acceleration integration (rate-independent across fixed step rates).
func (m *MoveTo) Step(x, y float64, dt time.Duration, cast SolidCastFunc) MoveToStepResult {
	res := MoveToStepResult{X: x, Y: y}
	if m == nil || !m.Enabled || !m.Active || dt <= 0 {
		return res
	}
	secs := dt.Seconds()
	toX := m.TargetX - x
	toY := m.TargetY - y
	dist := detmath.Hypot(toX, toY)
	if dist <= 1e-9 {
		m.Active = false
		m.speed = 0
		res.X = m.TargetX
		res.Y = m.TargetY
		res.Arrived = true
		return res
	}
	if m.MaxSpeed <= 0 {
		return res
	}

	var stepDist float64
	if m.Acceleration <= 0 {
		m.speed = m.MaxSpeed
		stepDist = m.MaxSpeed * secs
	} else {
		if m.speed > m.MaxSpeed {
			m.speed = m.MaxSpeed
		}
		tAccel := (m.MaxSpeed - m.speed) / m.Acceleration
		if tAccel <= 0 {
			m.speed = m.MaxSpeed
			stepDist = m.MaxSpeed * secs
		} else if secs <= tAccel {
			v1 := m.speed + m.Acceleration*secs
			stepDist = (m.speed + v1) * 0.5 * secs
			m.speed = v1
		} else {
			stepDist = (m.speed+m.MaxSpeed)*0.5*tAccel + m.MaxSpeed*(secs-tAccel)
			m.speed = m.MaxSpeed
		}
	}

	var dx, dy float64
	willArrive := stepDist >= dist
	if willArrive {
		dx = toX
		dy = toY
	} else {
		scale := stepDist / dist
		dx = toX * scale
		dy = toY * scale
	}

	if m.StopOnSolids && cast != nil {
		if hit, ok := cast(x, y, dx, dy); ok {
			t := clamp(hit.Fraction, 0, 1)
			res.X = x + dx*t
			res.Y = y + dy*t
			res.Blocked = true
			m.Active = false
			m.speed = 0
			return res
		}
	}

	if willArrive {
		res.X = m.TargetX
		res.Y = m.TargetY
		res.Arrived = true
		m.Active = false
		m.speed = 0
		return res
	}

	res.X = x + dx
	res.Y = y + dy
	return res
}

// Wrap wraps coordinates leaving one side of a rectangle onto the opposite side,
// preserving overshoot so wrapped motion stays rate-independent.
type Wrap struct {
	// Margin expands the wrap rectangle outward on all four sides (>= 0).
	Margin float64
	// Enabled controls whether Apply wraps coordinates.
	Enabled bool
}

// NewWrap returns an enabled Wrap behavior.
func NewWrap(margin float64) Wrap {
	return Wrap{Margin: margin, Enabled: true}
}

// Apply wraps (x, y) into bounds expanded by Margin and reports whether wrapping occurred.
func (w *Wrap) Apply(x, y float64, bounds Bounds) (nx, ny float64, wrapped bool) {
	if w == nil || !w.Enabled {
		return x, y, false
	}
	b := bounds.Expand(w.Margin)
	if !b.Valid() {
		return x, y, false
	}
	width := b.MaxX - b.MinX
	height := b.MaxY - b.MinY
	nx, ny = x, y
	if nx < b.MinX || nx > b.MaxX {
		nx = b.MinX + positiveMod(nx-b.MinX, width)
		wrapped = true
	}
	if ny < b.MinY || ny > b.MaxY {
		ny = b.MinY + positiveMod(ny-b.MinY, height)
		wrapped = true
	}
	return nx, ny, wrapped
}

// Bound clamps an object inside a rectangle, either by its edge extents or by its origin.
type Bound struct {
	// Edge keeps the object's full [x-Left, x+Right] x [y-Top, y+Bottom] box inside bounds;
	// when false, clamps only the origin (x, y).
	Edge bool
	// Enabled controls whether Apply clamps coordinates.
	Enabled bool
}

// NewBound returns an enabled Bound behavior.
func NewBound(edge bool) Bound {
	return Bound{Edge: edge, Enabled: true}
}

// Apply clamps (x, y) to stay inside bounds. When Edge is true, (left, top, right, bottom) are the
// non-negative distances from the object's origin to its four box edges.
func (b *Bound) Apply(x, y float64, bounds Bounds, left, top, right, bottom float64) (nx, ny float64) {
	if b == nil || !b.Enabled || !bounds.Valid() {
		return x, y
	}
	minX, maxX := bounds.MinX, bounds.MaxX
	minY, maxY := bounds.MinY, bounds.MaxY
	if b.Edge {
		minX += max(0, left)
		maxX -= max(0, right)
		minY += max(0, top)
		maxY -= max(0, bottom)
	}
	if minX > maxX {
		nx = (bounds.MinX + bounds.MaxX) * 0.5
	} else {
		nx = clamp(x, minX, maxX)
	}
	if minY > maxY {
		ny = (bounds.MinY + bounds.MaxY) * 0.5
	} else {
		ny = clamp(y, minY, maxY)
	}
	return nx, ny
}

// DestroyOutside tests whether an object has left a rectangle expanded by Margin.
type DestroyOutside struct {
	// Margin is the distance outside the bounds an object may travel before being destroyed (>= 0).
	Margin float64
	// Enabled controls whether Outside returns true.
	Enabled bool
}

// NewDestroyOutside returns an enabled DestroyOutside check.
func NewDestroyOutside(margin float64) DestroyOutside {
	return DestroyOutside{Margin: margin, Enabled: true}
}

// Outside reports whether (x, y) lies strictly outside bounds expanded by Margin.
func (d *DestroyOutside) Outside(x, y float64, bounds Bounds) bool {
	if d == nil || !d.Enabled || !bounds.Valid() {
		return false
	}
	b := bounds.Expand(d.Margin)
	return x < b.MinX || x > b.MaxX || y < b.MinY || y > b.MaxY
}

// Fade drives an opacity envelope: FadeIn -> Wait -> FadeOut, optionally marking the object
// for destruction when finished.
type Fade struct {
	// FadeIn is the ramp-up duration from 0 to BaseAlpha in seconds (>= 0).
	FadeIn float64
	// Wait is the hold duration at BaseAlpha in seconds (>= 0).
	Wait float64
	// FadeOut is the ramp-down duration from BaseAlpha to 0 in seconds (>= 0).
	FadeOut float64
	// Destroy reports whether the owner should be freed upon completion.
	Destroy bool
	// BaseAlpha is the peak opacity in [0, 1] (0 defaults to 1 if BaseAlphaSet is false).
	BaseAlpha float64
	// Running controls whether Step advances the fade timer.
	Running bool
	// Enabled controls whether Step runs at all.
	Enabled bool

	elapsed  float64
	finished bool
}

// FadeStepResult reports the computed opacity and completion flags for one Fade.Step call.
type FadeStepResult struct {
	Alpha         float64
	Finished      bool
	ShouldDestroy bool
}

// NewFade returns a Fade behavior configured with fadeIn, wait, fadeOut (seconds) and destroy flag.
func NewFade(fadeIn, wait, fadeOut float64, destroy, autostart bool) Fade {
	return Fade{
		FadeIn:    max(0, fadeIn),
		Wait:      max(0, wait),
		FadeOut:   max(0, fadeOut),
		Destroy:   destroy,
		BaseAlpha: 1,
		Running:   autostart,
		Enabled:   true,
	}
}

// Start restarts the fade envelope from elapsed = 0.
func (f *Fade) Start() {
	if f == nil {
		return
	}
	f.elapsed = 0
	f.finished = false
	f.Running = true
}

// Stop pauses the fade envelope without resetting elapsed time.
func (f *Fade) Stop() {
	if f == nil {
		return
	}
	f.Running = false
}

// Alpha returns the opacity at the current elapsed time without advancing.
func (f *Fade) Alpha() float64 {
	if f == nil {
		return 1
	}
	return f.alphaAt(f.elapsed)
}

func (f *Fade) alphaAt(t float64) float64 {
	base := clamp(f.BaseAlpha, 0, 1)
	fadeIn := max(0, f.FadeIn)
	wait := max(0, f.Wait)
	fadeOut := max(0, f.FadeOut)

	if t < fadeIn && fadeIn > 0 {
		return base * clamp(t/fadeIn, 0, 1)
	}
	if t < fadeIn+wait {
		return base
	}
	total := fadeIn + wait + fadeOut
	if t < total && fadeOut > 0 {
		return base * clamp(1-(t-fadeIn-wait)/fadeOut, 0, 1)
	}
	return 0
}

// Step advances the fade timer by dt and returns the current alpha and whether the fade finished on this step.
func (f *Fade) Step(dt time.Duration) FadeStepResult {
	if f == nil || !f.Enabled || !f.Running || f.finished {
		return FadeStepResult{Alpha: f.Alpha()}
	}
	if dt <= 0 {
		return FadeStepResult{Alpha: f.Alpha()}
	}
	f.elapsed += dt.Seconds()
	total := max(0, f.FadeIn) + max(0, f.Wait) + max(0, f.FadeOut)
	if f.elapsed >= total {
		f.elapsed = total
		f.finished = true
		f.Running = false
		return FadeStepResult{
			Alpha:         0,
			Finished:      true,
			ShouldDestroy: f.Destroy,
		}
	}
	return FadeStepResult{Alpha: f.alphaAt(f.elapsed)}
}

// Flash toggles visibility on and off for a requested duration, then restores visibility.
type Flash struct {
	// On is the visible interval duration in seconds (>= 0).
	On float64
	// Off is the hidden interval duration in seconds (>= 0).
	Off float64
	// Enabled controls whether Step advances the flash.
	Enabled bool

	active   bool
	elapsed  float64
	duration float64
}

// FlashStepResult reports whether the object should be visible and whether the flash ended on this step.
type FlashStepResult struct {
	Active   bool
	Visible  bool
	Finished bool
}

// NewFlash returns an enabled (initially idle) Flash behavior.
func NewFlash(on, off float64) Flash {
	return Flash{
		On:      max(0, on),
		Off:     max(0, off),
		Enabled: true,
	}
}

// Trigger starts flashing for duration seconds (<= 0 stops immediately).
func (f *Flash) Trigger(duration float64) {
	if f == nil {
		return
	}
	if !isFinite(duration) || duration <= 0 {
		f.Stop()
		return
	}
	f.active = true
	f.elapsed = 0
	f.duration = duration
}

// Stop halts flashing immediately and restores visibility.
func (f *Flash) Stop() {
	if f == nil {
		return
	}
	f.active = false
	f.elapsed = 0
	f.duration = 0
}

// Active reports whether the flash is currently running.
func (f *Flash) Active() bool {
	return f != nil && f.active
}

// Visible reports whether the object should currently be drawn.
func (f *Flash) Visible() bool {
	if f == nil || !f.active {
		return true
	}
	off := max(0, f.Off)
	on := max(0, f.On)
	period := off + on
	if period <= 0 {
		return true
	}
	if off == 0 {
		return true
	}
	if on == 0 {
		return false
	}
	phase := positiveMod(f.elapsed, period)
	return phase >= off
}

// Step advances the flash timer by dt and returns visibility and completion state.
func (f *Flash) Step(dt time.Duration) FlashStepResult {
	if f == nil || !f.Enabled || !f.active {
		return FlashStepResult{Active: f != nil && f.active, Visible: f.Visible()}
	}
	if dt <= 0 {
		return FlashStepResult{Active: true, Visible: f.Visible()}
	}
	f.elapsed += dt.Seconds()
	if f.elapsed >= f.duration {
		f.active = false
		f.elapsed = 0
		f.duration = 0
		return FlashStepResult{Active: false, Visible: true, Finished: true}
	}
	return FlashStepResult{Active: true, Visible: f.Visible()}
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func positiveMod(a, m float64) float64 {
	r := math.Mod(a, m)
	if r < 0 {
		r += m
	}
	if r >= m {
		r = 0
	}
	return r
}
