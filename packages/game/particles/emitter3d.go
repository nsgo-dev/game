package particles

import (
	"encoding/binary"
	"math"
	"math/rand/v2"
	"time"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	defaultStretchFactor = float32(0.05)
	degenerateVec3LenSq  = float32(1e-12)
)

type shape3DKind uint8

const (
	shape3DPoint shape3DKind = iota
	shape3DSphere
	shape3DHemisphere
	shape3DBox
	shape3DCone
	shape3DRing
)

// Shape3D describes where an [Emitter3D] places each new particle relative to its Position.
// Its zero value is [Point3D], which emits at Position.
type Shape3D struct {
	kind                 shape3DKind
	radius               float32
	angle                float32
	width, height, depth float32
}

// Point3D returns a 3D shape that emits particles at the emitter's Position.
func Point3D() Shape3D { return Shape3D{kind: shape3DPoint} }

// Sphere returns a 3D shape that emits particles uniformly throughout a sphere of radius centered on Position.
func Sphere(radius float32) Shape3D { return Shape3D{kind: shape3DSphere, radius: radius} }

// Hemisphere returns a 3D shape that emits particles uniformly throughout the upper hemisphere (along the emitter's Direction) of radius centered on Position.
func Hemisphere(radius float32) Shape3D { return Shape3D{kind: shape3DHemisphere, radius: radius} }

// Box returns a 3D shape that emits particles uniformly inside a width × height × depth box centered on Position, aligned with the emitter's basis (width along side, height along Direction, depth along forward).
func Box(width, height, depth float32) Shape3D {
	return Shape3D{kind: shape3DBox, width: width, height: height, depth: depth}
}

// Cone returns a 3D shape that emits particles from a disk of radius perpendicular to Direction and directs them outward within a cone of half-angle angle (in radians, clamped to [0, pi/2]).
func Cone(radius, angle float32) Shape3D {
	return Shape3D{kind: shape3DCone, radius: radius, angle: angle}
}

// Ring3D returns a 3D shape that emits particles uniformly on the circumference of a circle of radius in the plane perpendicular to Direction.
func Ring3D(radius float32) Shape3D { return Shape3D{kind: shape3DRing, radius: radius} }

// String returns the canonical string name of the 3D shape kind.
func (s Shape3D) String() string {
	switch s.kind {
	case shape3DSphere:
		return "sphere"
	case shape3DHemisphere:
		return "hemisphere"
	case shape3DBox:
		return "box"
	case shape3DCone:
		return "cone"
	case shape3DRing:
		return "ring"
	default:
		return "point"
	}
}

// BillboardMode3D selects how [Emitter3D] orients each particle quad toward the camera when packing GPU instances.
type BillboardMode3D uint8

const (
	// BillboardSpherical aligns every particle quad with the camera's view plane (right and up axes), applying per-particle roll rotation. This is the zero value and fastest mode.
	BillboardSpherical BillboardMode3D = iota
	// BillboardCylindrical locks each quad's up axis to [Emitter3D.Axis] (defaulting to +Y) and rotates only around that axis to face the camera.
	BillboardCylindrical
	// BillboardStretched aligns each quad's height axis with the particle's velocity vector and stretches its length proportionally to speed while facing the camera across the velocity axis.
	BillboardStretched
)

// String returns the canonical string name of the billboard mode.
func (m BillboardMode3D) String() string {
	switch m {
	case BillboardCylindrical:
		return "cylindrical"
	case BillboardStretched:
		return "stretched"
	default:
		return "spherical"
	}
}

// ParticleBlendMode selects how 3D particles blend into the color target in a GPU pipeline.
type ParticleBlendMode uint8

const (
	// BlendAlpha uses standard non-premultiplied alpha blending (srcAlpha, 1-srcAlpha). Call [Emitter3D.SortByDepth] before packing for correct transparency order.
	BlendAlpha ParticleBlendMode = iota
	// BlendAdditive uses additive blending (srcAlpha, 1). Order-independent, so depth sorting is unnecessary for fire, sparks and glow effects.
	BlendAdditive
	// BlendPremultiplied uses premultiplied alpha blending (1, 1-srcAlpha).
	BlendPremultiplied
)

// String returns the canonical string name of the blend mode.
func (m ParticleBlendMode) String() string {
	switch m {
	case BlendAdditive:
		return "additive"
	case BlendPremultiplied:
		return "premultiplied"
	default:
		return "alpha"
	}
}

// Particle3D is a read-only snapshot of one live particle in an [Emitter3D].
type Particle3D struct {
	// Position is the particle's position in world space (or emitter-local space when Space is SpaceLocal).
	Position math3d.Vec3
	// Velocity is the particle's velocity in world units per second.
	Velocity math3d.Vec3
	// Age is how long the particle has lived so far.
	Age time.Duration
	// Lifetime is how long the particle lives before expiring.
	Lifetime time.Duration
	// Size is the particle's world-space quad size after applying SizeCurve.
	Size float32
	// Alpha is the particle's opacity in [0, 1] after applying AlphaCurve and ColorCurve.
	Alpha float32
	// Rotation is the particle's roll angle in radians.
	Rotation float32
	// Color is the particle's RGB color evaluated from ColorCurve (or Emitter3D.Color when empty) with alpha in the low byte.
	Color core.Color
}

