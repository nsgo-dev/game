# 2D Rigid Bodies & Joints Architecture

This document records the design decision for 2D rigid-body dynamics in `github.com/nsgo-dev/game/packages/game/physics` ([#320](https://github.com/nsgo-dev/game/issues/320), [#321](https://github.com/nsgo-dev/game/issues/321), [#322](https://github.com/nsgo-dev/game/issues/322), [#804](https://github.com/nsgo-dev/game/issues/804)).

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
- **Continuous collision**: after positions are integrated, a dynamic body is swept from its start pose to its end pose against the static bodies its path crosses when any point of it moved farther than half its thickness that step, or always when it is a `Bullet`. Conservative advancement finds the time of impact, the earliest impact moves the body back to it, and the body's velocity into the surface is removed at the contact point, with the same restitution the contact solver applies. A body already touching a static body at the start of the step is not stopped by it, so a body sliding fast along the ground keeps its speed, and an impact that would leave a body that is not a `Bullet` only shallowly inside the shape is left to the regular contacts. Fast dynamic bodies are not swept against each other.
- **Sleeping**: a dynamic body moving slower than `SleepLinearThreshold` (2 px/s) and turning slower than `SleepAngularThreshold` (2°/s) accumulates rest time. The bodies linked by this step's contacts and joints form an island, and once every body of an island has rested for `TimeToSleep` (0.5 s) the whole island sleeps: its velocities are zeroed and the world neither integrates nor solves it, nor tests contacts between sleeping and static bodies. A sleeping body wakes, with the rest of its island, on `Wake`, a force, torque or impulse, a change to its pose or velocity, a contact with an awake body, a joint to an awake body, the removal or movement of a body it rests on, or a change to the gravity, damping or fluid it rests in. `RigidBody.Sleeping` reports the state and `RigidWorld.DisableSleep` turns it off.
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
- **Continuous collision**: fast bodies are swept against static bodies only, so a fast body can still pass through a thin dynamic body, and the impact ends the body's motion for that step rather than spending the rest of it sliding along the surface. Set `Bullet` on a body that must never end a step inside a static body.
- **Sleeping**: an island sleeps as a whole, so one body kept moving (a character pushing against a pile, a body under a constant `ApplyForce`) keeps every body it touches awake. A body moved in place by setting its fields wakes on the next step; changing the shape of a sleeping body does not wake it, so call `Wake` after editing one.
- **Scale**: the broadphase and sleeping keep a step proportional to the awake bodies and their contacts. In the performance scenario (`BenchmarkRigidWorld500Bodies` and `BenchmarkRigidWorld500BodiesSettled`), 500 boxes, circles, polygons and capsules falling into a bin take about 2 ms a step while they fall and 5 to 6 ms once the pile is packed and still awake, on a desktop CPU core, inside the 16.7 ms of a 60 fps frame. Games requiring large-scale multi-thousand-body simulations can integrate a dedicated physics engine alongside `packages/game`.

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
