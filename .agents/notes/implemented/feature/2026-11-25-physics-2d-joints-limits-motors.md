# Agent Note: Physics 2D Joint Types, Limits, Motors, and Break Thresholds

Status: implemented

## Problem

`packages/game/physics` previously provided only three 2D rigid-body joints (`DistanceJoint` as a fixed-length rod, `SpringJoint`, and a free-pin `RevoluteJoint`). Games needing doors, drawbridges, flippers, elevators, two-wheel vehicles, pointer dragging, slack ropes, or breakable chains lacked angle limits and motors on revolute joints, prismatic (slider), weld, wheel, and mouse/target joints, unilateral rope/max-distance constraints, reaction force/torque readouts, and break thresholds (#807).

Extending the 2D joint suite must keep the `Joint` interface backward-compatible, preserve `packages/game/physics` as a pure-Go zero-cgo **Stable** package, maintain FMA-free cross-architecture determinism across amd64 and arm64 (see [physics-save-determinism](2026-11-25-physics-save-determinism.md)), and serialize all new joint configuration, solver state, and break events cleanly in `RigidWorldSnapshot`.

## Decision

### Shared reaction tracking, connected-body filtering, and breaking (`packages/game/physics/joints.go`, `rigid.go`)

- `Joint` keeps its minimal three-method contract (`preStep`, `solveVelocity`, `solvePosition`). `RigidWorld.Step` discovers connected body pairs, `CollideConnected`, and break thresholds via unexported interfaces (`jointBodies`, `collideConnectedJoint`, `breakableJoint`), matching the 3D design in [physics3d-6dof-breakable-joints](2026-11-25-physics3d-6dof-breakable-joints.md).
- Every built-in joint embeds an unexported `jointReaction` accumulator that resets in `preStep(dt)`, sums every linear and angular constraint impulse applied during `solveVelocity()`, and exposes `ReactionForce() (fx, fy float32)`, `ReactionTorque() float32`, and `Broken() bool`, plus optional `BreakForce` and `BreakTorque` fields.
- Two-body joints default `CollideConnected` to `false`, suppressing contacts between connected bodies during both narrowphase contact detection and positional slop correction (essential for overlapping wheels, hinges, and chain links).
- `RigidWorld.evaluateBreakableJoints()` runs immediately after the sequential impulse velocity loop and before position integration and positional correction, removing broken joints in insertion order, clearing trailing slice slots, rebuilding ignored pairs if any joint broke, recording deterministic `JointBreakEvent` values for `RigidWorld.JointBreakEvents()`, and invoking `RigidWorld.OnJointBreak` after step completion.

### Joint types, limits, and motors (`packages/game/physics/joints.go`)

- `DistanceJoint` gains `MinLength`, `MaxLength`, `MaxOnly` (unilateral rope/max-distance mode, also constructed via `NewRopeJoint`), and accumulated unilateral impulse clamping so slack ropes apply zero force until stretched past `MaxLength`.
- `RevoluteJoint` gains `ReferenceAngle`, angular limits (`EnableLimit`, `LowerAngle`, `UpperAngle`), angular motor (`EnableMotor`, `MotorSpeed`, `MaxMotorTorque`), and `JointAngle()` / `JointSpeed()` helpers. Motors solve before limits so hard limits always override motor torque.
- `PrismaticJoint` (`NewPrismaticJoint`) solves a coupled 2×2 perpendicular-and-angle lock (falling back to scalar perpendicular lock when both bodies have zero inverse inertia), optional linear motor along `LocalAxisA`, and unilateral translation limits (`LowerTranslation`, `UpperTranslation`), exposing `JointTranslation()` and `JointSpeed()`.
- `WeldJoint` (`NewWeldJoint`) solves a coupled symmetric 3×3 block matrix (`symMat3_64`, with 2×2 fallback when rotation is locked) for rigid welds, or implicit Euler ERP/CFM soft springs via `FrequencyHz`/`DampingRatio` or direct `Stiffness`/`Damping` (skipping hard positional projection when soft).
- `WheelJoint` (`NewWheelJoint`) constrains a wheel hub to slide along `LocalAxisA` with an implicit suspension spring (`FrequencyHz`/`DampingRatio` or `Stiffness`/`Damping`), optional suspension travel limits, perpendicular strut lock, and a rotational drive motor (`MotorSpeed`, `MaxMotorTorque`).
- `MouseJoint` (`NewMouseJoint`, `SetTarget`) pulls a single body's local `Anchor` toward a world `Target` via a 2×2 implicit spring-damper whose accumulated per-step impulse magnitude is clamped to `MaxForce * dt`.

### Snapshots and FMA-free cross-architecture determinism (`packages/game/physics/snapshot.go`, `joints.go`)

- `RigidJointSnapshot` adds fields and `"prismatic"`, `"weld"`, `"wheel"`, and `"mouse"` kinds alongside `"distance"`, `"spring"`, and `"revolute"`, plus `RigidWorldSnapshot.JointBreakEvents` and per-joint `jointReaction` state, preserving in-place joint pointers on `Restore`.
- Every multiply-add/sub expression and impulse product in `joints.go` wraps its multiplication in explicit `float64(...)` conversions and uses `internal/detmath` (`Sincos`, `Hypot`), verified with `go build -gcflags=-S ./physics` to emit zero arm64 `FMADD`/`FMSUB`/`FNMADD`/`FNMSUB` instructions across `joints.go`, `rigid.go`, and `snapshot.go`.
- Golden checksums in `packages/game/physics/snapshot_test.go` (`TestGoldenTrajectory2D`) and `packages/game/net/rollback_physics_test.go` (`TestRollbackPhysics2DSoak`) are updated to reflect the expanded `RigidWorld.Checksum()` digest covering `jointReaction`, new joint fields, and `jointBreakEvents`.

## Alternatives considered

### Separate `RopeJoint` type instead of `MaxOnly` / `[MinLength, MaxLength]` on `DistanceJoint`

A dedicated `RopeJoint` struct would duplicate anchor, effective-mass, compliance, and damping logic from `DistanceJoint`. Supporting `MaxOnly` and `[MinLength, MaxLength]` directly on `DistanceJoint` with a `NewRopeJoint` convenience constructor keeps one implementation for rods, tethers, and range constraints while matching Box2D v3's distance joint model.

### Expose reaction and break methods directly on the `Joint` interface

Extending `Joint` with `ReactionForce`, `ReactionTorque`, `Broken`, and break checks would avoid internal type assertions in `RigidWorld.Step`, but would break any external or custom `Joint` implementation. Unexported optional interfaces (`jointBodies`, `collideConnectedJoint`, `breakableJoint`) keep `Joint` backward-compatible while giving all built-in joints concrete public methods.

### Skip hashing `jointReaction` in `RigidWorld.Checksum()` to leave pre-existing golden checksums unchanged

Omitting `jointReaction` and `jointBreakEvents` from `Checksum()` would preserve the old golden constants in `snapshot_test.go` and `rollback_physics_test.go`, but would make `Checksum()` blind to snapshot/restore bugs in `ReactionForce()`, `ReactionTorque()`, `Broken()`, and `JointBreakEvents()`. Hashing the complete state and updating the two golden constants keeps the checksum contract sound.

## Consequences

### Benefits

- 2D games can build limited hinges, motorized arms, sliders/elevators, rigid or spring welds, suspended two-wheel vehicles, pointer-dragged props, slack ropes, and breakable chains with deterministic pure-Go physics and full rollback/snapshot support.
- Zero arm64 FMA instructions in the 2D joint and rigid-body solver guarantee bit-identical trajectories across amd64 and arm64 CI targets.
- Existing `DistanceJoint`, `SpringJoint`, and `RevoluteJoint` callers keep source compatibility.

### Costs

- Two-body joints now default `CollideConnected` to `false`; callers that relied on connected bodies colliding with each other must explicitly set `CollideConnected = true`.
- `RigidWorld.Checksum()` outputs change for worlds containing joints because joint reaction accumulators, new joint parameters, and `jointBreakEvents` are now included in the digest.
