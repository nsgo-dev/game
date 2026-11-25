# 2D Rigid Bodies & Joints Architecture

This document records the design decision for 2D rigid-body dynamics in `github.com/nsgo-dev/game/packages/game/physics` ([#320](https://github.com/nsgo-dev/game/issues/320), [#321](https://github.com/nsgo-dev/game/issues/321), [#322](https://github.com/nsgo-dev/game/issues/322)).

## Decision

Implement a lightweight, pure-Go, deterministic 2D rigid-body and constraint solver (`RigidWorld`, `RigidBody`, and `Joint` types) directly inside `packages/game/physics`, rather than binding a C/C++ physics engine (such as Box2D or Chipmunk2D) via cgo or taking on an external Go dependency.

## Rationale

1. **Zero cgo and zero third-party dependencies**: NSGo games target macOS, iOS, Windows, Linux, Android, and Web (WASM). Wrapping Box2D or Chipmunk via cgo adds C/C++ cross-compilation toolchains, platform-specific static libraries, and WASM bridging complexity. A pure-Go solver builds identically on every target with standard `go tool nsgo`.
2. **Determinism on fixed steps**: `game.Loop` steps gameplay at a fixed `dt`. Keeping the solver free of map iteration, background threads, and platform-specific C math libraries ensures identical inputs produce repeatable simulation trajectories across runs and targets.
3. **Unified coordinate system and types**: `RigidBody` reuses `Shape` (`Box`, `Circle`, `Polygon`, `Capsule`, `Segment` and `Chain`), `LayerMask`, and NSGo's screen-space coordinates (+X right, +Y down, clockwise radians) directly, avoiding translation layers between kinematic physics, queries, sprites, and rigid bodies.
4. **Right-sized for 2D games**: Most 2D NSGo games need arcade dynamics—falling crates, stacks, projectiles, ragdolls, pendulums, ropes, and spring suspensions—rather than a full industrial CAD or continuous-collision solver.

## Solver Design

- **Integration**: Symplectic (semi-implicit) Euler. Forces, gravity, and linear/angular damping update velocities first; the constraint solver corrects velocities; updated velocities then advance positions and angles.
- **Contact generation**: Deterministic `i < j` body pair ordering with `LayerMask` filtering and rotated AABB culling. Narrow-phase handles Circle–Circle, Circle–OBB, and OBB–OBB (2D SAT with Sutherland–Hodgman clipping to produce up to 2 contact points so flat box stacks remain stable). Every pair involving a polygon, capsule, segment or chain treats both shapes as rounded hulls, a convex core plus a radius, and runs the same SAT and clipping on the cores, as Box2D's `b2CollidePolygons` does: up to 2 points for faces lying on faces, one point otherwise. A chain yields one manifold per touching segment and drops the normals its interior vertices would wrongly add, so bodies slide and roll across its seams.
- **Velocity resolution**: Sequential impulse solver (`RigidWorld.SolverIterations`, defaulting to 10) with accumulated normal and Coulomb friction clamping and a velocity threshold on restitution to prevent micro-bouncing in resting stacks.
- **Positional correction**: Direct linear projection with slop (`0.01`) after position integration, resolving residual overlap without injecting artificial bounce velocity.
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

- **Supported shapes**: oriented `Box` and `Circle`, convex `Polygon` (up to 8 vertices, optionally rounded) and `Capsule` for dynamic bodies, and `Segment` and `Chain` for static ones. Concave terrain is a `Chain` (or several convex polygons); concave dynamic bodies and swept continuous collision detection (CCD) are intentionally out of scope for the built-in solver, so a body whose center crosses a segment or chain within one step passes through it.
- **Scale**: Designed for tens to low hundreds of active rigid bodies per scene; games requiring large-scale multi-thousand-body simulations can integrate a dedicated physics engine alongside `packages/game`.

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
