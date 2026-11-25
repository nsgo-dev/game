# Agent Note: XPBD Ropes, Cloth and Soft Bodies in physics and physics3d

Status: implemented

## Problem

Neither `packages/game/physics` (2D) nor `packages/game/physics3d` (3D) had a particle-constraint solver for ropes, cloth, or deformable soft bodies. Building ropes or cloth out of rigid bodies and joints stretches under load, costs significantly more per link, and does not collide smoothly with 2D tile grids or 3D triangle meshes and heightfields.

## Decision

Both `packages/game/physics` and `packages/game/physics3d` add a self-contained, pure-Go, deterministic Extended Position-Based Dynamics (XPBD) solver (`SoftWorld`) in standalone files (`xpbd.go`, `xpbd_debugdraw.go`, `xpbd_debugdraw_release.go`) without modifying existing rigid-body shape or joint files or introducing any new package dependencies:

- **XPBD substepping and constraints**: `SoftWorld.Step(dt)` subdivides each fixed frame into `Substeps` (default 8) with `Iterations` Gauss-Seidel passes per substep (default 6), alternating distance-constraint sweep order for chain convergence and solving in `float64` internally for precision and bit-for-bit determinism (no map iteration anywhere). Both packages support bilateral and unilateral (`MaxOnly`) `DistanceConstraint`, singularity-free angular `BendingConstraint` (signed 2D hinge angle; Bridson/XPBD 3D dihedral angle without `arccos` division-by-zero on flat cloth), and volume/area preservation (`AreaConstraint` + 2D Procrustes `ShapeMatchConstraint` in 2D; tetrahedral `VolumeConstraint` in 3D).
- **Two-way rigid-body coupling across substeps**: When a `RigidAttachment` links a particle to a `*RigidBody` (2D) or `*Body` (3D), `SoftWorld.Step(dt)` interpolates each attached rigid body from its previous end-of-step pose (`bodyHistory`, or `pos - vel*dt` on the first frame) to its current post-`World.Step(dt)` pose across all `Substeps`. For rigid attachments (`Compliance == 0`), distance and tether constraints use the attached body's generalized inverse mass (`InvMass + InvInertia * |r x n|^2`) directly and accumulate linear and angular positional corrections (`corrPos`, `corrAng`), which are written back to `Pos`/`Rot` and added to `Vel`/`AngVel` as `corr / dt` at the end of the frame.
- **Unilateral long-range tethers in `AddRope`**: When `Compliance <= 0`, `AddRope` adds `MaxOnly` tether constraints from the pinned root to downstream vertices at their exact rest arc length. By the triangle inequality, straight-line distance never exceeds arc length even when a rope wraps around obstacles, guaranteeing `< 1%` stretch even under 20:1+ crate-to-rope mass ratios.
- **World collisions and layer filtering**: Particles resolve collisions inside every solver iteration against 2D `World.Bodies`, `RigidWorld.Bodies` (including rotated boxes), and `TileGrid` (`TileSolid` and `TileOneWay`), and 3D `World.Bodies()`, `TriMeshCollider`, and `HeightfieldCollider`, skipping the rigid body a particle is attached to and honoring `LayerMask` filtering throughout.
- **Release-safe debug drawing**: `DrawSoftWorld` is split by `//go:build !release` and `//go:build release` build tags matching the existing `debugdraw*.go` pattern, so release builds compile it to a zero-cost no-op.

## Alternatives considered

### Embed soft bodies directly inside `RigidWorld.Step` and `physics3d.World.Step`

Running XPBD substeps inside the rigid-body world's own `Step` loop would let rigid bodies and particles share a single unified substep timeline without pose history interpolation. It was rejected because it would couple soft-body state into the existing `RigidWorld` and `World` structs, conflict with concurrent rigid-body shape and joint work, and force callers who only use kinematic `World` + `TileGrid` in 2D to allocate a `RigidWorld`.

### Solve rigid attachments purely via post-step velocity impulses without positional coupling

Applying only spring or velocity impulses back onto attached rigid bodies avoids mutating rigid-body poses inside `SoftWorld.Step`. It was rejected because at 60 Hz a heavy swinging crate advances one full frame under gravity before `SoftWorld.Step` runs, creating 2-5% apparent rope stretch and substep velocity spikes on the attached particle; positional XPBD with interpolated substep poses keeps stretch well under 1% and preserves pendulum energy cleanly.

### Use classic Müller 2007 `arccos(n1 . n2)` dihedral bending in 3D cloth

The `arccos` formulation is shorter to write, but its gradient divides by `sqrt(1 - (n1 . n2)^2)`, which is singular at `0` and `pi`—precisely the rest state of flat cloth. The signed `atan2` dihedral formulation with Bridson edge-altitude gradients has no singularity on flat cloth.

## Consequences

- **Benefits**: Games get pure-Go, zero-cgo, bit-deterministic 2D and 3D ropes, cloth, and soft bodies with two-way rigid coupling, world/tile/mesh/heightfield collisions, and layer filtering across all NSGo targets (macOS, Windows, Linux, Android, Web/WASM) while keeping `game/physics` strictly free of Experimental imports.
- **Costs**: Callers using two-way rigid coupling must call `SoftWorld.Step(dt)` after `RigidWorld.Step(dt)` / `World.Step(dt)` on the fixed loop so attached rigid poses stay synchronized, and `SoftWorld` keeps a small per-attached-body pose slice (`bodyHistory`) to interpolate across substeps. Particle-to-particle self-collision is not included in this initial solver.
