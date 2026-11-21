# particles

`game/particles` provides a deterministic, seeded 2D particle `Emitter` stepped on `game.Loop` and drawn through a reusable `sprite.Batch`.

```go
import "github.com/nsgo-dev/game/packages/game/particles"
```

Two emitters configured with the same fields and `Seed` and stepped with identical `dt` produce bit-for-bit identical particles and draw operations.

## Configuring an Emitter

Configure an `Emitter` with an `Atlas`, one or more `Frames`, spawn shape, motion parameters, and lifetime curves:

```go
fire := &particles.Emitter{
	Atlas:        fxAtlas,
	Frames:       []sprite.Frame{fxAtlas.Frames["spark_0"], fxAtlas.Frames["spark_1"]},
	X:            160,
	Y:            120,
	Shape:        particles.Circle(6),
	Rate:         45, // particles per second
	MaxParticles: 128,
	LifetimeMin:  300 * time.Millisecond,
	LifetimeMax:  700 * time.Millisecond,
	SpeedMin:     20,
	SpeedMax:     65,
	AngleMin:     -math.Pi*0.75,
	AngleMax:     -math.Pi*0.25,
	GravityY:     -30, // rise upward
	SizeCurve: particles.FloatCurve{
		{T: 0, Value: 0.6},
		{T: 0.3, Value: 1.2},
		{T: 1, Value: 0},
	},
	AlphaCurve: particles.FloatCurve{
		{T: 0, Value: 1},
		{T: 1, Value: 0},
	},
	Seed: 42,
	Z:    20,
}
```

Step the emitter in `game.Loop.Update` and draw it in `core.Canvas.Draw`:

```go
// In Loop.Update:
fire.Update(dt)

// In Canvas.Draw:
fire.Draw(d)
```

- `MaxParticles` defaults to `256` when `<= 0`.
- `LifetimeMin` and `LifetimeMax` default to `1s` when both are `0`.
- `AngleMin` and `AngleMax` are in radians clockwise from `+X`.
- `InitialRotationMin`/`Max` and `RotationSpeedMin`/`Max` control starting angle and angular velocity in radians / radians per second.

## Emission shapes

`Emitter.Shape` determines where new particles spawn relative to `(X, Y)`:

| Shape | Spawn distribution |
| --- | --- |
| `Point()` | Exactly at `(X, Y)` (the zero value of `Shape`). |
| `Circle(radius)` | Uniformly over a disk of `radius` centered on `(X, Y)`. |
| `Ring(radius)` | Uniformly on the circumference of a circle of `radius`. |
| `Rect(width, height)` | Uniformly inside a `width × height` rectangle centered on `(X, Y)`. |

## Lifetime curves

Curves evaluate a particle's normalized age `t = Age / Lifetime` in `[0, 1]` by linear interpolation between stops ordered by `T`:

- **`SizeCurve` (`FloatCurve`)**: uniform sprite scale over life (empty = `1`).
- **`AlphaCurve` (`FloatCurve`)**: opacity over life (empty = `1`).
- **`RotationCurve` (`FloatCurve`)**: additive rotation in radians over life (empty = `0`).
- **`ColorCurve` (`ColorCurve`)**: linear RGBA interpolation stored on `Particle.Color` (empty = opaque white). Note: sprite tinting waits for [nsgo-dev/game#29](https://github.com/nsgo-dev/game/issues/29), so `Draw` currently multiplies `ColorCurve`'s alpha channel with `AlphaCurve`.

```go
glow := particles.ColorCurve{
	{T: 0, Color: core.RGBA(0xff, 0xd1, 0x66, 0xff)},
	{T: 1, Color: core.RGBA(0xef, 0x47, 0x6f, 0x00)},
}
```

## Bursts, inspection, and reset

For one-shot impacts (explosions, coin pickups, dust puffs), leave `Rate: 0` and call `Burst(count)`:

```go
sparks.X, sparks.Y = hit.X, hit.Y
sparks.Burst(24) // emits immediately, stopping at MaxParticles
```

Other methods on `*Emitter`:

- `Count()`: number of currently live particles.
- `Particles()`: live `[]Particle` slice in spawn order (backed by internal storage; copy if retaining a snapshot).
- `Clear()`: removes live particles and resets the fractional emission accumulator without resetting the PRNG.
- `Reset()`: clears live particles and re-seeds the PRNG from `Seed` for exact replay.
