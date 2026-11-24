# particles

`game/particles` provides deterministic, seeded particle emitters for both 2D (`Emitter`, drawn through `sprite.Batch`) and 3D (`Emitter3D` + `GPUParticles3D`, drawn in a single instanced `gpu.View` draw call).

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
- **`ColorCurve` (`ColorCurve`)**: linear RGBA interpolation stored on `Particle.Color` (empty = opaque white). `Draw` tints each particle's sprite by the color's RGB, and multiplies its alpha channel with `AlphaCurve`.

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

## Emitter definitions in YAML

An emitter's configuration can live in a YAML file that people and tools edit and review as text. The keys are the `Config` field names in lower camel case, `version: 1` comes first, and fields left at their zero value are omitted. A few are always written even at their default: `space` and `shape`, a sub-emitter's `trigger` and `config` (whose `space` and `shape` are written too), and `collision.mode` when a `collision` block is present:

```yaml
version: 1
x: 160
y: 120
space: world
shape:
  kind: circle
  radius: 6
rate: 45
maxParticles: 128
lifetimeMin: 300ms
lifetimeMax: 700ms
gravityY: -30
sizeCurve:
  - t: 0
    value: 0.6
  - t: 1
    value: 0
colorCurve:
  - t: 0
    color: "#ffd166ff"
  - t: 1
    color: "#ef476f00"
seed: 42
frameNames:
  - spark_0
  - spark_1
subEmitters:
  - trigger: death
    burstCount: 6
    config:
      space: world
      shape:
        kind: ring
        radius: 8
      lifetimeMin: 100ms
      lifetimeMax: 200ms
```

```go
cfg, err := particles.LoadConfigFS(assets, "fx/fire.yaml")
if err != nil {
	return err
}
fire := &particles.Emitter{Atlas: fxAtlas}
fire.ReloadConfig(cfg) // frameNames resolve against fire.Atlas
```

- `LoadConfigFile`, `LoadConfigFS` and `UnmarshalConfigYAML` read a definition; `SaveConfigFile` and `MarshalConfigYAML` write one.
- Saving is deterministic: keys keep a fixed order, indentation is two spaces, and a definition loaded and saved again comes back byte for byte when it is already in that form.
- Loading refuses a `version` other than `FormatVersion` or one inside a sub-emitter's `config`, unknown keys, duplicate keys, a key with no value, a second YAML document, a fraction in an integer field such as `maxParticles: 12.7`, and a shape size that does not apply to its kind, such as `width` on a `circle`.
- Durations are strings in Go's `time.Duration` form, such as `500ms` or `1m30s`; a bare number is refused because its unit would be ambiguous.
- Colors are quoted `"#rrggbb"` (opaque) or `"#rrggbbaa"` strings. Quote them: an unquoted `#` starts a YAML comment, and loading reports the key as having no value.
- `shape.kind` is `point`, `circle`, `ring` or `rect`; `space` is `world` or `local`; `collision.mode` is `none`, `stop`, `bounce` or `kill`; a sub-emitter's `trigger` is `birth`, `death` or `collision`.
- A runtime `Collider` is not part of the file, and a trail texture is saved by its URI (`trail.textureURI`).
- `MarshalConfigJSON` and `UnmarshalConfigJSON` encode the same schema as JSON for programs that exchange definitions as JSON; files people write are YAML.

### Hot reload

`HotConfig` keeps a definition and pushes changes to bound emitters without resetting live particles, the PRNG, attachments or runtime colliders. `ReloadBytes` takes the bytes a watcher such as `tooling/hotreload` delivers, and `ReloadHook` suits watchers that pass a path; a definition that fails to load returns an error and leaves the current one in place.

```go
hot, err := particles.LoadHotConfigFile("fx/fire.yaml")
if err != nil {
	return err
}
hot.Bind(fire)
watcher.Watch("fx/fire.yaml", func(data []byte) {
	if err := hot.ReloadBytes(data); err != nil {
		log.Printf("fx/fire.yaml: %v", err)
	}
})
```

From a background goroutine, call `StageBytes` and apply the staged definition on the game loop with `Flush`.

## Performance

