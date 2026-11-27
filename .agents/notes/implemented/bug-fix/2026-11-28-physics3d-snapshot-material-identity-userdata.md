# Agent Note: Shared PhysicsMaterial identity and Body.UserData in 3D snapshots

Status: implemented

## Problem

`physics3d.World.Restore` previously violated two guarantees documented in `docs/en/physics3d.md` and `docs/zh/physics3d.md` (and introduced alongside [physics world save/restore snapshots](../feature/2026-11-25-physics-save-determinism.md)):

1. **Shared `*PhysicsMaterial` splitting**: `snapshotBody3D` and `applyBody3DSnapshot` copied `Body.Material` inline into a fresh `&PhysicsMaterial` allocation per body on every `Restore`, even when restoring bodies in place. After a rollback, `body.Material == sharedMat` became false and every referencing body held an isolated copy, so runtime edits to a shared surface material silently stopped reaching restored bodies.
2. **Missing `Body.UserData`**: `docs/en/physics3d.md` promised that `Restore` preserves `UserData` on in-place bodies, matching 2D `physics.Body` and `physics.RigidBody`, but `physics3d.Body` had no `UserData` field.

## Decision

`pkg/physics3d` preserves shared `*PhysicsMaterial` identity across `Save` and `Restore` via a snapshot material table (`SnapshotVersion = 2`) and adds `Body.UserData any`:

- **Snapshot material table (`SnapshotVersion = 2`)**:
  - `WorldSnapshot` adds `Materials []PhysicsMaterial` (`json:"materials,omitempty"`) and `BodySnapshot` adds `MaterialIndex *int` (`json:"materialIndex,omitempty"`).
  - `World.Save` deduplicates distinct non-nil `*PhysicsMaterial` pointers across `w.bodies` in deterministic first-seen body order into `snap.Materials` and records each body's zero-based `MaterialIndex`. Distinct pointers with equal field values remain distinct entries.
  - `SnapshotVersion` bumps from 1 to 2 while `oldestSnapshotVersion` stays 1, so older builds refuse v2 snapshots instead of silently dropping `Materials`/`MaterialIndex`, and current builds still restore legacy v1 snapshots that store inline `BodySnapshot.Material`.
  - `World.Restore` validates every `MaterialIndex` against `len(snap.Materials)` before mutating `w`.
- **Safe pointer reuse on `Restore`**:
  - For each slot in `snap.Materials`, `Restore` checks matching live bodies (by `Body.ID`) for an unclaimed existing `*PhysicsMaterial` pointer whose current value equals `snap.Materials[idx]`. When found, every body referencing that slot receives the existing pointer—including bodies recreated after removal.
  - When no matching unclaimed live pointer exists (including restoring into a fresh `World` or when a caller-owned material diverged in value between `Save` and `Restore`), `Restore` allocates one shared `*PhysicsMaterial` for that slot so all referencing bodies stay linked without mutating caller-owned pointers.
  - Legacy v1 inline `BodySnapshot.Material` entries reuse an unclaimed live pointer on in-place restore when values match and copy otherwise.
- **`Body.UserData`**:
  - `Body` gains `UserData any`, excluded from `WorldSnapshot` and `World.Checksum`.
  - In-place `Restore` preserves `dst.UserData` for bodies matched by `ID`; newly allocated bodies (removed before restore or restored into a fresh `World`) start with `UserData == nil`.
- **Checksums unchanged**: `World.Checksum` continues hashing material values per body and excludes `UserData`, preserving all existing cross-architecture golden hashes.

## Alternatives considered

- **Mutate existing `*PhysicsMaterial` structs in place when values differ from the snapshot**: Rejected because plain-text snapshots record values and table indices, not pointer addresses. `Restore` cannot distinguish "caller edited `ice.Friction` in place" from "caller reassigned `body.Material = rubber`"; overwriting `*existing.Material` would silently corrupt shared game-level material presets such as `rubber`.
- **Only reuse `existing.Material` on value match without a snapshot material table**: Rejected because restoring into a fresh `World` (save/load or cold replay) or recreating bodies removed before rollback would still split shared materials into one private copy per body.
- **Remove `UserData` from `docs/en/physics3d.md` instead of adding the field**: Rejected because `physics.Body` and `physics.RigidBody` already expose `UserData any` for entity back-references, and adding it brings 3D parity with 2D without affecting snapshot size or checksums.

## Consequences

- **Benefits**: Shared `*PhysicsMaterial` pointers stay shared across both in-place rollbacks and fresh-world restores; editing a shared material after restore affects every referencing body; distinct materials with equal values remain distinct; `Body.UserData` matches 2D physics and survives in-place rollbacks; v1 snapshots and golden checksums stay compatible.
- **Costs**: If a caller mutates a shared `*PhysicsMaterial` in place between `Save` and `Restore`, `Restore` detaches the restored bodies onto a newly allocated shared `*PhysicsMaterial` holding the snapshotted values instead of mutating the caller's pointer back; callers needing rollback-mutated materials to stay bound to an external variable must re-read `body.Material` after `Restore` or keep material presets immutable and swap assignments.
