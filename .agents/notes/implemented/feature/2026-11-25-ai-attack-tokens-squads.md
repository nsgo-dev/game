# Agent Note: Attack tokens, engagement slots and squads in game/ai

Status: implemented

## Problem

Action games, brawlers, and shooters need enemy groups to coordinate without crowding the player: only a few enemies should attack at once while the rest circle in surrounding positions without crossing paths, and squads need shared tactical state, role specialization (flanker, suppressor, rusher), and squad-level orders readable by behavior trees.

Existing `game/rts` formations (`rts.Assign`) solve directional grid positioning for player-commanded units and live in an **Experimental** package, whereas `game/ai` is **Stable** (`TestStabilityTable` forbids stable packages from importing experimental ones). Behavior trees in `game/ai` do not yet have a dedicated `Blackboard` type, and all coordination primitives must remain deterministic across runs regardless of entity iteration order.

## Decision

Implement group combat coordination in `packages/game/ai/tokens.go` and `packages/game/ai/squad.go` across three composable, zero-goroutine primitives stepped on `game.Loop`:

### 1. Batched attack token pools (`TokenPool`, `TokenManager`)

- Agents submit or refresh attack requests via `Request(agentID, priority)` during a step; `Step(dt)` resolves grants and preemptions once per step in deterministic order (descending priority, ascending `AgentID` tie-break), making outcomes independent of agent tick order.
- Within `Step(dt)`, existing cooldown timers advance **before** active hold timeouts (`MaxHoldDuration`) expire into cooldown so a newly expired token always receives its full `Cooldown` duration rather than losing `dt` on the same step.
- Preemption transfers a token directly from the weakest holder (lowest priority, breaking ties by highest `AgentID`) without entering cooldown whenever `reqPriority > holderPriority` and (when `PreemptMargin > 0`) `reqPriority - holderPriority >= PreemptMargin`.
- `Remove(agentID)` (and `TokenManager.RemoveAgent(agentID)` across all targets) releases held tokens into cooldown and cancels pending requests when an agent dies or disengages.
- `WithAttackToken` and `HasAttackToken` integrate token arbitration directly with behavior trees: while waiting or after preemption, `WithAttackToken` resets any running attack child and returns `StatusFailure` so a parent `Selector` falls back to circling.

### 2. Non-crossing concentric engagement slots (`SlotAssigner`)

- `SlotAssigner` computes world-space slots on concentric `EngagementRing`s around a moving target and solves minimum-cost bipartite matching with a reusable Hungarian solver (`hungarianScratch`).
- When `len(agents) <= totalSlots`, each slot adds `ring * ringStep` with `ringStep = min(n, m) * (maxDist + stayBias) + 1`, guaranteeing inner rings fill completely before outer rings are used while leaving ring sums invariant under slot swaps so minimum Euclidean distance prevents straight-line agent-to-slot paths from crossing. When `len(agents) > totalSlots`, the cost matrix transposes (`slots x agents`) so every slot gets the optimal agent without crossing and excess agents return `Assigned: false`.
- Optional `StayBias > 0` subtracts a world-unit bonus from an agent's previously held slot to suppress oscillation across slot boundaries as the target moves.
- Inputs are canonicalized by ascending `AgentID` before solving, guaranteeing identical assignments regardless of input slice order and zero heap allocations after warm-up (`0 allocs/op` for both `n <= m` and `n > m`).

### 3. Squad coordination (`Squad`, `SquadState`, `SquadOrder`)

- `Squad` keeps member IDs in a sorted `[]uint64` slice for deterministic iteration.
- `SquadState` provides a self-contained shared key-value store with typed helpers (`Bool`, `Int`, `Float`, `String`, `Point`, `ID`), generic `SquadStateGet[T]`, and lexicographically sorted `Keys`, serving as the squad blackboard without blocking on a separate behavior-tree blackboard feature.
- `AssignRoles(specs...)` solves maximum-weight bipartite matching via `hungarianScratch` over an `n x (numSlots + n)` matrix with 0-cost unassigned dummy columns and eligible scores normalized into `[0, 1]` (with half-scaling fallback against `float64` overflow) shifted by `bigBonus = n + 2`. This maximizes the number of filled eligible roles first and total score second, respecting `NaN`/`±Inf` ineligibility markers.
- `SquadOrder` carries `Kind`, `TargetID`, `Position`, and a monotonically increasing `Revision` (incremented by `SetOrder` and `ClearOrder`), exposed to behavior trees via `SquadHasOrder`, `SquadHasRole`, `SquadStateBool`, and `SquadOrderAction`.

## Alternatives considered

### Importing or wrapping `game/rts` formation assignment

`rts.Assign` projects units along a facing axis and ranks them front-to-back and left-to-right in `O(n log n)`. Rejected for two reasons: (1) `game/ai` is Stable and cannot import Experimental `game/rts` (`TestStabilityTable`), and (2) radial concentric rings around an omnidirectional moving target do not have a single linear facing axis; Euclidean Hungarian matching guarantees non-crossing paths across arbitrary radial positions and multiple rings while supporting `StayBias` hysteresis cleanly.

### Granting tokens immediately inside `TokenPool.Request`

Granting tokens synchronously inside `Request` would make the first caller win when priorities tie or when two agents request before preemption runs, coupling combat outcomes to entity iteration order. Deferred resolution in `Step(dt)` keeps token arbitration strictly deterministic regardless of submission order.

### Greedy role assignment in `Squad.AssignRoles`

Iterating `SquadRoleSpec` in declaration order and picking the top-scoring remaining member is simpler than bipartite matching, but fails when the top scorer for role A is the only eligible member for role B (leaving role B unfilled or yielding a suboptimal total score). Reusing the package's existing `hungarianScratch` solver achieves globally optimal role assignment with negligible code overhead.

## Consequences

- **Benefits**: `game/ai` remains Stable with zero Experimental imports; combat groups cap simultaneous attackers with priority preemption, cooldowns, and clean death/disengage handling; concentric engagement slots stay non-crossing, hysteresis-stabilized around moving targets, and zero-allocation after warm-up; squads integrate directly with behavior trees and stay deterministic across arbitrary input permutations.
- **Costs**: Hungarian matching is `O(n^2 m)`, which is negligible for typical combat encounter sizes (dozens of enemies per target or squad) but not intended for thousands of agents sharing a single target ring; when behavior trees later gain a general `Blackboard` type, `SquadState` can either alias or bridge to it without breaking callers.

## Verification

- `packages/game/ai/squad_test.go` tests 2 tokens / 6 enemies with max-2 invariant, priority preemption (with and without `PreemptMargin`, tie-breaking, `+Inf` guard), cooldowns, `MaxHoldDuration` full-cooldown expiration, death/cancel removal, `TokenManager`, submission-order determinism, `WithAttackToken`/`HasAttackToken` BT execution, geometric non-crossing proof on single/multi-ring and `n > m` slot assignments, moving-target stability and cross-run determinism, `StayBias` hysteresis, `0 allocs/op` for `SlotAssigner.Assign`, and `Squad` roster/state/optimal roles/orders/BT integration.
- Verified with `gofmt -l packages/game/ai`, `env -u GOROOT go tool nsgo test ./ai/...`, `env -u GOROOT go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid'`, `env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web`, `env -u GOROOT go run ./tools/changelog check`, and `bun run verify-notes`.
