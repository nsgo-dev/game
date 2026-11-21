// Package particles provides a deterministic 2D particle emitter stepped by game.Loop and drawn through sprite.Batch.
package particles

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	defaultMaxParticles = 256
	pcgStream           = 0x9e3779b97f4a7c15
)

// Particle is the live state of one emitted particle.
type Particle struct {
	// Frame is the atlas frame this particle draws.
	Frame sprite.Frame
	// X and Y are the particle's position in the drawing.
	X, Y float32
	// VX and VY are the particle's velocity in points per second.
	VX, VY float32
	// Age is how long the particle has lived so far.
	Age time.Duration
	// Lifetime is how long the particle lives before expiring.
	Lifetime time.Duration
	// Size is the particle's uniform scale evaluated from Emitter.SizeCurve.
	Size float32
	// Alpha is the particle's opacity in [0, 1], evaluated from Emitter.AlphaCurve and Emitter.ColorCurve.
	Alpha float32
	// Rotation is the particle's angle in radians, clockwise on the screen.
	Rotation float32
	// Color is the particle's color evaluated from Emitter.ColorCurve.
	Color core.Color

	initialRotation float32
	rotationSpeed   float32
}

// Emitter spawns, updates and draws 2D particles deterministically from a seeded PRNG.
//
// Call Update from a game.Loop's fixed step and Draw from a Canvas's Draw callback. Continuous emission runs whenever Rate > 0; call Burst to emit particles on demand regardless of Rate.
//
// Two emitters configured with the same fields and Seed and stepped identically produce bit-for-bit identical particles and draw operations.
type Emitter struct {
	// Atlas holds the image that Frames reference. Draw does nothing when Atlas is nil.
	Atlas *sprite.Atlas
	// Frames are the atlas frames particles choose from at spawn time.
	Frames []sprite.Frame
	// X and Y are the emitter's position in the drawing.
	X, Y float32
	// Shape controls where particles spawn relative to X, Y; its zero value is Point.
	Shape Shape
	// Rate is how many particles emit per second during Update; 0 or less disables continuous emission.
	Rate float32
	// MaxParticles caps how many particles may be live at once, 256 when 0 or less.
	MaxParticles int
	// LifetimeMin and LifetimeMax give how long each particle lives; both 0 defaults to one second, and a LifetimeMax below LifetimeMin matches LifetimeMin.
	LifetimeMin, LifetimeMax time.Duration
	// SpeedMin and SpeedMax give each particle's initial speed in points per second.
	SpeedMin, SpeedMax float32
	// AngleMin and AngleMax give each particle's initial direction of travel in radians, clockwise from +X.
	AngleMin, AngleMax float32
	// GravityX and GravityY are constant acceleration applied to live particles, in points per second squared.
	GravityX, GravityY float32
	// InitialRotationMin and InitialRotationMax give each particle's starting rotation in radians.
	InitialRotationMin, InitialRotationMax float32
	// RotationSpeedMin and RotationSpeedMax give each particle's angular velocity in radians per second.
	RotationSpeedMin, RotationSpeedMax float32
	// SizeCurve scales each particle over its normalized life in [0, 1]; empty scales by 1.
	SizeCurve FloatCurve
	// AlphaCurve fades each particle over its normalized life in [0, 1]; empty keeps opacity at 1.
	AlphaCurve FloatCurve
	// RotationCurve adds rotation in radians over a particle's normalized life in [0, 1]; empty adds 0.
	RotationCurve FloatCurve
	// ColorCurve evaluates a color over each particle's normalized life in [0, 1] and stores it on Particle.Color.
	// Sprite tinting waits for nsgo-dev/game#29, so Draw applies only ColorCurve's alpha channel (multiplied with AlphaCurve) today.
	ColorCurve ColorCurve
	// Seed seeds the emitter's PCG PRNG for deterministic emission and motion.
	Seed uint64
	// Z sets the sprite Z order used when drawing this emitter's particles.
	Z int

	particles []Particle
	sprites   []sprite.Sprite
	batch     sprite.Batch
	rng       *rand.Rand
	seeded    bool
	emitAcc   float64
}

// Update advances live particles by dt, removes expired ones, and emits new particles at Rate.
// Non-positive dt does nothing.
func (e *Emitter) Update(dt time.Duration) {
	if dt <= 0 {
		return
	}
	dtSec := float32(dt.Seconds())
	write := 0
	for i := range e.particles {
		p := e.particles[i]
		p.Age += dt
		if p.Age >= p.Lifetime {
			continue
		}
		p.VX += e.GravityX * dtSec
		p.VY += e.GravityY * dtSec
		p.X += p.VX * dtSec
		p.Y += p.VY * dtSec
		e.evalCurves(&p)
		e.particles[write] = p
		write++
	}
	clear(e.particles[write:])
	e.particles = e.particles[:write]

	if e.Rate > 0 {
		e.emitAcc += float64(e.Rate) * dt.Seconds()
		if e.emitAcc >= 1 {
			n := int(e.emitAcc)
			e.emitAcc -= float64(n)
			room := e.maxParticles() - len(e.particles)
			if n > room {
				n = room
			}
			if n > 0 {
				e.initRNG()
				e.ensureCap()
				for range n {
					e.spawn()
				}
			}
		}
	}
}