// Emitter3DConfig is a serializable, reloadable snapshot of an [Emitter3D]'s configuration.
type Emitter3DConfig struct {
	Position           math3d.Vec3
	Direction          math3d.Vec3
	Up                 math3d.Vec3
	Rotation           math3d.Quat
	Space              SimulationSpace
	Shape              Shape3D
	Billboard          BillboardMode3D
	Blend              ParticleBlendMode
	StretchFactor      float32
	Axis               math3d.Vec3
	Rate               float32
	MaxParticles       int
	LifetimeMin        time.Duration
	LifetimeMax        time.Duration
	SpeedMin           float32
	SpeedMax           float32
	Spread             float32
	Drag               float32
	Gravity            math3d.Vec3
	SizeMin            float32
	SizeMax            float32
	InitialRotationMin float32
	InitialRotationMax float32
	RotationSpeedMin   float32
	RotationSpeedMax   float32
	SizeCurve          FloatCurve
	AlphaCurve         FloatCurve
	RotationCurve      FloatCurve
	ColorCurve         ColorCurve
	Color              core.Color
	Seed               uint64
}

func (c Emitter3DConfig) clone() Emitter3DConfig {
	out := c
	if len(c.SizeCurve) > 0 {
		out.SizeCurve = append(FloatCurve(nil), c.SizeCurve...)
	}
	if len(c.AlphaCurve) > 0 {
		out.AlphaCurve = append(FloatCurve(nil), c.AlphaCurve...)
	}
	if len(c.RotationCurve) > 0 {
		out.RotationCurve = append(FloatCurve(nil), c.RotationCurve...)
	}
	if len(c.ColorCurve) > 0 {
		out.ColorCurve = append(ColorCurve(nil), c.ColorCurve...)
	}
	return out
}

// Emitter3D spawns, updates and packs 3D particles deterministically from a seeded PRNG for instanced GPU rendering.
//
// It shares the emitter model of 2D [Emitter] (Rate, MaxParticles, LifetimeMin/Max, SpeedMin/Max, curves, SimulationSpace, Seed, Burst, Clear, Reset) and stores live particles in dense structure-of-arrays slices with swap-remove expiration so stepping and packing 10,000 particles allocates zero memory in steady state.
//
// Two emitters configured with the same fields and Seed and stepped identically produce bit-for-bit identical particles and packed GPU instance bytes.
type Emitter3D struct {
	// Position is the emitter's world-space origin.
	Position math3d.Vec3
	// Direction is the primary emission axis; zero defaults to +Y (0, 1, 0).
	Direction math3d.Vec3
	// Up is the reference up axis used to build the emitter's local frame; zero picks a deterministic perpendicular axis.
	Up math3d.Vec3
	// Rotation is an optional emitter orientation quaternion; zero is treated as identity. In SpaceLocal it rotates all live particles when packing; in SpaceWorld it rotates spawned offsets and velocities.
	Rotation math3d.Quat
	// Space controls whether particles simulate in world coordinates (SpaceWorld, default) or emitter-local coordinates (SpaceLocal).
	Space SimulationSpace
	// Shape controls where new particles spawn relative to Position; its zero value is Point3D.
	Shape Shape3D
	// Billboard selects how quads face the camera when AppendInstances packs GPU data.
	Billboard BillboardMode3D
	// Blend records the preferred GPU blend mode for pipelines built for this emitter.
	Blend ParticleBlendMode
	// StretchFactor scales velocity elongation in seconds for BillboardStretched (length = size + speed * StretchFactor); <= 0 defaults to 0.05.
	StretchFactor float32
	// Axis is the locked world up axis for BillboardCylindrical; zero defaults to +Y (0, 1, 0).
	Axis math3d.Vec3
	// Rate is how many particles emit per second during Step/Update; 0 or less disables continuous emission.
	Rate float32
	// MaxParticles caps how many particles may be live at once, 256 when 0 or less.
	MaxParticles int
	// LifetimeMin and LifetimeMax give how long each particle lives; both 0 defaults to one second, and a LifetimeMax below LifetimeMin matches LifetimeMin.
	LifetimeMin, LifetimeMax time.Duration
	// SpeedMin and SpeedMax give each particle's initial speed in world units per second.
	SpeedMin, SpeedMax float32
	// Spread is the emission cone half-angle in radians around Direction, clamped to [0, pi]. Zero emits straight along Direction; pi emits uniformly in all directions.
	Spread float32
	// Drag is exponential linear velocity damping in 1/s (>= 0); zero disables drag.
	Drag float32
	// Gravity is constant acceleration applied to live particles in world units per second squared.
	Gravity math3d.Vec3
	// SizeMin and SizeMax give each particle's base world-space quad size before SizeCurve; both <= 0 defaults to 1.
	SizeMin, SizeMax float32
	// InitialRotationMin and InitialRotationMax give each particle's starting roll angle in radians.
	InitialRotationMin, InitialRotationMax float32
	// RotationSpeedMin and RotationSpeedMax give each particle's roll angular velocity in radians per second.
	RotationSpeedMin, RotationSpeedMax float32
	// SizeCurve scales each particle's base size over its normalized age in [0, 1]; empty scales by 1.
	SizeCurve FloatCurve
	// AlphaCurve fades each particle over its normalized age in [0, 1]; empty keeps opacity at 1.
	AlphaCurve FloatCurve
	// RotationCurve adds roll rotation in radians over a particle's normalized age in [0, 1]; empty adds 0.
	RotationCurve FloatCurve
	// ColorCurve evaluates a color over each particle's normalized age in [0, 1]; empty uses Color (or opaque white when Color is 0).
	ColorCurve ColorCurve
	// Color is the base particle color used when ColorCurve is empty; zero means opaque white (0xffffffff).
	Color core.Color
	// Seed seeds the emitter's PCG PRNG for deterministic emission and motion.
	Seed uint64

	// Dense structure-of-arrays particle storage (0..count-1 are live).
	count    int
	pos      []math3d.Vec3
	vel      []math3d.Vec3
	age      []float32
	life     []float32
	invLife  []float32
	baseSize []float32
	initRot  []float32
	rotSpeed []float32
	size     []float32
	rot      []float32
	alpha    []float32
	color    []core.Color

	// Deterministic PRNG and continuous emission state.
	rng     *rand.Rand
	seeded  bool
	emitAcc float64

	// Reusable scratch buffers for zero-allocation depth sorting (4-pass LSB radix sort).
	sortKeys  []uint32
	sortOrder []int32
	sortTmpK  []uint32
	sortTmpO  []int32
	sorted    bool
}

