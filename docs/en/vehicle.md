# vehicle

```go
import "github.com/nsgo-dev/game/packages/game/vehicle"
```

Raycast cars on a [`physics3d`](physics3d.md) world: wheel colliders, spring-damper suspension, longitudinal and lateral tire friction and a car controller, plus race checkpoints, lap times and ghost replays.

## Model

A `Vehicle` is one dynamic `physics3d.Body`, the chassis, whose `Pos` is the center of mass, plus a list of wheels. Wheels have no bodies of their own: every fixed step, `Vehicle.Step` casts one ray per wheel from its anchor along the chassis' down axis, so a wheel costs one raycast and cannot snag on geometry.

For each wheel that touches the ground, a step:

1. **Suspension**: turns the ray's compression into a spring-damper force, `Stiffness * compression - Damping * extension speed`, never pulling and capped by `MaxForce`, and pushes the chassis up at the anchor.
2. **Tires**: computes a longitudinal impulse (engine on drive wheels, brakes on brake wheels, rolling resistance) and a lateral impulse that cancels the wheel's share of sideways slip, then limits their sum to a friction circle of `Friction * load`. A wheel that hits the limit reports `Sliding`, for skid marks or tire squeal.
3. **Reaction**: applies the opposite impulses to a dynamic body under the wheel, so a car can push a crate or a platform it drives over.

Every wheel's tire impulse is computed from the same chassis state and applied afterwards, so the result does not depend on wheel order and a symmetric car drives straight. Aerodynamic drag, `Drag * speed²`, then acts on the chassis, and `World.Step` integrates it.

Cars face `-Z` in local space, with `+X` to the right and `+Y` up, matching [3D Models from Blender](3d-models-blender.md).

## Driving a car

```go
world := physics3d.NewWorld()
world.AddBody(physics3d.NewStaticBody(
	physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 200, Y: 1, Z: 200}}),
	physics3d.Vec3{Y: -1},
))
car, err := vehicle.New(world, vehicle.ReferenceConfig(), physics3d.Vec3{Y: 0.65}, physics3d.QuatIdentity())
if err != nil { ... }

controls := vehicle.DefaultControls()
loop := &game.Loop{Update: func(dt time.Duration) {
	actions.Step()
	car.SetInput(controls.Read(actions))
	car.Step(world, dt)
	world.Step(dt)
}}
```

Call `Vehicle.Step` once per fixed step, before `World.Step`. `Input` holds `Throttle` in `[-1, 1]` (negative reverses), `Brake` in `[0, 1]`, `Steer` in `[-1, 1]` (positive steers right) and `Handbrake`; `SetInput` clamps each axis and treats NaN as 0, and the input stays in effect until it is set again. Steering eases toward the requested angle at `SteerRate`, and the engine force fades to zero at `MaxSpeed`.

`Controls` names the axes and action a car reads (by default `"throttle"`, `"brake"`, `"steer"` and `"handbrake"`), so the same code drives a car from live `*input.Actions`, a `replay.Recorder` while recording, or a `replay.Player` while playing back.

To read the car's state:

- `Speed()` is the signed speed along `Forward()`, negative while reversing; `Up()` is the chassis' up axis, such as to detect a roll-over.
- `Grounded()` counts the wheels touching the ground, and each `Wheels[i]` reports `Contact`, `ContactPoint`, `ContactNormal`, `Ground`, `SuspensionLength`, `SuspensionForce`, `SteerAngle`, `SpinSpeed`, `SpinAngle` and `Sliding`.
- `WheelTransform(i)` returns wheel `i`'s world-space center and orientation, including steering and spin, for drawing its mesh, whose axle is its local X axis.
- `Reset(pos, rot)` teleports the car, stops it and clears its wheels, such as to respawn at a checkpoint.

`RayMask` selects the layers wheel rays hit; trigger bodies and the chassis itself are always skipped, so checkpoints do not hold a car up. A settled car with no input sleeps like any other body and wakes when `SetInput` gives it throttle, brake, steering or handbrake.

## Car files

A car's tuning is a `Config`, written as versioned YAML. `ParseConfig` decodes and validates it, refusing unknown fields and trailing documents; `EncodeConfig` writes deterministic 2-space YAML that `ParseConfig` reads back unchanged. Lengths are meters, masses kilograms, forces newtons and angles degrees, and positions are in chassis space relative to the center of mass.

```yaml
version: 1
name: reference
mass: 1200
chassisHalfExtents: [0.9, 0.3, 2]
chassisOffset: [0, 0.2, 0]
engineForce: 6000
reverseForce: 3000
maxSpeed: 40
maxReverseSpeed: 8
brakeForce: 12000
handbrakeGrip: 0.4
maxSteerAngle: 30
steerRate: 120
drag: 0.4
rollingResistance: 0.015
rollInfluence: 0.1
angularDamping: 0.5
wheels:
  - name: front-left
    anchor: [-0.8, -0.1, -1.3]
    radius: 0.35
    restLength: 0.3
    stiffness: 30000
    damping: 4000
    maxForce: 20000
    friction: 1.2
    steer: true
    brake: true
  # front-right, rear-left and rear-right follow; the rear wheels set drive and handbrake instead of steer.
```

