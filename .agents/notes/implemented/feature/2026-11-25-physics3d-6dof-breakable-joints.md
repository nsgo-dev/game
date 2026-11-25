# Agent Note: Physics 3D Swing-Twist, 6DOF, Driven, and Breakable Joints

Status: implemented

## Problem

`packages/game/physics3d` previously had only five joints (`DistanceJoint`, `BallJoint`, `HingeJoint`, `SliderJoint`, `FixedJoint`). `BallJoint` supported only a symmetric cone limit with no twist bound, so `BuildRagdoll` shoulders and hips could spin freely around their own bone axis. `HingeJoint` and `SliderJoint` had only velocity motors (no spring or position drive for self-closing doors or suspensions), there was no generic 6DOF joint, and no joint reported its reaction force/torque or could break under load (#821).

Adding these capabilities must keep `Joint3D` backward-compatible for any external implementations, preserve zero-cgo pure-Go determinism across all NSGo targets, and avoid gimbal-lock instabilities on single-axis or multi-axis angular constraints.

## Decision

### Reaction force/torque tracking and breakable joints (`packages/game/physics3d/joints.go`, `world.go`)

- All built-in joints embed an unexported `jointReaction` state and expose `BreakForce`, `BreakTorque`, `ReactionForce() Vec3`, `ReactionTorque() Vec3`, and `Broken() bool`. `Joint3D` itself stays unchanged; `World.Step` discovers breakable joints via an unexported `breakableJoint` interface (mirroring `collideConnectedJoint`).
- `Prepare(dt)` resets per-step linear and angular accumulated impulses and records `dt`. Every linear anchor impulse and angular constraint impulse applied during `SolveVelocity` is summed; `ReactionForce()` and `ReactionTorque()` return those totals divided by `dt`.
- `World.evaluateBreakableJoints()` runs immediately after the velocity solver loop (before positional correction) so broken joints are removed in insertion order without snapping newly detached bodies back together. Breaking wakes dynamic bodies, stores deterministic `JointBreakEvent` values in `World.JointBreakEvents()`, and dispatches `World.OnJointBreak` after step reconciliation.

### Implicit spring/position drives on `HingeJoint` and `SliderJoint` (`joints.go`)

- `HingeJoint` gains `DriveTargetAngle`, `DriveStiffness`, `DriveDamping`, `DriveMaxTorque`, and `SliderJoint` gains `DriveTargetPos`, `DriveStiffness`, `DriveDamping`, `DriveMaxForce` (all zero-defaulted for full backward compatibility).
- Both reuse the implicit Euler ERP/CFM formulation (`softSpringParams`, matching `DistanceJoint`), solved before hard limits so limits always override drive impulses, and wake sleeping bodies in `Prepare` when offset from the target.

### `SwingTwistJoint` and `BuildRagdoll` (`joints_6dof.go`, `ragdoll.go`)

- `SwingTwistJoint` stores orthonormal local frames `(TwistAxis, PlaneAxis, TwistAxis x PlaneAxis)` on both bodies and performs exact quaternion swing-twist decomposition `qRel = qSwing * qTwist` around the local twist axis.
- Swing limits use a four-quadrant asymmetric elliptic cone `[SwingMin1, SwingMax1] x [SwingMin2, SwingMax2]` with exact ellipse-gradient outward normals for unilateral velocity clamping and radial projection for positional correction; twist is clamped independently in `[MinTwist, MaxTwist]` around `worldTB`.
- `BuildRagdoll` uses `SwingTwistJoint` for all non-hinge bones (keeping `HingeJoint` for elbows, knees, and explicit `HingeAxis`). `SkeletonJoint` adds optional asymmetric swing fields (`SwingMin1..SwingMax2`) and twist fields (`TwistLimit`, `MinTwist`, `MaxTwist`), defaulting to `±pi/4` twist when `SwingLimit > 0` so limbs cannot spin freely around their axis.

### `SixDOFJoint` (`joints_6dof.go`)

- `SixDOFJoint` configures `Linear [3]AxisConfig` and `Angular [3]AxisConfig` independently with `AxisMode` (`AxisFree`, `AxisLocked`, `AxisLimited`) and `DriveMode` (`DriveNone`, `DrivePosition`, `DriveVelocity`), plus `Stiffness`, `Damping`, `TargetPos`, `TargetVel`, and `MaxForce`.
- Linear solving uses the coupled 3×3 point-anchor solver when all three linear axes are locked and per-axis scalar rows otherwise.
- Angular solving uses exact quaternion lock when all three angular axes are locked, an unwrapped single-axis hinge path (singularity-free past `±pi` on X, Y, or Z) when two angular axes are locked, and the shortest-arc exponential-map rotation vector in frame A when two or three angular axes are unlocked, avoiding Euler gimbal lock at `±pi/2`.

## Alternatives considered

### Expand `Joint3D` interface with `ReactionForce`, `ReactionTorque`, and break methods

Adding methods directly to `Joint3D` would remove one internal type assertion in `World.Step`, but would break any caller implementing a custom `Joint3D`. Using an internal `breakableJoint` interface (consistent with `collideConnectedJoint`) preserves full interface compatibility while giving every built-in joint concrete `ReactionForce()`, `ReactionTorque()`, and `Broken()` methods.

### Keep `BallJoint` in `BuildRagdoll` unless twist fields are explicitly set

Leaving `BuildRagdoll` on `BallJoint` by default would avoid updating the joint-type assertion in `TestRagdollSkeletonKinematicDynamicAndBlended`, but any ragdoll built with `SwingLimit` alone would still have unconstrained axial limb spin, defeating the core motivation of #821. Switching non-hinge ragdoll joints to `SwingTwistJoint` with a `±pi/4` default twist bound when `SwingLimit > 0` fixes limb spin everywhere by default while honoring explicit asymmetric swing and twist ranges.

### Use intrinsic XYZ Euler angles for all `SixDOFJoint` angular constraints

Intrinsic XYZ Euler angles provide closed-form floating-axis Jacobians for three-axis limits, but suffer gimbal lock when the middle Y axis approaches `±pi/2` (even for a simple Y-axis wheel with X and Z locked). Specializing the 0-free-axis and 1-free-axis cases and using the symmetric exponential-map rotation vector for 2- and 3-free-axis cases avoids gimbal lock on every axis while keeping the implementation compact and deterministic.

## Consequences

- **Benefits**: Ragdolls constrain both asymmetric swing cones and axial twist out of the box; doors, suspensions, and articulated mechanisms can use implicit springs and position/velocity drives on `HingeJoint`, `SliderJoint`, and `SixDOFJoint`; destructible structures (such as plank bridges) can break deterministically on force or torque thresholds with accurate per-step reaction readouts.
- **Costs**: `BuildRagdoll` now emits `*SwingTwistJoint` instead of `*BallJoint` for non-hinge limbs (callers type-asserting `*BallJoint` on `Ragdoll.Joints` must expect `*SwingTwistJoint`). For `SixDOFJoint` configurations with two or three simultaneously unlocked angular axes, the exponential-map rotation vector approximates coupled large-angle (>90°) non-commutative rotations rather than exact multi-axis Euler frames; applications requiring large simultaneous swing and twist ranges should prefer `SwingTwistJoint`.
