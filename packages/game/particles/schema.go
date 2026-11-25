package particles

import "time"

// EmitterDoc describes the versioned YAML schema of a 2D particle emitter definition (version: 1) for JSON Schema generation.
type EmitterDoc struct {
	// Version is the schema version; it must be FormatVersion (1).
	Version int `yaml:"version" schema:"required"`
	// EmitterFieldsDoc holds the emitter fields written alongside version at the top level.
	EmitterFieldsDoc `yaml:",inline"`
}

// EmitterFieldsDoc describes the fields of a 2D particle emitter configuration (used at the top level and inside sub-emitters without version).
type EmitterFieldsDoc struct {
	// X is the emitter's default horizontal position in points.
	X float32 `yaml:"x,omitempty"`
	// Y is the emitter's default vertical position in points.
	Y float32 `yaml:"y,omitempty"`
	// Rotation is the emitter's rotation in radians, clockwise on screen.
	Rotation float32 `yaml:"rotation,omitempty"`
	// Space controls whether particles simulate in world coordinates ("world") or emitter-local coordinates ("local").
	Space string `yaml:"space,omitempty" schema:"enum=world|local"`
	// Shape controls where new particles spawn relative to the emitter's origin.
	Shape *ShapeDoc `yaml:"shape,omitempty"`
	// Rate is how many particles emit per second during Update; 0 disables continuous emission.
	Rate float32 `yaml:"rate,omitempty"`
	// Bursts schedules automatic bursts of particles at specific elapsed times during Update.
	Bursts []BurstDoc `yaml:"bursts,omitempty"`
	// MaxParticles caps how many particles may be live at once (defaults to 256 when 0 or omitted).
	MaxParticles int `yaml:"maxParticles,omitempty"`
	// LifetimeMin is the minimum particle lifetime as a Go duration string such as "300ms".
	LifetimeMin time.Duration `yaml:"lifetimeMin,omitempty"`
	// LifetimeMax is the maximum particle lifetime as a Go duration string such as "700ms".
	LifetimeMax time.Duration `yaml:"lifetimeMax,omitempty"`
	// SpeedMin is the minimum initial particle speed in points per second.
	SpeedMin float32 `yaml:"speedMin,omitempty"`
	// SpeedMax is the maximum initial particle speed in points per second.
	SpeedMax float32 `yaml:"speedMax,omitempty"`
	// AngleMin is the minimum initial direction of travel in radians, clockwise from +X.
	AngleMin float32 `yaml:"angleMin,omitempty"`
	// AngleMax is the maximum initial direction of travel in radians, clockwise from +X.
	AngleMax float32 `yaml:"angleMax,omitempty"`
	// GravityX is constant horizontal acceleration in points per second squared.
	GravityX float32 `yaml:"gravityX,omitempty"`
	// GravityY is constant vertical acceleration in points per second squared.
	GravityY float32 `yaml:"gravityY,omitempty"`
	// InitialRotationMin is the minimum starting particle rotation in radians.
	InitialRotationMin float32 `yaml:"initialRotationMin,omitempty"`
	// InitialRotationMax is the maximum starting particle rotation in radians.
	InitialRotationMax float32 `yaml:"initialRotationMax,omitempty"`
	// RotationSpeedMin is the minimum angular velocity in radians per second.
	RotationSpeedMin float32 `yaml:"rotationSpeedMin,omitempty"`
	// RotationSpeedMax is the maximum angular velocity in radians per second.
	RotationSpeedMax float32 `yaml:"rotationSpeedMax,omitempty"`
	// SizeCurve scales each particle over its normalized lifetime in [0, 1].
	SizeCurve []FloatStopDoc `yaml:"sizeCurve,omitempty"`
	// AlphaCurve fades each particle's opacity over its normalized lifetime in [0, 1].
	AlphaCurve []FloatStopDoc `yaml:"alphaCurve,omitempty"`
	// RotationCurve adds rotation in radians over a particle's normalized lifetime in [0, 1].
	RotationCurve []FloatStopDoc `yaml:"rotationCurve,omitempty"`
	// ColorCurve tints each particle over its normalized lifetime in [0, 1].
	ColorCurve []ColorStopDoc `yaml:"colorCurve,omitempty"`
	// Seed seeds the emitter's deterministic PCG PRNG.
	Seed uint64 `yaml:"seed,omitempty"`
	// Z sets the sprite Z draw order of emitted particles.
	Z int `yaml:"z,omitempty"`
	// FrameNames lists the atlas frame names particles pick from at spawn time.
	FrameNames []string `yaml:"frameNames,omitempty"`
	// Collision configures optional particle collision response.
	Collision *CollisionDoc `yaml:"collision,omitempty"`
	// Trail configures optional per-particle 2D ribbon trails.
	Trail *TrailDoc `yaml:"trail,omitempty"`
	// SubEmitters configures child bursts triggered on particle birth, death or collision.
	SubEmitters []SubEmitterDoc `yaml:"subEmitters,omitempty"`
}