Each live particle is one sprite in the emitter's `sprite.Batch`. Untinted particles, those without a `ColorCurve` or with a white one, draw in runs of one `DrawAtlas` call each ([nsgo-dev/game#28](https://github.com/nsgo-dev/game/issues/28)). A particle tinted by a non-white `ColorCurve` keeps its own image draw, which also allocates once inside NSGo's `Drawing`, so tinting thousands of particles costs more than fading them with `AlphaCurve`.

`examples/fountain` is the stress scene for this: four seeded fountains topped up to 5000 live particles after every fixed step, all rotating and fading. Run it with NSGo's performance monitor and read the FPS line at the top right, with the window visible (a covered window pauses `game.Loop`):

```sh
cd examples/fountain
go tool nsgo dev -fps
```

The up and down arrow keys move the particle count by 1000 (from 1000 to 20000) to find where a machine drops below its display rate, and `R` restarts the scene from its seed.

Two benchmarks track the CPU side of a frame, one for each way a particle is drawn:

```sh
# One Update and one Draw of 5000 tinted particles, one image draw each
cd packages/game && go tool nsgo test -run '^$' -bench Emitter5000UpdateDraw ./particles
# One step and one draw of the fountain scene's 5000 untinted particles, in DrawAtlas runs
cd examples/fountain && go tool nsgo test -run '^$' -bench Scene5000 .
```

