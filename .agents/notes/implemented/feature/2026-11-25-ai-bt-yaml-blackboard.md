# Agent Note: YAML behavior trees and typed blackboard with observer aborts in game/ai

Status: implemented

## Problem

Behavior trees in `game/ai` (`bt.go` and `bt_ext.go`) previously existed only as Go closures, violating the repository rule that author-facing definitions are diffable plain text, and leaves had no shared typed state outside captured variables. Issue #835 calls for a typed per-agent/shared `Blackboard` with change notifications, `BlackboardCondition` decorators with observer aborts (`none`, `self`, `lower`, `both`), a strict versioned `*.bt.yaml` format supporting all built-in nodes, registered leaves, and parameterized subtrees with cycle detection, and a published JSON Schema (`bt.v1.json`)—all inside Stable `game/ai` without importing any Experimental package.

## Decision

### 1. Typed `Blackboard` with tagged union values and revision-based observation

`packages/game/ai/blackboard.go` stores entries as a tagged struct `BlackboardValue` (`BlackboardKind`: `bool`, `int`, `float`, `string`, `vec2` via `math2d.Vec2`/`core.Point`, `vec3` via `ai.Vec3`, `entity` via `uint64`) rather than boxed `any`, avoiding heap allocations on reads and comparisons while keeping `game/ai` free of Experimental imports (`math3d` is avoided by reusing `ai.Vec3`, and `math2d`/`loaderr`/`random` are all Stable).

- Child scoping (`NewChildBlackboard(parent)`) resolves reads locally first and falls back to `parent`. Local `Set*` writes shadow the parent without mutating it; `SetShared*` targets `parent` when present.
- Mutation tracking uses two complementary mechanisms:
  - Monotonic `Revision()` and `KeyRevision(key)` sum revisions up the parent chain with zero allocations. `BlackboardCondition` polls `KeyRevision(key)` so short-lived agents sharing a squad blackboard never retain callback references or leak memory when garbage-collected.
  - `Watch(key, fn) cancel` provides synchronous change notifications (propagating non-shadowed parent updates) with explicit unsubscribe cleanup.

### 2. Observer aborts integrated cleanly into `Selector` and `Sequence`

`packages/game/ai/bt_blackboard.go` adds `BlackboardCondition(bb, key, op, value, abort, child)` and an internal `btObserver` / `btSelfAbortable` contract forwarded through all built-in composites and decorators.

- `Selector` and `Sequence` compute a single `hasObserver bool` at construction time. Existing trees without `BlackboardCondition` skip the observer path completely, preserving 100% backward compatibility with `Guard`-based preemption and zero extra allocations.
- When `Selector` has a running child (`s.running >= 0`), higher-priority branches containing a `BlackboardCondition` only preempt `s.running` if their abort mode includes `AbortLower` or `AbortBoth` and a watched key's revision changed with the condition evaluating true; `AbortNone` and `AbortSelf` branches let the running lower-priority branch finish.
- When a `BlackboardCondition` child is running, `AbortSelf` and `AbortBoth` re-evaluate on key change and reset the child on failure; `AbortNone` and `AbortLower` evaluate only on entry. Used without a child (`child == nil`), `BlackboardCondition` acts as a condition leaf that can still self-abort later running siblings in an enclosing `Sequence`.
- Numeric comparisons cross-compare `int` with `float64` and `entity` with non-negative `int` so YAML integer literals (e.g. `value: 32`) match `float64` or `uint64` blackboard entries without friction.

### 3. Strict versioned `*.bt.yaml` format and `bt.v1.json` schema

`packages/game/ai/bt_yaml.go` defines `BTFormatVersion = 1`, `BTSchemaURL = "https://game.nsgo.dev/schemas/bt.v1.json"`, `BehaviorTreeDoc`, `BTBlackboardKeyDoc`, and recursive `BTNodeDoc`.