// NewEmitter3D constructs an [Emitter3D] initialized from cfg.
func NewEmitter3D(cfg Emitter3DConfig) *Emitter3D {
	e := &Emitter3D{}
	e.ReloadConfig(cfg)
	return e
}

// Config returns a deep-copied [Emitter3DConfig] reflecting the emitter's current settings.
func (e *Emitter3D) Config() Emitter3DConfig {
	return Emitter3DConfig{
		Position:           e.Position,
		Direction:          e.Direction,
		Up:                 e.Up,
		Rotation:           e.Rotation,
		Space:              e.Space,
		Shape:              e.Shape,
		Billboard:          e.Billboard,
		Blend:              e.Blend,
		StretchFactor:      e.StretchFactor,
		Axis:               e.Axis,
		Rate:               e.Rate,
		MaxParticles:       e.MaxParticles,
		LifetimeMin:        e.LifetimeMin,
		LifetimeMax:        e.LifetimeMax,
		SpeedMin:           e.SpeedMin,
		SpeedMax:           e.SpeedMax,
		Spread:             e.Spread,
		Drag:               e.Drag,
		Gravity:            e.Gravity,
		SizeMin:            e.SizeMin,
		SizeMax:            e.SizeMax,
		InitialRotationMin: e.InitialRotationMin,
		InitialRotationMax: e.InitialRotationMax,
		RotationSpeedMin:   e.RotationSpeedMin,
		RotationSpeedMax:   e.RotationSpeedMax,
		SizeCurve:          e.SizeCurve,
		AlphaCurve:         e.AlphaCurve,
		RotationCurve:      e.RotationCurve,
		ColorCurve:         e.ColorCurve,
		Color:              e.Color,
		Seed:               e.Seed,
	}.clone()
}

// ReloadConfig applies cfg to e without clearing live particles or resetting the PRNG.
// If the new MaxParticles is smaller than the live particle count, excess particles are dropped.
func (e *Emitter3D) ReloadConfig(cfg Emitter3DConfig) {
	c := cfg.clone()
	e.Position = c.Position
	e.Direction = c.Direction
	e.Up = c.Up
	e.Rotation = c.Rotation
	e.Space = c.Space
	e.Shape = c.Shape
	e.Billboard = c.Billboard
	e.Blend = c.Blend
	e.StretchFactor = c.StretchFactor
	e.Axis = c.Axis
	e.Rate = c.Rate
	e.MaxParticles = c.MaxParticles
	e.LifetimeMin = c.LifetimeMin
	e.LifetimeMax = c.LifetimeMax
	e.SpeedMin = c.SpeedMin
	e.SpeedMax = c.SpeedMax
	e.Spread = c.Spread
	e.Drag = c.Drag
	e.Gravity = c.Gravity
	e.SizeMin = c.SizeMin
	e.SizeMax = c.SizeMax
	e.InitialRotationMin = c.InitialRotationMin
	e.InitialRotationMax = c.InitialRotationMax
	e.RotationSpeedMin = c.RotationSpeedMin
	e.RotationSpeedMax = c.RotationSpeedMax
	e.SizeCurve = c.SizeCurve
	e.AlphaCurve = c.AlphaCurve
	e.RotationCurve = c.RotationCurve
	e.ColorCurve = c.ColorCurve
	e.Color = c.Color
	e.Seed = c.Seed

	if capMax := e.maxParticles(); e.count > capMax {
		e.count = capMax
		e.sorted = false
	}
}

// Update advances live particles by dt, expires dead ones, and emits new particles at Rate.
// It is equivalent to [Emitter3D.Step] and matches 2D [Emitter.Update] for use in game.Loop.
func (e *Emitter3D) Update(dt time.Duration) {
	e.Step(dt)
}

