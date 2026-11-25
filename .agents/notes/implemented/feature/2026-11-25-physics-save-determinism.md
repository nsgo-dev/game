# Agent Note: Physics world save/restore snapshots and cross-architecture determinism

Status: implemented

## Problem

Rollback netcode (`packages/game/net`), deterministic replays, and mid-run save/load require two guarantees from the 2D, 3D, and vehicle physics packages that did not previously exist:

1. **Bit-identical state save and restore**: `physics.World`, `physics.RigidWorld`, `physics3d.World`, and `vehicle.Vehicle` hold private solver state—trigger enter/stay/exit maps, pending force/torque accumulators, sleep timers, warm-start contact impulse caches, active contact/trigger pair sets, previous interpolation poses, unwrapped hinge/6-DOF joint angles, and wheel compression/spin state. Without snapshotting every private field, restoring at step 100 and re-stepping to 200 diverges from an uninterrupted run.
2. **Bit-identical trajectories across CPU architectures (amd64 vs arm64)**: Go's language specification allows arm64 compilation to contract `a*b + c` into fused multiply-add (`FMADD`/`FMSUB`/`FNMADD`/`FNMSUB`) instructions with a single rounding, whereas amd64 performs two separate roundings. In addition, Go's standard library transcendentals (`math.Sin`, `math.Cos`, `math.Sincos`, `math.Tan`, `math.Atan2`, `math.Acos`, `math.Hypot`) emit `FMADDD`/`FMSUBD` on arm64. A 1-ULP difference between architectures quickly crosses contact or sleeping thresholds and causes multiplayer desyncs between arm64 and amd64 peers.

## Decision

`physics.World`, `physics.RigidWorld`, `physics3d.World`, and `vehicle.Vehicle` expose versioned, plain-text JSON-serializable `Save`, `Restore`, and `Checksum` APIs, and every stepped simulation path in `physics`, `physics3d`, and `vehicle` uses FMA-free arithmetic backed by a shared internal package `packages/game/internal/detmath`.

## Snapshot schema and pointer preservation

- Every snapshot type carries an explicit `Version int` field (`SnapshotVersion = 1`) and rejects unknown versions or out-of-range entity indices on `Restore`.
- Snapshots are plain Go structs with JSON tags (no `encoding/gob` or custom binary layouts) so they can be inspected, diffed, persisted as JSON, or passed directly through `net.SimState`.
- Map-backed private solver state (such as `physics.World`'s per-trigger active body sets, `physics3d.World`'s `warmStart`, `activeContacts`, and `ignoredPairs`) is serialized in canonical sorted key order so `Save()` and `Checksum()` are completely independent of Go map iteration order.
- `Checksum()` computes a deterministic zero-allocation FNV-1a 64-bit digest over all public and private simulation state, normalizing `-0.0` to `+0.0` and canonicalizing NaN bit patterns (`detmath.Hasher`).
- `Restore` mutates existing `*Body`, `*RigidBody`, `*Trigger`, `Joint`, `Joint3D`, and collider instances in place whenever counts and kinds match. This preserves gameplay references, `UserData`, and callbacks (`OnEnter`/`OnStay`/`OnExit`, `OnContact`/`OnTrigger`/`OnJointBreak`) across rollback ticks without re-wiring entity pointers. When counts or kinds differ (for example restoring into a freshly constructed empty world), `Restore` allocates the required instances cleanly.
- `physics3d.World` preserves existing `TriMeshCollider` BVHs when restored vertex/index buffers match in place, and re-attaches/detaches `VoxelGrid` instances by ID.
- `packages/game/physics` remains a **Stable** package (`TestStabilityTable`): it imports only `packages/game/internal/detmath` (internal packages are permitted for Stable packages) and never imports any Experimental package.

## Cross-architecture FMA elimination and `internal/detmath`

- `packages/game/internal/detmath` provides:
  - Explicit-conversion helpers (`MulAdd32`, `MulSub32`, `MulAdd64`, `MulSub64`) using `float32(a*b) + c` / `float64(a*b) + c`, which the Go language specification forbids contracting into FMA.
  - Pure-Go, FMA-free `Sin`, `Cos`, `Sincos`, `Tan`, `Atan`, `Atan2`, `Acos`, and `Hypot` using Cody-Waite range reduction and FDLIBM/Cephes minimax polynomials with explicit `float64(...)` conversions on every product, eliminating arm64 FMA instructions inside Go's `math` transcendentals.
  - Zero-allocation `Hasher` for canonical FNV-1a 64-bit state digests.
