# Agent Note: Direct voxel terrain collision for rigid bodies and characters

Status: implemented

## Problem

`packages/game/voxel` (introduced in [3D voxel chunks, greedy meshing, and YAML world persistence](./2026-11-24-voxel-chunks-and-meshing.md) and extended in [Voxel sky and block light propagation](./2026-11-25-voxel-sky-and-block-lighting.md)) can mesh and raycast a chunked block world, and `packages/game/physics3d` supports static triangle meshes and heightfields, but nothing can stand on, collide with, or walk across voxel terrain directly. Rebuilding a `TriMeshCollider` from chunk meshes after every `SetBlock` stalls frames, introduces BVH rebuild churn, and cannot easily wake sleeping crates when a support block is mined. Voxel games also frequently want a lightweight axis-separated AABB move-and-slide controller without instantiating a full rigid-body world.

## Decision

Keep `physics3d` free of a dependency on `voxel` (preserving `physics3d`'s zero-dependency standard-library design) by introducing an interface-backed `VoxelCollider` in `physics3d`, wiring a thin live adapter in `voxel`, and providing an axis-separated AABB mover directly in `voxel`:

- **Grid interfaces and `physics3d.VoxelCollider`**: `physics3d` defines `VoxelGrid` (`VoxelSize() float32`, `IsSolid(x, y, z int) bool`) plus two optional extension interfaces: `VoxelBoundsGrid` (for O(1) out-of-bounds ray rejection and long-ray clipping) and `VoxelEditNotifier` (`WatchVoxelEdits(onBlock, onReset) cancel` for immediate sleeping-body wakeup). `VoxelCollider` answers `QueryBlocks`, `TestShape`, `CollectContacts`, `Raycast` (3D Amanatides & Woo DDA), and `Sweep` (exact analytical Minkowski-sum swept AABB for axis-aligned boxes; sub-stepped binary-search TOI refinement for spheres, capsules, and oriented boxes) directly against solid blocks in deterministic ascending `(y, z, x)` order with zero triangle allocation.
- **Internal-face seam filtering**: Every solid voxel checks its 6 axis-aligned neighbors (`solidNeighborMask`). Axis-aligned box tests (`testAABBvsVoxel`) consider only exposed faces actually straddled by the box (evaluating Y before X/Z ties), and general shapes clamp occluded normal components (`clampNormalOccluded`) or select the shallowest straddled exposed face (`bestExposedFaceContact`), with a buried-shape fallback only when no exposed voxel contact exists. `sweepAABB` rejects hits whose face normal is occluded by an adjacent solid block. This eliminates phantom horizontal seam normals on flat floors and walls so sliding crates, spheres, capsules, and `CharacterController` never snag.
- **`physics3d.World` and `CharacterController` integration**: `World.AddVoxelCollider`, `RemoveVoxelCollider`, and `VoxelColliders` integrate `VoxelCollider` into narrowphase (`Step`), positional correction (`solverContact.current` re-testing the exact voxel coordinate), shock propagation, continuous collision detection (`sweepOverlaps`), layer-filtered `Raycast`/`RaycastFirst`/`QueryOverlap`, and `CharacterController` (`raycastFirst`, `sweepCapsule`, `deepestOverlapBody`), enabling single-block step-up (`StepHeight >= VoxelSize`) and clean jumping/landing.
- **Immediate edit reflection and dual-path wakeup**: `voxel.NewCollider(w)` / `w.Collider()` wraps `*voxel.World` in `worldVoxelGrid` implementing `VoxelGrid`, `VoxelBoundsGrid`, and `VoxelEditNotifier`. `voxel.World.SetBlock` notifies registered watchers whenever `IsSolid(oldID) != IsSolid(id)` (and `Clear` notifies `onReset`), triggering `VoxelCollider.NotifyBlockChanged` -> `World.WakeInBox` which transitively wakes sleeping bodies stacked via `activeContacts` without disturbing distant sleeping bodies. For custom `VoxelGrid` implementations that do not implement `VoxelEditNotifier`, `World.wakeForVoxels` runs at the start of `Step` to wake sleeping bodies whose voxel support disappeared or that became penetrated by a newly placed block.
- **Axis-separated AABB move-and-slide in `voxel`**: `voxel.MoveAndSlideAABB` and `voxel.CharacterMover` resolve movement in `float64` along `Y`, then `X`, then `Z` using exact per-axis slab sweeps against `World.isSolidXYZ`, with automatic single-block step-up (verifying ceiling headroom, greater horizontal progress, and valid landing height) while grounded.

## Alternatives considered

- **Converting greedy-meshed `ChunkMesh` buffers into `physics3d.TriMeshCollider` per chunk** — Reuses existing `TriMeshCollider` and BVH code with no new collider type in `physics3d`, but forces mesh + BVH rebuilds on every `SetBlock`, duplicates memory for triangle vertices and BVH nodes, couples collision updates to rendering remeshes, and still requires seam-normal filtering across chunk boundaries.
- **Having `physics3d` import `packages/game/voxel` directly** — Avoids the `VoxelGrid` interface indirection, but violates `physics3d`'s architectural rule (zero external package dependencies) and prevents non-`voxel` grid worlds (such as custom procedural grids or tile volumes) from using `VoxelCollider`.
- **Implementing `CharacterMover` solely on top of `physics3d.CharacterController` instead of adding axis-separated AABB move-and-slide** — Avoids maintaining a second character movement path, but voxel games often expect crisp square AABB block-edge footing and deterministic axis-separated sliding (Minecraft-style) without allocating a `physics3d.World` or dealing with capsule rounding on block corners.

## Consequences

- **Benefits**:
  - `World.SetBlock` and `World.Clear` take effect on the very next overlap, raycast, sweep, body step, or character move with zero meshing and zero rebuild cost.
  - Resting crates and stacked dynamic bodies sleep cleanly on voxel floors, wake immediately (locally, without waking distant bodies) when their supporting block is mined, and fall deterministically.
  - Both `physics3d.CharacterController` (capsule) and `voxel.CharacterMover` / `MoveAndSlideAABB` (axis-separated AABB) walk across chunk borders, pass through `NonSolid` blocks (e.g., water), step up single-block ledges, respect low ceilings, and jump/land deterministically.
  - Bit-for-bit identical trajectories across repeated runs for rigid bodies, `CharacterController`, and `CharacterMover`.
- **Costs and known limits**:
  - `VoxelCollider` treats every solid voxel as a full axis-aligned cube of edge `VoxelSize`; half-slabs, stairs, or partial-block shapes would require an optional per-voxel shape hook in a future extension.
  - `VoxelCollider` contacts are not cached in `World.cache` for warm-starting (matching `TriMeshCollider` and `HeightfieldCollider`), relying on the iterative solver and shock propagation for resting stack stability.
  - `voxel.World` retains a slice of edit watchers while colliders are attached; callers that dynamically discard a `VoxelCollider` should call `World.RemoveVoxelCollider` so the watcher is unregistered.

## Verification

Run from `/Users/e-hu/worktrees/agy-game-815`:

- `gofmt -l packages/game/voxel packages/game/physics3d` — confirms formatting is clean.
- `(cd packages/game && env -u GOROOT go tool nsgo test ./physics3d/...)` — runs unit tests for voxel overlap and seam filtering, DDA raycast, analytical AABB and capsule/sphere sweep, `Origin` offsets, CCD anti-tunneling, custom-grid support-removal wakeup, `RemoveVoxelCollider` wakeup, `CharacterController` 1-block step-up and jumping, and bit-exact determinism alongside all existing `physics3d` tests.
- `(cd packages/game && env -u GOROOT go tool nsgo test ./voxel/...)` — runs integration tests for `World.Collider()` immediate `SetBlock` reflection and `NonSolid` water handling, stacked crate rest/sleep/local-wakeup/fall on `SetBlock` and `World.Clear`, `CharacterController` multi-ledge step-up across negative/positive chunks, `CharacterMover` / `MoveAndSlideAABB` walking, ceiling clamping, diagonal wall sliding, half-voxel scaling, low-ceiling step-up rejection, and bit-exact determinism alongside all existing `voxel` tests.
- `(cd packages/game && env -u GOROOT go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid')` — confirms package stability and skill contract checks pass.
- `(cd packages/game && env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web)` — confirms cross-platform compilation and vetting across all five targets.
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes` — validates the changelog fragment and agent note tree/format rules.