// Step advances live particles by dt, removes expired ones via dense swap-remove, evaluates lifetime curves, and emits new particles at Rate.
// Non-positive dt does nothing.
func (e *Emitter3D) Step(dt time.Duration) {
	if dt <= 0 {
		return
	}
	e.sorted = false
	dtSec := float32(dt.Seconds())

	gx := e.Gravity.X * dtSec
	gy := e.Gravity.Y * dtSec
	gz := e.Gravity.Z * dtSec
	hasGravity := gx != 0 || gy != 0 || gz != 0

	hasDrag := e.Drag > 0
	var damp float32 = 1
	if hasDrag {
		damp = float32(math.Exp(float64(-e.Drag * dtSec)))
	}

	hasSizeCurve := len(e.SizeCurve) > 0
	hasAlphaCurve := len(e.AlphaCurve) > 0
	hasColorCurve := len(e.ColorCurve) > 0
	hasRotCurve := len(e.RotationCurve) > 0
	baseColor := e.baseColor()
	baseColorAlpha := float32(baseColor&0xff) / 255

	pos := e.pos
	vel := e.vel
	age := e.age
	life := e.life
	invLife := e.invLife
	baseSize := e.baseSize
	initRot := e.initRot
	rotSpeed := e.rotSpeed
	size := e.size
	rot := e.rot
	alpha := e.alpha
	col := e.color

	n := e.count
	for i := 0; i < n; {
		a := age[i] + dtSec
		if a >= life[i] {
			n--
			if i != n {
				pos[i] = pos[n]
				vel[i] = vel[n]
				age[i] = age[n]
				life[i] = life[n]
				invLife[i] = invLife[n]
				baseSize[i] = baseSize[n]
				initRot[i] = initRot[n]
				rotSpeed[i] = rotSpeed[n]
				size[i] = size[n]
				rot[i] = rot[n]
				alpha[i] = alpha[n]
				col[i] = col[n]
			}
			continue
		}
		age[i] = a

		v := vel[i]
		if hasGravity {
			v.X += gx
			v.Y += gy
			v.Z += gz
		}
		if hasDrag {
			v.X *= damp
			v.Y *= damp
			v.Z *= damp
		}
		vel[i] = v

		p := pos[i]
		p.X += v.X * dtSec
		p.Y += v.Y * dtSec
		p.Z += v.Z * dtSec
		pos[i] = p

		t := clamp01(a * invLife[i])
		if hasSizeCurve {
			size[i] = baseSize[i] * e.SizeCurve.At(t)
		} else {
			size[i] = baseSize[i]
		}

		al := float32(1)
		if hasAlphaCurve {
			al = e.AlphaCurve.At(t)
		}
		c := baseColor
		if hasColorCurve {
			c = e.ColorCurve.At(t)
			al *= float32(c&0xff) / 255
		} else {
			al *= baseColorAlpha
		}
		col[i] = c
		alpha[i] = clamp01(al)

		r := initRot[i] + rotSpeed[i]*a
		if hasRotCurve {
			r += e.RotationCurve.At(t)
		}
		rot[i] = r
		i++
	}
	e.count = n

	if e.Rate > 0 {
		e.emitAcc += float64(e.Rate) * dt.Seconds()
		if e.emitAcc >= 1 {
			emitN := int(e.emitAcc)
			e.emitAcc -= float64(emitN)
			room := e.maxParticles() - e.count
			if emitN > room {
				emitN = room
			}
			if emitN > 0 {
				e.emitBatch(emitN)
			}
		}
	}
}

// Burst immediately emits up to count particles without waiting for Step or requiring Rate > 0, stopping at MaxParticles.
func (e *Emitter3D) Burst(count int) {
	if count <= 0 {
		return
	}
	room := e.maxParticles() - e.count
	if count > room {
		count = room
	}
	if count <= 0 {
		return
	}
	e.sorted = false
	e.emitBatch(count)
}

// Count returns the number of currently live particles.
func (e *Emitter3D) Count() int { return e.count }

// Active reports whether the emitter currently has at least one live particle.
func (e *Emitter3D) Active() bool { return e.count > 0 }

// Particle returns a snapshot of the live particle at index i (0 <= i < Count()).
func (e *Emitter3D) Particle(i int) Particle3D {
	c := e.color[i]
	aByte := clampByte(e.alpha[i] * 255)
	return Particle3D{
		Position: e.pos[i],
		Velocity: e.vel[i],
		Age:      time.Duration(float64(e.age[i]) * float64(time.Second)),
		Lifetime: time.Duration(float64(e.life[i]) * float64(time.Second)),
		Size:     e.size[i],
		Alpha:    e.alpha[i],
		Rotation: e.rot[i],
		Color:    (c & 0xffffff00) | core.Color(aByte),
	}
}

// ParticleWorldPos returns the world-space position of particle i.
// For SpaceWorld emitters this returns the stored position directly; for SpaceLocal emitters it applies the emitter's Rotation and Position.
func (e *Emitter3D) ParticleWorldPos(i int) math3d.Vec3 {
	p := e.pos[i]
	if e.Space != SpaceLocal {
		return p
	}
	if rotM, ok := quatRotationMatrix(e.Rotation); ok {
		p = rotM.MulVec3(p)
	}
	return p.Add(e.Position)
}

// AppendParticles appends snapshots of all live particles to dst and returns the extended slice.
func (e *Emitter3D) AppendParticles(dst []Particle3D) []Particle3D {
	if e.count == 0 {
		return dst
	}
	for i := range e.count {
		dst = append(dst, e.Particle(i))
	}
	return dst
}

// Particles returns a slice containing snapshots of all live particles.
func (e *Emitter3D) Particles() []Particle3D {
	if e.count == 0 {
		return nil
	}
	return e.AppendParticles(make([]Particle3D, 0, e.count))
}

// Clear removes all live particles and resets the fractional emission accumulator without resetting the PRNG.
func (e *Emitter3D) Clear() {
	e.count = 0
	e.emitAcc = 0
	e.sorted = false
}

// Reset clears all live particles and re-seeds the PRNG from Seed so subsequent steps replay identically.
func (e *Emitter3D) Reset() {
	e.Clear()
	e.rng = nil
	e.seeded = false
}

// SortByDepth orders live particles back-to-front along camForward from camPos so alpha-blended particles pack farthest first in subsequent [Emitter3D.AppendInstances] calls.
//
// It uses a deterministic, zero-allocation 4-pass radix sort over IEEE-754 depth keys. The sorted order remains active until the next Step, Burst, Clear, or Reset.
func (e *Emitter3D) SortByDepth(camPos, camForward math3d.Vec3) {
	n := e.count
	if n == 0 {
		e.sorted = false
		return
	}
	fwd, ok := unitVec3(camForward)
	if !ok {
		fwd = math3d.Vec3Forward()
	}

	e.ensureSortScratch(n)
	keys := e.sortKeys[:n]
	order := e.sortOrder[:n]
	tmpK := e.sortTmpK[:n]
	tmpO := e.sortTmpO[:n]
	pos := e.pos[:n]

	if e.Space == SpaceLocal {
		// (emitterPos + R*localPos - camPos) · fwd = baseDepth + localPos · (R^T * fwd)
		baseDepth := e.Position.Sub(camPos).Dot(fwd)
		localFwd := fwd
		if rotM, hasRot := quatRotationMatrix(e.Rotation); hasRot {
			localFwd = rotM.Transpose().MulVec3(fwd)
		}
		for i := range n {
			p := pos[i]
			depth := baseDepth + p.X*localFwd.X + p.Y*localFwd.Y + p.Z*localFwd.Z
			keys[i] = float32SortKeyDesc(depth)
			order[i] = int32(i)
		}
	} else {
		cx, cy, cz := camPos.X, camPos.Y, camPos.Z
		fx, fy, fz := fwd.X, fwd.Y, fwd.Z
		for i := range n {
			p := pos[i]
			depth := (p.X-cx)*fx + (p.Y-cy)*fy + (p.Z-cz)*fz
			keys[i] = float32SortKeyDesc(depth)
			order[i] = int32(i)
		}
	}

	if n > 1 {
		radixSortKeys(keys, order, tmpK, tmpO)
	}
	e.sorted = true
}