- All stepped simulation files in `physics` (`area.go`, `character.go`, `joints.go`, `rigid.go`, `shape.go`, `tile.go`, `world.go`, `xpbd.go`; the last contractions in `xpbd.go`, `character.go`, `tile.go` and `world.go` went in [a later fix](../bug-fix/2026-11-25-physics-fma-xpbd-character-tiles.md), which also added an arm64 disassembly test), `physics3d` (`area.go`, `character.go`, `joints.go`, `joints_6dof.go`, `mesh_heightfield.go`, `obb.go`, `ragdoll.go`, `raycast.go`, `shape.go`, `vec3.go`, `voxel.go`, `world.go`), and `vehicle` (`vehicle.go`, `ghost.go`) wrap multiply-add/sub expressions in explicit conversions and call `detmath` transcendentals instead of `math` transcendentals.
- Verification with `go tool objdump` on compiled arm64 test binaries confirms **zero** `FMADD`, `FMSUB`, `FNMADD`, or `FNMSUB` instructions across `internal/detmath`, all stepped simulation files in `physics`, `physics3d`, and `vehicle`, and the golden/rollback test drivers.

## Alternatives considered

### Why not fixed-point arithmetic?

Rewriting the 2D/3D rigid-body solvers, GJK/SAT narrowphase, 7 joint types, and Pacejka tire model in fixed-point integers would break the existing `float32`/`float64` public APIs (including Stable `game/physics`), introduce overflow risks across large scenes, and degrade readability. Eliminating arm64 FMA contraction preserves the existing IEEE-754 public APIs while guaranteeing bit-identical rounding between amd64 and arm64.

### Why not rely on `math` transcendentals and only fix `a*b + c` in physics code?

Disassembling compiled arm64 Go binaries with `go tool objdump` showed that Go's standard library `math.Sin`, `math.Cos`, `math.Sincos`, `math.Tan`, `math.Atan2`, `math.Acos`, and `math.Hypot` emit `FMADDD`/`FMSUBD` on arm64 (for example inside `math.satan` and polynomial evaluation). Any scene involving rotations, hinge/6-DOF joint angles, capsule/OBB orientation, or vehicle slip angles (`Atan2`) would diverge between arm64 and amd64 without FMA-free transcendentals in `internal/detmath`.

### Why not allocate brand-new structs on every `Restore`?

Allocating new `*Body`/`*RigidBody`/`*Trigger` pointers on `Restore` would invalidate every pointer held by game code, break trigger and contact callbacks (`OnEnter`, `OnContact`, `OnJointBreak`), and increase GC pressure during frequent rollback re-simulations. Mutating existing instances in place when shapes/kinds match keeps rollback zero-friction for callers while still supporting restore into an empty world for cold save-file loading.

## Consequences

### Benefits

- `physics.World`, `physics.RigidWorld`, `physics3d.World`, and `vehicle.Vehicle` support bit-identical save/restore (`step 100 -> step 200 == restore@100 -> step 200`), plain-text JSON snapshots, and rollback netcode via `net.SimState`.
- Golden trajectory checksum tests in `physics`, `physics3d`, `vehicle`, and `net` enforce cross-architecture determinism on every CI run across both `depot-ubuntu-24.04` (amd64) and `depot-macos-15` (arm64).
- Existing entity pointers and callbacks survive rollback restores without re-registration.

### Costs

- Future physics math changes in `physics`, `physics3d`, and `vehicle` must keep multiply-add products wrapped in explicit `float32(...)`/`float64(...)` conversions and use `internal/detmath` instead of `math` transcendentals; forgetting to do so will be caught by the cross-architecture golden hash tests in CI.
- Disabling FMA on arm64 trades a small amount of fused-instruction throughput in inner solver loops for cross-architecture bit reproducibility.
- `Snapshot` structs must be updated whenever new private simulation fields are added to bodies, joints, or worlds.
