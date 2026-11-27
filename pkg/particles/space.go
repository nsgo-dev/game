package particles

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

const maxSubEmitterDepth = 4

// SimulationSpace controls whether particles simulate in world coordinates or emitter-local coordinates.
type SimulationSpace uint8

const (
	// SpaceWorld simulates particles in world coordinates so they keep their positions when the emitter moves (default).
	SpaceWorld SimulationSpace = iota
	// SpaceLocal simulates particles in emitter-local coordinates and transforms them by the emitter's current X, Y, and Rotation at Draw time.
	// On child sub-emitters, each trigger anchors its spawned local particles at the trigger point so subsequent triggers do not move earlier live children.
	SpaceLocal
)

// String returns the canonical string name of the simulation space.
func (s SimulationSpace) String() string {
	switch s {
	case SpaceLocal:
		return "local"
	default:
		return "world"
	}
}

// SubEmitterTrigger specifies which parent particle lifecycle event fires a SubEmitterRule.
type SubEmitterTrigger uint8

const (
	// TriggerOnBirth fires when a parent particle is spawned.
	TriggerOnBirth SubEmitterTrigger = iota
	// TriggerOnDeath fires when a parent particle expires (or is killed by CollideKill).
	TriggerOnDeath
	// TriggerOnCollision fires when a parent particle hits a Collider.
	TriggerOnCollision
)

// String returns the canonical string name of the sub-emitter trigger.
func (t SubEmitterTrigger) String() string {
	switch t {
	case TriggerOnDeath:
		return "death"
	case TriggerOnCollision:
		return "collision"
	default:
		return "birth"
	}
}

// SubEmitterRule spawns a burst of child particles from a parent particle event.
type SubEmitterRule struct {
	// Trigger selects when the sub-emitter fires.
	Trigger SubEmitterTrigger
	// Config configures the child emitter spawned by this rule.
	Config Config
	// BurstCount is how many child particles to emit on each trigger event; defaults to 1 when <= 0.
	BurstCount int
	// InheritVelocity adds InheritVelocity * parentVelocity to each spawned child particle's initial velocity.
	InheritVelocity float32
}

func (r SubEmitterRule) burstCount() int {
	if r.BurstCount > 0 {
		return r.BurstCount
	}
	return 1
}

func (r SubEmitterRule) clone() SubEmitterRule {
	return SubEmitterRule{
		Trigger:         r.Trigger,
		Config:          r.Config.clone(),
		BurstCount:      r.BurstCount,
		InheritVelocity: r.InheritVelocity,
	}
}

// Config is a serializable, reloadable snapshot of an Emitter's configuration.
type Config struct {
	X, Y               float32
	Rotation           float32
	Space              SimulationSpace
	Shape              Shape
	Rate               float32
	Bursts             []Burst
	MaxParticles       int
	LifetimeMin        time.Duration
	LifetimeMax        time.Duration
	SpeedMin           float32
	SpeedMax           float32
	AngleMin           float32
	AngleMax           float32
	GravityX           float32
	GravityY           float32
	InitialRotationMin float32
	InitialRotationMax float32
	RotationSpeedMin   float32
	RotationSpeedMax   float32
	SizeCurve          FloatCurve
	AlphaCurve         FloatCurve
	RotationCurve      FloatCurve
	ColorCurve         ColorCurve
	Seed               uint64
	Z                  int
	FrameNames         []string
	Collision          CollisionConfig
	Trail              *TrailConfig
	SubEmitters        []SubEmitterRule
}

func (c Config) clone() Config {
	out := c
	if len(c.Bursts) > 0 {
		out.Bursts = append([]Burst(nil), c.Bursts...)
	}
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
	if len(c.FrameNames) > 0 {
		out.FrameNames = append([]string(nil), c.FrameNames...)
	}
	if c.Trail != nil {
		tc := c.Trail.clone()
		out.Trail = &tc
	}
	if len(c.SubEmitters) > 0 {
		out.SubEmitters = make([]SubEmitterRule, len(c.SubEmitters))
		for i := range c.SubEmitters {
			out.SubEmitters[i] = c.SubEmitters[i].clone()
		}
	}
	return out
}

// NewEmitter constructs an Emitter initialized from cfg.
func NewEmitter(cfg Config) *Emitter {
	e := &Emitter{}
	e.ReloadConfig(cfg)
	return e
}