// AppendInstances packs all live particles into dst in the little-endian 40-byte [ParticleInstanceStride] GPU layout facing (camRight, camUp) and returns the extended slice.
//
// If [Emitter3D.SortByDepth] was called since the last mutation, instances are written in back-to-front sorted order. It allocates only when dst lacks capacity for Count() * ParticleInstanceStride more bytes.
func (e *Emitter3D) AppendInstances(dst []byte, camRight, camUp math3d.Vec3) []byte {
	n := e.count
	if n == 0 {
		return dst
	}
	need := n * ParticleInstanceStride
	start := len(dst)
	if cap(dst)-start < need {
		next := make([]byte, start+need)
		copy(next, dst)
		dst = next
	} else {
		dst = dst[:start+need]
	}
	out := dst[start:]

	cRight, cUp, cBack := orthonormalCameraAxes(camRight, camUp)

	isLocal := e.Space == SpaceLocal
	var localMat math3d.Mat3
	hasLocalRot := false
	if isLocal {
		localMat, hasLocalRot = quatRotationMatrix(e.Rotation)
	}
	epos := e.Position

	// Precompute cylindrical frame once if needed.
	var cylRight, cylUp math3d.Vec3
	if e.Billboard == BillboardCylindrical {
		cylRight, cylUp = cylindricalAxes(e.Axis, cRight, cBack)
	}
	stretchFactor := e.StretchFactor
	if stretchFactor <= 0 {
		stretchFactor = defaultStretchFactor
	}

	useOrder := e.sorted && len(e.sortOrder) >= n
	order := e.sortOrder
	pos := e.pos
	vel := e.vel
	size := e.size
	rot := e.rot
	alpha := e.alpha
	col := e.color

	for k := range n {
		idx := k
		if useOrder {
			idx = int(order[k])
		}
		p := pos[idx]
		if isLocal {
			if hasLocalRot {
				p = localMat.MulVec3(p)
			}
			p.X += epos.X
			p.Y += epos.Y
			p.Z += epos.Z
		}

		sz := size[idx]
		if !(sz > 0) {
			sz = 0
		}

		var qRight, qUp math3d.Vec3
		switch e.Billboard {
		case BillboardCylindrical:
			qRight = math3d.Vec3{X: cylRight.X * sz, Y: cylRight.Y * sz, Z: cylRight.Z * sz}
			qUp = math3d.Vec3{X: cylUp.X * sz, Y: cylUp.Y * sz, Z: cylUp.Z * sz}
		case BillboardStretched:
			v := vel[idx]
			if isLocal && hasLocalRot {
				v = localMat.MulVec3(v)
			}
			lenSq := v.X*v.X + v.Y*v.Y + v.Z*v.Z
			if lenSq > degenerateVec3LenSq {
				spd := float32(math.Sqrt(float64(lenSq)))
				invSpd := 1 / spd
				vDir := math3d.Vec3{X: v.X * invSpd, Y: v.Y * invSpd, Z: v.Z * invSpd}
				side, ok := unitVec3(vDir.Cross(cBack))
				if !ok {
					side = cRight
				}
				length := sz + spd*stretchFactor
				qRight = math3d.Vec3{X: side.X * sz, Y: side.Y * sz, Z: side.Z * sz}
				qUp = math3d.Vec3{X: vDir.X * length, Y: vDir.Y * length, Z: vDir.Z * length}
			} else {
				qRight = math3d.Vec3{X: cRight.X * sz, Y: cRight.Y * sz, Z: cRight.Z * sz}
				qUp = math3d.Vec3{X: cUp.X * sz, Y: cUp.Y * sz, Z: cUp.Z * sz}
			}
		default:
			r := rot[idx]
			if r != 0 {
				sn, cs := math.Sincos(float64(r))
				sf, cf := float32(sn)*sz, float32(cs)*sz
				qRight = math3d.Vec3{
					X: cRight.X*cf + cUp.X*sf,
					Y: cRight.Y*cf + cUp.Y*sf,
					Z: cRight.Z*cf + cUp.Z*sf,
				}
				qUp = math3d.Vec3{
					X: cUp.X*cf - cRight.X*sf,
					Y: cUp.Y*cf - cRight.Y*sf,
					Z: cUp.Z*cf - cRight.Z*sf,
				}
			} else {
				qRight = math3d.Vec3{X: cRight.X * sz, Y: cRight.Y * sz, Z: cRight.Z * sz}
				qUp = math3d.Vec3{X: cUp.X * sz, Y: cUp.Y * sz, Z: cUp.Z * sz}
			}
		}

		c := col[idx]
		aByte := byte(clampByte(alpha[idx] * 255))

		base := k * ParticleInstanceStride
		slot := out[base : base+ParticleInstanceStride]
		binary.LittleEndian.PutUint32(slot[0:], math.Float32bits(p.X))
		binary.LittleEndian.PutUint32(slot[4:], math.Float32bits(p.Y))
		binary.LittleEndian.PutUint32(slot[8:], math.Float32bits(p.Z))
		binary.LittleEndian.PutUint32(slot[12:], math.Float32bits(qRight.X))
		binary.LittleEndian.PutUint32(slot[16:], math.Float32bits(qRight.Y))
		binary.LittleEndian.PutUint32(slot[20:], math.Float32bits(qRight.Z))
		binary.LittleEndian.PutUint32(slot[24:], math.Float32bits(qUp.X))
		binary.LittleEndian.PutUint32(slot[28:], math.Float32bits(qUp.Y))
		binary.LittleEndian.PutUint32(slot[32:], math.Float32bits(qUp.Z))
		slot[36] = byte(c >> 24)
		slot[37] = byte(c >> 16)
		slot[38] = byte(c >> 8)
		slot[39] = aByte
	}

	return dst
}

