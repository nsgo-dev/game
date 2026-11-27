# Agent Note: Island-based sleeping and configurable sleep settings in physics3d

Status: implemented

## Problem

`physics3d.World` previously put dynamic bodies to sleep one at a time once an individual body stayed under hardcoded thresholds (`0.05 m/s`, `0.05 rad/s`, `0.4 s`), regardless of its neighbors. A sleeping body returned zero inverse mass and inertia to the solver and only woke when touched by a body faster than `wakeVelThreshold` (`0.2 m/s`). Any slower push (such as a 10 kg cube sliding at 0.1 m/s into a sleeping 1 kg cube) struck an immovable static-like wall and lost its momentum, and the bottom of a settling stack could fall asleep under upper bodies still moving. `World` also lacked `DisableSleep` and exported sleep thresholds, and `Body` lacked a per-body `AllowSleep` switch.

## Decision

`pkg/physics3d` adopts deterministic union-find island sleeping matching 2D `physics.RigidWorld` ([2D sleeping and islands](../feature/2026-11-25-physics-rigid-sleep-broadphase-warm-start-ccd.md)):

- **Pre-integration disturbance and contact wakeup (`pkg/physics3d/island.go`, `world.go`)**:
  - At the start of `World.Step`, after `bp.syncAllBodies`, `wakeDisturbed` wakes sleeping bodies affected by `DisableSleep`, `!AllowSleep`, direct velocity/pose edits, moved/removed static or kinematic partners in `activeContacts` or `joints`, `wakeForVoxels`, and active joint motors/drives (`Joint3D.Prepare`), then propagates wakeups through `wakeIslands` (union-find over solid `activeContacts` and `joints` with smaller body index as root).
  - `bp.findBodyPairs()` runs before velocity integration (since integration does not modify poses), followed by `wakeTouched`, which repeatedly wakes any sleeping body overlapping an awake dynamic body (at any speed), a moving/moved kinematic body, a moved static body, or a moving/moved active area trigger, calling `wakeIslands` after each round until fixpoint.
  - Because every touched island wakes before velocity integration and narrowphase contact generation, an awake dynamic body is never solved against a sleeping one, and newly woken islands receive full gravity integration and contacts on that same step.
  - Warm-start contact cache entries (`w.cache`) for solid sleeping/fixed pairs still present in `currentPairs` are preserved across steps so woken islands resume with their resting support impulses.
  - `ccd.go` (`sweepFastBody`) wakes a sleeping body and its island if a fast body's sweep stops against it; `WakeInBox` uses `wakeIslandsOf` so voxel wakeups propagate through both contacts and joints without disturbing unrelated sleeping islands.
- **Island-wide sleep gating (`updateSleep`)**:
  - `World` exports `DisableSleep`, `SleepLinearThreshold`, `SleepAngularThreshold`, and `TimeToSleep` along with `DefaultSleepLinearThreshold` (0.05), `DefaultSleepAngularThreshold` (0.05), and `DefaultTimeToSleep` (0.4). Non-positive, `NaN`, or infinite settings fall back to the defaults.
  - `Body` exports `AllowSleep bool` (defaulting to `true` via `NewDynamicBody`, `NewStaticBody`, and `AddBody` using an internal `allowSleepSet` flag mirroring `GravityScale`/`gravityScaleSet`, plus `Body.SetAllowSleep(bool)` for zero-initialized literals before `AddBody`).
  - After syncing dynamic poses, `updateSleep` updates each awake dynamic body's `sleepTimer` (resetting it for bodies with `!AllowSleep`, active velocity-motor joints, moving kinematic contacts, excessive speed/displacement, or `residualDepth > sleepMaxDepth`), builds deterministic union-find islands from this step's `contacts` and `joints`, and puts an island to sleep only when every dynamic body in the island has `sleepTimer >= timeToSleep`.
- **Snapshot schema (`SnapshotVersion = 3`)**:
  - `WorldSnapshot` adds `DisableSleep`, `SleepLinearThreshold`, `SleepAngularThreshold`, and `TimeToSleep`; `BodySnapshot` adds `AllowSleep` and `AllowSleepSet`.
  - `SnapshotVersion` bumps from 2 to 3 while `oldestSnapshotVersion` remains 1 (`Restore` defaults `AllowSleep` to `true` when reading v1/v2 snapshots).
  - `World.Checksum` hashes the new world and body sleep settings; golden checksums in `pkg/physics3d/snapshot_test.go`, `pkg/vehicle/determinism_test.go`, and `pkg/online/netplay/rollback_physics_test.go` are updated accordingly (while `vehicle.Checksum()` remains bit-identical).

## Alternatives considered

- **Lower `wakeVelThreshold` instead of island sleeping**: Reducing `wakeVelThreshold` to e.g. `1e-4` would still let arbitrarily slow contacts fail momentum conservation, would not prevent partial sleep of a settling stack, and would still solve newly touched sleeping bodies as immovable for one step if woken mid-narrowphase after their floor/neighbor pairs were already skipped.
- **Keep `findBodyPairs` after velocity integration and retroactively integrate newly woken bodies**: Avoids reordering broadphase ahead of integration, but duplicates velocity-integration logic and still risks missing floor/neighbor contacts whose pair index preceded the waking contact in `findBodyPairs`. Running `findBodyPairs` + `wakeTouched` before velocity integration is exact because velocity integration only mutates velocities, never poses.
- **Persist island IDs on `Body` and in `WorldSnapshot`**: Avoids rebuilding union-find on wakeup, but adds renumbering complexity on body/joint removal and extra snapshot state, whereas union-find over existing `activeContacts` and `joints` is stateless, zero-allocation (reusing scratch slices), and inherently deterministic.

## Consequences

- **Benefits**: Slow pushes at any speed (tested at 0.01, 0.1, and 1.0 m/s) wake sleeping bodies and conserve momentum within 1%; stacks and 20-cube pyramids sleep and wake strictly as whole islands without partial-sleep sagging; warm-start impulses survive sleeping; callers can tune thresholds or opt out globally (`World.DisableSleep`) or per-body (`Body.AllowSleep`).
- **Costs**: `World.Checksum()` changes (golden hashes updated in `pkg/physics3d`, `pkg/vehicle`, and `pkg/online/netplay`) and `SnapshotVersion` advances to 3 (older binaries refuse v3 snapshots, while v1 and v2 snapshots still restore cleanly). A single jittering or `AllowSleep = false` body keeps its entire connected island awake.
