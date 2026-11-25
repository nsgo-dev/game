# 2D Rigid Bodies & Joints Architecture

This document records the design decision for 2D rigid-body dynamics in `github.com/nsgo-dev/game/packages/game/physics` ([#320](https://github.com/nsgo-dev/game/issues/320), [#321](https://github.com/nsgo-dev/game/issues/321), [#322](https://github.com/nsgo-dev/game/issues/322), [#804](https://github.com/nsgo-dev/game/issues/804), [#805](https://github.com/nsgo-dev/game/issues/805)).

## Decision

Implement a lightweight, pure-Go, deterministic 2D rigid-body and constraint solver (`RigidWorld`, `RigidBody`, and `Joint` types) directly inside `packages/game/physics`, rather than binding a C/C++ physics engine (such as Box2D or Chipmunk2D) via cgo or taking on an external Go dependency.

## Rationale

1. **Zero cgo and zero third-party dependencies**: NSGo games target macOS, iOS, Windows, Linux, Android, and Web (WASM). Wrapping Box2D or Chipmunk via cgo adds C/C++ cross-compilation toolchains, platform-specific static libraries, and WASM bridging complexity. A pure-Go solver builds identically on every target with standard `go tool nsgo`.
2. **Determinism on fixed steps**: `game.Loop` steps gameplay at a fixed `dt`. Keeping the solver free of map iteration, background threads, and platform-specific C math libraries ensures identical inputs produce repeatable simulation trajectories across runs and targets.
3. **Unified coordinate system and types**: `RigidBody` reuses `Shape` (`Box`, `Circle`, `Polygon`, `Capsule`, `Segment` and `Chain`), `LayerMask`, and NSGo's screen-space coordinates (+X right, +Y down, clockwise radians) directly, avoiding translation layers between kinematic physics, queries, sprites, and rigid bodies.
4. **Right-sized for 2D games**: Most 2D NSGo games need arcade dynamics—falling crates, stacks, projectiles, ragdolls, pendulums, ropes, and spring suspensions—rather than a full industrial CAD solver.

## Solver Design

- **Integration**: Symplectic (semi-implicit) Euler. Forces, gravity, and linear/angular damping update velocities first; the constraint solver corrects velocities; updated velocities then advance positions and angles.
- **Broadphase**: sort and sweep. Each pass sorts the bodies' bounding boxes along X, ties broken by body index, sweeps the sorted list for boxes that overlap on both axes, and sorts the pairs by `(i, j)`. That is exactly the set and order of pairs the old `i < j` double loop tested, with the same `LayerMask` filtering, so the contacts and their order do not change; only the work does, which now follows the number of overlapping pairs instead of the square of the number of bodies. Contact detection and every iteration of the positional pass use it.
- **Contact generation**: Narrow-phase handles Circle–Circle, Circle–OBB, and OBB–OBB (2D SAT with Sutherland–Hodgman clipping to produce up to 2 contact points so flat box stacks remain stable). Every pair involving a polygon, capsule, segment or chain treats both shapes as rounded hulls, a convex core plus a radius, and runs the same SAT and clipping on the cores, as Box2D's `b2CollidePolygons` does: up to 2 points for faces lying on faces, one point otherwise. A chain yields one manifold per touching segment and drops the normals its interior vertices would wrongly add, so bodies slide and roll across its seams. A manifold of two overlapping shapes also keeps a clipped point that is still up to 1 px apart, which the solver lets close before pushing, so a box resting a hair off level keeps both corners in contact instead of rocking from one to the other. Every point carries a feature id naming the reference face and the incident vertex or clipping plane it came from, and the reference face only changes when another face separates clearly more, so the same physical contact keeps its id from step to step.
- **Velocity resolution**: Sequential impulse solver (`RigidWorld.SolverIterations`, defaulting to 10) with accumulated normal and Coulomb friction clamping and a velocity threshold on restitution to prevent micro-bouncing in resting stacks. Solving is warm-started: the normal and friction impulses each contact point ends a step with are cached per pair of bodies, manifold and feature id, scaled by the ratio of step lengths, and applied before the next step's iterations, so a resting stack starts each step already holding its weight.
- **Positional correction**: Direct linear projection with slop (`0.01`) after position integration, resolving residual overlap without injecting artificial bounce velocity.
- **Continuous collision**: after positions are integrated, a dynamic body is swept from its start pose to its end pose against the static bodies its path crosses when any point of it moved farther than half its thickness that step, or always when it is a `Bullet`. Conservative advancement finds the time of impact, the earliest impact moves the body back to it, and the body's velocity into the surface is removed at the contact point, with the same restitution the contact solver applies. A static surface the body rests on at the start of the step does not stop it, and neither does a static piece lying wholly behind that surface, such as the next tile of a tiled floor or the next segment of a flat chain, so a body sliding fast along the ground keeps its speed over the seams. A body fired at a static body it already touches, with no contact with it yet, stops where it is. An impact that would leave a body that is not a `Bullet` only shallowly inside the shape is left to the regular contacts. Fast dynamic bodies are not swept against each other.
- **Sleeping**: a dynamic body moving slower than `SleepLinearThreshold` (2 px/s) and turning slower than `SleepAngularThreshold` (2°/s) accumulates rest time. The bodies linked by this step's contacts and joints form an island, and once every body of an island has rested for `TimeToSleep` (0.5 s) the whole island sleeps: its velocities are zeroed and the world neither integrates nor solves it, nor tests contacts between sleeping and static bodies. A sleeping body wakes, with the rest of its island, on `Wake`, a force, torque or impulse, a change to its pose or velocity, a contact with an awake body, a joint to an awake body, the removal or movement of a body it rests on, or a change to the gravity, damping or fluid it rests in. A body that a `MouseJoint` holds, or that a joint with its motor on drives, stays awake while the drive is on, so writing a new `Target` or `MotorSpeed` takes effect on the next step. `RigidBody.Sleeping` reports the state and `RigidWorld.DisableSleep` turns it off.
- **Joints**:
  - `DistanceJoint` (`NewDistanceJoint`, `NewRopeJoint`): fixed-length rod constraint with optional `Stiffness` and XPBD `Compliance`, plus unilateral rope / max-distance mode (`MaxOnly: true`) and slack range mode (`[MinLength, MaxLength]`).
  - `SpringJoint` (`NewSpringJoint`): damped Hooke's law spring using unconditionally stable implicit impulse integration.
  - `RevoluteJoint` (`NewRevoluteJoint`): 2-DOF pin/hinge constraint using a 2×2 effective-mass block matrix, with optional angular limits (`EnableLimit`, `LowerAngle`, `UpperAngle`), angular motor (`EnableMotor`, `MotorSpeed`, `MaxMotorTorque`), and `JointAngle()` / `JointSpeed()` readouts.
  - `PrismaticJoint` (`NewPrismaticJoint`): 1-DOF linear slider along `LocalAxisA` with locked relative rotation (`ReferenceAngle`), optional translation limits (`EnableLimit`, `LowerTranslation`, `UpperTranslation`), linear motor (`EnableMotor`, `MotorSpeed`, `MaxMotorForce`), and `JointTranslation()` / `JointSpeed()` readouts.
  - `WeldJoint` (`NewWeldJoint`): 3-DOF rigid weld solved with a coupled 3×3 block matrix, or a soft damped spring pose lock via `FrequencyHz`/`DampingRatio` (or direct `Stiffness`/`Damping`).
  - `WheelJoint` (`NewWheelJoint`): 2D vehicle suspension constraining a wheel hub along `LocalAxisA` with a soft suspension spring (`FrequencyHz`/`DampingRatio` or `Stiffness`/`Damping`), optional suspension travel limits (`EnableLimit`, `LowerTranslation`, `UpperTranslation`), and a rotational drive motor (`EnableMotor`, `MotorSpeed`, `MaxMotorTorque`).
  - `MouseJoint` (`NewMouseJoint`): single-body target joint pulling `Anchor` toward a settable world `Target` (`SetTarget`) with a damped spring capped at `MaxForce`.
  - **Connected collisions, reactions, and breaking**: two-body joints disable contact between their connected bodies by default (`CollideConnected: false`). Every built-in joint tracks per-step `ReactionForce() (fx, fy float32)`, `ReactionTorque() float32`, and `Broken() bool`, and supports optional `BreakForce` / `BreakTorque` thresholds. Joints that exceed a threshold are removed deterministically in insertion order before positional correction and reported via `RigidWorld.JointBreakEvents()` and `RigidWorld.OnJointBreak`.

## Trade-offs & Scope

- **Supported shapes**: oriented `Box` and `Circle`, convex `Polygon` (up to 8 vertices, optionally rounded) and `Capsule` for dynamic bodies, and `Segment` and `Chain` for static ones. Concave terrain is a `Chain` (or several convex polygons); concave dynamic bodies are out of scope for the built-in solver.
- **Continuous collision**: fast bodies are swept against static bodies and tiles only, so a fast body can still pass through a thin dynamic or kinematic body, and the impact ends the body's motion for that step rather than spending the rest of it sliding along the surface. Set `Bullet` on a body that must never end a step inside a static body.
- **Sleeping**: an island sleeps as a whole, so one body kept moving (a character pushing against a pile, a body under a constant `ApplyForce`) keeps every body it touches awake. A body moved in place by setting its fields wakes on the next step; changing the shape of a sleeping body does not wake it, so call `Wake` after editing one.
- **Scale**: the broadphase and sleeping keep a step proportional to the awake bodies and their contacts. In the performance scenario (`BenchmarkRigidWorld500Bodies` and `BenchmarkRigidWorld500BodiesPacked`), 500 boxes, circles, polygons and capsules falling into a bin take about 2 ms a step while they fall and 5 to 6 ms once the pile is packed and still awake, on one core of a Ryzen 9 3900X running Linux, inside the 16.7 ms of a 60 fps frame. Games requiring large-scale multi-thousand-body simulations can integrate a dedicated physics engine alongside `packages/game`.

## Kinematic bodies, sensors, tiles and contact events

- **Kinematic bodies**: `RigidBody.Kinematic` makes a body move only as it is told, by `VX`, `VY` and `AngularVel` (with no gravity, force, damping or area effect) or by `SetTargetPose(x, y, angle)`, which gives the body the velocity that reaches the pose in the next step and lands it exactly there, the way an animation drives a moving platform; a body given no new target stops. The solver treats it as having infinite mass, so it pushes the dynamic bodies in its way and carries the ones resting on it by friction, and nothing moves it. It never collides with static or other kinematic bodies or with tiles, never sleeps, and keeps the bodies it moves against awake. Assigning `X` and `Y` directly teleports it: bodies it lands in are pushed out but not given its motion.
- **Sensors**: `RigidBody.Sensor` makes a body of any kind detect overlaps instead of colliding. After each step, `RigidWorld.SensorEvents()` (and `OnSensor`) lists the bodies that began or ended overlapping a sensor at their final poses. A sensor never detects another sensor, and `World.MoveAndSlide` ignores static sensors.
- **Tiles**: `RigidWorld.Tiles` takes any `TileGrid`, the same interface `World` and `MoveAndSlide` use. Each `TileSolid` cell collides with dynamic bodies as a box, and each `TileOneWay` cell as its top face only, catching a body that comes down onto it and letting one from below or the side pass. A face that a neighbouring solid tile covers never pushes: a contact leaning into it is clipped against the tile's open face instead, as a chain does at a flat vertex, so boxes slide and circles and capsules roll across tiled floors without catching on seams. The tiles of one flat surface correct a body's position once, by the deepest of them. `TileLayer` and `TileMask` filter tiles like a body's layers, `TileFriction` (zero means `DefaultTileFriction`, 0.4; negative is frictionless) and `TileRestitution` set their material, fast bodies are swept against them like any static body, and a sleeping body wakes when the tile under it is removed or the grid is moved or resized.
- **Queries**: `RigidWorld.Raycast`, `RaycastFirst`, `QueryPoint` and `QueryOverlap` see each body at its true pose (`RigidBody.WorldShape`, a turned box as a polygon) and the tiles, and return the `RaycastHit` and `OverlapHit` types of `World`'s queries with `RigidBody` set. `QueryMask` selects bodies, sensors (`QuerySensors`, which `QueryAll` leaves out, and which `ShapeCastOptions.QueryMask` needs for shape casts to hit sensors), solid and one-way tiles, and a `LayerMask` filters bodies by layer. Hits come in a fixed order: by distance for rays, bodies in `Bodies` order before tiles in row-major order for overlaps.
- **Contact events**: after each step, `RigidWorld.ContactEvents()` (and `OnContact`) lists the contacts that began, with their point, normal and `ApproachSpeed`, the speed at which the two closed before the step resolved the contact, and the contacts that ended because the bodies separated or one left the world. A body touching any number of tiles counts as one contact with the tiles, so a crate sliding over a tiled floor begins touching it once. A contact begins when the step first finds the bodies overlapping and ends on the first step that does not; contacts between bodies that are asleep carry over, so a resting pile reports nothing. Begin events come in the order the step finds contacts (pairs in `Bodies` order, then each body with the tiles), followed by end events, so the same inputs give the same events in the same order on every run and after `Save` and `Restore`.
- **Snapshots**: `RigidBodySnapshot` holds `Kinematic`, `Sensor` and a pending or last target, and `RigidWorldSnapshot` holds a `*Grid` `Tiles` (`GridSnapshot` now keeps the grid's origin too), the tile settings, tile entries of the contact cache, and the contacts and sensor overlaps the next step compares its own with. `Restore` clears `ContactEvents` and `SensorEvents`.

```go
level := physics.NewGrid(40, 12, 16, 16)
level.FillRect(0, 10, 40, 1, physics.TileSolid)
w := &physics.RigidWorld{GravityY: 600, Tiles: level}

lift := physics.NewRigidBox(100, 120, 96, 12, 1)
lift.Kinematic = true
crate := physics.NewRigidBox(100, 90, 20, 20, 1)
w.AddBody(lift, crate)
w.OnContact = func(ev physics.ContactEvent) {
	if ev.Kind == physics.ContactBegin && ev.ApproachSpeed > 200 {
		playThud(ev.Point, ev.ApproachSpeed)
	}
}

// Each fixed step, the lift follows its animation and carries the crate.
lift.SetTargetPose(liftX(t), 120, 0)
w.Step(dt)

if hit, ok := w.RaycastFirst(eye, target, physics.QueryAll, 0); ok && hit.RigidBody == crate {
	// The crate is in sight, at hit.Point, facing (hit.NormalX, hit.NormalY).
}
```

## Sleeping and fast bodies

```go
w := &physics.RigidWorld{GravityY: 600}
floor := physics.NewStaticRigidBox(100, 110, 400, 20)
crate := physics.NewRigidBox(100, 90, 20, 20, 1)
shot := physics.NewRigidCircle(0, 0, 4, 0.1)
shot.Bullet = true // never ends a step inside a static body
w.AddBody(floor, crate, shot)

// ... after half a second at rest:
if crate.Sleeping() {
	crate.ApplyImpulse(0, -100) // wakes it and every body it touches
}
```

## Bodies from any shape

`NewRigidBody(shape, mass)` places the body's center of mass at `shape.Centroid()` and takes its inertia from `shape.MomentOfInertia(mass)`; `NewStaticRigidBody(shape)` makes level geometry. A `Box` or `Circle` gives the same body as `NewRigidBox` or `NewRigidCircle`. Segments and chains have no area, so their bodies are always static.

A polygon, capsule, segment or chain body keeps its shape in local coordinates and writes `Shape` back rotated by `Angle` after each step, so `World` queries, triggers and `DrawShape` see it at its true pose. A polygon or capsule assigned to `Shape`, in a struct literal or between steps, stays where it is drawn: the next step moves `X` and `Y` to its centroid, so the body turns about its center of mass, and recomputes an `Inertia` the body derived itself (an `Inertia` you set to another value is kept). A segment or chain is anchored at the current `X` and `Y`.

```go
ramp, _ := physics.NewPolygon([]core.Point{{X: 0, Y: 50}, {X: 0, Y: 200}, {X: 300, Y: 200}}, 0)
rw.AddBody(physics.NewStaticRigidBody(physics.PolygonShape(ramp)))
crate := physics.NewRigidBody(physics.PolygonShape(physics.NewBoxPolygon(64.5, 70.6, 20, 20, 0.4636)), 1)
rw.AddBody(crate) // slides down the ramp
```

## Example

```go
var rw physics.RigidWorld
rw.GravityY = 600

floor := physics.NewStaticRigidBox(160, 220, 320, 20)
crate := physics.NewRigidBox(160, 100, 32, 32, 1)
pendulum := physics.NewRigidCircle(240, 140, 12, 1)
pivot := physics.NewStaticRigidBox(240, 60, 8, 8)

rw.AddBody(floor, crate, pendulum, pivot)
rope := physics.NewRopeJoint(pivot, pendulum, core.Point{}, core.Point{}, 80)
rope.BreakForce = 4000
rw.AddJoint(rope)
rw.OnJointBreak = func(ev physics.JointBreakEvent) {
	// Handle detached rope or destructible link deterministically.
	_ = ev
}

loop := &game.Loop{
	Rate: 60,
	Update: func(dt time.Duration) {
		rw.Step(dt)
	},
}
```