func (e *Emitter3D) maxParticles() int {
	if e.MaxParticles > 0 {
		return e.MaxParticles
	}
	return defaultMaxParticles
}

func (e *Emitter3D) baseColor() core.Color {
	if e.Color != 0 {
		return e.Color
	}
	return core.RGB(0xffffff)
}

func (e *Emitter3D) initRNG() {
	if !e.seeded {
		e.rng = rand.New(rand.NewPCG(e.Seed, pcgStream))
		e.seeded = true
	}
}

func (e *Emitter3D) ensureCap(want int) {
	if cap(e.pos) >= want {
		return
	}
	newCap := max(want, e.maxParticles())
	growVec3 := func(s []math3d.Vec3) []math3d.Vec3 {
		next := make([]math3d.Vec3, newCap)
		copy(next, s[:e.count])
		return next
	}
	growF32 := func(s []float32) []float32 {
		next := make([]float32, newCap)
		copy(next, s[:e.count])
		return next
	}
	growCol := func(s []core.Color) []core.Color {
		next := make([]core.Color, newCap)
		copy(next, s[:e.count])
		return next
	}
	e.pos = growVec3(e.pos)
	e.vel = growVec3(e.vel)
	e.age = growF32(e.age)
	e.life = growF32(e.life)
	e.invLife = growF32(e.invLife)
	e.baseSize = growF32(e.baseSize)
	e.initRot = growF32(e.initRot)
	e.rotSpeed = growF32(e.rotSpeed)
	e.size = growF32(e.size)
	e.rot = growF32(e.rot)
	e.alpha = growF32(e.alpha)
	e.color = growCol(e.color)
}

func (e *Emitter3D) ensureSortScratch(n int) {
	if cap(e.sortKeys) < n {
		newCap := max(n, e.maxParticles())
		e.sortKeys = make([]uint32, newCap)
		e.sortOrder = make([]int32, newCap)
		e.sortTmpK = make([]uint32, newCap)
		e.sortTmpO = make([]int32, newCap)
	}
}

func (e *Emitter3D) emitBatch(n int) {
	e.initRNG()
	e.ensureCap(e.count + n)

	side, dir, fwd := emitterBasis(e.Direction, e.Up)
	isLocal := e.Space == SpaceLocal
	var rotM math3d.Mat3
	hasRot := false
	if !isLocal {
		rotM, hasRot = quatRotationMatrix(e.Rotation)
	}

	spread := e.Spread
	if !(spread > 0) {
		spread = 0
	} else if spread > math.Pi {
		spread = math.Pi
	}
	oneMinusCosSpread := float64(1 - math.Cos(float64(spread)))

	hasSizeCurve := len(e.SizeCurve) > 0
	sizeAt0 := float32(1)
	if hasSizeCurve {
		sizeAt0 = e.SizeCurve.At(0)
	}
	alphaAt0 := float32(1)
	if len(e.AlphaCurve) > 0 {
		alphaAt0 = e.AlphaCurve.At(0)
	}
	colAt0 := e.baseColor()
	if len(e.ColorCurve) > 0 {
		colAt0 = e.ColorCurve.At(0)
	}
	alphaAt0 = clamp01(alphaAt0 * float32(colAt0&0xff) / 255)

	rotAt0 := float32(0)
	if len(e.RotationCurve) > 0 {
		rotAt0 = e.RotationCurve.At(0)
	}

	sizeLo, sizeHi := e.SizeMin, e.SizeMax
	if sizeLo <= 0 && sizeHi <= 0 {
		sizeLo, sizeHi = 1, 1
	} else if sizeHi < sizeLo {
		sizeHi = sizeLo
	}

	for range n {
		localOffset, shapeDir, hasShapeDir := e.Shape.sample(e.rng, side, dir, fwd)

		var velDir math3d.Vec3
		if hasShapeDir {
			if spread > 0 {
				sSide, sDir, sFwd := emitterBasis(shapeDir, dir)
				velDir = sampleConeDirection(e.rng, sSide, sDir, sFwd, oneMinusCosSpread)
			} else {
				velDir = shapeDir
			}
		} else if spread > 0 {
			velDir = sampleConeDirection(e.rng, side, dir, fwd, oneMinusCosSpread)
		} else {
			velDir = dir
		}

		speed := randFloat32(e.rng, e.SpeedMin, e.SpeedMax)
		vel := velDir.Scale(speed)

		var pos math3d.Vec3
		if isLocal {
			pos = localOffset
		} else {
			if hasRot {
				localOffset = rotM.MulVec3(localOffset)
				vel = rotM.MulVec3(vel)
			}
			pos = e.Position.Add(localOffset)
		}

		lifeSec := e.nextLifetimeSec()
		bSize := randFloat32(e.rng, sizeLo, sizeHi)
		iRot := randFloat32(e.rng, e.InitialRotationMin, e.InitialRotationMax)
		rSpd := randFloat32(e.rng, e.RotationSpeedMin, e.RotationSpeedMax)

		idx := e.count
		e.count++
		e.pos[idx] = pos
		e.vel[idx] = vel
		e.age[idx] = 0
		e.life[idx] = lifeSec
		e.invLife[idx] = 1 / lifeSec
		e.baseSize[idx] = bSize
		e.initRot[idx] = iRot
		e.rotSpeed[idx] = rSpd
		e.size[idx] = bSize * sizeAt0
		e.rot[idx] = iRot + rotAt0
		e.alpha[idx] = alphaAt0
		e.color[idx] = colAt0
	}
}