| Field | Meaning |
| --- | --- |
| `mass`, `chassisHalfExtents` | Chassis mass and collision box half sizes, all `> 0` |
| `chassisOffset` | Moves the box relative to the center of mass; a positive Y lowers the center of mass inside the box, which steadies the car |
| `engineForce`, `reverseForce` | Total drive force at full throttle and standstill, split over the drive wheels |
| `maxSpeed`, `maxReverseSpeed` | Speeds in m/s at which the drive force fades to zero |
| `brakeForce` | Total braking force at full brake, split over the brake wheels |
| `handbrakeGrip` | Lateral grip of handbrake wheels while the handbrake is held, in `[0, 1]`; lower values slide more |
| `maxSteerAngle`, `steerRate` | Largest steering angle in `[0, 80]` degrees and how fast steered wheels turn, in degrees per second |
| `drag`, `rollingResistance` | Aerodynamic drag coefficient and rolling resistance as a fraction of each wheel's load |
| `rollInfluence` | How much lateral tire force rolls the chassis, in `[0, 1]`: 0 at the center of mass' height, 1 at the contact point |
| `angularDamping` | The chassis' `physics3d.Body.AngularDamping` |
| `wheels` | 1 to 32 wheels with unique names, in a fixed order |
| `anchor`, `radius`, `restLength` | Top of the suspension, wheel radius and unloaded suspension length; the ray is `restLength + radius` long |
| `stiffness`, `damping`, `maxForce` | Spring rate in N/m, damper rate in N·s/m and an optional force cap (0 leaves it uncapped) |
| `friction` | Tire friction coefficient: the combined tire force stays within `friction` times the wheel's load |
| `steer`, `drive`, `brake`, `handbrake` | Which inputs the wheel receives |

`ReferenceConfig()` returns the reference car: a 1200 kg rear-wheel-drive hatchback with about 0.5 g of acceleration, 1 g of braking and a 40 m/s top speed. Its file is `vehicle/testdata/reference_car.yaml`.

## Races and laps

A race is an ordered list of checkpoint triggers. `AddCheckpoint` adds a static trigger body to the world, and `NewRace` tracks racers through them for a number of laps:

```go
gate := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 7, Y: 2, Z: 0.5}})
race, err := vehicle.NewRace(3,
	vehicle.AddCheckpoint(world, gate, start),
	vehicle.AddCheckpoint(world, gate, sector1),
	vehicle.AddCheckpoint(world, gate, sector2),
)
world.OnTrigger = race.Observe
racer := race.AddRacer(car.Body, "player")
race.OnLap = func(ev vehicle.LapEvent) { ... }

// Each fixed step:
race.Tick()
car.Step(world, dt)
world.Step(dt)
```

Checkpoint 0 is the start/finish line. A racer's first lap starts on the step it is added; it must enter checkpoints 1, 2, ... in order and then checkpoint 0 to complete a lap, and entering any other checkpoint is ignored, so cutting the track does not count. Times are counted in fixed steps: call `Tick` once per step, before `World.Step`, and multiply by the step duration for wall time.

Each `Racer` reports `Laps`, `Next`, `LastLap`, `BestLap`, `Finished` and `FinishStep`; `OnLap` receives a `LapEvent` for every completed lap, with whether it was the racer's best and final. `Standings()` orders racers: finished racers by finish step, then the rest by laps and checkpoints passed, ties in the order they were added. `Observe` ignores events that are not a checkpoint entry, so it can sit behind other trigger handlers.

## Ghosts

A `Ghost` is a recorded lap: the chassis pose sampled every `Interval` steps, played back as a translucent rival. It is pose data, so playing it needs no physics.

```go
rec := &vehicle.GhostRecorder{Interval: 2}
race.OnLap = func(ev vehicle.LapEvent) {
	rec.Record(car.Body.Pos, car.Body.Rot)
	if ev.Best {
		best = rec.Ghost(60, ev.Steps)
	}
	rec.Reset()
}
// After each World.Step:
rec.Record(car.Body.Pos, car.Body.Rot)

// Drawing the ghost, with game.Loop's draw alpha:
pos, rot := best.Pose(float64(step) + alpha)
```

`Pose` interpolates positions linearly and orientations spherically between frames, and clamps steps outside the recording to its first or last frame.

Ghosts are text, like every file in this repository: `EncodeGhost` writes versioned YAML with each frame's position and rotation on a flow-style line of its own, and `ParseGhost` reads it back unchanged, refusing unknown fields and more than an hour of frames at 60 Hz.

```yaml
version: 1
rate: 60
interval: 2
lapSteps: 812
frames:
  - p: [0, 0.62, 14]
    r: [0, 0, 0, 1]
  - p: [0, 0.62, 13.98]
    r: [0, 0, 0, 1]
```

To reproduce a drive exactly, such as for a bug report, record the car's input instead with `replay.NewRecorder(actions, seed, rate, controls.Actions(), controls.Axes())` and play it back through `replay.Player` into `Controls.Read`.

## Determinism

`Vehicle.Step` uses no maps, wall clock or randomness and visits wheels in `Config` order, and `physics3d` steps in a fixed order, so the same `Config`, start pose and per-step `Input` on the same world produce bit-identical poses, lap times and ghosts across runs on one platform. The package's acceptance test drives the reference car two laps around a test track (two 40 m straights joined by 15 m hairpins, with a speed bump) and checks that two runs end bit-identical; a second test records the drive with `replay`, round-trips the recording through its file format and replays it into a fresh world to the same end state.

## Limits

- The chassis collides as one box, and `physics3d` treats its inertia as isotropic, so pitch, roll and yaw respond alike to the same torque; tune `rollInfluence`, `chassisOffset` and `angularDamping` for handling.
- Tires use a friction circle rather than a slip-curve tire model such as Pacejka's, and the engine is a force that fades with speed rather than a torque curve with gears.
- Wheel rays test every body in the world in turn, as `physics3d` raycasts do; keep large static scenery in triangle meshes or heightfields.