// Config returns a deep-copied Config reflecting the emitter's current settings.
func (e *Emitter) Config() Config {
	cfg := Config{
		X:                  e.X,
		Y:                  e.Y,
		Rotation:           e.Rotation,
		Space:              e.Space,
		Shape:              e.Shape,
		Rate:               e.Rate,
		Bursts:             e.Bursts,
		MaxParticles:       e.MaxParticles,
		LifetimeMin:        e.LifetimeMin,
		LifetimeMax:        e.LifetimeMax,
		SpeedMin:           e.SpeedMin,
		SpeedMax:           e.SpeedMax,
		AngleMin:           e.AngleMin,
		AngleMax:           e.AngleMax,
		GravityX:           e.GravityX,
		GravityY:           e.GravityY,
		InitialRotationMin: e.InitialRotationMin,
		InitialRotationMax: e.InitialRotationMax,
		RotationSpeedMin:   e.RotationSpeedMin,
		RotationSpeedMax:   e.RotationSpeedMax,
		SizeCurve:          e.SizeCurve,
		AlphaCurve:         e.AlphaCurve,
		RotationCurve:      e.RotationCurve,
		ColorCurve:         e.ColorCurve,
		Seed:               e.Seed,
		Z:                  e.Z,
		Collision:          e.Collision,
		Trail:              e.Trail,
		SubEmitters:        e.SubEmitters,
	}
	if len(e.Frames) > 0 {
		cfg.FrameNames = make([]string, len(e.Frames))
		for i, f := range e.Frames {
			cfg.FrameNames[i] = f.Name
		}
	}
	return cfg.clone()
}

// Attach binds a position callback so the emitter tracks a moving object on every Update.
// Calling Attach immediately syncs X, Y to posFn(). Passing nil detaches.
func (e *Emitter) Attach(posFn func() (float32, float32)) {
	e.attachTransformFn = nil
	e.attachFn = posFn
	if posFn != nil {
		x, y := posFn()
		e.X, e.Y = x, y
		e.prevX, e.prevY = x, y
		e.hasPrevPos = true
	}
}

// AttachTransform binds a position and rotation callback so the emitter tracks a moving, rotating object on every Update.
// Calling AttachTransform immediately syncs X, Y, Rotation. Passing nil detaches.
func (e *Emitter) AttachTransform(fn func() (x, y, rotation float32)) {
	e.attachFn = nil
	e.attachTransformFn = fn
	if fn != nil {
		x, y, rot := fn()
		e.X, e.Y, e.Rotation = x, y, rot
		e.prevX, e.prevY = x, y
		e.hasPrevPos = true
	}
}

// Detach clears any callback bound via Attach or AttachTransform.
func (e *Emitter) Detach() {
	e.attachFn = nil
	e.attachTransformFn = nil
}

// Follow moves the emitter smoothly to (x, y), recording the previous position so multi-particle continuous emission interpolates along the motion path.
func (e *Emitter) Follow(x, y float32) {
	if !e.hasPrevPos {
		e.prevX, e.prevY = e.X, e.Y
		e.hasPrevPos = true
	}
	e.X, e.Y = x, y
}

// ParticleWorldPos converts a particle's coordinates to world coordinates.
// For SpaceWorld emitters this returns (p.X, p.Y) unchanged; for SpaceLocal emitters it applies Rotation and the particle's sub-emitter trigger anchor (or the emitter's current X, Y for top-level emitters).
func (e *Emitter) ParticleWorldPos(p Particle) (x, y float32) {
	return e.particleLocalToWorld(p, p.X, p.Y)
}

func (e *Emitter) particleOrigin(p Particle) (ox, oy float32) {
	if p.hasAnchor {
		return p.anchorX, p.anchorY
	}
	return e.X, e.Y
}

func (e *Emitter) particleLocalToWorld(p Particle, lx, ly float32) (wx, wy float32) {
	if e.Space != SpaceLocal {
		return lx, ly
	}
	ox, oy := e.particleOrigin(p)
	if e.Rotation == 0 {
		return ox + lx, oy + ly
	}
	sin, cos := math.Sincos(float64(e.Rotation))
	return ox + float32(cos)*lx - float32(sin)*ly, oy + float32(sin)*lx + float32(cos)*ly
}

func (e *Emitter) localVelToWorld(lvx, lvy float32) (wvx, wvy float32) {
	if e.Space != SpaceLocal || e.Rotation == 0 {
		return lvx, lvy
	}
	sin, cos := math.Sincos(float64(e.Rotation))
	return float32(cos)*lvx - float32(sin)*lvy, float32(sin)*lvx + float32(cos)*lvy
}

func (e *Emitter) particleWorldToLocal(p Particle, wx, wy float32) (lx, ly float32) {
	if e.Space != SpaceLocal {
		return wx, wy
	}
	ox, oy := e.particleOrigin(p)
	dx, dy := wx-ox, wy-oy
	if e.Rotation == 0 {
		return dx, dy
	}
	sin, cos := math.Sincos(float64(-e.Rotation))
	return float32(cos)*dx - float32(sin)*dy, float32(sin)*dx + float32(cos)*dy
}