func (e *Emitter3D) nextLifetimeSec() float32 {
	lo, hi := e.LifetimeMin, e.LifetimeMax
	if lo <= 0 && hi <= 0 {
		return 1
	}
	if hi <= lo {
		if lo <= 0 {
			return 1
		}
		return float32(lo.Seconds())
	}
	if lo < 1 {
		lo = 1
	}
	d := lo + time.Duration(e.rng.Int64N(int64(hi-lo)+1))
	return float32(d.Seconds())
}

func (s Shape3D) sample(rng *rand.Rand, side, dir, fwd math3d.Vec3) (offset, outDir math3d.Vec3, hasDir bool) {
	switch s.kind {
	case shape3DSphere:
		if !(s.radius > 0) {
			return math3d.Vec3{}, math3d.Vec3{}, false
		}
		u := rng.Float64()
		v := rng.Float64()
		w := rng.Float64()
		r := float64(s.radius) * math.Cbrt(u)
		cosTheta := 2*v - 1
		sinTheta := math.Sqrt(max(0, 1-cosTheta*cosTheta))
		sinPhi, cosPhi := math.Sincos(2 * math.Pi * w)
		lx := float32(r * sinTheta * cosPhi)
		ly := float32(r * cosTheta)
		lz := float32(r * sinTheta * sinPhi)
		return combineBasis(side, dir, fwd, lx, ly, lz), math3d.Vec3{}, false

	case shape3DHemisphere:
		if !(s.radius > 0) {
			return math3d.Vec3{}, math3d.Vec3{}, false
		}
		u := rng.Float64()
		v := rng.Float64()
		w := rng.Float64()
		r := float64(s.radius) * math.Cbrt(u)
		cosTheta := v // [0, 1] -> upper hemisphere along dir
		sinTheta := math.Sqrt(max(0, 1-cosTheta*cosTheta))
		sinPhi, cosPhi := math.Sincos(2 * math.Pi * w)
		lx := float32(r * sinTheta * cosPhi)
		ly := float32(r * cosTheta)
		lz := float32(r * sinTheta * sinPhi)
		return combineBasis(side, dir, fwd, lx, ly, lz), math3d.Vec3{}, false

	case shape3DBox:
		var lx, ly, lz float32
		if s.width > 0 {
			lx = (rng.Float32() - 0.5) * s.width
		}
		if s.height > 0 {
			ly = (rng.Float32() - 0.5) * s.height
		}
		if s.depth > 0 {
			lz = (rng.Float32() - 0.5) * s.depth
		}
		return combineBasis(side, dir, fwd, lx, ly, lz), math3d.Vec3{}, false

	case shape3DCone:
		halfAngle := float64(s.angle)
		if !(halfAngle > 0) {
			halfAngle = 0
		} else if halfAngle > math.Pi*0.5 {
			halfAngle = math.Pi * 0.5
		}
		phi := rng.Float64() * 2 * math.Pi
		sinPhi, cosPhi := math.Sincos(phi)
		var radialFrac float64
		if s.radius > 0 {
			radialFrac = math.Sqrt(rng.Float64())
			dist := float64(s.radius) * radialFrac
			offset = combineBasis(side, dir, fwd, float32(dist*cosPhi), 0, float32(dist*sinPhi))
			tilt := halfAngle * radialFrac
			sinT, cosT := math.Sincos(tilt)
			outDir = combineBasis(side, dir, fwd, float32(sinT*cosPhi), float32(cosT), float32(sinT*sinPhi))
			return offset, outDir, true
		}
		if halfAngle > 0 {
			oneMinusCos := 1 - math.Cos(halfAngle)
			cosT := 1 - rng.Float64()*oneMinusCos
			sinT := math.Sqrt(max(0, 1-cosT*cosT))
			outDir = combineBasis(side, dir, fwd, float32(sinT*cosPhi), float32(cosT), float32(sinT*sinPhi))
			return math3d.Vec3{}, outDir, true
		}
		return math3d.Vec3{}, dir, true

	case shape3DRing:
		if !(s.radius > 0) {
			return math3d.Vec3{}, math3d.Vec3{}, false
		}
		phi := rng.Float64() * 2 * math.Pi
		sinPhi, cosPhi := math.Sincos(phi)
		r := float64(s.radius)
		return combineBasis(side, dir, fwd, float32(r*cosPhi), 0, float32(r*sinPhi)), math3d.Vec3{}, false

	default:
		return math3d.Vec3{}, math3d.Vec3{}, false
	}
}

func combineBasis(side, dir, fwd math3d.Vec3, lx, ly, lz float32) math3d.Vec3 {
	return math3d.Vec3{
		X: side.X*lx + dir.X*ly + fwd.X*lz,
		Y: side.Y*lx + dir.Y*ly + fwd.Y*lz,
		Z: side.Z*lx + dir.Z*ly + fwd.Z*lz,
	}
}

func sampleConeDirection(rng *rand.Rand, side, dir, fwd math3d.Vec3, oneMinusCos float64) math3d.Vec3 {
	cosTheta := 1 - rng.Float64()*oneMinusCos
	sinTheta := math.Sqrt(max(0, 1-cosTheta*cosTheta))
	sinPhi, cosPhi := math.Sincos(rng.Float64() * 2 * math.Pi)
	return combineBasis(side, dir, fwd, float32(sinTheta*cosPhi), float32(cosTheta), float32(sinTheta*sinPhi))
}