// ShapeDoc describes a 2D emission shape in YAML.
type ShapeDoc struct {
	// Kind selects the shape: "point", "circle", "ring" or "rect".
	Kind string `yaml:"kind,omitempty" schema:"enum=point|circle|ring|rect"`
	// Radius is the radius in points for "circle" and "ring" shapes.
	Radius float32 `yaml:"radius,omitempty"`
	// Width is the width in points for a "rect" shape.
	Width float32 `yaml:"width,omitempty"`
	// Height is the height in points for a "rect" shape.
	Height float32 `yaml:"height,omitempty"`
}

// BurstDoc describes one scheduled particle burst in YAML.
type BurstDoc struct {
	// Time is the elapsed emitter time when the first burst fires, as a Go duration string such as "250ms".
	Time time.Duration `yaml:"time,omitempty"`
	// Count is the number of particles emitted on each firing.
	Count int `yaml:"count,omitempty" schema:"minimum=0"`
	// Cycles is how many times the burst fires (0 or 1 means once; negative repeats indefinitely every interval).
	Cycles int `yaml:"cycles,omitempty"`
	// Interval is the delay between repeated firings as a Go duration string such as "1s".
	Interval time.Duration `yaml:"interval,omitempty"`
}

// FloatStopDoc is one control point on a float curve at normalized age t in [0, 1].
type FloatStopDoc struct {
	// T is the normalized particle age in [0, 1].
	T float32 `yaml:"t"`
	// Value is the curve value at T.
	Value float32 `yaml:"value"`
}

// ColorStopDoc is one control point on a color curve at normalized age t in [0, 1].
type ColorStopDoc struct {
	// T is the normalized particle age in [0, 1].
	T float32 `yaml:"t"`
	// Color is a quoted hex color string "#rrggbb" or "#rrggbbaa".
	Color string `yaml:"color"`
}

// CollisionDoc describes particle collision settings in YAML.
type CollisionDoc struct {
	// Enabled turns particle collision checking on when a runtime Collider is attached.
	Enabled bool `yaml:"enabled,omitempty"`
	// Mode selects the impact response: "none", "stop", "bounce" or "kill".
	Mode string `yaml:"mode,omitempty" schema:"enum=none|stop|bounce|kill"`
	// Bounce is the restitution factor applied to normal velocity in "bounce" mode.
	Bounce float32 `yaml:"bounce,omitempty"`
	// Friction damps tangential velocity on impact in "bounce" mode, clamped to [0, 1].
	Friction float32 `yaml:"friction,omitempty"`
	// Radius is the particle collision radius in points.
	Radius float32 `yaml:"radius,omitempty"`
}

// TrailDoc describes per-particle ribbon trail settings in YAML.
type TrailDoc struct {
	// MaxPoints caps the recorded polyline vertices per trail (defaults to 64 when 0).
	MaxPoints int `yaml:"maxPoints,omitempty"`
	// Lifetime is how long each trail vertex lives in seconds (defaults to 0.5 when 0).
	Lifetime float32 `yaml:"lifetime,omitempty"`
	// MinDistance is the minimum spacing in points between consecutive trail vertices.
	MinDistance float32 `yaml:"minDistance,omitempty"`
	// Width is the base ribbon width in points (defaults to 4 when 0).
	Width float32 `yaml:"width,omitempty"`
	// WidthCurve scales Width from head (t=0) to tail (t=1).
	WidthCurve []FloatStopDoc `yaml:"widthCurve,omitempty"`
	// ColorGradient evaluates color from head (t=0) to tail (t=1).
	ColorGradient []ColorStopDoc `yaml:"colorGradient,omitempty"`
	// Color is the fallback solid hex color "#rrggbb" or "#rrggbbaa" when colorGradient is empty.
	Color string `yaml:"color,omitempty"`
	// TextureURI is an optional image URI mapped across trail segments.
	TextureURI string `yaml:"textureURI,omitempty"`
}

// SubEmitterDoc describes one child burst rule in YAML.
type SubEmitterDoc struct {
	// Trigger selects which parent particle event fires the burst: "birth", "death" or "collision".
	Trigger string `yaml:"trigger,omitempty" schema:"enum=birth|death|collision"`
	// BurstCount is how many child particles to emit on each trigger event.
	BurstCount int `yaml:"burstCount,omitempty"`
	// InheritVelocity multiplies parent velocity added to each spawned child particle.
	InheritVelocity float32 `yaml:"inheritVelocity,omitempty"`
	// Config configures the child emitter (without a version key).
	Config *EmitterFieldsDoc `yaml:"config,omitempty"`
}