func (e *Emitter) worldDirToLocal(wx, wy float32) (lx, ly float32) {
	if e.Space != SpaceLocal || e.Rotation == 0 {
		return wx, wy
	}
	sin, cos := math.Sincos(float64(-e.Rotation))
	return float32(cos)*wx - float32(sin)*wy, float32(sin)*wx + float32(cos)*wy
}

// Children returns the managed child emitters created for SubEmitters.
func (e *Emitter) Children() []*Emitter {
	return e.children
}

// TotalCount returns the number of live particles in this emitter plus all managed child sub-emitters recursively.
func (e *Emitter) TotalCount() int {
	total := len(e.particles)
	for _, ch := range e.children {
		if ch != nil {
			total += ch.TotalCount()
		}
	}
	return total
}

func (e *Emitter) ensureChildEmitter(idx int, rule SubEmitterRule) *Emitter {
	if len(e.children) < len(e.SubEmitters) {
		next := make([]*Emitter, len(e.SubEmitters))
		copy(next, e.children)
		e.children = next
	}
	ch := e.children[idx]
	if ch == nil {
		cfg := rule.Config.clone()
		if cfg.Seed == 0 {
			cfg.Seed = e.Seed ^ (uint64(idx+1) * pcgStream)
		}
		ch = NewEmitter(cfg)
		ch.subDepth = e.subDepth + 1
		e.bindChildAtlas(ch, rule.Config.FrameNames)
		e.children[idx] = ch
	} else if ch.Atlas == nil && e.Atlas != nil {
		e.bindChildAtlas(ch, rule.Config.FrameNames)
	}
	return ch
}

func (e *Emitter) bindChildAtlas(ch *Emitter, frameNames []string) {
	ch.Atlas = e.Atlas
	if ch.Atlas != nil && len(frameNames) > 0 {
		ch.Frames = resolveAtlasFrames(ch.Atlas, frameNames)
	}
	if len(ch.Frames) == 0 && len(e.Frames) > 0 {
		ch.Frames = append([]sprite.Frame(nil), e.Frames...)
	}
}

func resolveAtlasFrames(atlas *sprite.Atlas, names []string) []sprite.Frame {
	if atlas == nil || len(names) == 0 {
		return nil
	}
	frames := make([]sprite.Frame, 0, len(names))
	for _, name := range names {
		if f, ok := atlas.Frames[name]; ok {
			frames = append(frames, f)
		}
	}
	return frames
}

func (e *Emitter) triggerSubEmitters(trigger SubEmitterTrigger, wx, wy, wvx, wvy float32) {
	if len(e.SubEmitters) == 0 || e.subDepth >= maxSubEmitterDepth {
		return
	}
	for i := range e.SubEmitters {
		rule := e.SubEmitters[i]
		if rule.Trigger != trigger {
			continue
		}
		ch := e.ensureChildEmitter(i, rule)
		ch.X, ch.Y = wx, wy
		ch.burstWithInheritedVel(rule.burstCount(), wvx*rule.InheritVelocity, wvy*rule.InheritVelocity)
	}
}

// System manages multiple Emitters (and their sub-emitters and trails) as a single unit stepped by game.Loop.
type System struct {
	emitters []*Emitter
}

// NewSystem creates a System pre-populated with any provided emitters.
func NewSystem(emitters ...*Emitter) *System {
	s := &System{}
	for _, e := range emitters {
		s.Add(e)
	}
	return s
}

// Add registers an emitter with the system.
func (s *System) Add(e *Emitter) {
	if e == nil {
		return
	}
	s.emitters = append(s.emitters, e)
}

// Remove unregisters an emitter from the system.
func (s *System) Remove(e *Emitter) {
	for i, existing := range s.emitters {
		if existing == e {
			copy(s.emitters[i:], s.emitters[i+1:])
			s.emitters[len(s.emitters)-1] = nil
			s.emitters = s.emitters[:len(s.emitters)-1]
			return
		}
	}
}

// Update advances all registered emitters by dt.
func (s *System) Update(dt time.Duration) {
	for _, e := range s.emitters {
		e.Update(dt)
	}
}

// Draw draws all registered emitters into d in registration order.
func (s *System) Draw(d *core.Drawing) {
	for _, e := range s.emitters {
		e.Draw(d)
	}
}

// TotalCount returns the total number of live particles across all registered emitters and their sub-emitters.
func (s *System) TotalCount() int {
	total := 0
	for _, e := range s.emitters {
		total += e.TotalCount()
	}
	return total
}

// Clear clears all live particles from all registered emitters.
func (s *System) Clear() {
	for _, e := range s.emitters {
		e.Clear()
	}
}

// Reset resets all registered emitters and their PRNGs.
func (s *System) Reset() {
	for _, e := range s.emitters {
		e.Reset()
	}
}

// Emitters returns the slice of registered emitters.
func (s *System) Emitters() []*Emitter {
	return s.emitters
}
