# Agent Note: Shape Casts in 2D and 3D Physics

Status: implemented

## Problem

Both `packages/game/physics` (2D, Stable) and `packages/game/physics3d` (3D, Experimental) provided point queries, static overlaps, and segment raycasts, but no public API to sweep a collision `Shape` along a displacement vector and obtain its time of impact, contact point, and surface normal (#812). Games need shape casts for camera spring arms, thick projectiles, melee sweeps, ground/ledge probes wider than a ray, and AI clearance checks. `physics3d.CharacterController` previously ran an unexported sub-stepped capsule sweep internally, and `VoxelCollider` had a standalone `Sweep`, but neither was exposed as a general world query across all shape types and colliders.

## Decision

### 2D shape casts (`packages/game/physics/shapecast.go`)

- Add `ShapeCastHit` (`Fraction`, `Point`, `NormalX`, `NormalY`, `Body`, `RigidBody`, `Tile`, `Col`, `Row`, `TileFlags`) and `ShapeCastOptions` (`QueryMask`, `LayerMask`, `IgnoreBody`, `IgnoreRigidBody`).
- Add `ShapeCast`, `ShapeCastAll`, `ShapeCastLayer`, `ShapeCastAllLayer`, `ShapeCastWith`, and `ShapeCastAllWith` on `World`, `ShapeCast`, `ShapeCastAll`, `ShapeCastWith`, and `ShapeCastAllWith` on `RigidWorld`, plus standalone `ShapeCastShape`.
- Compute exact analytic times of impact in `float64`:
  - Box vs Box: Minkowski-expanded slab test with strict interior checks on zero-velocity axes so grazing parallel motion along walls and corridor floors never snags.
  - Circle vs Circle: quadratic ray-vs-expanded-disk sweep with strict `disc > 0` to reject grazing tangents.
  - Circle vs Box (and Box vs Circle by reversed-delta symmetry): exact rounded-rectangle Minkowski sum (four face segments plus four corner disks).
  - Oriented `RigidBody` boxes (`Angle != 0`): Circle vs OBB transforms into the OBB local frame; Box vs OBB runs exact 4-axis moving 2D SAT.
  - Tile sweeps (`World.Tiles`): honor `TileGridOrigin`, `QueryTiles`, and `QueryOneWay`, scan cells along the dominant motion axis in `O(distance / tileSize)` without visiting any cell twice, and suppress occluded cardinal tile seams at `Fraction > 0`.
- Tie-breaking is deterministic: ascending `Fraction`, then bodies in slice order before tiles in row-major `(Row, Col)` order. `ShapeCast` always matches `ShapeCastAll[0]`.

### 3D shape casts (`packages/game/physics3d/shapecast.go`, `voxel.go`)

- Reuse `RaycastHit` (`Body`, `TriMesh`, `Heightfield`, `Voxel`, `T`, `Distance`, `Point`, `Normal`) and `compareRaycastHits` (`T`, then `Body` < `TriMesh` < `Heightfield` < `Voxel`, then ascending ID) so shape casts share the exact result type and tie-breaking contract with `World.Raycast`.
- Add `ShapeCast`, `ShapeCastAll`, `ShapeCastLayer`, `ShapeCastAllLayer`, `ShapeCastWith`, and `ShapeCastAllWith` (with `ShapeCastOptions{LayerMask, IgnoreBody, IgnoreBodyID, IgnoreTriggers}`) on `World`, plus standalone `ShapeCastShape`, `TriMeshCollider.ShapeCast`, and `HeightfieldCollider.ShapeCast`. `ShapeCastLayer`/`ShapeCastAllLayer` treat explicit `mask == 0` as matching nothing (consistent with `RaycastLayer`), while zero `ShapeCastOptions.LayerMask` defaults to `LayerAll`.
- Compute exact times of impact per shape pair:
  - Analytic in `float64`: Sphere vs Sphere, Sphere vs Box/OBB (6 offset faces + 12 edge cylinders + 8 corner spheres, with Box/OBB vs Sphere by symmetry), Sphere vs Capsule (and Capsule vs Sphere), Sphere vs Triangle (2 offset faces + 3 edge cylinders + 3 vertex spheres), AABB vs AABB, OBB vs OBB (15-axis moving SAT), and Box/OBB vs Triangle (13-axis moving SAT).
  - Exact-distance conservative advancement (`<= 1e-5` tolerance, 64-iteration cap) for Capsule vs Box/OBB, Capsule vs Capsule, and Capsule vs Triangle, eliminating fixed sub-step tunneling at any sweep distance.
  - `TriMeshCollider.ShapeCast` uses front-to-back BVH pruning with Minkowski-expanded node AABBs; `VoxelCollider` combines `TestShape` initial-overlap detection with `VoxelCollider.Sweep` (bisection depth increased from 10 to 14 iterations for `< 1e-5` surface accuracy).
- Initial overlaps report `T == 0` (`Fraction == 0` in 2D) with the separation contact from `TestShapes` / `TestShape`.

## Alternatives considered

### Uniform sub-stepping + binary search for all shape pairs (like `CharacterController.sweepCapsule`)

Reusing fixed-step sampling plus bisection for every pair would keep code size smaller, but suffers from tunneling on long sweeps against thin walls or triangles unless step count scales linearly with `distance / minExtent`, which becomes slow for long camera arms or projectiles. Analytic Minkowski/SAT sweeps handle spheres, boxes, OBBs, and triangles in constant time at any sweep distance, and conservative advancement handles the three remaining capsule pairs without fixed sub-steps or tunneling.

### Separate `ShapeCastHit` struct in `physics3d`

Introducing a distinct `physics3d.ShapeCastHit` type would mirror 2D `physics.ShapeCastHit`, but `physics3d.RaycastHit` already carries every required field (`Body`, `TriMesh`, `Heightfield`, `Voxel`, `T`, `Distance`, `Point`, `Normal`) and issue #812 explicitly calls for using the same result shape as `RaycastHit`. Reusing `RaycastHit` lets camera, AI, and gameplay helpers consume raycast and shape-cast hits interchangeably.

### Adding a `Tiles` field to 2D `RigidWorld`

Adding `Tiles TileGrid` to `RigidWorld` solely for shape casts would imply that `RigidWorld.Step` resolves rigid bodies against tiles (which it does not). Keeping `RigidWorld.ShapeCast*` scoped to `RigidWorld.Bodies` avoids a misleading struct field while still supporting `ShapeCastOptions` uniformly with `World`.

## Consequences

- **Benefits**: Both 2D and 3D physics now support deterministic, exact-TOI shape casts against all supported shapes and static colliders without tunneling or corridor grazing snags, enabling camera spring arms, thick projectiles, melee sweeps, and clearance probes. `packages/game/physics` remains Stable with zero experimental imports.
- **Costs**: `shapecast.go` adds narrowphase sweep routines in both packages that must be kept in sync if new primitive kinds (such as 2D capsules or 3D convex hulls) are added later. In `physics3d`, `VoxelCollider.Sweep` for non-AABB shapes still relies on sub-stepped narrowphase + 14-step bisection rather than per-voxel conservative advancement, which bounds non-AABB voxel sweeps to `O(distance / stepLen)`.