// Burst immediately emits up to count particles without waiting for Update or requiring Rate > 0, stopping at MaxParticles.
func (e *Emitter) Burst(count int) {
	if count <= 0 {
		return
	}
	room := e.maxParticles() - len(e.particles)
	if count > room {
		count = room
	}
	if count <= 0 {
		return
	}
	e.initRNG()
	e.ensureCap()
	for range count {
		e.spawn()
	}
}

// Draw draws all live particles into d through a reusable sprite.Batch, oldest first.
func (e *Emitter) Draw(d *core.Drawing) {
	if e.Atlas == nil || len(e.particles) == 0 {
		return
	}
	n := len(e.particles)
	capWant := max(n, e.maxParticles())
	if cap(e.sprites) < n {
		e.sprites = make([]sprite.Sprite, n, capWant)
	} else {
		e.sprites = e.sprites[:n]
	}
	if cap(e.batch.Sprites) < n {
		e.batch.Sprites = make([]*sprite.Sprite, n, capWant)
	} else {
		e.batch.Sprites = e.batch.Sprites[:n]
	}
	for i := range e.particles {
		p := &e.particles[i]
		ox, oy := p.Frame.Width/2, p.Frame.Height/2
		if ox == 0 {
			ox = p.Frame.W / 2
		}
		if oy == 0 {
			oy = p.Frame.H / 2
		}
		e.sprites[i] = sprite.Sprite{
			Frame:    p.Frame,
			X:        p.X,
			Y:        p.Y,
			OriginX:  ox,
			OriginY:  oy,
			Rotation: p.Rotation,
			ScaleX:   p.Size,
			ScaleY:   p.Size,
			Alpha:    p.Alpha,
			Z:        e.Z,
		}
		e.batch.Sprites[i] = &e.sprites[i]
	}
	e.batch.Atlas = e.Atlas
	e.batch.Draw(d)
}

// Count returns the number of currently live particles.
func (e *Emitter) Count() int { return len(e.particles) }

// Particles returns the live particles in spawn order.
// The returned slice is backed by the emitter's storage and reused on subsequent updates; copy it to retain a snapshot.
func (e *Emitter) Particles() []Particle { return e.particles }

// Clear removes all live particles and resets the fractional emission accumulator without resetting the PRNG.
func (e *Emitter) Clear() {
	clear(e.particles)
	e.particles = e.particles[:0]
	clear(e.batch.Sprites)
	e.batch.Sprites = e.batch.Sprites[:0]
	e.emitAcc = 0
}

// Reset clears all live particles and re-seeds the PRNG from Seed so subsequent updates replay identically.
func (e *Emitter) Reset() {
	e.Clear()
	e.rng = nil
	e.seeded = false
}

func (e *Emitter) maxParticles() int {
	if e.MaxParticles > 0 {
		return e.MaxParticles
	}
	return defaultMaxParticles
}

func (e *Emitter) initRNG() {
	if !e.seeded {
		e.rng = rand.New(rand.NewPCG(e.Seed, pcgStream))
		e.seeded = true
	}
}

func (e *Emitter) ensureCap() {
	capWant := e.maxParticles()
	if cap(e.particles) < capWant {
		next := make([]Particle, len(e.particles), capWant)
		copy(next, e.particles)
		e.particles = next
	}
}

func (e *Emitter) spawn() {
	var frame sprite.Frame
	switch len(e.Frames) {
	case 0:
	case 1:
		frame = e.Frames[0]
	default:
		frame = e.Frames[e.rng.IntN(len(e.Frames))]
	}

	dx, dy := e.Shape.sample(e.rng)
	speed := e.randFloat(e.SpeedMin, e.SpeedMax)
	angle := e.randFloat(e.AngleMin, e.AngleMax)
	sin, cos := math.Sincos(float64(angle))

	p := Particle{
		Frame:           frame,
		X:               e.X + dx,
		Y:               e.Y + dy,
		VX:              speed * float32(cos),
		VY:              speed * float32(sin),
		Lifetime:        e.nextLifetime(),
		initialRotation: e.randFloat(e.InitialRotationMin, e.InitialRotationMax),
		rotationSpeed:   e.randFloat(e.RotationSpeedMin, e.RotationSpeedMax),
	}
	e.evalCurves(&p)
	e.particles = append(e.particles, p)
}

func (e *Emitter) nextLifetime() time.Duration {
	lo, hi := e.LifetimeMin, e.LifetimeMax
	if lo <= 0 && hi <= 0 {
		return time.Second
	}
	if hi <= lo {
		if lo <= 0 {
			return time.Second
		}
		return lo
	}
	if lo < 1 {
		lo = 1
	}
	return lo + time.Duration(e.rng.Int64N(int64(hi-lo)+1))
}

func (e *Emitter) randFloat(lo, hi float32) float32 {
	if hi <= lo {
		return lo
	}
	return lo + e.rng.Float32()*(hi-lo)
}

func (e *Emitter) evalCurves(p *Particle) {
	var t float32
	if p.Lifetime > 0 {
		t = float32(p.Age) / float32(p.Lifetime)
	}
	t = clamp01(t)

	p.Size = e.SizeCurve.At(t)

	alpha := e.AlphaCurve.At(t)
	color := core.RGB(0xffffff)
	if len(e.ColorCurve) > 0 {
		color = e.ColorCurve.At(t)
		alpha *= float32(color&0xff) / 255
	}
	p.Color = color
	p.Alpha = clamp01(alpha)

	rot := p.initialRotation + p.rotationSpeed*float32(p.Age.Seconds())
	if len(e.RotationCurve) > 0 {
		rot += e.RotationCurve.At(t)
	}
	p.Rotation = rot
}
