# Agent Note: Behavior tree and finite state machine snapshots

Status: implemented

## Problem

Save slots, rollback netcode, and state dumps need AI agents to resume mid-action with their exact running branch, timers, cooldowns, repeat counts, and state-machine elapsed durations intact. Previously, `game/ability`, `game/inventory`, `game/quest`, and `game/random` supported `Snapshot`/`Restore`, whereas `game/ai` behavior trees restarted from their root and `game/flow` `Machine[S]` instances restarted from their initial state while re-running `Enter`.

## Decision

Add plain, versioned, YAML- and JSON-serializable snapshot structs and transactional `Snapshot`/`Restore` APIs to both `game/ai` and `game/flow` without introducing any new package dependencies or importing `go.yaml.in/yaml/v3` into `game/flow`:

- **`game/ai` behavior tree snapshots (`packages/game/ai/bt_snapshot.go`)**:
  - `SnapshotBehaviorTree(root Node) (BehaviorTreeSnapshot, error)` and `RestoreBehaviorTree(root Node, snap BehaviorTreeSnapshot) error` operate directly on any `Node` tree, while `NewBehaviorTree(root)` wraps a root `Node` with `Tick`, `Reset`, `Snapshot`, and `Restore` methods.
  - `BehaviorTreeSnapshot` (`Version: BTSnapshotVersion = 1`) stores a flat `Nodes []BTNodeSnapshot` slice in deterministic pre-order traversal, keying every node by structural path (`"0"`, `"0/0"`, `"0/1/2"`, ...) and kind (`"sequence"`, `"selector"`, `"parallel"`, `"wait"`, `"cooldown"`, ..., `"attack_token"`, `"utility_selector"`, `"custom:<kind>"`).
  - `Status` implements `MarshalText`/`UnmarshalText` (`"success"`, `"failure"`, `"running"`) so `Parallel` child statuses serialize as readable names in YAML and JSON rather than numeric enum values.
  - Optional child indices (`RunningChild *int`, `ActiveChild *int`) use pointer fields with `omitempty` so idle `-1` omits the key while active child `0` is preserved unambiguously across YAML/JSON round-trips.
  - `WeightedSelector` records `RemainingOptions []bool` rather than raw `float64` weights so restored weights come directly from `baseWeights` without float-formatting drift.
  - Custom nodes participate via optional interfaces `SnapshotNode` (`SnapshotKind`, `SnapshotState`, `RestoreState`) and `ChildNodes` (`Children() []Node`).
  - `RestoreBehaviorTree` runs in three phases: (1) strict structural path/kind/policy and built-in bounds validation with staged commit closures, (2) transactional `RestoreState` on custom `SnapshotNode`s with rollback to prior captured states if any fails, and (3) infallible commit of built-in node mutations. Ancestor pointer tracking detects cycles while allowing shared DAG nodes across sibling branches.
- **`game/flow` state machine snapshots (`packages/game/flow/machine_snapshot.go`)**:
  - `Machine[S].Snapshot() MachineSnapshot[S]` records `Version` (`MachineSnapshotVersion = 1`), `Current`, `Previous`, `TimeInState`, and `Started`.
  - `Machine[S]` records its `initial` state alongside `states` and `transitions` so `hasState` recognizes states registered via `AddState`, `NewMachine`, or transition endpoints without falsely accepting zero-value states on zero-initialized machines.
  - `Machine[S].Restore(snap)` validates version, known `Current` (and non-zero `Previous`), non-negative `TimeInState`, and unstarted invariants (`!Started` requires zero `TimeInState` and zero `Previous`) before mutating fields, and never calls `Exit` or `Enter` hooks.

## Alternatives considered

- **Storing only stateful nodes in `BehaviorTreeSnapshot` alongside a separate hash/fingerprint for shape validation** — Produces slightly smaller snapshots for large mostly-stateless trees. Rejected because a hash cannot tell callers *which* path and node kind differed when a tree changes; storing the full pre-order path+kind list keeps snapshots compact (idle nodes emit only `path` and `kind`), self-describing in diffs, and lets `RestoreBehaviorTree` name the exact mismatch path and expected vs got kind.
- **Using a `reflect`-based walker or adding `Snapshot`/`Restore` methods to the public `ai.Node` interface** — Adding methods to `ai.Node` would be a breaking change to a Stable package and force every stateless custom leaf to implement boilerplate. Keeping `ai.Node` unchanged and providing optional `SnapshotNode`/`ChildNodes` interfaces preserves full backward compatibility while supporting stateful custom leaves, decorators, and composites.
- **Having `game/flow` expose dedicated YAML `Encode`/`Decode` helpers importing `go.yaml.in/yaml/v3`** — Convenient for standalone machine files, but `game/flow` currently has zero YAML dependency and snapshots are embedded inside caller-owned save files (alongside `ability.State`, `quest.Snapshot`, and `inventory.ContainerState`). Plain structs with `json:` and `yaml:` tags match those packages and avoid pulling `yaml/v3` into `game/flow`.

## Consequences

- **Benefits**:
  - Both behavior trees (Go-constructed and `BuildBehaviorTree` YAML-authored) and `flow.Machine[S]` resume mid-run step-for-step after YAML or JSON round-tripping, without re-triggering `Enter`/`Exit` hooks or resetting timers and cooldowns.
  - Shape mismatches and corrupted snapshot values fail atomically with errors wrapping `ai.ErrInvalidBTSnapshot` or `flow.ErrInvalidSnapshot` and never panic or leave partial state behind.
  - Both `game/ai` and `game/flow` remain Stable with zero new module dependencies.
- **Costs and known limits**:
  - Trees using `RandomSelector`, `RandomSequence`, or `WeightedSelector` preserve their active mid-run selection state in `BehaviorTreeSnapshot`, but callers that want identical *subsequent* random draws after the current run finishes must also snapshot and restore the caller-owned `*random.Stream`.
  - Custom `SnapshotNode.RestoreState` implementations decoding `any` after JSON unmarshaling must handle JSON numeric decoding (`float64`) as well as YAML numeric decoding (`int`), since snapshots are format-agnostic plain Go data.
  - `Cooldown` nodes serialize their relative remaining duration (`max(0, readyAt - clock.now)`) into `BTNodeSnapshot.Remaining` and re-anchor `readyAt` to the target tree's monotonic clock on restore; see [Behavior tree Cooldown runs on a monotonic tree clock and survives Reset](../bug-fix/2026-11-28-ai-bt-cooldown-tree-clock.md).

## Verification

- `cd packages/game && env -u GOROOT go tool nsgo test ./ai/...`
- `cd packages/game && env -u GOROOT go tool nsgo test ./flow/...`
- `cd packages/game && env -u GOROOT go tool nsgo test . -- -run 'TestFormatPackagesRunConformance|TestStabilityTable|TestAgentSkillValid'`
- `cd packages/game && env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web`
- `env -u GOROOT go run ./tools/changelog check`
- `bun run verify-notes`
