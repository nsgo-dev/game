// Package particles provides a deterministic 2D particle emitter stepped by game.Loop and drawn through sprite.Batch.
package particles

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/nsgo-dev/game/pkg/sprite"
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
	// X and Y are the particle's position in the drawing (or emitter-local coordinates when Emitter.Space is SpaceLocal).
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

// Burst schedules an automatic burst of particles during Emitter.Update.
type Burst struct {
	// Time is the elapsed emitter time when the first burst fires (>= 0).
	Time time.Duration
	// Count is how many particles to emit on each firing (> 0).
	Count int
	// Cycles is how many times the burst fires: 0 or 1 means once, and a negative value repeats indefinitely every Interval.
	Cycles int
	// Interval is the delay between repeated firings when Cycles != 1 (must be > 0 to repeat).
	Interval time.Duration
}

// Emitter spawns, updates and draws 2D particles deterministically from a seeded PRNG.
//
// Call Update from a game.Loop's fixed step and Draw from a Canvas's Draw callback. Continuous emission runs whenever Rate > 0; call Burst to emit particles on demand regardless of Rate.
//
// Two emitters configured with the same fields and Seed and stepped identically produce bit-for-bit identical particles and draw operations.
type Emitter struct {
	// Atlas holds the image that Frames reference. Draw skips particle sprites when Atlas is nil.
	Atlas *sprite.Atlas
	// Frames are the atlas frames particles choose from at spawn time.
	Frames []sprite.Frame
	// X and Y are the emitter's position in the drawing.
	X, Y float32
	// Rotation is the emitter's rotation in radians, clockwise on the screen.
	Rotation float32
	// Space controls whether particles simulate in world space (SpaceWorld, default) or emitter-local space (SpaceLocal).
	Space SimulationSpace
	// Shape controls where particles spawn relative to X, Y; its zero value is Point.
	Shape Shape
	// Rate is how many particles emit per second during Update; 0 or less disables continuous emission.
	Rate float32
	// Bursts schedules automatic bursts of particles at specific elapsed times during Update.
	Bursts []Burst
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
	// ColorCurve evaluates a color over each particle's normalized life in [0, 1] and stores it on Particle.Color. Draw tints each particle's sprite by the color's RGB, and its alpha channel multiplies AlphaCurve into Particle.Alpha.
	ColorCurve ColorCurve
	// Seed seeds the emitter's PCG PRNG for deterministic emission and motion.
	Seed uint64
	// Z sets the sprite Z order used when drawing this emitter's particles.
	Z int
	// Collision configures optional particle collision detection and response against a Collider.
	Collision CollisionConfig
	// Trail, when non-nil, automatically attaches a 2D ribbon trail to each emitted particle.
	Trail *TrailConfig
	// SubEmitters configures child bursts triggered on particle birth, death, or collision.
	SubEmitters []SubEmitterRule

	particles  []Particle
	sprites    []sprite.Sprite
	batch      sprite.Batch
	rng        *rand.Rand
	seeded     bool
	emitAcc    float64
	elapsed    time.Duration
	burstFired []int64

	attachFn          func() (float32, float32)
	attachTransformFn func() (float32, float32, float32)
	prevX, prevY      float32
	hasPrevPos        bool

	particleTrails []*Trail
	fadingTrails   []*Trail
	trailPool      []*Trail

	children []*Emitter
	subDepth int
}

