package vehicle

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/physics3d"
)

// Input is one step's driver input.
type Input struct {
	// Throttle in [-1, 1]: positive drives forward with EngineForce, negative drives backward with ReverseForce.
	Throttle float32
	// Brake in [0, 1] scales BrakeForce on brake wheels.
	Brake float32
	// Steer in [-1, 1]: positive steers right, negative left.
	Steer float32
	// Handbrake locks handbrake wheels and lowers their lateral grip to HandbrakeGrip.
	Handbrake bool
}

// clamped returns in with NaN values zeroed and every axis clamped to its documented range.
func (in Input) clamped() Input {
	return Input{
		Throttle:  clampAxis(in.Throttle, -1, 1),
		Brake:     clampAxis(in.Brake, 0, 1),
		Steer:     clampAxis(in.Steer, -1, 1),
		Handbrake: in.Handbrake,
	}
}

func clampAxis(v, lo, hi float32) float32 {
	if v != v {
		return 0
	}
	return min(max(v, lo), hi)
}

// Wheel is one wheel's configuration and the state Vehicle.Step left it in.
type Wheel struct {
	// Config is a copy of the wheel's configuration, for reading; Step uses the Vehicle's own copy, so changing it has no effect.
	Config WheelConfig
	// Contact reports whether the wheel's ray touched ground on the last step.
	Contact bool
	// ContactPoint is the world-space point where the wheel touches the ground.
	ContactPoint physics3d.Vec3
	// ContactNormal is the ground's surface normal at ContactPoint.
	ContactNormal physics3d.Vec3
	// Ground is the body under the wheel, or nil for no contact or a triangle mesh or heightfield.
	Ground *physics3d.Body
	// SuspensionLength is the current suspension length, from 0 (fully compressed) to RestLength (fully extended).
	SuspensionLength float32
	// SuspensionForce is the suspension force in newtons pushing the chassis up on the last step.
	SuspensionForce float32
	// SteerAngle is the wheel's steering angle in radians about the chassis' up axis; positive turns left.
	SteerAngle float32
	// SpinSpeed is the wheel's rolling speed in radians per second; positive rolls forward.
	SpinSpeed float32
	// SpinAngle is the accumulated rolling angle in radians, wrapped to [-π, π].
	SpinAngle float32
	// Sliding reports whether the tire force hit its friction limit on the last step, such as for skid marks or tire squeal.
	Sliding bool
}

// Vehicle is a raycast car: a dynamic chassis body in a physics3d.World and the wheels that hold it up.
type Vehicle struct {
	// Body is the chassis; its Pos is the center of mass.
	Body *physics3d.Body
	// Wheels holds each wheel's state, in Config.Wheels order.
	Wheels []Wheel
	// RayMask selects the layers wheel rays hit (0 hits every layer). Trigger bodies and the chassis itself are always skipped.
	RayMask physics3d.LayerMask

	cfg    Config
	input  Input
	steer  float32
	nDrive int
	nBrake int
	tires  []tireImpulse
}

// tireImpulse is one wheel's tire impulse for the current step, applied after every wheel's is computed.
type tireImpulse struct {
	long, side physics3d.Vec3
	r, rSide   physics3d.Vec3
	active     bool
}

// New validates cfg, adds the chassis to world at pos with orientation rot, and returns the Vehicle. The chassis rests on its suspension once World.Step runs; place pos about RestLength + Radius above the ground below each anchor.
func New(world *physics3d.World, cfg Config, pos physics3d.Vec3, rot physics3d.Quat) (*Vehicle, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg.Wheels = append([]WheelConfig(nil), cfg.Wheels...)
	body := physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{
		Center:      vec(cfg.ChassisOffset),
		HalfExtents: vec(cfg.ChassisHalfExtents),
	}), pos, cfg.Mass)
	body.Rot = rot.Norm()
	body.LinearDamping = 0
	body.AngularDamping = cfg.AngularDamping
	body.Friction = 0.3
	v := &Vehicle{
		Body:   world.AddBody(body),
		cfg:    cfg,
		Wheels: make([]Wheel, len(cfg.Wheels)),
		tires:  make([]tireImpulse, len(cfg.Wheels)),
	}
	for i, wc := range cfg.Wheels {
		v.Wheels[i] = Wheel{Config: wc, SuspensionLength: wc.RestLength}
		if wc.Drive {
			v.nDrive++
		}
		if wc.Brake {
			v.nBrake++
		}
	}
	return v, nil
}

func vec(a [3]float32) physics3d.Vec3 {
	return physics3d.Vec3{X: a[0], Y: a[1], Z: a[2]}
}

