# Agent Note: Identity-only handle matching in 2D physics snapshots

Status: implemented

## Problem

`World.Restore` and `RigidWorld.Restore` in `pkg/physics/snapshot.go` previously matched snapshot bodies and triggers to live pointers by non-zero `ID` and then fell back to slice index (`w.Bodies[i]`, `w.Triggers[i]`), and matched joints solely by slice index (`w.Joints[i]`). Because constructors such as `NewBoxBody` and `NewRigidBox` leave `ID == 0` and removing an element shifts later entries left, any `Remove`/`RemoveBody`/`RemoveTrigger`/`RemoveJoint` between `Save` and `Restore` silently overwrote surviving pointers with another entity's physics state while keeping their original `UserData` and callbacks (even when explicit non-zero `ID`s were set and an earlier body was removed). The world `Checksum()` still matched, hiding the corruption across rollback re-simulations (#1593).

## Decision

`World` and `RigidWorld` own monotonically increasing internal IDs (`nextBodyID`, `nextTriggerID`, and `nextJointID`) and `Restore` matches live handles strictly by identity, never by slice index:

- **Internal IDs**: `Body`, `RigidBody`, `Trigger`, and `jointReaction` (embedded in all seven built-in `Joint` types) carry an unexported `internalID uint64`. `Add`, `AddBody`, `AddTrigger`, `AddJoint`, and `Save` ensure every default-ID (`ID == 0`) body or trigger and every built-in joint has a non-zero monotonic `internalID`. `Remove*` leaves `internalID` intact so removing and re-adding the same pointer preserves its identity.
- **Snapshot format (`SnapshotVersion = 3`)**: `BodySnapshot`, `TriggerSnapshot`, `RigidBodySnapshot`, and `RigidJointSnapshot` add `InternalID uint64` (`json:"internalID,omitempty"`), populated for joints and for bodies/triggers whose user `ID == 0`. `SnapshotVersion` is bumped from 2 to 3 while `oldestSnapshotVersion` stays 1, so versions 1 and 2 still restore and older builds refuse version 3 snapshots instead of silently dropping internal IDs.
- **Strict identity matching in `Restore`**:
  - Bodies and triggers with `ID != 0` match only live handles with the same `ID`; those with `ID == 0` and `InternalID != 0` match only live handles with the same `internalID`; joints match only live joints with the same `internalID` (and matching concrete type).
  - Slice-index fallback is removed everywhere. Live pointers not present in the snapshot are detached untouched, and snapshot entries with no matching live pointer allocate fresh handles (`UserData == nil`).
  - `Restore` advances `nextBodyID`, `nextTriggerID`, and `nextJointID` to at least the highest restored `InternalID` (including detached references) so adding new handles after restoring into a fresh world never collides with restored IDs.
- **Checksums unchanged**: `internalID` is excluded from `World.Checksum` and `RigidWorld.Checksum`, keeping existing golden hashes intact and preventing rollback counter divergence between peers from altering simulation digests.

## Alternatives considered

- **Populate the exported `Body.ID` / `RigidBody.ID` / `Trigger.ID` field automatically on `Add*`**: Rejected because `Checksum()` hashes `ID`, which would break cross-architecture golden checksums for default-ID worlds (`TestGoldenTrajectorySoftWorld2D`, `TestGoldenTrajectoryTiles`) and mutate a user-owned field that callers expect to stay zero unless they set it.
- **Assign `internalID` even when user `ID != 0` and match by `internalID` first**: Rejected because loading a saved snapshot into a newly constructed level whose entities carry explicit game `ID`s (for example `player.ID = 1`) could have different creation order than the saved session; when the user provides a non-zero `ID`, that `ID` is the canonical identity.
- **Keep `SnapshotVersion` at 2**: Rejected because `pkg/physics/snapshot.go` bumps `SnapshotVersion` whenever a snapshot field is added so older builds fail loudly instead of silently ignoring `internalID` and falling back to index aliasing.

## Consequences

- **Benefits**: `Add`/`Remove` between `Save` and `Restore` never aliases surviving `*Body`, `*RigidBody`, `*Trigger`, or `Joint` handles to another entity's state, whether callers assign `ID`s or leave them at 0; existing golden checksums and version 1/2 snapshot compatibility are preserved.
- **Costs and known limits**: Restoring a legacy version 1 or 2 snapshot whose bodies/triggers have `ID == 0` (and therefore `InternalID == 0`), or any version 1/2 snapshot's joints, allocates fresh handles instead of reusing pointers by index. Callers that remove a body before rolling back to a snapshot where it existed receive a newly allocated `*Body`/`*RigidBody` with `UserData == nil` and must re-bind it (for example by `ID` or `Tag`), as documented in `docs/en/physics.md` and `docs/zh/physics.md`.
