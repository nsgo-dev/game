# Agent Note: Exact 3D TOI CCD and Bullet-vs-Dynamic Sweeps in 2D and 3D Physics

Status: implemented

## Problem

Continuous collision detection had two tunneling gaps (#1712):

1. In `pkg/physics` (`RigidWorld`), `sweepFastBodies` only swept fast bodies and `RigidBody.Bullet` bodies against static bodies and tiles, so bullets passed straight through thin dynamic and kinematic bodies.
2. In `pkg/physics3d` (`World`), `sweepFastBody` sampled evenly spaced positions capped at `maxSweepSteps = 32` while keeping end-of-step rotation fixed (`b.Rot`), only swept against static, kinematic, and sleeping bodies, and did not remove approach velocity on impact. A 3D body moving more than 32 times its thickness per step (e.g., a 10 cm sphere at 400–2000 m/s) tunneled through thin static walls in the gaps between samples, a fast-spinning plank passed through pillars, and there was no `Body.Bullet` mode to stop projectiles against awake dynamic bodies.

## Decision

1. **2D Bullet-vs-Dynamic/Kinematic CCD (`pkg/physics/rigid_ccd.go`, `pkg/physics/rigid.go`, `pkg/physics/rigid_island.go`)**:
   - `RigidWorld.Step` records `starts[i]` for every non-nil body (including sleeping bodies) before waking and substepping.
   - `sweepFastBodies` keeps static bodies and tiles as the only candidates for non-bullet fast bodies, and additionally sweeps `RigidBody.Bullet` bodies against all non-sensor dynamic and kinematic bodies using two-body conservative advancement (`sweepHullAgainstMoving`) that interpolates both bodies' poses across `[0, 1]`.
   - On impact with a dynamic target, `sweepFastBodies` wakes a sleeping target, rewinds a moved dynamic target to the same impact time `best.t` so neither body tunnels past the other, resolves two-body normal impulses with restitution at all shared impact points (`resolveImpact`), and emits `ContactBegin`. On impact with a moving kinematic target approaching the bullet, the bullet rides ahead of the target's remaining displacement after velocity resolution.
   - All candidate and mover lists reuse `rigidScratch` slices for zero allocations per step and FMA-free arithmetic.

2. **3D GJK Conservative Advancement & `Body.Bullet` (`pkg/physics3d/ccd.go`, `pkg/physics3d/world.go`, `pkg/physics3d/broadphase.go`, `pkg/physics3d/snapshot.go`)**:
   - `bpProxy` records start-of-step `prevPos` and `prevRot` before integration.
   - `sweepFastBody` replaces 32-step position sampling with uncapped GJK conservative advancement (`sweepConvexVsStatic3D` and `sweepConvexVsMoving3D`) interpolating both center-of-mass translation and orientation (`IntegrateQuat` / `Slerp`), bounded by the tight angular approach rate `|n × ω| * reach`. It supports all 6 shape kinds (`ShapeSphere`, `ShapeBox`, `ShapeCapsule`, `ShapeConvexHull`, `ShapeCylinder`, and `ShapeCompound` excluding trigger children), plus BVH ray-pruned `TriMeshCollider`, `HeightfieldCollider`, and chunked `VoxelCollider` sweeps with occluded-face filtering.
   - Static support planes (`ccdSupport3D` / `behindSupport3D`) suppress coplanar static seams (tiled boxes, triangle meshes, heightfields, voxels) without hiding dynamic targets. Start-of-step touching/kissing contacts evaluate the exact contact-point displacement `(dCom + ω × r) · n` and recover kissing normals via EPA so bodies sliding on floors or tipping on legs are not falsely arrested.
   - `Body.Bullet` is added to `physics3d.Body`, serialized in `BodySnapshot` under `SnapshotVersion = 4` (with backward-compatible v1–v3 restore defaulting `Bullet` to `false`), and hashed into `World.Checksum` via a tagged bit (`5`) only when set so existing golden checksums for non-bullet worlds remain unchanged.
   - Candidates are sorted by ID and hits tie-break deterministically by `(t, kind, targetID, childA, childB, subIdx, subY, subZ)` so AABB-tree broadphase stays bit-identical to brute-force mode.
   - Impact resolution (`resolveImpact3D`) places the body (and a moved dynamic target) at `best.t` (already separated by `targetSep`), resolves multi-point normal impulses with restitution via `solveBlockLCP`, wakes sleeping target islands, and records body-body impacts in `currentPairs` for `ContactEnter`. All scratch buffers live on `World.ccd` for zero steady-state allocations.

## Alternatives considered

- **Increasing `maxSweepSteps` in 3D instead of conservative advancement**: Simple to change, but any fixed cap still fails above that speed (e.g., 2000 m/s is >300× a 10 cm sphere's thickness), and raising the cap to hundreds of samples wastes O(dist/ext) work per step and still misses fast rotation. Conservative advancement converges in 2 iterations for linear motion at any speed and handles rotation directly.
- **Sweeping all fast non-bullet bodies against dynamic bodies**: Rejected because issue #1712 explicitly requires non-bullet fast bodies to preserve existing candidate sets (static/kinematic/sleeping in 3D, static/tiles in 2D) so existing dynamics and costs for ordinary fast debris stay unchanged while `Bullet` opts into dynamic-vs-dynamic TOI.
- **Using `ShapeCast` alone for 3D CCD**: `World.ShapeCast` only supports linear translation of Sphere/Box/Capsule (no rotation, no ConvexHull/Cylinder, and stops at `T == 0` on existing floor contacts). GJK conservative advancement reuses `gjk.go` across all convex shapes, triangles, and voxels uniformly with rotation interpolation and start-contact filtering.

## Consequences

- **Benefits**: 3D bodies moving at 400–5000 m/s stop cleanly at 2 cm static walls without a sample cap; spinning 2 m planks at 30 rad/s cannot sweep past thin pillars; 2D and 3D `Bullet` bodies stop at stationary, rotated, oblique, and moving thin dynamic walls with momentum transfer and contact events; steady-state CCD runs at ~1.7–1.9 µs/op (3D) and ~3.2 µs/op (2D) with 0 B/op and 0 allocs/op; `TestGoldenTrajectory2D`, `TestGoldenTrajectory3D`, `TestBroadphaseBitIdenticalToBruteForce`, and `TestNoFusedMultiplyAddOnArm64` remain green.
- **Costs**: `physics3d.SnapshotVersion` bumps from 3 to 4 (older snapshots v1–v3 remain readable). `Body.Bullet` adds an O(awake dynamic bodies) swept-AABB scan per bullet in 3D (avoiding stale pre-sync dynamic AABB tree leaves); worlds with thousands of simultaneous bullets may later warrant syncing swept AABBs into the dynamic tree.