// Config returns a copy of the car's tuning, as passed to New. The tuning is fixed for the Vehicle's lifetime; to drive with different tuning, remove the chassis from the world and create a new Vehicle.
func (v *Vehicle) Config() Config {
	cfg := v.cfg
	cfg.Wheels = append([]WheelConfig(nil), v.cfg.Wheels...)
	return cfg
}

// SetInput sets the input the next Step applies, clamping each axis to its range and treating NaN as 0. Input stays in effect until it is set again.
func (v *Vehicle) SetInput(in Input) {
	v.input = in.clamped()
	if v.Body.Sleeping && v.input != (Input{}) {
		v.Body.Wake()
	}
}

// Input returns the input the next Step applies.
func (v *Vehicle) Input() Input { return v.input }

// Forward returns the chassis' world-space forward axis (local -Z).
func (v *Vehicle) Forward() physics3d.Vec3 {
	return v.Body.Rot.RotateVec(physics3d.Vec3{Z: -1})
}

// Up returns the chassis' world-space up axis (local +Y).
func (v *Vehicle) Up() physics3d.Vec3 {
	return v.Body.Rot.RotateVec(physics3d.Vec3{Y: 1})
}

// Speed returns the chassis' signed speed along Forward in m/s; negative while reversing.
func (v *Vehicle) Speed() float32 {
	return v.Body.Vel.Dot(v.Forward())
}

// Grounded returns how many wheels touched the ground on the last Step.
func (v *Vehicle) Grounded() int {
	n := 0
	for i := range v.Wheels {
		if v.Wheels[i].Contact {
			n++
		}
	}
	return n
}

// Reset teleports the chassis to pos and rot, stops it, centers the steering and clears every wheel's contact, such as to respawn at a checkpoint.
func (v *Vehicle) Reset(pos physics3d.Vec3, rot physics3d.Quat) {
	b := v.Body
	b.Pos = pos
	b.Rot = rot.Norm()
	b.Vel = physics3d.Vec3{}
	b.AngVel = physics3d.Vec3{}
	b.Wake()
	v.steer = 0
	v.input = Input{}
	for i := range v.Wheels {
		w := &v.Wheels[i]
		wc := &v.cfg.Wheels[i]
		*w = Wheel{Config: *wc, SuspensionLength: wc.RestLength}
	}
}

// WheelTransform returns wheel i's world-space center and orientation, including steering and spin, for drawing its mesh. The mesh's axle is its local X axis.
func (v *Vehicle) WheelTransform(i int) (physics3d.Vec3, physics3d.Quat) {
	w := &v.Wheels[i]
	wc := &v.cfg.Wheels[i]
	rot := v.Body.Rot
	anchor := v.Body.Pos.Add(rot.RotateVec(vec(wc.Anchor)))
	center := anchor.Add(rot.RotateVec(physics3d.Vec3{Y: -w.SuspensionLength}))
	steer := physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, w.SteerAngle)
	// Rolling forward (toward -Z) turns the wheel's top toward -Z, a negative rotation about +X.
	spin := physics3d.QuatFromAxisAngle(physics3d.Vec3{X: 1}, -w.SpinAngle)
	return center, rot.Mul(steer).Mul(spin).Norm()
}