// Update advances live particles by dt, removes expired ones, resolves collisions, updates trails and sub-emitters, and emits new particles at Rate.
// Non-positive dt does nothing.
func (e *Emitter) Update(dt time.Duration) {
	if dt <= 0 {
		return
	}

	// Sync position/rotation from any attached target.
	if e.attachTransformFn != nil {
		if !e.hasPrevPos {
			e.prevX, e.prevY = e.X, e.Y
			e.hasPrevPos = true
		}
		e.X, e.Y, e.Rotation = e.attachTransformFn()
	} else if e.attachFn != nil {
		if !e.hasPrevPos {
			e.prevX, e.prevY = e.X, e.Y
			e.hasPrevPos = true
		}
		e.X, e.Y = e.attachFn()
	}

	// Advance existing child sub-emitters before triggering new bursts from this step's events.
	for _, ch := range e.children {
		if ch != nil {
			ch.Update(dt)
		}
	}

	dtSec := float32(dt.Seconds())

	// Update fading trails from previously expired particles.
	if len(e.fadingTrails) > 0 {
		fw := 0
		for _, tr := range e.fadingTrails {
			tr.Update(dtSec)
			if tr.PointCount() > 0 {
				e.fadingTrails[fw] = tr
				fw++
			} else {
				e.releaseTrail(tr)
			}
		}
		clear(e.fadingTrails[fw:])
		e.fadingTrails = e.fadingTrails[:fw]
	}

	collideActive := e.Collision.Enabled && e.Collision.Mode != CollideNone && e.Collision.Collider != nil
	hasSubs := len(e.SubEmitters) > 0 && e.subDepth < maxSubEmitterDepth
	hasTrails := e.Trail != nil || len(e.particleTrails) > 0
	isLocal := e.Space == SpaceLocal

	write := 0
	for i := range e.particles {
		p := e.particles[i]
		var tr *Trail
		if hasTrails && i < len(e.particleTrails) {
			tr = e.particleTrails[i]
		}

		p.Age += dt
		if p.Age >= p.Lifetime {
			if tr != nil {
				tr.Update(dtSec)
				if tr.PointCount() > 0 {
					e.fadingTrails = append(e.fadingTrails, tr)
				} else {
					e.releaseTrail(tr)
				}
			}
			if hasSubs {
				wx, wy := e.localToWorld(p.X, p.Y)
				wvx, wvy := e.localVelToWorld(p.VX, p.VY)
				e.triggerSubEmitters(TriggerOnDeath, wx, wy, wvx, wvy)
			}
			continue
		}

		prevPX, prevPY := p.X, p.Y
		p.VX += e.GravityX * dtSec
		p.VY += e.GravityY * dtSec
		p.X += p.VX * dtSec
		p.Y += p.VY * dtSec

		if collideActive {
			wPrevX, wPrevY := prevPX, prevPY
			wx, wy := p.X, p.Y
			if isLocal {
				wPrevX, wPrevY = e.localToWorld(prevPX, prevPY)
				wx, wy = e.localToWorld(p.X, p.Y)
			}
			hit, hitX, hitY, nx, ny := e.Collision.Collider.ResolveParticle(wPrevX, wPrevY, wx, wy, e.Collision.Radius)
			if hit {
				var wvx, wvy float32
				if hasSubs {
					wvx, wvy = e.localVelToWorld(p.VX, p.VY)
					e.triggerSubEmitters(TriggerOnCollision, hitX, hitY, wvx, wvy)
				}
				switch e.Collision.Mode {
				case CollideKill:
					if tr != nil {
						tr.Update(dtSec)
						if tr.PointCount() > 0 {
							e.fadingTrails = append(e.fadingTrails, tr)
						} else {
							e.releaseTrail(tr)
						}
					}
					if hasSubs {
						e.triggerSubEmitters(TriggerOnDeath, hitX, hitY, wvx, wvy)
					}
					continue
				case CollideStop:
					if isLocal {
						p.X, p.Y = e.worldToLocal(hitX, hitY)
					} else {
						p.X, p.Y = hitX, hitY
					}
					p.VX, p.VY = 0, 0
				case CollideBounce:
					lnx, lny := nx, ny
					if isLocal {
						p.X, p.Y = e.worldToLocal(hitX, hitY)
						lnx, lny = e.worldDirToLocal(nx, ny)
					} else {
						p.X, p.Y = hitX, hitY
					}
					vn := p.VX*lnx + p.VY*lny
					if vn < 0 {
						restitution := e.Collision.Bounce
						if restitution < 0 {
							restitution = 0
						}
						friction := clamp01(e.Collision.Friction)
						tx := p.VX - vn*lnx
						ty := p.VY - vn*lny
						damp := 1 - friction
						tx *= damp
						ty *= damp
						vnNew := -vn * restitution
						p.VX = tx + vnNew*lnx
						p.VY = ty + vnNew*lny
					}
				}
			}
		}

		e.evalCurves(&p)
		e.particles[write] = p
		if hasTrails {
			if tr == nil && e.Trail != nil {
				tr = e.acquireTrail(*e.Trail)
				tr.AddPoint(prevPX, prevPY)
			}
			if tr != nil {
				tr.Update(dtSec)
				tr.AddPoint(p.X, p.Y)
			}
			if write < len(e.particleTrails) {
				e.particleTrails[write] = tr
			} else {
				e.particleTrails = append(e.particleTrails, tr)
			}
		}
		write++
	}
	clear(e.particles[write:])
	e.particles = e.particles[:write]
	if hasTrails {
		if write < len(e.particleTrails) {
			clear(e.particleTrails[write:])
			e.particleTrails = e.particleTrails[:write]
		}
	}

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
				interpolate := !isLocal && e.hasPrevPos && n > 1 && (e.prevX != e.X || e.prevY != e.Y)
				if interpolate {
					dx := e.X - e.prevX
					dy := e.Y - e.prevY
					invN := float32(1) / float32(n)
					for k := range n {
						frac := float32(k+1) * invN
						e.spawnAt(e.prevX+dx*frac, e.prevY+dy*frac, 0, 0)
					}
				} else {
					for range n {
						e.spawnAt(e.X, e.Y, 0, 0)
					}
				}
			}
		}
	}

	if math.MaxInt64-e.elapsed < dt {
		e.elapsed = math.MaxInt64
	} else {
		e.elapsed += dt
	}
	if len(e.Bursts) > 0 {
		e.fireScheduledBursts()
	}

	e.prevX, e.prevY = e.X, e.Y
	e.hasPrevPos = true
}