func randFloat32(rng *rand.Rand, lo, hi float32) float32 {
	if hi <= lo {
		return lo
	}
	return lo + rng.Float32()*(hi-lo)
}

func unitVec3(v math3d.Vec3) (math3d.Vec3, bool) {
	l := v.LenSq()
	if !(l > degenerateVec3LenSq) || math.IsInf(float64(l), 0) {
		return math3d.Vec3{}, false
	}
	inv := 1 / float32(math.Sqrt(float64(l)))
	return math3d.Vec3{X: v.X * inv, Y: v.Y * inv, Z: v.Z * inv}, true
}

// emitterBasis constructs a right-handed orthonormal frame (side, dir, fwd) with dir along the emitter's Direction (default +Y).
func emitterBasis(direction, up math3d.Vec3) (side, dir, fwd math3d.Vec3) {
	var ok bool
	dir, ok = unitVec3(direction)
	if !ok {
		dir = math3d.Vec3Up()
	}
	if ref, ok := unitVec3(up); ok {
		if s, ok := unitVec3(dir.Cross(ref)); ok {
			return s, dir, s.Cross(dir)
		}
	}
	ref := math3d.Vec3UnitZ()
	if math.Abs(float64(dir.Z)) > 0.9 {
		ref = math3d.Vec3UnitX()
	}
	side, _ = unitVec3(dir.Cross(ref))
	fwd = side.Cross(dir)
	return side, dir, fwd
}

func quatRotationMatrix(q math3d.Quat) (math3d.Mat3, bool) {
	lSq := q.X*q.X + q.Y*q.Y + q.Z*q.Z + q.W*q.W
	if !(lSq > degenerateVec3LenSq) || math.IsInf(float64(lSq), 0) {
		return math3d.Mat3{}, false
	}
	// Identity quaternion fast path.
	if q.X == 0 && q.Y == 0 && q.Z == 0 {
		return math3d.Mat3{}, false
	}
	return q.Norm().ToMat3(), true
}

func orthonormalCameraAxes(camRight, camUp math3d.Vec3) (right, up, back math3d.Vec3) {
	r, okR := unitVec3(camRight)
	u, okU := unitVec3(camUp)
	if !okR && !okU {
		return math3d.Vec3Right(), math3d.Vec3Up(), math3d.Vec3UnitZ()
	}
	if !okR {
		ref := math3d.Vec3UnitZ()
		if math.Abs(float64(u.Z)) > 0.9 {
			ref = math3d.Vec3UnitX()
		}
		r, _ = unitVec3(u.Cross(ref))
		return r, u, r.Cross(u)
	}
	if !okU {
		ref := math3d.Vec3UnitZ()
		if math.Abs(float64(r.Z)) > 0.9 {
			ref = math3d.Vec3UnitY()
		}
		u, _ = unitVec3(ref.Cross(r))
		return r, u, r.Cross(u)
	}
	b, okB := unitVec3(r.Cross(u))
	if !okB {
		ref := math3d.Vec3UnitZ()
		if math.Abs(float64(r.Z)) > 0.9 {
			ref = math3d.Vec3UnitY()
		}
		b, _ = unitVec3(r.Cross(ref))
	}
	u = b.Cross(r)
	return r, u, b
}

func cylindricalAxes(axis, camRight, camBack math3d.Vec3) (right, up math3d.Vec3) {
	u, ok := unitVec3(axis)
	if !ok {
		u = math3d.Vec3Up()
	}
	if n, ok := unitVec3(camBack.Sub(u.Scale(camBack.Dot(u)))); ok {
		return u.Cross(n), u
	}
	if r, ok := unitVec3(camRight.Sub(u.Scale(camRight.Dot(u)))); ok {
		return r, u
	}
	ref := math3d.Vec3UnitX()
	if math.Abs(float64(u.X)) > 0.9 {
		ref = math3d.Vec3UnitZ()
	}
	r, _ := unitVec3(ref.Sub(u.Scale(ref.Dot(u))))
	return r, u
}

// float32SortKeyDesc maps float32 f to a uint32 such that larger f values yield smaller uint32 keys (descending / back-to-front order). NaNs sort last.
func float32SortKeyDesc(f float32) uint32 {
	if f != f {
		return ^uint32(0)
	}
	u := math.Float32bits(f)
	if u>>31 != 0 {
		u = ^u
	} else {
		u ^= 0x80000000
	}
	return ^u
}

// radixSortKeys performs a stable 4-pass 8-bit LSB radix sort on keys, permuting order in lockstep.
func radixSortKeys(keys []uint32, order []int32, tmpK []uint32, tmpO []int32) {
	n := len(keys)
	srcK, dstK := keys, tmpK
	srcO, dstO := order, tmpO

	for shift := uint(0); shift < 32; shift += 8 {
		var counts [256]uint32
		for i := range n {
			counts[(srcK[i]>>shift)&0xff]++
		}
		// Skip pass if all keys share the same byte.
		var sum uint32
		singleBucket := false
		for b := range 256 {
			c := counts[b]
			if int(c) == n {
				singleBucket = true
				break
			}
			counts[b] = sum
			sum += c
		}
		if singleBucket {
			continue
		}
		for i := range n {
			k := srcK[i]
			bucket := (k >> shift) & 0xff
			pos := counts[bucket]
			counts[bucket] = pos + 1
			dstK[pos] = k
			dstO[pos] = srcO[i]
		}
		srcK, dstK = dstK, srcK
		srcO, dstO = dstO, srcO
	}

	if &srcO[0] != &order[0] {
		copy(order, srcO)
		copy(keys, srcK)
	}
}