- `Default` and `Value` use `*any` with `yaml:",omitempty"` so explicit zero-valued YAML literals (`false`, `0`, `""`) are preserved on round-trip while absent fields are omitted cleanly.
- `ParseBehaviorTreeWith`, `LoadBehaviorTreeFS`, and `BuildBehaviorTree` enforce `KnownFields(true)`, single-document input, per-node-type field allowlists, valid operators/abort modes/durations, and subtree cycle detection (tracking the active path stack while allowing acyclic diamond reuse), locating all errors via `loaderr.Locate` with file, line, column, field path, and fix hint.
- Subtrees support `${param}` substitution (preserving native types for params, durations, and condition values) and transitive `keys` blackboard key remapping.
- `EncodeBehaviorTree` prefixes `BTSchemaComment` and emits deterministic 2-space YAML in fixed struct field order, verified by `internal/formattest.Run` and registered in `cmd/gameschema` (`bt.v1.json`) and `packages/game/formats_test.go`.

## Alternatives considered

### Attaching `Blackboard.Watch` listeners directly inside `BlackboardCondition`

Registering a listener on a shared squad parent blackboard from every agent's `BlackboardCondition` would leak closures in the parent blackboard whenever an agent dies unless every tree had an explicit `Close()` lifecycle. Polling `KeyRevision(key)` on `Tick` achieves identical next-tick abort semantics with zero allocations and automatic GC safety, while `Blackboard.Watch` remains available for imperative game-code observers.

### Wrapping `Node` in a separate `BlackboardSelector` instead of extending `Selector`

Requiring callers and YAML authors to choose a different selector type for observer aborts would split the API and surprise users coming from Unreal/Beehave/LimboAI where standard `Selector` honors decorator abort modes. Precomputing `hasObserver` in `Selector` keeps one unified composite with zero overhead for non-blackboard trees.

### Separate typed YAML fields (`boolValue`, `intValue`, `floatValue`, ...) instead of `value` + optional `valueType`

Seven mutually exclusive value fields would bloat every YAML condition and complicate authoring. Natural YAML scalars and 2/3-element arrays inferred from declared `blackboard:` key types (with optional `valueType` disambiguation) keep files readable while staying strictly validated by both `ValidateBehaviorTree` and `bt.v1.json`.

## Consequences

- **Benefits**: Designers and AI tools can author, diff, and validate behavior trees in `*.bt.yaml` with editor schema completion; YAML trees compile into the exact same runtime `Node` types as Go-built trees; per-agent and shared squad blackboards support typed keys, change listeners, and zero-allocation observer aborts; `game/ai` stays Stable with no Experimental imports.
- **Costs**: `BTNodeDoc` is a single tagged union struct whose per-type field restrictions are enforced by `ValidateBehaviorTree` rather than JSON Schema `oneOf` (matching `cmd/gameschema`'s generator design); `Selector` and `Sequence` carry one extra `hasObserver bool` field.

## Verification

- `packages/game/ai/bt_yaml_test.go` tests all 7 `Blackboard` types, parent/child scoping, shadowing, `Watch` propagation/cancellation, `BlackboardCondition` operators and all 4 abort modes, 0 allocs/op on `Tick`, byte-identical patrol/chase/attack leaf trace between YAML and Go-built trees, all 21 YAML node types, subtree parameter and key bindings (including transitive remapping and DAG reuse), cycle detection with exact file and line via `*loaderr.Error`, invalid field/type/op/abort line-number errors, and `formattest.Run` conformance against `testdata/bt/*.bt.yaml` and `docs/{en,zh}/formats.md`.
- `packages/game/cmd/gameschema/main_test.go` adds `TestBTSchemaShapeAndValidation` verifying `bt.v1.json` structure, doc descriptions, valid fixtures, and rejected invalid YAML.
- Verified with `go run ./cmd/gameschema -check`, `go tool nsgo test ./ai/...`, `go tool nsgo test ./cmd/gameschema/...`, `go tool nsgo test . -- -run 'TestFormatPackagesRunConformance|TestFormatsPage|TestStabilityTable|TestAgentSkillValid'`, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./tools/changelog check`, and `bun run verify-notes`.