func (e *Emitter) fireScheduledBursts() {
	if len(e.burstFired) < len(e.Bursts) {
		next := make([]int64, len(e.Bursts))
		copy(next, e.burstFired)
		e.burstFired = next
	} else if len(e.burstFired) > len(e.Bursts) {
		clear(e.burstFired[len(e.Bursts):])
		e.burstFired = e.burstFired[:len(e.Bursts)]
	}
	maxP := e.maxParticles()
	for i, b := range e.Bursts {
		if b.Count <= 0 || b.Time < 0 || e.elapsed < b.Time {
			continue
		}
		cycles := int64(b.Cycles)
		if cycles == 0 {
			cycles = 1
		}
		fired := e.burstFired[i]
		if cycles > 0 && fired >= cycles {
			continue
		}
		var totalDue int64
		if b.Interval <= 0 {
			totalDue = 1
		} else {
			span := e.elapsed - b.Time
			steps := int64(span / b.Interval)
			if steps == math.MaxInt64 {
				totalDue = math.MaxInt64
			} else {
				totalDue = steps + 1
			}
			if cycles > 0 && totalDue > cycles {
				totalDue = cycles
			}
		}
		if totalDue <= fired {
			continue
		}
		due := totalDue - fired
		e.burstFired[i] = totalDue
		room := maxP - len(e.particles)
		if room <= 0 {
			continue
		}
		var toSpawn int
		if due >= int64(room) || int64(b.Count) >= int64(room) || due*int64(b.Count) >= int64(room) {
			toSpawn = room
		} else {
			toSpawn = int(due) * b.Count
		}
		e.Burst(toSpawn)
	}
}

// Burst immediately emits up to count particles without waiting for Update or requiring Rate > 0, stopping at MaxParticles.
func (e *Emitter) Burst(count int) {
	e.burstWithInheritedVel(count, 0, 0)
}

func (e *Emitter) burstWithInheritedVel(count int, ivx, ivy float32) {
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
		e.spawnAt(e.X, e.Y, ivx, ivy)
	}
}

// Draw draws particle trails, all live particles through a reusable sprite.Batch (oldest first), and any managed child sub-emitters into d.
func (e *Emitter) Draw(d *core.Drawing) {
	if d == nil {
		return
	}

	if len(e.fadingTrails) > 0 || len(e.particleTrails) > 0 {
		localTrail := e.Space == SpaceLocal
		if localTrail {
			d.Save()
			d.Translate(e.X, e.Y)
			if e.Rotation != 0 {
				d.Rotate(e.Rotation)
			}
		}
		for _, tr := range e.fadingTrails {
			if tr != nil {
				tr.Draw(d)
			}
		}
		for _, tr := range e.particleTrails {
			if tr != nil {
				tr.Draw(d)
			}
		}
		if localTrail {
			d.Restore()
		}
	}

	if e.Atlas != nil && len(e.particles) > 0 {
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

		isLocal := e.Space == SpaceLocal
		var sinR, cosR float32
		hasLocalRot := isLocal && e.Rotation != 0
		if hasLocalRot {
			sn, cs := math.Sincos(float64(e.Rotation))
			sinR, cosR = float32(sn), float32(cs)
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
			sx, sy, srot := p.X, p.Y, p.Rotation
			if isLocal {
				if hasLocalRot {
					sx = e.X + cosR*p.X - sinR*p.Y
					sy = e.Y + sinR*p.X + cosR*p.Y
					srot = p.Rotation + e.Rotation
				} else {
					sx = e.X + p.X
					sy = e.Y + p.Y
				}
			}
			e.sprites[i] = sprite.Sprite{
				Frame:    p.Frame,
				X:        sx,
				Y:        sy,
				OriginX:  ox,
				OriginY:  oy,
				Rotation: srot,
				ScaleX:   p.Size,
				ScaleY:   p.Size,
				Alpha:    p.Alpha,
				// The color's alpha is already in p.Alpha, so the tint takes only its RGB.
				Tint: p.Color | 0xff,
				Z:    e.Z,
			}
			e.batch.Sprites[i] = &e.sprites[i]
		}
		e.batch.Atlas = e.Atlas
		e.batch.Draw(d)
	}

	for _, ch := range e.children {
		if ch != nil {
			ch.Draw(d)
		}
	}
}

