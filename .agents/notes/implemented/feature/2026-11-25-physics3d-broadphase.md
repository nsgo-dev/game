# Agent Note: physics3d Deterministic Static/Dynamic AABB-Tree Broadphase

Status: implemented

## Problem

Every `physics3d.World.Step` checked all `O(N^2)` body pairs and rebuilt `shapes`/`bounds` slices across all bodies, and every `Raycast`, `RaycastFirst`, `QueryOverlap`, CCD sweep, and `CharacterController` query scanned every body, `TriMeshCollider`, and `HeightfieldCollider` linearly (#811). A level with 2000 static colliders, 200 dynamic props, and 1000 per-frame AI line-of-sight rays therefore scaled quadratically in body count and linearly per ray, exceeding a 60 fps budget.

## Decision

### Separate static and dynamic AABB trees (`broadphase.go`)

- `World` embeds an internal `broadphase3D` holding two deterministic, FMA-free AABB trees:
  - `staticTree` holds static non-trigger bodies, `TriMeshCollider`s (one leaf per mesh), and `HeightfieldCollider`s (one leaf per heightfield). Bulk static insertions (such as level load or `Restore`) defer tree construction and materialize a balanced top-down longest-axis median BVH on first query/step; single mid-game static edits update incrementally and only trigger a top-down rebuild once edits exceed 25% of proxies (and at least 64 edits).
  - `dynamicTree` holds dynamic, kinematic, and trigger bodies (`!b.Static || b.IsTrigger`) with a `0.1` fat-AABB margin, `2.0x` predictive per-step displacement expansion, SAH insertion, and AVL height-bounded Box2D v3–style SAH rotations. Placing static triggers in `dynamicTree` ensures static-trigger vs. static-body pairs are still discovered without ever testing `staticTree` against `staticTree`.
- `aabbBoundsFromBox` expands tight bounds by a scale-aware epsilon (`max(1e-4, 1e-5*maxCoord)`) so float32 rounding in `Center ± HalfExtents` or slab ray tests can never cause a false negative relative to `aabbOverlap` or narrow-phase shape raycasts.
- All 5 shape kinds (`Box` including oriented boxes, `Sphere`, `Capsule`, `ConvexHull`, `Cylinder`) use `Body.WorldShape()` and `Shape.Bounds()`, cached per proxy with fast per-kind `shapesEqual` checks so unchanged static bodies never recompute transforms or touch the trees.

### Deterministic ordering and query integration (`world.go`, `layers_triggers.go`, `ccd.go`, `character.go`, `area.go`, `voxel.go`, `mesh_heightfield.go`, `snapshot.go`)

- `findBodyPairs` emits unique `(i, j)` pairs with `i < j` sorted in strictly ascending insertion order, matching the brute-force double loop.
- `queryStaticMeshesAndHeightfields` sorts overlapping meshes and heightfields into ascending insertion order.
- `RaycastFirstLayer`, `RaycastLayer`, `OverlapLayer`, `CharacterController.raycastFirst`, `CharacterController.sweepCapsule`, and `CharacterController.deepestOverlapBody` query the trees front-to-back / by AABB and preserve exact brute-force tie-breaking on `(T, kind, ID)` or `(Depth, kind, insertion order)`. Narrow-phase ray tests always receive the original `Ray.MaxDist` so `Hit.T` stays bit-identical while tree traversal prunes farther nodes using `best.Distance + eps`.
- `sweepFastBody` queries the union swept AABB once per fast body instead of rescanning the world on each CCD substep, and skips bodies with zero blocking candidates.
- `World.Step` iterates `dynamicList` for integration, CCD, static-collider narrowphase, area trigger collection, and sleeping, and reuses per-step scratch buffers (`dynPrevPos`, `pairBuf`, `contactsBuf`, `shockOrderBuf`, `shockHeightsBuf`, `curKeysBuf`, `exitKeysBuf`, alternating `activeContacts` / `nextActiveContacts` maps) to achieve 0 allocations/step in steady state.
- `World.SyncBroadphase()` lets callers who mutate `Body.Pos`, `Rot`, `Shape`, `Static`, or `IsTrigger` directly refresh the trees before running queries outside `Step`. `CharacterController.Move` syncs `dynamicList` once at entry.
- `World.Restore` rebuilds the broadphase deterministically without changing `WorldSnapshot` schema or `World.Checksum()` (see [physics-save-determinism](./2026-11-25-physics-save-determinism.md)).

## Alternatives considered

### 1D or 3D Sort-and-Sweep (SAP)

Sort-and-sweep is simple and cache-friendly for body-pair overlap when objects move coherently along one axis. However, SAP does not naturally accelerate arbitrary 3D raycasts (especially vertical or diagonal AI sight rays that cross the full sweep axis) without maintaining a separate spatial acceleration structure anyway. A dual AABB tree accelerates body pairs, AABB overlaps, CCD sweeps, character queries, and front-to-back pruned raycasts with one mechanism.

### Single combined dynamic AABB tree

Putting static and dynamic bodies in one tree avoids tree-migration code when `Body.Static` changes, but forces every dynamic-body step to traverse through thousands of static nodes mixed into the same hierarchy and risks rotating static level geometry whenever dynamic bodies move. Separate static and dynamic trees keep static geometry in a tight top-down BVH that never churns and completely eliminates static-vs-static pair work.

### Scanning `dynamicList` on every `RaycastFirst` / `OverlapLayer` call

Auto-syncing dynamic bodies inside every raycast would pick up un-stepped direct `Body.Pos` mutations without requiring `World.SyncBroadphase()`, but scanning hundreds of dynamic bodies per raycast turns 1000 raycasts/frame back into `O(R * N_dyn)` work. Syncing automatically on `AddBody`/`RemoveBody`/`Step`/`Restore` (plus once at the start of `CharacterController.Move`) and providing `World.SyncBroadphase()` for mid-frame direct mutations keeps raycasts `O(log N)` with zero allocations.

## Consequences

- **Bit-identical parity**: `TestBroadphaseBitIdenticalToBruteForce`, `TestBroadphaseQueriesBitIdenticalToBruteForce`, and `TestBroadphaseSaveRestoreBitIdentical` run broadphase and brute-force worlds side by side across all 5 shapes, oriented boxes, joints, triggers/areas, CCD, triMeshes, heightfields, voxels, `CharacterController`, and JSON snapshots, asserting bit-identical `Checksum()`, `ContactEvents()`, raycast hits (including exact distance ties), overlap hits, and trajectories.
- **Cross-architecture determinism**: all floating-point arithmetic in `broadphase.go` avoids arm64 FMA contraction (verified via `go tool objdump`).
- **Mutation contract**: callers that directly mutate `Body.Pos`/`Rot`/`Shape`/`Static`/`IsTrigger` and immediately invoke `World.Raycast*` or `World.Overlap*` before calling `World.Step` must call `World.SyncBroadphase()` first; `World.Step` always reconciles all direct mutations automatically.
- **Performance**: `BenchmarkBroadphaseStep2000Static200Dynamic`, `BenchmarkBroadphase1000Raycasts`, `BenchmarkBroadphaseStep1000Bodies`, and `BenchmarkBroadphaseCharacterMove2000Static` verify 0 allocs/op for steady-state stepping and raycasting and substantial speedup over brute force.