// Step advances the car's steering, suspension and tires by dt and applies their impulses to the chassis (and to dynamic bodies under the wheels). Call it once per fixed step, before World.Step.
func (v *Vehicle) Step(world *physics3d.World, dt time.Duration) {
	h := float32(dt.Seconds())
	if h <= 0 {
		return
	}
	b := v.Body
	cfg := &v.cfg
	in := v.input

	// Steering eases toward the requested angle at SteerRate.
	target := -in.Steer * deg2rad(cfg.MaxSteerAngle)
	maxDelta := deg2rad(cfg.SteerRate) * h
	v.steer += clampAxis(target-v.steer, -maxDelta, maxDelta)

	rot := b.Rot
	up := rot.RotateVec(physics3d.Vec3{Y: 1})
	fwd := rot.RotateVec(physics3d.Vec3{Z: -1})
	down := up.Neg()

	// 1. Wheel rays and suspension forces.
	grounded := 0
	for i := range v.Wheels {
		w := &v.Wheels[i]
		wc := &v.cfg.Wheels[i]
		w.SteerAngle = 0
		if wc.Steer {
			w.SteerAngle = v.steer
		}
		w.Sliding = false
		anchor := b.Pos.Add(rot.RotateVec(vec(wc.Anchor)))
		rayLen := wc.RestLength + wc.Radius
		hit, ok := v.castRay(world, anchor, anchor.Add(down.Scale(rayLen)))
		if !ok {
			w.Contact = false
			w.Ground = nil
			w.ContactPoint = physics3d.Vec3{}
			w.ContactNormal = physics3d.Vec3{}
			w.SuspensionLength = wc.RestLength
			w.SuspensionForce = 0
			continue
		}
		grounded++
		w.Contact = true
		w.Ground = hit.Body
		w.ContactPoint = hit.Point
		w.ContactNormal = hit.Normal
		w.SuspensionLength = clampAxis(hit.Distance-wc.Radius, 0, wc.RestLength)
		compression := wc.RestLength - w.SuspensionLength
		// Positive relative speed means the anchor is moving away from the ground (extending).
		rel := pointVel(b, anchor).Sub(groundVel(w.Ground, hit.Point)).Dot(up)
		force := wc.Stiffness*compression - wc.Damping*rel
		if force < 0 {
			force = 0
		}
		if wc.MaxForce > 0 && force > wc.MaxForce {
			force = wc.MaxForce
		}
		w.SuspensionForce = force
	}

	// Throttle is the only input that moves a resting car, so a throttle set once and held, such as against a wall, keeps the chassis awake; a held brake or handbrake lets it sleep.
	if b.Sleeping && in.Throttle != 0 {
		b.Wake()
	}
	if b.Sleeping {
		// A settled car with no input stays asleep; its wheel contacts above remain current for drawing.
		v.spinWheels(h)
		return
	}

	for i := range v.Wheels {
		w := &v.Wheels[i]
		wc := &v.cfg.Wheels[i]
		if !w.Contact || w.SuspensionForce == 0 {
			continue
		}
		anchor := b.Pos.Add(rot.RotateVec(vec(wc.Anchor)))
		j := up.Scale(w.SuspensionForce * h)
		applyAt(b, j, anchor.Sub(b.Pos))
		pushGround(w.Ground, j.Neg(), w.ContactPoint)
	}

	// 2. Tire forces at each contact, limited by a friction circle of Friction * load.
	speed := b.Vel.Dot(fwd)
	var drive float32
	switch {
	case in.Throttle > 0 && v.nDrive > 0:
		drive = cfg.EngineForce * in.Throttle * max(0, 1-max(speed, 0)/cfg.MaxSpeed) / float32(v.nDrive)
	case in.Throttle < 0 && v.nDrive > 0:
		drive = cfg.ReverseForce * in.Throttle * max(0, 1-max(-speed, 0)/cfg.MaxReverseSpeed) / float32(v.nDrive)
	}
	var brake float32
	if v.nBrake > 0 {
		brake = cfg.BrakeForce * in.Brake / float32(v.nBrake)
	}

	for i := range v.Wheels {
		v.tires[i] = tireImpulse{}
		w := &v.Wheels[i]
		wc := &v.cfg.Wheels[i]
		if !w.Contact {
			continue
		}
		n := w.ContactNormal
		wheelFwd := fwd
		if w.SteerAngle != 0 {
			wheelFwd = physics3d.QuatFromAxisAngle(up, w.SteerAngle).RotateVec(fwd)
		}
		f := wheelFwd.Sub(n.Scale(wheelFwd.Dot(n))).Norm()
		if f.LenSq() == 0 {
			continue
		}
		side := f.Cross(n)
		r := w.ContactPoint.Sub(b.Pos)
		vel := pointVel(b, w.ContactPoint).Sub(groundVel(w.Ground, w.ContactPoint))
		vf := vel.Dot(f)
		vs := vel.Dot(side)
		w.SpinSpeed = vf / wc.Radius
		maxJ := wc.Friction * w.SuspensionForce * h

		// Longitudinal: drive, brakes and rolling resistance. Braking can at most stop the wheel's share of the car.
		stopJ := -vf * effMass(b, r, f) / float32(grounded)
		var jf float32
		if wc.Drive {
			jf += drive * h
		}
		locked := in.Handbrake && wc.Handbrake
		switch {
		case locked:
			jf = stopJ
			w.SpinSpeed = 0
		default:
			resist := cfg.RollingResistance * w.SuspensionForce * h
			if wc.Brake {
				resist += brake * h
			}
			jf += towardZero(stopJ, resist)
		}

		// Lateral: cancel this wheel's share of the sideways slip.
		js := -vs * effMass(b, r, side) / float32(grounded)
		if locked {
			js *= cfg.HandbrakeGrip
		}

		if mag := hypot(jf, js); mag > maxJ {
			if mag > 0 {
				s := maxJ / mag
				jf *= s
				js *= s
			}
			w.Sliding = true
		}

		// Lateral force rolls the chassis only by RollInfluence of the contact's height below the center of mass.
		v.tires[i] = tireImpulse{
			long:   f.Scale(jf),
			side:   side.Scale(js),
			r:      r,
			rSide:  r.Sub(up.Scale(r.Dot(up) * (1 - cfg.RollInfluence))),
			active: true,
		}
	}
	// Every wheel's impulse is computed from the same chassis state and applied afterwards, so the result does not depend on wheel order and a symmetric car drives straight.
	for i := range v.Wheels {
		t := &v.tires[i]
		if !t.active {
			continue
		}
		w := &v.Wheels[i]
		applyAt(b, t.long, t.r)
		applyAt(b, t.side, t.rSide)
		pushGround(w.Ground, t.long.Add(t.side).Neg(), w.ContactPoint)
	}

	// 3. Aerodynamic drag.
	if cfg.Drag > 0 {
		vl := b.Vel.Len()
		if vl > 0 {
			// Never reverse the velocity within one step.
			j := min(cfg.Drag*vl*vl*h, vl*cfg.Mass)
			applyAt(b, b.Vel.Scale(-j/vl), physics3d.Vec3{})
		}
	}

	v.spinWheels(h)
}