On an AMD Ryzen 9 3900X (Linux) they take about 1.7 ms with 5000 allocations and 1.8 ms with 25 allocations. That is only the CPU cost of stepping the particles and recording their draw operations: it leaves out the platform drawing about 5000 images per frame, and for tinted particles the garbage collector reclaiming the allocation each draw makes, so it is not evidence that a device holds 60 fps. A 60 fps reading on macOS with `--nsgo-fps` has not been recorded yet ([nsgo-dev/game#85](https://github.com/nsgo-dev/game/issues/85)).

## 3D GPU-friendly particles

`Emitter3D` shares the 2D emitter model (`Rate`, `MaxParticles`, `LifetimeMin`/`Max`, `SpeedMin`/`Max`, `SizeCurve`, `AlphaCurve`, `RotationCurve`, `ColorCurve`, `SimulationSpace`, `Seed`, `Burst`, `Clear`, `Reset`) and adds 3D shapes, 3D gravity, linear drag, spread, billboard orientation modes, back-to-front depth sorting, and packed instanced GPU upload for `gpu.View`.

```go
sparks := particles.NewEmitter3D(particles.Emitter3DConfig{
	Position:     math3d.Vec3{Y: 1},
	Direction:    math3d.Vec3Up(),
	Shape:        particles.Cone(0.3, 0.4),
	Billboard:    particles.BillboardStretched,
	Blend:        particles.BlendAdditive,
	Rate:         4000,
	MaxParticles: 10000,
	LifetimeMin:  600 * time.Millisecond,
	LifetimeMax:  1400 * time.Millisecond,
	SpeedMin:     3,
	SpeedMax:     9,
	Spread:       0.2,
	Drag:         0.5,
	Gravity:      math3d.Vec3{Y: -9.8},
	SizeMin:      0.05,
	SizeMax:      0.15,
	ColorCurve: particles.ColorCurve{
		{T: 0, Color: core.RGBA(0xffd166, 1)},
		{T: 1, Color: core.RGBA(0xef476f, 0)},
	},
	Seed: 280,
})
```

### 3D emission shapes

`Emitter3D.Shape` (`Shape3D`) places spawned particles relative to `Position` in the emitter's `(side, Direction, forward)` frame:

| Shape | Spawn distribution |
| --- | --- |
| `Point3D()` | Exactly at `Position` (the zero value of `Shape3D`). |
| `Sphere(radius)` | Uniformly throughout a sphere of `radius`. |
| `Hemisphere(radius)` | Uniformly throughout the upper hemisphere along `Direction`. |
| `Box(width, height, depth)` | Uniformly inside a `width × height × depth` box centered on `Position`. |
| `Cone(radius, angle)` | Uniformly on a base disk of `radius` perpendicular to `Direction`, firing outward up to half-angle `angle` (radians). |
| `Ring3D(radius)` | Uniformly on a circle of `radius` perpendicular to `Direction`. |

### Billboard modes, blending, and depth sorting

- **`BillboardSpherical`** (default): camera-facing quads parallel to the view plane, supporting per-particle roll (`InitialRotationMin`/`Max`, `RotationSpeedMin`/`Max`, `RotationCurve`).
- **`BillboardCylindrical`**: quads locked to `Emitter3D.Axis` (defaults to `+Y`), rotating only around that axis toward the camera.
- **`BillboardStretched`**: quads aligned and stretched along each particle's velocity vector (`length = size + speed * StretchFactor`, defaulting `StretchFactor` to `0.05`s).
- **`BlendAdditive`** (`gpu.BlendAdditive()`): order-independent blending for fire, sparks, and magic; no depth sorting required.
- **`BlendAlpha`** (`gpu.BlendAlpha()`, default) and **`BlendPremultiplied`** (`gpu.BlendPremultipliedAlpha()`): call `e.SortByDepth(camPos, camForward)` before packing/uploading to order particles back-to-front with a deterministic, zero-allocation 4-pass radix sort.

### Instanced GPU rendering (`GPUParticles3D`)

`GPUParticles3D` owns a 4-vertex unit quad buffer (`StepVertex`) and a growable 40-byte-per-instance buffer (`StepInstance`: `center: vec3<f32>`, `right: vec3<f32>`, `up: vec3<f32>`, `color: unorm8x4`), drawing up to tens of thousands of particles in a single `DrawIndexedInstanced` call. It requires no compute shaders or storage buffers, so it runs identically on WebGL 2 / OpenGL ES 3.0 as well as Metal, Vulkan, D3D11, and WebGPU.

`ParticleWGSL`, `ParticleSoftCircleWGSL`, and `ParticleTexturedWGSL` all bind the standard 3D camera uniform (`view`, `proj`, `view_proj`, `eye`) at `@group(0) @binding(0)`, matching `render3d.CameraBuffer`:

```go
// In gpu.View OnReady:
pipe, err := particles.NewParticlePipeline(cx, dev, particles.ParticlePipelineOptions{
	CameraLayout: camBuf.Layout(),
	DepthFormat:  gpu.FormatDepth24Plus,
	Blend:        sparks.Blend,
	SoftCircle:   true,
})
gpuParticles, err := particles.NewGPUParticles3D(cx, dev, particles.GPUParticles3DOptions{
	Capacity: sparks.MaxParticles,
})

// In fixed Update:
sparks.Step(dt)

// In gpu.View OnFrame:
if sparks.Blend != particles.BlendAdditive {
	sparks.SortByDepth(cam.Pos, cam.Forward())
}
_ = gpuParticles.Upload(sparks, cam.Right(), cam.Up())
pass.SetPipeline(pipe)
pass.SetBindGroup(0, camBuf.BindGroup())
gpuParticles.Draw(pass) // 1 DrawIndexedInstanced call for all live particles
```

### 3D emitter definitions in YAML

`Emitter3DConfig` uses the same versioned YAML (and JSON) conventions as 2D `Config`: `LoadConfig3DFile`, `LoadConfig3DFS`, `UnmarshalConfig3DYAML`, `SaveConfig3DFile`, `MarshalConfig3DYAML`, `UnmarshalConfig3DJSON`, and `MarshalConfig3DJSON`. Saving is deterministic with fixed key order, and loading refuses unknown fields, missing/wrong `version`, null values, non-finite floats, and shape parameters that do not apply to `shape.kind`.

### 3D 10k particle benchmarks

`Emitter3D` stores particles in dense structure-of-arrays slices with swap-remove expiration and reusable radix-sort scratch buffers:

```sh
cd packages/game && go tool nsgo test -run '^$' -bench BenchmarkEmitter3D10k ./particles
```

On Apple Silicon (10,000 active particles with gravity, exponential drag, spin, and size/alpha/color/rotation curves):

- `BenchmarkEmitter3D10kStepAndPack`: ~0.34 ms/frame, `0 B/op`, `0 allocs/op` (additive / unsorted path).
- `BenchmarkEmitter3D10kStepSortAndPack`: ~0.44 ms/frame, `0 B/op`, `0 allocs/op` (alpha-blended path with back-to-front radix sort).

Both leave over 97% of a 16.6 ms (60 fps) desktop frame budget available, and all 10,000 instances upload as a single 400 KB buffer write and render in one `DrawIndexedInstanced` call.
