# Agent Note: Physics 3D Character Controller on Moving Platforms

Status: implemented

## Problem

`physics3d.CharacterController` previously walked static floors, slopes, stairs, and walls without tracking the body supporting it. Standing on a rising, descending, translating, or rotating kinematic or dynamic platform caused the capsule to slide off, fall behind, or sink into the platform; jumping or walking off a moving platform lost its momentum; and moving kinematic geometry (such as sliding doors, walls, or crushers) could pass through the capsule or leave it stuck without reporting a crush (#813).

The solution must work regardless of whether callers drive kinematic bodies by mutating `Pos`/`Rot` directly or via `Vel`/`AngVel`, regardless of call order between `World.Step` and `CharacterController.Move`, and must preserve bit-for-bit determinism across runs.

## Decision

### Exact rigid-body carry and ground reporting (`packages/game/physics3d/character.go`)

- `CharacterController` and `MoveResult` now report `GroundBody *Body`, `GroundVelocity Vec3` (linear plus angular velocity at the ground contact point), `GroundYawDelta float32`, and `Crushed bool`. `CharacterController` also exposes `Yaw float32`, `ApplyPlatformYaw bool` (enabled by `NewCharacterController`), and `PlatformOnLeave` (`PlatformLeaveAddVelocity`, `PlatformLeaveAddUpwardVelocity`, `PlatformLeaveDoNothing`).
- The controller records the previous pose `(groundBodyPos, groundBodyRot)` of its supporting body and a deterministic lookup table `kinematicPoses` (keyed by `Body.ID`, validated by pointer identity, and only queried while iterating `world.bodies` in insertion order — never range-iterated).
- Before Phase 1/2 movement, when grounded on a movable (`Kinematic || !Static`) body and not jumping, the controller computes the exact rigid-body displacement `curPos + (curRot * prevRot^-1) * (Pos - prevPos) - Pos` and applies it while ignoring the supporting body (fast-path direct sweep when unobstructed, falling back to horizontal slide + vertical sweep when a wall or ceiling blocks carry). Using the exact rigid transform eliminates circular-arc chord drift over long rotations.
- Yaw rotation around `+Y` is extracted from `deltaRot` via twist decomposition (`2 * atan2(dq.Y, dq.W)` on the canonicalized shortest-arc quaternion), reported in `GroundYawDelta`, and added to `Yaw` when `ApplyPlatformYaw` is true.

### `PlatformOnLeave` velocity inheritance

- Because `CharacterController.Move` overwrites horizontal `Vel.X`/`Vel.Z` from `wishMove` on every tick when horizontal gravity is zero, inherited horizontal platform momentum is stored in an internal `airCarryVel` added alongside `wishMove` while airborne, damped by area fluid drag/linear damping, projected onto any wall hit while airborne, and cleared upon landing.
- On a jump tick (`wasGrounded && wishMove.Y > 0`), the controller skips pre-move platform carry and walkable floor depenetration against the previous ground body so the jump receives exactly 1x the configured `PlatformOnLeave` velocity instead of double-counting the platform's displacement; a post-move depenetration pass guarantees the capsule still clears a fast rising platform under `PlatformLeaveDoNothing`.
- On walk-off (`wasGrounded && !Grounded` without jumping), `PlatformOnLeave` is applied once at the end of the step.

### Kinematic intrusion resolution and crush detection

- Before general depenetration, each moved kinematic body (other than the already-carried ground body) whose swept AABB overlaps the capsule is sub-stepped from its previous pose to its current pose (`lerpVec3` + `Slerp`, step `<= max(Radius*0.35, 0.02)`). Each contact pushes the capsule out along `ct.Normal` via a sweep ignoring the pushing body so thin or fast kinematic doors never flip normals and tunnel through the character; angled non-opposing obstructions deflect the remainder tangentially.
- Iterative depenetration (`maxDepenIters = 8`) records every significant contact normal (`Depth > 1e-3`) and whether a movable body produced it. If residual overlap `> 1e-3` remains after all iterations and any pair of recorded normals opposes (`n1.Dot(n2) < -0.5`) with at least one movable body involved, `Crushed` is set to `true` and the capsule settles at the midpoint of the final oscillation step.

## Alternatives considered

### Euler velocity integration for platform carry (`(Vel + AngVel x r) * dt`)

Carrying by first-order tangential velocity is simpler than tracking previous body poses, but it fails whenever callers move kinematic platforms by setting `Pos`/`Rot` directly (the dominant pattern in `physics3d`) and introduces quadratic radial outward drift on spinning platforms over 600+ steps. Tracking exact `(prevPos, prevRot) -> (curPos, curRot)` per `Move` call eliminates drift and works identically for pose-driven kinematic bodies, dynamic bodies, and any call order with `World.Step`, while keeping `Vel`/`AngVel` as a fallback for stationary conveyor-style kinematic bodies.

### Rely on `Body.lastPos`/`Body.lastRot` stored by `World.Step`

Reusing `World.Step`'s pose snapshot would avoid storing pose state on `CharacterController`, but `World.Step` overwrites `lastPos`/`lastRot` at the end of each step. Any loop that calls `World.Step(dt)` before `CharacterController.Move(...)` (or skips `World.Step` for kinematic-only worlds) would observe zero pose delta. Keeping pose tracking inside `CharacterController` makes it independent of `World.Step` ordering and leaves `world.go` untouched.

### Require an explicit `MovingPlatform` wrapper list (mirroring 2D `physics.CharacterBody`)

Passing a separate `[]*MovingPlatform` slice to `Move` would match 2D `physics.CharacterBody`, but would break the existing `CharacterController.Move(world, wishMove, dt)` signature and force callers to mirror bodies already in `World`. Discovering kinematic and dynamic motion directly from `World.bodies` preserves the existing `Move` signature and supports dynamic platforms automatically.

## Consequences

- **Benefits**: Characters ride rising, descending, translating, and rotating kinematic or dynamic platforms for 10+ seconds with zero drift; jumping and walking off obey Godot-style `PlatformOnLeave` semantics; sliding doors/walls push characters cleanly without tunneling; horizontal and vertical crushers reliably report `Crushed`; and `Move` stays 100% deterministic with no signature changes.
- **Costs**: `CharacterController` holds a small per-world `kinematicPoses` map (allocated lazily only when kinematic bodies exist in the world) and performs swept-AABB checks against moved kinematic bodies each `Move`. A kinematic body moved before the controller's very first `Move` call has no prior pose yet, so sub-stepped intrusion applies from the second `Move` onward (frame-1 overlaps still resolve through iterative depenetration).
