# Agent Note: Behavior tree Cooldown runs on a monotonic tree clock and survives Reset

Status: implemented

## Problem

`ai.Cooldown(d, child)` previously decremented a per-node `remaining` counter inside `cooldownNode.Tick` and zeroed `remaining` inside `cooldownNode.Reset`. Two defects followed directly from that design (#1586):

1. Any ancestor interruption that calls `Reset()` on a running subtree (`Guard` when its condition turns false, `Selector` preemption, `TimeLimit` timeout, `BlackboardCondition` abort) erased active cooldowns. Using the documented attack-and-wait example, leaving and re-entering range could trigger two attacks in 40 ms despite a 1.5 s cooldown.
2. When a branch was not ticked (guard false or higher-priority branch running), `cooldownNode.Tick` never ran and the cooldown froze indefinitely instead of elapsing in game time.

## Decision

Track `Cooldown` expiration as a `readyAt` timestamp against a shared monotonic `*btClock` owned by the outermost behavior tree node, preserve cooldown state across `Reset()`, and expose an explicit cooldown-clearing API:

- **Root-owned tree clock (`pkg/ai/bt.go`, `bt_ext.go`, `bt_blackboard.go`, `bt_inspect.go`, `bt_snapshot.go`, `tokens.go`, `utility.go`, `goap.go`)**:
  - Every `cooldownNode` initializes a `*btClock` (`now time.Duration`). Every built-in composite, decorator, `Named`, `BehaviorTree`, `WithAttackToken`, `UtilitySelector`, and `PlanRunner` embeds `btClockState` (`clock *btClock`, `hasParent bool`) and adopts children's clocks at construction time, unifying all descendant `Cooldown` nodes (including through custom `ChildNodes`) onto a single `*btClock` while marking adopted children `hasParent = true`.
  - Trees without any `Cooldown` descendant keep `clock == nil` and allocate nothing extra.
  - On `Tick(dt)`, only the root (`!hasParent && clock != nil`) advances `clock.now` once by `dt` (clamped at `maxBTDuration` when `dt > 0`). `cooldownNode` records `cooling = true` and `readyAt = clock.now + duration` on child `StatusSuccess` and checks `readyAt - clock.now > 0` in both `Tick` and `bbWantsPreempt`.
  - `BTInspector` does not adopt its root so callers may tick either the root `Node` or `ins.Tick(dt)` without double-advancing or suppressing clock advancement.
- **Interruption vs explicit hard reset (`pkg/ai/bt.go`, `bt_snapshot.go`)**:
  - `cooldownNode.Reset()` clears `running` and trace state and resets its child, never clearing `cooling` or `readyAt`, and `Reset()` never resets `clock.now`.
  - `ResetCooldowns(root Node)` and `(*BehaviorTree).ResetCooldowns()` walk the tree (with cycle/DAG protection, including custom `ChildNodes`) and clear active cooldown timers without interrupting running branches.
- **Snapshots (`pkg/ai/bt_snapshot.go`)**:
  - Extends [Behavior tree and finite state machine snapshots](../feature/2026-11-25-ai-bt-fsm-snapshot.md) without bumping `BTSnapshotVersion`: `captureBTNode` writes `rec.Remaining = max(0, readyAt - clock.now)` into the existing `BTNodeSnapshot.Remaining` field, and `validateBTNodeRestore` restores `cooling = rem > 0` and `readyAt = clock.now + rem`.

## Alternatives considered

- **Requiring callers to wrap trees in `ai.NewBehaviorTree` to get off-tick cooldowns** — Simpler internally because only `BehaviorTree.Tick` would advance a clock. Rejected because `docs/en/ai.md`, `BuildBehaviorTree`, and existing user code construct and tick bare `Node` roots (`ai.Selector(...)`) directly without `NewBehaviorTree`; requiring a wrapper would leave the documented example broken unless callers migrated.
- **Adding a `TickContext` parameter to `Node.Tick`** — Clean in a greenfield API, but `ai.Node` is a public Stable interface; changing `Tick(dt time.Duration) Status` would break every caller and custom `Node`.
- **Storing absolute `Clock` on `BehaviorTreeSnapshot` and `ReadyAt` on `BTNodeSnapshot`** — Directly mirrors the internal fields, but leaks ever-growing session uptime into save files (breaking determinism across trees ticked for different totals), complicates cross-node validation, and breaks restoring a snapshot into a tree whose clock is already non-zero. Encoding `Remaining = max(0, readyAt - clock.now)` preserves exact timing, keeps `BTSnapshotVersion = 1`, validates `0 <= Remaining <= duration` locally, and round-trips identically in YAML and JSON.

## Consequences

- **Benefits**:
  - Cooldowns honor their authored duration across guard interruptions, selector preemption, timeouts, blackboard aborts, and long off-tick intervals on both bare `Node` trees and `*BehaviorTree` wrappers.
  - `Tick(dt)` stays `0 allocs/op` and deterministic on `game.Loop`.
  - Snapshot save/restore preserves pending cooldowns (even while the branch is not ticked) with zero schema churn.
- **Costs and known limits**:
  - Custom composite nodes that hide a `Cooldown` child without implementing `ChildNodes` cannot have their child adopted by an ancestor built-in root; those `Cooldown` nodes fall back to advancing on their own `Tick`.
  - Callers that previously relied on `Node.Reset()` to wipe cooldowns on respawn must call `ResetCooldowns(root)` or `bt.ResetCooldowns()` explicitly.

## Verification

- `gofmt -l pkg/ai/`
- `go tool nsgo test ./pkg/ai/...`
- `go tool nsgo check -target macos_arm64,linux_x64,windows_x64 -jobs 2 ./pkg/ai`
- `(cd tools && go run ./changelog check)`
- `bun run verify-notes`