// Count returns the number of currently live particles in this emitter (excluding child sub-emitters).
func (e *Emitter) Count() int { return len(e.particles) }

// Particles returns the live particles in spawn order.
// The returned slice is backed by the emitter's storage and reused on subsequent updates; copy it to retain a snapshot.
func (e *Emitter) Particles() []Particle { return e.particles }

// Clear removes all live particles, trails, and child sub-emitter particles, and resets the fractional emission accumulator without resetting the PRNG.
func (e *Emitter) Clear() {
	clear(e.particles)
	e.particles = e.particles[:0]
	clear(e.batch.Sprites)
	e.batch.Sprites = e.batch.Sprites[:0]
	e.emitAcc = 0
	e.elapsed = 0
	clear(e.burstFired)
	e.burstFired = e.burstFired[:0]
	e.hasPrevPos = false

	for _, tr := range e.particleTrails {
		e.releaseTrail(tr)
	}
	clear(e.particleTrails)
	e.particleTrails = e.particleTrails[:0]
	for _, tr := range e.fadingTrails {
		e.releaseTrail(tr)
	}
	clear(e.fadingTrails)
	e.fadingTrails = e.fadingTrails[:0]

	for _, ch := range e.children {
		if ch != nil {
			ch.Clear()
		}
	}
}

// Reset clears all live particles, trails, and child sub-emitters and re-seeds the PRNG from Seed so subsequent updates replay identically.
func (e *Emitter) Reset() {
	e.Clear()
	e.rng = nil
	e.seeded = false
	for _, ch := range e.children {
		if ch != nil {
			ch.Reset()
		}
	}
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

func (e *Emitter) spawnAt(originX, originY, ivx, ivy float32) {
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

	var px, py float32
	if e.Space == SpaceLocal {
		px, py = dx, dy
	} else {
		if e.Rotation != 0 {
			rsin, rcos := math.Sincos(float64(e.Rotation))
			dx, dy = float32(float64(dx)*rcos-float64(dy)*rsin), float32(float64(dx)*rsin+float64(dy)*rcos)
			angle += e.Rotation
		}
		px, py = originX+dx, originY+dy
	}

	sin, cos := math.Sincos(float64(angle))
	vx := speed * float32(cos)
	vy := speed * float32(sin)
	if ivx != 0 || ivy != 0 {
		vx += ivx
		vy += ivy
	}

	p := Particle{
		Frame:           frame,
		X:               px,
		Y:               py,
		VX:              vx,
		VY:              vy,
		Lifetime:        e.nextLifetime(),
		initialRotation: e.randFloat(e.InitialRotationMin, e.InitialRotationMax),
		rotationSpeed:   e.randFloat(e.RotationSpeedMin, e.RotationSpeedMax),
	}
	e.evalCurves(&p)
	e.particles = append(e.particles, p)

	if e.Trail != nil {
		tr := e.acquireTrail(*e.Trail)
		tr.AddPoint(p.X, p.Y)
		for len(e.particleTrails) < len(e.particles)-1 {
			e.particleTrails = append(e.particleTrails, nil)
		}
		e.particleTrails = append(e.particleTrails, tr)
	} else if len(e.particleTrails) > 0 {
		e.particleTrails = append(e.particleTrails, nil)
	}

	if len(e.SubEmitters) > 0 && e.subDepth < maxSubEmitterDepth {
		wx, wy := e.localToWorld(p.X, p.Y)
		wvx, wvy := e.localVelToWorld(p.VX, p.VY)
		e.triggerSubEmitters(TriggerOnBirth, wx, wy, wvx, wvy)
	}
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