// spinWheels advances each wheel's rolling angle; airborne wheels keep spinning and slowly coast down.
func (v *Vehicle) spinWheels(h float32) {
	for i := range v.Wheels {
		w := &v.Wheels[i]
		wc := &v.cfg.Wheels[i]
		if !w.Contact {
			w.SpinSpeed *= 1 / (1 + h)
		}
		if v.input.Handbrake && wc.Handbrake {
			w.SpinSpeed = 0
		}
		w.SpinAngle = float32(math.Remainder(float64(w.SpinAngle+w.SpinSpeed*h), 2*math.Pi))
	}
}

// castRay returns the closest hit between from and to, skipping the chassis and trigger bodies.
func (v *Vehicle) castRay(world *physics3d.World, from, to physics3d.Vec3) (physics3d.RaycastHit, bool) {
	mask := v.RayMask
	if mask == 0 {
		mask = physics3d.LayerAll
	}
	for _, hit := range world.RaycastLayer(from, to, mask) {
		if hit.Body != nil && (hit.Body == v.Body || hit.Body.IsTrigger) {
			continue
		}
		return hit, true
	}
	return physics3d.RaycastHit{}, false
}

// pointVel returns the velocity of the point p fixed to body b.
func pointVel(b *physics3d.Body, p physics3d.Vec3) physics3d.Vec3 {
	return b.Vel.Add(b.AngVel.Cross(p.Sub(b.Pos)))
}

// groundVel returns the velocity of the ground at p, zero for static colliders.
func groundVel(g *physics3d.Body, p physics3d.Vec3) physics3d.Vec3 {
	if g == nil || g.Static {
		return physics3d.Vec3{}
	}
	return pointVel(g, p)
}

// activeInv returns b's inverse mass and inertia as the solver sees them: zero while static, kinematic or asleep.
func activeInv(b *physics3d.Body) (float32, float32) {
	if b.Static || b.Kinematic || b.Sleeping {
		return 0, 0
	}
	return b.InvMass, b.InvInertia
}

// effMass returns the effective mass of b along unit direction d at offset r from its center of mass.
func effMass(b *physics3d.Body, r, d physics3d.Vec3) float32 {
	invM, invI := activeInv(b)
	k := invM + invI*r.Cross(d).LenSq()
	if k <= 0 {
		return 0
	}
	return 1 / k
}

// applyAt adds impulse j at offset r from b's center of mass. It changes velocities directly instead of calling Body.ApplyImpulse so the suspension does not wake a settled car every step.
func applyAt(b *physics3d.Body, j, r physics3d.Vec3) {
	invM, invI := activeInv(b)
	if invM > 0 {
		b.Vel = b.Vel.Add(j.Scale(invM))
	}
	if invI > 0 {
		b.AngVel = b.AngVel.Add(r.Cross(j).Scale(invI))
	}
}

// pushGround applies the reaction impulse j at p to a dynamic ground body, waking it.
func pushGround(g *physics3d.Body, j, p physics3d.Vec3) {
	if g == nil || g.Static || g.Kinematic {
		return
	}
	g.ApplyImpulse(j)
	g.ApplyAngularImpulse(p.Sub(g.Pos).Cross(j))
}

// towardZero returns the impulse that moves toward stop without overshooting it, limited to magnitude limit.
func towardZero(stop, limit float32) float32 {
	if stop > 0 {
		return min(stop, limit)
	}
	return max(stop, -limit)
}

func hypot(a, b float32) float32 {
	return float32(math.Sqrt(float64(a*a + b*b)))
}

func deg2rad(d float32) float32 {
	return d * (math.Pi / 180)
}
